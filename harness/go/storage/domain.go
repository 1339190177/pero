package storage

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"sort"
	"sync"
)

type Validator func(json.RawMessage) error

type TableSpec struct{ Validate Validator }

type GlobalSpec struct {
	Validate Validator
	Initial  json.RawMessage
}

type InvalidRecordsPolicy string

const BackupAndSkip InvalidRecordsPolicy = "backup-and-skip"

type DomainSpec struct {
	Name               string
	Version            uint64
	CompatibleVersions []uint64
	// Empty retains the default: a schema-invalid record prevents opening.
	InvalidRecords InvalidRecordsPolicy
	Layout         Layout
	Global         *GlobalSpec
	Tables         map[string]TableSpec
}

func (s DomainSpec) Validate() error {
	if !unitNamePattern.MatchString(s.Name) {
		return fmt.Errorf("storage: domain name %q must match %s", s.Name, unitNamePattern)
	}
	if s.Layout != "" && s.Layout != LayoutSingle && s.Layout != LayoutPerRecord {
		return fmt.Errorf("storage: domain %q layout must be single or per-record", s.Name)
	}
	for _, version := range s.CompatibleVersions {
		if version >= s.Version {
			return fmt.Errorf("storage: domain %q compatible version %d must be below %d", s.Name, version, s.Version)
		}
	}
	if s.InvalidRecords != "" && s.InvalidRecords != BackupAndSkip {
		return fmt.Errorf("storage: domain %q invalid records policy %q", s.Name, s.InvalidRecords)
	}
	for table := range s.Tables {
		if !unitNamePattern.MatchString(table) {
			return fmt.Errorf("storage: domain %q table name %q must match %s", s.Name, table, unitNamePattern)
		}
		if s.Tables[table].Validate == nil {
			return fmt.Errorf("storage: domain %q table %q requires a validator", s.Name, table)
		}
	}
	if s.Global != nil {
		initial, err := detachJSON(s.Global.Initial)
		if err != nil || isJSONNull(initial) {
			return fmt.Errorf("storage: domain %q global initial must be valid non-null JSON", s.Name)
		}
		if s.Global.Validate == nil {
			return fmt.Errorf("storage: domain %q global requires a validator", s.Name)
		}
		if err := s.Global.Validate(json.RawMessage(`null`)); err == nil {
			return fmt.Errorf("storage: domain %q global validator must reject null", s.Name)
		}
		if err := s.Global.Validate(initial); err != nil {
			return fmt.Errorf("storage: domain %q global initial is invalid: %w", s.Name, err)
		}
	}
	return nil
}

func (s DomainSpec) descriptor() KVUnitDescriptor {
	tables := make([]string, 0, len(s.Tables))
	for table := range s.Tables {
		tables = append(tables, table)
	}
	sort.Strings(tables)
	layout := s.Layout
	if layout == "" {
		layout = LayoutSingle
	}
	return KVUnitDescriptor{Name: s.Name, Version: s.Version, CompatibleVersions: slices.Clone(s.CompatibleVersions), Tables: tables, HasGlobal: s.Global != nil, Layout: layout}
}

type DomainConfig struct {
	Backend string
	Routes  map[string]string
}

type DomainChanged struct {
	Domain    string
	Table     string
	Key       string
	Operation string
	Value     json.RawMessage
}

type domainListener struct {
	id uint64
	fn func(DomainChanged)
}

type DomainFacility struct {
	registry *BackendRegistry
	config   DomainConfig
	logger   *slog.Logger

	mu             sync.Mutex
	domains        map[string]*Domain
	reserved       map[string]bool
	listeners      map[uint64]func(DomainChanged)
	nextListenerID uint64
	closed         bool
}

func NewDomainFacility(registry *BackendRegistry, config DomainConfig, logger *slog.Logger) (*DomainFacility, error) {
	if registry == nil {
		return nil, errors.New("storage: domain facility requires a backend registry")
	}
	if config.Backend == "" {
		return nil, errors.New("storage: domain facility requires a default backend")
	}
	if logger == nil {
		logger = slog.Default()
	}
	return &DomainFacility{registry: registry, config: config, logger: logger, domains: make(map[string]*Domain), reserved: make(map[string]bool), listeners: make(map[uint64]func(DomainChanged))}, nil
}

