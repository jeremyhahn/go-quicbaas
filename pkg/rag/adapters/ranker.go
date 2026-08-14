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

package adapters

import (
	"context"

	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
	"github.com/jeremyhahn/go-quicbaas/pkg/rag/pipeline"
	"github.com/jeremyhahn/go-quicbaas/pkg/rag/ranking"
)

// RankerConfig holds configuration for the ranker adapter.
type RankerConfig struct {
	// EnableHybrid enables hybrid ranking combining vector and keyword results.
	EnableHybrid bool

	// VectorWeight is the weight for vector search results in hybrid mode.
	VectorWeight float32

	// KeywordWeight is the weight for keyword search results in hybrid mode.
	KeywordWeight float32
}

// DefaultRankerConfig returns sensible defaults for the ranker adapter.
func DefaultRankerConfig() *RankerConfig {
	return &RankerConfig{
		EnableHybrid:  true,
		VectorWeight:  0.7,
		KeywordWeight: 0.3,
	}
}

// Validate validates the ranker configuration.
func (c *RankerConfig) Validate() error {
	if c.VectorWeight < 0 || c.VectorWeight > 1 {
		return ErrInvalidConfig
	}
	if c.KeywordWeight < 0 || c.KeywordWeight > 1 {
		return ErrInvalidConfig
	}
	return nil
}

// RankerOption is a functional option for configuring the RankerAdapter.
type RankerOption func(*RankerAdapter)

// WithRankerConfig sets the ranker configuration.
func WithRankerConfig(config *RankerConfig) RankerOption {
	return func(a *RankerAdapter) {
		if config != nil {
			a.config = config
		}
	}
}

// WithRankerLogger sets the logger for the ranker adapter.
func WithRankerLogger(log *logger.Logger) RankerOption {
	return func(a *RankerAdapter) {
		if log != nil {
			a.logger = log
		}
	}
}

// WithHybridEnabled enables or disables hybrid ranking.
func WithHybridEnabled(enabled bool) RankerOption {
	return func(a *RankerAdapter) {
		a.config.EnableHybrid = enabled
	}
}

// RankerAdapter adapts ranking.HybridRanker to the pipeline.Ranker interface.
// It bridges the ranking implementation with the pipeline's expected interface,
// handling type conversions between ranking.Result and pipeline.Result types.
type RankerAdapter struct {
	hybridRanker *ranking.HybridRanker
	config       *RankerConfig
	logger       *logger.Logger
}

// NewRankerAdapter creates a new ranker adapter wrapping a HybridRanker.
func NewRankerAdapter(ranker *ranking.HybridRanker, opts ...RankerOption) (*RankerAdapter, error) {
	if ranker == nil {
		return nil, ErrNilHybridRanker
	}

	adapter := &RankerAdapter{
		hybridRanker: ranker,
		config:       DefaultRankerConfig(),
		logger:       nil,
	}

	// Apply options
	for _, opt := range opts {
		opt(adapter)
	}

	// Validate configuration
	if err := adapter.config.Validate(); err != nil {
		return nil, err
	}

	return adapter, nil
}

// Rank implements pipeline.Ranker interface.
// It ranks the provided results using the hybrid ranker and converts
// the results back to pipeline types.
func (a *RankerAdapter) Rank(ctx context.Context, query string, vectorResults, keywordResults []pipeline.Result) ([]pipeline.Result, error) {
	// Validate inputs
	if query == "" {
		return nil, ErrEmptyQuery
	}

	// Handle empty results
	if len(vectorResults) == 0 && len(keywordResults) == 0 {
		return nil, ErrEmptyResults
	}

	// Check context cancellation
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
	}

	// Convert pipeline results to ranking results
	rankingVectorResults := pipelineToRankingResults(vectorResults)
	rankingKeywordResults := pipelineToRankingResults(keywordResults)

	var scoredResults []ranking.ScoredResult
	var err error

	// Use hybrid ranking if enabled and both result sets are available
	switch {
	case a.config.EnableHybrid && len(rankingVectorResults) > 0 && len(rankingKeywordResults) > 0:
		scoredResults, err = a.hybridRanker.RankHybrid(rankingVectorResults, rankingKeywordResults, query)
	case len(rankingVectorResults) > 0:
		// Fall back to simple ranking with vector results
		scoredResults, err = a.hybridRanker.Rank(rankingVectorResults, query)
	default:
		// Fall back to simple ranking with keyword results
		scoredResults, err = a.hybridRanker.Rank(rankingKeywordResults, query)
	}

	if err != nil {
		if a.logger != nil {
			a.logger.WithError(err).Error("ranking operation failed")
		}
		return nil, ErrRankingFailed
	}

	// Check context cancellation before conversion
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
	}

	// Convert ranked results back to pipeline results
	pipelineResults := scoredResultsToPipelineResults(scoredResults)

	if a.logger != nil {
		a.logger.Debugf("ranked %d results for query", len(pipelineResults))
	}

	return pipelineResults, nil
}

// Close implements pipeline.Ranker interface.
func (a *RankerAdapter) Close() error {
	// HybridRanker doesn't have resources to close
	return nil
}

// pipelineToRankingResults converts pipeline.Result slice to ranking.Result slice.
func pipelineToRankingResults(results []pipeline.Result) []ranking.Result {
	if len(results) == 0 {
		return nil
	}

	rankingResults := make([]ranking.Result, len(results))
	for i, r := range results {
		rankingResults[i] = ranking.Result{
			ID:           r.ID,
			Content:      r.Content,
			Metadata:     r.Metadata,
			FilePath:     r.FilePath,
			StartLine:    r.StartLine,
			EndLine:      r.EndLine,
			VectorScore:  r.Score,
			KeywordScore: 0,
			RerankScore:  0,
			FinalScore:   r.Score,
		}
	}
	return rankingResults
}

// scoredResultsToPipelineResults converts ranking.ScoredResult slice to pipeline.Result slice.
func scoredResultsToPipelineResults(results []ranking.ScoredResult) []pipeline.Result {
	if len(results) == 0 {
		return nil
	}

	pipelineResults := make([]pipeline.Result, len(results))
	for i, r := range results {
		pipelineResults[i] = pipeline.Result{
			ID:        r.ID,
			Content:   r.Content,
			Metadata:  r.Metadata,
			FilePath:  r.FilePath,
			StartLine: r.StartLine,
			EndLine:   r.EndLine,
			Score:     r.FinalScore,
		}
	}
	return pipelineResults
}
