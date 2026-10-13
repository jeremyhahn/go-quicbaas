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
	"time"

	"github.com/jeremyhahn/go-quicbaas/pkg/api"
)

// RAGConfig holds RAG system configuration.
type RAGConfig struct {
	Enabled   bool   `yaml:"enabled" json:"enabled" mapstructure:"enabled"`
	IndexPath string `yaml:"index_path" json:"index_path" mapstructure:"index_path"`

	Embedding EmbeddingConfig `yaml:"embedding" json:"embedding" mapstructure:"embedding"`
	Vector    RAGVectorConfig `yaml:"vector" json:"vector" mapstructure:"vector"`
	Query     RAGQueryConfig  `yaml:"query" json:"query" mapstructure:"query"`
	Ranking   RankingConfig   `yaml:"ranking" json:"ranking" mapstructure:"ranking"`
	Context   ContextConfig   `yaml:"context" json:"context" mapstructure:"context"`
	Pipeline  PipelineConfig  `yaml:"pipeline" json:"pipeline" mapstructure:"pipeline"`
}

// EmbeddingConfig holds embedding provider configuration.
type EmbeddingConfig struct {
	Provider   string `yaml:"provider" json:"provider" mapstructure:"provider"`       // "ollama", "openai", "voyage", "lmstudio", "npu"
	Model      string `yaml:"model" json:"model" mapstructure:"model"`                // Model name
	APIURL     string `yaml:"api_url" json:"api_url" mapstructure:"api_url"`          // API endpoint URL
	APIKey     string `yaml:"api_key" json:"api_key" mapstructure:"api_key"`          // API key (if required)
	Dimensions int    `yaml:"dimensions" json:"dimensions" mapstructure:"dimensions"` // Embedding dimensions
	Timeout    int    `yaml:"timeout" json:"timeout" mapstructure:"timeout"`          // Timeout in seconds

	// Additional declares extra providers to register alongside the default
	// named by Provider. Each is registered under its own name and can then be
	// selected per request or promoted to the service default at runtime.
	//
	// This exists because go-qrdb validates the *configured default* against a
	// closed allowlist of its four built-in providers, so a provider it does not
	// know -- "npu", served by the OpenVINO sidecar -- cannot be named as
	// Provider at all. Listing it here is how it gets registered and becomes
	// usable. See docs/embeddings/npu.md and go-qrdb#528.
	//
	// Entries are flat: an Additional entry of its own is rejected by Validate.
	Additional []EmbeddingConfig `yaml:"additional" json:"additional,omitempty" mapstructure:"additional"`
}

// RAGVectorConfig holds vector index configuration for RAG.
type RAGVectorConfig struct {
	NumCentroids int    `yaml:"num_centroids" json:"num_centroids" mapstructure:"num_centroids"` // Number of IVF clusters
	Dimensions   int    `yaml:"dimensions" json:"dimensions" mapstructure:"dimensions"`          // Vector dimensions
	NProbe       int    `yaml:"nprobe" json:"nprobe" mapstructure:"nprobe"`                      // Number of clusters to search
	Metric       string `yaml:"metric" json:"metric" mapstructure:"metric"`                      // Distance metric: "cosine", "l2", "ip"
}

// RAGQueryConfig holds query handler configuration for RAG.
type RAGQueryConfig struct {
	CacheSize       int           `yaml:"cache_size" json:"cache_size" mapstructure:"cache_size"`                      // LRU cache size
	CacheTTL        time.Duration `yaml:"cache_ttl" json:"cache_ttl" mapstructure:"cache_ttl"`                         // Cache TTL
	CleanupInterval time.Duration `yaml:"cleanup_interval" json:"cleanup_interval" mapstructure:"cleanup_interval"`    // Cache cleanup interval
	DefaultTopK     int           `yaml:"default_top_k" json:"default_top_k" mapstructure:"default_top_k"`             // Default top-K results
	DefaultMinScore float32       `yaml:"default_min_score" json:"default_min_score" mapstructure:"default_min_score"` // Default min score
	DefaultNProbe   int           `yaml:"default_nprobe" json:"default_nprobe" mapstructure:"default_nprobe"`          // Default nprobe
}

