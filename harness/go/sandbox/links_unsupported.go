//go:build !linux && !darwin

package sandbox

import "os"

func singleLink(os.FileInfo) bool { return false }
