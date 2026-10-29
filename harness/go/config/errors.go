package config

import harnessllm "github.com/phodal/pero/harness/go/llm"

func NewProviderError(message, code string, cause error) *harnessllm.ProviderError {
	return harnessllm.NewProviderError(message, code, cause)
}
