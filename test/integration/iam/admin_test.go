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

//go:build integration
// +build integration

package iam_test

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/test/integration/dataplane"
	"github.com/jeremyhahn/go-quicbaas/test/integration/helpers"
)

// AdminAssignmentRequest represents a request to create an admin assignment.
type AdminAssignmentRequest struct {
	UserID            string             `json:"user_id"`
	AdminRole         string             `json:"admin_role"`
	TenantIDs         []string           `json:"tenant_ids,omitempty"`
	Permissions       []string           `json:"permissions,omitempty"`
	DeniedPermissions []string           `json:"denied_permissions,omitempty"`
	ResourceScopes    []ResourceScopeReq `json:"resource_scopes,omitempty"`
	ExpiresAt         *time.Time         `json:"expires_at,omitempty"`
	GrantedBy         string             `json:"granted_by"`
	Metadata          map[string]string  `json:"metadata,omitempty"`
}

// ResourceScopeReq represents a resource scope in requests.
type ResourceScopeReq struct {
	ResourceType    string `json:"resource_type"`
	ResourcePattern string `json:"resource_pattern"`
}

// AdminAssignmentResponse represents an admin assignment in responses.
type AdminAssignmentResponse struct {
	ID                string             `json:"id"`
	UserID            string             `json:"user_id"`
	AdminRole         string             `json:"admin_role"`
	TenantIDs         []string           `json:"tenant_ids,omitempty"`
	Permissions       []string           `json:"permissions,omitempty"`
	DeniedPermissions []string           `json:"denied_permissions,omitempty"`
	ResourceScopes    []ResourceScopeReq `json:"resource_scopes,omitempty"`
	ExpiresAt         *string            `json:"expires_at,omitempty"`
	GrantedBy         string             `json:"granted_by"`
	GrantedAt         string             `json:"granted_at"`
	LastUsedAt        *string            `json:"last_used_at,omitempty"`
	Active            bool               `json:"active"`
	Metadata          map[string]string  `json:"metadata,omitempty"`
}

// UpdateAdminAssignmentRequest represents a request to update an admin assignment.
type UpdateAdminAssignmentRequest struct {
	AdminRole         *string            `json:"admin_role,omitempty"`
	TenantIDs         []string           `json:"tenant_ids,omitempty"`
	Permissions       []string           `json:"permissions,omitempty"`
	DeniedPermissions []string           `json:"denied_permissions,omitempty"`
	ResourceScopes    []ResourceScopeReq `json:"resource_scopes,omitempty"`
	ExpiresAt         *time.Time         `json:"expires_at,omitempty"`
	Active            *bool              `json:"active,omitempty"`
	Metadata          map[string]string  `json:"metadata,omitempty"`
}

// AdminCheckRequest represents a request to check admin permissions.
type AdminCheckRequest struct {
	UserID       string `json:"user_id"`
	Permission   string `json:"permission"`
	TenantID     string `json:"tenant_id,omitempty"`
	ResourceType string `json:"resource_type,omitempty"`
	ResourceID   string `json:"resource_id,omitempty"`
}

// AdminCheckResponse represents the response from a permission check.
type AdminCheckResponse struct {
	Allowed      bool   `json:"allowed"`
	AssignmentID string `json:"assignment_id,omitempty"`
	AdminRole    string `json:"admin_role,omitempty"`
	Reason       string `json:"reason,omitempty"`
}

// ImpersonationRequest represents a request to start impersonation.
type ImpersonationRequest struct {
	TargetUserID string `json:"target_user_id"`
	TenantID     string `json:"tenant_id"`
	Reason       string `json:"reason"`
	Duration     string `json:"duration,omitempty"`
}

// ImpersonationSession represents an active impersonation session.
type ImpersonationSessionResponse struct {
	ID           string  `json:"id"`
	AdminUserID  string  `json:"admin_user_id"`
	TargetUserID string  `json:"target_user_id"`
	TenantID     string  `json:"tenant_id"`
	Reason       string  `json:"reason"`
	StartedAt    string  `json:"started_at"`
	ExpiresAt    string  `json:"expires_at"`
	EndedAt      *string `json:"ended_at,omitempty"`
	AuditLogID   string  `json:"audit_log_id"`
}

// ListAssignmentsResponse represents the response for listing assignments.
type ListAssignmentsResponse struct {
	Assignments []AdminAssignmentResponse `json:"assignments"`
	Total       int                       `json:"total,omitempty"`
	Cursor      string                    `json:"cursor,omitempty"`
}

// ListImpersonationsResponse represents the response for listing impersonation sessions.
type ListImpersonationsResponse struct {
	Sessions []ImpersonationSessionResponse `json:"sessions"`
	Total    int                            `json:"total,omitempty"`
}

// generateUniqueUserID generates a unique user ID for testing.
// setupAdminCluster is setupTestCluster acting as the system admin. Admin
// assignments and impersonation are control-plane state the system admin
// holds, and a caller with no bearer has no tenant to read or write them in.
func setupAdminCluster(t *testing.T) *testCluster {
	t.Helper()
	cluster := setupTestCluster(t)
	source := func(ctx context.Context) (string, error) {
		return helpers.AuthSystemAdminContext(ctx, cluster.httpEndpoint)
	}
	transport, err := dataplane.NewBearerTransport(source, cluster.client.Transport)
	require.NoError(t, err, "system admin HTTP transport")
	cluster.client.Transport = transport
	return cluster
}

func generateUniqueUserID() string {
	// Admin user IDs are qrdb uint64 user IDs carried as decimal strings
	// (json:"user_id,string"); a non-numeric ID fails to decode and the
	// handler answers 400. The shift keeps the ID within int64 and the OR
	// keeps it non-zero, which the handler requires.
	id := uuid.New()
	return strconv.FormatUint(binary.BigEndian.Uint64(id[:8])>>1|1, 10)
}

// createTestAssignment is a helper to create a test admin assignment.
func createTestAssignment(t *testing.T, cluster *testCluster, req AdminAssignmentRequest) AdminAssignmentResponse {
	t.Helper()

	body, err := json.Marshal(req)
	require.NoError(t, err)

	resp, err := cluster.client.Post(
		cluster.httpEndpoint+"/api/v1/admin/assignments",
		"application/json",
		bytes.NewReader(body),
	)
	require.NoError(t, err)
	defer resp.Body.Close()

	require.Equal(t, http.StatusCreated, resp.StatusCode, "Expected 201 Created for assignment creation")

	var result AdminAssignmentResponse
	err = json.NewDecoder(resp.Body).Decode(&result)
	require.NoError(t, err)

	return result
}

// deleteTestAssignment is a helper to delete a test admin assignment.
func deleteTestAssignment(t *testing.T, cluster *testCluster, assignmentID string) {
	t.Helper()

	req, err := http.NewRequest(
		http.MethodDelete,
		cluster.httpEndpoint+"/api/v1/admin/assignments/"+assignmentID,
		nil,
	)
	require.NoError(t, err)

	resp, err := cluster.client.Do(req)
	require.NoError(t, err)
	resp.Body.Close()
}

// impersonationFixture is a provisioned tenant whose admin is logged in, and a
// real user in that tenant. The ids are the numeric account ids the server
// keys admin assignments and impersonation sessions by.
type impersonationFixture struct {
	tenantID     string
	adminUserID  string
	targetUserID string

	// admin sends requests as the tenant admin. StartImpersonation takes the
	// acting admin from the caller's bearer, never from the request body.
	admin *http.Client
}

