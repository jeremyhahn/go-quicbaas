//go:build iam

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

package iam

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// newTenantAuthzProvider builds a minimal OIDCProvider wired only with an
// IdentityStore backed by an in-memory TenantManager. TenantAuthzMode depends
// solely on the identity store, so this keeps the test focused and fast.
func newTenantAuthzProvider(t *testing.T) (*OIDCProvider, *DefaultTenantManager) {
	t.Helper()
	storage := newMockTenantStorage()
	tm := NewDefaultTenantManager(storage)
	store := NewIdentityStore(IdentityStoreOptions{Tenants: tm})
	return &OIDCProvider{identityStore: store}, tm
}

func TestOIDCProviderTenantAuthzMode_Override(t *testing.T) {
	provider, tm := newTenantAuthzProvider(t)
	ctx := context.Background()

	_, err := tm.CreateTenant(ctx, CreateTenantOptions{
		ID:       "tenant-a",
		Name:     "Tenant A",
		Settings: &TenantSettings{AuthzMode: "online"},
	})
	require.NoError(t, err)

	mode, err := provider.TenantAuthzMode(ctx, "tenant-a")
	require.NoError(t, err)
	assert.Equal(t, "online", mode)
}

func TestOIDCProviderTenantAuthzMode_EmptyWhenUnset(t *testing.T) {
	provider, tm := newTenantAuthzProvider(t)
	ctx := context.Background()

	_, err := tm.CreateTenant(ctx, CreateTenantOptions{
		ID:   "tenant-b",
		Name: "Tenant B",
		// No AuthzMode override.
		Settings: &TenantSettings{},
	})
	require.NoError(t, err)

	mode, err := provider.TenantAuthzMode(ctx, "tenant-b")
	require.NoError(t, err)
	assert.Empty(t, mode, "unset override must resolve to empty so caller uses deployment default")
}

func TestOIDCProviderTenantAuthzMode_EmptyTenantID(t *testing.T) {
	provider, _ := newTenantAuthzProvider(t)
	mode, err := provider.TenantAuthzMode(context.Background(), "")
	require.NoError(t, err)
	assert.Empty(t, mode)
}

func TestOIDCProviderTenantAuthzMode_LookupErrorSurfaced(t *testing.T) {
	provider, _ := newTenantAuthzProvider(t)
	// No tenant created -> GetTenant returns a not-found error which is surfaced
	// to the caller. The server-side resolver is responsible for failing SAFE to
	// the deployment default on this error.
	mode, err := provider.TenantAuthzMode(context.Background(), "missing")
	require.Error(t, err)
	assert.Empty(t, mode)
}

// nilTenantManager returns (nil, nil) from GetTenant to exercise the defensive
// nil-tenant branch in TenantAuthzMode (a TenantManager that reports neither a
// tenant nor an error).
type nilTenantManager struct{}

func (nilTenantManager) CreateTenant(context.Context, CreateTenantOptions) (*Tenant, error) {
	return nil, nil
}
func (nilTenantManager) GetTenant(context.Context, string) (*Tenant, error) { return nil, nil }
func (nilTenantManager) UpdateTenant(context.Context, string, UpdateTenantOptions) (*Tenant, error) {
	return nil, nil
}
func (nilTenantManager) DeleteTenant(context.Context, string) error { return nil }
func (nilTenantManager) ListTenants(context.Context, ListOptions) ([]*Tenant, error) {
	return nil, nil
}
func (nilTenantManager) CheckQuota(context.Context, string, string) error { return nil }

func TestOIDCProviderTenantAuthzMode_NilTenant(t *testing.T) {
	store := NewIdentityStore(IdentityStoreOptions{Tenants: nilTenantManager{}})
	provider := &OIDCProvider{identityStore: store}

	mode, err := provider.TenantAuthzMode(context.Background(), "ghost")
	require.NoError(t, err)
	assert.Empty(t, mode, "a nil tenant must resolve to empty (use deployment default)")
}
