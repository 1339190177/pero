package model

import (
	"encoding/json"
	"errors"
)

// ContentJSONError is a bounded, payload-free codec diagnostic. Its kind is
// privately constructed so callers cannot make an extension override fields.
type ContentJSONError struct{ code string }

func (e *ContentJSONError) Error() string         { return "content block JSON rejected: " + e.code }
func (e *ContentJSONError) ToolErrorCode() string { return e.code }
func contentJSONError(code string) error          { return &ContentJSONError{code: code} }

func contentJSONCause(err error) error {
	var codec *ContentJSONError
	if errors.As(err, &codec) {
		return codec
	}
	return contentJSONError("INVALID_CONTENT_JSON")
}

// blockFields describes only fields the discriminated core kind interprets.
// Unknown types deliberately have no executable or attachment-bearing fields.
func blockFields(kind BlockType) map[string]bool {
	fields := map[string]bool{"type": true}
	add := func(names ...string) {
		for _, name := range names {
			fields[name] = true
		}
	}
	switch kind {
	case BlockText, BlockReasoning:
		add("text", "meta")
	case BlockImage:
		add("attachment", "offloaded", "data", "mimeType", "meta")
	case BlockFile:
		add("attachment", "meta")
	case BlockToolCall:
		add("toolCallId", "name", "arguments", "argumentsDelta", "meta")
	case BlockToolResult:
		add("toolCallId", "content", "isError", "meta")
	case BlockToolAddition, BlockToolRemoval:
		add("toolName", "meta")
	}
	return fields
}

// MarshalJSON never decodes opaque values through float64. It reserves both the
// fields interpreted by this kind and every explicitly supplied typed field.
// Extensions cannot override the discriminator or any native authority field.
func (b ContentBlock) MarshalJSON() ([]byte, error) {
	if err := representableBlockStrings(b); err != nil {
		return nil, err
	}
	type plain ContentBlock
	var data []byte
	var err error
	if b.Type == BlockFile {
		data, err = json.Marshal(struct {
			plain
			Attachment *FileAttachmentRef `json:"attachment,omitempty"`
		}{plain(b), b.FileAttachment})
	} else {
		data, err = json.Marshal(plain(b))
	}
	if err != nil {
		return nil, contentJSONCause(err)
	}
	attachmentExtra := b.ownedAttachmentExtensions()
	if len(b.Extensions) == 0 && !b.typeAbsent && attachmentExtra == nil {
		return data, nil
	}
	var result map[string]json.RawMessage
	if err = json.Unmarshal(data, &result); err != nil {
		return nil, contentJSONError("INVALID_CONTENT_JSON")
	}
	if attachmentExtra != nil {
		result["attachment"], err = attachmentExtra.marshalAttachment(result["attachment"])
		if err != nil {
			return nil, err
		}
	}
	if b.typeAbsent && b.Type == "" {
		delete(result, "type")
	}
	reserved := blockFields(b.Type)
	for key, value := range b.Extensions {
		_, present := result[key]
		if reserved[key] || present {
			return nil, contentJSONError("CONTENT_FIELD_COLLISION")
		}
		if !json.Valid(value) {
			return nil, contentJSONError("INVALID_CONTENT_JSON")
		}
		result[key] = value
	}
	return json.Marshal(result)
}

func (b *ContentBlock) UnmarshalJSON(data []byte) error {
	if err := representableBlockKeys(data); err != nil {
		return contentJSONCause(err)
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil {
		return contentJSONCause(err)
	}
	var kind BlockType
	if fields == nil {
		return contentJSONError("INVALID_CONTENT_BLOCK")
	}
	_, hasType := fields["type"]
	if hasType {
		if err := representableJSONString(fields["type"]); err != nil {
			return contentJSONCause(err)
		}
		if err := json.Unmarshal(fields["type"], &kind); err != nil {
			return contentJSONError("INVALID_CONTENT_BLOCK")
		}
	}
	reserved := blockFields(kind)
	native := make(map[string]json.RawMessage, len(reserved))
	extensions := make(map[string]json.RawMessage)
	var attachmentExtra, dimensionsExtra json.RawMessage
	for name, value := range fields {
		if reserved[name] {
			switch name {
			case "type", "text", "toolCallId", "name", "argumentsDelta", "toolName", "mimeType":
				if err := representableJSONString(value); err != nil {
					return contentJSONCause(err)
				}
			case "attachment":
				var err error
				value, attachmentExtra, dimensionsExtra, err = exactAttachmentJSON(kind, value)
				if err != nil {
					return contentJSONCause(err)
				}
			}
			native[name] = value
		} else {
			extensions[name] = append(json.RawMessage(nil), value...)
		}
	}
	encoded, err := json.Marshal(native)
	if err != nil {
		return contentJSONCause(err)
	}
	type plain ContentBlock
	var value plain
	value.typeAbsent = !hasType
	if kind == BlockFile {
		var file struct {
			Attachment *FileAttachmentRef `json:"attachment"`
		}
		if err = json.Unmarshal(encoded, &file); err != nil {
			return contentJSONCause(err)
		}
		delete(native, "attachment")
		encoded, err = json.Marshal(native)
		if err != nil {
			return contentJSONCause(err)
		}
		if err = json.Unmarshal(encoded, &value); err != nil {
			return contentJSONCause(err)
		}
		value.FileAttachment = file.Attachment
	} else if err = json.Unmarshal(encoded, &value); err != nil {
		return contentJSONCause(err)
	}
	if len(extensions) > 0 {
		value.Extensions = extensions
	}
	value.attachmentExtensions = bindAttachmentExtensions(ContentBlock(value), attachmentExtra, dimensionsExtra)
	*b = ContentBlock(value)
	return nil
}
