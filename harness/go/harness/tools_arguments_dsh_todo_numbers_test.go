package harness

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"

	point "github.com/phodal/pero/harness/go/runtime"
)

const todoNumberOverflowRaw = `{"todos":[{"content":1e309,"content":"kept","status":"pending"}]}`

func todoNumberPrepared(t *testing.T, marked bool) Tool {
	t.Helper()
	tool := dshTodoArgumentTool()
	tool.ArgumentErrorMode = ArgumentErrorsLegacy
	if marked {
		tool = WithDSHTodoNumberCompatibility(tool)
	}
	prepared, err := prepareTool(tool)
	if err != nil {
		t.Fatal(err)
	}
	return prepared
}

func TestDSHTodoNumberRetryRequiresDirectStdlibOverflow(t *testing.T) {
	tool := todoNumberPrepared(t, true)
	original := validateToolArguments(tool.inputValidator, json.RawMessage(todoNumberOverflowRaw))
	overflow, ok := original.(*json.UnmarshalTypeError)
	if !ok || overflow.Type != reflect.TypeFor[float64]() {
		t.Fatalf("actual validator error=%T %v", original, original)
	}
	if err := retryDSHTodoNumberArguments(tool, json.RawMessage(todoNumberOverflowRaw), original); err != nil {
		t.Fatal(err)
	}
	// Even an identical error string or a wrapped genuine error is not evidence
	// that the authoritative decoder returned this exact failure category.
	for _, other := range []error{errors.New(original.Error()), fmt.Errorf("wrapped: %w", original), &json.UnmarshalTypeError{Type: reflect.TypeFor[string]()}, &json.SyntaxError{}, (*json.UnmarshalTypeError)(nil)} {
		if got := retryDSHTodoNumberArguments(tool, json.RawMessage(todoNumberOverflowRaw), other); got != other {
			t.Fatalf("non-direct error replaced: %T", other)
		}
	}
	for _, raw := range []string{`{`, `{} {}`, `{"todos":null}`, `{"todos":[{"content":true,"status":"pending"}]}`, `{"todos":[{"content":1e309,"content":null,"status":"pending"}]}`, `{"todos":[{"content":"kept","status":1e309}]}`, `{"todos":[],"extra":1e309}`} {
		original := validateToolArguments(tool.inputValidator, json.RawMessage(raw))
		if original == nil {
			t.Fatalf("negative control accepted: %s", raw)
		}
		if got := retryDSHTodoNumberArguments(tool, json.RawMessage(raw), original); got != original {
			t.Fatalf("failure lost original identity: %s got=%v", raw, got)
		}
	}
	legacy := todoNumberPrepared(t, false)
	if err := validateRegisteredToolArguments(legacy, json.RawMessage(todoNumberOverflowRaw)); err == nil {
		t.Fatal("default tool acquired compatibility")
	}
	for _, mode := range []ToolArgumentErrorMode{ArgumentErrorsLegacy, ArgumentErrorsDSHAlpha3Todo} {
		tool := dshTodoArgumentTool()
		tool.ArgumentErrorMode = mode
		for _, marked := range []bool{false, true} {
			candidate := tool
			if marked {
				candidate = WithDSHTodoNumberCompatibility(candidate)
			}
			prepared, err := prepareTool(candidate)
			if err != nil {
				t.Fatal(err)
			}
			if accepted := validateRegisteredToolArguments(prepared, json.RawMessage(todoNumberOverflowRaw)) == nil; accepted != marked {
				t.Fatalf("diagnostic mode changed admission: mode=%d marked=%v", mode, marked)
			}
		}
	}
}

