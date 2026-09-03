package cli

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/fsnotify/fsnotify"
)

const goSourceDebounce = 120 * time.Millisecond

type goSourceWatcher struct {
	watcher   *fsnotify.Watcher
	changes   chan uint64
	errors    chan error
	done      chan struct{}
	closeOnce sync.Once
	revision  atomic.Uint64
}

func newGoSourceWatcher(root string) (*goSourceWatcher, error) {
	native, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, err
	}
	watcher := &goSourceWatcher{
		watcher: native,
		changes: make(chan uint64, 1),
		errors:  make(chan error, 1),
		done:    make(chan struct{}),
	}
	if _, err := watcher.addTree(root); err != nil {
		_ = native.Close()
		return nil, err
	}
	go watcher.run()
	return watcher, nil
}

func (watcher *goSourceWatcher) Changes() <-chan uint64 { return watcher.changes }
func (watcher *goSourceWatcher) Errors() <-chan error   { return watcher.errors }
func (watcher *goSourceWatcher) Revision() uint64       { return watcher.revision.Load() }

func (watcher *goSourceWatcher) Close() error {
	var err error
	watcher.closeOnce.Do(func() {
		close(watcher.done)
		err = watcher.watcher.Close()
	})
	return err
}

func (watcher *goSourceWatcher) addTree(root string) (bool, error) {
	containsGo := false
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkError error) error {
		if walkError != nil {
			return walkError
		}
		if entry.IsDir() {
			if path != root && shouldSkipDirectory(entry.Name()) {
				return filepath.SkipDir
			}
			if err := watcher.watcher.Add(path); err != nil {
				return fmt.Errorf("watch %s: %w", path, err)
			}
			return nil
		}
		if strings.HasSuffix(entry.Name(), ".go") {
			containsGo = true
		}
		return nil
	})
	return containsGo, err
}

func (watcher *goSourceWatcher) run() {
	var timer *time.Timer
	var timerChannel <-chan time.Time
	schedule := func() {
		if timer == nil {
			timer = time.NewTimer(goSourceDebounce)
		} else {
			if !timer.Stop() {
				select {
				case <-timer.C:
				default:
				}
			}
			timer.Reset(goSourceDebounce)
		}
		timerChannel = timer.C
	}
	defer func() {
		if timer != nil {
			timer.Stop()
		}
	}()

	for {
		select {
		case <-watcher.done:
			return
		case event, open := <-watcher.watcher.Events:
			if !open {
				return
			}
			if event.Op&fsnotify.Create != 0 {
				if info, err := os.Stat(event.Name); err == nil && info.IsDir() {
					if shouldSkipDirectory(filepath.Base(event.Name)) {
						continue
					}
					containsGo, addError := watcher.addTree(event.Name)
					if addError != nil {
						watcher.report(addError)
					}
					if containsGo {
						watcher.revision.Add(1)
						schedule()
					}
					continue
				}
			}
			if strings.HasSuffix(event.Name, ".go") && event.Op&(fsnotify.Create|fsnotify.Write|fsnotify.Remove|fsnotify.Rename) != 0 {
				watcher.revision.Add(1)
				schedule()
			}
		case err, open := <-watcher.watcher.Errors:
			if !open {
				return
			}
			watcher.report(err)
		case <-timerChannel:
			timerChannel = nil
			select {
			case watcher.changes <- watcher.Revision():
			default:
			}
		}
	}
}

func (watcher *goSourceWatcher) report(err error) {
	select {
	case watcher.errors <- err:
	default:
	}
}
