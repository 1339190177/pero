package harness

import (
	"context"
	"fmt"
)

// ExternalTurn is an execution Harness, not a model provider. It owns only the
// external protocol turn; the Agent worker still owns admission and lifecycle.
type ExternalTurn func(context.Context, *Agent, int, []Message) (FinishKind, error)

type ExternalModelChoice struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
}

type ExternalModelState struct {
	Current string                `json:"current"`
	Choices []ExternalModelChoice `json:"choices"`
}

type ExternalModels func(context.Context) (*ExternalModelState, error)
type ExternalSelectModel func(context.Context, string) (*ExternalModelState, error)

func (a *Agent) IsExternal() bool { return a.externalTurn != nil }

func (a *Agent) ExternalModels(ctx context.Context) (*ExternalModelState, error) {
	if a.externalModels == nil {
		return nil, nil
	}
	return a.externalModels(ctx)
}

func (a *Agent) SelectExternalModel(ctx context.Context, id string) (*ExternalModelState, error) {
	if err := a.mutationAdmissionError(); err != nil {
		return nil, err
	}
	if a.externalSelectModel == nil {
		return nil, nil
	}
	return a.externalSelectModel(ctx, id)
}

// ExternalConfigState is a select option advertised by this ACP session.
type ExternalConfigState struct {
	ConfigID    string                `json:"configId"`
	Name        string                `json:"name"`
	Description string                `json:"description,omitempty"`
	Category    string                `json:"category,omitempty"`
	Current     string                `json:"current"`
	Choices     []ExternalModelChoice `json:"choices"`
}
type ExternalConfigs func(context.Context) ([]ExternalConfigState, error)
type ExternalSelectConfig func(context.Context, string, string) ([]ExternalConfigState, error)

func (a *Agent) ExternalConfigs(ctx context.Context) ([]ExternalConfigState, error) {
	if a.externalConfigs == nil {
		return []ExternalConfigState{}, nil
	}
	return a.externalConfigs(ctx)
}
func (a *Agent) SelectExternalConfig(ctx context.Context, id, value string) ([]ExternalConfigState, error) {
	if err := a.mutationAdmissionError(); err != nil {
		return nil, err
	}
	if a.externalSelectConfig == nil {
		return nil, fmt.Errorf("ACP configuration is unavailable")
	}
	return a.externalSelectConfig(ctx, id, value)
}
