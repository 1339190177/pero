package mcp

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strings"
	"time"
	"unicode"

	"golang.org/x/text/unicode/norm"
)

const (
	defaultMCPReconnectInitialDelayMS = 500
	defaultMCPReconnectMaxDelayMS     = 30_000
	defaultMCPReconnectMaxAttempts    = 10
	maxMCPTimerDelayMS                = 2_147_483_647
	// CommandTransport may spend two five-second grace windows before its
	// final kill. One extra second proves that generation has quiesced before
	// another child is allowed to start.
	mcpGenerationCloseTimeout = 11 * time.Second
)

// MCPReconnectConfig controls automatic recovery after one MCP connection is
// lost. Nil fields use the DeepSeek Harness defaults.
type MCPReconnectConfig struct {
	Enabled        *bool `json:"enabled,omitempty"`
	InitialDelayMS int   `json:"initialDelayMs,omitempty"`
	MaxDelayMS     int   `json:"maxDelayMs,omitempty"`
	MaxAttempts    int   `json:"maxAttempts,omitempty"`
}

type ReconnectPolicy struct {
	Enabled      bool
	InitialDelay time.Duration
	MaxDelay     time.Duration
	MaxAttempts  int
}

func ResolveReconnectPolicy(config *MCPReconnectConfig) (ReconnectPolicy, error) {
	policy := ReconnectPolicy{
		Enabled:      true,
		InitialDelay: defaultMCPReconnectInitialDelayMS * time.Millisecond,
		MaxDelay:     defaultMCPReconnectMaxDelayMS * time.Millisecond,
		MaxAttempts:  defaultMCPReconnectMaxAttempts,
	}
	if config == nil {
		return policy, nil
	}
	if config.Enabled != nil {
		policy.Enabled = *config.Enabled
	}
	if config.InitialDelayMS != 0 {
		policy.InitialDelay = time.Duration(config.InitialDelayMS) * time.Millisecond
	}
	if config.MaxDelayMS != 0 {
		policy.MaxDelay = time.Duration(config.MaxDelayMS) * time.Millisecond
	}
	if config.MaxAttempts != 0 {
		policy.MaxAttempts = config.MaxAttempts
	}
	if config.InitialDelayMS < 0 || config.InitialDelayMS > maxMCPTimerDelayMS {
		return ReconnectPolicy{}, errors.New("initialDelayMs must be a positive timer duration")
	}
	if config.MaxDelayMS < 0 || config.MaxDelayMS > maxMCPTimerDelayMS {
		return ReconnectPolicy{}, errors.New("maxDelayMs must be a positive timer duration")
	}
	if policy.InitialDelay > policy.MaxDelay {
		return ReconnectPolicy{}, errors.New("initialDelayMs must not exceed maxDelayMs")
	}
	if config.MaxAttempts < 0 || policy.MaxAttempts < 1 {
		return ReconnectPolicy{}, errors.New("maxAttempts must be a positive integer")
	}
	return policy, nil
}

type MCPTransport string

const (
	MCPStdio          MCPTransport = "stdio"
	MCPStreamableHTTP MCPTransport = "streamable-http"
)

func NormalizeServerName(name string) string {
	valid := name != "" && len(name) <= 32
	for _, char := range name {
		if !(char == '_' || char == '-' || unicode.IsLetter(char) && char <= unicode.MaxASCII || unicode.IsDigit(char) && char <= unicode.MaxASCII) {
			valid = false
			break
		}
	}
	if valid {
		return name
	}
	var slug strings.Builder
	for _, char := range norm.NFKD.String(name) {
		if unicode.IsLetter(char) && char <= unicode.MaxASCII || unicode.IsDigit(char) && char <= unicode.MaxASCII || char == '_' || char == '-' {
			slug.WriteRune(char)
		} else if slug.Len() > 0 && !strings.HasSuffix(slug.String(), "_") {
			slug.WriteByte('_')
		}
		if slug.Len() >= 20 {
			break
		}
	}
	prefix := strings.Trim(slug.String(), "_")
	if prefix == "" {
		prefix = "server"
	}
	digest := sha256.Sum256([]byte(name))
	return prefix + "_" + hex.EncodeToString(digest[:4])
}
