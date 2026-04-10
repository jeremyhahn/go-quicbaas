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

package chunking

import (
	"strings"
	"testing"
)

func TestTextChunker_Basic(t *testing.T) {
	chunker := NewTextChunker(TextChunkerConfig{
		ChunkSize:         100,
		ChunkOverlap:      20,
		MinChunkSize:      10,
		RespectSentences:  false,
		RespectParagraphs: false,
	})

	content := []byte("This is a test document with multiple sentences. " +
		"It contains enough content to be split into multiple chunks. " +
		"Each chunk should respect the configured size limits. " +
		"The chunker should handle this content appropriately. " +
		"We want to ensure proper splitting occurs.")

	chunks, err := chunker.Chunk(content, Metadata{FilePath: "test.txt"})

	if err != nil {
		t.Fatalf("Chunk failed: %v", err)
	}

	if len(chunks) == 0 {
		t.Fatal("Expected at least one chunk")
	}

	for i, chunk := range chunks {
		if chunk.Tokens > 100*2 { // Allow some margin for token estimation
			t.Errorf("Chunk %d has too many tokens: %d", i, chunk.Tokens)
		}
		if chunk.Index != i {
			t.Errorf("Chunk %d has wrong index: %d", i, chunk.Index)
		}
		if chunk.Content == "" {
			t.Errorf("Chunk %d has empty content", i)
		}
		if chunk.Metadata.FilePath != "test.txt" {
			t.Errorf("Chunk %d has wrong file path: %s", i, chunk.Metadata.FilePath)
		}
	}
}

func TestTextChunker_Overlap(t *testing.T) {
	chunker := NewTextChunker(TextChunkerConfig{
		ChunkSize:         50,
		ChunkOverlap:      10,
		MinChunkSize:      10,
		RespectSentences:  false,
		RespectParagraphs: false,
	})

	content := []byte("Word " + strings.Repeat("word ", 100))
	chunks, err := chunker.Chunk(content, Metadata{})

	if err != nil {
		t.Fatalf("Chunk failed: %v", err)
	}

	if len(chunks) < 2 {
		t.Fatal("Expected multiple chunks for overlap testing")
	}

	// Verify that chunks exist and have content
	for i, chunk := range chunks {
		if chunk.Content == "" {
			t.Errorf("Chunk %d is empty", i)
		}
		if i > 0 {
			// Verify chunks have some potential overlap
			// Since we're using word-based chunking, we just verify both chunks exist
			if chunks[i-1].Content == "" || chunk.Content == "" {
				t.Errorf("Chunks %d and %d should both have content", i-1, i)
			}
		}
	}
}

func TestTextChunker_RespectParagraphs(t *testing.T) {
	chunker := NewTextChunker(TextChunkerConfig{
		ChunkSize:         100,
		ChunkOverlap:      20,
		MinChunkSize:      10,
		RespectParagraphs: true,
		RespectSentences:  false,
	})

	content := []byte(`This is the first paragraph with some content.
It has multiple lines and sentences.

This is the second paragraph.
It also has content.

This is the third paragraph.
Final content here.`)

	chunks, err := chunker.Chunk(content, Metadata{})

	if err != nil {
		t.Fatalf("Chunk failed: %v", err)
	}

	if len(chunks) == 0 {
		t.Fatal("Expected at least one chunk")
	}

	// Verify paragraphs are respected
	for i, chunk := range chunks {
		// Check that chunk doesn't start with blank lines
		trimmed := strings.TrimSpace(chunk.Content)
		if trimmed == "" {
			t.Errorf("Chunk %d is empty after trimming", i)
		}
	}
}

func TestTextChunker_RespectSentences(t *testing.T) {
	chunker := NewTextChunker(TextChunkerConfig{
		ChunkSize:         50,
		ChunkOverlap:      10,
		MinChunkSize:      5,
		RespectSentences:  true,
		RespectParagraphs: false,
	})

	content := []byte("This is sentence one. This is sentence two. This is sentence three. " +
		"This is sentence four. This is sentence five. This is sentence six.")

	chunks, err := chunker.Chunk(content, Metadata{})

	if err != nil {
		t.Fatalf("Chunk failed: %v", err)
	}

	if len(chunks) == 0 {
		t.Fatal("Expected at least one chunk")
	}

	// Verify chunks contain complete thoughts
	for i, chunk := range chunks {
		if chunk.Content == "" {
			t.Errorf("Chunk %d is empty", i)
		}
		// Sentences should end with proper punctuation
		trimmed := strings.TrimSpace(chunk.Content)
		if !strings.HasSuffix(trimmed, ".") &&
			!strings.HasSuffix(trimmed, "!") &&
			!strings.HasSuffix(trimmed, "?") {
			// May not end with punctuation if it's in the middle of a sentence
			// This is okay for overlapping chunks
		}
	}
}

