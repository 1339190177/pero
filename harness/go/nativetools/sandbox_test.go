package nativetools

import (
	"github.com/phodal/pero/harness/go/harness"
	"github.com/phodal/pero/harness/go/sandbox"
	"testing"
)

func TestStrictConfigFailsBeforeRegistration(t *testing.T) {
	for _, c := range []Options{
		{Workspace: t.TempDir(), Sandbox: &sandbox.Config{Mode: sandbox.ReadOnly}},
		{Workspace: t.TempDir(), EnableShell: true, Shell: "/bin/bash", Sandbox: &sandbox.Config{Mode: sandbox.Mode("danger-full-access")}},
		{Workspace: t.TempDir(), EnableShell: true, Shell: "/bin/bash", Sandbox: &sandbox.Config{Mode: sandbox.ReadOnly, RuntimePaths: []string{"/"}}},
	} {
		h, e := harness.Boot(t.Context(), harness.Options{})
		if e != nil {
			t.Fatal(e)
		}
		a, e := Mount(t.Context(), h, c)
		if e == nil || a != nil {
			t.Fatalf("unsafe config mounted: %v", e)
		}
	}
}
