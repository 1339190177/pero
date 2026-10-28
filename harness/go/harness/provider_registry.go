package harness

import (
	"context"
	"errors"
	"fmt"
	"github.com/phodal/pero/harness/go/internal/sessionlogroute"
	"log/slog"
	"sync"

	point "github.com/phodal/pero/harness/go/runtime"
)

const (
	NoAdapterCode            = "NO_ADAPTER"
	InvalidAdapterCode       = "INVALID_ADAPTER"
	DuplicateAdapterCode     = "DUPLICATE_ADAPTER"
	RegistrationDisposedCode = "REGISTRATION_DISPOSED"
)

// ProviderInfo is detached display metadata for one registered route.
type ProviderInfo struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// ProviderInfoResolver optionally gives an adapter a display name for each
// route it owns. ID must exactly preserve the registered provider string.
type ProviderInfoResolver interface {
	ResolveProviderInfo(provider string) ProviderInfo
}

type providerRoute struct {
	generation    *byte
	owner         *providerRegistrationState
	adapter       Adapter
	info          ProviderInfo
	policy        ResolvedRetryPolicy
	modelMetadata map[string]ModelInfo
}

// ProviderRegistry is the provider-neutral adapter published as AdapterKey.
// Route mutation is copy-on-commit under one lock; prepared calls retain the
// selected adapter generation after a later replace or disposal.
type ProviderRegistry struct {
	fallback          Adapter
	fallbackModelInfo ModelInfoResolver
	logger            *slog.Logger

	mu              sync.RWMutex
	revision        uint64
	routes          map[string]providerRoute
	order           []string
	watchers        map[uint64]func([]ProviderInfo)
	nextWatcherID   uint64
	directory       map[string]configurableProviderRoute
	directoryOrder  []string
	discoveries     map[string]modelDiscoveryOffer
	nextDiscoveryID uint64
}

func NewProviderRegistry(fallback Adapter, modelInfo ModelInfoResolver, logger *slog.Logger) *ProviderRegistry {
	if logger == nil {
		logger = slog.Default()
	}
	if modelInfo == nil && fallback != nil {
		modelInfo, _ = fallback.(ModelInfoResolver)
	}
	return &ProviderRegistry{
		fallback: fallback, fallbackModelInfo: modelInfo, logger: logger,
		routes: make(map[string]providerRoute), watchers: make(map[uint64]func([]ProviderInfo)),
		directory: make(map[string]configurableProviderRoute), discoveries: make(map[string]modelDiscoveryOffer),
	}
}

// ListProviders returns detached metadata in route registration order.
func (r *ProviderRegistry) ListProviders() []ProviderInfo {
	if r == nil {
		return nil
	}
	r.mu.RLock()
	result := r.listProvidersLocked()
	r.mu.RUnlock()
	return result
}

func (r *ProviderRegistry) listProvidersLocked() []ProviderInfo {
	result := make([]ProviderInfo, 0, len(r.order))
	for _, provider := range r.order {
		if route, ok := r.routes[provider]; ok {
			result = append(result, route.info)
		}
	}
	return result
}

// Watch registers an effect-owned topology observer. Observer panics are
// contained after the committed registry is already visible.
func (r *ProviderRegistry) Watch(run *point.Run, listener func([]ProviderInfo)) (point.Dispose, error) {
	if r == nil || listener == nil {
		return nil, errors.New("harness: provider registry and watcher must not be nil")
	}
	if run == nil || !run.Active() {
		return nil, point.ErrInactiveRun
	}
	r.mu.Lock()
	r.nextWatcherID++
	id := r.nextWatcherID
	r.watchers[id] = listener
	r.mu.Unlock()
	dispose := func(context.Context) error {
		r.mu.Lock()
		delete(r.watchers, id)
		r.mu.Unlock()
		return nil
	}
	if err := run.Defer("provider-registry:watch", dispose); err != nil {
		_ = dispose(context.Background())
		return nil, err
	}
	return dispose, nil
}

