package config

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"sort"
	"sync"
	"time"

	point "github.com/phodal/pero/harness/go/runtime"
	"gopkg.in/yaml.v3"
)

const (
	defaultCredentialWatchInterval = 100 * time.Millisecond
	defaultCredentialLockWait      = 30 * time.Second
	defaultCredentialLockPoll      = 25 * time.Millisecond
)

type FileCredentialOptions struct {
	Path          string
	DisableWatch  bool
	WatchInterval time.Duration
	LockWait      time.Duration
	LockPoll      time.Duration
	LookupEnv     func(string) (string, bool)
	LaunchEnv     *LaunchEnvironmentSnapshot
	Logger        *slog.Logger
}

type credentialDocument struct {
	refs        map[CredentialRef]string
	records     map[CredentialKey]CredentialRecord
	recordOrder []CredentialKey
	source      []byte
	ignoredKeys []string
}

// FileCredentialProvider implements the DSH v1 credential document. Process
// environment values are fallbacks below writable managed refs; records
// remain owner-defined opaque values in the same atomic YAML document.
type FileCredentialProvider struct {
	filename      string
	disableWatch  bool
	watchInterval time.Duration
	lockWait      time.Duration
	lockPoll      time.Duration
	lookupEnv     func(string) (string, bool)
	launchEnv     *LaunchEnvironmentSnapshot
	logger        *slog.Logger
	store         *CredentialStore

	mu              sync.Mutex
	watchStarted    bool
	disposed        bool
	warnedSource    [sha256.Size]byte
	hasWarnedSource bool
}

func NewFileCredentialProvider(options FileCredentialOptions) (*FileCredentialProvider, error) {
	if options.Path == "" {
		return nil, errors.New("harness: credential file path is required")
	}
	filename, err := filepath.Abs(options.Path)
	if err != nil {
		return nil, err
	}
	switch filepath.Ext(filename) {
	case ".yaml", ".yml":
	default:
		return nil, fmt.Errorf("harness: credential file %s must use .yaml or .yml", filename)
	}
	if options.WatchInterval == 0 {
		options.WatchInterval = defaultCredentialWatchInterval
	}
	if options.WatchInterval < 0 {
		return nil, errors.New("harness: credential watch interval must not be negative")
	}
	if options.LockWait == 0 {
		options.LockWait = defaultCredentialLockWait
	}
	if options.LockWait < 0 {
		return nil, errors.New("harness: credential lock wait must not be negative")
	}
	if options.LockPoll == 0 {
		options.LockPoll = defaultCredentialLockPoll
	}
	if options.LockPoll < time.Millisecond {
		return nil, errors.New("harness: credential lock poll must be at least one millisecond")
	}
	if options.LookupEnv == nil {
		options.LookupEnv = os.LookupEnv
	}
	if options.Logger == nil {
		options.Logger = slog.Default()
	}
	provider := &FileCredentialProvider{
		filename: filename, disableWatch: options.DisableWatch, watchInterval: options.WatchInterval,
		lockWait: options.LockWait, lockPoll: options.LockPoll, lookupEnv: options.LookupEnv, launchEnv: options.LaunchEnv, logger: options.Logger,
		store: NewCredentialStore(options.Logger),
	}
	if err := provider.migrateFlatDocument(context.Background()); err != nil {
		return nil, err
	}
	document, err := provider.loadDocument()
	if err != nil {
		return nil, err
	}
	if err := provider.applyDocument(document); err != nil {
		return nil, err
	}
	return provider, nil
}

func (p *FileCredentialProvider) Path() string {
	if p == nil {
		return ""
	}
	return p.filename
}

func (p *FileCredentialProvider) checkActive() error {
	if p == nil {
		return errors.New("harness: credential file provider is nil")
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.disposed {
		return NewProviderError("credential provider was disposed", CredentialsDisposedCode, nil)
	}
	return nil
}

func newCredentialDocument() credentialDocument {
	return credentialDocument{refs: make(map[CredentialRef]string), records: make(map[CredentialKey]CredentialRecord)}
}

func (p *FileCredentialProvider) readDocumentSource() ([]byte, bool, error) {
	info, err := os.Lstat(p.filename)
	if errors.Is(err, os.ErrNotExist) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return nil, false, fmt.Errorf("harness: credential file %s must be a regular non-symlink file", p.filename)
	}
	if runtime.GOOS != "windows" && info.Mode().Perm()&0o077 != 0 {
		return nil, false, fmt.Errorf("harness: credential file %s must have owner-only permissions (0600)", p.filename)
	}
	source, err := os.ReadFile(p.filename)
	if err != nil {
		return nil, false, err
	}
	return source, true, nil
}

