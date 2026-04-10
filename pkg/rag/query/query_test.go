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

package query

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestDefaultOptions(t *testing.T) {
	opts := DefaultOptions()

	assert.Equal(t, 10, opts.TopK)
	assert.Equal(t, float32(0.7), opts.MinScore)
	assert.Equal(t, true, opts.Rerank)
	assert.Equal(t, true, opts.IncludeSource)
	assert.Equal(t, 20, opts.NProbe)
}

func TestOptions_Customization(t *testing.T) {
	opts := Options{
		TopK:          5,
		MinScore:      0.8,
		Rerank:        false,
		IncludeSource: false,
		NProbe:        10,
		Language:      "go",
		Filters: map[string]interface{}{
			"type": "code",
		},
	}

	assert.Equal(t, 5, opts.TopK)
	assert.Equal(t, float32(0.8), opts.MinScore)
	assert.Equal(t, false, opts.Rerank)
	assert.Equal(t, false, opts.IncludeSource)
	assert.Equal(t, 10, opts.NProbe)
	assert.Equal(t, "go", opts.Language)
	assert.Equal(t, "code", opts.Filters["type"])
}

func TestResponse_Fields(t *testing.T) {
	response := &Response{
		Query: "test query",
		Results: []Result{
			{
				ID:      "1",
				Score:   0.9,
				Content: "test content",
				Metadata: map[string]interface{}{
					"type": "code",
				},
			},
		},
		Context:        "formatted context",
		ProcessingTime: 100 * time.Millisecond,
		CacheHit:       true,
	}

	assert.Equal(t, "test query", response.Query)
	assert.Len(t, response.Results, 1)
	assert.Equal(t, "1", response.Results[0].ID)
	assert.Equal(t, float32(0.9), response.Results[0].Score)
	assert.Equal(t, "test content", response.Results[0].Content)
	assert.Equal(t, "formatted context", response.Context)
	assert.Equal(t, 100*time.Millisecond, response.ProcessingTime)
	assert.True(t, response.CacheHit)
}

func TestResult_Metadata(t *testing.T) {
	result := Result{
		ID:      "test-id",
		Score:   0.85,
		Content: "sample content",
		Metadata: map[string]interface{}{
			"source":   "test.go",
			"language": "go",
			"line":     42,
		},
	}

	assert.Equal(t, "test-id", result.ID)
	assert.Equal(t, float32(0.85), result.Score)
	assert.Equal(t, "sample content", result.Content)
	assert.Equal(t, "test.go", result.Metadata["source"])
	assert.Equal(t, "go", result.Metadata["language"])
	assert.Equal(t, 42, result.Metadata["line"])
}

func TestStreamChunk_Result(t *testing.T) {
	result := &Result{
		ID:    "1",
		Score: 0.9,
	}

	chunk := StreamChunk{
		Result: result,
		Done:   false,
		Error:  nil,
	}

	assert.NotNil(t, chunk.Result)
	assert.Equal(t, "1", chunk.Result.ID)
	assert.False(t, chunk.Done)
	assert.Nil(t, chunk.Error)
}

func TestStreamChunk_Done(t *testing.T) {
	chunk := StreamChunk{
		Result: nil,
		Done:   true,
		Error:  nil,
	}

	assert.Nil(t, chunk.Result)
	assert.True(t, chunk.Done)
	assert.Nil(t, chunk.Error)
}

func TestStreamChunk_Error(t *testing.T) {
	chunk := StreamChunk{
		Result: nil,
		Done:   false,
		Error:  ErrEmptyQuery,
	}

	assert.Nil(t, chunk.Result)
	assert.False(t, chunk.Done)
	assert.Equal(t, ErrEmptyQuery, chunk.Error)
}
