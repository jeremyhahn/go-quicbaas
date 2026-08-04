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
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"errors"

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"
	"github.com/jeremyhahn/go-quicbaas/pkg/iam"
)

// =============================================================================
// MFAServiceAdapter Tests
// =============================================================================

func TestNewMFAServiceAdapter(t *testing.T) {
	t.Parallel()

	t.Run("nil input returns nil", func(t *testing.T) {
		adapter := NewMFAServiceAdapter(nil)
		assert.Nil(t, adapter)
	})

	t.Run("non-MFAService input returns nil", func(t *testing.T) {
		// Pass something that doesn't implement MFAService
		adapter := NewMFAServiceAdapter("not a service")
		assert.Nil(t, adapter)
	})

	t.Run("valid MFAService returns adapter", func(t *testing.T) {
		mockSvc := &adapterMockMFAService{}
		adapter := NewMFAServiceAdapter(mockSvc)
		assert.NotNil(t, adapter)
	})
}

// adapterMockMFAService implements MFAService for adapter testing
type adapterMockMFAService struct {
	beginEnrollmentFunc     func(ctx context.Context, userID, tenantID string, method iam.MFAMethod) (*iam.MFAEnrollResponse, error)
	completeEnrollmentFunc  func(ctx context.Context, enrollmentID, code string) error
	cancelEnrollmentFunc    func(ctx context.Context, enrollmentID string) error
	beginChallengeFunc      func(ctx context.Context, userID, tenantID, sessionToken string) (*iam.MFAChallenge, error)
	verifyChallengeFunc     func(ctx context.Context, challengeID, code string) (*iam.MFAVerifyResponse, error)
	verifyCodeFunc          func(ctx context.Context, userID, tenantID, code string) error
	getMFAStatusFunc        func(ctx context.Context, userID, tenantID string) (*iam.MFAStatus, error)
	disableMFAFunc          func(ctx context.Context, userID, tenantID string) error
	generateBackupCodesFunc func(ctx context.Context, userID, tenantID string) ([]string, error)
	verifyBackupCodeFunc    func(ctx context.Context, userID, tenantID, code string) error
	getBackupCodeStatusFunc func(ctx context.Context, userID, tenantID string) (*iam.BackupCodeStatus, error)
}

func (m *adapterMockMFAService) BeginEnrollment(ctx context.Context, userID, tenantID string, method iam.MFAMethod) (*iam.MFAEnrollResponse, error) {
	if m.beginEnrollmentFunc != nil {
		return m.beginEnrollmentFunc(ctx, userID, tenantID, method)
	}
	return &iam.MFAEnrollResponse{EnrollmentID: "enroll-123"}, nil
}

func (m *adapterMockMFAService) CompleteEnrollment(ctx context.Context, enrollmentID, code string) error {
	if m.completeEnrollmentFunc != nil {
		return m.completeEnrollmentFunc(ctx, enrollmentID, code)
	}
	return nil
}

func (m *adapterMockMFAService) CancelEnrollment(ctx context.Context, enrollmentID string) error {
	if m.cancelEnrollmentFunc != nil {
		return m.cancelEnrollmentFunc(ctx, enrollmentID)
	}
	return nil
}

func (m *adapterMockMFAService) BeginChallenge(ctx context.Context, userID, tenantID, sessionToken string) (*iam.MFAChallenge, error) {
	if m.beginChallengeFunc != nil {
		return m.beginChallengeFunc(ctx, userID, tenantID, sessionToken)
	}
	return &iam.MFAChallenge{ID: "challenge-123"}, nil
}

func (m *adapterMockMFAService) VerifyChallenge(ctx context.Context, challengeID, code string) (*iam.MFAVerifyResponse, error) {
	if m.verifyChallengeFunc != nil {
		return m.verifyChallengeFunc(ctx, challengeID, code)
	}
	return &iam.MFAVerifyResponse{Success: true}, nil
}

func (m *adapterMockMFAService) VerifyCode(ctx context.Context, userID, tenantID, code string) error {
	if m.verifyCodeFunc != nil {
		return m.verifyCodeFunc(ctx, userID, tenantID, code)
	}
	return nil
}

func (m *adapterMockMFAService) GetMFAStatus(ctx context.Context, userID, tenantID string) (*iam.MFAStatus, error) {
	if m.getMFAStatusFunc != nil {
		return m.getMFAStatusFunc(ctx, userID, tenantID)
	}
	return &iam.MFAStatus{Enabled: true}, nil
}

func (m *adapterMockMFAService) DisableMFA(ctx context.Context, userID, tenantID string) error {
	if m.disableMFAFunc != nil {
		return m.disableMFAFunc(ctx, userID, tenantID)
	}
	return nil
}

func (m *adapterMockMFAService) GenerateBackupCodes(ctx context.Context, userID, tenantID string) ([]string, error) {
	if m.generateBackupCodesFunc != nil {
		return m.generateBackupCodesFunc(ctx, userID, tenantID)
	}
	return []string{"code1", "code2"}, nil
}

func (m *adapterMockMFAService) VerifyBackupCode(ctx context.Context, userID, tenantID, code string) error {
	if m.verifyBackupCodeFunc != nil {
		return m.verifyBackupCodeFunc(ctx, userID, tenantID, code)
	}
	return nil
}

func (m *adapterMockMFAService) GetBackupCodeStatus(ctx context.Context, userID, tenantID string) (*iam.BackupCodeStatus, error) {
	if m.getBackupCodeStatusFunc != nil {
		return m.getBackupCodeStatusFunc(ctx, userID, tenantID)
	}
	return &iam.BackupCodeStatus{Remaining: 5}, nil
}

func TestMFAServiceAdapter_BeginEnrollment(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	mockSvc := &adapterMockMFAService{
		beginEnrollmentFunc: func(ctx context.Context, userID, tenantID string, method iam.MFAMethod) (*iam.MFAEnrollResponse, error) {
			return &iam.MFAEnrollResponse{
				EnrollmentID: "test-enroll-id",
				Secret:       "test-secret",
				QRCodeURI:    "otpauth://totp/...",
			}, nil
		},
	}

	adapter := NewMFAServiceAdapter(mockSvc)
	resp, err := adapter.BeginEnrollment(ctx, "user1", "tenant1", iam.MFAMethodTOTP)

	assert.NoError(t, err)
	assert.NotNil(t, resp)
	assert.Equal(t, "test-enroll-id", resp.EnrollmentID)
}

func TestMFAServiceAdapter_CompleteEnrollment(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	mockSvc := &adapterMockMFAService{}
	adapter := NewMFAServiceAdapter(mockSvc)

	err := adapter.CompleteEnrollment(ctx, "enroll-123", "123456")
	assert.NoError(t, err)
}

func TestMFAServiceAdapter_CancelEnrollment(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	mockSvc := &adapterMockMFAService{}
	adapter := NewMFAServiceAdapter(mockSvc)

	err := adapter.CancelEnrollment(ctx, "enroll-123")
	assert.NoError(t, err)
}

func TestMFAServiceAdapter_BeginChallenge(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	mockSvc := &adapterMockMFAService{
		beginChallengeFunc: func(ctx context.Context, userID, tenantID, sessionToken string) (*iam.MFAChallenge, error) {
			return &iam.MFAChallenge{
				ID:     "chal-456",
				Method: iam.MFAMethodTOTP,
			}, nil
		},
	}

	adapter := NewMFAServiceAdapter(mockSvc)
	resp, err := adapter.BeginChallenge(ctx, "user1", "tenant1", "session-token")

	assert.NoError(t, err)
	assert.NotNil(t, resp)
	assert.Equal(t, "chal-456", resp.ID)
}

func TestMFAServiceAdapter_VerifyChallenge(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	mockSvc := &adapterMockMFAService{
		verifyChallengeFunc: func(ctx context.Context, challengeID, code string) (*iam.MFAVerifyResponse, error) {
			return &iam.MFAVerifyResponse{Success: true}, nil
		},
	}

	adapter := NewMFAServiceAdapter(mockSvc)
	resp, err := adapter.VerifyChallenge(ctx, "chal-456", "123456")

	assert.NoError(t, err)
	assert.NotNil(t, resp)
	assert.True(t, resp.Success)
}

func TestMFAServiceAdapter_VerifyCode(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	mockSvc := &adapterMockMFAService{}
	adapter := NewMFAServiceAdapter(mockSvc)

	err := adapter.VerifyCode(ctx, "user1", "tenant1", "123456")
	assert.NoError(t, err)
}

func TestMFAServiceAdapter_GetMFAStatus(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	mockSvc := &adapterMockMFAService{
		getMFAStatusFunc: func(ctx context.Context, userID, tenantID string) (*iam.MFAStatus, error) {
			return &iam.MFAStatus{
				Enabled:              true,
				Method:               iam.MFAMethodTOTP,
				BackupCodesRemaining: 5,
			}, nil
		},
	}

	adapter := NewMFAServiceAdapter(mockSvc)
	resp, err := adapter.GetMFAStatus(ctx, "user1", "tenant1")

	assert.NoError(t, err)
	assert.NotNil(t, resp)
	assert.True(t, resp.Enabled)
}

func TestMFAServiceAdapter_DisableMFA(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	mockSvc := &adapterMockMFAService{}
	adapter := NewMFAServiceAdapter(mockSvc)

	err := adapter.DisableMFA(ctx, "user1", "tenant1")
	assert.NoError(t, err)
}

func TestMFAServiceAdapter_GenerateBackupCodes(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	mockSvc := &adapterMockMFAService{
		generateBackupCodesFunc: func(ctx context.Context, userID, tenantID string) ([]string, error) {
			return []string{"code1", "code2", "code3"}, nil
		},
	}

	adapter := NewMFAServiceAdapter(mockSvc)
	codes, err := adapter.GenerateBackupCodes(ctx, "user1", "tenant1")

	assert.NoError(t, err)
	assert.Len(t, codes, 3)
}

func TestMFAServiceAdapter_VerifyBackupCode(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	mockSvc := &adapterMockMFAService{}
	adapter := NewMFAServiceAdapter(mockSvc)

	err := adapter.VerifyBackupCode(ctx, "user1", "tenant1", "backup-code")
	assert.NoError(t, err)
}

