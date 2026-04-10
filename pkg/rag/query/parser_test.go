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

package query

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseQuery_Success(t *testing.T) {
	tests := []struct {
		name     string
		query    string
		wantCode bool
		wantLang string
	}{
		{
			name:     "simple text query",
			query:    "what is authentication",
			wantCode: false,
			wantLang: "",
		},
		{
			name:     "code function query",
			query:    "show me the authentication function",
			wantCode: true,
			wantLang: "",
		},
		{
			name:     "go code query",
			query:    "how to implement golang server",
			wantCode: false,
			wantLang: "",
		},
		{
			name:     "python code query",
			query:    "python implementation of authentication",
			wantCode: true,
			wantLang: "python",
		},
		{
			name:     "camelCase detection",
			query:    "how does handleRequest work",
			wantCode: false, // handleRequest is treated as one word in the query
			wantLang: "",
		},
		{
			name:     "snake_case detection",
			query:    "what is handle_request function",
			wantCode: true,
			wantLang: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			parsed, err := ParseQuery(tt.query)
			require.NoError(t, err)
			assert.Equal(t, tt.query, parsed.Original)
			assert.Equal(t, tt.wantCode, parsed.IsCode)
			assert.Equal(t, tt.wantLang, parsed.Language)
			assert.NotEmpty(t, parsed.Keywords)
		})
	}
}

func TestParseQuery_EmptyQuery(t *testing.T) {
	_, err := ParseQuery("")
	assert.Equal(t, ErrEmptyQuery, err)
}

func TestExtractKeywords(t *testing.T) {
	tests := []struct {
		name     string
		query    string
		wantSize int
		contains []string
		excludes []string
	}{
		{
			name:     "simple query",
			query:    "how to start the server",
			wantSize: 2,
			contains: []string{"start", "server"},
			excludes: []string{"how", "to", "the"},
		},
		{
			name:     "code query",
			query:    "function for authentication token",
			wantSize: 3,
			contains: []string{"function", "authentication", "token"},
			excludes: []string{"for"},
		},
		{
			name:     "with punctuation",
			query:    "what is the server.Start() method?",
			wantSize: 2,
			contains: []string{"server", "start"},
			excludes: []string{"what", "is", "the"},
		},
		{
			name:     "mixed case",
			query:    "How To Configure HNSW Index",
			wantSize: 3,
			contains: []string{"configure", "hnsw", "index"},
			excludes: []string{"how", "to"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			keywords := extractKeywords(tt.query)

			assert.GreaterOrEqual(t, len(keywords), tt.wantSize)

			for _, word := range tt.contains {
				assert.Contains(t, keywords, word, "should contain %s", word)
			}

			for _, word := range tt.excludes {
				assert.NotContains(t, keywords, word, "should not contain %s", word)
			}
		})
	}
}

