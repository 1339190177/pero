package main

import (
	"flag"
	"github.com/phodal/pero/harness/go/providers/openai"
	"testing"
)

func TestExplicitModelCatalogSharesValidatedCompactionCapacity(t *testing.T) {
	fs := flag.NewFlagSet("fixture", flag.ContinueOnError)
	ids := bindSelectableModels(fs)
	if err := fs.Parse([]string{"--selectable-model=b", "--selectable-model=c"}); err != nil {
		t.Fatal(err)
	}
	config := openai.Config{Models: []openai.ModelConfig{{ID: "a", MaxTokens: 128}}}
	comp, err := configuredCompaction(&config, compactionFlags{ContextWindow: 8192, Threshold: .8, Retain: .16})
	if err != nil {
		t.Fatal(err)
	}
	if err := configureSelectableModels(&config, *ids); err != nil {
		t.Fatal(err)
	}
	if comp == nil || comp.ContextWindow != 0 || len(config.Models) != 3 {
		t.Fatal(config, comp)
	}
	for _, m := range config.Models {
		if m.ContextWindow != 8192 || m.MaxTokens != 128 {
			t.Fatal(m)
		}
	}
}
func TestExplicitModelCatalogRejectsInvalidWithoutMutation(t *testing.T) {
	for _, ids := range [][]string{{"a"}, {"b", "b"}, {""}, {"a b"}} {
		c := openai.Config{Models: []openai.ModelConfig{{ID: "a"}}}
		if configureSelectableModels(&c, ids) == nil || len(c.Models) != 1 {
			t.Fatal(ids, c)
		}
	}
	c := openai.Config{Models: []openai.ModelConfig{{ID: "a"}}}
	if err := configureSelectableModels(&c, nil); err != nil || len(c.Models) != 1 {
		t.Fatal(err, c)
	}
}
