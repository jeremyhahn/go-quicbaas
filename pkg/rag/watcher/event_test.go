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
	"testing"
	"time"

	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/fsnotify/fsnotify"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestEventType_String(t *testing.T) {
	tests := []struct {
		name     string
		et       EventType
		expected string
	}{
		{"create", EventCreate, "create"},
		{"modify", EventModify, "modify"},
		{"delete", EventDelete, "delete"},
		{"rename", EventRename, "rename"},
		{"unknown", EventType(999), "unknown"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, tt.et.String())
		})
	}
}

func TestEvent_IsModification(t *testing.T) {
	tests := []struct {
		name     string
		event    Event
		expected bool
	}{
		{
			name:     "create is modification",
			event:    Event{Type: EventCreate},
			expected: true,
		},
		{
			name:     "modify is modification",
			event:    Event{Type: EventModify},
			expected: true,
		},
		{
			name:     "rename is modification",
			event:    Event{Type: EventRename},
			expected: true,
		},
		{
			name:     "delete is not modification",
			event:    Event{Type: EventDelete},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, tt.event.IsModification())
		})
	}
}

func TestEvent_IsDeletion(t *testing.T) {
	tests := []struct {
		name     string
		event    Event
		expected bool
	}{
		{
			name:     "delete is deletion",
			event:    Event{Type: EventDelete},
			expected: true,
		},
		{
			name:     "create is not deletion",
			event:    Event{Type: EventCreate},
			expected: false,
		},
		{
			name:     "modify is not deletion",
			event:    Event{Type: EventModify},
			expected: false,
		},
		{
			name:     "rename is not deletion",
			event:    Event{Type: EventRename},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, tt.event.IsDeletion())
		})
	}
}

// TestAddRecursive_Exclusions checks which entries a recursive walk watches
// under exclude patterns, IgnoreHidden, and plain files.
func TestAddRecursive_Exclusions(t *testing.T) {
	tests := []struct {
		name         string
		dirs         []string
		files        []string
		exclude      []string
		ignoreHidden bool
		watched      []string
		notWatched   []string
	}{
		{
			name:       "excluded directory and its children",
			dirs:       []string{"node_modules/nested", "src"},
			exclude:    []string{"node_modules", "**/node_modules/**"},
			watched:    []string{"src"},
			notWatched: []string{"node_modules", "node_modules/nested"},
		},
		{
			name:       "nested excluded directory",
			dirs:       []string{"a/b/c/vendor", "a/b/c/lib"},
			exclude:    []string{"vendor", "**/vendor/**"},
			watched:    []string{"a/b/c", "a/b/c/lib"},
			notWatched: []string{"a/b/c/vendor"},
		},
		{
			name:         "several patterns with hidden ignored",
			dirs:         []string{"node_modules", "vendor", ".cache", "build", "dist", "src"},
			exclude:      []string{"node_modules", "vendor", "build", "dist"},
			ignoreHidden: true,
			watched:      []string{"src"},
			notWatched:   []string{"node_modules", "vendor", ".cache", "build", "dist"},
		},
		{
			name:         "hidden directory only",
			dirs:         []string{".hidden", "normal"},
			ignoreHidden: true,
			watched:      []string{"normal"},
			notWatched:   []string{".hidden"},
		},
		{
			name:       "plain files are skipped",
			dirs:       []string{"subdir"},
			files:      []string{"file.txt"},
			watched:    []string{"subdir"},
			notWatched: []string{"file.txt"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmpDir := t.TempDir()
			for _, d := range tt.dirs {
				require.NoError(t, os.MkdirAll(filepath.Join(tmpDir, d), 0755))
			}
			for _, f := range tt.files {
				require.NoError(t, os.WriteFile(filepath.Join(tmpDir, f), []byte("test"), 0644))
			}

			w, err := New(Config{
				Paths:         []string{tmpDir},
				Recursive:     true,
				Exclude:       tt.exclude,
				IgnoreHidden:  tt.ignoreHidden,
				DebounceDelay: 50 * time.Millisecond,
				BatchInterval: 100 * time.Millisecond,
				BufferSize:    100,
			})
			require.NoError(t, err)
			defer w.Stop()

			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			require.NoError(t, w.Start(ctx))

			fw := w.(*fsWatcher)
			fw.mu.RLock()
			defer fw.mu.RUnlock()
			for _, p := range tt.watched {
				assert.Contains(t, fw.paths, filepath.Join(tmpDir, p), "%s should be watched", p)
			}
			for _, p := range tt.notWatched {
				assert.NotContains(t, fw.paths, filepath.Join(tmpDir, p), "%s should not be watched", p)
			}
		})
	}
}

