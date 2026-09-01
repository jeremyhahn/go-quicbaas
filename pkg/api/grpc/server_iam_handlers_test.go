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

func (m *mockIAMService) CreateUser(ctx context.Context, user *iam.User) (*iam.User, error) {
	args := m.Called(ctx, user)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*iam.User), args.Error(1)
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

func (m *mockIAMService) CreateGroup(ctx context.Context, group *iam.Group) (*iam.Group, error) {
	args := m.Called(ctx, group)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*iam.Group), args.Error(1)
}

func (m *mockIAMService) GetGroup(ctx context.Context, tenantID, groupID string) (*iam.Group, error) {
	args := m.Called(ctx, tenantID, groupID)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*iam.Group), args.Error(1)
}

func (m *mockIAMService) UpdateGroup(ctx context.Context, group *iam.Group) (*iam.Group, error) {
	args := m.Called(ctx, group)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*iam.Group), args.Error(1)
}

func (m *mockIAMService) DeleteGroup(ctx context.Context, tenantID, groupID string) error {
	return m.Called(ctx, tenantID, groupID).Error(0)
}

func (m *mockIAMService) ListGroups(ctx context.Context, tenantID string, limit, offset int) ([]*iam.Group, int, error) {
	args := m.Called(ctx, tenantID, limit, offset)
	if args.Get(0) == nil {
		return nil, args.Int(1), args.Error(2)
	}
	return args.Get(0).([]*iam.Group), args.Int(1), args.Error(2)
}

func (m *mockIAMService) AddUserToGroup(ctx context.Context, tenantID, groupID, userID string) error {
	return m.Called(ctx, tenantID, groupID, userID).Error(0)
}

func (m *mockIAMService) RemoveUserFromGroup(ctx context.Context, tenantID, groupID, userID string) error {
	return m.Called(ctx, tenantID, groupID, userID).Error(0)
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

func (m *mockIAMService) AssignRole(ctx context.Context, tenantID, userID, role string) error {
	return m.Called(ctx, tenantID, userID, role).Error(0)
}

func (m *mockIAMService) RevokeRole(ctx context.Context, tenantID, userID, role string) error {
	return m.Called(ctx, tenantID, userID, role).Error(0)
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

func (m *mockIAMService) ValidatePolicy(ctx context.Context, tenantID string, specYAML []byte) (bool, []string, error) {
	args := m.Called(ctx, tenantID, specYAML)
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

func (m *mockIAMService) GetActiveImpersonations(ctx context.Context, adminUserID string) ([]*iam.ImpersonationSession, error) {
	args := m.Called(ctx, adminUserID)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]*iam.ImpersonationSession), args.Error(1)
}

func (m *mockIAMService) GrantConsent(ctx context.Context, tenantID, userID string, purpose iam.ConsentPurpose, version string, expiresAt *time.Time, ipAddress, userAgent string) (*iam.Consent, error) {
	args := m.Called(ctx, tenantID, userID, purpose, version, expiresAt, ipAddress, userAgent)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*iam.Consent), args.Error(1)
}

func (m *mockIAMService) WithdrawConsent(ctx context.Context, tenantID, userID string, purpose iam.ConsentPurpose, ipAddress, userAgent string) error {
	return m.Called(ctx, tenantID, userID, purpose, ipAddress, userAgent).Error(0)
}

func (m *mockIAMService) GetConsent(ctx context.Context, tenantID, userID string, purpose iam.ConsentPurpose) (*iam.Consent, error) {
	args := m.Called(ctx, tenantID, userID, purpose)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*iam.Consent), args.Error(1)
}

func (m *mockIAMService) ListUserConsents(ctx context.Context, tenantID, userID string) ([]*iam.Consent, error) {
	args := m.Called(ctx, tenantID, userID)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]*iam.Consent), args.Error(1)
}

