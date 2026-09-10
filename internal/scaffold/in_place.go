package scaffold

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
)

// Claim complete top-level scaffold directories so neither existing nested
// files nor symlinks can be overwritten by scaffolding or initial generation.
func installInPlace(staging, target string) (resultError error) {
	root, err := os.OpenRoot(target)
	if err != nil {
		return fmt.Errorf("open current directory: %w", err)
	}
	defer root.Close()
	entries, err := os.ReadDir(staging)
	if err != nil {
		return err
	}
	// These are written by dependency setup and generation after scaffolding.
	paths := []string{"go.sum", "generated", "migrations", ".ridu"}
	for _, entry := range entries {
		paths = append(paths, entry.Name())
	}
	var conflicts []string
	for _, path := range paths {
		if _, err := root.Lstat(path); err == nil {
			conflicts = append(conflicts, path)
		} else if !os.IsNotExist(err) {
			return fmt.Errorf("inspect %s: %w", path, err)
		}
	}
	if len(conflicts) != 0 {
		return fmt.Errorf("current directory contains conflicting paths: %v; move them aside or choose a new directory; no project files were added", conflicts)
	}

	var created []string
	defer func() {
		if resultError == nil {
			return
		}
		// Remove only entries we created; never recursively remove a directory
		// that another process may have added files to in the meantime.
		for index := len(created) - 1; index >= 0; index-- {
			if err := root.Remove(created[index]); err != nil && !os.IsNotExist(err) {
				resultError = errors.Join(resultError, fmt.Errorf("remove incomplete scaffold path %s: %w", created[index], err))
			}
		}
	}()
	return filepath.WalkDir(staging, func(path string, entry fs.DirEntry, walkError error) error {
		if walkError != nil || path == staging {
			return walkError
		}
		relative, err := filepath.Rel(staging, path)
		if err != nil {
			return err
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if entry.IsDir() {
			if err := root.Mkdir(relative, info.Mode().Perm()); err != nil {
				return fmt.Errorf("create %s: %w", relative, err)
			}
			created = append(created, relative)
			return nil
		}
		source, err := os.Open(path)
		if err != nil {
			return err
		}
		defer source.Close()
		destination, err := root.OpenFile(relative, os.O_WRONLY|os.O_CREATE|os.O_EXCL, info.Mode().Perm())
		if err != nil {
			return fmt.Errorf("create %s: %w", relative, err)
		}
		created = append(created, relative)
		_, copyError := io.Copy(destination, source)
		return errors.Join(copyError, destination.Close())
	})
}
