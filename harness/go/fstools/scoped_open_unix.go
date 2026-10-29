//go:build !windows

package fstools

import (
	"os"
	"path/filepath"
	"syscall"
)

func openScopedTextFile(root *os.Root, name string) (*os.File, error) {
	// A concurrent FIFO replacement must not block before descriptor checks.
	return root.OpenFile(name, os.O_RDONLY|syscall.O_NONBLOCK, 0)
}

const nativeFileWrites = true

func canonicalWorkspacePath(name string) (string, error) { return filepath.EvalSymlinks(name) }
func scopedPathInfo(name string) (os.FileInfo, error)    { return os.Lstat(name) }
func scopedLink(info os.FileInfo) bool                   { return info.Mode()&os.ModeSymlink != 0 }
func nativeRelativePath(name string) bool                { return true }

func scopedEntryLink(entry os.DirEntry) (bool, error) { return entry.Type()&os.ModeSymlink != 0, nil }