func TestDSHTodoNumberRetryBudgetsAndOriginalFailure(t *testing.T) {
	tool := todoNumberPrepared(t, true)
	withExtra := func(value string) string {
		return strings.TrimSuffix(todoNumberOverflowRaw, "}") + `,"extra":` + value + `}`
	}
	byteLimit := todoNumberOverflowRaw + strings.Repeat(" ", dshTodoArgumentBytes-len(todoNumberOverflowRaw))
	// Base has 13 tokens; the extra member contributes key, array pair, and n values.
	tokenLimit := withExtra("[" + strings.Repeat("0,", dshTodoArgumentTokens-17) + "0]")
	depthLimit := withExtra(strings.Repeat("[", dshTodoArgumentDepth-1) + "0" + strings.Repeat("]", dshTodoArgumentDepth-1))
	for _, tc := range []struct {
		name, raw string
		accepted  bool
	}{
		{"byte-at", byteLimit, true}, {"byte-over", byteLimit + " ", false},
		{"token-at", tokenLimit, true}, {"token-over", strings.Replace(tokenLimit, `,"extra":[`, `,"extra":[0,`, 1), false},
		{"depth-at", depthLimit, true}, {"depth-over", withExtra(strings.Repeat("[", dshTodoArgumentDepth) + "0" + strings.Repeat("]", dshTodoArgumentDepth)), false},
		// Overwritten values count toward the budget even though none survive.
		{"overwritten-depth", `{"todos":[{"content":` + strings.Repeat("[", dshTodoArgumentDepth) + "1e309" + strings.Repeat("]", dshTodoArgumentDepth) + `,"content":"kept","status":"pending"}]}`, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			raw := json.RawMessage(tc.raw)
			before := string(raw)
			original := validateToolArguments(tool.inputValidator, raw)
			if _, ok := original.(*json.UnmarshalTypeError); !ok {
				t.Fatalf("not direct overflow: %T %v", original, original)
			}
			got := retryDSHTodoNumberArguments(tool, raw, original)
			if tc.accepted && got != nil || !tc.accepted && got != original {
				t.Fatalf("budget acceptance=%v error=%v", tc.accepted, got)
			}
			if string(raw) != before {
				t.Fatal("retry mutated raw audit")
			}
		})
	}
	for _, raw := range []string{`{"todos":[]}` + strings.Repeat(" ", dshTodoArgumentBytes), `{"todos":[],"extra":[` + strings.Repeat("0,", dshTodoArgumentTokens) + `0]}`, `{"todos":[],"extra":` + strings.Repeat("[", dshTodoArgumentDepth) + `0` + strings.Repeat("]", dshTodoArgumentDepth) + `}`} {
		if err := validateRegisteredToolArguments(tool, json.RawMessage(raw)); err != nil {
			t.Fatalf("retry budget became ordinary admission cap: %v", err)
		}
	}
}

