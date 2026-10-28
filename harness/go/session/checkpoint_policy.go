// Package session owns Session lifecycle, durability, and projection policy.
package session

import (
	"context"
	"errors"
	"sync"

	"github.com/phodal/pero/harness/go/runtime"
)

// CheckpointPolicyKey is the single authoritative identity for the Session
// checkpoint policy service. Compatibility packages must alias this value.
var CheckpointPolicyKey = runtime.NewKey[*CheckpointPolicy]("harness.session-checkpoint-policy")

type checkpointPolicyOverride struct {
	enabled bool
	active  bool
}

type CheckpointPolicySnapshot struct {
	Overridden bool `json:"overridden"`
	Enabled    bool `json:"enabled"`
}

// CheckpointPolicy controls the DSH-shaped semantic flush boundaries. The Go
// default is enabled; scoped profile layers may explicitly disable or restore
// it without altering Session persistence ownership itself.
type CheckpointPolicy struct {
	mu         sync.RWMutex
	overrides  []*checkpointPolicyOverride
	effective  bool
	overridden bool
}

func NewCheckpointPolicy() *CheckpointPolicy { return &CheckpointPolicy{effective: true} }

func (p *CheckpointPolicy) Enabled() bool {
	if p == nil {
		return true
	}
	p.mu.RLock()
	enabled := p.effective
	p.mu.RUnlock()
	return enabled
}

func (p *CheckpointPolicy) Snapshot() CheckpointPolicySnapshot {
	if p == nil {
		return CheckpointPolicySnapshot{Enabled: true}
	}
	p.mu.RLock()
	snapshot := CheckpointPolicySnapshot{Overridden: p.overridden, Enabled: p.effective}
	p.mu.RUnlock()
	return snapshot
}

func (p *CheckpointPolicy) Override(enabled bool) (runtime.Dispose, error) {
	if p == nil {
		return nil, errors.New("session-checkpoint-policy: policy is nil")
	}
	p.mu.Lock()
	override := &checkpointPolicyOverride{enabled: enabled, active: true}
	p.overrides = append(p.overrides, override)
	p.effective = enabled
	p.overridden = true
	p.mu.Unlock()
	var once sync.Once
	return func(context.Context) error {
		once.Do(func() {
			p.mu.Lock()
			override.active = false
			for len(p.overrides) > 0 && !p.overrides[len(p.overrides)-1].active {
				p.overrides = p.overrides[:len(p.overrides)-1]
			}
			p.effective = true
			p.overridden = false
			if len(p.overrides) > 0 {
				p.effective = p.overrides[len(p.overrides)-1].enabled
				p.overridden = true
			}
			p.mu.Unlock()
		})
		return nil
	}, nil
}
