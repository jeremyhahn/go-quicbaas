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
	"strings"
	"testing"
	"time"

	"github.com/jeremyhahn/go-quicbaas/pkg/rag/language"
)

// TestNew tests processor creation. Config.Validate normalises every field
// it checks instead of rejecting it, so New has no reachable error path.
func TestNew(t *testing.T) {
	tests := []struct {
		name            string
		config          Config
		wantConcurrency int
	}{
		{
			name:            "default config",
			config:          DefaultConfig(),
			wantConcurrency: DefaultConfig().MaxConcurrency,
		},
		{
			name: "custom config",
			config: Config{
				DefaultStrategy:     "text",
				MaxConcurrency:      4,
				EnableDeduplication: true,
			},
			wantConcurrency: 4,
		},
		{
			name:            "zero concurrency kept",
			config:          Config{MaxConcurrency: 0},
			wantConcurrency: 0,
		},
		{
			name:            "negative concurrency normalised to zero",
			config:          Config{MaxConcurrency: -1},
			wantConcurrency: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			proc, err := New(tt.config)
			if err != nil {
				t.Fatalf("New() error = %v", err)
			}
			p, ok := proc.(*processor)
			if !ok {
				t.Fatalf("New() returned %T, want *processor", proc)
			}
			if p.config.MaxConcurrency != tt.wantConcurrency {
				t.Errorf("MaxConcurrency = %d, want %d", p.config.MaxConcurrency, tt.wantConcurrency)
			}
		})
	}
}

// TestProcessGoFile tests processing a Go file
func TestProcessGoFile(t *testing.T) {
	// Create test Go file
	testDir := t.TempDir()
	goFile := filepath.Join(testDir, "test.go")
	goContent := `package main

import "fmt"

// Greet prints a greeting message
func Greet(name string) {
	fmt.Printf("Hello, %s!\n", name)
}

// Add adds two numbers
func Add(a, b int) int {
	return a + b
}
`
	if err := os.WriteFile(goFile, []byte(goContent), 0644); err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}

	// Create processor
	config := DefaultConfig()
	proc, err := New(config)
	if err != nil {
		t.Fatalf("Failed to create processor: %v", err)
	}

	// Process file
	ctx := context.Background()
	chunks, err := proc.Process(ctx, goFile)
	if err != nil {
		t.Fatalf("Process() error = %v", err)
	}

	// Verify chunks
	if len(chunks) == 0 {
		t.Error("Process() returned no chunks")
	}

	// Should have chunks for Greet and Add functions
	if len(chunks) < 2 {
		t.Errorf("Expected at least 2 chunks, got %d", len(chunks))
	}

	// Verify chunk content contains function names
	foundGreet := false
	foundAdd := false
	for _, chunk := range chunks {
		if strings.Contains(chunk.Content, "func Greet") {
			foundGreet = true
		}
		if strings.Contains(chunk.Content, "func Add") {
			foundAdd = true
		}
	}

	if !foundGreet {
		t.Error("Greet function not found in chunks")
	}
	if !foundAdd {
		t.Error("Add function not found in chunks")
	}

	// Verify metadata - language detection returns lowercase
	for _, chunk := range chunks {
		if chunk.Metadata.FilePath != goFile {
			t.Errorf("Expected FilePath %s, got %s", goFile, chunk.Metadata.FilePath)
		}
		// Language string from detector is lowercase "go"
		if !strings.EqualFold(chunk.Metadata.Language, "go") {
			t.Errorf("Expected Language 'go', got %s", chunk.Metadata.Language)
		}
		if chunk.Tokens == 0 {
			t.Error("Token count is zero")
		}
		if config.EnableDeduplication && chunk.Hash == "" {
			t.Error("Hash is empty with deduplication enabled")
		}
	}
}

