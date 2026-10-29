//go:build windows

package sandbox

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

// These tests change DACLs and integrity labels only below test-owned temporary
// directories. They are deliberately excluded from ordinary Windows go test.
// Run only in the disposable Windows CI job approved for this security fixture.
func windowsACLFixture(t *testing.T) (workspace, tempRoot string) {
	t.Helper()
	if os.Getenv("PERO_REQUIRE_WINDOWS_ACL") != "1" {
		t.Skip("Windows ACL/integrity modifications require PERO_REQUIRE_WINDOWS_ACL=1 on a disposable runner")
	}
	workspace, tempRoot = t.TempDir(), t.TempDir()
	var err error
	workspace, err = filepath.EvalSymlinks(workspace)
	if err != nil {
		t.Fatal(err)
	}
	tempRoot, err = filepath.EvalSymlinks(tempRoot)
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("TMP", tempRoot)
	t.Setenv("TEMP", tempRoot)
	return workspace, tempRoot
}

type aclFixtureProcess struct {
	command *exec.Cmd
	control io.WriteCloser
	output  bytes.Buffer
	done    chan struct{}
	err     error
}

func startACLFixture(t *testing.T, workspace string, mode Mode, args ...string) *aclFixtureProcess {
	t.Helper()
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	executor, err := New(Config{Mode: mode, Backend: BackendWindowsACL}, workspace)
	if err != nil {
		t.Fatal(err)
	}
	confined, err := executor.Confine(append([]string{executable, "__pero_acl_test_child"}, args...))
	if err != nil {
		t.Fatal(err)
	}
	if confined.Enforcement != Enforcement("partial") || confined.ReadIsolated || confined.NetworkIsolated {
		t.Fatalf("Windows ACL must advertise partial write restriction: %+v", confined)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	t.Cleanup(cancel)
	p := &aclFixtureProcess{command: exec.CommandContext(ctx, confined.Argv[0], confined.Argv[1:]...), done: make(chan struct{})}
	p.command.Dir, p.command.Env = workspace, executor.Environment()
	p.command.Stdout, p.command.Stderr = &p.output, &p.output
	p.command.WaitDelay = 2 * time.Second
	p.control, err = p.command.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := p.command.Start(); err != nil {
		t.Fatal(err)
	}
	go func() { p.err = p.command.Wait(); close(p.done) }()
	t.Cleanup(func() {
		_ = p.control.Close()
		select {
		case <-p.done:
		case <-time.After(10 * time.Second):
			_ = p.command.Process.Kill()
			<-p.done
			t.Error("runner did not finish cooperative cleanup")
		}
	})
	return p
}

func (p *aclFixtureProcess) wait(t *testing.T, code int) string {
	t.Helper()
	<-p.done
	_ = p.control.Close()
	actual := p.command.ProcessState.ExitCode()
	if actual != code {
		t.Fatalf("runner exit=%d want=%d err=%v output=%s", actual, code, p.err, p.output.String())
	}
	return p.output.String()
}

func TestWindowsACLRuntime(t *testing.T) {
	workspace, tempRoot := windowsACLFixture(t)
	outside := t.TempDir()
	secret := filepath.Join(outside, "secret.txt")
	inside := filepath.Join(workspace, "inside.txt")
	if err := os.WriteFile(secret, []byte("fixture-readable"), 0600); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name string
		mode Mode
		args []string
		code int
		want string
	}{
		{"read-only-write", ReadOnly, []string{"write", inside, "denied"}, 3, "fixture-denied"},
		{"outside-read", ReadOnly, []string{"read", secret}, 0, "fixture-readable"},
		{"workspace-write", WorkspaceWrite, []string{"write", inside, "allowed"}, 0, "fixture-ok"},
		{"outside-write", WorkspaceWrite, []string{"write", secret, "escaped"}, 3, "fixture-denied"},
		{"read-only-downgrade", ReadOnly, []string{"write", inside, "escaped"}, 3, "fixture-denied"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			output := startACLFixture(t, workspace, tc.mode, tc.args...).wait(t, tc.code)
			if !strings.Contains(output, tc.want) {
				t.Fatalf("candidate did not establish expected result: %q", output)
			}
		})
	}
	for path, want := range map[string]string{secret: "fixture-readable", inside: "allowed"} {
		data, err := os.ReadFile(path)
		if err != nil || string(data) != want {
			t.Fatalf("unexpected file state %s: %q %v", path, data, err)
		}
	}
	// The deny belongs on directories, not files: inheriting FILE_DELETE_CHILD
	// onto files would break the host's GENERIC_ALL opens even with FullControl.
	insidePath, err := windows.UTF16PtrFromString(inside)
	if err != nil {
		t.Fatal(err)
	}
	handle, err := windows.CreateFile(insidePath, windows.GENERIC_ALL, windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE, nil, windows.OPEN_EXISTING, 0, 0)
	if err != nil {
		t.Fatalf("host FullControl file open after grant: %v", err)
	}
	if err := windows.CloseHandle(handle); err != nil {
		t.Fatal(err)
	}
	startACLFixture(t, workspace, WorkspaceWrite, "delete", inside).wait(t, 0)
	if _, err := os.Stat(inside); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("ordinary granted deletion failed: %v", err)
	}
	// Protect one file from capability inheritance while retaining host cleanup
	// authority. Its parent must not provide a FILE_DELETE_CHILD bypass.
	victim := filepath.Join(workspace, "protected.txt")
	if err := os.WriteFile(victim, []byte("protected"), 0600); err != nil {
		t.Fatal(err)
	}
	var token windows.Token
	if err := windows.OpenProcessToken(windows.CurrentProcess(), windows.TOKEN_QUERY, &token); err != nil {
		t.Fatal(err)
	}
	user, err := token.GetTokenUser()
	token.Close()
	if err != nil {
		t.Fatal(err)
	}
	security, err := windows.SecurityDescriptorFromString("D:P(A;;FA;;;" + user.User.Sid.String() + ")")
	if err != nil {
		t.Fatal(err)
	}
	dacl, _, err := security.DACL()
	if err != nil {
		t.Fatal(err)
	}
	if err := windows.SetNamedSecurityInfo(victim, windows.SE_FILE_OBJECT, windows.DACL_SECURITY_INFORMATION|windows.PROTECTED_DACL_SECURITY_INFORMATION, nil, nil, dacl, nil); err != nil {
		t.Fatal(err)
	}
	if output := startACLFixture(t, workspace, WorkspaceWrite, "delete", victim).wait(t, 3); !strings.Contains(output, "fixture-denied") {
		t.Fatal(output)
	}
	if data, err := os.ReadFile(victim); err != nil || string(data) != "protected" {
		t.Fatalf("parent-delete bypass: %q %v", data, err)
	}
	output := startACLFixture(t, workspace, WorkspaceWrite, "inspect").wait(t, 0)
	var state aclFixtureState
	if err := json.Unmarshal([]byte(output), &state); err != nil {
		t.Fatalf("inspect: %v: %s", err, output)
	}
	if state.Integrity != "S-1-16-4096" || state.Temp != state.Tmp {
		t.Fatalf("child integrity/temp contract: %+v", state)
	}
	if rel, err := filepath.Rel(tempRoot, state.Temp); err != nil || rel == "." || !withinRoot(tempRoot, state.Temp) {
		t.Fatalf("private temp escaped test-owned root: %q (rel=%q err=%v)", state.Temp, rel, err)
	}
	if _, err := os.Stat(state.Temp); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("private temp survived normal exit: %v", err)
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	output = startACLFixture(t, workspace, ReadOnly, "connect", listener.Addr().String()).wait(t, 0)
	if !strings.Contains(output, "fixture-connected") {
		t.Fatalf("partial mode must not claim network isolation: %q", output)
	}
}

