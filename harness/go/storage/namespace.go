package storage

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
)

// JSONNamespace describes the footprint of a trusted JSON KV implementation.
// It is an internal composition handshake, not a profile option or permission
// to choose a storage path. A peer must obtain it from its actual selected
// backend before opening/loading the second cache. Native-first LoadAll may
// already have observed historical data; this proof cannot undo that read.
type JSONNamespace struct {
	Root   string   `json:"root"`
	Unit   string   `json:"unit"`
	Layout Layout   `json:"layout"`
	Tables []string `json:"tables"`
}

// KVNamespaceProver is optional. Custom backends that cannot identify their
// physical footprint must not participate in paired cache replacement.
type KVNamespaceProver interface {
	NamespaceProof() (*NamespaceProof, error)
}

// NamespaceProof binds the actual filesystem locations, including existing
// ancestor identities for paths which have not yet been created. It remains
// valid only while routing and directory topology are held stable by the Host.
// It is not a filesystem lock or a defense against concurrent external renames.
type NamespaceProof struct {
	description JSONNamespace
	root        namespaceLocation
	footprints  []namespaceLocation
	documents   []os.FileInfo // Fresh physical records; never part of stable routing equality.
}

type namespaceAnchor struct {
	info      os.FileInfo
	remaining string
}
type namespaceLocation struct {
	anchors       []namespaceAnchor
	directory     string
	directoryInfo os.FileInfo
}

// ProveJSONNamespace is for an authenticated, trusted backend handshake only.
// It does no mkdir, open/load, migration, or write. Re-checking both proofs just
// before mounting the peer prevents an aliased second cache from loading data.
func ProveJSONNamespace(description JSONNamespace) (*NamespaceProof, error) {
	if !filepath.IsAbs(description.Root) {
		return nil, errors.New("storage: namespace root must be absolute")
	}
	if description.Layout != LayoutSingle && description.Layout != LayoutPerRecord {
		return nil, errors.New("storage: unsupported namespace layout")
	}
	if err := validateDescriptor(KVUnitDescriptor{Name: description.Unit, Layout: description.Layout, Tables: description.Tables}); err != nil {
		return nil, err
	}
	description.Root = filepath.Clean(description.Root)
	description.Tables = slices.Clone(description.Tables)
	sort.Strings(description.Tables)
	rootInfo, err := os.Stat(description.Root)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("storage: namespace root: %w", err)
	}
	if err == nil && !rootInfo.IsDir() {
		return nil, errors.New("storage: namespace root is not a directory")
	}
	root, err := proveNamespaceLocation(description.Root)
	if err != nil {
		return nil, err
	}
	proof := &NamespaceProof{description: description, root: root}
	// The sibling legacy envelope is part of per-record bootstrap, too.
	paths := []string{filepath.Join(description.Root, description.Unit+".json")}
	if description.Layout == LayoutPerRecord {
		paths = append(paths, filepath.Join(description.Root, description.Unit))
		for _, table := range description.Tables {
			paths = append(paths, filepath.Join(description.Root, description.Unit, table))
		}
	}
	for _, path := range paths {
		location, err := proveNamespaceLocation(path)
		if err != nil {
			return nil, err
		}
		proof.footprints = append(proof.footprints, location)
	}
	if err := proof.sampleDocument(filepath.Join(description.Root, description.Unit+".json"), false); err != nil {
		return nil, err
	}
	if description.Layout == LayoutPerRecord {
		// Conservatively include global.json even for a table-only unit. Both
		// implementations follow record-file links when reading; distinct table
		// directories alone cannot establish a disjoint physical read footprint.
		if err := proof.sampleDocument(filepath.Join(description.Root, description.Unit, "global.json"), true); err != nil {
			return nil, err
		}
		for _, table := range description.Tables {
			directory := filepath.Join(description.Root, description.Unit, table)
			entries, err := os.ReadDir(directory)
			if errors.Is(err, os.ErrNotExist) {
				continue
			}
			if err != nil {
				return nil, err
			}
			for _, entry := range entries {
				if filepath.Ext(entry.Name()) != ".json" {
					continue
				}
				if err := proof.sampleDocument(filepath.Join(directory, entry.Name()), true); err != nil {
					return nil, err
				}
			}
		}
	}
	return proof, nil
}

