//go:build !rag

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

// Package ollama provides stub types when the rag build tag is not set.
package ollama

import (
	"context"
	"errors"
	"time"
)

// ErrRAGNotEnabled is returned when RAG features are not enabled.
var ErrRAGNotEnabled = errors.New("ollama: RAG features not enabled (build with -tags=rag)")

// Common errors for Ollama operations (stubs).
var (
	ErrOllamaUnavailable = errors.New("ollama server unavailable")
	ErrModelNotFound     = errors.New("model not found")
	ErrModelNotLoaded    = errors.New("model not loaded")
	ErrInvalidInput      = errors.New("invalid input")
	ErrEmptyResponse     = errors.New("empty response from ollama")
	ErrRequestFailed     = errors.New("request failed")
	ErrInvalidResponse   = errors.New("invalid response format")
	ErrTimeout           = errors.New("request timeout")
	ErrContextCanceled   = errors.New("context canceled")
)

// ClientConfig holds configuration for the Ollama client.
type ClientConfig struct {
	BaseURL             string
	Timeout             time.Duration
	MaxRetries          int
	RetryBackoff        time.Duration
	HealthCheckInterval time.Duration
}

// DefaultClientConfig returns the default client configuration.
func DefaultClientConfig() ClientConfig {
	return ClientConfig{}
}

// Client provides HTTP communication with the Ollama API.
type Client struct{}

// NewClient creates a new Ollama API client (stub).
func NewClient(_ ClientConfig) *Client {
	return &Client{}
}

// BaseURL returns the configured base URL.
func (c *Client) BaseURL() string {
	return ""
}

// Close closes the client and releases resources.
func (c *Client) Close() error {
	return nil
}

// EmbeddingService generates embeddings using the Ollama API.
type EmbeddingService struct{}

// NewEmbeddingService creates a new embedding service (stub).
func NewEmbeddingService(_ *Client, _ string) *EmbeddingService {
	return &EmbeddingService{}
}

// Model returns the configured embedding model.
func (s *EmbeddingService) Model() string {
	return ""
}

// SetModel changes the embedding model.
func (s *EmbeddingService) SetModel(_ string) {}

// Embed generates an embedding for a single text (stub).
func (s *EmbeddingService) Embed(_ context.Context, _ string) ([]float32, error) {
	return nil, ErrRAGNotEnabled
}

// EmbedWithModel generates an embedding for a single text using a specific model (stub).
func (s *EmbeddingService) EmbedWithModel(_ context.Context, _, _ string) ([]float32, error) {
	return nil, ErrRAGNotEnabled
}

// EmbedBatch generates embeddings for multiple texts (stub).
func (s *EmbeddingService) EmbedBatch(_ context.Context, _ []string) ([][]float32, error) {
	return nil, ErrRAGNotEnabled
}

// EmbedBatchWithModel generates embeddings for multiple texts using a specific model (stub).
func (s *EmbeddingService) EmbedBatchWithModel(_ context.Context, _ string, _ []string) ([][]float32, error) {
	return nil, ErrRAGNotEnabled
}

// HealthChecker provides health check functionality for the Ollama API.
type HealthChecker struct{}

// NewHealthChecker creates a new health checker (stub).
func NewHealthChecker(_ *Client) *HealthChecker {
	return &HealthChecker{}
}

// Check performs a health check against the Ollama server (stub).
func (h *HealthChecker) Check(_ context.Context) (*HealthStatus, error) {
	return &HealthStatus{Available: false, Error: ErrRAGNotEnabled.Error()}, ErrRAGNotEnabled
}

// IsAvailable returns the last known availability status without making a request.
func (h *HealthChecker) IsAvailable() bool {
	return false
}

// LastStatus returns the last known health status without making a request.
func (h *HealthChecker) LastStatus() *HealthStatus {
	return &HealthStatus{Available: false, Error: ErrRAGNotEnabled.Error()}
}

// ModelService provides model information from the Ollama API.
type ModelService struct{}