func (r *ProviderRegistry) notify(snapshot []ProviderInfo, watchers []func([]ProviderInfo)) {
	for _, watcher := range watchers {
		func() {
			defer func() {
				if recovered := recover(); recovered != nil {
					r.logger.Warn("provider topology watcher panicked", "panic", recovered)
				}
			}()
			watcher(append([]ProviderInfo(nil), snapshot...))
		}()
	}
}

func (r *ProviderRegistry) topologyLocked() ([]ProviderInfo, []func([]ProviderInfo)) {
	snapshot := r.listProvidersLocked()
	watchers := make([]func([]ProviderInfo), 0, len(r.watchers))
	for id := uint64(1); id <= r.nextWatcherID; id++ {
		if watcher, ok := r.watchers[id]; ok {
			watchers = append(watchers, watcher)
		}
	}
	return snapshot, watchers
}

type preparedProviderRoute struct {
	sessionLogCurrent func() bool
	adapter           Adapter
	policy            *ResolvedRetryPolicy
	resolver          ModelInfoResolver
}

func (r *ProviderRegistry) route(provider string) (preparedProviderRoute, error) {
	r.mu.RLock()
	route, found := r.routes[provider]
	fallback := r.fallback
	fallbackModelInfo := r.fallbackModelInfo
	r.mu.RUnlock()
	if found {
		policy := cloneRetryPolicy(route.policy)
		resolver, _ := route.adapter.(ModelInfoResolver)
		return preparedProviderRoute{adapter: route.adapter, policy: &policy, resolver: resolver, sessionLogCurrent: func() bool {
			r.mu.RLock()
			current, ok := r.routes[provider]
			r.mu.RUnlock()
			return ok && current.owner == route.owner && current.generation == route.generation
		}}, nil
	}
	if fallback == nil {
		return preparedProviderRoute{}, NewProviderError(
			fmt.Sprintf("no adapter registered for provider %q", provider), NoAdapterCode, nil,
		)
	}
	prepared := preparedProviderRoute{adapter: fallback, resolver: fallbackModelInfo}
	if policies, ok := fallback.(RetryPolicyProvider); ok {
		if policy, found := policies.RetryPolicyForProvider(provider); found {
			cloned := cloneRetryPolicy(policy)
			prepared.policy = &cloned
		}
	}
	return prepared, nil
}

func (r *ProviderRegistry) Generate(ctx context.Context, request GenerateRequest, emit func(StreamChunk) error) (Completion, error) {
	prepared, resolved, err := r.PrepareGenerateCall(ctx, request)
	if err != nil {
		return Completion{}, err
	}
	return prepared.Generate(ctx, resolved, emit)
}

// PrepareGenerateCall atomically selects a provider route and lets that
// adapter materialize request defaults while retaining the selected route
// generation for dispatch.
func (r *ProviderRegistry) PrepareGenerateCall(ctx context.Context, request GenerateRequest) (PreparedAdapterCall, GenerateRequest, error) {
	selected, err := r.route(request.Provider)
	if err != nil {
		return PreparedAdapterCall{}, GenerateRequest{}, err
	}
	if preparer, ok := selected.adapter.(GenerateCallPreparer); ok {
		prepared, resolved, err := preparer.PrepareGenerateCall(ctx, request)
		if err != nil {
			return PreparedAdapterCall{}, GenerateRequest{}, err
		}
		if resolved.Provider != request.Provider || resolved.Model != request.Model {
			return PreparedAdapterCall{}, GenerateRequest{}, NewProviderError(
				"prepared model call changed its provider route", InvalidPreparedCallCode, nil,
			)
		}
		if selected.policy != nil && prepared.RetryPolicy == nil {
			prepared.RetryPolicy = selected.policy
		}
		prepared = bindPreparedSessionLogOwner(prepared, selected.sessionLogCurrent)
		prepared, err = validatePreparedAdapterCall(resolved.Provider, resolved.Model, prepared)
		return prepared, resolved, err
	}
	prepared, err := r.PrepareAdapterCall(ctx, request.Provider, request.Model)
	return prepared, request, err
}

