// Package openai provides explicit, text-only OpenAI-compatible chat completions.
// Streaming assembly is adapted from the pinned DSH port; product routing,
// telemetry, identity persistence, Files uploads and settings discovery are absent.
package openai

import (
	"context"
	"errors"
	"fmt"
	openaisdk "github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/option"
	"github.com/phodal/pero/harness/go/harness"
	"github.com/phodal/pero/harness/go/llm"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
)

var errStreamClosed = errors.New("SSE stream ended without [DONE]")

type ModelConfig struct {
	ID, Name, Description    string
	ContextWindow, MaxTokens int
	InputModalities          []string
	Reasoning                *llm.ModelReasoningInfo
}
type Config struct {
	Endpoint, APIKey, ProviderName string
	Models                         []ModelConfig
	Client                         *http.Client
	RetryPolicy                    *llm.RetryPolicyConfig
	StreamIdleTimeoutMS            int64
}
type Adapter struct {
	client            openaisdk.ChatCompletionService
	models            map[string]ModelConfig
	providerName      string
	retryPolicy       llm.ResolvedRetryPolicy
	streamIdleTimeout time.Duration
}

func New(c Config) (*Adapter, error) {
	u, err := url.Parse(c.Endpoint)
	if err != nil || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return nil, errors.New("openai adapter: explicit endpoint without userinfo/query/fragment required")
	}
	if u.Scheme != "https" {
		ip := net.ParseIP(u.Hostname())
		if u.Scheme != "http" || ip == nil || !ip.IsLoopback() {
			return nil, errors.New("openai adapter: HTTPS required except loopback fixtures")
		}
	}
	if strings.TrimSpace(c.APIKey) == "" || strings.TrimSpace(c.ProviderName) == "" || len(c.Models) == 0 {
		return nil, errors.New("openai adapter: explicit credential, provider and model catalog required")
	}
	u.Path = strings.TrimSuffix(strings.TrimSuffix(u.Path, "/"), "/chat/completions") + "/"
	models := map[string]ModelConfig{}
	for _, m := range c.Models {
		if m.ID == "" || m.ContextWindow < 0 || m.MaxTokens < 0 {
			return nil, errors.New("openai adapter: invalid model declaration")
		}
		if _, ok := models[m.ID]; ok {
			return nil, errors.New("openai adapter: duplicate model")
		}
		for _, v := range m.InputModalities {
			if v != "text" {
				return nil, errors.New("openai adapter: only text input is supported")
			}
		}
		m.InputModalities = []string{"text"}
		if m.Name == "" {
			m.Name = m.ID
		}
		if m.Reasoning != nil {
			clone := *m.Reasoning
			clone.Efforts = append([]llm.ReasoningEffortInfo(nil), m.Reasoning.Efforts...)
			m.Reasoning = &clone
		}
		models[m.ID] = m
	}
	policy, err := llm.ResolveRetryPolicy(c.RetryPolicy)
	if err != nil {
		return nil, err
	}
	if c.StreamIdleTimeoutMS == 0 {
		c.StreamIdleTimeoutMS = 300000
	}
	if c.StreamIdleTimeoutMS < 1 || c.StreamIdleTimeoutMS > 2147483647 {
		return nil, errors.New("openai adapter: invalid idle timeout")
	}
	client := http.Client{}
	if c.Client != nil {
		client = *c.Client
	}
	// Redirects cannot move credentials or prompt bodies to a different destination.
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	transport := client.Transport
	if transport == nil {
		transport = http.DefaultTransport.(*http.Transport).Clone()
		transport.(*http.Transport).Proxy = nil
	}
	client.Transport = terminalCheckingTransport{next: transport}
	sdk := openaisdk.NewChatCompletionService(option.WithAPIKey(c.APIKey), option.WithBaseURL(u.String()), option.WithHTTPClient(&client), option.WithMaxRetries(0), option.WithOrganization(""), option.WithProject(""), option.WithHeader("User-Agent", "pero-agent/0.1"))
	return &Adapter{client: sdk, models: models, providerName: c.ProviderName, retryPolicy: policy, streamIdleTimeout: time.Duration(c.StreamIdleTimeoutMS) * time.Millisecond}, nil
}

func (a *Adapter) ResolveModelInfo(_ context.Context, provider, model string) (llm.ModelInfo, error) {
	m, ok := a.models[model]
	if !ok || provider != a.providerName {
		return llm.ModelInfo{}, fmt.Errorf("openai adapter: unconfigured provider/model")
	}
	var reasoning *llm.ModelReasoningInfo
	if m.Reasoning != nil {
		r := *m.Reasoning
		r.Efforts = append([]llm.ReasoningEffortInfo(nil), r.Efforts...)
		reasoning = &r
	}
	return llm.ModelInfo{Provider: provider, Model: model, Name: m.Name, Description: m.Description, InputModalities: []string{"text"}, ContextWindow: m.ContextWindow, DefaultMaxTokens: m.MaxTokens, Reasoning: reasoning}, nil
}
func (a *Adapter) RetryPolicyForProvider(provider string) (llm.ResolvedRetryPolicy, bool) {
	p := a.retryPolicy
	p.RetryableCodes = append([]string(nil), p.RetryableCodes...)
	return p, provider == a.providerName
}
func (a *Adapter) PrepareAdapterCall(ctx context.Context, provider, model string) (harness.PreparedAdapterCall, error) {
	info, err := a.ResolveModelInfo(ctx, provider, model)
	if err != nil {
		return harness.PreparedAdapterCall{}, err
	}
	p, _ := a.RetryPolicyForProvider(provider)
	return harness.PreparedAdapterCall{ModelInfo: info, RetryPolicy: &p, Generate: a.Generate}, nil
}
