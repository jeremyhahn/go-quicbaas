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

	"github.com/jeremyhahn/go-quicbaas/pkg/xkms"
)

// newManagerBackedProvider builds an OIDC provider whose signing key is the
// xkms-backed ES256 key manager, returning the provider and the manager so
// tests can drive rotation.
func newManagerBackedProvider(t *testing.T) (*OIDCProvider, *SigningKeyManager) {
	t.Helper()

	mgr, err := NewSigningKeyManagerFromXKMS("oidc-signing-provider-test", "")
	if err != nil {
		t.Fatalf("failed to create signing key manager: %v", err)
	}

	config := &OIDCConfig{
		Issuer:            "https://test.example.com",
		SigningKeyManager: mgr,
		AccessTokenTTL:    1 * time.Hour,
		RefreshTokenTTL:   24 * time.Hour,
		IDTokenTTL:        1 * time.Hour,
		AuthCodeTTL:       10 * time.Minute,
		PKCERequired:      true,
		AllowedMethods:    []string{CodeChallengeMethodS256},
	}

	memStorage := NewMemoryStorage()
	identityStore := NewIdentityStore(IdentityStoreOptions{
		UserStorage:  memStorage,
		GroupStorage: memStorage,
		RoleStorage:  memStorage,
	})

	deps := OIDCDependencies{
		IdentityStore: identityStore,
		AppStorage:    newMockAppStorage(),
		TokenStorage:  newMockTokenStorage(),
	}

	provider, err := NewOIDCProvider(config, deps)
	if err != nil {
		t.Fatalf("failed to create OIDC provider: %v", err)
	}
	return provider, mgr
}

func mintAccessToken(t *testing.T, p *OIDCProvider) string {
	t.Helper()
	ctx := context.Background()
	now := time.Now()
	claims := &Claims{
		Issuer:    p.config.Issuer,
		Subject:   "user-123",
		Audience:  []string{p.config.Issuer},
		ExpiresAt: NewUnixTime(now.Add(time.Hour)),
		IssuedAt:  NewUnixTime(now),
		NotBefore: NewUnixTime(now),
		JTI:       generateJTI(),
		TenantID:  "tenant-1",
	}
	token, err := p.tokenService.GenerateAccessToken(ctx, claims)
	if err != nil {
		t.Fatalf("GenerateAccessToken failed: %v", err)
	}
	return token
}

func TestProviderTokenCarriesKIDAndUsesES256(t *testing.T) {
	setupTestXKMS(t)
	provider, mgr := newManagerBackedProvider(t)

	if provider.config.SigningAlgorithm != SigningAlgorithmES256 {
		t.Fatalf("expected provider algorithm ES256, got %q", provider.config.SigningAlgorithm)
	}

	token := mintAccessToken(t, provider)

	kid, err := xkms.JWTExtractKID(token)
	if err != nil {
		t.Fatalf("JWTExtractKID failed: %v", err)
	}
	currentKID, _, _ := mgr.Current()
	if kid != currentKID {
		t.Fatalf("token kid %q != current kid %q", kid, currentKID)
	}
}

func TestProviderValidateAccessTokenWithManager(t *testing.T) {
	setupTestXKMS(t)
	provider, _ := newManagerBackedProvider(t)

	token := mintAccessToken(t, provider)

	claims, err := provider.ValidateAccessToken(context.Background(), token)
	if err != nil {
		t.Fatalf("ValidateAccessToken failed: %v", err)
	}
	if claims.Subject != "user-123" {
		t.Fatalf("unexpected subject: %q", claims.Subject)
	}
}

func TestProviderPreviousKeyTokenValidatesAfterRotation(t *testing.T) {
	setupTestXKMS(t)
	provider, mgr := newManagerBackedProvider(t)

	// Mint a token under the current (soon to be previous) key.
	tokenBeforeRotation := mintAccessToken(t, provider)
	oldKID, _ := xkms.JWTExtractKID(tokenBeforeRotation)

	if err := mgr.Rotate(); err != nil {
		t.Fatalf("Rotate failed: %v", err)
	}

	// The pre-rotation token must still validate against the retained
	// previous key during the overlap window.
	claims, err := provider.ValidateAccessToken(context.Background(), tokenBeforeRotation)
	if err != nil {
		t.Fatalf("pre-rotation token failed to validate after rotation: %v", err)
	}
	if claims.Subject != "user-123" {
		t.Fatalf("unexpected subject: %q", claims.Subject)
	}

	// A freshly minted token must carry the new kid and also validate.
	tokenAfterRotation := mintAccessToken(t, provider)
	newKID, _ := xkms.JWTExtractKID(tokenAfterRotation)
	if newKID == oldKID {
		t.Fatal("token minted after rotation still carries the old kid")
	}
	if _, err := provider.ValidateAccessToken(context.Background(), tokenAfterRotation); err != nil {
		t.Fatalf("post-rotation token failed to validate: %v", err)
	}
}

func TestProviderJWKSReflectsManager(t *testing.T) {
	setupTestXKMS(t)
	provider, mgr := newManagerBackedProvider(t)

	jwks := provider.GetJWKS()
	if len(jwks.Keys) != 1 {
		t.Fatalf("expected 1 JWKS key, got %d", len(jwks.Keys))
	}

	if err := mgr.Rotate(); err != nil {
		t.Fatalf("Rotate failed: %v", err)
	}

	// GetJWKS must return the live manager set (now two keys).
	jwks = provider.GetJWKS()
	if len(jwks.Keys) != 2 {
		t.Fatalf("expected 2 JWKS keys after rotation, got %d", len(jwks.Keys))
	}
}
