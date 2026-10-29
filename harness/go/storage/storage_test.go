package storage

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"testing"
	"time"
)

type counterRecord struct {
	N int `json:"n"`
}

type settingsRecord struct {
	Theme string `json:"theme"`
}

func counterValidator(raw json.RawMessage) error {
	var value counterRecord
	if err := json.Unmarshal(raw, &value); err != nil {
		return err
	}
	if value.N < 0 {
		return errors.New("negative counter")
	}
	return nil
}

func testFacility(t *testing.T, root string) (*JSONBackend, *DomainFacility) {
	t.Helper()
	backend, err := NewJSONBackend(root)
	if err != nil {
		t.Fatal(err)
	}
	registry := NewBackendRegistry()
	if _, err = registry.Register("json", backend); err != nil {
		t.Fatal(err)
	}
	facility, err := NewDomainFacility(registry, DomainConfig{Backend: "json"}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = facility.Close(context.Background()); _ = backend.Close(context.Background()) })
	return backend, facility
}

func counterSpec(version uint64, layout Layout) DomainSpec {
	return DomainSpec{Name: "counter_domain", Version: version, Layout: layout, Tables: map[string]TableSpec{"items": {Validate: counterValidator}}}
}

func settingsValidator(raw json.RawMessage) error {
	if string(raw) == "null" {
		return errors.New("settings must not be null")
	}
	var value settingsRecord
	if err := json.Unmarshal(raw, &value); err != nil {
		return err
	}
	if value.Theme == "" {
		return errors.New("theme is required")
	}
	return nil
}

func TestDomainExposesStablePluginHandlesAndTypedOperations(t *testing.T) {
	root := t.TempDir()
	_, facility := testFacility(t, root)
	domain, err := facility.Open(context.Background(), DomainSpec{
		Name: "plugin_state", Version: 2, Layout: LayoutSingle,
		Tables: map[string]TableSpec{"items": {Validate: JSONValidator(func(value counterRecord) error {
			if value.N < 0 {
				return errors.New("negative counter")
			}
			return nil
		})}},
		Global: &GlobalSpec{Validate: settingsValidator, Initial: json.RawMessage(`{"theme":"dark"}`)},
	})
	if err != nil {
		t.Fatal(err)
	}
	table, err := domain.Table("items")
	if err != nil {
		t.Fatal(err)
	}
	tableAgain, _ := domain.Table("items")
	if table != tableAgain {
		t.Fatal("table handle identity changed")
	}
	global, err := domain.Global()
	if err != nil {
		t.Fatal(err)
	}
	globalAgain, _ := domain.Global()
	if global != globalAgain {
		t.Fatal("global handle identity changed")
	}
	if err = TablePut(context.Background(), table, "main", counterRecord{N: 1}); err != nil {
		t.Fatal(err)
	}
	updated, err := TableUpdate(context.Background(), table, "main", func(value counterRecord) (counterRecord, error) {
		value.N += 4
		return value, nil
	})
	if err != nil || updated.N != 5 {
		t.Fatalf("updated=%+v err=%v", updated, err)
	}
	value, found, err := TableGet[counterRecord](table, "main")
	if err != nil || !found || value.N != 5 {
		t.Fatalf("value=%+v found=%v err=%v", value, found, err)
	}
	raw, err := global.Get()
	if err != nil || string(raw) != `{"theme":"dark"}` {
		t.Fatalf("initial global=%s err=%v", raw, err)
	}
	if err = global.Set(context.Background(), json.RawMessage(`{"theme":"light"}`)); err != nil {
		t.Fatal(err)
	}
	if err = domain.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	reopened, err := facility.Open(context.Background(), DomainSpec{
		Name: "plugin_state", Version: 2, Layout: LayoutSingle,
		Tables: map[string]TableSpec{"items": {Validate: counterValidator}},
		Global: &GlobalSpec{Validate: settingsValidator, Initial: json.RawMessage(`{"theme":"dark"}`)},
	})
	if err != nil {
		t.Fatal(err)
	}
	reopenedGlobal, _ := reopened.Global()
	raw, err = reopenedGlobal.Get()
	if err != nil || string(raw) != `{"theme":"light"}` {
		t.Fatalf("reopened global=%s err=%v", raw, err)
	}
}

