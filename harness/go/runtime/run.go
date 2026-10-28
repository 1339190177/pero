package runtime

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"
)

// Run is one activation of a stable Instance. It becomes invalid before
// cleanup starts; registration through an inactive Run fails closed.
type Run struct {
	Context  context.Context
	Scope    *Scope
	Instance *Instance

	activation    *activation
	effects       *EffectGroup
	beforeCleanup *EffectGroup
	active        atomic.Bool
	readable      atomic.Bool

	// Per-Run ownership prevents an old activation from tagging its successor.
	workCancelRequested atomic.Bool
}

func (r *Run) Active() bool { return r != nil && r.active.Load() }

// Logger returns the configured structured logger with stable lifecycle
// identity fields attached.
func (r *Run) Logger() *slog.Logger {
	if r == nil || r.Instance == nil || r.Instance.runtime == nil {
		return slog.Default()
	}
	return r.Instance.runtime.logger.With(
		"agent.instance.id", r.Instance.id,
		"agent.entry.id", r.Instance.entryID,
		"agent.definition.id", r.Instance.definition.descriptor.ID,
		"agent.activation.id", r.activation.id,
	)
}

func (r *Run) readableDuringCleanup() bool {
	return r != nil && r.readable.Load()
}

// CancelWork signals this exact activation's work contexts without releasing
// its resources. An owning lifecycle must still Dispose the Instance. A task
// may report a failure after cancellation and start cleanup independently;
// BeforeCleanup barriers are therefore required for multi-phase owners.
// A subsequent completed stop receipt for this exact activation remains
// available to Dispose until an explicit Instance restart or configuration
// update discards this failed generation. Startup failure semantics are unchanged.
func (r *Run) CancelWork() {
	if r != nil && r.activation != nil {
		// Publish ownership before cancellation can make a helper fail.
		r.workCancelRequested.Store(true)
		r.activation.cancel()
	}
}

// BeforeCleanup adds an effect-owned drain barrier after activation context
// cancellation and before provider draining or ordinary effects release. It is
// intended for framework-owned work that is not itself a Run.Go task. Barriers
// run in reverse registration order and all errors/panics are retained. A
// barrier must not synchronously Dispose/Wait its own Instance or Runtime.
// Independent EffectHandle.Stop/local Effect rollback retains its own resource
// ownership; callers must cancel/join work borrowing those local resources.
func (r *Run) BeforeCleanup(label string, dispose Dispose) error {
	if !r.Active() {
		return ErrInactiveRun
	}
	_, err := r.beforeCleanup.add(label, dispose)
	return err
}

func (r *Run) Defer(label string, dispose Dispose) error {
	if !r.Active() {
		return ErrInactiveRun
	}
	_, err := r.effects.add(label, dispose)
	return err
}

func (r *Run) Effect(label string, start func(*EffectScope) error) (EffectHandle, error) {
	if !r.Active() {
		return nil, ErrInactiveRun
	}
	child := newEffectGroup(label)
	entry, err := r.effects.addChild(label, child)
	if err != nil {
		return nil, err
	}
	handle := &effectHandle{entry: entry, group: child}
	if start == nil {
		return handle, nil
	}
	if err := callEffectStart(label, start, &EffectScope{group: child}); err != nil {
		cleanupErr := child.stop(context.Background())
		return nil, errors.Join(err, cleanupErr)
	}
	return handle, nil
}

func callEffectStart(label string, start func(*EffectScope) error, scope *EffectScope) (err error) {
	defer recoverError("effect start "+label, &err)
	return start(scope)
}

// Go starts a goroutine owned by this activation. Cleanup cancels it and waits
// for completion. A non-cancellation error fails the owning Instance.
func (r *Run) Go(label string, fn func(context.Context) error) error {
	if fn == nil {
		return nil
	}
	_, err := r.Effect(label, func(scope *EffectScope) error {
		taskContext, cancel := context.WithCancel(r.Context)
		done := make(chan struct{})
		var taskErr error
		var taskErrMu sync.Mutex

		if err := scope.Defer(label+"/wait", func(ctx context.Context) error {
			select {
			case <-done:
				taskErrMu.Lock()
				err := taskErr
				taskErrMu.Unlock()
				return err
			case <-ctx.Done():
				return fmt.Errorf("waiting for goroutine %q: %w", label, ctx.Err())
			}
		}); err != nil {
			cancel()
			return err
		}
		if err := scope.Defer(label+"/cancel", func(context.Context) error {
			cancel()
			return nil
		}); err != nil {
			cancel()
			return err
		}

		go func() {
			defer close(done)
			var err error
			func() {
				defer recoverError("goroutine "+label, &err)
				err = fn(taskContext)
			}()
			if errors.Is(err, context.Canceled) && taskContext.Err() != nil {
				err = nil
			}
			taskErrMu.Lock()
			taskErr = err
			taskErrMu.Unlock()
			if err != nil && r.Active() {
				r.Instance.reportActivationFailure(activationFailure{
					activation: r.activation,
					label:      label,
					err:        err,
				})
			}
		}()
		return nil
	})
	return err
}

func (r *Run) After(label string, delay time.Duration, fn func(context.Context) error) error {
	return r.Go(label, func(ctx context.Context) error {
		timer := time.NewTimer(delay)
		defer timer.Stop()
		select {
		case <-ctx.Done():
			return nil
		case <-timer.C:
			return fn(ctx)
		}
	})
}

func (r *Run) Every(label string, interval time.Duration, fn func(context.Context) error) error {
	if interval <= 0 {
		return fmt.Errorf("runtime: interval must be positive: %s", interval)
	}
	return r.Go(label, func(ctx context.Context) error {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return nil
			case <-ticker.C:
				if err := fn(ctx); err != nil {
					return err
				}
			}
		}
	})
}

// Mount creates a child instance whose lifetime is owned by this Run.
func (r *Run) Mount(entryID string, definition Definition, config []byte) (*Instance, error) {
	if !r.Active() {
		return nil, ErrInactiveRun
	}
	child, err := r.Scope.runtime.Mount(r.Scope, entryID, definition, config)
	if err != nil {
		return nil, err
	}
	if err := r.Defer("child:"+entryID, func(ctx context.Context) error {
		return child.Dispose(ctx)
	}); err != nil {
		cleanupErr := child.Dispose(context.Background())
		return nil, errors.Join(err, cleanupErr)
	}
	return child, nil
}
