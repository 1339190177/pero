package sandbox

import (
	"crypto/sha256"
	"encoding/binary"
	"fmt"
)

const (
	windowsACLRunnerArg         = "__pero_windows_acl_runner"
	windowsACLRunnerSignature   = "windows-acl-run"
	windowsACLRunnerFailureExit = 127
)

// windowsCapabilitySIDString mirrors DSH's workspaceWriteSid/tempWriteSid
// derivation. Keeping the pure wire identity outside the Windows-only runner
// lets every host certify the cross-runtime vectors; StringToSid remains in
// the native implementation.
func windowsCapabilitySIDString(path string, temporary bool) string {
	hash := sha256.New()
	if temporary {
		hash.Write([]byte{'t', 'e', 'm', 'p', 0})
	}
	hash.Write([]byte(path))
	digest := hash.Sum(nil)
	first := binary.LittleEndian.Uint32(digest[:4])%(1<<30-1) + 1
	second := binary.LittleEndian.Uint32(digest[4:8])%(1<<30-1) + 1
	value := fmt.Sprintf("S-1-4-%d-%d", first, second)
	if temporary {
		value += "-1"
	}
	return value
}
