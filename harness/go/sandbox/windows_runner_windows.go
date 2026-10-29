//go:build windows

package sandbox

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/sys/windows"
)

const (
	disableMaxPrivilege = 0x1
	writeRestricted     = 0x8
	fileGenericWrite    = 0x00120116
	fileDeleteChild     = 0x0040
	fileAllAccess       = 0x1f01ff
	grantMask           = (fileGenericWrite | windows.DELETE | fileDeleteChild) &^ 0x00020000
)

var (
	advapi32                  = windows.NewLazySystemDLL("advapi32.dll")
	createRestrictedTokenProc = advapi32.NewProc("CreateRestrictedToken")
	kernel32                  = windows.NewLazySystemDLL("kernel32.dll")
	setConsoleCtrlHandlerProc = kernel32.NewProc("SetConsoleCtrlHandler")
)

func init() {
	if len(os.Args) < 2 || os.Args[1] != windowsACLRunnerArg {
		return
	}
	code, err := runWindowsACLRunner(os.Args[2:])
	if err != nil {
		fmt.Fprintf(os.Stderr, "%s: %v\n", windowsACLRunnerSignature, err)
		os.Exit(windowsACLRunnerFailureExit)
	}
	os.Exit(int(code))
}

type windowsACLRequest struct {
	workspace string
	tempRoot  string
	mode      Mode
	argv      []string
}

func parseWindowsACLRequest(args []string) (windowsACLRequest, error) {
	var request windowsACLRequest
	for index := 0; index < len(args); {
		if args[index] == "--control-stdin" {
			index++
			continue
		}
		if args[index] == "--" {
			request.argv = append([]string(nil), args[index+1:]...)
			break
		}
		if index+1 >= len(args) {
			return windowsACLRequest{}, fmt.Errorf("missing value after %s", args[index])
		}
		name, value := args[index], args[index+1]
		switch name {
		case "--workspace":
			request.workspace = value
		case "--temp":
			request.tempRoot = value
		case "--mode":
			request.mode = Mode(value)
		default:
			return windowsACLRequest{}, fmt.Errorf("unknown argument: %s", name)
		}
		index += 2
	}
	if request.workspace == "" || request.tempRoot == "" || len(request.argv) == 0 {
		return windowsACLRequest{}, errors.New("--workspace, --temp, --mode, and a command after -- are required")
	}
	if request.mode != ReadOnly && request.mode != WorkspaceWrite {
		return windowsACLRequest{}, fmt.Errorf("unknown mode: %s", request.mode)
	}
	for _, entry := range []struct{ label, path string }{{"--workspace", request.workspace}, {"--temp", request.tempRoot}} {
		info, err := os.Stat(entry.path)
		if err != nil || !info.IsDir() {
			return windowsACLRequest{}, fmt.Errorf("%s is not an existing directory: %s", entry.label, entry.path)
		}
	}
	return request, nil
}

func runWindowsACLRunner(args []string) (code uint32, resultErr error) {
	if len(args) == 1 && args[0] == "--probe-child" {
		return 0, nil
	}
	if len(args) == 1 && args[0] == "--probe" {
		token, err := restrictedWriteToken(ReadOnly, nil, nil)
		if err != nil {
			return 0, err
		}
		defer token.Close()
		executable, err := os.Executable()
		if err != nil {
			return 0, err
		}
		cwd, err := windows.GetWindowsDirectory()
		if err != nil {
			return 0, err
		}
		return spawnRestrictedInherited(token, []string{executable, windowsACLRunnerArg, "--probe-child"}, cwd, nil)
	}
	request, err := parseWindowsACLRequest(args)
	if err != nil {
		return 0, err
	}
	if err := validateWindowsWorkspace(request.workspace); err != nil {
		return 0, fmt.Errorf("inspect workspace %q: %w", request.workspace, err)
	}
	cwd, err := os.Getwd()
	if err != nil {
		return 0, fmt.Errorf("read runner working directory: %w", err)
	}
	cwd, err = canonicalWindowsDirectory(cwd)
	if err != nil || !withinWindowsPath(request.workspace, cwd) {
		return 0, errors.New("runner cwd is outside workspace")
	}
	// Begin observing owner lifetime before setup; an already-cancelled owner
	// cannot launch candidate code after grants and token creation complete.
	cancelled := make(chan struct{})
	go func() { _, _ = io.Copy(io.Discard, os.Stdin); close(cancelled) }()

	tempRoot, err := canonicalWindowsDirectory(request.tempRoot)
	if err != nil {
		return 0, fmt.Errorf("resolve temp root: %w", err)
	}
	// A private temp below the workspace shares its standing SID and is not
	// private from sibling invocations. Reject before creating or granting it.
	if withinWindowsPath(request.workspace, tempRoot) {
		return 0, errors.New("temp root must be outside workspace")
	}
	if result, _, callErr := setConsoleCtrlHandlerProc.Call(0, 1); result == 0 {
		return 0, fmt.Errorf("SetConsoleCtrlHandler: %w", callErr)
	}
	var privateTemp string
	var workspaceSID, tempSID *windows.SID
	if request.mode == WorkspaceWrite {
		privateTemp, err = os.MkdirTemp(tempRoot, "pero-sandbox-")
		if err != nil {
			return 0, fmt.Errorf("create private temp: %w", err)
		}
		defer func() { resultErr = errors.Join(resultErr, os.RemoveAll(privateTemp)) }()
		canonicalTemp, err := canonicalWindowsDirectory(privateTemp)
		if err != nil {
			return 0, fmt.Errorf("resolve private temp %q: %w", privateTemp, err)
		}
		privateTemp = canonicalTemp
		if withinWindowsPath(request.workspace, privateTemp) || withinWindowsPath(privateTemp, request.workspace) {
			return 0, errors.New("private temp and workspace must be disjoint")
		}
		workspaceSID, err = capabilitySID(request.workspace, false)
		if err != nil {
			return 0, err
		}
		tempSID, err = capabilitySID(privateTemp, true)
		if err != nil {
			return 0, err
		}
		if err := updatePathGrant(request.workspace, workspaceSID, windows.GRANT_ACCESS); err != nil {
			return 0, fmt.Errorf("grant workspace: %w", err)
		}
		// Register revocation BEFORE apply, including potentially partial failures.
		defer func() {
			resultErr = errors.Join(resultErr, updatePathGrant(privateTemp, tempSID, windows.REVOKE_ACCESS))
		}()
		if err := updatePathGrant(privateTemp, tempSID, windows.GRANT_ACCESS); err != nil {
			return 0, fmt.Errorf("grant private temp: %w", err)
		}
		if err := os.Setenv("TMP", privateTemp); err != nil {
			return 0, err
		}
		if err := os.Setenv("TEMP", privateTemp); err != nil {
			return 0, err
		}
	}
	token, err := restrictedWriteToken(request.mode, workspaceSID, tempSID)
	if err != nil {
		return 0, err
	}
	defer token.Close()
	// Parent death closes the control pipe; candidate stdin is a separate NUL handle.
	return spawnRestrictedInherited(token, request.argv, cwd, cancelled)
}

func capabilitySID(path string, temporary bool) (*windows.SID, error) {
	return windows.StringToSid(windowsCapabilitySIDString(path, temporary))
}

func withinWindowsPath(root, candidate string) bool {
	relative, err := filepath.Rel(root, candidate)
	return err == nil && relative != ".." && !strings.HasPrefix(relative, `..\`)
}
