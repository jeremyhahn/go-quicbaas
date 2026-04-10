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

package indexing

import (
	"github.com/jeremyhahn/go-quicbaas/pkg/rag"
)

// indexTypeHandlers maps index type names to their factory functions for O(1) dispatch.
var indexTypeHandlers = map[string]func(cfg IndexingConfig, dimensions int) (rag.VectorIndex, error){
	"auto":   newAutoIndex,
	"memory": newMemoryIndex,
	"hnsw":   newHNSWOrMemoryIndex,
}

// NewVectorIndexFromConfig creates a VectorIndex based on the provided configuration.
// It selects the appropriate backend ("auto", "memory", or "hnsw") and returns
// a configured index with the given dimensionality.
func NewVectorIndexFromConfig(cfg IndexingConfig, dimensions int) (rag.VectorIndex, error) {
	if cfg.GPUAccelerated {
		return nil, &GPUUnavailableError{}
	}

	handler, ok := indexTypeHandlers[cfg.IndexType]
	if !ok {
		return nil, &UnsupportedIndexTypeError{IndexType: cfg.IndexType}
	}
	return handler(cfg, dimensions)
}

// newAutoIndex tries HNSW first, then falls back to memory.
func newAutoIndex(cfg IndexingConfig, dimensions int) (rag.VectorIndex, error) {
	// HNSW is not yet available as a standalone package,
	// so "auto" currently resolves to the in-memory brute-force index.
	return newMemoryIndex(cfg, dimensions)
}

// newMemoryIndex creates an in-memory brute-force vector index.
func newMemoryIndex(_ IndexingConfig, dimensions int) (rag.VectorIndex, error) {
	return rag.NewVectorIndex(&rag.VectorIndexConfig{
		Dimensions:     dimensions,
		DistanceMetric: rag.CosineDistance,
	})
}

// newHNSWOrMemoryIndex creates an HNSW index when the qrdb HNSW adapter is
// available, otherwise falls back to the in-memory brute-force index. The HNSW
// adapter will be wired in when the qrdb vector package exposes a standalone
// HNSW constructor compatible with the rag.VectorIndex interface.
func newHNSWOrMemoryIndex(cfg IndexingConfig, dimensions int) (rag.VectorIndex, error) {
	return newMemoryIndex(cfg, dimensions)
}
