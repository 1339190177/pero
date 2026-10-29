package tool

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"

	harnessmodel "github.com/phodal/pero/harness/go/model"
	harnesssession "github.com/phodal/pero/harness/go/session"
	point "github.com/phodal/pero/harness/go/runtime"
)

const ToolResultPruneMarker = "\n\n[... tool result middle pruned ...]\n\n"

var ResultPrunerPolicyKey = point.NewKey[*ToolResultPrunerPolicy]("harness.tool-result-pruner.policy")

type ToolResultPruneConfig struct {
	ThresholdChars *int `json:"thresholdChars,omitempty"`
	HeadChars      *int `json:"headChars,omitempty"`
	TailChars      *int `json:"tailChars,omitempty"`
}

type resolvedToolResultPruneConfig struct {
	thresholdChars int
	headChars      int
	tailChars      int
	marker         string
}

type PrunedToolResult struct {
	OriginalSeq    uint64                  `json:"originalSeq"`
	ReplacementSeq uint64                  `json:"replacementSeq"`
	ToolCallID     harnessmodel.ToolCallID `json:"toolCallId"`
	CharsBefore    int                     `json:"charsBefore"`
	CharsAfter     int                     `json:"charsAfter"`
}

type ToolResultPruneResult struct {
	Pruned       []PrunedToolResult `json:"pruned"`
	CharsRemoved int                `json:"charsRemoved"`
}

type ToolResultPruner struct {
	config resolvedToolResultPruneConfig
}

type toolResultPrunerOverride struct {
	id     uint64
	pruner *ToolResultPruner
	active bool
}

// ToolResultPrunerPolicy owns root-scoped profile overrides for the
// model-free tool-result pruner. An active nil pruner is intentional: it
// disables pruning even when an Agent has an explicit local configuration.
// With no active override, each Agent keeps using its own mounted pruner.
type ToolResultPrunerPolicy struct {
	mu             sync.RWMutex
	nextOverrideID uint64
	overrides      []*toolResultPrunerOverride
	effective      *ToolResultPruner
	overridden     bool
}

type ToolResultPrunerPolicySnapshot struct {
	Overridden     bool `json:"overridden"`
	Enabled        bool `json:"enabled"`
	ThresholdChars int  `json:"thresholdChars,omitempty"`
	HeadChars      int  `json:"headChars,omitempty"`
	TailChars      int  `json:"tailChars,omitempty"`
}

// Snapshot reports the immutable effective generation without exposing its
// executable pruner. It is suitable for diagnostics and reload barriers.
func (p *ToolResultPrunerPolicy) Snapshot() ToolResultPrunerPolicySnapshot {
	pruner, overridden := p.Effective()
	snapshot := ToolResultPrunerPolicySnapshot{Overridden: overridden, Enabled: overridden && pruner != nil}
	if pruner != nil {
		snapshot.ThresholdChars = pruner.config.thresholdChars
		snapshot.HeadChars = pruner.config.headChars
		snapshot.TailChars = pruner.config.tailChars
	}
	return snapshot
}

// Override installs one higher-priority profile policy until the returned
// disposer is called. The candidate is validated before the visible policy is
// changed, so a failed live reload retains the last-good generation.
func (p *ToolResultPrunerPolicy) Override(config ToolResultPruneConfig, enabled bool) (point.Dispose, error) {
	if p == nil {
		return nil, errors.New("harness: tool-result pruner policy is nil")
	}
	var next *ToolResultPruner
	var err error
	if enabled {
		next, err = NewToolResultPruner(config)
		if err != nil {
			return nil, err
		}
	}
	p.mu.Lock()
	p.nextOverrideID++
	override := &toolResultPrunerOverride{id: p.nextOverrideID, pruner: next, active: true}
	p.overrides = append(p.overrides, override)
	p.effective = next
	p.overridden = true
	p.mu.Unlock()
	var once sync.Once
	return func(context.Context) error {
		once.Do(func() {
			p.mu.Lock()
			override.active = false
			for len(p.overrides) > 0 && !p.overrides[len(p.overrides)-1].active {
				p.overrides = p.overrides[:len(p.overrides)-1]
			}
			p.effective = nil
			p.overridden = false
			if len(p.overrides) > 0 {
				p.effective = p.overrides[len(p.overrides)-1].pruner
				p.overridden = true
			}
			p.mu.Unlock()
		})
		return nil
	}, nil
}

// Effective returns the active profile override and whether one is installed.
// A nil pruner with overridden=true intentionally disables local pruning.
func (p *ToolResultPrunerPolicy) Effective() (*ToolResultPruner, bool) {
	if p == nil {
		return nil, false
	}
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.effective, p.overridden
}

func NewToolResultPruner(config ToolResultPruneConfig) (*ToolResultPruner, error) {
	resolved := resolvedToolResultPruneConfig{
		thresholdChars: 8192,
		headChars:      4096,
		tailChars:      1024,
		marker:         ToolResultPruneMarker,
	}
	if config.ThresholdChars != nil {
		resolved.thresholdChars = *config.ThresholdChars
	}
	if config.HeadChars != nil {
		resolved.headChars = *config.HeadChars
	}
	if config.TailChars != nil {
		resolved.tailChars = *config.TailChars
	}
	if resolved.thresholdChars <= 0 || resolved.headChars < 0 || resolved.tailChars < 0 {
		return nil, errors.New("harness: invalid tool-result prune character limits")
	}
	if resolved.headChars+len([]rune(resolved.marker))+resolved.tailChars > resolved.thresholdChars {
		return nil, errors.New("harness: tool-result prune head, marker, and tail exceed threshold")
	}
	return &ToolResultPruner{config: resolved}, nil
}

