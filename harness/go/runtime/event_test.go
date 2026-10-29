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

func eventDefinition(id string, start func(*Run) error) Definition {
	return Define(
		Descriptor{ID: id},
		DecodeJSON[struct{}],
		func(run *Run, _ struct{}) (Dispose, error) { return nil, start(run) },
	)
}

func TestSignalOrderingOnceAndLifecycleOwnership(t *testing.T) {
	runtime := New()
	signal := NewSignal[int](runtime, "test/order")
	var mu sync.Mutex
	var calls []int
	definition := eventDefinition("test/listeners", func(run *Run) error {
		appendCall := func(value int) {
			mu.Lock()
			calls = append(calls, value)
			mu.Unlock()
		}
		if _, err := signal.On(run, func(context.Context, int) error { appendCall(1); return nil }); err != nil {
			return err
		}
		if _, err := signal.On(run, func(context.Context, int) error { appendCall(2); return nil }); err != nil {
			return err
		}
		if _, err := signal.Once(run, func(context.Context, int) error { appendCall(0); return nil }, ListenerOptions{Prepend: true}); err != nil {
			return err
		}
		return nil
	})
	instance, err := runtime.Mount(nil, "listeners", definition, nil)
	if err != nil {
		t.Fatalf("mount: %v", err)
	}
	waitStable(t, instance)
	if err := signal.Emit(context.Background(), 1); err != nil {
		t.Fatalf("first emit: %v", err)
	}
	if err := signal.Emit(context.Background(), 2); err != nil {
		t.Fatalf("second emit: %v", err)
	}
	mu.Lock()
	got := append([]int(nil), calls...)
	mu.Unlock()
	if want := []int{0, 1, 2, 1, 2}; !reflect.DeepEqual(got, want) {
		t.Fatalf("calls = %v, want %v", got, want)
	}

	if err := instance.Restart(testContext(t)); err != nil {
		t.Fatalf("restart: %v", err)
	}
	waitStable(t, instance)
	mu.Lock()
	calls = nil
	mu.Unlock()
	if err := signal.Emit(context.Background(), 3); err != nil {
		t.Fatalf("emit after restart: %v", err)
	}
	mu.Lock()
	got = append([]int(nil), calls...)
	mu.Unlock()
	if want := []int{0, 1, 2}; !reflect.DeepEqual(got, want) {
		t.Fatalf("calls after restart = %v, want %v", got, want)
	}

	if err := instance.Dispose(testContext(t)); err != nil {
		t.Fatalf("dispose: %v", err)
	}
	mu.Lock()
	calls = nil
	mu.Unlock()
	if err := signal.Emit(context.Background(), 4); err != nil {
		t.Fatalf("emit after dispose: %v", err)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(calls) != 0 {
		t.Fatalf("listeners leaked after dispose: %v", calls)
	}
}

func TestSignalParallelWaitsAndJoinsErrors(t *testing.T) {
	runtime := New()
	signal := NewSignal[struct{}](runtime, "test/parallel")
	firstError := errors.New("first")
	secondError := errors.New("second")
	started := make(chan struct{})
	release := make(chan struct{})
	definition := eventDefinition("test/parallel-listeners", func(run *Run) error {
		if _, err := signal.On(run, func(context.Context, struct{}) error {
			<-release
			return firstError
		}); err != nil {
			return err
		}
		_, err := signal.On(run, func(context.Context, struct{}) error {
			close(started)
			return secondError
		})
		return err
	})
	instance, err := runtime.Mount(nil, "parallel-listeners", definition, nil)
	if err != nil {
		t.Fatalf("mount: %v", err)
	}
	waitStable(t, instance)
	result := make(chan error, 1)
	go func() { result <- signal.Parallel(context.Background(), struct{}{}) }()
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("parallel listener did not start")
	}
	select {
	case err := <-result:
		t.Fatalf("parallel returned before all listeners settled: %v", err)
	default:
	}
	close(release)
	err = <-result
	if !errors.Is(err, firstError) || !errors.Is(err, secondError) {
		t.Fatalf("parallel error = %v, want both listener errors", err)
	}
	if err := runtime.Dispose(testContext(t)); err != nil {
		t.Fatalf("dispose runtime: %v", err)
	}
}

