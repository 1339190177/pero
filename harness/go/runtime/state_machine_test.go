package runtime

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"
)

func TestDependencyRemovedDuringStartCleansBeforePending(t *testing.T) {
	runtime := New()
	provider, err := runtime.Mount(nil, "provider", databaseDefinition(nil, nil), []byte(`{"id":1}`))
	if err != nil {
		t.Fatalf("mount provider: %v", err)
	}
	waitStable(t, provider)

	started := make(chan context.Context, 1)
	startGate := make(chan struct{})
	var cleanups atomic.Int32
	consumerDefinition := Define(
		Descriptor{ID: "test/start-barrier", Requires: []Requirement{Require(databaseKey)}},
		DecodeJSON[struct{}],
		func(run *Run, _ struct{}) (Dispose, error) {
			if _, err := Resolve(run, databaseKey); err != nil {
				return nil, err
			}
			if err := run.Defer("resource", func(context.Context) error {
				cleanups.Add(1)
				return nil
			}); err != nil {
				return nil, err
			}
			started <- run.Context
			<-startGate
			return nil, nil
		},
	)
	consumer, err := runtime.Mount(nil, "consumer", consumerDefinition, nil)
	if err != nil {
		t.Fatalf("mount consumer: %v", err)
	}
	runContext := <-started
	disposed := make(chan error, 1)
	go func() { disposed <- provider.Dispose(testContext(t)) }()
	select {
	case <-runContext.Done():
	case <-time.After(time.Second):
		t.Fatal("consumer start context was not canceled after dependency removal")
	}
	close(startGate)
	if err := <-disposed; err != nil {
		t.Fatalf("dispose provider: %v", err)
	}
	waitStable(t, consumer)
	assertState(t, consumer, Pending)
	if got := cleanups.Load(); got != 1 {
		t.Fatalf("cleanup count = %d, want 1", got)
	}
	if err := runtime.Dispose(testContext(t)); err != nil {
		t.Fatalf("dispose runtime: %v", err)
	}
}

func TestDependencyReturnsDuringStopRestartsAfterCleanup(t *testing.T) {
	runtime := New()
	provider1, err := runtime.Mount(nil, "provider-1", databaseDefinition(nil, nil), []byte(`{"id":1}`))
	if err != nil {
		t.Fatalf("mount provider: %v", err)
	}
	waitStable(t, provider1)

	var starts atomic.Int32
	stopStarted := make(chan struct{})
	stopGate := make(chan struct{})
	seenIDs := make(chan int, 2)
	consumerDefinition := Define(
		Descriptor{ID: "test/stop-barrier", Requires: []Requirement{Require(databaseKey)}},
		DecodeJSON[struct{}],
		func(run *Run, _ struct{}) (Dispose, error) {
			database, err := Resolve(run, databaseKey)
			if err != nil {
				return nil, err
			}
			activation := starts.Add(1)
			seenIDs <- database.id
			return func(context.Context) error {
				if activation == 1 {
					close(stopStarted)
					<-stopGate
				}
				return nil
			}, nil
		},
	)
	consumer, err := runtime.Mount(nil, "consumer", consumerDefinition, nil)
	if err != nil {
		t.Fatalf("mount consumer: %v", err)
	}
	waitStable(t, consumer)
	if got := receiveInt(t, seenIDs); got != 1 {
		t.Fatalf("first provider ID = %d, want 1", got)
	}

	disposed := make(chan error, 1)
	go func() { disposed <- provider1.Dispose(testContext(t)) }()
	select {
	case <-stopStarted:
	case <-time.After(time.Second):
		t.Fatal("consumer did not begin stopping")
	}
	assertState(t, consumer, Stopping)

	provider2, err := runtime.Mount(nil, "provider-2", databaseDefinition(nil, nil), []byte(`{"id":2}`))
	if err != nil {
		t.Fatalf("mount replacement provider: %v", err)
	}
	waitStable(t, provider2)
	close(stopGate)
	if err := <-disposed; err != nil {
		t.Fatalf("dispose first provider: %v", err)
	}
	waitStable(t, consumer)
	assertState(t, consumer, Active)
	if got := receiveInt(t, seenIDs); got != 2 {
		t.Fatalf("replacement provider ID = %d, want 2", got)
	}
	if got := starts.Load(); got != 2 {
		t.Fatalf("consumer start count = %d, want 2", got)
	}
	if err := runtime.Dispose(testContext(t)); err != nil {
		t.Fatalf("dispose runtime: %v", err)
	}
}

