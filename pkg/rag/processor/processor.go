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
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"

	"github.com/jeremyhahn/go-quicbaas/pkg/rag/chunking"
	"github.com/jeremyhahn/go-quicbaas/pkg/rag/language"
	"github.com/jeremyhahn/go-quicbaas/pkg/rag/metadata"
)

var (
	// ErrInvalidConfig indicates invalid processor configuration.
	ErrInvalidConfig = errors.New("invalid processor configuration")
	// ErrFileNotFound indicates file does not exist.
	ErrFileNotFound = errors.New("file not found")
	// ErrEmptyContent indicates empty file content.
	ErrEmptyContent = errors.New("empty content")
	// ErrUnsupportedLanguage indicates unsupported language.
	ErrUnsupportedLanguage = errors.New("unsupported language")
	// ErrContextCanceled indicates context was canceled.
	ErrContextCanceled = errors.New("context canceled")
	// ErrBinaryFile indicates a binary file was detected.
	ErrBinaryFile = errors.New("binary file detected")
	// ErrPathTraversal indicates a path traversal attempt was detected.
	ErrPathTraversal = errors.New("path traversal detected")
	// ErrPathNotAllowed indicates a path is outside the allowed directories.
	ErrPathNotAllowed = errors.New("path not allowed")
)

// SentinelCauseError wraps a sentinel error with a cause using multi-unwrap.
type SentinelCauseError struct {
	Sentinel error
	Cause    error
}

// Error implements the error interface.
func (e *SentinelCauseError) Error() string {
	return e.Sentinel.Error() + ": " + e.Cause.Error()
}

// Unwrap returns both errors for multi-unwrap support (Go 1.20+).
func (e *SentinelCauseError) Unwrap() []error {
	return []error{e.Sentinel, e.Cause}
}

// ProcessorCauseError wraps a processor error with a cause.
type ProcessorCauseError struct {
	Message string
	Cause   error
}

// Error implements the error interface.
func (e *ProcessorCauseError) Error() string {
	return e.Message + ": " + e.Cause.Error()
}

// Unwrap returns the underlying cause.
func (e *ProcessorCauseError) Unwrap() error {
	return e.Cause
}

// FileProcessError wraps an error during batch file processing.
type FileProcessError struct {
	Path  string
	Cause error
}

// Error implements the error interface.
func (e *FileProcessError) Error() string {
	return e.Path + ": " + e.Cause.Error()
}

// Unwrap returns the underlying cause.
func (e *FileProcessError) Unwrap() error {
	return e.Cause
}

// PathValidationError wraps a path validation error with the offending path.
type PathValidationError struct {
	Path   string
	Reason error
}

// Error implements the error interface.
func (e *PathValidationError) Error() string {
	return e.Path + ": " + e.Reason.Error()
}

// Unwrap returns the underlying reason.
func (e *PathValidationError) Unwrap() error {
	return e.Reason
}

// Processor defines the document processing interface.
type Processor interface {
	// Process processes a single file and returns chunks
	Process(ctx context.Context, filePath string) ([]ProcessedChunk, error)

	// ProcessBatch processes multiple files in parallel and returns chunks
	ProcessBatch(ctx context.Context, filePaths []string) ([]ProcessedChunk, error)

	// ProcessContent processes content directly without reading from file
	ProcessContent(ctx context.Context, content []byte, filename string) ([]ProcessedChunk, error)
}

// ProcessedChunk represents a processed chunk with metadata.
type ProcessedChunk struct {
	Content  string                 // Chunk content
	Metadata metadata.ChunkMetadata // Comprehensive metadata
	Tokens   int                    // Token count
	Hash     string                 // SHA256 hash for deduplication
}

// processor implements the Processor interface.
type processor struct {
	config        Config
	detector      language.Detector
	metaExtractor metadata.Extractor
	textChunker   *chunking.TextChunker
	goCodeChunker *chunking.GoCodeChunker
	allowedDirs   []string // Resolved allowed directory paths
	// markdownChunker will be added when implemented in parallel
}

// New creates a new document processor.
func New(config Config) (Processor, error) {
	// Validate configuration
	if err := config.Validate(); err != nil {
		return nil, &SentinelCauseError{Sentinel: ErrInvalidConfig, Cause: err}
	}

	// Resolve allowed directories to absolute paths
	resolved := make([]string, 0, len(config.AllowedDirs))
	for _, dir := range config.AllowedDirs {
		clean := filepath.Clean(dir)
		if abs, err := filepath.Abs(clean); err == nil {
			resolved = append(resolved, abs)
		} else {
			resolved = append(resolved, clean)
		}
	}

	p := &processor{
		config:        config,
		detector:      language.NewDetector(),
		metaExtractor: metadata.NewExtractor(config.MetadataConfig),
		textChunker:   chunking.NewTextChunker(config.TextConfig),
		goCodeChunker: chunking.NewGoCodeChunker(config.CodeConfig),
		allowedDirs:   resolved,
	}

	return p, nil
}

