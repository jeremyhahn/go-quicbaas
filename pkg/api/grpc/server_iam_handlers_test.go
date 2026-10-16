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
	stderrors "errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"

	iampb "github.com/jeremyhahn/go-quicbaas/api/proto/iam"
	"github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/pkg/iam"
	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
)

// mockIAMService is a testify mock implementing the IAMService interface.
type mockIAMService struct {
	mock.Mock
}

func (m *mockIAMService) CreateTenant(ctx context.Context, opts iam.CreateTenantOptions) (*iam.Tenant, error) {
	args := m.Called(ctx, opts)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*iam.Tenant), args.Error(1)
}

func (m *mockIAMService) GetTenant(ctx context.Context, id string) (*iam.Tenant, error) {
	args := m.Called(ctx, id)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*iam.Tenant), args.Error(1)
}

func (m *mockIAMService) UpdateTenant(ctx context.Context, tenant *iam.Tenant) (*iam.Tenant, error) {
	args := m.Called(ctx, tenant)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*iam.Tenant), args.Error(1)
}

func (m *mockIAMService) DeleteTenant(ctx context.Context, id string) error {
	return m.Called(ctx, id).Error(0)
}

func (m *mockIAMService) ListTenants(ctx context.Context, limit, offset int) ([]*iam.Tenant, int, error) {
	args := m.Called(ctx, limit, offset)
	if args.Get(0) == nil {
		return nil, args.Int(1), args.Error(2)
	}
	return args.Get(0).([]*iam.Tenant), args.Int(1), args.Error(2)
}

func (m *mockIAMService) ProvisionTenant(ctx context.Context, req *service.IAMProvisionTenantRequest) (*service.IAMProvisionTenantResponse, error) {
	args := m.Called(ctx, req)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*service.IAMProvisionTenantResponse), args.Error(1)
}

func (m *mockIAMService) CreateUser(ctx context.Context, user *iam.User, inviteTTL time.Duration) (*iam.User, string, *iam.InviteRecord, error) {
	args := m.Called(ctx, user)
	if args.Get(0) == nil {
		return nil, "", nil, args.Error(1)
	}
	return args.Get(0).(*iam.User), "", nil, args.Error(1)
}

func (m *mockIAMService) GetUser(ctx context.Context, tenantID, userID string) (*iam.User, error) {
	args := m.Called(ctx, tenantID, userID)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*iam.User), args.Error(1)
}

func (m *mockIAMService) UpdateUser(ctx context.Context, user *iam.User) (*iam.User, error) {
	args := m.Called(ctx, user)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*iam.User), args.Error(1)
}

func (m *mockIAMService) DeleteUser(ctx context.Context, tenantID, userID string) error {
	return m.Called(ctx, tenantID, userID).Error(0)
}

func (m *mockIAMService) ListUsers(ctx context.Context, tenantID string, limit, offset int, filters map[string]string) ([]*iam.User, int, error) {
	args := m.Called(ctx, tenantID, limit, offset, filters)
	if args.Get(0) == nil {
		return nil, args.Int(1), args.Error(2)
	}
	return args.Get(0).([]*iam.User), args.Int(1), args.Error(2)
}

func (m *mockIAMService) CreateRole(ctx context.Context, role *iam.Role) (*iam.Role, error) {
	args := m.Called(ctx, role)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*iam.Role), args.Error(1)
}

func (m *mockIAMService) GetRole(ctx context.Context, tenantID, roleID string) (*iam.Role, error) {
	args := m.Called(ctx, tenantID, roleID)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*iam.Role), args.Error(1)
}

func (m *mockIAMService) UpdateRole(ctx context.Context, role *iam.Role) (*iam.Role, error) {
	args := m.Called(ctx, role)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*iam.Role), args.Error(1)
}

func (m *mockIAMService) DeleteRole(ctx context.Context, tenantID, roleID string) error {
	return m.Called(ctx, tenantID, roleID).Error(0)
}

func (m *mockIAMService) ListRoles(ctx context.Context, tenantID string, limit, offset int) ([]*iam.Role, int, error) {
	args := m.Called(ctx, tenantID, limit, offset)
	if args.Get(0) == nil {
		return nil, args.Int(1), args.Error(2)
	}
	return args.Get(0).([]*iam.Role), args.Int(1), args.Error(2)
}

func (m *mockIAMService) AssignRole(ctx context.Context, tenantID string, userID uint64, role string) error {
	return m.Called(ctx, tenantID, userID, role).Error(0)
}

func (m *mockIAMService) RevokeRole(ctx context.Context, tenantID string, userID uint64, role string) error {
	return m.Called(ctx, tenantID, userID, role).Error(0)
}

func (m *mockIAMService) ReconcileBuiltInRoles(ctx context.Context, tenantID string) (*iam.RoleReconcileReport, error) {
	args := m.Called(ctx, tenantID)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*iam.RoleReconcileReport), args.Error(1)
}

func (m *mockIAMService) RegisterApp(ctx context.Context, app *iam.Application) (*iam.Application, error) {
	args := m.Called(ctx, app)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*iam.Application), args.Error(1)
}

func (m *mockIAMService) GetApp(ctx context.Context, tenantID, clientID string) (*iam.Application, error) {
	args := m.Called(ctx, tenantID, clientID)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*iam.Application), args.Error(1)
}

func (m *mockIAMService) UpdateApp(ctx context.Context, app *iam.Application) (*iam.Application, error) {
	args := m.Called(ctx, app)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*iam.Application), args.Error(1)
}

func (m *mockIAMService) DeleteApp(ctx context.Context, tenantID, clientID string) error {
	return m.Called(ctx, tenantID, clientID).Error(0)
}

func (m *mockIAMService) ListApps(ctx context.Context, tenantID string, limit, offset int) ([]*iam.Application, int, error) {
	args := m.Called(ctx, tenantID, limit, offset)
	if args.Get(0) == nil {
		return nil, args.Int(1), args.Error(2)
	}
	return args.Get(0).([]*iam.Application), args.Int(1), args.Error(2)
}

func (m *mockIAMService) RotateAppSecret(ctx context.Context, tenantID, clientID string) (*iam.Application, error) {
	args := m.Called(ctx, tenantID, clientID)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*iam.Application), args.Error(1)
}

func (m *mockIAMService) CreatePolicy(ctx context.Context, policy *iam.Policy) (*iam.Policy, error) {
	args := m.Called(ctx, policy)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*iam.Policy), args.Error(1)
}

func (m *mockIAMService) GetPolicy(ctx context.Context, tenantID, policyName string) (*iam.Policy, error) {
	args := m.Called(ctx, tenantID, policyName)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*iam.Policy), args.Error(1)
}

func (m *mockIAMService) UpdatePolicy(ctx context.Context, policy *iam.Policy) (*iam.Policy, error) {
	args := m.Called(ctx, policy)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*iam.Policy), args.Error(1)
}

func (m *mockIAMService) DeletePolicy(ctx context.Context, tenantID, policyName string) error {
	return m.Called(ctx, tenantID, policyName).Error(0)
}

func (m *mockIAMService) ListPolicies(ctx context.Context, tenantID string, limit, offset int) ([]*iam.Policy, int, error) {
	args := m.Called(ctx, tenantID, limit, offset)
	if args.Get(0) == nil {
		return nil, args.Int(1), args.Error(2)
	}
	return args.Get(0).([]*iam.Policy), args.Int(1), args.Error(2)
}

func (m *mockIAMService) ValidatePolicy(ctx context.Context, tenantID, name string, specYAML []byte) (bool, []string, error) {
	args := m.Called(ctx, tenantID, name, specYAML)
	return args.Bool(0), args.Get(1).([]string), args.Error(2)
}

func (m *mockIAMService) TestPolicy(ctx context.Context, tenantID string, req *iam.CheckRequest) (*iam.CheckResponse, error) {
	args := m.Called(ctx, tenantID, req)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*iam.CheckResponse), args.Error(1)
}

func (m *mockIAMService) CreateAdminAssignment(ctx context.Context, opts iam.CreateAdminAssignmentOptions) (*iam.AdminAssignment, error) {
	args := m.Called(ctx, opts)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*iam.AdminAssignment), args.Error(1)
}

func (m *mockIAMService) GetAdminAssignment(ctx context.Context, id string) (*iam.AdminAssignment, error) {
	args := m.Called(ctx, id)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*iam.AdminAssignment), args.Error(1)
}

func (m *mockIAMService) UpdateAdminAssignment(ctx context.Context, id string, opts iam.UpdateAdminAssignmentOptions) (*iam.AdminAssignment, error) {
	args := m.Called(ctx, id, opts)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*iam.AdminAssignment), args.Error(1)
}

func (m *mockIAMService) DeleteAdminAssignment(ctx context.Context, id string) error {
	return m.Called(ctx, id).Error(0)
}

func (m *mockIAMService) ListAdminAssignments(ctx context.Context, opts iam.ListAdminAssignmentsOptions) ([]*iam.AdminAssignment, error) {
	args := m.Called(ctx, opts)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]*iam.AdminAssignment), args.Error(1)
}

func (m *mockIAMService) CheckAdminPermission(ctx context.Context, req *iam.AdminCheckRequest) (*iam.AdminCheckResponse, error) {
	args := m.Called(ctx, req)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*iam.AdminCheckResponse), args.Error(1)
}

func (m *mockIAMService) StartImpersonation(ctx context.Context, opts *iam.StartImpersonationOptions) (*iam.ImpersonationSession, error) {
	args := m.Called(ctx, opts)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*iam.ImpersonationSession), args.Error(1)
}

