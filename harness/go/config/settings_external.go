package config

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"strings"

	point "github.com/phodal/pero/harness/go/runtime"
)

const (
	MaxExternalSettingsNamespaces    = 256
	MaxExternalSettingsSnapshotBytes = 8 << 20
	maxSettingsJSONSafeRevision      = 1<<53 - 1
)

// ExternalSettingsMutator applies one ordered path mutation at the external
// authority and returns its already-redacted committed descriptor. It must not
// return secret values: RegisterExternalSettingsSource independently checks
// the result against its schema before publication.
type ExternalSettingsMutator func(
	context.Context,
	string,
	[]SettingsPathOp,
	*uint64,
) (SettingsDescriptor, error)

// ExternalSettingsSnapshot is one complete, already-redacted view from an
// external settings authority. Publishing a snapshot atomically adds, updates,
// and removes that source's namespaces without copying them into the local
// SettingsBackend.
type ExternalSettingsSnapshot struct {
	Writable   bool
	Namespaces []SettingsDescriptor
}

// ExternalSettingsSource describes one effect-owned external authority. ID is
// process-local identity used to reject accidental duplicate mounts.
type ExternalSettingsSource struct {
	ID       string
	Snapshot ExternalSettingsSnapshot
	Mutate   ExternalSettingsMutator
}

// ExternalSettingsMirror accepts later authoritative snapshots. Its lifetime
// belongs to the Point Run passed to RegisterExternalSettingsSource.
type ExternalSettingsMirror struct {
	provider *SettingsProvider
	source   *externalSettingsSource
}

type externalSettingsSource struct {
	name       string
	writable   bool
	namespaces []SettingsDescriptor
	byName     map[string]int
	mutate     ExternalSettingsMutator
	released   bool
}

// RegisterExternalSettingsSource projects a redacted external namespace set
// into this provider. Local registrations retain precedence for the same
// namespace, so a Host-native settings owner is never replaced by a remote
// shadow. Two external sources may not claim the same namespace.
func RegisterExternalSettingsSource(
	provider *SettingsProvider,
	run *point.Run,
	source ExternalSettingsSource,
) (*ExternalSettingsMirror, error) {
	if provider == nil || run == nil || !run.Active() {
		return nil, point.ErrInactiveRun
	}
	source.ID = strings.TrimSpace(source.ID)
	if source.ID == "" || len(source.ID) > 512 {
		return nil, NewProviderError("external settings source ID must contain 1 to 512 bytes", InvalidSettingsCode, nil)
	}
	namespaces, err := validateExternalSettingsSnapshot(source.Snapshot)
	if err != nil {
		return nil, err
	}
	if source.Snapshot.Writable && source.Mutate == nil {
		return nil, NewProviderError("writable external settings source requires a mutator", InvalidSettingsCode, nil)
	}
	provider.mu.Lock()
	if provider.disposed {
		provider.mu.Unlock()
		return nil, NewProviderError("settings provider was disposed", SettingsDisposedCode, nil)
	}
	if provider.externalSourceIDs[source.ID] != nil {
		provider.mu.Unlock()
		return nil, NewProviderError("external settings source is already registered", DuplicateSettingsCode, nil)
	}
	for _, descriptor := range namespaces {
		if owner := provider.externalNamespaceOwner[descriptor.Namespace]; owner != nil && !owner.released {
			provider.mu.Unlock()
			return nil, NewProviderError(
				fmt.Sprintf("settings namespace %q is already owned by another external source", descriptor.Namespace),
				DuplicateSettingsCode, nil,
			)
		}
	}
	registered := &externalSettingsSource{
		name: source.ID, writable: source.Snapshot.Writable,
		namespaces: namespaces, byName: indexExternalSettingsNamespaces(namespaces), mutate: source.Mutate,
	}
	provider.externalSources = append(provider.externalSources, registered)
	provider.externalSourceIDs[source.ID] = registered
	for _, descriptor := range namespaces {
		provider.externalNamespaceOwner[descriptor.Namespace] = registered
	}
	provider.mu.Unlock()

	mirror := &ExternalSettingsMirror{provider: provider, source: registered}
	if err := run.Defer("settings:external:"+source.ID, mirror.dispose); err != nil {
		_ = mirror.dispose(context.Background())
		return nil, err
	}
	provider.notifyDocument()
	return mirror, nil
}