func (p *NamespaceProof) sampleDocument(path string, rejectSymlink bool) error {
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		if rejectSymlink {
			return errors.New("storage: cache namespace contains a symlink record")
		}
		info, err = os.Stat(path)
		if err != nil {
			return err
		}
	}
	if !info.Mode().IsRegular() {
		return errors.New("storage: cache namespace contains a non-regular document")
	}
	p.documents = append(p.documents, info)
	return nil
}

func proveNamespaceLocation(path string) (namespaceLocation, error) {
	existing := path
	var suffix []string
	for {
		_, err := os.Lstat(existing)
		if err == nil {
			break
		}
		if !errors.Is(err, os.ErrNotExist) {
			return namespaceLocation{}, err
		}
		parent := filepath.Dir(existing)
		if parent == existing {
			return namespaceLocation{}, errors.New("storage: no existing namespace ancestor")
		}
		suffix = append([]string{filepath.Base(existing)}, suffix...)
		existing = parent
	}
	resolved, err := filepath.EvalSymlinks(existing)
	if err != nil {
		return namespaceLocation{}, fmt.Errorf("storage: resolve namespace: %w", err)
	}
	remaining := filepath.Join(suffix...)
	if remaining == "" {
		remaining = "."
	}
	var result namespaceLocation
	for {
		info, err := os.Stat(resolved)
		if err != nil {
			return namespaceLocation{}, err
		}
		if remaining != "." && !info.IsDir() {
			return namespaceLocation{}, errors.New("storage: namespace ancestor is not a directory")
		}
		if result.directoryInfo == nil && info.IsDir() {
			result.directory, result.directoryInfo = resolved, info
		}
		result.anchors = append(result.anchors, namespaceAnchor{info: info, remaining: remaining})
		parent := filepath.Dir(resolved)
		if parent == resolved {
			break
		}
		remaining = filepath.Join(filepath.Base(resolved), remaining)
		resolved = parent
	}
	return result, nil
}

func (p namespaceLocation) validateDirectory() error {
	current, err := os.Stat(p.directory)
	if err != nil {
		return err
	}
	if !os.SameFile(p.directoryInfo, current) {
		return errors.New("storage: namespace directory identity changed")
	}
	return nil
}

func locationsEqual(a, b namespaceLocation) bool {
	for _, x := range a.anchors {
		for _, y := range b.anchors {
			if x.remaining == y.remaining && os.SameFile(x.info, y.info) {
				return true
			}
		}
	}
	return false
}

func locationOverlaps(a, b namespaceLocation) bool {
	for _, x := range a.anchors {
		for _, y := range b.anchors {
			if os.SameFile(x.info, y.info) && (pathContains(x.remaining, y.remaining) || pathContains(y.remaining, x.remaining)) {
				return true
			}
		}
	}
	return false
}
func pathContains(parent, child string) bool {
	return parent == "." || parent == child || strings.HasPrefix(child, parent+string(filepath.Separator))
}

func (p *NamespaceProof) Description() JSONNamespace {
	if p == nil {
		return JSONNamespace{}
	}
	d := p.description
	d.Tables = slices.Clone(d.Tables)
	return d
}

