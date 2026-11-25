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
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/iam"
	"github.com/jeremyhahn/go-quicbaas/pkg/tenantctx"
)

// asAdminCaller binds the request's caller to tenantID, the tenant the
// delegated-admin handlers act in.
func asAdminCaller(r *http.Request, tenantID string) *http.Request {
	return r.WithContext(tenantctx.WithTenant(r.Context(), tenantID))
}

// seedAdminTenant stores a super-admin assignment for adminID in tenantID
// and an impersonation session that admin holds there.
func seedAdminTenant(t *testing.T, manager *iam.AdminManager, adminID, targetID uint64, tenantID string) (*iam.AdminAssignment, *iam.ImpersonationSession) {
	t.Helper()
	ctx := context.Background()
	assignment, err := manager.CreateAssignment(ctx, iam.CreateAdminAssignmentOptions{
		UserID:      adminID,
		AdminRole:   iam.AdminRoleSuperAdmin,
		TenantIDs:   []string{tenantID},
		Permissions: []iam.AdminPermission{iam.PermUserImpersonate},
		GrantedBy:   "system",
	})
	require.NoError(t, err)
	session, err := manager.StartImpersonation(ctxWithTestUser(ctx, adminID, tenantID), &iam.StartImpersonationOptions{
		TargetUserID: targetID,
		TenantID:     tenantID,
		Reason:       "test",
		Duration:     time.Hour,
	})
	require.NoError(t, err)
	return assignment, session
}

type assignmentsBody struct {
	Assignments []*iam.AdminAssignment `json:"assignments"`
}

type sessionsBody struct {
	Sessions []*iam.ImpersonationSession `json:"sessions"`
}

func assignmentIDs(t *testing.T, body []byte) []string {
	t.Helper()
	var resp assignmentsBody
	require.NoError(t, json.Unmarshal(body, &resp))
	ids := make([]string, 0, len(resp.Assignments))
	for _, a := range resp.Assignments {
		ids = append(ids, a.ID)
	}
	return ids
}

func sessionIDs(t *testing.T, body []byte) []string {
	t.Helper()
	var resp sessionsBody
	require.NoError(t, json.Unmarshal(body, &resp))
	ids := make([]string, 0, len(resp.Sessions))
	for _, s := range resp.Sessions {
		ids = append(ids, s.ID)
	}
	return ids
}

