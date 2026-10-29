package harness

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math"
	"reflect"
	"sort"
	"strconv"
	"strings"

	harnesstool "github.com/phodal/pero/harness/go/tool"
)

// ToolArgumentErrorMode selects a passive diagnostic after ordinary schema
// validation rejects a call. It never changes input admission or runs a callback.
type ToolArgumentErrorMode uint8

const (
	ArgumentErrorsLegacy ToolArgumentErrorMode = iota
	// ArgumentErrorsDSHAlpha3Flat preserves alpha3 diagnostics for open objects
	// whose declared properties are string, number, or boolean scalars.
	ArgumentErrorsDSHAlpha3Flat
	// ArgumentErrorsDSHAlpha3Todo supports only the exact alpha3 Todo input
	// schema. It does not expand the flat mode or change input admission.
	ArgumentErrorsDSHAlpha3Todo
)

type dshArgumentErrorProjection struct {
	flat *dshFlatArgumentSchema
	todo bool
}

type dshFlatArgumentProperty struct {
	name string
	kind string
	enum []any
}

type dshFlatArgumentSchema struct {
	required   []string
	properties []dshFlatArgumentProperty
}

func compileArgumentErrorProjection(mode ToolArgumentErrorMode, raw json.RawMessage) (*dshArgumentErrorProjection, error) {
	switch mode {
	case ArgumentErrorsLegacy:
		return nil, nil
	case ArgumentErrorsDSHAlpha3Flat:
		flat, err := compileDSHFlatArgumentSchema(raw)
		if err != nil {
			return nil, err
		}
		return &dshArgumentErrorProjection{flat: flat}, nil
	case ArgumentErrorsDSHAlpha3Todo:
		if err := compileDSHTodoArgumentSchema(raw); err != nil {
			return nil, err
		}
		return &dshArgumentErrorProjection{todo: true}, nil
	default:
		return nil, fmt.Errorf("harness: unsupported argument error mode %d", mode)
	}
}

