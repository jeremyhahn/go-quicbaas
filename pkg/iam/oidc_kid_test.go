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
	"crypto"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"errors"
	"testing"
	"time"

	"github.com/go-jose/go-jose/v4"
)

// testSigningKeyManager builds the provider's signing key manager from an
// in-memory test key.
func testSigningKeyManager(t testing.TB, signer crypto.Signer, alg string) *SigningKeyManager {
	t.Helper()
	mgr, err := NewSigningKeyManagerFromSigner(signer, alg)
	if err != nil {
		t.Fatalf("NewSigningKeyManagerFromSigner: %v", err)
	}
	return mgr
}

// verifyByKID verifies token the way a standards relying party does: it reads
// the kid from the JWS header, selects that key from the published JWKS, and
// checks the signature with it. A token without a kid, or with a kid the JWKS
// does not publish, fails.
func verifyByKID(t *testing.T, name, token string, jwks *jose.JSONWebKeySet) {
	t.Helper()
	jws, err := jose.ParseSigned(token, []jose.SignatureAlgorithm{jose.RS256, jose.ES256})
	if err != nil {
		t.Fatalf("%s: parse JWS: %v", name, err)
	}
	kid := jws.Signatures[0].Header.KeyID
	if kid == "" {
		t.Fatalf("%s: JWS header has no kid", name)
	}
	keys := jwks.Key(kid)
	if len(keys) != 1 {
		t.Fatalf("%s: JWKS has %d keys for kid %q, want 1", name, len(keys), kid)
	}
	if _, err := jws.Verify(keys[0]); err != nil {
		t.Fatalf("%s: verify with JWKS key %q: %v", name, kid, err)
	}
}

// newKIDTokenService builds a TokenService through NewOIDCProvider, the
// production constructor, so every token it signs carries the signing key
// manager's kid. config.SigningKeyManager is set from signer and
// config.SigningAlgorithm.
func newKIDTokenService(t *testing.T, signer crypto.Signer, config *OIDCConfig, resolver PermissionResolver) *TokenService {
	t.Helper()
	config.SigningKeyManager = testSigningKeyManager(t, signer, config.SigningAlgorithm)
	memStorage := NewMemoryStorage()
	provider, err := NewOIDCProvider(config, OIDCDependencies{
		IdentityStore: NewIdentityStore(IdentityStoreOptions{
			UserStorage: memStorage,
			RoleStorage: memStorage,
		}),
		AppStorage:         newMockAppStorage(),
		TokenStorage:       newMockTokenStorage(),
		PermissionResolver: resolver,
	})
	if err != nil {
		t.Fatalf("NewOIDCProvider: %v", err)
	}
	return provider.tokenService
}

// requireKIDInServiceJWKS asserts that token's header kid resolves in the
// JWKS svc publishes, and that the resolved key verifies the signature.
func requireKIDInServiceJWKS(t *testing.T, svc *TokenService, token string) {
	t.Helper()
	verifyByKID(t, "token", token, svc.keyManager.JWKS())
}

// TestOIDCTokensCarryKIDInPublishedJWKS pins that the ID token and access
// token a provider issues each carry a kid header naming a key in the JWKS
// the provider publishes, so a verifier that selects keys by kid accepts them.
func TestOIDCTokensCarryKIDInPublishedJWKS(t *testing.T) {
	t.Parallel()

	provider, _, appStorage, tokenStorage, memStorage := createTestOIDCProvider(t)
	ctx := context.Background()

	app := &Application{
		ClientID:      "kid-client",
		ClientSecret:  currentSecretHash(t, "kid-secret"),
		TenantID:      "tenant-1",
		RedirectURIs:  []string{"https://example.com/callback"},
		ResponseTypes: []string{ResponseTypeCode},
		GrantTypes:    []string{GrantTypeAuthorizationCode},
	}
	if err := appStorage.CreateApp(ctx, app); err != nil {
		t.Fatalf("CreateApp: %v", err)
	}
	addUserToMemStorage(ctx, memStorage, &User{
		ID:        321,
		TenantID:  "tenant-1",
		Email:     "kid@example.com",
		Lifecycle: LifecycleActive,
		Roles:     []string{"user"},
	})
	if err := tokenStorage.StoreAuthCode(ctx, &AuthCodeData{
		Code:        "kid-code",
		UserID:      321,
		TenantID:    "tenant-1",
		ClientID:    "kid-client",
		RedirectURI: "https://example.com/callback",
		Scope:       []string{"openid"},
		ExpiresAt:   time.Now().Add(10 * time.Minute),
		CreatedAt:   time.Now(),
	}); err != nil {
		t.Fatalf("StoreAuthCode: %v", err)
	}

	resp, err := provider.Token(ctx, &TokenRequest{
		GrantType:    GrantTypeAuthorizationCode,
		Code:         "kid-code",
		RedirectURI:  "https://example.com/callback",
		ClientID:     "kid-client",
		ClientSecret: "kid-secret",
	})
	if err != nil {
		t.Fatalf("Token: %v", err)
	}

	jwks := provider.GetJWKS()
	verifyByKID(t, "ID token", resp.IDToken, jwks)
	verifyByKID(t, "access token", resp.AccessToken, jwks)
}

