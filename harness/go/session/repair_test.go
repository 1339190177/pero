package session

import (
	"encoding/json"
	"github.com/phodal/pero/harness/go/model"
	"reflect"
	"testing"
)

func TestInterruptedTurnRepairReceiptsAreConservativeAndIdempotent(t *testing.T) {
	session := NewSession("repair", "")
	appendRow := func(kind string, value any) {
		t.Helper()
		if _, err := session.Append(kind, value); err != nil {
			t.Fatal(err)
		}
	}
	appendRow("turn/start", map[string]int{"turn": 1})
	appendRow("step/start", map[string]int{"turn": 1, "step": 1})
	appendRow("assistant/message", map[string]any{"turn": 1, "step": 1, "message": Message{Role: model.RoleAssistant, Content: []ContentBlock{{Type: BlockToolCall, ToolCallID: "started", Name: "write_file"}, {Type: BlockToolCall, ToolCallID: "never-started", Name: "read_file"}, {Type: BlockToolCall, ToolCallID: "finished", Name: "read_file"}}}})
	call, err := session.Append("tool/call", map[string]any{"turn": 1, "step": 1, "callId": "started", "name": "write_file"})
	if err != nil {
		t.Fatal(err)
	}
	appendRow("tool/result", ContentBlock{Type: BlockToolResult, ToolCallID: "finished", Content: []ContentBlock{{Type: BlockText, Text: "ok"}}})
	original := session.Events()
	closers := InterruptedTurnClosers(original)
	if len(closers) != 4 {
		t.Fatalf("closers=%+v", closers)
	}
	if err := session.RepairInterruptedTurn(); err != nil {
		t.Fatal(err)
	}
	events := session.Events()
	if !reflect.DeepEqual(events[len(original):], closers) {
		t.Fatal("repair differs from deterministic closer plan")
	}
	if !reflect.DeepEqual(events[:len(original)], original) {
		t.Fatal("repair rewrote committed prefix")
	}
	for i, code := range []string{ToolOutcomeUnknown, ToolNotStarted} {
		var block ContentBlock
		if err := json.Unmarshal(closers[i].Data, &block); err != nil {
			t.Fatal(err)
		}
		var meta struct {
			Error struct {
				Code string `json:"code"`
			} `json:"error"`
		}
		_ = json.Unmarshal(block.Meta, &meta)
		if !block.IsError || meta.Error.Code != code {
			t.Fatalf("receipt=%+v meta=%s", block, block.Meta)
		}
	}
	if !reflect.DeepEqual(closers[0].SourceEventSeqs, []uint64{call.Seq}) || len(closers[1].SourceEventSeqs) != 0 {
		t.Fatal("started provenance lost")
	}
	if err := session.RepairInterruptedTurn(); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(session.Events(), events) {
		t.Fatal("repeat repair appended duplicate receipts")
	}
	if _, err := session.DeriveMessages(); err != nil {
		t.Fatal(err)
	}
}
func TestInterruptedTurnRepairPreservesClosedSteps(t *testing.T) {
	session := NewSession("closed-step", "")
	for _, row := range []struct {
		kind  string
		value any
	}{{"turn/start", map[string]int{"turn": 1}}, {"step/start", map[string]int{"turn": 1, "step": 1}}, {"assistant/message", map[string]any{"message": Message{Role: model.RoleAssistant, Content: []ContentBlock{{Type: BlockToolCall, ToolCallID: "old"}}}}}, {"step/end", map[string]int{"turn": 1, "step": 1}}} {
		if _, err := session.Append(row.kind, row.value); err != nil {
			t.Fatal(err)
		}
	}
	closers := InterruptedTurnClosers(session.Events())
	if len(closers) != 1 || closers[0].Type != "turn/end" {
		t.Fatalf("closed step repaired again: %+v", closers)
	}
}
