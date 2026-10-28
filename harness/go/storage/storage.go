// Package storage implements DSH-shaped durable KV backends and domains.
// Backends own media; domains own schema-checked in-memory state, serialize
// writes, and publish changes only after durability succeeds.
package storage

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"sync"

	point "github.com/phodal/pero/harness/go/runtime"
)

var (
	BackendRegistryKey = point.NewKey[*BackendRegistry]("harness.storage.backends")
	DomainFacilityKey  = point.NewKey[*DomainFacility]("harness.storage.domains")
	JSONBackendKey     = point.NewKey[*JSONBackend]("harness.storage.json")
)

type ErrorCode string

const (
	BackendNotFound  ErrorCode = "backend-not-found"
	DuplicateBackend ErrorCode = "duplicate-backend"
	VersionMismatch  ErrorCode = "version-mismatch"
	MalformedMedium  ErrorCode = "malformed-medium"
	AlreadyOpen      ErrorCode = "already-open"
	FacetUnsupported ErrorCode = "facet-unsupported"
	InvalidRecord    ErrorCode = "invalid-record"
	MissingKey       ErrorCode = "missing-key"
	Closed           ErrorCode = "closed"
)

type Error struct {
	Code    ErrorCode
	Message string
	Domain  string
	Table   string
	Key     string
	Cause   error
}

func (e *Error) Error() string            { return e.Message }
func (e *Error) Unwrap() error            { return e.Cause }
func (e *Error) StorageErrorCode() string { return string(e.Code) }

type Layout string

const (
	LayoutSingle    Layout = "single"
	LayoutPerRecord Layout = "per-record"
)

var (
	unitNamePattern  = regexp.MustCompile(`^[a-z][a-z0-9_]*$`)
	recordKeyPattern = regexp.MustCompile(`^[a-zA-Z0-9_-]+$`)
)

type KVUnitDescriptor struct {
	Name    string
	Version uint64
	// CompatibleVersions admits predecessor stamps only for per-record reads.
	// Writes always use Version; the single layout still requires an exact stamp.
	// Nil preserves historical legacy bootstrap; a declared (even empty) slice
	// also restricts legacy bootstrap to current plus compatible versions.
	CompatibleVersions []uint64
	Tables             []string
	HasGlobal          bool
	Layout             Layout
}

type KVSnapshot struct {
	// TableOrder optionally preserves backend record iteration order. Backends
	// without an ordered medium retain the existing deterministic key fallback.
	TableOrder map[string][]string
	Tables     map[string]map[string]json.RawMessage
	Global     json.RawMessage
}

type KVUnit interface {
	LoadAll(context.Context) (KVSnapshot, error)
	PutRecord(context.Context, string, string, json.RawMessage) error
	DeleteRecord(context.Context, string, string) error
	SetGlobal(context.Context, json.RawMessage) error
	Close(context.Context) error
}

// KVRecordBackupper is optional. Disposable domains can preserve an invalid
// record before excluding it; backends without this capability fail closed.
type KVRecordBackupper interface {
	BackupRecord(context.Context, string, string) (string, error)
}

type KVFacet interface {
	Open(context.Context, KVUnitDescriptor) (KVUnit, error)
}

type Backend interface {
	KV() KVFacet
	Close(context.Context) error
}

type BackendRegistry struct {
	mu       sync.RWMutex
	backends map[string]Backend
}

func NewBackendRegistry() *BackendRegistry {
	return &BackendRegistry{backends: make(map[string]Backend)}
}

func (r *BackendRegistry) Register(name string, backend Backend) (func(), error) {
	if r == nil || name == "" || backend == nil {
		return nil, errors.New("storage: backend registration requires a name and backend")
	}
	r.mu.Lock()
	if _, exists := r.backends[name]; exists {
		r.mu.Unlock()
		return nil, &Error{Code: DuplicateBackend, Message: fmt.Sprintf("storage backend %q is already registered", name)}
	}
	r.backends[name] = backend
	r.mu.Unlock()
	var once sync.Once
	return func() {
		once.Do(func() {
			r.mu.Lock()
			if r.backends[name] == backend {
				delete(r.backends, name)
			}
			r.mu.Unlock()
		})
	}, nil
}

