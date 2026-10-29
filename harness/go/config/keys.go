package config

import point "github.com/phodal/pero/harness/go/runtime"

var (
	SettingsProviderKey   = point.NewKey[*SettingsProvider]("harness.settings")
	CredentialProviderKey = point.NewKey[CredentialProvider]("harness.credentials")
)