func (r *ProviderRegistry) ResolveModelInfo(ctx context.Context, provider, model string) (ModelInfo, error) {
	prepared, err := r.PrepareAdapterCall(ctx, provider, model)
	if err != nil {
		return ModelInfo{}, err
	}
	return prepared.ModelInfo, nil
}

func (r *ProviderRegistry) RetryPolicyForProvider(provider string) (ResolvedRetryPolicy, bool) {
	prepared, err := r.route(provider)
	if err != nil || prepared.policy == nil {
		return ResolvedRetryPolicy{}, false
	}
	return cloneRetryPolicy(*prepared.policy), true
}

// PrepareAdapterCall binds model metadata, retry policy, and dispatch to one
// route generation. A later atomic route replacement cannot split the call.
func (r *ProviderRegistry) PrepareAdapterCall(ctx context.Context, provider, model string) (PreparedAdapterCall, error) {
	selected, err := r.route(provider)
	if err != nil {
		return PreparedAdapterCall{}, err
	}
	if preparer, ok := selected.adapter.(AdapterCallPreparer); ok {
		prepared, err := preparer.PrepareAdapterCall(ctx, provider, model)
		if err != nil {
			return PreparedAdapterCall{}, err
		}
		if selected.policy != nil {
			prepared.RetryPolicy = selected.policy
		}
		return validatePreparedAdapterCall(provider, model, bindPreparedSessionLogOwner(prepared, selected.sessionLogCurrent))
	}
	info := ModelInfo{Provider: provider, Model: model}
	if selected.resolver != nil {
		info, err = selected.resolver.ResolveModelInfo(ctx, provider, model)
		if err != nil {
			return PreparedAdapterCall{}, err
		}
	}
	return validatePreparedAdapterCall(provider, model, bindPreparedSessionLogOwner(PreparedAdapterCall{
		ModelInfo: info, RetryPolicy: selected.policy, Generate: selected.adapter.Generate,
	}, selected.sessionLogCurrent))
}

type providerRegistrationState struct {
	identity *byte
	registry *ProviderRegistry
	adapter  Adapter

	mu       sync.Mutex
	owned    []string
	released bool
}

// Register atomically publishes an adapter's complete provider route set and
// binds its removal to run. The returned handle may replace the set or release
// it early; activation cleanup remains idempotent.
func (r *ProviderRegistry) Register(run *point.Run, providers []string, adapter Adapter) (*ProviderRegistration, error) {
	return r.register(run, providers, adapter, nil)
}

func (r *ProviderRegistry) register(run *point.Run, providers []string, adapter Adapter, publication *ModelMetadataPublication) (*ProviderRegistration, error) {
	if run == nil || !run.Active() {
		return nil, point.ErrInactiveRun
	}
	if adapter == nil {
		return nil, NewProviderError("adapter must not be nil", InvalidAdapterCode, nil)
	}
	if len(providers) == 0 {
		return nil, NewProviderError("an adapter must register at least one provider", InvalidAdapterCode, nil)
	}
	providers = append([]string(nil), providers...)
	registration := &providerRegistrationState{identity: new(byte), registry: r, adapter: adapter}
	if err := registration.replaceAdapterWithModelMetadata(providers, adapter, publication); err != nil {
		return nil, err
	}
	if err := run.Defer("provider-registry:unregister", registration.dispose); err != nil {
		_ = registration.dispose(context.Background())
		return nil, err
	}
	return &ProviderRegistration{state: registration}, nil
}

func (s *providerRegistrationState) replace(providers []string) error {
	s.mu.Lock()
	if s.released {
		s.mu.Unlock()
		return NewProviderError("a disposed adapter registration cannot replace its routes", RegistrationDisposedCode, nil)
	}
	snapshot, watchers, err := s.replaceLocked(providers, s.adapter, nil, true)
	s.mu.Unlock()
	if err == nil {
		s.registry.notify(snapshot, watchers)
	}
	return err
}