// setupImpersonationFixture provisions a tenant with the system admin, reads
// the tenant admin's numeric user id from its bearer, and creates a target
// user in the same tenant. The tenant is deleted when the test ends.
func setupImpersonationFixture(t *testing.T, cluster *testCluster) *impersonationFixture {
	t.Helper()

	systemBearer, err := helpers.AuthSystemAdminContext(t.Context(), cluster.httpEndpoint)
	require.NoError(t, err, "system admin login")

	tenantID := "impersonation-" + uuid.New().String()[:8]
	tenant, err := helpers.ProvisionTenant(t.Context(), cluster.httpEndpoint, tenantID, systemBearer)
	require.NoError(t, err, "provision tenant")
	t.Cleanup(func() {
		assert.NoError(t, helpers.DeleteTenant(context.Background(), cluster.httpEndpoint, tenantID, systemBearer),
			"delete tenant")
	})

	adminUserID, err := helpers.BearerUserID(tenant.AdminBearer)
	require.NoError(t, err, "tenant admin user id")

	tlsCfg, err := helpers.IntegrationTLSConfig()
	require.NoError(t, err, "load integration TLS config")
	admin := newIntegrationHTTPClient(tlsCfg)
	admin.Transport, err = dataplane.NewBearerTransport(func(context.Context) (string, error) {
		return tenant.AdminBearer, nil
	}, admin.Transport)
	require.NoError(t, err, "tenant admin HTTP transport")

	fixture := &impersonationFixture{
		tenantID:    tenantID,
		adminUserID: strconv.FormatUint(adminUserID, 10),
		admin:       admin,
	}
	fixture.targetUserID = fixture.createUser(t, cluster)
	return fixture
}

// createUser creates a user in the fixture's tenant and returns its numeric id.
func (f *impersonationFixture) createUser(t *testing.T, cluster *testCluster) string {
	t.Helper()

	name := "target-" + uuid.New().String()[:8]
	body, err := json.Marshal(map[string]string{
		"email":    name + "@test.local",
		"username": name,
		"name":     "Impersonation Target " + name,
	})
	require.NoError(t, err)

	resp, err := f.admin.Post(
		cluster.httpEndpoint+"/api/v1/tenants/"+f.tenantID+"/users",
		"application/json",
		bytes.NewReader(body),
	)
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, http.StatusCreated, resp.StatusCode, "Expected 201 Created for target user")

	var user struct {
		ID string `json:"id"`
	}
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&user))
	require.NotEmpty(t, user.ID, "created user has no id")
	return user.ID
}

// assign grants the fixture's admin an admin role scoped to its tenant, as the
// system admin, and removes the assignment when the test ends.
func (f *impersonationFixture) assign(t *testing.T, cluster *testCluster, role string, permissions ...string) {
	t.Helper()

	created := createTestAssignment(t, cluster, AdminAssignmentRequest{
		UserID:      f.adminUserID,
		AdminRole:   role,
		TenantIDs:   []string{f.tenantID},
		Permissions: permissions,
		GrantedBy:   "system",
	})
	t.Cleanup(func() { deleteTestAssignment(t, cluster, created.ID) })
}

// startImpersonation posts req as the fixture's admin.
func (f *impersonationFixture) startImpersonation(t *testing.T, cluster *testCluster, req ImpersonationRequest) *http.Response {
	t.Helper()

	body, err := json.Marshal(req)
	require.NoError(t, err)

	resp, err := f.admin.Post(
		cluster.httpEndpoint+"/api/v1/admin/impersonation/start",
		"application/json",
		bytes.NewReader(body),
	)
	require.NoError(t, err)
	return resp
}

// ============================================================================
// Assignment CRUD Tests
// ============================================================================

// TestAdmin_CreateAssignment_Success tests creating a valid admin assignment.
func TestAdmin_CreateAssignment_Success(t *testing.T) {
	cluster := setupAdminCluster(t)
	defer cluster.Close()

	userID := generateUniqueUserID()

	req := AdminAssignmentRequest{
		UserID:      userID,
		AdminRole:   "tenant_admin",
		TenantIDs:   []string{"test-tenant-1", "test-tenant-2"},
		Permissions: []string{"user:create", "user:read", "user:update"},
		GrantedBy:   "system",
		Metadata: map[string]string{
			"department": "engineering",
			"team":       "platform",
		},
	}

	body, err := json.Marshal(req)
	require.NoError(t, err)

	resp, err := cluster.client.Post(
		cluster.httpEndpoint+"/api/v1/admin/assignments",
		"application/json",
		bytes.NewReader(body),
	)
	require.NoError(t, err)
	defer resp.Body.Close()

	// Assert: Response status is 201 Created
	assert.Equal(t, http.StatusCreated, resp.StatusCode, "Expected 201 Created status")

	var result AdminAssignmentResponse
	err = json.NewDecoder(resp.Body).Decode(&result)
	require.NoError(t, err)

	// Assert: Response has correct fields
	assert.NotEmpty(t, result.ID, "Assignment ID should not be empty")
	assert.Equal(t, userID, result.UserID, "UserID should match request")
	assert.Equal(t, "tenant_admin", result.AdminRole, "AdminRole should match request")
	assert.ElementsMatch(t, []string{"test-tenant-1", "test-tenant-2"}, result.TenantIDs, "TenantIDs should match")
	assert.True(t, result.Active, "Assignment should be active by default")
	assert.NotEmpty(t, result.GrantedAt, "GrantedAt should be set")
	assert.Equal(t, "system", result.GrantedBy, "GrantedBy should match")
	assert.Equal(t, "engineering", result.Metadata["department"], "Metadata should be preserved")

	// Verify: Assignment exists via GET
	getResp, err := cluster.client.Get(cluster.httpEndpoint + "/api/v1/admin/assignments/" + result.ID)
	require.NoError(t, err)
	defer getResp.Body.Close()

	assert.Equal(t, http.StatusOK, getResp.StatusCode, "GET should return 200 OK")

	var retrieved AdminAssignmentResponse
	err = json.NewDecoder(getResp.Body).Decode(&retrieved)
	require.NoError(t, err)
	assert.Equal(t, result.ID, retrieved.ID, "Retrieved assignment should match created one")

	// Cleanup
	deleteTestAssignment(t, cluster, result.ID)
}

// TestAdmin_CreateAssignment_InvalidRole tests creating an assignment with invalid role.
func TestAdmin_CreateAssignment_InvalidRole(t *testing.T) {
	cluster := setupAdminCluster(t)
	defer cluster.Close()

	userID := generateUniqueUserID()

	req := AdminAssignmentRequest{
		UserID:    userID,
		AdminRole: "invalid_role_that_does_not_exist",
		GrantedBy: "system",
	}

	body, err := json.Marshal(req)
	require.NoError(t, err)

	resp, err := cluster.client.Post(
		cluster.httpEndpoint+"/api/v1/admin/assignments",
		"application/json",
		bytes.NewReader(body),
	)
	require.NoError(t, err)
	defer resp.Body.Close()

	// Assert: Response status is 400 Bad Request
	assert.Equal(t, http.StatusBadRequest, resp.StatusCode, "Expected 400 Bad Request for invalid role")

	// Assert: Error response contains relevant message
	responseBody, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	assert.Contains(t, string(responseBody), "invalid", "Error should mention invalid role")
}

// TestAdmin_CreateAssignment_MissingUserID tests creating an assignment without user ID.
func TestAdmin_CreateAssignment_MissingUserID(t *testing.T) {
	cluster := setupAdminCluster(t)
	defer cluster.Close()

	req := AdminAssignmentRequest{
		AdminRole: "super_admin",
		GrantedBy: "system",
	}

	body, err := json.Marshal(req)
	require.NoError(t, err)

	resp, err := cluster.client.Post(
		cluster.httpEndpoint+"/api/v1/admin/assignments",
		"application/json",
		bytes.NewReader(body),
	)
	require.NoError(t, err)
	defer resp.Body.Close()

	// Assert: Response status is 400 Bad Request
	assert.Equal(t, http.StatusBadRequest, resp.StatusCode, "Expected 400 Bad Request for missing user_id")
}

