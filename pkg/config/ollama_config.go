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
	"net/url"
	"time"

	"github.com/jeremyhahn/go-quicbaas/pkg/api"
)

// OllamaConfig holds Ollama integration configuration.
// QuicBaaS uses Ollama as an external service for embedding generation.
// Container orchestration and attestation are handled by go-trusted-platform.
type OllamaConfig struct {
	// Enabled enables Ollama integration.
	// When false, Ollama-dependent features will return errors.
	Enabled bool `mapstructure:"enabled" yaml:"enabled" json:"enabled"`

	// BaseURL is the Ollama API endpoint.
	// go-trusted-platform can configure this to point to an attested container.
	// Default: "http://localhost:11434"
	BaseURL string `mapstructure:"base_url" yaml:"base_url" json:"base_url"`

	// DefaultEmbeddingModel is the model used for embedding operations
	// when no specific model is provided.
	// Default: "nomic-embed-text"
	DefaultEmbeddingModel string `mapstructure:"default_embedding_model" yaml:"default_embedding_model" json:"default_embedding_model"`

	// Timeout is the timeout for Ollama API requests.
	// Embedding generation can be slow for large batches.
	// Default: 120s
	Timeout time.Duration `mapstructure:"timeout" yaml:"timeout" json:"timeout"`

	// MaxRetries is the maximum number of retry attempts for failed requests.
	// Retries use exponential backoff.
	// Default: 3
	MaxRetries int `mapstructure:"max_retries" yaml:"max_retries" json:"max_retries"`

	// RetryBackoff is the initial backoff duration between retries.
	// Each retry doubles the backoff duration.
	// Default: 1s
	RetryBackoff time.Duration `mapstructure:"retry_backoff" yaml:"retry_backoff" json:"retry_backoff"`

	// HealthCheckInterval is how often to check Ollama availability.
	// Set to 0 to disable health checking.
	// Default: 30s
	HealthCheckInterval time.Duration `mapstructure:"health_check_interval" yaml:"health_check_interval" json:"health_check_interval"`
}

// DefaultOllamaConfig returns the default Ollama configuration.
func DefaultOllamaConfig() OllamaConfig {
	return OllamaConfig{
		Enabled:               false,
		BaseURL:               "http://localhost:11434",
		DefaultEmbeddingModel: "nomic-embed-text",
		Timeout:               120 * time.Second,
		MaxRetries:            3,
		RetryBackoff:          time.Second,
		HealthCheckInterval:   30 * time.Second,
	}
}

// Validate validates the Ollama configuration.
func (c *OllamaConfig) Validate() error {
	if !c.Enabled {
		return nil
	}

	if c.BaseURL == "" {
		return api.NewEmptyWhenEnabledError("config.OllamaConfig.Validate", "ollama base_url", "ollama")
	}

	// Validate URL format
	parsedURL, err := url.Parse(c.BaseURL)
	if err != nil {
		return api.NewInvalidURLError("config.OllamaConfig.Validate", "ollama base_url", err)
	}

	if parsedURL.Scheme != "http" && parsedURL.Scheme != "https" {
		return api.NewURLSchemeError("config.OllamaConfig.Validate", "ollama base_url", []string{"http", "https"})
	}

	if parsedURL.Host == "" {
		return api.NewURLMissingHostError("config.OllamaConfig.Validate", "ollama base_url")
	}

	if c.DefaultEmbeddingModel == "" {
		return api.NewEmptyWhenEnabledError("config.OllamaConfig.Validate", "ollama default_embedding_model", "ollama")
	}

	if c.Timeout <= 0 {
		return api.NewPositiveValueRequiredError("config.OllamaConfig.Validate", "ollama timeout")
	}

	if c.MaxRetries < 0 {
		return api.NewNonNegativeValueRequiredError("config.OllamaConfig.Validate", "ollama max_retries")
	}

	if c.RetryBackoff < 0 {
		return api.NewNonNegativeValueRequiredError("config.OllamaConfig.Validate", "ollama retry_backoff")
	}

	if c.HealthCheckInterval < 0 {
		return api.NewNonNegativeValueRequiredError("config.OllamaConfig.Validate", "ollama health_check_interval")
	}

	return nil
}

// MergeDefaults merges with default config for missing values.
func (c *OllamaConfig) MergeDefaults() OllamaConfig {
	defaults := DefaultOllamaConfig()

	if c.BaseURL == "" {
		c.BaseURL = defaults.BaseURL
	}

	if c.DefaultEmbeddingModel == "" {
		c.DefaultEmbeddingModel = defaults.DefaultEmbeddingModel
	}

	if c.Timeout == 0 {
		c.Timeout = defaults.Timeout
	}

	if c.MaxRetries == 0 {
		c.MaxRetries = defaults.MaxRetries
	}

	if c.RetryBackoff == 0 {
		c.RetryBackoff = defaults.RetryBackoff
	}

	if c.HealthCheckInterval == 0 {
		c.HealthCheckInterval = defaults.HealthCheckInterval
	}

	return *c
}
