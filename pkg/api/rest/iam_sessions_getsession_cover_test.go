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
	stderrors "errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/iam"
	"github.com/jeremyhahn/go-quicbaas/pkg/testutil"
)

// TestGetSession_SuccessWithDeviceInfo drives the GetSession happy path for a
// session owned by the authenticated user, including the device-info mapping
// branch, and asserts the sanitized response shape.
func TestGetSession_SuccessWithDeviceInfo(t *testing.T) {
	now := time.Now().UTC()
	mgr := &mockSessionManager{sessions: []*iam.SessionExt{
		{
			ID:         "sess-1",
			UserID:     30001,
			TenantID:   "t1",
			Created:    now,
			LastActive: now,
			ExpiresAt:  now.Add(time.Hour),
			IPAddress:  "10.0.0.1",
			UserAgent:  "test-agent",
			DeviceInfo: &iam.DeviceInfo{
				ID:          "dev-1",
				Name:        "Laptop",
				Type:        "browser",
				Fingerprint: "fp-123",
				Trusted:     true,
			},
		},
	}}
	svc := &mockServiceWithSessions{QuicBaaS: testutil.NewMockQuicBaaS(), mgr: mgr}
	handler, cleanup := setupHandlerWithService(t, svc)
	defer cleanup()

	ctx := ctxWithTestSubject(context.Background(), "30001", "t1")
	req := withChiParams(
		httptest.NewRequest(http.MethodGet, "/api/v1/iam/sessions/sess-1", nil).WithContext(ctx),
		map[string]string{"id": "sess-1"},
	)
	w := httptest.NewRecorder()

	handler.GetSession(w, req)

	require.Equal(t, http.StatusOK, w.Code, "body: %s", w.Body.String())

	var resp map[string]interface{}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, "sess-1", resp["id"])
	device, ok := resp["device_info"].(map[string]interface{})
	require.True(t, ok, "device_info should be present")
	assert.Equal(t, "Laptop", device["name"])
}

// TestGetSession_ManagerError covers the branch where the session manager
// returns an error from Get, which is surfaced via handleServiceError.
func TestGetSession_ManagerError(t *testing.T) {
	mgr := &mockSessionManager{getErr: stderrors.New("backend unavailable")}
	svc := &mockServiceWithSessions{QuicBaaS: testutil.NewMockQuicBaaS(), mgr: mgr}
	handler, cleanup := setupHandlerWithService(t, svc)
	defer cleanup()

	ctx := ctxWithTestSubject(context.Background(), "30001", "t1")
	req := withChiParams(
		httptest.NewRequest(http.MethodGet, "/api/v1/iam/sessions/sess-x", nil).WithContext(ctx),
		map[string]string{"id": "sess-x"},
	)
	w := httptest.NewRecorder()

	handler.GetSession(w, req)

	assert.NotEqual(t, http.StatusOK, w.Code)
}

// TestGetSession_CrossUserDenied verifies a non-admin caller cannot read
// another user's session: ownership is checked against the authenticated
// Subject, not request-supplied identity.
func TestGetSession_CrossUserDenied(t *testing.T) {
	mgr := &mockSessionManager{sessions: []*iam.SessionExt{
		{ID: "sess-1", UserID: 40001, TenantID: "t1"},
	}}
	svc := &mockServiceWithSessions{QuicBaaS: testutil.NewMockQuicBaaS(), mgr: mgr}
	handler, cleanup := setupHandlerWithService(t, svc)
	defer cleanup()

	ctx := ctxWithTestSubject(context.Background(), "40003", "t1")
	req := withChiParams(
		httptest.NewRequest(http.MethodGet, "/api/v1/iam/sessions/sess-1", nil).WithContext(ctx),
		map[string]string{"id": "sess-1"},
	)
	w := httptest.NewRecorder()

	handler.GetSession(w, req)
	assert.Equal(t, http.StatusForbidden, w.Code)
	assert.Contains(t, w.Body.String(), "access denied")
}

