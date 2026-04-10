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

// Package service provides high-level service implementation for Ollama operations.
package service

import (
	"context"
	"time"

	"github.com/jeremyhahn/go-quicbaas/pkg/config"
	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
	"github.com/jeremyhahn/go-quicbaas/pkg/ollama"
)

// =============================================================================
// Ollama Service Implementation
// =============================================================================

// ollamaService implements the OllamaService interface.
type ollamaService struct {
	config    *config.OllamaConfig
	client    interfaces.OllamaClient
	embedding interfaces.OllamaEmbeddingService
	models    interfaces.OllamaModelService
	health    interfaces.OllamaHealthChecker
}

// OllamaServiceOption defines a functional option for configuring the OllamaService.
type OllamaServiceOption func(*ollamaService)

// WithOllamaClient sets a custom client for the OllamaService.
func WithOllamaClient(client interfaces.OllamaClient) OllamaServiceOption {
	return func(s *ollamaService) {
		s.client = client
	}
}

// WithOllamaEmbeddingService sets a custom embedding service for the OllamaService.
func WithOllamaEmbeddingService(embedding interfaces.OllamaEmbeddingService) OllamaServiceOption {
	return func(s *ollamaService) {
		s.embedding = embedding
	}
}

// WithOllamaModelService sets a custom model service for the OllamaService.
func WithOllamaModelService(models interfaces.OllamaModelService) OllamaServiceOption {
	return func(s *ollamaService) {
		s.models = models
	}
}

// WithOllamaHealthChecker sets a custom health checker for the OllamaService.
func WithOllamaHealthChecker(health interfaces.OllamaHealthChecker) OllamaServiceOption {
	return func(s *ollamaService) {
		s.health = health
	}
}

// NewOllamaService creates a new Ollama service instance.
// Use functional options to inject custom dependencies for testing.
func NewOllamaService(cfg *config.OllamaConfig, opts ...OllamaServiceOption) (OllamaService, error) {
	if cfg == nil {
		return nil, ErrOllamaNotConfigured
	}

	if !cfg.Enabled {
		return nil, ErrOllamaDisabled
	}

	// Validate configuration
	if err := cfg.Validate(); err != nil {
		return nil, err
	}

	svc := &ollamaService{
		config: cfg,
	}

	// Apply functional options
	for _, opt := range opts {
		opt(svc)
	}

	// Create default implementations if not provided
	if svc.client == nil {
		clientCfg := ollama.ClientConfig{
			BaseURL:             cfg.BaseURL,
			Timeout:             cfg.Timeout,
			MaxRetries:          cfg.MaxRetries,
			RetryBackoff:        cfg.RetryBackoff,
			HealthCheckInterval: cfg.HealthCheckInterval,
		}
		svc.client = ollama.NewClient(clientCfg)
	}

	if svc.embedding == nil {
		// We need a concrete client for the embedding service
		client, ok := svc.client.(*ollama.Client)
		if !ok {
			// If a custom client was provided, create a new one for sub-services
			clientCfg := ollama.ClientConfig{
				BaseURL:             cfg.BaseURL,
				Timeout:             cfg.Timeout,
				MaxRetries:          cfg.MaxRetries,
				RetryBackoff:        cfg.RetryBackoff,
				HealthCheckInterval: cfg.HealthCheckInterval,
			}
			client = ollama.NewClient(clientCfg)
		}
		svc.embedding = ollama.NewEmbeddingService(client, cfg.DefaultEmbeddingModel)
	}

	if svc.models == nil {
		client, ok := svc.client.(*ollama.Client)
		if !ok {
			clientCfg := ollama.ClientConfig{
				BaseURL:             cfg.BaseURL,
				Timeout:             cfg.Timeout,
				MaxRetries:          cfg.MaxRetries,
				RetryBackoff:        cfg.RetryBackoff,
				HealthCheckInterval: cfg.HealthCheckInterval,
			}
			client = ollama.NewClient(clientCfg)
		}
		svc.models = ollama.NewModelService(client)
	}

	if svc.health == nil {
		client, ok := svc.client.(*ollama.Client)
		if !ok {
			clientCfg := ollama.ClientConfig{
				BaseURL:             cfg.BaseURL,
				Timeout:             cfg.Timeout,
				MaxRetries:          cfg.MaxRetries,
				RetryBackoff:        cfg.RetryBackoff,
				HealthCheckInterval: cfg.HealthCheckInterval,
			}
			client = ollama.NewClient(clientCfg)
		}
		svc.health = ollama.NewHealthChecker(client)
	}

	return svc, nil
}

// NewDefaultOllamaService creates a new Ollama service with default dependencies.
// This is a convenience function that creates the real implementations.
func NewDefaultOllamaService(cfg *config.OllamaConfig) (OllamaService, error) {
	return NewOllamaService(cfg)
}

