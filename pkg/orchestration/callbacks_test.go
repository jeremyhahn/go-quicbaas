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

package orchestration

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestCallbackHandler_ReadFile_Allowed(t *testing.T) {
	dir := t.TempDir()
	testFile := filepath.Join(dir, "test.txt")
	if err := os.WriteFile(testFile, []byte("hello world"), 0644); err != nil {
		t.Fatal(err)
	}

	handler := NewCallbackHandler([]string{dir})

	ctx := context.Background()
	result, err := handler.HandleCallback(ctx, &ToolCall{
		ID:   "call-1",
		Name: "fs/read_text_file",
		Arguments: map[string]any{
			"path": testFile,
		},
	})
	if err != nil {
		t.Fatalf("HandleCallback() error = %v", err)
	}
	if result.IsError {
		t.Fatalf("unexpected error result: %s", result.Content)
	}
	if result.Content != "hello world" {
		t.Errorf("Content = %q, want %q", result.Content, "hello world")
	}
	if result.ToolCallID != "call-1" {
		t.Errorf("ToolCallID = %q, want %q", result.ToolCallID, "call-1")
	}
}

func TestCallbackHandler_ReadFile_Denied(t *testing.T) {
	allowedDir := t.TempDir()
	deniedDir := t.TempDir()
	testFile := filepath.Join(deniedDir, "secret.txt")
	if err := os.WriteFile(testFile, []byte("secret"), 0644); err != nil {
		t.Fatal(err)
	}

	handler := NewCallbackHandler([]string{allowedDir})

	ctx := context.Background()
	result, err := handler.HandleCallback(ctx, &ToolCall{
		ID:   "call-2",
		Name: "fs/read_text_file",
		Arguments: map[string]any{
			"path": testFile,
		},
	})
	if err != nil {
		t.Fatalf("HandleCallback() error = %v", err)
	}
	if !result.IsError {
		t.Error("expected error result for denied path")
	}
	if result.Content != "access denied: path outside allowed directories" {
		t.Errorf("Content = %q", result.Content)
	}
}

func TestCallbackHandler_WriteFile_Allowed(t *testing.T) {
	dir := t.TempDir()
	testFile := filepath.Join(dir, "output.txt")

	handler := NewCallbackHandler([]string{dir})

	ctx := context.Background()
	result, err := handler.HandleCallback(ctx, &ToolCall{
		ID:   "call-3",
		Name: "fs/write_text_file",
		Arguments: map[string]any{
			"path":    testFile,
			"content": "written content",
		},
	})
	if err != nil {
		t.Fatalf("HandleCallback() error = %v", err)
	}
	if result.IsError {
		t.Fatalf("unexpected error result: %s", result.Content)
	}

	// Verify file was written
	data, err := os.ReadFile(testFile)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if string(data) != "written content" {
		t.Errorf("file content = %q, want %q", string(data), "written content")
	}
}

func TestCallbackHandler_WriteFile_Denied(t *testing.T) {
	allowedDir := t.TempDir()
	deniedDir := t.TempDir()
	testFile := filepath.Join(deniedDir, "output.txt")

	handler := NewCallbackHandler([]string{allowedDir})

	ctx := context.Background()
	result, err := handler.HandleCallback(ctx, &ToolCall{
		ID:   "call-4",
		Name: "fs/write_text_file",
		Arguments: map[string]any{
			"path":    testFile,
			"content": "should not be written",
		},
	})
	if err != nil {
		t.Fatalf("HandleCallback() error = %v", err)
	}
	if !result.IsError {
		t.Error("expected error result for denied path")
	}

	// Verify file was NOT written
	if _, err := os.Stat(testFile); !os.IsNotExist(err) {
		t.Error("file should not have been created")
	}
}

func TestCallbackHandler_IsPathAllowed(t *testing.T) {
	dir := t.TempDir()
	handler := NewCallbackHandler([]string{dir})

	tests := []struct {
		name    string
		path    string
		allowed bool
	}{
		{
			name:    "ExactDir",
			path:    dir,
			allowed: true,
		},
		{
			name:    "FileInDir",
			path:    filepath.Join(dir, "file.txt"),
			allowed: true,
		},
		{
			name:    "SubdirFile",
			path:    filepath.Join(dir, "sub", "file.txt"),
			allowed: true,
		},
		{
			name:    "ParentTraversal",
			path:    filepath.Join(dir, "..", "escape.txt"),
			allowed: false,
		},
		{
			name:    "DoubleParentTraversal",
			path:    filepath.Join(dir, "..", "..", "escape.txt"),
			allowed: false,
		},
		{
			name:    "AbsoluteOutsidePath",
			path:    "/etc/passwd",
			allowed: false,
		},
		{
			name:    "DotInMiddle",
			path:    filepath.Join(dir, "sub", ".", "file.txt"),
			allowed: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := handler.isPathAllowed(tc.path)
			if got != tc.allowed {
				t.Errorf("isPathAllowed(%q) = %v, want %v", tc.path, got, tc.allowed)
			}
		})
	}
}