// TestAddRecursive_Symlinks checks that a recursive walk never watches a
// symlink's own path, whatever the link points at, and that Start succeeds
// for links to directories, to files and to nothing. addRecursive skips every
// non-directory entry before its symlink branch, and a symlink's DirEntry is
// never a directory, so a followed link to a directory is not watched either.
func TestAddRecursive_Symlinks(t *testing.T) {
	tests := []struct {
		name           string
		target         string // "dir", "file" or "missing"
		followSymlinks bool
	}{
		{name: "to directory, followed", target: "dir", followSymlinks: true},
		{name: "to directory, not followed", target: "dir", followSymlinks: false},
		{name: "to file, followed", target: "file", followSymlinks: true},
		{name: "broken, followed", target: "missing", followSymlinks: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmpDir := t.TempDir()
			realDir := filepath.Join(tmpDir, "real")
			require.NoError(t, os.Mkdir(realDir, 0755))
			realFile := filepath.Join(tmpDir, "file.txt")
			require.NoError(t, os.WriteFile(realFile, []byte("test"), 0644))
			subDir := filepath.Join(tmpDir, "subdir")
			require.NoError(t, os.Mkdir(subDir, 0755))

			targets := map[string]string{
				"dir":     realDir,
				"file":    realFile,
				"missing": filepath.Join(tmpDir, "nonexistent"),
			}
			linkPath := filepath.Join(subDir, "link")
			require.NoError(t, os.Symlink(targets[tt.target], linkPath))

			w, err := New(Config{
				Paths:          []string{tmpDir},
				Recursive:      true,
				FollowSymlinks: tt.followSymlinks,
				DebounceDelay:  50 * time.Millisecond,
				BatchInterval:  100 * time.Millisecond,
				BufferSize:     100,
			})
			require.NoError(t, err)
			defer w.Stop()

			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			require.NoError(t, w.Start(ctx))

			fw := w.(*fsWatcher)
			fw.mu.RLock()
			_, hasLink := fw.paths[linkPath]
			_, hasReal := fw.paths[realDir]
			_, hasSub := fw.paths[subDir]
			fw.mu.RUnlock()
			assert.False(t, hasLink, "symlink path itself must not be watched")
			assert.True(t, hasReal, "real directory should be watched")
			assert.True(t, hasSub, "subdirectory should be watched")
		})
	}
}

// TestProcessEvents_ExcludeInEvent tests that excluded paths in events are filtered.
func TestProcessEvents_ExcludeInEvent(t *testing.T) {
	tmpDir := t.TempDir()

	config := Config{
		Paths:         []string{tmpDir},
		Recursive:     false,
		Exclude:       []string{"*.tmp", "*.log"},
		DebounceDelay: 50 * time.Millisecond,
		BatchInterval: 100 * time.Millisecond,
		BufferSize:    100,
	}

	w, err := New(config)
	require.NoError(t, err)
	defer w.Stop()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	err = w.Start(ctx)
	require.NoError(t, err)

	// Create files that should be excluded
	tmpFile := filepath.Join(tmpDir, "test.tmp")
	err = os.WriteFile(tmpFile, []byte("temp"), 0644)
	require.NoError(t, err)

	logFile := filepath.Join(tmpDir, "test.log")
	err = os.WriteFile(logFile, []byte("log"), 0644)
	require.NoError(t, err)

	// Create a file that should be included
	txtFile := filepath.Join(tmpDir, "test.txt")
	err = os.WriteFile(txtFile, []byte("text"), 0644)
	require.NoError(t, err)

	// Check events channel - wait generously for the expected event
	events := w.Events()
	foundTxt := false

	timeout := time.After(5 * time.Second)
loop:
	for {
		select {
		case e, ok := <-events:
			if !ok {
				break loop
			}
			// Should only see the txt file
			if filepath.Base(e.Path) == "test.txt" {
				foundTxt = true
			}
			// Should NOT see tmp or log files
			assert.NotEqual(t, "test.tmp", filepath.Base(e.Path))
			assert.NotEqual(t, "test.log", filepath.Base(e.Path))
			if foundTxt {
				break loop
			}
		case <-timeout:
			break loop
		}
	}

	// The txt file event should have been received
	assert.True(t, foundTxt, "expected to receive event for test.txt")
}

