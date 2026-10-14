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

//go:build integration

// Package helpers provides shared utilities for integration tests.
package helpers

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jeremyhahn/go-quicbaas/test/integration/certfixture"
)

// tenantAdminAuthCache memoises (baseURL, tenantID) → bearer so each test
// tenant only pays the enrollment + login round-trip once per test run.
var tenantAdminAuthCache = struct {
	sync.Mutex
	tokens map[string]pivCachedToken
}{tokens: make(map[string]pivCachedToken)}

// AuthTenantAdmin obtains (and caches) a bearer token for a tenant-admin
// identity scoped to tenantID. It is the integration-harness counterpart of
// AuthPIV for tenant-scoped identities.
//
// Mechanism:
//  1. An ephemeral client certificate is ISSUED BY THE FIXTURE CA through the
//     product's own CA adapter (helpers.FixtureAuthority ->
//     pkg/server.NewCAService). The CA stamps the tenant-scoped SPIFFE URI SAN
//     spiffe://quicbaas.integration/tenant/<tenantID>/user/<cn> itself, which is
//     what the broker's PIV-login deriveLoginTenant resolves the tenant from.
//     Nothing here signs a certificate: before go-quicbaas#943 this helper read
//     the CA's private key off the mounted volume and called
//     x509.CreateCertificate, so the suite logged in with material the product
//     had never issued.
//  2. A new user is created under tenantID via the system-admin REST API
//     (POST /api/v1/tenants/{id}/users). The response body includes the
//     one-time invite_token (cleared at activation time).
//  3. The user is activated via POST /api/v1/iam/invites/{token}/activate/piv
//     with the PEM of the freshly minted cert in the request body. The
//     broker verifies the cert chain, computes the SHA-256 SPKI thumbprint,
//     and stores it on the user record so the PIV-login index can later
//     resolve the user.
//  4. A tenant_admin admin-assignment scoped to tenantID is created via
//     POST /api/v1/admin/assignments.
//  5. POST /api/v1/auth/login/piv is called with the ephemeral cert as the
//     mTLS client certificate. The broker reads the SPIFFE SAN to derive the
//     tenant, looks up the user by cert thumbprint, and issues a
//     tenant-scoped bearer.
//
// AuthTenantAdmin calls t.Fatal on any unrecoverable error so callers never
// receive an empty token. For the context-aware form with explicit error
// returns use AuthTenantAdminContext.
func AuthTenantAdmin(t *testing.T, baseURL, tenantID string) string {
	t.Helper()
	adminBearer := AuthPIV(t, baseURL)
	bearer, err := authTenantAdmin(context.Background(), baseURL, tenantID, adminBearer)
	if err != nil {
		t.Fatalf("AuthTenantAdmin(%q): %v", tenantID, err)
	}
	return bearer
}

// AuthTenantAdminContext is the context-aware variant of AuthTenantAdmin.
// adminBearer must be a valid system-admin bearer (e.g. from AuthPIVContext).
// Empty baseURL falls back to the standard integration node URL.
func AuthTenantAdminContext(ctx context.Context, baseURL, tenantID, adminBearer string) (string, error) {
	return authTenantAdmin(ctx, baseURL, tenantID, adminBearer)
}

func authTenantAdmin(ctx context.Context, baseURL, tenantID, adminBearer string) (string, error) {
	baseURL = strings.TrimRight(baseURL, "/")
	cacheKey := baseURL + "|tenant-admin|" + tenantID

	tenantAdminAuthCache.Lock()
	if cached, ok := tenantAdminAuthCache.tokens[cacheKey]; ok && time.Now().Before(cached.ExpiresAt) {
		tenantAdminAuthCache.Unlock()
		return cached.Bearer, nil
	}
	tenantAdminAuthCache.Unlock()

	// Steps 1-3: issue a tenant-scoped cert and enroll a PIV user bound to it.
	userID, certPEM, keyPEM, err := enrollTenantPIVUser(ctx, baseURL, tenantID, adminBearer)
	if err != nil {
		return "", fmt.Errorf("authTenantAdmin: %w", err)
	}

	// Step 4: grant tenant_admin role scoped to tenantID.
	if err := assignTenantAdmin(ctx, baseURL, userID, tenantID, adminBearer); err != nil {
		return "", fmt.Errorf("authTenantAdmin: assign tenant_admin for user %q in tenant %q: %w", userID, tenantID, err)
	}

	// Step 5: login with the ephemeral cert to obtain a tenant-scoped bearer.
	bearer, expiresIn, err := postLoginWithCert(ctx, baseURL, certPEM, keyPEM, pivCAFile())
	if err != nil {
		return "", fmt.Errorf("authTenantAdmin: PIV login for tenant %q: %w", tenantID, err)
	}

	tenantAdminAuthCache.Lock()
	tenantAdminAuthCache.tokens[cacheKey] = pivCachedToken{Bearer: bearer, ExpiresAt: bearerExpiry(expiresIn)}
	tenantAdminAuthCache.Unlock()

	return bearer, nil
}

