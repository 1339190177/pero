package session

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
)

// SessionPersistence materializes committed session facts and restores them
// without replaying live notifications.
type SessionPersistence interface {
	Attach(context.Context, *Session) (func() error, error)
	Load(context.Context, SessionID) (*Session, error)
	List(context.Context) ([]SessionHeader, error)
}

// SessionIdentityPersistence optionally checks an occupied durable identity
// without materializing or validating the full event log. A malformed existing
// entry still occupies its identity and must not be overwritten as a new child.
type SessionIdentityPersistence interface {
	Exists(context.Context, SessionID) (bool, error)
}

type SessionLocation struct {
	Kind string
	Path string
}

// SessionPersistenceLocator is the optional path projection used by compatible
// shell and hook integrations. Locate is a pure calculation and does not imply
// that the Session has already been materialized.
type SessionPersistenceLocator interface {
	Locate(SessionHeader) (SessionLocation, bool)
}

// SessionPersistenceCandidate is the header-level enumeration boundary used
// by projection consumers that must contain one damaged session without
// failing an otherwise healthy catalog.
type SessionPersistenceCandidate struct {
	ID     SessionID
	Header *SessionHeader
	Err    error
}

// SessionCatalogPersistence optionally exposes per-session header outcomes.
// Load remains authoritative for the full event log.
type SessionCatalogPersistence interface {
	ListCandidates(context.Context) ([]SessionPersistenceCandidate, error)
}

// JSONLSessionPersistence stores one header followed by committed events in a
// file per session. Appends are synchronous so a published event is durable
// before downstream protocol updates observe it.
type JSONLSessionPersistence struct {
	dir                 string
	mu                  sync.Mutex
	recoverCompleteTail bool
}

// JSONLPersistenceOptions permits explicit, lossless delimiter recovery only.
type JSONLPersistenceOptions struct{ RecoverCompleteTail bool }

