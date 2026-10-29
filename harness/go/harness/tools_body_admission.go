package harness

import (
	"bytes"
	"context"
	"errors"
	"sync"
	"sync/atomic"
)

type toolBodyAdmissionKey struct{}

// toolBodyAdmission proves post-policy body entry, not merely an allocated
// execution token. It expires before After, observers and final cleanup.
type toolBodyAdmission struct {
	children     int
	childrenDone chan struct{}
	retireOnce   sync.Once
	mu           sync.Mutex
	cleanups     []func()
	active       atomic.Bool
	token        ToolExecutionToken
	agent        *Agent
	name         string
	arguments    []byte
	owner        *toolOwner
	profile      *ProfileOwnerLease
}

func withToolBodyAdmission(ctx context.Context, request ToolRequest) (context.Context, func()) {
	admission := &toolBodyAdmission{token: request.executionToken, agent: request.Agent, name: request.Name, arguments: append([]byte(nil), request.Arguments...), owner: request.owner, profile: request.profileLease}
	admission.active.Store(true)
	return context.WithValue(ctx, toolBodyAdmissionKey{}, admission), admission.retire
}
func hasToolBodyAdmission(ctx context.Context, request ToolRequest) bool {
	if ctx == nil {
		return false
	}
	admission, _ := ctx.Value(toolBodyAdmissionKey{}).(*toolBodyAdmission)
	token, ok := toolExecutionFromContext(ctx)
	return admission != nil && admission.active.Load() && ok && token == request.executionToken && admission.token == token && admission.agent == request.Agent && admission.name == request.Name && bytes.Equal(admission.arguments, request.Arguments) && admission.owner == request.owner && admission.profile == request.profileLease
}

// deriveToolBodyAdmission linearizes child admission with parent sealing. The
// parent owns every accepted child's lifetime until its synchronous retirement.
func deriveToolBodyAdmission(ctx context.Context, outer, native ToolRequest) (context.Context, func(), func(), error) {
	if ctx == nil {
		return nil, nil, nil, errors.New("harness: missing parent body admission")
	}
	parent, _ := ctx.Value(toolBodyAdmissionKey{}).(*toolBodyAdmission)
	if parent == nil {
		return nil, nil, nil, errors.New("harness: missing parent body admission")
	}
	// Context access can invoke user-defined Value methods; resolve it before
	// entering the retirement critical section. Only immutable fields remain.
	token, validToken := toolExecutionFromContext(ctx)
	parent.mu.Lock()
	defer parent.mu.Unlock()
	if !parent.active.Load() || !validToken || token != outer.executionToken || parent.token != token || parent.agent != outer.Agent || parent.name != outer.Name || !bytes.Equal(parent.arguments, outer.Arguments) || parent.owner != outer.owner || parent.profile != outer.profileLease {
		return nil, nil, nil, errors.New("harness: parent body admission expired before captured dispatch")
	}
	parent.children++
	childCtx, retire := withToolBodyAdmission(ctx, native)
	var once sync.Once
	release := func() { once.Do(parent.releaseChild) }
	return childCtx, retire, release, nil
}

func (admission *toolBodyAdmission) retire() {
	admission.retireOnce.Do(func() {
		// Only framework body defers retire markers. Never wait while holding the
		// admission mutex or invoke callbacks under it. Admitted work is joined,
		// not cancelled merely because its enclosing callback returned.
		admission.mu.Lock()
		admission.active.Store(false)
		var wait chan struct{}
		if admission.children > 0 {
			admission.childrenDone = make(chan struct{})
			wait = admission.childrenDone
		}
		admission.mu.Unlock()
		if wait != nil {
			<-wait
		}
		admission.mu.Lock()
		cleanups := admission.cleanups
		admission.cleanups = nil
		admission.mu.Unlock()
		for i := len(cleanups) - 1; i >= 0; i-- {
			cleanups[i]()
		}
	})
}
func (admission *toolBodyAdmission) releaseChild() {
	admission.mu.Lock()
	defer admission.mu.Unlock()
	admission.children--
	if admission.children == 0 && admission.childrenDone != nil {
		close(admission.childrenDone)
		admission.childrenDone = nil
	}
}
