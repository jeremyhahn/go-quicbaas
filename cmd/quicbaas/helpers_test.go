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

package main

import (
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/client/vector"
)

func TestLoggerAdapterWithError(t *testing.T) {
	cfg := logger.LogConfig{
		Level:  "info",
		Format: "json",
		Output: "stdout",
	}
	baseLogger, err := logger.New(cfg)
	require.NoError(t, err)

	adapter := &loggerAdapter{Logger: baseLogger}
	testErr := errors.New("test error")
	result := adapter.WithError(testErr)

	assert.NotNil(t, result)
	_, ok := result.(interfaces.Logger)
	assert.True(t, ok, "WithError should return interfaces.Logger")

	resultAdapter, ok := result.(*loggerAdapter)
	assert.True(t, ok, "WithError should return *loggerAdapter")
	assert.NotNil(t, resultAdapter.Logger)
}

// TestLoggerAdapterWithField tests the WithField method.
func TestLoggerAdapterWithField(t *testing.T) {
	cfg := logger.LogConfig{
		Level:  "info",
		Format: "json",
		Output: "stdout",
	}
	baseLogger, err := logger.New(cfg)
	require.NoError(t, err)

	adapter := &loggerAdapter{Logger: baseLogger}
	result := adapter.WithField("key", "value")

	assert.NotNil(t, result)
	_, ok := result.(interfaces.Logger)
	assert.True(t, ok, "WithField should return interfaces.Logger")

	resultAdapter, ok := result.(*loggerAdapter)
	assert.True(t, ok, "WithField should return *loggerAdapter")
	assert.NotNil(t, resultAdapter.Logger)
}

// TestLoggerAdapterWithFields tests the WithFields method.
func TestLoggerAdapterWithFields(t *testing.T) {
	cfg := logger.LogConfig{
		Level:  "info",
		Format: "json",
		Output: "stdout",
	}
	baseLogger, err := logger.New(cfg)
	require.NoError(t, err)

	adapter := &loggerAdapter{Logger: baseLogger}
	fields := map[string]interface{}{
		"key1": "value1",
		"key2": 123,
		"key3": true,
	}
	result := adapter.WithFields(fields)

	assert.NotNil(t, result)
	_, ok := result.(interfaces.Logger)
	assert.True(t, ok, "WithFields should return interfaces.Logger")

	resultAdapter, ok := result.(*loggerAdapter)
	assert.True(t, ok, "WithFields should return *loggerAdapter")
	assert.NotNil(t, resultAdapter.Logger)
}

// TestAsInterfaceLogger tests the conversion function.
func TestAsInterfaceLogger(t *testing.T) {
	cfg := logger.LogConfig{
		Level:  "info",
		Format: "json",
		Output: "stdout",
	}
	baseLogger, err := logger.New(cfg)
	require.NoError(t, err)

	interfaceLogger := asInterfaceLogger(baseLogger)

	assert.NotNil(t, interfaceLogger)
	_, ok := interfaceLogger.(interfaces.Logger)
	assert.True(t, ok, "asInterfaceLogger should return interfaces.Logger")

	adapter, ok := interfaceLogger.(*loggerAdapter)
	assert.True(t, ok, "asInterfaceLogger should return *loggerAdapter")
	assert.Equal(t, baseLogger, adapter.Logger)
}

// TestLoggerAdapterChaining tests method chaining.
func TestLoggerAdapterChaining(t *testing.T) {
	cfg := logger.LogConfig{
		Level:  "debug",
		Format: "console",
		Output: "stdout",
	}
	baseLogger, err := logger.New(cfg)
	require.NoError(t, err)

	adapter := asInterfaceLogger(baseLogger)
	result := adapter.
		WithField("request_id", "123").
		WithField("user_id", "456").
		WithError(errors.New("test error"))

	assert.NotNil(t, result)
	_, ok := result.(interfaces.Logger)
	assert.True(t, ok, "Chained operations should return interfaces.Logger")
}

// TestLoggerAdapterWithEmptyFields tests edge case of empty fields map.
func TestLoggerAdapterWithEmptyFields(t *testing.T) {
	cfg := logger.LogConfig{
		Level:  "info",
		Format: "json",
		Output: "stdout",
	}
	baseLogger, err := logger.New(cfg)
	require.NoError(t, err)

	adapter := &loggerAdapter{Logger: baseLogger}
	result := adapter.WithFields(map[string]interface{}{})

	assert.NotNil(t, result)
	_, ok := result.(interfaces.Logger)
	assert.True(t, ok)
}

// TestLoggerAdapterWithNilError tests edge case of nil error.
func TestLoggerAdapterWithNilError(t *testing.T) {
	cfg := logger.LogConfig{
		Level:  "info",
		Format: "json",
		Output: "stdout",
	}
	baseLogger, err := logger.New(cfg)
	require.NoError(t, err)

	adapter := &loggerAdapter{Logger: baseLogger}
	result := adapter.WithError(nil)

	assert.NotNil(t, result)
	_, ok := result.(interfaces.Logger)
	assert.True(t, ok)
}

// TestLoggerAdapterMultipleFieldTypes tests various data types as field values.
func TestLoggerAdapterMultipleFieldTypes(t *testing.T) {
	cfg := logger.LogConfig{
		Level:  "info",
		Format: "json",
		Output: "stdout",
	}
	baseLogger, err := logger.New(cfg)
	require.NoError(t, err)

	adapter := asInterfaceLogger(baseLogger)

	tests := []struct {
		name  string
		key   string
		value interface{}
	}{
		{"string", "str_field", "test"},
		{"int", "int_field", 42},
		{"float", "float_field", 3.14},
		{"bool", "bool_field", true},
		{"nil", "nil_field", nil},
		{"slice", "slice_field", []string{"a", "b"}},
		{"map", "map_field", map[string]string{"k": "v"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := adapter.WithField(tt.key, tt.value)
			assert.NotNil(t, result)
			_, ok := result.(interfaces.Logger)
			assert.True(t, ok)
		})
	}
}

