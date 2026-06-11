//go:build mcp

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

package mcp

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	dberrors "github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/pkg/iam"
)

// adminMockQuicBaaS embeds mockService and implements adminManagerAccessor.
type adminMockQuicBaaS struct {
	mockService
	adminMgr *iam.AdminManager
}

// GetAdminManager implements the adminManagerAccessor interface.
func (m *adminMockQuicBaaS) GetAdminManager(_ context.Context) (*iam.AdminManager, error) {
	return m.adminMgr, nil
}

// newAdminMockWithManager creates a mock service with a real AdminManager
// backed by in-memory storage.
func newAdminMockWithManager(t *testing.T) *adminMockQuicBaaS {
	t.Helper()
	mgr, err := iam.NewAdminManager(iam.AdminManagerOptions{
		AdminStorage:         iam.NewMemoryAdminStorage(),
		ImpersonationStorage: iam.NewMemoryImpersonationStorage(),
	})
	require.NoError(t, err)
	return &adminMockQuicBaaS{adminMgr: mgr}
}

// newAdminMockWithoutManager creates a mock service without an AdminManager
// to test service unavailable scenarios.
func newAdminMockWithoutManager() *adminMockQuicBaaS {
	return &adminMockQuicBaaS{adminMgr: nil}
}

// =============================================================================
// Admin Assignment - Service Unavailable Tests
// =============================================================================

func TestExecuteIAMAdminAssignmentCreate_ServiceUnavailable(t *testing.T) {
	log := newTestLogger(t)
	// Use a service that doesn't implement adminManagerAccessor
	reg := &ToolRegistry{service: &mockService{}, logger: log}

	_, err := reg.executeIAMAdminAssignmentCreate(context.Background(), map[string]interface{}{
		"user_id": "user-1", "admin_role": "super_admin", "granted_by": "admin",
	})
	require.Error(t, err)
	var dErr *dberrors.QBError
	assert.ErrorAs(t, err, &dErr)
}

func TestExecuteIAMAdminAssignmentCreate_NilManager(t *testing.T) {
	log := newTestLogger(t)
	reg := &ToolRegistry{service: newAdminMockWithoutManager(), logger: log}

	_, err := reg.executeIAMAdminAssignmentCreate(context.Background(), map[string]interface{}{
		"user_id": "user-1", "admin_role": "super_admin", "granted_by": "admin",
	})
	require.Error(t, err)
	var dErr *dberrors.QBError
	assert.ErrorAs(t, err, &dErr)
}

func TestExecuteIAMAdminAssignmentGet_ServiceUnavailable(t *testing.T) {
	log := newTestLogger(t)
	reg := &ToolRegistry{service: &mockService{}, logger: log}

	_, err := reg.executeIAMAdminAssignmentGet(context.Background(), map[string]interface{}{
		"assignment_id": "test-id",
	})
	require.Error(t, err)
}

func TestExecuteIAMAdminAssignmentUpdate_ServiceUnavailable(t *testing.T) {
	log := newTestLogger(t)
	reg := &ToolRegistry{service: &mockService{}, logger: log}

	_, err := reg.executeIAMAdminAssignmentUpdate(context.Background(), map[string]interface{}{
		"assignment_id": "test-id",
	})
	require.Error(t, err)
}

func TestExecuteIAMAdminAssignmentDelete_ServiceUnavailable(t *testing.T) {
	log := newTestLogger(t)
	reg := &ToolRegistry{service: &mockService{}, logger: log}

	_, err := reg.executeIAMAdminAssignmentDelete(context.Background(), map[string]interface{}{
		"assignment_id": "test-id",
	})
	require.Error(t, err)
}

func TestExecuteIAMAdminAssignmentList_ServiceUnavailable(t *testing.T) {
	log := newTestLogger(t)
	reg := &ToolRegistry{service: &mockService{}, logger: log}

	_, err := reg.executeIAMAdminAssignmentList(context.Background(), map[string]interface{}{})
	require.Error(t, err)
}

func TestExecuteIAMAdminCheckPermission_ServiceUnavailable(t *testing.T) {
	log := newTestLogger(t)
	reg := &ToolRegistry{service: &mockService{}, logger: log}

	_, err := reg.executeIAMAdminCheckPermission(context.Background(), map[string]interface{}{
		"user_id": "user-1", "permission": "user:create",
	})
	require.Error(t, err)
}

