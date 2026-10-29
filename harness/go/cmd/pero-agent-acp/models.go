package main

import (
	"errors"
	"flag"
	"github.com/phodal/pero/harness/go/providers/openai"
	"strings"
)

func bindSelectableModels(flags *flag.FlagSet) *[]string {
	models := new([]string)
	flags.Func("selectable-model", "additional explicit model ID (repeat, max 32); all share --context-window, output and reasoning settings, which the operator must ensure every model supports", func(value string) error {
		if strings.TrimSpace(value) == "" || len(value) > 256 || strings.ContainsAny(value, "\x00\r\n\t ") || len(*models) >= 32 {
			return errors.New("selectable-model requires a model identifier (max 256 bytes, 32 additional models)")
		}
		*models = append(*models, value)
		return nil
	})
	return models
}

// Run after configuredCompaction has validated and declared the shared capacity.
// This is one adapter's explicit catalog, not new providers or persisted settings.
func configureSelectableModels(config *openai.Config, models []string) error {
	if len(models) == 0 {
		return nil
	}
	if config == nil || len(config.Models) != 1 {
		return errors.New("selectable models require one configured default")
	}
	seen := map[string]bool{config.Models[0].ID: true}
	for _, id := range models {
		if strings.TrimSpace(id) == "" || len(id) > 256 || strings.ContainsAny(id, "\x00\r\n\t ") || seen[id] || len(models) > 32 {
			return errors.New("invalid or duplicate selectable model")
		}
		seen[id] = true
	}
	for _, id := range models {
		entry := config.Models[0]
		entry.ID = id
		entry.Name = id
		entry.Description = ""
		config.Models = append(config.Models, entry)
	}
	return nil
}