// TestProcessBatch tests batch processing
func TestProcessBatch(t *testing.T) {
	// Create test files
	testDir := t.TempDir()

	files := map[string]string{
		"test1.go": `package main
func Test1() {
	println("test1")
}`,
		"test2.go": `package main
func Test2() {
	println("test2")
}`,
		"test3.txt": `This is a text file with enough content to create a meaningful chunk that will
meet the minimum chunk size requirements that are configured in the text chunker settings.
The content must be substantial enough to exceed the minimum token count threshold of 50 tokens
which is the default setting for the text chunker. This ensures that the chunker will process
the content correctly and create at least one valid chunk. Additional sentences help meet the
minimum requirements and validate the batch processing functionality works as expected.`,
	}

	var filePaths []string
	for name, content := range files {
		path := filepath.Join(testDir, name)
		if err := os.WriteFile(path, []byte(content), 0644); err != nil {
			t.Fatalf("Failed to create test file: %v", err)
		}
		filePaths = append(filePaths, path)
	}

	// Create processor
	config := DefaultConfig()
	config.MaxConcurrency = 2
	proc, err := New(config)
	if err != nil {
		t.Fatalf("Failed to create processor: %v", err)
	}

	// Process batch
	ctx := context.Background()
	chunks, err := proc.ProcessBatch(ctx, filePaths)
	if err != nil {
		t.Logf("ProcessBatch() completed with errors: %v", err)
	}

	// Verify chunks from all files
	if len(chunks) == 0 {
		t.Error("ProcessBatch() returned no chunks")
	}

	// Should have chunks from all files
	if len(chunks) < 3 {
		t.Errorf("Expected at least 3 chunks, got %d", len(chunks))
	}
}

// TestProcessContent tests processing content directly, one row per content
// type and per rejection.
func TestProcessContent(t *testing.T) {
	proc, err := New(DefaultConfig())
	if err != nil {
		t.Fatalf("Failed to create processor: %v", err)
	}

	// Long enough to meet the text chunker's minimum chunk size.
	longText := strings.Repeat("This is a test sentence that adds enough content. ", 15)

	tests := []struct {
		name     string
		content  []byte
		filename string
		wantLang string
		wantErr  error
	}{
		{
			name:     "go code",
			content:  []byte("package main\n\nfunc Hello() {\n\tprintln(\"hello world from the test function\")\n}\n"),
			filename: "test.go",
			wantLang: "go",
		},
		{
			name:     "markdown",
			content:  []byte("# Title\n\n" + longText),
			filename: "test.md",
			wantLang: "markdown",
		},
		{
			name:     "plain text",
			content:  []byte(longText),
			filename: "test.txt",
		},
		{
			name:     "python code",
			content:  []byte("def test():\n    " + longText + "\n    pass\n"),
			filename: "test.py",
			wantLang: "python",
		},
		{
			name:     "javascript",
			content:  []byte("function test() { /* " + longText + " */ }\n"),
			filename: "test.js",
			wantLang: "javascript",
		},
		{
			name:     "empty content",
			content:  []byte{},
			filename: "empty.txt",
			wantErr:  ErrEmptyContent,
		},
		{
			name:     "binary content",
			content:  makeBytes(100, 20),
			filename: "binary.bin",
			wantErr:  ErrBinaryFile,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			chunks, err := proc.ProcessContent(context.Background(), tt.content, tt.filename)
			if tt.wantErr != nil {
				if !errors.Is(err, tt.wantErr) {
					t.Fatalf("ProcessContent() error = %v, want %v", err, tt.wantErr)
				}
				if chunks != nil {
					t.Errorf("ProcessContent() returned %d chunks with an error", len(chunks))
				}
				return
			}
			if err != nil {
				t.Fatalf("ProcessContent() error = %v", err)
			}
			if len(chunks) == 0 {
				t.Fatal("ProcessContent() returned no chunks")
			}
			for i, chunk := range chunks {
				if chunk.Content == "" {
					t.Errorf("Chunk %d has empty content", i)
				}
				if chunk.Tokens == 0 {
					t.Errorf("Chunk %d token count is zero", i)
				}
				if tt.wantLang != "" && !strings.EqualFold(chunk.Metadata.Language, tt.wantLang) {
					t.Errorf("Chunk %d Language = %s, want %s", i, chunk.Metadata.Language, tt.wantLang)
				}
			}
		})
	}
}

