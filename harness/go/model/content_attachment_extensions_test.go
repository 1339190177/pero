package model

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

const attachmentExtraImage = `{"type":"image","attachment":{"attachmentId":"canonical","AttachmentID":"literal-alias","mediaType":"image/png","bytes":4,"width":1,"height":1,"annotation":{"counter":9007199254740993,"decimal":0.12345678901234567890123456789,"negativeZero":-0,"surrogate":"\ud800","\udfff":"\udc00"},"\ud800":{"raw":"\udfff"},"originalDimensions":{"width":2,"height":2,"Width":999,"annotation":{"negativeZero":-0,"\udfff":"\ud800"},"\udfff":"raw dimension key"}}}`
const attachmentExtraFile = `{"type":"file","attachment":{"attachmentId":"canonical-file","AttachmentID":"literal-file","name":"file.txt","bytes":4,"annotation":{"counter":9007199254740993,"decimal":0.12345678901234567890123456789,"negativeZero":-0,"surrogate":"\ud800"},"\udfff":"raw file key"}}`

func decodeAttachmentExtra(t *testing.T, raw string) ContentBlock {
	t.Helper()
	var block ContentBlock
	if err := json.Unmarshal([]byte(raw), &block); err != nil {
		t.Fatal(err)
	}
	return block
}
func marshalAttachmentExtra(t *testing.T, block ContentBlock) []byte {
	t.Helper()
	raw, err := json.Marshal(block)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}
func attachmentRawField(t *testing.T, raw []byte, keys ...string) json.RawMessage {
	t.Helper()
	value := json.RawMessage(raw)
	for _, key := range keys {
		var fields map[string]json.RawMessage
		if err := json.Unmarshal(value, &fields); err != nil {
			t.Fatal(err)
		}
		value = fields[key]
	}
	return value
}
func assertAttachmentRaw(t *testing.T, got, want []byte) {
	t.Helper()
	var actual, expected bytes.Buffer
	if err := json.Compact(&actual, got); err != nil {
		t.Fatal(err)
	}
	if err := json.Compact(&expected, want); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(actual.Bytes(), expected.Bytes()) {
		t.Fatalf("raw value changed: got %s want %s", actual.Bytes(), expected.Bytes())
	}
}

// Raw wire vectors use only the pre-S4 public API, so the baseline failure is
// observable data loss, never an unavailable sidecar field or method.
func TestAttachmentNestedRawExtrasAndExactAuthority(t *testing.T) {
	for _, input := range []string{attachmentExtraImage, attachmentExtraFile} {
		block := decodeAttachmentExtra(t, input)
		encoded := marshalAttachmentExtra(t, block)
		assertAttachmentRaw(t, attachmentRawField(t, encoded, "attachment", "annotation"), attachmentRawField(t, []byte(input), "attachment", "annotation"))
		assertAttachmentRaw(t, attachmentRawField(t, encoded, "attachment", "AttachmentID"), attachmentRawField(t, []byte(input), "attachment", "AttachmentID"))
		if !bytes.Contains(encoded, []byte(`"\udfff"`)) {
			t.Fatalf("unknown surrogate key lost: %s", encoded)
		}
		if block.Type == BlockImage {
			if block.Attachment.AttachmentID != "canonical" || block.Attachment.OriginalDimensions.Width != 2 {
				t.Fatal("aliases changed canonical image authority")
			}
			assertAttachmentRaw(t, attachmentRawField(t, encoded, "attachment", "originalDimensions", "annotation"), attachmentRawField(t, []byte(input), "attachment", "originalDimensions", "annotation"))
			if !bytes.Contains(encoded, []byte(`"\ud800":{"raw":"\udfff"}`)) {
				t.Fatal("direct raw attachment key changed")
			}
		} else if block.FileAttachment.AttachmentID != "canonical-file" {
			t.Fatal("alias changed canonical file authority")
		}
		again := marshalAttachmentExtra(t, decodeAttachmentExtra(t, string(encoded)))
		if !bytes.Equal(encoded, again) {
			t.Fatalf("roundtrip unstable: %s != %s", encoded, again)
		}
	}
	for _, kind := range []string{"image", "file"} {
		block := decodeAttachmentExtra(t, `{"type":"`+kind+`","attachment":{"AttachmentID":"not-authority","Name":"not-name","Bytes":999,"MediaType":"not-media","Width":999,"Height":999}}`)
		if kind == "image" {
			if *block.Attachment != (ImageAttachmentRef{}) {
				t.Fatalf("aliases populated image ref: %+v", block.Attachment)
			}
		} else if *block.FileAttachment != (FileAttachmentRef{}) {
			t.Fatalf("aliases populated file ref: %+v", block.FileAttachment)
		}
		if !bytes.Contains(marshalAttachmentExtra(t, block), []byte(`"AttachmentID":"not-authority"`)) {
			t.Fatal("literal alias discarded")
		}
	}
}

