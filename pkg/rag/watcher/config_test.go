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
	"reflect"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDefaultConfig(t *testing.T) {
	config := DefaultConfig()

	assert.NotNil(t, config.Exclude)
	assert.Equal(t, 1*time.Second, config.BatchInterval)
	assert.True(t, config.IgnoreHidden)
	assert.False(t, config.FollowSymlinks)
	assert.Equal(t, 0, config.MaxDepth)
	assert.Equal(t, 1000, config.BufferSize)
	assert.True(t, config.Recursive)
}

// TestConfig_NoDebounceDelayField guards #549: Config once carried a
// DebounceDelay that was validated and documented but never read, so a
// caller tuning it changed nothing. BatchInterval is the one coalescing
// window; no knob that configures nothing comes back.
func TestConfig_NoDebounceDelayField(t *testing.T) {
	_, ok := reflect.TypeOf(Config{}).FieldByName("DebounceDelay")
	assert.False(t, ok, "Config.DebounceDelay configures nothing; BatchInterval is the coalescing window")
}

func TestDefaultExclusions(t *testing.T) {
	exclusions := DefaultExclusions()

	assert.NotEmpty(t, exclusions)
	assert.Contains(t, exclusions, ".git/")
	assert.Contains(t, exclusions, "node_modules/")
	assert.Contains(t, exclusions, "*.pyc")
}

func TestConfig_Validate(t *testing.T) {
	tests := []struct {
		name      string
		config    Config
		expectErr error
	}{
		{
			name: "valid config",
			config: Config{
				Paths:         []string{"/tmp"},
				BatchInterval: 1 * time.Second,
				BufferSize:    1000,
				MaxDepth:      10,
			},
			expectErr: nil,
		},
		{
			name: "no paths",
			config: Config{
				Paths: []string{},
			},
			expectErr: ErrNoPaths,
		},
		{
			name: "negative batch interval",
			config: Config{
				Paths:         []string{"/tmp"},
				BatchInterval: -1 * time.Second,
			},
			expectErr: ErrInvalidBatchInterval,
		},
		{
			name: "negative max depth",
			config: Config{
				Paths:    []string{"/tmp"},
				MaxDepth: -1,
			},
			expectErr: ErrInvalidMaxDepth,
		},
		{
			name: "zero buffer size gets defaulted",
			config: Config{
				Paths:      []string{"/tmp"},
				BufferSize: 0,
			},
			expectErr: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.config.Validate()

			if tt.expectErr != nil {
				assert.ErrorIs(t, err, tt.expectErr)
			} else {
				assert.NoError(t, err)

				// Check that buffer size was defaulted if needed
				if tt.config.BufferSize == 0 {
					assert.Equal(t, 1000, tt.config.BufferSize)
				}
			}
		})
	}
}

func TestConfig_ValidateCorrectDefaults(t *testing.T) {
	config := Config{
		Paths:      []string{"/tmp"},
		BufferSize: -100, // Invalid, should be corrected
	}

	err := config.Validate()
	require.NoError(t, err)
	assert.Equal(t, 1000, config.BufferSize)
}

func TestNew_ValidConfig_Coverage(t *testing.T) {
	tmpDir := t.TempDir()

	config := Config{
		Paths:         []string{tmpDir},
		BufferSize:    100,
		BatchInterval: 100 * time.Millisecond,
	}

	w, err := New(config)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if w == nil {
		t.Fatal("expected non-nil watcher")
	}

	if err := w.Stop(); err != nil {
		t.Errorf("stop error: %v", err)
	}
}

// TestFsWatcher_Stop_NotStarted tests Stop when not started.
func TestFsWatcher_Stop_NotStarted(t *testing.T) {
	tmpDir := t.TempDir()

	config := Config{
		Paths:         []string{tmpDir},
		BufferSize:    100,
		BatchInterval: 100 * time.Millisecond,
	}

	w, err := New(config)
	if err != nil {
		t.Fatalf("failed to create watcher: %v", err)
	}

	// Stop without starting should return nil
	if err := w.Stop(); err != nil {
		t.Errorf("expected nil error when stopping non-started watcher, got: %v", err)
	}
}

