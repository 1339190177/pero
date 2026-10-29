package harness

import (
	"encoding/json"
	"reflect"
	"strings"
)

// ToolErrorOutputMode chooses passive synchronization of accepted error
// presentation. It does not change admission, execute callbacks or grant rights.
type ToolErrorOutputMode uint8

const (
	ErrorOutputLegacy ToolErrorOutputMode = iota
	ErrorOutputDSHAlpha3
)

// normalizeToolErrorOutputAfter uses the real pre-policy snapshot, not a
// coincidental match between current text and metadata. The exact selected
// Tool supplies request.output; names, request Meta and JSON cannot opt in.
func normalizeToolErrorOutputAfter(request ToolRequest, before, after ToolResult) ToolResult {
	if request.output == nil || request.output.errorMode != ErrorOutputDSHAlpha3 || !after.IsError {
		return after
	}
	after.Value, after.ConcludesTurn = nil, false
	if reflect.DeepEqual(before.Content, after.Content) {
		return after
	}
	// The final policy may explicitly remove the whole Meta object or only its
	// message. Never recreate removed fields or copy anything from before.Meta.
	var meta map[string]json.RawMessage
	if json.Unmarshal(after.Meta, &meta) != nil || meta == nil {
		return after
	}
	if _, exists := meta["message"]; !exists {
		return after
	}
	texts := make([]string, 0, len(after.Content))
	for _, block := range after.Content {
		if block.Type == BlockText {
			texts = append(texts, block.Text)
		}
	}
	// Empty content is valid redaction, including a nil or explicit empty slice.
	// An empty message is deliberate, never a request to fall back to old data.
	meta["message"], _ = json.Marshal(strings.Join(texts, "\n"))
	after.Meta, _ = json.Marshal(meta)
	return after
}

// ErrorOutputMode reports the private output contract of this admitted request
// to trusted in-process adapters. A constructed/model-supplied request has none.
func (request ToolRequest) ErrorOutputMode() ToolErrorOutputMode {
	if !request.executionToken.Valid() || request.output == nil {
		return ErrorOutputLegacy
	}
	return request.output.errorMode
}