// NewModelService creates a new model service (stub).
func NewModelService(_ *Client) *ModelService {
	return &ModelService{}
}

// List returns all available models from Ollama (stub).
func (s *ModelService) List(_ context.Context) ([]ModelInfo, error) {
	return nil, ErrRAGNotEnabled
}

// Show returns detailed information about a model (stub).
func (s *ModelService) Show(_ context.Context, _ string) (*ModelDetails, error) {
	return nil, ErrRAGNotEnabled
}

// IsAvailable checks if a model exists locally (stub).
func (s *ModelService) IsAvailable(_ context.Context, _ string) (bool, error) {
	return false, ErrRAGNotEnabled
}

// GetModelInfo returns information about a specific model from the list (stub).
func (s *ModelService) GetModelInfo(_ context.Context, _ string) (*ModelInfo, error) {
	return nil, ErrRAGNotEnabled
}

// EmbedRequest represents a request to generate embeddings.
type EmbedRequest struct {
	Model  string `json:"model"`
	Prompt string `json:"prompt"`
}

// EmbedResponse represents the response from the embeddings API.
type EmbedResponse struct {
	Embedding []float32 `json:"embedding"`
}

// EmbedBatchRequest represents a request to generate embeddings for multiple prompts.
type EmbedBatchRequest struct {
	Model   string   `json:"model"`
	Prompts []string `json:"prompts"`
}

// ModelInfo represents information about an Ollama model.
type ModelInfo struct {
	Name       string    `json:"name"`
	ModifiedAt time.Time `json:"modified_at"`
	Size       int64     `json:"size"`
	Digest     string    `json:"digest"`
	Details    Details   `json:"details,omitempty"`
}

// Details contains detailed model information.
type Details struct {
	Format            string   `json:"format"`
	Family            string   `json:"family"`
	Families          []string `json:"families"`
	ParameterSize     string   `json:"parameter_size"`
	QuantizationLevel string   `json:"quantization_level"`
}

// ModelDetails contains extended model information from the show endpoint.
type ModelDetails struct {
	License    string         `json:"license"`
	Modelfile  string         `json:"modelfile"`
	Parameters string         `json:"parameters"`
	Template   string         `json:"template"`
	Details    Details        `json:"details"`
	ModelInfo  map[string]any `json:"model_info"`
}

// ListModelsResponse represents the response from the list models API.
type ListModelsResponse struct {
	Models []ModelInfo `json:"models"`
}

// ShowModelRequest represents a request to show model details.
type ShowModelRequest struct {
	Name string `json:"name"`
}

// PullRequest represents a request to pull a model.
type PullRequest struct {
	Name   string `json:"name"`
	Stream bool   `json:"stream"`
}

// PullProgress represents progress during a model pull operation.
type PullProgress struct {
	Status    string `json:"status"`
	Digest    string `json:"digest,omitempty"`
	Total     int64  `json:"total,omitempty"`
	Completed int64  `json:"completed,omitempty"`
}

// HealthStatus represents the health status of the Ollama server.
type HealthStatus struct {
	Available   bool      `json:"available"`
	Version     string    `json:"version,omitempty"`
	LastChecked time.Time `json:"last_checked"`
	Error       string    `json:"error,omitempty"`
}

// VersionResponse represents the response from the version endpoint.
type VersionResponse struct {
	Version string `json:"version"`
}

// ErrorResponse represents an error response from the Ollama API.
type ErrorResponse struct {
	Error string `json:"error"`
}

// ChatRole represents the role in a chat message.
type ChatRole string

const (
	// ChatRoleSystem represents a system message that sets behavior.
	ChatRoleSystem ChatRole = "system"

	// ChatRoleUser represents a user message.
	ChatRoleUser ChatRole = "user"

	// ChatRoleAssistant represents an assistant response.
	ChatRoleAssistant ChatRole = "assistant"
)

// ChatMessage represents a single message in a chat conversation.
type ChatMessage struct {
	Role    ChatRole `json:"role"`
	Content string   `json:"content"`
}

