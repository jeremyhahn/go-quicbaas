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
	"encoding/base64"
	"encoding/json"
	"testing"
	"time"
)

// unsignedJWT builds an "alg":"none" JWT carrying claims. It parses as a JWT
// but carries no signature, so it must never be trusted for revocation.
func unsignedJWT(t *testing.T, claims map[string]any) string {
	t.Helper()
	header, err := json.Marshal(map[string]string{"alg": "none", "typ": "JWT"})
	if err != nil {
		t.Fatalf("marshal header: %v", err)
	}
	payload, err := json.Marshal(claims)
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}
	enc := base64.RawURLEncoding
	return enc.EncodeToString(header) + "." + enc.EncodeToString(payload) + "."
}

// assertIntrospectActive fails the test unless token still introspects active.
func assertIntrospectActive(t *testing.T, provider *OIDCProvider, token string) {
	t.Helper()
	resp, err := provider.Introspect(context.Background(), &IntrospectRequest{Token: token})
	if err != nil {
		t.Fatalf("Introspect() error = %v", err)
	}
	if !resp.Active {
		t.Fatalf("real token must stay active")
	}
}

func TestRevokeToken_UnverifiedJWTRevokesNothing(t *testing.T) {
	provider, _, _, _, _ := createTestOIDCProvider(t)
	attacker, _, _, _, _ := createTestOIDCProvider(t)
	ctx := context.Background()

	realToken, realJTI := mintIntrospectToken(t, provider, "tenant-a", "victim", 0, time.Hour)
	now := time.Now()

	// Wrongly signed: a different provider's key signs a token naming the
	// real jti under another tenant.
	wronglySigned, err := attacker.tokenService.GenerateAccessToken(ctx, &Claims{
		Issuer:    provider.config.Issuer,
		Subject:   "attacker",
		Audience:  []string{"https://rs.example.com"},
		ExpiresAt: NewUnixTime(now.Add(time.Hour)),
		IssuedAt:  NewUnixTime(now),
		JTI:       realJTI,
		TenantID:  "tenant-b",
	})
	if err != nil {
		t.Fatalf("GenerateAccessToken() error = %v", err)
	}

	tests := []struct {
		name  string
		token string
	}{
		{"unsigned other tenant", unsignedJWT(t, map[string]any{"jti": realJTI, "tenant_id": "tenant-b"})},
		{"unsigned same tenant", unsignedJWT(t, map[string]any{"jti": realJTI, "tenant_id": "tenant-a"})},
		{"wrongly signed other tenant", wronglySigned},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := provider.tokenService.RevokeToken(ctx, tt.token); err == nil {
				t.Fatalf("RevokeToken() of an unverified JWT must report the token unknown")
			}
			assertIntrospectActive(t, provider, realToken)
			for _, tenant := range []string{"tenant-a", "tenant-b"} {
				revoked, err := provider.tokenService.storage.IsTokenRevoked(withTokenTenant(ctx, tenant), realJTI)
				if err != nil {
					t.Fatalf("IsTokenRevoked(%s) error = %v", tenant, err)
				}
				if revoked {
					t.Fatalf("jti revoked in %s by an unverified token", tenant)
				}
			}
		})
	}
}

func TestRevokeToken_SignedAccessTokenRevoked(t *testing.T) {
	provider, _, _, _, _ := createTestOIDCProvider(t)
	ctx := context.Background()

	token, _ := mintIntrospectToken(t, provider, "tenant-a", "user-rev", 0, time.Hour)
	assertIntrospectActive(t, provider, token)

	if err := provider.tokenService.RevokeToken(ctx, token); err != nil {
		t.Fatalf("RevokeToken() error = %v", err)
	}
	resp, err := provider.Introspect(ctx, &IntrospectRequest{Token: token})
	if err != nil {
		t.Fatalf("Introspect() error = %v", err)
	}
	if resp.Active {
		t.Fatalf("revoked token must be inactive")
	}
}

func TestRevokeToken_StoredRefreshTokenRevokedByValue(t *testing.T) {
	provider, _, _, _, _ := createTestOIDCProvider(t)
	ctx := context.Background()

	refresh, err := provider.tokenService.GenerateRefreshToken(ctx, SessionRefreshOptions{
		Subject:  "user-123",
		TenantID: "tenant-a",
		ClientID: "client-123",
	})
	if err != nil {
		t.Fatalf("GenerateRefreshToken() error = %v", err)
	}
	// The mock store indexes tokens by user, not by value: register the
	// stored refresh record under its value so the by-value fallback finds it.
	tokens, err := provider.tokenService.storage.ListTokensByUser(ctx, "user-123")
	if err != nil {
		t.Fatalf("ListTokensByUser() error = %v", err)
	}
	var stored *Token
	for _, token := range tokens {
		if token.TokenType == TokenTypeRefresh {
			stored = token
		}
	}
	if stored == nil {
		t.Fatalf("no stored refresh token")
	}
	provider.tokenService.storage.(*mockTokenStorage).tokenValues[refresh] = stored

	if err := provider.tokenService.RevokeToken(ctx, refresh); err != nil {
		t.Fatalf("RevokeToken() error = %v", err)
	}
	revoked, err := provider.tokenService.storage.IsTokenRevoked(ctx, stored.JTI)
	if err != nil {
		t.Fatalf("IsTokenRevoked() error = %v", err)
	}
	if !revoked {
		t.Fatalf("stored refresh token must be revoked by value")
	}
}

// An expired token is not revocable by jti: it is already rejected by every
// verifier, and revocation acts only on claims the verified parse returns,
// which refuses an expired token. Revoking it reports the token unknown.
func TestRevokeToken_ExpiredSignedTokenNotRevocable(t *testing.T) {
	provider, _, _, _, _ := createTestOIDCProvider(t)
	ctx := context.Background()

	token, jti := mintIntrospectToken(t, provider, "tenant-a", "user-exp", 0, -time.Minute)

	if err := provider.tokenService.RevokeToken(ctx, token); err == nil {
		t.Fatalf("RevokeToken() of an expired token must report the token unknown")
	}
	revoked, err := provider.tokenService.storage.IsTokenRevoked(withTokenTenant(ctx, "tenant-a"), jti)
	if err != nil {
		t.Fatalf("IsTokenRevoked() error = %v", err)
	}
	if revoked {
		t.Fatalf("expired token must not be revoked by jti")
	}
}
