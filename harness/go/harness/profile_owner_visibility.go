package harness

import "context"

// ToolVisible observes the exact committed owner's definition in this Agent's
// actual executable catalog, including authoritative Host proxies, restrictions
// and scoped shadows. It grants no execution authority and borrows no operation.
// The name selects a catalog lookup; only the private lease proves ownership.
func (l *ProfileOwnerLease) ToolVisible(ctx context.Context, tools *ToolRegistry, agent *Agent, name string) (bool, error) {
	if l == nil || tools == nil || agent == nil || agent.tools != tools || agent.retired.Load() || !l.Active() {
		return false, nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return false, err
	}
	selected, found, err := tools.resolveForAgent(ctx, agent, name)
	if err != nil || !found {
		return false, err
	}
	if err := ctx.Err(); err != nil {
		return false, err
	}
	if agent.retired.Load() || !l.Active() || selected.profileLease != l || (selected.capturedAgent != nil && selected.capturedAgent != agent) {
		return false, nil
	}
	if selected.owner != nil && selected.owner.admissionError() != nil {
		return false, nil
	}
	return true, nil
}
