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

// Package main provides mock IAM client for testing CLI command success paths.
package main

import (
	"context"
	"time"

	"github.com/jeremyhahn/go-quicbaas/pkg/cli"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/client/iam"
)

// testMockAdminClient implements iam.AdminClient for testing.
type testMockAdminClient struct {
	CreateAssignmentFunc         func(ctx context.Context, req *iam.CreateAdminAssignmentRequest) (*iam.AdminAssignment, error)
	GetAssignmentFunc            func(ctx context.Context, assignmentID string) (*iam.AdminAssignment, error)
	UpdateAssignmentFunc         func(ctx context.Context, assignmentID string, req *iam.UpdateAdminAssignmentRequest) (*iam.AdminAssignment, error)
	DeleteAssignmentFunc         func(ctx context.Context, assignmentID string) error
	ListAssignmentsFunc          func(ctx context.Context, req *iam.ListAdminAssignmentsRequest) (*iam.ListAdminAssignmentsResponse, error)
	CheckPermissionFunc          func(ctx context.Context, req *iam.AdminCheckRequest) (*iam.AdminCheckResponse, error)
	StartImpersonationFunc       func(ctx context.Context, req *iam.ImpersonationRequest) (*iam.ImpersonationSession, error)
	EndImpersonationFunc         func(ctx context.Context, sessionID string) error
	ListActiveImpersonationsFunc func(ctx context.Context) (*iam.ListImpersonationsResponse, error)
}

func (m *testMockAdminClient) CreateAssignment(ctx context.Context, req *iam.CreateAdminAssignmentRequest) (*iam.AdminAssignment, error) {
	if m.CreateAssignmentFunc != nil {
		return m.CreateAssignmentFunc(ctx, req)
	}
	return &iam.AdminAssignment{
		ID:        "adm_test123",
		AdminID:   req.AdminID,
		RoleName:  req.RoleID,
		TenantID:  req.TenantID,
		CreatedBy: "system",
		CreatedAt: time.Now(),
		Status:    iam.AdminAssignmentStatusActive,
	}, nil
}

func (m *testMockAdminClient) GetAssignment(ctx context.Context, assignmentID string) (*iam.AdminAssignment, error) {
	if m.GetAssignmentFunc != nil {
		return m.GetAssignmentFunc(ctx, assignmentID)
	}
	return &iam.AdminAssignment{
		ID:          assignmentID,
		AdminID:     "user_abc",
		AdminName:   "Test Admin",
		RoleName:    "tenant_admin",
		TenantID:    "tenant_test",
		Scope:       iam.AdminScopeTenant,
		ScopeID:     "tenant_test",
		Permissions: []string{"user:create", "user:read"},
		CreatedBy:   "system",
		CreatedAt:   time.Now(),
		ExpiresAt:   time.Now().Add(24 * time.Hour),
		LastUsedAt:  time.Now(),
		Status:      iam.AdminAssignmentStatusActive,
	}, nil
}

func (m *testMockAdminClient) UpdateAssignment(ctx context.Context, assignmentID string, req *iam.UpdateAdminAssignmentRequest) (*iam.AdminAssignment, error) {
	if m.UpdateAssignmentFunc != nil {
		return m.UpdateAssignmentFunc(ctx, assignmentID, req)
	}
	return &iam.AdminAssignment{
		ID:        assignmentID,
		AdminID:   "user_abc",
		RoleName:  "tenant_admin",
		Status:    iam.AdminAssignmentStatusActive,
		CreatedAt: time.Now(),
	}, nil
}

func (m *testMockAdminClient) DeleteAssignment(ctx context.Context, assignmentID string) error {
	if m.DeleteAssignmentFunc != nil {
		return m.DeleteAssignmentFunc(ctx, assignmentID)
	}
	return nil
}

func (m *testMockAdminClient) ListAssignments(ctx context.Context, req *iam.ListAdminAssignmentsRequest) (*iam.ListAdminAssignmentsResponse, error) {
	if m.ListAssignmentsFunc != nil {
		return m.ListAssignmentsFunc(ctx, req)
	}
	return &iam.ListAdminAssignmentsResponse{
		Assignments: []iam.AdminAssignment{
			{
				ID:        "adm_001",
				AdminID:   "user_abc",
				RoleName:  "tenant_admin",
				TenantID:  "tenant_test",
				Status:    iam.AdminAssignmentStatusActive,
				CreatedAt: time.Now(),
			},
		},
		TotalCount: 1,
	}, nil
}

func (m *testMockAdminClient) CheckPermission(ctx context.Context, req *iam.AdminCheckRequest) (*iam.AdminCheckResponse, error) {
	if m.CheckPermissionFunc != nil {
		return m.CheckPermissionFunc(ctx, req)
	}
	return &iam.AdminCheckResponse{
		Allowed:      true,
		AssignmentID: "adm_001",
	}, nil
}

func (m *testMockAdminClient) StartImpersonation(ctx context.Context, req *iam.ImpersonationRequest) (*iam.ImpersonationSession, error) {
	if m.StartImpersonationFunc != nil {
		return m.StartImpersonationFunc(ctx, req)
	}
	return &iam.ImpersonationSession{
		ID:           "imp_test123",
		AdminID:      "admin_xyz",
		TargetUserID: req.TargetUserID,
		StartedAt:    time.Now(),
		ExpiresAt:    time.Now().Add(time.Hour),
		Active:       true,
	}, nil
}

func (m *testMockAdminClient) EndImpersonation(ctx context.Context, sessionID string) error {
	if m.EndImpersonationFunc != nil {
		return m.EndImpersonationFunc(ctx, sessionID)
	}
	return nil
}

func (m *testMockAdminClient) ListActiveImpersonations(ctx context.Context, adminUserID string) (*iam.ListImpersonationsResponse, error) {
	if m.ListActiveImpersonationsFunc != nil {
		return m.ListActiveImpersonationsFunc(ctx)
	}
	return &iam.ListImpersonationsResponse{
		Sessions: []iam.ImpersonationSession{
			{
				ID:           "imp_001",
				AdminID:      "admin_xyz",
				TargetUserID: "user_abc",
				StartedAt:    time.Now(),
				ExpiresAt:    time.Now().Add(time.Hour),
				Active:       true,
			},
		},
		TotalCount: 1,
	}, nil
}

