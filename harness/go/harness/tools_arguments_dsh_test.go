package harness

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	point "github.com/phodal/pero/harness/go/runtime"
)

const dshArgumentFixtureSchema = `{"type":"object","properties":{"file_path":{"type":"string"},"content":{"type":"string"},"offset":{"type":"number"},"replace_all":{"type":"boolean"},"sandbox_permissions":{"type":"string","enum":["workspace-write","danger-full-access"]}},"required":["file_path","content"]}`

func dshArgumentFixtureTool(body func(context.Context, json.RawMessage) (ToolResult, error)) Tool {
	return Tool{Schema: ToolSchema{Name: "diagnostic", InputSchema: json.RawMessage(dshArgumentFixtureSchema)}, ArgumentErrorMode: ArgumentErrorsDSHAlpha3Flat, Execute: body}
}

func TestDSHArgumentFailurePublicDiagnosticsAndAdmission(t *testing.T) {
	cases := []struct{ raw, message string }{
		{`null`, `"arguments" must be an object`},
		{`[]`, `"arguments" must be an object`},
		{`false`, `"arguments" must be an object`},
		{`1`, `"arguments" must be an object`},
		{`"text"`, `"arguments" must be an object`},
		{`{}`, `missing required property "file_path"; missing required property "content"`},
		{``, `missing required property "file_path"; missing required property "content"`},
		{`{"replace_all":null,"offset":"1","file_path":false}`, `missing required property "content"; "file_path" must be a string; "offset" must be a number; "replace_all" must be a boolean`},
		{`{"file_path":"x","content":null}`, `"content" must be a string`},
		{`{"file_path":"x","content":"","sandbox_permissions":"invalid"}`, `"sandbox_permissions" must be one of ["workspace-write","danger-full-access"]`},
		{`{"file_path":"x","content":"","sandbox_permissions":false}`, `"sandbox_permissions" must be a string`},
		{`{"file_path":"\ud83d\ude00","content":null}`, `"content" must be a string`},
	}
	for _, legacy := range []bool{false, true} {
		t.Run(map[bool]string{false: "dsh", true: "point"}[legacy], func(t *testing.T) {
			h := retirementHarness(t)
			agent, err := h.Agents().Create(t.Context(), AgentOptions{ID: "diagnostics"})
			if err != nil {
				t.Fatal(err)
			}
			if _, err := agent.Session().Append("turn/start", map[string]int{"turn": 1}); err != nil {
				t.Fatal(err)
			}
			bodies, before, after := 0, 0, 0
			approvals, reasons, guards := 0, 0, 0
			tool := dshArgumentFixtureTool(func(context.Context, json.RawMessage) (ToolResult, error) { bodies++; return ToolResult{}, nil })
			tool.RequiresApproval = true
			tool.ApprovalReason = func(ToolRequest) string { reasons++; return "synthetic diagnostic test" }
			if legacy {
				tool.ArgumentErrorMode = ArgumentErrorsLegacy
			}
			mountRetirementHooks(t, h, func(run *point.Run) error {
				if _, err := h.Approvals().Register(run, func(context.Context, ApprovalRequest) (ApprovalOutcome, error) {
					approvals++
					return ApprovalAllowedOnce, nil
				}); err != nil {
					return err
				}
				if _, err := h.Tools().Guard(run, func(context.Context, ToolRequest) error { guards++; return nil }); err != nil {
					return err
				}
				if _, err := h.Tools().Register(run, tool); err != nil {
					return err
				}
				if _, err := h.Tools().Before.Use(run, func(_ context.Context, request ToolRequest, next point.Next[ToolRequest]) (ToolRequest, error) {
					before++
					return next(request)
				}); err != nil {
					return err
				}
				_, err := h.Tools().After.Use(run, func(_ context.Context, outcome ToolOutcome, next point.Next[ToolOutcome]) (ToolOutcome, error) {
					after++
					return next(outcome)
				})
				return err
			})
			for _, tc := range cases {
				out, err := h.Tools().Execute(t.Context(), ToolRequest{Agent: agent, Name: "diagnostic", Arguments: json.RawMessage(tc.raw)})
				if err != nil || !out.Result.IsError || len(out.Result.Value) != 0 || out.Result.ConcludesTurn {
					t.Fatalf("raw=%s outcome=%+v err=%v", tc.raw, out, err)
				}
				if legacy {
					if len(out.Result.Meta) != 0 || len(out.Result.Content) != 1 || !strings.HasPrefix(out.Result.Content[0].Text, "invalid tool arguments for diagnostic:") {
						t.Fatalf("legacy changed: %+v", out.Result)
					}
					continue
				}
				message := "invalid arguments: " + tc.message
				var meta struct {
					Name, Code, Message string
					Info                struct{ Name, Code string }
				}
				if json.Unmarshal(out.Result.Meta, &meta) != nil || meta.Name != "ToolArgsError" || meta.Code != "INVALID_ARGS" || meta.Message != message || meta.Info.Name != meta.Name || meta.Info.Code != meta.Code || len(out.Result.Content) != 1 || out.Result.Content[0].Text != "Error: "+message {
					t.Fatalf("raw=%s result=%+v meta=%+v", tc.raw, out.Result, meta)
				}
			}
			if bodies != 0 || before != len(cases) || after != 0 || approvals != 0 || reasons != 0 || guards != 0 {
				t.Fatalf("failure changed pipeline execution: body=%d Before=%d After=%d approvals=%d reasons=%d guards=%d", bodies, before, after, approvals, reasons, guards)
			}
			out, err := h.Tools().Execute(t.Context(), ToolRequest{Agent: agent, Name: "diagnostic", Arguments: json.RawMessage(`{"file_path":"x","content":"","offset":1.5,"unknown":{"nested":true}}`)})
			if err != nil || out.Result.IsError || bodies != 1 || before != len(cases)+1 || after != 1 || approvals != 1 || reasons != 1 || guards != 1 {
				t.Fatalf("source open extras or numeric body domain changed: %+v err=%v counts=%d/%d/%d approvals=%d reasons=%d guards=%d", out.Result, err, bodies, before, after, approvals, reasons, guards)
			}
		})
	}
}

