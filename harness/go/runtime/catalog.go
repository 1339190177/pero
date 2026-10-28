package runtime

import (
	"errors"
	"fmt"
	"sort"
	"sync"
)

// DefinitionCatalog is a concurrency-safe registry of validated definitions.
type DefinitionCatalog struct {
	mu          sync.RWMutex
	definitions map[string]Definition
}

func NewDefinitionCatalog() *DefinitionCatalog {
	return &DefinitionCatalog{definitions: make(map[string]Definition)}
}

func (c *DefinitionCatalog) Register(definitions ...Definition) error {
	if c == nil {
		return errors.New("runtime: definition catalog must not be nil")
	}
	for _, definition := range definitions {
		if err := definition.validate(); err != nil {
			return err
		}
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.definitions == nil {
		c.definitions = make(map[string]Definition)
	}
	batch := make(map[string]struct{}, len(definitions))
	for _, definition := range definitions {
		id := definition.descriptor.ID
		if _, exists := c.definitions[id]; exists {
			return fmt.Errorf("%w: %q", ErrDefinitionConflict, id)
		}
		if _, exists := batch[id]; exists {
			return fmt.Errorf("%w: %q", ErrDefinitionConflict, id)
		}
		batch[id] = struct{}{}
	}
	for _, definition := range definitions {
		c.definitions[definition.descriptor.ID] = definition
	}
	return nil
}

func (c *DefinitionCatalog) Lookup(id string) (Definition, bool) {
	if c == nil {
		return Definition{}, false
	}
	c.mu.RLock()
	definition, ok := c.definitions[id]
	c.mu.RUnlock()
	return definition, ok
}

func (c *DefinitionCatalog) Descriptors() []Descriptor {
	if c == nil {
		return nil
	}
	c.mu.RLock()
	result := make([]Descriptor, 0, len(c.definitions))
	for _, definition := range c.definitions {
		result = append(result, definition.Descriptor())
	}
	c.mu.RUnlock()
	sort.Slice(result, func(left, right int) bool { return result[left].ID < result[right].ID })
	return result
}
