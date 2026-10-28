package session

import (
	"context"
	"errors"
)

var ErrSessionNotCurrent = errors.New("harness: Session is no longer the current store materialization")

// AppendLogExact commits a framework log-only fact only to the exact current
// Session materialization. Removal and replacement linearize with this commit;
// no store lock remains held during Session observers. This is an in-memory
// acceptance boundary, not a successful storage Flush.
func (s *SessionStore) AppendLogExact(captured *Session, eventType string, value any) (Event, error) {
	return s.AppendLogExactContext(context.Background(), captured, eventType, value)
}

// AppendLogExactContext additionally checks cancellation after acquiring the
// notification and membership lanes, immediately before the bounded commit.
func (s *SessionStore) AppendLogExactContext(ctx context.Context, captured *Session, eventType string, value any) (Event, error) {
	return s.AppendLogExactAdmitted(ctx, captured, eventType, value, nil)
}

// AppendLogExactAdmitted is a framework-only owner-admission boundary. The
// guard runs after encoding/notification admission and before store membership;
// it returns an unlock function held only through the in-memory commit. It must
// not execute user code, encode data, publish events or acquire notification
// lanes. This callback is not a plugin, wire or user-data extension point.
func (s *SessionStore) AppendLogExactAdmitted(ctx context.Context, captured *Session, eventType string, value any, admit func() (func(), error)) (Event, error) {
	if s == nil || captured == nil {
		return Event{}, ErrSessionNotCurrent
	}
	if eventType == "" || isSurfaceEventType(eventType) {
		return Event{}, errors.New("harness: exact store append requires a log-only event")
	}
	return captured.appendAtTimeWithStore(eventType, value, nil, nil, nil, nil, nil, s, ctx, admit)
}
