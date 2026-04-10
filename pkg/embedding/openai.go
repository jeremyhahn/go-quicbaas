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
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sync"
	"time"

	"github.com/jeremyhahn/go-quicbaas/pkg/api"
)

const (
	// DefaultOpenAIURL is the default OpenAI API endpoint.
	DefaultOpenAIURL = "https://api.openai.com"

	// DefaultOpenAITimeout is the default HTTP timeout.
	DefaultOpenAITimeout = 30 * time.Second

	// Default retry configuration.
	defaultMaxRetries     = 3
	defaultRetryBackoffMS = 1000
)

// OpenAI model specifications.
const (
	ModelTextEmbedding3Small = "text-embedding-3-small"
	ModelTextEmbedding3Large = "text-embedding-3-large"
	ModelTextEmbeddingAda002 = "text-embedding-ada-002"
)

// modelSpec defines specifications for OpenAI-compatible models.
type openaiModelSpec struct {
	dimensions int
	maxTokens  int
}

// openaiModels maps known model names to their specifications.
var openaiModels = map[string]openaiModelSpec{
	// OpenAI models
	ModelTextEmbedding3Small: {dimensions: 1536, maxTokens: 8191},
	ModelTextEmbedding3Large: {dimensions: 3072, maxTokens: 8191},
	ModelTextEmbeddingAda002: {dimensions: 1536, maxTokens: 8191},
	// Common Ollama models (when used via OpenAI-compatible API)
	"nomic-embed-text":  {dimensions: 768, maxTokens: 8192},
	"mxbai-embed-large": {dimensions: 1024, maxTokens: 8192},
	"all-minilm":        {dimensions: 384, maxTokens: 8192},
	"all-minilm-l6-v2":  {dimensions: 384, maxTokens: 8192},
	"all-minilm-l12-v2": {dimensions: 384, maxTokens: 8192},
	"bge-small-en-v1.5": {dimensions: 384, maxTokens: 8192},
	"bge-base-en-v1.5":  {dimensions: 768, maxTokens: 8192},
	"bge-large-en-v1.5": {dimensions: 1024, maxTokens: 8192},
	// LMStudio models
	"nomic-ai/nomic-embed-text-v1.5-GGUF": {dimensions: 768, maxTokens: 8192},
}

// OpenAIProvider implements the Provider interface for OpenAI-compatible embeddings.
// This works with OpenAI, Ollama (v1 API), and LMStudio.
type OpenAIProvider struct {
	client     *http.Client
	apiURL     string
	apiKey     string // Optional for local providers
	model      string
	dimensions int
	maxTokens  int
	maxRetries int
	backoffMS  int
	mu         sync.RWMutex
}

// openaiRequest represents the request body for OpenAI-compatible embeddings API.
type openaiRequest struct {
	Input []string `json:"input"`
	Model string   `json:"model"`
}

// openaiResponse represents the response from OpenAI-compatible embeddings API.
type openaiResponse struct {
	Object string            `json:"object"`
	Data   []openaiEmbedding `json:"data"`
	Model  string            `json:"model"`
	Usage  openaiUsage       `json:"usage"`
}

// openaiEmbedding represents a single embedding in the response.
type openaiEmbedding struct {
	Object    string    `json:"object"`
	Embedding []float32 `json:"embedding"`
	Index     int       `json:"index"`
}

// openaiUsage represents token usage information.
type openaiUsage struct {
	PromptTokens int `json:"prompt_tokens"`
	TotalTokens  int `json:"total_tokens"`
}

// openaiError represents an error response from OpenAI-compatible API.
type openaiError struct {
	Error struct {
		Message string `json:"message"`
		Type    string `json:"type"`
		Code    string `json:"code"`
	} `json:"error"`
}

