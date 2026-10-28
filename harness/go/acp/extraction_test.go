package acp

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"os"
	"testing"
	"time"

	"github.com/phodal/pero/harness/go/harness"
)

func TestExtractedACPWireLifecyclePermissionAndResume(t *testing.T) {
	ctx := context.Background()
	providerCalls, toolCalls := 0, 0
	h, err := harness.Boot(ctx, harness.Options{AdapterProviders: []string{"test"}, ApprovalPolicy: harness.ApprovalAsk, Adapter: harness.AdapterFunc(func(_ context.Context, request harness.GenerateRequest, _ func(harness.StreamChunk) error) (harness.Completion, error) {
		providerCalls++
		if providerCalls == 1 {
			return harness.Completion{Finish: harness.FinishToolCalls, Message: harness.Message{Role: harness.RoleAssistant, Content: []harness.ContentBlock{{Type: harness.BlockToolCall, Name: "guarded", ToolCallID: "call-1", Arguments: json.RawMessage(`{}`)}}}}, nil
		}
		return harness.Completion{Finish: harness.FinishStop, Message: harness.Message{Role: harness.RoleAssistant, Content: []harness.ContentBlock{{Type: harness.BlockText, Text: "done"}}}}, nil
	})})
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		_ = h.Dispose(ctx)
		if err := h.Drain(ctx); err != nil {
			t.Error(err)
		}
	}()
	_, err = h.RegisterTool(ctx, "test/guarded", harness.Tool{Schema: harness.ToolSchema{Name: "guarded", InputSchema: json.RawMessage(`{"type":"object"}`)}, RequiresApproval: true, Execute: func(context.Context, json.RawMessage) (harness.ToolResult, error) {
		toolCalls++
		return harness.ToolResult{}, nil
	}})
	if err != nil {
		t.Fatal(err)
	}
	persistence, err := harness.NewJSONLSessionPersistence(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	s, err := NewServer(Options{Harness: h, Persistence: persistence, Provider: "test", Model: "scripted"})
	if err != nil {
		t.Fatal(err)
	}
	client, server := net.Pipe()
	defer client.Close()
	_ = client.SetDeadline(time.Now().Add(10 * time.Second))
	served := make(chan error, 1)
	go func() { served <- s.Serve(ctx, server, server); _ = server.Close() }()
	defer func() {
		_ = client.Close()
		if err := <-served; err != nil {
			t.Error(err)
		}
	}()
	encoder := json.NewEncoder(client)
	scanner := bufio.NewScanner(client)
	var next int
	var updates, permissions int
	rpc := func(method string, params any) map[string]any {
		t.Helper()
		next++
		id := next
		if err := encoder.Encode(map[string]any{"jsonrpc": "2.0", "id": id, "method": method, "params": params}); err != nil {
			t.Fatal(err)
		}
		for scanner.Scan() {
			var message map[string]any
			if err := json.Unmarshal(scanner.Bytes(), &message); err != nil {
				t.Fatal(err)
			}
			if message["method"] == "session/request_permission" {
				permissions++
				if err := encoder.Encode(map[string]any{"jsonrpc": "2.0", "id": message["id"], "result": map[string]any{"outcome": map[string]any{"outcome": "selected", "optionId": "reject-once"}}}); err != nil {
					t.Fatal(err)
				}
				continue
			}
			if message["method"] == "session/update" {
				updates++
				continue
			}
			if message["id"] == float64(id) {
				if message["error"] != nil {
					t.Fatalf("%s: %v", method, message["error"])
				}
				return message["result"].(map[string]any)
			}
		}
		t.Fatalf("%s EOF: %v", method, scanner.Err())
		return nil
	}
	initialized := rpc("initialize", map[string]any{"protocolVersion": 1, "clientCapabilities": map[string]any{}})
	if initialized["protocolVersion"] != float64(1) {
		t.Fatal(initialized)
	}
	cwd := t.TempDir()
	created := rpc("session/new", map[string]any{"cwd": cwd, "mcpServers": []any{}})
	id := created["sessionId"].(string)
	result := rpc("session/prompt", map[string]any{"sessionId": id, "prompt": []any{map[string]any{"type": "text", "text": "do it"}}})
	if result["stopReason"] != "end_turn" || permissions != 1 || toolCalls != 0 || updates == 0 {
		t.Fatalf("result=%v permissions=%d tools=%d updates=%d", result, permissions, toolCalls, updates)
	}
	rpc("session/close", map[string]any{"sessionId": id})
	listed := rpc("session/list", map[string]any{})
	if len(listed["sessions"].([]any)) != 1 {
		t.Fatal("closed session not recoverable", listed)
	}
	before := updates
	rpc("session/resume", map[string]any{"sessionId": id, "cwd": cwd, "mcpServers": []any{}})
	if updates != before {
		t.Fatal("resume replayed old notifications")
	}
	rpc("session/prompt", map[string]any{"sessionId": id, "prompt": []any{map[string]any{"type": "text", "text": "continue"}}})
	if providerCalls != 3 || toolCalls != 0 {
		t.Fatal("resume replayed work")
	}
	rpc("session/close", map[string]any{"sessionId": id})
}

// A signal may close stdin, but must not turn a final durability failure into success.
func TestCancelledInputPreservesFinalCloseError(t *testing.T) {
	h, err := harness.Boot(context.Background(), harness.Options{})
	if err != nil {
		t.Fatal(err)
	}
	defer h.Dispose(context.Background())
	persistence, err := harness.NewJSONLSessionPersistence(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	s, err := NewServer(Options{Harness: h, Persistence: persistence, Provider: "fixture", Model: "fixture"})
	if err != nil {
		t.Fatal(err)
	}
	failure := errors.New("fixture final durability failure")
	s.retireErr = failure
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err = s.Serve(ctx, closedInput{}, io.Discard)
	if !errors.Is(err, failure) || errors.Is(err, os.ErrClosed) {
		t.Fatalf("shutdown error = %v", err)
	}
}

type closedInput struct{}

func (closedInput) Read([]byte) (int, error) { return 0, os.ErrClosed }