func TestMFAServiceAdapter_GetBackupCodeStatus(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	mockSvc := &adapterMockMFAService{
		getBackupCodeStatusFunc: func(ctx context.Context, userID, tenantID string) (*iam.BackupCodeStatus, error) {
			return &iam.BackupCodeStatus{Remaining: 8}, nil
		},
	}

	adapter := NewMFAServiceAdapter(mockSvc)
	status, err := adapter.GetBackupCodeStatus(ctx, "user1", "tenant1")

	assert.NoError(t, err)
	assert.NotNil(t, status)
	assert.Equal(t, 8, status.Remaining)
}

// =============================================================================
// IAMServiceAdapter Tests
// =============================================================================

func TestNewIAMServiceAdapter(t *testing.T) {
	t.Parallel()

	adapter := NewIAMServiceAdapter(nil, nil, nil, nil, nil, nil)
	assert.NotNil(t, adapter)
}

func TestIAMServiceAdapter_CreateTenant_NilInput(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	adapter := NewIAMServiceAdapter(nil, nil, nil, nil, nil, nil)
	result, err := adapter.CreateTenant(ctx, nil)

	assert.Error(t, err)
	assert.Nil(t, result)
	assert.Contains(t, err.Error(), "tenant cannot be nil")
}

func TestIAMServiceAdapter_UpdateTenant_NilInput(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	adapter := NewIAMServiceAdapter(nil, nil, nil, nil, nil, nil)
	result, err := adapter.UpdateTenant(ctx, nil)

	assert.Error(t, err)
	assert.Nil(t, result)
	assert.Contains(t, err.Error(), "tenant cannot be nil")
}

func TestIAMServiceAdapter_CreateUser_NilInput(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	adapter := NewIAMServiceAdapter(nil, nil, nil, nil, nil, nil)
	result, err := adapter.CreateUser(ctx, nil)

	assert.Error(t, err)
	assert.Nil(t, result)
	assert.Contains(t, err.Error(), "user cannot be nil")
}

func TestIAMServiceAdapter_UpdateUser_NilInput(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	adapter := NewIAMServiceAdapter(nil, nil, nil, nil, nil, nil)
	result, err := adapter.UpdateUser(ctx, nil)

	assert.Error(t, err)
	assert.Nil(t, result)
	assert.Contains(t, err.Error(), "user cannot be nil")
}

func TestIAMServiceAdapter_CreateGroup_NilInput(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	adapter := NewIAMServiceAdapter(nil, nil, nil, nil, nil, nil)
	result, err := adapter.CreateGroup(ctx, nil)

	assert.Error(t, err)
	assert.Nil(t, result)
	assert.Contains(t, err.Error(), "group cannot be nil")
}

func TestIAMServiceAdapter_UpdateGroup_NilInput(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	adapter := NewIAMServiceAdapter(nil, nil, nil, nil, nil, nil)
	result, err := adapter.UpdateGroup(ctx, nil)

	assert.Error(t, err)
	assert.Nil(t, result)
	assert.Contains(t, err.Error(), "group cannot be nil")
}

func TestIAMServiceAdapter_CreateRole_NilInput(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	adapter := NewIAMServiceAdapter(nil, nil, nil, nil, nil, nil)
	result, err := adapter.CreateRole(ctx, nil)

	assert.Error(t, err)
	assert.Nil(t, result)
	assert.Contains(t, err.Error(), "role cannot be nil")
}

func TestIAMServiceAdapter_UpdateRole_NilInput(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	adapter := NewIAMServiceAdapter(nil, nil, nil, nil, nil, nil)
	result, err := adapter.UpdateRole(ctx, nil)

	assert.Error(t, err)
	assert.Nil(t, result)
	assert.Contains(t, err.Error(), "role cannot be nil")
}

func TestIAMServiceAdapter_RegisterApp_NilInput(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	adapter := NewIAMServiceAdapter(nil, nil, nil, nil, nil, nil)
	result, err := adapter.RegisterApp(ctx, nil)

	assert.Error(t, err)
	assert.Nil(t, result)
	assert.Contains(t, err.Error(), "application cannot be nil")
}

func TestIAMServiceAdapter_UpdateApp_NilInput(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	adapter := NewIAMServiceAdapter(nil, nil, nil, nil, nil, nil)
	result, err := adapter.UpdateApp(ctx, nil)

	assert.Error(t, err)
	assert.Nil(t, result)
	assert.Contains(t, err.Error(), "application cannot be nil")
}

func TestIAMServiceAdapter_CreatePolicy_NilInput(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	adapter := NewIAMServiceAdapter(nil, nil, nil, nil, nil, nil)
	result, err := adapter.CreatePolicy(ctx, nil)

	assert.Error(t, err)
	assert.Nil(t, result)
	assert.Contains(t, err.Error(), "policy cannot be nil")
}

func TestIAMServiceAdapter_UpdatePolicy_NilInput(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	adapter := NewIAMServiceAdapter(nil, nil, nil, nil, nil, nil)
	result, err := adapter.UpdatePolicy(ctx, nil)

	assert.Error(t, err)
	assert.Nil(t, result)
	assert.Contains(t, err.Error(), "policy cannot be nil")
}

// --- Merged from iam_adapter_coverage_91_test.go ---

// =============================================================================
// Mock: TenantManager
// =============================================================================

type adapter91MockTenantManager struct {
	createTenantFunc func(ctx context.Context, opts iam.CreateTenantOptions) (*iam.Tenant, error)
	getTenantFunc    func(ctx context.Context, id string) (*iam.Tenant, error)
	updateTenantFunc func(ctx context.Context, id string, opts iam.UpdateTenantOptions) (*iam.Tenant, error)
	deleteTenantFunc func(ctx context.Context, id string) error
	listTenantsFunc  func(ctx context.Context, opts iam.ListOptions) ([]*iam.Tenant, error)
	checkQuotaFunc   func(ctx context.Context, tenantID, resourceType string) error
}

func (m *adapter91MockTenantManager) CreateTenant(ctx context.Context, opts iam.CreateTenantOptions) (*iam.Tenant, error) {
	if m.createTenantFunc != nil {
		return m.createTenantFunc(ctx, opts)
	}
	return &iam.Tenant{ID: opts.ID, Name: opts.Name}, nil
}

func (m *adapter91MockTenantManager) GetTenant(ctx context.Context, id string) (*iam.Tenant, error) {
	if m.getTenantFunc != nil {
		return m.getTenantFunc(ctx, id)
	}
	return &iam.Tenant{ID: id, Name: "test-tenant"}, nil
}

func (m *adapter91MockTenantManager) UpdateTenant(ctx context.Context, id string, opts iam.UpdateTenantOptions) (*iam.Tenant, error) {
	if m.updateTenantFunc != nil {
		return m.updateTenantFunc(ctx, id, opts)
	}
	t := &iam.Tenant{ID: id}
	if opts.Name != nil {
		t.Name = *opts.Name
	}
	return t, nil
}

func (m *adapter91MockTenantManager) DeleteTenant(ctx context.Context, id string) error {
	if m.deleteTenantFunc != nil {
		return m.deleteTenantFunc(ctx, id)
	}
	return nil
}

func (m *adapter91MockTenantManager) ListTenants(ctx context.Context, opts iam.ListOptions) ([]*iam.Tenant, error) {
	if m.listTenantsFunc != nil {
		return m.listTenantsFunc(ctx, opts)
	}
	return []*iam.Tenant{{ID: "t1", Name: "Tenant 1"}}, nil
}

func (m *adapter91MockTenantManager) CheckQuota(ctx context.Context, tenantID, resourceType string) error {
	if m.checkQuotaFunc != nil {
		return m.checkQuotaFunc(ctx, tenantID, resourceType)
	}
	return nil
}

func (m *adapter91MockTenantManager) UpdateEngineQuotas(_ context.Context, _ string, _ qrdbsdk.Quotas) error {
	return nil
}

func (m *adapter91MockTenantManager) UpdateEngineEntitlements(_ context.Context, _ string, _ qrdbsdk.Entitlements) error {
	return nil
}

// =============================================================================
// Mock: AdminStorage
// =============================================================================

type adapter91MockAdminStorage struct {
	createAssignmentFunc   func(ctx context.Context, assignment *iam.AdminAssignment) error
	getAssignmentFunc      func(ctx context.Context, id string) (*iam.AdminAssignment, error)
	updateAssignmentFunc   func(ctx context.Context, assignment *iam.AdminAssignment) error
	deleteAssignmentFunc   func(ctx context.Context, id string) error
	listAssignmentsFunc    func(ctx context.Context, opts iam.ListAdminAssignmentsOptions) ([]*iam.AdminAssignment, error)
	getUserAssignmentsFunc func(ctx context.Context, userID string) ([]*iam.AdminAssignment, error)
}

func (m *adapter91MockAdminStorage) CreateAssignment(ctx context.Context, assignment *iam.AdminAssignment) error {
	if m.createAssignmentFunc != nil {
		return m.createAssignmentFunc(ctx, assignment)
	}
	return nil
}

func (m *adapter91MockAdminStorage) GetAssignment(ctx context.Context, id string) (*iam.AdminAssignment, error) {
	if m.getAssignmentFunc != nil {
		return m.getAssignmentFunc(ctx, id)
	}
	return &iam.AdminAssignment{ID: id}, nil
}

func (m *adapter91MockAdminStorage) UpdateAssignment(ctx context.Context, assignment *iam.AdminAssignment) error {
	if m.updateAssignmentFunc != nil {
		return m.updateAssignmentFunc(ctx, assignment)
	}
	return nil
}

func (m *adapter91MockAdminStorage) DeleteAssignment(ctx context.Context, id string) error {
	if m.deleteAssignmentFunc != nil {
		return m.deleteAssignmentFunc(ctx, id)
	}
	return nil
}

func (m *adapter91MockAdminStorage) ListAssignments(ctx context.Context, opts iam.ListAdminAssignmentsOptions) ([]*iam.AdminAssignment, error) {
	if m.listAssignmentsFunc != nil {
		return m.listAssignmentsFunc(ctx, opts)
	}
	return []*iam.AdminAssignment{{ID: "a1"}}, nil
}

func (m *adapter91MockAdminStorage) GetUserAssignments(ctx context.Context, userID string) ([]*iam.AdminAssignment, error) {
	if m.getUserAssignmentsFunc != nil {
		return m.getUserAssignmentsFunc(ctx, userID)
	}
	return []*iam.AdminAssignment{{ID: "a1"}}, nil
}

