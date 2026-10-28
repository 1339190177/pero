// Contract algorithms ported from DeepSeek DSH (MIT); see
// DEEPSEEK_LICENSE.txt for the retained notice.
package session

import (
	"bytes"
	"encoding/json"
	"fmt"
	"github.com/phodal/pero/harness/go/model"
)

type DeveloperMessageEvent struct {
	Turn      int     `json:"turn"`
	Step      int     `json:"step"`
	Message   Message `json:"message"`
	HeaderSeq uint64  `json:"headerSeq,omitempty"`
}

// FoldToolHistory reconstructs the actual selected declaration series from
// committed log order. Legacy headers without matching updates fall back to
// their latest active declarations, exactly like official Session.toolHistory.
func FoldToolHistory(events []Event) (*model.ToolHistory, error) {
	headers := map[uint64][]model.ToolSchema{}
	declared := map[string]model.ToolSchema{}
	available := map[string]bool{}
	var active []model.ToolSchema
	history := &model.ToolHistory{Tools: []model.ToolSchema{}, Updates: []model.ToolHistoryUpdate{}}
	initialized := false
	for _, event := range events {
		switch event.Type {
		case "request/header":
			var header RequestHeaderEvent
			if err := json.Unmarshal(event.Data, &header); err != nil {
				return nil, fmt.Errorf("tool history header at %d: %w", event.Seq, err)
			}
			tools := header.Tools
			if tools == nil {
				tools = []model.ToolSchema{}
			}
			headers[event.Seq] = tools
			redeclared := false
			for _, tool := range tools {
				if previous, ok := declared[tool.Name]; ok {
					left, _ := json.Marshal(previous)
					right, _ := json.Marshal(tool)
					if !bytes.Equal(left, right) {
						redeclared = true
						break
					}
				}
			}
			if !initialized || header.Reason == "series" || header.StartsSeries || redeclared {
				initialized = true
				declared = map[string]model.ToolSchema{}
				available = map[string]bool{}
				for _, tool := range tools {
					declared[tool.Name] = tool
					available[tool.Name] = true
				}
				history = &model.ToolHistory{Tools: tools, Updates: []model.ToolHistoryUpdate{}}
			}
			active = tools
		case "developer/message":
			var payload DeveloperMessageEvent
			if err := json.Unmarshal(event.Data, &payload); err != nil {
				return nil, fmt.Errorf("tool history developer at %d: %w", event.Seq, err)
			}
			additions := []model.ToolSchema{}
			for _, block := range payload.Message.Content {
				if block.Type != model.BlockToolAddition {
					continue
				}
				found := false
				for _, tool := range headers[payload.HeaderSeq] {
					if tool.Name == block.ToolName {
						additions = append(additions, tool)
						found = true
						break
					}
				}
				if !found {
					return nil, fmt.Errorf("tool history: missing definition for %s", block.ToolName)
				}
			}
			for _, tool := range additions {
				declared[tool.Name] = tool
			}
			for _, block := range payload.Message.Content {
				if block.Type == model.BlockToolAddition {
					available[block.ToolName] = true
				} else if block.Type == model.BlockToolRemoval {
					delete(available, block.ToolName)
				}
			}
			history.Updates = append(history.Updates, model.ToolHistoryUpdate{MessageID: payload.Message.ID, Additions: additions})
		}
	}
	mismatch := len(active) != len(available)
	for _, tool := range active {
		if !available[tool.Name] {
			mismatch = true
		}
	}
	if mismatch {
		history = &model.ToolHistory{Tools: active, Updates: []model.ToolHistoryUpdate{}}
	}
	return model.CloneToolHistory(history), nil
}
func (s *Session) ToolHistory() (*model.ToolHistory, error) { return FoldToolHistory(s.Events()) }

// AppendToolUpdates publishes the name delta attached to one newly committed
// change/resume header. Schema redefinition starts a new declaration series in
// FoldToolHistory; it does not fabricate a second addition for the same name.
func (s *Session) AppendToolUpdates(turn, step int, previous, current []model.ToolSchema, headerSeq uint64) error {
	before, after := map[string]bool{}, map[string]bool{}
	for _, tool := range previous {
		before[tool.Name] = true
	}
	for _, tool := range current {
		after[tool.Name] = true
	}
	content := []model.ContentBlock{}
	hasAdditions := false
	for _, tool := range current {
		if !before[tool.Name] {
			content = append(content, model.ContentBlock{Type: model.BlockToolAddition, ToolName: tool.Name})
			hasAdditions = true
		}
	}
	for _, tool := range previous {
		if !after[tool.Name] {
			content = append(content, model.ContentBlock{Type: model.BlockToolRemoval, ToolName: tool.Name})
		}
	}
	if len(content) == 0 {
		return nil
	}
	payload := DeveloperMessageEvent{Turn: turn, Step: step, Message: model.Message{ID: fmt.Sprintf("%s:tools:%d", s.Header().ID, headerSeq), Role: model.RoleDeveloper, Source: json.RawMessage(`{"kind":"tool-registry"}`), Content: content}}
	if hasAdditions {
		payload.HeaderSeq = headerSeq
	}
	_, err := s.Append("developer/message", payload)
	return err
}
