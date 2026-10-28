package session

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/phodal/pero/harness/go/runtime"
	"log/slog"
	"sort"
	"sync"
)

type ProjectionChangeListener func(*Session, string, json.RawMessage, uint64)

type ProjectionSnapshot struct {
	// AsOfSeq is the raw Go Session watermark; zero denotes an empty log.
	AsOfSeq uint64                     `json:"asOfSeq"`
	Values  map[string]json.RawMessage `json:"values"`
}

type ProjectionCheckpointRow struct {
	Version uint64          `json:"ver"`
	Seq     uint64          `json:"seq"`
	Value   json.RawMessage `json:"val"`
}

type ProjectionCheckpoint map[string]ProjectionCheckpointRow

type projectionCell struct {
	state         any
	observedSeq   uint64
	previousView  any
	previousKnown bool
	currentView   any
	currentKnown  bool
	err           error
}

type projectionRegistration struct {
	definition ProjectionDefinition
	refs       int
	cells      map[*Session]*projectionCell
}

type projectionListenerEntry struct {
	id       uint64
	listener ProjectionChangeListener
}

type projectionNotification struct {
	session   *Session
	key       string
	value     json.RawMessage
	seq       uint64
	listeners []projectionListenerEntry
}

// SessionProjectionRegistry eagerly drives registered folds over committed
// events and owns their per-session watermarks.
type SessionProjectionRegistry struct {
	store  *SessionStore
	logger *slog.Logger

	mu               sync.Mutex
	disposed         bool
	registrations    map[string]*projectionRegistration
	listeners        map[uint64]ProjectionChangeListener
	nextListenerID   uint64
	sessions         map[*Session]func()
	unsubscribeStore func()
}

func NewSessionProjectionRegistry(store *SessionStore, logger *slog.Logger) (*SessionProjectionRegistry, error) {
	if store == nil {
		return nil, errors.New("harness: session projection registry requires a session store")
	}
	if logger == nil {
		logger = slog.Default()
	}
	registry := &SessionProjectionRegistry{
		store: store, logger: logger, registrations: make(map[string]*projectionRegistration),
		listeners: make(map[uint64]ProjectionChangeListener), sessions: make(map[*Session]func()),
	}
	registry.unsubscribeStore = store.Subscribe(func(event SessionStoreEvent) {
		switch event.Kind {
		case SessionStoreAdded:
			registry.attach(event.Session)
		case SessionStoreRemoved:
			registry.detach(event.Session)
		}
	})
	for _, session := range store.List() {
		registry.attach(session)
	}
	return registry, nil
}

func validateProjectionDefinition(definition ProjectionDefinition) error {
	if definition.Key == "" {
		return errors.New("harness: session projection key must be non-empty")
	}
	if definition.Init == nil || definition.Apply == nil || definition.EncodeState == nil || definition.DecodeState == nil {
		return fmt.Errorf("harness: session projection %q requires init, apply, encodeState, and decodeState", definition.Key)
	}
	if definition.Wire != nil && (definition.Wire.View == nil || definition.Wire.EncodeView == nil) {
		return fmt.Errorf("harness: session projection %q wire requires view and encodeView", definition.Key)
	}
	return nil
}

