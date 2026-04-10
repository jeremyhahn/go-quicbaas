//go:build !rag

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

package rag

import (
	"context"
	"errors"
	"time"

	"github.com/jeremyhahn/go-quicbaas/pkg/config"
)

// ErrRAGNotCompiled is returned when RAG functions are called without the rag build tag.
var ErrRAGNotCompiled = errors.New("rag support not compiled - build with -tags rag")

// System is a stub type when RAG support is not compiled.
type System struct{}

// NewSystem returns an error indicating RAG support is not compiled.
func NewSystem(cfg config.RAGConfig) (*System, error) {
	return nil, ErrRAGNotCompiled
}

// Query returns an error indicating RAG support is not compiled.
func (s *System) Query(ctx context.Context, query string, opts QueryOptions) (*QueryResponse, error) {
	return nil, ErrRAGNotCompiled
}

// IndexContent returns an error indicating RAG support is not compiled.
func (s *System) IndexContent(ctx context.Context, id string, content string, metadata map[string]interface{}) error {
	return ErrRAGNotCompiled
}

// Stats returns an empty Stats struct when RAG support is not compiled.
func (s *System) Stats() Stats {
	return Stats{}
}

// Close is a no-op when RAG support is not compiled.
func (s *System) Close() error {
	return nil
}

// QueryOptions configures pipeline behavior (stub version).
type QueryOptions struct {
	TopK          int
	MinScore      float32
	Namespace     string
	Filters       map[string]interface{}
	EnableHybrid  bool
	EnableRerank  bool
	RerankTopK    int
	VectorWeight  float32
	KeywordWeight float32
	MaxTokens     int
	IncludeSource bool
	Template      string
	Format        string
	UseCache      bool
	Timeout       time.Duration
	Parallel      bool
}

// QueryResponse represents a complete RAG query response (stub version).
type QueryResponse struct {
	Query             string
	Results           []Result
	Context           string
	ContextTokens     int
	Truncated         bool
	Intent            string
	ProcessingTime    time.Duration
	VectorSearchTime  time.Duration
	KeywordSearchTime time.Duration
	RerankTime        time.Duration
	ContextBuildTime  time.Duration
	CacheHit          bool
	AvgRelevance      float32
	ResultsFound      int
}

// Result represents a single search result (stub version).
type Result struct {
	ID        string
	Content   string
	Metadata  map[string]interface{}
	FilePath  string
	StartLine int
	EndLine   int
	Score     float32
}

// Stats represents pipeline statistics (stub version).
type Stats struct {
	TotalQueries       int64
	CachedQueries      int64
	FailedQueries      int64
	AvgLatencyMs       float64
	P50LatencyMs       float64
	P95LatencyMs       float64
	P99LatencyMs       float64
	CacheHitRate       float64
	CacheSize          int
	CacheEvictions     int64
	ZeroResultQueries  int64
	AvgResultsReturned float64
	AvgRelevanceScore  float64
	VectorSearchAvgMs  float64
	KeywordSearchAvgMs float64
	RerankAvgMs        float64
	ContextBuildAvgMs  float64
}
