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
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/iam"
)

// TestCreateUser_ForwardedHostAndCappedExpiry exercises the X-Forwarded-Host
// invite-URL branch, the invite-expiry cap (> 168h is clamped), and the default
// auth-methods branch (omitted -> ["webauthn"]).
func TestCreateUser_ForwardedHostAndCappedExpiry(t *testing.T) {
	_, _, memStorage, handler, cleanup := setupIAMTestServer(t)
	defer cleanup()

	ctx := context.Background()
	require.NoError(t, memStorage.CreateTenant(ctx, &iam.Tenant{ID: "tenant1", Name: "Tenant 1"}))

	// invite_expiry_hours > 168 must be clamped; auth_methods omitted defaults.
	body := `{"email":"fwd@example.com","username":"fwduser","name":"Fwd User","invite_expiry_hours":1000}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/tenants/tenant1/users", bytes.NewReader([]byte(body)))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Forwarded-Host", "public.example.com")
	req = withChiParams(req, map[string]string{"tenant_id": "tenant1"})
	w := httptest.NewRecorder()

	handler.CreateUser(w, req)

	require.Equal(t, http.StatusCreated, w.Code, "body: %s", w.Body.String())

	var resp map[string]interface{}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	inviteURL, _ := resp["invite_url"].(string)
	assert.Contains(t, inviteURL, "public.example.com",
		"invite URL should use the X-Forwarded-Host value")
}

// TestListUsers_TenantNotFound covers the 404 branch taken when the tenant does
// not exist in the identity store.
func TestListUsers_TenantNotFound(t *testing.T) {
	_, _, _, handler, cleanup := setupIAMTestServer(t)
	defer cleanup()

	req := httptest.NewRequest(http.MethodGet, "/api/v1/tenants/ghost/users", nil)
	req = withChiParams(req, map[string]string{"tenant_id": "ghost"})
	w := httptest.NewRecorder()

	handler.ListUsers(w, req)

	require.Equal(t, http.StatusNotFound, w.Code, "body: %s", w.Body.String())
	assert.Contains(t, w.Body.String(), "tenant not found")
}
