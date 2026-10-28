package storage

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

type perRecordUnit struct {
	descriptor KVUnitDescriptor
	dir        string
	mu         sync.Mutex
	closed     bool
	onClose    func()
	closeOnce  sync.Once
}

func (u *perRecordUnit) LoadAll(ctx context.Context) (KVSnapshot, error) {
	u.mu.Lock()
	defer u.mu.Unlock()
	if u.closed {
		return KVSnapshot{}, &Error{Code: Closed, Message: fmt.Sprintf("unit %q is closed", u.descriptor.Name)}
	}
	result := KVSnapshot{Tables: make(map[string]map[string]json.RawMessage), Global: json.RawMessage(`null`)}
	for _, table := range u.descriptor.Tables {
		result.Tables[table] = make(map[string]json.RawMessage)
	}
	entries, err := os.ReadDir(u.dir)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return KVSnapshot{}, err
	}
	hasDocuments := false
	for _, entry := range entries {
		if err := ctx.Err(); err != nil {
			return KVSnapshot{}, err
		}
		if entry.Name() == "global.json" && u.descriptor.HasGlobal {
			hasDocuments = true
			if raw, ok := readRecordDocument(filepath.Join(u.dir, entry.Name()), u.descriptor.Version, u.descriptor.CompatibleVersions...); ok {
				result.Global = raw
			}
			continue
		}
		if !entry.IsDir() {
			continue
		}
		records := result.Tables[entry.Name()]
		if records == nil {
			continue
		}
		files, readErr := os.ReadDir(filepath.Join(u.dir, entry.Name()))
		if readErr != nil {
			return KVSnapshot{}, readErr
		}
		for _, file := range files {
			if filepath.Ext(file.Name()) == ".json" {
				hasDocuments = true
				if file.IsDir() {
					continue
				}
				key := file.Name()[:len(file.Name())-5]
				if recordKeyPattern.MatchString(key) {
					if raw, ok := readRecordDocument(filepath.Join(u.dir, entry.Name(), file.Name()), u.descriptor.Version, u.descriptor.CompatibleVersions...); ok {
						records[key] = raw
					}
				}
			}
		}
	}
	if !hasDocuments {
		if err := u.bootstrapLegacy(result); err != nil {
			return KVSnapshot{}, err
		}
	}
	return result, nil
}

func (u *perRecordUnit) bootstrapLegacy(result KVSnapshot) error {
	path := filepath.Join(filepath.Dir(u.dir), u.descriptor.Name+".json")
	raw, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	var document struct {
		Unit struct {
			Name    string          `json:"name"`
			Version json.RawMessage `json:"version"`
		} `json:"unit"`
		Tables map[string]map[string]json.RawMessage `json:"tables"`
	}
	// Explicit predecessor admission also bounds legacy conversion. Keep the
	// historical default for domains that did not opt into compatibility.
	if u.descriptor.CompatibleVersions != nil {
		var envelope, unit map[string]json.RawMessage
		var name string
		if json.Unmarshal(raw, &envelope) != nil || json.Unmarshal(envelope["unit"], &unit) != nil || json.Unmarshal(unit["name"], &name) != nil || name != u.descriptor.Name || !acceptedJSONVersion(unit["version"], u.descriptor.Version, u.descriptor.CompatibleVersions) || json.Unmarshal(envelope["tables"], &document.Tables) != nil {
			return nil
		}
	} else if json.Unmarshal(raw, &document) != nil || document.Unit.Name != u.descriptor.Name {
		return nil
	}
	for table, records := range document.Tables {
		if result.Tables[table] == nil {
			continue
		}
		for key, value := range records {
			if !recordKeyPattern.MatchString(key) {
				continue
			}
			if err := writeRecordDocument(filepath.Join(u.dir, table, key+".json"), u.descriptor.Version, value); err != nil {
				return err
			}
			result.Tables[table][key], _ = detachJSON(value)
		}
	}
	return nil
}

func (u *perRecordUnit) PutRecord(ctx context.Context, table, key string, value json.RawMessage) error {
	u.mu.Lock()
	defer u.mu.Unlock()
	if err := u.assert(table, key); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	return writeRecordDocument(filepath.Join(u.dir, table, key+".json"), u.descriptor.Version, value)
}
func (u *perRecordUnit) DeleteRecord(ctx context.Context, table, key string) error {
	u.mu.Lock()
	defer u.mu.Unlock()
	if err := u.assert(table, key); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	err := os.Remove(filepath.Join(u.dir, table, key+".json"))
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
}

// BackupRecord matches the JSON backend's per-record salvage contract. Rename
// failure leaves the source in place and propagates; no delete/skip fallback.
func (u *perRecordUnit) BackupRecord(ctx context.Context, table, key string) (string, error) {
	u.mu.Lock()
	defer u.mu.Unlock()
	if err := u.assert(table, key); err != nil {
		return "", err
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}
	path := filepath.Join(u.dir, table, key+".json")
	moved := path + ".bak." + time.Now().Format("200601021504")
	if err := os.Rename(path, moved); err != nil {
		return "", err
	}
	return moved, nil
}

