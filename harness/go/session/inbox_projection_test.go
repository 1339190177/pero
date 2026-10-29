package session

import (
	"bytes"
	"encoding/json"
	"testing"
)

func TestInboxProjectionPreservesDurableWireAndRejectsInvalidSplices(t *testing.T) {
	d := InboxProjectionDefinition()
	initial, err := d.Init(SessionHeader{})
	if err != nil {
		t.Fatal(err)
	}
	apply := func(state any, seq uint64, payload string) (any, error) {
		return d.Apply(state, Event{Seq: seq, Type: "agent/inbox/spliced", Data: json.RawMessage(payload)})
	}
	next, err := apply(initial, 1, `{"target":"next-turn","start":0,"inserted":[{"id":"one","future":{"lossless":true}}]}`)
	if err != nil {
		t.Fatal(err)
	}
	if len((*initial.(*inboxProjectionState).Pending)[InboxNextTurn]) != 0 {
		t.Fatal("mutated previous snapshot")
	}
	unchanged, err := d.Apply(next, Event{Seq: 2, Type: "schema/change"})
	if err != nil || unchanged != next {
		t.Fatalf("irrelevant event changed identity: %v", err)
	}
	view, err := d.Wire.View(next)
	if err != nil {
		t.Fatal(err)
	}
	wire, err := d.Wire.EncodeView(view)
	if err != nil {
		t.Fatal(err)
	}
	var decoded map[string][]map[string]any
	if err = json.Unmarshal(wire, &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded["next-turn"][0]["future"] == nil || decoded["next-step"] == nil {
		t.Fatalf("lost complete wire state: %s", wire)
	}
	for _, payload := range []string{
		`{"target":"next-step","start":0,"inserted":[{"id":"one"}]}`,
		`{"target":"next-turn","start":-1,"inserted":[]}`,
		`{"target":"next-turn","start":0,"removedCount":2,"inserted":[]}`,
		`{"target":"next-turn","start":0.5,"inserted":[]}`,
		`{"target":"unknown","start":0,"inserted":[]}`,
	} {
		if _, err := apply(next, 3, payload); err == nil {
			t.Fatalf("accepted invalid splice %s", payload)
		}
	}
	cleared, err := apply(next, 4, `{"target":"next-turn","start":0,"removedCount":1,"inserted":[]}`)
	if err != nil {
		t.Fatal(err)
	}
	wire, err = d.EncodeState(cleared)
	if err != nil {
		t.Fatal(err)
	}
	restored, err := d.DecodeState(wire)
	if err != nil {
		t.Fatal(err)
	}
	if len((*restored.(*inboxProjectionState).Pending)[InboxNextTurn]) != 0 {
		t.Fatal("checkpoint retained removed message")
	}
}

func TestInboxProjectionMovesLosslessAtomicSnapshots(t *testing.T) {
	d := InboxProjectionDefinition()
	if d.StateVersion != 3 {
		t.Fatalf("move fold must invalidate old cache: %d", d.StateVersion)
	}
	state, err := d.Init(SessionHeader{})
	if err != nil {
		t.Fatal(err)
	}
	apply := func(kind, payload string) {
		t.Helper()
		state, err = d.Apply(state, Event{Seq: 7, Type: kind, Data: json.RawMessage(payload)})
		if err != nil {
			t.Fatal(err)
		}
	}
	apply("agent/inbox/spliced", `{"target":"next-turn","start":0,"inserted":[{"id":"before"},{"id":"move","future":{"nested":[1,{"keep":true}]}},{"id":"after"}]}`)
	apply("agent/inbox/spliced", `{"target":"next-step","start":0,"inserted":[{"id":"existing"}]}`)
	previous := state
	before, _ := d.EncodeState(previous)
	original := append(json.RawMessage(nil), (*previous.(*inboxProjectionState).Pending)[InboxNextTurn][1]...)
	apply("agent/inbox/moved", `{"id":"move","target":"next-step"}`)
	old, _ := d.EncodeState(previous)
	if !bytes.Equal(before, old) {
		t.Fatal("move mutated previous snapshot")
	}
	view := *state.(*inboxProjectionState).Pending
	if len(view[InboxNextTurn]) != 2 || len(view[InboxNextStep]) != 2 || !bytes.Equal(view[InboxNextStep][1], original) {
		t.Fatalf("lossy move: %+v", view)
	}
	same, err := d.Apply(state, Event{Type: "agent/inbox/moved", Data: json.RawMessage(`{"id":"move","target":"next-step"}`)})
	if err != nil || same != state {
		t.Fatalf("same-target move changed snapshot: %v", err)
	}
	before, _ = d.EncodeState(state)
	for _, payload := range []string{`{"id":"missing","target":"next-step"}`, `{"id":"move","target":"unknown"}`, `{"target":"next-turn"}`, `{"id":5,"target":"next-turn"}`, `null`} {
		if _, err := d.Apply(state, Event{Seq: 8, Type: "agent/inbox/moved", Data: json.RawMessage(payload)}); err == nil {
			t.Fatalf("accepted invalid move: %s", payload)
		}
		after, _ := d.EncodeState(state)
		if !bytes.Equal(before, after) {
			t.Fatalf("failed move mutated snapshot: %s", payload)
		}
	}
	apply("agent/inbox/moved", `{"id":"move","target":"next-turn"}`)
	view = *state.(*inboxProjectionState).Pending
	if len(view[InboxNextTurn]) != 3 || !bytes.Equal(view[InboxNextTurn][2], original) || len(view[InboxNextStep]) != 1 {
		t.Fatalf("reverse move ordering: %+v", view)
	}
}

func TestInboxProjectionOldCheckpointRebuildsDelegatedBoundary(t *testing.T) {
	parent := NewSession("cached-inbox-parent", "")
	mustAppend(t, parent, "turn/start", map[string]int{"turn": 1})
	inbox, err := NewDurableInbox(parent)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := inbox.Append(InboxNextTurn, inboxFixtureMessage("cached-ancestor", "ancestor")); err != nil {
		t.Fatal(err)
	}
	mustAppend(t, parent, "turn/end", map[string]any{"turn": 1, "reason": "stop"})
	child, err := parent.ForkCompletedTurnsForSubagent("cached-inbox-child", 1)
	if err != nil {
		t.Fatal(err)
	}
	own, err := NewDurableInbox(child)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := own.Append(InboxNextTurn, inboxFixtureMessage("cached-own", "own")); err != nil {
		t.Fatal(err)
	}
	d := InboxProjectionDefinition()
	registry := &SessionProjectionRegistry{registrations: map[string]*projectionRegistration{"inbox": {definition: d}}}
	legacy := ProjectionCheckpoint{"inbox": {Version: 2, Seq: child.Header().SeedLength, Value: json.RawMessage(`{"next-turn":[{"id":"cached-ancestor"}],"next-step":[]}`)}}
	if floor, registered := registry.RestoreFloor(legacy); !registered || floor != 1 {
		t.Fatalf("legacy checkpoint must force full replay: floor=%d registered=%t", floor, registered)
	}
	if _, err := registry.Restore(legacy, child.Events()[child.Header().SeedLength:], child.Header().SeedLength+1, child.Header()); err == nil {
		t.Fatal("version mismatch accepted a truncated suffix replay")
	}
	rebuilt, err := registry.Restore(legacy, child.Events(), 1, child.Header())
	if err != nil {
		t.Fatal(err)
	}
	check := func(result ProjectionRestoreResult) {
		t.Helper()
		var view InboxProjectionState
		if err := json.Unmarshal(result.Snapshot.Values["inbox"], &view); err != nil {
			t.Fatal(err)
		}
		if len(view[InboxNextTurn]) != 1 || !bytes.Contains(view[InboxNextTurn][0], []byte(`"cached-own"`)) || result.Checkpoint["inbox"].Version != 3 {
			t.Fatalf("rebuilt projection revived ancestor: %+v", result)
		}
	}
	check(rebuilt)
	current, err := registry.Restore(rebuilt.Checkpoint, nil, uint64(len(child.Events()))+1, child.Header())
	if err != nil {
		t.Fatal(err)
	}
	check(current)
}
