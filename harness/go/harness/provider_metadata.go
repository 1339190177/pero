package harness

import (
	"context"
	"fmt"

	point "github.com/phodal/pero/harness/go/runtime"
)

// ModelMetadataPublication is a trusted owner's data-only publication. Only
// exact records also present in Catalog for the same provider are readable.
// Registration detaches the supported fields; it never calls a model service.
type ModelMetadataPublication struct {
	Catalog []ModelCatalogEntry
	Models  []ModelInfo
}

// ProviderRouteReference is an opaque identity for one native route generation.
// It retains no adapter, registration state, registry, or dispatch capability.
type ProviderRouteReference struct {
	provider   string
	owner      *byte
	generation *byte
}

// ProviderExport is detached topology and its matching native route identity.
type ProviderExport struct {
	Info        ProviderInfo
	RetryPolicy ResolvedRetryPolicy
	Route       ProviderRouteReference `json:"-"`
}

// ProviderExportSnapshot captures topology and route proofs in one registry read.
// Revision advances on every successful route replacement or first disposal.
type ProviderExportSnapshot struct {
	Revision  uint64
	Providers []ProviderExport
}

// ExportSnapshot reads only committed data. It does not invoke provider, retry,
// catalog, resolver, preparation, or other adapter callbacks.
func (r *ProviderRegistry) ExportSnapshot() ProviderExportSnapshot {
	if r == nil {
		return ProviderExportSnapshot{}
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	result := ProviderExportSnapshot{Revision: r.revision, Providers: make([]ProviderExport, 0, len(r.order))}
	for _, provider := range r.order {
		if route, found := r.routes[provider]; found {
			result.Providers = append(result.Providers, ProviderExport{
				Info: route.info, RetryPolicy: cloneRetryPolicy(route.policy),
				Route: ProviderRouteReference{provider: provider, owner: route.owner.identity, generation: route.generation},
			})
		}
	}
	return result
}

// RegisterWithModelMetadata atomically publishes the adapter and its validated
// catalog-eligible exact metadata. Existing Register does not enroll metadata.
func (r *ProviderRegistry) RegisterWithModelMetadata(run *point.Run, providers []string, adapter Adapter, publication ModelMetadataPublication) (*ProviderRegistration, error) {
	return r.register(run, providers, adapter, &publication)
}

// ReplaceAdapterWithModelMetadata commits one adapter generation and its
// metadata together. Any validation or route conflict leaves both unchanged.
func (r *ProviderRegistration) ReplaceAdapterWithModelMetadata(providers []string, adapter Adapter, publication ModelMetadataPublication) error {
	if r == nil || r.state == nil {
		return NewProviderError("provider registration is unavailable", RegistrationDisposedCode, nil)
	}
	return r.state.replaceAdapterWithModelMetadata(append([]string(nil), providers...), adapter, &publication)
}

// PublishedModelInfo is a passive exact lookup for an already exported route.
// The caller must independently authorize the expected reference in its scope.
// Stale or missing routes fail closed; neither registry fallback nor an adapter
// callback can supply metadata. An eligible route with no published exact entry
// returns identity only. The detached result linearizes at a final cancellation
// channel check while the route read lock is held. Context methods are invoked
// outside that lock because callers can supply reentrant implementations.
func (r *ProviderRegistry) PublishedModelInfo(ctx context.Context, expected ProviderRouteReference, model string) (ModelInfo, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return ModelInfo{}, err
	}
	if r == nil || expected.provider == "" || expected.owner == nil || expected.generation == nil {
		return ModelInfo{}, NewProviderError("published model metadata route is unavailable", NoAdapterCode, nil)
	}
	if model == "" {
		return ModelInfo{}, NewProviderError("published model metadata needs a non-empty model id", "INVALID_MODEL_INFO", nil)
	}
	done := ctx.Done()
	r.mu.RLock()
	route, found := r.routes[expected.provider]
	if !found || route.owner.identity != expected.owner || route.generation != expected.generation {
		r.mu.RUnlock()
		return ModelInfo{}, NewProviderError("published model metadata route is unavailable", NoAdapterCode, nil)
	}
	result := ModelInfo{Provider: expected.provider, Model: model, Name: model}
	if info, published := route.modelMetadata[model]; published {
		result = clonePublishedModelInfo(info)
	}
	select {
	case <-done:
		r.mu.RUnlock()
		err := ctx.Err()
		if err == nil {
			err = context.Canceled
		}
		return ModelInfo{}, err
	default:
		r.mu.RUnlock()
		return result, nil
	}
}

