package tool

import (
	"context"
	"errors"

	harnessmodel "github.com/phodal/pero/harness/go/model"
	point "github.com/phodal/pero/harness/go/runtime"
)

const DefaultSpillMaxInlineBytes = 50_000

// spillCharsPerToken is DSH token-meter CHARS_PER_TOKEN. Base's
// maxInlineTokens budget is applied to the byte comparator at this density.
// A 12500 token cap is 50000 bytes, the same budget as DefaultSpillMaxInlineBytes.
// Block overhead and image token pricing stay outside this comparator.
const spillCharsPerToken = 4

type SpillOwner struct {
	SessionID harnessmodel.SessionID
}

type SpillSource struct {
	ToolName string
	CallID   harnessmodel.ToolCallID
	Label    string
}

type SaveTextSpill struct {
	Owner         SpillOwner
	Source        SpillSource
	SuggestedName string
	Content       string
}

type SpillRef struct {
	Locator       string
	Bytes         int64
	RetrievalHint string
}

type SpillStore interface {
	SaveText(context.Context, SaveTextSpill) (SpillRef, error)
}

// SpillStoreLifecycle owns backend startup work that must quiesce on effect disposal.
type SpillStoreLifecycle interface {
	StartSpillLifecycle(context.Context) (point.Dispose, error)
}

type SpillTextReader interface {
	LookupText(context.Context, SpillOwner, string, int) (SpillTextLookup, error)
}

type SpillTextRead struct {
	Text      string
	Truncated bool
}

type SpillTextLookup struct {
	Recognized bool
	Read       *SpillTextRead
}

type SpillPolicyOptions struct {
	MaxInlineBytes int
}

// SpillPolicyConfig preserves DSH's distinction between an omitted cap (the
// policy registers no transformer) and an explicit zero cap. maxInlineTokens
// is the current Base field; maxInlineBytes remains the comparator budget.
type SpillPolicyConfig struct {
	MaxInlineBytes  *int `json:"maxInlineBytes,omitempty"`
	MaxInlineTokens *int `json:"maxInlineTokens,omitempty"`
}

// ValidateSpillPolicyConfig checks a DSH-shaped profile policy without
// changing any registered backend or visible generation.
func ValidateSpillPolicyConfig(config SpillPolicyConfig) error {
	if config.MaxInlineTokens != nil && config.MaxInlineBytes != nil {
		return errors.New("spill-policy: maxInlineTokens and maxInlineBytes cannot both be set")
	}
	if config.MaxInlineTokens != nil && *config.MaxInlineTokens < 0 {
		return errors.New("spill-policy: maxInlineTokens must be a non-negative integer")
	}
	if config.MaxInlineBytes != nil && *config.MaxInlineBytes < 0 {
		return errors.New("spill-policy: maxInlineBytes must be a non-negative integer")
	}
	return nil
}

// ResolveSpillPolicyConfig turns a Base maxInlineTokens cap into the byte
// budget the comparator already enforces. An omitted cap stays omitted.
func ResolveSpillPolicyConfig(config SpillPolicyConfig) (SpillPolicyConfig, error) {
	if err := ValidateSpillPolicyConfig(config); err != nil {
		return SpillPolicyConfig{}, err
	}
	if config.MaxInlineTokens == nil {
		return SpillPolicyConfig{MaxInlineBytes: config.MaxInlineBytes}, nil
	}
	bytes := *config.MaxInlineTokens * spillCharsPerToken
	return SpillPolicyConfig{MaxInlineBytes: &bytes}, nil
}
