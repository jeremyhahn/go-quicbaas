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

//go:build grpc

package grpc

import (
	"testing"

	"github.com/stretchr/testify/assert"

	pb "github.com/jeremyhahn/go-quicbaas/api/proto"
)

// =============================================================================
// Distance Metric Conversion Tests
// =============================================================================

func TestProtoDistanceMetricToString(t *testing.T) {
	tests := []struct {
		name     string
		input    pb.DistanceMetric
		expected string
	}{
		{
			name:     "L2 metric",
			input:    pb.DistanceMetric_DISTANCE_METRIC_L2,
			expected: "L2",
		},
		{
			name:     "COSINE metric",
			input:    pb.DistanceMetric_DISTANCE_METRIC_COSINE,
			expected: "COSINE",
		},
		{
			name:     "DOT_PRODUCT metric",
			input:    pb.DistanceMetric_DISTANCE_METRIC_DOT_PRODUCT,
			expected: "DOT_PRODUCT",
		},
		{
			name:     "MANHATTAN metric",
			input:    pb.DistanceMetric_DISTANCE_METRIC_MANHATTAN,
			expected: "MANHATTAN",
		},
		{
			name:     "unspecified defaults to L2",
			input:    pb.DistanceMetric_DISTANCE_METRIC_UNSPECIFIED,
			expected: "L2",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := protoDistanceMetricToString(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestStringToProtoDistanceMetric(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected pb.DistanceMetric
	}{
		{
			name:     "L2 string",
			input:    "L2",
			expected: pb.DistanceMetric_DISTANCE_METRIC_L2,
		},
		{
			name:     "COSINE string",
			input:    "COSINE",
			expected: pb.DistanceMetric_DISTANCE_METRIC_COSINE,
		},
		{
			name:     "DOT_PRODUCT string",
			input:    "DOT_PRODUCT",
			expected: pb.DistanceMetric_DISTANCE_METRIC_DOT_PRODUCT,
		},
		{
			name:     "MANHATTAN string",
			input:    "MANHATTAN",
			expected: pb.DistanceMetric_DISTANCE_METRIC_MANHATTAN,
		},
		{
			name:     "unknown defaults to L2",
			input:    "UNKNOWN",
			expected: pb.DistanceMetric_DISTANCE_METRIC_L2,
		},
		{
			name:     "empty defaults to L2",
			input:    "",
			expected: pb.DistanceMetric_DISTANCE_METRIC_L2,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := stringToProtoDistanceMetric(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

// =============================================================================
// Index Type Conversion Tests
// =============================================================================

func TestProtoIndexTypeToString(t *testing.T) {
	tests := []struct {
		name     string
		input    pb.IndexType
		expected string
	}{
		{
			name:     "HNSW index",
			input:    pb.IndexType_INDEX_TYPE_HNSW,
			expected: "HNSW",
		},
		{
			name:     "FLAT index",
			input:    pb.IndexType_INDEX_TYPE_FLAT,
			expected: "FLAT",
		},
		{
			name:     "IVF_FLAT index",
			input:    pb.IndexType_INDEX_TYPE_IVF_FLAT,
			expected: "IVF_FLAT",
		},
		{
			name:     "IVF_PQ index",
			input:    pb.IndexType_INDEX_TYPE_IVF_PQ,
			expected: "IVF_PQ",
		},
		{
			name:     "unspecified defaults to HNSW",
			input:    pb.IndexType_INDEX_TYPE_UNSPECIFIED,
			expected: "HNSW",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := protoIndexTypeToString(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestStringToProtoIndexType(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected pb.IndexType
	}{
		{
			name:     "HNSW string",
			input:    "HNSW",
			expected: pb.IndexType_INDEX_TYPE_HNSW,
		},
		{
			name:     "FLAT string",
			input:    "FLAT",
			expected: pb.IndexType_INDEX_TYPE_FLAT,
		},
		{
			name:     "IVF_FLAT string",
			input:    "IVF_FLAT",
			expected: pb.IndexType_INDEX_TYPE_IVF_FLAT,
		},
		{
			name:     "IVF_PQ string",
			input:    "IVF_PQ",
			expected: pb.IndexType_INDEX_TYPE_IVF_PQ,
		},
		{
			name:     "unknown defaults to HNSW",
			input:    "UNKNOWN",
			expected: pb.IndexType_INDEX_TYPE_HNSW,
		},
		{
			name:     "empty defaults to HNSW",
			input:    "",
			expected: pb.IndexType_INDEX_TYPE_HNSW,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := stringToProtoIndexType(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}
