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
	"errors"
	"testing"
	"time"

	"github.com/jeremyhahn/go-quicbaas/pkg/xkms"
	platformtoken "github.com/jeremyhahn/go-quicbaas/sdk/go/platformtoken"
)

// fakeOIDCSettings is an in-memory TenantOIDCSettingsProvider for resolver tests.
type fakeOIDCSettings struct {
	dedicated map[string]bool
	err       error
}

func (f *fakeOIDCSettings) OIDCSettingsFor(_ context.Context, tenantID string) (*TenantOIDCConfig, error) {
	if f.err != nil {
		return nil, f.err
	}
	if f.dedicated[tenantID] {
		return &TenantOIDCConfig{DedicatedSigningKey: true}, nil
	}
	return nil, nil
}

func (f *fakeOIDCSettings) DedicatedSigningTenants(_ context.Context) ([]string, error) {
	if f.err != nil {
		return nil, f.err
	}
	var ids []string
	for id, on := range f.dedicated {
		if on {
			ids = append(ids, id)
		}
	}
	return ids, nil
}

// newResolverWithGlobal builds a resolver over a real xkms-backed global key.
func newResolverWithGlobal(t *testing.T, settings TenantOIDCSettingsProvider) (*TenantSigningKeyResolver, *SigningKeyManager) {
	t.Helper()
	global, err := NewSigningKeyManagerFromXKMS(OIDCSigningKeyCN, "")
	if err != nil {
		t.Fatalf("global signing key manager: %v", err)
	}
	r, err := NewTenantSigningKeyResolver(global, settings)
	if err != nil {
		t.Fatalf("resolver: %v", err)
	}
	return r, global
}

// TestResolver_NonDedicatedTenantUsesGlobalKey verifies scenario (d): a tenant
// that has not opted in uses the global key/kid.
func TestResolver_NonDedicatedTenantUsesGlobalKey(t *testing.T) {
	setupTestXKMS(t)
	settings := &fakeOIDCSettings{dedicated: map[string]bool{}}
	r, global := newResolverWithGlobal(t, settings)

	mgr, err := r.ManagerFor(context.Background(), "ordinary-tenant")
	if err != nil {
		t.Fatalf("ManagerFor: %v", err)
	}
	if mgr != global {
		t.Fatalf("non-dedicated tenant must use the global manager")
	}
	gkid, _, _ := global.Current()
	rkid, _, _ := mgr.Current()
	if rkid != gkid {
		t.Fatalf("non-dedicated tenant kid %q != global kid %q", rkid, gkid)
	}
}

// TestResolver_DedicatedTenantGetsOwnKid verifies a dedicated tenant resolves
// to a DIFFERENT manager with a DIFFERENT kid than the global key, and that the
// per-tenant manager is cached (same instance on the second call).
func TestResolver_DedicatedTenantGetsOwnKid(t *testing.T) {
	setupTestXKMS(t)
	settings := &fakeOIDCSettings{dedicated: map[string]bool{"iso-tenant": true}}
	r, global := newResolverWithGlobal(t, settings)

	mgr, err := r.ManagerFor(context.Background(), "iso-tenant")
	if err != nil {
		t.Fatalf("ManagerFor: %v", err)
	}
	if mgr == global {
		t.Fatalf("dedicated tenant must NOT use the global manager")
	}
	gkid, _, _ := global.Current()
	tkid, _, _ := mgr.Current()
	if tkid == "" || tkid == gkid {
		t.Fatalf("dedicated tenant kid %q must differ from global kid %q", tkid, gkid)
	}

	again, err := r.ManagerFor(context.Background(), "iso-tenant")
	if err != nil {
		t.Fatalf("ManagerFor (cached): %v", err)
	}
	if again != mgr {
		t.Fatalf("dedicated tenant manager must be cached")
	}
}

