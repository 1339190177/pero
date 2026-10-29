package storage

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

func putCompatibilityDocument(t *testing.T, root, key string, version uint64, value string) []byte {
	t.Helper()
	raw := []byte(fmt.Sprintf(`{"version":%d,"record":%s}`, version, value))
	path := filepath.Join(root, "counter_domain", "items", key+".json")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	return raw
}

func TestPerRecordCompatibilityIsOptInAndWritesCurrentStamp(t *testing.T) {
	for _, compatible := range []bool{false, true} {
		t.Run(fmt.Sprint(compatible), func(t *testing.T) {
			root := t.TempDir()
			for version := uint64(3); version <= 8; version++ {
				putCompatibilityDocument(t, root, fmt.Sprintf("v%d", version), version, `{"n":1}`)
			}
			_, facility := testFacility(t, root)
			spec := counterSpec(7, LayoutPerRecord)
			if compatible {
				spec.CompatibleVersions = []uint64{3, 4, 5, 6}
			}
			domain, err := facility.Open(t.Context(), spec)
			if err != nil {
				t.Fatal(err)
			}
			table, _ := domain.Table("items")
			for version := uint64(3); version <= 8; version++ {
				_, found, err := table.Get(fmt.Sprintf("v%d", version))
				want := version == 7 || compatible && version < 7
				if err != nil || found != want {
					t.Fatalf("v%d found=%v want=%v err=%v", version, found, want, err)
				}
			}
			if err = table.Put(t.Context(), "v3", json.RawMessage(`{"n":2}`)); err != nil {
				t.Fatal(err)
			}
			raw, err := os.ReadFile(filepath.Join(root, "counter_domain", "items", "v3.json"))
			if err != nil {
				t.Fatal(err)
			}
			var document struct {
				Version uint64 `json:"version"`
			}
			if err = json.Unmarshal(raw, &document); err != nil || document.Version != 7 {
				t.Fatalf("rewrite=%s err=%v", raw, err)
			}
		})
	}
}

func TestSingleLayoutStillRejectsCompatibleVersion(t *testing.T) {
	root := t.TempDir()
	_, facility := testFacility(t, root)
	domain, err := facility.Open(t.Context(), counterSpec(3, LayoutSingle))
	if err != nil {
		t.Fatal(err)
	}
	table, _ := domain.Table("items")
	if err = table.Put(t.Context(), "old", json.RawMessage(`{"n":1}`)); err != nil {
		t.Fatal(err)
	}
	if err = domain.Close(t.Context()); err != nil {
		t.Fatal(err)
	}
	spec := counterSpec(7, LayoutSingle)
	spec.CompatibleVersions = []uint64{3}
	_, err = facility.Open(t.Context(), spec)
	var classified *Error
	if !errors.As(err, &classified) || classified.Code != VersionMismatch {
		t.Fatalf("whole-unit compatibility changed: %v", err)
	}
}

func TestLegacyBootstrapCompatibilityIsBoundedOnlyWhenOptedIn(t *testing.T) {
	for _, compatible := range []bool{false, true} {
		for _, version := range []uint64{2, 3, 7, 8} {
			t.Run(fmt.Sprintf("compatible=%v/v%d", compatible, version), func(t *testing.T) {
				root := t.TempDir()
				path := filepath.Join(root, "counter_domain.json")
				original := []byte(fmt.Sprintf(`{"unit":{"name":"counter_domain","version":%d},"tables":{"items":{"old":{"n":1}}}}`, version))
				if err := os.WriteFile(path, original, 0o600); err != nil {
					t.Fatal(err)
				}
				_, facility := testFacility(t, root)
				spec := counterSpec(7, LayoutPerRecord)
				if compatible {
					spec.CompatibleVersions = []uint64{3, 4, 5, 6}
				}
				domain, err := facility.Open(t.Context(), spec)
				if err != nil {
					t.Fatal(err)
				}
				table, _ := domain.Table("items")
				_, found, err := table.Get("old")
				// Other domains retain their previously shipped legacy behavior;
				// cache opt-in accepts only the proved version set.
				want := !compatible || version == 3 || version == 7
				if err != nil || found != want {
					t.Fatalf("found=%v want=%v err=%v", found, want, err)
				}
				preserved, err := os.ReadFile(path)
				if err != nil || !bytes.Equal(preserved, original) {
					t.Fatalf("legacy source changed: %s %v", preserved, err)
				}
				if !want {
					if _, err = os.Stat(filepath.Join(root, "counter_domain", "items", "old.json")); !errors.Is(err, os.ErrNotExist) {
						t.Fatalf("unaccepted legacy stamp materialized a record: %v", err)
					}
				}
			})
		}
	}
}

