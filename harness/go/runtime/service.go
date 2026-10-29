package runtime

import (
	"errors"
	"fmt"
	"sync"
)

var errLeaseRevisionChanged = errors.New("runtime: service lease revision changed")

type serviceAddress struct {
	keyID uint64
	realm RealmID
}

type providerRevision struct {
	keyID        uint64
	present      bool
	instanceID   uint64
	activationID uint64
	leaseID      uint64
	generation   uint64
}

type resolvedService struct {
	key      AnyKey
	address  serviceAddress
	value    any
	revision providerRevision
}

type leaseRecord struct {
	mu sync.RWMutex

	id      uint64
	key     AnyKey
	address serviceAddress
	owner   *activation
	value   any
	check   func(any) bool

	generation uint64
	ready      bool
	published  bool
	closed     bool
}

type Lease[T any] struct {
	record *leaseRecord
}

func (l *Lease[T]) SetReady(ready bool) error {
	if l == nil || l.record == nil {
		return ErrClosedLease
	}
	return l.record.owner.instance.runtime.updateLease(l.record, nil, &ready)
}

func (l *Lease[T]) Replace(value T) error {
	if l == nil || l.record == nil {
		return ErrClosedLease
	}
	wrapped := any(value)
	return l.record.owner.instance.runtime.updateLease(l.record, &wrapped, nil)
}

// Refresh re-evaluates a ProvideChecked readiness predicate and notifies
// dependents when availability changes.
func (l *Lease[T]) Refresh() error {
	if l == nil || l.record == nil {
		return ErrClosedLease
	}
	record := l.record
	for {
		record.mu.RLock()
		value, check, closed, generation := record.value, record.check, record.closed, record.generation
		record.mu.RUnlock()
		if closed {
			return ErrClosedLease
		}
		if check == nil {
			return nil
		}
		ready := checkServiceReady(check, value)
		err := record.owner.instance.runtime.updateLease(record, nil, &ready, generation)
		if errors.Is(err, errLeaseRevisionChanged) {
			continue
		}
		return err
	}
}

func Provide[T any](run *Run, key Key[T], value T) (*Lease[T], error) {
	return provide(run, key, value, nil)
}

func ProvideChecked[T any](run *Run, key Key[T], value T, check func(T) bool) (*Lease[T], error) {
	var erased func(any) bool
	if check != nil {
		erased = func(value any) bool {
			typed, ok := value.(T)
			return ok && check(typed)
		}
	}
	return provide(run, key, value, erased)
}

func provide[T any](run *Run, key Key[T], value T, check func(any) bool) (*Lease[T], error) {
	if run == nil || !run.Active() || run.activation == nil {
		return nil, ErrInactiveRun
	}
	if key.id == 0 {
		return nil, ErrInvalidKey
	}
	if !run.Instance.provides(key.id) {
		return nil, fmt.Errorf("%w: definition %q does not provide %q", ErrUndeclaredService, run.Instance.definition.descriptor.ID, key.name)
	}

	activation := run.activation
	address := serviceAddress{keyID: key.id, realm: run.Scope.realm(key.id)}
	record := &leaseRecord{
		id:         run.Instance.runtime.nextLeaseID.Add(1),
		key:        key.Any(),
		address:    address,
		owner:      activation,
		value:      value,
		check:      check,
		generation: 1,
		ready:      checkServiceReady(check, value),
	}

	activation.mu.Lock()
	defer activation.mu.Unlock()
	if !run.Active() || activation.closed {
		return nil, ErrInactiveRun
	}
	if _, exists := activation.staged[address]; exists {
		return nil, fmt.Errorf("%w: %s realm=%d", ErrDuplicateProvide, key.name, address.realm)
	}
	activation.staged[address] = record
	return &Lease[T]{record: record}, nil
}

func checkServiceReady(check func(any) bool, value any) (ready bool) {
	if check == nil {
		return true
	}
	defer func() {
		if recover() != nil {
			ready = false
		}
	}()
	return check(value)
}

func Resolve[T any](run *Run, key Key[T]) (T, error) {
	var zero T
	if run == nil || !run.readableDuringCleanup() || run.activation == nil {
		return zero, ErrInactiveRun
	}
	if key.id == 0 {
		return zero, ErrInvalidKey
	}
	if !run.Instance.requires(key.id) && !run.Instance.provides(key.id) {
		return zero, fmt.Errorf("%w: definition %q does not declare %q", ErrUndeclaredService, run.Instance.definition.descriptor.ID, key.name)
	}

	address := serviceAddress{keyID: key.id, realm: run.Scope.realm(key.id)}
	activation := run.activation
	if run.Instance.provides(key.id) {
		activation.mu.Lock()
		if record, ok := activation.staged[address]; ok {
			record.mu.RLock()
			value, ready, closed := record.value, record.ready, record.closed
			record.mu.RUnlock()
			activation.mu.Unlock()
			if !ready || closed {
				return zero, fmt.Errorf("%w: %s realm=%d", ErrServiceUnavailable, key.name, address.realm)
			}
			typed, ok := value.(T)
			if !ok {
				return zero, fmt.Errorf("runtime: service %q has value type %T", key.name, value)
			}
			return typed, nil
		}
		activation.mu.Unlock()
	}

	service, ok := activation.dependencies[key.id]
	if !ok {
		return zero, fmt.Errorf("%w: %s realm=%d", ErrServiceUnavailable, key.name, address.realm)
	}
	typed, ok := service.value.(T)
	if !ok {
		return zero, fmt.Errorf("runtime: service %q has value type %T", key.name, service.value)
	}
	return typed, nil
}

func Optional[T any](run *Run, key Key[T]) (T, bool) {
	value, err := Resolve(run, key)
	return value, err == nil
}

// Lookup performs an administrative lookup without a plugin declaration check.
func Lookup[T any](runtime *Runtime, scope *Scope, key Key[T]) (T, bool) {
	var zero T
	if runtime == nil || key.id == 0 {
		return zero, false
	}
	if scope == nil {
		scope = runtime.Root()
	}
	address := serviceAddress{keyID: key.id, realm: scope.realm(key.id)}
	runtime.mu.RLock()
	record := runtime.services[address]
	if record == nil {
		runtime.mu.RUnlock()
		return zero, false
	}
	record.mu.RLock()
	value, ready, closed := record.value, record.ready, record.closed
	record.mu.RUnlock()
	runtime.mu.RUnlock()
	if !ready || closed {
		return zero, false
	}
	typed, ok := value.(T)
	return typed, ok
}
