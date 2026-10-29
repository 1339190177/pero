package config

import (
	"bytes"
	"encoding/json"
	"errors"
	"gopkg.in/yaml.v3"
	"io"
	"sort"
)

func parseCredentialYAMLRoot(source []byte) (*yaml.Node, error) {
	root := &yaml.Node{}
	decoder := yaml.NewDecoder(bytes.NewReader(source))
	if err := decoder.Decode(root); err != nil && !errors.Is(err, io.EOF) {
		return nil, err
	}
	if root.Kind == 0 || (root.Kind == yaml.DocumentNode && len(root.Content) == 0) {
		root.Kind = yaml.DocumentNode
		root.Content = []*yaml.Node{{Kind: yaml.MappingNode, Tag: "!!map"}}
	}
	if root.Kind != yaml.DocumentNode || len(root.Content) != 1 || root.Content[0].Kind != yaml.MappingNode {
		return nil, errors.New("document root must be a mapping")
	}
	var trailing yaml.Node
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		if err != nil {
			return nil, err
		}
		if len(trailing.Content) != 0 {
			return nil, errors.New("document must contain exactly one YAML document")
		}
	}
	return root, nil
}

// RenderFlatCredentialMigration recognizes the exact pre-versioned document
// shape accepted by early DSH builds and returns its lossless v1 envelope.
// Unrecognized input is never rewritten.
func RenderFlatCredentialMigration(source []byte) ([]byte, bool) {
	root, err := parseCredentialYAMLRoot(source)
	if err != nil {
		return nil, false
	}
	mapping := root.Content[0]
	if len(mapping.Content) == 0 {
		return nil, false
	}
	for _, line := range bytes.Split(source, []byte("\n")) {
		if bytes.HasPrefix(line, []byte("%")) || bytes.HasPrefix(line, []byte("---")) || bytes.HasPrefix(line, []byte("...")) {
			return nil, false
		}
	}
	seen := make(map[string]struct{}, len(mapping.Content)/2)
	for index := 0; index < len(mapping.Content); index += 2 {
		name, value := mapping.Content[index], mapping.Content[index+1]
		if name.Kind != yaml.ScalarNode || name.Tag != "!!str" || name.Value == "version" {
			return nil, false
		}
		if _, duplicate := seen[name.Value]; duplicate {
			return nil, false
		}
		seen[name.Value] = struct{}{}
		if _, err := ParseCredentialRef(name.Value); err != nil {
			return nil, false
		}
		if value.Kind != yaml.ScalarNode || value.Tag != "!!str" || value.Value == "" {
			return nil, false
		}
	}
	lines := bytes.Split(source, []byte("\n"))
	var body bytes.Buffer
	for index, line := range lines {
		if index > 0 {
			body.WriteByte('\n')
		}
		if len(line) > 0 {
			body.WriteString("  ")
		}
		body.Write(line)
	}
	var migrated bytes.Buffer
	migrated.WriteString("version: 1\nrefs:\n")
	migrated.Write(body.Bytes())
	if !bytes.HasSuffix(source, []byte("\n")) {
		migrated.WriteByte('\n')
	}
	return migrated.Bytes(), true
}

func decodeCredentialRefs(node *yaml.Node, target map[CredentialRef]string) error {
	if node.Kind != yaml.MappingNode {
		return NewProviderError("credential refs must be a mapping", InvalidCredentialCode, nil)
	}
	for index := 0; index < len(node.Content); index += 2 {
		nameNode, valueNode := node.Content[index], node.Content[index+1]
		if nameNode.Kind != yaml.ScalarNode || valueNode.Kind != yaml.ScalarNode || valueNode.Tag != "!!str" {
			return NewProviderError("credential refs must map names to strings", InvalidCredentialCode, nil)
		}
		ref, err := ParseCredentialRef(nameNode.Value)
		if err != nil {
			return NewProviderError("credential file contains an invalid ref name", InvalidCredentialCode, nil)
		}
		if _, duplicate := target[ref]; duplicate {
			return NewProviderError("credential file contains a duplicate ref", InvalidCredentialCode, nil)
		}
		if valueNode.Value == "" {
			return NewProviderError("credential file contains an empty ref value", InvalidCredentialCode, nil)
		}
		target[ref] = valueNode.Value
	}
	return nil
}

