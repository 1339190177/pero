//go:build linux

package shell

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestCancellationKillsTermIgnoringDescendantAfterLeaderExit(t *testing.T) {
	e := testExecutor(t, LocalOptions{Timeout: 120 * time.Millisecond})
	_, err := runCommand(t, e, context.Background(), `(trap '' TERM; while :; do sleep 1; done) & echo $! > child.pid; wait`)
	if err == nil {
		t.Fatal("missing timeout")
	}
	data, err := os.ReadFile(filepath.Join(e.cwd, "child.pid"))
	if err != nil {
		t.Fatal(err)
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(time.Second)
	for {
		data, readErr := os.ReadFile(fmt.Sprintf("/proc/%d/stat", pid))
		if os.IsNotExist(readErr) {
			break
		}
		// An orphaned zombie has exited and cannot execute; container PID1 may delay reap.
		if readErr == nil {
			tail := string(data)[strings.LastIndex(string(data), ")")+1:]
			if strings.HasPrefix(strings.TrimSpace(tail), "Z ") {
				break
			}
		}
		if time.Now().After(deadline) {
			t.Fatalf("descendant %d still executing: %s", pid, data)
		}
		time.Sleep(10 * time.Millisecond)
	}
}