func (p *FileCredentialProvider) loadDocument() (credentialDocument, error) {
	document := newCredentialDocument()
	source, found, err := p.readDocumentSource()
	if err != nil || !found {
		if err == nil {
			p.hasWarnedSource = false
		}
		return document, err
	}
	document.source = append([]byte(nil), source...)
	if len(bytes.TrimSpace(source)) == 0 {
		p.hasWarnedSource = false
		return document, nil
	}
	root, err := parseCredentialYAMLRoot(source)
	if err != nil {
		return document, NewProviderError("credential file is not a valid single YAML mapping", InvalidCredentialCode, nil)
	}
	mapping := root.Content[0]
	if len(mapping.Content) == 0 {
		p.hasWarnedSource = false
		return document, nil
	}
	seen := make(map[string]struct{}, len(mapping.Content)/2)
	hasVersion := false
	for index := 0; index < len(mapping.Content); index += 2 {
		name := mapping.Content[index]
		if name.Kind != yaml.ScalarNode || name.Tag != "!!str" {
			return document, NewProviderError("credential file contains a non-string top-level key", InvalidCredentialCode, nil)
		}
		if _, duplicate := seen[name.Value]; duplicate {
			return document, NewProviderError("credential file contains a duplicate top-level key", InvalidCredentialCode, nil)
		}
		seen[name.Value] = struct{}{}
		hasVersion = hasVersion || name.Value == "version"
	}
	if !hasVersion {
		return document, NewProviderError("credential file uses the pre-release flat layout; add version 1 and nest the existing entries under refs", InvalidCredentialCode, nil)
	}
	version := 0
	for index := 0; index < len(mapping.Content); index += 2 {
		name, value := mapping.Content[index], mapping.Content[index+1]
		switch name.Value {
		case "version":
			if err := value.Decode(&version); err != nil {
				return document, NewProviderError("credential file version is invalid", InvalidCredentialCode, nil)
			}
		case "refs":
			if err := decodeCredentialRefs(value, document.refs); err != nil {
				return document, err
			}
		case "records":
			order, err := decodeCredentialRecords(value, document.records)
			if err != nil {
				return document, err
			}
			document.recordOrder = order
		default:
			document.ignoredKeys = append(document.ignoredKeys, name.Value)
		}
	}
	if version != 1 {
		return document, NewProviderError("credential file must declare version 1", InvalidCredentialCode, nil)
	}
	fingerprint := sha256.Sum256(document.source)
	if !p.hasWarnedSource || p.warnedSource != fingerprint {
		if len(document.ignoredKeys) != 0 {
			p.logger.Warn("credential file ignores unknown top-level keys; only refs and records entries resolve", "keys", document.ignoredKeys, "path", p.filename)
		}
		p.warnedSource, p.hasWarnedSource = fingerprint, true
	}
	return document, nil
}

func parseCredentialYAMLRoot(source []byte) (*yaml.Node, error) {
	root := &yaml.Node{}
	decoder := yaml.NewDecoder(bytes.NewReader(source))
	if err := decoder.Decode(root); err != nil && !errors.Is(err, io.EOF) {
		return nil, err
	}
	if root.Kind == 0 || (root.Kind == yaml.DocumentNode && len(root.Content) == 0) {
		root.Kind = yaml.DocumentNode
		root.Content = []*yaml.Node{{Kind: yaml.MappingNode, Tag: "!!map"}}
	}
	if root.Kind != yaml.DocumentNode || len(root.Content) != 1 || root.Content[0].Kind != yaml.MappingNode {
		return nil, errors.New("document root must be a mapping")
	}
	var trailing yaml.Node
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		if err != nil {
			return nil, err
		}
		if len(trailing.Content) != 0 {
			return nil, errors.New("document must contain exactly one YAML document")
		}
	}
	return root, nil
}

