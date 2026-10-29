//go:build !windows

package sandbox

func (e *Executor) checkWindowsACL() error {
	return &UnavailableError{Mode: e.policy.Mode, Detail: "Windows ACL backend requires Windows; no fallback"}
}
func (e *Executor) confineWindowsACL([]string) (Confined, error) {
	return Confined{}, e.checkWindowsACL()
}
func windowsEnvironment() []string { return nil }
