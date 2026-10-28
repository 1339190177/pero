package session

import (
	"encoding/json"
	"fmt"
	"sort"
	"time"

	"github.com/phodal/pero/harness/go/model"
)

// SystemPromptEvent is the canonical durable prompt surface payload. Empty
// content reserves the prompt position without sending a provider message.
type SystemPromptEvent struct {
	Turn    int     `json:"turn"`
	Step    int     `json:"step"`
	Message Message `json:"message"`
}

type systemPromptNode struct {
	seq   uint64
	index int
	text  string
}

func systemPromptNodes(events []Event, surface []uint64) ([]systemPromptNode, error) {
	nodes := make([]systemPromptNode, 0)
	for index, seq := range surface {
		if seq == 0 || seq > uint64(len(events)) {
			return nil, fmt.Errorf("harness: invalid system surface seq %d", seq)
		}
		event := events[seq-1]
		if event.Type != "system/message" {
			continue
		}
		if err := validateSystemPromptEvent(event); err != nil {
			return nil, err
		}
		var payload SystemPromptEvent
		if err := json.Unmarshal(event.Data, &payload); err != nil {
			return nil, err
		}
		text := ""
		if len(payload.Message.Content) != 0 {
			text = payload.Message.Content[0].Text
		}
		nodes = append(nodes, systemPromptNode{seq: seq, index: index, text: text})
	}
	return nodes, nil
}

func nonCanonicalSystemPromptError(seq uint64) error {
	return fmt.Errorf("harness: historical system content at seq %d needs an explicit canonical replacement before execution", seq)
}

func validateSystemPromptEvent(event Event) error {
	return validateSystemPromptEventWithDecoder(event, decodeSurfaceMessage)
}

func validateSystemPromptEventWithDecoder(event Event, decode func(Event) (Message, error)) error {
	if event.Type != "system/message" {
		return nil
	}
	message, err := decode(event)
	if err != nil {
		return fmt.Errorf("harness: decode system/message: %w", err)
	}
	var source struct {
		Kind string `json:"kind"`
	}
	if message.ID == "" || message.Role != model.RoleSystem || json.Unmarshal(message.Source, &source) != nil || source.Kind != "system-prompt" || len(message.Content) > 1 || len(message.Content) == 1 && (message.Content[0].Type != model.BlockText || message.Content[0].Text == "") {
		return fmt.Errorf("harness: system/message requires an identified system-prompt message with empty content or one nonempty text block")
	}
	return nil
}

