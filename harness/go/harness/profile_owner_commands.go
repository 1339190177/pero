package harness

import (
	"context"

	point "github.com/phodal/pero/harness/go/runtime"
)

func (command CommandDefinition) ownerAvailable() bool {
	return (command.profileLease == nil || command.profileLease.Active()) && (command.available == nil || command.available())
}
func (command CommandDefinition) beginOwnerOperation(ctx context.Context) (context.Context, func(), error) {
	if !command.ownerAvailable() {
		return ctx, nil, ErrProfileOwnerUnavailable
	}
	ctx, release, err := command.profileLease.BeginOperation(ctx)
	if err != nil {
		return ctx, nil, err
	}
	if command.admit == nil {
		return ctx, release, nil
	}
	ctx, releaseDependency, err := command.admit(ctx)
	if err != nil {
		release()
		return ctx, nil, err
	}
	return ctx, func() { releaseDependency(); release() }, nil
}

// ManagedCompactCommandDefinition retains command-compact's independent intent
// while its DSH backend is absent. The predicate and operation borrower are
// private native composition, never profile JSON or provider-authored callbacks.
// Generic Point composition without the DSH managed realm retains its command.
func (h *Harness) ManagedCompactCommandDefinition() CommandDefinition {
	command := CompactCommandDefinition()
	root := func() *ManagedCompactionDefaults {
		defaults, found := point.Lookup(h.Runtime(), h.Runtime().Root(), basicCompactionDefaultsKey)
		if !found {
			return nil
		}
		return defaults.managed
	}
	command.available = func() bool { current := root(); return current == nil || current.available() }
	command.admit = func(ctx context.Context) (context.Context, func(), error) {
		current := root()
		if current == nil {
			return ctx, func() {}, nil
		}
		ctx, _, release, err := current.acquire(ctx)
		return ctx, release, err
	}
	handler := command.Handler
	// Direct captures also retain the backend through the actual callback body.
	command.Handler = func(ctx context.Context, in CommandInvocation) (CommandResult, error) {
		ctx, release, err := command.admit(ctx)
		if err != nil {
			return CommandResult{}, err
		}
		defer release()
		return handler(ctx, in)
	}
	return command
}
