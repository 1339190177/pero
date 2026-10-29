package fstools

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func assertWindowsNoStage(t *testing.T, root string) {
	t.Helper()
	entries, err := os.ReadDir(root)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), ".pero-create-") {
			t.Fatalf("owned staging file leaked: %s", entry.Name())
		}
	}
}

func TestWindowsWorkspaceCreateNew(t *testing.T) {
	w, root := windowsReadWorkspace(t)
	if w.SupportsReplacement() {
		t.Fatal("Windows must not advertise metadata-unsafe replacement")
	}
	if err := os.Mkdir(filepath.Join(root, "sub"), 0700); err != nil {
		t.Fatal(err)
	}
	text := "created 世界\r\n"
	staged := false
	w.beforePublish = func() {
		staged = true
		if _, err := os.Stat(filepath.Join(root, "sub", "new.txt")); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("target visible before publication: %v", err)
		}
		entries, err := os.ReadDir(filepath.Join(root, "sub"))
		if err != nil || len(entries) != 1 || !strings.HasPrefix(entries[0].Name(), ".pero-create-") {
			t.Fatalf("exclusive staging: %v %v", entries, err)
		}
		opened, err := openScopedTextFile(w.root, "sub/"+entries[0].Name())
		if err != nil {
			t.Fatal(err)
		}
		data, err := io.ReadAll(io.LimitReader(opened, MaxFileBytes+1))
		if closeErr := opened.Close(); closeErr != nil {
			t.Fatal(closeErr)
		}
		if err != nil || string(data) != text {
			t.Fatalf("staged bytes: %q %v", data, err)
		}
	}
	receipt, err := w.Write(t.Context(), "sub/new.txt", text)
	if err != nil || receipt.State != "applied" || receipt.AfterDigest != scopedTextDigest([]byte(text)) || !staged {
		t.Fatalf("create: %+v %v staged=%v", receipt, err, staged)
	}
	got, err := w.Read(t.Context(), "sub/new.txt")
	if err != nil || got.Text != text {
		t.Fatalf("created readback: %+v %v", got, err)
	}
	assertWindowsNoStage(t, filepath.Join(root, "sub"))
}

func TestWindowsWorkspaceCreateNeverReplaces(t *testing.T) {
	w, root := windowsReadWorkspace(t)
	outside := filepath.Join(t.TempDir(), "outside.txt")
	windowsReadFile(t, outside, "original")
	if err := os.Link(outside, filepath.Join(root, "existing.txt")); err != nil {
		t.Fatal(err)
	}
	for _, text := range []string{"replacement", "original"} {
		receipt, err := w.Write(t.Context(), "existing.txt", text)
		if err == nil || receipt.State != "not-applied" {
			t.Fatalf("existing target accepted: %+v %v", receipt, err)
		}
	}
	if _, err := w.Edit(t.Context(), "existing.txt", "original", "replacement", false); err == nil {
		t.Fatal("existing edit accepted")
	}
	data, err := os.ReadFile(outside)
	if err != nil || string(data) != "original" {
		t.Fatalf("outside hardlink changed: %q %v", data, err)
	}
	// Install a hard link after staging to exercise the native no-replace
	// publication, not only the common existing-file preflight.
	w.beforePublish = func() {
		if err := os.Link(outside, filepath.Join(root, "raced.txt")); err != nil {
			t.Fatal(err)
		}
	}
	receipt, err := w.Write(t.Context(), "raced.txt", "must not publish")
	if err == nil || receipt.State != "not-applied" {
		t.Fatalf("raced collision: %+v %v", receipt, err)
	}
	data, err = os.ReadFile(outside)
	if err != nil || string(data) != "original" {
		t.Fatalf("raced hardlink changed: %q %v", data, err)
	}
	assertWindowsNoStage(t, root)
}

func TestWindowsWorkspaceCreateCancellationAndReceipts(t *testing.T) {
	for _, boundary := range []string{"before-stage", "before-publication", "after-publication"} {
		t.Run(boundary, func(t *testing.T) {
			w, root := windowsReadWorkspace(t)
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			switch boundary {
			case "before-stage":
				cancel()
			case "before-publication":
				w.beforePublish = cancel
			case "after-publication":
				w.afterCreate = cancel
			}
			receipt, err := w.Write(ctx, "new.txt", "complete content")
			if !errors.Is(err, context.Canceled) {
				t.Fatalf("cancellation: %+v %v", receipt, err)
			}
			data, readErr := os.ReadFile(filepath.Join(root, "new.txt"))
			if boundary == "after-publication" {
				if receipt.State != "unknown" || readErr != nil || string(data) != "complete content" {
					t.Fatalf("committed cancellation lost effect: %+v %q %v", receipt, data, readErr)
				}
			} else if receipt.State == "applied" || receipt.State == "unknown" || !errors.Is(readErr, os.ErrNotExist) {
				t.Fatalf("precommit cancellation effect: %+v %q %v", receipt, data, readErr)
			}
			assertWindowsNoStage(t, root)
		})
	}
}

func TestWindowsWorkspaceCreateRejectsRedirectsAndInvalidText(t *testing.T) {
	w, root := windowsReadWorkspace(t)
	outside := t.TempDir()
	windowsReadJunction(t, filepath.Join(root, "link"), outside)
	for _, name := range []string{"link/new.txt", "../new.txt", "new.txt:stream", "NUL.txt", "new.txt.", "missing/new.txt"} {
		if _, err := w.Write(t.Context(), name, "not allowed"); err == nil {
			t.Fatalf("invalid creation accepted: %s", name)
		}
	}
	for _, text := range []string{"\x00", "\xff", strings.Repeat("x", MaxFileBytes+1)} {
		if _, err := w.Write(t.Context(), "bad.txt", text); err == nil {
			t.Fatal("unbounded or binary creation accepted")
		}
	}
	if entries, err := os.ReadDir(outside); err != nil || len(entries) != 0 {
		t.Fatalf("outside creation escaped: %v %v", entries, err)
	}
	if _, err := os.Stat(filepath.Join(root, "bad.txt")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("invalid text created target: %v", err)
	}
	assertWindowsNoStage(t, root)
}
