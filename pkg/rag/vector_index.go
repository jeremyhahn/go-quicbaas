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

package rag

import (
	"context"
	"math"
	"sort"
	"sync"

	"github.com/jeremyhahn/go-quicbaas/pkg/config"
	"github.com/jeremyhahn/go-quicbaas/pkg/embedding"
	"github.com/jeremyhahn/go-quicbaas/pkg/rag/pipeline"
)

// DistanceMetric defines the distance function used for vector similarity.
type DistanceMetric int

const (
	// CosineDistance measures the cosine similarity between vectors.
	CosineDistance DistanceMetric = iota
	// L2Distance measures Euclidean distance between vectors.
	L2Distance
	// DotProductDistance measures dot product similarity between vectors.
	DotProductDistance
)

// VectorIndexConfig configures the in-memory vector index.
type VectorIndexConfig struct {
	// Dimensions is the vector dimensionality.
	Dimensions int
	// DistanceMetric is the distance function used for similarity.
	DistanceMetric DistanceMetric
	// NumCentroids is the number of clusters (used for training).
	NumCentroids int
	// DefaultNProbe is the default number of clusters to search.
	DefaultNProbe int
}

// VectorSearchResult represents a single search result from the vector index.
type VectorSearchResult struct {
	// ID is the vector identifier.
	ID string
	// Score is the similarity score.
	Score float32
}

// VectorIndex defines the interface for an in-memory vector index.
type VectorIndex interface {
	// Train trains the index with sample vectors for clustering.
	Train(vectors [][]float32) error
	// IsTrained returns whether the index has been trained.
	IsTrained() bool
	// Add inserts a vector with an identifier and metadata.
	Add(id string, vector []float32, metadata map[string]interface{}) error
	// Search finds the k most similar vectors to the query.
	Search(query []float32, k int, nprobe int) ([]VectorSearchResult, error)
	// Remove deletes a vector by its identifier.
	Remove(id string) error
	// Save persists the index to disk at the given path.
	Save(path string) error
	// Load restores the index from disk at the given path.
	Load(path string) error
	// Size returns the number of vectors in the index.
	Size() int
}

// vectorEntry stores a single indexed vector with its metadata.
type vectorEntry struct {
	id       string
	vector   []float32
	metadata map[string]interface{}
}

// inMemoryVectorIndex provides a simple in-memory vector index with brute-force search.
// It supports training (for API compatibility) and exact nearest-neighbor search.
type inMemoryVectorIndex struct {
	mu      sync.RWMutex
	entries []vectorEntry
	idIndex map[string]int
	config  VectorIndexConfig
	trained bool
}

// NewVectorIndex creates a new in-memory vector index with the given configuration.
func NewVectorIndex(cfg *VectorIndexConfig) (VectorIndex, error) {
	if cfg.Dimensions <= 0 {
		return nil, &VectorIndexConfigError{Field: "Dimensions", Reason: "must be positive"}
	}
	return &inMemoryVectorIndex{
		entries: make([]vectorEntry, 0),
		idIndex: make(map[string]int),
		config:  *cfg,
		trained: false,
	}, nil
}

// Train marks the index as trained. The in-memory index does not require
// actual clustering, so training is a no-op aside from marking status.
func (idx *inMemoryVectorIndex) Train(vectors [][]float32) error {
	idx.mu.Lock()
	defer idx.mu.Unlock()
	idx.trained = true
	return nil
}

// IsTrained returns whether the index has been trained.
func (idx *inMemoryVectorIndex) IsTrained() bool {
	idx.mu.RLock()
	defer idx.mu.RUnlock()
	return idx.trained
}

