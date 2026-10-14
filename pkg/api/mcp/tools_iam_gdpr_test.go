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

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	dberrors "github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/pkg/iam"
)

// gdprMockQuicBaaS embeds mockService and implements gdprManagerAccessor.
type gdprMockQuicBaaS struct {
	mockService
	gdprMgr *iam.GDPRManager
}

// GetGDPRManager implements the gdprManagerAccessor interface.
func (m *gdprMockQuicBaaS) GetGDPRManager(_ context.Context) (*iam.GDPRManager, error) {
	return m.gdprMgr, nil
}

// mockUserStorage provides a minimal in-memory user storage for GDPR tests.
type mockUserStorage struct {
	users map[mockUserKey]*iam.User
}

// mockUserKey scopes a user id to its tenant.
type mockUserKey struct {
	tenantID string
	userID   uint64
}

func newMockUserStorage() *mockUserStorage {
	return &mockUserStorage{users: make(map[mockUserKey]*iam.User)}
}

func (m *mockUserStorage) CreateUser(_ context.Context, user *iam.User) error {
	m.users[mockUserKey{user.TenantID, user.ID}] = user
	return nil
}

func (m *mockUserStorage) GetUser(_ context.Context, tenantID string, userID uint64) (*iam.User, error) {
	u, ok := m.users[mockUserKey{tenantID, userID}]
	if !ok {
		return nil, iam.ErrUserNotFound
	}
	return u, nil
}

// MarkUserDeleting is unused by the GDPR tests; it satisfies iam.UserStorage.
func (m *mockUserStorage) MarkUserDeleting(_ context.Context, _ string, _ uint64) error {
	return iam.ErrUserNotFound
}

func (m *mockUserStorage) GetUserByEmail(_ context.Context, _, _ string) (*iam.User, error) {
	return nil, iam.ErrUserNotFound
}

// GetUserByUsername is unused by the GDPR tests; it satisfies iam.UserStorage.
func (m *mockUserStorage) GetUserByUsername(_ context.Context, _, _ string) (*iam.User, error) {
	return nil, iam.ErrUserNotFound
}

func (m *mockUserStorage) GetUserByPIVThumbprint(_ context.Context, _ string) (*iam.User, error) {
	return nil, iam.ErrUserNotFound
}

func (m *mockUserStorage) UpdateUser(_ context.Context, user *iam.User) error {
	m.users[mockUserKey{user.TenantID, user.ID}] = user
	return nil
}

func (m *mockUserStorage) DeleteUser(_ context.Context, tenantID string, userID uint64) error {
	delete(m.users, mockUserKey{tenantID, userID})
	return nil
}

func (m *mockUserStorage) ListUsers(_ context.Context, _ string, _ iam.ListOptions) ([]*iam.User, error) {
	result := make([]*iam.User, 0, len(m.users))
	for _, u := range m.users {
		result = append(result, u)
	}
	return result, nil
}

// newGDPRMockWithManager creates a mock service with a real GDPRManager
// backed by in-memory storage.
func newGDPRMockWithManager(t *testing.T) *gdprMockQuicBaaS {
	t.Helper()
	mgr, err := iam.NewGDPRManager(iam.GDPRManagerDependencies{
		ConsentStorage:    iam.NewMemoryConsentStorage(),
		DataExportStorage: iam.NewMemoryDataExportStorage(),
		DeletionStorage:   iam.NewMemoryDeletionStorage(),
		UserStorage:       newMockUserStorage(),
	})
	require.NoError(t, err)
	return &gdprMockQuicBaaS{gdprMgr: mgr}
}

// newGDPRMockWithoutManager creates a mock service without a GDPRManager
// to test service unavailable scenarios.
func newGDPRMockWithoutManager() *gdprMockQuicBaaS {
	return &gdprMockQuicBaaS{gdprMgr: nil}
}

// =============================================================================
// GDPR - Service Unavailable Tests
// =============================================================================

func TestExecuteIAMGDPRConsentGrant_ServiceUnavailable(t *testing.T) {
	log := newTestLogger(t)
	// Use a service that doesn't implement gdprManagerAccessor
	reg := &ToolRegistry{service: &mockService{}, logger: log}

	_, err := reg.executeIAMGDPRConsentGrant(context.Background(), map[string]interface{}{
		"tenant_id": "t1", "user_id": "30001", "purpose": "marketing", "version": "1.0",
	})
	require.Error(t, err)
	var dErr *dberrors.QBError
	assert.ErrorAs(t, err, &dErr)
}

func TestExecuteIAMGDPRConsentGrant_NilManager(t *testing.T) {
	log := newTestLogger(t)
	reg := &ToolRegistry{service: newGDPRMockWithoutManager(), logger: log}

	_, err := reg.executeIAMGDPRConsentGrant(context.Background(), map[string]interface{}{
		"tenant_id": "t1", "user_id": "30001", "purpose": "marketing", "version": "1.0",
	})
	require.Error(t, err)
}

