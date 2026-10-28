package config

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"regexp"
	"sort"
	"strings"
	"sync"

	point "github.com/phodal/pero/harness/go/runtime"
)

const (
	MissingCredentialCode   = "MISSING_CREDENTIAL"
	InvalidCredentialCode   = "INVALID_CREDENTIAL"
	CredentialsDisposedCode = "CREDENTIALS_DISPOSED"
)

var credentialRefPattern = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)
var credentialKeySegmentPattern = regexp.MustCompile(`^[a-z][a-z0-9-]*$`)

// CredentialRef is a validated POSIX-style environment-variable name. It is
// an address only; secret values never belong in provider configuration.
type CredentialRef string

func ParseCredentialRef(value string) (CredentialRef, error) {
	if !credentialRefPattern.MatchString(value) {
		return "", fmt.Errorf("harness: credential ref %q must match %s", value, credentialRefPattern)
	}
	return CredentialRef(value), nil
}

func validateCredentialRef(ref CredentialRef) error {
	_, err := ParseCredentialRef(string(ref))
	return err
}

type ResolvedCredential struct {
	Value  string
	Source string
}

// CredentialKey addresses one owner-defined stored record as <scope>/<id>.
// Its slash keeps this key space disjoint from CredentialRef.
type CredentialKey string

func NewCredentialKey(scope, id string) (CredentialKey, error) {
	for _, segment := range []string{scope, id} {
		if !credentialKeySegmentPattern.MatchString(segment) {
			return "", fmt.Errorf("harness: credential key segment %q must match %s", segment, credentialKeySegmentPattern)
		}
	}
	return CredentialKey(scope + "/" + id), nil
}

func ParseCredentialKey(value string) (CredentialKey, error) {
	segments := strings.Split(value, "/")
	if len(segments) != 2 {
		return "", fmt.Errorf("harness: credential key %q must be <scope>/<id>", value)
	}
	return NewCredentialKey(segments[0], segments[1])
}

func (k CredentialKey) Scope() string {
	scope, _, _ := strings.Cut(string(k), "/")
	return scope
}

func (k CredentialKey) ID() string {
	_, id, _ := strings.Cut(string(k), "/")
	return id
}

func validateCredentialKey(key CredentialKey) error {
	_, err := ParseCredentialKey(string(key))
	return err
}

type CredentialRecordKind string

const (
	CredentialRecordAPIKey CredentialRecordKind = "api-key"
	CredentialRecordGrant  CredentialRecordKind = "grant"
)

// CredentialRecord preserves both DSH record shapes. Key is a pointer so an
// absent key remains distinct from an invalid blank key. GrantPayload is
// opaque owner JSON and is never interpreted by the credential seam.
type CredentialRecord struct {
	Kind         CredentialRecordKind `json:"kind"`
	Key          *string              `json:"key,omitempty"`
	Env          map[string]string    `json:"env,omitempty"`
	GrantPayload json.RawMessage      `json:"payload,omitempty"`
}

type CredentialRecordInfo struct {
	Configured bool                 `json:"configured"`
	Kind       CredentialRecordKind `json:"kind,omitempty"`
	Writable   bool                 `json:"writable"`
}

type CredentialRecordEntry struct {
	Key  CredentialKey        `json:"key"`
	Kind CredentialRecordKind `json:"kind"`
}

// SecretRefView names one registered secretRef. It has no field that can carry the value.
type SecretRefView struct {
	Ref    string `json:"ref"`
	Source string `json:"source,omitempty"`
}

type CredentialRecordMutation func(*CredentialRecord) (*CredentialRecord, error)

// CredentialResolver resolves a reference once per operation. Implementations
// must not return an empty value as configured.
type CredentialResolver interface {
	ResolveCredential(context.Context, CredentialRef) (ResolvedCredential, bool, error)
}

type CredentialProvider interface {
	CredentialResolver
	Describe(CredentialRef) (CredentialInfo, error)
	Set(CredentialRef, string, string) error
	Unset(CredentialRef) error
	ReadRecord(CredentialKey) (*CredentialRecord, error)
	DescribeRecord(CredentialKey) (CredentialRecordInfo, error)
	ListRecords() ([]CredentialRecordEntry, error)
	ModifyRecord(context.Context, CredentialKey, CredentialRecordMutation) (*CredentialRecord, error)
	DeleteRecord(context.Context, CredentialKey) error
	Watch(*point.Run, func(CredentialRef)) (point.Dispose, error)
	WatchRecords(*point.Run, func(CredentialKey)) (point.Dispose, error)
}

