package harness

import (
	"bytes"
	"context"
	"errors"
	harnesstool "github.com/phodal/pero/harness/go/tool"
)

type dshOutputProjectionKey struct{}
type dshOutputProjectionMode struct {
	capture *toolDispatchCapture
	token   ToolExecutionToken
	pending bool
}

// WithDSHOutputProjection explicitly selects the installed official source
// presentation/spill policy for one authenticated capture. Only the trusted
// Host adapter may call it after installing that policy for the projector ID.
// No JSON/profile option exposes it, and it grants no execution authority.
func WithDSHOutputProjection(ctx context.Context, captured Tool) (context.Context, error) {
	capture := captured.dispatchCapture
	if capture == nil || capture.agent == nil || capture.base.registry == nil || capture.tool.outputValidator == nil || capture.tool.Schema.OutputProjection == "" {
		return nil, errors.New("harness: DSH output projection requires an exact declared base capture")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	return context.WithValue(ctx, dshOutputProjectionKey{}, &dshOutputProjectionMode{capture: capture, pending: true}), nil
}
func UsesDSHOutputProjection(ctx context.Context) bool {
	if ctx == nil {
		return false
	}
	mode, _ := ctx.Value(dshOutputProjectionKey{}).(*dshOutputProjectionMode)
	if mode == nil {
		return false
	}
	if mode.pending {
		return true
	} // exact captured pure-renderer invocation
	token, ok := toolExecutionFromContext(ctx)
	return ok && token == mode.token
}
func requestedDSHProjection(ctx context.Context) *dshOutputProjectionMode {
	if ctx == nil {
		return nil
	}
	mode, _ := ctx.Value(dshOutputProjectionKey{}).(*dshOutputProjectionMode)
	if mode != nil && mode.pending {
		return mode
	}
	return nil
}

// BindDSHOutputProjection enrolls only this exact authoritative proxy in the
// installed source-owned presentation/spill policy. The caller must establish
// the official post hook before exposing the proxy. Native direct calls keep
// native presentation and spill ownership.
func (b ToolCatalogBase) BindDSHOutputProjection(proxy, captured Tool) (Tool, error) {
	proxy, err := b.BindCapturedOwner(proxy, captured)
	if err != nil {
		return Tool{}, err
	}
	capture := captured.dispatchCapture
	if capture.tool.outputValidator == nil || capture.tool.Schema.OutputProjection == "" || !sameOutputProjectionContract(proxy.Schema, capture.tool.Schema) {
		return Tool{}, errors.New("harness: DSH output projection proxy contract differs from its capture")
	}
	proxy.sourceProjectionCapture = capture
	return proxy, nil
}
func sameOutputProjectionContract(a, b ToolSchema) bool {
	return a.Name == b.Name && a.OutputProjection == b.OutputProjection && bytes.Equal(a.OutputSchema, b.OutputSchema) && bytes.Equal(a.OutputProjectionConfig, b.OutputProjectionConfig)
}
func admitDSHProjection(ctx context.Context, request ToolRequest, tool Tool, mode *dshOutputProjectionMode) (context.Context, bool, error) {
	capture := tool.sourceProjectionCapture
	if capture == nil && mode != nil {
		capture = mode.capture
	}
	ctx = context.WithValue(ctx, dshOutputProjectionKey{}, (*dshOutputProjectionMode)(nil))
	if capture == nil {
		return ctx, false, nil
	}
	if capture.agent != request.Agent || capture.tool.Schema.Name != request.Name || tool.owner != capture.tool.owner || tool.profileLease != capture.tool.profileLease || !sameOutputProjectionContract(tool.Schema, capture.tool.Schema) ||
		(tool.sourceProjectionCapture != capture && (tool.outputValidator != capture.tool.outputValidator || tool.inputValidator != capture.tool.inputValidator || tool.owner != capture.tool.owner || tool.profileLease != capture.tool.profileLease)) {
		return ctx, false, errors.New("harness: DSH output projection does not match the admitted capture")
	}
	return context.WithValue(ctx, dshOutputProjectionKey{}, &dshOutputProjectionMode{capture: capture, token: request.executionToken}), true, nil
}

type canonicalPresentationBinding struct{ request ToolRequest }

func sealCanonicalPresentation(request ToolRequest, result ToolResult) ToolResult {
	if request.output == nil || result.IsError {
		return harnesstool.ClearPresentation(result)
	}
	return harnesstool.CapturePresentation(result, &canonicalPresentationBinding{request: request})
}

// CanonicalPresentationChanges compares the final accepted pair to its exact
// normalized native presentation. Missing, value-mismatched, or retired
// provenance fails closed. It never parses content into a canonical value.
func CanonicalPresentationChanges(result ToolResult) (contentChanged, metaChanged bool, err error) {
	opaque, contentChanged, metaChanged, valid := harnesstool.InspectPresentation(result)
	binding, ok := opaque.(*canonicalPresentationBinding)
	if !valid || !ok || binding == nil || binding.request.output == nil || !binding.request.executionToken.Valid() {
		return false, false, errors.New("harness: canonical presentation receipt is missing or mismatched")
	}
	request := binding.request
	if request.Agent != nil && request.Agent.retired.Load() {
		return false, false, ErrToolOwnerRetired
	}
	if request.owner != nil && request.owner.admissionError() != nil {
		return false, false, ErrToolOwnerRetired
	}
	if request.profileLease != nil && !request.profileLease.active() {
		return false, false, ErrProfileOwnerUnavailable
	}
	return contentChanged, metaChanged, nil
}