// testMockTenantClient implements iam.TenantClient for testing.
type testMockTenantClient struct {
	CreateFunc func(ctx context.Context, req *iam.CreateTenantRequest) (*iam.Tenant, error)
	GetFunc    func(ctx context.Context, tenantID string) (*iam.Tenant, error)
	UpdateFunc func(ctx context.Context, tenantID string, req *iam.UpdateTenantRequest) (*iam.Tenant, error)
	DeleteFunc func(ctx context.Context, tenantID string) error
	ListFunc   func(ctx context.Context, req *iam.ListTenantsRequest) (*iam.ListTenantsResponse, error)

	ProvisionFunc func(ctx context.Context, req *iam.ProvisionTenantRequest) (*iam.ProvisionTenantResponse, error)
}

func (m *testMockTenantClient) Create(ctx context.Context, req *iam.CreateTenantRequest) (*iam.Tenant, error) {
	if m.CreateFunc != nil {
		return m.CreateFunc(ctx, req)
	}
	return &iam.Tenant{
		ID:          req.Name,
		Name:        req.Name,
		DisplayName: req.DisplayName,
		Description: req.Description,
		Status:      iam.TenantStatusActive,
		CreatedAt:   time.Now(),
		UpdatedAt:   time.Now(),
	}, nil
}

func (m *testMockTenantClient) Get(ctx context.Context, tenantID string) (*iam.Tenant, error) {
	if m.GetFunc != nil {
		return m.GetFunc(ctx, tenantID)
	}
	return &iam.Tenant{
		ID:          tenantID,
		Name:        tenantID,
		DisplayName: "Test Tenant",
		Status:      iam.TenantStatusActive,
		CreatedAt:   time.Now(),
		UpdatedAt:   time.Now(),
	}, nil
}

func (m *testMockTenantClient) Update(ctx context.Context, tenantID string, req *iam.UpdateTenantRequest) (*iam.Tenant, error) {
	if m.UpdateFunc != nil {
		return m.UpdateFunc(ctx, tenantID, req)
	}
	return &iam.Tenant{
		ID:          tenantID,
		Name:        tenantID,
		DisplayName: "Updated Tenant",
		Status:      iam.TenantStatusActive,
		CreatedAt:   time.Now(),
		UpdatedAt:   time.Now(),
	}, nil
}

func (m *testMockTenantClient) Delete(ctx context.Context, tenantID string) error {
	if m.DeleteFunc != nil {
		return m.DeleteFunc(ctx, tenantID)
	}
	return nil
}

func (m *testMockTenantClient) Provision(ctx context.Context, req *iam.ProvisionTenantRequest) (*iam.ProvisionTenantResponse, error) {
	if m.ProvisionFunc != nil {
		return m.ProvisionFunc(ctx, req)
	}
	return &iam.ProvisionTenantResponse{TenantID: req.TenantID}, nil
}

func (m *testMockTenantClient) List(ctx context.Context, req *iam.ListTenantsRequest) (*iam.ListTenantsResponse, error) {
	if m.ListFunc != nil {
		return m.ListFunc(ctx, req)
	}
	return &iam.ListTenantsResponse{
		Tenants: []iam.Tenant{
			{ID: "tenant_001", Name: "tenant_001", DisplayName: "Test Tenant", Status: iam.TenantStatusActive, CreatedAt: time.Now()},
		},
		Pagination: iam.PaginationInfo{TotalCount: 1, HasMore: false},
	}, nil
}

// testMockRoleClient implements iam.RoleClient for testing.
type testMockRoleClient struct {
	CreateFunc    func(ctx context.Context, tenantID string, req *iam.CreateRoleRequest) (*iam.Role, error)
	GetFunc       func(ctx context.Context, tenantID, roleID string) (*iam.Role, error)
	UpdateFunc    func(ctx context.Context, tenantID, roleID string, req *iam.UpdateRoleRequest) (*iam.Role, error)
	DeleteFunc    func(ctx context.Context, tenantID, roleID string) error
	ListFunc      func(ctx context.Context, tenantID string, req *iam.ListRolesRequest) (*iam.ListRolesResponse, error)
	ReconcileFunc func(ctx context.Context, tenantID string) (*iam.ReconcileRolesResponse, error)
}

func (m *testMockRoleClient) Create(ctx context.Context, tenantID string, req *iam.CreateRoleRequest) (*iam.Role, error) {
	if m.CreateFunc != nil {
		return m.CreateFunc(ctx, tenantID, req)
	}
	return &iam.Role{ID: req.Name, Name: req.Name, DisplayName: req.DisplayName, TenantID: tenantID, Permissions: req.Permissions, CreatedAt: time.Now()}, nil
}

func (m *testMockRoleClient) Get(ctx context.Context, tenantID, roleID string) (*iam.Role, error) {
	if m.GetFunc != nil {
		return m.GetFunc(ctx, tenantID, roleID)
	}
	return &iam.Role{ID: roleID, Name: roleID, DisplayName: "Test Role", TenantID: tenantID, Permissions: []string{"read", "write"}, CreatedAt: time.Now()}, nil
}

func (m *testMockRoleClient) Update(ctx context.Context, tenantID, roleID string, req *iam.UpdateRoleRequest) (*iam.Role, error) {
	if m.UpdateFunc != nil {
		return m.UpdateFunc(ctx, tenantID, roleID, req)
	}
	return &iam.Role{ID: roleID, Name: roleID, DisplayName: "Updated Role", TenantID: tenantID, CreatedAt: time.Now()}, nil
}

func (m *testMockRoleClient) Delete(ctx context.Context, tenantID, roleID string) error {
	if m.DeleteFunc != nil {
		return m.DeleteFunc(ctx, tenantID, roleID)
	}
	return nil
}

func (m *testMockRoleClient) List(ctx context.Context, tenantID string, req *iam.ListRolesRequest) (*iam.ListRolesResponse, error) {
	if m.ListFunc != nil {
		return m.ListFunc(ctx, tenantID, req)
	}
	return &iam.ListRolesResponse{
		Roles:      []iam.Role{{ID: "role_001", Name: "admin", DisplayName: "Admin", TenantID: tenantID, Permissions: []string{"all"}, CreatedAt: time.Now()}},
		Pagination: iam.PaginationInfo{TotalCount: 1, HasMore: false},
	}, nil
}

