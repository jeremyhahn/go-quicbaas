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

package processor

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// TestValidatePath_NoAllowedDirs tests path validation when no allowed directories are configured.
func TestValidatePath_NoAllowedDirs(t *testing.T) {
	config := DefaultConfig()
	// No AllowedDirs configured
	proc, err := New(config)
	if err != nil {
		t.Fatalf("Failed to create processor: %v", err)
	}
	p := proc.(*processor)

	tests := []struct {
		name    string
		path    string
		wantErr error
	}{
		{
			name:    "simple absolute path",
			path:    "/tmp/test.go",
			wantErr: nil,
		},
		{
			name:    "relative path without traversal",
			path:    "testdata/test.go",
			wantErr: nil,
		},
		{
			// filepath.Clean resolves /tmp/../etc/passwd to /etc/passwd (no .. remaining)
			// so with no AllowedDirs, this is accepted as a valid absolute path
			name:    "absolute path with dot-dot resolves cleanly",
			path:    "/tmp/../etc/passwd",
			wantErr: nil,
		},
		{
			name:    "relative path with dot-dot is rejected",
			path:    "../../../etc/passwd",
			wantErr: ErrPathTraversal,
		},
		{
			name:    "relative path with leading dot-dot",
			path:    "../secret.txt",
			wantErr: ErrPathTraversal,
		},
		{
			name:    "path with redundant separators cleaned",
			path:    "/tmp///test.go",
			wantErr: nil,
		},
		{
			name:    "path with dot segment",
			path:    "/tmp/./test.go",
			wantErr: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := p.validatePath(tt.path)
			if tt.wantErr == nil {
				if err != nil {
					t.Errorf("validatePath(%q) unexpected error: %v", tt.path, err)
				}
				return
			}
			if err == nil {
				t.Errorf("validatePath(%q) expected error %v, got nil", tt.path, tt.wantErr)
				return
			}
			var pathErr *PathValidationError
			if !errors.As(err, &pathErr) {
				t.Errorf("validatePath(%q) expected PathValidationError, got %T: %v", tt.path, err, err)
				return
			}
			if !errors.Is(pathErr.Reason, tt.wantErr) {
				t.Errorf("validatePath(%q) expected reason %v, got %v", tt.path, tt.wantErr, pathErr.Reason)
			}
		})
	}
}

// TestValidatePath_WithAllowedDirs tests path validation with configured allowed directories.
func TestValidatePath_WithAllowedDirs(t *testing.T) {
	tmpDir := t.TempDir()
	allowedDir := filepath.Join(tmpDir, "allowed")
	outsideDir := filepath.Join(tmpDir, "outside")

	if err := os.MkdirAll(allowedDir, 0755); err != nil {
		t.Fatalf("Failed to create allowed dir: %v", err)
	}
	if err := os.MkdirAll(outsideDir, 0755); err != nil {
		t.Fatalf("Failed to create outside dir: %v", err)
	}

	// Create test files
	allowedFile := filepath.Join(allowedDir, "test.go")
	if err := os.WriteFile(allowedFile, []byte("package main"), 0644); err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}
	outsideFile := filepath.Join(outsideDir, "secret.go")
	if err := os.WriteFile(outsideFile, []byte("package secret"), 0644); err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}

	config := DefaultConfig()
	config.AllowedDirs = []string{allowedDir}
	proc, err := New(config)
	if err != nil {
		t.Fatalf("Failed to create processor: %v", err)
	}
	p := proc.(*processor)

	tests := []struct {
		name    string
		path    string
		wantErr error
	}{
		{
			name:    "file within allowed directory",
			path:    allowedFile,
			wantErr: nil,
		},
		{
			name:    "file outside allowed directory",
			path:    outsideFile,
			wantErr: ErrPathNotAllowed,
		},
		{
			name:    "traversal attack targeting outside",
			path:    filepath.Join(allowedDir, "..", "outside", "secret.go"),
			wantErr: ErrPathNotAllowed,
		},
		{
			name:    "subdirectory within allowed",
			path:    filepath.Join(allowedDir, "sub", "file.go"),
			wantErr: nil,
		},
		{
			name:    "absolute path to /etc/passwd",
			path:    "/etc/passwd",
			wantErr: ErrPathNotAllowed,
		},
		{
			// Relative path with .. is resolved to absolute before checking against allowed dirs
			name:    "relative traversal with allowed dirs configured",
			path:    "../../../etc/passwd",
			wantErr: ErrPathNotAllowed,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := p.validatePath(tt.path)
			if tt.wantErr == nil {
				if err != nil {
					t.Errorf("validatePath(%q) unexpected error: %v", tt.path, err)
				}
				return
			}
			if err == nil {
				t.Errorf("validatePath(%q) expected error %v, got nil", tt.path, tt.wantErr)
				return
			}
			var pathErr *PathValidationError
			if !errors.As(err, &pathErr) {
				t.Errorf("validatePath(%q) expected PathValidationError, got %T: %v", tt.path, err, err)
				return
			}
			if !errors.Is(pathErr.Reason, tt.wantErr) {
				t.Errorf("validatePath(%q) expected reason %v, got %v", tt.path, tt.wantErr, pathErr.Reason)
			}
		})
	}
}

