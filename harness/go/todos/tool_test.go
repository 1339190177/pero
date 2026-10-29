package todos

import (
	"context"
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/phodal/pero/harness/go/harness"
)

type noCallAdapter struct{}

func (noCallAdapter) Generate(context.Context, harness.GenerateRequest, func(harness.StreamChunk) error) (harness.Completion, error) {
	return harness.Completion{}, nil
}

func TestTodoWriteReplacesValidatesAndClearsAtNextTurn(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	h, err := harness.Boot(ctx, harness.Options{Adapter: noCallAdapter{}})
	if err != nil {
		t.Fatal(err)
	}
	defer h.Dispose(ctx)
	contribution, err := Mount(ctx, h, Options{})
	if err != nil {
		t.Fatal(err)
	}
	defer contribution.Dispose(ctx)
	agent, err := h.Agents().Create(ctx, harness.AgentOptions{ID: "todo-test", CWD: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := agent.Session().Append("turn/start", map[string]int{"turn": 1}); err != nil {
		t.Fatal(err)
	}
	outcome, err := h.Tools().Execute(ctx, harness.ToolRequest{
		Agent: agent, CallID: "todos-1", Name: ToolName,
		Arguments: json.RawMessage(`{"todos":[{"content":" inspect ","status":"in_progress"},{"content":"test","status":"pending"}]}`),
	})
	if err != nil || outcome.Result.IsError || !strings.Contains(outcome.Result.Content[0].Text, "1 pending, 1 in progress") {
		t.Fatalf("write outcome=%+v error=%v", outcome.Result, err)
	}
	current, found, err := Current(agent.Session())
	if err != nil || !found || !reflect.DeepEqual(current, []Item{{Content: "inspect", Status: InProgress}, {Content: "test", Status: Pending}}) {
		t.Fatalf("current=%+v found=%v error=%v", current, found, err)
	}
	invalid, err := h.Tools().Execute(ctx, harness.ToolRequest{
		Agent: agent, CallID: "todos-2", Name: ToolName,
		Arguments: json.RawMessage(`{"todos":[{"content":"a","status":"in_progress"},{"content":"b","status":"in_progress"}]}`),
	})
	if err != nil || !invalid.Result.IsError || !strings.Contains(invalid.Result.Content[0].Text, "at most one") {
		t.Fatalf("invalid outcome=%+v error=%v", invalid.Result, err)
	}
	if _, err := agent.Session().Append("turn/end", map[string]any{"turn": 1, "reason": "stop"}); err != nil {
		t.Fatal(err)
	}
	if _, err := agent.Session().Append("turn/start", map[string]int{"turn": 2}); err != nil {
		t.Fatal(err)
	}
	current, found, err = Current(agent.Session())
	if err != nil || found || current != nil {
		t.Fatalf("cleared current=%+v found=%v error=%v", current, found, err)
	}
}