func (m *mockIAMService) EndImpersonation(ctx context.Context, sessionID string) error {
	return m.Called(ctx, sessionID).Error(0)
}

func (m *mockIAMService) GetActiveImpersonations(ctx context.Context, adminUserID uint64) ([]*iam.ImpersonationSession, error) {
	args := m.Called(ctx, adminUserID)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]*iam.ImpersonationSession), args.Error(1)
}

func (m *mockIAMService) GrantConsent(ctx context.Context, tenantID string, userID uint64, purpose iam.ConsentPurpose, version string, expiresAt *time.Time, ipAddress, userAgent string) (*iam.Consent, error) {
	args := m.Called(ctx, tenantID, userID, purpose, version, expiresAt, ipAddress, userAgent)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*iam.Consent), args.Error(1)
}

func (m *mockIAMService) WithdrawConsent(ctx context.Context, tenantID string, userID uint64, purpose iam.ConsentPurpose, ipAddress, userAgent string) error {
	return m.Called(ctx, tenantID, userID, purpose, ipAddress, userAgent).Error(0)
}

func (m *mockIAMService) GetConsent(ctx context.Context, tenantID string, userID uint64, purpose iam.ConsentPurpose) (*iam.Consent, error) {
	args := m.Called(ctx, tenantID, userID, purpose)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*iam.Consent), args.Error(1)
}

func (m *mockIAMService) ListUserConsents(ctx context.Context, tenantID string, userID uint64) ([]*iam.Consent, error) {
	args := m.Called(ctx, tenantID, userID)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]*iam.Consent), args.Error(1)
}

func (m *mockIAMService) RequestExport(ctx context.Context, tenantID string, userID, requestedBy uint64, format iam.DataExportFormat, includeCategories, excludeCategories []string) (*iam.DataExportRequest, error) {
	args := m.Called(ctx, tenantID, userID, requestedBy, format, includeCategories, excludeCategories)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*iam.DataExportRequest), args.Error(1)
}

func (m *mockIAMService) GetExportStatus(ctx context.Context, exportID string) (*iam.DataExportRequest, error) {
	args := m.Called(ctx, exportID)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*iam.DataExportRequest), args.Error(1)
}

func (m *mockIAMService) GenerateExport(ctx context.Context, exportID string) (*iam.UserDataExport, error) {
	args := m.Called(ctx, exportID)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*iam.UserDataExport), args.Error(1)
}

func (m *mockIAMService) RequestDeletion(ctx context.Context, tenantID string, userID, requestedBy uint64, reason string, deleteCategories, preserveCategories []string, gracePeriodDays int) (*iam.DataDeletionRequest, error) {
	args := m.Called(ctx, tenantID, userID, requestedBy, reason, deleteCategories, preserveCategories, gracePeriodDays)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*iam.DataDeletionRequest), args.Error(1)
}

func (m *mockIAMService) CancelDeletion(ctx context.Context, deletionID, cancelledBy string) error {
	return m.Called(ctx, deletionID, cancelledBy).Error(0)
}

func (m *mockIAMService) ProcessDeletion(ctx context.Context, deletionID string) (*iam.DeletionSummary, error) {
	args := m.Called(ctx, deletionID)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*iam.DeletionSummary), args.Error(1)
}

func (m *mockIAMService) GetDeletionStatus(ctx context.Context, deletionID string) (*iam.DataDeletionRequest, error) {
	args := m.Called(ctx, deletionID)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*iam.DataDeletionRequest), args.Error(1)
}

// newTestIAMServer creates an IAMServer with a mock service for testing.
func newTestIAMServer(t *testing.T) (*IAMServer, *mockIAMService) {
	t.Helper()
	log, err := logger.New(logger.LogConfig{Level: "error", Format: "json", Output: "stdout"})
	require.NoError(t, err)
	svc := &mockIAMService{}
	srv := NewIAMServer(svc, log)
	return srv, svc
}

// =============================================================================
// Tenant Operations
// =============================================================================

func TestIAMServer_CreateTenant_Success(t *testing.T) {
	srv, svc := newTestIAMServer(t)
	ctx := callerContext("t1")

	svc.On("CreateTenant", ctx, mock.AnythingOfType("iam.CreateTenantOptions")).
		Return(&iam.Tenant{ID: "t1", Name: "Tenant One"}, nil)

	resp, err := srv.CreateTenant(ctx, &iampb.CreateTenantRequest{
		Id:   "t1",
		Name: "Tenant One",
		Settings: &iampb.TenantSettings{
			MaxUsers:      100,
			MfaRequired:   true,
			NamedNetworks: map[string]string{"office": "10.0.0.0/8"},
		},
	})

	require.NoError(t, err)
	assert.Equal(t, "t1", resp.Id)
	svc.AssertExpectations(t)
}

func TestIAMServer_CreateTenant_MissingID(t *testing.T) {
	srv, _ := newTestIAMServer(t)

	_, err := srv.CreateTenant(callerContext("t1"), &iampb.CreateTenantRequest{Name: "Test"})

	st, ok := status.FromError(err)
	require.True(t, ok)
	assert.Equal(t, codes.InvalidArgument, st.Code())
}

func TestIAMServer_CreateTenant_MissingName(t *testing.T) {
	srv, _ := newTestIAMServer(t)

	_, err := srv.CreateTenant(callerContext("t1"), &iampb.CreateTenantRequest{Id: "t1"})

	st, ok := status.FromError(err)
	require.True(t, ok)
	assert.Equal(t, codes.InvalidArgument, st.Code())
}

func TestIAMServer_CreateTenant_ServiceError(t *testing.T) {
	srv, svc := newTestIAMServer(t)

	svc.On("CreateTenant", mock.Anything, mock.Anything).
		Return(nil, &errors.QBError{Code: errors.ErrAlreadyExists, Op: "CreateTenant", Err: stderrors.New("exists")})

	_, err := srv.CreateTenant(callerContext("t1"), &iampb.CreateTenantRequest{Id: "t1", Name: "Test"})

	st, ok := status.FromError(err)
	require.True(t, ok)
	assert.Equal(t, codes.AlreadyExists, st.Code())
	svc.AssertExpectations(t)
}

func TestIAMServer_GetTenant_Success(t *testing.T) {
	srv, svc := newTestIAMServer(t)

	svc.On("GetTenant", mock.Anything, "t1").
		Return(&iam.Tenant{ID: "t1", Name: "Tenant One"}, nil)

	resp, err := srv.GetTenant(callerContext("t1"), &iampb.GetTenantRequest{Id: "t1"})

	require.NoError(t, err)
	assert.Equal(t, "t1", resp.Id)
	svc.AssertExpectations(t)
}

func TestIAMServer_GetTenant_MissingID(t *testing.T) {
	srv, _ := newTestIAMServer(t)

	_, err := srv.GetTenant(callerContext("t1"), &iampb.GetTenantRequest{})

	st, ok := status.FromError(err)
	require.True(t, ok)
	assert.Equal(t, codes.InvalidArgument, st.Code())
}

func TestIAMServer_UpdateTenant_Success(t *testing.T) {
	srv, svc := newTestIAMServer(t)

	svc.On("UpdateTenant", mock.Anything, mock.AnythingOfType("*iam.Tenant")).
		Return(&iam.Tenant{ID: "t1", Name: "Updated"}, nil)

	resp, err := srv.UpdateTenant(callerContext("t1"), &iampb.UpdateTenantRequest{
		Id:   "t1",
		Name: "Updated",
		Settings: &iampb.TenantSettings{
			MaxUsers:      200,
			MfaRequired:   false,
			NamedNetworks: map[string]string{"vpn": "172.16.0.0/12"},
		},
	})

	require.NoError(t, err)
	assert.Equal(t, "t1", resp.Id)
	svc.AssertExpectations(t)
}

func TestIAMServer_UpdateTenant_MissingID(t *testing.T) {
	srv, _ := newTestIAMServer(t)

	_, err := srv.UpdateTenant(callerContext("t1"), &iampb.UpdateTenantRequest{})

	st, ok := status.FromError(err)
	require.True(t, ok)
	assert.Equal(t, codes.InvalidArgument, st.Code())
}

func TestIAMServer_DeleteTenant_Success(t *testing.T) {
	srv, svc := newTestIAMServer(t)

	svc.On("DeleteTenant", mock.Anything, "t1").Return(nil)

	resp, err := srv.DeleteTenant(callerContext("t1"), &iampb.DeleteTenantRequest{Id: "t1"})

	require.NoError(t, err)
	assert.True(t, resp.Success)
	svc.AssertExpectations(t)
}

func TestIAMServer_DeleteTenant_MissingID(t *testing.T) {
	srv, _ := newTestIAMServer(t)

	_, err := srv.DeleteTenant(callerContext("t1"), &iampb.DeleteTenantRequest{})

	st, ok := status.FromError(err)
	require.True(t, ok)
	assert.Equal(t, codes.InvalidArgument, st.Code())
}

func TestIAMServer_ListTenants_Success(t *testing.T) {
	srv, svc := newTestIAMServer(t)

	svc.On("ListTenants", mock.Anything, 100, 0).
		Return([]*iam.Tenant{{ID: "t1", Name: "One"}, {ID: "t2", Name: "Two"}}, 2, nil)

	resp, err := srv.ListTenants(callerContext("t1"), &iampb.ListTenantsRequest{})

	require.NoError(t, err)
	assert.Len(t, resp.Tenants, 2)
	assert.Equal(t, int32(2), resp.Total)
	svc.AssertExpectations(t)
}

