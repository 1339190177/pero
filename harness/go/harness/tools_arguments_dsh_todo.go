package harness

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"sort"
	"strconv"
	"strings"

	harnesstool "github.com/phodal/pero/harness/go/tool"
)

// These limits bound additional, opt-in diagnostics and compatibility retries.
// They do not cap the authoritative validator or previously accepted inputs.
const (
	dshTodoSchemaBytes      = 64 << 10
	dshTodoSchemaTokens     = 4096
	dshTodoArgumentBytes    = 1 << 20
	dshTodoArgumentTokens   = 65536
	dshTodoArgumentDepth    = 64
	dshTodoViolationLimit   = 4096
	dshTodoMessageBytes     = 256 << 10
	dshTodoInvalidPrefix    = "invalid arguments: "
	dshTodoStatusDiagnostic = ` must be one of ["pending","in_progress","completed"]`
)

func checkDSHTodoSchemaBytes(raw json.RawMessage) error {
	if len(raw) > dshTodoSchemaBytes {
		return fmt.Errorf("harness: DSH todo argument diagnostic schema exceeds %d bytes", dshTodoSchemaBytes)
	}
	return nil
}

// dshTodoJSONBudget uses a token stream, not recursive callbacks or an object
// graph. Count every raw subtree, including overwritten members and open extras.
func dshTodoJSONBudget(raw json.RawMessage, maxTokens, maxDepth int) bool {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	tokens, depth := 0, 0
	for {
		token, err := decoder.Token()
		if err == io.EOF {
			return tokens > 0 && depth == 0
		}
		if err != nil {
			return false
		}
		tokens++
		if tokens > maxTokens {
			return false
		}
		if delim, ok := token.(json.Delim); ok {
			switch delim {
			case '{', '[':
				depth++
				if depth > maxDepth {
					return false
				}
			case '}', ']':
				depth--
				if depth < 0 {
					return false
				}
			}
		}
	}
}

type dshTodoJSONObject struct {
	keys      []string
	values    map[string]json.RawMessage
	duplicate bool
}

// JSON.parse uses the last value but keeps the key's first insertion position.
// Schema callers reject duplicates; argument callers deliberately retain them.
func dshTodoObject(raw json.RawMessage) (dshTodoJSONObject, bool) {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	if token, err := decoder.Token(); err != nil || token != json.Delim('{') {
		return dshTodoJSONObject{}, false
	}
	object := dshTodoJSONObject{values: make(map[string]json.RawMessage)}
	for decoder.More() {
		token, err := decoder.Token()
		if err != nil {
			return dshTodoJSONObject{}, false
		}
		key, ok := token.(string)
		if !ok {
			return dshTodoJSONObject{}, false
		}
		var value json.RawMessage
		if decoder.Decode(&value) != nil {
			return dshTodoJSONObject{}, false
		}
		if _, exists := object.values[key]; exists {
			object.duplicate = true
		} else {
			object.keys = append(object.keys, key)
		}
		object.values[key] = value
	}
	if token, err := decoder.Token(); err != nil || token != json.Delim('}') {
		return dshTodoJSONObject{}, false
	}
	if _, err := decoder.Token(); err != io.EOF {
		return dshTodoJSONObject{}, false
	}
	return object, true
}

func dshTodoString(raw json.RawMessage, want string) bool {
	var value any
	if json.Unmarshal(raw, &value) != nil {
		return false
	}
	text, ok := value.(string)
	return ok && text == want
}

func dshTodoStrings(raw json.RawMessage, want ...string) bool {
	var values []any
	if json.Unmarshal(raw, &values) != nil || values == nil || len(values) != len(want) {
		return false
	}
	for i, value := range values {
		if text, ok := value.(string); !ok || text != want[i] {
			return false
		}
	}
	return true
}