// Register installs or reference-counts one exact projection key on a runtime
// effect. Sharing across state versions fails closed.
func (r *SessionProjectionRegistry) Register(run *runtime.Run, definition ProjectionDefinition) (runtime.Dispose, error) {
	if run == nil || !run.Active() {
		return nil, runtime.ErrInactiveRun
	}
	if err := validateProjectionDefinition(definition); err != nil {
		return nil, err
	}
	r.mu.Lock()
	if r.disposed {
		r.mu.Unlock()
		return nil, errors.New("harness: session projection registry disposed")
	}
	if existing := r.registrations[definition.Key]; existing != nil {
		if existing.definition.StateVersion != definition.StateVersion {
			r.mu.Unlock()
			return nil, fmt.Errorf("harness: session projection key %q is already registered at stateVersion %d; refusing stateVersion %d", definition.Key, existing.definition.StateVersion, definition.StateVersion)
		}
		existing.refs++
		r.mu.Unlock()
		return r.projectionDisposer(run, definition.Key)
	}
	sessions := make([]*Session, 0, len(r.sessions))
	for session := range r.sessions {
		sessions = append(sessions, session)
	}
	r.mu.Unlock()
	sort.Slice(sessions, func(i, j int) bool { return sessions[i].Header().ID < sessions[j].Header().ID })
	for _, session := range sessions {
		session.notifyMu.Lock()
	}
	cells := make(map[*Session]*projectionCell, len(sessions))
	var buildErr error
	for _, session := range sessions {
		cell, err := buildProjectionCell(definition, session.Header(), session.Events())
		if err != nil {
			buildErr = err
			break
		}
		cells[session] = cell
	}
	if buildErr == nil {
		r.mu.Lock()
		if r.disposed {
			buildErr = errors.New("harness: session projection registry disposed")
		} else if existing := r.registrations[definition.Key]; existing != nil {
			if existing.definition.StateVersion != definition.StateVersion {
				buildErr = fmt.Errorf("harness: session projection key %q changed version during registration", definition.Key)
			} else {
				existing.refs++
			}
		} else {
			// A Session can be removed while a late unit is folding its frozen
			// log. Publish only cells whose exact Session object is still live;
			// Sessions added during the fold materialize lazily on first drive or
			// read. This prevents an HMR/register race from retaining a detached
			// lifecycle or grafting its state onto a same-ID replacement.
			liveCells := make(map[*Session]*projectionCell, len(cells))
			for session, cell := range cells {
				if _, live := r.sessions[session]; live {
					liveCells[session] = cell
				}
			}
			r.registrations[definition.Key] = &projectionRegistration{definition: definition, refs: 1, cells: liveCells}
		}
		r.mu.Unlock()
	}
	for index := len(sessions) - 1; index >= 0; index-- {
		sessions[index].notifyMu.Unlock()
	}
	if buildErr != nil {
		return nil, buildErr
	}
	return r.projectionDisposer(run, definition.Key)
}

func (r *SessionProjectionRegistry) projectionDisposer(run *runtime.Run, key string) (runtime.Dispose, error) {
	var once sync.Once
	dispose := runtime.Dispose(func(context.Context) error {
		once.Do(func() {
			r.mu.Lock()
			if registration := r.registrations[key]; registration != nil {
				registration.refs--
				if registration.refs == 0 {
					delete(r.registrations, key)
				}
			}
			r.mu.Unlock()
		})
		return nil
	})
	if err := run.Defer("session-projection:"+key, dispose); err != nil {
		_ = dispose(context.Background())
		return nil, err
	}
	return dispose, nil
}

func (r *SessionProjectionRegistry) OnChanged(run *runtime.Run, listener ProjectionChangeListener) (runtime.Dispose, error) {
	if run == nil || !run.Active() {
		return nil, runtime.ErrInactiveRun
	}
	if listener == nil {
		return nil, errors.New("harness: projection change listener is nil")
	}
	r.mu.Lock()
	if r.disposed {
		r.mu.Unlock()
		return nil, errors.New("harness: session projection registry disposed")
	}
	r.nextListenerID++
	id := r.nextListenerID
	r.listeners[id] = listener
	r.mu.Unlock()
	var once sync.Once
	dispose := runtime.Dispose(func(context.Context) error {
		once.Do(func() {
			r.mu.Lock()
			delete(r.listeners, id)
			r.mu.Unlock()
		})
		return nil
	})
	if err := run.Defer("session-projection:listener", dispose); err != nil {
		_ = dispose(context.Background())
		return nil, err
	}
	return dispose, nil
}

