package todos

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/phodal/pero/harness/go/harness"
	point "github.com/phodal/pero/harness/go/runtime"
)

func todoSnapshots(session *harness.Session) []json.RawMessage {
	var result []json.RawMessage
	for _, event := range session.Events() {
		if event.Type == "todo/write" {
			result = append(result, event.Data)
		}
	}
	return result
}

func todoDomainHarness(t *testing.T) (*harness.Harness, *int) {
	t.Helper()
	calls := new(int)
	h, err := harness.Boot(t.Context(), harness.Options{Adapter: harness.AdapterFunc(func(context.Context, harness.GenerateRequest, func(harness.StreamChunk) error) (harness.Completion, error) {
		*calls++
		return harness.Completion{}, errors.New("unexpected provider execution")
	})})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		bounded := func(action func(context.Context) error) error {
			cleanup, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			return action(cleanup)
		}
		requestErr, finalErr := bounded(h.Dispose), bounded(h.Drain)
		if finalErr == nil && requestErr == harness.ErrHarnessDisposalInProgress {
			requestErr = nil
		}
		if err := errors.Join(requestErr, finalErr); err != nil {
			t.Errorf("Todo fixture final cleanup: %v", err)
		}
		if *calls != 0 {
			t.Errorf("unexpected provider calls=%d", *calls)
		}
	})
	return h, calls
}

func TestTodoPublicModeCompatibilityAndForgedOptions(t *testing.T) {
	for _, tc := range []struct {
		name string
		mode ContentNormalizationMode
		open bool
	}{
		{"Point", ContentNormalizationLegacy, false}, {"open Point", ContentNormalizationLegacy, true}, {"DSH", ContentNormalizationDSHAlpha3, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h, _ := todoDomainHarness(t)
			if _, err := Mount(t.Context(), h, Options{ContentNormalization: tc.mode, AllowAdditionalInputProperties: tc.open}); err != nil {
				t.Fatal(err)
			}
			agent, err := h.Agents().Create(t.Context(), harness.AgentOptions{ID: "todo-compat"})
			if err != nil {
				t.Fatal(err)
			}
			for _, raw := range []string{
				`{"todos":[{"content":"\ufeff","status":"pending"}]}`,
				`{"todos":[{"content":"\u0085","status":"pending"}]}`,
				`{"todos":[{"content":"\ufeff keep \ufeff","status":"pending"}],"normalization":null,"ContentNormalization":0,"meta":{"ContentNormalization":1}}`,
				`{"todos":[{"content":"\ufeff keep \ufeff","status":"pending"}],"normalization":"ecmascript-trim-v1","ContentNormalization":1}`,
			} {
				before := todoSnapshots(agent.Session())
				out, err := h.Tools().Execute(t.Context(), harness.ToolRequest{Agent: agent, Name: ToolName, Arguments: json.RawMessage(raw)})
				if err != nil {
					t.Fatal(err)
				}
				var args struct {
					Todos []Item `json:"todos"`
				}
				if err := json.Unmarshal([]byte(raw), &args); err != nil {
					t.Fatal(err)
				}
				want, _, domainErr := normalizeWithMode(args.Todos, false, tc.mode)
				schemaReject := !tc.open && strings.Contains(raw, `"normalization"`)
				if out.Result.IsError != (domainErr != nil || schemaReject) {
					t.Fatalf("raw=%s result=%+v err=%v", raw, out.Result, domainErr)
				}
				if out.Result.IsError {
					if !reflect.DeepEqual(todoSnapshots(agent.Session()), before) || len(out.Result.Value) != 0 {
						t.Fatal("rejection committed")
					}
					if !schemaReject && tc.mode == ContentNormalizationDSHAlpha3 {
						var meta map[string]string
						if json.Unmarshal(out.Result.Meta, &meta) != nil || len(meta) != 1 || meta["message"] != domainErr.Error() || len(out.Result.Content) != 1 || out.Result.Content[0].Text != "Error: "+domainErr.Error() {
							t.Fatalf("DSH plain domain envelope=%+v", out.Result)
						}
					} else if len(out.Result.Meta) != 0 {
						t.Fatalf("legacy/schema error gained metadata: %s", out.Result.Meta)
					}
					if !schemaReject && tc.mode == ContentNormalizationLegacy && (len(out.Result.Content) != 1 || out.Result.Content[0].Text != domainErr.Error()) {
						t.Fatalf("legacy domain text changed: %+v", out.Result)
					}
					continue
				}
				var value todoToolValue
				if json.Unmarshal(out.Result.Value, &value) != nil || !reflect.DeepEqual(value.Todos, want) {
					t.Fatalf("value=%s want=%+v", out.Result.Value, want)
				}
				snapshots := todoSnapshots(agent.Session())
				if len(snapshots) != len(before)+1 {
					t.Fatal("write count changed")
				}
				var expected any = writeEvent{Todos: want}
				if tc.mode == ContentNormalizationDSHAlpha3 {
					expected = normalizedWriteEvent{Todos: want, Normalization: ecmascriptTrimV1}
				}
				encoded, _ := json.Marshal(expected)
				if string(snapshots[len(snapshots)-1]) != string(encoded) {
					t.Fatalf("event=%s expected=%s", snapshots[len(snapshots)-1], encoded)
				}
				current, found, err := Current(agent.Session())
				if err != nil || !found || !reflect.DeepEqual(current, want) {
					t.Fatalf("Current=%+v/%v %v", current, found, err)
				}
			}
		})
	}
}

