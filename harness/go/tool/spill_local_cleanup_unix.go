//go:build aix || darwin || dragonfly || freebsd || linux || netbsd || openbsd || solaris

package tool

import (
	"fmt"
	"os"
	"path/filepath"
	"syscall"
)

func trustedSpillCleanupDirectory(path string, info os.FileInfo, checkAncestors bool) bool {
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || int(stat.Uid) != os.Geteuid() || info.Mode().Perm()&0o022 != 0 {
		return false
	}
	if !checkAncestors {
		return true
	}
	childInfo := info
	child := path
	for {
		parent := filepath.Dir(child)
		if parent == child {
			return true
		}
		parentInfo, err := os.Lstat(parent)
		if err != nil || !parentInfo.IsDir() {
			return false
		}
		if parentInfo.Mode().Perm()&0o022 != 0 {
			sticky := parentInfo.Mode()&os.ModeSticky != 0
			childStat, childOK := childInfo.Sys().(*syscall.Stat_t)
			if !sticky || !childOK || int(childStat.Uid) != os.Geteuid() {
				return false
			}
		}
		child, childInfo = parent, parentInfo
	}
}

func spillCleanupIdentity(path string, info os.FileInfo) string {
	if stat, ok := info.Sys().(*syscall.Stat_t); ok {
		return fmt.Sprintf("%d:%d", stat.Dev, stat.Ino)
	}
	return path
}
