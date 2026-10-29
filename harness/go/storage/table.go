package storage

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
)

type Table struct {
	domain *Domain
	name   string
}

func (t *Table) Get(key string) (json.RawMessage, bool, error) {
	if t == nil || t.domain == nil {
		return nil, false, errors.New("storage: table is nil")
	}
	t.domain.stateMu.RLock()
	if t.domain.closed {
		t.domain.stateMu.RUnlock()
		return nil, false, &Error{Code: Closed, Domain: t.domain.spec.Name, Message: fmt.Sprintf("domain %q is closed", t.domain.spec.Name)}
	}
	value, ok := t.domain.tables[t.name][key]
	copy := append(json.RawMessage(nil), value...)
	t.domain.stateMu.RUnlock()
	return copy, ok, nil
}

func (t *Table) Entries() (map[string]json.RawMessage, error) {
	if t == nil || t.domain == nil {
		return nil, errors.New("storage: table is nil")
	}
	t.domain.stateMu.RLock()
	if t.domain.closed {
		t.domain.stateMu.RUnlock()
		return nil, &Error{Code: Closed, Domain: t.domain.spec.Name, Message: fmt.Sprintf("domain %q is closed", t.domain.spec.Name)}
	}
	result := make(map[string]json.RawMessage, len(t.domain.tables[t.name]))
	for key, value := range t.domain.tables[t.name] {
		result[key] = append(json.RawMessage(nil), value...)
	}
	t.domain.stateMu.RUnlock()
	return result, nil
}

