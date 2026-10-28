package runtime

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"sort"
	"sync"
	"sync/atomic"
	"time"
)

type Options struct {
	CleanupTimeout time.Duration
	Logger         *slog.Logger
}

type Runtime struct {
	mu sync.RWMutex

	context context.Context
	cancel  context.CancelFunc
	root    *Scope

	cleanupTimeout time.Duration
	logger         *slog.Logger
	disposed       bool
	instances      map[uint64]*Instance
	entryIDs       map[string]uint64
	services       map[serviceAddress]*leaseRecord
	dependents     map[serviceAddress]map[uint64]*Instance

	nextInstanceID   atomic.Uint64
	nextActivationID atomic.Uint64
	nextLeaseID      atomic.Uint64
}

func New(options ...Options) *Runtime {
	configured := Options{CleanupTimeout: 5 * time.Second}
	if len(options) > 0 {
		configured = options[0]
		if configured.CleanupTimeout == 0 {
			configured.CleanupTimeout = 5 * time.Second
		}
	}
	if configured.Logger == nil {
		configured.Logger = slog.New(slog.NewTextHandler(io.Discard, nil))
	}
	ctx, cancel := context.WithCancel(context.Background())
	runtime := &Runtime{
		context:        ctx,
		cancel:         cancel,
		cleanupTimeout: configured.CleanupTimeout,
		logger:         configured.Logger,
		instances:      make(map[uint64]*Instance),
		entryIDs:       make(map[string]uint64),
		services:       make(map[serviceAddress]*leaseRecord),
		dependents:     make(map[serviceAddress]map[uint64]*Instance),
	}
	runtime.root = &Scope{runtime: runtime}
	return runtime
}

func NewRuntime(options Options) *Runtime { return New(options) }

func (r *Runtime) Root() *Scope { return r.root }

func (r *Runtime) Logger() *slog.Logger { return r.logger }

func (r *Runtime) Mount(scope *Scope, entryID string, definition Definition, config []byte) (*Instance, error) {
	if err := definition.validate(); err != nil {
		return nil, err
	}
	if entryID == "" {
		return nil, errors.New("runtime: entry ID must not be empty")
	}
	if scope == nil {
		scope = r.root
	}
	if scope.runtime != r {
		return nil, errors.New("runtime: scope belongs to another runtime")
	}

	decoded, err := definition.decode(json.RawMessage(config))
	if err != nil {
		return nil, fmt.Errorf("runtime: decode config for %q: %w", definition.descriptor.ID, err)
	}

	instance := newInstance(r, scope, entryID, definition, decoded)
	r.mu.Lock()
	if r.disposed {
		r.mu.Unlock()
		return nil, ErrClosedRuntime
	}
	if existing, ok := r.entryIDs[entryID]; ok {
		r.mu.Unlock()
		return nil, fmt.Errorf("runtime: entry ID %q is already mounted by instance %d", entryID, existing)
	}
	instance.id = r.nextInstanceID.Add(1)
	r.instances[instance.id] = instance
	r.entryIDs[entryID] = instance.id
	for _, requirement := range definition.descriptor.Requires {
		address := serviceAddress{keyID: requirement.Key.id, realm: scope.realm(requirement.Key.id)}
		set := r.dependents[address]
		if set == nil {
			set = make(map[uint64]*Instance)
			r.dependents[address] = set
		}
		set[instance.id] = instance
	}
	r.mu.Unlock()

	instance.startActor()
	return instance, nil
}

func (r *Runtime) Dispose(ctx context.Context) error {
	r.mu.Lock()
	if !r.disposed {
		r.disposed = true
		r.cancel()
	}
	instances := make([]*Instance, 0, len(r.instances))
	for _, instance := range r.instances {
		instances = append(instances, instance)
	}
	r.mu.Unlock()

	var joined error
	for _, instance := range instances {
		if err := instance.Dispose(ctx); err != nil {
			joined = errors.Join(joined, fmt.Errorf("dispose instance %q: %w", instance.entryID, err))
		}
	}
	return joined
}