func TestIAMServer_ListTenants_ServiceError(t *testing.T) {
	srv, svc := newTestIAMServer(t)

	svc.On("ListTenants", mock.Anything, 10, 5).
		Return(nil, 0, &errors.QBError{Code: errors.ErrInternal, Op: "ListTenants", Err: stderrors.New("failed")})

	_, err := srv.ListTenants(callerContext("t1"), &iampb.ListTenantsRequest{Limit: 10, Offset: 5})

	require.Error(t, err)
	svc.AssertExpectations(t)
}

// =============================================================================
// User Operations
// =============================================================================

func TestIAMServer_CreateUser_Success(t *testing.T) {
	srv, svc := newTestIAMServer(t)

	svc.On("CreateUser", mock.Anything, mock.AnythingOfType("*iam.User")).
		Return(&iam.User{ID: 30001, TenantID: "t1", Email: "user@test.com"}, nil)

	resp, err := srv.CreateUser(callerContext("t1"), &iampb.CreateUserRequest{
		TenantId:   "t1",
		Email:      "user@test.com",
		Name:       "Test User",
		Roles:      []string{"admin"},
		Attributes: map[string]string{"dept": "eng"},
	})

	require.NoError(t, err)
	assert.Equal(t, "30001", resp.Id)
	svc.AssertExpectations(t)
}

func TestIAMServer_CreateUser_MissingTenantID(t *testing.T) {
	srv, _ := newTestIAMServer(t)

	_, err := srv.CreateUser(callerContext("t1"), &iampb.CreateUserRequest{Email: "test@test.com"})

	st, ok := status.FromError(err)
	require.True(t, ok)
	assert.Equal(t, codes.InvalidArgument, st.Code())
}

func TestIAMServer_CreateUser_MissingEmail(t *testing.T) {
	srv, _ := newTestIAMServer(t)

	_, err := srv.CreateUser(callerContext("t1"), &iampb.CreateUserRequest{TenantId: "t1"})

	st, ok := status.FromError(err)
	require.True(t, ok)
	assert.Equal(t, codes.InvalidArgument, st.Code())
}

func TestIAMServer_GetUser_Success(t *testing.T) {
	srv, svc := newTestIAMServer(t)

	svc.On("GetUser", mock.Anything, "t1", "30001").
		Return(&iam.User{ID: 30001, TenantID: "t1", Email: "user@test.com"}, nil)

	resp, err := srv.GetUser(callerContext("t1"), &iampb.GetUserRequest{TenantId: "t1", UserId: "30001"})

	require.NoError(t, err)
	assert.Equal(t, "30001", resp.Id)
	svc.AssertExpectations(t)
}

func TestIAMServer_GetUser_MissingTenantID(t *testing.T) {
	srv, _ := newTestIAMServer(t)

	_, err := srv.GetUser(callerContext("t1"), &iampb.GetUserRequest{UserId: "30001"})

	st, ok := status.FromError(err)
	require.True(t, ok)
	assert.Equal(t, codes.InvalidArgument, st.Code())
}

func TestIAMServer_GetUser_MissingUserID(t *testing.T) {
	srv, _ := newTestIAMServer(t)

	_, err := srv.GetUser(callerContext("t1"), &iampb.GetUserRequest{TenantId: "t1"})

	st, ok := status.FromError(err)
	require.True(t, ok)
	assert.Equal(t, codes.InvalidArgument, st.Code())
}

func TestIAMServer_UpdateUser_Success(t *testing.T) {
	srv, svc := newTestIAMServer(t)

	svc.On("UpdateUser", mock.Anything, mock.AnythingOfType("*iam.User")).
		Return(&iam.User{ID: 30001, TenantID: "t1", Email: "updated@test.com"}, nil)

	resp, err := srv.UpdateUser(callerContext("t1"), &iampb.UpdateUserRequest{
		TenantId:   "t1",
		UserId:     "30001",
		Email:      "updated@test.com",
		Name:       "Updated",
		Roles:      []string{"viewer"},
		Attributes: map[string]string{"level": "senior"},
	})

	require.NoError(t, err)
	assert.Equal(t, "30001", resp.Id)
	svc.AssertExpectations(t)
}

func TestIAMServer_UpdateUser_MissingTenantID(t *testing.T) {
	srv, _ := newTestIAMServer(t)

	_, err := srv.UpdateUser(callerContext("t1"), &iampb.UpdateUserRequest{UserId: "30001"})

	st, ok := status.FromError(err)
	require.True(t, ok)
	assert.Equal(t, codes.InvalidArgument, st.Code())
}

func TestIAMServer_UpdateUser_MissingUserID(t *testing.T) {
	srv, _ := newTestIAMServer(t)

	_, err := srv.UpdateUser(callerContext("t1"), &iampb.UpdateUserRequest{TenantId: "t1"})

	st, ok := status.FromError(err)
	require.True(t, ok)
	assert.Equal(t, codes.InvalidArgument, st.Code())
}

func TestIAMServer_DeleteUser_Success(t *testing.T) {
	srv, svc := newTestIAMServer(t)

	svc.On("DeleteUser", mock.Anything, "t1", "30001").Return(nil)

	resp, err := srv.DeleteUser(callerContext("t1"), &iampb.DeleteUserRequest{TenantId: "t1", UserId: "30001"})

	require.NoError(t, err)
	assert.True(t, resp.Success)
	svc.AssertExpectations(t)
}

func TestIAMServer_DeleteUser_MissingFields(t *testing.T) {
	srv, _ := newTestIAMServer(t)

	_, err := srv.DeleteUser(callerContext("t1"), &iampb.DeleteUserRequest{UserId: "30001"})
	require.Error(t, err)

	_, err = srv.DeleteUser(callerContext("t1"), &iampb.DeleteUserRequest{TenantId: "t1"})
	require.Error(t, err)
}

func TestIAMServer_ListUsers_Success(t *testing.T) {
	srv, svc := newTestIAMServer(t)

	// The filters are exactly the two the request carries: there is no group
	// filter, because quicbaas keeps no groups.
	wantFilters := map[string]string{"email": "a@test.com", "role": "admin"}
	svc.On("ListUsers", mock.Anything, "t1", 100, 0, wantFilters).
		Return([]*iam.User{{ID: 30001, TenantID: "t1", Email: "a@test.com"}}, 1, nil)

	resp, err := srv.ListUsers(callerContext("t1"), &iampb.ListUsersRequest{
		TenantId: "t1",
		Email:    "a@test.com",
		Role:     "admin",
	})

	require.NoError(t, err)
	assert.Len(t, resp.Users, 1)
	assert.Equal(t, int32(1), resp.Total)
	svc.AssertExpectations(t)
}

func TestIAMServer_ListUsers_MissingTenantID(t *testing.T) {
	srv, _ := newTestIAMServer(t)

	_, err := srv.ListUsers(callerContext("t1"), &iampb.ListUsersRequest{})

	st, ok := status.FromError(err)
	require.True(t, ok)
	assert.Equal(t, codes.InvalidArgument, st.Code())
}

// =============================================================================
// Role Operations
// =============================================================================

func TestIAMServer_CreateRole_Success(t *testing.T) {
	srv, svc := newTestIAMServer(t)

	svc.On("CreateRole", mock.Anything, mock.AnythingOfType("*iam.Role")).
		Return(&iam.Role{ID: "r1", TenantID: "t1", Name: "Admin"}, nil)

	resp, err := srv.CreateRole(callerContext("t1"), &iampb.CreateRoleRequest{
		TenantId: "t1", Id: "r1", Name: "Admin", Description: "Admin role", Permissions: []string{"read", "write"},
	})

	require.NoError(t, err)
	assert.Equal(t, "r1", resp.Id)
	svc.AssertExpectations(t)
}

func TestIAMServer_CreateRole_MissingFields(t *testing.T) {
	srv, _ := newTestIAMServer(t)
	ctx := callerContext("t1")

	_, err := srv.CreateRole(ctx, &iampb.CreateRoleRequest{Id: "r1", Name: "Admin"})
	require.Error(t, err)

	_, err = srv.CreateRole(ctx, &iampb.CreateRoleRequest{TenantId: "t1", Name: "Admin"})
	require.Error(t, err)

	_, err = srv.CreateRole(ctx, &iampb.CreateRoleRequest{TenantId: "t1", Id: "r1"})
	require.Error(t, err)
}

func TestIAMServer_GetRole_Success(t *testing.T) {
	srv, svc := newTestIAMServer(t)

	svc.On("GetRole", mock.Anything, "t1", "r1").
		Return(&iam.Role{ID: "r1", TenantID: "t1", Name: "Admin"}, nil)

	resp, err := srv.GetRole(callerContext("t1"), &iampb.GetRoleRequest{TenantId: "t1", RoleId: "r1"})

	require.NoError(t, err)
	assert.Equal(t, "r1", resp.Id)
	svc.AssertExpectations(t)
}

func TestIAMServer_GetRole_MissingFields(t *testing.T) {
	srv, _ := newTestIAMServer(t)

	_, err := srv.GetRole(callerContext("t1"), &iampb.GetRoleRequest{RoleId: "r1"})
	require.Error(t, err)

	_, err = srv.GetRole(callerContext("t1"), &iampb.GetRoleRequest{TenantId: "t1"})
	require.Error(t, err)
}

