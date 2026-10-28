package session

import (
	"bytes"
	"encoding/json"
	"os"
	"reflect"
	"testing"
	"time"
)

func opaqueRecord(t *testing.T, seq uint64) Event {
	t.Helper()
	var e Event
	if err := json.Unmarshal([]byte(`{"seq":1,"time":"2026-10-15T00:00:00Z","type":"plugin:future/example","data":{"turn":999,"body":{"goalId":"not-an-admission"}},"ignorable":true,"surfaceOp":{"future":[null,{"op":"replace","start":1,"end":999}]},"sourceEventSeqs":{"future":[1,"2",null]}}`), &e); err != nil {
		t.Fatal(err)
	}
	e.Seq = seq
	return e
}

func TestOpaqueSessionPersistRestoreForkAndIsolation(t *testing.T) {
	original := NewSession("opaque-source", t.TempDir())
	for _, v := range []struct {
		kind string
		data any
	}{{"turn/start", map[string]int{"turn": 1}}, {"user/message", textMessage("user", "keep surface")}, {"turn/end", map[string]any{"turn": 1, "reason": "stop"}}} {
		if _, err := original.Append(v.kind, v.data); err != nil {
			t.Fatal(err)
		}
	}
	events := append(original.Events(), opaqueRecord(t, 4))
	restored, err := RestoreSession(original.Header(), events)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(restored.SurfaceSeqs(), original.SurfaceSeqs()) {
		t.Fatal("opaque replacement affected surface")
	}
	if turn, err := restored.LastTurn(); err != nil || turn != 1 {
		t.Fatalf("opaque turn interpreted: %d %v", turn, err)
	}
	events[3].OpaqueSourceEventSeqs[0] = '!'
	if !json.Valid(restored.Events()[3].OpaqueSourceEventSeqs) {
		t.Fatal("restore retained caller bytes")
	}
	p, err := NewJSONLSessionPersistence(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	close, err := p.Attach(t.Context(), restored)
	if err != nil {
		t.Fatal(err)
	}
	if err = close(); err != nil {
		t.Fatal(err)
	}
	loaded, err := p.Load(t.Context(), restored.Header().ID)
	if err != nil {
		t.Fatal(err)
	}
	child, err := loaded.ForkCompleted("opaque-child", nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range []*Session{restored, loaded, child} {
		got := s.Events()[3]
		if !got.IsOpaque() || got.SourceEventSeqs != nil || string(got.OpaqueSourceEventSeqs) != `{"future":[1,"2",null]}` {
			t.Fatalf("roundtrip=%#v", got)
		}
		got.OpaqueSourceEventSeqs[0] = '!'
		got.SurfaceOp[0] = '!'
		if !json.Valid(s.Events()[3].OpaqueSourceEventSeqs) || !json.Valid(s.Events()[3].SurfaceOp) {
			t.Fatal("snapshot alias")
		}
	}
	// Subscriber snapshots cannot mutate stored or later subscriber metadata.
	event := loaded.Events()[3]
	loaded.notifySubscriber(func(got Event) { got.OpaqueSourceEventSeqs[0] = '!'; got.SurfaceOp[0] = '!' }, event)
	if !json.Valid(event.OpaqueSourceEventSeqs) || !json.Valid(event.SurfaceOp) {
		t.Fatal("subscriber aliased event")
	}
}

func TestKnownIgnorableEventsRetainValidation(t *testing.T) {
	header := NewSession("known", "").Header()
	for _, kind := range []string{"user/message", "tool/result", "request/header", "goal/round", "assistant/chunk"} {
		e := Event{Seq: 1, Time: time.Now(), Type: kind, Data: json.RawMessage(`{}`), Ignorable: true, SurfaceOp: json.RawMessage(`{"future":"opaque"}`)}
		if _, err := RestoreSession(header, []Event{e}); err == nil {
			t.Fatalf("known %s bypassed validation", kind)
		}
	}
	e := opaqueRecord(t, 1)
	e.Ignorable = false
	if _, err := RestoreSession(header, []Event{e}); err == nil {
		t.Fatal("unmarked raw metadata admitted")
	}
}

// The actual persisted-log decoder retains ordinary event envelope behavior.
// Semantic restoration remains authoritative after permissive field decoding.
func TestJSONLPersistencePreservesKnownEventEnvelopeCompatibility(t *testing.T) {
	p, err := NewJSONLSessionPersistence(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	original := NewSession("legacy-known-envelope", "")
	if _, err = original.Append("user/message", textMessage("user", "known message")); err != nil {
		t.Fatal(err)
	}
	detach, err := p.Attach(t.Context(), original)
	if err != nil {
		t.Fatal(err)
	}
	if err = detach(); err != nil {
		t.Fatal(err)
	}
	if _, err = p.Load(t.Context(), original.Header().ID); err != nil {
		t.Fatal(err)
	}
	path := p.path(original.Header().ID)
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	lines := bytes.Split(bytes.TrimSpace(raw), []byte("\n"))
	if len(lines) != 2 {
		t.Fatalf("unexpected persisted lines: %d", len(lines))
	}
	event, err := json.Marshal(original.Events()[0])
	if err != nil {
		t.Fatal(err)
	}
	event = append(event[:len(event)-1], []byte(`,"previouslyIgnored":true,"seq":1,"Type":"user/message","ignorable":false}`)...)
	stored := append(append(append(append([]byte{}, lines[0]...), []byte("\n{\"event\":")...), event...), []byte("}\n")...)
	if err = os.WriteFile(path, stored, 0600); err != nil {
		t.Fatal(err)
	}
	restored, err := p.Load(t.Context(), original.Header().ID)
	if err != nil {
		t.Fatalf("ordinary legacy envelope rejected: %v", err)
	}
	if !reflect.DeepEqual(restored.Events(), original.Events()) || !reflect.DeepEqual(restored.SurfaceSeqs(), original.SurfaceSeqs()) {
		t.Fatal("legacy envelope changed restored history or surface")
	}
	after, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(after, stored) {
		t.Fatalf("compatible load rewrote stored history: %v", err)
	}
}
