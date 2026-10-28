package tool

import (
	"encoding/json"
	"fmt"
	"strconv"

	"github.com/google/jsonschema-go/jsonschema"
	"github.com/phodal/pero/harness/go/internal/jsonwire"
)

// CompileOutputSchema accepts the same enforced schema subset as DSH, with
// any JSON value root. An empty object declares unconstrained JSON, while an
// absent declaration leaves legacy rendered-result tools unchanged.
func CompileOutputSchema(name string, raw json.RawMessage) (*jsonschema.Resolved, error) {
	if err := validateOutputJSON(raw); err != nil {
		return nil, fmt.Errorf("harness: tool %q has invalid output schema: %w", name, err)
	}
	// Reuse the existing subset checker through an object property, rather
	// than weakening its structured-subagent object-root requirement.
	wrapper, err := json.Marshal(map[string]any{
		"type": "object", "properties": map[string]json.RawMessage{"value": raw},
		"required": []string{"value"}, "additionalProperties": false,
	})
	if err != nil {
		return nil, err
	}
	if err := ValidateObjectJSONSchema(wrapper); err != nil {
		return nil, fmt.Errorf("harness: tool %q has unsupported output schema: %w", name, err)
	}
	var schema jsonschema.Schema
	if err := json.Unmarshal(raw, &schema); err != nil {
		return nil, err
	}
	return schema.Resolve(nil)
}

// ValidateOutput checks a detached validation view without rewriting raw.
// The bounded lossless-number gate runs before decoding to float64: the
// validator's json.Number type handling differs from JSON numeric types.
func ValidateOutput(validator *jsonschema.Resolved, raw json.RawMessage) error {
	if validator == nil {
		return fmt.Errorf("canonical output requires a compiled schema")
	}
	if err := validateOutputJSON(raw); err != nil {
		return err
	}
	var value any
	if err := json.Unmarshal(raw, &value); err != nil {
		return err
	}
	return validator.Validate(value)
}

func validateOutputJSON(raw json.RawMessage) error {
	if err := ValidateNativeJSONDecoding(raw); err != nil {
		return err
	}
	if err := jsonwire.ValidateDSHJSONSnapshot(raw); err != nil {
		return err
	}
	return jsonwire.ValidateJavaScriptJSONNumbers(raw)
}

// ValidateNativeJSONDecoding guards a selected canonical domain before Go DTO
// decoding can replace invalid UTF-8 or lone UTF-16 escapes. This does not
// change admission of opaque raw JSON elsewhere in the runtime.
func ValidateNativeJSONDecoding(raw json.RawMessage) error {
	if len(raw) == 0 || !json.Valid(raw) {
		return fmt.Errorf("native JSON decoding requires one JSON value")
	}
	if err := jsonwire.ValidateJSONUTF8(raw); err != nil {
		return err
	}
	return validateOutputStringIdentity(raw)
}

// encoding/json replaces lone escaped UTF-16 surrogates with U+FFFD. Reject
// those inputs before constructing a Go validation view so const/enum and
// property names cannot be validated against a different string identity.
// Valid surrogate pairs, and literal backslash-u text, remain supported.
func validateOutputStringIdentity(raw []byte) error {
	for i := 0; i < len(raw); i++ {
		if raw[i] != '\\' {
			continue
		}
		i++
		if raw[i] != 'u' {
			continue
		}
		unit, _ := strconv.ParseUint(string(raw[i+1:i+5]), 16, 16)
		i += 4
		if unit < 0xd800 || unit > 0xdfff {
			continue
		}
		if unit <= 0xdbff && i+6 < len(raw) && raw[i+1] == '\\' && raw[i+2] == 'u' {
			low, err := strconv.ParseUint(string(raw[i+3:i+7]), 16, 16)
			if err == nil && low >= 0xdc00 && low <= 0xdfff {
				i += 6
				continue
			}
		}
		return fmt.Errorf("canonical output cannot contain a lone escaped UTF-16 surrogate")
	}
	return nil
}