// TestProcess_PathTraversalRejected tests that Process rejects relative path traversal attacks.
func TestProcess_PathTraversalRejected(t *testing.T) {
	config := DefaultConfig()
	proc, err := New(config)
	if err != nil {
		t.Fatalf("Failed to create processor: %v", err)
	}

	ctx := context.Background()
	_, err = proc.Process(ctx, "../../../etc/passwd")
	if err == nil {
		t.Error("Expected error for path traversal, got nil")
	}

	var pathErr *PathValidationError
	if !errors.As(err, &pathErr) {
		t.Errorf("Expected PathValidationError, got %T: %v", err, err)
		return
	}
	if !errors.Is(pathErr.Reason, ErrPathTraversal) {
		t.Errorf("Expected ErrPathTraversal reason, got %v", pathErr.Reason)
	}
}

// TestProcess_AllowedDirEnforced tests that Process enforces allowed directory restrictions.
func TestProcess_AllowedDirEnforced(t *testing.T) {
	tmpDir := t.TempDir()
	allowedDir := filepath.Join(tmpDir, "allowed")
	if err := os.MkdirAll(allowedDir, 0755); err != nil {
		t.Fatalf("Failed to create allowed dir: %v", err)
	}

	// Create a file inside the allowed directory
	goFile := filepath.Join(allowedDir, "test.go")
	goContent := `package main

func Hello() {
	println("hello")
}
`
	if err := os.WriteFile(goFile, []byte(goContent), 0644); err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}

	// Create a file outside the allowed directory
	outsideFile := filepath.Join(tmpDir, "outside.go")
	if err := os.WriteFile(outsideFile, []byte(goContent), 0644); err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}

	config := DefaultConfig()
	config.AllowedDirs = []string{allowedDir}
	proc, err := New(config)
	if err != nil {
		t.Fatalf("Failed to create processor: %v", err)
	}

	ctx := context.Background()

	// File inside allowed dir should work
	chunks, err := proc.Process(ctx, goFile)
	if err != nil {
		t.Errorf("Process() for allowed file error = %v", err)
	}
	if len(chunks) == 0 {
		t.Error("Expected chunks for allowed file")
	}

	// File outside allowed dir should be rejected
	_, err = proc.Process(ctx, outsideFile)
	if err == nil {
		t.Error("Expected error for file outside allowed directory")
	}
	var pathErr *PathValidationError
	if !errors.As(err, &pathErr) {
		t.Errorf("Expected PathValidationError, got %T: %v", err, err)
	}
}

// TestProcess_PathCleaningApplied tests that filepath.Clean is applied to normalize paths.
func TestProcess_PathCleaningApplied(t *testing.T) {
	tmpDir := t.TempDir()
	goFile := filepath.Join(tmpDir, "test.go")
	goContent := `package main

func Hello() {
	println("hello")
}
`
	if err := os.WriteFile(goFile, []byte(goContent), 0644); err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}

	config := DefaultConfig()
	config.AllowedDirs = []string{tmpDir}
	proc, err := New(config)
	if err != nil {
		t.Fatalf("Failed to create processor: %v", err)
	}

	ctx := context.Background()

	// Path with dot segments should be cleaned and accepted
	messyPath := filepath.Join(tmpDir, ".", "test.go")
	chunks, err := proc.Process(ctx, messyPath)
	if err != nil {
		t.Errorf("Process() for cleaned path error = %v", err)
	}
	if len(chunks) == 0 {
		t.Error("Expected chunks for cleaned path")
	}
}

// TestProcessBatch_PathTraversalInBatch tests that batch processing rejects relative path traversal.
func TestProcessBatch_PathTraversalInBatch(t *testing.T) {
	tmpDir := t.TempDir()
	validFile := filepath.Join(tmpDir, "valid.go")
	goContent := `package main

func Hello() {
	println("hello")
}
`
	if err := os.WriteFile(validFile, []byte(goContent), 0644); err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}

	config := DefaultConfig()
	proc, err := New(config)
	if err != nil {
		t.Fatalf("Failed to create processor: %v", err)
	}

	ctx := context.Background()

	// Use a relative traversal path that filepath.Clean cannot resolve away
	traversalPath := "../../../etc/passwd"

	chunks, err := proc.ProcessBatch(ctx, []string{validFile, traversalPath})
	// Should have an error from the traversal path
	if err == nil {
		t.Error("Expected error from batch with traversal path")
	}
	// Should still have partial results from valid file
	if len(chunks) == 0 {
		t.Log("No partial results (acceptable depending on goroutine scheduling)")
	}
}

