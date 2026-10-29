//go:build windows

package sandbox

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"runtime"
	"strings"
	"unsafe"

	"golang.org/x/sys/windows"
)

// DSH grants a capability allow, container-only ambient-delete deny and Low
// no-write-up label atomically. Removing FILE_DELETE_CHILD from only the allow
// is insufficient: Windows can authorize deletion through the parent DACL.
func updatePathGrant(path string, sid *windows.SID, mode windows.ACCESS_MODE) error {
	digest := sha256.Sum256([]byte(strings.ToLower(path)))
	name, _ := windows.UTF16PtrFromString(fmt.Sprintf("Local\\PeroSandboxACL-%x", digest[:8]))
	mutex, err := windows.CreateMutex(nil, false, name)
	if err != nil && !errors.Is(err, windows.ERROR_ALREADY_EXISTS) {
		return err
	}
	defer windows.CloseHandle(mutex)
	state, err := windows.WaitForSingleObject(mutex, windows.INFINITE)
	if err != nil || (state != windows.WAIT_OBJECT_0 && state != windows.WAIT_ABANDONED) {
		return fmt.Errorf("ACL mutex wait state=%d: %v", state, err)
	}
	defer windows.ReleaseMutex(mutex)

	descriptor, err := windows.GetNamedSecurityInfo(path, windows.SE_FILE_OBJECT, windows.DACL_SECURITY_INFORMATION|windows.LABEL_SECURITY_INFORMATION)
	if err != nil {
		return fmt.Errorf("GetNamedSecurityInfo: %w", err)
	}
	current, _, err := descriptor.DACL()
	if err != nil {
		return err
	}
	// Revoking an unapplied grant must not turn a NULL DACL into deny-all.
	if mode == windows.REVOKE_ACCESS && current == nil {
		return nil
	}
	label, _, err := descriptor.SACL()
	if errors.Is(err, windows.ERROR_OBJECT_NOT_FOUND) {
		label, err = nil, nil
	}
	if err != nil {
		return err
	}
	world, err := windows.CreateWellKnownSid(windows.WinWorldSid)
	if err != nil {
		return err
	}
	low, err := windows.StringToSid("S-1-16-4096")
	if err != nil {
		return err
	}
	if mode == windows.GRANT_ACCESS && hasExactCapabilityGrant(current, sid) &&
		hasExactACE(current, windows.ACCESS_DENIED_ACE_TYPE, windows.CONTAINER_INHERIT_ACE, fileDeleteChild, world) &&
		hasExactACE(label, 0x11, windows.SUB_CONTAINERS_AND_OBJECTS_INHERIT, 1, low) {
		return nil
	}
	entry := func(s *windows.SID, access windows.ACCESS_MODE, mask uint32, inheritance uint32) windows.EXPLICIT_ACCESS {
		return windows.EXPLICIT_ACCESS{AccessPermissions: windows.ACCESS_MASK(mask), AccessMode: access, Inheritance: inheritance,
			Trustee: windows.TRUSTEE{TrusteeForm: windows.TRUSTEE_IS_SID, TrusteeType: windows.TRUSTEE_IS_UNKNOWN, TrusteeValue: windows.TrusteeValueFromSID(s)}}
	}
	var pinner runtime.Pinner
	pinner.Pin(sid)
	pinner.Pin(world)
	defer pinner.Unpin()
	entries := []windows.EXPLICIT_ACCESS{entry(sid, mode, 0, windows.SUB_CONTAINERS_AND_OBJECTS_INHERIT)}
	flags := windows.DACL_SECURITY_INFORMATION | windows.LABEL_SECURITY_INFORMATION
	var labelDescriptor *windows.SECURITY_DESCRIPTOR
	var newLabel *windows.ACL
	if mode == windows.GRANT_ACCESS {
		entries = []windows.EXPLICIT_ACCESS{
			entry(world, windows.DENY_ACCESS, fileDeleteChild, windows.CONTAINER_INHERIT_ACE),
			entry(sid, windows.GRANT_ACCESS, grantMask, windows.SUB_CONTAINERS_AND_OBJECTS_INHERIT),
		}
		labelDescriptor, err = windows.SecurityDescriptorFromString("S:(ML;OICI;NW;;;LW)")
		if err != nil {
			return err
		}
		newLabel, _, err = labelDescriptor.SACL()
		if err != nil {
			return err
		}
	} else if mode != windows.REVOKE_ACCESS {
		return errors.New("unsupported ACL edit")
	} else if hasForeignCapabilityGrant(current, sid) {
		// Another live grant still needs the shared Low label.
		flags = windows.DACL_SECURITY_INFORMATION
	}
	merged, err := windows.ACLFromEntries(entries, current)
	runtime.KeepAlive(descriptor)
	if err != nil {
		return fmt.Errorf("SetEntriesInAcl: %w", err)
	}
	err = windows.SetNamedSecurityInfo(path, windows.SE_FILE_OBJECT, windows.SECURITY_INFORMATION(flags), nil, nil, merged, newLabel)
	runtime.KeepAlive(labelDescriptor)
	if err != nil {
		return fmt.Errorf("SetNamedSecurityInfo: %w", err)
	}
	return nil
}

func hasExactACE(acl *windows.ACL, kind, inheritance byte, mask uint32, sid *windows.SID) bool {
	if acl == nil {
		return false
	}
	for i := uint32(0); i < uint32(acl.AceCount); i++ {
		var ace *windows.ACCESS_ALLOWED_ACE
		if windows.GetAce(acl, i, &ace) != nil || ace == nil {
			return false
		}
		if ace.Header.AceType == kind && ace.Header.AceFlags == inheritance && uint32(ace.Mask) == mask &&
			(*windows.SID)(unsafe.Pointer(&ace.SidStart)).Equals(sid) {
			return true
		}
	}
	return false
}

func hasExactCapabilityGrant(acl *windows.ACL, sid *windows.SID) bool {
	return hasExactACE(acl, windows.ACCESS_ALLOWED_ACE_TYPE, windows.SUB_CONTAINERS_AND_OBJECTS_INHERIT, grantMask, sid)
}

func hasForeignCapabilityGrant(acl *windows.ACL, sid *windows.SID) bool {
	if acl == nil {
		return false
	}
	for i := uint32(0); i < uint32(acl.AceCount); i++ {
		var ace *windows.ACCESS_ALLOWED_ACE
		if windows.GetAce(acl, i, &ace) != nil || ace == nil {
			return true
		}
		if ace.Header.AceType == windows.ACCESS_ALLOWED_ACE_TYPE && uint32(ace.Mask) == grantMask &&
			!(*windows.SID)(unsafe.Pointer(&ace.SidStart)).Equals(sid) {
			return true
		}
	}
	return false
}
