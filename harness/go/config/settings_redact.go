package config

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sort"
	"strconv"
)

type settingsSchemaResolver struct {
	refs map[string]json.RawMessage
}

type settingsSchemaNode struct {
	Type                 string          `json:"type"`
	Meta                 json.RawMessage `json:"meta"`
	Role                 string          `json:"role"`
	XRole                string          `json:"x-role"`
	Dict                 json.RawMessage `json:"dict"`
	Properties           json.RawMessage `json:"properties"`
	Inner                json.RawMessage `json:"inner"`
	Items                json.RawMessage `json:"items"`
	AdditionalProperties json.RawMessage `json:"additionalProperties"`
}

type settingsSchemaPair struct {
	key   string
	value json.RawMessage
}

func redactSettingsDescriptor(descriptor SettingsDescriptor) (SettingsDescriptor, error) {
	if len(bytes.TrimSpace(descriptor.Schema)) == 0 {
		descriptor.Secrets = []SettingsSecretView{}
		return descriptor, nil
	}
	resolver, root, err := newSettingsSchemaResolver(descriptor.Schema)
	if err != nil {
		return SettingsDescriptor{}, NewProviderError("settings schema cannot be used for secret redaction", InvalidSettingsCode, err)
	}
	value, err := redactSettingsDocumentValue(resolver, root, descriptor.Value, true)
	if err != nil {
		return SettingsDescriptor{}, err
	}
	descriptor.Value = value.value
	descriptor.Secrets = value.secrets
	if len(descriptor.Base) != 0 {
		redacted, redactErr := redactSettingsDocumentValue(resolver, root, descriptor.Base, false)
		if redactErr != nil {
			return SettingsDescriptor{}, redactErr
		}
		descriptor.Base = redacted.value
	}
	if len(descriptor.User) != 0 {
		redacted, redactErr := redactSettingsDocumentValue(resolver, root, descriptor.User, false)
		if redactErr != nil {
			return SettingsDescriptor{}, redactErr
		}
		descriptor.User = redacted.value
	}
	return descriptor, nil
}

type redactedSettingsValue struct {
	value   json.RawMessage
	secrets []SettingsSecretView
}

func redactSettingsDocumentValue(
	resolver settingsSchemaResolver,
	root json.RawMessage,
	raw json.RawMessage,
	collectSecrets bool,
) (redactedSettingsValue, error) {
	value, err := decodeSettingsJSONValue(raw)
	if err != nil {
		return redactedSettingsValue{}, err
	}
	secrets := make([]SettingsSecretView, 0)
	redacted, omitted, err := walkSettingsSecrets(resolver, root, value, true, nil, &secrets)
	if err != nil {
		return redactedSettingsValue{}, NewProviderError("settings schema cannot be used for secret redaction", InvalidSettingsCode, err)
	}
	if omitted {
		redacted = nil
	}
	encoded, err := json.Marshal(redacted)
	if err != nil {
		return redactedSettingsValue{}, err
	}
	if !collectSecrets {
		secrets = nil
	}
	return redactedSettingsValue{value: encoded, secrets: secrets}, nil
}

func newSettingsSchemaResolver(schema json.RawMessage) (settingsSchemaResolver, json.RawMessage, error) {
	var envelope map[string]json.RawMessage
	if err := json.Unmarshal(schema, &envelope); err != nil {
		return settingsSchemaResolver{}, nil, err
	}
	uid, hasUID := envelope["uid"]
	refsRaw, hasRefs := envelope["refs"]
	if hasUID != hasRefs {
		return settingsSchemaResolver{}, nil, errors.New("settings schema has an incomplete uid/refs envelope")
	}
	if !hasUID {
		return settingsSchemaResolver{}, append(json.RawMessage(nil), schema...), nil
	}
	if len(bytes.TrimSpace(uid)) == 0 || bytes.Equal(bytes.TrimSpace(uid), []byte("null")) {
		return settingsSchemaResolver{}, nil, errors.New("settings schema uid is missing")
	}
	var refs map[string]json.RawMessage
	if err := json.Unmarshal(refsRaw, &refs); err != nil {
		return settingsSchemaResolver{}, nil, err
	}
	if refs == nil {
		return settingsSchemaResolver{}, nil, errors.New("settings schema refs must be an object")
	}
	resolver := settingsSchemaResolver{refs: refs}
	root, err := resolver.resolveRelation(uid)
	return resolver, root, err
}

func (r settingsSchemaResolver) resolveRelation(relation json.RawMessage) (json.RawMessage, error) {
	trimmed := bytes.TrimSpace(relation)
	if len(trimmed) == 0 {
		return nil, nil
	}
	if trimmed[0] == '{' {
		return append(json.RawMessage(nil), trimmed...), nil
	}
	var key string
	if trimmed[0] == '"' {
		if err := json.Unmarshal(trimmed, &key); err != nil {
			return nil, err
		}
	} else {
		var number json.Number
		decoder := json.NewDecoder(bytes.NewReader(trimmed))
		decoder.UseNumber()
		if err := decoder.Decode(&number); err != nil {
			return nil, err
		}
		key = number.String()
	}
	node, found := r.refs[key]
	if !found {
		return nil, fmt.Errorf("settings schema references missing node %q", key)
	}
	return append(json.RawMessage(nil), node...), nil
}

