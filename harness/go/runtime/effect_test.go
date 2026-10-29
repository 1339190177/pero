package runtime

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestEffectGroupStopsInReverseAndOnlyOnce(t *testing.T) {
	group := newEffectGroup("test")
	var mu sync.Mutex
	var order []string
	for _, label := range []string{"first", "second", "third"} {
		label := label
		if _, err := group.add(label, func(context.Context) error {
			mu.Lock()
			order = append(order, label)
			mu.Unlock()
			return nil
		}); err != nil {
			t.Fatalf("add effect: %v", err)
		}
	}

	const callers = 16
	start := make(chan struct{})
	errorsByCaller := make(chan error, callers)
	for range callers {
		go func() {
			<-start
			errorsByCaller <- group.stop(context.Background())
		}()
	}
	close(start)
	for range callers {
		if err := <-errorsByCaller; err != nil {
			t.Fatalf("stop effect group: %v", err)
		}
	}

	mu.Lock()
	defer mu.Unlock()
	if want := []string{"third", "second", "first"}; !reflect.DeepEqual(order, want) {
		t.Fatalf("cleanup order = %v, want %v", order, want)
	}
	if _, err := group.add("late", func(context.Context) error { return nil }); !errors.Is(err, ErrEffectClosed) {
		t.Fatalf("late add error = %v, want ErrEffectClosed", err)
	}
}

func TestRunGoCancelsAndWaits(t *testing.T) {
	runtime := New()
	started := make(chan struct{})
	exited := make(chan struct{})
	definition := Define(
		Descriptor{ID: "test/goroutine"},
		DecodeJSON[struct{}],
		func(run *Run, _ struct{}) (Dispose, error) {
			if err := run.Go("worker", func(ctx context.Context) error {
				close(started)
				<-ctx.Done()
				close(exited)
				return nil
			}); err != nil {
				return nil, err
			}
			return nil, nil
		},
	)
	instance, err := runtime.Mount(nil, "worker", definition, nil)
	if err != nil {
		t.Fatalf("mount: %v", err)
	}
	waitStable(t, instance)
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("worker did not start")
	}
	if err := instance.Dispose(testContext(t)); err != nil {
		t.Fatalf("dispose: %v", err)
	}
	select {
	case <-exited:
	default:
		t.Fatal("Dispose returned before the worker exited")
	}
}

func TestRunGoFailureFailsOwningInstance(t *testing.T) {
	runtime := New()
	taskError := errors.New("task failed")
	definition := Define(
		Descriptor{ID: "test/task-error"},
		DecodeJSON[struct{}],
		func(run *Run, _ struct{}) (Dispose, error) {
			return nil, run.Go("worker", func(context.Context) error { return taskError })
		},
	)
	instance, err := runtime.Mount(nil, "task-error", definition, nil)
	if err != nil {
		t.Fatalf("mount: %v", err)
	}
	waitState(t, instance, Failed)
	if snapshot := instance.Snapshot(); snapshot.State != Failed || !strings.Contains(snapshot.LastError, taskError.Error()) {
		t.Fatalf("snapshot = %+v, want failed task error", snapshot)
	}
	if err := instance.Dispose(testContext(t)); err != nil {
		t.Fatalf("dispose: %v", err)
	}
}

func TestEffectStartFailureCleansPartialRegistration(t *testing.T) {
	runtime := New()
	startError := errors.New("start failed")
	var cleaned atomic.Int32
	definition := Define(
		Descriptor{ID: "test/partial-effect"},
		DecodeJSON[struct{}],
		func(run *Run, _ struct{}) (Dispose, error) {
			_, err := run.Effect("partial", func(scope *EffectScope) error {
				if err := scope.Defer("resource", func(context.Context) error {
					cleaned.Add(1)
					return nil
				}); err != nil {
					return err
				}
				return startError
			})
			return nil, err
		},
	)
	instance, err := runtime.Mount(nil, "partial", definition, nil)
	if err != nil {
		t.Fatalf("mount: %v", err)
	}
	waitStable(t, instance)
	if snapshot := instance.Snapshot(); snapshot.State != Failed || snapshot.LastError == "" {
		t.Fatalf("snapshot = %+v, want failed with error", snapshot)
	}
	if got := cleaned.Load(); got != 1 {
		t.Fatalf("cleanup count = %d, want 1", got)
	}
	if err := instance.Dispose(testContext(t)); err != nil {
		t.Fatalf("dispose failed instance: %v", err)
	}
}

func TestInstanceSnapshotIncludesNestedEffectTree(t *testing.T) {
	runtime := New()
	definition := Define(
		Descriptor{ID: "test/nested-effects"},
		DecodeJSON[struct{}],
		func(run *Run, _ struct{}) (Dispose, error) {
			_, err := run.Effect("outer", func(scope *EffectScope) error {
				return scope.Defer("inner", func(context.Context) error { return nil })
			})
			return nil, err
		},
	)
	instance, err := runtime.Mount(nil, "nested-effects", definition, nil)
	if err != nil {
		t.Fatalf("mount: %v", err)
	}
	waitStable(t, instance)
	effects := instance.Snapshot().Effects
	if len(effects) != 1 || effects[0].Label != "outer" || len(effects[0].Children) != 1 || effects[0].Children[0].Label != "inner" {
		t.Fatalf("nested effects = %+v", effects)
	}
	if err := runtime.Dispose(testContext(t)); err != nil {
		t.Fatalf("dispose runtime: %v", err)
	}
}
