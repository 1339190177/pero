package config

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"reflect"
)

type settingsPreparedPathOp struct {
	op    string
	path  []string
	value any
}

func applySettingsPathOp(section map[string]any, op settingsPreparedPathOp) (map[string]any, bool, error) {
	if len(op.path) == 0 {
		if op.op == "unset" {
			return map[string]any{}, len(section) != 0, nil
		}
		root, ok := op.value.(map[string]any)
		if !ok {
			return nil, false, errors.New("setting the section root requires an object")
		}
		return cloneSettingsMap(root), !reflect.DeepEqual(section, root), nil
	}
	result := cloneSettingsMap(section)
	head := op.path[0]
	if len(op.path) == 1 {
		if op.op == "unset" {
			if _, found := result[head]; !found {
				return result, false, nil
			}
			delete(result, head)
		} else {
			if current, found := result[head]; found && reflect.DeepEqual(current, op.value) {
				return result, false, nil
			}
			result[head] = cloneSettingsValue(op.value)
		}
		return result, true, nil
	}
	child, ok := result[head].(map[string]any)
	if !ok {
		if op.op == "unset" {
			return result, false, nil
		}
		child = map[string]any{}
	}
	next, changed, err := applySettingsPathOp(child, settingsPreparedPathOp{op: op.op, path: op.path[1:], value: op.value})
	if err != nil {
		return nil, false, err
	}
	if !changed {
		return result, false, nil
	}
	result[head] = next
	return result, true, nil
}

func decodeSettingsJSONValue(raw json.RawMessage) (any, error) {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	var value any
	if err := decoder.Decode(&value); err != nil {
		return nil, err
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		if err != nil {
			return nil, err
		}
		return nil, errors.New("JSON value has trailing data")
	}
	return value, nil
}

func cloneSettingsMap(source map[string]any) map[string]any {
	result := make(map[string]any, len(source))
	for key, value := range source {
		result[key] = cloneSettingsValue(value)
	}
	return result
}

func cloneSettingsValue(value any) any {
	switch typed := value.(type) {
	case map[string]any:
		return cloneSettingsMap(typed)
	case []any:
		result := make([]any, len(typed))
		for index, item := range typed {
			result[index] = cloneSettingsValue(item)
		}
		return result
	default:
		return typed
	}
}

func normalizeSettingsObject(value any, label string) (map[string]any, error) {
	if value == nil {
		return map[string]any{}, nil
	}
	raw, err := normalizeSettingsObjectRaw(value, label)
	if err != nil {
		return nil, err
	}
	return decodeSettingsObject(raw)
}

func normalizeSettingsObjectRaw(value any, label string) (json.RawMessage, error) {
	raw, err := json.Marshal(value)
	if err != nil {
		return nil, NewProviderError("settings "+label+" must be JSON-compatible", InvalidSettingsCode, err)
	}
	object, err := decodeSettingsObject(raw)
	if err != nil {
		return nil, NewProviderError("settings "+label+" must be a JSON object", InvalidSettingsCode, err)
	}
	return encodeSettingsObject(object), nil
}

func decodeSettingsObject(raw json.RawMessage) (map[string]any, error) {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	var object map[string]any
	if err := decoder.Decode(&object); err != nil {
		return nil, err
	}
	if object == nil {
		return nil, errors.New("settings section must be an object")
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		if err != nil {
			return nil, err
		}
		return nil, errors.New("settings section has trailing data")
	}
	return object, nil
}

func encodeSettingsObject(object map[string]any) json.RawMessage {
	raw, _ := json.Marshal(object)
	return raw
}

func deepMergeSettings(under, over map[string]any) map[string]any {
	result := make(map[string]any, len(under)+len(over))
	for key, value := range under {
		result[key] = value
	}
	for key, value := range over {
		if lower, lowerOK := result[key].(map[string]any); lowerOK {
			if upper, upperOK := value.(map[string]any); upperOK {
				result[key] = deepMergeSettings(lower, upper)
				continue
			}
		}
		result[key] = value
	}
	return result
}

func sameRawSettingsSection(left json.RawMessage, leftFound bool, right json.RawMessage, rightFound bool) bool {
	if leftFound != rightFound {
		return false
	}
	if !leftFound {
		return true
	}
	var leftValue, rightValue any
	if json.Unmarshal(left, &leftValue) != nil || json.Unmarshal(right, &rightValue) != nil {
		return bytes.Equal(left, right)
	}
	leftCanonical, _ := json.Marshal(leftValue)
	rightCanonical, _ := json.Marshal(rightValue)
	return bytes.Equal(leftCanonical, rightCanonical)
}