// TestContainsTraversal tests the containsTraversal helper function.
func TestContainsTraversal(t *testing.T) {
	tests := []struct {
		name     string
		path     string
		expected bool
	}{
		{
			name:     "no traversal absolute",
			path:     filepath.Clean("/tmp/test.go"),
			expected: false,
		},
		{
			name:     "no traversal relative",
			path:     filepath.Clean("test.go"),
			expected: false,
		},
		{
			name:     "leading dot-dot",
			path:     filepath.Clean("../test.go"),
			expected: true,
		},
		{
			// filepath.Clean("/a/../../b") resolves to "/b" which has no .. components
			name:     "absolute deep traversal resolves clean",
			path:     filepath.Clean("/a/../../b"),
			expected: false,
		},
		{
			// filepath.Clean resolves /tmp/.. to /
			name:     "dot-dot at end resolves to parent",
			path:     filepath.Clean("/tmp/.."),
			expected: false,
		},
		{
			name:     "current directory",
			path:     filepath.Clean("."),
			expected: false,
		},
		{
			// Relative traversal that cannot be resolved
			name:     "relative multiple dot-dot",
			path:     filepath.Clean("../../secret"),
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := containsTraversal(tt.path)
			if got != tt.expected {
				t.Errorf("containsTraversal(%q) = %v, want %v", tt.path, got, tt.expected)
			}
		})
	}
}

// TestPathValidationError_Error tests the PathValidationError error message.
func TestPathValidationError_Error(t *testing.T) {
	err := &PathValidationError{
		Path:   "/tmp/../etc/passwd",
		Reason: ErrPathTraversal,
	}
	expected := "/tmp/../etc/passwd: path traversal detected"
	if err.Error() != expected {
		t.Errorf("Expected error message %q, got %q", expected, err.Error())
	}
}

// TestPathValidationError_Unwrap tests the PathValidationError unwrap behavior.
func TestPathValidationError_Unwrap(t *testing.T) {
	err := &PathValidationError{
		Path:   "/tmp/test",
		Reason: ErrPathNotAllowed,
	}
	if !errors.Is(err, ErrPathNotAllowed) {
		t.Error("Expected errors.Is to match ErrPathNotAllowed")
	}
}

// TestMultipleAllowedDirs tests path validation with multiple allowed directories.
func TestMultipleAllowedDirs(t *testing.T) {
	tmpDir := t.TempDir()
	dir1 := filepath.Join(tmpDir, "dir1")
	dir2 := filepath.Join(tmpDir, "dir2")
	outsideDir := filepath.Join(tmpDir, "outside")

	for _, d := range []string{dir1, dir2, outsideDir} {
		if err := os.MkdirAll(d, 0755); err != nil {
			t.Fatalf("Failed to create dir %s: %v", d, err)
		}
	}

	config := DefaultConfig()
	config.AllowedDirs = []string{dir1, dir2}
	proc, err := New(config)
	if err != nil {
		t.Fatalf("Failed to create processor: %v", err)
	}
	p := proc.(*processor)

	// File in dir1 should be allowed
	_, err = p.validatePath(filepath.Join(dir1, "test.go"))
	if err != nil {
		t.Errorf("Expected path in dir1 to be allowed: %v", err)
	}

	// File in dir2 should be allowed
	_, err = p.validatePath(filepath.Join(dir2, "test.go"))
	if err != nil {
		t.Errorf("Expected path in dir2 to be allowed: %v", err)
	}

	// File in outsideDir should be rejected
	_, err = p.validatePath(filepath.Join(outsideDir, "test.go"))
	if err == nil {
		t.Error("Expected path in outsideDir to be rejected")
	}
}

// TestValidatePath_AllowedDirsBlocksAbsoluteTraversal tests that allowed dirs
// prevent accessing sibling directories via absolute path traversal.
func TestValidatePath_AllowedDirsBlocksAbsoluteTraversal(t *testing.T) {
	tmpDir := t.TempDir()
	allowedDir := filepath.Join(tmpDir, "allowed")
	secretDir := filepath.Join(tmpDir, "secret")

	if err := os.MkdirAll(allowedDir, 0755); err != nil {
		t.Fatalf("Failed to create allowed dir: %v", err)
	}
	if err := os.MkdirAll(secretDir, 0755); err != nil {
		t.Fatalf("Failed to create secret dir: %v", err)
	}

	config := DefaultConfig()
	config.AllowedDirs = []string{allowedDir}
	proc, err := New(config)
	if err != nil {
		t.Fatalf("Failed to create processor: %v", err)
	}
	p := proc.(*processor)

	// Absolute traversal: filepath.Clean resolves .. but Rel check catches it
	traversalPath := filepath.Join(allowedDir, "..", "secret", "data.txt")
	_, err = p.validatePath(traversalPath)
	if err == nil {
		t.Error("Expected error for traversal via allowed dir parent")
	}
	var pathErr *PathValidationError
	if !errors.As(err, &pathErr) {
		t.Errorf("Expected PathValidationError, got %T: %v", err, err)
		return
	}
	if !errors.Is(pathErr.Reason, ErrPathNotAllowed) {
		t.Errorf("Expected ErrPathNotAllowed, got %v", pathErr.Reason)
	}
}