// RankingConfig holds ranking system configuration.
type RankingConfig struct {
	Strategy      string  `yaml:"strategy" json:"strategy" mapstructure:"strategy"`                   // "bm25", "fusion", "hybrid"
	BM25K1        float32 `yaml:"bm25_k1" json:"bm25_k1" mapstructure:"bm25_k1"`                      // BM25 k1 parameter
	BM25B         float32 `yaml:"bm25_b" json:"bm25_b" mapstructure:"bm25_b"`                         // BM25 b parameter
	RRFK          int     `yaml:"rrf_k" json:"rrf_k" mapstructure:"rrf_k"`                            // RRF k parameter
	EnableRerank  bool    `yaml:"enable_rerank" json:"enable_rerank" mapstructure:"enable_rerank"`    // Enable cross-encoder reranking
	RerankTopK    int     `yaml:"rerank_top_k" json:"rerank_top_k" mapstructure:"rerank_top_k"`       // Top-K for reranking
	VectorWeight  float32 `yaml:"vector_weight" json:"vector_weight" mapstructure:"vector_weight"`    // Hybrid vector weight
	KeywordWeight float32 `yaml:"keyword_weight" json:"keyword_weight" mapstructure:"keyword_weight"` // Hybrid keyword weight
}

// ContextConfig holds context builder configuration.
type ContextConfig struct {
	MaxTokens          int     `yaml:"max_tokens" json:"max_tokens" mapstructure:"max_tokens"`                                  // Max context tokens
	DefaultTemplate    string  `yaml:"default_template" json:"default_template" mapstructure:"default_template"`                // Default template: "markdown", "json", "xml", "plain"
	IncludeCitations   bool    `yaml:"include_citations" json:"include_citations" mapstructure:"include_citations"`             // Include source citations
	TruncationStrategy string  `yaml:"truncation_strategy" json:"truncation_strategy" mapstructure:"truncation_strategy"`       // "smart", "hard", "none"
	MaxResultsPerChunk int     `yaml:"max_results_per_chunk" json:"max_results_per_chunk" mapstructure:"max_results_per_chunk"` // Max results per context
	TokenMultiplier    float32 `yaml:"token_multiplier" json:"token_multiplier" mapstructure:"token_multiplier"`                // Token estimation multiplier
}

// PipelineConfig holds pipeline orchestrator configuration.
type PipelineConfig struct {
	Timeout                 time.Duration `yaml:"timeout" json:"timeout" mapstructure:"timeout"`                                                       // Query timeout
	MaxConcurrentQueries    int           `yaml:"max_concurrent_queries" json:"max_concurrent_queries" mapstructure:"max_concurrent_queries"`          // Max concurrent queries
	EnableCircuitBreaker    bool          `yaml:"enable_circuit_breaker" json:"enable_circuit_breaker" mapstructure:"enable_circuit_breaker"`          // Enable circuit breaker
	CircuitBreakerThreshold int           `yaml:"circuit_breaker_threshold" json:"circuit_breaker_threshold" mapstructure:"circuit_breaker_threshold"` // Circuit breaker failure threshold
	CircuitBreakerTimeout   time.Duration `yaml:"circuit_breaker_timeout" json:"circuit_breaker_timeout" mapstructure:"circuit_breaker_timeout"`       // Circuit breaker timeout
	EnableMetrics           bool          `yaml:"enable_metrics" json:"enable_metrics" mapstructure:"enable_metrics"`                                  // Enable metrics
	EnableTracing           bool          `yaml:"enable_tracing" json:"enable_tracing" mapstructure:"enable_tracing"`                                  // Enable tracing
	MetricsNamespace        string        `yaml:"metrics_namespace" json:"metrics_namespace" mapstructure:"metrics_namespace"`                         // Metrics namespace
	DefaultTopK             int           `yaml:"default_top_k" json:"default_top_k" mapstructure:"default_top_k"`                                     // Default top-K
	DefaultMaxTokens        int           `yaml:"default_max_tokens" json:"default_max_tokens" mapstructure:"default_max_tokens"`                      // Default max tokens
	DefaultMinScore         float32       `yaml:"default_min_score" json:"default_min_score" mapstructure:"default_min_score"`                         // Default min score
	DefaultNamespace        string        `yaml:"default_namespace" json:"default_namespace" mapstructure:"default_namespace"`                         // Default namespace
	EnableCache             bool          `yaml:"enable_cache" json:"enable_cache" mapstructure:"enable_cache"`                                        // Enable query cache
	EnableHybrid            bool          `yaml:"enable_hybrid" json:"enable_hybrid" mapstructure:"enable_hybrid"`                                     // Enable hybrid search
	EnableRerank            bool          `yaml:"enable_rerank" json:"enable_rerank" mapstructure:"enable_rerank"`                                     // Enable reranking
}

