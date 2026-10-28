package storage

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
)

// TableEntry is a detached record in the storage unit's iteration order.
type TableEntry struct {
	Key   string
	Value json.RawMessage
}

// OrderedEntries takes one atomic snapshot, preserving inserts across updates
// and moving a deleted/reinserted key to the end. Whole-unit JSON retains this
// order on disk without any additional user-visible schema properties.
func (t *Table) OrderedEntries() ([]TableEntry, error) {
	if t == nil || t.domain == nil {
		return nil, errors.New("storage: table is nil")
	}
	t.domain.stateMu.RLock()
	defer t.domain.stateMu.RUnlock()
	if t.domain.closed {
		return nil, &Error{Code: Closed, Domain: t.domain.spec.Name, Message: fmt.Sprintf("domain %q is closed", t.domain.spec.Name)}
	}
	entries := make([]TableEntry, 0, len(t.domain.tables[t.name]))
	for _, key := range t.domain.tableOrder[t.name] {
		if raw, ok := t.domain.tables[t.name][key]; ok {
			entries = append(entries, TableEntry{Key: key, Value: append(json.RawMessage(nil), raw...)})
		}
	}
	return entries, nil
}
func normalizedRecordOrder(records map[string]json.RawMessage, provided []string) []string {
	result := make([]string, 0, len(records))
	seen := map[string]bool{}
	for _, key := range provided {
		if _, exists := records[key]; exists && !seen[key] {
			result = append(result, key)
			seen[key] = true
		}
	}
	missing := []string{}
	for key := range records {
		if !seen[key] {
			missing = append(missing, key)
		}
	}
	sort.Strings(missing)
	return append(result, missing...)
}
func removeOrderedKey(order []string, key string) []string {
	next := make([]string, 0, len(order))
	for _, candidate := range order {
		if candidate != key {
			next = append(next, candidate)
		}
	}
	return next
}
func decodeRecordOrder(raw json.RawMessage) ([]string, error) {
	if len(raw) == 0 || bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		return nil, nil
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	token, err := decoder.Token()
	if err != nil {
		return nil, err
	}
	if token != json.Delim('{') {
		return nil, errors.New("storage: table records must be an object")
	}
	order := []string{}
	seen := map[string]bool{}
	for decoder.More() {
		token, err := decoder.Token()
		if err != nil {
			return nil, err
		}
		key, ok := token.(string)
		if !ok {
			return nil, errors.New("storage: invalid record key")
		}
		var value json.RawMessage
		if err = decoder.Decode(&value); err != nil {
			return nil, err
		}
		if !seen[key] {
			order = append(order, key)
			seen[key] = true
		}
	}
	_, err = decoder.Token()
	return order, err
}
func marshalOrderedRecords(records map[string]json.RawMessage, order []string) (json.RawMessage, error) {
	var output bytes.Buffer
	output.WriteByte('{')
	for i, key := range normalizedRecordOrder(records, order) {
		if i > 0 {
			output.WriteByte(',')
		}
		encoded, _ := json.Marshal(key)
		output.Write(encoded)
		output.WriteByte(':')
		value, err := detachJSON(records[key])
		if err != nil {
			return nil, err
		}
		output.Write(value)
	}
	output.WriteByte('}')
	return output.Bytes(), nil
}
