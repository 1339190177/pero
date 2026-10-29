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

type database struct {
	id int
}

var databaseKey = NewKey[*database]("test.database")

type databaseConfig struct {
	ID int `json:"id"`
}

func databaseDefinition(leaseTarget chan<- *Lease[*database], startGate <-chan struct{}) Definition {
	return Define(
		Descriptor{ID: "test/database", Provides: []AnyKey{databaseKey.Any()}},
		DecodeJSON[databaseConfig],
		func(run *Run, config databaseConfig) (Dispose, error) {
			lease, err := Provide(run, databaseKey, &database{id: config.ID})
			if err != nil {
				return nil, err
			}
			if leaseTarget != nil {
				leaseTarget <- lease
			}
			if startGate != nil {
				select {
				case <-startGate:
				case <-run.Context.Done():
					return nil, run.Context.Err()
				}
			}
			return nil, nil
		},
	)
}

func consumerDefinition(starts chan<- int, stops *atomic.Int32) Definition {
	return Define(
		Descriptor{ID: "test/consumer", Requires: []Requirement{Require(databaseKey)}},
		DecodeJSON[struct{}],
		func(run *Run, _ struct{}) (Dispose, error) {
			database, err := Resolve(run, databaseKey)
			if err != nil {
				return nil, err
			}
			starts <- database.id
			return func(context.Context) error {
				stops.Add(1)
				return nil
			}, nil
		},
	)
}

func TestProviderConsumerLifecycle(t *testing.T) {
	runtime := New()
	starts := make(chan int, 4)
	var stops atomic.Int32
	consumer, err := runtime.Mount(nil, "consumer", consumerDefinition(starts, &stops), nil)
	if err != nil {
		t.Fatalf("mount consumer: %v", err)
	}
	waitStable(t, consumer)
	assertState(t, consumer, Pending)
	if missing := consumer.Snapshot().Missing; len(missing) != 1 || missing[0].Key != databaseKey.Name() {
		t.Fatalf("missing = %v, want database", missing)
	}

	provider1, err := runtime.Mount(nil, "database-1", databaseDefinition(nil, nil), []byte(`{"id":1}`))
	if err != nil {
		t.Fatalf("mount provider: %v", err)
	}
	waitStable(t, provider1)
	waitStable(t, consumer)
	assertState(t, provider1, Active)
	assertState(t, consumer, Active)
	if got := receiveInt(t, starts); got != 1 {
		t.Fatalf("first database ID = %d, want 1", got)
	}
	firstProvider := consumer.Snapshot().Providers[0]

	if err := provider1.Dispose(testContext(t)); err != nil {
		t.Fatalf("dispose provider: %v", err)
	}
	waitStable(t, consumer)
	assertState(t, consumer, Pending)
	if got := stops.Load(); got != 1 {
		t.Fatalf("consumer stop count = %d, want 1", got)
	}

	provider2, err := runtime.Mount(nil, "database-2", databaseDefinition(nil, nil), []byte(`{"id":2}`))
	if err != nil {
		t.Fatalf("mount replacement provider: %v", err)
	}
	waitStable(t, provider2)
	waitStable(t, consumer)
	if got := receiveInt(t, starts); got != 2 {
		t.Fatalf("replacement database ID = %d, want 2", got)
	}
	secondProvider := consumer.Snapshot().Providers[0]
	if firstProvider.ActivationID == secondProvider.ActivationID || firstProvider.LeaseID == secondProvider.LeaseID {
		t.Fatalf("provider revision did not change: first=%+v second=%+v", firstProvider, secondProvider)
	}

	if err := runtime.Dispose(testContext(t)); err != nil {
		t.Fatalf("dispose runtime: %v", err)
	}
	if snapshot := runtime.Snapshot(); snapshot.Instances != 0 || snapshot.Services != 0 || !snapshot.Disposed {
		t.Fatalf("runtime residue: %+v", snapshot)
	}
}