// Embed generates an embedding for a single text using the default model.
func (s *ollamaService) Embed(ctx context.Context, req *OllamaEmbedRequest) (*OllamaEmbedResponse, error) {
	if req == nil {
		return nil, ErrInvalidRequest
	}

	start := time.Now()
	embedding, err := s.embedding.Embed(ctx, req.Text)
	if err != nil {
		return nil, err
	}

	return &OllamaEmbedResponse{
		Embedding:      embedding,
		Model:          s.embedding.Model(),
		Dimensions:     len(embedding),
		ProcessingTime: time.Since(start),
		Timestamp:      time.Now(),
	}, nil
}

// EmbedWithModel generates an embedding using a specific model.
func (s *ollamaService) EmbedWithModel(ctx context.Context, req *OllamaEmbedWithModelRequest) (*OllamaEmbedResponse, error) {
	if req == nil {
		return nil, ErrInvalidRequest
	}

	start := time.Now()
	embedding, err := s.embedding.EmbedWithModel(ctx, req.Model, req.Text)
	if err != nil {
		return nil, err
	}

	return &OllamaEmbedResponse{
		Embedding:      embedding,
		Model:          req.Model,
		Dimensions:     len(embedding),
		ProcessingTime: time.Since(start),
		Timestamp:      time.Now(),
	}, nil
}

// EmbedBatch generates embeddings for multiple texts.
func (s *ollamaService) EmbedBatch(ctx context.Context, req *OllamaEmbedBatchRequest) (*OllamaEmbedBatchResponse, error) {
	if req == nil {
		return nil, ErrInvalidRequest
	}

	start := time.Now()
	var embeddings [][]float32
	var err error
	model := req.Model
	if model == "" {
		model = s.embedding.Model()
		embeddings, err = s.embedding.EmbedBatch(ctx, req.Texts)
	} else {
		embeddings, err = s.embedding.EmbedBatchWithModel(ctx, model, req.Texts)
	}
	if err != nil {
		return nil, err
	}

	dimensions := 0
	if len(embeddings) > 0 {
		dimensions = len(embeddings[0])
	}

	return &OllamaEmbedBatchResponse{
		Embeddings:     embeddings,
		Model:          model,
		Dimensions:     dimensions,
		Count:          len(embeddings),
		ProcessingTime: time.Since(start),
		Timestamp:      time.Now(),
	}, nil
}

// ListModels returns all available models from Ollama.
func (s *ollamaService) ListModels(ctx context.Context, req *OllamaListModelsRequest) (*OllamaListModelsResponse, error) {
	models, err := s.models.List(ctx)
	if err != nil {
		return nil, err
	}

	result := make([]OllamaModelInfo, len(models))
	for i, m := range models {
		result[i] = OllamaModelInfo{
			Name:       m.Name,
			Size:       m.Size,
			Digest:     m.Digest,
			ModifiedAt: m.ModifiedAt,
		}
	}

	return &OllamaListModelsResponse{
		Models:    result,
		Count:     len(result),
		Timestamp: time.Now(),
	}, nil
}

// GetModelInfo returns detailed information about a specific model.
func (s *ollamaService) GetModelInfo(ctx context.Context, req *OllamaGetModelInfoRequest) (*OllamaGetModelInfoResponse, error) {
	if req == nil || req.Model == "" {
		return nil, ErrInvalidRequest
	}

	details, err := s.models.Show(ctx, req.Model)
	if err != nil {
		return nil, err
	}

	return &OllamaGetModelInfoResponse{
		Model: req.Model,
		Details: OllamaModelDetails{
			License:           details.License,
			Modelfile:         details.Modelfile,
			Format:            details.Details.Format,
			Family:            details.Details.Family,
			ParameterSize:     details.Details.ParameterSize,
			QuantizationLevel: details.Details.QuantizationLevel,
		},
		Timestamp: time.Now(),
	}, nil
}

// Health checks Ollama connectivity and availability.
func (s *ollamaService) Health(ctx context.Context, req *OllamaHealthRequest) (*OllamaHealthResponse, error) {
	status, err := s.health.Check(ctx)

	response := &OllamaHealthResponse{
		Available:   status.Available,
		Version:     status.Version,
		BaseURL:     s.client.BaseURL(),
		Error:       status.Error,
		LastChecked: status.LastChecked,
		Timestamp:   time.Now(),
	}

	// Always return the response even if there was an error
	if err != nil {
		response.Error = err.Error()
	}

	return response, nil
}

// IsAvailable returns true if Ollama is available and responding.
func (s *ollamaService) IsAvailable() bool {
	return s.health.IsAvailable()
}

// DefaultModel returns the configured default embedding model.
func (s *ollamaService) DefaultModel() string {
	return s.config.DefaultEmbeddingModel
}

// BaseURL returns the configured Ollama base URL.
func (s *ollamaService) BaseURL() string {
	return s.config.BaseURL
}

// =============================================================================
// Ollama Service Errors
// =============================================================================

// Ollama service errors.
var (
	ErrOllamaNotConfigured = &ollamaServiceError{message: "ollama not configured"}
	ErrOllamaDisabled      = &ollamaServiceError{message: "ollama integration disabled"}
	ErrInvalidRequest      = &ollamaServiceError{message: "invalid request"}
)

type ollamaServiceError struct {
	message string
}

func (e *ollamaServiceError) Error() string {
	return e.message
}
