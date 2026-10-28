package session

// LatestEvent reads one detached event without copying the entire transcript.
// It is intended for small log-owned configuration values queried on each step.
func (s *Session) LatestEvent(eventType string) (Event, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for i := len(s.events) - 1; i >= 0; i-- {
		if s.events[i].Type == eventType {
			return cloneEvents(s.events[i : i+1])[0], true
		}
	}
	return Event{}, false
}
