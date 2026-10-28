// Package sandbox contains Point's strict native boundary without product services.
package sandbox

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

type Mode string

const (
	ReadOnly       Mode = "read-only"
	WorkspaceWrite Mode = "workspace-write"
)

type Policy struct {
	Mode             Mode
	WorkspaceRoot    string
	Strict           *StrictProfile
	WritableSubpaths []string
}
type Enforcement string

const EnforcementFull Enforcement = "full"

type RunnerFailureRule struct{ FatalSignatures []string }
type Confined struct {
	Argv                          []string
	Enforcement                   Enforcement
	ReadIsolated, NetworkIsolated bool
	DenialSignatures              []string
	RunnerFailureRules            []RunnerFailureRule
}
type UnavailableError struct {
	Mode   Mode
	Detail string
	Cause  error
}

func (e *UnavailableError) Error() string { return "SANDBOX_UNAVAILABLE: " + e.Detail }
func (e *UnavailableError) Unwrap() error { return e.Cause }

type localOptions struct {
	RunnerCommand                           []string
	Platform, BwrapCommand, SeatbeltCommand string
	ProbeTimeout                            time.Duration
}
type LocalProvider struct{ options localOptions }

// Config is trusted launch input only. No tool/session can change its authority.
// Network access is always denied; no permissive fallback or escalation exists.
type Config struct {
	Mode         Mode
	RuntimePaths []string
}
type Executor struct {
	provider LocalProvider
	policy   Policy
	identity os.FileInfo
}

func New(config Config, workspace string) (*Executor, error) {
	if config.Mode != ReadOnly && config.Mode != WorkspaceWrite {
		return nil, errors.New("sandbox: mode must be read-only or workspace-write")
	}
	root, err := filepath.EvalSymlinks(workspace)
	if err != nil || root != workspace || !filepath.IsAbs(root) || root == "/" {
		return nil, errors.New("sandbox: workspace must be canonical absolute non-root directory")
	}
	info, err := os.Stat(root)
	if err != nil || !info.IsDir() {
		return nil, errors.New("sandbox: workspace unavailable")
	}
	e := &Executor{provider: LocalProvider{options: localOptions{Platform: runtime.GOOS, BwrapCommand: "/usr/bin/bwrap", SeatbeltCommand: "/usr/bin/sandbox-exec", ProbeTimeout: 5 * time.Second}}, policy: Policy{Mode: config.Mode, WorkspaceRoot: root, Strict: &StrictProfile{ReadOnlyPaths: append([]string(nil), config.RuntimePaths...)}}, identity: info}
	if _, err = e.Confine([]string{"/usr/bin/true"}); err != nil {
		return nil, err
	}
	return e, nil
}
func (e *Executor) Confine(argv []string) (Confined, error) {
	if e == nil || len(argv) == 0 {
		return Confined{}, errors.New("sandbox: missing executor or command")
	}
	info, err := os.Lstat(e.policy.WorkspaceRoot)
	if err != nil || !info.IsDir() || !os.SameFile(info, e.identity) {
		return Confined{}, errors.New("sandbox: workspace replaced")
	}
	if err := validateWorkspace(e.policy.WorkspaceRoot); err != nil {
		return Confined{}, err
	}
	return e.provider.confineStrict(argv, e.policy)
}
func (e *Executor) Environment() []string { return StrictEnvironment(e.policy) }
func withinRoot(root, target string) bool {
	r, e := filepath.Rel(root, target)
	return e == nil && r != ".." && !strings.HasPrefix(r, ".."+string(filepath.Separator)) && !filepath.IsAbs(r)
}
func sbplString(value string) string {
	value = strings.ReplaceAll(value, `\`, `\\`)
	value = strings.ReplaceAll(value, `"`, `\"`)
	return `"` + value + `"`
}
func (c Config) Validate() error {
	if c.Mode != ReadOnly && c.Mode != WorkspaceWrite {
		return fmt.Errorf("sandbox: invalid mode %q", c.Mode)
	}
	return nil
}
