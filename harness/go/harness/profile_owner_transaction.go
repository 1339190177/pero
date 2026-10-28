package harness

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"sort"
	"sync"
)

// ProfileOwnerIdleReservation is an exact-incarnation reservation supplied by
// a trusted owner. Retire joins ownership release independently of the caller;
// Offer replays retained derived work into a sealed candidate without consuming
// the receipt. Release restores the exact old incarnation after a busy abort.
// Projection cache is the first participant; authority roots never enroll.
type ProfileOwnerIdleReservation interface {
	Release() (func(), error)
	Retire(context.Context) error
	Offer(ProfileOwner) error
}

// ProfileOwnerTransaction owns the registry's existing serialization gate
// across Loader evaluation and policy settlement. It must be settled with
// Commit or Abort; registry Close also joins and cleans abandoned transactions.
// No request context is retained across RPC boundaries.
type ProfileOwnerTransaction struct {
	mu           sync.Mutex
	registry     *ProfileOwnerRegistry
	ids          []string
	scoped       bool
	original     map[string]*profileOwnerEntry
	reservations map[string]ProfileOwnerIdleReservation
	receipts     map[string][]ProfileOwnerIdleReservation
	staged       map[string]*profileOwnerEntry
	changed      []string
	retired      map[string]bool
	planned      bool
	rolledBack   bool
	decided      bool
	finished     bool
	result       error
	replay       []func()
}

