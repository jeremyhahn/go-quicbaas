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
