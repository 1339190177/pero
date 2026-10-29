//go:build windows

package fstools

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

const windowsReadSharingViolation = syscall.Errno(32) // ERROR_SHARING_VIOLATION is not exported by syscall.

// These contracts only use files and junctions in test-owned temporary trees.
// They require native success, without ACL changes, elevated privileges or skips.
func windowsReadWorkspace(t *testing.T) (*Workspace, string) {
	t.Helper()
	root := filepath.Join(t.TempDir(), "workspace")
	if err := os.Mkdir(root, 0700); err != nil {
		t.Fatal(err)
	}
	w, err := OpenWorkspace(root)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := w.Close(); err != nil {
			t.Error(err)
		}
	})
	return w, root
}

func windowsReadFile(t *testing.T, name, text string) {
	t.Helper()
	if err := os.WriteFile(name, []byte(text), 0600); err != nil {
		t.Fatal(err)
	}
}

func windowsReadJunction(t *testing.T, link, target string) {
	t.Helper()
	windowsRoot := os.Getenv("SystemRoot")
	if windowsRoot == "" || !filepath.IsAbs(windowsRoot) {
		t.Fatal("native junction fixture requires an absolute SystemRoot")
	}
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	output, err := exec.CommandContext(ctx, filepath.Join(windowsRoot, "System32", "cmd.exe"), "/d", "/c", "mklink", "/J", link, target).CombinedOutput()
	if err != nil {
		t.Fatalf("create test-owned junction %q -> %q: %v: %s", link, target, err, output)
	}
	t.Cleanup(func() {
		if err := os.Remove(link); err != nil && !errors.Is(err, os.ErrNotExist) {
			t.Error(err)
		}
	})
	info, err := os.Lstat(link)
	if err != nil {
		t.Fatal(err)
	}
	attrs, ok := info.Sys().(*syscall.Win32FileAttributeData)
	if !ok || attrs.FileAttributes&syscall.FILE_ATTRIBUTE_REPARSE_POINT == 0 {
		t.Fatalf("junction fixture is not a native reparse point: %+v", info)
	}
}

func TestWindowsWorkspaceReadListSearch(t *testing.T) {
	w, root := windowsReadWorkspace(t)
	if err := os.Mkdir(filepath.Join(root, "src"), 0700); err != nil {
		t.Fatal(err)
	}
	windowsReadFile(t, filepath.Join(root, "src", "unicode.txt"), "first\r\nneedle 世界\r\n")
	value, err := w.Read(t.Context(), "src/unicode.txt")
	if err != nil || value.Path != "src/unicode.txt" || value.Text != "first\r\nneedle 世界\r\n" || value.Digest != scopedTextDigest([]byte(value.Text)) {
		t.Fatalf("native read: %+v %v", value, err)
	}
	listed, err := w.List(t.Context(), "src")
	if err != nil || listed.Truncated || len(listed.Entries) != 1 || listed.Entries[0].Name != "unicode.txt" || listed.Entries[0].Directory || listed.Entries[0].Symlink {
		t.Fatalf("native list: %+v %v", listed, err)
	}
	found, err := w.Search(t.Context(), "", "needle")
	if err != nil || found.Truncated || found.ScannedFiles != 1 || len(found.Matches) != 1 || found.Matches[0].Path != "src/unicode.txt" || found.Matches[0].Line != 2 {
		t.Fatalf("native search: %+v %v", found, err)
	}
	if err := w.CheckCWD(root); err != nil {
		t.Fatalf("configured cwd rejected: %v", err)
	}
	if err := w.CheckCWD(t.TempDir()); err == nil {
		t.Fatal("unconfigured cwd accepted")
	}
	// An already-present hard link is readable workspace authority, not a claim
	// of provenance. No operation in this contract mutates either link.
	original := filepath.Join(t.TempDir(), "original.txt")
	windowsReadFile(t, original, "approved linked text")
	if err := os.Link(original, filepath.Join(root, "linked.txt")); err != nil {
		t.Fatal(err)
	}
	linked, err := w.Read(t.Context(), "linked.txt")
	if err != nil || linked.Text != "approved linked text" {
		t.Fatalf("preexisting hard-link read: %+v %v", linked, err)
	}
}