func TestWindowsACLJobLifecycle(t *testing.T) {
	workspace, _ := windowsACLFixture(t)
	for _, finish := range []string{"cancel", "exit", "kill"} {
		t.Run(finish, func(t *testing.T) {
			root := filepath.Join(workspace, finish)
			if err := os.Mkdir(root, 0700); err != nil {
				t.Fatal(err)
			}
			p := startACLFixture(t, workspace, WorkspaceWrite, "tree", root)
			parent := awaitACLFixtureState(t, filepath.Join(root, "parent.json"), p)
			child := awaitACLFixtureState(t, filepath.Join(root, "child.json"), p)
			handles := []windows.Handle{}
			for _, pid := range []int{parent.PID, child.PID} {
				h, err := windows.OpenProcess(windows.SYNCHRONIZE|windows.PROCESS_TERMINATE, false, uint32(pid))
				if err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() {
					// A failed containment assertion must not leak this known fixture child.
					if status, _ := windows.WaitForSingleObject(h, 0); status == uint32(windows.WAIT_TIMEOUT) {
						_ = windows.TerminateProcess(h, 1)
						_, _ = windows.WaitForSingleObject(h, 5000)
					}
					windows.CloseHandle(h)
				})
				if status, err := windows.WaitForSingleObject(h, 0); err != nil || status != uint32(windows.WAIT_TIMEOUT) {
					t.Fatalf("fixture process %d was not running: %d %v", pid, status, err)
				}
				handles = append(handles, h)
			}
			// A new session has the same workspace SID but a distinct temp SID.
			output := startACLFixture(t, workspace, WorkspaceWrite, "write", filepath.Join(parent.Temp, "foreign.txt"), "escaped").wait(t, 3)
			if !strings.Contains(output, "fixture-denied") {
				t.Fatalf("sibling session temp was writable: %s", output)
			}
			if finish == "cancel" {
				if err := p.control.Close(); err != nil {
					t.Fatal(err)
				}
				<-p.done
			} else if finish == "kill" {
				if err := p.command.Process.Kill(); err != nil {
					t.Fatal(err)
				}
				<-p.done
			} else {
				if err := os.WriteFile(filepath.Join(root, "exit"), nil, 0600); err != nil {
					t.Fatal(err)
				}
				p.wait(t, 0)
			}
			for _, h := range handles {
				if status, err := windows.WaitForSingleObject(h, 5000); err != nil || status != windows.WAIT_OBJECT_0 {
					t.Fatalf("Job Object leaked a descendant: status=%d err=%v output=%s", status, err, p.output.String())
				}
			}
			if finish == "kill" {
				if info, err := os.Stat(parent.Temp); err != nil || !info.IsDir() {
					t.Fatalf("forced kill must not claim deferred temp cleanup: %v", err)
				}
				t.Log("Forced runner termination stopped descendants but left private temp; host fixture removes the residual explicitly")
				if err := os.RemoveAll(parent.Temp); err != nil {
					t.Fatal(err)
				}
			}
			if _, err := os.Stat(parent.Temp); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("private temp survived %s: %s: %v; %s", finish, parent.Temp, err, p.output.String())
			}
		})
	}
}

