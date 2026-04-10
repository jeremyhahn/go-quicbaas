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

// Result represents a single search result with metadata and scoring.
type Result struct {
	// Vector data
	ID        string    // Vector ID
	Vector    []float32 // Embedding vector
	Namespace string    // Namespace

	// Content
	Content string // Original content

	// Metadata from chunk
	Metadata Metadata // Full metadata

	// Scoring (from different ranking stages)
	VectorScore  float32 // Vector similarity score (0-1)
	KeywordScore float32 // BM25 keyword score (0-1)
	RerankScore  float32 // Reranker score (0-1)
	FusionScore  float32 // Final fused score (0-1)
}

// Metadata holds all metadata for a search result.
type Metadata struct {
	// Source information
	FilePath string
	FileName string
	Language string

	// Position information
	StartLine int
	EndLine   int
	StartByte int
	EndByte   int

	// Code context (for code files)
	PackageName  string
	FunctionName string
	ClassName    string
	MethodName   string

	// Semantic information
	Keywords []string
	Tags     []string
	Category string
}

// Template defines output format.
type Template string

const (
	TemplateMarkdown Template = "markdown"
	TemplateJSON     Template = "json"
	TemplateXML      Template = "xml"
	TemplatePlain    Template = "plain"
)

// BuiltContext represents formatted context with metadata.
type BuiltContext struct {
	Content   string     // Formatted context
	Tokens    int        // Token count
	Results   []Result   // Included results
	Truncated bool       // Whether truncation occurred
	Citations []Citation // Source citations
}

// Citation represents a source citation.
type Citation struct {
	Index     int     // Citation number
	FilePath  string  // Source file
	StartLine int     // Start line
	EndLine   int     // End line
	Score     float32 // Relevance score
}