// RenderFlatCredentialMigration recognizes the exact pre-versioned document
// shape accepted by early DSH builds and returns its lossless v1 envelope.
// Unrecognized input is never rewritten.
func RenderFlatCredentialMigration(source []byte) ([]byte, bool) {
	root, err := parseCredentialYAMLRoot(source)
	if err != nil {
		return nil, false
	}
	mapping := root.Content[0]
	if len(mapping.Content) == 0 {
		return nil, false
	}
	for _, line := range bytes.Split(source, []byte("\n")) {
		if bytes.HasPrefix(line, []byte("%")) || bytes.HasPrefix(line, []byte("---")) || bytes.HasPrefix(line, []byte("...")) {
			return nil, false
		}
	}
	seen := make(map[string]struct{}, len(mapping.Content)/2)
	for index := 0; index < len(mapping.Content); index += 2 {
		name, value := mapping.Content[index], mapping.Content[index+1]
		if name.Kind != yaml.ScalarNode || name.Tag != "!!str" || name.Value == "version" {
			return nil, false
		}
		if _, duplicate := seen[name.Value]; duplicate {
			return nil, false
		}
		seen[name.Value] = struct{}{}
		if _, err := ParseCredentialRef(name.Value); err != nil {
			return nil, false
		}
		if value.Kind != yaml.ScalarNode || value.Tag != "!!str" || value.Value == "" {
			return nil, false
		}
	}
	lines := bytes.Split(source, []byte("\n"))
	var body bytes.Buffer
	for index, line := range lines {
		if index > 0 {
			body.WriteByte('\n')
		}
		if len(line) > 0 {
			body.WriteString("  ")
		}
		body.Write(line)
	}
	var migrated bytes.Buffer
	migrated.WriteString("version: 1\nrefs:\n")
	migrated.Write(body.Bytes())
	if !bytes.HasSuffix(source, []byte("\n")) {
		migrated.WriteByte('\n')
	}
	return migrated.Bytes(), true
}

func (p *FileCredentialProvider) migrateFlatDocument(ctx context.Context) error {
	source, found, err := p.readDocumentSource()
	if err != nil || !found {
		return err
	}
	if _, recognized := RenderFlatCredentialMigration(source); !recognized {
		return nil
	}
	release, err := p.acquireLock(ctx)
	if err != nil {
		return err
	}
	defer release()
	current, stillPresent, err := p.readDocumentSource()
	if err != nil {
		return err
	}
	if !stillPresent {
		return fmt.Errorf("harness: credential file %s disappeared during flat-layout migration", p.filename)
	}
	migrated, recognized := RenderFlatCredentialMigration(current)
	if !recognized {
		return nil
	}
	if err := atomicWriteSettingsFile(p.filename, migrated); err != nil {
		return err
	}
	p.logger.Info("credential file migrated to version 1; values are unchanged", "path", p.filename)
	return nil
}

func decodeCredentialRefs(node *yaml.Node, target map[CredentialRef]string) error {
	if node.Kind != yaml.MappingNode {
		return NewProviderError("credential refs must be a mapping", InvalidCredentialCode, nil)
	}
	for index := 0; index < len(node.Content); index += 2 {
		nameNode, valueNode := node.Content[index], node.Content[index+1]
		if nameNode.Kind != yaml.ScalarNode || valueNode.Kind != yaml.ScalarNode || valueNode.Tag != "!!str" {
			return NewProviderError("credential refs must map names to strings", InvalidCredentialCode, nil)
		}
		ref, err := ParseCredentialRef(nameNode.Value)
		if err != nil {
			return NewProviderError("credential file contains an invalid ref name", InvalidCredentialCode, nil)
		}
		if _, duplicate := target[ref]; duplicate {
			return NewProviderError("credential file contains a duplicate ref", InvalidCredentialCode, nil)
		}
		if valueNode.Value == "" {
			return NewProviderError("credential file contains an empty ref value", InvalidCredentialCode, nil)
		}
		target[ref] = valueNode.Value
	}
	return nil
}

func decodeCredentialRecords(node *yaml.Node, target map[CredentialKey]CredentialRecord) ([]CredentialKey, error) {
	if node.Kind != yaml.MappingNode {
		return nil, NewProviderError("credential records must be a mapping", InvalidCredentialCode, nil)
	}
	order := make([]CredentialKey, 0, len(node.Content)/2)
	for index := 0; index < len(node.Content); index += 2 {
		keyNode, recordNode := node.Content[index], node.Content[index+1]
		if keyNode.Kind != yaml.ScalarNode {
			return nil, NewProviderError("credential record keys must be strings", InvalidCredentialCode, nil)
		}
		key, err := ParseCredentialKey(keyNode.Value)
		if err != nil {
			return nil, NewProviderError("credential file contains an invalid record key", InvalidCredentialCode, nil)
		}
		if _, duplicate := target[key]; duplicate {
			return nil, NewProviderError("credential file contains a duplicate record key", InvalidCredentialCode, nil)
		}
		record, err := decodeCredentialRecordNode(key, recordNode)
		if err != nil {
			return nil, err
		}
		target[key] = record
		order = append(order, key)
	}
	return order, nil
}

