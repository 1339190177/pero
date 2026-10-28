package storage

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"os"
	"path/filepath"
	"runtime"
	"sync"
)

// JSONBackend implements DSH's single/per-record human-readable backend.
type JSONBackend struct {
	root   string
	mu     sync.Mutex
	open   map[string]KVUnit
	closed bool
}

func NewJSONBackend(root string) (*JSONBackend, error) {
	if root == "" {
		return nil, errors.New("storage: JSON backend root is empty")
	}
	absolute, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(absolute, 0o700); err != nil {
		return nil, err
	}
	return &JSONBackend{root: absolute, open: make(map[string]KVUnit)}, nil
}

func (b *JSONBackend) KV() KVFacet { return jsonFacet{backend: b} }

type jsonFacet struct{ backend *JSONBackend }

func (f jsonFacet) Open(_ context.Context, descriptor KVUnitDescriptor) (KVUnit, error) {
	if err := validateDescriptor(descriptor); err != nil {
		return nil, err
	}
	b := f.backend
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.closed {
		return nil, &Error{Code: Closed, Message: "json backend is closed"}
	}
	if b.open[descriptor.Name] != nil {
		return nil, fmt.Errorf("storage: unit %q is already open", descriptor.Name)
	}
	var unit KVUnit
	onClose := func(candidate KVUnit) {
		b.mu.Lock()
		if b.open[descriptor.Name] == candidate {
			delete(b.open, descriptor.Name)
		}
		b.mu.Unlock()
	}
	if descriptor.Layout == LayoutPerRecord {
		candidate := &perRecordUnit{descriptor: descriptor, dir: filepath.Join(b.root, descriptor.Name)}
		candidate.onClose = func() { onClose(candidate) }
		unit = candidate
	} else {
		candidate := &singleUnit{descriptor: descriptor, path: filepath.Join(b.root, descriptor.Name+".json")}
		candidate.onClose = func() { onClose(candidate) }
		unit = candidate
	}
	b.open[descriptor.Name] = unit
	return unit, nil
}

func (b *JSONBackend) Close(ctx context.Context) error {
	if b == nil {
		return nil
	}
	b.mu.Lock()
	b.closed = true
	units := make([]KVUnit, 0, len(b.open))
	for _, unit := range b.open {
		units = append(units, unit)
	}
	b.mu.Unlock()
	var result error
	for _, unit := range units {
		result = errors.Join(result, unit.Close(ctx))
	}
	return result
}

func validateDescriptor(descriptor KVUnitDescriptor) error {
	if !unitNamePattern.MatchString(descriptor.Name) {
		return &Error{Code: MalformedMedium, Message: fmt.Sprintf("invalid unit name %q", descriptor.Name)}
	}
	if descriptor.Layout != "" && descriptor.Layout != LayoutSingle && descriptor.Layout != LayoutPerRecord {
		return &Error{Code: MalformedMedium, Message: fmt.Sprintf("invalid layout %q", descriptor.Layout)}
	}
	for _, version := range descriptor.CompatibleVersions {
		if version >= descriptor.Version {
			return &Error{Code: MalformedMedium, Message: fmt.Sprintf("unit %q compatible version %d must be below %d", descriptor.Name, version, descriptor.Version)}
		}
	}
	seen := make(map[string]bool)
	for _, table := range descriptor.Tables {
		if !unitNamePattern.MatchString(table) || seen[table] {
			return &Error{Code: MalformedMedium, Message: fmt.Sprintf("invalid table name %q in unit %q", table, descriptor.Name)}
		}
		seen[table] = true
	}
	return nil
}

func emptySnapshot(descriptor KVUnitDescriptor) KVSnapshot {
	result := KVSnapshot{TableOrder: make(map[string][]string), Tables: make(map[string]map[string]json.RawMessage), Global: json.RawMessage(`null`)}
	for _, table := range descriptor.Tables {
		result.Tables[table] = make(map[string]json.RawMessage)
	}
	return result
}
func cloneSnapshot(source KVSnapshot) KVSnapshot {
	result := KVSnapshot{TableOrder: make(map[string][]string), Tables: make(map[string]map[string]json.RawMessage), Global: append(json.RawMessage(nil), source.Global...)}
	for table, records := range source.Tables {
		copyRecords := make(map[string]json.RawMessage, len(records))
		for key, value := range records {
			copyRecords[key] = append(json.RawMessage(nil), value...)
		}
		result.Tables[table] = copyRecords
		result.TableOrder[table] = append([]string(nil), source.TableOrder[table]...)
	}
	return result
}