func awaitACLFixtureState(t *testing.T, path string, p *aclFixtureProcess) aclFixtureState {
	t.Helper()
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	deadline := time.NewTimer(10 * time.Second)
	defer deadline.Stop()
	for {
		var state aclFixtureState
		if data, err := os.ReadFile(path); err == nil && json.Unmarshal(data, &state) == nil {
			return state
		}
		select {
		case <-p.done:
			t.Fatalf("runner exited before fixture readiness: %v: %s", p.err, p.output.String())
		case <-deadline.C:
			t.Fatal("fixture did not become ready")
		case <-ticker.C:
		}
	}
}

// Everyone's ambient Modify grant passes the restricting-SID intersection.
// Explicit Medium integrity must still prevent a Low child from using it.
func TestWindowsACLAmbientAndForeignRoots(t *testing.T) {
	workspace, _ := windowsACLFixture(t)
	mediumRoot, foreignRoot := t.TempDir(), t.TempDir()
	descriptor, err := windows.GetNamedSecurityInfo(mediumRoot, windows.SE_FILE_OBJECT, windows.DACL_SECURITY_INFORMATION)
	if err != nil {
		t.Fatal(err)
	}
	current, _, err := descriptor.DACL()
	if err != nil {
		t.Fatal(err)
	}
	world, err := windows.CreateWellKnownSid(windows.WinWorldSid)
	if err != nil {
		t.Fatal(err)
	}
	var pin runtime.Pinner
	pin.Pin(world)
	defer pin.Unpin()
	merged, err := windows.ACLFromEntries([]windows.EXPLICIT_ACCESS{{
		AccessPermissions: 0x1301bf, AccessMode: windows.GRANT_ACCESS, // File Modify, including read/execute/delete.
		Inheritance: windows.SUB_CONTAINERS_AND_OBJECTS_INHERIT,
		Trustee:     windows.TRUSTEE{TrusteeForm: windows.TRUSTEE_IS_SID, TrusteeType: windows.TRUSTEE_IS_UNKNOWN, TrusteeValue: windows.TrusteeValueFromSID(world)},
	}}, current)
	runtime.KeepAlive(descriptor)
	if err != nil {
		t.Fatal(err)
	}
	medium, err := windows.SecurityDescriptorFromString("S:(ML;OICI;NW;;;ME)")
	if err != nil {
		t.Fatal(err)
	}
	label, _, err := medium.SACL()
	if err != nil {
		t.Fatal(err)
	}
	if err := windows.SetNamedSecurityInfo(mediumRoot, windows.SE_FILE_OBJECT, windows.DACL_SECURITY_INFORMATION|windows.LABEL_SECURITY_INFORMATION, nil, nil, merged, label); err != nil {
		t.Fatal(err)
	}
	runtime.KeepAlive(medium)
	foreignVictim := filepath.Join(foreignRoot, "victim.txt")
	if err := os.WriteFile(foreignVictim, []byte("foreign"), 0600); err != nil {
		t.Fatal(err)
	}
	foreignSID, err := capabilitySID(foreignRoot, false)
	if err != nil {
		t.Fatal(err)
	}
	if err := updatePathGrant(foreignRoot, foreignSID, windows.GRANT_ACCESS); err != nil {
		t.Fatal(err)
	}
	foreignSecurity, err := windows.GetNamedSecurityInfo(foreignRoot, windows.SE_FILE_OBJECT, windows.LABEL_SECURITY_INFORMATION)
	if err != nil {
		t.Fatal(err)
	}
	foreignLabel, _, err := foreignSecurity.SACL()
	if err != nil {
		t.Fatal(err)
	}
	if countACLFixtureACE(t, foreignLabel, 17, 3, 1, "S-1-16-4096") != 1 {
		t.Fatal("foreign root did not become Low; cross-root test would only prove MIC")
	}
	runtime.KeepAlive(foreignSecurity)
	// Inspect the victim too: an unpropagated Medium child would make a denied
	// delete pass for the wrong reason. File label inheritance flags differ.
	victimSecurity, err := windows.GetNamedSecurityInfo(foreignVictim, windows.SE_FILE_OBJECT, windows.LABEL_SECURITY_INFORMATION)
	if err != nil {
		t.Fatal(err)
	}
	victimLabel, _, err := victimSecurity.SACL()
	if err != nil {
		t.Fatal(err)
	}
	victimLow := false
	if victimLabel != nil {
		for i := uint32(0); i < uint32(victimLabel.AceCount); i++ {
			var ace *windows.ACCESS_ALLOWED_ACE
			if err := windows.GetAce(victimLabel, i, &ace); err != nil {
				t.Fatal(err)
			}
			if ace.Header.AceType == 17 && uint32(ace.Mask)&1 != 0 && (*windows.SID)(unsafe.Pointer(&ace.SidStart)).String() == "S-1-16-4096" {
				victimLow = true
			}
		}
	}
	if !victimLow {
		t.Fatal("foreign victim did not inherit Low integrity; deletion test would only prove MIC")
	}
	runtime.KeepAlive(victimSecurity)
	for _, mode := range []Mode{ReadOnly, WorkspaceWrite} {
		t.Run(string(mode), func(t *testing.T) {
			outsideWrite := filepath.Join(mediumRoot, string(mode)+".txt")
			if output := startACLFixture(t, workspace, mode, "write", outsideWrite, "escaped").wait(t, 3); !strings.Contains(output, "fixture-denied") {
				t.Fatal(output)
			}
			if _, err := os.Stat(outsideWrite); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("ambient Everyone-Modify escaped Low integrity: %v", err)
			}
			if output := startACLFixture(t, workspace, mode, "delete", foreignVictim).wait(t, 3); !strings.Contains(output, "fixture-denied") {
				t.Fatal(output)
			}
			if data, err := os.ReadFile(foreignVictim); err != nil || string(data) != "foreign" {
				t.Fatalf("cross-root delete escaped: %q %v", data, err)
			}
		})
	}
}

