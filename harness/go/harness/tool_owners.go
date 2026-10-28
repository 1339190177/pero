package harness

import (
	"context"
	"encoding/json"
	"errors"
	"sync"

	point "github.com/phodal/pero/harness/go/runtime"
)

// ErrToolOwnerRetired rejects callbacks captured from a withdrawn exact
// registration, including after the same tool name is mounted again.
var ErrToolOwnerRetired = errors.New("harness: tool owner is retired")

// toolOwner leases the exact registration's activation. Unregister fences new
// callbacks; activation cleanup cancels and joins admitted bodies before any
// ordinary resources are released. Callbacks remain synchronous, preserving
// tool timeout quiescence and the Agent's existing retirement semantics.
// No registry or owner lock is held while calling plugin code.
type toolOwner struct {
	registry *ToolRegistry
	run      *point.Run
	mu       sync.Mutex
	closed   bool
	calls    map[*toolOwnerCall]struct{}
	wg       sync.WaitGroup
}

type toolOwnerCall struct {
	owner    *toolOwner
	context  context.Context
	cancel   context.CancelCauseFunc
	stopRoot func() bool
}

func newToolOwner(registry *ToolRegistry, run *point.Run) *toolOwner {
	return &toolOwner{registry: registry, run: run, calls: make(map[*toolOwnerCall]struct{})}
}

func (o *toolOwner) admissionErrorLocked() error {
	if o.closed || o.registry.closed.Load() || !o.run.Active() || o.run.Context.Err() != nil {
		return ErrToolOwnerRetired
	}
	return nil
}

func (o *toolOwner) admissionError() error {
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.admissionErrorLocked()
}

func (o *toolOwner) unregister() {
	o.mu.Lock()
	o.closed = true
	o.mu.Unlock()
}

func (o *toolOwner) drain(context.Context) error {
	o.mu.Lock()
	o.closed = true
	calls := make([]*toolOwnerCall, 0, len(o.calls))
	for call := range o.calls {
		calls = append(calls, call)
	}
	o.mu.Unlock()
	for _, call := range calls {
		call.cancel(ErrToolOwnerRetired)
	}
	// Admission and Add share mu with the closed fence. A canceled cleanup
	// waiter must not release borrowed resources before their bodies settle.
	o.wg.Wait()
	return nil
}

func (o *toolOwner) begin(ctx context.Context) (*toolOwnerCall, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	o.mu.Lock()
	defer o.mu.Unlock()
	if err := o.admissionErrorLocked(); err != nil {
		return nil, err
	}
	if err := contextFailure(ctx); err != nil {
		return nil, err
	}
	callCtx, cancel := context.WithCancelCause(ctx)
	call := &toolOwnerCall{owner: o, context: callCtx, cancel: cancel}
	call.stopRoot = context.AfterFunc(o.run.Context, func() { cancel(ErrToolOwnerRetired) })
	o.registry.callbacks.Add(1)
	// Harness sealing publishes closed before observing callbacks. A racing
	// increment must recheck the fence before any callback body may start.
	if err := o.admissionErrorLocked(); err != nil {
		o.registry.callbacks.Add(-1)
		call.stopRoot()
		cancel(err)
		return nil, err
	}
	o.registry.ownerMu.Lock()
	if o.registry.activeOwners == nil {
		o.registry.activeOwners = make(map[*toolOwner]int)
	}
	o.registry.activeOwners[o]++
	o.registry.ownerMu.Unlock()
	o.calls[call] = struct{}{}
	o.wg.Add(1)
	return call, nil
}

func (c *toolOwnerCall) release() {
	c.stopRoot()
	c.cancel(nil)
	c.owner.mu.Lock()
	delete(c.owner.calls, c)
	c.owner.mu.Unlock()
	registry := c.owner.registry
	registry.ownerMu.Lock()
	registry.activeOwners[c.owner]--
	if registry.activeOwners[c.owner] == 0 {
		delete(registry.activeOwners, c.owner)
	}
	registry.ownerMu.Unlock()
	registry.callbacks.Add(-1)
	c.owner.wg.Done()
}

