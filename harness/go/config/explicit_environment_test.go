package config

import (
	"testing"
)

func TestEnvironmentHomeMustBeExplicit(t *testing.T) {
	t.Setenv("DSH_HOME", t.TempDir())
	if _, err := LoadLayeredEnvironment(LayeredEnvironmentOptions{CWD: t.TempDir(), Environ: []string{"DSH_HOME=/must-not-read"}}); err == nil {
		t.Fatal("implicitly discovered product environment")
	}
}
