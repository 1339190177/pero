//go:build !windows

package fstools

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path"
)

func (w *Workspace) writeNew(ctx context.Context, name, text string) (ScopedTextReceipt, error) {
	var err error
	// Exclusive creation never follows or overwrites an attacker-installed target.
	// A failed write may leave a partial file; report unknown rather than retrying.
	receipt := ScopedTextReceipt{Path: name, AfterDigest: scopedTextDigest([]byte(text)), State: "not-applied"}
	if err = w.check(); err != nil {
		return receipt, err
	}
	parentName, baseName := path.Dir(name), path.Base(name)
	parent, err := w.root.OpenRoot(parentName)
	if err != nil {
		return receipt, err
	}
	defer parent.Close()
	parentInfo, err := parent.Stat(".")
	if err != nil {
		return receipt, err
	}
	file, err := parent.OpenFile(baseName, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return receipt, err
	}
	receipt.State = "unknown"
	_, writeErr := file.Write([]byte(text))
	syncErr := file.Sync()
	writtenInfo, statErr := file.Stat()
	closeErr := file.Close()
	if err = errors.Join(writeErr, syncErr, statErr, closeErr); err != nil {
		return receipt, err
	}
	if w.afterCreate != nil {
		w.afterCreate()
	}
	if err = w.check(); err != nil {
		return receipt, err
	}
	currentParent, err := w.root.Lstat(parentName)
	if err != nil || !currentParent.IsDir() || !os.SameFile(parentInfo, currentParent) {
		return receipt, errors.New("file parent changed; creation outcome requires reconciliation")
	}
	current, err := parent.Lstat(baseName)
	if err != nil || !sameFileInfo(writtenInfo, current) {
		return receipt, errors.New("created target changed; creation outcome requires reconciliation")
	}
	readback, err := openScopedTextFile(parent, baseName)
	if err != nil {
		return receipt, err
	}
	readInfo, readStatErr := readback.Stat()
	data, readErr := io.ReadAll(io.LimitReader(readback, MaxFileBytes+1))
	readCloseErr := readback.Close()
	if err = errors.Join(readStatErr, readErr, readCloseErr); err != nil {
		return receipt, err
	}
	if !sameFileInfo(writtenInfo, readInfo) || !bytes.Equal(data, []byte(text)) {
		return receipt, errors.New("created target readback uncertain; reconcile before retry")
	}
	dir, err := parent.Open(".")
	if err != nil {
		return receipt, err
	}
	syncErr = dir.Sync()
	closeErr = dir.Close()
	if err = errors.Join(syncErr, closeErr); err != nil {
		return receipt, err
	}
	if err = ctx.Err(); err != nil {
		return receipt, err
	}
	if err = w.check(); err != nil {
		return receipt, err
	}
	receipt.State = "applied"
	return receipt, nil
}
