package harness

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sort"
	"sync"

	point "github.com/phodal/pero/harness/go/runtime"
)

// ProfileOwnerRow is one evaluated official profile row. The trusted Host
// registers its identity and factory; the profile cannot choose a Go definition.
type ProfileOwnerRow struct {
	ID       string          `json:"id"`
	Name     string          `json:"name"`
	Disabled bool            `json:"disabled"`
	Config   json.RawMessage `json:"config,omitempty"`
}

type ProfileOwner interface{ Dispose(context.Context) error }

// ProfileOwnerRegistration is deployment wiring, never guest input. Mount must
// clean up a failed start and return only after the new owner is ready. Owners
// must cancel and join admitted callbacks before Dispose returns.
type ProfileOwnerRegistration struct {
	Row      ProfileOwnerRow
	Validate func(json.RawMessage) error
	Mount    func(context.Context, json.RawMessage, *ProfileOwnerLease) (ProfileOwner, error)
	// Idle owners use sealed factories and participate in the outer profile
	// transaction. These hooks are trusted composition, never profile input.
	Reserve   func(ProfileOwner) (ProfileOwnerIdleReservation, error)
	Activate  func(ProfileOwner) (func(), error)
	Published func(ProfileOwner)
}

var ErrProfileOwnerUnavailable = errors.New("harness: profile owner is unavailable or changing")
var ErrProfileOwnerReentrant = errors.New("harness: cannot recompose profile owners from an owned callback")

type profileOwnerCallContextKey struct{}

var profileOwnerRegistryKey = point.NewKey[*ProfileOwnerRegistry]("harness.profile-owners")

type profileOwnerEntry struct {
	registration ProfileOwnerRegistration
	row          ProfileOwnerRow
	owner        ProfileOwner
	lease        *ProfileOwnerLease
}

// ProfileOwnerRegistry owns only explicitly registered, independently
// disposable leaf contributions. Core authorities are deliberately absent.
// It survives bridge generations and uses no guest-visible DefinitionCatalog.
type ProfileOwnerRegistry struct {
	mu          sync.Mutex
	gate        chan struct{}
	entries     map[string]*profileOwnerEntry
	closed      bool
	failure     error
	revision    uint64
	instance    *point.Instance
	mountCancel context.CancelCauseFunc
	closeDone   chan struct{}
	closeErr    error
	retirement  *ProfileRetirement
	retained    []ProfileOwner
	transaction *ProfileOwnerTransaction
}

// ProfileOwnerLease fences an exact contribution incarnation. Its command
// wrapper complements CommandRegistry's cancellation/drain owner: a candidate
// cannot execute before commit and a captured old handler cannot be revived.
type ProfileOwnerLease struct {
	registry            *ProfileOwnerRegistry
	id                  string
	admitted            bool // guarded by registry.mu; only published incarnations admit
	retired             bool
	operations          int
	idleReservation     *ProfileOwnerIdleLease
	retirementDone      chan struct{}
	reservationEligible bool // registry-owned; disposal permanently closes minting
}

func (l *ProfileOwnerLease) active() bool {
	if l == nil || l.registry == nil {
		return false
	}
	r := l.registry
	r.mu.Lock()
	defer r.mu.Unlock()
	return l.activeLocked()
}

func (l *ProfileOwnerLease) activeLocked() bool {
	r := l.registry
	e := r.entries[l.id]
	return !r.closed && r.failure == nil && !l.retired && l.reservationEligible && l.admitted && e != nil && !e.row.Disabled && e.lease == l
}

// Active reports admission for this exact committed incarnation. It is not a
// drain receipt: the native owner must still cancel and join accepted work.
func (l *ProfileOwnerLease) Active() bool { return l.active() }

// CallContext marks an owned callback so a lifecycle request made from inside
// that callback fails promptly instead of waiting for its own cleanup.
func (l *ProfileOwnerLease) CallContext(ctx context.Context) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	if l == nil || l.registry == nil {
		return ctx
	}
	return context.WithValue(ctx, profileOwnerCallContextKey{}, l.registry)
}

func (l *ProfileOwnerLease) WrapCommand(command CommandDefinition) CommandDefinition {
	handler := command.Handler
	command.profileLease = l
	if handler == nil {
		return command
	}
	command.Handler = func(ctx context.Context, invocation CommandInvocation) (CommandResult, error) {
		if !l.active() {
			return CommandResult{}, ErrProfileOwnerUnavailable
		}
		owned, release, err := l.BeginOperation(ctx)
		if err != nil {
			return CommandResult{}, err
		}
		defer release() // actual callback may outlive the public cancellation return
		result, err := handler(l.CallContext(owned), invocation)
		if !l.active() {
			return CommandResult{}, errors.Join(err, ErrProfileOwnerUnavailable)
		}
		return result, err
	}
	return command
}

