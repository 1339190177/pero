package session

import (
	"errors"
	"os"
	"runtime"
)

// syncDirectory is retained by session persistence. Media owns an equivalent
// helper for its independently durable content-addressed store.
func syncDirectory(path string) error {
	// Windows cannot flush a directory opened with os.Open. Snapshot files
	// are already synced before their exclusive link is published; do not
	// claim the additional POSIX directory-entry power-loss guarantee there.
	if runtime.GOOS == "windows" {
		return nil
	}
	directory, err := os.Open(path)
	if err != nil {
		return err
	}
	return errors.Join(directory.Sync(), directory.Close())
}
