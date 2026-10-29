// Package sessionlogroute carries a private, request-lifetime destination proof.
// Wire provider labels, model names and profile JSON never grant this proof.
package sessionlogroute

import (
	"context"
	"errors"
	"net/url"
	"strings"
	"sync/atomic"
)

type key struct{}
type payloadKey struct{}
type commitKey struct{}

// CommitProof retains one original provider attempt. Its private fields cannot
// be reconstructed from wire metadata or a later caller's context.
type CommitProof struct {
	original context.Context
	permit   *permit
}

var ErrCommitRouteExpired = errors.New("harness: Session-log provider route is no longer admitted")

func CaptureCommitProof(ctx context.Context) *CommitProof {
	proof := &CommitProof{original: ctx}
	if ctx != nil {
		proof.permit, _ = ctx.Value(key{}).(*permit)
	}
	return proof
}

// WithCommitProof is framework-only and applies to the native log append, not
// independent request-extension callbacks or later provider attempts.
func WithCommitProof(ctx context.Context, proof *CommitProof) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	return context.WithValue(ctx, commitKey{}, proof)
}

// CheckCommitProof runs after Session notification admission, before taking
// Agent/store/log locks. A successful check is this route's admission point;
// later cancellation cannot undo an already admitted in-memory fact. The
// captured owner predicate is bounded registry bookkeeping, never provider code.
// Unmarked ordinary framework appends retain their existing behavior.
func CheckCommitProof(ctx context.Context) error {
	if ctx == nil {
		return nil
	}
	proof, present := ctx.Value(commitKey{}).(*CommitProof)
	if !present {
		return nil
	}
	if proof == nil || proof.original == nil {
		return ErrCommitRouteExpired
	}
	if err := proof.original.Err(); err != nil {
		return err
	}
	p := proof.permit
	if p == nil || !p.active.Load() || p.original.Err() != nil || (p.owner != nil && !p.owner()) {
		return ErrCommitRouteExpired
	}
	return nil
}

func MarkPayload(ctx context.Context) context.Context {
	return context.WithValue(ctx, payloadKey{}, true)
}
func HasPayload(ctx context.Context) bool {
	if ctx == nil {
		return false
	}
	marked, _ := ctx.Value(payloadKey{}).(bool)
	return marked
}

type ownerKey struct{}

// BindOwner narrows only extra Session-log disclosure, not the existing
// prepared model call lifetime or its ordinary generation semantics.
func BindOwner(ctx context.Context, current func() bool) context.Context {
	return context.WithValue(Exclude(ctx), ownerKey{}, current)
}

type permit struct {
	original context.Context
	owner    func() bool
	active   atomic.Bool
}

// Destination permits only the official HTTPS API origin. Custom compatible
// endpoints remain usable, but cannot receive this additional Session history.
func Destination(endpoint string) bool {
	u, err := url.Parse(endpoint)
	return err == nil && u.Scheme == "https" && strings.EqualFold(u.Hostname(), "api.deepseek.com") && (u.Port() == "" || u.Port() == "443") && u.User == nil && u.RawQuery == "" && u.Fragment == ""
}

// Exclude shadows an inherited permit and append proof at every new provider
// attempt. A proof is local to one native append, never a nested call's owner.
func Exclude(ctx context.Context) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	return context.WithValue(context.WithValue(context.WithValue(ctx, key{}, (*permit)(nil)), payloadKey{}, false), commitKey{}, struct{}{})
}

// Begin is called only by a provider-owned, explicitly enrolled adapter. The
// returned revoker invalidates retained contexts when that attempt finishes.
func Begin(ctx context.Context, endpoint string) (context.Context, func()) {
	if ctx == nil {
		ctx = context.Background()
	}
	ctx = Exclude(ctx)
	if !Destination(endpoint) || ctx.Err() != nil {
		return ctx, func() {}
	}
	owner, _ := ctx.Value(ownerKey{}).(func() bool)
	if owner != nil && !owner() {
		return ctx, func() {}
	}
	p := &permit{original: ctx, owner: owner}
	p.active.Store(true)
	return context.WithValue(ctx, key{}, p), func() { p.active.Store(false) }
}
func Allowed(ctx context.Context) bool {
	if ctx == nil || ctx.Err() != nil {
		return false
	}
	p, _ := ctx.Value(key{}).(*permit)
	return p != nil && p.active.Load() && p.original.Err() == nil && (p.owner == nil || p.owner())
}
