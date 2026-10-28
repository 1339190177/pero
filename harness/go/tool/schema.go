package tool

import (
	"encoding/json"
	"fmt"

	"github.com/google/jsonschema-go/jsonschema"
)

// CompileInputSchema validates and resolves an object-rooted tool schema.
func CompileInputSchema(name string, raw json.RawMessage) (*jsonschema.Resolved, error) {
	if len(raw) == 0 || !json.Valid(raw) {
		return nil, fmt.Errorf("harness: tool %q has invalid input schema", name)
	}
	var schema jsonschema.Schema
	if err := json.Unmarshal(raw, &schema); err != nil {
		return nil, fmt.Errorf("harness: tool %q has invalid input schema: %w", name, err)
	}
	if schema.Type != "object" {
		return nil, fmt.Errorf("harness: tool %q input schema must describe an object", name)
	}
	resolved, err := schema.Resolve(nil)
	if err != nil {
		return nil, fmt.Errorf("harness: tool %q has unsupported input schema: %w", name, err)
	}
	return resolved, nil
}

// ValidateArguments decodes and validates one tool argument object against a
// previously compiled schema.
func ValidateArguments(validator *jsonschema.Resolved, raw json.RawMessage) error {
	if len(raw) == 0 {
		raw = json.RawMessage(`{}`)
	}
	var arguments any
	if err := json.Unmarshal(raw, &arguments); err != nil {
		return err
	}
	return validator.Validate(arguments)
}
