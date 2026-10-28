package session

import (
	"context"
	"errors"
	"fmt"
	"os"
	"testing"
)

func TestJSONLIdentityPreflightDoesNotReadOrRewriteExistingLog(t *testing.T) {
	p, err := NewJSONLSessionPersistence(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	id := SessionID("reserved-child")
	if found, err := p.Exists(t.Context(), id); err != nil || found {
		t.Fatalf("missing identity=(%v,%v)", found, err)
	}
	malformed := []byte("not a valid session log\n")
	if err = os.WriteFile(p.path(id), malformed, 0600); err != nil {
		t.Fatal(err)
	}
	if found, err := p.Exists(t.Context(), id); err != nil || !found {
		t.Fatalf("occupied malformed identity=(%v,%v)", found, err)
	}
	got, err := os.ReadFile(p.path(id))
	if err != nil || string(got) != string(malformed) {
		t.Fatalf("identity lookup changed data: %q %v", got, err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if found, err := p.Exists(ctx, id); found || !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled identity=(%v,%v)", found, err)
	}
}

func TestJSONLUnterminatedRecordRejectsLoadAndAttach(t *testing.T) {
	for _, withEvent := range []bool{false, true} {
		t.Run(fmt.Sprint(withEvent), func(t *testing.T) {
			p, err := NewJSONLSessionPersistence(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			s := NewSession("unterminated", "")
			detach, err := p.Attach(t.Context(), s)
			if err != nil {
				t.Fatal(err)
			}
			if withEvent {
				if _, err := s.Append("fixture/one", map[string]int{"n": 1}); err != nil {
					t.Fatal(err)
				}
			}
			if err := detach(); err != nil {
				t.Fatal(err)
			}
			loaded, err := p.Load(t.Context(), s.Header().ID)
			if err != nil {
				t.Fatal(err)
			}
			path := p.path(s.Header().ID)
			raw, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			torn := raw[:len(raw)-1]
			if err := os.WriteFile(path, torn, 0600); err != nil {
				t.Fatal(err)
			}
			if _, err := p.Load(t.Context(), s.Header().ID); err == nil {
				t.Fatal("accepted unterminated record")
			}
			if close, err := p.Attach(t.Context(), loaded); err == nil {
				close()
				t.Fatal("attached to unterminated record")
			}
			after, err := os.ReadFile(path)
			if err != nil || string(after) != string(torn) {
				t.Fatalf("failed admission changed bytes: %q %v", after, err)
			}
		})
	}
}