func compileDSHFlatArgumentSchema(raw json.RawMessage) (*dshFlatArgumentSchema, error) {
	if err := harnesstool.ValidateNativeJSONDecoding(raw); err != nil {
		return nil, fmt.Errorf("harness: DSH argument diagnostic schema: %w", err)
	}
	var schema map[string]json.RawMessage
	if err := json.Unmarshal(raw, &schema); err != nil {
		return nil, err
	}
	for key := range schema {
		switch key {
		case "type", "properties", "required", "additionalProperties":
		case "description", "title":
			var annotation any
			if json.Unmarshal(schema[key], &annotation) != nil || !dshArgumentType(annotation, "string") {
				return nil, fmt.Errorf("harness: DSH flat argument diagnostics require string %s", key)
			}
		default:
			return nil, fmt.Errorf("harness: DSH flat argument diagnostics do not support schema keyword %q", key)
		}
	}
	var kind string
	if json.Unmarshal(schema["type"], &kind) != nil || kind != "object" {
		return nil, fmt.Errorf("harness: DSH flat argument diagnostics require an object schema")
	}
	if value, ok := schema["additionalProperties"]; ok && !bytes.Equal(bytes.TrimSpace(value), []byte("true")) {
		return nil, fmt.Errorf("harness: DSH flat argument diagnostics require an open object schema")
	}
	result := &dshFlatArgumentSchema{}
	if required, ok := schema["required"]; ok {
		var names []any
		if err := json.Unmarshal(required, &names); err != nil || names == nil {
			return nil, fmt.Errorf("harness: DSH flat argument diagnostics require an array of required names")
		}
		for _, value := range names {
			name, ok := value.(string)
			if !ok {
				return nil, fmt.Errorf("harness: DSH flat argument diagnostics require string required names")
			}
			result.required = append(result.required, name)
		}
	}
	// Decode property tokens in source declaration order; a Go map would make
	// a multi-error diagnostic nondeterministic and differ from defineTool.
	decoder := json.NewDecoder(bytes.NewReader(schema["properties"]))
	if token, err := decoder.Token(); err != nil || token != json.Delim('{') {
		return nil, fmt.Errorf("harness: DSH flat argument diagnostics require properties")
	}
	declared := map[string]bool{}
	for decoder.More() {
		token, err := decoder.Token()
		if err != nil {
			return nil, err
		}
		name, ok := token.(string)
		if !ok || declared[name] {
			return nil, fmt.Errorf("harness: DSH flat argument diagnostics require unique property names")
		}
		declared[name] = true
		var property map[string]json.RawMessage
		if err := decoder.Decode(&property); err != nil {
			return nil, err
		}
		for key := range property {
			switch key {
			case "type", "enum":
			case "description", "title":
				var annotation any
				if json.Unmarshal(property[key], &annotation) != nil || !dshArgumentType(annotation, "string") {
					return nil, fmt.Errorf("harness: DSH flat argument diagnostics require string property %s", key)
				}
			default:
				return nil, fmt.Errorf("harness: DSH flat argument diagnostics do not support property keyword %q", key)
			}
		}
		entry := dshFlatArgumentProperty{name: name}
		if json.Unmarshal(property["type"], &entry.kind) != nil || (entry.kind != "string" && entry.kind != "number" && entry.kind != "boolean") {
			return nil, fmt.Errorf("harness: DSH flat argument diagnostics require scalar properties")
		}
		if enum, ok := property["enum"]; ok {
			if err := json.Unmarshal(enum, &entry.enum); err != nil || len(entry.enum) == 0 {
				return nil, fmt.Errorf("harness: DSH flat argument diagnostics require a nonempty scalar enum")
			}
			for _, value := range entry.enum {
				if !dshArgumentType(value, entry.kind) {
					return nil, fmt.Errorf("harness: DSH flat argument diagnostics require a type-matching enum")
				}
			}
		}
		result.properties = append(result.properties, entry)
	}
	// Object.entries visits canonical array-index keys before other keys.
	sort.SliceStable(result.properties, func(i, j int) bool {
		a, aok := dshArgumentIndex(result.properties[i].name)
		b, bok := dshArgumentIndex(result.properties[j].name)
		return aok && (!bok || a < b)
	})
	for _, name := range result.required {
		if !declared[name] {
			return nil, fmt.Errorf("harness: DSH flat argument diagnostics require declared required properties")
		}
	}
	return result, nil
}

func dshArgumentType(value any, kind string) bool {
	switch kind {
	case "string":
		_, ok := value.(string)
		return ok
	case "number":
		number, ok := value.(float64)
		return ok && !math.IsInf(number, 0) && !math.IsNaN(number) && !(number == 0 && math.Signbit(number))
	case "boolean":
		_, ok := value.(bool)
		return ok
	default:
		return false
	}
}

