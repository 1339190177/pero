package model

import (
	"bytes"
	"encoding/json"
)

// Attachment metadata belongs to the block and the exact decoded reference,
// never to a store capability. Keeping it here leaves both public refs
// comparable and prevents a replacement ref from inheriting another ref's data.
// A shallow block copy keeps the same owner; CloneContentBlock detaches it.
// The sidecar is emitted only while its ref pointer, typed values and
// dimensions pointer/values still match. An equal fresh ref does not match.
// Null/absent decoded refs have no sidecar. Public assignments have no mutation
// history: restoring the exact original pointer and values restores its owner.
// Cloning a mismatched/nil state permanently drops the detached sidecar.
// These private raw objects retain unknown keys as JSON tokens, including keys
// that cannot be represented by a Go string.
type contentAttachmentExtensions struct {
	fields     json.RawMessage
	dimensions json.RawMessage
	imageOwner *ImageAttachmentRef
	imageValue ImageAttachmentRef
	imageSize  ImageDimensions
	fileOwner  *FileAttachmentRef
	fileValue  FileAttachmentRef
}

func bindAttachmentExtensions(b ContentBlock, fields, dimensions json.RawMessage) *contentAttachmentExtensions {
	if len(fields) == 0 && len(dimensions) == 0 {
		return nil
	}
	extra := &contentAttachmentExtensions{fields: fields, dimensions: dimensions}
	switch {
	case b.Type == BlockImage && b.Attachment != nil:
		extra.imageOwner, extra.imageValue = b.Attachment, *b.Attachment
		if b.Attachment.OriginalDimensions != nil {
			extra.imageSize = *b.Attachment.OriginalDimensions
		}
	case b.Type == BlockFile && b.FileAttachment != nil:
		extra.fileOwner, extra.fileValue = b.FileAttachment, *b.FileAttachment
	default:
		return nil
	}
	return extra
}

func (b ContentBlock) ownedAttachmentExtensions() *contentAttachmentExtensions {
	extra := b.attachmentExtensions
	if extra == nil {
		return nil
	}
	switch {
	case b.Type == BlockImage && b.Attachment != nil && b.Attachment == extra.imageOwner:
		if *b.Attachment == extra.imageValue && (b.Attachment.OriginalDimensions == nil || *b.Attachment.OriginalDimensions == extra.imageSize) {
			return extra
		}
	case b.Type == BlockFile && b.FileAttachment != nil && b.FileAttachment == extra.fileOwner:
		if *b.FileAttachment == extra.fileValue {
			return extra
		}
	}
	return nil
}

// splitAttachmentObject classifies exact canonical keys for typed lookup.
// Unknown keys and values are copied as raw JSON, not retained as Go strings.
// Canonical duplicates retain encoding/json's existing last-value behavior.
func splitAttachmentObject(data []byte, known map[string]bool) (map[string]json.RawMessage, json.RawMessage, error) {
	if !json.Valid(data) {
		return nil, nil, contentJSONError("INVALID_CONTENT_JSON")
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	if token, err := decoder.Token(); err != nil || token != json.Delim('{') {
		return nil, nil, contentJSONError("INVALID_CONTENT_JSON")
	}
	fields := make(map[string]json.RawMessage)
	var extra bytes.Buffer
	extra.WriteByte('{')
	for decoder.More() {
		start := decoder.InputOffset()
		token, err := decoder.Token()
		if err != nil {
			return nil, nil, contentJSONError("INVALID_CONTENT_JSON")
		}
		key, ok := token.(string)
		if !ok {
			return nil, nil, contentJSONError("INVALID_CONTENT_JSON")
		}
		rawKey := bytes.TrimLeft(data[start:decoder.InputOffset()], " \t\n\r,")
		var value json.RawMessage
		if err := decoder.Decode(&value); err != nil {
			return nil, nil, contentJSONError("INVALID_CONTENT_JSON")
		}
		if known[key] {
			fields[key] = value
			continue
		}
		if extra.Len() > 1 {
			extra.WriteByte(',')
		}
		extra.Write(rawKey)
		extra.WriteByte(':')
		extra.Write(value)
	}
	if extra.Len() == 1 {
		return fields, nil, nil
	}
	extra.WriteByte('}')
	return fields, extra.Bytes(), nil
}

// exactAttachmentJSON retains literal aliases as opaque metadata and selects
// only exact typed leaves. It adds no required-field or storage validation.
func exactAttachmentJSON(kind BlockType, data []byte) ([]byte, json.RawMessage, json.RawMessage, error) {
	if bytes.Equal(bytes.TrimSpace(data), []byte("null")) {
		return data, nil, nil, nil
	}
	known := map[string]bool{"attachmentId": true, "name": true, "bytes": true}
	if kind == BlockImage {
		for _, name := range []string{"mediaType", "width", "height", "originalDimensions"} {
			known[name] = true
		}
	}
	fields, extra, err := splitAttachmentObject(data, known)
	if err != nil {
		return nil, nil, nil, err
	}
	for _, key := range []string{"attachmentId", "name", "mediaType"} {
		if raw, exists := fields[key]; exists {
			if err := representableJSONString(raw); err != nil {
				return nil, nil, nil, err
			}
		}
	}
	var dimensionsExtra json.RawMessage
	if raw, exists := fields["originalDimensions"]; exists && !bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		dimensions, extra, err := splitAttachmentObject(raw, map[string]bool{"width": true, "height": true})
		if err != nil {
			return nil, nil, nil, err
		}
		dimensionsExtra = extra
		fields["originalDimensions"], err = json.Marshal(dimensions)
		if err != nil {
			return nil, nil, nil, contentJSONError("INVALID_CONTENT_JSON")
		}
	}
	typed, err := json.Marshal(fields)
	return typed, extra, dimensionsExtra, err
}

// Both objects are private valid JSON with disjoint canonical/extra keys.
// Join their member bytes instead of decoding unknown keys or numbers.
func appendAttachmentFields(typed, extra json.RawMessage) json.RawMessage {
	if len(extra) == 0 {
		return typed
	}
	result := append(json.RawMessage(nil), typed[:len(typed)-1]...)
	if len(typed) > 2 {
		result = append(result, ',')
	}
	return append(result, extra[1:]...)
}

func (extra *contentAttachmentExtensions) marshalAttachment(typed json.RawMessage) (json.RawMessage, error) {
	if len(extra.dimensions) != 0 {
		var ref map[string]json.RawMessage
		if err := json.Unmarshal(typed, &ref); err != nil {
			return nil, contentJSONError("INVALID_CONTENT_JSON")
		}
		ref["originalDimensions"] = appendAttachmentFields(ref["originalDimensions"], extra.dimensions)
		var err error
		typed, err = json.Marshal(ref)
		if err != nil {
			return nil, contentJSONError("INVALID_CONTENT_JSON")
		}
	}
	return appendAttachmentFields(typed, extra.fields), nil
}
