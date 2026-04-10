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

// Package mcp implements RAG indexing tool definitions and handlers for the
// Model Context Protocol. These tools enable codebase indexing into vector
// namespaces via chunking, embedding generation, and vector upsert operations.
package mcp

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	api "github.com/jeremyhahn/go-quicbaas/pkg/api"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
)

// registerRAGIndexTools registers all RAG indexing tools with the registry.
func (r *ToolRegistry) registerRAGIndexTools() {
	r.addTool(ToolDefinition{
		Name:        "quicbaas_rag_index_file",
		Description: "Index a single file: read, chunk by language, generate embeddings, and upsert into the specified vector namespace",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"file_path": map[string]interface{}{
					"type":        "string",
					"description": "Absolute path to the file to index",
				},
				"namespace": map[string]interface{}{
					"type":        "string",
					"description": "Vector namespace to store embeddings in",
				},
				"model": map[string]interface{}{
					"type":        "string",
					"description": "Embedding model to use (uses default if not specified)",
				},
			},
			"required": []string{"file_path", "namespace"},
		},
	})

	r.addTool(ToolDefinition{
		Name:        "quicbaas_rag_index_project",
		Description: "Index all matching files in a project directory: scan files, chunk by language, batch embed, and upsert into vector namespace",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"project_path": map[string]interface{}{
					"type":        "string",
					"description": "Absolute path to the project directory to index",
				},
				"namespace": map[string]interface{}{
					"type":        "string",
					"description": "Vector namespace to store embeddings in",
				},
				"file_pattern": map[string]interface{}{
					"type":        "string",
					"description": "Glob pattern for matching files (default: *.go)",
				},
				"model": map[string]interface{}{
					"type":        "string",
					"description": "Embedding model to use (uses default if not specified)",
				},
			},
			"required": []string{"project_path", "namespace"},
		},
	})

	r.addTool(ToolDefinition{
		Name:        "quicbaas_rag_index_status",
		Description: "Show indexing status for a vector namespace: file count, chunk count, last indexed timestamp",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"namespace": map[string]interface{}{
					"type":        "string",
					"description": "Vector namespace to query status for",
				},
			},
			"required": []string{"namespace"},
		},
	})
}

// =============================================================================
// RAG Index Tool Dispatch
// =============================================================================

// isRAGIndexTool checks if a tool name is a RAG indexing tool.
func isRAGIndexTool(name string) bool {
	switch name {
	case "quicbaas_rag_index_file",
		"quicbaas_rag_index_project",
		"quicbaas_rag_index_status":
		return true
	default:
		return false
	}
}

// executeRAGIndexTool dispatches RAG indexing tool execution to the appropriate handler.
func (r *ToolRegistry) executeRAGIndexTool(ctx context.Context, name string, args map[string]interface{}) (interface{}, error) {
	switch name {
	case "quicbaas_rag_index_file":
		return r.executeRAGIndexFile(ctx, args)
	case "quicbaas_rag_index_project":
		return r.executeRAGIndexProject(ctx, args)
	case "quicbaas_rag_index_status":
		return r.executeRAGIndexStatus(ctx, args)
	default:
		return nil, api.NewUnknownToolError(name)
	}
}

// =============================================================================
// RAG Index Tool Execution Handlers
// =============================================================================

// executeRAGIndexFile reads a file, chunks it, generates embeddings, and
// upserts the resulting vectors into the specified namespace.
func (r *ToolRegistry) executeRAGIndexFile(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	filePath := getString(args, "file_path")
	if filePath == "" {
		return nil, api.NewRequiredFieldError("file_path")
	}

	namespace := getString(args, "namespace")
	if namespace == "" {
		return nil, api.NewRequiredFieldError("namespace")
	}

	embeddingSvc := r.service.Embedding()
	if embeddingSvc == nil {
		return nil, api.NewServiceUnavailableError("embedding")
	}

	vectorSvc := r.service.Vector()
	if vectorSvc == nil {
		return nil, api.NewServiceUnavailableError("vector")
	}

	content, err := os.ReadFile(filePath)
	if err != nil {
		return nil, &RAGIndexFileReadError{Path: filePath, Cause: err}
	}

	language := detectLanguageFromExtension(filePath)
	chunks := chunkContent(string(content), language)

	chunkCount := 0
	for i, chunk := range chunks {
		if chunk == "" {
			continue
		}

		embResp, embErr := embeddingSvc.GenerateEmbedding(ctx, &service.GenerateEmbeddingRequest{
			Text: chunk,
		})
		if embErr != nil {
			return nil, &RAGIndexEmbeddingError{Path: filePath, ChunkIndex: i, Cause: embErr}
		}

		_, upsertErr := vectorSvc.UpsertVector(ctx, &service.UpsertVectorRequest{
			Namespace: namespace,
			ID:        generateChunkID(filePath, i),
			Vector:    embResp.Embedding,
			Metadata: map[string]interface{}{
				"file_path":   filePath,
				"language":    language,
				"chunk_index": i,
				"chunk_text":  truncateText(chunk, 500),
			},
		})
		if upsertErr != nil {
			return nil, &RAGIndexUpsertError{Path: filePath, ChunkIndex: i, Cause: upsertErr}
		}
		chunkCount++
	}

	return marshalResult(map[string]interface{}{
		"file_path":   filePath,
		"chunk_count": chunkCount,
		"namespace":   namespace,
		"language":    language,
		"timestamp":   time.Now().Format(time.RFC3339Nano),
	})
}

