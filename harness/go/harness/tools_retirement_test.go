package harness

import (
	"context"
	"encoding/json"
	"errors"
	"sync/atomic"
	"testing"

	point "github.com/phodal/pero/harness/go/runtime"
)

func mountRetirementHooks(t *testing.T, h *Harness, apply func(*point.Run) error) {
	t.Helper()
	definition := point.Define(point.Descriptor{ID: "test/tool-retirement"}, point.DecodeJSON[struct{}], func(run *point.Run, _ struct{}) (point.Dispose, error) { return nil, apply(run) })
	instance, err := h.Runtime().Mount(nil, "test/tool-retirement", definition, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = instance.Dispose(context.Background()) })
	if err := instance.WaitStable(t.Context()); err != nil {
		t.Fatal(err)
	}
	if instance.Snapshot().State == point.Failed {
		t.Fatal(instance.Snapshot().LastError)
	}
}

func retirementHarness(t *testing.T) *Harness {
	t.Helper()
	h, err := Boot(t.Context(), Options{Adapter: AdapterFunc(func(context.Context, GenerateRequest, func(StreamChunk) error) (Completion, error) {
		return Completion{Message: Message{Role: RoleAssistant}, Finish: FinishStop}, nil
	})})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = h.Dispose(context.Background()) })
	return h
}
func requireRetiredToolResult(t *testing.T, out ToolResult, code string) {
	t.Helper()
	var meta struct {
		Code string `json:"code"`
	}
	if err := json.Unmarshal(out.Meta, &meta); err != nil {
		t.Fatal(err)
	}
	if !out.IsError || out.ConcludesTurn || meta.Code != code {
		t.Fatalf("retired outcome = %+v; code = %s", out, meta.Code)
	}
}

func TestToolDispatchFencesRetiredAgentAcrossCallbacks(t *testing.T) {
	for _, phase := range []string{"entry", "before", "resolver", "approval"} {
		t.Run(phase, func(t *testing.T) {
			h := retirementHarness(t)
			agent, err := h.Agents().Create(t.Context(), AgentOptions{ID: "retirement"})
			if err != nil {
				t.Fatal(err)
			}
			if phase == "approval" {
				if _, err := agent.Session().Append("turn/start", map[string]int{"turn": 1}); err != nil {
					t.Fatal(err)
				}
			}
			retire := func() error { return h.Agents().RemoveExact(context.Background(), agent) }
			var bodies, before, after atomic.Int32
			tool := Tool{Schema: ToolSchema{Name: "retirement_probe", InputSchema: json.RawMessage(`{"type":"object"}`)}, RequiresApproval: phase == "approval", Execute: func(context.Context, json.RawMessage) (ToolResult, error) { bodies.Add(1); return ToolResult{}, nil }}
			mountRetirementHooks(t, h, func(run *point.Run) error {
				if _, err := h.Tools().Register(run, tool); err != nil {
					return err
				}
				if _, err := h.Tools().Before.Use(run, func(_ context.Context, request ToolRequest, next point.Next[ToolRequest]) (ToolRequest, error) {
					before.Add(1)
					if phase == "before" {
						if err := retire(); err != nil {
							return request, err
						}
					}
					return next(request)
				}); err != nil {
					return err
				}
				if _, err := h.Tools().After.Use(run, func(_ context.Context, out ToolOutcome, next point.Next[ToolOutcome]) (ToolOutcome, error) {
					after.Add(1)
					return next(out)
				}); err != nil {
					return err
				}
				if phase == "resolver" {
					_, err := h.Tools().RegisterCatalogProvider(run, "retirement", ToolCatalogProvider{Authoritative: true, Schemas: func(context.Context, *Agent) ([]ToolSchema, error) { return []ToolSchema{tool.Schema}, nil }, Resolve: func(context.Context, *Agent, string) (Tool, bool, error) { err := retire(); return tool, true, err }})
					return err
				}
				if phase == "approval" {
					_, err := h.Approvals().Register(run, func(context.Context, ApprovalRequest) (ApprovalOutcome, error) { return ApprovalAllowedOnce, retire() })
					return err
				}
				return nil
			})
			if phase == "entry" {
				if err := retire(); err != nil {
					t.Fatal(err)
				}
			}
			out, err := h.Tools().Execute(t.Context(), ToolRequest{Agent: agent, CallID: "call", Name: tool.Schema.Name, Arguments: json.RawMessage(`{}`)})
			if err != nil {
				t.Fatal(err)
			}
			requireRetiredToolResult(t, out.Result, ToolAbortedBeforeDispatch)
			if bodies.Load() != 0 {
				t.Fatal("retired generation entered tool body")
			}
			if phase == "entry" {
				if before.Load() != 0 || after.Load() != 0 {
					t.Fatal("late admission invoked policy callbacks")
				}
			} else if before.Load() != 1 || after.Load() != 1 {
				t.Fatalf("cleanup callbacks: before=%d after=%d", before.Load(), after.Load())
			}
		})
	}
}

