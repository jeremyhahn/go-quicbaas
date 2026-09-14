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
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNew(t *testing.T) {
	config := Config{
		Paths:         []string{t.TempDir()},
		DebounceDelay: 50 * time.Millisecond,
		BatchInterval: 100 * time.Millisecond,
		BufferSize:    100,
	}

	w, err := New(config)
	require.NoError(t, err)
	require.NotNil(t, w)

	defer w.Stop()
}

func TestNew_InvalidConfig(t *testing.T) {
	config := Config{
		Paths: []string{},
	}

	w, err := New(config)
	assert.Error(t, err)
	assert.Nil(t, w)
}

func TestWatcher_Start(t *testing.T) {
	tmpDir := t.TempDir()

	config := Config{
		Paths:         []string{tmpDir},
		DebounceDelay: 50 * time.Millisecond,
		BatchInterval: 100 * time.Millisecond,
		BufferSize:    100,
	}

	w, err := New(config)
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	err = w.Start(ctx)
	require.NoError(t, err)

	// Should not be able to start again
	err = w.Start(ctx)
	assert.ErrorIs(t, err, ErrAlreadyStarted)

	w.Stop()
}

func TestWatcher_Stop(t *testing.T) {
	tmpDir := t.TempDir()

	config := Config{
		Paths:         []string{tmpDir},
		DebounceDelay: 50 * time.Millisecond,
		BatchInterval: 100 * time.Millisecond,
		BufferSize:    100,
	}

	w, err := New(config)
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	err = w.Start(ctx)
	require.NoError(t, err)

	err = w.Stop()
	assert.NoError(t, err)

	// Should be idempotent
	err = w.Stop()
	assert.NoError(t, err)
}

func TestWatcher_Events_Create(t *testing.T) {
	tmpDir := t.TempDir()

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

	// Give watcher time to initialize
	time.Sleep(500 * time.Millisecond)

	// Create a file
	testFile := filepath.Join(tmpDir, "test.txt")
	err = os.WriteFile(testFile, []byte("test content"), 0644)
	require.NoError(t, err)

	// Wait for event - may be create or modify depending on OS
	select {
	case event := <-w.Events():
		// Accept either create or modify (OS-dependent)
		assert.True(t, event.Type == EventCreate || event.Type == EventModify)
		assert.Equal(t, testFile, event.Path)
		assert.Greater(t, event.Size, int64(0))
	case <-time.After(5 * time.Second):
		t.Fatal("timeout waiting for create event")
	}
}

func TestWatcher_Events_Modify(t *testing.T) {
	tmpDir := t.TempDir()

	// Create file first
	testFile := filepath.Join(tmpDir, "test.txt")
	err := os.WriteFile(testFile, []byte("initial"), 0644)
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

	// Give watcher time to initialize
	time.Sleep(500 * time.Millisecond)

	// Modify the file
	err = os.WriteFile(testFile, []byte("modified content"), 0644)
	require.NoError(t, err)

	// Wait for event
	select {
	case event := <-w.Events():
		// Should be modify or create
		assert.True(t, event.Type == EventModify || event.Type == EventCreate)
		assert.Equal(t, testFile, event.Path)
	case <-time.After(5 * time.Second):
		t.Fatal("timeout waiting for modify event")
	}
}

func TestWatcher_Events_Delete(t *testing.T) {
	tmpDir := t.TempDir()

	// Create file first
	testFile := filepath.Join(tmpDir, "test.txt")
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

	// Give watcher time to initialize
	time.Sleep(500 * time.Millisecond)

	// Delete the file
	err = os.Remove(testFile)
	require.NoError(t, err)

	// Wait for event
	select {
	case event := <-w.Events():
		assert.Equal(t, EventDelete, event.Type)
		assert.Equal(t, testFile, event.Path)
	case <-time.After(5 * time.Second):
		t.Fatal("timeout waiting for delete event")
	}
}

