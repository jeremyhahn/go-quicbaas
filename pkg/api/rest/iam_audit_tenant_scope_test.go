//go:build rest

// Copyright (c) 2026 Jeremy Hahn
// Copyright (c) 2026 Automate The Things, LLC
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
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"

	"github.com/jeremyhahn/go-quicbaas/pkg/iam"
	"github.com/jeremyhahn/go-quicbaas/pkg/tenantctx"
	"github.com/jeremyhahn/go-quicbaas/pkg/testutil"
)

// ctxWithCallerTenant binds a caller tenant into ctx exactly as the
// production auth middleware does (iam.ContextWithTenant, injectPrincipal
// — pkg/api/rest/auth_support.go), the piece ctxWithTestSubject alone does
// NOT bind. Tests that exercise iam.ResolveRequestTenant (the read-path
// tenant-scoping guard) need both: ctxWithTestSubject for requireSubject
// and this for the tenant check.
func ctxWithCallerTenant(parent context.Context, tenantID string) context.Context {
	return tenantctx.WithTenant(parent, tenantID)
}

// TestListAuditEvents_TenantScoping is the P5.2 closing-gap guard: an
// authenticated caller may list its own tenant's audit events (or omit
// tenant_id, defaulting to it) but a DIFFERENT tenant_id from ANY caller —
// system-tenant included (#356) — is rejected 403 before the audit logger
// is ever reached — proven here by using a service double with NO
// GetAuditLogger implementation at all, so any code path that reached
// storage would fail differently.
func TestListAuditEvents_TenantScoping(t *testing.T) {
	t.Run("cross-tenant request from a non-system caller is rejected and leaks no events", func(t *testing.T) {
		handler, _, cleanup := setupIAMHandler(t)
		defer cleanup()

		req := httptest.NewRequest(http.MethodGet, "/api/v1/iam/audit?tenant_id=tenant-b", nil)
		req = req.WithContext(ctxWithCallerTenant(req.Context(), "tenant-a"))
		w := httptest.NewRecorder()

		handler.ListAuditEvents(w, req)

		require.Equal(t, http.StatusForbidden, w.Code, "body: %s", w.Body.String())
		assert.NotContains(t, w.Body.String(), "audit logger not available",
			"a rejected cross-tenant request must never reach the audit logger accessor")
	})

	t.Run("own tenant_id is honored (reaches the next real dependency)", func(t *testing.T) {
		handler, _, cleanup := setupIAMHandler(t)
		defer cleanup()

		req := httptest.NewRequest(http.MethodGet, "/api/v1/iam/audit?tenant_id=tenant-a", nil)
		req = req.WithContext(ctxWithCallerTenant(req.Context(), "tenant-a"))
		w := httptest.NewRecorder()

		handler.ListAuditEvents(w, req)

		assert.Equal(t, http.StatusInternalServerError, w.Code)
		assert.Contains(t, w.Body.String(), "audit logger not available")
	})

	t.Run("system-tenant caller is denied another tenant's chain (no operator lift, #356)", func(t *testing.T) {
		handler, _, cleanup := setupIAMHandler(t)
		defer cleanup()

		req := httptest.NewRequest(http.MethodGet, "/api/v1/iam/audit?tenant_id=tenant-b", nil)
		req = req.WithContext(ctxWithCallerTenant(req.Context(), qrdbsdk.SystemTenantID))
		w := httptest.NewRecorder()

		handler.ListAuditEvents(w, req)

		require.Equal(t, http.StatusForbidden, w.Code, "body: %s", w.Body.String())
		assert.NotContains(t, w.Body.String(), "audit logger not available",
			"a rejected cross-tenant request must never reach the audit logger accessor")
	})

	t.Run("system-tenant caller may still read its own system chain", func(t *testing.T) {
		handler, _, cleanup := setupIAMHandler(t)
		defer cleanup()

		req := httptest.NewRequest(http.MethodGet, "/api/v1/iam/audit?tenant_id="+qrdbsdk.SystemTenantID, nil)
		req = req.WithContext(ctxWithCallerTenant(req.Context(), qrdbsdk.SystemTenantID))
		w := httptest.NewRecorder()

		handler.ListAuditEvents(w, req)

		assert.Equal(t, http.StatusInternalServerError, w.Code)
		assert.Contains(t, w.Body.String(), "audit logger not available")
	})
}

