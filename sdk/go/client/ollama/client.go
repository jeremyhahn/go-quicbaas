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

package ollama

import (
	"context"
	"time"

	"github.com/jeremyhahn/go-quicbaas/sdk/go/errors"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/transport"
)

const (
	serviceName = "quicbaas.Ollama"
)

// Client provides Ollama embedding service operations.
type Client interface {
	// Embed generates an embedding for a single text using the default model.
	Embed(ctx context.Context, req *EmbedRequest) (*EmbedResponse, error)

	// EmbedWithModel generates an embedding using a specific model.
	EmbedWithModel(ctx context.Context, req *EmbedWithModelRequest) (*EmbedResponse, error)

	// EmbedBatch generates embeddings for multiple texts.
	EmbedBatch(ctx context.Context, req *EmbedBatchRequest) (*EmbedBatchResponse, error)

	// ListModels returns all available models from Ollama.
	ListModels(ctx context.Context, req *ListModelsRequest) (*ListModelsResponse, error)

	// GetModelInfo returns detailed information about a specific model.
	GetModelInfo(ctx context.Context, req *GetModelInfoRequest) (*GetModelInfoResponse, error)

	// Health checks Ollama connectivity and availability.
	Health(ctx context.Context, req *HealthRequest) (*HealthResponse, error)

	// IsAvailable returns true if Ollama is available and responding.
	IsAvailable() bool

	// DefaultModel returns the configured default embedding model.
	DefaultModel() string

	// BaseURL returns the configured Ollama base URL.
	BaseURL() string
}

// =============================================================================
// Request Types
// =============================================================================

// EmbedRequest requests embedding generation for a single text.
type EmbedRequest struct {
	// Text is the input text to embed.
	Text string
}

// EmbedWithModelRequest requests embedding generation with a specific model.
type EmbedWithModelRequest struct {
	// Text is the input text to embed.
	Text string

	// Model is the embedding model to use.
	Model string
}

// EmbedBatchRequest requests batch embedding generation.
type EmbedBatchRequest struct {
	// Texts is the list of input texts to embed.
	Texts []string

	// Model optionally specifies which model to use (uses default if empty).
	Model string
}

// ListModelsRequest requests a list of available models.
type ListModelsRequest struct{}

// GetModelInfoRequest requests detailed model information.
type GetModelInfoRequest struct {
	// Model is the model name.
	Model string
}

// HealthRequest requests Ollama health status.
type HealthRequest struct{}

// =============================================================================
// Response Types
// =============================================================================

// EmbedResponse returns the generated embedding.
type EmbedResponse struct {
	// Embedding is the embedding vector.
	Embedding []float32

	// Model is the model used.
	Model string

	// Dimensions is the embedding vector dimensions.
	Dimensions int

	// ProcessingTime is the time taken to generate the embedding.
	ProcessingTime time.Duration

	// Timestamp is when the embedding was generated.
	Timestamp time.Time
}

// EmbedBatchResponse returns batch embedding results.
type EmbedBatchResponse struct {
	// Embeddings is the list of embedding vectors (same order as input).
	Embeddings [][]float32

	// Model is the model used.
	Model string

	// Dimensions is the embedding vector dimensions.
	Dimensions int

	// Count is the number of embeddings generated.
	Count int

	// ProcessingTime is the total processing time.
	ProcessingTime time.Duration

	// Timestamp is when the batch was processed.
	Timestamp time.Time
}

// ModelInfo contains information about an Ollama model.
type ModelInfo struct {
	// Name is the model name.
	Name string

	// Size is the model size in bytes.
	Size int64

	// Digest is the model digest/hash.
	Digest string

	// ModifiedAt is when the model was last modified.
	ModifiedAt time.Time
}

// ListModelsResponse returns the list of available models.
type ListModelsResponse struct {
	// Models is the list of available models.
	Models []ModelInfo

	// Count is the number of models.
	Count int

	// Timestamp is when the list was retrieved.
	Timestamp time.Time
}

// ModelDetails contains detailed model information.
type ModelDetails struct {
	// License is the model license.
	License string

	// Modelfile is the model file contents.
	Modelfile string

	// Format is the model format (e.g., "gguf").
	Format string

	// Family is the model family (e.g., "nomic", "llama").
	Family string

	// ParameterSize is the model parameter size (e.g., "137M").
	ParameterSize string

	// QuantizationLevel is the quantization level (e.g., "Q4_0").
	QuantizationLevel string
}

// GetModelInfoResponse returns detailed model information.
type GetModelInfoResponse struct {
	// Model is the model name.
	Model string

	// Details contains the model details.
	Details ModelDetails

	// Timestamp is when the info was retrieved.
	Timestamp time.Time
}

