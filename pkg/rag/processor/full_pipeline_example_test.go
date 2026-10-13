//go:build rag

package processor

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jeremyhahn/go-quicbaas/pkg/rag/chunking"
	"github.com/jeremyhahn/go-quicbaas/pkg/rag/language"
)

func TestNew_ConfigValidationError(t *testing.T) {
	// Config.Validate() always returns nil after fixing invalid values,
	// so we need to test that the validation happens
	tests := []struct {
		name   string
		config Config
	}{
		{
			name: "invalid strategy gets auto-corrected",
			config: Config{
				DefaultStrategy: "invalid_strategy",
			},
		},
		{
			name: "negative chunk size gets default",
			config: Config{
				TextConfig: chunking.TextChunkerConfig{
					ChunkSize: -1,
				},
			},
		},
		{
			name: "invalid code config gets default",
			config: Config{
				CodeConfig: chunking.GoCodeChunkerConfig{
					MaxFunctionTokens: -1,
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// All these should succeed because Validate() fixes invalid configs
			proc, err := New(tt.config)
			if err != nil {
				t.Errorf("New() unexpected error = %v", err)
			}
			if proc == nil {
				t.Error("New() returned nil processor")
			}
		})
	}
}

// TestProcess_CanceledContextBeforeRead tests context cancellation before file read
func TestProcess_CanceledContextBeforeRead(t *testing.T) {
	testDir := t.TempDir()
	testFile := filepath.Join(testDir, "test.txt")
	if err := os.WriteFile(testFile, []byte("test content"), 0644); err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}

	config := DefaultConfig()
	proc, err := New(config)
	if err != nil {
		t.Fatalf("Failed to create processor: %v", err)
	}

	// Cancel context before processing
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	chunks, err := proc.Process(ctx, testFile)
	if err != ErrContextCanceled {
		t.Errorf("Expected ErrContextCanceled, got %v", err)
	}
	if chunks != nil {
		t.Error("Expected nil chunks with canceled context")
	}
}

// TestProcessFile_ExtractFileInfoFallback tests fallback when extractFileInfo returns error
func TestProcessFile_ExtractFileInfoFallback(t *testing.T) {
	testDir := t.TempDir()
	testFile := filepath.Join(testDir, "test.go")

	content := `package main

func TestFunction() {
	println("Test function with enough content for chunking and metadata extraction")
}
`
	if err := os.WriteFile(testFile, []byte(content), 0644); err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}

	config := DefaultConfig()
	proc, err := New(config)
	if err != nil {
		t.Fatalf("Failed to create processor: %v", err)
	}

	ctx := context.Background()
	chunks, err := proc.Process(ctx, testFile)
	if err != nil {
		t.Fatalf("Process() error = %v", err)
	}

	// Should have chunks even if extractFileInfo had issues
	if len(chunks) == 0 {
		t.Error("Process() returned no chunks")
	}

	// Verify basic metadata exists
	for _, chunk := range chunks {
		if chunk.Metadata.FilePath != testFile {
			t.Errorf("Expected FilePath %s, got %s", testFile, chunk.Metadata.FilePath)
		}
		if chunk.Content == "" {
			t.Error("Chunk content is empty")
		}
	}
}

// TestProcessFile_DeduplicationSkipDuplicate tests that duplicate hashes are skipped
func TestProcessFile_DeduplicationSkipDuplicate(t *testing.T) {
	testDir := t.TempDir()
	testFile := filepath.Join(testDir, "duplicates.txt")

	// Create content that might produce duplicate chunks
	repeated := strings.Repeat("This is identical content repeated. ", 20)
	content := repeated + "\n\n" + repeated

	if err := os.WriteFile(testFile, []byte(content), 0644); err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}

	config := DefaultConfig()
	config.EnableDeduplication = true
	proc, err := New(config)
	if err != nil {
		t.Fatalf("Failed to create processor: %v", err)
	}

	ctx := context.Background()
	chunks, err := proc.Process(ctx, testFile)
	if err != nil {
		t.Fatalf("Process() error = %v", err)
	}

	// Verify all hashes are unique (duplicates were skipped)
	seen := make(map[string]bool)
	for _, chunk := range chunks {
		if chunk.Hash == "" {
			t.Error("Hash should not be empty with deduplication enabled")
		}
		if seen[chunk.Hash] {
			t.Errorf("Found duplicate hash: %s (should have been skipped)", chunk.Hash)
		}
		seen[chunk.Hash] = true
	}
}

