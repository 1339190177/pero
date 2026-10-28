package model

import (
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestOpaqueEventEnvelopePresenceAndClone(t *testing.T) {
	for _, raw := range []string{"", `null`, `[]`, `{}`, `"opaque"`, `17`, `true`, `{"future":[1,"2",null],"text":"\ud800"}`} {
		t.Run(raw, func(t *testing.T) {
			metadata := ""
			if raw != "" {
				metadata = `,"surfaceOp":` + raw + `,"sourceEventSeqs":` + raw
			}
			input := `{"seq":1,"time":"2026-10-15T00:00:00Z","type":"plugin:future/example","data":{"turn":999},"ignorable":true` + metadata + `}`
			var event Event
			if err := json.Unmarshal([]byte(input), &event); err != nil {
				t.Fatal(err)
			}
			if !event.IsOpaque() || event.SourceEventSeqs != nil || string(event.OpaqueSourceEventSeqs) != raw || string(event.SurfaceOp) != raw {
				t.Fatalf("lost opaque metadata: %#v", event)
			}
			out, err := json.Marshal(event)
			if err != nil {
				t.Fatal(err)
			}
			var fields map[string]json.RawMessage
			if err = json.Unmarshal(out, &fields); err != nil {
				t.Fatal(err)
			}
			if string(fields["sourceEventSeqs"]) != raw || string(fields["surfaceOp"]) != raw {
				t.Fatalf("presence changed: %s", out)
			}
			clone := event.Clone()
			if len(clone.OpaqueSourceEventSeqs) > 0 {
				clone.OpaqueSourceEventSeqs[0] = '!'
				clone.SurfaceOp[0] = '!'
			}
			clone.Data[0] = '!'
			if string(event.OpaqueSourceEventSeqs) != raw || !json.Valid(event.Data) {
				t.Fatal("clone aliased original")
			}
		})
	}
}

func TestOpaqueEventEnvelopeRejectsAmbiguityAndInvalidMarker(t *testing.T) {
	base := `{"seq":1,"time":"2026-10-15T00:00:00Z","type":"plugin:future/example","data":null`
	for _, suffix := range []string{`,"extra":1,"ignorable":true}`, `,"ignorable":true,"ignorable":true}`, `,"ignorable":true,"ignorable":false}`, `,"sourceEventSeqs":{}}`} {
		var e Event
		if err := json.Unmarshal([]byte(base+suffix), &e); err == nil {
			t.Fatalf("accepted %s", suffix)
		}
	}
	for _, kind := range []string{"user/message", "tool/result", "request/header", "goal/round", "assistant/chunk"} {
		var e Event
		raw := strings.Replace(base, "plugin:future/example", kind, 1) + `,"ignorable":true,"sourceEventSeqs":{}}`
		if err := json.Unmarshal([]byte(raw), &e); err == nil {
			t.Fatalf("known type %s gained opaque authority", kind)
		}
	}
	e := Event{Seq: 1, Time: time.Now(), Type: "plugin:future/example", Data: json.RawMessage(`null`), Ignorable: true, OpaqueSourceEventSeqs: json.RawMessage(`[]`), SourceEventSeqs: []uint64{}}
	if _, err := json.Marshal(e); err == nil {
		t.Fatal("typed/raw conflict accepted")
	}
	e.SourceEventSeqs = nil
	e.Ignorable = false
	if _, err := json.Marshal(e); err == nil {
		t.Fatal("unmarked opaque metadata accepted")
	}
}

func TestKnownEventJSONPreservesTypedProvenance(t *testing.T) {
	e := Event{Seq: 2, Time: time.Unix(1, 0).UTC(), Type: "user/message", Data: json.RawMessage(`{}`), SourceEventSeqs: []uint64{1}, Ignorable: true}
	raw, err := json.Marshal(e)
	if err != nil {
		t.Fatal(err)
	}
	var restored Event
	if err = json.Unmarshal(raw, &restored); err != nil {
		t.Fatal(err)
	}
	if restored.IsOpaque() || !reflect.DeepEqual(restored.SourceEventSeqs, e.SourceEventSeqs) || restored.OpaqueSourceEventSeqs != nil {
		t.Fatalf("known event changed: %#v", restored)
	}
}

// This is the exact pre-opaque six-field decoder, deliberately without custom
// methods. It is the oracle for acceptance and receiver mutation, rather than a
// manual approximation of encoding/json's case, duplicate and null behavior.
type legacySessionEvent struct {
	Seq             uint64          `json:"seq"`
	Time            time.Time       `json:"time"`
	Type            string          `json:"type"`
	Data            json.RawMessage `json:"data"`
	SurfaceOp       json.RawMessage `json:"surfaceOp,omitempty"`
	SourceEventSeqs []uint64        `json:"sourceEventSeqs,omitempty"`
}

func legacyEventFields(e Event) legacySessionEvent {
	return legacySessionEvent{e.Seq, e.Time, e.Type, e.Data, e.SurfaceOp, e.SourceEventSeqs}
}

func TestKnownEventEnvelopePreservesLegacyDecoder(t *testing.T) {
	inputs := []struct{ name, raw string }{
		{"empty", `{}`},
		{"null", `null`},
		{"partial", `{"seq":9,"data":{"updated":true}}`},
		{"null_fields", `{"seq":null,"time":null,"type":null,"data":null,"surfaceOp":null,"sourceEventSeqs":null}`},
		{"ignored_extra", `{"extra":{"nested":true}}`},
		{"known_extra", `{"type":"goal/round","extra":true}`},
		{"case_aliases", `{"SEQ":8,"Type":"goal/round","DATA":null,"SOURCEEVENTSEQS":[2,3]}`},
		{"unicode_alias", `{"ſeq":8,"type":"goal/round"}`},
		{"duplicate_known", `{"type":"goal/round","type":"user/message","seq":1,"seq":2}`},
		{"alias_known_last", `{"type":"plugin:future/example","Type":"goal/round","ignorable":true}`},
		{"alias_known_first", `{"Type":"goal/round","type":"plugin:future/example"}`},
		{"duplicate_array", `{"sourceEventSeqs":[1],"SOURCEEVENTSEQS":[2,3]}`},
		{"empty_array", `{"sourceEventSeqs":[]}`},
		{"known_true_marker", `{"type":"goal/round","ignorable":true}`},
		{"known_false_marker", `{"type":"goal/round","ignorable":false}`},
		{"unknown_false_marker", `{"type":"plugin:future/example","ignorable":false,"extra":1}`},
		{"unknown_null_marker", `{"type":"plugin:future/example","ignorable":null}`},
		{"unknown_string_marker", `{"type":"plugin:future/example","ignorable":"true"}`},
		{"unknown_object_marker", `{"type":"plugin:future/example","ignorable":{}}`},
		{"uppercase_marker", `{"type":"plugin:future/example","IGNORABLE":true}`},
		{"type_probe_error", `{"type":3,"seq":8,"data":{"after":true},"ignorable":true}`},
		{"sequence_type_error", `{"type":"goal/round","seq":"bad","data":{"after":true}}`},
		{"typed_provenance_error", `{"type":"goal/round","sourceEventSeqs":{},"ignorable":true}`},
		{"time_error", `{"time":"bad","seq":8}`},
		{"negative_sequence", `{"seq":-1,"data":null}`},
		{"surrogate_type", `{"type":"\ud800"}`},
		{"invalid_utf8_type", "{\"type\":\"" + string([]byte{0xff}) + "\"}"},
		{"invalid_utf8_raw", "{\"data\":\"" + string([]byte{0xff}) + "\"}"},
	}
	seeds := []struct {
		name  string
		event Event
	}{
		{"zero", Event{}},
		{"known", Event{Seq: 17, Time: time.Unix(1, 0).UTC(), Type: "goal/round", Data: json.RawMessage(`{"retained":true}`), SurfaceOp: json.RawMessage(`{"retained":"surface"}`), SourceEventSeqs: []uint64{2, 4}}},
		{"opaque", Event{Seq: 17, Time: time.Unix(1, 0).UTC(), Type: "plugin:prior/example", Data: json.RawMessage(`{"retained":true}`), Ignorable: true, OpaqueSourceEventSeqs: json.RawMessage(`{"prior":true}`)}},
	}
	for _, seed := range seeds {
		for _, input := range inputs {
			t.Run(seed.name+"/"+input.name, func(t *testing.T) {
				got := seed.event.Clone()
				want := legacyEventFields(seed.event.Clone())
				wantErr := json.Unmarshal([]byte(input.raw), &want)
				gotErr := json.Unmarshal([]byte(input.raw), &got)
				if (gotErr == nil) != (wantErr == nil) || reflect.TypeOf(gotErr) != reflect.TypeOf(wantErr) {
					t.Fatalf("decoder error changed: got %v; legacy %v", gotErr, wantErr)
				}
				var gotType, wantType *json.UnmarshalTypeError
				if errors.As(gotErr, &gotType) && errors.As(wantErr, &wantType) {
					// The method-free oracle and alias have different Go type names;
					// the JSON field, value, destination type and offset stay exact.
					if gotType.Field != wantType.Field || gotType.Value != wantType.Value || gotType.Type != wantType.Type || gotType.Offset != wantType.Offset {
						t.Fatalf("type error changed: got %#v; legacy %#v", gotType, wantType)
					}
				}
				if !reflect.DeepEqual(legacyEventFields(got), want) {
					t.Fatalf("receiver changed: got %#v; legacy %#v", legacyEventFields(got), want)
				}
				if got.Ignorable || got.OpaqueSourceEventSeqs != nil {
					t.Fatalf("legacy decode retained opaque state: %#v", got)
				}
			})
		}
	}
}

func TestOpaqueEventOptInCannotDowngradeOrReuseLegacyState(t *testing.T) {
	base := `{"seq":1,"time":"2026-10-15T00:00:00Z","type":"plugin:future/example","data":null`
	for _, raw := range []string{
		base + `,"ignorable":true,"ignorable":false}`,
		base + `,"ignorable":false,"ignorable":true}`,
		`{"seq":1,"time":"2026-10-15T00:00:00Z","Type":"goal/round","type":"plugin:future/example","data":null,"ignorable":true}`,
		`{"type":"plugin:future/example","ignorable":true}`,
		`{"ignorable":true}`,
	} {
		t.Run(raw, func(t *testing.T) {
			event := Event{Seq: 17, Time: time.Unix(1, 0).UTC(), Type: "plugin:prior/example", Data: json.RawMessage(`null`), Ignorable: true, OpaqueSourceEventSeqs: json.RawMessage(`{"prior":true}`)}
			before := event.Clone()
			if err := json.Unmarshal([]byte(raw), &event); err == nil {
				t.Fatalf("opaque attempt admitted legacy envelope: %s", raw)
			}
			if !reflect.DeepEqual(event, before) {
				t.Fatal("rejected strict opaque attempt mutated receiver")
			}
		})
	}
}

func TestEventEnvelopeWhitespaceNullAndRequiredPresence(t *testing.T) {
	// Lock down Decoder.RawMessage's current literal behavior independently of
	// the envelope guard. The explicit null checks still trim defensively.
	var raw json.RawMessage
	decoder := json.NewDecoder(strings.NewReader(" \n null \t "))
	if err := decoder.Decode(&raw); err != nil || string(raw) != "null" {
		t.Fatalf("RawMessage literal changed: %q %v", raw, err)
	}
	for _, kind := range []string{"plugin:future/example"} {
		t.Run(kind, func(t *testing.T) {
			fields := []string{`"seq":1`, `"time":"0001-01-01T00:00:00Z"`, `"type":"` + kind + `"`, `"data":null`}
			envelope := func(values []string) string {
				return "{" + strings.Join(append(append([]string{}, values...), `"ignorable":true`), ",") + "}"
			}
			for index, field := range []string{"seq", "time", "type", "data"} {
				missing := append(append([]string{}, fields[:index]...), fields[index+1:]...)
				var event Event
				if err := json.Unmarshal([]byte(envelope(missing)), &event); err == nil {
					t.Fatalf("missing %s admitted", field)
				}
				nullable := append([]string{}, fields...)
				nullable[index] = `"` + field + `":` + " \n null \t "
				err := json.Unmarshal([]byte(envelope(nullable)), &event)
				if field == "data" {
					if err != nil || strings.TrimSpace(string(event.Data)) != "null" {
						t.Fatalf("present null data lost: %#v %v", event, err)
					}
				} else if err == nil {
					t.Fatalf("whitespace null %s admitted", field)
				}
			}
		})
	}
}
