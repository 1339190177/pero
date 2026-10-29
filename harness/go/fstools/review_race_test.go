package fstools

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

type replaceOnErr struct {
	context.Context
	once func()
}

func (c *replaceOnErr) Err() error {
	if c.once != nil {
		f := c.once
		c.once = nil
		f()
	}
	return nil
}
func TestReviewSearchDoesNotReadReplacedRoot(t *testing.T) {
	requireNativeWorkspace(t)
	root := filepath.Join(t.TempDir(), "root")
	os.Mkdir(root, 0700)
	os.WriteFile(filepath.Join(root, "same"), []byte("old allowed"), 0600)
	w, e := OpenWorkspace(root)
	if e != nil {
		t.Fatal(e)
	}
	defer w.Close()
	c := &replaceOnErr{Context: context.Background(), once: func() {
		if e := os.Rename(root, root+"-old"); e != nil {
			t.Fatal(e)
		}
		os.Mkdir(root, 0700)
		os.WriteFile(filepath.Join(root, "same"), []byte("new confidential"), 0600)
	}}
	got, e := w.Search(c, "", "confidential")
	if e == nil && len(got.Matches) > 0 {
		t.Fatalf("UNAUTHORIZED REPLACEMENT ROOT READ: %+v", got)
	}
}
func TestReviewCreateDoesNotClaimAppliedOnReplacedRoot(t *testing.T) {
	requireNativeWorkspace(t)
	root := filepath.Join(t.TempDir(), "root")
	os.Mkdir(root, 0700)
	w, e := OpenWorkspace(root)
	if e != nil {
		t.Fatal(e)
	}
	defer w.Close()
	c := &replaceOnErr{Context: context.Background(), once: func() { os.Rename(root, root+"-old"); os.Mkdir(root, 0700) }}
	got, e := w.Write(c, "made", "content")
	if e == nil && got.State == "applied" {
		t.Fatalf("FALSE APPLIED AFTER ROOT REPLACEMENT: %+v", got)
	}
}