func TestExecuteIAMGDPRConsentWithdraw_ServiceUnavailable(t *testing.T) {
	log := newTestLogger(t)
	reg := &ToolRegistry{service: &mockService{}, logger: log}

	_, err := reg.executeIAMGDPRConsentWithdraw(context.Background(), map[string]interface{}{
		"tenant_id": "t1", "user_id": "30001", "purpose": "marketing",
	})
	require.Error(t, err)
}

func TestExecuteIAMGDPRConsentGet_ServiceUnavailable(t *testing.T) {
	log := newTestLogger(t)
	reg := &ToolRegistry{service: &mockService{}, logger: log}

	_, err := reg.executeIAMGDPRConsentGet(context.Background(), map[string]interface{}{
		"tenant_id": "t1", "user_id": "30001", "purpose": "marketing",
	})
	require.Error(t, err)
}

func TestExecuteIAMGDPRConsentList_ServiceUnavailable(t *testing.T) {
	log := newTestLogger(t)
	reg := &ToolRegistry{service: &mockService{}, logger: log}

	_, err := reg.executeIAMGDPRConsentList(context.Background(), map[string]interface{}{
		"tenant_id": "t1", "user_id": "30001",
	})
	require.Error(t, err)
}

func TestExecuteIAMGDPRExportRequest_ServiceUnavailable(t *testing.T) {
	log := newTestLogger(t)
	reg := &ToolRegistry{service: &mockService{}, logger: log}

	_, err := reg.executeIAMGDPRExportRequest(context.Background(), map[string]interface{}{
		"tenant_id": "t1", "user_id": "30001", "requested_by": "admin",
	})
	require.Error(t, err)
}

func TestExecuteIAMGDPRExportStatus_ServiceUnavailable(t *testing.T) {
	log := newTestLogger(t)
	reg := &ToolRegistry{service: &mockService{}, logger: log}

	_, err := reg.executeIAMGDPRExportStatus(context.Background(), map[string]interface{}{
		"export_id": "export-1",
	})
	require.Error(t, err)
}

func TestExecuteIAMGDPRExportGenerate_ServiceUnavailable(t *testing.T) {
	log := newTestLogger(t)
	reg := &ToolRegistry{service: &mockService{}, logger: log}

	_, err := reg.executeIAMGDPRExportGenerate(context.Background(), map[string]interface{}{
		"export_id": "export-1",
	})
	require.Error(t, err)
}

func TestExecuteIAMGDPRDeletionRequest_ServiceUnavailable(t *testing.T) {
	log := newTestLogger(t)
	reg := &ToolRegistry{service: &mockService{}, logger: log}

	_, err := reg.executeIAMGDPRDeletionRequest(context.Background(), map[string]interface{}{
		"tenant_id": "t1", "user_id": "30001", "requested_by": "admin",
	})
	require.Error(t, err)
}

func TestExecuteIAMGDPRDeletionCancel_ServiceUnavailable(t *testing.T) {
	log := newTestLogger(t)
	reg := &ToolRegistry{service: &mockService{}, logger: log}

	_, err := reg.executeIAMGDPRDeletionCancel(context.Background(), map[string]interface{}{
		"deletion_id": "del-1", "cancelled_by": "admin",
	})
	require.Error(t, err)
}

func TestExecuteIAMGDPRDeletionProcess_ServiceUnavailable(t *testing.T) {
	log := newTestLogger(t)
	reg := &ToolRegistry{service: &mockService{}, logger: log}

	_, err := reg.executeIAMGDPRDeletionProcess(context.Background(), map[string]interface{}{
		"deletion_id": "del-1",
	})
	require.Error(t, err)
}

func TestExecuteIAMGDPRDeletionStatus_ServiceUnavailable(t *testing.T) {
	log := newTestLogger(t)
	reg := &ToolRegistry{service: &mockService{}, logger: log}

	_, err := reg.executeIAMGDPRDeletionStatus(context.Background(), map[string]interface{}{
		"deletion_id": "del-1",
	})
	require.Error(t, err)
}

// =============================================================================
// GDPR - Success Tests
// =============================================================================

func TestExecuteIAMGDPRConsentGrant_Success(t *testing.T) {
	log := newTestLogger(t)
	mock := newGDPRMockWithManager(t)
	reg := &ToolRegistry{service: mock, logger: log}

	result, err := reg.executeIAMGDPRConsentGrant(context.Background(), map[string]interface{}{
		"tenant_id": "t1",
		"user_id":   "30001",
		"purpose":   "marketing",
		"version":   "1.0",
	})
	require.NoError(t, err)
	require.NotNil(t, result)

	resultMap, ok := result.(map[string]interface{})
	require.True(t, ok, "result should be a map")
	assert.Equal(t, "marketing", resultMap["purpose"])
}

