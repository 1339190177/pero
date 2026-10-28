package harness

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"reflect"

	harnesstool "github.com/phodal/pero/harness/go/tool"
)

// WithDSHTodoNumberCompatibility selects bounded alpha.3 last-member number
// admission for trusted Todo semantic registration. It is independent of error
// presentation and cannot be selected by model input, metadata or Tool JSON.
// Registration still requires the exact bounded Todo input contract (including
// the existing closed-root variant). All other tools retain legacy admission.
func WithDSHTodoNumberCompatibility(tool Tool) Tool {
	tool.dshTodoNumberCompatibility = true
	return tool
}

func bindCapturedDSHTodoNumbers(proxy, captured Tool) (Tool, error) {
	// A proxy must never add this mode to an unmarked native capture.
	proxy.dshTodoNumberCompatibility = captured.dshTodoNumberCompatibility
	if !captured.dshTodoNumberCompatibility {
		return proxy, nil
	}
	if captured.owner != nil {
		if err := captured.owner.admissionError(); err != nil {
			return Tool{}, err
		}
	}
	if err := checkDSHTodoSchemaBytes(proxy.Schema.InputSchema); err != nil {
		return Tool{}, err
	}
	var proxySchema, capturedSchema any
	if json.Unmarshal(proxy.Schema.InputSchema, &proxySchema) != nil || json.Unmarshal(captured.Schema.InputSchema, &capturedSchema) != nil || !reflect.DeepEqual(proxySchema, capturedSchema) {
		return Tool{}, errors.New("harness: Todo number compatibility requires the exact captured input contract")
	}
	// Keep the immutable definition's spelling/order through transport.
	proxy.Schema.InputSchema = append(json.RawMessage(nil), captured.Schema.InputSchema...)
	return proxy, nil
}

func validateRegisteredToolArguments(tool Tool, raw json.RawMessage) error {
	original := validateToolArguments(tool.inputValidator, raw)
	if original == nil || !tool.dshTodoNumberCompatibility {
		return original
	}
	return retryDSHTodoNumberArguments(tool, raw, original)
}

func retryDSHTodoNumberArguments(tool Tool, raw json.RawMessage, original error) error {
	// ValidateArguments decodes into any and returns encoding/json's error
	// directly. For that decode, this exact concrete type/target identifies a
	// float64 overflow. Do not unwrap policy/user errors or inspect error text.
	overflow, ok := original.(*json.UnmarshalTypeError)
	if !ok || overflow == nil || overflow.Type != reflect.TypeFor[float64]() {
		return original
	}
	selected, ok := dshTodoLastMemberArguments(raw)
	if !ok || validateToolArguments(tool.inputValidator, selected) != nil {
		return original
	}
	// This is only a temporary validator view. The original request and its
	// body-level selected-subtree Unicode guard retain the unchanged raw bytes.
	return nil
}

// dshTodoLastMemberArguments is an optional bounded validator/diagnostic view.
// The original request always retains its raw bytes and first-insertion order.
func dshTodoLastMemberArguments(raw json.RawMessage) (json.RawMessage, bool) {
	// Count every raw value, including overwritten members and open extras,
	// before allocating the optional last-key graph. These limits never cap
	// ordinary accepted inputs.
	if len(raw) > dshTodoArgumentBytes || !dshTodoJSONBudget(raw, dshTodoArgumentTokens, dshTodoArgumentDepth) {
		return nil, false
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	var value any
	if decoder.Decode(&value) != nil {
		return nil, false
	}
	var tail any
	if decoder.Decode(&tail) != io.EOF {
		return nil, false
	}
	selected, err := json.Marshal(value)
	return selected, err == nil
}

// projectRegisteredToolArgumentFailure preserves ordinary projection first.
// The fallback is passive: only a direct native float64 decode failure from
// the captured validator may justify inspecting Todo's selected last values.
func projectRegisteredToolArgumentFailure(tool Tool, raw json.RawMessage, original error) (ToolResult, bool) {
	if result, ok := projectArgumentFailure(tool, raw); ok {
		return result, true
	}
	projection := tool.argumentErrorProjection
	if !tool.dshTodoNumberCompatibility || projection == nil || !projection.todo || tool.inputValidator == nil ||
		(tool.owner != nil && tool.owner.admissionError() != nil) {
		return ToolResult{}, false
	}
	overflow, ok := original.(*json.UnmarshalTypeError)
	if !ok || overflow == nil || overflow.Type != reflect.TypeFor[float64]() {
		return ToolResult{}, false
	}
	// Canonicalization must not launder discarded invalid UTF-8 or UTF-16.
	if len(raw) > dshTodoArgumentBytes || harnesstool.ValidateNativeJSONDecoding(raw) != nil {
		return ToolResult{}, false
	}
	selected, ok := dshTodoLastMemberArguments(raw)
	if !ok {
		return ToolResult{}, false
	}
	var value any
	// Surviving overflow anywhere, including open extras, is not a schema
	// diagnostic. The same captured validator must still reject this view.
	if json.Unmarshal(selected, &value) != nil || validateToolArguments(tool.inputValidator, selected) == nil {
		return ToolResult{}, false
	}
	// The snapshot supplies selected values only. Original raw objects supply
	// key identity/order; marshaled Go maps would reorder unknown properties.
	return projectDSHTodoArgumentValueFailure(raw, value)
}