// TestProcessEvents_HiddenFilesFiltered tests that hidden files are filtered when IgnoreHidden is true.
func TestProcessEvents_HiddenFilesFiltered(t *testing.T) {
	tmpDir := t.TempDir()

	config := Config{
		Paths:         []string{tmpDir},
		Recursive:     false,
		IgnoreHidden:  true,
		DebounceDelay: 50 * time.Millisecond,
		BatchInterval: 100 * time.Millisecond,
		BufferSize:    100,
	}

	w, err := New(config)
	require.NoError(t, err)
	defer w.Stop()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	err = w.Start(ctx)
	require.NoError(t, err)

	// Create a hidden file
	hiddenFile := filepath.Join(tmpDir, ".hidden")
	err = os.WriteFile(hiddenFile, []byte("hidden"), 0644)
	require.NoError(t, err)

	// Create a visible file
	visibleFile := filepath.Join(tmpDir, "visible.txt")
	err = os.WriteFile(visibleFile, []byte("visible"), 0644)
	require.NoError(t, err)

	events := w.Events()
	foundVisible := false

	timeout := time.After(5 * time.Second)
loop:
	for {
		select {
		case e, ok := <-events:
			if !ok {
				break loop
			}
			// Should only see visible file
			if filepath.Base(e.Path) == "visible.txt" {
				foundVisible = true
			}
			// Should NOT see hidden files
			assert.False(t, filepath.Base(e.Path)[0] == '.', "should not see hidden file: %s", e.Path)
		case <-timeout:
			break loop
		}
	}

	assert.True(t, foundVisible, "expected to receive event for visible.txt")
}

// TestStop_Idempotent tests that Stop succeeds, and keeps succeeding,
// whether or not the watcher was started.
func TestStop_Idempotent(t *testing.T) {
	for _, started := range []bool{true, false} {
		t.Run(fmt.Sprintf("started=%v", started), func(t *testing.T) {
			w, err := New(Config{
				Paths:         []string{t.TempDir()},
				DebounceDelay: 50 * time.Millisecond,
				BatchInterval: 100 * time.Millisecond,
				BufferSize:    100,
			})
			require.NoError(t, err)

			if started {
				ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
				defer cancel()
				require.NoError(t, w.Start(ctx))
			}

			assert.NoError(t, w.Stop())
			assert.NoError(t, w.Stop())
		})
	}
}

func TestStop_ReportsCloseFailureFromContextCancellation(t *testing.T) {
	config := Config{
		Paths:         []string{t.TempDir()},
		DebounceDelay: 50 * time.Millisecond,
		BatchInterval: 100 * time.Millisecond,
		BufferSize:    100,
	}
	w, err := New(config)
	require.NoError(t, err)

	fw, ok := w.(*fsWatcher)
	require.True(t, ok)
	errClose := errors.New("close failed")
	closeReal := fw.closeNotifier
	fw.closeNotifier = func() error {
		require.NoError(t, closeReal())
		return errClose
	}

	ctx, cancel := context.WithCancel(context.Background())
	require.NoError(t, w.Start(ctx))
	cancel()

	// Whether the cancellation or this call stops the watcher, the failed
	// close must reach the owner, and keep reaching it.
	for range 2 {
		err = w.Stop()
		var causeErr *WatcherCauseError
		require.ErrorAs(t, err, &causeErr)
		require.ErrorIs(t, err, errClose)
	}
}

func TestNew_RejectsMalformedExcludePattern(t *testing.T) {
	_, err := New(Config{
		Paths:      []string{t.TempDir()},
		Exclude:    []string{"[unclosed"},
		BufferSize: 10,
	})
	require.ErrorIs(t, err, filepath.ErrBadPattern)
}

