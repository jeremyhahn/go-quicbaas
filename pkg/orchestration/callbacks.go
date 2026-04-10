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
	"os"
	"path/filepath"
	"strings"
)

// CallbackHandler handles agent-to-client callbacks such as file system
// operations and permission requests. It enforces path-based access control
// to restrict agent file access to allowed directories.
type CallbackHandler struct {
	allowedPaths []string
}

// NewCallbackHandler creates a new CallbackHandler that restricts file access
// to the specified allowed paths.
func NewCallbackHandler(allowedPaths []string) *CallbackHandler {
	resolved := make([]string, 0, len(allowedPaths))
	for _, p := range allowedPaths {
		clean := filepath.Clean(p)
		if abs, err := filepath.EvalSymlinks(clean); err == nil {
			resolved = append(resolved, abs)
		} else {
			resolved = append(resolved, clean)
		}
	}
	return &CallbackHandler{
		allowedPaths: resolved,
	}
}

// HandleCallback processes an agent callback request and returns the result.
func (h *CallbackHandler) HandleCallback(ctx context.Context, toolCall *ToolCall) (*ToolResult, error) {
	if toolCall == nil {
		return nil, &InputValidationError{Field: "tool_call", Message: "cannot be nil"}
	}

	args, ok := toolCall.Arguments.(map[string]any)
	if !ok {
		return &ToolResult{
			ToolCallID: toolCall.ID,
			Content:    "invalid arguments: expected object",
			IsError:    true,
		}, nil
	}

	switch toolCall.Name {
	case "fs/read_text_file":
		return h.handleReadFile(ctx, toolCall.ID, args)
	case "fs/write_text_file":
		return h.handleWriteFile(ctx, toolCall.ID, args)
	case "session/request_permission":
		return h.handleRequestPermission(ctx, toolCall.ID, args)
	default:
		return &ToolResult{
			ToolCallID: toolCall.ID,
			Content:    "unknown tool: " + toolCall.Name,
			IsError:    true,
		}, nil
	}
}

// handleReadFile handles the fs/read_text_file callback.
func (h *CallbackHandler) handleReadFile(ctx context.Context, toolCallID string, args map[string]any) (*ToolResult, error) {
	pathVal, ok := args["path"]
	if !ok {
		return &ToolResult{
			ToolCallID: toolCallID,
			Content:    "missing required argument: path",
			IsError:    true,
		}, nil
	}

	path, ok := pathVal.(string)
	if !ok {
		return &ToolResult{
			ToolCallID: toolCallID,
			Content:    "path must be a string",
			IsError:    true,
		}, nil
	}

	if !h.isPathAllowed(path) {
		return &ToolResult{
			ToolCallID: toolCallID,
			Content:    "access denied: path outside allowed directories",
			IsError:    true,
		}, nil
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return &ToolResult{
			ToolCallID: toolCallID,
			Content:    "read failed: " + err.Error(),
			IsError:    true,
		}, nil
	}

	return &ToolResult{
		ToolCallID: toolCallID,
		Content:    string(data),
	}, nil
}

// handleWriteFile handles the fs/write_text_file callback.
func (h *CallbackHandler) handleWriteFile(ctx context.Context, toolCallID string, args map[string]any) (*ToolResult, error) {
	pathVal, ok := args["path"]
	if !ok {
		return &ToolResult{
			ToolCallID: toolCallID,
			Content:    "missing required argument: path",
			IsError:    true,
		}, nil
	}

	path, ok := pathVal.(string)
	if !ok {
		return &ToolResult{
			ToolCallID: toolCallID,
			Content:    "path must be a string",
			IsError:    true,
		}, nil
	}

	contentVal, ok := args["content"]
	if !ok {
		return &ToolResult{
			ToolCallID: toolCallID,
			Content:    "missing required argument: content",
			IsError:    true,
		}, nil
	}

	content, ok := contentVal.(string)
	if !ok {
		return &ToolResult{
			ToolCallID: toolCallID,
			Content:    "content must be a string",
			IsError:    true,
		}, nil
	}

	if !h.isPathAllowed(path) {
		return &ToolResult{
			ToolCallID: toolCallID,
			Content:    "access denied: path outside allowed directories",
			IsError:    true,
		}, nil
	}

	if err := os.WriteFile(path, []byte(content), 0600); err != nil {
		return &ToolResult{
			ToolCallID: toolCallID,
			Content:    "write failed: " + err.Error(),
			IsError:    true,
		}, nil
	}

	return &ToolResult{
		ToolCallID: toolCallID,
		Content:    "file written successfully",
	}, nil
}

// handleRequestPermission handles the session/request_permission callback.
// By default, permissions are auto-approved for paths within allowed directories.
func (h *CallbackHandler) handleRequestPermission(ctx context.Context, toolCallID string, args map[string]any) (*ToolResult, error) {
	pathVal, ok := args["path"]
	if !ok {
		// No path specified, approve by default
		return &ToolResult{
			ToolCallID: toolCallID,
			Content:    "approved",
		}, nil
	}

	path, ok := pathVal.(string)
	if !ok {
		return &ToolResult{
			ToolCallID: toolCallID,
			Content:    "path must be a string",
			IsError:    true,
		}, nil
	}

	if h.isPathAllowed(path) {
		return &ToolResult{
			ToolCallID: toolCallID,
			Content:    "approved",
		}, nil
	}

	return &ToolResult{
		ToolCallID: toolCallID,
		Content:    "denied: path outside allowed directories",
		IsError:    true,
	}, nil
}

// isPathAllowed checks if a file path is within the allowed directories.
// It resolves symlinks and uses filepath.Rel to detect path traversal.
func (h *CallbackHandler) isPathAllowed(path string) bool {
	// Clean and resolve the path
	clean := filepath.Clean(path)

	// Attempt to resolve symlinks; use the clean path if resolution fails
	resolved, err := filepath.EvalSymlinks(clean)
	if err != nil {
		// If the file doesn't exist yet, resolve the parent directory
		dir := filepath.Dir(clean)
		resolvedDir, dirErr := filepath.EvalSymlinks(dir)
		if dirErr != nil {
			// Cannot resolve parent either, use cleaned path
			resolved = clean
		} else {
			resolved = filepath.Join(resolvedDir, filepath.Base(clean))
		}
	}

	for _, allowed := range h.allowedPaths {
		rel, err := filepath.Rel(allowed, resolved)
		if err != nil {
			continue
		}
		// The relative path must not start with ".." to be within the allowed dir
		if !strings.HasPrefix(rel, "..") {
			return true
		}
	}

	return false
}