func TestWindowsWorkspaceRejectsPathAliases(t *testing.T) {
	w, root := windowsReadWorkspace(t)
	windowsReadFile(t, filepath.Join(root, "ordinary.txt"), "ordinary")
	// Prove ADS rejection on a real stream, not merely a missing path.
	windowsReadFile(t, filepath.Join(root, "ordinary.txt")+":hidden", "hidden stream")
	if err := os.Mkdir(filepath.Join(root, "dir"), 0700); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"COM12", "LPT23"} {
		windowsReadFile(t, filepath.Join(root, name), "valid ordinary filename")
		value, err := w.Read(t.Context(), name)
		if err != nil || value.Text != "valid ordinary filename" {
			t.Fatalf("nonreserved filename rejected %q: %+v %v", name, value, err)
		}
	}
	for _, name := range []string{
		"../ordinary.txt", "dir/../../ordinary.txt", "./ordinary.txt", "dir//ordinary.txt",
		filepath.Join(root, "ordinary.txt"), `\ordinary.txt`, `dir\ordinary.txt`,
		"ordinary.txt:hidden", "ordinary.txt::$DATA", "C:ordinary.txt", "ordinary.txt\x00",
		"NUL", "NUL.txt", "CON", "CON .txt", "COM1", "LPT1.txt", "COM¹", "ordinary.txt.", "ordinary.txt ", "dir.", "dir ",
	} {
		t.Run(fmt.Sprintf("%q", name), func(t *testing.T) {
			if value, err := w.Read(t.Context(), name); err == nil || value.Text != "" {
				t.Fatalf("invalid read path accepted: %+v %v", value, err)
			}
			if listed, err := w.List(t.Context(), name); err == nil || len(listed.Entries) != 0 {
				t.Fatalf("invalid list path accepted: %+v %v", listed, err)
			}
			if found, err := w.Search(t.Context(), name, "ordinary"); err == nil || len(found.Matches) != 0 {
				t.Fatalf("invalid search path accepted: %+v %v", found, err)
			}
		})
	}
}

func TestWindowsWorkspaceJunctionBoundaries(t *testing.T) {
	w, root := windowsReadWorkspace(t)
	outside := t.TempDir()
	windowsReadFile(t, filepath.Join(outside, "secret.txt"), "outside-only needle")
	if err := os.Mkdir(filepath.Join(root, "real"), 0700); err != nil {
		t.Fatal(err)
	}
	windowsReadFile(t, filepath.Join(root, "real", "inside.txt"), "inside needle")
	windowsReadJunction(t, filepath.Join(root, "outside-link"), outside)
	windowsReadJunction(t, filepath.Join(root, "inside-link"), filepath.Join(root, "real"))
	for _, link := range []string{"outside-link", "inside-link"} {
		file := "secret.txt"
		if link == "inside-link" {
			file = "inside.txt"
		}
		// Exercise the native boundary directly: preflight Lstat is not the
		// protection against a selected component changing before open.
		if opened, err := openScopedTextFile(w.root, link+"/"+file); err == nil {
			opened.Close()
			t.Fatalf("native handle open followed reparse point %s", link)
		}
		if value, err := w.Read(t.Context(), link+"/"+file); err == nil || value.Text != "" {
			t.Fatalf("read followed selected junction %s: %+v %v", link, value, err)
		}
		if listed, err := w.List(t.Context(), link); err == nil || len(listed.Entries) != 0 {
			t.Fatalf("list followed selected junction %s: %+v %v", link, listed, err)
		}
		if found, err := w.Search(t.Context(), link, "needle"); err == nil || len(found.Matches) != 0 {
			t.Fatalf("search followed selected junction %s: %+v %v", link, found, err)
		}
	}
	found, err := w.Search(t.Context(), "", "needle")
	if err != nil || len(found.Matches) != 1 || found.Matches[0].Path != "real/inside.txt" {
		t.Fatalf("search must skip junction children: %+v %v", found, err)
	}
	// A trusted launch-time root alias selects the resolved directory, unlike
	// an untrusted tool-selected child junction.
	alias := filepath.Join(t.TempDir(), "root-alias")
	windowsReadJunction(t, alias, root)
	selected, err := OpenWorkspace(alias)
	if err != nil {
		t.Fatalf("trusted root junction admission: %v", err)
	}
	defer selected.Close()
	if err := selected.CheckCWD(root); err != nil {
		t.Fatalf("resolved root cwd: %v", err)
	}
	if err := selected.CheckCWD(alias); err != nil {
		t.Fatalf("equivalent trusted root cwd: %v", err)
	}
	value, err := selected.Read(t.Context(), "real/inside.txt")
	if err != nil || value.Text != "inside needle" {
		t.Fatalf("resolved root read: %+v %v", value, err)
	}
}

