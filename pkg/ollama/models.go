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

import (
	"context"
	"encoding/json"
)

// ModelService provides model information from the Ollama API.
type ModelService struct {
	client *Client
}

// NewModelService creates a new model service.
func NewModelService(client *Client) *ModelService {
	return &ModelService{
		client: client,
	}
}

// List returns all available models from Ollama.
func (s *ModelService) List(ctx context.Context) ([]ModelInfo, error) {
	respBody, err := s.client.doRequest(ctx, "GET", "/api/tags", nil)
	if err != nil {
		return nil, err
	}

	var resp ListModelsResponse
	if err := json.Unmarshal(respBody, &resp); err != nil {
		return nil, &OllamaSentinelCauseError{Sentinel: ErrInvalidResponse, Cause: err}
	}

	return resp.Models, nil
}

// Show returns detailed information about a model.
func (s *ModelService) Show(ctx context.Context, model string) (*ModelDetails, error) {
	if model == "" {
		return nil, &OllamaSentinelMessageError{Sentinel: ErrInvalidInput, Message: "model name cannot be empty"}
	}

	req := ShowModelRequest{
		Name: model,
	}

	respBody, err := s.client.doRequest(ctx, "POST", "/api/show", req)
	if err != nil {
		return nil, err
	}

	var resp ModelDetails
	if err := json.Unmarshal(respBody, &resp); err != nil {
		return nil, &OllamaSentinelCauseError{Sentinel: ErrInvalidResponse, Cause: err}
	}

	return &resp, nil
}

// IsAvailable checks if a model exists locally.
func (s *ModelService) IsAvailable(ctx context.Context, model string) (bool, error) {
	if model == "" {
		return false, &OllamaSentinelMessageError{Sentinel: ErrInvalidInput, Message: "model name cannot be empty"}
	}

	models, err := s.List(ctx)
	if err != nil {
		return false, err
	}

	for _, m := range models {
		if m.Name == model {
			return true, nil
		}
	}

	return false, nil
}

// GetModelInfo returns information about a specific model from the list.
func (s *ModelService) GetModelInfo(ctx context.Context, model string) (*ModelInfo, error) {
	if model == "" {
		return nil, &OllamaSentinelMessageError{Sentinel: ErrInvalidInput, Message: "model name cannot be empty"}
	}

	models, err := s.List(ctx)
	if err != nil {
		return nil, err
	}

	for _, m := range models {
		if m.Name == model {
			return &m, nil
		}
	}

	return nil, &OllamaSentinelMessageError{Sentinel: ErrModelNotFound, Message: model}
}
