//go:build windows

package durable

// SyncDirectory does nothing on Windows. Windows has no directory fsync:
// FlushFileBuffers on a directory handle fails with ERROR_ACCESS_DENIED, and
// NTFS journals the metadata of a completed create, rename, link, or delete.
func SyncDirectory(string) error {
	return nil
}
