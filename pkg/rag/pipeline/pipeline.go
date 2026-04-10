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
	"errors"
	"sync"
	"time"
)

// pipelineImpl implements the Pipeline interface.
type pipelineImpl struct {
	config Config

	// Components
	queryHandler   QueryHandler
	vectorSearcher VectorSearcher
	ranker         Ranker
	contextBuilder ContextBuilder

	// State management
	mu        sync.RWMutex
	closed    bool
	semaphore chan struct{} // Concurrency limiter

	// Resilience
	circuitBreaker *CircuitBreaker

	// Observability
	metrics *Metrics
}

// NewPipeline creates a new RAG pipeline.
func NewPipeline(
	config Config,
	queryHandler QueryHandler,
	vectorSearcher VectorSearcher,
	ranker Ranker,
	contextBuilder ContextBuilder,
) (Pipeline, error) {
	// Validate config
	if err := config.Validate(); err != nil {
		return nil, &PipelineError{
			Err:       err,
			Component: "pipeline",
			Operation: "validate_config",
		}
	}

	// Validate dependencies
	if queryHandler == nil {
		return nil, ErrMissingDependency
	}
	if vectorSearcher == nil {
		return nil, ErrMissingDependency
	}
	if ranker == nil {
		return nil, ErrMissingDependency
	}
	if contextBuilder == nil {
		return nil, ErrMissingDependency
	}

	p := &pipelineImpl{
		config:         config,
		queryHandler:   queryHandler,
		vectorSearcher: vectorSearcher,
		ranker:         ranker,
		contextBuilder: contextBuilder,
		semaphore:      make(chan struct{}, config.MaxConcurrentQueries),
	}

	// Initialize circuit breaker
	if config.EnableCircuitBreaker {
		p.circuitBreaker = NewCircuitBreaker(
			config.CircuitBreakerThreshold,
			config.CircuitBreakerTimeout,
		)
	}

	// Initialize metrics
	if config.EnableMetrics {
		p.metrics = NewMetrics(config.MetricsNamespace)
	}

	return p, nil
}

// Query executes a complete RAG query.
func (p *pipelineImpl) Query(ctx context.Context, query string, opts QueryOptions) (*QueryResponse, error) {
	// Check if pipeline is closed
	p.mu.RLock()
	if p.closed {
		p.mu.RUnlock()
		return nil, ErrPipelineClosed
	}
	p.mu.RUnlock()

	// Validate query
	if query == "" {
		return nil, ErrInvalidQuery
	}

	// Apply concurrency limit
	select {
	case p.semaphore <- struct{}{}:
		defer func() { <-p.semaphore }()
	case <-ctx.Done():
		return nil, ErrQueryTimeout
	}

	// Apply query timeout
	if opts.Timeout == 0 {
		opts.Timeout = p.config.QueryTimeout
	}
	ctx, cancel := context.WithTimeout(ctx, opts.Timeout)
	defer cancel()

	// Execute pipeline with circuit breaker
	var response *QueryResponse
	var err error

	if p.circuitBreaker != nil {
		err = p.circuitBreaker.Execute(func() error {
			response, err = p.executePipeline(ctx, query, opts)
			return err
		})
	} else {
		response, err = p.executePipeline(ctx, query, opts)
	}

	// Record metrics
	if p.metrics != nil {
		if err != nil {
			p.metrics.RecordQueryError(err)
		} else {
			// Convert QueryResponse to metrics Response
			metricsResp := &Response{
				Query:             response.Query,
				ProcessingTime:    response.ProcessingTime,
				VectorSearchTime:  response.VectorSearchTime,
				KeywordSearchTime: response.KeywordSearchTime,
				RerankTime:        response.RerankTime,
				ContextBuildTime:  response.ContextBuildTime,
				CacheHit:          response.CacheHit,
				AvgRelevance:      response.AvgRelevance,
				Results:           make([]interface{}, len(response.Results)),
			}
			p.metrics.RecordQuery(metricsResp)
		}
	}

	return response, err
}