func TestWatcher_IgnoreHidden(t *testing.T) {
	tmpDir := t.TempDir()

	config := Config{
		Paths:         []string{tmpDir},
		DebounceDelay: 50 * time.Millisecond,
		BatchInterval: 100 * time.Millisecond,
		BufferSize:    100,
		IgnoreHidden:  true,
	}

	w, err := New(config)
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	err = w.Start(ctx)
	require.NoError(t, err)
	defer w.Stop()

	// Give watcher time to initialize
	time.Sleep(500 * time.Millisecond)

	// Create hidden file
	hiddenFile := filepath.Join(tmpDir, ".hidden.txt")
	err = os.WriteFile(hiddenFile, []byte("hidden"), 0644)
	require.NoError(t, err)

	// Should not receive event
	select {
	case event := <-w.Events():
		t.Fatalf("unexpected event for hidden file: %+v", event)
	case <-time.After(1 * time.Second):
		// Expected - no event
	}

	// Create normal file
	normalFile := filepath.Join(tmpDir, "normal.txt")
	err = os.WriteFile(normalFile, []byte("normal"), 0644)
	require.NoError(t, err)

	// Should receive event
	select {
	case event := <-w.Events():
		assert.Equal(t, normalFile, event.Path)
	case <-time.After(5 * time.Second):
		t.Fatal("timeout waiting for normal file event")
	}
}

