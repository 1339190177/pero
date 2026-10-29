package mcp

import (
	"encoding/json"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	harnessmodel "github.com/phodal/pero/harness/go/model"
	harnesstool "github.com/phodal/pero/harness/go/tool"
)

// canonicalResult retains the protocol content alongside its machine-readable
// result; model projection must never replace or infer structuredContent.
type canonicalResult struct {
	Content           []mcp.Content `json:"content"`
	StructuredContent any           `json:"structuredContent,omitempty"`
}

// Match DSH's supportedOutputSchema: unsupported advertised vocabulary falls
// back to unconstrained JSON rather than making the whole server unavailable.
func canonicalOutputSchema(name string, advertised any) json.RawMessage {
	structured := json.RawMessage(`{}`)
	required := []string{"content"}
	if advertised != nil {
		if raw, err := json.Marshal(advertised); err == nil {
			if _, err := harnesstool.CompileOutputSchema(name, raw); err == nil {
				structured = raw
				required = append(required, "structuredContent")
			}
		}
	}
	raw, _ := json.Marshal(map[string]any{
		"type":       "object",
		"properties": map[string]json.RawMessage{"content": json.RawMessage(`{"type":"array","items":{}}`), "structuredContent": structured},
		"required":   required, "additionalProperties": false,
	})
	return raw
}

// RenderToolOutput projects an accepted canonical value after a tool policy
// rewrites it, using the same content conversion as the initial MCP response.
func RenderToolOutput(raw json.RawMessage) ([]harnessmodel.ContentBlock, error) {
	var result mcp.CallToolResult
	if err := json.Unmarshal(raw, &result); err != nil {
		return nil, err
	}
	converted, err := fromMCPResult(&result)
	return converted.Content, err
}
