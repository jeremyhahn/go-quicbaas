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

func TestLoggerAdapter(t *testing.T) {
	baseLogger, err := logger.New(logger.LogConfig{
		Level:  "info",
		Format: "json",
		Output: "stdout",
	})
	require.NoError(t, err)

	base := asInterfaceLogger(baseLogger)
	baseAdapter, ok := base.(*loggerAdapter)
	require.True(t, ok, "asInterfaceLogger should return *loggerAdapter")
	assert.Equal(t, baseLogger, baseAdapter.Logger)

	tests := []struct {
		name string
		call func(l interfaces.Logger) interfaces.Logger
	}{
		{"with error", func(l interfaces.Logger) interfaces.Logger {
			return l.WithError(errors.New("test error"))
		}},
		{"with nil error", func(l interfaces.Logger) interfaces.Logger {
			return l.WithError(nil)
		}},
		{"with field", func(l interfaces.Logger) interfaces.Logger {
			return l.WithField("key", "value")
		}},
		{"with fields", func(l interfaces.Logger) interfaces.Logger {
			return l.WithFields(map[string]interface{}{"key1": "value1", "key2": 123, "key3": true})
		}},
		{"with empty fields", func(l interfaces.Logger) interfaces.Logger {
			return l.WithFields(map[string]interface{}{})
		}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resultAdapter, ok := tt.call(base).(*loggerAdapter)
			require.True(t, ok, "adapter methods should return *loggerAdapter")
			assert.NotNil(t, resultAdapter.Logger)
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

// The UpdateMetadata, HybridSearch and SearchByMetadata formatters print
// plain text and do not read outputFormat.
func TestFormatVectorUpdateMetadataResponse(t *testing.T) {
	tests := []struct {
		name     string
		response *vector.UpdateMetadataResponse
	}{
		{"success", &vector.UpdateMetadataResponse{Success: true, Version: 1, Timestamp: time.Now()}},
		{"failure", &vector.UpdateMetadataResponse{Success: false}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.NoError(t, formatVectorUpdateMetadataResponse(tt.response))
		})
	}
}

func TestFormatVectorHybridSearchResponse(t *testing.T) {
	tests := []struct {
		name     string
		response *vector.HybridSearchResponse
	}{
		{
			name: "with results",
			response: &vector.HybridSearchResponse{
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
			},
		},
		{
			name: "result without vector",
			response: &vector.HybridSearchResponse{
				Results:         []*vector.SearchResult{{Score: 0.5, Rank: 1}},
				TotalCandidates: 5,
				TotalFiltered:   1,
				SearchTimeMs:    10,
				Timestamp:       time.Now(),
			},
		},
		{
			name: "no results",
			response: &vector.HybridSearchResponse{
				Results:      []*vector.SearchResult{},
				SearchTimeMs: 10,
				Timestamp:    time.Now(),
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.NoError(t, formatVectorHybridSearchResponse(tt.response))
		})
	}
}

func TestFormatVectorSearchByMetadataResponse(t *testing.T) {
	tests := []struct {
		name     string
		response *vector.SearchByMetadataResponse
	}{
		{
			name: "with results",
			response: &vector.SearchByMetadataResponse{
				Results: []*vector.VectorEntity{
					{
						ID:        "vec-1",
						Vector:    []float32{0.1, 0.2, 0.3},
						Metadata:  map[string]string{"key": "value"},
						Version:   1,
						CreatedAt: time.Now(),
					},
				},
				TotalCount:   1,
				SearchTimeMs: 25,
				Timestamp:    time.Now(),
			},
		},
		{
			name: "no results",
			response: &vector.SearchByMetadataResponse{
				Results:      []*vector.VectorEntity{},
				SearchTimeMs: 10,
				Timestamp:    time.Now(),
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.NoError(t, formatVectorSearchByMetadataResponse(tt.response))
		})
	}
}

// =============================================================================
// MCP KMS Parsing Function Tests
// =============================================================================

// TestParseKMSResponses covers each MCP KMS parser with a populated result
// and with nil or empty input, which yields a zero-valued response.
func TestParseKMSResponses(t *testing.T) {
	tests := []struct {
		name  string
		check func(t *testing.T)
	}{
		{"import key", func(t *testing.T) {
			resp, err := parseKMSImportKeyResponse(map[string]interface{}{
				"key_id":    "imported-key-123",
				"algorithm": "AES-256-GCM",
				"status":    "active",
			})
			require.NoError(t, err)
			assert.Equal(t, "imported-key-123", resp.Key.KeyID)
			assert.Equal(t, "AES-256-GCM", string(resp.Key.Algorithm))
			assert.Equal(t, "active", string(resp.Key.Status))
		}},
		{"import key nil", func(t *testing.T) {
			resp, err := parseKMSImportKeyResponse(nil)
			require.NoError(t, err)
			assert.Equal(t, "", resp.Key.KeyID)
		}},
		{"import key empty map", func(t *testing.T) {
			resp, err := parseKMSImportKeyResponse(map[string]interface{}{})
			require.NoError(t, err)
			assert.Equal(t, "", resp.Key.KeyID)
		}},
		{"get key flat structure", func(t *testing.T) {
			resp, err := parseKMSGetKeyResponse(map[string]interface{}{
				"key_id":     "key-123",
				"algorithm":  "AES-256-GCM",
				"status":     "active",
				"version":    float64(1),
				"created_at": float64(time.Now().Unix()),
			})
			require.NoError(t, err)
			assert.NotNil(t, resp.Key)
			assert.NotNil(t, resp.Metadata)
		}},
		{"get key nested metadata", func(t *testing.T) {
			resp, err := parseKMSGetKeyResponse(map[string]interface{}{
				"metadata": map[string]interface{}{
					"key_id":     "key-456",
					"algorithm":  "CHACHA20-POLY1305",
					"status":     "disabled",
					"version":    float64(2),
					"created_at": float64(time.Now().Unix()),
				},
			})
			require.NoError(t, err)
			assert.NotNil(t, resp.Key)
			assert.NotNil(t, resp.Metadata)
		}},
		{"get key nil", func(t *testing.T) {
			resp, err := parseKMSGetKeyResponse(nil)
			require.NoError(t, err)
			assert.Equal(t, "", resp.Key.KeyID)
		}},
		{"rotate key", func(t *testing.T) {
			resp, err := parseKMSRotateKeyResponse(map[string]interface{}{
				"key_id":      "key-123",
				"old_version": float64(1),
				"new_version": float64(2),
			})
			require.NoError(t, err)
			assert.Equal(t, "key-123", resp.KeyID)
			assert.Equal(t, uint64(1), resp.OldVersion)
			assert.Equal(t, uint64(2), resp.NewVersion)
		}},
		{"rotate key nil", func(t *testing.T) {
			resp, err := parseKMSRotateKeyResponse(nil)
			require.NoError(t, err)
			assert.Equal(t, "", resp.KeyID)
		}},
		{"delete key", func(t *testing.T) {
			resp, err := parseKMSDeleteKeyResponse(map[string]interface{}{
				"key_id": "key-123",
				"status": "deleted",
			})
			require.NoError(t, err)
			assert.Equal(t, "key-123", resp.KeyID)
			assert.Equal(t, "deleted", string(resp.Status))
		}},
		{"delete key nil", func(t *testing.T) {
			resp, err := parseKMSDeleteKeyResponse(nil)
			require.NoError(t, err)
			assert.Equal(t, "", resp.KeyID)
		}},
		{"update key status", func(t *testing.T) {
			resp, err := parseKMSUpdateKeyStatusResponse(map[string]interface{}{
				"key_id":     "key-123",
				"old_status": "active",
				"new_status": "disabled",
			})
			require.NoError(t, err)
			assert.Equal(t, "key-123", resp.KeyID)
			assert.Equal(t, "active", string(resp.OldStatus))
			assert.Equal(t, "disabled", string(resp.NewStatus))
		}},
		{"update key status nil", func(t *testing.T) {
			resp, err := parseKMSUpdateKeyStatusResponse(nil)
			require.NoError(t, err)
			assert.Equal(t, "", resp.KeyID)
		}},
		{"list key versions", func(t *testing.T) {
			resp, err := parseKMSListKeyVersionsResponse(map[string]interface{}{
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
			})
			require.NoError(t, err)
			assert.Len(t, resp.Versions, 2)
			assert.Equal(t, uint64(1), resp.Versions[0].Version)
			assert.Equal(t, uint64(2), resp.Versions[1].Version)
		}},
		{"list key versions nil", func(t *testing.T) {
			resp, err := parseKMSListKeyVersionsResponse(nil)
			require.NoError(t, err)
			assert.Len(t, resp.Versions, 0)
		}},
		{"list key versions empty", func(t *testing.T) {
			resp, err := parseKMSListKeyVersionsResponse(map[string]interface{}{"versions": []interface{}{}})
			require.NoError(t, err)
			assert.Len(t, resp.Versions, 0)
		}},
		{"list key versions invalid entry type", func(t *testing.T) {
			resp, err := parseKMSListKeyVersionsResponse(map[string]interface{}{"versions": []interface{}{"invalid"}})
			require.NoError(t, err)
			assert.Len(t, resp.Versions, 0)
		}},
		{"export key", func(t *testing.T) {
			resp, err := parseKMSExportKeyResponse(map[string]interface{}{
				"key_id":      "key-123",
				"wrapped_key": "base64-wrapped-key-data",
			})
			require.NoError(t, err)
			assert.Equal(t, "key-123", resp.KeyID)
			assert.Equal(t, []byte("base64-wrapped-key-data"), resp.WrappedKey)
		}},
		{"export key nil", func(t *testing.T) {
			resp, err := parseKMSExportKeyResponse(nil)
			require.NoError(t, err)
			assert.Equal(t, "", resp.KeyID)
			assert.Nil(t, resp.WrappedKey)
		}},
		{"unwrap key", func(t *testing.T) {
			resp, err := parseKMSUnwrapKeyResponse(map[string]interface{}{
				"key_id":    "key-123",
				"algorithm": "AES-256-GCM",
				"status":    "active",
			})
			require.NoError(t, err)
			assert.Equal(t, "key-123", resp.KeyID)
			assert.Equal(t, "AES-256-GCM", string(resp.Algorithm))
			assert.Equal(t, "active", string(resp.Status))
		}},
		{"unwrap key nil", func(t *testing.T) {
			resp, err := parseKMSUnwrapKeyResponse(nil)
			require.NoError(t, err)
			assert.Equal(t, "", resp.KeyID)
		}},
		{"set key safety", func(t *testing.T) {
			resp, err := parseKMSSetKeySafetyResponse(map[string]interface{}{
				"key_id": "key-123",
				"config": map[string]interface{}{
					"max_operations":  float64(1000000),
					"max_bytes":       float64(1073741824),
					"warning_percent": float64(80),
				},
			})
			require.NoError(t, err)
			assert.Equal(t, "key-123", resp.KeyID)
			assert.Equal(t, uint64(1000000), resp.Config.MaxOperations)
			assert.Equal(t, uint64(1073741824), resp.Config.MaxBytes)
			assert.Equal(t, 80, resp.Config.WarningPercent)
		}},
		{"set key safety nil", func(t *testing.T) {
			resp, err := parseKMSSetKeySafetyResponse(nil)
			require.NoError(t, err)
			assert.Equal(t, "", resp.KeyID)
		}},
		{"clear key safety", func(t *testing.T) {
			resp, err := parseKMSClearKeySafetyResponse(map[string]interface{}{"key_id": "key-123"})
			require.NoError(t, err)
			assert.Equal(t, "key-123", resp.KeyID)
		}},
		{"clear key safety nil", func(t *testing.T) {
			resp, err := parseKMSClearKeySafetyResponse(nil)
			require.NoError(t, err)
			assert.Equal(t, "", resp.KeyID)
		}},
		{"encrypt", func(t *testing.T) {
			resp, err := parseKMSEncryptResponse(map[string]interface{}{
				"key_id":      "key-123",
				"key_version": float64(1),
				"ciphertext":  "encrypted-data",
			})
			require.NoError(t, err)
			assert.Equal(t, "key-123", resp.KeyID)
			assert.Equal(t, uint64(1), resp.KeyVersion)
			assert.Equal(t, []byte("encrypted-data"), resp.Ciphertext)
		}},
		{"encrypt nil", func(t *testing.T) {
			resp, err := parseKMSEncryptResponse(nil)
			require.NoError(t, err)
			assert.Equal(t, "", resp.KeyID)
		}},
		{"decrypt", func(t *testing.T) {
			resp, err := parseKMSDecryptResponse(map[string]interface{}{
				"key_id":      "key-123",
				"key_version": float64(1),
				"plaintext":   "decrypted-data",
			})
			require.NoError(t, err)
			assert.Equal(t, "key-123", resp.KeyID)
			assert.Equal(t, uint64(1), resp.KeyVersion)
			assert.Equal(t, []byte("decrypted-data"), resp.Plaintext)
		}},
		{"decrypt nil", func(t *testing.T) {
			resp, err := parseKMSDecryptResponse(nil)
			require.NoError(t, err)
			assert.Equal(t, "", resp.KeyID)
		}},
	}

	for _, tt := range tests {
		t.Run(tt.name, tt.check)
	}
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

func TestFormatVectorResponses_InvalidFormat(t *testing.T) {
	tests := []struct {
		name   string
		format func() error
	}{
		{"namespace create", func() error {
			return formatVectorNamespaceCreateResponse(&vector.CreateNamespaceResponse{
				Success: true, Namespace: "ns", Timestamp: time.Now(),
			})
		}},
		{"namespace delete", func() error {
			return formatVectorNamespaceDeleteResponse(&vector.DeleteNamespaceResponse{
				Deleted: true, Timestamp: time.Now(),
			})
		}},
		{"namespace list", func() error {
			return formatVectorNamespaceListResponse(&vector.ListNamespacesResponse{
				Namespaces: []*vector.NamespaceInfo{}, Timestamp: time.Now(),
			})
		}},
		{"namespace info", func() error {
			return formatVectorNamespaceInfoResponse(&vector.GetNamespaceInfoResponse{
				Info: &vector.NamespaceInfo{}, Timestamp: time.Now(),
			})
		}},
		{"upsert", func() error {
			return formatVectorUpsertResponse(&vector.UpsertResponse{
				ID: "vec-1", Success: true, Timestamp: time.Now(),
			})
		}},
		{"batch upsert", func() error {
			return formatVectorBatchUpsertResponse(&vector.BatchUpsertResponse{
				VectorsSucceeded: 5, Timestamp: time.Now(),
			})
		}},
		{"get", func() error {
			return formatVectorGetResponse(&vector.GetResponse{
				Vector: &vector.VectorEntity{ID: "v1", Vector: []float32{0.1}},
				Found:  true, Timestamp: time.Now(),
			})
		}},
		{"delete", func() error {
			return formatVectorDeleteResponse(&vector.DeleteResponse{
				Deleted: true, Timestamp: time.Now(),
			})
		}},
		{"batch delete", func() error {
			return formatVectorBatchDeleteResponse(&vector.BatchDeleteResponse{
				DeletedCount: 3, Timestamp: time.Now(),
			})
		}},
		{"list", func() error {
			return formatVectorListResponse(&vector.ListVectorsResponse{
				Vectors: []*vector.VectorEntity{}, Timestamp: time.Now(),
			})
		}},
		{"search similar", func() error {
			return formatVectorSearchSimilarResponse(&vector.SearchSimilarResponse{
				Results: []*vector.SearchResult{}, Timestamp: time.Now(),
			})
		}},
		{"search by id", func() error {
			return formatVectorSearchByIDResponse(&vector.SearchByIDResponse{
				Results: []*vector.SearchResult{}, Timestamp: time.Now(),
			})
		}},
		{"index rebuild", func() error {
			return formatVectorIndexRebuildResponse(&vector.RebuildIndexResponse{
				OperationID: "op-1", Status: "running",
			})
		}},
		{"index progress", func() error {
			return formatVectorIndexProgressResponse(&vector.GetIndexProgressResponse{
				Namespace: "ns", PercentComplete: 50.0,
			})
		}},
		{"index stats", func() error {
			return formatVectorIndexStatsResponse(&vector.GetStatsResponse{
				Namespace: "ns", VectorCount: 100,
			})
		}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			testWithInvalidOutputFormat(t, tt.format)
		})
	}
}