// NewOpenAIProvider creates a new OpenAI-compatible embedding provider.
// This supports OpenAI, Ollama (via v1 API), and LMStudio.
func NewOpenAIProvider(config Config) (*OpenAIProvider, error) {
	// Set defaults
	apiURL := config.APIURL
	if apiURL == "" {
		apiURL = DefaultOpenAIURL
	}

	model := config.Model
	if model == "" {
		model = ModelTextEmbedding3Small
	}

	timeout := time.Duration(config.Timeout) * time.Second
	if timeout == 0 {
		timeout = DefaultOpenAITimeout
	}

	// Set retry configuration
	maxRetries := config.MaxRetries
	if maxRetries <= 0 {
		maxRetries = defaultMaxRetries
	}

	backoffMS := config.RetryBackoffMS
	if backoffMS <= 0 {
		backoffMS = defaultRetryBackoffMS
	}

	// Determine dimensions and max tokens
	dimensions := config.Dimensions
	maxTokens := 8192 // Default max tokens

	// Use model spec if available, but allow config override
	if spec, ok := openaiModels[model]; ok {
		if dimensions == 0 {
			dimensions = spec.dimensions
		}
		maxTokens = spec.maxTokens
	}

	// Create HTTP client with timeout
	client := &http.Client{
		Timeout: timeout,
		Transport: &http.Transport{
			MaxIdleConns:        10,
			MaxIdleConnsPerHost: 10,
			IdleConnTimeout:     90 * time.Second,
			DisableKeepAlives:   false,
		},
	}

	provider := &OpenAIProvider{
		client:     client,
		apiURL:     apiURL,
		apiKey:     config.APIKey,
		model:      model,
		dimensions: dimensions, // Will auto-detect if 0
		maxTokens:  maxTokens,
		maxRetries: maxRetries,
		backoffMS:  backoffMS,
	}

	return provider, nil
}

// Embed generates an embedding for a single text.
func (p *OpenAIProvider) Embed(ctx context.Context, text string) ([]float32, error) {
	// Validate input
	if text == "" {
		return nil, api.NewEmbeddingInvalidInputError("OpenAIProvider.Embed", "text cannot be empty", ErrInvalidInput)
	}

	// Use batch API for consistency
	embeddings, err := p.EmbedBatch(ctx, []string{text})
	if err != nil {
		return nil, err
	}

	if len(embeddings) == 0 {
		return nil, api.NewEmbeddingEmptyResponseError("OpenAIProvider.Embed")
	}

	return embeddings[0], nil
}

// EmbedBatch generates embeddings for multiple texts in a single request.
func (p *OpenAIProvider) EmbedBatch(ctx context.Context, texts []string) ([][]float32, error) {
	if len(texts) == 0 {
		return nil, api.NewEmbeddingInvalidInputError("OpenAIProvider.EmbedBatch", "texts cannot be empty", ErrInvalidInput)
	}

	// Validate texts
	for i, text := range texts {
		if text == "" {
			return nil, api.NewEmbeddingInvalidInputError("OpenAIProvider.EmbedBatch",
				fmt.Sprintf("text at index %d is empty", i), ErrInvalidInput)
		}
	}

	// Retry with exponential backoff
	var lastErr error
	for attempt := 0; attempt <= p.maxRetries; attempt++ {
		if attempt > 0 {
			// Calculate backoff duration: backoffMS * 2^(attempt-1)
			backoff := time.Duration(p.backoffMS*(1<<uint(attempt-1))) * time.Millisecond //nolint:gosec // G115: Safe conversion - attempt limited by retry count
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(backoff):
			}
		}

		embeddings, err := p.embedBatchOnce(ctx, texts)
		if err == nil {
			return embeddings, nil
		}

		lastErr = err

		// Don't retry on client errors (4xx except 429)
		if errors.Is(err, ErrInvalidCredentials) || errors.Is(err, ErrInvalidInput) || errors.Is(err, ErrModelNotFound) {
			return nil, err
		}

		// Only retry on rate limit or server errors
		if !errors.Is(err, ErrRateLimitExceeded) && !errors.Is(err, ErrProviderUnavailable) {
			// Check if it's a connection error (for local providers)
			if !isConnectionRefused(err) {
				return nil, err
			}
		}
	}

	return nil, api.NewEmbeddingRetryExhaustedError("OpenAIProvider.EmbedBatch", p.maxRetries, lastErr)
}