func decodeCredentialRecordNode(key CredentialKey, node *yaml.Node) (CredentialRecord, error) {
	if node.Kind != yaml.MappingNode {
		return CredentialRecord{}, NewProviderError("credential record must be a mapping", InvalidCredentialCode, nil)
	}
	var record CredentialRecord
	seen := make(map[string]struct{}, len(node.Content)/2)
	for index := 0; index < len(node.Content); index += 2 {
		name, value := node.Content[index].Value, node.Content[index+1]
		if _, duplicate := seen[name]; duplicate {
			return CredentialRecord{}, NewProviderError("credential record contains a duplicate field", InvalidCredentialCode, nil)
		}
		seen[name] = struct{}{}
		switch name {
		case "kind":
			if value.Kind != yaml.ScalarNode || value.Tag != "!!str" {
				return CredentialRecord{}, NewProviderError("credential record kind must be a string", InvalidCredentialCode, nil)
			}
			record.Kind = CredentialRecordKind(value.Value)
		case "key":
			if value.Kind != yaml.ScalarNode || value.Tag != "!!str" {
				return CredentialRecord{}, NewProviderError("credential record key value must be a string", InvalidCredentialCode, nil)
			}
			secret := value.Value
			record.Key = &secret
		case "env":
			if value.Kind != yaml.MappingNode {
				return CredentialRecord{}, NewProviderError("credential record env must be a mapping", InvalidCredentialCode, nil)
			}
			record.Env = make(map[string]string, len(value.Content)/2)
			for envIndex := 0; envIndex < len(value.Content); envIndex += 2 {
				envName, envValue := value.Content[envIndex], value.Content[envIndex+1]
				if envName.Kind != yaml.ScalarNode || envValue.Kind != yaml.ScalarNode || envValue.Tag != "!!str" {
					return CredentialRecord{}, NewProviderError("credential record env values must be strings", InvalidCredentialCode, nil)
				}
				if _, duplicate := record.Env[envName.Value]; duplicate {
					return CredentialRecord{}, NewProviderError("credential record contains a duplicate env name", InvalidCredentialCode, nil)
				}
				record.Env[envName.Value] = envValue.Value
			}
		case "payload":
			var decoded any
			if err := value.Decode(&decoded); err != nil {
				return CredentialRecord{}, NewProviderError("credential grant payload is invalid", InvalidCredentialCode, nil)
			}
			normalized, err := normalizeYAMLValue(decoded)
			if err != nil {
				return CredentialRecord{}, NewProviderError("credential grant payload is invalid", InvalidCredentialCode, nil)
			}
			record.GrantPayload, err = json.Marshal(normalized)
			if err != nil {
				return CredentialRecord{}, NewProviderError("credential grant payload is invalid", InvalidCredentialCode, nil)
			}
		default:
			return CredentialRecord{}, NewProviderError("credential record contains an unknown field", InvalidCredentialCode, nil)
		}
	}
	validated, err := validateCredentialRecord(key, &record)
	if err != nil {
		return CredentialRecord{}, NewProviderError("credential record is invalid", InvalidCredentialCode, nil)
	}
	return validated, nil
}

func (p *FileCredentialProvider) ResolveCredential(ctx context.Context, ref CredentialRef) (ResolvedCredential, bool, error) {
	if err := p.checkActive(); err != nil {
		return ResolvedCredential{}, false, err
	}
	if err := validateCredentialRef(ref); err != nil {
		return ResolvedCredential{}, false, err
	}
	resolved, found, err := p.store.ResolveCredential(ctx, ref)
	if err != nil || found {
		return resolved, found, err
	}
	if value, found := p.inheritedCredential(ref); found {
		return ResolvedCredential{Value: value, Source: "env"}, true, nil
	}
	if entry, found := p.dotenvCredential(ref); found {
		return ResolvedCredential{Value: entry.Value, Source: string(entry.Source)}, true, nil
	}
	return ResolvedCredential{}, false, nil
}

func (p *FileCredentialProvider) Describe(ref CredentialRef) (CredentialInfo, error) {
	if err := p.checkActive(); err != nil {
		return CredentialInfo{}, err
	}
	if err := validateCredentialRef(ref); err != nil {
		return CredentialInfo{}, err
	}
	info, err := p.store.Describe(ref)
	if err != nil || info.Configured {
		return info, err
	}
	if _, found := p.inheritedCredential(ref); found {
		return CredentialInfo{Configured: true, Source: "env", Writable: true}, nil
	}
	if entry, found := p.dotenvCredential(ref); found {
		return CredentialInfo{Configured: true, Source: string(entry.Source), Writable: true}, nil
	}
	return info, nil
}