func TestTodoPublicSchemaRejectionKeepsOriginalAdmissionOrder(t *testing.T) {
	h, _ := todoDomainHarness(t)
	before, after, body, approvals, reasons, guards := 0, 0, 0, 0, 0, 0
	if _, err := Mount(t.Context(), h, Options{ContentNormalization: ContentNormalizationDSHAlpha3, AllowAdditionalInputProperties: true, ToolWrapper: func(tool harness.Tool) harness.Tool {
		original := tool.ExecuteRequest
		tool.ExecuteRequest = func(ctx context.Context, request harness.ToolRequest) (harness.ToolResult, error) {
			body++
			return original(ctx, request)
		}
		tool.RequiresApproval = true
		tool.ApprovalReason = func(harness.ToolRequest) string { reasons++; return "synthetic Todo admission" }
		return tool
	}}); err != nil {
		t.Fatal(err)
	}
	agent, err := h.Agents().Create(t.Context(), harness.AgentOptions{ID: "todo-admission", Configure: func(_ context.Context, run *point.Run, _ *harness.Agent) error {
		if _, err := h.Tools().Before.Use(run, func(_ context.Context, request harness.ToolRequest, next point.Next[harness.ToolRequest]) (harness.ToolRequest, error) {
			before++
			return next(request)
		}); err != nil {
			return err
		}
		if _, err := h.Tools().After.Use(run, func(_ context.Context, out harness.ToolOutcome, next point.Next[harness.ToolOutcome]) (harness.ToolOutcome, error) {
			after++
			return next(out)
		}); err != nil {
			return err
		}
		if _, err := h.Tools().Guard(run, func(context.Context, harness.ToolRequest) error { guards++; return nil }); err != nil {
			return err
		}
		_, err := h.Approvals().Register(run, func(context.Context, harness.ApprovalRequest) (harness.ApprovalOutcome, error) {
			approvals++
			return harness.ApprovalAllowedOnce, nil
		})
		return err
	}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := agent.Session().Append("turn/start", map[string]int{"turn": 1}); err != nil {
		t.Fatal(err)
	}
	cases := []string{`null`, `[]`, `{}`, `{"TODOS":[]}`, `{"todos":null}`, `{"todos":false}`, `{"todos":1}`, `{"todos":"x"}`, `{"todos":{}}`, `{"todos":[null]}`, `{"todos":[false]}`, `{"todos":[1]}`, `{"todos":[[]]}`, `{"todos":["x"]}`, `{"todos":[{}]}`, `{"todos":[{"content":null,"status":"pending"}]}`, `{"todos":[{"content":"x","status":false}]}`, `{"todos":[{"content":"x","status":"doing"}]}`, `{"todos":[{"content":"x","status":"pending","extra":true}]}`, `{"todos":[{"CONTENT":"x","status":"pending"}]}`}
	for _, raw := range cases {
		out, err := h.Tools().Execute(t.Context(), harness.ToolRequest{Agent: agent, Name: ToolName, Arguments: json.RawMessage(raw)})
		if err != nil || !out.Result.IsError || len(out.Result.Meta) != 0 || len(out.Result.Content) != 1 || !strings.HasPrefix(out.Result.Content[0].Text, "invalid tool arguments for todo_write:") {
			t.Fatalf("schema diagnostic contract=%+v %v", out.Result, err)
		}
	}
	if before != len(cases) || after != 0 || body != 0 || approvals != 0 || reasons != 0 || guards != 0 || len(todoSnapshots(agent.Session())) != 0 {
		t.Fatalf("schema effects: Before=%d After=%d body=%d approval=%d reason=%d guard=%d", before, after, body, approvals, reasons, guards)
	}
	out, err := h.Tools().Execute(t.Context(), harness.ToolRequest{Agent: agent, Name: ToolName, Arguments: json.RawMessage(`{"todos":[{"content":"\ufeff","status":"pending"}]}`)})
	if err != nil || !out.Result.IsError || before != len(cases)+1 || after != 1 || body != 1 || approvals != 1 || reasons != 1 || guards != 1 || len(todoSnapshots(agent.Session())) != 0 {
		t.Fatalf("domain admission moved: %+v %v counters=%d/%d/%d/%d/%d/%d", out.Result, err, before, after, body, approvals, reasons, guards)
	}
}

func TestTodoCommitBoundaryRetainsSuccessfulAppend(t *testing.T) {
	for _, phase := range []string{"pre-cancel", "deny", "retire-agent", "guard-failure", "body-cancel", "after-cancel", "after-failure"} {
		t.Run(phase, func(t *testing.T) {
			h, _ := todoDomainHarness(t)
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			body := 0
			if _, err := Mount(t.Context(), h, Options{ContentNormalization: ContentNormalizationDSHAlpha3, ToolWrapper: func(tool harness.Tool) harness.Tool {
				original := tool.ExecuteRequest
				tool.ExecuteRequest = func(ctx context.Context, request harness.ToolRequest) (harness.ToolResult, error) {
					body++
					// Cancellation after admission has no new transactional power:
					// the existing Todo body ignores ctx and Append is non-contextual.
					if phase == "body-cancel" {
						cancel()
					}
					return original(ctx, request)
				}
				return tool
			}}); err != nil {
				t.Fatal(err)
			}
			sentinel := errors.New("SAFE_AFTER_FAILURE")
			agent, err := h.Agents().Create(t.Context(), harness.AgentOptions{ID: "todo-commit", Configure: func(_ context.Context, run *point.Run, _ *harness.Agent) error {
				if _, err := h.Tools().Before.Use(run, func(_ context.Context, request harness.ToolRequest, next point.Next[harness.ToolRequest]) (harness.ToolRequest, error) {
					if phase == "deny" {
						request.DenyReason = "synthetic deny"
					}
					return next(request)
				}); err != nil {
					return err
				}
				if _, err := h.Tools().Guard(run, func(context.Context, harness.ToolRequest) error {
					if phase == "guard-failure" {
						return errors.New("synthetic guard failure")
					}
					return nil
				}); err != nil {
					return err
				}
				_, err := h.Tools().After.Use(run, func(_ context.Context, out harness.ToolOutcome, next point.Next[harness.ToolOutcome]) (harness.ToolOutcome, error) {
					out, err := next(out)
					if phase == "after-cancel" {
						cancel()
					}
					if phase == "after-failure" {
						return out, sentinel
					}
					return out, err
				})
				return err
			}})
			if err != nil {
				t.Fatal(err)
			}
			if phase == "pre-cancel" {
				cancel()
			}
			if phase == "retire-agent" {
				if err := h.Agents().RemoveExact(t.Context(), agent); err != nil {
					t.Fatal(err)
				}
			}
			out, err := h.Tools().Execute(ctx, harness.ToolRequest{Agent: agent, Name: ToolName, Arguments: json.RawMessage(`{"todos":[{"content":"\u0085","status":"pending"}]}`)})
			committed := phase == "body-cancel" || phase == "after-cancel" || phase == "after-failure"
			want := 0
			if committed {
				want = 1
			}
			if body != want || len(todoSnapshots(agent.Session())) != want {
				t.Fatalf("phase=%s body=%d commits=%d out=%+v err=%v", phase, body, len(todoSnapshots(agent.Session())), out, err)
			}
			if phase == "after-failure" && !errors.Is(err, sentinel) {
				t.Fatalf("lost After failure: %v", err)
			}
			if phase == "pre-cancel" || phase == "deny" || phase == "retire-agent" || phase == "guard-failure" || phase == "body-cancel" || phase == "after-failure" {
				if !out.Result.IsError {
					t.Fatalf("failure became success: %+v", out)
				}
			}
			if committed {
				list, found, err := Current(agent.Session())
				if err != nil || !found || len(list) != 1 || list[0].Content != "\u0085" {
					t.Fatalf("successful append lost: %+v/%v %v", list, found, err)
				}
			}
		})
	}
}

func TestTodoCapturedOwnerReplacementDoesNotTransferMode(t *testing.T) {
	h, _ := todoDomainHarness(t)
	owner, err := Mount(t.Context(), h, Options{ContentNormalization: ContentNormalizationDSHAlpha3})
	if err != nil {
		t.Fatal(err)
	}
	agent, err := h.Agents().Create(t.Context(), harness.AgentOptions{ID: "todo-owner"})
	if err != nil {
		t.Fatal(err)
	}
	captured, ok := h.Tools().Resolve(agent.Scope(), ToolName)
	if !ok || captured.ErrorOutputMode != harness.ErrorOutputDSHAlpha3 {
		t.Fatal("DSH capture missing")
	}
	if err := owner.Dispose(t.Context()); err != nil {
		t.Fatal(err)
	}
	if _, err := Mount(t.Context(), h, Options{}); err != nil {
		t.Fatal(err)
	}
	if _, err := captured.ExecuteRequest(t.Context(), harness.ToolRequest{Agent: agent, Name: ToolName, Arguments: json.RawMessage(`{"todos":[]}`)}); !errors.Is(err, harness.ErrToolOwnerRetired) {
		t.Fatalf("stale owner dispatched: %v", err)
	}
	if len(todoSnapshots(agent.Session())) != 0 {
		t.Fatal("retired body committed")
	}
	out, err := h.Tools().Execute(t.Context(), harness.ToolRequest{Agent: agent, Name: ToolName, Arguments: json.RawMessage(`{"todos":[{"content":"\ufeff","status":"pending"}]}`)})
	if err != nil || out.Result.IsError || out.Request.ErrorOutputMode() != harness.ErrorOutputLegacy {
		t.Fatalf("replacement inherited mode: %+v %v", out, err)
	}
	if snapshots := todoSnapshots(agent.Session()); len(snapshots) != 1 || strings.Contains(string(snapshots[0]), "normalization") {
		t.Fatalf("replacement inherited record format: %s", snapshots)
	}
}

// Plain Go public invocation has no JS lossless-object precheck. The official
// composed bridge has that precheck and is covered separately by its fixture.
func TestTodoDirectNativeIgnoredNegativeZeroRemainsExplicitResidual(t *testing.T) {
	for _, mode := range []ContentNormalizationMode{ContentNormalizationLegacy, ContentNormalizationDSHAlpha3} {
		h, _ := todoDomainHarness(t)
		if _, err := Mount(t.Context(), h, Options{ContentNormalization: mode, AllowAdditionalInputProperties: true}); err != nil {
			t.Fatal(err)
		}
		agent, err := h.Agents().Create(t.Context(), harness.AgentOptions{ID: "todo-native-number"})
		if err != nil {
			t.Fatal(err)
		}
		out, err := h.Tools().Execute(t.Context(), harness.ToolRequest{Agent: agent, Name: ToolName, Arguments: json.RawMessage(`{"todos":[],"extra":-0}`)})
		if err != nil || out.Result.IsError || string(out.Result.Value) != `{"todos":[],"counts":{"pending":0,"inProgress":0,"completed":0}}` || len(todoSnapshots(agent.Session())) != 1 {
			t.Fatalf("native number boundary changed: %+v %v", out, err)
		}
	}
}

func TestTodoDSHDoesNotClassifyMatchingForeignErrorText(t *testing.T) {
	h, _ := todoDomainHarness(t)
	const message = "invalid todo: `content` must be a non-empty string"
	if _, err := Mount(t.Context(), h, Options{ContentNormalization: ContentNormalizationDSHAlpha3, ToolWrapper: func(tool harness.Tool) harness.Tool {
		tool.ExecuteRequest = func(context.Context, harness.ToolRequest) (harness.ToolResult, error) {
			return harness.ToolResult{}, errors.New(message)
		}
		return tool
	}}); err != nil {
		t.Fatal(err)
	}
	agent, err := h.Agents().Create(t.Context(), harness.AgentOptions{ID: "todo-foreign-error"})
	if err != nil {
		t.Fatal(err)
	}
	out, err := h.Tools().Execute(t.Context(), harness.ToolRequest{Agent: agent, Name: ToolName, Arguments: json.RawMessage(`{"todos":[]}`)})
	if err != nil || !out.Result.IsError || len(out.Result.Meta) != 0 || len(out.Result.Content) != 1 || out.Result.Content[0].Text != message || len(todoSnapshots(agent.Session())) != 0 {
		t.Fatalf("foreign error reclassified by message: %+v %v", out, err)
	}
}