// TestFlushEvents_ChannelFull tests that flushEvents drains the debouncer and
// drops the events that do not fit in the events channel.
func TestFlushEvents_ChannelFull(t *testing.T) {
	w, err := New(Config{
		Paths:         []string{t.TempDir()},
		DebounceDelay: 50 * time.Millisecond,
		BatchInterval: 100 * time.Millisecond,
		BufferSize:    2,
	})
	require.NoError(t, err)
	defer w.Stop()

	fw := w.(*fsWatcher)
	for i := range 5 {
		fw.debouncer.Add(Event{Type: EventCreate, Path: fmt.Sprintf("/file%d.txt", i)})
	}

	fw.flushEvents()

	assert.Len(t, fw.events, 2, "only the buffered events are delivered")
	assert.Zero(t, fw.debouncer.Count(), "the debouncer is drained")
}

// TestNew_InvalidPath tests New with an invalid path that doesn't exist.
func TestNew_InvalidPath(t *testing.T) {
	config := Config{
		Paths:         []string{"/this/path/does/not/exist"},
		DebounceDelay: 50 * time.Millisecond,
		BatchInterval: 100 * time.Millisecond,
		BufferSize:    100,
	}

	w, err := New(config)
	// Should still create watcher, but Start will fail
	if err != nil {
		assert.Error(t, err)
	} else {
		require.NotNil(t, w)
		ctx := context.Background()
		err = w.Start(ctx)
		assert.Error(t, err)
		w.Stop()
	}
}

// TestAddRecursive_ErrorReadDir tests addRecursive when ReadDir fails.
func TestAddRecursive_ErrorReadDir(t *testing.T) {
	tmpDir := t.TempDir()
	subDir := filepath.Join(tmpDir, "subdir")
	err := os.Mkdir(subDir, 0755)
	require.NoError(t, err)

	// Create a directory with no read permissions
	noReadDir := filepath.Join(subDir, "noread")
	err = os.Mkdir(noReadDir, 0000)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, os.Chmod(noReadDir, 0755)) })

	config := Config{
		Paths:         []string{tmpDir},
		Recursive:     true,
		DebounceDelay: 50 * time.Millisecond,
		BatchInterval: 100 * time.Millisecond,
		BufferSize:    100,
	}

	w, err := New(config)
	require.NoError(t, err)
	defer w.Stop()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// A failure below the root is skipped, so Start succeeds and the
	// readable siblings stay watched.
	require.NoError(t, w.Start(ctx))
	fw := w.(*fsWatcher)
	fw.mu.RLock()
	_, hasSub := fw.paths[subDir]
	fw.mu.RUnlock()
	assert.True(t, hasSub, "readable subdirectory should be watched")
}

// TestConvertEvent_DeletedFile tests convertEvent when file is deleted.
func TestConvertEvent_DeletedFile(t *testing.T) {
	tmpDir := t.TempDir()
	testFile := filepath.Join(tmpDir, "test.txt")

	// Create and delete file
	err := os.WriteFile(testFile, []byte("content"), 0644)
	require.NoError(t, err)

	config := Config{
		Paths:         []string{tmpDir},
		DebounceDelay: 50 * time.Millisecond,
		BatchInterval: 100 * time.Millisecond,
		BufferSize:    100,
		IgnoreHidden:  false,
	}

	w, err := New(config)
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	err = w.Start(ctx)
	require.NoError(t, err)
	defer w.Stop()

	// Delete the file - this triggers delete event where os.Stat will fail
	err = os.Remove(testFile)
	require.NoError(t, err)

	// Should receive delete event with size 0
	select {
	case event := <-w.Events():
		assert.Equal(t, EventDelete, event.Type)
		assert.Equal(t, testFile, event.Path)
		assert.Equal(t, int64(0), event.Size)
	case <-time.After(5 * time.Second):
		t.Fatal("timeout waiting for delete event")
	}
}

