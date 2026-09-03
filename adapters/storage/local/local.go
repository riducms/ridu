// Package local provides filesystem object storage for development and
// single-node deployments.
package local

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/riducms/ridu/storage"
)

type filesystemOperations struct {
	stat          func(string) (fs.FileInfo, error)
	mkdir         func(string, fs.FileMode) error
	createTemp    func(string, string) (*os.File, error)
	rename        func(string, string) error
	remove        func(string) error
	syncDirectory func(string) error
}

type Backend struct {
	root       string
	filesystem filesystemOperations
}

func defaultFilesystemOperations() filesystemOperations {
	return filesystemOperations{
		stat:       os.Stat,
		mkdir:      os.Mkdir,
		createTemp: os.CreateTemp,
		rename:     os.Rename,
		remove:     os.Remove,
		syncDirectory: func(path string) error {
			directory, err := os.Open(path)
			if err != nil {
				return err
			}
			return errors.Join(directory.Sync(), directory.Close())
		},
	}
}

func New(root string) (*Backend, error) {
	absolute, err := filepath.Abs(root)
	if err != nil {
		return nil, fmt.Errorf("resolve local storage root: %w", err)
	}
	filesystem := defaultFilesystemOperations()
	if err := mkdirAllDurable(filesystem, absolute, 0o750); err != nil {
		return nil, fmt.Errorf("create local storage root: %w", err)
	}
	return &Backend{root: absolute, filesystem: filesystem}, nil
}

// Ping verifies that the configured storage root accepts a durable probe write.
// The private probe is always removed before Ping returns.
func (backend *Backend) Ping(ctx context.Context) (result error) {
	if err := ctx.Err(); err != nil {
		return err
	}
	info, err := backend.filesystem.stat(backend.root)
	if err != nil {
		return fmt.Errorf("stat local storage root: %w", err)
	}
	if !info.IsDir() {
		return fmt.Errorf("local storage root is not a directory")
	}

	probe, err := backend.filesystem.createTemp(backend.root, ".ridu-readiness-*")
	if err != nil {
		return fmt.Errorf("create local storage readiness probe: %w", err)
	}
	probePath := probe.Name()
	closed := false
	removed := false
	defer func() {
		var cleanupErrors []error
		if !closed {
			if err := probe.Close(); err != nil {
				cleanupErrors = append(cleanupErrors, fmt.Errorf("close local storage readiness probe: %w", err))
			}
		}
		if !removed {
			if err := backend.filesystem.remove(probePath); err != nil && !errors.Is(err, fs.ErrNotExist) {
				cleanupErrors = append(cleanupErrors, fmt.Errorf("remove local storage readiness probe: %w", err))
			} else if err == nil {
				if err := backend.filesystem.syncDirectory(backend.root); err != nil {
					cleanupErrors = append(cleanupErrors, fmt.Errorf("sync local storage root after readiness cleanup: %w", err))
				}
			}
		}
		result = errors.Join(append([]error{result}, cleanupErrors...)...)
	}()

	if _, err := probe.Write([]byte{0}); err != nil {
		return fmt.Errorf("write local storage readiness probe: %w", err)
	}
	if err := probe.Sync(); err != nil {
		return fmt.Errorf("sync local storage readiness probe: %w", err)
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := probe.Close(); err != nil {
		return fmt.Errorf("close local storage readiness probe: %w", err)
	}
	closed = true
	if err := backend.filesystem.remove(probePath); err != nil {
		return fmt.Errorf("remove local storage readiness probe: %w", err)
	}
	removed = true
	if err := backend.filesystem.syncDirectory(backend.root); err != nil {
		return fmt.Errorf("sync local storage root after readiness probe: %w", err)
	}
	return nil
}

func (backend *Backend) path(key string) (string, error) {
	clean := filepath.Clean(filepath.FromSlash(key))
	if clean == "." || filepath.IsAbs(clean) || clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("invalid object key %q", key)
	}
	path := filepath.Join(backend.root, clean)
	relative, err := filepath.Rel(backend.root, path)
	if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("object key escapes storage root")
	}
	return path, nil
}