// Publish replaces this source's complete redacted namespace snapshot.
func (m *ExternalSettingsMirror) Publish(snapshot ExternalSettingsSnapshot) error {
	if m == nil || m.provider == nil || m.source == nil {
		return NewProviderError("external settings mirror is unavailable", SettingsDisposedCode, nil)
	}
	namespaces, err := validateExternalSettingsSnapshot(snapshot)
	if err != nil {
		return err
	}
	p := m.provider
	p.mu.Lock()
	changed := false
	defer func() {
		p.mu.Unlock()
		if changed {
			p.notifyDocument()
		}
	}()
	if p.disposed || m.source.released || p.externalSourceIDs[m.source.name] != m.source {
		return NewProviderError("external settings mirror was disposed", SettingsDisposedCode, nil)
	}
	if snapshot.Writable && m.source.mutate == nil {
		return NewProviderError("writable external settings source requires a mutator", InvalidSettingsCode, nil)
	}
	for _, descriptor := range namespaces {
		if owner := p.externalNamespaceOwner[descriptor.Namespace]; owner != nil && owner != m.source && !owner.released {
			return NewProviderError(
				fmt.Sprintf("settings namespace %q is already owned by another external source", descriptor.Namespace),
				DuplicateSettingsCode, nil,
			)
		}
	}
	for _, descriptor := range m.source.namespaces {
		if p.externalNamespaceOwner[descriptor.Namespace] == m.source {
			delete(p.externalNamespaceOwner, descriptor.Namespace)
		}
	}
	m.source.writable = snapshot.Writable
	m.source.namespaces = namespaces
	m.source.byName = indexExternalSettingsNamespaces(namespaces)
	for _, descriptor := range namespaces {
		p.externalNamespaceOwner[descriptor.Namespace] = m.source
	}
	changed = true
	return nil
}

func (m *ExternalSettingsMirror) dispose(context.Context) error {
	if m == nil || m.provider == nil || m.source == nil {
		return nil
	}
	p := m.provider
	p.mu.Lock()
	if m.source.released {
		p.mu.Unlock()
		return nil
	}
	m.source.released = true
	delete(p.externalSourceIDs, m.source.name)
	for _, descriptor := range m.source.namespaces {
		if p.externalNamespaceOwner[descriptor.Namespace] == m.source {
			delete(p.externalNamespaceOwner, descriptor.Namespace)
		}
	}
	for index, candidate := range p.externalSources {
		if candidate == m.source {
			p.externalSources = append(p.externalSources[:index], p.externalSources[index+1:]...)
			break
		}
	}
	p.mu.Unlock()
	p.notifyDocument()
	return nil
}

func (p *SettingsProvider) externalDescriptorsLocked() []SettingsDescriptor {
	result := make([]SettingsDescriptor, 0)
	for _, source := range p.externalSources {
		if source == nil || source.released {
			continue
		}
		for _, descriptor := range source.namespaces {
			if local := p.registrations[descriptor.Namespace]; local != nil && !local.released {
				continue
			}
			result = append(result, cloneExternalSettingsDescriptor(descriptor))
		}
	}
	return result
}

