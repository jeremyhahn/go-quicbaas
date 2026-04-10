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

	"github.com/jeremyhahn/go-quicbaas/pkg/auth"
	dberrors "github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
)

// =============================================================================
// Helper: create a real auth.RoleManager for testing
// =============================================================================

func createRBACTestRoleManager(t *testing.T) *auth.RoleManager {
	t.Helper()
	log, err := logger.New(logger.LogConfig{
		Level:  "error",
		Format: "json",
		Output: "stderr",
	})
	require.NoError(t, err)
	return auth.NewRoleManager(log)
}

// =============================================================================
// CreateAuthRole Tests
// =============================================================================

func TestRBACService_CreateAuthRole_Success(t *testing.T) {
	roleMgr := createRBACTestRoleManager(t)
	mockAuth := &mockAuthManager{
		roleManagerReturn: roleMgr,
	}

	svc := createTestService(t, withAuthManager(mockAuth))

	req := &AuthCreateRoleRequest{
		RoleName:    "custom-role",
		Permissions: []string{"read", "write"},
	}

	resp, err := svc.CreateAuthRole(context.Background(), req)
	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.Equal(t, "custom-role", resp.RoleID)
	assert.NotZero(t, resp.Timestamp)
}

func TestRBACService_CreateAuthRole_RoleAlreadyExists(t *testing.T) {
	roleMgr := createRBACTestRoleManager(t)
	mockAuth := &mockAuthManager{
		roleManagerReturn: roleMgr,
	}

	svc := createTestService(t, withAuthManager(mockAuth))

	req := &AuthCreateRoleRequest{
		RoleName:    "duplicate-role",
		Permissions: []string{"read"},
	}
	resp, err := svc.CreateAuthRole(context.Background(), req)
	require.NoError(t, err)
	require.NotNil(t, resp)

	// Try to create again - should fail
	resp2, err := svc.CreateAuthRole(context.Background(), req)
	require.Error(t, err)
	assert.Nil(t, resp2)

	var dErr *dberrors.DragonError
	require.True(t, dberrors.As(err, &dErr))
	assert.Equal(t, dberrors.ErrInvalidInput, dErr.Code)
}

func TestRBACService_CreateAuthRole_EmptyRoleName(t *testing.T) {
	roleMgr := createRBACTestRoleManager(t)
	mockAuth := &mockAuthManager{
		roleManagerReturn: roleMgr,
	}

	svc := createTestService(t, withAuthManager(mockAuth))

	req := &AuthCreateRoleRequest{
		RoleName: "",
	}

	resp, err := svc.CreateAuthRole(context.Background(), req)
	require.Error(t, err)
	assert.Nil(t, resp)

	var dErr *dberrors.DragonError
	require.True(t, dberrors.As(err, &dErr))
	assert.Equal(t, dberrors.ErrInvalidInput, dErr.Code)
}

func TestRBACService_CreateAuthRole_NilAuthManager(t *testing.T) {
	svc := createTestService(t)
	db := svc.(*quicBaaS)
	db.authManager = nil

	req := &AuthCreateRoleRequest{
		RoleName:    "orphan-role",
		Permissions: []string{"read"},
	}

	resp, err := svc.CreateAuthRole(context.Background(), req)
	require.Error(t, err)
	assert.Nil(t, resp)

	var dErr *dberrors.DragonError
	require.True(t, dberrors.As(err, &dErr))
	assert.Equal(t, dberrors.ErrConfiguration, dErr.Code)
}

func TestRBACService_CreateAuthRole_InvalidRoleManagerType(t *testing.T) {
	// Return nil role manager which fails the type assertion
	mockAuth := &mockAuthManager{
		roleManagerReturn: nil,
	}

	svc := createTestService(t, withAuthManager(mockAuth))

	req := &AuthCreateRoleRequest{
		RoleName:    "test-role",
		Permissions: []string{"read"},
	}

	resp, err := svc.CreateAuthRole(context.Background(), req)
	require.Error(t, err)
	assert.Nil(t, resp)
}

// =============================================================================
// DeleteAuthRole Tests
// =============================================================================