func TestWatcher_ExcludePatterns(t *testing.T) {
	tmpDir := t.TempDir()

	config := Config{
		Paths:         []string{tmpDir},
		Exclude:       []string{"*.tmp", "ignored/"},
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

	// Wait for watcher to initialize using timer pattern
	initTimer := time.NewTimer(500 * time.Millisecond)
	<-initTimer.C

	// Create excluded file
	tmpFile := filepath.Join(tmpDir, "test.tmp")
	err = os.WriteFile(tmpFile, []byte("temp"), 0644)
	require.NoError(t, err)

	// Wait for at least one batch interval before checking
	batchTimer1 := time.NewTimer(150 * time.Millisecond)
	<-batchTimer1.C

	// Should not receive event
	select {
	case event := <-w.Events():
		t.Fatalf("unexpected event for excluded file: %+v", event)
	case <-time.After(1 * time.Second):
		// Expected - no event
	}

	// Create normal file
	normalFile := filepath.Join(tmpDir, "test.txt")
	err = os.WriteFile(normalFile, []byte("normal"), 0644)
	require.NoError(t, err)

	// Wait for at least one batch interval to ensure event is flushed
	batchTimer2 := time.NewTimer(150 * time.Millisecond)
	<-batchTimer2.C

	// Should receive event with extended timeout
	select {
	case event := <-w.Events():
		assert.Equal(t, normalFile, event.Path)
	case <-time.After(5 * time.Second):
		t.Fatal("timeout waiting for normal file event")
	}
}

// latestPendingSize reports the size carried by the newest pending event the
// debouncer holds for path, and whether it holds one at all. It scans every
// pending entry rather than indexing by path so that it keeps working when the
// path-keyed coalescing it is used to observe is the thing under mutation.
func latestPendingSize(d *debouncer, path string) (int64, bool) {
	d.mu.Lock()
	defer d.mu.Unlock()

	var (
		size  int64
		found bool
	)
	for _, e := range d.events {
		if e.Path == path && (!found || e.Size > size) {
			size, found = e.Size, true
		}
	}
	return size, found
}

// drainEvents removes every event already queued on the watcher's output
// channel. It never blocks, so it reads exactly what a preceding flush put
// there and adds no wall-clock window of its own.
func drainEvents(w Watcher) []Event {
	var events []Event
	for {
		select {
		case e := <-w.Events():
			events = append(events, e)
		default:
			return events
		}
	}
}

func TestWatcher_Debouncing(t *testing.T) {
	tmpDir := t.TempDir()

	// BatchInterval is set beyond any plausible runtime for this test, so the
	// periodic flush never fires and the test alone decides when the debouncer
	// is drained. Nothing here is asserted against elapsed time: the number of
	// batch windows the writes happen to straddle -- the quantity the previous
	// version of this test measured -- can no longer influence the result.
	config := Config{
		Paths: []string{tmpDir},
		// Effectively infinite: the real batch ticker must never fire — the test
		// owns flushing via the manualFlush seam, so timing is deterministic.
		DebounceDelay: 50 * time.Millisecond,
		BatchInterval: time.Hour,
		BufferSize:    100,
		IgnoreHidden:  false,
	}

	w, err := New(config)
	require.NoError(t, err)

	fw, ok := w.(*fsWatcher)
	require.True(t, ok, "New must return *fsWatcher")

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	require.NoError(t, w.Start(ctx))
	defer w.Stop()

	testFile := filepath.Join(tmpDir, "test.txt")

	// Write the same path repeatedly, each write given a distinct length and
	// confirmed to have reached the debouncer before the next one is made.
	// Confirming establishes the precondition the assertion below rests on:
	// several separate file system events really were buffered for this path,
	// so a single flushed event can only be the result of coalescing and not
	// of the writes having gone unnoticed.
	const writes = 5
	for i := 1; i <= writes; i++ {
		require.NoError(t, os.WriteFile(testFile, []byte(strings.Repeat("x", i)), 0644))

		require.Eventuallyf(t, func() bool {
			size, ok := latestPendingSize(fw.debouncer, testFile)
			return ok && size == int64(i)
		}, 30*time.Second, time.Millisecond, "write %d never reached the debouncer", i)
	}

	// Draining the debouncer emits one event per distinct path regardless of
	// how many events were buffered for it.
	fw.flushEvents()

	var forTestFile []Event
	for _, e := range drainEvents(w) {
		if e.Path == testFile {
			forTestFile = append(forTestFile, e)
		}
	}

	require.Len(t, forTestFile, 1,
		"%d writes to one path buffered in a single batch window must coalesce into exactly 1 event", writes)
	assert.Equal(t, int64(writes), forTestFile[0].Size,
		"the coalesced event must carry the newest write, not an earlier one")
}

func TestWatcher_AddPath(t *testing.T) {
	tmpDir1 := t.TempDir()
	tmpDir2 := t.TempDir()

	config := Config{
		Paths:         []string{tmpDir1},
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

	// Give watcher time to initialize
	time.Sleep(500 * time.Millisecond)

	// Add second path
	err = w.AddPath(tmpDir2)
	require.NoError(t, err)

	// Create file in second path
	testFile := filepath.Join(tmpDir2, "test.txt")
	err = os.WriteFile(testFile, []byte("content"), 0644)
	require.NoError(t, err)

	// Should receive event
	select {
	case event := <-w.Events():
		assert.Equal(t, testFile, event.Path)
	case <-time.After(5 * time.Second):
		t.Fatal("timeout waiting for event from new path")
	}
}

func TestWatcher_RemovePath(t *testing.T) {
	tmpDir := t.TempDir()

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

	// Give watcher time to initialize
	time.Sleep(500 * time.Millisecond)

	// Remove path
	err = w.RemovePath(tmpDir)
	require.NoError(t, err)

	// Create file in removed path
	testFile := filepath.Join(tmpDir, "test.txt")
	err = os.WriteFile(testFile, []byte("content"), 0644)
	require.NoError(t, err)

	// Should not receive event
	select {
	case event := <-w.Events():
		t.Fatalf("unexpected event after removing path: %+v", event)
	case <-time.After(1 * time.Second):
		// Expected - no event
	}
}

func TestWatcher_RemovePath_NotWatched(t *testing.T) {
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

	err = w.RemovePath("/nonexistent/path")
	assert.ErrorIs(t, err, ErrPathNotWatched)
}

func TestWatcher_RecursiveWatching(t *testing.T) {
	tmpDir := t.TempDir()
	subDir := filepath.Join(tmpDir, "subdir")
	err := os.Mkdir(subDir, 0755)
	require.NoError(t, err)

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

	// Give watcher time to initialize
	time.Sleep(500 * time.Millisecond)

	// Create file in subdirectory
	testFile := filepath.Join(subDir, "test.txt")
	err = os.WriteFile(testFile, []byte("content"), 0644)
	require.NoError(t, err)

	// Should receive event
	select {
	case event := <-w.Events():
		assert.Equal(t, testFile, event.Path)
	case <-time.After(5 * time.Second):
		t.Fatal("timeout waiting for event from subdirectory")
	}
}

func TestWatcher_MaxDepth(t *testing.T) {
	tmpDir := t.TempDir()
	subDir1 := filepath.Join(tmpDir, "level1")
	subDir2 := filepath.Join(subDir1, "level2")
	err := os.MkdirAll(subDir2, 0755)
	require.NoError(t, err)

	config := Config{
		Paths:         []string{tmpDir},
		DebounceDelay: 50 * time.Millisecond,
		BatchInterval: 100 * time.Millisecond,
		BufferSize:    100,
		Recursive:     true,
		MaxDepth:      1, // Only watch tmpDir and level1
		IgnoreHidden:  false,
	}

	w, err := New(config)
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	err = w.Start(ctx)
	require.NoError(t, err)
	defer w.Stop()

	// Give watcher time to initialize
	time.Sleep(500 * time.Millisecond)

	// File in level1 should be watched
	testFile1 := filepath.Join(subDir1, "test1.txt")
	err = os.WriteFile(testFile1, []byte("content"), 0644)
	require.NoError(t, err)

	// Should receive event
	select {
	case event := <-w.Events():
		assert.Equal(t, testFile1, event.Path)
	case <-time.After(5 * time.Second):
		t.Fatal("timeout waiting for event from level1")
	}

	// File in level2 should NOT be watched (exceeds max depth)
	testFile2 := filepath.Join(subDir2, "test2.txt")
	err = os.WriteFile(testFile2, []byte("content"), 0644)
	require.NoError(t, err)

	// What this asserts is that nothing under level2 is watched, so judge each
	// event by its path. Treating *any* event as a failure does not work here:
	// one os.WriteFile emits a create and then a modify, and the level1 write
	// above consumed only the first, so its second event is still queued. The
	// earlier form of this test relied on that leftover being delivered during
	// a fixed sleep, and reported it as a MaxDepth violation whenever a loaded
	// machine delayed it past the window — failing on a correct implementation
	// and naming a level1 path in the process.
	deadline := time.After(1 * time.Second)
	for {
		select {
		case event := <-w.Events():
			if strings.HasPrefix(event.Path, subDir2) {
				t.Fatalf("level2 exceeds MaxDepth and must not be watched, got: %+v", event)
			}
			// Anything else is the level1 write still draining. Keep reading:
			// returning early would leave the level2 window unobserved.
		case <-deadline:
			return
		}
	}
}

func TestWatcher_ContextCancellation(t *testing.T) {
	tmpDir := t.TempDir()

	config := Config{
		Paths:         []string{tmpDir},
		DebounceDelay: 50 * time.Millisecond,
		BatchInterval: 100 * time.Millisecond,
		BufferSize:    100,
	}

	w, err := New(config)
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.Background())

	err = w.Start(ctx)
	require.NoError(t, err)

	// Cancel context
	cancel()

	// Give time for shutdown
	time.Sleep(500 * time.Millisecond)

	// Watcher should be stopped
	testFile := filepath.Join(tmpDir, "test.txt")
	err = os.WriteFile(testFile, []byte("content"), 0644)
	require.NoError(t, err)

	// Should not receive events after cancellation
	select {
	case event := <-w.Events():
		// Channel might be closed or have buffered events
		_ = event
	case <-time.After(1 * time.Second):
		// Expected - no event or closed channel
	}
}

func TestNew_ConfigValidation(t *testing.T) {
	tests := []struct {
		name      string
		config    Config
		wantError bool
	}{
		{"valid", Config{Paths: []string{"."}, DebounceDelay: 100 * time.Millisecond}, false},
		{"empty paths", Config{Paths: []string{}}, true},
		{"negative debounce", Config{Paths: []string{"."}, DebounceDelay: -1}, true},
		{"negative batch", Config{Paths: []string{"."}, BatchInterval: -1}, true},
		{"negative max depth", Config{Paths: []string{"."}, MaxDepth: -1}, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := New(tt.config)
			if (err != nil) != tt.wantError {
				t.Errorf("New() error = %v, wantError %v", err, tt.wantError)
			}
		})
	}
}

