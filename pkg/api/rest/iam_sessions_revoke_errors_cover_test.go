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
	stderrors "errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/jeremyhahn/go-quicbaas/pkg/iam"
	"github.com/jeremyhahn/go-quicbaas/pkg/testutil"
)

// TestRevokeSession_GetError covers the branch where mgr.Get returns an error
// before the ownership check in RevokeSession.
func TestRevokeSession_GetError(t *testing.T) {
	mgr := &mockSessionManager{getErr: stderrors.New("lookup failed")}
	svc := &mockServiceWithSessions{QuicBaaS: testutil.NewMockQuicBaaS(), mgr: mgr}
	handler, cleanup := setupHandlerWithService(t, svc)
	defer cleanup()

	ctx := context.WithValue(context.Background(), "user_id", "u1")
	ctx = context.WithValue(ctx, "tenant_id", "t1")
	req := withChiParams(
		httptest.NewRequest(http.MethodDelete, "/x", nil).WithContext(ctx),
		map[string]string{"id": "sess-1"},
	)
	w := httptest.NewRecorder()

	handler.RevokeSession(w, req)
	assert.NotEqual(t, http.StatusNoContent, w.Code)
}

// TestRevokeSession_RevokeError covers the branch where mgr.Revoke fails after a
// successful ownership check.
func TestRevokeSession_RevokeError(t *testing.T) {
	mgr := &mockSessionManager{
		sessions:  []*iam.SessionExt{{ID: "sess-1", UserID: "u1", TenantID: "t1"}},
		revokeErr: stderrors.New("revoke failed"),
	}
	svc := &mockServiceWithSessions{QuicBaaS: testutil.NewMockQuicBaaS(), mgr: mgr}
	handler, cleanup := setupHandlerWithService(t, svc)
	defer cleanup()

	ctx := context.WithValue(context.Background(), "user_id", "u1")
	ctx = context.WithValue(ctx, "tenant_id", "t1")
	req := withChiParams(
		httptest.NewRequest(http.MethodDelete, "/x", nil).WithContext(ctx),
		map[string]string{"id": "sess-1"},
	)
	w := httptest.NewRecorder()

	handler.RevokeSession(w, req)
	assert.NotEqual(t, http.StatusNoContent, w.Code)
}

// TestRevokeAllSessions_RevokeAllForUserError covers the no-except branch where
// RevokeAllForUser returns an error.
func TestRevokeAllSessions_RevokeAllForUserError(t *testing.T) {
	mgr := &mockSessionManager{revokeErr: stderrors.New("bulk revoke failed")}
	svc := &mockServiceWithSessions{QuicBaaS: testutil.NewMockQuicBaaS(), mgr: mgr}
	handler, cleanup := setupHandlerWithService(t, svc)
	defer cleanup()

	ctx := context.WithValue(context.Background(), "user_id", "u1")
	ctx = context.WithValue(ctx, "tenant_id", "t1")
	req := httptest.NewRequest(http.MethodDelete, "/api/v1/iam/sessions", nil).WithContext(ctx)
	w := httptest.NewRecorder()

	handler.RevokeAllSessions(w, req)
	assert.NotEqual(t, http.StatusNoContent, w.Code)
}

// TestRevokeAllSessions_RevokeAllExceptError covers the except branch where
// RevokeAllExcept returns an error after the ownership check passes.
func TestRevokeAllSessions_RevokeAllExceptError(t *testing.T) {
	mgr := &mockSessionManager{
		sessions:  []*iam.SessionExt{{ID: "keep", UserID: "u1", TenantID: "t1"}},
		revokeErr: stderrors.New("revoke except failed"),
	}
	svc := &mockServiceWithSessions{QuicBaaS: testutil.NewMockQuicBaaS(), mgr: mgr}
	handler, cleanup := setupHandlerWithService(t, svc)
	defer cleanup()

	ctx := context.WithValue(context.Background(), "user_id", "u1")
	ctx = context.WithValue(ctx, "tenant_id", "t1")
	req := httptest.NewRequest(http.MethodDelete, "/api/v1/iam/sessions?except=keep", nil).WithContext(ctx)
	w := httptest.NewRecorder()

	handler.RevokeAllSessions(w, req)
	assert.NotEqual(t, http.StatusNoContent, w.Code)
}

// TestRevokeOtherSessions_GetError covers the branch where the current-session
// lookup fails in RevokeOtherSessions.
func TestRevokeOtherSessions_GetError(t *testing.T) {
	mgr := &mockSessionManager{getErr: stderrors.New("lookup failed")}
	svc := &mockServiceWithSessions{QuicBaaS: testutil.NewMockQuicBaaS(), mgr: mgr}
	handler, cleanup := setupHandlerWithService(t, svc)
	defer cleanup()

	ctx := context.WithValue(context.Background(), "user_id", "u1")
	ctx = context.WithValue(ctx, "tenant_id", "t1")
	ctx = context.WithValue(ctx, "session_id", "current")
	req := httptest.NewRequest(http.MethodPost, "/api/v1/iam/sessions/revoke-others", nil).WithContext(ctx)
	w := httptest.NewRecorder()

	handler.RevokeOtherSessions(w, req)
	assert.NotEqual(t, http.StatusOK, w.Code)
}
