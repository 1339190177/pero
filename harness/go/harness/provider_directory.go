package harness

import (
	"context"
	"errors"
	"fmt"
	"sync"

	point "github.com/phodal/pero/harness/go/runtime"
)

const (
	InvalidDirectoryCode   = "INVALID_DIRECTORY"
	DuplicateDirectoryCode = "DUPLICATE_DIRECTORY"
	InvalidDiscoveryCode   = "INVALID_DISCOVERY"
	DuplicateDiscoveryCode = "DUPLICATE_DISCOVERY"
	NoDiscoveryCode        = "NO_DISCOVERY"
	InvalidCatalogCode     = "INVALID_CATALOG"
)

// ConfigurableProvider describes one route a settings surface can activate,
// whether or not an adapter currently owns the live route.
type ConfigurableProvider struct {
	Provider          string   `json:"provider"`
	DisplayName       string   `json:"displayName"`
	SettingsNamespace string   `json:"settingsNs"`
	SettingsPath      []string `json:"settingsPath"`
	Declared          *bool    `json:"declared,omitempty"`
}

func cloneConfigurableProvider(entry ConfigurableProvider) ConfigurableProvider {
	// The root provider profile is the empty path, serialized as [] so the
	// generated DSH editor can iterate it like every nested profile path.
	entry.SettingsPath = append([]string{}, entry.SettingsPath...)
	if entry.Declared != nil {
		value := *entry.Declared
		entry.Declared = &value
	}
	return entry
}

type configurableProviderRoute struct {
	owner *configurableProviderRegistrationState
	entry ConfigurableProvider
}

type configurableProviderRegistrationState struct {
	registry *ProviderRegistry

	mu       sync.Mutex
	held     []ConfigurableProvider
	released bool
}

// ConfigurableProviderRegistration is the live, atomically replaceable handle
// returned by RegisterConfigurableProviders.
type ConfigurableProviderRegistration struct {
	state *configurableProviderRegistrationState
}

// RegisterConfigurableProviders publishes an effect-owned provider directory
// contribution. Initial registration must be non-empty; Replace may be empty.
func (r *ProviderRegistry) RegisterConfigurableProviders(run *point.Run, entries []ConfigurableProvider) (*ConfigurableProviderRegistration, error) {
	if r == nil || run == nil || !run.Active() {
		return nil, point.ErrInactiveRun
	}
	if len(entries) == 0 {
		return nil, NewProviderError("a configurable-provider registration must declare at least one provider", InvalidDirectoryCode, nil)
	}
	state := &configurableProviderRegistrationState{registry: r}
	if err := state.replace(cloneConfigurableProviders(entries)); err != nil {
		return nil, err
	}
	if err := run.Defer("provider-directory:unregister", state.dispose); err != nil {
		_ = state.dispose(context.Background())
		return nil, err
	}
	return &ConfigurableProviderRegistration{state: state}, nil
}

func cloneConfigurableProviders(entries []ConfigurableProvider) []ConfigurableProvider {
	result := make([]ConfigurableProvider, len(entries))
	for index, entry := range entries {
		result[index] = cloneConfigurableProvider(entry)
	}
	return result
}

func validateConfigurableProviders(entries []ConfigurableProvider, owner *configurableProviderRegistrationState) ([]configurableProviderRoute, error) {
	seen := make(map[string]struct{}, len(entries))
	prepared := make([]configurableProviderRoute, 0, len(entries))
	for _, source := range entries {
		entry := cloneConfigurableProvider(source)
		if entry.Provider == "" || entry.DisplayName == "" || entry.SettingsNamespace == "" {
			return nil, NewProviderError("configurable providers need a non-empty provider, displayName, and settingsNs", InvalidDirectoryCode, nil)
		}
		for _, segment := range entry.SettingsPath {
			if segment == "" {
				return nil, NewProviderError(
					fmt.Sprintf("configurable provider %q has an empty settingsPath segment", entry.Provider), InvalidDirectoryCode, nil,
				)
			}
		}
		if _, duplicate := seen[entry.Provider]; duplicate {
			return nil, NewProviderError(
				fmt.Sprintf("configurable provider %q is already declared", entry.Provider), DuplicateDirectoryCode, nil,
			)
		}
		seen[entry.Provider] = struct{}{}
		prepared = append(prepared, configurableProviderRoute{owner: owner, entry: entry})
	}
	return prepared, nil
}