func TestToolResultRetirementStillRunsRollbackObservers(t *testing.T) {
	for _, phase := range []string{"body", "after", "observer"} {
		t.Run(phase, func(t *testing.T) {
			h := retirementHarness(t)
			agent, err := h.Agents().Create(t.Context(), AgentOptions{ID: "result-retirement"})
			if err != nil {
				t.Fatal(err)
			}
			retire := func() error { return h.Agents().RemoveExact(context.Background(), agent) }
			var after, observer, results int
			var observed, announced ToolResult
			mountRetirementHooks(t, h, func(run *point.Run) error {
				if _, err := h.Tools().Register(run, Tool{Schema: ToolSchema{Name: "retirement_probe", InputSchema: json.RawMessage(`{"type":"object"}`)}, Execute: func(context.Context, json.RawMessage) (ToolResult, error) {
					if phase == "body" {
						if err := retire(); err != nil {
							return ToolResult{}, err
						}
					}
					return ToolResult{ConcludesTurn: true}, nil
				}, ObserveResult: func(out ToolOutcome) {
					observer++
					observed = out.Result
					if phase == "observer" {
						if err := retire(); err != nil {
							t.Error(err)
						}
					}
				}}); err != nil {
					return err
				}
				if _, err := h.Tools().After.Use(run, func(_ context.Context, out ToolOutcome, next point.Next[ToolOutcome]) (ToolOutcome, error) {
					after++
					if phase == "after" {
						if err := retire(); err != nil {
							return out, err
						}
					}
					return next(out)
				}); err != nil {
					return err
				}
				_, err := h.Tools().Results.On(run, func(_ context.Context, out ToolOutcome) error { results++; announced = out.Result; return nil })
				return err
			})
			out, err := h.Tools().Execute(t.Context(), ToolRequest{Agent: agent, CallID: "call", Name: "retirement_probe", Arguments: json.RawMessage(`{}`)})
			if err != nil {
				t.Fatal(err)
			}
			requireRetiredToolResult(t, out.Result, ToolAborted)
			if after != 1 || observer != 1 || results != 1 {
				t.Fatalf("cleanup skipped: after=%d observer=%d results=%d", after, observer, results)
			}
			if phase != "observer" {
				requireRetiredToolResult(t, observed, ToolAborted)
			}
			requireRetiredToolResult(t, announced, ToolAborted)
		})
	}
}

func TestAdmittedBaseDispatchRejectsRetiredGeneration(t *testing.T) {
	h := retirementHarness(t)
	agent, err := h.Agents().Create(t.Context(), AgentOptions{ID: "nested-retirement"})
	if err != nil {
		t.Fatal(err)
	}
	var base ToolCatalogBase
	var bodies atomic.Int32
	var nested ToolResult
	mountRetirementHooks(t, h, func(run *point.Run) error {
		if _, err := h.Tools().Register(run, Tool{Schema: ToolSchema{Name: "base_probe", InputSchema: json.RawMessage(`{"type":"object"}`)}, Execute: func(context.Context, json.RawMessage) (ToolResult, error) { bodies.Add(1); return ToolResult{}, nil }}); err != nil {
			return err
		}
		presenter := Tool{Schema: ToolSchema{Name: "presenter", InputSchema: json.RawMessage(`{"type":"object"}`)}, ExecuteRequest: func(ctx context.Context, request ToolRequest) (ToolResult, error) {
			if err := h.Agents().RemoveExact(context.Background(), request.Agent); err != nil {
				return ToolResult{}, err
			}
			request.Name = "base_probe"
			var err error
			nested, err = base.Dispatch(ctx, request)
			return nested, err
		}}
		_, err := h.Tools().RegisterCatalogProvider(run, "presentation", ToolCatalogProvider{Authoritative: true, Bind: func(view ToolCatalogBase) { base = view }, Schemas: func(context.Context, *Agent) ([]ToolSchema, error) { return []ToolSchema{presenter.Schema}, nil }, Resolve: func(_ context.Context, _ *Agent, name string) (Tool, bool, error) {
			return presenter, name == "presenter", nil
		}})
		return err
	})
	out, err := h.Tools().Execute(t.Context(), ToolRequest{Agent: agent, CallID: "call", Name: "presenter", Arguments: json.RawMessage(`{}`)})
	if err != nil {
		t.Fatal(err)
	}
	requireRetiredToolResult(t, nested, ToolAbortedBeforeDispatch)
	requireRetiredToolResult(t, out.Result, ToolAbortedBeforeDispatch)
	if bodies.Load() != 0 {
		t.Fatal("admitted token revived a retired Agent")
	}
}

func TestToolDispatchAllowsPrepublicationButRetiresFailedActivation(t *testing.T) {
	h := retirementHarness(t)
	var calls atomic.Int32
	if _, err := h.RegisterTool(t.Context(), "prepublication-probe", Tool{Schema: ToolSchema{Name: "prepublication_probe", InputSchema: json.RawMessage(`{"type":"object"}`)}, Execute: func(context.Context, json.RawMessage) (ToolResult, error) { calls.Add(1); return ToolResult{}, nil }}); err != nil {
		t.Fatal(err)
	}
	var leaked *Agent
	expected := errors.New("configure deliberately rejected")
	_, err := h.Agents().Create(t.Context(), AgentOptions{ID: "failed-activation", Configure: func(ctx context.Context, _ *point.Run, agent *Agent) error {
		leaked = agent
		out, err := h.Tools().Execute(ctx, ToolRequest{Agent: agent, CallID: "setup", Name: "prepublication_probe", Arguments: json.RawMessage(`{}`)})
		if err != nil {
			return err
		}
		if out.Result.IsError {
			return errors.New("prepublication dispatch was mistaken for retirement")
		}
		return expected
	}})
	if err == nil || leaked == nil || calls.Load() != 1 {
		t.Fatalf("setup err=%v leaked=%p calls=%d", err, leaked, calls.Load())
	}
	out, err := h.Tools().Execute(t.Context(), ToolRequest{Agent: leaked, CallID: "late", Name: "prepublication_probe", Arguments: json.RawMessage(`{}`)})
	if err != nil {
		t.Fatal(err)
	}
	requireRetiredToolResult(t, out.Result, ToolAbortedBeforeDispatch)
	if calls.Load() != 1 {
		t.Fatal("failed activation retained dispatch authority")
	}
}