func (m *mockIAMService) RequestExport(ctx context.Context, tenantID, userID, requestedBy string, format iam.DataExportFormat, includeCategories, excludeCategories []string) (*iam.DataExportRequest, error) {
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

func (m *mockIAMService) RequestDeletion(ctx context.Context, tenantID, userID, requestedBy, reason string, deleteCategories, preserveCategories []string, gracePeriodDays int) (*iam.DataDeletionRequest, error) {
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
	ctx := context.Background()

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

	_, err := srv.CreateTenant(context.Background(), &iampb.CreateTenantRequest{Name: "Test"})

	st, ok := status.FromError(err)
	require.True(t, ok)
	assert.Equal(t, codes.InvalidArgument, st.Code())
}

func TestIAMServer_CreateTenant_MissingName(t *testing.T) {
	srv, _ := newTestIAMServer(t)

	_, err := srv.CreateTenant(context.Background(), &iampb.CreateTenantRequest{Id: "t1"})

	st, ok := status.FromError(err)
	require.True(t, ok)
	assert.Equal(t, codes.InvalidArgument, st.Code())
}

func TestIAMServer_CreateTenant_ServiceError(t *testing.T) {
	srv, svc := newTestIAMServer(t)

	svc.On("CreateTenant", mock.Anything, mock.Anything).
		Return(nil, &errors.QBError{Code: errors.ErrAlreadyExists, Op: "CreateTenant", Err: stderrors.New("exists")})

	_, err := srv.CreateTenant(context.Background(), &iampb.CreateTenantRequest{Id: "t1", Name: "Test"})

	st, ok := status.FromError(err)
	require.True(t, ok)
	assert.Equal(t, codes.AlreadyExists, st.Code())
	svc.AssertExpectations(t)
}

func TestIAMServer_GetTenant_Success(t *testing.T) {
	srv, svc := newTestIAMServer(t)

	svc.On("GetTenant", mock.Anything, "t1").
		Return(&iam.Tenant{ID: "t1", Name: "Tenant One"}, nil)

	resp, err := srv.GetTenant(context.Background(), &iampb.GetTenantRequest{Id: "t1"})

	require.NoError(t, err)
	assert.Equal(t, "t1", resp.Id)
	svc.AssertExpectations(t)
}

func TestIAMServer_GetTenant_MissingID(t *testing.T) {
	srv, _ := newTestIAMServer(t)

	_, err := srv.GetTenant(context.Background(), &iampb.GetTenantRequest{})

	st, ok := status.FromError(err)
	require.True(t, ok)
	assert.Equal(t, codes.InvalidArgument, st.Code())
}

func TestIAMServer_UpdateTenant_Success(t *testing.T) {
	srv, svc := newTestIAMServer(t)

	svc.On("UpdateTenant", mock.Anything, mock.AnythingOfType("*iam.Tenant")).
		Return(&iam.Tenant{ID: "t1", Name: "Updated"}, nil)

	resp, err := srv.UpdateTenant(context.Background(), &iampb.UpdateTenantRequest{
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

	_, err := srv.UpdateTenant(context.Background(), &iampb.UpdateTenantRequest{})

	st, ok := status.FromError(err)
	require.True(t, ok)
	assert.Equal(t, codes.InvalidArgument, st.Code())
}

func TestIAMServer_DeleteTenant_Success(t *testing.T) {
	srv, svc := newTestIAMServer(t)

	svc.On("DeleteTenant", mock.Anything, "t1").Return(nil)

	resp, err := srv.DeleteTenant(context.Background(), &iampb.DeleteTenantRequest{Id: "t1"})

	require.NoError(t, err)
	assert.True(t, resp.Success)
	svc.AssertExpectations(t)
}

func TestIAMServer_DeleteTenant_MissingID(t *testing.T) {
	srv, _ := newTestIAMServer(t)

	_, err := srv.DeleteTenant(context.Background(), &iampb.DeleteTenantRequest{})

	st, ok := status.FromError(err)
	require.True(t, ok)
	assert.Equal(t, codes.InvalidArgument, st.Code())
}

func TestIAMServer_ListTenants_Success(t *testing.T) {
	srv, svc := newTestIAMServer(t)

	svc.On("ListTenants", mock.Anything, 100, 0).
		Return([]*iam.Tenant{{ID: "t1", Name: "One"}, {ID: "t2", Name: "Two"}}, 2, nil)

	resp, err := srv.ListTenants(context.Background(), &iampb.ListTenantsRequest{})

	require.NoError(t, err)
	assert.Len(t, resp.Tenants, 2)
	assert.Equal(t, int32(2), resp.Total)
	svc.AssertExpectations(t)
}

func TestIAMServer_ListTenants_ServiceError(t *testing.T) {
	srv, svc := newTestIAMServer(t)

	svc.On("ListTenants", mock.Anything, 10, 5).
		Return(nil, 0, &errors.QBError{Code: errors.ErrInternal, Op: "ListTenants", Err: stderrors.New("failed")})

	_, err := srv.ListTenants(context.Background(), &iampb.ListTenantsRequest{Limit: 10, Offset: 5})

	require.Error(t, err)
	svc.AssertExpectations(t)
}

// =============================================================================
// User Operations
// =============================================================================

func TestIAMServer_CreateUser_Success(t *testing.T) {
	srv, svc := newTestIAMServer(t)

	svc.On("CreateUser", mock.Anything, mock.AnythingOfType("*iam.User")).
		Return(&iam.User{ID: "u1", TenantID: "t1", Email: "user@test.com"}, nil)

	resp, err := srv.CreateUser(context.Background(), &iampb.CreateUserRequest{
		TenantId:   "t1",
		Email:      "user@test.com",
		Name:       "Test User",
		Roles:      []string{"admin"},
		Groups:     []string{"devs"},
		Attributes: map[string]string{"dept": "eng"},
	})

	require.NoError(t, err)
	assert.Equal(t, "u1", resp.Id)
	svc.AssertExpectations(t)
}

func TestIAMServer_CreateUser_MissingTenantID(t *testing.T) {
	srv, _ := newTestIAMServer(t)

	_, err := srv.CreateUser(context.Background(), &iampb.CreateUserRequest{Email: "test@test.com"})

	st, ok := status.FromError(err)
	require.True(t, ok)
	assert.Equal(t, codes.InvalidArgument, st.Code())
}

func TestIAMServer_CreateUser_MissingEmail(t *testing.T) {
	srv, _ := newTestIAMServer(t)

	_, err := srv.CreateUser(context.Background(), &iampb.CreateUserRequest{TenantId: "t1"})

	st, ok := status.FromError(err)
	require.True(t, ok)
	assert.Equal(t, codes.InvalidArgument, st.Code())
}

func TestIAMServer_GetUser_Success(t *testing.T) {
	srv, svc := newTestIAMServer(t)

	svc.On("GetUser", mock.Anything, "t1", "u1").
		Return(&iam.User{ID: "u1", TenantID: "t1", Email: "user@test.com"}, nil)

	resp, err := srv.GetUser(context.Background(), &iampb.GetUserRequest{TenantId: "t1", UserId: "u1"})

	require.NoError(t, err)
	assert.Equal(t, "u1", resp.Id)
	svc.AssertExpectations(t)
}

func TestIAMServer_GetUser_MissingTenantID(t *testing.T) {
	srv, _ := newTestIAMServer(t)

	_, err := srv.GetUser(context.Background(), &iampb.GetUserRequest{UserId: "u1"})

	st, ok := status.FromError(err)
	require.True(t, ok)
	assert.Equal(t, codes.InvalidArgument, st.Code())
}

func TestIAMServer_GetUser_MissingUserID(t *testing.T) {
	srv, _ := newTestIAMServer(t)

	_, err := srv.GetUser(context.Background(), &iampb.GetUserRequest{TenantId: "t1"})

	st, ok := status.FromError(err)
	require.True(t, ok)
	assert.Equal(t, codes.InvalidArgument, st.Code())
}

func TestIAMServer_UpdateUser_Success(t *testing.T) {
	srv, svc := newTestIAMServer(t)

	svc.On("UpdateUser", mock.Anything, mock.AnythingOfType("*iam.User")).
		Return(&iam.User{ID: "u1", TenantID: "t1", Email: "updated@test.com"}, nil)

	resp, err := srv.UpdateUser(context.Background(), &iampb.UpdateUserRequest{
		TenantId:   "t1",
		UserId:     "u1",
		Email:      "updated@test.com",
		Name:       "Updated",
		Roles:      []string{"viewer"},
		Groups:     []string{"ops"},
		Attributes: map[string]string{"level": "senior"},
	})

	require.NoError(t, err)
	assert.Equal(t, "u1", resp.Id)
	svc.AssertExpectations(t)
}

func TestIAMServer_UpdateUser_MissingTenantID(t *testing.T) {
	srv, _ := newTestIAMServer(t)

	_, err := srv.UpdateUser(context.Background(), &iampb.UpdateUserRequest{UserId: "u1"})

	st, ok := status.FromError(err)
	require.True(t, ok)
	assert.Equal(t, codes.InvalidArgument, st.Code())
}

func TestIAMServer_UpdateUser_MissingUserID(t *testing.T) {
	srv, _ := newTestIAMServer(t)

	_, err := srv.UpdateUser(context.Background(), &iampb.UpdateUserRequest{TenantId: "t1"})

	st, ok := status.FromError(err)
	require.True(t, ok)
	assert.Equal(t, codes.InvalidArgument, st.Code())
}

func TestIAMServer_DeleteUser_Success(t *testing.T) {
	srv, svc := newTestIAMServer(t)

	svc.On("DeleteUser", mock.Anything, "t1", "u1").Return(nil)

	resp, err := srv.DeleteUser(context.Background(), &iampb.DeleteUserRequest{TenantId: "t1", UserId: "u1"})

	require.NoError(t, err)
	assert.True(t, resp.Success)
	svc.AssertExpectations(t)
}

func TestIAMServer_DeleteUser_MissingFields(t *testing.T) {
	srv, _ := newTestIAMServer(t)

	_, err := srv.DeleteUser(context.Background(), &iampb.DeleteUserRequest{UserId: "u1"})
	require.Error(t, err)

	_, err = srv.DeleteUser(context.Background(), &iampb.DeleteUserRequest{TenantId: "t1"})
	require.Error(t, err)
}

func TestIAMServer_ListUsers_Success(t *testing.T) {
	srv, svc := newTestIAMServer(t)

	svc.On("ListUsers", mock.Anything, "t1", 100, 0, mock.AnythingOfType("map[string]string")).
		Return([]*iam.User{{ID: "u1", TenantID: "t1", Email: "a@test.com"}}, 1, nil)

	resp, err := srv.ListUsers(context.Background(), &iampb.ListUsersRequest{
		TenantId: "t1",
		Email:    "a@test.com",
		Role:     "admin",
		Group:    "devs",
	})

	require.NoError(t, err)
	assert.Len(t, resp.Users, 1)
	assert.Equal(t, int32(1), resp.Total)
	svc.AssertExpectations(t)
}

func TestIAMServer_ListUsers_MissingTenantID(t *testing.T) {
	srv, _ := newTestIAMServer(t)

	_, err := srv.ListUsers(context.Background(), &iampb.ListUsersRequest{})

	st, ok := status.FromError(err)
	require.True(t, ok)
	assert.Equal(t, codes.InvalidArgument, st.Code())
}

// =============================================================================
// Group Operations
// =============================================================================

func TestIAMServer_CreateGroup_Success(t *testing.T) {
	srv, svc := newTestIAMServer(t)

	svc.On("CreateGroup", mock.Anything, mock.AnythingOfType("*iam.Group")).
		Return(&iam.Group{ID: "g1", TenantID: "t1", Name: "Devs"}, nil)

	resp, err := srv.CreateGroup(context.Background(), &iampb.CreateGroupRequest{
		TenantId:    "t1",
		Id:          "g1",
		Name:        "Devs",
		Description: "Development team",
	})

	require.NoError(t, err)
	assert.Equal(t, "g1", resp.Id)
	svc.AssertExpectations(t)
}

func TestIAMServer_CreateGroup_MissingFields(t *testing.T) {
	srv, _ := newTestIAMServer(t)
	ctx := context.Background()

	_, err := srv.CreateGroup(ctx, &iampb.CreateGroupRequest{Id: "g1", Name: "Test"})
	require.Error(t, err)

	_, err = srv.CreateGroup(ctx, &iampb.CreateGroupRequest{TenantId: "t1", Name: "Test"})
	require.Error(t, err)

	_, err = srv.CreateGroup(ctx, &iampb.CreateGroupRequest{TenantId: "t1", Id: "g1"})
	require.Error(t, err)
}

func TestIAMServer_GetGroup_Success(t *testing.T) {
	srv, svc := newTestIAMServer(t)

	svc.On("GetGroup", mock.Anything, "t1", "g1").
		Return(&iam.Group{ID: "g1", TenantID: "t1", Name: "Devs"}, nil)

	resp, err := srv.GetGroup(context.Background(), &iampb.GetGroupRequest{TenantId: "t1", GroupId: "g1"})

	require.NoError(t, err)
	assert.Equal(t, "g1", resp.Id)
	svc.AssertExpectations(t)
}

func TestIAMServer_GetGroup_MissingFields(t *testing.T) {
	srv, _ := newTestIAMServer(t)

	_, err := srv.GetGroup(context.Background(), &iampb.GetGroupRequest{GroupId: "g1"})
	require.Error(t, err)

	_, err = srv.GetGroup(context.Background(), &iampb.GetGroupRequest{TenantId: "t1"})
	require.Error(t, err)
}

func TestIAMServer_UpdateGroup_Success(t *testing.T) {
	srv, svc := newTestIAMServer(t)

	svc.On("UpdateGroup", mock.Anything, mock.AnythingOfType("*iam.Group")).
		Return(&iam.Group{ID: "g1", TenantID: "t1", Name: "Updated"}, nil)

	resp, err := srv.UpdateGroup(context.Background(), &iampb.UpdateGroupRequest{
		TenantId: "t1", GroupId: "g1", Name: "Updated", Description: "Updated desc",
	})

	require.NoError(t, err)
	assert.Equal(t, "g1", resp.Id)
	svc.AssertExpectations(t)
}

func TestIAMServer_UpdateGroup_MissingFields(t *testing.T) {
	srv, _ := newTestIAMServer(t)

	_, err := srv.UpdateGroup(context.Background(), &iampb.UpdateGroupRequest{GroupId: "g1"})
	require.Error(t, err)

	_, err = srv.UpdateGroup(context.Background(), &iampb.UpdateGroupRequest{TenantId: "t1"})
	require.Error(t, err)
}

func TestIAMServer_DeleteGroup_Success(t *testing.T) {
	srv, svc := newTestIAMServer(t)

	svc.On("DeleteGroup", mock.Anything, "t1", "g1").Return(nil)

	resp, err := srv.DeleteGroup(context.Background(), &iampb.DeleteGroupRequest{TenantId: "t1", GroupId: "g1"})

	require.NoError(t, err)
	assert.True(t, resp.Success)
	svc.AssertExpectations(t)
}

func TestIAMServer_DeleteGroup_MissingFields(t *testing.T) {
	srv, _ := newTestIAMServer(t)

	_, err := srv.DeleteGroup(context.Background(), &iampb.DeleteGroupRequest{GroupId: "g1"})
	require.Error(t, err)

	_, err = srv.DeleteGroup(context.Background(), &iampb.DeleteGroupRequest{TenantId: "t1"})
	require.Error(t, err)
}

func TestIAMServer_ListGroups_Success(t *testing.T) {
	srv, svc := newTestIAMServer(t)

	svc.On("ListGroups", mock.Anything, "t1", 100, 0).
		Return([]*iam.Group{{ID: "g1", TenantID: "t1", Name: "Devs"}}, 1, nil)

	resp, err := srv.ListGroups(context.Background(), &iampb.ListGroupsRequest{TenantId: "t1"})

	require.NoError(t, err)
	assert.Len(t, resp.Groups, 1)
	svc.AssertExpectations(t)
}

func TestIAMServer_ListGroups_MissingTenantID(t *testing.T) {
	srv, _ := newTestIAMServer(t)

	_, err := srv.ListGroups(context.Background(), &iampb.ListGroupsRequest{})

	require.Error(t, err)
}

func TestIAMServer_AddUserToGroup_Success(t *testing.T) {
	srv, svc := newTestIAMServer(t)

	svc.On("AddUserToGroup", mock.Anything, "t1", "g1", "u1").Return(nil)

	resp, err := srv.AddUserToGroup(context.Background(), &iampb.AddUserToGroupRequest{
		TenantId: "t1", GroupId: "g1", UserId: "u1",
	})

	require.NoError(t, err)
	assert.True(t, resp.Success)
	svc.AssertExpectations(t)
}

func TestIAMServer_AddUserToGroup_MissingFields(t *testing.T) {
	srv, _ := newTestIAMServer(t)
	ctx := context.Background()

	_, err := srv.AddUserToGroup(ctx, &iampb.AddUserToGroupRequest{GroupId: "g1", UserId: "u1"})
	require.Error(t, err)

	_, err = srv.AddUserToGroup(ctx, &iampb.AddUserToGroupRequest{TenantId: "t1", UserId: "u1"})
	require.Error(t, err)

	_, err = srv.AddUserToGroup(ctx, &iampb.AddUserToGroupRequest{TenantId: "t1", GroupId: "g1"})
	require.Error(t, err)
}

func TestIAMServer_RemoveUserFromGroup_Success(t *testing.T) {
	srv, svc := newTestIAMServer(t)

	svc.On("RemoveUserFromGroup", mock.Anything, "t1", "g1", "u1").Return(nil)

	resp, err := srv.RemoveUserFromGroup(context.Background(), &iampb.RemoveUserFromGroupRequest{
		TenantId: "t1", GroupId: "g1", UserId: "u1",
	})

	require.NoError(t, err)
	assert.True(t, resp.Success)
	svc.AssertExpectations(t)
}

func TestIAMServer_RemoveUserFromGroup_MissingFields(t *testing.T) {
	srv, _ := newTestIAMServer(t)
	ctx := context.Background()

	_, err := srv.RemoveUserFromGroup(ctx, &iampb.RemoveUserFromGroupRequest{GroupId: "g1", UserId: "u1"})
	require.Error(t, err)

	_, err = srv.RemoveUserFromGroup(ctx, &iampb.RemoveUserFromGroupRequest{TenantId: "t1", UserId: "u1"})
	require.Error(t, err)

	_, err = srv.RemoveUserFromGroup(ctx, &iampb.RemoveUserFromGroupRequest{TenantId: "t1", GroupId: "g1"})
	require.Error(t, err)
}

// =============================================================================
// Role Operations
// =============================================================================

func TestIAMServer_CreateRole_Success(t *testing.T) {
	srv, svc := newTestIAMServer(t)

	svc.On("CreateRole", mock.Anything, mock.AnythingOfType("*iam.Role")).
		Return(&iam.Role{ID: "r1", TenantID: "t1", Name: "Admin"}, nil)

	resp, err := srv.CreateRole(context.Background(), &iampb.CreateRoleRequest{
		TenantId: "t1", Id: "r1", Name: "Admin", Description: "Admin role", Permissions: []string{"read", "write"},
	})

	require.NoError(t, err)
	assert.Equal(t, "r1", resp.Id)
	svc.AssertExpectations(t)
}

func TestIAMServer_CreateRole_MissingFields(t *testing.T) {
	srv, _ := newTestIAMServer(t)
	ctx := context.Background()

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

	resp, err := srv.GetRole(context.Background(), &iampb.GetRoleRequest{TenantId: "t1", RoleId: "r1"})

	require.NoError(t, err)
	assert.Equal(t, "r1", resp.Id)
	svc.AssertExpectations(t)
}

func TestIAMServer_GetRole_MissingFields(t *testing.T) {
	srv, _ := newTestIAMServer(t)

	_, err := srv.GetRole(context.Background(), &iampb.GetRoleRequest{RoleId: "r1"})
	require.Error(t, err)

	_, err = srv.GetRole(context.Background(), &iampb.GetRoleRequest{TenantId: "t1"})
	require.Error(t, err)
}

func TestIAMServer_UpdateRole_Success(t *testing.T) {
	srv, svc := newTestIAMServer(t)

	svc.On("UpdateRole", mock.Anything, mock.AnythingOfType("*iam.Role")).
		Return(&iam.Role{ID: "r1", TenantID: "t1", Name: "Updated"}, nil)

	resp, err := srv.UpdateRole(context.Background(), &iampb.UpdateRoleRequest{
		TenantId: "t1", RoleId: "r1", Name: "Updated", Description: "Updated desc", Permissions: []string{"read"},
	})

	require.NoError(t, err)
	assert.Equal(t, "r1", resp.Id)
	svc.AssertExpectations(t)
}

func TestIAMServer_UpdateRole_MissingFields(t *testing.T) {
	srv, _ := newTestIAMServer(t)

	_, err := srv.UpdateRole(context.Background(), &iampb.UpdateRoleRequest{RoleId: "r1"})
	require.Error(t, err)

	_, err = srv.UpdateRole(context.Background(), &iampb.UpdateRoleRequest{TenantId: "t1"})
	require.Error(t, err)
}

func TestIAMServer_DeleteRole_Success(t *testing.T) {
	srv, svc := newTestIAMServer(t)

	svc.On("DeleteRole", mock.Anything, "t1", "r1").Return(nil)

	resp, err := srv.DeleteRole(context.Background(), &iampb.DeleteRoleRequest{TenantId: "t1", RoleId: "r1"})

	require.NoError(t, err)
	assert.True(t, resp.Success)
	svc.AssertExpectations(t)
}

func TestIAMServer_DeleteRole_MissingFields(t *testing.T) {
	srv, _ := newTestIAMServer(t)

	_, err := srv.DeleteRole(context.Background(), &iampb.DeleteRoleRequest{RoleId: "r1"})
	require.Error(t, err)

	_, err = srv.DeleteRole(context.Background(), &iampb.DeleteRoleRequest{TenantId: "t1"})
	require.Error(t, err)
}

func TestIAMServer_ListRoles_Success(t *testing.T) {
	srv, svc := newTestIAMServer(t)

	svc.On("ListRoles", mock.Anything, "t1", 100, 0).
		Return([]*iam.Role{{ID: "r1", TenantID: "t1", Name: "Admin"}}, 1, nil)

	resp, err := srv.ListRoles(context.Background(), &iampb.ListRolesRequest{TenantId: "t1"})

	require.NoError(t, err)
	assert.Len(t, resp.Roles, 1)
	svc.AssertExpectations(t)
}

func TestIAMServer_ListRoles_MissingTenantID(t *testing.T) {
	srv, _ := newTestIAMServer(t)

	_, err := srv.ListRoles(context.Background(), &iampb.ListRolesRequest{})
	require.Error(t, err)
}

func TestIAMServer_AssignRole_Success(t *testing.T) {
	srv, svc := newTestIAMServer(t)

	svc.On("AssignRole", mock.Anything, "t1", "u1", "admin").Return(nil)

	resp, err := srv.AssignRole(context.Background(), &iampb.AssignRoleRequest{
		TenantId: "t1", UserId: "u1", Role: "admin",
	})

	require.NoError(t, err)
	assert.True(t, resp.Success)
	svc.AssertExpectations(t)
}

func TestIAMServer_AssignRole_MissingFields(t *testing.T) {
	srv, _ := newTestIAMServer(t)
	ctx := context.Background()

	_, err := srv.AssignRole(ctx, &iampb.AssignRoleRequest{UserId: "u1", Role: "admin"})
	require.Error(t, err)

	_, err = srv.AssignRole(ctx, &iampb.AssignRoleRequest{TenantId: "t1", Role: "admin"})
	require.Error(t, err)

	_, err = srv.AssignRole(ctx, &iampb.AssignRoleRequest{TenantId: "t1", UserId: "u1"})
	require.Error(t, err)
}

func TestIAMServer_RevokeRole_Success(t *testing.T) {
	srv, svc := newTestIAMServer(t)

	svc.On("RevokeRole", mock.Anything, "t1", "u1", "admin").Return(nil)

	resp, err := srv.RevokeRole(context.Background(), &iampb.RevokeRoleRequest{
		TenantId: "t1", UserId: "u1", Role: "admin",
	})

	require.NoError(t, err)
	assert.True(t, resp.Success)
	svc.AssertExpectations(t)
}

func TestIAMServer_RevokeRole_MissingFields(t *testing.T) {
	srv, _ := newTestIAMServer(t)
	ctx := context.Background()

	_, err := srv.RevokeRole(ctx, &iampb.RevokeRoleRequest{UserId: "u1", Role: "admin"})
	require.Error(t, err)

	_, err = srv.RevokeRole(ctx, &iampb.RevokeRoleRequest{TenantId: "t1", Role: "admin"})
	require.Error(t, err)

	_, err = srv.RevokeRole(ctx, &iampb.RevokeRoleRequest{TenantId: "t1", UserId: "u1"})
	require.Error(t, err)
}

// =============================================================================
// Application Operations
// =============================================================================

func TestIAMServer_RegisterApp_Success(t *testing.T) {
	srv, svc := newTestIAMServer(t)

	svc.On("RegisterApp", mock.Anything, mock.AnythingOfType("*iam.Application")).
		Return(&iam.Application{ClientID: "c1", TenantID: "t1", Name: "MyApp"}, nil)

	resp, err := srv.RegisterApp(context.Background(), &iampb.RegisterAppRequest{
		TenantId: "t1", Name: "MyApp", RedirectUris: []string{"http://localhost"}, GrantTypes: []string{"authorization_code"},
	})

	require.NoError(t, err)
	assert.Equal(t, "c1", resp.ClientId)
	svc.AssertExpectations(t)
}

func TestIAMServer_RegisterApp_MissingFields(t *testing.T) {
	srv, _ := newTestIAMServer(t)

	_, err := srv.RegisterApp(context.Background(), &iampb.RegisterAppRequest{Name: "Test"})
	require.Error(t, err)

	_, err = srv.RegisterApp(context.Background(), &iampb.RegisterAppRequest{TenantId: "t1"})
	require.Error(t, err)
}

func TestIAMServer_GetApp_Success(t *testing.T) {
	srv, svc := newTestIAMServer(t)

	svc.On("GetApp", mock.Anything, "t1", "c1").
		Return(&iam.Application{ClientID: "c1", TenantID: "t1", Name: "MyApp", ClientSecret: "secret"}, nil)

	resp, err := srv.GetApp(context.Background(), &iampb.GetAppRequest{TenantId: "t1", ClientId: "c1"})

	require.NoError(t, err)
	assert.Equal(t, "c1", resp.ClientId)
	assert.Empty(t, resp.ClientSecret, "client secret should be redacted in GET")
	svc.AssertExpectations(t)
}

func TestIAMServer_GetApp_MissingFields(t *testing.T) {
	srv, _ := newTestIAMServer(t)

	_, err := srv.GetApp(context.Background(), &iampb.GetAppRequest{ClientId: "c1"})
	require.Error(t, err)

	_, err = srv.GetApp(context.Background(), &iampb.GetAppRequest{TenantId: "t1"})
	require.Error(t, err)
}

func TestIAMServer_UpdateApp_Success(t *testing.T) {
	srv, svc := newTestIAMServer(t)

	svc.On("UpdateApp", mock.Anything, mock.AnythingOfType("*iam.Application")).
		Return(&iam.Application{ClientID: "c1", TenantID: "t1", Name: "Updated", ClientSecret: "secret"}, nil)

	resp, err := srv.UpdateApp(context.Background(), &iampb.UpdateAppRequest{
		TenantId: "t1", ClientId: "c1", Name: "Updated", RedirectUris: []string{"http://example.com"},
	})

	require.NoError(t, err)
	assert.Empty(t, resp.ClientSecret, "client secret should be redacted in Update")
	svc.AssertExpectations(t)
}

func TestIAMServer_UpdateApp_MissingFields(t *testing.T) {
	srv, _ := newTestIAMServer(t)

	_, err := srv.UpdateApp(context.Background(), &iampb.UpdateAppRequest{ClientId: "c1"})
	require.Error(t, err)

	_, err = srv.UpdateApp(context.Background(), &iampb.UpdateAppRequest{TenantId: "t1"})
	require.Error(t, err)
}

func TestIAMServer_DeleteApp_Success(t *testing.T) {
	srv, svc := newTestIAMServer(t)

	svc.On("DeleteApp", mock.Anything, "t1", "c1").Return(nil)

	resp, err := srv.DeleteApp(context.Background(), &iampb.DeleteAppRequest{TenantId: "t1", ClientId: "c1"})

	require.NoError(t, err)
	assert.True(t, resp.Success)
	svc.AssertExpectations(t)
}

func TestIAMServer_DeleteApp_MissingFields(t *testing.T) {
	srv, _ := newTestIAMServer(t)

	_, err := srv.DeleteApp(context.Background(), &iampb.DeleteAppRequest{ClientId: "c1"})
	require.Error(t, err)

	_, err = srv.DeleteApp(context.Background(), &iampb.DeleteAppRequest{TenantId: "t1"})
	require.Error(t, err)
}

func TestIAMServer_ListApps_Success(t *testing.T) {
	srv, svc := newTestIAMServer(t)

	svc.On("ListApps", mock.Anything, "t1", 100, 0).
		Return([]*iam.Application{{ClientID: "c1", TenantID: "t1", Name: "App1", ClientSecret: "secret"}}, 1, nil)

	resp, err := srv.ListApps(context.Background(), &iampb.ListAppsRequest{TenantId: "t1"})

	require.NoError(t, err)
	assert.Len(t, resp.Apps, 1)
	assert.Empty(t, resp.Apps[0].ClientSecret, "client secret should be redacted in list")
	svc.AssertExpectations(t)
}

func TestIAMServer_ListApps_MissingTenantID(t *testing.T) {
	srv, _ := newTestIAMServer(t)

	_, err := srv.ListApps(context.Background(), &iampb.ListAppsRequest{})
	require.Error(t, err)
}

func TestIAMServer_RotateAppSecret_Success(t *testing.T) {
	srv, svc := newTestIAMServer(t)

	svc.On("RotateAppSecret", mock.Anything, "t1", "c1").
		Return(&iam.Application{ClientID: "c1", TenantID: "t1", ClientSecret: "new-secret"}, nil)

	resp, err := srv.RotateAppSecret(context.Background(), &iampb.RotateAppSecretRequest{TenantId: "t1", ClientId: "c1"})

	require.NoError(t, err)
	assert.Equal(t, "new-secret", resp.ClientSecret, "secret should be returned after rotation")
	svc.AssertExpectations(t)
}

func TestIAMServer_RotateAppSecret_MissingFields(t *testing.T) {
	srv, _ := newTestIAMServer(t)

	_, err := srv.RotateAppSecret(context.Background(), &iampb.RotateAppSecretRequest{ClientId: "c1"})
	require.Error(t, err)

	_, err = srv.RotateAppSecret(context.Background(), &iampb.RotateAppSecretRequest{TenantId: "t1"})
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

	resp, err := srv.CreatePolicy(context.Background(), &iampb.CreatePolicyRequest{
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

	resp, err := srv.CreatePolicy(context.Background(), &iampb.CreatePolicyRequest{
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
	ctx := context.Background()

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

	resp, err := srv.GetPolicy(context.Background(), &iampb.GetPolicyRequest{TenantId: "t1", PolicyName: "my-policy"})

	require.NoError(t, err)
	assert.Equal(t, "my-policy", resp.Name)
	svc.AssertExpectations(t)
}

func TestIAMServer_GetPolicy_MissingFields(t *testing.T) {
	srv, _ := newTestIAMServer(t)

	_, err := srv.GetPolicy(context.Background(), &iampb.GetPolicyRequest{PolicyName: "test"})
	require.Error(t, err)

	_, err = srv.GetPolicy(context.Background(), &iampb.GetPolicyRequest{TenantId: "t1"})
	require.Error(t, err)
}

func TestIAMServer_UpdatePolicy_Success(t *testing.T) {
	srv, svc := newTestIAMServer(t)

	svc.On("UpdatePolicy", mock.Anything, mock.AnythingOfType("*iam.Policy")).
		Return(&iam.Policy{Metadata: iam.PolicyMetadata{Name: "my-policy", Tenant: "t1"}}, nil)

	resp, err := srv.UpdatePolicy(context.Background(), &iampb.UpdatePolicyRequest{
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

	_, err := srv.UpdatePolicy(context.Background(), &iampb.UpdatePolicyRequest{PolicyName: "test"})
	require.Error(t, err)

	_, err = srv.UpdatePolicy(context.Background(), &iampb.UpdatePolicyRequest{TenantId: "t1"})
	require.Error(t, err)
}

func TestIAMServer_DeletePolicy_Success(t *testing.T) {
	srv, svc := newTestIAMServer(t)

	svc.On("DeletePolicy", mock.Anything, "t1", "my-policy").Return(nil)

	resp, err := srv.DeletePolicy(context.Background(), &iampb.DeletePolicyRequest{TenantId: "t1", PolicyName: "my-policy"})

	require.NoError(t, err)
	assert.True(t, resp.Success)
	svc.AssertExpectations(t)
}

func TestIAMServer_DeletePolicy_MissingFields(t *testing.T) {
	srv, _ := newTestIAMServer(t)

	_, err := srv.DeletePolicy(context.Background(), &iampb.DeletePolicyRequest{PolicyName: "test"})
	require.Error(t, err)

	_, err = srv.DeletePolicy(context.Background(), &iampb.DeletePolicyRequest{TenantId: "t1"})
	require.Error(t, err)
}

func TestIAMServer_ListPolicies_Success(t *testing.T) {
	srv, svc := newTestIAMServer(t)

	svc.On("ListPolicies", mock.Anything, "t1", 100, 0).
		Return([]*iam.Policy{{Metadata: iam.PolicyMetadata{Name: "p1"}}}, 1, nil)

	resp, err := srv.ListPolicies(context.Background(), &iampb.ListPoliciesRequest{TenantId: "t1"})

	require.NoError(t, err)
	assert.Len(t, resp.Policies, 1)
	svc.AssertExpectations(t)
}

func TestIAMServer_ListPolicies_MissingTenantID(t *testing.T) {
	srv, _ := newTestIAMServer(t)

	_, err := srv.ListPolicies(context.Background(), &iampb.ListPoliciesRequest{})
	require.Error(t, err)
}

func TestIAMServer_ValidatePolicy_Success(t *testing.T) {
	srv, svc := newTestIAMServer(t)

	svc.On("ValidatePolicy", mock.Anything, "t1", mock.Anything).
		Return(true, []string{}, nil)

	resp, err := srv.ValidatePolicy(context.Background(), &iampb.ValidatePolicyRequest{
		TenantId: "t1", SpecYaml: []byte("rules: []"),
	})

	require.NoError(t, err)
	assert.True(t, resp.Valid)
	svc.AssertExpectations(t)
}

func TestIAMServer_ValidatePolicy_MissingFields(t *testing.T) {
	srv, _ := newTestIAMServer(t)

	_, err := srv.ValidatePolicy(context.Background(), &iampb.ValidatePolicyRequest{SpecYaml: []byte("x")})
	require.Error(t, err)

	_, err = srv.ValidatePolicy(context.Background(), &iampb.ValidatePolicyRequest{TenantId: "t1"})
	require.Error(t, err)
}

func TestIAMServer_TestPolicy_Success(t *testing.T) {
	srv, svc := newTestIAMServer(t)

	svc.On("TestPolicy", mock.Anything, "t1", mock.AnythingOfType("*iam.CheckRequest")).
		Return(&iam.CheckResponse{Allowed: true, MatchedPolicy: "p1", MatchedRule: "r1", EvaluationTimeNs: 100}, nil)

	resp, err := srv.TestPolicy(context.Background(), &iampb.TestPolicyRequest{
		TenantId: "t1",
		Subject:  &iampb.Subject{Id: "u1"},
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
	ctx := context.Background()

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
		Return(&iam.AdminAssignment{ID: "a1", UserID: "u1", AdminRole: iam.AdminRoleSuperAdmin}, nil)

	resp, err := srv.CreateAdminAssignment(context.Background(), &iampb.CreateAdminAssignmentRequest{
		UserId:            "u1",
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
	ctx := context.Background()

	_, err := srv.CreateAdminAssignment(ctx, &iampb.CreateAdminAssignmentRequest{
		AdminRole: iampb.AdminRole_ADMIN_ROLE_SUPER_ADMIN, GrantedBy: "admin1",
	})
	require.Error(t, err)

	_, err = srv.CreateAdminAssignment(ctx, &iampb.CreateAdminAssignmentRequest{
		UserId: "u1", GrantedBy: "admin1",
	})
	require.Error(t, err)

	_, err = srv.CreateAdminAssignment(ctx, &iampb.CreateAdminAssignmentRequest{
		UserId: "u1", AdminRole: iampb.AdminRole_ADMIN_ROLE_SUPER_ADMIN,
	})
	require.Error(t, err)
}

func TestIAMServer_GetAdminAssignment_Success(t *testing.T) {
	srv, svc := newTestIAMServer(t)

	svc.On("GetAdminAssignment", mock.Anything, "a1").
		Return(&iam.AdminAssignment{ID: "a1", UserID: "u1"}, nil)

	resp, err := srv.GetAdminAssignment(context.Background(), &iampb.GetAdminAssignmentRequest{Id: "a1"})

	require.NoError(t, err)
	assert.Equal(t, "a1", resp.Id)
	svc.AssertExpectations(t)
}

func TestIAMServer_GetAdminAssignment_MissingID(t *testing.T) {
	srv, _ := newTestIAMServer(t)

	_, err := srv.GetAdminAssignment(context.Background(), &iampb.GetAdminAssignmentRequest{})
	require.Error(t, err)
}

func TestIAMServer_UpdateAdminAssignment_Success(t *testing.T) {
	srv, svc := newTestIAMServer(t)

	svc.On("UpdateAdminAssignment", mock.Anything, "a1", mock.AnythingOfType("iam.UpdateAdminAssignmentOptions")).
		Return(&iam.AdminAssignment{ID: "a1", UserID: "u1"}, nil)

	resp, err := srv.UpdateAdminAssignment(context.Background(), &iampb.UpdateAdminAssignmentRequest{
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

	_, err := srv.UpdateAdminAssignment(context.Background(), &iampb.UpdateAdminAssignmentRequest{})
	require.Error(t, err)
}

func TestIAMServer_DeleteAdminAssignment_Success(t *testing.T) {
	srv, svc := newTestIAMServer(t)

	svc.On("DeleteAdminAssignment", mock.Anything, "a1").Return(nil)

	resp, err := srv.DeleteAdminAssignment(context.Background(), &iampb.DeleteAdminAssignmentRequest{Id: "a1"})

	require.NoError(t, err)
	assert.True(t, resp.Success)
	svc.AssertExpectations(t)
}

func TestIAMServer_DeleteAdminAssignment_MissingID(t *testing.T) {
	srv, _ := newTestIAMServer(t)

	_, err := srv.DeleteAdminAssignment(context.Background(), &iampb.DeleteAdminAssignmentRequest{})
	require.Error(t, err)
}

func TestIAMServer_ListAdminAssignments_Success(t *testing.T) {
	srv, svc := newTestIAMServer(t)

	svc.On("ListAdminAssignments", mock.Anything, mock.AnythingOfType("iam.ListAdminAssignmentsOptions")).
		Return([]*iam.AdminAssignment{{ID: "a1", UserID: "u1"}}, nil)

	resp, err := srv.ListAdminAssignments(context.Background(), &iampb.ListAdminAssignmentsRequest{
		UserId: "u1", TenantId: "t1", ActiveOnly: true,
	})

	require.NoError(t, err)
	assert.Len(t, resp.Assignments, 1)
	svc.AssertExpectations(t)
}

func TestIAMServer_CheckAdminPermission_Success(t *testing.T) {
	srv, svc := newTestIAMServer(t)

	svc.On("CheckAdminPermission", mock.Anything, mock.AnythingOfType("*iam.AdminCheckRequest")).
		Return(&iam.AdminCheckResponse{Allowed: true, AssignmentID: "a1", AdminRole: iam.AdminRoleSuperAdmin, Reason: "granted"}, nil)

	resp, err := srv.CheckAdminPermission(context.Background(), &iampb.CheckAdminPermissionRequest{
		UserId: "u1", Permission: "manage_users", TenantId: "t1", ResourceType: "user", ResourceId: "u2",
	})

	require.NoError(t, err)
	assert.True(t, resp.Allowed)
	svc.AssertExpectations(t)
}

func TestIAMServer_CheckAdminPermission_MissingFields(t *testing.T) {
	srv, _ := newTestIAMServer(t)

	_, err := srv.CheckAdminPermission(context.Background(), &iampb.CheckAdminPermissionRequest{Permission: "x"})
	require.Error(t, err)

	_, err = srv.CheckAdminPermission(context.Background(), &iampb.CheckAdminPermissionRequest{UserId: "u1"})
	require.Error(t, err)
}

// =============================================================================
// Impersonation Operations
// =============================================================================

func TestIAMServer_StartImpersonation_Success(t *testing.T) {
	srv, svc := newTestIAMServer(t)

	svc.On("StartImpersonation", mock.Anything, mock.AnythingOfType("*iam.StartImpersonationOptions")).
		Return(&iam.ImpersonationSession{ID: "s1", AdminUserID: "admin1", TargetUserID: "u1"}, nil)

	resp, err := srv.StartImpersonation(context.Background(), &iampb.StartImpersonationRequest{
		AdminUserId: "admin1", TargetUserId: "u1", TenantId: "t1", Reason: "support",
	})

	require.NoError(t, err)
	assert.Equal(t, "s1", resp.Id)
	svc.AssertExpectations(t)
}

func TestIAMServer_StartImpersonation_MissingFields(t *testing.T) {
	srv, _ := newTestIAMServer(t)
	ctx := context.Background()

	_, err := srv.StartImpersonation(ctx, &iampb.StartImpersonationRequest{
		TargetUserId: "u1", TenantId: "t1", Reason: "x",
	})
	require.Error(t, err)

	_, err = srv.StartImpersonation(ctx, &iampb.StartImpersonationRequest{
		AdminUserId: "a", TenantId: "t1", Reason: "x",
	})
	require.Error(t, err)

	_, err = srv.StartImpersonation(ctx, &iampb.StartImpersonationRequest{
		AdminUserId: "a", TargetUserId: "u1", Reason: "x",
	})
	require.Error(t, err)

	_, err = srv.StartImpersonation(ctx, &iampb.StartImpersonationRequest{
		AdminUserId: "a", TargetUserId: "u1", TenantId: "t1",
	})
	require.Error(t, err)
}

func TestIAMServer_EndImpersonation_Success(t *testing.T) {
	srv, svc := newTestIAMServer(t)

	svc.On("EndImpersonation", mock.Anything, "s1").Return(nil)

	resp, err := srv.EndImpersonation(context.Background(), &iampb.EndImpersonationRequest{SessionId: "s1"})

	require.NoError(t, err)
	assert.True(t, resp.Success)
	svc.AssertExpectations(t)
}

func TestIAMServer_EndImpersonation_MissingSessionID(t *testing.T) {
	srv, _ := newTestIAMServer(t)

	_, err := srv.EndImpersonation(context.Background(), &iampb.EndImpersonationRequest{})
	require.Error(t, err)
}

func TestIAMServer_GetActiveImpersonations_Success(t *testing.T) {
	srv, svc := newTestIAMServer(t)

	svc.On("GetActiveImpersonations", mock.Anything, "admin1").
		Return([]*iam.ImpersonationSession{{ID: "s1", AdminUserID: "admin1"}}, nil)

	resp, err := srv.GetActiveImpersonations(context.Background(), &iampb.GetActiveImpersonationsRequest{AdminUserId: "admin1"})

	require.NoError(t, err)
	assert.Len(t, resp.Sessions, 1)
	svc.AssertExpectations(t)
}

func TestIAMServer_GetActiveImpersonations_MissingAdminUserID(t *testing.T) {
	srv, _ := newTestIAMServer(t)

	_, err := srv.GetActiveImpersonations(context.Background(), &iampb.GetActiveImpersonationsRequest{})
	require.Error(t, err)
}

// =============================================================================
// GDPR Consent Operations
// =============================================================================

func TestIAMServer_GrantConsent_Success(t *testing.T) {
	srv, svc := newTestIAMServer(t)

	svc.On("GrantConsent", mock.Anything, "t1", "u1", mock.Anything, "v1", mock.Anything, "1.2.3.4", "Mozilla").
		Return(&iam.Consent{ID: "c1", TenantID: "t1", UserID: "u1"}, nil)

	resp, err := srv.GrantConsent(context.Background(), &iampb.GrantConsentRequest{
		TenantId:  "t1",
		UserId:    "u1",
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
	ctx := context.Background()

	_, err := srv.GrantConsent(ctx, &iampb.GrantConsentRequest{
		UserId: "u1", Purpose: iampb.ConsentPurpose_CONSENT_PURPOSE_DATA_PROCESSING,
	})
	require.Error(t, err)

	_, err = srv.GrantConsent(ctx, &iampb.GrantConsentRequest{
		TenantId: "t1", Purpose: iampb.ConsentPurpose_CONSENT_PURPOSE_DATA_PROCESSING,
	})
	require.Error(t, err)

	_, err = srv.GrantConsent(ctx, &iampb.GrantConsentRequest{
		TenantId: "t1", UserId: "u1",
	})
	require.Error(t, err)
}

func TestIAMServer_WithdrawConsent_Success(t *testing.T) {
	srv, svc := newTestIAMServer(t)

	svc.On("WithdrawConsent", mock.Anything, "t1", "u1", mock.Anything, "1.2.3.4", "Mozilla").Return(nil)

	resp, err := srv.WithdrawConsent(context.Background(), &iampb.WithdrawConsentRequest{
		TenantId:  "t1",
		UserId:    "u1",
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
	ctx := context.Background()

	_, err := srv.WithdrawConsent(ctx, &iampb.WithdrawConsentRequest{UserId: "u1", Purpose: iampb.ConsentPurpose_CONSENT_PURPOSE_DATA_PROCESSING})
	require.Error(t, err)

	_, err = srv.WithdrawConsent(ctx, &iampb.WithdrawConsentRequest{TenantId: "t1", Purpose: iampb.ConsentPurpose_CONSENT_PURPOSE_DATA_PROCESSING})
	require.Error(t, err)

	_, err = srv.WithdrawConsent(ctx, &iampb.WithdrawConsentRequest{TenantId: "t1", UserId: "u1"})
	require.Error(t, err)
}

func TestIAMServer_GetConsent_Success(t *testing.T) {
	srv, svc := newTestIAMServer(t)

	svc.On("GetConsent", mock.Anything, "t1", "u1", mock.Anything).
		Return(&iam.Consent{ID: "c1", TenantID: "t1", UserID: "u1"}, nil)

	resp, err := srv.GetConsent(context.Background(), &iampb.GetConsentRequest{
		TenantId: "t1", UserId: "u1", Purpose: iampb.ConsentPurpose_CONSENT_PURPOSE_DATA_PROCESSING,
	})

	require.NoError(t, err)
	assert.Equal(t, "c1", resp.Id)
	svc.AssertExpectations(t)
}

func TestIAMServer_GetConsent_MissingFields(t *testing.T) {
	srv, _ := newTestIAMServer(t)
	ctx := context.Background()

	_, err := srv.GetConsent(ctx, &iampb.GetConsentRequest{UserId: "u1", Purpose: iampb.ConsentPurpose_CONSENT_PURPOSE_DATA_PROCESSING})
	require.Error(t, err)

	_, err = srv.GetConsent(ctx, &iampb.GetConsentRequest{TenantId: "t1", Purpose: iampb.ConsentPurpose_CONSENT_PURPOSE_DATA_PROCESSING})
	require.Error(t, err)

	_, err = srv.GetConsent(ctx, &iampb.GetConsentRequest{TenantId: "t1", UserId: "u1"})
	require.Error(t, err)
}

func TestIAMServer_ListUserConsents_Success(t *testing.T) {
	srv, svc := newTestIAMServer(t)

	svc.On("ListUserConsents", mock.Anything, "t1", "u1").
		Return([]*iam.Consent{{ID: "c1", TenantID: "t1", UserID: "u1"}}, nil)

	resp, err := srv.ListUserConsents(context.Background(), &iampb.ListUserConsentsRequest{TenantId: "t1", UserId: "u1"})

	require.NoError(t, err)
	assert.Len(t, resp.Consents, 1)
	svc.AssertExpectations(t)
}

func TestIAMServer_ListUserConsents_MissingFields(t *testing.T) {
	srv, _ := newTestIAMServer(t)

	_, err := srv.ListUserConsents(context.Background(), &iampb.ListUserConsentsRequest{UserId: "u1"})
	require.Error(t, err)

	_, err = srv.ListUserConsents(context.Background(), &iampb.ListUserConsentsRequest{TenantId: "t1"})
	require.Error(t, err)
}

// =============================================================================
// GDPR Data Export Operations
// =============================================================================

func TestIAMServer_RequestExport_Success(t *testing.T) {
	srv, svc := newTestIAMServer(t)

	svc.On("RequestExport", mock.Anything, "t1", "u1", "admin1", mock.Anything, mock.Anything, mock.Anything).
		Return(&iam.DataExportRequest{ID: "e1", TenantID: "t1", UserID: "u1"}, nil)

	resp, err := srv.RequestExport(context.Background(), &iampb.RequestExportRequest{
		TenantId:          "t1",
		UserId:            "u1",
		RequestedBy:       "admin1",
		IncludeCategories: []string{"profile"},
		ExcludeCategories: []string{"audit"},
	})

	require.NoError(t, err)
	assert.Equal(t, "e1", resp.Id)
	svc.AssertExpectations(t)
}

func TestIAMServer_RequestExport_MissingFields(t *testing.T) {
	srv, _ := newTestIAMServer(t)
	ctx := context.Background()

	_, err := srv.RequestExport(ctx, &iampb.RequestExportRequest{UserId: "u1", RequestedBy: "a"})
	require.Error(t, err)

	_, err = srv.RequestExport(ctx, &iampb.RequestExportRequest{TenantId: "t1", RequestedBy: "a"})
	require.Error(t, err)

	_, err = srv.RequestExport(ctx, &iampb.RequestExportRequest{TenantId: "t1", UserId: "u1"})
	require.Error(t, err)
}

func TestIAMServer_GetExportStatus_Success(t *testing.T) {
	srv, svc := newTestIAMServer(t)

	svc.On("GetExportStatus", mock.Anything, "e1").
		Return(&iam.DataExportRequest{ID: "e1"}, nil)

	resp, err := srv.GetExportStatus(context.Background(), &iampb.GetExportStatusRequest{ExportId: "e1"})

	require.NoError(t, err)
	assert.Equal(t, "e1", resp.Id)
	svc.AssertExpectations(t)
}

func TestIAMServer_GetExportStatus_MissingID(t *testing.T) {
	srv, _ := newTestIAMServer(t)

	_, err := srv.GetExportStatus(context.Background(), &iampb.GetExportStatusRequest{})
	require.Error(t, err)
}

func TestIAMServer_GenerateExport_Success(t *testing.T) {
	srv, svc := newTestIAMServer(t)

	svc.On("GenerateExport", mock.Anything, "e1").
		Return(&iam.UserDataExport{ExportMetadata: iam.ExportMetadata{ExportID: "e1"}}, nil)

	resp, err := srv.GenerateExport(context.Background(), &iampb.GenerateExportRequest{ExportId: "e1"})

	require.NoError(t, err)
	assert.Equal(t, "e1", resp.ExportMetadata.ExportId)
	svc.AssertExpectations(t)
}

func TestIAMServer_GenerateExport_MissingID(t *testing.T) {
	srv, _ := newTestIAMServer(t)

	_, err := srv.GenerateExport(context.Background(), &iampb.GenerateExportRequest{})
	require.Error(t, err)
}

// =============================================================================
// GDPR Data Deletion Operations
// =============================================================================

func TestIAMServer_RequestDeletion_Success(t *testing.T) {
	srv, svc := newTestIAMServer(t)

	svc.On("RequestDeletion", mock.Anything, "t1", "u1", "admin1", "gdpr", mock.Anything, mock.Anything, 30).
		Return(&iam.DataDeletionRequest{ID: "d1", TenantID: "t1", UserID: "u1"}, nil)

	resp, err := srv.RequestDeletion(context.Background(), &iampb.RequestDeletionRequest{
		TenantId:           "t1",
		UserId:             "u1",
		RequestedBy:        "admin1",
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
	ctx := context.Background()

	_, err := srv.RequestDeletion(ctx, &iampb.RequestDeletionRequest{UserId: "u1", RequestedBy: "a"})
	require.Error(t, err)

	_, err = srv.RequestDeletion(ctx, &iampb.RequestDeletionRequest{TenantId: "t1", RequestedBy: "a"})
	require.Error(t, err)

	_, err = srv.RequestDeletion(ctx, &iampb.RequestDeletionRequest{TenantId: "t1", UserId: "u1"})
	require.Error(t, err)
}

func TestIAMServer_CancelDeletion_Success(t *testing.T) {
	srv, svc := newTestIAMServer(t)

	svc.On("CancelDeletion", mock.Anything, "d1", "admin1").Return(nil)

	resp, err := srv.CancelDeletion(context.Background(), &iampb.CancelDeletionRequest{DeletionId: "d1", CancelledBy: "admin1"})

	require.NoError(t, err)
	assert.True(t, resp.Success)
	svc.AssertExpectations(t)
}

func TestIAMServer_CancelDeletion_MissingFields(t *testing.T) {
	srv, _ := newTestIAMServer(t)

	_, err := srv.CancelDeletion(context.Background(), &iampb.CancelDeletionRequest{CancelledBy: "admin1"})
	require.Error(t, err)

	_, err = srv.CancelDeletion(context.Background(), &iampb.CancelDeletionRequest{DeletionId: "d1"})
	require.Error(t, err)
}

func TestIAMServer_ProcessDeletion_Success(t *testing.T) {
	srv, svc := newTestIAMServer(t)

	svc.On("ProcessDeletion", mock.Anything, "d1").
		Return(&iam.DeletionSummary{ProfileDeleted: true, SessionsDeleted: 5}, nil)

	resp, err := srv.ProcessDeletion(context.Background(), &iampb.ProcessDeletionRequest{DeletionId: "d1"})

	require.NoError(t, err)
	assert.True(t, resp.ProfileDeleted)
	assert.Equal(t, int32(5), resp.SessionsDeleted)
	svc.AssertExpectations(t)
}

func TestIAMServer_ProcessDeletion_MissingID(t *testing.T) {
	srv, _ := newTestIAMServer(t)

	_, err := srv.ProcessDeletion(context.Background(), &iampb.ProcessDeletionRequest{})
	require.Error(t, err)
}

func TestIAMServer_GetDeletionStatus_Success(t *testing.T) {
	srv, svc := newTestIAMServer(t)

	svc.On("GetDeletionStatus", mock.Anything, "d1").
		Return(&iam.DataDeletionRequest{ID: "d1"}, nil)

	resp, err := srv.GetDeletionStatus(context.Background(), &iampb.GetDeletionStatusRequest{DeletionId: "d1"})

	require.NoError(t, err)
	assert.Equal(t, "d1", resp.Id)
	svc.AssertExpectations(t)
}

func TestIAMServer_GetDeletionStatus_MissingID(t *testing.T) {
	srv, _ := newTestIAMServer(t)

	_, err := srv.GetDeletionStatus(context.Background(), &iampb.GetDeletionStatusRequest{})
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