func (m *testMockRoleClient) Reconcile(ctx context.Context, tenantID string) (*iam.ReconcileRolesResponse, error) {
	if m.ReconcileFunc != nil {
		return m.ReconcileFunc(ctx, tenantID)
	}
	return &iam.ReconcileRolesResponse{
		TenantID: tenantID,
		Roles: []iam.RoleReconcileResult{
			{RoleID: "role_001", Name: "admin", Added: []string{"kv:write"}, Removed: []string{"kv:put"}},
		},
		Changed: true,
	}, nil
}

// testMockUserClient implements iam.UserClient for testing.
type testMockUserClient struct {
	CreateFunc         func(ctx context.Context, tenantID string, req *iam.CreateUserRequest) (*iam.User, error)
	GetFunc            func(ctx context.Context, tenantID, userID string) (*iam.User, error)
	GetByEmailFunc     func(ctx context.Context, tenantID, email string) (*iam.User, error)
	FindByUsernameFunc func(ctx context.Context, tenantID, username string) (*iam.User, error)
	UpdateFunc         func(ctx context.Context, tenantID, userID string, req *iam.UpdateUserRequest) (*iam.User, error)
	DeleteFunc         func(ctx context.Context, tenantID, userID string) error
	ListFunc           func(ctx context.Context, tenantID string, req *iam.ListUsersRequest) (*iam.ListUsersResponse, error)
	AssignRoleFunc     func(ctx context.Context, tenantID, userID, roleID string) error
	RevokeRoleFunc     func(ctx context.Context, tenantID, userID, roleID string) error
	SuspendFunc        func(ctx context.Context, tenantID, userID string) error
	UnsuspendFunc      func(ctx context.Context, tenantID, userID string) error
	ApproveFunc        func(ctx context.Context, tenantID, userID string) error
	RejectFunc         func(ctx context.Context, tenantID, userID, reason string) error
}

func (m *testMockUserClient) Create(ctx context.Context, tenantID string, req *iam.CreateUserRequest) (*iam.User, error) {
	if m.CreateFunc != nil {
		return m.CreateFunc(ctx, tenantID, req)
	}
	return &iam.User{ID: "user_new", Username: req.Username, Email: req.Email, DisplayName: req.DisplayName, TenantID: tenantID, Status: iam.UserStatusActive, CreatedAt: time.Now()}, nil
}

func (m *testMockUserClient) Get(ctx context.Context, tenantID, userID string) (*iam.User, error) {
	if m.GetFunc != nil {
		return m.GetFunc(ctx, tenantID, userID)
	}
	return &iam.User{ID: userID, Username: "testuser", Email: "test@example.com", DisplayName: "Test User", TenantID: tenantID, Status: iam.UserStatusActive, Roles: []string{"admin"}, CreatedAt: time.Now()}, nil
}

func (m *testMockUserClient) GetByEmail(ctx context.Context, tenantID, email string) (*iam.User, error) {
	if m.GetByEmailFunc != nil {
		return m.GetByEmailFunc(ctx, tenantID, email)
	}
	return &iam.User{ID: "user_001", Username: "testuser", Email: email, TenantID: tenantID, Status: iam.UserStatusActive, CreatedAt: time.Now()}, nil
}

func (m *testMockUserClient) FindByUsername(ctx context.Context, tenantID, username string) (*iam.User, error) {
	if m.FindByUsernameFunc != nil {
		return m.FindByUsernameFunc(ctx, tenantID, username)
	}
	return &iam.User{ID: "user_001", Username: username, Email: username + "@example.com", TenantID: tenantID, Status: iam.UserStatusActive, CreatedAt: time.Now()}, nil
}

func (m *testMockUserClient) Update(ctx context.Context, tenantID, userID string, req *iam.UpdateUserRequest) (*iam.User, error) {
	if m.UpdateFunc != nil {
		return m.UpdateFunc(ctx, tenantID, userID, req)
	}
	return &iam.User{ID: userID, Username: "testuser", Email: "updated@example.com", TenantID: tenantID, Status: iam.UserStatusActive, CreatedAt: time.Now()}, nil
}

func (m *testMockUserClient) Delete(ctx context.Context, tenantID, userID string) error {
	if m.DeleteFunc != nil {
		return m.DeleteFunc(ctx, tenantID, userID)
	}
	return nil
}

func (m *testMockUserClient) List(ctx context.Context, tenantID string, req *iam.ListUsersRequest) (*iam.ListUsersResponse, error) {
	if m.ListFunc != nil {
		return m.ListFunc(ctx, tenantID, req)
	}
	return &iam.ListUsersResponse{
		Users:      []iam.User{{ID: "user_001", Username: "testuser", Email: "test@example.com", TenantID: tenantID, Status: iam.UserStatusActive, CreatedAt: time.Now()}},
		Pagination: iam.PaginationInfo{TotalCount: 1, HasMore: false},
	}, nil
}

func (m *testMockUserClient) AssignRole(ctx context.Context, tenantID, userID, roleID string) error {
	if m.AssignRoleFunc != nil {
		return m.AssignRoleFunc(ctx, tenantID, userID, roleID)
	}
	return nil
}

func (m *testMockUserClient) RevokeRole(ctx context.Context, tenantID, userID, roleID string) error {
	if m.RevokeRoleFunc != nil {
		return m.RevokeRoleFunc(ctx, tenantID, userID, roleID)
	}
	return nil
}

func (m *testMockUserClient) Suspend(ctx context.Context, tenantID, userID string) error {
	if m.SuspendFunc != nil {
		return m.SuspendFunc(ctx, tenantID, userID)
	}
	return nil
}

func (m *testMockUserClient) Unsuspend(ctx context.Context, tenantID, userID string) error {
	if m.UnsuspendFunc != nil {
		return m.UnsuspendFunc(ctx, tenantID, userID)
	}
	return nil
}

func (m *testMockUserClient) Approve(ctx context.Context, tenantID, userID string) error {
	if m.ApproveFunc != nil {
		return m.ApproveFunc(ctx, tenantID, userID)
	}
	return nil
}

func (m *testMockUserClient) Reject(ctx context.Context, tenantID, userID, reason string) error {
	if m.RejectFunc != nil {
		return m.RejectFunc(ctx, tenantID, userID, reason)
	}
	return nil
}

