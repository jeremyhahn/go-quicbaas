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
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/iam"
	"github.com/jeremyhahn/go-quicbaas/pkg/tenantctx"
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

func auditDeleteReq(userID uint64) *http.Request {
	r := systemRequest(http.MethodDelete,
		"/api/v1/iam/audit?tenant_id=tenant1&before=2024-01-01T00:00:00Z", nil)
	// Bind the caller's authenticated tenant to match the requested
	// tenant_id — with the #356 operator lift removed, ResolveRequestTenant
	// confines every caller to its own tenant, so an unbound caller tenant
	// would 403 before the stage each test means to exercise.
	r = r.WithContext(ctxWithCallerTenant(r.Context(), "tenant1"))
	if userID != 0 {
		r = r.WithContext(ctxWithTestUser(r.Context(), userID, "tenant1"))
	}
	return r
}

func TestDeleteOldAuditEvents_Validation(t *testing.T) {
	handler, _, cleanup := setupAuditDeleteHandler(t)
	defer cleanup()

	// Missing tenant_id no longer 400s: it defaults to the caller's own
	// authenticated tenant (iam.ResolveRequestTenant) — empty in this
	// unauthenticated test context — and the request proceeds past tenant
	// resolution and the "before" parse (present and valid) to the next
	// real check, requireSubject, which 401s with no authenticated
	// caller on the request.
	w := httptest.NewRecorder()
	handler.DeleteOldAuditEvents(w, systemRequest(http.MethodDelete, "/api/v1/iam/audit?before=2024-01-01T00:00:00Z", nil))
	assert.Equal(t, http.StatusUnauthorized, w.Code)

	// Missing before -> 400. The caller tenant is bound to match the
	// requested tenant_id so the request clears tenant scoping (#356).
	w = httptest.NewRecorder()
	req := systemRequest(http.MethodDelete, "/api/v1/iam/audit?tenant_id=t", nil)
	req = req.WithContext(ctxWithCallerTenant(req.Context(), "t"))
	handler.DeleteOldAuditEvents(w, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)

	// Invalid before format -> 400
	w = httptest.NewRecorder()
	req = systemRequest(http.MethodDelete, "/api/v1/iam/audit?tenant_id=t&before=not-a-date", nil)
	req = req.WithContext(ctxWithCallerTenant(req.Context(), "t"))
	handler.DeleteOldAuditEvents(w, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestDeleteOldAuditEvents_Unauthenticated(t *testing.T) {
	handler, _, cleanup := setupAuditDeleteHandler(t)
	defer cleanup()

	w := httptest.NewRecorder()
	handler.DeleteOldAuditEvents(w, auditDeleteReq(0)) // no user on context
	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

func TestDeleteOldAuditEvents_Forbidden(t *testing.T) {
	handler, _, cleanup := setupAuditDeleteHandler(t)
	defer cleanup()

	// Authenticated user with no admin assignment -> CheckPermission denies -> 403.
	w := httptest.NewRecorder()
	handler.DeleteOldAuditEvents(w, auditDeleteReq(70002))
	assert.Equal(t, http.StatusForbidden, w.Code)
}

func TestDeleteOldAuditEvents_Success(t *testing.T) {
	handler, admin, cleanup := setupAuditDeleteHandler(t)
	defer cleanup()

	// Grant the user the audit:delete permission for tenant1.
	_, err := admin.CreateAssignment(tenantctx.WithSystem(context.Background()), iam.CreateAdminAssignmentOptions{
		UserID:      70001,
		AdminRole:   iam.AdminRoleTenantAdmin,
		TenantIDs:   []string{"tenant1"},
		Permissions: []iam.AdminPermission{iam.PermAuditDelete},
		GrantedBy:   "super",
	})
	require.NoError(t, err)

	w := httptest.NewRecorder()
	handler.DeleteOldAuditEvents(w, auditDeleteReq(70001))
	require.Equal(t, http.StatusOK, w.Code, "body: %s", w.Body.String())
	assert.Contains(t, w.Body.String(), "deleted_count")
}
