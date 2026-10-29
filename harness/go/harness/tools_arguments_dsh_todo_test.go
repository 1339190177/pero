package harness

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"
)

// Exact native Todo shape reviewed in proposal-v4.md (f0ce2dc817f5de400dd4f694a74d576d5637ad7747719afa9dd47e553c50f2ad).
// The public source matrix is exercised by todos/argument_errors_test.go.
const dshTodoArgumentFixtureSchema = `{"type":"object","properties":{"todos":{"type":"array","items":{"type":"object","properties":{"content":{"type":"string"},"status":{"type":"string","enum":["pending","in_progress","completed"]}},"required":["content","status"],"additionalProperties":false}}},"required":["todos"],"additionalProperties":true}`

func dshTodoArgumentTool() Tool {
	return Tool{
		Schema:            ToolSchema{Name: "todo_diagnostic", InputSchema: json.RawMessage(dshTodoArgumentFixtureSchema)},
		ArgumentErrorMode: ArgumentErrorsDSHAlpha3Todo,
		Execute:           func(context.Context, json.RawMessage) (ToolResult, error) { return ToolResult{}, nil },
	}
}

func dshTodoArgumentHarness(t *testing.T) *Harness {
	t.Helper()
	h, err := Boot(t.Context(), Options{Adapter: AdapterFunc(func(context.Context, GenerateRequest, func(StreamChunk) error) (Completion, error) {
		t.Error("diagnostic test invoked a provider")
		return Completion{}, errors.New("unexpected provider call")
	})})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		bounded := func(action func(context.Context) error) error {
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			return action(ctx)
		}
		requestErr, finalErr := bounded(h.Dispose), bounded(h.Drain)
		if finalErr == nil && requestErr == ErrHarnessDisposalInProgress {
			requestErr = nil
		}
		if err := errors.Join(requestErr, finalErr); err != nil {
			t.Errorf("Todo diagnostic fixture cleanup: %v", err)
		}
	})
	return h
}

func requireDSHTodoArgumentResult(t *testing.T, result ToolResult, message string) {
	t.Helper()
	wantMeta, err := json.Marshal(struct {
		Message string `json:"message"`
		Name    string `json:"name"`
		Code    string `json:"code"`
		Info    struct {
			Name string `json:"name"`
			Code string `json:"code"`
		} `json:"info"`
	}{message, "ToolArgsError", "INVALID_ARGS", struct {
		Name string `json:"name"`
		Code string `json:"code"`
	}{"ToolArgsError", "INVALID_ARGS"}})
	if err != nil {
		t.Fatal(err)
	}
	if !result.IsError || result.ConcludesTurn || len(result.Value) != 0 || len(result.AdditionalContexts) != 0 || string(result.Meta) != string(wantMeta) || len(result.Content) != 1 || result.Content[0].Type != BlockText || result.Content[0].Text != "Error: "+message {
		t.Fatalf("diagnostic envelope=%+v meta=%s want=%s", result, result.Meta, wantMeta)
	}
}

