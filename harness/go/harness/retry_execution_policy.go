package harness

import harnessllm "github.com/phodal/pero/harness/go/llm"

type RetryExecutionPolicySnapshot = harnessllm.RetryExecutionPolicySnapshot
type RetryExecutionPolicy = harnessllm.RetryExecutionPolicy

func NewRetryExecutionPolicy() *RetryExecutionPolicy { return harnessllm.NewRetryExecutionPolicy() }
