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

package config

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDefaultRAGConfig(t *testing.T) {
	cfg := DefaultRAGConfig()

	assert.False(t, cfg.Enabled)
	assert.Equal(t, "./data/rag_index", cfg.IndexPath)
	assert.NotEmpty(t, cfg.Embedding.Provider)
	assert.NotEmpty(t, cfg.Embedding.Model)
	assert.Greater(t, cfg.Vector.NumCentroids, 0)
	assert.Greater(t, cfg.Vector.Dimensions, 0)
	assert.Greater(t, cfg.Query.CacheSize, 0)
	assert.Greater(t, cfg.Query.CacheTTL, time.Duration(0))
	assert.NotEmpty(t, cfg.Ranking.Strategy)
	assert.Greater(t, cfg.Context.MaxTokens, 0)
	assert.NotEmpty(t, cfg.Context.DefaultTemplate)
	assert.Greater(t, cfg.Pipeline.Timeout, time.Duration(0))
	assert.Greater(t, cfg.Pipeline.MaxConcurrentQueries, 0)
}

func TestRAGConfig_Validate_Success(t *testing.T) {
	tests := []struct {
		name   string
		config RAGConfig
	}{
		{
			name:   "default config",
			config: DefaultRAGConfig(),
		},
		{
			name: "disabled config",
			config: RAGConfig{
				Enabled: false,
			},
		},
		{
			name: "custom valid config",
			config: RAGConfig{
				Enabled:   true,
				IndexPath: "/tmp/test",
				Embedding: EmbeddingConfig{
					Provider:   "ollama",
					Model:      "all-minilm",
					APIURL:     "http://localhost:11434",
					Dimensions: 384,
				},
				Vector: RAGVectorConfig{
					NumCentroids: 100,
					Dimensions:   384,
					NProbe:       10,
				},
				Query: RAGQueryConfig{
					CacheSize: 1000,
					CacheTTL:  time.Hour,
				},
				Ranking: RankingConfig{
					Strategy:      "hybrid",
					BM25K1:        1.2,
					BM25B:         0.75,
					VectorWeight:  0.7,
					KeywordWeight: 0.3,
				},
				Context: ContextConfig{
					MaxTokens:          4000,
					DefaultTemplate:    "markdown",
					MaxResultsPerChunk: 20,
					TokenMultiplier:    1.3,
				},
				Pipeline: PipelineConfig{
					Timeout:              30 * time.Second,
					MaxConcurrentQueries: 100,
					EnableCircuitBreaker: true,
					DefaultTopK:          10,
					DefaultMaxTokens:     8000,
					DefaultMinScore:      0.7,
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.config.Validate()
			assert.NoError(t, err)
		})
	}
}

func TestRAGConfig_Validate_Errors(t *testing.T) {
	tests := []struct {
		name    string
		config  RAGConfig
		wantErr string
	}{
		{
			name: "invalid embedding provider",
			config: RAGConfig{
				Enabled: true,
				Embedding: EmbeddingConfig{
					Provider: "",
				},
			},
			wantErr: "embedding provider",
		},
		{
			name: "invalid embedding dimensions",
			config: RAGConfig{
				Enabled: true,
				Embedding: EmbeddingConfig{
					Provider:   "ollama",
					Dimensions: 0,
				},
			},
			wantErr: "dimensions",
		},
		{
			name: "invalid vector dimensions",
			config: RAGConfig{
				Enabled: true,
				Embedding: EmbeddingConfig{
					Provider:   "ollama",
					APIURL:     "http://localhost:11434",
					Dimensions: 384,
				},
				Vector: RAGVectorConfig{
					NumCentroids: 100,
					Dimensions:   0,
					NProbe:       10,
				},
			},
			wantErr: "vector dimensions",
		},
		{
			name: "dimension mismatch",
			config: RAGConfig{
				Enabled: true,
				Embedding: EmbeddingConfig{
					Provider:   "ollama",
					APIURL:     "http://localhost:11434",
					Dimensions: 384,
				},
				Vector: RAGVectorConfig{
					NumCentroids: 100,
					Dimensions:   512, // Mismatch
					NProbe:       10,
				},
			},
			wantErr: "dimension mismatch",
		},
		{
			name: "invalid cache size",
			config: RAGConfig{
				Enabled: true,
				Embedding: EmbeddingConfig{
					Provider:   "ollama",
					APIURL:     "http://localhost:11434",
					Dimensions: 384,
				},
				Vector: RAGVectorConfig{
					NumCentroids: 100,
					Dimensions:   384,
					NProbe:       10,
				},
				Query: RAGQueryConfig{
					CacheSize: -1,
				},
			},
			wantErr: "cache size",
		},
		{
			name: "invalid pipeline timeout",
			config: RAGConfig{
				Enabled: true,
				Embedding: EmbeddingConfig{
					Provider:   "ollama",
					APIURL:     "http://localhost:11434",
					Dimensions: 384,
				},
				Vector: RAGVectorConfig{
					NumCentroids: 100,
					Dimensions:   384,
					NProbe:       10,
				},
				Query: RAGQueryConfig{
					CacheSize: 100,
				},
				Ranking: RankingConfig{
					Strategy:      "hybrid",
					BM25K1:        1.2,
					BM25B:         0.75,
					VectorWeight:  0.7,
					KeywordWeight: 0.3,
				},
				Context: ContextConfig{
					MaxTokens:          4000,
					DefaultTemplate:    "markdown",
					MaxResultsPerChunk: 20,
					TokenMultiplier:    1.3,
				},
				Pipeline: PipelineConfig{
					Timeout: 0,
				},
			},
			wantErr: "timeout",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.config.Validate()
			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.wantErr)
		})
	}
}