func TestAttachmentTypedStringsAndOpaqueShapes(t *testing.T) {
	for _, raw := range []string{`{"type":"image","attachment":{"attachmentId":"\ud800"}}`, `{"type":"image","attachment":{"mediaType":"\udfff"}}`, `{"type":"file","attachment":{"name":"\ud800"}}`} {
		var block ContentBlock
		err := json.Unmarshal([]byte(raw), &block)
		if err == nil || !strings.Contains(err.Error(), "UNREPRESENTABLE_JSON_STRING") {
			t.Fatalf("typed string accepted: %v", err)
		}
	}
	for _, raw := range []string{`{"type":"plugin/widget","attachment":{"attachmentId":"\ud800","originalDimensions":{"width":"opaque"},"\udfff":"\ud800"}}`, `{"type":"plugin/widget","attachment":[{"attachmentId":"not-capability"}]}`} {
		block := decodeAttachmentExtra(t, raw)
		if block.Attachment != nil || block.FileAttachment != nil {
			t.Fatal("opaque shape acquired ref")
		}
		assertAttachmentRaw(t, attachmentRawField(t, marshalAttachmentExtra(t, block), "attachment"), attachmentRawField(t, []byte(raw), "attachment"))
	}
}

func TestAttachmentSidecarOwnershipAndCloneIsolation(t *testing.T) {
	for _, input := range []string{attachmentExtraImage, attachmentExtraFile} {
		original := decodeAttachmentExtra(t, input)
		before := marshalAttachmentExtra(t, original)
		shallow := original
		clone := CloneContentBlock(shallow)
		if clone.attachmentExtensions == original.attachmentExtensions {
			t.Fatal("clone shares sidecar")
		}
		clone.attachmentExtensions.fields[1] = ' '
		if len(clone.attachmentExtensions.dimensions) != 0 {
			clone.attachmentExtensions.dimensions[1] = ' '
		}
		if !bytes.Equal(marshalAttachmentExtra(t, original), before) {
			t.Fatal("clone raw mutation reached original")
		}
		clone = CloneContentBlock(shallow)
		if original.Type == BlockImage {
			original.Attachment.AttachmentID = "changed"
		} else {
			original.FileAttachment.AttachmentID = "changed"
		}
		if bytes.Contains(marshalAttachmentExtra(t, original), []byte(`"annotation"`)) || bytes.Contains(marshalAttachmentExtra(t, shallow), []byte(`"annotation"`)) {
			t.Fatal("shared ref mutation retained stale extras")
		}
		if !bytes.Equal(marshalAttachmentExtra(t, clone), before) {
			t.Fatal("source ref mutation reached detached clone")
		}
		if CloneContentBlock(shallow).attachmentExtensions != nil {
			t.Fatal("clone retained mismatched owner")
		}
	}
	for _, mutate := range []struct {
		name  string
		apply func(*ContentBlock)
	}{
		{"equal-new-image", func(b *ContentBlock) { ref := *b.Attachment; b.Attachment = &ref }},
		{"other-image", func(b *ContentBlock) { b.Attachment = &ImageAttachmentRef{AttachmentID: "other"} }},
		{"typed-media-change", func(b *ContentBlock) { b.Attachment.MediaType = "image/jpeg" }},
		{"dimensions-pointer", func(b *ContentBlock) {
			dims := *b.Attachment.OriginalDimensions
			b.Attachment.OriginalDimensions = &dims
		}},
		{"dimensions-value", func(b *ContentBlock) { b.Attachment.OriginalDimensions.Width++ }},
		{"dimensions-nil", func(b *ContentBlock) { b.Attachment.OriginalDimensions = nil }},
		{"ref-nil", func(b *ContentBlock) { b.Attachment = nil }},
		{"kind-change", func(b *ContentBlock) {
			b.Type = BlockFile
			b.FileAttachment = &FileAttachmentRef{AttachmentID: "other"}
		}},
	} {
		t.Run(mutate.name, func(t *testing.T) {
			block := decodeAttachmentExtra(t, attachmentExtraImage)
			mutate.apply(&block)
			if bytes.Contains(marshalAttachmentExtra(t, block), []byte(`"annotation"`)) {
				t.Fatal("changed owner inherited annotation")
			}
		})
	}
	originalImage := decodeAttachmentExtra(t, attachmentExtraImage)
	clonedImage := CloneContentBlock(originalImage)
	clonedImage.Attachment.OriginalDimensions.Width++
	if originalImage.Attachment.OriginalDimensions.Width != 2 {
		t.Fatal("clone dimensions mutation reached original")
	}
	file := decodeAttachmentExtra(t, attachmentExtraFile)
	ref := *file.FileAttachment
	file.FileAttachment = &ref
	if bytes.Contains(marshalAttachmentExtra(t, file), []byte(`"annotation"`)) {
		t.Fatal("equal fresh file ref inherited extras")
	}
}