func TestDomainSerializesDurableWritesAndReopensBothLayouts(t *testing.T) {
	for _, layout := range []Layout{LayoutSingle, LayoutPerRecord} {
		t.Run(string(layout), func(t *testing.T) {
			_, facility := testFacility(t, t.TempDir())
			domain, err := facility.Open(context.Background(), counterSpec(0, layout))
			if err != nil {
				t.Fatal(err)
			}
			table, err := domain.Table("items")
			if err != nil {
				t.Fatal(err)
			}
			var observed int
			facility.Subscribe(func(DomainChanged) { panic("observer failure") })
			facility.Subscribe(func(change DomainChanged) {
				if change.Operation != "put" {
					return
				}
				raw, found, readErr := table.Get(change.Key)
				if readErr != nil || !found {
					t.Fatalf("post-durable read found=%v err=%v", found, readErr)
				}
				var value counterRecord
				if json.Unmarshal(raw, &value) != nil {
					t.Fatal("bad stored value")
				}
				observed = value.N
			})
			if err = table.Put(context.Background(), "main", json.RawMessage(`{"n":0}`)); err != nil {
				t.Fatal(err)
			}
			const writers = 64
			var group sync.WaitGroup
			for index := 0; index < writers; index++ {
				group.Add(1)
				go func() {
					defer group.Done()
					_, updateErr := table.Update(context.Background(), "main", func(raw json.RawMessage) (json.RawMessage, error) {
						var value counterRecord
						if err := json.Unmarshal(raw, &value); err != nil {
							return nil, err
						}
						value.N++
						return json.Marshal(value)
					})
					if updateErr != nil {
						t.Error(updateErr)
					}
				}()
			}
			group.Wait()
			if observed != writers {
				t.Fatalf("last observed value=%d", observed)
			}
			raw, found, err := table.Get("main")
			var value counterRecord
			_ = json.Unmarshal(raw, &value)
			if err != nil || !found || value.N != writers {
				t.Fatalf("value=%+v found=%v err=%v", value, found, err)
			}
			if err = domain.Close(context.Background()); err != nil {
				t.Fatal(err)
			}
			reopened, err := facility.Open(context.Background(), counterSpec(0, layout))
			if err != nil {
				t.Fatal(err)
			}
			reopenedTable, _ := reopened.Table("items")
			raw, found, err = reopenedTable.Get("main")
			_ = json.Unmarshal(raw, &value)
			if err != nil || !found || value.N != writers {
				t.Fatalf("reopened value=%+v found=%v err=%v", value, found, err)
			}
		})
	}
}

func TestPerRecordFailureLeavesMemoryUntouchedAndVersionBumpDiscardsRow(t *testing.T) {
	root := t.TempDir()
	_, facility := testFacility(t, root)
	domain, err := facility.Open(context.Background(), counterSpec(0, LayoutPerRecord))
	if err != nil {
		t.Fatal(err)
	}
	table, _ := domain.Table("items")
	blocker := filepath.Join(root, "counter_domain", "items", "blocked.json")
	if err = os.MkdirAll(blocker, 0o700); err != nil {
		t.Fatal(err)
	}
	if err = table.Put(context.Background(), "blocked", json.RawMessage(`{"n":1}`)); err == nil {
		t.Fatal("blocked durable write succeeded")
	}
	if _, found, readErr := table.Get("blocked"); readErr != nil || found {
		t.Fatalf("failed write changed memory found=%v err=%v", found, readErr)
	}
	if err = os.RemoveAll(blocker); err != nil {
		t.Fatal(err)
	}
	if err = table.Put(context.Background(), "blocked", json.RawMessage(`{"n":2}`)); err != nil {
		t.Fatal(err)
	}
	if err = domain.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	upgraded, err := facility.Open(context.Background(), counterSpec(1, LayoutPerRecord))
	if err != nil {
		t.Fatal(err)
	}
	upgradedTable, _ := upgraded.Table("items")
	if _, found, err := upgradedTable.Get("blocked"); err != nil || found {
		t.Fatalf("stale per-record version served found=%v err=%v", found, err)
	}
}

