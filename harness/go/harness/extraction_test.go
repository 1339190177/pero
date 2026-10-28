package harness

import (
	"context"
	"encoding/json"
	"errors"
	"sync/atomic"
	"testing"
)

func testBoot(t *testing.T, adapter Adapter) *Harness {
	t.Helper()
	h, err := Boot(context.Background(), Options{Adapter: adapter, AdapterProviders: []string{"test"}, ApprovalPolicy: ApprovalNever})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = h.Dispose(context.Background())
		if err := h.Drain(context.Background()); err != nil {
			t.Error(err)
		}
	})
	return h
}

func TestExtractedLoopDurableToolRoundTripAndResume(t *testing.T) {
	ctx := context.Background()
	persistence, err := NewJSONLSessionPersistence(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	var requests, executions atomic.Int32
	adapter := AdapterFunc(func(ctx context.Context, request GenerateRequest, emit func(StreamChunk) error) (Completion, error) {
		// Every provider dispatch must see the durable prefix, not an unflushed claim.
		stored, err := persistence.Load(ctx, "roundtrip")
		if err != nil || len(stored.Events()) == 0 {
			t.Fatalf("dispatch preceded durable prefix: %v", err)
		}
		if requests.Add(1) == 1 {
			return Completion{Finish: FinishToolCalls, Message: Message{Role: RoleAssistant, Content: []ContentBlock{{Type: BlockToolCall, ToolCallID: "call-1", Name: "double", Arguments: json.RawMessage(`{"value":21}`)}}}}, nil
		}
		if requests.Load() == 2 {
			found := false
			for _, message := range request.Messages {
				for _, block := range message.Content {
					if block.Type == BlockToolResult && block.ToolCallID == "call-1" {
						found = true
					}
				}
			}
			if !found {
				t.Fatal("second step lost committed tool result")
			}
		}
		return Completion{Finish: FinishStop, Message: Message{Role: RoleAssistant, Content: []ContentBlock{{Type: BlockText, Text: "42"}}}}, nil
	})
	h := testBoot(t, adapter)
	_, err = h.RegisterTool(ctx, "test/double", Tool{Schema: ToolSchema{Name: "double", InputSchema: json.RawMessage(`{"type":"object","properties":{"value":{"type":"integer"}},"required":["value"],"additionalProperties":false}`)}, Execute: func(ctx context.Context, input json.RawMessage) (ToolResult, error) {
		executions.Add(1)
		stored, err := persistence.Load(ctx, "roundtrip")
		if err != nil || len(stored.Events()) == 0 {
			t.Fatalf("tool preceded durability: %v", err)
		}
		return ToolResult{Content: []ContentBlock{{Type: BlockText, Text: "42"}}}, nil
	}})
	if err != nil {
		t.Fatal(err)
	}
	agent, err := h.Agents().Create(ctx, AgentOptions{ID: "roundtrip", Provider: "test", Model: "scripted"})
	if err != nil {
		t.Fatal(err)
	}
	detach, err := persistence.Attach(ctx, agent.Session())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := agent.Prompt(ctx, Message{Role: RoleUser, Content: []ContentBlock{{Type: BlockText, Text: "double 21"}}}); err != nil {
		t.Fatal(err)
	}
	if executions.Load() != 1 || requests.Load() != 2 {
		t.Fatalf("requests=%d tools=%d", requests.Load(), executions.Load())
	}
	if err := h.Agents().RemoveExact(ctx, agent); err != nil {
		t.Fatal(err)
	}
	if err := detach(); err != nil {
		t.Fatal(err)
	}
	restored, err := persistence.Load(ctx, "roundtrip")
	if err != nil {
		t.Fatal(err)
	}
	before := len(restored.Events())
	resumed, err := h.Agents().Resume(ctx, restored, AgentOptions{Provider: "test", Model: "scripted"})
	if err != nil {
		t.Fatal(err)
	}
	detach, err = persistence.Attach(ctx, resumed.Session())
	if err != nil {
		t.Fatal(err)
	}
	defer detach()
	if _, err := resumed.Prompt(ctx, Message{Role: RoleUser, Content: []ContentBlock{{Type: BlockText, Text: "continue"}}}); err != nil {
		t.Fatal(err)
	}
	if len(resumed.Session().Events()) <= before || executions.Load() != 1 || requests.Load() != 3 {
		t.Fatal("resume replayed tool or lost history")
	}
}

func TestExtractedToolValidationAndApprovalFailClosed(t *testing.T) {
	h := testBoot(t, AdapterFunc(func(context.Context, GenerateRequest, func(StreamChunk) error) (Completion, error) {
		return Completion{}, errors.New("unused")
	}))
	var calls atomic.Int32
	_, err := h.RegisterTool(context.Background(), "test/guarded", Tool{Schema: ToolSchema{Name: "guarded", InputSchema: json.RawMessage(`{"type":"object","properties":{"value":{"type":"integer"}},"required":["value"]}`)}, RequiresApproval: true, Execute: func(context.Context, json.RawMessage) (ToolResult, error) { calls.Add(1); return ToolResult{}, nil }})
	if err != nil {
		t.Fatal(err)
	}
	agent, err := h.Agents().Create(context.Background(), AgentOptions{ID: "denied", Provider: "test", Model: "scripted"})
	if err != nil {
		t.Fatal(err)
	}
	for _, args := range []string{`{"value":"bad"}`, `{"value":1}`} {
		outcome, err := h.Tools().Execute(context.Background(), ToolRequest{Agent: agent, CallID: "guard", Name: "guarded", Arguments: json.RawMessage(args)})
		if err == nil && !outcome.Result.IsError {
			t.Fatal("invalid or unapproved call succeeded")
		}
	}
	if calls.Load() != 0 {
		t.Fatal("guarded body executed")
	}
}

func TestExtractedLoopCancellationSettles(t *testing.T) {
	entered := make(chan struct{})
	h := testBoot(t, AdapterFunc(func(ctx context.Context, _ GenerateRequest, _ func(StreamChunk) error) (Completion, error) {
		close(entered)
		<-ctx.Done()
		return Completion{}, ctx.Err()
	}))
	a, err := h.Agents().Create(context.Background(), AgentOptions{ID: "cancel", Provider: "test", Model: "scripted"})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() {
		_, err := a.Prompt(ctx, Message{Role: RoleUser, Content: []ContentBlock{{Type: BlockText, Text: "wait"}}})
		done <- err
	}()
	<-entered
	cancel()
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation=%v", err)
	}
}
