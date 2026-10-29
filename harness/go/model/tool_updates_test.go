package model

import (
	"encoding/json"
	"reflect"
	"testing"
)

type toolProjectionCase struct {
	Name     string       `json:"name"`
	Messages []Message    `json:"messages"`
	Tools    []ToolSchema `json:"tools"`
	Mode     string       `json:"mode,omitempty"`
	History  *ToolHistory `json:"history,omitempty"`
}

func toolProjectionCases() []toolProjectionCase {
	search := ToolSchema{Name: "search", Description: "Search", InputSchema: json.RawMessage(`{"type":"object"}`)}
	fetch := ToolSchema{Name: "fetch", Description: "Fetch", InputSchema: json.RawMessage(`{"type":"object","properties":{"url":{"type":"string"}}}`)}
	user := Message{ID: "user", Role: RoleUser, Source: json.RawMessage(`{"kind":"user"}`), Content: []ContentBlock{{Type: BlockText, Text: "hello"}}}
	update := func(id string, blocks ...ContentBlock) Message {
		return Message{ID: id, Role: RoleDeveloper, Source: json.RawMessage(`{"kind":"tool-registry"}`), Content: blocks}
	}
	added := update("added", ContentBlock{Type: BlockToolAddition, ToolName: "fetch"})
	duplicate := update("duplicate", ContentBlock{Type: BlockToolAddition, ToolName: "fetch"})
	removed := update("removed", ContentBlock{Type: BlockToolRemoval, ToolName: "search"})
	restored := update("restored", ContentBlock{Type: BlockToolAddition, ToolName: "search"})
	history := &ToolHistory{Tools: []ToolSchema{search}, Updates: []ToolHistoryUpdate{{"added", []ToolSchema{fetch}}, {"duplicate", []ToolSchema{fetch}}, {"removed", []ToolSchema{}}, {"restored", []ToolSchema{search}}}}
	deferred := search
	deferred.DeferLoading = true
	return []toolProjectionCase{
		{"incapable", []Message{user, added}, []ToolSchema{deferred}, "", history},
		{"no-history", []Message{user, added}, []ToolSchema{fetch}, "in-history", nil},
		{"incomplete-prefix", []Message{user, added}, []ToolSchema{fetch}, "in-history", history},
		{"add-remove-readd", []Message{user, added, duplicate, removed, restored}, []ToolSchema{search, fetch}, "in-history", history},
		{"addition-only", []Message{user, added, duplicate, removed, restored}, []ToolSchema{fetch}, "addition-only", history},
		{"old-series", []Message{added, user}, []ToolSchema{search}, "in-history", &ToolHistory{Tools: []ToolSchema{search}, Updates: []ToolHistoryUpdate{}}},
		{"deferred-baseline", []Message{user, restored}, []ToolSchema{deferred}, "in-history", &ToolHistory{Tools: []ToolSchema{deferred}, Updates: []ToolHistoryUpdate{{"restored", []ToolSchema{search}}}}},
	}
}
func TestToolUpdatesAreCapabilitySpecificAndDetached(t *testing.T) {
	for _, test := range toolProjectionCases() {
		t.Run(test.Name, func(t *testing.T) {
			before, _ := json.Marshal(test)
			got, err := ProjectToolUpdates(test.Messages, test.Tools, test.Mode, test.History)
			if err != nil {
				t.Fatal(err)
			}
			if test.Mode == "" {
				for _, m := range got.Messages {
					if m.Role == RoleDeveloper {
						t.Fatal("unsupported route retained developer message")
					}
				}
				for _, tool := range got.Tools {
					if tool.DeferLoading {
						t.Fatal("unsupported route retained deferred tool")
					}
				}
			}
			if test.Name == "add-remove-readd" {
				if !reflect.DeepEqual([]string{got.Messages[0].ID, got.Messages[1].ID, got.Messages[2].ID, got.Messages[3].ID}, []string{"user", "added", "removed", "restored"}) {
					t.Fatalf("projected messages=%+v", got.Messages)
				}
				if len(got.Tools) != 2 || !got.Tools[1].DeferLoading {
					t.Fatalf("historical declarations=%+v", got.Tools)
				}
			}
			if test.Name == "addition-only" {
				if len(got.Tools) != 1 || got.Tools[0].Name != "fetch" || len(got.Messages) != 2 || got.Messages[1].ID != "added" {
					t.Fatalf("addition only=%+v", got)
				}
			}
			if len(got.Messages) > 0 && len(got.Messages[0].Content) > 0 {
				got.Messages[0].Content[0].Text = "mutated"
			}
			if len(got.Tools) > 0 && len(got.Tools[0].InputSchema) > 0 {
				got.Tools[0].InputSchema[0] = '!'
			}
			after, _ := json.Marshal(test)
			if string(before) != string(after) {
				t.Fatal("projection mutated its authority input")
			}
		})
	}
	if _, err := ProjectToolUpdates(nil, nil, "unknown", nil); err == nil {
		t.Fatal("unknown capability accepted")
	}
}