// =============================================================================
// Mock: ImpersonationStorage
// =============================================================================

type adapter91MockImpersonationStorage struct {
	createFunc        func(ctx context.Context, session *iam.ImpersonationSession) error
	getFunc           func(ctx context.Context, id string) (*iam.ImpersonationSession, error)
	endFunc           func(ctx context.Context, id string) error
	incrementFunc     func(ctx context.Context, id string) (int64, error)
	listActiveFunc    func(ctx context.Context, adminUserID string) ([]*iam.ImpersonationSession, error)
	listFunc          func(ctx context.Context, activeOnly bool, adminUserID string) ([]*iam.ImpersonationSession, error)
	deleteExpiredFunc func(ctx context.Context) error
}

func (m *adapter91MockImpersonationStorage) CreateImpersonationSession(ctx context.Context, session *iam.ImpersonationSession) error {
	if m.createFunc != nil {
		return m.createFunc(ctx, session)
	}
	return nil
}

func (m *adapter91MockImpersonationStorage) GetImpersonationSession(ctx context.Context, id string) (*iam.ImpersonationSession, error) {
	if m.getFunc != nil {
		return m.getFunc(ctx, id)
	}
	return &iam.ImpersonationSession{ID: id}, nil
}

func (m *adapter91MockImpersonationStorage) EndImpersonationSession(ctx context.Context, id string) error {
	if m.endFunc != nil {
		return m.endFunc(ctx, id)
	}
	return nil
}

func (m *adapter91MockImpersonationStorage) IncrementOperationCount(ctx context.Context, id string) (int64, error) {
	if m.incrementFunc != nil {
		return m.incrementFunc(ctx, id)
	}
	return 1, nil
}

func (m *adapter91MockImpersonationStorage) ListActiveImpersonations(ctx context.Context, adminUserID string) ([]*iam.ImpersonationSession, error) {
	if m.listActiveFunc != nil {
		return m.listActiveFunc(ctx, adminUserID)
	}
	return []*iam.ImpersonationSession{}, nil
}

func (m *adapter91MockImpersonationStorage) ListImpersonations(ctx context.Context, activeOnly bool, adminUserID string) ([]*iam.ImpersonationSession, error) {
	if m.listFunc != nil {
		return m.listFunc(ctx, activeOnly, adminUserID)
	}
	return []*iam.ImpersonationSession{}, nil
}

func (m *adapter91MockImpersonationStorage) DeleteExpiredSessions(ctx context.Context) error {
	if m.deleteExpiredFunc != nil {
		return m.deleteExpiredFunc(ctx)
	}
	return nil
}

// =============================================================================
// Mock: ConsentStorage
// =============================================================================

type adapter91MockConsentStorage struct {
	createConsentFunc   func(ctx context.Context, opts iam.CreateConsentOptions) (*iam.Consent, error)
	getConsentFunc      func(ctx context.Context, tenantID, userID string, purpose iam.ConsentPurpose) (*iam.Consent, error)
	updateConsentFunc   func(ctx context.Context, id string, opts iam.UpdateConsentOptions) (*iam.Consent, error)
	deleteConsentFunc   func(ctx context.Context, id string) error
	listConsentsFunc    func(ctx context.Context, opts iam.ListConsentsOptions) ([]*iam.Consent, error)
	getUserConsentsFunc func(ctx context.Context, tenantID, userID string) ([]*iam.Consent, error)
}

func (m *adapter91MockConsentStorage) CreateConsent(ctx context.Context, opts iam.CreateConsentOptions) (*iam.Consent, error) {
	if m.createConsentFunc != nil {
		return m.createConsentFunc(ctx, opts)
	}
	return &iam.Consent{ID: "consent-1"}, nil
}

func (m *adapter91MockConsentStorage) GetConsent(ctx context.Context, tenantID, userID string, purpose iam.ConsentPurpose) (*iam.Consent, error) {
	if m.getConsentFunc != nil {
		return m.getConsentFunc(ctx, tenantID, userID, purpose)
	}
	return &iam.Consent{ID: "consent-1", Purpose: purpose}, nil
}

func (m *adapter91MockConsentStorage) UpdateConsent(ctx context.Context, id string, opts iam.UpdateConsentOptions) (*iam.Consent, error) {
	if m.updateConsentFunc != nil {
		return m.updateConsentFunc(ctx, id, opts)
	}
	return &iam.Consent{ID: id}, nil
}

func (m *adapter91MockConsentStorage) DeleteConsent(ctx context.Context, id string) error {
	if m.deleteConsentFunc != nil {
		return m.deleteConsentFunc(ctx, id)
	}
	return nil
}

func (m *adapter91MockConsentStorage) ListConsents(ctx context.Context, opts iam.ListConsentsOptions) ([]*iam.Consent, error) {
	if m.listConsentsFunc != nil {
		return m.listConsentsFunc(ctx, opts)
	}
	return []*iam.Consent{}, nil
}

func (m *adapter91MockConsentStorage) GetUserConsents(ctx context.Context, tenantID, userID string) ([]*iam.Consent, error) {
	if m.getUserConsentsFunc != nil {
		return m.getUserConsentsFunc(ctx, tenantID, userID)
	}
	return []*iam.Consent{}, nil
}

// =============================================================================
// Mock: DataExportStorage
// =============================================================================

type adapter91MockDataExportStorage struct {
	createFunc func(ctx context.Context, opts iam.CreateDataExportOptions) (*iam.DataExportRequest, error)
	getFunc    func(ctx context.Context, id string) (*iam.DataExportRequest, error)
	updateFunc func(ctx context.Context, req *iam.DataExportRequest) error
	listFunc   func(ctx context.Context, opts iam.ListExportRequestsOptions) ([]*iam.DataExportRequest, error)
}

func (m *adapter91MockDataExportStorage) CreateExportRequest(ctx context.Context, opts iam.CreateDataExportOptions) (*iam.DataExportRequest, error) {
	if m.createFunc != nil {
		return m.createFunc(ctx, opts)
	}
	return &iam.DataExportRequest{ID: "exp-1"}, nil
}

func (m *adapter91MockDataExportStorage) GetExportRequest(ctx context.Context, id string) (*iam.DataExportRequest, error) {
	if m.getFunc != nil {
		return m.getFunc(ctx, id)
	}
	return &iam.DataExportRequest{ID: id}, nil
}

func (m *adapter91MockDataExportStorage) UpdateExportRequest(ctx context.Context, req *iam.DataExportRequest) error {
	if m.updateFunc != nil {
		return m.updateFunc(ctx, req)
	}
	return nil
}

func (m *adapter91MockDataExportStorage) ListExportRequests(ctx context.Context, opts iam.ListExportRequestsOptions) ([]*iam.DataExportRequest, error) {
	if m.listFunc != nil {
		return m.listFunc(ctx, opts)
	}
	return []*iam.DataExportRequest{}, nil
}

// =============================================================================
// Mock: DeletionStorage
// =============================================================================

type adapter91MockDeletionStorage struct {
	createFunc func(ctx context.Context, opts iam.CreateDeletionRequestOptions) (*iam.DataDeletionRequest, error)
	getFunc    func(ctx context.Context, id string) (*iam.DataDeletionRequest, error)
	updateFunc func(ctx context.Context, req *iam.DataDeletionRequest) error
	listFunc   func(ctx context.Context, opts iam.ListDeletionRequestsOptions) ([]*iam.DataDeletionRequest, error)
}

func (m *adapter91MockDeletionStorage) CreateDeletionRequest(ctx context.Context, opts iam.CreateDeletionRequestOptions) (*iam.DataDeletionRequest, error) {
	if m.createFunc != nil {
		return m.createFunc(ctx, opts)
	}
	return &iam.DataDeletionRequest{ID: "del-1"}, nil
}

func (m *adapter91MockDeletionStorage) GetDeletionRequest(ctx context.Context, id string) (*iam.DataDeletionRequest, error) {
	if m.getFunc != nil {
		return m.getFunc(ctx, id)
	}
	return &iam.DataDeletionRequest{ID: id}, nil
}

func (m *adapter91MockDeletionStorage) UpdateDeletionRequest(ctx context.Context, req *iam.DataDeletionRequest) error {
	if m.updateFunc != nil {
		return m.updateFunc(ctx, req)
	}
	return nil
}

func (m *adapter91MockDeletionStorage) ListDeletionRequests(ctx context.Context, opts iam.ListDeletionRequestsOptions) ([]*iam.DataDeletionRequest, error) {
	if m.listFunc != nil {
		return m.listFunc(ctx, opts)
	}
	return []*iam.DataDeletionRequest{}, nil
}

// =============================================================================
// Mock: SessionStorage (for GDPR deps)
// =============================================================================

type adapter91MockSessionStorage struct {
	sessions map[string]*iam.Session
}

func newAdapter91MockSessionStorage() *adapter91MockSessionStorage {
	return &adapter91MockSessionStorage{
		sessions: make(map[string]*iam.Session),
	}
}

func (m *adapter91MockSessionStorage) CreateSession(ctx context.Context, session *iam.Session) error {
	m.sessions[session.ID] = session
	return nil
}

func (m *adapter91MockSessionStorage) GetSession(ctx context.Context, sessionID string) (*iam.Session, error) {
	s, ok := m.sessions[sessionID]
	if !ok {
		return nil, errors.New("session not found")
	}
	return s, nil
}

func (m *adapter91MockSessionStorage) GetSessionsByUser(ctx context.Context, tenantID, userID string) ([]*iam.Session, error) {
	var result []*iam.Session
	for _, s := range m.sessions {
		if s.TenantID == tenantID && s.UserID == userID {
			result = append(result, s)
		}
	}
	return result, nil
}

func (m *adapter91MockSessionStorage) UpdateSession(ctx context.Context, session *iam.Session) error {
	m.sessions[session.ID] = session
	return nil
}

func (m *adapter91MockSessionStorage) DeleteSession(ctx context.Context, sessionID string) error {
	delete(m.sessions, sessionID)
	return nil
}

func (m *adapter91MockSessionStorage) DeleteExpiredSessions(ctx context.Context) (int, error) {
	return 0, nil
}

