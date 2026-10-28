package runtime

import (
	"context"
	"errors"
)

// EmitCurrent preserves Emit's serial listener snapshot and fail-fast behavior,
// but obtains the value again immediately before each listener. A preceding
// listener may have invalidated the old value's lifecycle authority. Neither
// the provider nor any listener runs under the registration lock.
func (s *Signal[T]) EmitCurrent(ctx context.Context, current func() T) error {
	if s == nil || current == nil {
		return errors.New("runtime: signal and current value provider must not be nil")
	}
	ctx = eventContext(ctx)
	for _, entry := range s.store.snapshot() {
		err := func() (err error) {
			defer recoverNamedError("signal", s.name, &err)
			return entry.listener(ctx, current())
		}()
		if err != nil {
			return err
		}
	}
	return nil
}

// EmitAllCurrent combines EmitAll's run-every-listener error containment with
// EmitCurrent's fresh value per listener. Provider failures are contained too,
// so a failed snapshot cannot starve later lifecycle cleanup.
func (s *Signal[T]) EmitAllCurrent(ctx context.Context, current func() T) error {
	if s == nil || current == nil {
		return errors.New("runtime: signal and current value provider must not be nil")
	}
	ctx = eventContext(ctx)
	var errs []error
	for _, entry := range s.store.snapshot() {
		err := func() (err error) {
			defer recoverNamedError("signal", s.name, &err)
			return entry.listener(ctx, current())
		}()
		if err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}