// TestAdmin_CreateAssignment_MissingGrantedBy tests creating an assignment without granted_by.
func TestAdmin_CreateAssignment_MissingGrantedBy(t *testing.T) {
	cluster := setupAdminCluster(t)
	defer cluster.Close()

	req := AdminAssignmentRequest{
		UserID:    generateUniqueUserID(),
		AdminRole: "super_admin",
	}

	body, err := json.Marshal(req)
	require.NoError(t, err)

	resp, err := cluster.client.Post(
		cluster.httpEndpoint+"/api/v1/admin/assignments",
		"application/json",
		bytes.NewReader(body),
	)
	require.NoError(t, err)
	defer resp.Body.Close()

	// Assert: Response status is 400 Bad Request
	assert.Equal(t, http.StatusBadRequest, resp.StatusCode, "Expected 400 Bad Request for missing granted_by")
}

// TestAdmin_GetAssignment_Success tests retrieving an assignment by ID.
func TestAdmin_GetAssignment_Success(t *testing.T) {
	cluster := setupAdminCluster(t)
	defer cluster.Close()

	// Create an assignment first
	userID := generateUniqueUserID()
	createReq := AdminAssignmentRequest{
		UserID:      userID,
		AdminRole:   "user_admin",
		TenantIDs:   []string{"tenant-a"},
		Permissions: []string{"user:read", "user:update"},
		GrantedBy:   "system",
	}

	created := createTestAssignment(t, cluster, createReq)
	defer deleteTestAssignment(t, cluster, created.ID)

	// GET the assignment by ID
	resp, err := cluster.client.Get(cluster.httpEndpoint + "/api/v1/admin/assignments/" + created.ID)
	require.NoError(t, err)
	defer resp.Body.Close()

	// Assert: Response status is 200 OK
	assert.Equal(t, http.StatusOK, resp.StatusCode, "Expected 200 OK")

	var retrieved AdminAssignmentResponse
	err = json.NewDecoder(resp.Body).Decode(&retrieved)
	require.NoError(t, err)

	// Assert: All fields match
	assert.Equal(t, created.ID, retrieved.ID, "ID should match")
	assert.Equal(t, userID, retrieved.UserID, "UserID should match")
	assert.Equal(t, "user_admin", retrieved.AdminRole, "AdminRole should match")
	assert.ElementsMatch(t, []string{"tenant-a"}, retrieved.TenantIDs, "TenantIDs should match")
	assert.True(t, retrieved.Active, "Assignment should be active")
}

// TestAdmin_GetAssignment_NotFound tests retrieving a non-existent assignment.
func TestAdmin_GetAssignment_NotFound(t *testing.T) {
	cluster := setupAdminCluster(t)
	defer cluster.Close()

	nonExistentID := uuid.New().String()

	resp, err := cluster.client.Get(cluster.httpEndpoint + "/api/v1/admin/assignments/" + nonExistentID)
	require.NoError(t, err)
	defer resp.Body.Close()

	// Assert: Response status is 404 Not Found
	assert.Equal(t, http.StatusNotFound, resp.StatusCode, "Expected 404 Not Found for non-existent assignment")
}

// TestAdmin_UpdateAssignment_Success tests updating an existing assignment.
func TestAdmin_UpdateAssignment_Success(t *testing.T) {
	cluster := setupAdminCluster(t)
	defer cluster.Close()

	// Create an assignment first
	userID := generateUniqueUserID()
	createReq := AdminAssignmentRequest{
		UserID:    userID,
		AdminRole: "user_admin",
		TenantIDs: []string{"tenant-a"},
		GrantedBy: "system",
	}

	created := createTestAssignment(t, cluster, createReq)
	defer deleteTestAssignment(t, cluster, created.ID)

	// Update the assignment
	newRole := "tenant_admin"
	updateReq := UpdateAdminAssignmentRequest{
		AdminRole: &newRole,
		TenantIDs: []string{"tenant-a", "tenant-b"},
		Metadata: map[string]string{
			"updated": "true",
		},
	}

	body, err := json.Marshal(updateReq)
	require.NoError(t, err)

	req, err := http.NewRequest(
		http.MethodPut,
		cluster.httpEndpoint+"/api/v1/admin/assignments/"+created.ID,
		bytes.NewReader(body),
	)
	require.NoError(t, err)
	req.Header.Set("Content-Type", "application/json")

	resp, err := cluster.client.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()

	// Assert: Response status is 200 OK
	assert.Equal(t, http.StatusOK, resp.StatusCode, "Expected 200 OK for update")

	var updated AdminAssignmentResponse
	err = json.NewDecoder(resp.Body).Decode(&updated)
	require.NoError(t, err)

	// Assert: Changes persisted
	assert.Equal(t, "tenant_admin", updated.AdminRole, "AdminRole should be updated")
	assert.ElementsMatch(t, []string{"tenant-a", "tenant-b"}, updated.TenantIDs, "TenantIDs should be updated")
	assert.Equal(t, "true", updated.Metadata["updated"], "Metadata should be updated")
}

// TestAdmin_UpdateAssignment_NotFound tests updating a non-existent assignment.
func TestAdmin_UpdateAssignment_NotFound(t *testing.T) {
	cluster := setupAdminCluster(t)
	defer cluster.Close()

	nonExistentID := uuid.New().String()
	newRole := "tenant_admin"
	updateReq := UpdateAdminAssignmentRequest{
		AdminRole: &newRole,
	}

	body, err := json.Marshal(updateReq)
	require.NoError(t, err)

	req, err := http.NewRequest(
		http.MethodPut,
		cluster.httpEndpoint+"/api/v1/admin/assignments/"+nonExistentID,
		bytes.NewReader(body),
	)
	require.NoError(t, err)
	req.Header.Set("Content-Type", "application/json")

	resp, err := cluster.client.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()

	// Assert: Response status is 404 Not Found
	assert.Equal(t, http.StatusNotFound, resp.StatusCode, "Expected 404 Not Found")
}

// TestAdmin_DeleteAssignment_Success tests deleting an existing assignment.
func TestAdmin_DeleteAssignment_Success(t *testing.T) {
	cluster := setupAdminCluster(t)
	defer cluster.Close()

	// Create an assignment first
	userID := generateUniqueUserID()
	createReq := AdminAssignmentRequest{
		UserID:    userID,
		AdminRole: "audit_admin",
		GrantedBy: "system",
	}

	created := createTestAssignment(t, cluster, createReq)

	// DELETE the assignment
	req, err := http.NewRequest(
		http.MethodDelete,
		cluster.httpEndpoint+"/api/v1/admin/assignments/"+created.ID,
		nil,
	)
	require.NoError(t, err)

	resp, err := cluster.client.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()

	// Assert: Response status is 204 No Content
	assert.Equal(t, http.StatusNoContent, resp.StatusCode, "Expected 204 No Content for delete")

	// Verify: GET returns 404
	getResp, err := cluster.client.Get(cluster.httpEndpoint + "/api/v1/admin/assignments/" + created.ID)
	require.NoError(t, err)
	defer getResp.Body.Close()

	assert.Equal(t, http.StatusNotFound, getResp.StatusCode, "Deleted assignment should return 404")
}

// TestAdmin_DeleteAssignment_NotFound tests deleting a non-existent assignment.
func TestAdmin_DeleteAssignment_NotFound(t *testing.T) {
	cluster := setupAdminCluster(t)
	defer cluster.Close()

	nonExistentID := uuid.New().String()

	req, err := http.NewRequest(
		http.MethodDelete,
		cluster.httpEndpoint+"/api/v1/admin/assignments/"+nonExistentID,
		nil,
	)
	require.NoError(t, err)

	resp, err := cluster.client.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()

	// Assert: Response status is 404 Not Found
	assert.Equal(t, http.StatusNotFound, resp.StatusCode, "Expected 404 Not Found for non-existent assignment")
}