func TestIAMServer_UpdateRole_Success(t *testing.T) {
	srv, svc := newTestIAMServer(t)

	svc.On("UpdateRole", mock.Anything, mock.AnythingOfType("*iam.Role")).
		Return(&iam.Role{ID: "r1", TenantID: "t1", Name: "Updated"}, nil)

	resp, err := srv.UpdateRole(callerContext("t1"), &iampb.UpdateRoleRequest{
		TenantId: "t1", RoleId: "r1", Name: "Updated", Description: "Updated desc", Permissions: []string{"read"},
	})

	require.NoError(t, err)
	assert.Equal(t, "r1", resp.Id)
	svc.AssertExpectations(t)
}

func TestIAMServer_UpdateRole_MissingFields(t *testing.T) {
	srv, _ := newTestIAMServer(t)

	_, err := srv.UpdateRole(callerContext("t1"), &iampb.UpdateRoleRequest{RoleId: "r1"})
	require.Error(t, err)

	_, err = srv.UpdateRole(callerContext("t1"), &iampb.UpdateRoleRequest{TenantId: "t1"})
	require.Error(t, err)
}

func TestIAMServer_DeleteRole_Success(t *testing.T) {
	srv, svc := newTestIAMServer(t)

	svc.On("DeleteRole", mock.Anything, "t1", "r1").Return(nil)

	resp, err := srv.DeleteRole(callerContext("t1"), &iampb.DeleteRoleRequest{TenantId: "t1", RoleId: "r1"})

	require.NoError(t, err)
	assert.True(t, resp.Success)
	svc.AssertExpectations(t)
}

func TestIAMServer_DeleteRole_MissingFields(t *testing.T) {
	srv, _ := newTestIAMServer(t)

	_, err := srv.DeleteRole(callerContext("t1"), &iampb.DeleteRoleRequest{RoleId: "r1"})
	require.Error(t, err)

	_, err = srv.DeleteRole(callerContext("t1"), &iampb.DeleteRoleRequest{TenantId: "t1"})
	require.Error(t, err)
}

func TestIAMServer_ListRoles_Success(t *testing.T) {
	srv, svc := newTestIAMServer(t)

	svc.On("ListRoles", mock.Anything, "t1", 100, 0).
		Return([]*iam.Role{{ID: "r1", TenantID: "t1", Name: "Admin"}}, 1, nil)

	resp, err := srv.ListRoles(callerContext("t1"), &iampb.ListRolesRequest{TenantId: "t1"})

	require.NoError(t, err)
	assert.Len(t, resp.Roles, 1)
	svc.AssertExpectations(t)
}

func TestIAMServer_ListRoles_MissingTenantID(t *testing.T) {
	srv, _ := newTestIAMServer(t)

	_, err := srv.ListRoles(callerContext("t1"), &iampb.ListRolesRequest{})
	require.Error(t, err)
}

func TestIAMServer_AssignRole_Success(t *testing.T) {
	srv, svc := newTestIAMServer(t)

	svc.On("AssignRole", mock.Anything, "t1", uint64(30001), "admin").Return(nil)

	resp, err := srv.AssignRole(callerContext("t1"), &iampb.AssignRoleRequest{
		TenantId: "t1", UserId: "30001", Role: "admin",
	})

	require.NoError(t, err)
	assert.True(t, resp.Success)
	svc.AssertExpectations(t)
}

func TestIAMServer_AssignRole_MissingFields(t *testing.T) {
	srv, _ := newTestIAMServer(t)
	ctx := callerContext("t1")

	_, err := srv.AssignRole(ctx, &iampb.AssignRoleRequest{UserId: "30001", Role: "admin"})
	require.Error(t, err)

	_, err = srv.AssignRole(ctx, &iampb.AssignRoleRequest{TenantId: "t1", Role: "admin"})
	require.Error(t, err)

	_, err = srv.AssignRole(ctx, &iampb.AssignRoleRequest{TenantId: "t1", UserId: "30001"})
	require.Error(t, err)
}

func TestIAMServer_RevokeRole_Success(t *testing.T) {
	srv, svc := newTestIAMServer(t)

	svc.On("RevokeRole", mock.Anything, "t1", uint64(30001), "admin").Return(nil)

	resp, err := srv.RevokeRole(callerContext("t1"), &iampb.RevokeRoleRequest{
		TenantId: "t1", UserId: "30001", Role: "admin",
	})

	require.NoError(t, err)
	assert.True(t, resp.Success)
	svc.AssertExpectations(t)
}

func TestIAMServer_RevokeRole_MissingFields(t *testing.T) {
	srv, _ := newTestIAMServer(t)
	ctx := callerContext("t1")

	_, err := srv.RevokeRole(ctx, &iampb.RevokeRoleRequest{UserId: "30001", Role: "admin"})
	require.Error(t, err)

	_, err = srv.RevokeRole(ctx, &iampb.RevokeRoleRequest{TenantId: "t1", Role: "admin"})
	require.Error(t, err)

	_, err = srv.RevokeRole(ctx, &iampb.RevokeRoleRequest{TenantId: "t1", UserId: "30001"})
	require.Error(t, err)
}

func TestIAMServer_ReconcileRoles_Success(t *testing.T) {
	srv, svc := newTestIAMServer(t)

	svc.On("ReconcileBuiltInRoles", mock.Anything, "t1").
		Return(&iam.RoleReconcileReport{
			TenantID: "t1",
			Roles: []iam.RoleReconcileResult{
				{RoleID: "r-admin", Name: "admin", Added: []string{"roles:reconcile"}},
				{RoleID: "r-operator", Name: "operator", Created: true, Added: []string{"kv:get"}, Removed: []string{"kv:legacy"}},
			},
		}, nil)

	resp, err := srv.ReconcileRoles(callerContext("t1"), &iampb.ReconcileRolesRequest{TenantId: "t1"})

	require.NoError(t, err)
	assert.Equal(t, "t1", resp.TenantId)
	assert.True(t, resp.Changed)
	require.Len(t, resp.Roles, 2)
	assert.Equal(t, "admin", resp.Roles[0].Name)
	assert.Equal(t, []string{"roles:reconcile"}, resp.Roles[0].Added)
	assert.True(t, resp.Roles[1].Created)
	assert.Equal(t, []string{"kv:legacy"}, resp.Roles[1].Removed)
	svc.AssertExpectations(t)
}

// TestIAMServer_ReconcileRoles_NoDrift pins the idempotent answer: a tenant
// already agreeing with the build reports no roles and changed=false, which is
// a success, not an error.
func TestIAMServer_ReconcileRoles_NoDrift(t *testing.T) {
	srv, svc := newTestIAMServer(t)

	svc.On("ReconcileBuiltInRoles", mock.Anything, "t1").
		Return(&iam.RoleReconcileReport{TenantID: "t1"}, nil)

	resp, err := srv.ReconcileRoles(callerContext("t1"), &iampb.ReconcileRolesRequest{TenantId: "t1"})

	require.NoError(t, err)
	assert.Equal(t, "t1", resp.TenantId)
	assert.False(t, resp.Changed)
	assert.Empty(t, resp.Roles)
	svc.AssertExpectations(t)
}

func TestIAMServer_ReconcileRoles_MissingTenantID(t *testing.T) {
	srv, _ := newTestIAMServer(t)

	_, err := srv.ReconcileRoles(callerContext("t1"), &iampb.ReconcileRolesRequest{})

	require.Error(t, err)
	assert.Equal(t, codes.InvalidArgument, status.Code(err))
}

func TestIAMServer_ReconcileRoles_ServiceError(t *testing.T) {
	srv, svc := newTestIAMServer(t)

	svc.On("ReconcileBuiltInRoles", mock.Anything, "t1").
		Return(nil, stderrors.New("role storage cannot reconcile built-ins"))

	_, err := srv.ReconcileRoles(callerContext("t1"), &iampb.ReconcileRolesRequest{TenantId: "t1"})

	require.Error(t, err)
	svc.AssertExpectations(t)
}

// =============================================================================
// Application Operations
// =============================================================================

func TestIAMServer_RegisterApp_Success(t *testing.T) {
	srv, svc := newTestIAMServer(t)

	svc.On("RegisterApp", mock.Anything, mock.AnythingOfType("*iam.Application")).
		Return(&iam.Application{ClientID: "c1", TenantID: "t1", Name: "MyApp"}, nil)

	resp, err := srv.RegisterApp(callerContext("t1"), &iampb.RegisterAppRequest{
		TenantId: "t1", Name: "MyApp", RedirectUris: []string{"http://localhost"}, GrantTypes: []string{"authorization_code"},
	})

	require.NoError(t, err)
	assert.Equal(t, "c1", resp.ClientId)
	svc.AssertExpectations(t)
}

func TestIAMServer_RegisterApp_MissingFields(t *testing.T) {
	srv, _ := newTestIAMServer(t)

	_, err := srv.RegisterApp(callerContext("t1"), &iampb.RegisterAppRequest{Name: "Test"})
	require.Error(t, err)

	_, err = srv.RegisterApp(callerContext("t1"), &iampb.RegisterAppRequest{TenantId: "t1"})
	require.Error(t, err)
}

func TestIAMServer_GetApp_Success(t *testing.T) {
	srv, svc := newTestIAMServer(t)

	svc.On("GetApp", mock.Anything, "t1", "c1").
		Return(&iam.Application{ClientID: "c1", TenantID: "t1", Name: "MyApp", ClientSecret: "secret"}, nil)

	resp, err := srv.GetApp(callerContext("t1"), &iampb.GetAppRequest{TenantId: "t1", ClientId: "c1"})

	require.NoError(t, err)
	assert.Equal(t, "c1", resp.ClientId)
	assert.Empty(t, resp.ClientSecret, "client secret should be redacted in GET")
	svc.AssertExpectations(t)
}

