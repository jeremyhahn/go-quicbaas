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

import "testing"

func TestSimpleTokenizer_Count(t *testing.T) {
	tokenizer := NewSimpleTokenizer()

	testCases := []struct {
		name     string
		text     string
		minCount int
		maxCount int
	}{
		{
			name:     "empty string",
			text:     "",
			minCount: 0,
			maxCount: 0,
		},
		{
			name:     "single word",
			text:     "hello",
			minCount: 1,
			maxCount: 2,
		},
		{
			name:     "simple sentence",
			text:     "Hello world",
			minCount: 2,
			maxCount: 3,
		},
		{
			name:     "sentence with punctuation",
			text:     "Hello, world!",
			minCount: 2,
			maxCount: 4,
		},
		{
			name:     "multiple sentences",
			text:     "This is a test. It has multiple sentences.",
			minCount: 8,
			maxCount: 12,
		},
		{
			name:     "whitespace variations",
			text:     "This   has    multiple     spaces",
			minCount: 4,
			maxCount: 6,
		},
		{
			name:     "tabs and newlines",
			text:     "This\thas\ttabs\nand\nnewlines",
			minCount: 5,
			maxCount: 7,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			count := tokenizer.Count(tc.text)

			if count < tc.minCount || count > tc.maxCount {
				t.Errorf("Count %d not in expected range [%d, %d]",
					count, tc.minCount, tc.maxCount)
			}
		})
	}
}

func TestSimpleTokenizer_Estimate(t *testing.T) {
	tokenizer := NewSimpleTokenizer()

	text := "This is a test sentence with some words."
	count := tokenizer.Count(text)
	estimate := tokenizer.Estimate(text)

	if count != estimate {
		t.Errorf("Count and Estimate should be equal: %d != %d", count, estimate)
	}
}

func TestSimpleTokenizer_CountWords(t *testing.T) {
	tokenizer := NewSimpleTokenizer()

	testCases := []struct {
		name          string
		text          string
		expectedWords int
	}{
		{
			name:          "empty string",
			text:          "",
			expectedWords: 0,
		},
		{
			name:          "single word",
			text:          "hello",
			expectedWords: 1,
		},
		{
			name:          "multiple words",
			text:          "hello world test",
			expectedWords: 3,
		},
		{
			name:          "words with punctuation",
			text:          "hello, world! test?",
			expectedWords: 3,
		},
		{
			name:          "extra whitespace",
			text:          "  hello   world  ",
			expectedWords: 2,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			words := tokenizer.CountWords(tc.text)

			if words != tc.expectedWords {
				t.Errorf("Expected %d words, got %d", tc.expectedWords, words)
			}
		})
	}
}

func TestSimpleTokenizer_Unicode(t *testing.T) {
	tokenizer := NewSimpleTokenizer()

	testCases := []struct {
		name string
		text string
	}{
		{
			name: "chinese characters",
			text: "你好世界",
		},
		{
			name: "japanese characters",
			text: "こんにちは世界",
		},
		{
			name: "emoji",
			text: "Hello 👋 World 🌍",
		},
		{
			name: "mixed unicode",
			text: "Hello мир world 世界",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			count := tokenizer.Count(tc.text)

			// Should return a reasonable count (not zero, not negative)
			if count <= 0 {
				t.Errorf("Expected positive count for unicode text, got %d", count)
			}
		})
	}
}

func TestSimpleTokenizer_LongText(t *testing.T) {
	tokenizer := NewSimpleTokenizer()

	// Build a long text
	longText := ""
	for i := 0; i < 1000; i++ {
		longText += "word "
	}

	count := tokenizer.Count(longText)

	// Should be approximately 1000 words * 1.15 = 1150 tokens
	expectedMin := 1000
	expectedMax := 1200

	if count < expectedMin || count > expectedMax {
		t.Errorf("Count %d not in expected range [%d, %d]",
			count, expectedMin, expectedMax)
	}
}

func BenchmarkSimpleTokenizer_Count(b *testing.B) {
	tokenizer := NewSimpleTokenizer()
	text := "This is a benchmark test sentence with multiple words and punctuation."

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = tokenizer.Count(text)
	}
}

func BenchmarkSimpleTokenizer_CountLarge(b *testing.B) {
	tokenizer := NewSimpleTokenizer()

	// Build a larger text
	longText := ""
	for i := 0; i < 1000; i++ {
		longText += "This is a test sentence with some content. "
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = tokenizer.Count(longText)
	}
}