func dshTodoSchemaObject(raw json.RawMessage, allowed ...string) (dshTodoJSONObject, bool) {
	object, ok := dshTodoObject(raw)
	if !ok || object.duplicate {
		return dshTodoJSONObject{}, false
	}
	for _, key := range object.keys {
		if key == "title" || key == "description" {
			var value any
			if json.Unmarshal(object.values[key], &value) != nil {
				return dshTodoJSONObject{}, false
			}
			if _, ok := value.(string); !ok {
				return dshTodoJSONObject{}, false
			}
			continue
		}
		found := false
		for _, name := range allowed {
			found = found || key == name
		}
		if !found {
			return dshTodoJSONObject{}, false
		}
	}
	return object, true
}

func compileDSHTodoArgumentSchema(raw json.RawMessage) error {
	return compileDSHTodoSchema(raw, false)
}

// Semantic Todo registrations also support the pre-existing closed-root option;
// diagnostic registration retains its exact open-root grammar.
func compileDSHTodoSchema(raw json.RawMessage, allowClosedRoot bool) error {
	if err := checkDSHTodoSchemaBytes(raw); err != nil {
		return err
	}
	unsupported := errors.New("harness: DSH todo argument diagnostics require the exact Todo input schema")
	if !dshTodoJSONBudget(raw, dshTodoSchemaTokens, dshTodoArgumentDepth) || harnesstool.ValidateNativeJSONDecoding(raw) != nil {
		return unsupported
	}
	root, ok := dshTodoSchemaObject(raw, "type", "properties", "required", "additionalProperties")
	if !ok || !dshTodoString(root.values["type"], "object") || !dshTodoStrings(root.values["required"], "todos") {
		return unsupported
	}
	if open, exists := root.values["additionalProperties"]; exists && !bytes.Equal(bytes.TrimSpace(open), []byte("true")) && !(allowClosedRoot && bytes.Equal(bytes.TrimSpace(open), []byte("false"))) {
		return unsupported
	}
	properties, ok := dshTodoObject(root.values["properties"])
	if !ok || properties.duplicate || len(properties.keys) != 1 || properties.keys[0] != "todos" {
		return unsupported
	}
	array, ok := dshTodoSchemaObject(properties.values["todos"], "type", "items")
	if !ok || !dshTodoString(array.values["type"], "array") {
		return unsupported
	}
	item, ok := dshTodoSchemaObject(array.values["items"], "type", "properties", "required", "additionalProperties")
	if !ok || !dshTodoString(item.values["type"], "object") || !dshTodoStrings(item.values["required"], "content", "status") || !bytes.Equal(bytes.TrimSpace(item.values["additionalProperties"]), []byte("false")) {
		return unsupported
	}
	fields, ok := dshTodoObject(item.values["properties"])
	if !ok || fields.duplicate || len(fields.keys) != 2 || fields.keys[0] != "content" || fields.keys[1] != "status" {
		return unsupported
	}
	content, ok := dshTodoSchemaObject(fields.values["content"], "type")
	if !ok || !dshTodoString(content.values["type"], "string") {
		return unsupported
	}
	status, ok := dshTodoSchemaObject(fields.values["status"], "type", "enum")
	if !ok || !dshTodoString(status.values["type"], "string") || !dshTodoStrings(status.values["enum"], "pending", "in_progress", "completed") {
		return unsupported
	}
	return nil
}

type dshTodoViolations struct {
	messages []string
	bytes    int
}

func (v *dshTodoViolations) add(parts ...string) bool {
	length := 0
	for _, part := range parts {
		length += len(part)
	}
	if len(v.messages) > 0 {
		length += len("; ")
	}
	if len(v.messages) >= dshTodoViolationLimit || length > dshTodoMessageBytes-len(dshTodoInvalidPrefix)-v.bytes {
		return false
	}
	v.bytes += length
	v.messages = append(v.messages, strings.Join(parts, ""))
	return true
}

