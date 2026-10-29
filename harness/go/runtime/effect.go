package runtime

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
)

type Dispose func(context.Context) error

type EffectState uint8

const (
	EffectActive EffectState = iota
	EffectStopping
	EffectStopped
)

func (s EffectState) String() string {
	switch s {
	case EffectActive:
		return "active"
	case EffectStopping:
		return "stopping"
	case EffectStopped:
		return "stopped"
	default:
		return "unknown"
	}
}

type EffectMeta struct {
	ID       uint64
	Label    string
	State    EffectState
	Children []EffectMeta
}

type effectEntry struct {
	id      uint64
	label   string
	dispose Dispose
	child   *EffectGroup
	state   atomic.Uint32
}

type EffectGroup struct {
	mu      sync.Mutex
	label   string
	nextID  uint64
	state   EffectState
	entries []*effectEntry
	done    chan struct{}
	stopErr error
}

func newEffectGroup(label string) *EffectGroup {
	return &EffectGroup{label: label, state: EffectActive, done: make(chan struct{})}
}

func (g *EffectGroup) add(label string, dispose Dispose) (*effectEntry, error) {
	return g.addEntry(label, dispose, nil)
}

func (g *EffectGroup) addChild(label string, child *EffectGroup) (*effectEntry, error) {
	if child == nil {
		return nil, nil
	}
	return g.addEntry(label, child.stop, child)
}

func (g *EffectGroup) addEntry(label string, dispose Dispose, child *EffectGroup) (*effectEntry, error) {
	if dispose == nil {
		return nil, nil
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.state != EffectActive {
		return nil, ErrEffectClosed
	}
	g.nextID++
	entry := &effectEntry{id: g.nextID, label: label, dispose: dispose, child: child}
	entry.state.Store(uint32(EffectActive))
	g.entries = append(g.entries, entry)
	return entry, nil
}

func (g *EffectGroup) stop(ctx context.Context) error {
	if ctx == nil {
		ctx = context.Background()
	}

	g.mu.Lock()
	if g.state != EffectActive {
		done := g.done
		g.mu.Unlock()
		select {
		case <-done:
			g.mu.Lock()
			err := g.stopErr
			g.mu.Unlock()
			return err
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	g.state = EffectStopping
	entries := append([]*effectEntry(nil), g.entries...)
	g.mu.Unlock()

	var joined error
	for index := len(entries) - 1; index >= 0; index-- {
		entry := entries[index]
		entry.state.Store(uint32(EffectStopping))
		if err := callDispose(ctx, entry.label, entry.dispose); err != nil {
			joined = errors.Join(joined, fmt.Errorf("effect %q: %w", entry.label, err))
		}
		entry.state.Store(uint32(EffectStopped))
	}

	g.mu.Lock()
	g.stopErr = joined
	g.state = EffectStopped
	close(g.done)
	g.mu.Unlock()
	return joined
}

func callDispose(ctx context.Context, label string, dispose Dispose) (err error) {
	defer recoverError("disposer "+label, &err)
	return dispose(ctx)
}

func (g *EffectGroup) snapshot() []EffectMeta {
	g.mu.Lock()
	entries := append([]*effectEntry(nil), g.entries...)
	g.mu.Unlock()
	result := make([]EffectMeta, 0, len(entries))
	for _, entry := range entries {
		meta := EffectMeta{
			ID: entry.id, Label: entry.label, State: EffectState(entry.state.Load()),
		}
		if entry.child != nil {
			meta.Children = entry.child.snapshot()
		}
		result = append(result, meta)
	}
	return result
}

type EffectScope struct {
	group *EffectGroup
}

func (s *EffectScope) Defer(label string, dispose Dispose) error {
	if s == nil || s.group == nil {
		return ErrEffectClosed
	}
	_, err := s.group.add(label, dispose)
	return err
}

type EffectHandle interface {
	Stop(context.Context) error
	Meta() EffectMeta
}

type effectHandle struct {
	entry *effectEntry
	group *EffectGroup
}

func (h *effectHandle) Stop(ctx context.Context) error { return h.group.stop(ctx) }

func (h *effectHandle) Meta() EffectMeta {
	if h == nil || h.entry == nil {
		return EffectMeta{}
	}
	meta := EffectMeta{
		ID: h.entry.id, Label: h.entry.label, State: EffectState(h.entry.state.Load()),
	}
	if h.entry.child != nil {
		meta.Children = h.entry.child.snapshot()
	}
	return meta
}
