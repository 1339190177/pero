package fstools

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func scopedFixture(t *testing.T) (string, *ScopedTextFiles, ScopedText) {
	t.Helper()
	if runtime.GOOS != "linux" && runtime.GOOS != "darwin" {
		t.Skip("descriptor-rooted Unix scoped replacement is unavailable")
	}
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err = os.Mkdir(filepath.Join(root, "src"), 0700); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(root, "src", "one.txt"), []byte("before\n"), 0640); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(root, "other.txt"), []byte("untouched\n"), 0600); err != nil {
		t.Fatal(err)
	}
	scope, err := OpenScopedTextFiles(root, []string{"src/one.txt"}, 1024)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = scope.Close() })
	before, err := scope.Read(t.Context(), "src/one.txt")
	if err != nil {
		t.Fatal(err)
	}
	return root, scope, before
}

func allowScoped(context.Context) error { return nil }

func TestScopedTextReplacementReadsBackExactBytesAndPreservesMode(t *testing.T) {
	root, scope, before := scopedFixture(t)
	calls := 0
	receipt, err := scope.Replace(t.Context(), before.Path, before.Digest, []byte("after\r\n"), func(context.Context) error { calls++; return nil })
	if err != nil || receipt.State != "applied" || receipt.BeforeDigest != before.Digest || calls != 2 {
		t.Fatalf("replacement receipt: %+v checks=%d err=%v", receipt, calls, err)
	}
	readback, err := scope.Read(t.Context(), before.Path)
	if err != nil || readback.Text != "after\r\n" || readback.Digest != receipt.AfterDigest {
		t.Fatalf("readback: %+v %v", readback, err)
	}
	info, err := os.Stat(filepath.Join(root, "src", "one.txt"))
	if err != nil || info.Mode().Perm() != 0640 {
		t.Fatalf("mode: %v %v", info, err)
	}
	entries, _ := os.ReadDir(filepath.Join(root, "src"))
	if len(entries) != 1 {
		t.Fatalf("staging leaked: %+v", entries)
	}
	untouched, _ := os.ReadFile(filepath.Join(root, "other.txt"))
	if string(untouched) != "untouched\n" {
		t.Fatal("unselected file changed")
	}
}

func TestScopedTextReplacementRefusesUndeclaredStaleAndUnauthorised(t *testing.T) {
	for _, name := range []string{"undeclared", "wrong digest", "no authority", "revoked before staging", "revoked before commit", "changed while authorizing", "oversized", "binary", "unchanged"} {
		t.Run(name, func(t *testing.T) {
			root, scope, before := scopedFixture(t)
			path, digest, after, checks := before.Path, before.Digest, []byte("after\n"), 0
			authorize := allowScoped
			switch name {
			case "undeclared":
				path = "other.txt"
			case "wrong digest":
				digest = scopedTextDigest([]byte("wrong"))
			case "no authority":
				authorize = nil
			case "revoked before staging":
				authorize = func(context.Context) error { return errors.New("revoked") }
			case "revoked before commit":
				authorize = func(context.Context) error {
					checks++
					if checks == 2 {
						return errors.New("revoked")
					}
					return nil
				}
			case "changed while authorizing":
				authorize = func(context.Context) error {
					checks++
					if checks == 2 {
						return os.WriteFile(filepath.Join(root, "src", "one.txt"), []byte("external\n"), 0640)
					}
					return nil
				}
			case "oversized":
				after = []byte(strings.Repeat("x", 1025))
			case "binary":
				after = []byte{0, 1}
			case "unchanged":
				after = []byte(before.Text)
			}
			receipt, err := scope.Replace(t.Context(), path, digest, after, authorize)
			if err == nil || receipt.State != "not-applied" {
				t.Fatalf("unsafe write: %+v %v", receipt, err)
			}
			actual, _ := os.ReadFile(filepath.Join(root, "src", "one.txt"))
			want := "before\n"
			if name == "changed while authorizing" {
				want = "external\n"
			}
			if string(actual) != want {
				t.Fatalf("preexisting work changed: %q", actual)
			}
			entries, _ := os.ReadDir(filepath.Join(root, "src"))
			if len(entries) != 1 {
				t.Fatal("staging not cleaned after refusal")
			}
		})
	}
}

func TestScopedTextManifestRejectsTraversalSymlinksAndNonText(t *testing.T) {
	root, _, _ := scopedFixture(t)
	if err := os.Symlink("src", filepath.Join(root, "link")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("one.txt", filepath.Join(root, "src", "link.txt")); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "binary"), []byte{0, 255}, 0600); err != nil {
		t.Fatal(err)
	}
	for _, paths := range [][]string{nil, {"../outside"}, {"/outside"}, {"src/../other.txt"}, {"src//one.txt"}, {"src\\one.txt"}, {"src/one.txt", "src/one.txt"}, {"link/one.txt"}, {"src/link.txt"}, {"missing"}, {"src"}, {"binary"}} {
		scope, err := OpenScopedTextFiles(root, paths, 1024)
		if err == nil {
			_ = scope.Close()
			t.Errorf("invalid manifest accepted: %q", paths)
		}
	}
}

func TestScopedTextParentRedirectionCannotWriteAnUnselectedDirectory(t *testing.T) {
	root, scope, before := scopedFixture(t)
	if err := os.Mkdir(filepath.Join(root, "other"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "other", "one.txt"), []byte(before.Text), 0600); err != nil {
		t.Fatal(err)
	}
	calls := 0
	receipt, err := scope.Replace(t.Context(), before.Path, before.Digest, []byte("after\n"), func(context.Context) error {
		calls++
		if calls == 2 {
			if err := os.Rename(filepath.Join(root, "src"), filepath.Join(root, "old")); err != nil {
				return err
			}
			return os.Symlink("other", filepath.Join(root, "src"))
		}
		return nil
	})
	if err == nil || receipt.State != "not-applied" {
		t.Fatalf("redirected write: %+v %v", receipt, err)
	}
	for _, dir := range []string{"old", "other"} {
		actual, _ := os.ReadFile(filepath.Join(root, dir, "one.txt"))
		if string(actual) != before.Text {
			t.Fatalf("%s was mutated: %q", dir, actual)
		}
	}
}

func TestScopedTextCommittedEffectWithLostReadbackRemainsUnknown(t *testing.T) {
	_, scope, before := scopedFixture(t)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	scope.afterCommit = cancel
	receipt, err := scope.Replace(ctx, before.Path, before.Digest, []byte("after\n"), allowScoped)
	if err == nil || receipt.State != "unknown" {
		t.Fatalf("lost readback presented as safe retry/success: %+v %v", receipt, err)
	}
	readback, err := scope.Read(t.Context(), before.Path)
	if err != nil || readback.Digest != receipt.AfterDigest {
		t.Fatalf("same-intent reconciliation cannot observe committed bytes: %+v %v", readback, err)
	}
	// The helper has no retry loop; the Host reconciles this observed digest
	// against its durable intent instead of executing the mutation again.
}

func TestScopedTextOwnerCloseAndRootReplacementFailClosed(t *testing.T) {
	root, scope, before := scopedFixture(t)
	if err := os.Rename(root, root+"-moved"); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(root + "-moved") })
	if err := os.Mkdir(root, 0700); err != nil {
		t.Fatal(err)
	}
	if _, err := scope.Read(t.Context(), before.Path); err == nil {
		t.Fatal("replacement root inherited authority")
	}
	if err := scope.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := scope.Read(t.Context(), before.Path); err == nil {
		t.Fatal("closed owner still read")
	}
}
