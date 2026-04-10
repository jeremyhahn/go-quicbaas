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
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sync"
	"time"
)

// EmbeddingProvider generates embeddings from text.
type EmbeddingProvider interface {
	// Encode generates an embedding for the given text.
	Encode(ctx context.Context, text string) ([]float32, error)
}

// VectorSearcher performs vector similarity search.
type VectorSearcher interface {
	// Search performs vector similarity search.
	Search(ctx context.Context, vector []float32, opts SearchOptions) ([]Result, error)
}

// SearchOptions configures vector search.
type SearchOptions struct {
	TopK     int
	MinScore float32
	NProbe   int
	Filters  map[string]interface{}
}

// handler implements the Handler interface.
type handler struct {
	embedder EmbeddingProvider
	searcher VectorSearcher
	cache    Cache
	config   Config

	mu     sync.RWMutex
	closed bool

	// Cleanup goroutine
	stopCleanup chan struct{}
	wg          sync.WaitGroup
}

// NewHandler creates a new query handler.
func NewHandler(embedder EmbeddingProvider, searcher VectorSearcher, config Config) (Handler, error) {
	if err := config.Validate(); err != nil {
		return nil, &SentinelCauseError{Sentinel: ErrInvalidOptions, Cause: err}
	}

	if embedder == nil {
		return nil, ErrNilEmbedder
	}

	if searcher == nil {
		return nil, ErrNilSearcher
	}

	var cache Cache
	if config.EnableCache {
		cache = NewCache(config.CacheSize)
	}

	h := &handler{
		embedder:    embedder,
		searcher:    searcher,
		cache:       cache,
		config:      config,
		stopCleanup: make(chan struct{}),
	}

	// Start cleanup goroutine if cache is enabled
	if config.EnableCache {
		h.wg.Add(1)
		go h.cleanupLoop()
	}

	return h, nil
}

// Query executes a complete RAG query and returns results.
func (h *handler) Query(ctx context.Context, query string, opts Options) (*Response, error) {
	h.mu.RLock()
	if h.closed {
		h.mu.RUnlock()
		return nil, ErrClosed
	}
	h.mu.RUnlock()

	start := time.Now()

	// Apply defaults
	opts = h.applyDefaults(opts)

	// Validate query
	if query == "" {
		return nil, ErrEmptyQuery
	}

	// Create context with timeout
	ctx, cancel := context.WithTimeout(ctx, h.config.QueryTimeout)
	defer cancel()

	// Check cache
	if h.cache != nil {
		cacheKey := h.cacheKey(query, opts)
		if cached, ok := h.cache.Get(cacheKey); ok {
			cached.CacheHit = true
			return cached, nil
		}
	}

	// Parse query
	parsed, err := ParseQuery(query)
	if err != nil {
		return nil, &SentinelCauseError{Sentinel: ErrQueryParseFailed, Cause: err}
	}

	// Generate embedding
	embedding, err := h.embedder.Encode(ctx, parsed.Original)
	if err != nil {
		return nil, &SentinelCauseError{Sentinel: ErrEmbeddingFailed, Cause: err}
	}

	// Search
	searchOpts := SearchOptions{
		TopK:     opts.TopK,
		MinScore: opts.MinScore,
		NProbe:   opts.NProbe,
		Filters:  opts.Filters,
	}

	results, err := h.searcher.Search(ctx, embedding, searchOpts)
	if err != nil {
		return nil, &SentinelCauseError{Sentinel: ErrSearchFailed, Cause: err}
	}

	// Build context (simple implementation for now)
	contextText := h.buildContext(results, opts)

	// Create response
	response := &Response{
		Query:          query,
		Results:        results,
		Context:        contextText,
		ProcessingTime: time.Since(start),
		CacheHit:       false,
	}

	// Cache result
	if h.cache != nil {
		cacheKey := h.cacheKey(query, opts)
		h.cache.Set(cacheKey, response, h.config.CacheTTL)
	}

	return response, nil
}

// StreamQuery executes a streaming RAG query.
func (h *handler) StreamQuery(ctx context.Context, query string, opts Options) (<-chan StreamChunk, error) {
	h.mu.RLock()
	if h.closed {
		h.mu.RUnlock()
		return nil, ErrClosed
	}
	h.mu.RUnlock()

	ch := make(chan StreamChunk, 10)

	go func() {
		defer close(ch)

		// Execute query
		response, err := h.Query(ctx, query, opts)
		if err != nil {
			ch <- StreamChunk{Error: err}
			return
		}

		// Stream results
		for i := range response.Results {
			select {
			case <-ctx.Done():
				ch <- StreamChunk{Error: ErrContextCanceled}
				return
			case ch <- StreamChunk{Result: &response.Results[i]}:
			}
		}

		// Send done
		ch <- StreamChunk{Done: true}
	}()

	return ch, nil
}

// Close releases resources.
func (h *handler) Close() error {
	h.mu.Lock()
	defer h.mu.Unlock()

	if h.closed {
		return nil
	}

	h.closed = true

	// Stop cleanup goroutine
	if h.stopCleanup != nil {
		close(h.stopCleanup)
		h.wg.Wait()
	}

	// Clear cache
	if h.cache != nil {
		h.cache.Clear()
	}

	return nil
}

// applyDefaults applies default options where not specified.
func (h *handler) applyDefaults(opts Options) Options {
	if opts.TopK == 0 {
		opts.TopK = h.config.DefaultTopK
	}
	if opts.MinScore == 0 {
		opts.MinScore = h.config.DefaultMinScore
	}
	if opts.NProbe == 0 {
		opts.NProbe = h.config.DefaultNProbe
	}
	return opts
}

// cacheKey generates a cache key from query and options.
func (h *handler) cacheKey(query string, opts Options) string {
	// Create deterministic key from query and options
	data := struct {
		Query    string
		TopK     int
		MinScore float32
		NProbe   int
		Filters  map[string]interface{}
	}{
		Query:    query,
		TopK:     opts.TopK,
		MinScore: opts.MinScore,
		NProbe:   opts.NProbe,
		Filters:  opts.Filters,
	}

	bytes, _ := json.Marshal(data)
	hash := sha256.Sum256(bytes)
	return hex.EncodeToString(hash[:])
}

// buildContext builds context from results.
func (h *handler) buildContext(results []Result, opts Options) string {
	if len(results) == 0 {
		return "No relevant results found."
	}

	// Simple context building - just concatenate results
	var context string
	for i, result := range results {
		if i > 0 {
			context += "\n\n"
		}
		context += fmt.Sprintf("Result %d (score: %.2f):\n%s", i+1, result.Score, result.Content)

		if opts.IncludeSource {
			if source, ok := result.Metadata["source"].(string); ok {
				context += "\nSource: " + source
			}
		}
	}

	return context
}

// cleanupLoop periodically cleans expired cache entries.
func (h *handler) cleanupLoop() {
	defer h.wg.Done()

	ticker := time.NewTicker(h.config.CleanupInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			if lruCache, ok := h.cache.(*lruCache); ok {
				lruCache.CleanExpired()
			}
		case <-h.stopCleanup:
			return
		}
	}
}
