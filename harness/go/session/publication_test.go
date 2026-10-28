package session

import (
	"errors"
	"reflect"
	"strings"
	"testing"
)

func TestSessionLogPublicationPrecedesSubscribers(t *testing.T) {
	s := NewSession("publication", "")
	var order []string
	published := false
	for _, label := range []string{"first", "second"} {
		s.Subscribe(func(event Event) {
			if !published {
				t.Error("subscriber ran before native publication")
			}
			if events := s.Events(); len(events) != 1 || events[0].Type != "job/registered" {
				t.Errorf("committed log = %#v", events)
			}
			order = append(order, label)
		})
	}
	event, err := s.AppendLogWithPublication("job/registered", map[string]string{"jobId": "fixture"}, func() {
		// Both reads require the Session log mutex, so this also proves release.
		if len(s.Events()) != 1 || len(s.SurfaceSeqs()) != 0 {
			t.Error("publication did not see log-only commit")
		}
		published = true
		order = append(order, "publish")
	}, func() { t.Error("unexpected fallback") })
	if err != nil || event.Seq != 1 {
		t.Fatalf("append = %#v, %v", event, err)
	}
	if !reflect.DeepEqual(order, []string{"publish", "first", "second"}) {
		t.Fatalf("order = %v", order)
	}
}

func TestSessionLogPublicationFailureAndRecovery(t *testing.T) {
	t.Run("precommit", func(t *testing.T) {
		s := NewSession("failure", "")
		called := false
		publisher := func() { called = true }
		event, err := s.AppendLogWithPublication("job/settled", make(chan int), publisher, publisher)
		if err == nil || event.Seq != 0 || called || len(s.Events()) != 0 {
			t.Fatalf("failed append published state: %#v %v %v", event, err, called)
		}
	})
	t.Run("panic recovered before subscriber", func(t *testing.T) {
		s := NewSession("recovery", "")
		done := make(chan struct{})
		observed := false
		s.Subscribe(func(Event) {
			select {
			case <-done:
				observed = true
			default:
				t.Error("subscriber entered before recovery released waiter")
			}
		})
		event, err := s.AppendLogWithPublication("job/settled", struct{}{}, func() { panic("publication") }, func() { close(done) })
		if err == nil || !strings.Contains(err.Error(), "publication") || event.Seq != 1 || !observed {
			t.Fatalf("recovered append = %#v %v observed=%v", event, err, observed)
		}
	})
	t.Run("double panic explicit", func(t *testing.T) {
		s := NewSession("double-failure", "")
		observed := false
		s.Subscribe(func(Event) { observed = true })
		event, err := s.AppendLogWithPublication("job/settled", struct{}{}, func() { panic("primary") }, func() { panic("fallback") })
		if err == nil || !strings.Contains(err.Error(), "primary") || !strings.Contains(err.Error(), "fallback") || event.Seq != 1 || observed {
			t.Fatalf("double failure = %#v %v observed=%v", event, err, observed)
		}
	})
	t.Run("surface prohibited", func(t *testing.T) {
		s := NewSession("surface", "")
		called := false
		event, err := s.AppendLogWithPublication("user/message", struct{}{}, func() { called = true }, func() { called = true })
		if err == nil || event.Seq != 0 || called {
			t.Fatal("surface event admitted")
		}
		if _, err := s.Append("fixture", struct{}{}); err != nil && !errors.Is(err, ErrSessionChanged) {
			t.Fatal(err)
		}
	})
}
