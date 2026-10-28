package tool

import (
	"encoding/json"
	"testing"
)

func TestCanonicalOutputSchemaAndExactJSON(t *testing.T) {
	for _, tc := range []struct{ name, schema, valid, invalid string }{
		{"null", `{"type":"null"}`, `null`, `{}`},
		{"string", `{"type":"string"}`, `"value"`, `1`},
		{"array", `{"type":"array","items":{"type":"boolean"}}`, `[true,false]`, `[1]`},
		{"fraction", `{"type":"number","const":0.1}`, `0.1`, `0.2`},
		{"enum", `{"type":"integer","enum":[1,2]}`, `2`, `3`},
		{"object", `{"type":"object","properties":{"n":{"type":"integer"}},"required":["n"],"additionalProperties":false}`, `{"n":7}`, `{"n":7,"extra":true}`},
		{"oneOf", `{"oneOf":[{"type":"null"},{"type":"string"}]}`, `null`, `false`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			validator, err := CompileOutputSchema(tc.name, json.RawMessage(tc.schema))
			if err != nil {
				t.Fatal(err)
			}
			if err := ValidateOutput(validator, json.RawMessage(tc.valid)); err != nil {
				t.Fatalf("valid output rejected: %v", err)
			}
			if err := ValidateOutput(validator, json.RawMessage(tc.invalid)); err == nil {
				t.Fatal("invalid output admitted")
			}
		})
	}
	any, err := CompileOutputSchema("any", json.RawMessage(`{}`))
	if err != nil {
		t.Fatal(err)
	}
	for _, raw := range []string{"", `null null`, `NaN`, `9007199254740993`, `1e999999999`, `-0`, `{"n":-0.0}`} {
		if err := ValidateOutput(any, json.RawMessage(raw)); err == nil {
			t.Fatalf("lossy or absent JSON admitted: %q", raw)
		}
	}
	if err := ValidateOutput(any, json.RawMessage(`9007199254740992`)); err != nil {
		t.Fatalf("exactly representable number rejected: %v", err)
	}
	for _, raw := range []string{"", `null`, `true`, `{"type":"number","minimum":1}`, `{"type":["null","string"]}`, `{"type":"number","const":9007199254740993}`} {
		if _, err := CompileOutputSchema("invalid", json.RawMessage(raw)); err == nil {
			t.Fatalf("unsupported or lossy output schema admitted: %q", raw)
		}
	}
}

func TestCanonicalOutputPreservesStringIdentity(t *testing.T) {
	validator, err := CompileOutputSchema("replacement", json.RawMessage(`{"type":"string","const":"�"}`))
	if err != nil {
		t.Fatal(err)
	}
	for _, raw := range []string{`"\ud800"`, `"\udfff"`, `"\ud800\u0041"`} {
		if err := ValidateOutput(validator, json.RawMessage(raw)); err == nil {
			t.Fatalf("lossy escaped string validated as replacement character: %s", raw)
		}
		if err := ValidateNativeJSONDecoding(json.RawMessage(raw)); err == nil {
			t.Fatalf("lossy DTO input admitted: %s", raw)
		}
	}
	if _, err := CompileOutputSchema("surrogate", json.RawMessage(`{"type":"string","const":"\ud800"}`)); err == nil {
		t.Fatal("lossy schema constant admitted")
	}
	for _, raw := range []string{`"�"`, `"\ud83d\ude00"`, `"\\ud800"`, `{"\ud83d\ude00":"ok"}`} {
		if err := ValidateNativeJSONDecoding(json.RawMessage(raw)); err != nil {
			t.Fatalf("lossless string rejected: %s: %v", raw, err)
		}
	}
	pair, err := CompileOutputSchema("pair", json.RawMessage(`{"type":"string","const":"\ud83d\ude00"}`))
	if err != nil {
		t.Fatal(err)
	}
	if err := ValidateOutput(pair, json.RawMessage(`"😀"`)); err != nil {
		t.Fatalf("valid surrogate pair identity changed: %v", err)
	}
}