// TestFsWatcher_StartAndStop tests start and stop lifecycle.
func TestFsWatcher_StartAndStop(t *testing.T) {
	tmpDir := t.TempDir()

	config := Config{
		Paths:         []string{tmpDir},
		BufferSize:    100,
		BatchInterval: 100 * time.Millisecond,
	}

	w, err := New(config)
	if err != nil {
		t.Fatalf("failed to create watcher: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := w.Start(ctx); err != nil {
		t.Fatalf("failed to start: %v", err)
	}

	// Try to start again - should fail
	if err := w.Start(ctx); err != ErrAlreadyStarted {
		t.Errorf("expected ErrAlreadyStarted, got: %v", err)
	}

	if err := w.Stop(); err != nil {
		t.Errorf("stop error: %v", err)
	}
}

// TestFsWatcher_RemovePath_Coverage tests removing a watched path.
func TestFsWatcher_RemovePath_Coverage(t *testing.T) {
	tmpDir := t.TempDir()

	config := Config{
		Paths:         []string{tmpDir},
		BufferSize:    100,
		BatchInterval: 100 * time.Millisecond,
	}

	w, err := New(config)
	if err != nil {
		t.Fatalf("failed to create watcher: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := w.Start(ctx); err != nil {
		t.Fatalf("failed to start: %v", err)
	}

	// Remove the path
	if err := w.RemovePath(tmpDir); err != nil {
		t.Errorf("RemovePath error: %v", err)
	}

	// Removing again should return ErrPathNotWatched
	err = w.RemovePath(tmpDir)
	if !errors.Is(err, ErrPathNotWatched) {
		t.Errorf("expected ErrPathNotWatched, got: %v", err)
	}

	if err := w.Stop(); err != nil {
		t.Errorf("stop error: %v", err)
	}
}

// TestFsWatcher_AddPath_NonExistent tests adding a non-existent path.
func TestFsWatcher_AddPath_NonExistent(t *testing.T) {
	tmpDir := t.TempDir()

	config := Config{
		Paths:         []string{tmpDir},
		BufferSize:    100,
		BatchInterval: 100 * time.Millisecond,
	}

	w, err := New(config)
	if err != nil {
		t.Fatalf("failed to create watcher: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := w.Start(ctx); err != nil {
		t.Fatalf("failed to start: %v", err)
	}

	// Add non-existent path
	err = w.AddPath("/non/existent/path")
	if err == nil {
		t.Error("expected error for non-existent path")
	}

	if err := w.Stop(); err != nil {
		t.Errorf("stop error: %v", err)
	}
}

// TestAddRecursive_ExcludedPath_Coverage tests that excluded paths are skipped.
func TestAddRecursive_ExcludedPath_Coverage(t *testing.T) {
	tmpDir := t.TempDir()

	// Create a .git directory (commonly excluded)
	gitDir := filepath.Join(tmpDir, ".git")
	if err := os.Mkdir(gitDir, 0o755); err != nil {
		t.Fatalf("failed to create .git dir: %v", err)
	}

	config := Config{
		Paths:         []string{tmpDir},
		BufferSize:    100,
		BatchInterval: 100 * time.Millisecond,
		Exclude:       []string{".git/"},
		Recursive:     true,
		IgnoreHidden:  true,
	}

	w, err := New(config)
	if err != nil {
		t.Fatalf("failed to create watcher: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := w.Start(ctx); err != nil {
		t.Fatalf("failed to start: %v", err)
	}

	// .git should be excluded (not watched)
	fw := w.(*fsWatcher)
	fw.mu.Lock()
	_, watched := fw.paths[gitDir]
	fw.mu.Unlock()

	if watched {
		t.Error("expected .git to be excluded from watched paths")
	}

	if err := w.Stop(); err != nil {
		t.Errorf("stop error: %v", err)
	}
}

// TestFsWatcher_ContextCancellation tests cleanup on context cancellation.
func TestFsWatcher_ContextCancellation(t *testing.T) {
	tmpDir := t.TempDir()

	config := Config{
		Paths:         []string{tmpDir},
		BufferSize:    100,
		BatchInterval: 100 * time.Millisecond,
	}

	w, err := New(config)
	if err != nil {
		t.Fatalf("failed to create watcher: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())

	if err := w.Start(ctx); err != nil {
		t.Fatalf("failed to start: %v", err)
	}

	// Cancel context to trigger cleanup
	cancel()

	// Stop should still work after context cancellation
	_ = w.Stop()
}

// TestFsWatcher_Events_Channel tests the Events channel.
func TestFsWatcher_Events_Channel(t *testing.T) {
	tmpDir := t.TempDir()

	config := Config{
		Paths:         []string{tmpDir},
		BufferSize:    100,
		BatchInterval: 100 * time.Millisecond,
	}

	w, err := New(config)
	if err != nil {
		t.Fatalf("failed to create watcher: %v", err)
	}

	// Events channel should be available
	events := w.Events()
	if events == nil {
		t.Fatal("expected non-nil events channel")
	}

	if err := w.Stop(); err != nil {
		t.Errorf("stop error: %v", err)
	}
}

// TestNew_WithGitIgnorePatterns tests New with gitignore patterns.
func TestNew_WithGitIgnorePatterns(t *testing.T) {
	tmpDir := t.TempDir()

	config := Config{
		Paths:         []string{tmpDir},
		BufferSize:    100,
		BatchInterval: 100 * time.Millisecond,
		Exclude:       []string{"*.log", "node_modules/", ".git/"},
	}

	w, err := New(config)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if err := w.Stop(); err != nil {
		t.Errorf("stop error: %v", err)
	}
}

// TestFsWatcher_AddPath_ExistingPath tests adding a path that's already watched.
func TestFsWatcher_AddPath_ExistingPath(t *testing.T) {
	tmpDir := t.TempDir()

	config := Config{
		Paths:         []string{tmpDir},
		BufferSize:    100,
		BatchInterval: 100 * time.Millisecond,
	}

	w, err := New(config)
	if err != nil {
		t.Fatalf("failed to create watcher: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := w.Start(ctx); err != nil {
		t.Fatalf("failed to start: %v", err)
	}

	// Add the same path again - should be a no-op
	if err := w.AddPath(tmpDir); err != nil {
		t.Errorf("AddPath error: %v", err)
	}

	if err := w.Stop(); err != nil {
		t.Errorf("stop error: %v", err)
	}
}

// TestFsWatcher_AddPath_File tests adding a file (not directory) path.
func TestFsWatcher_AddPath_File(t *testing.T) {
	tmpDir := t.TempDir()
	tmpFile := filepath.Join(tmpDir, "test.txt")

	// Create a file
	f, err := os.Create(tmpFile)
	if err != nil {
		t.Fatalf("failed to create file: %v", err)
	}
	f.Close()

	config := Config{
		Paths:         []string{tmpDir},
		BufferSize:    100,
		BatchInterval: 100 * time.Millisecond,
	}

	w, err := New(config)
	if err != nil {
		t.Fatalf("failed to create watcher: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := w.Start(ctx); err != nil {
		t.Fatalf("failed to start: %v", err)
	}

	// Adding a file (not directory) - fsnotify allows watching files too
	// but our addRecursive only processes directories
	err = w.AddPath(tmpFile)
	// The result depends on implementation - just ensure it doesn't panic
	_ = err

	if err := w.Stop(); err != nil {
		t.Errorf("stop error: %v", err)
	}
}

// TestFsWatcher_RemovePath_NotStarted tests RemovePath when watcher is not started.
func TestFsWatcher_RemovePath_NotStarted(t *testing.T) {
	tmpDir := t.TempDir()

	config := Config{
		Paths:         []string{tmpDir},
		BufferSize:    100,
		BatchInterval: 100 * time.Millisecond,
	}

	w, err := New(config)
	if err != nil {
		t.Fatalf("failed to create watcher: %v", err)
	}

	// RemovePath when not started - path won't be tracked yet
	err = w.RemovePath(tmpDir)
	if !errors.Is(err, ErrPathNotWatched) {
		t.Errorf("expected ErrPathNotWatched, got: %v", err)
	}

	if err := w.Stop(); err != nil {
		t.Errorf("stop error: %v", err)
	}
}

// TestFsWatcher_AddPath_NewSubdirectory tests adding a new subdirectory.
func TestFsWatcher_AddPath_NewSubdirectory(t *testing.T) {
	tmpDir := t.TempDir()
	subDir := filepath.Join(tmpDir, "subdir")

	config := Config{
		Paths:         []string{tmpDir},
		BufferSize:    100,
		BatchInterval: 100 * time.Millisecond,
		Recursive:     true,
	}

	w, err := New(config)
	if err != nil {
		t.Fatalf("failed to create watcher: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := w.Start(ctx); err != nil {
		t.Fatalf("failed to start: %v", err)
	}

	// Create a new subdirectory
	if err := os.Mkdir(subDir, 0o755); err != nil {
		t.Fatalf("failed to create subdir: %v", err)
	}

	// Add the new subdirectory
	if err := w.AddPath(subDir); err != nil {
		t.Errorf("AddPath error: %v", err)
	}

	if err := w.Stop(); err != nil {
		t.Errorf("stop error: %v", err)
	}
}

// TestConfig_Validate_Coverage tests Config validation edge cases.
func TestConfig_Validate_Coverage(t *testing.T) {
	tmpDir := t.TempDir()

	tests := []struct {
		name    string
		config  Config
		wantErr bool
	}{
		{
			name: "valid config",
			config: Config{
				Paths:         []string{tmpDir},
				BufferSize:    100,
				BatchInterval: 100 * time.Millisecond,
			},
			wantErr: false,
		},
		{
			name: "empty paths",
			config: Config{
				Paths:         []string{},
				BufferSize:    100,
				BatchInterval: 100 * time.Millisecond,
			},
			wantErr: true,
		},
		{
			name: "zero buffer size gets auto-corrected",
			config: Config{
				Paths:         []string{tmpDir},
				BufferSize:    0,
				BatchInterval: 100 * time.Millisecond,
			},
			wantErr: false, // Gets auto-corrected to 1000
		},
		{
			name: "negative batch interval",
			config: Config{
				Paths:         []string{tmpDir},
				BufferSize:    100,
				BatchInterval: -1 * time.Millisecond,
			},
			wantErr: true,
		},
		{
			name: "negative max depth",
			config: Config{
				Paths:         []string{tmpDir},
				BufferSize:    100,
				BatchInterval: 100 * time.Millisecond,
				MaxDepth:      -1,
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.config.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

// TestDefaultConfig_Coverage tests DefaultConfig values.
func TestDefaultConfig_Coverage(t *testing.T) {
	config := DefaultConfig()

	if config.BufferSize <= 0 {
		t.Errorf("expected positive BufferSize, got %d", config.BufferSize)
	}

	if config.BatchInterval <= 0 {
		t.Errorf("expected positive BatchInterval, got %v", config.BatchInterval)
	}
}

// TestDefaultExclusions_Coverage tests DefaultExclusions returns expected patterns.
func TestDefaultExclusions_Coverage(t *testing.T) {
	exclusions := DefaultExclusions()

	if len(exclusions) == 0 {
		t.Error("expected non-empty default exclusions")
	}

	// Check for some expected patterns
	hasGit := false
	hasNodeModules := false
	for _, pattern := range exclusions {
		if pattern == ".git/" {
			hasGit = true
		}
		if pattern == "node_modules/" {
			hasNodeModules = true
		}
	}

	if !hasGit {
		t.Error("expected .git/ in default exclusions")
	}
	if !hasNodeModules {
		t.Error("expected node_modules/ in default exclusions")
	}
}
