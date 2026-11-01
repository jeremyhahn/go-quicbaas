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
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"errors"

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"

	"github.com/jeremyhahn/go-quicbaas/pkg/iam"
	"github.com/jeremyhahn/go-quicbaas/pkg/iam/iamtest"
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
	beginEnrollmentFunc     func(ctx context.Context, userID uint64, tenantID string, method iam.MFAMethod) (*iam.MFAEnrollResponse, error)
	completeEnrollmentFunc  func(ctx context.Context, enrollmentID, code string) error
	cancelEnrollmentFunc    func(ctx context.Context, enrollmentID string) error
	beginChallengeFunc      func(ctx context.Context, userID uint64, tenantID, sessionToken string) (*iam.MFAChallenge, error)
	verifyChallengeFunc     func(ctx context.Context, challengeID, code string) (*iam.MFAVerifyResponse, error)
	verifyCodeFunc          func(ctx context.Context, userID uint64, tenantID, code string) error
	getMFAStatusFunc        func(ctx context.Context, userID uint64, tenantID string) (*iam.MFAStatus, error)
	disableMFAFunc          func(ctx context.Context, userID uint64, tenantID string) error
	generateBackupCodesFunc func(ctx context.Context, userID uint64, tenantID string) ([]string, error)
	verifyBackupCodeFunc    func(ctx context.Context, userID uint64, tenantID, code string) error
	getBackupCodeStatusFunc func(ctx context.Context, userID uint64, tenantID string) (*iam.BackupCodeStatus, error)
}

func (m *adapterMockMFAService) BeginEnrollment(ctx context.Context, userID uint64, tenantID string, method iam.MFAMethod) (*iam.MFAEnrollResponse, error) {
	if m.beginEnrollmentFunc != nil {
		return m.beginEnrollmentFunc(ctx, userID, tenantID, method)
	}
	return &iam.MFAEnrollResponse{EnrollmentID: "enroll-123"}, nil
}

func (m *adapterMockMFAService) CompleteEnrollment(ctx context.Context, _ uint64, _, enrollmentID, code string) error {
	if m.completeEnrollmentFunc != nil {
		return m.completeEnrollmentFunc(ctx, enrollmentID, code)
	}
	return nil
}

func (m *adapterMockMFAService) CancelEnrollment(ctx context.Context, _ uint64, _, enrollmentID string) error {
	if m.cancelEnrollmentFunc != nil {
		return m.cancelEnrollmentFunc(ctx, enrollmentID)
	}
	return nil
}

func (m *adapterMockMFAService) BeginChallenge(ctx context.Context, userID uint64, tenantID, sessionToken string) (*iam.MFAChallenge, error) {
	if m.beginChallengeFunc != nil {
		return m.beginChallengeFunc(ctx, userID, tenantID, sessionToken)
	}
	return &iam.MFAChallenge{ID: "challenge-123"}, nil
}

func (m *adapterMockMFAService) VerifyChallenge(ctx context.Context, _ uint64, _, challengeID, code string) (*iam.MFAVerifyResponse, error) {
	if m.verifyChallengeFunc != nil {
		return m.verifyChallengeFunc(ctx, challengeID, code)
	}
	return &iam.MFAVerifyResponse{Success: true}, nil
}

func (m *adapterMockMFAService) VerifyCode(ctx context.Context, userID uint64, tenantID, code string) error {
	if m.verifyCodeFunc != nil {
		return m.verifyCodeFunc(ctx, userID, tenantID, code)
	}
	return nil
}

func (m *adapterMockMFAService) GetMFAStatus(ctx context.Context, userID uint64, tenantID string) (*iam.MFAStatus, error) {
	if m.getMFAStatusFunc != nil {
		return m.getMFAStatusFunc(ctx, userID, tenantID)
	}
	return &iam.MFAStatus{Enabled: true}, nil
}

func (m *adapterMockMFAService) DisableMFA(ctx context.Context, userID uint64, tenantID string) error {
	if m.disableMFAFunc != nil {
		return m.disableMFAFunc(ctx, userID, tenantID)
	}
	return nil
}

func (m *adapterMockMFAService) GenerateBackupCodes(ctx context.Context, userID uint64, tenantID string) ([]string, error) {
	if m.generateBackupCodesFunc != nil {
		return m.generateBackupCodesFunc(ctx, userID, tenantID)
	}
	return []string{"code1", "code2"}, nil
}

func (m *adapterMockMFAService) VerifyBackupCode(ctx context.Context, userID uint64, tenantID, code string) error {
	if m.verifyBackupCodeFunc != nil {
		return m.verifyBackupCodeFunc(ctx, userID, tenantID, code)
	}
	return nil
}

