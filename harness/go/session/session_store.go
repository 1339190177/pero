package session

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"sync"
)

type SessionStore struct {
	mu          sync.RWMutex
	notifyMu    sync.Mutex
	sessions    map[SessionID]*Session
	subscribers map[uint64]func(SessionStoreEvent)
	nextSubID   uint64
}

type SessionStoreEventKind string

const (
	SessionStoreAdded   SessionStoreEventKind = "added"
	SessionStoreRemoved SessionStoreEventKind = "removed"
)

type SessionStoreEvent struct {
	Kind    SessionStoreEventKind
	Session *Session
}

func NewSessionStore() *SessionStore {
	return &SessionStore{sessions: make(map[SessionID]*Session), subscribers: make(map[uint64]func(SessionStoreEvent))}
}

func (s *SessionStore) Create(id SessionID, cwd string) (*Session, error) {
	if s == nil {
		return nil, errors.New("harness: session store is nil")
	}
	session := NewSession(id, cwd)
	s.notifyMu.Lock()
	defer s.notifyMu.Unlock()
	s.mu.Lock()
	if _, exists := s.sessions[session.header.ID]; exists {
		s.mu.Unlock()
		return nil, fmt.Errorf("harness: session %q already exists", session.header.ID)
	}
	s.sessions[session.header.ID] = session
	session.mu.Lock()
	session.published = true
	session.mu.Unlock()
	listeners := s.storeListenersLocked()
	s.mu.Unlock()
	s.notifyStoreListeners(listeners, SessionStoreEvent{Kind: SessionStoreAdded, Session: session})
	return session, nil
}

// Add publishes an already materialized session, as used by persistence
// resume. The session retains its original header and append-only sequence.
func (s *SessionStore) Add(session *Session) error {
	if s == nil || session == nil {
		return errors.New("harness: session store or session is nil")
	}
	s.notifyMu.Lock()
	defer s.notifyMu.Unlock()
	s.mu.Lock()
	if _, exists := s.sessions[session.header.ID]; exists {
		s.mu.Unlock()
		return fmt.Errorf("harness: session %q already exists", session.header.ID)
	}
	s.sessions[session.header.ID] = session
	session.mu.Lock()
	session.published = true
	session.mu.Unlock()
	listeners := s.storeListenersLocked()
	s.mu.Unlock()
	s.notifyStoreListeners(listeners, SessionStoreEvent{Kind: SessionStoreAdded, Session: session})
	return nil
}

func (s *SessionStore) Get(id SessionID) (*Session, bool) {
	if s == nil {
		return nil, false
	}
	s.mu.RLock()
	session, ok := s.sessions[id]
	s.mu.RUnlock()
	return session, ok
}

// List returns a detached, deterministic snapshot of the currently
// materialized Sessions. The Session handles remain concurrency-safe; callers
// must use their snapshot methods rather than retaining internal event slices.
func (s *SessionStore) List() []*Session {
	if s == nil {
		return nil
	}
	s.mu.RLock()
	result := make([]*Session, 0, len(s.sessions))
	for _, session := range s.sessions {
		result = append(result, session)
	}
	s.mu.RUnlock()
	sort.Slice(result, func(i, j int) bool {
		left, right := result[i].Header(), result[j].Header()
		if !left.CreatedAt.Equal(right.CreatedAt) {
			return left.CreatedAt.After(right.CreatedAt)
		}
		return left.ID < right.ID
	})
	return result
}

func (s *SessionStore) Remove(id SessionID) bool {
	return s.remove(id, nil)
}

// RemoveExact withdraws only this materialization. Delayed rollback must not
// remove a different Session that has since been published under the same ID.
func (s *SessionStore) RemoveExact(session *Session) bool {
	if session == nil {
		return false
	}
	return s.remove(session.Header().ID, session)
}

func (s *SessionStore) remove(id SessionID, expected *Session) bool {
	if s == nil {
		return false
	}
	s.notifyMu.Lock()
	defer s.notifyMu.Unlock()
	s.mu.Lock()
	session, exists := s.sessions[id]
	if expected != nil && session != expected {
		s.mu.Unlock()
		return false
	}
	delete(s.sessions, id)
	listeners := s.storeListenersLocked()
	s.mu.Unlock()
	if exists {
		s.notifyStoreListeners(listeners, SessionStoreEvent{Kind: SessionStoreRemoved, Session: session})
	}
	return exists
}

// Subscribe observes exact Session publication and removal in mutation order.
// The listener runs after the store mutation is visible and must not mutate the
// same SessionStore synchronously.
func (s *SessionStore) Subscribe(listener func(SessionStoreEvent)) func() {
	if s == nil || listener == nil {
		return func() {}
	}
	s.mu.Lock()
	if s.subscribers == nil {
		s.subscribers = make(map[uint64]func(SessionStoreEvent))
	}
	s.nextSubID++
	id := s.nextSubID
	s.subscribers[id] = listener
	s.mu.Unlock()
	var once sync.Once
	return func() {
		once.Do(func() {
			s.mu.Lock()
			delete(s.subscribers, id)
			s.mu.Unlock()
		})
	}
}

func (s *SessionStore) storeListenersLocked() []func(SessionStoreEvent) {
	ids := make([]uint64, 0, len(s.subscribers))
	for id := range s.subscribers {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	listeners := make([]func(SessionStoreEvent), 0, len(ids))
	for _, id := range ids {
		listeners = append(listeners, s.subscribers[id])
	}
	return listeners
}

func (s *SessionStore) notifyStoreListeners(listeners []func(SessionStoreEvent), event SessionStoreEvent) {
	for _, listener := range listeners {
		func() {
			defer func() {
				if recovered := recover(); recovered != nil {
					slog.Default().Warn("session store subscriber panicked after mutation", "kind", event.Kind, "session", event.Session.Header().ID, "panic", recovered)
				}
			}()
			listener(event)
		}()
	}
}

func (s *SessionStore) Dispose(context.Context) error {
	s.notifyMu.Lock()
	defer s.notifyMu.Unlock()
	s.mu.Lock()
	sessions := make([]*Session, 0, len(s.sessions))
	for _, session := range s.sessions {
		sessions = append(sessions, session)
	}
	sort.Slice(sessions, func(i, j int) bool { return sessions[i].Header().ID < sessions[j].Header().ID })
	s.sessions = make(map[SessionID]*Session)
	listeners := s.storeListenersLocked()
	s.mu.Unlock()
	for _, session := range sessions {
		s.notifyStoreListeners(listeners, SessionStoreEvent{Kind: SessionStoreRemoved, Session: session})
	}
	return nil
}