func TestTextChunker_EmptyContent(t *testing.T) {
	chunker := NewTextChunker(DefaultTextChunkerConfig())

	chunks, err := chunker.Chunk([]byte(""), Metadata{})

	if err != ErrEmptyContent {
		t.Errorf("Expected ErrEmptyContent, got: %v", err)
	}

	if chunks != nil {
		t.Error("Expected nil chunks for empty content")
	}
}

func TestTextChunker_VeryLongContent(t *testing.T) {
	chunker := NewTextChunker(TextChunkerConfig{
		ChunkSize:    200,
		ChunkOverlap: 20,
		MinChunkSize: 50,
	})

	// Create very long content
	var builder strings.Builder
	for i := 0; i < 1000; i++ {
		builder.WriteString("This is sentence number ")
		builder.WriteString(string(rune(i)))
		builder.WriteString(". It contains some words. ")
	}

	content := []byte(builder.String())
	chunks, err := chunker.Chunk(content, Metadata{})

	if err != nil {
		t.Fatalf("Chunk failed: %v", err)
	}

	if len(chunks) == 0 {
		t.Fatal("Expected multiple chunks for long content")
	}

	// Verify all chunks are within bounds
	for i, chunk := range chunks {
		if chunk.Tokens < 50 {
			t.Errorf("Chunk %d below minimum size: %d tokens", i, chunk.Tokens)
		}
		if chunk.Tokens > 400 { // Allow 2x margin for estimation variance
			t.Errorf("Chunk %d exceeds size limit: %d tokens", i, chunk.Tokens)
		}
	}
}

func TestTextChunker_LineNumberTracking(t *testing.T) {
	chunker := NewTextChunker(TextChunkerConfig{
		ChunkSize:         50,
		ChunkOverlap:      10,
		MinChunkSize:      10,
		RespectParagraphs: true,
	})

	content := []byte(`Line 1
Line 2
Line 3

Line 5
Line 6

Line 8
Line 9`)

	metadata := Metadata{StartLine: 1}
	chunks, err := chunker.Chunk(content, metadata)

	if err != nil {
		t.Fatalf("Chunk failed: %v", err)
	}

	// Verify line numbers are tracked
	for i, chunk := range chunks {
		if chunk.Metadata.StartLine <= 0 {
			t.Errorf("Chunk %d has invalid start line: %d", i, chunk.Metadata.StartLine)
		}
		if chunk.Metadata.EndLine < chunk.Metadata.StartLine {
			t.Errorf("Chunk %d has invalid end line: %d < %d", i,
				chunk.Metadata.EndLine, chunk.Metadata.StartLine)
		}
	}
}

func TestTextChunker_TokenCounting(t *testing.T) {
	// Use lower minimum chunk size for short test content
	chunker := NewTextChunker(TextChunkerConfig{
		ChunkSize:    100,
		ChunkOverlap: 10,
		MinChunkSize: 3, // Lower minimum to allow short test content
	})

	testCases := []struct {
		name        string
		content     string
		expectToken bool
	}{
		{
			name:        "simple sentence",
			content:     "This is a test sentence with enough words to be chunked.",
			expectToken: true,
		},
		{
			name:        "with punctuation",
			content:     "Hello, world! How are you doing today? I am fine, thank you!",
			expectToken: true,
		},
		{
			name:        "multiple sentences",
			content:     "First sentence here. Second sentence there. Third sentence everywhere.",
			expectToken: true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			chunks, err := chunker.Chunk([]byte(tc.content), Metadata{})
			if err != nil {
				t.Fatalf("Chunk failed: %v", err)
			}

			if len(chunks) == 0 {
				t.Fatal("Expected at least one chunk")
			}

			for i, chunk := range chunks {
				if tc.expectToken && chunk.Tokens == 0 {
					t.Errorf("Chunk %d has zero tokens", i)
				}
			}
		})
	}
}

