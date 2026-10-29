package session

import harnessmodel "github.com/phodal/pero/harness/go/model"

type SessionID = harnessmodel.SessionID
type ToolCallID = harnessmodel.ToolCallID
type Message = harnessmodel.Message
type ContentBlock = harnessmodel.ContentBlock
type ToolSchema = harnessmodel.ToolSchema
type ReasoningEffort = harnessmodel.ReasoningEffort
type Event = harnessmodel.Event
type SurfaceRange = harnessmodel.SurfaceRange
type SessionHeader = harnessmodel.SessionHeader

const (
	RoleTool        = harnessmodel.RoleTool
	RoleUser        = harnessmodel.RoleUser
	BlockText       = harnessmodel.BlockText
	BlockToolCall   = harnessmodel.BlockToolCall
	BlockToolResult = harnessmodel.BlockToolResult
)

func cloneMessage(message Message) Message { return harnessmodel.CloneMessage(message) }
func cloneMessages(messages []Message) []Message {
	return harnessmodel.CloneMessages(messages)
}