func TestDSHTodoNumberRegistrationAndCapturedIdentity(t *testing.T) {
	for _, raw := range []string{dshTodoArgumentFixtureSchema, dshTodoArgumentFixtureSchema + strings.Repeat(" ", dshTodoSchemaBytes-len(dshTodoArgumentFixtureSchema)), strings.Replace(dshTodoArgumentFixtureSchema, `"additionalProperties":true`, `"additionalProperties":false`, 1)} {
		definition := WithDSHTodoNumberCompatibility(dshTodoArgumentTool())
		definition.ArgumentErrorMode = ArgumentErrorsLegacy
		definition.Schema.InputSchema = json.RawMessage(raw)
		prepared, err := prepareTool(definition)
		if err != nil {
			t.Fatal(err)
		}
		definition.Schema.InputSchema[0] = '!'
		if string(prepared.Schema.InputSchema) != raw || !prepared.dshTodoNumberCompatibility {
			t.Fatal("private definition did not snapshot schema")
		}
	}
	for _, raw := range []string{`{"type":"object"}`, strings.Replace(dshTodoArgumentFixtureSchema, `"type":"string"`, `"type":"number"`, 1), strings.Replace(dshTodoArgumentFixtureSchema, `"additionalProperties":false`, `"additionalProperties":true`, 1), dshTodoArgumentFixtureSchema + strings.Repeat(" ", dshTodoSchemaBytes)} {
		definition := WithDSHTodoNumberCompatibility(dshTodoArgumentTool())
		definition.ArgumentErrorMode = ArgumentErrorsLegacy
		definition.Schema.InputSchema = json.RawMessage(raw)
		if _, err := prepareTool(definition); err == nil {
			t.Fatal("unsupported registration accepted")
		}
	}
	var forged Tool
	if err := json.Unmarshal([]byte(`{"dshTodoNumberCompatibility":true,"DSHTodoNumberCompatibility":true,"ArgumentErrorMode":2}`), &forged); err != nil || forged.dshTodoNumberCompatibility || forged.ArgumentErrorMode != ArgumentErrorsLegacy {
		t.Fatalf("JSON selected private mode: %+v %v", forged, err)
	}
	h := dshTodoArgumentHarness(t)
	agent, err := h.Agents().Create(t.Context(), AgentOptions{ID: "todo-number-capture"})
	if err != nil {
		t.Fatal(err)
	}
	definition := WithDSHTodoNumberCompatibility(dshTodoArgumentTool())
	definition.ArgumentErrorMode = ArgumentErrorsLegacy
	owner, err := h.RegisterTool(t.Context(), "todo-number-owner", definition)
	if err != nil {
		t.Fatal(err)
	}
	base := ToolCatalogBase{registry: h.Tools(), scope: h.Runtime().Root(), generation: "todo-number-presenter"}
	captured, found, err := base.Resolve(t.Context(), agent, definition.Schema.Name)
	if err != nil || !found {
		t.Fatalf("capture %v %v", found, err)
	}
	proxy := definition
	proxy.dshTodoNumberCompatibility = false
	captured.dshTodoNumberCompatibility = false
	captured.Schema.InputSchema = json.RawMessage(`{"type":"object"}`)
	bound, err := base.BindCapturedOwner(proxy, captured)
	if err != nil || !bound.dshTodoNumberCompatibility {
		t.Fatalf("capture lost exact mode: %v", err)
	}
	bound, err = prepareTool(bound)
	if err != nil || validateRegisteredToolArguments(bound, json.RawMessage(todoNumberOverflowRaw)) != nil {
		t.Fatalf("bound acceptance: %v", err)
	}
	changed := proxy
	changed.Schema.InputSchema = json.RawMessage(strings.Replace(dshTodoArgumentFixtureSchema, `"additionalProperties":true`, `"additionalProperties":false`, 1))
	if _, err := base.BindCapturedOwner(changed, captured); err == nil {
		t.Fatal("changed contract borrowed mode")
	}
	if _, err := base.BindCapturedOwner(proxy, definition); err == nil {
		t.Fatal("plain fields forged capture")
	}
	if err := owner.Dispose(t.Context()); err != nil {
		t.Fatal(err)
	}
	replacement := definition
	replacement.dshTodoNumberCompatibility = false
	if _, err := h.RegisterTool(t.Context(), "todo-number-new", replacement); err != nil {
		t.Fatal(err)
	}
	if _, err := base.BindCapturedOwner(proxy, captured); err == nil {
		t.Fatal("stale capture rebound")
	}
	current, found, err := base.Resolve(t.Context(), agent, definition.Schema.Name)
	if err != nil || !found {
		t.Fatal(err)
	}
	bound, err = base.BindCapturedOwner(WithDSHTodoNumberCompatibility(proxy), current)
	if err != nil || bound.dshTodoNumberCompatibility {
		t.Fatalf("proxy added mode to legacy capture: %v", err)
	}
}

