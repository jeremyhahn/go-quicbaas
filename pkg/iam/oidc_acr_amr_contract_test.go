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
	"encoding/base64"
	"encoding/json"
	"slices"
	"strings"
	"testing"
	"time"
)

// The acr/amr contract: both are ID-token claims, set only by the
// hardware-attested login (TokenService.IssueHardwareTokens). Discovery
// advertises both; every other ID token omits them. The one access token
// that carries a method is the mTLS service-account token
// (TokenService.IssueServiceAccountTokens, /oauth2/token-mtls): it states
// the ServiceAccountAMR value and no acr, since that grant issues no ID
// token. Every other access token, renewed ones included, omits both.

// acrAmrContractPayload decodes a JWT's payload segment without verifying it:
// the contract is about which claims are present, not who signed them.
func acrAmrContractPayload(t *testing.T, token string) map[string]any {
	t.Helper()
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		t.Fatalf("token has %d segments, want 3", len(parts))
	}
	raw, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		t.Fatalf("decode payload: %v", err)
	}
	claims := map[string]any{}
	if err := json.Unmarshal(raw, &claims); err != nil {
		t.Fatalf("unmarshal payload: %v", err)
	}
	return claims
}

func assertNoAcrAmr(t *testing.T, what string, claims map[string]any) {
	t.Helper()
	for _, name := range []string{"acr", "amr"} {
		if got, ok := claims[name]; ok {
			t.Errorf("%s carries %s = %v, want no such claim", what, name, got)
		}
	}
}

func TestAcrAmrContract_DiscoveryListsAcrAndAmr(t *testing.T) {
	t.Parallel()

	provider, _, _, _, _ := createTestOIDCProvider(t)

	claims, ok := provider.GetConfiguration()["claims_supported"].([]string)
	if !ok {
		t.Fatalf("claims_supported is %T, want []string", provider.GetConfiguration()["claims_supported"])
	}
	for _, name := range []string{"acr", "amr"} {
		if !slices.Contains(claims, name) {
			t.Errorf("claims_supported = %v, want it to list %q", claims, name)
		}
	}
}

func TestAcrAmrContract_AuthCodeIDTokenOmitsAcrAmr(t *testing.T) {
	t.Parallel()

	provider, _, appStorage, tokenStorage, memStorage := createTestOIDCProvider(t)
	ctx := context.Background()

	if err := appStorage.CreateApp(ctx, &Application{
		ClientID:      "acr-amr-client",
		ClientSecret:  currentSecretHash(t, "acr-amr-secret"),
		TenantID:      "tenant-1",
		RedirectURIs:  []string{"https://example.com/callback"},
		ResponseTypes: []string{ResponseTypeCode},
		GrantTypes:    []string{GrantTypeAuthorizationCode},
	}); err != nil {
		t.Fatalf("CreateApp: %v", err)
	}
	addUserToMemStorage(ctx, memStorage, &User{
		ID:        123,
		TenantID:  "tenant-1",
		Email:     "acr-amr@example.com",
		Lifecycle: LifecycleActive,
		Name:      "Acr Amr",
		Roles:     []string{"user"},
	})
	if err := tokenStorage.StoreAuthCode(ctx, &AuthCodeData{
		Code:        "acr-amr-code",
		UserID:      123,
		TenantID:    "tenant-1",
		ClientID:    "acr-amr-client",
		RedirectURI: "https://example.com/callback",
		Scope:       []string{"openid", "profile", "email"},
		ExpiresAt:   time.Now().Add(10 * time.Minute),
		CreatedAt:   time.Now(),
	}); err != nil {
		t.Fatalf("StoreAuthCode: %v", err)
	}

	resp, err := provider.Token(ctx, &TokenRequest{
		GrantType:    GrantTypeAuthorizationCode,
		Code:         "acr-amr-code",
		RedirectURI:  "https://example.com/callback",
		ClientID:     "acr-amr-client",
		ClientSecret: "acr-amr-secret",
	})
	if err != nil {
		t.Fatalf("Token: %v", err)
	}
	if resp.IDToken == "" {
		t.Fatal("authorization-code exchange returned no ID token (openid scope present)")
	}

	assertNoAcrAmr(t, "authorization-code ID token", acrAmrContractPayload(t, resp.IDToken))
	assertNoAcrAmr(t, "authorization-code access token", acrAmrContractPayload(t, resp.AccessToken))
}

func TestAcrAmrContract_RenewedAccessTokenOmitsAcrAmr(t *testing.T) {
	provider, storage := newSessionRefreshProvider(t, 15*time.Minute, 7*24*time.Hour)
	ctx := context.Background()

	// The refresh gate resolves the session's principal by its immutable id,
	// so the account has to exist for the renewal to be granted at all.
	user := seedCeremonyUser(t, storage, "acme", "hw-admin", []string{"admin"})

	set, err := provider.tokenService.IssueHardwareTokens(ctx, HardwareTokenOptions{
		User:        user,
		AMR:         []string{"webauthn", "hwk"},
		ACR:         ACRHardwareAttested,
		Attestation: testHardwareAttestation("cb69481e8ff7403993ec0a2729a154a8"),
	})
	if err != nil {
		t.Fatalf("proven hardware was refused: %v", err)
	}

	// The login's ID token is where the contract puts both claims.
	id := acrAmrContractPayload(t, set.IDToken)
	if got := id["acr"]; got != ACRHardwareAttested {
		t.Errorf("hardware-attested ID token acr = %v, want %q", got, ACRHardwareAttested)
	}
	if _, ok := id["amr"]; !ok {
		t.Error("hardware-attested ID token carries no amr claim")
	}

	tokens, err := provider.RefreshSession(ctx, SessionRefreshRequest{RefreshToken: set.RefreshToken})
	if err != nil {
		t.Fatalf("RefreshSession: %v", err)
	}
	assertNoAcrAmr(t, "renewed access token", acrAmrContractPayload(t, tokens.AccessToken))
}

