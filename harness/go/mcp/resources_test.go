package mcp

import (
	"strings"
	"testing"
)

func TestRenderResourceResultRedactsBlobAndKeepsOrder(t *testing.T) {
	raw := []byte(`{"contents":[{"uri":"docs://text","mimeType":"text/plain","text":"Read this guide."},{"uri":"docs://binary","mimeType":"application/octet-stream","blob":"AQIDBA=="}]}`)
	text, err := RenderResourceResult("docs", raw)
	if err != nil {
		t.Fatal(err)
	}
	want := "MCP server: docs\n" +
		`{"contents":[{"uri":"docs://text","mimeType":"text/plain","text":"Read this guide."},` +
		`{"uri":"docs://binary","mimeType":"application/octet-stream","blob":` +
		`"[binary resource: 8 base64 characters; available to programmatic callers]"}]}`
	if text != want {
		t.Fatalf("rendered = %q", text)
	}
	if strings.Contains(text, "AQIDBA==") {
		t.Fatal("binary payload leaked into model text")
	}
}