func (s *providerRegistrationState) replaceAdapter(providers []string, adapter Adapter) error {
	return s.replaceAdapterWithModelMetadata(providers, adapter, nil)
}

func (s *providerRegistrationState) replaceAdapterWithModelMetadata(providers []string, adapter Adapter, publication *ModelMetadataPublication) error {
	if adapter == nil {
		return NewProviderError("adapter must not be nil", InvalidAdapterCode, nil)
	}
	s.mu.Lock()
	if s.released {
		s.mu.Unlock()
		return NewProviderError("a disposed adapter registration cannot replace its adapter", RegistrationDisposedCode, nil)
	}
	snapshot, watchers, err := s.replaceLocked(providers, adapter, publication, false)
	s.mu.Unlock()
	if err == nil {
		s.registry.notify(snapshot, watchers)
	}
	return err
}

func (s *providerRegistrationState) replaceLocked(
	providers []string,
	adapter Adapter,
	publication *ModelMetadataPublication,
	preserveMetadata bool,
) ([]ProviderInfo, []func([]ProviderInfo), error) {
	metadata, err := preparePublishedModels(providers, publication)
	if err != nil {
		return nil, nil, err
	}
	prepared, err := prepareProviderRoutes(providers, adapter, s)
	if err != nil {
		return nil, nil, err
	}
	r := s.registry
	r.mu.Lock()
	for _, route := range prepared {
		if existing, found := r.routes[route.info.ID]; found && existing.owner != s {
			r.mu.Unlock()
			return nil, nil, NewProviderError(
				fmt.Sprintf("an adapter for provider %q is already registered", route.info.ID), DuplicateAdapterCode, nil,
			)
		}
	}
	for index := range prepared {
		route := &prepared[index]
		route.modelMetadata = metadata[route.info.ID]
		if preserveMetadata {
			if previous, found := r.routes[route.info.ID]; found && previous.owner == s {
				route.modelMetadata = previous.modelMetadata
			}
		}
	}
	owned := make(map[string]struct{}, len(s.owned))
	for _, provider := range s.owned {
		owned[provider] = struct{}{}
		delete(r.routes, provider)
	}
	nextOrder := make([]string, 0, len(r.order)-len(owned)+len(prepared))
	for _, provider := range r.order {
		if _, remove := owned[provider]; !remove {
			nextOrder = append(nextOrder, provider)
		}
	}
	s.owned = s.owned[:0]
	for _, route := range prepared {
		route.generation = new(byte)
		r.routes[route.info.ID] = route
		nextOrder = append(nextOrder, route.info.ID)
		s.owned = append(s.owned, route.info.ID)
	}
	s.adapter = adapter
	r.order = nextOrder
	r.revision++
	snapshot, watchers := r.topologyLocked()
	r.mu.Unlock()
	return snapshot, watchers, nil
}

func (s *providerRegistrationState) dispose(context.Context) error {
	s.mu.Lock()
	if s.released {
		s.mu.Unlock()
		return nil
	}
	s.released = true
	r := s.registry
	r.mu.Lock()
	owned := make(map[string]struct{}, len(s.owned))
	for _, provider := range s.owned {
		owned[provider] = struct{}{}
		if route, found := r.routes[provider]; found && route.owner == s {
			delete(r.routes, provider)
		}
	}
	nextOrder := r.order[:0]
	for _, provider := range r.order {
		if _, remove := owned[provider]; !remove {
			nextOrder = append(nextOrder, provider)
		}
	}
	r.order = nextOrder
	s.owned = nil
	r.revision++
	snapshot, watchers := r.topologyLocked()
	r.mu.Unlock()
	s.mu.Unlock()
	r.notify(snapshot, watchers)
	return nil
}