func TestWindowsACLAdmissionBoundaries(t *testing.T) {
	workspace, _ := windowsACLFixture(t)
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	sentinel := filepath.Join(workspace, "candidate-ran.txt")
	t.Run("hardlink", func(t *testing.T) {
		source := filepath.Join(t.TempDir(), "original.txt")
		if err := os.WriteFile(source, []byte("untouched"), 0600); err != nil {
			t.Fatal(err)
		}
		alias := filepath.Join(workspace, "alias.txt")
		if err := os.Link(source, alias); err != nil {
			t.Fatal(err)
		}
		defer os.Remove(alias)
		rootBefore, sourceBefore := aclFixtureSecurity(t, workspace), aclFixtureSecurity(t, source)
		executor, err := New(Config{Mode: WorkspaceWrite, Backend: BackendWindowsACL}, workspace)
		if err == nil || executor != nil || !strings.Contains(err.Error(), "hard link") {
			t.Fatalf("hardlink admission did not fail closed: %v %v", executor, err)
		}
		if aclFixtureSecurity(t, workspace) != rootBefore || aclFixtureSecurity(t, source) != sourceBefore {
			t.Fatal("rejected hardlink admission modified a security descriptor")
		}
	})
	alias := filepath.Join(t.TempDir(), "temp-junction")
	windowsRoot, err := windows.GetWindowsDirectory()
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if output, err := exec.CommandContext(ctx, filepath.Join(windowsRoot, "System32", "cmd.exe"), "/d", "/c", "mklink", "/J", alias, workspace).CombinedOutput(); err != nil {
		t.Fatalf("test-owned junction setup: %v: %s", err, output)
	}
	t.Cleanup(func() {
		if err := os.Remove(alias); err != nil && !errors.Is(err, os.ErrNotExist) {
			t.Error(err)
		}
	})
	for _, tc := range []struct{ name, temp string }{{"direct-overlap", workspace}, {"junction-overlap", alias}} {
		t.Run(tc.name, func(t *testing.T) {
			before := aclFixtureSecurity(t, workspace)
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			cmd := exec.CommandContext(ctx, executable, windowsACLRunnerArg, "--workspace", workspace, "--temp", tc.temp, "--mode", string(WorkspaceWrite), "--control-stdin", "--", executable, "__pero_acl_test_child", "write", sentinel, "escaped")
			cmd.Dir, cmd.Env = workspace, windowsEnvironment()
			control, err := cmd.StdinPipe()
			if err != nil {
				t.Fatal(err)
			}
			defer control.Close()
			output, err := cmd.CombinedOutput()
			var exited *exec.ExitError
			if !errors.As(err, &exited) || exited.ExitCode() != windowsACLRunnerFailureExit || !strings.Contains(string(output), "temp root must be outside workspace") {
				t.Fatalf("overlapping temp did not fail before grant: %v: %s", err, output)
			}
			if aclFixtureSecurity(t, workspace) != before {
				t.Fatal("overlap rejection modified workspace security")
			}
			if _, err := os.Stat(sentinel); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("candidate executed after overlap rejection: %v", err)
			}
		})
	}
	t.Run("already-cancelled", func(t *testing.T) {
		token, err := restrictedWriteToken(ReadOnly, nil, nil)
		if err != nil {
			t.Fatal(err)
		}
		defer token.Close()
		cancelled := make(chan struct{})
		close(cancelled)
		code, err := spawnRestrictedInherited(token, []string{executable, "__pero_acl_test_child", "write", sentinel, "escaped"}, workspace, cancelled)
		if err != nil || code != 1 {
			t.Fatalf("pre-cancelled launch=%d %v", code, err)
		}
		if _, err := os.Stat(sentinel); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("pre-cancelled candidate executed: %v", err)
		}
	})
}

