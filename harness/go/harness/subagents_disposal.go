// Adapted from subagents_lifecycle.go: declarations split by responsibility without behavior changes.
package harness

import (
	"context"
	"errors"
	"sync"
)

// guardSubagentDisposal keeps the provider callback outside every lock. The
// first caller performs cleanup; concurrent/reentrant callers receive pending,
// and calls after completion receive the original cleanup result.
type subagentDisposalClaim struct {
	mu           sync.Mutex
	started      bool
	done         chan struct{}
	result       error
	dispose      func(context.Context) error
	scopeWait    func(context.Context) error
	scopePending func() bool
}

func newSubagentDisposalClaim(dispose func(context.Context) error) *subagentDisposalClaim {
	if dispose == nil {
		return nil
	}
	return &subagentDisposalClaim{done: make(chan struct{}), dispose: dispose}
}

func (s *subagentDisposalClaim) callback() func(context.Context) error {
	if s == nil {
		return nil
	}
	return s.invoke
}

func guardSubagentDisposal(dispose func(context.Context) error) func(context.Context) error {
	return newSubagentDisposalClaim(dispose).callback()
}

type subagentDisposalPending struct{ claim *subagentDisposalClaim }

func (e *subagentDisposalPending) Error() string { return ErrSubagentDisposalInProgress.Error() }

func (e *subagentDisposalPending) Unwrap() error { return ErrSubagentDisposalInProgress }

func (s *subagentDisposalClaim) bindAgent(manager *AgentManager, agent *Agent) {
	if s == nil || manager == nil || agent == nil {
		return
	}
	s.mu.Lock()
	s.scopeWait = func(ctx context.Context) error { return manager.waitAgentScope(ctx, agent) }
	s.scopePending = func() bool { return manager.agentScopePending(agent) }
	s.mu.Unlock()
}

func (s *subagentDisposalClaim) complete(err error) {
	s.mu.Lock()
	s.result = err
	close(s.done)
	s.mu.Unlock()
}

func (s *subagentDisposalClaim) invoke(ctx context.Context) error {
	s.mu.Lock()
	if s.started {
		select {
		case <-s.done:
			err := s.result
			s.mu.Unlock()
			return err
		default:
			s.mu.Unlock()
			return &subagentDisposalPending{claim: s}
		}
	}
	s.started = true
	s.mu.Unlock()
	if ctx == nil {
		ctx = context.Background()
	}
	err := callSubagentCleanup(func() error { return s.dispose(ctx) })
	s.mu.Lock()
	scopeWait, scopePending := s.scopeWait, s.scopePending
	s.mu.Unlock()
	if scopeWait != nil && (errors.Is(err, ErrAgentDisposalInProgress) || scopePending != nil && scopePending()) {
		// The first Remove may be busy, or an ancestor may already have withdrawn
		// this child. A nil duplicate Remove is not a cleanup receipt. Settle only
		// after the exact worker/writer scope has actually stopped.
		go func() { s.complete(errors.Join(withoutRetirementPending(err), scopeWait(context.Background()))) }()
		return &subagentDisposalPending{claim: s}
	}
	s.complete(err)
	return err
}

func (s *subagentDisposalClaim) wait(ctx context.Context) error {
	select {
	case <-s.done:
	case <-ctx.Done():
		return ctx.Err()
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.result
}

func (s *subagentDisposalClaim) inProgress() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.started {
		return false
	}
	select {
	case <-s.done:
		return false
	default:
		return true
	}
}

func (r *SubagentRegistry) beginSubagentCallback() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.disposed {
		return false
	}
	r.callbacks.Add(1)
	r.callbackWG.Add(1)
	return true
}

func (r *SubagentRegistry) endSubagentCallback() {
	r.callbacks.Add(-1)
	r.callbackWG.Done()
}

// Called only while an admitted preparation owns callbackWG. Any asynchronous
// exact cleanup extends that existing receipt before its admission is released.
func (r *SubagentRegistry) cleanupPreparedSubagent(dispose func(context.Context) error) error {
	if dispose == nil {
		return nil
	}
	err := callSubagentCleanup(func() error { return dispose(context.Background()) })
	var pending *subagentDisposalPending
	if errors.As(err, &pending) {
		r.callbackWG.Add(1)
		go func() {
			defer r.callbackWG.Done()
			finalErr := pending.claim.wait(context.Background())
			r.mu.Lock()
			r.callbackCleanupErr = errors.Join(r.callbackCleanupErr, finalErr)
			r.mu.Unlock()
		}()
		return err
	}
	r.mu.Lock()
	r.callbackCleanupErr = errors.Join(r.callbackCleanupErr, err)
	r.mu.Unlock()
	return err
}

func (r *SubagentRegistry) withSubagentCallback(callback func() error) error {
	if !r.beginSubagentCallback() {
		return errors.New("harness: subagent registry is disposed")
	}
	defer r.endSubagentCallback()
	return callback()
}

func (r *SubagentRegistry) emitSubagentStarted(ctx context.Context, event SubagentLifecycle) error {
	return r.withSubagentCallback(func() error { return r.Started.Emit(ctx, event) })
}

func (r *SubagentRegistry) emitSubagentEnded(ctx context.Context, event SubagentLifecycle) error {
	return r.withSubagentCallback(func() error { return r.Ended.Emit(ctx, event) })
}

func (r *SubagentRegistry) deliverSubagentMessage(parent *Agent, message Message, wake bool) error {
	return r.withSubagentCallback(func() error { return parent.Deliver(message, wake) })
}