func TestDSHTodoNumberSchedulingAndExecutionShareAdmission(t *testing.T) {
	for _, marked := range []bool{false, true} {
		t.Run(fmt.Sprint(marked), func(t *testing.T) {
			h := dshTodoArgumentHarness(t)
			agent, err := h.Agents().Create(t.Context(), AgentOptions{ID: "todo-number-classifier"})
			if err != nil {
				t.Fatal(err)
			}
			tool := dshTodoArgumentTool()
			tool.ArgumentErrorMode = ArgumentErrorsLegacy
			body, classifier := 0, 0
			var bodyRaw, classifierRaw string
			tool.Execute = func(_ context.Context, raw json.RawMessage) (ToolResult, error) {
				body++
				bodyRaw = string(raw)
				return ToolResult{}, nil
			}
			tool.IsConcurrencySafe = func(_ context.Context, r ToolRequest) bool {
				classifier++
				classifierRaw = string(r.Arguments)
				return true
			}
			if marked {
				tool = WithDSHTodoNumberCompatibility(tool)
			}
			if _, err := h.RegisterTool(t.Context(), "todo-number-classifier", tool); err != nil {
				t.Fatal(err)
			}
			request := ToolRequest{Agent: agent, Name: tool.Schema.Name, Arguments: json.RawMessage(todoNumberOverflowRaw)}
			wantMode := ExecutionSerial
			if marked {
				wantMode = ExecutionParallel
			}
			if got := h.Tools().ExecutionMode(t.Context(), request); got != wantMode {
				t.Fatalf("classifier mode=%s", got)
			}
			out, err := h.Tools().Execute(t.Context(), request)
			if err != nil || out.Result.IsError == marked || string(out.Request.Arguments) != todoNumberOverflowRaw {
				t.Fatalf("execute=%+v %v", out, err)
			}
			want := 0
			if marked {
				want = 1
			}
			if body != want || classifier != want || marked && (bodyRaw != todoNumberOverflowRaw || classifierRaw != todoNumberOverflowRaw) {
				t.Fatalf("body=%d classifier=%d", body, classifier)
			}
			request.Arguments = json.RawMessage(`{"todos":[],"extra":1e309}`)
			if h.Tools().ExecutionMode(t.Context(), request) != ExecutionSerial || classifier != want {
				t.Fatal("surviving overflow reached classifier")
			}
		})
	}
}

func TestDSHTodoNumberCapturedDispatchKeepsRawAndOwner(t *testing.T) {
	h := dshTodoArgumentHarness(t)
	bodies, replacements := 0, 0
	native := WithDSHTodoNumberCompatibility(dshTodoArgumentTool())
	native.ArgumentErrorMode = ArgumentErrorsLegacy
	native.Execute = func(_ context.Context, raw json.RawMessage) (ToolResult, error) {
		bodies++
		if string(raw) != todoNumberOverflowRaw {
			t.Error("captured body lost original raw")
		}
		return ToolResult{}, nil
	}
	owner, err := h.RegisterTool(t.Context(), "todo-number-native", native)
	if err != nil {
		t.Fatal(err)
	}
	var base ToolCatalogBase
	var capture Tool
	var savedCtx context.Context
	var savedRequest ToolRequest
	agent, err := h.Agents().Create(t.Context(), AgentOptions{ID: "todo-number-dispatch", Configure: func(_ context.Context, run *point.Run, _ *Agent) error {
		_, err := h.Tools().RegisterCatalogProvider(run, "todo-number-presenter", ToolCatalogProvider{Authoritative: true, Bind: func(b ToolCatalogBase) { base = b }, Schemas: func(context.Context, *Agent) ([]ToolSchema, error) { return []ToolSchema{native.Schema}, nil }, Resolve: func(ctx context.Context, agent *Agent, name string) (Tool, bool, error) {
			if name != native.Schema.Name {
				return Tool{}, false, nil
			}
			if capture.dispatchCapture == nil {
				var found bool
				var err error
				capture, found, err = base.Resolve(ctx, agent, name)
				if err != nil || !found {
					return Tool{}, found, err
				}
			}
			proxy := Tool{Schema: native.Schema, ExecuteRequest: func(ctx context.Context, request ToolRequest) (ToolResult, error) {
				savedCtx, savedRequest = ctx, request
				// Public edits do not alter the immutable native mode or schema.
				captured := capture
				captured.dshTodoNumberCompatibility = false
				captured.Schema.InputSchema = json.RawMessage(`{"type":"object"}`)
				return base.DispatchCaptured(ctx, request, captured)
			}}
			bound, err := base.BindCapturedOwner(proxy, capture)
			return bound, err == nil, err
		}})
		return err
	}})
	if err != nil {
		t.Fatal(err)
	}
	request := ToolRequest{Agent: agent, Name: native.Schema.Name, Arguments: json.RawMessage(todoNumberOverflowRaw)}
	out, err := h.Tools().Execute(t.Context(), request)
	if err != nil || out.Result.IsError || bodies != 1 || string(out.Request.Arguments) != todoNumberOverflowRaw {
		t.Fatalf("captured execution=%+v %v bodies=%d", out, err, bodies)
	}
	if _, err := base.DispatchCaptured(savedCtx, savedRequest, capture); err == nil {
		t.Fatal("expired body admission reused")
	}
	if err := owner.Dispose(t.Context()); err != nil {
		t.Fatal(err)
	}
	replacement := native
	replacement.dshTodoNumberCompatibility = false
	replacement.Execute = func(context.Context, json.RawMessage) (ToolResult, error) { replacements++; return ToolResult{}, nil }
	if _, err := h.RegisterTool(t.Context(), "todo-number-replacement", replacement); err != nil {
		t.Fatal(err)
	}
	out, err = h.Tools().Execute(t.Context(), request)
	if err == nil && !out.Result.IsError || bodies != 1 || replacements != 0 {
		t.Fatalf("stale capture rebound: %+v %v bodies=%d replacements=%d", out, err, bodies, replacements)
	}
}

