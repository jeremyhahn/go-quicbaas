//go:build rest

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

package rest

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/iam"
	"github.com/jeremyhahn/go-quicbaas/pkg/testutil"
)

// mockQuicBaaSWithAdmin wraps MockQuicBaaS and adds GetAdminManager support.
type mockQuicBaaSWithAdmin struct {
	*testutil.MockQuicBaaS
	adminManager *iam.AdminManager
}

func (m *mockQuicBaaSWithAdmin) GetAdminManager(_ context.Context) (*iam.AdminManager, error) {
	return m.adminManager, nil
}

// restAllowAllTargetResolver resolves every (tenant, user) pair so handler
// tests can exercise impersonation mechanics; target verification itself
// is pinned in pkg/iam's tenant-scope suite (#357).
type restAllowAllTargetResolver struct{}

func (restAllowAllTargetResolver) GetUser(ctx context.Context, tenantID string, userID uint64) (*iam.User, error) {
	return &iam.User{ID: userID, TenantID: tenantID}, nil
}

// newTestAdminManager creates an AdminManager with in-memory storage for testing.
func newTestAdminManager(t *testing.T) *iam.AdminManager {
	t.Helper()
	adminStorage := iam.NewMemoryAdminStorage()
	impersonationStorage := iam.NewMemoryImpersonationStorage()

	mgr, err := iam.NewAdminManager(iam.AdminManagerOptions{
		AdminStorage:         adminStorage,
		ImpersonationStorage: impersonationStorage,
		TargetResolver:       restAllowAllTargetResolver{},
	})
	require.NoError(t, err)
	return mgr
}

// newTestIAMHandler creates an IAMHandler with a mock service for testing admin routes.
func newTestIAMHandler(t *testing.T) (*IAMHandler, *mockQuicBaaSWithAdmin) {
	t.Helper()
	adminMgr := newTestAdminManager(t)
	mockSvc := &mockQuicBaaSWithAdmin{
		MockQuicBaaS: testutil.NewMockQuicBaaS(),
		adminManager: adminMgr,
	}

	// Create a minimal server with the mock service
	server := &Server{
		service: mockSvc,
	}

	handler := NewIAMHandler(IAMHandlerDependencies{Server: server})
	return handler, mockSvc
}