func TestDSHTodoArgumentDescriptorExactGrammar(t *testing.T) {
	valid := []string{
		dshTodoArgumentFixtureSchema,
		strings.TrimSuffix(dshTodoArgumentFixtureSchema, `,"additionalProperties":true}`) + `}`,
		strings.ReplaceAll(strings.ReplaceAll(dshTodoArgumentFixtureSchema, `"type":`, `"title":"","description":"annotation 😀","type":`), `"additionalProperties":true`, `"additionalProperties": true`),
	}
	for _, raw := range valid {
		tool := dshTodoArgumentTool()
		tool.Schema.InputSchema = json.RawMessage(raw)
		prepared, err := prepareTool(tool)
		if err != nil || prepared.argumentErrorProjection == nil || !prepared.argumentErrorProjection.todo {
			t.Fatalf("supported exact descriptor: %v", err)
		}
		tool.Schema.InputSchema[0] = '!'
		if string(prepared.Schema.InputSchema) != raw {
			t.Fatal("prepared descriptor retained caller-owned schema bytes")
		}
	}
	change := func(old, replacement string) string {
		if !strings.Contains(dshTodoArgumentFixtureSchema, old) {
			t.Fatalf("missing mutation anchor %q", old)
		}
		return strings.Replace(dshTodoArgumentFixtureSchema, old, replacement, 1)
	}
	invalid := map[string]string{
		"closed root":             change(`"additionalProperties":true`, `"additionalProperties":false`),
		"root schema openness":    change(`"additionalProperties":true`, `"additionalProperties":{}`),
		"null root openness":      change(`"additionalProperties":true`, `"additionalProperties":null`),
		"open item":               change(`"additionalProperties":false`, `"additionalProperties":true`),
		"absent item openness":    change(`,"additionalProperties":false`, ``),
		"null root required":      change(`"required":["todos"]`, `"required":null`),
		"missing root required":   change(`,"required":["todos"]`, ``),
		"duplicate root required": change(`"required":["todos"]`, `"required":["todos","todos"]`),
		"extra root required":     change(`"required":["todos"]`, `"required":["todos","other"]`),
		"null item required":      change(`"required":["content","status"]`, `"required":null`),
		"reverse item required":   change(`"required":["content","status"]`, `"required":["status","content"]`),
		"duplicate item required": change(`"required":["content","status"]`, `"required":["content","content","status"]`),
		"missing item required":   change(`,"required":["content","status"]`, ``),
		"required null member":    change(`"required":["content","status"]`, `"required":["content",null]`),
		"missing property":        change(`"content":{"type":"string"},`, ``),
		"extra property":          change(`"content":{"type":"string"}`, `"content":{"type":"string"},"extra":{"type":"string"}`),
		"extra root array":        change(`"properties":{"todos":`, `"properties":{"other":{"type":"array"},"todos":`),
		"reverse properties":      change(`"content":{"type":"string"},"status":{"type":"string","enum":["pending","in_progress","completed"]}`, `"status":{"type":"string","enum":["pending","in_progress","completed"]},"content":{"type":"string"}`),
		"content enum":            change(`"content":{"type":"string"}`, `"content":{"type":"string","enum":["x"]}`),
		"nonstring leaf":          change(`"content":{"type":"string"}`, `"content":{"type":"number"}`),
		"nested leaf":             change(`"content":{"type":"string"}`, `"content":{"type":"object","properties":{}}`),
		"missing enum":            change(`,"enum":["pending","in_progress","completed"]`, ``),
		"reverse enum":            change(`"enum":["pending","in_progress","completed"]`, `"enum":["completed","in_progress","pending"]`),
		"duplicate enum":          change(`"enum":["pending","in_progress","completed"]`, `"enum":["pending","pending","in_progress","completed"]`),
		"enum wrong type":         change(`"enum":["pending","in_progress","completed"]`, `"enum":["pending",1,"completed"]`),
		"extra root keyword":      change(`"type":"object"`, `"type":"object","minProperties":0`),
		"extra array keyword":     change(`"type":"array"`, `"type":"array","minItems":0`),
		"extra item keyword":      change(`"required":["content","status"]`, `"minProperties":0,"required":["content","status"]`),
		"extra content keyword":   change(`"content":{"type":"string"}`, `"content":{"type":"string","minLength":0}`),
		"extra status keyword":    change(`"status":{"type":"string"`, `"status":{"type":"string","minLength":0`),
		"null properties":         change(`"properties":{"todos":`, `"properties":null,"unused":{"todos":`),
		"null items":              change(`"items":{"type":"object"`, `"items":null,"unused":{"type":"object"`),
		"tuple items":             change(`"items":{"type":"object"`, `"items":[],"unused":{"type":"object"`),
		"array enum":              change(`"type":"array"`, `"type":"array","enum":[[]]`),
		"null enum":               change(`"enum":["pending","in_progress","completed"]`, `"enum":null`),
		"empty enum":              change(`"enum":["pending","in_progress","completed"]`, `"enum":[]`),
		"case leaf keyword":       change(`"content":{"type":"string"}`, `"content":{"Type":"string"}`),
		"case array type":         change(`"type":"array"`, `"type":"ARRAY"`),
		"null leaf type":          change(`"content":{"type":"string"}`, `"content":{"type":null}`),
		"duplicate root keyword":  change(`"type":"object"`, `"type":"object","type":"object"`),
		"escaped root keyword":    change(`"type":"object"`, `"type":"object","ty\u0070e":"object"`),
		"case root keyword":       change(`"type":"object"`, `"Type":"object"`),
		"duplicate root property": change(`"properties":{"todos":`, `"properties":{"todos":{},"todos":`),
		"escaped root property":   change(`"properties":{"todos":`, `"properties":{"to\u0064os":{},"todos":`),
		"duplicate field":         change(`"content":{"type":"string"}`, `"content":{"type":"string"},"content":{"type":"string"}`),
		"escaped field":           change(`"content":{"type":"string"}`, `"con\u0074ent":{"type":"string"},"content":{"type":"string"}`),
		"duplicate item keyword":  change(`"required":["content","status"]`, `"required":["content","status"],"required":["content","status"]`),
		"duplicate array keyword": change(`"type":"array"`, `"type":"array","type":"array"`),
		"duplicate leaf keyword":  change(`"content":{"type":"string"}`, `"content":{"type":"string","type":"string"}`),
		"invalid schema UTF8":     change(`"type":"object"`, "\"title\":\"\xff\",\"type\":\"object\""),
		"lone schema surrogate":   change(`"type":"object"`, `"title":"\ud800","type":"object"`),
	}
	for _, name := range []string{"", "other", "todo.list", `todo\"list`, "TODOS"} {
		encoded, _ := json.Marshal(name)
		invalid["renamed root "+name] = strings.ReplaceAll(dshTodoArgumentFixtureSchema, `"todos"`, string(encoded))
	}
	for _, name := range []string{"", "other", "item.content", `item\"content`, "CONTENT"} {
		encoded, _ := json.Marshal(name)
		invalid["renamed field "+name] = strings.ReplaceAll(dshTodoArgumentFixtureSchema, `"content"`, string(encoded))
	}
	for _, anchor := range []string{`"type":"object"`, `"type":"array"`, `"items":{`, `"content":{`, `"status":{`} {
		for _, annotation := range []string{"title", "description"} {
			if strings.HasSuffix(anchor, "{") {
				invalid[anchor+annotation] = change(anchor, anchor+`"`+annotation+`":null,`)
			} else {
				invalid[anchor+annotation] = change(anchor, `"`+annotation+`":null,`+anchor)
			}
		}
	}
	for name, raw := range invalid {
		t.Run(name, func(t *testing.T) {
			if err := compileDSHTodoArgumentSchema(json.RawMessage(raw)); err == nil {
				t.Fatalf("unsupported descriptor admitted: %s", raw)
			}
			tool := dshTodoArgumentTool()
			tool.Schema.InputSchema = json.RawMessage(raw)
			_, got := prepareTool(tool)
			if got == nil {
				t.Fatal("unsupported registration admitted")
			}
			_, original := compileToolInputSchema(tool.Schema.Name, tool.Schema.InputSchema)
			if original != nil && got.Error() != original.Error() {
				t.Fatalf("descriptor displaced authoritative compiler: got=%v want=%v", got, original)
			}
			tool.ArgumentErrorMode = ArgumentErrorsLegacy
			_, legacy := prepareTool(tool)
			if fmt.Sprint(legacy) != fmt.Sprint(original) {
				t.Fatalf("legacy registration changed: %v / %v", legacy, original)
			}
		})
	}
}

