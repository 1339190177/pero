package runtime

import (
	"context"
	"sync/atomic"
)

// RealmID selects an independent provider slot for one service key. Zero is the
// root realm shared by scopes without an override.
type RealmID uint64

var nextRealmID atomic.Uint64

func NewRealm() RealmID { return RealmID(nextRealmID.Add(1)) }

type Scope struct {
	runtime *Runtime
	parent  *Scope
	realms  map[uint64]RealmID
}

type scopeConfig struct {
	realms map[uint64]RealmID
}

type ScopeOption interface {
	applyScope(*scopeConfig)
}

type scopeOptionFunc func(*scopeConfig)

func (fn scopeOptionFunc) applyScope(cfg *scopeConfig) { fn(cfg) }

func WithRealm[T any](key Key[T], realm RealmID) ScopeOption {
	return scopeOptionFunc(func(cfg *scopeConfig) {
		if cfg.realms == nil {
			cfg.realms = make(map[uint64]RealmID, 1)
		}
		cfg.realms[key.id] = realm
	})
}

// Child returns an immutable child view. Only local overrides are allocated;
// all other realm lookups follow the parent chain.
func (s *Scope) Child(options ...ScopeOption) *Scope {
	if s == nil {
		return nil
	}
	cfg := scopeConfig{}
	for _, option := range options {
		if option != nil {
			option.applyScope(&cfg)
		}
	}
	return &Scope{runtime: s.runtime, parent: s, realms: cfg.realms}
}

func Isolate[T any](scope *Scope, key Key[T]) *Scope {
	return scope.Child(WithRealm(key, NewRealm()))
}

func JoinRealm[T any](scope *Scope, key Key[T], realm RealmID) *Scope {
	return scope.Child(WithRealm(key, realm))
}

func (s *Scope) realm(keyID uint64) RealmID {
	for current := s; current != nil; current = current.parent {
		if realm, ok := current.realms[keyID]; ok {
			return realm
		}
	}
	return 0
}

func (s *Scope) Runtime() *Runtime {
	if s == nil {
		return nil
	}
	return s.runtime
}

// Contains reports whether candidate is this scope or one of its descendants.
// It lets extension registries implement spatial visibility without exposing
// the scope parent chain.
func (s *Scope) Contains(candidate *Scope) bool {
	if s == nil || candidate == nil || s.runtime != candidate.runtime {
		return false
	}
	for current := candidate; current != nil; current = current.parent {
		if current == s {
			return true
		}
	}
	return false
}

type callContextKey struct{}

type callContextValue struct {
	scope    *Scope
	instance *Instance
}

// CallContext marks a service call with the caller's spatial identity.
func (r *Run) CallContext(parent context.Context) context.Context {
	if parent == nil {
		parent = context.Background()
	}
	return context.WithValue(parent, callContextKey{}, callContextValue{
		scope: r.Scope, instance: r.Instance,
	})
}

func ScopeFromContext(ctx context.Context) (*Scope, bool) {
	if ctx == nil {
		return nil, false
	}
	value, ok := ctx.Value(callContextKey{}).(callContextValue)
	return value.scope, ok && value.scope != nil
}

func InstanceFromContext(ctx context.Context) (*Instance, bool) {
	if ctx == nil {
		return nil, false
	}
	value, ok := ctx.Value(callContextKey{}).(callContextValue)
	return value.instance, ok && value.instance != nil
}