func TestLegacyBootstrapEmptyCompatibilityAndHistoricalStampDefaults(t *testing.T) {
	for _, stamp := range []string{`2`, `7`, `8`, `"old"`, `2.5`, `-1`, `null`} {
		for _, explicit := range []bool{false, true} {
			t.Run(fmt.Sprintf("stamp=%s/explicit=%v", stamp, explicit), func(t *testing.T) {
				root := t.TempDir()
				raw := []byte(fmt.Sprintf(`{"unit":{"name":"counter_domain","version":%s},"tables":{"items":{"old":{"n":1}}}}`, stamp))
				if err := os.WriteFile(filepath.Join(root, "counter_domain.json"), raw, 0o600); err != nil {
					t.Fatal(err)
				}
				_, facility := testFacility(t, root)
				spec := counterSpec(7, LayoutPerRecord)
				if explicit {
					spec.CompatibleVersions = []uint64{}
				}
				domain, err := facility.Open(t.Context(), spec)
				if err != nil {
					t.Fatal(err)
				}
				table, _ := domain.Table("items")
				_, found, err := table.Get("old")
				want := !explicit || stamp == "7"
				if err != nil || found != want {
					t.Fatalf("found=%v want=%v err=%v", found, want, err)
				}
			})
		}
	}
}

func TestInvalidRecordBackupIsOptInAndRawMalformedStaysUntouched(t *testing.T) {
	for _, policy := range []InvalidRecordsPolicy{"", BackupAndSkip} {
		t.Run(string(policy), func(t *testing.T) {
			root := t.TempDir()
			bad := putCompatibilityDocument(t, root, "bad", 7, `{"n":-1}`)
			putCompatibilityDocument(t, root, "good", 3, `{"n":2}`)
			rawPath := filepath.Join(root, "counter_domain", "items", "malformed.json")
			malformed := []byte(`{"version":7,"record":`)
			if err := os.WriteFile(rawPath, malformed, 0o600); err != nil {
				t.Fatal(err)
			}
			_, facility := testFacility(t, root)
			spec := counterSpec(7, LayoutPerRecord)
			spec.CompatibleVersions, spec.InvalidRecords = []uint64{3}, policy
			domain, err := facility.Open(t.Context(), spec)
			if policy == "" {
				var classified *Error
				if !errors.As(err, &classified) || classified.Code != InvalidRecord {
					t.Fatalf("default no longer fails on invalid record: %v", err)
				}
				unchanged, readErr := os.ReadFile(filepath.Join(root, "counter_domain", "items", "bad.json"))
				if readErr != nil || !bytes.Equal(unchanged, bad) {
					t.Fatalf("default changed record: %s %v", unchanged, readErr)
				}
			} else {
				if err != nil {
					t.Fatal(err)
				}
				table, _ := domain.Table("items")
				for _, key := range []string{"bad", "malformed"} {
					if _, found, err := table.Get(key); err != nil || found {
						t.Fatalf("invalid %s present=%v err=%v", key, found, err)
					}
				}
				if _, found, err := table.Get("good"); err != nil || !found {
					t.Fatalf("good neighbor absent: %v", err)
				}
				backups, err := filepath.Glob(filepath.Join(root, "counter_domain", "items", "bad.json.bak.*"))
				if err != nil || len(backups) != 1 {
					t.Fatalf("backups=%v err=%v", backups, err)
				}
				preserved, err := os.ReadFile(backups[0])
				if err != nil || !bytes.Equal(preserved, bad) {
					t.Fatalf("backup changed bytes: %s %v", preserved, err)
				}
			}
			unchanged, err := os.ReadFile(rawPath)
			if err != nil || !bytes.Equal(unchanged, malformed) {
				t.Fatalf("malformed raw envelope changed: %s %v", unchanged, err)
			}
			backups, err := filepath.Glob(rawPath + ".bak.*")
			if err != nil || len(backups) != 0 {
				t.Fatalf("raw malformed file was treated as a schema record: %v %v", backups, err)
			}
		})
	}
}

