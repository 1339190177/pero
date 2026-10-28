package shell

import (
	"context"
	"errors"
	"fmt"
	"github.com/phodal/pero/harness/go/sandbox"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const (
	defaultTimeout     = 30 * time.Second
	maximumTimeout     = 120 * time.Second
	maximumOutputBytes = 64 * 1024
	defaultGrace       = 250 * time.Millisecond
)

var ErrTimeout = errors.New("shell execution timed out")

type LocalOptions struct {
	Sandbox        *sandbox.Config
	Shell          string
	CWD            string
	Timeout        time.Duration
	MaxTimeout     time.Duration
	MaxOutputBytes int
	Grace          time.Duration
	// BaseEnv is an explicit trusted allowlist input, never inherited implicitly.
	// Only PATH, LANG, LC_ALL and TZ are accepted; no per-command environment seam.
	BaseEnv []string
}

type LocalExecutor struct {
	sandbox                    *sandbox.Executor
	shell, cwd                 string
	workspaceIdentity          os.FileInfo
	timeout, maxTimeout, grace time.Duration
	maxOutputBytes             int
	baseEnv                    []string
}

func NewLocalExecutor(options LocalOptions) (*LocalExecutor, error) {
	if !platformSupported() {
		return nil, errors.New("shell: process-group ownership is unsupported on this platform")
	}
	if !filepath.IsAbs(options.Shell) || !filepath.IsAbs(options.CWD) {
		return nil, errors.New("shell: explicit absolute executable and workspace directory required")
	}
	shell, err := filepath.EvalSymlinks(options.Shell)
	if err != nil {
		return nil, fmt.Errorf("shell: executable: %w", err)
	}
	info, err := os.Stat(shell)
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0111 == 0 {
		return nil, errors.New("shell: executable must be a regular executable file")
	}
	cwd, err := filepath.EvalSymlinks(options.CWD)
	if err != nil {
		return nil, fmt.Errorf("shell: workspace: %w", err)
	}
	info, err = os.Stat(cwd)
	if err != nil || !info.IsDir() {
		return nil, errors.New("shell: workspace must be a directory")
	}
	if options.Timeout == 0 {
		options.Timeout = defaultTimeout
	}
	if options.MaxTimeout == 0 {
		options.MaxTimeout = maximumTimeout
	}
	if options.MaxOutputBytes == 0 {
		options.MaxOutputBytes = maximumOutputBytes
	}
	if options.Grace == 0 {
		options.Grace = defaultGrace
	}
	if options.Timeout < 0 || options.MaxTimeout <= 0 || options.MaxTimeout > maximumTimeout || options.Timeout > options.MaxTimeout || options.MaxOutputBytes < 1 || options.MaxOutputBytes > maximumOutputBytes || options.Grace < 0 || options.Grace > time.Second {
		return nil, errors.New("shell: limits exceed supported bounds")
	}
	env, err := explicitEnvironment(options.BaseEnv)
	if err != nil {
		return nil, err
	}
	var confined *sandbox.Executor
	if options.Sandbox != nil {
		confined, err = sandbox.New(*options.Sandbox, cwd)
		if err != nil {
			return nil, err
		}
	}
	return &LocalExecutor{sandbox: confined, shell: shell, cwd: cwd, workspaceIdentity: info, timeout: options.Timeout, maxTimeout: options.MaxTimeout, maxOutputBytes: options.MaxOutputBytes, grace: options.Grace, baseEnv: env}, nil
}

func explicitEnvironment(entries []string) ([]string, error) {
	values := map[string]string{"PATH": "/usr/bin:/bin", "NO_COLOR": "1", "TERM": "dumb", "PAGER": "cat", "GIT_PAGER": "cat"}
	for _, entry := range entries {
		name, value, ok := strings.Cut(entry, "=")
		if !ok || strings.ContainsRune(value, 0) {
			return nil, errors.New("shell: invalid explicit environment")
		}
		switch name {
		case "PATH", "LANG", "LC_ALL", "TZ":
			values[name] = value
		default:
			return nil, errors.New("shell: environment variable is not allowlisted")
		}
	}
	names := make([]string, 0, len(values))
	for name := range values {
		names = append(names, name)
	}
	sort.Strings(names)
	result := make([]string, 0, len(names))
	for _, name := range names {
		result = append(result, name+"="+values[name])
	}
	return result, nil
}

// verifyWorkspace rejects replacement of the trusted workspace pathname between
// operations. The final check/start interval is not an OS isolation boundary.
func (e *LocalExecutor) verifyWorkspace() error {
	if e == nil || e.workspaceIdentity == nil {
		return errors.New("shell: workspace identity is unavailable")
	}
	current, err := os.Lstat(e.cwd)
	if err != nil || !current.IsDir() || !os.SameFile(e.workspaceIdentity, current) {
		return errors.New("shell: configured workspace directory was replaced or removed")
	}
	return nil
}

func (e *LocalExecutor) Resolve(request ExecRequest) (ExecSpec, error) {
	if e == nil {
		return ExecSpec{}, errors.New("shell: local executor is nil")
	}
	if err := e.verifyWorkspace(); err != nil {
		return ExecSpec{}, err
	}
	if strings.TrimSpace(request.Command) == "" || len(request.Command) > 16*1024 || strings.ContainsRune(request.Command, 0) {
		return ExecSpec{}, errors.New("shell: command must be non-empty and at most 16 KiB")
	}
	workdir := request.Workdir
	if workdir == "" {
		workdir = e.cwd
	} else if !filepath.IsAbs(workdir) {
		workdir = filepath.Join(e.cwd, workdir)
	}
	workdir, err := filepath.EvalSymlinks(workdir)
	if err != nil {
		return ExecSpec{}, fmt.Errorf("shell: workdir: %w", err)
	}
	rel, err := filepath.Rel(e.cwd, workdir)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return ExecSpec{}, errors.New("shell: launch directory is outside configured workspace")
	}
	info, err := os.Stat(workdir)
	if err != nil || !info.IsDir() {
		return ExecSpec{}, errors.New("shell: workdir is not a directory")
	}
	timeout := request.Timeout
	if timeout == 0 {
		timeout = e.timeout
	}
	if timeout <= 0 || timeout > e.maxTimeout {
		return ExecSpec{}, errors.New("shell: timeout exceeds configured bounds")
	}
	limit := request.StdoutMaxBytes
	if limit == 0 {
		limit = e.maxOutputBytes
	}
	if limit < 1 || limit > e.maxOutputBytes {
		return ExecSpec{}, errors.New("shell: output limit exceeds configured bounds")
	}
	return ExecSpec{Command: request.Command, Workdir: workdir, Timeout: timeout, StdoutMaxBytes: limit}, nil
}

