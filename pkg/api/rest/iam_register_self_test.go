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
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/config"
)

// --------------------------------------------------------------------------
// Helpers
// --------------------------------------------------------------------------

// buildHandlerWithRegEnabled creates a test server with self-service
// registration enabled and returns the IAMHandler and a cleanup function.
func buildHandlerWithRegEnabled(t *testing.T, cfg config.IAMSelfServiceRegistrationConfig) (*IAMHandler, func()) {
	t.Helper()
	server, mockService, _, handler, cleanup := setupIAMTestServer(t)
	_ = mockService

	server.config.IAMSelfServiceRegistration = cfg
	return handler, cleanup
}

// postJSON fires a POST to the given handler function with a JSON body.
func postJSON(t *testing.T, handler http.HandlerFunc, body interface{}) *httptest.ResponseRecorder {
	t.Helper()
	b, err := json.Marshal(body)
	require.NoError(t, err)
	req := httptest.NewRequest(http.MethodPost, "/", bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	handler(w, req)
	return w
}

// --------------------------------------------------------------------------
// slugify
// --------------------------------------------------------------------------

func TestSlugify_BasicConversion(t *testing.T) {
	assert.Equal(t, "acme-corp", slugify("Acme Corp"))
	assert.Equal(t, "my-org", slugify("My Org"))
}

func TestSlugify_SpecialCharsStripped(t *testing.T) {
	// Exclamation mark is not alphanumeric/hyphen; replaced by hyphen then
	// collapsed into the surrounding context.
	result := slugify("Acme!Corp")
	assert.Equal(t, "acme-corp", result)
}

func TestSlugify_TooShortPadded(t *testing.T) {
	// A single-char name produces a slug shorter than 3; "org" is appended.
	result := slugify("a")
	assert.True(t, len(result) >= 3, "slug should be at least 3 chars, got %q", result)
}

func TestSlugify_TruncatedAt50(t *testing.T) {
	long := strings.Repeat("a", 60)
	result := slugify(long)
	assert.LessOrEqual(t, len(result), 50)
}

func TestSlugify_EmptyInput(t *testing.T) {
	// Empty name → no useful slug; deriveTenantID falls back to UUID.
	assert.Equal(t, "", slugify(""))
}

func TestSlugify_MultipleSpaces(t *testing.T) {
	// Multiple spaces collapse to a single hyphen.
	assert.Equal(t, "hello-world", slugify("  Hello   World  "))
}

func TestSlugify_SpecialAndSpaces(t *testing.T) {
	// Ampersand is stripped; surrounding spaces become hyphens that collapse.
	assert.Equal(t, "acme-corp", slugify("Acme & Corp"))
}

// --------------------------------------------------------------------------
// isReservedTenantID
// --------------------------------------------------------------------------

func TestIsReservedTenantID_SystemAndDefault(t *testing.T) {
	assert.True(t, isReservedTenantID("system"))
	assert.True(t, isReservedTenantID("default"))
}

func TestIsReservedTenantID_UnderscorePrefix(t *testing.T) {
	assert.True(t, isReservedTenantID("_internal"))
	assert.True(t, isReservedTenantID("_"))
}

func TestIsReservedTenantID_ColonPattern(t *testing.T) {
	assert.True(t, isReservedTenantID("ns:tenant"))
	assert.True(t, isReservedTenantID("a:b"))
}

func TestIsReservedTenantID_ValidIDs(t *testing.T) {
	assert.False(t, isReservedTenantID("acme"))
	assert.False(t, isReservedTenantID("my-org-42"))
	assert.False(t, isReservedTenantID("xyz"))
}

// --------------------------------------------------------------------------
// registrationSessionStore
// --------------------------------------------------------------------------

func TestRegistrationSessionStore_PutAndGet(t *testing.T) {
	store := &registrationSessionStore{}
	sess := newRegSession("acme", "Acme", "user@acme.com", "keyspace", 300)

	store.put("sess-1", sess)
	got, ok := store.get("sess-1")
	require.True(t, ok, "valid session should be retrievable")
	assert.Equal(t, "acme", got.tenantID)
	assert.Equal(t, "user@acme.com", got.email)
}

func TestRegistrationSessionStore_TTLExpiry(t *testing.T) {
	store := &registrationSessionStore{}

	// Use a negative duration so the session is immediately expired.
	sess := newRegSession("acme", "Acme", "user@acme.com", "keyspace", -1)
	store.put("expired", sess)
	_, ok := store.get("expired")
	assert.False(t, ok, "expired session should not be returned")
}

func TestRegistrationSessionStore_Delete(t *testing.T) {
	store := &registrationSessionStore{}
	sess := newRegSession("acme", "Acme", "user@acme.com", "keyspace", 1)
	store.put("to-delete", sess)
	store.delete("to-delete")
	_, ok := store.get("to-delete")
	assert.False(t, ok, "deleted session should not be retrievable")
}

func TestRegistrationSessionStore_GetMissing(t *testing.T) {
	store := &registrationSessionStore{}
	_, ok := store.get("nonexistent")
	assert.False(t, ok)
}

// newRegSession is a constructor helper for tests.
func newRegSession(tenantID, tenantName, email, isolationMode string, ttlSeconds int) *registrationSession {
	return &registrationSession{
		tenantID:      tenantID,
		tenantName:    tenantName,
		email:         email,
		isolationMode: isolationMode,
		expiresAt:     time.Now().Add(time.Duration(ttlSeconds) * time.Second),
	}
}

// --------------------------------------------------------------------------
// RegisterSelfBegin — gate checks
// --------------------------------------------------------------------------

func TestRegisterSelfBegin_DisabledReturns403(t *testing.T) {
	handler, cleanup := buildHandlerWithRegEnabled(t, config.IAMSelfServiceRegistrationConfig{
		Enabled: false,
	})
	defer cleanup()

	w := postJSON(t, handler.RegisterSelfBegin, SelfRegisterBeginRequest{
		TenantName:  "Acme",
		Email:       "user@acme.com",
		DisplayName: "Alice",
	})

	assert.Equal(t, http.StatusForbidden, w.Code)
	var body map[string]interface{}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
	assert.Equal(t, ErrSelfRegistrationDisabled, body["error"])
}

func TestRegisterSelfBegin_MissingTenantName(t *testing.T) {
	handler, cleanup := buildHandlerWithRegEnabled(t, config.IAMSelfServiceRegistrationConfig{
		Enabled: true,
	})
	defer cleanup()

	w := postJSON(t, handler.RegisterSelfBegin, SelfRegisterBeginRequest{
		Email:       "user@acme.com",
		DisplayName: "Alice",
	})
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestRegisterSelfBegin_MissingEmail(t *testing.T) {
	handler, cleanup := buildHandlerWithRegEnabled(t, config.IAMSelfServiceRegistrationConfig{
		Enabled: true,
	})
	defer cleanup()

	w := postJSON(t, handler.RegisterSelfBegin, SelfRegisterBeginRequest{
		TenantName:  "Acme",
		DisplayName: "Alice",
	})
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestRegisterSelfBegin_MissingDisplayName(t *testing.T) {
	handler, cleanup := buildHandlerWithRegEnabled(t, config.IAMSelfServiceRegistrationConfig{
		Enabled: true,
	})
	defer cleanup()

	w := postJSON(t, handler.RegisterSelfBegin, SelfRegisterBeginRequest{
		TenantName: "Acme",
		Email:      "user@acme.com",
	})
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestRegisterSelfBegin_InvalidIsolationModeRejected(t *testing.T) {
	handler, cleanup := buildHandlerWithRegEnabled(t, config.IAMSelfServiceRegistrationConfig{
		Enabled:               true,
		AllowedIsolationModes: []string{"keyspace"},
		DefaultIsolationMode:  "keyspace",
	})
	defer cleanup()

	w := postJSON(t, handler.RegisterSelfBegin, SelfRegisterBeginRequest{
		TenantName:    "Acme",
		Email:         "user@acme.com",
		DisplayName:   "Alice",
		IsolationMode: "barrier",
	})
	assert.Equal(t, http.StatusBadRequest, w.Code)
	var body map[string]interface{}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
	assert.Equal(t, ErrIsolationModeNotAllowed, body["error"])
}

func TestRegisterSelfBegin_DefaultIsolationModeApplied(t *testing.T) {
	// When the request omits isolation_mode and AllowedIsolationModes is
	// keyspace only, the default should be accepted (no 400).
	// The flow will fail at WebAuthn because the mock service doesn't expose
	// GetWebAuthnManager — but the gate validation passes.
	handler, cleanup := buildHandlerWithRegEnabled(t, config.IAMSelfServiceRegistrationConfig{
		Enabled:               true,
		AllowedIsolationModes: []string{"keyspace"},
		DefaultIsolationMode:  "keyspace",
	})
	defer cleanup()

	w := postJSON(t, handler.RegisterSelfBegin, SelfRegisterBeginRequest{
		TenantName:  "Acme Corp",
		Email:       "user@acme.com",
		DisplayName: "Alice",
		// IsolationMode intentionally omitted — default applies.
	})

	// Expected: passes gate, fails at WebAuthn (no manager) → 500.
	// Any status other than 400/403 means the gate passed.
	assert.NotEqual(t, http.StatusForbidden, w.Code)
	assert.NotEqual(t, http.StatusBadRequest, w.Code)
}

func TestRegisterSelfBegin_InvalidJSON(t *testing.T) {
	handler, cleanup := buildHandlerWithRegEnabled(t, config.IAMSelfServiceRegistrationConfig{
		Enabled: true,
	})
	defer cleanup()

	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader("{not json"))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	handler.RegisterSelfBegin(w, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

// --------------------------------------------------------------------------
// RegisterSelfFinish — gate + session checks
// --------------------------------------------------------------------------

func TestRegisterSelfFinish_DisabledReturns403(t *testing.T) {
	handler, cleanup := buildHandlerWithRegEnabled(t, config.IAMSelfServiceRegistrationConfig{
		Enabled: false,
	})
	defer cleanup()

	w := postJSON(t, handler.RegisterSelfFinish, SelfRegisterFinishRequest{
		SessionID:  "some-session",
		Credential: map[string]interface{}{"id": "abc"},
	})

	assert.Equal(t, http.StatusForbidden, w.Code)
	var body map[string]interface{}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
	assert.Equal(t, ErrSelfRegistrationDisabled, body["error"])
}

func TestRegisterSelfFinish_MissingSessionID(t *testing.T) {
	handler, cleanup := buildHandlerWithRegEnabled(t, config.IAMSelfServiceRegistrationConfig{
		Enabled: true,
	})
	defer cleanup()

	w := postJSON(t, handler.RegisterSelfFinish, SelfRegisterFinishRequest{
		Credential: map[string]interface{}{"id": "abc"},
	})
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestRegisterSelfFinish_MissingCredential(t *testing.T) {
	handler, cleanup := buildHandlerWithRegEnabled(t, config.IAMSelfServiceRegistrationConfig{
		Enabled: true,
	})
	defer cleanup()

	w := postJSON(t, handler.RegisterSelfFinish, SelfRegisterFinishRequest{
		SessionID: "some-session",
	})
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestRegisterSelfFinish_ExpiredSession(t *testing.T) {
	handler, cleanup := buildHandlerWithRegEnabled(t, config.IAMSelfServiceRegistrationConfig{
		Enabled: true,
	})
	defer cleanup()

	// No session was stored → get returns not-ok.
	w := postJSON(t, handler.RegisterSelfFinish, SelfRegisterFinishRequest{
		SessionID:  "nonexistent-session-id",
		Credential: map[string]interface{}{"id": "abc"},
	})
	assert.Equal(t, http.StatusBadRequest, w.Code)
	var body map[string]interface{}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
	assert.Equal(t, ErrRegistrationSessionExpired, body["error"])
}

func TestRegisterSelfFinish_InvalidJSON(t *testing.T) {
	handler, cleanup := buildHandlerWithRegEnabled(t, config.IAMSelfServiceRegistrationConfig{
		Enabled: true,
	})
	defer cleanup()

	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader("not-json"))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	handler.RegisterSelfFinish(w, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

// --------------------------------------------------------------------------
// ApproveUser
// --------------------------------------------------------------------------

func TestApproveUser_MissingUserID(t *testing.T) {
	_, _, _, handler, cleanup := setupIAMTestServer(t)
	defer cleanup()

	req := httptest.NewRequest(http.MethodPost, "/api/v1/iam/users//approve", nil)
	w := httptest.NewRecorder()
	handler.ApproveUser(w, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestApproveUser_MissingTenantID(t *testing.T) {
	_, _, _, handler, cleanup := setupIAMTestServer(t)
	defer cleanup()

	req := httptest.NewRequest(http.MethodPost, "/api/v1/iam/users/user-1/approve", nil)
	req = withChiParams(req, map[string]string{"user_id": "user-1"})
	w := httptest.NewRecorder()
	handler.ApproveUser(w, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestApproveUser_UnknownUserReturnsError(t *testing.T) {
	server, mockService, _, handler, cleanup := setupIAMTestServer(t)
	defer cleanup()
	_ = server
	_ = mockService

	req := httptest.NewRequest(http.MethodPost, "/api/v1/iam/users/nonexistent/approve?tenant_id=acme", nil)
	req = withChiParams(req, map[string]string{"user_id": "nonexistent"})
	w := httptest.NewRecorder()
	handler.ApproveUser(w, req)
	// Identity store returns not-found → any non-2xx is acceptable.
	assert.NotEqual(t, http.StatusOK, w.Code)
}

// --------------------------------------------------------------------------
// RejectUser
// --------------------------------------------------------------------------

func TestRejectUser_MissingUserID(t *testing.T) {
	_, _, _, handler, cleanup := setupIAMTestServer(t)
	defer cleanup()

	req := httptest.NewRequest(http.MethodPost, "/api/v1/iam/users//reject", nil)
	w := httptest.NewRecorder()
	handler.RejectUser(w, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestRejectUser_MissingTenantID(t *testing.T) {
	_, _, _, handler, cleanup := setupIAMTestServer(t)
	defer cleanup()

	req := httptest.NewRequest(http.MethodPost, "/api/v1/iam/users/user-1/reject", nil)
	req = withChiParams(req, map[string]string{"user_id": "user-1"})
	w := httptest.NewRecorder()
	handler.RejectUser(w, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestRejectUser_UnknownUserReturnsError(t *testing.T) {
	_, _, _, handler, cleanup := setupIAMTestServer(t)
	defer cleanup()

	req := httptest.NewRequest(http.MethodPost, "/api/v1/iam/users/nonexistent/reject?tenant_id=acme", nil)
	req = withChiParams(req, map[string]string{"user_id": "nonexistent"})
	w := httptest.NewRecorder()
	handler.RejectUser(w, req)
	assert.NotEqual(t, http.StatusOK, w.Code)
}

// --------------------------------------------------------------------------
// stringInSlice
// --------------------------------------------------------------------------

func TestStringInSlice_Found(t *testing.T) {
	assert.True(t, stringInSlice("keyspace", []string{"keyspace", "barrier"}))
}

func TestStringInSlice_NotFound(t *testing.T) {
	assert.False(t, stringInSlice("other", []string{"keyspace", "barrier"}))
}

func TestStringInSlice_EmptyList(t *testing.T) {
	assert.False(t, stringInSlice("keyspace", []string{}))
}

// --------------------------------------------------------------------------
// selfRegConfig
// --------------------------------------------------------------------------

func TestSelfRegConfig_NilServer(t *testing.T) {
	h := &IAMHandler{}
	cfg := h.selfRegConfig()
	assert.False(t, cfg.Enabled, "nil server should return zero config (disabled)")
}

func TestSelfRegConfig_ReadsFromServerConfig(t *testing.T) {
	server, _, _, handler, cleanup := setupIAMTestServer(t)
	defer cleanup()

	server.config.IAMSelfServiceRegistration = config.IAMSelfServiceRegistrationConfig{
		Enabled:     true,
		DefaultRole: "Operator",
	}

	cfg := handler.selfRegConfig()
	assert.True(t, cfg.Enabled)
	assert.Equal(t, "Operator", cfg.DefaultRole)
}
