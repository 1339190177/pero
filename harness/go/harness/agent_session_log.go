package harness

import (
	"context"
	"errors"

	"github.com/phodal/pero/harness/go/internal/sessionlogroute"
)

// AppendAgentLogExact admits one framework log-only fact for an exact live
// Agent and current Session. Its operation is joined by Agent retirement;
// SessionStore membership and cancellation are rechecked at the log commit.
// The returned Event reports memory acceptance, not durable Flush completion.
func (m *AgentManager) AppendAgentLogExact(ctx context.Context, agent *Agent, eventType string, value any) (Event, error) {
	return m.appendAgentLogExact(ctx, agent, eventType, value, false)
}

// AppendAgentLogExactAndFlush keeps the admitted Agent operation alive through
// the configured persistence barrier. A nonzero returned Event with an error
// means memory acceptance succeeded but Flush did not; it is not durable success.
// An unattached in-memory Session has no external storage durability guarantee.
func (m *AgentManager) AppendAgentLogExactAndFlush(ctx context.Context, agent *Agent, eventType string, value any) (Event, error) {
	return m.appendAgentLogExact(ctx, agent, eventType, value, true)
}

func (m *AgentManager) appendAgentLogExact(ctx context.Context, agent *Agent, eventType string, value any, flush bool) (Event, error) {
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
	event, err := m.sessions.AppendLogExactAdmitted(operation.context, agent.Session(), eventType, value, func() (func(), error) {
		// The original provider attempt can expire while notification delivery
		// is blocked, even when Accept supplies a fresh live context. Check its
		// private proof at admission, before holding membership locks.
		if err := sessionlogroute.CheckCommitProof(operation.context); err != nil {
			return nil, err
		}
		m.mu.RLock()
		current, ok := m.agents[agent.ID()]
		if !ok || current.agent != agent || agent.retired.Load() || m.closing || m.disposed {
			m.mu.RUnlock()
			return nil, ErrAgentRetired
		}
		return m.mu.RUnlock, nil
	})
	if err == nil && flush {
		err = flushAcceptedAgentLog(operation.context, agent.Session())
	}
	return event, err
}

// A checkpoint is an external callback boundary. Keep its panic separate from
// the already committed event so callers retain the exact memory acceptance
// receipt, and never invoke methods on the recovered value while reporting it.
func flushAcceptedAgentLog(ctx context.Context, session *Session) (err error) {
	defer func() {
		if recover() != nil {
			err = errors.New("harness: Session-log durability checkpoint panicked")
		}
	}()
	return session.Flush(ctx)
}