func (f *DomainFacility) Open(ctx context.Context, spec DomainSpec) (*Domain, error) {
	if err := spec.Validate(); err != nil {
		return nil, err
	}
	f.mu.Lock()
	if f.closed {
		f.mu.Unlock()
		return nil, &Error{Code: Closed, Message: "storage domain facility is closed"}
	}
	if f.reserved[spec.Name] {
		f.mu.Unlock()
		return nil, &Error{Code: AlreadyOpen, Domain: spec.Name, Message: fmt.Sprintf("domain %q is already open", spec.Name)}
	}
	f.reserved[spec.Name] = true
	f.mu.Unlock()
	releaseReservation := true
	defer func() {
		if releaseReservation {
			f.mu.Lock()
			delete(f.reserved, spec.Name)
			f.mu.Unlock()
		}
	}()
	backendName := f.config.Backend
	if routed := f.config.Routes[spec.Name]; routed != "" {
		backendName = routed
	}
	backend, err := f.registry.Get(backendName)
	if err != nil {
		return nil, err
	}
	facet := backend.KV()
	if facet == nil {
		return nil, &Error{Code: FacetUnsupported, Domain: spec.Name, Message: fmt.Sprintf("backend %q routed for domain %q has no kv facet", backendName, spec.Name)}
	}
	unit, err := facet.Open(ctx, spec.descriptor())
	if err != nil {
		return nil, err
	}
	snapshot, err := unit.LoadAll(ctx)
	if err != nil {
		_ = unit.Close(context.Background())
		return nil, err
	}
	domain, err := newDomain(ctx, f, spec, unit, snapshot)
	if err != nil {
		_ = unit.Close(context.Background())
		return nil, err
	}
	f.mu.Lock()
	if f.closed {
		f.mu.Unlock()
		_ = unit.Close(context.Background())
		return nil, &Error{Code: Closed, Message: "storage domain facility closed during open"}
	}
	f.domains[spec.Name] = domain
	f.mu.Unlock()
	releaseReservation = false
	return domain, nil
}

func (f *DomainFacility) Get(name string) (*Domain, bool) {
	if f == nil {
		return nil, false
	}
	f.mu.Lock()
	domain := f.domains[name]
	f.mu.Unlock()
	return domain, domain != nil
}

func (f *DomainFacility) Subscribe(listener func(DomainChanged)) func() {
	if f == nil || listener == nil {
		return func() {}
	}
	f.mu.Lock()
	f.nextListenerID++
	id := f.nextListenerID
	f.listeners[id] = listener
	f.mu.Unlock()
	var once sync.Once
	return func() { once.Do(func() { f.mu.Lock(); delete(f.listeners, id); f.mu.Unlock() }) }
}