// CommitSystemPrompt reconciles the effective prompt under the route's admitted
// update capability. All replacements commit together and retain their original
// source events. On historical logs without a system node this appends, exactly
// like official SystemPromptProjection; it never rewrites an old log prefix.
func (s *Session) CommitSystemPrompt(turn, step int, rendered string, inHistory, startsSeries bool) error {
	s.notifyMu.Lock()
	defer s.notifyMu.Unlock()
	s.mu.Lock()
	nodes, err := systemPromptNodes(s.events, s.surface)
	if err != nil {
		s.mu.Unlock()
		return err
	}
	type change struct {
		seq  uint64
		text string
	}
	var changes []change
	if len(nodes) == 0 {
		changes = append(changes, change{text: rendered})
	} else {
		head := nodes[0]
		latest := head
		for _, node := range nodes {
			if node.text != "" {
				latest = node
			}
		}
		if !inHistory || startsSeries || rendered == "" {
			for _, node := range nodes[1:] {
				if node.text != "" {
					changes = append(changes, change{seq: node.seq})
				}
			}
			if head.text != rendered {
				changes = append(changes, change{seq: head.seq, text: rendered})
			}
		} else if latest.text != rendered {
			changes = append(changes, change{text: rendered})
		}
	}
	if len(changes) == 0 {
		s.mu.Unlock()
		return nil
	}
	candidate := &Session{events: append([]Event(nil), s.events...), surface: append([]uint64(nil), s.surface...)}
	committed := make([]Event, 0, len(changes))
	for _, change := range changes {
		seq := uint64(len(candidate.events) + 1)
		content := []ContentBlock{}
		if change.text != "" {
			content = append(content, ContentBlock{Type: model.BlockText, Text: change.text})
		}
		payload := SystemPromptEvent{Turn: turn, Step: step, Message: Message{ID: fmt.Sprintf("%s:system:%d", s.header.ID, seq), Role: model.RoleSystem, Source: json.RawMessage(`{"kind":"system-prompt"}`), Content: content}}
		raw, err := json.Marshal(payload)
		if err != nil {
			s.mu.Unlock()
			return err
		}
		event := Event{Seq: seq, Time: time.Now().UTC(), Type: "system/message", Data: raw, SurfaceOp: json.RawMessage(`"append"`)}
		if change.seq != 0 {
			event.SurfaceOp = json.RawMessage(fmt.Sprintf(`{"op":"replace","start":%d,"end":%d}`, change.seq, change.seq))
			event.SourceEventSeqs = []uint64{change.seq}
		}
		if err := candidate.applySurfaceEventLocked(event); err != nil {
			s.mu.Unlock()
			return err
		}
		candidate.events = append(candidate.events, event)
		committed = append(committed, event)
	}
	s.events, s.surface = candidate.events, candidate.surface
	ids := make([]uint64, 0, len(s.subscribers))
	for id := range s.subscribers {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	listeners := make([]func(Event), 0, len(ids))
	for _, id := range ids {
		listeners = append(listeners, s.subscribers[id])
	}
	s.mu.Unlock()
	for _, event := range committed {
		for _, listener := range listeners {
			s.notifySubscriber(listener, event)
		}
	}
	return nil
}

// ProviderMessages projects the legacy Go adapter API: System is sent separately
// and must not also appear in Messages. Durable ContextPlan keeps these nodes.
func ProviderMessages(messages []Message) []Message {
	result := make([]Message, 0, len(messages))
	for _, message := range messages {
		if message.Role != model.RoleSystem {
			result = append(result, cloneMessage(message))
		}
	}
	return result
}

// EffectiveSystemPrompt reads the last active canonical prompt. Legacy logs
// without any surviving system node retain their request-header prompt.
func (s *Session) EffectiveSystemPrompt() (string, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	nodes, err := systemPromptNodes(s.events, s.surface)
	if err != nil {
		return "", err
	}
	if len(nodes) > 0 {
		text := nodes[0].text
		for _, node := range nodes {
			if node.text != "" {
				text = node.text
			}
		}
		return text, nil
	}
	for index := len(s.events) - 1; index >= 0; index-- {
		if s.events[index].Type != "request/header" {
			continue
		}
		var header RequestHeaderEvent
		if err := json.Unmarshal(s.events[index].Data, &header); err != nil {
			return "", err
		}
		return header.System, nil
	}
	return "", nil
}

// SurfaceContentGeneration follows the official replacement/projection counter:
// ordinary message appends do not invalidate the already cached request prefix.
func (s *Session) SurfaceContentGeneration() uint64 {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var generation uint64
	for _, event := range s.events {
		if event.Type == imageOffloadEventType || (IsSurfaceEventType(event.Type) && len(event.SurfaceOp) > 0 && string(event.SurfaceOp) != `"append"`) {
			generation++
		}
	}
	return generation
}

// CommitRuntimeContext appends the current dynamic snapshot without modifying
// a saved system prompt. It follows the retained surface (including restore and
// compaction), rather than trusting the most recent historical snapshot.
func (s *Session) CommitRuntimeContext(snapshot model.RuntimeContextSnapshot) error {
	current := snapshot.Text
	events := s.Events()
	surface := make(map[uint64]bool)
	for _, seq := range s.SurfaceSeqs() {
		surface[seq] = true
	}
	seen := false
	var retained *string
	for i := len(events) - 1; i >= 0; i-- {
		event := events[i]
		if event.Type != "user/message" {
			continue
		}
		message, err := decodeSurfaceMessage(event)
		if err != nil {
			return err
		}
		var source struct {
			Kind string `json:"kind"`
		}
		if json.Unmarshal(message.Source, &source) != nil || source.Kind != "runtime-context" {
			continue
		}
		seen = true
		if surface[event.Seq] {
			if len(message.Content) == 1 && message.Content[0].Type == model.BlockText {
				text := message.Content[0].Text
				retained = &text
			}
			break
		}
	}
	if !seen && current == "" {
		return nil
	}
	if current == "" {
		current = "Current runtime context: none. Earlier runtime-context snapshots no longer apply."
	}
	if retained != nil && *retained == current {
		return nil
	}
	source := struct {
		Kind     string                        `json:"kind"`
		Form     string                        `json:"form,omitempty"`
		Sections []model.RuntimeContextSection `json:"sections,omitempty"`
	}{Kind: "runtime-context"}
	if snapshot.Text != "" && len(snapshot.Sections) > 0 {
		source.Form = "snapshot"
		source.Sections = snapshot.Sections
	}
	encoded, err := json.Marshal(source)
	if err != nil {
		return err
	}
	_, err = s.Append("user/message", Message{
		ID:   fmt.Sprintf("%s:runtime-context:%d", s.Header().ID, len(events)+1),
		Role: model.RoleUser, Source: encoded,
		Content: []ContentBlock{{Type: model.BlockText, Text: current}},
	})
	return err
}