func (r *Runtime) removeInstance(instance *Instance) {
	r.mu.Lock()
	delete(r.instances, instance.id)
	if r.entryIDs[instance.entryID] == instance.id {
		delete(r.entryIDs, instance.entryID)
	}
	for _, requirement := range instance.definition.descriptor.Requires {
		address := serviceAddress{keyID: requirement.Key.id, realm: instance.scope.realm(requirement.Key.id)}
		set := r.dependents[address]
		delete(set, instance.id)
		if len(set) == 0 {
			delete(r.dependents, address)
		}
	}
	r.mu.Unlock()
}

func (r *Runtime) resolveDependencies(instance *Instance) desiredEpoch {
	epoch := desiredEpoch{
		configRevision: instance.configRevision,
		ready:          true,
		providers:      make([]providerRevision, 0, len(instance.definition.descriptor.Requires)),
		dependencies:   make(map[uint64]resolvedService, len(instance.definition.descriptor.Requires)),
	}
	r.mu.RLock()
	for _, requirement := range instance.definition.descriptor.Requires {
		address := serviceAddress{keyID: requirement.Key.id, realm: instance.scope.realm(requirement.Key.id)}
		record := r.services[address]
		revision := providerRevision{keyID: requirement.Key.id}
		if record != nil {
			record.mu.RLock()
			if record.published && record.ready && !record.closed {
				revision.present = true
				revision.instanceID = record.owner.instance.id
				revision.activationID = record.owner.id
				revision.leaseID = record.id
				revision.generation = record.generation
				epoch.dependencies[requirement.Key.id] = resolvedService{
					key: requirement.Key, address: address, value: record.value, revision: revision,
				}
			}
			record.mu.RUnlock()
		}
		if !revision.present && !requirement.Optional {
			epoch.ready = false
			epoch.missing = append(epoch.missing, ServiceAddressSnapshot{Key: requirement.Key.name, Realm: address.realm})
		}
		epoch.providers = append(epoch.providers, revision)
	}
	r.mu.RUnlock()
	return epoch
}

func (r *Runtime) publish(activation *activation) error {
	activation.mu.Lock()
	records := make([]*leaseRecord, 0, len(activation.staged))
	for _, record := range activation.staged {
		records = append(records, record)
	}
	activation.mu.Unlock()

	r.mu.Lock()
	for _, record := range records {
		if existing := r.services[record.address]; existing != nil && existing != record {
			r.mu.Unlock()
			return fmt.Errorf("%w: %s realm=%d", ErrServiceConflict, record.key.name, record.address.realm)
		}
	}
	affected := make(map[uint64]*Instance)
	for _, record := range records {
		record.mu.Lock()
		if record.closed {
			record.mu.Unlock()
			r.mu.Unlock()
			return ErrClosedLease
		}
		record.published = true
		record.mu.Unlock()
		r.services[record.address] = record
		for id, dependent := range r.dependents[record.address] {
			affected[id] = dependent
		}
	}
	r.mu.Unlock()
	activation.published.Store(true)
	notifyInstances(affected)
	return nil
}

func (r *Runtime) unpublish(activation *activation) map[uint64]*Instance {
	activation.mu.Lock()
	records := make([]*leaseRecord, 0, len(activation.staged))
	for _, record := range activation.staged {
		records = append(records, record)
	}
	activation.mu.Unlock()

	r.mu.Lock()
	affected := make(map[uint64]*Instance)
	for _, record := range records {
		record.mu.Lock()
		wasPublished := record.published
		record.published = false
		record.mu.Unlock()
		if existing := r.services[record.address]; existing == record {
			delete(r.services, record.address)
		}
		if wasPublished {
			for id, dependent := range r.dependents[record.address] {
				affected[id] = dependent
			}
		}
	}
	r.mu.Unlock()
	activation.published.Store(false)
	notifyInstances(affected)
	return affected
}

func (r *Runtime) closeActivation(activation *activation) {
	activation.mu.Lock()
	activation.closed = true
	for _, record := range activation.staged {
		record.mu.Lock()
		record.closed = true
		record.ready = false
		record.mu.Unlock()
	}
	activation.mu.Unlock()
}

