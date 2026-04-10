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

package indexing

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/jeremyhahn/go-quicbaas/pkg/rag"
)

// newTestPipeline creates an IndexingPipeline wired with in-memory test dependencies.
func newTestPipeline(t *testing.T, dimensions int) (*IndexingPipeline, string) {
	t.Helper()
	tmpDir := t.TempDir()
	cfg := IndexingConfig{
		Enabled:        true,
		IndexType:      "memory",
		BatchSize:      32,
		MaxConcurrency: 2,
		ChunkMaxTokens: 2000,
		ChunkOverlap:   200,
		ProjectPath:    tmpDir,
		Namespace:      "test-ns",
	}

	embedder := newTestEmbeddingProvider(dimensions)
	index, err := rag.NewVectorIndex(&rag.VectorIndexConfig{
		Dimensions:     dimensions,
		DistanceMetric: rag.CosineDistance,
	})
	if err != nil {
		t.Fatalf("failed to create vector index: %v", err)
	}

	metadata := NewMetadataStore(newMemoryKVStore())
	logger := newTestLogger()

	pipeline := NewIndexingPipeline(cfg, embedder, index, metadata, logger)
	return pipeline, tmpDir
}

func TestIndexingPipeline_IndexFile_GoSource(t *testing.T) {
	pipeline, tmpDir := newTestPipeline(t, 64)

	goFile := filepath.Join(tmpDir, "main.go")
	content := `package main

import "fmt"

// Hello prints a greeting.
func Hello() {
	fmt.Println("hello world")
}
`
	if err := os.WriteFile(goFile, []byte(content), 0644); err != nil {
		t.Fatalf("failed to write test file: %v", err)
	}

	ctx := context.Background()
	result, err := pipeline.IndexFile(ctx, goFile)
	if err != nil {
		t.Fatalf("IndexFile failed: %v", err)
	}
	if result.Skipped {
		t.Fatal("expected file to be indexed, not skipped")
	}
	if result.ChunkCount == 0 {
		t.Fatal("expected at least one chunk")
	}
	if result.Language != "go" {
		t.Fatalf("expected language go, got %s", result.Language)
	}
	if len(result.VectorIDs) == 0 {
		t.Fatal("expected at least one vector ID")
	}
}

func TestIndexingPipeline_IndexFile_IncrementalSkip(t *testing.T) {
	pipeline, tmpDir := newTestPipeline(t, 64)

	goFile := filepath.Join(tmpDir, "skip.go")
	content := `package main

func Skip() {}
`
	if err := os.WriteFile(goFile, []byte(content), 0644); err != nil {
		t.Fatalf("failed to write test file: %v", err)
	}

	ctx := context.Background()

	// First index.
	result1, err := pipeline.IndexFile(ctx, goFile)
	if err != nil {
		t.Fatalf("first IndexFile failed: %v", err)
	}
	if result1.Skipped {
		t.Fatal("first index should not be skipped")
	}

	// Second index with same content should be skipped.
	result2, err := pipeline.IndexFile(ctx, goFile)
	if err != nil {
		t.Fatalf("second IndexFile failed: %v", err)
	}
	if !result2.Skipped {
		t.Fatal("second index with unchanged content should be skipped")
	}
}

func TestIndexingPipeline_IndexFile_ReindexOnChange(t *testing.T) {
	pipeline, tmpDir := newTestPipeline(t, 64)

	goFile := filepath.Join(tmpDir, "change.go")
	content1 := `package main

func V1() {}
`
	if err := os.WriteFile(goFile, []byte(content1), 0644); err != nil {
		t.Fatalf("failed to write test file: %v", err)
	}

	ctx := context.Background()

	result1, err := pipeline.IndexFile(ctx, goFile)
	if err != nil {
		t.Fatalf("first IndexFile failed: %v", err)
	}
	if result1.Skipped {
		t.Fatal("first index should not be skipped")
	}

	// Modify content.
	content2 := `package main

func V2() {}

func V3() {}
`
	if err := os.WriteFile(goFile, []byte(content2), 0644); err != nil {
		t.Fatalf("failed to write modified file: %v", err)
	}

	result2, err := pipeline.IndexFile(ctx, goFile)
	if err != nil {
		t.Fatalf("second IndexFile failed: %v", err)
	}
	if result2.Skipped {
		t.Fatal("changed file should not be skipped")
	}
	if result2.ChunkCount <= result1.ChunkCount {
		t.Fatalf("expected more chunks after adding functions, got %d vs %d",
			result2.ChunkCount, result1.ChunkCount)
	}
}

func TestIndexingPipeline_IndexFile_NonexistentFile(t *testing.T) {
	pipeline, _ := newTestPipeline(t, 64)
	ctx := context.Background()

	_, err := pipeline.IndexFile(ctx, "/nonexistent/path/file.go")
	if err == nil {
		t.Fatal("expected error for nonexistent file")
	}
	var fileErr *FileReadError
	if !errors.As(err, &fileErr) {
		t.Fatalf("expected *FileReadError, got %T", err)
	}
}

