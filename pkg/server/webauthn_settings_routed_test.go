//go:build !integration

// Copyright (c) 2026 Jeremy Hahn
// Copyright (c) 2026 Automate The Things, LLC
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

package server

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/iam"
	"github.com/jeremyhahn/go-quicbaas/pkg/kvstore"
	"github.com/jeremyhahn/go-quicbaas/pkg/tenantctx"
)

// Credential administration reaches the WebAuthn settings adapter with the
// target tenant bound to the context. The tenant catalog lives in the system
// namespace, so on tenant-routed storage the adapter must still find a
// barrier-isolated tenant's relying-party override: reading the tenant's own
// store finds no catalog record, which would hand the tenant the global
// relying party.
func TestTenantManagerWebAuthnSettings_ReadsTheCatalogForATenantBoundCaller(t *testing.T) {
	r := newFakeStoreResolver(kvstore.NewMemory())
	r.barrierTenant("acme")
	tenants := iam.NewDefaultTenantManager(iam.NewBackendStorage(newTenantRoutingKVStore(r)))

	ctx := context.Background()
	sys := tenantctx.WithSystem(ctx)
	_, err := tenants.CreateTenant(sys, iam.CreateTenantOptions{ID: "acme", Name: "Acme"})
	require.NoError(t, err)
	override := &iam.TenantWebAuthnConfig{
		RPID:          "acme.example.com",
		RPDisplayName: "Acme",
		RPOrigins:     []string{"https://acme.example.com"},
	}
	_, err = tenants.UpdateTenant(sys, "acme", iam.UpdateTenantOptions{
		Settings: &iam.TenantSettings{WebAuthn: override},
	})
	require.NoError(t, err)
	adapter := iam.NewTenantManagerWebAuthnSettings(tenants)

	for name, callerCtx := range map[string]context.Context{
		"a caller bound to the tenant": tenantctx.WithTenant(ctx, "acme"),
		"a caller bound to the system": sys,
	} {
		t.Run(name, func(t *testing.T) {
			got, err := adapter.WebAuthnSettingsFor(callerCtx, "acme")
			require.NoError(t, err)
			require.NotNil(t, got, "the tenant's override must be found, not the global relying party")
			assert.Equal(t, override, got)
		})
	}
}
