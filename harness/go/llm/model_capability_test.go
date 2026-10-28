package llm

import "testing"

func TestPreparedModelConversationCapabilitiesRejectUnknownModes(t *testing.T) {
	for _, info := range []ModelInfo{{}, {SystemPromptUpdate: "in-history"}, {ToolUpdate: "addition-only"}, {ToolUpdate: "in-history"}} {
		if err := ValidateModelReasoningInfo(info); err != nil {
			t.Fatalf("valid capability rejected: %+v %v", info, err)
		}
	}
	for _, info := range []ModelInfo{{SystemPromptUpdate: "arbitrary"}, {ToolUpdate: "all"}} {
		err := ValidateModelReasoningInfo(info)
		if err == nil || ProviderFailureFromError(err).Code != "INVALID_MODEL_INFO" {
			t.Fatalf("invalid capability accepted or lost domain code: %+v %v", info, err)
		}
	}
}