func (u *perRecordUnit) SetGlobal(ctx context.Context, value json.RawMessage) error {
	u.mu.Lock()
	defer u.mu.Unlock()
	if u.closed {
		return &Error{Code: Closed, Message: fmt.Sprintf("unit %q is closed", u.descriptor.Name)}
	}
	if !u.descriptor.HasGlobal {
		return fmt.Errorf("storage: unit %q declares no global", u.descriptor.Name)
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	return writeRecordDocument(filepath.Join(u.dir, "global.json"), u.descriptor.Version, value)
}
func (u *perRecordUnit) assert(table, key string) error {
	if u.closed {
		return &Error{Code: Closed, Message: fmt.Sprintf("unit %q is closed", u.descriptor.Name)}
	}
	if !contains(u.descriptor.Tables, table) {
		return fmt.Errorf("storage: unit %q declares no table %q", u.descriptor.Name, table)
	}
	if !recordKeyPattern.MatchString(key) {
		return fmt.Errorf("storage: unit %q per-record key %q is not path-safe", u.descriptor.Name, key)
	}
	return nil
}
func (u *perRecordUnit) Close(context.Context) error {
	u.closeOnce.Do(func() {
		u.mu.Lock()
		u.closed = true
		u.mu.Unlock()
		if u.onClose != nil {
			u.onClose()
		}
	})
	return nil
}

type singleUnit struct {
	descriptor KVUnitDescriptor
	path       string
	mu         sync.Mutex
	closed     bool
	loaded     bool
	state      KVSnapshot
	onClose    func()
	closeOnce  sync.Once
}

func (u *singleUnit) LoadAll(ctx context.Context) (KVSnapshot, error) {
	u.mu.Lock()
	defer u.mu.Unlock()
	if u.closed {
		return KVSnapshot{}, &Error{Code: Closed, Message: fmt.Sprintf("unit %q is closed", u.descriptor.Name)}
	}
	if err := ctx.Err(); err != nil {
		return KVSnapshot{}, err
	}
	if !u.loaded {
		state, err := loadSingleDocument(u.path, u.descriptor)
		if err != nil {
			return KVSnapshot{}, err
		}
		u.state, u.loaded = state, true
	}
	return cloneSnapshot(u.state), nil
}
func (u *singleUnit) PutRecord(ctx context.Context, table, key string, value json.RawMessage) error {
	u.mu.Lock()
	defer u.mu.Unlock()
	if err := u.ensure(table); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	next := cloneSnapshot(u.state)
	raw, err := detachJSON(value)
	if err != nil {
		return err
	}
	if _, existed := next.Tables[table][key]; !existed {
		next.TableOrder[table] = append(next.TableOrder[table], key)
	}
	next.Tables[table][key] = raw
	if err := writeSingleDocument(u.path, u.descriptor, next); err != nil {
		return err
	}
	u.state = next
	return nil
}
func (u *singleUnit) DeleteRecord(ctx context.Context, table, key string) error {
	u.mu.Lock()
	defer u.mu.Unlock()
	if err := u.ensure(table); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	next := cloneSnapshot(u.state)
	delete(next.Tables[table], key)
	next.TableOrder[table] = removeOrderedKey(next.TableOrder[table], key)
	if err := writeSingleDocument(u.path, u.descriptor, next); err != nil {
		return err
	}
	u.state = next
	return nil
}
func (u *singleUnit) SetGlobal(ctx context.Context, value json.RawMessage) error {
	u.mu.Lock()
	defer u.mu.Unlock()
	if u.closed {
		return &Error{Code: Closed, Message: fmt.Sprintf("unit %q is closed", u.descriptor.Name)}
	}
	if !u.descriptor.HasGlobal {
		return fmt.Errorf("storage: unit %q declares no global", u.descriptor.Name)
	}
	if !u.loaded {
		state, err := loadSingleDocument(u.path, u.descriptor)
		if err != nil {
			return err
		}
		u.state, u.loaded = state, true
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	next := cloneSnapshot(u.state)
	raw, err := detachJSON(value)
	if err != nil {
		return err
	}
	next.Global = raw
	if err := writeSingleDocument(u.path, u.descriptor, next); err != nil {
		return err
	}
	u.state = next
	return nil
}
func (u *singleUnit) ensure(table string) error {
	if u.closed {
		return &Error{Code: Closed, Message: fmt.Sprintf("unit %q is closed", u.descriptor.Name)}
	}
	if !contains(u.descriptor.Tables, table) {
		return fmt.Errorf("storage: unit %q declares no table %q", u.descriptor.Name, table)
	}
	if !u.loaded {
		state, err := loadSingleDocument(u.path, u.descriptor)
		if err != nil {
			return err
		}
		u.state, u.loaded = state, true
	}
	return nil
}
func (u *singleUnit) Close(context.Context) error {
	u.closeOnce.Do(func() {
		u.mu.Lock()
		u.closed = true
		u.mu.Unlock()
		if u.onClose != nil {
			u.onClose()
		}
	})
	return nil
}
