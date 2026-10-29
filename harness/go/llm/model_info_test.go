package llm

import "testing"

func TestValidateReasoningEffortUsesAuthoritativeMetadata(t *testing.T) {
	unknown := ModelInfo{Provider: "p", Model: "m"}
	if err := ValidateReasoningEffort(unknown, "custom"); err != nil {
		t.Fatalf("unknown metadata rejected provider-owned effort: %v", err)
	}
	known := ModelInfo{Provider: "p", Model: "m", Reasoning: &ModelReasoningInfo{
		Efforts: []ReasoningEffortInfo{{ID: "high", Name: "High"}},
	}}
	if err := ValidateReasoningEffort(known, "high"); err != nil {
		t.Fatal(err)
	}
	if err := ValidateReasoningEffort(known, "low"); err == nil {
		t.Fatal("unsupported effort was accepted")
	}
}

func TestCloneModelInfoDetachesNestedMetadata(t *testing.T) {
	source := ModelInfo{InputModalities: []string{"text"}, Reasoning: &ModelReasoningInfo{
		Efforts: []ReasoningEffortInfo{{ID: "high", Name: "High"}}, DefaultEffort: "high",
	}}
	cloned := CloneModelInfo(source)
	cloned.InputModalities[0] = "image"
	cloned.Reasoning.Efforts[0].Name = "Changed"
	if source.InputModalities[0] != "text" || source.Reasoning.Efforts[0].Name != "High" {
		t.Fatalf("source mutated: %+v", source)
	}
}