func prepareProviderRoutes(providers []string, adapter Adapter, owner *providerRegistrationState) ([]providerRoute, error) {
	seen := make(map[string]struct{}, len(providers))
	prepared := make([]providerRoute, 0, len(providers))
	for _, provider := range providers {
		if provider == "" {
			return nil, NewProviderError("adapter provider names must be non-empty", InvalidAdapterCode, nil)
		}
		if _, duplicate := seen[provider]; duplicate {
			return nil, NewProviderError(
				fmt.Sprintf("an adapter for provider %q is already registered", provider), DuplicateAdapterCode, nil,
			)
		}
		seen[provider] = struct{}{}
		info := ProviderInfo{ID: provider, Name: provider}
		if resolver, ok := adapter.(ProviderInfoResolver); ok {
			info = resolver.ResolveProviderInfo(provider)
		}
		if info.ID != provider || info.Name == "" {
			return nil, NewProviderError(
				fmt.Sprintf("adapter metadata for provider %q must preserve its id and have a non-empty name", provider),
				InvalidAdapterCode, nil,
			)
		}
		policy, err := ResolveRetryPolicy(nil)
		if err != nil {
			return nil, err
		}
		if source, ok := adapter.(RetryPolicyProvider); ok {
			if candidate, found := source.RetryPolicyForProvider(provider); found {
				if err := ValidateResolvedRetryPolicy(candidate); err != nil {
					return nil, NewProviderError(
						fmt.Sprintf("adapter retry policy for provider %q is invalid: %v", provider, err), InvalidAdapterCode, err,
					)
				}
				policy = cloneRetryPolicy(candidate)
			}
		}
		prepared = append(prepared, providerRoute{owner: owner, adapter: adapter, info: info, policy: policy})
	}
	return prepared, nil
}

// ProviderRegistration is an effect-owned dynamic provider contribution.
type ProviderRegistration struct {
	state    *providerRegistrationState
	instance *point.Instance
}

// Replace atomically swaps every route owned by this registration. An empty
// replacement is valid and leaves the live contribution holding no routes.
// Published metadata survives only on retained same-provider routes; every
// replacement still creates fresh route-generation references.
func (r *ProviderRegistration) Replace(providers []string) error {
	if r == nil || r.state == nil {
		return NewProviderError("provider registration is unavailable", RegistrationDisposedCode, nil)
	}
	return r.state.replace(append([]string(nil), providers...))
}

// ReplaceAdapter atomically swaps both the complete route set and the adapter
// generation that owns it. Calls prepared before the swap retain the previous
// adapter; calls prepared after it cannot observe a mixture of generations.
// This ordinary replacement clears any passive model metadata publication.
func (r *ProviderRegistration) ReplaceAdapter(providers []string, adapter Adapter) error {
	if r == nil || r.state == nil {
		return NewProviderError("provider registration is unavailable", RegistrationDisposedCode, nil)
	}
	return r.state.replaceAdapter(append([]string(nil), providers...), adapter)
}

func (r *ProviderRegistration) Dispose(ctx context.Context) error {
	if r == nil {
		return nil
	}
	if r.instance != nil {
		return r.instance.Dispose(ctx)
	}
	if r.state != nil {
		return r.state.dispose(ctx)
	}
	return nil
}

var _ Adapter = (*ProviderRegistry)(nil)
var _ ModelInfoResolver = (*ProviderRegistry)(nil)
var _ RetryPolicyProvider = (*ProviderRegistry)(nil)
var _ AdapterCallPreparer = (*ProviderRegistry)(nil)
var _ GenerateCallPreparer = (*ProviderRegistry)(nil)

func bindPreparedSessionLogOwner(prepared PreparedAdapterCall, current func() bool) PreparedAdapterCall {
	if prepared.Generate == nil {
		return prepared
	}
	generate := prepared.Generate
	prepared.Generate = func(ctx context.Context, request GenerateRequest, emit func(StreamChunk) error) (Completion, error) {
		return generate(sessionlogroute.BindOwner(ctx, current), request, emit)
	}
	return prepared
}