// This function is reached only after the unchanged validator has rejected.
// Nothing here can admit a call, run policy, execute a body or alter durable data.
func projectDSHTodoArgumentFailure(raw json.RawMessage) (ToolResult, bool) {
	if len(raw) == 0 {
		raw = json.RawMessage(`{}`)
	}
	if len(raw) > dshTodoArgumentBytes || !dshTodoJSONBudget(raw, dshTodoArgumentTokens, dshTodoArgumentDepth) || harnesstool.ValidateNativeJSONDecoding(raw) != nil {
		return ToolResult{}, false
	}
	var value any
	if json.Unmarshal(raw, &value) != nil {
		return ToolResult{}, false
	}
	return projectDSHTodoArgumentValueFailure(raw, value)
}

// Both callers establish bounded, Unicode-safe raw input and a successfully
// decoded float64 snapshot. Preserve raw member identity/order for diagnostics.
func projectDSHTodoArgumentValueFailure(raw json.RawMessage, value any) (ToolResult, bool) {
	// Match the flat mode's conservative whole-value representation boundary.
	// Final duplicate values matter; an earlier overwritten -0 is not retained.
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
	violations := dshTodoViolations{}
	object, isObject := value.(map[string]any)
	if !isObject {
		if !violations.add(`"arguments" must be an object`) {
			return ToolResult{}, false
		}
	} else {
		root, ok := dshTodoObject(raw)
		if !ok || !dshTodoSameKeys(root, object) {
			return ToolResult{}, false
		}
		listRaw, present := root.values["todos"]
		if !present {
			if !violations.add(`missing required property "todos"`) {
				return ToolResult{}, false
			}
		} else if list, ok := object["todos"].([]any); !ok {
			if !violations.add(`"todos" must be an array`) {
				return ToolResult{}, false
			}
		} else {
			var items []json.RawMessage
			if json.Unmarshal(listRaw, &items) != nil || items == nil || len(items) != len(list) {
				return ToolResult{}, false
			}
			for index, rawItem := range items {
				if !dshTodoItemViolations(&violations, rawItem, list[index], index) {
					return ToolResult{}, false
				}
			}
		}
	}
	if len(violations.messages) == 0 {
		return ToolResult{}, false
	}
	return toolPolicyErrorResult(dshTodoInvalidPrefix+strings.Join(violations.messages, "; "), "ToolArgsError", "INVALID_ARGS", nil), true
}

func dshTodoSameKeys(raw dshTodoJSONObject, value map[string]any) bool {
	if len(raw.keys) != len(value) {
		return false
	}
	for _, key := range raw.keys {
		if _, exists := value[key]; !exists {
			return false
		}
	}
	return true
}

func dshTodoItemViolations(violations *dshTodoViolations, raw json.RawMessage, snapshot any, index int) bool {
	path := "todos[" + strconv.Itoa(index) + "]"
	values, isObject := snapshot.(map[string]any)
	if !isObject {
		return violations.add(`"` + path + `" must be an object`)
	}
	item, ok := dshTodoObject(raw)
	if !ok || !dshTodoSameKeys(item, values) {
		return false
	}
	for _, key := range []string{"content", "status"} {
		if _, present := values[key]; !present {
			if !violations.add(`missing required property "` + path + "." + key + `"`) {
				return false
			}
		}
	}
	for _, key := range []string{"content", "status"} {
		value, present := values[key]
		if !present {
			continue
		}
		text, isString := value.(string)
		field := `"` + path + "." + key + `"`
		if !isString {
			if !violations.add(field + " must be a string") {
				return false
			}
		} else if key == "status" && text != "pending" && text != "in_progress" && text != "completed" {
			if !violations.add(field + dshTodoStatusDiagnostic) {
				return false
			}
		}
	}
	// Object.keys: canonical array-index keys precede ordinary first-seen keys.
	sort.SliceStable(item.keys, func(i, j int) bool {
		a, aok := dshArgumentIndex(item.keys[i])
		b, bok := dshArgumentIndex(item.keys[j])
		return aok && (!bok || a < b)
	})
	for _, key := range item.keys {
		if key != "content" && key != "status" {
			if !violations.add(`"`, path, ".", key, `" is not a declared property (additionalProperties: false)`) {
				return false
			}
		}
	}
	return true
}