// testMockSessionClient implements iam.SessionClient for testing.
type testMockSessionClient struct {
	GetFunc          func(ctx context.Context, sessionID string) (*iam.Session, error)
	ListFunc         func(ctx context.Context, req *iam.ListSessionsRequest) (*iam.ListSessionsResponse, error)
	RevokeFunc       func(ctx context.Context, sessionID string) error
	RevokeAllFunc    func(ctx context.Context, tenantID, userID string) error
	RevokeOthersFunc func(ctx context.Context, keepSessionID string) error
	TouchFunc        func(ctx context.Context, sessionID string) error
}

func (m *testMockSessionClient) Get(ctx context.Context, sessionID string) (*iam.Session, error) {
	if m.GetFunc != nil {
		return m.GetFunc(ctx, sessionID)
	}
	return &iam.Session{ID: sessionID, UserID: "user_001", TenantID: "tenant_test", IPAddress: "127.0.0.1", UserAgent: "test-agent", CreatedAt: time.Now(), ExpiresAt: time.Now().Add(time.Hour)}, nil
}

func (m *testMockSessionClient) List(ctx context.Context, req *iam.ListSessionsRequest) (*iam.ListSessionsResponse, error) {
	if m.ListFunc != nil {
		return m.ListFunc(ctx, req)
	}
	return &iam.ListSessionsResponse{
		Sessions:   []iam.Session{{ID: "sess_001", UserID: "user_001", TenantID: "tenant_test", IPAddress: "127.0.0.1", CreatedAt: time.Now(), ExpiresAt: time.Now().Add(time.Hour)}},
		TotalCount: 1,
	}, nil
}

func (m *testMockSessionClient) Revoke(ctx context.Context, sessionID string) error {
	if m.RevokeFunc != nil {
		return m.RevokeFunc(ctx, sessionID)
	}
	return nil
}

func (m *testMockSessionClient) RevokeAll(ctx context.Context, tenantID, userID string) error {
	if m.RevokeAllFunc != nil {
		return m.RevokeAllFunc(ctx, tenantID, userID)
	}
	return nil
}

func (m *testMockSessionClient) RevokeOthers(ctx context.Context, keepSessionID string) error {
	if m.RevokeOthersFunc != nil {
		return m.RevokeOthersFunc(ctx, keepSessionID)
	}
	return nil
}

func (m *testMockSessionClient) Touch(ctx context.Context, sessionID string) error {
	if m.TouchFunc != nil {
		return m.TouchFunc(ctx, sessionID)
	}
	return nil
}

// testMockAppClient implements iam.AppClient for testing.
type testMockAppClient struct {
	RegisterFunc     func(ctx context.Context, tenantID string, req *iam.RegisterAppRequest) (*iam.Application, error)
	GetFunc          func(ctx context.Context, tenantID, clientID string) (*iam.Application, error)
	UpdateFunc       func(ctx context.Context, tenantID, clientID string, req *iam.UpdateAppRequest) (*iam.Application, error)
	DeleteFunc       func(ctx context.Context, tenantID, clientID string) error
	ListFunc         func(ctx context.Context, tenantID string) (*iam.ListAppsResponse, error)
	RotateSecretFunc func(ctx context.Context, tenantID, clientID string) (*iam.RotateSecretResponse, error)
}

func (m *testMockAppClient) Register(ctx context.Context, tenantID string, req *iam.RegisterAppRequest) (*iam.Application, error) {
	if m.RegisterFunc != nil {
		return m.RegisterFunc(ctx, tenantID, req)
	}
	return &iam.Application{ClientID: "app_new", Name: req.Name, TenantID: tenantID, ClientSecret: "secret_abc123", CreatedAt: time.Now()}, nil
}

func (m *testMockAppClient) Get(ctx context.Context, tenantID, clientID string) (*iam.Application, error) {
	if m.GetFunc != nil {
		return m.GetFunc(ctx, tenantID, clientID)
	}
	return &iam.Application{ClientID: clientID, Name: "Test App", TenantID: tenantID, CreatedAt: time.Now()}, nil
}

func (m *testMockAppClient) Update(ctx context.Context, tenantID, clientID string, req *iam.UpdateAppRequest) (*iam.Application, error) {
	if m.UpdateFunc != nil {
		return m.UpdateFunc(ctx, tenantID, clientID, req)
	}
	return &iam.Application{ClientID: clientID, Name: "Updated App", TenantID: tenantID, CreatedAt: time.Now()}, nil
}

func (m *testMockAppClient) Delete(ctx context.Context, tenantID, clientID string) error {
	if m.DeleteFunc != nil {
		return m.DeleteFunc(ctx, tenantID, clientID)
	}
	return nil
}

func (m *testMockAppClient) List(ctx context.Context, tenantID string) (*iam.ListAppsResponse, error) {
	if m.ListFunc != nil {
		return m.ListFunc(ctx, tenantID)
	}
	return &iam.ListAppsResponse{
		Applications: []iam.Application{{ClientID: "app_001", Name: "Test App", TenantID: tenantID, CreatedAt: time.Now()}},
		TotalCount:   1,
	}, nil
}

func (m *testMockAppClient) RotateSecret(ctx context.Context, tenantID, clientID string) (*iam.RotateSecretResponse, error) {
	if m.RotateSecretFunc != nil {
		return m.RotateSecretFunc(ctx, tenantID, clientID)
	}
	return &iam.RotateSecretResponse{ClientID: clientID, ClientSecret: "new_secret_xyz"}, nil
}

// testMockAuditClient implements iam.AuditClient for testing.
type testMockAuditClient struct {
	QueryFunc       func(ctx context.Context, filter *iam.AuditFilter) (*iam.AuditQueryResponse, error)
	GetFunc         func(ctx context.Context, tenantID, eventID string) (*iam.AuditEvent, error)
	DeleteFunc      func(ctx context.Context, req *iam.AuditDeleteRequest) error
	RecordFunc      func(ctx context.Context, event *iam.AuditEvent) (string, error)
	RecordAsyncFunc func(ctx context.Context, event *iam.AuditEvent) error
}

func (m *testMockAuditClient) Query(ctx context.Context, filter *iam.AuditFilter) (*iam.AuditQueryResponse, error) {
	if m.QueryFunc != nil {
		return m.QueryFunc(ctx, filter)
	}
	return &iam.AuditQueryResponse{
		Events: []iam.AuditEvent{
			{ID: "evt_001", TenantID: "tenant_test", Action: "user.login", EventType: iam.AuditEventTypeAuthentication, Outcome: iam.AuditOutcomeSuccess, Timestamp: time.Now()},
		},
		TotalCount: 1,
	}, nil
}

