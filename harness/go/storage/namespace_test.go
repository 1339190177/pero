package storage

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func proveTestNamespace(t *testing.T, root string) *NamespaceProof {
	t.Helper()
	p, err := ProveJSONNamespace(JSONNamespace{Root: root, Unit: "session_projcache", Layout: LayoutPerRecord, Tables: []string{"sessions"}})
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func TestNamespaceProofUsesOpenedUnitAndPermitsNormalCreation(t *testing.T) {
	root := t.TempDir()
	_, facility := testFacility(t, root)
	domain, err := facility.Open(context.Background(), counterSpec(1, LayoutPerRecord))
	if err != nil {
		t.Fatal(err)
	}
	proof, err := domain.NamespaceProof()
	if err != nil {
		t.Fatal(err)
	}
	got := proof.Description()
	if got.Root != root || got.Unit != "counter_domain" || got.Layout != LayoutPerRecord || len(got.Tables) != 1 || got.Tables[0] != "items" {
		t.Fatalf("actual unit proof: %#v", got)
	}
	got.Tables[0] = "foreign"
	if proof.Description().Tables[0] != "items" {
		t.Fatal("description aliases proof")
	}
	table, err := domain.Table("items")
	if err != nil {
		t.Fatal(err)
	}
	if err = table.Put(context.Background(), "one", []byte(`{"n":1}`)); err != nil {
		t.Fatal(err)
	}
	if err = proof.Validate(); err != nil {
		t.Fatalf("normal writes changed namespace: %v", err)
	}
	if err = domain.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err = domain.NamespaceProof(); err == nil {
		t.Fatal("closed unit issued proof")
	}
	if err = proof.Validate(); err != nil {
		t.Fatalf("retained proof after close: %v", err)
	}
}

func TestNamespaceProofRejectsDirectAndSymlinkAliasesWithoutCreatingCache(t *testing.T) {
	root := t.TempDir()
	a := proveTestNamespace(t, root)
	if err := a.RequireDisjoint(proveTestNamespace(t, root)); err == nil {
		t.Fatal("same namespace admitted")
	}
	alias := filepath.Join(t.TempDir(), "alias")
	if err := os.Symlink(root, alias); err != nil {
		t.Fatal(err)
	}
	if err := a.RequireDisjoint(proveTestNamespace(t, alias)); err == nil {
		t.Fatal("symlink alias admitted")
	}
	other := t.TempDir()
	if err := a.RequireDisjoint(proveTestNamespace(t, other)); err != nil {
		t.Fatalf("distinct namespace: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "session_projcache")); !os.IsNotExist(err) {
		t.Fatalf("proof created cache: %v", err)
	}
}

func TestNamespaceProofRejectsNestedTableAliasAndChangedRouting(t *testing.T) {
	left, right := t.TempDir(), t.TempDir()
	target := filepath.Join(left, "session_projcache", "sessions")
	if err := os.MkdirAll(target, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(right, "session_projcache"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, filepath.Join(right, "session_projcache", "sessions")); err != nil {
		t.Fatal(err)
	}
	if err := proveTestNamespace(t, left).RequireDisjoint(proveTestNamespace(t, right)); err == nil {
		t.Fatal("table alias admitted")
	}
	stable := proveTestNamespace(t, right)
	if err := os.Remove(filepath.Join(right, "session_projcache", "sessions")); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(right, "session_projcache", "sessions"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := stable.Validate(); err == nil {
		t.Fatal("changed table routing retained proof")
	}
}

func TestNamespaceProofRejectsReplacedRootAndUnknownBackend(t *testing.T) {
	parent := t.TempDir()
	root := filepath.Join(parent, "storage")
	if err := os.Mkdir(root, 0700); err != nil {
		t.Fatal(err)
	}
	proof := proveTestNamespace(t, root)
	if err := os.Rename(root, root+"-old"); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(root, 0700); err != nil {
		t.Fatal(err)
	}
	if err := proof.Validate(); err == nil {
		t.Fatal("root replacement retained proof")
	}
	if err := (*NamespaceProof)(nil).RequireDisjoint(proof); err == nil {
		t.Fatal("unknown proof admitted")
	}
	domain := &Domain{}
	if _, err := domain.NamespaceProof(); err == nil {
		t.Fatal("unknown backend issued proof")
	}
}

func TestNamespaceProofIncludesLegacyEnvelopeAliases(t *testing.T) {
	left, right := t.TempDir(), t.TempDir()
	source := filepath.Join(left, "session_projcache.json")
	if err := os.WriteFile(source, []byte("{}"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Link(source, filepath.Join(right, "session_projcache.json")); err != nil {
		t.Fatal(err)
	}
	if err := proveTestNamespace(t, left).RequireDisjoint(proveTestNamespace(t, right)); err == nil {
		t.Fatal("hard-linked legacy envelope admitted")
	}
}

func TestNamespaceProofResamplesLegacyInodesBeforeCheckingDisjointness(t *testing.T) {
	left, right := t.TempDir(), t.TempDir()
	a, b := filepath.Join(left, "session_projcache.json"), filepath.Join(right, "session_projcache.json")
	for _, path := range []string{a, b} {
		if err := os.WriteFile(path, []byte("{}"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	first, second := proveTestNamespace(t, left), proveTestNamespace(t, right)
	if err := first.RequireDisjoint(second); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(b); err != nil {
		t.Fatal(err)
	}
	if err := os.Link(a, b); err != nil {
		t.Fatal(err)
	}
	if err := first.RequireDisjoint(second); err == nil {
		t.Fatal("new legacy hard-link alias escaped retained proofs")
	}
}

func TestNamespaceProofRejectsSymlinkAddedBelowPreviouslyAbsentFootprint(t *testing.T) {
	left, right := t.TempDir(), t.TempDir()
	first, second := proveTestNamespace(t, left), proveTestNamespace(t, right)
	if err := os.MkdirAll(filepath.Join(left, "session_projcache", "sessions"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(right, "session_projcache"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(left, "session_projcache", "sessions"), filepath.Join(right, "session_projcache", "sessions")); err != nil {
		t.Fatal(err)
	}
	if err := first.RequireDisjoint(second); err == nil {
		t.Fatal("new descendant symlink alias escaped retained proofs")
	}
}

func TestNamespaceProofRejectsExistingAndNewPerRecordAliases(t *testing.T) {
	for _, link := range []string{"hardlink", "symlink", "legacy-hardlink"} {
		for _, existing := range []bool{false, true} {
			name := link + "/new"
			if existing {
				name = link + "/existing"
			}
			t.Run(name, func(t *testing.T) {
				left, right := t.TempDir(), t.TempDir()
				leftTable := filepath.Join(left, "session_projcache", "sessions")
				rightTable := filepath.Join(right, "session_projcache", "sessions")
				for _, path := range []string{leftTable, rightTable} {
					if err := os.MkdirAll(path, 0700); err != nil {
						t.Fatal(err)
					}
				}
				source := filepath.Join(leftTable, "source.json")
				if link == "legacy-hardlink" {
					source = filepath.Join(left, "session_projcache.json")
				}
				if err := os.WriteFile(source, []byte("{}"), 0600); err != nil {
					t.Fatal(err)
				}
				var a, b *NamespaceProof
				if !existing {
					a, b = proveTestNamespace(t, left), proveTestNamespace(t, right)
					if err := a.RequireDisjoint(b); err != nil {
						t.Fatal(err)
					}
				}
				target := filepath.Join(rightTable, "different-session-id.json")
				var err error
				if link == "symlink" {
					err = os.Symlink(source, target)
				} else {
					err = os.Link(source, target)
				}
				if err != nil {
					t.Fatal(err)
				}
				if existing {
					a = proveTestNamespace(t, left)
					b, err = ProveJSONNamespace(JSONNamespace{Root: right, Unit: "session_projcache", Layout: LayoutPerRecord, Tables: []string{"sessions"}})
					if err != nil {
						if link == "symlink" {
							return
						}
						t.Fatal(err)
					}
				}
				if err = a.RequireDisjoint(b); err == nil {
					t.Fatal("per-record physical alias admitted")
				}
			})
		}
	}
}

func TestNamespaceProofPermitsIndependentAtomicRecordReplacement(t *testing.T) {
	left, right := t.TempDir(), t.TempDir()
	a, b := proveTestNamespace(t, left), proveTestNamespace(t, right)
	for _, root := range []string{left, right} {
		table := filepath.Join(root, "session_projcache", "sessions")
		if err := os.MkdirAll(table, 0700); err != nil {
			t.Fatal(err)
		}
		path := filepath.Join(table, "same-id.json")
		if err := os.WriteFile(path, []byte("{}"), 0600); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path+".tmp", []byte("{\"new\":true}"), 0600); err != nil {
			t.Fatal(err)
		}
		if err := os.Rename(path+".tmp", path); err != nil {
			t.Fatal(err)
		}
	}
	if err := a.RequireSame(proveTestNamespace(t, left)); err != nil {
		t.Fatal(err)
	}
	if err := a.RequireDisjoint(b); err != nil {
		t.Fatal(err)
	}
}

