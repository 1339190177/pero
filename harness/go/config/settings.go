package config

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	point "github.com/phodal/pero/harness/go/runtime"
	"log/slog"
	"regexp"
	"sync"
)

const (
	InvalidSettingsCode   = "INVALID_SETTINGS"
	DuplicateSettingsCode = "DUPLICATE_SETTINGS"
	SettingsConflictCode  = "SETTINGS_CONFLICT"
	SettingsDisposedCode  = "SETTINGS_DISPOSED"
)

var settingsNamespacePattern = regexp.MustCompile(`^[a-z][a-z0-9-]*$`)

type SettingsApplies string

const (
	SettingsLive    SettingsApplies = "live"
	SettingsRestart SettingsApplies = "restart"
)

type SettingsUpdateSource string

const (
	SettingsSourceUpdate   SettingsUpdateSource = "update"
	SettingsSourceProvider SettingsUpdateSource = "provider"
)

// SettingsBackend owns the raw user document. WriteSection must serialize
// against other writers, re-read authoritative storage, mutate only namespace,
// and return the complete committed document. A mutation may return a nil
// section to preserve the authoritative section, including its absent state.
type SettingsBackend interface {
	Load(context.Context) (map[string]json.RawMessage, error)
	CommitSection(context.Context, string, SettingsMutation) (map[string]json.RawMessage, error)
}

type SettingsMutation func(json.RawMessage, bool) (json.RawMessage, error)

type SettingsWatchBackend interface {
	StartWatching(*point.Run, *SettingsProvider) error
}

type SettingsSectionSpec[T any] struct {
	Defaults any
	Base     any
	Schema   json.RawMessage
	Applies  SettingsApplies
	Decode   func(json.RawMessage) (T, error)
	Validate func(T) error
}

type SettingsDescribeOptions struct {
	RedactSecrets bool
}

type SettingsSecretView struct {
	Path []string `json:"path"`
	Set  bool     `json:"set"`
}

type SettingsDescriptor struct {
	AutoGenerate *bool                `json:"autoGenerate,omitempty"`
	Namespace    string               `json:"ns"`
	Schema       json.RawMessage      `json:"schema,omitempty"`
	Value        json.RawMessage      `json:"value"`
	Revision     uint64               `json:"revision"`
	Base         json.RawMessage      `json:"base,omitempty"`
	User         json.RawMessage      `json:"user,omitempty"`
	Applies      SettingsApplies      `json:"applies"`
	Secrets      []SettingsSecretView `json:"secrets,omitempty"`
}

// SettingsPathOp is the DSH-compatible path mutation used by configuration
// surfaces that hold a partial or redacted section. An empty path addresses
// the complete user section; nested paths address object keys only.
type SettingsPathOp struct {
	Op    string          `json:"op"`
	Path  []string        `json:"path"`
	Value json.RawMessage `json:"value,omitempty"`
}

type SettingsConflictError struct {
	Namespace string
	Expected  uint64
	Actual    uint64
}

func (e *SettingsConflictError) Error() string {
	return fmt.Sprintf(
		"settings namespace %q changed since it was read (expected revision %d, now %d)",
		e.Namespace, e.Expected, e.Actual,
	)
}

func (e *SettingsConflictError) Code() string { return SettingsConflictCode }

type settingsWatcher struct {
	id       uint64
	callback func(any, any)

	orderMu   sync.Mutex
	orderCond *sync.Cond
	issued    uint64
	completed uint64
	active    bool
}

type settingsWatcherInvocation struct {
	watcher *settingsWatcher
	ordinal uint64
}

type settingsRegistration struct {
	provider  *SettingsProvider
	owner     *point.Run
	namespace string
	schema    json.RawMessage
	base      json.RawMessage
	applies   SettingsApplies

	resolve func(json.RawMessage, bool) (any, error)
	clone   func(any) (any, error)
	encode  func(any) (json.RawMessage, error)

	current  any
	user     json.RawMessage
	hasUser  bool
	revision uint64
	released bool

	watchers      map[uint64]*settingsWatcher
	nextWatcherID uint64
}