func TestDSHArgumentFailureDoesNotReclassifyNativeJSONLimits(t *testing.T) {
	h := retirementHarness(t)
	agent, err := h.Agents().Create(t.Context(), AgentOptions{ID: "diagnostic-limits"})
	if err != nil {
		t.Fatal(err)
	}
	calls := 0
	if _, err := h.RegisterTool(t.Context(), "diagnostic-limits", dshArgumentFixtureTool(func(context.Context, json.RawMessage) (ToolResult, error) { calls++; return ToolResult{}, nil })); err != nil {
		t.Fatal(err)
	}
	for _, raw := range []string{`{`, `{"file_path":"x","content":"","offset":1e400}`, `{"file_path":false,"content":"","offset":-0}`, `{"file_path":false,"content":"","offset":-1e-999}`, `{"file_path":false,"content":"","unknown":[{"n":-0}]}`, `{"file_path":"\ud800","content":null}`, "{\"file_path\":\"\xff\",\"content\":null}"} {
		out, err := h.Tools().Execute(t.Context(), ToolRequest{Agent: agent, Name: "diagnostic", Arguments: json.RawMessage(raw)})
		if err != nil || !out.Result.IsError || len(out.Result.Meta) != 0 || len(out.Result.Content) != 1 || !strings.HasPrefix(out.Result.Content[0].Text, "invalid tool arguments for diagnostic:") {
			t.Fatalf("native JSON boundary was mislabeled source INVALID_ARGS: raw=%q result=%+v err=%v", raw, out.Result, err)
		}
	}
	if calls != 0 {
		t.Fatal("unrepresentable rejected input executed")
	}
}

