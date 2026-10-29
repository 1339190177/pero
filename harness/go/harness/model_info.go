package harness

import harnessllm "github.com/phodal/pero/harness/go/llm"

type ModelInfo = harnessllm.ModelInfo
type ModelReasoningInfo = harnessllm.ModelReasoningInfo
type ReasoningEffortInfo = harnessllm.ReasoningEffortInfo
type ModelInfoResolver = harnessllm.ModelInfoResolver
type ModelInfoResolverFunc = harnessllm.ModelInfoResolverFunc

// ValidateReasoningEffort rejects only when the adapter published an
// authoritative effort catalog. Unknown metadata remains provider-owned.
func ValidateReasoningEffort(info ModelInfo, effort ReasoningEffort) error {
	return harnessllm.ValidateReasoningEffort(info, effort)
}

func validateModelReasoningInfo(info ModelInfo) error {
	return harnessllm.ValidateModelReasoningInfo(info)
}

func cloneModelInfo(info ModelInfo) ModelInfo {
	return harnessllm.CloneModelInfo(info)
}
