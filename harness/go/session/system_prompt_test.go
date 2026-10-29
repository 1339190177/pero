package session

import (
	"reflect"
	"testing"

	"github.com/phodal/pero/harness/go/model"
)

func TestSystemPromptCanonicalProjectionAndLegacyReplay(t *testing.T) {
	for _, legacy := range []bool{false, true} {
		t.Run(map[bool]string{false: "new", true: "legacy"}[legacy], func(t *testing.T) {
			s := NewSession("prompt", t.TempDir())
			if legacy {
				mustAppend(t, s, "user/message", Message{Role: model.RoleUser, Content: []ContentBlock{{Type: model.BlockText, Text: "old"}}})
				mustAppend(t, s, "request/header", RequestHeaderEvent{System: "old prompt"})
			}
			if err := s.CommitSystemPrompt(1, 1, "", false, false); err != nil {
				t.Fatal(err)
			}
			head := s.SurfaceSeqs()[len(s.SurfaceSeqs())-1]
			before := len(s.Events())
			if err := s.CommitSystemPrompt(1, 1, "", false, false); err != nil || len(s.Events()) != before {
				t.Fatalf("empty prompt repeated: %v", err)
			}
			if err := s.CommitSystemPrompt(1, 2, "alpha", false, false); err != nil {
				t.Fatal(err)
			}
			first := s.SurfaceSeqs()[len(s.SurfaceSeqs())-1]
			if first == head {
				t.Fatal("prompt was mutated in place")
			}
			if err := s.CommitSystemPrompt(1, 3, "beta", true, false); err != nil {
				t.Fatal(err)
			}
			if err := s.CommitSystemPrompt(1, 4, "gamma", true, true); err != nil {
				t.Fatal(err)
			}
			nodes, err := systemPromptNodes(s.Events(), s.SurfaceSeqs())
			if err != nil || len(nodes) != 2 || nodes[0].text != "gamma" || nodes[1].text != "" {
				t.Fatalf("consolidation: %+v %v", nodes, err)
			}
			_, err = s.AppendReplacement("user/message", Message{Role: model.RoleUser}, SurfaceRange{Start: nodes[0].seq, End: nodes[0].seq}, []uint64{nodes[0].seq})
			if err == nil {
				t.Fatal("system head erased")
			}
			restored, err := RestoreSession(s.Header(), s.Events())
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(s.SurfaceSeqs(), restored.SurfaceSeqs()) {
				t.Fatal("restore changed prompt surface")
			}
			if err = restored.CommitSystemPrompt(2, 1, "", false, false); err != nil {
				t.Fatal(err)
			}
			messages, err := restored.DeriveMessages()
			if err != nil {
				t.Fatal(err)
			}
			for _, message := range messages {
				if message.Role == model.RoleSystem {
					t.Fatal("empty prompt reached provider history")
				}
			}
		})
	}
}

func TestRuntimeContextPreservesSystemAndRetainedSnapshot(t *testing.T) {
	s := NewSession("runtime", t.TempDir())
	if err := s.CommitRuntimeContext(model.RuntimeContextSnapshot{Text: ""}); err != nil || len(s.Events()) != 0 {
		t.Fatalf("empty initial context: %v", err)
	}
	if err := s.CommitSystemPrompt(1, 1, "saved system", false, false); err != nil {
		t.Fatal(err)
	}
	if err := s.CommitRuntimeContext(model.RuntimeContextSnapshot{Text: "current URL one"}); err != nil {
		t.Fatal(err)
	}
	before := len(s.Events())
	restored, err := RestoreSession(s.Header(), s.Events())
	if err != nil {
		t.Fatal(err)
	}
	if err := restored.CommitRuntimeContext(model.RuntimeContextSnapshot{Text: "current URL one"}); err != nil || len(restored.Events()) != before {
		t.Fatalf("restore duplicated snapshot: %v", err)
	}
	if err := restored.CommitRuntimeContext(model.RuntimeContextSnapshot{Text: "current URL two"}); err != nil {
		t.Fatal(err)
	}
	if err := restored.CommitRuntimeContext(model.RuntimeContextSnapshot{Text: ""}); err != nil {
		t.Fatal(err)
	}
	if got, err := restored.EffectiveSystemPrompt(); err != nil || got != "saved system" {
		t.Fatalf("system changed: %q %v", got, err)
	}
	before = len(restored.Events())
	if err := restored.CommitRuntimeContext(model.RuntimeContextSnapshot{Text: ""}); err != nil || len(restored.Events()) != before {
		t.Fatalf("clear duplicated: %v", err)
	}
	last := restored.Events()[before-1]
	if _, err := restored.AppendReplacement("user/message", Message{Role: model.RoleUser, Content: []ContentBlock{{Type: model.BlockText, Text: "compacted"}}}, SurfaceRange{Start: last.Seq, End: last.Seq}, []uint64{last.Seq}); err != nil {
		t.Fatal(err)
	}
	if err := restored.CommitRuntimeContext(model.RuntimeContextSnapshot{Text: ""}); err != nil || len(restored.Events()) != before+2 {
		t.Fatalf("removed context was not re-emitted: %v", err)
	}
}