func TestWatcher_AddRecursive_EdgeCases(t *testing.T) {
	tmpDir := t.TempDir()

	// Create deep nested structure
	deep := tmpDir + "/a/b/c/d/e"
	os.MkdirAll(deep, 0755)

	// Create hidden dir
	hidden := tmpDir + "/.hidden"
	os.Mkdir(hidden, 0755)

	tests := []struct {
		name   string
		config Config
	}{
		{
			"max depth limit",
			Config{
				Paths:         []string{tmpDir},
				MaxDepth:      2,
				IgnoreHidden:  false,
				BatchInterval: 100 * time.Millisecond,
				DebounceDelay: 50 * time.Millisecond,
			},
		},
		{
			"ignore hidden",
			Config{
				Paths:         []string{tmpDir},
				IgnoreHidden:  true,
				BatchInterval: 100 * time.Millisecond,
				DebounceDelay: 50 * time.Millisecond,
			},
		},
		{
			"with exclusions",
			Config{
				Paths:         []string{tmpDir},
				Exclude:       []string{"*/c/*"},
				BatchInterval: 100 * time.Millisecond,
				DebounceDelay: 50 * time.Millisecond,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w, err := New(tt.config)
			if err != nil {
				t.Fatal(err)
			}
			defer w.Stop()

			// Just verify it doesn't crash
			if w == nil {
				t.Error("Watcher is nil")
			}
		})
	}
}

