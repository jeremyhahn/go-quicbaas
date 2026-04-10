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
	"time"
)

const (
	// DefaultLMStudioURL is the default LMStudio API endpoint.
	DefaultLMStudioURL = "http://localhost:1234"

	// DefaultLMStudioTimeout is the default HTTP timeout for LMStudio requests.
	DefaultLMStudioTimeout = 60 * time.Second

	// DefaultLMStudioMaxRetries is the default number of retry attempts.
	DefaultLMStudioMaxRetries = 3

	// DefaultLMStudioRetryBackoffMS is the default backoff in milliseconds.
	DefaultLMStudioRetryBackoffMS = 1000
)

// lmstudioModelSpec defines specifications for known LMStudio embedding models.
type lmstudioModelSpec struct {
	dimensions int
	maxTokens  int
}

// lmstudioModels maps known LMStudio model names to their specifications.
var lmstudioModels = map[string]lmstudioModelSpec{
	// Common LMStudio embedding models
	"nomic-ai/nomic-embed-text-v1.5-GGUF":          {dimensions: 768, maxTokens: 8192},
	"nomic-embed-text-v1.5-GGUF":                   {dimensions: 768, maxTokens: 8192},
	"BAAI/bge-small-en-v1.5-GGUF":                  {dimensions: 384, maxTokens: 8192},
	"BAAI/bge-base-en-v1.5-GGUF":                   {dimensions: 768, maxTokens: 8192},
	"BAAI/bge-large-en-v1.5-GGUF":                  {dimensions: 1024, maxTokens: 8192},
	"sentence-transformers/all-MiniLM-L6-v2-GGUF":  {dimensions: 384, maxTokens: 8192},
	"sentence-transformers/all-MiniLM-L12-v2-GGUF": {dimensions: 384, maxTokens: 8192},
	"Xenova/all-MiniLM-L6-v2-onnx":                 {dimensions: 384, maxTokens: 8192},
	"mixedbread-ai/mxbai-embed-large-v1-GGUF":      {dimensions: 1024, maxTokens: 8192},
	"intfloat/e5-small-v2-GGUF":                    {dimensions: 384, maxTokens: 8192},
	"intfloat/e5-base-v2-GGUF":                     {dimensions: 768, maxTokens: 8192},
	"intfloat/e5-large-v2-GGUF":                    {dimensions: 1024, maxTokens: 8192},
	"Snowflake/snowflake-arctic-embed-m-v1.5-GGUF": {dimensions: 768, maxTokens: 8192},
	"Snowflake/snowflake-arctic-embed-l-v2.0-GGUF": {dimensions: 1024, maxTokens: 8192},
}

// LMStudioConfig holds configuration for the LMStudio embedding provider.
type LMStudioConfig struct {
	// BaseURL is the LMStudio API endpoint.
	// Defaults to http://localhost:1234
	BaseURL string

	// Model is the embedding model name loaded in LMStudio.
	Model string

	// Dimensions is the expected embedding dimensions.
	// If 0, will auto-detect from known models or first response.
	Dimensions int

	// Timeout is the HTTP request timeout in seconds.
	Timeout int

	// MaxRetries is the maximum number of retry attempts.
	MaxRetries int

	// RetryBackoffMS is the base backoff duration in milliseconds.
	RetryBackoffMS int
}

// DefaultLMStudioConfig returns the default LMStudio configuration.
func DefaultLMStudioConfig() LMStudioConfig {
	return LMStudioConfig{
		BaseURL:        DefaultLMStudioURL,
		Model:          "nomic-ai/nomic-embed-text-v1.5-GGUF",
		Dimensions:     0, // Auto-detect
		Timeout:        int(DefaultLMStudioTimeout.Seconds()),
		MaxRetries:     DefaultLMStudioMaxRetries,
		RetryBackoffMS: DefaultLMStudioRetryBackoffMS,
	}
}

// LMStudioProvider implements the Provider interface for LMStudio.
// LMStudio exposes an OpenAI-compatible API, so this is a thin wrapper
// around OpenAIProvider with LMStudio-specific defaults.
type LMStudioProvider struct {
	openai *OpenAIProvider
	model  string
}