func TestDSHTodoArgumentSchemaBudgetsAndValidatorPriority(t *testing.T) {
	for _, delta := range []int{-1, 0, 1} {
		raw := dshTodoArgumentFixtureSchema + strings.Repeat(" ", dshTodoSchemaBytes+delta-len(dshTodoArgumentFixtureSchema))
		tool := dshTodoArgumentTool()
		tool.Schema.InputSchema = json.RawMessage(raw)
		_, err := prepareTool(tool)
		if (err != nil) != (delta > 0) {
			t.Fatalf("schema bytes=%d: %v", len(raw), err)
		}
		tool.ArgumentErrorMode = ArgumentErrorsLegacy
		if _, err := prepareTool(tool); err != nil {
			t.Fatalf("Todo byte cap leaked into legacy: %v", err)
		}
	}
	flat := dshArgumentFixtureTool(func(context.Context, json.RawMessage) (ToolResult, error) { return ToolResult{}, nil })
	flat.Schema.InputSchema = append(flat.Schema.InputSchema, []byte(strings.Repeat(" ", dshTodoSchemaBytes))...)
	if _, err := prepareTool(flat); err != nil {
		t.Fatalf("Todo byte cap leaked into Flat: %v", err)
	}
	tool := dshTodoArgumentTool()
	tool.Schema.InputSchema = json.RawMessage("!" + strings.Repeat(" ", dshTodoSchemaBytes))
	_, err := prepareTool(tool)
	if err == nil || err.Error() != fmt.Sprintf("harness: DSH todo argument diagnostic schema exceeds %d bytes", dshTodoSchemaBytes) {
		t.Fatalf("raw-byte guard did not precede compiler: %v", err)
	}
	// This malformed schema is below the byte cap but above the token cap.
	// The original compiler must still win over descriptor/token interpretation.
	tool.Schema.InputSchema = json.RawMessage(`{"type":17,"required":[` + strings.TrimSuffix(strings.Repeat(`"x",`, dshTodoSchemaTokens), ",") + `]}`)
	_, original := compileToolInputSchema(tool.Schema.Name, tool.Schema.InputSchema)
	_, got := prepareTool(tool)
	if original == nil || got == nil || got.Error() != original.Error() {
		t.Fatalf("compiler precedence: got=%v original=%v", got, original)
	}
	// The exact descriptor grammar cannot contain 4096 distinct tokens. Exercise
	// the token boundary directly, then require every unsupported large shape to
	// fail registration rather than treating a budget pass as schema acceptance.
	for _, delta := range []int{-1, 0, 1} {
		tokens := dshTodoSchemaTokens + delta
		raw := json.RawMessage("[" + strings.TrimSuffix(strings.Repeat("0,", tokens-2), ",") + "]")
		if got := dshTodoJSONBudget(raw, dshTodoSchemaTokens, dshTodoArgumentDepth); got != (delta <= 0) {
			t.Fatalf("schema tokens=%d budget=%v", tokens, got)
		}
		if err := compileDSHTodoArgumentSchema(raw); err == nil {
			t.Fatal("token-only shape became a descriptor")
		}
		tool = dshTodoArgumentTool()
		tool.Schema.InputSchema = json.RawMessage(`{"type":"object","unknown":` + string(raw) + `}`)
		if _, err := prepareTool(tool); err == nil {
			t.Fatal("unsupported high-token object registered")
		}
	}
	tool = dshTodoArgumentTool()
	tool.ArgumentErrorMode = ArgumentErrorsDSHAlpha3Flat
	if _, err := prepareTool(tool); err == nil {
		t.Fatal("Todo changed Flat's scalar-only schema contract")
	}
}

