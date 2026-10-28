package config

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"sort"
	"sync"
	"sync/atomic"

	point "github.com/phodal/pero/harness/go/runtime"
)

// WatchDocument emits payload-free invalidations after authoritative settings
// or their namespace topology change. Listeners must re-read redacted views;
// secret-bearing documents never enter this process notification surface.
func (p *SettingsProvider) WatchDocument(run *point.Run, listener func()) (point.Dispose, error) {
	if p == nil || listener == nil {
		return nil, errors.New("settings: provider and document listener required")
	}
	if run == nil || !run.Active() {
		return nil, point.ErrInactiveRun
	}
	p.mu.Lock()
	if p.disposed {
		p.mu.Unlock()
		return nil, NewProviderError("settings provider was disposed", SettingsDisposedCode, nil)
	}
	p.documentWatchMu.Lock()
	if p.documentWatchers == nil {
		p.documentWatchers = make(map[uint64]func())
	}
	p.nextDocumentWatcher++
	id := p.nextDocumentWatcher
	p.documentWatchers[id] = func() {
		p.documentWatchMu.Lock()
		_, active := p.documentWatchers[id]
		p.documentWatchMu.Unlock()
		if active && run.Active() {
			listener()
		}
	}
	p.documentWatchMu.Unlock()
	p.mu.Unlock()
	dispose := point.Dispose(func(context.Context) error {
		p.documentWatchMu.Lock()
		delete(p.documentWatchers, id)
		p.documentWatchMu.Unlock()
		return nil
	})
	if err := run.Defer("settings:document-watch", dispose); err != nil {
		_ = dispose(context.Background())
		return nil, err
	}
	return dispose, nil
}
func (p *SettingsProvider) notifyDocument() {
	p.documentWatchMu.Lock()
	listeners := make([]func(), 0, len(p.documentWatchers))
	for id := uint64(1); id <= p.nextDocumentWatcher; id++ {
		if fn := p.documentWatchers[id]; fn != nil {
			listeners = append(listeners, fn)
		}
	}
	p.documentWatchMu.Unlock()
	for _, listener := range listeners {
		func() {
			defer func() {
				if recovered := recover(); recovered != nil {
					p.logger.Warn("settings document observer panicked", "panic", recovered)
				}
			}()
			listener()
		}()
	}
}

// settingsNamespaceVersion is only an observation identity, never settings
// content or a synthesized revision. External descriptors are already redacted;
// their hash also detects presentation changes with an unchanged revision.
type settingsNamespaceVersion struct {
	owner        any
	revision     uint64
	presentation [32]byte
}

func (p *SettingsProvider) namespaceVersionLocked(namespace string) (settingsNamespaceVersion, bool) {
	if p.disposed {
		return settingsNamespaceVersion{}, false
	}
	if registration := p.registrations[namespace]; registration != nil && !registration.released {
		return settingsNamespaceVersion{owner: registration, revision: registration.revision}, true
	}
	source := p.externalNamespaceOwner[namespace]
	if source == nil || source.released {
		return settingsNamespaceVersion{}, false
	}
	index, ok := source.byName[namespace]
	if !ok {
		return settingsNamespaceVersion{}, false
	}
	descriptor := source.namespaces[index]
	encoded, _ := json.Marshal(descriptor)
	return settingsNamespaceVersion{owner: source, revision: descriptor.Revision, presentation: sha256.Sum256(encoded)}, true
}
func (p *SettingsProvider) namespaceVersions() map[string]settingsNamespaceVersion {
	p.mu.Lock()
	defer p.mu.Unlock()
	versions := make(map[string]settingsNamespaceVersion, len(p.registrations)+len(p.externalNamespaceOwner))
	for namespace := range p.registrations {
		if value, ok := p.namespaceVersionLocked(namespace); ok {
			versions[namespace] = value
		}
	}
	for namespace := range p.externalNamespaceOwner {
		if value, ok := p.namespaceVersionLocked(namespace); ok {
			versions[namespace] = value
		}
	}
	return versions
}

// WatchNamespaces reports actual current public descriptor (ns, revision)
// tuples after successful mutation/publication. Superseded same-id owners and
// delayed callbacks recheck current ownership and serialize delivery. Namespace removal has no
// descriptor revision and remains a distinct WatchDocument invalidation.
func (p *SettingsProvider) WatchNamespaces(run *point.Run, listener func(string, uint64)) (point.Dispose, error) {
	if p == nil || listener == nil {
		return nil, errors.New("settings: provider and namespace listener required")
	}
	previous := p.namespaceVersions()
	var delivery sync.Mutex
	var delivering, pending bool
	var alive atomic.Bool
	alive.Store(true)
	synchronize := func() {
		if !alive.Load() || run == nil || !run.Active() {
			return
		}
		delivery.Lock()
		pending = true
		if delivering {
			delivery.Unlock()
			return
		}
		delivering = true
		for {
			pending = false
			next := p.namespaceVersions()
			changed := make([]string, 0)
			for namespace, value := range next {
				if old, found := previous[namespace]; !found || old != value {
					changed = append(changed, namespace)
				}
			}
			previous = next
			delivery.Unlock()
			sort.Strings(changed)
			for _, namespace := range changed {
				if !alive.Load() || !run.Active() {
					break
				}
				p.mu.Lock()
				current, exists := p.namespaceVersionLocked(namespace)
				p.mu.Unlock()
				if exists && current == next[namespace] {
					// A single drain serializes callbacks without holding either
					// mutex. Reentrant writes queue the next authoritative scan.
					func() {
						defer func() {
							if recovered := recover(); recovered != nil {
								p.logger.Warn("settings namespace observer panicked", "panic", recovered)
							}
						}()
						listener(namespace, current.revision)
					}()
				}
			}
			delivery.Lock()
			if !pending || !alive.Load() || !run.Active() {
				delivering = false
				delivery.Unlock()
				return
			}
		}
	}

	dispose, err := p.WatchDocument(run, synchronize)
	if err != nil {
		alive.Store(false)
		return nil, err
	}
	// Reconcile any authoritative change between the first snapshot and watch
	// admission; queued control events still follow the readiness frame.
	synchronize()
	return func(ctx context.Context) error { alive.Store(false); return dispose(ctx) }, nil
}
