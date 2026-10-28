package harness

import (
	"context"
	"fmt"
	"sync"
)

// ProfileOwnerBusyError is a retryable refusal, never a request to cancel
// accepted work. Only trusted idle-owner registrations opt into this barrier.
type ProfileOwnerBusyError struct {
	OwnerID          string
	ActiveOperations int
	Reserved         bool
}

func (e *ProfileOwnerBusyError) Error() string {
	if e.Reserved {
		return fmt.Sprintf("harness: profile owner %q is reserved for replacement", e.OwnerID)
	}
	return fmt.Sprintf("harness: profile owner %q is busy: %d active operations", e.OwnerID, e.ActiveOperations)
}
func (*ProfileOwnerBusyError) ToolErrorCode() string { return "OWNER_BUSY" }

// BeginOperation borrows this exact committed incarnation through completion.
// The release must cover post-body policy, presentation and finalization, not
// merely the callback body. A canceled caller must still release only after
// its accepted work settles. Non-idle registrations retain legacy behavior.
func (l *ProfileOwnerLease) BeginOperation(ctx context.Context) (context.Context, func(), error) {
	if ctx == nil {
		ctx = context.Background()
	}
	noop := func() {}
	if l == nil || l.registry == nil {
		return ctx, noop, nil
	}
	r := l.registry
	r.mu.Lock()
	if !l.activeLocked() {
		r.mu.Unlock()
		return ctx, nil, ErrProfileOwnerUnavailable
	}
	if r.entries[l.id].registration.Reserve == nil {
		r.mu.Unlock()
		return ctx, noop, nil
	}
	if l.idleReservation != nil {
		r.mu.Unlock()
		return ctx, nil, &ProfileOwnerBusyError{OwnerID: l.id, Reserved: true}
	}
	if err := ctx.Err(); err != nil {
		r.mu.Unlock()
		return ctx, nil, err
	}
	l.operations++
	r.mu.Unlock()
	var once sync.Once
	release := func() {
		once.Do(func() {
			r.mu.Lock()
			l.operations--
			if l.operations == 0 && l.retirementDone != nil {
				select {
				case <-l.retirementDone:
				default:
					close(l.retirementDone)
				}
			}
			r.mu.Unlock()
		})
	}
	return l.CallContext(ctx), release, nil
}

// ProfileOwnerIdleLease is a nonblocking exact-incarnation reservation. It
// carries no resource authority: the trusted owner still owns its disposer.
type ProfileOwnerIdleLease struct {
	lease    *ProfileOwnerLease
	released bool // guarded by registry.mu
	retired  bool
}

// TryReserveIdle refuses accepted work without changing its admission or
// cancellation. Sealed rollback candidates are reservable, but retired leases
// can never be reused. No registry gate is acquired or waited on here.
func (l *ProfileOwnerLease) TryReserveIdle() (*ProfileOwnerIdleLease, error) {
	if l == nil || l.registry == nil {
		return nil, ErrProfileOwnerUnavailable
	}
	r := l.registry
	r.mu.Lock()
	defer r.mu.Unlock()
	entry := r.entries[l.id]
	if r.closed || r.failure != nil || l.retired || !l.reservationEligible || entry == nil || entry.registration.Reserve == nil {
		return nil, ErrProfileOwnerUnavailable
	}
	if l.idleReservation != nil {
		return nil, &ProfileOwnerBusyError{OwnerID: l.id, Reserved: true}
	}
	if l.operations != 0 {
		return nil, &ProfileOwnerBusyError{OwnerID: l.id, ActiveOperations: l.operations}
	}
	reservation := &ProfileOwnerIdleLease{lease: l}
	l.idleReservation = reservation
	return reservation, nil
}

func (r *ProfileOwnerIdleLease) Release() error {
	if r == nil || r.lease == nil {
		return ErrProfileOwnerUnavailable
	}
	l := r.lease
	l.registry.mu.Lock()
	defer l.registry.mu.Unlock()
	if r.released {
		return nil
	}
	if r.retired || l.retired || l.idleReservation != r {
		return ErrProfileOwnerUnavailable
	}
	r.released = true
	l.idleReservation = nil
	return nil
}

// Retire closes the exact already-idle lease before its owner releases any
// registrations. The caller must still join the owner's actual disposal.
func (r *ProfileOwnerIdleLease) Retire() error {
	if r == nil || r.lease == nil {
		return ErrProfileOwnerUnavailable
	}
	l := r.lease
	l.registry.mu.Lock()
	defer l.registry.mu.Unlock()
	if r.retired {
		return nil
	}
	if r.released || l.retired || l.idleReservation != r || l.operations != 0 {
		return ErrProfileOwnerUnavailable
	}
	r.retired = true
	l.retired = true
	return nil
}

// DrainOperations seals admission and joins accepted callbacks without
// canceling them. Owner shutdown calls this before releasing resources. A
// same-owner callback must ask its outer lifecycle to retire it instead of
// synchronously joining itself. Context cancellation cannot release resources
// while a callback still borrows them.
func (l *ProfileOwnerLease) DrainOperations(ctx context.Context) error {
	if l == nil || l.registry == nil {
		return nil
	}
	if ctx != nil && ctx.Value(profileOwnerCallContextKey{}) == l.registry {
		return ErrProfileOwnerReentrant
	}
	r := l.registry
	r.mu.Lock()
	l.retired = true
	l.reservationEligible = false
	if l.retirementDone == nil {
		l.retirementDone = make(chan struct{})
		if l.operations == 0 {
			close(l.retirementDone)
		}
	}
	done := l.retirementDone
	r.mu.Unlock()
	<-done
	return nil
}

// closeReservationAdmission belongs to the registry, not a custom owner.
// Existing exact tokens still release/retire normally, while no abandoned or
// retired lease (including never-published candidates) can mint another one.
func (l *ProfileOwnerLease) closeReservationAdmission() {
	if l == nil || l.registry == nil {
		return
	}
	l.registry.mu.Lock()
	l.reservationEligible = false
	l.registry.mu.Unlock()
}
