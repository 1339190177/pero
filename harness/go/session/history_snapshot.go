package session

// HistorySnapshot returns detached durable history and its current surface from
// one read boundary. Consumers must not combine independent Events/SurfaceSeqs
// reads when deciding whether a published context remains visible.
func (s *Session) HistorySnapshot() ([]Event, []uint64) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return cloneEvents(s.events), append([]uint64(nil), s.surface...)
}