// executePipeline performs the actual pipeline execution.
func (p *pipelineImpl) executePipeline(ctx context.Context, query string, opts QueryOptions) (*QueryResponse, error) {
	start := time.Now()

	// Apply defaults
	opts = p.applyDefaults(opts)

	// Step 1: Process query (parse, cache check, embedding)
	processed, err := p.queryHandler.Process(ctx, query)
	if err != nil {
		return nil, NewPipelineError(err, "query_handler", "process", nil)
	}

	// Check cache hit
	if processed.CacheHit && processed.CachedResp != nil {
		processed.CachedResp.CacheHit = true
		return processed.CachedResp, nil
	}

	// Step 2: Vector search
	vectorStart := time.Now()
	searchOpts := SearchOptions{
		TopK:      opts.RerankTopK, // Get more for reranking
		MinScore:  opts.MinScore,
		Namespace: opts.Namespace,
		Filters:   opts.Filters,
	}
	vectorResults, err := p.vectorSearcher.Search(ctx, processed.Embedding, searchOpts)
	if err != nil {
		return nil, NewPipelineError(err, "vector_search", "search", nil)
	}
	vectorSearchTime := time.Since(vectorStart)

	// Handle empty vector search results - return early with empty response
	if len(vectorResults) == 0 {
		return &QueryResponse{
			Query:            query,
			Results:          nil,
			Context:          "",
			Intent:           processed.Intent,
			ProcessingTime:   time.Since(start),
			VectorSearchTime: vectorSearchTime,
			ResultsFound:     0,
		}, nil
	}

	// Step 3: Rank results (includes keyword scoring and fusion)
	rankStart := time.Now()
	rankedResults, err := p.ranker.Rank(ctx, query, vectorResults, nil)
	if err != nil {
		return nil, NewPipelineError(err, "ranker", "rank", nil)
	}
	rankTime := time.Since(rankStart)

	// Filter by MinScore after ranking
	if opts.MinScore > 0 {
		filteredResults := make([]Result, 0, len(rankedResults))
		for _, r := range rankedResults {
			if r.Score >= opts.MinScore {
				filteredResults = append(filteredResults, r)
			}
		}
		rankedResults = filteredResults
	}

	// Limit to TopK
	if len(rankedResults) > opts.TopK {
		rankedResults = rankedResults[:opts.TopK]
	}

	// Handle empty results after filtering - return early with empty response
	if len(rankedResults) == 0 {
		return &QueryResponse{
			Query:            query,
			Results:          nil,
			Context:          "",
			Intent:           processed.Intent,
			ProcessingTime:   time.Since(start),
			VectorSearchTime: vectorSearchTime,
			RerankTime:       rankTime,
			ResultsFound:     len(vectorResults),
		}, nil
	}

	// Step 4: Build context
	contextStart := time.Now()
	contextOpts := ContextOptions{
		MaxTokens:     opts.MaxTokens,
		IncludeSource: opts.IncludeSource,
		Template:      opts.Template,
		Format:        opts.Format,
	}
	built, err := p.contextBuilder.Build(ctx, rankedResults, contextOpts)
	if err != nil {
		return nil, NewPipelineError(err, "context_builder", "build", nil)
	}
	contextBuildTime := time.Since(contextStart)

	// Calculate average relevance
	var totalRelevance float32
	for _, r := range rankedResults {
		totalRelevance += r.Score
	}
	avgRelevance := float32(0)
	if len(rankedResults) > 0 {
		avgRelevance = totalRelevance / float32(len(rankedResults))
	}

	// Build response
	response := &QueryResponse{
		Query:             query,
		Results:           rankedResults,
		Context:           built.Content,
		ContextTokens:     built.Tokens,
		Truncated:         built.Truncated,
		Intent:            processed.Intent,
		ProcessingTime:    time.Since(start),
		VectorSearchTime:  vectorSearchTime,
		KeywordSearchTime: 0, // Not tracked separately in this simplified version
		RerankTime:        rankTime,
		ContextBuildTime:  contextBuildTime,
		CacheHit:          false,
		AvgRelevance:      avgRelevance,
		ResultsFound:      len(vectorResults),
	}

	return response, nil
}

