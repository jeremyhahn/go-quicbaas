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
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/metadata"

	"github.com/jeremyhahn/go-quicbaas/pkg/errors"
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

func TestCheckAuth_AuthDisabled(t *testing.T) {
	// Create service with auth disabled
	config := &ServiceConfig{
		NodeID:     1,
		Version:    "1.0.0",
		EnableAuth: false,
	}

	svc, err := NewServiceBuilder().
		WithRaftNode(&mockRaftNode{}).
		WithConfig(config).
		WithLogger(createTestLogger(t)).
		Build()

	require.NoError(t, err)
	defer svc.Close()

	impl, ok := svc.(*quicBaaS)
	require.True(t, ok)

	// Should succeed because auth is disabled
	err = impl.checkAuth(context.Background(), "read")
	require.NoError(t, err)
}

func TestCheckAuth_AuthEnabled_NoToken(t *testing.T) {
	// Create service with auth enabled but no auth manager
	config := &ServiceConfig{
		NodeID:     1,
		Version:    "1.0.0",
		EnableAuth: true,
	}

	svc, err := NewServiceBuilder().
		WithRaftNode(&mockRaftNode{}).
		WithConfig(config).
		WithLogger(createTestLogger(t)).
		Build()

	require.NoError(t, err)
	defer svc.Close()

	impl, ok := svc.(*quicBaaS)
	require.True(t, ok)

	// Should fail because auth is enabled and no token is provided
	err = impl.checkAuth(context.Background(), "read")
	require.Error(t, err)

	// Verify the error is an authorization error
	var qbErr *errors.QBError
	require.ErrorAs(t, err, &qbErr)
	assert.Equal(t, errors.ErrUnauthorized, qbErr.Code)
}

func TestCheckAuth_AuthEnabled_WithToken_NoManager(t *testing.T) {
	// Create service with auth enabled but no auth manager
	config := &ServiceConfig{
		NodeID:     1,
		Version:    "1.0.0",
		EnableAuth: true,
	}

	svc, err := NewServiceBuilder().
		WithRaftNode(&mockRaftNode{}).
		WithConfig(config).
		WithLogger(createTestLogger(t)).
		Build()

	require.NoError(t, err)
	defer svc.Close()

	impl, ok := svc.(*quicBaaS)
	require.True(t, ok)

	// Create context with token in metadata
	md := metadata.Pairs("authorization", "Bearer test-token")
	ctx := metadata.NewIncomingContext(context.Background(), md)

	// Should succeed because token is provided and auth manager is nil
	// (no validation happens when auth manager is not configured)
	err = impl.checkAuth(ctx, "read")
	require.NoError(t, err)
}

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