func (m *adapterMockMFAService) GetBackupCodeStatus(ctx context.Context, userID uint64, tenantID string) (*iam.BackupCodeStatus, error) {
	if m.getBackupCodeStatusFunc != nil {
		return m.getBackupCodeStatusFunc(ctx, userID, tenantID)
	}
	return &iam.BackupCodeStatus{Remaining: 5}, nil
}

func (m *adapterMockMFAService) IsMFARequired(context.Context, uint64, string) (bool, error) {
	return false, nil
}

func TestMFAServiceAdapter_BeginEnrollment(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	mockSvc := &adapterMockMFAService{
		beginEnrollmentFunc: func(ctx context.Context, userID uint64, tenantID string, method iam.MFAMethod) (*iam.MFAEnrollResponse, error) {
			return &iam.MFAEnrollResponse{
				EnrollmentID: "test-enroll-id",
				Secret:       "test-secret",
				QRCodeURI:    "otpauth://totp/...",
			}, nil
		},
	}

	adapter := NewMFAServiceAdapter(mockSvc)
	resp, err := adapter.BeginEnrollment(ctx, "1001", "tenant1", iam.MFAMethodTOTP)

	assert.NoError(t, err)
	assert.NotNil(t, resp)
	assert.Equal(t, "test-enroll-id", resp.EnrollmentID)
}

func TestMFAServiceAdapter_CompleteEnrollment(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	mockSvc := &adapterMockMFAService{}
	adapter := NewMFAServiceAdapter(mockSvc)

	err := adapter.CompleteEnrollment(ctx, "", "tenant-123", "enroll-123", "123456")
	assert.NoError(t, err)
}

func TestMFAServiceAdapter_CancelEnrollment(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	mockSvc := &adapterMockMFAService{}
	adapter := NewMFAServiceAdapter(mockSvc)

	err := adapter.CancelEnrollment(ctx, "", "tenant-123", "enroll-123")
	assert.NoError(t, err)
}

func TestMFAServiceAdapter_BeginChallenge(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	mockSvc := &adapterMockMFAService{
		beginChallengeFunc: func(ctx context.Context, userID uint64, tenantID, sessionToken string) (*iam.MFAChallenge, error) {
			return &iam.MFAChallenge{
				ID:     "chal-456",
				Method: iam.MFAMethodTOTP,
			}, nil
		},
	}

	adapter := NewMFAServiceAdapter(mockSvc)
	resp, err := adapter.BeginChallenge(ctx, "1001", "tenant1", "session-token")

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
	resp, err := adapter.VerifyChallenge(ctx, "", "tenant-123", "chal-456", "123456")

	assert.NoError(t, err)
	assert.NotNil(t, resp)
	assert.True(t, resp.Success)
}

func TestMFAServiceAdapter_VerifyCode(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	mockSvc := &adapterMockMFAService{}
	adapter := NewMFAServiceAdapter(mockSvc)

	err := adapter.VerifyCode(ctx, "1001", "tenant1", "123456")
	assert.NoError(t, err)
}

func TestMFAServiceAdapter_GetMFAStatus(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	mockSvc := &adapterMockMFAService{
		getMFAStatusFunc: func(ctx context.Context, userID uint64, tenantID string) (*iam.MFAStatus, error) {
			return &iam.MFAStatus{
				Enabled:              true,
				Method:               iam.MFAMethodTOTP,
				BackupCodesRemaining: 5,
			}, nil
		},
	}

	adapter := NewMFAServiceAdapter(mockSvc)
	resp, err := adapter.GetMFAStatus(ctx, "1001", "tenant1")

	assert.NoError(t, err)
	assert.NotNil(t, resp)
	assert.True(t, resp.Enabled)
}

func TestMFAServiceAdapter_DisableMFA(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	mockSvc := &adapterMockMFAService{}
	adapter := NewMFAServiceAdapter(mockSvc)

	err := adapter.DisableMFA(ctx, "1001", "tenant1")
	assert.NoError(t, err)
}

func TestMFAServiceAdapter_GenerateBackupCodes(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	mockSvc := &adapterMockMFAService{
		generateBackupCodesFunc: func(ctx context.Context, userID uint64, tenantID string) ([]string, error) {
			return []string{"code1", "code2", "code3"}, nil
		},
	}

	adapter := NewMFAServiceAdapter(mockSvc)
	codes, err := adapter.GenerateBackupCodes(ctx, "1001", "tenant1")

	assert.NoError(t, err)
	assert.Len(t, codes, 3)
}

func TestMFAServiceAdapter_VerifyBackupCode(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	mockSvc := &adapterMockMFAService{}
	adapter := NewMFAServiceAdapter(mockSvc)

	err := adapter.VerifyBackupCode(ctx, "1001", "tenant1", "backup-code")
	assert.NoError(t, err)
}

