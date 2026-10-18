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

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/iam"
	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
	"github.com/jeremyhahn/go-quicbaas/pkg/testutil"
)

// realTenantManagerService serves the REST tenant handlers from a real
// iam.DefaultTenantManager, so the write path's own validation is exercised.
type realTenantManagerService struct {
	*testutil.MockQuicBaaS
	tenants *iam.DefaultTenantManager
}

func (s *realTenantManagerService) GetTenantManager(context.Context) (TenantManager, error) {
	return s.tenants, nil
}

func newRealTenantManagerHandler(t *testing.T) (*IAMHandler, *iam.DefaultTenantManager) {
	t.Helper()
	log, err := logger.New(logger.LogConfig{Level: "error", Format: "json", Output: "stdout"})
	require.NoError(t, err)

	tenants := iam.NewDefaultTenantManager(iam.NewMemoryStorage())
	_, err = tenants.CreateTenant(context.Background(), iam.CreateTenantOptions{ID: "acme", Name: "Acme"})
	require.NoError(t, err)

	cfg := DefaultConfig()
	cfg.Address = ":0"
	server, err := NewServer(&realTenantManagerService{
		MockQuicBaaS: testutil.NewMockQuicBaaS(),
		tenants:      tenants,
	}, cfg, log)
	require.NoError(t, err)
	t.Cleanup(func() { _ = server.Close() })

	return NewIAMHandler(IAMHandlerDependencies{Server: server}), tenants
}

func putTenant(handler *IAMHandler, tenantID, body string) *httptest.ResponseRecorder {
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("tenant_id", tenantID)
	req := httptest.NewRequest(http.MethodPut, "/api/v1/tenants/"+tenantID, strings.NewReader(body))
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	handler.UpdateTenant(w, req)
	return w
}

func TestUpdateTenant_RefusesAnInvalidWebAuthnOverrideAsBadRequest(t *testing.T) {
	tests := []struct {
		name  string
		body  string
		field string
	}{
		{
			name:  "RP ID is a URL",
			body:  `{"settings":{"webauthn":{"rp_id":"https://acme.example.com"}}}`,
			field: "settings.webauthn.rp_id",
		},
		{
			name:  "origin has no host",
			body:  `{"settings":{"webauthn":{"rp_origins":["https://"]}}}`,
			field: "settings.webauthn.rp_origins",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			handler, tenants := newRealTenantManagerHandler(t)

			w := putTenant(handler, "acme", tt.body)

			assert.Equal(t, http.StatusBadRequest, w.Code, w.Body.String())
			assert.Contains(t, w.Body.String(), tt.field)
			stored, err := tenants.GetTenant(context.Background(), "acme")
			require.NoError(t, err)
			assert.Nil(t, stored.Settings.WebAuthn, "a refused override must not be stored")
		})
	}
}

func TestUpdateTenant_StoresAValidWebAuthnOverride(t *testing.T) {
	handler, tenants := newRealTenantManagerHandler(t)

	w := putTenant(handler, "acme",
		`{"settings":{"webauthn":{"rp_id":"acme.example.com","rp_origins":["https://acme.example.com"]}}}`)

	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	stored, err := tenants.GetTenant(context.Background(), "acme")
	require.NoError(t, err)
	require.NotNil(t, stored.Settings.WebAuthn)
	assert.Equal(t, "acme.example.com", stored.Settings.WebAuthn.RPID)
	assert.Equal(t, []string{"https://acme.example.com"}, stored.Settings.WebAuthn.RPOrigins)
}

// TestUpdateTenant_StoresDisplayNameInSettings proves a display_name update
// lands in the tenant's display name, leaves its Name alone and keeps the
// other stored settings (#1518).
func TestUpdateTenant_StoresDisplayNameInSettings(t *testing.T) {
	handler, tenants := newRealTenantManagerHandler(t)
	ctx := context.Background()

	w := putTenant(handler, "acme", `{"settings":{"max_users":42}}`)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	before, err := tenants.GetTenant(ctx, "acme")
	require.NoError(t, err)

	w = putTenant(handler, "acme", `{"display_name":"Acme Corp"}`)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())

	stored, err := tenants.GetTenant(ctx, "acme")
	require.NoError(t, err)
	assert.Equal(t, "Acme Corp", stored.Settings.DisplayName)
	assert.Equal(t, before.Name, stored.Name)
	assert.Equal(t, 42, stored.Settings.MaxUsers)
}

// TestUpdateTenant_DisplayNameOfUnknownTenantIsNotFound proves a display_name
// update of a tenant that does not exist answers 404.
func TestUpdateTenant_DisplayNameOfUnknownTenantIsNotFound(t *testing.T) {
	handler, _ := newRealTenantManagerHandler(t)

	w := putTenant(handler, "missing", `{"display_name":"Nobody"}`)

	assert.Equal(t, http.StatusNotFound, w.Code, w.Body.String())
}
