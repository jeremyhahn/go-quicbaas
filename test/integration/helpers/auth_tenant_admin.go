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
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"testing"
	"time"
)

// tenantAdminAuthCache memoises (baseURL, tenantID) → bearer so each test
// tenant only pays the enrollment + login round-trip once per test run.
var tenantAdminAuthCache = struct {
	sync.Mutex
	tokens map[string]pivCachedToken
}{tokens: make(map[string]pivCachedToken)}

// integrationCAKeyPath resolves the integration CA private key on disk.
// The key is required at runtime to sign the per-tenant client certificate.
// Override via QUICBAAS_TEST_CA_KEY if the mount point differs.
func integrationCAKeyPath() string {
	if v := os.Getenv("QUICBAAS_TEST_CA_KEY"); v != "" {
		return v
	}
	return "/etc/quicbaas/certs/ca.key"
}

// AuthTenantAdmin obtains (and caches) a bearer token for a tenant-admin
// identity scoped to tenantID. It is the integration-harness counterpart of
// AuthPIV for tenant-scoped identities.
//
// Mechanism:
//  1. An ephemeral ECDSA P-256 client certificate is minted in-process,
//     signed by the integration CA (whose key is at
//     /etc/quicbaas/certs/ca.key or QUICBAAS_TEST_CA_KEY). The certificate
//     carries a tenant-scoped SPIFFE URI SAN
//     spiffe://quicbaas.integration/tenant/<tenantID>/user/tenant-admin so
//     the broker's PIV-login deriveLoginTenant resolves the correct tenant.
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

	// Step 1: generate ephemeral tenant-scoped client cert signed by the
	// integration CA.
	certDER, keyDER, err := mintTenantClientCert(tenantID)
	if err != nil {
		return "", fmt.Errorf("authTenantAdmin: mint cert for %q: %w", tenantID, err)
	}

	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certDER})
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER})

	// Step 2: create user under the tenant (as system admin). The response
	// contains the one-time invite_token.
	email := fmt.Sprintf("tenant-admin-%s@integration.test", sanitizeTenantID(tenantID))
	userID, inviteToken, err := createTenantAdminUser(ctx, baseURL, tenantID, email, adminBearer)
	if err != nil {
		return "", fmt.Errorf("authTenantAdmin: create user for %q: %w", tenantID, err)
	}

	// Step 3: activate the user via the PIV invite flow, binding the cert.
	if err := activateUserWithCert(ctx, baseURL, inviteToken, certPEM); err != nil {
		return "", fmt.Errorf("authTenantAdmin: activate user %q in tenant %q: %w", userID, tenantID, err)
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

	expiry := time.Now().Add(15 * time.Minute)
	if expiresIn > 60 {
		expiry = time.Now().Add(time.Duration(expiresIn-60) * time.Second)
	}
	tenantAdminAuthCache.Lock()
	tenantAdminAuthCache.tokens[cacheKey] = pivCachedToken{Bearer: bearer, ExpiresAt: expiry}
	tenantAdminAuthCache.Unlock()

	return bearer, nil
}

// mintTenantClientCert generates an ephemeral ECDSA P-256 client certificate
// for a tenant-admin identity, signed by the integration CA. The certificate
// embeds a tenant-scoped SPIFFE URI SAN of the form:
//
//	spiffe://quicbaas.integration/tenant/<tenantID>/user/tenant-admin
//
// Returns the DER-encoded cert and the DER-encoded private key.
func mintTenantClientCert(tenantID string) (certDER, keyDER []byte, err error) {
	// Verify the integration CA is loadable before continuing (fail fast if the
	// container mount is missing).
	if _, err := IntegrationCertPool(); err != nil {
		return nil, nil, fmt.Errorf("load CA pool: %w", err)
	}

	// Load the CA private key so we can sign the tenant client cert.
	caKeyPath := integrationCAKeyPath()
	caKeyBytes, err := os.ReadFile(caKeyPath) //nolint:gosec // path from test config
	if err != nil {
		return nil, nil, fmt.Errorf("read CA key %s: %w", caKeyPath, err)
	}

	caKeyBlock, _ := pem.Decode(caKeyBytes)
	if caKeyBlock == nil {
		return nil, nil, fmt.Errorf("decode CA key PEM: no PEM block in %s", caKeyPath)
	}
	caPrivKey, err := x509.ParseECPrivateKey(caKeyBlock.Bytes)
	if err != nil {
		return nil, nil, fmt.Errorf("parse CA private key: %w", err)
	}

	// Load the CA certificate to use as the issuer template.
	caCertPEM, err := os.ReadFile(pivCAFile()) //nolint:gosec // path from test config
	if err != nil {
		return nil, nil, fmt.Errorf("read CA cert %s: %w", pivCAFile(), err)
	}
	caCertBlock, _ := pem.Decode(caCertPEM)
	if caCertBlock == nil {
		return nil, nil, fmt.Errorf("decode CA cert PEM: no PEM block")
	}
	caCert, err := x509.ParseCertificate(caCertBlock.Bytes)
	if err != nil {
		return nil, nil, fmt.Errorf("parse CA cert: %w", err)
	}

	// Generate an ephemeral client key.
	clientKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, nil, fmt.Errorf("generate client key: %w", err)
	}

	serialLimit := new(big.Int).Lsh(big.NewInt(1), 128)
	serial, err := rand.Int(rand.Reader, serialLimit)
	if err != nil {
		return nil, nil, fmt.Errorf("generate serial: %w", err)
	}

	now := time.Now()
	spiffeURI := &url.URL{
		Scheme: "spiffe",
		Host:   "quicbaas.integration",
		Path:   "/tenant/" + sanitizeTenantID(tenantID) + "/user/tenant-admin",
	}
	template := &x509.Certificate{
		SerialNumber: serial,
		Subject: pkix.Name{
			Organization: []string{"QuicBaaS Integration Tests"},
			CommonName:   "tenant-admin@" + tenantID,
		},
		NotBefore:   now,
		NotAfter:    now.Add(24 * time.Hour),
		KeyUsage:    x509.KeyUsageDigitalSignature,
		ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth},
		EmailAddresses: []string{
			fmt.Sprintf("tenant-admin-%s@integration.test", sanitizeTenantID(tenantID)),
		},
		URIs: []*url.URL{spiffeURI},
	}

	certDER, err = x509.CreateCertificate(rand.Reader, template, caCert, &clientKey.PublicKey, caPrivKey)
	if err != nil {
		return nil, nil, fmt.Errorf("create client cert: %w", err)
	}

	keyDER, err = x509.MarshalECPrivateKey(clientKey)
	if err != nil {
		return nil, nil, fmt.Errorf("marshal client key: %w", err)
	}

	return certDER, keyDER, nil
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

// activateUserWithCert calls the PIV invite-activation endpoint to bind the
// given cert PEM to the pending user identified by inviteToken. After this
// call the user's PIVCertThumbprint is set and the user transitions to Active.
// The endpoint is public (no auth bearer required).
func activateUserWithCert(ctx context.Context, baseURL, inviteToken string, certPEM []byte) error {
	reqBody := map[string]interface{}{
		"cert_pem": string(certPEM),
	}
	_, err := postJSON(ctx, baseURL+"/api/v1/iam/invites/"+inviteToken+"/activate/piv", "", reqBody)
	return err
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
