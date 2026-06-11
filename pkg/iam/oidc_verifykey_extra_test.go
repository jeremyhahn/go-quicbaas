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
	"time"
)

// resolveVerificationKey resolves a token's kid via the manager and rejects an
// unknown kid; ValidateIDToken round-trips through that resolution.
func TestOIDCProvider_ResolveVerificationKey_Manager(t *testing.T) {
	setupTestXKMS(t)
	provider, mgr := newManagerBackedProvider(t)
	ctx := context.Background()

	// Mint an ID token via the token service so it carries the manager's kid.
	now := time.Now()
	idToken, err := provider.tokenService.GenerateIDToken(ctx, &IDTokenClaims{
		Issuer:    provider.config.Issuer,
		Subject:   "user-123",
		Audience:  []string{provider.config.Issuer},
		ExpiresAt: NewUnixTime(now.Add(time.Hour)),
		IssuedAt:  NewUnixTime(now),
		AuthTime:  NewUnixTime(now),
		TenantID:  "tenant-1",
	})
	if err != nil {
		t.Fatalf("GenerateIDToken: %v", err)
	}

	// resolveVerificationKey returns the manager's current public key.
	pub, err := provider.resolveVerificationKey(idToken)
	if err != nil {
		t.Fatalf("resolveVerificationKey: %v", err)
	}
	if pub == nil {
		t.Fatal("resolveVerificationKey returned a nil key")
	}

	// ValidateIDToken verifies and decodes the claims.
	claims, err := provider.ValidateIDToken(ctx, idToken)
	if err != nil {
		t.Fatalf("ValidateIDToken: %v", err)
	}
	if claims.Subject != "user-123" {
		t.Fatalf("subject = %q, want user-123", claims.Subject)
	}

	_ = mgr // manager kept alive for the duration of the test
}

// A token whose kid is unknown to the manager fails closed with UnknownKeyIDError.
func TestOIDCProvider_ResolveVerificationKey_UnknownKID(t *testing.T) {
	setupTestXKMS(t)
	providerA, _ := newManagerBackedProvider(t)
	ctx := context.Background()

	// Mint a token from a *different* manager (different kid).
	otherMgr, err := NewSigningKeyManagerFromXKMS("oidc-other-signing", "")
	if err != nil {
		t.Fatalf("other manager: %v", err)
	}
	otherProvider, err := NewOIDCProvider(&OIDCConfig{
		Issuer:            "https://test.example.com",
		SigningKeyManager: otherMgr,
		AccessTokenTTL:    time.Hour,
		RefreshTokenTTL:   time.Hour,
		IDTokenTTL:        time.Hour,
		AuthCodeTTL:       time.Minute,
		PKCERequired:      true,
		AllowedMethods:    []string{CodeChallengeMethodS256},
	}, OIDCDependencies{
		IdentityStore: NewIdentityStore(IdentityStoreOptions{
			UserStorage:  NewMemoryStorage(),
			GroupStorage: NewMemoryStorage(),
			RoleStorage:  NewMemoryStorage(),
		}),
		AppStorage:   newMockAppStorage(),
		TokenStorage: newMockTokenStorage(),
	})
	if err != nil {
		t.Fatalf("other provider: %v", err)
	}
	foreignToken := mintAccessToken(t, otherProvider)

	// providerA's manager does not know the foreign kid.
	_, err = providerA.resolveVerificationKey(foreignToken)
	if err == nil {
		t.Fatal("resolveVerificationKey(foreign kid) want error")
	}
	var unknown *UnknownKeyIDError
	if !errors.As(err, &unknown) {
		t.Fatalf("want UnknownKeyIDError, got %T: %v", err, err)
	}
	_ = ctx
}

// publicForKIDInJWKS resolves a matching kid and returns nil for a miss / nil set.
func TestPublicForKIDInJWKS(t *testing.T) {
	setupTestXKMS(t)
	_, mgr := newManagerBackedProvider(t)

	jwks := mgr.JWKS()
	if jwks == nil || len(jwks.Keys) == 0 {
		t.Fatal("manager JWKS is empty")
	}
	kid := jwks.Keys[0].KeyID

	if pub := publicForKIDInJWKS(jwks, kid); pub == nil {
		t.Fatal("publicForKIDInJWKS should resolve a present kid")
	}
	if pub := publicForKIDInJWKS(jwks, "no-such-kid"); pub != nil {
		t.Fatal("publicForKIDInJWKS should return nil for a missing kid")
	}
	if pub := publicForKIDInJWKS(nil, kid); pub != nil {
		t.Fatal("publicForKIDInJWKS(nil set) should return nil")
	}
}

// A provider with no signing material at all reports SigningKeyUnavailableError.
func TestOIDCProvider_ResolveVerificationKey_NoKey(t *testing.T) {
	p := &OIDCProvider{config: &OIDCConfig{}}
	_, err := p.resolveVerificationKey("eyJhbGciOiJSUzI1NiJ9.e30.")
	if err == nil {
		t.Fatal("resolveVerificationKey with no key want error")
	}
	var unavailable *SigningKeyUnavailableError
	if !errors.As(err, &unavailable) {
		t.Fatalf("want SigningKeyUnavailableError, got %T", err)
	}
}
