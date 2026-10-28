package model

import (
	"encoding/json"
	"os"
	"reflect"
	"testing"
)

func TestSessionHeaderDoesNotOwnProjectState(t *testing.T) {
	if _, exists := reflect.TypeOf(SessionHeader{}).FieldByName("ProjectBinding"); exists {
		t.Fatal("session header exposes Point project ownership")
	}
	var header SessionHeader
	if err := json.Unmarshal([]byte(`{"version":1,"id":"session","projectBinding":{"projectId":"private","taskId":"task","runId":"run","contextSnapshotId":"context"}}`), &header); err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(header)
	if err != nil {
		t.Fatal(err)
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(encoded, &fields); err != nil {
		t.Fatal(err)
	}
	if _, exists := fields["projectBinding"]; exists {
		t.Fatal("project binding leaked into persistence wire")
	}
}

func TestProductEventsRemainOpaqueExtensions(t *testing.T) {
	data, err := os.ReadFile("SOURCE.json")
	if err != nil {
		t.Fatal(err)
	}
	var source struct {
		Removed []string `json:"removed_native_product_event_types"`
	}
	if err := json.Unmarshal(data, &source); err != nil {
		t.Fatal(err)
	}
	if len(source.Removed) != 32 {
		t.Fatalf("incomplete product boundary inventory: %d", len(source.Removed))
	}
	for _, kind := range source.Removed {
		t.Run(kind, func(t *testing.T) {
			if IsKnownSessionEventType(kind) {
				t.Fatal("product event acquired core authority")
			}
			input := `{"seq":1,"time":"2026-10-15T00:00:00Z","type":"` + kind + `","data":{"revision":1},"ignorable":true,"sourceEventSeqs":{"opaque":true}}`
			var event Event
			if err := json.Unmarshal([]byte(input), &event); err != nil {
				t.Fatal(err)
			}
			if !event.IsOpaque() || event.SourceEventSeqs != nil {
				t.Fatal("product extension became typed")
			}
			encoded, err := json.Marshal(event)
			if err != nil {
				t.Fatal(err)
			}
			var restored Event
			if err := json.Unmarshal(encoded, &restored); err != nil {
				t.Fatal(err)
			}
			if !restored.IsOpaque() || string(restored.OpaqueSourceEventSeqs) != `{"opaque":true}` || string(restored.Data) != `{"revision":1}` {
				t.Fatal("opaque extension did not round-trip")
			}
		})
	}
}