func decodeCredentialRecords(node *yaml.Node, target map[CredentialKey]CredentialRecord) ([]CredentialKey, error) {
	if node.Kind != yaml.MappingNode {
		return nil, NewProviderError("credential records must be a mapping", InvalidCredentialCode, nil)
	}
	order := make([]CredentialKey, 0, len(node.Content)/2)
	for index := 0; index < len(node.Content); index += 2 {
		keyNode, recordNode := node.Content[index], node.Content[index+1]
		if keyNode.Kind != yaml.ScalarNode {
			return nil, NewProviderError("credential record keys must be strings", InvalidCredentialCode, nil)
		}
		key, err := ParseCredentialKey(keyNode.Value)
		if err != nil {
			return nil, NewProviderError("credential file contains an invalid record key", InvalidCredentialCode, nil)
		}
		if _, duplicate := target[key]; duplicate {
			return nil, NewProviderError("credential file contains a duplicate record key", InvalidCredentialCode, nil)
		}
		record, err := decodeCredentialRecordNode(key, recordNode)
		if err != nil {
			return nil, err
		}
		target[key] = record
		order = append(order, key)
	}
	return order, nil
}

func decodeCredentialRecordNode(key CredentialKey, node *yaml.Node) (CredentialRecord, error) {
	if node.Kind != yaml.MappingNode {
		return CredentialRecord{}, NewProviderError("credential record must be a mapping", InvalidCredentialCode, nil)
	}
	var record CredentialRecord
	seen := make(map[string]struct{}, len(node.Content)/2)
	for index := 0; index < len(node.Content); index += 2 {
		name, value := node.Content[index].Value, node.Content[index+1]
		if _, duplicate := seen[name]; duplicate {
			return CredentialRecord{}, NewProviderError("credential record contains a duplicate field", InvalidCredentialCode, nil)
		}
		seen[name] = struct{}{}
		switch name {
		case "kind":
			if value.Kind != yaml.ScalarNode || value.Tag != "!!str" {
				return CredentialRecord{}, NewProviderError("credential record kind must be a string", InvalidCredentialCode, nil)
			}
			record.Kind = CredentialRecordKind(value.Value)
		case "key":
			if value.Kind != yaml.ScalarNode || value.Tag != "!!str" {
				return CredentialRecord{}, NewProviderError("credential record key value must be a string", InvalidCredentialCode, nil)
			}
			secret := value.Value
			record.Key = &secret
		case "env":
			if value.Kind != yaml.MappingNode {
				return CredentialRecord{}, NewProviderError("credential record env must be a mapping", InvalidCredentialCode, nil)
			}
			record.Env = make(map[string]string, len(value.Content)/2)
			for envIndex := 0; envIndex < len(value.Content); envIndex += 2 {
				envName, envValue := value.Content[envIndex], value.Content[envIndex+1]
				if envName.Kind != yaml.ScalarNode || envValue.Kind != yaml.ScalarNode || envValue.Tag != "!!str" {
					return CredentialRecord{}, NewProviderError("credential record env values must be strings", InvalidCredentialCode, nil)
				}
				if _, duplicate := record.Env[envName.Value]; duplicate {
					return CredentialRecord{}, NewProviderError("credential record contains a duplicate env name", InvalidCredentialCode, nil)
				}
				record.Env[envName.Value] = envValue.Value
			}
		case "payload":
			var decoded any
			if err := value.Decode(&decoded); err != nil {
				return CredentialRecord{}, NewProviderError("credential grant payload is invalid", InvalidCredentialCode, nil)
			}
			normalized, err := normalizeYAMLValue(decoded)
			if err != nil {
				return CredentialRecord{}, NewProviderError("credential grant payload is invalid", InvalidCredentialCode, nil)
			}
			record.GrantPayload, err = json.Marshal(normalized)
			if err != nil {
				return CredentialRecord{}, NewProviderError("credential grant payload is invalid", InvalidCredentialCode, nil)
			}
		default:
			return CredentialRecord{}, NewProviderError("credential record contains an unknown field", InvalidCredentialCode, nil)
		}
	}
	validated, err := validateCredentialRecord(key, &record)
	if err != nil {
		return CredentialRecord{}, NewProviderError("credential record is invalid", InvalidCredentialCode, nil)
	}
	return validated, nil
}

