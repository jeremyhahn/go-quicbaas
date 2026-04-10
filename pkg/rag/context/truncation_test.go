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

package context

import (
	"strings"
	"testing"
)

func TestSmartTruncate(t *testing.T) {
	tokenizer := NewSimpleTokenizer()

	tests := []struct {
		name        string
		results     []Result
		maxTokens   int
		expectCount int
		expectTrunc bool
	}{
		{
			name:        "empty results",
			results:     []Result{},
			maxTokens:   1000,
			expectCount: 0,
			expectTrunc: false,
		},
		{
			name: "single result fits",
			results: []Result{
				{Content: "short content"},
			},
			maxTokens:   1000,
			expectCount: 1,
			expectTrunc: false,
		},
		{
			name: "multiple results fit",
			results: []Result{
				{Content: "content one"},
				{Content: "content two"},
				{Content: "content three"},
			},
			maxTokens:   1000,
			expectCount: 3,
			expectTrunc: false,
		},
		{
			name: "truncate to fit",
			results: []Result{
				{Content: strings.Repeat("word ", 100)},
				{Content: strings.Repeat("word ", 100)},
				{Content: strings.Repeat("word ", 100)},
			},
			maxTokens:   300,
			expectCount: 1,
			expectTrunc: false, // SmartTruncate doesn't set this flag
		},
		{
			name: "very low token limit",
			results: []Result{
				{Content: "content"},
			},
			maxTokens:   50,
			expectCount: 1,
			expectTrunc: false,
		},
		{
			name: "zero token limit",
			results: []Result{
				{Content: "content"},
			},
			maxTokens:   0,
			expectCount: 1, // At least 1 result
			expectTrunc: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			truncated := SmartTruncate(tt.results, tt.maxTokens, tokenizer)

			if len(truncated) != tt.expectCount {
				t.Errorf("SmartTruncate() returned %d results, want %d",
					len(truncated), tt.expectCount)
			}

			// Verify all results have content
			for i, result := range truncated {
				if result.Content == "" {
					t.Errorf("Result %d has empty content", i)
				}
			}
		})
	}
}

func TestTruncateContent(t *testing.T) {
	tokenizer := NewSimpleTokenizer()

	tests := []struct {
		name      string
		content   string
		maxTokens int
		expectLen int // approximate
	}{
		{
			name:      "empty content",
			content:   "",
			maxTokens: 100,
			expectLen: 0,
		},
		{
			name:      "short content",
			content:   "hello world",
			maxTokens: 100,
			expectLen: 11,
		},
		{
			name:      "truncate needed",
			content:   strings.Repeat("word ", 100),
			maxTokens: 10,
			expectLen: 50, // Approximate, should be truncated
		},
		{
			name:      "zero tokens",
			content:   "hello world",
			maxTokens: 0,
			expectLen: 0,
		},
		{
			name:      "negative tokens",
			content:   "hello world",
			maxTokens: -1,
			expectLen: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			truncated := TruncateContent(tt.content, tt.maxTokens, tokenizer)

			if tt.expectLen == 0 {
				if truncated != "" && truncated != "..." {
					t.Errorf("TruncateContent() = %q, want empty or '...'", truncated)
				}
				return
			}

			// Check if content was truncated (has ... at end)
			wasTruncated := strings.HasSuffix(truncated, "...")

			// If truncated, verify it's shorter than original
			if wasTruncated && len(truncated) >= len(tt.content) {
				t.Errorf("Truncated content is not shorter than original")
			}

			// Verify token count is within limit
			tokens := tokenizer.Count(truncated)
			if tokens > tt.maxTokens+5 { // Allow small variance
				t.Errorf("TruncateContent() resulted in %d tokens, max is %d",
					tokens, tt.maxTokens)
			}
		})
	}
}

func TestTruncateHard(t *testing.T) {
	tests := []struct {
		name     string
		content  string
		maxChars int
		expected string
	}{
		{
			name:     "no truncation needed",
			content:  "hello",
			maxChars: 10,
			expected: "hello",
		},
		{
			name:     "exact fit",
			content:  "hello",
			maxChars: 5,
			expected: "hello",
		},
		{
			name:     "truncate needed",
			content:  "hello world",
			maxChars: 8,
			expected: "hello...",
		},
		{
			name:     "very short limit",
			content:  "hello",
			maxChars: 2,
			expected: "he",
		},
		{
			name:     "zero limit",
			content:  "hello",
			maxChars: 0,
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := TruncateHard(tt.content, tt.maxChars)
			if result != tt.expected {
				t.Errorf("TruncateHard() = %q, want %q", result, tt.expected)
			}
		})
	}
}