func TestIndexingPipeline_IndexFile_MarkdownSource(t *testing.T) {
	pipeline, tmpDir := newTestPipeline(t, 64)

	mdFile := filepath.Join(tmpDir, "readme.md")
	content := `# Project Title

This is a description of the project with enough content to exceed the minimum chunk size for testing purposes.
It needs to be relatively long to ensure that the markdown chunker creates at least one chunk from this content.
Here is some more text to make sure we have enough tokens to pass the minimum chunk size threshold easily.

## Features

- Feature one with description that is long enough
- Feature two with description that is long enough
- Feature three with description that is long enough
`
	if err := os.WriteFile(mdFile, []byte(content), 0644); err != nil {
		t.Fatalf("failed to write test file: %v", err)
	}

	ctx := context.Background()
	result, err := pipeline.IndexFile(ctx, mdFile)
	if err != nil {
		t.Fatalf("IndexFile failed: %v", err)
	}
	if result.Skipped {
		t.Fatal("expected file to be indexed")
	}
	if result.Language != "markdown" {
		t.Fatalf("expected language markdown, got %s", result.Language)
	}
}

func TestIndexingPipeline_IndexFile_TextFallback(t *testing.T) {
	pipeline, tmpDir := newTestPipeline(t, 64)

	txtFile := filepath.Join(tmpDir, "notes.txt")
	content := `This is a plain text file with enough content to be indexed.
It should fall back to the text chunker since there is no specialized
chunker for .txt files. Adding more lines here to ensure we have enough
tokens to exceed the minimum chunk size threshold. This paragraph
contains important information about the project and its requirements.
The text chunker should handle this content properly and create at least
one meaningful chunk from it. Let us add even more content to be safe.
`
	if err := os.WriteFile(txtFile, []byte(content), 0644); err != nil {
		t.Fatalf("failed to write test file: %v", err)
	}

	ctx := context.Background()
	result, err := pipeline.IndexFile(ctx, txtFile)
	if err != nil {
		t.Fatalf("IndexFile failed: %v", err)
	}
	if result.Skipped {
		t.Fatal("expected file to be indexed")
	}
}

func TestIndexingPipeline_RemoveFile_Success(t *testing.T) {
	pipeline, tmpDir := newTestPipeline(t, 64)

	goFile := filepath.Join(tmpDir, "removeme.go")
	content := `package main

func RemoveMe() {}
`
	if err := os.WriteFile(goFile, []byte(content), 0644); err != nil {
		t.Fatalf("failed to write test file: %v", err)
	}

	ctx := context.Background()

	// Index the file first.
	result, err := pipeline.IndexFile(ctx, goFile)
	if err != nil {
		t.Fatalf("IndexFile failed: %v", err)
	}
	if result.Skipped {
		t.Fatal("expected file to be indexed")
	}

	// Verify metadata exists.
	record, err := pipeline.metadata.GetFileRecord(ctx, "test-ns", goFile)
	if err != nil {
		t.Fatalf("GetFileRecord failed: %v", err)
	}
	if record == nil {
		t.Fatal("expected metadata record to exist")
	}

	// Remove the file.
	if err := pipeline.RemoveFile(ctx, goFile); err != nil {
		t.Fatalf("RemoveFile failed: %v", err)
	}

	// Verify metadata is gone.
	record, err = pipeline.metadata.GetFileRecord(ctx, "test-ns", goFile)
	if err != nil {
		t.Fatalf("GetFileRecord after remove failed: %v", err)
	}
	if record != nil {
		t.Fatal("expected metadata record to be removed")
	}
}

func TestIndexingPipeline_RemoveFile_NotIndexed(t *testing.T) {
	pipeline, _ := newTestPipeline(t, 64)
	ctx := context.Background()

	// Removing a file that was never indexed should succeed silently.
	if err := pipeline.RemoveFile(ctx, "/nonexistent"); err != nil {
		t.Fatalf("RemoveFile on non-indexed file should not error, got: %v", err)
	}
}

func TestIndexingPipeline_IndexBatch_Success(t *testing.T) {
	pipeline, tmpDir := newTestPipeline(t, 64)

	files := []string{
		filepath.Join(tmpDir, "batch1.go"),
		filepath.Join(tmpDir, "batch2.go"),
	}

	for i, f := range files {
		content := "package main\n\nfunc Batch" + strconv.Itoa(i) + "() {}\n"
		if err := os.WriteFile(f, []byte(content), 0644); err != nil {
			t.Fatalf("failed to write test file: %v", err)
		}
	}

	ctx := context.Background()
	result, err := pipeline.IndexBatch(ctx, files)
	if err != nil {
		t.Fatalf("IndexBatch failed: %v", err)
	}
	if result.Total != 2 {
		t.Fatalf("expected total 2, got %d", result.Total)
	}
	if result.Indexed != 2 {
		t.Fatalf("expected 2 indexed, got %d", result.Indexed)
	}
	if result.Errors != 0 {
		t.Fatalf("expected 0 errors, got %d", result.Errors)
	}
}

