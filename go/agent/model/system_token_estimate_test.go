package model

import (
	"reflect"
	"testing"
)

func TestSystemAndToolMessagePrices(t *testing.T) {
	messages := []Message{
		{Role: RoleSystem, Content: []ContentBlock{}},
		{Role: RoleSystem, Content: []ContentBlock{{Type: BlockText, Text: "😀 prompt"}}},
		{Role: RoleTool, Content: []ContentBlock{{Type: BlockToolResult, ToolCallID: "c", Content: []ContentBlock{{Type: BlockText, Text: "result😀"}}}}},
	}
	got := make([]int, 0, len(messages))
	for _, message := range messages {
		got = append(got, EstimateMessageTokens(message))
	}
	if !reflect.DeepEqual(got, []int{0, 7, 10}) {
		t.Fatalf("canonical system/tool estimates=%v", got)
	}
}