type CredentialWatchProvider interface {
	StartWatching(*point.Run) error
}

type CredentialProviderLifecycle interface {
	Dispose(context.Context) error
}

type CredentialResolverFunc func(context.Context, CredentialRef) (ResolvedCredential, bool, error)

func (fn CredentialResolverFunc) ResolveCredential(ctx context.Context, ref CredentialRef) (ResolvedCredential, bool, error) {
	return fn(ctx, ref)
}

// EnvironmentCredentialResolver reads the current process environment at each
// operation. Lookup may be replaced in tests or by a launch-environment seam.
type EnvironmentCredentialResolver struct {
	Lookup    func(string) (string, bool)
	LaunchEnv *LaunchEnvironmentSnapshot
}

func (r EnvironmentCredentialResolver) ResolveCredential(_ context.Context, ref CredentialRef) (ResolvedCredential, bool, error) {
	if err := validateCredentialRef(ref); err != nil {
		return ResolvedCredential{}, false, err
	}
	if r.LaunchEnv != nil {
		entry, found := r.LaunchEnv.Get(string(ref))
		if !found || entry.Value == "" {
			return ResolvedCredential{}, false, nil
		}
		source := string(entry.Source)
		if entry.Source == LaunchEnvironmentProcess {
			source = "env"
		}
		return ResolvedCredential{Value: entry.Value, Source: source}, true, nil
	}
	lookup := r.Lookup
	if lookup == nil {
		lookup = os.LookupEnv
	}
	value, found := lookup(string(ref))
	if !found || value == "" {
		return ResolvedCredential{}, false, nil
	}
	return ResolvedCredential{Value: value, Source: "env"}, true, nil
}

type CredentialInfo struct {
	Configured bool   `json:"configured"`
	Source     string `json:"source,omitempty"`
	Writable   bool   `json:"writable"`
}

// CredentialStore is a process-local writable credential seam. Enumeration
// returns addresses only; no API returns a secret value.
type CredentialStore struct {
	logger *slog.Logger

	mu                  sync.RWMutex
	values              map[CredentialRef]ResolvedCredential
	watchers            map[uint64]func(CredentialRef)
	nextWatcherID       uint64
	records             map[CredentialKey]CredentialRecord
	recordOrder         []CredentialKey
	recordWatchers      map[uint64]func(CredentialKey)
	nextRecordWatcherID uint64
	disposed            bool
}

func NewCredentialStore(logger *slog.Logger) *CredentialStore {
	if logger == nil {
		logger = slog.Default()
	}
	return &CredentialStore{
		logger: logger, values: make(map[CredentialRef]ResolvedCredential), watchers: make(map[uint64]func(CredentialRef)),
		records: make(map[CredentialKey]CredentialRecord), recordWatchers: make(map[uint64]func(CredentialKey)),
	}
}

func (s *CredentialStore) ResolveCredential(_ context.Context, ref CredentialRef) (ResolvedCredential, bool, error) {
	if s == nil {
		return ResolvedCredential{}, false, errors.New("harness: credential store is nil")
	}
	if err := validateCredentialRef(ref); err != nil {
		return ResolvedCredential{}, false, err
	}
	s.mu.RLock()
	if s.disposed {
		s.mu.RUnlock()
		return ResolvedCredential{}, false, NewProviderError("credential store was disposed", CredentialsDisposedCode, nil)
	}
	resolved, found := s.values[ref]
	s.mu.RUnlock()
	if !found || resolved.Value == "" {
		return ResolvedCredential{}, false, nil
	}
	return resolved, true, nil
}

func (s *CredentialStore) Describe(ref CredentialRef) (CredentialInfo, error) {
	resolved, found, err := s.ResolveCredential(context.Background(), ref)
	if err != nil {
		return CredentialInfo{}, err
	}
	if !found {
		return CredentialInfo{Writable: true}, nil
	}
	return CredentialInfo{Configured: true, Source: resolved.Source, Writable: true}, nil
}

func (s *CredentialStore) Set(ref CredentialRef, value, source string) error {
	if s == nil {
		return errors.New("harness: credential store is nil")
	}
	if err := validateCredentialRef(ref); err != nil {
		return err
	}
	if value == "" {
		return errors.New("harness: credential value must not be empty; use Unset")
	}
	if source == "" {
		source = "memory"
	}
	s.mu.Lock()
	if s.disposed {
		s.mu.Unlock()
		return NewProviderError("credential store was disposed", CredentialsDisposedCode, nil)
	}
	s.values[ref] = ResolvedCredential{Value: value, Source: source}
	watchers := s.watchersLocked()
	s.mu.Unlock()
	s.notify(ref, watchers)
	return nil
}

