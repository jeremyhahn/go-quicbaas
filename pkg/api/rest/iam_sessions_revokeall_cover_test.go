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
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/jeremyhahn/go-quicbaas/pkg/auth"
	"github.com/jeremyhahn/go-quicbaas/pkg/iam"
	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
	"github.com/jeremyhahn/go-quicbaas/pkg/testutil"
)

func revokeAllCtx() context.Context {
	return ctxWithTestSubject(context.Background(), "30001", "t1")
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

// numericUsernameCtx is a caller who logged in as "00042" and whose
// credential verified account 7. User 42 is somebody else.
func numericUsernameCtx() context.Context {
	return auth.SubjectIntoContext(context.Background(), interfaces.Subject{
		ID: "00042", IDKind: interfaces.SubjectIDUsername, UserID: "7",
		TenantID: "t1", Authenticated: true,
	})
}

// A numeric login name must not reach the sessions of the user whose id it
// spells: the self-service list and revoke act on the verified account.
func TestSelfServiceSessions_ANumericUsernameActsOnItsOwnAccount(t *testing.T) {
	mgr := &mockSessionManager{}
	svc := &mockServiceWithSessions{QuicBaaS: testutil.NewMockQuicBaaS(), mgr: mgr}
	handler, cleanup := setupHandlerWithService(t, svc)
	defer cleanup()

	w := httptest.NewRecorder()
	handler.ListSessions(w, httptest.NewRequest(http.MethodGet, "/api/v1/iam/sessions", nil).
		WithContext(numericUsernameCtx()))
	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, uint64(7), mgr.listedUserID, "list must be scoped to the verified account")

	w = httptest.NewRecorder()
	handler.RevokeAllSessions(w, httptest.NewRequest(http.MethodDelete, "/api/v1/iam/sessions", nil).
		WithContext(numericUsernameCtx()))
	assert.Equal(t, http.StatusNoContent, w.Code)
	assert.Equal(t, uint64(7), mgr.revokedUserID, "revoke must be scoped to the verified account")
}

func TestSelfServiceSessions_ACallerWithNoVerifiedAccountIsRefused(t *testing.T) {
	mgr := &mockSessionManager{}
	svc := &mockServiceWithSessions{QuicBaaS: testutil.NewMockQuicBaaS(), mgr: mgr}
	handler, cleanup := setupHandlerWithService(t, svc)
	defer cleanup()
	unverified := auth.SubjectIntoContext(context.Background(), interfaces.Subject{
		ID: "00042", TenantID: "t1", Authenticated: true,
	})

	w := httptest.NewRecorder()
	handler.ListSessions(w, httptest.NewRequest(http.MethodGet, "/api/v1/iam/sessions", nil).WithContext(unverified))
	assert.Equal(t, http.StatusForbidden, w.Code)
	assert.Zero(t, mgr.listedUserID)

	w = httptest.NewRecorder()
	handler.RevokeAllSessions(w, httptest.NewRequest(http.MethodDelete, "/api/v1/iam/sessions", nil).WithContext(unverified))
	assert.Equal(t, http.StatusForbidden, w.Code)
	assert.Zero(t, mgr.revokedUserID)
}

// RevokeAllSessions with an except session belonging to the user succeeds.
func TestRevokeAllSessions_ExceptOwn(t *testing.T) {
	mgr := &mockSessionManager{sessions: []*iam.SessionExt{
		{ID: "keep", UserID: 30001, TenantID: "t1"},
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
		{ID: "other", UserID: 40001, TenantID: "t1"},
	}}
	svc := &mockServiceWithSessions{QuicBaaS: testutil.NewMockQuicBaaS(), mgr: mgr}
	handler, cleanup := setupHandlerWithService(t, svc)
	defer cleanup()

	req := httptest.NewRequest(http.MethodDelete, "/api/v1/iam/sessions?except=other", nil).WithContext(revokeAllCtx())
	w := httptest.NewRecorder()
	handler.RevokeAllSessions(w, req)
	assert.Equal(t, http.StatusForbidden, w.Code)
}

// Query parameters are never an identity source: without an authenticated
// Subject the request is rejected even when user_id / tenant_id are supplied.
func TestRevokeAllSessions_QueryParamsNotIdentity(t *testing.T) {
	mgr := &mockSessionManager{}
	svc := &mockServiceWithSessions{QuicBaaS: testutil.NewMockQuicBaaS(), mgr: mgr}
	handler, cleanup := setupHandlerWithService(t, svc)
	defer cleanup()

	req := httptest.NewRequest(http.MethodDelete, "/api/v1/iam/sessions?user_id=30001&tenant_id=t1", nil)
	w := httptest.NewRecorder()
	handler.RevokeAllSessions(w, req)
	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

// Explicit query params that match the authenticated Subject (the Go SDK REST
// transport emits them) still succeed.
func TestRevokeAllSessions_QueryParamsMatchingSubject(t *testing.T) {
	mgr := &mockSessionManager{}
	svc := &mockServiceWithSessions{QuicBaaS: testutil.NewMockQuicBaaS(), mgr: mgr}
	handler, cleanup := setupHandlerWithService(t, svc)
	defer cleanup()

	req := httptest.NewRequest(http.MethodDelete, "/api/v1/iam/sessions?user_id=30001&tenant_id=t1", nil).
		WithContext(revokeAllCtx())
	w := httptest.NewRecorder()
	handler.RevokeAllSessions(w, req)
	assert.Equal(t, http.StatusNoContent, w.Code)
}

// A user_id query param naming another user is a cross-user attempt -> 403.
func TestRevokeAllSessions_QueryParamCrossUserDenied(t *testing.T) {
	mgr := &mockSessionManager{}
	svc := &mockServiceWithSessions{QuicBaaS: testutil.NewMockQuicBaaS(), mgr: mgr}
	handler, cleanup := setupHandlerWithService(t, svc)
	defer cleanup()

	req := httptest.NewRequest(http.MethodDelete, "/api/v1/iam/sessions?user_id=40001", nil).
		WithContext(revokeAllCtx())
	w := httptest.NewRecorder()
	handler.RevokeAllSessions(w, req)
	assert.Equal(t, http.StatusForbidden, w.Code)
}

func revokeOtherCtx() context.Context {
	return ctxWithTestSubject(context.Background(), "30001", "t1")
}

func TestRevokeSession_OwnAndForeign(t *testing.T) {
	mgr := &mockSessionManager{sessions: []*iam.SessionExt{
		{ID: "own", UserID: 30001, TenantID: "t1"},
		{ID: "foreign", UserID: 40001, TenantID: "t1"},
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
		{ID: "current", UserID: 30001, TenantID: "t1"},
	}}
	svc := &mockServiceWithSessions{QuicBaaS: testutil.NewMockQuicBaaS(), mgr: mgr}
	handler, cleanup := setupHandlerWithService(t, svc)
	defer cleanup()

	req := httptest.NewRequest(http.MethodPost, "/api/v1/iam/sessions/revoke-others",
		strings.NewReader(`{"keep_session_id":"current"}`)).WithContext(revokeOtherCtx())
	w := httptest.NewRecorder()
	handler.RevokeOtherSessions(w, req)
	assert.Equal(t, http.StatusOK, w.Code, "body: %s", w.Body.String())
}