// HealthResponse returns Ollama health status.
type HealthResponse struct {
	// Available indicates if Ollama is available.
	Available bool

	// Version is the Ollama server version.
	Version string

	// BaseURL is the Ollama API URL.
	BaseURL string

	// Error contains any error message.
	Error string

	// LastChecked is when the health was last checked.
	LastChecked time.Time

	// Timestamp is when this response was generated.
	Timestamp time.Time
}

// =============================================================================
// Internal Response Types for Transport
// =============================================================================

// isAvailableResponse is the internal response for IsAvailable.
type isAvailableResponse struct {
	Available bool
}

// defaultModelResponse is the internal response for DefaultModel.
type defaultModelResponse struct {
	Model string
}

// baseURLResponse is the internal response for BaseURL.
type baseURLResponse struct {
	BaseURL string
}

// =============================================================================
// Client Implementation
// =============================================================================

// client implements the Client interface.
type client struct {
	transport transport.Transport
}

// NewClient creates a new Ollama client.
func NewClient(t transport.Transport) Client {
	if t == nil {
		return nil
	}
	return &client{
		transport: t,
	}
}

// Embed generates an embedding for a single text using the default model.
func (c *client) Embed(ctx context.Context, req *EmbedRequest) (*EmbedResponse, error) {
	if err := validateEmbedRequest(req); err != nil {
		return nil, err
	}

	if ctx == nil {
		return nil, errors.NewInvalidInput("ollama.Embed", "context is required")
	}

	resp, err := c.transport.Request(ctx, serviceName, "Embed", req)
	if err != nil {
		return nil, err
	}

	if resp == nil {
		return nil, errors.NewInternal("ollama.Embed", "nil response from server")
	}

	embedResp, ok := resp.(*EmbedResponse)
	if !ok {
		return nil, errors.NewInternal("ollama.Embed", "invalid response type")
	}

	return embedResp, nil
}

// EmbedWithModel generates an embedding using a specific model.
func (c *client) EmbedWithModel(ctx context.Context, req *EmbedWithModelRequest) (*EmbedResponse, error) {
	if err := validateEmbedWithModelRequest(req); err != nil {
		return nil, err
	}

	if ctx == nil {
		return nil, errors.NewInvalidInput("ollama.EmbedWithModel", "context is required")
	}

	resp, err := c.transport.Request(ctx, serviceName, "EmbedWithModel", req)
	if err != nil {
		return nil, err
	}

	if resp == nil {
		return nil, errors.NewInternal("ollama.EmbedWithModel", "nil response from server")
	}

	embedResp, ok := resp.(*EmbedResponse)
	if !ok {
		return nil, errors.NewInternal("ollama.EmbedWithModel", "invalid response type")
	}

	return embedResp, nil
}

// EmbedBatch generates embeddings for multiple texts.
func (c *client) EmbedBatch(ctx context.Context, req *EmbedBatchRequest) (*EmbedBatchResponse, error) {
	if err := validateEmbedBatchRequest(req); err != nil {
		return nil, err
	}

	if ctx == nil {
		return nil, errors.NewInvalidInput("ollama.EmbedBatch", "context is required")
	}

	resp, err := c.transport.Request(ctx, serviceName, "EmbedBatch", req)
	if err != nil {
		return nil, err
	}

	if resp == nil {
		return nil, errors.NewInternal("ollama.EmbedBatch", "nil response from server")
	}

	batchResp, ok := resp.(*EmbedBatchResponse)
	if !ok {
		return nil, errors.NewInternal("ollama.EmbedBatch", "invalid response type")
	}

	return batchResp, nil
}

// ListModels returns all available models from Ollama.
func (c *client) ListModels(ctx context.Context, req *ListModelsRequest) (*ListModelsResponse, error) {
	if req == nil {
		return nil, errors.NewInvalidInput("ollama.ListModels", "request is required")
	}

	if ctx == nil {
		return nil, errors.NewInvalidInput("ollama.ListModels", "context is required")
	}

	resp, err := c.transport.Request(ctx, serviceName, "ListModels", req)
	if err != nil {
		return nil, err
	}

	if resp == nil {
		return &ListModelsResponse{
			Models:    []ModelInfo{},
			Count:     0,
			Timestamp: time.Now(),
		}, nil
	}

	listResp, ok := resp.(*ListModelsResponse)
	if !ok {
		return nil, errors.NewInternal("ollama.ListModels", "invalid response type")
	}

	return listResp, nil
}

