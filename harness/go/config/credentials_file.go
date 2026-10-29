package config

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	point "github.com/phodal/pero/harness/go/runtime"
	"gopkg.in/yaml.v3"
	"log/slog"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"sort"
	"sync"
	"time"
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