func (t *Table) Keys() ([]string, error) {
	entries, err := t.Entries()
	if err != nil {
		return nil, err
	}
	keys := make([]string, 0, len(entries))
	for key := range entries {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys, nil
}

func (t *Table) Size() (int, error) {
	entries, err := t.Entries()
	return len(entries), err
}

func (t *Table) Put(ctx context.Context, key string, value json.RawMessage) error {
	next, err := detachJSON(value)
	if err != nil {
		return err
	}
	return t.domain.runWrite(func() error {
		if err := t.domain.unit.PutRecord(ctx, t.name, key, next); err != nil {
			return err
		}
		t.domain.stateMu.Lock()
		if _, existed := t.domain.tables[t.name][key]; !existed {
			t.domain.tableOrder[t.name] = append(t.domain.tableOrder[t.name], key)
		}
		t.domain.tables[t.name][key] = next
		t.domain.stateMu.Unlock()
		t.domain.facility.emit(DomainChanged{Domain: t.domain.spec.Name, Table: t.name, Key: key, Operation: "put", Value: next})
		return nil
	})
}

func (t *Table) Delete(ctx context.Context, key string) (bool, error) {
	deleted := false
	err := t.domain.runWrite(func() error {
		t.domain.stateMu.RLock()
		_, exists := t.domain.tables[t.name][key]
		t.domain.stateMu.RUnlock()
		if !exists {
			return nil
		}
		if err := t.domain.unit.DeleteRecord(ctx, t.name, key); err != nil {
			return err
		}
		t.domain.stateMu.Lock()
		delete(t.domain.tables[t.name], key)
		t.domain.tableOrder[t.name] = removeOrderedKey(t.domain.tableOrder[t.name], key)
		t.domain.stateMu.Unlock()
		deleted = true
		t.domain.facility.emit(DomainChanged{Domain: t.domain.spec.Name, Table: t.name, Key: key, Operation: "deleted"})
		return nil
	})
	return deleted, err
}

func (t *Table) Update(ctx context.Context, key string, transform func(json.RawMessage) (json.RawMessage, error)) (result json.RawMessage, err error) {
	if transform == nil {
		return nil, errors.New("storage: table update transform is nil")
	}
	err = t.domain.runWrite(func() error {
		t.domain.stateMu.RLock()
		current, exists := t.domain.tables[t.name][key]
		current = append(json.RawMessage(nil), current...)
		t.domain.stateMu.RUnlock()
		if !exists {
			return &Error{Code: MissingKey, Domain: t.domain.spec.Name, Table: t.name, Key: key, Message: fmt.Sprintf("domain %q table %q has no record %q to update", t.domain.spec.Name, t.name, key)}
		}
		var next json.RawMessage
		var transformErr error
		func() {
			defer func() {
				if recovered := recover(); recovered != nil {
					transformErr = fmt.Errorf("storage: update transform panicked: %v", recovered)
				}
			}()
			next, transformErr = transform(current)
		}()
		if transformErr != nil {
			return transformErr
		}
		next, transformErr = detachJSON(next)
		if transformErr != nil {
			return transformErr
		}
		if writeErr := t.domain.unit.PutRecord(ctx, t.name, key, next); writeErr != nil {
			return writeErr
		}
		t.domain.stateMu.Lock()
		t.domain.tables[t.name][key] = next
		t.domain.stateMu.Unlock()
		result = append(json.RawMessage(nil), next...)
		t.domain.facility.emit(DomainChanged{Domain: t.domain.spec.Name, Table: t.name, Key: key, Operation: "put", Value: next})
		return nil
	})
	return result, err
}

type Global struct{ domain *Domain }

func (g *Global) Get() (json.RawMessage, error) {
	if g == nil || g.domain == nil {
		return nil, errors.New("storage: global is nil")
	}
	g.domain.stateMu.RLock()
	if g.domain.closed {
		g.domain.stateMu.RUnlock()
		return nil, &Error{Code: Closed, Domain: g.domain.spec.Name, Message: fmt.Sprintf("domain %q is closed", g.domain.spec.Name)}
	}
	value := append(json.RawMessage(nil), g.domain.global...)
	g.domain.stateMu.RUnlock()
	return value, nil
}

func (g *Global) Set(ctx context.Context, value json.RawMessage) error {
	next, err := detachJSON(value)
	if err != nil || isJSONNull(next) {
		if err != nil {
			return err
		}
		return errors.New("storage: global value must not be null")
	}
	return g.domain.runWrite(func() error {
		if err := g.domain.unit.SetGlobal(ctx, next); err != nil {
			return err
		}
		g.domain.stateMu.Lock()
		g.domain.global = next
		g.domain.stateMu.Unlock()
		g.domain.facility.emit(DomainChanged{Domain: g.domain.spec.Name, Operation: "put", Value: next})
		return nil
	})
}

// JSONValidator builds a durable-boundary validator for a typed plugin record.
func JSONValidator[T any](validate func(T) error) Validator {
	return func(raw json.RawMessage) error {
		var value T
		if err := json.Unmarshal(raw, &value); err != nil {
			return err
		}
		if validate != nil {
			return validate(value)
		}
		return nil
	}
}

func TableGet[T any](table *Table, key string) (T, bool, error) {
	var value T
	raw, found, err := table.Get(key)
	if err != nil || !found {
		return value, found, err
	}
	if err = json.Unmarshal(raw, &value); err != nil {
		return value, false, err
	}
	return value, true, nil
}

func TablePut[T any](ctx context.Context, table *Table, key string, value T) error {
	raw, err := json.Marshal(value)
	if err != nil {
		return err
	}
	return table.Put(ctx, key, raw)
}

func TableUpdate[T any](ctx context.Context, table *Table, key string, transform func(T) (T, error)) (T, error) {
	var result T
	if transform == nil {
		return result, errors.New("storage: typed table update transform is nil")
	}
	_, err := table.Update(ctx, key, func(raw json.RawMessage) (json.RawMessage, error) {
		var current T
		if err := json.Unmarshal(raw, &current); err != nil {
			return nil, err
		}
		next, err := transform(current)
		if err != nil {
			return nil, err
		}
		result = next
		return json.Marshal(next)
	})
	return result, err
}

func detachJSON(raw json.RawMessage) (json.RawMessage, error) {
	if !json.Valid(raw) {
		return nil, errors.New("storage: value is not valid JSON")
	}
	var compact bytes.Buffer
	if err := json.Compact(&compact, raw); err != nil {
		return nil, err
	}
	return append(json.RawMessage(nil), compact.Bytes()...), nil
}

func isJSONNull(raw json.RawMessage) bool { return bytes.Equal(bytes.TrimSpace(raw), []byte("null")) }
