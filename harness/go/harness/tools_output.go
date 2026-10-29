package harness

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"

	"github.com/google/jsonschema-go/jsonschema"
	harnesstool "github.com/phodal/pero/harness/go/tool"
)

// ToolOutputProjection contains only replayable presentation. Output renderers
// cannot confer success, conclude a turn, or attach deferred user contexts.
type ToolOutputProjection struct {
	Content []ContentBlock
	Meta    json.RawMessage
}

type toolOutputContract struct {
	validator       *jsonschema.Resolved
	render          func(context.Context, ToolRequest, json.RawMessage) (ToolOutputProjection, error)
	preserveInitial bool
	settlement      *toolProjectionSettlement
	notification    *toolTerminalNotification
	errorMode       ToolErrorOutputMode
}

func outputContract(tool Tool) *toolOutputContract {
	if tool.outputValidator == nil {
		return nil
	}
	contract := &toolOutputContract{validator: tool.outputValidator, render: tool.RenderOutput, preserveInitial: tool.PreserveInitialOutputPair, errorMode: tool.ErrorOutputMode}
	if tool.SettleOutput != nil {
		contract.settlement = &toolProjectionSettlement{callback: tool.SettleOutput}
	}
	if tool.NotifyTerminal != nil {
		contract.notification = &toolTerminalNotification{callback: tool.NotifyTerminal}
	}
	return contract
}

func cloneToolResult(result ToolResult) ToolResult {
	result.Value = append(json.RawMessage(nil), result.Value...)
	result.Content = cloneContentBlocks(result.Content)
	result.Meta = append(json.RawMessage(nil), result.Meta...)
	result.AdditionalContexts = append([]DeferredContext(nil), result.AdditionalContexts...)
	for i := range result.AdditionalContexts {
		result.AdditionalContexts[i].Content = cloneContentBlocks(result.AdditionalContexts[i].Content)
		result.AdditionalContexts[i].Source = append(json.RawMessage(nil), result.AdditionalContexts[i].Source...)
	}
	return result
}

func toolOutputError(request ToolRequest, result ToolResult, err error) ToolResult {
	return toolPolicyErrorResult(fmt.Sprintf("tool %q returned invalid output: %v", request.Name, err), "ToolOutputError", "INVALID_TOOL_OUTPUT", result.AdditionalContexts)
}

func renderToolOutput(ctx context.Context, request ToolRequest, result ToolResult) (projected ToolResult) {
	projected = result
	defer func() {
		if recovered := recover(); recovered != nil {
			projected = toolOutputError(request, result, fmt.Errorf("renderer panicked: %v", recovered))
		}
	}()
	arguments := append(json.RawMessage(nil), request.Arguments...)
	request.Arguments = arguments
	projection, err := request.output.render(ctx, request, append(json.RawMessage(nil), result.Value...))
	if err != nil {
		if errors.Is(err, ErrToolOwnerRetired) || errors.Is(err, ErrProfileOwnerUnavailable) {
			return retiredToolOwnerResult(result.AdditionalContexts)
		}
		if contextFailure(ctx) != nil {
			return toolPolicyErrorResult("tool output rendering aborted", "AbortError", ToolAborted, result.AdditionalContexts)
		}
		return toolOutputError(request, result, err)
	}
	projected.Content = cloneContentBlocks(projection.Content)
	projected.Meta = append(json.RawMessage(nil), projection.Meta...)
	return sealCanonicalPresentation(request, projected)
}

// normalizeToolOutput snapshots the body's value before policy or observers
// can retain it. A trusted bridge may supply an already rendered value pair;
// without a renderer, later canonical-value replacements fail closed.
func normalizeToolOutput(ctx context.Context, request ToolRequest, result ToolResult) ToolResult {
	if request.output == nil {
		return result
	}
	result = cloneToolResult(result)
	if result.IsError {
		result.Value, result.ConcludesTurn = nil, false
		return harnesstool.ClearPresentation(result)
	}
	if err := harnesstool.ValidateOutput(request.output.validator, result.Value); err != nil {
		return toolOutputError(request, result, err)
	}
	if request.output.render != nil && !request.output.preserveInitial {
		return renderToolOutput(ctx, request, result)
	}
	return sealCanonicalPresentation(request, result)
}

func normalizeToolOutputAfter(ctx context.Context, request ToolRequest, before, after ToolResult) ToolResult {
	if request.output == nil {
		return after
	}
	after = cloneToolResult(after)
	if after.IsError {
		after = normalizeToolErrorOutputAfter(request, before, after)
		after.Value, after.ConcludesTurn = nil, false
		return harnesstool.ClearPresentation(after)
	}
	if before.IsError {
		return toolOutputError(request, after, fmt.Errorf("post-execute cannot replace a failed canonical result with success"))
	}
	if err := harnesstool.ValidateOutput(request.output.validator, after.Value); err != nil {
		return toolOutputError(request, after, err)
	}
	if !bytes.Equal(before.Value, after.Value) {
		if !reflect.DeepEqual(before.Content, after.Content) || !bytes.Equal(before.Meta, after.Meta) {
			return toolOutputError(request, after, fmt.Errorf("post-execute cannot replace both canonical value and presentation"))
		}
		if request.output.render == nil {
			return toolOutputError(request, after, fmt.Errorf("canonical value replacement requires an output renderer"))
		}
		return renderToolOutput(ctx, request, after)
	}
	return harnesstool.InheritPresentation(after, before)
}

func observerToolOutcome(outcome ToolOutcome) ToolOutcome {
	if outcome.Request.output != nil {
		outcome.Result = cloneToolResult(outcome.Result)
	}
	return outcome
}