func (m *testMockAuditClient) Get(ctx context.Context, tenantID, eventID string) (*iam.AuditEvent, error) {
	if m.GetFunc != nil {
		return m.GetFunc(ctx, tenantID, eventID)
	}
	return &iam.AuditEvent{
		ID:        eventID,
		TenantID:  tenantID,
		Action:    "user.login",
		EventType: iam.AuditEventTypeAuthentication,
		Outcome:   iam.AuditOutcomeSuccess,
		Timestamp: time.Now(),
	}, nil
}

func (m *testMockAuditClient) Delete(ctx context.Context, req *iam.AuditDeleteRequest) error {
	if m.DeleteFunc != nil {
		return m.DeleteFunc(ctx, req)
	}
	return nil
}

func (m *testMockAuditClient) Record(ctx context.Context, event *iam.AuditEvent) (string, error) {
	if m.RecordFunc != nil {
		return m.RecordFunc(ctx, event)
	}
	return "evt_recorded", nil
}

func (m *testMockAuditClient) RecordAsync(ctx context.Context, event *iam.AuditEvent) error {
	if m.RecordAsyncFunc != nil {
		return m.RecordAsyncFunc(ctx, event)
	}
	return nil
}

// testMockGDPRClient implements iam.GDPRClient for testing.
type testMockGDPRClient struct {
	GrantConsentFunc      func(ctx context.Context, tenantID, userID string, req *iam.GrantConsentRequest) error
	WithdrawConsentFunc   func(ctx context.Context, tenantID, userID, purpose string) error
	GetConsentFunc        func(ctx context.Context, tenantID, userID, purpose string) (*iam.Consent, error)
	ListConsentsFunc      func(ctx context.Context, tenantID, userID string) (*iam.ListConsentsResponse, error)
	RequestExportFunc     func(ctx context.Context, tenantID, userID string, req *iam.ExportRequest) (*iam.DataExportRequest, error)
	GetExportStatusFunc   func(ctx context.Context, tenantID, userID, exportID string) (*iam.DataExportRequest, error)
	GenerateExportFunc    func(ctx context.Context, tenantID, userID, exportID string) (*iam.UserDataExport, error)
	RequestDeletionFunc   func(ctx context.Context, tenantID, userID string, req *iam.DeletionRequest) (*iam.DataDeletionRequest, error)
	CancelDeletionFunc    func(ctx context.Context, tenantID, userID, deletionID string) error
	GetDeletionStatusFunc func(ctx context.Context, tenantID, userID, deletionID string) (*iam.DataDeletionRequest, error)
}

func (m *testMockGDPRClient) GrantConsent(ctx context.Context, tenantID, userID string, req *iam.GrantConsentRequest) error {
	if m.GrantConsentFunc != nil {
		return m.GrantConsentFunc(ctx, tenantID, userID, req)
	}
	return nil
}

func (m *testMockGDPRClient) WithdrawConsent(ctx context.Context, tenantID, userID, purpose string) error {
	if m.WithdrawConsentFunc != nil {
		return m.WithdrawConsentFunc(ctx, tenantID, userID, purpose)
	}
	return nil
}

func (m *testMockGDPRClient) GetConsent(ctx context.Context, tenantID, userID, purpose string) (*iam.Consent, error) {
	if m.GetConsentFunc != nil {
		return m.GetConsentFunc(ctx, tenantID, userID, purpose)
	}
	return &iam.Consent{
		ID:         "consent_001",
		UserID:     userID,
		TenantID:   tenantID,
		Purpose:    purpose,
		Status:     iam.ConsentStatusGranted,
		LegalBasis: iam.LegalBasisConsent,
		GrantedAt:  time.Now(),
	}, nil
}

func (m *testMockGDPRClient) ListConsents(ctx context.Context, tenantID, userID string) (*iam.ListConsentsResponse, error) {
	if m.ListConsentsFunc != nil {
		return m.ListConsentsFunc(ctx, tenantID, userID)
	}
	return &iam.ListConsentsResponse{
		Consents: []iam.Consent{
			{ID: "consent_001", UserID: userID, TenantID: tenantID, Purpose: "analytics", Status: iam.ConsentStatusGranted, GrantedAt: time.Now()},
		},
		TotalCount: 1,
	}, nil
}

func (m *testMockGDPRClient) RequestExport(ctx context.Context, tenantID, userID string, req *iam.ExportRequest) (*iam.DataExportRequest, error) {
	if m.RequestExportFunc != nil {
		return m.RequestExportFunc(ctx, tenantID, userID, req)
	}
	return &iam.DataExportRequest{
		ID:          "export_001",
		UserID:      userID,
		TenantID:    tenantID,
		Status:      iam.ExportStatusPending,
		RequestedAt: time.Now(),
	}, nil
}

func (m *testMockGDPRClient) GetExportStatus(ctx context.Context, tenantID, userID, exportID string) (*iam.DataExportRequest, error) {
	if m.GetExportStatusFunc != nil {
		return m.GetExportStatusFunc(ctx, tenantID, userID, exportID)
	}
	return &iam.DataExportRequest{
		ID:          exportID,
		UserID:      userID,
		TenantID:    tenantID,
		Status:      iam.ExportStatusCompleted,
		RequestedAt: time.Now(),
	}, nil
}

func (m *testMockGDPRClient) GenerateExport(ctx context.Context, tenantID, userID, exportID string) (*iam.UserDataExport, error) {
	if m.GenerateExportFunc != nil {
		return m.GenerateExportFunc(ctx, tenantID, userID, exportID)
	}
	return &iam.UserDataExport{RequestID: exportID, UserID: userID}, nil
}

func (m *testMockGDPRClient) RequestDeletion(ctx context.Context, tenantID, userID string, req *iam.DeletionRequest) (*iam.DataDeletionRequest, error) {
	if m.RequestDeletionFunc != nil {
		return m.RequestDeletionFunc(ctx, tenantID, userID, req)
	}
	return &iam.DataDeletionRequest{
		ID:          "del_001",
		UserID:      userID,
		TenantID:    tenantID,
		Status:      iam.DeletionStatusPending,
		RequestedAt: time.Now(),
	}, nil
}

func (m *testMockGDPRClient) CancelDeletion(ctx context.Context, tenantID, userID, deletionID string) error {
	if m.CancelDeletionFunc != nil {
		return m.CancelDeletionFunc(ctx, tenantID, userID, deletionID)
	}
	return nil
}