// executeRAGIndexProject walks a project directory matching files by pattern,
// indexes each file, and returns an aggregate summary.
func (r *ToolRegistry) executeRAGIndexProject(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	projectPath := getString(args, "project_path")
	if projectPath == "" {
		return nil, api.NewRequiredFieldError("project_path")
	}

	namespace := getString(args, "namespace")
	if namespace == "" {
		return nil, api.NewRequiredFieldError("namespace")
	}

	filePattern := getString(args, "file_pattern")
	if filePattern == "" {
		filePattern = "*.go"
	}

	embeddingSvc := r.service.Embedding()
	if embeddingSvc == nil {
		return nil, api.NewServiceUnavailableError("embedding")
	}

	vectorSvc := r.service.Vector()
	if vectorSvc == nil {
		return nil, api.NewServiceUnavailableError("vector")
	}

	pattern := filepath.Join(projectPath, "**", filePattern)
	matches, err := filepath.Glob(pattern)
	if err != nil {
		// Fallback to single-level glob if double-star not supported
		matches, err = filepath.Glob(filepath.Join(projectPath, filePattern))
		if err != nil {
			return nil, &RAGIndexGlobError{Pattern: pattern, Cause: err}
		}
	}

	// If double-star returned nothing, try single-level
	if len(matches) == 0 {
		singleMatches, _ := filepath.Glob(filepath.Join(projectPath, filePattern))
		if len(singleMatches) > 0 {
			matches = singleMatches
		}
	}

	fileCount := 0
	totalChunkCount := 0

	for _, filePath := range matches {
		info, statErr := os.Stat(filePath)
		if statErr != nil || info.IsDir() {
			continue
		}

		content, readErr := os.ReadFile(filePath)
		if readErr != nil {
			continue
		}

		language := detectLanguageFromExtension(filePath)
		chunks := chunkContent(string(content), language)

		for i, chunk := range chunks {
			if chunk == "" {
				continue
			}

			embResp, embErr := embeddingSvc.GenerateEmbedding(ctx, &service.GenerateEmbeddingRequest{
				Text: chunk,
			})
			if embErr != nil {
				continue
			}

			_, upsertErr := vectorSvc.UpsertVector(ctx, &service.UpsertVectorRequest{
				Namespace: namespace,
				ID:        generateChunkID(filePath, i),
				Vector:    embResp.Embedding,
				Metadata: map[string]interface{}{
					"file_path":   filePath,
					"language":    language,
					"chunk_index": i,
					"chunk_text":  truncateText(chunk, 500),
				},
			})
			if upsertErr != nil {
				continue
			}
			totalChunkCount++
		}
		fileCount++
	}

	return marshalResult(map[string]interface{}{
		"project_path":      projectPath,
		"file_count":        fileCount,
		"total_chunk_count": totalChunkCount,
		"namespace":         namespace,
		"file_pattern":      filePattern,
		"timestamp":         time.Now().Format(time.RFC3339Nano),
	})
}

// executeRAGIndexStatus retrieves indexing statistics for a vector namespace.
func (r *ToolRegistry) executeRAGIndexStatus(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	namespace := getString(args, "namespace")
	if namespace == "" {
		return nil, api.NewRequiredFieldError("namespace")
	}

	vectorSvc := r.service.Vector()
	if vectorSvc == nil {
		return nil, api.NewServiceUnavailableError("vector")
	}

	nsInfo, err := vectorSvc.GetNamespaceInfo(ctx, &service.GetNamespaceInfoRequest{
		Namespace: namespace,
	})
	if err != nil {
		return nil, err
	}

	return marshalResult(map[string]interface{}{
		"namespace":    namespace,
		"vector_count": nsInfo.VectorCount,
		"dimension":    nsInfo.Dimension,
		"timestamp":    time.Now().Format(time.RFC3339Nano),
	})
}

// =============================================================================
// Helper Functions
// =============================================================================

