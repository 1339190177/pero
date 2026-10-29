package harness

import (
	"context"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	point "github.com/phodal/pero/harness/go/runtime"
)

// tools/list_changed must replace the registered MCP generation. The server
// fixture speaks the SDK's in-memory transport; no subprocess or network.
func TestMCPToolListChangedUpdatesRegisteredTools(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()

	server := mcp.NewServer(&mcp.Implementation{Name: "list-changed-fixture", Version: "1"}, nil)
	add := func(name, description string) {
		t.Helper()
		server.AddTool(&mcp.Tool{Name: name, Description: description, InputSchema: map[string]any{"type": "object"}}, nil)
	}
	add("alpha", "first")
	clientTransport, serverTransport := mcp.NewInMemoryTransports()
	session, err := server.Connect(ctx, serverTransport, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = session.Close() })

	h := retirementHarness(t)
	agent, err := h.Agents().Create(ctx, AgentOptions{ID: "mcp-list-changed", Configure: func(ctx context.Context, run *point.Run, _ *Agent) error {
		return MountMCPServers(ctx, run, h.Tools(), h.Prompts(), []MCPServerConfig{{
			Name: "fixture", TestTransport: clientTransport, FailOnStartupError: true,
		}})
	}})
	if err != nil {
		t.Fatal(err)
	}

	alpha := PublicMCPToolName("fixture", "alpha")
	beta := PublicMCPToolName("fixture", "beta")
	assertFixtureTools(t, h.Tools(), agent.Scope(), map[string]string{alpha: "first"})

	add("beta", "second")
	waitForFixtureTools(t, ctx, h.Tools(), agent.Scope(), map[string]string{alpha: "first", beta: "second"})

	server.RemoveTools("alpha")
	waitForFixtureTools(t, ctx, h.Tools(), agent.Scope(), map[string]string{beta: "second"})

	add("beta", "revised")
	waitForFixtureTools(t, ctx, h.Tools(), agent.Scope(), map[string]string{beta: "revised"})
	if _, ok := h.Tools().Resolve(agent.Scope(), alpha); ok {
		t.Fatal("removed MCP tool stayed registered")
	}
	if _, ok := h.Tools().Resolve(agent.Scope(), beta); !ok {
		t.Fatal("updated MCP tool was not registered")
	}
}

func fixtureMCPTools(registry *ToolRegistry, scope *point.Scope, prefix string) map[string]string {
	got := map[string]string{}
	for _, schema := range registry.Schemas(scope) {
		if strings.HasPrefix(schema.Name, prefix) {
			got[schema.Name] = schema.Description
		}
	}
	return got
}

func assertFixtureTools(t *testing.T, registry *ToolRegistry, scope *point.Scope, want map[string]string) {
	t.Helper()
	if got := fixtureMCPTools(registry, scope, "mcp__fixture__"); !reflect.DeepEqual(got, want) {
		t.Fatalf("registered MCP tools = %#v, want %#v", got, want)
	}
}

func waitForFixtureTools(t *testing.T, ctx context.Context, registry *ToolRegistry, scope *point.Scope, want map[string]string) {
	t.Helper()
	deadline, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	ticker := time.NewTicker(5 * time.Millisecond)
	defer ticker.Stop()
	for {
		if got := fixtureMCPTools(registry, scope, "mcp__fixture__"); reflect.DeepEqual(got, want) {
			return
		}
		select {
		case <-deadline.Done():
			t.Fatalf("registered MCP tools = %#v, want %#v", fixtureMCPTools(registry, scope, "mcp__fixture__"), want)
		case <-ticker.C:
		}
	}
}