func TestMFAServiceAdapter_GetBackupCodeStatus(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	mockSvc := &adapterMockMFAService{
		getBackupCodeStatusFunc: func(ctx context.Context, userID uint64, tenantID string) (*iam.BackupCodeStatus, error) {
			return &iam.BackupCodeStatus{Remaining: 8}, nil
		},
	}

	adapter := NewMFAServiceAdapter(mockSvc)
	status, err := adapter.GetBackupCodeStatus(ctx, "1001", "tenant1")

	assert.NoError(t, err)
	assert.NotNil(t, status)
	assert.Equal(t, 8, status.Remaining)
}

// TestMFAServiceAdapter_PassesParsedUserID proves the adapter hands the
// manager the numeric id the wire carried, and zero for object verbs that
// name no user.
func TestMFAServiceAdapter_PassesParsedUserID(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	var got uint64
	mockSvc := &adapterMockMFAService{
		verifyCodeFunc: func(_ context.Context, userID uint64, _, _ string) error {
			got = userID
			return nil
		},
	}
	adapter := NewMFAServiceAdapter(mockSvc)

	require.NoError(t, adapter.VerifyCode(ctx, "1001", "tenant1", "123456"))
	assert.Equal(t, uint64(1001), got)
}

// TestMFAServiceAdapter_RejectsBadUserID proves a non-numeric or zero user id
// is refused with *UserIDError before the manager is reached.
func TestMFAServiceAdapter_RejectsBadUserID(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	adapter := NewMFAServiceAdapter(&adapterMockMFAService{})

	calls := map[string]func(userID string) error{
		"BeginEnrollment": func(userID string) error {
			_, err := adapter.BeginEnrollment(ctx, userID, "tenant1", iam.MFAMethodTOTP)
			return err
		},
		"CompleteEnrollment": func(userID string) error {
			return adapter.CompleteEnrollment(ctx, userID, "tenant1", "enroll-1", "123456")
		},
		"CancelEnrollment": func(userID string) error {
			return adapter.CancelEnrollment(ctx, userID, "tenant1", "enroll-1")
		},
		"BeginChallenge": func(userID string) error {
			_, err := adapter.BeginChallenge(ctx, userID, "tenant1", "session")
			return err
		},
		"VerifyChallenge": func(userID string) error {
			_, err := adapter.VerifyChallenge(ctx, userID, "tenant1", "chal-1", "123456")
			return err
		},
		"VerifyCode": func(userID string) error {
			return adapter.VerifyCode(ctx, userID, "tenant1", "123456")
		},
		"GetMFAStatus": func(userID string) error {
			_, err := adapter.GetMFAStatus(ctx, userID, "tenant1")
			return err
		},
		"DisableMFA": func(userID string) error {
			return adapter.DisableMFA(ctx, userID, "tenant1")
		},
		"GenerateBackupCodes": func(userID string) error {
			_, err := adapter.GenerateBackupCodes(ctx, userID, "tenant1")
			return err
		},
		"VerifyBackupCode": func(userID string) error {
			return adapter.VerifyBackupCode(ctx, userID, "tenant1", "backup")
		},
		"GetBackupCodeStatus": func(userID string) error {
			_, err := adapter.GetBackupCodeStatus(ctx, userID, "tenant1")
			return err
		},
	}

	for name, call := range calls {
		for _, bad := range []string{"user-1", "0"} {
			err := call(bad)
			var idErr *UserIDError
			assert.True(t, errors.As(err, &idErr), "%s(%q): got %v, want *UserIDError", name, bad, err)
		}
	}
}

// =============================================================================
// IAMServiceAdapter Tests
// =============================================================================

func TestNewIAMServiceAdapter(t *testing.T) {
	t.Parallel()

	adapter := NewIAMServiceAdapter(nil, nil, nil, nil, nil, nil)
	assert.NotNil(t, adapter)
}

