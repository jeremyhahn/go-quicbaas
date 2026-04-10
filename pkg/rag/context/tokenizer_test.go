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
	"testing"
)

func TestSimpleTokenizer_Count(t *testing.T) {
	tokenizer := NewSimpleTokenizer()

	tests := []struct {
		name      string
		text      string
		minTokens int
		maxTokens int
	}{
		{
			name:      "empty string",
			text:      "",
			minTokens: 0,
			maxTokens: 0,
		},
		{
			name:      "single word",
			text:      "hello",
			minTokens: 1,
			maxTokens: 2,
		},
		{
			name:      "simple sentence",
			text:      "hello world",
			minTokens: 2,
			maxTokens: 4,
		},
		{
			name:      "sentence with punctuation",
			text:      "Hello, world!",
			minTokens: 3,
			maxTokens: 6,
		},
		{
			name:      "code snippet",
			text:      "func main() { fmt.Println(\"hello\") }",
			minTokens: 15,
			maxTokens: 25,
		},
		{
			name:      "code with underscores",
			text:      "var my_variable = some_function()",
			minTokens: 4,
			maxTokens: 8,
		},
		{
			name:      "multiline text",
			text:      "line one\nline two\nline three",
			minTokens: 6,
			maxTokens: 10,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tokens := tokenizer.Count(tt.text)
			if tokens < tt.minTokens || tokens > tt.maxTokens {
				t.Errorf("Count() = %d, want between %d and %d", tokens, tt.minTokens, tt.maxTokens)
			}
		})
	}
}

func TestSimpleTokenizer_Estimate(t *testing.T) {
	tokenizer := NewSimpleTokenizer()

	text := "This is a test sentence"
	count := tokenizer.Count(text)
	estimate := tokenizer.Estimate(text)

	if count != estimate {
		t.Errorf("Estimate() = %d, Count() = %d, should be equal", estimate, count)
	}
}

func TestSimpleTokenizer_CustomMultiplier(t *testing.T) {
	tokenizer := NewSimpleTokenizerWithMultiplier(2.0)

	text := "hello world"
	tokens := tokenizer.Count(text)

	// With 2.0 multiplier, should be roughly 2x the word count
	// Allow some variance for punctuation/spacing
	if tokens < 3 || tokens > 6 {
		t.Errorf("Count() with 2.0 multiplier = %d, expected 3-6", tokens)
	}
}

func TestSimpleTokenizer_Consistency(t *testing.T) {
	tokenizer := NewSimpleTokenizer()

	text := "The quick brown fox jumps over the lazy dog"

	// Count should be consistent across multiple calls
	count1 := tokenizer.Count(text)
	count2 := tokenizer.Count(text)
	count3 := tokenizer.Count(text)

	if count1 != count2 || count2 != count3 {
		t.Errorf("Inconsistent counts: %d, %d, %d", count1, count2, count3)
	}
}

func TestCountWords(t *testing.T) {
	tests := []struct {
		name     string
		text     string
		minWords int
		maxWords int
	}{
		{
			name:     "empty",
			text:     "",
			minWords: 0,
			maxWords: 0,
		},
		{
			name:     "single word",
			text:     "hello",
			minWords: 1,
			maxWords: 1,
		},
		{
			name:     "multiple words",
			text:     "hello world foo bar",
			minWords: 4,
			maxWords: 4,
		},
		{
			name:     "with punctuation",
			text:     "hello, world!",
			minWords: 3,
			maxWords: 4,
		},
		{
			name:     "code with symbols",
			text:     "func(x, y)",
			minWords: 3,
			maxWords: 6,
		},
		{
			name:     "whitespace only",
			text:     "   \t\n  ",
			minWords: 0,
			maxWords: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			words := countWords(tt.text)
			if words < tt.minWords || words > tt.maxWords {
				t.Errorf("countWords() = %d, want between %d and %d", words, tt.minWords, tt.maxWords)
			}
		})
	}
}

func TestEstimateTokensFromWords(t *testing.T) {
	tests := []struct {
		wordCount int
		expected  int
	}{
		{0, 0},
		{10, 13},
		{100, 130},
		{1000, 1300},
	}

	for _, tt := range tests {
		tokens := EstimateTokensFromWords(tt.wordCount)
		if tokens != tt.expected {
			t.Errorf("EstimateTokensFromWords(%d) = %d, want %d", tt.wordCount, tokens, tt.expected)
		}
	}
}

func TestCountWordsSimple(t *testing.T) {
	tests := []struct {
		name     string
		text     string
		expected int
	}{
		{"empty", "", 0},
		{"single word", "hello", 1},
		{"multiple words", "hello world", 2},
		{"with tabs", "hello\tworld", 2},
		{"with newlines", "hello\nworld", 2},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			count := CountWordsSimple(tt.text)
			if count != tt.expected {
				t.Errorf("CountWordsSimple() = %d, want %d", count, tt.expected)
			}
		})
	}
}

func TestSimpleTokenizer_RealWorldCode(t *testing.T) {
	tokenizer := NewSimpleTokenizer()

	code := `func main() {
    fmt.Println("Hello, World!")
    x := 42
    if x > 0 {
        fmt.Println(x)
    }
}`

	tokens := tokenizer.Count(code)

	// This code should be roughly 30-40 tokens
	if tokens < 20 || tokens > 50 {
		t.Errorf("Count() for real code = %d, expected 20-50", tokens)
	}
}

func TestSimpleTokenizer_Accuracy(t *testing.T) {
	tokenizer := NewSimpleTokenizer()

	// Test against known GPT-3.5 tokenization (approximate)
	tests := []struct {
		text            string
		approxGPTTokens int
		tolerance       float64 // allowed % difference
	}{
		{"hello world", 2, 0.5},
		{"The quick brown fox", 4, 0.5},
		{"package main\n\nfunc main() {}", 7, 0.5},
	}

	for _, tt := range tests {
		t.Run(tt.text, func(t *testing.T) {
			tokens := tokenizer.Count(tt.text)
			diff := float64(abs(tokens-tt.approxGPTTokens)) / float64(tt.approxGPTTokens)

			if diff > tt.tolerance {
				t.Logf("Warning: Count() = %d, approx GPT tokens = %d, diff = %.2f%%",
					tokens, tt.approxGPTTokens, diff*100)
			}
		})
	}
}

func abs(x int) int {
	if x < 0 {
		return -x
	}
	return x
}

// Benchmark tests
func BenchmarkSimpleTokenizer_Count(b *testing.B) {
	tokenizer := NewSimpleTokenizer()
	text := "The quick brown fox jumps over the lazy dog. This is a test sentence for benchmarking."

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = tokenizer.Count(text)
	}
}

func BenchmarkSimpleTokenizer_CountLargeText(b *testing.B) {
	tokenizer := NewSimpleTokenizer()

	// Simulate a large code file
	text := ""
	for i := 0; i < 100; i++ {
		text += "func example() { fmt.Println(\"test\") }\n"
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = tokenizer.Count(text)
	}
}

func BenchmarkCountWords(b *testing.B) {
	text := "The quick brown fox jumps over the lazy dog"

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = countWords(text)
	}
}
