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
	"time"

	"github.com/jeremyhahn/go-quicbaas/pkg/api"
)

const (
	// VoyageAPIURL is the default Voyage AI API endpoint.
	VoyageAPIURL = "https://api.voyageai.com/v1/embeddings"

	// Default timeout for HTTP requests.
	defaultTimeout = 30 * time.Second

	// Voyage model specifications.
	modelVoyageCode2  = "voyage-code-2"
	modelVoyage2      = "voyage-2"
	modelVoyageLarge2 = "voyage-large-2"
)

// modelSpec defines specifications for Voyage AI models.
type modelSpec struct {
	dimensions int
	maxTokens  int
}

// voyageModels maps model names to their specifications.
var voyageModels = map[string]modelSpec{
	modelVoyageCode2:  {dimensions: 1536, maxTokens: 32000},
	modelVoyage2:      {dimensions: 1024, maxTokens: 16000},
	modelVoyageLarge2: {dimensions: 1536, maxTokens: 16000},
}

// VoyageProvider implements the Provider interface for Voyage AI.
type VoyageProvider struct {
	apiKey     string
	apiURL     string
	model      string
	dimensions int
	maxTokens  int
	httpClient *http.Client
	maxRetries int
	backoffMS  int
}

// voyageRequest represents the request body for Voyage AI API.
type voyageRequest struct {
	Input []string `json:"input"`
	Model string   `json:"model"`
}

// voyageResponse represents the response from Voyage AI API.
type voyageResponse struct {
	Object string            `json:"object"`
	Data   []voyageEmbedding `json:"data"`
	Model  string            `json:"model"`
	Usage  voyageUsage       `json:"usage"`
}

// voyageEmbedding represents a single embedding in the response.
type voyageEmbedding struct {
	Object    string    `json:"object"`
	Embedding []float32 `json:"embedding"`
	Index     int       `json:"index"`
}

// voyageUsage represents token usage information.
type voyageUsage struct {
	TotalTokens int `json:"total_tokens"`
}

// voyageError represents an error response from Voyage AI API.
type voyageError struct {
	Error struct {
		Message string `json:"message"`
		Type    string `json:"type"`
		Code    string `json:"code"`
	} `json:"error"`
}

// NewVoyageProvider creates a new Voyage AI embedding provider.
func NewVoyageProvider(config Config) (*VoyageProvider, error) {
	// Validate required fields
	if config.APIKey == "" {
		return nil, ErrInvalidCredentials
	}

	if config.Model == "" {
		config.Model = modelVoyageCode2
	}

	// Validate model
	spec, ok := voyageModels[config.Model]
	if !ok {
		return nil, api.NewEmbeddingModelNotFoundError("NewVoyageProvider", config.Model, ErrModelNotFound)
	}

	// Set API URL
	apiURL := config.APIURL
	if apiURL == "" {
		apiURL = VoyageAPIURL
	}

	// Set timeout
	timeout := time.Duration(config.Timeout) * time.Second
	if timeout == 0 {
		timeout = defaultTimeout
	}

	// Set retry configuration
	// Use default retries if not explicitly set (negative value)
	// Allow 0 retries for testing/no-retry scenarios
	maxRetries := config.MaxRetries
	if maxRetries < 0 {
		maxRetries = 3
	}

	backoffMS := config.RetryBackoffMS
	if backoffMS <= 0 {
		backoffMS = 1000
	}

	// Override dimensions if specified, otherwise use model default
	dimensions := spec.dimensions
	if config.Dimensions > 0 {
		dimensions = config.Dimensions
	}

	return &VoyageProvider{
		apiKey:     config.APIKey,
		apiURL:     apiURL,
		model:      config.Model,
		dimensions: dimensions,
		maxTokens:  spec.maxTokens,
		httpClient: &http.Client{
			Timeout: timeout,
		},
		maxRetries: maxRetries,
		backoffMS:  backoffMS,
	}, nil
}

// Embed generates an embedding for a single text.
func (v *VoyageProvider) Embed(ctx context.Context, text string) ([]float32, error) {
	if text == "" {
		return nil, api.NewEmbeddingInvalidInputError("VoyageProvider.Embed", "text cannot be empty", ErrInvalidInput)
	}

	embeddings, err := v.EmbedBatch(ctx, []string{text})
	if err != nil {
		return nil, err
	}

	if len(embeddings) == 0 {
		return nil, api.NewEmbeddingEmptyResponseError("VoyageProvider.Embed")
	}

	return embeddings[0], nil
}

