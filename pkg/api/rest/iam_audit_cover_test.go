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
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/iam"
	"github.com/jeremyhahn/go-quicbaas/pkg/testutil"
)

// adminAuditService exposes both an AdminManager and an AuditLogger.
type adminAuditService struct {
	*testutil.MockQuicBaaS
	admin *iam.AdminManager
	audit iam.AuditLogger
}

func (s *adminAuditService) GetAdminManager(_ context.Context) (*iam.AdminManager, error) {
	return s.admin, nil
}

func (s *adminAuditService) GetAuditLogger(_ context.Context) (iam.AuditLogger, error) {
	return s.audit, nil
}

func setupAuditDeleteHandler(t *testing.T) (*IAMHandler, *iam.AdminManager, func()) {
	t.Helper()
	admin := newTestAdminManager(t)
	mockSvc := &adminAuditService{
		MockQuicBaaS: testutil.NewMockQuicBaaS(),
		admin:        admin,
		audit:        &captureAuditLogger{},
	}
	server := &Server{service: mockSvc}
	handler := NewIAMHandler(IAMHandlerDependencies{Server: server})
	return handler, admin, func() {}
}

func auditDeleteReq(userSub string) *http.Request {
	r := httptest.NewRequest(http.MethodDelete,
		"/api/v1/iam/audit?tenant_id=tenant1&before=2024-01-01T00:00:00Z", nil)
	if userSub != "" {
		ctx := context.WithValue(r.Context(), ContextKeyUser, &iam.UserInfoResponse{Sub: userSub})
		r = r.WithContext(ctx)
	}
	return r
}

func TestDeleteOldAuditEvents_Validation(t *testing.T) {
	handler, _, cleanup := setupAuditDeleteHandler(t)
	defer cleanup()

	// Missing tenant_id -> 400
	w := httptest.NewRecorder()
	handler.DeleteOldAuditEvents(w, httptest.NewRequest(http.MethodDelete, "/api/v1/iam/audit?before=2024-01-01T00:00:00Z", nil))
	assert.Equal(t, http.StatusBadRequest, w.Code)

	// Missing before -> 400
	w = httptest.NewRecorder()
	handler.DeleteOldAuditEvents(w, httptest.NewRequest(http.MethodDelete, "/api/v1/iam/audit?tenant_id=t", nil))
	assert.Equal(t, http.StatusBadRequest, w.Code)

	// Invalid before format -> 400
	w = httptest.NewRecorder()
	handler.DeleteOldAuditEvents(w, httptest.NewRequest(http.MethodDelete, "/api/v1/iam/audit?tenant_id=t&before=not-a-date", nil))
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestDeleteOldAuditEvents_Unauthenticated(t *testing.T) {
	handler, _, cleanup := setupAuditDeleteHandler(t)
	defer cleanup()

	w := httptest.NewRecorder()
	handler.DeleteOldAuditEvents(w, auditDeleteReq("")) // no user on context
	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

func TestDeleteOldAuditEvents_Forbidden(t *testing.T) {
	handler, _, cleanup := setupAuditDeleteHandler(t)
	defer cleanup()

	// Authenticated user with no admin assignment -> CheckPermission denies -> 403.
	w := httptest.NewRecorder()
	handler.DeleteOldAuditEvents(w, auditDeleteReq("user-no-perm"))
	assert.Equal(t, http.StatusForbidden, w.Code)
}

func TestDeleteOldAuditEvents_Success(t *testing.T) {
	handler, admin, cleanup := setupAuditDeleteHandler(t)
	defer cleanup()

	// Grant the user the audit:delete permission for tenant1.
	_, err := admin.CreateAssignment(context.Background(), iam.CreateAdminAssignmentOptions{
		UserID:      "audit-admin",
		AdminRole:   iam.AdminRoleTenantAdmin,
		TenantIDs:   []string{"tenant1"},
		Permissions: []iam.AdminPermission{iam.PermAuditDelete},
		GrantedBy:   "super",
	})
	require.NoError(t, err)

	w := httptest.NewRecorder()
	handler.DeleteOldAuditEvents(w, auditDeleteReq("audit-admin"))
	require.Equal(t, http.StatusOK, w.Code, "body: %s", w.Body.String())
	assert.Contains(t, w.Body.String(), "deleted_count")
}