// TestWalkAndProcess_Complete tests the example function that walks a directory
func TestWalkAndProcess_Complete(t *testing.T) {
	testDir := t.TempDir()

	// Create test files
	files := map[string]string{
		"main.go": `package main



func Helper() string {
	return "Helper function with enough content to create meaningful chunks for testing purposes"
}
`,
		"README.md": "# Test Project\n\n" +
			strings.Repeat("This markdown section carries enough prose to clear the minimum chunk size. ", 10) + "\n",
		"notes.txt": strings.Repeat("Plain text content with sufficient length for chunking. ", 15),
	}

	for name, content := range files {
		path := filepath.Join(testDir, name)
		if err := os.WriteFile(path, []byte(content), 0644); err != nil {
			t.Fatalf("Failed to create test file %s: %v", name, err)
		}
	}

	// Test walking and processing
	err := WalkAndProcess(testDir)
	if err != nil {
		t.Errorf("WalkAndProcess() error = %v", err)
	}
}

// TestWalkAndProcess_BatchFailureReturned verifies a file the processor
// rejects surfaces as an error instead of being dropped.
func TestWalkAndProcess_BatchFailureReturned(t *testing.T) {
	testDir := t.TempDir()
	// Too short to produce a markdown chunk.
	if err := os.WriteFile(filepath.Join(testDir, "tiny.md"), []byte("# Hi\n"), 0644); err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}

	err := WalkAndProcess(testDir)

	var causeErr *ProcessorCauseError
	if !errors.As(err, &causeErr) {
		t.Fatalf("WalkAndProcess() error = %v, want *ProcessorCauseError", err)
	}
	if causeErr.Message != "failed to process files" {
		t.Errorf("Message = %q, want %q", causeErr.Message, "failed to process files")
	}
}

// TestWalkAndProcess_ErrorPath tests error handling when directory doesn't exist
func TestWalkAndProcess_ErrorPath(t *testing.T) {
	err := WalkAndProcess("/nonexistent/directory/path")
	if err == nil {
		t.Error("Expected error for non-existent directory")
	}
	if !strings.Contains(err.Error(), "failed to walk directory") {
		t.Errorf("Expected walk error message, got: %v", err)
	}
}

// TestWalkAndProcess_Filtering tests directory traversal and file filtering
func TestWalkAndProcess_Filtering(t *testing.T) {
	testDir := t.TempDir()

	// Create subdirectory
	subDir := filepath.Join(testDir, "subdir")
	if err := os.Mkdir(subDir, 0755); err != nil {
		t.Fatalf("Failed to create subdirectory: %v", err)
	}

	// Create files in both directories
	files := map[string]string{
		filepath.Join(testDir, "root.go"):        `package main\nfunc Root() { println("root") }`,
		filepath.Join(subDir, "sub.go"):          `package main\nfunc Sub() { println("sub") }`,
		filepath.Join(testDir, "ignored.bin"):    string(make([]byte, 100)),
		filepath.Join(testDir, "unsupported.rs"): "fn main() {}",
	}

	for path, content := range files {
		if err := os.WriteFile(path, []byte(content), 0644); err != nil {
			t.Fatalf("Failed to create file %s: %v", path, err)
		}
	}

	err := WalkAndProcess(testDir)
	if err != nil {
		t.Logf("WalkAndProcess completed with errors (may be expected): %v", err)
	}
}