type dshTodoBudgetCase struct {
	name, raw string
	project   bool
}

func dshTodoArgumentBudgetCases() []dshTodoBudgetCase {
	var cases []dshTodoBudgetCase
	for _, delta := range []int{-1, 0, 1} {
		bytePrefix, byteSuffix := `{"todos":null,"ignored":"`, `"}`
		cases = append(cases, dshTodoBudgetCase{fmt.Sprintf("bytes_%+d", delta), bytePrefix + strings.Repeat("x", dshTodoArgumentBytes+delta-len(bytePrefix)-len(byteSuffix)) + byteSuffix, delta <= 0})
		// Root delimiters, two keys, null, and array delimiters use seven tokens.
		n := dshTodoArgumentTokens + delta - 7
		cases = append(cases, dshTodoBudgetCase{fmt.Sprintf("tokens_%+d", delta), `{"todos":null,"ignored":[` + strings.TrimSuffix(strings.Repeat("0,", n), ",") + `]}`, delta <= 0})
		n = dshTodoArgumentDepth + delta - 1 // Root object counts as one open container.
		cases = append(cases, dshTodoBudgetCase{fmt.Sprintf("depth_%+d", delta), `{"todos":null,"ignored":` + strings.Repeat("[", n) + "0" + strings.Repeat("]", n) + `}`, delta <= 0})
		n = dshTodoViolationLimit + delta
		cases = append(cases, dshTodoBudgetCase{fmt.Sprintf("violations_%+d", delta), `{"todos":[` + strings.TrimSuffix(strings.Repeat("null,", n), ",") + `]}`, delta <= 0})
		fixed := len(dshTodoInvalidPrefix) + len(`"todos[0]." is not a declared property (additionalProperties: false)`)
		key := strings.Repeat("x", dshTodoMessageBytes+delta-fixed)
		cases = append(cases, dshTodoBudgetCase{fmt.Sprintf("message_bytes_%+d", delta), `{"todos":[{"content":"x","status":"pending","` + key + `":0}]}`, delta <= 0})
		keyBytes := dshTodoMessageBytes + delta - fixed
		key = strings.Repeat("😀", keyBytes/4) + strings.Repeat("x", keyBytes%4)
		cases = append(cases, dshTodoBudgetCase{fmt.Sprintf("utf8_message_bytes_%+d", delta), `{"todos":[{"content":"x","status":"pending","` + key + `":0}]}`, delta <= 0})
	}
	// Count overwritten raw subtrees, even though JSON.parse's final value is tiny.
	n := dshTodoArgumentTokens - 7
	cases = append(cases, dshTodoBudgetCase{"overwritten_tokens", `{"todos":null,"ignored":[` + strings.TrimSuffix(strings.Repeat("0,", n), ",") + `],"ignored":0}`, false})
	n = dshTodoArgumentDepth
	cases = append(cases, dshTodoBudgetCase{"overwritten_depth", `{"todos":null,"ignored":` + strings.Repeat("[", n) + "0" + strings.Repeat("]", n) + `,"ignored":0}`, false})
	return cases
}

