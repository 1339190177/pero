package todos

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/phodal/pero/harness/go/harness"
	harnesstool "github.com/phodal/pero/harness/go/tool"
)

// ContentNormalizationMode selects Todo content semantics, not permissions.
// The zero value preserves ordinary Point callers and untagged historical rows.
type ContentNormalizationMode uint8

const (
	ContentNormalizationLegacy ContentNormalizationMode = iota
	ContentNormalizationDSHAlpha3
)

// This optional todo/write field is data-format interpretation only. Old
// readers ignore it and reapply legacy trimming: downgrade is not semantically
// safe for NEL/BOM content. It is deliberately absent from canonical tool Value.
const ecmascriptTrimV1 = "ecmascript-trim-v1"

type normalizedWriteEvent struct {
	Todos         []Item `json:"todos"`
	Normalization string `json:"normalization"`
}

// Only this private type may select the DSH plain domain-error envelope.
// Runtime/ownership/decoding failures do not gain classification by their text.
type todoDomainError struct{ message string }

func (e *todoDomainError) Error() string { return e.message }

func dshTodoFailure(err *todoDomainError) harness.ToolResult {
	meta, _ := json.Marshal(map[string]string{"message": err.Error()})
	return harness.ToolResult{IsError: true,
		Content: []harness.ContentBlock{{Type: harness.BlockText, Text: "Error: " + err.Error()}}, Meta: meta}
}

func normalize(source []Item, allowParallel bool) ([]Item, map[Status]int, error) {
	return normalizeWithMode(source, allowParallel, ContentNormalizationLegacy)
}

func normalizeWithMode(source []Item, allowParallel bool, mode ContentNormalizationMode) ([]Item, map[Status]int, error) {
	domainError := func(message string) error {
		if mode == ContentNormalizationDSHAlpha3 {
			return &todoDomainError{message: message}
		}
		return errors.New(message)
	}
	result := make([]Item, 0, len(source))
	counts := map[Status]int{Pending: 0, InProgress: 0, Completed: 0}
	seen := make(map[string]struct{}, len(source))
	for _, sourceItem := range source {
		content := strings.TrimSpace(sourceItem.Content)
		if mode == ContentNormalizationDSHAlpha3 {
			content = strings.TrimFunc(sourceItem.Content, isECMAScriptWhitespace)
		}
		item := Item{Content: content, Status: sourceItem.Status}
		if item.Content == "" {
			message := "invalid todo: content must be a non-empty string"
			if mode == ContentNormalizationDSHAlpha3 {
				message = "invalid todo: `content` must be a non-empty string"
			}
			return nil, nil, domainError(message)
		}
		if _, duplicate := seen[item.Content]; duplicate {
			quoted := fmt.Sprintf("%q", item.Content)
			if mode == ContentNormalizationDSHAlpha3 {
				quoted = quoteECMAScriptString(item.Content)
			}
			return nil, nil, domainError("invalid todos: duplicate content " + quoted)
		}
		if _, valid := counts[item.Status]; !valid {
			return nil, nil, domainError(fmt.Sprintf("invalid todo status %q", item.Status))
		}
		seen[item.Content] = struct{}{}
		counts[item.Status]++
		result = append(result, item)
	}
	if !allowParallel && counts[InProgress] > 1 {
		return nil, nil, domainError(fmt.Sprintf("invalid todos: at most one task may be in_progress (got %d)", counts[InProgress]))
	}
	return result, counts, nil
}

func isECMAScriptWhitespace(r rune) bool {
	switch r {
	case '\t', '\n', '\v', '\f', '\r', ' ', '\u00a0', '\u1680',
		'\u2000', '\u2001', '\u2002', '\u2003', '\u2004', '\u2005', '\u2006',
		'\u2007', '\u2008', '\u2009', '\u200a', '\u2028', '\u2029', '\u202f',
		'\u205f', '\u3000', '\ufeff':
		return true
	}
	return false
}

// Input has already passed ValidateNativeJSONDecoding. Unlike Go %q or the
// default JSON encoder, JSON.stringify leaves U+0085/U+2028/U+2029/HTML intact.
func quoteECMAScriptString(value string) string {
	var out strings.Builder
	out.WriteByte('"')
	const hex = "0123456789abcdef"
	for _, r := range value {
		switch r {
		case '"', '\\':
			out.WriteByte('\\')
			out.WriteRune(r)
		case '\b':
			out.WriteString(`\b`)
		case '\f':
			out.WriteString(`\f`)
		case '\n':
			out.WriteString(`\n`)
		case '\r':
			out.WriteString(`\r`)
		case '\t':
			out.WriteString(`\t`)
		default:
			if r < 0x20 {
				out.WriteString(`\u00`)
				out.WriteByte(hex[r>>4])
				out.WriteByte(hex[r&15])
			} else {
				out.WriteRune(r)
			}
		}
	}
	out.WriteByte('"')
	return out.String()
}

// decodeWriteEvent preserves the old decoder verbatim for missing format tags.
// A present tag is a format declaration, never tool/owner authorization.
func decodeWriteEvent(raw json.RawMessage) (writeEvent, ContentNormalizationMode, error) {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		var legacy writeEvent
		legacyErr := json.Unmarshal(raw, &legacy)
		return legacy, ContentNormalizationLegacy, legacyErr
	}
	tag, tagged := fields["normalization"]
	if !tagged {
		var event writeEvent
		err := json.Unmarshal(raw, &event)
		return event, ContentNormalizationLegacy, err
	}
	formatError := func() (writeEvent, ContentNormalizationMode, error) {
		return writeEvent{}, ContentNormalizationLegacy, errors.New("invalid normalized todo/write format")
	}
	if !rawJSONType(tag, '"') {
		return formatError()
	}
	var format string
	if json.Unmarshal(tag, &format) != nil || format != ecmascriptTrimV1 {
		return formatError()
	}
	if harnesstool.ValidateNativeJSONDecoding(raw) != nil || !rawJSONType(fields["todos"], '[') {
		return formatError()
	}
	var rows []json.RawMessage
	if json.Unmarshal(fields["todos"], &rows) != nil {
		return formatError()
	}
	items := make([]Item, 0, len(rows))
	for _, row := range rows {
		if !rawJSONType(row, '{') {
			return formatError()
		}
		var itemFields map[string]json.RawMessage
		if json.Unmarshal(row, &itemFields) != nil || len(itemFields) != 2 || !rawJSONType(itemFields["content"], '"') || !rawJSONType(itemFields["status"], '"') {
			return formatError()
		}
		var item Item
		if json.Unmarshal(row, &item) != nil || (item.Status != Pending && item.Status != InProgress && item.Status != Completed) {
			return formatError()
		}
		items = append(items, item)
	}
	return writeEvent{Todos: items}, ContentNormalizationDSHAlpha3, nil
}

func rawJSONType(raw json.RawMessage, first byte) bool {
	raw = bytes.TrimSpace(raw)
	return len(raw) > 0 && raw[0] == first
}