// Process implements the Processor interface.
func (p *processor) Process(ctx context.Context, filePath string) ([]ProcessedChunk, error) {
	// Check context
	if err := ctx.Err(); err != nil {
		return nil, ErrContextCanceled
	}

	// Validate and normalize the file path
	cleanPath, err := p.validatePath(filePath)
	if err != nil {
		return nil, err
	}

	// Read file
	content, err := os.ReadFile(cleanPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, ErrFileNotFound
		}
		return nil, &ProcessorCauseError{Message: "failed to read file", Cause: err}
	}

	// Process content
	return p.processFile(ctx, content, cleanPath)
}

// ProcessBatch implements the Processor interface.
func (p *processor) ProcessBatch(ctx context.Context, filePaths []string) ([]ProcessedChunk, error) {
	if len(filePaths) == 0 {
		return []ProcessedChunk{}, nil
	}

	// Determine concurrency
	concurrency := p.config.MaxConcurrency
	if concurrency == 0 {
		concurrency = runtime.NumCPU()
	}

	// Create semaphore for concurrency control
	sem := make(chan struct{}, concurrency)
	defer close(sem)

	// Process files in parallel
	var (
		mu      sync.Mutex
		wg      sync.WaitGroup
		results []ProcessedChunk
		errs    []error
	)

	for _, filePath := range filePaths {
		// Check context
		if err := ctx.Err(); err != nil {
			break
		}

		wg.Add(1)
		go func(path string) {
			defer wg.Done()

			// Acquire semaphore
			sem <- struct{}{}
			defer func() { <-sem }()

			// Process file
			chunks, err := p.Process(ctx, path)

			// Collect results
			mu.Lock()
			if err != nil {
				errs = append(errs, &FileProcessError{Path: path, Cause: err})
			} else {
				results = append(results, chunks...)
			}
			mu.Unlock()
		}(filePath)
	}

	// Wait for all goroutines
	wg.Wait()

	// Check for errors
	if len(errs) > 0 {
		// Return partial results with first error
		return results, errs[0]
	}

	return results, nil
}

// ProcessContent implements the Processor interface.
func (p *processor) ProcessContent(ctx context.Context, content []byte, filename string) ([]ProcessedChunk, error) {
	// Check context
	if err := ctx.Err(); err != nil {
		return nil, ErrContextCanceled
	}

	return p.processFile(ctx, content, filename)
}

// validatePath normalizes and validates a file path against path traversal attacks
// and allowed directory restrictions.
func (p *processor) validatePath(filePath string) (string, error) {
	// Normalize the path to remove redundant separators and resolve . and ..
	clean := filepath.Clean(filePath)

	// If no allowed directories are configured, reject paths containing traversal
	if len(p.allowedDirs) == 0 {
		if containsTraversal(clean) {
			return "", &PathValidationError{Path: filePath, Reason: ErrPathTraversal}
		}
		return clean, nil
	}

	// Resolve to absolute path for comparison against allowed directories
	abs, err := filepath.Abs(clean)
	if err != nil {
		return "", &PathValidationError{Path: filePath, Reason: ErrPathNotAllowed}
	}

	// Check if the resolved path is within any allowed directory
	for _, allowed := range p.allowedDirs {
		rel, err := filepath.Rel(allowed, abs)
		if err != nil {
			continue
		}
		// The relative path must not start with ".." to be within the allowed dir
		if !strings.HasPrefix(rel, "..") {
			return abs, nil
		}
	}

	return "", &PathValidationError{Path: filePath, Reason: ErrPathNotAllowed}
}

// containsTraversal checks if a cleaned path still contains path traversal components.
func containsTraversal(cleanPath string) bool {
	// After filepath.Clean, ".." segments are only present if the path truly
	// traverses above its starting point. Check for leading or embedded ".."
	// components in the cleaned path.
	for _, part := range strings.Split(cleanPath, string(filepath.Separator)) {
		if part == ".." {
			return true
		}
	}
	return false
}

