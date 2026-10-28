package harness

import (
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestMCPHTTPRedirectDoesNotForwardConfiguredCredentials(t *testing.T) {
	var reached atomic.Bool
	destination := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { reached.Store(true); w.WriteHeader(200) }))
	defer destination.Close()
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer fixture" || r.Header.Get("X-Key") != "fixture" {
			t.Error("explicit origin credentials missing")
		}
		http.Redirect(w, r, destination.URL, http.StatusTemporaryRedirect)
	}))
	defer origin.Close()
	transport, err := newMCPTransport(MCPServerConfig{Transport: MCPStreamableHTTP, URL: origin.URL, Headers: map[string]string{"Authorization": "Bearer fixture", "X-Key": "fixture"}})
	if err != nil {
		t.Fatal(err)
	}
	client := transport.(*mcp.StreamableClientTransport).HTTPClient
	response, err := client.Get(origin.URL)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusTemporaryRedirect || reached.Load() {
		t.Fatal("MCP followed credential-bearing redirect")
	}
}
