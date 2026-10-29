package sandbox

import (
	"errors"
	"io/fs"
	"path/filepath"
	"time"
)

// Workspace mounts must not export ambient host IPC or aliases to outside file
// inodes. The operator owns parent directories and excludes concurrent host
// mutation during admission/launch; pathname mounting alone cannot seal that race.
func validateWorkspace(root string) error {
	deadline := time.Now().Add(5 * time.Second)
	count := 0
	return filepath.WalkDir(root, func(_ string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		count++
		if count > 500000 || time.Now().After(deadline) {
			return errors.New("sandbox: workspace inspection exceeded budget")
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		mode := info.Mode()
		if !strictRuntimeFileType(mode) {
			return errors.New("sandbox: workspace cannot contain host sockets, FIFOs or devices")
		}
		if mode.IsRegular() && !singleLink(info) {
			return errors.New("sandbox: workspace regular files must have exactly one hard link")
		}
		return nil
	})
}