// embedBatchOnce performs a single embedding request without retries.
func (p *OpenAIProvider) embedBatchOnce(ctx context.Context, texts []string) ([][]float32, error) {
	// Create request
	reqBody := openaiRequest{
		Input: texts,
		Model: p.model,
	}

	jsonData, err := json.Marshal(reqBody)
	if err != nil {
		return nil, api.NewEmbeddingMarshalRequestError("OpenAIProvider.embedBatchOnce", err)
	}

	// Create HTTP request
	url := fmt.Sprintf("%s/v1/embeddings", p.apiURL)
	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewBuffer(jsonData))
	if err != nil {
		return nil, api.NewEmbeddingCreateRequestError("OpenAIProvider.embedBatchOnce", err)
	}

	req.Header.Set("Content-Type", "application/json")

	// Add authorization header if API key is provided
	if p.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+p.apiKey)
	}

	// Send request
	resp, err := p.client.Do(req)
	if err != nil {
		// Check if server is not running (for local providers)
		if isConnectionRefused(err) {
			return nil, api.NewEmbeddingServerNotRunningError("OpenAIProvider.embedBatchOnce", p.apiURL)
		}
		return nil, api.NewEmbeddingProviderUnavailableError("OpenAIProvider.embedBatchOnce", err)
	}
	defer resp.Body.Close()

	// Read response body
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, api.NewEmbeddingReadResponseError("OpenAIProvider.embedBatchOnce", err)
	}

	// Handle error responses
	if resp.StatusCode != http.StatusOK {
		return nil, p.handleErrorResponse(resp.StatusCode, body)
	}

	// Parse response
	var openaiResp openaiResponse
	if err := json.Unmarshal(body, &openaiResp); err != nil {
		return nil, api.NewEmbeddingUnmarshalResponseError("OpenAIProvider.embedBatchOnce", err)
	}

	// Extract embeddings
	if len(openaiResp.Data) == 0 {
		return nil, api.NewEmbeddingEmptyInResponseError("OpenAIProvider.embedBatchOnce")
	}

	embeddings := make([][]float32, len(texts))
	for _, data := range openaiResp.Data {
		if data.Index < 0 || data.Index >= len(texts) {
			return nil, api.NewEmbeddingInvalidIndexError("OpenAIProvider.embedBatchOnce", data.Index)
		}

		// Validate embedding
		if len(data.Embedding) == 0 {
			return nil, api.NewEmbeddingEmptyAtIndexError("OpenAIProvider.embedBatchOnce", data.Index)
		}

		// Auto-detect and validate dimensions
		if err := p.validateDimensions(len(data.Embedding)); err != nil {
			return nil, err
		}

		embeddings[data.Index] = data.Embedding
	}

	// Validate all embeddings were returned
	for i, emb := range embeddings {
		if emb == nil {
			return nil, api.NewEmbeddingMissingAtIndexError("OpenAIProvider.embedBatchOnce", i)
		}
	}

	return embeddings, nil
}

// handleErrorResponse maps HTTP error responses to appropriate errors.
func (p *OpenAIProvider) handleErrorResponse(statusCode int, body []byte) error {
	// Try to parse error message
	var errResp openaiError
	var errMsg string
	if err := json.Unmarshal(body, &errResp); err == nil && errResp.Error.Message != "" {
		errMsg = errResp.Error.Message
	} else {
		errMsg = string(body)
	}
	if errMsg == "" {
		errMsg = "unknown error"
	}

	// Map status codes to errors
	switch statusCode {
	case http.StatusUnauthorized, http.StatusForbidden:
		return api.NewEmbeddingHTTPError("OpenAIProvider.handleErrorResponse", statusCode, errMsg, ErrInvalidCredentials)
	case http.StatusBadRequest:
		return api.NewEmbeddingHTTPError("OpenAIProvider.handleErrorResponse", statusCode, errMsg, ErrInvalidInput)
	case http.StatusNotFound:
		return api.NewEmbeddingHTTPModelNotFoundError("OpenAIProvider.handleErrorResponse", p.model, errMsg, ErrModelNotFound)
	case http.StatusTooManyRequests:
		return api.NewEmbeddingHTTPError("OpenAIProvider.handleErrorResponse", statusCode, errMsg, ErrRateLimitExceeded)
	case http.StatusInternalServerError, http.StatusBadGateway, http.StatusServiceUnavailable, http.StatusGatewayTimeout:
		return api.NewEmbeddingHTTPError("OpenAIProvider.handleErrorResponse", statusCode, errMsg, ErrProviderUnavailable)
	default:
		return api.NewEmbeddingHTTPUnknownError("OpenAIProvider.handleErrorResponse", statusCode, errMsg)
	}
}

// validateDimensions validates and auto-detects embedding dimensions.
func (p *OpenAIProvider) validateDimensions(received int) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	// Auto-detect dimensions on first embedding
	if p.dimensions == 0 {
		p.dimensions = received
		return nil
	}

	// Validate subsequent embeddings match
	if p.dimensions != received {
		return api.NewEmbeddingDimensionsMismatchError("OpenAIProvider.validateDimensions", p.dimensions, received)
	}

	return nil
}

// Dimensions returns the dimensionality of embeddings produced by this provider.
func (p *OpenAIProvider) Dimensions() int {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.dimensions
}

// MaxTokens returns the maximum number of tokens this provider can handle.
func (p *OpenAIProvider) MaxTokens() int {
	return p.maxTokens
}

// Name returns the name of this provider.
func (p *OpenAIProvider) Name() string {
	return "openai"
}

// Close closes the provider and releases resources.
func (p *OpenAIProvider) Close() error {
	// Close idle connections
	p.client.CloseIdleConnections()
	return nil
}