// TestGetAuditEvent_TenantScoping mirrors TestListAuditEvents_TenantScoping
// for the single-event lookup.
func TestGetAuditEvent_TenantScoping(t *testing.T) {
	t.Run("cross-tenant request from a non-system caller is rejected and leaks no events", func(t *testing.T) {
		handler, _, cleanup := setupIAMHandler(t)
		defer cleanup()

		req := httptest.NewRequest(http.MethodGet, "/api/v1/iam/audit/ev1?tenant_id=tenant-b", nil)
		req = withChiParams(req, map[string]string{"id": "ev1"})
		req = req.WithContext(ctxWithCallerTenant(req.Context(), "tenant-a"))
		w := httptest.NewRecorder()

		handler.GetAuditEvent(w, req)

		require.Equal(t, http.StatusForbidden, w.Code, "body: %s", w.Body.String())
		assert.NotContains(t, w.Body.String(), "audit logger not available")
	})

	t.Run("system-tenant caller is denied another tenant's event (no operator lift, #356)", func(t *testing.T) {
		handler, _, cleanup := setupIAMHandler(t)
		defer cleanup()

		req := httptest.NewRequest(http.MethodGet, "/api/v1/iam/audit/ev1?tenant_id=tenant-b", nil)
		req = withChiParams(req, map[string]string{"id": "ev1"})
		req = req.WithContext(ctxWithCallerTenant(req.Context(), qrdbsdk.SystemTenantID))
		w := httptest.NewRecorder()

		handler.GetAuditEvent(w, req)

		require.Equal(t, http.StatusForbidden, w.Code, "body: %s", w.Body.String())
		assert.NotContains(t, w.Body.String(), "audit logger not available")
	})
}

// TestDeleteOldAuditEvents_TenantScoping proves DELETE enforces the SAME
// tenant-scoping rule as the read path, running BEFORE the audit:delete
// admin-permission check: ANY cross-tenant request — system-tenant caller
// included — is rejected 403 without ever reaching requireSubject or the
// AdminManager permission check, and a same-tenant request with the
// audit:delete grant still succeeds exactly as
// TestDeleteOldAuditEvents_Success already proves for the ungated case.
func TestDeleteOldAuditEvents_TenantScoping(t *testing.T) {
	t.Run("cross-tenant request from a non-system caller is rejected before auth/permission checks", func(t *testing.T) {
		handler, _, cleanup := setupAuditDeleteHandler(t)
		defer cleanup()

		// No subject is bound on the request at all: if the tenant check
		// did not run first, this would 401 (requireSubject) rather than
		// 403 (tenant scope), proving ordering as well as outcome.
		req := httptest.NewRequest(http.MethodDelete,
			"/api/v1/iam/audit?tenant_id=tenant-b&before=2024-01-01T00:00:00Z", nil)
		req = req.WithContext(ctxWithCallerTenant(req.Context(), "tenant-a"))
		w := httptest.NewRecorder()

		handler.DeleteOldAuditEvents(w, req)

		require.Equal(t, http.StatusForbidden, w.Code, "body: %s", w.Body.String())
	})

	t.Run("system-tenant caller is denied another tenant's chain even with an audit:delete grant (#356)", func(t *testing.T) {
		handler, admin, cleanup := setupAuditDeleteHandler(t)
		defer cleanup()

		// The grant exists and names the target tenant, so this proves the
		// tenant-scope gate fires first: the denial is the operator-isolation
		// rule (no system-tenant lift), not a missing permission.
		_, err := admin.CreateAssignment(context.Background(), iam.CreateAdminAssignmentOptions{
			UserID:      70010,
			AdminRole:   iam.AdminRoleTenantAdmin,
			TenantIDs:   []string{"tenant-b"},
			Permissions: []iam.AdminPermission{iam.PermAuditDelete},
			GrantedBy:   "super",
		})
		require.NoError(t, err)

		req := httptest.NewRequest(http.MethodDelete,
			"/api/v1/iam/audit?tenant_id=tenant-b&before=2024-01-01T00:00:00Z", nil)
		req = req.WithContext(ctxWithTestUser(req.Context(), 70010, "system"))
		req = req.WithContext(ctxWithCallerTenant(req.Context(), qrdbsdk.SystemTenantID))
		w := httptest.NewRecorder()

		handler.DeleteOldAuditEvents(w, req)

		require.Equal(t, http.StatusForbidden, w.Code, "body: %s", w.Body.String())
	})

	t.Run("system-tenant caller with the audit:delete grant may still prune its own system chain", func(t *testing.T) {
		handler, admin, cleanup := setupAuditDeleteHandler(t)
		defer cleanup()

		_, err := admin.CreateAssignment(context.Background(), iam.CreateAdminAssignmentOptions{
			UserID:      70010,
			AdminRole:   iam.AdminRoleTenantAdmin,
			TenantIDs:   []string{qrdbsdk.SystemTenantID},
			Permissions: []iam.AdminPermission{iam.PermAuditDelete},
			GrantedBy:   "super",
		})
		require.NoError(t, err)

		req := httptest.NewRequest(http.MethodDelete,
			"/api/v1/iam/audit?tenant_id="+qrdbsdk.SystemTenantID+"&before=2024-01-01T00:00:00Z", nil)
		req = req.WithContext(ctxWithTestUser(req.Context(), 70010, "system"))
		req = req.WithContext(ctxWithCallerTenant(req.Context(), qrdbsdk.SystemTenantID))
		w := httptest.NewRecorder()

		handler.DeleteOldAuditEvents(w, req)

		require.Equal(t, http.StatusOK, w.Code, "body: %s", w.Body.String())
		assert.Contains(t, w.Body.String(), "deleted_count")
	})
}