func loadSingleDocument(path string, descriptor KVUnitDescriptor) (KVSnapshot, error) {
	raw, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return emptySnapshot(descriptor), nil
	}
	if err != nil {
		return KVSnapshot{}, err
	}
	var document struct {
		Unit struct {
			Name    string  `json:"name"`
			Version *uint64 `json:"version"`
		} `json:"unit"`
		Global json.RawMessage                       `json:"global"`
		Tables map[string]map[string]json.RawMessage `json:"tables"`
	}
	if err := json.Unmarshal(raw, &document); err != nil {
		return KVSnapshot{}, &Error{Code: MalformedMedium, Cause: err, Message: fmt.Sprintf("unit %q file is not valid JSON", descriptor.Name)}
	}
	if document.Unit.Name != descriptor.Name || document.Unit.Version == nil {
		return KVSnapshot{}, &Error{Code: MalformedMedium, Message: fmt.Sprintf("unit %q has a missing or foreign header", descriptor.Name)}
	}
	if *document.Unit.Version != descriptor.Version {
		return KVSnapshot{}, &Error{Code: VersionMismatch, Message: fmt.Sprintf("unit %q stored version %d != expected %d", descriptor.Name, *document.Unit.Version, descriptor.Version)}
	}
	if document.Tables == nil {
		return KVSnapshot{}, &Error{Code: MalformedMedium, Message: fmt.Sprintf("unit %q tables is not an object", descriptor.Name)}
	}
	var orderedDocument struct {
		Tables map[string]json.RawMessage `json:"tables"`
	}
	if err := json.Unmarshal(raw, &orderedDocument); err != nil {
		return KVSnapshot{}, err
	}
	result := emptySnapshot(descriptor)
	for _, table := range descriptor.Tables {
		order, err := decodeRecordOrder(orderedDocument.Tables[table])
		if err != nil {
			return KVSnapshot{}, err
		}
		result.TableOrder[table] = order
	}
	if len(document.Global) != 0 {
		result.Global, _ = detachJSON(document.Global)
	}
	for _, table := range descriptor.Tables {
		for key, value := range document.Tables[table] {
			detached, detachErr := detachJSON(value)
			if detachErr != nil {
				return KVSnapshot{}, &Error{Code: MalformedMedium, Cause: detachErr, Message: fmt.Sprintf("unit %q table %q has invalid JSON", descriptor.Name, table)}
			}
			result.Tables[table][key] = detached
		}
	}
	return result, nil
}

func writeSingleDocument(path string, descriptor KVUnitDescriptor, state KVSnapshot) error {
	document := struct {
		Unit struct {
			Name    string `json:"name"`
			Version uint64 `json:"version"`
		} `json:"unit"`
		Global json.RawMessage            `json:"global"`
		Tables map[string]json.RawMessage `json:"tables"`
	}{Global: state.Global, Tables: make(map[string]json.RawMessage)}
	for table, records := range state.Tables {
		raw, err := marshalOrderedRecords(records, state.TableOrder[table])
		if err != nil {
			return err
		}
		document.Tables[table] = raw
	}
	document.Unit.Name, document.Unit.Version = descriptor.Name, descriptor.Version
	raw, err := json.MarshalIndent(document, "", "  ")
	if err != nil {
		return err
	}
	return writeAtomic(path, append(raw, '\n'))
}

func readRecordDocument(path string, version uint64, compatible ...uint64) (json.RawMessage, bool) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, false
	}
	if compatible != nil {
		// The opted-in DSH envelope uses exact names. Case aliases are foreign
		// documents, not admitted records to move aside for schema failure.
		var document map[string]json.RawMessage
		if json.Unmarshal(raw, &document) != nil || !acceptedJSONVersion(document["version"], version, compatible) {
			return nil, false
		}
		value, err := detachJSON(document["record"])
		return value, err == nil
	}
	var document struct {
		Version *uint64         `json:"version"`
		Record  json.RawMessage `json:"record"`
	}
	if json.Unmarshal(raw, &document) != nil || document.Version == nil || !acceptedRecordVersion(*document.Version, version, compatible) || len(document.Record) == 0 || !json.Valid(document.Record) {
		return nil, false
	}
	value, err := detachJSON(document.Record)
	return value, err == nil
}

func acceptedJSONVersion(raw json.RawMessage, current uint64, compatible []uint64) bool {
	// JSON's equivalent numeric forms (7, 7.0, 7e0) have the same upstream
	// stamp. Exact normalization also preserves native uint64 descriptors
	// above JavaScript's safe-integer limit without rounding foreign stamps.
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 || (raw[0] != '-' && (raw[0] < '0' || raw[0] > '9')) || !json.Valid(raw) {
		return false
	}
	stamped, ok := new(big.Rat).SetString(string(raw))
	if !ok || !stamped.IsInt() || !stamped.Num().IsUint64() {
		return false
	}
	return acceptedRecordVersion(stamped.Num().Uint64(), current, compatible)
}

func acceptedRecordVersion(stored, current uint64, compatible []uint64) bool {
	if stored == current {
		return true
	}
	for _, version := range compatible {
		if stored == version {
			return true
		}
	}
	return false
}

func writeRecordDocument(path string, version uint64, value json.RawMessage) error {
	detached, err := detachJSON(value)
	if err != nil {
		return err
	}
	document := struct {
		Version uint64          `json:"version"`
		Record  json.RawMessage `json:"record"`
	}{version, detached}
	raw, err := json.MarshalIndent(document, "", "  ")
	if err != nil {
		return err
	}
	return writeAtomic(path, append(raw, '\n'))
}

func writeAtomic(path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	file, err := os.CreateTemp(filepath.Dir(path), ".storage-*.tmp")
	if err != nil {
		return err
	}
	temporary := file.Name()
	cleanup := func() { _ = os.Remove(temporary) }
	if err = file.Chmod(0o600); err == nil {
		_, err = file.Write(data)
	}
	if err == nil {
		err = file.Sync()
	}
	closeErr := file.Close()
	if err == nil {
		err = closeErr
	}
	if err != nil {
		cleanup()
		return err
	}
	if err = os.Rename(temporary, path); err != nil {
		cleanup()
		return err
	}
	if runtime.GOOS != "windows" {
		directory, openErr := os.Open(filepath.Dir(path))
		if openErr != nil {
			return openErr
		}
		syncErr := directory.Sync()
		closeErr = directory.Close()
		if syncErr != nil {
			return syncErr
		}
		if closeErr != nil {
			return closeErr
		}
	}
	return nil
}

func contains(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}
