//go:build rag

// Copyright (c) 2025 Jeremy Hahn
// Copyright (c) 2025 Automate The Things, LLC
//
// This file is part of go-quicbaas.
//
// go-quicbaas is dual-licensed:
//
// 1. GNU Affero General Public License v3.0 (AGPL-3.0)
//    See LICENSE file or visit https://www.gnu.org/licenses/agpl-3.0.html
//
// 2. Commercial License
//    Contact licensing@automatethethings.com for commercial licensing options.

package watcher

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
)

// Watcher watches file system changes and emits events.
type Watcher interface {
	// Start begins watching for file system events.
	Start(ctx context.Context) error

	// Stop stops watching for events.
	Stop() error

	// Events returns a read-only channel of file system events.
	Events() <-chan Event

	// AddPath adds a new path to watch.
	AddPath(path string) error

	// RemovePath stops watching a path.
	RemovePath(path string) error
}

// fsWatcher implements the Watcher interface using fsnotify.
type fsWatcher struct {
	config    Config
	watcher   *fsnotify.Watcher
	events    chan Event
	errors    chan error
	gitignore *gitIgnoreMatcher
	debouncer *debouncer

	mu      sync.RWMutex
	paths   map[string]bool
	started bool
	stopCh  chan struct{}
	wg      sync.WaitGroup

	// stopMu serializes stops; stopErr records a failed close for every
	// later Stop to return. closeNotifier closes the fsnotify watcher.
	stopMu        sync.Mutex
	stopErr       error
	closeNotifier func() error

	// Test seams (nil in production; a nil channel case never fires and a nil
	// func is never called, so these have zero effect on shipped behavior).
	// They let tests drive the debounce/batch cycle deterministically instead of
	// betting on wall-clock timing: onDebounce fires after each event is added to
	// the debouncer, and a send on manualFlush forces a batch flush. Set before
	// Start so the processBatches/processEvents goroutines observe them.
	manualFlush chan struct{}
	onDebounce  func()
}

// New creates a new file system watcher with the given configuration.
func New(config Config) (Watcher, error) {
	if err := config.Validate(); err != nil {
		return nil, &WatcherCauseError{Message: "invalid config", Cause: err}
	}

	// Build the matcher first: it can fail, and nothing needs releasing yet.
	gitignore, err := newGitIgnoreMatcher(config.Exclude)
	if err != nil {
		return nil, &WatcherCauseError{Message: "failed to create gitignore matcher", Cause: err}
	}

	fw, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, &WatcherCauseError{Message: "failed to create fsnotify watcher", Cause: err}
	}

	w := &fsWatcher{
		config:        config,
		watcher:       fw,
		closeNotifier: fw.Close,
		events:        make(chan Event, config.BufferSize),
		errors:        make(chan error, 10),
		gitignore:     gitignore,
		debouncer:     newDebouncer(config.DebounceDelay),
		paths:         make(map[string]bool),
		stopCh:        make(chan struct{}),
	}

	return w, nil
}

// Start begins watching for file system events.
func (w *fsWatcher) Start(ctx context.Context) error {
	w.mu.Lock()
	if w.started {
		w.mu.Unlock()
		return ErrAlreadyStarted
	}
	w.started = true
	w.mu.Unlock()

	// Add initial paths
	for _, path := range w.config.Paths {
		if err := w.addPath(path); err != nil {
			return &WatcherCauseError{Message: "failed to add path " + path, Cause: err}
		}
	}

	// Start event processing goroutines
	w.wg.Add(2)
	go w.processEvents(ctx)
	go w.processBatches(ctx)

	// Wait for context cancellation
	go func() {
		<-ctx.Done()
		w.shutdown()
	}()

	return nil
}

// Stop stops watching for events.
func (w *fsWatcher) Stop() error {
	w.shutdown()

	w.stopMu.Lock()
	defer w.stopMu.Unlock()
	return w.stopErr
}

// shutdown stops the watcher if it is running. A failure to close the
// fsnotify watcher is kept in stopErr, so a stop triggered by Start's context
// still reaches the owner through its own Stop call.
func (w *fsWatcher) shutdown() {
	w.stopMu.Lock()
	defer w.stopMu.Unlock()

	w.mu.Lock()
	if !w.started {
		w.mu.Unlock()
		return
	}
	w.started = false
	w.mu.Unlock()

	// Signal stop
	close(w.stopCh)

	// Close fsnotify watcher
	if err := w.closeNotifier(); err != nil {
		w.stopErr = &WatcherCauseError{Message: "failed to close watcher", Cause: err}
		return
	}

	// Wait for goroutines
	w.wg.Wait()

	// Close channels
	close(w.events)
	close(w.errors)
}

// Events returns a read-only channel of file system events.
func (w *fsWatcher) Events() <-chan Event {
	return w.events
}

// AddPath adds a new path to watch.
func (w *fsWatcher) AddPath(path string) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.addPath(path)
}

// RemovePath stops watching a path.
func (w *fsWatcher) RemovePath(path string) error {
	w.mu.Lock()
	defer w.mu.Unlock()

	if !w.paths[path] {
		return ErrPathNotWatched
	}

	if err := w.watcher.Remove(path); err != nil {
		return &WatcherCauseError{Message: "failed to remove path", Cause: err}
	}

	delete(w.paths, path)
	return nil
}

