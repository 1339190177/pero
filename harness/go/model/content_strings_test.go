package model

import (
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"
)

func TestContentTypedStringsAndKeysRejectUnrepresentableSurrogates(t *testing.T) {
	for _, raw := range []string{`{"type":"opaque","\ud800":"private"}`, `{"type":"\ud800"}`, `{"type":"text","text":"\udc00"}`, `{"type":"tool-call","name":"private\ud800","toolCallId":"x"}`, `{"type":"tool-call","argumentsDelta":"\ud800"}`, `{"type":"image","attachment":{"attachmentId":"\udc00"}}`, `{"type":"file","attachment":{"name":"\ud800"}}`, `{"type":"tool-result","content":[{"type":"text","text":"\ud800"}]}`} {
		var block ContentBlock
		err := json.Unmarshal([]byte(raw), &block)
		var codec interface{ ToolErrorCode() string }
		if !errors.As(err, &codec) || codec.ToolErrorCode() != "UNREPRESENTABLE_JSON_STRING" || strings.Contains(err.Error(), "private") {
			t.Fatalf("unrepresentable string error=%v", err)
		}
	}
	for _, raw := range []string{`{"type":"text","text":"\ud83d\ude00"}`, `{"type":"text","text":"\\ud800"}`, `{"type":"text","text":"�"}`} {
		var block ContentBlock
		if err := json.Unmarshal([]byte(raw), &block); err != nil {
			t.Fatalf("representable string rejected: %v", err)
		}
	}
}

func TestOpaqueRawStringsAndMetaRetainSurrogateEscapes(t *testing.T) {
	for _, raw := range []string{`{"type":"opaque","value":"\ud800","nested":{"\udc00":"\udfff"}}`, `{"type":"text","text":"valid","meta":{"value":"\ud800"}}`, `{"type":"tool-call","name":"tool","toolCallId":"x","arguments":{"value":"\ud800"}}`} {
		var block ContentBlock
		if err := json.Unmarshal([]byte(raw), &block); err != nil {
			t.Fatal(err)
		}
		encoded, err := json.Marshal(block)
		if err != nil {
			t.Fatal(err)
		}
		var before, after map[string]json.RawMessage
		_ = json.Unmarshal([]byte(raw), &before)
		_ = json.Unmarshal(encoded, &after)
		if !reflect.DeepEqual(before, after) {
			t.Fatalf("raw string JSON changed: %s", encoded)
		}
	}
}

func TestNativeInvalidUTF8CannotSilentlyBecomeReplacementCharacters(t *testing.T) {
	invalid := string([]byte{0xed, 0xa0, 0x80})
	for _, block := range []ContentBlock{{Type: BlockText, Text: invalid}, {Type: BlockFile, FileAttachment: &FileAttachmentRef{Name: invalid}}} {
		_, err := json.Marshal(block)
		var codec interface{ ToolErrorCode() string }
		if !errors.As(err, &codec) || codec.ToolErrorCode() != "UNREPRESENTABLE_JSON_STRING" {
			t.Fatalf("invalid UTF8 encoded: %v", err)
		}
	}
}

func TestContentCodecAttachmentUnknownStringsAreNotTypedLeaves(t *testing.T) {
	for _, raw := range []string{
		`{"type":"image","attachment":{"attachmentId":"exact","AttachmentID":"\ud800","mediaType":"image/png","bytes":1,"width":1,"height":1,"annotation":{"\ud800":"\udfff"},"originalDimensions":{"width":1,"height":1,"annotation":"\ud800"}}}`,
		`{"type":"file","attachment":{"AttachmentID":"\ud800","Name":"\udfff","bytes":1,"annotation":"\ud800"}}`,
	} {
		var block ContentBlock
		if err := json.Unmarshal([]byte(raw), &block); err != nil {
			t.Fatalf("unknown nested string was interpreted: %v", err)
		}
		if block.FileAttachment != nil && (block.FileAttachment.AttachmentID != "" || block.FileAttachment.Name != "") {
			t.Fatal("attachment alias acquired lookup authority")
		}
		if block.Attachment != nil && block.Attachment.AttachmentID != "exact" {
			t.Fatal("attachment alias replaced canonical lookup ID")
		}
	}
	invalid := string([]byte{0xed, 0xa0, 0x80})
	block := ContentBlock{Type: "opaque"}
	mutableContentExtensions(t, &block)[invalid] = json.RawMessage(`1`)
	if _, err := json.Marshal(block); err == nil {
		t.Fatal("invalid UTF-8 map key accepted")
	}
}
