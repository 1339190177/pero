package session

import (
	"errors"
	"os"
	"sync"
)

// ErrSessionAlreadyOwned means another attachment holds the Session's write
// lease. Read-only Load and List operations remain available.
var ErrSessionAlreadyOwned = errors.New("harness: session is already owned by a writer")

type sessionWriteLease struct {
	*os.File
	mu     sync.Mutex
	closed bool
}

// Close relinquishes ownership explicitly before closing the descriptor.
// A concurrent fork can inherit the open file description until exec, even
// with CLOEXEC. Closing only this process's descriptor does not release that
// inherited flock; explicit unlock does, after the owner has stopped writing.
// Serialize the complete descriptor operation so concurrent closes cannot act
// on a descriptor reused by a successor. A repeated close returns os.ErrClosed.
func (lease *sessionWriteLease) Close() error {
	lease.mu.Lock()
	defer lease.mu.Unlock()
	if lease.closed {
		return os.ErrClosed
	}
	lease.closed = true
	return errors.Join(unlockSessionFile(lease.File), lease.File.Close())
}

// acquireSessionWriteLease uses a stable sidecar inode rather than locking
// the log, whose initial snapshot is published by an exclusive hard link.
// The sidecar is deliberately retained after close; unlinking a live lock
// would let another process acquire a different inode for the same Session.
func acquireSessionWriteLease(path string) (*sessionWriteLease, error) {
	for range 3 {
		file, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
		if err != nil {
			return nil, err
		}
		if err := lockSessionFile(file); err != nil {
			return nil, errors.Join(err, file.Close())
		}
		lease := &sessionWriteLease{File: file}
		held, heldErr := file.Stat()
		current, currentErr := os.Stat(path)
		if heldErr == nil && currentErr == nil && os.SameFile(held, current) {
			return lease, nil
		}
		if err := lease.Close(); err != nil {
			return nil, err
		}
		if heldErr != nil {
			return nil, heldErr
		}
		if currentErr != nil && !errors.Is(currentErr, os.ErrNotExist) {
			return nil, currentErr
		}
	}
	return nil, ErrSessionAlreadyOwned
}
