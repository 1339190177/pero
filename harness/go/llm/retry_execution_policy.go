package llm

import (
	"context"
	"errors"
	"sync"
)

type retryExecutionOverride struct {
	enabled bool
	active  bool
}

type RetryExecutionPolicySnapshot struct {
	Overridden bool `json:"overridden"`
	Enabled    bool `json:"enabled"`
}

// RetryExecutionPolicy controls whether the DSH-shaped llm-retry executor is
// present. Provider adapters continue to own retry policy values; this service
// only models the effect-owned executor lifecycle exposed by the Base profile.
type RetryExecutionPolicy struct {
	mu         sync.RWMutex
	overrides  []*retryExecutionOverride
	effective  bool
	overridden bool
	generation context.Context
	cancel     context.CancelCauseFunc
}

func NewRetryExecutionPolicy() *RetryExecutionPolicy {
	generation, cancel := context.WithCancelCause(context.Background())
	return &RetryExecutionPolicy{effective: true, generation: generation, cancel: cancel}
}

func (p *RetryExecutionPolicy) Enabled() bool {
	if p == nil {
		return true
	}
	p.mu.RLock()
	enabled := p.effective
	p.mu.RUnlock()
	return enabled
}

func (p *RetryExecutionPolicy) Snapshot() RetryExecutionPolicySnapshot {
	if p == nil {
		return RetryExecutionPolicySnapshot{Enabled: true}
	}
	p.mu.RLock()
	snapshot := RetryExecutionPolicySnapshot{Overridden: p.overridden, Enabled: p.effective}
	p.mu.RUnlock()
	return snapshot
}

// Capture binds one request-error callback to the current executor generation.
// Disabling the profile aborts waits already owned by that generation, matching
// disposal of DSH's plugin effect during live profile replacement.
func (p *RetryExecutionPolicy) Capture() (bool, context.Context) {
	if p == nil {
		return true, context.Background()
	}
	p.mu.RLock()
	enabled, generation := p.effective, p.generation
	p.mu.RUnlock()
	return enabled, generation
}

func (p *RetryExecutionPolicy) Override(enabled bool) (func(context.Context) error, error) {
	if p == nil {
		return nil, errors.New("llm-retry: execution policy is nil")
	}
	p.mu.Lock()
	override := &retryExecutionOverride{enabled: enabled, active: true}
	p.overrides = append(p.overrides, override)
	p.applyEffectiveLocked(enabled, true)
	p.mu.Unlock()
	var once sync.Once
	return func(context.Context) error {
		once.Do(func() {
			p.mu.Lock()
			override.active = false
			for len(p.overrides) > 0 && !p.overrides[len(p.overrides)-1].active {
				p.overrides = p.overrides[:len(p.overrides)-1]
			}
			nextEnabled, nextOverridden := true, false
			if len(p.overrides) > 0 {
				nextEnabled = p.overrides[len(p.overrides)-1].enabled
				nextOverridden = true
			}
			p.applyEffectiveLocked(nextEnabled, nextOverridden)
			p.mu.Unlock()
		})
		return nil
	}, nil
}

func (p *RetryExecutionPolicy) applyEffectiveLocked(enabled, overridden bool) {
	if p.effective != enabled {
		if p.cancel != nil {
			p.cancel(errors.New("llm-retry: executor generation replaced"))
		}
		p.generation, p.cancel = context.WithCancelCause(context.Background())
	}
	p.effective = enabled
	p.overridden = overridden
}