func TestRAGConfig_MergeDefaults(t *testing.T) {
	cfg := RAGConfig{
		Enabled:   true,
		IndexPath: "/custom/path",
	}

	merged := cfg.MergeDefaults()

	assert.True(t, merged.Enabled)
	assert.Equal(t, "/custom/path", merged.IndexPath)
	assert.NotEmpty(t, merged.Embedding.Provider)
	assert.Greater(t, merged.Vector.NumCentroids, 0)
}

func TestEmbeddingConfig_Validate(t *testing.T) {
	tests := []struct {
		name    string
		config  EmbeddingConfig
		wantErr bool
	}{
		{
			name: "valid ollama",
			config: EmbeddingConfig{
				Provider:   "ollama",
				Model:      "all-minilm",
				APIURL:     "http://localhost:11434",
				Dimensions: 384,
			},
			wantErr: false,
		},
		{
			name: "valid openai",
			config: EmbeddingConfig{
				Provider:   "openai",
				Model:      "text-embedding-3-small",
				APIKey:     "sk-test",
				Dimensions: 1536,
			},
			wantErr: false,
		},
		{
			name: "missing provider",
			config: EmbeddingConfig{
				Model:      "test",
				Dimensions: 384,
			},
			wantErr: true,
		},
		{
			name: "invalid dimensions",
			config: EmbeddingConfig{
				Provider:   "ollama",
				Dimensions: 0,
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.config.Validate()
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestRAGVectorConfig_Validate(t *testing.T) {
	tests := []struct {
		name    string
		config  RAGVectorConfig
		wantErr bool
	}{
		{
			name: "valid",
			config: RAGVectorConfig{
				NumCentroids: 100,
				Dimensions:   384,
				NProbe:       10,
			},
			wantErr: false,
		},
		{
			name: "invalid num centroids",
			config: RAGVectorConfig{
				NumCentroids: 0,
				Dimensions:   384,
			},
			wantErr: true,
		},
		{
			name: "invalid dimensions",
			config: RAGVectorConfig{
				NumCentroids: 100,
				Dimensions:   0,
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.config.Validate()
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestRAGQueryConfig_Validate(t *testing.T) {
	tests := []struct {
		name    string
		config  RAGQueryConfig
		wantErr bool
	}{
		{
			name: "valid",
			config: RAGQueryConfig{
				CacheSize: 1000,
				CacheTTL:  time.Hour,
			},
			wantErr: false,
		},
		{
			name: "invalid cache size",
			config: RAGQueryConfig{
				CacheSize: -1,
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.config.Validate()
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestPipelineConfig_Validate(t *testing.T) {
	tests := []struct {
		name    string
		config  PipelineConfig
		wantErr bool
	}{
		{
			name: "valid",
			config: PipelineConfig{
				Timeout:              30 * time.Second,
				MaxConcurrentQueries: 100,
				DefaultTopK:          10,
				DefaultMaxTokens:     8000,
				DefaultMinScore:      0.7,
			},
			wantErr: false,
		},
		{
			name: "invalid timeout",
			config: PipelineConfig{
				Timeout: 0,
			},
			wantErr: true,
		},
		{
			name: "invalid max concurrent",
			config: PipelineConfig{
				Timeout:              30 * time.Second,
				MaxConcurrentQueries: 0,
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.config.Validate()
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestRankingConfig_Validate(t *testing.T) {
	tests := []struct {
		name    string
		config  RankingConfig
		wantErr bool
	}{
		{
			name: "valid",
			config: RankingConfig{
				Strategy:      "hybrid",
				BM25K1:        1.2,
				BM25B:         0.75,
				VectorWeight:  0.7,
				KeywordWeight: 0.3,
			},
			wantErr: false,
		},
		{
			name: "invalid strategy",
			config: RankingConfig{
				Strategy: "invalid",
			},
			wantErr: true,
		},
		{
			name: "invalid bm25_k1",
			config: RankingConfig{
				Strategy: "hybrid",
				BM25K1:   0,
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.config.Validate()
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestContextConfig_Validate(t *testing.T) {
	tests := []struct {
		name    string
		config  ContextConfig
		wantErr bool
	}{
		{
			name: "valid",
			config: ContextConfig{
				MaxTokens:          4000,
				DefaultTemplate:    "markdown",
				MaxResultsPerChunk: 20,
				TokenMultiplier:    1.3,
			},
			wantErr: false,
		},
		{
			name: "invalid max tokens",
			config: ContextConfig{
				MaxTokens: 0,
			},
			wantErr: true,
		},
		{
			name: "invalid template",
			config: ContextConfig{
				MaxTokens:       4000,
				DefaultTemplate: "invalid",
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.config.Validate()
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}
