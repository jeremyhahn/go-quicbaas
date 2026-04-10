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

package ollama

import "time"

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
// Note: Ollama's native API doesn't support batch embeddings, so we handle this client-side.
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
