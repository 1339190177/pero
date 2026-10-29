//go:build linux || darwin

package shell

import (
	"os"
	"os/exec"
	"syscall"
	"time"
)

func platformSupported() bool            { return true }
func configureProcess(command *exec.Cmd) { command.SysProcAttr = &syscall.SysProcAttr{Setpgid: true} }
func killProcessGroup(process *os.Process) {
	if process != nil {
		_ = syscall.Kill(-process.Pid, syscall.SIGKILL)
	}
}
func terminateProcess(process *os.Process, grace time.Duration) {
	if process == nil {
		return
	}
	_ = syscall.Kill(-process.Pid, syscall.SIGTERM)
	// Do not stop when the group leader exits: descendants may ignore TERM.
	timer := time.NewTimer(grace)
	defer timer.Stop()
	<-timer.C
	killProcessGroup(process)
}
func processSignal(state *os.ProcessState) string {
	if state == nil {
		return ""
	}
	status, ok := state.Sys().(syscall.WaitStatus)
	if !ok || !status.Signaled() {
		return ""
	}
	return status.Signal().String()
}
