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
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/iam"
)

// An impersonation header with an unknown session ID is rejected with 401.
func TestImpersonationMiddleware_InvalidSession(t *testing.T) {
	handler, _ := newTestIAMHandler(t)

	nextCalled := false
	next := http.HandlerFunc(func(http.ResponseWriter, *http.Request) { nextCalled = true })

	req := httptest.NewRequest(http.MethodGet, "/api/v1/resource", nil)
	req.Header.Set("X-Impersonation-Session-ID", "does-not-exist")
	w := httptest.NewRecorder()
	handler.ImpersonationMiddleware(next).ServeHTTP(w, req)

	assert.Equal(t, http.StatusUnauthorized, w.Code)
	assert.False(t, nextCalled)
}

// A valid active impersonation session enriches the context and passes through.
func TestImpersonationMiddleware_ValidSession(t *testing.T) {
	handler, mockSvc := newTestIAMHandler(t)
	ctx := context.Background()

	_, err := mockSvc.adminManager.CreateAssignment(ctx, iam.CreateAdminAssignmentOptions{
		UserID:      "admin_user",
		AdminRole:   iam.AdminRoleSuperAdmin,
		TenantIDs:   []string{"tenant_1"},
		Permissions: []iam.AdminPermission{iam.PermUserImpersonate},
		GrantedBy:   "system",
	})
	require.NoError(t, err)

	session, err := mockSvc.adminManager.StartImpersonation(ctx, &iam.StartImpersonationOptions{
		AdminUserID:  "admin_user",
		TargetUserID: "target_user",
		TenantID:     "tenant_1",
		Reason:       "support",
		Duration:     time.Hour,
	})
	require.NoError(t, err)

	gotContext := false
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if iam.GetImpersonationContext(r.Context()) != nil {
			gotContext = true
		}
		w.WriteHeader(http.StatusOK)
	})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/resource", nil)
	req.Header.Set("X-Impersonation-Session-ID", session.ID)
	w := httptest.NewRecorder()
	handler.ImpersonationMiddleware(next).ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code)
	assert.True(t, gotContext, "impersonation context must be on the request")
}

// ImpersonationAuditMiddleware records the operation when an impersonation
// context is present on the request.
func TestImpersonationAuditMiddleware_Records(t *testing.T) {
	handler, mockSvc := newTestIAMHandler(t)
	ctx := context.Background()

	_, err := mockSvc.adminManager.CreateAssignment(ctx, iam.CreateAdminAssignmentOptions{
		UserID:      "admin_user",
		AdminRole:   iam.AdminRoleSuperAdmin,
		TenantIDs:   []string{"tenant_1"},
		Permissions: []iam.AdminPermission{iam.PermUserImpersonate},
		GrantedBy:   "system",
	})
	require.NoError(t, err)

	session, err := mockSvc.adminManager.StartImpersonation(ctx, &iam.StartImpersonationOptions{
		AdminUserID: "admin_user", TargetUserID: "target", TenantID: "tenant_1",
		Reason: "support", Duration: time.Hour,
	})
	require.NoError(t, err)

	impCtx := iam.ImpersonationContextFromSession(session)
	reqCtx := iam.WithImpersonationContext(context.Background(), impCtx)

	nextCalled := false
	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		nextCalled = true
		w.WriteHeader(http.StatusOK)
	})
	req := httptest.NewRequest(http.MethodGet, "/api/v1/resource", nil).WithContext(reqCtx)
	w := httptest.NewRecorder()
	handler.ImpersonationAuditMiddleware(next).ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code)
	require.True(t, nextCalled)
}