type failingRecordBackup struct{ KVUnit }

func (f failingRecordBackup) BackupRecord(context.Context, string, string) (string, error) {
	return "", errors.New("fixture backup failure")
}

func TestInvalidRecordCannotSkipWithoutSuccessfulBackup(t *testing.T) {
	root := t.TempDir()
	original := putCompatibilityDocument(t, root, "bad", 7, `{"n":-1}`)
	backend, facility := testFacility(t, root)
	spec := counterSpec(7, LayoutPerRecord)
	spec.InvalidRecords = BackupAndSkip
	unit, err := backend.KV().Open(t.Context(), spec.descriptor())
	if err != nil {
		t.Fatal(err)
	}
	defer unit.Close(context.Background())
	snapshot, err := unit.LoadAll(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	// Embedding the base interface deliberately hides the optional capability.
	withoutBackup := struct{ KVUnit }{unit}
	for _, candidate := range []KVUnit{withoutBackup, failingRecordBackup{unit}} {
		if _, err = newDomain(t.Context(), facility, spec, candidate, snapshot); err == nil {
			t.Fatal("failed/unsupported backup silently skipped invalid record")
		}
	}
	unchanged, err := os.ReadFile(filepath.Join(root, "counter_domain", "items", "bad.json"))
	if err != nil || !bytes.Equal(unchanged, original) {
		t.Fatalf("failed backup lost source bytes: %s %v", unchanged, err)
	}
}

func TestCompatibilitySpecRejectsUnprovenVersionsAndPolicies(t *testing.T) {
	for _, version := range []uint64{7, 8} {
		spec := counterSpec(7, LayoutPerRecord)
		spec.CompatibleVersions = []uint64{version}
		if spec.Validate() == nil || validateDescriptor(spec.descriptor()) == nil {
			t.Fatalf("admitted invalid compatible version %d", version)
		}
	}
	spec := counterSpec(7, LayoutPerRecord)
	spec.InvalidRecords = "skip"
	if spec.Validate() == nil {
		t.Fatal("admitted unsupported invalid-record policy")
	}
}

func TestCompatibleEnvelopeUsesExactFieldsAndNumericStamps(t *testing.T) {
	for _, fixture := range []struct {
		name, document     string
		explicit, original bool
	}{
		{"uppercase", `{"Version":7,"Record":null}`, false, true},
		{"missing lowercase record", `{"version":7,"Record":null}`, false, true},
		{"foreign canonical stamp", `{"version":8,"Version":7,"record":null}`, false, true},
		{"valid canonical stamp", `{"version":7,"Version":8,"record":null}`, true, false},
		{"decimal", `{"version":7.0,"record":null}`, true, false},
		{"exponent", `{"version":7e0,"record":null}`, true, false},
		{"null record", `{"version":7,"record":null}`, true, true},
		{"missing record", `{"version":7}`, false, false},
	} {
		t.Run(fixture.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "record.json")
			if err := os.WriteFile(path, []byte(fixture.document), 0o600); err != nil {
				t.Fatal(err)
			}
			for _, explicit := range []bool{false, true} {
				var compatible []uint64
				want := fixture.original
				if explicit {
					compatible, want = []uint64{}, fixture.explicit
				}
				_, found := readRecordDocument(path, 7, compatible...)
				if found != want {
					t.Fatalf("explicit=%v found=%v want=%v", explicit, found, want)
				}
			}
			unchanged, err := os.ReadFile(path)
			if err != nil || string(unchanged) != fixture.document {
				t.Fatalf("envelope read changed source: %s %v", unchanged, err)
			}
		})
	}
	for _, fixture := range []struct {
		raw     string
		current uint64
		want    bool
	}{
		{"9007199254740993", 9007199254740992, false},
		{"9007199254740993.0", 9007199254740993, true},
		{"18446744073709551615", ^uint64(0), true},
		{"18446744073709551616", ^uint64(0), false},
	} {
		if got := acceptedJSONVersion([]byte(fixture.raw), fixture.current, nil); got != fixture.want {
			t.Fatalf("stamp=%s current=%d accepted=%v", fixture.raw, fixture.current, got)
		}
	}
}