// TestRecordAuditEvent_TenantScoping proves the REST write path applies the
// SAME rule as the read path (iam.ResolveRequestTenant): an event lands in
// the caller's own authenticated tenant; a body tenant_id naming any other
// tenant is rejected 403 — from a system-tenant caller too (#356) — and the
// event never reaches the audit logger.
func TestRecordAuditEvent_TenantScoping(t *testing.T) {
	newRecordReq := func(body, callerTenant string) *http.Request {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/iam/audit", strings.NewReader(body))
		return req.WithContext(ctxWithCallerTenant(req.Context(), callerTenant))
	}
	eventBody := `{"event_type":"authentication","category":"authentication","action":"login","outcome":"success"}`

	t.Run("cross-tenant tenant_id from a non-system caller is rejected and nothing is recorded", func(t *testing.T) {
		al := &mockAuditLogger91{}
		handler, cleanup := setupHandlerWithService(t, &mockServiceWithAudit{QuicBaaS: testutil.NewMockQuicBaaS(), loggerInst: al})
		defer cleanup()

		w := httptest.NewRecorder()
		handler.RecordAuditEvent(w, newRecordReq(`{"tenant_id":"tenant-b","event":`+eventBody+`}`, "tenant-a"))

		require.Equal(t, http.StatusForbidden, w.Code, "body: %s", w.Body.String())
		assert.Empty(t, al.events, "a rejected cross-tenant record must never reach the audit logger")
	})

	t.Run("cross-tenant tenant_id from the system tenant is rejected (no operator lift, #356)", func(t *testing.T) {
		al := &mockAuditLogger91{}
		handler, cleanup := setupHandlerWithService(t, &mockServiceWithAudit{QuicBaaS: testutil.NewMockQuicBaaS(), loggerInst: al})
		defer cleanup()

		w := httptest.NewRecorder()
		handler.RecordAuditEvent(w, newRecordReq(`{"tenant_id":"tenant-b","event":`+eventBody+`}`, qrdbsdk.SystemTenantID))

		require.Equal(t, http.StatusForbidden, w.Code, "body: %s", w.Body.String())
		assert.Empty(t, al.events, "a rejected cross-tenant record must never reach the audit logger")
	})

	t.Run("caller's own tenant_id is honored and stamped onto the event", func(t *testing.T) {
		al := &mockAuditLogger91{}
		handler, cleanup := setupHandlerWithService(t, &mockServiceWithAudit{QuicBaaS: testutil.NewMockQuicBaaS(), loggerInst: al})
		defer cleanup()

		w := httptest.NewRecorder()
		handler.RecordAuditEvent(w, newRecordReq(`{"tenant_id":"tenant-a","event":`+eventBody+`}`, "tenant-a"))

		require.Equal(t, http.StatusCreated, w.Code, "body: %s", w.Body.String())
		require.Len(t, al.events, 1)
		assert.Equal(t, "tenant-a", al.events[0].TenantID)
	})

	t.Run("omitted tenant_id defaults to the caller's own tenant", func(t *testing.T) {
		al := &mockAuditLogger91{}
		handler, cleanup := setupHandlerWithService(t, &mockServiceWithAudit{QuicBaaS: testutil.NewMockQuicBaaS(), loggerInst: al})
		defer cleanup()

		w := httptest.NewRecorder()
		handler.RecordAuditEvent(w, newRecordReq(`{"event":`+eventBody+`}`, "tenant-a"))

		require.Equal(t, http.StatusCreated, w.Code, "body: %s", w.Body.String())
		require.Len(t, al.events, 1)
		assert.Equal(t, "tenant-a", al.events[0].TenantID)
	})
}