func TestIAMServer_GetApp_MissingFields(t *testing.T) {
	srv, _ := newTestIAMServer(t)

	_, err := srv.GetApp(callerContext("t1"), &iampb.GetAppRequest{ClientId: "c1"})
	require.Error(t, err)

	_, err = srv.GetApp(callerContext("t1"), &iampb.GetAppRequest{TenantId: "t1"})
	require.Error(t, err)
}

func TestIAMServer_UpdateApp_Success(t *testing.T) {
	srv, svc := newTestIAMServer(t)

	svc.On("UpdateApp", mock.Anything, mock.AnythingOfType("*iam.Application")).
		Return(&iam.Application{ClientID: "c1", TenantID: "t1", Name: "Updated", ClientSecret: "secret"}, nil)

	resp, err := srv.UpdateApp(callerContext("t1"), &iampb.UpdateAppRequest{
		TenantId: "t1", ClientId: "c1", Name: "Updated", RedirectUris: []string{"http://example.com"},
	})

	require.NoError(t, err)
	assert.Empty(t, resp.ClientSecret, "client secret should be redacted in Update")
	svc.AssertExpectations(t)
}

func TestIAMServer_UpdateApp_MissingFields(t *testing.T) {
	srv, _ := newTestIAMServer(t)

	_, err := srv.UpdateApp(callerContext("t1"), &iampb.UpdateAppRequest{ClientId: "c1"})
	require.Error(t, err)

	_, err = srv.UpdateApp(callerContext("t1"), &iampb.UpdateAppRequest{TenantId: "t1"})
	require.Error(t, err)
}

func TestIAMServer_DeleteApp_Success(t *testing.T) {
	srv, svc := newTestIAMServer(t)

	svc.On("DeleteApp", mock.Anything, "t1", "c1").Return(nil)

	resp, err := srv.DeleteApp(callerContext("t1"), &iampb.DeleteAppRequest{TenantId: "t1", ClientId: "c1"})

	require.NoError(t, err)
	assert.True(t, resp.Success)
	svc.AssertExpectations(t)
}

func TestIAMServer_DeleteApp_MissingFields(t *testing.T) {
	srv, _ := newTestIAMServer(t)

	_, err := srv.DeleteApp(callerContext("t1"), &iampb.DeleteAppRequest{ClientId: "c1"})
	require.Error(t, err)

	_, err = srv.DeleteApp(callerContext("t1"), &iampb.DeleteAppRequest{TenantId: "t1"})
	require.Error(t, err)
}

func TestIAMServer_ListApps_Success(t *testing.T) {
	srv, svc := newTestIAMServer(t)

	svc.On("ListApps", mock.Anything, "t1", 100, 0).
		Return([]*iam.Application{{ClientID: "c1", TenantID: "t1", Name: "App1", ClientSecret: "secret"}}, 1, nil)

	resp, err := srv.ListApps(callerContext("t1"), &iampb.ListAppsRequest{TenantId: "t1"})

	require.NoError(t, err)
	assert.Len(t, resp.Apps, 1)
	assert.Empty(t, resp.Apps[0].ClientSecret, "client secret should be redacted in list")
	svc.AssertExpectations(t)
}

func TestIAMServer_ListApps_MissingTenantID(t *testing.T) {
	srv, _ := newTestIAMServer(t)

	_, err := srv.ListApps(callerContext("t1"), &iampb.ListAppsRequest{})
	require.Error(t, err)
}

func TestIAMServer_RotateAppSecret_Success(t *testing.T) {
	srv, svc := newTestIAMServer(t)

	svc.On("RotateAppSecret", mock.Anything, "t1", "c1").
		Return(&iam.Application{ClientID: "c1", TenantID: "t1", ClientSecret: "new-secret"}, nil)

	resp, err := srv.RotateAppSecret(callerContext("t1"), &iampb.RotateAppSecretRequest{TenantId: "t1", ClientId: "c1"})

	require.NoError(t, err)
	assert.Equal(t, "new-secret", resp.ClientSecret, "secret should be returned after rotation")
	svc.AssertExpectations(t)
}

func TestIAMServer_RotateAppSecret_MissingFields(t *testing.T) {
	srv, _ := newTestIAMServer(t)

	_, err := srv.RotateAppSecret(callerContext("t1"), &iampb.RotateAppSecretRequest{ClientId: "c1"})
	require.Error(t, err)

	_, err = srv.RotateAppSecret(callerContext("t1"), &iampb.RotateAppSecretRequest{TenantId: "t1"})
	require.Error(t, err)
}

// =============================================================================
// Policy Operations
// =============================================================================

func TestIAMServer_CreatePolicy_FullFormat(t *testing.T) {
	srv, svc := newTestIAMServer(t)

	specYAML := []byte(`apiVersion: iam.quicbaas.io/v1
kind: Policy
metadata:
  name: test-policy
spec:
  rules:
    - effect: allow
      actions: ["read"]
      resources: ["*"]
`)

	svc.On("CreatePolicy", mock.Anything, mock.AnythingOfType("*iam.Policy")).
		Return(&iam.Policy{
			APIVersion: "iam.quicbaas.io/v1",
			Kind:       "Policy",
			Metadata:   iam.PolicyMetadata{Name: "test-policy", Tenant: "t1"},
		}, nil)

	resp, err := srv.CreatePolicy(callerContext("t1"), &iampb.CreatePolicyRequest{
		TenantId:    "t1",
		Name:        "test-policy",
		Description: "A test policy",
		SpecYaml:    specYAML,
		Labels:      map[string]string{"env": "test"},
		Priority:    10,
	})

	require.NoError(t, err)
	assert.Equal(t, "test-policy", resp.Name)
	svc.AssertExpectations(t)
}

func TestIAMServer_CreatePolicy_SpecOnly(t *testing.T) {
	srv, svc := newTestIAMServer(t)

	specYAML := []byte(`rules:
  - effect: allow
    actions: ["read"]
    resources: ["*"]
`)

	svc.On("CreatePolicy", mock.Anything, mock.AnythingOfType("*iam.Policy")).
		Return(&iam.Policy{
			APIVersion: "iam.quicbaas.io/v1",
			Kind:       "Policy",
			Metadata:   iam.PolicyMetadata{Name: "test-policy", Tenant: "t1"},
		}, nil)

	resp, err := srv.CreatePolicy(callerContext("t1"), &iampb.CreatePolicyRequest{
		TenantId: "t1",
		Name:     "test-policy",
		SpecYaml: specYAML,
	})

	require.NoError(t, err)
	assert.NotNil(t, resp)
	svc.AssertExpectations(t)
}

func TestIAMServer_CreatePolicy_MissingFields(t *testing.T) {
	srv, _ := newTestIAMServer(t)
	ctx := callerContext("t1")

	_, err := srv.CreatePolicy(ctx, &iampb.CreatePolicyRequest{Name: "test", SpecYaml: []byte("rules: []")})
	require.Error(t, err)

	_, err = srv.CreatePolicy(ctx, &iampb.CreatePolicyRequest{TenantId: "t1", SpecYaml: []byte("rules: []")})
	require.Error(t, err)

	_, err = srv.CreatePolicy(ctx, &iampb.CreatePolicyRequest{TenantId: "t1", Name: "test"})
	require.Error(t, err)
}

func TestIAMServer_GetPolicy_Success(t *testing.T) {
	srv, svc := newTestIAMServer(t)

	svc.On("GetPolicy", mock.Anything, "t1", "my-policy").
		Return(&iam.Policy{Metadata: iam.PolicyMetadata{Name: "my-policy", Tenant: "t1"}}, nil)

	resp, err := srv.GetPolicy(callerContext("t1"), &iampb.GetPolicyRequest{TenantId: "t1", PolicyName: "my-policy"})

	require.NoError(t, err)
	assert.Equal(t, "my-policy", resp.Name)
	svc.AssertExpectations(t)
}

func TestIAMServer_GetPolicy_MissingFields(t *testing.T) {
	srv, _ := newTestIAMServer(t)

	_, err := srv.GetPolicy(callerContext("t1"), &iampb.GetPolicyRequest{PolicyName: "test"})
	require.Error(t, err)

	_, err = srv.GetPolicy(callerContext("t1"), &iampb.GetPolicyRequest{TenantId: "t1"})
	require.Error(t, err)
}

func TestIAMServer_UpdatePolicy_Success(t *testing.T) {
	srv, svc := newTestIAMServer(t)

	svc.On("UpdatePolicy", mock.Anything, mock.AnythingOfType("*iam.Policy")).
		Return(&iam.Policy{Metadata: iam.PolicyMetadata{Name: "my-policy", Tenant: "t1"}}, nil)

	resp, err := srv.UpdatePolicy(callerContext("t1"), &iampb.UpdatePolicyRequest{
		TenantId:    "t1",
		PolicyName:  "my-policy",
		Description: "Updated",
		Labels:      map[string]string{"env": "prod"},
		Priority:    5,
		SpecYaml:    []byte("rules: []"),
	})

	require.NoError(t, err)
	assert.NotNil(t, resp)
	svc.AssertExpectations(t)
}

func TestIAMServer_UpdatePolicy_MissingFields(t *testing.T) {
	srv, _ := newTestIAMServer(t)

	_, err := srv.UpdatePolicy(callerContext("t1"), &iampb.UpdatePolicyRequest{PolicyName: "test"})
	require.Error(t, err)

	_, err = srv.UpdatePolicy(callerContext("t1"), &iampb.UpdatePolicyRequest{TenantId: "t1"})
	require.Error(t, err)
}

