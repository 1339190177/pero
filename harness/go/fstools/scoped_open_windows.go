package fstools

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

const nativeFileReplacement = false

// Windows names have aliases beyond slash/dot traversal. The public tools use
// one canonical spelling and never address streams, devices or DOS aliases.
func nativeRelativePath(name string) bool {
	if !filepath.IsLocal(name) || strings.ContainsAny(name, `\\:<>"|?*`) {
		return false
	}
	for _, part := range strings.Split(name, "/") {
		if strings.TrimRight(part, ". ") != part {
			return false
		}
		for _, r := range part {
			if r < 32 {
				return false
			}
		}
		base := strings.ToUpper(strings.TrimRight(strings.SplitN(part, ".", 2)[0], " "))
		switch base {
		case "CON", "PRN", "AUX", "NUL", "CONIN$", "CONOUT$":
			return false
		}
		if strings.HasPrefix(base, "COM") || strings.HasPrefix(base, "LPT") {
			switch base[3:] {
			case "1", "2", "3", "4", "5", "6", "7", "8", "9", "¹", "²", "³":
				return false
			}
		}
	}
	return true
}

func scopedLink(info os.FileInfo) bool {
	attrs, ok := info.Sys().(*syscall.Win32FileAttributeData)
	return !ok || attrs.FileAttributes&windows.FILE_ATTRIBUTE_REPARSE_POINT != 0
}

// os.Root follows relative reparse links which remain inside its root. Native
// file tools have the narrower no-selected-reparse policy, so use the pinned
// root handle with OBJ_DONT_REPARSE instead. Do not fall back after rejection.
func openScopedTextFile(root *os.Root, name string) (*os.File, error) {
	parent, err := root.Open(".")
	if err != nil {
		return nil, err
	}
	defer parent.Close()
	return openWindowsFileAt(parent, name, windows.FILE_GENERIC_READ, windows.FILE_OPEN,
		windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE, 0)
}

// The parent handle and relative name are the authority; no absolute child path
// is reopened. Additional modes are limited to read and exclusive staging.
func openWindowsFileAt(parent *os.File, name string, access, disposition, share, options uint32) (*os.File, error) {
	if name == "." {
		name = ""
	} // NT opens the parent handle with an empty relative name.
	objectName, err := windows.NewNTUnicodeString(filepath.FromSlash(name))
	if err != nil {
		return nil, err
	}
	attrs := windows.OBJECT_ATTRIBUTES{RootDirectory: windows.Handle(parent.Fd()), ObjectName: objectName, Attributes: windows.OBJ_DONT_REPARSE | windows.OBJ_CASE_INSENSITIVE}
	attrs.Length = uint32(unsafe.Sizeof(attrs))
	var handle windows.Handle
	err = windows.NtCreateFile(&handle, access, &attrs, &windows.IO_STATUS_BLOCK{}, nil, windows.FILE_ATTRIBUTE_NORMAL,
		share, disposition,
		windows.FILE_SYNCHRONOUS_IO_NONALERT|windows.FILE_OPEN_REPARSE_POINT|windows.FILE_OPEN_FOR_BACKUP_INTENT|options, 0, 0)
	if err != nil {
		return nil, &os.PathError{Op: "openat", Path: name, Err: err}
	}
	file := os.NewFile(uintptr(handle), name)
	var info windows.ByHandleFileInformation
	if err = windows.GetFileInformationByHandle(handle, &info); err == nil && info.FileAttributes&windows.FILE_ATTRIBUTE_REPARSE_POINT != 0 {
		err = errors.New("reparse paths are not allowed")
	}
	if err != nil {
		file.Close()
		return nil, err
	}
	return file, nil
}

// Resolve only the explicitly selected launch root; child paths never use this
// pathname API. GetFinalPathNameByHandle also resolves directory junctions,
// unlike filepath.EvalSymlinks on current Go Windows builds.
func canonicalWorkspacePath(name string) (string, error) {
	file, err := os.Open(name)
	if err != nil {
		return "", err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil || !info.IsDir() {
		return "", errors.New("workspace must be a directory")
	}
	buffer := make([]uint16, windows.MAX_LONG_PATH)
	length, err := windows.GetFinalPathNameByHandle(windows.Handle(file.Fd()), &buffer[0], uint32(len(buffer)), 0)
	if err != nil {
		return "", err
	}
	if length == 0 || length >= uint32(len(buffer)) {
		return "", errors.New("Windows workspace path exceeds limit")
	}
	canonical := strings.TrimPrefix(windows.UTF16ToString(buffer[:length]), `\\?\`)
	if len(canonical) < 3 || canonical[1:3] != `:\` || !filepath.IsAbs(canonical) {
		return "", errors.New("Windows workspace must be on a local drive")
	}
	return canonical, nil
}

// File.Stat captures the Windows identity eagerly. Lstat may defer the file ID
// lookup until SameFile, when a replaced pathname could identify another object.
func scopedPathInfo(name string) (os.FileInfo, error) {
	path, err := windows.UTF16PtrFromString(name)
	if err != nil {
		return nil, err
	}
	handle, err := windows.CreateFile(path, windows.FILE_READ_ATTRIBUTES, windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE, nil,
		windows.OPEN_EXISTING, windows.FILE_FLAG_BACKUP_SEMANTICS|windows.FILE_FLAG_OPEN_REPARSE_POINT, 0)
	if err != nil {
		return nil, err
	}
	file := os.NewFile(uintptr(handle), name)
	defer file.Close()
	info, err := file.Stat()
	if err == nil && scopedLink(info) {
		return nil, errors.New("reparse workspace root is not allowed")
	}
	return info, err
}

func scopedEntryLink(entry os.DirEntry) (bool, error) {
	info, err := entry.Info()
	if err != nil {
		return false, err
	}
	return scopedLink(info), nil
}