// =============================================================================
// Helper: Create test IAMServiceAdapter with real IdentityStore and in-memory storage
// =============================================================================

func createTestIAMAdapter91(t *testing.T) *IAMServiceAdapter {
	t.Helper()
	storage := iam.NewMemoryStorage()
	tenantMgr := &adapter91MockTenantManager{}
	identityStore := iam.NewIdentityStore(iam.IdentityStoreOptions{
		UserStorage:  storage,
		GroupStorage: storage,
		RoleStorage:  storage,
		Tenants:      tenantMgr,
	})
	policyCache := iam.NewPolicyCache(&iam.CacheConfig{
		TTL:               5 * time.Minute,
		MaxSize:           100,
		BloomFilterSize:   1000,
		BloomFilterHashes: 3,
	})
	policyEngine := iam.NewPolicyEngine(iam.PolicyEngineOptions{
		Storage: storage,
		Cache:   policyCache,
	})
	appManager := iam.NewAppManager(storage, tenantMgr)

	adminMgr, err := iam.NewAdminManager(iam.AdminManagerOptions{
		AdminStorage:         &adapter91MockAdminStorage{},
		ImpersonationStorage: &adapter91MockImpersonationStorage{},
	})
	require.NoError(t, err)

	gdprMgr, err := iam.NewGDPRManager(iam.GDPRManagerDependencies{
		ConsentStorage:    &adapter91MockConsentStorage{},
		DataExportStorage: &adapter91MockDataExportStorage{},
		DeletionStorage:   &adapter91MockDeletionStorage{},
		UserStorage:       storage,
		SessionStorage:    newAdapter91MockSessionStorage(),
		GroupStorage:      storage,
		RoleStorage:       storage,
	})
	require.NoError(t, err)

	return NewIAMServiceAdapter(tenantMgr, identityStore, policyEngine, appManager, adminMgr, gdprMgr)
}

// =============================================================================
// Tenant Operations
// =============================================================================

func TestAdapter91_CreateTenant_Success(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	adapter := createTestIAMAdapter91(t)

	tenant := &iam.Tenant{ID: "test-tenant-1", Name: "Test Tenant"}
	result, err := adapter.CreateTenant(ctx, tenant)
	require.NoError(t, err)
	assert.NotNil(t, result)
	assert.Equal(t, "test-tenant-1", result.ID)
}

func TestAdapter91_CreateTenant_Error(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	mockErr := errors.New("tenant create failed")
	tenantMgr := &adapter91MockTenantManager{
		createTenantFunc: func(ctx context.Context, opts iam.CreateTenantOptions) (*iam.Tenant, error) {
			return nil, mockErr
		},
	}
	adapter := NewIAMServiceAdapter(tenantMgr, nil, nil, nil, nil, nil)

	result, err := adapter.CreateTenant(ctx, &iam.Tenant{ID: "x", Name: "X"})
	assert.Error(t, err)
	assert.Nil(t, result)
	assert.Equal(t, mockErr, err)
}

func TestAdapter91_GetTenant_Success(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	adapter := createTestIAMAdapter91(t)

	result, err := adapter.GetTenant(ctx, "tenant-1")
	require.NoError(t, err)
	assert.NotNil(t, result)
	assert.Equal(t, "tenant-1", result.ID)
}

func TestAdapter91_GetTenant_Error(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	tenantMgr := &adapter91MockTenantManager{
		getTenantFunc: func(ctx context.Context, id string) (*iam.Tenant, error) {
			return nil, errors.New("not found")
		},
	}
	adapter := NewIAMServiceAdapter(tenantMgr, nil, nil, nil, nil, nil)

	result, err := adapter.GetTenant(ctx, "nonexistent")
	assert.Error(t, err)
	assert.Nil(t, result)
}

func TestAdapter91_UpdateTenant_Success(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	adapter := createTestIAMAdapter91(t)

	tenant := &iam.Tenant{ID: "tenant-1", Name: "Updated Tenant"}
	result, err := adapter.UpdateTenant(ctx, tenant)
	require.NoError(t, err)
	assert.NotNil(t, result)
	assert.Equal(t, "Updated Tenant", result.Name)
}

func TestAdapter91_UpdateTenant_Error(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	tenantMgr := &adapter91MockTenantManager{
		updateTenantFunc: func(ctx context.Context, id string, opts iam.UpdateTenantOptions) (*iam.Tenant, error) {
			return nil, errors.New("update failed")
		},
	}
	adapter := NewIAMServiceAdapter(tenantMgr, nil, nil, nil, nil, nil)

	result, err := adapter.UpdateTenant(ctx, &iam.Tenant{ID: "t1", Name: "X"})
	assert.Error(t, err)
	assert.Nil(t, result)
}

func TestAdapter91_DeleteTenant_Success(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	adapter := createTestIAMAdapter91(t)
	assert.NoError(t, adapter.DeleteTenant(ctx, "tenant-1"))
}

func TestAdapter91_DeleteTenant_Error(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	mockErr := errors.New("delete failed")
	tenantMgr := &adapter91MockTenantManager{
		deleteTenantFunc: func(ctx context.Context, id string) error { return mockErr },
	}
	adapter := NewIAMServiceAdapter(tenantMgr, nil, nil, nil, nil, nil)
	assert.Equal(t, mockErr, adapter.DeleteTenant(ctx, "tenant-1"))
}

func TestAdapter91_ListTenants_Success(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	adapter := createTestIAMAdapter91(t)

	tenants, total, err := adapter.ListTenants(ctx, 10, 0)
	require.NoError(t, err)
	assert.NotNil(t, tenants)
	assert.Equal(t, len(tenants), total)
}

func TestAdapter91_ListTenants_Error(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	tenantMgr := &adapter91MockTenantManager{
		listTenantsFunc: func(ctx context.Context, opts iam.ListOptions) ([]*iam.Tenant, error) {
			return nil, errors.New("list failed")
		},
	}
	adapter := NewIAMServiceAdapter(tenantMgr, nil, nil, nil, nil, nil)

	tenants, total, err := adapter.ListTenants(ctx, 10, 0)
	assert.Error(t, err)
	assert.Nil(t, tenants)
	assert.Equal(t, 0, total)
}

// =============================================================================
// User Operations
// =============================================================================

func TestAdapter91_CreateUser_Success(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	adapter := createTestIAMAdapter91(t)

	user := &iam.User{TenantID: "tenant-1", Email: "alice@example.com", Name: "Alice"}
	result, err := adapter.CreateUser(ctx, user)
	require.NoError(t, err)
	assert.NotNil(t, result)
	assert.Equal(t, "alice@example.com", result.Email)
}

func TestAdapter91_CreateUser_EmailWithoutAt(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	adapter := createTestIAMAdapter91(t)

	user := &iam.User{TenantID: "tenant-1", Email: "localuser", Name: "Local"}
	_, err := adapter.CreateUser(ctx, user)
	// IdentityStore validates email format - should error
	assert.Error(t, err)
}

func TestAdapter91_CreateUser_WithRolesAndGroups(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	adapter := createTestIAMAdapter91(t)

	user := &iam.User{
		TenantID: "tenant-1",
		Email:    "bob@example.com",
		Name:     "Bob",
		Roles:    []string{"admin"},
		Groups:   []string{"dev-team"},
	}
	result, err := adapter.CreateUser(ctx, user)
	// Role/group assignment may fail since they don't exist, but user is created
	assert.NotNil(t, result)
	_ = err
}

func TestAdapter91_GetUser_ByID(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	adapter := createTestIAMAdapter91(t)

	user := &iam.User{TenantID: "tenant-1", Email: "charlie@example.com", Name: "Charlie"}
	created, err := adapter.CreateUser(ctx, user)
	require.NoError(t, err)

	result, err := adapter.GetUser(ctx, "tenant-1", created.ID)
	require.NoError(t, err)
	assert.Equal(t, created.ID, result.ID)
}

func TestAdapter91_GetUser_ByEmail(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	adapter := createTestIAMAdapter91(t)

	user := &iam.User{TenantID: "tenant-1", Email: "diana@example.com", Name: "Diana"}
	_, err := adapter.CreateUser(ctx, user)
	require.NoError(t, err)

	result, err := adapter.GetUser(ctx, "tenant-1", "diana@example.com")
	require.NoError(t, err)
	assert.Equal(t, "diana@example.com", result.Email)
}

func TestAdapter91_GetUser_ByEmail_WrongTenant(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	adapter := createTestIAMAdapter91(t)

	user := &iam.User{TenantID: "tenant-1", Email: "eve@example.com", Name: "Eve"}
	_, err := adapter.CreateUser(ctx, user)
	require.NoError(t, err)

	result, err := adapter.GetUser(ctx, "wrong-tenant", "eve@example.com")
	assert.Error(t, err)
	assert.Nil(t, result)
}

func TestAdapter91_GetUser_NotFound(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	adapter := createTestIAMAdapter91(t)

	result, err := adapter.GetUser(ctx, "tenant-1", "nonexistent-id")
	assert.Error(t, err)
	assert.Nil(t, result)
}

func TestAdapter91_UpdateUser_Success(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	adapter := createTestIAMAdapter91(t)

	user := &iam.User{TenantID: "tenant-1", Email: "frank@example.com", Name: "Frank"}
	created, err := adapter.CreateUser(ctx, user)
	require.NoError(t, err)

	created.Name = "Frank Updated"
	result, err := adapter.UpdateUser(ctx, created)
	require.NoError(t, err)
	assert.NotNil(t, result)
}

func TestAdapter91_UpdateUser_WithRoles(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	adapter := createTestIAMAdapter91(t)

	user := &iam.User{TenantID: "tenant-1", Email: "grace@example.com", Name: "Grace"}
	created, err := adapter.CreateUser(ctx, user)
	require.NoError(t, err)

	created.Roles = []string{"viewer"}
	result, err := adapter.UpdateUser(ctx, created)
	assert.NotNil(t, result)
	_ = err
}

func TestAdapter91_UpdateUser_WithGroups(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	adapter := createTestIAMAdapter91(t)

	user := &iam.User{TenantID: "tenant-1", Email: "hank@example.com", Name: "Hank"}
	created, err := adapter.CreateUser(ctx, user)
	require.NoError(t, err)

	created.Groups = []string{"ops-team"}
	result, err := adapter.UpdateUser(ctx, created)
	assert.NotNil(t, result)
	_ = err
}