type SettingsProvider struct {
	documentWatchMu     sync.Mutex
	nextDocumentWatcher uint64
	documentWatchers    map[uint64]func()
	backend             SettingsBackend
	logger              *slog.Logger

	mu                     sync.Mutex
	document               map[string]json.RawMessage
	registrations          map[string]*settingsRegistration
	order                  []string
	externalSources        []*externalSettingsSource
	externalSourceIDs      map[string]*externalSettingsSource
	externalNamespaceOwner map[string]*externalSettingsSource
	disposed               bool
}

// Writable reports whether settings changes have a durable backend. A core
// provider without one still serves composed defaults, but feature services
// that promise persistence can preserve DSH's no-op save behavior.
func (p *SettingsProvider) Writable() bool {
	return p != nil && p.backend != nil
}

func OpenSettingsProvider(ctx context.Context, backend SettingsBackend, logger *slog.Logger) (*SettingsProvider, error) {
	if logger == nil {
		logger = slog.Default()
	}
	document := make(map[string]json.RawMessage)
	if backend != nil {
		loaded, err := backend.Load(ctx)
		if err != nil {
			return nil, err
		}
		document = cloneSettingsDocument(loaded)
	}
	return &SettingsProvider{
		backend: backend, logger: logger, document: document, registrations: make(map[string]*settingsRegistration),
		externalSourceIDs: make(map[string]*externalSettingsSource), externalNamespaceOwner: make(map[string]*externalSettingsSource),
	}, nil
}

func cloneSettingsDocument(source map[string]json.RawMessage) map[string]json.RawMessage {
	result := make(map[string]json.RawMessage, len(source))
	for namespace, section := range source {
		result[namespace] = append(json.RawMessage(nil), section...)
	}
	return result
}

func validateSettingsNamespace(namespace string) error {
	if !settingsNamespacePattern.MatchString(namespace) {
		return NewProviderError(
			fmt.Sprintf("settings namespace %q must match %s", namespace, settingsNamespacePattern), InvalidSettingsCode, nil,
		)
	}
	return nil
}