func (m *testMockGDPRClient) GetDeletionStatus(ctx context.Context, tenantID, userID, deletionID string) (*iam.DataDeletionRequest, error) {
	if m.GetDeletionStatusFunc != nil {
		return m.GetDeletionStatusFunc(ctx, tenantID, userID, deletionID)
	}
	return &iam.DataDeletionRequest{
		ID:          deletionID,
		UserID:      userID,
		TenantID:    tenantID,
		Status:      iam.DeletionStatusPending,
		RequestedAt: time.Now(),
	}, nil
}

// testMockPEPClient implements iam.PEPClient for testing.
type testMockPEPClient struct {
	CheckFunc      func(ctx context.Context, req *iam.CheckRequest) (*iam.CheckResponse, error)
	CheckBatchFunc func(ctx context.Context, req *iam.CheckBatchRequest) (*iam.CheckBatchResponse, error)
}

func (m *testMockPEPClient) Check(ctx context.Context, req *iam.CheckRequest) (*iam.CheckResponse, error) {
	if m.CheckFunc != nil {
		return m.CheckFunc(ctx, req)
	}
	return &iam.CheckResponse{
		Allowed: true,
	}, nil
}

func (m *testMockPEPClient) CheckBatch(ctx context.Context, req *iam.CheckBatchRequest) (*iam.CheckBatchResponse, error) {
	if m.CheckBatchFunc != nil {
		return m.CheckBatchFunc(ctx, req)
	}
	results := make([]iam.CheckResponse, len(req.Requests))
	for i := range req.Requests {
		results[i] = iam.CheckResponse{Allowed: true}
	}
	return &iam.CheckBatchResponse{Results: results}, nil
}

// testMockPolicyClient implements iam.PolicyClient for testing.
type testMockPolicyClient struct {
	CreateFunc                  func(ctx context.Context, tenantID string, req *iam.CreatePolicyRequest) (*iam.Policy, error)
	GetFunc                     func(ctx context.Context, tenantID, policyName string) (*iam.Policy, error)
	UpdateFunc                  func(ctx context.Context, tenantID, policyName string, req *iam.UpdatePolicyRequest) (*iam.Policy, error)
	DeleteFunc                  func(ctx context.Context, tenantID, policyName string) error
	ListFunc                    func(ctx context.Context, tenantID string, req *iam.ListPoliciesRequest) (*iam.ListPoliciesResponse, error)
	ValidateFunc                func(ctx context.Context, tenantID string, policy *iam.Policy) (*iam.ValidationResult, error)
	TestFunc                    func(ctx context.Context, tenantID string, req *iam.TestPolicyRequest) (*iam.TestPolicyResponse, error)
	SimulateFunc                func(ctx context.Context, tenantID string, req *iam.SimulateRequest) (*iam.SimulateResponse, error)
	GetEffectivePermissionsFunc func(ctx context.Context, tenantID string, req *iam.EffectivePermissionsRequest) (*iam.EffectivePermissionsResponse, error)
}

func (m *testMockPolicyClient) Create(ctx context.Context, tenantID string, req *iam.CreatePolicyRequest) (*iam.Policy, error) {
	if m.CreateFunc != nil {
		return m.CreateFunc(ctx, tenantID, req)
	}
	return &iam.Policy{Name: req.Name, TenantID: tenantID, Description: req.Description, CreatedAt: time.Now()}, nil
}

func (m *testMockPolicyClient) Get(ctx context.Context, tenantID, policyName string) (*iam.Policy, error) {
	if m.GetFunc != nil {
		return m.GetFunc(ctx, tenantID, policyName)
	}
	return &iam.Policy{Name: policyName, TenantID: tenantID, Description: "Test policy", CreatedAt: time.Now()}, nil
}

func (m *testMockPolicyClient) Update(ctx context.Context, tenantID, policyName string, req *iam.UpdatePolicyRequest) (*iam.Policy, error) {
	if m.UpdateFunc != nil {
		return m.UpdateFunc(ctx, tenantID, policyName, req)
	}
	return &iam.Policy{Name: policyName, TenantID: tenantID, CreatedAt: time.Now()}, nil
}

func (m *testMockPolicyClient) Delete(ctx context.Context, tenantID, policyName string) error {
	if m.DeleteFunc != nil {
		return m.DeleteFunc(ctx, tenantID, policyName)
	}
	return nil
}

func (m *testMockPolicyClient) List(ctx context.Context, tenantID string, req *iam.ListPoliciesRequest) (*iam.ListPoliciesResponse, error) {
	if m.ListFunc != nil {
		return m.ListFunc(ctx, tenantID, req)
	}
	return &iam.ListPoliciesResponse{
		Policies: []iam.Policy{{Name: "test-policy", TenantID: tenantID, CreatedAt: time.Now()}},
	}, nil
}

func (m *testMockPolicyClient) Validate(ctx context.Context, tenantID string, policy *iam.Policy) (*iam.ValidationResult, error) {
	if m.ValidateFunc != nil {
		return m.ValidateFunc(ctx, tenantID, policy)
	}
	return &iam.ValidationResult{Valid: true}, nil
}

func (m *testMockPolicyClient) Test(ctx context.Context, tenantID string, req *iam.TestPolicyRequest) (*iam.TestPolicyResponse, error) {
	if m.TestFunc != nil {
		return m.TestFunc(ctx, tenantID, req)
	}
	return &iam.TestPolicyResponse{
		Results: []iam.TestCaseResult{{Name: "test1", Passed: true}},
	}, nil
}

func (m *testMockPolicyClient) Simulate(ctx context.Context, tenantID string, req *iam.SimulateRequest) (*iam.SimulateResponse, error) {
	if m.SimulateFunc != nil {
		return m.SimulateFunc(ctx, tenantID, req)
	}
	return &iam.SimulateResponse{Allowed: true}, nil
}

func (m *testMockPolicyClient) GetEffectivePermissions(ctx context.Context, tenantID string, req *iam.EffectivePermissionsRequest) (*iam.EffectivePermissionsResponse, error) {
	if m.GetEffectivePermissionsFunc != nil {
		return m.GetEffectivePermissionsFunc(ctx, tenantID, req)
	}
	return &iam.EffectivePermissionsResponse{
		Permissions: []iam.EffectivePermission{{Action: "read"}, {Action: "write"}},
	}, nil
}

