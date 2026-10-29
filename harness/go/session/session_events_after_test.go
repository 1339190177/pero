package session

import (
	"bytes"
	"testing"
)

func TestEventsAfterReturnsTheIsolatedSuffix(t *testing.T) {
	session := NewSession("suffix", "")
	mustAppend(t, session, "fact", map[string]any{"value": 1})
	mustAppend(t, session, "fact", map[string]any{"value": 2})
	all := session.Events()
	if len(all) < 2 {
		t.Fatalf("fixture too short: %d", len(all))
	}
	cut := uint64(len(all) - 1)
	suffix, ok := session.EventsAfter(cut)
	if !ok || len(suffix) != 1 || suffix[0].Seq != cut+1 {
		t.Fatalf("suffix after %d = %d events, ok=%v", cut, len(suffix), ok)
	}
	if empty, ok := session.EventsAfter(uint64(len(all))); !ok || len(empty) != 0 {
		t.Fatalf("the exact tail is an empty suffix, got %d ok=%v", len(empty), ok)
	}
	if _, ok := session.EventsAfter(uint64(len(all)) + 1); ok {
		t.Fatal("a cut beyond the log must be refused, not answered with an empty suffix")
	}
	want := bytes.Clone(all[len(all)-1].Data)
	for index := range suffix[0].Data {
		suffix[0].Data[index] = 'x'
	}
	if got := session.Events()[len(all)-1].Data; !bytes.Equal(got, want) {
		t.Fatal("mutating a returned suffix changed the durable log")
	}
}