func (r *Runtime) waitForProviderDrain(
	ctx context.Context,
	provider *activation,
	dependents map[uint64]*Instance,
) error {
	var joined error
	for _, dependent := range dependents {
		if dependent == provider.instance {
			continue
		}
		if err := dependent.waitForProviderDrain(ctx, provider.instance.id, provider.id); err != nil {
			joined = errors.Join(joined, fmt.Errorf("drain dependent %q: %w", dependent.entryID, err))
		}
	}
	return joined
}

func (r *Runtime) updateLease(record *leaseRecord, value *any, ready *bool, expectedGeneration ...uint64) error {
	if record.owner == nil || record.owner.run == nil || !record.owner.run.Active() {
		return ErrNotOwner
	}
	if value != nil && ready == nil && record.check != nil {
		checked := checkServiceReady(record.check, *value)
		ready = &checked
	}
	r.mu.Lock()
	record.mu.Lock()
	if !record.owner.run.Active() {
		record.mu.Unlock()
		r.mu.Unlock()
		return ErrNotOwner
	}
	if record.closed {
		record.mu.Unlock()
		r.mu.Unlock()
		return ErrClosedLease
	}
	if len(expectedGeneration) > 0 && record.generation != expectedGeneration[0] {
		record.mu.Unlock()
		r.mu.Unlock()
		return errLeaseRevisionChanged
	}
	changed := false
	if value != nil {
		record.value = *value
		changed = true
	}
	if ready != nil && record.ready != *ready {
		record.ready = *ready
		changed = true
	}
	if changed {
		record.generation++
	}
	published := record.published
	record.mu.Unlock()
	affected := make(map[uint64]*Instance)
	if changed && published {
		for id, dependent := range r.dependents[record.address] {
			affected[id] = dependent
		}
	}
	r.mu.Unlock()
	notifyInstances(affected)
	return nil
}

func notifyInstances(instances map[uint64]*Instance) {
	for _, instance := range instances {
		instance.notifyDependencyChanged()
	}
}

type RuntimeSnapshot struct {
	Instances       int
	Services        int
	Disposed        bool
	InstanceDetails []InstanceSnapshot
	ServiceDetails  []ServiceSnapshot
}

type ServiceSnapshot struct {
	Key          string
	Realm        RealmID
	InstanceID   uint64
	ActivationID uint64
	LeaseID      uint64
	Generation   uint64
	Ready        bool
}

func (r *Runtime) Snapshot() RuntimeSnapshot {
	r.mu.RLock()
	snapshot := RuntimeSnapshot{
		Instances: len(r.instances), Services: len(r.services), Disposed: r.disposed,
		InstanceDetails: make([]InstanceSnapshot, 0, len(r.instances)),
		ServiceDetails:  make([]ServiceSnapshot, 0, len(r.services)),
	}
	for _, instance := range r.instances {
		snapshot.InstanceDetails = append(snapshot.InstanceDetails, instance.Snapshot())
	}
	for address, record := range r.services {
		record.mu.RLock()
		snapshot.ServiceDetails = append(snapshot.ServiceDetails, ServiceSnapshot{
			Key: record.key.name, Realm: address.realm,
			InstanceID: record.owner.instance.id, ActivationID: record.owner.id,
			LeaseID: record.id, Generation: record.generation, Ready: record.ready && !record.closed,
		})
		record.mu.RUnlock()
	}
	r.mu.RUnlock()
	sort.Slice(snapshot.InstanceDetails, func(a, b int) bool {
		return snapshot.InstanceDetails[a].ID < snapshot.InstanceDetails[b].ID
	})
	sort.Slice(snapshot.ServiceDetails, func(a, b int) bool {
		left, right := snapshot.ServiceDetails[a], snapshot.ServiceDetails[b]
		if left.Key != right.Key {
			return left.Key < right.Key
		}
		return left.Realm < right.Realm
	})
	return snapshot
}
