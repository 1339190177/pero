package session

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	harnessmodel "github.com/phodal/pero/harness/go/model"
)

func TestImageOffloadMarksRetainedInputWithoutReplacingTheMessage(t *testing.T) {
	session := NewSession("images", "/tmp")
	source, err := session.Append("user/message", Message{Role: RoleUser, Content: []ContentBlock{
		{Type: BlockText, Text: "before"},
		imageBlock("one"), imageBlock("two"),
		{Type: BlockText, Text: "after"},
		imageBlock("three"),
	}})
	if err != nil {
		t.Fatal(err)
	}
	before, err := session.DeriveMessages()
	if err != nil {
		t.Fatal(err)
	}
	decision, err := session.Append("image/offload", imageOffload(source.Seq, 1))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(source.Data), "offloaded") || strings.Contains(string(decision.Data), "offloaded") {
		t.Fatalf("durable events stored the derived mark: %s %s", source.Data, decision.Data)
	}
	if surface := session.SurfaceSeqs(); len(surface) != 1 || surface[0] != source.Seq {
		t.Fatalf("surface = %v", surface)
	}
	messages, err := session.DeriveMessages()
	if err != nil {
		t.Fatal(err)
	}
	if marks := offloadedMarks(messages); !equalBools(marks, []bool{false, true, false}) {
		t.Fatalf("marks = %v", marks)
	}
	if !reflect.DeepEqual(messages[0].Content[0], before[0].Content[0]) || messages[0].Content[3].Text != "after" {
		t.Fatalf("unselected blocks changed: %+v", messages[0].Content)
	}
	if _, err := session.Append("image/offload", imageOffload(source.Seq, 0, 2)); err != nil {
		t.Fatal(err)
	}
	messages, err = session.DeriveMessages()
	if err != nil {
		t.Fatal(err)
	}
	if marks := offloadedMarks(messages); !equalBools(marks, []bool{true, true, true}) {
		t.Fatalf("marks = %v", marks)
	}
}

