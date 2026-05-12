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
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/iam"
	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
	"github.com/jeremyhahn/go-quicbaas/pkg/testutil"
)

// TestBeginRegistration tests the BeginRegistration handler.
func TestBeginRegistration(t *testing.T) {
	t.Run("missing tenant ID", func(t *testing.T) {
		handler, _, cleanup := setupIAMHandler(t)
		defer cleanup()

		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("tenant_id", "")

		req := httptest.NewRequest(http.MethodPost, "/api/v1/tenants//webauthn/register/begin", strings.NewReader(`{}`))
		req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()

		handler.BeginRegistration(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
		assert.Contains(t, w.Body.String(), "tenant ID is required")
	})

	t.Run("invalid JSON body", func(t *testing.T) {
		handler, _, cleanup := setupIAMHandler(t)
		defer cleanup()

		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("tenant_id", "tenant-123")

		req := httptest.NewRequest(http.MethodPost, "/api/v1/tenants/tenant-123/webauthn/register/begin", strings.NewReader("{invalid"))
		req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()

		handler.BeginRegistration(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
		assert.Contains(t, w.Body.String(), "invalid JSON body")
	})

	t.Run("missing user_id", func(t *testing.T) {
		handler, _, cleanup := setupIAMHandler(t)
		defer cleanup()

		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("tenant_id", "tenant-123")

		body := `{"user_id":""}`
		req := httptest.NewRequest(http.MethodPost, "/api/v1/tenants/tenant-123/webauthn/register/begin", strings.NewReader(body))
		req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()

		handler.BeginRegistration(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
		assert.Contains(t, w.Body.String(), "user_id is required")
	})

	t.Run("webauthn manager not available", func(t *testing.T) {
		handler, _, cleanup := setupIAMHandler(t)
		defer cleanup()

		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("tenant_id", "tenant-123")

		body := `{"user_id":"user-123"}`
		req := httptest.NewRequest(http.MethodPost, "/api/v1/tenants/tenant-123/webauthn/register/begin", strings.NewReader(body))
		req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()

		handler.BeginRegistration(w, req)

		assert.Equal(t, http.StatusInternalServerError, w.Code)
		assert.Contains(t, w.Body.String(), "WebAuthn manager not available")
	})
}

// TestFinishRegistration tests the FinishRegistration handler.
func TestFinishRegistration(t *testing.T) {
	t.Run("missing tenant ID", func(t *testing.T) {
		handler, _, cleanup := setupIAMHandler(t)
		defer cleanup()

		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("tenant_id", "")

		req := httptest.NewRequest(http.MethodPost, "/api/v1/tenants//webauthn/register/finish", strings.NewReader(`{}`))
		req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()

		handler.FinishRegistration(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
		assert.Contains(t, w.Body.String(), "tenant ID is required")
	})

	t.Run("invalid JSON body", func(t *testing.T) {
		handler, _, cleanup := setupIAMHandler(t)
		defer cleanup()

		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("tenant_id", "tenant-123")

		req := httptest.NewRequest(http.MethodPost, "/api/v1/tenants/tenant-123/webauthn/register/finish", strings.NewReader("{invalid"))
		req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()

		handler.FinishRegistration(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
		assert.Contains(t, w.Body.String(), "invalid JSON body")
	})

	t.Run("missing user_id", func(t *testing.T) {
		handler, _, cleanup := setupIAMHandler(t)
		defer cleanup()

		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("tenant_id", "tenant-123")

		body := `{"user_id":"","credential":{}}`
		req := httptest.NewRequest(http.MethodPost, "/api/v1/tenants/tenant-123/webauthn/register/finish", strings.NewReader(body))
		req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()

		handler.FinishRegistration(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
		assert.Contains(t, w.Body.String(), "user_id is required")
	})

	t.Run("missing credential", func(t *testing.T) {
		handler, _, cleanup := setupIAMHandler(t)
		defer cleanup()

		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("tenant_id", "tenant-123")

		body := `{"user_id":"user-123"}`
		req := httptest.NewRequest(http.MethodPost, "/api/v1/tenants/tenant-123/webauthn/register/finish", strings.NewReader(body))
		req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()

		handler.FinishRegistration(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
		assert.Contains(t, w.Body.String(), "credential is required")
	})

	t.Run("webauthn manager not available", func(t *testing.T) {
		handler, _, cleanup := setupIAMHandler(t)
		defer cleanup()

		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("tenant_id", "tenant-123")

		body := `{"user_id":"user-123","credential":{"id":"cred-123"}}`
		req := httptest.NewRequest(http.MethodPost, "/api/v1/tenants/tenant-123/webauthn/register/finish", strings.NewReader(body))
		req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()

		handler.FinishRegistration(w, req)

		assert.Equal(t, http.StatusInternalServerError, w.Code)
		assert.Contains(t, w.Body.String(), "WebAuthn manager not available")
	})
}

// TestBeginLogin tests the BeginLogin handler.
func TestBeginLogin(t *testing.T) {
	t.Run("missing tenant ID", func(t *testing.T) {
		handler, _, cleanup := setupIAMHandler(t)
		defer cleanup()

		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("tenant_id", "")

		req := httptest.NewRequest(http.MethodPost, "/api/v1/tenants//webauthn/login/begin", strings.NewReader(`{}`))
		req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()

		handler.BeginLogin(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
		assert.Contains(t, w.Body.String(), "tenant ID is required")
	})

	t.Run("invalid JSON body", func(t *testing.T) {
		handler, _, cleanup := setupIAMHandler(t)
		defer cleanup()

		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("tenant_id", "tenant-123")

		req := httptest.NewRequest(http.MethodPost, "/api/v1/tenants/tenant-123/webauthn/login/begin", strings.NewReader("{invalid"))
		req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()

		handler.BeginLogin(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
		assert.Contains(t, w.Body.String(), "invalid JSON body")
	})

	t.Run("missing email", func(t *testing.T) {
		handler, _, cleanup := setupIAMHandler(t)
		defer cleanup()

		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("tenant_id", "tenant-123")

		body := `{"email":""}`
		req := httptest.NewRequest(http.MethodPost, "/api/v1/tenants/tenant-123/webauthn/login/begin", strings.NewReader(body))
		req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()

		handler.BeginLogin(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
		assert.Contains(t, w.Body.String(), "email is required")
	})

	t.Run("webauthn manager not available", func(t *testing.T) {
		handler, _, cleanup := setupIAMHandler(t)
		defer cleanup()

		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("tenant_id", "tenant-123")

		body := `{"email":"user@example.com"}`
		req := httptest.NewRequest(http.MethodPost, "/api/v1/tenants/tenant-123/webauthn/login/begin", strings.NewReader(body))
		req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()

		handler.BeginLogin(w, req)

		assert.Equal(t, http.StatusInternalServerError, w.Code)
		assert.Contains(t, w.Body.String(), "WebAuthn manager not available")
	})
}

// TestFinishLogin tests the FinishLogin handler.
func TestFinishLogin(t *testing.T) {
	t.Run("missing tenant ID", func(t *testing.T) {
		handler, _, cleanup := setupIAMHandler(t)
		defer cleanup()

		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("tenant_id", "")

		req := httptest.NewRequest(http.MethodPost, "/api/v1/tenants//webauthn/login/finish", strings.NewReader(`{}`))
		req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()

		handler.FinishLogin(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
		assert.Contains(t, w.Body.String(), "tenant ID is required")
	})

	t.Run("invalid JSON body", func(t *testing.T) {
		handler, _, cleanup := setupIAMHandler(t)
		defer cleanup()

		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("tenant_id", "tenant-123")

		req := httptest.NewRequest(http.MethodPost, "/api/v1/tenants/tenant-123/webauthn/login/finish", strings.NewReader("{invalid"))
		req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()

		handler.FinishLogin(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
		assert.Contains(t, w.Body.String(), "invalid JSON body")
	})

	t.Run("missing credential", func(t *testing.T) {
		handler, _, cleanup := setupIAMHandler(t)
		defer cleanup()

		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("tenant_id", "tenant-123")

		body := `{}`
		req := httptest.NewRequest(http.MethodPost, "/api/v1/tenants/tenant-123/webauthn/login/finish", strings.NewReader(body))
		req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()

		handler.FinishLogin(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
		assert.Contains(t, w.Body.String(), "credential is required")
	})

	t.Run("webauthn manager not available", func(t *testing.T) {
		handler, _, cleanup := setupIAMHandler(t)
		defer cleanup()

		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("tenant_id", "tenant-123")

		body := `{"credential":{"id":"cred-123"}}`
		req := httptest.NewRequest(http.MethodPost, "/api/v1/tenants/tenant-123/webauthn/login/finish", strings.NewReader(body))
		req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()

		handler.FinishLogin(w, req)

		assert.Equal(t, http.StatusInternalServerError, w.Code)
		assert.Contains(t, w.Body.String(), "WebAuthn manager not available")
	})
}

// =============================================================================
// Audit logging unit tests for WebAuthn handlers
// =============================================================================

// webauthnAuditMockService embeds MockQuicBaaS and overrides GetAuditLogger
// to surface a captureAuditLogger for audit-event assertions.
type webauthnAuditMockService struct {
	*testutil.MockQuicBaaS
	auditLogger *captureAuditLogger
}

func (s *webauthnAuditMockService) GetAuditLogger(_ context.Context) (iam.AuditLogger, error) {
	return s.auditLogger, nil
}

// newWebAuthnAuditServer creates a Server with a captureAuditLogger wired.
func newWebAuthnAuditServer(t *testing.T, capture *captureAuditLogger) *Server {
	t.Helper()
	log, err := logger.New(logger.LogConfig{Level: "error", Format: "json", Output: "stdout"})
	require.NoError(t, err)
	mock := &webauthnAuditMockService{
		MockQuicBaaS: testutil.NewMockQuicBaaS(),
		auditLogger:  capture,
	}
	cfg := DefaultConfig()
	cfg.Address = ":0"
	srv, err := NewServer(mock, cfg, log)
	require.NoError(t, err)
	return srv
}

// TestWebAuthn_LogBeginLoginAudit verifies that logWebAuthnBeginLoginAudit
// emits event_type "auth.login.fido2.begin" with the expected payload fields.
func TestWebAuthn_LogBeginLoginAudit(t *testing.T) {
	capture := &captureAuditLogger{}
	srv := newWebAuthnAuditServer(t, capture)
	h := &IAMHandler{server: srv}

	req := httptest.NewRequest(http.MethodPost, "/api/v1/tenants/t1/webauthn/login/begin", nil)
	req.Header.Set("X-Request-ID", "begin-req-1")

	h.logWebAuthnBeginLoginAudit(context.Background(), "tenant-1", "user@example.com", "begin-req-1", req)

	require.Len(t, capture.events, 1)
	ev := capture.events[0]
	assert.Equal(t, "auth.login.fido2.begin", ev.Action)
	assert.Equal(t, iam.AuditOutcomeSuccess, ev.Outcome)
	assert.Equal(t, "user@example.com", ev.Details["user_email"])
	assert.Equal(t, "begin-req-1", ev.Details["request_id"])
}

// TestWebAuthn_LogSuccessAudit verifies that logWebAuthnLoginAudit
// emits event_type "auth.login.fido2.finish.success" with the expected payload.
func TestWebAuthn_LogSuccessAudit(t *testing.T) {
	capture := &captureAuditLogger{}
	srv := newWebAuthnAuditServer(t, capture)
	h := &IAMHandler{server: srv}

	req := httptest.NewRequest(http.MethodPost, "/api/v1/tenants/t1/webauthn/login/finish", nil)
	req.Header.Set("X-Request-ID", "finish-req-2")

	h.logWebAuthnLoginAudit(context.Background(), "tenant-1", "user-1", "user@example.com",
		"aaguid-hex", "finish-req-2", req)

	require.Len(t, capture.events, 1)
	ev := capture.events[0]
	assert.Equal(t, "auth.login.fido2.finish.success", ev.Action)
	assert.Equal(t, iam.AuditOutcomeSuccess, ev.Outcome)
	assert.Equal(t, "user-1", ev.Details["user_id"])
	assert.Equal(t, "user@example.com", ev.Details["user_email"])
	assert.Equal(t, "aaguid-hex", ev.Details["aaguid"])
	assert.Equal(t, "finish-req-2", ev.Details["request_id"])
}

// TestWebAuthn_LogFailureAudit verifies that logWebAuthnLoginFailureAudit
// emits event_type "auth.login.fido2.finish.failure" with the expected payload.
func TestWebAuthn_LogFailureAudit(t *testing.T) {
	capture := &captureAuditLogger{}
	srv := newWebAuthnAuditServer(t, capture)
	h := &IAMHandler{server: srv}

	req := httptest.NewRequest(http.MethodPost, "/api/v1/tenants/t1/webauthn/login/finish", nil)
	req.Header.Set("X-Request-ID", "fail-req-3")

	h.logWebAuthnLoginFailureAudit(context.Background(), "tenant-1", "user@example.com",
		ErrUnauthorized, "credential mismatch", "fail-req-3", req)

	require.Len(t, capture.events, 1)
	ev := capture.events[0]
	assert.Equal(t, "auth.login.fido2.finish.failure", ev.Action)
	assert.Equal(t, iam.AuditOutcomeFailure, ev.Outcome)
	assert.Equal(t, ErrUnauthorized, ev.Details["error_code"])
	assert.Equal(t, "fail-req-3", ev.Details["request_id"])
}