func TestDSHArgumentFailureOptInRejectsUnsupportedSchemas(t *testing.T) {
	for _, schema := range []string{
		`{"type":"object","properties":{"n":{"type":"integer"}}}`,
		`{"type":"object","properties":{"a":{"type":"array"}}}`,
		`{"type":"object","properties":{"s":{"type":"string","minLength":1}}}`,
		`{"type":"object","properties":{"s":{"type":"string"}},"additionalProperties":false}`,
		`{"type":"object","properties":{"s":{"type":"string","enum":[1]}}}`,
	} {
		tool := dshArgumentFixtureTool(func(context.Context, json.RawMessage) (ToolResult, error) {
			t.Fatal("schema compilation executed body")
			return ToolResult{}, nil
		})
		tool.Schema.InputSchema = json.RawMessage(schema)
		if _, err := prepareTool(tool); err == nil {
			t.Fatalf("unsupported diagnostic schema admitted: %s", schema)
		}
		tool.ArgumentErrorMode = ArgumentErrorsLegacy
		if _, err := prepareTool(tool); err != nil {
			t.Fatalf("legacy schema changed: %s: %v", schema, err)
		}
	}
	tool := dshArgumentFixtureTool(func(context.Context, json.RawMessage) (ToolResult, error) { return ToolResult{}, nil })
	tool.ArgumentErrorMode = ToolArgumentErrorMode(255)
	if _, err := prepareTool(tool); err == nil {
		t.Fatal("unknown trusted mode accepted")
	}
}

func TestDSHArgumentFailureRejectsSourceUnsupportedScalarSchemaValues(t *testing.T) {
	for _, schema := range []string{
		`{"type":"object","properties":{"n":{"type":"number","enum":[-0]}}}`,
		`{"type":"object","properties":{"n":{"type":"number","enum":[-1e-999]}}}`,
		`{"type":"object","properties":{},"required":null}`,
		`{"type":"object","properties":{"":{"type":"string"}},"required":[null]}`,
		`{"type":"object","properties":{},"title":null}`,
		`{"type":"object","properties":{},"description":null}`,
		`{"type":"object","properties":{"s":{"type":"string","title":null}}}`,
		`{"type":"object","properties":{"s":{"type":"string","description":null}}}`,
	} {
		if _, err := compileArgumentErrorProjection(ArgumentErrorsDSHAlpha3Flat, json.RawMessage(schema)); err == nil {
			t.Fatalf("source-unsupported schema accepted by opt-in descriptor: %s", schema)
		}
		tool := dshArgumentFixtureTool(func(context.Context, json.RawMessage) (ToolResult, error) {
			t.Fatal("schema compilation executed body")
			return ToolResult{}, nil
		})
		tool.Schema.InputSchema = json.RawMessage(schema)
		if _, err := prepareTool(tool); err == nil {
			t.Fatalf("source-unsupported schema accepted by opt-in registration: %s", schema)
		}
		if _, err := compileArgumentErrorProjection(ArgumentErrorsLegacy, json.RawMessage(schema)); err != nil {
			t.Fatalf("legacy projection changed: %s: %v", schema, err)
		}
	}
	for _, schema := range []string{
		`{"type":"object","properties":{"n":{"type":"number","enum":[0]}}}`,
		`{"type":"object","properties":{"":{"type":"string","description":"","title":""}},"required":[""],"description":"","title":""}`,
		`{"type":"object","properties":{},"required":[]}`,
	} {
		if _, err := compileArgumentErrorProjection(ArgumentErrorsDSHAlpha3Flat, json.RawMessage(schema)); err != nil {
			t.Fatalf("supported schema rejected by opt-in descriptor: %s: %v", schema, err)
		}
	}
}