func NewJSONLSessionPersistence(dir string, options ...JSONLPersistenceOptions) (*JSONLSessionPersistence, error) {
	if dir == "" {
		return nil, errors.New("harness: persistence directory is empty")
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	if len(options) > 1 {
		return nil, errors.New("harness: at most one persistence options value is supported")
	}
	p := &JSONLSessionPersistence{dir: dir}
	if len(options) == 1 {
		p.recoverCompleteTail = options[0].RecoverCompleteTail
	}
	return p, nil
}

type persistedLine struct {
	Header *SessionHeader `json:"header,omitempty"`
	Event  *Event         `json:"event,omitempty"`
}

func (p *JSONLSessionPersistence) Attach(ctx context.Context, session *Session) (_ func() error, resultErr error) {
	if session == nil {
		return nil, errors.New("harness: cannot persist nil session")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	// Lifecycle consumers may race to attach the same live Session. Serialize
	// through checkpoint publication so the loser receives the established
	// same-identity error, never a misleading competing-writer lease failure.
	session.persistenceAttachMu.Lock()
	defer session.persistenceAttachMu.Unlock()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if session.DurabilityAttached() {
		return nil, ErrSessionDurabilityAttached
	}
	// A persistence object or Session pointer is not a writer identity. Hold
	// a kernel lease across the entire attachment, including snapshots and
	// final flush, so another backend/process cannot append a competing seq.
	lease, err := acquireSessionWriteLease(p.path(session.header.ID) + ".lock")
	if err != nil {
		return nil, fmt.Errorf("harness: attach session %q: %w", session.header.ID, err)
	}
	attached := false
	defer func() {
		if !attached {
			resultErr = errors.Join(resultErr, lease.Close())
		}
	}()
	var writeErr error
	var writeMu sync.Mutex
	closed := false
	path := p.path(session.header.ID)
	p.mu.Lock()
	session.mu.Lock()
	_, statErr := os.Stat(path)
	created := errors.Is(statErr, os.ErrNotExist)
	persistedCount := len(session.events)
	if statErr != nil && !created {
		session.mu.Unlock()
		p.mu.Unlock()
		return nil, statErr
	}
	if !created {
		// Cold observation does not reserve a write generation. Revalidate
		// after acquiring the lease instead of appending stale in-memory
		// state over a log that another owner has advanced or replaced.
		stored, err := p.Load(ctx, session.header.ID)
		if err == nil {
			persistedCount = len(stored.events)
			err = comparePersistedSession(stored, session)
		}
		if err == nil && p.recoverCompleteTail {
			err = restoreCompleteTailDelimiter(path)
		}
		if err != nil {
			session.mu.Unlock()
			p.mu.Unlock()
			return nil, err
		}
	}
	if created {
		if err := writeSessionSnapshotExclusive(p.dir, path, session.header, session.events); err != nil {
			session.mu.Unlock()
			p.mu.Unlock()
			return nil, err
		}
	}
	file, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		if created {
			_ = os.Remove(path)
		}
		session.mu.Unlock()
		p.mu.Unlock()
		return nil, err
	}
	encoder := json.NewEncoder(file)
	// All writes share a cursor. A flush may run from an earlier observer,
	// before this listener receives an already committed event; encoding the
	// committed suffix there must make the later notification a no-op.
	writeEvent := func(event Event) {
		if writeErr != nil || closed || event.Seq <= uint64(persistedCount) {
			return
		}
		if event.Seq != uint64(persistedCount+1) {
			writeErr = fmt.Errorf("harness: non-contiguous session persistence append at seq %d", event.Seq)
			return
		}
		writeErr = encoder.Encode(persistedLine{Event: &event})
		if writeErr == nil {
			persistedCount++
		}
	}
	// Restore admission may append repair/configuration facts before a Host
	// installs persistence. Only the immutable, byte-validated load frontier
	// authorizes this suffix; a disk advanced by another writer still refuses.
	for _, event := range session.events[persistedCount:] {
		writeEvent(event)
	}
	if writeErr == nil {
		writeErr = file.Sync()
	}
	if writeErr != nil {
		session.mu.Unlock()
		p.mu.Unlock()
		return nil, errors.Join(writeErr, file.Close())
	}
	drainCommitted := func() error {
		if writeErr != nil {
			return writeErr
		}
		session.mu.RLock()
		pending := cloneEvents(session.events[persistedCount:])
		session.mu.RUnlock()
		for _, event := range pending {
			writeEvent(event)
		}
		return writeErr
	}
	listener := func(event Event) {
		writeMu.Lock()
		defer writeMu.Unlock()
		writeEvent(event)
	}
	session.nextSubID++
	subscriberID := session.nextSubID
	session.subscribers[subscriberID] = listener
	session.mu.Unlock()
	p.mu.Unlock()
	var unsubscribeOnce sync.Once
	unsubscribe := func() {
		unsubscribeOnce.Do(func() {
			session.mu.Lock()
			delete(session.subscribers, subscriberID)
			session.mu.Unlock()
		})
	}
	detachCheckpoint, err := session.InstallDurabilityCheckpoint(func(ctx context.Context) error {
		writeMu.Lock()
		defer writeMu.Unlock()
		if err := ctx.Err(); err != nil {
			return err
		}
		if closed {
			return errors.New("harness: session persistence is detached")
		}
		if err := drainCommitted(); err != nil {
			return err
		}
		return file.Sync()
	})
	if err != nil {
		unsubscribe()
		writeMu.Lock()
		closed = true
		closeErr := file.Close()
		writeMu.Unlock()
		if created {
			_ = os.Remove(path)
		}
		return nil, errors.Join(err, closeErr)
	}
	var once sync.Once
	var closeErr error
	attached = true
	return func() error {
		once.Do(func() {
			session.persistenceAttachMu.Lock()
			defer session.persistenceAttachMu.Unlock()
			detachCheckpoint()
			unsubscribe()
			writeMu.Lock()
			defer writeMu.Unlock()
			drainErr := drainCommitted()
			closed = true
			closeErr = errors.Join(drainErr, file.Sync(), file.Close(), lease.Close())
		})
		return closeErr
	}, nil
}

// ErrSessionPersistenceConflict refuses write attachment of a different or
// stale in-memory snapshot. Load again after the previous writer detaches.
var ErrSessionPersistenceConflict = errors.New("harness: persisted session differs from attachment snapshot")

// comparePersistedSession runs while the candidate's commit lock is held.
// Compare serialized records rather than Go time locations or nil/empty
// implementation details that the JSONL format does not distinguish.
func comparePersistedSession(stored, candidate *Session) error {
	if len(stored.events) > len(candidate.events) {
		return ErrSessionPersistenceConflict
	}
	if len(stored.events) < len(candidate.events) &&
		(!candidate.restored.Load() || candidate.firstLiveSeq != uint64(len(stored.events)+1)) {
		return ErrSessionPersistenceConflict
	}
	equal := func(left, right persistedLine) bool {
		leftJSON, leftErr := json.Marshal(left)
		rightJSON, rightErr := json.Marshal(right)
		return leftErr == nil && rightErr == nil && bytes.Equal(leftJSON, rightJSON)
	}
	if !equal(persistedLine{Header: &stored.header}, persistedLine{Header: &candidate.header}) {
		return ErrSessionPersistenceConflict
	}
	for index := range stored.events {
		if !equal(persistedLine{Event: &stored.events[index]}, persistedLine{Event: &candidate.events[index]}) {
			return ErrSessionPersistenceConflict
		}
	}
	return nil
}

// Exists is read-only. Final Attach writer leases still arbitrate a concurrent
// creator between this preflight and actual Session materialization.
func (p *JSONLSessionPersistence) Exists(ctx context.Context, id SessionID) (bool, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return false, err
	}
	_, err := os.Lstat(p.path(id))
	if canceled := ctx.Err(); canceled != nil {
		return false, canceled
	}
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	return err == nil, err
}

