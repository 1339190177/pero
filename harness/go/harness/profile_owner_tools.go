package harness

import (
	"context"
	"encoding/json"
	"errors"
)

// WrapTool supplies the transaction fence around the native registry's exact
// activation drain. Neither candidates nor retained old callbacks can execute
// outside their committed profile incarnation. The registry also hides this
// tool from discovery until the same lease is admitted.
func (l *ProfileOwnerLease) WrapTool(tool Tool) Tool {
	tool.profileLease = l
	if render := tool.RenderOutput; render != nil {
		tool.RenderOutput = func(ctx context.Context, request ToolRequest, value json.RawMessage) (ToolOutputProjection, error) {
			if !l.active() {
				return ToolOutputProjection{}, ErrProfileOwnerUnavailable
			}
			ctx, release, err := l.BeginOperation(ctx)
			if err != nil {
				return ToolOutputProjection{}, err
			}
			defer release()
			result, err := render(l.CallContext(ctx), request, value)
			if !l.active() {
				return ToolOutputProjection{}, errors.Join(err, ErrProfileOwnerUnavailable)
			}
			return result, err
		}
	}
	if execute := tool.Execute; execute != nil {
		tool.Execute = func(ctx context.Context, raw json.RawMessage) (ToolResult, error) {
			if !l.active() {
				return ToolResult{}, ErrProfileOwnerUnavailable
			}
			ctx, release, err := l.BeginOperation(ctx)
			if err != nil {
				return ToolResult{}, err
			}
			defer release()
			result, err := execute(l.CallContext(ctx), raw)
			if !l.active() {
				return result, errors.Join(err, ErrProfileOwnerUnavailable)
			}
			return result, err
		}
	}
	if execute := tool.ExecuteRequest; execute != nil {
		tool.ExecuteRequest = func(ctx context.Context, request ToolRequest) (ToolResult, error) {
			if !l.active() {
				return ToolResult{}, ErrProfileOwnerUnavailable
			}
			ctx, release, err := l.BeginOperation(ctx)
			if err != nil {
				return ToolResult{}, err
			}
			defer release()
			result, err := execute(l.CallContext(ctx), request)
			if !l.active() {
				return result, errors.Join(err, ErrProfileOwnerUnavailable)
			}
			return result, err
		}
	}
	if classify := tool.IsConcurrencySafe; classify != nil {
		tool.IsConcurrencySafe = func(ctx context.Context, request ToolRequest) bool {
			ctx, release, err := l.BeginOperation(ctx)
			if err != nil {
				return false
			}
			defer release()
			return l.active() && classify(l.CallContext(ctx), request) && l.active()
		}
	}
	if reason := tool.ApprovalReason; reason != nil {
		tool.ApprovalReason = func(request ToolRequest) string {
			if !l.active() {
				return ""
			}
			_, release, err := l.BeginOperation(context.Background())
			if err != nil {
				return ""
			}
			defer release()
			result := reason(request)
			if !l.active() {
				return ""
			}
			return result
		}
	}
	if observe := tool.ObserveResult; observe != nil {
		tool.ObserveResult = func(outcome ToolOutcome) {
			_, release, err := l.BeginOperation(context.Background())
			if err != nil {
				return
			}
			defer release()
			if l.active() {
				observe(outcome)
			}
		}
	}
	return tool
}