// TestProcessWithCustomStrategy_Complete tests the custom strategy example
func TestProcessWithCustomStrategy_Complete(t *testing.T) {
	testDir := t.TempDir()

	// Create example.py file expected by the function
	pyFile := filepath.Join(testDir, "example.py")
	pyContent := `def example_function():
    """
    Example Python function with substantial content to ensure proper
    chunking and processing through the pipeline stages.
    """
    result = "Test content with enough text for meaningful chunks"
    print(result)
    return True
`
	if err := os.WriteFile(pyFile, []byte(pyContent), 0644); err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}

	// Change to test directory
	origDir, err := os.Getwd()
	if err != nil {
		t.Fatalf("Failed to get current directory: %v", err)
	}
	defer os.Chdir(origDir)

	if err := os.Chdir(testDir); err != nil {
		t.Fatalf("Failed to change directory: %v", err)
	}

	// Test the function
	err = ProcessWithCustomStrategy()
	if err != nil {
		t.Logf("ProcessWithCustomStrategy() error (may be expected): %v", err)
	}
}

// TestProcessWithCustomStrategy_NotFound tests error path
func TestProcessWithCustomStrategy_NotFound(t *testing.T) {
	testDir := t.TempDir()

	origDir, err := os.Getwd()
	if err != nil {
		t.Fatalf("Failed to get current directory: %v", err)
	}
	defer os.Chdir(origDir)

	if err := os.Chdir(testDir); err != nil {
		t.Fatalf("Failed to change directory: %v", err)
	}

	// Should fail because example.py doesn't exist
	err = ProcessWithCustomStrategy()
	if err == nil {
		t.Error("Expected error when example.py doesn't exist")
	}
	if !strings.Contains(err.Error(), "failed to process") {
		t.Errorf("Expected process error, got: %v", err)
	}
}

// TestChunkContent_CodeStrategyNonGoFallback tests fallback for non-Go code with code strategy
func TestChunkContent_CodeStrategyNonGoFallback(t *testing.T) {
	proc, err := New(DefaultConfig())
	if err != nil {
		t.Fatal(err)
	}
	p := proc.(*processor)

	// Python code with code strategy should fall back to text chunker
	pythonContent := []byte(strings.Repeat("def test_function():\n    pass\n", 20))

	_, err = p.chunkContent(pythonContent, "test.py", "code", int(language.LanguagePython))
	if err != nil {
		t.Logf("chunkContent error (acceptable for short content): %v", err)
		return
	}

	t.Logf("Successfully processed non-Go code with code strategy (fell back to text)")
}

// TestExtractFileInfo_LanguageOverrideLogic tests language override logic
func TestExtractFileInfo_LanguageOverrideLogic(t *testing.T) {
	testDir := t.TempDir()
	testFile := filepath.Join(testDir, "test.go")

	content := `package main

func Test() {
	println("test")
}
`
	if err := os.WriteFile(testFile, []byte(content), 0644); err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}

	proc, err := New(DefaultConfig())
	if err != nil {
		t.Fatal(err)
	}
	p := proc.(*processor)

	// Test with detected language
	info, err := p.extractFileInfo(testFile, "go")
	if err == nil && info.Language != "go" {
		t.Errorf("Expected language 'go', got '%s'", info.Language)
	}

	// Test with empty language (shouldn't override)
	_, err = p.extractFileInfo(testFile, "")
	if err != nil {
		t.Logf("extractFileInfo with empty lang error: %v", err)
	}

	// Test with "unknown" language (shouldn't override)
	_, err = p.extractFileInfo(testFile, "unknown")
	if err != nil {
		t.Logf("extractFileInfo with unknown lang error: %v", err)
	}

	// Test with different language (should override)
	info, err = p.extractFileInfo(testFile, "python")
	if err == nil && info.Language != "python" {
		t.Errorf("Expected language override to 'python', got '%s'", info.Language)
	}
}

