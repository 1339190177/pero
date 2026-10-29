package harness

import (
	"encoding/json"

	"github.com/google/jsonschema-go/jsonschema"
	harnesstool "github.com/phodal/pero/harness/go/tool"
)

func ValidateObjectJSONSchema(raw json.RawMessage) error {
	return harnesstool.ValidateObjectJSONSchema(raw)
}

func compileToolInputSchema(name string, raw json.RawMessage) (*jsonschema.Resolved, error) {
	return harnesstool.CompileInputSchema(name, raw)
}

func validateToolArguments(validator *jsonschema.Resolved, raw json.RawMessage) error {
	return harnesstool.ValidateArguments(validator, raw)
}