// TestGitIgnoreMatcher_PatternForms tests Match across the supported pattern forms.
func TestGitIgnoreMatcher_PatternForms(t *testing.T) {
	tests := []struct {
		name     string
		patterns []string
		cases    map[string]bool
	}{
		{
			name:     "negation",
			patterns: []string{"*.log", "!important.log"},
			cases: map[string]bool{
				"/app/error.log":     true,
				"/app/important.log": false,
				"/app/debug.log":     true,
			},
		},
		{
			name:     "directory only",
			patterns: []string{"temp/", "cache/"},
			cases: map[string]bool{
				"/app/temp/file.txt":      true,
				"/app/cache/data":         true,
				"/app/temp":               true,
				"/app/cache":              true,
				"/temp/file.txt":          true,
				"/app/temporary/file.txt": false,
				"/app/file.temp":          false,
			},
		},
		{
			name:     "double star",
			patterns: []string{"**/*.tmp"},
			cases: map[string]bool{
				"/app/build/output.tmp":      true,
				"/deep/nested/path/file.tmp": true,
				"/app/src/index.js":          false,
			},
		},
		{
			name:     "base name wildcard",
			patterns: []string{"*.txt"},
			cases: map[string]bool{
				"/root/file.txt":      true,
				"/deep/path/file.txt": true,
				"/file.log":           false,
			},
		},
		{
			name:     "path component",
			patterns: []string{"node_modules"},
			cases: map[string]bool{
				"/app/node_modules/package":        true,
				"/node_modules/lib":                true,
				"/deep/path/node_modules/index.js": true,
				"/app/not_node_modules/file":       false,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			matcher, err := newGitIgnoreMatcher(tt.patterns)
			require.NoError(t, err)
			for path, want := range tt.cases {
				assert.Equal(t, want, matcher.Match(path), "path: %s", path)
			}
		})
	}
}

// TestGitIgnoreMatcher_EmptyPattern tests empty pattern handling.
func TestGitIgnoreMatcher_EmptyPattern(t *testing.T) {
	patterns := []string{"", "*.log", ""}
	matcher, err := newGitIgnoreMatcher(patterns)
	require.NoError(t, err)
	require.NotNil(t, matcher)

	// Should have only one pattern (*.log)
	assert.Len(t, matcher.patterns, 1)
}

// TestMatchWildcard_SingleStar tests matchWildcard with single *.
func TestMatchWildcard_SingleStar(t *testing.T) {
	matcher, err := newGitIgnoreMatcher([]string{})
	require.NoError(t, err)

	testCases := []struct {
		path     string
		pattern  string
		expected bool
	}{
		{"/any/path", "*", true},
		{"/app/file.txt", "*.txt", true},
		{"/app/file.log", "*.txt", false},
	}

	for _, tc := range testCases {
		t.Run(tc.path, func(t *testing.T) {
			matched := matcher.matchWildcard(tc.path, tc.pattern)
			assert.Equal(t, tc.expected, matched, "path: %s, pattern: %s", tc.path, tc.pattern)
		})
	}
}

// TestRemovePath_WatcherRemoveError tests RemovePath when the watched path exists.
func TestRemovePath_WatcherRemoveError(t *testing.T) {
	tmpDir := t.TempDir()
	tmpDir2 := t.TempDir()

	config := Config{
		Paths:         []string{tmpDir},
		DebounceDelay: 50 * time.Millisecond,
		BatchInterval: 100 * time.Millisecond,
		BufferSize:    100,
	}

	w, err := New(config)
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	err = w.Start(ctx)
	require.NoError(t, err)
	defer w.Stop()

	// Add a second path
	err = w.AddPath(tmpDir2)
	require.NoError(t, err)

	// Remove it successfully
	err = w.RemovePath(tmpDir2)
	require.NoError(t, err)

	// Try to remove again - should get ErrPathNotWatched
	err = w.RemovePath(tmpDir2)
	assert.ErrorIs(t, err, ErrPathNotWatched)
}

// TestProcessEvents_WatcherClosed tests that Stop and a cancelled Start
// context both end processing and close the events channel.
func TestProcessEvents_WatcherClosed(t *testing.T) {
	tests := []struct {
		name string
		stop func(t *testing.T, w Watcher, cancel context.CancelFunc)
	}{
		{name: "Stop", stop: func(t *testing.T, w Watcher, _ context.CancelFunc) { require.NoError(t, w.Stop()) }},
		{name: "context cancelled", stop: func(_ *testing.T, _ Watcher, cancel context.CancelFunc) { cancel() }},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w, err := New(Config{
				Paths:         []string{t.TempDir()},
				DebounceDelay: 50 * time.Millisecond,
				BatchInterval: 100 * time.Millisecond,
				BufferSize:    100,
			})
			require.NoError(t, err)
			defer w.Stop()

			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			require.NoError(t, w.Start(ctx))

			tt.stop(t, w, cancel)

			// The goroutines exit and the events channel is closed.
			timeout := time.After(5 * time.Second)
			for {
				select {
				case _, ok := <-w.Events():
					if !ok {
						return
					}
				case <-timeout:
					t.Fatal("events channel was not closed")
				}
			}
		})
	}
}