// TestStrategySelection tests strategy selection: an override wins, then a
// non-auto default, then auto-selection by language.
func TestStrategySelection(t *testing.T) {
	overrides := map[language.Language]string{
		language.LanguageGo:       "text",
		language.LanguageMarkdown: "text",
	}

	tests := []struct {
		name            string
		defaultStrategy string
		overrides       map[language.Language]string
		lang            language.Language
		expected        string
	}{
		{name: "auto go", lang: language.LanguageGo, expected: "code"},
		{name: "auto markdown", lang: language.LanguageMarkdown, expected: "markdown"},
		{name: "auto python falls back to text", lang: language.LanguagePython, expected: "text"},
		{name: "auto javascript falls back to text", lang: language.LanguageJavaScript, expected: "text"},
		{name: "auto unknown", lang: language.LanguageUnknown, expected: "text"},
		{name: "override go", overrides: overrides, lang: language.LanguageGo, expected: "text"},
		{name: "override markdown", overrides: overrides, lang: language.LanguageMarkdown, expected: "text"},
		{name: "no override for unknown", overrides: overrides, lang: language.LanguageUnknown, expected: "text"},
		{name: "non-auto default", defaultStrategy: "text", lang: language.LanguageGo, expected: "text"},
		{name: "override beats non-auto default", defaultStrategy: "code", overrides: overrides, lang: language.LanguageGo, expected: "text"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			config := DefaultConfig()
			if tt.defaultStrategy != "" {
				config.DefaultStrategy = tt.defaultStrategy
			}
			config.StrategyOverrides = tt.overrides
			proc := &processor{config: config}

			if strategy := proc.selectStrategy(tt.lang, 1.0); strategy != tt.expected {
				t.Errorf("Expected strategy %s, got %s", tt.expected, strategy)
			}
		})
	}
}

// TestDeduplication tests hash-based deduplication: with it enabled every
// chunk carries a hash and identical chunks are dropped; with it disabled no
// hash is set and identical chunks are kept.
func TestDeduplication(t *testing.T) {
	duplicated := []byte(`package main

func Dup() {
	println("duplicate body for the deduplication test")
}

func Dup() {
	println("duplicate body for the deduplication test")
}
`)

	tests := []struct {
		name       string
		enabled    bool
		wantChunks int
	}{
		{name: "enabled", enabled: true, wantChunks: 1},
		{name: "disabled", enabled: false, wantChunks: 2},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			config := DefaultConfig()
			config.EnableDeduplication = tt.enabled
			proc, err := New(config)
			if err != nil {
				t.Fatalf("Failed to create processor: %v", err)
			}

			chunks, err := proc.ProcessContent(context.Background(), duplicated, "dup.go")
			if err != nil {
				t.Fatalf("ProcessContent() error = %v", err)
			}
			if len(chunks) != tt.wantChunks {
				t.Fatalf("got %d chunks, want %d", len(chunks), tt.wantChunks)
			}
			for i, chunk := range chunks {
				if tt.enabled && chunk.Hash == "" {
					t.Errorf("Chunk %d hash is empty with deduplication enabled", i)
				}
				if !tt.enabled && chunk.Hash != "" {
					t.Errorf("Chunk %d hash is set with deduplication disabled", i)
				}
			}
		})
	}
}

// TestContextCancellation tests that Process and ProcessContent reject a
// canceled context before doing any work.
func TestContextCancellation(t *testing.T) {
	testFile := filepath.Join(t.TempDir(), "test.txt")
	if err := os.WriteFile(testFile, []byte("test content"), 0644); err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}

	proc, err := New(DefaultConfig())
	if err != nil {
		t.Fatalf("Failed to create processor: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	tests := []struct {
		name string
		run  func() ([]ProcessedChunk, error)
	}{
		{
			name: "Process",
			run:  func() ([]ProcessedChunk, error) { return proc.Process(ctx, testFile) },
		},
		{
			name: "ProcessContent",
			run: func() ([]ProcessedChunk, error) {
				return proc.ProcessContent(ctx, []byte("package main\nfunc test() {}"), "test.go")
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			chunks, err := tt.run()
			if !errors.Is(err, ErrContextCanceled) {
				t.Errorf("Expected ErrContextCanceled, got %v", err)
			}
			if chunks != nil {
				t.Error("Expected nil chunks with canceled context")
			}
		})
	}
}

