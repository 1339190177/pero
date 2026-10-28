package session

import (
	"errors"
	"testing"

	harnessmodel "github.com/phodal/pero/harness/go/model"
)

func TestSubagentExactSeedPreservesChosenCompletedPrefix(t *testing.T) {
	source := NewSession("exact-prefix-parent", "/cwd")
	appendTurn(t, source, 1, "first")
	first := source.Events()[len(source.Events())-1].Seq
	appendTurn(t, source, 2, "second")
	child, err := source.ForkCompletedTurnsForSubagentAt("exact-prefix-child", 2, first)
	if err != nil {
		t.Fatal(err)
	}
	if child.Header().SeedLength != first || child.Header().ParentSession != source.Header().ID || child.Header().Origin != "subagent" || child.Header().DelegationDepth != 2 {
		t.Fatalf("header=%+v", child.Header())
	}
	if len(child.Events()) != int(first)+1 {
		t.Fatalf("events=%+v", child.Events())
	}
	for _, seq := range []uint64{0, 1, first - 1, uint64(len(source.Events())) + 1, 1 << 53} {
		if _, err := source.ForkCompletedTurnsForSubagentAt("invalid-child", 1, seq); err == nil {
			t.Fatalf("accepted invalid seed boundary %d", seq)
		}
	}
}

func TestSubagentExactSeedRejectsPendingToolAndOpenTurn(t *testing.T) {
	source := NewSession("pending-prefix-parent", "/cwd")
	mustAppend(t, source, "turn/start", map[string]int{"turn": 1})
	mustAppend(t, source, "assistant/message", map[string]any{"message": Message{Role: harnessmodel.RoleAssistant, Content: []ContentBlock{{Type: BlockToolCall, ToolCallID: "unanswered", Name: "read", Arguments: []byte(`{}`)}}}})
	pendingSeq := source.Events()[len(source.Events())-1].Seq
	if _, err := source.ForkCompletedTurnsForSubagentAt("open-child", 1, pendingSeq); !errors.Is(err, ErrInvalidSessionForkAnchor) {
		t.Fatalf("open prefix=%v", err)
	}
	mustAppend(t, source, "turn/end", map[string]any{"turn": 1, "reason": "stop"})
	closedSeq := source.Events()[len(source.Events())-1].Seq
	if _, err := source.ForkCompletedTurnsForSubagentAt("pending-child", 1, closedSeq); err == nil {
		t.Fatal("accepted completed bracket with an unanswered tool call")
	}
}