func (p *SettingsProvider) mutateExternal(
	ctx context.Context,
	source *externalSettingsSource,
	namespace string,
	ops []SettingsPathOp,
	expectedRevision *uint64,
) (SettingsDescriptor, error) {
	p.mu.Lock()
	if p.disposed || source.released || p.externalNamespaceOwner[namespace] != source {
		p.mu.Unlock()
		return SettingsDescriptor{}, NewProviderError("external settings source was disposed before mutation", SettingsDisposedCode, nil)
	}
	writable := source.writable
	mutate := source.mutate
	p.mu.Unlock()
	if !writable {
		return SettingsDescriptor{}, NewProviderError(
			fmt.Sprintf("external settings namespace %q is read-only", namespace), InvalidSettingsCode, nil,
		)
	}
	if mutate == nil {
		return SettingsDescriptor{}, NewProviderError("external settings mutator is unavailable", SettingsDisposedCode, nil)
	}
	detachedOps := make([]SettingsPathOp, len(ops))
	for index, op := range ops {
		detachedOps[index] = SettingsPathOp{Op: op.Op, Path: append([]string(nil), op.Path...), Value: append(json.RawMessage(nil), op.Value...)}
	}
	var revision *uint64
	if expectedRevision != nil {
		copy := *expectedRevision
		revision = &copy
	}
	committed, err := mutate(ctx, namespace, detachedOps, revision)
	if err != nil {
		return SettingsDescriptor{}, err
	}
	validated, err := validateExternalSettingsDescriptor(committed)
	if err != nil {
		return SettingsDescriptor{}, err
	}
	if validated.Namespace != namespace {
		return SettingsDescriptor{}, NewProviderError("external settings mutator changed the namespace", InvalidSettingsCode, nil)
	}
	p.mu.Lock()
	if p.disposed || source.released || p.externalNamespaceOwner[namespace] != source {
		p.mu.Unlock()
		return SettingsDescriptor{}, NewProviderError("external settings source was disposed during mutation", SettingsDisposedCode, nil)
	}
	index, found := source.byName[namespace]
	if !found {
		p.mu.Unlock()
		return SettingsDescriptor{}, NewProviderError("external settings namespace disappeared during mutation", SettingsDisposedCode, nil)
	}
	changed := validated.Revision >= source.namespaces[index].Revision
	if changed {
		source.namespaces[index] = validated
	}
	result := cloneExternalSettingsDescriptor(source.namespaces[index])
	p.mu.Unlock()
	if changed {
		p.notifyDocument()
	}
	return result, nil
}

func validateExternalSettingsSnapshot(snapshot ExternalSettingsSnapshot) ([]SettingsDescriptor, error) {
	if len(snapshot.Namespaces) > MaxExternalSettingsNamespaces {
		return nil, NewProviderError(
			fmt.Sprintf("external settings snapshot exceeds %d namespaces", MaxExternalSettingsNamespaces), InvalidSettingsCode, nil,
		)
	}
	result := make([]SettingsDescriptor, len(snapshot.Namespaces))
	seen := make(map[string]struct{}, len(snapshot.Namespaces))
	total := 0
	for index, descriptor := range snapshot.Namespaces {
		validated, err := validateExternalSettingsDescriptor(descriptor)
		if err != nil {
			return nil, err
		}
		if _, duplicate := seen[validated.Namespace]; duplicate {
			return nil, NewProviderError(
				fmt.Sprintf("external settings snapshot repeats namespace %q", validated.Namespace), DuplicateSettingsCode, nil,
			)
		}
		seen[validated.Namespace] = struct{}{}
		total += len(validated.Namespace) + len(validated.Schema) + len(validated.Value) + len(validated.Base) + len(validated.User)
		for _, secret := range validated.Secrets {
			total += len(secret.Path) * 16
			for _, part := range secret.Path {
				total += len(part)
			}
		}
		if total > MaxExternalSettingsSnapshotBytes {
			return nil, NewProviderError("external settings snapshot exceeds its byte limit", InvalidSettingsCode, nil)
		}
		result[index] = validated
	}
	return result, nil
}