// TestAdmin_ListAssignments_Empty tests listing assignments when none exist.
func TestAdmin_ListAssignments_Empty(t *testing.T) {
	cluster := setupAdminCluster(t)
	defer cluster.Close()

	// A fresh qrdb user ID (uint64) that holds no assignment; a UUID is not a
	// user ID and the handler answers it 400.
	uniqueFilter := generateUniqueUserID()

	resp, err := cluster.client.Get(cluster.httpEndpoint + "/api/v1/admin/assignments?user_id=" + uniqueFilter)
	require.NoError(t, err)
	defer resp.Body.Close()

	// Assert: Response status is 200 OK
	assert.Equal(t, http.StatusOK, resp.StatusCode, "Expected 200 OK")

	var result ListAssignmentsResponse
	err = json.NewDecoder(resp.Body).Decode(&result)
	require.NoError(t, err)

	// Assert: Empty list
	assert.Empty(t, result.Assignments, "Assignments list should be empty for non-existent user")
}

// TestAdmin_ListAssignments_WithFiltering tests listing assignments with various filters.
func TestAdmin_ListAssignments_WithFiltering(t *testing.T) {
	cluster := setupAdminCluster(t)
	defer cluster.Close()

	// Create multiple assignments with different roles and tenants
	testPrefix := uuid.New().String()[:8]

	assignments := []AdminAssignmentRequest{
		{
			UserID:    fmt.Sprintf("%s-user-1", testPrefix),
			AdminRole: "super_admin",
			GrantedBy: "system",
		},
		{
			UserID:    fmt.Sprintf("%s-user-2", testPrefix),
			AdminRole: "tenant_admin",
			TenantIDs: []string{"tenant-alpha"},
			GrantedBy: "system",
		},
		{
			UserID:    fmt.Sprintf("%s-user-3", testPrefix),
			AdminRole: "tenant_admin",
			TenantIDs: []string{"tenant-beta"},
			GrantedBy: "system",
		},
		{
			UserID:    fmt.Sprintf("%s-user-4", testPrefix),
			AdminRole: "user_admin",
			TenantIDs: []string{"tenant-alpha"},
			GrantedBy: "system",
		},
	}

	var createdIDs []string
	for _, req := range assignments {
		created := createTestAssignment(t, cluster, req)
		createdIDs = append(createdIDs, created.ID)
	}
	defer func() {
		for _, id := range createdIDs {
			deleteTestAssignment(t, cluster, id)
		}
	}()

	// Test: Filter by admin_role
	t.Run("FilterByRole", func(t *testing.T) {
		resp, err := cluster.client.Get(cluster.httpEndpoint + "/api/v1/admin/assignments?admin_role=tenant_admin")
		require.NoError(t, err)
		defer resp.Body.Close()

		assert.Equal(t, http.StatusOK, resp.StatusCode)

		var result ListAssignmentsResponse
		err = json.NewDecoder(resp.Body).Decode(&result)
		require.NoError(t, err)

		// All returned should be tenant_admin
		for _, assignment := range result.Assignments {
			if assignment.AdminRole != "" {
				assert.Equal(t, "tenant_admin", assignment.AdminRole, "Should only return tenant_admin roles")
			}
		}
	})

	// Test: Filter by tenant_id
	t.Run("FilterByTenant", func(t *testing.T) {
		resp, err := cluster.client.Get(cluster.httpEndpoint + "/api/v1/admin/assignments?tenant_id=tenant-alpha")
		require.NoError(t, err)
		defer resp.Body.Close()

		assert.Equal(t, http.StatusOK, resp.StatusCode)

		var result ListAssignmentsResponse
		err = json.NewDecoder(resp.Body).Decode(&result)
		require.NoError(t, err)

		// All returned should have tenant-alpha in TenantIDs
		for _, assignment := range result.Assignments {
			if len(assignment.TenantIDs) > 0 {
				assert.Contains(t, assignment.TenantIDs, "tenant-alpha", "Should only return assignments for tenant-alpha")
			}
		}
	})

	// Test: Filter by user_id
	t.Run("FilterByUserID", func(t *testing.T) {
		userID := fmt.Sprintf("%s-user-2", testPrefix)
		resp, err := cluster.client.Get(cluster.httpEndpoint + "/api/v1/admin/assignments?user_id=" + userID)
		require.NoError(t, err)
		defer resp.Body.Close()

		assert.Equal(t, http.StatusOK, resp.StatusCode)

		var result ListAssignmentsResponse
		err = json.NewDecoder(resp.Body).Decode(&result)
		require.NoError(t, err)

		// Should return exactly one assignment
		require.Len(t, result.Assignments, 1, "Should return exactly one assignment for specific user")
		assert.Equal(t, userID, result.Assignments[0].UserID, "UserID should match")
	})

	// Test: Filter by active_only
	t.Run("FilterByActive", func(t *testing.T) {
		resp, err := cluster.client.Get(cluster.httpEndpoint + "/api/v1/admin/assignments?active_only=true")
		require.NoError(t, err)
		defer resp.Body.Close()

		assert.Equal(t, http.StatusOK, resp.StatusCode)

		var result ListAssignmentsResponse
		err = json.NewDecoder(resp.Body).Decode(&result)
		require.NoError(t, err)

		// All returned should be active
		for _, assignment := range result.Assignments {
			assert.True(t, assignment.Active, "All assignments should be active")
		}
	})
}

// ============================================================================
// Permission Check Tests
// ============================================================================

// TestAdmin_CheckPermission_Allowed tests checking a permission that is allowed.
func TestAdmin_CheckPermission_Allowed(t *testing.T) {
	cluster := setupAdminCluster(t)
	defer cluster.Close()

	// Create an assignment with specific permissions
	userID := generateUniqueUserID()
	createReq := AdminAssignmentRequest{
		UserID:    userID,
		AdminRole: "super_admin",
		GrantedBy: "system",
	}

	created := createTestAssignment(t, cluster, createReq)
	defer deleteTestAssignment(t, cluster, created.ID)

	// Check permission
	checkReq := AdminCheckRequest{
		UserID:     userID,
		Permission: "user:create",
	}

	body, err := json.Marshal(checkReq)
	require.NoError(t, err)

	resp, err := cluster.client.Post(
		cluster.httpEndpoint+"/api/v1/admin/check-permission",
		"application/json",
		bytes.NewReader(body),
	)
	require.NoError(t, err)
	defer resp.Body.Close()

	// Assert: Response status is 200 OK
	assert.Equal(t, http.StatusOK, resp.StatusCode, "Expected 200 OK")

	var checkResp AdminCheckResponse
	err = json.NewDecoder(resp.Body).Decode(&checkResp)
	require.NoError(t, err)

	// Assert: Permission is allowed
	assert.True(t, checkResp.Allowed, "Permission should be allowed for super_admin")
	assert.Equal(t, "super_admin", checkResp.AdminRole, "AdminRole should be returned")
	assert.NotEmpty(t, checkResp.AssignmentID, "AssignmentID should be returned")
}

// TestAdmin_CheckPermission_Denied tests checking a permission that is denied.
func TestAdmin_CheckPermission_Denied(t *testing.T) {
	cluster := setupAdminCluster(t)
	defer cluster.Close()

	// Create a read_only assignment (no user:create permission)
	userID := generateUniqueUserID()
	createReq := AdminAssignmentRequest{
		UserID:    userID,
		AdminRole: "read_only",
		GrantedBy: "system",
	}

	created := createTestAssignment(t, cluster, createReq)
	defer deleteTestAssignment(t, cluster, created.ID)

	// Check permission for user:create (not in read_only permissions)
	checkReq := AdminCheckRequest{
		UserID:     userID,
		Permission: "user:create",
	}

	body, err := json.Marshal(checkReq)
	require.NoError(t, err)

	resp, err := cluster.client.Post(
		cluster.httpEndpoint+"/api/v1/admin/check-permission",
		"application/json",
		bytes.NewReader(body),
	)
	require.NoError(t, err)
	defer resp.Body.Close()

	// Assert: Response status is 200 OK
	assert.Equal(t, http.StatusOK, resp.StatusCode, "Expected 200 OK")

	var checkResp AdminCheckResponse
	err = json.NewDecoder(resp.Body).Decode(&checkResp)
	require.NoError(t, err)

	// Assert: Permission is denied
	assert.False(t, checkResp.Allowed, "Permission should be denied for read_only role")
	assert.NotEmpty(t, checkResp.Reason, "Reason should be provided for denial")
}