// BeginSelected reserves all selected idle participants before any evaluation
// side effects. Unrelated edits may receive a retryable busy refusal. Equality
// is established only from evaluated rows and releases the same incarnation.
func (r *ProfileOwnerRegistry) BeginSelected(ctx context.Context, ids []string) (*ProfileOwnerTransaction, error) {
	return r.beginOwnerTransaction(ctx, ids, true)
}
func (r *ProfileOwnerRegistry) beginOwnerTransaction(ctx context.Context, ids []string, scoped bool) (*ProfileOwnerTransaction, error) {
	if r == nil {
		return nil, ErrProfileOwnerUnavailable
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if ctx.Value(profileOwnerCallContextKey{}) == r {
		return nil, ErrProfileOwnerReentrant
	}
	if err := r.acquire(ctx); err != nil {
		return nil, err
	}
	tx := &ProfileOwnerTransaction{registry: r, ids: append([]string(nil), ids...), scoped: scoped,
		original: map[string]*profileOwnerEntry{}, reservations: map[string]ProfileOwnerIdleReservation{}, receipts: map[string][]ProfileOwnerIdleReservation{},
		staged: map[string]*profileOwnerEntry{}, retired: map[string]bool{}}
	r.mu.Lock()
	if r.closed || r.failure != nil {
		r.mu.Unlock()
		r.gate <- struct{}{}
		return nil, ErrProfileOwnerUnavailable
	}
	selected := map[string]bool{}
	for _, id := range ids {
		if r.entries[id] == nil || selected[id] {
			r.mu.Unlock()
			r.gate <- struct{}{}
			return nil, fmt.Errorf("harness: invalid profile owner selection %q", id)
		}
		selected[id] = true
	}
	for id, entry := range r.entries {
		if !scoped || selected[id] {
			tx.original[id] = entry
		}
	}
	tx.mu.Lock()
	r.transaction = tx
	r.mu.Unlock()
	keys := make([]string, 0, len(tx.original))
	for id := range tx.original {
		keys = append(keys, id)
	}
	sort.Strings(keys)
	var err error
	for _, id := range keys {
		e := tx.original[id]
		if e.owner != nil && e.registration.Reserve != nil {
			reservation, reserveErr := e.registration.Reserve(e.owner)
			err = reserveErr
			if reservation != nil {
				tx.reservations[id] = reservation
			}
			if err == nil && reservation == nil {
				err = errors.New("harness: idle owner returned no reservation")
			}
			if err != nil {
				break
			}
		}
	}
	if err == nil {
		err = ctx.Err()
	}
	r.mu.Lock()
	closed := r.closed
	r.mu.Unlock()
	if closed {
		err = errors.Join(err, ErrProfileOwnerUnavailable)
	}
	if err != nil {
		releaseErr := tx.releaseReservations()
		err = errors.Join(err, releaseErr)
		if releaseErr != nil {
			tx.failCleanup(releaseErr)
		}
		tx.finishLocked(err)
		tx.mu.Unlock()
		return nil, err
	}
	tx.mu.Unlock()
	return tx, nil
}

func (t *ProfileOwnerTransaction) Validate(ctx context.Context, rows []ProfileOwnerRow) error {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.finished || t.planned {
		return ErrProfileOwnerUnavailable
	}
	_, err := t.registry.plan(rows, t.ids, t.scoped)
	if ctx != nil {
		err = errors.Join(err, ctx.Err())
	}
	return err
}

// Stage retires changed owners and mounts sealed replacements. It never
// activates them. On failure the caller restores JS/policies and calls Abort;
// Rollback may be used earlier to prepare fresh sealed native rollback owners.
func (t *ProfileOwnerTransaction) Stage(ctx context.Context, rows []ProfileOwnerRow) error {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.finished || t.planned {
		return ErrProfileOwnerUnavailable
	}
	if ctx == nil {
		ctx = context.Background()
	}
	r := t.registry
	next, err := r.plan(rows, t.ids, t.scoped)
	if err != nil {
		return err
	}
	if err = ctx.Err(); err != nil {
		return err
	}
	for id, old := range t.original {
		row := next[id]
		if old.row.Disabled != row.Disabled || !bytes.Equal(old.row.Config, row.Config) {
			t.changed = append(t.changed, id)
		}
	}
	sort.Strings(t.changed)
	t.planned = true
	r.mu.Lock()
	if r.closed {
		r.mu.Unlock()
		return ErrProfileOwnerUnavailable
	}
	for _, id := range t.changed {
		t.original[id].lease.admitted = false
	}
	r.mu.Unlock()
	for _, id := range t.changed {
		old := t.original[id]
		old.lease.closeReservationAdmission()
		if old.owner != nil {
			if reservation := t.reservations[id]; reservation != nil {
				err = reservation.Retire(context.Background())
				t.receipts[id] = append(t.receipts[id], reservation)
				delete(t.reservations, id)
			} else {
				err = old.owner.Dispose(context.Background())
			}
			if err != nil {
				return t.failCleanup(fmt.Errorf("retire profile owner %q: %w", id, err))
			}
		}
		t.retired[id] = true
		candidate := &profileOwnerEntry{registration: old.registration, row: next[id], lease: &ProfileOwnerLease{registry: r, id: id, reservationEligible: true}}
		t.staged[id] = candidate
		if !candidate.row.Disabled {
			candidate.owner, err = r.mount(ctx, candidate)
			if err == nil && candidate.owner == nil {
				err = errors.New("harness: profile owner factory returned no owner")
			}
			if err != nil {
				return err
			}
			for _, receipt := range t.receipts[id] {
				if err = receipt.Offer(candidate.owner); err != nil {
					return err
				}
			}
		}
	}
	return ctx.Err()
}

// Rollback prepares fresh sealed owners for the prior configuration. It does
// not release unchanged reservations or admit any restored owner; Abort does
// that only after all other participants have restored successfully.
func (t *ProfileOwnerTransaction) Rollback() error {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.rollbackLocked()
}
func (t *ProfileOwnerTransaction) rollbackLocked() error {
	if t.finished || t.decided {
		return ErrProfileOwnerUnavailable
	}
	if t.rolledBack {
		return t.result
	}
	r := t.registry
	var joined error
	for i := len(t.changed) - 1; i >= 0; i-- {
		id := t.changed[i]
		candidate := t.staged[id]
		if candidate == nil {
			continue
		}
		if candidate.owner == nil {
			candidate.lease.closeReservationAdmission()
			continue
		}
		var err error
		if candidate.registration.Reserve != nil {
			reservation, reserveErr := candidate.registration.Reserve(candidate.owner)
			err = reserveErr
			if err == nil && reservation == nil {
				err = errors.New("harness: idle owner returned no rollback reservation")
			}
			if err == nil {
				candidate.lease.closeReservationAdmission()
				err = reservation.Retire(context.Background())
				t.receipts[id] = append(t.receipts[id], reservation)
			} else if reservation != nil {
				// A failed reservation may still own admission. Release its exact
				// token before retaining the candidate for final cleanup.
				replay, releaseErr := reservation.Release()
				if replay != nil {
					t.replay = append(t.replay, replay)
				}
				err = errors.Join(err, releaseErr)
			}
		} else {
			candidate.lease.closeReservationAdmission()
			err = candidate.owner.Dispose(context.Background())
		}
		candidate.lease.closeReservationAdmission()
		if err != nil {
			r.retained = append(r.retained, candidate.owner)
			joined = errors.Join(joined, err)
		}
	}
	if joined != nil {
		return t.failCleanup(joined)
	}
	t.staged = map[string]*profileOwnerEntry{}
	r.mu.Lock()
	closed := r.closed
	failure := r.failure
	r.mu.Unlock()
	if closed || failure != nil {
		return errors.Join(ErrProfileOwnerUnavailable, failure)
	}
	for _, id := range t.changed {
		if !t.retired[id] {
			continue
		}
		old := t.original[id]
		restored := &profileOwnerEntry{registration: old.registration, row: cloneProfileOwnerRow(old.row), lease: &ProfileOwnerLease{registry: r, id: id, reservationEligible: true}}
		t.staged[id] = restored
		if !restored.row.Disabled {
			var err error
			restored.owner, err = r.mount(context.Background(), restored)
			if err == nil && restored.owner == nil {
				err = errors.New("harness: profile owner rollback returned no owner")
			}
			if err == nil {
				for _, receipt := range t.receipts[id] {
					if err = receipt.Offer(restored.owner); err != nil {
						break
					}
				}
			}
			if err != nil {
				restored.lease.closeReservationAdmission()
				return t.failCleanup(err)
			}
		}
	}
	t.rolledBack = true
	return nil
}

// Commit is the irreversible common decision. After it starts activating an
// owner, no rollback is permitted: an unexpected trusted activation error is
// retained as a fail-closed cleanup fault. Transport callers reconcile the
// same transaction receipt, rather than replaying an ambiguous commit.
func (t *ProfileOwnerTransaction) Commit() error {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.finished {
		return t.result
	}
	if !t.planned || t.rolledBack {
		return ErrProfileOwnerUnavailable
	}
	return t.publishLocked()
}

// Abort settles a failed outer transaction after JS/policy restoration. If no
// evaluated owner changed, it only releases reservations, preserving leases,
// objects, options and registry revision.
func (t *ProfileOwnerTransaction) Abort() error {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.finished {
		return t.result
	}
	if t.decided {
		return ErrProfileOwnerUnavailable
	}
	if t.planned && len(t.changed) > 0 && !t.rolledBack {
		if err := t.rollbackLocked(); err != nil {
			return t.fenceAndFinishLocked(err)
		}
	}
	return t.publishLocked()
}

func (t *ProfileOwnerTransaction) publishLocked() error {
	r := t.registry
	r.mu.Lock()
	unavailable := r.closed || r.failure != nil
	r.mu.Unlock()
	if unavailable {
		return t.fenceAndFinishLocked(ErrProfileOwnerUnavailable)
	}
	t.decided = true
	var joined error
	// Old unchanged cache owners remain reserved until the common decision.
	for _, id := range t.changed {
		entry := t.staged[id]
		if entry != nil && entry.owner != nil && entry.registration.Activate != nil {
			replay, err := entry.registration.Activate(entry.owner)
			if replay != nil {
				t.replay = append(t.replay, replay)
			}
			joined = errors.Join(joined, err)
		}
	}
	joined = errors.Join(joined, t.releaseReservations())
	if joined != nil {
		return t.fenceAndFinishLocked(joined)
	}
	r.mu.Lock()
	// A concurrent Close owns the admitted decision and joins it through gate.
	for _, id := range t.changed {
		if entry := t.staged[id]; entry != nil {
			r.entries[id] = entry
		}
		if entry := r.entries[id]; entry != nil {
			entry.lease.admitted = !r.closed
		}
	}
	if len(t.changed) > 0 {
		r.revision++
	}
	r.mu.Unlock()
	for _, id := range t.changed {
		e := r.entries[id]
		if e.registration.Published != nil {
			e.registration.Published(e.owner)
		}
	}
	t.finishLocked(nil)
	return nil
}
func (t *ProfileOwnerTransaction) releaseReservations() error {
	var joined error
	ids := make([]string, 0, len(t.reservations))
	for id := range t.reservations {
		ids = append(ids, id)
	}
	sort.Sort(sort.Reverse(sort.StringSlice(ids)))
	for _, id := range ids {
		replay, err := t.reservations[id].Release()
		if replay != nil {
			t.replay = append(t.replay, replay)
		}
		joined = errors.Join(joined, err)
		delete(t.reservations, id)
	}
	return joined
}
func (t *ProfileOwnerTransaction) fenceAndFinishLocked(err error) error {
	t.failCleanup(err)
	for _, entry := range t.staged {
		entry.lease.closeReservationAdmission()
		if entry.owner != nil {
			t.registry.retained = append(t.registry.retained, entry.owner)
		}
	}
	t.staged = map[string]*profileOwnerEntry{}
	t.finishLocked(err)
	return err
}

func (t *ProfileOwnerTransaction) failCleanup(err error) error {
	r := t.registry
	r.mu.Lock()
	r.failure = errors.Join(r.failure, err)
	r.mu.Unlock()
	t.result = errors.Join(t.result, err)
	return err
}
func (t *ProfileOwnerTransaction) finishLocked(err error) {
	if t.finished {
		return
	}
	t.finished = true
	t.result = err
	r := t.registry
	r.mu.Lock()
	if r.transaction == t {
		r.transaction = nil
	}
	r.mu.Unlock()
	r.gate <- struct{}{}
	for _, replay := range t.replay {
		replay()
	}
	t.replay = nil
}
func (t *ProfileOwnerTransaction) close() {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.finished {
		return
	}
	// Admission stays sealed. No restoration is useful during shutdown; Close
	// owns old entries and this backlog, including any uncertain disposer.
	for _, entry := range t.staged {
		entry.lease.closeReservationAdmission()
		if entry.owner != nil {
			t.registry.retained = append(t.registry.retained, entry.owner)
		}
	}
	t.finishLocked(ErrProfileOwnerUnavailable)
}

func (r *ProfileOwnerRegistry) reconcileWithIdleOwners(ctx context.Context, ids []string, rows []ProfileOwnerRow, scoped bool) error {
	if r == nil {
		return ErrProfileOwnerUnavailable
	}
	r.mu.Lock()
	hasIdle := false
	for id, e := range r.entries {
		if e.registration.Reserve != nil {
			if !scoped {
				hasIdle = true
				break
			}
			for _, selected := range ids {
				if id == selected {
					hasIdle = true
					break
				}
			}
		}
	}
	r.mu.Unlock()
	if !hasIdle {
		return r.reconcile(ctx, ids, rows, scoped)
	}
	tx, err := r.beginOwnerTransaction(ctx, ids, scoped)
	if err != nil {
		return err
	}
	if err = tx.Stage(ctx, rows); err != nil {
		return errors.Join(err, tx.Abort())
	}
	return tx.Commit()
}

// Fence retains candidate ownership without re-admission after a failed peer
// rollback. Shutdown owns the cleanup backlog; no stale generation is revived.
func (t *ProfileOwnerTransaction) Fence(cause error) error {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.finished {
		return t.result
	}
	if cause == nil {
		cause = ErrProfileOwnerUnavailable
	}
	return t.fenceAndFinishLocked(cause)
}
