package harness

import "encoding/json"

// CompositionAgentPreset is the ordinary preset mounted for this process
// identity. A public fork composes the source's current preset before publication,
// even when its exact inherited cut records an older selection. Restored Agents
// have no initial override and replay the persisted header and complete log.
// This selector conveys composition identity, never permission authority.
func (a *Agent) CompositionAgentPreset() string {
	selected := a.session.Header().AgentPreset
	first := uint64(0)
	if a.initialAgentPreset != "" {
		selected = a.initialAgentPreset
		first = a.session.FirstLiveSeq()
	}
	for _, event := range a.session.Events() {
		if event.Type != "agent-preset/selected" || event.Seq < first {
			continue
		}
		var payload struct {
			AgentPreset string `json:"agentPreset"`
		}
		if json.Unmarshal(event.Data, &payload) == nil && payload.AgentPreset != "" {
			selected = payload.AgentPreset
		}
	}
	return selected
}