// Add inserts a vector into the index. If the id already exists, it is replaced.
func (idx *inMemoryVectorIndex) Add(id string, vector []float32, metadata map[string]interface{}) error {
	if len(vector) != idx.config.Dimensions {
		return &VectorDimensionError{
			Expected: idx.config.Dimensions,
			Got:      len(vector),
		}
	}

	idx.mu.Lock()
	defer idx.mu.Unlock()

	// Check if the id already exists and replace it.
	if existingIdx, ok := idx.idIndex[id]; ok {
		idx.entries[existingIdx] = vectorEntry{
			id:       id,
			vector:   vector,
			metadata: metadata,
		}
		return nil
	}

	idx.idIndex[id] = len(idx.entries)
	idx.entries = append(idx.entries, vectorEntry{
		id:       id,
		vector:   vector,
		metadata: metadata,
	})
	return nil
}

// Search performs a brute-force nearest-neighbor search, returning up to k results.
// The nprobe parameter is accepted for API compatibility but is unused by the
// brute-force implementation.
func (idx *inMemoryVectorIndex) Search(query []float32, k int, nprobe int) ([]VectorSearchResult, error) {
	if len(query) != idx.config.Dimensions {
		return nil, &VectorDimensionError{
			Expected: idx.config.Dimensions,
			Got:      len(query),
		}
	}

	idx.mu.RLock()
	defer idx.mu.RUnlock()

	if len(idx.entries) == 0 {
		return nil, nil
	}

	// Compute distance for each entry.
	type scored struct {
		id    string
		score float32
	}
	results := make([]scored, 0, len(idx.entries))

	for _, entry := range idx.entries {
		var score float32
		switch idx.config.DistanceMetric {
		case CosineDistance:
			score = cosineSimilarity(query, entry.vector)
		case DotProductDistance:
			score = dotProduct(query, entry.vector)
		case L2Distance:
			dist := l2Distance(query, entry.vector)
			// Convert distance to similarity (higher is better).
			score = 1.0 / (1.0 + dist)
		default:
			score = cosineSimilarity(query, entry.vector)
		}
		results = append(results, scored{id: entry.id, score: score})
	}

	// Sort by score descending.
	sort.Slice(results, func(i, j int) bool {
		return results[i].score > results[j].score
	})

	// Limit to k results.
	if k > len(results) {
		k = len(results)
	}

	output := make([]VectorSearchResult, k)
	for i := 0; i < k; i++ {
		output[i] = VectorSearchResult{
			ID:    results[i].id,
			Score: results[i].score,
		}
	}
	return output, nil
}

// Remove deletes a vector by its identifier.
func (idx *inMemoryVectorIndex) Remove(id string) error {
	idx.mu.Lock()
	defer idx.mu.Unlock()

	entryIdx, ok := idx.idIndex[id]
	if !ok {
		return nil // Not found, nothing to remove.
	}

	// Swap with the last entry for O(1) removal.
	lastIdx := len(idx.entries) - 1
	if entryIdx != lastIdx {
		idx.entries[entryIdx] = idx.entries[lastIdx]
		idx.idIndex[idx.entries[entryIdx].id] = entryIdx
	}
	idx.entries = idx.entries[:lastIdx]
	delete(idx.idIndex, id)

	return nil
}

// Save is a no-op for the in-memory index. Persistence is not supported.
func (idx *inMemoryVectorIndex) Save(path string) error {
	return nil
}

// Load is a no-op for the in-memory index. Persistence is not supported.
func (idx *inMemoryVectorIndex) Load(path string) error {
	return nil
}

// Size returns the number of vectors in the index.
func (idx *inMemoryVectorIndex) Size() int {
	idx.mu.RLock()
	defer idx.mu.RUnlock()
	return len(idx.entries)
}

// RAGSearcher bridges a VectorIndex and embedding.Provider to implement
// pipeline.VectorSearcher. It handles auto-embedding of content on add
// and similarity search using pre-computed query embeddings.
type RAGSearcher struct {
	index    VectorIndex
	embedder embedding.Provider

	mu       sync.RWMutex
	contents map[string]string
}

