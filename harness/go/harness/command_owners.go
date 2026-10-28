package harness

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"

	point "github.com/phodal/pero/harness/go/runtime"
)

// ErrCommandOwnerRetired rejects callbacks retained from a withdrawn exact
// registration, even when a replacement now publishes the same command name.
var ErrCommandOwnerRetired = errors.New("harness: command owner is retired")

// commandOwner binds a registration to its exact activation, not its scope or
// command name. Unregister only fences admission; actual activation cleanup
// cancels and joins every admitted callback before releasing owner resources.
// Neither the registry nor owner mutex is held while calling plugin code.
type commandOwner struct {
	registry *CommandRegistry
	run      *point.Run
	mu       sync.Mutex
	closed   bool
	calls    map[*commandOwnerCall]struct{}
	wg       sync.WaitGroup
}

type commandOwnerCall struct {
	owner     *commandOwner
	context   context.Context
	cancel    context.CancelCauseFunc
	stopRoot  func() bool
	operation *agentOperation
	refs      atomic.Int64
}

func newCommandOwner(registry *CommandRegistry, run *point.Run) *commandOwner {
	return &commandOwner{registry: registry, run: run, calls: make(map[*commandOwnerCall]struct{})}
}

func (o *commandOwner) admissionErrorLocked() error {
	if o.closed || o.registry.closed.Load() || !o.run.Active() || o.run.Context.Err() != nil {
		return ErrCommandOwnerRetired
	}
	return nil
}

func (o *commandOwner) admissionError() error {
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.admissionErrorLocked()
}

func (o *commandOwner) unregister() {
	o.mu.Lock()
	o.closed = true
	o.mu.Unlock()
}

func (o *commandOwner) drain(context.Context) error {
	o.mu.Lock()
	o.closed = true
	calls := make([]*commandOwnerCall, 0, len(o.calls))
	for call := range o.calls {
		calls = append(calls, call)
	}
	o.mu.Unlock()
	for _, call := range calls {
		call.cancel(ErrCommandOwnerRetired)
	}
	// The closed fence and all prior Add calls share mu. This is the owner
	// barrier, so a canceled cleanup wait cannot release resources early.
	o.wg.Wait()
	return nil
}

func (o *commandOwner) begin(ctx context.Context, agent *Agent) (*commandOwnerCall, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	var operation *agentOperation
	if agent != nil {
		var err error
		operation, err = agent.beginOperation(ctx)
		if err != nil {
			return nil, err
		}
		ctx = operation.context
	}
	o.mu.Lock()
	defer o.mu.Unlock()
	if err := o.admissionErrorLocked(); err != nil {
		if operation != nil {
			operation.release()
		}
		return nil, err
	}
	if err := commandContextFailure(ctx); err != nil {
		if operation != nil {
			operation.release()
		}
		return nil, err
	}
	callCtx, cancel := context.WithCancelCause(ctx)
	call := &commandOwnerCall{owner: o, context: callCtx, cancel: cancel, operation: operation}
	call.stopRoot = context.AfterFunc(o.run.Context, func() { cancel(ErrCommandOwnerRetired) })
	o.registry.callbacks.Add(1)
	// Harness sealing sets closed before observing callbacks. Incrementing
	// first and then rechecking prevents new callback bodies after that fence.
	if err := o.admissionErrorLocked(); err != nil {
		o.registry.callbacks.Add(-1)
		call.stopRoot()
		cancel(err)
		if operation != nil {
			operation.release()
		}
		return nil, err
	}
	call.refs.Store(2) // caller and actual callback body
	o.calls[call] = struct{}{}
	o.wg.Add(1)
	return call, nil
}

func (c *commandOwnerCall) release() {
	if c.refs.Add(-1) != 0 {
		return
	}
	c.stopRoot()
	c.cancel(nil)
	c.owner.mu.Lock()
	delete(c.owner.calls, c)
	c.owner.mu.Unlock()
	if c.operation != nil {
		c.operation.release()
	}
	c.owner.registry.callbacks.Add(-1)
	c.owner.wg.Done()
}

// invokeCommandOwner keeps the body lease after an RPC caller stops waiting.
// A blocked or panicking List/Resolve has the same ownership as a Handler.
func invokeCommandOwner[T any](ctx context.Context, agent *Agent, owner *commandOwner, label string, callback func(context.Context) (T, error)) (T, error) {
	var zero T
	call, err := owner.begin(ctx, agent)
	if err != nil {
		return zero, err
	}
	defer call.release()
	type outcome struct {
		value T
		err   error
	}
	settled := make(chan outcome, 1)
	go func() {
		defer call.release()
		completed := outcome{}
		defer func() {
			if recovered := recover(); recovered != nil {
				completed.err = fmt.Errorf("harness: %s panic: %v", label, recovered)
			}
			settled <- completed
		}()
		if agent != nil {
			if completed.err = agent.operationAdmissionError(); completed.err != nil {
				return
			}
		}
		if completed.err = commandContextFailure(call.context); completed.err != nil {
			return
		}
		if completed.err = owner.admissionError(); completed.err != nil {
			return
		}
		completed.value, completed.err = callback(call.context)
	}()
	select {
	case completed := <-settled:
		// Retirement sets the exact Agent fence before asynchronous context
		// cancellation. A direct resolved Handler must observe it too.
		if agent != nil {
			if err := agent.operationAdmissionError(); err != nil {
				return zero, err
			}
		}
		if err := commandContextFailure(call.context); err != nil {
			return zero, err
		}
		if err := owner.admissionError(); err != nil {
			return zero, err
		}
		return completed.value, completed.err
	case <-call.context.Done():
		return zero, commandContextFailure(call.context)
	}
}

func (o *commandOwner) wrapHandler(handler CommandHandler) CommandHandler {
	return func(ctx context.Context, invocation CommandInvocation) (CommandResult, error) {
		return invokeCommandOwner(ctx, invocation.Agent, o, "command handler", func(ctx context.Context) (CommandResult, error) {
			return handler(ctx, invocation)
		})
	}
}

func (e commandProviderEntry) list(ctx context.Context, agent *Agent) ([]CommandDescriptor, error) {
	return invokeCommandOwner(ctx, agent, e.owner, "command provider list", func(ctx context.Context) ([]CommandDescriptor, error) {
		return e.provider.List(ctx, agent)
	})
}

func (e commandProviderEntry) resolve(ctx context.Context, agent *Agent, name string) (CommandDefinition, bool, error) {
	type resolved struct {
		command CommandDefinition
		found   bool
	}
	result, err := invokeCommandOwner(ctx, agent, e.owner, "command provider resolve", func(ctx context.Context) (resolved, error) {
		command, found, err := e.provider.Resolve(ctx, agent, name)
		return resolved{command, found}, err
	})
	if err == nil && result.found && result.command.Handler != nil {
		result.command.Handler = e.owner.wrapHandler(result.command.Handler)
	}
	return result.command, result.found, err
}

func (r *CommandRegistry) sealAdmission() bool {
	if r == nil {
		return false
	}
	r.closed.Store(true)
	return r.callbacks.Load() != 0
}
