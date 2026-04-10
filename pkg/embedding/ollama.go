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
	"sync"
	"time"

	"github.com/jeremyhahn/go-quicbaas/pkg/ollama"
)

const (
	// DefaultOllamaURL is the default Ollama API endpoint.
	DefaultOllamaURL = "http://localhost:11434"

	// DefaultOllamaTimeout is the default HTTP timeout for Ollama requests.
	DefaultOllamaTimeout = 120 * time.Second

	// DefaultOllamaMaxRetries is the default number of retry attempts.
	DefaultOllamaMaxRetries = 3

	// DefaultOllamaRetryBackoff is the default backoff duration between retries.
	DefaultOllamaRetryBackoff = 1 * time.Second
)

// ollamaModelSpec defines specifications for known Ollama embedding models.
type ollamaModelSpec struct {
	dimensions int
	maxTokens  int
}

// ollamaModels maps known Ollama model names to their specifications.
var ollamaModels = map[string]ollamaModelSpec{
	// Popular embedding models
	"nomic-embed-text":       {dimensions: 768, maxTokens: 8192},
	"mxbai-embed-large":      {dimensions: 1024, maxTokens: 8192},
	"all-minilm":             {dimensions: 384, maxTokens: 8192},
	"all-minilm-l6-v2":       {dimensions: 384, maxTokens: 8192},
	"all-minilm-l12-v2":      {dimensions: 384, maxTokens: 8192},
	"bge-small-en-v1.5":      {dimensions: 384, maxTokens: 8192},
	"bge-base-en-v1.5":       {dimensions: 768, maxTokens: 8192},
	"bge-large-en-v1.5":      {dimensions: 1024, maxTokens: 8192},
	"bge-m3":                 {dimensions: 1024, maxTokens: 8192},
	"snowflake-arctic-embed": {dimensions: 1024, maxTokens: 8192},
	"e5-small-v2":            {dimensions: 384, maxTokens: 8192},
	"e5-base-v2":             {dimensions: 768, maxTokens: 8192},
	"e5-large-v2":            {dimensions: 1024, maxTokens: 8192},
}

// OllamaConfig holds configuration for the Ollama embedding provider.
type OllamaConfig struct {
	// BaseURL is the Ollama API endpoint.
	BaseURL string

	// Model is the embedding model name.
	Model string

	// Dimensions is the expected embedding dimensions.
	// If 0, will auto-detect from known models or first response.
	Dimensions int

	// Timeout is the HTTP request timeout.
	Timeout time.Duration

	// MaxRetries is the maximum number of retry attempts.
	MaxRetries int

	// RetryBackoff is the base backoff duration between retries.
	RetryBackoff time.Duration
}

// DefaultOllamaConfig returns the default Ollama configuration.
func DefaultOllamaConfig() OllamaConfig {
	return OllamaConfig{
		BaseURL:      DefaultOllamaURL,
		Model:        "nomic-embed-text",
		Dimensions:   0, // Auto-detect
		Timeout:      DefaultOllamaTimeout,
		MaxRetries:   DefaultOllamaMaxRetries,
		RetryBackoff: DefaultOllamaRetryBackoff,
	}
}

// OllamaProvider implements the Provider interface using the native Ollama API.
// It wraps the pkg/ollama package for embedding generation.
type OllamaProvider struct {
	client           *ollama.Client
	embeddingService *ollama.EmbeddingService
	model            string
	dimensions       int
	maxTokens        int
	mu               sync.RWMutex
}

