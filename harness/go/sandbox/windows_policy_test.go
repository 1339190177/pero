package sandbox

import (
	"runtime"
	"testing"
)

func TestWindowsCapabilityIdentity(t *testing.T) {
	// Cross-runtime vectors produced by the pinned official DSH SHA-256 recipe.
	for _, tc := range []struct {
		path      string
		temporary bool
		want      string
	}{
		{`C:\Users\agent\repo`, false, "S-1-4-907248133-152761708"},
		{`C:\Users\agent\repo`, true, "S-1-4-887108711-914846316-1"},
		{`C:\Temp\dsh-a`, true, "S-1-4-486551534-917162856-1"},
	} {
		if got := windowsCapabilitySIDString(tc.path, tc.temporary); got != tc.want {
			t.Fatalf("%q temp=%v: %s", tc.path, tc.temporary, got)
		}
	}
}

func TestWindowsPartialRequiresExplicitBackend(t *testing.T) {
	for _, c := range []Config{
		{Backend: "auto", Mode: ReadOnly},
		{Backend: BackendWindowsACL, Mode: WorkspaceWrite, RuntimePaths: []string{`C:\Windows`}},
		{Backend: BackendWindowsACL, Mode: "full-access"},
	} {
		if err := c.Validate(); err == nil {
			t.Fatalf("accepted unsupported config: %+v", c)
		}
	}
	if runtime.GOOS != "windows" {
		if _, err := New(Config{Backend: BackendWindowsACL, Mode: ReadOnly}, t.TempDir()); err == nil {
			t.Fatal("partial backend silently fell back on non-Windows")
		}
	} else {
		// No opt-in: strict is still unavailable and cannot materialize ACL grants.
		if _, err := New(Config{Mode: WorkspaceWrite}, t.TempDir()); err == nil {
			t.Fatal("strict Windows silently fell back to partial")
		}
	}
}