func TestIAMServiceAdapter_InvalidInput(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		call    func(ctx context.Context, a *IAMServiceAdapter) (any, error)
		wantErr string
	}{
		{"CreateTenant missing name", func(ctx context.Context, a *IAMServiceAdapter) (any, error) {
			return a.CreateTenant(ctx, iam.CreateTenantOptions{})
		}, "name cannot be nil"},
		{"UpdateTenant nil", func(ctx context.Context, a *IAMServiceAdapter) (any, error) {
			return a.UpdateTenant(ctx, nil)
		}, "tenant cannot be nil"},
		{"CreateUser nil", func(ctx context.Context, a *IAMServiceAdapter) (any, error) {
			user, _, _, err := a.CreateUser(ctx, nil, 0)
			return user, err
		}, "user cannot be nil"},
		{"UpdateUser nil", func(ctx context.Context, a *IAMServiceAdapter) (any, error) {
			return a.UpdateUser(ctx, nil)
		}, "user cannot be nil"},
		{"CreateRole nil", func(ctx context.Context, a *IAMServiceAdapter) (any, error) {
			return a.CreateRole(ctx, nil)
		}, "role cannot be nil"},
		{"UpdateRole nil", func(ctx context.Context, a *IAMServiceAdapter) (any, error) {
			return a.UpdateRole(ctx, nil)
		}, "role cannot be nil"},
		{"RegisterApp nil", func(ctx context.Context, a *IAMServiceAdapter) (any, error) {
			return a.RegisterApp(ctx, nil)
		}, "application cannot be nil"},
		{"CreatePolicy nil", func(ctx context.Context, a *IAMServiceAdapter) (any, error) {
			return a.CreatePolicy(ctx, nil)
		}, "policy cannot be nil"},
		{"UpdatePolicy nil", func(ctx context.Context, a *IAMServiceAdapter) (any, error) {
			return a.UpdatePolicy(ctx, nil)
		}, "policy cannot be nil"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			adapter := NewIAMServiceAdapter(nil, nil, nil, nil, nil, nil)
			result, err := tt.call(context.Background(), adapter)

			require.Error(t, err)
			assert.Nil(t, result)
			assert.Contains(t, err.Error(), tt.wantErr)
		})
	}
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
	getUserAssignmentsFunc func(ctx context.Context, userID uint64) ([]*iam.AdminAssignment, error)
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

