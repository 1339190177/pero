package llm

import (
	"errors"
	"fmt"
	"math"
)

const (
	RetryNormal RetryMode = "normal"
	RetryAlways RetryMode = "always"
)

// defaultRetryableCodes is the bounded transient recovery set applied when a
// provider omits an explicit policy. MalformedResponseCode belongs here because
// one completed response whose streamed tool-call JSON does not parse is a
// transient generation failure: the adapter fails closed without repairing or
// executing those arguments, and a bounded retry can recover the step. Every
// other malformed tool result stays non-retryable: this code only ever appears
// on a provider generation failure.
var defaultRetryableCodes = []string{
	EmptyResponseCode,
	MalformedResponseCode,
	"RATE_LIMIT",
	"SERVER",
	"TIMEOUT",
	TransportErrorCode,
}

// RetryMode selects bounded transient recovery or unbounded recovery until the
// request or owning effect is cancelled.
type RetryMode string

type RetryBackoffConfig struct {
	InitialDelayMS *int64   `json:"initialDelayMs,omitempty"`
	MaxDelayMS     *int64   `json:"maxDelayMs,omitempty"`
	JitterRatio    *float64 `json:"jitterRatio,omitempty"`
}

type RetryPolicyConfig struct {
	Mode           RetryMode           `json:"mode"`
	MaxRetries     *int                `json:"maxRetries,omitempty"`
	RetryableCodes []string            `json:"retryableCodes,omitempty"`
	Backoff        *RetryBackoffConfig `json:"backoff,omitempty"`
}

// ResolvedRetryPolicy is immutable by convention. Adapters should return a
// detached copy from RetryPolicyForProvider.
type ResolvedRetryPolicy struct {
	Mode           RetryMode
	MaxRetries     int
	RetryableCodes []string
	InitialDelayMS int64
	MaxDelayMS     int64
	JitterRatio    float64
}

type RetryPolicyProvider interface {
	RetryPolicyForProvider(provider string) (ResolvedRetryPolicy, bool)
}

func CloneRetryPolicy(policy ResolvedRetryPolicy) ResolvedRetryPolicy {
	policy.RetryableCodes = append([]string(nil), policy.RetryableCodes...)
	return policy
}

func ValidateResolvedRetryPolicy(policy ResolvedRetryPolicy) error {
	if policy.Mode != RetryNormal && policy.Mode != RetryAlways {
		return errors.New(`harness: resolved retry policy mode must be "normal" or "always"`)
	}
	if policy.Mode == RetryNormal {
		if policy.MaxRetries < 0 {
			return errors.New("harness: resolved retry max retries must not be negative")
		}
		if len(policy.RetryableCodes) == 0 {
			return errors.New("harness: resolved retryable codes must not be empty")
		}
		seen := make(map[string]struct{}, len(policy.RetryableCodes))
		for _, code := range policy.RetryableCodes {
			if code == "" {
				return errors.New("harness: resolved retryable codes must contain only non-empty strings")
			}
			if _, duplicate := seen[code]; duplicate {
				return errors.New("harness: resolved retryable codes must not contain duplicates")
			}
			seen[code] = struct{}{}
		}
	}
	const maxTimerDelayMS = int64(math.MaxInt32)
	if policy.InitialDelayMS <= 0 || policy.InitialDelayMS > maxTimerDelayMS ||
		policy.MaxDelayMS <= 0 || policy.MaxDelayMS > maxTimerDelayMS || policy.InitialDelayMS > policy.MaxDelayMS {
		return errors.New("harness: resolved retry delays are invalid")
	}
	if math.IsNaN(policy.JitterRatio) || math.IsInf(policy.JitterRatio, 0) || policy.JitterRatio < 0 || policy.JitterRatio > 1 {
		return errors.New("harness: resolved retry jitter ratio must be between 0 and 1")
	}
	return nil
}

// ResolveRetryPolicy validates, defaults, and detaches one provider policy.
func ResolveRetryPolicy(config *RetryPolicyConfig) (ResolvedRetryPolicy, error) {
	resolved := ResolvedRetryPolicy{
		Mode: RetryNormal, MaxRetries: 5,
		RetryableCodes: append([]string(nil), defaultRetryableCodes...),
		InitialDelayMS: 500, MaxDelayMS: 10_000, JitterRatio: 0.1,
	}
	if config == nil {
		return resolved, nil
	}
	if config.Mode != RetryNormal && config.Mode != RetryAlways {
		return ResolvedRetryPolicy{}, errors.New(`harness: retry policy mode must be "normal" or "always"`)
	}
	resolved.Mode = config.Mode
	if config.Mode == RetryNormal {
		if config.MaxRetries != nil {
			if *config.MaxRetries < 0 {
				return ResolvedRetryPolicy{}, errors.New("harness: retry policy max retries must not be negative")
			}
			resolved.MaxRetries = *config.MaxRetries
		}
		if config.RetryableCodes != nil {
			if len(config.RetryableCodes) == 0 {
				return ResolvedRetryPolicy{}, errors.New("harness: retryable codes must not be empty")
			}
			seen := make(map[string]struct{}, len(config.RetryableCodes))
			resolved.RetryableCodes = make([]string, len(config.RetryableCodes))
			for index, code := range config.RetryableCodes {
				if code == "" {
					return ResolvedRetryPolicy{}, errors.New("harness: retryable codes must contain only non-empty strings")
				}
				if _, duplicate := seen[code]; duplicate {
					return ResolvedRetryPolicy{}, errors.New("harness: retryable codes must not contain duplicates")
				}
				seen[code] = struct{}{}
				resolved.RetryableCodes[index] = code
			}
		}
	}
	if config.Backoff != nil {
		if config.Backoff.InitialDelayMS != nil {
			resolved.InitialDelayMS = *config.Backoff.InitialDelayMS
		}
		if config.Backoff.MaxDelayMS != nil {
			resolved.MaxDelayMS = *config.Backoff.MaxDelayMS
		}
		if config.Backoff.JitterRatio != nil {
			resolved.JitterRatio = *config.Backoff.JitterRatio
		}
	}
	const maxTimerDelayMS = int64(math.MaxInt32)
	if resolved.InitialDelayMS <= 0 || resolved.InitialDelayMS > maxTimerDelayMS {
		return ResolvedRetryPolicy{}, fmt.Errorf("harness: retry initial delay must be from 1 through %d milliseconds", maxTimerDelayMS)
	}
	if resolved.MaxDelayMS <= 0 || resolved.MaxDelayMS > maxTimerDelayMS {
		return ResolvedRetryPolicy{}, fmt.Errorf("harness: retry maximum delay must be from 1 through %d milliseconds", maxTimerDelayMS)
	}
	if resolved.InitialDelayMS > resolved.MaxDelayMS {
		return ResolvedRetryPolicy{}, errors.New("harness: retry initial delay must not exceed maximum delay")
	}
	if math.IsNaN(resolved.JitterRatio) || math.IsInf(resolved.JitterRatio, 0) || resolved.JitterRatio < 0 || resolved.JitterRatio > 1 {
		return ResolvedRetryPolicy{}, errors.New("harness: retry jitter ratio must be between 0 and 1")
	}
	if resolved.Mode == RetryAlways {
		resolved.MaxRetries = 0
		resolved.RetryableCodes = nil
	}
	return resolved, nil
}
