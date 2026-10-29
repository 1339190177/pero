package session

import (
	"errors"
	"fmt"
	"log/slog"
	"time"
)

// ErrSessionForkUnavailable means the requested history does not contain a
// completed turn that can be used as a stable fork boundary.
var ErrSessionForkUnavailable = errors.New("harness: session fork unavailable")

// ErrInvalidSessionForkAnchor means an anchor cannot be represented by DSH's
// non-negative JSON safe-integer sequence contract.
var ErrInvalidSessionForkAnchor = errors.New("harness: invalid session fork anchor")

const maxJSONSafeInteger = uint64(1<<53 - 1)

// ForkCompleted creates a detached child from the completed turn containing
// atSeq. A nil or past-end anchor selects the last completed turn. Facts
// between that turn and the next turn/start are included, matching DSH's
// stable-prefix semantics. The child receives one session/end-seed marker and
// is not published to a SessionStore or AgentManager.
func (s *Session) ForkCompleted(id SessionID, atSeq *uint64) (*Session, error) {
	if s == nil {
		return nil, errors.New("harness: fork source session is nil")
	}
	if id == "" {
		return nil, errors.New("harness: fork child session id is empty")
	}
	if id == s.header.ID {
		return nil, errors.New("harness: fork child session id matches source")
	}
	if atSeq != nil && *atSeq > maxJSONSafeInteger {
		return nil, fmt.Errorf("%w: atSeq must be a non-negative safe integer", ErrInvalidSessionForkAnchor)
	}

	events := s.Events()
	lastSeq := uint64(0)
	if len(events) != 0 {
		lastSeq = events[len(events)-1].Seq
	}
	boundary := -1
	if atSeq != nil {
		for index, event := range events {
			if event.Type == "turn/end" && event.Seq >= *atSeq {
				boundary = index
				break
			}
		}
	}
	if boundary < 0 && (atSeq == nil || *atSeq > lastSeq) {
		for index := len(events) - 1; index >= 0; index-- {
			if events[index].Type == "turn/end" {
				boundary = index
				break
			}
		}
	}
	if boundary < 0 {
		if atSeq != nil && *atSeq <= lastSeq {
			return nil, fmt.Errorf("%w: session %q has not completed the turn containing event %d", ErrSessionForkUnavailable, s.header.ID, *atSeq)
		}
		return nil, fmt.Errorf("%w: session %q has no completed turn", ErrSessionForkUnavailable, s.header.ID)
	}

	cut := boundary + 1
	for cut < len(events) && events[cut].Type != "turn/start" {
		cut++
	}
	seed := cloneEvents(events[:cut])
	if _, err := inspectCompactionLog(seed); err != nil {
		return nil, fmt.Errorf("harness: validate fork seed: %w", err)
	}
	if open, err := inspectOpenTurn(seed); err != nil {
		return nil, fmt.Errorf("harness: validate fork turns: %w", err)
	} else if open != nil {
		return nil, fmt.Errorf("%w: session %q fork boundary leaves turn %d open", ErrSessionForkUnavailable, s.header.ID, *open)
	}

	child := &Session{
		header: SessionHeader{
			Version: SessionFormatVersion, ID: id, CreatedAt: time.Now().UTC(), CWD: s.header.CWD, ManagedCWD: s.header.ManagedCWD,
			ParentSession: s.header.ID, SeedLength: uint64(len(seed)),
			InboxSeedLength: min(executableInboxSeedLength(s.header), uint64(len(seed))),
		},
		logger:      slog.Default(),
		events:      seed,
		subscribers: make(map[uint64]func(Event)),
	}
	for _, event := range child.events {
		if err := child.applySurfaceEventLocked(event); err != nil {
			return nil, fmt.Errorf("harness: rebuild fork surface at seq %d: %w", event.Seq, err)
		}
	}
	// Every child owns its cut marker, even when the prefix ends in an
	// ancestor marker. Inherited events remain unchanged.
	if _, err := child.Append("session/end-seed", struct {
		Inherited bool `json:"inherited"`
	}{Inherited: true}); err != nil {
		return nil, fmt.Errorf("harness: append fork seed boundary: %w", err)
	}
	child.firstLiveSeq = uint64(len(child.events) + 1)
	return child, nil
}