func TestDSHTodoArgumentProjectionBudgetBoundariesAreAtomic(t *testing.T) {
	for _, tc := range dshTodoArgumentBudgetCases() {
		t.Run(tc.name, func(t *testing.T) {
			result, projected := projectDSHTodoArgumentFailure(json.RawMessage(tc.raw))
			if projected != tc.project {
				t.Fatalf("projection=%v want=%v", projected, tc.project)
			}
			if !projected {
				if !reflect.DeepEqual(result, ToolResult{}) {
					t.Fatal("budget exhaustion returned a partial source result")
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
			}
			if strings.HasPrefix(tc.name, "violations_") {
				want := dshTodoViolationLimit
				if strings.HasSuffix(tc.name, "-1") {
					want--
				}
				if got := strings.Count(meta.Message, "must be an object"); got != want {
					t.Fatalf("complete violations=%d want=%d", got, want)
				}
			}
		})
	}
	for _, delta := range []int{-1, 0, 1} {
		v := dshTodoViolations{}
		if !v.add("first") {
			t.Fatal("first append")
		}
		before := v.bytes
		length := dshTodoMessageBytes - len(dshTodoInvalidPrefix) - before - len("; ") + delta
		got := v.add(strings.Repeat("x", length))
		if got != (delta <= 0) {
			t.Fatalf("separator byte boundary %+d: %v", delta, got)
		}
		if !got && (v.bytes != before || len(v.messages) != 1) {
			t.Fatal("failed append partially changed diagnostics")
		}
	}
}

func TestDSHTodoArgumentPublicBudgetFallbackPreservesNativeError(t *testing.T) {
	h := dshTodoArgumentHarness(t)
	agent, err := h.Agents().Create(t.Context(), AgentOptions{ID: "todo-budget"})
	if err != nil {
		t.Fatal(err)
	}
	tool := dshTodoArgumentTool()
	bodies := 0
	tool.Execute = func(context.Context, json.RawMessage) (ToolResult, error) { bodies++; return ToolResult{}, nil }
	if _, err := h.RegisterTool(t.Context(), "todo-budget", tool); err != nil {
		t.Fatal(err)
	}
	validator, err := compileToolInputSchema(tool.Schema.Name, tool.Schema.InputSchema)
	if err != nil {
		t.Fatal(err)
	}
	cases := dshTodoArgumentBudgetCases()
	for index, raw := range []string{
		`{`, `{} {}`, `{"todos":null,"n":1e400}`, `{"todos":null,"n":-0}`, `{"todos":null,"n":-1e-999}`, `{"todos":null,"extra":[{"n":-0}]}`,
		`{"todos":[{"content":"\ud800","status":null}]}`, `{"todos":[{"\ud800":1,"content":"x","status":"pending"}]}`,
		"{\"todos\":null,\"extra\":\"\xff\"}",
	} {
		cases = append(cases, dshTodoBudgetCase{fmt.Sprintf("native_representation_%d", index), raw, false})
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			raw := json.RawMessage(tc.raw)
			original := validateToolArguments(validator, raw)
			if original == nil {
				t.Fatal("budget fixture was not rejected by original validator")
			}
			out, err := h.Tools().Execute(t.Context(), ToolRequest{Agent: agent, Name: tool.Schema.Name, Arguments: raw})
			if err != nil || !out.Result.IsError || bodies != 0 {
				t.Fatalf("admission changed: %+v %v bodies=%d", out.Result, err, bodies)
			}
			if tc.project {
				var meta struct{ Message string }
				if err := json.Unmarshal(out.Result.Meta, &meta); err != nil {
					t.Fatal(err)
				}
				requireDSHTodoArgumentResult(t, out.Result, meta.Message)
			} else if len(out.Result.Meta) != 0 || len(out.Result.Value) != 0 || len(out.Result.Content) != 1 || out.Result.Content[0].Text != "invalid tool arguments for "+tool.Schema.Name+": "+original.Error() {
				t.Fatalf("fallback replaced native error: %+v original=%v", out.Result, original)
			}
		})
	}
}