func (s *configurableProviderRegistrationState) replace(entries []ConfigurableProvider) error {
	prepared, err := validateConfigurableProviders(entries, s)
	if err != nil {
		return err
	}
	s.mu.Lock()
	if s.released {
		s.mu.Unlock()
		return NewProviderError("this configurable-provider registration was disposed", RegistrationDisposedCode, nil)
	}
	r := s.registry
	r.mu.Lock()
	for _, candidate := range prepared {
		if existing, found := r.directory[candidate.entry.Provider]; found && existing.owner != s {
			r.mu.Unlock()
			s.mu.Unlock()
			return NewProviderError(
				fmt.Sprintf("configurable provider %q is already declared", candidate.entry.Provider), DuplicateDirectoryCode, nil,
			)
		}
	}
	owned := make(map[string]struct{}, len(s.held))
	for _, entry := range s.held {
		owned[entry.Provider] = struct{}{}
		delete(r.directory, entry.Provider)
	}
	nextOrder := make([]string, 0, len(r.directoryOrder)-len(owned)+len(prepared))
	for _, provider := range r.directoryOrder {
		if _, remove := owned[provider]; !remove {
			nextOrder = append(nextOrder, provider)
		}
	}
	s.held = s.held[:0]
	for _, candidate := range prepared {
		r.directory[candidate.entry.Provider] = candidate
		nextOrder = append(nextOrder, candidate.entry.Provider)
		s.held = append(s.held, cloneConfigurableProvider(candidate.entry))
	}
	r.directoryOrder = nextOrder
	snapshot, watchers := r.topologyLocked()
	r.mu.Unlock()
	s.mu.Unlock()
	r.notify(snapshot, watchers)
	return nil
}

func (s *configurableProviderRegistrationState) dispose(context.Context) error {
	s.mu.Lock()
	if s.released {
		s.mu.Unlock()
		return nil
	}
	s.released = true
	r := s.registry
	r.mu.Lock()
	owned := make(map[string]struct{}, len(s.held))
	for _, entry := range s.held {
		owned[entry.Provider] = struct{}{}
		if current, found := r.directory[entry.Provider]; found && current.owner == s {
			delete(r.directory, entry.Provider)
		}
	}
	nextOrder := r.directoryOrder[:0]
	for _, provider := range r.directoryOrder {
		if _, remove := owned[provider]; !remove {
			nextOrder = append(nextOrder, provider)
		}
	}
	r.directoryOrder = nextOrder
	s.held = nil
	snapshot, watchers := r.topologyLocked()
	r.mu.Unlock()
	s.mu.Unlock()
	r.notify(snapshot, watchers)
	return nil
}

func (r *ConfigurableProviderRegistration) Replace(entries []ConfigurableProvider) error {
	if r == nil || r.state == nil {
		return NewProviderError("configurable-provider registration is unavailable", RegistrationDisposedCode, nil)
	}
	return r.state.replace(cloneConfigurableProviders(entries))
}

func (r *ConfigurableProviderRegistration) Dispose(ctx context.Context) error {
	if r == nil || r.state == nil {
		return nil
	}
	return r.state.dispose(ctx)
}

// ListConfigurableProviders returns detached entries in declaration order.
func (r *ProviderRegistry) ListConfigurableProviders() []ConfigurableProvider {
	if r == nil {
		return nil
	}
	r.mu.RLock()
	result := make([]ConfigurableProvider, 0, len(r.directoryOrder))
	for _, provider := range r.directoryOrder {
		if route, found := r.directory[provider]; found {
			result = append(result, cloneConfigurableProvider(route.entry))
		}
	}
	r.mu.RUnlock()
	return result
}

// ModelDiscoveryRequest is one draft endpoint interrogation. APIKey is
// operation-local and is never persisted by ProviderRegistry.
type ModelDiscoveryRequest struct {
	Provider string `json:"provider,omitempty"`
	BaseURL  string `json:"baseURL,omitempty"`
	API      string `json:"api,omitempty"`
	APIKey   string `json:"apiKey,omitempty"`
}

type DiscoveredModel struct {
	ID            string `json:"id"`
	Name          string `json:"name,omitempty"`
	ContextWindow int    `json:"contextWindow,omitempty"`
	MaxTokens     int    `json:"maxTokens,omitempty"`
}

type ModelDiscoveryFunc func(context.Context, ModelDiscoveryRequest) ([]DiscoveredModel, error)

type modelDiscoveryOffer struct {
	id       uint64
	discover ModelDiscoveryFunc
}

