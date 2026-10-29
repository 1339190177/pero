package model

import "testing"

func TestEstimateMessageTokensUsesUTF16AndCanonicalToolFraming(t *testing.T) {
	if got := EstimateMessageTokens(Message{Content: []ContentBlock{{Type: BlockText, Text: "😀"}}}); got != 9 {
		t.Fatalf("emoji message tokens = %d, want 9", got)
	}
	nested := Message{Role: RoleTool, Content: []ContentBlock{{
		Type: BlockToolResult, Content: []ContentBlock{{Type: BlockText, Text: "text"}},
	}}}
	if got := EstimateMessageTokens(nested); got != 9 {
		t.Fatalf("nested tool result tokens = %d, want 9", got)
	}
}

func TestEstimateMessageTokensIgnoresImageOffloadMark(t *testing.T) {
	image := ContentBlock{Type: BlockImage, Data: []byte{1, 2, 3, 4, 5, 6, 7, 8}, MIMEType: "image/png", Attachment: &ImageAttachmentRef{
		AttachmentID: "sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
		MediaType:    "image/png", Width: 2, Height: 3, Name: "shot.png",
	}}
	unmarked := EstimateMessageTokens(Message{Content: []ContentBlock{image}})
	image.Offloaded = true
	marked := EstimateMessageTokens(Message{Content: []ContentBlock{image}})
	if marked != unmarked {
		t.Fatalf("offload mark changed the heuristic: marked=%d unmarked=%d", marked, unmarked)
	}
}
