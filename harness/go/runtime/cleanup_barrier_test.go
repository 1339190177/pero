package runtime

import (
	"context"
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestBeforeCleanupRunsAfterCancellationBeforeResourcesAcrossStopPaths(t *testing.T) {
	for _, mode := range []string{"dispose", "start-error", "start-panic", "task-failure"} {
		t.Run(mode, func(t *testing.T) {
			runtime := New()
			ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
			defer cancel()
			entered, release, resource := make(chan struct{}), make(chan struct{}), make(chan struct{})
			var once sync.Once
			unblock := func() { once.Do(func() { close(release) }) }
			defer unblock()
			defer func() { unblock(); _ = runtime.Dispose(context.Background()) }()
			failure := errors.New("activation proof failure")
			definition := Define(Descriptor{ID: "test/cleanup-barrier-" + mode}, DecodeJSON[struct{}], func(run *Run, _ struct{}) (Dispose, error) {
				if err := run.BeforeCleanup("work-drain", func(context.Context) error {
					if run.Context.Err() == nil {
						return errors.New("barrier ran before cancellation")
					}
					close(entered)
					<-release
					return nil
				}); err != nil {
					return nil, err
				}
				if err := run.Defer("resource", func(context.Context) error { close(resource); return nil }); err != nil {
					return nil, err
				}
				switch mode {
				case "start-error":
					return nil, failure
				case "start-panic":
					panic(failure)
				case "task-failure":
					if err := run.Go("fail", func(context.Context) error { return failure }); err != nil {
						return nil, err
					}
					<-run.Context.Done()
				}
				return nil, nil
			})
			instance, err := runtime.Mount(nil, "barrier", definition, nil)
			if err != nil {
				t.Fatal(err)
			}
			disposed := make(chan error, 1)
			if mode == "dispose" {
				if err := instance.WaitStable(ctx); err != nil {
					t.Fatal(err)
				}
				go func() { disposed <- instance.Dispose(ctx) }()
			}
			select {
			case <-entered:
			case <-ctx.Done():
				t.Fatal("cleanup barrier was never entered")
			}
			select {
			case <-resource:
				t.Fatal("resource released before barrier completion")
			default:
			}
			unblock()
			if mode == "dispose" {
				select {
				case err := <-disposed:
					if err != nil {
						t.Fatal(err)
					}
				case <-ctx.Done():
					t.Fatal(ctx.Err())
				}
			} else {
				if err := instance.WaitStable(ctx); err != nil {
					t.Fatal(err)
				}
				if instance.Snapshot().State != Failed || !strings.Contains(instance.LastError().Error(), failure.Error()) {
					t.Fatalf("failure receipt=%+v / %v", instance.Snapshot(), instance.LastError())
				}
			}
			select {
			case <-resource:
			default:
				t.Fatal("terminal receipt omitted resource cleanup")
			}
		})
	}
}

func TestBeforeCleanupErrorsAndPanicsPreserveLaterCleanup(t *testing.T) {
	runtime := New()
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	first, second := errors.New("first barrier failure"), errors.New("resource failure")
	var order []string
	definition := Define(Descriptor{ID: "test/cleanup-barrier-errors"}, DecodeJSON[struct{}], func(run *Run, _ struct{}) (Dispose, error) {
		if err := run.BeforeCleanup("first", func(context.Context) error { order = append(order, "first"); return first }); err != nil {
			return nil, err
		}
		if err := run.BeforeCleanup("panic", func(context.Context) error { order = append(order, "panic"); panic("barrier panic") }); err != nil {
			return nil, err
		}
		return nil, run.Defer("resource", func(context.Context) error { order = append(order, "resource"); return second })
	})
	instance, err := runtime.Mount(nil, "barrier-errors", definition, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := instance.WaitStable(ctx); err != nil {
		t.Fatal(err)
	}
	err = instance.Dispose(ctx)
	if !errors.Is(err, first) || !errors.Is(err, second) || !strings.Contains(err.Error(), "barrier panic") {
		t.Fatalf("cleanup errors=%v", err)
	}
	if strings.Join(order, ",") != "panic,first,resource" {
		t.Fatalf("cleanup order=%v", order)
	}
	_ = runtime.Dispose(ctx)
}

func TestBeforeCleanupDoesNotOwnIndependentLocalEffectRollback(t *testing.T) {
	runtime := New()
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	localClosed, activationStopped := make(chan struct{}), make(chan struct{})
	expected := errors.New("local effect failure")
	definition := Define(Descriptor{ID: "test/local-effect-ownership"}, DecodeJSON[struct{}], func(run *Run, _ struct{}) (Dispose, error) {
		if err := run.BeforeCleanup("activation", func(context.Context) error { close(activationStopped); return nil }); err != nil {
			return nil, err
		}
		_, err := run.Effect("local", func(scope *EffectScope) error {
			if err := scope.Defer("local-resource", func(context.Context) error { close(localClosed); return nil }); err != nil {
				return err
			}
			return expected
		})
		if !errors.Is(err, expected) {
			return nil, errors.New("local rollback lost failure")
		}
		return nil, nil
	})
	instance, err := runtime.Mount(nil, "local-effect", definition, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := instance.WaitStable(ctx); err != nil {
		t.Fatal(err)
	}
	select {
	case <-localClosed:
	default:
		t.Fatal("local effect owns its own rollback")
	}
	select {
	case <-activationStopped:
		t.Fatal("local rollback incorrectly stopped activation")
	default:
	}
	if err := instance.Dispose(ctx); err != nil {
		t.Fatal(err)
	}
	select {
	case <-activationStopped:
	default:
		t.Fatal("activation barrier omitted")
	}
	_ = runtime.Dispose(ctx)
}

// A completed stop must return the same cleanup receipt whether Dispose is
// admitted before or after the actor receives the stop result. Retrying the
// failed generation, however, must not taint a later successful disposal.
func TestCompletedStopReceiptSurvivesUntilDisposalOrRetry(t *testing.T) {
	for _, mode := range []string{"dispose-during-stop", "dispose-after-stop", "restart", "update", "restart-start-failure", "update-start-failure", "restart-autonomous-failure", "update-autonomous-failure"} {
		t.Run(mode, func(t *testing.T) {
			runtime := New()
			ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
			defer cancel()
			entered, release := make(chan struct{}), make(chan struct{})
			var once sync.Once
			unblock := func() { once.Do(func() { close(release) }) }
			defer func() { unblock(); _ = runtime.Dispose(context.Background()) }()
			helperFailure, cleanupFailure := errors.New("cancelled helper failed"), errors.New("resource cleanup failed")
			startFailure := errors.New("replacement start failed")
			replacementFailure := errors.New("replacement task failed")
			failReplacement, replacementStopping := make(chan struct{}), make(chan struct{})
			var starts, cleanups atomic.Int32
			runs := make(chan *Run, 2)
			definition := Define(Descriptor{ID: "test/completed-stop-receipt"}, DecodeJSON[struct{}], func(run *Run, _ struct{}) (Dispose, error) {
				generation := starts.Add(1)
				runs <- run
				if err := run.Defer("resource", func(context.Context) error {
					cleanups.Add(1)
					if generation == 1 {
						return cleanupFailure
					}
					return nil
				}); err != nil {
					return nil, err
				}
				if generation != 1 {
					if strings.HasSuffix(mode, "-start-failure") {
						return nil, startFailure
					}
					if strings.HasSuffix(mode, "-autonomous-failure") {
						if err := run.BeforeCleanup("replacement-stopping", func(context.Context) error { close(replacementStopping); return nil }); err != nil {
							return nil, err
						}
						return nil, run.Go("replacement", func(ctx context.Context) error {
							select {
							case <-failReplacement:
								return replacementFailure
							case <-ctx.Done():
								return nil
							}
						})
					}
					return nil, nil
				}
				if err := run.BeforeCleanup("retiring-phase", func(context.Context) error {
					close(entered)
					<-release
					return nil
				}); err != nil {
					return nil, err
				}
				return nil, run.Go("helper", func(ctx context.Context) error { <-ctx.Done(); return helperFailure })
			})
			instance, err := runtime.Mount(nil, "completed-stop-receipt", definition, nil)
			if err != nil {
				t.Fatal(err)
			}
			if err := instance.WaitStable(ctx); err != nil {
				t.Fatal(err)
			}
			assertState(t, instance, Active)
			firstRun := <-runs
			firstRun.CancelWork()
			select {
			case <-entered:
			case <-ctx.Done():
				t.Fatal(ctx.Err())
			}
			assertState(t, instance, Stopping)
			if got := cleanups.Load(); got != 0 {
				t.Fatalf("resource cleanup before retiring phase finished=%d", got)
			}
			if mode == "dispose-during-stop" {
				// Await command admission, not goroutine scheduling, before
				// allowing the completed cleanup result to reach the actor.
				if err := instance.send(ctx, instanceCommand{kind: commandDispose, ack: make(chan error, 1)}); err != nil {
					t.Fatal(err)
				}
			}
			unblock()
			if err := instance.WaitStable(ctx); err != nil {
				t.Fatal(err)
			}
			if mode != "dispose-during-stop" {
				assertState(t, instance, Failed)
			}
			if err := instance.LastError(); !errors.Is(err, helperFailure) || !errors.Is(err, cleanupFailure) {
				t.Fatalf("completed cleanup receipt=%v", err)
			}
			if got := cleanups.Load(); got != 1 {
				t.Fatalf("first generation cleanup count=%d, want 1", got)
			}
			if strings.HasPrefix(mode, "restart") || strings.HasPrefix(mode, "update") {
				if strings.HasPrefix(mode, "restart") {
					err = instance.Restart(ctx)
				} else {
					err = instance.Update(ctx, []byte(`{}`))
				}
				if err != nil {
					t.Fatal(err)
				}
				if err := instance.WaitStable(ctx); err != nil {
					t.Fatal(err)
				}
				if strings.HasSuffix(mode, "-start-failure") {
					assertState(t, instance, Failed)
					if err := instance.LastError(); !errors.Is(err, startFailure) || errors.Is(err, helperFailure) || errors.Is(err, cleanupFailure) {
						t.Fatalf("replacement start receipt=%v", err)
					}
				} else {
					assertState(t, instance, Active)
					if err := instance.LastError(); err != nil {
						t.Fatalf("retry retained diagnostic failure=%v", err)
					}
				}
				// A retained Run from the previous generation must not change
				// this replacement's ordinary autonomous-failure contract.
				firstRun.CancelWork()
				if strings.HasSuffix(mode, "-autonomous-failure") {
					close(failReplacement)
					select {
					case <-replacementStopping:
					case <-ctx.Done():
						t.Fatal(ctx.Err())
					}
					if err := instance.WaitStable(ctx); err != nil {
						t.Fatal(err)
					}
					assertState(t, instance, Failed)
					if err := instance.LastError(); !errors.Is(err, replacementFailure) || errors.Is(err, helperFailure) || errors.Is(err, cleanupFailure) {
						t.Fatalf("replacement task receipt=%v", err)
					}
				}
				if err := instance.Dispose(ctx); err != nil {
					t.Fatalf("retry disposal retained stale receipt=%v", err)
				}
				if starts.Load() != 2 || cleanups.Load() != 2 {
					t.Fatalf("retry starts=%d cleanups=%d, want 2 each", starts.Load(), cleanups.Load())
				}
				return
			}
			for receipt := 0; receipt < 2; receipt++ {
				if err := instance.Dispose(ctx); !errors.Is(err, helperFailure) || !errors.Is(err, cleanupFailure) {
					t.Fatalf("disposal receipt %d=%v", receipt, err)
				}
			}
			if got := cleanups.Load(); got != 1 {
				t.Fatalf("repeated disposal cleanup count=%d, want 1", got)
			}
		})
	}
}