func TestFormatVectorUpsertResponse(t *testing.T) {
	tests := []struct {
		name         string
		response     *vector.UpsertResponse
		outputFormat string
		expectError  bool
	}{
		{
			name: "success table format",
			response: &vector.UpsertResponse{
				ID:        "vec-123",
				Success:   true,
				Version:   1,
				Timestamp: time.Now(),
			},
			outputFormat: "table",
			expectError:  false,
		},
		{
			name: "success json format",
			response: &vector.UpsertResponse{
				ID:        "vec-123",
				Success:   true,
				Version:   1,
				Timestamp: time.Now(),
			},
			outputFormat: "json",
			expectError:  false,
		},
		{
			name: "failure response",
			response: &vector.UpsertResponse{
				ID:        "vec-456",
				Success:   false,
				Version:   0,
				Timestamp: time.Now(),
			},
			outputFormat: "table",
			expectError:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			outputFormat = tt.outputFormat
			err := formatVectorUpsertResponse(tt.response)
			if tt.expectError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestFormatVectorUpsertResponseInvalidFormat(t *testing.T) {
	outputFormat = "invalid-format"
	resp := &vector.UpsertResponse{
		ID:        "vec-123",
		Success:   true,
		Timestamp: time.Now(),
	}
	err := formatVectorUpsertResponse(resp)
	assert.Error(t, err)
}

func TestFormatVectorBatchUpsertResponse(t *testing.T) {
	tests := []struct {
		name         string
		response     *vector.BatchUpsertResponse
		outputFormat string
		expectError  bool
	}{
		{
			name: "all succeeded table format",
			response: &vector.BatchUpsertResponse{
				VectorsSucceeded: 10,
				VectorsFailed:    0,
				Timestamp:        time.Now(),
			},
			outputFormat: "table",
			expectError:  false,
		},
		{
			name: "partial failure table format",
			response: &vector.BatchUpsertResponse{
				VectorsSucceeded: 8,
				VectorsFailed:    2,
				Timestamp:        time.Now(),
			},
			outputFormat: "table",
			expectError:  false,
		},
		{
			name: "json format",
			response: &vector.BatchUpsertResponse{
				VectorsSucceeded: 10,
				VectorsFailed:    0,
				Timestamp:        time.Now(),
			},
			outputFormat: "json",
			expectError:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			outputFormat = tt.outputFormat
			err := formatVectorBatchUpsertResponse(tt.response)
			if tt.expectError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestFormatVectorBatchUpsertResponseInvalidFormat(t *testing.T) {
	outputFormat = "invalid-format"
	resp := &vector.BatchUpsertResponse{
		VectorsSucceeded: 10,
		VectorsFailed:    0,
		Timestamp:        time.Now(),
	}
	err := formatVectorBatchUpsertResponse(resp)
	assert.Error(t, err)
}

func TestFormatVectorGetResponse(t *testing.T) {
	tests := []struct {
		name         string
		response     *vector.GetResponse
		outputFormat string
		expectError  bool
	}{
		{
			name: "found vector table format",
			response: &vector.GetResponse{
				Vector: &vector.VectorEntity{
					ID:        "vec-123",
					Namespace: "test-ns",
					Vector:    []float32{0.1, 0.2, 0.3, 0.4, 0.5, 0.6, 0.7},
					Metadata:  map[string]string{"key": "value"},
					Version:   1,
					CreatedAt: time.Now(),
					UpdatedAt: time.Now(),
				},
				Found:     true,
				Timestamp: time.Now(),
			},
			outputFormat: "table",
			expectError:  false,
		},
		{
			name: "not found table format",
			response: &vector.GetResponse{
				Vector:    nil,
				Found:     false,
				Timestamp: time.Now(),
			},
			outputFormat: "table",
			expectError:  false,
		},
		{
			name: "found vector json format",
			response: &vector.GetResponse{
				Vector: &vector.VectorEntity{
					ID:        "vec-123",
					Namespace: "test-ns",
					Vector:    []float32{0.1, 0.2, 0.3},
				},
				Found:     true,
				Timestamp: time.Now(),
			},
			outputFormat: "json",
			expectError:  false,
		},
		{
			name: "empty vector data",
			response: &vector.GetResponse{
				Vector: &vector.VectorEntity{
					ID:        "vec-123",
					Namespace: "test-ns",
					Vector:    []float32{},
				},
				Found:     true,
				Timestamp: time.Now(),
			},
			outputFormat: "table",
			expectError:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			outputFormat = tt.outputFormat
			err := formatVectorGetResponse(tt.response)
			if tt.expectError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestFormatVectorGetResponseInvalidFormat(t *testing.T) {
	outputFormat = "invalid-format"
	resp := &vector.GetResponse{
		Vector: &vector.VectorEntity{
			ID: "vec-123",
		},
		Found:     true,
		Timestamp: time.Now(),
	}
	err := formatVectorGetResponse(resp)
	assert.Error(t, err)
}

func TestFormatVectorDeleteResponse(t *testing.T) {
	tests := []struct {
		name         string
		response     *vector.DeleteResponse
		outputFormat string
		expectError  bool
	}{
		{
			name: "deleted table format",
			response: &vector.DeleteResponse{
				Deleted:   true,
				Timestamp: time.Now(),
			},
			outputFormat: "table",
			expectError:  false,
		},
		{
			name: "not deleted table format",
			response: &vector.DeleteResponse{
				Deleted:   false,
				Timestamp: time.Now(),
			},
			outputFormat: "table",
			expectError:  false,
		},
		{
			name: "json format",
			response: &vector.DeleteResponse{
				Deleted:   true,
				Timestamp: time.Now(),
			},
			outputFormat: "json",
			expectError:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			outputFormat = tt.outputFormat
			err := formatVectorDeleteResponse(tt.response)
			if tt.expectError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestFormatVectorDeleteResponseInvalidFormat(t *testing.T) {
	outputFormat = "invalid-format"
	resp := &vector.DeleteResponse{
		Deleted:   true,
		Timestamp: time.Now(),
	}
	err := formatVectorDeleteResponse(resp)
	assert.Error(t, err)
}

func TestFormatVectorBatchDeleteResponse(t *testing.T) {
	tests := []struct {
		name         string
		response     *vector.BatchDeleteResponse
		outputFormat string
		expectError  bool
	}{
		{
			name: "all deleted table format",
			response: &vector.BatchDeleteResponse{
				DeletedCount: 10,
				Errors:       map[string]string{},
				Timestamp:    time.Now(),
			},
			outputFormat: "table",
			expectError:  false,
		},
		{
			name: "partial failure table format",
			response: &vector.BatchDeleteResponse{
				DeletedCount: 8,
				Errors:       map[string]string{"vec-1": "not found", "vec-2": "permission denied"},
				Timestamp:    time.Now(),
			},
			outputFormat: "table",
			expectError:  false,
		},
		{
			name: "json format",
			response: &vector.BatchDeleteResponse{
				DeletedCount: 10,
				Errors:       map[string]string{},
				Timestamp:    time.Now(),
			},
			outputFormat: "json",
			expectError:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			outputFormat = tt.outputFormat
			err := formatVectorBatchDeleteResponse(tt.response)
			if tt.expectError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestFormatVectorBatchDeleteResponseInvalidFormat(t *testing.T) {
	outputFormat = "invalid-format"
	resp := &vector.BatchDeleteResponse{
		DeletedCount: 10,
		Timestamp:    time.Now(),
	}
	err := formatVectorBatchDeleteResponse(resp)
	assert.Error(t, err)
}

func TestFormatVectorListResponse(t *testing.T) {
	tests := []struct {
		name         string
		response     *vector.ListVectorsResponse
		outputFormat string
		expectError  bool
	}{
		{
			name: "with vectors table format",
			response: &vector.ListVectorsResponse{
				Vectors: []*vector.VectorEntity{
					{ID: "vec-1", Version: 1, CreatedAt: time.Now(), UpdatedAt: time.Now()},
					{ID: "vec-2", Version: 2, CreatedAt: time.Now(), UpdatedAt: time.Now()},
				},
				TotalCount:    2,
				NextPageToken: "",
				Timestamp:     time.Now(),
			},
			outputFormat: "table",
			expectError:  false,
		},
		{
			name: "with pagination table format",
			response: &vector.ListVectorsResponse{
				Vectors: []*vector.VectorEntity{
					{ID: "vec-1", Version: 1, CreatedAt: time.Now(), UpdatedAt: time.Now()},
				},
				TotalCount:    100,
				NextPageToken: "next-token-123",
				Timestamp:     time.Now(),
			},
			outputFormat: "table",
			expectError:  false,
		},
		{
			name: "json format",
			response: &vector.ListVectorsResponse{
				Vectors: []*vector.VectorEntity{
					{ID: "vec-1", Version: 1, CreatedAt: time.Now(), UpdatedAt: time.Now()},
				},
				TotalCount: 1,
				Timestamp:  time.Now(),
			},
			outputFormat: "json",
			expectError:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			outputFormat = tt.outputFormat
			err := formatVectorListResponse(tt.response)
			if tt.expectError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestFormatVectorListResponseInvalidFormat(t *testing.T) {
	outputFormat = "invalid-format"
	resp := &vector.ListVectorsResponse{
		Vectors:    []*vector.VectorEntity{},
		TotalCount: 0,
		Timestamp:  time.Now(),
	}
	err := formatVectorListResponse(resp)
	assert.Error(t, err)
}

func TestFormatVectorSearchSimilarResponse(t *testing.T) {
	tests := []struct {
		name         string
		response     *vector.SearchSimilarResponse
		outputFormat string
		expectError  bool
	}{
		{
			name: "with results table format",
			response: &vector.SearchSimilarResponse{
				Results: []*vector.SearchResult{
					{
						Vector:   &vector.VectorEntity{ID: "vec-1", Metadata: map[string]string{"key": "value"}},
						Score:    0.95,
						Distance: 0.05,
						Rank:     1,
					},
					{
						Vector:   &vector.VectorEntity{ID: "vec-2", Metadata: map[string]string{"key": "value2"}},
						Score:    0.85,
						Distance: 0.15,
						Rank:     2,
					},
				},
				SearchTimeMs: 25,
				Timestamp:    time.Now(),
			},
			outputFormat: "table",
			expectError:  false,
		},
		{
			name: "empty results table format",
			response: &vector.SearchSimilarResponse{
				Results:      []*vector.SearchResult{},
				SearchTimeMs: 10,
				Timestamp:    time.Now(),
			},
			outputFormat: "table",
			expectError:  false,
		},
		{
			name: "json format",
			response: &vector.SearchSimilarResponse{
				Results: []*vector.SearchResult{
					{Vector: &vector.VectorEntity{ID: "vec-1"}, Score: 0.95},
				},
				SearchTimeMs: 25,
				Timestamp:    time.Now(),
			},
			outputFormat: "json",
			expectError:  false,
		},
		{
			name: "result with nil vector",
			response: &vector.SearchSimilarResponse{
				Results: []*vector.SearchResult{
					{Vector: nil, Score: 0.95, Rank: 1},
				},
				SearchTimeMs: 25,
				Timestamp:    time.Now(),
			},
			outputFormat: "table",
			expectError:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			outputFormat = tt.outputFormat
			err := formatVectorSearchSimilarResponse(tt.response)
			if tt.expectError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestFormatVectorSearchSimilarResponseInvalidFormat(t *testing.T) {
	outputFormat = "invalid-format"
	resp := &vector.SearchSimilarResponse{
		Results:      []*vector.SearchResult{},
		SearchTimeMs: 10,
		Timestamp:    time.Now(),
	}
	err := formatVectorSearchSimilarResponse(resp)
	assert.Error(t, err)
}

func TestFormatVectorSearchByIDResponse(t *testing.T) {
	tests := []struct {
		name         string
		response     *vector.SearchByIDResponse
		outputFormat string
		expectError  bool
	}{
		{
			name: "with results table format",
			response: &vector.SearchByIDResponse{
				Results: []*vector.SearchResult{
					{
						Vector:   &vector.VectorEntity{ID: "vec-1", Metadata: map[string]string{"key": "value"}},
						Score:    0.95,
						Distance: 0.05,
						Rank:     1,
					},
				},
				SearchTimeMs: 25,
				Timestamp:    time.Now(),
			},
			outputFormat: "table",
			expectError:  false,
		},
		{
			name: "json format",
			response: &vector.SearchByIDResponse{
				Results:      []*vector.SearchResult{},
				SearchTimeMs: 10,
				Timestamp:    time.Now(),
			},
			outputFormat: "json",
			expectError:  false,
		},
		{
			name: "result with nil vector",
			response: &vector.SearchByIDResponse{
				Results: []*vector.SearchResult{
					{Vector: nil, Score: 0.95, Rank: 1},
				},
				SearchTimeMs: 25,
				Timestamp:    time.Now(),
			},
			outputFormat: "table",
			expectError:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			outputFormat = tt.outputFormat
			err := formatVectorSearchByIDResponse(tt.response)
			if tt.expectError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestFormatVectorSearchByIDResponseInvalidFormat(t *testing.T) {
	outputFormat = "invalid-format"
	resp := &vector.SearchByIDResponse{
		Results:      []*vector.SearchResult{},
		SearchTimeMs: 10,
		Timestamp:    time.Now(),
	}
	err := formatVectorSearchByIDResponse(resp)
	assert.Error(t, err)
}

func TestFormatVectorIndexRebuildResponse(t *testing.T) {
	tests := []struct {
		name         string
		response     *vector.RebuildIndexResponse
		outputFormat string
		expectError  bool
	}{
		{
			name: "started table format",
			response: &vector.RebuildIndexResponse{
				OperationID:         "op-123",
				Status:              "started",
				EstimatedDurationMs: 5000,
				Started:             time.Now(),
				Timestamp:           time.Now(),
			},
			outputFormat: "table",
			expectError:  false,
		},
		{
			name: "no estimated duration",
			response: &vector.RebuildIndexResponse{
				OperationID:         "op-456",
				Status:              "started",
				EstimatedDurationMs: 0,
				Started:             time.Now(),
				Timestamp:           time.Now(),
			},
			outputFormat: "table",
			expectError:  false,
		},
		{
			name: "json format",
			response: &vector.RebuildIndexResponse{
				OperationID: "op-789",
				Status:      "started",
				Timestamp:   time.Now(),
			},
			outputFormat: "json",
			expectError:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			outputFormat = tt.outputFormat
			err := formatVectorIndexRebuildResponse(tt.response)
			if tt.expectError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestFormatVectorIndexRebuildResponseInvalidFormat(t *testing.T) {
	outputFormat = "invalid-format"
	resp := &vector.RebuildIndexResponse{
		OperationID: "op-123",
		Status:      "started",
		Timestamp:   time.Now(),
	}
	err := formatVectorIndexRebuildResponse(resp)
	assert.Error(t, err)
}

func TestFormatVectorIndexProgressResponse(t *testing.T) {
	tests := []struct {
		name         string
		response     *vector.GetIndexProgressResponse
		outputFormat string
		expectError  bool
	}{
		{
			name: "in progress table format",
			response: &vector.GetIndexProgressResponse{
				OperationID:          "op-123",
				Namespace:            "test-ns",
				Status:               "in_progress",
				PercentComplete:      45.5,
				VectorsIndexed:       455,
				TotalVectors:         1000,
				ElapsedTimeMs:        10000,
				EstimatedRemainingMs: 12000,
				Timestamp:            time.Now(),
			},
			outputFormat: "table",
			expectError:  false,
		},
		{
			name: "with error message",
			response: &vector.GetIndexProgressResponse{
				OperationID:          "op-456",
				Namespace:            "test-ns",
				Status:               "failed",
				PercentComplete:      0,
				VectorsIndexed:       0,
				TotalVectors:         1000,
				ElapsedTimeMs:        0,
				EstimatedRemainingMs: 0,
				ErrorMessage:         "index build failed",
				Timestamp:            time.Now(),
			},
			outputFormat: "table",
			expectError:  false,
		},
		{
			name: "json format",
			response: &vector.GetIndexProgressResponse{
				OperationID:     "op-789",
				Namespace:       "test-ns",
				Status:          "completed",
				PercentComplete: 100,
				Timestamp:       time.Now(),
			},
			outputFormat: "json",
			expectError:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			outputFormat = tt.outputFormat
			err := formatVectorIndexProgressResponse(tt.response)
			if tt.expectError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestFormatVectorIndexProgressResponseInvalidFormat(t *testing.T) {
	outputFormat = "invalid-format"
	resp := &vector.GetIndexProgressResponse{
		OperationID: "op-123",
		Namespace:   "test-ns",
		Status:      "in_progress",
		Timestamp:   time.Now(),
	}
	err := formatVectorIndexProgressResponse(resp)
	assert.Error(t, err)
}

func TestFormatVectorIndexStatsResponse(t *testing.T) {
	tests := []struct {
		name         string
		response     *vector.GetStatsResponse
		outputFormat string
		expectError  bool
	}{
		{
			name: "table format",
			response: &vector.GetStatsResponse{
				Namespace:        "test-ns",
				VectorCount:      10000,
				IndexSizeBytes:   1024 * 1024 * 10, // 10 MB
				StorageSizeBytes: 1024 * 1024 * 20, // 20 MB
			},
			outputFormat: "table",
			expectError:  false,
		},
		{
			name: "json format",
			response: &vector.GetStatsResponse{
				Namespace:        "test-ns",
				VectorCount:      10000,
				IndexSizeBytes:   1024 * 1024,
				StorageSizeBytes: 1024 * 1024 * 2,
			},
			outputFormat: "json",
			expectError:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			outputFormat = tt.outputFormat
			err := formatVectorIndexStatsResponse(tt.response)
			if tt.expectError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestFormatVectorIndexStatsResponseInvalidFormat(t *testing.T) {
	outputFormat = "invalid-format"
	resp := &vector.GetStatsResponse{
		Namespace:   "test-ns",
		VectorCount: 10000,
	}
	err := formatVectorIndexStatsResponse(resp)
	assert.Error(t, err)
}

func TestFormatVectorUpdateMetadataResponse(t *testing.T) {
	tests := []struct {
		name         string
		response     *vector.UpdateMetadataResponse
		outputFormat string
		expectError  bool
	}{
		{
			name: "success table format",
			response: &vector.UpdateMetadataResponse{
				Success:   true,
				Version:   1,
				Timestamp: time.Now(),
			},
			outputFormat: "table",
			expectError:  false,
		},
		{
			name: "failure table format",
			response: &vector.UpdateMetadataResponse{
				Success:   false,
				Version:   0,
				Timestamp: time.Now(),
			},
			outputFormat: "table",
			expectError:  false,
		},
		{
			name: "json format",
			response: &vector.UpdateMetadataResponse{
				Success:   true,
				Version:   2,
				Timestamp: time.Now(),
			},
			outputFormat: "json",
			expectError:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			outputFormat = tt.outputFormat
			err := formatVectorUpdateMetadataResponse(tt.response)
			if tt.expectError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestFormatVectorUpdateMetadataResponseFallback(t *testing.T) {
	outputFormat = "invalid-format"
	resp := &vector.UpdateMetadataResponse{
		Success:   true,
		Version:   1,
		Timestamp: time.Now(),
	}
	err := formatVectorUpdateMetadataResponse(resp)
	assert.NoError(t, err)
}

func TestFormatVectorHybridSearchResponse(t *testing.T) {
	tests := []struct {
		name         string
		response     *vector.HybridSearchResponse
		outputFormat string
		expectError  bool
	}{
		{
			name: "with results table format",
			response: &vector.HybridSearchResponse{
				Results: []*vector.SearchResult{
					{
						Vector:   &vector.VectorEntity{ID: "vec-1", Metadata: map[string]string{"key": "value"}},
						Score:    0.95,
						Distance: 0.05,
						Rank:     1,
					},
				},
				SearchTimeMs: 25,
				Timestamp:    time.Now(),
			},
			outputFormat: "table",
			expectError:  false,
		},
		{
			name: "json format",
			response: &vector.HybridSearchResponse{
				Results:      []*vector.SearchResult{},
				SearchTimeMs: 10,
				Timestamp:    time.Now(),
			},
			outputFormat: "json",
			expectError:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			outputFormat = tt.outputFormat
			err := formatVectorHybridSearchResponse(tt.response)
			if tt.expectError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestFormatVectorHybridSearchResponseFallback(t *testing.T) {
	outputFormat = "invalid-format"
	resp := &vector.HybridSearchResponse{
		Results:      []*vector.SearchResult{},
		SearchTimeMs: 10,
		Timestamp:    time.Now(),
	}
	err := formatVectorHybridSearchResponse(resp)
	assert.NoError(t, err)
}

func TestFormatVectorSearchByMetadataResponse(t *testing.T) {
	tests := []struct {
		name         string
		response     *vector.SearchByMetadataResponse
		outputFormat string
		expectError  bool
	}{
		{
			name: "with results table format",
			response: &vector.SearchByMetadataResponse{
				Results: []*vector.VectorEntity{
					{ID: "vec-1", Metadata: map[string]string{"key": "value"}, Version: 1, CreatedAt: time.Now()},
				},
				TotalCount:   1,
				SearchTimeMs: 25,
				Timestamp:    time.Now(),
			},
			outputFormat: "table",
			expectError:  false,
		},
		{
			name: "json format",
			response: &vector.SearchByMetadataResponse{
				Results:      []*vector.VectorEntity{},
				TotalCount:   0,
				SearchTimeMs: 10,
				Timestamp:    time.Now(),
			},
			outputFormat: "json",
			expectError:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			outputFormat = tt.outputFormat
			err := formatVectorSearchByMetadataResponse(tt.response)
			if tt.expectError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestFormatVectorSearchByMetadataResponseFallback(t *testing.T) {
	outputFormat = "invalid-format"
	resp := &vector.SearchByMetadataResponse{
		Results:    []*vector.VectorEntity{},
		TotalCount: 0,
		Timestamp:  time.Now(),
	}
	err := formatVectorSearchByMetadataResponse(resp)
	assert.NoError(t, err)
}

// =============================================================================
// MCP KMS Parsing Function Tests
// =============================================================================

func TestParseKMSImportKeyResponse(t *testing.T) {
	result := map[string]interface{}{
		"key_id":    "imported-key-123",
		"algorithm": "AES-256-GCM",
		"status":    "active",
	}

	resp, err := parseKMSImportKeyResponse(result)
	require.NoError(t, err)
	assert.Equal(t, "imported-key-123", resp.Key.KeyID)
	assert.Equal(t, "AES-256-GCM", string(resp.Key.Algorithm))
	assert.Equal(t, "active", string(resp.Key.Status))
}

func TestParseKMSImportKeyResponseInvalidData(t *testing.T) {
	// Test with nil
	resp, err := parseKMSImportKeyResponse(nil)
	require.NoError(t, err)
	assert.Equal(t, "", resp.Key.KeyID)

	// Test with empty map
	resp, err = parseKMSImportKeyResponse(map[string]interface{}{})
	require.NoError(t, err)
	assert.Equal(t, "", resp.Key.KeyID)
}

func TestParseKMSGetKeyResponse(t *testing.T) {
	tests := []struct {
		name   string
		result map[string]interface{}
	}{
		{
			name: "flat structure",
			result: map[string]interface{}{
				"key_id":     "key-123",
				"algorithm":  "AES-256-GCM",
				"status":     "active",
				"version":    float64(1),
				"created_at": float64(time.Now().Unix()),
			},
		},
		{
			name: "nested metadata",
			result: map[string]interface{}{
				"metadata": map[string]interface{}{
					"key_id":     "key-456",
					"algorithm":  "CHACHA20-POLY1305",
					"status":     "disabled",
					"version":    float64(2),
					"created_at": float64(time.Now().Unix()),
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resp, err := parseKMSGetKeyResponse(tt.result)
			require.NoError(t, err)
			assert.NotNil(t, resp.Key)
			assert.NotNil(t, resp.Metadata)
		})
	}
}

func TestParseKMSGetKeyResponseInvalidData(t *testing.T) {
	resp, err := parseKMSGetKeyResponse(nil)
	require.NoError(t, err)
	assert.Equal(t, "", resp.Key.KeyID)
}

func TestParseKMSRotateKeyResponse(t *testing.T) {
	result := map[string]interface{}{
		"key_id":      "key-123",
		"old_version": float64(1),
		"new_version": float64(2),
	}

	resp, err := parseKMSRotateKeyResponse(result)
	require.NoError(t, err)
	assert.Equal(t, "key-123", resp.KeyID)
	assert.Equal(t, uint64(1), resp.OldVersion)
	assert.Equal(t, uint64(2), resp.NewVersion)
}

func TestParseKMSRotateKeyResponseInvalidData(t *testing.T) {
	resp, err := parseKMSRotateKeyResponse(nil)
	require.NoError(t, err)
	assert.Equal(t, "", resp.KeyID)
}

func TestParseKMSDeleteKeyResponse(t *testing.T) {
	result := map[string]interface{}{
		"key_id": "key-123",
		"status": "deleted",
	}

	resp, err := parseKMSDeleteKeyResponse(result)
	require.NoError(t, err)
	assert.Equal(t, "key-123", resp.KeyID)
	assert.Equal(t, "deleted", string(resp.Status))
}

func TestParseKMSDeleteKeyResponseInvalidData(t *testing.T) {
	resp, err := parseKMSDeleteKeyResponse(nil)
	require.NoError(t, err)
	assert.Equal(t, "", resp.KeyID)
}

func TestParseKMSUpdateKeyStatusResponse(t *testing.T) {
	result := map[string]interface{}{
		"key_id":     "key-123",
		"old_status": "active",
		"new_status": "disabled",
	}

	resp, err := parseKMSUpdateKeyStatusResponse(result)
	require.NoError(t, err)
	assert.Equal(t, "key-123", resp.KeyID)
	assert.Equal(t, "active", string(resp.OldStatus))
	assert.Equal(t, "disabled", string(resp.NewStatus))
}

func TestParseKMSUpdateKeyStatusResponseInvalidData(t *testing.T) {
	resp, err := parseKMSUpdateKeyStatusResponse(nil)
	require.NoError(t, err)
	assert.Equal(t, "", resp.KeyID)
}

func TestParseKMSListKeyVersionsResponse(t *testing.T) {
	result := map[string]interface{}{
		"versions": []interface{}{
			map[string]interface{}{
				"version":    float64(1),
				"status":     "active",
				"algorithm":  "AES-256-GCM",
				"created_at": float64(time.Now().Unix()),
			},
			map[string]interface{}{
				"version":    float64(2),
				"status":     "active",
				"algorithm":  "AES-256-GCM",
				"created_at": float64(time.Now().Unix()),
			},
		},
	}

	resp, err := parseKMSListKeyVersionsResponse(result)
	require.NoError(t, err)
	assert.Len(t, resp.Versions, 2)
	assert.Equal(t, uint64(1), resp.Versions[0].Version)
	assert.Equal(t, uint64(2), resp.Versions[1].Version)
}

func TestParseKMSListKeyVersionsResponseInvalidData(t *testing.T) {
	// Test with nil
	resp, err := parseKMSListKeyVersionsResponse(nil)
	require.NoError(t, err)
	assert.Len(t, resp.Versions, 0)

	// Test with empty versions
	result := map[string]interface{}{"versions": []interface{}{}}
	resp, err = parseKMSListKeyVersionsResponse(result)
	require.NoError(t, err)
	assert.Len(t, resp.Versions, 0)

	// Test with invalid version entry type
	result = map[string]interface{}{"versions": []interface{}{"invalid"}}
	resp, err = parseKMSListKeyVersionsResponse(result)
	require.NoError(t, err)
	assert.Len(t, resp.Versions, 0)
}

func TestParseKMSExportKeyResponse(t *testing.T) {
	result := map[string]interface{}{
		"key_id":      "key-123",
		"wrapped_key": "base64-wrapped-key-data",
	}

	resp, err := parseKMSExportKeyResponse(result)
	require.NoError(t, err)
	assert.Equal(t, "key-123", resp.KeyID)
	assert.Equal(t, []byte("base64-wrapped-key-data"), resp.WrappedKey)
}

func TestParseKMSExportKeyResponseInvalidData(t *testing.T) {
	resp, err := parseKMSExportKeyResponse(nil)
	require.NoError(t, err)
	assert.Equal(t, "", resp.KeyID)
	assert.Nil(t, resp.WrappedKey)
}

func TestParseKMSUnwrapKeyResponse(t *testing.T) {
	result := map[string]interface{}{
		"key_id":    "key-123",
		"algorithm": "AES-256-GCM",
		"status":    "active",
	}

	resp, err := parseKMSUnwrapKeyResponse(result)
	require.NoError(t, err)
	assert.Equal(t, "key-123", resp.KeyID)
	assert.Equal(t, "AES-256-GCM", string(resp.Algorithm))
	assert.Equal(t, "active", string(resp.Status))
}

func TestParseKMSUnwrapKeyResponseInvalidData(t *testing.T) {
	resp, err := parseKMSUnwrapKeyResponse(nil)
	require.NoError(t, err)
	assert.Equal(t, "", resp.KeyID)
}

func TestParseKMSSetKeySafetyResponse(t *testing.T) {
	result := map[string]interface{}{
		"key_id": "key-123",
		"config": map[string]interface{}{
			"max_operations":  float64(1000000),
			"max_bytes":       float64(1073741824),
			"warning_percent": float64(80),
		},
	}

	resp, err := parseKMSSetKeySafetyResponse(result)
	require.NoError(t, err)
	assert.Equal(t, "key-123", resp.KeyID)
	assert.Equal(t, uint64(1000000), resp.Config.MaxOperations)
	assert.Equal(t, uint64(1073741824), resp.Config.MaxBytes)
	assert.Equal(t, 80, resp.Config.WarningPercent)
}

func TestParseKMSSetKeySafetyResponseInvalidData(t *testing.T) {
	resp, err := parseKMSSetKeySafetyResponse(nil)
	require.NoError(t, err)
	assert.Equal(t, "", resp.KeyID)
}

func TestParseKMSClearKeySafetyResponse(t *testing.T) {
	result := map[string]interface{}{
		"key_id": "key-123",
	}

	resp, err := parseKMSClearKeySafetyResponse(result)
	require.NoError(t, err)
	assert.Equal(t, "key-123", resp.KeyID)
}

func TestParseKMSClearKeySafetyResponseInvalidData(t *testing.T) {
	resp, err := parseKMSClearKeySafetyResponse(nil)
	require.NoError(t, err)
	assert.Equal(t, "", resp.KeyID)
}

func TestParseKMSEncryptResponse(t *testing.T) {
	result := map[string]interface{}{
		"key_id":      "key-123",
		"key_version": float64(1),
		"ciphertext":  "encrypted-data",
	}

	resp, err := parseKMSEncryptResponse(result)
	require.NoError(t, err)
	assert.Equal(t, "key-123", resp.KeyID)
	assert.Equal(t, uint64(1), resp.KeyVersion)
	assert.Equal(t, []byte("encrypted-data"), resp.Ciphertext)
}

func TestParseKMSEncryptResponseInvalidData(t *testing.T) {
	resp, err := parseKMSEncryptResponse(nil)
	require.NoError(t, err)
	assert.Equal(t, "", resp.KeyID)
}

func TestParseKMSDecryptResponse(t *testing.T) {
	result := map[string]interface{}{
		"key_id":      "key-123",
		"key_version": float64(1),
		"plaintext":   "decrypted-data",
	}

	resp, err := parseKMSDecryptResponse(result)
	require.NoError(t, err)
	assert.Equal(t, "key-123", resp.KeyID)
	assert.Equal(t, uint64(1), resp.KeyVersion)
	assert.Equal(t, []byte("decrypted-data"), resp.Plaintext)
}

func TestParseKMSDecryptResponseInvalidData(t *testing.T) {
	resp, err := parseKMSDecryptResponse(nil)
	require.NoError(t, err)
	assert.Equal(t, "", resp.KeyID)
}

// =============================================================================
// DEK Parsing Function Tests
// =============================================================================

func TestParseDEKCreateResponse(t *testing.T) {
	tests := []struct {
		name   string
		result map[string]interface{}
	}{
		{
			name: "flat structure",
			result: map[string]interface{}{
				"path":      "/tenants/acme",
				"algorithm": "AES-256-GCM",
				"version":   float64(1),
				"status":    "active",
			},
		},
		{
			name: "nested dek",
			result: map[string]interface{}{
				"dek": map[string]interface{}{
					"path":      "/tenants/acme",
					"algorithm": "AES-256-GCM",
					"version":   float64(1),
					"status":    "active",
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resp, err := parseDEKCreateResponse(tt.result)
			require.NoError(t, err)
			assert.NotNil(t, resp.DEK)
		})
	}
}

func TestParseDEKCreateResponseInvalidData(t *testing.T) {
	resp, err := parseDEKCreateResponse(nil)
	require.NoError(t, err)
	assert.Equal(t, "", resp.DEK.Path)
}

func TestParseDEKGetResponse(t *testing.T) {
	result := map[string]interface{}{
		"dek": map[string]interface{}{
			"path":        "/tenants/acme",
			"algorithm":   "AES-256-GCM",
			"version":     float64(1),
			"status":      "active",
			"usage_count": float64(100),
			"bytes_used":  float64(1024),
		},
	}

	resp, err := parseDEKGetResponse(result)
	require.NoError(t, err)
	assert.Equal(t, "/tenants/acme", resp.DEK.Path)
	assert.Equal(t, uint64(100), resp.DEK.UsageCount)
	assert.Equal(t, uint64(1024), resp.DEK.BytesUsed)
}

func TestParseDEKGetResponseInvalidData(t *testing.T) {
	resp, err := parseDEKGetResponse(nil)
	require.NoError(t, err)
	assert.Equal(t, "", resp.DEK.Path)
}

func TestParseDEKRotateResponse(t *testing.T) {
	result := map[string]interface{}{
		"path":        "/tenants/acme",
		"old_version": float64(1),
		"new_version": float64(2),
	}

	resp, err := parseDEKRotateResponse(result)
	require.NoError(t, err)
	assert.Equal(t, "/tenants/acme", resp.Path)
	assert.Equal(t, uint64(1), resp.OldVersion)
	assert.Equal(t, uint64(2), resp.NewVersion)
}

func TestParseDEKRotateResponseInvalidData(t *testing.T) {
	resp, err := parseDEKRotateResponse(nil)
	require.NoError(t, err)
	assert.Equal(t, "", resp.Path)
}

func TestParseDEKDeleteResponse(t *testing.T) {
	result := map[string]interface{}{
		"path":    "/tenants/acme",
		"deleted": true,
	}

	resp, err := parseDEKDeleteResponse(result)
	require.NoError(t, err)
	assert.Equal(t, "/tenants/acme", resp.Path)
	assert.True(t, resp.Deleted)
}

func TestParseDEKDeleteResponseInvalidData(t *testing.T) {
	resp, err := parseDEKDeleteResponse(nil)
	require.NoError(t, err)
	assert.Equal(t, "", resp.Path)
	assert.False(t, resp.Deleted)
}

func TestParseDEKStatusResponse(t *testing.T) {
	result := map[string]interface{}{
		"dek": map[string]interface{}{
			"path":        "/tenants/acme",
			"status":      "active",
			"usage_count": float64(100),
			"bytes_used":  float64(1024),
		},
	}

	resp, err := parseDEKStatusResponse(result)
	require.NoError(t, err)
	assert.Equal(t, "/tenants/acme", resp.DEK.Path)
	assert.Equal(t, "active", resp.DEK.Status)
}

func TestParseDEKStatusResponseInvalidData(t *testing.T) {
	resp, err := parseDEKStatusResponse(nil)
	require.NoError(t, err)
	assert.Equal(t, "", resp.DEK.Path)
}

func TestParseDEKSetDefaultResponse(t *testing.T) {
	result := map[string]interface{}{
		"path":             "/tenants/acme",
		"previous_default": "/tenants/old",
	}

	resp, err := parseDEKSetDefaultResponse(result)
	require.NoError(t, err)
	assert.Equal(t, "/tenants/acme", resp.Path)
	assert.Equal(t, "/tenants/old", resp.PreviousDefault)
}

func TestParseDEKSetDefaultResponseInvalidData(t *testing.T) {
	resp, err := parseDEKSetDefaultResponse(nil)
	require.NoError(t, err)
	assert.Equal(t, "", resp.Path)
}

func TestParseDEKSetSafetyResponse(t *testing.T) {
	result := map[string]interface{}{
		"path": "/tenants/acme",
		"config": map[string]interface{}{
			"max_operations":  float64(1000000),
			"max_bytes":       float64(1073741824),
			"warning_percent": float64(80),
		},
	}

	resp, err := parseDEKSetSafetyResponse(result)
	require.NoError(t, err)
	assert.Equal(t, "/tenants/acme", resp.Path)
	assert.Equal(t, uint64(1000000), resp.Config.MaxOperations)
	assert.Equal(t, uint64(1073741824), resp.Config.MaxBytes)
	assert.Equal(t, 80, resp.Config.WarningPercent)
}

func TestParseDEKSetSafetyResponseInvalidData(t *testing.T) {
	resp, err := parseDEKSetSafetyResponse(nil)
	require.NoError(t, err)
	assert.Equal(t, "", resp.Path)
}

// =============================================================================
// Backend and Safety Response Parsing Tests
// =============================================================================

func TestParseBackendStatusResponse(t *testing.T) {
	result := map[string]interface{}{
		"backend_type": "quicbaas",
		"status":       "healthy",
		"device":       "/dev/sda1",
		"region":       "us-west-2",
	}

	resp, err := parseBackendStatusResponse(result)
	require.NoError(t, err)
	assert.Equal(t, "quicbaas", resp.BackendType)
	assert.Equal(t, "healthy", resp.Status)
	assert.Equal(t, "/dev/sda1", resp.Device)
	assert.Equal(t, "us-west-2", resp.Region)
}

func TestParseBackendStatusResponseInvalidData(t *testing.T) {
	resp, err := parseBackendStatusResponse(nil)
	require.NoError(t, err)
	assert.Equal(t, "", resp.BackendType)
}

func TestParseSafetyReportResponse(t *testing.T) {
	result := map[string]interface{}{
		"total_operations":      float64(1000000),
		"total_bytes_processed": float64(1073741824),
		"server_config": map[string]interface{}{
			"enforce":        true,
			"max_operations": float64(10000000),
			"max_bytes":      float64(10737418240),
		},
		"items": []interface{}{
			map[string]interface{}{
				"id":            "key-1",
				"type":          "key",
				"usage_percent": float64(85.5),
			},
		},
	}

	resp, err := parseSafetyReportResponse(result)
	require.NoError(t, err)
	assert.Equal(t, uint64(1000000), resp.TotalOperations)
	assert.Equal(t, uint64(1073741824), resp.TotalBytesProcessed)
	assert.True(t, resp.ServerConfig.Enforce)
	assert.Len(t, resp.Items, 1)
	assert.Equal(t, "key-1", resp.Items[0].ID)
}

func TestParseSafetyReportResponseInvalidData(t *testing.T) {
	resp, err := parseSafetyReportResponse(nil)
	require.NoError(t, err)
	assert.Equal(t, uint64(0), resp.TotalOperations)
	assert.Len(t, resp.Items, 0)
}

// =============================================================================
// Additional Helper Function Tests
// =============================================================================

func TestGetBytesWithByteSlice(t *testing.T) {
	m := map[string]interface{}{
		"data": []byte("hello world"),
	}
	result := getBytes(m, "data")
	assert.Equal(t, []byte("hello world"), result)
}

func TestGetInt64WithIntTypes(t *testing.T) {
	m := map[string]interface{}{
		"int64_val": int64(42),
		"int_val":   int(100),
	}

	assert.Equal(t, int64(42), getInt64(m, "int64_val"))
	assert.Equal(t, int64(100), getInt64(m, "int_val"))
}

func testWithInvalidOutputFormat(t *testing.T, fn func() error) {
	t.Helper()
	origFmt := outputFormat
	defer func() { outputFormat = origFmt }()
	outputFormat = "INVALID_FORMAT"

	err := fn()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "unknown output format")
}

func TestFormatVectorNamespaceCreateResponse_InvalidFormat(t *testing.T) {
	testWithInvalidOutputFormat(t, func() error {
		return formatVectorNamespaceCreateResponse(&vector.CreateNamespaceResponse{
			Success: true, Namespace: "ns", Timestamp: time.Now(),
		})
	})
}

func TestFormatVectorNamespaceDeleteResponse_InvalidFormat(t *testing.T) {
	testWithInvalidOutputFormat(t, func() error {
		return formatVectorNamespaceDeleteResponse(&vector.DeleteNamespaceResponse{
			Deleted: true, Timestamp: time.Now(),
		})
	})
}

func TestFormatVectorNamespaceListResponse_InvalidFormat(t *testing.T) {
	testWithInvalidOutputFormat(t, func() error {
		return formatVectorNamespaceListResponse(&vector.ListNamespacesResponse{
			Namespaces: []*vector.NamespaceInfo{}, Timestamp: time.Now(),
		})
	})
}

func TestFormatVectorNamespaceInfoResponse_InvalidFormat(t *testing.T) {
	testWithInvalidOutputFormat(t, func() error {
		return formatVectorNamespaceInfoResponse(&vector.GetNamespaceInfoResponse{
			Info: &vector.NamespaceInfo{}, Timestamp: time.Now(),
		})
	})
}

func TestFormatVectorUpsertResponse_InvalidFormat(t *testing.T) {
	testWithInvalidOutputFormat(t, func() error {
		return formatVectorUpsertResponse(&vector.UpsertResponse{
			ID: "vec-1", Success: true, Timestamp: time.Now(),
		})
	})
}

func TestFormatVectorBatchUpsertResponse_InvalidFormat(t *testing.T) {
	testWithInvalidOutputFormat(t, func() error {
		return formatVectorBatchUpsertResponse(&vector.BatchUpsertResponse{
			VectorsSucceeded: 5, Timestamp: time.Now(),
		})
	})
}

func TestFormatVectorGetResponse_InvalidFormat(t *testing.T) {
	testWithInvalidOutputFormat(t, func() error {
		return formatVectorGetResponse(&vector.GetResponse{
			Vector: &vector.VectorEntity{ID: "v1", Vector: []float32{0.1}},
			Found:  true, Timestamp: time.Now(),
		})
	})
}

func TestFormatVectorDeleteResponse_InvalidFormat(t *testing.T) {
	testWithInvalidOutputFormat(t, func() error {
		return formatVectorDeleteResponse(&vector.DeleteResponse{
			Deleted: true, Timestamp: time.Now(),
		})
	})
}

func TestFormatVectorBatchDeleteResponse_InvalidFormat(t *testing.T) {
	testWithInvalidOutputFormat(t, func() error {
		return formatVectorBatchDeleteResponse(&vector.BatchDeleteResponse{
			DeletedCount: 3, Timestamp: time.Now(),
		})
	})
}

func TestFormatVectorListResponse_InvalidFormat(t *testing.T) {
	testWithInvalidOutputFormat(t, func() error {
		return formatVectorListResponse(&vector.ListVectorsResponse{
			Vectors: []*vector.VectorEntity{}, Timestamp: time.Now(),
		})
	})
}

func TestFormatVectorSearchSimilarResponse_InvalidFormat(t *testing.T) {
	testWithInvalidOutputFormat(t, func() error {
		return formatVectorSearchSimilarResponse(&vector.SearchSimilarResponse{
			Results: []*vector.SearchResult{}, Timestamp: time.Now(),
		})
	})
}

func TestFormatVectorSearchByIDResponse_InvalidFormat(t *testing.T) {
	testWithInvalidOutputFormat(t, func() error {
		return formatVectorSearchByIDResponse(&vector.SearchByIDResponse{
			Results: []*vector.SearchResult{}, Timestamp: time.Now(),
		})
	})
}

func TestFormatVectorIndexRebuildResponse_InvalidFormat(t *testing.T) {
	testWithInvalidOutputFormat(t, func() error {
		return formatVectorIndexRebuildResponse(&vector.RebuildIndexResponse{
			OperationID: "op-1", Status: "running",
		})
	})
}

func TestFormatVectorIndexProgressResponse_InvalidFormat(t *testing.T) {
	testWithInvalidOutputFormat(t, func() error {
		return formatVectorIndexProgressResponse(&vector.GetIndexProgressResponse{
			Namespace: "ns", PercentComplete: 50.0,
		})
	})
}

func TestFormatVectorIndexStatsResponse_InvalidFormat(t *testing.T) {
	testWithInvalidOutputFormat(t, func() error {
		return formatVectorIndexStatsResponse(&vector.GetStatsResponse{
			Namespace: "ns", VectorCount: 100,
		})
	})
}

// =============================================================================
// Vector Format Functions - UpdateMetadata, HybridSearch, SearchByMetadata
// These format functions do not use ParseOutputFormat but have uncovered paths
// =============================================================================

func TestFormatVectorUpdateMetadataResponse_FailedPath(t *testing.T) {
	resp := &vector.UpdateMetadataResponse{
		Success: false,
	}
	err := formatVectorUpdateMetadataResponse(resp)
	assert.NoError(t, err)
}

func TestFormatVectorHybridSearchResponse_WithResults(t *testing.T) {
	resp := &vector.HybridSearchResponse{
		Results: []*vector.SearchResult{
			{
				Vector: &vector.VectorEntity{
					ID:       "vec-1",
					Vector:   []float32{0.1, 0.2},
					Metadata: map[string]string{"key": "val"},
				},
				Score: 0.95,
				Rank:  1,
			},
		},
		TotalCandidates:  10,
		TotalFiltered:    5,
		RerankingApplied: true,
		SearchTimeMs:     15,
		Timestamp:        time.Now(),
	}
	err := formatVectorHybridSearchResponse(resp)
	assert.NoError(t, err)
}

func TestFormatVectorHybridSearchResponse_NilVector(t *testing.T) {
	resp := &vector.HybridSearchResponse{
		Results: []*vector.SearchResult{
			{Score: 0.5, Rank: 1},
		},
		TotalCandidates: 5,
		TotalFiltered:   1,
		SearchTimeMs:    10,
		Timestamp:       time.Now(),
	}
	err := formatVectorHybridSearchResponse(resp)
	assert.NoError(t, err)
}

func TestFormatVectorSearchByMetadataResponse_WithResults(t *testing.T) {
	resp := &vector.SearchByMetadataResponse{
		Results: []*vector.VectorEntity{
			{
				ID:       "vec-1",
				Vector:   []float32{0.1, 0.2, 0.3},
				Metadata: map[string]string{"type": "test"},
			},
		},
		TotalCount:   1,
		SearchTimeMs: 5,
		Timestamp:    time.Now(),
	}
	err := formatVectorSearchByMetadataResponse(resp)
	assert.NoError(t, err)
}