// MountProfileOwners is called by the native composition root, once per App.
func MountProfileOwners(ctx context.Context, h *Harness) (*ProfileOwnerRegistry, error) {
	if h == nil || h.Runtime() == nil {
		return nil, errors.New("harness: profile owners require a running Harness")
	}
	r := &ProfileOwnerRegistry{gate: make(chan struct{}, 1), entries: map[string]*profileOwnerEntry{}}
	r.gate <- struct{}{}
	definition := point.Define(point.Descriptor{
		ID: "harness/profile-owners", Requires: []point.Requirement{point.Require(CommandRegistryKey)},
		Provides: []point.AnyKey{profileOwnerRegistryKey.Any()},
	}, point.DecodeJSON[struct{}], func(run *point.Run, _ struct{}) (point.Dispose, error) {
		if _, err := point.Provide(run, profileOwnerRegistryKey, r); err != nil {
			return nil, err
		}
		return r.Close, nil
	})
	instance, err := h.Runtime().Mount(nil, "harness/profile-owners", definition, nil)
	if err != nil {
		return nil, err
	}
	r.instance = instance
	if err = instance.WaitStable(ctx); err != nil {
		_ = instance.Dispose(context.Background())
		return nil, err
	}
	if instance.State() != point.Active {
		_ = instance.Dispose(context.Background())
		return nil, errors.Join(errors.New("harness: profile owner registry did not activate"), instance.LastError())
	}
	return r, nil
}

func (h *Harness) ProfileOwners() *ProfileOwnerRegistry {
	if h == nil || h.Runtime() == nil {
		return nil
	}
	r, _ := point.Lookup(h.Runtime(), h.Runtime().Root(), profileOwnerRegistryKey)
	return r
}

func (r *ProfileOwnerRegistry) acquire(ctx context.Context) error {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-r.gate:
		return nil
	}
}

func cloneProfileOwnerRow(row ProfileOwnerRow) ProfileOwnerRow {
	row.Config = append(json.RawMessage(nil), row.Config...)
	return row
}

func normalizeProfileOwnerConfig(raw json.RawMessage) (json.RawMessage, error) {
	if len(bytes.TrimSpace(raw)) == 0 {
		return json.RawMessage(`{}`), nil
	}
	var value any
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	if err := decoder.Decode(&value); err != nil {
		return nil, err
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		if err != nil {
			return nil, err
		}
		return nil, errors.New("harness: profile config must contain one JSON value")
	}
	return json.Marshal(value)
}