func aclFixtureSecurity(t *testing.T, path string) string {
	t.Helper()
	descriptor, err := windows.GetNamedSecurityInfo(path, windows.SE_FILE_OBJECT, windows.DACL_SECURITY_INFORMATION|windows.LABEL_SECURITY_INFORMATION)
	if err != nil {
		t.Fatal(err)
	}
	return descriptor.String()
}

func TestWindowsACLGrantRepairAndRevoke(t *testing.T) {
	workspace, _ := windowsACLFixture(t)
	sid, err := capabilitySID(workspace, false)
	if err != nil {
		t.Fatal(err)
	}
	second, err := capabilitySID(filepath.Join(workspace, "private-temp-identity"), true)
	if err != nil {
		t.Fatal(err)
	}
	// Seed the older Point capability-only grant. The upgraded backend must
	// repair it rather than incorrectly taking the old idempotence fast path.
	descriptor, err := windows.GetNamedSecurityInfo(workspace, windows.SE_FILE_OBJECT, windows.DACL_SECURITY_INFORMATION)
	if err != nil {
		t.Fatal(err)
	}
	current, _, err := descriptor.DACL()
	if err != nil {
		t.Fatal(err)
	}
	var pin runtime.Pinner
	pin.Pin(sid)
	defer pin.Unpin()
	merged, err := windows.ACLFromEntries([]windows.EXPLICIT_ACCESS{{
		AccessPermissions: windows.ACCESS_MASK(grantMask), AccessMode: windows.GRANT_ACCESS,
		Inheritance: windows.SUB_CONTAINERS_AND_OBJECTS_INHERIT,
		Trustee:     windows.TRUSTEE{TrusteeForm: windows.TRUSTEE_IS_SID, TrusteeType: windows.TRUSTEE_IS_UNKNOWN, TrusteeValue: windows.TrusteeValueFromSID(sid)},
	}}, current)
	runtime.KeepAlive(descriptor)
	if err != nil {
		t.Fatal(err)
	}
	if err := windows.SetNamedSecurityInfo(workspace, windows.SE_FILE_OBJECT, windows.DACL_SECURITY_INFORMATION, nil, nil, merged, nil); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name                 string
		sid                  *windows.SID
		mode                 windows.ACCESS_MODE
		first, second, label bool
	}{
		{"repair", sid, windows.GRANT_ACCESS, true, false, true},
		{"idempotent", sid, windows.GRANT_ACCESS, true, false, true},
		{"shared", second, windows.GRANT_ACCESS, true, true, true},
		{"revoke-first", sid, windows.REVOKE_ACCESS, false, true, true},
		{"revoke-last", second, windows.REVOKE_ACCESS, false, false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if err := updatePathGrant(workspace, tc.sid, tc.mode); err != nil {
				t.Fatal(err)
			}
			descriptor, err := windows.GetNamedSecurityInfo(workspace, windows.SE_FILE_OBJECT, windows.DACL_SECURITY_INFORMATION|windows.LABEL_SECURITY_INFORMATION)
			if err != nil {
				t.Fatal(err)
			}
			dacl, _, err := descriptor.DACL()
			if err != nil {
				t.Fatal(err)
			}
			sacl, _, err := descriptor.SACL()
			if err != nil && !errors.Is(err, windows.ERROR_OBJECT_NOT_FOUND) {
				t.Fatal(err)
			}
			if got := countACLFixtureACE(t, dacl, 0, 3, uint32(grantMask), sid.String()); got != boolCount(tc.first) {
				t.Fatalf("workspace allow count=%d want=%t", got, tc.first)
			}
			if got := countACLFixtureACE(t, dacl, 0, 3, uint32(grantMask), second.String()); got != boolCount(tc.second) {
				t.Fatalf("second allow count=%d want=%t", got, tc.second)
			}
			if got := countACLFixtureACE(t, dacl, 1, 2, 0x40, "S-1-1-0"); got != 1 {
				t.Fatalf("Everyone FILE_DELETE_CHILD deny must be CI-only exactly once: %d", got)
			}
			if got := countACLFixtureACE(t, sacl, 17, 3, 1, "S-1-16-4096"); got != boolCount(tc.label) {
				t.Fatalf("Low no-write-up label count=%d want=%t", got, tc.label)
			}
			runtime.KeepAlive(descriptor)
		})
	}
}