// NewOllamaProvider creates a new Ollama embedding provider.
func NewOllamaProvider(config OllamaConfig) (*OllamaProvider, error) {
	// Apply defaults
	if config.BaseURL == "" {
		config.BaseURL = DefaultOllamaURL
	}
	if config.Model == "" {
		config.Model = "nomic-embed-text"
	}
	if config.Timeout == 0 {
		config.Timeout = DefaultOllamaTimeout
	}
	if config.MaxRetries == 0 {
		config.MaxRetries = DefaultOllamaMaxRetries
	}
	if config.RetryBackoff == 0 {
		config.RetryBackoff = DefaultOllamaRetryBackoff
	}

	// Determine dimensions and max tokens from known models
	dimensions := config.Dimensions
	maxTokens := 8192 // Default max tokens

	if spec, ok := ollamaModels[config.Model]; ok {
		if dimensions == 0 {
			dimensions = spec.dimensions
		}
		maxTokens = spec.maxTokens
	}

	// Create Ollama client
	clientConfig := ollama.ClientConfig{
		BaseURL:      config.BaseURL,
		Timeout:      config.Timeout,
		MaxRetries:   config.MaxRetries,
		RetryBackoff: config.RetryBackoff,
	}
	client := ollama.NewClient(clientConfig)

	// Create embedding service
	embeddingService := ollama.NewEmbeddingService(client, config.Model)

	return &OllamaProvider{
		client:           client,
		embeddingService: embeddingService,
		model:            config.Model,
		dimensions:       dimensions,
		maxTokens:        maxTokens,
	}, nil
}

// Embed generates an embedding for a single text.
func (p *OllamaProvider) Embed(ctx context.Context, text string) ([]float32, error) {
	if text == "" {
		return nil, &InvalidInputError{Message: "text cannot be empty"}
	}

	embedding, err := p.embeddingService.Embed(ctx, text)
	if err != nil {
		return nil, p.mapError(err)
	}

	if len(embedding) == 0 {
		return nil, &EmptyResponseError{Message: "no embedding returned from Ollama"}
	}

	// Auto-detect and validate dimensions
	if err := p.validateDimensions(len(embedding)); err != nil {
		return nil, err
	}

	return embedding, nil
}

// EmbedBatch generates embeddings for multiple texts.
func (p *OllamaProvider) EmbedBatch(ctx context.Context, texts []string) ([][]float32, error) {
	if len(texts) == 0 {
		return nil, &InvalidInputError{Message: "texts cannot be empty"}
	}

	// Validate all texts
	for i, text := range texts {
		if text == "" {
			return nil, &InvalidInputError{Message: "text at index " + itoa(i) + " is empty"}
		}
	}

	embeddings, err := p.embeddingService.EmbedBatch(ctx, texts)
	if err != nil {
		return nil, p.mapError(err)
	}

	if len(embeddings) == 0 {
		return nil, &EmptyResponseError{Message: "no embeddings returned from Ollama"}
	}

	// Validate dimensions for all embeddings
	for i, embedding := range embeddings {
		if len(embedding) == 0 {
			return nil, &EmptyResponseError{Message: "empty embedding at index " + itoa(i)}
		}
		if err := p.validateDimensions(len(embedding)); err != nil {
			return nil, err
		}
	}

	return embeddings, nil
}

// Dimensions returns the dimensionality of embeddings produced by this provider.
func (p *OllamaProvider) Dimensions() int {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.dimensions
}

// MaxTokens returns the maximum number of tokens this provider can handle.
func (p *OllamaProvider) MaxTokens() int {
	return p.maxTokens
}

// Name returns the name of this provider.
func (p *OllamaProvider) Name() string {
	return "ollama"
}

// Close closes the provider and releases resources.
func (p *OllamaProvider) Close() error {
	return p.client.Close()
}

// Model returns the configured embedding model.
func (p *OllamaProvider) Model() string {
	return p.model
}

// BaseURL returns the configured base URL.
func (p *OllamaProvider) BaseURL() string {
	return p.client.BaseURL()
}

// validateDimensions validates and auto-detects embedding dimensions.
func (p *OllamaProvider) validateDimensions(received int) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	// Auto-detect dimensions on first embedding
	if p.dimensions == 0 {
		p.dimensions = received
		return nil
	}

	// Validate subsequent embeddings match
	if p.dimensions != received {
		return &DimensionsMismatchError{
			Expected: p.dimensions,
			Received: received,
		}
	}

	return nil
}