// ForkCompletedTurnsForSubagent creates a detached delegated Session seeded
// through the source's last completed turn/end. Unlike the workspace-facing
// ForkCompleted operation, it excludes between-turn maintenance facts so the
// inherited model prefix matches DSH's fork-in-process provider exactly.
func (s *Session) ForkCompletedTurnsForSubagent(id SessionID, depth int) (*Session, error) {
	if s == nil {
		return nil, errors.New("harness: fork source session is nil")
	}
	if id == "" || id == s.header.ID {
		return nil, errors.New("harness: invalid fork subagent child id")
	}
	if depth <= 0 {
		return nil, errors.New("harness: fork subagent depth must be positive")
	}
	events := s.Events()
	boundary := -1
	for index := len(events) - 1; index >= 0; index-- {
		if events[index].Type == "turn/end" {
			boundary = index
			break
		}
	}
	if boundary < 0 {
		return nil, fmt.Errorf("%w: session %q has no completed turn", ErrSessionForkUnavailable, s.header.ID)
	}
	child, err := forkSessionFromSeed(s, id, cloneEvents(events[:boundary+1]))
	if err != nil {
		return nil, err
	}
	child.header.Origin = "subagent"
	child.header.InboxSeedLength = child.header.SeedLength
	child.header.DelegationDepth = depth
	return child, nil
}

func forkSessionFromSeed(source *Session, id SessionID, seed []Event) (*Session, error) {
	if _, err := inspectCompactionLog(seed); err != nil {
		return nil, fmt.Errorf("harness: validate fork seed: %w", err)
	}
	if open, err := inspectOpenTurn(seed); err != nil {
		return nil, fmt.Errorf("harness: validate fork turns: %w", err)
	} else if open != nil {
		return nil, fmt.Errorf("%w: session %q fork boundary leaves turn %d open", ErrSessionForkUnavailable, source.header.ID, *open)
	}
	child := &Session{
		header: SessionHeader{
			Version: SessionFormatVersion, ID: id, CreatedAt: time.Now().UTC(), CWD: source.header.CWD, ManagedCWD: source.header.ManagedCWD,
			ParentSession: source.header.ID, SeedLength: uint64(len(seed)),
			InboxSeedLength: min(executableInboxSeedLength(source.header), uint64(len(seed))),
		},
		logger: slog.Default(), events: seed, subscribers: make(map[uint64]func(Event)),
	}
	for _, event := range child.events {
		if err := child.applySurfaceEventLocked(event); err != nil {
			return nil, fmt.Errorf("harness: rebuild fork surface at seq %d: %w", event.Seq, err)
		}
	}
	// Every child owns its cut marker, even when the prefix ends in an
	// ancestor marker. Inherited events remain unchanged.
	if _, err := child.Append("session/end-seed", struct {
		Inherited bool `json:"inherited"`
	}{Inherited: true}); err != nil {
		return nil, fmt.Errorf("harness: append fork seed boundary: %w", err)
	}
	child.firstLiveSeq = uint64(len(child.events) + 1)
	return child, nil
}

func cloneEvents(source []Event) []Event {
	result := make([]Event, len(source))
	for index, event := range source {
		result[index] = event.Clone()
	}
	return result
}

// ForkCompletedTurnsForSubagentAt retains the exact completed parent prefix
// selected by a detached provider contribution. It does not accept new events
// or header authority from that provider.
func (s *Session) ForkCompletedTurnsForSubagentAt(id SessionID, depth int, endSeq uint64) (*Session, error) {
	if s == nil || id == "" || id == s.Header().ID || depth <= 0 || endSeq == 0 || endSeq > maxJSONSafeInteger {
		return nil, errors.New("harness: invalid exact subagent seed boundary")
	}
	events := s.Events()
	if endSeq > uint64(len(events)) || events[endSeq-1].Seq != endSeq || events[endSeq-1].Type != "turn/end" {
		return nil, ErrInvalidSessionForkAnchor
	}
	child, err := forkSessionFromSeed(s, id, cloneEvents(events[:endSeq]))
	if err != nil {
		return nil, err
	}
	messages, err := child.DeriveMessages()
	if err != nil {
		return nil, err
	}
	pending := make(map[ToolCallID]bool)
	for _, message := range messages {
		for _, block := range message.Content {
			switch block.Type {
			case BlockToolCall:
				if block.ToolCallID == "" || pending[block.ToolCallID] {
					return nil, errors.New("harness: unbalanced tool call in subagent seed")
				}
				pending[block.ToolCallID] = true
			case BlockToolResult:
				if !pending[block.ToolCallID] {
					return nil, errors.New("harness: unmatched tool result in subagent seed")
				}
				delete(pending, block.ToolCallID)
			}
		}
	}
	if len(pending) != 0 {
		return nil, errors.New("harness: subagent seed has pending tool calls")
	}
	child.header.Origin = "subagent"
	child.header.InboxSeedLength = child.header.SeedLength
	child.header.DelegationDepth = depth
	return child, nil
}
