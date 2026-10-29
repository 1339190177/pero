package media

import (
	"encoding/json"
	"fmt"
	"math"
	"reflect"
	"strings"
	"testing"

	harnessmodel "github.com/phodal/pero/harness/go/model"
)

func projectionImage(index int, bytes int64) harnessmodel.ContentBlock {
	ref := harnessmodel.ImageAttachmentRef{
		AttachmentID: fmt.Sprintf("sha256:%064x", index+1), MediaType: "image/png",
		Bytes: bytes, Width: 10, Height: 20, Name: fmt.Sprintf("image-%d.png", index),
	}
	return harnessmodel.ContentBlock{Type: harnessmodel.BlockImage, Attachment: &ref}
}

func countBlockTypes(blocks []harnessmodel.ContentBlock) (texts, images int) {
	for _, block := range blocks {
		switch block.Type {
		case harnessmodel.BlockText:
			texts++
		case harnessmodel.BlockImage:
			images++
		case harnessmodel.BlockToolResult:
			nestedText, nestedImages := countBlockTypes(block.Content)
			texts += nestedText
			images += nestedImages
		}
	}
	return texts, images
}

func TestOffloadedImagePrefixCountMatchesDSHQuanta(t *testing.T) {
	lengths := []int64{4, 4, 4, 4}
	tests := []struct {
		name                    string
		lengths                 []int64
		maxImages, countQuantum int
		maxBytes, byteQuantum   int64
		expected                int
	}{
		{name: "unbounded", lengths: lengths, expected: 0},
		{name: "within bytes", lengths: lengths, maxBytes: 16, expected: 0},
		{name: "within count", lengths: lengths, maxImages: 4, expected: 0},
		{name: "count quantum", lengths: append(append([]int64(nil), lengths...), 4), maxImages: 4, countQuantum: 2, expected: 2},
		{name: "byte quantum crossing", lengths: append(append([]int64(nil), lengths...), 1), maxBytes: 16, byteQuantum: 5, expected: 2},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			actual, err := OffloadedImagePrefixCount(test.lengths, test.maxImages, test.maxBytes, test.countQuantum, test.byteQuantum)
			if err != nil || actual != test.expected {
				t.Fatalf("count=%d error=%v", actual, err)
			}
		})
	}
}

func TestOffloadRequestImagesKeepsStableHighWatermarkPrefix(t *testing.T) {
	const mib int64 = 1024 * 1024
	project := func(count int) []harnessmodel.ContentBlock {
		content := make([]harnessmodel.ContentBlock, count)
		for index := range content {
			content[index] = projectionImage(index, mib)
		}
		messages, err := OffloadRequestImages([]harnessmodel.Message{{Role: harnessmodel.RoleUser, Content: content}}, ImageRequestOffloadPolicy{
			Representation: ImageRepresentationRaw, MaxBytes: 128 * mib, ByteQuantum: 64 * mib,
			Placeholder: func(harnessmodel.ContentBlock) (string, error) { return "omitted", nil },
		})
		if err != nil {
			t.Fatal(err)
		}
		return messages[0].Content
	}
	for _, test := range []struct{ count, omitted, retained int }{
		{128, 0, 128}, {129, 65, 64}, {192, 65, 127}, {193, 129, 64},
	} {
		texts, images := countBlockTypes(project(test.count))
		if texts != test.omitted || images != test.retained {
			t.Fatalf("count=%d text=%d images=%d", test.count, texts, images)
		}
	}
}

