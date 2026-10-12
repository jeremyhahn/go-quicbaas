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
	"errors"
	"testing"
)

// stubWebAuthnSettings is an injectable TenantWebAuthnSettingsProvider for
// exercising resolveConfig without a live qrdb backend.
type stubWebAuthnSettings struct {
	override *TenantWebAuthnConfig
	err      error
}

func (s stubWebAuthnSettings) WebAuthnSettingsFor(_ context.Context, _ string) (*TenantWebAuthnConfig, error) {
	return s.override, s.err
}

func newTestWebAuthnFactory(t *testing.T, settings TenantWebAuthnSettingsProvider) *WebAuthnServiceFactory {
	t.Helper()
	f, err := NewWebAuthnServiceFactory(WebAuthnFactoryParams{
		Config: WebAuthnFactoryConfig{
			RPID:          "example.com",
			RPDisplayName: "Example",
			RPOrigins:     []string{"https://example.com"},
		},
		QRDB:                dummyQRDBClient,
		AttestationRecorder: recorderOf(&fakeUserAuthFactorClient{}),
		Settings:            settings,
	})
	if err != nil {
		t.Fatalf("NewWebAuthnServiceFactory: %v", err)
	}
	return f
}

// Factory accessor returns the wrapped factory instance.
func TestXKMSWebAuthnManager_FactoryAccessor(t *testing.T) {
	f := newTestWebAuthnFactory(t, nil)
	m, err := NewXKMSWebAuthnManager(f)
	if err != nil {
		t.Fatalf("NewXKMSWebAuthnManager: %v", err)
	}
	if m.Factory() != f {
		t.Fatal("Factory() did not return the wrapped factory")
	}
}

// resolveConfig: no settings provider yields the global config unchanged.
func TestWebAuthnFactory_ResolveConfig_NoSettings(t *testing.T) {
	f := newTestWebAuthnFactory(t, nil)
	cfg, err := f.resolveConfig(context.Background(), "acme")
	if err != nil {
		t.Fatalf("resolveConfig: %v", err)
	}
	if cfg.RPID != "example.com" {
		t.Fatalf("RPID = %q, want example.com", cfg.RPID)
	}
}

// resolveConfig: a nil override inherits the global config; a populated override
// replaces the per-field values; a provider error propagates.
func TestWebAuthnFactory_ResolveConfig_WithSettings(t *testing.T) {
	// Nil override -> global config.
	f := newTestWebAuthnFactory(t, stubWebAuthnSettings{override: nil})
	cfg, err := f.resolveConfig(context.Background(), "acme")
	if err != nil || cfg.RPID != "example.com" {
		t.Fatalf("nil override resolveConfig = %v, %v", cfg, err)
	}

	// Populated override -> per-field replacement.
	f = newTestWebAuthnFactory(t, stubWebAuthnSettings{override: &TenantWebAuthnConfig{
		RPID:          "tenant.example",
		RPDisplayName: "Tenant",
		RPOrigins:     []string{"https://tenant.example"},
	}})
	cfg, err = f.resolveConfig(context.Background(), "acme")
	if err != nil {
		t.Fatalf("resolveConfig(override): %v", err)
	}
	if cfg.RPID != "tenant.example" || cfg.RPDisplayName != "Tenant" || len(cfg.RPOrigins) != 1 {
		t.Fatalf("override not applied: %+v", cfg)
	}

	// Provider error propagates.
	f = newTestWebAuthnFactory(t, stubWebAuthnSettings{err: errors.New("settings down")})
	if _, err := f.resolveConfig(context.Background(), "acme"); err == nil {
		t.Fatal("resolveConfig should propagate provider error")
	}
}

// Reset, ResetTenant, and OnTenantSettingsChanged evict cached services. We seed
// the cache directly (a nil service entry is sufficient to observe eviction).
func TestWebAuthnFactory_CacheEviction(t *testing.T) {
	f := newTestWebAuthnFactory(t, nil)

	count := func() int {
		n := 0
		f.services.Range(func(_, _ any) bool { n++; return true })
		return n
	}

	f.services.Store("t1", (*struct{})(nil))
	f.services.Store("t2", (*struct{})(nil))
	if count() != 2 {
		t.Fatalf("seed count = %d, want 2", count())
	}

	f.ResetTenant("t1")
	if count() != 1 {
		t.Fatalf("after ResetTenant count = %d, want 1", count())
	}
	if got := f.generation("t1").Load(); got != 1 {
		t.Fatalf("after ResetTenant t1 generation = %d, want 1", got)
	}

	f.OnTenantSettingsChanged("t2")
	if count() != 0 {
		t.Fatalf("after OnTenantSettingsChanged count = %d, want 0", count())
	}
	if got := f.generation("t2").Load(); got != 1 {
		t.Fatalf("after OnTenantSettingsChanged t2 generation = %d, want 1", got)
	}

	// Resetting an uncached tenant evicts nothing, but still bumps its
	// generation so a build in flight for it cannot cache a stale service.
	f.services.Store("t4", (*struct{})(nil))
	f.ResetTenant("uncached")
	if count() != 1 {
		t.Fatalf("after ResetTenant of an uncached tenant count = %d, want 1", count())
	}
	if got := f.generation("uncached").Load(); got != 1 {
		t.Fatalf("uncached generation = %d, want 1", got)
	}

	// Re-seed and Reset clears everything and bumps every known generation,
	// including tenants that are not cached.
	f.services.Store("t3", (*struct{})(nil))
	f.Reset()
	if count() != 0 {
		t.Fatalf("after Reset count = %d, want 0", count())
	}
	want := map[string]uint64{"t1": 2, "t2": 2, "uncached": 2}
	for tenantID, w := range want {
		if got := f.generation(tenantID).Load(); got != w {
			t.Fatalf("after Reset %s generation = %d, want %d", tenantID, got, w)
		}
	}
}

// TenantManagerWebAuthnSettings adapter resolves overrides and handles nils.
func TestTenantManagerWebAuthnSettings_Adapter(t *testing.T) {
	ctx := context.Background()

	// Nil adapter / nil manager returns (nil, nil).
	var nilAdapter *TenantManagerWebAuthnSettings
	if cfg, err := nilAdapter.WebAuthnSettingsFor(ctx, "t"); cfg != nil || err != nil {
		t.Fatalf("nil adapter WebAuthnSettingsFor = %v, %v", cfg, err)
	}

	mgr := newTestTenantManager()
	withOverride, err := mgr.CreateTenant(ctx, CreateTenantOptions{
		Name: "Override",
		Settings: &TenantSettings{
			WebAuthn: &TenantWebAuthnConfig{RPID: "rp.example"},
		},
	})
	if err != nil {
		t.Fatalf("CreateTenant: %v", err)
	}

	adapter := NewTenantManagerWebAuthnSettings(mgr)
	cfg, err := adapter.WebAuthnSettingsFor(ctx, withOverride.ID)
	if err != nil {
		t.Fatalf("WebAuthnSettingsFor: %v", err)
	}
	if cfg == nil || cfg.RPID != "rp.example" {
		t.Fatalf("WebAuthnSettingsFor = %v, want RPID rp.example", cfg)
	}

	// An unresolvable tenant is treated as no override (nil, nil).
	cfg, err = adapter.WebAuthnSettingsFor(ctx, "does-not-exist")
	if err != nil || cfg != nil {
		t.Fatalf("WebAuthnSettingsFor(missing) = %v, %v; want nil, nil", cfg, err)
	}
}
