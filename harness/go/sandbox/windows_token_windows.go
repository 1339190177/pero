//go:build windows

package sandbox

import (
	"errors"
	"fmt"
	"golang.org/x/sys/windows"
	"runtime"
	"unsafe"
)

func restrictedWriteToken(mode Mode, workspaceSID, tempSID *windows.SID) (windows.Token, error) {
	var current windows.Token
	access := uint32(windows.TOKEN_QUERY | windows.TOKEN_DUPLICATE | windows.TOKEN_ADJUST_DEFAULT | windows.TOKEN_ASSIGN_PRIMARY)
	if err := windows.OpenProcessToken(windows.CurrentProcess(), access, &current); err != nil {
		return 0, fmt.Errorf("OpenProcessToken: %w", err)
	}
	defer current.Close()
	groups, err := current.GetTokenGroups()
	if err != nil {
		return 0, fmt.Errorf("GetTokenInformation TokenGroups: %w", err)
	}
	var logonSID *windows.SID
	for _, group := range groups.AllGroups() {
		if group.Attributes&windows.SE_GROUP_LOGON_ID == windows.SE_GROUP_LOGON_ID {
			logonSID, err = group.Sid.Copy()
			break
		}
	}
	if err != nil || logonSID == nil {
		return 0, errors.New("CreateRestrictedToken prerequisite failed: no logon SID")
	}
	worldSID, err := windows.CreateWellKnownSid(windows.WinWorldSid)
	if err != nil {
		return 0, fmt.Errorf("CreateWellKnownSid Everyone: %w", err)
	}
	sids := []*windows.SID{logonSID, worldSID}
	defaultSID := worldSID
	if mode == WorkspaceWrite {
		if workspaceSID == nil {
			return 0, errors.New("workspace-write requires workspace capability SID")
		}
		sids = append(sids, workspaceSID)
		defaultSID = workspaceSID
		if tempSID != nil {
			sids = append(sids, tempSID)
			defaultSID = tempSID
		}
	}
	restricting := make([]windows.SIDAndAttributes, len(sids))
	var pinner runtime.Pinner
	for index, sid := range sids {
		pinner.Pin(sid)
		restricting[index] = windows.SIDAndAttributes{Sid: sid}
	}
	defer pinner.Unpin()
	var restricted windows.Token
	result, _, callErr := createRestrictedTokenProc.Call(
		uintptr(current), disableMaxPrivilege|writeRestricted,
		0, 0, 0, 0, uintptr(len(restricting)), uintptr(unsafe.Pointer(&restricting[0])), uintptr(unsafe.Pointer(&restricted)),
	)
	if result == 0 {
		return 0, fmt.Errorf("CreateRestrictedToken: %w", callErr)
	}
	if err := setTokenLowIntegrity(restricted); err != nil {
		restricted.Close()
		return 0, err
	}
	if err := setTokenDefaultACL(restricted, defaultSID); err != nil {
		restricted.Close()
		return 0, err
	}
	return restricted, nil
}

func setTokenDefaultACL(token windows.Token, sid *windows.SID) error {
	var needed uint32
	_ = windows.GetTokenInformation(token, windows.TokenDefaultDacl, nil, 0, &needed)
	if needed == 0 {
		return errors.New("GetTokenInformation TokenDefaultDacl returned zero size")
	}
	buffer := make([]byte, needed)
	if err := windows.GetTokenInformation(token, windows.TokenDefaultDacl, &buffer[0], needed, &needed); err != nil {
		return fmt.Errorf("GetTokenInformation TokenDefaultDacl: %w", err)
	}
	current := *(**windows.ACL)(unsafe.Pointer(&buffer[0]))
	if current == nil {
		return errors.New("restricted token carries no default DACL")
	}
	var pinner runtime.Pinner
	pinner.Pin(sid)
	defer pinner.Unpin()
	entry := windows.EXPLICIT_ACCESS{
		AccessPermissions: windows.ACCESS_MASK(fileAllAccess), AccessMode: windows.GRANT_ACCESS,
		Inheritance: windows.SUB_CONTAINERS_AND_OBJECTS_INHERIT,
		Trustee:     windows.TRUSTEE{TrusteeForm: windows.TRUSTEE_IS_SID, TrusteeType: windows.TRUSTEE_IS_UNKNOWN, TrusteeValue: windows.TrusteeValueFromSID(sid)},
	}
	merged, err := windows.ACLFromEntries([]windows.EXPLICIT_ACCESS{entry}, current)
	if err != nil {
		return fmt.Errorf("SetEntriesInAcl default DACL: %w", err)
	}
	info := struct{ DefaultDACL *windows.ACL }{merged}
	if err := windows.SetTokenInformation(token, windows.TokenDefaultDacl, (*byte)(unsafe.Pointer(&info)), uint32(unsafe.Sizeof(info))); err != nil {
		return fmt.Errorf("SetTokenInformation TokenDefaultDacl: %w", err)
	}
	runtime.KeepAlive(merged)
	return nil
}

// Mandatory integrity complements WRITE_RESTRICTED: ambient Everyone write
// grants on Medium objects must not become an escape from the write allowlist.
func setTokenLowIntegrity(token windows.Token) error {
	low, err := windows.StringToSid("S-1-16-4096")
	if err != nil {
		return err
	}
	var pinner runtime.Pinner
	pinner.Pin(low)
	defer pinner.Unpin()
	label := windows.Tokenmandatorylabel{Label: windows.SIDAndAttributes{Sid: low, Attributes: windows.SE_GROUP_INTEGRITY}}
	if err := windows.SetTokenInformation(token, windows.TokenIntegrityLevel, (*byte)(unsafe.Pointer(&label)), label.Size()); err != nil {
		return fmt.Errorf("SetTokenInformation Low integrity: %w", err)
	}
	return nil
}