func (p *FileCredentialProvider) inheritedCredential(ref CredentialRef) (string, bool) {
	if p.launchEnv != nil {
		entry, found := p.launchEnv.GetFrom(string(ref), LaunchEnvironmentProcess)
		return entry.Value, found && entry.Value != ""
	}
	value, found := p.lookupEnv(string(ref))
	return value, found && value != ""
}

func (p *FileCredentialProvider) dotenvCredential(ref CredentialRef) (LaunchEnvironmentEntry, bool) {
	if p.launchEnv == nil {
		return LaunchEnvironmentEntry{}, false
	}
	entry, found := p.launchEnv.GetFrom(string(ref), LaunchEnvironmentProject, LaunchEnvironmentUser)
	return entry, found && entry.Value != ""
}

func (p *FileCredentialProvider) Set(ref CredentialRef, value, _ string) error {
	if err := p.checkActive(); err != nil {
		return err
	}
	if err := validateCredentialRef(ref); err != nil {
		return err
	}
	if value == "" {
		return errors.New("harness: credential value must not be empty; use Unset")
	}
	return p.commit(context.Background(), func(document *credentialDocument) (bool, error) {
		if document.refs[ref] == value {
			return false, nil
		}
		document.refs[ref] = value
		return true, nil
	})
}

func (p *FileCredentialProvider) Unset(ref CredentialRef) error {
	if err := p.checkActive(); err != nil {
		return err
	}
	if err := validateCredentialRef(ref); err != nil {
		return err
	}
	return p.commit(context.Background(), func(document *credentialDocument) (bool, error) {
		if _, found := document.refs[ref]; !found {
			return false, nil
		}
		delete(document.refs, ref)
		return true, nil
	})
}

func (p *FileCredentialProvider) ReadRecord(key CredentialKey) (*CredentialRecord, error) {
	return p.store.ReadRecord(key)
}

func (p *FileCredentialProvider) DescribeRecord(key CredentialKey) (CredentialRecordInfo, error) {
	return p.store.DescribeRecord(key)
}

func (p *FileCredentialProvider) ListRecords() ([]CredentialRecordEntry, error) {
	return p.store.ListRecords()
}

func (p *FileCredentialProvider) ListSecretRefs() ([]SecretRefView, error) {
	return p.store.ListSecretRefs()
}

func (p *FileCredentialProvider) ModifyRecord(ctx context.Context, key CredentialKey, mutation CredentialRecordMutation) (*CredentialRecord, error) {
	if err := validateCredentialKey(key); err != nil {
		return nil, err
	}
	if mutation == nil {
		return nil, errors.New("harness: credential record mutation must not be nil")
	}
	var result *CredentialRecord
	err := p.commit(ctx, func(document *credentialDocument) (bool, error) {
		var current *CredentialRecord
		var previous *CredentialRecord
		if stored, found := document.records[key]; found {
			cloned := cloneCredentialRecord(stored)
			current = &cloned
			original := cloneCredentialRecord(stored)
			previous = &original
		}
		next, err := mutation(current)
		if err != nil {
			return false, err
		}
		if next == nil {
			result = current
			return false, nil
		}
		validated, err := validateCredentialRecord(key, next)
		if err != nil {
			return false, err
		}
		if _, found := document.records[key]; !found {
			document.recordOrder = append(document.recordOrder, key)
		}
		document.records[key] = validated
		cloned := cloneCredentialRecord(validated)
		result = &cloned
		return !reflect.DeepEqual(previous, result), nil
	})
	return result, err
}

func (p *FileCredentialProvider) DeleteRecord(ctx context.Context, key CredentialKey) error {
	if err := validateCredentialKey(key); err != nil {
		return err
	}
	return p.commit(ctx, func(document *credentialDocument) (bool, error) {
		if _, found := document.records[key]; !found {
			return false, nil
		}
		delete(document.records, key)
		for index, ordered := range document.recordOrder {
			if ordered == key {
				document.recordOrder = append(document.recordOrder[:index], document.recordOrder[index+1:]...)
				break
			}
		}
		return true, nil
	})
}

func (p *FileCredentialProvider) Watch(run *point.Run, listener func(CredentialRef)) (point.Dispose, error) {
	return p.store.Watch(run, listener)
}

func (p *FileCredentialProvider) WatchRecords(run *point.Run, listener func(CredentialKey)) (point.Dispose, error) {
	return p.store.WatchRecords(run, listener)
}