func TestWindowsWorkspaceReadBoundsAndCancellation(t *testing.T) {
	w, root := windowsReadWorkspace(t)
	for name, text := range map[string]string{
		"nul.txt": "a\x00b", "invalid-utf8.txt": "\xff", "oversized.txt": strings.Repeat("x", MaxFileBytes+1),
	} {
		windowsReadFile(t, filepath.Join(root, name), text)
		if value, err := w.Read(t.Context(), name); err == nil || value.Text != "" {
			t.Fatalf("unbounded or binary text accepted %s: bytes=%d err=%v", name, len(value.Text), err)
		}
	}
	windowsReadFile(t, filepath.Join(root, "exact.txt"), strings.Repeat("x", MaxFileBytes))
	value, err := w.Read(t.Context(), "exact.txt")
	if err != nil || len(value.Text) != MaxFileBytes {
		t.Fatalf("exact read bound: bytes=%d err=%v", len(value.Text), err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := w.Read(ctx, "exact.txt"); !errors.Is(err, context.Canceled) {
		t.Fatalf("read cancellation: %v", err)
	}
	if _, err := w.List(ctx, ""); !errors.Is(err, context.Canceled) {
		t.Fatalf("list cancellation: %v", err)
	}
	if _, err := w.Search(ctx, "", "x"); !errors.Is(err, context.Canceled) {
		t.Fatalf("search cancellation: %v", err)
	}
}

func TestWindowsWorkspaceTraversalBounds(t *testing.T) {
	w, root := windowsReadWorkspace(t)
	for i := 0; i < 513; i++ {
		windowsReadFile(t, filepath.Join(root, fmt.Sprintf("file-%03d.txt", i)), "x")
	}
	listed, err := w.List(t.Context(), "")
	if err != nil || !listed.Truncated || len(listed.Entries) != MaxEntries {
		t.Fatalf("directory bound: entries=%d truncated=%t err=%v", len(listed.Entries), listed.Truncated, err)
	}
	found, err := w.Search(t.Context(), "", "not-present")
	if err != nil || !found.Truncated || found.ScannedFiles != 512 || len(found.Matches) != 0 {
		t.Fatalf("search entry bound: %+v %v", found, err)
	}
	if err := os.Mkdir(filepath.Join(root, "matches"), 0700); err != nil {
		t.Fatal(err)
	}
	windowsReadFile(t, filepath.Join(root, "matches", "many.txt"), strings.Repeat("needle\n", 70))
	found, err = w.Search(t.Context(), "matches", "needle")
	if err != nil || !found.Truncated || found.ScannedFiles != 1 || len(found.Matches) != 64 || found.Matches[63].Line != 64 {
		t.Fatalf("search match bound: %+v %v", found, err)
	}
}

func TestWindowsWorkspacePinnedRootIdentity(t *testing.T) {
	w, root := windowsReadWorkspace(t)
	windowsReadFile(t, filepath.Join(root, "same.txt"), "approved")
	// Do not read before replacement: admission must capture the original ID
	// eagerly, not let the first SameFile resolve a stale pathname later.
	if err := os.Rename(root, root+"-original"); err != nil {
		if !errors.Is(err, windowsReadSharingViolation) && !errors.Is(err, syscall.ERROR_ACCESS_DENIED) {
			t.Fatalf("unexpected root rename failure: %v", err)
		}
		// Current Go opens the root without FILE_SHARE_DELETE, so Windows
		// prevents replacement while the capability is alive.
		value, readErr := w.Read(t.Context(), "same.txt")
		if readErr != nil || value.Text != "approved" {
			t.Fatalf("denied rename damaged the original root: %+v %v", value, readErr)
		}
		if err := w.CheckCWD(root); err != nil {
			t.Fatalf("denied rename changed cwd identity: %v", err)
		}
		t.Logf("native root handle prevented replacement: %v", err)
		return
	}
	if err := os.Mkdir(root, 0700); err != nil {
		t.Fatal(err)
	}
	windowsReadFile(t, filepath.Join(root, "same.txt"), "unapproved replacement")
	if value, err := w.Read(t.Context(), "same.txt"); err == nil || value.Text != "" {
		t.Fatalf("replacement root read: %+v %v", value, err)
	}
	if listed, err := w.List(t.Context(), ""); err == nil || len(listed.Entries) != 0 {
		t.Fatalf("replacement root listed: %+v %v", listed, err)
	}
	if found, err := w.Search(t.Context(), "", "unapproved"); err == nil || len(found.Matches) != 0 {
		t.Fatalf("replacement root searched: %+v %v", found, err)
	}
	if err := w.CheckCWD(root); err == nil {
		t.Fatal("replacement root admitted as session cwd")
	}
}

func TestWindowsWorkspaceSearchRootReplacement(t *testing.T) {
	w, root := windowsReadWorkspace(t)
	windowsReadFile(t, filepath.Join(root, "same.txt"), "approved")
	replaced := false
	w.beforeSearchRead = func() {
		w.beforeSearchRead = nil
		if err := os.Rename(root, root+"-original"); err != nil {
			if !errors.Is(err, windowsReadSharingViolation) && !errors.Is(err, syscall.ERROR_ACCESS_DENIED) {
				t.Fatalf("unexpected root rename failure: %v", err)
			}
			t.Logf("native root handle prevented replacement during search: %v", err)
			return
		}
		replaced = true
		if err := os.Mkdir(root, 0700); err != nil {
			t.Fatal(err)
		}
		windowsReadFile(t, filepath.Join(root, "same.txt"), "unapproved replacement")
	}
	found, err := w.Search(t.Context(), "", "unapproved")
	if len(found.Matches) != 0 || (replaced && err == nil) || (!replaced && err != nil) {
		t.Fatalf("search read replacement root between enumeration and read: %+v %v", found, err)
	}
	if w.beforeSearchRead != nil {
		t.Fatal("search never reached the replacement boundary")
	}
	if !replaced {
		value, readErr := w.Read(t.Context(), "same.txt")
		if readErr != nil || value.Text != "approved" {
			t.Fatalf("denied search-boundary rename damaged the original root: %+v %v", value, readErr)
		}
		if err := w.CheckCWD(root); err != nil {
			t.Fatalf("denied search-boundary rename changed cwd identity: %v", err)
		}
	}
}
