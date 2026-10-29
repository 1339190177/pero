package tool

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestValidateObjectJSONSchemaMatchesStructuredSubset(t *testing.T) {
	valid := []json.RawMessage{
		json.RawMessage(`{"type":"object","properties":{"answer":{"type":"number"},"note":{"type":"string","enum":["ok","retry"]}},"required":["answer"],"additionalProperties":false}`),
		json.RawMessage(`{"type":"object","properties":{"choice":{"oneOf":[{"type":"string"},{"type":"null"}]}}}`),
	}
	for _, schema := range valid {
		if err := ValidateObjectJSONSchema(schema); err != nil {
			t.Fatalf("valid schema %s: %v", schema, err)
		}
	}
	invalid := []struct {
		schema json.RawMessage
		part   string
	}{
		{json.RawMessage(`{"type":"string"}`), `schema.type must be "object"`},
		{json.RawMessage(`{"type":"object","minProperties":1}`), "not a supported keyword"},
		{json.RawMessage(`{"type":"object","required":["missing"]}`), "not in properties"},
		{json.RawMessage(`{"type":"object","properties":{"x":{"type":["string","null"]}}}`), "type must be one of"},
		{json.RawMessage(`{"type":"object","properties":{"x":{"oneOf":[]}}}`), "at least two schemas"},
		{json.RawMessage(`{"type":"object","properties":{"x":{"type":"string","enum":["a"],"const":"b"}}}`), "const must be one of"},
	}
	for _, fixture := range invalid {
		err := ValidateObjectJSONSchema(fixture.schema)
		if err == nil || !strings.Contains(err.Error(), fixture.part) {
			t.Fatalf("invalid schema %s error=%v want %q", fixture.schema, err, fixture.part)
		}
	}
}