func (p *FileCredentialProvider) commit(ctx context.Context, mutation func(*credentialDocument) (bool, error)) error {
	if p == nil {
		return errors.New("harness: credential file provider is nil")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	p.mu.Lock()
	var notifications credentialNotifications
	defer func() {
		p.mu.Unlock()
		p.notifyChanges(notifications)
	}()
	if p.disposed {
		return NewProviderError("credential provider was disposed", CredentialsDisposedCode, nil)
	}
	if err := os.MkdirAll(filepath.Dir(p.filename), 0o700); err != nil {
		return err
	}
	release, err := p.acquireLock(ctx)
	if err != nil {
		return err
	}
	defer release()
	document, err := p.loadDocument()
	if err != nil {
		return err
	}
	changed, err := mutation(&document)
	if err != nil {
		return err
	}
	if changed {
		rendered, err := renderCredentialDocument(document)
		if err != nil {
			return err
		}
		if err := atomicWriteSettingsFile(p.filename, rendered); err != nil {
			return err
		}
		document.source = rendered
		p.warnedSource, p.hasWarnedSource = sha256.Sum256(rendered), true
	}
	notifications, err = p.applyDocumentState(document)
	return err
}

func renderCredentialDocument(document credentialDocument) ([]byte, error) {
	root, err := parseCredentialYAMLRoot(document.source)
	if err != nil {
		root, _ = parseYAMLRoot(nil)
	}
	mapping := root.Content[0]
	// Alias identity, not anchor spelling, determines what retained metadata
	// means. Duplicate anchor names are legal YAML; reparsing alone could let an
	// alias silently retarget after a managed anchor is removed.
	managed := make(map[*yaml.Node]bool)
	var collect func(*yaml.Node)
	collect = func(node *yaml.Node) {
		managed[node] = true
		for _, child := range node.Content {
			collect(child)
		}
	}
	for i := 0; i < len(mapping.Content); i += 2 {
		switch mapping.Content[i].Value {
		case "version", "refs", "records":
			collect(mapping.Content[i+1])
		}
	}
	var referencesManaged func(*yaml.Node) bool
	referencesManaged = func(node *yaml.Node) bool {
		if node.Kind == yaml.AliasNode && managed[node.Alias] {
			return true
		}
		for _, child := range node.Content {
			if referencesManaged(child) {
				return true
			}
		}
		return false
	}
	for i := 0; i < len(mapping.Content); i += 2 {
		switch mapping.Content[i].Value {
		case "version", "refs", "records":
			continue
		}
		if referencesManaged(mapping.Content[i+1]) {
			return nil, NewProviderError("credential edit cannot preserve metadata aliases into managed entries", InvalidCredentialCode, nil)
		}
	}
	setYAMLMappingValue(mapping, "version", &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!int", Value: "1"})
	setYAMLMappingValue(mapping, "refs", encodeCredentialRefs(document.refs))
	records, err := encodeCredentialRecords(document.records, document.recordOrder)
	if err != nil {
		return nil, err
	}
	setYAMLMappingValue(mapping, "records", records)
	var output bytes.Buffer
	encoder := yaml.NewEncoder(&output)
	encoder.SetIndent(2)
	if err := encoder.Encode(root); err != nil {
		return nil, err
	}
	if err := encoder.Close(); err != nil {
		return nil, err
	}
	// Replacing managed nodes may remove anchors referenced by retained foreign
	// metadata. Reject before atomic publication rather than persist dangling
	// aliases and make the next reload/boot unreadable. Never echo YAML values.
	if _, err := parseCredentialYAMLRoot(output.Bytes()); err != nil {
		return nil, NewProviderError("credential edit would invalidate retained YAML aliases", InvalidCredentialCode, nil)
	}
	return output.Bytes(), nil
}

func setYAMLMappingValue(mapping *yaml.Node, name string, next *yaml.Node) {
	for index := 0; index < len(mapping.Content); index += 2 {
		if mapping.Content[index].Value == name {
			previous := mapping.Content[index+1]
			next.HeadComment, next.LineComment, next.FootComment = previous.HeadComment, previous.LineComment, previous.FootComment
			mapping.Content[index+1] = next
			return
		}
	}
	mapping.Content = append(mapping.Content, &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: name}, next)
}

func encodeCredentialRefs(refs map[CredentialRef]string) *yaml.Node {
	node := &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
	names := make([]string, 0, len(refs))
	for ref := range refs {
		names = append(names, string(ref))
	}
	sort.Strings(names)
	for _, name := range names {
		node.Content = append(node.Content,
			&yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: name},
			&yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: refs[CredentialRef(name)]},
		)
	}
	return node
}

