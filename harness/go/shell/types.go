// Package shell provides bounded foreground Bash execution, not an OS sandbox.
package shell

import (
	"context"
	"time"
)

type CollectedOutput struct {
	Text      string `json:"text"`
	Truncated bool   `json:"truncated"`
}

type ExecRequest struct {
	Command        string
	Workdir        string
	Timeout        time.Duration
	StdoutMaxBytes int
}

type ExecSpec struct {
	Command        string
	Workdir        string
	Timeout        time.Duration
	StdoutMaxBytes int
}

type RunResult struct {
	ExitCode *int            `json:"exitCode"`
	Signal   string          `json:"signal,omitempty"`
	TimedOut bool            `json:"timedOut"`
	Aborted  bool            `json:"aborted"`
	Timeout  time.Duration   `json:"-"`
	Stdout   CollectedOutput `json:"stdout"`
	Stderr   CollectedOutput `json:"stderr"`
}

type Executor interface {
	Resolve(ExecRequest) (ExecSpec, error)
	Run(context.Context, ExecSpec) (RunResult, error)
}
