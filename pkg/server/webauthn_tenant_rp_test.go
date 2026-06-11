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
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/iam"
)

// fakeWebAuthnSettings is an in-memory TenantWebAuthnSettingsProvider whose
// per-tenant override can be mutated between calls so tests can drive the
// per-field-fallback and cache-rebuild scenarios without a live tenant store.
type fakeWebAuthnSettings struct {
	mu        sync.Mutex
	overrides map[string]*iam.TenantWebAuthnConfig
}

func newFakeWebAuthnSettings() *fakeWebAuthnSettings {
	return &fakeWebAuthnSettings{overrides: map[string]*iam.TenantWebAuthnConfig{}}
}

func (f *fakeWebAuthnSettings) set(tenantID string, cfg *iam.TenantWebAuthnConfig) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.overrides[tenantID] = cfg
}

func (f *fakeWebAuthnSettings) WebAuthnSettingsFor(_ context.Context, tenantID string) (*iam.TenantWebAuthnConfig, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.overrides[tenantID], nil
}

// buildLiveWebAuthnFactoryWithSettings mirrors buildLiveWebAuthnFactory but
// injects a per-tenant WebAuthn settings provider so ServiceFor resolves the
// tenant's RP override with per-field fallback to the global config.
func buildLiveWebAuthnFactoryWithSettings(t *testing.T, settings iam.TenantWebAuthnSettingsProvider) *iam.WebAuthnServiceFactory {
	t.Helper()

	node := buildMemoryStandaloneNode(t)
	t.Cleanup(func() { _ = node.Close() })

	client := node.Client()
	require.NotNil(t, client, "memory node must expose a live SDK client")

	factory, err := iam.NewWebAuthnServiceFactory(iam.WebAuthnFactoryParams{
		Config: iam.WebAuthnFactoryConfig{
			RPID:          "global.example.com",
			RPDisplayName: "Global RP",
			RPOrigins:     []string{"https://global.example.com"},
			Timeout:       60 * time.Second,
		},
		QRDB:     client,
		Settings: settings,
	})
	require.NoError(t, err)
	return factory
}

// TestWebAuthnFactory_PerTenantRPID_PerFieldFallback verifies scenario (a): a
// tenant that sets only RPID gets ITS RPID but INHERITS the global origins
// (per-field fallback), and a tenant with no override gets the global RP.
func TestWebAuthnFactory_PerTenantRPID_PerFieldFallback(t *testing.T) {
	settings := newFakeWebAuthnSettings()

	// Dedicated tenant overrides only RPID + display name; origins inherit.
	settings.set("barrier-tenant", &iam.TenantWebAuthnConfig{
		RPID:          "barrier.example.com",
		RPDisplayName: "Barrier RP",
		// RPOrigins intentionally empty -> inherits global origins.
	})

	factory := buildLiveWebAuthnFactoryWithSettings(t, settings)
	ctx := context.Background()

	// Dedicated tenant: its RPID, its display name, INHERITED global origins.
	svc, err := factory.ServiceFor(ctx, "barrier-tenant")
	require.NoError(t, err)
	cfg := svc.Config()
	assert.Equal(t, "barrier.example.com", cfg.RPID, "tenant RPID must override the global")
	assert.Equal(t, "Barrier RP", cfg.RPDisplayName)
	assert.Equal(t, []string{"https://global.example.com"}, cfg.RPOrigins,
		"empty origins must fall back to the global origins (per-field fallback)")

	// Tenant with no override: full global RP (today's behavior preserved).
	plain, err := factory.ServiceFor(ctx, "plain-tenant")
	require.NoError(t, err)
	pcfg := plain.Config()
	assert.Equal(t, "global.example.com", pcfg.RPID)
	assert.Equal(t, "Global RP", pcfg.RPDisplayName)
	assert.Equal(t, []string{"https://global.example.com"}, pcfg.RPOrigins)
}

// TestWebAuthnFactory_OverrideOrigins verifies the origins field is overridden
// when the tenant sets it (the complement of the inherit-origins case).
func TestWebAuthnFactory_OverrideOrigins(t *testing.T) {
	settings := newFakeWebAuthnSettings()
	settings.set("vhost-tenant", &iam.TenantWebAuthnConfig{
		RPID:      "vhost.example.com",
		RPOrigins: []string{"https://vhost.example.com", "https://www.vhost.example.com"},
		// RPDisplayName empty -> inherits global display name.
	})

	factory := buildLiveWebAuthnFactoryWithSettings(t, settings)
	svc, err := factory.ServiceFor(context.Background(), "vhost-tenant")
	require.NoError(t, err)
	cfg := svc.Config()
	assert.Equal(t, "vhost.example.com", cfg.RPID)
	assert.Equal(t, "Global RP", cfg.RPDisplayName, "empty display name must inherit the global")
	assert.Equal(t, []string{"https://vhost.example.com", "https://www.vhost.example.com"}, cfg.RPOrigins)
}

// TestWebAuthnFactory_SettingsChangeRebuildsCachedService verifies scenario
// (b): after a tenant updates its WebAuthn settings, OnTenantSettingsChanged
// evicts the cached service so the next ServiceFor rebuilds it with the new RP.
func TestWebAuthnFactory_SettingsChangeRebuildsCachedService(t *testing.T) {
	settings := newFakeWebAuthnSettings()
	settings.set("rotating-tenant", &iam.TenantWebAuthnConfig{RPID: "old.example.com"})

	factory := buildLiveWebAuthnFactoryWithSettings(t, settings)
	ctx := context.Background()

	first, err := factory.ServiceFor(ctx, "rotating-tenant")
	require.NoError(t, err)
	assert.Equal(t, "old.example.com", first.Config().RPID)

	// Same tenant resolves to the cached service before any change.
	cached, err := factory.ServiceFor(ctx, "rotating-tenant")
	require.NoError(t, err)
	assert.Same(t, first, cached, "service must be cached")

	// Update the tenant's settings and notify the factory (the wiring the
	// TenantManager settings observer performs after UpdateTenant).
	settings.set("rotating-tenant", &iam.TenantWebAuthnConfig{RPID: "new.example.com"})
	factory.OnTenantSettingsChanged("rotating-tenant")

	rebuilt, err := factory.ServiceFor(ctx, "rotating-tenant")
	require.NoError(t, err)
	assert.NotSame(t, first, rebuilt, "cache must rebuild after a settings change")
	assert.Equal(t, "new.example.com", rebuilt.Config().RPID,
		"rebuilt service must reflect the updated RPID")
}