// enrollTenantPIVUser has the fixture CA issue an ephemeral tenant-scoped
// client cert, creates a PIV user in tenantID (as the system admin) and
// activates it through the invite flow, binding the cert. The user holds no
// role: the caller grants whatever the principal is for.
func enrollTenantPIVUser(ctx context.Context, baseURL, tenantID, adminBearer string) (userID string, certPEM, keyPEM []byte, err error) {
	certPEM, keyPEM, err = IssueTenantClientCert(ctx, tenantID)
	if err != nil {
		return "", nil, nil, &TenantAdminCertError{TenantID: tenantID, Cause: err}
	}

	// The create-user response carries the one-time invite_token.
	email := fmt.Sprintf("tenant-admin-%s@integration.test", sanitizeTenantID(tenantID))
	userID, inviteToken, err := createTenantAdminUser(ctx, baseURL, tenantID, email, adminBearer)
	if err != nil {
		return "", nil, nil, fmt.Errorf("create user for %q: %w", tenantID, err)
	}

	if err := ActivateInvitePIV(ctx, baseURL, tenantID, inviteToken, certPEM); err != nil {
		return "", nil, nil, fmt.Errorf("activate user %q in tenant %q: %w", userID, tenantID, err)
	}
	return userID, certPEM, keyPEM, nil
}

// TenantAdminCertError reports a failure obtaining the tenant-admin client
// certificate for a tenant.
type TenantAdminCertError struct {
	// TenantID is the tenant the credential was for.
	TenantID string

	// Cause is the underlying failure.
	Cause error
}

func (e *TenantAdminCertError) Error() string {
	return "authTenantAdmin: tenant-admin certificate for " + e.TenantID + ": " + e.Cause.Error()
}

func (e *TenantAdminCertError) Unwrap() error { return e.Cause }

// IssueTenantClientCert asks the fixture CA for a tenant-admin client
// credential scoped to tenantID, and returns the certificate and private key as
// PEM.
//
// The certificate is issued under the go-xkms "client" profile, so it carries
// ClientAuth extended key usage and — because the profile maps to the SPIFFE
// "user" kind (pkg/service.SPIFFEKindForProfile) — a tenant-scoped SPIFFE URI
// SAN under the fixture trust domain. That SAN is what PIV login derives the
// tenant from, so a credential minted any other way cannot log in.
//
// The tenant segment is the sanitized tenant id, matching the email local part
// built for the same identity, so the SAN and the user record agree.
func IssueTenantClientCert(ctx context.Context, tenantID string) (certPEM, keyPEM []byte, err error) {
	// Verify the fixture trust anchor is loadable before continuing, so a
	// missing container mount fails here rather than at TLS handshake time.
	if _, err := IntegrationCertPool(); err != nil {
		return nil, nil, err
	}

	authority, err := FixtureAuthority(ctx)
	if err != nil {
		return nil, nil, err
	}

	sanitized := sanitizeTenantID(tenantID)
	issued, err := authority.IssueClient(ctx, certfixture.ClientFixture{
		CommonName:   "tenant-admin@" + tenantID,
		Email:        fmt.Sprintf("tenant-admin-%s@integration.test", sanitized),
		TenantID:     sanitized,
		ValidityDays: 1,
	})
	if err != nil {
		return nil, nil, err
	}
	return issued.CertificatePEM, issued.PrivateKeyPEM, nil
}

