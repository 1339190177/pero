package openai

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/phodal/pero/harness/go/harness"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestSSELineBufferIsBounded(t *testing.T) {
	b := &doneCheckingBody{ReadCloser: io.NopCloser(bytes.NewReader(bytes.Repeat([]byte{'x'}, 1024*1024+1)))}
	_, err := io.Copy(io.Discard, b)
	if err == nil || !strings.Contains(err.Error(), "bounded output") {
		t.Fatalf("missing stream bound: %v", err)
	}
}

func TestCallbackCancellationCannotBecomeSuccess(t *testing.T) {
	a, _ := fixture(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		event(w, `{"choices":[{"index":0,"delta":{"content":"hello"},"finish_reason":"stop"}]}`)
		event(w, "[DONE]")
	})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	_, err := a.Generate(ctx, request(), func(harness.StreamChunk) error { cancel(); return nil })
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled response became success: %v", err)
	}
}

func fixture(t *testing.T, h http.HandlerFunc) (*Adapter, *httptest.Server) {
	t.Helper()
	s := httptest.NewServer(h)
	t.Cleanup(s.Close)
	a, err := New(Config{Endpoint: s.URL + "/v1", APIKey: "fixture-not-a-real-key", ProviderName: "fixture", Models: []ModelConfig{{ID: "fixture-model", ContextWindow: 8192}}, StreamIdleTimeoutMS: 500})
	if err != nil {
		t.Fatal(err)
	}
	return a, s
}
func request() harness.GenerateRequest {
	return harness.GenerateRequest{Provider: "fixture", Model: "fixture-model", Messages: []harness.Message{{Role: harness.RoleUser, Content: []harness.ContentBlock{{Type: harness.BlockText, Text: "fixture prompt"}}}}}
}
func event(w http.ResponseWriter, s string) {
	fmt.Fprintf(w, "data: %s\n\n", s)
	if f, ok := w.(http.Flusher); ok {
		f.Flush()
	}
}
func TestExplicitConfiguration(t *testing.T) {
	for _, c := range []Config{{}, {Endpoint: "https://example.com", APIKey: "test", ProviderName: "x"}, {Endpoint: "http://example.com", APIKey: "test", ProviderName: "x", Models: []ModelConfig{{ID: "m"}}}, {Endpoint: "https://u:p@example.com", APIKey: "test", ProviderName: "x", Models: []ModelConfig{{ID: "m"}}}, {Endpoint: "https://example.com", APIKey: "test", ProviderName: "x", Models: []ModelConfig{{ID: "m", InputModalities: []string{"image"}}}}} {
		if _, err := New(c); err == nil {
			t.Fatalf("accepted unsafe/implicit configuration %+v", c)
		}
	}
}
func TestTextStreamNoProductHeadersOrPayload(t *testing.T) {
	a, _ := fixture(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/chat/completions" {
			t.Errorf("path %s", r.URL.Path)
		}
		for k := range r.Header {
			if strings.Contains(strings.ToLower(k), "deepseek") || strings.Contains(strings.ToLower(k), "point") {
				t.Errorf("product header %s", k)
			}
		}
		var b map[string]any
		if err := json.NewDecoder(r.Body).Decode(&b); err != nil {
			t.Error(err)
		}
		if _, ok := b["dsh_session_log"]; ok {
			t.Error("session log uploaded")
		}
		if b["model"] != "fixture-model" {
			t.Error("wrong model")
		}
		w.Header().Set("Content-Type", "text/event-stream")
		event(w, `{"choices":[{"index":0,"delta":{"content":"hello"},"finish_reason":null}]}`)
		event(w, `{"choices":[{"index":0,"delta":{},"finish_reason":"stop"}]}`)
		event(w, "[DONE]")
	})
	var chunks int
	c, err := a.Generate(context.Background(), request(), func(harness.StreamChunk) error { chunks++; return nil })
	if err != nil {
		t.Fatal(err)
	}
	if c.Message.Content[0].Text != "hello" || chunks == 0 || c.Finish != harness.FinishStop {
		t.Fatalf("bad completion %+v", c)
	}
}
func TestToolArgumentsAssemble(t *testing.T) {
	a, _ := fixture(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		event(w, `{"choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"id":"call-1","type":"function","function":{"name":"fixture_tool","arguments":"{\"x\":"}}]}}]}`)
		event(w, `{"choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"function":{"arguments":"1}"}}]},"finish_reason":"tool_calls"}]}`)
		event(w, "[DONE]")
	})
	c, err := a.Generate(context.Background(), request(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if c.Finish != harness.FinishToolCalls || string(c.Message.Content[0].Arguments) != `{"x":1}` {
		t.Fatalf("bad tools %+v", c)
	}
}
func TestRejectUnsupportedWithoutNetwork(t *testing.T) {
	var calls atomic.Int32
	a, _ := fixture(t, func(w http.ResponseWriter, r *http.Request) { calls.Add(1) })
	for _, kind := range []harness.BlockType{harness.BlockImage, harness.BlockFile, "unknown"} {
		r := request()
		r.Messages[0].Content[0].Type = kind
		if _, err := a.Generate(context.Background(), r, nil); err == nil {
			t.Errorf("accepted %s", kind)
		}
	}
	r := request()
	r.Model = "unknown"
	if _, err := a.Generate(context.Background(), r, nil); err == nil {
		t.Error("accepted unknown model")
	}
	r = request()
	r.PrepareExtensions = func(context.Context, json.RawMessage) (*harness.PreparedRequestExtensions, error) {
		t.Error("extension invoked")
		return nil, nil
	}
	if _, err := a.Generate(context.Background(), r, nil); err == nil {
		t.Error("accepted extension")
	}
	if calls.Load() != 0 {
		t.Fatal("unexpected network")
	}
}
func TestCancellationStopsStream(t *testing.T) {
	stopped := make(chan struct{})
	a, _ := fixture(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(200)
		w.(http.Flusher).Flush()
		<-r.Context().Done()
		close(stopped)
	})
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	_, err := a.Generate(ctx, request(), nil)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("expected cancellation, got %v", err)
	}
	select {
	case <-stopped:
	case <-time.After(time.Second):
		t.Fatal("request remained active")
	}
}
func TestTruncatedStreamAndMalformedToolFail(t *testing.T) {
	for _, data := range []string{`{"choices":[{"index":0,"delta":{"content":"partial"}}]}`, `{"choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"id":"c","function":{"name":"tool","arguments":"not-json"}}]},"finish_reason":"tool_calls"}]}`} {
		a, _ := fixture(t, func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/event-stream")
			event(w, data)
			if strings.Contains(data, "not-json") {
				event(w, "[DONE]")
			}
		})
		if _, err := a.Generate(context.Background(), request(), nil); err == nil {
			t.Error("accepted malformed/incomplete stream")
		}
	}
}
func TestHTTPErrorAndRedirectNeverRetry(t *testing.T) {
	for _, status := range []int{http.StatusTooManyRequests, http.StatusTemporaryRedirect} {
		var calls atomic.Int32
		a, _ := fixture(t, func(w http.ResponseWriter, r *http.Request) {
			calls.Add(1)
			w.Header().Set("Location", "http://127.0.0.1:1/forbidden")
			w.Header().Set("Retry-After", "1")
			w.WriteHeader(status)
			fmt.Fprint(w, `{"error":{"message":"fixture failure","type":"rate_limit"}}`)
		})
		if _, err := a.Generate(context.Background(), request(), nil); err == nil {
			t.Error("accepted HTTP failure")
		}
		if calls.Load() != 1 {
			t.Fatalf("retried %d times", calls.Load())
		}
	}
}

func TestProviderErrorsDoNotEchoRemoteSecrets(t *testing.T) {
	a, _ := fixture(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(401)
		fmt.Fprint(w, `{"error":{"message":"fixture-not-a-real-key private-prompt","type":"auth"}}`)
	})
	_, err := a.Generate(context.Background(), request(), nil)
	if err == nil || strings.Contains(err.Error(), "fixture-not-a-real-key") || strings.Contains(err.Error(), "private-prompt") {
		t.Fatalf("unsafe error %v", err)
	}
}

func TestDoneWithoutFinishAndIncompleteToolsFail(t *testing.T) {
	for _, data := range []string{`{"choices":[{"index":0,"delta":{"content":"partial"}}]}`, `{"choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"function":{"arguments":"{}"}}]},"finish_reason":"tool_calls"}]}`} {
		a, _ := fixture(t, func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/event-stream")
			event(w, data)
			event(w, "[DONE]")
		})
		if _, err := a.Generate(context.Background(), request(), nil); err == nil {
			t.Fatal("accepted incomplete response")
		}
	}
}