// TestAdmin_CheckPermission_NoAssignment tests checking permission for user without assignment.
func TestAdmin_CheckPermission_NoAssignment(t *testing.T) {
	cluster := setupAdminCluster(t)
	defer cluster.Close()

	// Use a user ID that doesn't have any assignment
	nonExistentUser := generateUniqueUserID()

	checkReq := AdminCheckRequest{
		UserID:     nonExistentUser,
		Permission: "user:read",
	}

	body, err := json.Marshal(checkReq)
	require.NoError(t, err)

	resp, err := cluster.client.Post(
		cluster.httpEndpoint+"/api/v1/admin/check-permission",
		"application/json",
		bytes.NewReader(body),
	)
	require.NoError(t, err)
	defer resp.Body.Close()

	// Assert: Response status is 200 OK (check succeeded, permission denied)
	assert.Equal(t, http.StatusOK, resp.StatusCode, "Expected 200 OK")

	var checkResp AdminCheckResponse
	err = json.NewDecoder(resp.Body).Decode(&checkResp)
	require.NoError(t, err)

	// Assert: Permission is denied
	assert.False(t, checkResp.Allowed, "Permission should be denied for user without assignment")
}

// TestAdmin_CheckPermission_ExpiredAssignment tests permission check with expired assignment.
func TestAdmin_CheckPermission_ExpiredAssignment(t *testing.T) {
	cluster := setupAdminCluster(t)
	defer cluster.Close()

	// Create an assignment with past expiry
	userID := generateUniqueUserID()
	pastTime := time.Now().Add(-24 * time.Hour)
	createReq := AdminAssignmentRequest{
		UserID:    userID,
		AdminRole: "super_admin",
		ExpiresAt: &pastTime,
		GrantedBy: "system",
	}

	body, err := json.Marshal(createReq)
	require.NoError(t, err)

	resp, err := cluster.client.Post(
		cluster.httpEndpoint+"/api/v1/admin/assignments",
		"application/json",
		bytes.NewReader(body),
	)
	require.NoError(t, err)
	defer resp.Body.Close()

	// If creation succeeds with expired assignment, check permission
	if resp.StatusCode == http.StatusCreated {
		var created AdminAssignmentResponse
		err = json.NewDecoder(resp.Body).Decode(&created)
		require.NoError(t, err)
		defer deleteTestAssignment(t, cluster, created.ID)

		// Check permission
		checkReq := AdminCheckRequest{
			UserID:     userID,
			Permission: "user:create",
		}

		checkBody, err := json.Marshal(checkReq)
		require.NoError(t, err)

		checkResp, err := cluster.client.Post(
			cluster.httpEndpoint+"/api/v1/admin/check-permission",
			"application/json",
			bytes.NewReader(checkBody),
		)
		require.NoError(t, err)
		defer checkResp.Body.Close()

		var result AdminCheckResponse
		err = json.NewDecoder(checkResp.Body).Decode(&result)
		require.NoError(t, err)

		// Assert: Permission is denied due to expiry
		assert.False(t, result.Allowed, "Permission should be denied for expired assignment")
		assert.Contains(t, result.Reason, "expir", "Reason should mention expiration")
	}
}

// TestAdmin_CheckPermission_TenantScope tests tenant-scoped permission checking.
func TestAdmin_CheckPermission_TenantScope(t *testing.T) {
	cluster := setupAdminCluster(t)
	defer cluster.Close()

	// Create tenant-scoped admin
	userID := generateUniqueUserID()
	createReq := AdminAssignmentRequest{
		UserID:    userID,
		AdminRole: "tenant_admin",
		TenantIDs: []string{"tenant-x"},
		GrantedBy: "system",
	}

	created := createTestAssignment(t, cluster, createReq)
	defer deleteTestAssignment(t, cluster, created.ID)

	// Test: Check permission for correct tenant
	t.Run("CorrectTenant", func(t *testing.T) {
		checkReq := AdminCheckRequest{
			UserID:     userID,
			Permission: "user:create",
			TenantID:   "tenant-x",
		}

		body, err := json.Marshal(checkReq)
		require.NoError(t, err)

		resp, err := cluster.client.Post(
			cluster.httpEndpoint+"/api/v1/admin/check-permission",
			"application/json",
			bytes.NewReader(body),
		)
		require.NoError(t, err)
		defer resp.Body.Close()

		var result AdminCheckResponse
		err = json.NewDecoder(resp.Body).Decode(&result)
		require.NoError(t, err)

		// Assert: Permission is allowed for correct tenant
		assert.True(t, result.Allowed, "Permission should be allowed for assigned tenant")
	})

	// Test: Check permission for wrong tenant
	t.Run("WrongTenant", func(t *testing.T) {
		checkReq := AdminCheckRequest{
			UserID:     userID,
			Permission: "user:create",
			TenantID:   "tenant-y", // Different tenant
		}

		body, err := json.Marshal(checkReq)
		require.NoError(t, err)

		resp, err := cluster.client.Post(
			cluster.httpEndpoint+"/api/v1/admin/check-permission",
			"application/json",
			bytes.NewReader(body),
		)
		require.NoError(t, err)
		defer resp.Body.Close()

		var result AdminCheckResponse
		err = json.NewDecoder(resp.Body).Decode(&result)
		require.NoError(t, err)

		// Assert: Permission is denied for wrong tenant
		assert.False(t, result.Allowed, "Permission should be denied for non-assigned tenant")
	})
}

// TestAdmin_CheckPermission_DeniedPermissions tests explicit permission denial.
func TestAdmin_CheckPermission_DeniedPermissions(t *testing.T) {
	cluster := setupAdminCluster(t)
	defer cluster.Close()

	// Create assignment with explicitly denied permissions
	userID := generateUniqueUserID()
	createReq := AdminAssignmentRequest{
		UserID:            userID,
		AdminRole:         "super_admin",           // Has user:delete by default
		DeniedPermissions: []string{"user:delete"}, // Explicitly deny
		GrantedBy:         "system",
	}

	created := createTestAssignment(t, cluster, createReq)
	defer deleteTestAssignment(t, cluster, created.ID)

	// Check the denied permission
	checkReq := AdminCheckRequest{
		UserID:     userID,
		Permission: "user:delete",
	}

	body, err := json.Marshal(checkReq)
	require.NoError(t, err)

	resp, err := cluster.client.Post(
		cluster.httpEndpoint+"/api/v1/admin/check-permission",
		"application/json",
		bytes.NewReader(body),
	)
	require.NoError(t, err)
	defer resp.Body.Close()

	var result AdminCheckResponse
	err = json.NewDecoder(resp.Body).Decode(&result)
	require.NoError(t, err)

	// Assert: Permission is denied despite role normally having it
	assert.False(t, result.Allowed, "Explicitly denied permission should be denied")
}

// ============================================================================
// Impersonation Tests
// ============================================================================