// An unattested PIV login, admitted through iam.piv_login.allow_unattested,
// is issued through IssueHardwareTokens as well: its ID token names what was
// proven -- amr ["mtls"] -- and carries no acr, and neither its access token
// nor a renewed one carries either claim.
func TestAcrAmrContract_UnattestedPIVLoginReportsMtlsAmr(t *testing.T) {
	provider, storage := newSessionRefreshProvider(t, 15*time.Minute, 7*24*time.Hour)
	ctx := context.Background()

	user := seedCeremonyUser(t, storage, "acme", "piv-admin", []string{"admin"})

	// The values the PIV login handler passes on its unattested path.
	set, err := provider.tokenService.IssueHardwareTokens(ctx, HardwareTokenOptions{
		User:        user,
		AMR:         []string{"mtls"},
		ACR:         "",
		Attestation: UnattestedPIVCredential(),
	})
	if err != nil {
		t.Fatalf("unattested PIV login was refused: %v", err)
	}

	id := acrAmrContractPayload(t, set.IDToken)
	if _, ok := id["acr"]; ok {
		t.Errorf("unattested ID token carries acr %v, want none", id["acr"])
	}
	amr, ok := id["amr"].([]any)
	if !ok || len(amr) != 1 || amr[0] != "mtls" {
		t.Errorf("unattested ID token amr = %v, want [mtls]", id["amr"])
	}
	assertNoAcrAmr(t, "unattested login access token", acrAmrContractPayload(t, set.AccessToken))

	tokens, err := provider.RefreshSession(ctx, SessionRefreshRequest{RefreshToken: set.RefreshToken})
	if err != nil {
		t.Fatalf("RefreshSession: %v", err)
	}
	assertNoAcrAmr(t, "renewed unattested access token", acrAmrContractPayload(t, tokens.AccessToken))
}

// TestAcrAmrContract_ServiceAccountAccessTokenCarriesAttestAMR pins the one
// access-token exception: the mTLS client_credentials grant issues no ID
// token, so its access token states how the certificate was attested.
func TestAcrAmrContract_ServiceAccountAccessTokenCarriesAttestAMR(t *testing.T) {
	t.Parallel()

	provider, _, appStorage, _, _ := createTestOIDCProvider(t)
	ctx := context.Background()

	if err := appStorage.CreateApp(ctx, &Application{
		ClientID:   "svc-test",
		TenantID:   "tenant-1",
		GrantTypes: []string{GrantTypeClientCredentials},
		Scopes:     []string{"mcp:tools"},
	}); err != nil {
		t.Fatalf("CreateApp: %v", err)
	}

	tests := []struct {
		attest string
		want   []string
	}{
		{attest: AttestMethodManual, want: []string{"mtls"}},
		{attest: AttestMethodXKey, want: []string{"mtls"}},
		{attest: AttestMethodTPM, want: []string{"mtls", "hwk"}},
	}
	for _, tt := range tests {
		t.Run(tt.attest, func(t *testing.T) {
			resp, err := provider.TokenMTLS(ctx, &MTLSTokenRequest{
				GrantType:          GrantTypeClientCredentials,
				ClientID:           "svc-test",
				Scope:              "mcp:tools",
				CertThumbprintS256: "thumb-test",
				AttestMethod:       tt.attest,
			})
			if err != nil {
				t.Fatalf("TokenMTLS: %v", err)
			}
			claims := acrAmrContractPayload(t, resp.AccessToken)
			if got, ok := claims["acr"]; ok {
				t.Errorf("service-account access token carries acr = %v, want no such claim", got)
			}
			raw, ok := claims["amr"].([]any)
			if !ok {
				t.Fatalf("service-account access token amr = %v, want %v", claims["amr"], tt.want)
			}
			got := make([]string, 0, len(raw))
			for _, v := range raw {
				s, _ := v.(string)
				got = append(got, s)
			}
			if !slices.Equal(got, tt.want) {
				t.Errorf("service-account access token amr = %v, want %v", got, tt.want)
			}
		})
	}
}

// TestAcrAmrContract_SecretClientCredentialsAccessTokenOmitsAcrAmr: a
// secret-authenticated client_credentials token proves no method worth
// stating, so it carries neither claim.
func TestAcrAmrContract_SecretClientCredentialsAccessTokenOmitsAcrAmr(t *testing.T) {
	t.Parallel()

	provider, _, appStorage, _, _ := createTestOIDCProvider(t)
	ctx := context.Background()

	if err := appStorage.CreateApp(ctx, &Application{
		ClientID:     "svc-test",
		ClientSecret: currentSecretHash(t, "svc-secret"),
		TenantID:     "tenant-1",
		GrantTypes:   []string{GrantTypeClientCredentials},
		Scopes:       []string{"mcp:tools"},
	}); err != nil {
		t.Fatalf("CreateApp: %v", err)
	}

	resp, err := provider.Token(ctx, &TokenRequest{
		GrantType:    GrantTypeClientCredentials,
		ClientID:     "svc-test",
		ClientSecret: "svc-secret",
		Scope:        []string{"mcp:tools"},
	})
	if err != nil {
		t.Fatalf("Token: %v", err)
	}
	assertNoAcrAmr(t, "client_credentials access token", acrAmrContractPayload(t, resp.AccessToken))
}
