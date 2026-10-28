//go:build linux

package media

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestOfficialImageCodecCancellationKillsOwnedProcessGroup(t *testing.T) {
	root := t.TempDir()
	t.Setenv("TMPDIR", root)
	fifo := filepath.Join(root, "ready")
	if err := syscall.Mkfifo(fifo, 0600); err != nil {
		t.Fatal(err)
	}
	reader, err := os.OpenFile(fifo, os.O_RDWR, 0600)
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	script := filepath.Join(root, "node-fixture")
	if err := os.WriteFile(script, []byte("#!/bin/sh\nsleep 120 &\necho $! > '"+fifo+"'\nwait\n"), 0700); err != nil {
		t.Fatal(err)
	}
	owner, cancel := context.WithCancel(t.Context())
	defer cancel()
	codec := &officialImageCodec{owner: owner, config: OfficialImageCodecConfig{Node: script, Anchor: filepath.Join(root, "package.json"), Timeout: 10 * time.Second, MaxInputBytes: 1024, MaxOutputBytes: 1024}}
	completed := make(chan error, 1)
	go func() { _, err := codec.call(t.Context(), map[string]any{"operation": "identify"}); completed <- err }()
	ready := make(chan string, 1)
	go func() {
		scanner := bufio.NewScanner(reader)
		if scanner.Scan() {
			ready <- scanner.Text()
		} else {
			ready <- ""
		}
	}()
	var pid int
	select {
	case value := <-ready:
		pid, err = strconv.Atoi(value)
		if err != nil {
			t.Fatalf("ready PID %q: %v", value, err)
		}
	case err := <-completed:
		t.Fatalf("exited before ready: %v", err)
	case <-time.After(5 * time.Second):
		t.Fatal("fixture did not publish readiness")
	}
	defer syscall.Kill(pid, syscall.SIGKILL)
	cancel()
	select {
	case err := <-completed:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("cancellation=%v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("codec did not stop")
	}
	if data, err := os.ReadFile(fmt.Sprintf("/proc/%d/stat", pid)); err == nil {
		suffix := string(data[strings.LastIndex(string(data), ")")+2:])
		// Linux /proc reports X (dead) before release_task removes the PID.
		if !strings.HasPrefix(suffix, "Z ") && !strings.HasPrefix(suffix, "X ") {
			t.Fatalf("descendant survived cancellation: %s", data)
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), "point-image-codec-") {
			t.Fatalf("codec scratch leaked: %s", entry.Name())
		}
	}
}

func TestOfficialImageCodecTimeoutAndOutputLimitStopProcess(t *testing.T) {
	for _, test := range []struct {
		name, body  string
		timeout     time.Duration
		wantTimeout bool
	}{
		{"timeout", "sleep 120", 30 * time.Millisecond, true},
		{"oversize", "yes x", 10 * time.Second, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			root := t.TempDir()
			t.Setenv("TMPDIR", root)
			script := filepath.Join(root, "node-fixture")
			if err := os.WriteFile(script, []byte("#!/bin/sh\n"+test.body+"\n"), 0700); err != nil {
				t.Fatal(err)
			}
			codec := &officialImageCodec{owner: t.Context(), config: OfficialImageCodecConfig{Node: script, Anchor: filepath.Join(root, "package.json"), Timeout: test.timeout, MaxInputBytes: 1024, MaxOutputBytes: 1024}}
			_, err := codec.call(t.Context(), map[string]any{"operation": "identify"})
			if test.wantTimeout && !errors.Is(err, context.DeadlineExceeded) {
				t.Fatalf("timeout=%v", err)
			}
			if !test.wantTimeout && (err == nil || !strings.Contains(err.Error(), "output limit")) {
				t.Fatalf("output limit=%v", err)
			}
		})
	}
}