// mapError maps Ollama errors to embedding provider errors.
func (p *OllamaProvider) mapError(err error) error {
	if err == nil {
		return nil
	}

	// Check for context errors first using errors.Is for wrapped errors
	if errors.Is(err, context.Canceled) {
		return &ContextCanceledError{Cause: err}
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return &TimeoutError{Cause: err}
	}

	// Map Ollama-specific errors using errors.Is for proper wrapped error handling
	switch {
	case errors.Is(err, ollama.ErrOllamaUnavailable):
		return &ProviderUnavailableError{Provider: "ollama", Cause: err}
	case errors.Is(err, ollama.ErrModelNotFound):
		return &ModelNotFoundError{Model: p.model, Cause: err}
	case errors.Is(err, ollama.ErrInvalidInput):
		return &InvalidInputError{Message: err.Error()}
	case errors.Is(err, ollama.ErrEmptyResponse):
		return &EmptyResponseError{Message: err.Error()}
	case errors.Is(err, ollama.ErrTimeout):
		return &TimeoutError{Cause: err}
	case errors.Is(err, ollama.ErrContextCanceled):
		return &ContextCanceledError{Cause: err}
	default:
		return &ProviderError{Provider: "ollama", Message: err.Error()}
	}
}

// itoa converts an integer to a string without using strconv.
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	if n < 0 {
		return "-" + itoa(-n)
	}
	result := ""
	for n > 0 {
		result = string(rune('0'+n%10)) + result
		n /= 10
	}
	return result
}

// Typed errors for OllamaProvider.

// InvalidInputError indicates invalid input was provided.
type InvalidInputError struct {
	Message string
}

func (e *InvalidInputError) Error() string {
	return "invalid input: " + e.Message
}

// EmptyResponseError indicates an empty response was received.
type EmptyResponseError struct {
	Message string
}

func (e *EmptyResponseError) Error() string {
	return "empty response: " + e.Message
}

// DimensionsMismatchError indicates a mismatch in embedding dimensions.
type DimensionsMismatchError struct {
	Expected int
	Received int
}

func (e *DimensionsMismatchError) Error() string {
	return "dimensions mismatch: expected " + itoa(e.Expected) + ", got " + itoa(e.Received)
}

// ProviderUnavailableError indicates the provider is not reachable.
type ProviderUnavailableError struct {
	Provider string
	Cause    error
}

func (e *ProviderUnavailableError) Error() string {
	if e.Cause != nil {
		return e.Provider + " provider unavailable: " + e.Cause.Error()
	}
	return e.Provider + " provider unavailable"
}

func (e *ProviderUnavailableError) Unwrap() error {
	return e.Cause
}

// ModelNotFoundError indicates the requested model does not exist.
type ModelNotFoundError struct {
	Model string
	Cause error
}

func (e *ModelNotFoundError) Error() string {
	if e.Cause != nil {
		return "model not found: " + e.Model + ": " + e.Cause.Error()
	}
	return "model not found: " + e.Model
}

func (e *ModelNotFoundError) Unwrap() error {
	return e.Cause
}

// TimeoutError indicates a timeout occurred.
type TimeoutError struct {
	Cause error
}

func (e *TimeoutError) Error() string {
	if e.Cause != nil {
		return "request timeout: " + e.Cause.Error()
	}
	return "request timeout"
}

func (e *TimeoutError) Unwrap() error {
	return e.Cause
}

// ContextCanceledError indicates the context was canceled.
type ContextCanceledError struct {
	Cause error
}

func (e *ContextCanceledError) Error() string {
	if e.Cause != nil {
		return "context canceled: " + e.Cause.Error()
	}
	return "context canceled"
}

func (e *ContextCanceledError) Unwrap() error {
	return e.Cause
}

// ProviderError indicates a general provider error.
type ProviderError struct {
	Provider string
	Message  string
}

func (e *ProviderError) Error() string {
	return e.Provider + " error: " + e.Message
}
