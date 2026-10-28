// Package llm owns provider-neutral model metadata and failures. It depends on
// the shared value model, never on the harness composition root.
package llm

import harnessmodel "github.com/phodal/pero/go/agent/model"

// ModelInfo is adapter-owned metadata for one exact provider/model route.
// ContextWindow is zero when the adapter does not advertise a capacity.
type ModelInfo struct {
	// Conversation update modes are admitted by the exact prepared adapter
	// generation. Empty means the capability is absent.
	SystemPromptUpdate string              `json:"systemPromptUpdate,omitempty"`
	ToolUpdate         string              `json:"toolUpdate,omitempty"`
	Provider           string              `json:"provider"`
	Model              string              `json:"model"`
	Name               string              `json:"name,omitempty"`
	Description        string              `json:"description,omitempty"`
	InputModalities    []string            `json:"inputModalities,omitempty"`
	ContextWindow      int                 `json:"contextWindow,omitempty"`
	DefaultMaxTokens   int                 `json:"defaultMaxTokens,omitempty"`
	Reasoning          *ModelReasoningInfo `json:"reasoning,omitempty"`
	// DescriptionPresent preserves an explicitly supplied empty description.
	// It is data only; ordinary JSON omits this presence flag.
	DescriptionPresent bool `json:"-"`
}

// ModelReasoningInfo is adapter-owned metadata for selectable reasoning
// effort ids. Nil means the adapter does not publish enough information to
// validate an effort locally; a non-nil empty list means none are supported.
type ModelReasoningInfo struct {
	Efforts       []ReasoningEffortInfo        `json:"efforts"`
	DefaultEffort harnessmodel.ReasoningEffort `json:"defaultEffort,omitempty"`
}

type ReasoningEffortInfo struct {
	ID          harnessmodel.ReasoningEffort `json:"id"`
	Name        string                       `json:"name"`
	Description string                       `json:"description,omitempty"`
	// DescriptionPresent preserves an explicitly supplied empty description.
	// It is data only; ordinary JSON omits this presence flag.
	DescriptionPresent bool `json:"-"`
}
