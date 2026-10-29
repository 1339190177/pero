package harness

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	mathrand "math/rand/v2"
	"sort"
	"sync"
	"time"

	point "github.com/phodal/pero/harness/go/runtime"
)

// LLMRetryEvent is appended before a cancellable provider retry wait.
type LLMRetryEvent struct {
	RetryID    string          `json:"retryId"`
	Turn       int             `json:"turn"`
	Step       int             `json:"step"`
	Provider   string          `json:"provider"`
	Mode       RetryMode       `json:"mode"`
	PolicyKey  string          `json:"policyKey"`
	Retry      int             `json:"retry"`
	MaxRetries *int            `json:"maxRetries,omitempty"`
	DelayMS    int64           `json:"delayMs"`
	Failure    ProviderFailure `json:"failure"`
}

// LLMRetryStartedEvent is appended after the delay and before the next model
// request, distinguishing scheduled retries from attempts that actually began.
type LLMRetryStartedEvent struct {
	RetryID string `json:"retryId"`
	Turn    int    `json:"turn"`
	Step    int    `json:"step"`
	Retry   int    `json:"retry"`
}

type retryExecutor struct {
	policies        RetryPolicyProvider
	executionPolicy *RetryExecutionPolicy
	random          func() float64
	lifetime        context.Context
	cancel          context.CancelCauseFunc

	mu       sync.Mutex
	active   int
	disposed bool
	drained  *sync.Cond
	logger   interface{ Warn(string, ...any) }
}

func newRetryExecutor(policies RetryPolicyProvider, random func() float64, logger interface{ Warn(string, ...any) }, executionPolicy ...*RetryExecutionPolicy) *retryExecutor {
	if random == nil {
		random = mathrand.Float64
	}
	lifetime, cancel := context.WithCancelCause(context.Background())
	var control *RetryExecutionPolicy
	if len(executionPolicy) > 0 {
		control = executionPolicy[0]
	}
	executor := &retryExecutor{policies: policies, executionPolicy: control, random: random, lifetime: lifetime, cancel: cancel, logger: logger}
	executor.drained = sync.NewCond(&executor.mu)
	return executor
}

func (e *retryExecutor) begin() bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.disposed {
		return false
	}
	e.active++
	return true
}

func (e *retryExecutor) end() {
	e.mu.Lock()
	e.active--
	if e.active == 0 {
		e.drained.Broadcast()
	}
	e.mu.Unlock()
}

func (e *retryExecutor) dispose(context.Context) error {
	e.mu.Lock()
	if !e.disposed {
		e.disposed = true
		e.cancel(errors.New("harness: retry effect disposed"))
	}
	for e.active != 0 {
		e.drained.Wait()
	}
	e.mu.Unlock()
	return nil
}

func (e *retryExecutor) recover(ctx context.Context, input RequestErrorInput, next point.Next[RequestErrorInput]) (RequestErrorInput, error) {
	enabled, generation := e.executionPolicy.Capture()
	if !enabled {
		return next(input)
	}
	var policy ResolvedRetryPolicy
	ok := input.RetryPolicy != nil
	if ok {
		policy = cloneRetryPolicy(*input.RetryPolicy)
	} else {
		policy, ok = e.policies.RetryPolicyForProvider(input.Provider)
	}
	if !ok {
		return next(input)
	}
	if err := ValidateResolvedRetryPolicy(policy); err != nil {
		return input, fmt.Errorf("harness: provider retry policy: %w", err)
	}
	if policy.Mode == RetryAlways {
		downstream, downstreamErr := next(input)
		if err := contextFailure(ctx); err != nil {
			return input, err
		}
		if err := contextFailure(e.lifetime); err != nil {
			return input, err
		}
		if generation.Err() != nil {
			return input, nil
		}
		if downstreamErr != nil && e.logger != nil {
			e.logger.Warn("provider always-retry ignored downstream recovery failure", "provider", input.Provider, "error", downstreamErr)
		}
		if downstreamErr == nil && downstream.Retry {
			return downstream, nil
		}
	} else if !containsCode(policy.RetryableCodes, input.Failure.Code) {
		return next(input)
	}

	policyKey, err := retryPolicyKey(policy)
	if err != nil {
		return input, err
	}
	previousRetry, retryID, err := retryStateForOpenStep(input.Agent.Session(), input.Turn, input.Step, input.Provider, policyKey)
	if err != nil {
		return input, err
	}
	if policy.Mode == RetryNormal && previousRetry >= policy.MaxRetries {
		return next(input)
	}
	retry := previousRetry + 1
	if retryID == "" {
		retryID, err = newRetryID()
		if err != nil {
			return input, err
		}
	}
	delayMS := input.Failure.ProviderRetryAfterMS
	if delayMS > policy.MaxDelayMS {
		if policy.Mode == RetryNormal {
			return next(input)
		}
		delayMS = 0
	}
	if delayMS <= 0 {
		delayMS = localRetryDelay(policy, retry, e.random())
	}
	event := LLMRetryEvent{
		RetryID: retryID, Turn: input.Turn, Step: input.Step, Provider: input.Provider,
		Mode: policy.Mode, PolicyKey: policyKey, Retry: retry, DelayMS: delayMS, Failure: input.Failure,
	}
	if policy.Mode == RetryNormal {
		maxRetries := policy.MaxRetries
		event.MaxRetries = &maxRetries
	}
	if _, err := input.Agent.Session().Append("llm/retry", event); err != nil {
		return input, err
	}
	if !waitForRetry(ctx, e.lifetime, generation, time.Duration(delayMS)*time.Millisecond) {
		if err := contextFailure(ctx); err != nil {
			return input, err
		}
		if err := contextFailure(e.lifetime); err != nil {
			return input, err
		}
		return input, nil
	}
	if err := contextFailure(ctx); err != nil {
		return input, err
	}
	if err := contextFailure(e.lifetime); err != nil {
		return input, err
	}
	if generation.Err() != nil {
		return input, nil
	}
	if _, err := input.Agent.Session().Append("llm/retry-started", LLMRetryStartedEvent{
		RetryID: retryID, Turn: input.Turn, Step: input.Step, Retry: retry,
	}); err != nil {
		return input, err
	}
	input.Retry = true
	return input, nil
}