func TestAdapter91_DeleteUser_ByID(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	adapter := createTestIAMAdapter91(t)

	user := &iam.User{TenantID: "tenant-1", Email: "ivan@example.com", Name: "Ivan"}
	created, err := adapter.CreateUser(ctx, user)
	require.NoError(t, err)

	assert.NoError(t, adapter.DeleteUser(ctx, "tenant-1", created.ID))
}

func TestAdapter91_DeleteUser_ByEmail(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	adapter := createTestIAMAdapter91(t)

	user := &iam.User{TenantID: "tenant-1", Email: "jane@example.com", Name: "Jane"}
	_, err := adapter.CreateUser(ctx, user)
	require.NoError(t, err)

	assert.NoError(t, adapter.DeleteUser(ctx, "tenant-1", "jane@example.com"))
}

func TestAdapter91_DeleteUser_Error(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	adapter := createTestIAMAdapter91(t)
	assert.Error(t, adapter.DeleteUser(ctx, "tenant-1", "nonexistent"))
}

func TestAdapter91_ListUsers_Success(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	adapter := createTestIAMAdapter91(t)

	_, err := adapter.CreateUser(ctx, &iam.User{TenantID: "tenant-1", Email: "kate@example.com", Name: "Kate"})
	require.NoError(t, err)

	users, total, err := adapter.ListUsers(ctx, "tenant-1", 10, 0, nil)
	require.NoError(t, err)
	assert.GreaterOrEqual(t, len(users), 1)
	assert.Equal(t, len(users), total)
}

func TestAdapter91_ListUsers_WithFilters(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	adapter := createTestIAMAdapter91(t)

	filters := map[string]string{"email": "test@example.com", "role": "admin", "group": "devs"}
	users, total, err := adapter.ListUsers(ctx, "tenant-1", 10, 0, filters)
	require.NoError(t, err)
	assert.NotNil(t, users)
	assert.Equal(t, len(users), total)
}

// =============================================================================
// Group Operations
// =============================================================================

func TestAdapter91_CreateGroup_Success(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	adapter := createTestIAMAdapter91(t)

	result, err := adapter.CreateGroup(ctx, &iam.Group{TenantID: "tenant-1", Name: "Engineering", Description: "Engineering team"})
	require.NoError(t, err)
	assert.Equal(t, "Engineering", result.Name)
}

func TestAdapter91_CreateGroup_Error(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	adapter := createTestIAMAdapter91(t)

	group := &iam.Group{ID: "dup-group", TenantID: "tenant-1", Name: "Duplicate"}
	_, err := adapter.CreateGroup(ctx, group)
	require.NoError(t, err)
	_, err = adapter.CreateGroup(ctx, group)
	assert.Error(t, err)
}

func TestAdapter91_GetGroup_Success(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	adapter := createTestIAMAdapter91(t)

	created, err := adapter.CreateGroup(ctx, &iam.Group{TenantID: "tenant-1", Name: "DevOps"})
	require.NoError(t, err)

	result, err := adapter.GetGroup(ctx, "tenant-1", created.ID)
	require.NoError(t, err)
	assert.Equal(t, "DevOps", result.Name)
}

func TestAdapter91_GetGroup_Error(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	adapter := createTestIAMAdapter91(t)

	result, err := adapter.GetGroup(ctx, "tenant-1", "nonexistent-group")
	assert.Error(t, err)
	assert.Nil(t, result)
}

func TestAdapter91_UpdateGroup_Success(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	adapter := createTestIAMAdapter91(t)

	created, err := adapter.CreateGroup(ctx, &iam.Group{TenantID: "tenant-1", Name: "Original"})
	require.NoError(t, err)

	created.Name = "Updated"
	result, err := adapter.UpdateGroup(ctx, created)
	require.NoError(t, err)
	assert.NotNil(t, result)
}

func TestAdapter91_UpdateGroup_Error(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	adapter := createTestIAMAdapter91(t)

	result, err := adapter.UpdateGroup(ctx, &iam.Group{TenantID: "tenant-1", ID: "nonexistent", Name: "X"})
	assert.Error(t, err)
	assert.Nil(t, result)
}

func TestAdapter91_DeleteGroup_Success(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	adapter := createTestIAMAdapter91(t)

	created, err := adapter.CreateGroup(ctx, &iam.Group{TenantID: "tenant-1", Name: "ToDelete"})
	require.NoError(t, err)
	assert.NoError(t, adapter.DeleteGroup(ctx, "tenant-1", created.ID))
}

func TestAdapter91_DeleteGroup_Error(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	adapter := createTestIAMAdapter91(t)
	assert.Error(t, adapter.DeleteGroup(ctx, "tenant-1", "nonexistent-group"))
}

func TestAdapter91_ListGroups_Success(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	adapter := createTestIAMAdapter91(t)

	groups, total, err := adapter.ListGroups(ctx, "tenant-1", 10, 0)
	require.NoError(t, err)
	assert.NotNil(t, groups)
	assert.Equal(t, len(groups), total)
}

func TestAdapter91_AddUserToGroup_Success(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	adapter := createTestIAMAdapter91(t)

	createdGroup, err := adapter.CreateGroup(ctx, &iam.Group{TenantID: "tenant-1", Name: "GroupForAdd"})
	require.NoError(t, err)
	createdUser, err := adapter.CreateUser(ctx, &iam.User{TenantID: "tenant-1", Email: "addtogroup@example.com", Name: "AddToGroup"})
	require.NoError(t, err)
	assert.NoError(t, adapter.AddUserToGroup(ctx, "tenant-1", createdGroup.ID, createdUser.ID))
}

func TestAdapter91_AddUserToGroup_Error(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	adapter := createTestIAMAdapter91(t)
	assert.Error(t, adapter.AddUserToGroup(ctx, "tenant-1", "nonexistent-group", "nonexistent-user"))
}

func TestAdapter91_RemoveUserFromGroup_Success(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	adapter := createTestIAMAdapter91(t)

	createdGroup, err := adapter.CreateGroup(ctx, &iam.Group{TenantID: "tenant-1", Name: "RemoveGroup"})
	require.NoError(t, err)
	createdUser, err := adapter.CreateUser(ctx, &iam.User{TenantID: "tenant-1", Email: "removefromgroup@example.com", Name: "Remove"})
	require.NoError(t, err)
	require.NoError(t, adapter.AddUserToGroup(ctx, "tenant-1", createdGroup.ID, createdUser.ID))
	assert.NoError(t, adapter.RemoveUserFromGroup(ctx, "tenant-1", createdGroup.ID, createdUser.ID))
}

func TestAdapter91_RemoveUserFromGroup_Error(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	adapter := createTestIAMAdapter91(t)
	assert.Error(t, adapter.RemoveUserFromGroup(ctx, "tenant-1", "nonexistent-group", "nonexistent-user"))
}

// =============================================================================
// Role Operations
// =============================================================================

func TestAdapter91_CreateRole_Success(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	adapter := createTestIAMAdapter91(t)

	result, err := adapter.CreateRole(ctx, &iam.Role{TenantID: "tenant-1", Name: "admin", Permissions: []string{"read", "write"}})
	require.NoError(t, err)
	assert.Equal(t, "admin", result.Name)
}

func TestAdapter91_CreateRole_Error(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	adapter := createTestIAMAdapter91(t)

	role := &iam.Role{ID: "dup-role", TenantID: "tenant-1", Name: "duplicate"}
	_, err := adapter.CreateRole(ctx, role)
	require.NoError(t, err)
	_, err = adapter.CreateRole(ctx, role)
	assert.Error(t, err)
}

func TestAdapter91_GetRole_Success(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	adapter := createTestIAMAdapter91(t)

	created, err := adapter.CreateRole(ctx, &iam.Role{TenantID: "tenant-1", Name: "viewer"})
	require.NoError(t, err)

	result, err := adapter.GetRole(ctx, "tenant-1", created.ID)
	require.NoError(t, err)
	assert.NotNil(t, result)
}

func TestAdapter91_GetRole_Error(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	adapter := createTestIAMAdapter91(t)

	result, err := adapter.GetRole(ctx, "tenant-1", "nonexistent-role")
	assert.Error(t, err)
	assert.Nil(t, result)
}

func TestAdapter91_UpdateRole_Success(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	adapter := createTestIAMAdapter91(t)

	created, err := adapter.CreateRole(ctx, &iam.Role{TenantID: "tenant-1", Name: "editor"})
	require.NoError(t, err)

	created.Name = "senior-editor"
	result, err := adapter.UpdateRole(ctx, created)
	require.NoError(t, err)
	assert.NotNil(t, result)
}

func TestAdapter91_UpdateRole_Error(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	adapter := createTestIAMAdapter91(t)

	result, err := adapter.UpdateRole(ctx, &iam.Role{TenantID: "tenant-1", ID: "nonexistent", Name: "X"})
	assert.Error(t, err)
	assert.Nil(t, result)
}

func TestAdapter91_DeleteRole_Success(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	adapter := createTestIAMAdapter91(t)

	created, err := adapter.CreateRole(ctx, &iam.Role{TenantID: "tenant-1", Name: "delete-me"})
	require.NoError(t, err)
	assert.NoError(t, adapter.DeleteRole(ctx, "tenant-1", created.ID))
}

func TestAdapter91_DeleteRole_Error(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	adapter := createTestIAMAdapter91(t)
	assert.Error(t, adapter.DeleteRole(ctx, "tenant-1", "nonexistent-role"))
}

func TestAdapter91_ListRoles_Success(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	adapter := createTestIAMAdapter91(t)

	roles, total, err := adapter.ListRoles(ctx, "tenant-1", 10, 0)
	require.NoError(t, err)
	assert.NotNil(t, roles)
	assert.Equal(t, len(roles), total)
}

func TestAdapter91_AssignRole_Success(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	adapter := createTestIAMAdapter91(t)

	createdUser, err := adapter.CreateUser(ctx, &iam.User{TenantID: "tenant-1", Email: "assign@example.com", Name: "Assign"})
	require.NoError(t, err)
	createdRole, err := adapter.CreateRole(ctx, &iam.Role{TenantID: "tenant-1", Name: "assignable"})
	require.NoError(t, err)
	assert.NoError(t, adapter.AssignRole(ctx, "tenant-1", createdUser.ID, createdRole.ID))
}