// ChatRequest represents a request to the Ollama chat API.
type ChatRequest struct {
	Model    string        `json:"model"`
	Messages []ChatMessage `json:"messages"`
	Stream   bool          `json:"stream"`
	Options  *ModelOptions `json:"options,omitempty"`
}

// ChatResponse represents the response from the Ollama chat API.
type ChatResponse struct {
	Model           string      `json:"model"`
	CreatedAt       string      `json:"created_at"`
	Message         ChatMessage `json:"message"`
	Done            bool        `json:"done"`
	TotalDuration   int64       `json:"total_duration,omitempty"`
	LoadDuration    int64       `json:"load_duration,omitempty"`
	PromptEvalCount int         `json:"prompt_eval_count,omitempty"`
	EvalCount       int         `json:"eval_count,omitempty"`
	EvalDuration    int64       `json:"eval_duration,omitempty"`
}

// GenerateRequest represents a request to the Ollama generate API.
type GenerateRequest struct {
	Model   string        `json:"model"`
	Prompt  string        `json:"prompt"`
	System  string        `json:"system,omitempty"`
	Stream  bool          `json:"stream"`
	Options *ModelOptions `json:"options,omitempty"`
}

// GenerateResponse represents the response from the Ollama generate API.
type GenerateResponse struct {
	Model           string `json:"model"`
	CreatedAt       string `json:"created_at"`
	Response        string `json:"response"`
	Done            bool   `json:"done"`
	TotalDuration   int64  `json:"total_duration,omitempty"`
	LoadDuration    int64  `json:"load_duration,omitempty"`
	PromptEvalCount int    `json:"prompt_eval_count,omitempty"`
	EvalCount       int    `json:"eval_count,omitempty"`
	EvalDuration    int64  `json:"eval_duration,omitempty"`
}

// ModelOptions represents optional model parameters.
type ModelOptions struct {
	Temperature   float64  `json:"temperature,omitempty"`
	TopP          float64  `json:"top_p,omitempty"`
	TopK          int      `json:"top_k,omitempty"`
	NumPredict    int      `json:"num_predict,omitempty"`
	Stop          []string `json:"stop,omitempty"`
	Seed          int      `json:"seed,omitempty"`
	RepeatPenalty float64  `json:"repeat_penalty,omitempty"`
}

// ChatService provides chat and generation capabilities using the Ollama API.
type ChatService struct{}

// NewChatService creates a new chat service (stub).
func NewChatService(_ *Client, _ string) *ChatService {
	return &ChatService{}
}

// Model returns the configured chat model.
func (s *ChatService) Model() string {
	return ""
}

// SetModel changes the chat model.
func (s *ChatService) SetModel(_ string) {}

// Chat sends a chat completion request (stub).
func (s *ChatService) Chat(_ context.Context, _ []ChatMessage, _ *ModelOptions) (*ChatResponse, error) {
	return nil, ErrRAGNotEnabled
}

// ChatWithModel sends a chat completion request using a specific model (stub).
func (s *ChatService) ChatWithModel(_ context.Context, _ string, _ []ChatMessage, _ *ModelOptions) (*ChatResponse, error) {
	return nil, ErrRAGNotEnabled
}

// Generate sends a text generation request (stub).
func (s *ChatService) Generate(_ context.Context, _ string, _ *ModelOptions) (*GenerateResponse, error) {
	return nil, ErrRAGNotEnabled
}

// GenerateWithModel sends a text generation request using a specific model (stub).
func (s *ChatService) GenerateWithModel(_ context.Context, _, _ string, _ *ModelOptions) (*GenerateResponse, error) {
	return nil, ErrRAGNotEnabled
}

// GenerateWithSystem sends a text generation request with a system prompt (stub).
func (s *ChatService) GenerateWithSystem(_ context.Context, _, _ string, _ *ModelOptions) (*GenerateResponse, error) {
	return nil, ErrRAGNotEnabled
}
