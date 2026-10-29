package harness

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	point "github.com/phodal/pero/harness/go/runtime"
)

const AgentDefaultModelSettingsNamespace = "agent-default-model"

var AgentDefaultModelKey = point.NewKey[*AgentDefaultModel]("harness.agent-default-model")

type AgentModelSelection struct {
	Provider        string          `json:"provider"`
	Model           string          `json:"model"`
	ReasoningEffort ReasoningEffort `json:"reasoningEffort,omitempty"`
}

type AgentDefaultModelOptions struct {
	EntryID  string
	Provider string
	Model    string
}

// AgentDefaultModel owns the process-wide selection sampled by newly created
// Agents that do not carry an explicit route. Its settings section is live;
// existing Agents retain the selection already captured on their handle.
type AgentDefaultModel struct {
	base     AgentModelSelection
	scope    *SettingsScope[AgentModelSelection]
	writable bool
}

func (s *AgentDefaultModel) CurrentSelection() AgentModelSelection {
	if s == nil {
		return AgentModelSelection{}
	}
	if s.scope != nil {
		if current, err := s.scope.Get(); err == nil {
			return current
		}
	}
	return s.base
}

// SaveSelection replaces the complete user selection. Without a durable
// settings backend it is intentionally a no-op, matching DSH's optional
// settings-provider contract.
func (s *AgentDefaultModel) SaveSelection(ctx context.Context, next AgentModelSelection) error {
	if err := validateAgentModelSelection(next); err != nil {
		return err
	}
	if s == nil || s.scope == nil || !s.writable {
		return nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	return s.scope.Replace(ctx, next, nil)
}

// Watch observes committed selection changes in settings order. The watcher is
// owned by run and is removed before that effect finishes disposing.
func (s *AgentDefaultModel) Watch(run *point.Run, callback func(AgentModelSelection, AgentModelSelection)) (point.Dispose, error) {
	if s == nil || s.scope == nil {
		return nil, errors.New("harness: default model is unavailable")
	}
	return s.scope.Watch(run, callback)
}

type AgentDefaultModelContribution struct {
	instance *point.Instance
	service  *AgentDefaultModel
}

func (c *AgentDefaultModelContribution) Service() *AgentDefaultModel {
	if c == nil {
		return nil
	}
	return c.service
}

func (c *AgentDefaultModelContribution) Dispose(ctx context.Context) error {
	if c == nil || c.instance == nil {
		return nil
	}
	return c.instance.Dispose(ctx)
}

func MountAgentDefaultModel(ctx context.Context, h *Harness, options AgentDefaultModelOptions) (*AgentDefaultModelContribution, error) {
	if h == nil || h.runtime == nil {
		return nil, errors.New("harness: default model requires a running harness")
	}
	base := AgentModelSelection{Provider: options.Provider, Model: options.Model}
	if err := validateAgentModelSelection(base); err != nil {
		return nil, fmt.Errorf("harness: default model: %w", err)
	}
	entryID := options.EntryID
	if entryID == "" {
		entryID = "agent-default-model"
	}
	var service *AgentDefaultModel
	definition := point.Define(
		point.Descriptor{
			ID: "harness/contribution/agent-default-model", Requires: []point.Requirement{point.Require(SettingsProviderKey)},
			Provides: []point.AnyKey{AgentDefaultModelKey.Any()},
		},
		point.DecodeJSON[struct{}],
		func(run *point.Run, _ struct{}) (point.Dispose, error) {
			settings, err := point.Resolve(run, SettingsProviderKey)
			if err != nil {
				return nil, err
			}
			scope, err := RegisterSettingsSection(settings, run, AgentDefaultModelSettingsNamespace, SettingsSectionSpec[AgentModelSelection]{
				Defaults: map[string]any{}, Base: base, Applies: SettingsLive,
				Schema:   json.RawMessage(`{"type":"object","properties":{"provider":{"type":"string","minLength":1},"model":{"type":"string","minLength":1},"reasoningEffort":{"type":"string","minLength":1}},"required":["provider","model"],"additionalProperties":false}`),
				Validate: validateAgentModelSelection,
			})
			if err != nil {
				return nil, err
			}
			service = &AgentDefaultModel{base: base, scope: scope, writable: settings.Writable()}
			_, err = point.Provide(run, AgentDefaultModelKey, service)
			return nil, err
		},
	)
	instance, err := h.runtime.Mount(nil, entryID, definition, nil)
	if err != nil {
		return nil, err
	}
	if err = instance.WaitStable(ctx); err != nil {
		_ = instance.Dispose(context.Background())
		return nil, err
	}
	if snapshot := instance.Snapshot(); snapshot.State == point.Failed {
		_ = instance.Dispose(context.Background())
		return nil, fmt.Errorf("harness: default model activation failed: %s", snapshot.LastError)
	}
	if service == nil {
		_ = instance.Dispose(context.Background())
		return nil, errors.New("harness: default model failed to publish")
	}
	return &AgentDefaultModelContribution{instance: instance, service: service}, nil
}

func validateAgentModelSelection(value AgentModelSelection) error {
	if strings.TrimSpace(value.Provider) == "" || strings.TrimSpace(value.Model) == "" {
		return errors.New("provider and model must be non-empty strings")
	}
	if value.Provider != strings.TrimSpace(value.Provider) || value.Model != strings.TrimSpace(value.Model) {
		return errors.New("provider and model must be normalized")
	}
	if value.ReasoningEffort != "" && string(value.ReasoningEffort) != strings.TrimSpace(string(value.ReasoningEffort)) {
		return errors.New("reasoning effort must be normalized")
	}
	return nil
}