// createTenantAdminUser creates a pending user under tenantID using the
// system-admin bearer and returns the user ID and the one-time invite token.
// The user is provisioned with auth_methods=["piv"] so the PIV activation
// path is expected.
func createTenantAdminUser(ctx context.Context, baseURL, tenantID, email, adminBearer string) (userID, inviteToken string, err error) {
	reqBody := map[string]interface{}{
		"email":        email,
		"username":     "tenant-admin-" + sanitizeTenantID(tenantID),
		"name":         "Tenant Admin (" + tenantID + ")",
		"enabled":      true,
		"auth_methods": []string{"piv"},
	}

	body, err := postJSON(ctx, baseURL+"/api/v1/tenants/"+tenantID+"/users", adminBearer, reqBody)
	if err != nil {
		return "", "", err
	}

	var resp struct {
		ID          string `json:"id"`
		InviteToken string `json:"invite_token"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return "", "", fmt.Errorf("decode create-user response: %w; raw=%s", err, body)
	}
	if resp.ID == "" {
		return "", "", fmt.Errorf("create-user response missing id; raw=%s", body)
	}
	if resp.InviteToken == "" {
		return "", "", fmt.Errorf("create-user response missing invite_token; raw=%s", body)
	}

	return resp.ID, resp.InviteToken, nil
}

// ActivateInvitePIV calls tenantID's PIV invite-activation endpoint to bind
// the given cert PEM to the pending user identified by inviteToken. After
// this call the user's PIV-attested cert binding is set and the user
// transitions to Active. The endpoint is public (no auth bearer required).
// A refused activation returns an *AuthBadStatusError carrying the status.
func ActivateInvitePIV(ctx context.Context, baseURL, tenantID, inviteToken string, certPEM []byte) error {
	reqBody := map[string]interface{}{
		"cert_pem": string(certPEM),
	}
	endpoint := strings.TrimRight(baseURL, "/") + "/api/v1/iam/" + url.PathEscape(tenantID) +
		"/invites/" + url.PathEscape(inviteToken) + "/activate/piv"
	_, err := postJSON(ctx, endpoint, "", reqBody)
	return err
}

// LoginWithCert performs the mTLS PIV login with an in-memory client cert
// and key, trusting the PIV CA the suite's other logins trust, and returns
// the issued bearer token.
func LoginWithCert(ctx context.Context, baseURL string, certPEM, keyPEM []byte) (string, error) {
	bearer, _, err := postLoginWithCert(ctx, strings.TrimRight(baseURL, "/"), certPEM, keyPEM, pivCAFile())
	return bearer, err
}

// assignTenantAdmin creates a tenant_admin admin-assignment scoped to the
// given tenantID for userID, using the system-admin bearer.
func assignTenantAdmin(ctx context.Context, baseURL, userID, tenantID, adminBearer string) error {
	reqBody := map[string]interface{}{
		"user_id":    userID,
		"admin_role": "tenant_admin",
		"tenant_ids": []string{tenantID},
		"granted_by": "integration-test",
	}
	_, err := postJSON(ctx, baseURL+"/api/v1/admin/assignments", adminBearer, reqBody)
	return err
}

// postLoginWithCert performs the mTLS PIV login flow against baseURL and
// returns the access token and expires_in value. The cert/key are PEM-encoded
// bytes (not file paths) — generated in-process by mintTenantClientCert.
// caPath is the trusted CA file path on disk (same as for AuthPIV).
func postLoginWithCert(ctx context.Context, baseURL string, certPEM, keyPEM []byte, caPath string) (bearer string, expiresIn int, err error) {
	tlsCert, err := tls.X509KeyPair(certPEM, keyPEM)
	if err != nil {
		return "", 0, &AuthError{Op: "load client cert pair", URL: "<in-memory>", Cause: err}
	}

	roots, err := loadCAPool(caPath)
	if err != nil {
		return "", 0, &AuthError{Op: "load CA pool", URL: caPath, Cause: err}
	}

	tlsCfg := &tls.Config{
		Certificates: []tls.Certificate{tlsCert},
		RootCAs:      roots,
		MinVersion:   tls.VersionTLS13,
	}

	client := &http.Client{
		Timeout: 30 * time.Second,
		Transport: &http.Transport{
			TLSClientConfig:     tlsCfg,
			TLSHandshakeTimeout: 10 * time.Second,
		},
	}

	endpoint := baseURL + "/api/v1/auth/login/piv"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, nil)
	if err != nil {
		return "", 0, &AuthError{Op: "build request", URL: endpoint, Cause: err}
	}

	resp, err := client.Do(req)
	if err != nil {
		return "", 0, &AuthError{Op: "POST", URL: endpoint, Cause: err}
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", 0, &AuthError{Op: "read response", URL: endpoint, Cause: err}
	}

	if resp.StatusCode != http.StatusOK {
		return "", 0, &AuthBadStatusError{URL: endpoint, Status: resp.StatusCode, Body: string(respBody)}
	}

	var out struct {
		AccessToken string `json:"access_token"`
		ExpiresIn   int    `json:"expires_in"`
	}
	if err := json.Unmarshal(respBody, &out); err != nil {
		return "", 0, &AuthError{Op: "decode response", URL: endpoint, Cause: err}
	}
	if out.AccessToken == "" {
		return "", 0, &AuthError{Op: "missing access_token", URL: endpoint}
	}

	return out.AccessToken, out.ExpiresIn, nil
}

// postJSON posts a JSON body to the given URL with an optional Bearer token.
// It expects a 2xx status response and returns the raw response body.
func postJSON(ctx context.Context, rawURL, bearer string, body interface{}) ([]byte, error) {
	data, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("postJSON: marshal: %w", err)
	}

	client := NewHTTPClient(30 * time.Second)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, rawURL, bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("postJSON: build request %s: %w", rawURL, err)
	}
	req.Header.Set("Content-Type", "application/json")
	if bearer != "" {
		req.Header.Set("Authorization", "Bearer "+bearer)
	}

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("postJSON: POST %s: %w", rawURL, err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("postJSON: read response %s: %w", rawURL, err)
	}

	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return nil, &AuthBadStatusError{URL: rawURL, Status: resp.StatusCode, Body: string(respBody)}
	}

	return respBody, nil
}

// sanitizeTenantID strips characters that are not safe for use in email
// addresses or URL path segments (leaving only alphanumeric and hyphens).
func sanitizeTenantID(tenantID string) string {
	var b strings.Builder
	for _, r := range tenantID {
		switch {
		case r >= 'a' && r <= 'z':
			b.WriteRune(r)
		case r >= 'A' && r <= 'Z':
			b.WriteRune(r)
		case r >= '0' && r <= '9':
			b.WriteRune(r)
		case r == '-':
			b.WriteRune(r)
		default:
			b.WriteRune('-')
		}
	}
	return b.String()
}
