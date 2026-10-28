package session

import (
	"errors"
	"fmt"
)

// AppendLogWithPublication is a framework-only ownership publication seam.
// It commits a log-only fact, releases the log mutex, publishes already
// prepared native state, then delivers subscribers in their existing order.
// The bounded publication callback must not invoke producers, listeners,
// Session mutation, or other user code. It runs inside the notification lane,
// but outside the log mutex; its owning registry must not hold its own lock
// while calling this method. No JSON or guest API exposes this callback.
//
// On a precommit error, neither callback has run and the returned Event is empty.
// If publish panics, recoverPublication runs in the same lane before subscribers.
// Both must be idempotent and publish the same prepared state. A nonempty Event
// proves log commitment, even if recovery was needed. If both callbacks panic,
// subscribers are withheld and the explicit error must be handled by the owner.
func (s *Session) AppendLogWithPublication(eventType string, value any, publish, recoverPublication func()) (Event, error) {
	if s == nil || publish == nil || recoverPublication == nil || eventType == "" || isSurfaceEventType(eventType) {
		return Event{}, errors.New("harness: log publication requires a session, log-only event, and publication callbacks")
	}
	var recovered error
	event, err := s.appendAtTimeWithPublication(eventType, value, nil, nil, nil, nil, func() error {
		recovered = publishCommittedFact(publish)
		if recovered != nil {
			return publishCommittedFact(recoverPublication)
		}
		return nil
	})
	return event, errors.Join(recovered, err)
}

func publishCommittedFact(publish func()) (err error) {
	if publish == nil {
		return nil
	}
	defer func() {
		if recovered := recover(); recovered != nil {
			err = fmt.Errorf("harness: committed fact publication panicked: %v", recovered)
		}
	}()
	publish()
	return nil
}