func (s *CredentialStore) Unset(ref CredentialRef) error {
	if s == nil {
		return errors.New("harness: credential store is nil")
	}
	if err := validateCredentialRef(ref); err != nil {
		return err
	}
	s.mu.Lock()
	if s.disposed {
		s.mu.Unlock()
		return NewProviderError("credential store was disposed", CredentialsDisposedCode, nil)
	}
	_, found := s.values[ref]
	if found {
		delete(s.values, ref)
	}
	watchers := s.watchersLocked()
	s.mu.Unlock()
	if found {
		s.notify(ref, watchers)
	}
	return nil
}

func validateCredentialRecord(key CredentialKey, source *CredentialRecord) (CredentialRecord, error) {
	if err := validateCredentialKey(key); err != nil {
		return CredentialRecord{}, err
	}
	if source == nil {
		return CredentialRecord{}, errors.New("harness: credential record must not be nil")
	}
	record := CredentialRecord{Kind: source.Kind}
	switch source.Kind {
	case CredentialRecordAPIKey:
		if len(source.GrantPayload) != 0 {
			return CredentialRecord{}, fmt.Errorf("harness: api-key record %q cannot contain a grant payload", key)
		}
		if source.Key != nil {
			if *source.Key == "" {
				return CredentialRecord{}, fmt.Errorf("harness: api-key record %q has an empty key", key)
			}
			value := *source.Key
			record.Key = &value
		}
		if source.Env != nil {
			record.Env = make(map[string]string, len(source.Env))
			for name, value := range source.Env {
				if _, err := ParseCredentialRef(name); err != nil {
					return CredentialRecord{}, fmt.Errorf("harness: api-key record %q environment: %w", key, err)
				}
				if value == "" {
					return CredentialRecord{}, fmt.Errorf("harness: api-key record %q env %q must be a non-empty string", key, name)
				}
				record.Env[name] = value
			}
		}
	case CredentialRecordGrant:
		if source.Key != nil || len(source.Env) != 0 {
			return CredentialRecord{}, fmt.Errorf("harness: grant record %q cannot contain api-key fields", key)
		}
		if len(source.GrantPayload) == 0 {
			return CredentialRecord{}, fmt.Errorf("harness: grant record %q has no payload", key)
		}
		var payload any
		decoder := json.NewDecoder(strings.NewReader(string(source.GrantPayload)))
		decoder.UseNumber()
		if err := decoder.Decode(&payload); err != nil {
			return CredentialRecord{}, fmt.Errorf("harness: grant record %q payload is not JSON-compatible", key)
		}
		var trailing any
		if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
			return CredentialRecord{}, fmt.Errorf("harness: grant record %q payload is not JSON-compatible", key)
		}
		raw, err := json.Marshal(payload)
		if err != nil {
			return CredentialRecord{}, fmt.Errorf("harness: grant record %q payload is not JSON-compatible", key)
		}
		record.GrantPayload = raw
	default:
		return CredentialRecord{}, fmt.Errorf("harness: credential record %q has unknown kind %q", key, source.Kind)
	}
	return record, nil
}

func cloneCredentialRecord(record CredentialRecord) CredentialRecord {
	if record.Key != nil {
		value := *record.Key
		record.Key = &value
	}
	if record.Env != nil {
		environment := make(map[string]string, len(record.Env))
		for name, value := range record.Env {
			environment[name] = value
		}
		record.Env = environment
	}
	record.GrantPayload = append(json.RawMessage(nil), record.GrantPayload...)
	return record
}

