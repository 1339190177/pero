package harness

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"strconv"
	"sync"
)

// SourceFinalContentSnapshot is presentation-only data for the authenticated
// bridge-private channel. Never place it in ToolRequest JSON, tool schemas or
// model-visible Value. It cannot authorize a native body or registry operation.
type SourceFinalContentSnapshot struct {
	SourceExecutionID string  `json:"sourceExecutionId"`
	Projection        string  `json:"projection"`
	OutputLimitBytes  *string `json:"outputLimitBytes,omitempty"`
}

// SourceFinalContentReceipt binds an immutable presentation snapshot to one
// captured native definition and source execution. SourceExecutionID is only
// correlation; the opaque receipt and exact capture are the trusted boundary.
type SourceFinalContentReceipt struct {
	mu                sync.Mutex
	capture           *toolDispatchCapture
	sourceExecutionID string
	arguments         json.RawMessage
	snapshot          SourceFinalContentSnapshot
	released          bool
	transferred       bool
	executed          bool
	bodyAdmitted      bool
	executionCancel   context.CancelFunc
}

// PrepareSourceFinalContent is non-effecting and permits already-aborted source
// executions to receive their error presentation cap. Source must call its real
// createExecution once first, then supply only the frozen argument snapshot;
// failed materialization supplies nil. It must never reread caller getters.
func (b ToolCatalogBase) PrepareSourceFinalContent(_ context.Context, captured Tool, sourceExecutionID string, frozenArguments json.RawMessage) (*SourceFinalContentReceipt, error) {
	capture := captured.dispatchCapture
	if b.registry == nil || !b.catalogIdentity.active() || capture == nil || capture.base != b || capture.agent == nil || sourceExecutionID == "" || capture.tool.Schema.Name != captured.Schema.Name {
		return nil, errors.New("harness: source final-content preparation requires an exact captured execution")
	}
	tool := capture.tool
	if capture.agent.retired.Load() {
		return nil, ErrToolOwnerRetired
	}
	if tool.owner == nil || tool.owner.registry != b.registry || !tool.owner.run.Scope.Contains(capture.agent.Scope()) {
		return nil, ErrToolOwnerRetired
	}
	if err := tool.owner.admissionError(); err != nil {
		return nil, err
	}
	if tool.profileLease != nil && !tool.profileLease.active() {
		return nil, ErrProfileOwnerUnavailable
	}
	if tool.nativeFinalContent == nil || tool.nativeFinalContent.sourceSnapshot == nil {
		return nil, errors.New("harness: captured tool has no source presentation snapshot capability")
	}
	arguments := append(json.RawMessage(nil), frozenArguments...)
	request := ToolRequest{Agent: capture.agent, Name: tool.Schema.Name, Arguments: arguments, owner: tool.owner, profileLease: tool.profileLease}
	limit, err := tool.nativeFinalContent.sourceSnapshot(request)
	if err != nil {
		return nil, err
	}
	// Snapshot lookup may wait behind registry work. Retirement that wins that
	// interval must fence this new receipt, without revoking older pure receipts.
	if !b.catalogIdentity.active() || capture.agent.retired.Load() {
		return nil, ErrToolOwnerRetired
	}
	if err := tool.owner.admissionError(); err != nil {
		return nil, err
	}
	if tool.profileLease != nil && !tool.profileLease.active() {
		return nil, ErrProfileOwnerUnavailable
	}
	snapshot := SourceFinalContentSnapshot{SourceExecutionID: sourceExecutionID, Projection: tool.Schema.OutputProjection}
	if limit != nil {
		if *limit <= 0 {
			return nil, errors.New("harness: source presentation cap must be positive when present")
		}
		copied := strconv.Itoa(*limit)
		snapshot.OutputLimitBytes = &copied
	}
	return &SourceFinalContentReceipt{capture: capture, sourceExecutionID: sourceExecutionID, arguments: arguments, snapshot: snapshot}, nil
}

// TakeSnapshot transfers the immutable data exactly once to the source's exact
// created execution closure. The bridge must bind that closure to the original
// source token, never a later token reusing a call ID. Owner retirement does not
// invalidate already captured pure presentation, but a new capture cannot take it.
func (receipt *SourceFinalContentReceipt) TakeSnapshot(captured Tool, sourceExecutionID string, frozenArguments json.RawMessage) (SourceFinalContentSnapshot, error) {
	if receipt == nil {
		return SourceFinalContentSnapshot{}, errors.New("harness: missing source presentation receipt")
	}
	receipt.mu.Lock()
	defer receipt.mu.Unlock()
	if receipt.released || receipt.transferred || receipt.capture != captured.dispatchCapture || receipt.sourceExecutionID != sourceExecutionID || !bytes.Equal(receipt.arguments, frozenArguments) {
		return SourceFinalContentSnapshot{}, errors.New("harness: source presentation receipt does not match its exact execution")
	}
	snapshot := receipt.snapshot
	if snapshot.OutputLimitBytes != nil {
		value := *snapshot.OutputLimitBytes
		snapshot.OutputLimitBytes = &value
	}
	receipt.transferred = true
	receipt.snapshot = SourceFinalContentSnapshot{}
	return snapshot, nil
}
func (receipt *SourceFinalContentReceipt) releaseLocked() {
	receipt.released = true
	receipt.capture = nil
	receipt.arguments = nil
	receipt.sourceExecutionID = ""
	receipt.snapshot = SourceFinalContentSnapshot{}
}

// Release covers source finalization, preparation failure and bridge teardown.
func (receipt *SourceFinalContentReceipt) Release() {
	if receipt == nil {
		return
	}
	receipt.mu.Lock()
	cancel := receipt.executionCancel
	receipt.executionCancel = nil
	receipt.releaseLocked()
	receipt.mu.Unlock()
	if cancel != nil {
		cancel()
	}
}