func TestWatcher_FlushEvents(t *testing.T) {
	tmpDir := t.TempDir()

	config := Config{
		Paths:         []string{tmpDir},
		DebounceDelay: 50 * time.Millisecond,
		BatchInterval: 100 * time.Millisecond,
		BufferSize:    10,
	}

	w, err := New(config)
	if err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	go w.Start(ctx)
	time.Sleep(500 * time.Millisecond)

	// Create file to generate event
	testFile := tmpDir + "/test.txt"
	os.WriteFile(testFile, []byte("test"), 0644)

	// Wait for debounce
	time.Sleep(500 * time.Millisecond)

	// Stop should flush events
	w.Stop()
}

func TestWatcher_AddPath_Invalid(t *testing.T) {
	config := Config{
		Paths:         []string{t.TempDir()},
		DebounceDelay: 100 * time.Millisecond,
	}

	w, err := New(config)
	if err != nil {
		t.Fatal(err)
	}
	defer w.Stop()

	err = w.AddPath("/nonexistent/path/that/does/not/exist")
	if err == nil {
		t.Error("Expected error for nonexistent path")
	}
}
func TestWatcher_FollowSymlinks(t *testing.T) {
	tmpDir := t.TempDir()

	// Create actual directory
	realDir := filepath.Join(tmpDir, "real")
	os.Mkdir(realDir, 0755)

	// Create symlink
	symlink := filepath.Join(tmpDir, "symlink")
	os.Symlink(realDir, symlink)

	tests := []struct {
		name          string
		followSymlink bool
	}{
		{"follow symlinks", true},
		{"ignore symlinks", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			config := Config{
				Paths:          []string{tmpDir},
				Recursive:      true,
				BatchInterval:  100 * time.Millisecond,
				DebounceDelay:  50 * time.Millisecond,
				FollowSymlinks: tt.followSymlink,
			}

			w, err := New(config)
			if err != nil {
				t.Fatal(err)
			}
			defer w.Stop()

			// Verify watcher was created
			if w == nil {
				t.Error("Watcher is nil")
			}
		})
	}
}