// Register mounts the actual native factory and records its evaluated bootstrap
// as the first committed snapshot. Only trusted composition code calls it.
func (r *ProfileOwnerRegistry) Register(ctx context.Context, registration ProfileOwnerRegistration) error {
	if r == nil || registration.Row.ID == "" || registration.Row.Name == "" || registration.Mount == nil || registration.Validate == nil {
		return errors.New("harness: profile owner requires identity, validation, and mount")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if ctx.Value(profileOwnerCallContextKey{}) == r {
		return ErrProfileOwnerReentrant
	}
	if err := r.acquire(ctx); err != nil {
		return err
	}
	var replay func()
	defer func() {
		r.gate <- struct{}{}
		if replay != nil {
			replay()
		}
	}()
	r.mu.Lock()
	unavailable := r.closed || r.failure != nil
	duplicate := r.entries[registration.Row.ID] != nil
	r.mu.Unlock()
	if unavailable {
		return ErrProfileOwnerUnavailable
	}
	if duplicate {
		return fmt.Errorf("harness: duplicate profile owner %q", registration.Row.ID)
	}
	row := cloneProfileOwnerRow(registration.Row)
	config, err := normalizeProfileOwnerConfig(row.Config)
	if err != nil {
		return err
	}
	row.Config = config
	if err = registration.Validate(config); err != nil {
		return err
	}
	entry := &profileOwnerEntry{registration: registration, row: row, lease: &ProfileOwnerLease{registry: r, id: row.ID, reservationEligible: true}}
	if !row.Disabled {
		entry.owner, err = r.mount(ctx, entry)
		if err != nil {
			return errors.Join(err, r.discardUncommittedOwner(entry))
		}
		if entry.owner == nil {
			entry.lease.closeReservationAdmission()
			return errors.New("harness: profile owner factory returned no owner")
		}
	}
	r.mu.Lock()
	publishErr := ctx.Err()
	if r.closed {
		publishErr = errors.Join(publishErr, ErrProfileOwnerUnavailable)
	}
	if publishErr != nil {
		r.mu.Unlock()
		return errors.Join(publishErr, r.discardUncommittedOwner(entry))
	}
	r.entries[row.ID] = entry
	entry.lease.admitted = true
	r.revision++
	r.mu.Unlock()
	if registration.Activate != nil && entry.owner != nil {
		var err error
		replay, err = registration.Activate(entry.owner)
		if err != nil {
			r.mu.Lock()
			r.failure = errors.Join(r.failure, err)
			entry.lease.admitted = false
			r.mu.Unlock()
			return err
		}
	}
	if registration.Published != nil {
		registration.Published(entry.owner)
	}
	return nil
}

// mount owns a readiness operation, distinct from the returned owner's
// lifetime. Close cancels the active factory synchronously, including a
// restoration whose caller is deliberately independent of the failed request.
// The transaction gate ensures at most one factory is active.
func (r *ProfileOwnerRegistry) mount(ctx context.Context, entry *profileOwnerEntry) (ProfileOwner, error) {
	ctx, cancel := context.WithCancelCause(ctx)
	r.mu.Lock()
	if r.closed {
		r.mu.Unlock()
		cancel(ErrProfileOwnerUnavailable)
		return nil, ErrProfileOwnerUnavailable
	}
	r.mountCancel = cancel
	r.mu.Unlock()
	defer func() {
		r.mu.Lock()
		r.mountCancel = nil
		r.mu.Unlock()
		cancel(nil)
	}()
	return entry.registration.Mount(ctx, append(json.RawMessage(nil), entry.row.Config...), entry.lease)
}

// The transaction gate owns this cleanup backlog. An uncertain disposer is
// retained for parent shutdown and fences all admission and replacement.
func (r *ProfileOwnerRegistry) discardUncommittedOwner(entry *profileOwnerEntry) error {
	entry.lease.closeReservationAdmission()
	owner := entry.owner
	if owner == nil {
		return nil
	}
	err := owner.Dispose(context.Background())
	if err != nil {
		r.retained = append(r.retained, owner)
		r.mu.Lock()
		r.failure = errors.Join(r.failure, err)
		r.mu.Unlock()
	}
	return err
}

func (r *ProfileOwnerRegistry) Rows() []ProfileOwnerRow {
	if r == nil {
		return nil
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	rows := make([]ProfileOwnerRow, 0, len(r.entries))
	for _, entry := range r.entries {
		rows = append(rows, cloneProfileOwnerRow(entry.row))
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].ID < rows[j].ID })
	return rows
}

func (r *ProfileOwnerRegistry) Revision() uint64 {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.revision
}

// plan runs with gate held. Disabled rows retain only their last evaluated
// options; a dormant expression must never be decoded or speculatively run.
func (r *ProfileOwnerRegistry) plan(rows []ProfileOwnerRow, selectedIDs []string, scoped bool) (map[string]ProfileOwnerRow, error) {
	r.mu.Lock()
	if r.closed || r.failure != nil {
		err := errors.Join(ErrProfileOwnerUnavailable, r.failure)
		r.mu.Unlock()
		return nil, err
	}
	entries := make(map[string]*profileOwnerEntry, len(r.entries))
	for id, entry := range r.entries {
		entries[id] = entry
	}
	r.mu.Unlock()
	selected := make(map[string]bool, len(selectedIDs))
	for _, id := range selectedIDs {
		if entries[id] == nil || selected[id] {
			return nil, fmt.Errorf("harness: invalid profile owner selection %q", id)
		}
		selected[id] = true
	}
	next := make(map[string]ProfileOwnerRow, len(entries))
	for _, row := range rows {
		entry := entries[row.ID]
		if entry == nil || row.Name != entry.row.Name {
			return nil, fmt.Errorf("harness: unregistered profile owner identity %q", row.ID)
		}
		if scoped && !selected[row.ID] {
			return nil, fmt.Errorf("harness: profile owner %q is outside this profile", row.ID)
		}
		if _, exists := next[row.ID]; exists {
			return nil, fmt.Errorf("harness: duplicate profile owner row %q", row.ID)
		}
		row = cloneProfileOwnerRow(row)
		if row.Disabled {
			row.Config = append(json.RawMessage(nil), entry.row.Config...)
		} else {
			config, err := normalizeProfileOwnerConfig(row.Config)
			if err != nil {
				return nil, fmt.Errorf("harness: profile owner %q config: %w", row.ID, err)
			}
			if err = entry.registration.Validate(config); err != nil {
				return nil, fmt.Errorf("harness: profile owner %q config: %w", row.ID, err)
			}
			row.Config = config
		}
		next[row.ID] = row
	}
	for id, entry := range entries {
		if _, present := next[id]; !present {
			row := cloneProfileOwnerRow(entry.row)
			if !scoped || selected[id] {
				row.Disabled = true
			}
			next[id] = row
		}
	}
	return next, nil
}

