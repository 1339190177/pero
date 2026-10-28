package harness

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	point "github.com/phodal/pero/harness/go/runtime"
)

func TestToolRegistrationIdentityUsesExactAgentCatalog(t *testing.T) {
	h, err := Boot(t.Context(), Options{Adapter: AdapterFunc(func(context.Context, GenerateRequest, func(StreamChunk) error) (Completion, error) {
		return Completion{}, nil
	})})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = h.Dispose(context.Background()); _ = h.Drain(context.Background()) })
	tool := Tool{Schema: ToolSchema{Name: "identity", InputSchema: json.RawMessage(`{"type":"object"}`)}, Execute: func(context.Context, json.RawMessage) (ToolResult, error) { return ToolResult{}, nil }}
	var receipt ToolRegistration
	def := point.Define(point.Descriptor{ID: "test/tool-identity"}, point.DecodeJSON[struct{}], func(run *point.Run, _ struct{}) (point.Dispose, error) {
		var err error
		receipt, _, err = h.Tools().RegisterWithIdentity(run, tool)
		return nil, err
	})
	owner, err := h.Runtime().Mount(nil, "identity", def, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err = owner.WaitStable(t.Context()); err != nil {
		t.Fatal(err)
	}
	agent, err := h.Agents().Create(t.Context(), AgentOptions{ID: "identity-agent"})
	if err != nil {
		t.Fatal(err)
	}
	check := func(want bool) {
		t.Helper()
		got, err := receipt.Visible(t.Context(), agent)
		if err != nil || got != want {
			t.Fatalf("visible=%v/%v want=%v", got, err, want)
		}
	}
	check(true)
	mask, err := h.RegisterToolRestrictionAt(t.Context(), agent, "identity-mask", ToolRestriction{Deny: []string{"identity"}})
	if err != nil {
		t.Fatal(err)
	}
	check(false)
	if err = mask.Dispose(t.Context()); err != nil {
		t.Fatal(err)
	}
	shadow, err := h.RegisterToolAt(t.Context(), agent.Scope(), "identity-shadow", tool)
	if err != nil {
		t.Fatal(err)
	}
	check(false)
	if err = shadow.Dispose(t.Context()); err != nil {
		t.Fatal(err)
	}
	check(true)
	var base ToolCatalogBase
	hidden, forged := false, false
	presenterDef := point.Define(point.Descriptor{ID: "test/identity-presenter"}, point.DecodeJSON[struct{}], func(run *point.Run, _ struct{}) (point.Dispose, error) {
		return h.Tools().RegisterCatalogProvider(run, "identity-presenter", ToolCatalogProvider{Authoritative: true, Bind: func(value ToolCatalogBase) { base = value }, Schemas: func(ctx context.Context, a *Agent) ([]ToolSchema, error) { return base.Schemas(ctx, a) }, Resolve: func(ctx context.Context, a *Agent, name string) (Tool, bool, error) {
			if hidden {
				return Tool{}, false, nil
			}
			if forged {
				return tool, true, nil
			}
			captured, ok, err := base.Resolve(ctx, a, name)
			if err != nil || !ok {
				return captured, ok, err
			}
			proxy, err := base.BindCapturedOwner(tool, captured)
			return proxy, true, err
		}})
	})
	presenter, err := h.Runtime().Mount(nil, "identity-presenter", presenterDef, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err = presenter.WaitStable(t.Context()); err != nil {
		t.Fatal(err)
	}
	check(true)
	hidden = true
	check(false)
	hidden = false
	forged = true
	check(false)
	forged = false
	check(true)
	if err = presenter.Dispose(t.Context()); err != nil {
		t.Fatal(err)
	}
	old := receipt
	if err = owner.Dispose(t.Context()); err != nil {
		t.Fatal(err)
	}
	replacement, err := h.Runtime().Mount(nil, "identity", def, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err = replacement.WaitStable(t.Context()); err != nil {
		t.Fatal(err)
	}
	check(true)
	if visible, err := old.Visible(t.Context(), agent); err != nil || visible {
		t.Fatalf("stale receipt revived: %v/%v", visible, err)
	}
}

func TestToolRegistrationVisibilityRechecksAgentAndContextAfterResolver(t *testing.T) {
	for _, action := range []string{"cancel", "retire"} {
		t.Run(action, func(t *testing.T) {
			h := retirementHarness(t)
			agent, err := h.Agents().Create(t.Context(), AgentOptions{ID: "identity-reentry"})
			if err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			native := Tool{Schema: ToolSchema{Name: "identity_reentry", InputSchema: json.RawMessage(`{"type":"object"}`)}, Execute: func(context.Context, json.RawMessage) (ToolResult, error) { return ToolResult{}, nil }}
			var receipt ToolRegistration
			var base ToolCatalogBase
			mutate := false
			mountRetirementHooks(t, h, func(run *point.Run) error {
				var err error
				receipt, _, err = h.Tools().RegisterWithIdentity(run, native)
				if err != nil {
					return err
				}
				_, err = h.Tools().RegisterCatalogProvider(run, "identity-reentry", ToolCatalogProvider{
					Authoritative: true,
					Bind:          func(value ToolCatalogBase) { base = value },
					Schemas:       func(ctx context.Context, a *Agent) ([]ToolSchema, error) { return base.Schemas(ctx, a) },
					Resolve: func(ctx context.Context, a *Agent, name string) (Tool, bool, error) {
						captured, found, err := base.Resolve(ctx, a, name)
						if err != nil || !found {
							return captured, found, err
						}
						proxy, err := base.BindCapturedOwner(native, captured)
						if err != nil {
							return Tool{}, false, err
						}
						if mutate {
							if action == "cancel" {
								cancel()
							} else if err = h.Agents().RemoveExact(context.Background(), a); err != nil {
								return Tool{}, false, err
							}
						}
						return proxy, true, nil
					},
				})
				return err
			})
			if visible, err := receipt.Visible(ctx, agent); err != nil || !visible {
				t.Fatalf("live registration invisible: %v/%v", visible, err)
			}
			mutate = true
			visible, err := receipt.Visible(ctx, agent)
			if visible {
				t.Fatal("resolver published visibility after retirement or cancellation")
			}
			if action == "cancel" && !errors.Is(err, context.Canceled) {
				t.Fatalf("cancellation identity lost: %v", err)
			}
			if action == "retire" && err != nil {
				t.Fatal(err)
			}
			if err := receipt.owner.admissionError(); err != nil {
				t.Fatalf("fixture retired native owner instead of Agent: %v", err)
			}
			if action == "retire" {
				if !agent.retired.Load() {
					t.Fatal("resolver did not retire exact Agent")
				}
				mutate = false
				replacement, err := h.Agents().Resume(t.Context(), agent.Session(), AgentOptions{ID: agent.ID()})
				if err != nil {
					t.Fatal(err)
				}
				if visible, err := receipt.Visible(t.Context(), replacement); err != nil || !visible {
					t.Fatalf("fresh Agent lost surviving registration: %v/%v", visible, err)
				}
				if visible, err := receipt.Visible(t.Context(), agent); err != nil || visible {
					t.Fatalf("old exact Agent revived: %v/%v", visible, err)
				}
			}
		})
	}
}
