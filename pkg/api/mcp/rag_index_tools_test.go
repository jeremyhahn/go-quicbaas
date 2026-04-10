//go:build mcp && rag

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

package mcp

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
)

func newRAGIndexRegistry(t *testing.T) *ToolRegistry {
	t.Helper()
	svc := &extendedMockService{
		embeddingSvc: &mockEmbeddingService{},
	}
	log, _ := logger.New(logger.LogConfig{Level: "error", Output: "stdout"})
	return NewToolRegistry(svc, log)
}

func TestRegisterRAGIndexTools(t *testing.T) {
	registry := newRAGIndexRegistry(t)
	tools := registry.ListTools()

	found := 0
	expected := map[string]bool{
		"quicbaas_rag_index_file":    false,
		"quicbaas_rag_index_project": false,
		"quicbaas_rag_index_status":  false,
	}

	for _, tool := range tools {
		if _, ok := expected[tool.Name]; ok {
			expected[tool.Name] = true
			found++
		}
	}

	assert.Equal(t, 3, found, "expected 3 RAG index tools to be registered")
	for name, registered := range expected {
		assert.True(t, registered, "tool %s should be registered", name)
	}
}

func TestIsRAGIndexTool(t *testing.T) {
	tests := []struct {
		name     string
		toolName string
		expected bool
	}{
		{
			name:     "index file tool",
			toolName: "quicbaas_rag_index_file",
			expected: true,
		},
		{
			name:     "index project tool",
			toolName: "quicbaas_rag_index_project",
			expected: true,
		},
		{
			name:     "index status tool",
			toolName: "quicbaas_rag_index_status",
			expected: true,
		},
		{
			name:     "unknown tool",
			toolName: "quicbaas_unknown_tool",
			expected: false,
		},
		{
			name:     "empty string",
			toolName: "",
			expected: false,
		},
		{
			name:     "rag query tool is not index tool",
			toolName: "quicbaas_rag_query",
			expected: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := isRAGIndexTool(tc.toolName)
			assert.Equal(t, tc.expected, result)
		})
	}
}

func TestExecuteRAGIndexTool_UnknownTool(t *testing.T) {
	registry := newRAGIndexRegistry(t)
	ctx := context.Background()

	result, err := registry.executeRAGIndexTool(ctx, "quicbaas_nonexistent", map[string]interface{}{})
	assert.Nil(t, result)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "quicbaas_nonexistent")
}

func TestExecuteRAGIndexFile_MissingPath(t *testing.T) {
	registry := newRAGIndexRegistry(t)
	ctx := context.Background()

	result, err := registry.ExecuteTool(ctx, "quicbaas_rag_index_file", map[string]interface{}{
		"namespace": "test-ns",
	})
	assert.Nil(t, result)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "file_path")
}

func TestExecuteRAGIndexFile_MissingNamespace(t *testing.T) {
	registry := newRAGIndexRegistry(t)
	ctx := context.Background()

	result, err := registry.ExecuteTool(ctx, "quicbaas_rag_index_file", map[string]interface{}{
		"file_path": "/tmp/test.go",
	})
	assert.Nil(t, result)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "namespace")
}

func TestExecuteRAGIndexProject_MissingPath(t *testing.T) {
	registry := newRAGIndexRegistry(t)
	ctx := context.Background()

	result, err := registry.ExecuteTool(ctx, "quicbaas_rag_index_project", map[string]interface{}{
		"namespace": "test-ns",
	})
	assert.Nil(t, result)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "project_path")
}

func TestExecuteRAGIndexProject_MissingNamespace(t *testing.T) {
	registry := newRAGIndexRegistry(t)
	ctx := context.Background()

	result, err := registry.ExecuteTool(ctx, "quicbaas_rag_index_project", map[string]interface{}{
		"project_path": "/tmp/project",
	})
	assert.Nil(t, result)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "namespace")
}

func TestExecuteRAGIndexStatus_MissingNamespace(t *testing.T) {
	registry := newRAGIndexRegistry(t)
	ctx := context.Background()

	result, err := registry.ExecuteTool(ctx, "quicbaas_rag_index_status", map[string]interface{}{})
	assert.Nil(t, result)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "namespace")
}

