package runtime

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
)

func boolPointer(value bool) *bool { return &value }

func TestDefinitionCatalogRejectsDuplicatesAtomically(t *testing.T) {
	catalog := NewDefinitionCatalog()
	first := eventDefinition("test/catalog", func(*Run) error { return nil })
	second := eventDefinition("test/catalog", func(*Run) error { return nil })
	if err := catalog.Register(first); err != nil {
		t.Fatalf("register first: %v", err)
	}
	if err := catalog.Register(second); !errors.Is(err, ErrDefinitionConflict) {
		t.Fatalf("register duplicate = %v, want ErrDefinitionConflict", err)
	}
	if got := catalog.Descriptors(); len(got) != 1 || got[0].ID != "test/catalog" {
		t.Fatalf("descriptors = %+v", got)
	}
}

func TestApplicationReconcilesCreateUpdateDisableEnableRemove(t *testing.T) {
	type config struct {
		Value int `json:"value"`
	}
	var starts atomic.Int32
	var stops atomic.Int32
	values := make(chan int, 4)
	definition := Define(
		Descriptor{ID: "test/reconciled"},
		DecodeJSON[config],
		func(_ *Run, config config) (Dispose, error) {
			starts.Add(1)
			values <- config.Value
			return func(context.Context) error { stops.Add(1); return nil }, nil
		},
	)
	catalog := NewDefinitionCatalog()
	if err := catalog.Register(definition); err != nil {
		t.Fatalf("register: %v", err)
	}
	application := NewApplication(New(), catalog)
	ctx := testContext(t)
	if err := application.Apply(ctx, []EntrySpec{{ID: "worker", Definition: "test/reconciled", Config: []byte(`{"value":1}`)}}); err != nil {
		t.Fatalf("create: %v", err)
	}
	if got := <-values; got != 1 {
		t.Fatalf("created value = %d", got)
	}
	if err := application.Apply(ctx, []EntrySpec{{ID: "worker", Definition: "test/reconciled", Config: []byte(`{"value":2}`)}}); err != nil {
		t.Fatalf("update: %v", err)
	}
	if got := <-values; got != 2 {
		t.Fatalf("updated value = %d", got)
	}
	if err := application.Apply(ctx, []EntrySpec{{ID: "worker", Definition: "test/reconciled", Config: []byte(`{"value":2}`), Enabled: boolPointer(false)}}); err != nil {
		t.Fatalf("disable: %v", err)
	}
	if snapshot := application.Snapshot(); len(snapshot) != 1 || snapshot[0].Enabled || snapshot[0].Instance != nil {
		t.Fatalf("disabled snapshot = %+v", snapshot)
	}
	if err := application.Apply(ctx, []EntrySpec{{ID: "worker", Definition: "test/reconciled", Config: []byte(`{"value":3}`), Enabled: boolPointer(true)}}); err != nil {
		t.Fatalf("enable: %v", err)
	}
	if got := <-values; got != 3 {
		t.Fatalf("enabled value = %d", got)
	}
	if err := application.Apply(ctx, nil); err != nil {
		t.Fatalf("remove: %v", err)
	}
	if snapshot := application.Snapshot(); len(snapshot) != 0 {
		t.Fatalf("removed snapshot = %+v", snapshot)
	}
	if starts.Load() != 3 || stops.Load() != 3 {
		t.Fatalf("starts/stops = %d/%d, want 3/3", starts.Load(), stops.Load())
	}
	if err := application.Dispose(ctx); err != nil {
		t.Fatalf("dispose: %v", err)
	}
}

func TestApplicationPreflightIsStrictAndDoesNotDisturbRunningEntry(t *testing.T) {
	type config struct {
		Value int `json:"value"`
	}
	definition := Define(
		Descriptor{ID: "test/strict-entry"},
		DecodeJSON[config],
		func(_ *Run, _ config) (Dispose, error) { return nil, nil },
	)
	catalog := NewDefinitionCatalog()
	if err := catalog.Register(definition); err != nil {
		t.Fatalf("register: %v", err)
	}
	application := NewApplication(New(), catalog)
	ctx := testContext(t)
	valid := []byte(`{"entries":[{"id":"worker","definition":"test/strict-entry","config":{"value":1}}]}`)
	if err := application.ApplyJSON(ctx, valid); err != nil {
		t.Fatalf("apply valid: %v", err)
	}
	before := application.Snapshot()[0].Instance.ID
	invalid := []byte(`{"entries":[{"id":"worker","definition":"test/strict-entry","config":{"value":2,"unknown":true}}]}`)
	if err := application.ApplyJSON(ctx, invalid); !errors.Is(err, ErrInvalidApplication) {
		t.Fatalf("invalid config error = %v", err)
	}
	if after := application.Snapshot()[0].Instance.ID; after != before {
		t.Fatalf("running instance changed after invalid document: before=%d after=%d", before, after)
	}
	unknownDocumentField := []byte(`{"entries":[],"unknown":true}`)
	if _, err := DecodeApplicationJSON(unknownDocumentField); !errors.Is(err, ErrInvalidApplication) {
		t.Fatalf("unknown document field error = %v", err)
	}
	if err := application.Dispose(ctx); err != nil {
		t.Fatalf("dispose: %v", err)
	}
}