func TestRBACService_DeleteAuthRole_Success(t *testing.T) {
	roleMgr := createRBACTestRoleManager(t)
	mockAuth := &mockAuthManager{
		roleManagerReturn: roleMgr,
	}

	svc := createTestService(t, withAuthManager(mockAuth))

	// Create a role first
	createReq := &AuthCreateRoleRequest{
		RoleName:    "deletable-role",
		Permissions: []string{"read"},
	}
	_, err := svc.CreateAuthRole(context.Background(), createReq)
	require.NoError(t, err)

	// Delete it
	deleteReq := &AuthDeleteRoleRequest{
		RoleName: "deletable-role",
	}

	resp, err := svc.DeleteAuthRole(context.Background(), deleteReq)
	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.True(t, resp.Success)
}

func TestRBACService_DeleteAuthRole_NotFound(t *testing.T) {
	roleMgr := createRBACTestRoleManager(t)
	mockAuth := &mockAuthManager{
		roleManagerReturn: roleMgr,
	}

	svc := createTestService(t, withAuthManager(mockAuth))

	req := &AuthDeleteRoleRequest{
		RoleName: "nonexistent-role",
	}

	resp, err := svc.DeleteAuthRole(context.Background(), req)
	require.Error(t, err)
	assert.Nil(t, resp)

	var dErr *dberrors.DragonError
	require.True(t, dberrors.As(err, &dErr))
	assert.Equal(t, dberrors.ErrNotFound, dErr.Code)
}

func TestRBACService_DeleteAuthRole_EmptyRoleName(t *testing.T) {
	roleMgr := createRBACTestRoleManager(t)
	mockAuth := &mockAuthManager{
		roleManagerReturn: roleMgr,
	}

	svc := createTestService(t, withAuthManager(mockAuth))

	req := &AuthDeleteRoleRequest{
		RoleName: "",
	}

	resp, err := svc.DeleteAuthRole(context.Background(), req)
	require.Error(t, err)
	assert.Nil(t, resp)

	var dErr *dberrors.DragonError
	require.True(t, dberrors.As(err, &dErr))
	assert.Equal(t, dberrors.ErrInvalidInput, dErr.Code)
}

func TestRBACService_DeleteAuthRole_NilAuthManager(t *testing.T) {
	svc := createTestService(t)
	db := svc.(*quicBaaS)
	db.authManager = nil

	req := &AuthDeleteRoleRequest{
		RoleName: "orphan-role",
	}

	resp, err := svc.DeleteAuthRole(context.Background(), req)
	require.Error(t, err)
	assert.Nil(t, resp)

	var dErr *dberrors.DragonError
	require.True(t, dberrors.As(err, &dErr))
	assert.Equal(t, dberrors.ErrConfiguration, dErr.Code)
}

func TestRBACService_DeleteAuthRole_InvalidRoleManagerType(t *testing.T) {
	mockAuth := &mockAuthManager{
		roleManagerReturn: nil,
	}

	svc := createTestService(t, withAuthManager(mockAuth))

	req := &AuthDeleteRoleRequest{
		RoleName: "test-role",
	}

	resp, err := svc.DeleteAuthRole(context.Background(), req)
	require.Error(t, err)
	assert.Nil(t, resp)
}

// =============================================================================
// CreateAuthRole - With Multiple Permissions
// =============================================================================

func TestRBACService_CreateAuthRole_MultiplePermissions(t *testing.T) {
	roleMgr := createRBACTestRoleManager(t)
	mockAuth := &mockAuthManager{
		roleManagerReturn: roleMgr,
	}

	svc := createTestService(t, withAuthManager(mockAuth))

	req := &AuthCreateRoleRequest{
		RoleName:    "multi-perm-role",
		Permissions: []string{"read", "write", "delete", "admin"},
	}

	resp, err := svc.CreateAuthRole(context.Background(), req)
	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.Equal(t, "multi-perm-role", resp.RoleID)

	// Verify the role exists in the manager via the RoleInfo interface
	role := roleMgr.GetRole("multi-perm-role")
	require.NotNil(t, role)
	assert.Equal(t, "multi-perm-role", role.GetName())
}