// TestAdmin_StartImpersonation_Success tests starting impersonation with proper permissions.
func TestAdmin_StartImpersonation_Success(t *testing.T) {
	cluster := setupAdminCluster(t)
	defer cluster.Close()

	// The tenant admin is granted impersonation in its tenant and starts the
	// session itself: the server records the caller as the acting admin.
	fixture := setupImpersonationFixture(t, cluster)
	fixture.assign(t, cluster, "super_admin", "user:impersonate")

	resp := fixture.startImpersonation(t, cluster, ImpersonationRequest{
		TargetUserID: fixture.targetUserID,
		TenantID:     fixture.tenantID,
		Reason:       "Support ticket #12345 - user cannot access dashboard",
		Duration:     "1h",
	})
	defer resp.Body.Close()

	// Assert: Response status is 200 OK
	require.Equal(t, http.StatusOK, resp.StatusCode, "Expected 200 OK for impersonation start")

	var session ImpersonationSessionResponse
	err := json.NewDecoder(resp.Body).Decode(&session)
	require.NoError(t, err)

	// Assert: Session created with correct fields
	assert.NotEmpty(t, session.ID, "Session ID should not be empty")
	assert.Equal(t, fixture.adminUserID, session.AdminUserID, "AdminUserID should be the authenticated caller")
	assert.Equal(t, fixture.targetUserID, session.TargetUserID, "TargetUserID should match")
	assert.Equal(t, fixture.tenantID, session.TenantID, "TenantID should match")
	assert.Equal(t, "Support ticket #12345 - user cannot access dashboard", session.Reason, "Reason should match")
	assert.NotEmpty(t, session.StartedAt, "StartedAt should be set")
	assert.NotEmpty(t, session.ExpiresAt, "ExpiresAt should be set")
	assert.Nil(t, session.EndedAt, "EndedAt should be nil for active session")
}

// TestAdmin_StartImpersonation_NotAllowed tests starting impersonation without permission.
func TestAdmin_StartImpersonation_NotAllowed(t *testing.T) {
	cluster := setupAdminCluster(t)
	defer cluster.Close()

	// The tenant admin holds only read_only, which has no impersonation
	// permission, so a well-formed request against a real target is denied.
	fixture := setupImpersonationFixture(t, cluster)
	fixture.assign(t, cluster, "read_only")

	resp := fixture.startImpersonation(t, cluster, ImpersonationRequest{
		TargetUserID: fixture.targetUserID,
		TenantID:     fixture.tenantID,
		Reason:       "Unauthorized attempt",
	})
	defer resp.Body.Close()

	// Assert: Response status is 403 Forbidden
	assert.Equal(t, http.StatusForbidden, resp.StatusCode, "Expected 403 Forbidden for unauthorized impersonation")
}

// TestAdmin_StartImpersonation_MissingReason tests starting impersonation without reason.
func TestAdmin_StartImpersonation_MissingReason(t *testing.T) {
	cluster := setupAdminCluster(t)
	defer cluster.Close()

	// A real numeric target, so the 400 comes from the missing reason and not
	// from an unparseable target id.
	fixture := setupImpersonationFixture(t, cluster)
	fixture.assign(t, cluster, "super_admin", "user:impersonate")

	resp := fixture.startImpersonation(t, cluster, ImpersonationRequest{
		TargetUserID: fixture.targetUserID,
		TenantID:     fixture.tenantID,
		// Missing reason
	})
	defer resp.Body.Close()

	// Assert: Response status is 400 Bad Request
	assert.Equal(t, http.StatusBadRequest, resp.StatusCode, "Expected 400 Bad Request for missing reason")
}

// TestAdmin_EndImpersonation_Success tests ending an impersonation session.
func TestAdmin_EndImpersonation_Success(t *testing.T) {
	cluster := setupAdminCluster(t)
	defer cluster.Close()

	fixture := setupImpersonationFixture(t, cluster)
	fixture.assign(t, cluster, "super_admin", "user:impersonate")

	// Start impersonation
	startResp := fixture.startImpersonation(t, cluster, ImpersonationRequest{
		TargetUserID: fixture.targetUserID,
		TenantID:     fixture.tenantID,
		Reason:       "Testing impersonation flow",
		Duration:     "1h",
	})
	defer startResp.Body.Close()

	require.Equal(t, http.StatusOK, startResp.StatusCode)

	var session ImpersonationSessionResponse
	err := json.NewDecoder(startResp.Body).Decode(&session)
	require.NoError(t, err)

	// End impersonation as the admin who started it
	endReq, err := http.NewRequest(
		http.MethodPost,
		cluster.httpEndpoint+"/api/v1/admin/impersonation/"+session.ID+"/end",
		nil,
	)
	require.NoError(t, err)

	endResp, err := fixture.admin.Do(endReq)
	require.NoError(t, err)
	defer endResp.Body.Close()

	// Assert: Response status is 200 OK
	assert.Equal(t, http.StatusOK, endResp.StatusCode, "Expected 200 OK for ending impersonation")

	var endedSession ImpersonationSessionResponse
	err = json.NewDecoder(endResp.Body).Decode(&endedSession)
	require.NoError(t, err)

	// Assert: Session is ended
	assert.NotNil(t, endedSession.EndedAt, "EndedAt should be set")
}

// TestAdmin_EndImpersonation_NotFound tests ending a non-existent impersonation session.
func TestAdmin_EndImpersonation_NotFound(t *testing.T) {
	cluster := setupAdminCluster(t)
	defer cluster.Close()

	nonExistentID := uuid.New().String()

	req, err := http.NewRequest(
		http.MethodPost,
		cluster.httpEndpoint+"/api/v1/admin/impersonation/"+nonExistentID+"/end",
		nil,
	)
	require.NoError(t, err)

	resp, err := cluster.client.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()

	// Assert: Response status is 404 Not Found
	assert.Equal(t, http.StatusNotFound, resp.StatusCode, "Expected 404 Not Found for non-existent session")
}

// TestAdmin_ListActiveImpersonations tests listing active impersonation sessions.
func TestAdmin_ListActiveImpersonations(t *testing.T) {
	cluster := setupAdminCluster(t)
	defer cluster.Close()

	fixture := setupImpersonationFixture(t, cluster)
	fixture.assign(t, cluster, "super_admin", "user:impersonate")

	// Start multiple impersonation sessions, each against its own real user
	var sessionIDs []string
	for i := 0; i < 3; i++ {
		resp := fixture.startImpersonation(t, cluster, ImpersonationRequest{
			TargetUserID: fixture.createUser(t, cluster),
			TenantID:     fixture.tenantID,
			Reason:       fmt.Sprintf("Testing session %d", i+1),
			Duration:     "1h",
		})
		require.Equal(t, http.StatusOK, resp.StatusCode, "Expected 200 OK for impersonation start %d", i+1)

		var session ImpersonationSessionResponse
		err := json.NewDecoder(resp.Body).Decode(&session)
		resp.Body.Close()
		require.NoError(t, err)
		sessionIDs = append(sessionIDs, session.ID)
	}

	// List active impersonations as the system admin
	resp, err := cluster.client.Get(cluster.httpEndpoint + "/api/v1/admin/impersonation/?active=true")
	require.NoError(t, err)
	defer resp.Body.Close()

	// Assert: Response status is 200 OK
	assert.Equal(t, http.StatusOK, resp.StatusCode, "Expected 200 OK")

	var result ListImpersonationsResponse
	err = json.NewDecoder(resp.Body).Decode(&result)
	require.NoError(t, err)

	// Assert: All created sessions are returned
	assert.GreaterOrEqual(t, len(result.Sessions), len(sessionIDs), "Should return at least the created sessions")

	// Verify our sessions are in the list
	returnedIDs := make(map[string]bool)
	for _, session := range result.Sessions {
		returnedIDs[session.ID] = true
		assert.Nil(t, session.EndedAt, "Active sessions should have nil EndedAt")
	}

	for _, id := range sessionIDs {
		assert.True(t, returnedIDs[id], "Created session should be in active list")
	}

	// Cleanup: End all sessions
	for _, id := range sessionIDs {
		req, err := http.NewRequest(
			http.MethodPost,
			cluster.httpEndpoint+"/api/v1/admin/impersonation/"+id+"/end",
			nil,
		)
		require.NoError(t, err)
		resp, err := fixture.admin.Do(req)
		require.NoError(t, err)
		resp.Body.Close()
	}
}