func TestDSHTodoArgumentValidCallsBypassProjectionBudgets(t *testing.T) {
	h := dshTodoArgumentHarness(t)
	agent, err := h.Agents().Create(t.Context(), AgentOptions{ID: "todo-budget-valid"})
	if err != nil {
		t.Fatal(err)
	}
	tool := dshTodoArgumentTool()
	bodies := 0
	tool.Execute = func(context.Context, json.RawMessage) (ToolResult, error) { bodies++; return ToolResult{}, nil }
	if _, err := h.RegisterTool(t.Context(), "todo-budget-valid", tool); err != nil {
		t.Fatal(err)
	}
	for _, raw := range []string{
		`{"todos":[],"ignored":"` + strings.Repeat("x", dshTodoArgumentBytes) + `"}`,
		`{"todos":[],"ignored":[` + strings.TrimSuffix(strings.Repeat("0,", dshTodoArgumentTokens), ",") + `]}`,
		`{"todos":[],"ignored":` + strings.Repeat("[", dshTodoArgumentDepth) + "0" + strings.Repeat("]", dshTodoArgumentDepth) + `}`,
		`{"todos":[],"ignored":-0}`,
	} {
		before := bodies
		out, err := h.Tools().Execute(t.Context(), ToolRequest{Agent: agent, Name: tool.Schema.Name, Arguments: json.RawMessage(raw)})
		if err != nil || out.Result.IsError || bodies != before+1 {
			t.Fatalf("projection became admission: %+v %v bodies=%d", out.Result, err, bodies)
		}
	}
}

func TestDSHTodoArgumentRepresentationFallbackAndDuplicateIdentity(t *testing.T) {
	for _, raw := range []string{
		`{`, `{} {}`, `{"todos":null,"n":1e400}`, `{"todos":null,"n":-0}`, `{"todos":null,"n":-1e-999}`, `{"todos":null,"extra":[{"n":-0}]}`,
		`{"todos":[{"content":"\ud800","status":null}]}`, `{"todos":[{"\ud800":1,"content":null,"status":null}]}`,
		"{\"todos\":null,\"extra\":\"\xff\"}",
	} {
		if got, ok := projectDSHTodoArgumentFailure(json.RawMessage(raw)); ok || !reflect.DeepEqual(got, ToolResult{}) {
			t.Fatalf("unrepresentable input projected: raw=%q result=%+v", raw, got)
		}
	}
	for _, tc := range []struct{ raw, message string }{
		{``, `missing required property "todos"`},
		{`{"todos":null,"n":-0,"n":0}`, `"todos" must be an array`},
		{`{"todos":[],"to\u0064os":[{"content":false,"content":"x","status":null}]}`, `"todos[0].status" must be a string`},
		{`{"todos":[{"content":"x","status":"pending","":1,"q\".x[0]\n":2}]}`, "\"todos[0].\" is not a declared property (additionalProperties: false); \"todos[0].q\".x[0]\n\" is not a declared property (additionalProperties: false)"},
		{`{"todos":[{"content":"\ud83d\ude00","status":null}]}`, `"todos[0].status" must be a string`},
	} {
		got, ok := projectDSHTodoArgumentFailure(json.RawMessage(tc.raw))
		if !ok {
			t.Fatalf("representable duplicate identity rejected: %s", tc.raw)
		}
		requireDSHTodoArgumentResult(t, got, dshTodoInvalidPrefix+tc.message)
	}
	if _, ok := projectDSHTodoArgumentFailure(json.RawMessage(`{"todos":[]}`)); ok {
		t.Fatal("no-violation input synthesized an error")
	}
}

