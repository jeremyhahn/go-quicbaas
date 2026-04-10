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
	"crypto/sha256"
	"encoding/hex"
	"os"
	"strconv"
	"time"

	"github.com/jeremyhahn/go-quicbaas/pkg/embedding"
	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
	"github.com/jeremyhahn/go-quicbaas/pkg/rag"
	"github.com/jeremyhahn/go-quicbaas/pkg/rag/chunking"
	"github.com/jeremyhahn/go-quicbaas/pkg/rag/language"
)

// IndexResult holds the outcome of indexing a single file.
type IndexResult struct {
	FilePath   string
	ChunkCount int
	VectorIDs  []string
	Skipped    bool
	Language   string
	Duration   time.Duration
}

// BatchIndexResult holds the outcome of a batch indexing operation.
type BatchIndexResult struct {
	Results  []*IndexResult
	Total    int
	Indexed  int
	Skipped  int
	Errors   int
	Duration time.Duration
}

// IndexingPipeline orchestrates the read -> chunk -> embed -> store flow.
type IndexingPipeline struct {
	chunkers map[language.Language]chunking.Strategy
	embedder embedding.Provider
	index    rag.VectorIndex
	metadata MetadataStore
	config   IndexingConfig
	logger   interfaces.Logger
	detector language.Detector
}

// NewIndexingPipeline creates a new IndexingPipeline with language-specific chunkers.
func NewIndexingPipeline(
	cfg IndexingConfig,
	embedder embedding.Provider,
	index rag.VectorIndex,
	metadata MetadataStore,
	logger interfaces.Logger,
) *IndexingPipeline {
	// Build language -> chunker map.
	chunkers := map[language.Language]chunking.Strategy{
		language.LanguageGo: chunking.NewGoCodeChunker(chunking.GoCodeChunkerConfig{
			MaxFunctionTokens: cfg.ChunkMaxTokens,
			IncludeImports:    true,
			IncludeComments:   true,
			IncludePackage:    true,
		}),
		language.LanguageMarkdown: chunking.NewMarkdownChunker(chunking.MarkdownChunkerConfig{
			MaxHeaderDepth:   3,
			KeepCodeBlocks:   true,
			IncludeHierarchy: true,
			TargetTokens:     cfg.ChunkMaxTokens / 4,
			MaxTokens:        cfg.ChunkMaxTokens,
			MinChunkSize:     50,
		}),
	}

	return &IndexingPipeline{
		chunkers: chunkers,
		embedder: embedder,
		index:    index,
		metadata: metadata,
		config:   cfg,
		logger:   logger,
		detector: language.NewDetector(),
	}
}

// IndexFile processes a single file through the indexing pipeline.
// It returns a result indicating what was done, or an error on failure.
// Files whose content has not changed since last indexing are skipped.
func (p *IndexingPipeline) IndexFile(ctx context.Context, filePath string) (*IndexResult, error) {
	start := time.Now()

	// 1. Read file content.
	content, err := os.ReadFile(filePath)
	if err != nil {
		return nil, &FileReadError{Path: filePath, Cause: err}
	}

	// 2. Compute SHA-256 checksum.
	hashBytes := sha256.Sum256(content)
	checksum := hex.EncodeToString(hashBytes[:])

	// 3. Check metadata store for incremental skip.
	namespace := p.config.Namespace
	existing, err := p.metadata.GetFileRecord(ctx, namespace, filePath)
	if err != nil {
		p.logger.Warnf("metadata lookup failed for %s: %v", filePath, err)
		// Continue with indexing even if metadata lookup fails.
	}
	if existing != nil && existing.Checksum == checksum {
		return &IndexResult{
			FilePath: filePath,
			Skipped:  true,
			Duration: time.Since(start),
		}, nil
	}

	// 4. Detect language.
	lang, _, langErr := p.detector.Detect(content, filePath)
	if langErr != nil {
		lang = language.LanguageUnknown
	}

	// 5. Select chunker.
	chunker := p.selectChunker(lang)

	// 6. Chunk content.
	meta := chunking.Metadata{
		FilePath: filePath,
		Language: int(lang),
	}
	chunks, err := chunker.Chunk(content, meta)
	if err != nil {
		return nil, &ChunkingError{Path: filePath, Cause: err}
	}

	if len(chunks) == 0 {
		return &IndexResult{
			FilePath:   filePath,
			ChunkCount: 0,
			Skipped:    true,
			Duration:   time.Since(start),
		}, nil
	}

	// 7. Embed chunks via batch.
	texts := make([]string, len(chunks))
	for i, chunk := range chunks {
		texts[i] = chunk.Content
	}
	embeddings, err := p.embedder.EmbedBatch(ctx, texts)
	if err != nil {
		return nil, &EmbeddingError{Path: filePath, Cause: err}
	}

	// 8. Remove old vectors if file was previously indexed.
	if existing != nil && len(existing.ChunkIDs) > 0 {
		for _, oldID := range existing.ChunkIDs {
			if removeErr := p.index.Remove(oldID); removeErr != nil {
				p.logger.Warnf("failed to remove old vector %s: %v", oldID, removeErr)
			}
		}
	}

	// 9. Add new vectors to index.
	vectorIDs := make([]string, len(chunks))
	for i, chunk := range chunks {
		vectorID := buildVectorID(namespace, filePath, i)
		vectorMeta := map[string]interface{}{
			"file_path": filePath,
			"language":  lang.String(),
			"chunk_idx": i,
			"tokens":    chunk.Tokens,
		}
		if chunk.Metadata.FunctionName != "" {
			vectorMeta["function"] = chunk.Metadata.FunctionName
		}
		if chunk.Metadata.ClassName != "" {
			vectorMeta["class"] = chunk.Metadata.ClassName
		}

		if err := p.index.Add(vectorID, embeddings[i], vectorMeta); err != nil {
			return nil, &IndexStoreError{Path: filePath, Cause: err}
		}
		vectorIDs[i] = vectorID
	}

	// 10. Update metadata store.
	fileInfo, statErr := os.Stat(filePath)
	var fileSize int64
	if statErr == nil {
		fileSize = fileInfo.Size()
	}

	record := &FileRecord{
		Namespace: namespace,
		FilePath:  filePath,
		Checksum:  checksum,
		ChunkIDs:  vectorIDs,
		IndexedAt: time.Now().Unix(),
		Size:      fileSize,
		Language:  lang.String(),
	}
	if err := p.metadata.PutFileRecord(ctx, record); err != nil {
		return nil, &MetadataStoreError{
			Operation: "put",
			Key:       filePath,
			Cause:     err,
		}
	}

	return &IndexResult{
		FilePath:   filePath,
		ChunkCount: len(chunks),
		VectorIDs:  vectorIDs,
		Skipped:    false,
		Language:   lang.String(),
		Duration:   time.Since(start),
	}, nil
}