// TestAdmin_ListImpersonations_FilterByAdmin tests filtering impersonations by admin user.
func TestAdmin_ListImpersonations_FilterByAdmin(t *testing.T) {
	cluster := setupAdminCluster(t)
	defer cluster.Close()

	// Two logged-in admins, each in its own tenant
	admins := []*impersonationFixture{
		setupImpersonationFixture(t, cluster),
		setupImpersonationFixture(t, cluster),
	}

	// Each admin starts a session as itself
	for _, admin := range admins {
		admin.assign(t, cluster, "super_admin", "user:impersonate")
		resp := admin.startImpersonation(t, cluster, ImpersonationRequest{
			TargetUserID: admin.targetUserID,
			TenantID:     admin.tenantID,
			Reason:       "Testing filter",
			Duration:     "1h",
		})
		resp.Body.Close()
		require.Equal(t, http.StatusOK, resp.StatusCode, "Expected 200 OK for impersonation start")
	}

	// List impersonations for admin1 only
	admin1ID := admins[0].adminUserID
	resp, err := cluster.client.Get(cluster.httpEndpoint + "/api/v1/admin/impersonation/?admin_user_id=" + admin1ID)
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode)

	var result ListImpersonationsResponse
	err = json.NewDecoder(resp.Body).Decode(&result)
	require.NoError(t, err)

	// admin1's session is listed, and every returned session belongs to admin1
	require.NotEmpty(t, result.Sessions, "admin1's session should be listed")
	for _, session := range result.Sessions {
		assert.Equal(t, admin1ID, session.AdminUserID, "All sessions should belong to admin1")
	}
}

// ============================================================================
// Edge Cases and Validation Tests
// ============================================================================

// TestAdmin_CreateAssignment_AllRoles tests creating assignments for all valid roles.
func TestAdmin_CreateAssignment_AllRoles(t *testing.T) {
	cluster := setupAdminCluster(t)
	defer cluster.Close()

	roles := []string{
		"super_admin",
		"tenant_admin",
		"user_admin",
		"policy_admin",
		"audit_admin",
		"read_only",
	}

	for _, role := range roles {
		t.Run(role, func(t *testing.T) {
			userID := generateUniqueUserID()
			createReq := AdminAssignmentRequest{
				UserID:    userID,
				AdminRole: role,
				GrantedBy: "system",
			}

			created := createTestAssignment(t, cluster, createReq)
			defer deleteTestAssignment(t, cluster, created.ID)

			assert.Equal(t, role, created.AdminRole, "AdminRole should match")
			assert.True(t, created.Active, "Assignment should be active")
		})
	}
}

// TestAdmin_UpdateAssignment_Deactivate tests deactivating an assignment.
func TestAdmin_UpdateAssignment_Deactivate(t *testing.T) {
	cluster := setupAdminCluster(t)
	defer cluster.Close()

	// Create an assignment
	userID := generateUniqueUserID()
	createReq := AdminAssignmentRequest{
		UserID:    userID,
		AdminRole: "tenant_admin",
		GrantedBy: "system",
	}

	created := createTestAssignment(t, cluster, createReq)
	defer deleteTestAssignment(t, cluster, created.ID)

	// Deactivate the assignment
	active := false
	updateReq := UpdateAdminAssignmentRequest{
		Active: &active,
	}

	body, err := json.Marshal(updateReq)
	require.NoError(t, err)

	req, err := http.NewRequest(
		http.MethodPut,
		cluster.httpEndpoint+"/api/v1/admin/assignments/"+created.ID,
		bytes.NewReader(body),
	)
	require.NoError(t, err)
	req.Header.Set("Content-Type", "application/json")

	resp, err := cluster.client.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode)

	var updated AdminAssignmentResponse
	err = json.NewDecoder(resp.Body).Decode(&updated)
	require.NoError(t, err)

	// Assert: Assignment is deactivated
	assert.False(t, updated.Active, "Assignment should be deactivated")

	// Verify: Permission check should fail for inactive assignment
	checkReq := AdminCheckRequest{
		UserID:     userID,
		Permission: "user:read",
	}

	checkBody, err := json.Marshal(checkReq)
	require.NoError(t, err)

	checkResp, err := cluster.client.Post(
		cluster.httpEndpoint+"/api/v1/admin/check-permission",
		"application/json",
		bytes.NewReader(checkBody),
	)
	require.NoError(t, err)
	defer checkResp.Body.Close()

	var result AdminCheckResponse
	err = json.NewDecoder(checkResp.Body).Decode(&result)
	require.NoError(t, err)

	assert.False(t, result.Allowed, "Permission should be denied for inactive assignment")
}

// TestAdmin_CreateAssignment_WithResourceScopes tests creating assignment with resource scopes.
func TestAdmin_CreateAssignment_WithResourceScopes(t *testing.T) {
	cluster := setupAdminCluster(t)
	defer cluster.Close()

	userID := generateUniqueUserID()
	createReq := AdminAssignmentRequest{
		UserID:    userID,
		AdminRole: "user_admin",
		TenantIDs: []string{"tenant-a"},
		ResourceScopes: []ResourceScopeReq{
			{
				ResourceType:    "user",
				ResourcePattern: "user:engineering-*",
			},
			{
				ResourceType:    "group",
				ResourcePattern: "group:dev-*",
			},
		},
		GrantedBy: "system",
	}

	created := createTestAssignment(t, cluster, createReq)
	defer deleteTestAssignment(t, cluster, created.ID)

	// Assert: Resource scopes are preserved
	assert.Len(t, created.ResourceScopes, 2, "Should have 2 resource scopes")

	scopeTypes := make(map[string]string)
	for _, scope := range created.ResourceScopes {
		scopeTypes[scope.ResourceType] = scope.ResourcePattern
	}

	assert.Equal(t, "user:engineering-*", scopeTypes["user"], "User scope should match")
	assert.Equal(t, "group:dev-*", scopeTypes["group"], "Group scope should match")
}

// TestAdmin_CheckPermission_WithResourceContext tests permission check with resource context.
func TestAdmin_CheckPermission_WithResourceContext(t *testing.T) {
	cluster := setupAdminCluster(t)
	defer cluster.Close()

	// Create scoped admin
	userID := generateUniqueUserID()
	createReq := AdminAssignmentRequest{
		UserID:    userID,
		AdminRole: "user_admin",
		TenantIDs: []string{"tenant-a"},
		ResourceScopes: []ResourceScopeReq{
			{
				ResourceType:    "user",
				ResourcePattern: "user:engineering-*",
			},
		},
		GrantedBy: "system",
	}

	created := createTestAssignment(t, cluster, createReq)
	defer deleteTestAssignment(t, cluster, created.ID)

	// Test: Check permission for matching resource
	t.Run("MatchingResource", func(t *testing.T) {
		checkReq := AdminCheckRequest{
			UserID:       userID,
			Permission:   "user:update",
			TenantID:     "tenant-a",
			ResourceType: "user",
			ResourceID:   "engineering-alice",
		}

		body, err := json.Marshal(checkReq)
		require.NoError(t, err)

		resp, err := cluster.client.Post(
			cluster.httpEndpoint+"/api/v1/admin/check-permission",
			"application/json",
			bytes.NewReader(body),
		)
		require.NoError(t, err)
		defer resp.Body.Close()

		var result AdminCheckResponse
		err = json.NewDecoder(resp.Body).Decode(&result)
		require.NoError(t, err)

		// Should be allowed for matching pattern
		assert.True(t, result.Allowed, "Should be allowed for matching resource pattern")
	})

	// Test: Check permission for non-matching resource
	t.Run("NonMatchingResource", func(t *testing.T) {
		checkReq := AdminCheckRequest{
			UserID:       userID,
			Permission:   "user:update",
			TenantID:     "tenant-a",
			ResourceType: "user",
			ResourceID:   "sales-bob", // Doesn't match "engineering-*"
		}

		body, err := json.Marshal(checkReq)
		require.NoError(t, err)

		resp, err := cluster.client.Post(
			cluster.httpEndpoint+"/api/v1/admin/check-permission",
			"application/json",
			bytes.NewReader(body),
		)
		require.NoError(t, err)
		defer resp.Body.Close()

		var result AdminCheckResponse
		err = json.NewDecoder(resp.Body).Decode(&result)
		require.NoError(t, err)

		// Should be denied for non-matching pattern
		assert.False(t, result.Allowed, "Should be denied for non-matching resource pattern")
	})
}

