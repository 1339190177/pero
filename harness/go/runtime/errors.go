package runtime

import (
	"errors"
	"fmt"
	"runtime/debug"
)

var (
	ErrClosedRuntime      = errors.New("runtime: runtime is closed")
	ErrInactiveRun        = errors.New("runtime: run is inactive")
	ErrEffectClosed       = errors.New("runtime: effect group is stopping or stopped")
	ErrInvalidDefinition  = errors.New("runtime: invalid definition")
	ErrInvalidKey         = errors.New("runtime: invalid service key")
	ErrUndeclaredService  = errors.New("runtime: service is not declared by the definition")
	ErrServiceUnavailable = errors.New("runtime: service is unavailable")
	ErrServiceConflict    = errors.New("runtime: service address already has a provider")
	ErrDuplicateProvide   = errors.New("runtime: service was already provided by this activation")
	ErrClosedLease        = errors.New("runtime: service lease is closed")
	ErrNotOwner           = errors.New("runtime: operation is not owned by this activation")
	ErrDisposedInstance   = errors.New("runtime: instance is disposed")
	ErrDefinitionConflict = errors.New("runtime: definition ID is already registered")
	ErrUnknownDefinition  = errors.New("runtime: definition is not registered")
	ErrInvalidApplication = errors.New("runtime: invalid application document")
)

// PanicError preserves a recovered panic and its stack at a framework boundary.
type PanicError struct {
	Boundary string
	Value    any
	Stack    []byte
}

func (e *PanicError) Error() string {
	return fmt.Sprintf("runtime: panic in %s: %v", e.Boundary, e.Value)
}

func recoverError(boundary string, target *error) {
	if value := recover(); value != nil {
		*target = &PanicError{Boundary: boundary, Value: value, Stack: debug.Stack()}
	}
}

func recoverNamedError(kind, name string, target *error) {
	if value := recover(); value != nil {
		*target = &PanicError{Boundary: kind + " " + name, Value: value, Stack: debug.Stack()}
	}
}
