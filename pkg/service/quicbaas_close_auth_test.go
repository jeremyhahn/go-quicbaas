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
	stderrors "errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/metadata"
)

// =============================================================================
// Close Deep Path Tests
// =============================================================================

func TestQuicBaaS_Close_WithAllComponentsNoError(t *testing.T) {
	mockRaft := &mockRaftNode{}
	mockBackup := &mockBackupManager{}

	svc := createTestService(t,
		withMockRaftNode(mockRaft),
		withBackupManager(mockBackup),
	)

	db := svc.(*quicBaaS)
	err := db.Close()
	require.NoError(t, err)
	assert.True(t, db.closed)
}

func TestQuicBaaS_Close_AlreadyClosedIdempotent(t *testing.T) {
	svc := createTestService(t)

	db := svc.(*quicBaaS)

	err1 := db.Close()
	require.NoError(t, err1)
	assert.True(t, db.closed)

	// Second call should be idempotent
	err2 := db.Close()
	require.NoError(t, err2)
}

func TestQuicBaaS_Close_WithBackupManagerCleanup(t *testing.T) {
	mockBackup := &mockBackupManager{}

	svc := createTestService(t, withBackupManager(mockBackup))

	db := svc.(*quicBaaS)
	err := db.Close()
	require.NoError(t, err)
	assert.True(t, db.closed)
}

func TestQuicBaaS_Close_NilLoggerDoesNotPanic(t *testing.T) {
	svc := createTestService(t)
	db := svc.(*quicBaaS)
	db.logger = nil

	err := db.Close()
	require.NoError(t, err)
	assert.True(t, db.closed)
}

// =============================================================================
// checkAuth Deep Path Tests
// =============================================================================

func TestQuicBaaS_CheckAuth_AuthDisabledAlwaysPasses(t *testing.T) {
	svc := createTestService(t)
	db := svc.(*quicBaaS)
	db.config.EnableAuth = false

	err := db.checkAuth(context.Background(), "read")
	require.NoError(t, err)
}

func TestQuicBaaS_CheckAuth_AuthEnabled_EmptyTokenUnauthorized(t *testing.T) {
	svc := createTestService(t)
	db := svc.(*quicBaaS)
	db.config.EnableAuth = true

	// Context without any metadata - ExtractTokenFromContext returns "", nil
	// checkAuth treats "" token as unauthorized
	err := db.checkAuth(context.Background(), "read")
	require.Error(t, err)
}

func TestQuicBaaS_CheckAuth_AuthEnabled_ValidTokenAuthorized(t *testing.T) {
	mockAuth := &mockAuthManager{
		authorizeFn: func(ctx context.Context, token, resource, permission string) error {
			return nil // Authorized
		},
	}

	svc := createTestService(t, withAuthManager(mockAuth))
	db := svc.(*quicBaaS)
	db.config.EnableAuth = true

	// Add token via gRPC incoming metadata
	md := metadata.New(map[string]string{
		"authorization": "Bearer valid-token-123",
	})
	ctx := metadata.NewIncomingContext(context.Background(), md)

	err := db.checkAuth(ctx, "read")
	require.NoError(t, err)
}

func TestQuicBaaS_CheckAuth_AuthEnabled_AuthorizationDenied(t *testing.T) {
	mockAuth := &mockAuthManager{
		authorizeFn: func(ctx context.Context, token, resource, permission string) error {
			return stderrors.New("forbidden")
		},
	}

	svc := createTestService(t, withAuthManager(mockAuth))
	db := svc.(*quicBaaS)
	db.config.EnableAuth = true

	md := metadata.New(map[string]string{
		"authorization": "Bearer bad-token-456",
	})
	ctx := metadata.NewIncomingContext(context.Background(), md)

	err := db.checkAuth(ctx, "write")
	require.Error(t, err)
}

func TestQuicBaaS_CheckAuth_AuthEnabled_NoAuthManagerPassesWithToken(t *testing.T) {
	svc := createTestService(t)
	db := svc.(*quicBaaS)
	db.config.EnableAuth = true
	db.authManager = nil

	md := metadata.New(map[string]string{
		"authorization": "Bearer some-token",
	})
	ctx := metadata.NewIncomingContext(context.Background(), md)

	// With token but no auth manager, checkAuth should pass (no manager to validate)
	err := db.checkAuth(ctx, "read")
	require.NoError(t, err)
}

func TestQuicBaaS_CheckAuth_AuthEnabled_XAuthTokenHeader(t *testing.T) {
	mockAuth := &mockAuthManager{
		authorizeFn: func(ctx context.Context, token, resource, permission string) error {
			return nil
		},
	}

	svc := createTestService(t, withAuthManager(mockAuth))
	db := svc.(*quicBaaS)
	db.config.EnableAuth = true

	// Use x-auth-token header instead of authorization
	md := metadata.New(map[string]string{
		"x-auth-token": "my-api-key",
	})
	ctx := metadata.NewIncomingContext(context.Background(), md)

	err := db.checkAuth(ctx, "read")
	require.NoError(t, err)
}

// =============================================================================
// GetUpgradeStatus Error Paths
// =============================================================================

func TestOperationsService_GetUpgradeStatus_NilRequestFails(t *testing.T) {
	svc := createTestService(t)
	resp, err := svc.GetUpgradeStatus(context.Background(), nil)
	assert.Nil(t, resp)
	require.Error(t, err)
}

func TestOperationsService_GetUpgradeStatus_EmptyJobIDFails(t *testing.T) {
	svc := createTestService(t)
	resp, err := svc.GetUpgradeStatus(context.Background(), &GetUpgradeStatusRequest{JobID: ""})
	assert.Nil(t, resp)
	require.Error(t, err)
}

func TestOperationsService_GetUpgradeStatus_JobNotFoundFails(t *testing.T) {
	svc := createTestService(t)
	resp, err := svc.GetUpgradeStatus(context.Background(), &GetUpgradeStatusRequest{JobID: "nonexistent"})
	assert.Nil(t, resp)
	require.Error(t, err)
}