func TestImageOffloadRejectsMalformedRepeatedAndInactiveTargets(t *testing.T) {
	session := NewSession("images", "/tmp")
	source, err := session.Append("user/message", Message{Role: RoleUser, Content: []ContentBlock{imageBlock("one"), imageBlock("two")}})
	if err != nil {
		t.Fatal(err)
	}
	events := len(session.Events())
	rejected := []any{
		nil,
		map[string]any{},
		map[string]any{"targets": []any{}},
		map[string]any{"targets": "bad"},
		map[string]any{"targets": []any{nil}},
		map[string]any{"targets": []any{map[string]any{"seq": source.Seq, "imageIndexes": []any{}}}},
		map[string]any{"targets": []any{map[string]any{"seq": source.Seq, "imageIndexes": []any{0}, "extra": true}}},
		map[string]any{"targets": []any{map[string]any{"seq": source.Seq, "imageIndexes": []any{0}}}, "extra": true},
		map[string]any{"targets": []any{map[string]any{"seq": -1, "imageIndexes": []any{0}}}},
		map[string]any{"targets": []any{map[string]any{"seq": source.Seq, "imageIndexes": []any{-1}}}},
		map[string]any{"targets": []any{map[string]any{"seq": source.Seq, "imageIndexes": []any{0.5}}}},
		map[string]any{"targets": []any{map[string]any{"seq": source.Seq, "imageIndexes": []any{"0"}}}},
		map[string]any{"targets": []any{map[string]any{"seq": source.Seq, "imageIndexes": []any{1, 0}}}},
		map[string]any{"targets": []any{map[string]any{"seq": source.Seq, "imageIndexes": []any{0, 0}}}},
		map[string]any{"targets": []any{map[string]any{"seq": source.Seq, "imageIndexes": []any{2}}}},
		map[string]any{"targets": []any{map[string]any{"seq": source.Seq + 1, "imageIndexes": []any{0}}}},
		map[string]any{"targets": []any{
			map[string]any{"seq": source.Seq, "imageIndexes": []any{0}},
			map[string]any{"seq": source.Seq, "imageIndexes": []any{1}},
		}},
		json.RawMessage(`{"targets":[{"seq":-0,"imageIndexes":[0]}]}`),
	}
	for _, payload := range rejected {
		if _, err := session.Append("image/offload", payload); err == nil || !strings.Contains(err.Error(), "image/offload") {
			t.Fatalf("payload %#v error = %v", payload, err)
		}
	}
	if len(session.Events()) != events {
		t.Fatalf("rejected payloads were committed: %d", len(session.Events()))
	}
	if _, err := session.Append("image/offload", imageOffload(source.Seq, 0)); err != nil {
		t.Fatal(err)
	}
	if _, err := session.Append("image/offload", imageOffload(source.Seq, 0)); err == nil || !strings.Contains(err.Error(), "already offloaded") {
		t.Fatalf("repeated index error = %v", err)
	}
	assistant, err := session.Append("assistant/message", map[string]any{
		"turn": 1, "step": 1, "message": Message{Role: harnessmodel.RoleAssistant, Content: []ContentBlock{imageBlock("assistant")}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := session.Append("image/offload", imageOffload(assistant.Seq, 0)); err == nil || !strings.Contains(err.Error(), "user/message") {
		t.Fatalf("assistant target error = %v", err)
	}
	if _, err := session.AppendReplacement("user/message", Message{Role: RoleUser, Content: []ContentBlock{{Type: BlockText, Text: "summary"}}}, SurfaceRange{Start: source.Seq, End: source.Seq}, []uint64{source.Seq}); err != nil {
		t.Fatal(err)
	}
	if _, err := session.Append("image/offload", imageOffload(source.Seq, 1)); err == nil || !strings.Contains(err.Error(), "not a current") {
		t.Fatalf("shadowed target error = %v", err)
	}
	if _, err := session.DeriveMessages(); err != nil {
		t.Fatalf("historical omission broke replay: %v", err)
	}
}

func TestOffloadOldestImagesSkipsAssistantOutputAndAlreadyOmittedImages(t *testing.T) {
	session := NewSession("images", "/tmp")
	user, err := session.Append("user/message", Message{Role: RoleUser, Content: []ContentBlock{imageBlock("one"), imageBlock("two")}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := session.Append("assistant/message", map[string]any{
		"turn": 1, "step": 1, "message": Message{Role: harnessmodel.RoleAssistant, Content: []ContentBlock{imageBlock("assistant")}},
	}); err != nil {
		t.Fatal(err)
	}
	tool, err := session.Append("tool/result", ContentBlock{
		Type: BlockToolResult, ToolCallID: "shot", Content: []ContentBlock{imageBlock("tool")},
	})
	if err != nil {
		t.Fatal(err)
	}
	seqs := session.SurfaceSeqs()
	recorded, err := session.OffloadOldestImages(seqs, 1)
	if err != nil || !recorded {
		t.Fatalf("recorded=%v err=%v", recorded, err)
	}
	recorded, err = session.OffloadOldestImages(seqs, 1)
	if err != nil || !recorded {
		t.Fatalf("second recorded=%v err=%v", recorded, err)
	}
	recorded, err = session.OffloadOldestImages(seqs, 1)
	if err != nil || !recorded {
		t.Fatalf("third recorded=%v err=%v", recorded, err)
	}
	if recorded, err := session.OffloadOldestImages(seqs, 1); err != nil || recorded {
		t.Fatalf("exhausted recorded=%v err=%v", recorded, err)
	}
	decisions := make([]imageOffloadTarget, 0, 3)
	for _, event := range session.Events() {
		if event.Type != "image/offload" {
			continue
		}
		var payload struct {
			Targets []imageOffloadTarget `json:"targets"`
		}
		if err := json.Unmarshal(event.Data, &payload); err != nil {
			t.Fatal(err)
		}
		decisions = append(decisions, payload.Targets...)
	}
	if len(decisions) != 3 || decisions[0].Seq != user.Seq || decisions[0].ImageIndexes[0] != 0 ||
		decisions[1].Seq != user.Seq || decisions[1].ImageIndexes[0] != 1 ||
		decisions[2].Seq != tool.Seq || decisions[2].ImageIndexes[0] != 0 {
		t.Fatalf("decisions = %+v", decisions)
	}
	messages, err := session.DeriveMessages()
	if err != nil {
		t.Fatal(err)
	}
	if !messages[0].Content[0].Offloaded || !messages[0].Content[1].Offloaded || messages[1].Content[0].Offloaded || !messages[2].Content[0].Content[0].Offloaded {
		t.Fatalf("messages = %+v", messages)
	}
}

func imageBlock(name string) ContentBlock {
	return ContentBlock{Type: harnessmodel.BlockImage, MIMEType: "image/png", Data: []byte(name)}
}

func imageOffload(seq uint64, indexes ...int) map[string]any {
	return map[string]any{"targets": []any{map[string]any{"seq": seq, "imageIndexes": indexes}}}
}

func offloadedMarks(messages []Message) []bool {
	marks := make([]bool, 0)
	var walk func([]ContentBlock)
	walk = func(blocks []ContentBlock) {
		for _, block := range blocks {
			if block.Type == harnessmodel.BlockImage {
				marks = append(marks, block.Offloaded)
			}
			walk(block.Content)
		}
	}
	for _, message := range messages {
		walk(message.Content)
	}
	return marks
}

func equalBools(left, right []bool) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}
