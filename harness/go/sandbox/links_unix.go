//go:build linux || darwin

package sandbox

import (
	"os"
	"syscall"
)

func singleLink(info os.FileInfo) bool {
	s, ok := info.Sys().(*syscall.Stat_t)
	return ok && s.Nlink == 1
}
