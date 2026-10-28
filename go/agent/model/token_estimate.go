package model

import "encoding/json"

const (
	tokenCharactersPerToken = 4
	tokenBlockOverhead      = 4
	tokenRoleOverhead       = 4
)

// EstimateMessageTokens applies the fixed-density, UTF-16-based estimate used
// by the Harness and DSH projections. It does not incorporate provider usage
// anchors or Session state.
func EstimateMessageTokens(message Message) int {
	if message.Role == RoleSystem {
		if len(message.Content) == 0 {
			return 0
		}
		characters := 0
		for _, block := range message.Content {
			if block.Type == BlockText {
				characters += stringCodeUnits(block.Text)
			} else if raw, err := json.Marshal(block); err == nil {
				characters += stringCodeUnits(string(raw))
			}
		}
		return divideTokens(characters) + tokenRoleOverhead
	}
	if message.Role == RoleTool {
		// Native logs retain the result wrapper; current DSH ToolMessage and
		// provider requests carry its content directly, with no wrapper price.
		content := make([]ContentBlock, 0, len(message.Content))
		for _, block := range message.Content {
			if block.Type == BlockToolResult {
				content = append(content, block.Content...)
			} else {
				content = append(content, block)
			}
		}
		return estimateContentTokens(content) + tokenRoleOverhead
	}
	return estimateContentTokens(message.Content) + tokenRoleOverhead
}

func estimateContentTokens(blocks []ContentBlock) int {
	tokens := 0
	for _, block := range blocks {
		switch block.Type {
		case BlockText, BlockReasoning:
			tokens += divideTokens(stringCodeUnits(block.Text)) + tokenBlockOverhead
		case BlockToolCall:
			tokens += divideTokens(stringCodeUnits(block.Name)) +
				divideTokens(stringCodeUnits(string(block.Arguments))) + tokenBlockOverhead
		case BlockToolResult:
			tokens += estimateContentTokens(block.Content) + tokenBlockOverhead
		case BlockImage:
			// Image offload marks do not change this reference-only heuristic.
			// Route pricing owns their placeholders.
			block.Offloaded = false
			if encoded, err := json.Marshal(block); err == nil {
				tokens += divideTokens(stringCodeUnits(string(encoded))) + tokenBlockOverhead
			}
		default:
			if encoded, err := json.Marshal(block); err == nil {
				tokens += divideTokens(stringCodeUnits(string(encoded))) + tokenBlockOverhead
			}
		}
	}
	return tokens
}

// stringCodeUnits mirrors JavaScript String.length so DSH and Go price
// non-ASCII content under the same UTF-16 density without allocating.
func stringCodeUnits(value string) int {
	units := 0
	for _, character := range value {
		units++
		if character > 0xffff {
			units++
		}
	}
	return units
}

func divideTokens(characters int) int {
	return (characters + tokenCharactersPerToken - 1) / tokenCharactersPerToken
}
