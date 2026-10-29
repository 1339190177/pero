package harness

import (
	"context"
	"sync/atomic"

	point "github.com/phodal/pero/harness/go/runtime"
)

// An admitted operation shares the Agent's one-way authority and retirement
// receipt. Caller cancellation can finish an RPC while its actual callback
// remains owned until the last reference returns. No callback holds operationMu.
type agentRetirementPhase struct{ hooksDone chan struct{} }

type agentOperation struct {
	agent    *Agent
	context  context.Context
	cancel   context.CancelCauseFunc
	refs     atomic.Int64
	stopRoot func() bool
}

func (a *Agent) beginOperation(ctx context.Context) (*agentOperation, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	a.operationMu.Lock()
	defer a.operationMu.Unlock()
	if err := a.mutationAdmissionError(); err != nil {
		return nil, err
	}
	if err := commandContextFailure(ctx); err != nil {
		return nil, err
	}
	operationCtx, cancel := context.WithCancelCause(ctx)
	operation := &agentOperation{agent: a, context: operationCtx, cancel: cancel}
	if a.operationRoot != nil {
		root := a.operationRoot
		if root.Err() != nil {
			cancel(context.Cause(root))
			return nil, context.Cause(root)
		}
		operation.stopRoot = context.AfterFunc(root, func() { cancel(context.Cause(root)) })
	}
	operation.refs.Store(1)
	if a.operations == nil {
		a.operations = make(map[*agentOperation]struct{})
	}
	a.operations[operation] = struct{}{}
	a.operationWG.Add(1)
	a.operationCount.Add(1)
	return operation, nil
}

func (o *agentOperation) retain() { o.refs.Add(1) }
func (o *agentOperation) release() {
	if o.refs.Add(-1) != 0 {
		return
	}
	if o.stopRoot != nil {
		o.stopRoot()
	}
	o.cancel(nil)
	o.agent.operationMu.Lock()
	delete(o.agent.operations, o)
	o.agent.operationMu.Unlock()
	o.agent.operationCount.Add(-1)
	o.agent.operationWG.Done()
}

func (a *Agent) cancelOperations(cause error) {
	a.operationMu.Lock()
	operations := make([]*agentOperation, 0, len(a.operations))
	for operation := range a.operations {
		operations = append(operations, operation)
	}
	rootCancel := a.operationRootCancel
	a.operationMu.Unlock()
	for _, operation := range operations {
		operation.cancel(cause)
	}
	if rootCancel != nil {
		rootCancel()
	}
}

// The retired fence is set before this admission barrier. Every prior Add is
// complete and no later operation can enter, making the WaitGroup join safe.
func (a *Agent) waitOperations() {
	a.operationMu.Lock()
	a.operationMu.Unlock()
	a.operationWG.Wait()
}

func (a *Agent) bindOperationRun(run *point.Run) error {
	a.operationMu.Lock()
	a.operationRoot = run.Context
	a.operationRootCancel = run.CancelWork
	retired := a.retired.Load()
	a.operationMu.Unlock()
	if retired {
		run.CancelWork()
		return ErrAgentRetired
	}
	return nil
}

// cleanupPhase linearizes autonomous scope cleanup with manager retirement.
// A startup failure that began first is joined via activation.done; a failure
// after retirement admission waits that exact phase's hooks before resources.
func (m *AgentManager) operationCleanupPhase(agent *Agent) *agentRetirementPhase {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	for {
		m.mu.Lock()
		agent.retired.Store(true)
		if phase := agent.operationPhase.Load(); phase != nil {
			m.mu.Unlock()
			return phase
		}
		var target *Agent
		if current, ok := m.agents[agent.ID()]; ok && current.agent == agent {
			target = agent
		}
		// Terminal seal may precede full map withdrawal. Select the highest already
		// retired exact ancestor so a candidate cannot bypass an ancestor's hooks.
		for parent := agent.Parent(); parent != nil; parent = parent.Parent() {
			if current, ok := m.agents[parent.ID()]; ok && current.agent == parent && parent.retired.Load() {
				target = parent
			}
		}
		if target == nil {
			if activation := m.activating[agent.ID()]; activation != nil && activation.candidate == agent {
				activation.cleanupStarted = true
			}
			m.mu.Unlock()
			return nil
		}
		m.mu.Unlock()
		_ = m.RemoveExact(ctx, target)
	}
}

func (m *AgentManager) drainAgentOperationsBeforeCleanup(agent *Agent) error {
	if phase := m.operationCleanupPhase(agent); phase != nil {
		<-phase.hooksDone
	}
	agent.cancelOperations(ErrAgentRetired)
	agent.waitOperations()
	return nil
}

// Scope cancellation is checked synchronously as well as through AfterFunc;
// asynchronous context propagation alone cannot be an admission boundary.
func (a *Agent) operationAdmissionError() error {
	if err := a.mutationAdmissionError(); err != nil {
		return err
	}
	a.operationMu.Lock()
	root := a.operationRoot
	a.operationMu.Unlock()
	if root != nil && root.Err() != nil {
		return ErrAgentRetired
	}
	return nil
}
