package session

import "github.com/phodal/pero/harness/go/runtime"

var (
	StoreKey              = runtime.NewKey[*SessionStore]("harness.sessions")
	ProjectionRegistryKey = runtime.NewKey[*SessionProjectionRegistry]("harness.session-projections")
)
