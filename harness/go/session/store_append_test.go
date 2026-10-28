package session

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestSessionStoreAppendExactRetiredWhileWaitingForNotificationLane(t *testing.T) {
	store := NewSessionStore()
	captured, err := store.Create("exact-append", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	captured.notifyMu.Lock()
	done := make(chan error, 1)
	go func() {
		_, err := store.AppendLogExact(captured, "session/log-accepted", map[string]any{"throughSeq": 0})
		done <- err
	}()
	if !store.RemoveExact(captured) {
		t.Fatal("remove failed")
	}
	replacement, err := store.Create("exact-append", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	captured.notifyMu.Unlock()
	select {
	case err := <-done:
		if !errors.Is(err, ErrSessionNotCurrent) {
			t.Fatalf("late append: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("append blocked")
	}
	if len(captured.Events()) != 0 || len(replacement.Events()) != 0 {
		t.Fatal("late acceptance reached retired or replacement Session")
	}
}

func TestSessionStoreAppendExactReleasesMembershipBeforeObservers(t *testing.T) {
	store := NewSessionStore()
	captured, err := store.Create("observe-append", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	entered, release := make(chan struct{}), make(chan struct{})
	captured.Subscribe(func(event Event) {
		if _, ok := store.Get(captured.Header().ID); !ok {
			t.Error("committed observer did not see current Session")
		}
		if len(captured.Events()) != 1 {
			t.Error("observer did not see committed event")
		}
		close(entered)
		<-release
	})
	done := make(chan error, 1)
	go func() {
		_, err := store.AppendLogExact(captured, "session/log-accepted", map[string]any{"throughSeq": 0})
		done <- err
	}()
	<-entered
	removed := make(chan bool, 1)
	go func() { removed <- store.RemoveExact(captured) }()
	select {
	case ok := <-removed:
		if !ok {
			t.Error("remove failed")
		}
	case <-time.After(time.Second):
		close(release)
		t.Fatal("store membership lock leaked into observer")
	}
	close(release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if len(captured.Events()) != 1 {
		t.Fatal("accepted fact lost on later removal")
	}
}

func TestSessionStoreAppendExactAllowsTailGrowthAndSafeObserverRemoval(t *testing.T) {
	store := NewSessionStore()
	captured, err := store.Create("tail-append", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if _, err = captured.Append("synthetic/unrelated", map[string]any{"value": 1}); err != nil {
		t.Fatal(err)
	}
	captured.Subscribe(func(event Event) {
		if event.Type == "session/log-accepted" && !store.RemoveExact(captured) {
			t.Error("observer could not remove current Session")
		}
	})
	event, err := store.AppendLogExact(captured, "session/log-accepted", map[string]any{"throughSeq": 0})
	if err != nil || event.Seq != 2 {
		t.Fatalf("tail acceptance: %+v %v", event, err)
	}
	if _, err = store.AppendLogExact(captured, "session/log-accepted", nil); !errors.Is(err, ErrSessionNotCurrent) {
		t.Fatalf("removed acceptance: %v", err)
	}
}

func TestSessionStoreAppendExactRejectsSurfaceAndEncodingFailure(t *testing.T) {
	store := NewSessionStore()
	captured, err := store.Create("invalid-append", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if _, err = store.AppendLogExact(captured, "user/message", nil); err == nil {
		t.Fatal("surface accepted")
	}
	if _, err = store.AppendLogExact(captured, "session/log-accepted", make(chan int)); err == nil {
		t.Fatal("unserializable accepted")
	}
	if len(captured.Events()) != 0 || !store.RemoveExact(captured) {
		t.Fatal("failed append mutated or stranded store")
	}
}

func TestSessionStoreAppendExactCancellationWhileWaiting(t *testing.T) {
	store := NewSessionStore()
	captured, err := store.Create("cancel-append", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	captured.notifyMu.Lock()
	done := make(chan error, 1)
	go func() {
		_, err := store.AppendLogExactContext(ctx, captured, "session/log-accepted", nil)
		done <- err
	}()
	cancel()
	captured.notifyMu.Unlock()
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled append: %v", err)
	}
	if len(captured.Events()) != 0 {
		t.Fatal("canceled acceptance committed")
	}
}