func preparePublishedModels(providers []string, publication *ModelMetadataPublication) (map[string]map[string]ModelInfo, error) {
	if publication == nil {
		return nil, nil
	}
	allowed := make(map[string]struct{}, len(providers))
	for _, provider := range providers {
		allowed[provider] = struct{}{}
	}
	type modelID struct{ provider, model string }
	catalog := make(map[modelID]struct{}, len(publication.Catalog))
	for _, entry := range publication.Catalog {
		if err := validatePublishedModelIdentity(allowed, entry.Provider, entry.ID, entry.Name); err != nil {
			return nil, err
		}
		id := modelID{entry.Provider, entry.ID}
		if _, duplicate := catalog[id]; duplicate {
			return nil, invalidModelPublication("duplicate catalog model %q for provider %q", entry.ID, entry.Provider)
		}
		if err := validatePublishedModalities(entry.Provider, entry.ID, entry.InputModalities); err != nil {
			return nil, err
		}
		catalog[id] = struct{}{}
	}
	seen := make(map[modelID]struct{}, len(publication.Models))
	result := make(map[string]map[string]ModelInfo)
	for _, source := range publication.Models {
		if err := validatePublishedModelIdentity(allowed, source.Provider, source.Model, source.Name); err != nil {
			return nil, err
		}
		id := modelID{source.Provider, source.Model}
		if _, duplicate := seen[id]; duplicate {
			return nil, invalidModelPublication("duplicate exact model %q for provider %q", source.Model, source.Provider)
		}
		seen[id] = struct{}{}
		if source.ContextWindow < 0 || source.DefaultMaxTokens < 0 {
			return nil, invalidModelPublication("model %q for provider %q has a negative context window or token limit", source.Model, source.Provider)
		}
		if err := validatePublishedModalities(source.Provider, source.Model, source.InputModalities); err != nil {
			return nil, err
		}
		info := clonePublishedModelInfo(source)
		if err := validateModelReasoningInfo(info); err != nil {
			return nil, invalidModelPublication("%v", err)
		}
		if _, eligible := catalog[id]; !eligible {
			continue
		}
		if result[info.Provider] == nil {
			result[info.Provider] = make(map[string]ModelInfo)
		}
		result[info.Provider][info.Model] = info
	}
	return result, nil
}

func invalidModelPublication(format string, args ...any) error {
	return NewProviderError("invalid model metadata publication: "+fmt.Sprintf(format, args...), "INVALID_MODEL_INFO", nil)
}

func validatePublishedModelIdentity(allowed map[string]struct{}, provider, model, name string) error {
	if _, found := allowed[provider]; !found || provider == "" || model == "" || name == "" {
		return invalidModelPublication("model %q must name a registered provider, a non-empty id, and a non-empty display name", model)
	}
	return nil
}

func validatePublishedModalities(provider, model string, modalities []string) error {
	seen := make(map[string]struct{}, len(modalities))
	for _, modality := range modalities {
		if modality == "" {
			return invalidModelPublication("model %q for provider %q has an empty input modality", model, provider)
		}
		if _, duplicate := seen[modality]; duplicate {
			return invalidModelPublication("model %q for provider %q has duplicate input modality %q", model, provider, modality)
		}
		seen[modality] = struct{}{}
	}
	return nil
}

// Keep this projection explicit. Prepared-call update capabilities and future
// ModelInfo fields do not automatically become passive metadata permissions.
func clonePublishedModelInfo(source ModelInfo) ModelInfo {
	result := ModelInfo{
		Provider: source.Provider, Model: source.Model, Name: source.Name,
		Description: source.Description, DescriptionPresent: source.DescriptionPresent,
		InputModalities: append([]string(nil), source.InputModalities...),
		ContextWindow:   source.ContextWindow, DefaultMaxTokens: source.DefaultMaxTokens,
	}
	if source.Reasoning != nil {
		result.Reasoning = &ModelReasoningInfo{
			DefaultEffort: source.Reasoning.DefaultEffort,
			Efforts:       make([]ReasoningEffortInfo, len(source.Reasoning.Efforts)),
		}
		for index, effort := range source.Reasoning.Efforts {
			result.Reasoning.Efforts[index] = ReasoningEffortInfo{
				ID: effort.ID, Name: effort.Name, Description: effort.Description,
				DescriptionPresent: effort.DescriptionPresent,
			}
		}
	}
	return result
}