// projectArgumentFailure is called only after the authoritative validator has
// rejected. Unsupported native JSON identities retain the original diagnostic;
// they must not be relabeled as a source schema violation.
func projectArgumentFailure(tool Tool, raw json.RawMessage) (ToolResult, bool) {
	projection := tool.argumentErrorProjection
	if projection == nil || (tool.owner != nil && tool.owner.admissionError() != nil) {
		return ToolResult{}, false
	}
	if projection.todo {
		return projectDSHTodoArgumentFailure(raw)
	}
	schema := projection.flat
	if schema == nil {
		return ToolResult{}, false
	}
	if len(raw) == 0 {
		raw = json.RawMessage(`{}`)
	}
	if harnesstool.ValidateNativeJSONDecoding(raw) != nil {
		return ToolResult{}, false
	}
	var value any
	if json.Unmarshal(raw, &value) != nil {
		return ToolResult{}, false
	}
	// The authoritative native validator accepts negative zero, while source
	// schemas exclude it. Do not partially relabel a rejected mixed input or
	// change admission. Include negative underflow and open nested extras.
	pending := []any{value}
	for len(pending) > 0 {
		current := pending[len(pending)-1]
		pending = pending[:len(pending)-1]
		switch current := current.(type) {
		case float64:
			if current == 0 && math.Signbit(current) {
				return ToolResult{}, false
			}
		case []any:
			pending = append(pending, current...)
		case map[string]any:
			for _, child := range current {
				pending = append(pending, child)
			}
		}
	}
	object, ok := value.(map[string]any)
	violations := []string{}
	if !ok {
		violations = append(violations, `"arguments" must be an object`)
	} else {
		for _, name := range schema.required {
			if _, present := object[name]; !present {
				violations = append(violations, `missing required property "`+name+`"`)
			}
		}
		for _, property := range schema.properties {
			value, present := object[property.name]
			if !present {
				continue
			}
			path := property.name
			if path == "" {
				path = "arguments"
			}
			if !dshArgumentType(value, property.kind) {
				violations = append(violations, `"`+path+`" must be a `+property.kind)
				continue
			}
			if len(property.enum) > 0 {
				matches := false
				for _, allowed := range property.enum {
					matches = matches || reflect.DeepEqual(value, allowed)
				}
				if !matches {
					allowed := dshArgumentEnumJSON(property.enum)
					violations = append(violations, `"`+path+`" must be one of `+allowed)
				}
			}
		}
	}
	if len(violations) == 0 {
		return ToolResult{}, false
	}
	return toolPolicyErrorResult("invalid arguments: "+strings.Join(violations, "; "), "ToolArgsError", "INVALID_ARGS", nil), true
}

func bindCapturedArgumentErrors(proxy Tool, captured Tool) (Tool, error) {
	if captured.ArgumentErrorMode == ArgumentErrorsLegacy {
		return proxy, nil
	}
	if captured.owner != nil {
		if err := captured.owner.admissionError(); err != nil {
			return Tool{}, err
		}
	}
	var proxySchema, capturedSchema any
	if json.Unmarshal(proxy.Schema.InputSchema, &proxySchema) != nil || json.Unmarshal(captured.Schema.InputSchema, &capturedSchema) != nil || !reflect.DeepEqual(proxySchema, capturedSchema) {
		return Tool{}, fmt.Errorf("harness: argument diagnostic binding requires the exact captured input contract")
	}
	proxy.ArgumentErrorMode = captured.ArgumentErrorMode
	// Restore the captured declaration order after a JSON bridge transported
	// semantically equivalent schema fields in a different order.
	proxy.Schema.InputSchema = append(json.RawMessage(nil), captured.Schema.InputSchema...)
	proxy.argumentErrorProjection = captured.argumentErrorProjection
	return proxy, nil
}

func dshArgumentIndex(name string) (uint64, bool) {
	n, err := strconv.ParseUint(name, 10, 32)
	return n, err == nil && n < 4294967295 && strconv.FormatUint(n, 10) == name
}

func dshArgumentEnumJSON(values []any) string {
	var buffer bytes.Buffer
	encoder := json.NewEncoder(&buffer)
	encoder.SetEscapeHTML(false)
	_ = encoder.Encode(values)
	encoded := strings.TrimSuffix(buffer.String(), "\n")
	// JSON.stringify emits the two JavaScript line-separator runes literally.
	// Decode only an unescaped JSON escape, preserving literal backslash-u text.
	var out strings.Builder
	for i := 0; i < len(encoded); i++ {
		if encoded[i] == '\\' && i+1 < len(encoded) {
			if strings.HasPrefix(encoded[i:], `\u2028`) {
				out.WriteRune('\u2028')
				i += 5
				continue
			}
			if strings.HasPrefix(encoded[i:], `\u2029`) {
				out.WriteRune('\u2029')
				i += 5
				continue
			}
			out.WriteByte(encoded[i])
			i++
			out.WriteByte(encoded[i])
			continue
		}
		out.WriteByte(encoded[i])
	}
	return out.String()
}