func containsCode(codes []string, code string) bool {
	for _, candidate := range codes {
		if candidate == code {
			return true
		}
	}
	return false
}

func retryPolicyKey(policy ResolvedRetryPolicy) (string, error) {
	if policy.Mode == RetryAlways {
		data, err := json.Marshal([]any{policy.Mode, policy.InitialDelayMS, policy.MaxDelayMS, policy.JitterRatio})
		return string(data), err
	}
	codes := append([]string(nil), policy.RetryableCodes...)
	sort.Strings(codes)
	data, err := json.Marshal([]any{policy.Mode, policy.MaxRetries, codes, policy.InitialDelayMS, policy.MaxDelayMS, policy.JitterRatio})
	return string(data), err
}

func retryStateForOpenStep(session *Session, turn, step int, provider, policyKey string) (int, string, error) {
	if session == nil {
		return 0, "", errors.New("harness: retry recovery requires a session")
	}
	events := session.Events()
	start := -1
	for index := len(events) - 1; index >= 0; index-- {
		if events[index].Type != "step/start" {
			continue
		}
		var marker struct{ Turn, Step int }
		if err := json.Unmarshal(events[index].Data, &marker); err != nil {
			return 0, "", fmt.Errorf("harness: decode step/start at seq %d: %w", events[index].Seq, err)
		}
		if marker.Turn == turn && marker.Step == step {
			start = index
			break
		}
	}
	if start < 0 {
		return 0, "", fmt.Errorf("harness: retry state has no open step %d/%d", turn, step)
	}
	retry := 0
	retryID := ""
	for _, event := range events[start+1:] {
		if event.Type == "step/end" || event.Type == "turn/end" {
			return 0, "", fmt.Errorf("harness: retry state step %d/%d is closed", turn, step)
		}
		if event.Type != "llm/retry" {
			continue
		}
		var scheduled LLMRetryEvent
		if err := json.Unmarshal(event.Data, &scheduled); err != nil {
			return 0, "", fmt.Errorf("harness: decode llm/retry at seq %d: %w", event.Seq, err)
		}
		if scheduled.Turn == turn && scheduled.Step == step && scheduled.Provider == provider && scheduled.PolicyKey == policyKey {
			retry = scheduled.Retry
			retryID = scheduled.RetryID
		}
	}
	return retry, retryID, nil
}

func localRetryDelay(policy ResolvedRetryPolicy, retry int, sample float64) int64 {
	delay := policy.InitialDelayMS
	for count := 1; count < retry && delay < policy.MaxDelayMS; count++ {
		if delay > policy.MaxDelayMS/2 {
			delay = policy.MaxDelayMS
			break
		}
		delay *= 2
	}
	if sample < 0 {
		sample = 0
	} else if sample > 1 {
		sample = 1
	}
	multiplier := 1 - policy.JitterRatio + 2*policy.JitterRatio*sample
	result := int64(math.Min(float64(delay)*multiplier, float64(policy.MaxDelayMS)))
	if result < 1 {
		return 1
	}
	return result
}

func waitForRetry(request, lifetime, generation context.Context, delay time.Duration) bool {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-timer.C:
		return true
	case <-request.Done():
		return false
	case <-lifetime.Done():
		return false
	case <-generation.Done():
		return false
	}
}

func newRetryID() (string, error) {
	var value [16]byte
	if _, err := rand.Read(value[:]); err != nil {
		return "", fmt.Errorf("harness: create retry id: %w", err)
	}
	value[6] = value[6]&0x0f | 0x40
	value[8] = value[8]&0x3f | 0x80
	encoded := hex.EncodeToString(value[:])
	return encoded[0:8] + "-" + encoded[8:12] + "-" + encoded[12:16] + "-" + encoded[16:20] + "-" + encoded[20:32], nil
}