// TestConvertEvent tests the event type and size convertEvent derives from
// each fsnotify operation, and that chmod and unknown operations are dropped.
func TestConvertEvent(t *testing.T) {
	tmpDir := t.TempDir()
	testFile := filepath.Join(tmpDir, "test.txt")
	require.NoError(t, os.WriteFile(testFile, []byte("content"), 0644))

	w, err := New(Config{
		Paths:         []string{tmpDir},
		DebounceDelay: 50 * time.Millisecond,
		BatchInterval: 100 * time.Millisecond,
		BufferSize:    100,
	})
	require.NoError(t, err)
	defer w.Stop()
	fw := w.(*fsWatcher)

	tests := []struct {
		name     string
		op       fsnotify.Op
		wantNil  bool
		wantType EventType
		wantSize int64
	}{
		{name: "create", op: fsnotify.Create, wantType: EventCreate, wantSize: 7},
		{name: "write", op: fsnotify.Write, wantType: EventModify, wantSize: 7},
		{name: "remove", op: fsnotify.Remove, wantType: EventDelete, wantSize: 0},
		{name: "rename", op: fsnotify.Rename, wantType: EventDelete, wantSize: 0},
		{name: "chmod", op: fsnotify.Chmod, wantNil: true},
		{name: "unknown", op: 0, wantNil: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			event := fw.convertEvent(fsnotify.Event{Name: testFile, Op: tt.op})
			if tt.wantNil {
				assert.Nil(t, event)
				return
			}
			require.NotNil(t, event)
			assert.Equal(t, tt.wantType, event.Type)
			assert.Equal(t, testFile, event.Path)
			assert.Equal(t, tt.wantSize, event.Size)
		})
	}
}

// TestAddPath_FileNotDirectory tests adding a file path instead of directory.
func TestAddPath_FileNotDirectory(t *testing.T) {
	tmpDir := t.TempDir()
	testFile := filepath.Join(tmpDir, "file.txt")
	err := os.WriteFile(testFile, []byte("test"), 0644)
	require.NoError(t, err)

	config := Config{
		Paths:         []string{tmpDir},
		DebounceDelay: 50 * time.Millisecond,
		BatchInterval: 100 * time.Millisecond,
		BufferSize:    100,
		Recursive:     false,
	}

	w, err := New(config)
	require.NoError(t, err)
	defer w.Stop()

	// Adding a file should work (fsnotify can watch files)
	err = w.AddPath(testFile)
	assert.NoError(t, err)
}

// TestWatcher_RemovePath_WithError tests removing a path that fails.
func TestWatcher_RemovePath_WithError(t *testing.T) {
	tmpDir := t.TempDir()

	config := Config{
		Paths:         []string{tmpDir},
		DebounceDelay: 50 * time.Millisecond,
		BatchInterval: 100 * time.Millisecond,
		BufferSize:    100,
	}

	w, err := New(config)
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	err = w.Start(ctx)
	require.NoError(t, err)
	defer w.Stop()

	// Try to remove a path that isn't being watched
	err = w.RemovePath("/some/other/path")
	assert.ErrorIs(t, err, ErrPathNotWatched)
}

// TestWatcher_NonRecursive tests non-recursive watching.
func TestWatcher_NonRecursive(t *testing.T) {
	tmpDir := t.TempDir()
	subDir := filepath.Join(tmpDir, "subdir")
	err := os.Mkdir(subDir, 0755)
	require.NoError(t, err)

	config := Config{
		Paths:         []string{tmpDir},
		DebounceDelay: 50 * time.Millisecond,
		BatchInterval: 100 * time.Millisecond,
		BufferSize:    100,
		Recursive:     false, // Non-recursive
		IgnoreHidden:  false,
	}

	w, err := New(config)
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	err = w.Start(ctx)
	require.NoError(t, err)
	defer w.Stop()

	time.Sleep(500 * time.Millisecond)

	// Create file in subdirectory - should NOT be watched
	testFile := filepath.Join(subDir, "test.txt")
	err = os.WriteFile(testFile, []byte("content"), 0644)
	require.NoError(t, err)

	// Should not receive event
	select {
	case event := <-w.Events():
		t.Fatalf("unexpected event from subdirectory in non-recursive mode: %+v", event)
	case <-time.After(1 * time.Second):
		// Expected - no event
	}

	// But file in root should be watched
	rootFile := filepath.Join(tmpDir, "root.txt")
	err = os.WriteFile(rootFile, []byte("content"), 0644)
	require.NoError(t, err)

	select {
	case event := <-w.Events():
		assert.Equal(t, rootFile, event.Path)
	case <-time.After(5 * time.Second):
		t.Fatal("timeout waiting for event from root directory")
	}
}

