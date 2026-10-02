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
	"crypto/x509"
	"crypto/x509/pkix"
	"math/big"
	"net/url"
	"testing"
	"time"

	"github.com/jeremyhahn/go-quicbaas/pkg/tenantctx"
)

// mintAccessToken issues a signed access token through the provider's token
// service and returns the token string and the JTI it was minted with.
// mintIntrospectToken mints a token for introspection tests.
//
// userID names the principal: a non-empty one becomes the uid claim, and an
// empty one makes the token a SERVICE principal through ptyp. One or the other
// is required, because liveness resolves the principal from those two claims
// and nothing else — a token carrying neither predates the contract and is
// denied (#390). These fixtures used to carry neither and passed only because
// an unresolvable subject was inferred to be a service principal.
func mintIntrospectToken(t *testing.T, provider *OIDCProvider, tenant, subject, userID string, ttl time.Duration) (string, string) {
	t.Helper()
	now := time.Now()
	jti := "introspect-jti-" + subject + "-" + tenant
	principalType := ""
	if userID == "" {
		principalType = PrincipalTypeService
	}
	claims := &Claims{
		UserID:        userID,
		PrincipalType: principalType,
		Issuer:        provider.config.Issuer,
		Subject:       subject,
		Audience:      []string{"https://rs.example.com"},
		ExpiresAt:     NewUnixTime(now.Add(ttl)),
		IssuedAt:      NewUnixTime(now),
		NotBefore:     NewUnixTime(now),
		JTI:           jti,
		TenantID:      tenant,
		Roles:         []string{"admin"},
		Scope:         "openid profile agent:runtime:spawn",
		SCP:           []string{"openid", "profile", "agent:runtime:spawn"},
		ClientID:      "app_" + tenant + "_rs",
		Name:          "Test Subject",
	}
	token, err := provider.tokenService.GenerateAccessToken(context.Background(), claims)
	if err != nil {
		t.Fatalf("GenerateAccessToken() error = %v", err)
	}
	return token, jti
}

// TestOIDCIntrospect_ActiveToken verifies a valid token returns active=true
// with the full standard claim set (RFC 7662 §2.2).
func TestOIDCIntrospect_ActiveToken(t *testing.T) {
	provider, _, _, _, _ := createTestOIDCProvider(t)
	ctx := context.Background()

	token, jti := mintIntrospectToken(t, provider, "tenant-a", "user-1", "", time.Hour)

	resp, err := provider.Introspect(ctx, &IntrospectRequest{Token: token})
	if err != nil {
		t.Fatalf("Introspect() error = %v", err)
	}
	if !resp.Active {
		t.Fatalf("expected active=true, got false")
	}
	if resp.Subject != "user-1" {
		t.Errorf("sub = %q, want user-1", resp.Subject)
	}
	if resp.TenantID != "tenant-a" {
		t.Errorf("tenant_id = %q, want tenant-a", resp.TenantID)
	}
	if resp.JTI != jti {
		t.Errorf("jti = %q, want %q", resp.JTI, jti)
	}
	if len(resp.SCP) != 3 {
		t.Errorf("scp len = %d, want 3", len(resp.SCP))
	}
	if resp.Scope == "" {
		t.Errorf("scope must be populated")
	}
	if resp.Issuer != provider.config.Issuer {
		t.Errorf("iss = %q, want %q", resp.Issuer, provider.config.Issuer)
	}
	if resp.ClientID == "" {
		t.Errorf("client_id must be populated")
	}
	if resp.ExpiresAt == 0 || resp.IssuedAt == 0 {
		t.Errorf("exp/iat must be populated, got exp=%d iat=%d", resp.ExpiresAt, resp.IssuedAt)
	}
	if resp.TokenType != "Bearer" {
		t.Errorf("token_type = %q, want Bearer", resp.TokenType)
	}
}

// TestOIDCIntrospect_InvalidToken verifies a garbage/unsigned token returns
// active=false with no claims (fail-closed, RFC 7662).
func TestOIDCIntrospect_InvalidToken(t *testing.T) {
	provider, _, _, _, _ := createTestOIDCProvider(t)
	ctx := context.Background()

	resp, err := provider.Introspect(ctx, &IntrospectRequest{Token: "not.a.jwt"})
	if err != nil {
		t.Fatalf("Introspect() unexpected error = %v", err)
	}
	if resp.Active {
		t.Fatalf("invalid token must be inactive")
	}
	if resp.Subject != "" || resp.TenantID != "" || resp.JTI != "" {
		t.Errorf("inactive response must not leak claims: %+v", resp)
	}
}

// TestOIDCIntrospect_ExpiredToken verifies an expired token returns
// active=false.
func TestOIDCIntrospect_ExpiredToken(t *testing.T) {
	provider, _, _, _, _ := createTestOIDCProvider(t)
	ctx := context.Background()

	// Negative TTL -> already expired.
	token, _ := mintIntrospectToken(t, provider, "tenant-a", "user-exp", "", -time.Minute)

	resp, err := provider.Introspect(ctx, &IntrospectRequest{Token: token})
	if err != nil {
		t.Fatalf("Introspect() error = %v", err)
	}
	if resp.Active {
		t.Fatalf("expired token must be inactive")
	}
	if resp.Subject != "" {
		t.Errorf("inactive response must not leak claims")
	}
}

