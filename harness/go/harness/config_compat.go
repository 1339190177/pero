package harness

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"

	harnessconfig "github.com/phodal/pero/harness/go/config"
	point "github.com/phodal/pero/harness/go/runtime"
)

// These aliases preserve the root-package API while config owns launch-time
// environment and outbound proxy policy. New domain packages should import
// config directly.
type LaunchEnvironmentSource = harnessconfig.LaunchEnvironmentSource

const (
	LaunchEnvironmentProcess = harnessconfig.LaunchEnvironmentProcess
	LaunchEnvironmentProject = harnessconfig.LaunchEnvironmentProject
	LaunchEnvironmentUser    = harnessconfig.LaunchEnvironmentUser
)

type LaunchEnvironmentLayer = harnessconfig.LaunchEnvironmentLayer
type LaunchEnvironmentEntry = harnessconfig.LaunchEnvironmentEntry
type LaunchEnvironmentSnapshot = harnessconfig.LaunchEnvironmentSnapshot
type LayeredEnvironmentOptions = harnessconfig.LayeredEnvironmentOptions

type OutboundProxyDiagnostic = harnessconfig.OutboundProxyDiagnostic
type OutboundProxyPolicy = harnessconfig.OutboundProxyPolicy
type OutboundProxyResolution = harnessconfig.OutboundProxyResolution

func NewLaunchEnvironmentSnapshot(layers []LaunchEnvironmentLayer) (*LaunchEnvironmentSnapshot, error) {
	return harnessconfig.NewLaunchEnvironmentSnapshot(layers)
}

func LoadLayeredEnvironment(options LayeredEnvironmentOptions) (*LaunchEnvironmentSnapshot, error) {
	return harnessconfig.LoadLayeredEnvironment(options)
}

func ResolveOutboundProxy(snapshot *LaunchEnvironmentSnapshot) OutboundProxyResolution {
	return harnessconfig.ResolveOutboundProxy(snapshot)
}

func InstallOutboundProxy(policy OutboundProxyPolicy) func() {
	return harnessconfig.InstallOutboundProxy(policy)
}

const (
	InvalidSettingsCode              = harnessconfig.InvalidSettingsCode
	DuplicateSettingsCode            = harnessconfig.DuplicateSettingsCode
	SettingsConflictCode             = harnessconfig.SettingsConflictCode
	SettingsDisposedCode             = harnessconfig.SettingsDisposedCode
	SettingsLive                     = harnessconfig.SettingsLive
	SettingsRestart                  = harnessconfig.SettingsRestart
	SettingsSourceUpdate             = harnessconfig.SettingsSourceUpdate
	SettingsSourceProvider           = harnessconfig.SettingsSourceProvider
	MaxExternalSettingsNamespaces    = harnessconfig.MaxExternalSettingsNamespaces
	MaxExternalSettingsSnapshotBytes = harnessconfig.MaxExternalSettingsSnapshotBytes
	maxSettingsJSONSafeRevision      = 1<<53 - 1

	MissingCredentialCode   = harnessconfig.MissingCredentialCode
	InvalidCredentialCode   = harnessconfig.InvalidCredentialCode
	CredentialsDisposedCode = harnessconfig.CredentialsDisposedCode
	CredentialRecordAPIKey  = harnessconfig.CredentialRecordAPIKey
	CredentialRecordGrant   = harnessconfig.CredentialRecordGrant
)

type SettingsApplies = harnessconfig.SettingsApplies
type SettingsUpdateSource = harnessconfig.SettingsUpdateSource
type SettingsBackend = harnessconfig.SettingsBackend
type SettingsMutation = harnessconfig.SettingsMutation
type SettingsWatchBackend = harnessconfig.SettingsWatchBackend
type SettingsSectionSpec[T any] = harnessconfig.SettingsSectionSpec[T]
type SettingsDescribeOptions = harnessconfig.SettingsDescribeOptions
type SettingsSecretView = harnessconfig.SettingsSecretView
type SettingsDescriptor = harnessconfig.SettingsDescriptor
type SettingsPathOp = harnessconfig.SettingsPathOp
type SettingsConflictError = harnessconfig.SettingsConflictError
type SettingsProvider = harnessconfig.SettingsProvider
type SettingsScope[T any] = harnessconfig.SettingsScope[T]
type FileSettingsOptions = harnessconfig.FileSettingsOptions
type FileSettingsBackend = harnessconfig.FileSettingsBackend
type ExternalSettingsMutator = harnessconfig.ExternalSettingsMutator
type ExternalSettingsSnapshot = harnessconfig.ExternalSettingsSnapshot
type ExternalSettingsSource = harnessconfig.ExternalSettingsSource
type ExternalSettingsMirror = harnessconfig.ExternalSettingsMirror