func TestAdminHandlers_AnotherTenantsObjectIsNotFound(t *testing.T) {
	handler, mockSvc := newTestIAMHandler(t)
	manager := mockSvc.adminManager
	assignmentB, sessionB := seedAdminTenant(t, manager, 60001, 60002, "tenant-b")
	const callerTenant = "tenant-a"
	adminB := strconv.FormatUint(60001, 10)

	assertBUnchanged := func(t *testing.T) {
		t.Helper()
		got, err := manager.GetAssignment(context.Background(), assignmentB.ID)
		require.NoError(t, err)
		assert.Equal(t, []string{"tenant-b"}, got.TenantIDs)
	}

	t.Run("get", func(t *testing.T) {
		req := withChiParams(tenantRequest(callerTenant, http.MethodGet, "/api/v1/admin/assignments/"+assignmentB.ID, nil), map[string]string{"assignment_id": assignmentB.ID})
		rr := httptest.NewRecorder()
		handler.GetAdminAssignment(rr, req)
		assert.Equal(t, http.StatusNotFound, rr.Code)
	})

	t.Run("update", func(t *testing.T) {
		req := withChiParams(tenantRequest(callerTenant, http.MethodPut, "/api/v1/admin/assignments/"+assignmentB.ID,
			strings.NewReader(`{"tenant_ids":["tenant-a"]}`)), map[string]string{"assignment_id": assignmentB.ID})
		rr := httptest.NewRecorder()
		handler.UpdateAdminAssignment(rr, req)
		assert.Equal(t, http.StatusNotFound, rr.Code)
		assertBUnchanged(t)
	})

	t.Run("delete", func(t *testing.T) {
		req := withChiParams(tenantRequest(callerTenant, http.MethodDelete, "/api/v1/admin/assignments/"+assignmentB.ID, nil), map[string]string{"assignment_id": assignmentB.ID})
		rr := httptest.NewRecorder()
		handler.DeleteAdminAssignment(rr, req)
		assert.Equal(t, http.StatusNotFound, rr.Code)
		assertBUnchanged(t)
	})

	t.Run("list without a tenant filter", func(t *testing.T) {
		req := tenantRequest(callerTenant, http.MethodGet, "/api/v1/admin/assignments?tenant_id=", nil)
		rr := httptest.NewRecorder()
		handler.ListAdminAssignments(rr, req)
		require.Equal(t, http.StatusOK, rr.Code)
		assert.NotContains(t, assignmentIDs(t, rr.Body.Bytes()), assignmentB.ID)
	})

	t.Run("list naming another tenant", func(t *testing.T) {
		req := tenantRequest(callerTenant, http.MethodGet, "/api/v1/admin/assignments?tenant_id=tenant-b", nil)
		rr := httptest.NewRecorder()
		handler.ListAdminAssignments(rr, req)
		assert.Equal(t, http.StatusForbidden, rr.Code)
	})

	t.Run("user assignments", func(t *testing.T) {
		req := withChiParams(tenantRequest(callerTenant, http.MethodGet, "/api/v1/admin/users/"+adminB+"/assignments", nil), map[string]string{"user_id": adminB})
		rr := httptest.NewRecorder()
		handler.GetUserAssignments(rr, req)
		require.Equal(t, http.StatusOK, rr.Code)
		assert.Empty(t, assignmentIDs(t, rr.Body.Bytes()))
	})

	t.Run("create in another tenant", func(t *testing.T) {
		req := tenantRequest(callerTenant, http.MethodPost, "/api/v1/admin/assignments",
			strings.NewReader(`{"user_id":"60003","admin_role":"tenant_admin","tenant_ids":["tenant-b"],"granted_by":"tester"}`))
		rr := httptest.NewRecorder()
		handler.CreateAdminAssignment(rr, req)
		assert.Equal(t, http.StatusForbidden, rr.Code)
		stored, err := manager.ListAssignments(context.Background(), iam.ListAdminAssignmentsOptions{UserID: 60003})
		require.NoError(t, err)
		assert.Empty(t, stored)
	})

	t.Run("check permission in another tenant", func(t *testing.T) {
		body := `{"user_id":"60001","permission":"` + string(iam.PermUserImpersonate) + `","tenant_id":"tenant-b"}`
		req := tenantRequest(callerTenant, http.MethodPost, "/api/v1/admin/check-permission", strings.NewReader(body))
		rr := httptest.NewRecorder()
		handler.CheckAdminPermission(rr, req)
		assert.Equal(t, http.StatusForbidden, rr.Code)
	})

	t.Run("start impersonation in another tenant", func(t *testing.T) {
		req := tenantRequest(callerTenant, http.MethodPost, "/api/v1/admin/impersonation/start",
			strings.NewReader(`{"target_user_id":"60002","tenant_id":"tenant-b","reason":"test"}`))
		rr := httptest.NewRecorder()
		handler.StartImpersonation(rr, req)
		assert.Equal(t, http.StatusForbidden, rr.Code)
	})

	t.Run("end another tenant's session", func(t *testing.T) {
		req := withChiParams(tenantRequest(callerTenant, http.MethodPost, "/api/v1/admin/impersonation/"+sessionB.ID+"/end", nil), map[string]string{"session_id": sessionB.ID})
		rr := httptest.NewRecorder()
		handler.EndImpersonation(rr, req)
		assert.Equal(t, http.StatusNotFound, rr.Code)
		got, err := manager.GetImpersonationSession(context.Background(), sessionB.ID)
		require.NoError(t, err)
		assert.Nil(t, got.EndedAt)
	})

	t.Run("active impersonations", func(t *testing.T) {
		req := withChiParams(tenantRequest(callerTenant, http.MethodGet, "/api/v1/admin/users/"+adminB+"/impersonations", nil), map[string]string{"user_id": adminB})
		rr := httptest.NewRecorder()
		handler.ListActiveImpersonations(rr, req)
		require.Equal(t, http.StatusOK, rr.Code)
		assert.NotContains(t, sessionIDs(t, rr.Body.Bytes()), sessionB.ID)
	})

	t.Run("impersonations", func(t *testing.T) {
		req := tenantRequest(callerTenant, http.MethodGet, "/api/v1/admin/impersonation", nil)
		rr := httptest.NewRecorder()
		handler.ListImpersonations(rr, req)
		require.Equal(t, http.StatusOK, rr.Code)
		assert.NotContains(t, sessionIDs(t, rr.Body.Bytes()), sessionB.ID)
	})
}

