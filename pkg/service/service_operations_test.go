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

package service

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// =============================================================================
// Service Lifecycle Tests
// =============================================================================

func TestServiceImpl_Close_MultipleCalls(t *testing.T) {
	svc := createTestService(t)

	// First close should succeed
	err := svc.Close()
	require.NoError(t, err)

	// Second close should be idempotent and succeed
	err = svc.Close()
	require.NoError(t, err)
}

// =============================================================================
// CheckAuth Helper Tests
// =============================================================================

// =============================================================================
// Consistency Level String Tests
// =============================================================================

func TestConsistencyLevel_StringMethod(t *testing.T) {
	tests := []struct {
		level ConsistencyLevel
		want  string
	}{
		{ConsistencyLevelUnspecified, "unspecified"},
		{ConsistencyLevelEventual, "eventual"},
		{ConsistencyLevelLinearizable, "linearizable"},
		{ConsistencyLevelStrong, "strong"},
		{ConsistencyLevel(999), "unknown"}, // Unknown value
	}

	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			got := tt.level.String()
			assert.Equal(t, tt.want, got)
		})
	}
}

// =============================================================================
// Additional Validation Tests
// =============================================================================

func TestValidatePutRequest_EmptyData(t *testing.T) {
	kvSvc := &kvService{}

	req := &PutRequest{
		EntityType: "test",
		ID:         1,
		Data:       []byte{}, // Empty but not nil - this is valid for key-value stores
	}

	err := kvSvc.validatePutRequest(req)
	require.NoError(t, err) // Empty data is valid - key-value stores allow empty values
}

func TestValidateGetRequest_ZeroID(t *testing.T) {
	kvSvc := &kvService{}

	req := &GetRequest{
		EntityType: "test",
		ID:         0, // Zero ID should be allowed
	}

	err := kvSvc.validateGetRequest(req)
	require.NoError(t, err)
}

func TestValidateDeleteRequest_ZeroID(t *testing.T) {
	kvSvc := &kvService{}

	req := &DeleteRequest{
		EntityType: "test",
		ID:         0, // Zero ID should be allowed
	}

	err := kvSvc.validateDeleteRequest(req)
	require.NoError(t, err)
}

func TestValidateListRequest_ZeroPageSize(t *testing.T) {
	kvSvc := &kvService{}

	req := &ListRequest{
		EntityType: "test",
		PageSize:   0, // Zero page size should be allowed (means default)
	}

	err := kvSvc.validateListRequest(req)
	require.NoError(t, err)
}