// TestAdmin_Assignment_WithExpiry tests creating assignment with expiry and verifying expiration behavior.
func TestAdmin_Assignment_WithExpiry(t *testing.T) {
	cluster := setupAdminCluster(t)
	defer cluster.Close()

	// Create assignment with future expiry
	userID := generateUniqueUserID()
	futureTime := time.Now().Add(24 * time.Hour)
	createReq := AdminAssignmentRequest{
		UserID:    userID,
		AdminRole: "tenant_admin",
		ExpiresAt: &futureTime,
		GrantedBy: "system",
	}

	created := createTestAssignment(t, cluster, createReq)
	defer deleteTestAssignment(t, cluster, created.ID)

	// Assert: ExpiresAt is set
	assert.NotNil(t, created.ExpiresAt, "ExpiresAt should be set")

	// Check permission - should be allowed (not expired yet)
	checkReq := AdminCheckRequest{
		UserID:     userID,
		Permission: "user:read",
	}

	body, err := json.Marshal(checkReq)
	require.NoError(t, err)

	resp, err := cluster.client.Post(
		cluster.httpEndpoint+"/api/v1/admin/check-permission",
		"application/json",
		bytes.NewReader(body),
	)
	require.NoError(t, err)
	defer resp.Body.Close()

	var result AdminCheckResponse
	err = json.NewDecoder(resp.Body).Decode(&result)
	require.NoError(t, err)

	assert.True(t, result.Allowed, "Permission should be allowed for non-expired assignment")
}

// TestAdmin_Impersonation_AuditTrail tests that impersonation creates audit trail.
func TestAdmin_Impersonation_AuditTrail(t *testing.T) {
	cluster := setupAdminCluster(t)
	defer cluster.Close()

	fixture := setupImpersonationFixture(t, cluster)
	fixture.assign(t, cluster, "super_admin", "user:impersonate")

	// Start impersonation
	resp := fixture.startImpersonation(t, cluster, ImpersonationRequest{
		TargetUserID: fixture.targetUserID,
		TenantID:     fixture.tenantID,
		Reason:       "Audit trail test",
		Duration:     "30m",
	})
	defer resp.Body.Close()

	require.Equal(t, http.StatusOK, resp.StatusCode, "Expected 200 OK for impersonation start")

	var session ImpersonationSessionResponse
	err := json.NewDecoder(resp.Body).Decode(&session)
	require.NoError(t, err)

	// Assert: AuditLogID is set
	assert.NotEmpty(t, session.AuditLogID, "AuditLogID should be set for impersonation")

	// End impersonation to clean up
	endReq, err := http.NewRequest(
		http.MethodPost,
		cluster.httpEndpoint+"/api/v1/admin/impersonation/"+session.ID+"/end",
		nil,
	)
	require.NoError(t, err)
	endResp, err := fixture.admin.Do(endReq)
	require.NoError(t, err)
	endResp.Body.Close()
}

// TestAdmin_MultipleAssignments_SameUser tests a user with multiple assignments.
func TestAdmin_MultipleAssignments_SameUser(t *testing.T) {
	cluster := setupAdminCluster(t)
	defer cluster.Close()

	userID := generateUniqueUserID()

	// Create multiple assignments for the same user
	assignments := []AdminAssignmentRequest{
		{
			UserID:    userID,
			AdminRole: "tenant_admin",
			TenantIDs: []string{"tenant-a"},
			GrantedBy: "system",
		},
		{
			UserID:    userID,
			AdminRole: "audit_admin",
			TenantIDs: []string{"tenant-b"},
			GrantedBy: "system",
		},
	}

	var createdIDs []string
	for _, req := range assignments {
		created := createTestAssignment(t, cluster, req)
		createdIDs = append(createdIDs, created.ID)
	}
	defer func() {
		for _, id := range createdIDs {
			deleteTestAssignment(t, cluster, id)
		}
	}()

	// List assignments for user
	resp, err := cluster.client.Get(cluster.httpEndpoint + "/api/v1/admin/assignments?user_id=" + userID)
	require.NoError(t, err)
	defer resp.Body.Close()

	var result ListAssignmentsResponse
	err = json.NewDecoder(resp.Body).Decode(&result)
	require.NoError(t, err)

	// Assert: Both assignments are returned
	assert.Len(t, result.Assignments, 2, "Should have 2 assignments for user")

	// Check permission in tenant-a (should use tenant_admin)
	checkReq := AdminCheckRequest{
		UserID:     userID,
		Permission: "user:create",
		TenantID:   "tenant-a",
	}

	body, err := json.Marshal(checkReq)
	require.NoError(t, err)

	checkResp, err := cluster.client.Post(
		cluster.httpEndpoint+"/api/v1/admin/check-permission",
		"application/json",
		bytes.NewReader(body),
	)
	require.NoError(t, err)
	defer checkResp.Body.Close()

	var checkResult AdminCheckResponse
	err = json.NewDecoder(checkResp.Body).Decode(&checkResult)
	require.NoError(t, err)

	assert.True(t, checkResult.Allowed, "Should be allowed via tenant_admin assignment")
	assert.Equal(t, "tenant_admin", checkResult.AdminRole, "Should use tenant_admin role for tenant-a")
}

// TestAdmin_Pagination tests pagination for listing assignments.
func TestAdmin_Pagination(t *testing.T) {
	cluster := setupAdminCluster(t)
	defer cluster.Close()

	// Create multiple assignments
	testPrefix := uuid.New().String()[:8]
	var createdIDs []string

	for i := 0; i < 5; i++ {
		createReq := AdminAssignmentRequest{
			UserID:    fmt.Sprintf("%s-paginate-user-%d", testPrefix, i),
			AdminRole: "read_only",
			GrantedBy: "system",
		}
		created := createTestAssignment(t, cluster, createReq)
		createdIDs = append(createdIDs, created.ID)
	}
	defer func() {
		for _, id := range createdIDs {
			deleteTestAssignment(t, cluster, id)
		}
	}()

	// Test pagination with limit
	t.Run("WithLimit", func(t *testing.T) {
		resp, err := cluster.client.Get(cluster.httpEndpoint + "/api/v1/admin/assignments?limit=2")
		require.NoError(t, err)
		defer resp.Body.Close()

		assert.Equal(t, http.StatusOK, resp.StatusCode)

		var result ListAssignmentsResponse
		err = json.NewDecoder(resp.Body).Decode(&result)
		require.NoError(t, err)

		assert.LessOrEqual(t, len(result.Assignments), 2, "Should respect limit")
	})

	// Test pagination with offset
	t.Run("WithOffset", func(t *testing.T) {
		resp, err := cluster.client.Get(cluster.httpEndpoint + "/api/v1/admin/assignments?limit=2&offset=2")
		require.NoError(t, err)
		defer resp.Body.Close()

		assert.Equal(t, http.StatusOK, resp.StatusCode)

		var result ListAssignmentsResponse
		err = json.NewDecoder(resp.Body).Decode(&result)
		require.NoError(t, err)

		// Should return different results than first page
		assert.LessOrEqual(t, len(result.Assignments), 2, "Should respect limit")
	})
}