// DefaultRAGConfig returns default RAG configuration.
func DefaultRAGConfig() RAGConfig {
	return RAGConfig{
		Enabled:   false,
		IndexPath: "./data/rag_index",
		Embedding: EmbeddingConfig{
			Provider:   "ollama",
			Model:      "all-minilm",
			APIURL:     DefaultOllamaBaseURL,
			Dimensions: 384,
			Timeout:    30,
		},
		Vector: RAGVectorConfig{
			NumCentroids: 100,
			Dimensions:   384,
			NProbe:       10,
			Metric:       "cosine",
		},
		Query: RAGQueryConfig{
			CacheSize:       1000,
			CacheTTL:        time.Hour,
			CleanupInterval: 5 * time.Minute,
			DefaultTopK:     10,
			DefaultMinScore: 0.7,
			DefaultNProbe:   10,
		},
		Ranking: RankingConfig{
			Strategy:      "hybrid",
			BM25K1:        1.2,
			BM25B:         0.75,
			RRFK:          60,
			EnableRerank:  false,
			RerankTopK:    50,
			VectorWeight:  0.7,
			KeywordWeight: 0.3,
		},
		Context: ContextConfig{
			MaxTokens:          4000,
			DefaultTemplate:    "markdown",
			IncludeCitations:   true,
			TruncationStrategy: "smart",
			MaxResultsPerChunk: 20,
			TokenMultiplier:    1.3,
		},
		Pipeline: PipelineConfig{
			Timeout:                 30 * time.Second,
			MaxConcurrentQueries:    100,
			EnableCircuitBreaker:    true,
			CircuitBreakerThreshold: 5,
			CircuitBreakerTimeout:   60 * time.Second,
			EnableMetrics:           true,
			EnableTracing:           false,
			MetricsNamespace:        "quicbaas_rag",
			DefaultTopK:             10,
			DefaultMaxTokens:        8000,
			DefaultMinScore:         0.7,
			DefaultNamespace:        "default",
			EnableCache:             true,
			EnableHybrid:            true,
			EnableRerank:            false,
		},
	}
}

// Validate validates the RAG configuration.
func (c *RAGConfig) Validate() error {
	if !c.Enabled {
		return nil
	}

	// Validate embedding config
	if err := c.Embedding.Validate(); err != nil {
		return api.NewConfigSectionValidationError("config.RAGConfig.Validate", "embedding", err)
	}

	// Validate vector config
	if err := c.Vector.Validate(); err != nil {
		return api.NewConfigSectionValidationError("config.RAGConfig.Validate", "vector", err)
	}

	// Validate dimension consistency
	if c.Embedding.Dimensions != c.Vector.Dimensions {
		return api.NewDimensionMismatchError("config.RAGConfig.Validate",
			"embedding", c.Embedding.Dimensions, "vector", c.Vector.Dimensions)
	}

	// Validate query config
	if err := c.Query.Validate(); err != nil {
		return api.NewConfigSectionValidationError("config.RAGConfig.Validate", "query", err)
	}

	// Validate ranking config
	if err := c.Ranking.Validate(); err != nil {
		return api.NewConfigSectionValidationError("config.RAGConfig.Validate", "ranking", err)
	}

	// Validate context config
	if err := c.Context.Validate(); err != nil {
		return api.NewConfigSectionValidationError("config.RAGConfig.Validate", "context", err)
	}

	// Validate pipeline config
	if err := c.Pipeline.Validate(); err != nil {
		return api.NewConfigSectionValidationError("config.RAGConfig.Validate", "pipeline", err)
	}

	return nil
}

// MergeDefaults merges with default config for missing values.
func (c *RAGConfig) MergeDefaults() RAGConfig {
	defaults := DefaultRAGConfig()

	if c.IndexPath == "" {
		c.IndexPath = defaults.IndexPath
	}

	if c.Embedding.Provider == "" {
		c.Embedding = defaults.Embedding
	}

	if c.Vector.NumCentroids == 0 {
		c.Vector = defaults.Vector
	}

	if c.Query.CacheSize == 0 {
		c.Query = defaults.Query
	}

	if c.Ranking.Strategy == "" {
		c.Ranking = defaults.Ranking
	}

	if c.Context.MaxTokens == 0 {
		c.Context = defaults.Context
	}

	if c.Pipeline.Timeout == 0 {
		c.Pipeline = defaults.Pipeline
	}

	return *c
}

// Validate validates embedding configuration.
func (c *EmbeddingConfig) Validate() error {
	if c.Provider == "" {
		return api.NewRequiredFieldError("embedding provider")
	}

	if c.Dimensions <= 0 {
		return api.NewPositiveValueRequiredError("config.EmbeddingConfig.Validate", "embedding dimensions")
	}

	// Validate provider-specific requirements
	switch c.Provider {
	case "openai", "voyage":
		if c.APIKey == "" {
			return api.NewProviderRequirementError("config.EmbeddingConfig.Validate", c.Provider, "API key")
		}
	case "ollama", "lmstudio":
		if c.APIURL == "" {
			return api.NewProviderRequirementError("config.EmbeddingConfig.Validate", c.Provider, "API URL")
		}
	default:
		// Allow custom providers
	}

	// Validate the extra providers, one level deep. Nesting is rejected rather
	// than silently ignored, so a mis-indented config fails loudly.
	for i := range c.Additional {
		extra := &c.Additional[i]
		if len(extra.Additional) > 0 {
			return api.NewValidationError(
				"config.EmbeddingConfig.Validate",
				"embedding.additional entries must not themselves declare additional providers",
			)
		}
		if err := extra.Validate(); err != nil {
			return err
		}
	}

	return nil
}

