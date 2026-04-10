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
	"fmt"
	"sync"
)

// EmbeddingService generates embeddings using the Ollama API.
type EmbeddingService struct {
	client *Client
	model  string
}

// NewEmbeddingService creates a new embedding service.
func NewEmbeddingService(client *Client, model string) *EmbeddingService {
	return &EmbeddingService{
		client: client,
		model:  model,
	}
}

// Model returns the configured embedding model.
func (s *EmbeddingService) Model() string {
	return s.model
}

// SetModel changes the embedding model.
func (s *EmbeddingService) SetModel(model string) {
	s.model = model
}

// Embed generates an embedding for a single text.
func (s *EmbeddingService) Embed(ctx context.Context, text string) ([]float32, error) {
	if text == "" {
		return nil, &OllamaSentinelMessageError{Sentinel: ErrInvalidInput, Message: "text cannot be empty"}
	}

	return s.EmbedWithModel(ctx, s.model, text)
}

// EmbedWithModel generates an embedding for a single text using a specific model.
func (s *EmbeddingService) EmbedWithModel(ctx context.Context, model, text string) ([]float32, error) {
	if model == "" {
		return nil, &OllamaSentinelMessageError{Sentinel: ErrInvalidInput, Message: "model cannot be empty"}
	}
	if text == "" {
		return nil, &OllamaSentinelMessageError{Sentinel: ErrInvalidInput, Message: "text cannot be empty"}
	}

	req := EmbedRequest{
		Model:  model,
		Prompt: text,
	}

	respBody, err := s.client.doRequest(ctx, "POST", "/api/embeddings", req)
	if err != nil {
		return nil, err
	}

	var resp EmbedResponse
	if err := json.Unmarshal(respBody, &resp); err != nil {
		return nil, &OllamaSentinelCauseError{Sentinel: ErrInvalidResponse, Cause: err}
	}

	if len(resp.Embedding) == 0 {
		return nil, ErrEmptyResponse
	}

	return resp.Embedding, nil
}

// EmbedBatch generates embeddings for multiple texts.
// Note: Ollama's native API doesn't support batch embeddings,
// so we process texts concurrently client-side.
func (s *EmbeddingService) EmbedBatch(ctx context.Context, texts []string) ([][]float32, error) {
	return s.EmbedBatchWithModel(ctx, s.model, texts)
}

// EmbedBatchWithModel generates embeddings for multiple texts using a specific model.
func (s *EmbeddingService) EmbedBatchWithModel(ctx context.Context, model string, texts []string) ([][]float32, error) {
	if model == "" {
		return nil, &OllamaSentinelMessageError{Sentinel: ErrInvalidInput, Message: "model cannot be empty"}
	}
	if len(texts) == 0 {
		return nil, &OllamaSentinelMessageError{Sentinel: ErrInvalidInput, Message: "texts cannot be empty"}
	}

	// Validate all texts
	for i, text := range texts {
		if text == "" {
			return nil, &OllamaSentinelMessageError{
				Sentinel: ErrInvalidInput,
				Message:  fmt.Sprintf("text at index %d is empty", i),
			}
		}
	}

	// For small batches, process sequentially
	if len(texts) <= 3 {
		return s.embedBatchSequential(ctx, model, texts)
	}

	// For larger batches, process concurrently
	return s.embedBatchConcurrent(ctx, model, texts)
}

// embedBatchSequential processes texts sequentially.
func (s *EmbeddingService) embedBatchSequential(ctx context.Context, model string, texts []string) ([][]float32, error) {
	embeddings := make([][]float32, len(texts))

	for i, text := range texts {
		embedding, err := s.EmbedWithModel(ctx, model, text)
		if err != nil {
			return nil, &OllamaEmbedIndexError{Index: i, Cause: err}
		}
		embeddings[i] = embedding
	}

	return embeddings, nil
}

// embedBatchConcurrent processes texts concurrently with limited parallelism.
func (s *EmbeddingService) embedBatchConcurrent(ctx context.Context, model string, texts []string) ([][]float32, error) {
	const maxConcurrency = 5

	embeddings := make([][]float32, len(texts))
	errs := make([]error, len(texts))

	var wg sync.WaitGroup
	sem := make(chan struct{}, maxConcurrency)

	for i, text := range texts {
		wg.Add(1)
		go func(idx int, txt string) {
			defer wg.Done()

			// Acquire semaphore
			select {
			case <-ctx.Done():
				errs[idx] = ctx.Err()
				return
			case sem <- struct{}{}:
				defer func() { <-sem }()
			}

			embedding, err := s.EmbedWithModel(ctx, model, txt)
			if err != nil {
				errs[idx] = err
				return
			}
			embeddings[idx] = embedding
		}(i, text)
	}

	wg.Wait()

	// Check for errors
	for i, err := range errs {
		if err != nil {
			return nil, &OllamaEmbedIndexError{Index: i, Cause: err}
		}
	}

	return embeddings, nil
}
