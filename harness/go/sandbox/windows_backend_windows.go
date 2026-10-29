//go:build windows

package sandbox

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"golang.org/x/sys/windows"
)

func (e *Executor) checkWindowsACL() error {
	if err := validateWindowsWorkspace(e.policy.WorkspaceRoot); err != nil {
		return err
	}
	program, err := os.Executable()
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	// This fixed read-only self-probe creates no directory grants or labels and
	// never accepts candidate code. A failure cannot fall back to normal exec.
	cmd := exec.CommandContext(ctx, program, windowsACLRunnerArg, "--probe")
	cmd.Env = windowsEnvironment()
	output, err := cmd.CombinedOutput()
	if err != nil {
		return &UnavailableError{Mode: e.policy.Mode, Detail: fmt.Sprintf("Windows partial ACL probe failed: %s", strings.TrimSpace(string(output))), Cause: err}
	}
	return nil
}

func (e *Executor) confineWindowsACL(argv []string) (Confined, error) {
	if err := validateWindowsWorkspace(e.policy.WorkspaceRoot); err != nil {
		return Confined{}, err
	}
	if !filepath.IsAbs(argv[0]) {
		return Confined{}, errors.New("Windows ACL requires an absolute executable")
	}
	program, err := os.Executable()
	if err != nil {
		return Confined{}, err
	}
	prefix := []string{program, windowsACLRunnerArg, "--workspace", e.policy.WorkspaceRoot, "--temp", os.TempDir(), "--mode", string(e.policy.Mode), "--control-stdin", "--"}
	return Confined{Argv: append(prefix, argv...), Enforcement: EnforcementPartial,
		ReadIsolated: false, NetworkIsolated: false,
		DenialSignatures:   []string{"access is denied", "access to the path", "permission denied"},
		RunnerFailureRules: []RunnerFailureRule{{FatalSignatures: []string{windowsACLRunnerSignature + ": "}}}}, nil
}

// The wrapper and child receive no ambient credentials or profile environment.
// This is not read isolation: a child can still read host-visible files.
func windowsEnvironment() []string {
	root, err := windows.GetWindowsDirectory()
	if err != nil {
		return []string{}
	}
	return []string{"SystemRoot=" + root, "WINDIR=" + root,
		"PATH=" + filepath.Join(root, "System32"), "TEMP=" + os.TempDir(), "TMP=" + os.TempDir(),
		"NO_COLOR=1", "TERM=dumb"}
}

// ACL inheritance is pathname-based. Reject reparse points and hardlink aliases
// before any grant, and require caller-owned ancestors/no external mutation
// during admission and launch, as the strict mount backend already does.
func validateWindowsWorkspace(root string) error {
	canonical, err := filepath.EvalSymlinks(root)
	if err != nil || canonical != root || !filepath.IsAbs(root) || filepath.Dir(root) == root || strings.HasPrefix(root, `\\`) {
		return errors.New("Windows ACL requires a canonical local non-root workspace")
	}
	deadline, count := time.Now().Add(5*time.Second), 0
	return filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		count++
		if count > 500000 || time.Now().After(deadline) {
			return errors.New("Windows workspace inspection exceeded budget")
		}
		name, err := windows.UTF16PtrFromString(path)
		if err != nil {
			return err
		}
		h, err := windows.CreateFile(name, 0, windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE, nil, windows.OPEN_EXISTING, windows.FILE_FLAG_OPEN_REPARSE_POINT|windows.FILE_FLAG_BACKUP_SEMANTICS, 0)
		if err != nil {
			return err
		}
		var info windows.ByHandleFileInformation
		err = windows.GetFileInformationByHandle(h, &info)
		closeErr := windows.CloseHandle(h)
		if err != nil || closeErr != nil {
			return errors.Join(err, closeErr)
		}
		if info.FileAttributes&windows.FILE_ATTRIBUTE_REPARSE_POINT != 0 {
			return errors.New("Windows ACL workspace cannot contain reparse points")
		}
		if !entry.IsDir() && info.NumberOfLinks != 1 {
			return errors.New("Windows ACL workspace files must have exactly one hard link")
		}
		return nil
	})
}
