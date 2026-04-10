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

package grpc

import (
	"math"
	"testing"

	"github.com/stretchr/testify/assert"

	pb "github.com/jeremyhahn/go-quicbaas/api/proto"
)

// =============================================================================
// ValidateUpsertRequest Tests
// =============================================================================

func TestVectorValidator_ValidateUpsertRequest(t *testing.T) {
	validator := NewVectorValidator()

	tests := []struct {
		name        string
		request     *pb.UpsertVectorRequest
		expectError bool
		errorMsg    string
	}{
		{
			name:        "nil_request",
			request:     nil,
			expectError: true,
			errorMsg:    "request cannot be nil",
		},
		{
			name: "nil_vector",
			request: &pb.UpsertVectorRequest{
				Vector: nil,
			},
			expectError: true,
			errorMsg:    "vector is required",
		},
		{
			name: "missing_namespace",
			request: &pb.UpsertVectorRequest{
				Vector: &pb.VectorEntity{
					Namespace: "",
					Id:        "id1",
					Vector:    []float32{1.0, 2.0, 3.0},
				},
			},
			expectError: true,
			errorMsg:    "namespace is required",
		},
		{
			name: "missing_id",
			request: &pb.UpsertVectorRequest{
				Vector: &pb.VectorEntity{
					Namespace: "test",
					Id:        "",
					Vector:    []float32{1.0, 2.0, 3.0},
				},
			},
			expectError: true,
			errorMsg:    "vector ID is required",
		},
		{
			name: "empty_vector_data",
			request: &pb.UpsertVectorRequest{
				Vector: &pb.VectorEntity{
					Namespace: "test",
					Id:        "id1",
					Vector:    []float32{},
				},
			},
			expectError: true,
			errorMsg:    "vector data is required",
		},
		{
			name: "valid_request",
			request: &pb.UpsertVectorRequest{
				Vector: &pb.VectorEntity{
					Namespace: "test",
					Id:        "id1",
					Vector:    []float32{1.0, 2.0, 3.0},
				},
			},
			expectError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validator.ValidateUpsertRequest(tt.request)
			if tt.expectError {
				assert.Error(t, err)
				assert.Contains(t, err.Error(), tt.errorMsg)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

// =============================================================================
// ValidateDeleteRequest Tests
// =============================================================================

func TestVectorValidator_ValidateDeleteRequest(t *testing.T) {
	validator := NewVectorValidator()

	tests := []struct {
		name        string
		request     *pb.DeleteVectorRequest
		expectError bool
		errorMsg    string
	}{
		{
			name:        "nil_request",
			request:     nil,
			expectError: true,
			errorMsg:    "request cannot be nil",
		},
		{
			name: "missing_namespace",
			request: &pb.DeleteVectorRequest{
				Namespace: "",
				Id:        "id1",
			},
			expectError: true,
			errorMsg:    "namespace is required",
		},
		{
			name: "missing_id",
			request: &pb.DeleteVectorRequest{
				Namespace: "test",
				Id:        "",
			},
			expectError: true,
			errorMsg:    "vector ID is required",
		},
		{
			name: "valid_request",
			request: &pb.DeleteVectorRequest{
				Namespace: "test",
				Id:        "id1",
			},
			expectError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validator.ValidateDeleteRequest(tt.request)
			if tt.expectError {
				assert.Error(t, err)
				assert.Contains(t, err.Error(), tt.errorMsg)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

// =============================================================================
// ValidateGetRequest Tests
// =============================================================================

func TestVectorValidator_ValidateGetRequest(t *testing.T) {
	validator := NewVectorValidator()

	tests := []struct {
		name        string
		request     *pb.GetVectorRequest
		expectError bool
		errorMsg    string
	}{
		{
			name:        "nil_request",
			request:     nil,
			expectError: true,
			errorMsg:    "request cannot be nil",
		},
		{
			name: "missing_namespace",
			request: &pb.GetVectorRequest{
				Namespace: "",
				Id:        "id1",
			},
			expectError: true,
			errorMsg:    "namespace is required",
		},
		{
			name: "missing_id",
			request: &pb.GetVectorRequest{
				Namespace: "test",
				Id:        "",
			},
			expectError: true,
			errorMsg:    "vector ID is required",
		},
		{
			name: "valid_request",
			request: &pb.GetVectorRequest{
				Namespace: "test",
				Id:        "id1",
			},
			expectError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validator.ValidateGetRequest(tt.request)
			if tt.expectError {
				assert.Error(t, err)
				assert.Contains(t, err.Error(), tt.errorMsg)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

// =============================================================================
// ValidateSearchRequest Tests
// =============================================================================

func TestVectorValidator_ValidateSearchRequest(t *testing.T) {
	validator := NewVectorValidator()

	tests := []struct {
		name        string
		request     *pb.SearchSimilarRequest
		expectError bool
		errorMsg    string
	}{
		{
			name:        "nil_request",
			request:     nil,
			expectError: true,
			errorMsg:    "request cannot be nil",
		},
		{
			name: "missing_namespace",
			request: &pb.SearchSimilarRequest{
				Namespace:   "",
				QueryVector: []float32{1.0, 2.0, 3.0},
				K:           10,
			},
			expectError: true,
			errorMsg:    "namespace is required",
		},
		{
			name: "empty_query_vector",
			request: &pb.SearchSimilarRequest{
				Namespace:   "test",
				QueryVector: []float32{},
				K:           10,
			},
			expectError: true,
			errorMsg:    "query vector is required",
		},
		{
			name: "k_zero",
			request: &pb.SearchSimilarRequest{
				Namespace:   "test",
				QueryVector: []float32{1.0, 2.0, 3.0},
				K:           0,
			},
			expectError: true,
			errorMsg:    "k must be positive",
		},
		{
			name: "k_negative",
			request: &pb.SearchSimilarRequest{
				Namespace:   "test",
				QueryVector: []float32{1.0, 2.0, 3.0},
				K:           -5,
			},
			expectError: true,
			errorMsg:    "k must be positive",
		},
		{
			name: "k_too_large",
			request: &pb.SearchSimilarRequest{
				Namespace:   "test",
				QueryVector: []float32{1.0, 2.0, 3.0},
				K:           maxK + 1,
			},
			expectError: true,
			errorMsg:    "exceeds maximum allowed",
		},
		{
			name: "valid_request",
			request: &pb.SearchSimilarRequest{
				Namespace:   "test",
				QueryVector: []float32{1.0, 2.0, 3.0},
				K:           10,
			},
			expectError: false,
		},
		{
			name: "valid_request_max_k",
			request: &pb.SearchSimilarRequest{
				Namespace:   "test",
				QueryVector: []float32{1.0, 2.0, 3.0},
				K:           maxK,
			},
			expectError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validator.ValidateSearchRequest(tt.request)
			if tt.expectError {
				assert.Error(t, err)
				assert.Contains(t, err.Error(), tt.errorMsg)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

// =============================================================================
// ValidateCreateNamespaceRequest Tests
// =============================================================================

func TestVectorValidator_ValidateCreateNamespaceRequest(t *testing.T) {
	validator := NewVectorValidator()

	tests := []struct {
		name        string
		request     *pb.CreateNamespaceRequest
		expectError bool
		errorMsg    string
	}{
		{
			name:        "nil_request",
			request:     nil,
			expectError: true,
			errorMsg:    "request cannot be nil",
		},
		{
			name:        "nil_config",
			request:     &pb.CreateNamespaceRequest{},
			expectError: true,
			errorMsg:    "config is required",
		},
		{
			name: "missing_namespace_name",
			request: &pb.CreateNamespaceRequest{
				Config: &pb.NamespaceConfig{
					Namespace:      "",
					Dimensions:     128,
					DistanceMetric: pb.DistanceMetric_DISTANCE_METRIC_COSINE,
				},
			},
			expectError: true,
			errorMsg:    "namespace name is required",
		},
		{
			name: "zero_dimensions",
			request: &pb.CreateNamespaceRequest{
				Config: &pb.NamespaceConfig{
					Namespace:      "test",
					Dimensions:     0,
					DistanceMetric: pb.DistanceMetric_DISTANCE_METRIC_COSINE,
				},
			},
			expectError: true,
			errorMsg:    "dimensions must be positive",
		},
		{
			name: "negative_dimensions",
			request: &pb.CreateNamespaceRequest{
				Config: &pb.NamespaceConfig{
					Namespace:      "test",
					Dimensions:     -1,
					DistanceMetric: pb.DistanceMetric_DISTANCE_METRIC_COSINE,
				},
			},
			expectError: true,
			errorMsg:    "dimensions must be positive",
		},
		{
			name: "dimensions_exceed_max",
			request: &pb.CreateNamespaceRequest{
				Config: &pb.NamespaceConfig{
					Namespace:      "test",
					Dimensions:     maxDimensions + 1,
					DistanceMetric: pb.DistanceMetric_DISTANCE_METRIC_COSINE,
				},
			},
			expectError: true,
			errorMsg:    "dimensions cannot exceed",
		},
		{
			name: "unspecified_distance_metric",
			request: &pb.CreateNamespaceRequest{
				Config: &pb.NamespaceConfig{
					Namespace:      "test",
					Dimensions:     128,
					DistanceMetric: pb.DistanceMetric_DISTANCE_METRIC_UNSPECIFIED,
				},
			},
			expectError: true,
			errorMsg:    "distance_metric is required",
		},
		{
			name: "valid_request_cosine",
			request: &pb.CreateNamespaceRequest{
				Config: &pb.NamespaceConfig{
					Namespace:      "test",
					Dimensions:     128,
					DistanceMetric: pb.DistanceMetric_DISTANCE_METRIC_COSINE,
				},
			},
			expectError: false,
		},
		{
			name: "valid_request_euclidean",
			request: &pb.CreateNamespaceRequest{
				Config: &pb.NamespaceConfig{
					Namespace:      "test",
					Dimensions:     256,
					DistanceMetric: pb.DistanceMetric_DISTANCE_METRIC_L2,
				},
			},
			expectError: false,
		},
		{
			name: "valid_request_dot_product",
			request: &pb.CreateNamespaceRequest{
				Config: &pb.NamespaceConfig{
					Namespace:      "test",
					Dimensions:     512,
					DistanceMetric: pb.DistanceMetric_DISTANCE_METRIC_DOT_PRODUCT,
				},
			},
			expectError: false,
		},
		{
			name: "valid_request_max_dimensions",
			request: &pb.CreateNamespaceRequest{
				Config: &pb.NamespaceConfig{
					Namespace:      "test",
					Dimensions:     maxDimensions,
					DistanceMetric: pb.DistanceMetric_DISTANCE_METRIC_COSINE,
				},
			},
			expectError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validator.ValidateCreateNamespaceRequest(tt.request)
			if tt.expectError {
				assert.Error(t, err)
				assert.Contains(t, err.Error(), tt.errorMsg)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

// =============================================================================
// ValidateBatchDeleteRequest Tests
// =============================================================================

func TestVectorValidator_ValidateBatchDeleteRequest(t *testing.T) {
	validator := NewVectorValidator()

	tests := []struct {
		name        string
		request     *pb.BatchDeleteVectorsRequest
		expectError bool
		errorMsg    string
	}{
		{
			name:        "nil_request",
			request:     nil,
			expectError: true,
			errorMsg:    "request cannot be nil",
		},
		{
			name: "missing_namespace",
			request: &pb.BatchDeleteVectorsRequest{
				Namespace: "",
				Ids:       []string{"id1"},
			},
			expectError: true,
			errorMsg:    "namespace is required",
		},
		{
			name: "empty_ids",
			request: &pb.BatchDeleteVectorsRequest{
				Namespace: "test",
				Ids:       []string{},
			},
			expectError: true,
			errorMsg:    "at least one vector ID is required",
		},
		{
			name: "valid_request",
			request: &pb.BatchDeleteVectorsRequest{
				Namespace: "test",
				Ids:       []string{"id1", "id2"},
			},
			expectError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validator.ValidateBatchDeleteRequest(tt.request)
			if tt.expectError {
				assert.Error(t, err)
				assert.Contains(t, err.Error(), tt.errorMsg)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestVectorValidator_ValidateSearchByIDRequest(t *testing.T) {
	validator := NewVectorValidator()

	tests := []struct {
		name        string
		request     *pb.SearchByIDRequest
		expectError bool
		errorMsg    string
	}{
		{
			name:        "nil_request",
			request:     nil,
			expectError: true,
			errorMsg:    "request cannot be nil",
		},
		{
			name: "missing_namespace",
			request: &pb.SearchByIDRequest{
				Namespace: "",
				Id:        "id1",
				K:         10,
			},
			expectError: true,
			errorMsg:    "namespace is required",
		},
		{
			name: "missing_id",
			request: &pb.SearchByIDRequest{
				Namespace: "test",
				Id:        "",
				K:         10,
			},
			expectError: true,
			errorMsg:    "vector ID is required",
		},
		{
			name: "k_zero",
			request: &pb.SearchByIDRequest{
				Namespace: "test",
				Id:        "id1",
				K:         0,
			},
			expectError: true,
			errorMsg:    "k must be positive",
		},
		{
			name: "k_too_large",
			request: &pb.SearchByIDRequest{
				Namespace: "test",
				Id:        "id1",
				K:         maxK + 1,
			},
			expectError: true,
			errorMsg:    "exceeds maximum allowed",
		},
		{
			name: "valid_request",
			request: &pb.SearchByIDRequest{
				Namespace: "test",
				Id:        "id1",
				K:         10,
			},
			expectError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validator.ValidateSearchByIDRequest(tt.request)
			if tt.expectError {
				assert.Error(t, err)
				assert.Contains(t, err.Error(), tt.errorMsg)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestVectorValidator_ValidateListRequest(t *testing.T) {
	validator := NewVectorValidator()

	tests := []struct {
		name        string
		request     *pb.ListVectorsRequest
		expectError bool
		errorMsg    string
	}{
		{
			name:        "nil_request",
			request:     nil,
			expectError: true,
			errorMsg:    "request cannot be nil",
		},
		{
			name: "missing_namespace",
			request: &pb.ListVectorsRequest{
				Namespace: "",
				PageSize:  10,
			},
			expectError: true,
			errorMsg:    "namespace is required",
		},
		{
			name: "negative_page_size",
			request: &pb.ListVectorsRequest{
				Namespace: "test",
				PageSize:  -1,
			},
			expectError: true,
			errorMsg:    "page size cannot be negative",
		},
		{
			name: "page_size_too_large",
			request: &pb.ListVectorsRequest{
				Namespace: "test",
				PageSize:  maxPageSize + 1,
			},
			expectError: true,
			errorMsg:    "page size cannot exceed",
		},
		{
			name: "valid_request",
			request: &pb.ListVectorsRequest{
				Namespace: "test",
				PageSize:  10,
			},
			expectError: false,
		},
		{
			name: "valid_request_zero_page_size",
			request: &pb.ListVectorsRequest{
				Namespace: "test",
				PageSize:  0,
			},
			expectError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validator.ValidateListRequest(tt.request)
			if tt.expectError {
				assert.Error(t, err)
				assert.Contains(t, err.Error(), tt.errorMsg)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestVectorValidator_ValidateUpdateNamespaceRequest(t *testing.T) {
	validator := NewVectorValidator()

	tests := []struct {
		name        string
		request     *pb.UpdateNamespaceRequest
		expectError bool
		errorMsg    string
	}{
		{
			name:        "nil_request",
			request:     nil,
			expectError: true,
			errorMsg:    "request cannot be nil",
		},
		{
			name: "missing_namespace",
			request: &pb.UpdateNamespaceRequest{
				Namespace: "",
			},
			expectError: true,
			errorMsg:    "namespace is required",
		},
		{
			name: "valid_request",
			request: &pb.UpdateNamespaceRequest{
				Namespace: "test",
			},
			expectError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validator.ValidateUpdateNamespaceRequest(tt.request)
			if tt.expectError {
				assert.Error(t, err)
				assert.Contains(t, err.Error(), tt.errorMsg)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestVectorValidator_ValidateDeleteNamespaceRequest(t *testing.T) {
	validator := NewVectorValidator()

	tests := []struct {
		name        string
		request     *pb.DeleteNamespaceRequest
		expectError bool
		errorMsg    string
	}{
		{
			name:        "nil_request",
			request:     nil,
			expectError: true,
			errorMsg:    "request cannot be nil",
		},
		{
			name: "missing_namespace",
			request: &pb.DeleteNamespaceRequest{
				Namespace: "",
			},
			expectError: true,
			errorMsg:    "namespace is required",
		},
		{
			name: "valid_request",
			request: &pb.DeleteNamespaceRequest{
				Namespace: "test",
			},
			expectError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validator.ValidateDeleteNamespaceRequest(tt.request)
			if tt.expectError {
				assert.Error(t, err)
				assert.Contains(t, err.Error(), tt.errorMsg)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestVectorValidator_ValidateGetNamespaceInfoRequest(t *testing.T) {
	validator := NewVectorValidator()

	tests := []struct {
		name        string
		request     *pb.GetNamespaceInfoRequest
		expectError bool
		errorMsg    string
	}{
		{
			name:        "nil_request",
			request:     nil,
			expectError: true,
			errorMsg:    "request cannot be nil",
		},
		{
			name: "missing_namespace",
			request: &pb.GetNamespaceInfoRequest{
				Namespace: "",
			},
			expectError: true,
			errorMsg:    "namespace is required",
		},
		{
			name: "valid_request",
			request: &pb.GetNamespaceInfoRequest{
				Namespace: "test",
			},
			expectError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validator.ValidateGetNamespaceInfoRequest(tt.request)
			if tt.expectError {
				assert.Error(t, err)
				assert.Contains(t, err.Error(), tt.errorMsg)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestVectorValidator_ValidateRebuildIndexRequest(t *testing.T) {
	validator := NewVectorValidator()

	tests := []struct {
		name        string
		request     *pb.RebuildIndexRequest
		expectError bool
		errorMsg    string
	}{
		{
			name:        "nil_request",
			request:     nil,
			expectError: true,
			errorMsg:    "request cannot be nil",
		},
		{
			name: "missing_namespace",
			request: &pb.RebuildIndexRequest{
				Namespace: "",
			},
			expectError: true,
			errorMsg:    "namespace is required",
		},
		{
			name: "valid_request",
			request: &pb.RebuildIndexRequest{
				Namespace: "test",
			},
			expectError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validator.ValidateRebuildIndexRequest(tt.request)
			if tt.expectError {
				assert.Error(t, err)
				assert.Contains(t, err.Error(), tt.errorMsg)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestVectorValidator_ValidateSearchRequest_WithInvalidValues(t *testing.T) {
	validator := NewVectorValidator()

	tests := []struct {
		name        string
		request     *pb.SearchSimilarRequest
		expectError bool
		errorMsg    string
	}{
		{
			name: "query_vector_with_nan",
			request: &pb.SearchSimilarRequest{
				Namespace:   "test",
				QueryVector: []float32{1.0, float32(math.NaN()), 3.0},
				K:           10,
			},
			expectError: true,
			errorMsg:    "NaN or Inf not allowed",
		},
		{
			name: "query_vector_with_inf",
			request: &pb.SearchSimilarRequest{
				Namespace:   "test",
				QueryVector: []float32{1.0, float32(math.Inf(1)), 3.0},
				K:           10,
			},
			expectError: true,
			errorMsg:    "NaN or Inf not allowed",
		},
		{
			name: "query_vector_with_negative_inf",
			request: &pb.SearchSimilarRequest{
				Namespace:   "test",
				QueryVector: []float32{1.0, float32(math.Inf(-1)), 3.0},
				K:           10,
			},
			expectError: true,
			errorMsg:    "NaN or Inf not allowed",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validator.ValidateSearchRequest(tt.request)
			if tt.expectError {
				assert.Error(t, err)
				assert.Contains(t, err.Error(), tt.errorMsg)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestVectorValidator_ValidateBatchUpsertRequest_WithInvalidValues(t *testing.T) {
	validator := NewVectorValidator()

	tests := []struct {
		name        string
		request     *pb.BatchUpsertVectorsRequest
		expectError bool
		errorMsg    string
	}{
		{
			name:        "nil_request",
			request:     nil,
			expectError: true,
			errorMsg:    "request cannot be nil",
		},
		{
			name: "empty_vectors",
			request: &pb.BatchUpsertVectorsRequest{
				Vectors: []*pb.VectorEntity{},
			},
			expectError: true,
			errorMsg:    "vectors cannot be empty",
		},
		{
			name: "vector_with_nan",
			request: &pb.BatchUpsertVectorsRequest{
				Vectors: []*pb.VectorEntity{
					{
						Namespace: "test",
						Id:        "id1",
						Vector:    []float32{1.0, float32(math.NaN()), 3.0},
					},
				},
			},
			expectError: true,
			errorMsg:    "NaN or Inf not allowed",
		},
		{
			name: "vector_with_inf",
			request: &pb.BatchUpsertVectorsRequest{
				Vectors: []*pb.VectorEntity{
					{
						Namespace: "test",
						Id:        "id1",
						Vector:    []float32{1.0, float32(math.Inf(1)), 3.0},
					},
				},
			},
			expectError: true,
			errorMsg:    "NaN or Inf not allowed",
		},
		{
			name: "different_namespaces",
			request: &pb.BatchUpsertVectorsRequest{
				Vectors: []*pb.VectorEntity{
					{
						Namespace: "test1",
						Id:        "id1",
						Vector:    []float32{1.0, 2.0, 3.0},
					},
					{
						Namespace: "test2",
						Id:        "id2",
						Vector:    []float32{1.0, 2.0, 3.0},
					},
				},
			},
			expectError: true,
			errorMsg:    "all vectors must be in the same namespace",
		},
		{
			name: "missing_namespace_in_batch",
			request: &pb.BatchUpsertVectorsRequest{
				Vectors: []*pb.VectorEntity{
					{
						Namespace: "",
						Id:        "id1",
						Vector:    []float32{1.0, 2.0, 3.0},
					},
				},
			},
			expectError: true,
			errorMsg:    "namespace is required",
		},
		{
			name: "missing_id_in_batch",
			request: &pb.BatchUpsertVectorsRequest{
				Vectors: []*pb.VectorEntity{
					{
						Namespace: "test",
						Id:        "",
						Vector:    []float32{1.0, 2.0, 3.0},
					},
				},
			},
			expectError: true,
			errorMsg:    "ID is required",
		},
		{
			name: "missing_vector_data_in_batch",
			request: &pb.BatchUpsertVectorsRequest{
				Vectors: []*pb.VectorEntity{
					{
						Namespace: "test",
						Id:        "id1",
						Vector:    []float32{},
					},
				},
			},
			expectError: true,
			errorMsg:    "vector data is required",
		},
		{
			name: "valid_batch_request",
			request: &pb.BatchUpsertVectorsRequest{
				Vectors: []*pb.VectorEntity{
					{
						Namespace: "test",
						Id:        "id1",
						Vector:    []float32{1.0, 2.0, 3.0},
					},
					{
						Namespace: "test",
						Id:        "id2",
						Vector:    []float32{4.0, 5.0, 6.0},
					},
				},
			},
			expectError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validator.ValidateBatchUpsertRequest(tt.request)
			if tt.expectError {
				assert.Error(t, err)
				assert.Contains(t, err.Error(), tt.errorMsg)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestVectorValidator_ValidateUpsertRequest_WithInvalidValues(t *testing.T) {
	validator := NewVectorValidator()

	tests := []struct {
		name        string
		request     *pb.UpsertVectorRequest
		expectError bool
		errorMsg    string
	}{
		{
			name: "vector_with_nan",
			request: &pb.UpsertVectorRequest{
				Vector: &pb.VectorEntity{
					Namespace: "test",
					Id:        "id1",
					Vector:    []float32{1.0, float32(math.NaN()), 3.0},
				},
			},
			expectError: true,
			errorMsg:    "NaN or Inf not allowed",
		},
		{
			name: "vector_with_inf",
			request: &pb.UpsertVectorRequest{
				Vector: &pb.VectorEntity{
					Namespace: "test",
					Id:        "id1",
					Vector:    []float32{1.0, float32(math.Inf(1)), 3.0},
				},
			},
			expectError: true,
			errorMsg:    "NaN or Inf not allowed",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validator.ValidateUpsertRequest(tt.request)
			if tt.expectError {
				assert.Error(t, err)
				assert.Contains(t, err.Error(), tt.errorMsg)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestVectorValidator_ValidateBatchUpsertRequest_MaxBatchSize(t *testing.T) {
	validator := NewVectorValidator()

	// Create a batch that exceeds the max size
	vectors := make([]*pb.VectorEntity, maxBatchSize+1)
	for i := 0; i < maxBatchSize+1; i++ {
		vectors[i] = &pb.VectorEntity{
			Namespace: "test",
			Id:        "id",
			Vector:    []float32{1.0, 2.0, 3.0},
		}
	}

	req := &pb.BatchUpsertVectorsRequest{
		Vectors: vectors,
	}

	err := validator.ValidateBatchUpsertRequest(req)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "exceeds maximum allowed")
}

func TestVectorValidator_ValidateUpdateMetadataRequest(t *testing.T) {
	validator := NewVectorValidator()

	tests := []struct {
		name        string
		request     *pb.UpdateMetadataRequest
		expectError bool
		errorMsg    string
	}{
		{
			name:        "nil_request",
			request:     nil,
			expectError: true,
			errorMsg:    "request cannot be nil",
		},
		{
			name: "missing_namespace",
			request: &pb.UpdateMetadataRequest{
				Namespace: "",
				Id:        "id1",
				Metadata:  map[string]string{"key": "value"},
			},
			expectError: true,
			errorMsg:    "namespace is required",
		},
		{
			name: "missing_id",
			request: &pb.UpdateMetadataRequest{
				Namespace: "test",
				Id:        "",
				Metadata:  map[string]string{"key": "value"},
			},
			expectError: true,
			errorMsg:    "vector ID is required",
		},
		{
			name: "empty_metadata",
			request: &pb.UpdateMetadataRequest{
				Namespace: "test",
				Id:        "id1",
				Metadata:  map[string]string{},
			},
			expectError: true,
			errorMsg:    "metadata cannot be empty",
		},
		{
			name: "nil_metadata",
			request: &pb.UpdateMetadataRequest{
				Namespace: "test",
				Id:        "id1",
				Metadata:  nil,
			},
			expectError: true,
			errorMsg:    "metadata cannot be empty",
		},
		{
			name: "valid_request",
			request: &pb.UpdateMetadataRequest{
				Namespace: "test",
				Id:        "id1",
				Metadata:  map[string]string{"key": "value"},
			},
			expectError: false,
		},
		{
			name: "valid_request_with_merge",
			request: &pb.UpdateMetadataRequest{
				Namespace: "test",
				Id:        "id1",
				Metadata:  map[string]string{"key": "value"},
				Merge:     true,
			},
			expectError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validator.ValidateUpdateMetadataRequest(tt.request)
			if tt.expectError {
				assert.Error(t, err)
				assert.Contains(t, err.Error(), tt.errorMsg)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestVectorValidator_ValidateGetVectorStatsRequest(t *testing.T) {
	validator := NewVectorValidator()

	tests := []struct {
		name        string
		request     *pb.GetVectorStatsRequest
		expectError bool
		errorMsg    string
	}{
		{
			name:        "nil_request",
			request:     nil,
			expectError: true,
			errorMsg:    "request cannot be nil",
		},
		{
			name: "valid_request_with_namespace",
			request: &pb.GetVectorStatsRequest{
				Namespace: "test",
			},
			expectError: false,
		},
		{
			name: "valid_request_empty_namespace_for_all_stats",
			request: &pb.GetVectorStatsRequest{
				Namespace: "",
			},
			expectError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validator.ValidateGetVectorStatsRequest(tt.request)
			if tt.expectError {
				assert.Error(t, err)
				assert.Contains(t, err.Error(), tt.errorMsg)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestVectorValidator_ValidateGetIndexProgressRequest(t *testing.T) {
	validator := NewVectorValidator()

	tests := []struct {
		name        string
		request     *pb.GetIndexProgressRequest
		expectError bool
		errorMsg    string
	}{
		{
			name:        "nil_request",
			request:     nil,
			expectError: true,
			errorMsg:    "request cannot be nil",
		},
		{
			name: "missing_namespace",
			request: &pb.GetIndexProgressRequest{
				Namespace: "",
			},
			expectError: true,
			errorMsg:    "namespace is required",
		},
		{
			name: "valid_request",
			request: &pb.GetIndexProgressRequest{
				Namespace: "test",
			},
			expectError: false,
		},
		{
			name: "valid_request_with_operation_id",
			request: &pb.GetIndexProgressRequest{
				Namespace:   "test",
				OperationId: "op-123",
			},
			expectError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validator.ValidateGetIndexProgressRequest(tt.request)
			if tt.expectError {
				assert.Error(t, err)
				assert.Contains(t, err.Error(), tt.errorMsg)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}
