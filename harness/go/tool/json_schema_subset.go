// Package tool owns model-facing tool contracts, schema admission, registry,
// and execution policy.
package tool

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"
)

var supportedSchemaKeywords = map[string]struct{}{
	"type": {}, "oneOf": {}, "properties": {}, "required": {}, "additionalProperties": {},
	"items": {}, "enum": {}, "const": {}, "description": {}, "title": {}, "default": {}, "examples": {},
}

// ValidateObjectJSONSchema enforces the DSH object-rooted JSON Schema subset
// used by structured subagents. Unsupported keywords fail before a child is
// published instead of being accepted without enforcement.
func ValidateObjectJSONSchema(raw json.RawMessage) error {
	if len(raw) == 0 || !json.Valid(raw) {
		return errorsUnsupportedSchema("schema must be valid JSON")
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	var root any
	if err := decoder.Decode(&root); err != nil {
		return errorsUnsupportedSchema(err.Error())
	}
	type task struct {
		value any
		path  string
	}
	tasks := []task{{value: root, path: "schema"}}
	violations := make([]string, 0)
	for len(tasks) != 0 {
		current := tasks[len(tasks)-1]
		tasks = tasks[:len(tasks)-1]
		node, ok := current.value.(map[string]any)
		if !ok {
			violations = append(violations, current.path+" must be a schema object")
			continue
		}
		keys := make([]string, 0, len(node))
		for key := range node {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		for _, key := range keys {
			if _, ok := supportedSchemaKeywords[key]; !ok {
				violations = append(violations, fmt.Sprintf("%s.%s is not a supported keyword", current.path, key))
			}
		}
		for _, annotation := range []string{"description", "title"} {
			if value, exists := node[annotation]; exists {
				if _, ok := value.(string); !ok {
					violations = append(violations, fmt.Sprintf("%s.%s must be a string", current.path, annotation))
				}
			}
		}
		typeValue, hasType := node["type"]
		oneOfValue, hasOneOf := node["oneOf"]
		if hasType && hasOneOf {
			violations = append(violations, current.path+" cannot declare both type and oneOf")
			continue
		}
		if hasOneOf {
			for _, key := range []string{"properties", "required", "additionalProperties", "items", "enum", "const"} {
				if _, exists := node[key]; exists {
					violations = append(violations, fmt.Sprintf("%s.%s is not supported beside oneOf", current.path, key))
				}
			}
			branches, ok := oneOfValue.([]any)
			if !ok || len(branches) < 2 {
				violations = append(violations, current.path+".oneOf must be an array of at least two schemas")
			} else {
				for index := len(branches) - 1; index >= 0; index-- {
					tasks = append(tasks, task{value: branches[index], path: fmt.Sprintf("%s.oneOf[%d]", current.path, index)})
				}
			}
			continue
		}
		if !hasType {
			for _, key := range []string{"properties", "required", "additionalProperties", "items", "enum", "const"} {
				if _, exists := node[key]; exists {
					violations = append(violations, fmt.Sprintf("%s.%s requires type or oneOf", current.path, key))
				}
			}
			continue
		}
		schemaType, ok := typeValue.(string)
		if !ok || !supportedSchemaType(schemaType) {
			violations = append(violations, current.path+".type must be one of object/array/string/number/integer/boolean/null")
			continue
		}
		for key, allowed := range map[string]string{
			"properties": "object", "required": "object", "additionalProperties": "object", "items": "array",
		} {
			if _, exists := node[key]; exists && schemaType != allowed {
				violations = append(violations, fmt.Sprintf("%s.%s is not supported on type %q", current.path, key, schemaType))
			}
		}
		if schemaType == "object" {
			properties := map[string]any{}
			if value, exists := node["properties"]; exists {
				var propertiesOK bool
				properties, propertiesOK = value.(map[string]any)
				if !propertiesOK {
					violations = append(violations, current.path+".properties must be an object of schemas")
					properties = map[string]any{}
				} else {
					propertyNames := make([]string, 0, len(properties))
					for name := range properties {
						propertyNames = append(propertyNames, name)
					}
					sort.Sort(sort.Reverse(sort.StringSlice(propertyNames)))
					for _, name := range propertyNames {
						tasks = append(tasks, task{value: properties[name], path: current.path + ".properties." + name})
					}
				}
			}
			if value, exists := node["required"]; exists {
				required, ok := value.([]any)
				if !ok {
					violations = append(violations, current.path+".required must be an array of strings")
				} else {
					for _, item := range required {
						name, ok := item.(string)
						if !ok {
							violations = append(violations, current.path+".required must be an array of strings")
							continue
						}
						if _, declared := properties[name]; !declared {
							violations = append(violations, fmt.Sprintf("%s.required names %q which is not in properties", current.path, name))
						}
					}
				}
			}
			if value, exists := node["additionalProperties"]; exists {
				if _, ok := value.(bool); !ok {
					violations = append(violations, current.path+".additionalProperties must be a boolean")
				}
			}
		}
		if schemaType == "array" {
			if value, exists := node["items"]; exists {
				tasks = append(tasks, task{value: value, path: current.path + ".items"})
			}
		}
		if schemaType == "object" || schemaType == "array" {
			for _, key := range []string{"enum", "const"} {
				if _, exists := node[key]; exists {
					violations = append(violations, fmt.Sprintf("%s.%s is not supported on type %q", current.path, key, schemaType))
				}
			}
			continue
		}
		var enumValues []any
		enumValid := false
		if value, exists := node["enum"]; exists {
			values, ok := value.([]any)
			if !ok || len(values) == 0 {
				violations = append(violations, fmt.Sprintf("%s.enum must be a non-empty array of %s values", current.path, schemaType))
			} else {
				enumValues = values
				enumValid = true
				for _, candidate := range values {
					if !schemaScalarMatches(schemaType, candidate) {
						violations = append(violations, fmt.Sprintf("%s.enum must contain only %s values", current.path, schemaType))
						enumValid = false
						break
					}
				}
			}
		}
		if value, exists := node["const"]; exists {
			if !schemaScalarMatches(schemaType, value) {
				violations = append(violations, fmt.Sprintf("%s.const must be a %s value", current.path, schemaType))
			} else if enumValid && !schemaScalarIncluded(enumValues, value) {
				violations = append(violations, fmt.Sprintf("%s.const must be one of %s.enum when both are declared", current.path, current.path))
			}
		}
	}
	rootObject, ok := root.(map[string]any)
	if ok && len(violations) == 0 && rootObject["type"] != "object" {
		violations = append(violations, `schema.type must be "object" (structured output is object-rooted)`)
	}
	if len(violations) != 0 {
		return errorsUnsupportedSchema(violations...)
	}
	_, err := CompileInputSchema("structured_output", raw)
	return err
}

func schemaScalarIncluded(values []any, target any) bool {
	for _, value := range values {
		left, leftNumber := value.(json.Number)
		right, rightNumber := target.(json.Number)
		if leftNumber && rightNumber {
			leftValue, leftErr := strconv.ParseFloat(string(left), 64)
			rightValue, rightErr := strconv.ParseFloat(string(right), 64)
			if leftErr == nil && rightErr == nil && leftValue == rightValue {
				return true
			}
			continue
		}
		if value == target {
			return true
		}
	}
	return false
}

func supportedSchemaType(value string) bool {
	switch value {
	case "object", "array", "string", "number", "integer", "boolean", "null":
		return true
	default:
		return false
	}
}

func schemaScalarMatches(schemaType string, value any) bool {
	switch schemaType {
	case "string":
		_, ok := value.(string)
		return ok
	case "boolean":
		_, ok := value.(bool)
		return ok
	case "null":
		return value == nil
	case "number", "integer":
		number, ok := value.(json.Number)
		if !ok {
			return false
		}
		parsed, err := strconv.ParseFloat(string(number), 64)
		if err != nil || math.IsInf(parsed, 0) || math.IsNaN(parsed) || parsed == 0 && len(number) > 0 && number[0] == '-' {
			return false
		}
		return schemaType == "number" || math.Trunc(parsed) == parsed
	default:
		return false
	}
}

func errorsUnsupportedSchema(violations ...string) error {
	return fmt.Errorf("unsupported JSON schema: %s", strings.Join(violations, "; "))
}
