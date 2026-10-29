package session

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"testing"
)

func TestSessionUserAdmissionCASAtomicAndReentrantRead(t *testing.T) {
	s := NewSession("admission", "")
	message := Message{ID: "native-round", Role: RoleUser, Source: json.RawMessage(`{"kind":"plugin"}`), Content: []ContentBlock{{Type: BlockText, Text: "reserved"}}}
	var order []string
	for _, label := range []string{"a", "b"} {
		s.Subscribe(func(event Event) {
			if len(s.Events()) != 2 || !reflect.DeepEqual(s.SurfaceSeqs(), []uint64{2}) {
				t.Error("subscriber observed partial batch")
			}
			order = append(order, event.Type+label)
		})
	}
	events, err := s.AppendUserAdmissionCAS(context.Background(), 0, []AdmissionFact{{Type: "native/round", Value: struct{}{}}}, []Message{message})
	if err != nil || len(events) != 2 {
		t.Fatalf("commit=%v %v", events, err)
	}
	if !reflect.DeepEqual(order, []string{"native/rounda", "native/roundb", "user/messagea", "user/messageb"}) {
		t.Fatal(order)
	}
	if _, err := s.AppendUserAdmissionCAS(context.Background(), 0, nil, []Message{message}); !errors.Is(err, ErrSessionChanged) {
		t.Fatal(err)
	}
	if len(s.Events()) != 2 {
		t.Fatal("CAS miss partially committed")
	}
}

func TestSessionUserAdmissionCASFailureIsAtomic(t *testing.T) {
	for _, mode := range []string{"encode", "surface-fact", "message", "canceled"} {
		t.Run(mode, func(t *testing.T) {
			s := NewSession("failure", "")
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			facts := []AdmissionFact{{Type: "native/round", Value: struct{}{}}}
			messages := []Message{{ID: "round", Role: RoleUser, Source: json.RawMessage(`{"kind":"plugin"}`), Content: []ContentBlock{{Type: BlockText, Text: "input"}}}}
			switch mode {
			case "encode":
				facts[0].Value = make(chan int)
			case "surface-fact":
				facts[0].Type = "user/message"
			case "message":
				messages[0].Source = json.RawMessage(`{`)
			case "canceled":
				cancel()
			}
			called := false
			s.Subscribe(func(Event) { called = true })
			if _, err := s.AppendUserAdmissionCAS(ctx, 0, facts, messages); err == nil {
				t.Fatal("invalid batch accepted")
			}
			if len(s.Events()) != 0 || len(s.SurfaceSeqs()) != 0 || called {
				t.Fatal("failed batch became visible")
			}
		})
	}
}