func TestProviderDisposalDrainsConsumersBeforeClosingResource(t *testing.T) {
	type resource struct{ closed atomic.Bool }
	key := NewKey[*resource]("test/drained-resource")
	value := &resource{}
	providerCleaned := make(chan struct{})
	providerDefinition := Define(
		Descriptor{ID: "test/drained-provider", Provides: []AnyKey{key.Any()}},
		DecodeJSON[struct{}],
		func(run *Run, _ struct{}) (Dispose, error) {
			if _, err := Provide(run, key, value); err != nil {
				return nil, err
			}
			return func(context.Context) error {
				value.closed.Store(true)
				close(providerCleaned)
				return nil
			}, nil
		},
	)

	consumerCleanupStarted := make(chan struct{})
	releaseConsumer := make(chan struct{})
	consumerDefinition := Define(
		Descriptor{ID: "test/drained-consumer", Requires: []Requirement{Require(key)}},
		DecodeJSON[struct{}],
		func(run *Run, _ struct{}) (Dispose, error) {
			if _, err := Resolve(run, key); err != nil {
				return nil, err
			}
			return func(context.Context) error {
				resolved, err := Resolve(run, key)
				if err != nil {
					return err
				}
				if resolved.closed.Load() {
					return errors.New("provider resource closed before consumer cleanup")
				}
				close(consumerCleanupStarted)
				<-releaseConsumer
				return nil
			}, nil
		},
	)

	runtime := New()
	provider, err := runtime.Mount(nil, "drained-provider", providerDefinition, nil)
	if err != nil {
		t.Fatalf("mount provider: %v", err)
	}
	waitStable(t, provider)
	consumer, err := runtime.Mount(nil, "drained-consumer", consumerDefinition, nil)
	if err != nil {
		t.Fatalf("mount consumer: %v", err)
	}
	waitStable(t, consumer)

	disposed := make(chan error, 1)
	go func() { disposed <- provider.Dispose(testContext(t)) }()
	select {
	case <-consumerCleanupStarted:
	case <-time.After(time.Second):
		t.Fatal("consumer cleanup did not start")
	}
	select {
	case <-providerCleaned:
		t.Fatal("provider cleanup ran before consumer cleanup completed")
	default:
	}
	close(releaseConsumer)
	if err := <-disposed; err != nil {
		t.Fatalf("dispose provider: %v", err)
	}
	select {
	case <-providerCleaned:
	case <-time.After(time.Second):
		t.Fatal("provider cleanup did not run after consumer drained")
	}
	if err := runtime.Dispose(testContext(t)); err != nil {
		t.Fatalf("dispose runtime: %v", err)
	}
}

func TestLeaseReadinessAndReplacementDriveNewActivation(t *testing.T) {
	runtime := New()
	leases := make(chan *Lease[*database], 1)
	provider, err := runtime.Mount(nil, "provider", databaseDefinition(leases, nil), []byte(`{"id":1}`))
	if err != nil {
		t.Fatalf("mount provider: %v", err)
	}
	waitStable(t, provider)
	lease := <-leases
	starts := make(chan int, 3)
	var stops atomic.Int32
	consumer, err := runtime.Mount(nil, "consumer", consumerDefinition(starts, &stops), nil)
	if err != nil {
		t.Fatalf("mount consumer: %v", err)
	}
	waitStable(t, consumer)
	if got := receiveInt(t, starts); got != 1 {
		t.Fatalf("initial service ID = %d, want 1", got)
	}
	firstGeneration := consumer.Snapshot().Providers[0].Generation

	if err := lease.SetReady(false); err != nil {
		t.Fatalf("set lease not ready: %v", err)
	}
	waitStable(t, consumer)
	assertState(t, consumer, Pending)
	if err := lease.Replace(&database{id: 2}); err != nil {
		t.Fatalf("replace service: %v", err)
	}
	if err := lease.SetReady(true); err != nil {
		t.Fatalf("set lease ready: %v", err)
	}
	waitStable(t, consumer)
	assertState(t, consumer, Active)
	if got := receiveInt(t, starts); got != 2 {
		t.Fatalf("replacement service ID = %d, want 2", got)
	}
	if generation := consumer.Snapshot().Providers[0].Generation; generation <= firstGeneration {
		t.Fatalf("provider generation = %d, want > %d", generation, firstGeneration)
	}

	if err := provider.Dispose(testContext(t)); err != nil {
		t.Fatalf("dispose provider: %v", err)
	}
	if err := lease.Replace(&database{id: 3}); !errors.Is(err, ErrNotOwner) && !errors.Is(err, ErrClosedLease) {
		t.Fatalf("replace through closed lease error = %v", err)
	}
	if err := runtime.Dispose(testContext(t)); err != nil {
		t.Fatalf("dispose runtime: %v", err)
	}
}