func RegisterSettingsSection[T any](
	provider *SettingsProvider,
	run *point.Run,
	namespace string,
	spec SettingsSectionSpec[T],
) (*SettingsScope[T], error) {
	if provider == nil || run == nil || !run.Active() {
		return nil, point.ErrInactiveRun
	}
	if err := validateSettingsNamespace(namespace); err != nil {
		return nil, err
	}
	if spec.Applies == "" {
		spec.Applies = SettingsLive
	}
	if spec.Applies != SettingsLive && spec.Applies != SettingsRestart {
		return nil, NewProviderError("settings applies must be live or restart", InvalidSettingsCode, nil)
	}
	defaults, err := normalizeSettingsObject(spec.Defaults, "defaults")
	if err != nil {
		return nil, err
	}
	base, err := normalizeSettingsObject(spec.Base, "base")
	if err != nil {
		return nil, err
	}
	decode := spec.Decode
	if decode == nil {
		decode = func(raw json.RawMessage) (T, error) {
			var result T
			err := json.Unmarshal(raw, &result)
			return result, err
		}
	}
	resolve := func(user json.RawMessage, hasUser bool) (T, error) {
		merged := deepMergeSettings(map[string]any{}, defaults)
		merged = deepMergeSettings(merged, base)
		if hasUser {
			userObject, objectErr := decodeSettingsObject(user)
			if objectErr != nil {
				var zero T
				return zero, objectErr
			}
			merged = deepMergeSettings(merged, userObject)
		}
		raw, marshalErr := json.Marshal(merged)
		if marshalErr != nil {
			var zero T
			return zero, marshalErr
		}
		value, decodeErr := decode(raw)
		if decodeErr != nil {
			var zero T
			return zero, decodeErr
		}
		if spec.Validate != nil {
			if validateErr := spec.Validate(value); validateErr != nil {
				var zero T
				return zero, validateErr
			}
		}
		return value, nil
	}
	clone := func(value T) (T, error) {
		raw, marshalErr := json.Marshal(value)
		if marshalErr != nil {
			var zero T
			return zero, marshalErr
		}
		return decode(raw)
	}
	registration := &settingsRegistration{
		provider: provider, owner: run, namespace: namespace, schema: append(json.RawMessage(nil), spec.Schema...),
		base: encodeSettingsObject(base), applies: spec.Applies, watchers: make(map[uint64]*settingsWatcher),
		resolve: func(raw json.RawMessage, found bool) (any, error) { return resolve(raw, found) },
		clone:   func(value any) (any, error) { return clone(value.(T)) },
		encode:  func(value any) (json.RawMessage, error) { return json.Marshal(value.(T)) },
	}
	provider.mu.Lock()
	if provider.disposed {
		provider.mu.Unlock()
		return nil, NewProviderError("settings provider was disposed", SettingsDisposedCode, nil)
	}
	if _, duplicate := provider.registrations[namespace]; duplicate {
		provider.mu.Unlock()
		return nil, NewProviderError(
			fmt.Sprintf("settings namespace %q is already registered", namespace), DuplicateSettingsCode, nil,
		)
	}
	user, hasUser := provider.document[namespace]
	current, err := registration.resolve(user, hasUser)
	if err != nil {
		provider.mu.Unlock()
		return nil, NewProviderError(
			fmt.Sprintf("settings namespace %q cannot resolve its stored section", namespace), InvalidSettingsCode, err,
		)
	}
	registration.current = current
	registration.user = append(json.RawMessage(nil), user...)
	registration.hasUser = hasUser
	provider.registrations[namespace] = registration
	provider.order = append(provider.order, namespace)
	provider.mu.Unlock()
	if err := run.Defer("settings:unregister:"+namespace, registration.dispose); err != nil {
		_ = registration.dispose(context.Background())
		return nil, err
	}
	provider.notifyDocument()
	return &SettingsScope[T]{registration: registration}, nil
}

func (r *settingsRegistration) dispose(context.Context) error {
	p := r.provider
	p.mu.Lock()
	if r.released {
		p.mu.Unlock()
		return nil
	}
	r.released = true
	delete(p.registrations, r.namespace)
	nextOrder := p.order[:0]
	for _, namespace := range p.order {
		if namespace != r.namespace {
			nextOrder = append(nextOrder, namespace)
		}
	}
	p.order = nextOrder
	for _, watcher := range r.watchers {
		watcher.orderMu.Lock()
		watcher.active = false
		watcher.orderCond.Broadcast()
		watcher.orderMu.Unlock()
	}
	r.watchers = nil
	p.mu.Unlock()
	p.notifyDocument()
	return nil
}

type settingsNotification struct {
	namespace string
	watchers  []settingsWatcherInvocation
	next      any
	previous  any
	clone     func(any) (any, error)
}

func (p *SettingsProvider) publishLocked(
	document map[string]json.RawMessage,
	source SettingsUpdateSource,
) []settingsNotification {
	_ = source
	notifications := make([]settingsNotification, 0)
	for _, namespace := range p.order {
		registration := p.registrations[namespace]
		if registration == nil || registration.released {
			continue
		}
		nextUser, hasNextUser := document[namespace]
		rawChanged := !sameRawSettingsSection(
			registration.user, registration.hasUser, nextUser, hasNextUser,
		)
		if rawChanged {
			registration.revision++
			registration.user = append(json.RawMessage(nil), nextUser...)
			registration.hasUser = hasNextUser
		}
		next, err := registration.resolve(nextUser, hasNextUser)
		if err != nil {
			if rawChanged {
				p.logger.Warn("settings kept last-good value after invalid provider snapshot", "namespace", namespace, "error", err)
			}
			continue
		}
		previousRaw, previousErr := registration.encode(registration.current)
		nextRaw, nextErr := registration.encode(next)
		if previousErr != nil || nextErr != nil || bytes.Equal(previousRaw, nextRaw) {
			continue
		}
		previous := registration.current
		registration.current = next
		watchers := make([]settingsWatcherInvocation, 0, len(registration.watchers))
		for id := uint64(1); id <= registration.nextWatcherID; id++ {
			if watcher, found := registration.watchers[id]; found {
				watcher.orderMu.Lock()
				watcher.issued++
				ordinal := watcher.issued
				watcher.orderMu.Unlock()
				watchers = append(watchers, settingsWatcherInvocation{watcher: watcher, ordinal: ordinal})
			}
		}
		notifications = append(notifications, settingsNotification{
			namespace: namespace, watchers: watchers, next: next, previous: previous, clone: registration.clone,
		})
	}
	p.document = cloneSettingsDocument(document)
	return notifications
}