func TestAttachmentNilRoundtripAndExactOwnerRestoration(t *testing.T) {
	for _, input := range []string{attachmentExtraImage, attachmentExtraFile} {
		block := decodeAttachmentExtra(t, input)
		before := marshalAttachmentExtra(t, block)
		image, file := block.Attachment, block.FileAttachment
		block.Attachment = nil
		block.FileAttachment = nil
		without := marshalAttachmentExtra(t, block)
		if bytes.Contains(without, []byte(`"annotation"`)) {
			t.Fatal("nil ref emitted extras")
		}
		restored := decodeAttachmentExtra(t, string(without))
		restored.Attachment = image
		restored.FileAttachment = file
		if bytes.Contains(marshalAttachmentExtra(t, restored), []byte(`"annotation"`)) {
			t.Fatal("JSON nil roundtrip resurrected extras")
		}
		detached := CloneContentBlock(block)
		detached.Attachment = image
		detached.FileAttachment = file
		if bytes.Contains(marshalAttachmentExtra(t, detached), []byte(`"annotation"`)) {
			t.Fatal("clone of nil owner resurrected extras")
		}
		// Public assignments are not observable. Returning this same value to its
		// exact original owner legitimately makes that owner's metadata visible.
		block.Attachment = image
		block.FileAttachment = file
		if !bytes.Equal(marshalAttachmentExtra(t, block), before) {
			t.Fatal("exact original owner restoration changed metadata")
		}
	}
	for _, kind := range []string{"image", "file"} {
		block := decodeAttachmentExtra(t, `{"type":"`+kind+`","attachment":null}`)
		if block.Attachment != nil || block.FileAttachment != nil || block.attachmentExtensions != nil {
			t.Fatal("null acquired a ref or sidecar")
		}
	}
}

func TestAttachmentSidecarDetachesInputBytes(t *testing.T) {
	raw := []byte(attachmentExtraImage)
	var block ContentBlock
	if err := json.Unmarshal(raw, &block); err != nil {
		t.Fatal(err)
	}
	before := marshalAttachmentExtra(t, block)
	for i := range raw {
		raw[i] = 'x'
	}
	if !bytes.Equal(marshalAttachmentExtra(t, block), before) {
		t.Fatal("input buffer mutation reached attachment sidecar")
	}
}

func TestAttachmentOwnerValueRestorationAfterMarshalAndMismatchedClone(t *testing.T) {
	for _, vector := range []struct {
		name, raw       string
		mutate, restore func(*ContentBlock)
	}{
		{"image-scalar", attachmentExtraImage, func(b *ContentBlock) { b.Attachment.AttachmentID = "different-image" }, func(b *ContentBlock) { b.Attachment.AttachmentID = "canonical" }},
		{"image-dimensions", attachmentExtraImage, func(b *ContentBlock) { b.Attachment.OriginalDimensions.Width = 99 }, func(b *ContentBlock) { b.Attachment.OriginalDimensions.Width = 2 }},
		{"file-scalar", attachmentExtraFile, func(b *ContentBlock) { b.FileAttachment.Name = "different-file.txt" }, func(b *ContentBlock) { b.FileAttachment.Name = "file.txt" }},
	} {
		t.Run(vector.name, func(t *testing.T) {
			original := decodeAttachmentExtra(t, vector.raw)
			before := marshalAttachmentExtra(t, original)
			shallow := original
			vector.mutate(&original)
			// Exercise serialization of the mismatch before restoring the public ref.
			// MarshalJSON must not acquire mutation-history state or erase its owner.
			changed := marshalAttachmentExtra(t, original)
			if bytes.Contains(changed, []byte(`"annotation"`)) {
				t.Fatal("changed typed owner emitted stale metadata")
			}
			if !bytes.Equal(marshalAttachmentExtra(t, shallow), changed) {
				t.Fatal("shallow copy did not observe its shared owner's changed value")
			}
			detached := CloneContentBlock(original)
			if detached.attachmentExtensions != nil {
				t.Fatal("clone retained metadata for a mismatched typed owner")
			}
			vector.restore(&original)
			if !bytes.Equal(marshalAttachmentExtra(t, original), before) || !bytes.Equal(marshalAttachmentExtra(t, shallow), before) {
				t.Fatal("restoring exact original owner values did not restore its metadata")
			}
			// Restoring the same typed values on the detached clone cannot reconstruct
			// metadata deliberately dropped while that clone was made from a mismatch.
			vector.restore(&detached)
			if bytes.Contains(marshalAttachmentExtra(t, detached), []byte(`"annotation"`)) {
				t.Fatal("restoring a mismatch-cloned ref resurrected discarded metadata")
			}
			if !bytes.Equal(marshalAttachmentExtra(t, original), before) {
				t.Fatal("detached clone restoration changed original owner")
			}
		})
	}
}