// Validate validates vector configuration.
func (c *RAGVectorConfig) Validate() error {
	if c.NumCentroids <= 0 {
		return api.NewPositiveValueRequiredError("config.RAGVectorConfig.Validate", "num_centroids")
	}

	if c.Dimensions <= 0 {
		return api.NewPositiveValueRequiredError("config.RAGVectorConfig.Validate", "vector dimensions")
	}

	if c.NProbe <= 0 {
		c.NProbe = 10 // Default
	}

	if c.NProbe > c.NumCentroids {
		return api.NewValueExceedsLimitError("config.RAGVectorConfig.Validate",
			"nprobe", c.NProbe, "num_centroids", c.NumCentroids)
	}

	return nil
}

// Validate validates query configuration.
func (c *RAGQueryConfig) Validate() error {
	if c.CacheSize < 0 {
		return api.NewNonNegativeValueRequiredError("config.RAGQueryConfig.Validate", "cache size")
	}

	if c.DefaultTopK < 0 {
		return api.NewNonNegativeValueRequiredError("config.RAGQueryConfig.Validate", "default_top_k")
	}

	if c.DefaultMinScore < 0 || c.DefaultMinScore > 1 {
		return api.NewFloatRangeError("config.RAGQueryConfig.Validate", "default_min_score", 0, 1)
	}

	return nil
}

// Validate validates ranking configuration.
func (c *RankingConfig) Validate() error {
	if c.Strategy == "" {
		return api.NewRequiredFieldError("ranking strategy")
	}

	validStrategies := map[string]bool{
		"bm25":   true,
		"fusion": true,
		"hybrid": true,
		"vector": true,
	}

	if !validStrategies[c.Strategy] {
		return api.NewInvalidInputError("invalid ranking strategy: " + c.Strategy)
	}

	if c.BM25K1 <= 0 {
		return api.NewPositiveValueRequiredError("config.RankingConfig.Validate", "bm25_k1")
	}

	if c.BM25B < 0 || c.BM25B > 1 {
		return api.NewFloatRangeError("config.RankingConfig.Validate", "bm25_b", 0, 1)
	}

	if c.VectorWeight < 0 || c.VectorWeight > 1 {
		return api.NewFloatRangeError("config.RankingConfig.Validate", "vector_weight", 0, 1)
	}

	if c.KeywordWeight < 0 || c.KeywordWeight > 1 {
		return api.NewFloatRangeError("config.RankingConfig.Validate", "keyword_weight", 0, 1)
	}

	return nil
}

// Validate validates context configuration.
func (c *ContextConfig) Validate() error {
	if c.MaxTokens <= 0 {
		return api.NewPositiveValueRequiredError("config.ContextConfig.Validate", "max_tokens")
	}

	if c.DefaultTemplate == "" {
		return api.NewRequiredFieldError("default_template")
	}

	validTemplates := map[string]bool{
		"markdown": true,
		"json":     true,
		"xml":      true,
		"plain":    true,
	}

	if !validTemplates[c.DefaultTemplate] {
		return api.NewInvalidInputError("invalid template: " + c.DefaultTemplate)
	}

	if c.MaxResultsPerChunk <= 0 {
		return api.NewPositiveValueRequiredError("config.ContextConfig.Validate", "max_results_per_chunk")
	}

	if c.TokenMultiplier <= 0 {
		return api.NewPositiveValueRequiredError("config.ContextConfig.Validate", "token_multiplier")
	}

	return nil
}

// Validate validates pipeline configuration.
func (c *PipelineConfig) Validate() error {
	if c.Timeout <= 0 {
		return api.NewPositiveValueRequiredError("config.PipelineConfig.Validate", "timeout")
	}

	if c.MaxConcurrentQueries <= 0 {
		return api.NewPositiveValueRequiredError("config.PipelineConfig.Validate", "max_concurrent_queries")
	}

	if c.DefaultTopK < 0 {
		return api.NewNonNegativeValueRequiredError("config.PipelineConfig.Validate", "default_top_k")
	}

	if c.DefaultMaxTokens <= 0 {
		return api.NewPositiveValueRequiredError("config.PipelineConfig.Validate", "default_max_tokens")
	}

	if c.DefaultMinScore < 0 || c.DefaultMinScore > 1 {
		return api.NewFloatRangeError("config.PipelineConfig.Validate", "default_min_score", 0, 1)
	}

	return nil
}
