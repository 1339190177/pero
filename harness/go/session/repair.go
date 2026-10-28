package session

import (
	"encoding/json"
	"errors"
)

const ToolNotStarted = "TOOL_NOT_STARTED"
const ToolOutcomeUnknown = "TOOL_OUTCOME_UNKNOWN"
const interruptedStartedText = "The tool call was interrupted after it was recorded, but no result was durably recorded. Its outcome is unknown. Decide whether to retry from the tool semantics: retry only if the operation is read-only or idempotent; if it may have side effects, first verify external state or ask the user. Do not retry blindly."
const forkedStartedText = "The history inherited by this branch records this tool call starting but does not include its result. The parent session may have completed it after the fork point. Decide whether to retry from the tool semantics: retry only if the operation is read-only or idempotent; if it may have side effects, first verify external state or ask the user. Do not retry blindly."
const forkedNotStartedText = "The history inherited by this branch has no record of this tool call starting. The parent session may have executed it after the fork point. Decide whether to retry from the tool semantics: retry only if the operation is read-only or idempotent; if it may have side effects, first verify external state or ask the user. Do not retry blindly."
const interruptedNotStartedText = "The tool call was interrupted before the Harness recorded it as started. Retry it if it is still needed."

// InterruptedTurnClosers supplies missing receipts and lifecycle boundaries for
// an open crash tail. It never executes a tool or changes any committed event.
// Native receipts store the upstream error taxonomy in ContentBlock.Meta.
func InterruptedTurnClosers(events []Event) []Event {
	return openTurnClosers(events, "interrupted")
}

// openTurnClosers shares lifecycle recovery while retaining cause-specific
// model guidance: a parent may have executed beyond an inherited fork cut.
func openTurnClosers(events []Event, cause string) []Event {
	type pendingCall struct {
		id         ToolCallID
		turn, step int
		seq        uint64
	}
	var calls []pendingCall
	openTurn, openStep := -1, -1
	for _, event := range events {
		var cursor struct {
			Turn int `json:"turn"`
			Step int `json:"step"`
		}
		_ = json.Unmarshal(event.Data, &cursor)
		switch event.Type {
		case "turn/start":
			openTurn, openStep = cursor.Turn, -1
			calls = nil
		case "turn/end":
			openTurn, openStep = -1, -1
			calls = nil
		case "step/start":
			openStep = cursor.Step
		case "step/end":
			openStep = -1
			calls = nil
		case "assistant/message":
			var data struct {
				Message Message `json:"message"`
			}
			if json.Unmarshal(event.Data, &data) != nil {
				continue
			}
			for _, block := range data.Message.Content {
				if block.Type == BlockToolCall {
					found := false
					for i := range calls {
						if calls[i].id == block.ToolCallID {
							calls[i] = pendingCall{id: block.ToolCallID, turn: cursor.Turn, step: cursor.Step}
							found = true
							break
						}
					}
					if !found {
						calls = append(calls, pendingCall{id: block.ToolCallID, turn: cursor.Turn, step: cursor.Step})
					}
				}
			}
		case "tool/call":
			var data struct {
				CallID ToolCallID `json:"callId"`
			}
			_ = json.Unmarshal(event.Data, &data)
			for i := range calls {
				if calls[i].id == data.CallID {
					calls[i].seq = event.Seq
				}
			}
		case "tool/result":
			if len(event.SurfaceOp) != 0 && string(event.SurfaceOp) != `"append"` {
				continue
			}
			var result ContentBlock
			if json.Unmarshal(event.Data, &result) != nil {
				continue
			}
			for i := range calls {
				if calls[i].id == result.ToolCallID {
					calls = append(calls[:i], calls[i+1:]...)
					break
				}
			}
		}
	}
	if openTurn < 0 || len(events) == 0 {
		return nil
	}
	last := events[len(events)-1]
	closers := make([]Event, 0, len(calls)+2)
	appendEvent := func(kind string, data any, source []uint64) {
		raw, _ := json.Marshal(data)
		event := Event{Seq: last.Seq + uint64(len(closers)) + 1, Time: last.Time, Type: kind, Data: raw, SourceEventSeqs: source}
		if kind == "tool/result" {
			event.SurfaceOp = json.RawMessage(`"append"`)
		}
		closers = append(closers, event)
	}
	startedText, notStartedText := interruptedStartedText, interruptedNotStartedText
	if cause == "forked" {
		startedText, notStartedText = forkedStartedText, forkedNotStartedText
	}
	for _, call := range calls {
		code, name, text := ToolNotStarted, "ToolNotStartedError", notStartedText
		var source []uint64
		if call.seq != 0 {
			code, name, text = ToolOutcomeUnknown, "ToolOutcomeUnknownError", startedText
			source = []uint64{call.seq}
		}
		meta, _ := json.Marshal(map[string]any{"error": map[string]string{"name": name, "code": code}, "turn": call.turn, "step": call.step})
		appendEvent("tool/result", ContentBlock{Type: BlockToolResult, ToolCallID: call.id, IsError: true, Meta: meta, Content: []ContentBlock{{Type: BlockText, Text: text}}}, source)
	}
	if openStep >= 0 {
		appendEvent("step/end", map[string]int{"turn": openTurn, "step": openStep}, nil)
	}
	appendEvent("turn/end", map[string]any{"turn": openTurn, "reason": map[string]string{"kind": cause}}, nil)
	return closers
}

// RepairInterruptedTurn runs only on the inactive resume candidate. Each
// append is compare-and-set fenced; an unexpected writer aborts activation.
func (s *Session) RepairInterruptedTurn() error {
	if s == nil {
		return errors.New("harness: cannot repair a nil session")
	}
	for _, event := range InterruptedTurnClosers(s.Events()) {
		expected := event.Seq - 1
		if _, err := s.appendAtTime(event.Type, event.Data, event.SurfaceOp, event.SourceEventSeqs, &expected, &event.Time); err != nil {
			return err
		}
	}
	return nil
}