func (p *SettingsProvider) notify(notifications []settingsNotification) {
	for _, notification := range notifications {
		for _, invocation := range notification.watchers {
			func() {
				watcher := invocation.watcher
				watcher.orderMu.Lock()
				for watcher.completed+1 != invocation.ordinal {
					watcher.orderCond.Wait()
				}
				active := watcher.active
				watcher.orderMu.Unlock()
				defer func() {
					watcher.orderMu.Lock()
					watcher.completed = invocation.ordinal
					watcher.orderCond.Broadcast()
					watcher.orderMu.Unlock()
				}()
				if !active {
					return
				}
				defer func() {
					if recovered := recover(); recovered != nil {
						p.logger.Warn("settings watcher panicked", "namespace", notification.namespace, "panic", recovered)
					}
				}()
				next, nextErr := notification.clone(notification.next)
				previous, previousErr := notification.clone(notification.previous)
				if nextErr != nil || previousErr != nil {
					p.logger.Warn("settings watcher value clone failed", "namespace", notification.namespace)
					return
				}
				watcher.callback(next, previous)
			}()
		}
	}
}

func (p *SettingsProvider) Reload(ctx context.Context) error {
	if p == nil || p.backend == nil {
		return nil
	}
	document, err := p.backend.Load(ctx)
	if err != nil {
		return err
	}
	p.mu.Lock()
	if p.disposed {
		p.mu.Unlock()
		return NewProviderError("settings provider was disposed", SettingsDisposedCode, nil)
	}
	notifications := p.publishLocked(document, SettingsSourceProvider)
	p.mu.Unlock()
	p.notify(notifications)
	p.notifyDocument()
	return nil
}