// TestGetSession_CrossTenantDenied verifies tenant isolation: same user ID in
// a different tenant is still denied.
func TestGetSession_CrossTenantDenied(t *testing.T) {
	mgr := &mockSessionManager{sessions: []*iam.SessionExt{
		{ID: "sess-1", UserID: 30001, TenantID: "other-tenant"},
	}}
	svc := &mockServiceWithSessions{QuicBaaS: testutil.NewMockQuicBaaS(), mgr: mgr}
	handler, cleanup := setupHandlerWithService(t, svc)
	defer cleanup()

	ctx := ctxWithTestSubject(context.Background(), "30001", "t1")
	req := withChiParams(
		httptest.NewRequest(http.MethodGet, "/api/v1/iam/sessions/sess-1", nil).WithContext(ctx),
		map[string]string{"id": "sess-1"},
	)
	w := httptest.NewRecorder()

	handler.GetSession(w, req)
	assert.Equal(t, http.StatusForbidden, w.Code)
}

// TestListSessions_QueryParamCrossUserDenied verifies the user_id query
// parameter cannot be used to list another user's sessions.
func TestListSessions_QueryParamCrossUserDenied(t *testing.T) {
	mgr := &mockSessionManager{sessions: []*iam.SessionExt{
		{ID: "sess-1", UserID: 40001, TenantID: "t1"},
	}}
	svc := &mockServiceWithSessions{QuicBaaS: testutil.NewMockQuicBaaS(), mgr: mgr}
	handler, cleanup := setupHandlerWithService(t, svc)
	defer cleanup()

	ctx := ctxWithTestSubject(context.Background(), "40003", "t1")
	req := httptest.NewRequest(http.MethodGet, "/api/v1/iam/sessions?user_id=40001", nil).WithContext(ctx)
	w := httptest.NewRecorder()

	handler.ListSessions(w, req)
	assert.Equal(t, http.StatusForbidden, w.Code)
	assert.Contains(t, w.Body.String(), "does not match authenticated user")
}

// TestListSessions_QueryParamsMatchingSubject verifies self-referential query
// params (as emitted by the Go SDK REST transport) still succeed.
func TestListSessions_QueryParamsMatchingSubject(t *testing.T) {
	now := time.Now().UTC()
	mgr := &mockSessionManager{sessions: []*iam.SessionExt{
		{ID: "sess-1", UserID: 30001, TenantID: "t1", Created: now, LastActive: now, ExpiresAt: now.Add(time.Hour)},
	}}
	svc := &mockServiceWithSessions{QuicBaaS: testutil.NewMockQuicBaaS(), mgr: mgr}
	handler, cleanup := setupHandlerWithService(t, svc)
	defer cleanup()

	ctx := ctxWithTestSubject(context.Background(), "30001", "t1")
	req := httptest.NewRequest(http.MethodGet, "/api/v1/iam/sessions?user_id=30001&tenant_id=t1", nil).WithContext(ctx)
	w := httptest.NewRecorder()

	handler.ListSessions(w, req)
	require.Equal(t, http.StatusOK, w.Code, "body: %s", w.Body.String())
	assert.Contains(t, w.Body.String(), "sess-1")
}

// TestRevokeOtherSessions_ForeignKeepDenied verifies the keep_session_id must
// belong to the authenticated caller.
func TestRevokeOtherSessions_ForeignKeepDenied(t *testing.T) {
	mgr := &mockSessionManager{sessions: []*iam.SessionExt{
		{ID: "foreign", UserID: 40001, TenantID: "t1"},
	}}
	svc := &mockServiceWithSessions{QuicBaaS: testutil.NewMockQuicBaaS(), mgr: mgr}
	handler, cleanup := setupHandlerWithService(t, svc)
	defer cleanup()

	ctx := ctxWithTestSubject(context.Background(), "30001", "t1")
	req := httptest.NewRequest(http.MethodPost, "/api/v1/iam/sessions/revoke-others",
		strings.NewReader(`{"keep_session_id":"foreign"}`)).WithContext(ctx)
	w := httptest.NewRecorder()

	handler.RevokeOtherSessions(w, req)
	assert.Equal(t, http.StatusForbidden, w.Code)
	assert.Contains(t, w.Body.String(), "access denied")
}
