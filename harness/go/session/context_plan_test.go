package session

import (
	"encoding/json"
	"reflect"
	"testing"
)

func planReceipt(plan ContextPlan) map[string]any {
	return map[string]any{"kind": plan.Kind, "digest": plan.Digest(), "memoryTokens": plan.MemoryTokens, "memoryBudget": plan.MemoryBudget}
}

func TestContextPlanStableDigestAndReplay(t *testing.T) {
	s := NewSession("plan", "")
	mustAppend(t, s, "user/message", Message{Role: RoleUser, Content: []ContentBlock{{Type: BlockText, Text: "hello"}}})
	plan, err := s.ContextPlan()
	if err != nil {
		t.Fatal(err)
	}
	const want = "sha256:74c432f8ed335b7e8afc8d7b480bc14f55e419a39d28e4e36d1e6b63aefee2f0"
	if got := plan.Digest(); got != want {
		t.Fatalf("digest = %s; plan=%+v", got, plan)
	}
	receipt := mustAppend(t, s, "request/context-plan", planReceipt(plan))
	restored, err := RestoreSession(s.Header(), s.Events())
	if err != nil {
		t.Fatal(err)
	}
	input, seq, err := restored.LatestInputPlan()
	if err != nil || seq != receipt.Seq || input.Digest() != plan.Digest() {
		t.Fatalf("input=%+v seq=%d err=%v", input, seq, err)
	}
	before := s.Events()
	for _, key := range []string{"digest", "memoryTokens", "memoryBudget"} {
		bad := planReceipt(plan)
		if key == "digest" {
			bad[key] = "sha256:wrong"
		} else {
			bad[key] = -1
		}
		if _, err := s.Append("request/context-plan", bad); err == nil {
			t.Fatalf("accepted bad %s", key)
		}
	}
	if !reflect.DeepEqual(before, s.Events()) {
		t.Fatal("invalid receipt changed log")
	}
}

func TestContextPlanRejectsPointReceiptsWithoutReinterpretation(t *testing.T) {
	for _, kind := range []string{"point.context.plan.v1", "point.context.plan.v2", "point.context.plan.v3", "point.context.plan.v4"} {
		t.Run(kind, func(t *testing.T) {
			s := NewSession("old-receipt", "")
			plan, err := s.ContextPlan()
			if err != nil {
				t.Fatal(err)
			}
			payload := planReceipt(plan)
			payload["kind"] = kind
			if _, err := s.Append("request/context-plan", payload); err == nil {
				t.Fatal("accepted foreign policy receipt")
			}
			raw, err := json.Marshal(payload)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := RestoreSession(s.Header(), []Event{{Seq: 1, Type: "request/context-plan", Data: raw}}); err == nil {
				t.Fatal("restored foreign policy receipt")
			}
		})
	}
}

func TestContextPlanDoesNotInterpretProductPolicyMetadata(t *testing.T) {
	s := NewSession("opaque-policy", "")
	for _, text := range []string{"first", "second"} {
		mustAppend(t, s, "user/message", Message{Role: RoleUser, Content: []ContentBlock{{Type: BlockText, Text: text}}, Source: json.RawMessage(`{"plugin":"@point/channels-memory","view":{"turn":2},"sourceRules":[{"eventDigest":"any","eventSeq":1,"throughSeq":2}]}`)})
	}
	plan, err := s.ContextPlan()
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Messages()) != 2 || len(plan.Omitted) != 0 || plan.MemoryBudget != 0 || plan.MemoryTokens != 0 {
		t.Fatalf("product metadata changed plan: %+v", plan)
	}
	if plan.Messages()[0].Content[0].Text != "first" || plan.Messages()[1].Content[0].Text != "second" {
		t.Fatal("surface order changed")
	}
	if string(plan.Messages()[0].Source) != string(plan.Messages()[1].Source) {
		t.Fatal("opaque source changed")
	}
}