func TestSignalEmitAllContainsFailuresAndPreservesOrder(t *testing.T) {
	runtime := New()
	signal := NewSignal[int](runtime, "test/emit-all")
	var calls []string
	definition := Define(
		Descriptor{ID: "test/emit-all-listeners"},
		DecodeJSON[struct{}],
		func(run *Run, _ struct{}) (Dispose, error) {
			var registrationErr error
			if _, err := signal.On(run, func(context.Context, int) error {
				calls = append(calls, "first")
				return errors.New("first failed")
			}); err != nil {
				registrationErr = err
			}
			if _, err := signal.On(run, func(context.Context, int) error {
				calls = append(calls, "second")
				return errors.New("second failed")
			}); err != nil && registrationErr == nil {
				registrationErr = err
			}
			return nil, registrationErr
		},
	)
	instance, err := runtime.Mount(nil, "test/emit-all-listeners", definition, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer instance.Dispose(context.Background())
	waitStable(t, instance)
	err = signal.EmitAll(context.Background(), 1)
	if err == nil || !strings.Contains(err.Error(), "first failed") || !strings.Contains(err.Error(), "second failed") {
		t.Fatalf("EmitAll error=%v", err)
	}
	if !reflect.DeepEqual(calls, []string{"first", "second"}) {
		t.Fatalf("EmitAll calls=%v", calls)
	}
}

func TestQueryBailAndPipelineWaterfall(t *testing.T) {
	runtime := New()
	query := NewQuery[string, int](runtime, "test/query")
	pipeline := NewPipeline[int](runtime, "test/waterfall")
	var thirdCalled atomic.Bool
	definition := eventDefinition("test/query-pipeline", func(run *Run) error {
		if _, err := query.On(run, func(context.Context, string) (int, bool, error) {
			return 99, false, nil
		}); err != nil {
			return err
		}
		if _, err := query.On(run, func(context.Context, string) (int, bool, error) {
			return 0, true, nil
		}); err != nil {
			return err
		}
		if _, err := query.On(run, func(context.Context, string) (int, bool, error) {
			thirdCalled.Store(true)
			return 3, true, nil
		}); err != nil {
			return err
		}
		for range 2 {
			if _, err := pipeline.Use(run, func(_ context.Context, value int, next Next[int]) (int, error) {
				rest, err := next(value)
				return value + rest, err
			}); err != nil {
				return err
			}
		}
		return nil
	})
	instance, err := runtime.Mount(nil, "query-pipeline", definition, nil)
	if err != nil {
		t.Fatalf("mount: %v", err)
	}
	waitStable(t, instance)
	value, handled, err := query.Bail(context.Background(), "input")
	if err != nil || !handled || value != 0 {
		t.Fatalf("query = (%d, %v, %v), want (0, true, nil)", value, handled, err)
	}
	if thirdCalled.Load() {
		t.Fatal("query did not bail after handled result")
	}
	result, err := pipeline.Run(context.Background(), 1, func(context.Context, int) (int, error) { return 2, nil })
	if err != nil || result != 4 {
		t.Fatalf("pipeline = (%d, %v), want (4, nil)", result, err)
	}
	if err := runtime.Dispose(testContext(t)); err != nil {
		t.Fatalf("dispose runtime: %v", err)
	}
}

func TestSignalRecoversListenerPanic(t *testing.T) {
	runtime := New()
	signal := NewSignal[struct{}](runtime, "test/panic")
	instance, err := runtime.Mount(nil, "panic-listener", eventDefinition("test/panic-listener", func(run *Run) error {
		_, err := signal.On(run, func(context.Context, struct{}) error { panic("boom") })
		return err
	}), nil)
	if err != nil {
		t.Fatalf("mount: %v", err)
	}
	waitStable(t, instance)
	if err := signal.Emit(context.Background(), struct{}{}); err == nil || !strings.Contains(err.Error(), "boom") {
		t.Fatalf("emit error = %v, want recovered panic", err)
	}
	if err := runtime.Dispose(testContext(t)); err != nil {
		t.Fatalf("dispose runtime: %v", err)
	}
}

func TestSignalFiltersByCallerContextAndSupportsGlobalListener(t *testing.T) {
	runtime := New()
	signal := NewSignal[struct{}](runtime, "test/filter")
	callerRuns := make(chan *Run, 1)
	caller, err := runtime.Mount(nil, "caller", eventDefinition("test/filter-caller", func(run *Run) error {
		callerRuns <- run
		return nil
	}), nil)
	if err != nil {
		t.Fatalf("mount caller: %v", err)
	}
	waitStable(t, caller)
	callerRun := <-callerRuns
	var filtered atomic.Int32
	var global atomic.Int32
	listener, err := runtime.Mount(nil, "filter-listener", eventDefinition("test/filter-listener", func(run *Run) error {
		if _, err := signal.On(run, func(context.Context, struct{}) error {
			filtered.Add(1)
			return nil
		}, ListenerOptions{Filter: func(_ context.Context, _ *Scope, instance *Instance) bool {
			return instance == caller
		}}); err != nil {
			return err
		}
		_, err := signal.On(run, func(context.Context, struct{}) error {
			global.Add(1)
			return nil
		}, ListenerOptions{Global: true, Filter: func(context.Context, *Scope, *Instance) bool { return false }})
		return err
	}), nil)
	if err != nil {
		t.Fatalf("mount listener: %v", err)
	}
	waitStable(t, listener)
	if err := signal.Emit(context.Background(), struct{}{}); err != nil {
		t.Fatalf("unscoped emit: %v", err)
	}
	if err := signal.Emit(callerRun.CallContext(context.Background()), struct{}{}); err != nil {
		t.Fatalf("caller emit: %v", err)
	}
	if filtered.Load() != 1 || global.Load() != 2 {
		t.Fatalf("filtered/global calls = %d/%d, want 1/2", filtered.Load(), global.Load())
	}
	if err := runtime.Dispose(testContext(t)); err != nil {
		t.Fatalf("dispose runtime: %v", err)
	}
}