// TestOIDCIntrospect_RevokedToken verifies a revoked token returns
// active=false and that the revocation lookup is reached via the tenant claim.
func TestOIDCIntrospect_RevokedToken(t *testing.T) {
	provider, _, _, _, _ := createTestOIDCProvider(t)
	ctx := context.Background()

	token, _ := mintIntrospectToken(t, provider, "tenant-a", "user-rev", "", time.Hour)

	// Confirm active before revocation.
	resp, err := provider.Introspect(ctx, &IntrospectRequest{Token: token})
	if err != nil || !resp.Active {
		t.Fatalf("token should be active before revocation: resp=%+v err=%v", resp, err)
	}

	// Revoke and re-introspect.
	if err := provider.tokenService.RevokeToken(ctx, token); err != nil {
		t.Fatalf("RevokeToken() error = %v", err)
	}
	resp, err = provider.Introspect(ctx, &IntrospectRequest{Token: token})
	if err != nil {
		t.Fatalf("Introspect() error = %v", err)
	}
	if resp.Active {
		t.Fatalf("revoked token must be inactive")
	}
}

// TestOIDCIntrospect_TenantFromClaim verifies the tenant is derived from the
// token's own tenant_id claim (no global table): a context carrying a
// DIFFERENT tenant must not change the introspection result, and the response
// carries the token's tenant.
func TestOIDCIntrospect_TenantFromClaim(t *testing.T) {
	provider, _, _, _, _ := createTestOIDCProvider(t)

	token, _ := mintIntrospectToken(t, provider, "tenant-b", "user-2", "", time.Hour)

	// Context tenant deliberately differs from the token's tenant claim.
	ctx := tenantctx.WithTenant(context.Background(), "tenant-wrong")

	resp, err := provider.Introspect(ctx, &IntrospectRequest{Token: token})
	if err != nil {
		t.Fatalf("Introspect() error = %v", err)
	}
	if !resp.Active {
		t.Fatalf("token must be active regardless of context tenant")
	}
	if resp.TenantID != "tenant-b" {
		t.Errorf("tenant must come from the claim: got %q want tenant-b", resp.TenantID)
	}
}

// TestOIDCIntrospect_EmptyToken verifies a missing token is an invalid_request
// error (the caller must supply a token).
func TestOIDCIntrospect_EmptyToken(t *testing.T) {
	provider, _, _, _, _ := createTestOIDCProvider(t)

	_, err := provider.Introspect(context.Background(), &IntrospectRequest{Token: ""})
	if err == nil {
		t.Fatalf("expected error for empty token")
	}
	oauthErr, ok := err.(*OAuthError)
	if !ok {
		t.Fatalf("expected *OAuthError, got %T", err)
	}
	if oauthErr.ErrorCode != ErrOAuthInvalidRequest {
		t.Errorf("error code = %q, want %q", oauthErr.ErrorCode, ErrOAuthInvalidRequest)
	}
}

// TestOIDCAuthenticateClient verifies the introspection caller-authentication
// primitive accepts valid client credentials and rejects bad ones.
func TestOIDCAuthenticateClient(t *testing.T) {
	provider, _, appStorage, _, _ := createTestOIDCProvider(t)
	ctx := context.Background()

	hashed := currentSecretHash(t, "s3cret")
	appStorage.apps["rs-client"] = &Application{
		ClientID:     "rs-client",
		ClientSecret: hashed,
	}

	if err := provider.AuthenticateClient(ctx, "rs-client", "s3cret"); err != nil {
		t.Fatalf("AuthenticateClient() valid = %v", err)
	}
	if err := provider.AuthenticateClient(ctx, "rs-client", "wrong"); err == nil {
		t.Fatalf("AuthenticateClient() must reject a wrong secret")
	}
	if err := provider.AuthenticateClient(ctx, "", ""); err == nil {
		t.Fatalf("AuthenticateClient() must reject an empty client_id")
	}
}

// TestServiceIdentityFromCert verifies the service-identity extractor prefers a
// SPIFFE URI SAN and falls back to the Subject DN.
func TestServiceIdentityFromCert(t *testing.T) {
	if got := ServiceIdentityFromCert(nil); got != "" {
		t.Errorf("nil cert = %q, want empty", got)
	}

	spiffeURI, _ := url.Parse("spiffe://example.org/tenant/acme/svc/resource-server")
	certWithSPIFFE := &x509.Certificate{
		Subject:      pkix.Name{CommonName: "rs.example.com"},
		URIs:         []*url.URL{spiffeURI},
		SerialNumber: big.NewInt(1),
	}
	if got := ServiceIdentityFromCert(certWithSPIFFE); got != "spiffe://example.org/tenant/acme/svc/resource-server" {
		t.Errorf("spiffe identity = %q", got)
	}

	certNoSPIFFE := &x509.Certificate{
		Subject:      pkix.Name{CommonName: "rs.example.com"},
		SerialNumber: big.NewInt(2),
	}
	if got := ServiceIdentityFromCert(certNoSPIFFE); got != "CN=rs.example.com" {
		t.Errorf("dn identity = %q, want CN=rs.example.com", got)
	}
}