func TestOffloadRequestImagesWalksMessagesAndNestedToolResultsInOrder(t *testing.T) {
	first, second, third := projectionImage(0, 2), projectionImage(1, 2), projectionImage(2, 2)
	source := []harnessmodel.Message{
		{Role: harnessmodel.RoleUser, Content: []harnessmodel.ContentBlock{first}},
		{Role: harnessmodel.RoleTool, Content: []harnessmodel.ContentBlock{{Type: harnessmodel.BlockToolResult, ToolCallID: "call", Content: []harnessmodel.ContentBlock{
			{Type: harnessmodel.BlockText, Text: "before"}, second,
		}}}},
		{Role: harnessmodel.RoleUser, Content: []harnessmodel.ContentBlock{third}},
	}
	if count, err := OffloadedImagePrefixCount([]int64{2, 2, 2}, 1, 0, 2, 0); err != nil || count != 2 {
		t.Fatalf("prefix count=%d err=%v", count, err)
	}
	remaining := 2
	_, _, _ = replaceOldestImageBlocks(source[0].Content, &remaining, func(harnessmodel.ContentBlock) (string, error) { return "omitted", nil })
	if remaining != 1 {
		t.Fatalf("remaining after first=%d", remaining)
	}
	probe, replaced, probeErr := replaceOldestImageBlocks(source[1].Content, &remaining, func(harnessmodel.ContentBlock) (string, error) { return "omitted", nil })
	if probeErr != nil || !replaced || remaining != 0 || probe[0].Content[1].Type != harnessmodel.BlockText {
		t.Fatalf("nested probe=%+v replaced=%v remaining=%d err=%v", probe, replaced, remaining, probeErr)
	}
	projected, err := OffloadRequestImages(source, ImageRequestOffloadPolicy{
		Representation: ImageRepresentationRaw, MaxImages: 1, CountQuantum: 2,
		Placeholder: func(block harnessmodel.ContentBlock) (string, error) { return "omitted:" + block.Attachment.Name, nil },
	})
	if err != nil {
		t.Fatal(err)
	}
	if projected[0].Content[0].Text != "omitted:image-0.png" ||
		projected[1].Content[0].Content[1].Text != "omitted:image-1.png" ||
		projected[2].Content[0].Type != harnessmodel.BlockImage {
		t.Fatalf("projected = %+v", projected)
	}
	if source[0].Content[0].Type != harnessmodel.BlockImage || source[1].Content[0].Content[1].Type != harnessmodel.BlockImage {
		t.Fatalf("source mutated = %+v", source)
	}
}

func TestImageProjectionUsesBase64LengthsAndStableText(t *testing.T) {
	image := projectionImage(0, 3)
	projected, err := OffloadRequestImages([]harnessmodel.Message{{Role: harnessmodel.RoleUser, Content: []harnessmodel.ContentBlock{image}}}, ImageRequestOffloadPolicy{
		Representation: ImageRepresentationBase64, MaxBytes: 3,
		Placeholder: func(block harnessmodel.ContentBlock) (string, error) { return OffloadedImageText(block), nil },
	})
	if err != nil {
		t.Fatal(err)
	}
	text := projected[0].Content[0].Text
	if !strings.Contains(text, `image omitted to fit request image limits; "image-0.png"`) ||
		!strings.Contains(text, image.Attachment.AttachmentID) || !strings.Contains(text, "ask the user to attach it again") {
		t.Fatalf("placeholder = %q", text)
	}
	textOnly := ProjectImagesForTextModel([]harnessmodel.Message{{Role: harnessmodel.RoleUser, Content: []harnessmodel.ContentBlock{image}}})
	if textOnly[0].Content[0].Text != "[image omitted because this model accepts text only; attachment sha256:00000000]" {
		t.Fatalf("text-only = %q", textOnly[0].Content[0].Text)
	}
	if handle := RequestImageHandleText(*image.Attachment, 5, 7); handle !=
		`Image "image-0.png" (`+image.Attachment.AttachmentID+`); request preview 5x7px. It may be resized or re-encoded; source dimensions, format, and byte size may differ.` {
		t.Fatalf("handle = %q", handle)
	}
}

func TestOffloadedImagePrefixCountRejectsInvalidLengths(t *testing.T) {
	for _, lengths := range [][]int64{{-1}, {math.MaxInt64, 1}} {
		if _, err := OffloadedImagePrefixCount(lengths, 0, 1, 1, 1); err == nil {
			t.Fatalf("accepted lengths %v", lengths)
		}
	}
	if _, err := OffloadRequestImages([]harnessmodel.Message{{Role: harnessmodel.RoleUser, Content: []harnessmodel.ContentBlock{projectionImage(0, 1)}}}, ImageRequestOffloadPolicy{}); err == nil {
		t.Fatal("accepted incomplete policy")
	}
}

