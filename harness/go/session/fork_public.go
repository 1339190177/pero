package session

import (
	"errors"
	"fmt"
	"time"
)

// ForkPublic creates an ordinary child at the public DSH Session Controller
// boundary. An explicit anchor is the exact inclusive Go event sequence (the
// frontend translates DSH's zero-based coordinate). Without an anchor, the
// latest completed prefix ends before the next input/turn begins. Inherited
// inbox facts remain facts; constructing this child does not authorize a wake.
// ForkCompleted retains its separate completed-turn selection contract.
func (s *Session) ForkPublic(id SessionID, atSeq *uint64) (*Session, error) {
	if s == nil {
		return nil, errors.New("harness: fork source session is nil")
	}
	source := s.Header()
	if id == "" || id == source.ID {
		return nil, errors.New("harness: invalid fork child session id")
	}
	if atSeq != nil && *atSeq > maxJSONSafeInteger {
		return nil, fmt.Errorf("%w: atSeq must be a non-negative safe integer", ErrInvalidSessionForkAnchor)
	}
	events := s.Events()
	boundary := latestPublicForkBoundary(events)
	if atSeq != nil {
		boundary = -1
		if *atSeq > 0 && *atSeq <= uint64(len(events)) && events[*atSeq-1].Seq == *atSeq {
			boundary = int(*atSeq - 1)
		}
	}
	if boundary < 0 {
		if atSeq != nil {
			return nil, fmt.Errorf("%w: event %d does not exist in session %q", ErrSessionForkUnavailable, *atSeq, source.ID)
		}
		return nil, fmt.Errorf("%w: session %q has no completed turn", ErrSessionForkUnavailable, source.ID)
	}
	inherited := uint64(boundary + 1)
	seed := cloneEvents(events[:boundary+1])
	seed = append(seed, Event{Seq: inherited + 1, Time: events[boundary].Time, Type: "session/end-seed", Data: []byte(`{"inherited":true}`)})
	seed = append(seed, openTurnClosers(seed, "forked")...)
	header := SessionHeader{
		Version: SessionFormatVersion, ID: id, CreatedAt: time.Now().UTC(),
		CWD: source.CWD, ManagedCWD: source.ManagedCWD,
		ParentSession: source.ID, SeedLength: inherited,
		InboxSeedLength: min(executableInboxSeedLength(source), inherited),
	}
	child, err := RestoreSession(header, seed)
	if err != nil {
		return nil, fmt.Errorf("harness: validate public fork seed: %w", err)
	}
	// A new child is not a reopened process generation, despite using the same
	// invariant-preserving replay constructor to materialize its detached seed.
	child.restored.Store(false)
	return child, nil
}

func latestPublicForkBoundary(events []Event) int {
	boundary := -1
	for i := len(events) - 1; i >= 0; i-- {
		if events[i].Type == "turn/end" {
			boundary = i
			break
		}
	}
	if boundary < 0 {
		return -1
	}
	for i := boundary + 1; i < len(events); i++ {
		event := events[i]
		if event.Type == "turn/start" || event.Type == "agent/inbox/spliced" ||
			(event.Type == "user/message" && (len(event.SurfaceOp) == 0 || string(event.SurfaceOp) == `"append"`)) {
			break
		}
		boundary = i
	}
	return boundary
}
