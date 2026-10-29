package harness

import harnesstool "github.com/phodal/pero/harness/go/tool"

type ExecutionMode = harnesstool.ExecutionMode

const (
	ExecutionParallel = harnesstool.ExecutionParallel
	ExecutionSerial   = harnesstool.ExecutionSerial
)

type ToolResult = harnesstool.Result
type ToolRestriction = harnesstool.Restriction