func (p *JSONLSessionPersistence) Load(ctx context.Context, id SessionID) (*Session, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	file, err := os.Open(p.path(id))
	if err != nil {
		return nil, err
	}
	defer file.Close()
	scanner := bufio.NewScanner(file)
	if !p.recoverCompleteTail {
		scanner.Split(scanPersistedLine)
	}
	scanner.Buffer(make([]byte, 64*1024), 16*1024*1024)
	var header *SessionHeader
	var events []Event
	for scanner.Scan() {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		var line persistedLine
		if err := json.Unmarshal(scanner.Bytes(), &line); err != nil {
			return nil, fmt.Errorf("harness: decode persisted session %q: %w", id, err)
		}
		if (line.Header == nil) == (line.Event == nil) {
			return nil, fmt.Errorf("harness: persisted session %q requires exactly one header or event per line", id)
		}
		if line.Header != nil {
			if header != nil || line.Header.ID != id || line.Header.Version != SessionFormatVersion {
				return nil, fmt.Errorf("harness: invalid persisted header for %q", id)
			}
			header = line.Header
			continue
		}
		if header == nil || line.Event.Seq != uint64(len(events)+1) {
			return nil, fmt.Errorf("harness: non-contiguous persisted event for %q", id)
		}
		events = append(events, *line.Event)
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	if header == nil {
		return nil, fmt.Errorf("harness: persisted session %q has no header", id)
	}
	// Physical JSONL admission cannot be weaker than any other restoration
	// path. RestoreSession owns header identity, context-plan, dispatch,
	// surface and compaction validation as those invariants evolve.
	session, err := RestoreSession(*header, events)
	if err != nil {
		return nil, fmt.Errorf("harness: validate persisted session %q: %w", id, err)
	}
	return session, nil
}

func (p *JSONLSessionPersistence) List(ctx context.Context) ([]SessionHeader, error) {
	candidates, err := p.ListCandidates(ctx)
	if err != nil {
		return nil, err
	}
	result := make([]SessionHeader, 0, len(candidates))
	for _, candidate := range candidates {
		if candidate.Err != nil {
			return nil, candidate.Err
		}
		if candidate.Header != nil {
			result = append(result, *candidate.Header)
		}
	}
	return result, nil
}

func (p *JSONLSessionPersistence) ListCandidates(ctx context.Context) ([]SessionPersistenceCandidate, error) {
	entries, err := os.ReadDir(p.dir)
	if err != nil {
		return nil, err
	}
	result := make([]SessionPersistenceCandidate, 0, len(entries))
	for _, entry := range entries {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".jsonl" {
			continue
		}
		id := SessionID(strings.TrimSuffix(entry.Name(), ".jsonl"))
		header, headerErr := readPersistedSessionHeader(p.path(id), id)
		candidate := SessionPersistenceCandidate{ID: id, Err: headerErr}
		if headerErr == nil {
			candidate.Header = &header
		}
		result = append(result, candidate)
	}
	sort.Slice(result, func(i, j int) bool {
		left, right := result[i].Header, result[j].Header
		if left != nil && right != nil && !left.CreatedAt.Equal(right.CreatedAt) {
			return left.CreatedAt.After(right.CreatedAt)
		}
		if left != nil && right == nil {
			return true
		}
		if left == nil && right != nil {
			return false
		}
		return result[i].ID < result[j].ID
	})
	return result, nil
}

// restoreCompleteTailDelimiter runs only after full logical validation and stale
// state comparison under the writer lease. It never discards a byte. A private,
// exclusive backup must be durable before the original log can be changed.
func restoreCompleteTailDelimiter(path string) (resultErr error) {
	file, err := os.OpenFile(path, os.O_RDWR, 0)
	if err != nil {
		return err
	}
	defer func() { resultErr = errors.Join(resultErr, file.Close()) }()
	info, err := file.Stat()
	if err != nil {
		return err
	}
	if info.Size() == 0 {
		return errors.New("harness: cannot recover empty session")
	}
	var last [1]byte
	if _, err = file.ReadAt(last[:], info.Size()-1); err != nil {
		return err
	}
	if last[0] == '\n' {
		return nil
	}
	backup, err := os.OpenFile(path+".tail-backup", os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return fmt.Errorf("harness: preserve session tail: %w", err)
	}
	copied, copyErr := io.Copy(backup, file)
	if copyErr == nil && copied != info.Size() {
		copyErr = errors.New("harness: session size changed during backup")
	}
	backupErr := errors.Join(copyErr, backup.Sync(), backup.Close())
	if backupErr != nil {
		return fmt.Errorf("harness: preserve session tail: %w", backupErr)
	}
	if err = syncDirectory(filepath.Dir(path)); err != nil {
		return err
	}
	if _, err = file.WriteAt([]byte("\n"), info.Size()); err != nil {
		return err
	}
	return file.Sync()
}

// JSONL records are committed only with their newline delimiter. Accepting a
// complete JSON object at EOF would let the next append concatenate two records.
// Keep torn tails untouched and fail closed until explicitly repaired.
func scanPersistedLine(data []byte, atEOF bool) (int, []byte, error) {
	if atEOF && len(data) > 0 && bytes.IndexByte(data, '\n') < 0 {
		return 0, nil, errors.New("harness: unterminated persisted JSONL record")
	}
	return bufio.ScanLines(data, atEOF)
}

func readPersistedSessionHeader(path string, id SessionID) (SessionHeader, error) {
	file, err := os.Open(path)
	if err != nil {
		return SessionHeader{}, err
	}
	defer file.Close()
	scanner := bufio.NewScanner(file)
	scanner.Split(scanPersistedLine)
	scanner.Buffer(make([]byte, 64*1024), 16*1024*1024)
	if !scanner.Scan() {
		if err := scanner.Err(); err != nil {
			return SessionHeader{}, err
		}
		return SessionHeader{}, fmt.Errorf("harness: persisted session %q has no header", id)
	}
	var line persistedLine
	if err := json.Unmarshal(scanner.Bytes(), &line); err != nil {
		return SessionHeader{}, fmt.Errorf("harness: decode persisted header %q: %w", id, err)
	}
	if line.Header == nil || line.Event != nil || line.Header.ID != id || line.Header.Version != SessionFormatVersion {
		return SessionHeader{}, fmt.Errorf("harness: invalid persisted header for %q", id)
	}
	return *line.Header, nil
}

func (p *JSONLSessionPersistence) path(id SessionID) string {
	name := string(id)
	if name == "" || strings.ContainsAny(name, "/\\\x00") || name == "." || name == ".." {
		name = fmt.Sprintf("invalid-%x", []byte(name))
	}
	return filepath.Join(p.dir, name+".jsonl")
}

func (p *JSONLSessionPersistence) Locate(header SessionHeader) (SessionLocation, bool) {
	if p == nil || header.ID == "" {
		return SessionLocation{}, false
	}
	path, err := filepath.Abs(p.path(header.ID))
	if err != nil {
		return SessionLocation{}, false
	}
	return SessionLocation{Kind: "jsonl", Path: path}, true
}

func writeSessionSnapshotExclusive(dir, path string, header SessionHeader, events []Event) (result error) {
	file, err := os.CreateTemp(dir, ".session-*.tmp")
	if err != nil {
		return err
	}
	temporary := file.Name()
	fileClosed := false
	defer func() {
		if !fileClosed {
			result = errors.Join(result, file.Close())
		}
		_ = os.Remove(temporary)
	}()
	if err := file.Chmod(0o600); err != nil {
		return err
	}
	encoder := json.NewEncoder(file)
	if err := encoder.Encode(persistedLine{Header: &header}); err != nil {
		return err
	}
	for index := range events {
		event := events[index]
		if err := encoder.Encode(persistedLine{Event: &event}); err != nil {
			return err
		}
	}
	if err := file.Sync(); err != nil {
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	fileClosed = true
	if err := os.Link(temporary, path); err != nil {
		return err
	}
	if err := syncDirectory(dir); err != nil {
		_ = os.Remove(path)
		return err
	}
	return nil
}