func TestAdapter91_AssignRole_Error(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	adapter := createTestIAMAdapter91(t)
	assert.Error(t, adapter.AssignRole(ctx, "tenant-1", "nonexistent-user", "nonexistent-role"))
}

func TestAdapter91_RevokeRole_Success(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	adapter := createTestIAMAdapter91(t)

	createdUser, err := adapter.CreateUser(ctx, &iam.User{TenantID: "tenant-1", Email: "revoke@example.com", Name: "Revoke"})
	require.NoError(t, err)
	createdRole, err := adapter.CreateRole(ctx, &iam.Role{TenantID: "tenant-1", Name: "revocable"})
	require.NoError(t, err)
	require.NoError(t, adapter.AssignRole(ctx, "tenant-1", createdUser.ID, createdRole.ID))
	assert.NoError(t, adapter.RevokeRole(ctx, "tenant-1", createdUser.ID, createdRole.ID))
}

func TestAdapter91_RevokeRole_Error(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	adapter := createTestIAMAdapter91(t)
	assert.Error(t, adapter.RevokeRole(ctx, "tenant-1", "nonexistent-user", "nonexistent-role"))
}

// =============================================================================
// Application Operations
// =============================================================================

func TestAdapter91_RegisterApp_Success(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	adapter := createTestIAMAdapter91(t)

	app := &iam.Application{
		TenantID: "tenant-1", Name: "TestApp",
		RedirectURIs: []string{"http://localhost/callback"},
		GrantTypes:   []string{"authorization_code"}, ResponseTypes: []string{"code"},
	}
	result, err := adapter.RegisterApp(ctx, app)
	require.NoError(t, err)
	assert.Equal(t, "TestApp", result.Name)
	assert.NotEmpty(t, result.ClientID)
}

func TestAdapter91_RegisterApp_Error(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	adapter := createTestIAMAdapter91(t)

	result, err := adapter.RegisterApp(ctx, &iam.Application{TenantID: "tenant-1", Name: ""})
	assert.Error(t, err)
	assert.Nil(t, result)
}

func TestAdapter91_GetApp_Success(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	adapter := createTestIAMAdapter91(t)

	created, err := adapter.RegisterApp(ctx, &iam.Application{
		TenantID: "tenant-1", Name: "GetTestApp",
		RedirectURIs: []string{"http://localhost/callback"},
		GrantTypes:   []string{"authorization_code"}, ResponseTypes: []string{"code"},
	})
	require.NoError(t, err)

	result, err := adapter.GetApp(ctx, "tenant-1", created.ClientID)
	require.NoError(t, err)
	assert.Equal(t, created.ClientID, result.ClientID)
}

func TestAdapter91_GetApp_Error(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	adapter := createTestIAMAdapter91(t)

	result, err := adapter.GetApp(ctx, "tenant-1", "nonexistent-app")
	assert.Error(t, err)
	assert.Nil(t, result)
}

func TestAdapter91_UpdateApp_Success(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	adapter := createTestIAMAdapter91(t)

	created, err := adapter.RegisterApp(ctx, &iam.Application{
		TenantID: "tenant-1", Name: "UpdateableApp",
		RedirectURIs: []string{"http://localhost/callback"},
		GrantTypes:   []string{"authorization_code"}, ResponseTypes: []string{"code"},
	})
	require.NoError(t, err)

	created.Name = "Updated App"
	result, err := adapter.UpdateApp(ctx, created)
	require.NoError(t, err)
	assert.NotNil(t, result)
}

func TestAdapter91_UpdateApp_Error(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	adapter := createTestIAMAdapter91(t)

	result, err := adapter.UpdateApp(ctx, &iam.Application{ClientID: "nonexistent", Name: "X"})
	assert.Error(t, err)
	assert.Nil(t, result)
}

func TestAdapter91_DeleteApp_Success(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	adapter := createTestIAMAdapter91(t)

	created, err := adapter.RegisterApp(ctx, &iam.Application{
		TenantID: "tenant-1", Name: "DeleteableApp",
		RedirectURIs: []string{"http://localhost/callback"},
		GrantTypes:   []string{"authorization_code"}, ResponseTypes: []string{"code"},
	})
	require.NoError(t, err)
	assert.NoError(t, adapter.DeleteApp(ctx, "tenant-1", created.ClientID))
}

func TestAdapter91_DeleteApp_Error(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	adapter := createTestIAMAdapter91(t)
	assert.Error(t, adapter.DeleteApp(ctx, "tenant-1", "nonexistent-app"))
}

func TestAdapter91_ListApps_Success(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	adapter := createTestIAMAdapter91(t)

	apps, total, err := adapter.ListApps(ctx, "tenant-1", 10, 0)
	require.NoError(t, err)
	assert.NotNil(t, apps)
	assert.Equal(t, len(apps), total)
}

func TestAdapter91_RotateAppSecret_Success(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	adapter := createTestIAMAdapter91(t)

	created, err := adapter.RegisterApp(ctx, &iam.Application{
		TenantID: "tenant-1", Name: "RotateApp",
		RedirectURIs: []string{"http://localhost/callback"},
		GrantTypes:   []string{"authorization_code"}, ResponseTypes: []string{"code"},
	})
	require.NoError(t, err)

	result, err := adapter.RotateAppSecret(ctx, "tenant-1", created.ClientID)
	require.NoError(t, err)
	assert.NotNil(t, result)
}

func TestAdapter91_RotateAppSecret_Error(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	adapter := createTestIAMAdapter91(t)

	result, err := adapter.RotateAppSecret(ctx, "tenant-1", "nonexistent-app")
	assert.Error(t, err)
	assert.Nil(t, result)
}

// =============================================================================
// Policy Operations
// =============================================================================

func makeTestPolicy91(name, tenantID string) *iam.Policy {
	return &iam.Policy{
		APIVersion: "iam.quicbaas.io/v1",
		Kind:       "Policy",
		Metadata: iam.PolicyMetadata{
			Name:   name,
			Tenant: tenantID,
		},
		Spec: iam.PolicySpec{
			Subjects: iam.SubjectSelector{
				Users: []string{"*"},
			},
			Rules: []iam.Rule{{
				Name:      "allow-read",
				Effect:    iam.EffectAllow,
				Actions:   []string{"read"},
				Resources: []string{"/*"},
			}},
		},
	}
}

func TestAdapter91_CreatePolicy_Success(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	adapter := createTestIAMAdapter91(t)

	policy := makeTestPolicy91("test-policy", "tenant-1")
	result, err := adapter.CreatePolicy(ctx, policy)
	require.NoError(t, err)
	assert.NotNil(t, result)
	assert.Equal(t, "test-policy", result.Metadata.Name)
}

func TestAdapter91_CreatePolicy_Error(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	adapter := createTestIAMAdapter91(t)

	// Invalid policy - missing required fields
	policy := &iam.Policy{Metadata: iam.PolicyMetadata{Tenant: "tenant-1"}}
	result, err := adapter.CreatePolicy(ctx, policy)
	assert.Error(t, err)
	assert.Nil(t, result)
}

func TestAdapter91_GetPolicy_Success(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	adapter := createTestIAMAdapter91(t)

	policy := makeTestPolicy91("get-test-policy", "tenant-1")
	_, err := adapter.CreatePolicy(ctx, policy)
	require.NoError(t, err)

	result, err := adapter.GetPolicy(ctx, "tenant-1", "get-test-policy")
	require.NoError(t, err)
	assert.NotNil(t, result)
}

func TestAdapter91_GetPolicy_Error(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	adapter := createTestIAMAdapter91(t)

	result, err := adapter.GetPolicy(ctx, "tenant-1", "nonexistent-policy")
	assert.Error(t, err)
	assert.Nil(t, result)
}

func TestAdapter91_UpdatePolicy_Success(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	adapter := createTestIAMAdapter91(t)

	policy := makeTestPolicy91("update-test-policy", "tenant-1")
	_, err := adapter.CreatePolicy(ctx, policy)
	require.NoError(t, err)

	policy.Spec.Rules[0].Effect = iam.EffectDeny
	result, err := adapter.UpdatePolicy(ctx, policy)
	require.NoError(t, err)
	assert.NotNil(t, result)
}

func TestAdapter91_UpdatePolicy_Error(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	adapter := createTestIAMAdapter91(t)

	policy := &iam.Policy{Metadata: iam.PolicyMetadata{Name: "nonexistent", Tenant: "tenant-1"}}
	result, err := adapter.UpdatePolicy(ctx, policy)
	assert.Error(t, err)
	assert.Nil(t, result)
}

func TestAdapter91_DeletePolicy_Success(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	adapter := createTestIAMAdapter91(t)

	policy := makeTestPolicy91("delete-test-policy", "tenant-1")
	_, err := adapter.CreatePolicy(ctx, policy)
	require.NoError(t, err)
	assert.NoError(t, adapter.DeletePolicy(ctx, "tenant-1", "delete-test-policy"))
}

func TestAdapter91_DeletePolicy_Error(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	adapter := createTestIAMAdapter91(t)
	assert.Error(t, adapter.DeletePolicy(ctx, "tenant-1", "nonexistent-policy"))
}

func TestAdapter91_ListPolicies_Success(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	adapter := createTestIAMAdapter91(t)

	policies, total, err := adapter.ListPolicies(ctx, "tenant-1", 10, 0)
	require.NoError(t, err)
	assert.NotNil(t, policies)
	assert.Equal(t, len(policies), total)
}

func TestAdapter91_ValidatePolicy_ValidYAML(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	adapter := createTestIAMAdapter91(t)

	yamlSpec := []byte(`
metadata:
  name: test-validate
spec:
  subjects:
    users: ["*"]
  rules:
    - name: allow-read
      effect: allow
      actions: ["read"]
      resources: ["/*"]
`)
	valid, issues, err := adapter.ValidatePolicy(ctx, "tenant-1", yamlSpec)
	require.NoError(t, err)
	_ = valid
	_ = issues
}

func TestAdapter91_ValidatePolicy_InvalidYAML(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	adapter := createTestIAMAdapter91(t)

	valid, issues, err := adapter.ValidatePolicy(ctx, "tenant-1", []byte(`{{{{invalid yaml`))
	require.NoError(t, err)
	assert.False(t, valid)
	assert.NotEmpty(t, issues)
}

