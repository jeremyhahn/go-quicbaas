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

func TestSlugify(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{name: "basic conversion", in: "Acme Corp", want: "acme-corp"},
		{name: "second basic conversion", in: "My Org", want: "my-org"},
		// Exclamation mark is not alphanumeric/hyphen; replaced by hyphen.
		{name: "special chars stripped", in: "Acme!Corp", want: "acme-corp"},
		// Empty name → no useful slug; deriveTenantID falls back to UUID.
		{name: "empty input", in: "", want: ""},
		// Multiple spaces collapse to a single hyphen.
		{name: "multiple spaces", in: "  Hello   World  ", want: "hello-world"},
		// Ampersand is stripped; surrounding spaces become hyphens that collapse.
		{name: "special and spaces", in: "Acme & Corp", want: "acme-corp"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, slugify(tt.in))
		})
	}
}

func TestSlugify_LengthBounds(t *testing.T) {
	// A single-char name produces a slug shorter than 3; "org" is appended.
	short := slugify("a")
	assert.GreaterOrEqual(t, len(short), 3, "slug should be at least 3 chars, got %q", short)

	long := slugify(strings.Repeat("a", 60))
	assert.LessOrEqual(t, len(long), 50)
}

// --------------------------------------------------------------------------
// isReservedTenantID
// --------------------------------------------------------------------------

func TestIsReservedTenantID(t *testing.T) {
	tests := []struct {
		id   string
		want bool
	}{
		{id: "system", want: true},
		{id: "default", want: true},
		{id: "_internal", want: true},
		{id: "_", want: true},
		{id: "ns:tenant", want: true},
		{id: "a:b", want: true},
		{id: "acme", want: false},
		{id: "my-org-42", want: false},
		{id: "xyz", want: false},
	}
	for _, tt := range tests {
		t.Run(tt.id, func(t *testing.T) {
			assert.Equal(t, tt.want, isReservedTenantID(tt.id))
		})
	}
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

func TestRegisterSelfBegin_InvalidRequestReturns400(t *testing.T) {
	keyspaceOnly := config.IAMSelfServiceRegistrationConfig{
		Enabled:               true,
		AllowedIsolationModes: []string{"keyspace"},
		DefaultIsolationMode:  "keyspace",
	}
	tests := []struct {
		name    string
		body    string
		wantErr string
	}{
		{
			name: "missing tenant name",
			body: `{"email":"user@acme.com","display_name":"Alice"}`,
		},
		{
			name: "missing email",
			body: `{"tenant_name":"Acme","display_name":"Alice"}`,
		},
		{
			name: "missing display name",
			body: `{"tenant_name":"Acme","email":"user@acme.com"}`,
		},
		{
			name:    "isolation mode not allowed",
			body:    `{"tenant_name":"Acme","email":"user@acme.com","display_name":"Alice","isolation_mode":"barrier"}`,
			wantErr: ErrIsolationModeNotAllowed,
		},
		{
			name: "invalid JSON",
			body: "{not json",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			handler, cleanup := buildHandlerWithRegEnabled(t, keyspaceOnly)
			defer cleanup()

			req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(tt.body))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()
			handler.RegisterSelfBegin(w, req)

			assert.Equal(t, http.StatusBadRequest, w.Code)
			if tt.wantErr != "" {
				var body map[string]interface{}
				require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
				assert.Equal(t, tt.wantErr, body["error"])
			}
		})
	}
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
	req = withChiParams(req, map[string]string{"user_id": "10001"})
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
	req = withChiParams(req, map[string]string{"user_id": "10001"})
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

func TestStringInSlice(t *testing.T) {
	tests := []struct {
		name  string
		value string
		list  []string
		want  bool
	}{
		{name: "found", value: "keyspace", list: []string{"keyspace", "barrier"}, want: true},
		{name: "not found", value: "other", list: []string{"keyspace", "barrier"}, want: false},
		{name: "empty list", value: "keyspace", list: []string{}, want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, stringInSlice(tt.value, tt.list))
		})
	}
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