func (s *CredentialStore) ReadRecord(key CredentialKey) (*CredentialRecord, error) {
	if s == nil {
		return nil, errors.New("harness: credential store is nil")
	}
	if err := validateCredentialKey(key); err != nil {
		return nil, err
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.disposed {
		return nil, NewProviderError("credential store was disposed", CredentialsDisposedCode, nil)
	}
	record, found := s.records[key]
	if !found {
		return nil, nil
	}
	cloned := cloneCredentialRecord(record)
	return &cloned, nil
}

func (s *CredentialStore) DescribeRecord(key CredentialKey) (CredentialRecordInfo, error) {
	record, err := s.ReadRecord(key)
	if err != nil {
		return CredentialRecordInfo{}, err
	}
	if record == nil {
		return CredentialRecordInfo{Writable: true}, nil
	}
	return CredentialRecordInfo{Configured: true, Kind: record.Kind, Writable: true}, nil
}

// ListSecretRefs enumerates registered secretRef addresses and where each was
// registered. Values are not copied out.
func (s *CredentialStore) ListSecretRefs() ([]SecretRefView, error) {
	if s == nil {
		return nil, errors.New("harness: credential store is nil")
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.disposed {
		return nil, NewProviderError("credential store was disposed", CredentialsDisposedCode, nil)
	}
	refs := make([]string, 0, len(s.values))
	for ref, resolved := range s.values {
		if resolved.Value == "" {
			continue
		}
		refs = append(refs, string(ref))
	}
	sort.Strings(refs)
	views := make([]SecretRefView, 0, len(refs))
	for _, ref := range refs {
		views = append(views, SecretRefView{Ref: ref, Source: s.values[CredentialRef(ref)].Source})
	}
	return views, nil
}

func (s *CredentialStore) ListRecords() ([]CredentialRecordEntry, error) {
	if s == nil {
		return nil, errors.New("harness: credential store is nil")
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.disposed {
		return nil, NewProviderError("credential store was disposed", CredentialsDisposedCode, nil)
	}
	result := make([]CredentialRecordEntry, 0, len(s.recordOrder))
	for _, key := range s.recordOrder {
		if record, found := s.records[key]; found {
			result = append(result, CredentialRecordEntry{Key: key, Kind: record.Kind})
		}
	}
	return result, nil
}

func (s *CredentialStore) ModifyRecord(
	_ context.Context,
	key CredentialKey,
	mutation CredentialRecordMutation,
) (*CredentialRecord, error) {
	if s == nil {
		return nil, errors.New("harness: credential store is nil")
	}
	if err := validateCredentialKey(key); err != nil {
		return nil, err
	}
	if mutation == nil {
		return nil, errors.New("harness: credential record mutation must not be nil")
	}
	s.mu.Lock()
	if s.disposed {
		s.mu.Unlock()
		return nil, NewProviderError("credential store was disposed", CredentialsDisposedCode, nil)
	}
	var current *CredentialRecord
	if stored, found := s.records[key]; found {
		cloned := cloneCredentialRecord(stored)
		current = &cloned
	}
	next, err := mutation(current)
	if err != nil {
		s.mu.Unlock()
		return nil, err
	}
	if next == nil {
		s.mu.Unlock()
		return current, nil
	}
	validated, err := validateCredentialRecord(key, next)
	if err != nil {
		s.mu.Unlock()
		return nil, err
	}
	if _, found := s.records[key]; !found {
		s.recordOrder = append(s.recordOrder, key)
	}
	s.records[key] = validated
	watchers := s.recordWatchersLocked()
	s.mu.Unlock()
	s.notifyRecord(key, watchers)
	result := cloneCredentialRecord(validated)
	return &result, nil
}

func (s *CredentialStore) DeleteRecord(_ context.Context, key CredentialKey) error {
	if s == nil {
		return errors.New("harness: credential store is nil")
	}
	if err := validateCredentialKey(key); err != nil {
		return err
	}
	s.mu.Lock()
	if s.disposed {
		s.mu.Unlock()
		return NewProviderError("credential store was disposed", CredentialsDisposedCode, nil)
	}
	_, found := s.records[key]
	if found {
		delete(s.records, key)
		for index, ordered := range s.recordOrder {
			if ordered == key {
				s.recordOrder = append(s.recordOrder[:index], s.recordOrder[index+1:]...)
				break
			}
		}
	}
	watchers := s.recordWatchersLocked()
	s.mu.Unlock()
	if found {
		s.notifyRecord(key, watchers)
	}
	return nil
}

func (s *CredentialStore) WatchRecords(run *point.Run, listener func(CredentialKey)) (point.Dispose, error) {
	if s == nil || run == nil || !run.Active() {
		return nil, point.ErrInactiveRun
	}
	if listener == nil {
		return nil, errors.New("harness: credential record watcher must not be nil")
	}
	s.mu.Lock()
	if s.disposed {
		s.mu.Unlock()
		return nil, NewProviderError("credential store was disposed", CredentialsDisposedCode, nil)
	}
	s.nextRecordWatcherID++
	id := s.nextRecordWatcherID
	s.recordWatchers[id] = listener
	s.mu.Unlock()
	dispose := func(context.Context) error {
		s.mu.Lock()
		delete(s.recordWatchers, id)
		s.mu.Unlock()
		return nil
	}
	if err := run.Defer("credentials:unwatch-records", dispose); err != nil {
		_ = dispose(context.Background())
		return nil, err
	}
	return dispose, nil
}

func (s *CredentialStore) recordWatchersLocked() []func(CredentialKey) {
	result := make([]func(CredentialKey), 0, len(s.recordWatchers))
	for id := uint64(1); id <= s.nextRecordWatcherID; id++ {
		if watcher, found := s.recordWatchers[id]; found {
			result = append(result, watcher)
		}
	}
	return result
}

func (s *CredentialStore) notifyRecord(key CredentialKey, watchers []func(CredentialKey)) {
	for _, watcher := range watchers {
		func() {
			defer func() {
				if recovered := recover(); recovered != nil {
					s.logger.Warn("credential record watcher panicked", "key", key, "panic", recovered)
				}
			}()
			watcher(key)
		}()
	}
}

func (s *CredentialStore) Dispose(context.Context) error {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	s.disposed = true
	s.values = nil
	s.records = nil
	s.recordOrder = nil
	s.watchers = nil
	s.recordWatchers = nil
	s.mu.Unlock()
	return nil
}

func (s *CredentialStore) Watch(run *point.Run, listener func(CredentialRef)) (point.Dispose, error) {
	if s == nil || run == nil || !run.Active() {
		return nil, point.ErrInactiveRun
	}
	if listener == nil {
		return nil, errors.New("harness: credential watcher must not be nil")
	}
	s.mu.Lock()
	if s.disposed {
		s.mu.Unlock()
		return nil, NewProviderError("credential store was disposed", CredentialsDisposedCode, nil)
	}
	s.nextWatcherID++
	id := s.nextWatcherID
	s.watchers[id] = listener
	s.mu.Unlock()
	dispose := func(context.Context) error {
		s.mu.Lock()
		delete(s.watchers, id)
		s.mu.Unlock()
		return nil
	}
	if err := run.Defer("credentials:unwatch", dispose); err != nil {
		_ = dispose(context.Background())
		return nil, err
	}
	return dispose, nil
}

func (s *CredentialStore) watchersLocked() []func(CredentialRef) {
	result := make([]func(CredentialRef), 0, len(s.watchers))
	for id := uint64(1); id <= s.nextWatcherID; id++ {
		if watcher, found := s.watchers[id]; found {
			result = append(result, watcher)
		}
	}
	return result
}

func (s *CredentialStore) notify(ref CredentialRef, watchers []func(CredentialRef)) {
	for _, watcher := range watchers {
		func() {
			defer func() {
				if recovered := recover(); recovered != nil {
					s.logger.Warn("credential watcher panicked", "ref", ref, "panic", recovered)
				}
			}()
			watcher(ref)
		}()
	}
}

// AssertUsableAPIKey trims one supplied value and rejects anything outside
// printable non-space ASCII. Diagnostics identify only the reference, never
// the secret value or any substring of it.
func AssertUsableAPIKey(raw, providerPackage string, ref CredentialRef) (string, error) {
	value := strings.TrimSpace(raw)
	if value == "" {
		return "", NewProviderError(
			fmt.Sprintf("%s: the API key resolved from %s is blank", providerPackage, ref), InvalidCredentialCode, nil,
		)
	}
	for _, character := range []byte(value) {
		if character < 0x21 || character > 0x7e {
			return "", NewProviderError(
				fmt.Sprintf("%s: the API key resolved from %s contains characters no HTTP header can carry", providerPackage, ref),
				InvalidCredentialCode, nil,
			)
		}
	}
	return value, nil
}

func ResolveAPIKey(ctx context.Context, resolver CredentialResolver, providerPackage string, ref CredentialRef) (string, error) {
	if resolver == nil {
		return "", NewProviderError(
			fmt.Sprintf("%s: no credential resolver is mounted for %s", providerPackage, ref), MissingCredentialCode, nil,
		)
	}
	resolved, found, err := resolver.ResolveCredential(ctx, ref)
	if err != nil {
		return "", err
	}
	if !found {
		return "", NewProviderError(
			fmt.Sprintf("%s: no API key is configured for %s", providerPackage, ref), MissingCredentialCode, nil,
		)
	}
	return AssertUsableAPIKey(resolved.Value, providerPackage, ref)
}

var _ CredentialResolver = (*CredentialStore)(nil)
var _ CredentialResolver = EnvironmentCredentialResolver{}
var _ CredentialProvider = (*CredentialStore)(nil)