// TestWatcher_AddPath_AlreadyWatched tests adding a path that's already watched.
func TestWatcher_AddPath_AlreadyWatched(t *testing.T) {
	tmpDir := t.TempDir()

	config := Config{
		Paths:         []string{tmpDir},
		DebounceDelay: 50 * time.Millisecond,
		BatchInterval: 100 * time.Millisecond,
		BufferSize:    100,
	}

	w, err := New(config)
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	err = w.Start(ctx)
	require.NoError(t, err)
	defer w.Stop()

	// Add the same path again - should not error
	err = w.AddPath(tmpDir)
	assert.NoError(t, err)
}

// TestWatcher_CreateDirectory tests creating a new directory.
func TestWatcher_CreateDirectory(t *testing.T) {
	tmpDir := t.TempDir()

	config := Config{
		Paths:         []string{tmpDir},
		DebounceDelay: 50 * time.Millisecond,
		BatchInterval: 100 * time.Millisecond,
		BufferSize:    100,
		Recursive:     true,
		IgnoreHidden:  false,
	}

	w, err := New(config)
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	err = w.Start(ctx)
	require.NoError(t, err)
	defer w.Stop()

	time.Sleep(500 * time.Millisecond)

	// Create a new directory
	newDir := filepath.Join(tmpDir, "newdir")
	err = os.Mkdir(newDir, 0755)
	require.NoError(t, err)

	// May or may not receive an event depending on OS
	select {
	case <-w.Events():
		// Got event, that's fine
	case <-time.After(1 * time.Second):
		// No event, also fine - directories might not trigger events
	}
}

// TestAddPath_StatError tests that AddPath rejects a path that does not exist.
func TestAddPath_StatError(t *testing.T) {
	tmpDir := t.TempDir()

	config := Config{
		Paths:         []string{tmpDir},
		DebounceDelay: 50 * time.Millisecond,
		BatchInterval: 100 * time.Millisecond,
		BufferSize:    100,
	}

	w, err := New(config)
	require.NoError(t, err)
	defer w.Stop()

	// Try to add a path that doesn't exist
	err = w.AddPath("/this/path/absolutely/does/not/exist/anywhere")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to stat path")
}

// TestAddRecursive_MaxDepth tests how deep a recursive walk watches.
func TestAddRecursive_MaxDepth(t *testing.T) {
	tests := []struct {
		name       string
		maxDepth   int
		watched    []string
		notWatched []string
	}{
		{name: "unlimited", maxDepth: 0, watched: []string{"l1", "l1/l2", "l1/l2/l3", "l1/l2/l3/l4"}},
		{name: "limited to two levels", maxDepth: 2, watched: []string{"l1", "l1/l2"}, notWatched: []string{"l1/l2/l3", "l1/l2/l3/l4"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmpDir := t.TempDir()
			require.NoError(t, os.MkdirAll(filepath.Join(tmpDir, "l1", "l2", "l3", "l4"), 0755))

			w, err := New(Config{
				Paths:         []string{tmpDir},
				Recursive:     true,
				MaxDepth:      tt.maxDepth,
				DebounceDelay: 50 * time.Millisecond,
				BatchInterval: 100 * time.Millisecond,
				BufferSize:    100,
			})
			require.NoError(t, err)
			defer w.Stop()

			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			require.NoError(t, w.Start(ctx))

			fw := w.(*fsWatcher)
			fw.mu.RLock()
			defer fw.mu.RUnlock()
			for _, p := range tt.watched {
				assert.Contains(t, fw.paths, filepath.Join(tmpDir, p), "%s should be watched", p)
			}
			for _, p := range tt.notWatched {
				assert.NotContains(t, fw.paths, filepath.Join(tmpDir, p), "%s should not be watched", p)
			}
		})
	}
}