func TestExecuteIAMGDPRConsentGrant_InvalidExpiration(t *testing.T) {
	log := newTestLogger(t)
	mock := newGDPRMockWithManager(t)
	reg := &ToolRegistry{service: mock, logger: log}

	_, err := reg.executeIAMGDPRConsentGrant(context.Background(), map[string]interface{}{
		"tenant_id":  "t1",
		"user_id":    "30001",
		"purpose":    "marketing",
		"version":    "1.0",
		"expires_at": "not-a-date",
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "expires_at")
}

func TestExecuteIAMGDPRConsentGrant_InvalidPurpose(t *testing.T) {
	log := newTestLogger(t)
	mock := newGDPRMockWithManager(t)
	reg := &ToolRegistry{service: mock, logger: log}

	_, err := reg.executeIAMGDPRConsentGrant(context.Background(), map[string]interface{}{
		"tenant_id": "t1",
		"user_id":   "30001",
		"purpose":   "invalid_purpose",
		"version":   "1.0",
	})
	require.Error(t, err)
}

func TestExecuteIAMGDPRConsentWithdraw_Success(t *testing.T) {
	log := newTestLogger(t)
	mock := newGDPRMockWithManager(t)
	reg := &ToolRegistry{service: mock, logger: log}

	// Grant consent first
	_, err := reg.executeIAMGDPRConsentGrant(context.Background(), map[string]interface{}{
		"tenant_id": "t1", "user_id": "30001", "purpose": "marketing", "version": "1.0",
	})
	require.NoError(t, err)

	// Withdraw consent
	result, err := reg.executeIAMGDPRConsentWithdraw(context.Background(), map[string]interface{}{
		"tenant_id": "t1",
		"user_id":   "30001",
		"purpose":   "marketing",
	})
	require.NoError(t, err)
	require.NotNil(t, result)

	resultMap, ok := result.(map[string]interface{})
	require.True(t, ok)
	assert.Equal(t, true, resultMap["withdrawn"])
}

func TestExecuteIAMGDPRConsentGet_Success(t *testing.T) {
	log := newTestLogger(t)
	mock := newGDPRMockWithManager(t)
	reg := &ToolRegistry{service: mock, logger: log}

	// Grant consent first
	_, err := reg.executeIAMGDPRConsentGrant(context.Background(), map[string]interface{}{
		"tenant_id": "t1", "user_id": "30001", "purpose": "analytics", "version": "2.0",
	})
	require.NoError(t, err)

	// Get consent
	result, err := reg.executeIAMGDPRConsentGet(context.Background(), map[string]interface{}{
		"tenant_id": "t1",
		"user_id":   "30001",
		"purpose":   "analytics",
	})
	require.NoError(t, err)
	require.NotNil(t, result)

	resultMap, ok := result.(map[string]interface{})
	require.True(t, ok)
	assert.Equal(t, "analytics", resultMap["purpose"])
}

func TestExecuteIAMGDPRConsentGet_NotFound(t *testing.T) {
	log := newTestLogger(t)
	mock := newGDPRMockWithManager(t)
	reg := &ToolRegistry{service: mock, logger: log}

	_, err := reg.executeIAMGDPRConsentGet(context.Background(), map[string]interface{}{
		"tenant_id": "t1", "user_id": "30001", "purpose": "marketing",
	})
	require.Error(t, err)
}

func TestExecuteIAMGDPRConsentList_Success(t *testing.T) {
	log := newTestLogger(t)
	mock := newGDPRMockWithManager(t)
	reg := &ToolRegistry{service: mock, logger: log}

	// Grant some consents
	_, err := reg.executeIAMGDPRConsentGrant(context.Background(), map[string]interface{}{
		"tenant_id": "t1", "user_id": "30001", "purpose": "marketing", "version": "1.0",
	})
	require.NoError(t, err)

	result, err := reg.executeIAMGDPRConsentList(context.Background(), map[string]interface{}{
		"tenant_id": "t1",
		"user_id":   "30001",
	})
	require.NoError(t, err)
	require.NotNil(t, result)

	resultMap, ok := result.(map[string]interface{})
	require.True(t, ok)
	assert.NotNil(t, resultMap["consents"])
}

func TestExecuteIAMGDPRExportRequest_Success(t *testing.T) {
	log := newTestLogger(t)
	mock := newGDPRMockWithManager(t)
	reg := &ToolRegistry{service: mock, logger: log}

	result, err := reg.executeIAMGDPRExportRequest(context.Background(), map[string]interface{}{
		"tenant_id":    "t1",
		"user_id":      "30001",
		"requested_by": "admin",
		"format":       "json",
	})
	require.NoError(t, err)
	require.NotNil(t, result)

	resultMap, ok := result.(map[string]interface{})
	require.True(t, ok)
	assert.NotEmpty(t, resultMap["id"])
}

func TestExecuteIAMGDPRDeletionRequest_Success(t *testing.T) {
	log := newTestLogger(t)
	mock := newGDPRMockWithManager(t)
	reg := &ToolRegistry{service: mock, logger: log}

	result, err := reg.executeIAMGDPRDeletionRequest(context.Background(), map[string]interface{}{
		"tenant_id":    "t1",
		"user_id":      "30001",
		"requested_by": "admin",
		"reason":       "user requested deletion",
	})
	require.NoError(t, err)
	require.NotNil(t, result)

	resultMap, ok := result.(map[string]interface{})
	require.True(t, ok)
	assert.NotEmpty(t, resultMap["id"])
}
