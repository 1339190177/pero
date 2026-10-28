// pero-agent-acp runs the extracted agent loop over ACP stdio, without product services.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/phodal/pero/harness/go/acp"
	"github.com/phodal/pero/harness/go/harness"
	"github.com/phodal/pero/harness/go/providers/openai"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run() (resultErr error) {
	endpoint := flag.String("endpoint", "", "explicit OpenAI-compatible HTTPS endpoint (loopback HTTP allowed)")
	model := flag.String("model", "", "explicit provider model ID")
	sessions := flag.String("session-dir", "", "private directory for durable JSONL sessions (required)")
	keyEnv := flag.String("api-key-env", "AGENT_PROVIDER_API_KEY", "environment variable containing the provider credential")
	approval := flag.String("approval", "never", "tool approval policy: never or ask via ACP client")
	flag.Parse()
	if flag.NArg() != 0 || strings.TrimSpace(*model) == "" || strings.TrimSpace(*sessions) == "" {
		return errors.New("--model and --session-dir are required; positional arguments are unsupported")
	}
	if *approval != "never" && *approval != "ask" {
		return errors.New("--approval must be never or ask")
	}
	credential, err := takeProviderCredential(*keyEnv)
	if err != nil {
		return err
	}
	adapter, err := openai.New(openai.Config{Endpoint: *endpoint, APIKey: credential, ProviderName: "configured", Models: []openai.ModelConfig{{ID: *model}}})
	if err != nil {
		return err
	}
	persistence, err := harness.NewJSONLSessionPersistence(*sessions)
	if err != nil {
		return err
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	h, err := harness.Boot(ctx, harness.Options{Adapter: adapter, AdapterProviders: []string{"configured"}, ModelInfo: adapter, ApprovalPolicy: harness.ApprovalPolicy(*approval)})
	if err != nil {
		return err
	}
	defer func() {
		cleanup, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		resultErr = errors.Join(resultErr, h.Dispose(cleanup), h.Drain(cleanup))
	}()
	server, err := acp.NewServer(acp.Options{Harness: h, Persistence: persistence, Provider: "configured", Model: *model, Name: "pero-agent-acp", Version: "0.1"})
	if err != nil {
		return err
	}
	// Closing stdin interrupts a blocked scanner when the process receives a signal.
	done := make(chan struct{})
	defer close(done)
	go func() {
		select {
		case <-ctx.Done():
			_ = os.Stdin.Close()
		case <-done:
		}
	}()
	err = server.Serve(ctx, os.Stdin, os.Stdout)
	return err
}

// Remove the actual configured name before any MCP child can inherit it. This
// applies even to names not matched by generic secret-name filters.
func takeProviderCredential(name string) (string, error) {
	if name == "" {
		return "", errors.New("credential environment variable name must not be empty")
	}
	value := os.Getenv(name)
	if err := os.Unsetenv(name); err != nil {
		return "", errors.New("could not clear provider credential environment variable")
	}
	return value, nil
}