// TestFileNotFound tests handling of non-existent files
func TestFileNotFound(t *testing.T) {
	config := DefaultConfig()
	proc, err := New(config)
	if err != nil {
		t.Fatalf("Failed to create processor: %v", err)
	}

	ctx := context.Background()
	_, err = proc.Process(ctx, "/nonexistent/file.txt")
	if err != ErrFileNotFound {
		t.Errorf("Expected ErrFileNotFound, got %v", err)
	}
}

// TestBinaryFileSkipping tests binary file detection
func TestBinaryFileSkipping(t *testing.T) {
	testDir := t.TempDir()
	binaryFile := filepath.Join(testDir, "test.bin")

	// Create binary content (lots of null bytes)
	binaryContent := make([]byte, 1000)
	for i := range binaryContent {
		if i%2 == 0 {
			binaryContent[i] = 0 // Null bytes
		} else {
			binaryContent[i] = byte(i % 256)
		}
	}

	if err := os.WriteFile(binaryFile, binaryContent, 0644); err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}

	config := DefaultConfig()
	config.SkipBinaryFiles = true
	proc, err := New(config)
	if err != nil {
		t.Fatalf("Failed to create processor: %v", err)
	}

	ctx := context.Background()
	_, err = proc.Process(ctx, binaryFile)
	if err == nil {
		t.Error("Expected error for binary file")
	}
	if !strings.Contains(err.Error(), "binary") {
		t.Errorf("Expected binary file error, got: %v", err)
	}
}

// TestBatchWithErrors tests batch edge cases: an empty list, a missing file
// next to a valid one, and a context canceled before the batch starts.
func TestBatchWithErrors(t *testing.T) {
	validFile := filepath.Join(t.TempDir(), "valid.txt")
	if err := os.WriteFile(validFile, []byte(strings.Repeat("valid content with enough text ", 10)), 0644); err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}
	invalidFile := "/nonexistent/invalid.txt"

	proc, err := New(DefaultConfig())
	if err != nil {
		t.Fatalf("Failed to create processor: %v", err)
	}

	canceled, cancel := context.WithCancel(context.Background())
	cancel()

	tests := []struct {
		name        string
		ctx         context.Context
		files       []string
		wantChunks  bool
		wantErrPath string
		ignoreErr   bool
	}{
		{name: "empty list", ctx: context.Background(), files: []string{}},
		{
			name:        "missing file keeps partial results",
			ctx:         context.Background(),
			files:       []string{validFile, invalidFile},
			wantChunks:  true,
			wantErrPath: invalidFile,
		},
		// A canceled batch starts no work. Whether it reports an error is
		// not asserted: today it returns none.
		{name: "canceled context", ctx: canceled, files: []string{validFile}, ignoreErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			chunks, err := proc.ProcessBatch(tt.ctx, tt.files)
			if tt.wantErrPath != "" {
				var fileErr *FileProcessError
				if !errors.As(err, &fileErr) {
					t.Fatalf("ProcessBatch() error = %v, want *FileProcessError", err)
				}
				if fileErr.Path != tt.wantErrPath || !errors.Is(fileErr.Cause, ErrFileNotFound) {
					t.Errorf("FileProcessError = {%s, %v}, want {%s, %v}", fileErr.Path, fileErr.Cause, tt.wantErrPath, ErrFileNotFound)
				}
			} else if !tt.ignoreErr && err != nil {
				t.Errorf("ProcessBatch() error = %v", err)
			}
			if gotChunks := len(chunks) > 0; gotChunks != tt.wantChunks {
				t.Errorf("ProcessBatch() returned %d chunks, want chunks: %v", len(chunks), tt.wantChunks)
			}
		})
	}
}

