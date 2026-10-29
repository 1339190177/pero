package sandbox

import (
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestProfilesDenyNetworkAndHostRoot(t *testing.T) {
	p := Policy{Mode: ReadOnly, WorkspaceRoot: "/work", Strict: &StrictProfile{ReadOnlyPaths: []string{"/usr/bin"}}}
	mounts := []strictMount{{source: "/usr/bin", destination: "/usr/bin"}}
	args := strictBwrapProfile(p, mounts, true)
	s := strings.Join(args, " ")
	for _, want := range []string{"--unshare-net", "--clearenv", "--cap-drop ALL", "--ro-bind /work /work", "--remount-ro /"} {
		if !strings.Contains(s, want) {
			t.Fatal(want, s)
		}
	}
	if strings.Contains(s, "--ro-bind / /") {
		t.Fatal("host root exported")
	}
	seat := strictSeatbeltProfile(p, mounts, true)
	if !strings.Contains(seat, "(deny default)") || !strings.Contains(seat, "(deny network*)") || strings.Contains(seat, "file-write* (subpath") {
		t.Fatal(seat)
	}
	p.Mode = WorkspaceWrite
	if !strings.Contains(strings.Join(strictBwrapProfile(p, mounts, true), " "), "--bind /work /work") {
		t.Fatal("workspace grant absent")
	}
}
func TestInvalidPolicies(t *testing.T) {
	for _, mode := range []Mode{"", "danger-full-access", "auto"} {
		if _, e := New(Config{Mode: mode}, t.TempDir()); e == nil {
			t.Fatal(mode)
		}
	}
	for _, paths := range [][]string{nil, {"/"}, {"/tmp"}, {"/usr/../usr"}, {"/usr/bin", "/usr/bin"}} {
		if e := ValidateStrictProfile(StrictProfile{ReadOnlyPaths: paths}, t.TempDir()); e == nil {
			t.Fatal(paths)
		}
	}
	if strictRuntimeFileType(os.ModeSocket) || strictRuntimeFileType(os.ModeNamedPipe) || strictRuntimeFileType(os.ModeDevice) {
		t.Fatal("IPC runtime allowed")
	}
}
func TestUnsupportedFailsClosed(t *testing.T) {
	p := LocalProvider{options: localOptions{Platform: "unsupported"}}
	_, e := p.confineStrict([]string{"/usr/bin/true"}, Policy{Mode: ReadOnly, WorkspaceRoot: t.TempDir(), Strict: &StrictProfile{ReadOnlyPaths: []string{"/usr/bin/true"}}})
	var u *UnavailableError
	if !errors.As(e, &u) {
		t.Fatal(e)
	}
}
func TestNativeRuntime(t *testing.T) {
	required := os.Getenv("PERO_REQUIRE_STRICT_SANDBOX") == "1"
	if runtime.GOOS != "linux" {
		if required {
			t.Fatal("strict sandbox positive fixture requires Linux")
		}
		t.Skip("Linux runtime validation; Darwin profile fixture only")
	}
	if _, e := os.Stat("/usr/bin/bwrap"); e != nil {
		if required {
			t.Fatal("required /usr/bin/bwrap absent")
		}
		t.Skip("UNSUPPORTED: /usr/bin/bwrap absent")
	}
	work := t.TempDir()
	outside := t.TempDir()
	secret := filepath.Join(outside, "private")
	os.WriteFile(secret, []byte("fixture-secret"), 0600)
	paths := []string{"/usr/bin/true", "/usr/bin/bash", "/usr/bin/cat", "/lib/x86_64-linux-gnu/libc.so.6", "/lib/x86_64-linux-gnu/libtinfo.so.6", "/lib64/ld-linux-x86-64.so.2"}
	e, err := New(Config{Mode: WorkspaceWrite, RuntimePaths: paths}, work)
	if err != nil {
		var u *UnavailableError
		if !errors.As(err, &u) {
			t.Fatal(err)
		}
		if required {
			t.Fatalf("required strict sandbox unavailable: %v", err)
		}
		t.Skipf("UNSUPPORTED runtime: %v; candidate command not launched", err)
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	port := listener.Addr().(*net.TCPAddr).Port
	command := "printf ok > '" + work + "/ok'; if /usr/bin/cat '" + secret + "'; then exit 41; fi; if printf bad > '" + secret + "'; then exit 42; fi; if : > /dev/tcp/127.0.0.1/" + fmt.Sprint(port) + "; then exit 43; fi"
	c, err := e.Confine([]string{"/usr/bin/bash", "--noprofile", "--norc", "-c", command})
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(c.Argv[0], c.Argv[1:]...)
	cmd.Env = e.Environment()
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("%v: %s", err, out)
	}
	if b, err := os.ReadFile(filepath.Join(work, "ok")); err != nil || string(b) != "ok" {
		t.Fatal("allowed write failed", err)
	}
	if b, _ := os.ReadFile(secret); string(b) != "fixture-secret" {
		t.Fatal("outside mutation")
	}
}

func TestWorkspaceRejectsOutsideHardlinks(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	source := filepath.Join(outside, "source")
	if e := os.WriteFile(source, []byte("fixture"), 0600); e != nil {
		t.Fatal(e)
	}
	if e := os.Link(source, filepath.Join(root, "linked")); e != nil {
		t.Fatal(e)
	}
	if e := validateWorkspace(root); e == nil {
		t.Fatal("outside hardlink accepted")
	}
}

func TestWorkspaceRejectsHostSocket(t *testing.T) {
	root := t.TempDir()
	socket, e := net.Listen("unix", filepath.Join(root, "host.sock"))
	if e != nil {
		t.Skipf("host blocks Unix socket fixture: %v; socket file-type rejection unit tested", e)
	}
	defer socket.Close()
	if e := validateWorkspace(root); e == nil {
		t.Fatal("host Unix socket accepted")
	}
}
