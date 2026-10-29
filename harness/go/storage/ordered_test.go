package storage

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestOrderedTablePreservesMediumOrderUpdateAndReinsert(t *testing.T) {
	root := t.TempDir()
	raw := `{"unit":{"name":"ordered","version":1},"global":null,"tables":{"items":{"z":{"n":1},"a":{"n":2}}}}`
	if err := os.WriteFile(filepath.Join(root, "ordered.json"), []byte(raw), 0600); err != nil {
		t.Fatal(err)
	}
	_, facility := testFacility(t, root)
	spec := DomainSpec{Name: "ordered", Version: 1, Layout: LayoutSingle, Tables: map[string]TableSpec{"items": {Validate: counterValidator}}}
	domain, err := facility.Open(context.Background(), spec)
	if err != nil {
		t.Fatal(err)
	}
	table, _ := domain.Table("items")
	check := func(want []string) {
		t.Helper()
		entries, err := table.OrderedEntries()
		if err != nil {
			t.Fatal(err)
		}
		got := []string{}
		for _, entry := range entries {
			got = append(got, entry.Key)
		}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("order=%v want %v", got, want)
		}
		if len(entries) > 0 {
			entries[0].Value[0] = '!'
			if raw, _, _ := table.Get(entries[0].Key); !json.Valid(raw) {
				t.Fatal("ordered value aliases live storage")
			}
		}
	}
	check([]string{"z", "a"})
	if err = table.Put(t.Context(), "z", json.RawMessage(`{"n":3}`)); err != nil {
		t.Fatal(err)
	}
	if err = table.Put(t.Context(), "m", json.RawMessage(`{"n":4}`)); err != nil {
		t.Fatal(err)
	}
	if _, err = table.Delete(t.Context(), "a"); err != nil {
		t.Fatal(err)
	}
	if err = table.Put(t.Context(), "a", json.RawMessage(`{"n":5}`)); err != nil {
		t.Fatal(err)
	}
	check([]string{"z", "m", "a"})
	if err = domain.Close(t.Context()); err != nil {
		t.Fatal(err)
	}
	domain, err = facility.Open(t.Context(), spec)
	if err != nil {
		t.Fatal(err)
	}
	table, _ = domain.Table("items")
	check([]string{"z", "m", "a"})
}