// EmbedBatch generates embeddings for multiple texts in a single request.
func (v *VoyageProvider) EmbedBatch(ctx context.Context, texts []string) ([][]float32, error) {
	if len(texts) == 0 {
		return nil, api.NewEmbeddingInvalidInputError("VoyageProvider.EmbedBatch", "texts cannot be empty", ErrInvalidInput)
	}

	// Validate texts
	for i, text := range texts {
		if text == "" {
			return nil, api.NewEmbeddingInvalidInputError("VoyageProvider.EmbedBatch",
				fmt.Sprintf("text at index %d is empty", i), ErrInvalidInput)
		}
	}

	// Retry with exponential backoff
	var lastErr error
	for attempt := 0; attempt <= v.maxRetries; attempt++ {
		if attempt > 0 {
			// Calculate backoff duration: backoffMS * 2^(attempt-1)
			backoff := time.Duration(v.backoffMS*(1<<uint(attempt-1))) * time.Millisecond //nolint:gosec // Exponential backoff bounded
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(backoff):
			}
		}

		embeddings, err := v.embedBatchOnce(ctx, texts)
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
			return nil, err
		}
	}

	return nil, api.NewEmbeddingRetryExhaustedError("VoyageProvider.EmbedBatch", v.maxRetries, lastErr)
}

// embedBatchOnce performs a single embedding request without retries.
func (v *VoyageProvider) embedBatchOnce(ctx context.Context, texts []string) ([][]float32, error) {
	// Create request body
	reqBody := voyageRequest{
		Input: texts,
		Model: v.model,
	}

	jsonData, err := json.Marshal(reqBody)
	if err != nil {
		return nil, api.NewEmbeddingMarshalRequestError("VoyageProvider.embedBatchOnce", err)
	}

	// Create HTTP request
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, v.apiURL, bytes.NewReader(jsonData))
	if err != nil {
		return nil, api.NewEmbeddingCreateRequestError("VoyageProvider.embedBatchOnce", err)
	}

	// Set headers
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+v.apiKey)

	// Send request
	resp, err := v.httpClient.Do(req)
	if err != nil {
		return nil, api.NewEmbeddingProviderUnavailableError("VoyageProvider.embedBatchOnce", err)
	}
	defer resp.Body.Close()

	// Read response body
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, api.NewEmbeddingReadResponseError("VoyageProvider.embedBatchOnce", err)
	}

	// Handle error responses
	if resp.StatusCode != http.StatusOK {
		return nil, v.handleErrorResponse(resp.StatusCode, body)
	}

	// Parse response
	var voyageResp voyageResponse
	if err := json.Unmarshal(body, &voyageResp); err != nil {
		return nil, api.NewEmbeddingUnmarshalResponseError("VoyageProvider.embedBatchOnce", err)
	}

	// Extract embeddings
	if len(voyageResp.Data) == 0 {
		return nil, api.NewEmbeddingEmptyInResponseError("VoyageProvider.embedBatchOnce")
	}

	embeddings := make([][]float32, len(texts))
	for _, data := range voyageResp.Data {
		if data.Index < 0 || data.Index >= len(texts) {
			return nil, api.NewEmbeddingInvalidIndexError("VoyageProvider.embedBatchOnce", data.Index)
		}
		embeddings[data.Index] = data.Embedding
	}

	// Validate all embeddings were returned
	for i, emb := range embeddings {
		if emb == nil {
			return nil, api.NewEmbeddingMissingAtIndexError("VoyageProvider.embedBatchOnce", i)
		}
	}

	return embeddings, nil
}

// handleErrorResponse maps HTTP error responses to appropriate errors.
func (v *VoyageProvider) handleErrorResponse(statusCode int, body []byte) error {
	// Try to parse error message
	var errResp voyageError
	errMsg := "unknown error"
	if err := json.Unmarshal(body, &errResp); err == nil && errResp.Error.Message != "" {
		errMsg = errResp.Error.Message
	} else if len(body) > 0 {
		errMsg = string(body)
	}

	// Map status codes to errors
	switch statusCode {
	case http.StatusUnauthorized:
		return api.NewEmbeddingHTTPError("VoyageProvider.handleErrorResponse", statusCode, errMsg, ErrInvalidCredentials)
	case http.StatusBadRequest:
		return api.NewEmbeddingHTTPError("VoyageProvider.handleErrorResponse", statusCode, errMsg, ErrInvalidInput)
	case http.StatusNotFound:
		return api.NewEmbeddingHTTPError("VoyageProvider.handleErrorResponse", statusCode, errMsg, ErrModelNotFound)
	case http.StatusTooManyRequests:
		return api.NewEmbeddingHTTPError("VoyageProvider.handleErrorResponse", statusCode, errMsg, ErrRateLimitExceeded)
	case http.StatusInternalServerError, http.StatusBadGateway, http.StatusServiceUnavailable, http.StatusGatewayTimeout:
		return api.NewEmbeddingHTTPError("VoyageProvider.handleErrorResponse", statusCode, errMsg, ErrProviderUnavailable)
	default:
		return api.NewEmbeddingHTTPUnknownError("VoyageProvider.handleErrorResponse", statusCode, errMsg)
	}
}

// Dimensions returns the dimensionality of embeddings produced by this provider.
func (v *VoyageProvider) Dimensions() int {
	return v.dimensions
}

// MaxTokens returns the maximum number of tokens this provider can handle.
func (v *VoyageProvider) MaxTokens() int {
	return v.maxTokens
}

// Name returns the name of this provider.
func (v *VoyageProvider) Name() string {
	return "voyage"
}

// Close closes the provider and releases resources.
func (v *VoyageProvider) Close() error {
	// Close idle connections
	v.httpClient.CloseIdleConnections()
	return nil
}
