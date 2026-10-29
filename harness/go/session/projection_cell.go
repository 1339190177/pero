package session

import (
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
)

func (r *SessionProjectionRegistry) materializeCellLocked(registration *projectionRegistration, session *Session, events []Event) (*projectionCell, error) {
	cell := registration.cells[session]
	if cell == nil {
		var err error
		cell, err = buildProjectionCell(registration.definition, session.Header(), events)
		if err != nil {
			return nil, err
		}
		// Ordinary cold reads are transient and must not retain a detached
		// Session forever. Hydrate installs an explicit prepared cell itself;
		// attached live Sessions are safe to cache here.
		if _, attached := r.sessions[session]; attached {
			registration.cells[session] = cell
		}
	} else if cell.err != nil {
		return nil, cell.err
	} else if cell.observedSeq < eventTail(events) {
		var err error
		cell, err = advanceProjectionCell(registration.definition, cell, events)
		if err != nil {
			return nil, err
		}
	}
	return cell, nil
}

func advanceProjectionCell(definition ProjectionDefinition, cell *projectionCell, events []Event) (*projectionCell, error) {
	if cell == nil {
		return nil, errors.New("harness: cannot advance a nil projection cell")
	}
	if cell.err != nil {
		return nil, cell.err
	}
	for _, event := range events {
		if event.Seq <= cell.observedSeq {
			continue
		}
		if event.Seq != cell.observedSeq+1 {
			return nil, fmt.Errorf("harness: session projection %q cannot advance across missing seq %d", definition.Key, cell.observedSeq+1)
		}
		next, err := callProjectionApply(definition, cell.state, event)
		if err != nil {
			return nil, err
		}
		same, err := projectionSameState(definition, cell.state, next)
		if err != nil {
			return nil, err
		}
		if !same {
			cell.previousView, cell.previousKnown = cell.currentView, cell.currentKnown
			cell.currentView, cell.currentKnown = nil, false
		}
		cell.state, cell.observedSeq = next, event.Seq
	}
	return cell, nil
}

func buildProjectionCell(definition ProjectionDefinition, header SessionHeader, events []Event) (*projectionCell, error) {
	state, err := callProjectionInit(definition, header)
	if err != nil {
		return nil, fmt.Errorf("harness: init session projection %q: %w", definition.Key, err)
	}
	observed := uint64(0)
	for _, event := range events {
		if event.Seq != observed+1 {
			return nil, fmt.Errorf("harness: session projection %q cannot build across missing seq %d", definition.Key, observed+1)
		}
		state, err = callProjectionApply(definition, state, event)
		if err != nil {
			return nil, fmt.Errorf("harness: apply session projection %q at seq %d: %w", definition.Key, event.Seq, err)
		}
		observed = event.Seq
	}
	return &projectionCell{state: state, observedSeq: observed}, nil
}

func projectionView(definition ProjectionDefinition, state any) (json.RawMessage, any, error) {
	if definition.Wire == nil {
		return nil, nil, fmt.Errorf("harness: session projection %q has no wire view", definition.Key)
	}
	value, err := callProjectionView(definition, state)
	if err != nil {
		return nil, nil, fmt.Errorf("harness: view session projection %q: %w", definition.Key, err)
	}
	raw, err := callProjectionEncodeView(definition, value)
	if err != nil {
		return nil, nil, fmt.Errorf("harness: encode session projection %q view: %w", definition.Key, err)
	}
	return append(json.RawMessage(nil), raw...), value, nil
}

func projectionSameState(definition ProjectionDefinition, left, right any) (same bool, err error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			err = fmt.Errorf("harness: compare session projection %q state: %v", definition.Key, recovered)
		}
	}()
	if definition.SameState != nil {
		return definition.SameState(left, right), nil
	}
	return sameProjectionIdentity(left, right), nil
}

func callProjectionInit(definition ProjectionDefinition, header SessionHeader) (state any, err error) {
	defer projectionRecover(definition.Key, "init", &err)
	return definition.Init(header)
}

func callProjectionApply(definition ProjectionDefinition, state any, event Event) (next any, err error) {
	defer projectionRecover(definition.Key, fmt.Sprintf("apply at seq %d", event.Seq), &err)
	return definition.Apply(state, event)
}

func callProjectionEncodeState(definition ProjectionDefinition, state any) (raw json.RawMessage, err error) {
	defer projectionRecover(definition.Key, "encode state", &err)
	raw, err = definition.EncodeState(state)
	if err == nil && !json.Valid(raw) {
		err = errors.New("encoded state is not valid JSON")
	}
	return append(json.RawMessage(nil), raw...), err
}

func callProjectionDecodeState(definition ProjectionDefinition, raw json.RawMessage) (state any, err error) {
	defer projectionRecover(definition.Key, "decode state", &err)
	if !json.Valid(raw) {
		return nil, errors.New("checkpoint state is not valid JSON")
	}
	return definition.DecodeState(append(json.RawMessage(nil), raw...))
}

func callProjectionView(definition ProjectionDefinition, state any) (value any, err error) {
	defer projectionRecover(definition.Key, "view", &err)
	return definition.Wire.View(state)
}

func callProjectionEncodeView(definition ProjectionDefinition, value any) (raw json.RawMessage, err error) {
	defer projectionRecover(definition.Key, "encode view", &err)
	raw, err = definition.Wire.EncodeView(value)
	if err == nil && !json.Valid(raw) {
		err = errors.New("encoded view is not valid JSON")
	}
	return append(json.RawMessage(nil), raw...), err
}

func projectionRecover(key, operation string, target *error) {
	if recovered := recover(); recovered != nil {
		*target = fmt.Errorf("harness: session projection %q %s panicked: %v", key, operation, recovered)
	}
}

func sameProjectionIdentity(left, right any) bool {
	if left == nil || right == nil {
		return left == nil && right == nil
	}
	leftValue, rightValue := reflect.ValueOf(left), reflect.ValueOf(right)
	if leftValue.Type() != rightValue.Type() {
		return false
	}
	switch leftValue.Kind() {
	case reflect.Bool, reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr,
		reflect.Float32, reflect.Float64, reflect.Complex64, reflect.Complex128, reflect.String:
		return leftValue.Interface() == rightValue.Interface()
	case reflect.Chan, reflect.Func, reflect.Map, reflect.Pointer, reflect.Slice, reflect.UnsafePointer:
		if leftValue.IsNil() || rightValue.IsNil() {
			return leftValue.IsNil() && rightValue.IsNil()
		}
		return leftValue.Pointer() == rightValue.Pointer()
	default:
		// Struct/array/interface aggregate values have no stable reference
		// identity in Go; conservatively treat a returned value as changed.
		return false
	}
}