// Validate permits ordinary creation of previously absent cache directories,
// but rejects a replacement root, changed symlink target, or unknown proof.
func (p *NamespaceProof) Validate() error {
	if p == nil {
		return errors.New("storage: namespace proof unavailable")
	}
	if err := p.root.validateDirectory(); err != nil {
		return err
	}
	for _, location := range p.footprints {
		if err := location.validateDirectory(); err != nil {
			return err
		}
	}
	current, err := ProveJSONNamespace(p.description)
	if err != nil {
		return err
	}
	if !locationsEqual(p.root, current.root) {
		return errors.New("storage: namespace root identity changed")
	}
	for i, location := range p.footprints {
		if !locationsEqual(location, current.footprints[i]) {
			return errors.New("storage: namespace footprint changed")
		}
	}
	return nil
}

// RequireDisjoint fails closed on unknown, changed, or overlapping footprints.
// Ancestor file identities detect aliases even where canonical strings differ
// (for example a bind mount). No DomainFacility pointer equality is assumed.
func (p *NamespaceProof) RequireDisjoint(other *NamespaceProof) error {
	if err := p.Validate(); err != nil {
		return err
	}
	if err := other.Validate(); err != nil {
		return err
	}
	// Compare newly sampled footprints. Atomic publication may replace an
	// existing envelope inode while its directory and pathname stay stable;
	// retained samples alone would miss a newly introduced hard-link alias.
	current, err := ProveJSONNamespace(p.description)
	if err != nil {
		return err
	}
	peer, err := ProveJSONNamespace(other.description)
	if err != nil {
		return err
	}
	// Compare records against every peer document, including its legacy
	// envelope. Keep exact SameFile checks rather than inode guesses or file
	// contents; normal atomic record replacement remains valid on re-sampling.
	for _, a := range current.documents {
		for _, b := range peer.documents {
			if os.SameFile(a, b) {
				return errors.New("storage: cache namespace documents alias")
			}
		}
	}
	for _, a := range current.footprints {
		for _, b := range peer.footprints {
			if locationOverlaps(a, b) {
				return errors.New("storage: cache namespaces alias")
			}
		}
	}
	return nil
}

func (d *Domain) NamespaceProof() (*NamespaceProof, error) {
	if d == nil {
		return nil, errors.New("storage: namespace domain unavailable")
	}
	d.stateMu.RLock()
	defer d.stateMu.RUnlock()
	if d.disposing || d.closed {
		return nil, errors.New("storage: namespace domain is closing")
	}
	prover, ok := d.unit.(KVNamespaceProver)
	if !ok {
		return nil, errors.New("storage: backend cannot prove its physical namespace")
	}
	return prover.NamespaceProof()
}
func (u *perRecordUnit) NamespaceProof() (*NamespaceProof, error) {
	u.mu.Lock()
	defer u.mu.Unlock()
	if u.closed {
		return nil, errors.New("storage: namespace unit is closed")
	}
	return ProveJSONNamespace(JSONNamespace{Root: filepath.Dir(u.dir), Unit: u.descriptor.Name, Layout: LayoutPerRecord, Tables: u.descriptor.Tables})
}
func (u *singleUnit) NamespaceProof() (*NamespaceProof, error) {
	u.mu.Lock()
	defer u.mu.Unlock()
	if u.closed {
		return nil, errors.New("storage: namespace unit is closed")
	}
	return ProveJSONNamespace(JSONNamespace{Root: filepath.Dir(u.path), Unit: u.descriptor.Name, Layout: LayoutSingle, Tables: u.descriptor.Tables})
}

// RequireSame binds a replacement to the exact retained footprint.
func (p *NamespaceProof) RequireSame(other *NamespaceProof) error {
	if err := p.Validate(); err != nil {
		return err
	}
	if err := other.Validate(); err != nil {
		return err
	}
	if p.description.Unit != other.description.Unit || p.description.Layout != other.description.Layout || !slices.Equal(p.description.Tables, other.description.Tables) || !locationsEqual(p.root, other.root) || len(p.footprints) != len(other.footprints) {
		return errors.New("storage: replacement namespace differs")
	}
	for i, location := range p.footprints {
		if !locationsEqual(location, other.footprints[i]) {
			return errors.New("storage: replacement footprint differs")
		}
	}
	return nil
}
