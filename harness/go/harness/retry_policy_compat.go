package harness

import harnessllm "github.com/phodal/pero/harness/go/llm"

type RetryMode = harnessllm.RetryMode

const (
	RetryNormal = harnessllm.RetryNormal
	RetryAlways = harnessllm.RetryAlways
)

type RetryBackoffConfig = harnessllm.RetryBackoffConfig
type RetryPolicyConfig = harnessllm.RetryPolicyConfig
type ResolvedRetryPolicy = harnessllm.ResolvedRetryPolicy
type RetryPolicyProvider = harnessllm.RetryPolicyProvider

func cloneRetryPolicy(policy ResolvedRetryPolicy) ResolvedRetryPolicy {
	return harnessllm.CloneRetryPolicy(policy)
}

func ValidateResolvedRetryPolicy(policy ResolvedRetryPolicy) error {
	return harnessllm.ValidateResolvedRetryPolicy(policy)
}

func ResolveRetryPolicy(config *RetryPolicyConfig) (ResolvedRetryPolicy, error) {
	return harnessllm.ResolveRetryPolicy(config)
}