func renderCredentialDocument(document credentialDocument) ([]byte, error) {
	root, err := parseCredentialYAMLRoot(document.source)
	if err != nil {
		root, _ = parseYAMLRoot(nil)
	}
	mapping := root.Content[0]
	// Alias identity, not anchor spelling, determines what retained metadata
	// means. Duplicate anchor names are legal YAML; reparsing alone could let an
	// alias silently retarget after a managed anchor is removed.
	managed := make(map[*yaml.Node]bool)
	var collect func(*yaml.Node)
	collect = func(node *yaml.Node) {
		managed[node] = true
		for _, child := range node.Content {
			collect(child)
		}
	}
	for i := 0; i < len(mapping.Content); i += 2 {
		switch mapping.Content[i].Value {
		case "version", "refs", "records":
			collect(mapping.Content[i+1])
		}
	}
	var referencesManaged func(*yaml.Node) bool
	referencesManaged = func(node *yaml.Node) bool {
		if node.Kind == yaml.AliasNode && managed[node.Alias] {
			return true
		}
		for _, child := range node.Content {
			if referencesManaged(child) {
				return true
			}
		}
		return false
	}
	for i := 0; i < len(mapping.Content); i += 2 {
		switch mapping.Content[i].Value {
		case "version", "refs", "records":
			continue
		}
		if referencesManaged(mapping.Content[i+1]) {
			return nil, NewProviderError("credential edit cannot preserve metadata aliases into managed entries", InvalidCredentialCode, nil)
		}
	}
	setYAMLMappingValue(mapping, "version", &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!int", Value: "1"})
	setYAMLMappingValue(mapping, "refs", encodeCredentialRefs(document.refs))
	records, err := encodeCredentialRecords(document.records, document.recordOrder)
	if err != nil {
		return nil, err
	}
	setYAMLMappingValue(mapping, "records", records)
	var output bytes.Buffer
	encoder := yaml.NewEncoder(&output)
	encoder.SetIndent(2)
	if err := encoder.Encode(root); err != nil {
		return nil, err
	}
	if err := encoder.Close(); err != nil {
		return nil, err
	}
	// Replacing managed nodes may remove anchors referenced by retained foreign
	// metadata. Reject before atomic publication rather than persist dangling
	// aliases and make the next reload/boot unreadable. Never echo YAML values.
	if _, err := parseCredentialYAMLRoot(output.Bytes()); err != nil {
		return nil, NewProviderError("credential edit would invalidate retained YAML aliases", InvalidCredentialCode, nil)
	}
	return output.Bytes(), nil
}

func setYAMLMappingValue(mapping *yaml.Node, name string, next *yaml.Node) {
	for index := 0; index < len(mapping.Content); index += 2 {
		if mapping.Content[index].Value == name {
			previous := mapping.Content[index+1]
			next.HeadComment, next.LineComment, next.FootComment = previous.HeadComment, previous.LineComment, previous.FootComment
			mapping.Content[index+1] = next
			return
		}
	}
	mapping.Content = append(mapping.Content, &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: name}, next)
}

func encodeCredentialRefs(refs map[CredentialRef]string) *yaml.Node {
	node := &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
	names := make([]string, 0, len(refs))
	for ref := range refs {
		names = append(names, string(ref))
	}
	sort.Strings(names)
	for _, name := range names {
		node.Content = append(node.Content,
			&yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: name},
			&yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: refs[CredentialRef(name)]},
		)
	}
	return node
}

func encodeCredentialRecords(records map[CredentialKey]CredentialRecord, order []CredentialKey) (*yaml.Node, error) {
	node := &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
	seen := make(map[CredentialKey]struct{}, len(records))
	ordered := append([]CredentialKey(nil), order...)
	remaining := make([]string, 0)
	for key := range records {
		found := false
		for _, candidate := range ordered {
			if candidate == key {
				found = true
				break
			}
		}
		if !found {
			remaining = append(remaining, string(key))
		}
	}
	sort.Strings(remaining)
	for _, key := range remaining {
		ordered = append(ordered, CredentialKey(key))
	}
	for _, key := range ordered {
		record, found := records[key]
		if !found {
			continue
		}
		if _, duplicate := seen[key]; duplicate {
			continue
		}
		seen[key] = struct{}{}
		recordNode, err := encodeCredentialRecord(record)
		if err != nil {
			return nil, err
		}
		node.Content = append(node.Content, &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: string(key)}, recordNode)
	}
	return node, nil
}

func encodeCredentialRecord(record CredentialRecord) (*yaml.Node, error) {
	node := &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
	appendField := func(name string, value *yaml.Node) {
		node.Content = append(node.Content, &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: name}, value)
	}
	appendField("kind", &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: string(record.Kind)})
	if record.Key != nil {
		appendField("key", &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: *record.Key})
	}
	if record.Env != nil {
		appendField("env", encodeStringMap(record.Env))
	}
	if len(record.GrantPayload) != 0 {
		var decoded any
		if err := json.Unmarshal(record.GrantPayload, &decoded); err != nil {
			return nil, err
		}
		payload := &yaml.Node{}
		if err := payload.Encode(decoded); err != nil {
			return nil, err
		}
		appendField("payload", payload)
	}
	return node, nil
}

func encodeStringMap(values map[string]string) *yaml.Node {
	node := &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		node.Content = append(node.Content,
			&yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: key},
			&yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: values[key]},
		)
	}
	return node
}