func TestStagedServiceIsInvisibleUntilStartCompletes(t *testing.T) {
	runtime := New()
	gate := make(chan struct{})
	starts := make(chan int, 1)
	var stops atomic.Int32
	consumer, err := runtime.Mount(nil, "consumer", consumerDefinition(starts, &stops), nil)
	if err != nil {
		t.Fatalf("mount consumer: %v", err)
	}
	provider, err := runtime.Mount(nil, "provider", databaseDefinition(nil, gate), []byte(`{"id":7}`))
	if err != nil {
		t.Fatalf("mount provider: %v", err)
	}
	waitState(t, provider, Starting)
	if _, ok := Lookup(runtime, runtime.Root(), databaseKey); ok {
		t.Fatal("staged service was visible through administrative lookup")
	}
	assertState(t, consumer, Pending)
	close(gate)
	waitStable(t, provider)
	waitStable(t, consumer)
	if got := receiveInt(t, starts); got != 7 {
		t.Fatalf("database ID = %d, want 7", got)
	}
	if err := runtime.Dispose(testContext(t)); err != nil {
		t.Fatalf("dispose runtime: %v", err)
	}
}

func TestStartFailureCleansEffectsInReverse(t *testing.T) {
	runtime := New()
	startError := errors.New("boom")
	var mu sync.Mutex
	var order []string
	definition := Define(
		Descriptor{ID: "test/failing-start"},
		DecodeJSON[struct{}],
		func(run *Run, _ struct{}) (Dispose, error) {
			for _, label := range []string{"first", "second", "third"} {
				label := label
				if err := run.Defer(label, func(context.Context) error {
					mu.Lock()
					order = append(order, label)
					mu.Unlock()
					return nil
				}); err != nil {
					return nil, err
				}
			}
			return nil, startError
		},
	)
	instance, err := runtime.Mount(nil, "failing", definition, nil)
	if err != nil {
		t.Fatalf("mount: %v", err)
	}
	waitStable(t, instance)
	assertState(t, instance, Failed)
	mu.Lock()
	got := fmt.Sprint(order)
	mu.Unlock()
	if got != "[third second first]" {
		t.Fatalf("cleanup order = %s", got)
	}
	if err := instance.Dispose(testContext(t)); err != nil {
		t.Fatalf("dispose failed instance: %v", err)
	}
}

func TestFailedInstanceDoesNotRetryOnDependencyChurn(t *testing.T) {
	runtime := New()
	leases := make(chan *Lease[*database], 1)
	provider, err := runtime.Mount(nil, "provider", databaseDefinition(leases, nil), []byte(`{"id":1}`))
	if err != nil {
		t.Fatalf("mount provider: %v", err)
	}
	waitStable(t, provider)
	lease := <-leases
	var attempts atomic.Int32
	definition := Define(
		Descriptor{ID: "test/fails-once", Requires: []Requirement{Require(databaseKey)}},
		DecodeJSON[struct{}],
		func(run *Run, _ struct{}) (Dispose, error) {
			if _, err := Resolve(run, databaseKey); err != nil {
				return nil, err
			}
			if attempts.Add(1) == 1 {
				return nil, errors.New("intentional")
			}
			return nil, nil
		},
	)
	consumer, err := runtime.Mount(nil, "consumer", definition, nil)
	if err != nil {
		t.Fatalf("mount consumer: %v", err)
	}
	waitStable(t, consumer)
	assertState(t, consumer, Failed)
	if err := lease.SetReady(false); err != nil {
		t.Fatalf("set not ready: %v", err)
	}
	if err := lease.SetReady(true); err != nil {
		t.Fatalf("set ready: %v", err)
	}
	time.Sleep(10 * time.Millisecond)
	if got := attempts.Load(); got != 1 {
		t.Fatalf("attempts after dependency churn = %d, want 1", got)
	}
	if err := consumer.Restart(testContext(t)); err != nil {
		t.Fatalf("restart: %v", err)
	}
	waitStable(t, consumer)
	assertState(t, consumer, Active)
	if got := attempts.Load(); got != 2 {
		t.Fatalf("attempts after explicit restart = %d, want 2", got)
	}
	if err := runtime.Dispose(testContext(t)); err != nil {
		t.Fatalf("dispose runtime: %v", err)
	}
}