func (p *ToolResultPruner) MeasureContent(content []harnessmodel.ContentBlock) int {
	if p == nil {
		return 0
	}
	total := 0
	for _, block := range content {
		if block.Type == harnessmodel.BlockText {
			total += len([]rune(block.Text))
		}
	}
	return total
}

func (p *ToolResultPruner) PruneContent(content []harnessmodel.ContentBlock) ([]harnessmodel.ContentBlock, bool, error) {
	if p == nil {
		return nil, false, errors.New("harness: tool-result pruner is nil")
	}
	total := p.MeasureContent(content)
	if total <= p.config.thresholdChars {
		return harnessmodel.CloneContentBlocks(content), false, nil
	}
	headRemaining, tailStart := p.config.headChars, total-p.config.tailChars
	textOffset, markerWritten := 0, false
	result := make([]harnessmodel.ContentBlock, 0, len(content)+1)
	for _, source := range content {
		if source.Type != harnessmodel.BlockText {
			result = append(result, harnessmodel.CloneContentBlock(source))
			continue
		}
		characters := []rune(source.Text)
		blockStart, blockEnd := textOffset, textOffset+len(characters)
		keepHead := min(len(characters), max(0, headRemaining-blockStart))
		keepTailFrom := max(0, tailStart-blockStart)
		if keepTailFrom > len(characters) {
			keepTailFrom = len(characters)
		}
		removedStart, removedEnd := blockStart+keepHead, blockStart+keepTailFrom
		marker := ""
		if removedEnd > removedStart && !markerWritten {
			marker = p.config.marker
			markerWritten = true
		}
		text := string(characters[:keepHead]) + marker + string(characters[keepTailFrom:])
		if text != "" {
			block := harnessmodel.CloneContentBlock(source)
			block.Text = text
			result = append(result, block)
		}
		textOffset = blockEnd
	}
	if !markerWritten {
		return nil, false, errors.New("harness: over-budget tool result produced no prunable span")
	}
	after := p.MeasureContent(result)
	if after > p.config.thresholdChars || after >= total {
		return nil, false, errors.New("harness: tool-result replacement must shrink within threshold")
	}
	return result, true, nil
}

func (p *ToolResultPruner) PruneSession(session *harnesssession.Session) (ToolResultPruneResult, error) {
	if p == nil || session == nil {
		return ToolResultPruneResult{}, errors.New("harness: tool-result pruning requires a pruner and session")
	}
	events := session.Events()
	if active, err := harnesssession.InspectCompactionLog(events); err != nil {
		return ToolResultPruneResult{}, err
	} else if active {
		return ToolResultPruneResult{}, errors.New("harness: tool-result pruning found an open compaction bracket")
	}
	if open, err := harnesssession.InspectOpenTurn(events); err != nil {
		return ToolResultPruneResult{}, err
	} else if open == nil {
		return ToolResultPruneResult{}, errors.New("harness: tool-result pruning requires an open turn")
	}
	eventsBySeq := make(map[uint64]harnessmodel.Event, len(events))
	for _, event := range events {
		eventsBySeq[event.Seq] = event
	}
	result := ToolResultPruneResult{}
	for _, seq := range session.SurfaceSeqs() {
		event, ok := eventsBySeq[seq]
		if !ok || event.Type != "tool/result" {
			continue
		}
		var block harnessmodel.ContentBlock
		if err := json.Unmarshal(event.Data, &block); err != nil {
			return result, fmt.Errorf("harness: decode tool/result at seq %d: %w", seq, err)
		}
		before := p.MeasureContent(block.Content)
		pruned, changed, err := p.PruneContent(block.Content)
		if err != nil {
			return result, fmt.Errorf("harness: prune tool/result at seq %d: %w", seq, err)
		}
		if !changed {
			continue
		}
		var raw map[string]json.RawMessage
		if err := json.Unmarshal(event.Data, &raw); err != nil {
			return result, fmt.Errorf("harness: preserve tool/result at seq %d: %w", seq, err)
		}
		content, err := json.Marshal(pruned)
		if err != nil {
			return result, err
		}
		raw["content"] = content
		replacement, err := json.Marshal(raw)
		if err != nil {
			return result, err
		}
		shadowedTokens := harnessmodel.EstimateMessageTokens(harnessmodel.Message{
			Role:    harnessmodel.RoleTool,
			Content: []harnessmodel.ContentBlock{block},
		})
		committed, err := session.AppendPrunedToolResult(seq, replacement, shadowedTokens)
		if err != nil {
			return result, err
		}
		after := p.MeasureContent(pruned)
		result.Pruned = append(result.Pruned, PrunedToolResult{
			OriginalSeq: seq, ReplacementSeq: committed.Seq, ToolCallID: block.ToolCallID,
			CharsBefore: before, CharsAfter: after,
		})
		result.CharsRemoved += before - after
	}
	return result, nil
}