func (r *BackendRegistry) Get(name string) (Backend, error) {
	if r == nil {
		return nil, &Error{Code: BackendNotFound, Message: "storage backend registry is nil"}
	}
	r.mu.RLock()
	backend := r.backends[name]
	names := make([]string, 0, len(r.backends))
	for candidate := range r.backends {
		names = append(names, candidate)
	}
	r.mu.RUnlock()
	if backend == nil {
		sort.Strings(names)
		available := "none"
		if len(names) != 0 {
			available = fmt.Sprint(names)
		}
		return nil, &Error{Code: BackendNotFound, Message: fmt.Sprintf("storage backend %q is not registered (registered: %s)", name, available)}
	}
	return backend, nil
}

func (r *BackendRegistry) Names() []string {
	if r == nil {
		return nil
	}
	r.mu.RLock()
	result := make([]string, 0, len(r.backends))
	for name := range r.backends {
		result = append(result, name)
	}
	r.mu.RUnlock()
	sort.Strings(result)
	return result
}

type CoreOptions struct {
	JSONRoot       string
	DefaultBackend string
	Routes         map[string]string
}

func Definitions(options CoreOptions) []point.Definition {
	backendName := options.DefaultBackend
	if backendName == "" {
		backendName = "json"
	}
	hub := point.Define(point.Descriptor{ID: "harness/core/storage", Provides: []point.AnyKey{BackendRegistryKey.Any()}}, point.DecodeJSON[struct{}], func(run *point.Run, _ struct{}) (point.Dispose, error) {
		_, err := point.Provide(run, BackendRegistryKey, NewBackendRegistry())
		return nil, err
	})
	definitions := []point.Definition{hub}
	if options.JSONRoot != "" {
		root := options.JSONRoot
		definitions = append(definitions, point.Define(point.Descriptor{ID: "harness/core/storage-json", Requires: []point.Requirement{point.Require(BackendRegistryKey)}, Provides: []point.AnyKey{JSONBackendKey.Any()}}, point.DecodeJSON[struct{}], func(run *point.Run, _ struct{}) (point.Dispose, error) {
			registry, err := point.Resolve(run, BackendRegistryKey)
			if err != nil {
				return nil, err
			}
			backend, err := NewJSONBackend(root)
			if err != nil {
				return nil, err
			}
			unregister, err := registry.Register("json", backend)
			if err != nil {
				_ = backend.Close(context.Background())
				return nil, err
			}
			if _, err = point.Provide(run, JSONBackendKey, backend); err != nil {
				unregister()
				_ = backend.Close(context.Background())
				return nil, err
			}
			return func(ctx context.Context) error { unregister(); return backend.Close(ctx) }, nil
		}))
	}
	requires := []point.Requirement{point.Require(BackendRegistryKey)}
	if options.JSONRoot != "" && backendName == "json" {
		requires = append(requires, point.Require(JSONBackendKey))
	}
	routes := make(map[string]string, len(options.Routes))
	for key, value := range options.Routes {
		routes[key] = value
	}
	definitions = append(definitions, point.Define(point.Descriptor{ID: "harness/core/storage-domain", Requires: requires, Provides: []point.AnyKey{DomainFacilityKey.Any()}}, point.DecodeJSON[struct{}], func(run *point.Run, _ struct{}) (point.Dispose, error) {
		registry, err := point.Resolve(run, BackendRegistryKey)
		if err != nil {
			return nil, err
		}
		facility, err := NewDomainFacility(registry, DomainConfig{Backend: backendName, Routes: routes}, run.Logger())
		if err != nil {
			return nil, err
		}
		if _, err = point.Provide(run, DomainFacilityKey, facility); err != nil {
			return nil, err
		}
		return facility.Close, nil
	}))
	return definitions
}
