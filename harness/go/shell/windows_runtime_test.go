//go:build windows

package shell

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/phodal/pero/harness/go/sandbox"
	"golang.org/x/sys/windows"
)

// This opt-in changes ACL/integrity labels only inside a disposable test-owned
// workspace and its runner-created private temp directory. Never use real data.
func TestWindowsACLShellCancellationCleansJobAndPrivateTemp(t *testing.T) {
	if os.Getenv("PERO_REQUIRE_WINDOWS_ACL") != "1" {
		t.Skip("requires explicit disposable Windows ACL test authorization")
	}
	pwsh, err := exec.LookPath("pwsh.exe")
	if err != nil {
		t.Fatal(err)
	}
	pwsh, err = filepath.Abs(pwsh)
	if err != nil {
		t.Fatal(err)
	}
	root, privateTempRoot := t.TempDir(), t.TempDir()
	t.Setenv("TMP", privateTempRoot)
	t.Setenv("TEMP", privateTempRoot)
	e, err := NewLocalExecutor(LocalOptions{Shell: pwsh, CWD: root, Sandbox: &sandbox.Config{Mode: sandbox.WorkspaceWrite, Backend: sandbox.BackendWindowsACL}, Timeout: 30 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	if err := e.CheckConfinement(t.Context()); err == nil {
		t.Fatal("partial sandbox attested strict confinement")
	}
	t.Setenv("PERO_PRIVATE_SHELL_FIXTURE", "must-not-inherit")
	marker := filepath.Join(root, "started.json")
	command := `$ErrorActionPreference = 'Stop'; if ($env:PERO_PRIVATE_SHELL_FIXTURE) { throw 'inherited secret' }; ` +
		`$child = Start-Process -FilePath '` + strings.ReplaceAll(pwsh, "'", "''") + `' -ArgumentList '-NoLogo','-NoProfile','-NonInteractive','-Command','Start-Sleep -Seconds 60' -NoNewWindow -PassThru; ` +
		`[IO.File]::WriteAllText('` + strings.ReplaceAll(marker, "'", "''") + `', (@{ pid=$child.Id; temp=$env:TEMP } | ConvertTo-Json -Compress)); Start-Sleep -Seconds 60`
	spec, err := e.Resolve(ExecRequest{Command: command})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	type completion struct {
		result RunResult
		err    error
	}
	done := make(chan completion, 1)
	go func() { result, err := e.Run(ctx, spec); done <- completion{result, err} }()
	var started struct {
		PID  uint32 `json:"pid"`
		Temp string `json:"temp"`
	}
	deadline := time.NewTimer(20 * time.Second)
	defer deadline.Stop()
	tick := time.NewTicker(20 * time.Millisecond)
	defer tick.Stop()
ready:
	for {
		select {
		case early := <-done:
			t.Fatalf("shell exited before readiness: %+v %v", early.result, early.err)
		case <-deadline.C:
			t.Fatal("PowerShell child did not report readiness")
		case <-tick.C:
			data, err := os.ReadFile(marker)
			if err == nil && json.Unmarshal(data, &started) == nil && started.PID != 0 && started.Temp != "" {
				break ready
			}
		}
	}
	rel, err := filepath.Rel(privateTempRoot, started.Temp)
	if err != nil || rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || filepath.IsAbs(rel) {
		t.Fatalf("runner temp was not in the explicit test-owned root: %q %v", started.Temp, err)
	}
	handle, err := windows.OpenProcess(windows.SYNCHRONIZE, false, started.PID)
	if err != nil {
		t.Fatal(err)
	}
	defer windows.CloseHandle(handle)
	if state, err := windows.WaitForSingleObject(handle, 0); err != nil || state != uint32(windows.WAIT_TIMEOUT) {
		t.Fatalf("descendant was not running: state=%d err=%v", state, err)
	}
	cancel()
	select {
	case finished := <-done:
		if !errors.Is(finished.err, context.Canceled) || !finished.result.Aborted || finished.result.TimedOut || finished.result.CleanupUncertain {
			t.Fatalf("cancellation result=%+v err=%v", finished.result, finished.err)
		}
		if finished.result.Enforcement != sandbox.EnforcementPartial || finished.result.ReadIsolated || finished.result.NetworkIsolated {
			t.Fatalf("partial enforcement mislabeled: %+v", finished.result)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("cooperative Windows runner cancellation did not finish")
	}
	if state, err := windows.WaitForSingleObject(handle, 0); err != nil || state != windows.WAIT_OBJECT_0 {
		t.Fatalf("descendant escaped job cleanup: state=%d err=%v", state, err)
	}
	if _, err := os.Stat(started.Temp); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("private temporary directory not removed: %q %v", started.Temp, err)
	}
}
