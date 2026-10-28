package todos

import (
	"context"
	"encoding/json"
	"reflect"
	"slices"
	"testing"

	"github.com/phodal/pero/harness/go/harness"
	point "github.com/phodal/pero/harness/go/runtime"
)

func TestTodoCanonicalOutputPreservesPresentationAndState(t *testing.T) {
	h, err := harness.Boot(t.Context(), harness.Options{Adapter: noCallAdapter{}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = h.Dispose(context.Background()); _ = h.Drain(context.Background()) })
	if _, err := Mount(t.Context(), h, Options{AllowParallelInProgress: true}); err != nil {
		t.Fatal(err)
	}
	agent, err := h.Agents().Create(t.Context(), harness.AgentOptions{ID: "todo-canonical"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := agent.Session().Append("turn/start", map[string]int{"turn": 1}); err != nil {
		t.Fatal(err)
	}
	definition, ok := h.Tools().Resolve(agent.Scope(), ToolName)
	if !ok || len(definition.Schema.OutputSchema) == 0 || definition.RenderOutput == nil {
		t.Fatal("canonical output contract missing")
	}
	for _, test := range []struct{ name, input, value, text string }{
		{"list", `{"todos":[{"content":" inspect ","status":"in_progress"},{"content":"test","status":"pending"},{"content":"done","status":"completed"}]}`, `{"todos":[{"content":"inspect","status":"in_progress"},{"content":"test","status":"pending"},{"content":"done","status":"completed"}],"counts":{"pending":1,"inProgress":1,"completed":1}}`, "Updated todo list: 1 pending, 1 in progress, 1 completed."},
		{"empty", `{"todos":[]}`, `{"todos":[],"counts":{"pending":0,"inProgress":0,"completed":0}}`, "Updated todo list: 0 pending, 0 in progress, 0 completed."},
	} {
		outcome, err := h.Tools().Execute(t.Context(), harness.ToolRequest{Agent: agent, CallID: harness.ToolCallID(test.name), Name: ToolName, Arguments: json.RawMessage(test.input)})
		if err != nil || outcome.Result.IsError {
			t.Fatalf("%s: %+v, %v", test.name, outcome, err)
		}
		var got, want any
		if err := json.Unmarshal(outcome.Result.Value, &got); err != nil {
			t.Fatal(err)
		}
		if err := json.Unmarshal([]byte(test.value), &want); err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("%s value=%s", test.name, outcome.Result.Value)
		}
		if len(outcome.Result.Content) != 1 || outcome.Result.Content[0].Text != test.text {
			t.Fatalf("%s presentation=%+v", test.name, outcome.Result.Content)
		}
		var value todoToolValue
		if err := json.Unmarshal(outcome.Result.Value, &value); err != nil {
			t.Fatal(err)
		}
		current, found, err := Current(agent.Session())
		if err != nil || !found || !slices.Equal(current, value.Todos) {
			t.Fatalf("state=%+v value=%+v found=%v err=%v", current, value.Todos, found, err)
		}
	}
	invalid, err := h.Tools().Execute(t.Context(), harness.ToolRequest{Agent: agent, CallID: "invalid", Name: ToolName, Arguments: json.RawMessage(`{"todos":[{"content":" ","status":"pending"}]}`)})
	if err != nil || !invalid.Result.IsError || len(invalid.Result.Value) != 0 {
		t.Fatalf("error exposed a canonical success: %+v, %v", invalid, err)
	}
	for _, raw := range []json.RawMessage{
		append(append([]byte(`{"todos":[{"content":"`), 0xff), []byte(`","status":"pending"}]}`)...),
		json.RawMessage(`{"todos":[{"content":"\ud800","status":"pending"}]}`),
	} {
		outcome, err := h.Tools().Execute(t.Context(), harness.ToolRequest{Agent: agent, CallID: "invalid-string", Name: ToolName, Arguments: raw})
		if err != nil || !outcome.Result.IsError || len(outcome.Result.Value) != 0 {
			t.Fatalf("unrepresentable text was normalized: %+v, %v", outcome, err)
		}
	}

	projection, err := definition.RenderOutput(t.Context(), harness.ToolRequest{}, json.RawMessage(`{"todos":[],"counts":{"pending":3,"inProgress":2,"completed":1}}`))
	if err != nil || projection.Content[0].Text != "Updated todo list: 3 pending, 2 in progress, 1 completed." {
		t.Fatalf("value renderer=%+v, %v", projection, err)
	}
}

func TestTodoCanonicalValueOnlyRewriteRendersJSONIntegers(t *testing.T) {
	for _, tc := range []struct{ name, value, text string }{
		{"decimal and exponent", `{"todos":[],"counts":{"pending":1.0,"inProgress":0e0,"completed":0}}`, "Updated todo list: 1 pending, 0 in progress, 0 completed."},
		{"beyond native integer range", `{"todos":[],"counts":{"pending":100000000000000000000,"inProgress":1e21,"completed":0}}`, "Updated todo list: 100000000000000000000 pending, 1e+21 in progress, 0 completed."},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h, err := harness.Boot(t.Context(), harness.Options{Adapter: noCallAdapter{}})
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = h.Dispose(context.Background()); _ = h.Drain(context.Background()) })
			if _, err := Mount(t.Context(), h, Options{}); err != nil {
				t.Fatal(err)
			}
			agent, err := h.Agents().Create(t.Context(), harness.AgentOptions{ID: "todo-rewrite", Configure: func(_ context.Context, run *point.Run, _ *harness.Agent) error {
				_, err := h.Tools().After.Use(run, func(_ context.Context, outcome harness.ToolOutcome, next point.Next[harness.ToolOutcome]) (harness.ToolOutcome, error) {
					result, err := next(outcome)
					result.Result.Value = json.RawMessage(tc.value)
					return result, err
				})
				return err
			}})
			if err != nil {
				t.Fatal(err)
			}
			outcome, err := h.Tools().Execute(t.Context(), harness.ToolRequest{Agent: agent, CallID: "rewrite", Name: ToolName, Arguments: json.RawMessage(`{"todos":[]}`)})
			if err != nil || outcome.Result.IsError || string(outcome.Result.Value) != tc.value || len(outcome.Result.Content) != 1 || outcome.Result.Content[0].Text != tc.text {
				t.Fatalf("canonical rewrite=%+v, %v", outcome, err)
			}
		})
	}
}