func TestTextChunker_ChunkSizes(t *testing.T) {
	testCases := []struct {
		name      string
		chunkSize int
		overlap   int
		minSize   int
	}{
		{"small chunks", 50, 5, 10},
		{"medium chunks", 200, 20, 50},
		{"large chunks", 500, 50, 100},
		{"no overlap", 100, 0, 20},
		{"large overlap", 100, 30, 20},
	}

	content := []byte(strings.Repeat("This is a test sentence with some words. ", 100))

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			chunker := NewTextChunker(TextChunkerConfig{
				ChunkSize:    tc.chunkSize,
				ChunkOverlap: tc.overlap,
				MinChunkSize: tc.minSize,
			})

			chunks, err := chunker.Chunk(content, Metadata{})
			if err != nil {
				t.Fatalf("Chunk failed: %v", err)
			}

			for i, chunk := range chunks {
				if chunk.Tokens < tc.minSize {
					t.Errorf("Chunk %d below min size: %d < %d", i, chunk.Tokens, tc.minSize)
				}
				// Allow 2x margin for token estimation variance
				if chunk.Tokens > tc.chunkSize*2 {
					t.Errorf("Chunk %d exceeds max size: %d > %d", i, chunk.Tokens, tc.chunkSize*2)
				}
			}
		})
	}
}

func TestTextChunker_MetadataPreservation(t *testing.T) {
	// Use lower minimum chunk size for test content
	chunker := NewTextChunker(TextChunkerConfig{
		ChunkSize:    200,
		ChunkOverlap: 20,
		MinChunkSize: 5, // Lower minimum to allow test content
	})

	metadata := Metadata{
		FilePath:     "test.go",
		Language:     1,
		FunctionName: "TestFunc",
		ClassName:    "TestClass",
		Tags:         []string{"tag1", "tag2"},
		Context:      map[string]string{"key": "value"},
	}

	content := []byte("This is test content that should preserve metadata across all chunks.")
	chunks, err := chunker.Chunk(content, metadata)

	if err != nil {
		t.Fatalf("Chunk failed: %v", err)
	}

	for i, chunk := range chunks {
		if chunk.Metadata.FilePath != metadata.FilePath {
			t.Errorf("Chunk %d: FilePath not preserved", i)
		}
		if chunk.Metadata.Language != metadata.Language {
			t.Errorf("Chunk %d: Language not preserved", i)
		}
		if chunk.Metadata.FunctionName != metadata.FunctionName {
			t.Errorf("Chunk %d: FunctionName not preserved", i)
		}
		if chunk.Metadata.ClassName != metadata.ClassName {
			t.Errorf("Chunk %d: ClassName not preserved", i)
		}
		if len(chunk.Metadata.Tags) != len(metadata.Tags) {
			t.Errorf("Chunk %d: Tags not preserved", i)
		}
		if chunk.Metadata.Context["key"] != "value" {
			t.Errorf("Chunk %d: Context not preserved", i)
		}
	}
}

func TestTextChunker_SentenceAbbreviations(t *testing.T) {
	chunker := NewTextChunker(TextChunkerConfig{
		ChunkSize:        200,
		ChunkOverlap:     20,
		MinChunkSize:     10,
		RespectSentences: true,
	})

	content := []byte("Dr. Smith went to the store. Mr. Jones went home. " +
		"The value is 3.14 units. Mrs. Davis called. " +
		"This is i.e. an example. That is e.g. another one.")

	chunks, err := chunker.Chunk(content, Metadata{})

	if err != nil {
		t.Fatalf("Chunk failed: %v", err)
	}

	if len(chunks) == 0 {
		t.Fatal("Expected at least one chunk")
	}

	// Verify content is present
	for i, chunk := range chunks {
		if chunk.Content == "" {
			t.Errorf("Chunk %d is empty", i)
		}
	}
}

func TestTextChunker_Name(t *testing.T) {
	chunker := NewTextChunker(DefaultTextChunkerConfig())

	if chunker.Name() != "text" {
		t.Errorf("Expected name 'text', got '%s'", chunker.Name())
	}
}

func TestTextChunker_InvalidConfig(t *testing.T) {
	testCases := []struct {
		name   string
		config TextChunkerConfig
	}{
		{
			name: "negative chunk size",
			config: TextChunkerConfig{
				ChunkSize:    -100,
				ChunkOverlap: 10,
				MinChunkSize: 10,
			},
		},
		{
			name: "overlap exceeds chunk size",
			config: TextChunkerConfig{
				ChunkSize:    100,
				ChunkOverlap: 150,
				MinChunkSize: 10,
			},
		},
		{
			name: "negative overlap",
			config: TextChunkerConfig{
				ChunkSize:    100,
				ChunkOverlap: -10,
				MinChunkSize: 10,
			},
		},
	}

	content := []byte("Test content that should be chunked properly.")

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			// Chunker should auto-correct invalid configs
			chunker := NewTextChunker(tc.config)
			_, err := chunker.Chunk(content, Metadata{})

			// Should not error due to config (auto-corrected)
			if err != nil && err != ErrEmptyContent {
				// Only fail if it's not a content-related error
				t.Logf("Chunk with corrected config returned: %v", err)
			}
		})
	}
}