func todoNumberDiagnosticPrepared(t *testing.T) Tool {
	t.Helper()
	tool, err := prepareTool(WithDSHTodoNumberCompatibility(dshTodoArgumentTool()))
	if err != nil {
		t.Fatal(err)
	}
	return tool
}

func TestDSHTodoNumberFailureProjectionProvenanceAndValidation(t *testing.T) {
	tool := todoNumberDiagnosticPrepared(t)
	raw := json.RawMessage(`{"todos":[{"content":1e309,"content":null,"status":"pending"}]}`)
	original := validateToolArguments(tool.inputValidator, raw)
	if overflow, ok := original.(*json.UnmarshalTypeError); !ok || overflow.Type != reflect.TypeFor[float64]() {
		t.Fatalf("actual validator error=%T %v", original, original)
	}
	if got := retryDSHTodoNumberArguments(tool, raw, original); got != original {
		t.Fatal("schema-invalid last value lost original error identity")
	}
	result, ok := projectRegisteredToolArgumentFailure(tool, raw, original)
	if !ok {
		t.Fatal("direct overflow did not project")
	}
	requireDSHTodoArgumentResult(t, result, `invalid arguments: "todos[0].content" must be a string`)
	for _, other := range []error{nil, errors.New(original.Error()), fmt.Errorf("wrapped: %w", original), &json.UnmarshalTypeError{Type: reflect.TypeFor[string]()}, &json.SyntaxError{}, (*json.UnmarshalTypeError)(nil)} {
		if result, ok := projectRegisteredToolArgumentFailure(tool, raw, other); ok || !reflect.DeepEqual(result, ToolResult{}) {
			t.Fatalf("non-direct error projected: %T %+v", other, result)
		}
	}
	for _, gate := range []string{"unmarked", "legacy", "flat", "no-validator", "accepting-validator"} {
		t.Run(gate, func(t *testing.T) {
			candidate := tool
			switch gate {
			case "unmarked":
				candidate.dshTodoNumberCompatibility = false
			case "legacy":
				candidate.argumentErrorProjection = nil
			case "flat":
				candidate.argumentErrorProjection = &dshArgumentErrorProjection{flat: &dshFlatArgumentSchema{}}
			case "no-validator":
				candidate.inputValidator = nil
			case "accepting-validator":
				var err error
				candidate.inputValidator, err = compileToolInputSchema("accepting", json.RawMessage(`{"type":"object"}`))
				if err != nil {
					t.Fatal(err)
				}
			}
			if result, ok := projectRegisteredToolArgumentFailure(candidate, raw, original); ok || !reflect.DeepEqual(result, ToolResult{}) {
				t.Fatalf("gate acquired projection: %+v", result)
			}
		})
	}
	// Admission belongs exclusively to the existing retry, even when called
	// directly with a genuine original decode error from an ultimately valid view.
	valid := json.RawMessage(todoNumberOverflowRaw)
	if result, ok := projectRegisteredToolArgumentFailure(tool, valid, validateToolArguments(tool.inputValidator, valid)); ok || !reflect.DeepEqual(result, ToolResult{}) {
		t.Fatal("passive projection synthesized failure for valid final values")
	}
}