func TestIAMServer_DeletePolicy_Success(t *testing.T) {
	srv, svc := newTestIAMServer(t)

	svc.On("DeletePolicy", mock.Anything, "t1", "my-policy").Return(nil)

	resp, err := srv.DeletePolicy(callerContext("t1"), &iampb.DeletePolicyRequest{TenantId: "t1", PolicyName: "my-policy"})

	require.NoError(t, err)
	assert.True(t, resp.Success)
	svc.AssertExpectations(t)
}

func TestIAMServer_DeletePolicy_MissingFields(t *testing.T) {
	srv, _ := newTestIAMServer(t)

	_, err := srv.DeletePolicy(callerContext("t1"), &iampb.DeletePolicyRequest{PolicyName: "test"})
	require.Error(t, err)

	_, err = srv.DeletePolicy(callerContext("t1"), &iampb.DeletePolicyRequest{TenantId: "t1"})
	require.Error(t, err)
}

func TestIAMServer_ListPolicies_Success(t *testing.T) {
	srv, svc := newTestIAMServer(t)

	svc.On("ListPolicies", mock.Anything, "t1", 100, 0).
		Return([]*iam.Policy{{Metadata: iam.PolicyMetadata{Name: "p1"}}}, 1, nil)

	resp, err := srv.ListPolicies(callerContext("t1"), &iampb.ListPoliciesRequest{TenantId: "t1"})

	require.NoError(t, err)
	assert.Len(t, resp.Policies, 1)
	svc.AssertExpectations(t)
}

func TestIAMServer_ListPolicies_MissingTenantID(t *testing.T) {
	srv, _ := newTestIAMServer(t)

	_, err := srv.ListPolicies(callerContext("t1"), &iampb.ListPoliciesRequest{})
	require.Error(t, err)
}

func TestIAMServer_ValidatePolicy_Success(t *testing.T) {
	srv, svc := newTestIAMServer(t)

	svc.On("ValidatePolicy", mock.Anything, "t1", "", mock.Anything).
		Return(true, []string{}, nil)

	resp, err := srv.ValidatePolicy(callerContext("t1"), &iampb.ValidatePolicyRequest{
		TenantId: "t1", SpecYaml: []byte("rules: []"),
	})

	require.NoError(t, err)
	assert.True(t, resp.Valid)
	svc.AssertExpectations(t)
}

func TestIAMServer_ValidatePolicy_MissingFields(t *testing.T) {
	srv, _ := newTestIAMServer(t)

	_, err := srv.ValidatePolicy(callerContext("t1"), &iampb.ValidatePolicyRequest{SpecYaml: []byte("x")})
	require.Error(t, err)

	_, err = srv.ValidatePolicy(callerContext("t1"), &iampb.ValidatePolicyRequest{TenantId: "t1"})
	require.Error(t, err)
}

func TestIAMServer_TestPolicy_Success(t *testing.T) {
	srv, svc := newTestIAMServer(t)

	svc.On("TestPolicy", mock.Anything, "t1", mock.AnythingOfType("*iam.CheckRequest")).
		Return(&iam.CheckResponse{Allowed: true, MatchedPolicy: "p1", MatchedRule: "r1", EvaluationTimeNs: 100}, nil)

	resp, err := srv.TestPolicy(callerContext("t1"), &iampb.TestPolicyRequest{
		TenantId: "t1",
		Subject:  &iampb.Subject{Id: "30001"},
		Action:   "read",
		Resource: "documents",
		Context:  map[string]string{"env": "prod"},
	})

	require.NoError(t, err)
	assert.True(t, resp.Allowed)
	assert.Equal(t, "p1", resp.MatchedPolicy)
	svc.AssertExpectations(t)
}

func TestIAMServer_TestPolicy_MissingFields(t *testing.T) {
	srv, _ := newTestIAMServer(t)
	ctx := callerContext("t1")

	_, err := srv.TestPolicy(ctx, &iampb.TestPolicyRequest{Subject: &iampb.Subject{}, Action: "read", Resource: "x"})
	require.Error(t, err)

	_, err = srv.TestPolicy(ctx, &iampb.TestPolicyRequest{TenantId: "t1", Action: "read", Resource: "x"})
	require.Error(t, err)

	_, err = srv.TestPolicy(ctx, &iampb.TestPolicyRequest{TenantId: "t1", Subject: &iampb.Subject{}, Resource: "x"})
	require.Error(t, err)

	_, err = srv.TestPolicy(ctx, &iampb.TestPolicyRequest{TenantId: "t1", Subject: &iampb.Subject{}, Action: "read"})
	require.Error(t, err)
}

// =============================================================================
// Delegated Administration
// =============================================================================

func TestIAMServer_CreateAdminAssignment_Success(t *testing.T) {
	srv, svc := newTestIAMServer(t)

	svc.On("CreateAdminAssignment", mock.Anything, mock.AnythingOfType("iam.CreateAdminAssignmentOptions")).
		Return(&iam.AdminAssignment{ID: "a1", UserID: 30001, AdminRole: iam.AdminRoleSuperAdmin}, nil)

	resp, err := srv.CreateAdminAssignment(callerContext("t1"), &iampb.CreateAdminAssignmentRequest{
		UserId:            "30001",
		AdminRole:         iampb.AdminRole_ADMIN_ROLE_SUPER_ADMIN,
		GrantedBy:         "admin1",
		TenantIds:         []string{"t1"},
		Permissions:       []string{"manage_users"},
		DeniedPermissions: []string{"delete_tenant"},
		ResourceScopes:    []*iampb.ResourceScope{{ResourceType: "user", ResourcePattern: "*"}},
		ExpiresAt:         timestamppb.New(time.Now().Add(24 * time.Hour)),
		Metadata:          map[string]string{"reason": "promotion"},
	})

	require.NoError(t, err)
	assert.Equal(t, "a1", resp.Id)
	svc.AssertExpectations(t)
}

func TestIAMServer_CreateAdminAssignment_MissingFields(t *testing.T) {
	srv, _ := newTestIAMServer(t)
	ctx := callerContext("t1")

	_, err := srv.CreateAdminAssignment(ctx, &iampb.CreateAdminAssignmentRequest{
		AdminRole: iampb.AdminRole_ADMIN_ROLE_SUPER_ADMIN, GrantedBy: "admin1",
	})
	require.Error(t, err)

	_, err = srv.CreateAdminAssignment(ctx, &iampb.CreateAdminAssignmentRequest{
		UserId: "30001", GrantedBy: "admin1",
	})
	require.Error(t, err)

	_, err = srv.CreateAdminAssignment(ctx, &iampb.CreateAdminAssignmentRequest{
		UserId: "30001", AdminRole: iampb.AdminRole_ADMIN_ROLE_SUPER_ADMIN,
	})
	require.Error(t, err)
}

func TestIAMServer_GetAdminAssignment_Success(t *testing.T) {
	srv, svc := newTestIAMServer(t)

	svc.On("GetAdminAssignment", mock.Anything, "a1").
		Return(&iam.AdminAssignment{ID: "a1", UserID: 30001}, nil)

	resp, err := srv.GetAdminAssignment(callerContext("t1"), &iampb.GetAdminAssignmentRequest{Id: "a1"})

	require.NoError(t, err)
	assert.Equal(t, "a1", resp.Id)
	svc.AssertExpectations(t)
}

func TestIAMServer_GetAdminAssignment_MissingID(t *testing.T) {
	srv, _ := newTestIAMServer(t)

	_, err := srv.GetAdminAssignment(callerContext("t1"), &iampb.GetAdminAssignmentRequest{})
	require.Error(t, err)
}

func TestIAMServer_UpdateAdminAssignment_Success(t *testing.T) {
	srv, svc := newTestIAMServer(t)

	svc.On("UpdateAdminAssignment", mock.Anything, "a1", mock.AnythingOfType("iam.UpdateAdminAssignmentOptions")).
		Return(&iam.AdminAssignment{ID: "a1", UserID: 30001}, nil)

	resp, err := srv.UpdateAdminAssignment(callerContext("t1"), &iampb.UpdateAdminAssignmentRequest{
		Id:                "a1",
		AdminRole:         iampb.AdminRole_ADMIN_ROLE_TENANT_ADMIN,
		TenantIds:         []string{"t1"},
		Permissions:       []string{"read"},
		DeniedPermissions: []string{"delete"},
		ResourceScopes:    []*iampb.ResourceScope{{ResourceType: "doc", ResourcePattern: "test/*"}},
		ExpiresAt:         timestamppb.New(time.Now().Add(48 * time.Hour)),
		Metadata:          map[string]string{"note": "updated"},
		Active:            true,
	})

	require.NoError(t, err)
	assert.Equal(t, "a1", resp.Id)
	svc.AssertExpectations(t)
}

func TestIAMServer_UpdateAdminAssignment_MissingID(t *testing.T) {
	srv, _ := newTestIAMServer(t)

	_, err := srv.UpdateAdminAssignment(callerContext("t1"), &iampb.UpdateAdminAssignmentRequest{})
	require.Error(t, err)
}

func TestIAMServer_DeleteAdminAssignment_Success(t *testing.T) {
	srv, svc := newTestIAMServer(t)

	svc.On("DeleteAdminAssignment", mock.Anything, "a1").Return(nil)

	resp, err := srv.DeleteAdminAssignment(callerContext("t1"), &iampb.DeleteAdminAssignmentRequest{Id: "a1"})

	require.NoError(t, err)
	assert.True(t, resp.Success)
	svc.AssertExpectations(t)
}

