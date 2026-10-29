//go:build windows

package sandbox

import (
	"errors"
	"fmt"
	"golang.org/x/sys/windows"
	"os"
	"os/exec"
	"path/filepath"
	"time"
	"unsafe"
)

func spawnRestrictedInherited(token windows.Token, argv []string, cwd string, cancelled <-chan struct{}) (code uint32, resultErr error) {
	select {
	case <-cancelled:
		return 1, nil
	default:
	}
	program, err := exec.LookPath(argv[0])
	if err != nil {
		return 0, err
	}
	program, err = filepath.Abs(program)
	if err != nil {
		return 0, err
	}
	application, err := windows.UTF16PtrFromString(program)
	if err != nil {
		return 0, err
	}
	commandLine, err := windows.UTF16PtrFromString(windows.ComposeCommandLine(argv))
	if err != nil {
		return 0, err
	}
	directory, err := windows.UTF16PtrFromString(cwd)
	if err != nil {
		return 0, err
	}
	job, err := windows.CreateJobObject(nil, nil)
	if err != nil {
		return 0, fmt.Errorf("CreateJobObject: %w", err)
	}
	defer func() {
		terminateErr := windows.TerminateJobObject(job, 1)
		emptyErr := waitWindowsJobEmpty(job)
		closeErr := windows.CloseHandle(job)
		resultErr = errors.Join(resultErr, terminateErr, emptyErr, closeErr)
	}()
	limits := windows.JOBOBJECT_EXTENDED_LIMIT_INFORMATION{}
	limits.BasicLimitInformation.LimitFlags = windows.JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE
	if _, err := windows.SetInformationJobObject(job, windows.JobObjectExtendedLimitInformation, uintptr(unsafe.Pointer(&limits)), uint32(unsafe.Sizeof(limits))); err != nil {
		return 0, fmt.Errorf("SetInformationJobObject: %w", err)
	}
	null, err := os.Open(os.DevNull)
	if err != nil {
		return 0, err
	}
	defer null.Close()
	stdin := windows.Handle(null.Fd())
	stdout, err := windows.GetStdHandle(windows.STD_OUTPUT_HANDLE)
	if err != nil {
		return 0, fmt.Errorf("GetStdHandle stdout: %w", err)
	}
	stderr, err := windows.GetStdHandle(windows.STD_ERROR_HANDLE)
	if err != nil {
		return 0, fmt.Errorf("GetStdHandle stderr: %w", err)
	}
	handles := []windows.Handle{stdin, stdout}
	if stderr != stdout {
		handles = append(handles, stderr)
	}
	enabled := make([]windows.Handle, 0, len(handles))
	for _, handle := range handles {
		if err := windows.SetHandleInformation(handle, windows.HANDLE_FLAG_INHERIT, windows.HANDLE_FLAG_INHERIT); err != nil {
			for _, inherited := range enabled {
				_ = windows.SetHandleInformation(inherited, windows.HANDLE_FLAG_INHERIT, 0)
			}
			return 0, fmt.Errorf("SetHandleInformation enable inherit: %w", err)
		}
		enabled = append(enabled, handle)
	}
	defer func() {
		for _, handle := range enabled {
			_ = windows.SetHandleInformation(handle, windows.HANDLE_FLAG_INHERIT, 0)
		}
	}()
	attributes, err := windows.NewProcThreadAttributeList(1)
	if err != nil {
		return 0, err
	}
	defer attributes.Delete()
	if err := attributes.Update(windows.PROC_THREAD_ATTRIBUTE_HANDLE_LIST, unsafe.Pointer(&handles[0]), uintptr(len(handles))*unsafe.Sizeof(handles[0])); err != nil {
		return 0, err
	}
	startup := windows.StartupInfoEx{StartupInfo: windows.StartupInfo{Cb: uint32(unsafe.Sizeof(windows.StartupInfoEx{})), Flags: windows.STARTF_USESTDHANDLES}, ProcThreadAttributeList: attributes.List()}
	startup.StdInput = stdin
	startup.StdOutput = stdout
	startup.StdErr = stderr
	var process windows.ProcessInformation
	flags := uint32(windows.CREATE_SUSPENDED | windows.CREATE_UNICODE_ENVIRONMENT | windows.EXTENDED_STARTUPINFO_PRESENT)
	if err := windows.CreateProcessAsUser(token, application, commandLine, nil, nil, true, flags, nil, directory, &startup.StartupInfo, &process); err != nil {
		return 0, fmt.Errorf("CreateProcessAsUser: %w", err)
	}
	defer windows.CloseHandle(process.Process)
	defer windows.CloseHandle(process.Thread)
	if err := windows.AssignProcessToJobObject(job, process.Process); err != nil {
		terminateErr := windows.TerminateProcess(process.Process, 1)
		_, waitErr := windows.WaitForSingleObject(process.Process, windows.INFINITE)
		return 0, errors.Join(fmt.Errorf("AssignProcessToJobObject: %w", err), terminateErr, waitErr)
	}
	// Setup may have outlived the owner. Recheck after Job assignment while the
	// child is still suspended; the deferred Job teardown reaps it without ever
	// running candidate instructions when cancellation has already been observed.
	select {
	case <-cancelled:
		return 1, nil
	default:
	}
	if _, err := windows.ResumeThread(process.Thread); err != nil {
		_ = windows.TerminateProcess(process.Process, 1)
		return 0, fmt.Errorf("ResumeThread: %w", err)
	}
	// Poll the observable child handle while accepting parent EOF cancellation.
	for {
		select {
		case <-cancelled:
			if err := windows.TerminateJobObject(job, 1); err != nil {
				return 0, fmt.Errorf("TerminateJobObject: %w", err)
			}
			cancelled = nil
		default:
		}
		state, err := windows.WaitForSingleObject(process.Process, 20)
		if err != nil {
			return 0, fmt.Errorf("WaitForSingleObject child: %w", err)
		}
		if state == windows.WAIT_OBJECT_0 {
			break
		}
		if state != uint32(windows.WAIT_TIMEOUT) {
			return 0, fmt.Errorf("unexpected child wait state %d", state)
		}
	}
	// A foreground child's exit is not proof its descendants are gone. Terminate
	// the owned Job and observe ActiveProcesses=0 before revoking temp authority.
	// The deferred termination and observed-empty check runs before grants revoke.
	var exitCode uint32
	if err := windows.GetExitCodeProcess(process.Process, &exitCode); err != nil {
		return 0, fmt.Errorf("GetExitCodeProcess: %w", err)
	}
	return exitCode, nil
}

func waitWindowsJobEmpty(job windows.Handle) error {
	type accounting struct {
		TotalUserTime, TotalKernelTime, ThisPeriodTotalUserTime, ThisPeriodTotalKernelTime int64
		TotalPageFaultCount, TotalProcesses, ActiveProcesses, TotalTerminatedProcesses     uint32
	}
	deadline := time.Now().Add(4 * time.Second)
	for {
		var info accounting
		if err := windows.QueryInformationJobObject(job, windows.JobObjectBasicAccountingInformation, uintptr(unsafe.Pointer(&info)), uint32(unsafe.Sizeof(info)), nil); err != nil {
			return fmt.Errorf("QueryInformationJobObject: %w", err)
		}
		if info.ActiveProcesses == 0 {
			return nil
		}
		if time.Now().After(deadline) {
			return errors.New("Windows Job still active; descendant cleanup uncertain")
		}
		time.Sleep(10 * time.Millisecond)
	}
}