func TestExecuteIAMImpersonationStart_ServiceUnavailable(t *testing.T) {
	log := newTestLogger(t)
	reg := &ToolRegistry{service: &mockService{}, logger: log}

	_, err := reg.executeIAMImpersonationStart(context.Background(), map[string]interface{}{
		"admin_user_id": "admin", "target_user_id": "user-1",
		"tenant_id": "t1", "reason": "support",
	})
	require.Error(t, err)
}

func TestExecuteIAMImpersonationEnd_ServiceUnavailable(t *testing.T) {
	log := newTestLogger(t)
	reg := &ToolRegistry{service: &mockService{}, logger: log}

	_, err := reg.executeIAMImpersonationEnd(context.Background(), map[string]interface{}{
		"session_id": "imp-1",
	})
	require.Error(t, err)
}

func TestExecuteIAMImpersonationList_ServiceUnavailable(t *testing.T) {
	log := newTestLogger(t)
	reg := &ToolRegistry{service: &mockService{}, logger: log}

	_, err := reg.executeIAMImpersonationList(context.Background(), map[string]interface{}{
		"admin_user_id": "admin",
	})
	require.Error(t, err)
}

// =============================================================================
// Admin Assignment - Success Tests
// =============================================================================

func TestExecuteIAMAdminAssignmentCreate_Success(t *testing.T) {
	log := newTestLogger(t)
	mock := newAdminMockWithManager(t)
	reg := &ToolRegistry{service: mock, logger: log}

	result, err := reg.executeIAMAdminAssignmentCreate(context.Background(), map[string]interface{}{
		"user_id":    "user-1",
		"admin_role": "super_admin",
		"granted_by": "admin-user",
		"tenant_ids": []interface{}{"t1", "t2"},
	})
	require.NoError(t, err)
	require.NotNil(t, result)

	resultMap, ok := result.(map[string]interface{})
	require.True(t, ok, "result should be a map")
	assert.Equal(t, "user-1", resultMap["user_id"])
	assert.Equal(t, "super_admin", resultMap["admin_role"])
	assert.Equal(t, true, resultMap["active"])
	assert.NotEmpty(t, resultMap["id"])
}

