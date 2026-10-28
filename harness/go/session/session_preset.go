package session

import (
	"errors"
	"strings"
)

// ConfigureAgentPreset records a new Session's creation-time composition before
// publication. Fork seeds may already contain events. Restored Sessions retain
// their immutable original header and record later choices as selection events.
func (s *Session) ConfigureAgentPreset(preset string) error {
	if s == nil || preset == "" || strings.TrimSpace(preset) != preset {
		return errors.New("harness: invalid Agent preset")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.published || s.restored.Load() || s.header.AgentPreset != "" {
		return errors.New("harness: Agent preset must be configured before publication")
	}
	s.header.AgentPreset = preset
	return nil
}
