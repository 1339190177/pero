package session

import (
	"context"
	"errors"
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