func TestDSHTodoArgumentCaptureRetainsExactOwnerAndDeclarationOrder(t *testing.T) {
	h := dshTodoArgumentHarness(t)
	agent, err := h.Agents().Create(t.Context(), AgentOptions{ID: "todo-capture"})
	if err != nil {
		t.Fatal(err)
	}
	tool := dshTodoArgumentTool()
	owner, err := h.RegisterTool(t.Context(), "todo-old", tool)
	if err != nil {
		t.Fatal(err)
	}
	base := ToolCatalogBase{registry: h.Tools(), scope: h.Runtime().Root(), generation: "todo-presenter"}
	captured, ok, err := base.Resolve(t.Context(), agent, tool.Schema.Name)
	if err != nil || !ok {
		t.Fatalf("capture=%v/%v", ok, err)
	}
	proxy := tool
	proxy.ArgumentErrorMode = ArgumentErrorsLegacy
	proxy.Schema.InputSchema = json.RawMessage(strings.Replace(dshTodoArgumentFixtureSchema, `"content":{"type":"string"},"status":{"type":"string","enum":["pending","in_progress","completed"]}`, `"status":{"enum":["pending","in_progress","completed"],"type":"string"},"content":{"type":"string"}`, 1))
	unbound := proxy
	unbound.ArgumentErrorMode = ArgumentErrorsDSHAlpha3Todo
	if _, err := prepareTool(unbound); err == nil {
		t.Fatal("unbound reversed declarations accepted by Todo descriptor")
	}
	// The public capture is mutable; its private receipt owns this contract.
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
	if bound.ArgumentErrorMode != ArgumentErrorsDSHAlpha3Todo || string(bound.Schema.InputSchema) != dshTodoArgumentFixtureSchema {
		t.Fatal("schema transport changed captured mode/order")
	}
	result, ok := projectArgumentFailure(bound, json.RawMessage(`{"todos":[{"status":null,"content":null}]}`))
	if !ok {
		t.Fatal("capture lost projection")
	}
	requireDSHTodoArgumentResult(t, result, `invalid arguments: "todos[0].content" must be a string; "todos[0].status" must be a string`)
	changed := proxy
	changed.Schema.InputSchema = json.RawMessage(strings.Replace(dshTodoArgumentFixtureSchema, `"additionalProperties":true`, `"additionalProperties":false`, 1))
	if _, err := base.BindCapturedOwner(changed, captured); err == nil {
		t.Fatal("changed proxy schema inherited projection")
	}
	forged := tool
	forged.ArgumentErrorMode = ArgumentErrorsDSHAlpha3Todo
	if _, err := base.BindCapturedOwner(proxy, forged); err == nil {
		t.Fatal("forged public fields supplied a capture")
	}
	if err := owner.Dispose(t.Context()); err != nil {
		t.Fatal(err)
	}
	replacement := tool
	replacement.ArgumentErrorMode = ArgumentErrorsLegacy
	if _, err := h.RegisterTool(t.Context(), "todo-new", replacement); err != nil {
		t.Fatal(err)
	}
	if _, err := base.BindCapturedOwner(proxy, captured); err == nil {
		t.Fatal("same-name replacement revived stale capture")
	}
	if _, ok := projectArgumentFailure(bound, json.RawMessage(`{}`)); ok {
		t.Fatal("retired descriptor projected a new error")
	}
	out, err := h.Tools().Execute(t.Context(), ToolRequest{Agent: agent, Name: tool.Schema.Name, Arguments: json.RawMessage(`{"ArgumentErrorMode":2,"meta":{"ArgumentErrorMode":2},"reply":{"ArgumentErrorMode":2}}`)})
	if err != nil || !out.Result.IsError || len(out.Result.Meta) != 0 || len(out.Result.Content) != 1 || !strings.HasPrefix(out.Result.Content[0].Text, "invalid tool arguments for "+tool.Schema.Name+":") {
		t.Fatalf("replacement acquired captured mode: %+v %v", out.Result, err)
	}
	var decoded Tool
	if err := json.Unmarshal([]byte(`{"ArgumentErrorMode":2,"argumentErrorProjection":{"todo":true}}`), &decoded); err != nil || decoded.ArgumentErrorMode != ArgumentErrorsLegacy || decoded.argumentErrorProjection != nil {
		t.Fatalf("JSON forged trusted mode: %+v %v", decoded, err)
	}
}
