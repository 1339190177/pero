package session

import (
	"encoding/json"
	"fmt"
)

// ProjectionWireDefinition converts one host state into its client-visible
// whole JSON value. EncodeView is the schema/serialization boundary.
type ProjectionWireDefinition struct {
	View       func(any) (any, error)
	EncodeView func(any) (json.RawMessage, error)
}

// ProjectionDefinition is one synchronous, state-driven Session fold. State
// must be JSON serializable; DecodeState validates persisted checkpoints.
type ProjectionDefinition struct {
	Key          string
	StateVersion uint64
	Init         func(SessionHeader) (any, error)
	Apply        func(any, Event) (any, error)
	EncodeState  func(any) (json.RawMessage, error)
	DecodeState  func(json.RawMessage) (any, error)
	// SameState may override Go's DSH-shaped identity comparison. Aggregate
	// states should normally be pointers and return the same pointer when an
	// event is irrelevant.
	SameState func(any, any) bool
	Wire      *ProjectionWireDefinition
}

// ProjectionSpec is the typed authoring face for a client-visible unit.
type ProjectionSpec[S, V any] struct {
	Key           string
	StateVersion  uint64
	Init          func(SessionHeader) S
	Apply         func(S, Event) S
	ValidateState func(S) error
	Same          func(S, S) bool
	View          func(S) V
	ValidateView  func(V) error
}

// HostProjectionSpec is the typed authoring face for a host-only unit.
type HostProjectionSpec[S any] struct {
	Key           string
	StateVersion  uint64
	Init          func(SessionHeader) S
	Apply         func(S, Event) S
	ValidateState func(S) error
	Same          func(S, S) bool
}

// DefineProjection type-erases one typed client-visible projection while
// retaining JSON checkpoint and view validation.
func DefineProjection[S, V any](spec ProjectionSpec[S, V]) ProjectionDefinition {
	definition := typedProjectionBase(spec.Key, spec.StateVersion, spec.Init, spec.Apply, spec.ValidateState, spec.Same)
	definition.Wire = &ProjectionWireDefinition{
		View: func(state any) (any, error) {
			value, ok := state.(S)
			if !ok {
				return nil, fmt.Errorf("projection %q received state %T, expected its declared type", spec.Key, state)
			}
			if spec.View == nil {
				return nil, fmt.Errorf("projection %q has no view function", spec.Key)
			}
			return spec.View(value), nil
		},
		EncodeView: func(value any) (json.RawMessage, error) {
			typed, ok := value.(V)
			if !ok {
				return nil, fmt.Errorf("projection %q produced view %T, expected its declared type", spec.Key, value)
			}
			if spec.ValidateView != nil {
				if err := spec.ValidateView(typed); err != nil {
					return nil, err
				}
			}
			return marshalProjectionJSON(typed)
		},
	}
	return definition
}

// DefineHostProjection type-erases one typed host-only projection.
func DefineHostProjection[S any](spec HostProjectionSpec[S]) ProjectionDefinition {
	return typedProjectionBase(spec.Key, spec.StateVersion, spec.Init, spec.Apply, spec.ValidateState, spec.Same)
}

func typedProjectionBase[S any](key string, version uint64, init func(SessionHeader) S, apply func(S, Event) S, validate func(S) error, same func(S, S) bool) ProjectionDefinition {
	return ProjectionDefinition{
		Key: key, StateVersion: version,
		Init: func(header SessionHeader) (any, error) {
			if init == nil {
				return nil, fmt.Errorf("projection %q has no init function", key)
			}
			return init(header), nil
		},
		Apply: func(state any, event Event) (any, error) {
			value, ok := state.(S)
			if !ok {
				return nil, fmt.Errorf("projection %q received state %T, expected its declared type", key, state)
			}
			if apply == nil {
				return nil, fmt.Errorf("projection %q has no apply function", key)
			}
			return apply(value, event), nil
		},
		EncodeState: func(state any) (json.RawMessage, error) {
			value, ok := state.(S)
			if !ok {
				return nil, fmt.Errorf("projection %q received state %T, expected its declared type", key, state)
			}
			if validate != nil {
				if err := validate(value); err != nil {
					return nil, err
				}
			}
			return marshalProjectionJSON(value)
		},
		DecodeState: func(raw json.RawMessage) (any, error) {
			var value S
			if err := json.Unmarshal(raw, &value); err != nil {
				return nil, err
			}
			if validate != nil {
				if err := validate(value); err != nil {
					return nil, err
				}
			}
			return value, nil
		},
		SameState: func(left, right any) bool {
			leftValue, leftOK := left.(S)
			rightValue, rightOK := right.(S)
			if !leftOK || !rightOK {
				return false
			}
			if same != nil {
				return same(leftValue, rightValue)
			}
			return sameProjectionIdentity(leftValue, rightValue)
		},
	}
}

func marshalProjectionJSON(value any) (json.RawMessage, error) {
	raw, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	var compact json.RawMessage
	if err := json.Unmarshal(raw, &compact); err != nil {
		return nil, err
	}
	return append(json.RawMessage(nil), compact...), nil
}

// ProjectionState reads a typed host state without exposing registry storage.
func ProjectionState[S any](registry *SessionProjectionRegistry, session *Session, key string) (S, bool, error) {
	var zero S
	state, found, err := registry.StateOf(session, key)
	if err != nil || !found {
		return zero, found, err
	}
	value, ok := state.(S)
	if !ok {
		return zero, false, fmt.Errorf("projection %q state has type %T", key, state)
	}
	return value, true, nil
}