// processFile processes file content through the pipeline.
func (p *processor) processFile(ctx context.Context, content []byte, filePath string) ([]ProcessedChunk, error) {
	// Check for empty content
	if len(content) == 0 {
		return nil, ErrEmptyContent
	}

	// Skip binary files if configured
	if p.config.SkipBinaryFiles && isBinary(content) {
		return nil, ErrBinaryFile
	}

	// Stage 1: Language Detection
	lang, confidence, err := p.detector.Detect(content, filePath)
	if err != nil {
		return nil, &ProcessorCauseError{Message: "language detection failed", Cause: err}
	}

	// Stage 2: Strategy Selection
	strategy := p.selectStrategy(lang, confidence)

	// Stage 3: Extract file metadata
	fileInfo, err := p.extractFileInfo(filePath, lang.String())
	if err != nil {
		// Use basic file info if extraction fails
		fileInfo = metadata.FileInfo{
			Path:     filePath,
			Language: lang.String(),
		}
	}

	// Stage 4: Chunking
	chunks, err := p.chunkContent(content, filePath, strategy, int(lang))
	if err != nil {
		return nil, &ProcessorCauseError{Message: "chunking failed", Cause: err}
	}

	// Stage 5: Metadata Extraction
	processed := make([]ProcessedChunk, 0, len(chunks))
	seen := make(map[string]bool) // For deduplication

	for _, chunk := range chunks {
		// Extract metadata
		chunkMeta := p.metaExtractor.Extract(chunk, fileInfo)

		// Create processed chunk
		pc := ProcessedChunk{
			Content:  chunk.Content,
			Metadata: chunkMeta,
			Tokens:   chunk.Tokens,
		}

		// Calculate hash for deduplication
		if p.config.EnableDeduplication {
			pc.Hash = calculateHash(chunk.Content)
			if seen[pc.Hash] {
				continue // Skip duplicate
			}
			seen[pc.Hash] = true
		}

		processed = append(processed, pc)
	}

	return processed, nil
}

// selectStrategy selects the appropriate chunking strategy based on language.
func (p *processor) selectStrategy(lang language.Language, confidence float64) string {
	// Check for explicit override
	if override, ok := p.config.StrategyOverrides[lang]; ok {
		return override
	}

	// Use default if not auto
	if p.config.DefaultStrategy != "auto" {
		return p.config.DefaultStrategy
	}

	// Auto-select based on language
	switch lang {
	case language.LanguageGo:
		return "code"
	case language.LanguageMarkdown:
		return "markdown"
	default:
		// Default to text for everything else
		return "text"
	}
}

// chunkContent performs chunking based on the selected strategy.
func (p *processor) chunkContent(content []byte, filePath string, strategy string, lang int) ([]chunking.Chunk, error) {
	// Create base metadata
	meta := chunking.Metadata{
		FilePath: filePath,
		Language: lang,
	}

	// Select and apply chunker
	switch strategy {
	case "code":
		// Use Go code chunker for Go files
		if lang == int(language.LanguageGo) {
			return p.goCodeChunker.Chunk(content, meta)
		}
		// Fall back to text for non-Go code
		return p.textChunker.Chunk(content, meta)

	case "markdown":
		// Markdown chunker will be implemented in parallel
		// For now, fall back to text chunker
		return p.textChunker.Chunk(content, meta)

	case "text":
		return p.textChunker.Chunk(content, meta)

	default:
		return p.textChunker.Chunk(content, meta)
	}
}

// extractFileInfo extracts file-level metadata.
func (p *processor) extractFileInfo(filePath string, lang string) (metadata.FileInfo, error) {
	info, err := p.metaExtractor.ExtractFileInfo(filePath)
	if err != nil {
		return metadata.FileInfo{}, err
	}

	// Override language if we have a better detection
	if lang != "" && lang != "unknown" {
		info.Language = lang
	}

	return info, nil
}

// calculateHash calculates SHA256 hash of content for deduplication.
func calculateHash(content string) string {
	hash := sha256.Sum256([]byte(content))
	return hex.EncodeToString(hash[:])
}

// isBinary performs a simple heuristic check for binary content.
func isBinary(content []byte) bool {
	if len(content) == 0 {
		return false
	}

	// Check first 512 bytes (or less if content is shorter)
	checkLen := 512
	if len(content) < checkLen {
		checkLen = len(content)
	}

	// Count null bytes
	nullCount := 0
	for i := 0; i < checkLen; i++ {
		if content[i] == 0 {
			nullCount++
		}
	}

	// If more than 10% null bytes, likely binary
	return float64(nullCount)/float64(checkLen) > 0.1
}