// TestResolver_JWKSPublishesBothKids verifies the aggregated JWKS includes the
// global kid AND every active dedicated tenant kid.
func TestResolver_JWKSPublishesBothKids(t *testing.T) {
	setupTestXKMS(t)
	settings := &fakeOIDCSettings{dedicated: map[string]bool{"iso-tenant": true}}
	r, global := newResolverWithGlobal(t, settings)

	// Force the dedicated manager to exist so we know both kids.
	dedicated, err := r.ManagerFor(context.Background(), "iso-tenant")
	if err != nil {
		t.Fatalf("ManagerFor: %v", err)
	}
	gkid, _, _ := global.Current()
	tkid, _, _ := dedicated.Current()

	jwks := r.JWKS(context.Background())
	have := map[string]bool{}
	for _, k := range jwks.Keys {
		have[k.KeyID] = true
	}
	if !have[gkid] {
		t.Fatalf("JWKS missing global kid %q", gkid)
	}
	if !have[tkid] {
		t.Fatalf("JWKS missing dedicated tenant kid %q", tkid)
	}
}

// TestResolver_DedicatedTenantFailsClosed verifies scenario (e): a dedicated
// tenant whose key cannot be built (e.g. sealed) fails closed — ManagerFor
// returns a TenantSigningKeyUnavailableError instead of the global key.
func TestResolver_DedicatedTenantFailsClosed(t *testing.T) {
	setupTestXKMS(t)
	settings := &fakeOIDCSettings{dedicated: map[string]bool{"sealed-tenant": true}}
	r, global := newResolverWithGlobal(t, settings)

	// Inject a builder that fails for the sealed tenant (models a sealed
	// per-tenant keystore that cannot produce a signer).
	sealErr := errors.New("tenant keystore sealed")
	r.build = func(tenantID string) (*SigningKeyManager, error) {
		return nil, sealErr
	}

	_, err := r.ManagerFor(context.Background(), "sealed-tenant")
	if err == nil {
		t.Fatalf("dedicated tenant with unbuildable key must fail closed, got nil error")
	}
	var unavailable *TenantSigningKeyUnavailableError
	if !errors.As(err, &unavailable) {
		t.Fatalf("expected *TenantSigningKeyUnavailableError, got %T: %v", err, err)
	}
	if unavailable.TenantID != "sealed-tenant" {
		t.Fatalf("error tenant id = %q, want sealed-tenant", unavailable.TenantID)
	}
	if !errors.Is(err, sealErr) {
		t.Fatalf("error must wrap the build cause")
	}

	// A non-dedicated tenant still uses the global key normally even though the
	// builder would fail (the builder is never invoked for it).
	mgr, err := r.ManagerFor(context.Background(), "ordinary-tenant")
	if err != nil {
		t.Fatalf("non-dedicated tenant must not fail closed: %v", err)
	}
	if mgr != global {
		t.Fatalf("non-dedicated tenant must use the global manager")
	}
}

// TestResolver_OnSettingsChangeEvictsCache verifies the resolver rebuilds a
// tenant's manager after its settings change (cache invalidation).
func TestResolver_OnSettingsChangeEvictsCache(t *testing.T) {
	setupTestXKMS(t)
	settings := &fakeOIDCSettings{dedicated: map[string]bool{"iso-tenant": true}}
	r, _ := newResolverWithGlobal(t, settings)

	first, err := r.ManagerFor(context.Background(), "iso-tenant")
	if err != nil {
		t.Fatalf("ManagerFor: %v", err)
	}

	r.OnTenantSettingsChanged("iso-tenant")

	second, err := r.ManagerFor(context.Background(), "iso-tenant")
	if err != nil {
		t.Fatalf("ManagerFor after eviction: %v", err)
	}
	if first == second {
		t.Fatalf("manager must be rebuilt after a settings change")
	}
}

