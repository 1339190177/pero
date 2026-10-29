package runtime

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
)

type Descriptor struct {
	ID       string
	Name     string
	Requires []Requirement
	Provides []AnyKey
}

// Definition is a type-erased plugin factory. Construct one with Define so
// configuration remains typed at the public boundary.
type Definition struct {
	descriptor Descriptor
	decode     func(json.RawMessage) (any, error)
	start      func(*Run, any) (Dispose, error)
}

func Define[C any](
	descriptor Descriptor,
	decode func(json.RawMessage) (C, error),
	start func(*Run, C) (Dispose, error),
) Definition {
	return Definition{
		descriptor: descriptor,
		decode: func(raw json.RawMessage) (any, error) {
			if decode == nil {
				var zero C
				return zero, nil
			}
			return decode(raw)
		},
		start: func(run *Run, config any) (Dispose, error) {
			typed, ok := config.(C)
			if !ok {
				return nil, fmt.Errorf("%w: decoded config type %T", ErrInvalidDefinition, config)
			}
			if start == nil {
				return nil, nil
			}
			return start(run, typed)
		},
	}
}

func (d Definition) Descriptor() Descriptor {
	copyDescriptor := d.descriptor
	copyDescriptor.Requires = append([]Requirement(nil), d.descriptor.Requires...)
	copyDescriptor.Provides = append([]AnyKey(nil), d.descriptor.Provides...)
	return copyDescriptor
}

// DecodeJSON strictly decodes one JSON value and rejects unknown fields and
// trailing data. It is intended to be passed directly to Define.
func DecodeJSON[C any](raw json.RawMessage) (C, error) {
	var value C
	if len(bytes.TrimSpace(raw)) == 0 {
		raw = json.RawMessage("{}")
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&value); err != nil {
		return value, err
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		if err == nil {
			return value, errors.New("runtime: config contains trailing JSON value")
		}
		return value, err
	}
	return value, nil
}

func (d Definition) validate() error {
	if d.descriptor.ID == "" {
		return fmt.Errorf("%w: descriptor ID must not be empty", ErrInvalidDefinition)
	}
	if d.decode == nil || d.start == nil {
		return fmt.Errorf("%w: definition %q was not created with Define", ErrInvalidDefinition, d.descriptor.ID)
	}
	seenRequirements := make(map[uint64]struct{}, len(d.descriptor.Requires))
	for _, requirement := range d.descriptor.Requires {
		if err := validateAnyKey(requirement.Key); err != nil {
			return fmt.Errorf("%w: definition %q requirement: %v", ErrInvalidDefinition, d.descriptor.ID, err)
		}
		if _, exists := seenRequirements[requirement.Key.id]; exists {
			return fmt.Errorf("%w: definition %q repeats requirement %q", ErrInvalidDefinition, d.descriptor.ID, requirement.Key.name)
		}
		seenRequirements[requirement.Key.id] = struct{}{}
	}
	seenProvides := make(map[uint64]struct{}, len(d.descriptor.Provides))
	for _, key := range d.descriptor.Provides {
		if err := validateAnyKey(key); err != nil {
			return fmt.Errorf("%w: definition %q provide: %v", ErrInvalidDefinition, d.descriptor.ID, err)
		}
		if _, exists := seenProvides[key.id]; exists {
			return fmt.Errorf("%w: definition %q repeats provided service %q", ErrInvalidDefinition, d.descriptor.ID, key.name)
		}
		seenProvides[key.id] = struct{}{}
	}
	return nil
}
