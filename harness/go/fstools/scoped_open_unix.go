//go:build !windows

package fstools

import (
	"os"
	"syscall"
)

func openScopedTextFile(root *os.Root, name string) (*os.File, error) {
	// A concurrent FIFO replacement must not block before descriptor checks.
	return root.OpenFile(name, os.O_RDONLY|syscall.O_NONBLOCK, 0)
}