func encodeCredentialRecords(records map[CredentialKey]CredentialRecord, order []CredentialKey) (*yaml.Node, error) {
	node := &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
	seen := make(map[CredentialKey]struct{}, len(records))
	ordered := append([]CredentialKey(nil), order...)
	remaining := make([]string, 0)
	for key := range records {
		found := false
		for _, candidate := range ordered {
			if candidate == key {
				found = true
				break
			}
		}
		if !found {
			remaining = append(remaining, string(key))
		}
	}
	sort.Strings(remaining)
	for _, key := range remaining {
		ordered = append(ordered, CredentialKey(key))
	}
	for _, key := range ordered {
		record, found := records[key]
		if !found {
			continue
		}
		if _, duplicate := seen[key]; duplicate {
			continue
		}
		seen[key] = struct{}{}
		recordNode, err := encodeCredentialRecord(record)
		if err != nil {
			return nil, err
		}
		node.Content = append(node.Content, &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: string(key)}, recordNode)
	}
	return node, nil
}

func encodeCredentialRecord(record CredentialRecord) (*yaml.Node, error) {
	node := &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
	appendField := func(name string, value *yaml.Node) {
		node.Content = append(node.Content, &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: name}, value)
	}
	appendField("kind", &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: string(record.Kind)})
	if record.Key != nil {
		appendField("key", &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: *record.Key})
	}
	if record.Env != nil {
		appendField("env", encodeStringMap(record.Env))
	}
	if len(record.GrantPayload) != 0 {
		var decoded any
		if err := json.Unmarshal(record.GrantPayload, &decoded); err != nil {
			return nil, err
		}
		payload := &yaml.Node{}
		if err := payload.Encode(decoded); err != nil {
			return nil, err
		}
		appendField("payload", payload)
	}
	return node, nil
}

func encodeStringMap(values map[string]string) *yaml.Node {
	node := &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		node.Content = append(node.Content,
			&yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: key},
			&yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: values[key]},
		)
	}
	return node
}

type credentialNotifications struct {
	refs           []CredentialRef
	records        []CredentialKey
	refWatchers    []func(CredentialRef)
	recordWatchers []func(CredentialKey)
}

func (p *FileCredentialProvider) applyDocument(document credentialDocument) error {
	notifications, err := p.applyDocumentState(document)
	if err != nil {
		return err
	}
	p.notifyChanges(notifications)
	return nil
}

func (p *FileCredentialProvider) applyDocumentState(document credentialDocument) (credentialNotifications, error) {
	store := p.store
	store.mu.Lock()
	if store.disposed {
		store.mu.Unlock()
		return credentialNotifications{}, NewProviderError("credential store was disposed", CredentialsDisposedCode, nil)
	}
	refChanged := make([]CredentialRef, 0)
	for ref, previous := range store.values {
		if next, found := document.refs[ref]; !found || next != previous.Value {
			refChanged = append(refChanged, ref)
		}
	}
	for ref, next := range document.refs {
		if previous, found := store.values[ref]; !found || previous.Value != next {
			if !containsCredentialRef(refChanged, ref) {
				refChanged = append(refChanged, ref)
			}
		}
	}
	recordChanged := make([]CredentialKey, 0)
	for key, previous := range store.records {
		if next, found := document.records[key]; !found || !reflect.DeepEqual(previous, next) {
			recordChanged = append(recordChanged, key)
		}
	}
	for key, next := range document.records {
		if previous, found := store.records[key]; !found || !reflect.DeepEqual(previous, next) {
			if !containsCredentialKey(recordChanged, key) {
				recordChanged = append(recordChanged, key)
			}
		}
	}
	store.values = make(map[CredentialRef]ResolvedCredential, len(document.refs))
	for ref, value := range document.refs {
		store.values[ref] = ResolvedCredential{Value: value, Source: "file"}
	}
	store.records = make(map[CredentialKey]CredentialRecord, len(document.records))
	for key, record := range document.records {
		store.records[key] = cloneCredentialRecord(record)
	}
	store.recordOrder = append([]CredentialKey(nil), document.recordOrder...)
	notifications := credentialNotifications{
		refs: refChanged, records: recordChanged,
		refWatchers: store.watchersLocked(), recordWatchers: store.recordWatchersLocked(),
	}
	store.mu.Unlock()
	return notifications, nil
}

