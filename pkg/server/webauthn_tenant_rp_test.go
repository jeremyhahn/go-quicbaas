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
	"sync"
	"testing"
	"time"

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"
	xkmssdk "github.com/jeremyhahn/go-xkms/sdk/go"

	"github.com/jeremyhahn/go-quicbaas/pkg/testutil"

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
		QRDB:                client,
		AttestationRecorder: func() qrdbsdk.AttestationRecorder { return testutil.NewMemoryUserAuthFactors() },
		Settings:            settings,
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

// TestWebAuthnFactory_StoredInvalidOverrideFailsClosed pins go-quicbaas#368:
// an override stored before tenant writes were validated must not build a
// relying party that can never verify a ceremony. The live xkms service
// accepts a hostless origin, so only the factory's own check refuses it.
func TestWebAuthnFactory_StoredInvalidOverrideFailsClosed(t *testing.T) {
	settings := newFakeWebAuthnSettings()
	settings.set("legacy-tenant", &iam.TenantWebAuthnConfig{
		RPID:      "legacy.example.com",
		RPOrigins: []string{"https://legacy.example.com", "https://"},
	})

	factory := buildLiveWebAuthnFactoryWithSettings(t, settings)
	svc, err := factory.ServiceFor(context.Background(), "legacy-tenant")
	assert.Nil(t, svc)
	var rpErr *iam.WebAuthnTenantRPError
	require.ErrorAs(t, err, &rpErr)
	assert.Equal(t, "legacy-tenant", rpErr.TenantID)

	// A tenant with no override is unaffected by its neighbour's bad one.
	_, err = factory.ServiceFor(context.Background(), "other-tenant")
	require.NoError(t, err)
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

// gatedWebAuthnSettings wraps fakeWebAuthnSettings so a test can park the
// FIRST settings read inside the factory's service build. That first call
// reads the inner settings, closes entered, blocks until release is closed,
// then returns gatedErr when it is set or the settings it read otherwise. Every later call passes straight
// through to the inner settings.
type gatedWebAuthnSettings struct {
	inner    *fakeWebAuthnSettings
	once     sync.Once
	entered  chan struct{}
	release  chan struct{}
	gatedErr error
}

func newGatedWebAuthnSettings(inner *fakeWebAuthnSettings) *gatedWebAuthnSettings {
	return &gatedWebAuthnSettings{
		inner:   inner,
		entered: make(chan struct{}),
		release: make(chan struct{}),
	}
}

func (g *gatedWebAuthnSettings) WebAuthnSettingsFor(ctx context.Context, tenantID string) (*iam.TenantWebAuthnConfig, error) {
	gated := false
	g.once.Do(func() { gated = true })
	// Read before parking, so a gated call returns the settings that were
	// committed when the build read them, not those committed while it waited.
	override, err := g.inner.WebAuthnSettingsFor(ctx, tenantID)
	if gated {
		close(g.entered)
		<-g.release
		if g.gatedErr != nil {
			return nil, g.gatedErr
		}
	}
	return override, err
}

// serviceResult carries a ServiceFor outcome out of a goroutine.
type serviceResult struct {
	svc *xkmssdk.WebAuthnService
	err error
}

// TestWebAuthnFactory_ResetDuringBuildEvictsStaleService pins go-quicbaas#384:
// a settings change that lands while a ServiceFor build is reading the OLD
// settings must not leave the stale relying party cached.
func TestWebAuthnFactory_ResetDuringBuildEvictsStaleService(t *testing.T) {
	inner := newFakeWebAuthnSettings()
	inner.set("racing-tenant", &iam.TenantWebAuthnConfig{RPID: "old.example.com"})
	gated := newGatedWebAuthnSettings(inner)
	factory := buildLiveWebAuthnFactoryWithSettings(t, gated)
	ctx := context.Background()

	done := make(chan serviceResult, 1)
	go func() {
		svc, err := factory.ServiceFor(ctx, "racing-tenant")
		done <- serviceResult{svc: svc, err: err}
	}()
	<-gated.entered

	// The build has read nothing yet; commit the new settings and notify the
	// factory while the build is parked. The reset must not block.
	inner.set("racing-tenant", &iam.TenantWebAuthnConfig{RPID: "new.example.com"})
	factory.OnTenantSettingsChanged("racing-tenant")
	close(gated.release)

	inFlight := <-done
	require.NoError(t, inFlight.err)
	assert.Equal(t, "old.example.com", inFlight.svc.Config().RPID,
		"the in-flight caller keeps the relying party it was built with")

	next, err := factory.ServiceFor(ctx, "racing-tenant")
	require.NoError(t, err)
	assert.NotSame(t, inFlight.svc, next, "the stale service must not outlive the reset")
	assert.Equal(t, "new.example.com", next.Config().RPID)
}

// TestWebAuthnFactory_ResetBeforeBuildKeepsFreshService covers a reset that
// completes before the build starts: the build reads the committed settings,
// and its service stays cached rather than being evicted needlessly.
func TestWebAuthnFactory_ResetBeforeBuildKeepsFreshService(t *testing.T) {
	settings := newFakeWebAuthnSettings()
	settings.set("settled-tenant", &iam.TenantWebAuthnConfig{RPID: "new.example.com"})
	factory := buildLiveWebAuthnFactoryWithSettings(t, settings)
	ctx := context.Background()

	factory.OnTenantSettingsChanged("settled-tenant")

	built, err := factory.ServiceFor(ctx, "settled-tenant")
	require.NoError(t, err)
	assert.Equal(t, "new.example.com", built.Config().RPID)

	cached, err := factory.ServiceFor(ctx, "settled-tenant")
	require.NoError(t, err)
	assert.Same(t, built, cached, "a build that follows the reset must stay cached")
}

// TestWebAuthnFactory_ResetDuringFailedBuildCachesNothing covers the error
// path: a build that races a reset and fails to read settings caches nothing,
// surfaces the typed error unchanged, and a later call builds afresh.
func TestWebAuthnFactory_ResetDuringFailedBuildCachesNothing(t *testing.T) {
	inner := newFakeWebAuthnSettings()
	inner.set("failing-tenant", &iam.TenantWebAuthnConfig{RPID: "new.example.com"})
	gated := newGatedWebAuthnSettings(inner)
	gated.gatedErr = &iam.WebAuthnSettingsReadError{TenantID: "failing-tenant", Cause: errSettingsUnavailable}
	factory := buildLiveWebAuthnFactoryWithSettings(t, gated)
	ctx := context.Background()

	done := make(chan serviceResult, 1)
	go func() {
		svc, err := factory.ServiceFor(ctx, "failing-tenant")
		done <- serviceResult{svc: svc, err: err}
	}()
	<-gated.entered
	factory.OnTenantSettingsChanged("failing-tenant")
	close(gated.release)

	failed := <-done
	require.Error(t, failed.err)
	assert.Nil(t, failed.svc)
	var readErr *iam.WebAuthnSettingsReadError
	require.ErrorAs(t, failed.err, &readErr)
	assert.Equal(t, "failing-tenant", readErr.TenantID)

	rebuilt, err := factory.ServiceFor(ctx, "failing-tenant")
	require.NoError(t, err, "a failed build must leave nothing cached")
	assert.Equal(t, "new.example.com", rebuilt.Config().RPID)
}

// settingsUnavailableError is the cause the failing settings read wraps.
type settingsUnavailableError struct{}

func (settingsUnavailableError) Error() string { return "tenant settings unavailable" }

var errSettingsUnavailable = settingsUnavailableError{}