// TestConcurrencyLimit tests concurrency limiting
func TestConcurrencyLimit(t *testing.T) {
	testDir := t.TempDir()

	// Create multiple test files
	var filePaths []string
	for i := 0; i < 10; i++ {
		filename := filepath.Join(testDir, "test"+string(rune('0'+i))+".txt")
		content := "Test content for file with enough text to meet minimum chunk size requirements."
		if err := os.WriteFile(filename, []byte(content), 0644); err != nil {
			t.Fatalf("Failed to create test file: %v", err)
		}
		filePaths = append(filePaths, filename)
	}

	config := DefaultConfig()
	config.MaxConcurrency = 2 // Limit to 2 concurrent operations
	proc, err := New(config)
	if err != nil {
		t.Fatalf("Failed to create processor: %v", err)
	}

	ctx := context.Background()
	start := time.Now()
	_, err = proc.ProcessBatch(ctx, filePaths)
	duration := time.Since(start)

	if err != nil {
		t.Logf("Batch processing completed with some errors: %v", err)
	}

	t.Logf("Processed %d files in %v with concurrency limit of 2", len(filePaths), duration)
}

// TestCalculateHash tests that the hash is a stable SHA-256 hex digest.
func TestCalculateHash(t *testing.T) {
	hash1 := calculateHash("test content")
	hash2 := calculateHash("test content")
	hash3 := calculateHash("different content")

	if hash1 != hash2 {
		t.Error("Same content should produce same hash")
	}
	if hash1 == hash3 {
		t.Error("Different content should produce different hash")
	}
	if len(hash1) != 64 {
		t.Errorf("Expected SHA256 hash length 64, got %d", len(hash1))
	}
}

// TestIsBinary tests binary detection against the 10% null-byte threshold.
func TestIsBinary(t *testing.T) {
	tests := []struct {
		name    string
		content []byte
		want    bool
	}{
		{"text", []byte("This is plain text"), false},
		{"utf8", []byte("hello 世界"), false},
		{"empty", []byte{}, false},
		{"few nulls", makeBytes(100, 5), false},  // 5% nulls
		{"many nulls", makeBytes(100, 20), true}, // 20% nulls
		{"short text with nulls", append([]byte("text"), make([]byte, 10)...), true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isBinary(tt.content); got != tt.want {
				t.Errorf("isBinary() = %v, want %v", got, tt.want)
			}
		})
	}
}

// TestChunkContent_AllStrategies tests all chunking strategies
func TestChunkContent_AllStrategies(t *testing.T) {
	proc, err := New(DefaultConfig())
	if err != nil {
		t.Fatal(err)
	}
	p := proc.(*processor)

	// Longer content to meet minimum chunk size
	longText := strings.Repeat("This is test content that meets minimum size requirements. ", 10)

	tests := []struct {
		name     string
		content  []byte
		filename string
		strategy string
		lang     int
	}{
		{
			name:     "go code chunking",
			content:  []byte("package main\n\nfunc test() { println(\"hello\") }\n"),
			filename: "test.go",
			strategy: "code",
			lang:     int(language.LanguageGo),
		},
		{
			name:     "text chunking",
			content:  []byte(longText),
			filename: "test.txt",
			strategy: "text",
			lang:     int(language.LanguageUnknown),
		},
		{
			name:     "markdown chunking",
			content:  []byte("# Title\n\n" + longText),
			filename: "test.md",
			strategy: "markdown",
			lang:     int(language.LanguageMarkdown),
		},
		{
			name:     "code strategy for non-go",
			content:  []byte("def test(): # " + longText + "\n    pass"),
			filename: "test.py",
			strategy: "code",
			lang:     int(language.LanguagePython),
		},
		{
			name:     "unknown strategy defaults to text",
			content:  []byte(longText),
			filename: "test.txt",
			strategy: "unknown",
			lang:     int(language.LanguageUnknown),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			chunks, err := p.chunkContent(tt.content, tt.filename, tt.strategy, tt.lang)
			if err != nil {
				t.Fatalf("chunkContent() error = %v", err)
			}
			if len(chunks) == 0 {
				t.Error("chunkContent() returned no chunks")
			}
		})
	}
}

// Helper to create byte array with specific null byte count
func makeBytes(length, nulls int) []byte {
	b := make([]byte, length)
	for i := 0; i < nulls && i < length; i++ {
		b[i] = 0
	}
	for i := nulls; i < length; i++ {
		b[i] = byte('a' + (i % 26))
	}
	return b
}
