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
	"errors"
	"time"
)

// Configuration validation errors.
var (
	// ErrInvalidMaxConcurrentQueries is returned when max_concurrent_queries is not positive.
	ErrInvalidMaxConcurrentQueries = errors.New("max_concurrent_queries must be positive")

	// ErrInvalidQueryTimeout is returned when query_timeout is not positive.
	ErrInvalidQueryTimeout = errors.New("query_timeout must be positive")

	// ErrInvalidDefaultTopK is returned when default_top_k is not positive.
	ErrInvalidDefaultTopK = errors.New("default_top_k must be positive")

	// ErrInvalidDefaultMaxTokens is returned when default_max_tokens is not positive.
	ErrInvalidDefaultMaxTokens = errors.New("default_max_tokens must be positive")

	// ErrInvalidDefaultMinScore is returned when default_min_score is out of range.
	ErrInvalidDefaultMinScore = errors.New("default_min_score must be between 0 and 1")
)

// Config holds the complete pipeline configuration.
type Config struct {
	// Pipeline behavior
	EnableCache      bool   `yaml:"enable_cache" json:"enable_cache"`
	EnableHybrid     bool   `yaml:"enable_hybrid" json:"enable_hybrid"`
	EnableRerank     bool   `yaml:"enable_rerank" json:"enable_rerank"`
	DefaultNamespace string `yaml:"default_namespace" json:"default_namespace"`

	// Performance tuning
	MaxConcurrentQueries int           `yaml:"max_concurrent_queries" json:"max_concurrent_queries"`
	QueryTimeout         time.Duration `yaml:"query_timeout" json:"query_timeout"`
	WorkerPoolSize       int           `yaml:"worker_pool_size" json:"worker_pool_size"`

	// Defaults
	DefaultTopK      int     `yaml:"default_top_k" json:"default_top_k"`
	DefaultMaxTokens int     `yaml:"default_max_tokens" json:"default_max_tokens"`
	DefaultMinScore  float32 `yaml:"default_min_score" json:"default_min_score"`

	// Resilience configuration
	EnableCircuitBreaker    bool          `yaml:"enable_circuit_breaker" json:"enable_circuit_breaker"`
	CircuitBreakerThreshold int           `yaml:"circuit_breaker_threshold" json:"circuit_breaker_threshold"`
	CircuitBreakerTimeout   time.Duration `yaml:"circuit_breaker_timeout" json:"circuit_breaker_timeout"`

	// Observability
	EnableMetrics    bool   `yaml:"enable_metrics" json:"enable_metrics"`
	EnableTracing    bool   `yaml:"enable_tracing" json:"enable_tracing"`
	MetricsNamespace string `yaml:"metrics_namespace" json:"metrics_namespace"`
}

// DefaultConfig returns default pipeline configuration.
func DefaultConfig() Config {
	return Config{
		EnableCache:             true,
		EnableHybrid:            true,
		EnableRerank:            true,
		DefaultNamespace:        "default",
		MaxConcurrentQueries:    100,
		QueryTimeout:            30 * time.Second,
		WorkerPoolSize:          10,
		DefaultTopK:             10,
		DefaultMaxTokens:        8000,
		DefaultMinScore:         0.7,
		EnableCircuitBreaker:    true,
		CircuitBreakerThreshold: 5,
		CircuitBreakerTimeout:   60 * time.Second,
		EnableMetrics:           true,
		EnableTracing:           false,
		MetricsNamespace:        "quicbaas_rag",
	}
}

// Validate validates the pipeline configuration.
func (c *Config) Validate() error {
	if c.MaxConcurrentQueries <= 0 {
		return ErrInvalidMaxConcurrentQueries
	}
	if c.QueryTimeout <= 0 {
		return ErrInvalidQueryTimeout
	}
	if c.DefaultTopK <= 0 {
		return ErrInvalidDefaultTopK
	}
	if c.DefaultMaxTokens <= 0 {
		return ErrInvalidDefaultMaxTokens
	}
	if c.DefaultMinScore < 0 || c.DefaultMinScore > 1 {
		return ErrInvalidDefaultMinScore
	}

	return nil
}

// Copy creates a copy of the config.
func (c *Config) Copy() Config {
	return *c
}

// ConfigBuilder provides a builder pattern for configuration.
type ConfigBuilder struct {
	config Config
}

// NewConfigBuilder creates a new config builder with defaults.
func NewConfigBuilder() *ConfigBuilder {
	return &ConfigBuilder{
		config: DefaultConfig(),
	}
}

// WithCache sets cache enablement.
func (b *ConfigBuilder) WithCache(enabled bool) *ConfigBuilder {
	b.config.EnableCache = enabled
	return b
}

// WithHybrid sets hybrid search enablement.
func (b *ConfigBuilder) WithHybrid(enabled bool) *ConfigBuilder {
	b.config.EnableHybrid = enabled
	return b
}

// WithRerank sets reranking enablement.
func (b *ConfigBuilder) WithRerank(enabled bool) *ConfigBuilder {
	b.config.EnableRerank = enabled
	return b
}

// WithNamespace sets default namespace.
func (b *ConfigBuilder) WithNamespace(namespace string) *ConfigBuilder {
	b.config.DefaultNamespace = namespace
	return b
}

// WithMaxConcurrentQueries sets max concurrent queries.
func (b *ConfigBuilder) WithMaxConcurrentQueries(max int) *ConfigBuilder {
	b.config.MaxConcurrentQueries = max
	return b
}

// WithQueryTimeout sets query timeout.
func (b *ConfigBuilder) WithQueryTimeout(timeout time.Duration) *ConfigBuilder {
	b.config.QueryTimeout = timeout
	return b
}

// WithDefaultTopK sets default top K.
func (b *ConfigBuilder) WithDefaultTopK(topK int) *ConfigBuilder {
	b.config.DefaultTopK = topK
	return b
}

// WithDefaultMaxTokens sets default max tokens.
func (b *ConfigBuilder) WithDefaultMaxTokens(maxTokens int) *ConfigBuilder {
	b.config.DefaultMaxTokens = maxTokens
	return b
}

// WithDefaultMinScore sets default min score.
func (b *ConfigBuilder) WithDefaultMinScore(minScore float32) *ConfigBuilder {
	b.config.DefaultMinScore = minScore
	return b
}

// WithCircuitBreaker sets circuit breaker enablement.
func (b *ConfigBuilder) WithCircuitBreaker(enabled bool) *ConfigBuilder {
	b.config.EnableCircuitBreaker = enabled
	return b
}

// WithMetrics sets metrics enablement.
func (b *ConfigBuilder) WithMetrics(enabled bool) *ConfigBuilder {
	b.config.EnableMetrics = enabled
	return b
}

// WithTracing sets tracing enablement.
func (b *ConfigBuilder) WithTracing(enabled bool) *ConfigBuilder {
	b.config.EnableTracing = enabled
	return b
}

// Build returns the configured Config.
func (b *ConfigBuilder) Build() Config {
	return b.config
}