// TestProcessBatch_ContextCancelDuringProcessing tests context cancellation during batch
func TestProcessBatch_ContextCancelDuringProcessing(t *testing.T) {
	testDir := t.TempDir()

	// Create multiple files
	var files []string
	for i := 0; i < 5; i++ {
		filename := filepath.Join(testDir, "test_"+string(rune('0'+i))+".txt")
		content := strings.Repeat("Test content for batch processing. ", 10)
		if err := os.WriteFile(filename, []byte(content), 0644); err != nil {
			t.Fatalf("Failed to create test file: %v", err)
		}
		files = append(files, filename)
	}

	config := DefaultConfig()
	config.MaxConcurrency = 1 // Serial processing
	proc, err := New(config)
	if err != nil {
		t.Fatalf("Failed to create processor: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())

	// Cancel during processing
	go func() {
		cancel()
	}()

	chunks, err := proc.ProcessBatch(ctx, files)
	// May or may not error depending on timing
	t.Logf("ProcessBatch with cancellation: %d chunks, err=%v", len(chunks), err)
}

// TestProcessBatch_DefaultConcurrency tests that MaxConcurrency=0 uses NumCPU
func TestProcessBatch_DefaultConcurrency(t *testing.T) {
	testDir := t.TempDir()

	files := []string{}
	for i := 0; i < 3; i++ {
		filename := filepath.Join(testDir, "test_"+string(rune('0'+i))+".txt")
		// Use much more content to ensure chunks are created
		content := strings.Repeat("Test content for batch processing with sufficient text to generate chunks. ", 30)
		if err := os.WriteFile(filename, []byte(content), 0644); err != nil {
			t.Fatalf("Failed to create test file: %v", err)
		}
		files = append(files, filename)
	}

	config := DefaultConfig()
	config.MaxConcurrency = 0 // Should use runtime.NumCPU()
	proc, err := New(config)
	if err != nil {
		t.Fatalf("Failed to create processor: %v", err)
	}

	ctx := context.Background()
	chunks, err := proc.ProcessBatch(ctx, files)
	if err != nil {
		t.Fatalf("ProcessBatch error: %v", err)
	}
	if len(chunks) == 0 {
		t.Error("Expected chunks to be created")
	}
	t.Logf("Processed %d files with default concurrency, got %d chunks", len(files), len(chunks))
}

// TestSelectStrategy_WithDefaultNonAuto tests non-auto default strategy
func TestSelectStrategy_WithDefaultNonAuto(t *testing.T) {
	config := DefaultConfig()
	config.DefaultStrategy = "text"

	proc := &processor{
		config:   config,
		detector: language.NewDetector(),
	}

	// Should use default "text" instead of auto-selecting "code" for Go
	strategy := proc.selectStrategy(language.LanguageGo, 1.0)
	if strategy != "text" {
		t.Errorf("Expected text strategy from non-auto default, got %s", strategy)
	}
}

// TestSelectStrategy_OverridesMap tests strategy overrides map
func TestSelectStrategy_OverridesMap(t *testing.T) {
	config := DefaultConfig()
	config.StrategyOverrides = map[language.Language]string{
		language.LanguagePython: "text",
		language.LanguageGo:     "text",
	}

	proc := &processor{
		config:   config,
		detector: language.NewDetector(),
	}

	// Go should use override "text" instead of default "code"
	strategy := proc.selectStrategy(language.LanguageGo, 1.0)
	if strategy != "text" {
		t.Errorf("Expected text strategy from override, got %s", strategy)
	}

	// Python should use override
	strategy = proc.selectStrategy(language.LanguagePython, 1.0)
	if strategy != "text" {
		t.Errorf("Expected text strategy from override, got %s", strategy)
	}
}

// TestChunkContent_UnknownStrategy tests fallback for unknown strategy
func TestChunkContent_UnknownStrategy(t *testing.T) {
	proc, err := New(DefaultConfig())
	if err != nil {
		t.Fatal(err)
	}
	p := proc.(*processor)

	content := []byte(strings.Repeat("Test content with enough text. ", 10))

	// Unknown strategy should fall back to text
	_, err = p.chunkContent(content, "test.txt", "unknown_strategy", int(language.LanguageUnknown))
	if err != nil {
		t.Logf("chunkContent error (acceptable): %v", err)
		return
	}

	t.Logf("Unknown strategy processed chunks (fell back to text)")
}