// addPath adds a path to watch (must be called with lock held).
func (w *fsWatcher) addPath(path string) error {
	// Check if path exists
	info, err := os.Stat(path)
	if err != nil {
		return &WatcherCauseError{Message: "failed to stat path", Cause: err}
	}

	// Add the path
	if err := w.watcher.Add(path); err != nil {
		return &WatcherCauseError{Message: "failed to watch path", Cause: err}
	}

	w.paths[path] = true

	// If it's a directory and recursive watching is enabled, add subdirectories
	if info.IsDir() && w.config.Recursive {
		if err := w.addRecursive(path, 0); err != nil {
			return err
		}
	}

	return nil
}

// addRecursive recursively adds subdirectories to watch.
func (w *fsWatcher) addRecursive(root string, depth int) error {
	// Check max depth
	if w.config.MaxDepth > 0 && depth >= w.config.MaxDepth {
		return nil
	}

	entries, err := os.ReadDir(root)
	if err != nil {
		return &WatcherCauseError{Message: "failed to read directory", Cause: err}
	}

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}

		name := entry.Name()
		path := filepath.Join(root, name)

		// Skip hidden directories if configured
		if w.config.IgnoreHidden && strings.HasPrefix(name, ".") {
			continue
		}

		// Skip excluded paths
		if w.shouldExclude(path) {
			continue
		}

		// Handle symlinks
		if entry.Type()&os.ModeSymlink != 0 {
			if !w.config.FollowSymlinks {
				continue
			}

			// Resolve symlink
			realPath, err := filepath.EvalSymlinks(path)
			if err != nil {
				continue
			}

			// Check if it's a directory
			info, err := os.Stat(realPath)
			if err != nil || !info.IsDir() {
				continue
			}

			path = realPath
		}

		// Add directory
		if err := w.watcher.Add(path); err != nil {
			// Continue on error for individual directories
			continue
		}

		w.paths[path] = true

		// Recursively add subdirectories
		if err := w.addRecursive(path, depth+1); err != nil {
			// Continue on error
			continue
		}
	}

	return nil
}

// shouldExclude checks if a path should be excluded.
func (w *fsWatcher) shouldExclude(path string) bool {
	return w.gitignore.Match(path)
}

// processEvents processes fsnotify events and sends them to the debouncer.
func (w *fsWatcher) processEvents(ctx context.Context) {
	defer w.wg.Done()

	for {
		select {
		case <-w.stopCh:
			return

		case <-ctx.Done():
			return

		case event, ok := <-w.watcher.Events:
			if !ok {
				return
			}

			// Skip excluded paths
			if w.shouldExclude(event.Name) {
				continue
			}

			// Skip hidden files if configured
			if w.config.IgnoreHidden {
				base := filepath.Base(event.Name)
				if strings.HasPrefix(base, ".") {
					continue
				}
			}

			// Convert fsnotify event to our event type
			e := w.convertEvent(event)
			if e == nil {
				continue
			}

			// Send to debouncer
			w.debouncer.Add(*e)
			if w.onDebounce != nil {
				w.onDebounce()
			}

		case err, ok := <-w.watcher.Errors:
			if !ok {
				return
			}

			// Send error to error channel (non-blocking)
			select {
			case w.errors <- err:
			default:
			}
		}
	}
}

// processBatches processes debounced events and sends them to the output channel.
func (w *fsWatcher) processBatches(ctx context.Context) {
	defer w.wg.Done()

	ticker := time.NewTicker(w.config.BatchInterval)
	defer ticker.Stop()

	for {
		select {
		case <-w.stopCh:
			// Flush remaining events
			w.flushEvents()
			return

		case <-ctx.Done():
			// Flush remaining events
			w.flushEvents()
			return

		case <-w.manualFlush:
			// Test-driven flush (nil channel in production: never fires).
			w.flushReady()

		case <-ticker.C:
			w.flushReady()
		}
	}
}

// flushEvents sends every buffered event, ready or not, to the output channel.
// It drains the debouncer at shutdown.
func (w *fsWatcher) flushEvents() {
	w.sendEvents(w.debouncer.Flush())
}

// flushReady sends only the events whose debounce window has passed.
func (w *fsWatcher) flushReady() {
	w.sendEvents(w.debouncer.FlushReady())
}

// sendEvents delivers events to the events channel, dropping any that do not
// fit.
func (w *fsWatcher) sendEvents(events []Event) {
	for _, event := range events {
		select {
		case w.events <- event:
		case <-w.stopCh:
			return
		default:
			// Drop event if channel is full
		}
	}
}

// convertEvent converts a fsnotify event to our event type.
func (w *fsWatcher) convertEvent(fsEvent fsnotify.Event) *Event {
	var eventType EventType

	switch {
	case fsEvent.Op&fsnotify.Create == fsnotify.Create:
		eventType = EventCreate
	case fsEvent.Op&fsnotify.Write == fsnotify.Write:
		eventType = EventModify
	case fsEvent.Op&fsnotify.Remove == fsnotify.Remove:
		eventType = EventDelete
	case fsEvent.Op&fsnotify.Rename == fsnotify.Rename:
		eventType = EventDelete // Treat rename as delete for simplicity
	case fsEvent.Op&fsnotify.Chmod == fsnotify.Chmod:
		// Ignore chmod events
		return nil
	default:
		return nil
	}

	// Get file size for non-delete events
	var size int64
	if eventType != EventDelete {
		if info, err := os.Stat(fsEvent.Name); err == nil {
			size = info.Size()
		}
	}

	return &Event{
		Type:      eventType,
		Path:      fsEvent.Name,
		Timestamp: time.Now(),
		Size:      size,
	}
}
