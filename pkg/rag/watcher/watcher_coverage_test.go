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
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/fsnotify/fsnotify"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestWatcher_ConvertEvent deterministically exercises every branch of
// convertEvent (create/write/remove/rename/chmod/unknown) without relying on
// real filesystem event timing, which is racy. This guards the op->EventType
// mapping and the chmod/unknown drop behaviour.
func TestWatcher_ConvertEvent(t *testing.T) {
	tmpDir := t.TempDir()
	existing := filepath.Join(tmpDir, "exists.txt")
	require.NoError(t, os.WriteFile(existing, []byte("hello"), 0o644))

	config := Config{
		Paths:         []string{tmpDir},
		DebounceDelay: 10 * time.Millisecond,
		BatchInterval: 20 * time.Millisecond,
		BufferSize:    8,
	}
	w, err := New(config)
	require.NoError(t, err)
	t.Cleanup(func() { assert.NoError(t, w.Stop()) })
	impl := w.(*fsWatcher)

	tests := []struct {
		name     string
		op       fsnotify.Op
		path     string
		wantNil  bool
		wantType EventType
		wantSize int64
	}{
		{"create existing has size", fsnotify.Create, existing, false, EventCreate, 5},
		{"write existing has size", fsnotify.Write, existing, false, EventModify, 5},
		{"remove no stat", fsnotify.Remove, existing, false, EventDelete, 0},
		{"rename treated as delete", fsnotify.Rename, existing, false, EventDelete, 0},
		{"chmod ignored", fsnotify.Chmod, existing, true, 0, 0},
		{"create missing file zero size", fsnotify.Create, filepath.Join(tmpDir, "ghost.txt"), false, EventCreate, 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := impl.convertEvent(fsnotify.Event{Name: tt.path, Op: tt.op})
			if tt.wantNil {
				assert.Nil(t, got)
				return
			}
			require.NotNil(t, got)
			assert.Equal(t, tt.wantType, got.Type)
			assert.Equal(t, tt.path, got.Path)
			assert.Equal(t, tt.wantSize, got.Size)
		})
	}
}

// TestWatcher_ConvertEvent_Unknown verifies that an op with no recognised bits
// is dropped (returns nil).
func TestWatcher_ConvertEvent_Unknown(t *testing.T) {
	config := Config{
		Paths:         []string{t.TempDir()},
		DebounceDelay: 10 * time.Millisecond,
		BatchInterval: 20 * time.Millisecond,
	}
	w, err := New(config)
	require.NoError(t, err)
	t.Cleanup(func() { assert.NoError(t, w.Stop()) })
	impl := w.(*fsWatcher)

	got := impl.convertEvent(fsnotify.Event{Name: "x", Op: fsnotify.Op(0)})
	assert.Nil(t, got)
}

// TestWatcherCauseError_Unwrap verifies that WatcherCauseError exposes its
// wrapped cause via errors.Is/errors.As, which is the contract callers rely on
// when inspecting watcher failures.
func TestWatcherCauseError_Unwrap(t *testing.T) {
	cause := ErrPathNotWatched
	err := &WatcherCauseError{Message: "wrapping", Cause: cause}

	assert.Equal(t, cause, err.Unwrap())
	assert.True(t, errors.Is(err, ErrPathNotWatched))
	assert.Contains(t, err.Error(), "wrapping")
	assert.Contains(t, err.Error(), ErrPathNotWatched.Error())
}