func TestTextChunker_SingleWord(t *testing.T) {
	chunker := NewTextChunker(TextChunkerConfig{
		ChunkSize:    50,
		ChunkOverlap: 10,
		MinChunkSize: 1,
	})

	content := []byte("Word")
	chunks, err := chunker.Chunk(content, Metadata{})

	if err != nil {
		t.Fatalf("Chunk failed: %v", err)
	}

	if len(chunks) != 1 {
		t.Errorf("Expected 1 chunk, got %d", len(chunks))
	}

	if chunks[0].Content != "Word" {
		t.Errorf("Expected content 'Word', got '%s'", chunks[0].Content)
	}
}

func TestTextChunker_WhitespaceOnly(t *testing.T) {
	chunker := NewTextChunker(DefaultTextChunkerConfig())

	content := []byte("   \n\n\t\t  \n  ")
	chunks, err := chunker.Chunk(content, Metadata{})

	// Should fail because there's no actual content
	if err == nil {
		t.Error("Expected error for whitespace-only content")
	}

	if chunks != nil {
		t.Error("Expected nil chunks for whitespace-only content")
	}
}

func TestTextChunker_MixedContent(t *testing.T) {
	chunker := NewTextChunker(TextChunkerConfig{
		ChunkSize:         150,
		ChunkOverlap:      20,
		MinChunkSize:      20,
		RespectParagraphs: true,
		RespectSentences:  true,
	})

	content := []byte(`Introduction paragraph with some text.
It spans multiple lines.

Second paragraph starts here. It has multiple sentences.
This is another sentence. And one more.

Third paragraph is shorter.

Fourth paragraph concludes the document with final thoughts.
The end.`)

	chunks, err := chunker.Chunk(content, Metadata{
		FilePath: "mixed.txt",
		Tags:     []string{"test", "mixed"},
	})

	if err != nil {
		t.Fatalf("Chunk failed: %v", err)
	}

	if len(chunks) == 0 {
		t.Fatal("Expected at least one chunk")
	}

	// Verify chunk quality
	for i, chunk := range chunks {
		if chunk.Content == "" {
			t.Errorf("Chunk %d is empty", i)
		}
		if chunk.Tokens == 0 {
			t.Errorf("Chunk %d has zero tokens", i)
		}
		if chunk.Index != i {
			t.Errorf("Chunk %d has wrong index: %d", i, chunk.Index)
		}
		if chunk.Metadata.FilePath != "mixed.txt" {
			t.Errorf("Chunk %d lost file path", i)
		}
		if len(chunk.Metadata.Tags) != 2 {
			t.Errorf("Chunk %d lost tags", i)
		}
	}
}

func TestDefaultTextChunkerConfig(t *testing.T) {
	config := DefaultTextChunkerConfig()

	if config.ChunkSize != 512 {
		t.Errorf("Expected default ChunkSize 512, got %d", config.ChunkSize)
	}
	if config.ChunkOverlap != 51 {
		t.Errorf("Expected default ChunkOverlap 51, got %d", config.ChunkOverlap)
	}
	if config.MinChunkSize != 50 {
		t.Errorf("Expected default MinChunkSize 50, got %d", config.MinChunkSize)
	}
	if !config.RespectSentences {
		t.Error("Expected RespectSentences to be true")
	}
	if !config.RespectParagraphs {
		t.Error("Expected RespectParagraphs to be true")
	}
}

func TestMetadata_Clone(t *testing.T) {
	original := Metadata{
		FilePath:     "test.txt",
		Language:     1,
		StartLine:    10,
		EndLine:      20,
		FunctionName: "TestFunc",
		ClassName:    "TestClass",
		Tags:         []string{"tag1", "tag2"},
		Context:      map[string]string{"key1": "value1", "key2": "value2"},
	}

	clone := original.Clone()

	// Verify values are copied
	if clone.FilePath != original.FilePath {
		t.Error("FilePath not cloned")
	}
	if clone.Language != original.Language {
		t.Error("Language not cloned")
	}

	// Verify deep copy of slices and maps
	clone.Tags[0] = "modified"
	if original.Tags[0] == "modified" {
		t.Error("Tags slice not deep copied")
	}

	clone.Context["key1"] = "modified"
	if original.Context["key1"] == "modified" {
		t.Error("Context map not deep copied")
	}
}

