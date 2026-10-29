package session

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"time"
)

// AdmissionFact is a trusted framework-owned log-only fact that must commit
// atomically with the exact user messages it authorizes. It is not a guest API.
type AdmissionFact struct {
	Type  string
	Value any
}

// AppendUserAdmissionCAS commits a prepared native admission and its messages
// as one log/surface transaction. No callbacks run under the log mutex. Existing
// synchronous subscriber ordering is preserved; subscribers see the whole batch.
// A CAS miss or validation/cancellation error commits nothing. Optional native
// owner lifetimes are checked inside the same commit mutex, after waiting for
// notification delivery. Retirement before that linearization cannot commit.
func (s *Session) AppendUserAdmissionCAS(ctx context.Context, expectedLastSeq uint64, facts []AdmissionFact, messages []Message, lifetimes ...context.Context) ([]Event, error) {
	if s == nil || ctx == nil {
		return nil, errors.New("harness: admission requires session and context")
	}
	prepared := make([]Event, 0, len(facts)+len(messages))
	for _, fact := range facts {
		if fact.Type == "" || isSurfaceEventType(fact.Type) {
			return nil, errors.New("harness: admission facts must be log-only")
		}
		data, err := json.Marshal(fact.Value)
		if err != nil {
			return nil, err
		}
		prepared = append(prepared, Event{Type: fact.Type, Data: data})
	}
	for _, message := range messages {
		if message.Role != RoleUser {
			continue
		}
		data, err := json.Marshal(message)
		if err != nil {
			return nil, err
		}
		prepared = append(prepared, Event{Type: "user/message", Data: data, SurfaceOp: json.RawMessage(`"append"`)})
	}
	s.notifyMu.Lock()
	defer s.notifyMu.Unlock()
	s.mu.Lock()
	if err := ctx.Err(); err != nil {
		s.mu.Unlock()
		return nil, err
	}
	for _, lifetime := range lifetimes {
		if lifetime == nil {
			s.mu.Unlock()
			return nil, errors.New("harness: admission lifetime is required")
		}
		if err := lifetime.Err(); err != nil {
			s.mu.Unlock()
			return nil, err
		}
	}
	if uint64(len(s.events)) != expectedLastSeq {
		s.mu.Unlock()
		return nil, ErrSessionChanged
	}
	candidate := &Session{events: append([]Event(nil), s.events...), surface: append([]uint64(nil), s.surface...)}
	for i := range prepared {
		event := &prepared[i]
		event.Seq = uint64(len(candidate.events) + 1)
		event.Time = time.Now().UTC()
		if err := validateContextPlanEvent(candidate.events, candidate.surface, *event); err != nil {
			s.mu.Unlock()
			return nil, err
		}
		if err := validateCodeDispatchEvent(candidate.events, *event); err != nil {
			s.mu.Unlock()
			return nil, err
		}
		// Admission is deliberately limited to ordinary log facts and user appends;
		// special lifecycle writers retain their own validated APIs.
		if event.Type == "request/context" || event.Type == imageOffloadEventType || isCompactionEventType(event.Type) {
			s.mu.Unlock()
			return nil, fmt.Errorf("harness: %q is not an admission fact", event.Type)
		}
		if err := candidate.applySurfaceEventLocked(*event); err != nil {
			s.mu.Unlock()
			return nil, err
		}
		candidate.events = append(candidate.events, *event)
	}
	s.events, s.surface = candidate.events, candidate.surface
	ids := make([]uint64, 0, len(s.subscribers))
	for id := range s.subscribers {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	subscribers := make([]func(Event), 0, len(ids))
	for _, id := range ids {
		subscribers = append(subscribers, s.subscribers[id])
	}
	s.mu.Unlock()
	for _, event := range prepared {
		for _, subscriber := range subscribers {
			s.notifySubscriber(subscriber, event)
		}
	}
	return prepared, nil
}
