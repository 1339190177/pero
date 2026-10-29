//go:build linux || darwin

package sandbox

import (
	"path/filepath"
	"syscall"
	"testing"
)

func TestWorkspaceRejectsFIFO(t *testing.T) {
	root := t.TempDir()
	if err := syscall.Mkfifo(filepath.Join(root, "pipe"), 0600); err != nil {
		t.Skipf("host blocks FIFO fixture: %v", err)
	}
	if err := validateWorkspace(root); err == nil {
		t.Fatal("FIFO accepted")
	}
}