// TestTokenServiceTokensAlwaysCarryKIDInJWKS proves every JWT the token
// service signs (access, ID and both hardware tokens) carries a kid that its
// published JWKS contains, so an external RP's JWKS lookup resolves.
func TestTokenServiceTokensAlwaysCarryKIDInJWKS(t *testing.T) {
	t.Parallel()

	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("GenerateKey: %v", err)
	}
	svc := newKIDTokenService(t, key, &OIDCConfig{
		Issuer:           "https://test.example.com",
		SigningAlgorithm: SigningAlgorithmES256,
		AccessTokenTTL:   time.Hour,
		RefreshTokenTTL:  24 * time.Hour,
		IDTokenTTL:       time.Hour,
		AuthCodeTTL:      10 * time.Minute,
	}, nil)
	ctx := context.Background()
	now := time.Now()

	access, err := svc.GenerateAccessToken(ctx, &Claims{
		Issuer:    "https://test.example.com",
		Subject:   "user-1",
		Audience:  []string{"https://test.example.com"},
		ExpiresAt: NewUnixTime(now.Add(time.Hour)),
		IssuedAt:  NewUnixTime(now),
		NotBefore: NewUnixTime(now),
		JTI:       "kid-jti",
		TenantID:  "tenant-1",
	})
	if err != nil {
		t.Fatalf("GenerateAccessToken: %v", err)
	}
	id, err := svc.GenerateIDToken(ctx, &IDTokenClaims{
		Issuer:    "https://test.example.com",
		Subject:   "user-1",
		Audience:  []string{"client-1"},
		ExpiresAt: NewUnixTime(now.Add(time.Hour)),
		IssuedAt:  NewUnixTime(now),
		AuthTime:  NewUnixTime(now),
		TenantID:  "tenant-1",
	})
	if err != nil {
		t.Fatalf("GenerateIDToken: %v", err)
	}
	set, err := svc.IssueHardwareTokens(ctx, HardwareTokenOptions{
		User: &User{ID: 7, TenantID: "tenant-1", Email: "kid@example.com"},
	})
	if err != nil {
		t.Fatalf("IssueHardwareTokens: %v", err)
	}

	tokens := map[string]string{
		"access token":          access,
		"ID token":              id,
		"hardware access token": set.AccessToken,
		"hardware ID token":     set.IDToken,
	}
	jwks := svc.keyManager.JWKS()
	for name, token := range tokens {
		verifyByKID(t, name, token, jwks)
	}
}

// TestValidateAccessTokenRejectsKIDNotInJWKS proves a token whose kid the
// provider's JWKS does not contain fails verification with UnknownKeyIDError.
func TestValidateAccessTokenRejectsKIDNotInJWKS(t *testing.T) {
	t.Parallel()

	provider, _, _, _, _ := createTestOIDCProvider(t)

	foreignKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("GenerateKey: %v", err)
	}
	foreign := newKIDTokenService(t, foreignKey, &OIDCConfig{
		Issuer:           "https://test.example.com",
		SigningAlgorithm: SigningAlgorithmES256,
		AccessTokenTTL:   time.Hour,
	}, nil)
	now := time.Now()
	token, err := foreign.GenerateAccessToken(context.Background(), &Claims{
		Issuer:    "https://test.example.com",
		Subject:   "user-1",
		Audience:  []string{"https://test.example.com"},
		ExpiresAt: NewUnixTime(now.Add(time.Hour)),
		IssuedAt:  NewUnixTime(now),
		NotBefore: NewUnixTime(now),
		JTI:       "foreign-jti",
		TenantID:  "tenant-1",
	})
	if err != nil {
		t.Fatalf("GenerateAccessToken: %v", err)
	}

	_, err = provider.ValidateAccessToken(context.Background(), token)
	var unknown *UnknownKeyIDError
	if !errors.As(err, &unknown) {
		t.Fatalf("ValidateAccessToken err = %T %v, want *UnknownKeyIDError", err, err)
	}
	if unknown.KID == "" {
		t.Fatal("UnknownKeyIDError must name the token's kid")
	}
}
