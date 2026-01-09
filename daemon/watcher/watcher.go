package watcher

import (
	"context"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/fsnotify/fsnotify"
)

// EventType defines the type of file system change.
type EventType string

const (
	EventCreate EventType = "create"
	EventModify EventType = "modify"
	EventDelete EventType = "delete"
	EventRename EventType = "rename"
)

// Event represents a normalized file system event.
type Event struct {
	Path string
	Type EventType
}

// Watcher monitors a directory for file system changes.
type Watcher struct {
	root     string
	fsw      *fsnotify.Watcher
	events   chan Event
	debounce time.Duration
}

// NewWatcher creates a new Watcher instance.
func NewWatcher(root string, debounce time.Duration) (*Watcher, error) {
	fsw, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, err
	}

	return &Watcher{
		root:     root,
		fsw:      fsw,
		events:   make(chan Event, 100),
		debounce: debounce,
	}, nil
}

// Start begins watching the root directory recursively.
func (w *Watcher) Start(ctx context.Context) error {
	// Add root and subdirectories recursively
	if err := w.addRecursive(w.root); err != nil {
		return err
	}

	go w.watchLoop(ctx)
	return nil
}

// Events returns the channel for normalized events.
func (w *Watcher) Events() <-chan Event {
	return w.events
}

// watchLoop processes raw fsnotify events with debouncing.
func (w *Watcher) watchLoop(ctx context.Context) {
	defer w.fsw.Close()

	// path -> timer for debouncing
	pending := make(map[string]*time.Timer)

	for {
		select {
		case <-ctx.Done():
			return
		case err, ok := <-w.fsw.Errors:
			if !ok {
				return
			}
			log.Printf("Watcher error: %v", err)
		case fsEvent, ok := <-w.fsw.Events:
			if !ok {
				return
			}

			// Normalized path relative to root
			relPath, err := filepath.Rel(w.root, fsEvent.Name)
			if err != nil {
				continue
			}

			// Skip .psync directories
			if shouldIgnore(relPath) {
				continue
			}

			// Handle directory creation (need to watch new subdirs)
			if fsEvent.Has(fsnotify.Create) {
				if info, err := os.Stat(fsEvent.Name); err == nil && info.IsDir() {
					w.addRecursive(fsEvent.Name)
				}
			}

			// Debounce logic
			if timer, ok := pending[relPath]; ok {
				timer.Stop()
			}

			timer := time.AfterFunc(w.debounce, func() {
				var eventType EventType
				switch {
				case fsEvent.Has(fsnotify.Create):
					eventType = EventCreate
				case fsEvent.Has(fsnotify.Write):
					eventType = EventModify
				case fsEvent.Has(fsnotify.Remove):
					eventType = EventDelete
				case fsEvent.Has(fsnotify.Rename):
					eventType = EventRename
				default:
					return
				}

				w.events <- Event{
					Path: relPath,
					Type: eventType,
				}
			})
			pending[relPath] = timer
		}
	}
}

// addRecursive recursively adds directories to the watcher.
func (w *Watcher) addRecursive(root string) error {
	return filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			// Skip .psync directories
			if shouldIgnore(info.Name()) {
				return filepath.SkipDir
			}
			return w.fsw.Add(path)
		}
		return nil
	})
}

// shouldIgnore returns true if the path should be ignored (starts with .psync or ends with .tmp).
func shouldIgnore(path string) bool {
	// Check each path component
	for part := range strings.SplitSeq(path, string(filepath.Separator)) {
		if strings.HasPrefix(part, ".psync") {
			return true
		}
	}
	// Ignore temporary files
	if strings.HasSuffix(path, ".tmp") {
		return true
	}
	return false
}
