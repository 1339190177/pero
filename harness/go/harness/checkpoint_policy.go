package harness

import (
	"context"
	"errors"

	point "github.com/phodal/pero/harness/go/runtime"
	harnesssession "github.com/phodal/pero/harness/go/session"
)

type CheckpointPolicySnapshot = harnesssession.CheckpointPolicySnapshot
type CheckpointPolicy = harnesssession.CheckpointPolicy

func NewCheckpointPolicy() *CheckpointPolicy { return harnesssession.NewCheckpointPolicy() }

func (a *Agent) checkpoint(ctx context.Context) error {
	if a == nil || a.session == nil {
		return errors.New("session-checkpoint-policy: checkpoint requires an Agent Session")
	}
	if a.scope != nil {
		if policy, ok := point.Lookup(a.scope.Runtime(), a.scope, CheckpointPolicyKey); ok && !policy.Enabled() {
			return nil
		}
	}
	return a.session.Flush(ctx)
}