func boolCount(value bool) int {
	if value {
		return 1
	}
	return 0
}

func countACLFixtureACE(t *testing.T, acl *windows.ACL, kind, flags byte, mask uint32, sid string) int {
	t.Helper()
	if acl == nil {
		return 0
	}
	count := 0
	for i := uint32(0); i < uint32(acl.AceCount); i++ {
		var ace *windows.ACCESS_ALLOWED_ACE
		if err := windows.GetAce(acl, i, &ace); err != nil {
			t.Fatal(err)
		}
		if ace.Header.AceType == kind && ace.Header.AceFlags == flags && uint32(ace.Mask) == mask && (*windows.SID)(unsafe.Pointer(&ace.SidStart)).String() == sid {
			count++
		}
	}
	return count
}

type aclFixtureState struct {
	PID       int    `json:"pid"`
	Temp      string `json:"temp"`
	Tmp       string `json:"tmp"`
	Integrity string `json:"integrity,omitempty"`
}

// Child operations run in the actual restricted token. The ordinary test binary
// never takes this path; no environment flag can accidentally enter it.
func init() {
	if len(os.Args) < 3 || os.Args[1] != "__pero_acl_test_child" {
		return
	}
	if err := runACLFixtureChild(os.Args[2:]); err != nil {
		if errors.Is(err, os.ErrPermission) {
			fmt.Fprintln(os.Stderr, "fixture-denied:", err)
			os.Exit(3)
		}
		fmt.Fprintln(os.Stderr, "fixture-error:", err)
		os.Exit(4)
	}
	os.Exit(0)
}

