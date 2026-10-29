// Package runtime provides a typed, dependency-reactive plugin runtime.
//
// Each mounted Definition has a stable Instance identity and one serialized
// activation at a time. Runs own their effects, goroutines, child instances,
// and staged service leases. Required services are addressed by typed Key and
// per-key Realm; provider publication, readiness, replacement, and removal
// drive consumers toward the latest dependency epoch.
//
// The package intentionally uses context.Context only for cancellation and
// call propagation. Scope represents spatial service resolution, while Run
// represents one temporal activation.
package runtime
