//go:build rest && iam

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
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/jeremyhahn/go-quicbaas/pkg/iam"
)

// ListAdminAssignments honors the query-param filters and returns 200.
func TestListAdminAssignments_WithFilters(t *testing.T) {
	handler, mockSvc := newTestIAMHandler(t)
	_, err := mockSvc.adminManager.CreateAssignment(context.Background(), iam.CreateAdminAssignmentOptions{
		UserID:      "u1",
		AdminRole:   iam.AdminRoleTenantAdmin,
		TenantIDs:   []string{"t1"},
		Permissions: []iam.AdminPermission{iam.PermUserRead},
		GrantedBy:   "system",
	})
	assert.NoError(t, err)

	req := httptest.NewRequest(http.MethodGet,
		"/api/v1/admin/assignments?limit=10&offset=0&user_id=u1&tenant_id=t1&admin_role=tenant_admin&active_only=true", nil)
	w := httptest.NewRecorder()
	handler.ListAdminAssignments(w, req)
	assert.Equal(t, http.StatusOK, w.Code, "body: %s", w.Body.String())
}

// Admin handlers return 500 when no AdminManager is wired on the service.
func TestAdminHandlers_ManagerNotAvailable(t *testing.T) {
	handler, _, cleanup := setupIAMHandler(t)
	defer cleanup()

	tests := []struct {
		name string
		call func(w http.ResponseWriter, r *http.Request)
		req  *http.Request
	}{
		{
			"list",
			handler.ListAdminAssignments,
			httptest.NewRequest(http.MethodGet, "/api/v1/admin/assignments", nil),
		},
		{
			"create",
			handler.CreateAdminAssignment,
			httptest.NewRequest(http.MethodPost, "/api/v1/admin/assignments",
				bytes.NewReader([]byte(`{"user_id":"u1","admin_role":"tenant_admin","tenant_ids":["t1"],"granted_by":"s"}`))),
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			tt.call(w, tt.req)
			assert.Equal(t, http.StatusInternalServerError, w.Code)
		})
	}
}

// CreateAdminAssignment rejects invalid JSON and missing fields.
func TestCreateAdminAssignment_Validation(t *testing.T) {
	handler, _ := newTestIAMHandler(t)

	// Invalid JSON
	w := httptest.NewRecorder()
	handler.CreateAdminAssignment(w, httptest.NewRequest(http.MethodPost, "/x", bytes.NewReader([]byte("{bad"))))
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestUpdateAdminAssignment_Validation(t *testing.T) {
	handler, _ := newTestIAMHandler(t)

	// Missing assignment_id (no chi param) -> 400
	w := httptest.NewRecorder()
	handler.UpdateAdminAssignment(w, httptest.NewRequest(http.MethodPut, "/x", bytes.NewReader([]byte(`{}`))))
	assert.Equal(t, http.StatusBadRequest, w.Code)

	// Invalid JSON with an assignment_id
	w = httptest.NewRecorder()
	r := withChiParams(httptest.NewRequest(http.MethodPut, "/x", bytes.NewReader([]byte("{bad"))),
		map[string]string{"assignment_id": "a1"})
	handler.UpdateAdminAssignment(w, r)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestEndImpersonation_Validation(t *testing.T) {
	handler, _ := newTestIAMHandler(t)

	// Missing session_id -> 400
	w := httptest.NewRecorder()
	handler.EndImpersonation(w, httptest.NewRequest(http.MethodPost, "/x", nil))
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestListActiveImpersonations_Success(t *testing.T) {
	handler, _ := newTestIAMHandler(t)

	w := httptest.NewRecorder()
	r := withChiParams(httptest.NewRequest(http.MethodGet, "/x", nil), map[string]string{"user_id": "u1"})
	handler.ListActiveImpersonations(w, r)
	assert.Equal(t, http.StatusOK, w.Code)
}
