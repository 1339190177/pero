package llm

import (
	"context"
	"fmt"

	harnessmodel "github.com/phodal/pero/harness/go/model"
)

// ValidateReasoningEffort rejects only when the adapter published an
// authoritative effort catalog. Unknown metadata remains provider-owned.
func ValidateReasoningEffort(info ModelInfo, effort harnessmodel.ReasoningEffort) error {
	if effort == "" || info.Reasoning == nil {
		return nil
	}
	for _, candidate := range info.Reasoning.Efforts {
		if candidate.ID == effort {
			return nil
		}
	}
	return fmt.Errorf("harness: model %s/%s does not support reasoning effort %q", info.Provider, info.Model, effort)
}

func ValidateModelReasoningInfo(info ModelInfo) error {
	if info.SystemPromptUpdate != "" && info.SystemPromptUpdate != "in-history" {
		return NewProviderError(fmt.Sprintf("adapter returned invalid system prompt update mode for provider %q model %q", info.Provider, info.Model), "INVALID_MODEL_INFO", nil)
	}
	if info.ToolUpdate != "" && info.ToolUpdate != "in-history" && info.ToolUpdate != "addition-only" {
		return NewProviderError(fmt.Sprintf("adapter returned invalid tool update mode for provider %q model %q", info.Provider, info.Model), "INVALID_MODEL_INFO", nil)
	}
	if info.Reasoning == nil {
		return nil
	}
	seen := make(map[harnessmodel.ReasoningEffort]struct{}, len(info.Reasoning.Efforts))
	for _, effort := range info.Reasoning.Efforts {
		if effort.ID == "" || effort.Name == "" {
			return fmt.Errorf("harness: model %s/%s advertises an invalid reasoning effort", info.Provider, info.Model)
		}
		if _, duplicate := seen[effort.ID]; duplicate {
			return fmt.Errorf("harness: model %s/%s advertises duplicate reasoning effort %q", info.Provider, info.Model, effort.ID)
		}
		seen[effort.ID] = struct{}{}
	}
	if info.Reasoning.DefaultEffort != "" {
		if _, found := seen[info.Reasoning.DefaultEffort]; !found {
			return fmt.Errorf("harness: model %s/%s advertises unknown default reasoning effort %q", info.Provider, info.Model, info.Reasoning.DefaultEffort)
		}
	}
	return nil
}

func CloneModelInfo(info ModelInfo) ModelInfo {
	info.InputModalities = append([]string(nil), info.InputModalities...)
	if info.Reasoning != nil {
		reasoning := *info.Reasoning
		reasoning.Efforts = append([]ReasoningEffortInfo(nil), info.Reasoning.Efforts...)
		info.Reasoning = &reasoning
	}
	return info
}

// ModelInfoResolver resolves metadata through the adapter that owns a route.
type ModelInfoResolver interface {
	ResolveModelInfo(context.Context, string, string) (ModelInfo, error)
}

type ModelInfoResolverFunc func(context.Context, string, string) (ModelInfo, error)

func (fn ModelInfoResolverFunc) ResolveModelInfo(ctx context.Context, provider, model string) (ModelInfo, error) {
	return fn(ctx, provider, model)
}