func TestFitResults(t *testing.T) {
	tokenizer := NewSimpleTokenizer()

	tests := []struct {
		name      string
		results   []Result
		maxTokens int
		expectMin int
		expectMax int
	}{
		{
			name:      "empty results",
			results:   []Result{},
			maxTokens: 1000,
			expectMin: 0,
			expectMax: 0,
		},
		{
			name: "all fit",
			results: []Result{
				{Content: "one"},
				{Content: "two"},
				{Content: "three"},
			},
			maxTokens: 1000,
			expectMin: 3,
			expectMax: 3,
		},
		{
			name: "partial fit",
			results: []Result{
				{Content: strings.Repeat("word ", 50)},
				{Content: strings.Repeat("word ", 50)},
				{Content: strings.Repeat("word ", 50)},
			},
			maxTokens: 200,
			expectMin: 1,
			expectMax: 2,
		},
		{
			name: "very low limit",
			results: []Result{
				{Content: "content"},
			},
			maxTokens: 50,
			expectMin: 1,
			expectMax: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			indices := FitResults(tt.results, tt.maxTokens, tokenizer)

			if len(indices) < tt.expectMin || len(indices) > tt.expectMax {
				t.Errorf("FitResults() returned %d indices, want between %d and %d",
					len(indices), tt.expectMin, tt.expectMax)
			}

			// Verify indices are valid
			for _, idx := range indices {
				if idx < 0 || idx >= len(tt.results) {
					t.Errorf("Invalid index %d for results length %d", idx, len(tt.results))
				}
			}
		})
	}
}

func TestSmartTruncate_PreservesMetadata(t *testing.T) {
	tokenizer := NewSimpleTokenizer()

	results := []Result{
		{
			Content: "test content",
			Metadata: Metadata{
				FilePath:  "/path/to/file.go",
				StartLine: 10,
				EndLine:   20,
			},
		},
	}

	truncated := SmartTruncate(results, 1000, tokenizer)

	if len(truncated) != 1 {
		t.Fatalf("Expected 1 result, got %d", len(truncated))
	}

	if truncated[0].Metadata.FilePath != "/path/to/file.go" {
		t.Errorf("Metadata was not preserved")
	}
}

func TestTruncateContent_WordBoundary(t *testing.T) {
	tokenizer := NewSimpleTokenizer()

	content := "The quick brown fox jumps over the lazy dog"
	truncated := TruncateContent(content, 5, tokenizer)

	// Should truncate at word boundary - verify it's a complete word or ends with ...
	if !strings.HasSuffix(truncated, "...") {
		t.Logf("Truncated content: %s", truncated)
		// If no ..., content should still be complete words
		words := strings.Fields(truncated)
		rejoined := strings.Join(words, " ")
		if rejoined != truncated {
			t.Errorf("TruncateContent() did not truncate at word boundary: %s", truncated)
		}
	}
}

func TestSmartTruncate_LargeContent(t *testing.T) {
	tokenizer := NewSimpleTokenizer()

	// Create a large result
	largeContent := strings.Repeat("This is a test sentence. ", 1000)

	results := []Result{
		{Content: largeContent},
	}

	truncated := SmartTruncate(results, 500, tokenizer)

	if len(truncated) != 1 {
		t.Fatalf("Expected 1 result, got %d", len(truncated))
	}

	// Content should be truncated
	if len(truncated[0].Content) >= len(largeContent) {
		t.Errorf("Content was not truncated")
	}

	// Verify token count is within limit
	tokens := tokenizer.Count(truncated[0].Content)
	if tokens > 600 { // Allow some overhead
		t.Errorf("Truncated content has %d tokens, exceeds limit", tokens)
	}
}

// Benchmark tests
func BenchmarkSmartTruncate(b *testing.B) {
	tokenizer := NewSimpleTokenizer()

	results := make([]Result, 10)
	for i := range results {
		results[i] = Result{
			Content: strings.Repeat("word ", 100),
		}
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = SmartTruncate(results, 1000, tokenizer)
	}
}

func BenchmarkTruncateContent(b *testing.B) {
	tokenizer := NewSimpleTokenizer()
	content := strings.Repeat("word ", 1000)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = TruncateContent(content, 100, tokenizer)
	}
}

func BenchmarkFitResults(b *testing.B) {
	tokenizer := NewSimpleTokenizer()

	results := make([]Result, 20)
	for i := range results {
		results[i] = Result{
			Content: strings.Repeat("test content ", 50),
		}
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = FitResults(results, 1000, tokenizer)
	}
}