func TestExecuteIAMAdminAssignmentCreate_InvalidExpiration(t *testing.T) {
	log := newTestLogger(t)
	mock := newAdminMockWithManager(t)
	reg := &ToolRegistry{service: mock, logger: log}

	_, err := reg.executeIAMAdminAssignmentCreate(context.Background(), map[string]interface{}{
		"user_id":    "user-1",
		"admin_role": "super_admin",
		"granted_by": "admin-user",
		"expires_at": "not-a-date",
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "expires_at")
}

func TestExecuteIAMAdminAssignmentCreate_MissingRequired(t *testing.T) {
	log := newTestLogger(t)
	mock := newAdminMockWithManager(t)
	reg := &ToolRegistry{service: mock, logger: log}

	// Missing user_id
	_, err := reg.executeIAMAdminAssignmentCreate(context.Background(), map[string]interface{}{
		"admin_role": "super_admin",
		"granted_by": "admin-user",
	})
	require.Error(t, err)
}

func TestExecuteIAMAdminAssignmentGet_Success(t *testing.T) {
	log := newTestLogger(t)
	mock := newAdminMockWithManager(t)
	reg := &ToolRegistry{service: mock, logger: log}

	// Create an assignment first
	createResult, err := reg.executeIAMAdminAssignmentCreate(context.Background(), map[string]interface{}{
		"user_id":    "user-1",
		"admin_role": "super_admin",
		"granted_by": "admin-user",
	})
	require.NoError(t, err)
	createMap := createResult.(map[string]interface{})
	assignmentID := createMap["id"].(string)

	// Get the assignment
	result, err := reg.executeIAMAdminAssignmentGet(context.Background(), map[string]interface{}{
		"assignment_id": assignmentID,
	})
	require.NoError(t, err)
	require.NotNil(t, result)

	resultMap, ok := result.(map[string]interface{})
	require.True(t, ok)
	assert.Equal(t, assignmentID, resultMap["id"])
}

func TestExecuteIAMAdminAssignmentGet_NotFound(t *testing.T) {
	log := newTestLogger(t)
	mock := newAdminMockWithManager(t)
	reg := &ToolRegistry{service: mock, logger: log}

	_, err := reg.executeIAMAdminAssignmentGet(context.Background(), map[string]interface{}{
		"assignment_id": "nonexistent",
	})
	require.Error(t, err)
}

func TestExecuteIAMAdminAssignmentUpdate_Success(t *testing.T) {
	log := newTestLogger(t)
	mock := newAdminMockWithManager(t)
	reg := &ToolRegistry{service: mock, logger: log}

	// Create an assignment first
	createResult, err := reg.executeIAMAdminAssignmentCreate(context.Background(), map[string]interface{}{
		"user_id":    "user-1",
		"admin_role": "super_admin",
		"granted_by": "admin-user",
	})
	require.NoError(t, err)
	createMap := createResult.(map[string]interface{})
	assignmentID := createMap["id"].(string)

	// Update the assignment
	result, err := reg.executeIAMAdminAssignmentUpdate(context.Background(), map[string]interface{}{
		"assignment_id": assignmentID,
		"admin_role":    "tenant_admin",
		"tenant_ids":    []interface{}{"t1"},
	})
	require.NoError(t, err)
	require.NotNil(t, result)

	resultMap, ok := result.(map[string]interface{})
	require.True(t, ok)
	assert.Equal(t, "tenant_admin", resultMap["admin_role"])
}

func TestExecuteIAMAdminAssignmentUpdate_InvalidExpiration(t *testing.T) {
	log := newTestLogger(t)
	mock := newAdminMockWithManager(t)
	reg := &ToolRegistry{service: mock, logger: log}

	_, err := reg.executeIAMAdminAssignmentUpdate(context.Background(), map[string]interface{}{
		"assignment_id": "test-id",
		"expires_at":    "invalid-date",
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "expires_at")
}

func TestExecuteIAMAdminAssignmentDelete_Success(t *testing.T) {
	log := newTestLogger(t)
	mock := newAdminMockWithManager(t)
	reg := &ToolRegistry{service: mock, logger: log}

	// Create an assignment first
	createResult, err := reg.executeIAMAdminAssignmentCreate(context.Background(), map[string]interface{}{
		"user_id":    "user-1",
		"admin_role": "super_admin",
		"granted_by": "admin-user",
	})
	require.NoError(t, err)
	createMap := createResult.(map[string]interface{})
	assignmentID := createMap["id"].(string)

	// Delete the assignment
	result, err := reg.executeIAMAdminAssignmentDelete(context.Background(), map[string]interface{}{
		"assignment_id": assignmentID,
	})
	require.NoError(t, err)
	require.NotNil(t, result)

	resultMap, ok := result.(map[string]interface{})
	require.True(t, ok)
	assert.Equal(t, true, resultMap["deleted"])
}

func TestExecuteIAMAdminAssignmentDelete_NotFound(t *testing.T) {
	log := newTestLogger(t)
	mock := newAdminMockWithManager(t)
	reg := &ToolRegistry{service: mock, logger: log}

	_, err := reg.executeIAMAdminAssignmentDelete(context.Background(), map[string]interface{}{
		"assignment_id": "nonexistent",
	})
	require.Error(t, err)
}

func TestExecuteIAMAdminAssignmentList_Success(t *testing.T) {
	log := newTestLogger(t)
	mock := newAdminMockWithManager(t)
	reg := &ToolRegistry{service: mock, logger: log}

	// Create an assignment
	_, err := reg.executeIAMAdminAssignmentCreate(context.Background(), map[string]interface{}{
		"user_id":    "user-1",
		"admin_role": "super_admin",
		"granted_by": "admin-user",
	})
	require.NoError(t, err)

	// List assignments
	result, err := reg.executeIAMAdminAssignmentList(context.Background(), map[string]interface{}{})
	require.NoError(t, err)
	require.NotNil(t, result)

	resultMap, ok := result.(map[string]interface{})
	require.True(t, ok)
	assert.NotNil(t, resultMap["assignments"])
}

func TestExecuteIAMAdminAssignmentList_DefaultLimit(t *testing.T) {
	log := newTestLogger(t)
	mock := newAdminMockWithManager(t)
	reg := &ToolRegistry{service: mock, logger: log}

	// List with no explicit limit (should use default of 100)
	result, err := reg.executeIAMAdminAssignmentList(context.Background(), map[string]interface{}{
		"active_only": false,
	})
	require.NoError(t, err)
	require.NotNil(t, result)
}

func TestExecuteIAMAdminCheckPermission_Success(t *testing.T) {
	log := newTestLogger(t)
	mock := newAdminMockWithManager(t)
	reg := &ToolRegistry{service: mock, logger: log}

	// Create a super_admin assignment first
	_, err := reg.executeIAMAdminAssignmentCreate(context.Background(), map[string]interface{}{
		"user_id":    "admin-user",
		"admin_role": "super_admin",
		"granted_by": "system",
	})
	require.NoError(t, err)

	// Check permission
	result, err := reg.executeIAMAdminCheckPermission(context.Background(), map[string]interface{}{
		"user_id":    "admin-user",
		"permission": "user:create",
	})
	require.NoError(t, err)
	require.NotNil(t, result)

	resultMap, ok := result.(map[string]interface{})
	require.True(t, ok)
	assert.Equal(t, true, resultMap["allowed"])
}

func TestExecuteIAMAdminCheckPermission_Denied(t *testing.T) {
	log := newTestLogger(t)
	mock := newAdminMockWithManager(t)
	reg := &ToolRegistry{service: mock, logger: log}

	// Check permission for user with no assignments
	result, err := reg.executeIAMAdminCheckPermission(context.Background(), map[string]interface{}{
		"user_id":    "no-admin-user",
		"permission": "user:create",
	})
	require.NoError(t, err)
	require.NotNil(t, result)

	resultMap, ok := result.(map[string]interface{})
	require.True(t, ok)
	assert.Equal(t, false, resultMap["allowed"])
}

// =============================================================================
// Impersonation - Success Tests
// =============================================================================

func TestExecuteIAMImpersonationStart_Success(t *testing.T) {
	log := newTestLogger(t)
	mock := newAdminMockWithManager(t)
	reg := &ToolRegistry{service: mock, logger: log}

	// Create an admin assignment first (required for impersonation)
	_, err := reg.executeIAMAdminAssignmentCreate(context.Background(), map[string]interface{}{
		"user_id":    "admin-user",
		"admin_role": "super_admin",
		"granted_by": "system",
	})
	require.NoError(t, err)

	result, err := reg.executeIAMImpersonationStart(context.Background(), map[string]interface{}{
		"admin_user_id":    "admin-user",
		"target_user_id":   "user-1",
		"tenant_id":        "t1",
		"reason":           "support investigation",
		"duration_minutes": float64(30),
	})
	require.NoError(t, err)
	require.NotNil(t, result)

	resultMap, ok := result.(map[string]interface{})
	require.True(t, ok)
	assert.NotEmpty(t, resultMap["id"])
}

func TestExecuteIAMImpersonationStart_DefaultDuration(t *testing.T) {
	log := newTestLogger(t)
	mock := newAdminMockWithManager(t)
	reg := &ToolRegistry{service: mock, logger: log}

	// Create admin assignment
	_, err := reg.executeIAMAdminAssignmentCreate(context.Background(), map[string]interface{}{
		"user_id":    "admin-user",
		"admin_role": "super_admin",
		"granted_by": "system",
	})
	require.NoError(t, err)

	// Start without specifying duration (should default to 60 minutes)
	result, err := reg.executeIAMImpersonationStart(context.Background(), map[string]interface{}{
		"admin_user_id":  "admin-user",
		"target_user_id": "user-1",
		"tenant_id":      "t1",
		"reason":         "testing",
	})
	require.NoError(t, err)
	require.NotNil(t, result)

	resultMap := result.(map[string]interface{})
	// Check that expiration is approximately 60 minutes from now
	expiresStr, ok := resultMap["expires_at"].(string)
	require.True(t, ok)
	expiresAt, err := time.Parse(time.RFC3339Nano, expiresStr)
	require.NoError(t, err)
	assert.WithinDuration(t, time.Now().Add(60*time.Minute), expiresAt, 5*time.Second)
}

func TestExecuteIAMImpersonationEnd_Success(t *testing.T) {
	log := newTestLogger(t)
	mock := newAdminMockWithManager(t)
	reg := &ToolRegistry{service: mock, logger: log}

	// Create admin assignment and start impersonation
	_, err := reg.executeIAMAdminAssignmentCreate(context.Background(), map[string]interface{}{
		"user_id":    "admin-user",
		"admin_role": "super_admin",
		"granted_by": "system",
	})
	require.NoError(t, err)

	startResult, err := reg.executeIAMImpersonationStart(context.Background(), map[string]interface{}{
		"admin_user_id":  "admin-user",
		"target_user_id": "user-1",
		"tenant_id":      "t1",
		"reason":         "testing",
	})
	require.NoError(t, err)
	startMap := startResult.(map[string]interface{})
	sessionID := startMap["id"].(string)

	// End the impersonation
	result, err := reg.executeIAMImpersonationEnd(context.Background(), map[string]interface{}{
		"session_id": sessionID,
	})
	require.NoError(t, err)
	require.NotNil(t, result)

	resultMap, ok := result.(map[string]interface{})
	require.True(t, ok)
	assert.Equal(t, true, resultMap["ended"])
}

func TestExecuteIAMImpersonationEnd_NotFound(t *testing.T) {
	log := newTestLogger(t)
	mock := newAdminMockWithManager(t)
	reg := &ToolRegistry{service: mock, logger: log}

	_, err := reg.executeIAMImpersonationEnd(context.Background(), map[string]interface{}{
		"session_id": "nonexistent",
	})
	require.Error(t, err)
}

func TestExecuteIAMImpersonationList_Success(t *testing.T) {
	log := newTestLogger(t)
	mock := newAdminMockWithManager(t)
	reg := &ToolRegistry{service: mock, logger: log}

	result, err := reg.executeIAMImpersonationList(context.Background(), map[string]interface{}{
		"admin_user_id": "admin-user",
	})
	require.NoError(t, err)
	require.NotNil(t, result)

	resultMap, ok := result.(map[string]interface{})
	require.True(t, ok)
	assert.NotNil(t, resultMap["sessions"])
}

func TestExecuteIAMImpersonationList_Empty(t *testing.T) {
	log := newTestLogger(t)
	mock := newAdminMockWithManager(t)
	reg := &ToolRegistry{service: mock, logger: log}

	result, err := reg.executeIAMImpersonationList(context.Background(), map[string]interface{}{
		"admin_user_id": "no-sessions-user",
	})
	require.NoError(t, err)
	require.NotNil(t, result)
}

// =============================================================================
// Admin Assignment - With Expiration
// =============================================================================

func TestExecuteIAMAdminAssignmentCreate_WithExpiration(t *testing.T) {
	log := newTestLogger(t)
	mock := newAdminMockWithManager(t)
	reg := &ToolRegistry{service: mock, logger: log}

	expiresAt := time.Now().Add(24 * time.Hour).UTC().Format(time.RFC3339)
	result, err := reg.executeIAMAdminAssignmentCreate(context.Background(), map[string]interface{}{
		"user_id":    "user-1",
		"admin_role": "tenant_admin",
		"granted_by": "admin-user",
		"expires_at": expiresAt,
	})
	require.NoError(t, err)
	require.NotNil(t, result)

	resultMap := result.(map[string]interface{})
	assert.NotEmpty(t, resultMap["expires_at"])
}

func TestExecuteIAMAdminAssignmentCreate_WithMetadata(t *testing.T) {
	log := newTestLogger(t)
	mock := newAdminMockWithManager(t)
	reg := &ToolRegistry{service: mock, logger: log}

	result, err := reg.executeIAMAdminAssignmentCreate(context.Background(), map[string]interface{}{
		"user_id":    "user-1",
		"admin_role": "super_admin",
		"granted_by": "admin-user",
		"metadata": map[string]interface{}{
			"ticket": "SUPPORT-123",
		},
	})
	require.NoError(t, err)
	require.NotNil(t, result)

	resultMap := result.(map[string]interface{})
	metadata, ok := resultMap["metadata"].(map[string]interface{})
	require.True(t, ok)
	assert.Equal(t, "SUPPORT-123", metadata["ticket"])
}
