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

// Package main provides tests for embedding format functions with invalid
// output format to cover the ParseOutputFormat error paths.
package main

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/jeremyhahn/go-quicbaas/sdk/go/client/embedding"
)

func testWithInvalidEmbeddingFormat(t *testing.T, fn func() error) {
	t.Helper()
	saved := outputFormat
	defer func() { outputFormat = saved }()
	outputFormat = "INVALID_FORMAT_XYZ"

	err := fn()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "INVALID_FORMAT_XYZ")
}

func TestFormatEmbeddingGenerateResponse_InvalidFormat(t *testing.T) {
	testWithInvalidEmbeddingFormat(t, func() error {
		return formatEmbeddingGenerateResponse(&embedding.GenerateEmbeddingResponse{
			Embedding: []float32{0.1, 0.2},
			Timestamp: time.Now(),
		})
	})
}

func TestFormatEmbeddingBatchResponse_InvalidFormat(t *testing.T) {
	testWithInvalidEmbeddingFormat(t, func() error {
		return formatEmbeddingBatchResponse(&embedding.BatchGenerateEmbeddingsResponse{
			Results: []*embedding.BatchEmbeddingResult{
				{Embedding: []float32{0.1}},
				{Embedding: []float32{0.2}},
			},
			Timestamp: time.Now(),
		})
	})
}

func TestFormatEmbeddingProvidersResponse_InvalidFormat(t *testing.T) {
	testWithInvalidEmbeddingFormat(t, func() error {
		return formatEmbeddingProvidersResponse(&embedding.ListProvidersResponse{
			Providers: []*embedding.ProviderInfo{},
			Timestamp: time.Now(),
		})
	})
}

func TestFormatEmbeddingProviderInfoResponse_InvalidFormat(t *testing.T) {
	testWithInvalidEmbeddingFormat(t, func() error {
		return formatEmbeddingProviderInfoResponse(&embedding.GetProviderInfoResponse{
			Info:      &embedding.ProviderInfo{},
			Timestamp: time.Now(),
		})
	})
}

func TestFormatEmbeddingSetDefaultResponse_InvalidFormat(t *testing.T) {
	testWithInvalidEmbeddingFormat(t, func() error {
		return formatEmbeddingSetDefaultResponse(&embedding.SetDefaultProviderResponse{
			Provider:  embedding.ProviderVoyage,
			Timestamp: time.Now(),
		})
	})
}

func TestFormatEmbeddingCacheStatsResponse_InvalidFormat(t *testing.T) {
	testWithInvalidEmbeddingFormat(t, func() error {
		return formatEmbeddingCacheStatsResponse(&embedding.GetEmbeddingCacheStatsResponse{
			Timestamp: time.Now(),
		})
	})
}

func TestFormatEmbeddingCacheClearResponse_InvalidFormat(t *testing.T) {
	testWithInvalidEmbeddingFormat(t, func() error {
		return formatEmbeddingCacheClearResponse(&embedding.ClearEmbeddingCacheResponse{
			EntriesCleared: 10,
			Timestamp:      time.Now(),
		})
	})
}
