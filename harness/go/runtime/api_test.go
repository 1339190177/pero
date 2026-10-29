package runtime

import (
	"context"
	"errors"
	"testing"
)

func TestProviderCanResolveItsStagedService(t *testing.T) {
	runtime := New()
	resolved := make(chan int, 1)
	definition := Define(
		Descriptor{ID: "test/self-resolve", Provides: []AnyKey{databaseKey.Any()}},
		DecodeJSON[struct{}],
		func(run *Run, _ struct{}) (Dispose, error) {
			if _, err := Provide(run, databaseKey, &database{id: 42}); err != nil {
				return nil, err
			}
			service, err := Resolve(run, databaseKey)
			if err != nil {
				return nil, err
			}
			resolved <- service.id
			return nil, nil
		},
	)
	instance, err := runtime.Mount(nil, "provider", definition, nil)
	if err != nil {
		t.Fatalf("mount: %v", err)
	}
	waitStable(t, instance)
	if got := receiveInt(t, resolved); got != 42 {
		t.Fatalf("self-resolved service ID = %d, want 42", got)
	}
	if err := runtime.Dispose(testContext(t)); err != nil {
		t.Fatalf("dispose runtime: %v", err)
	}
}

func TestUndeclaredServiceAccessFailsClosed(t *testing.T) {
	runtime := New()
	accessError := make(chan error, 1)
	definition := Define(
		Descriptor{ID: "test/undeclared"},
		DecodeJSON[struct{}],
		func(run *Run, _ struct{}) (Dispose, error) {
			_, err := Resolve(run, databaseKey)
			accessError <- err
			return nil, nil
		},
	)
	instance, err := runtime.Mount(nil, "instance", definition, nil)
	if err != nil {
		t.Fatalf("mount: %v", err)
	}
	waitStable(t, instance)
	if err := <-accessError; !errors.Is(err, ErrUndeclaredService) {
		t.Fatalf("resolve error = %v, want ErrUndeclaredService", err)
	}
	if err := runtime.Dispose(testContext(t)); err != nil {
		t.Fatalf("dispose runtime: %v", err)
	}
}

func TestCallContextCarriesScopeAndInstance(t *testing.T) {
	runtime := New()
	checked := make(chan bool, 1)
	definition := Define(
		Descriptor{ID: "test/caller-context"},
		DecodeJSON[struct{}],
		func(run *Run, _ struct{}) (Dispose, error) {
			ctx := run.CallContext(context.Background())
			scope, scopeOK := ScopeFromContext(ctx)
			instance, instanceOK := InstanceFromContext(ctx)
			checked <- scopeOK && instanceOK && scope == run.Scope && instance == run.Instance
			return nil, nil
		},
	)
	instance, err := runtime.Mount(nil, "instance", definition, nil)
	if err != nil {
		t.Fatalf("mount: %v", err)
	}
	waitStable(t, instance)
	if ok := <-checked; !ok {
		t.Fatal("caller context did not preserve scope and instance")
	}
	if err := runtime.Dispose(testContext(t)); err != nil {
		t.Fatalf("dispose runtime: %v", err)
	}
}

func TestFrameworkBoundariesRecoverPanics(t *testing.T) {
	t.Run("start", func(t *testing.T) {
		runtime := New()
		definition := Define(
			Descriptor{ID: "test/start-panic"},
			DecodeJSON[struct{}],
			func(*Run, struct{}) (Dispose, error) { panic("start panic") },
		)
		instance, err := runtime.Mount(nil, "instance", definition, nil)
		if err != nil {
			t.Fatalf("mount: %v", err)
		}
		waitStable(t, instance)
		assertState(t, instance, Failed)
		if instance.Snapshot().LastError == "" {
			t.Fatal("start panic was not retained in diagnostics")
		}
		if err := runtime.Dispose(testContext(t)); err != nil {
			t.Fatalf("dispose runtime: %v", err)
		}
	})

	t.Run("dispose", func(t *testing.T) {
		runtime := New()
		definition := Define(
			Descriptor{ID: "test/dispose-panic"},
			DecodeJSON[struct{}],
			func(*Run, struct{}) (Dispose, error) {
				return func(context.Context) error { panic("dispose panic") }, nil
			},
		)
		instance, err := runtime.Mount(nil, "instance", definition, nil)
		if err != nil {
			t.Fatalf("mount: %v", err)
		}
		waitStable(t, instance)
		err = instance.Dispose(testContext(t))
		var panicError *PanicError
		if !errors.As(err, &panicError) {
			t.Fatalf("dispose error = %v, want PanicError", err)
		}
	})
}

func TestFailedProviderNeverPublishesStagedService(t *testing.T) {
	runtime := New()
	definition := Define(
		Descriptor{ID: "test/failed-provider", Provides: []AnyKey{databaseKey.Any()}},
		DecodeJSON[struct{}],
		func(run *Run, _ struct{}) (Dispose, error) {
			if _, err := Provide(run, databaseKey, &database{id: 1}); err != nil {
				return nil, err
			}
			return nil, errors.New("startup failed")
		},
	)
	provider, err := runtime.Mount(nil, "provider", definition, nil)
	if err != nil {
		t.Fatalf("mount: %v", err)
	}
	waitStable(t, provider)
	assertState(t, provider, Failed)
	if _, ok := Lookup(runtime, runtime.Root(), databaseKey); ok {
		t.Fatal("failed provider published a staged service")
	}
	if snapshot := runtime.Snapshot(); snapshot.Services != 0 || len(snapshot.ServiceDetails) != 0 {
		t.Fatalf("service registry residue: %+v", snapshot)
	}
	if err := runtime.Dispose(testContext(t)); err != nil {
		t.Fatalf("dispose runtime: %v", err)
	}
}

func TestRuntimeSnapshotReturnsDeterministicDetails(t *testing.T) {
	runtime := New()
	provider, err := runtime.Mount(nil, "provider", databaseDefinition(nil, nil), []byte(`{"id":1}`))
	if err != nil {
		t.Fatalf("mount: %v", err)
	}
	waitStable(t, provider)
	snapshot := runtime.Snapshot()
	if snapshot.Instances != 1 || len(snapshot.InstanceDetails) != 1 {
		t.Fatalf("instance details = %+v", snapshot)
	}
	if snapshot.Services != 1 || len(snapshot.ServiceDetails) != 1 {
		t.Fatalf("service details = %+v", snapshot)
	}
	service := snapshot.ServiceDetails[0]
	if service.Key != databaseKey.Name() || service.InstanceID != provider.ID() || !service.Ready {
		t.Fatalf("service snapshot = %+v", service)
	}
	if err := runtime.Dispose(testContext(t)); err != nil {
		t.Fatalf("dispose runtime: %v", err)
	}
}