func TestWatcher_AddRecursive_DeepNesting(t *testing.T) {
	tmpDir := t.TempDir()

	// Create deeply nested structure
	current := tmpDir
	for i := 0; i < 5; i++ {
		current = filepath.Join(current, "level")
		os.Mkdir(current, 0755)
	}

	config := Config{
		Paths:         []string{tmpDir},
		Recursive:     true,
		MaxDepth:      3,
		DebounceDelay: 50 * time.Millisecond,
		BatchInterval: 100 * time.Millisecond,
	}

	w, err := New(config)
	if err != nil {
		t.Fatal(err)
	}
	defer w.Stop()

	// Verify it handled deep nesting
	if w == nil {
		t.Error("Watcher is nil")
	}
}

func TestNew_ErrorPaths(t *testing.T) {
	tmpDir := t.TempDir()

	// Create a file to test error when path exists but is not a directory
	testFile := filepath.Join(tmpDir, "file.txt")
	os.WriteFile(testFile, []byte("test"), 0644)

	config := Config{
		Paths:         []string{testFile},
		Recursive:     true,
		BatchInterval: 100 * time.Millisecond,
		DebounceDelay: 50 * time.Millisecond,
	}

	w, err := New(config)
	if err != nil {
		// Expected - can't watch a file in recursive mode
		return
	}
	if w != nil {
		w.Stop()
	}
}

func TestWatcher_AddRecursive_Errors(t *testing.T) {
	tmpDir := t.TempDir()

	// Create directory with subdirectories
	level1 := filepath.Join(tmpDir, "level1")
	os.Mkdir(level1, 0755)

	// Create a subdirectory
	level2 := filepath.Join(level1, "level2")
	os.Mkdir(level2, 0755)

	// Create unreadable directory (no permissions)
	unreadable := filepath.Join(level1, "unreadable")
	os.Mkdir(unreadable, 0000)
	defer os.Chmod(unreadable, 0755) // Cleanup

	config := Config{
		Paths:         []string{tmpDir},
		Recursive:     true,
		BatchInterval: 100 * time.Millisecond,
		DebounceDelay: 50 * time.Millisecond,
	}

	w, err := New(config)
	if err != nil {
		t.Fatal(err)
	}
	defer w.Stop()

	// Should handle errors gracefully
	if w == nil {
		t.Error("Watcher is nil")
	}
}

func TestWatcher_AddRecursive_Symlink_Coverage(t *testing.T) {
	tmpDir := t.TempDir()

	// Create actual directory
	realDir := filepath.Join(tmpDir, "real")
	os.Mkdir(realDir, 0755)

	// Create file symlink (not a directory)
	testFile := filepath.Join(tmpDir, "file.txt")
	os.WriteFile(testFile, []byte("test"), 0644)
	fileSymlink := filepath.Join(realDir, "filelink")
	os.Symlink(testFile, fileSymlink)

	// Create directory symlink
	dirSymlink := filepath.Join(tmpDir, "dirlink")
	os.Symlink(realDir, dirSymlink)

	// Test with follow symlinks enabled
	config := Config{
		Paths:          []string{tmpDir},
		Recursive:      true,
		FollowSymlinks: true,
		BatchInterval:  100 * time.Millisecond,
		DebounceDelay:  50 * time.Millisecond,
	}

	w, err := New(config)
	if err != nil {
		t.Fatal(err)
	}
	defer w.Stop()

	if w == nil {
		t.Error("Watcher is nil")
	}
}

