//go:build !windows

package session

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/sys/unix"
)

// A fork creates the same shared open-file-description as dup. Retain that
// description deterministically instead of depending on the fork-to-exec race.
func TestSessionWriteLeaseReleaseWithInheritedDescription(t *testing.T) {
	path := filepath.Join(t.TempDir(), "writer.lock")
	owner, err := acquireSessionWriteLease(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = owner.Close() })
	inheritedFD, err := unix.Dup(int(owner.Fd()))
	if err != nil {
		t.Fatal(err)
	}
	unix.CloseOnExec(inheritedFD)
	inherited := os.NewFile(uintptr(inheritedFD), "inherited writer lease")
	t.Cleanup(func() { _ = inherited.Close() })

	closeConcurrently := func(wantSuccessful int) {
		t.Helper()
		start := make(chan struct{})
		results := make(chan error, 16)
		for range 16 {
			go func() { <-start; results <- owner.Close() }()
		}
		close(start)
		successful := 0
		for range 16 {
			err := <-results
			if err == nil {
				successful++
			} else if !errors.Is(err, os.ErrClosed) {
				t.Errorf("concurrent close error=%v", err)
			}
		}
		if successful != wantSuccessful {
			t.Fatalf("successful closes=%d, want %d", successful, wantSuccessful)
		}
	}
	assertExcluded := func(stage string) {
		t.Helper()
		other, err := acquireSessionWriteLease(path)
		if other != nil {
			_ = other.Close()
		}
		if !errors.Is(err, ErrSessionAlreadyOwned) {
			t.Fatalf("%s: competing writer error=%v", stage, err)
		}
	}
	assertExcluded("owner active with inherited description")
	assertExcluded("failed acquisition must not release active owner")
	closeConcurrently(1)
	successor, err := acquireSessionWriteLease(path)
	if err != nil {
		t.Fatalf("retired owner retained lease through inherited description: %v", err)
	}
	t.Cleanup(func() { _ = successor.Close() })
	assertExcluded("successor active")
	if err := inherited.Close(); err != nil {
		t.Fatal(err)
	}
	assertExcluded("closing predecessor description must not unlock successor")
	closeConcurrently(0)
	assertExcluded("failed predecessor release must not unlock successor")
	if err := successor.Close(); err != nil {
		t.Fatal(err)
	}
	final, err := acquireSessionWriteLease(path)
	if err != nil {
		t.Fatalf("successor release retained writer: %v", err)
	}
	if err := final.Close(); err != nil {
		t.Fatal(err)
	}
}
