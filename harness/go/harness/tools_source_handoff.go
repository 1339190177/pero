package harness

import (
	"bytes"
	"context"
	"errors"
	"sync/atomic"
)

// sourceFinalContentTransfer changes only the last presentation projector of
// one newly admitted native execution. The original capability identity stays
// intact for captured inner dispatch. It never disables output validation,
// rendering, policy, spill, observers, cleanup or owner admission.
type sourceFinalContentTransfer struct {
	claimed    atomic.Bool
	capability *nativeFinalContent
	agent      *Agent
	owner      *toolOwner
	profile    *ProfileOwnerLease
	name       string
	arguments  []byte
}

func (transfer *sourceFinalContentTransfer) claim(request ToolRequest, capability *nativeFinalContent) (func(ToolResult) (ToolOutputProjection, error), error) {
	if transfer == nil || !transfer.claimed.CompareAndSwap(false, true) {
		return nil, errors.New("harness: source presentation transfer was already claimed")
	}
	if transfer.capability != capability || transfer.agent != request.Agent || transfer.owner != request.owner || transfer.profile != request.profileLease || transfer.name != request.Name || !bytes.Equal(transfer.arguments, request.Arguments) || !request.executionToken.Valid() {
		return nil, errors.New("harness: source presentation transfer does not match native admission")
	}
	return func(result ToolResult) (ToolOutputProjection, error) {
		return ToolOutputProjection{Content: result.Content, Meta: result.Meta}, nil
	}, nil
}

// ExecuteCaptured is only for a nested source execution that already installed
// its synchronous finalizer from TakeSnapshot. It occupies one execution
// attempt before any callback, then runs the ordinary native pipeline against
// the original immutable definition. The receipt pins a selection; it grants
// no effect authority, approval, body token or bypass of current restrictions.
// Prepared top-level native dispatch must retain its existing outer finalizer
// and must not use this nested handoff.
func (receipt *SourceFinalContentReceipt) ExecuteCaptured(ctx context.Context, request ToolRequest, sourceExecutionID string) (ToolOutcome, error) {
	if receipt == nil {
		return ToolOutcome{}, errors.New("harness: missing source execution handoff")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	receipt.mu.Lock()
	if receipt.released || receipt.executed {
		receipt.mu.Unlock()
		return ToolOutcome{}, errors.New("harness: source execution handoff is unavailable or already attempted")
	}
	receipt.executed = true
	executionCtx, cancel := context.WithCancel(ctx)
	receipt.executionCancel = cancel
	capture := receipt.capture
	transferred := receipt.transferred
	matches := capture != nil && receipt.sourceExecutionID == sourceExecutionID && bytes.Equal(receipt.arguments, request.Arguments) && capture.agent == request.Agent && capture.tool.Schema.Name == request.Name
	receipt.mu.Unlock()
	defer func() { cancel(); receipt.mu.Lock(); receipt.executionCancel = nil; receipt.mu.Unlock() }()
	if !transferred || !matches {
		return ToolOutcome{}, errors.New("harness: source execution does not match its transferred presentation receipt")
	}
	base := capture.base
	selected := cloneTool(capture.tool)
	selected.nativeFinalTransfer = &sourceFinalContentTransfer{capability: selected.nativeFinalContent, agent: capture.agent, owner: selected.owner, profile: selected.profileLease, name: selected.Schema.Name, arguments: append([]byte(nil), request.Arguments...)}
	original := selected
	selected.Execute = nil
	selected.ExecuteRequest = func(bodyCtx context.Context, bodyRequest ToolRequest) (ToolResult, error) {
		if err := receipt.admitCapturedBody(base, bodyRequest); err != nil {
			return ToolResult{}, err
		}
		if original.ExecuteRequest != nil {
			return original.ExecuteRequest(bodyCtx, bodyRequest)
		}
		return original.Execute(bodyCtx, bodyRequest.Arguments)
	}
	prepare := func(context.Context, *Agent, string) (Tool, bool, error) {
		if !base.catalogIdentity.active() {
			return Tool{}, false, ErrToolOwnerRetired
		}
		return cloneTool(selected), true, nil
	}
	// executeWithResolver revalidates exact owner/profile and the captured
	// selection fingerprint after native Before, without invoking Resolve again.
	return base.registry.executeWithResolver(executionCtx, request, true, func(context.Context, *Agent, string) (Tool, bool, error) { return cloneTool(selected), true, nil }, prepare)
}

func (receipt *SourceFinalContentReceipt) admitCapturedBody(base ToolCatalogBase, request ToolRequest) error {
	receipt.mu.Lock()
	defer receipt.mu.Unlock()
	capture := receipt.capture
	if receipt.released || receipt.bodyAdmitted || !base.catalogIdentity.active() || capture == nil || capture.agent != request.Agent || capture.tool.Schema.Name != request.Name || !bytes.Equal(receipt.arguments, request.Arguments) {
		return errors.New("harness: source execution owner or arguments changed before body admission")
	}
	receipt.bodyAdmitted = true
	return nil
}
