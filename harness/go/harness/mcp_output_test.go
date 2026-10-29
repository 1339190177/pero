package harness

import (
	"context"
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	point "github.com/phodal/pero/harness/go/runtime"
)

// Actual SDK tools/list and tools/call exercise the mount and registry boundary.
func TestMCPCanonicalOutput(t *testing.T) {
	supported := json.RawMessage(`{"type":"object","properties":{"count":{"type":"integer"}},"required":["count"],"additionalProperties":false}`)
	unsupported := json.RawMessage(`{"type":"object","patternProperties":{"^x-":{"type":"string"}}}`)
	text := []mcp.Content{&mcp.TextContent{Text: "summary"}}
	for _, tc := range []struct {
		name                 string
		schema               any
		content              []mcp.Content
		structured           any
		remoteError, rewrite bool
		wantText, wantValue  string
		wantError            bool
		wantImage            bool
	}{
		{name: "text and structured", schema: supported, content: text, structured: map[string]any{"count": 7}, wantText: "summary", wantValue: `{"content":[{"type":"text","text":"summary"}],"structuredContent":{"count":7}}`},
		{name: "structured only", schema: supported, content: []mcp.Content{}, structured: map[string]any{"count": 7}, wantText: `{"count":7}`, wantValue: `{"content":[],"structuredContent":{"count":7}}`},
		{name: "image content", content: []mcp.Content{&mcp.ImageContent{Data: []byte{1, 2, 3}, MIMEType: "image/png"}}, wantImage: true, wantValue: `{"content":[{"type":"image","data":"AQID","mimeType":"image/png"}]}`},
		{name: "legacy text", content: text, wantText: "summary", wantValue: `{"content":[{"type":"text","text":"summary"}]}`},
		{name: "invalid structured", schema: supported, content: text, structured: map[string]any{"count": "seven"}, wantError: true},
		{name: "missing required structured", schema: supported, content: text, wantError: true},
		{name: "unsupported schema preserves structured", schema: unsupported, content: text, structured: map[string]any{"x-count": "seven"}, wantText: "summary", wantValue: `{"content":[{"type":"text","text":"summary"}],"structuredContent":{"x-count":"seven"}}`},
		{name: "unsupported schema permits text only", schema: unsupported, content: text, wantText: "summary", wantValue: `{"content":[{"type":"text","text":"summary"}]}`},
		{name: "remote error bypasses success schema", schema: supported, content: text, remoteError: true, wantError: true, wantText: "summary"},
		{name: "policy value reprojects", schema: supported, content: text, structured: map[string]any{"count": 7}, rewrite: true, wantText: "updated", wantValue: `{"content":[{"type":"text","text":"updated"}],"structuredContent":{"count":8}}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := mcp.NewServer(&mcp.Implementation{Name: "canonical-fixture", Version: "1"}, nil)
			server.AddTool(&mcp.Tool{Name: "count", InputSchema: json.RawMessage(`{"type":"object"}`), OutputSchema: tc.schema}, func(context.Context, *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
				return &mcp.CallToolResult{Content: tc.content, StructuredContent: tc.structured, IsError: tc.remoteError}, nil
			})
			ct, st := mcp.NewInMemoryTransports()
			session, err := server.Connect(t.Context(), st, nil)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = session.Close() })
			h := retirementHarness(t)
			mountRetirementHooks(t, h, func(run *point.Run) error {
				if err := MountMCPServers(t.Context(), run, h.Tools(), h.Prompts(), []MCPServerConfig{{Name: "fixture", TestTransport: ct, FailOnStartupError: true}}); err != nil {
					return err
				}
				if tc.rewrite {
					_, err := h.Tools().After.Use(run, func(_ context.Context, out ToolOutcome, next point.Next[ToolOutcome]) (ToolOutcome, error) {
						out.Result.Value = json.RawMessage(tc.wantValue)
						return next(out)
					})
					return err
				}
				return nil
			})
			agent, err := h.Agents().Create(t.Context(), AgentOptions{ID: "mcp-canonical"})
			if err != nil {
				t.Fatal(err)
			}
			outcome, err := h.Tools().Execute(t.Context(), ToolRequest{Agent: agent, Name: PublicMCPToolName("fixture", "count"), Arguments: json.RawMessage(`{}`)})
			if err != nil {
				t.Fatal(err)
			}
			out := outcome.Result
			if out.IsError != tc.wantError {
				t.Fatalf("error=%v content=%+v", out.IsError, out.Content)
			}
			if tc.wantError {
				if out.Value != nil {
					t.Fatalf("failure exposed success value: %s", out.Value)
				}
				if !tc.remoteError && !strings.Contains(string(out.Meta), "INVALID_TOOL_OUTPUT") {
					t.Fatalf("diagnostic=%s", out.Meta)
				}
			} else {
				var got, want any
				if err := json.Unmarshal(out.Value, &got); err != nil {
					t.Fatal(err)
				}
				if err := json.Unmarshal([]byte(tc.wantValue), &want); err != nil {
					t.Fatal(err)
				}
				if !reflect.DeepEqual(got, want) {
					t.Fatalf("canonical=%s want=%s", out.Value, tc.wantValue)
				}
			}
			if tc.wantImage && (len(out.Content) != 1 || out.Content[0].Type != BlockImage || !reflect.DeepEqual(out.Content[0].Data, []byte{1, 2, 3}) || out.Content[0].MIMEType != "image/png") {
				t.Fatalf("image projection = %+v", out.Content)
			}
			if tc.wantText != "" && (len(out.Content) != 1 || out.Content[0].Text != tc.wantText) {
				t.Fatalf("projection=%+v want=%q", out.Content, tc.wantText)
			}
		})
	}
}