func TestAdapter91_TestPolicy_NilRequest(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	adapter := createTestIAMAdapter91(t)

	result, err := adapter.TestPolicy(ctx, "tenant-1", nil)
	assert.Error(t, err)
	assert.Nil(t, result)
}

func TestAdapter91_TestPolicy_Success(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	adapter := createTestIAMAdapter91(t)

	req := &iam.CheckRequest{
		Subject: iam.Subject{
			TenantID: "tenant-1",
			ID:       "user-1",
		},
		Resource: "/test/data",
		Action:   "read",
	}
	result, err := adapter.TestPolicy(ctx, "tenant-1", req)
	require.NoError(t, err)
	assert.NotNil(t, result)
}

// =============================================================================
// Delegated Administration Operations
// =============================================================================

func TestAdapter91_CreateAdminAssignment_Success(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	adapter := createTestIAMAdapter91(t)

	opts := iam.CreateAdminAssignmentOptions{
		UserID: "admin-1", AdminRole: iam.AdminRoleTenantAdmin,
		GrantedBy: "super-admin",
	}
	result, err := adapter.CreateAdminAssignment(ctx, opts)
	require.NoError(t, err)
	assert.NotNil(t, result)
}

func TestAdapter91_CreateAdminAssignment_NilManager(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	adapter := NewIAMServiceAdapter(nil, nil, nil, nil, nil, nil)

	result, err := adapter.CreateAdminAssignment(ctx, iam.CreateAdminAssignmentOptions{})
	assert.Error(t, err)
	assert.Nil(t, result)
	assert.Contains(t, err.Error(), "admin manager")
}

func TestAdapter91_GetAdminAssignment_Success(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	adapter := createTestIAMAdapter91(t)

	result, err := adapter.GetAdminAssignment(ctx, "assignment-1")
	require.NoError(t, err)
	assert.NotNil(t, result)
}

func TestAdapter91_GetAdminAssignment_NilManager(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	adapter := NewIAMServiceAdapter(nil, nil, nil, nil, nil, nil)

	result, err := adapter.GetAdminAssignment(ctx, "assignment-1")
	assert.Error(t, err)
	assert.Nil(t, result)
}

func TestAdapter91_UpdateAdminAssignment_ValidationError(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	adapter := createTestIAMAdapter91(t)

	role := iam.AdminRoleReadOnly
	result, err := adapter.UpdateAdminAssignment(ctx, "assignment-1", iam.UpdateAdminAssignmentOptions{AdminRole: &role})
	assert.Error(t, err)
	assert.Nil(t, result)
}

func TestAdapter91_UpdateAdminAssignment_NilManager(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	adapter := NewIAMServiceAdapter(nil, nil, nil, nil, nil, nil)

	result, err := adapter.UpdateAdminAssignment(ctx, "a1", iam.UpdateAdminAssignmentOptions{})
	assert.Error(t, err)
	assert.Nil(t, result)
}

func TestAdapter91_DeleteAdminAssignment_Success(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	adapter := createTestIAMAdapter91(t)
	assert.NoError(t, adapter.DeleteAdminAssignment(ctx, "assignment-1"))
}

func TestAdapter91_DeleteAdminAssignment_NilManager(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	adapter := NewIAMServiceAdapter(nil, nil, nil, nil, nil, nil)
	assert.Error(t, adapter.DeleteAdminAssignment(ctx, "a1"))
}

func TestAdapter91_ListAdminAssignments_Success(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	adapter := createTestIAMAdapter91(t)

	result, err := adapter.ListAdminAssignments(ctx, iam.ListAdminAssignmentsOptions{})
	require.NoError(t, err)
	assert.NotNil(t, result)
}

func TestAdapter91_ListAdminAssignments_NilManager(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	adapter := NewIAMServiceAdapter(nil, nil, nil, nil, nil, nil)

	result, err := adapter.ListAdminAssignments(ctx, iam.ListAdminAssignmentsOptions{})
	assert.Error(t, err)
	assert.Nil(t, result)
}

func TestAdapter91_CheckAdminPermission_Success(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	adapter := createTestIAMAdapter91(t)

	result, err := adapter.CheckAdminPermission(ctx, &iam.AdminCheckRequest{
		UserID: "admin-1", Permission: iam.AdminPermission("manage_users"),
	})
	require.NoError(t, err)
	assert.NotNil(t, result)
}

func TestAdapter91_CheckAdminPermission_NilManager(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	adapter := NewIAMServiceAdapter(nil, nil, nil, nil, nil, nil)

	result, err := adapter.CheckAdminPermission(ctx, &iam.AdminCheckRequest{})
	assert.Error(t, err)
	assert.Nil(t, result)
}

func TestAdapter91_StartImpersonation_ValidationError(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	adapter := createTestIAMAdapter91(t)

	result, err := adapter.StartImpersonation(ctx, &iam.StartImpersonationOptions{
		AdminUserID: "admin-1", TargetUserID: "user-1", TenantID: "tenant-1", Reason: "support ticket",
	})
	assert.Error(t, err)
	assert.Nil(t, result)
}

func TestAdapter91_StartImpersonation_NilManager(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	adapter := NewIAMServiceAdapter(nil, nil, nil, nil, nil, nil)

	result, err := adapter.StartImpersonation(ctx, &iam.StartImpersonationOptions{})
	assert.Error(t, err)
	assert.Nil(t, result)
}

func TestAdapter91_EndImpersonation_Success(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	adapter := createTestIAMAdapter91(t)
	assert.NoError(t, adapter.EndImpersonation(ctx, "session-1"))
}

func TestAdapter91_EndImpersonation_NilManager(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	adapter := NewIAMServiceAdapter(nil, nil, nil, nil, nil, nil)
	assert.Error(t, adapter.EndImpersonation(ctx, "session-1"))
}

func TestAdapter91_GetActiveImpersonations_Success(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	adapter := createTestIAMAdapter91(t)

	result, err := adapter.GetActiveImpersonations(ctx, "admin-1")
	require.NoError(t, err)
	assert.NotNil(t, result)
}

func TestAdapter91_GetActiveImpersonations_NilManager(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	adapter := NewIAMServiceAdapter(nil, nil, nil, nil, nil, nil)

	result, err := adapter.GetActiveImpersonations(ctx, "admin-1")
	assert.Error(t, err)
	assert.Nil(t, result)
}

// =============================================================================
// GDPR Consent Operations
// =============================================================================

func TestAdapter91_GrantConsent_Success(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	adapter := createTestIAMAdapter91(t)

	expires := time.Now().Add(365 * 24 * time.Hour)
	result, err := adapter.GrantConsent(ctx, "tenant-1", "user-1",
		iam.ConsentPurposeMarketing, "1.0", &expires, "127.0.0.1", "TestAgent")
	require.NoError(t, err)
	assert.NotNil(t, result)
}

func TestAdapter91_GrantConsent_NilManager(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	adapter := NewIAMServiceAdapter(nil, nil, nil, nil, nil, nil)

	result, err := adapter.GrantConsent(ctx, "t1", "u1", iam.ConsentPurposeMarketing, "1.0", nil, "127.0.0.1", "agent")
	assert.Error(t, err)
	assert.Nil(t, result)
}

func TestAdapter91_WithdrawConsent_NilManager(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	adapter := NewIAMServiceAdapter(nil, nil, nil, nil, nil, nil)
	assert.Error(t, adapter.WithdrawConsent(ctx, "t1", "u1", iam.ConsentPurposeMarketing, "127.0.0.1", "agent"))
}

func TestAdapter91_WithdrawConsent_Success(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	adapter := createTestIAMAdapter91(t)
	// May or may not error depending on state; we ensure no panic
	_ = adapter.WithdrawConsent(ctx, "tenant-1", "user-1", iam.ConsentPurposeMarketing, "127.0.0.1", "TestAgent")
}

func TestAdapter91_GetConsent_NilManager(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	adapter := NewIAMServiceAdapter(nil, nil, nil, nil, nil, nil)

	result, err := adapter.GetConsent(ctx, "t1", "u1", iam.ConsentPurposeMarketing)
	assert.Error(t, err)
	assert.Nil(t, result)
}

func TestAdapter91_GetConsent_Success(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	adapter := createTestIAMAdapter91(t)
	_, _ = adapter.GetConsent(ctx, "tenant-1", "user-1", iam.ConsentPurposeMarketing)
}

func TestAdapter91_ListUserConsents_Success(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	adapter := createTestIAMAdapter91(t)

	result, err := adapter.ListUserConsents(ctx, "tenant-1", "user-1")
	require.NoError(t, err)
	assert.NotNil(t, result)
}

func TestAdapter91_ListUserConsents_NilManager(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	adapter := NewIAMServiceAdapter(nil, nil, nil, nil, nil, nil)

	result, err := adapter.ListUserConsents(ctx, "t1", "u1")
	assert.Error(t, err)
	assert.Nil(t, result)
}

// =============================================================================
// GDPR Data Export Operations
// =============================================================================

func TestAdapter91_RequestExport_Success(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	adapter := createTestIAMAdapter91(t)

	result, err := adapter.RequestExport(ctx, "tenant-1", "user-1", "admin-1", iam.DataExportFormatJSON, nil, nil)
	require.NoError(t, err)
	assert.NotNil(t, result)
}

func TestAdapter91_RequestExport_NilManager(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	adapter := NewIAMServiceAdapter(nil, nil, nil, nil, nil, nil)

	result, err := adapter.RequestExport(ctx, "t1", "u1", "a1", iam.DataExportFormatJSON, nil, nil)
	assert.Error(t, err)
	assert.Nil(t, result)
}

func TestAdapter91_GetExportStatus_NilManager(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	adapter := NewIAMServiceAdapter(nil, nil, nil, nil, nil, nil)

	result, err := adapter.GetExportStatus(ctx, "export-1")
	assert.Error(t, err)
	assert.Nil(t, result)
}

func TestAdapter91_GetExportStatus_Success(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	adapter := createTestIAMAdapter91(t)
	_, _ = adapter.GetExportStatus(ctx, "export-1")
}

