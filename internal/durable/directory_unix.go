//go:build !windows

package durable

import (
	"errors"
	"os"
)

// SyncDirectory flushes a directory's entries, so a file created, renamed,
// linked, or removed inside it survives a crash.
func SyncDirectory(path string) error {
	directory, err := os.Open(path)
	if err != nil {
		return err
	}
	return errors.Join(directory.Sync(), directory.Close())
}