// applyDefaults applies default values to query options.
func (p *pipelineImpl) applyDefaults(opts QueryOptions) QueryOptions {
	if opts.TopK == 0 {
		opts.TopK = p.config.DefaultTopK
	}
	if opts.MinScore == 0 {
		opts.MinScore = p.config.DefaultMinScore
	}
	if opts.Namespace == "" {
		opts.Namespace = p.config.DefaultNamespace
	}
	if opts.RerankTopK == 0 {
		opts.RerankTopK = opts.TopK * 5 // Get 5x for reranking
	}
	if opts.MaxTokens == 0 {
		opts.MaxTokens = p.config.DefaultMaxTokens
	}
	if opts.Timeout == 0 {
		opts.Timeout = p.config.QueryTimeout
	}

	// Apply config defaults
	if !opts.EnableHybrid {
		opts.EnableHybrid = p.config.EnableHybrid
	}
	if !opts.EnableRerank {
		opts.EnableRerank = p.config.EnableRerank
	}

	return opts
}

// Stats returns pipeline statistics.
func (p *pipelineImpl) Stats() Stats {
	if p.metrics == nil {
		return Stats{}
	}
	return p.metrics.GetStats()
}

// Health returns pipeline health status.
func (p *pipelineImpl) Health() HealthStatus {
	p.mu.RLock()
	defer p.mu.RUnlock()

	health := HealthStatus{
		ComponentHealth: make(map[string]bool),
		LastCheck:       time.Now(),
	}

	// Check component health
	health.ComponentHealth["query_handler"] = p.queryHandler != nil
	health.ComponentHealth["vector_searcher"] = p.vectorSearcher != nil
	health.ComponentHealth["ranker"] = p.ranker != nil
	health.ComponentHealth["context_builder"] = p.contextBuilder != nil

	// Check circuit breaker state
	if p.circuitBreaker != nil {
		health.ComponentHealth["circuit_breaker"] = !p.circuitBreaker.IsOpen()
	}

	// Determine overall health
	health.Healthy = true
	for _, healthy := range health.ComponentHealth {
		if !healthy {
			health.Healthy = false
			health.Status = "degraded"
			break
		}
	}

	if health.Healthy {
		health.Status = "healthy"
	} else if len(health.ComponentHealth) == 0 {
		health.Status = "unhealthy"
	}

	return health
}

// Close releases all resources.
func (p *pipelineImpl) Close() error {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.closed {
		return ErrPipelineClosed
	}

	var errs []error

	// Close components
	if p.queryHandler != nil {
		if err := p.queryHandler.Close(); err != nil {
			errs = append(errs, &PipelineError{
				Err:       err,
				Component: "query_handler",
				Operation: "close",
			})
		}
	}

	if p.vectorSearcher != nil {
		if err := p.vectorSearcher.Close(); err != nil {
			errs = append(errs, &PipelineError{
				Err:       err,
				Component: "vector_searcher",
				Operation: "close",
			})
		}
	}

	if p.ranker != nil {
		if err := p.ranker.Close(); err != nil {
			errs = append(errs, &PipelineError{
				Err:       err,
				Component: "ranker",
				Operation: "close",
			})
		}
	}

	if p.contextBuilder != nil {
		if err := p.contextBuilder.Close(); err != nil {
			errs = append(errs, &PipelineError{
				Err:       err,
				Component: "context_builder",
				Operation: "close",
			})
		}
	}

	p.closed = true

	if len(errs) > 0 {
		return errors.Join(errs...)
	}

	return nil
}