func TestDSHTodoNumberFailureProjectionKeepsRepresentationBoundaries(t *testing.T) {
	tool := todoNumberDiagnosticPrepared(t)
	for _, raw := range []string{
		`{"todos":[{"content":1e309,"content":null,"status":"pending"}],"extra":1e309}`,
		`{"todos":null,"extra":1e309}`,
		`{"todos":1e309,"todos":null,"extra":-0}`,
		`{"todos":1e309,"todos":null,"extra":-1e-999}`,
		`{"todos":1e309,"todos":null,"extra":[{"n":-0}]}`,
		`{"todos":[{"content":"\ud800","content":1e309,"content":null,"status":"pending"}]}`,
		`{"todos":1e309,"todos":null,"extra":{"\ud800":0,"\ud800":1}}`,
		`{"todos":1e309,"todos":null,"extra":"` + string([]byte{0xff}) + `"}`,
	} {
		original := validateToolArguments(tool.inputValidator, json.RawMessage(raw))
		if overflow, ok := original.(*json.UnmarshalTypeError); !ok || overflow.Type != reflect.TypeFor[float64]() {
			t.Fatalf("control lacks native overflow: %T %v", original, original)
		}
		if got := retryDSHTodoNumberArguments(tool, json.RawMessage(raw), original); got != original {
			t.Fatal("representation refusal lost original error")
		}
		if result, ok := projectRegisteredToolArgumentFailure(tool, json.RawMessage(raw), original); ok || !reflect.DeepEqual(result, ToolResult{}) {
			t.Fatalf("representation boundary laundered: %q %+v", raw, result)
		}
	}
}

func TestDSHTodoNumberFailureProjectionBudgetBoundaries(t *testing.T) {
	tool := todoNumberDiagnosticPrepared(t)
	const prefix = `{"todos":1e309,`
	const base = `{"todos":1e309,"todos":null}`
	withExtra := func(value string) string { return strings.TrimSuffix(base, "}") + `,"extra":` + value + `}` }
	byteLimit := base + strings.Repeat(" ", dshTodoArgumentBytes-len(base))
	// Base has six tokens; the extra member contributes key, array pair and n values.
	tokenLimit := withExtra("[" + strings.Repeat("0,", dshTodoArgumentTokens-10) + "0]")
	depthLimit := withExtra(strings.Repeat("[", dshTodoArgumentDepth-1) + "0" + strings.Repeat("]", dshTodoArgumentDepth-1))
	cases := []dshTodoBudgetCase{
		{"byte-at", byteLimit, true}, {"byte-over", byteLimit + " ", false},
		{"token-at", tokenLimit, true}, {"token-over", strings.Replace(tokenLimit, `,"extra":[`, `,"extra":[0,`, 1), false},
		{"depth-at", depthLimit, true}, {"depth-over", withExtra(strings.Repeat("[", dshTodoArgumentDepth) + "0" + strings.Repeat("]", dshTodoArgumentDepth)), false},
		{"overwritten-depth", `{"todos":` + strings.Repeat("[", dshTodoArgumentDepth) + "1e309" + strings.Repeat("]", dshTodoArgumentDepth) + `,"todos":null}`, false},
		{"overwritten-tokens", strings.TrimSuffix(tokenLimit, "}") + `,"extra":0}`, false},
	}
	for _, tc := range dshTodoArgumentBudgetCases() {
		if strings.HasPrefix(tc.name, "violations_") || strings.Contains(tc.name, "message_bytes_") {
			tc.raw = prefix + strings.TrimPrefix(tc.raw, "{")
			cases = append(cases, tc)
		}
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			raw := json.RawMessage(tc.raw)
			original := validateToolArguments(tool.inputValidator, raw)
			if overflow, ok := original.(*json.UnmarshalTypeError); !ok || overflow.Type != reflect.TypeFor[float64]() {
				t.Fatalf("budget fixture lacks overflow: %T %v", original, original)
			}
			if got := retryDSHTodoNumberArguments(tool, raw, original); got != original {
				t.Fatal("budget fixture changed admission/error identity")
			}
			result, projected := projectRegisteredToolArgumentFailure(tool, raw, original)
			if projected != tc.project || string(raw) != tc.raw {
				t.Fatalf("project=%v want=%v or raw mutated", projected, tc.project)
			}
			if !projected {
				if !reflect.DeepEqual(result, ToolResult{}) {
					t.Fatal("budget exhaustion returned partial diagnostics")
				}
				return
			}
			var meta struct{ Message string }
			if err := json.Unmarshal(result.Meta, &meta); err != nil {
				t.Fatal(err)
			}
			requireDSHTodoArgumentResult(t, result, meta.Message)
			if strings.Contains(tc.name, "message_bytes_") {
				want := dshTodoMessageBytes
				if strings.HasSuffix(tc.name, "-1") {
					want--
				}
				if len(meta.Message) != want {
					t.Fatalf("message bytes=%d want=%d", len(meta.Message), want)
				}
			} else if strings.HasPrefix(tc.name, "violations_") {
				want := dshTodoViolationLimit
				if strings.HasSuffix(tc.name, "-1") {
					want--
				}
				if got := strings.Count(meta.Message, "must be an object"); got != want {
					t.Fatalf("violations=%d want=%d", got, want)
				}
			} else {
				requireDSHTodoArgumentResult(t, result, `invalid arguments: "todos" must be an array`)
			}
		})
	}
}

