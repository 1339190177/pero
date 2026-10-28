package model

import (
	"bytes"
	"encoding/json"
	"unicode/utf8"
)

// Go strings cannot represent an isolated UTF-16 surrogate. JSON.parse can.
// Reject only strings/keys this decoder must convert to Go strings; opaque
// RawMessage values remain raw. No historical raw Session data is rewritten.
func representableJSONString(raw []byte) error {
	raw = bytes.TrimSpace(raw)
	if !utf8.Valid(raw) {
		return contentJSONError("UNREPRESENTABLE_JSON_STRING")
	}
	if len(raw) < 2 || raw[0] != '"' {
		return nil
	}
	hex := func(b byte) int {
		switch {
		case b >= '0' && b <= '9':
			return int(b - '0')
		case b >= 'a' && b <= 'f':
			return int(b-'a') + 10
		case b >= 'A' && b <= 'F':
			return int(b-'A') + 10
		}
		return -1
	}
	unit := func(index int) (int, bool) {
		if index+6 > len(raw) || raw[index] != '\\' || raw[index+1] != 'u' {
			return 0, false
		}
		n := 0
		for _, b := range raw[index+2 : index+6] {
			d := hex(b)
			if d < 0 {
				return 0, false
			}
			n = n*16 + d
		}
		return n, true
	}
	for i := 1; i < len(raw)-1; i++ {
		if raw[i] != '\\' {
			continue
		}
		if i+1 >= len(raw) {
			return contentJSONError("INVALID_CONTENT_JSON")
		}
		if raw[i+1] != 'u' {
			i++
			continue
		}
		first, ok := unit(i)
		if !ok {
			return contentJSONError("INVALID_CONTENT_JSON")
		}
		if first >= 0xD800 && first <= 0xDBFF {
			second, ok := unit(i + 6)
			if !ok || second < 0xDC00 || second > 0xDFFF {
				return contentJSONError("UNREPRESENTABLE_JSON_STRING")
			}
			i += 11
		} else if first >= 0xDC00 && first <= 0xDFFF {
			return contentJSONError("UNREPRESENTABLE_JSON_STRING")
		} else {
			i += 5
		}
	}
	return nil
}

func representableBlockKeys(data []byte) error {
	decoder := json.NewDecoder(bytes.NewReader(data))
	token, err := decoder.Token()
	if err != nil || token != json.Delim('{') {
		return contentJSONError("INVALID_CONTENT_BLOCK")
	}
	for decoder.More() {
		start := decoder.InputOffset()
		if _, err = decoder.Token(); err != nil {
			return contentJSONError("INVALID_CONTENT_JSON")
		}
		key := bytes.TrimLeft(data[start:decoder.InputOffset()], " \t\n\r,")
		if err = representableJSONString(key); err != nil {
			return err
		}
		var value json.RawMessage
		if err = decoder.Decode(&value); err != nil {
			return contentJSONError("INVALID_CONTENT_JSON")
		}
	}
	return nil
}

func representableBlockStrings(b ContentBlock) error {
	values := []string{string(b.Type), b.ToolName, b.Text, b.MIMEType, string(b.ToolCallID), b.Name, b.ArgumentsDelta}
	if b.Attachment != nil {
		values = append(values, b.Attachment.AttachmentID, b.Attachment.MediaType, b.Attachment.Name)
	}
	if b.FileAttachment != nil {
		values = append(values, b.FileAttachment.AttachmentID, b.FileAttachment.Name)
	}
	for _, value := range values {
		if !utf8.ValidString(value) {
			return contentJSONError("UNREPRESENTABLE_JSON_STRING")
		}
	}
	for key := range b.Extensions {
		if !utf8.ValidString(key) {
			return contentJSONError("UNREPRESENTABLE_JSON_STRING")
		}
	}
	return nil
}
