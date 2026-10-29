package session

import (
	"encoding/json"
	"github.com/phodal/pero/harness/go/model"
	"reflect"
	"testing"
)

func TestToolHistoryLiveColdSeriesAndHistoricalSchema(t *testing.T) {
	s := NewSession("tools-history", "")
	search := model.ToolSchema{Name: "search", InputSchema: json.RawMessage(`{"type":"object"}`)}
	fetch := model.ToolSchema{Name: "fetch", InputSchema: json.RawMessage(`{"type":"object","properties":{"url":{"type":"string"}}}`)}
	first, err := s.Append("request/header", RequestHeaderEvent{Reason: "initial", Tools: []ToolSchema{search}})
	if err != nil {
		t.Fatal(err)
	}
	_ = first
	added, err := s.Append("request/header", RequestHeaderEvent{Reason: "change", Tools: []ToolSchema{search, fetch}})
	if err != nil {
		t.Fatal(err)
	}
	if err = s.AppendToolUpdates(1, 1, []ToolSchema{search}, []ToolSchema{search, fetch}, added.Seq); err != nil {
		t.Fatal(err)
	}
	removed, err := s.Append("request/header", RequestHeaderEvent{Reason: "change", Tools: []ToolSchema{fetch}})
	if err != nil {
		t.Fatal(err)
	}
	if err = s.AppendToolUpdates(1, 2, []ToolSchema{search, fetch}, []ToolSchema{fetch}, removed.Seq); err != nil {
		t.Fatal(err)
	}
	got, err := s.ToolHistory()
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Tools) != 1 || got.Tools[0].Name != "search" || len(got.Updates) != 2 || len(got.Updates[0].Additions) != 1 || got.Updates[0].Additions[0].Name != "fetch" || len(got.Updates[1].Additions) != 0 {
		t.Fatalf("historical definitions=%+v", got)
	}
	cold, err := RestoreSession(s.Header(), s.Events())
	if err != nil {
		t.Fatal(err)
	}
	replay, err := cold.ToolHistory()
	if err != nil || !reflect.DeepEqual(got, replay) {
		t.Fatalf("cold history=%+v %v", replay, err)
	}
	messages, err := s.DeriveMessages()
	if err != nil || len(messages) != 2 || messages[0].Role != model.RoleDeveloper || messages[1].Content[0].Type != model.BlockToolRemoval {
		t.Fatalf("developer surface=%+v %v", messages, err)
	}
	got.Updates[0].Additions[0].InputSchema[0] = '!'
	fresh, _ := s.ToolHistory()
	if !json.Valid(fresh.Updates[0].Additions[0].InputSchema) {
		t.Fatal("caller mutated history")
	}
	changed := fetch
	changed.Description = "new definition"
	if _, err = s.Append("request/header", RequestHeaderEvent{Reason: "change", Tools: []ToolSchema{changed}}); err != nil {
		t.Fatal(err)
	}
	next, _ := s.ToolHistory()
	if len(next.Updates) != 0 || len(next.Tools) != 1 || next.Tools[0].Description != "new definition" {
		t.Fatalf("schema redeclaration did not start series: %+v", next)
	}
}
func TestDeveloperMessageAdmissionIsAtomicAndReplayValidated(t *testing.T) {
	s := NewSession("developer-admission", "")
	header, err := s.Append("request/header", RequestHeaderEvent{Reason: "initial", Tools: []ToolSchema{{Name: "new", InputSchema: json.RawMessage(`{"type":"object"}`)}}})
	if err != nil {
		t.Fatal(err)
	}
	valid := map[string]any{"turn": 1, "step": 1, "headerSeq": header.Seq, "message": map[string]any{"id": "update", "role": "developer", "source": map[string]any{"kind": "tool-registry"}, "content": []any{map[string]any{"type": "tool-addition", "toolName": "new"}}}}
	encode, _ := json.Marshal(valid)
	cases := []struct {
		name   string
		change func(map[string]any)
	}{
		{"missing-header", func(v map[string]any) { delete(v, "headerSeq") }},
		{"future-header", func(v map[string]any) { v["headerSeq"] = 99 }},
		{"unknown-tool", func(v map[string]any) {
			v["message"].(map[string]any)["content"].([]any)[0].(map[string]any)["toolName"] = "absent"
		}},
		{"inline-definition", func(v map[string]any) {
			v["message"].(map[string]any)["content"].([]any)[0].(map[string]any)["tool"] = map[string]any{}
		}},
		{"wrong-role", func(v map[string]any) { v["message"].(map[string]any)["role"] = "user" }},
		{"removal-with-header", func(v map[string]any) {
			v["message"].(map[string]any)["content"].([]any)[0].(map[string]any)["type"] = "tool-removal"
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var v map[string]any
			_ = json.Unmarshal(encode, &v)
			tc.change(v)
			before := len(s.Events())
			if _, err := s.Append("developer/message", v); err == nil {
				t.Fatal("malformed developer message committed")
			}
			if len(s.Events()) != before {
				t.Fatal("rejected message mutated log")
			}
			raw, _ := json.Marshal(v)
			events := append(s.Events(), Event{Seq: uint64(before + 1), Type: "developer/message", Data: raw, SurfaceOp: json.RawMessage(`"append"`)})
			if _, err := RestoreSession(s.Header(), events); err == nil {
				t.Fatal("malformed developer message restored")
			}
		})
	}
	if _, err = s.Append("developer/message", valid); err != nil {
		t.Fatal(err)
	}
}
