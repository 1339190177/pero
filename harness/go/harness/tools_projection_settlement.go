package harness

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"reflect"
	"sync"
)

// One contract is created per admitted execution. Cache success AND uncertain
// failure so a lost bridge reply can never replay a spill side effect.
type toolProjectionSettlement struct {
	once       sync.Once
	callback   func(context.Context, ToolRequest, ToolResult) (ToolOutputProjection, error)
	accepted   ToolResult
	projection ToolOutputProjection
	err        error
}

func (settlement *toolProjectionSettlement) invoke(ctx context.Context, request ToolRequest, result ToolResult) (ToolOutputProjection, error) {
	settlement.once.Do(func() {
		settlement.accepted = cloneToolResult(result)
		settlement.projection, settlement.err = callToolProjectionSettlement(ctx, request, cloneToolResult(result), settlement.callback)
		settlement.projection.Content = cloneContentBlocks(settlement.projection.Content)
		settlement.projection.Meta = append([]byte(nil), settlement.projection.Meta...)
	})
	if !bytes.Equal(result.Value, settlement.accepted.Value) || !reflect.DeepEqual(result.Content, settlement.accepted.Content) || !bytes.Equal(result.Meta, settlement.accepted.Meta) {
		return ToolOutputProjection{}, errors.New("harness: repeated output settlement changed its accepted pair")
	}
	return ToolOutputProjection{Content: cloneContentBlocks(settlement.projection.Content), Meta: append([]byte(nil), settlement.projection.Meta...)}, settlement.err
}
func callToolProjectionSettlement(ctx context.Context, request ToolRequest, result ToolResult, callback func(context.Context, ToolRequest, ToolResult) (ToolOutputProjection, error)) (projection ToolOutputProjection, err error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			err = fmt.Errorf("source output settlement panicked: %v", recovered)
		}
	}()
	if err = contextFailure(ctx); err != nil {
		return projection, err
	}
	if request.Agent != nil {
		operation, beginErr := request.Agent.beginOperation(ctx)
		if beginErr != nil {
			return projection, beginErr
		}
		defer operation.release()
		ctx = operation.context
	}
	if request.profileLease != nil {
		if !request.profileLease.active() {
			return projection, ErrProfileOwnerUnavailable
		}
		ctx = request.profileLease.CallContext(ctx)
	}
	if request.owner != nil {
		call, beginErr := request.owner.begin(ctx)
		if beginErr != nil {
			return projection, beginErr
		}
		defer call.release()
		ctx = call.context
	}
	request.Arguments = append([]byte(nil), request.Arguments...)
	projection, err = callback(ctx, request, result)
	if err == nil {
		err = contextFailure(ctx)
	}
	if err == nil && request.Agent != nil {
		err = request.Agent.operationAdmissionError()
	}
	if err == nil && request.owner != nil {
		err = request.owner.admissionError()
	}
	if err == nil && request.profileLease != nil && !request.profileLease.active() {
		err = ErrProfileOwnerUnavailable
	}
	return projection, err
}
func settleToolProjection(ctx context.Context, request ToolRequest, result ToolResult) ToolResult {
	if request.output == nil || request.output.settlement == nil || result.IsError {
		return result
	}
	if !request.dshOutputProjection {
		return toolOutputError(request, result, errors.New("source output settlement lacks exact admitted projection mode"))
	}
	if contextFailure(ctx) != nil {
		return toolPolicyErrorResult("tool output settlement aborted", "AbortError", ToolAborted, result.AdditionalContexts)
	}
	projection, err := request.output.settlement.invoke(ctx, request, result)
	if err != nil {
		if errors.Is(err, ErrToolOwnerRetired) || errors.Is(err, ErrProfileOwnerUnavailable) || errors.Is(err, ErrAgentRetired) {
			return retiredToolOwnerResult(result.AdditionalContexts)
		}
		if contextFailure(ctx) != nil {
			return toolPolicyErrorResult("tool output settlement aborted", "AbortError", ToolAborted, result.AdditionalContexts)
		}
		return toolOutputError(request, result, err)
	}
	result.Content = cloneContentBlocks(projection.Content)
	result.Meta = append([]byte(nil), projection.Meta...)
	return result
}