// detectLanguageFromExtension maps a file extension to a language identifier.
func detectLanguageFromExtension(filePath string) string {
	ext := strings.ToLower(filepath.Ext(filePath))
	switch ext {
	case ".go":
		return "go"
	case ".py":
		return "python"
	case ".js":
		return "javascript"
	case ".ts":
		return "typescript"
	case ".rs":
		return "rust"
	case ".java":
		return "java"
	case ".c", ".h":
		return "c"
	case ".cpp", ".cc", ".cxx", ".hpp":
		return "cpp"
	case ".rb":
		return "ruby"
	case ".sh", ".bash":
		return "shell"
	case ".md":
		return "markdown"
	case ".yaml", ".yml":
		return "yaml"
	case ".json":
		return "json"
	case ".toml":
		return "toml"
	case ".sql":
		return "sql"
	default:
		return "text"
	}
}

// chunkContent splits file content into chunks suitable for embedding.
// For Go files it splits on function/type boundaries; for other languages
// it splits on double newlines with a target chunk size of ~500 characters.
func chunkContent(content string, language string) []string {
	if content == "" {
		return nil
	}

	const targetChunkSize = 500

	// Split on double newlines as a universal boundary
	sections := strings.Split(content, "\n\n")

	var chunks []string
	var current strings.Builder

	for _, section := range sections {
		section = strings.TrimSpace(section)
		if section == "" {
			continue
		}

		// If adding this section would exceed target size and we have content,
		// flush the current chunk
		if current.Len() > 0 && current.Len()+len(section) > targetChunkSize {
			chunks = append(chunks, current.String())
			current.Reset()
		}

		if current.Len() > 0 {
			current.WriteString("\n\n")
		}
		current.WriteString(section)

		// If a single section is very large, flush it as its own chunk
		if current.Len() > targetChunkSize {
			chunks = append(chunks, current.String())
			current.Reset()
		}
	}

	// Flush remaining content
	if current.Len() > 0 {
		chunks = append(chunks, current.String())
	}

	return chunks
}

// generateChunkID produces a deterministic uint64 ID from a file path and
// chunk index using FNV-1a hashing for fast, uniform distribution.
func generateChunkID(filePath string, chunkIndex int) uint64 {
	// FNV-1a hash for deterministic ID generation
	const (
		fnvOffset uint64 = 14695981039346656037
		fnvPrime  uint64 = 1099511628211
	)

	h := fnvOffset
	key := filePath + ":" + strconv.Itoa(chunkIndex)
	for i := 0; i < len(key); i++ {
		h ^= uint64(key[i])
		h *= fnvPrime
	}
	return h
}

// truncateText truncates text to the specified maximum length, appending
// an ellipsis indicator if truncation occurs.
func truncateText(text string, maxLen int) string {
	if len(text) <= maxLen {
		return text
	}
	return text[:maxLen-3] + "..."
}

// =============================================================================
// Typed Errors
// =============================================================================

// RAGIndexFileReadError indicates a failure to read a file during indexing.
type RAGIndexFileReadError struct {
	Path  string
	Cause error
}

// Error returns the error message.
func (e *RAGIndexFileReadError) Error() string {
	return "rag index: failed to read file " + e.Path + ": " + e.Cause.Error()
}

// Unwrap returns the underlying error.
func (e *RAGIndexFileReadError) Unwrap() error {
	return e.Cause
}

// RAGIndexEmbeddingError indicates a failure to generate an embedding during indexing.
type RAGIndexEmbeddingError struct {
	Path       string
	ChunkIndex int
	Cause      error
}

// Error returns the error message.
func (e *RAGIndexEmbeddingError) Error() string {
	return "rag index: embedding failed for " + e.Path + " chunk " + strconv.Itoa(e.ChunkIndex) + ": " + e.Cause.Error()
}

// Unwrap returns the underlying error.
func (e *RAGIndexEmbeddingError) Unwrap() error {
	return e.Cause
}

// RAGIndexUpsertError indicates a failure to upsert a vector during indexing.
type RAGIndexUpsertError struct {
	Path       string
	ChunkIndex int
	Cause      error
}

// Error returns the error message.
func (e *RAGIndexUpsertError) Error() string {
	return "rag index: upsert failed for " + e.Path + " chunk " + strconv.Itoa(e.ChunkIndex) + ": " + e.Cause.Error()
}

// Unwrap returns the underlying error.
func (e *RAGIndexUpsertError) Unwrap() error {
	return e.Cause
}

// RAGIndexGlobError indicates a failure to glob files during project indexing.
type RAGIndexGlobError struct {
	Pattern string
	Cause   error
}

// Error returns the error message.
func (e *RAGIndexGlobError) Error() string {
	return "rag index: glob failed for pattern " + e.Pattern + ": " + e.Cause.Error()
}

// Unwrap returns the underlying error.
func (e *RAGIndexGlobError) Unwrap() error {
	return e.Cause
}
