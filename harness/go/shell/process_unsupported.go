//go:build !linux && !darwin

package shell

import (
	"os"
	"os/exec"
	"time"
)

func platformSupported() bool                     { return false }
func configureProcess(*exec.Cmd)                  {}
func killProcessGroup(*os.Process)                {}
func terminateProcess(*os.Process, time.Duration) {}
func processSignal(*os.ProcessState) string       { return "" }