// RegisterModelDiscovery offers one effect-owned interrogation per settings
// namespace.
func (r *ProviderRegistry) RegisterModelDiscovery(run *point.Run, settingsNamespace string, discover ModelDiscoveryFunc) (point.Dispose, error) {
	if r == nil || run == nil || !run.Active() {
		return nil, point.ErrInactiveRun
	}
	if settingsNamespace == "" || discover == nil {
		return nil, NewProviderError("model discovery needs a non-empty settings namespace and implementation", InvalidDiscoveryCode, nil)
	}
	r.mu.Lock()
	if _, duplicate := r.discoveries[settingsNamespace]; duplicate {
		r.mu.Unlock()
		return nil, NewProviderError(
			fmt.Sprintf("model discovery for %q is already registered", settingsNamespace), DuplicateDiscoveryCode, nil,
		)
	}
	r.nextDiscoveryID++
	offer := modelDiscoveryOffer{id: r.nextDiscoveryID, discover: discover}
	r.discoveries[settingsNamespace] = offer
	r.mu.Unlock()
	dispose := func(context.Context) error {
		r.mu.Lock()
		if current, found := r.discoveries[settingsNamespace]; found && current.id == offer.id {
			delete(r.discoveries, settingsNamespace)
		}
		r.mu.Unlock()
		return nil
	}
	if err := run.Defer("provider-directory:model-discovery", dispose); err != nil {
		_ = dispose(context.Background())
		return nil, err
	}
	return dispose, nil
}

func (r *ProviderRegistry) DiscoverModels(ctx context.Context, settingsNamespace string, request ModelDiscoveryRequest) ([]DiscoveredModel, error) {
	if r == nil {
		return nil, errors.New("harness: provider registry is nil")
	}
	r.mu.RLock()
	offer, found := r.discoveries[settingsNamespace]
	r.mu.RUnlock()
	if !found {
		return nil, NewProviderError(
			fmt.Sprintf("no model discovery is registered for %q", settingsNamespace), NoDiscoveryCode, nil,
		)
	}
	if request.Provider == "" && request.BaseURL == "" {
		return nil, NewProviderError("model discovery needs a provider route or a baseURL", InvalidDiscoveryCode, nil)
	}
	if ctx == nil {
		ctx = context.Background()
	}
	discovered, err := offer.discover(ctx, request)
	if err != nil {
		return nil, err
	}
	seen := make(map[string]struct{}, len(discovered))
	result := make([]DiscoveredModel, 0, len(discovered))
	for _, model := range discovered {
		if model.ID == "" {
			continue
		}
		if _, duplicate := seen[model.ID]; duplicate {
			continue
		}
		seen[model.ID] = struct{}{}
		result = append(result, model)
	}
	return result, nil
}

// ModelCatalogEntry is advisory provider-owned display metadata. Absence from
// the catalog never rejects an otherwise valid model id.
type ModelCatalogEntry struct {
	Provider        string   `json:"provider"`
	ID              string   `json:"id"`
	Name            string   `json:"name"`
	Description     string   `json:"description,omitempty"`
	InputModalities []string `json:"inputModalities,omitempty"`
	// DescriptionPresent preserves an explicitly supplied empty description.
	// It is data only; ordinary Point JSON omits this presence flag.
	DescriptionPresent bool `json:"-"`
}

type ModelCatalogProvider interface {
	ListModels(context.Context, string) ([]ModelCatalogEntry, error)
}

func (r *ProviderRegistry) ListModels(ctx context.Context, provider string) ([]ModelCatalogEntry, error) {
	selected, err := r.route(provider)
	if err != nil {
		return nil, err
	}
	catalog, ok := selected.adapter.(ModelCatalogProvider)
	if !ok {
		return []ModelCatalogEntry{}, nil
	}
	models, err := catalog.ListModels(ctx, provider)
	if err != nil {
		return nil, err
	}
	seen := make(map[string]struct{}, len(models))
	result := make([]ModelCatalogEntry, len(models))
	for index, model := range models {
		if model.Provider != provider || model.ID == "" || model.Name == "" {
			return nil, NewProviderError(
				fmt.Sprintf("adapter returned invalid model metadata for provider %q", provider), InvalidCatalogCode, nil,
			)
		}
		if _, duplicate := seen[model.ID]; duplicate {
			return nil, NewProviderError(
				fmt.Sprintf("adapter returned duplicate model %q for provider %q", model.ID, provider), InvalidCatalogCode, nil,
			)
		}
		seen[model.ID] = struct{}{}
		model.InputModalities = append([]string(nil), model.InputModalities...)
		result[index] = model
	}
	return result, nil
}
