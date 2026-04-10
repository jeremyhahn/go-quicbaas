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
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	pb "github.com/jeremyhahn/go-quicbaas/api/proto"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
)

func TestParseVectorID(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected uint64
	}{
		{
			name:     "numeric ID",
			input:    "12345",
			expected: 12345,
		},
		{
			name:     "large numeric ID",
			input:    "18446744073709551615", // max uint64
			expected: 18446744073709551615,
		},
		{
			name:  "string ID",
			input: "test-id",
		},
		{
			name:  "empty string",
			input: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := parseVectorID(tt.input)
			if tt.name == "numeric ID" || tt.name == "large numeric ID" {
				assert.Equal(t, tt.expected, result)
			} else if tt.name == "empty string" {
				assert.Equal(t, uint64(0), result)
			} else {
				assert.NotEqual(t, uint64(0), result)
			}
		})
	}
}

func TestFormatVectorID(t *testing.T) {
	tests := []struct {
		name     string
		id       uint64
		metadata map[string]string
		expected string
	}{
		{
			name:     "numeric ID without metadata",
			id:       12345,
			metadata: nil,
			expected: "12345",
		},
		{
			name:     "ID with original metadata",
			id:       99999,
			metadata: map[string]string{MetadataKeyOriginalID: "test-id"},
			expected: "test-id",
		},
		{
			name:     "ID with other metadata",
			id:       54321,
			metadata: map[string]string{"key": "value"},
			expected: "54321",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := formatVectorID(tt.id, tt.metadata)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestStoreOriginalID(t *testing.T) {
	tests := []struct {
		name        string
		id          string
		initialMeta map[string]interface{}
		shouldStore bool
	}{
		{
			name:        "numeric ID not stored",
			id:          "12345",
			initialMeta: make(map[string]interface{}),
			shouldStore: false,
		},
		{
			name:        "string ID stored",
			id:          "test-id",
			initialMeta: make(map[string]interface{}),
			shouldStore: true,
		},
		{
			name:        "UUID stored",
			id:          "550e8400-e29b-41d4-a716-446655440000",
			initialMeta: make(map[string]interface{}),
			shouldStore: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			metadata := tt.initialMeta
			storeOriginalID(tt.id, metadata)

			if tt.shouldStore {
				assert.Contains(t, metadata, MetadataKeyOriginalID)
				assert.Equal(t, tt.id, metadata[MetadataKeyOriginalID])
			} else {
				assert.NotContains(t, metadata, MetadataKeyOriginalID)
			}
		})
	}
}

func TestConvertProtoConsistencyLevel(t *testing.T) {
	tests := []struct {
		name     string
		input    pb.VectorConsistencyLevel
		expected service.ConsistencyLevel
	}{
		{
			name:     "ALL to Strong",
			input:    pb.VectorConsistencyLevel_VECTOR_CONSISTENCY_LEVEL_ALL,
			expected: service.ConsistencyLevelStrong,
		},
		{
			name:     "QUORUM to Linearizable",
			input:    pb.VectorConsistencyLevel_VECTOR_CONSISTENCY_LEVEL_QUORUM,
			expected: service.ConsistencyLevelLinearizable,
		},
		{
			name:     "ONE to Eventual",
			input:    pb.VectorConsistencyLevel_VECTOR_CONSISTENCY_LEVEL_ONE,
			expected: service.ConsistencyLevelEventual,
		},
		{
			name:     "UNSPECIFIED defaults to Linearizable",
			input:    pb.VectorConsistencyLevel_VECTOR_CONSISTENCY_LEVEL_UNSPECIFIED,
			expected: service.ConsistencyLevelLinearizable,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := convertProtoConsistencyLevel(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestParseVectorID_EdgeCases(t *testing.T) {
	// Test with very long string
	longString := make([]byte, 1000)
	for i := range longString {
		longString[i] = 'a'
	}

	id := parseVectorID(string(longString))
	assert.NotEqual(t, uint64(0), id)

	// Test with special characters
	specialID := "test-id-with-special-chars!@#$%^&*()"
	id = parseVectorID(specialID)
	assert.NotEqual(t, uint64(0), id)

	// Test with unicode characters
	unicodeID := "测试-id-тест"
	id = parseVectorID(unicodeID)
	assert.NotEqual(t, uint64(0), id)
}

func TestFormatVectorID_EmptyMetadata(t *testing.T) {
	// Test with empty but non-nil metadata
	metadata := map[string]string{}
	result := formatVectorID(12345, metadata)
	assert.Equal(t, "12345", result)
}

func TestStoreOriginalID_WithExistingMetadata(t *testing.T) {
	// Test that storeOriginalID doesn't overwrite existing metadata
	metadata := map[string]interface{}{
		"existing_key": "existing_value",
	}

	storeOriginalID("test-id", metadata)

	assert.Contains(t, metadata, "existing_key")
	assert.Equal(t, "existing_value", metadata["existing_key"])
	assert.Contains(t, metadata, MetadataKeyOriginalID)
	assert.Equal(t, "test-id", metadata[MetadataKeyOriginalID])
}

func TestParseVectorID_Consistency(t *testing.T) {
	// Test that same input always produces same output
	id1 := "consistent-test-id"

	result1 := parseVectorID(id1)
	result2 := parseVectorID(id1)

	assert.Equal(t, result1, result2, "parseVectorID should be deterministic")
}

func TestParseVectorID_DifferentInputs(t *testing.T) {
	// Test that different inputs produce different outputs
	id1 := parseVectorID("id-one")
	id2 := parseVectorID("id-two")

	assert.NotEqual(t, id1, id2, "different strings should produce different hashes")
}

func TestFormatVectorID_LargeNumbers(t *testing.T) {
	tests := []struct {
		name     string
		id       uint64
		expected string
	}{
		{
			name:     "max uint64",
			id:       18446744073709551615,
			expected: "18446744073709551615",
		},
		{
			name:     "zero",
			id:       0,
			expected: "0",
		},
		{
			name:     "one",
			id:       1,
			expected: "1",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := formatVectorID(tt.id, nil)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestStoreOriginalID_BoundaryValues(t *testing.T) {
	tests := []struct {
		name        string
		id          string
		shouldStore bool
	}{
		{
			name:        "zero",
			id:          "0",
			shouldStore: false,
		},
		{
			name:        "max uint64 as string",
			id:          "18446744073709551615",
			shouldStore: false,
		},
		{
			name:        "max uint64 + 1 as string (overflow)",
			id:          "18446744073709551616",
			shouldStore: true, // Will fail to parse as uint64, so it's stored
		},
		{
			name:        "single char",
			id:          "a",
			shouldStore: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			metadata := make(map[string]interface{})
			storeOriginalID(tt.id, metadata)

			if tt.shouldStore {
				require.Contains(t, metadata, MetadataKeyOriginalID)
			} else {
				assert.NotContains(t, metadata, MetadataKeyOriginalID)
			}
		})
	}
}

func TestMetadataKeyOriginalID_Constant(t *testing.T) {
	// Verify the constant value
	assert.Equal(t, "__original_id", MetadataKeyOriginalID)
}
