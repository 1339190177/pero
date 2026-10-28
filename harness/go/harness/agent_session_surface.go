package harness

import "context"

// AppendAgentSurfaceExact and AppendAgentReplacementExact commit only while
// this exact Agent and Session still own admission. The receipt is memory
// acceptance; callers must separately establish any required Flush barrier.
func (m *AgentManager) AppendAgentSurfaceExact(ctx context.Context, agent *Agent, eventType string, value any, sources []uint64) (Event, error) {
	return m.appendAgentSurfaceExact(ctx, agent, eventType, value, nil, sources)
}
func (m *AgentManager) AppendAgentReplacementExact(ctx context.Context, agent *Agent, eventType string, value any, replaced SurfaceRange, sources []uint64) (Event, error) {
	return m.appendAgentSurfaceExact(ctx, agent, eventType, value, &replaced, sources)
}
func (m *AgentManager) appendAgentSurfaceExact(ctx context.Context, agent *Agent, eventType string, value any, replaced *SurfaceRange, sources []uint64) (Event, error) {
	if m == nil || agent == nil {
		return Event{}, ErrAgentRetired
	}
	m.mu.RLock()
	current, ok := m.agents[agent.ID()]
	if !ok || current.agent != agent || m.closing || m.disposed {
		m.mu.RUnlock()
		return Event{}, ErrAgentRetired
	}
	operation, err := agent.beginOperation(ctx)
	m.mu.RUnlock()
	if err != nil {
		return Event{}, err
	}
	defer operation.release()
	admit := func() (func(), error) {
		m.mu.RLock()
		current, ok := m.agents[agent.ID()]
		if !ok || current.agent != agent || agent.retired.Load() || m.closing || m.disposed {
			m.mu.RUnlock()
			return nil, ErrAgentRetired
		}
		return m.mu.RUnlock, nil
	}
	if replaced != nil {
		return m.sessions.AppendReplacementExactAdmitted(operation.context, agent.Session(), eventType, value, *replaced, sources, admit)
	}
	return m.sessions.AppendSurfaceExactAdmitted(operation.context, agent.Session(), eventType, value, sources, admit)
}