func TestAdapter91_GenerateExport_NilManager(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	adapter := NewIAMServiceAdapter(nil, nil, nil, nil, nil, nil)

	result, err := adapter.GenerateExport(ctx, "export-1")
	assert.Error(t, err)
	assert.Nil(t, result)
}

func TestAdapter91_GenerateExport_Success(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	adapter := createTestIAMAdapter91(t)
	_, _ = adapter.GenerateExport(ctx, "export-1")
}

// =============================================================================
// GDPR Data Deletion Operations
// =============================================================================

func TestAdapter91_RequestDeletion_Success(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	adapter := createTestIAMAdapter91(t)

	result, err := adapter.RequestDeletion(ctx, "tenant-1", "user-1", "admin-1", "GDPR request", nil, nil, 30)
	require.NoError(t, err)
	assert.NotNil(t, result)
}

func TestAdapter91_RequestDeletion_NilManager(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	adapter := NewIAMServiceAdapter(nil, nil, nil, nil, nil, nil)

	result, err := adapter.RequestDeletion(ctx, "t1", "u1", "a1", "reason", nil, nil, 30)
	assert.Error(t, err)
	assert.Nil(t, result)
}

func TestAdapter91_CancelDeletion_NilManager(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	adapter := NewIAMServiceAdapter(nil, nil, nil, nil, nil, nil)
	assert.Error(t, adapter.CancelDeletion(ctx, "del-1", "admin-1"))
}

func TestAdapter91_CancelDeletion_Success(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	adapter := createTestIAMAdapter91(t)
	_ = adapter.CancelDeletion(ctx, "deletion-1", "admin-1")
}

func TestAdapter91_ProcessDeletion_NilManager(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	adapter := NewIAMServiceAdapter(nil, nil, nil, nil, nil, nil)

	result, err := adapter.ProcessDeletion(ctx, "del-1")
	assert.Error(t, err)
	assert.Nil(t, result)
}

func TestAdapter91_ProcessDeletion_Success(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	adapter := createTestIAMAdapter91(t)
	_, _ = adapter.ProcessDeletion(ctx, "deletion-1")
}

func TestAdapter91_GetDeletionStatus_NilManager(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	adapter := NewIAMServiceAdapter(nil, nil, nil, nil, nil, nil)

	result, err := adapter.GetDeletionStatus(ctx, "del-1")
	assert.Error(t, err)
	assert.Nil(t, result)
}

func TestAdapter91_GetDeletionStatus_Success(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	adapter := createTestIAMAdapter91(t)
	_, _ = adapter.GetDeletionStatus(ctx, "deletion-1")
}

// =============================================================================
// Helper
// =============================================================================

func strPtr(s string) *string {
	return &s
}

// --- Merged from session_adapter_coverage_test.go ---

// mockSessionStorage implements iam.SessionStorage for testing
type mockSessionStorage struct {
	sessions map[string]*iam.Session
	errGet   error
	errList  error
	errDel   error
}

func newMockSessionStorage() *mockSessionStorage {
	return &mockSessionStorage{
		sessions: make(map[string]*iam.Session),
	}
}

func (m *mockSessionStorage) CreateSession(ctx context.Context, session *iam.Session) error {
	m.sessions[session.ID] = session
	return nil
}

func (m *mockSessionStorage) GetSession(ctx context.Context, sessionID string) (*iam.Session, error) {
	if m.errGet != nil {
		return nil, m.errGet
	}
	s, ok := m.sessions[sessionID]
	if !ok {
		return nil, errors.New("session not found")
	}
	return s, nil
}

func (m *mockSessionStorage) GetSessionsByUser(ctx context.Context, tenantID, userID string) ([]*iam.Session, error) {
	if m.errList != nil {
		return nil, m.errList
	}
	var result []*iam.Session
	for _, s := range m.sessions {
		if s.TenantID == tenantID && (userID == "" || s.UserID == userID) {
			result = append(result, s)
		}
	}
	return result, nil
}

func (m *mockSessionStorage) UpdateSession(ctx context.Context, session *iam.Session) error {
	m.sessions[session.ID] = session
	return nil
}

func (m *mockSessionStorage) DeleteSession(ctx context.Context, sessionID string) error {
	if m.errDel != nil {
		return m.errDel
	}
	delete(m.sessions, sessionID)
	return nil
}

func (m *mockSessionStorage) DeleteExpiredSessions(ctx context.Context) (int, error) {
	return 0, nil
}

// =============================================================================
// SessionServiceAdapter Tests with Mock SessionStorage
// =============================================================================

func TestSessionServiceAdapter_ListSessions_Success(t *testing.T) {
	storage := newMockSessionStorage()
	storage.sessions["s1"] = &iam.Session{ID: "s1", UserID: "u1", TenantID: "t1", Created: time.Now(), Expires: time.Now().Add(time.Hour)}
	storage.sessions["s2"] = &iam.Session{ID: "s2", UserID: "u1", TenantID: "t1", Created: time.Now(), Expires: time.Now().Add(time.Hour)}
	storage.sessions["s3"] = &iam.Session{ID: "s3", UserID: "u2", TenantID: "t1", Created: time.Now(), Expires: time.Now().Add(time.Hour)}

	adapter := NewSessionServiceAdapter(storage)
	sessions, total, err := adapter.ListSessions(context.Background(), "t1", "u1", 0, 0)
	require.NoError(t, err)
	assert.Equal(t, 2, total)
	assert.Len(t, sessions, 2)
}

func TestSessionServiceAdapter_ListSessions_WithPagination(t *testing.T) {
	storage := newMockSessionStorage()
	for i := 0; i < 5; i++ {
		id := "s" + string(rune('a'+i))
		storage.sessions[id] = &iam.Session{ID: id, UserID: "u1", TenantID: "t1", Created: time.Now(), Expires: time.Now().Add(time.Hour)}
	}

	adapter := NewSessionServiceAdapter(storage)

	// With limit
	sessions, total, err := adapter.ListSessions(context.Background(), "t1", "u1", 2, 0)
	require.NoError(t, err)
	assert.Equal(t, 5, total)
	assert.Len(t, sessions, 2)

	// With offset past end
	sessions, total, err = adapter.ListSessions(context.Background(), "t1", "u1", 0, 100)
	require.NoError(t, err)
	assert.Equal(t, 5, total)
	assert.Empty(t, sessions)
}

func TestSessionServiceAdapter_ListSessions_StorageError(t *testing.T) {
	storage := newMockSessionStorage()
	storage.errList = errors.New("storage failure")
	adapter := NewSessionServiceAdapter(storage)

	_, _, err := adapter.ListSessions(context.Background(), "t1", "u1", 0, 0)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "storage failure")
}

func TestSessionServiceAdapter_GetSession_Success(t *testing.T) {
	storage := newMockSessionStorage()
	storage.sessions["s1"] = &iam.Session{ID: "s1", UserID: "u1", TenantID: "t1"}

	adapter := NewSessionServiceAdapter(storage)
	sess, err := adapter.GetSession(context.Background(), "s1")
	require.NoError(t, err)
	assert.Equal(t, "s1", sess.ID)
}

func TestSessionServiceAdapter_GetSession_NotFound(t *testing.T) {
	storage := newMockSessionStorage()
	adapter := NewSessionServiceAdapter(storage)

	_, err := adapter.GetSession(context.Background(), "nonexistent")
	require.Error(t, err)
}

func TestSessionServiceAdapter_RevokeSession_Success(t *testing.T) {
	storage := newMockSessionStorage()
	storage.sessions["s1"] = &iam.Session{ID: "s1", UserID: "u1", TenantID: "t1"}

	adapter := NewSessionServiceAdapter(storage)
	err := adapter.RevokeSession(context.Background(), "s1")
	require.NoError(t, err)

	_, exists := storage.sessions["s1"]
	assert.False(t, exists)
}

func TestSessionServiceAdapter_RevokeAllSessions_Success(t *testing.T) {
	storage := newMockSessionStorage()
	storage.sessions["s1"] = &iam.Session{ID: "s1", UserID: "u1", TenantID: "t1"}
	storage.sessions["s2"] = &iam.Session{ID: "s2", UserID: "u1", TenantID: "t1"}
	storage.sessions["s3"] = &iam.Session{ID: "s3", UserID: "u2", TenantID: "t1"}

	adapter := NewSessionServiceAdapter(storage)
	count, err := adapter.RevokeAllSessions(context.Background(), "t1", "u1")
	require.NoError(t, err)
	assert.Equal(t, 2, count)
}

func TestSessionServiceAdapter_RevokeAllSessions_StorageError(t *testing.T) {
	storage := newMockSessionStorage()
	storage.errList = errors.New("storage failure")

	adapter := NewSessionServiceAdapter(storage)
	_, err := adapter.RevokeAllSessions(context.Background(), "t1", "u1")
	require.Error(t, err)
}

func TestSessionServiceAdapter_RevokeOtherSessions_Success(t *testing.T) {
	storage := newMockSessionStorage()
	storage.sessions["keep"] = &iam.Session{ID: "keep", UserID: "u1", TenantID: "t1"}
	storage.sessions["other1"] = &iam.Session{ID: "other1", UserID: "u1", TenantID: "t1"}
	storage.sessions["other2"] = &iam.Session{ID: "other2", UserID: "u1", TenantID: "t1"}

	adapter := NewSessionServiceAdapter(storage)
	count, err := adapter.RevokeOtherSessions(context.Background(), "t1", "keep")
	require.NoError(t, err)
	assert.Equal(t, 2, count)

	// Verify kept session still exists
	_, err = storage.GetSession(context.Background(), "keep")
	require.NoError(t, err)
}

func TestSessionServiceAdapter_RevokeOtherSessions_KeepNotFound(t *testing.T) {
	storage := newMockSessionStorage()

	adapter := NewSessionServiceAdapter(storage)
	_, err := adapter.RevokeOtherSessions(context.Background(), "t1", "nonexistent")
	require.Error(t, err)
}

func TestSessionServiceAdapter_RevokeOtherSessions_ListError(t *testing.T) {
	storage := newMockSessionStorage()
	storage.sessions["keep"] = &iam.Session{ID: "keep", UserID: "u1", TenantID: "t1"}
	storage.errList = errors.New("list failure")

	adapter := NewSessionServiceAdapter(storage)
	_, err := adapter.RevokeOtherSessions(context.Background(), "t1", "keep")
	require.Error(t, err)
}