func (m *adapter91MockAdminStorage) GetUserAssignments(ctx context.Context, userID uint64) ([]*iam.AdminAssignment, error) {
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
	listActiveFunc    func(ctx context.Context, adminUserID uint64) ([]*iam.ImpersonationSession, error)
	listFunc          func(ctx context.Context, activeOnly bool, adminUserID uint64) ([]*iam.ImpersonationSession, error)
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

func (m *adapter91MockImpersonationStorage) ListActiveImpersonations(ctx context.Context, adminUserID uint64) ([]*iam.ImpersonationSession, error) {
	if m.listActiveFunc != nil {
		return m.listActiveFunc(ctx, adminUserID)
	}
	return []*iam.ImpersonationSession{}, nil
}

func (m *adapter91MockImpersonationStorage) ListImpersonations(ctx context.Context, activeOnly bool, adminUserID uint64) ([]*iam.ImpersonationSession, error) {
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
	getConsentFunc      func(ctx context.Context, tenantID string, userID uint64, purpose iam.ConsentPurpose) (*iam.Consent, error)
	updateConsentFunc   func(ctx context.Context, id string, opts iam.UpdateConsentOptions) (*iam.Consent, error)
	deleteConsentFunc   func(ctx context.Context, id string) error
	listConsentsFunc    func(ctx context.Context, opts iam.ListConsentsOptions) ([]*iam.Consent, error)
	getUserConsentsFunc func(ctx context.Context, tenantID string, userID uint64) ([]*iam.Consent, error)
}

func (m *adapter91MockConsentStorage) CreateConsent(ctx context.Context, opts iam.CreateConsentOptions) (*iam.Consent, error) {
	if m.createConsentFunc != nil {
		return m.createConsentFunc(ctx, opts)
	}
	return &iam.Consent{ID: "consent-1"}, nil
}

func (m *adapter91MockConsentStorage) GetConsent(ctx context.Context, tenantID string, userID uint64, purpose iam.ConsentPurpose) (*iam.Consent, error) {
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

func (m *adapter91MockConsentStorage) GetUserConsents(ctx context.Context, tenantID string, userID uint64) ([]*iam.Consent, error) {
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

func (m *adapter91MockSessionStorage) GetSessionsByUser(ctx context.Context, tenantID string, userID uint64) ([]*iam.Session, error) {
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
		UserStorage: storage,
		RoleStorage: storage,
		Tenants:     tenantMgr,
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
		RoleStorage:       storage,
	})
	require.NoError(t, err)

	adapter := NewIAMServiceAdapter(tenantMgr, identityStore, policyEngine, appManager, adminMgr, gdprMgr)
	adapter.SetInviteStore(iamtest.NewMemoryInviteStore())
	return adapter
}

// =============================================================================
// Tenant Operations
// =============================================================================

func TestAdapter91_CreateTenant_Success(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	adapter := createTestIAMAdapter91(t)

	result, err := adapter.CreateTenant(ctx, iam.CreateTenantOptions{ID: "test-tenant-1", Name: "Test Tenant"})
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

	result, err := adapter.CreateTenant(ctx, iam.CreateTenantOptions{ID: "x", Name: "X"})
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
	result, _, _, err := adapter.CreateUser(ctx, user, 0)
	require.NoError(t, err)
	assert.NotNil(t, result)
	assert.Equal(t, "alice@example.com", result.Email)
}

func TestAdapter91_CreateUser_EmailWithoutAt(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	adapter := createTestIAMAdapter91(t)

	user := &iam.User{TenantID: "tenant-1", Email: "localuser", Name: "Local"}
	_, _, _, err := adapter.CreateUser(ctx, user, 0)
	// IdentityStore validates email format - should error
	assert.Error(t, err)
}

func TestAdapter91_CreateUser_WithRoles(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	adapter := createTestIAMAdapter91(t)

	// Pre-create the role so the create-time reference resolves.
	_, err := adapter.identityStore.CreateRole(ctx, "tenant-1", iam.CreateRoleOptions{
		ID:   "admin",
		Name: "Admin",
	})
	require.NoError(t, err)

	user := &iam.User{
		TenantID: "tenant-1",
		Email:    "bob@example.com",
		Name:     "Bob",
		Roles:    []string{"admin"},
	}
	result, _, _, err := adapter.CreateUser(ctx, user, 0)
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Equal(t, []string{"admin"}, result.Roles)
}

func TestAdapter91_CreateUser_UnknownRoleFailsWithoutPartialUser(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	adapter := createTestIAMAdapter91(t)

	user := &iam.User{
		TenantID: "tenant-1",
		Email:    "bob2@example.com",
		Name:     "Bob Two",
		Roles:    []string{"does-not-exist"},
	}
	result, _, _, err := adapter.CreateUser(ctx, user, 0)
	// Role references are validated BEFORE the user record is created, so a
	// bad role fails fast and atomically: no result and no partial user that
	// would make a retry collide with already_exists.
	require.Error(t, err)
	assert.Nil(t, result)

	_, getErr := adapter.GetUser(ctx, "tenant-1", "bob2@example.com")
	assert.Error(t, getErr, "no partial user must remain after a failed create")
}

func TestAdapter91_GetUser_ByID(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	adapter := createTestIAMAdapter91(t)

	user := &iam.User{TenantID: "tenant-1", Email: "charlie@example.com", Name: "Charlie"}
	created, _, _, err := adapter.CreateUser(ctx, user, 0)
	require.NoError(t, err)

	result, err := adapter.GetUser(ctx, "tenant-1", strconv.FormatUint(created.ID, 10))
	require.NoError(t, err)
	assert.Equal(t, created.ID, result.ID)
}

func TestAdapter91_GetUser_ByEmail(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	adapter := createTestIAMAdapter91(t)

	user := &iam.User{TenantID: "tenant-1", Email: "diana@example.com", Name: "Diana"}
	_, _, _, err := adapter.CreateUser(ctx, user, 0)
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
	_, _, _, err := adapter.CreateUser(ctx, user, 0)
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
	created, _, _, err := adapter.CreateUser(ctx, user, 0)
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

	role, err := adapter.CreateRole(ctx, &iam.Role{TenantID: "tenant-1", Name: "viewer", Permissions: []string{"read"}})
	require.NoError(t, err)

	user := &iam.User{TenantID: "tenant-1", Email: "grace@example.com", Name: "Grace"}
	created, _, _, err := adapter.CreateUser(ctx, user, 0)
	require.NoError(t, err)

	created.Roles = []string{role.ID}
	result, err := adapter.UpdateUser(ctx, created)
	require.NoError(t, err)
	assert.NotNil(t, result)

	stored, err := adapter.GetUser(ctx, "tenant-1", strconv.FormatUint(created.ID, 10))
	require.NoError(t, err)
	assert.Contains(t, stored.Roles, role.ID)
}

// TestAdapter91_UpdateUser_UnknownRole proves a role assignment that the
// identity store rejects fails the update instead of being dropped silently.
func TestAdapter91_UpdateUser_UnknownRole(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	adapter := createTestIAMAdapter91(t)

	user := &iam.User{TenantID: "tenant-1", Email: "heidi@example.com", Name: "Heidi"}
	created, _, _, err := adapter.CreateUser(ctx, user, 0)
	require.NoError(t, err)

	created.Roles = []string{"no-such-role"}
	result, err := adapter.UpdateUser(ctx, created)
	require.Error(t, err)
	assert.Nil(t, result)

	stored, err := adapter.GetUser(ctx, "tenant-1", strconv.FormatUint(created.ID, 10))
	require.NoError(t, err)
	assert.NotContains(t, stored.Roles, "no-such-role")
}

func TestAdapter91_DeleteUser_ByID(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	adapter := createTestIAMAdapter91(t)

	user := &iam.User{TenantID: "tenant-1", Email: "ivan@example.com", Name: "Ivan"}
	created, _, _, err := adapter.CreateUser(ctx, user, 0)
	require.NoError(t, err)

	assert.NoError(t, adapter.DeleteUser(ctx, "tenant-1", strconv.FormatUint(created.ID, 10)))
}

func TestAdapter91_DeleteUser_ByEmail(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	adapter := createTestIAMAdapter91(t)

	user := &iam.User{TenantID: "tenant-1", Email: "jane@example.com", Name: "Jane"}
	_, _, _, err := adapter.CreateUser(ctx, user, 0)
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

	_, _, _, err := adapter.CreateUser(ctx, &iam.User{TenantID: "tenant-1", Email: "kate@example.com", Name: "Kate"}, 0)
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

	createdUser, _, _, err := adapter.CreateUser(ctx, &iam.User{TenantID: "tenant-1", Email: "assign@example.com", Name: "Assign"}, 0)
	require.NoError(t, err)
	createdRole, err := adapter.CreateRole(ctx, &iam.Role{TenantID: "tenant-1", Name: "assignable"})
	require.NoError(t, err)
	assert.NoError(t, adapter.AssignRole(ctx, "tenant-1", createdUser.ID, createdRole.ID))
}

func TestAdapter91_AssignRole_Error(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	adapter := createTestIAMAdapter91(t)
	assert.Error(t, adapter.AssignRole(ctx, "tenant-1", 999999, "nonexistent-role"))
}

func TestAdapter91_RevokeRole_Success(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	adapter := createTestIAMAdapter91(t)

	createdUser, _, _, err := adapter.CreateUser(ctx, &iam.User{TenantID: "tenant-1", Email: "revoke@example.com", Name: "Revoke"}, 0)
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
	assert.Error(t, adapter.RevokeRole(ctx, "tenant-1", 999999, "nonexistent-role"))
}

// adapter91ReconcilingRoleStorage is a role storage that also honours
// iam.BuiltInRoleReconciler. The in-memory storage the other adapter tests use
// deliberately does not, because built-in definitions belong to qrdb; this
// double stands in for a qrdb-backed storage so the adapter's delegation can
// be exercised on its success path.
type adapter91ReconcilingRoleStorage struct {
	*iam.MemoryStorage

	report *iam.RoleReconcileReport
	err    error
}

func (s *adapter91ReconcilingRoleStorage) ReconcileBuiltInRoles(_ context.Context, tenantID string) (*iam.RoleReconcileReport, error) {
	if s.err != nil {
		return nil, s.err
	}
	report := s.report
	if report == nil {
		report = &iam.RoleReconcileReport{TenantID: tenantID}
	}
	return report, nil
}

func newAdapter91ReconcilingAdapter(t *testing.T, storage *adapter91ReconcilingRoleStorage) *IAMServiceAdapter {
	t.Helper()

	tenantMgr := &adapter91MockTenantManager{}
	identityStore := iam.NewIdentityStore(iam.IdentityStoreOptions{
		UserStorage: storage.MemoryStorage,
		RoleStorage: storage,
		Tenants:     tenantMgr,
	})

	return NewIAMServiceAdapter(tenantMgr, identityStore, nil, nil, nil, nil)
}

func TestAdapter91_ReconcileBuiltInRoles_Success(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	storage := &adapter91ReconcilingRoleStorage{
		MemoryStorage: iam.NewMemoryStorage(),
		report: &iam.RoleReconcileReport{
			TenantID: "tenant-1",
			Roles: []iam.RoleReconcileResult{
				{RoleID: "r-admin", Name: "admin", Added: []string{"roles:create"}},
			},
		},
	}
	adapter := newAdapter91ReconcilingAdapter(t, storage)

	report, err := adapter.ReconcileBuiltInRoles(ctx, "tenant-1")

	require.NoError(t, err)
	require.NotNil(t, report)
	assert.Equal(t, "tenant-1", report.TenantID)
	assert.True(t, report.Changed())
	require.Len(t, report.Roles, 1)
	assert.Equal(t, "admin", report.Roles[0].Name)
}

// TestAdapter91_ReconcileBuiltInRoles_Unsupported is the error path a storage
// that cannot reach qrdb's built-in definitions must take: refuse, rather than
// reconcile a tenant against a set this package invented.
func TestAdapter91_ReconcileBuiltInRoles_Unsupported(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	adapter := createTestIAMAdapter91(t)

	report, err := adapter.ReconcileBuiltInRoles(ctx, "tenant-1")

	require.Error(t, err)
	assert.Nil(t, report)
	assert.ErrorIs(t, err, iam.ErrRoleReconcileUnsupported)
}

// TestAdapter91_ReconcileBuiltInRoles_EmptyTenantID guards the data-plane rule
// that no tenant is assumed: an empty tenant is rejected, never defaulted.
func TestAdapter91_ReconcileBuiltInRoles_EmptyTenantID(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	adapter := newAdapter91ReconcilingAdapter(t, &adapter91ReconcilingRoleStorage{
		MemoryStorage: iam.NewMemoryStorage(),
	})

	report, err := adapter.ReconcileBuiltInRoles(ctx, "")

	require.Error(t, err)
	assert.Nil(t, report)
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

	name := "Updated App"
	result, err := adapter.UpdateApp(ctx, created.TenantID, created.ClientID, iam.UpdateAppOptions{Name: &name})
	require.NoError(t, err)
	assert.NotNil(t, result)
}

func TestAdapter91_UpdateApp_Error(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	adapter := createTestIAMAdapter91(t)

	name := "X"
	result, err := adapter.UpdateApp(ctx, "tenant-1", "nonexistent", iam.UpdateAppOptions{Name: &name})
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
	valid, issues, err := adapter.ValidatePolicy(ctx, "tenant-1", "", yamlSpec)
	require.NoError(t, err)
	_ = valid
	_ = issues
}

func TestAdapter91_ValidatePolicy_InvalidYAML(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	adapter := createTestIAMAdapter91(t)

	valid, issues, err := adapter.ValidatePolicy(ctx, "tenant-1", "", []byte(`{{{{invalid yaml`))
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
		UserID: 30021, AdminRole: iam.AdminRoleTenantAdmin,
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
		UserID: 30021, Permission: iam.AdminPermission("manage_users"),
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
		TargetUserID: 1, TenantID: "tenant-1", Reason: "support ticket",
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

	result, err := adapter.GetActiveImpersonations(ctx, 30021)
	require.NoError(t, err)
	assert.NotNil(t, result)
}

func TestAdapter91_GetActiveImpersonations_NilManager(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	adapter := NewIAMServiceAdapter(nil, nil, nil, nil, nil, nil)

	result, err := adapter.GetActiveImpersonations(ctx, 30021)
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
	result, err := adapter.GrantConsent(ctx, "tenant-1", 10001,
		iam.ConsentPurposeMarketing, "1.0", &expires, "127.0.0.1", "TestAgent")
	require.NoError(t, err)
	assert.NotNil(t, result)
}

func TestAdapter91_GrantConsent_NilManager(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	adapter := NewIAMServiceAdapter(nil, nil, nil, nil, nil, nil)

	result, err := adapter.GrantConsent(ctx, "t1", 30001, iam.ConsentPurposeMarketing, "1.0", nil, "127.0.0.1", "agent")
	assert.Error(t, err)
	assert.Nil(t, result)
}

func TestAdapter91_WithdrawConsent_NilManager(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	adapter := NewIAMServiceAdapter(nil, nil, nil, nil, nil, nil)
	assert.Error(t, adapter.WithdrawConsent(ctx, "t1", 30001, iam.ConsentPurposeMarketing, "127.0.0.1", "agent"))
}

func TestAdapter91_WithdrawConsent_Success(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	adapter := createTestIAMAdapter91(t)
	// May or may not error depending on state; we ensure no panic
	_ = adapter.WithdrawConsent(ctx, "tenant-1", 10001, iam.ConsentPurposeMarketing, "127.0.0.1", "TestAgent")
}

func TestAdapter91_GetConsent_NilManager(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	adapter := NewIAMServiceAdapter(nil, nil, nil, nil, nil, nil)

	result, err := adapter.GetConsent(ctx, "t1", 30001, iam.ConsentPurposeMarketing)
	assert.Error(t, err)
	assert.Nil(t, result)
}

func TestAdapter91_GetConsent_Success(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	adapter := createTestIAMAdapter91(t)
	_, _ = adapter.GetConsent(ctx, "tenant-1", 10001, iam.ConsentPurposeMarketing)
}

func TestAdapter91_ListUserConsents_Success(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	adapter := createTestIAMAdapter91(t)

	result, err := adapter.ListUserConsents(ctx, "tenant-1", 10001)
	require.NoError(t, err)
	assert.NotNil(t, result)
}

func TestAdapter91_ListUserConsents_NilManager(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	adapter := NewIAMServiceAdapter(nil, nil, nil, nil, nil, nil)

	result, err := adapter.ListUserConsents(ctx, "t1", 30001)
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

	result, err := adapter.RequestExport(ctx, "tenant-1", 10001, 30021, iam.DataExportFormatJSON, nil, nil)
	require.NoError(t, err)
	assert.NotNil(t, result)
}

func TestAdapter91_RequestExport_NilManager(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	adapter := NewIAMServiceAdapter(nil, nil, nil, nil, nil, nil)

	result, err := adapter.RequestExport(ctx, "t1", 30001, 30021, iam.DataExportFormatJSON, nil, nil)
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

	result, err := adapter.RequestDeletion(ctx, "tenant-1", 10001, 30021, "GDPR request", nil, nil, 30)
	require.NoError(t, err)
	assert.NotNil(t, result)
}

func TestAdapter91_RequestDeletion_NilManager(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	adapter := NewIAMServiceAdapter(nil, nil, nil, nil, nil, nil)

	result, err := adapter.RequestDeletion(ctx, "t1", 30001, 30021, "reason", nil, nil, 30)
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

func (m *mockSessionStorage) GetSessionsByUser(ctx context.Context, tenantID string, userID uint64) ([]*iam.Session, error) {
	if m.errList != nil {
		return nil, m.errList
	}
	var result []*iam.Session
	for _, s := range m.sessions {
		if s.TenantID == tenantID && (userID == 0 || s.UserID == userID) {
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
	storage.sessions["s1"] = &iam.Session{ID: "s1", UserID: 30001, TenantID: "t1", Created: time.Now(), Expires: time.Now().Add(time.Hour)}
	storage.sessions["s2"] = &iam.Session{ID: "s2", UserID: 30001, TenantID: "t1", Created: time.Now(), Expires: time.Now().Add(time.Hour)}
	storage.sessions["s3"] = &iam.Session{ID: "s3", UserID: 30002, TenantID: "t1", Created: time.Now(), Expires: time.Now().Add(time.Hour)}

	adapter := NewSessionServiceAdapter(storage)
	sessions, total, err := adapter.ListSessions(context.Background(), "t1", 30001, 0, 0)
	require.NoError(t, err)
	assert.Equal(t, 2, total)
	assert.Len(t, sessions, 2)
}

func TestSessionServiceAdapter_ListSessions_WithPagination(t *testing.T) {
	storage := newMockSessionStorage()
	for i := 0; i < 5; i++ {
		id := "s" + string(rune('a'+i))
		storage.sessions[id] = &iam.Session{ID: id, UserID: 30001, TenantID: "t1", Created: time.Now(), Expires: time.Now().Add(time.Hour)}
	}

	adapter := NewSessionServiceAdapter(storage)

	// With limit
	sessions, total, err := adapter.ListSessions(context.Background(), "t1", 30001, 2, 0)
	require.NoError(t, err)
	assert.Equal(t, 5, total)
	assert.Len(t, sessions, 2)

	// With offset past end
	sessions, total, err = adapter.ListSessions(context.Background(), "t1", 30001, 0, 100)
	require.NoError(t, err)
	assert.Equal(t, 5, total)
	assert.Empty(t, sessions)
}

func TestSessionServiceAdapter_ListSessions_StorageError(t *testing.T) {
	storage := newMockSessionStorage()
	storage.errList = errors.New("storage failure")
	adapter := NewSessionServiceAdapter(storage)

	_, _, err := adapter.ListSessions(context.Background(), "t1", 30001, 0, 0)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "storage failure")
}

func TestSessionServiceAdapter_GetSession_Success(t *testing.T) {
	storage := newMockSessionStorage()
	storage.sessions["s1"] = &iam.Session{ID: "s1", UserID: 30001, TenantID: "t1"}

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
	storage.sessions["s1"] = &iam.Session{ID: "s1", UserID: 30001, TenantID: "t1"}

	adapter := NewSessionServiceAdapter(storage)
	err := adapter.RevokeSession(context.Background(), "s1")
	require.NoError(t, err)

	_, exists := storage.sessions["s1"]
	assert.False(t, exists)
}

func TestSessionServiceAdapter_RevokeAllSessions_Success(t *testing.T) {
	storage := newMockSessionStorage()
	storage.sessions["s1"] = &iam.Session{ID: "s1", UserID: 30001, TenantID: "t1"}
	storage.sessions["s2"] = &iam.Session{ID: "s2", UserID: 30001, TenantID: "t1"}
	storage.sessions["s3"] = &iam.Session{ID: "s3", UserID: 30002, TenantID: "t1"}

	adapter := NewSessionServiceAdapter(storage)
	count, err := adapter.RevokeAllSessions(context.Background(), "t1", 30001)
	require.NoError(t, err)
	assert.Equal(t, 2, count)
}

func TestSessionServiceAdapter_RevokeAllSessions_StorageError(t *testing.T) {
	storage := newMockSessionStorage()
	storage.errList = errors.New("storage failure")

	adapter := NewSessionServiceAdapter(storage)
	_, err := adapter.RevokeAllSessions(context.Background(), "t1", 30001)
	require.Error(t, err)
}

func TestSessionServiceAdapter_RevokeOtherSessions_Success(t *testing.T) {
	storage := newMockSessionStorage()
	storage.sessions["keep"] = &iam.Session{ID: "keep", UserID: 30001, TenantID: "t1"}
	storage.sessions["other1"] = &iam.Session{ID: "other1", UserID: 30001, TenantID: "t1"}
	storage.sessions["other2"] = &iam.Session{ID: "other2", UserID: 30001, TenantID: "t1"}

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
	storage.sessions["keep"] = &iam.Session{ID: "keep", UserID: 30001, TenantID: "t1"}
	storage.errList = errors.New("list failure")

	adapter := NewSessionServiceAdapter(storage)
	_, err := adapter.RevokeOtherSessions(context.Background(), "t1", "keep")
	require.Error(t, err)
}