func TestSplitWords(t *testing.T) {
	tests := []struct {
		name string
		text string
		want []string
	}{
		{
			name: "simple text",
			text: "hello world",
			want: []string{"hello", "world"},
		},
		{
			name: "with punctuation",
			text: "hello, world!",
			want: []string{"hello", "world"},
		},
		{
			name: "with numbers",
			text: "test123 value456",
			want: []string{"test123", "value456"},
		},
		{
			name: "with underscore",
			text: "handle_request function",
			want: []string{"handle_request", "function"},
		},
		{
			name: "with hyphen",
			text: "multi-threaded server",
			want: []string{"multi-threaded", "server"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := splitWords(tt.text)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestDetectCode(t *testing.T) {
	tests := []struct {
		name  string
		query string
		want  bool
	}{
		{
			name:  "code function",
			query: "show me the authentication function",
			want:  true,
		},
		{
			name:  "code method",
			query: "what is the method for handling requests",
			want:  true,
		},
		{
			name:  "code class",
			query: "how to implement a class",
			want:  true,
		},
		{
			name:  "code implementation",
			query: "show implementation of server",
			want:  true,
		},
		{
			name:  "code example",
			query: "give me an example of authentication",
			want:  true,
		},
		{
			name:  "snake_case in query",
			query: "what is handle_request",
			want:  true,
		},
		{
			name:  "text query",
			query: "what is authentication",
			want:  false,
		},
		{
			name:  "documentation",
			query: "how to configure the system",
			want:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := detectCode(tt.query)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestDetectLanguage(t *testing.T) {
	tests := []struct {
		name  string
		query string
		want  string
	}{
		{
			name:  "go",
			query: "how to implement in golang",
			want:  "go",
		},
		{
			name:  "go short",
			query: "show me go implementation",
			want:  "go",
		},
		{
			name:  "python",
			query: "python code for authentication",
			want:  "python",
		},
		{
			name:  "javascript",
			query: "javascript implementation",
			want:  "javascript",
		},
		{
			name:  "node",
			query: "node server example",
			want:  "javascript",
		},
		{
			name:  "java",
			query: "java class implementation",
			want:  "java",
		},
		{
			name:  "rust",
			query: "rust implementation",
			want:  "rust",
		},
		{
			name:  "c++",
			query: "c++ code example",
			want:  "c++",
		},
		{
			name:  "no language",
			query: "how to implement authentication",
			want:  "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := detectLanguage(tt.query)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestHasCamelCase(t *testing.T) {
	tests := []struct {
		name string
		word string
		want bool
	}{
		{
			name: "camelCase",
			word: "handleRequest",
			want: true,
		},
		{
			name: "PascalCase",
			word: "HandleRequest",
			want: true,
		},
		{
			name: "lowercase",
			word: "handlerequest",
			want: false,
		},
		{
			name: "UPPERCASE",
			word: "HANDLEREQUEST",
			want: false,
		},
		{
			name: "mixed",
			word: "helloWorld",
			want: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := hasCamelCase(tt.word)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestHasSnakeCase(t *testing.T) {
	tests := []struct {
		name string
		word string
		want bool
	}{
		{
			name: "snake_case",
			word: "handle_request",
			want: true,
		},
		{
			name: "no underscore",
			word: "handlerequest",
			want: false,
		},
		{
			name: "multiple underscores",
			word: "handle_http_request",
			want: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := hasSnakeCase(tt.word)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestIsStopword(t *testing.T) {
	tests := []struct {
		name string
		word string
		want bool
	}{
		{
			name: "stopword the",
			word: "the",
			want: true,
		},
		{
			name: "stopword is",
			word: "is",
			want: true,
		},
		{
			name: "stopword and",
			word: "and",
			want: true,
		},
		{
			name: "not stopword",
			word: "authentication",
			want: false,
		},
		{
			name: "not stopword function",
			word: "function",
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isStopword(tt.word)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestContainsWord(t *testing.T) {
	tests := []struct {
		name  string
		query string
		word  string
		want  bool
	}{
		{
			name:  "contains word",
			query: "show me go code",
			word:  "go",
			want:  true,
		},
		{
			name:  "does not contain word",
			query: "show me golang code",
			word:  "go",
			want:  false,
		},
		{
			name:  "partial match not counted",
			query: "show me python code",
			word:  "py",
			want:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := containsWord(tt.query, tt.word)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestParseQuery_Performance(t *testing.T) {
	query := "how to implement authentication function in golang with proper error handling"

	// Run multiple times to ensure consistent performance
	for i := 0; i < 1000; i++ {
		_, err := ParseQuery(query)
		require.NoError(t, err)
	}
}

func BenchmarkParseQuery(b *testing.B) {
	query := "how to implement authentication function in golang with proper error handling"

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = ParseQuery(query)
	}
}

func BenchmarkExtractKeywords(b *testing.B) {
	query := "how to implement authentication function in golang with proper error handling"

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = extractKeywords(query)
	}
}

func BenchmarkDetectCode(b *testing.B) {
	query := "show me the authentication function implementation"

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = detectCode(query)
	}
}