func TestCallbackHandler_IsPathAllowed_Symlink(t *testing.T) {
	allowedDir := t.TempDir()
	outsideDir := t.TempDir()

	// Create a symlink inside allowedDir pointing outside
	symlink := filepath.Join(allowedDir, "escape-link")
	if err := os.Symlink(outsideDir, symlink); err != nil {
		t.Skipf("cannot create symlink: %v", err)
	}

	handler := NewCallbackHandler([]string{allowedDir})

	// A path through the symlink should resolve outside the allowed dir
	escapePath := filepath.Join(symlink, "file.txt")
	if handler.isPathAllowed(escapePath) {
		t.Error("symlink escape should be denied")
	}
}

func TestCallbackHandler_HandleCallback_UnknownTool(t *testing.T) {
	handler := NewCallbackHandler(nil)

	ctx := context.Background()
	result, err := handler.HandleCallback(ctx, &ToolCall{
		ID:        "call-5",
		Name:      "unknown/tool",
		Arguments: map[string]any{},
	})
	if err != nil {
		t.Fatalf("HandleCallback() error = %v", err)
	}
	if !result.IsError {
		t.Error("expected error result for unknown tool")
	}
	if result.Content != "unknown tool: unknown/tool" {
		t.Errorf("Content = %q", result.Content)
	}
}

func TestCallbackHandler_HandleCallback_NilToolCall(t *testing.T) {
	handler := NewCallbackHandler(nil)

	ctx := context.Background()
	_, err := handler.HandleCallback(ctx, nil)
	if err == nil {
		t.Fatal("HandleCallback(nil) should return error")
	}
	if !errors.Is(err, ErrInvalidInput) {
		t.Errorf("error = %v, want ErrInvalidInput", err)
	}
}

func TestCallbackHandler_HandleCallback_InvalidArguments(t *testing.T) {
	handler := NewCallbackHandler(nil)

	ctx := context.Background()
	result, err := handler.HandleCallback(ctx, &ToolCall{
		ID:        "call-6",
		Name:      "fs/read_text_file",
		Arguments: "not a map",
	})
	if err != nil {
		t.Fatalf("HandleCallback() error = %v", err)
	}
	if !result.IsError {
		t.Error("expected error result for invalid arguments")
	}
}

func TestCallbackHandler_RequestPermission(t *testing.T) {
	dir := t.TempDir()
	handler := NewCallbackHandler([]string{dir})

	t.Run("AllowedPath", func(t *testing.T) {
		ctx := context.Background()
		result, err := handler.HandleCallback(ctx, &ToolCall{
			ID:   "call-7",
			Name: "session/request_permission",
			Arguments: map[string]any{
				"path": filepath.Join(dir, "file.txt"),
			},
		})
		if err != nil {
			t.Fatalf("HandleCallback() error = %v", err)
		}
		if result.IsError {
			t.Errorf("expected approved, got error: %s", result.Content)
		}
		if result.Content != "approved" {
			t.Errorf("Content = %q, want %q", result.Content, "approved")
		}
	})

	t.Run("DeniedPath", func(t *testing.T) {
		ctx := context.Background()
		result, err := handler.HandleCallback(ctx, &ToolCall{
			ID:   "call-8",
			Name: "session/request_permission",
			Arguments: map[string]any{
				"path": "/etc/passwd",
			},
		})
		if err != nil {
			t.Fatalf("HandleCallback() error = %v", err)
		}
		if !result.IsError {
			t.Error("expected denied for outside path")
		}
	})

	t.Run("NoPath", func(t *testing.T) {
		ctx := context.Background()
		result, err := handler.HandleCallback(ctx, &ToolCall{
			ID:        "call-9",
			Name:      "session/request_permission",
			Arguments: map[string]any{},
		})
		if err != nil {
			t.Fatalf("HandleCallback() error = %v", err)
		}
		if result.IsError {
			t.Errorf("expected approved when no path, got error: %s", result.Content)
		}
		if result.Content != "approved" {
			t.Errorf("Content = %q, want %q", result.Content, "approved")
		}
	})
}

func TestCallbackHandler_ReadFile_MissingPath(t *testing.T) {
	handler := NewCallbackHandler(nil)

	ctx := context.Background()
	result, err := handler.HandleCallback(ctx, &ToolCall{
		ID:        "call-10",
		Name:      "fs/read_text_file",
		Arguments: map[string]any{},
	})
	if err != nil {
		t.Fatalf("HandleCallback() error = %v", err)
	}
	if !result.IsError {
		t.Error("expected error for missing path")
	}
}

func TestCallbackHandler_WriteFile_MissingContent(t *testing.T) {
	dir := t.TempDir()
	handler := NewCallbackHandler([]string{dir})

	ctx := context.Background()
	result, err := handler.HandleCallback(ctx, &ToolCall{
		ID:   "call-11",
		Name: "fs/write_text_file",
		Arguments: map[string]any{
			"path": filepath.Join(dir, "file.txt"),
		},
	})
	if err != nil {
		t.Fatalf("HandleCallback() error = %v", err)
	}
	if !result.IsError {
		t.Error("expected error for missing content")
	}
}

