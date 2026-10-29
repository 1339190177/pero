package harness

import "errors"

func (m *AgentManager) reserveAgentActivation(id SessionID, parent *Agent) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closing || m.disposed {
		return ErrAgentManagerDisposed
	}
	if _, ok := m.agents[id]; ok {
		return errors.New("harness: session is already active or activating")
	}
	if _, ok := m.activating[id]; ok {
		return errors.New("harness: session is already active or activating")
	}
	for agent := range m.retiring {
		if agent.ID() == id {
			return errors.New("harness: session is already active or activating")
		}
	}
	if m.activating == nil {
		m.activating = make(map[SessionID]*agentActivation)
	}
	m.activating[id] = &agentActivation{parent: parent, done: make(chan struct{})}
	return nil
}
func (m *AgentManager) releaseAgentActivation(id SessionID) {
	m.mu.Lock()
	if activation := m.activating[id]; activation != nil {
		if m.closing {
			m.terminalErr = errors.Join(m.terminalErr, activation.cleanupErr)
		}
		close(activation.done)
	}
	delete(m.activating, id)
	m.signalLifecycleLocked()
	m.mu.Unlock()
}
