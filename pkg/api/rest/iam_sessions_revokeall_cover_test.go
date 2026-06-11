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

	"github.com/jeremyhahn/go-quicbaas/pkg/iam"
	"github.com/jeremyhahn/go-quicbaas/pkg/testutil"
)

func revokeAllCtx() context.Context {
	ctx := context.WithValue(context.Background(), "user_id", "u1")
	return context.WithValue(ctx, "tenant_id", "t1")
}

// RevokeAllSessions with no except param revokes all sessions for the user.
func TestRevokeAllSessions_All(t *testing.T) {
	mgr := &mockSessionManager{}
	svc := &mockServiceWithSessions{QuicBaaS: testutil.NewMockQuicBaaS(), mgr: mgr}
	handler, cleanup := setupHandlerWithService(t, svc)
	defer cleanup()

	req := httptest.NewRequest(http.MethodDelete, "/api/v1/iam/sessions", nil).WithContext(revokeAllCtx())
	w := httptest.NewRecorder()
	handler.RevokeAllSessions(w, req)
	assert.Equal(t, http.StatusNoContent, w.Code)
}

// RevokeAllSessions with an except session belonging to the user succeeds.
func TestRevokeAllSessions_ExceptOwn(t *testing.T) {
	mgr := &mockSessionManager{sessions: []*iam.SessionExt{
		{ID: "keep", UserID: "u1", TenantID: "t1"},
	}}
	svc := &mockServiceWithSessions{QuicBaaS: testutil.NewMockQuicBaaS(), mgr: mgr}
	handler, cleanup := setupHandlerWithService(t, svc)
	defer cleanup()

	req := httptest.NewRequest(http.MethodDelete, "/api/v1/iam/sessions?except=keep", nil).WithContext(revokeAllCtx())
	w := httptest.NewRecorder()
	handler.RevokeAllSessions(w, req)
	assert.Equal(t, http.StatusNoContent, w.Code)
}

// RevokeAllSessions with an except session owned by a different user -> 403.
func TestRevokeAllSessions_ExceptForeign(t *testing.T) {
	mgr := &mockSessionManager{sessions: []*iam.SessionExt{
		{ID: "other", UserID: "victim", TenantID: "t1"},
	}}
	svc := &mockServiceWithSessions{QuicBaaS: testutil.NewMockQuicBaaS(), mgr: mgr}
	handler, cleanup := setupHandlerWithService(t, svc)
	defer cleanup()

	req := httptest.NewRequest(http.MethodDelete, "/api/v1/iam/sessions?except=other", nil).WithContext(revokeAllCtx())
	w := httptest.NewRecorder()
	handler.RevokeAllSessions(w, req)
	assert.Equal(t, http.StatusForbidden, w.Code)
}

// User and tenant supplied via query params (CLI path) also work.
func TestRevokeAllSessions_QueryParams(t *testing.T) {
	mgr := &mockSessionManager{}
	svc := &mockServiceWithSessions{QuicBaaS: testutil.NewMockQuicBaaS(), mgr: mgr}
	handler, cleanup := setupHandlerWithService(t, svc)
	defer cleanup()

	req := httptest.NewRequest(http.MethodDelete, "/api/v1/iam/sessions?user_id=u1&tenant_id=t1", nil)
	w := httptest.NewRecorder()
	handler.RevokeAllSessions(w, req)
	assert.Equal(t, http.StatusNoContent, w.Code)
}

func revokeOtherCtx() context.Context {
	ctx := context.WithValue(context.Background(), "user_id", "u1")
	ctx = context.WithValue(ctx, "tenant_id", "t1")
	return context.WithValue(ctx, "session_id", "current")
}

func TestRevokeSession_OwnAndForeign(t *testing.T) {
	mgr := &mockSessionManager{sessions: []*iam.SessionExt{
		{ID: "own", UserID: "u1", TenantID: "t1"},
		{ID: "foreign", UserID: "victim", TenantID: "t1"},
	}}
	svc := &mockServiceWithSessions{QuicBaaS: testutil.NewMockQuicBaaS(), mgr: mgr}
	handler, cleanup := setupHandlerWithService(t, svc)
	defer cleanup()

	// Own session -> 204
	req := withChiParams(httptest.NewRequest(http.MethodDelete, "/x", nil).WithContext(revokeAllCtx()),
		map[string]string{"id": "own"})
	w := httptest.NewRecorder()
	handler.RevokeSession(w, req)
	assert.Equal(t, http.StatusNoContent, w.Code)

	// Foreign session -> 403
	req = withChiParams(httptest.NewRequest(http.MethodDelete, "/x", nil).WithContext(revokeAllCtx()),
		map[string]string{"id": "foreign"})
	w = httptest.NewRecorder()
	handler.RevokeSession(w, req)
	assert.Equal(t, http.StatusForbidden, w.Code)
}

func TestRevokeOtherSessions_Success(t *testing.T) {
	mgr := &mockSessionManager{sessions: []*iam.SessionExt{
		{ID: "current", UserID: "u1", TenantID: "t1"},
	}}
	svc := &mockServiceWithSessions{QuicBaaS: testutil.NewMockQuicBaaS(), mgr: mgr}
	handler, cleanup := setupHandlerWithService(t, svc)
	defer cleanup()

	req := httptest.NewRequest(http.MethodPost, "/api/v1/iam/sessions/revoke-others", nil).WithContext(revokeOtherCtx())
	w := httptest.NewRecorder()
	handler.RevokeOtherSessions(w, req)
	assert.Equal(t, http.StatusOK, w.Code, "body: %s", w.Body.String())
}