func TestSingleLayoutRejectsMissingVersionZeroAndDomainCloseFencesAccess(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "counter_domain.json"), []byte(`{"unit":{"name":"counter_domain"},"global":null,"tables":{"items":{}}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	_, facility := testFacility(t, root)
	if _, err := facility.Open(context.Background(), counterSpec(0, LayoutSingle)); err == nil {
		t.Fatal("missing version was accepted as version zero")
	}
	if err := os.Remove(filepath.Join(root, "counter_domain.json")); err != nil {
		t.Fatal(err)
	}
	domain, err := facility.Open(context.Background(), counterSpec(0, LayoutSingle))
	if err != nil {
		t.Fatal(err)
	}
	table, _ := domain.Table("items")
	if err = domain.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, _, err = table.Get("x"); err == nil {
		t.Fatal("read after close succeeded")
	}
	if err = table.Put(context.Background(), "x", json.RawMessage(`{"n":1}`)); err == nil {
		t.Fatal("write after close succeeded")
	}
}

type blockingBackend struct{ unit *blockingUnit }

func (b *blockingBackend) KV() KVFacet                 { return blockingFacet{unit: b.unit} }
func (b *blockingBackend) Close(context.Context) error { return nil }

type blockingFacet struct{ unit *blockingUnit }

func (f blockingFacet) Open(context.Context, KVUnitDescriptor) (KVUnit, error) { return f.unit, nil }

type blockingUnit struct {
	started chan struct{}
	release chan struct{}
	once    sync.Once
}

func (u *blockingUnit) LoadAll(context.Context) (KVSnapshot, error) {
	return KVSnapshot{Tables: map[string]map[string]json.RawMessage{"items": {}}, Global: json.RawMessage(`null`)}, nil
}
func (u *blockingUnit) PutRecord(_ context.Context, _, key string, _ json.RawMessage) error {
	if key == "slow" {
		u.once.Do(func() { close(u.started) })
		<-u.release
	}
	return nil
}
func (u *blockingUnit) DeleteRecord(context.Context, string, string) error { return nil }
func (u *blockingUnit) SetGlobal(context.Context, json.RawMessage) error   { return nil }
func (u *blockingUnit) Close(context.Context) error                        { return nil }

func TestDomainCloseImmediatelyFencesNewWritesAndDrainsAdmittedWrite(t *testing.T) {
	unit := &blockingUnit{started: make(chan struct{}), release: make(chan struct{})}
	registry := NewBackendRegistry()
	if _, err := registry.Register("blocking", &blockingBackend{unit: unit}); err != nil {
		t.Fatal(err)
	}
	facility, err := NewDomainFacility(registry, DomainConfig{Backend: "blocking"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	domain, err := facility.Open(context.Background(), counterSpec(0, LayoutSingle))
	if err != nil {
		t.Fatal(err)
	}
	table, _ := domain.Table("items")
	writeDone := make(chan error, 1)
	go func() { writeDone <- table.Put(context.Background(), "slow", json.RawMessage(`{"n":1}`)) }()
	<-unit.started
	closeDone := make(chan error, 1)
	go func() { closeDone <- domain.Close(context.Background()) }()
	deadline := time.Now().Add(time.Second)
	for {
		domain.stateMu.RLock()
		disposing := domain.disposing
		domain.stateMu.RUnlock()
		if disposing {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("domain close did not install admission fence")
		}
		runtime.Gosched()
	}
	if err = table.Put(context.Background(), "late", json.RawMessage(`{"n":2}`)); err == nil {
		t.Fatal("write admitted after close began")
	}
	select {
	case err = <-closeDone:
		t.Fatalf("close did not drain admitted write: %v", err)
	default:
	}
	close(unit.release)
	if err = <-writeDone; err != nil {
		t.Fatal(err)
	}
	if err = <-closeDone; err != nil {
		t.Fatal(err)
	}
}

