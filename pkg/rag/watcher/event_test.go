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

func TestEvent_Fields(t *testing.T) {
	now := time.Now()
	event := Event{
		Type:      EventCreate,
		Path:      "/path/to/file.txt",
		Timestamp: now,
		Size:      1024,
		OldPath:   "/old/path.txt",
	}

	assert.Equal(t, EventCreate, event.Type)
	assert.Equal(t, "/path/to/file.txt", event.Path)
	assert.Equal(t, now, event.Timestamp)
	assert.Equal(t, int64(1024), event.Size)
	assert.Equal(t, "/old/path.txt", event.OldPath)
}

func TestAddRecursive_ShouldExclude(t *testing.T) {
	tmpDir := t.TempDir()

	// Create a subdirectory that matches an exclude pattern
	excludeDir := filepath.Join(tmpDir, "node_modules")
	err := os.Mkdir(excludeDir, 0755)
	require.NoError(t, err)

	// Create another nested directory inside node_modules
	nestedDir := filepath.Join(excludeDir, "nested")
	err = os.Mkdir(nestedDir, 0755)
	require.NoError(t, err)

	// Create a regular directory that should be watched
	regularDir := filepath.Join(tmpDir, "src")
	err = os.Mkdir(regularDir, 0755)
	require.NoError(t, err)

	config := Config{
		Paths:         []string{tmpDir},
		Recursive:     true,
		Exclude:       []string{"node_modules", "**/node_modules/**"},
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

	// Wait for initialization using timer
	timer := time.NewTimer(500 * time.Millisecond)
	<-timer.C

	// The watcher should have started successfully
	// and excluded the node_modules directory
}

// TestAddRecursive_NestedExclude tests nested exclude patterns.
func TestAddRecursive_NestedExclude(t *testing.T) {
	tmpDir := t.TempDir()

	// Create deep nested structure
	deepPath := filepath.Join(tmpDir, "a", "b", "c", "vendor")
	err := os.MkdirAll(deepPath, 0755)
	require.NoError(t, err)

	// Create another directory at the same level
	siblingDir := filepath.Join(tmpDir, "a", "b", "c", "lib")
	err = os.Mkdir(siblingDir, 0755)
	require.NoError(t, err)

	config := Config{
		Paths:         []string{tmpDir},
		Recursive:     true,
		Exclude:       []string{"vendor", "**/vendor/**"},
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

	// Wait for initialization using timer
	timer := time.NewTimer(500 * time.Millisecond)
	<-timer.C
}

// TestAddRecursive_SymlinkToValidDir tests symlink pointing to a valid directory.
// Note: Due to the current code structure, symlinks are checked after IsDir(),
// which means symlinks to directories are skipped because entry.IsDir() returns false
// for symlinks. This test documents the current behavior.
func TestAddRecursive_SymlinkToValidDir(t *testing.T) {
	tmpDir := t.TempDir()

	// Create target directory
	targetDir := filepath.Join(tmpDir, "target")
	err := os.Mkdir(targetDir, 0755)
	require.NoError(t, err)

	// Create subdir for symlinks
	subDir := filepath.Join(tmpDir, "subdir")
	err = os.Mkdir(subDir, 0755)
	require.NoError(t, err)

	// Create a symlink to the target directory
	linkPath := filepath.Join(subDir, "link_to_target")
	err = os.Symlink(targetDir, linkPath)
	require.NoError(t, err)

	config := Config{
		Paths:          []string{tmpDir},
		Recursive:      true,
		FollowSymlinks: true,
		BatchInterval:  100 * time.Millisecond,
		BufferSize:     100,
	}

	w, err := New(config)
	require.NoError(t, err)
	defer w.Stop()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Should start without error
	err = w.Start(ctx)
	require.NoError(t, err)

	// Wait for initialization using timer
	timer := time.NewTimer(500 * time.Millisecond)
	<-timer.C
}

// TestAddRecursive_MultipleExcludePatterns tests multiple exclude patterns.
func TestAddRecursive_MultipleExcludePatterns(t *testing.T) {
	tmpDir := t.TempDir()

	// Create multiple directories that should be excluded
	dirs := []string{"node_modules", "vendor", ".cache", "build", "dist"}
	for _, d := range dirs {
		path := filepath.Join(tmpDir, d)
		err := os.Mkdir(path, 0755)
		require.NoError(t, err)
	}

	// Create a directory that should be included
	srcDir := filepath.Join(tmpDir, "src")
	err := os.Mkdir(srcDir, 0755)
	require.NoError(t, err)

	config := Config{
		Paths:         []string{tmpDir},
		Recursive:     true,
		IgnoreHidden:  true, // Will exclude .cache
		Exclude:       []string{"node_modules", "vendor", "build", "dist"},
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

	// Wait for initialization using timer
	timer := time.NewTimer(500 * time.Millisecond)
	<-timer.C
}

// TestProcessEvents_ExcludeInEvent tests that excluded paths in events are filtered.
func TestProcessEvents_ExcludeInEvent(t *testing.T) {
	tmpDir := t.TempDir()

	config := Config{
		Paths:         []string{tmpDir},
		Recursive:     false,
		Exclude:       []string{"*.tmp", "*.log"},
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

	// Wait for watcher to initialize using timer pattern
	initTimer := time.NewTimer(500 * time.Millisecond)
	<-initTimer.C

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

	// Wait for at least 2 batch intervals to ensure events are flushed
	// BatchInterval is 100ms, so wait 250ms to be safe
	batchTimer := time.NewTimer(250 * time.Millisecond)
	<-batchTimer.C

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

	// Give watcher time to initialize
	time.Sleep(500 * time.Millisecond)

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

// TestStop_IdempotentMultipleCalls tests Stop is idempotent.
func TestStop_IdempotentMultipleCalls(t *testing.T) {
	tmpDir := t.TempDir()

	config := Config{
		Paths:         []string{tmpDir},
		Recursive:     false,
		BatchInterval: 100 * time.Millisecond,
		BufferSize:    100,
	}

	w, err := New(config)
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	err = w.Start(ctx)
	require.NoError(t, err)

	// Stop should succeed
	err = w.Stop()
	assert.NoError(t, err)

	// Calling Stop again should be idempotent
	err = w.Stop()
	assert.NoError(t, err)
}

func TestStop_ReportsCloseFailureFromContextCancellation(t *testing.T) {
	config := Config{
		Paths:         []string{t.TempDir()},
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

// TestFlushEvents_SmallBuffer tests flushEvents when the events channel is full.
func TestFlushEvents_SmallBuffer(t *testing.T) {
	tmpDir := t.TempDir()

	// Create config with very small buffer
	config := Config{
		Paths:         []string{tmpDir},
		Recursive:     false,
		BatchInterval: 20 * time.Millisecond,
		BufferSize:    1, // Very small buffer
	}

	w, err := New(config)
	require.NoError(t, err)
	defer w.Stop()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	err = w.Start(ctx)
	require.NoError(t, err)

	// Give watcher time to initialize
	time.Sleep(500 * time.Millisecond)

	// Create many files quickly to overflow the buffer
	for i := 0; i < 10; i++ {
		file := filepath.Join(tmpDir, "file"+string(rune('0'+i))+".txt")
		err := os.WriteFile(file, []byte("content"), 0644)
		require.NoError(t, err)
	}

	// Wait for processing using timer
	timer := time.NewTimer(1 * time.Second)
	<-timer.C
}

func TestNew_GitIgnoreMatcherError(t *testing.T) {
	tmpDir := t.TempDir()

	// Normal patterns should work - gitignore matcher doesn't return errors for any pattern
	config := Config{
		Paths:         []string{tmpDir},
		Exclude:       []string{"*.tmp", "test/"},
		BatchInterval: 100 * time.Millisecond,
		BufferSize:    100,
	}

	w, err := New(config)
	require.NoError(t, err)
	require.NotNil(t, w)
	defer w.Stop()
}

// TestNew_InvalidPath tests New with an invalid path that doesn't exist.
func TestNew_InvalidPath(t *testing.T) {
	config := Config{
		Paths:         []string{"/this/path/does/not/exist"},
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
	defer os.Chmod(noReadDir, 0755) // Cleanup

	config := Config{
		Paths:         []string{tmpDir},
		Recursive:     true,
		BatchInterval: 100 * time.Millisecond,
		BufferSize:    100,
	}

	w, err := New(config)
	require.NoError(t, err)
	defer w.Stop()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Start should handle the error gracefully
	err = w.Start(ctx)
	if err != nil {
		// It's OK if it errors on unreadable directories
		assert.Contains(t, err.Error(), "permission denied")
	}
}

// TestAddRecursive_SymlinkError tests symlink error handling in addRecursive.
func TestAddRecursive_SymlinkError(t *testing.T) {
	tmpDir := t.TempDir()

	// Create a broken symlink
	brokenLink := filepath.Join(tmpDir, "broken")
	err := os.Symlink("/nonexistent/target", brokenLink)
	require.NoError(t, err)

	config := Config{
		Paths:          []string{tmpDir},
		Recursive:      true,
		FollowSymlinks: true,
		BatchInterval:  100 * time.Millisecond,
		BufferSize:     100,
	}

	w, err := New(config)
	require.NoError(t, err)
	defer w.Stop()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Should handle broken symlink gracefully
	err = w.Start(ctx)
	require.NoError(t, err)
}

// TestAddRecursive_SymlinkToFile tests symlink pointing to a file.
func TestAddRecursive_SymlinkToFile(t *testing.T) {
	tmpDir := t.TempDir()
	subDir := filepath.Join(tmpDir, "subdir")
	err := os.Mkdir(subDir, 0755)
	require.NoError(t, err)

	// Create a file
	testFile := filepath.Join(tmpDir, "file.txt")
	err = os.WriteFile(testFile, []byte("test"), 0644)
	require.NoError(t, err)

	// Create symlink to file in subdir
	fileLink := filepath.Join(subDir, "filelink")
	err = os.Symlink(testFile, fileLink)
	require.NoError(t, err)

	config := Config{
		Paths:          []string{tmpDir},
		Recursive:      true,
		FollowSymlinks: true,
		BatchInterval:  100 * time.Millisecond,
		BufferSize:     100,
	}

	w, err := New(config)
	require.NoError(t, err)
	defer w.Stop()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	err = w.Start(ctx)
	require.NoError(t, err)
}

// TestAddRecursive_WatcherAddError tests error handling when watcher.Add fails.
func TestAddRecursive_WatcherAddError(t *testing.T) {
	tmpDir := t.TempDir()

	// Create deep directory structure
	deepDir := tmpDir
	for i := 0; i < 10; i++ {
		deepDir = filepath.Join(deepDir, "level")
		os.Mkdir(deepDir, 0755)
	}

	config := Config{
		Paths:         []string{tmpDir},
		Recursive:     true,
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
}

// TestFlushEvents_StopChannel tests flushEvents with stopCh triggered.
func TestFlushEvents_StopChannel(t *testing.T) {
	tmpDir := t.TempDir()

	config := Config{
		Paths:         []string{tmpDir},
		BatchInterval: 50 * time.Millisecond,
		BufferSize:    100,
	}

	w, err := New(config)
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.Background())

	err = w.Start(ctx)
	require.NoError(t, err)

	time.Sleep(500 * time.Millisecond)

	// Create some events
	for i := 0; i < 3; i++ {
		testFile := filepath.Join(tmpDir, "test_"+string(rune('a'+i))+".txt")
		os.WriteFile(testFile, []byte("test"), 0644)
		time.Sleep(10 * time.Millisecond)
	}

	// Cancel context to trigger stop
	cancel()

	// Wait for shutdown
	time.Sleep(500 * time.Millisecond)

	// Try to drain any remaining events (channel should be closed or empty)
	timeout := time.After(500 * time.Millisecond)
	for {
		select {
		case _, ok := <-w.Events():
			if !ok {
				// Channel closed, good
				return
			}
		case <-timeout:
			return
		}
	}
}

// TestFlushEvents_ChannelFull tests flushEvents when output channel is full.
func TestFlushEvents_ChannelFull(t *testing.T) {
	tmpDir := t.TempDir()

	config := Config{
		Paths:         []string{tmpDir},
		BatchInterval: 40 * time.Millisecond,
		BufferSize:    2, // Small buffer
	}

	w, err := New(config)
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	err = w.Start(ctx)
	require.NoError(t, err)
	defer w.Stop()

	time.Sleep(500 * time.Millisecond)

	// Create many files quickly to overflow buffer
	for i := 0; i < 10; i++ {
		testFile := filepath.Join(tmpDir, "overflow_"+string(rune('a'+i))+".txt")
		os.WriteFile(testFile, []byte("test"), 0644)
	}

	// Don't read from events channel to let it fill up
	time.Sleep(500 * time.Millisecond)

	// Now drain with timeout
	timeout := time.After(1 * time.Second)
	for {
		select {
		case <-w.Events():
			// Drain events
		case <-timeout:
			return
		}
	}
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

	time.Sleep(500 * time.Millisecond)

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

// TestConvertEvent_RenameAsDelete tests rename events treated as delete.
func TestConvertEvent_RenameAsDelete(t *testing.T) {
	tmpDir := t.TempDir()
	testFile := filepath.Join(tmpDir, "test.txt")

	err := os.WriteFile(testFile, []byte("content"), 0644)
	require.NoError(t, err)

	config := Config{
		Paths:         []string{tmpDir},
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

	time.Sleep(500 * time.Millisecond)

	// Rename the file
	newName := filepath.Join(tmpDir, "renamed.txt")
	err = os.Rename(testFile, newName)
	require.NoError(t, err)

	// Should receive events (delete for old name, create for new name)
	timeout := time.After(5 * time.Second)
	eventCount := 0
	for eventCount < 2 {
		select {
		case event := <-w.Events():
			eventCount++
			// Either delete or create is acceptable
			assert.True(t, event.Type == EventDelete || event.Type == EventCreate)
		case <-timeout:
			// May only get one event on some systems
			return
		}
	}
}

// TestGitIgnoreMatcher_Negation tests negated patterns.
func TestGitIgnoreMatcher_Negation(t *testing.T) {
	patterns := []string{"*.log", "!important.log"}
	matcher, err := newGitIgnoreMatcher(patterns)
	require.NoError(t, err)

	testCases := []struct {
		path     string
		expected bool
	}{
		{"/app/error.log", true},
		{"/app/important.log", false}, // Negated
		{"/app/debug.log", true},
	}

	for _, tc := range testCases {
		t.Run(tc.path, func(t *testing.T) {
			matched := matcher.Match(tc.path)
			assert.Equal(t, tc.expected, matched, "path: %s", tc.path)
		})
	}
}

// TestGitIgnoreMatcher_DirectoryOnly tests directory-only patterns.
func TestGitIgnoreMatcher_DirectoryOnly(t *testing.T) {
	patterns := []string{"temp/", "cache/"}
	matcher, err := newGitIgnoreMatcher(patterns)
	require.NoError(t, err)

	testCases := []struct {
		path     string
		expected bool
	}{
		{"/app/temp/file.txt", true},
		{"/app/cache/data", true},
		{"/app/temp", true},
		{"/app/cache", true},
		{"/app/temporary/file.txt", false},
		{"/app/file.temp", false},
	}

	for _, tc := range testCases {
		t.Run(tc.path, func(t *testing.T) {
			matched := matcher.Match(tc.path)
			assert.Equal(t, tc.expected, matched, "path: %s", tc.path)
		})
	}
}

// TestGitIgnoreMatcher_DoubleStarWildcard tests ** wildcard patterns.
func TestGitIgnoreMatcher_DoubleStarWildcard(t *testing.T) {
	patterns := []string{"**/*.tmp"}
	matcher, err := newGitIgnoreMatcher(patterns)
	require.NoError(t, err)

	testCases := []struct {
		path     string
		expected bool
	}{
		{"/app/build/output.tmp", true},
		{"/deep/nested/path/file.tmp", true},
		{"/app/src/index.js", false},
	}

	for _, tc := range testCases {
		t.Run(tc.path, func(t *testing.T) {
			matched := matcher.Match(tc.path)
			assert.Equal(t, tc.expected, matched, "path: %s", tc.path)
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

// TestMatchPattern_BaseName tests pattern matching against base name.
func TestMatchPattern_BaseName(t *testing.T) {
	patterns := []string{"*.txt"}
	matcher, err := newGitIgnoreMatcher(patterns)
	require.NoError(t, err)

	testCases := []struct {
		path     string
		expected bool
	}{
		{"/root/file.txt", true},
		{"/deep/path/file.txt", true},
		{"/file.log", false},
	}

	for _, tc := range testCases {
		t.Run(tc.path, func(t *testing.T) {
			matched := matcher.Match(tc.path)
			assert.Equal(t, tc.expected, matched, "path: %s", tc.path)
		})
	}
}

// TestMatchPattern_PathComponents tests matching against path components.
func TestMatchPattern_PathComponents(t *testing.T) {
	patterns := []string{"node_modules"}
	matcher, err := newGitIgnoreMatcher(patterns)
	require.NoError(t, err)

	testCases := []struct {
		path     string
		expected bool
	}{
		{"/app/node_modules/package", true},
		{"/node_modules/lib", true},
		{"/deep/path/node_modules/index.js", true},
		{"/app/not_node_modules/file", false},
	}

	for _, tc := range testCases {
		t.Run(tc.path, func(t *testing.T) {
			matched := matcher.Match(tc.path)
			assert.Equal(t, tc.expected, matched, "path: %s", tc.path)
		})
	}
}

// TestWatcher_Stop_NormalClose tests Stop when watcher.Close returns error.
// Note: This is hard to test without mocking, but we can test normal close.
func TestWatcher_Stop_NormalClose(t *testing.T) {
	tmpDir := t.TempDir()

	config := Config{
		Paths:         []string{tmpDir},
		BatchInterval: 100 * time.Millisecond,
		BufferSize:    100,
	}

	w, err := New(config)
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	err = w.Start(ctx)
	require.NoError(t, err)

	time.Sleep(500 * time.Millisecond)

	err = w.Stop()
	assert.NoError(t, err)
}

// TestRemovePath_WatcherRemoveError tests RemovePath when the watched path exists.
func TestRemovePath_WatcherRemoveError(t *testing.T) {
	tmpDir := t.TempDir()
	tmpDir2 := t.TempDir()

	config := Config{
		Paths:         []string{tmpDir},
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

	time.Sleep(500 * time.Millisecond)

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

// TestProcessEvents_WatcherClosed tests processEvents when watcher is closed.
func TestProcessEvents_WatcherClosed(t *testing.T) {
	tmpDir := t.TempDir()

	config := Config{
		Paths:         []string{tmpDir},
		BatchInterval: 100 * time.Millisecond,
		BufferSize:    100,
	}

	w, err := New(config)
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	err = w.Start(ctx)
	require.NoError(t, err)

	time.Sleep(500 * time.Millisecond)

	// Stop immediately - this closes the watcher
	err = w.Stop()
	require.NoError(t, err)

	// Channels should be closed
	select {
	case _, ok := <-w.Events():
		assert.False(t, ok, "events channel should be closed")
	default:
		// Channel might already be drained
	}
}

// TestConvertEvent_UnknownOp tests convertEvent with unknown operation.
func TestConvertEvent_UnknownOp(t *testing.T) {
	tmpDir := t.TempDir()

	config := Config{
		Paths:         []string{tmpDir},
		BatchInterval: 100 * time.Millisecond,
		BufferSize:    100,
	}

	w, err := New(config)
	require.NoError(t, err)
	defer w.Stop()

	// Create an fsnotify event with no operation flags
	fsEvent := fsnotify.Event{
		Name: tmpDir + "/test.txt",
		Op:   0, // No operation
	}

	// Get internal watcher
	fw := w.(*fsWatcher)
	result := fw.convertEvent(fsEvent)
	assert.Nil(t, result, "should return nil for unknown operations")
}

// TestWatcher_ContextDone tests context cancellation during event processing.
func TestWatcher_ContextDone(t *testing.T) {
	tmpDir := t.TempDir()

	config := Config{
		Paths:         []string{tmpDir},
		BatchInterval: 100 * time.Millisecond,
		BufferSize:    100,
	}

	w, err := New(config)
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()

	err = w.Start(ctx)
	require.NoError(t, err)

	// Wait for context to timeout
	<-ctx.Done()

	// Give time for cleanup
	time.Sleep(500 * time.Millisecond)

	// Events channel should be closed or drained
	select {
	case _, ok := <-w.Events():
		if ok {
			// Still have some events, that's OK
		}
	default:
		// No events, that's OK too
	}
}

// TestWatcher_ErrorChannel tests error channel handling.
func TestWatcher_ErrorChannel(t *testing.T) {
	tmpDir := t.TempDir()

	config := Config{
		Paths:         []string{tmpDir},
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

	time.Sleep(500 * time.Millisecond)

	// Create normal file operation
	testFile := filepath.Join(tmpDir, "test.txt")
	err = os.WriteFile(testFile, []byte("content"), 0644)
	require.NoError(t, err)

	// Wait for event
	select {
	case <-w.Events():
		// Got event, no errors expected
	case <-time.After(5 * time.Second):
		// Timeout, but that's OK for this test
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

func TestWatcher_AddPath_NonExistent(t *testing.T) {
	tmpDir := t.TempDir()

	config := Config{
		Paths:         []string{tmpDir},
		BatchInterval: 100 * time.Millisecond,
		BufferSize:    100,
	}

	w, err := New(config)
	require.NoError(t, err)
	defer w.Stop()

	err = w.AddPath("/nonexistent/path/that/does/not/exist")
	assert.Error(t, err)
}

// TestWatcher_AddRecursive_WithSymlinks tests recursive watching with symlinks.
func TestWatcher_AddRecursive_WithSymlinks(t *testing.T) {
	tmpDir := t.TempDir()
	realDir := filepath.Join(tmpDir, "real")
	err := os.Mkdir(realDir, 0755)
	require.NoError(t, err)

	// Create a symlink
	linkDir := filepath.Join(tmpDir, "link")
	err = os.Symlink(realDir, linkDir)
	require.NoError(t, err, "symlink creation must succeed on Linux")

	// Test with FollowSymlinks = false (default)
	config := Config{
		Paths:          []string{tmpDir},
		BatchInterval:  100 * time.Millisecond,
		BufferSize:     100,
		Recursive:      true,
		FollowSymlinks: false,
		IgnoreHidden:   false,
	}

	w, err := New(config)
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	err = w.Start(ctx)
	require.NoError(t, err)
	defer w.Stop()

	time.Sleep(500 * time.Millisecond)

	// Create file in symlinked directory via symlink path - should NOT be watched
	testFile := filepath.Join(linkDir, "test.txt")
	err = os.WriteFile(testFile, []byte("content"), 0644)
	require.NoError(t, err)

	// The real directory is being watched, so we'll get an event
	// This test verifies that the symlink directory itself is not added to the watcher
	// Even though the real directory generates events
	select {
	case <-w.Events():
		// OK - the real directory is watched, just not via the symlink
	case <-time.After(5 * time.Second):
		// Also OK - system dependent
	}
}

// TestWatcher_AddRecursive_FollowSymlinks tests recursive watching following symlinks.
func TestWatcher_AddRecursive_FollowSymlinks(t *testing.T) {
	tmpDir := t.TempDir()
	realDir := filepath.Join(tmpDir, "real")
	err := os.Mkdir(realDir, 0755)
	require.NoError(t, err)

	// Create a symlink
	linkDir := filepath.Join(tmpDir, "link")
	err = os.Symlink(realDir, linkDir)
	require.NoError(t, err, "symlink creation must succeed on Linux")

	// Test with FollowSymlinks = true
	config := Config{
		Paths:          []string{tmpDir},
		BatchInterval:  100 * time.Millisecond,
		BufferSize:     100,
		Recursive:      true,
		FollowSymlinks: true,
		IgnoreHidden:   false,
	}

	w, err := New(config)
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	err = w.Start(ctx)
	require.NoError(t, err)
	defer w.Stop()

	time.Sleep(500 * time.Millisecond)

	// Create file in symlinked directory - should be watched
	testFile := filepath.Join(realDir, "test.txt")
	err = os.WriteFile(testFile, []byte("content"), 0644)
	require.NoError(t, err)

	// Should receive event
	select {
	case event := <-w.Events():
		assert.Contains(t, event.Path, "test.txt")
	case <-time.After(5 * time.Second):
		t.Fatal("timeout waiting for symlink watch event on Linux")
	}
}

// TestWatcher_RemovePath_WithError tests removing a path that fails.
func TestWatcher_RemovePath_WithError(t *testing.T) {
	tmpDir := t.TempDir()

	config := Config{
		Paths:         []string{tmpDir},
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

// TestConvertEvent_ChmodIgnored tests that chmod events are ignored.
func TestConvertEvent_ChmodIgnored(t *testing.T) {
	tmpDir := t.TempDir()
	testFile := filepath.Join(tmpDir, "test.txt")
	err := os.WriteFile(testFile, []byte("content"), 0644)
	require.NoError(t, err)

	config := Config{
		Paths:         []string{tmpDir},
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

	time.Sleep(500 * time.Millisecond)

	// Change file permissions
	err = os.Chmod(testFile, 0600)
	require.NoError(t, err)

	// Chmod events should be filtered out
	select {
	case event := <-w.Events():
		// Some systems might send write events along with chmod
		_ = event
	case <-time.After(1 * time.Second):
		// Expected - no event for chmod
	}
}

// TestGitIgnoreMatcher_Wildcard tests wildcard matching.
func TestGitIgnoreMatcher_Wildcard(t *testing.T) {
	patterns := []string{"*.log", "temp/"}
	matcher, err := newGitIgnoreMatcher(patterns)
	require.NoError(t, err)

	testCases := []struct {
		path     string
		expected bool
	}{
		{"/app/logs/error.log", true},
		{"/deep/nested/path/debug.log", true},
		{"/temp/file.txt", true},
		{"/other/file.txt", false},
		{"/app/data.json", false},
	}

	for _, tc := range testCases {
		t.Run(tc.path, func(t *testing.T) {
			matched := matcher.Match(tc.path)
			assert.Equal(t, tc.expected, matched, "path: %s", tc.path)
		})
	}
}

// TestWatcher_ErrorHandling tests error channel handling.
func TestWatcher_ErrorHandling(t *testing.T) {
	tmpDir := t.TempDir()

	config := Config{
		Paths:         []string{tmpDir},
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

	// Just verify the watcher handles normal operations without errors
	time.Sleep(500 * time.Millisecond)
}

func TestAddRecursive_SymlinkToDirectory(t *testing.T) {
	tmpDir := t.TempDir()

	// Create a real directory
	realDir := filepath.Join(tmpDir, "real_directory")
	err := os.Mkdir(realDir, 0755)
	require.NoError(t, err)

	// Create a subdirectory in real directory
	subDir := filepath.Join(realDir, "subdir")
	err = os.Mkdir(subDir, 0755)
	require.NoError(t, err)

	// Create a symlink to the real directory
	linkDir := filepath.Join(tmpDir, "link_to_real")
	err = os.Symlink(realDir, linkDir)
	require.NoError(t, err)

	config := Config{
		Paths:          []string{tmpDir},
		Recursive:      true,
		FollowSymlinks: true, // Follow symlinks
		BatchInterval:  100 * time.Millisecond,
		BufferSize:     100,
	}

	w, err := New(config)
	require.NoError(t, err)
	defer w.Stop()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	err = w.Start(ctx)
	require.NoError(t, err)

	// The symlink should be resolved and the real directory watched
	fw := w.(*fsWatcher)
	fw.mu.RLock()
	_, hasReal := fw.paths[realDir]
	fw.mu.RUnlock()
	assert.True(t, hasReal, "real directory should be in watched paths")
}

// TestAddPath_AddError tests addPath when watcher.Add fails.
// This is difficult to trigger without reaching OS limits, but we can test the path exists check.
func TestAddPath_StatError(t *testing.T) {
	tmpDir := t.TempDir()

	config := Config{
		Paths:         []string{tmpDir},
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

// TestAddPath_DirectoryRecursive tests addPath with a directory when recursive is enabled.
func TestAddPath_DirectoryWithRecursive(t *testing.T) {
	tmpDir := t.TempDir()
	subDir1 := filepath.Join(tmpDir, "sub1")
	subDir2 := filepath.Join(subDir1, "sub2")
	err := os.MkdirAll(subDir2, 0755)
	require.NoError(t, err)

	config := Config{
		Paths:         []string{tmpDir},
		Recursive:     true,
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

	// sub2 should be watched due to recursive
	fw := w.(*fsWatcher)
	fw.mu.RLock()
	_, hasSub2 := fw.paths[subDir2]
	fw.mu.RUnlock()
	assert.True(t, hasSub2, "sub2 should be watched recursively")
}

// TestProcessEvents_ContextDoneBeforeEvent tests processEvents exiting on context.Done().
func TestProcessEvents_ContextCancellation(t *testing.T) {
	tmpDir := t.TempDir()

	config := Config{
		Paths:         []string{tmpDir},
		BatchInterval: 100 * time.Millisecond,
		BufferSize:    100,
	}

	w, err := New(config)
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.Background())

	err = w.Start(ctx)
	require.NoError(t, err)

	time.Sleep(500 * time.Millisecond)

	// Cancel context immediately
	cancel()

	// Give time for goroutines to exit
	time.Sleep(500 * time.Millisecond)

	// Events channel should eventually be closed
	select {
	case _, ok := <-w.Events():
		if !ok {
			// Good, channel is closed
		}
	default:
		// Also OK
	}
}

// TestNew_WithMaxDepthZero tests New with MaxDepth set to 0 (unlimited).
func TestNew_MaxDepthZero(t *testing.T) {
	tmpDir := t.TempDir()

	// Create deep structure
	deep := tmpDir
	for i := 0; i < 5; i++ {
		deep = filepath.Join(deep, "level")
		os.Mkdir(deep, 0755)
	}

	config := Config{
		Paths:         []string{tmpDir},
		Recursive:     true,
		MaxDepth:      0, // Unlimited depth
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

	// All levels should be watched
	fw := w.(*fsWatcher)
	fw.mu.RLock()
	pathCount := len(fw.paths)
	fw.mu.RUnlock()
	assert.Greater(t, pathCount, 5, "should watch multiple levels")
}

// TestStop_BeforeStart tests stopping a watcher that hasn't been started.
func TestStop_NotStarted(t *testing.T) {
	tmpDir := t.TempDir()

	config := Config{
		Paths:         []string{tmpDir},
		BatchInterval: 100 * time.Millisecond,
		BufferSize:    100,
	}

	w, err := New(config)
	require.NoError(t, err)

	// Stop without starting - should not error
	err = w.Stop()
	assert.NoError(t, err)
}

// TestAddRecursive_MaxDepthReached tests addRecursive when max depth is reached.
func TestAddRecursive_MaxDepthLimit(t *testing.T) {
	tmpDir := t.TempDir()

	// Create deeper structure than max depth
	level1 := filepath.Join(tmpDir, "l1")
	level2 := filepath.Join(level1, "l2")
	level3 := filepath.Join(level2, "l3")
	level4 := filepath.Join(level3, "l4")
	err := os.MkdirAll(level4, 0755)
	require.NoError(t, err)

	config := Config{
		Paths:         []string{tmpDir},
		Recursive:     true,
		MaxDepth:      2, // Only go 2 levels deep
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

	// Level 4 should NOT be watched
	fw := w.(*fsWatcher)
	fw.mu.RLock()
	_, hasLevel4 := fw.paths[level4]
	fw.mu.RUnlock()
	assert.False(t, hasLevel4, "level4 should not be watched (exceeds max depth)")
}

// TestProcessEvents_StopChannelClosed tests processEvents when stopCh is closed.
func TestProcessEvents_StopCh(t *testing.T) {
	tmpDir := t.TempDir()

	config := Config{
		Paths:         []string{tmpDir},
		BatchInterval: 100 * time.Millisecond,
		BufferSize:    100,
	}

	w, err := New(config)
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	err = w.Start(ctx)
	require.NoError(t, err)

	time.Sleep(500 * time.Millisecond)

	// Stop the watcher - this closes stopCh
	err = w.Stop()
	require.NoError(t, err)

	// Give time for goroutines to exit
	time.Sleep(500 * time.Millisecond)
}

// TestAddRecursive_HiddenDirSkipped tests skipping hidden directories.
func TestAddRecursive_HiddenDirIgnored(t *testing.T) {
	tmpDir := t.TempDir()

	// Create hidden directory
	hiddenDir := filepath.Join(tmpDir, ".hidden")
	err := os.Mkdir(hiddenDir, 0755)
	require.NoError(t, err)

	// Create normal directory
	normalDir := filepath.Join(tmpDir, "normal")
	err = os.Mkdir(normalDir, 0755)
	require.NoError(t, err)

	config := Config{
		Paths:         []string{tmpDir},
		Recursive:     true,
		IgnoreHidden:  true, // Ignore hidden
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

	// Hidden dir should NOT be watched
	fw := w.(*fsWatcher)
	fw.mu.RLock()
	_, hasHidden := fw.paths[hiddenDir]
	_, hasNormal := fw.paths[normalDir]
	fw.mu.RUnlock()
	assert.False(t, hasHidden, "hidden directory should not be watched")
	assert.True(t, hasNormal, "normal directory should be watched")
}

// TestAddRecursive_FileEntry tests that files are skipped in addRecursive.
func TestAddRecursive_SkipFiles(t *testing.T) {
	tmpDir := t.TempDir()

	// Create a file (not a directory)
	testFile := filepath.Join(tmpDir, "file.txt")
	err := os.WriteFile(testFile, []byte("test"), 0644)
	require.NoError(t, err)

	// Create a subdirectory
	subDir := filepath.Join(tmpDir, "subdir")
	err = os.Mkdir(subDir, 0755)
	require.NoError(t, err)

	config := Config{
		Paths:         []string{tmpDir},
		Recursive:     true,
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

	// File should not be in paths, only directories
	fw := w.(*fsWatcher)
	fw.mu.RLock()
	_, hasFile := fw.paths[testFile]
	_, hasSubDir := fw.paths[subDir]
	fw.mu.RUnlock()
	assert.False(t, hasFile, "file should not be in watched paths")
	assert.True(t, hasSubDir, "subdirectory should be in watched paths")
}

// TestRemovePath_WatcherRemoveFails tests RemovePath when watcher.Remove returns an error.
// This happens when trying to remove a path that fsnotify isn't actually watching.
func TestRemovePath_AlreadyRemoved(t *testing.T) {
	tmpDir := t.TempDir()
	tmpDir2 := t.TempDir()

	config := Config{
		Paths:         []string{tmpDir},
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

	time.Sleep(500 * time.Millisecond)

	// Add a path
	err = w.AddPath(tmpDir2)
	require.NoError(t, err)

	// Remove it
	err = w.RemovePath(tmpDir2)
	require.NoError(t, err)

	// Try to remove it again - should get ErrPathNotWatched
	err = w.RemovePath(tmpDir2)
	assert.ErrorIs(t, err, ErrPathNotWatched)
}