func (r *ProfileOwnerRegistry) Validate(rows []ProfileOwnerRow) error {
	return r.ValidateContext(context.Background(), rows)
}

// ValidateContext cancels a queued validation when its bridge request closes.
func (r *ProfileOwnerRegistry) ValidateContext(ctx context.Context, rows []ProfileOwnerRow) error {
	return r.validate(ctx, nil, rows, false)
}

// ValidateSelected validates only the native identities enrolled by the
// trusted profile composition. Omission affects selected owners only.
func (r *ProfileOwnerRegistry) ValidateSelected(ctx context.Context, ids []string, rows []ProfileOwnerRow) error {
	return r.validate(ctx, ids, rows, true)
}

func (r *ProfileOwnerRegistry) validate(ctx context.Context, ids []string, rows []ProfileOwnerRow, scoped bool) error {
	if r == nil {
		return ErrProfileOwnerUnavailable
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if ctx.Value(profileOwnerCallContextKey{}) == r {
		return ErrProfileOwnerReentrant
	}
	if err := r.acquire(ctx); err != nil {
		return err
	}
	defer func() { r.gate <- struct{}{} }()
	_, err := r.plan(rows, ids, scoped)
	return errors.Join(err, ctx.Err())
}

// Reconcile validates the complete snapshot before mutation, retires exact
// owners, and mounts replacements behind an admission fence. Failed starts
// restore the prior snapshot using fresh incarnations, never stale handlers.
func (r *ProfileOwnerRegistry) Reconcile(ctx context.Context, rows []ProfileOwnerRow) error {
	return r.reconcileWithIdleOwners(ctx, nil, rows, false)
}

// ReconcileSelected preserves unselected owners in the same serialized
// transaction, so concurrent profiles cannot overwrite each other's commits.
// IDs are fixed by trusted enrollment, never supplied by a profile request.
func (r *ProfileOwnerRegistry) ReconcileSelected(ctx context.Context, ids []string, rows []ProfileOwnerRow) error {
	return r.reconcileWithIdleOwners(ctx, ids, rows, true)
}

func (r *ProfileOwnerRegistry) reconcile(ctx context.Context, ids []string, rows []ProfileOwnerRow, scoped bool) error {
	if r == nil {
		return ErrProfileOwnerUnavailable
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if ctx.Value(profileOwnerCallContextKey{}) == r {
		return ErrProfileOwnerReentrant
	}
	if err := r.acquire(ctx); err != nil {
		return err
	}
	defer func() { r.gate <- struct{}{} }()
	next, err := r.plan(rows, ids, scoped)
	if err != nil {
		return err
	}
	if err = ctx.Err(); err != nil {
		return err
	}
	r.mu.Lock()
	if r.closed {
		r.mu.Unlock()
		return ErrProfileOwnerUnavailable
	}
	changed := make([]string, 0)
	for id, row := range next {
		old := r.entries[id].row
		if old.Disabled != row.Disabled || !bytes.Equal(old.Config, row.Config) {
			changed = append(changed, id)
		}
	}
	if len(changed) == 0 {
		r.mu.Unlock()
		return nil
	}
	sort.Strings(changed)
	for _, id := range changed {
		r.entries[id].lease.admitted = false
	}
	r.mu.Unlock()
	staged := make(map[string]*profileOwnerEntry)
	retired := make([]string, 0, len(changed))
	var cleanupFailure error
	for _, id := range changed {
		old := r.entries[id]
		old.lease.closeReservationAdmission()
		if old.owner != nil {
			if err = old.owner.Dispose(context.Background()); err != nil {
				cleanupFailure = fmt.Errorf("retire profile owner %q: %w", id, err)
				break
			}
		}
		retired = append(retired, id)
		candidate := &profileOwnerEntry{registration: old.registration, row: next[id], lease: &ProfileOwnerLease{registry: r, id: id, reservationEligible: true}}
		staged[id] = candidate
		if err = ctx.Err(); err != nil {
			break
		}
		r.mu.Lock()
		closed := r.closed
		r.mu.Unlock()
		if closed {
			err = ErrProfileOwnerUnavailable
			break
		}
		if !candidate.row.Disabled {
			candidate.owner, err = r.mount(ctx, candidate)
			if err == nil && candidate.owner == nil {
				err = errors.New("harness: profile owner factory returned no owner")
			}
			if err != nil {
				break
			}
		}
	}
	r.mu.Lock()
	closed := r.closed
	r.mu.Unlock()
	if err == nil && cleanupFailure == nil && !closed {
		err = ctx.Err()
	}
	if err == nil && cleanupFailure == nil && !closed {
		r.mu.Lock()
		// Close can race the readiness result; never publish after its fence.
		if !r.closed {
			for id, candidate := range staged {
				r.entries[id] = candidate
				candidate.lease.admitted = true
			}
			r.revision++
			r.mu.Unlock()
			return nil
		}
		closed = true
		r.mu.Unlock()
	}
	if closed {
		err = errors.Join(err, ErrProfileOwnerUnavailable)
	}
	var rollbackErr error
	for i := len(retired) - 1; i >= 0; i-- {
		id := retired[i]
		if candidate := staged[id]; candidate != nil {
			candidate.lease.closeReservationAdmission()
			if candidate.owner == nil {
				continue
			}
			if disposeErr := candidate.owner.Dispose(context.Background()); disposeErr != nil {
				rollbackErr = errors.Join(rollbackErr, disposeErr)
				r.retained = append(r.retained, candidate.owner)
			}
		}
	}
	rollbackCleanupErr := rollbackErr
	// A cleanup error has an uncertain resource receipt. Keep admission closed;
	// do not overlap a replacement with resources whose release was not proved.
	if cleanupFailure == nil && rollbackErr == nil && !closed {
		for _, id := range retired {
			old := r.entries[id]
			restored := &profileOwnerEntry{registration: old.registration, row: cloneProfileOwnerRow(old.row), lease: &ProfileOwnerLease{registry: r, id: id, reservationEligible: true}}
			if !restored.row.Disabled {
				restored.owner, rollbackErr = r.mount(context.Background(), restored)
				if rollbackErr == nil && restored.owner == nil {
					rollbackErr = errors.New("harness: profile owner rollback returned no owner")
				}
			}
			r.mu.Lock()
			r.entries[id] = restored
			r.mu.Unlock()
			if rollbackErr != nil {
				break
			}
		}
		if rollbackErr == nil {
			r.mu.Lock()
			if !r.closed {
				// Later changed owners may never have reached retirement if an
				// earlier start failed; their untouched leases need admission too.
				for _, id := range changed {
					r.entries[id].lease.admitted = true
				}
			}
			r.mu.Unlock()
		}
	}
	if cleanupFailure != nil || rollbackErr != nil {
		r.mu.Lock()
		// Shutdown no longer needs restoration. Its interrupted readiness is
		// not a cleanup failure; every returned handle still belongs to Close.
		r.failure = errors.Join(r.failure, cleanupFailure, rollbackCleanupErr)
		if !r.closed {
			r.failure = errors.Join(r.failure, rollbackErr)
		}
		r.mu.Unlock()
	}
	return errors.Join(err, cleanupFailure, rollbackErr)
}

// ProfileRetirement is the full logical completion of one exact registry's
// terminal retirement. It does not include removal of the registry runtime
// mount; an outer App receipt covers that additional disposal.
// Obtain it through BeginRetire. Do not copy or construct receipts yourself.
type ProfileRetirement struct {
	registry *ProfileOwnerRegistry
	done     <-chan struct{}
	cleanup  context.Context
}

// CheckRetirementWait rejects a known same-registry callback before a joining
// lifecycle API changes state. It is only a provenance check: a context without
// the marker does not prove that waiting is safe. In particular, synchronous
// Session subscribers must request BeginRetire and leave Drain to an outer owner.
func (r *ProfileOwnerRegistry) CheckRetirementWait(ctx context.Context) error {
	if r != nil && ctx != nil && ctx.Value(profileOwnerCallContextKey{}) == r {
		return ErrProfileOwnerReentrant
	}
	return nil
}

// BeginRetire seals managed admission synchronously and starts one owned
// retirement transaction without waiting for callbacks, transactions or owner
// disposal. The first request owns cleanup values; later waiters cannot replace
// them. It is safe to request from a callback. Return from that callback
// before an outer lifecycle calls the receipt's Drain.
func (r *ProfileOwnerRegistry) BeginRetire(ctx context.Context) *ProfileRetirement {
	if r == nil {
		return nil
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closeDone == nil {
		r.closed = true
		if r.mountCancel != nil {
			r.mountCancel(ErrProfileOwnerUnavailable)
		}
		r.closeDone = make(chan struct{})
		r.retirement = &ProfileRetirement{registry: r, done: r.closeDone}
		r.retirement.cleanup = r.retirement.CleanupContext(ctx)
		go r.finishClose(r.retirement.cleanup)
		if transaction := r.transaction; transaction != nil {
			go transaction.close()
		}
	}
	return r.retirement
}

// CleanupContext transfers a request context to an outer cleanup worker after
// this exact registry has been sealed. It retains values but removes deadline
// and cancellation, and detaches only this registry's private self-join marker.
// It grants no admission or other authority. Use only for owned cleanup, never
// to make a synchronous callback join itself; Drain's callback rule is unchanged.
func (p *ProfileRetirement) CleanupContext(ctx context.Context) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	ctx = context.WithoutCancel(ctx)
	if p != nil && p.registry != nil && ctx.Value(profileOwnerCallContextKey{}) == p.registry {
		ctx = context.WithValue(ctx, profileOwnerCallContextKey{}, nil)
	}
	return ctx
}

// Drain joins all accepted logical work, transactions and owner disposal for
// this receipt. Cancellation stops only this wait; retirement remains owned.
// Never call Drain from a callback whose return retirement requires. Known
// marked reentry is refused, but a contextless subscriber must choose the
// nonwaiting API explicitly. An already completed receipt returns its stored
// result even when this wait's context has been canceled.
func (p *ProfileRetirement) Drain(ctx context.Context) error {
	if p == nil || p.registry == nil {
		return nil
	}
	if err := p.registry.CheckRetirementWait(ctx); err != nil {
		return err
	}
	if ctx == nil {
		ctx = context.Background()
	}
	select {
	case <-p.done:
	default:
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-p.done:
		}
	}
	p.registry.mu.Lock()
	defer p.registry.mu.Unlock()
	return p.registry.closeErr
}

// Close seals admission synchronously and retains ownership while all current
// work and any transaction settle. A canceled caller only cancels its wait.
// A synchronous callback must instead BeginRetire and return before outer Drain.
func (r *ProfileOwnerRegistry) Close(ctx context.Context) error {
	if err := r.CheckRetirementWait(ctx); err != nil {
		return err
	}
	return r.BeginRetire(ctx).Drain(ctx)
}

func (r *ProfileOwnerRegistry) finishClose(ctx context.Context) {
	<-r.gate
	defer func() { r.gate <- struct{}{} }()
	ids := make([]string, 0, len(r.entries))
	for id := range r.entries {
		ids = append(ids, id)
	}
	sort.Sort(sort.Reverse(sort.StringSlice(ids)))
	var joined error
	for _, id := range ids {
		r.entries[id].lease.closeReservationAdmission()
		if owner := r.entries[id].owner; owner != nil {
			joined = errors.Join(joined, owner.Dispose(ctx))
		}
	}
	for _, owner := range r.retained {
		joined = errors.Join(joined, owner.Dispose(ctx))
	}
	r.mu.Lock()
	r.closeErr = errors.Join(r.failure, joined)
	close(r.closeDone)
	r.mu.Unlock()
}

// Dispose also removes the registry's runtime mount, allowing an outer App to
// remount its trusted contribution set without leaving a closed root behind.
func (r *ProfileOwnerRegistry) Dispose(ctx context.Context) error {
	if r == nil {
		return nil
	}
	if err := r.CheckRetirementWait(ctx); err != nil {
		return err
	}
	err := r.Close(ctx)
	if r.instance != nil {
		err = errors.Join(err, r.instance.Dispose(ctx))
	}
	return err
}

// HasIdleOwners reports trusted registration capabilities for a fixed Host
// enrollment. Profile rows cannot grant this capability or remove a required
// participant from a running bridge's transaction boundary.
func (r *ProfileOwnerRegistry) HasIdleOwners(ids []string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, id := range ids {
		if entry := r.entries[id]; entry != nil && entry.registration.Reserve != nil {
			return true
		}
	}
	return false
}
