package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestEnvironmentHomeMustBeExplicit(t *testing.T) {
	t.Setenv("DSH_HOME", t.TempDir())
	if _, err := LoadLayeredEnvironment(LayeredEnvironmentOptions{CWD: t.TempDir(), Environ: []string{"DSH_HOME=/must-not-read"}}); err == nil {
		t.Fatal("implicitly discovered product environment")
	}
}

func TestFileSettingsCommitPreservesExactIntegerChanges(t *testing.T) {
	for _, value := range []string{"9007199254740993", "9223372036854775807", "-9223372036854775808"} {
		t.Run(value, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "settings.json")
			if err := os.WriteFile(path, []byte(`{"sample":{"id":9007199254740992}}`), 0600); err != nil {
				t.Fatal(err)
			}
			backend, err := NewFileSettingsBackend(FileSettingsOptions{Path: path, DisableWatch: true})
			if err != nil {
				t.Fatal(err)
			}
			next := json.RawMessage(`{"id":` + value + `}`)
			doc, err := backend.CommitSection(t.Context(), "sample", func(json.RawMessage, bool) (json.RawMessage, error) { return next, nil })
			if err != nil {
				t.Fatal(err)
			}
			var returned map[string]json.Number
			if err := json.Unmarshal(doc["sample"], &returned); err != nil || returned["id"].String() != value {
				t.Fatalf("returned section lost integer: %s (%v)", doc["sample"], err)
			}
			raw, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			var stored map[string]map[string]json.Number
			if err := json.Unmarshal(raw, &stored); err != nil {
				t.Fatal(err)
			}
			if stored["sample"]["id"].String() != value {
				t.Fatalf("persisted integer=%s want=%s", stored["sample"]["id"], value)
			}
		})
	}
}