func TestProjectImagesForTextModelLeavesNonImageContentEquivalent(t *testing.T) {
	source := []harnessmodel.Message{{Role: harnessmodel.RoleUser, Content: []harnessmodel.ContentBlock{{Type: harnessmodel.BlockText, Text: "kept"}}}}
	if projected := ProjectImagesForTextModel(source); !reflect.DeepEqual(projected, source) {
		t.Fatalf("projected = %+v", projected)
	}
}

func TestRequiredImageOffloadSkipsMarkedImagesAndProjectionReplacesOnlyThose(t *testing.T) {
	marked := projectionImage(0, 10)
	marked.Offloaded = true
	kept := projectionImage(1, 10)
	nested := projectionImage(2, 10)
	source := []harnessmodel.Message{{Role: harnessmodel.RoleUser, Content: []harnessmodel.ContentBlock{
		{Type: harnessmodel.BlockText, Text: "before"},
		marked,
		kept,
		{Type: harnessmodel.BlockToolResult, Content: []harnessmodel.ContentBlock{nested}},
	}}}
	count, err := RequiredImageOffload(source, ImageRequestOffloadPolicy{
		MaxImages: 1, CountQuantum: 1, Representation: ImageRepresentationRaw,
	})
	if err != nil || count != 1 {
		t.Fatalf("count=%d err=%v", count, err)
	}
	projected, err := ProjectOffloadedImages(source, func(block harnessmodel.ContentBlock) (string, error) {
		return OffloadedImageText(block), nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if source[0].Content[1].Type != harnessmodel.BlockImage || !source[0].Content[1].Offloaded {
		t.Fatalf("source changed: %+v", source[0].Content[1])
	}
	content := projected[0].Content
	if content[0].Text != "before" || content[1].Type != harnessmodel.BlockText ||
		!strings.Contains(content[1].Text, "image omitted to fit request image limits") ||
		!strings.Contains(content[1].Text, "No local normalized image path is available") ||
		content[2].Type != harnessmodel.BlockImage || content[2].Offloaded ||
		content[3].Type != harnessmodel.BlockToolResult || content[3].Content[0].Type != harnessmodel.BlockImage {
		t.Fatalf("projected = %+v", content)
	}
	unmarked := []harnessmodel.Message{{Role: harnessmodel.RoleUser, Content: []harnessmodel.ContentBlock{kept}}}
	same, err := ProjectOffloadedImages(unmarked, func(harnessmodel.ContentBlock) (string, error) { return "unused", nil })
	if err != nil || !reflect.DeepEqual(same, unmarked) {
		t.Fatalf("unmarked projection = %+v err=%v", same, err)
	}
}

func TestOffloadedImagePlaceholderNamesLocalReadOnlyPath(t *testing.T) {
	store, err := OpenLocalAttachmentStore(LocalAttachmentStoreConfig{Home: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	ref := harnessmodel.ImageAttachmentRef{
		AttachmentID: "sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
		MediaType:    "image/png", Bytes: 8, Width: 2, Height: 3, Name: "shot.png",
	}
	block := harnessmodel.ContentBlock{Type: harnessmodel.BlockImage, Attachment: &ref, Offloaded: true}
	hostPath, err := store.ImageHostPath(ref)
	if err != nil {
		t.Fatal(err)
	}
	text, err := OffloadedImagePlaceholder(store, block)
	if err != nil {
		t.Fatal(err)
	}
	want := `[image omitted to fit request image limits; "shot.png" (` + ref.AttachmentID + `).` +
		` Normalized copy (read-only; may be resized or re-encoded): ` + jsonQuote(hostPath) +
		` (2x3px, image/png). Source dimensions, format, and byte size may differ.` +
		` Copy to a writable path ending in .png before editing.]`
	if text != want {
		t.Fatalf("placeholder = %q\nwant %q", text, want)
	}
	declined, err := ResolveImageAttachmentAccess(store, ref, func(string) (string, bool) { return "", false })
	if err != nil || declined != nil {
		t.Fatalf("declined access = %+v err=%v", declined, err)
	}
	if _, err := store.ImageHostPath(harnessmodel.ImageAttachmentRef{AttachmentID: "sha256:nope"}); err == nil {
		t.Fatal("accepted an invalid attachment id")
	}
}

func jsonQuote(value string) string {
	encoded, _ := json.Marshal(value)
	return string(encoded)
}