// NewLMStudioProvider creates a new LMStudio embedding provider.
// LMStudio uses an OpenAI-compatible API, so we delegate to OpenAIProvider.
func NewLMStudioProvider(config LMStudioConfig) (*LMStudioProvider, error) {
	// Apply defaults
	if config.BaseURL == "" {
		config.BaseURL = DefaultLMStudioURL
	}
	if config.Model == "" {
		config.Model = "nomic-ai/nomic-embed-text-v1.5-GGUF"
	}
	if config.Timeout == 0 {
		config.Timeout = int(DefaultLMStudioTimeout.Seconds())
	}
	if config.MaxRetries == 0 {
		config.MaxRetries = DefaultLMStudioMaxRetries
	}
	if config.RetryBackoffMS == 0 {
		config.RetryBackoffMS = DefaultLMStudioRetryBackoffMS
	}

	// Determine dimensions from known LMStudio models
	dimensions := config.Dimensions
	if dimensions == 0 {
		if spec, ok := lmstudioModels[config.Model]; ok {
			dimensions = spec.dimensions
		}
	}

	// Create OpenAI provider configuration
	// LMStudio does not require an API key (empty string is fine)
	openaiConfig := Config{
		Provider:       "openai",
		APIURL:         config.BaseURL,
		APIKey:         "", // LMStudio does not require API key
		Model:          config.Model,
		Dimensions:     dimensions,
		Timeout:        config.Timeout,
		MaxRetries:     config.MaxRetries,
		RetryBackoffMS: config.RetryBackoffMS,
	}

	openai, err := NewOpenAIProvider(openaiConfig)
	if err != nil {
		return nil, &LMStudioInitError{Cause: err}
	}

	return &LMStudioProvider{
		openai: openai,
		model:  config.Model,
	}, nil
}

// Embed generates an embedding for a single text.
func (p *LMStudioProvider) Embed(ctx context.Context, text string) ([]float32, error) {
	embedding, err := p.openai.Embed(ctx, text)
	if err != nil {
		return nil, p.mapError(err)
	}
	return embedding, nil
}

// EmbedBatch generates embeddings for multiple texts in a single request.
func (p *LMStudioProvider) EmbedBatch(ctx context.Context, texts []string) ([][]float32, error) {
	embeddings, err := p.openai.EmbedBatch(ctx, texts)
	if err != nil {
		return nil, p.mapError(err)
	}
	return embeddings, nil
}

// Dimensions returns the dimensionality of embeddings produced by this provider.
func (p *LMStudioProvider) Dimensions() int {
	return p.openai.Dimensions()
}

// MaxTokens returns the maximum number of tokens this provider can handle.
func (p *LMStudioProvider) MaxTokens() int {
	return p.openai.MaxTokens()
}

// Name returns the name of this provider.
func (p *LMStudioProvider) Name() string {
	return "lmstudio"
}

// Close closes the provider and releases resources.
func (p *LMStudioProvider) Close() error {
	return p.openai.Close()
}

// Model returns the configured embedding model.
func (p *LMStudioProvider) Model() string {
	return p.model
}

// BaseURL returns the configured base URL.
func (p *LMStudioProvider) BaseURL() string {
	return p.openai.apiURL
}

// mapError maps OpenAI errors to LMStudio-specific errors for better context.
func (p *LMStudioProvider) mapError(err error) error {
	if err == nil {
		return nil
	}

	// Check for context errors using errors.Is for wrapped errors
	if errors.Is(err, context.Canceled) {
		return &ContextCanceledError{Cause: err}
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return &TimeoutError{Cause: err}
	}

	// The underlying OpenAI provider already wraps errors appropriately,
	// but we can add LMStudio context if needed
	return &LMStudioError{Message: err.Error(), Cause: err}
}

// LMStudioInitError indicates an error during LMStudio provider initialization.
type LMStudioInitError struct {
	Cause error
}

func (e *LMStudioInitError) Error() string {
	if e.Cause != nil {
		return "failed to initialize LMStudio provider: " + e.Cause.Error()
	}
	return "failed to initialize LMStudio provider"
}

func (e *LMStudioInitError) Unwrap() error {
	return e.Cause
}

// LMStudioError indicates an error from LMStudio operations.
type LMStudioError struct {
	Message string
	Cause   error
}

func (e *LMStudioError) Error() string {
	if e.Cause != nil && e.Message != e.Cause.Error() {
		return "lmstudio: " + e.Message
	}
	return "lmstudio: " + e.Message
}

func (e *LMStudioError) Unwrap() error {
	return e.Cause
}
