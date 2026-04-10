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

package ranking

// Config holds configuration for ranking components.
type Config struct {
	// BM25 parameters
	BM25K1 float32 // Term frequency saturation (default: 1.2)
	BM25B  float32 // Length normalization (default: 0.75)

	// Fusion weights
	VectorWeight  float32 // Weight for vector search (default: 0.7)
	KeywordWeight float32 // Weight for keyword search (default: 0.3)

	// Reranking
	RerankTopK   int  // Number of candidates to rerank (default: 50)
	EnableRerank bool // Enable reranking (default: true)

	// RRF parameter
	RRFK float32 // Reciprocal rank fusion constant (default: 60)
}

// DefaultConfig returns the default ranking configuration.
func DefaultConfig() Config {
	return Config{
		BM25K1:        1.2,
		BM25B:         0.75,
		VectorWeight:  0.7,
		KeywordWeight: 0.3,
		RerankTopK:    50,
		EnableRerank:  true,
		RRFK:          60,
	}
}

// Validate checks if the configuration is valid.
func (c *Config) Validate() error {
	if c.BM25K1 <= 0 {
		return ErrInvalidConfig
	}
	if c.BM25B < 0 || c.BM25B > 1 {
		return ErrInvalidConfig
	}
	if c.VectorWeight < 0 || c.VectorWeight > 1 {
		return ErrInvalidConfig
	}
	if c.KeywordWeight < 0 || c.KeywordWeight > 1 {
		return ErrInvalidConfig
	}
	if c.VectorWeight+c.KeywordWeight > 1.0 {
		return ErrInvalidConfig
	}
	if c.RerankTopK <= 0 {
		return ErrInvalidConfig
	}
	if c.RRFK <= 0 {
		return ErrInvalidConfig
	}
	return nil
}