func TestIAMServer_DeleteAdminAssignment_MissingID(t *testing.T) {
	srv, _ := newTestIAMServer(t)

	_, err := srv.DeleteAdminAssignment(callerContext("t1"), &iampb.DeleteAdminAssignmentRequest{})
	require.Error(t, err)
}

func TestIAMServer_ListAdminAssignments_Success(t *testing.T) {
	srv, svc := newTestIAMServer(t)

	svc.On("ListAdminAssignments", mock.Anything, mock.AnythingOfType("iam.ListAdminAssignmentsOptions")).
		Return([]*iam.AdminAssignment{{ID: "a1", UserID: 30001}}, nil)

	resp, err := srv.ListAdminAssignments(callerContext("t1"), &iampb.ListAdminAssignmentsRequest{
		UserId: "30001", TenantId: "t1", ActiveOnly: true,
	})

	require.NoError(t, err)
	assert.Len(t, resp.Assignments, 1)
	svc.AssertExpectations(t)
}

func TestIAMServer_CheckAdminPermission_Success(t *testing.T) {
	srv, svc := newTestIAMServer(t)

	svc.On("CheckAdminPermission", mock.Anything, mock.AnythingOfType("*iam.AdminCheckRequest")).
		Return(&iam.AdminCheckResponse{Allowed: true, AssignmentID: "a1", AdminRole: iam.AdminRoleSuperAdmin, Reason: "granted"}, nil)

	resp, err := srv.CheckAdminPermission(callerContext("t1"), &iampb.CheckAdminPermissionRequest{
		UserId: "30001", Permission: "manage_users", TenantId: "t1", ResourceType: "user", ResourceId: "30002",
	})

	require.NoError(t, err)
	assert.True(t, resp.Allowed)
	svc.AssertExpectations(t)
}

func TestIAMServer_CheckAdminPermission_MissingFields(t *testing.T) {
	srv, _ := newTestIAMServer(t)

	_, err := srv.CheckAdminPermission(callerContext("t1"), &iampb.CheckAdminPermissionRequest{Permission: "x"})
	require.Error(t, err)

	_, err = srv.CheckAdminPermission(callerContext("t1"), &iampb.CheckAdminPermissionRequest{UserId: "30001"})
	require.Error(t, err)
}

// =============================================================================
// Impersonation Operations
// =============================================================================

func TestIAMServer_StartImpersonation_Success(t *testing.T) {
	srv, svc := newTestIAMServer(t)

	svc.On("StartImpersonation", mock.Anything, mock.AnythingOfType("*iam.StartImpersonationOptions")).
		Return(&iam.ImpersonationSession{ID: "s1", AdminUserID: 30021, TargetUserID: 30001}, nil)

	// The request carries no admin_user_id — the actor is the
	// authenticated caller, resolved server-side (go-quicbaas#383).
	resp, err := srv.StartImpersonation(callerContext("t1"), &iampb.StartImpersonationRequest{
		TargetUserId: "30001", TenantId: "t1", Reason: "support",
	})

	require.NoError(t, err)
	assert.Equal(t, "s1", resp.Id)
	svc.AssertExpectations(t)
}

func TestIAMServer_StartImpersonation_MissingFields(t *testing.T) {
	srv, _ := newTestIAMServer(t)
	ctx := callerContext("t1")

	// Missing target_user_id.
	_, err := srv.StartImpersonation(ctx, &iampb.StartImpersonationRequest{
		TenantId: "t1", Reason: "x",
	})
	require.Error(t, err)

	// Missing tenant_id.
	_, err = srv.StartImpersonation(ctx, &iampb.StartImpersonationRequest{
		TargetUserId: "30001", Reason: "x",
	})
	require.Error(t, err)

	// Missing reason.
	_, err = srv.StartImpersonation(ctx, &iampb.StartImpersonationRequest{
		TargetUserId: "30001", TenantId: "t1",
	})
	require.Error(t, err)
}

func TestIAMServer_EndImpersonation_Success(t *testing.T) {
	srv, svc := newTestIAMServer(t)

	svc.On("EndImpersonation", mock.Anything, "s1").Return(nil)

	resp, err := srv.EndImpersonation(callerContext("t1"), &iampb.EndImpersonationRequest{SessionId: "s1"})

	require.NoError(t, err)
	assert.True(t, resp.Success)
	svc.AssertExpectations(t)
}

func TestIAMServer_EndImpersonation_MissingSessionID(t *testing.T) {
	srv, _ := newTestIAMServer(t)

	_, err := srv.EndImpersonation(callerContext("t1"), &iampb.EndImpersonationRequest{})
	require.Error(t, err)
}

func TestIAMServer_GetActiveImpersonations_Success(t *testing.T) {
	srv, svc := newTestIAMServer(t)

	svc.On("GetActiveImpersonations", mock.Anything, uint64(30021)).
		Return([]*iam.ImpersonationSession{{ID: "s1", AdminUserID: 30021}}, nil)

	resp, err := srv.GetActiveImpersonations(callerContext("t1"), &iampb.GetActiveImpersonationsRequest{AdminUserId: "30021"})

	require.NoError(t, err)
	assert.Len(t, resp.Sessions, 1)
	svc.AssertExpectations(t)
}

func TestIAMServer_GetActiveImpersonations_MissingAdminUserID(t *testing.T) {
	srv, _ := newTestIAMServer(t)

	_, err := srv.GetActiveImpersonations(callerContext("t1"), &iampb.GetActiveImpersonationsRequest{})
	require.Error(t, err)
}

// =============================================================================
// GDPR Consent Operations
// =============================================================================

func TestIAMServer_GrantConsent_Success(t *testing.T) {
	srv, svc := newTestIAMServer(t)

	svc.On("GrantConsent", mock.Anything, "t1", uint64(30001), mock.Anything, "v1", mock.Anything, "1.2.3.4", "Mozilla").
		Return(&iam.Consent{ID: "c1", TenantID: "t1", UserID: 30001}, nil)

	resp, err := srv.GrantConsent(callerContext("t1"), &iampb.GrantConsentRequest{
		TenantId:  "t1",
		UserId:    "30001",
		Purpose:   iampb.ConsentPurpose_CONSENT_PURPOSE_DATA_PROCESSING,
		Version:   "v1",
		ExpiresAt: timestamppb.New(time.Now().Add(365 * 24 * time.Hour)),
		IpAddress: "1.2.3.4",
		UserAgent: "Mozilla",
	})

	require.NoError(t, err)
	assert.Equal(t, "c1", resp.Id)
	svc.AssertExpectations(t)
}

func TestIAMServer_GrantConsent_MissingFields(t *testing.T) {
	srv, _ := newTestIAMServer(t)
	ctx := callerContext("t1")

	_, err := srv.GrantConsent(ctx, &iampb.GrantConsentRequest{
		UserId: "30001", Purpose: iampb.ConsentPurpose_CONSENT_PURPOSE_DATA_PROCESSING,
	})
	require.Error(t, err)

	_, err = srv.GrantConsent(ctx, &iampb.GrantConsentRequest{
		TenantId: "t1", Purpose: iampb.ConsentPurpose_CONSENT_PURPOSE_DATA_PROCESSING,
	})
	require.Error(t, err)

	_, err = srv.GrantConsent(ctx, &iampb.GrantConsentRequest{
		TenantId: "t1", UserId: "30001",
	})
	require.Error(t, err)
}

func TestIAMServer_WithdrawConsent_Success(t *testing.T) {
	srv, svc := newTestIAMServer(t)

	svc.On("WithdrawConsent", mock.Anything, "t1", uint64(30001), mock.Anything, "1.2.3.4", "Mozilla").Return(nil)

	resp, err := srv.WithdrawConsent(callerContext("t1"), &iampb.WithdrawConsentRequest{
		TenantId:  "t1",
		UserId:    "30001",
		Purpose:   iampb.ConsentPurpose_CONSENT_PURPOSE_DATA_PROCESSING,
		IpAddress: "1.2.3.4",
		UserAgent: "Mozilla",
	})

	require.NoError(t, err)
	assert.True(t, resp.Success)
	svc.AssertExpectations(t)
}

func TestIAMServer_WithdrawConsent_MissingFields(t *testing.T) {
	srv, _ := newTestIAMServer(t)
	ctx := callerContext("t1")

	_, err := srv.WithdrawConsent(ctx, &iampb.WithdrawConsentRequest{UserId: "30001", Purpose: iampb.ConsentPurpose_CONSENT_PURPOSE_DATA_PROCESSING})
	require.Error(t, err)

	_, err = srv.WithdrawConsent(ctx, &iampb.WithdrawConsentRequest{TenantId: "t1", Purpose: iampb.ConsentPurpose_CONSENT_PURPOSE_DATA_PROCESSING})
	require.Error(t, err)

	_, err = srv.WithdrawConsent(ctx, &iampb.WithdrawConsentRequest{TenantId: "t1", UserId: "30001"})
	require.Error(t, err)
}

func TestIAMServer_GetConsent_Success(t *testing.T) {
	srv, svc := newTestIAMServer(t)

	svc.On("GetConsent", mock.Anything, "t1", uint64(30001), mock.Anything).
		Return(&iam.Consent{ID: "c1", TenantID: "t1", UserID: 30001}, nil)

	resp, err := srv.GetConsent(callerContext("t1"), &iampb.GetConsentRequest{
		TenantId: "t1", UserId: "30001", Purpose: iampb.ConsentPurpose_CONSENT_PURPOSE_DATA_PROCESSING,
	})

	require.NoError(t, err)
	assert.Equal(t, "c1", resp.Id)
	svc.AssertExpectations(t)
}