func TestDSHTodoNumberFailureProjectionRegistryAndCapturedDispatch(t *testing.T) {
	h := dshTodoArgumentHarness(t)
	body, classifier, approvals, guards := 0, 0, 0, 0
	tool := WithDSHTodoNumberCompatibility(dshTodoArgumentTool())
	tool.RequiresApproval = true
	tool.Execute = func(context.Context, json.RawMessage) (ToolResult, error) { body++; return ToolResult{}, nil }
	tool.IsConcurrencySafe = func(context.Context, ToolRequest) bool { classifier++; return true }
	owner, err := h.RegisterTool(t.Context(), "todo-passive-native", tool)
	if err != nil {
		t.Fatal(err)
	}
	agent, err := h.Agents().Create(t.Context(), AgentOptions{ID: "todo-passive", Configure: func(_ context.Context, run *point.Run, _ *Agent) error {
		if _, err := h.Tools().Guard(run, func(context.Context, ToolRequest) error { guards++; return nil }); err != nil {
			return err
		}
		_, err := h.Approvals().Register(run, func(context.Context, ApprovalRequest) (ApprovalOutcome, error) {
			approvals++
			return ApprovalAllowedOnce, nil
		})
		return err
	}})
	if err != nil {
		t.Fatal(err)
	}
	base := ToolCatalogBase{registry: h.Tools(), scope: h.Runtime().Root(), generation: "todo-passive-presenter"}
	captured, found, err := base.Resolve(t.Context(), agent, tool.Schema.Name)
	if err != nil || !found {
		t.Fatalf("capture=%v %v", found, err)
	}
	for _, tc := range []struct{ name, raw, message string }{
		{`final-content-null`, `{"todos":[{"content":1e309,"content":null,"status":"pending"}]}`, `invalid arguments: "todos[0].content" must be a string`},
		{`final-status-enum`, `{"todos":[{"content":"kept","status":1e309,"status":"doing"}]}`, `invalid arguments: "todos[0].status" must be one of ["pending","in_progress","completed"]`},
		{`unknown-member`, `{"todos":[{"content":1e309,"content":"kept","status":"pending","extra":1}]}`, `invalid arguments: "todos[0].extra" is not a declared property (additionalProperties: false)`},
		{`root-array`, `[{"x":1e309,"x":0}]`, `invalid arguments: "arguments" must be an object`},
		{`final-todos-null`, `{"todos":1e309,"todos":null}`, `invalid arguments: "todos" must be an array`},
		{`final-item-null`, `{"todos":1e309,"todos":[null]}`, `invalid arguments: "todos[0]" must be an object`},
		{`ordered-errors`, `{"todos":[{"zzz":1e309,"aaa":2,"zzz":0,"10":1,"2":1,"content":null,"status":"doing"}]}`, `invalid arguments: "todos[0].content" must be a string; "todos[0].status" must be one of ["pending","in_progress","completed"]; "todos[0].2" is not a declared property (additionalProperties: false); "todos[0].10" is not a declared property (additionalProperties: false); "todos[0].zzz" is not a declared property (additionalProperties: false); "todos[0].aaa" is not a declared property (additionalProperties: false)`},
		{`missing-property-index-order`, `{"todos":1e309,"todos":[{"zzz":0,"content":null},{}]}`, `invalid arguments: missing required property "todos[0].status"; "todos[0].content" must be a string; "todos[0].zzz" is not a declared property (additionalProperties: false); missing required property "todos[1].content"; missing required property "todos[1].status"`},
		{`escaped-duplicate`, `{"todos":[{"content":1e309,"con\u0074ent":null,"status":"pending"}]}`, `invalid arguments: "todos[0].content" must be a string`},
		{`schema-before-domain`, `{"todos":1e309,"todos":[{"content":" ","status":"pending"},null]}`, `invalid arguments: "todos[1]" must be an object`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			request := ToolRequest{Agent: agent, Name: tool.Schema.Name, Arguments: json.RawMessage(tc.raw)}
			if h.Tools().ExecutionMode(t.Context(), request) != ExecutionSerial || classifier != 0 {
				t.Fatal("rejected call reached classifier")
			}
			out, err := h.Tools().Execute(t.Context(), request)
			if err != nil || string(out.Request.Arguments) != tc.raw {
				t.Fatalf("registry raw audit: %+v %v", out, err)
			}
			requireDSHTodoArgumentResult(t, out.Result, tc.message)
			// Internal admission fixture reaches the captured rejection branch;
			// official Native/PTC integration is covered in the plugins suite.
			request.executionToken = ToolExecutionToken{id: h.Tools().nextExecution.Add(1)}
			ctx := context.WithValue(t.Context(), toolExecutionContextKey{}, request.executionToken)
			ctx, retire := withToolBodyAdmission(ctx, request)
			result, err := base.DispatchCaptured(ctx, request, captured)
			retire()
			if err != nil || string(request.Arguments) != tc.raw {
				t.Fatalf("captured rejection/raw: %+v %v", result, err)
			}
			requireDSHTodoArgumentResult(t, result, tc.message)
			if body != 0 || approvals != 0 || guards != 0 || classifier != 0 {
				t.Fatalf("effects body=%d approval=%d guard=%d classifier=%d", body, approvals, guards, classifier)
			}
		})
	}
	bound, err := base.BindCapturedOwner(tool, captured)
	if err != nil {
		t.Fatal(err)
	}
	bound, err = prepareTool(bound)
	if err != nil {
		t.Fatal(err)
	}
	raw := json.RawMessage(`{"todos":1e309,"todos":null}`)
	original := validateToolArguments(bound.inputValidator, raw)
	if err := owner.Dispose(t.Context()); err != nil {
		t.Fatal(err)
	}
	if _, ok := projectRegisteredToolArgumentFailure(bound, raw, original); ok {
		t.Fatal("expired owner projected diagnostic")
	}
	if _, err := base.BindCapturedOwner(tool, captured); err == nil {
		t.Fatal("expired capture rebound")
	}
	agent.retired.Store(true)
	out, err := h.Tools().Execute(t.Context(), ToolRequest{Agent: agent, Name: tool.Schema.Name, Arguments: raw})
	if err != nil || !out.Result.IsError || strings.Contains(string(out.Result.Meta), "INVALID_ARGS") || body != 0 || approvals != 0 || guards != 0 || classifier != 0 {
		t.Fatalf("retired caller order changed: %+v %v", out, err)
	}
}
