package fstools

import (
	"context"
	"crypto/rand"
	"errors"
	"io"
	"os"
	"path"
	"unsafe"

	"golang.org/x/sys/windows"
)

// Windows publication is deliberately create-only. Replacing an existing file
// by handle rename would discard security/stream metadata; pathname ReplaceFileW
// would reopen untrusted names. Neither is an acceptable silent fallback.
func (w *Workspace) writeNew(ctx context.Context, name, text string) (receipt ScopedTextReceipt, resultErr error) {
	receipt = ScopedTextReceipt{Path: name, AfterDigest: scopedTextDigest([]byte(text)), State: "not-applied"}
	parentName, baseName := path.Dir(name), path.Base(name)
	parent, err := openScopedTextFile(w.root, parentName)
	if err != nil {
		return receipt, err
	}
	defer parent.Close()
	parentInfo, err := parent.Stat()
	if err != nil || !parentInfo.IsDir() {
		return receipt, errors.New("creation parent is not a directory")
	}
	checkParent := func() error {
		if err := ctx.Err(); err != nil {
			return err
		}
		if _, err := w.checked(name, false); err != nil {
			return err
		}
		current, err := w.root.Lstat(parentName)
		if err != nil || !os.SameFile(parentInfo, current) {
			return errors.New("creation parent was replaced")
		}
		return nil
	}
	if err := checkParent(); err != nil {
		return receipt, err
	}
	stage, err := openWindowsFileAt(parent, ".pero-create-"+rand.Text()+".tmp",
		windows.FILE_GENERIC_READ|windows.FILE_GENERIC_WRITE|windows.DELETE, windows.FILE_CREATE,
		windows.FILE_SHARE_READ, windows.FILE_NON_DIRECTORY_FILE)
	if err != nil {
		return receipt, err
	}
	// Before publication, only the owned handle may be deleted. Once publication
	// is attempted an ambiguous failure must not cause deletion of a committed
	// destination; report unknown and leave reconciliation to the caller.
	cleanupStage := true
	defer func() {
		if cleanupStage {
			remove := byte(1)
			resultErr = errors.Join(resultErr, windows.NtSetInformationFile(windows.Handle(stage.Fd()), &windows.IO_STATUS_BLOCK{}, &remove, 1, windows.FileDispositionInformation))
		}
		closeErr := stage.Close()
		resultErr = errors.Join(resultErr, closeErr)
		if resultErr != nil && receipt.State == "applied" {
			receipt.State = "unknown"
		}
	}()
	if _, err := stage.Write([]byte(text)); err != nil {
		return receipt, err
	}
	if err := stage.Sync(); err != nil {
		return receipt, err
	}
	if w.beforePublish != nil {
		w.beforePublish()
	}
	if err := checkParent(); err != nil {
		return receipt, err
	}

	// FILE_RENAME_INFORMATION with ReplaceIfExists=FALSE and a single basename.
	// RootDirectory is the pinned parent, not a pathname discovered after staging.
	leaf, err := windows.UTF16FromString(baseName)
	if err != nil {
		return receipt, err
	}
	type renameInformation struct {
		ReplaceIfExists uint32
		RootDirectory   windows.Handle
		FileNameLength  uint32
		FileName        [1]uint16
	}
	var header renameInformation
	nameBytes := uint32((len(leaf) - 1) * 2)
	buffer := make([]byte, int(unsafe.Offsetof(header.FileName))+int(nameBytes))
	rename := (*renameInformation)(unsafe.Pointer(&buffer[0]))
	rename.RootDirectory = windows.Handle(parent.Fd())
	rename.FileNameLength = nameBytes
	copy(unsafe.Slice(&rename.FileName[0], len(leaf)-1), leaf)
	receipt.State, cleanupStage = "unknown", false
	err = windows.NtSetInformationFile(windows.Handle(stage.Fd()), &windows.IO_STATUS_BLOCK{}, &buffer[0], uint32(len(buffer)), windows.FileRenameInformation)
	if err != nil {
		if errors.Is(err, windows.STATUS_OBJECT_NAME_COLLISION) {
			// This documented no-replace result proves the owned stage is still
			// unpublished, so it is safe to remove by handle.
			receipt.State, cleanupStage = "not-applied", true
		}
		return receipt, err
	}
	if w.afterCreate != nil {
		w.afterCreate()
	}
	if err := checkParent(); err != nil {
		return receipt, err
	}
	writtenInfo, err := stage.Stat()
	if err != nil {
		return receipt, err
	}
	readback, err := openScopedTextFile(w.root, name)
	if err != nil {
		return receipt, err
	}
	defer func() {
		resultErr = errors.Join(resultErr, readback.Close())
		if resultErr != nil && receipt.State == "applied" {
			receipt.State = "unknown"
		}
	}()
	readInfo, err := readback.Stat()
	if err != nil || !sameFileInfo(writtenInfo, readInfo) {
		return receipt, errors.New("created target identity is uncertain")
	}
	data, err := io.ReadAll(io.LimitReader(readback, MaxFileBytes+1))
	if err != nil || string(data) != text {
		return receipt, errors.New("created target readback is uncertain")
	}
	if err := checkParent(); err != nil {
		return receipt, err
	}
	// Content was flushed before atomic publication. Windows does not expose
	// the Unix directory-fsync contract; applied records observed readback only.
	receipt.State = "applied"
	return receipt, nil
}