func TestIAMServer_GetConsent_MissingFields(t *testing.T) {
	srv, _ := newTestIAMServer(t)
	ctx := callerContext("t1")

	_, err := srv.GetConsent(ctx, &iampb.GetConsentRequest{UserId: "30001", Purpose: iampb.ConsentPurpose_CONSENT_PURPOSE_DATA_PROCESSING})
	require.Error(t, err)

	_, err = srv.GetConsent(ctx, &iampb.GetConsentRequest{TenantId: "t1", Purpose: iampb.ConsentPurpose_CONSENT_PURPOSE_DATA_PROCESSING})
	require.Error(t, err)

	_, err = srv.GetConsent(ctx, &iampb.GetConsentRequest{TenantId: "t1", UserId: "30001"})
	require.Error(t, err)
}

func TestIAMServer_ListUserConsents_Success(t *testing.T) {
	srv, svc := newTestIAMServer(t)

	svc.On("ListUserConsents", mock.Anything, "t1", uint64(30001)).
		Return([]*iam.Consent{{ID: "c1", TenantID: "t1", UserID: 30001}}, nil)

	resp, err := srv.ListUserConsents(callerContext("t1"), &iampb.ListUserConsentsRequest{TenantId: "t1", UserId: "30001"})

	require.NoError(t, err)
	assert.Len(t, resp.Consents, 1)
	svc.AssertExpectations(t)
}

func TestIAMServer_ListUserConsents_MissingFields(t *testing.T) {
	srv, _ := newTestIAMServer(t)

	_, err := srv.ListUserConsents(callerContext("t1"), &iampb.ListUserConsentsRequest{UserId: "30001"})
	require.Error(t, err)

	_, err = srv.ListUserConsents(callerContext("t1"), &iampb.ListUserConsentsRequest{TenantId: "t1"})
	require.Error(t, err)
}

// =============================================================================
// GDPR Data Export Operations
// =============================================================================

func TestIAMServer_RequestExport_Success(t *testing.T) {
	srv, svc := newTestIAMServer(t)

	svc.On("RequestExport", mock.Anything, "t1", uint64(30001), uint64(30021), mock.Anything, mock.Anything, mock.Anything).
		Return(&iam.DataExportRequest{ID: "e1", TenantID: "t1", UserID: 30001}, nil)

	resp, err := srv.RequestExport(callerContext("t1"), &iampb.RequestExportRequest{
		TenantId:          "t1",
		UserId:            "30001",
		RequestedBy:       "30021",
		IncludeCategories: []string{"profile"},
		ExcludeCategories: []string{"audit"},
	})

	require.NoError(t, err)
	assert.Equal(t, "e1", resp.Id)
	svc.AssertExpectations(t)
}

func TestIAMServer_RequestExport_MissingFields(t *testing.T) {
	srv, _ := newTestIAMServer(t)
	ctx := callerContext("t1")

	_, err := srv.RequestExport(ctx, &iampb.RequestExportRequest{UserId: "30001", RequestedBy: "a"})
	require.Error(t, err)

	_, err = srv.RequestExport(ctx, &iampb.RequestExportRequest{TenantId: "t1", RequestedBy: "a"})
	require.Error(t, err)

	_, err = srv.RequestExport(ctx, &iampb.RequestExportRequest{TenantId: "t1", UserId: "30001"})
	require.Error(t, err)
}

func TestIAMServer_GetExportStatus_Success(t *testing.T) {
	srv, svc := newTestIAMServer(t)

	svc.On("GetExportStatus", mock.Anything, "e1").
		Return(&iam.DataExportRequest{ID: "e1"}, nil)

	resp, err := srv.GetExportStatus(callerContext("t1"), &iampb.GetExportStatusRequest{ExportId: "e1"})

	require.NoError(t, err)
	assert.Equal(t, "e1", resp.Id)
	svc.AssertExpectations(t)
}

func TestIAMServer_GetExportStatus_MissingID(t *testing.T) {
	srv, _ := newTestIAMServer(t)

	_, err := srv.GetExportStatus(callerContext("t1"), &iampb.GetExportStatusRequest{})
	require.Error(t, err)
}

func TestIAMServer_GenerateExport_Success(t *testing.T) {
	srv, svc := newTestIAMServer(t)

	svc.On("GenerateExport", mock.Anything, "e1").
		Return(&iam.UserDataExport{ExportMetadata: iam.ExportMetadata{ExportID: "e1"}}, nil)

	resp, err := srv.GenerateExport(callerContext("t1"), &iampb.GenerateExportRequest{ExportId: "e1"})

	require.NoError(t, err)
	assert.Equal(t, "e1", resp.ExportMetadata.ExportId)
	svc.AssertExpectations(t)
}

func TestIAMServer_GenerateExport_MissingID(t *testing.T) {
	srv, _ := newTestIAMServer(t)

	_, err := srv.GenerateExport(callerContext("t1"), &iampb.GenerateExportRequest{})
	require.Error(t, err)
}

// =============================================================================
// GDPR Data Deletion Operations
// =============================================================================

func TestIAMServer_RequestDeletion_Success(t *testing.T) {
	srv, svc := newTestIAMServer(t)

	svc.On("RequestDeletion", mock.Anything, "t1", uint64(30001), uint64(30021), "gdpr", mock.Anything, mock.Anything, 30).
		Return(&iam.DataDeletionRequest{ID: "d1", TenantID: "t1", UserID: 30001}, nil)

	resp, err := srv.RequestDeletion(callerContext("t1"), &iampb.RequestDeletionRequest{
		TenantId:           "t1",
		UserId:             "30001",
		RequestedBy:        "30021",
		Reason:             "gdpr",
		DeleteCategories:   []string{"profile"},
		PreserveCategories: []string{"audit"},
		GracePeriodDays:    30,
	})

	require.NoError(t, err)
	assert.Equal(t, "d1", resp.Id)
	svc.AssertExpectations(t)
}

func TestIAMServer_RequestDeletion_MissingFields(t *testing.T) {
	srv, _ := newTestIAMServer(t)
	ctx := callerContext("t1")

	_, err := srv.RequestDeletion(ctx, &iampb.RequestDeletionRequest{UserId: "30001", RequestedBy: "a"})
	require.Error(t, err)

	_, err = srv.RequestDeletion(ctx, &iampb.RequestDeletionRequest{TenantId: "t1", RequestedBy: "a"})
	require.Error(t, err)

	_, err = srv.RequestDeletion(ctx, &iampb.RequestDeletionRequest{TenantId: "t1", UserId: "30001"})
	require.Error(t, err)
}

func TestIAMServer_CancelDeletion_Success(t *testing.T) {
	srv, svc := newTestIAMServer(t)

	svc.On("CancelDeletion", mock.Anything, "d1", "admin1").Return(nil)

	resp, err := srv.CancelDeletion(callerContext("t1"), &iampb.CancelDeletionRequest{DeletionId: "d1", CancelledBy: "admin1"})

	require.NoError(t, err)
	assert.True(t, resp.Success)
	svc.AssertExpectations(t)
}

func TestIAMServer_CancelDeletion_MissingFields(t *testing.T) {
	srv, _ := newTestIAMServer(t)

	_, err := srv.CancelDeletion(callerContext("t1"), &iampb.CancelDeletionRequest{CancelledBy: "admin1"})
	require.Error(t, err)

	_, err = srv.CancelDeletion(callerContext("t1"), &iampb.CancelDeletionRequest{DeletionId: "d1"})
	require.Error(t, err)
}

func TestIAMServer_ProcessDeletion_Success(t *testing.T) {
	srv, svc := newTestIAMServer(t)

	svc.On("ProcessDeletion", mock.Anything, "d1").
		Return(&iam.DeletionSummary{ProfileDeleted: true, SessionsDeleted: 5}, nil)

	resp, err := srv.ProcessDeletion(callerContext("t1"), &iampb.ProcessDeletionRequest{DeletionId: "d1"})

	require.NoError(t, err)
	assert.True(t, resp.ProfileDeleted)
	assert.Equal(t, int32(5), resp.SessionsDeleted)
	svc.AssertExpectations(t)
}

func TestIAMServer_ProcessDeletion_MissingID(t *testing.T) {
	srv, _ := newTestIAMServer(t)

	_, err := srv.ProcessDeletion(callerContext("t1"), &iampb.ProcessDeletionRequest{})
	require.Error(t, err)
}

func TestIAMServer_GetDeletionStatus_Success(t *testing.T) {
	srv, svc := newTestIAMServer(t)

	svc.On("GetDeletionStatus", mock.Anything, "d1").
		Return(&iam.DataDeletionRequest{ID: "d1"}, nil)

	resp, err := srv.GetDeletionStatus(callerContext("t1"), &iampb.GetDeletionStatusRequest{DeletionId: "d1"})

	require.NoError(t, err)
	assert.Equal(t, "d1", resp.Id)
	svc.AssertExpectations(t)
}

func TestIAMServer_GetDeletionStatus_MissingID(t *testing.T) {
	srv, _ := newTestIAMServer(t)

	_, err := srv.GetDeletionStatus(callerContext("t1"), &iampb.GetDeletionStatusRequest{})
	require.Error(t, err)
}

// =============================================================================
// NewIAMServer Tests
// =============================================================================

func TestNewIAMServer_Success(t *testing.T) {
	log, err := logger.New(logger.LogConfig{Level: "error", Format: "json", Output: "stdout"})
	require.NoError(t, err)

	svc := &mockIAMService{}
	srv := NewIAMServer(svc, log)

	assert.NotNil(t, srv)
	assert.NotNil(t, srv.iamService)
	assert.NotNil(t, srv.logger)
}