func TestIndexingPipeline_IndexBatch_PartialFailure(t *testing.T) {
	pipeline, tmpDir := newTestPipeline(t, 64)

	goodFile := filepath.Join(tmpDir, "good.go")
	if err := os.WriteFile(goodFile, []byte("package main\n\nfunc Good() {}\n"), 0644); err != nil {
		t.Fatalf("failed to write test file: %v", err)
	}

	files := []string{goodFile, "/nonexistent/bad.go"}

	ctx := context.Background()
	result, err := pipeline.IndexBatch(ctx, files)
	if err != nil {
		t.Fatalf("IndexBatch should not return error on partial failure, got: %v", err)
	}
	if result.Indexed != 1 {
		t.Fatalf("expected 1 indexed, got %d", result.Indexed)
	}
	if result.Errors != 1 {
		t.Fatalf("expected 1 error, got %d", result.Errors)
	}
}

func TestIndexingPipeline_IndexFile_EmbeddingError(t *testing.T) {
	tmpDir := t.TempDir()
	cfg := IndexingConfig{
		Enabled:        true,
		IndexType:      "memory",
		BatchSize:      32,
		MaxConcurrency: 2,
		ChunkMaxTokens: 2000,
		ChunkOverlap:   200,
		ProjectPath:    tmpDir,
		Namespace:      "test-ns",
	}

	embErr := errors.New("embedding service unavailable")
	embedder := newFailingEmbeddingProvider(64, embErr)
	index, err := rag.NewVectorIndex(&rag.VectorIndexConfig{
		Dimensions:     64,
		DistanceMetric: rag.CosineDistance,
	})
	if err != nil {
		t.Fatalf("failed to create vector index: %v", err)
	}

	metadata := NewMetadataStore(newMemoryKVStore())
	logger := newTestLogger()
	pipeline := NewIndexingPipeline(cfg, embedder, index, metadata, logger)

	goFile := filepath.Join(tmpDir, "embed_fail.go")
	if err := os.WriteFile(goFile, []byte("package main\n\nfunc Fail() {}\n"), 0644); err != nil {
		t.Fatalf("failed to write test file: %v", err)
	}

	ctx := context.Background()
	_, indexErr := pipeline.IndexFile(ctx, goFile)
	if indexErr == nil {
		t.Fatal("expected error from failing embedding provider")
	}
	var embeddingErr *EmbeddingError
	if !errors.As(indexErr, &embeddingErr) {
		t.Fatalf("expected *EmbeddingError, got %T", indexErr)
	}
}

func TestIndexingPipeline_IndexBatch_ContextCancellation(t *testing.T) {
	pipeline, tmpDir := newTestPipeline(t, 64)

	files := make([]string, 10)
	for i := range files {
		f := filepath.Join(tmpDir, "cancel"+strconv.Itoa(i)+".go")
		if err := os.WriteFile(f, []byte("package main\n\nfunc Cancel"+strconv.Itoa(i)+"() {}\n"), 0644); err != nil {
			t.Fatalf("failed to write test file: %v", err)
		}
		files[i] = f
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately.

	_, err := pipeline.IndexBatch(ctx, files)
	if err == nil {
		t.Fatal("expected context cancellation error")
	}
}

func TestBuildVectorID_ChunkIndexFormat(t *testing.T) {
	id := buildVectorID("ns", "/file.go", 42)
	suffix := ":" + strconv.Itoa(42)
	if len(id) < len(suffix) || id[len(id)-len(suffix):] != suffix {
		t.Errorf("buildVectorID should end with chunk index suffix %q, got %q", suffix, id)
	}
}

func TestBuildVectorID_Deterministic(t *testing.T) {
	id1 := buildVectorID("ns", "/file.go", 0)
	id2 := buildVectorID("ns", "/file.go", 0)
	id3 := buildVectorID("ns", "/file.go", 1)

	if id1 != id2 {
		t.Fatal("same inputs should produce same vector ID")
	}
	if id1 == id3 {
		t.Fatal("different chunk indices should produce different vector IDs")
	}
}

func TestBuildVectorID_DifferentFiles(t *testing.T) {
	id1 := buildVectorID("ns", "/file1.go", 0)
	id2 := buildVectorID("ns", "/file2.go", 0)

	if id1 == id2 {
		t.Fatal("different file paths should produce different vector IDs")
	}
}
