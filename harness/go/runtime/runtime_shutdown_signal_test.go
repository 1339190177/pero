package runtime

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// This wrapper preserves the parent's Done channel. Once cleanup is blocked,
// it turns any new evaluation of Done into a positive, controlled observation.
// Mount/startup finish before arming, so their context propagation is untouched.
type shutdownDoneEvaluationProbe struct {
	context.Context
	armed    atomic.Bool
	observed chan struct{}
	release  chan struct{}
	once     sync.Once
}

func (p *shutdownDoneEvaluationProbe) Done() <-chan struct{} {
	if p.armed.Load() {
		p.once.Do(func() { close(p.observed) })
		<-p.release
	}
	return p.Context.Done()
}

func TestRuntimeShutdownBlockedCleanupRemainsResponsive(t *testing.T) {
	exerciseRuntimeBlockedShutdown(t, false)
}

func TestRuntimeShutdownDoesNotReevaluateCanceledContext(t *testing.T) {
	exerciseRuntimeBlockedShutdown(t, true)
}

func exerciseRuntimeBlockedShutdown(t *testing.T, observeDone bool) {
	t.Helper()
	runtime := New()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	cleanupEntered := make(chan struct{})
	releaseCleanup := make(chan struct{})
	releaseProbe := make(chan struct{})
	var cleanupOnce, probeOnce sync.Once
	unblockCleanup := func() { cleanupOnce.Do(func() { close(releaseCleanup) }) }
	unblockProbe := func() { probeOnce.Do(func() { close(releaseProbe) }) }
	var probe *shutdownDoneEvaluationProbe
	var observed <-chan struct{}
	var disposed, commandsDone chan error
	joinedDispose, joinedCommands := false, false
	defer func() {
		if probe != nil {
			probe.armed.Store(false)
		}
		unblockProbe()
		unblockCleanup()
		// Every failing path releases both barriers and joins the goroutines.
		joinCtx, joinCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer joinCancel()
		if disposed == nil {
			if err := runtime.Dispose(joinCtx); err != nil {
				t.Error("early fixture runtime disposal:", err)
			}
		}
		if commandsDone != nil && !joinedCommands {
			select {
			case <-commandsDone:
			case <-joinCtx.Done():
				t.Error("command driver did not finish during cleanup:", joinCtx.Err())
			}
		}
		if disposed != nil && !joinedDispose {
			select {
			case <-disposed:
			case <-joinCtx.Done():
				t.Error("runtime disposal did not finish during cleanup:", joinCtx.Err())
			}
		}
	}()
	if observeDone {
		probe = &shutdownDoneEvaluationProbe{
			Context: runtime.context, observed: make(chan struct{}), release: releaseProbe,
		}
		// No actor exists yet; the wrapper is immutable after publication.
		runtime.context = probe
		observed = probe.observed
	}
	cleanupErr := errors.New("blocked shutdown cleanup sentinel")
	definition := Define(
		Descriptor{ID: "test/blocked-shutdown"},
		DecodeJSON[struct{}],
		func(*Run, struct{}) (Dispose, error) {
			return func(context.Context) error {
				close(cleanupEntered)
				<-releaseCleanup
				return cleanupErr
			}, nil
		},
	)
	instance, err := runtime.Mount(nil, "blocked-shutdown", definition, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := instance.WaitStable(ctx); err != nil {
		t.Fatal(err)
	}
	disposed = make(chan error, 1)
	go func() { disposed <- runtime.Dispose(ctx) }()
	select {
	case <-cleanupEntered:
	case <-ctx.Done():
		t.Fatal("cleanup did not begin:", ctx.Err())
	}
	if probe != nil {
		probe.armed.Store(true)
	}
	commandsDone = make(chan error, 1)
	go func() {
		// An old actor can have at most one select already evaluated before
		// arming. A second ACK requires another select evaluation, so an old
		// actor must hit the probe before it can complete both commands.
		for n := 0; n < 2; n++ {
			if err := instance.Restart(ctx); !errors.Is(err, ErrDisposedInstance) {
				commandsDone <- fmt.Errorf("shutdown command %d: got %v, want ErrDisposedInstance", n+1, err)
				return
			}
		}
		commandsDone <- nil
	}()
	reevaluated := false
	var commandsErr error
	select {
	case <-observed:
		reevaluated = true
		// Release the deliberate probe before joining anything on the RED
		// path; a failing assertion must never leave the actor trapped.
		unblockProbe()
		select {
		case commandsErr = <-commandsDone:
			joinedCommands = true
		case <-ctx.Done():
			t.Fatal("commands did not finish after probe release:", ctx.Err())
		}
	case commandsErr = <-commandsDone:
		joinedCommands = true
	case <-ctx.Done():
		t.Fatal("shutdown commands did not respond:", ctx.Err())
	}
	select {
	case <-observed:
		reevaluated = true
	default:
	}
	unblockProbe()
	if commandsErr != nil {
		t.Error(commandsErr)
	}
	select {
	case err := <-disposed:
		joinedDispose = true
		t.Fatalf("Dispose completed before cleanup was released: %v", err)
	default:
	}
	if snapshot := instance.Snapshot(); snapshot.State != Stopping {
		t.Errorf("blocked cleanup state = %s, want stopping", snapshot.State)
	}
	unblockCleanup()
	select {
	case err := <-disposed:
		joinedDispose = true
		if !errors.Is(err, cleanupErr) {
			t.Errorf("Dispose lost cleanup error: %v", err)
		}
	case <-ctx.Done():
		t.Fatal("Dispose did not join released cleanup:", ctx.Err())
	}
	select {
	case <-instance.done:
	default:
		t.Error("Dispose returned before the instance actor finished")
	}
	if snapshot := instance.Snapshot(); snapshot.State != Disposed || len(snapshot.Providers) != 0 {
		t.Errorf("final instance snapshot = %+v", snapshot)
	}
	if snapshot := runtime.Snapshot(); !snapshot.Disposed || snapshot.Instances != 0 || snapshot.Services != 0 {
		t.Errorf("final runtime snapshot = %+v", snapshot)
	}
	if reevaluated {
		t.Error("shutdown actor re-evaluated the canceled runtime context while cleanup was blocked")
	}
}