// testMockInviteClient implements iam.InviteClient for testing.
type testMockInviteClient struct {
	GetInviteFunc           func(ctx context.Context, tenantID, token string) (*iam.InviteMetadata, error)
	ActivateFIDO2BeginFunc  func(ctx context.Context, tenantID, token string) (map[string]interface{}, error)
	ActivateFIDO2FinishFunc func(ctx context.Context, tenantID, token string, credential map[string]interface{}) (*iam.ActivationResponse, error)
	ActivatePIVFunc         func(ctx context.Context, tenantID, token, certPEM string) (*iam.ActivationResponse, error)
}

func (m *testMockInviteClient) GetInvite(ctx context.Context, tenantID, token string) (*iam.InviteMetadata, error) {
	if m.GetInviteFunc != nil {
		return m.GetInviteFunc(ctx, tenantID, token)
	}
	return &iam.InviteMetadata{UserID: "user_001", Email: "invitee@example.com", AuthMethods: []string{"fido2", "piv"}, ExpiresAt: time.Now().Add(24 * time.Hour)}, nil
}

func (m *testMockInviteClient) ActivateFIDO2Begin(ctx context.Context, tenantID, token string) (map[string]interface{}, error) {
	if m.ActivateFIDO2BeginFunc != nil {
		return m.ActivateFIDO2BeginFunc(ctx, tenantID, token)
	}
	return map[string]interface{}{"publicKey": map[string]interface{}{}}, nil
}

func (m *testMockInviteClient) ActivateFIDO2Finish(ctx context.Context, tenantID, token string, credential map[string]interface{}) (*iam.ActivationResponse, error) {
	if m.ActivateFIDO2FinishFunc != nil {
		return m.ActivateFIDO2FinishFunc(ctx, tenantID, token, credential)
	}
	return &iam.ActivationResponse{UserID: "user_001", Message: "activated"}, nil
}

func (m *testMockInviteClient) ActivatePIV(ctx context.Context, tenantID, token, certPEM string) (*iam.ActivationResponse, error) {
	if m.ActivatePIVFunc != nil {
		return m.ActivatePIVFunc(ctx, tenantID, token, certPEM)
	}
	return &iam.ActivationResponse{UserID: "user_001", Message: "activated"}, nil
}

// testMockIAMClient implements iam.Client for testing.
type testMockIAMClient struct {
	adminClient   *testMockAdminClient
	tenantClient  *testMockTenantClient
	userClient    *testMockUserClient
	roleClient    *testMockRoleClient
	sessionClient *testMockSessionClient
	appClient     *testMockAppClient
	auditClient   *testMockAuditClient
	gdprClient    *testMockGDPRClient
	pepClient     *testMockPEPClient
	policyClient  *testMockPolicyClient
	inviteClient  *testMockInviteClient

	// Federation methods
	CreateProviderFunc     func(ctx context.Context, opts *cli.CreateProviderOptions) (*cli.Provider, error)
	GetProviderFunc        func(ctx context.Context, tenantID, providerID string) (*cli.Provider, error)
	UpdateProviderFunc     func(ctx context.Context, opts *cli.UpdateProviderOptions) (*cli.Provider, error)
	DeleteProviderFunc     func(ctx context.Context, tenantID, providerID string) error
	ListProvidersFunc      func(ctx context.Context, tenantID string) ([]*cli.Provider, error)
	LinkIdentityFunc       func(ctx context.Context, opts *cli.LinkIdentityOptions) (*cli.FederatedIdentity, error)
	UnlinkIdentityFunc     func(ctx context.Context, tenantID, userID, providerID string) error
	ListUserIdentitiesFunc func(ctx context.Context, tenantID, userID string) ([]*cli.FederatedIdentity, error)

	// MFA methods
	GetMFAStatusFunc          func(ctx context.Context, tenantID, userID string) (*cli.MFAStatus, error)
	BeginMFAEnrollmentFunc    func(ctx context.Context, tenantID, userID, method string) (*cli.MFAEnrollment, error)
	CompleteMFAEnrollmentFunc func(ctx context.Context, enrollmentID, code string) error
	DisableMFAFunc            func(ctx context.Context, tenantID, userID string) error
	GetBackupCodeStatusFunc   func(ctx context.Context, tenantID, userID string) (*cli.BackupCodeStatus, error)
	GenerateBackupCodesFunc   func(ctx context.Context, tenantID, userID string) ([]string, error)

	// WebAuthn credential administration methods
	ListWebAuthnCredentialsFunc   func(ctx context.Context, tenantID, userID string) ([]*cli.WebAuthnCredential, error)
	DeleteWebAuthnCredentialFunc  func(ctx context.Context, tenantID, userID, credentialID string) error
	DeleteWebAuthnCredentialsFunc func(ctx context.Context, tenantID, userID string) (int, error)
}

func (m *testMockIAMClient) Tenant() iam.TenantClient   { return m.tenantClient }
func (m *testMockIAMClient) User() iam.UserClient       { return m.userClient }
func (m *testMockIAMClient) Role() iam.RoleClient       { return m.roleClient }
func (m *testMockIAMClient) Policy() iam.PolicyClient   { return m.policyClient }
func (m *testMockIAMClient) PEP() iam.PEPClient         { return m.pepClient }
func (m *testMockIAMClient) App() iam.AppClient         { return m.appClient }
func (m *testMockIAMClient) Session() iam.SessionClient { return m.sessionClient }
func (m *testMockIAMClient) Audit() iam.AuditClient     { return m.auditClient }
func (m *testMockIAMClient) GDPR() iam.GDPRClient       { return m.gdprClient }
func (m *testMockIAMClient) Admin() iam.AdminClient     { return m.adminClient }
func (m *testMockIAMClient) Invite() iam.InviteClient   { return m.inviteClient }
func (m *testMockIAMClient) Close() error               { return nil }

func (m *testMockIAMClient) CreateProvider(ctx context.Context, opts *cli.CreateProviderOptions) (*cli.Provider, error) {
	if m.CreateProviderFunc != nil {
		return m.CreateProviderFunc(ctx, opts)
	}
	return &cli.Provider{ID: "provider_new", Name: opts.Name, Type: opts.Type, TenantID: opts.TenantID, Enabled: true}, nil
}