// GetModelInfo returns detailed information about a specific model.
func (c *client) GetModelInfo(ctx context.Context, req *GetModelInfoRequest) (*GetModelInfoResponse, error) {
	if err := validateGetModelInfoRequest(req); err != nil {
		return nil, err
	}

	if ctx == nil {
		return nil, errors.NewInvalidInput("ollama.GetModelInfo", "context is required")
	}

	resp, err := c.transport.Request(ctx, serviceName, "GetModelInfo", req)
	if err != nil {
		return nil, err
	}

	if resp == nil {
		return nil, errors.NewInternal("ollama.GetModelInfo", "nil response from server")
	}

	infoResp, ok := resp.(*GetModelInfoResponse)
	if !ok {
		return nil, errors.NewInternal("ollama.GetModelInfo", "invalid response type")
	}

	return infoResp, nil
}

// Health checks Ollama connectivity and availability.
func (c *client) Health(ctx context.Context, req *HealthRequest) (*HealthResponse, error) {
	if req == nil {
		return nil, errors.NewInvalidInput("ollama.Health", "request is required")
	}

	if ctx == nil {
		return nil, errors.NewInvalidInput("ollama.Health", "context is required")
	}

	resp, err := c.transport.Request(ctx, serviceName, "Health", req)
	if err != nil {
		return nil, err
	}

	if resp == nil {
		return &HealthResponse{
			Available: false,
			Timestamp: time.Now(),
		}, nil
	}

	healthResp, ok := resp.(*HealthResponse)
	if !ok {
		return nil, errors.NewInternal("ollama.Health", "invalid response type")
	}

	return healthResp, nil
}

// IsAvailable returns true if Ollama is available and responding.
func (c *client) IsAvailable() bool {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	resp, err := c.transport.Request(ctx, serviceName, "IsAvailable", &struct{}{})
	if err != nil {
		return false
	}

	if resp == nil {
		return false
	}

	availResp, ok := resp.(*isAvailableResponse)
	if !ok {
		return false
	}

	return availResp.Available
}

// DefaultModel returns the configured default embedding model.
func (c *client) DefaultModel() string {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	resp, err := c.transport.Request(ctx, serviceName, "DefaultModel", &struct{}{})
	if err != nil {
		return ""
	}

	if resp == nil {
		return ""
	}

	modelResp, ok := resp.(*defaultModelResponse)
	if !ok {
		return ""
	}

	return modelResp.Model
}

// BaseURL returns the configured Ollama base URL.
func (c *client) BaseURL() string {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	resp, err := c.transport.Request(ctx, serviceName, "BaseURL", &struct{}{})
	if err != nil {
		return ""
	}

	if resp == nil {
		return ""
	}

	urlResp, ok := resp.(*baseURLResponse)
	if !ok {
		return ""
	}

	return urlResp.BaseURL
}

// =============================================================================
// Validation Functions
// =============================================================================

func validateEmbedRequest(req *EmbedRequest) error {
	if req == nil {
		return errors.NewInvalidInput("ollama.Embed", "request is required")
	}
	if req.Text == "" {
		return errors.NewInvalidInput("ollama.Embed", "text is required")
	}
	return nil
}

func validateEmbedWithModelRequest(req *EmbedWithModelRequest) error {
	if req == nil {
		return errors.NewInvalidInput("ollama.EmbedWithModel", "request is required")
	}
	if req.Text == "" {
		return errors.NewInvalidInput("ollama.EmbedWithModel", "text is required")
	}
	if req.Model == "" {
		return errors.NewInvalidInput("ollama.EmbedWithModel", "model is required")
	}
	return nil
}

func validateEmbedBatchRequest(req *EmbedBatchRequest) error {
	if req == nil {
		return errors.NewInvalidInput("ollama.EmbedBatch", "request is required")
	}
	if len(req.Texts) == 0 {
		return errors.NewInvalidInput("ollama.EmbedBatch", "texts is required")
	}
	for i, text := range req.Texts {
		if text == "" {
			return errors.NewInvalidInput("ollama.EmbedBatch", "text at index "+intToString(i)+" is empty")
		}
	}
	return nil
}

func validateGetModelInfoRequest(req *GetModelInfoRequest) error {
	if req == nil {
		return errors.NewInvalidInput("ollama.GetModelInfo", "request is required")
	}
	if req.Model == "" {
		return errors.NewInvalidInput("ollama.GetModelInfo", "model is required")
	}
	return nil
}

// intToString converts an integer to a string without using strconv to avoid import.
func intToString(i int) string {
	if i == 0 {
		return "0"
	}
	negative := i < 0
	if negative {
		i = -i
	}
	var buf [20]byte
	pos := len(buf)
	for i > 0 {
		pos--
		buf[pos] = byte('0' + i%10)
		i /= 10
	}
	if negative {
		pos--
		buf[pos] = '-'
	}
	return string(buf[pos:])
}
