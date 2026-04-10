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

package pipeline

import (
	"context"
	"time"
)

// Pipeline orchestrates the complete RAG query flow.
type Pipeline interface {
	// Query executes a complete RAG query
	Query(ctx context.Context, q string, opts QueryOptions) (*QueryResponse, error)

	// Stats returns pipeline statistics
	Stats() Stats

	// Health returns pipeline health status
	Health() HealthStatus

	// Close releases all resources
	Close() error
}

// QueryOptions configures pipeline behavior.
type QueryOptions struct {
	// Retrieval configuration
	TopK      int                    // Final number of results (default: 10)
	MinScore  float32                // Minimum similarity threshold (default: 0.7)
	Namespace string                 // Vector namespace (default: "default")
	Filters   map[string]interface{} // Metadata filters

	// Ranking configuration
	EnableHybrid  bool    // Enable hybrid search (default: true)
	EnableRerank  bool    // Enable cross-encoder reranking (default: true)
	RerankTopK    int     // Candidates for reranking (default: 50)
	VectorWeight  float32 // Vector search weight (default: 0.7)
	KeywordWeight float32 // Keyword search weight (default: 0.3)

	// Context building configuration
	MaxTokens     int    // Maximum context tokens (default: 8000)
	IncludeSource bool   // Include source citations (default: true)
	Template      string // Context template name (default: "default")
	Format        string // Output format: "markdown", "json", "xml"

	// Performance configuration
	UseCache bool          // Enable query caching (default: true)
	Timeout  time.Duration // Query timeout (default: 30s)
	Parallel bool          // Enable parallel search (default: true)
}

// QueryResponse represents a complete RAG query response.
type QueryResponse struct {
	// Results
	Query         string   // Original query
	Results       []Result // Ranked search results
	Context       string   // LLM-ready context
	ContextTokens int      // Token count in context
	Truncated     bool     // Whether results were truncated

	// Query analysis
	Intent string // Detected query intent

	// Performance metrics
	ProcessingTime    time.Duration // Total processing time
	VectorSearchTime  time.Duration // Vector search latency
	KeywordSearchTime time.Duration // Keyword search latency
	RerankTime        time.Duration // Reranking latency
	ContextBuildTime  time.Duration // Context building latency
	CacheHit          bool          // Whether result came from cache

	// Quality metrics
	AvgRelevance float32 // Average relevance score
	ResultsFound int     // Total results before ranking
}

// Result represents a single search result.
type Result struct {
	ID        string
	Content   string
	Metadata  map[string]interface{}
	FilePath  string
	StartLine int
	EndLine   int
	Score     float32
}

// HealthStatus represents pipeline health.
type HealthStatus struct {
	Healthy         bool
	Status          string          // "healthy", "degraded", "unhealthy"
	ComponentHealth map[string]bool // Component-level health
	Errors          []string        // Recent errors
	LastCheck       time.Time
}

// QueryHandler processes queries.
type QueryHandler interface {
	Process(ctx context.Context, query string) (*ProcessedQuery, error)
	Close() error
}

// ProcessedQuery represents a processed query.
type ProcessedQuery struct {
	Original   string
	Cleaned    string
	Embedding  []float32
	Intent     string
	CacheHit   bool
	CachedResp *QueryResponse
}

// VectorSearcher performs vector search.
type VectorSearcher interface {
	Search(ctx context.Context, embedding []float32, opts SearchOptions) ([]Result, error)
	Close() error
}

// SearchOptions configures vector search.
type SearchOptions struct {
	TopK      int
	MinScore  float32
	Namespace string
	Filters   map[string]interface{}
}

// Ranker ranks and fuses results.
type Ranker interface {
	Rank(ctx context.Context, query string, vectorResults, keywordResults []Result) ([]Result, error)
	Close() error
}

// ContextBuilder builds context from results.
type ContextBuilder interface {
	Build(ctx context.Context, results []Result, opts ContextOptions) (*BuiltContext, error)
	Close() error
}

// ContextOptions configures context building.
type ContextOptions struct {
	MaxTokens     int
	IncludeSource bool
	Template      string
	Format        string
}

// BuiltContext represents formatted context.
type BuiltContext struct {
	Content   string
	Tokens    int
	Truncated bool
}