func TestLegacyBootstrapDoesNotRestoreDeletedLastRecord(t *testing.T) {
	root := t.TempDir()
	legacy := filepath.Join(root, "counter_domain.json")
	if err := os.WriteFile(legacy, []byte(`{"unit":{"name":"counter_domain","version":7},"tables":{"items":{"old":{"n":1}}}}`), 0600); err != nil {
		t.Fatal(err)
	}
	backend, err := NewJSONBackend(root)
	if err != nil {
		t.Fatal(err)
	}
	defer backend.Close(t.Context())
	descriptor := KVUnitDescriptor{Name: "counter_domain", Version: 7, CompatibleVersions: []uint64{}, Tables: []string{"items"}, Layout: LayoutPerRecord}
	unit, err := backend.KV().Open(t.Context(), descriptor)
	if err != nil {
		t.Fatal(err)
	}
	initial, err := unit.LoadAll(t.Context())
	if err != nil || len(initial.Tables["items"]) != 1 {
		t.Fatalf("initial=%+v err=%v", initial, err)
	}
	if err := unit.DeleteRecord(t.Context(), "items", "old"); err != nil {
		t.Fatal(err)
	}
	if err := unit.Close(t.Context()); err != nil {
		t.Fatal(err)
	}
	unit, err = backend.KV().Open(t.Context(), descriptor)
	if err != nil {
		t.Fatal(err)
	}
	restored, err := unit.LoadAll(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if len(restored.Tables["items"]) != 0 {
		t.Fatalf("deleted legacy record resurrected: %+v", restored.Tables["items"])
	}
}

func TestLegacyBootstrapRecognizesOnlyDeclaredState(t *testing.T) {
	for _, fixture := range []struct {
		name, directory, document string
		wantLegacy                bool
	}{
		{"empty declared table", "items", "", false},
		{"malformed declared record", "items", "broken.json", false},
		{"unknown directory", "other", "", true},
	} {
		t.Run(fixture.name, func(t *testing.T) {
			root := t.TempDir()
			legacy := []byte(`{"unit":{"name":"counter_domain","version":7},"tables":{"items":{"old":{"n":1}}}}`)
			path := filepath.Join(root, "counter_domain.json")
			if err := os.WriteFile(path, legacy, 0600); err != nil {
				t.Fatal(err)
			}
			dir := filepath.Join(root, "counter_domain", fixture.directory)
			if err := os.MkdirAll(dir, 0700); err != nil {
				t.Fatal(err)
			}
			if fixture.document != "" {
				if err := os.WriteFile(filepath.Join(dir, fixture.document), []byte("{broken"), 0600); err != nil {
					t.Fatal(err)
				}
			}
			_, facility := testFacility(t, root)
			domain, err := facility.Open(t.Context(), counterSpec(7, LayoutPerRecord))
			if err != nil {
				t.Fatal(err)
			}
			defer domain.Close(t.Context())
			table, _ := domain.Table("items")
			if _, found, err := table.Get("old"); err != nil || found != fixture.wantLegacy {
				t.Fatalf("legacy found=%v want=%v err=%v", found, fixture.wantLegacy, err)
			}
			after, err := os.ReadFile(path)
			if err != nil || !bytes.Equal(after, legacy) {
				t.Fatalf("legacy bytes changed: %s %v", after, err)
			}
			if fixture.document != "" {
				after, err := os.ReadFile(filepath.Join(dir, fixture.document))
				if err != nil || string(after) != "{broken" {
					t.Fatalf("malformed record changed: %s %v", after, err)
				}
			}
		})
	}
}