// NewRAGSearcher creates a new RAGSearcher wrapping the given index and embedder.
func NewRAGSearcher(index VectorIndex, embedder embedding.Provider) *RAGSearcher {
	return &RAGSearcher{
		index:    index,
		embedder: embedder,
		contents: make(map[string]string),
	}
}

// Add embeds the content and stores it in the vector index.
func (rs *RAGSearcher) Add(ctx context.Context, id string, content string, metadata map[string]interface{}) error {
	emb, err := rs.embedder.Embed(ctx, content)
	if err != nil {
		return err
	}

	rs.mu.Lock()
	rs.contents[id] = content
	rs.mu.Unlock()

	return rs.index.Add(id, emb, metadata)
}

// Search performs a similarity search using the provided embedding vector.
// It satisfies pipeline.VectorSearcher.
func (rs *RAGSearcher) Search(ctx context.Context, queryEmbedding []float32, opts pipeline.SearchOptions) ([]pipeline.Result, error) {
	k := opts.TopK
	if k <= 0 {
		k = 10
	}

	results, err := rs.index.Search(queryEmbedding, k, 0)
	if err != nil {
		return nil, err
	}

	pipelineResults := make([]pipeline.Result, 0, len(results))
	rs.mu.RLock()
	defer rs.mu.RUnlock()

	for _, r := range results {
		if opts.MinScore > 0 && r.Score < opts.MinScore {
			continue
		}
		content := rs.contents[r.ID]
		pipelineResults = append(pipelineResults, pipeline.Result{
			ID:      r.ID,
			Content: content,
			Score:   r.Score,
		})
	}

	return pipelineResults, nil
}

// Close is a no-op. The RAGSearcher does not own the index or embedder lifecycle.
func (rs *RAGSearcher) Close() error {
	return nil
}

// createVectorIndexFromConfig creates a VectorIndex from the RAG vector configuration.
func createVectorIndexFromConfig(cfg config.RAGVectorConfig) (VectorIndex, error) {
	var metric DistanceMetric
	switch cfg.Metric {
	case "cosine":
		metric = CosineDistance
	case "l2":
		metric = L2Distance
	case "ip", "dot":
		metric = DotProductDistance
	default:
		metric = CosineDistance
	}

	indexCfg := &VectorIndexConfig{
		Dimensions:     cfg.Dimensions,
		DistanceMetric: metric,
		NumCentroids:   cfg.NumCentroids,
		DefaultNProbe:  cfg.NProbe,
	}

	return NewVectorIndex(indexCfg)
}

// Distance computation functions.

// cosineSimilarity computes the cosine similarity between two vectors.
func cosineSimilarity(a, b []float32) float32 {
	var dotProd, normA, normB float64
	for i := range a {
		ai := float64(a[i])
		bi := float64(b[i])
		dotProd += ai * bi
		normA += ai * ai
		normB += bi * bi
	}
	denom := math.Sqrt(normA) * math.Sqrt(normB)
	if denom == 0 {
		return 0
	}
	return float32(dotProd / denom)
}

// dotProduct computes the dot product between two vectors.
func dotProduct(a, b []float32) float32 {
	var sum float64
	for i := range a {
		sum += float64(a[i]) * float64(b[i])
	}
	return float32(sum)
}

// l2Distance computes the Euclidean distance between two vectors.
func l2Distance(a, b []float32) float32 {
	var sum float64
	for i := range a {
		diff := float64(a[i]) - float64(b[i])
		sum += diff * diff
	}
	return float32(math.Sqrt(sum))
}

// VectorIndexConfigError indicates an invalid vector index configuration.
type VectorIndexConfigError struct {
	Field  string
	Reason string
}

// Error implements the error interface.
func (e *VectorIndexConfigError) Error() string {
	return "invalid vector index config: " + e.Field + " " + e.Reason
}

// VectorDimensionError indicates a vector dimension mismatch.
type VectorDimensionError struct {
	Expected int
	Got      int
}

// Error implements the error interface.
func (e *VectorDimensionError) Error() string {
	return "vector dimension mismatch"
}