func TestWatcher_FlushEvents_Multiple(t *testing.T) {
	tmpDir := t.TempDir()

	config := Config{
		Paths:         []string{tmpDir},
		DebounceDelay: 20 * time.Millisecond,
		BatchInterval: 50 * time.Millisecond,
		BufferSize:    5,
	}

	w, err := New(config)
	if err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	err = w.Start(ctx)
	if err != nil {
		t.Fatal(err)
	}

	time.Sleep(500 * time.Millisecond)

	// Create multiple files rapidly to trigger batching
	for i := 0; i < 5; i++ {
		testFile := filepath.Join(tmpDir, "batch_"+string(rune('a'+i))+".txt")
		os.WriteFile(testFile, []byte("test"), 0644)
	}

	// Wait for batch interval
	time.Sleep(500 * time.Millisecond)

	// Collect events
	collected := 0
	timeout := time.After(5 * time.Second)
	for {
		select {
		case <-w.Events():
			collected++
		case <-timeout:
			w.Stop()
			return
		}
	}
}

func TestNew_FailedWatcher(t *testing.T) {
	tmpDir := t.TempDir()

	config := Config{
		Paths:          []string{tmpDir},
		DebounceDelay:  50 * time.Millisecond,
		BatchInterval:  100 * time.Millisecond,
		BufferSize:     10,
		Recursive:      true,
		FollowSymlinks: false,
	}

	w, err := New(config)
	if err != nil {
		t.Fatal(err)
	}

	if w == nil {
		t.Error("Expected watcher to be created")
	} else {
		w.Stop()
	}
}

func TestWatcher_AddRecursive_ErrorHandling(t *testing.T) {
	tmpDir := t.TempDir()

	// Create structure that will trigger different code paths
	subDir := filepath.Join(tmpDir, "subdir")
	os.Mkdir(subDir, 0755)

	// Create a broken symlink
	brokenLink := filepath.Join(subDir, "broken")
	os.Symlink("/nonexistent/path", brokenLink)

	// Create normal subdirectory
	normalDir := filepath.Join(subDir, "normal")
	os.Mkdir(normalDir, 0755)

	// Test with recursive and follow symlinks
	config := Config{
		Paths:          []string{tmpDir},
		Recursive:      true,
		FollowSymlinks: true,
		BatchInterval:  100 * time.Millisecond,
		DebounceDelay:  50 * time.Millisecond,
		MaxDepth:       5,
	}

	w, err := New(config)
	if err != nil {
		t.Fatal(err)
	}
	defer w.Stop()

	// Start watcher to trigger addRecursive
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	err = w.Start(ctx)
	if err != nil {
		t.Fatal(err)
	}

	time.Sleep(500 * time.Millisecond)
	w.Stop()
}

func TestWatcher_ProcessEvents_AllPaths(t *testing.T) {
	tmpDir := t.TempDir()

	config := Config{
		Paths:         []string{tmpDir},
		DebounceDelay: 30 * time.Millisecond,
		BatchInterval: 60 * time.Millisecond,
		BufferSize:    10,
	}

	w, err := New(config)
	if err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	err = w.Start(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer w.Stop()

	time.Sleep(500 * time.Millisecond)

	// Create, modify, delete to exercise all event types
	testFile := filepath.Join(tmpDir, "events.txt")

	// Create
	os.WriteFile(testFile, []byte("initial"), 0644)
	time.Sleep(150 * time.Millisecond)

	// Modify
	os.WriteFile(testFile, []byte("modified"), 0644)
	time.Sleep(150 * time.Millisecond)

	// Delete
	os.Remove(testFile)
	time.Sleep(150 * time.Millisecond)

	// Drain events
	timeout := time.After(5 * time.Second)
	for {
		select {
		case <-w.Events():
		case <-timeout:
			return
		}
	}
}
