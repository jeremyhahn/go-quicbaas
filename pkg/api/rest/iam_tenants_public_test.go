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
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/iam"
)

// TestListPublicTenants_TenantManagerNotAvailable verifies that when the service
// doesn't implement GetTenantManager, the handler returns 500.
func TestListPublicTenants_TenantManagerNotAvailable(t *testing.T) {
	handler, _, cleanup := setupIAMHandler(t)
	defer cleanup()

	req := httptest.NewRequest(http.MethodGet, "/api/v1/iam/tenants/public", nil)
	w := httptest.NewRecorder()

	handler.ListPublicTenants(w, req)

	assert.Equal(t, http.StatusInternalServerError, w.Code)
	assert.Contains(t, w.Body.String(), "tenant manager not available")
}

// TestListPublicTenants_ReturnsVisibleTenants verifies that tenants without
// PublicLoginHidden set (the default) are returned.
func TestListPublicTenants_ReturnsVisibleTenants(t *testing.T) {
	server, mockSvc, _, handler, cleanup := setupIAMTestServer(t)
	_ = server
	defer cleanup()

	ctx := context.Background()
	mgr, err := mockSvc.GetTenantManager(ctx)
	require.NoError(t, err)

	// Create two tenants: one visible, one hidden.
	_, err = mgr.CreateTenant(ctx, iam.CreateTenantOptions{
		ID:   "acme",
		Name: "Acme Corp",
		Settings: &iam.TenantSettings{
			DisplayName: "Acme Corporation",
		},
	})
	require.NoError(t, err)

	_, err = mgr.CreateTenant(ctx, iam.CreateTenantOptions{
		ID:   "internal",
		Name: "Internal SA",
		Settings: &iam.TenantSettings{
			PublicLoginHidden: true,
		},
	})
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/iam/tenants/public", nil)
	w := httptest.NewRecorder()

	handler.ListPublicTenants(w, req)

	require.Equal(t, http.StatusOK, w.Code)

	var resp PublicTenantsResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))

	// "internal" is hidden; only "acme" should appear.
	for _, t2 := range resp.Tenants {
		assert.NotEqual(t, "internal", t2.ID, "hidden tenant must not appear in public list")
	}
	var found bool
	for _, t2 := range resp.Tenants {
		if t2.ID == "acme" {
			found = true
			assert.Equal(t, "Acme Corporation", t2.DisplayName)
		}
	}
	assert.True(t, found, "visible tenant 'acme' must appear in public list")
}

// TestListPublicTenants_DisplayNameFallback verifies that when DisplayName is
// empty, the tenant Name is used as the display_name.
func TestListPublicTenants_DisplayNameFallback(t *testing.T) {
	_, mockSvc, _, handler, cleanup := setupIAMTestServer(t)
	defer cleanup()

	ctx := context.Background()
	mgr, err := mockSvc.GetTenantManager(ctx)
	require.NoError(t, err)

	_, err = mgr.CreateTenant(ctx, iam.CreateTenantOptions{
		ID:   "noname",
		Name: "No Display Name",
		// No DisplayName set — should fall back to Name.
	})
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/iam/tenants/public", nil)
	w := httptest.NewRecorder()

	handler.ListPublicTenants(w, req)

	require.Equal(t, http.StatusOK, w.Code)

	var resp PublicTenantsResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))

	for _, t2 := range resp.Tenants {
		if t2.ID == "noname" {
			assert.Equal(t, "No Display Name", t2.DisplayName, "display_name must fall back to Name when empty")
			return
		}
	}
	t.Error("expected tenant 'noname' in response")
}