func runACLFixtureChild(args []string) error {
	switch args[0] {
	case "write":
		if err := os.WriteFile(args[1], []byte(args[2]), 0600); err != nil {
			return err
		}
		fmt.Println("fixture-ok")
	case "delete":
		if err := os.Remove(args[1]); err != nil {
			return err
		}
		fmt.Println("fixture-ok")
	case "read":
		data, err := os.ReadFile(args[1])
		if err != nil {
			return err
		}
		fmt.Print(string(data))
	case "connect":
		connection, err := net.DialTimeout("tcp", args[1], 3*time.Second)
		if err != nil {
			return err
		}
		defer connection.Close()
		fmt.Println("fixture-connected")
	case "inspect":
		var token windows.Token
		if err := windows.OpenProcessToken(windows.CurrentProcess(), windows.TOKEN_QUERY, &token); err != nil {
			return err
		}
		defer token.Close()
		var needed uint32
		_ = windows.GetTokenInformation(token, windows.TokenIntegrityLevel, nil, 0, &needed)
		buffer := make([]byte, needed)
		if len(buffer) == 0 {
			return errors.New("token integrity query returned no bytes")
		}
		if err := windows.GetTokenInformation(token, windows.TokenIntegrityLevel, &buffer[0], needed, &needed); err != nil {
			return err
		}
		label := (*windows.SIDAndAttributes)(unsafe.Pointer(&buffer[0]))
		state := aclFixtureState{PID: os.Getpid(), Temp: os.Getenv("TEMP"), Tmp: os.Getenv("TMP"), Integrity: label.Sid.String()}
		if err := os.WriteFile(filepath.Join(state.Temp, "child.txt"), []byte("private"), 0600); err != nil {
			return err
		}
		return json.NewEncoder(os.Stdout).Encode(state)
	case "tree", "hold":
		state := aclFixtureState{PID: os.Getpid(), Temp: os.Getenv("TEMP"), Tmp: os.Getenv("TMP")}
		filename := "child.json"
		if args[0] == "tree" {
			filename = "parent.json"
			executable, err := os.Executable()
			if err != nil {
				return err
			}
			child := exec.Command(executable, "__pero_acl_test_child", "hold", args[1])
			child.Stdout, child.Stderr = os.Stdout, os.Stderr
			if err := child.Start(); err != nil {
				return err
			}
		}
		data, err := json.Marshal(state)
		if err != nil {
			return err
		}
		if err := os.WriteFile(filepath.Join(args[1], filename), data, 0600); err != nil {
			return err
		}
		for {
			if args[0] == "tree" {
				if _, err := os.Stat(filepath.Join(args[1], "exit")); err == nil {
					return nil
				}
			}
			time.Sleep(10 * time.Millisecond) // bounded by the parent-owned Job Object
		}
	default:
		return fmt.Errorf("unknown fixture operation %q", args[0])
	}
	return nil
}