func (p *SettingsProvider) Describe(options ...SettingsDescribeOptions) ([]SettingsDescriptor, error) {
	if p == nil {
		return nil, errors.New("harness: settings provider is nil")
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	result := make([]SettingsDescriptor, 0, len(p.order))
	for _, namespace := range p.order {
		registration := p.registrations[namespace]
		if registration == nil || registration.released {
			continue
		}
		value, err := registration.encode(registration.current)
		if err != nil {
			return nil, err
		}
		descriptor := SettingsDescriptor{
			Namespace: namespace, Schema: append(json.RawMessage(nil), registration.schema...),
			Value: append(json.RawMessage(nil), value...), Revision: registration.revision, Applies: registration.applies,
		}
		if len(registration.base) != 0 && string(registration.base) != "{}" {
			descriptor.Base = append(json.RawMessage(nil), registration.base...)
		}
		if registration.hasUser {
			descriptor.User = append(json.RawMessage(nil), registration.user...)
		}
		if len(options) > 0 && options[0].RedactSecrets {
			descriptor, err = redactSettingsDescriptor(descriptor)
			if err != nil {
				return nil, err
			}
		}
		result = append(result, descriptor)
	}
	result = append(result, p.externalDescriptorsLocked()...)
	return result, nil
}

// Owns reports whether this provider owns a local namespace rather than a mirror.
func (p *SettingsProvider) Owns(namespace string) bool {
	if p == nil {
		return false
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	registration := p.registrations[namespace]
	return !p.disposed && registration != nil && !registration.released
}

// WriteSection merges a patch or replaces the authoritative user section. It
// deliberately never reconstructs stored settings from a redacted descriptor.
func (p *SettingsProvider) WriteSection(ctx context.Context, namespace string, value any, replace bool, revision *uint64, options ...SettingsDescribeOptions) (SettingsDescriptor, error) {
	if p == nil {
		return SettingsDescriptor{}, errors.New("settings provider is unavailable")
	}
	p.mu.Lock()
	registration := p.registrations[namespace]
	p.mu.Unlock()
	if registration == nil {
		return SettingsDescriptor{}, NewProviderError("settings namespace is unavailable", InvalidSettingsCode, nil)
	}
	scope := &SettingsScope[json.RawMessage]{registration: registration}
	var err error
	if replace {
		err = scope.Replace(ctx, value, revision)
	} else {
		err = scope.Update(ctx, value, revision)
	}
	if err != nil {
		return SettingsDescriptor{}, err
	}
	descriptors, err := p.Describe(options...)
	if err != nil {
		return SettingsDescriptor{}, err
	}
	for _, descriptor := range descriptors {
		if descriptor.Namespace == namespace {
			return descriptor, nil
		}
	}
	return SettingsDescriptor{}, NewProviderError("settings namespace was disposed", SettingsDisposedCode, nil)
}

// Mutate applies ordered path edits to the authoritative stored user section
// under revision CAS. It never rebuilds a section from a caller's partial
// descriptor, so fields outside the edited paths remain intact.
func (p *SettingsProvider) Mutate(
	ctx context.Context,
	namespace string,
	ops []SettingsPathOp,
	expectedRevision *uint64,
	options ...SettingsDescribeOptions,
) (SettingsDescriptor, error) {
	if p == nil {
		return SettingsDescriptor{}, errors.New("harness: settings provider is nil")
	}
	if err := validateSettingsNamespace(namespace); err != nil {
		return SettingsDescriptor{}, err
	}
	prepared := make([]settingsPreparedPathOp, len(ops))
	for index, op := range ops {
		prepared[index].op = op.Op
		prepared[index].path = append([]string(nil), op.Path...)
		switch op.Op {
		case "set":
			if len(op.Value) == 0 {
				return SettingsDescriptor{}, NewProviderError("settings set path operation requires a value", InvalidSettingsCode, nil)
			}
			value, err := decodeSettingsJSONValue(op.Value)
			if err != nil {
				return SettingsDescriptor{}, NewProviderError("settings set path operation requires JSON-compatible data", InvalidSettingsCode, err)
			}
			prepared[index].value = value
		case "unset":
			if len(op.Value) != 0 {
				return SettingsDescriptor{}, NewProviderError("settings unset path operation must not carry a value", InvalidSettingsCode, nil)
			}
		default:
			return SettingsDescriptor{}, NewProviderError("settings path operation must be set or unset", InvalidSettingsCode, nil)
		}
	}
	if ctx == nil {
		ctx = context.Background()
	}
	p.mu.Lock()
	if p.disposed {
		p.mu.Unlock()
		return SettingsDescriptor{}, NewProviderError("settings provider was disposed", SettingsDisposedCode, nil)
	}
	registration := p.registrations[namespace]
	if registration == nil || registration.released {
		external := p.externalNamespaceOwner[namespace]
		if external != nil && !external.released {
			p.mu.Unlock()
			return p.mutateExternal(ctx, external, namespace, ops, expectedRevision)
		}
		p.mu.Unlock()
		return SettingsDescriptor{}, NewProviderError(
			fmt.Sprintf("settings namespace %q is not registered", namespace), InvalidSettingsCode, nil,
		)
	}
	if expectedRevision != nil && *expectedRevision != registration.revision {
		actual := registration.revision
		p.mu.Unlock()
		return SettingsDescriptor{}, &SettingsConflictError{Namespace: namespace, Expected: *expectedRevision, Actual: actual}
	}
	originalUser := append(json.RawMessage(nil), registration.user...)
	originalHasUser := registration.hasUser
	mutation := func(current json.RawMessage, hasCurrent bool) (json.RawMessage, error) {
		if expectedRevision != nil && !sameRawSettingsSection(originalUser, originalHasUser, current, hasCurrent) {
			return nil, &SettingsConflictError{
				Namespace: namespace, Expected: *expectedRevision, Actual: registration.revision + 1,
			}
		}
		section := map[string]any{}
		if hasCurrent {
			var err error
			section, err = decodeSettingsObject(current)
			if err != nil {
				return nil, NewProviderError(
					fmt.Sprintf("settings namespace %q has an invalid stored section", namespace), InvalidSettingsCode, err,
				)
			}
		}
		changed := false
		for _, op := range prepared {
			var opChanged bool
			var err error
			section, opChanged, err = applySettingsPathOp(section, op)
			if err != nil {
				return nil, NewProviderError(
					fmt.Sprintf("settings namespace %q rejected a path operation", namespace), InvalidSettingsCode, err,
				)
			}
			changed = changed || opChanged
		}
		if !changed {
			return nil, nil
		}
		next := encodeSettingsObject(section)
		if _, err := registration.resolve(next, true); err != nil {
			return nil, NewProviderError(
				fmt.Sprintf("settings namespace %q rejected the candidate section", namespace), InvalidSettingsCode, err,
			)
		}
		return next, nil
	}
	document := cloneSettingsDocument(p.document)
	if p.backend != nil {
		var err error
		document, err = p.backend.CommitSection(ctx, namespace, mutation)
		if err != nil {
			p.mu.Unlock()
			return SettingsDescriptor{}, err
		}
	} else {
		next, err := mutation(registration.user, registration.hasUser)
		if err != nil {
			p.mu.Unlock()
			return SettingsDescriptor{}, err
		}
		if next != nil {
			document[namespace] = append(json.RawMessage(nil), next...)
		}
	}
	notifications := p.publishLocked(document, SettingsSourceUpdate)
	descriptor, err := settingsDescriptorOf(registration)
	if err == nil && len(options) > 0 && options[0].RedactSecrets {
		descriptor, err = redactSettingsDescriptor(descriptor)
	}
	p.mu.Unlock()
	p.notify(notifications)
	p.notifyDocument()
	if err != nil {
		return SettingsDescriptor{}, err
	}
	return descriptor, nil
}

func settingsDescriptorOf(registration *settingsRegistration) (SettingsDescriptor, error) {
	value, err := registration.encode(registration.current)
	if err != nil {
		return SettingsDescriptor{}, err
	}
	descriptor := SettingsDescriptor{
		Namespace: registration.namespace, Schema: append(json.RawMessage(nil), registration.schema...),
		Value: append(json.RawMessage(nil), value...), Revision: registration.revision, Applies: registration.applies,
	}
	if len(registration.base) != 0 && string(registration.base) != "{}" {
		descriptor.Base = append(json.RawMessage(nil), registration.base...)
	}
	if registration.hasUser {
		descriptor.User = append(json.RawMessage(nil), registration.user...)
	}
	return descriptor, nil
}

func (p *SettingsProvider) Dispose(context.Context) error {
	if p == nil {
		return nil
	}
	p.mu.Lock()
	p.disposed = true
	for _, registration := range p.registrations {
		registration.released = true
		for _, watcher := range registration.watchers {
			watcher.orderMu.Lock()
			watcher.active = false
			watcher.orderCond.Broadcast()
			watcher.orderMu.Unlock()
		}
		registration.watchers = nil
	}
	for _, source := range p.externalSources {
		source.released = true
	}
	p.registrations = make(map[string]*settingsRegistration)
	p.order = nil
	p.externalSources = nil
	p.externalSourceIDs = make(map[string]*externalSettingsSource)
	p.externalNamespaceOwner = make(map[string]*externalSettingsSource)
	p.documentWatchMu.Lock()
	p.documentWatchers = nil
	p.documentWatchMu.Unlock()
	p.mu.Unlock()
	return nil
}