// Run adapts the source foreground lifecycle. Cancellation owns the process
// group until forced cleanup completes, even when Bash exits before descendants.
func (e *LocalExecutor) Run(ctx context.Context, spec ExecSpec) (RunResult, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	resolved, err := e.Resolve(ExecRequest(spec))
	if err != nil {
		return RunResult{}, err
	}
	spec = resolved
	result := RunResult{Timeout: spec.Timeout}
	if err := ctx.Err(); err != nil {
		result.Aborted = true
		return result, err
	}
	executionCtx, cancel := context.WithCancelCause(context.Background())
	stopCaller := context.AfterFunc(ctx, func() { cancel(ctx.Err()) })
	timer := time.AfterFunc(spec.Timeout, func() { cancel(ErrTimeout) })
	defer func() { stopCaller(); timer.Stop(); cancel(nil) }()
	argv := []string{e.shell, "--noprofile", "--norc", "-c", spec.Command}
	env := append([]string(nil), e.baseEnv...)
	if e.sandbox != nil {
		confined, err := e.sandbox.Confine(argv)
		if err != nil {
			return result, err
		}
		argv = confined.Argv
		env = e.sandbox.Environment()
	}
	command := exec.Command(argv[0], argv[1:]...)
	command.Dir, command.Env = spec.Workdir, env
	stdout, stderr := newTailWriter(spec.StdoutMaxBytes), newTailWriter(e.maxOutputBytes)
	command.Stdout, command.Stderr = stdout, stderr
	command.WaitDelay = e.grace
	configureProcess(command)
	if err := e.verifyWorkspace(); err != nil {
		return result, err
	}
	if cause := context.Cause(executionCtx); cause != nil {
		result.TimedOut = errors.Is(cause, ErrTimeout)
		result.Aborted = !result.TimedOut
		return result, cause
	}
	if err := command.Start(); err != nil {
		return result, fmt.Errorf("shell: start bash: %w", err)
	}
	done, killerDone := make(chan struct{}), make(chan struct{})
	go func() {
		defer close(killerDone)
		select {
		case <-executionCtx.Done():
			terminateProcess(command.Process, e.grace)
		case <-done:
			killProcessGroup(command.Process)
		}
	}()
	waitErr := command.Wait()
	close(done)
	<-killerDone
	result.Stdout, result.Stderr = stdout.Output(), stderr.Output()
	cause := context.Cause(executionCtx)
	result.TimedOut = errors.Is(cause, ErrTimeout)
	result.Aborted = cause != nil && !result.TimedOut
	result.Signal = processSignal(command.ProcessState)
	if command.ProcessState != nil {
		if code := command.ProcessState.ExitCode(); code >= 0 {
			result.ExitCode = &code
		}
	}
	if cause != nil {
		return result, cause
	}
	if waitErr != nil {
		var exitError *exec.ExitError
		if !errors.As(waitErr, &exitError) {
			return result, fmt.Errorf("shell: wait for bash: %w", waitErr)
		}
	}
	return result, nil
}

// CheckConfinement validates the immutable strict configuration and probes only
// a fixed trusted runtime command. It never executes the proposed shell body.
// This is admission evidence, not a claim that a future child already exists;
// Run revalidates and must fail closed before launching every candidate command.
func (e *LocalExecutor) CheckConfinement(ctx context.Context) error {
	if ctx == nil {
		return errors.New("shell: confinement check requires context")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if e == nil || e.sandbox == nil {
		return errors.New("shell: strict confinement is not configured")
	}
	if err := e.verifyWorkspace(); err != nil {
		return err
	}
	if _, err := e.sandbox.Confine([]string{"/usr/bin/true"}); err != nil {
		return err
	}
	return ctx.Err()
}