func TestCallbackHandler_ReadFile_NonexistentFile(t *testing.T) {
	dir := t.TempDir()
	handler := NewCallbackHandler([]string{dir})

	ctx := context.Background()
	result, err := handler.HandleCallback(ctx, &ToolCall{
		ID:   "call-12",
		Name: "fs/read_text_file",
		Arguments: map[string]any{
			"path": filepath.Join(dir, "nonexistent.txt"),
		},
	})
	if err != nil {
		t.Fatalf("HandleCallback() error = %v", err)
	}
	if !result.IsError {
		t.Error("expected error for nonexistent file")
	}
}

func TestCallbackHandler_ReadFile_InvalidPathType(t *testing.T) {
	handler := NewCallbackHandler(nil)

	ctx := context.Background()
	result, err := handler.HandleCallback(ctx, &ToolCall{
		ID:   "call-13",
		Name: "fs/read_text_file",
		Arguments: map[string]any{
			"path": 12345,
		},
	})
	if err != nil {
		t.Fatalf("HandleCallback() error = %v", err)
	}
	if !result.IsError {
		t.Error("expected error for non-string path")
	}
	if result.Content != "path must be a string" {
		t.Errorf("Content = %q", result.Content)
	}
}

func TestCallbackHandler_WriteFile_MissingPath(t *testing.T) {
	handler := NewCallbackHandler(nil)

	ctx := context.Background()
	result, err := handler.HandleCallback(ctx, &ToolCall{
		ID:   "call-14",
		Name: "fs/write_text_file",
		Arguments: map[string]any{
			"content": "data",
		},
	})
	if err != nil {
		t.Fatalf("HandleCallback() error = %v", err)
	}
	if !result.IsError {
		t.Error("expected error for missing path")
	}
	if result.Content != "missing required argument: path" {
		t.Errorf("Content = %q", result.Content)
	}
}

func TestCallbackHandler_WriteFile_InvalidPathType(t *testing.T) {
	handler := NewCallbackHandler(nil)

	ctx := context.Background()
	result, err := handler.HandleCallback(ctx, &ToolCall{
		ID:   "call-15",
		Name: "fs/write_text_file",
		Arguments: map[string]any{
			"path":    42,
			"content": "data",
		},
	})
	if err != nil {
		t.Fatalf("HandleCallback() error = %v", err)
	}
	if !result.IsError {
		t.Error("expected error for non-string path")
	}
	if result.Content != "path must be a string" {
		t.Errorf("Content = %q", result.Content)
	}
}

func TestCallbackHandler_WriteFile_InvalidContentType(t *testing.T) {
	dir := t.TempDir()
	handler := NewCallbackHandler([]string{dir})

	ctx := context.Background()
	result, err := handler.HandleCallback(ctx, &ToolCall{
		ID:   "call-16",
		Name: "fs/write_text_file",
		Arguments: map[string]any{
			"path":    filepath.Join(dir, "file.txt"),
			"content": 12345,
		},
	})
	if err != nil {
		t.Fatalf("HandleCallback() error = %v", err)
	}
	if !result.IsError {
		t.Error("expected error for non-string content")
	}
	if result.Content != "content must be a string" {
		t.Errorf("Content = %q", result.Content)
	}
}

func TestCallbackHandler_RequestPermission_InvalidPathType(t *testing.T) {
	handler := NewCallbackHandler(nil)

	ctx := context.Background()
	result, err := handler.HandleCallback(ctx, &ToolCall{
		ID:   "call-17",
		Name: "session/request_permission",
		Arguments: map[string]any{
			"path": 12345,
		},
	})
	if err != nil {
		t.Fatalf("HandleCallback() error = %v", err)
	}
	if !result.IsError {
		t.Error("expected error for non-string path")
	}
	if result.Content != "path must be a string" {
		t.Errorf("Content = %q", result.Content)
	}
}

func TestCallbackHandler_WriteFile_WriteError(t *testing.T) {
	dir := t.TempDir()
	handler := NewCallbackHandler([]string{dir})

	// Write to a path inside a nonexistent subdirectory
	ctx := context.Background()
	result, err := handler.HandleCallback(ctx, &ToolCall{
		ID:   "call-18",
		Name: "fs/write_text_file",
		Arguments: map[string]any{
			"path":    filepath.Join(dir, "nonexistent", "sub", "file.txt"),
			"content": "data",
		},
	})
	if err != nil {
		t.Fatalf("HandleCallback() error = %v", err)
	}
	if !result.IsError {
		t.Error("expected error for write to nonexistent directory")
	}
}

func TestNewCallbackHandler_UnresolvablePaths(t *testing.T) {
	// Pass a path that doesn't exist and can't be resolved
	handler := NewCallbackHandler([]string{"/nonexistent/path/abc123"})
	if handler == nil {
		t.Fatal("NewCallbackHandler should not return nil")
	}
	// The handler should still work, just with the cleaned path
	if len(handler.allowedPaths) != 1 {
		t.Errorf("allowedPaths length = %d, want 1", len(handler.allowedPaths))
	}
}