// RemoveFile removes all vectors and metadata for a previously indexed file.
func (p *IndexingPipeline) RemoveFile(ctx context.Context, filePath string) error {
	namespace := p.config.Namespace
	record, err := p.metadata.GetFileRecord(ctx, namespace, filePath)
	if err != nil {
		return err
	}
	if record == nil {
		return nil // Nothing to remove.
	}

	// Remove vectors from the index.
	for _, chunkID := range record.ChunkIDs {
		if removeErr := p.index.Remove(chunkID); removeErr != nil {
			p.logger.Warnf("failed to remove vector %s: %v", chunkID, removeErr)
		}
	}

	// Remove metadata record.
	return p.metadata.DeleteFileRecord(ctx, namespace, filePath)
}

// IndexBatch processes multiple files with batched embedding.
func (p *IndexingPipeline) IndexBatch(ctx context.Context, filePaths []string) (*BatchIndexResult, error) {
	start := time.Now()
	result := &BatchIndexResult{
		Total:   len(filePaths),
		Results: make([]*IndexResult, 0, len(filePaths)),
	}

	for _, path := range filePaths {
		select {
		case <-ctx.Done():
			result.Duration = time.Since(start)
			return result, ctx.Err()
		default:
		}

		res, err := p.IndexFile(ctx, path)
		if err != nil {
			p.logger.Errorf("failed to index file %s: %v", path, err)
			result.Errors++
			continue
		}
		result.Results = append(result.Results, res)
		if res.Skipped {
			result.Skipped++
		} else {
			result.Indexed++
		}
	}

	result.Duration = time.Since(start)
	return result, nil
}

// selectChunker returns the appropriate chunker for the detected language.
// Falls back to a TextChunker for languages without a specialized chunker.
func (p *IndexingPipeline) selectChunker(lang language.Language) chunking.Strategy {
	if chunker, ok := p.chunkers[lang]; ok {
		return chunker
	}
	return chunking.NewTextChunker(chunking.TextChunkerConfig{
		ChunkSize:         p.config.ChunkMaxTokens,
		ChunkOverlap:      p.config.ChunkOverlap,
		MinChunkSize:      50,
		RespectSentences:  true,
		RespectParagraphs: true,
	})
}

// buildVectorID creates a deterministic vector ID from namespace, file path, and chunk index.
func buildVectorID(namespace, filePath string, chunkIndex int) string {
	// Use a short hash of the file path for a compact ID.
	hash := sha256.Sum256([]byte(filePath))
	shortHash := hex.EncodeToString(hash[:8])
	return namespace + ":" + shortHash + ":" + strconv.Itoa(chunkIndex)
}