type CredentialRef = harnessconfig.CredentialRef
type ResolvedCredential = harnessconfig.ResolvedCredential
type CredentialKey = harnessconfig.CredentialKey
type CredentialRecordKind = harnessconfig.CredentialRecordKind
type CredentialRecord = harnessconfig.CredentialRecord
type CredentialRecordInfo = harnessconfig.CredentialRecordInfo
type CredentialRecordEntry = harnessconfig.CredentialRecordEntry
type SecretRefView = harnessconfig.SecretRefView
type CredentialRecordMutation = harnessconfig.CredentialRecordMutation
type CredentialResolver = harnessconfig.CredentialResolver
type CredentialProvider = harnessconfig.CredentialProvider
type CredentialWatchProvider = harnessconfig.CredentialWatchProvider
type CredentialProviderLifecycle = harnessconfig.CredentialProviderLifecycle
type CredentialResolverFunc = harnessconfig.CredentialResolverFunc
type EnvironmentCredentialResolver = harnessconfig.EnvironmentCredentialResolver
type CredentialInfo = harnessconfig.CredentialInfo
type CredentialStore = harnessconfig.CredentialStore
type FileCredentialOptions = harnessconfig.FileCredentialOptions
type FileCredentialProvider = harnessconfig.FileCredentialProvider

func OpenSettingsProvider(ctx context.Context, backend SettingsBackend, logger *slog.Logger) (*SettingsProvider, error) {
	return harnessconfig.OpenSettingsProvider(ctx, backend, logger)
}

func RegisterSettingsSection[T any](provider *SettingsProvider, run *point.Run, namespace string, spec SettingsSectionSpec[T]) (*SettingsScope[T], error) {
	return harnessconfig.RegisterSettingsSection(provider, run, namespace, spec)
}

func NewFileSettingsBackend(options FileSettingsOptions) (*FileSettingsBackend, error) {
	return harnessconfig.NewFileSettingsBackend(options)
}

func RegisterExternalSettingsSource(provider *SettingsProvider, run *point.Run, source ExternalSettingsSource) (*ExternalSettingsMirror, error) {
	return harnessconfig.RegisterExternalSettingsSource(provider, run, source)
}

func ParseCredentialRef(value string) (CredentialRef, error) {
	return harnessconfig.ParseCredentialRef(value)
}

func NewCredentialKey(scope, id string) (CredentialKey, error) {
	return harnessconfig.NewCredentialKey(scope, id)
}

func ParseCredentialKey(value string) (CredentialKey, error) {
	return harnessconfig.ParseCredentialKey(value)
}

func NewCredentialStore(logger *slog.Logger) *CredentialStore {
	return harnessconfig.NewCredentialStore(logger)
}

func AssertUsableAPIKey(raw, providerPackage string, ref CredentialRef) (string, error) {
	return harnessconfig.AssertUsableAPIKey(raw, providerPackage, ref)
}

func ResolveAPIKey(ctx context.Context, resolver CredentialResolver, providerPackage string, ref CredentialRef) (string, error) {
	return harnessconfig.ResolveAPIKey(ctx, resolver, providerPackage, ref)
}

func NewFileCredentialProvider(options FileCredentialOptions) (*FileCredentialProvider, error) {
	return harnessconfig.NewFileCredentialProvider(options)
}

func RenderFlatCredentialMigration(source []byte) ([]byte, bool) {
	return harnessconfig.RenderFlatCredentialMigration(source)
}

func cloneSettingsDocument(source map[string]json.RawMessage) map[string]json.RawMessage {
	if source == nil {
		return nil
	}
	result := make(map[string]json.RawMessage, len(source))
	for key, value := range source {
		result[key] = append(json.RawMessage(nil), value...)
	}
	return result
}

func sameRawSettingsSection(left json.RawMessage, leftFound bool, right json.RawMessage, rightFound bool) bool {
	if leftFound != rightFound {
		return false
	}
	if !leftFound {
		return true
	}
	var leftValue, rightValue any
	if json.Unmarshal(left, &leftValue) != nil || json.Unmarshal(right, &rightValue) != nil {
		return bytes.Equal(left, right)
	}
	leftCanonical, _ := json.Marshal(leftValue)
	rightCanonical, _ := json.Marshal(rightValue)
	return bytes.Equal(leftCanonical, rightCanonical)
}
