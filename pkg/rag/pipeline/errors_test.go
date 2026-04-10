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

package pipeline

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestPipelineErrorWrapping(t *testing.T) {
	tests := []struct {
		name      string
		err       error
		component string
		operation string
		details   map[string]interface{}
		wantMsg   string
	}{
		{
			name:      "basic error",
			err:       errors.New("test error"),
			component: "query_handler",
			operation: "process",
			details:   nil,
			wantMsg:   "query_handler failed during process: test error",
		},
		{
			name:      "error with details",
			err:       errors.New("connection failed"),
			component: "vector_search",
			operation: "search",
			details: map[string]interface{}{
				"namespace": "default",
				"topK":      10,
			},
			wantMsg: "vector_search failed during search: connection failed",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pErr := NewPipelineError(tt.err, tt.component, tt.operation, tt.details)
			assert.Error(t, pErr)
			assert.Contains(t, pErr.Error(), tt.wantMsg)

			// Test unwrapping
			assert.True(t, errors.Is(pErr, tt.err))

			// Verify details are accessible
			if pipelineErr, ok := pErr.(*PipelineError); ok {
				assert.Equal(t, tt.component, pipelineErr.Component)
				assert.Equal(t, tt.operation, pipelineErr.Operation)
				if tt.details != nil {
					assert.Equal(t, tt.details, pipelineErr.Details)
				}
			}
		})
	}
}

func TestErrorTypes(t *testing.T) {
	tests := []struct {
		name string
		err  error
	}{
		{"ErrPipelineNotInitialized", ErrPipelineNotInitialized},
		{"ErrPipelineClosed", ErrPipelineClosed},
		{"ErrQueryTimeout", ErrQueryTimeout},
		{"ErrInvalidQuery", ErrInvalidQuery},
		{"ErrNoResults", ErrNoResults},
		{"ErrQueryHandlerFailed", ErrQueryHandlerFailed},
		{"ErrVectorSearchFailed", ErrVectorSearchFailed},
		{"ErrKeywordSearchFailed", ErrKeywordSearchFailed},
		{"ErrRankingFailed", ErrRankingFailed},
		{"ErrRerankingFailed", ErrRerankingFailed},
		{"ErrContextBuildFailed", ErrContextBuildFailed},
		{"ErrConcurrencyLimitReached", ErrConcurrencyLimitReached},
		{"ErrCircuitBreakerOpen", ErrCircuitBreakerOpen},
		{"ErrResourceExhausted", ErrResourceExhausted},
		{"ErrInvalidConfiguration", ErrInvalidConfiguration},
		{"ErrMissingDependency", ErrMissingDependency},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.NotNil(t, tt.err)
			assert.NotEmpty(t, tt.err.Error())
		})
	}
}

func TestErrorComparison(t *testing.T) {
	// Test that sentinel errors are comparable
	err1 := ErrPipelineClosed
	err2 := ErrPipelineClosed
	assert.True(t, errors.Is(err1, err2))

	// Test that wrapped errors preserve identity
	wrapped := NewPipelineError(ErrQueryTimeout, "test", "op", nil)
	assert.True(t, errors.Is(wrapped, ErrQueryTimeout))
}