func TestCheckedServiceRefreshDrivesAvailability(t *testing.T) {
	type service struct{ healthy atomic.Bool }
	key := NewKey[*service]("test/checked-service")
	value := &service{}
	leases := make(chan *Lease[*service], 1)
	providerDefinition := Define(
		Descriptor{ID: "test/checked-provider", Provides: []AnyKey{key.Any()}},
		DecodeJSON[struct{}],
		func(run *Run, _ struct{}) (Dispose, error) {
			lease, err := ProvideChecked(run, key, value, func(service *service) bool { return service.healthy.Load() })
			if err == nil {
				leases <- lease
			}
			return nil, err
		},
	)
	consumerDefinition := Define(
		Descriptor{ID: "test/checked-consumer", Requires: []Requirement{Require(key)}},
		DecodeJSON[struct{}],
		func(run *Run, _ struct{}) (Dispose, error) {
			_, err := Resolve(run, key)
			return nil, err
		},
	)
	runtime := New()
	provider, err := runtime.Mount(nil, "checked-provider", providerDefinition, nil)
	if err != nil {
		t.Fatalf("mount provider: %v", err)
	}
	waitStable(t, provider)
	lease := <-leases
	consumer, err := runtime.Mount(nil, "checked-consumer", consumerDefinition, nil)
	if err != nil {
		t.Fatalf("mount consumer: %v", err)
	}
	waitStable(t, consumer)
	assertState(t, consumer, Pending)
	value.healthy.Store(true)
	if err := lease.Refresh(); err != nil {
		t.Fatalf("refresh healthy: %v", err)
	}
	waitStable(t, consumer)
	assertState(t, consumer, Active)
	value.healthy.Store(false)
	if err := lease.Refresh(); err != nil {
		t.Fatalf("refresh unhealthy: %v", err)
	}
	waitStable(t, consumer)
	assertState(t, consumer, Pending)
	if err := runtime.Dispose(testContext(t)); err != nil {
		t.Fatalf("dispose runtime: %v", err)
	}
}

func TestOptionalDependencyAppearingRestartsConsumer(t *testing.T) {
	runtime := New()
	starts := make(chan bool, 2)
	definition := Define(
		Descriptor{ID: "test/optional", Requires: []Requirement{Want(databaseKey)}},
		DecodeJSON[struct{}],
		func(run *Run, _ struct{}) (Dispose, error) {
			_, available := Optional(run, databaseKey)
			starts <- available
			return nil, nil
		},
	)
	consumer, err := runtime.Mount(nil, "consumer", definition, nil)
	if err != nil {
		t.Fatalf("mount consumer: %v", err)
	}
	waitStable(t, consumer)
	assertState(t, consumer, Active)
	if available := <-starts; available {
		t.Fatal("optional service unexpectedly available")
	}
	provider, err := runtime.Mount(nil, "provider", databaseDefinition(nil, nil), []byte(`{"id":1}`))
	if err != nil {
		t.Fatalf("mount provider: %v", err)
	}
	waitStable(t, provider)
	waitStable(t, consumer)
	if available := <-starts; !available {
		t.Fatal("optional service did not appear in replacement activation")
	}
	if err := runtime.Dispose(testContext(t)); err != nil {
		t.Fatalf("dispose runtime: %v", err)
	}
}

func TestRealmIsolationOnlyWakesMatchingConsumers(t *testing.T) {
	runtime := New()
	isolated := Isolate(runtime.Root(), databaseKey)
	rootStarts := make(chan int, 1)
	isolatedStarts := make(chan int, 1)
	var rootStops atomic.Int32
	var isolatedStops atomic.Int32
	rootConsumer, err := runtime.Mount(nil, "root-consumer", consumerDefinition(rootStarts, &rootStops), nil)
	if err != nil {
		t.Fatalf("mount root consumer: %v", err)
	}
	isolatedConsumer, err := runtime.Mount(isolated, "isolated-consumer", consumerDefinition(isolatedStarts, &isolatedStops), nil)
	if err != nil {
		t.Fatalf("mount isolated consumer: %v", err)
	}
	rootProvider, err := runtime.Mount(nil, "root-provider", databaseDefinition(nil, nil), []byte(`{"id":1}`))
	if err != nil {
		t.Fatalf("mount root provider: %v", err)
	}
	waitStable(t, rootProvider)
	waitStable(t, rootConsumer)
	waitStable(t, isolatedConsumer)
	assertState(t, rootConsumer, Active)
	assertState(t, isolatedConsumer, Pending)
	if got := receiveInt(t, rootStarts); got != 1 {
		t.Fatalf("root service ID = %d, want 1", got)
	}

	sharedIsolatedRealm := JoinRealm(runtime.Root(), databaseKey, isolated.realm(databaseKey.id))
	isolatedProvider, err := runtime.Mount(sharedIsolatedRealm, "isolated-provider", databaseDefinition(nil, nil), []byte(`{"id":2}`))
	if err != nil {
		t.Fatalf("mount isolated provider: %v", err)
	}
	waitStable(t, isolatedProvider)
	waitStable(t, isolatedConsumer)
	assertState(t, isolatedConsumer, Active)
	if got := receiveInt(t, isolatedStarts); got != 2 {
		t.Fatalf("isolated service ID = %d, want 2", got)
	}
	if err := runtime.Dispose(testContext(t)); err != nil {
		t.Fatalf("dispose runtime: %v", err)
	}
}