// TestProvider_DedicatedAndGlobalTokensValidateViaPlatformToken is the
// end-to-end scenario (c)+(d): a dedicated tenant's access token is signed by a
// DIFFERENT kid than a global (non-dedicated) tenant's token, the provider JWKS
// publishes BOTH kids, and the single-anchor platformtoken validator verifies
// BOTH against that one JWKS + one issuer.
func TestProvider_DedicatedAndGlobalTokensValidateViaPlatformToken(t *testing.T) {
	setupTestXKMS(t)

	const issuer = "https://broker.example.com"
	settings := &fakeOIDCSettings{dedicated: map[string]bool{"dedicated-tenant": true}}
	resolver, global := newResolverWithGlobal(t, settings)

	memStorage := NewMemoryStorage()
	identityStore := NewIdentityStore(IdentityStoreOptions{
		UserStorage:  memStorage,
		GroupStorage: memStorage,
		RoleStorage:  memStorage,
	})
	provider, err := NewOIDCProvider(&OIDCConfig{
		Issuer:            issuer,
		SigningAlgorithm:  SigningAlgorithmES256,
		SigningKeyManager: global,
		TenantSigningKeys: resolver,
		AccessTokenTTL:    time.Hour,
		RefreshTokenTTL:   time.Hour,
		IDTokenTTL:        time.Hour,
		AuthCodeTTL:       10 * time.Minute,
		PKCERequired:      true,
		AllowedMethods:    []string{CodeChallengeMethodS256},
	}, OIDCDependencies{
		IdentityStore: identityStore,
		AppStorage:    newMockAppStorage(),
		TokenStorage:  newMockTokenStorage(),
	})
	if err != nil {
		t.Fatalf("NewOIDCProvider: %v", err)
	}

	mint := func(tenantID string) string {
		t.Helper()
		now := time.Now()
		claims := &Claims{
			Issuer:    issuer,
			Subject:   "user-" + tenantID,
			Audience:  []string{issuer},
			ExpiresAt: NewUnixTime(now.Add(time.Hour)),
			IssuedAt:  NewUnixTime(now),
			NotBefore: NewUnixTime(now),
			JTI:       generateJTI(),
			TenantID:  tenantID,
		}
		tok, err := provider.tokenService.GenerateAccessToken(context.Background(), claims)
		if err != nil {
			t.Fatalf("GenerateAccessToken(%s): %v", tenantID, err)
		}
		return tok
	}

	dedicatedTok := mint("dedicated-tenant")
	globalTok := mint("ordinary-tenant")

	dedKID, _ := xkms.JWTExtractKID(dedicatedTok)
	globKID, _ := xkms.JWTExtractKID(globalTok)
	if dedKID == "" || globKID == "" {
		t.Fatalf("both tokens must carry a kid (ded=%q glob=%q)", dedKID, globKID)
	}
	if dedKID == globKID {
		t.Fatalf("dedicated tenant token kid must differ from the global tenant token kid")
	}

	// The provider's published JWKS must contain both kids (single anchor).
	jwks := provider.GetJWKS()
	have := map[string]bool{}
	for _, k := range jwks.Keys {
		have[k.KeyID] = true
	}
	if !have[dedKID] || !have[globKID] {
		t.Fatalf("JWKS must publish both kids: have=%v ded=%q glob=%q", have, dedKID, globKID)
	}

	// Single-anchor validator: one issuer, one JWKS (multi-kid). It must verify
	// BOTH tokens by kid.
	validator, err := platformtoken.NewValidator(platformtoken.Options{
		Issuer:   issuer,
		Audience: issuer,
		JWKS:     jwks,
	})
	if err != nil {
		t.Fatalf("NewValidator: %v", err)
	}

	if _, err := validator.ValidatePlatformToken(context.Background(), dedicatedTok); err != nil {
		t.Fatalf("validator rejected dedicated-tenant token: %v", err)
	}
	if _, err := validator.ValidatePlatformToken(context.Background(), globalTok); err != nil {
		t.Fatalf("validator rejected global-tenant token: %v", err)
	}
}
