package harness

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	harnessmcp "github.com/phodal/pero/harness/go/mcp"
	point "github.com/phodal/pero/harness/go/runtime"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

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
	client := transport.(*authenticationTransport).HTTPClient
	response, err := client.Get(origin.URL)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusTemporaryRedirect || reached.Load() {
		t.Fatal("MCP followed credential-bearing redirect")
	}
}

// Synthetic endpoint only: never uses provider credentials or a model.
func TestMCPHTTPAuthenticationIsTerminalAndDiagnosticsAreRedacted(t *testing.T) {
	for _, status := range []int{http.StatusUnauthorized, http.StatusForbidden} {
		for _, failStartup := range []bool{false, true} {
			t.Run(fmt.Sprintf("%d/strict=%v", status, failStartup), func(t *testing.T) {
				const secret = "synthetic-mcp-secret"
				var attempts atomic.Int32
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					attempts.Add(1)
					if r.Header.Get("Authorization") != "Bearer "+secret {
						t.Error("missing explicit credential")
					}
					w.Header().Set("Content-Type", "application/json")
					w.WriteHeader(status)
					fmt.Fprintf(w, `{"jsonrpc":"2.0","id":1,"error":{"code":-32000,"message":%q}}`, secret)
				}))
				defer server.Close()
				var logs lockedLog
				runtime := point.New(point.Options{Logger: slog.New(slog.NewTextHandler(&logs, nil))})
				ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				defer cancel()
				result := make(chan error, 1)
				config := MCPServerConfig{Transport: MCPStreamableHTTP, URL: server.URL + "?token=" + secret, Headers: map[string]string{"Authorization": "Bearer " + secret}}
				definition := point.Define(point.Descriptor{ID: "auth-fixture"}, point.DecodeJSON[struct{}], func(run *point.Run, _ struct{}) (point.Dispose, error) {
					err := harnessmcp.MountConnection(ctx, run, harnessmcp.ConnectionOptions{ServerName: "fixture", Reconnect: harnessmcp.ReconnectPolicy{Enabled: true, InitialDelay: time.Millisecond, MaxDelay: time.Millisecond, MaxAttempts: 2}, FailOnStartupError: failStartup, TransportFactory: func() (mcp.Transport, error) { return newMCPTransport(config) }}, func(*point.Run, string, []harnessmcp.Tool) (point.Dispose, error) {
						t.Error("unexpected tools publication")
						return nil, nil
					})
					result <- err
					return nil, err
				})
				instance, err := runtime.Mount(runtime.Root(), "auth-fixture", definition, nil)
				if err != nil {
					t.Fatal(err)
				}
				select {
				case err = <-result:
				case <-ctx.Done():
					t.Fatal("mount timed out")
				}
				if failStartup && !errors.Is(err, harnessmcp.ErrAuthentication) {
					t.Fatalf("missing typed auth rejection: %v", err)
				}
				if !failStartup && err != nil {
					t.Fatal(err)
				}
				if strings.Contains(fmt.Sprint(err), secret) {
					t.Fatal("startup error leaked credential")
				}
				if err = instance.WaitStable(ctx); err != nil {
					t.Fatal(err)
				}
				// Several complete retry windows; if misclassified, all attempts finish here.
				time.Sleep(50 * time.Millisecond)
				if got := attempts.Load(); got != 1 {
					t.Fatalf("auth request retried: %d", got)
				}
				if err = runtime.Dispose(ctx); err != nil {
					t.Fatal(err)
				}
				if strings.Contains(logs.String(), secret) || strings.Contains(logs.String(), server.URL) {
					t.Fatalf("secret endpoint leaked in logs: %s", logs.String())
				}
			})
		}
	}
}

type lockedLog struct {
	mu   sync.Mutex
	data bytes.Buffer
}

func (b *lockedLog) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.data.Write(p)
}
func (b *lockedLog) String() string { b.mu.Lock(); defer b.mu.Unlock(); return b.data.String() }

func TestMCPHTTPTransientFailureStillReconnectsWithoutLeakingBody(t *testing.T) {
	const secret = "synthetic-mcp-response-secret"
	var attempts atomic.Int32
	sdkServer := mcp.NewServer(&mcp.Implementation{Name: "fixture", Version: "1"}, nil)
	sdkHandler := mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return sdkServer }, nil)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost && attempts.Add(1) <= 2 {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusInternalServerError)
			fmt.Fprintf(w, `{"jsonrpc":"2.0","id":1,"error":{"code":-32000,"message":%q}}`, secret)
			return
		}
		sdkHandler.ServeHTTP(w, r)
	}))
	defer server.Close()
	var logs lockedLog
	runtime := point.New(point.Options{Logger: slog.New(slog.NewTextHandler(&logs, nil))})
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	published := make(chan struct{}, 1)
	var disposed atomic.Int32
	config := MCPServerConfig{Transport: MCPStreamableHTTP, URL: server.URL + "?token=" + secret}
	definition := point.Define(point.Descriptor{ID: "reconnect-fixture"}, point.DecodeJSON[struct{}], func(run *point.Run, _ struct{}) (point.Dispose, error) {
		err := harnessmcp.MountConnection(ctx, run, harnessmcp.ConnectionOptions{ServerName: "fixture", Reconnect: harnessmcp.ReconnectPolicy{Enabled: true, InitialDelay: time.Millisecond, MaxDelay: time.Millisecond, MaxAttempts: 2}, TransportFactory: func() (mcp.Transport, error) { return newMCPTransport(config) }}, func(*point.Run, string, []harnessmcp.Tool) (point.Dispose, error) {
			published <- struct{}{}
			return func(context.Context) error { disposed.Add(1); return nil }, nil
		})
		return nil, err
	})
	instance, err := runtime.Mount(runtime.Root(), "reconnect-fixture", definition, nil)
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-published:
	case <-ctx.Done():
		t.Fatalf("reconnect did not publish: %s", logs.String())
	}
	if err = instance.WaitStable(ctx); err != nil {
		t.Fatal(err)
	}
	if err = runtime.Dispose(ctx); err != nil {
		t.Fatal(err)
	}
	if attempts.Load() < 3 {
		t.Fatal("supervisor did not reconnect after both initial handshake failures")
	}
	if disposed.Load() != 1 {
		t.Fatalf("generation disposer calls: %d", disposed.Load())
	}
	if strings.Contains(logs.String(), secret) || strings.Contains(logs.String(), server.URL) {
		t.Fatalf("diagnostic leaked remote payload: %s", logs.String())
	}
}

func TestMCPSafeErrorPreservesClassificationNotSecretText(t *testing.T) {
	for _, cause := range []error{context.Canceled, context.DeadlineExceeded, harnessmcp.ErrAuthentication, errors.New("synthetic-secret")} {
		wrapped := fmt.Errorf("endpoint ?token=synthetic-secret: %w", cause)
		safe := harnessmcp.SafeError(wrapped)
		if !errors.Is(safe, cause) || strings.Contains(safe.Error(), "synthetic-secret") {
			t.Fatalf("unsafe error boundary: %v", safe)
		}
	}
	if harnessmcp.SafeError(nil) != nil {
		t.Fatal("nil error changed")
	}
}