func walkSettingsSecrets(
	resolver settingsSchemaResolver,
	relation json.RawMessage,
	value any,
	present bool,
	path []string,
	secrets *[]SettingsSecretView,
) (any, bool, error) {
	rawNode, err := resolver.resolveRelation(relation)
	if err != nil {
		return nil, false, err
	}
	if len(rawNode) == 0 {
		return cloneSettingsValue(value), false, nil
	}
	var node settingsSchemaNode
	if err := json.Unmarshal(rawNode, &node); err != nil {
		return nil, false, err
	}
	secret, err := settingsSchemaNodeIsSecret(node.Meta, node.Role, node.XRole)
	if err != nil {
		return nil, false, err
	}
	if secret {
		*secrets = append(*secrets, SettingsSecretView{Path: append([]string(nil), path...), Set: present})
		return nil, true, nil
	}
	switch node.Type {
	case "object":
		properties := node.Properties
		if len(bytes.TrimSpace(properties)) == 0 {
			properties = node.Dict
		}
		pairs, err := orderedSettingsSchemaObject(properties)
		if err != nil {
			return nil, false, err
		}
		source, sourceIsMap := value.(map[string]any)
		result := map[string]any{}
		if sourceIsMap {
			for key, entry := range source {
				result[key] = cloneSettingsValue(entry)
			}
		}
		for _, pair := range pairs {
			entry, found := source[pair.key]
			redacted, omit, err := walkSettingsSecrets(
				resolver, pair.value, entry, sourceIsMap && found, appendSettingsSecretPath(path, pair.key), secrets,
			)
			if err != nil {
				return nil, false, err
			}
			if omit || !sourceIsMap || !found {
				delete(result, pair.key)
				continue
			}
			result[pair.key] = redacted
		}
		if !sourceIsMap && len(result) == 0 {
			return cloneSettingsValue(value), false, nil
		}
		return result, false, nil
	case "dict":
		source, ok := value.(map[string]any)
		if !ok {
			return cloneSettingsValue(value), false, nil
		}
		inner := node.Inner
		if len(bytes.TrimSpace(inner)) == 0 {
			inner = node.AdditionalProperties
		}
		keys := make([]string, 0, len(source))
		for key := range source {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		result := make(map[string]any, len(source))
		for _, key := range keys {
			redacted, omit, err := walkSettingsSecrets(resolver, inner, source[key], true, appendSettingsSecretPath(path, key), secrets)
			if err != nil {
				return nil, false, err
			}
			if !omit {
				result[key] = redacted
			}
		}
		return result, false, nil
	case "array":
		source, ok := value.([]any)
		if !ok {
			return cloneSettingsValue(value), false, nil
		}
		inner := node.Inner
		if len(bytes.TrimSpace(inner)) == 0 {
			inner = node.Items
		}
		result := make([]any, len(source))
		for index, entry := range source {
			redacted, omit, err := walkSettingsSecrets(
				resolver, inner, entry, true, appendSettingsSecretPath(path, strconv.Itoa(index)), secrets,
			)
			if err != nil {
				return nil, false, err
			}
			if !omit {
				result[index] = redacted
			}
		}
		return result, false, nil
	default:
		return cloneSettingsValue(value), false, nil
	}
}

func appendSettingsSecretPath(path []string, segment string) []string {
	next := make([]string, len(path)+1)
	copy(next, path)
	next[len(path)] = segment
	return next
}

func settingsSchemaNodeIsSecret(meta json.RawMessage, roles ...string) (bool, error) {
	for _, role := range roles {
		if role == "secret" {
			return true, nil
		}
	}
	if len(bytes.TrimSpace(meta)) == 0 {
		return false, nil
	}
	var value struct {
		Role string `json:"role"`
	}
	if err := json.Unmarshal(meta, &value); err != nil {
		return false, err
	}
	return value.Role == "secret", nil
}

func orderedSettingsSchemaObject(raw json.RawMessage) ([]settingsSchemaPair, error) {
	if len(bytes.TrimSpace(raw)) == 0 || bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		return nil, nil
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	token, err := decoder.Token()
	if err != nil {
		return nil, err
	}
	delimiter, ok := token.(json.Delim)
	if !ok || delimiter != '{' {
		return nil, errors.New("settings schema relation dictionary must be an object")
	}
	pairs := make([]settingsSchemaPair, 0)
	for decoder.More() {
		keyToken, err := decoder.Token()
		if err != nil {
			return nil, err
		}
		key, ok := keyToken.(string)
		if !ok {
			return nil, errors.New("settings schema dictionary key must be a string")
		}
		var value json.RawMessage
		if err := decoder.Decode(&value); err != nil {
			return nil, err
		}
		pairs = append(pairs, settingsSchemaPair{key: key, value: value})
	}
	if _, err := decoder.Token(); err != nil {
		return nil, err
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		if err != nil {
			return nil, err
		}
		return nil, errors.New("settings schema dictionary has trailing data")
	}
	return pairs, nil
}