func (r *SessionProjectionRegistry) StateOf(session *Session, key string) (any, bool, error) {
	if session == nil {
		return nil, false, errors.New("harness: projection state requires a session")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	registration := r.registrations[key]
	if registration == nil {
		return nil, false, nil
	}
	events := session.Events()
	cell, err := r.materializeCellLocked(registration, session, events)
	if err != nil {
		return nil, false, err
	}
	return cell.state, true, nil
}

func (r *SessionProjectionRegistry) Snapshot(session *Session, keys ...string) (ProjectionSnapshot, error) {
	if session == nil {
		return ProjectionSnapshot{}, errors.New("harness: projection snapshot requires a session")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	events := session.Events()
	selected := stringSet(keys)
	values := make(map[string]json.RawMessage)
	for _, key := range sortedProjectionKeys(r.registrations) {
		registration := r.registrations[key]
		if registration.definition.Wire == nil || selected != nil && !selected[key] {
			continue
		}
		cell, err := r.materializeCellLocked(registration, session, events)
		if err != nil {
			return ProjectionSnapshot{}, err
		}
		value, _, err := projectionView(registration.definition, cell.state)
		if err != nil {
			return ProjectionSnapshot{}, err
		}
		values[key] = value
	}
	return ProjectionSnapshot{AsOfSeq: eventTail(events), Values: values}, nil
}

func (r *SessionProjectionRegistry) CachedSnapshot(session *Session, keys ...string) (*ProjectionSnapshot, error) {
	if session == nil {
		return nil, errors.New("harness: cached projection snapshot requires a session")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	selected := stringSet(keys)
	values := make(map[string]json.RawMessage)
	var asOf uint64
	found := false
	for _, key := range sortedProjectionKeys(r.registrations) {
		registration := r.registrations[key]
		if registration.definition.Wire == nil || selected != nil && !selected[key] {
			continue
		}
		cell := registration.cells[session]
		if cell == nil {
			continue
		}
		if cell.err != nil {
			return nil, cell.err
		}
		value, _, err := projectionView(registration.definition, cell.state)
		if err != nil {
			return nil, err
		}
		values[key] = value
		if !found || cell.observedSeq < asOf {
			asOf = cell.observedSeq
		}
		found = true
	}
	if !found {
		return nil, nil
	}
	return &ProjectionSnapshot{AsOfSeq: asOf, Values: values}, nil
}

func (r *SessionProjectionRegistry) Checkpoint(session *Session) (ProjectionCheckpoint, error) {
	if session == nil {
		return nil, errors.New("harness: projection checkpoint requires a session")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	events := session.Events()
	checkpoint := make(ProjectionCheckpoint, len(r.registrations))
	for _, key := range sortedProjectionKeys(r.registrations) {
		registration := r.registrations[key]
		cell, err := r.materializeCellLocked(registration, session, events)
		if err != nil {
			return nil, err
		}
		value, err := callProjectionEncodeState(registration.definition, cell.state)
		if err != nil {
			return nil, fmt.Errorf("harness: encode session projection %q checkpoint: %w", key, err)
		}
		checkpoint[key] = ProjectionCheckpointRow{Version: registration.definition.StateVersion, Seq: cell.observedSeq, Value: append(json.RawMessage(nil), value...)}
	}
	return checkpoint, nil
}

// RestoreFloor returns the inclusive raw Go event seq required for a suffix
// read. The bool is false when no projection is registered.
func (r *SessionProjectionRegistry) RestoreFloor(checkpoint ProjectionCheckpoint) (uint64, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.registrations) == 0 {
		return 0, false
	}
	floor := ^uint64(0)
	for key, registration := range r.registrations {
		row, ok := checkpoint[key]
		need := uint64(1)
		if ok && row.Version == registration.definition.StateVersion && row.Seq > 0 {
			need = row.Seq
		}
		if need < floor {
			floor = need
		}
	}
	return floor, true
}

func (r *SessionProjectionRegistry) ViewCheckpoint(checkpoint ProjectionCheckpoint, keys ...string) map[string]json.RawMessage {
	r.mu.Lock()
	defer r.mu.Unlock()
	selected := stringSet(keys)
	values := make(map[string]json.RawMessage)
	for _, key := range sortedProjectionKeys(r.registrations) {
		registration := r.registrations[key]
		definition := registration.definition
		if definition.Wire == nil || selected != nil && !selected[key] {
			continue
		}
		row, ok := checkpoint[key]
		if !ok || row.Version != definition.StateVersion {
			continue
		}
		state, err := callProjectionDecodeState(definition, row.Value)
		if err != nil {
			continue
		}
		value, _, err := projectionView(definition, state)
		if err == nil {
			values[key] = value
		}
	}
	return values
}

type ProjectionRestoreResult struct {
	Snapshot   ProjectionSnapshot
	Checkpoint ProjectionCheckpoint
}

// Restore folds registered units over a contiguous persisted suffix. baseSeq
// is the first raw Go event seq (1 for a full read).
func (r *SessionProjectionRegistry) Restore(checkpoint ProjectionCheckpoint, events []Event, baseSeq uint64, header SessionHeader) (ProjectionRestoreResult, error) {
	if baseSeq == 0 {
		return ProjectionRestoreResult{}, errors.New("harness: projection restore baseSeq must be positive")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.restoreLocked(checkpoint, events, baseSeq, header)
}

func (r *SessionProjectionRegistry) restoreLocked(checkpoint ProjectionCheckpoint, events []Event, baseSeq uint64, header SessionHeader) (ProjectionRestoreResult, error) {
	endSeq := baseSeq - 1
	if len(events) > 0 {
		endSeq = events[len(events)-1].Seq
	}
	values := make(map[string]json.RawMessage)
	refreshed := make(ProjectionCheckpoint, len(r.registrations))
	for _, key := range sortedProjectionKeys(r.registrations) {
		definition := r.registrations[key].definition
		row, exists := checkpoint[key]
		usable := exists && row.Version == definition.StateVersion && row.Seq >= baseSeq-1 && row.Seq <= endSeq
		if !usable && baseSeq > 1 {
			return ProjectionRestoreResult{}, fmt.Errorf("harness: session projection %q cannot restore from seq %d: checkpoint missing, version-mismatched, or beyond log end; re-read from seq 1", key, baseSeq)
		}
		var state any
		var err error
		if usable {
			state, err = callProjectionDecodeState(definition, row.Value)
		} else {
			state, err = callProjectionInit(definition, header)
		}
		if err != nil {
			return ProjectionRestoreResult{}, fmt.Errorf("harness: restore session projection %q state: %w", key, err)
		}
		from := baseSeq - 1
		if usable {
			from = row.Seq
		}
		for seq := from + 1; seq <= endSeq; seq++ {
			index := seq - baseSeq
			if index >= uint64(len(events)) || events[index].Seq != seq {
				return ProjectionRestoreResult{}, fmt.Errorf("harness: session projection %q cannot restore across missing seq %d", key, seq)
			}
			state, err = callProjectionApply(definition, state, events[index])
			if err != nil {
				return ProjectionRestoreResult{}, fmt.Errorf("harness: apply session projection %q at seq %d: %w", key, seq, err)
			}
		}
		encoded, err := callProjectionEncodeState(definition, state)
		if err != nil {
			return ProjectionRestoreResult{}, fmt.Errorf("harness: encode restored session projection %q: %w", key, err)
		}
		refreshed[key] = ProjectionCheckpointRow{Version: definition.StateVersion, Seq: endSeq, Value: encoded}
		if definition.Wire != nil {
			value, _, err := projectionView(definition, state)
			if err != nil {
				return ProjectionRestoreResult{}, err
			}
			values[key] = value
		}
	}
	return ProjectionRestoreResult{Snapshot: ProjectionSnapshot{AsOfSeq: endSeq, Values: values}, Checkpoint: refreshed}, nil
}

func (r *SessionProjectionRegistry) Hydrate(session *Session, checkpoint ProjectionCheckpoint, events []Event, baseSeq uint64) (ProjectionSnapshot, error) {
	if session == nil || baseSeq == 0 {
		return ProjectionSnapshot{}, errors.New("harness: projection hydrate requires a session and positive baseSeq")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	endSeq := baseSeq - 1
	if len(events) > 0 {
		endSeq = events[len(events)-1].Seq
	}
	complete := true
	for _, registration := range r.registrations {
		if cell := registration.cells[session]; cell == nil || cell.observedSeq != endSeq {
			complete = false
			break
		}
	}
	if complete {
		return r.snapshotFromCellsLocked(session, endSeq)
	}
	restored, err := r.restoreLocked(checkpoint, events, baseSeq, session.Header())
	if err != nil {
		return ProjectionSnapshot{}, err
	}
	for key, registration := range r.registrations {
		row := restored.Checkpoint[key]
		current := registration.cells[session]
		if current != nil && current.observedSeq > row.Seq {
			continue
		}
		state, err := callProjectionDecodeState(registration.definition, row.Value)
		if err != nil {
			return ProjectionSnapshot{}, err
		}
		registration.cells[session] = &projectionCell{state: state, observedSeq: row.Seq}
	}
	return restored.Snapshot, nil
}

func (r *SessionProjectionRegistry) snapshotFromCellsLocked(session *Session, asOf uint64) (ProjectionSnapshot, error) {
	values := make(map[string]json.RawMessage)
	for _, key := range sortedProjectionKeys(r.registrations) {
		registration := r.registrations[key]
		if registration.definition.Wire == nil {
			continue
		}
		cell := registration.cells[session]
		if cell == nil || cell.err != nil {
			if cell != nil {
				return ProjectionSnapshot{}, cell.err
			}
			continue
		}
		value, _, err := projectionView(registration.definition, cell.state)
		if err != nil {
			return ProjectionSnapshot{}, err
		}
		values[key] = value
	}
	return ProjectionSnapshot{AsOfSeq: asOf, Values: values}, nil
}

func (r *SessionProjectionRegistry) attach(session *Session) {
	if session == nil {
		return
	}
	session.notifyMu.Lock()
	defer session.notifyMu.Unlock()
	events := session.Events()
	unsubscribe := session.Subscribe(func(event Event) { r.drive(session, event) })
	r.mu.Lock()
	if r.disposed || r.sessions[session] != nil {
		r.mu.Unlock()
		unsubscribe()
		return
	}
	cells := make(map[string]*projectionCell, len(r.registrations))
	for key, registration := range r.registrations {
		cell := registration.cells[session]
		var err error
		if cell == nil {
			cell, err = buildProjectionCell(registration.definition, session.Header(), events)
		} else if cell.observedSeq < eventTail(events) {
			cell, err = advanceProjectionCell(registration.definition, cell, events)
		}
		if err != nil {
			cell = &projectionCell{observedSeq: eventTail(events), err: err}
		}
		cells[key] = cell
	}
	for key, cell := range cells {
		r.registrations[key].cells[session] = cell
	}
	r.sessions[session] = unsubscribe
	r.mu.Unlock()
}

func (r *SessionProjectionRegistry) detach(session *Session) {
	if session == nil {
		return
	}
	r.mu.Lock()
	unsubscribe, exists := r.sessions[session]
	if exists {
		delete(r.sessions, session)
		for _, registration := range r.registrations {
			delete(registration.cells, session)
		}
	}
	r.mu.Unlock()
	if exists && unsubscribe != nil {
		unsubscribe()
	}
}

func (r *SessionProjectionRegistry) drive(session *Session, event Event) {
	r.mu.Lock()
	if r.disposed {
		r.mu.Unlock()
		return
	}
	if _, attached := r.sessions[session]; !attached {
		r.mu.Unlock()
		return
	}
	listeners := r.listenerSnapshotLocked()
	notifications := make([]projectionNotification, 0)
	for _, key := range sortedProjectionKeys(r.registrations) {
		registration := r.registrations[key]
		cell := registration.cells[session]
		if cell == nil {
			prefix := session.Events()
			cut := len(prefix)
			for cut > 0 && prefix[cut-1].Seq >= event.Seq {
				cut--
			}
			var err error
			cell, err = buildProjectionCell(registration.definition, session.Header(), prefix[:cut])
			if err != nil {
				cell = &projectionCell{observedSeq: event.Seq - 1, err: err}
			}
			registration.cells[session] = cell
		}
		if cell.observedSeq >= event.Seq || cell.err != nil {
			continue
		}
		if cell.observedSeq+1 != event.Seq {
			cell.err = fmt.Errorf("harness: session projection %q cannot advance across missing seq %d", key, cell.observedSeq+1)
			continue
		}
		previous := cell.state
		next, err := callProjectionApply(registration.definition, previous, event)
		if err != nil {
			cell.err = fmt.Errorf("harness: apply session projection %q at seq %d: %w", key, event.Seq, err)
			continue
		}
		same, err := projectionSameState(registration.definition, previous, next)
		if err != nil {
			cell.err = err
			continue
		}
		changed := !same
		cell.state, cell.observedSeq = next, event.Seq
		if changed && registration.definition.Wire != nil {
			cell.previousView, cell.previousKnown = cell.currentView, cell.currentKnown
			if len(listeners) == 0 {
				cell.currentView, cell.currentKnown = nil, false
				continue
			}
			raw, value, viewErr := projectionView(registration.definition, next)
			if viewErr != nil {
				cell.err = viewErr
				continue
			}
			cell.currentView, cell.currentKnown = value, true
			if !cell.previousKnown || !sameProjectionIdentity(cell.previousView, value) {
				notifications = append(notifications, projectionNotification{session: session, key: key, value: raw, seq: event.Seq, listeners: listeners})
			}
		}
	}
	r.mu.Unlock()
	for _, notification := range notifications {
		for _, entry := range notification.listeners {
			func() {
				defer func() {
					if recovered := recover(); recovered != nil {
						r.logger.Warn("session projection listener panicked", "key", notification.key, "seq", notification.seq, "panic", recovered)
					}
				}()
				entry.listener(notification.session, notification.key, append(json.RawMessage(nil), notification.value...), notification.seq)
			}()
		}
	}
}

func (r *SessionProjectionRegistry) listenerSnapshotLocked() []projectionListenerEntry {
	ids := make([]uint64, 0, len(r.listeners))
	for id := range r.listeners {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	result := make([]projectionListenerEntry, 0, len(ids))
	for _, id := range ids {
		result = append(result, projectionListenerEntry{id: id, listener: r.listeners[id]})
	}
	return result
}

func sortedProjectionKeys(registrations map[string]*projectionRegistration) []string {
	keys := make([]string, 0, len(registrations))
	for key := range registrations {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func stringSet(values []string) map[string]bool {
	if len(values) == 0 {
		return nil
	}
	result := make(map[string]bool, len(values))
	for _, value := range values {
		result[value] = true
	}
	return result
}

func eventTail(events []Event) uint64 {
	if len(events) == 0 {
		return 0
	}
	return events[len(events)-1].Seq
}

func (r *SessionProjectionRegistry) Dispose(context.Context) error {
	r.mu.Lock()
	if r.disposed {
		r.mu.Unlock()
		return nil
	}
	r.disposed = true
	unsubscribeStore := r.unsubscribeStore
	r.unsubscribeStore = nil
	unsubscribers := make([]func(), 0, len(r.sessions))
	for _, unsubscribe := range r.sessions {
		if unsubscribe != nil {
			unsubscribers = append(unsubscribers, unsubscribe)
		}
	}
	r.sessions = make(map[*Session]func())
	r.registrations = make(map[string]*projectionRegistration)
	r.listeners = make(map[uint64]ProjectionChangeListener)
	r.mu.Unlock()
	if unsubscribeStore != nil {
		unsubscribeStore()
	}
	for _, unsubscribe := range unsubscribers {
		unsubscribe()
	}
	return nil
}