func (p *FileCredentialProvider) notifyChanges(notifications credentialNotifications) {
	sort.Slice(notifications.refs, func(i, j int) bool { return notifications.refs[i] < notifications.refs[j] })
	sort.Slice(notifications.records, func(i, j int) bool { return notifications.records[i] < notifications.records[j] })
	for _, ref := range notifications.refs {
		p.store.notify(ref, notifications.refWatchers)
	}
	for _, key := range notifications.records {
		p.store.notifyRecord(key, notifications.recordWatchers)
	}
}

func containsCredentialRef(values []CredentialRef, target CredentialRef) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func containsCredentialKey(values []CredentialKey, target CredentialKey) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func (p *FileCredentialProvider) acquireLock(ctx context.Context) (func(), error) {
	lockPath := p.filename + ".lock"
	deadline := time.Now().Add(p.lockWait)
	for {
		file, err := os.OpenFile(lockPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
		if err == nil {
			_, _ = fmt.Fprintf(file, "%d\n", os.Getpid())
			_ = file.Close()
			return func() { _ = os.Remove(lockPath) }, nil
		}
		if !errors.Is(err, os.ErrExist) {
			return nil, err
		}
		if p.lockWait == 0 || !time.Now().Before(deadline) {
			return nil, fmt.Errorf("harness: timed out waiting for credential writer lock %s", lockPath)
		}
		timer := time.NewTimer(p.lockPoll)
		select {
		case <-ctx.Done():
			if !timer.Stop() {
				<-timer.C
			}
			return nil, ctx.Err()
		case <-timer.C:
		}
	}
}

type credentialFingerprint struct {
	exists bool
	mode   os.FileMode
	digest [sha256.Size]byte
}

func (p *FileCredentialProvider) fingerprint() (credentialFingerprint, error) {
	info, err := os.Lstat(p.filename)
	if errors.Is(err, os.ErrNotExist) {
		return credentialFingerprint{}, nil
	}
	if err != nil {
		return credentialFingerprint{}, err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return credentialFingerprint{}, fmt.Errorf("harness: credential file %s must be a regular non-symlink file", p.filename)
	}
	source, err := os.ReadFile(p.filename)
	if err != nil {
		return credentialFingerprint{}, err
	}
	return credentialFingerprint{exists: true, mode: info.Mode().Perm(), digest: sha256.Sum256(source)}, nil
}

func (p *FileCredentialProvider) Reload(context.Context) error {
	p.mu.Lock()
	var notifications credentialNotifications
	defer func() {
		p.mu.Unlock()
		p.notifyChanges(notifications)
	}()
	if p.disposed {
		return NewProviderError("credential provider was disposed", CredentialsDisposedCode, nil)
	}
	document, err := p.loadDocument()
	if err != nil {
		return err
	}
	notifications, err = p.applyDocumentState(document)
	return err
}

func (p *FileCredentialProvider) StartWatching(run *point.Run) error {
	if p == nil || run == nil || !run.Active() {
		return point.ErrInactiveRun
	}
	if p.disableWatch {
		return nil
	}
	p.mu.Lock()
	if p.disposed {
		p.mu.Unlock()
		return NewProviderError("credential provider was disposed", CredentialsDisposedCode, nil)
	}
	if p.watchStarted {
		p.mu.Unlock()
		return errors.New("harness: credential file watcher already started")
	}
	initial, err := p.fingerprint()
	if err != nil {
		p.mu.Unlock()
		return err
	}
	p.watchStarted = true
	p.mu.Unlock()
	ctx, cancel := context.WithCancel(run.Context)
	var wait sync.WaitGroup
	wait.Add(1)
	go func() {
		defer wait.Done()
		last := initial
		ticker := time.NewTicker(p.watchInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				current, fingerprintErr := p.fingerprint()
				if fingerprintErr != nil {
					p.logger.Warn("credential watcher could not inspect document", "error", fingerprintErr)
					continue
				}
				if current == last {
					continue
				}
				last = current
				if reloadErr := p.Reload(ctx); reloadErr != nil && ctx.Err() == nil {
					p.logger.Warn("credential watcher kept last-good document", "error", reloadErr)
				}
			}
		}
	}()
	stop := func(context.Context) error {
		cancel()
		wait.Wait()
		return nil
	}
	if err := run.Defer("credential-file:stop-watcher", stop); err != nil {
		_ = stop(context.Background())
		return err
	}
	return nil
}

func (p *FileCredentialProvider) Dispose(ctx context.Context) error {
	if p == nil {
		return nil
	}
	p.mu.Lock()
	p.disposed = true
	p.mu.Unlock()
	return p.store.Dispose(ctx)
}

var _ CredentialProvider = (*FileCredentialProvider)(nil)