func (m *testMockIAMClient) GetProvider(ctx context.Context, tenantID, providerID string) (*cli.Provider, error) {
	if m.GetProviderFunc != nil {
		return m.GetProviderFunc(ctx, tenantID, providerID)
	}
	return &cli.Provider{ID: providerID, Name: "Test Provider", Type: "oidc", TenantID: tenantID, Enabled: true}, nil
}

func (m *testMockIAMClient) UpdateProvider(ctx context.Context, opts *cli.UpdateProviderOptions) (*cli.Provider, error) {
	if m.UpdateProviderFunc != nil {
		return m.UpdateProviderFunc(ctx, opts)
	}
	return &cli.Provider{ID: opts.ProviderID, Name: "Updated Provider", Type: "oidc", TenantID: opts.TenantID, Enabled: true}, nil
}

func (m *testMockIAMClient) DeleteProvider(ctx context.Context, tenantID, providerID string) error {
	if m.DeleteProviderFunc != nil {
		return m.DeleteProviderFunc(ctx, tenantID, providerID)
	}
	return nil
}

func (m *testMockIAMClient) ListProviders(ctx context.Context, tenantID string) ([]*cli.Provider, error) {
	if m.ListProvidersFunc != nil {
		return m.ListProvidersFunc(ctx, tenantID)
	}
	return []*cli.Provider{{ID: "provider_001", Name: "Test Provider", Type: "oidc", TenantID: tenantID, Enabled: true}}, nil
}

func (m *testMockIAMClient) LinkIdentity(ctx context.Context, opts *cli.LinkIdentityOptions) (*cli.FederatedIdentity, error) {
	if m.LinkIdentityFunc != nil {
		return m.LinkIdentityFunc(ctx, opts)
	}
	return &cli.FederatedIdentity{ProviderID: opts.ProviderID, ExternalID: opts.AuthCode, UserID: opts.UserID}, nil
}

func (m *testMockIAMClient) UnlinkIdentity(ctx context.Context, tenantID, userID, providerID string) error {
	if m.UnlinkIdentityFunc != nil {
		return m.UnlinkIdentityFunc(ctx, tenantID, userID, providerID)
	}
	return nil
}

func (m *testMockIAMClient) ListUserIdentities(ctx context.Context, tenantID, userID string) ([]*cli.FederatedIdentity, error) {
	if m.ListUserIdentitiesFunc != nil {
		return m.ListUserIdentitiesFunc(ctx, tenantID, userID)
	}
	return []*cli.FederatedIdentity{{ProviderID: "provider_001", ExternalID: "ext_001", UserID: userID}}, nil
}

func (m *testMockIAMClient) GetMFAStatus(ctx context.Context, tenantID, userID string) (*cli.MFAStatus, error) {
	if m.GetMFAStatusFunc != nil {
		return m.GetMFAStatusFunc(ctx, tenantID, userID)
	}
	return &cli.MFAStatus{Enabled: true, Method: "totp", BackupCodesRemaining: 8}, nil
}

func (m *testMockIAMClient) BeginMFAEnrollment(ctx context.Context, tenantID, userID, method string) (*cli.MFAEnrollment, error) {
	if m.BeginMFAEnrollmentFunc != nil {
		return m.BeginMFAEnrollmentFunc(ctx, tenantID, userID, method)
	}
	return &cli.MFAEnrollment{EnrollmentID: "enroll_001", Secret: "JBSWY3DPEHPK3PXP", ProvisioningURI: "otpauth://totp/QuicBaaS:user@example.com?secret=JBSWY3DPEHPK3PXP"}, nil
}

func (m *testMockIAMClient) CompleteMFAEnrollment(ctx context.Context, enrollmentID, code string) error {
	if m.CompleteMFAEnrollmentFunc != nil {
		return m.CompleteMFAEnrollmentFunc(ctx, enrollmentID, code)
	}
	return nil
}

func (m *testMockIAMClient) ListWebAuthnCredentials(ctx context.Context, tenantID, userID string) ([]*cli.WebAuthnCredential, error) {
	if m.ListWebAuthnCredentialsFunc != nil {
		return m.ListWebAuthnCredentialsFunc(ctx, tenantID, userID)
	}
	return nil, nil
}

func (m *testMockIAMClient) DeleteWebAuthnCredential(ctx context.Context, tenantID, userID, credentialID string) error {
	if m.DeleteWebAuthnCredentialFunc != nil {
		return m.DeleteWebAuthnCredentialFunc(ctx, tenantID, userID, credentialID)
	}
	return nil
}

func (m *testMockIAMClient) DeleteWebAuthnCredentials(ctx context.Context, tenantID, userID string) (int, error) {
	if m.DeleteWebAuthnCredentialsFunc != nil {
		return m.DeleteWebAuthnCredentialsFunc(ctx, tenantID, userID)
	}
	return 0, nil
}

func (m *testMockIAMClient) DisableMFA(ctx context.Context, tenantID, userID string) error {
	if m.DisableMFAFunc != nil {
		return m.DisableMFAFunc(ctx, tenantID, userID)
	}
	return nil
}

func (m *testMockIAMClient) GetBackupCodeStatus(ctx context.Context, tenantID, userID string) (*cli.BackupCodeStatus, error) {
	if m.GetBackupCodeStatusFunc != nil {
		return m.GetBackupCodeStatusFunc(ctx, tenantID, userID)
	}
	return &cli.BackupCodeStatus{TotalCodes: 10, RemainingCodes: 8}, nil
}

func (m *testMockIAMClient) GenerateBackupCodes(ctx context.Context, tenantID, userID string) ([]string, error) {
	if m.GenerateBackupCodesFunc != nil {
		return m.GenerateBackupCodesFunc(ctx, tenantID, userID)
	}
	return []string{"AAAA-BBBB", "CCCC-DDDD", "EEEE-FFFF", "GGGG-HHHH"}, nil
}

// newTestMockIAMClient creates a fully populated mock IAM client with default implementations.
func newTestMockIAMClient() *testMockIAMClient {
	return &testMockIAMClient{
		adminClient:   &testMockAdminClient{},
		tenantClient:  &testMockTenantClient{},
		userClient:    &testMockUserClient{},
		roleClient:    &testMockRoleClient{},
		sessionClient: &testMockSessionClient{},
		appClient:     &testMockAppClient{},
		auditClient:   &testMockAuditClient{},
		gdprClient:    &testMockGDPRClient{},
		pepClient:     &testMockPEPClient{},
		policyClient:  &testMockPolicyClient{},
		inviteClient:  &testMockInviteClient{},
	}
}
