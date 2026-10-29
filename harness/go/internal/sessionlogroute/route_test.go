package sessionlogroute

import (
	"context"
	"errors"
	"testing"
)

func TestDestinationAndLifetime(t *testing.T) {
	for _, endpoint := range []string{"http://api.deepseek.com", "https://api.deepseek.com.evil.test", "https://other.test", "https://api.deepseek.com:444", "https://user@api.deepseek.com", "https://api.deepseek.com/?target=other", "https://api.deepseek.com/#fragment"} {
		ctx, revoke := Begin(t.Context(), endpoint)
		if Allowed(ctx) {
			t.Fatalf("admitted %q", endpoint)
		}
		revoke()
	}
	for _, endpoint := range []string{"https://api.deepseek.com", "https://api.deepseek.com/v1/", "https://api.deepseek.com:443/chat/completions"} {
		original, cancel := context.WithCancel(t.Context())
		ctx, revoke := Begin(original, endpoint)
		if !Allowed(ctx) {
			t.Fatalf("rejected %q", endpoint)
		}
		if Allowed(context.Background()) {
			t.Fatal("unbound context admitted")
		}
		cancel()
		if Allowed(ctx) {
			t.Fatal("cancel retained permit")
		}
		revoke()
	}
	ctx, revoke := Begin(t.Context(), "https://api.deepseek.com")
	revoke()
	revoke()
	if Allowed(ctx) {
		t.Fatal("revocation retained permit")
	}
}

func TestNestedAttemptsCannotInheritDisclosure(t *testing.T) {
	parent, revoke := Begin(t.Context(), "https://api.deepseek.com")
	defer revoke()
	if !Allowed(parent) {
		t.Fatal("parent missing")
	}
	if Allowed(Exclude(parent)) {
		t.Fatal("generic attempt inherited permit")
	}
	child, done := Begin(parent, "https://other.invalid")
	defer done()
	if Allowed(child) {
		t.Fatal("custom destination inherited permit")
	}
	stale := BindOwner(parent, func() bool { return false })
	child, done = Begin(stale, "https://api.deepseek.com")
	defer done()
	if Allowed(child) {
		t.Fatal("stale owner inherited permit")
	}
	if !Allowed(parent) {
		t.Fatal("child revoked unrelated parent")
	}
}

func TestCommitProofRetainsOriginalAttemptAndCancellation(t *testing.T) {
	original, cancel := context.WithCancel(t.Context())
	defer cancel()
	permit, revoke := Begin(original, "https://api.deepseek.com")
	defer revoke()
	proof := CaptureCommitProof(permit)
	accepted := WithCommitProof(t.Context(), proof)
	if err := CheckCommitProof(accepted); err != nil {
		t.Fatal(err)
	}
	cancel()
	revoke()
	if err := CheckCommitProof(accepted); !errors.Is(err, context.Canceled) {
		t.Fatalf("original cancellation must precede revoked-route error: %v", err)
	}
	if err := CheckCommitProof(t.Context()); err != nil {
		t.Fatalf("unmarked append changed: %v", err)
	}
	if err := CheckCommitProof(Exclude(accepted)); err != nil {
		t.Fatalf("nested attempt inherited another append's proof: %v", err)
	}
	if err := CheckCommitProof(WithCommitProof(t.Context(), CaptureCommitProof(t.Context()))); !errors.Is(err, ErrCommitRouteExpired) {
		t.Fatalf("missing original permit admitted: %v", err)
	}
}

