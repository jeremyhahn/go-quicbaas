//go:build vector

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

package embedding

import (
	"context"
	"errors"
)

// Common errors.
var (
	ErrProviderNotFound    = errors.New("embedding provider not found")
	ErrInvalidCredentials  = errors.New("invalid credentials")
	ErrRateLimitExceeded   = errors.New("rate limit exceeded")
	ErrInvalidInput        = errors.New("invalid input")
	ErrDimensionsMismatch  = errors.New("dimensions mismatch")
	ErrTokenLimitExceeded  = errors.New("token limit exceeded")
	ErrModelNotFound       = errors.New("model not found")
	ErrProviderUnavailable = errors.New("provider unavailable")
)

// Provider defines the interface for embedding providers.
type Provider interface {
	// Embed generates an embedding for a single text
	Embed(ctx context.Context, text string) ([]float32, error)

	// EmbedBatch generates embeddings for multiple texts in a single request
	EmbedBatch(ctx context.Context, texts []string) ([][]float32, error)

	// Dimensions returns the dimensionality of embeddings produced by this provider
	Dimensions() int

	// MaxTokens returns the maximum number of tokens this provider can handle
	MaxTokens() int

	// Name returns the name of this provider
	Name() string

	// Close closes the provider and releases resources
	Close() error
}

// Metadata contains information about the embedding request.
type Metadata struct {
	Model      string            // Model name
	Dimensions int               // Embedding dimensions
	Tokens     int               // Number of tokens processed
	Source     string            // Source file or identifier
	Language   string            // Programming language or natural language
	Tags       map[string]string // Custom tags
}

// Result represents an embedding result with metadata.
type Result struct {
	Embedding []float32 // The embedding vector
	Metadata  Metadata  // Associated metadata
}

// BatchResult represents a batch embedding result.
type BatchResult struct {
	Embeddings [][]float32 // The embedding vectors
	Metadata   []Metadata  // Metadata for each embedding
}

// Config holds configuration for embedding providers.
type Config struct {
	// Provider type: "voyage", "ollama", "lmstudio", "openai"
	Provider string `yaml:"provider" json:"provider"`

	// API configuration
	APIURL string `yaml:"api_url" json:"api_url"`
	APIKey string `yaml:"api_key" json:"api_key"`

	// Model configuration
	Model      string `yaml:"model" json:"model"`
	Dimensions int    `yaml:"dimensions" json:"dimensions"`

	// Performance tuning
	MaxBatchSize int `yaml:"max_batch_size" json:"max_batch_size"`
	Timeout      int `yaml:"timeout_seconds" json:"timeout_seconds"`

	// Cache configuration
	CacheEnabled  bool `yaml:"cache_enabled" json:"cache_enabled"`
	CacheSize     int  `yaml:"cache_size" json:"cache_size"`
	CacheTTL      int  `yaml:"cache_ttl_seconds" json:"cache_ttl_seconds"`
	CacheCompress bool `yaml:"cache_compress" json:"cache_compress"`

	// Retry configuration
	MaxRetries     int `yaml:"max_retries" json:"max_retries"`
	RetryBackoffMS int `yaml:"retry_backoff_ms" json:"retry_backoff_ms"`
}

// DefaultConfig returns default configuration.
func DefaultConfig() Config {
	return Config{
		Provider:       "voyage",
		APIURL:         "",
		APIKey:         "",
		Model:          "voyage-code-2",
		Dimensions:     1536,
		MaxBatchSize:   128,
		Timeout:        30,
		CacheEnabled:   true,
		CacheSize:      10000,
		CacheTTL:       3600,
		CacheCompress:  true,
		MaxRetries:     3,
		RetryBackoffMS: 1000,
	}
}

// Validate validates the configuration.
func (c *Config) Validate() error {
	if c.Provider == "" {
		return ErrProviderRequired
	}

	if c.Provider != "voyage" && c.Provider != "ollama" && c.Provider != "lmstudio" && c.Provider != "openai" {
		return ErrProviderInvalid
	}

	if c.APIURL == "" {
		return ErrAPIURLRequired
	}

	if c.Provider == "voyage" && c.APIKey == "" {
		return ErrAPIKeyRequired
	}

	if c.Model == "" {
		return ErrModelRequired
	}

	if c.Dimensions <= 0 {
		return ErrDimensionsPositive
	}

	if c.MaxBatchSize <= 0 {
		return ErrMaxBatchSizePositive
	}

	if c.Timeout <= 0 {
		return ErrTimeoutPositive
	}

	return nil
}
