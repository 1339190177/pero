//go:build !(aix || darwin || dragonfly || freebsd || linux || netbsd || openbsd || solaris)

package tool

import "os"

func trustedSpillCleanupDirectory(string, os.FileInfo, bool) bool { return true }

func spillCleanupIdentity(path string, _ os.FileInfo) string { return path }
