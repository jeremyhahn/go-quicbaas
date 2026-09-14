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

package iam

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// User email/PIV-thumbprint index scoping tests were removed in phase C1-F:
// BackendStorage no longer persists users or those lookup indexes (qrdb owns
// the user record). Tenant scoping of the user lookup indexes is covered by the
// qrdbadapter test suite. The app/token tenant-scoping tests below remain — the
// app and token indexes are still BaaS-owned.

// Scenario (e): OAuth app keys are tenant-scoped; the clientID encodes the
// tenant (app_<tenant>_<suffix>), so two tenants' apps never collide.
func TestAppKey_TenantScopedByClientID(t *testing.T) {
	kv := newMockKVStore()
	storage := NewBackendStorage(kv)
	ctx := context.Background()

	appA := &Application{ClientID: "app_tenant-a_abc123", TenantID: "tenant-a", Name: "A"}
	appB := &Application{ClientID: "app_tenant-b_def456", TenantID: "tenant-b", Name: "B"}
	require.NoError(t, storage.CreateApp(ctx, appA))
	require.NoError(t, storage.CreateApp(ctx, appB))

	gotA, err := storage.GetApp(ctx, "app_tenant-a_abc123")
	require.NoError(t, err)
	assert.Equal(t, "tenant-a", gotA.TenantID)

	gotB, err := storage.GetApp(ctx, "app_tenant-b_def456")
	require.NoError(t, err)
	assert.Equal(t, "tenant-b", gotB.TenantID)

	// The clientID parser extracts the tenant.
	assert.Equal(t, "tenant-a", TenantFromClientID("app_tenant-a_abc123"))
	assert.Equal(t, "", TenantFromClientID("not-an-app-id"))
}

// Scenario (e): OAuth token validation derives the tenant from the JWT
// tenant_id claim — modeled here by the token record's TenantID being used to
// scope the key, resolved on read via the tenant in context.
func TestTokenKey_TenantScoped_DerivedFromClaim(t *testing.T) {
	kv := newMockKVStore()
	storage := NewBackendStorage(kv)

	ctxA := ContextWithTenant(context.Background(), "tenant-a")
	ctxB := ContextWithTenant(context.Background(), "tenant-b")

	require.NoError(t, storage.StoreToken(ctxA, &Token{JTI: "shared-jti", Subject: "u", TenantID: "tenant-a", TokenType: TokenTypeAccess, Expires: time.Now().Add(time.Hour)}))
	require.NoError(t, storage.StoreToken(ctxB, &Token{JTI: "shared-jti", Subject: "u", TenantID: "tenant-b", TokenType: TokenTypeAccess, Expires: time.Now().Add(time.Hour)}))

	gotA, err := storage.GetToken(ctxA, "shared-jti")
	require.NoError(t, err)
	assert.Equal(t, "tenant-a", gotA.TenantID)

	gotB, err := storage.GetToken(ctxB, "shared-jti")
	require.NoError(t, err)
	assert.Equal(t, "tenant-b", gotB.TenantID)
}
