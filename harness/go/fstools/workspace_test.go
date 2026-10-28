package fstools

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func workspace(t *testing.T) (*Workspace, string) {
	t.Helper()
	p := t.TempDir()
	w, e := OpenWorkspace(p)
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { w.Close() })
	return w, p
}
func TestWorkspaceReadWriteEditListSearch(t *testing.T) {
	w, p := workspace(t)
	ctx := t.Context()
	r, e := w.Write(ctx, "sample.txt", "first\nneedle\n")
	if e != nil || r.State != "applied" {
		t.Fatalf("write %+v %v", r, e)
	}
	value, e := w.Read(ctx, "sample.txt")
	if e != nil || value.Text != "first\nneedle\n" {
		t.Fatalf("read %+v %v", value, e)
	}
	r, e = w.Edit(ctx, "sample.txt", "needle", "updated", false)
	if e != nil || r.State != "applied" {
		t.Fatalf("edit %+v %v", r, e)
	}
	listed, e := w.List(ctx, "")
	if e != nil || len(listed.Entries) != 1 || listed.Entries[0].Name != "sample.txt" {
		t.Fatalf("list %+v %v", listed, e)
	}
	found, e := w.Search(ctx, "", "updated")
	if e != nil || len(found.Matches) != 1 || found.Matches[0].Line != 2 {
		t.Fatalf("search %+v %v", found, e)
	}
	data, e := os.ReadFile(filepath.Join(p, "sample.txt"))
	if e != nil || string(data) != "first\nupdated\n" {
		t.Fatalf("disk %q %v", data, e)
	}
}
func TestWorkspaceRejectsTraversalSymlinksAndBinary(t *testing.T) {
	w, p := workspace(t)
	outside := t.TempDir()
	os.WriteFile(filepath.Join(outside, "secret"), []byte("outside"), 0600)
	os.Symlink(outside, filepath.Join(p, "link"))
	for _, name := range []string{"../secret", filepath.Join(outside, "secret"), "link/secret", "a/../../secret", "./secret", "a\\secret"} {
		if _, e := w.Read(t.Context(), name); e == nil {
			t.Errorf("read allowed %q", name)
		}
		if _, e := w.Write(t.Context(), name, "bad"); e == nil {
			t.Errorf("write allowed %q", name)
		}
	}
	data, _ := os.ReadFile(filepath.Join(outside, "secret"))
	if string(data) != "outside" {
		t.Fatal("outside changed")
	}
	if _, e := w.Write(t.Context(), "binary", "a\x00b"); e == nil {
		t.Fatal("binary accepted")
	}
}
func TestWorkspaceCapsCancellationAndAmbiguousEdit(t *testing.T) {
	w, p := workspace(t)
	if _, e := w.Write(t.Context(), "huge", strings.Repeat("x", MaxFileBytes+1)); e == nil {
		t.Fatal("oversize accepted")
	}
	if _, e := os.Stat(filepath.Join(p, "huge")); !os.IsNotExist(e) {
		t.Fatal("oversize write created file")
	}
	w.Write(t.Context(), "dupe", "same same")
	if _, e := w.Edit(t.Context(), "dupe", "same", "other", false); e == nil {
		t.Fatal("ambiguous edit accepted")
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, e := w.Write(ctx, "cancelled", "x"); e == nil {
		t.Fatal("cancelled write accepted")
	}
	if _, e := os.Stat(filepath.Join(p, "cancelled")); !os.IsNotExist(e) {
		t.Fatal("cancelled write created file")
	}
	w.Write(t.Context(), "result", strings.Repeat("x", 32000))
	value, _ := w.Read(t.Context(), "result")
	out, e := FormatRead(value, 1, 200)
	if e != nil || !out["truncated"].(bool) {
		t.Fatalf("read cap %v %v", out, e)
	}
}
func TestWorkspaceRejectsReplacedRootAndWrongSessionCWD(t *testing.T) {
	w, p := workspace(t)
	if w.CheckCWD(t.TempDir()) == nil {
		t.Fatal("unconfigured cwd accepted")
	}
	moved := p + "-moved"
	if e := os.Rename(p, moved); e != nil {
		t.Fatal(e)
	}
	defer os.RemoveAll(moved)
	os.Mkdir(p, 0700)
	if _, e := w.Write(t.Context(), "file", "x"); e == nil {
		t.Fatal("replaced root accepted")
	}
}

func TestSearchRejectsReplacedRootBeforeReading(t *testing.T) {
	w, p := workspace(t)
	os.WriteFile(filepath.Join(p, "file"), []byte("original"), 0600)
	moved := p + "-moved"
	defer os.RemoveAll(moved)
	w.beforeSearchRead = func() {
		w.beforeSearchRead = nil
		if e := os.Rename(p, moved); e != nil {
			t.Fatal(e)
		}
		os.Mkdir(p, 0700)
		os.WriteFile(filepath.Join(p, "file"), []byte("secret-new-root"), 0600)
	}
	result, e := w.Search(t.Context(), "", "secret")
	if e == nil || len(result.Matches) != 0 {
		t.Fatalf("read replaced root: %+v %v", result, e)
	}
}
func TestCreatedFileUncertainAfterParentOrRootReplacement(t *testing.T) {
	for _, parent := range []bool{false, true} {
		t.Run(map[bool]string{false: "root", true: "parent"}[parent], func(t *testing.T) {
			w, p := workspace(t)
			name := "file"
			old := p
			if parent {
				os.Mkdir(filepath.Join(p, "dir"), 0700)
				name = "dir/file"
				old = filepath.Join(p, "dir")
			}
			moved := old + "-moved"
			defer os.RemoveAll(moved)
			w.afterCreate = func() {
				if e := os.Rename(old, moved); e != nil {
					t.Fatal(e)
				}
				os.Mkdir(old, 0700)
			}
			receipt, e := w.Write(t.Context(), name, "approved-content")
			if e == nil || receipt.State != "unknown" {
				t.Fatalf("creation misreported: %+v %v", receipt, e)
			}
			data, e := os.ReadFile(filepath.Join(moved, "file"))
			if e != nil || string(data) != "approved-content" {
				t.Fatalf("effect evidence %q %v", data, e)
			}
		})
	}
}