func (backend *Backend) Put(ctx context.Context, key string, source io.Reader, size int64, _ string) (result error) {
	if err := ctx.Err(); err != nil {
		return err
	}
	if size < 0 {
		return fmt.Errorf("local storage object size must not be negative")
	}
	if source == nil {
		return fmt.Errorf("local storage object source is required")
	}
	path, err := backend.path(key)
	if err != nil {
		return err
	}
	directory := filepath.Dir(path)
	if err := mkdirAllDurable(backend.filesystem, directory, 0o750); err != nil {
		return fmt.Errorf("create local storage object directory: %w", err)
	}
	temporary, err := backend.filesystem.createTemp(directory, ".ridu-upload-*")
	if err != nil {
		return fmt.Errorf("create local storage temporary object: %w", err)
	}
	temporaryPath := temporary.Name()
	committed := false
	closed := false
	defer func() {
		var cleanupErrors []error
		if !closed {
			if err := temporary.Close(); err != nil {
				cleanupErrors = append(cleanupErrors, fmt.Errorf("close local storage temporary object: %w", err))
			}
		}
		if !committed {
			if err := backend.filesystem.remove(temporaryPath); err != nil && !errors.Is(err, fs.ErrNotExist) {
				cleanupErrors = append(cleanupErrors, fmt.Errorf("remove local storage temporary object: %w", err))
			} else if err == nil {
				if err := backend.filesystem.syncDirectory(directory); err != nil {
					cleanupErrors = append(cleanupErrors, fmt.Errorf("sync local storage object directory after cleanup: %w", err))
				}
			}
		}
		result = errors.Join(append([]error{result}, cleanupErrors...)...)
	}()
	stopCancellation := interruptReaderOnCancellation(ctx, source)
	if err := copyDeclaredSize(ctx, temporary, source, size); err != nil {
		stopCancellation()
		return err
	}
	stopCancellation()
	if err := temporary.Sync(); err != nil {
		return fmt.Errorf("sync local storage temporary object: %w", err)
	}
	if err := temporary.Close(); err != nil {
		return fmt.Errorf("close local storage temporary object: %w", err)
	}
	closed = true
	if err := backend.filesystem.rename(temporaryPath, path); err != nil {
		return fmt.Errorf("replace local storage object: %w", err)
	}
	committed = true
	if err := backend.filesystem.syncDirectory(directory); err != nil {
		return fmt.Errorf("sync local storage object directory after replace: %w", err)
	}
	return nil
}

func (backend *Backend) Open(_ context.Context, key string) (io.ReadCloser, storage.Object, error) {
	path, err := backend.path(key)
	if err != nil {
		return nil, storage.Object{}, err
	}
	file, err := os.Open(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, storage.Object{}, storage.ErrNotFound
	}
	if err != nil {
		return nil, storage.Object{}, err
	}
	info, err := file.Stat()
	if err != nil {
		_ = file.Close()
		return nil, storage.Object{}, err
	}
	header := make([]byte, 512)
	read, readError := file.Read(header)
	if readError != nil && !errors.Is(readError, io.EOF) {
		_ = file.Close()
		return nil, storage.Object{}, readError
	}
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		_ = file.Close()
		return nil, storage.Object{}, err
	}
	return file, storage.Object{Key: key, Size: info.Size(), ContentType: http.DetectContentType(header[:read]), ModifiedAt: info.ModTime()}, nil
}

