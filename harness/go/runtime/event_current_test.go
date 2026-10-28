package runtime

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
)

func TestSignalEmitCurrentRefreshesBetweenListenersWithoutChangingEmit(t *testing.T) {
	runtime := New()
	t.Cleanup(func() { _ = runtime.Dispose(context.Background()) })
	signal := NewSignal[int](runtime, "test/current")
	current, reads := 1, 0
	var values []int
	stop := errors.New("stop")
	definition := eventDefinition("test/current-listeners", func(run *Run) error {
		if _, err := signal.On(run, func(_ context.Context, value int) error { values = append(values, value); current = 2; return nil }); err != nil {
			return err
		}
		if _, err := signal.On(run, func(ctx context.Context, value int) error {
			if !errors.Is(ctx.Err(), context.Canceled) {
				return errors.New("cancelled context was not forwarded")
			}
			values = append(values, value)
			return stop
		}); err != nil {
			return err
		}
		_, err := signal.On(run, func(context.Context, int) error { t.Error("listener ran after failure"); return nil })
		return err
	})
	instance, err := runtime.Mount(nil, "current-listeners", definition, nil)
	if err != nil {
		t.Fatal(err)
	}
	waitStable(t, instance)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	provider := func() int { reads++; return current }
	if err := signal.EmitCurrent(ctx, provider); !errors.Is(err, stop) {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(values, []int{1, 2}) || reads != 2 {
		t.Fatalf("values=%v reads=%d", values, reads)
	}
	values = nil
	if err := signal.Emit(ctx, 9); !errors.Is(err, stop) {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(values, []int{9, 9}) {
		t.Fatalf("Emit semantics changed: %v", values)
	}
	if err := signal.EmitCurrent(ctx, func() int { panic("provider failed") }); err == nil || !strings.Contains(err.Error(), "provider failed") {
		t.Fatalf("provider panic escaped: %v", err)
	}
	// Registration disposal is allowed inside the current-value provider;
	// this uses Emit's existing captured-listener semantics for this dispatch.
	values = nil
	if err := signal.EmitCurrent(ctx, func() int {
		if err := instance.Dispose(context.Background()); err != nil {
			t.Error(err)
		}
		return 7
	}); !errors.Is(err, stop) {
		t.Fatal(err)
	}
	reads = 0
	if err := signal.EmitCurrent(ctx, provider); err != nil || reads != 0 {
		t.Fatalf("disposed listener/provider leaked: err=%v reads=%d", err, reads)
	}
}

func TestSignalEmitAllCurrentContainsFailuresAndRefreshesSnapshots(t *testing.T) {
	runtime := New()
	t.Cleanup(func() { _ = runtime.Dispose(context.Background()) })
	signal := NewSignal[[]int](runtime, "test/all-current")
	stop := errors.New("stop")
	var seen []int
	instance, err := runtime.Mount(nil, "all-current-listeners", eventDefinition("test/all-current-listeners", func(run *Run) error {
		for i := range 4 {
			if _, err := signal.On(run, func(_ context.Context, value []int) error {
				seen = append(seen, value[0])
				value[0] = 99
				switch i {
				case 0:
					return stop
				case 1:
					panic("listener failed")
				}
				return nil
			}); err != nil {
				return err
			}
		}
		return nil
	}), nil)
	if err != nil {
		t.Fatal(err)
	}
	waitStable(t, instance)
	reads := 0
	err = signal.EmitAllCurrent(context.Background(), func() []int {
		reads++
		if reads == 3 {
			panic("provider failed")
		}
		return []int{7}
	})
	if !errors.Is(err, stop) || !strings.Contains(err.Error(), "listener failed") || !strings.Contains(err.Error(), "provider failed") {
		t.Fatalf("failures not joined: %v", err)
	}
	if reads != 4 || !reflect.DeepEqual(seen, []int{7, 7, 7}) {
		t.Fatalf("cleanup starved or snapshot reused: reads=%d seen=%v", reads, seen)
	}
}