func TestDSHArgumentFailureCaptureKeepsDeclarationOrderAndOwner(t *testing.T) {
	h := retirementHarness(t)
	agent, err := h.Agents().Create(t.Context(), AgentOptions{ID: "diagnostic-capture"})
	if err != nil {
		t.Fatal(err)
	}
	calls := 0
	tool := dshArgumentFixtureTool(func(context.Context, json.RawMessage) (ToolResult, error) { calls++; return ToolResult{}, nil })
	owner, err := h.RegisterTool(t.Context(), "diagnostic-old", tool)
	if err != nil {
		t.Fatal(err)
	}
	base := ToolCatalogBase{registry: h.Tools(), scope: h.Runtime().Root(), generation: "diagnostic-presenter"}
	captured, ok, err := base.Resolve(t.Context(), agent, "diagnostic")
	if err != nil || !ok {
		t.Fatalf("capture=%v/%v", ok, err)
	}
	var unordered map[string]any
	_ = json.Unmarshal(tool.Schema.InputSchema, &unordered)
	reordered, _ := json.Marshal(unordered)
	proxy := tool
	proxy.ArgumentErrorMode = ArgumentErrorsLegacy
	proxy.Schema.InputSchema = reordered
	// Public mutations of the returned capture cannot change its private receipt.
	captured.ArgumentErrorMode = ArgumentErrorsLegacy
	captured.Schema.InputSchema = json.RawMessage(`{"type":"object"}`)
	bound, err := base.BindCapturedOwner(proxy, captured)
	if err != nil {
		t.Fatal(err)
	}
	bound, err = prepareTool(bound)
	if err != nil {
		t.Fatal(err)
	}
	if bound.ArgumentErrorMode != ArgumentErrorsDSHAlpha3Flat || string(bound.Schema.InputSchema) != dshArgumentFixtureSchema {
		t.Fatal("captured diagnostic identity/order changed")
	}
	result, ok := projectArgumentFailure(bound, json.RawMessage(`{"offset":false,"content":null,"file_path":null}`))
	if !ok || result.Content[0].Text != `Error: invalid arguments: "file_path" must be a string; "content" must be a string; "offset" must be a number` {
		t.Fatalf("capture order=%+v", result)
	}
	changed := proxy
	changed.Schema.InputSchema = json.RawMessage(`{"type":"object","properties":{}}`)
	if _, err := base.BindCapturedOwner(changed, captured); err == nil {
		t.Fatal("changed proxy input contract accepted")
	}
	if err := owner.Dispose(t.Context()); err != nil {
		t.Fatal(err)
	}
	replacement := tool
	replacement.ArgumentErrorMode = ArgumentErrorsLegacy
	if _, err := h.RegisterTool(t.Context(), "diagnostic-new", replacement); err != nil {
		t.Fatal(err)
	}
	if _, err := base.BindCapturedOwner(proxy, captured); err == nil {
		t.Fatal("retired capture revived by same-name replacement")
	}
	if _, ok := projectArgumentFailure(bound, json.RawMessage(`{}`)); ok {
		t.Fatal("retired descriptor reused")
	}
	out, err := h.Tools().Execute(t.Context(), ToolRequest{Agent: agent, Name: "diagnostic", Arguments: json.RawMessage(`{}`)})
	if err != nil || !out.Result.IsError || len(out.Result.Meta) != 0 || calls != 0 {
		t.Fatalf("same-name replacement inherited old mode or executed: %+v err=%v calls=%d", out.Result, err, calls)
	}
}

func TestDSHArgumentFailureDescriptorSnapshotsSchemaAndJSONRendering(t *testing.T) {
	tool := dshArgumentFixtureTool(func(context.Context, json.RawMessage) (ToolResult, error) { return ToolResult{}, nil })
	prepared, err := prepareTool(tool)
	if err != nil {
		t.Fatal(err)
	}
	tool.Schema.InputSchema[0] = '!'
	if string(prepared.Schema.InputSchema) != dshArgumentFixtureSchema {
		t.Fatal("registered diagnostic schema retained caller-owned bytes")
	}
	tool.Schema.InputSchema = json.RawMessage(`{"type":"object","properties":{"z":{"type":"string"},"10":{"type":"string"},"2":{"type":"string"},"":{"type":"string"}}}`)
	prepared, err = prepareTool(tool)
	if err != nil {
		t.Fatal(err)
	}
	result, ok := projectArgumentFailure(prepared, json.RawMessage(`{"z":null,"10":null,"2":null,"":null}`))
	if !ok || result.Content[0].Text != `Error: invalid arguments: "2" must be a string; "10" must be a string; "z" must be a string; "arguments" must be a string` {
		t.Fatalf("source Object.entries order/path changed: %+v", result)
	}
	if got := dshArgumentEnumJSON([]any{"<tag>", "\u2028", `\u2028`, "\u2029"}); got != "[\"<tag>\",\"\u2028\",\"\\\\u2028\",\"\u2029\"]" {
		t.Fatalf("source enum JSON changed: %q", got)
	}
}