// TestWatcher_Start_AddRecursive_Symlinks exercises addRecursive's symlink
// branches by starting the watcher (which triggers addPath -> addRecursive)
// against a tree containing a directory symlink, a file symlink, and a broken
// symlink with FollowSymlinks enabled. This is the only path that walks the
// EvalSymlinks / os.Stat resolution branches.
func TestWatcher_Start_AddRecursive_Symlinks(t *testing.T) {
	tmpDir := t.TempDir()

	realDir := filepath.Join(tmpDir, "real")
	require.NoError(t, os.Mkdir(realDir, 0o755))
	// nested dir under real so recursion descends past the symlink target
	require.NoError(t, os.Mkdir(filepath.Join(realDir, "nested"), 0o755))

	// directory symlink -> resolves to a directory and should be added
	dirSymlink := filepath.Join(tmpDir, "dirlink")
	require.NoError(t, os.Symlink(realDir, dirSymlink))

	// file symlink -> resolves to a non-directory and should be skipped
	targetFile := filepath.Join(tmpDir, "target.txt")
	require.NoError(t, os.WriteFile(targetFile, []byte("x"), 0o644))
	fileSymlink := filepath.Join(tmpDir, "filelink")
	require.NoError(t, os.Symlink(targetFile, fileSymlink))

	// broken symlink -> EvalSymlinks fails and should be skipped
	brokenSymlink := filepath.Join(tmpDir, "brokenlink")
	require.NoError(t, os.Symlink(filepath.Join(tmpDir, "does-not-exist"), brokenSymlink))

	config := Config{
		Paths:          []string{tmpDir},
		Recursive:      true,
		FollowSymlinks: true,
		DebounceDelay:  10 * time.Millisecond,
		BatchInterval:  20 * time.Millisecond,
		BufferSize:     16,
	}

	w, err := New(config)
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	require.NoError(t, w.Start(ctx))

	// The real directory must be watched. Symlink resolution adds the resolved
	// directory path to the watch set, proving the follow branch executed.
	impl := w.(*fsWatcher)
	impl.mu.RLock()
	_, realWatched := impl.paths[realDir]
	impl.mu.RUnlock()
	assert.True(t, realWatched, "real directory should be watched")

	cancel()
	require.NoError(t, w.Stop())
}

// TestWatcher_Start_AddRecursive_NoFollowSymlink verifies that with
// FollowSymlinks disabled the directory symlink is skipped (the continue branch)
// rather than resolved.
func TestWatcher_Start_AddRecursive_NoFollowSymlink(t *testing.T) {
	tmpDir := t.TempDir()

	realDir := filepath.Join(tmpDir, "real")
	require.NoError(t, os.Mkdir(realDir, 0o755))
	dirSymlink := filepath.Join(tmpDir, "dirlink")
	require.NoError(t, os.Symlink(realDir, dirSymlink))

	config := Config{
		Paths:          []string{tmpDir},
		Recursive:      true,
		FollowSymlinks: false,
		DebounceDelay:  10 * time.Millisecond,
		BatchInterval:  20 * time.Millisecond,
		BufferSize:     16,
	}

	w, err := New(config)
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	require.NoError(t, w.Start(ctx))

	impl := w.(*fsWatcher)
	impl.mu.RLock()
	_, symlinkWatched := impl.paths[dirSymlink]
	impl.mu.RUnlock()
	assert.False(t, symlinkWatched, "symlink path should not be watched when FollowSymlinks is false")

	cancel()
	require.NoError(t, w.Stop())
}

// TestWatcher_Start_AddRecursive_ExcludeAndHidden exercises the exclusion and
// hidden-directory skip branches inside addRecursive by starting the watcher
// against a tree that includes an excluded dir and a hidden dir.
func TestWatcher_Start_AddRecursive_ExcludeAndHidden(t *testing.T) {
	tmpDir := t.TempDir()

	require.NoError(t, os.Mkdir(filepath.Join(tmpDir, "keep"), 0o755))
	require.NoError(t, os.Mkdir(filepath.Join(tmpDir, "node_modules"), 0o755))
	require.NoError(t, os.Mkdir(filepath.Join(tmpDir, ".hidden"), 0o755))

	config := Config{
		Paths:         []string{tmpDir},
		Recursive:     true,
		IgnoreHidden:  true,
		Exclude:       []string{"node_modules/"},
		DebounceDelay: 10 * time.Millisecond,
		BatchInterval: 20 * time.Millisecond,
		BufferSize:    16,
	}

	w, err := New(config)
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	require.NoError(t, w.Start(ctx))

	impl := w.(*fsWatcher)
	impl.mu.RLock()
	_, keepWatched := impl.paths[filepath.Join(tmpDir, "keep")]
	_, excludedWatched := impl.paths[filepath.Join(tmpDir, "node_modules")]
	_, hiddenWatched := impl.paths[filepath.Join(tmpDir, ".hidden")]
	impl.mu.RUnlock()

	assert.True(t, keepWatched, "regular subdir should be watched")
	assert.False(t, excludedWatched, "excluded subdir must be skipped")
	assert.False(t, hiddenWatched, "hidden subdir must be skipped")

	cancel()
	require.NoError(t, w.Stop())
}