func validateExternalSettingsDescriptor(descriptor SettingsDescriptor) (SettingsDescriptor, error) {
	if err := validateSettingsNamespace(descriptor.Namespace); err != nil {
		return SettingsDescriptor{}, err
	}
	if len(bytes.TrimSpace(descriptor.Schema)) == 0 || !json.Valid(descriptor.Schema) {
		return SettingsDescriptor{}, NewProviderError("external settings descriptor requires a valid schema", InvalidSettingsCode, nil)
	}
	if len(bytes.TrimSpace(descriptor.Value)) == 0 || !json.Valid(descriptor.Value) {
		return SettingsDescriptor{}, NewProviderError("external settings descriptor requires a valid value", InvalidSettingsCode, nil)
	}
	for label, raw := range map[string]json.RawMessage{"base": descriptor.Base, "user": descriptor.User} {
		if len(raw) != 0 && !json.Valid(raw) {
			return SettingsDescriptor{}, NewProviderError("external settings descriptor has invalid "+label, InvalidSettingsCode, nil)
		}
	}
	if descriptor.Applies != SettingsLive && descriptor.Applies != SettingsRestart {
		return SettingsDescriptor{}, NewProviderError("external settings descriptor applies must be live or restart", InvalidSettingsCode, nil)
	}
	if descriptor.Revision > maxSettingsJSONSafeRevision {
		return SettingsDescriptor{}, NewProviderError("external settings revision exceeds the JSON safe-integer range", InvalidSettingsCode, nil)
	}
	if descriptor.Secrets == nil {
		return SettingsDescriptor{}, NewProviderError("external settings descriptor is not proven redacted", InvalidSettingsCode, nil)
	}
	redacted, err := redactSettingsDescriptor(cloneExternalSettingsDescriptor(descriptor))
	if err != nil {
		return SettingsDescriptor{}, err
	}
	for _, pair := range [][2]json.RawMessage{{descriptor.Value, redacted.Value}, {descriptor.Base, redacted.Base}, {descriptor.User, redacted.User}} {
		if !equalOptionalJSON(pair[0], pair[1]) {
			return SettingsDescriptor{}, NewProviderError("external settings descriptor contains a schema-declared secret", InvalidSettingsCode, nil)
		}
	}
	return cloneExternalSettingsDescriptor(descriptor), nil
}

func equalOptionalJSON(left, right json.RawMessage) bool {
	if len(left) == 0 || len(right) == 0 {
		return len(left) == len(right)
	}
	var leftValue, rightValue any
	leftDecoder := json.NewDecoder(bytes.NewReader(left))
	leftDecoder.UseNumber()
	rightDecoder := json.NewDecoder(bytes.NewReader(right))
	rightDecoder.UseNumber()
	if leftDecoder.Decode(&leftValue) != nil || rightDecoder.Decode(&rightValue) != nil {
		return false
	}
	return reflect.DeepEqual(leftValue, rightValue)
}

func cloneExternalSettingsDescriptor(descriptor SettingsDescriptor) SettingsDescriptor {
	if descriptor.AutoGenerate != nil {
		value := *descriptor.AutoGenerate
		descriptor.AutoGenerate = &value
	}
	descriptor.Schema = append(json.RawMessage(nil), descriptor.Schema...)
	descriptor.Value = append(json.RawMessage(nil), descriptor.Value...)
	descriptor.Base = append(json.RawMessage(nil), descriptor.Base...)
	descriptor.User = append(json.RawMessage(nil), descriptor.User...)
	secrets := make([]SettingsSecretView, len(descriptor.Secrets))
	for index, secret := range descriptor.Secrets {
		secrets[index] = SettingsSecretView{Path: append([]string(nil), secret.Path...), Set: secret.Set}
	}
	descriptor.Secrets = secrets
	return descriptor
}

func indexExternalSettingsNamespaces(namespaces []SettingsDescriptor) map[string]int {
	result := make(map[string]int, len(namespaces))
	for index, descriptor := range namespaces {
		result[descriptor.Namespace] = index
	}
	return result
}