func TestDetectLanguageFromExtension(t *testing.T) {
	tests := []struct {
		name     string
		filePath string
		expected string
	}{
		{name: "go file", filePath: "/src/main.go", expected: "go"},
		{name: "python file", filePath: "/src/main.py", expected: "python"},
		{name: "javascript file", filePath: "/src/index.js", expected: "javascript"},
		{name: "typescript file", filePath: "/src/index.ts", expected: "typescript"},
		{name: "rust file", filePath: "/src/main.rs", expected: "rust"},
		{name: "java file", filePath: "/src/Main.java", expected: "java"},
		{name: "c file", filePath: "/src/main.c", expected: "c"},
		{name: "cpp file", filePath: "/src/main.cpp", expected: "cpp"},
		{name: "ruby file", filePath: "/src/main.rb", expected: "ruby"},
		{name: "shell file", filePath: "/src/run.sh", expected: "shell"},
		{name: "markdown file", filePath: "/docs/README.md", expected: "markdown"},
		{name: "yaml file", filePath: "/config.yaml", expected: "yaml"},
		{name: "json file", filePath: "/config.json", expected: "json"},
		{name: "unknown extension", filePath: "/data/file.xyz", expected: "text"},
		{name: "no extension", filePath: "/bin/executable", expected: "text"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := detectLanguageFromExtension(tc.filePath)
			assert.Equal(t, tc.expected, result)
		})
	}
}

func TestChunkContent(t *testing.T) {
	tests := []struct {
		name           string
		content        string
		language       string
		minChunks      int
		expectNonEmpty bool
	}{
		{
			name:           "empty content",
			content:        "",
			language:       "go",
			minChunks:      0,
			expectNonEmpty: false,
		},
		{
			name:           "single small section",
			content:        "package main\n\nfunc main() {}",
			language:       "go",
			minChunks:      1,
			expectNonEmpty: true,
		},
		{
			name:           "whitespace only",
			content:        "   \n\n   \n\n   ",
			language:       "text",
			minChunks:      0,
			expectNonEmpty: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			chunks := chunkContent(tc.content, tc.language)
			if tc.expectNonEmpty {
				assert.GreaterOrEqual(t, len(chunks), tc.minChunks)
				for _, chunk := range chunks {
					assert.NotEmpty(t, chunk)
				}
			} else {
				assert.LessOrEqual(t, len(chunks), tc.minChunks)
			}
		})
	}
}

func TestGenerateChunkID(t *testing.T) {
	// Deterministic: same inputs produce same output
	id1 := generateChunkID("/src/main.go", 0)
	id2 := generateChunkID("/src/main.go", 0)
	assert.Equal(t, id1, id2, "same inputs should produce same ID")

	// Different chunk indices produce different IDs
	id3 := generateChunkID("/src/main.go", 1)
	assert.NotEqual(t, id1, id3, "different chunk indices should produce different IDs")

	// Different file paths produce different IDs
	id4 := generateChunkID("/src/other.go", 0)
	assert.NotEqual(t, id1, id4, "different file paths should produce different IDs")
}

func TestTruncateText(t *testing.T) {
	tests := []struct {
		name     string
		text     string
		maxLen   int
		expected string
	}{
		{name: "short text", text: "hello", maxLen: 10, expected: "hello"},
		{name: "exact length", text: "hello", maxLen: 5, expected: "hello"},
		{name: "needs truncation", text: "hello world this is long", maxLen: 10, expected: "hello w..."},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := truncateText(tc.text, tc.maxLen)
			assert.Equal(t, tc.expected, result)
		})
	}
}

func TestRAGIndexFileReadError(t *testing.T) {
	err := &RAGIndexFileReadError{
		Path:  "/tmp/missing.go",
		Cause: assert.AnError,
	}
	assert.Contains(t, err.Error(), "/tmp/missing.go")
	assert.Equal(t, assert.AnError, err.Unwrap())
}

func TestRAGIndexEmbeddingError(t *testing.T) {
	err := &RAGIndexEmbeddingError{
		Path:       "/tmp/test.go",
		ChunkIndex: 2,
		Cause:      assert.AnError,
	}
	assert.Contains(t, err.Error(), "/tmp/test.go")
	assert.Contains(t, err.Error(), "2")
	assert.Equal(t, assert.AnError, err.Unwrap())
}

func TestRAGIndexUpsertError(t *testing.T) {
	err := &RAGIndexUpsertError{
		Path:       "/tmp/test.go",
		ChunkIndex: 1,
		Cause:      assert.AnError,
	}
	assert.Contains(t, err.Error(), "/tmp/test.go")
	assert.Contains(t, err.Error(), "1")
	assert.Equal(t, assert.AnError, err.Unwrap())
}

func TestRAGIndexGlobError(t *testing.T) {
	err := &RAGIndexGlobError{
		Pattern: "/src/**/*.go",
		Cause:   assert.AnError,
	}
	assert.Contains(t, err.Error(), "/src/**/*.go")
	assert.Equal(t, assert.AnError, err.Unwrap())
}