func (f *DomainFacility) emit(change DomainChanged) {
	f.mu.Lock()
	ids := make([]uint64, 0, len(f.listeners))
	for id := range f.listeners {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	listeners := make([]domainListener, 0, len(ids))
	for _, id := range ids {
		listeners = append(listeners, domainListener{id: id, fn: f.listeners[id]})
	}
	f.mu.Unlock()
	for _, listener := range listeners {
		func() {
			defer func() {
				if recovered := recover(); recovered != nil {
					f.logger.Warn("storage domain listener panicked after durable change", "domain", change.Domain, "table", change.Table, "key", change.Key, "panic", recovered)
				}
			}()
			copy := change
			copy.Value = append(json.RawMessage(nil), change.Value...)
			listener.fn(copy)
		}()
	}
}

func (f *DomainFacility) release(name string, domain *Domain) {
	f.mu.Lock()
	if f.domains[name] == domain {
		delete(f.domains, name)
	}
	delete(f.reserved, name)
	f.mu.Unlock()
}

func (f *DomainFacility) Close(ctx context.Context) error {
	if f == nil {
		return nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	f.mu.Lock()
	if f.closed {
		f.mu.Unlock()
		return nil
	}
	f.closed = true
	domains := make([]*Domain, 0, len(f.domains))
	for _, domain := range f.domains {
		domains = append(domains, domain)
	}
	f.mu.Unlock()
	sort.Slice(domains, func(i, j int) bool { return domains[i].Name() < domains[j].Name() })
	var result error
	for _, domain := range domains {
		result = errors.Join(result, domain.Close(ctx))
	}
	return result
}

type Domain struct {
	facility *DomainFacility
	spec     DomainSpec
	unit     KVUnit

	stateMu      sync.RWMutex
	tables       map[string]map[string]json.RawMessage
	tableOrder   map[string][]string
	tableHandles map[string]*Table
	global       json.RawMessage
	globalHandle *Global
	disposing    bool
	closed       bool
	writes       sync.WaitGroup
	writeMu      sync.Mutex
	closeOnce    sync.Once
	closeDone    chan struct{}
	closeErr     error
}

func newDomain(ctx context.Context, facility *DomainFacility, spec DomainSpec, unit KVUnit, snapshot KVSnapshot) (*Domain, error) {
	d := &Domain{tableOrder: make(map[string][]string), facility: facility, spec: spec, unit: unit, tables: make(map[string]map[string]json.RawMessage), tableHandles: make(map[string]*Table), closeDone: make(chan struct{})}
	for table, tableSpec := range spec.Tables {
		records := make(map[string]json.RawMessage)
		for key, raw := range snapshot.Tables[table] {
			value, err := detachJSON(raw)
			if err == nil && tableSpec.Validate != nil {
				err = tableSpec.Validate(value)
			}
			if err != nil {
				invalid := invalidRecordError(spec.Name, table, key, err)
				backupper, supported := unit.(KVRecordBackupper)
				if spec.InvalidRecords != BackupAndSkip || !supported {
					return nil, invalid
				}
				moved, backupErr := backupper.BackupRecord(ctx, table, key)
				if backupErr != nil {
					return nil, errors.Join(invalid, fmt.Errorf("backup invalid record: %w", backupErr))
				}
				facility.logger.Error("invalid storage record backed up and treated as absent", "domain", spec.Name, "table", table, "key", key, "backup", moved, "cause", err)
				continue
			}
			records[key] = value
		}
		d.tables[table] = records
		d.tableOrder[table] = normalizedRecordOrder(records, snapshot.TableOrder[table])
		d.tableHandles[table] = &Table{domain: d, name: table}
	}
	if spec.Global != nil {
		d.globalHandle = &Global{domain: d}
		if len(snapshot.Global) == 0 || isJSONNull(snapshot.Global) {
			d.global, _ = detachJSON(spec.Global.Initial)
		} else {
			value, err := detachJSON(snapshot.Global)
			if err != nil {
				return nil, invalidRecordError(spec.Name, "", "", err)
			}
			if err := spec.Global.Validate(value); err != nil {
				return nil, invalidRecordError(spec.Name, "", "", err)
			}
			d.global = value
		}
	}
	return d, nil
}

func invalidRecordError(domain, table, key string, cause error) error {
	slot := "global"
	if table != "" {
		slot = fmt.Sprintf("record %q in table %q", key, table)
	}
	return &Error{Code: InvalidRecord, Domain: domain, Table: table, Key: key, Cause: cause, Message: fmt.Sprintf("domain %q: stored %s does not match its schema", domain, slot)}
}

func (d *Domain) Name() string {
	if d == nil {
		return ""
	}
	return d.spec.Name
}

func (d *Domain) Table(name string) (*Table, error) {
	if d == nil {
		return nil, errors.New("storage: domain is nil")
	}
	d.stateMu.RLock()
	_, exists := d.tables[name]
	closed := d.closed
	d.stateMu.RUnlock()
	if closed {
		return nil, &Error{Code: Closed, Domain: d.spec.Name, Message: fmt.Sprintf("domain %q is closed", d.spec.Name)}
	}
	if !exists {
		return nil, fmt.Errorf("storage: domain %q declares no table %q", d.spec.Name, name)
	}
	return d.tableHandles[name], nil
}

func (d *Domain) Global() (*Global, error) {
	if d == nil || d.spec.Global == nil {
		return nil, fmt.Errorf("storage: domain %q declares no global", d.Name())
	}
	d.stateMu.RLock()
	closed := d.closed
	d.stateMu.RUnlock()
	if closed {
		return nil, &Error{Code: Closed, Domain: d.spec.Name, Message: fmt.Sprintf("domain %q is closed", d.spec.Name)}
	}
	return d.globalHandle, nil
}

func (d *Domain) beginWrite() error {
	d.stateMu.Lock()
	if d.disposing || d.closed {
		d.stateMu.Unlock()
		return &Error{Code: Closed, Domain: d.spec.Name, Message: fmt.Sprintf("domain %q is closed", d.spec.Name)}
	}
	d.writes.Add(1)
	d.stateMu.Unlock()
	return nil
}

func (d *Domain) runWrite(job func() error) error {
	if err := d.beginWrite(); err != nil {
		return err
	}
	defer d.writes.Done()
	d.writeMu.Lock()
	defer d.writeMu.Unlock()
	return job()
}

func (d *Domain) Close(ctx context.Context) error {
	if d == nil {
		return nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	d.closeOnce.Do(func() {
		d.stateMu.Lock()
		d.disposing = true
		d.stateMu.Unlock()
		go d.runClose()
	})
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-d.closeDone:
		return d.closeErr
	}
}

func (d *Domain) runClose() {
	d.writes.Wait()
	err := d.unit.Close(context.Background())
	if err == nil {
		d.stateMu.Lock()
		d.closed = true
		d.stateMu.Unlock()
		d.facility.release(d.spec.Name, d)
	}
	d.closeErr = err
	close(d.closeDone)
}
