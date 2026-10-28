package runtime

import (
	"fmt"
	"sync/atomic"
)

var nextKeyID atomic.Uint64

// Key is the typed identity of a service. The display name is diagnostic only;
// identity comes from the private process-local ID.
type Key[T any] struct {
	id   uint64
	name string
}

// AnyKey is the type-erased form used by descriptors and the runtime registry.
type AnyKey struct {
	id   uint64
	name string
}

func NewKey[T any](name string) Key[T] {
	if name == "" {
		panic("runtime: service key name must not be empty")
	}
	return Key[T]{id: nextKeyID.Add(1), name: name}
}

func (k Key[T]) Any() AnyKey    { return AnyKey{id: k.id, name: k.name} }
func (k Key[T]) Name() string   { return k.name }
func (k Key[T]) String() string { return k.name }

func (k AnyKey) ID() uint64     { return k.id }
func (k AnyKey) Name() string   { return k.name }
func (k AnyKey) String() string { return k.name }
func (k AnyKey) valid() bool    { return k.id != 0 && k.name != "" }

type Requirement struct {
	Key      AnyKey
	Optional bool
}

func Require[T any](key Key[T]) Requirement {
	return Requirement{Key: key.Any()}
}

func Want[T any](key Key[T]) Requirement {
	return Requirement{Key: key.Any(), Optional: true}
}

func validateAnyKey(key AnyKey) error {
	if !key.valid() {
		return fmt.Errorf("%w: name=%q id=%d", ErrInvalidKey, key.name, key.id)
	}
	return nil
}
