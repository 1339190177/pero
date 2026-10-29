package session

import (
	"encoding/json"
	"fmt"
	"github.com/phodal/pero/harness/go/model"
)

// validateDeveloperMessageEvent checks local role/block constraints and resolves
// additions only against an earlier committed request header. It runs before
// both live publication and cold replay mutation.
func validateDeveloperMessageEvent(events []Event, event Event) error {
	if !isSurfaceEventType(event.Type) {
		return nil
	}
	var data map[string]json.RawMessage
	if err := json.Unmarshal(event.Data, &data); err != nil {
		return fmt.Errorf("harness: invalid surface message: %w", err)
	}
	raw := event.Data
	if event.Type != "user/message" {
		raw = data["message"]
	}
	if event.Type == "tool/result" {
		raw = event.Data
	}
	var msg struct {
		ID      string            `json:"id"`
		Role    string            `json:"role"`
		Content []json.RawMessage `json:"content"`
	}
	if err := json.Unmarshal(raw, &msg); err != nil {
		return fmt.Errorf("harness: invalid surface message: %w", err)
	}
	developer := event.Type == "developer/message"
	if developer != (msg.Role == string(model.RoleDeveloper)) {
		return fmt.Errorf("harness: developer/message and developer role must occur together")
	}
	if developer && msg.ID == "" {
		return fmt.Errorf("harness: developer/message requires an identified message")
	}
	additions := []string{}
	for _, rawBlock := range msg.Content {
		var block map[string]json.RawMessage
		if err := json.Unmarshal(rawBlock, &block); err != nil {
			return fmt.Errorf("harness: invalid message content: %w", err)
		}
		var kind, name string
		_ = json.Unmarshal(block["type"], &kind)
		if kind != "tool-addition" && kind != "tool-removal" {
			continue
		}
		if !developer {
			return fmt.Errorf("harness: tool-change blocks require developer role")
		}
		if json.Unmarshal(block["toolName"], &name) != nil || name == "" {
			return fmt.Errorf("harness: %s requires nonempty toolName", kind)
		}
		if kind == "tool-addition" {
			if _, inline := block["tool"]; inline {
				return fmt.Errorf("harness: tool-addition must omit inline tool definition")
			}
			additions = append(additions, name)
		}
	}
	if !developer {
		return nil
	}
	headerRaw, hasHeader := data["headerSeq"]
	if hasHeader != (len(additions) > 0) {
		return fmt.Errorf("harness: developer/message requires headerSeq exactly when additions are present")
	}
	if !hasHeader {
		return nil
	}
	var seq uint64
	if json.Unmarshal(headerRaw, &seq) != nil || seq == 0 || seq >= event.Seq || seq > uint64(len(events)) || events[seq-1].Type != "request/header" {
		return fmt.Errorf("harness: developer/message headerSeq must reference an earlier request/header")
	}
	var header struct {
		Tools []map[string]json.RawMessage `json:"tools"`
	}
	if err := json.Unmarshal(events[seq-1].Data, &header); err != nil {
		return err
	}
	for _, name := range additions {
		var matched map[string]json.RawMessage
		count := 0
		for _, tool := range header.Tools {
			var toolName string
			_ = json.Unmarshal(tool["name"], &toolName)
			if toolName == name {
				count++
				matched = tool
			}
		}
		if count != 1 {
			return fmt.Errorf("harness: developer/message addition %q must name exactly one tool in header %d", name, seq)
		}
		// Go's canonical ToolSchema omits an empty description. Its zero value is
		// the string "", which the bridge maps to the complete DSH declaration.
		var description string
		if value, ok := matched["description"]; ok && json.Unmarshal(value, &description) != nil {
			return fmt.Errorf("harness: tool description must be a string")
		}
		var schema map[string]json.RawMessage
		if json.Unmarshal(matched["inputSchema"], &schema) != nil || schema == nil {
			return fmt.Errorf("harness: addition %q requires complete tool inputSchema", name)
		}
		if value, ok := matched["deferLoading"]; ok && string(value) != "true" {
			return fmt.Errorf("harness: referenced deferLoading must be true when present")
		}
	}
	return nil
}
