package config

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	point "github.com/phodal/pero/harness/go/runtime"
	"sync"
)

type SettingsScope[T any] struct {
	registration *settingsRegistration
}

// HasUserSection reports whether this namespace has a stored user section.
func (s *SettingsScope[T]) HasUserSection() bool {
	if s == nil || s.registration == nil {
		return false
	}
	r := s.registration
	r.provider.mu.Lock()
	defer r.provider.mu.Unlock()
	if r.released || r.provider.disposed {
		return false
	}
	return r.hasUser
}

func (s *SettingsScope[T]) Get() (T, error) {
	var zero T
	if s == nil || s.registration == nil {
		return zero, NewProviderError("settings scope is unavailable", SettingsDisposedCode, nil)
	}
	r := s.registration
	r.provider.mu.Lock()
	if r.released || r.provider.disposed {
		r.provider.mu.Unlock()
		return zero, NewProviderError("settings scope was disposed", SettingsDisposedCode, nil)
	}
	detached, err := r.clone(r.current)
	r.provider.mu.Unlock()
	if err != nil {
		return zero, err
	}
	return detached.(T), nil
}

func (s *SettingsScope[T]) Revision() (uint64, error) {
	if s == nil || s.registration == nil {
		return 0, NewProviderError("settings scope is unavailable", SettingsDisposedCode, nil)
	}
	r := s.registration
	r.provider.mu.Lock()
	defer r.provider.mu.Unlock()
	if r.released || r.provider.disposed {
		return 0, NewProviderError("settings scope was disposed", SettingsDisposedCode, nil)
	}
	return r.revision, nil
}

func (s *SettingsScope[T]) Update(ctx context.Context, patch any, expectedRevision *uint64) error {
	raw, err := normalizeSettingsObjectRaw(patch, "patch")
	if err != nil {
		return err
	}
	return s.commit(ctx, raw, false, expectedRevision)
}

func (s *SettingsScope[T]) Replace(ctx context.Context, section any, expectedRevision *uint64) error {
	raw, err := normalizeSettingsObjectRaw(section, "section")
	if err != nil {
		return err
	}
	return s.commit(ctx, raw, true, expectedRevision)
}

func (s *SettingsScope[T]) commit(
	ctx context.Context,
	incoming json.RawMessage,
	replace bool,
	expectedRevision *uint64,
) error {
	if s == nil || s.registration == nil {
		return NewProviderError("settings scope is unavailable", SettingsDisposedCode, nil)
	}
	r := s.registration
	p := r.provider
	p.mu.Lock()
	if r.released || p.disposed {
		p.mu.Unlock()
		return NewProviderError("settings scope was disposed", SettingsDisposedCode, nil)
	}
	if expectedRevision != nil && *expectedRevision != r.revision {
		actual := r.revision
		p.mu.Unlock()
		return &SettingsConflictError{Namespace: r.namespace, Expected: *expectedRevision, Actual: actual}
	}
	mutation := func(current json.RawMessage, hasCurrent bool) (json.RawMessage, error) {
		if expectedRevision != nil && !sameRawSettingsSection(r.user, r.hasUser, current, hasCurrent) {
			return nil, &SettingsConflictError{
				Namespace: r.namespace, Expected: *expectedRevision, Actual: r.revision + 1,
			}
		}
		next := incoming
		if !replace {
			currentObject := map[string]any{}
			if hasCurrent {
				var decodeErr error
				currentObject, decodeErr = decodeSettingsObject(current)
				if decodeErr != nil {
					return nil, NewProviderError(
						fmt.Sprintf("settings namespace %q has an invalid stored section", r.namespace), InvalidSettingsCode, decodeErr,
					)
				}
			}
			patch, _ := decodeSettingsObject(incoming)
			next = encodeSettingsObject(deepMergeSettings(currentObject, patch))
		}
		if _, resolveErr := r.resolve(next, true); resolveErr != nil {
			return nil, NewProviderError(
				fmt.Sprintf("settings namespace %q rejected the candidate section", r.namespace), InvalidSettingsCode, resolveErr,
			)
		}
		return next, nil
	}
	document := cloneSettingsDocument(p.document)
	if p.backend != nil {
		var err error
		document, err = p.backend.CommitSection(ctx, r.namespace, mutation)
		if err != nil {
			p.mu.Unlock()
			return err
		}
	} else {
		next, err := mutation(r.user, r.hasUser)
		if err != nil {
			p.mu.Unlock()
			return err
		}
		if sameRawSettingsSection(r.user, r.hasUser, next, true) {
			p.mu.Unlock()
			return nil
		}
		document[r.namespace] = append(json.RawMessage(nil), next...)
	}
	notifications := p.publishLocked(document, SettingsSourceUpdate)
	p.mu.Unlock()
	p.notify(notifications)
	p.notifyDocument()
	return nil
}

func (s *SettingsScope[T]) Watch(run *point.Run, callback func(T, T)) (point.Dispose, error) {
	if s == nil || s.registration == nil || run == nil || !run.Active() {
		return nil, point.ErrInactiveRun
	}
	if callback == nil {
		return nil, errors.New("harness: settings watcher must not be nil")
	}
	r := s.registration
	r.provider.mu.Lock()
	if r.released || r.provider.disposed {
		r.provider.mu.Unlock()
		return nil, NewProviderError("settings scope was disposed", SettingsDisposedCode, nil)
	}
	r.nextWatcherID++
	id := r.nextWatcherID
	watcher := &settingsWatcher{
		id: id, active: true, callback: func(next, previous any) { callback(next.(T), previous.(T)) },
	}
	watcher.orderCond = sync.NewCond(&watcher.orderMu)
	r.watchers[id] = watcher
	r.provider.mu.Unlock()
	dispose := func(context.Context) error {
		r.provider.mu.Lock()
		delete(r.watchers, id)
		r.provider.mu.Unlock()
		watcher.orderMu.Lock()
		watcher.active = false
		watcher.orderCond.Broadcast()
		watcher.orderMu.Unlock()
		return nil
	}
	if err := run.Defer("settings:unwatch:"+r.namespace, dispose); err != nil {
		_ = dispose(context.Background())
		return nil, err
	}
	return dispose, nil
}