func (backend *Backend) Delete(ctx context.Context, key string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	path, err := backend.path(key)
	if err != nil {
		return err
	}
	err = backend.filesystem.remove(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	syncError := backend.filesystem.syncDirectory(filepath.Dir(path))
	if syncError != nil {
		syncError = fmt.Errorf("sync local storage object directory after delete: %w", syncError)
	}
	return errors.Join(ctx.Err(), syncError)
}

func (backend *Backend) List(ctx context.Context, list storage.ListRequest) (storage.ListPage, error) {
	if list.Limit < 1 || list.Limit > storage.MaxListPageSize {
		return storage.ListPage{}, fmt.Errorf("local storage list limit must be between 1 and %d", storage.MaxListPageSize)
	}
	if list.Cursor != "" && !strings.HasPrefix(list.Cursor, list.Prefix) {
		return storage.ListPage{}, fmt.Errorf("local storage list cursor is outside the requested prefix")
	}
	objects := make([]storage.Object, 0, list.Limit+1)
	err := filepath.WalkDir(backend.root, func(path string, entry fs.DirEntry, walkError error) error {
		if walkError != nil {
			return walkError
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		key, err := filepath.Rel(backend.root, path)
		if err != nil {
			return err
		}
		key = filepath.ToSlash(key)
		if !strings.HasPrefix(key, list.Prefix) || key <= list.Cursor {
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		objects = append(objects, storage.Object{Key: key, Size: info.Size(), ModifiedAt: info.ModTime()})
		if len(objects) > list.Limit {
			return fs.SkipAll
		}
		return nil
	})
	if err != nil {
		return storage.ListPage{}, err
	}
	sort.Slice(objects, func(left, right int) bool { return objects[left].Key < objects[right].Key })
	page := storage.ListPage{Objects: objects}
	if len(page.Objects) > list.Limit {
		page.Objects = page.Objects[:list.Limit]
		page.NextCursor = page.Objects[len(page.Objects)-1].Key
	}
	return page, nil
}

func mkdirAllDurable(filesystem filesystemOperations, directory string, mode fs.FileMode) error {
	directory = filepath.Clean(directory)
	var missing []string
	for current := directory; ; current = filepath.Dir(current) {
		info, err := filesystem.stat(current)
		if err == nil {
			if !info.IsDir() {
				return fmt.Errorf("%s exists and is not a directory", current)
			}
			break
		}
		if !errors.Is(err, fs.ErrNotExist) {
			return err
		}
		missing = append(missing, current)
		parent := filepath.Dir(current)
		if parent == current {
			return fmt.Errorf("no existing parent directory for %s", directory)
		}
	}
	for index := len(missing) - 1; index >= 0; index-- {
		created := missing[index]
		if err := filesystem.mkdir(created, mode); err != nil {
			if !errors.Is(err, fs.ErrExist) {
				return err
			}
			info, statError := filesystem.stat(created)
			if statError != nil {
				return statError
			}
			if !info.IsDir() {
				return fmt.Errorf("%s exists and is not a directory", created)
			}
		}
		if err := filesystem.syncDirectory(filepath.Dir(created)); err != nil {
			return fmt.Errorf("sync parent after creating %s: %w", created, err)
		}
	}
	return nil
}

func copyDeclaredSize(ctx context.Context, destination io.Writer, source io.Reader, declared int64) error {
	buffer := make([]byte, 32*1024)
	written := int64(0)
	emptyReads := 0
	for written < declared {
		if err := ctx.Err(); err != nil {
			return err
		}
		remaining := declared - written
		readBuffer := buffer
		if remaining < int64(len(readBuffer)) {
			readBuffer = readBuffer[:remaining]
		}
		read, readError := source.Read(readBuffer)
		if read < 0 || read > len(readBuffer) {
			return fmt.Errorf("read local storage object: invalid byte count %d", read)
		}
		if read > 0 {
			emptyReads = 0
			for offset := 0; offset < read; {
				count, err := destination.Write(readBuffer[offset:read])
				offset += count
				if err != nil {
					return fmt.Errorf("write local storage object: %w", err)
				}
				if count == 0 {
					return fmt.Errorf("write local storage object: %w", io.ErrShortWrite)
				}
			}
			written += int64(read)
		} else {
			emptyReads++
			if emptyReads >= 100 {
				return fmt.Errorf("read local storage object: %w", io.ErrNoProgress)
			}
		}
		if readError != nil {
			if errors.Is(readError, io.EOF) {
				if written == declared {
					return nil
				}
				return fmt.Errorf("local storage object size mismatch: declared %d bytes, received %d", declared, written)
			}
			if err := ctx.Err(); err != nil {
				return err
			}
			return fmt.Errorf("read local storage object: %w", readError)
		}
	}

	emptyReads = 0
	var extra [1]byte
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		read, readError := source.Read(extra[:])
		if read > 0 {
			return fmt.Errorf("local storage object size mismatch: declared %d bytes, received more", declared)
		}
		if readError != nil {
			if errors.Is(readError, io.EOF) {
				return nil
			}
			if err := ctx.Err(); err != nil {
				return err
			}
			return fmt.Errorf("read local storage object: %w", readError)
		}
		emptyReads++
		if emptyReads >= 100 {
			return fmt.Errorf("read local storage object: %w", io.ErrNoProgress)
		}
	}
}

func interruptReaderOnCancellation(ctx context.Context, source io.Reader) func() {
	closer, ok := source.(io.Closer)
	if !ok || ctx.Done() == nil {
		return func() {}
	}
	stop := make(chan struct{})
	done := make(chan struct{})
	go func() {
		defer close(done)
		select {
		case <-ctx.Done():
			_ = closer.Close()
		case <-stop:
		}
	}()
	return func() {
		close(stop)
		<-done
	}
}

var _ storage.Backend = (*Backend)(nil)
var _ storage.HealthBackend = (*Backend)(nil)
