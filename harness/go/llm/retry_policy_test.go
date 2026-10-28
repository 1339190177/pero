package llm

import (
	"reflect"
	"testing"
)

func TestResolveRetryPolicyMatchesDSHDefaultsAndValidation(t *testing.T) {
	policy, err := ResolveRetryPolicy(nil)
	if err != nil {
		t.Fatal(err)
	}
	if policy.Mode != RetryNormal || policy.MaxRetries != 5 || policy.InitialDelayMS != 500 ||
		policy.MaxDelayMS != 10_000 || policy.JitterRatio != 0.1 ||
		!reflect.DeepEqual(policy.RetryableCodes, []string{EmptyResponseCode, MalformedResponseCode, "RATE_LIMIT", "SERVER", "TIMEOUT", TransportErrorCode}) {
		t.Fatalf("default policy = %+v", policy)
	}
	zero := int64(0)
	if _, err := ResolveRetryPolicy(&RetryPolicyConfig{Mode: RetryNormal, Backoff: &RetryBackoffConfig{InitialDelayMS: &zero}}); err == nil {
		t.Fatal("zero initial delay was accepted")
	}
	duplicate := []string{"SERVER", "SERVER"}
	if _, err := ResolveRetryPolicy(&RetryPolicyConfig{Mode: RetryNormal, RetryableCodes: duplicate}); err == nil {
		t.Fatal("duplicate retryable code was accepted")
	}
	if _, err := ResolveRetryPolicy(&RetryPolicyConfig{}); err == nil {
		t.Fatal("missing mode was accepted")
	}
}

func TestExplicitRetryableCodesReplaceDefaults(t *testing.T) {
	// An explicit provider policy owns its code list exactly. Omitting
	// MALFORMED_RESPONSE must keep omitting it rather than silently merging the
	// new default back in.
	maxRetries := 1
	policy, err := ResolveRetryPolicy(&RetryPolicyConfig{
		Mode: RetryNormal, MaxRetries: &maxRetries, RetryableCodes: []string{"SERVER"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(policy.RetryableCodes, []string{"SERVER"}) {
		t.Fatalf("explicit codes = %v", policy.RetryableCodes)
	}
	defaults, err := ResolveRetryPolicy(nil)
	if err != nil {
		t.Fatal(err)
	}
	explicit, err := ResolveRetryPolicy(&RetryPolicyConfig{
		Mode: RetryNormal, RetryableCodes: []string{EmptyResponseCode, "RATE_LIMIT"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !contains(defaults.RetryableCodes, MalformedResponseCode) {
		t.Fatalf("default codes lost %s: %v", MalformedResponseCode, defaults.RetryableCodes)
	}
	if contains(explicit.RetryableCodes, MalformedResponseCode) {
		t.Fatalf("explicit codes gained %s: %v", MalformedResponseCode, explicit.RetryableCodes)
	}
}

func contains(codes []string, code string) bool {
	for _, candidate := range codes {
		if candidate == code {
			return true
		}
	}
	return false
}

func TestCloneRetryPolicyDetachesCodes(t *testing.T) {
	source := ResolvedRetryPolicy{Mode: RetryNormal, RetryableCodes: []string{"SERVER"}}
	cloned := CloneRetryPolicy(source)
	cloned.RetryableCodes[0] = "TIMEOUT"
	if source.RetryableCodes[0] != "SERVER" {
		t.Fatalf("source mutated: %+v", source)
	}
}
