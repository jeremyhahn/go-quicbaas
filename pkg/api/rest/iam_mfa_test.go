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
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/assert"
)

// TestErrMFANotAvailable tests the ErrMFANotAvailable error value.
func TestErrMFANotAvailable(t *testing.T) {
	assert.Equal(t, "MFA service not available", ErrMFANotAvailable.Error())
}

// TestBeginMFAEnrollment tests the BeginMFAEnrollment handler.
func TestBeginMFAEnrollment(t *testing.T) {
	t.Run("missing tenant_id", func(t *testing.T) {
		handler, _, cleanup := setupIAMHandler(t)
		defer cleanup()

		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("tenant_id", "")
		rctx.URLParams.Add("user_id", "10123")

		req := httptest.NewRequest(http.MethodPost, "/api/v1/tenants//users/user-123/mfa/enroll", nil)
		req = req.WithContext(callerRouteContext(req.Context(), rctx))
		w := httptest.NewRecorder()

		handler.BeginMFAEnrollment(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
		assert.Contains(t, w.Body.String(), "tenant ID is required")
	})

	t.Run("missing user_id", func(t *testing.T) {
		handler, _, cleanup := setupIAMHandler(t)
		defer cleanup()

		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("tenant_id", "tenant-123")
		rctx.URLParams.Add("user_id", "")

		req := httptest.NewRequest(http.MethodPost, "/api/v1/tenants/tenant-123/users//mfa/enroll", nil)
		req = req.WithContext(callerRouteContext(req.Context(), rctx))
		w := httptest.NewRecorder()

		handler.BeginMFAEnrollment(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
		assert.Contains(t, w.Body.String(), "user ID is required")
	})

	t.Run("invalid JSON body", func(t *testing.T) {
		handler, _, cleanup := setupIAMHandler(t)
		defer cleanup()

		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("tenant_id", "tenant-123")
		rctx.URLParams.Add("user_id", "10123")

		req := httptest.NewRequest(http.MethodPost, "/api/v1/tenants/tenant-123/users/user-123/mfa/enroll", strings.NewReader("invalid json"))
		req.Header.Set("Content-Type", "application/json")
		req.ContentLength = 12
		req = req.WithContext(callerRouteContext(req.Context(), rctx))
		w := httptest.NewRecorder()

		handler.BeginMFAEnrollment(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
		assert.Contains(t, w.Body.String(), "invalid JSON")
	})

	t.Run("MFA service not available", func(t *testing.T) {
		handler, _, cleanup := setupIAMHandler(t)
		defer cleanup()

		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("tenant_id", "tenant-123")
		rctx.URLParams.Add("user_id", "10123")

		body := `{"method":"totp"}`
		req := httptest.NewRequest(http.MethodPost, "/api/v1/tenants/tenant-123/users/user-123/mfa/enroll", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.ContentLength = int64(len(body))
		req = req.WithContext(callerRouteContext(req.Context(), rctx))
		w := httptest.NewRecorder()

		handler.BeginMFAEnrollment(w, req)

		assert.Equal(t, http.StatusInternalServerError, w.Code)
		assert.Contains(t, w.Body.String(), "MFA service not available")
	})

	t.Run("empty body - defaults to TOTP", func(t *testing.T) {
		handler, _, cleanup := setupIAMHandler(t)
		defer cleanup()

		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("tenant_id", "tenant-123")
		rctx.URLParams.Add("user_id", "10123")

		req := httptest.NewRequest(http.MethodPost, "/api/v1/tenants/tenant-123/users/user-123/mfa/enroll", nil)
		req = req.WithContext(callerRouteContext(req.Context(), rctx))
		w := httptest.NewRecorder()

		handler.BeginMFAEnrollment(w, req)

		assert.Equal(t, http.StatusInternalServerError, w.Code)
		assert.Contains(t, w.Body.String(), "MFA service not available")
	})
}

// TestCompleteMFAEnrollment tests the CompleteMFAEnrollment handler.
func TestCompleteMFAEnrollment(t *testing.T) {
	t.Run("missing tenant_id and user_id", func(t *testing.T) {
		handler, _, cleanup := setupIAMHandler(t)
		defer cleanup()

		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("tenant_id", "")
		rctx.URLParams.Add("user_id", "")

		req := httptest.NewRequest(http.MethodPost, "/api/v1/tenants//users//mfa/verify", strings.NewReader("{}"))
		req.Header.Set("Content-Type", "application/json")
		req = req.WithContext(callerRouteContext(req.Context(), rctx))
		w := httptest.NewRecorder()

		handler.CompleteMFAEnrollment(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
		assert.Contains(t, w.Body.String(), "tenant ID and user ID are required")
	})

	t.Run("invalid JSON body", func(t *testing.T) {
		handler, _, cleanup := setupIAMHandler(t)
		defer cleanup()

		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("tenant_id", "tenant-123")
		rctx.URLParams.Add("user_id", "10123")

		req := httptest.NewRequest(http.MethodPost, "/api/v1/tenants/tenant-123/users/user-123/mfa/verify", strings.NewReader("invalid json"))
		req.Header.Set("Content-Type", "application/json")
		req = req.WithContext(callerRouteContext(req.Context(), rctx))
		w := httptest.NewRecorder()

		handler.CompleteMFAEnrollment(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
		assert.Contains(t, w.Body.String(), "invalid JSON")
	})

	t.Run("missing enrollment_id and code", func(t *testing.T) {
		handler, _, cleanup := setupIAMHandler(t)
		defer cleanup()

		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("tenant_id", "tenant-123")
		rctx.URLParams.Add("user_id", "10123")

		body := `{"enrollment_id":"","code":""}`
		req := httptest.NewRequest(http.MethodPost, "/api/v1/tenants/tenant-123/users/user-123/mfa/verify", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req = req.WithContext(callerRouteContext(req.Context(), rctx))
		w := httptest.NewRecorder()

		handler.CompleteMFAEnrollment(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
		assert.Contains(t, w.Body.String(), "enrollment_id and code are required")
	})

	t.Run("MFA service not available", func(t *testing.T) {
		handler, _, cleanup := setupIAMHandler(t)
		defer cleanup()

		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("tenant_id", "tenant-123")
		rctx.URLParams.Add("user_id", "10123")

		body := `{"enrollment_id":"enroll-123","code":"123456"}`
		req := httptest.NewRequest(http.MethodPost, "/api/v1/tenants/tenant-123/users/user-123/mfa/verify", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req = req.WithContext(callerRouteContext(req.Context(), rctx))
		w := httptest.NewRecorder()

		handler.CompleteMFAEnrollment(w, req)

		assert.Equal(t, http.StatusInternalServerError, w.Code)
		assert.Contains(t, w.Body.String(), "MFA service not available")
	})
}

// TestCancelMFAEnrollment tests the CancelMFAEnrollment handler.
func TestCancelMFAEnrollment(t *testing.T) {
	t.Run("missing required IDs", func(t *testing.T) {
		handler, _, cleanup := setupIAMHandler(t)
		defer cleanup()

		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("tenant_id", "")
		rctx.URLParams.Add("user_id", "")
		rctx.URLParams.Add("enrollment_id", "")

		req := httptest.NewRequest(http.MethodDelete, "/api/v1/tenants//users//mfa/enroll/", nil)
		req = req.WithContext(callerRouteContext(req.Context(), rctx))
		w := httptest.NewRecorder()

		handler.CancelMFAEnrollment(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
		assert.Contains(t, w.Body.String(), "tenant ID, user ID, and enrollment ID are required")
	})

	t.Run("MFA service not available", func(t *testing.T) {
		handler, _, cleanup := setupIAMHandler(t)
		defer cleanup()

		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("tenant_id", "tenant-123")
		rctx.URLParams.Add("user_id", "10123")
		rctx.URLParams.Add("enrollment_id", "enroll-123")

		req := httptest.NewRequest(http.MethodDelete, "/api/v1/tenants/tenant-123/users/user-123/mfa/enroll/enroll-123", nil)
		req = req.WithContext(callerRouteContext(req.Context(), rctx))
		w := httptest.NewRecorder()

		handler.CancelMFAEnrollment(w, req)

		assert.Equal(t, http.StatusInternalServerError, w.Code)
		assert.Contains(t, w.Body.String(), "MFA service not available")
	})
}

// TestGetMFAStatus tests the GetMFAStatus handler.
func TestGetMFAStatus(t *testing.T) {
	t.Run("missing tenant_id and user_id", func(t *testing.T) {
		handler, _, cleanup := setupIAMHandler(t)
		defer cleanup()

		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("tenant_id", "")
		rctx.URLParams.Add("user_id", "")

		req := httptest.NewRequest(http.MethodGet, "/api/v1/tenants//users//mfa", nil)
		req = req.WithContext(callerRouteContext(req.Context(), rctx))
		w := httptest.NewRecorder()

		handler.GetMFAStatus(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
		assert.Contains(t, w.Body.String(), "tenant ID and user ID are required")
	})

	t.Run("MFA service not available", func(t *testing.T) {
		handler, _, cleanup := setupIAMHandler(t)
		defer cleanup()

		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("tenant_id", "tenant-123")
		rctx.URLParams.Add("user_id", "10123")

		req := httptest.NewRequest(http.MethodGet, "/api/v1/tenants/tenant-123/users/user-123/mfa", nil)
		req = req.WithContext(callerRouteContext(req.Context(), rctx))
		w := httptest.NewRecorder()

		handler.GetMFAStatus(w, req)

		assert.Equal(t, http.StatusInternalServerError, w.Code)
		assert.Contains(t, w.Body.String(), "MFA service not available")
	})
}

// TestDisableMFA tests the DisableMFA handler.
func TestDisableMFA(t *testing.T) {
	t.Run("missing tenant_id and user_id", func(t *testing.T) {
		handler, _, cleanup := setupIAMHandler(t)
		defer cleanup()

		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("tenant_id", "")
		rctx.URLParams.Add("user_id", "")

		req := httptest.NewRequest(http.MethodDelete, "/api/v1/tenants//users//mfa", nil)
		req = req.WithContext(callerRouteContext(req.Context(), rctx))
		w := httptest.NewRecorder()

		handler.DisableMFA(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
		assert.Contains(t, w.Body.String(), "tenant ID and user ID are required")
	})

	t.Run("MFA service not available", func(t *testing.T) {
		handler, _, cleanup := setupIAMHandler(t)
		defer cleanup()

		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("tenant_id", "tenant-123")
		rctx.URLParams.Add("user_id", "10123")

		req := httptest.NewRequest(http.MethodDelete, "/api/v1/tenants/tenant-123/users/user-123/mfa", nil)
		req = req.WithContext(callerRouteContext(req.Context(), rctx))
		w := httptest.NewRecorder()

		handler.DisableMFA(w, req)

		assert.Equal(t, http.StatusInternalServerError, w.Code)
		assert.Contains(t, w.Body.String(), "MFA service not available")
	})
}

// TestBeginMFAChallenge tests the BeginMFAChallenge handler.
func TestBeginMFAChallenge(t *testing.T) {
	t.Run("missing tenant_id and user_id", func(t *testing.T) {
		handler, _, cleanup := setupIAMHandler(t)
		defer cleanup()

		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("tenant_id", "")
		rctx.URLParams.Add("user_id", "")

		req := httptest.NewRequest(http.MethodPost, "/api/v1/tenants//users//mfa/challenge", strings.NewReader("{}"))
		req.Header.Set("Content-Type", "application/json")
		req = req.WithContext(callerRouteContext(req.Context(), rctx))
		w := httptest.NewRecorder()

		handler.BeginMFAChallenge(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
		assert.Contains(t, w.Body.String(), "tenant ID and user ID are required")
	})

	t.Run("invalid JSON body", func(t *testing.T) {
		handler, _, cleanup := setupIAMHandler(t)
		defer cleanup()

		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("tenant_id", "tenant-123")
		rctx.URLParams.Add("user_id", "10123")

		req := httptest.NewRequest(http.MethodPost, "/api/v1/tenants/tenant-123/users/user-123/mfa/challenge", strings.NewReader("invalid json"))
		req.Header.Set("Content-Type", "application/json")
		req = req.WithContext(callerRouteContext(req.Context(), rctx))
		w := httptest.NewRecorder()

		handler.BeginMFAChallenge(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
		assert.Contains(t, w.Body.String(), "invalid JSON")
	})

	t.Run("missing session_token", func(t *testing.T) {
		handler, _, cleanup := setupIAMHandler(t)
		defer cleanup()

		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("tenant_id", "tenant-123")
		rctx.URLParams.Add("user_id", "10123")

		body := `{"session_token":""}`
		req := httptest.NewRequest(http.MethodPost, "/api/v1/tenants/tenant-123/users/user-123/mfa/challenge", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req = req.WithContext(callerRouteContext(req.Context(), rctx))
		w := httptest.NewRecorder()

		handler.BeginMFAChallenge(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
		assert.Contains(t, w.Body.String(), "session_token is required")
	})

	t.Run("MFA service not available", func(t *testing.T) {
		handler, _, cleanup := setupIAMHandler(t)
		defer cleanup()

		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("tenant_id", "tenant-123")
		rctx.URLParams.Add("user_id", "10123")

		body := `{"session_token":"session-123"}`
		req := httptest.NewRequest(http.MethodPost, "/api/v1/tenants/tenant-123/users/user-123/mfa/challenge", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req = req.WithContext(callerRouteContext(req.Context(), rctx))
		w := httptest.NewRecorder()

		handler.BeginMFAChallenge(w, req)

		assert.Equal(t, http.StatusInternalServerError, w.Code)
		assert.Contains(t, w.Body.String(), "MFA service not available")
	})
}

// TestVerifyMFAChallenge tests the VerifyMFAChallenge handler.
func TestVerifyMFAChallenge(t *testing.T) {
	t.Run("missing tenant_id and user_id", func(t *testing.T) {
		handler, _, cleanup := setupIAMHandler(t)
		defer cleanup()

		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("tenant_id", "")
		rctx.URLParams.Add("user_id", "")

		req := httptest.NewRequest(http.MethodPost, "/api/v1/tenants//users//mfa/verify-challenge", strings.NewReader("{}"))
		req.Header.Set("Content-Type", "application/json")
		req = req.WithContext(callerRouteContext(req.Context(), rctx))
		w := httptest.NewRecorder()

		handler.VerifyMFAChallenge(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
		assert.Contains(t, w.Body.String(), "tenant ID and user ID are required")
	})

	t.Run("invalid JSON body", func(t *testing.T) {
		handler, _, cleanup := setupIAMHandler(t)
		defer cleanup()

		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("tenant_id", "tenant-123")
		rctx.URLParams.Add("user_id", "10123")

		req := httptest.NewRequest(http.MethodPost, "/api/v1/tenants/tenant-123/users/user-123/mfa/verify-challenge", strings.NewReader("invalid json"))
		req.Header.Set("Content-Type", "application/json")
		req = req.WithContext(callerRouteContext(req.Context(), rctx))
		w := httptest.NewRecorder()

		handler.VerifyMFAChallenge(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
		assert.Contains(t, w.Body.String(), "invalid JSON")
	})

	t.Run("missing challenge_id and code", func(t *testing.T) {
		handler, _, cleanup := setupIAMHandler(t)
		defer cleanup()

		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("tenant_id", "tenant-123")
		rctx.URLParams.Add("user_id", "10123")

		body := `{"challenge_id":"","code":""}`
		req := httptest.NewRequest(http.MethodPost, "/api/v1/tenants/tenant-123/users/user-123/mfa/verify-challenge", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req = req.WithContext(callerRouteContext(req.Context(), rctx))
		w := httptest.NewRecorder()

		handler.VerifyMFAChallenge(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
		assert.Contains(t, w.Body.String(), "challenge_id and code are required")
	})

	t.Run("MFA service not available", func(t *testing.T) {
		handler, _, cleanup := setupIAMHandler(t)
		defer cleanup()

		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("tenant_id", "tenant-123")
		rctx.URLParams.Add("user_id", "10123")

		body := `{"challenge_id":"challenge-123","code":"123456"}`
		req := httptest.NewRequest(http.MethodPost, "/api/v1/tenants/tenant-123/users/user-123/mfa/verify-challenge", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req = req.WithContext(callerRouteContext(req.Context(), rctx))
		w := httptest.NewRecorder()

		handler.VerifyMFAChallenge(w, req)

		assert.Equal(t, http.StatusInternalServerError, w.Code)
		assert.Contains(t, w.Body.String(), "MFA service not available")
	})
}

// TestGenerateBackupCodes tests the GenerateBackupCodes handler.
func TestGenerateBackupCodes(t *testing.T) {
	t.Run("missing tenant_id and user_id", func(t *testing.T) {
		handler, _, cleanup := setupIAMHandler(t)
		defer cleanup()

		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("tenant_id", "")
		rctx.URLParams.Add("user_id", "")

		req := httptest.NewRequest(http.MethodPost, "/api/v1/tenants//users//mfa/backup-codes", nil)
		req = req.WithContext(callerRouteContext(req.Context(), rctx))
		w := httptest.NewRecorder()

		handler.GenerateBackupCodes(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
		assert.Contains(t, w.Body.String(), "tenant ID and user ID are required")
	})

	t.Run("MFA service not available", func(t *testing.T) {
		handler, _, cleanup := setupIAMHandler(t)
		defer cleanup()

		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("tenant_id", "tenant-123")
		rctx.URLParams.Add("user_id", "10123")

		req := httptest.NewRequest(http.MethodPost, "/api/v1/tenants/tenant-123/users/user-123/mfa/backup-codes", nil)
		req = req.WithContext(callerRouteContext(req.Context(), rctx))
		w := httptest.NewRecorder()

		handler.GenerateBackupCodes(w, req)

		assert.Equal(t, http.StatusInternalServerError, w.Code)
		assert.Contains(t, w.Body.String(), "MFA service not available")
	})
}

// TestGetBackupCodeStatus tests the GetBackupCodeStatus handler.
func TestGetBackupCodeStatus(t *testing.T) {
	t.Run("missing tenant_id and user_id", func(t *testing.T) {
		handler, _, cleanup := setupIAMHandler(t)
		defer cleanup()

		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("tenant_id", "")
		rctx.URLParams.Add("user_id", "")

		req := httptest.NewRequest(http.MethodGet, "/api/v1/tenants//users//mfa/backup-codes", nil)
		req = req.WithContext(callerRouteContext(req.Context(), rctx))
		w := httptest.NewRecorder()

		handler.GetBackupCodeStatus(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
		assert.Contains(t, w.Body.String(), "tenant ID and user ID are required")
	})

	t.Run("MFA service not available", func(t *testing.T) {
		handler, _, cleanup := setupIAMHandler(t)
		defer cleanup()

		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("tenant_id", "tenant-123")
		rctx.URLParams.Add("user_id", "10123")

		req := httptest.NewRequest(http.MethodGet, "/api/v1/tenants/tenant-123/users/user-123/mfa/backup-codes", nil)
		req = req.WithContext(callerRouteContext(req.Context(), rctx))
		w := httptest.NewRecorder()

		handler.GetBackupCodeStatus(w, req)

		assert.Equal(t, http.StatusInternalServerError, w.Code)
		assert.Contains(t, w.Body.String(), "MFA service not available")
	})
}
