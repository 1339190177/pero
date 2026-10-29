package session

import (
	"encoding/json"
	"fmt"
)

func isCompactionEventType(eventType string) bool {
	return eventType == "compaction/start" || eventType == "compaction/summary" || eventType == "compaction/end"
}

// InspectCompactionLog validates durable compaction brackets and reports
// whether the final bracket remains open.
func InspectCompactionLog(events []Event) (bool, error) {
	openID := ""
	openSourceCommandID := ""
	summarized := false
	summarySeq := uint64(0)
	replaced := false
	for _, event := range events {
		switch event.Type {
		case "compaction/start":
			var payload struct {
				CompactionID    string  `json:"compactionId"`
				SourceCommandID *string `json:"sourceCommandId,omitempty"`
			}
			if err := json.Unmarshal(event.Data, &payload); err != nil || payload.CompactionID == "" ||
				(payload.SourceCommandID != nil && *payload.SourceCommandID == "") {
				return false, fmt.Errorf("harness: invalid compaction/start at seq %d", event.Seq)
			}
			if openID != "" {
				return false, fmt.Errorf("harness: compaction/start at seq %d while %s is still open", event.Seq, openID)
			}
			openID = payload.CompactionID
			openSourceCommandID = stringValue(payload.SourceCommandID)
			summarized = false
			summarySeq = 0
			replaced = false
		case "compaction/summary":
			var payload struct {
				CompactionID    string  `json:"compactionId"`
				SourceCommandID *string `json:"sourceCommandId,omitempty"`
			}
			if err := json.Unmarshal(event.Data, &payload); err != nil || payload.CompactionID == "" ||
				(payload.SourceCommandID != nil && *payload.SourceCommandID == "") {
				return false, fmt.Errorf("harness: invalid compaction/summary at seq %d", event.Seq)
			}
			if openID == "" || payload.CompactionID != openID ||
				stringValue(payload.SourceCommandID) != openSourceCommandID || summarized {
				return false, fmt.Errorf("harness: compaction/summary at seq %d does not match one unsummarized start", event.Seq)
			}
			summarized = true
			summarySeq = event.Seq
		case "compaction/end":
			var payload struct {
				CompactionID    string  `json:"compactionId"`
				SourceCommandID *string `json:"sourceCommandId,omitempty"`
				Error           string  `json:"error,omitempty"`
			}
			if err := json.Unmarshal(event.Data, &payload); err != nil || payload.CompactionID == "" ||
				(payload.SourceCommandID != nil && *payload.SourceCommandID == "") {
				return false, fmt.Errorf("harness: invalid compaction/end at seq %d", event.Seq)
			}
			if openID == "" || payload.CompactionID != openID || stringValue(payload.SourceCommandID) != openSourceCommandID {
				return false, fmt.Errorf("harness: compaction/end at seq %d does not match an open start", event.Seq)
			}
			if payload.Error == "" && (!summarized || !replaced) {
				return false, fmt.Errorf("harness: successful compaction/end at seq %d has no committed summary replacement", event.Seq)
			}
			openID = ""
			openSourceCommandID = ""
			summarized = false
			summarySeq = 0
			replaced = false
		default:
			if openID != "" && summarized && isSurfaceEventType(event.Type) && citesSource(event, summarySeq) {
				var operation struct {
					Op string `json:"op"`
				}
				if json.Unmarshal(event.SurfaceOp, &operation) == nil && operation.Op == "replace" {
					var checkpoint struct {
						Source *struct {
							Kind            string `json:"kind"`
							Plugin          string `json:"plugin"`
							CompactionID    string `json:"compactionId"`
							SourceCommandID string `json:"sourceCommandId,omitempty"`
						} `json:"source,omitempty"`
					}
					if err := json.Unmarshal(event.Data, &checkpoint); err != nil {
						return false, fmt.Errorf("harness: invalid compaction checkpoint at seq %d", event.Seq)
					}
					if checkpoint.Source != nil && (checkpoint.Source.Kind != "plugin" || checkpoint.Source.Plugin != "compact" ||
						checkpoint.Source.CompactionID != openID || checkpoint.Source.SourceCommandID != openSourceCommandID) {
						return false, fmt.Errorf("harness: compaction checkpoint at seq %d has mismatched provenance", event.Seq)
					}
					if openSourceCommandID != "" && checkpoint.Source == nil {
						return false, fmt.Errorf("harness: compaction checkpoint at seq %d has no command provenance", event.Seq)
					}
					replaced = true
				}
			}
		}
	}
	return openID != "", nil
}

func inspectCompactionLog(events []Event) (bool, error) { return InspectCompactionLog(events) }

// InspectOpenTurn validates durable turn brackets and returns the open turn.
func InspectOpenTurn(events []Event) (*int, error) {
	var open *int
	for _, event := range events {
		if event.Type != "turn/start" && event.Type != "turn/end" {
			continue
		}
		var payload struct {
			Turn int `json:"turn"`
		}
		if err := json.Unmarshal(event.Data, &payload); err != nil || payload.Turn <= 0 {
			return nil, fmt.Errorf("harness: invalid %s at seq %d", event.Type, event.Seq)
		}
		if event.Type == "turn/start" {
			if open != nil {
				return nil, fmt.Errorf("harness: turn/start at seq %d while turn %d is still open", event.Seq, *open)
			}
			value := payload.Turn
			open = &value
			continue
		}
		if open == nil || *open != payload.Turn {
			return nil, fmt.Errorf("harness: turn/end at seq %d does not match an open turn", event.Seq)
		}
		open = nil
	}
	return open, nil
}

func inspectOpenTurn(events []Event) (*int, error) { return InspectOpenTurn(events) }

func citesSource(event Event, source uint64) bool {
	for _, seq := range event.SourceEventSeqs {
		if seq == source {
			return true
		}
	}
	return false
}

func stringValue(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}