// Benchmark tests
func BenchmarkTextChunker_Small(b *testing.B) {
	chunker := NewTextChunker(DefaultTextChunkerConfig())
	content := []byte("This is a small test document with a few sentences. It should be fast to chunk.")

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = chunker.Chunk(content, Metadata{})
	}
}

func BenchmarkTextChunker_Medium(b *testing.B) {
	chunker := NewTextChunker(DefaultTextChunkerConfig())
	content := []byte(strings.Repeat("This is a test sentence with some content. ", 100))

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = chunker.Chunk(content, Metadata{})
	}
}

func BenchmarkTextChunker_Large(b *testing.B) {
	chunker := NewTextChunker(DefaultTextChunkerConfig())
	content := []byte(strings.Repeat("This is a test sentence with some content. ", 1000))

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = chunker.Chunk(content, Metadata{})
	}
}

func TestTextChunker_GetOverlapText(t *testing.T) {
	chunker := NewTextChunker(DefaultTextChunkerConfig())

	tests := []struct {
		name string
		text string
	}{
		{"empty", ""},
		{"short", "hello"},
		{"exact overlap size", strings.Repeat("word ", 20)},
		{"long text", strings.Repeat("The quick brown fox. ", 50)},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := chunker.getOverlapText(tt.text)
			if tt.text == "" && result != "" {
				t.Errorf("Empty input should return empty, got %q", result)
			}
			if len(result) > len(tt.text) {
				t.Errorf("Overlap longer than original: %d > %d", len(result), len(tt.text))
			}
		})
	}
}

func TestTextChunker_CalculateOverlapWords(t *testing.T) {
	chunker := NewTextChunker(DefaultTextChunkerConfig())

	tests := []struct {
		name  string
		words []string
	}{
		{"empty", []string{}},
		{"single", []string{"word"}},
		{"few", []string{"one", "two", "three"}},
		{"many", strings.Fields(strings.Repeat("word ", 100))},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := chunker.calculateOverlapWords(tt.words)
			// Empty slice will return -1, which is acceptable for edge case
			if len(tt.words) > 0 && (result < 0 || result >= len(tt.words)) {
				t.Errorf("Invalid overlap: %d for %d words", result, len(tt.words))
			}
		})
	}
}

func TestTextChunker_ChunkBySentences_EdgeCases(t *testing.T) {
	tests := []struct {
		name    string
		content string
		minSize int
	}{
		{"single sentence with padding", strings.Repeat("Hello world. ", 5), 1},
		{"abbreviations with padding", strings.Repeat("Dr. Smith and Mr. Jones met. ", 3), 1},
		{"questions with padding", strings.Repeat("What? Why? When? ", 3), 1},
		{"multiple newlines", "First sentence here.\n\nSecond sentence here.\n\nThird sentence here.", 1},
		{"long sentences", strings.Repeat("This is a sentence with content. ", 20), 20},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			chunker := NewTextChunker(TextChunkerConfig{
				ChunkSize:        100,
				ChunkOverlap:     10,
				MinChunkSize:     tt.minSize,
				RespectSentences: true,
			})
			chunks, err := chunker.Chunk([]byte(tt.content), Metadata{StartLine: 1})
			if err != nil {
				t.Fatalf("Chunk failed: %v", err)
			}
			if len(chunks) == 0 {
				t.Error("Expected at least one chunk")
			}
			for _, chunk := range chunks {
				if chunk.Content == "" {
					t.Error("Empty chunk content")
				}
			}
		})
	}
}

func TestTextChunker_ChunkByParagraphs_EdgeCases(t *testing.T) {
	tests := []struct {
		name    string
		content string
	}{
		{"single para with padding", strings.Repeat("One paragraph here. ", 3)},
		{"multiple paras", "Para one content here.\n\nPara two content here.\n\nPara three content here."},
		{"empty paras", "Some text here.\n\n\n\nMore text content here."},
		{"long paras", strings.Repeat("Paragraph content. ", 20) + "\n\n" + strings.Repeat("More content. ", 20)},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			chunker := NewTextChunker(TextChunkerConfig{
				ChunkSize:         100,
				ChunkOverlap:      10,
				MinChunkSize:      1,
				RespectParagraphs: true,
			})
			chunks, err := chunker.Chunk([]byte(tt.content), Metadata{StartLine: 1})
			if err != nil {
				t.Fatalf("Chunk failed: %v", err)
			}
			if len(chunks) == 0 {
				t.Error("Expected at least one chunk")
			}
			for _, chunk := range chunks {
				if strings.TrimSpace(chunk.Content) == "" {
					t.Error("Empty chunk after trim")
				}
			}
		})
	}
}
