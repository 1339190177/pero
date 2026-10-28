//go:build windows

package media

import (
	"context"
	"os"
	"os/exec"
	"strconv"
	"syscall"
	"time"
)

func configureImageCodecProcess(command *exec.Cmd) {
	command.SysProcAttr = &syscall.SysProcAttr{CreationFlags: syscall.CREATE_NEW_PROCESS_GROUP}
	command.Cancel = func() error {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		killer := exec.CommandContext(ctx, "taskkill", "/PID", strconv.Itoa(command.Process.Pid), "/T", "/F")
		killer.Env = imageCodecEnvironment(os.Environ(), command.Dir)
		_ = killer.Run()
		return command.Process.Kill()
	}
}

// Windows taskkill owns cancellation-time tree cleanup. Post-leader-exit Job
// Object containment is a separate native Windows acceptance requirement.
func cleanupImageCodecProcess(*exec.Cmd) {}
