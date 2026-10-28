package session

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"testing"
)

func TestJSONLInboxResumeCancellationAndIdentity(t *testing.T) {
	p, err := NewJSONLSessionPersistence(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	s := NewSession("pending", "")
	detach, err := p.Attach(t.Context(), s)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = detach() })
	inbox, err := NewDurableInbox(s)
	if err != nil {
		t.Fatal(err)
	}
	message := Message{ID: "input-1", Role: RoleUser, Source: json.RawMessage(`{"kind":"user"}`), Content: []ContentBlock{{Type: BlockText, Text: "run after resume"}}}
	if accepted, err := inbox.Append(InboxNextTurn, message); err != nil || !accepted {
		t.Fatalf("accepted=%v err=%v", accepted, err)
	}
	if err := s.Flush(t.Context()); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := p.Load(ctx, s.Header().ID); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled load: %v", err)
	}
	if err := detach(); err != nil {
		t.Fatal(err)
	}
	loaded, err := p.Load(t.Context(), s.Header().ID)
	if err != nil {
		t.Fatal(err)
	}
	resumed, err := NewDurableInbox(loaded)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(resumed.Pending(InboxNextTurn), []Message{message}) {
		t.Fatal("pending input lost")
	}
	if accepted, err := resumed.Append(InboxNextTurn, message); err != nil || accepted {
		t.Fatalf("duplicate accepted=%v err=%v", accepted, err)
	}
	claimed, err := resumed.Claim(InboxNextTurn)
	if err != nil || len(claimed) != 1 {
		t.Fatalf("claim=%v err=%v", claimed, err)
	}
	mutated := message
	mutated.Content = []ContentBlock{{Type: BlockText, Text: "different"}}
	mustAppend(t, loaded, "user/message", mutated)
	if _, err := NewDurableInbox(loaded); err == nil {
		t.Fatal("accepted rewritten claimed input")
	}
}