func TestScopeIsolationIsPerKey(t *testing.T) {
	runtime := New()
	otherKey := NewKey[string]("test.other")
	root := runtime.Root()
	databaseIsolated := Isolate(root, databaseKey)
	if root.realm(databaseKey.id) == databaseIsolated.realm(databaseKey.id) {
		t.Fatal("database realm was not isolated")
	}
	if root.realm(otherKey.id) != databaseIsolated.realm(otherKey.id) {
		t.Fatal("isolating database unexpectedly changed another key")
	}
	joined := JoinRealm(root, databaseKey, databaseIsolated.realm(databaseKey.id))
	if joined.realm(databaseKey.id) != databaseIsolated.realm(databaseKey.id) {
		t.Fatal("joined scope did not resolve the shared realm")
	}
}

func TestConfigDecodeFailureDoesNotStopCurrentActivation(t *testing.T) {
	runtime := New()
	provider, err := runtime.Mount(nil, "provider", databaseDefinition(nil, nil), []byte(`{"id":1}`))
	if err != nil {
		t.Fatalf("mount: %v", err)
	}
	waitStable(t, provider)
	before := provider.Snapshot()
	if err := provider.Update(testContext(t), []byte(`{"id":2,"unknown":true}`)); err == nil {
		t.Fatal("invalid config update unexpectedly succeeded")
	}
	after := provider.Snapshot()
	if after.State != Active || after.CurrentRevision != before.CurrentRevision {
		t.Fatalf("invalid update changed activation: before=%+v after=%+v", before, after)
	}
	if err := runtime.Dispose(testContext(t)); err != nil {
		t.Fatalf("dispose runtime: %v", err)
	}
}

func TestRuntimeDisposeContinuesAfterCallerContextExpires(t *testing.T) {
	runtime := New()
	instance, err := runtime.Mount(nil, "provider", databaseDefinition(nil, nil), []byte(`{"id":1}`))
	if err != nil {
		t.Fatalf("mount: %v", err)
	}
	waitStable(t, instance)
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	if err := runtime.Dispose(canceled); !errors.Is(err, context.Canceled) {
		t.Fatalf("dispose error = %v, want context canceled", err)
	}
	ctx := testContext(t)
	for {
		if snapshot := runtime.Snapshot(); snapshot.Disposed && snapshot.Instances == 0 && snapshot.Services == 0 {
			return
		}
		select {
		case <-ctx.Done():
			t.Fatalf("runtime did not finish disposal after caller left: %+v", runtime.Snapshot())
		case <-time.After(time.Millisecond):
		}
	}
}

func testContext(t *testing.T) context.Context {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	t.Cleanup(cancel)
	return ctx
}

func waitStable(t *testing.T, instance *Instance) {
	t.Helper()
	if err := instance.WaitStable(testContext(t)); err != nil {
		t.Fatalf("wait for %q to stabilize: %v; snapshot=%+v", instance.EntryID(), err, instance.Snapshot())
	}
}

func waitState(t *testing.T, instance *Instance, state State) {
	t.Helper()
	ctx := testContext(t)
	for {
		if instance.Snapshot().State == state {
			return
		}
		select {
		case <-ctx.Done():
			t.Fatalf("wait for %q state %s: %v; snapshot=%+v", instance.EntryID(), state, ctx.Err(), instance.Snapshot())
		case <-time.After(time.Millisecond):
		}
	}
}

func assertState(t *testing.T, instance *Instance, state State) {
	t.Helper()
	if got := instance.Snapshot().State; got != state {
		t.Fatalf("%q state = %s, want %s; snapshot=%+v", instance.EntryID(), got, state, instance.Snapshot())
	}
}

func receiveInt(t *testing.T, values <-chan int) int {
	t.Helper()
	select {
	case value := <-values:
		return value
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for value")
		return 0
	}
}
