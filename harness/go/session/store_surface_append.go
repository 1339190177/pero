package session

import (
	"context"
	"encoding/json"
	"errors"
)

// These framework-only variants reuse the log commit's notification/store/
// owner lock order. Admission covers memory acceptance, never storage Flush.
func (s *SessionStore) AppendSurfaceExactAdmitted(ctx context.Context, captured *Session, eventType string, value any, sources []uint64, admit func() (func(), error)) (Event, error) {
	return s.appendSurfaceExactAdmitted(ctx, captured, eventType, value, json.RawMessage(`"append"`), sources, admit)
}
func (s *SessionStore) AppendReplacementExactAdmitted(ctx context.Context, captured *Session, eventType string, value any, replaced SurfaceRange, sources []uint64, admit func() (func(), error)) (Event, error) {
	operation, err := json.Marshal(struct {
		Op    string `json:"op"`
		Start uint64 `json:"start"`
		End   uint64 `json:"end"`
	}{"replace", replaced.Start, replaced.End})
	if err != nil {
		return Event{}, err
	}
	return s.appendSurfaceExactAdmitted(ctx, captured, eventType, value, operation, sources, admit)
}
func (s *SessionStore) appendSurfaceExactAdmitted(ctx context.Context, captured *Session, eventType string, value any, operation json.RawMessage, sources []uint64, admit func() (func(), error)) (Event, error) {
	if s == nil || captured == nil {
		return Event{}, ErrSessionNotCurrent
	}
	if !isSurfaceEventType(eventType) {
		return Event{}, errors.New("harness: exact surface append requires a surface event")
	}
	return captured.appendAtTimeWithStore(eventType, value, operation, append([]uint64(nil), sources...), nil, nil, nil, s, ctx, admit)
}
