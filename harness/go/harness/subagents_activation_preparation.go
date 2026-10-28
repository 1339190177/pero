// Adapted from subagents_activation.go: declarations split by responsibility without behavior changes.
package harness

import (
	"context"
	"errors"
	"sync"
)

// Preparations are tracked even before the parent's managed record binds:
// Activated observers can delegate while a provider is publishing that Agent.
// Registry mutex protects token ownership and adoption by a later activation.
type subagentPreparation struct {
	bound     *subagentRecord
	parent    *Agent
	published chan struct{}
	settled   chan struct{}
	finalErr  error
	facade    *SubagentFacade
	capacity  *subagentCapacityLease
	receipt   *subagentParentReceipt
	owner     *subagentActivationState
	cancel    context.CancelCauseFunc
	stop      func() bool
}

// reserveSubagentPreparation prevents a managed parent from settling while
// an accepted child preparation has not yet published or rolled back.
func (r *SubagentRegistry) reserveSubagentPreparation(ctx context.Context, parent *Agent) (context.Context, func(), error) {
	if ctx == nil {
		ctx = context.Background()
	}
	facade, _ := ctx.Value(subagentFacadeContextKey{}).(*SubagentFacade)
	r.mu.Lock()
	if r.disposed || !r.preparationAdmitsLocked(ctx, parent) {
		r.mu.Unlock()
		return nil, nil, ErrSubagentAdmissionClosed
	}
	if facade != nil && (facade.registry != r || facade.closing) {
		r.mu.Unlock()
		return nil, nil, ErrSubagentFacadeDraining
	}
	state := r.activationParents[parent]
	if state != nil && state.closing.Load() {
		r.mu.Unlock()
		return nil, nil, errors.New("harness: parent activation is closing")
	}
	if state != nil && state.structuredAdmission != nil && state.structuredAdmission.captures != 0 {
		r.mu.Unlock()
		return nil, nil, ErrSubagentInputClosed
	}
	preparedCtx, cancel := context.WithCancelCause(ctx)
	token := &subagentPreparation{cancel: cancel, facade: facade, parent: parent, published: make(chan struct{}), settled: make(chan struct{})}
	if facade != nil {
		facade.preparations[token] = struct{}{}
		facade.signalLocked()
	}
	if r.parentPreparations == nil {
		r.parentPreparations = make(map[*Agent]map[*subagentPreparation]struct{})
	}
	if r.parentPreparations[parent] == nil {
		r.parentPreparations[parent] = make(map[*subagentPreparation]struct{})
	}
	r.parentPreparations[parent][token] = struct{}{}
	if r.preparationReceipts == nil {
		r.preparationReceipts = make(map[*Agent]map[*subagentPreparation]struct{})
	}
	if r.preparationReceipts[parent] == nil {
		r.preparationReceipts[parent] = make(map[*subagentPreparation]struct{})
	}
	r.preparationReceipts[parent][token] = struct{}{}
	r.signalSubagentOwnershipLocked()
	r.adoptSubagentPreparationLocked(state, token)
	r.mu.Unlock()
	var once sync.Once
	return context.WithValue(preparedCtx, subagentPreparationContextKey{}, token), func() {
		once.Do(func() {
			r.mu.Lock()
			if token.stop != nil {
				token.stop()
			}
			delete(r.parentPreparations[parent], token)
			if len(r.parentPreparations[parent]) == 0 {
				delete(r.parentPreparations, parent)
			}
			if token.owner != nil {
				token.owner.preparing--
				r.signalActivationLocked(token.owner)
			}
			token.owner, token.stop = nil, nil
			// Facade admission remains outstanding through the exact rollback receipt.
			r.mu.Unlock()
			cancel(nil)
			if token.receipt != nil {
				token.receipt.publish()
			}
			close(token.published)
			finish := func(err error) {
				r.mu.Lock()
				token.finalErr = err
				delete(r.preparationReceipts[parent], token)
				if len(r.preparationReceipts[parent]) == 0 {
					delete(r.preparationReceipts, parent)
				}
				close(token.settled)
				r.signalSubagentOwnershipLocked()
				if token.facade != nil {
					token.facade.err = errors.Join(token.facade.err, err)
					delete(token.facade.preparations, token)
					token.facade.signalLocked()
				}
				r.mu.Unlock()
			}
			if token.receipt == nil {
				finish(nil)
			} else {
				go func() { finish(token.receipt.claim.wait(context.Background())) }()
			}
		})
	}, nil
}

func (r *SubagentRegistry) adoptSubagentPreparationLocked(state *subagentActivationState, token *subagentPreparation) {
	if state == nil || token.owner != nil {
		return
	}
	token.owner = state
	state.preparing++
	token.stop = context.AfterFunc(state.preparationContext, func() { token.cancel(context.Cause(state.preparationContext)) })
}