func TestAdminHandlers_OwnTenantObjectIsServed(t *testing.T) {
	handler, mockSvc := newTestIAMHandler(t)
	manager := mockSvc.adminManager
	const callerTenant = "tenant-a"
	assignmentA, sessionA := seedAdminTenant(t, manager, 61001, 61002, callerTenant)
	adminA := strconv.FormatUint(61001, 10)

	t.Run("get", func(t *testing.T) {
		req := withChiParams(tenantRequest(callerTenant, http.MethodGet, "/api/v1/admin/assignments/"+assignmentA.ID, nil), map[string]string{"assignment_id": assignmentA.ID})
		rr := httptest.NewRecorder()
		handler.GetAdminAssignment(rr, req)
		assert.Equal(t, http.StatusOK, rr.Code)
	})

	t.Run("update", func(t *testing.T) {
		req := withChiParams(tenantRequest(callerTenant, http.MethodPut, "/api/v1/admin/assignments/"+assignmentA.ID,
			strings.NewReader(`{"tenant_ids":["tenant-a"]}`)), map[string]string{"assignment_id": assignmentA.ID})
		rr := httptest.NewRecorder()
		handler.UpdateAdminAssignment(rr, req)
		assert.Equal(t, http.StatusOK, rr.Code)
	})

	t.Run("list", func(t *testing.T) {
		req := tenantRequest(callerTenant, http.MethodGet, "/api/v1/admin/assignments?tenant_id=tenant-a", nil)
		rr := httptest.NewRecorder()
		handler.ListAdminAssignments(rr, req)
		require.Equal(t, http.StatusOK, rr.Code)
		assert.Contains(t, assignmentIDs(t, rr.Body.Bytes()), assignmentA.ID)
	})

	t.Run("user assignments", func(t *testing.T) {
		req := withChiParams(tenantRequest(callerTenant, http.MethodGet, "/api/v1/admin/users/"+adminA+"/assignments", nil), map[string]string{"user_id": adminA})
		rr := httptest.NewRecorder()
		handler.GetUserAssignments(rr, req)
		require.Equal(t, http.StatusOK, rr.Code)
		assert.Contains(t, assignmentIDs(t, rr.Body.Bytes()), assignmentA.ID)
	})

	t.Run("create in own tenant", func(t *testing.T) {
		req := tenantRequest(callerTenant, http.MethodPost, "/api/v1/admin/assignments",
			strings.NewReader(`{"user_id":"61003","admin_role":"tenant_admin","granted_by":"tester"}`))
		rr := httptest.NewRecorder()
		handler.CreateAdminAssignment(rr, req)
		require.Equal(t, http.StatusCreated, rr.Code)
		var created iam.AdminAssignment
		require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &created))
		assert.Equal(t, []string{callerTenant}, created.TenantIDs)
	})

	t.Run("check permission in own tenant", func(t *testing.T) {
		body := `{"user_id":"61001","permission":"` + string(iam.PermUserImpersonate) + `"}`
		req := tenantRequest(callerTenant, http.MethodPost, "/api/v1/admin/check-permission", strings.NewReader(body))
		rr := httptest.NewRecorder()
		handler.CheckAdminPermission(rr, req)
		require.Equal(t, http.StatusOK, rr.Code)
		var resp iam.AdminCheckResponse
		require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &resp))
		assert.True(t, resp.Allowed)
	})

	t.Run("start impersonation in own tenant", func(t *testing.T) {
		req := tenantRequest(callerTenant, http.MethodPost, "/api/v1/admin/impersonation/start",
			strings.NewReader(`{"target_user_id":"61004","tenant_id":"tenant-a","reason":"test"}`))
		req = req.WithContext(ctxWithTestUser(req.Context(), 61001, callerTenant))
		rr := httptest.NewRecorder()
		handler.StartImpersonation(rr, req)
		assert.Equal(t, http.StatusOK, rr.Code)
	})

	t.Run("active impersonations", func(t *testing.T) {
		req := withChiParams(tenantRequest(callerTenant, http.MethodGet, "/api/v1/admin/users/"+adminA+"/impersonations", nil), map[string]string{"user_id": adminA})
		rr := httptest.NewRecorder()
		handler.ListActiveImpersonations(rr, req)
		require.Equal(t, http.StatusOK, rr.Code)
		assert.Contains(t, sessionIDs(t, rr.Body.Bytes()), sessionA.ID)
	})

	t.Run("impersonations", func(t *testing.T) {
		req := tenantRequest(callerTenant, http.MethodGet, "/api/v1/admin/impersonation", nil)
		rr := httptest.NewRecorder()
		handler.ListImpersonations(rr, req)
		require.Equal(t, http.StatusOK, rr.Code)
		assert.Contains(t, sessionIDs(t, rr.Body.Bytes()), sessionA.ID)
	})

	t.Run("end own session", func(t *testing.T) {
		req := withChiParams(tenantRequest(callerTenant, http.MethodPost, "/api/v1/admin/impersonation/"+sessionA.ID+"/end", nil), map[string]string{"session_id": sessionA.ID})
		rr := httptest.NewRecorder()
		handler.EndImpersonation(rr, req)
		assert.Equal(t, http.StatusOK, rr.Code)
	})

	t.Run("delete", func(t *testing.T) {
		req := withChiParams(tenantRequest(callerTenant, http.MethodDelete, "/api/v1/admin/assignments/"+assignmentA.ID, nil), map[string]string{"assignment_id": assignmentA.ID})
		rr := httptest.NewRecorder()
		handler.DeleteAdminAssignment(rr, req)
		assert.Equal(t, http.StatusNoContent, rr.Code)
	})
}