func (o *toolOwner) wrap(tool Tool) Tool {
	tool.owner = o
	if render := tool.RenderOutput; render != nil {
		tool.RenderOutput = func(ctx context.Context, request ToolRequest, value json.RawMessage) (ToolOutputProjection, error) {
			call, err := o.begin(ctx)
			if err != nil {
				return ToolOutputProjection{}, err
			}
			defer call.release()
			result, err := render(call.context, request, value)
			if err == nil {
				err = errors.Join(contextFailure(call.context), o.admissionError())
			}
			return result, err
		}
	}
	if execute := tool.Execute; execute != nil {
		tool.Execute = func(ctx context.Context, arguments json.RawMessage) (ToolResult, error) {
			return o.execute(ctx, func(ctx context.Context) (ToolResult, error) { return execute(ctx, arguments) })
		}
	}
	if execute := tool.ExecuteRequest; execute != nil {
		tool.ExecuteRequest = func(ctx context.Context, request ToolRequest) (ToolResult, error) {
			return o.execute(ctx, func(ctx context.Context) (ToolResult, error) { return execute(ctx, request) })
		}
	}
	if classify := tool.IsConcurrencySafe; classify != nil {
		tool.IsConcurrencySafe = func(ctx context.Context, request ToolRequest) (safe bool) {
			call, err := o.begin(ctx)
			if err != nil {
				return false
			}
			defer call.release()
			defer func() {
				if recover() != nil {
					safe = false
				}
			}()
			safe = classify(call.context, request)
			return safe && contextFailure(call.context) == nil && o.admissionError() == nil
		}
	}
	if describe := tool.ApprovalDescriptor; describe != nil {
		tool.ApprovalDescriptor = func(ctx context.Context, request ToolRequest) (*NativeApprovalDescriptor, error) {
			call, err := o.begin(ctx)
			if err != nil {
				return nil, err
			}
			defer call.release()
			result, err := describe(call.context, request)
			if err != nil {
				return nil, err
			}
			if err := contextFailure(call.context); err != nil {
				return nil, err
			}
			if err := o.admissionError(); err != nil {
				return nil, err
			}
			return result, nil
		}
	}
	if reason := tool.ApprovalReason; reason != nil {
		tool.ApprovalReason = func(request ToolRequest) string {
			call, err := o.begin(context.Background())
			if err != nil {
				return ""
			}
			defer call.release()
			result := reason(request)
			if o.admissionError() != nil {
				return ""
			}
			return result
		}
	}
	if observe := tool.ObserveResult; observe != nil {
		tool.ObserveResult = func(outcome ToolOutcome) {
			call, err := o.begin(context.Background())
			if err != nil {
				return
			}
			defer call.release()
			// Agent retirement still delivers rollback observations. This fence
			// is solely about the registration owner's borrowed resources.
			observe(retireSuccessfulToolOutcome(outcome))
		}
	}
	return tool
}

func (o *toolOwner) execute(ctx context.Context, execute func(context.Context) (ToolResult, error)) (ToolResult, error) {
	call, err := o.begin(ctx)
	if err != nil {
		return ToolResult{}, err
	}
	defer call.release()
	result, err := execute(call.context)
	if o.admissionError() != nil && !result.IsError && err == nil {
		return retiredToolOwnerResult(result.AdditionalContexts), nil
	}
	return result, err
}

func retiredToolOwnerResult(contexts []DeferredContext) ToolResult {
	return toolPolicyErrorResult("tool call aborted: registration owner was retired", "AbortError", ToolAborted, contexts)
}

func (r *ToolRegistry) sealAdmission() bool {
	if r == nil {
		return false
	}
	r.closed.Store(true)
	return r.callbacks.Load() != 0
}

// callbacksWithin detects owner-scope reentry even for raw Execute closures
// without a request Agent. Global tools do not change unrelated Agent removal.
// Retained callbacks stay counted after their registry entry is withdrawn.
func (r *ToolRegistry) callbacksWithin(scope *point.Scope) bool {
	if r == nil || scope == nil {
		return false
	}
	r.ownerMu.Lock()
	defer r.ownerMu.Unlock()
	for owner := range r.activeOwners {
		if scope.Contains(owner.run.Scope) {
			return true
		}
	}
	return false
}
