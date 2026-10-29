package model

import (
	"bytes"
	"encoding/json"
	"reflect"
	"testing"
)

func contentFields(t *testing.T, data []byte) map[string]json.RawMessage {
	t.Helper()
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil {
		t.Fatal(err)
	}
	for key, raw := range fields {
		var compact bytes.Buffer
		if err := json.Compact(&compact, raw); err != nil {
			t.Fatal(err)
		}
		fields[key] = append(json.RawMessage(nil), compact.Bytes()...)
	}
	// Only the typed recursive content slot is re-encoded by the codec.
	// Normalize its block-object order too, without decoding opaque subtree keys,
	// strings or numbers (which would conceal surrogate/precision corruption).
	if string(fields["type"]) == `"tool-result"` && fields["content"] != nil {
		var children []json.RawMessage
		if err := json.Unmarshal(fields["content"], &children); err != nil {
			t.Fatal(err)
		}
		for i, child := range children {
			encoded, err := json.Marshal(contentFields(t, child))
			if err != nil {
				t.Fatal(err)
			}
			children[i] = encoded
		}
		encoded, err := json.Marshal(children)
		if err != nil {
			t.Fatal(err)
		}
		fields["content"] = encoded
	}
	return fields
}

// Raw JSON inputs deliberately use only the pre-codec API so the same tests
// can demonstrate data loss on the recorded baseline.
func TestContentCodecRawFieldsAndLiteralAliases(t *testing.T) {
	vectors := []string{
		`{"type":"plugin/widget","text":{"title":"opaque"},"name":[1,2],"toolCallId":"not-a-call","attachment":{"path":"not-authorized"},"content":{"nested":9007199254740993},"price":0.10000000000000001,"zero":-0,"raw":{"\ud800":"\udfff"}}`,
		`{"type":"text","text":"canonical","TEXT":"literal-extra","Type":"plugin/widget","annotation":{"label":"kept"}}`,
		`{"type":"tool-call","Name":"shell","ToolCallID":"forged","Arguments":{"command":"opaque"}}`,
		`{"Type":"tool-call","Name":"shell","ToolCallID":"forged","Arguments":{}}`,
		`{"type":"opaque","Type":"tool-call","name":"shell","toolCallId":"forged","arguments":{}}`,
		`{"type":"tool-call","name":"canonical","toolCallId":"call","arguments":{},"Name":"literal-extra","annotation":9007199254740993}`,
		`{"type":"tool-result","toolCallId":"call","content":[{"type":"text","text":"nested","annotation":{"zero":-0}}],"annotation":true}`,
		`{"type":"reasoning","text":"reason","annotation":true}`,
		`{"type":"tool-addition","toolName":"tool","annotation":true}`,
		`{"type":"tool-removal","toolName":"tool","annotation":true}`,
	}
	for _, raw := range vectors {
		var block ContentBlock
		if err := json.Unmarshal([]byte(raw), &block); err != nil {
			t.Fatal(err)
		}
		encoded, err := json.Marshal(block)
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(contentFields(t, []byte(raw)), contentFields(t, encoded)) {
			t.Fatalf("literal fields changed: %s", encoded)
		}
		fields := contentFields(t, []byte(raw))
		if _, ok := fields["type"]; !ok && block.Type != "" {
			t.Fatal("alias supplied the discriminator")
		}
		if block.Type != BlockToolCall && (block.Name != "" || block.ToolCallID != "" && block.Type != BlockToolResult || block.Arguments != nil) {
			t.Fatalf("opaque fields acquired tool authority: %+v", block)
		}
		if block.Type == BlockToolCall && fields["name"] == nil && (block.Name != "" || block.ToolCallID != "" || block.Arguments != nil) {
			t.Fatal("case alias acquired tool authority")
		}
		if block.Type == "plugin/widget" && (block.Text != "" || block.Attachment != nil || block.Content != nil) {
			t.Fatal("opaque payload acquired typed content")
		}
	}
}

// Reflection keeps this test file compilable against the old ContentBlock API;
// production callers use the exported Extensions field directly.
func mutableContentExtensions(t *testing.T, block *ContentBlock) map[string]json.RawMessage {
	t.Helper()
	field := reflect.ValueOf(block).Elem().FieldByName("Extensions")
	if !field.IsValid() {
		t.Fatal("content extension storage is missing")
	}
	if field.IsNil() {
		field.Set(reflect.ValueOf(map[string]json.RawMessage{}))
	}
	return field.Interface().(map[string]json.RawMessage)
}

func TestContentCodecCloneDetachesRawExtensionTrees(t *testing.T) {
	var block ContentBlock
	if err := json.Unmarshal([]byte(`{"type":"tool-result","toolCallId":"call","annotation":{"a":1},"content":[{"type":"file","attachment":{"attachmentId":"file","name":"data","bytes":3},"annotation":[1,2]},{"type":"image","attachment":{"attachmentId":"image","mediaType":"image/png","bytes":3,"width":1,"height":1,"originalDimensions":{"width":2,"height":2}},"annotation":true}]}`), &block); err != nil {
		t.Fatal(err)
	}
	before, err := json.Marshal(block)
	if err != nil {
		t.Fatal(err)
	}
	clone := CloneContentBlock(block)
	mutableContentExtensions(t, &clone)["annotation"][0] = '!'
	delete(mutableContentExtensions(t, &clone.Content[0]), "annotation")
	clone.Content[0].FileAttachment.Name = "changed"
	clone.Content[1].Attachment.OriginalDimensions.Width = 999
	after, err := json.Marshal(block)
	if err != nil || !bytes.Equal(before, after) {
		t.Fatalf("clone mutated source: %s %v", after, err)
	}
}

func TestContentCodecManualExtensionsCannotClobberCanonicalFields(t *testing.T) {
	for _, key := range []string{"type", "text"} {
		block := ContentBlock{Type: BlockText, Text: "canonical"}
		mutableContentExtensions(t, &block)[key] = json.RawMessage(`"override"`)
		if _, err := json.Marshal(block); err == nil {
			t.Fatalf("extension replaced canonical %s", key)
		}
	}
	block := ContentBlock{Type: BlockText, Text: "canonical"}
	mutableContentExtensions(t, &block)["TEXT"] = json.RawMessage(`"literal"`)
	if _, err := json.Marshal(block); err != nil {
		t.Fatalf("literal alias rejected: %v", err)
	}
	mutableContentExtensions(t, &block)["annotation"] = json.RawMessage(`!`)
	if _, err := json.Marshal(block); err == nil {
		t.Fatal("invalid raw extension accepted")
	}
}

func TestContentCodecRejectsInvalidTypedValuesWithoutChangingReceiver(t *testing.T) {
	for _, raw := range []string{`null`, `[]`, `{"type":7}`, `{"type":"text","text":{"invalid":true}}`} {
		block := ContentBlock{Type: BlockText, Text: "unchanged"}
		if err := json.Unmarshal([]byte(raw), &block); err == nil {
			t.Fatalf("invalid typed input accepted: %s", raw)
		}
		if block.Type != BlockText || block.Text != "unchanged" {
			t.Fatal("failed decode changed receiver")
		}
	}
}