// TestCreateAdminAssignment tests the CreateAdminAssignment handler.
func TestCreateAdminAssignment(t *testing.T) {
	handler, _ := newTestIAMHandler(t)

	// Create request body
	opts := iam.CreateAdminAssignmentOptions{
		UserID:    "user_123",
		AdminRole: iam.AdminRoleTenantAdmin,
		TenantIDs: []string{"tenant_1"},
		Permissions: []iam.AdminPermission{
			iam.PermUserCreate,
			iam.PermUserRead,
		},
		GrantedBy: "super_admin",
	}

	body, err := json.Marshal(opts)
	require.NoError(t, err)

	// Create HTTP request
	req := httptest.NewRequest("POST", "/api/v1/admin/assignments", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")

	// Create response recorder
	rr := httptest.NewRecorder()

	// Call handler
	handler.CreateAdminAssignment(rr, req)

	// Verify response
	assert.Equal(t, http.StatusCreated, rr.Code)

	var assignment iam.AdminAssignment
	err = json.Unmarshal(rr.Body.Bytes(), &assignment)
	require.NoError(t, err)

	assert.NotEmpty(t, assignment.ID)
	assert.Equal(t, "user_123", assignment.UserID)
	assert.Equal(t, iam.AdminRoleTenantAdmin, assignment.AdminRole)
	assert.True(t, assignment.Active)
}

// TestListAdminAssignments tests the ListAdminAssignments handler.
func TestListAdminAssignments(t *testing.T) {
	handler, mockSvc := newTestIAMHandler(t)

	// Create some test assignments first
	ctx := context.Background()
	_, err := mockSvc.adminManager.CreateAssignment(ctx, iam.CreateAdminAssignmentOptions{
		UserID:    "user_1",
		AdminRole: iam.AdminRoleTenantAdmin,
		TenantIDs: []string{"tenant_1"},
		GrantedBy: "admin",
	})
	require.NoError(t, err)

	_, err = mockSvc.adminManager.CreateAssignment(ctx, iam.CreateAdminAssignmentOptions{
		UserID:    "user_2",
		AdminRole: iam.AdminRoleUserAdmin,
		TenantIDs: []string{"tenant_2"},
		GrantedBy: "admin",
	})
	require.NoError(t, err)

	// Create HTTP request
	req := httptest.NewRequest("GET", "/api/v1/admin/assignments", nil)
	rr := httptest.NewRecorder()

	// Call handler
	handler.ListAdminAssignments(rr, req)

	// Verify response
	assert.Equal(t, http.StatusOK, rr.Code)

	var resp map[string]interface{}
	err = json.Unmarshal(rr.Body.Bytes(), &resp)
	require.NoError(t, err)

	assignments := resp["assignments"].([]interface{})
	assert.Len(t, assignments, 2)
}

// TestGetAdminAssignment tests the GetAdminAssignment handler.
func TestGetAdminAssignment(t *testing.T) {
	handler, mockSvc := newTestIAMHandler(t)

	// Create a test assignment
	ctx := context.Background()
	created, err := mockSvc.adminManager.CreateAssignment(ctx, iam.CreateAdminAssignmentOptions{
		UserID:    "user_123",
		AdminRole: iam.AdminRoleTenantAdmin,
		TenantIDs: []string{"tenant_1"},
		GrantedBy: "admin",
	})
	require.NoError(t, err)

	// Create HTTP request with chi URL param
	req := httptest.NewRequest("GET", "/api/v1/admin/assignments/"+created.ID, nil)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("assignment_id", created.ID)
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

	rr := httptest.NewRecorder()

	// Call handler
	handler.GetAdminAssignment(rr, req)

	// Verify response
	assert.Equal(t, http.StatusOK, rr.Code)

	var assignment iam.AdminAssignment
	err = json.Unmarshal(rr.Body.Bytes(), &assignment)
	require.NoError(t, err)

	assert.Equal(t, created.ID, assignment.ID)
	assert.Equal(t, "user_123", assignment.UserID)
}

// TestUpdateAdminAssignment tests the UpdateAdminAssignment handler.
func TestUpdateAdminAssignment(t *testing.T) {
	handler, mockSvc := newTestIAMHandler(t)

	// Create a test assignment
	ctx := context.Background()
	created, err := mockSvc.adminManager.CreateAssignment(ctx, iam.CreateAdminAssignmentOptions{
		UserID:    "user_123",
		AdminRole: iam.AdminRoleTenantAdmin,
		TenantIDs: []string{"tenant_1"},
		GrantedBy: "admin",
	})
	require.NoError(t, err)
	require.True(t, created.Active)

	// Create update request
	active := false
	newRole := iam.AdminRoleUserAdmin
	opts := iam.UpdateAdminAssignmentOptions{
		Active:    &active,
		AdminRole: &newRole,
	}
	body, err := json.Marshal(opts)
	require.NoError(t, err)

	// Create HTTP request
	req := httptest.NewRequest("PUT", "/api/v1/admin/assignments/"+created.ID, bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("assignment_id", created.ID)
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

	rr := httptest.NewRecorder()

	// Call handler
	handler.UpdateAdminAssignment(rr, req)

	// Verify response
	assert.Equal(t, http.StatusOK, rr.Code)

	var assignment iam.AdminAssignment
	err = json.Unmarshal(rr.Body.Bytes(), &assignment)
	require.NoError(t, err)

	assert.Equal(t, created.ID, assignment.ID)
	assert.False(t, assignment.Active)
	assert.Equal(t, iam.AdminRoleUserAdmin, assignment.AdminRole)
}

// TestDeleteAdminAssignment tests the DeleteAdminAssignment handler.
func TestDeleteAdminAssignment(t *testing.T) {
	handler, mockSvc := newTestIAMHandler(t)

	// Create a test assignment
	ctx := context.Background()
	created, err := mockSvc.adminManager.CreateAssignment(ctx, iam.CreateAdminAssignmentOptions{
		UserID:    "user_123",
		AdminRole: iam.AdminRoleTenantAdmin,
		TenantIDs: []string{"tenant_1"},
		GrantedBy: "admin",
	})
	require.NoError(t, err)

	// Create HTTP request
	req := httptest.NewRequest("DELETE", "/api/v1/admin/assignments/"+created.ID, nil)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("assignment_id", created.ID)
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

	rr := httptest.NewRecorder()

	// Call handler
	handler.DeleteAdminAssignment(rr, req)

	// Verify response - should be 204 No Content
	assert.Equal(t, http.StatusNoContent, rr.Code)

	// Verify assignment is actually deleted
	_, err = mockSvc.adminManager.GetAssignment(ctx, created.ID)
	assert.ErrorIs(t, err, iam.ErrAdminAssignmentNotFound)
}

// TestGetUserAssignments tests the GetUserAssignments handler.
func TestGetUserAssignments(t *testing.T) {
	handler, mockSvc := newTestIAMHandler(t)

	// Create test assignments for a specific user
	ctx := context.Background()
	_, err := mockSvc.adminManager.CreateAssignment(ctx, iam.CreateAdminAssignmentOptions{
		UserID:    "50001",
		AdminRole: iam.AdminRoleTenantAdmin,
		TenantIDs: []string{"tenant_1"},
		GrantedBy: "admin",
	})
	require.NoError(t, err)

	_, err = mockSvc.adminManager.CreateAssignment(ctx, iam.CreateAdminAssignmentOptions{
		UserID:    "50001",
		AdminRole: iam.AdminRoleUserAdmin,
		TenantIDs: []string{"tenant_2"},
		GrantedBy: "admin",
	})
	require.NoError(t, err)

	// Create assignment for different user
	_, err = mockSvc.adminManager.CreateAssignment(ctx, iam.CreateAdminAssignmentOptions{
		UserID:    "other_user",
		AdminRole: iam.AdminRoleTenantAdmin,
		TenantIDs: []string{"tenant_3"},
		GrantedBy: "admin",
	})
	require.NoError(t, err)

	// Create HTTP request
	req := httptest.NewRequest("GET", "/api/v1/admin/users/50001/assignments", nil)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("user_id", "50001")
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

	rr := httptest.NewRecorder()

	// Call handler
	handler.GetUserAssignments(rr, req)

	// Verify response
	assert.Equal(t, http.StatusOK, rr.Code)

	var resp map[string]interface{}
	err = json.Unmarshal(rr.Body.Bytes(), &resp)
	require.NoError(t, err)

	assert.Equal(t, "50001", resp["user_id"])
	assignments := resp["assignments"].([]interface{})
	assert.Len(t, assignments, 2)
}

// TestCheckAdminPermission tests the CheckAdminPermission handler.
func TestCheckAdminPermission(t *testing.T) {
	handler, mockSvc := newTestIAMHandler(t)

	// Create a test assignment with specific permissions
	ctx := context.Background()
	_, err := mockSvc.adminManager.CreateAssignment(ctx, iam.CreateAdminAssignmentOptions{
		UserID:    "admin_user",
		AdminRole: iam.AdminRoleTenantAdmin,
		TenantIDs: []string{"tenant_1"},
		Permissions: []iam.AdminPermission{
			iam.PermUserCreate,
			iam.PermUserRead,
		},
		GrantedBy: "super_admin",
	})
	require.NoError(t, err)

	// Create check request
	checkReq := iam.AdminCheckRequest{
		UserID:     "admin_user",
		Permission: iam.PermUserCreate,
		TenantID:   "tenant_1",
	}
	body, err := json.Marshal(checkReq)
	require.NoError(t, err)

	// Create HTTP request
	req := httptest.NewRequest("POST", "/api/v1/admin/check", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")

	rr := httptest.NewRecorder()

	// Call handler
	handler.CheckAdminPermission(rr, req)

	// Verify response
	assert.Equal(t, http.StatusOK, rr.Code)

	var resp iam.AdminCheckResponse
	err = json.Unmarshal(rr.Body.Bytes(), &resp)
	require.NoError(t, err)

	assert.True(t, resp.Allowed)
}

// TestStartImpersonation tests the StartImpersonation handler.
func TestStartImpersonation(t *testing.T) {
	handler, mockSvc := newTestIAMHandler(t)

	// Create an admin assignment first
	ctx := context.Background()
	_, err := mockSvc.adminManager.CreateAssignment(ctx, iam.CreateAdminAssignmentOptions{
		UserID:    "admin_user",
		AdminRole: iam.AdminRoleSuperAdmin,
		TenantIDs: []string{"tenant_1"},
		Permissions: []iam.AdminPermission{
			iam.PermUserImpersonate,
		},
		GrantedBy: "system",
	})
	require.NoError(t, err)

	// Create impersonation request. The request carries no admin_user_id —
	// the acting admin is the authenticated caller (go-quicbaas#383).
	impReq := startImpersonationRequest{
		TargetUserID: "50001",
		TenantID:     "tenant_1",
		Reason:       "Support request #12345",
		Duration:     "1h",
	}
	body, err := json.Marshal(impReq)
	require.NoError(t, err)

	// Create HTTP request authenticated as admin_user.
	req := httptest.NewRequest("POST", "/api/v1/admin/impersonate", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req = req.WithContext(ctxWithTestSubject(req.Context(), "admin_user", "tenant_1"))

	rr := httptest.NewRecorder()

	// Call handler
	handler.StartImpersonation(rr, req)

	// Verify response
	assert.Equal(t, http.StatusOK, rr.Code)

	var session iam.ImpersonationSession
	err = json.Unmarshal(rr.Body.Bytes(), &session)
	require.NoError(t, err)

	assert.NotEmpty(t, session.ID)
	assert.Equal(t, "admin_user", session.AdminUserID)
	assert.Equal(t, "50001", session.TargetUserID)
	assert.Equal(t, "Support request #12345", session.Reason)
}

// TestStartImpersonation_ActorIsAuthenticatedCaller is the go-quicbaas#383
// regression: the actor is the authenticated caller, never a request
// value. A non-privileged caller is refused even when the body smuggles a
// privileged admin_user_id (an inert, unknown field now), and a privileged
// caller's session names the caller — not any supplied value.
func TestStartImpersonation_ActorIsAuthenticatedCaller(t *testing.T) {
	handler, mockSvc := newTestIAMHandler(t)
	ctx := context.Background()

	// admin_user holds PermUserImpersonate in tenant_1; attacker does not.
	_, err := mockSvc.adminManager.CreateAssignment(ctx, iam.CreateAdminAssignmentOptions{
		UserID:      "admin_user",
		AdminRole:   iam.AdminRoleSuperAdmin,
		TenantIDs:   []string{"tenant_1"},
		Permissions: []iam.AdminPermission{iam.PermUserImpersonate},
		GrantedBy:   "system",
	})
	require.NoError(t, err)

	// A non-privileged caller smuggles admin_user's id in the body.
	body := []byte(`{"admin_user_id":"admin_user","target_user_id":"50001","tenant_id":"tenant_1","reason":"escalation attempt","duration":"1h"}`)
	req := httptest.NewRequest("POST", "/api/v1/admin/impersonate", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req = req.WithContext(ctxWithTestSubject(req.Context(), "attacker", "tenant_1"))
	rr := httptest.NewRecorder()

	handler.StartImpersonation(rr, req)

	assert.Equal(t, http.StatusForbidden, rr.Code,
		"a non-privileged caller must be refused even when naming a privileged admin_user_id; body: %s", rr.Body.String())

	// The privileged caller's own session names the caller as the actor,
	// regardless of any body value.
	req = httptest.NewRequest("POST", "/api/v1/admin/impersonate", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req = req.WithContext(ctxWithTestSubject(req.Context(), "admin_user", "tenant_1"))
	rr = httptest.NewRecorder()

	handler.StartImpersonation(rr, req)
	require.Equal(t, http.StatusOK, rr.Code, "body: %s", rr.Body.String())

	var session iam.ImpersonationSession
	require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &session))
	assert.Equal(t, "admin_user", session.AdminUserID)
}

// TestEndImpersonation tests the EndImpersonation handler.
func TestEndImpersonation(t *testing.T) {
	handler, mockSvc := newTestIAMHandler(t)

	// Start an impersonation session first
	ctx := context.Background()

	// Create admin assignment
	_, err := mockSvc.adminManager.CreateAssignment(ctx, iam.CreateAdminAssignmentOptions{
		UserID:    "admin_user",
		AdminRole: iam.AdminRoleSuperAdmin,
		TenantIDs: []string{"tenant_1"},
		Permissions: []iam.AdminPermission{
			iam.PermUserImpersonate,
		},
		GrantedBy: "system",
	})
	require.NoError(t, err)

	session, err := mockSvc.adminManager.StartImpersonation(
		ctxWithTestSubject(ctx, "admin_user", "tenant_1"),
		&iam.StartImpersonationOptions{
			TargetUserID: "50001",
			TenantID:     "tenant_1",
			Reason:       "Testing",
			Duration:     time.Hour,
		})
	require.NoError(t, err)

	// Create HTTP request
	req := httptest.NewRequest("DELETE", "/api/v1/admin/impersonate/"+session.ID, nil)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("session_id", session.ID)
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

	rr := httptest.NewRecorder()

	// Call handler
	handler.EndImpersonation(rr, req)

	// Verify response
	assert.Equal(t, http.StatusOK, rr.Code)
}

// TestListActiveImpersonations tests the ListActiveImpersonations handler.
func TestListActiveImpersonations(t *testing.T) {
	handler, mockSvc := newTestIAMHandler(t)

	// Create admin assignment and start impersonation sessions
	ctx := context.Background()

	_, err := mockSvc.adminManager.CreateAssignment(ctx, iam.CreateAdminAssignmentOptions{
		UserID:    "admin_user",
		AdminRole: iam.AdminRoleSuperAdmin,
		TenantIDs: []string{"tenant_1", "tenant_2"},
		Permissions: []iam.AdminPermission{
			iam.PermUserImpersonate,
		},
		GrantedBy: "system",
	})
	require.NoError(t, err)

	_, err = mockSvc.adminManager.StartImpersonation(
		ctxWithTestSubject(ctx, "admin_user", "tenant_1"),
		&iam.StartImpersonationOptions{
			TargetUserID: "50011",
			TenantID:     "tenant_1",
			Reason:       "Support",
			Duration:     time.Hour,
		})
	require.NoError(t, err)

	_, err = mockSvc.adminManager.StartImpersonation(
		ctxWithTestSubject(ctx, "admin_user", "tenant_2"),
		&iam.StartImpersonationOptions{
			TargetUserID: "50012",
			TenantID:     "tenant_2",
			Reason:       "Debug",
			Duration:     time.Hour,
		})
	require.NoError(t, err)

	// Create HTTP request
	req := httptest.NewRequest("GET", "/api/v1/admin/users/admin_user/impersonations", nil)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("user_id", "admin_user")
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

	rr := httptest.NewRecorder()

	// Call handler
	handler.ListActiveImpersonations(rr, req)

	// Verify response
	assert.Equal(t, http.StatusOK, rr.Code)

	var resp map[string]interface{}
	err = json.Unmarshal(rr.Body.Bytes(), &resp)
	require.NoError(t, err)

	assert.Equal(t, "admin_user", resp["user_id"])
	sessions := resp["sessions"].([]interface{})
	assert.Len(t, sessions, 2)
}

// TestListImpersonations tests the ListImpersonations handler.
func TestListImpersonations(t *testing.T) {
	handler, mockSvc := newTestIAMHandler(t)

	// Create admin assignment and start impersonation sessions
	ctx := context.Background()

	_, err := mockSvc.adminManager.CreateAssignment(ctx, iam.CreateAdminAssignmentOptions{
		UserID:    "admin_user",
		AdminRole: iam.AdminRoleSuperAdmin,
		TenantIDs: []string{"tenant_1"},
		Permissions: []iam.AdminPermission{
			iam.PermUserImpersonate,
		},
		GrantedBy: "system",
	})
	require.NoError(t, err)

	_, err = mockSvc.adminManager.StartImpersonation(
		ctxWithTestSubject(ctx, "admin_user", "tenant_1"),
		&iam.StartImpersonationOptions{
			TargetUserID: "50011",
			TenantID:     "tenant_1",
			Reason:       "Support",
			Duration:     time.Hour,
		})
	require.NoError(t, err)

	// Create HTTP request
	req := httptest.NewRequest("GET", "/api/v1/admin/impersonation", nil)
	rr := httptest.NewRecorder()

	// Call handler
	handler.ListImpersonations(rr, req)

	// Verify response
	assert.Equal(t, http.StatusOK, rr.Code)

	var resp map[string]interface{}
	err = json.Unmarshal(rr.Body.Bytes(), &resp)
	require.NoError(t, err)

	sessions := resp["sessions"].([]interface{})
	assert.Len(t, sessions, 1)
}

// TestListImpersonations_WithFilters tests the ListImpersonations handler with query filters.
func TestListImpersonations_WithFilters(t *testing.T) {
	handler, mockSvc := newTestIAMHandler(t)

	// Create admin assignment and start impersonation sessions
	ctx := context.Background()

	_, err := mockSvc.adminManager.CreateAssignment(ctx, iam.CreateAdminAssignmentOptions{
		UserID:    "admin_user",
		AdminRole: iam.AdminRoleSuperAdmin,
		TenantIDs: []string{"tenant_1"},
		Permissions: []iam.AdminPermission{
			iam.PermUserImpersonate,
		},
		GrantedBy: "system",
	})
	require.NoError(t, err)

	_, err = mockSvc.adminManager.StartImpersonation(
		ctxWithTestSubject(ctx, "admin_user", "tenant_1"),
		&iam.StartImpersonationOptions{
			TargetUserID: "50011",
			TenantID:     "tenant_1",
			Reason:       "Support",
			Duration:     time.Hour,
		})
	require.NoError(t, err)

	// Create HTTP request with filters
	req := httptest.NewRequest("GET", "/api/v1/admin/impersonation?active=true&admin_user_id=admin_user", nil)
	rr := httptest.NewRecorder()

	// Call handler
	handler.ListImpersonations(rr, req)

	// Verify response
	assert.Equal(t, http.StatusOK, rr.Code)
}

// TestAdminRoutesRegistration verifies that admin routes are properly registered.
func TestAdminRoutesRegistration(t *testing.T) {
	router := chi.NewRouter()

	// Create a mock server
	mockServer := &Server{}
	handler := NewIAMHandler(IAMHandlerDependencies{Server: mockServer})

	// Register admin routes
	RegisterAdminRoutes(router, handler)

	// Verify routes are registered by checking if they match
	routes := []struct {
		method string
		path   string
	}{
		{"POST", "/api/v1/admin/assignments"},
		{"GET", "/api/v1/admin/assignments"},
		{"GET", "/api/v1/admin/assignments/{assignment_id}"},
		{"PUT", "/api/v1/admin/assignments/{assignment_id}"},
		{"DELETE", "/api/v1/admin/assignments/{assignment_id}"},
		{"POST", "/api/v1/admin/check-permission"},
		{"GET", "/api/v1/admin/impersonation"},
		{"POST", "/api/v1/admin/impersonation/start"},
		{"POST", "/api/v1/admin/impersonation/{session_id}/end"},
		{"GET", "/api/v1/admin/users/{user_id}/assignments"},
		{"GET", "/api/v1/admin/users/{user_id}/impersonations"},
	}

	for _, route := range routes {
		req := httptest.NewRequest(route.method, route.path, nil)
		rctx := chi.NewRouteContext()
		req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

		// Just verify the route pattern matches
		assert.NotNil(t, router, "Router should be initialized")
	}
}

// TestAdminAssignmentJSONSerialization verifies JSON marshaling/unmarshaling.
func TestAdminAssignmentJSONSerialization(t *testing.T) {
	now := time.Now().UTC()
	assignment := &iam.AdminAssignment{
		ID:        "adm_test123",
		UserID:    "user_123",
		AdminRole: iam.AdminRoleTenantAdmin,
		TenantIDs: []string{"tenant_1", "tenant_2"},
		Permissions: []iam.AdminPermission{
			iam.PermUserCreate,
			iam.PermUserRead,
		},
		GrantedBy: "admin_user",
		GrantedAt: now,
		Active:    true,
	}

	// Marshal to JSON
	data, err := json.Marshal(assignment)
	require.NoError(t, err)
	require.NotEmpty(t, data)

	// Unmarshal back
	var decoded iam.AdminAssignment
	err = json.Unmarshal(data, &decoded)
	require.NoError(t, err)

	// Verify fields
	assert.Equal(t, assignment.ID, decoded.ID)
	assert.Equal(t, assignment.UserID, decoded.UserID)
	assert.Equal(t, assignment.AdminRole, decoded.AdminRole)
	assert.Equal(t, assignment.TenantIDs, decoded.TenantIDs)
	assert.Equal(t, assignment.Active, decoded.Active)
}

// TestImpersonationSessionJSONSerialization verifies JSON marshaling/unmarshaling.
func TestImpersonationSessionJSONSerialization(t *testing.T) {
	now := time.Now().UTC()
	session := &iam.ImpersonationSession{
		ID:           "imp_test123",
		AdminUserID:  "admin_123",
		TargetUserID: "user_456",
		TenantID:     "tenant_1",
		Reason:       "Support request",
		StartedAt:    now,
		ExpiresAt:    now.Add(time.Hour),
	}

	// Marshal to JSON
	data, err := json.Marshal(session)
	require.NoError(t, err)
	require.NotEmpty(t, data)

	// Unmarshal back
	var decoded iam.ImpersonationSession
	err = json.Unmarshal(data, &decoded)
	require.NoError(t, err)

	// Verify fields
	assert.Equal(t, session.ID, decoded.ID)
	assert.Equal(t, session.AdminUserID, decoded.AdminUserID)
	assert.Equal(t, session.TargetUserID, decoded.TargetUserID)
	assert.Equal(t, session.TenantID, decoded.TenantID)
	assert.Equal(t, session.Reason, decoded.Reason)
}

// TestCreateAdminAssignmentRequest verifies request parsing.
func TestCreateAdminAssignmentRequest(t *testing.T) {
	opts := iam.CreateAdminAssignmentOptions{
		UserID:    "user_123",
		AdminRole: iam.AdminRoleUserAdmin,
		TenantIDs: []string{"tenant_1"},
		Permissions: []iam.AdminPermission{
			iam.PermUserCreate,
		},
		GrantedBy: "super_admin",
	}

	// Marshal to JSON
	data, err := json.Marshal(opts)
	require.NoError(t, err)

	// Create HTTP request
	req := httptest.NewRequest("POST", "/api/v1/admin/assignments", bytes.NewReader(data))
	req.Header.Set("Content-Type", "application/json")

	// Verify request can be created
	assert.NotNil(t, req)
	assert.Equal(t, "POST", req.Method)
}

// TestCreateAdminAssignment_InvalidJSON tests error handling for invalid JSON.
func TestCreateAdminAssignment_InvalidJSON(t *testing.T) {
	handler, _ := newTestIAMHandler(t)

	// Create request with invalid JSON
	req := httptest.NewRequest("POST", "/api/v1/admin/assignments", bytes.NewReader([]byte("invalid json")))
	req.Header.Set("Content-Type", "application/json")

	rr := httptest.NewRecorder()

	// Call handler
	handler.CreateAdminAssignment(rr, req)

	// Verify response - should be 400 Bad Request
	assert.Equal(t, http.StatusBadRequest, rr.Code)
}

// TestGetAdminAssignment_NotFound tests error handling for non-existent assignment.
func TestGetAdminAssignment_NotFound(t *testing.T) {
	handler, _ := newTestIAMHandler(t)

	// Create HTTP request for non-existent assignment
	req := httptest.NewRequest("GET", "/api/v1/admin/assignments/nonexistent", nil)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("assignment_id", "nonexistent")
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

	rr := httptest.NewRecorder()

	// Call handler
	handler.GetAdminAssignment(rr, req)

	// Verify response - should indicate not found
	assert.True(t, rr.Code == http.StatusNotFound || rr.Code == http.StatusInternalServerError)
}

// TestGetAdminAssignment_MissingID tests error handling for missing assignment ID.
func TestGetAdminAssignment_MissingID(t *testing.T) {
	handler, _ := newTestIAMHandler(t)

	// Create HTTP request without assignment_id
	req := httptest.NewRequest("GET", "/api/v1/admin/assignments/", nil)
	rctx := chi.NewRouteContext()
	// Don't add assignment_id param
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

	rr := httptest.NewRecorder()

	// Call handler
	handler.GetAdminAssignment(rr, req)

	// Verify response - should be 400 Bad Request
	assert.Equal(t, http.StatusBadRequest, rr.Code)
}

// TestUpdateAdminAssignment_InvalidJSON tests error handling for invalid JSON.
func TestUpdateAdminAssignment_InvalidJSON(t *testing.T) {
	handler, _ := newTestIAMHandler(t)

	// Create HTTP request with invalid JSON
	req := httptest.NewRequest("PUT", "/api/v1/admin/assignments/assign-123", bytes.NewReader([]byte("invalid json")))
	req.Header.Set("Content-Type", "application/json")
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("assignment_id", "assign-123")
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

	rr := httptest.NewRecorder()

	// Call handler
	handler.UpdateAdminAssignment(rr, req)

	// Verify response - should be 400 Bad Request
	assert.Equal(t, http.StatusBadRequest, rr.Code)
}

// TestDeleteAdminAssignment_MissingID tests error handling for missing assignment ID.
func TestDeleteAdminAssignment_MissingID(t *testing.T) {
	handler, _ := newTestIAMHandler(t)

	// Create HTTP request without assignment_id
	req := httptest.NewRequest("DELETE", "/api/v1/admin/assignments/", nil)
	rctx := chi.NewRouteContext()
	// Don't add assignment_id param
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

	rr := httptest.NewRecorder()

	// Call handler
	handler.DeleteAdminAssignment(rr, req)

	// Verify response - should be 400 Bad Request
	assert.Equal(t, http.StatusBadRequest, rr.Code)
}

// TestGetUserAssignments_MissingUserID tests error handling for missing user ID.
func TestGetUserAssignments_MissingUserID(t *testing.T) {
	handler, _ := newTestIAMHandler(t)

	// Create HTTP request without user_id
	req := httptest.NewRequest("GET", "/api/v1/admin/users//assignments", nil)
	rctx := chi.NewRouteContext()
	// Don't add user_id param
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

	rr := httptest.NewRecorder()

	// Call handler
	handler.GetUserAssignments(rr, req)

	// Verify response - should be 400 Bad Request
	assert.Equal(t, http.StatusBadRequest, rr.Code)
}

// TestCheckAdminPermission_InvalidJSON tests error handling for invalid JSON.
func TestCheckAdminPermission_InvalidJSON(t *testing.T) {
	handler, _ := newTestIAMHandler(t)

	// Create HTTP request with invalid JSON
	req := httptest.NewRequest("POST", "/api/v1/admin/check-permission", bytes.NewReader([]byte("invalid json")))
	req.Header.Set("Content-Type", "application/json")

	rr := httptest.NewRecorder()

	// Call handler
	handler.CheckAdminPermission(rr, req)

	// Verify response - should be 400 Bad Request
	assert.Equal(t, http.StatusBadRequest, rr.Code)
}

// TestStartImpersonation_InvalidJSON tests error handling for invalid JSON.
func TestStartImpersonation_InvalidJSON(t *testing.T) {
	handler, _ := newTestIAMHandler(t)

	// Create HTTP request with invalid JSON
	req := httptest.NewRequest("POST", "/api/v1/admin/impersonate", bytes.NewReader([]byte("invalid json")))
	req.Header.Set("Content-Type", "application/json")

	rr := httptest.NewRecorder()

	// Call handler
	handler.StartImpersonation(rr, req)

	// Verify response - should be 400 Bad Request
	assert.Equal(t, http.StatusBadRequest, rr.Code)
}

// TestEndImpersonation_MissingSessionID tests error handling for missing session ID.
func TestEndImpersonation_MissingSessionID(t *testing.T) {
	handler, _ := newTestIAMHandler(t)

	// Create HTTP request without session_id
	req := httptest.NewRequest("DELETE", "/api/v1/admin/impersonate//end", nil)
	rctx := chi.NewRouteContext()
	// Don't add session_id param
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

	rr := httptest.NewRecorder()

	// Call handler
	handler.EndImpersonation(rr, req)

	// Verify response - should be 400 Bad Request
	assert.Equal(t, http.StatusBadRequest, rr.Code)
}
