package acp

import (
	"context"
	"errors"
	"github.com/phodal/pero/harness/go/harness"
)

type setConfigOptionParams struct {
	SessionID string `json:"sessionId"`
	ConfigID  string `json:"configId"`
	Value     string `json:"value"`
}

func configuredModelChoices(o Options) ([]harness.ModelCatalogEntry, int, int, error) {
	if !o.ModelSelection {
		return nil, 0, 0, nil
	}
	choices, err := o.Harness.Providers().ListModels(context.Background(), o.Provider)
	if err != nil {
		return nil, 0, 0, err
	}
	if len(choices) == 0 || len(choices) > 33 {
		return nil, 0, 0, errors.New("acp: model selection requires an explicit catalog of 1 to 33 models")
	}
	capacity, output := -1, -1
	found := false
	for _, choice := range choices {
		info, err := o.Harness.Providers().ResolveModelInfo(context.Background(), o.Provider, choice.ID)
		if err != nil {
			return nil, 0, 0, err
		}
		if capacity < 0 {
			capacity, output = info.ContextWindow, info.DefaultMaxTokens
		}
		if info.ContextWindow != capacity || info.DefaultMaxTokens != output {
			return nil, 0, 0, errors.New("acp: selectable models must share declared context and output capacity")
		}
		found = found || choice.ID == o.Model
	}
	if !found {
		return nil, 0, 0, errors.New("acp: default model is absent from the configured catalog")
	}
	if o.Compaction != nil && (capacity <= 0 || (o.Compaction.ContextWindow != 0 && o.Compaction.ContextWindow != capacity)) {
		return nil, 0, 0, errors.New("acp: selectable model compaction requires matching declared capacity")
	}
	return choices, capacity, output, nil
}

func (s *Server) validateModelChoice(ctx context.Context, selection harness.AgentModelSelection) error {
	if len(s.modelChoices) == 0 {
		return invalidError("model selection is not enabled")
	}
	found := false
	for _, choice := range s.modelChoices {
		found = found || (choice.Provider == selection.Provider && choice.ID == selection.Model)
	}
	if !found {
		return invalidError("model is not in the configured catalog")
	}
	info, err := s.h.Providers().ResolveModelInfo(ctx, selection.Provider, selection.Model)
	if err != nil {
		return err
	}
	if info.ContextWindow != s.modelCapacity || info.DefaultMaxTokens != s.modelOutput {
		return invalidError("configured model capacity changed; restart the server")
	}
	return nil
}

func (s *Server) checkRestoredModel(ctx context.Context, session *harness.Session) error {
	if len(s.modelChoices) == 0 {
		return nil
	}
	selection, found, err := harness.RecordedModelSelection(session)
	if err != nil {
		return invalidError("invalid recorded model selection")
	}
	if !found {
		selection = harness.AgentModelSelection{Provider: s.provider, Model: s.model}
	}
	return s.validateModelChoice(ctx, selection)
}

func (s *Server) configOptions(selection harness.AgentModelSelection) []any {
	if len(s.modelChoices) == 0 {
		return []any{}
	}
	options := make([]any, 0, len(s.modelChoices))
	for _, choice := range s.modelChoices {
		options = append(options, map[string]any{"value": choice.ID, "name": choice.Name})
	}
	return []any{map[string]any{"id": "model", "name": "Model", "category": "model", "type": "select", "currentValue": selection.Model, "options": options}}
}

func (s *Server) sessionConfiguration(id harness.SessionID, includeID bool) (any, *rpcError) {
	record, err := s.currentRecord(id)
	if err != nil {
		return nil, classify(err)
	}
	result := map[string]any{"configOptions": s.configOptions(record.agent.ModelSelection())}
	if includeID {
		result["sessionId"] = id
	}
	return result, nil
}

func (s *Server) setConfigOption(ctx context.Context, p setConfigOptionParams) (any, *rpcError) {
	if p.ConfigID != "model" || len(s.modelChoices) == 0 {
		return nil, invalid("unsupported config option")
	}
	record, err := s.currentRecord(harness.SessionID(p.SessionID))
	if err != nil {
		return nil, classify(err)
	}
	record.mu.Lock()
	if record.closing || record.inflight || record.commandActive != 0 || record.agent.Status() != harness.AgentIdle {
		record.mu.Unlock()
		return nil, invalid("model may only change while the session is idle")
	}
	// Reserve the existing command lifecycle receipt so close drains this change.
	// Do not hold record.mu across resolver, session subscribers or persistence:
	// streaming projection reenters record.mu synchronously during Append.
	record.modelChanging = true
	record.commandActive++
	record.commandsIdle = make(chan struct{})
	record.mu.Unlock()
	defer func() {
		record.mu.Lock()
		record.modelChanging = false
		record.commandActive--
		if record.commandActive == 0 {
			close(record.commandsIdle)
		}
		record.mu.Unlock()
	}()
	next := record.agent.ModelSelection()
	next.Provider = s.provider
	next.Model = p.Value
	// Reasoning selection is a provider default, not carried from a different model.
	next.ReasoningEffort = ""
	if err = s.validateModelChoice(ctx, next); err != nil {
		return nil, classify(err)
	}
	err = record.agent.SelectModel(ctx, next)
	options := s.configOptions(record.agent.ModelSelection())
	if err != nil {
		// Append may have succeeded before a durability failure. Never acknowledge
		// rollback or keep using uncertain state; close and require explicit restore.
		s.requestRecordClose(record)
		return nil, classify(err)
	}
	return map[string]any{"configOptions": options}, nil
}