func TestConflictingProviderFailsWithoutReplacingActiveService(t *testing.T) {
	runtime := New()
	provider1, err := runtime.Mount(nil, "provider-1", databaseDefinition(nil, nil), []byte(`{"id":1}`))
	if err != nil {
		t.Fatalf("mount first provider: %v", err)
	}
	waitStable(t, provider1)
	provider2, err := runtime.Mount(nil, "provider-2", databaseDefinition(nil, nil), []byte(`{"id":2}`))
	if err != nil {
		t.Fatalf("mount conflicting provider: %v", err)
	}
	waitStable(t, provider2)
	assertState(t, provider2, Failed)
	service, ok := Lookup(runtime, runtime.Root(), databaseKey)
	if !ok || service.id != 1 {
		t.Fatalf("active service = %+v, available=%v; want provider 1", service, ok)
	}
	if err := runtime.Dispose(testContext(t)); err != nil {
		t.Fatalf("dispose runtime: %v", err)
	}
}

func TestParentRunOwnsChildInstance(t *testing.T) {
	runtime := New()
	childTarget := make(chan *Instance, 1)
	childDefinition := Define(
		Descriptor{ID: "test/child"},
		DecodeJSON[struct{}],
		func(*Run, struct{}) (Dispose, error) { return nil, nil },
	)
	parentDefinition := Define(
		Descriptor{ID: "test/parent"},
		DecodeJSON[struct{}],
		func(run *Run, _ struct{}) (Dispose, error) {
			child, err := run.Mount("child", childDefinition, nil)
			if err != nil {
				return nil, err
			}
			childTarget <- child
			return nil, nil
		},
	)
	parent, err := runtime.Mount(nil, "parent", parentDefinition, nil)
	if err != nil {
		t.Fatalf("mount parent: %v", err)
	}
	waitStable(t, parent)
	child := <-childTarget
	waitStable(t, child)
	if err := parent.Dispose(testContext(t)); err != nil {
		t.Fatalf("dispose parent: %v", err)
	}
	assertState(t, child, Disposed)
	if snapshot := runtime.Snapshot(); snapshot.Instances != 0 || snapshot.Services != 0 {
		t.Fatalf("runtime residue after parent disposal: %+v", snapshot)
	}
	if err := runtime.Dispose(testContext(t)); err != nil {
		t.Fatalf("dispose runtime: %v", err)
	}
}

func TestConcurrentInstanceDisposeRunsCleanupOnce(t *testing.T) {
	runtime := New()
	cleanupStarted := make(chan struct{})
	cleanupGate := make(chan struct{})
	var cleanups atomic.Int32
	definition := Define(
		Descriptor{ID: "test/concurrent-dispose"},
		DecodeJSON[struct{}],
		func(*Run, struct{}) (Dispose, error) {
			return func(context.Context) error {
				if cleanups.Add(1) == 1 {
					close(cleanupStarted)
				}
				<-cleanupGate
				return nil
			}, nil
		},
	)
	instance, err := runtime.Mount(nil, "instance", definition, nil)
	if err != nil {
		t.Fatalf("mount: %v", err)
	}
	waitStable(t, instance)
	const callers = 16
	results := make(chan error, callers)
	for range callers {
		go func() { results <- instance.Dispose(testContext(t)) }()
	}
	select {
	case <-cleanupStarted:
	case <-time.After(time.Second):
		t.Fatal("cleanup did not start")
	}
	close(cleanupGate)
	for range callers {
		if err := <-results; err != nil {
			t.Fatalf("concurrent dispose: %v", err)
		}
	}
	if got := cleanups.Load(); got != 1 {
		t.Fatalf("cleanup count = %d, want 1", got)
	}
}
