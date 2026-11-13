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

// tenantSetupTimeout bounds provisioning a tenant and deleting it, so a hung
// server fails the test instead of stalling it.
const tenantSetupTimeout = 30 * time.Second

// provisionedTenants memoises (baseURL, tenantID) → the admin session of the
// tenant provisioned for it, so a second call for the same tenant reuses its
// admin instead of provisioning the tenant twice. An entry lives until the
// test that provisioned the tenant ends and deletes it.
var provisionedTenants = struct {
	sync.Mutex
	sessions map[string]*TenantAdminSession
}{sessions: make(map[string]*TenantAdminSession)}

// AuthTenantAdmin returns a bearer for the admin of tenantID, acting as a
// production caller does: the system admin only provisions the tenant
// (ProvisionTenant, control plane), and every tenant-scoped call is then made
// by the tenant's own admin, whose bearer is bound to the tenant. The system
// bearer is never used inside the tenant: the users route refuses it, because
// its tenant is "system".
//
// The first call for a tenant provisions it (see TenantAdmin), so callers do
// not create the tenant first. A later call returns the admin's bearer,
// renewed with its certificate when the last one nears expiry, so calling
// AuthTenantAdmin before each step keeps a long test authenticated.
//
// AuthTenantAdmin calls t.Fatal on any unrecoverable error so callers never
// receive an empty token.
func AuthTenantAdmin(t *testing.T, baseURL, tenantID string) string {
	t.Helper()
	bearer, err := TenantAdmin(t, baseURL, tenantID).Bearer(context.Background())
	if err != nil {
		t.Fatalf("AuthTenantAdmin(%q): %v", tenantID, err)
	}
	return bearer
}

// TenantAdmin returns the admin session of tenantID. The first call for a
// tenant has the system admin provision it and registers a t.Cleanup that
// deletes it when t ends, so no fixture tenant outlives its test.
//
// TenantAdmin calls t.Fatal if the tenant cannot be provisioned.
func TenantAdmin(t *testing.T, baseURL, tenantID string) *TenantAdminSession {
	t.Helper()
	baseURL = strings.TrimRight(baseURL, "/")
	cacheKey := baseURL + "|" + tenantID

	provisionedTenants.Lock()
	defer provisionedTenants.Unlock()

	if session, ok := provisionedTenants.sessions[cacheKey]; ok {
		return session
	}

	tenant, err := provisionTestTenant(t, baseURL, tenantID)
	if err != nil {
		t.Fatalf("TenantAdmin(%q): %v", tenantID, err)
	}
	session := NewTenantAdminSession(baseURL, tenant)
	provisionedTenants.sessions[cacheKey] = session
	t.Cleanup(func() {
		provisionedTenants.Lock()
		delete(provisionedTenants.sessions, cacheKey)
		provisionedTenants.Unlock()
	})
	return session
}

// ProvisionTestTenant has the system admin provision tenantID and registers a
// t.Cleanup that deletes it, again as the system admin, when t ends.
//
// ProvisionTestTenant calls t.Fatal if the tenant cannot be provisioned.
func ProvisionTestTenant(t *testing.T, baseURL, tenantID string) *ProvisionedTenant {
	t.Helper()
	tenant, err := provisionTestTenant(t, baseURL, tenantID)
	if err != nil {
		t.Fatalf("ProvisionTestTenant(%q): %v", tenantID, err)
	}
	return tenant
}

func provisionTestTenant(t *testing.T, baseURL, tenantID string) (*ProvisionedTenant, error) {
	ctx, cancel := context.WithTimeout(context.Background(), tenantSetupTimeout)
	defer cancel()

	systemBearer, err := AuthSystemAdminContext(ctx, baseURL)
	if err != nil {
		return nil, &TenantProvisionError{Step: "system admin login", TenantID: tenantID, Cause: err}
	}
	tenant, err := ProvisionTenant(ctx, baseURL, tenantID, systemBearer)
	if err != nil {
		return nil, err
	}
	t.Cleanup(func() { deleteTestTenant(t, baseURL, tenantID) })
	return tenant, nil
}

// deleteTestTenant deletes tenantID as the system admin, logging in afresh
// because the test may have outlived any earlier system bearer. A failure is
// a test error: a tenant left behind accumulates across a soak.
func deleteTestTenant(t *testing.T, baseURL, tenantID string) {
	ctx, cancel := context.WithTimeout(context.Background(), tenantSetupTimeout)
	defer cancel()

	if err := deleteTenantAsSystemAdmin(ctx, baseURL, tenantID); err != nil {
		t.Errorf("delete fixture tenant %q: %v", tenantID, err)
	}
}

// deleteTenantAsSystemAdmin logs in as the system admin and deletes
// tenantID, the one way a fixture tenant is removed.
func deleteTenantAsSystemAdmin(ctx context.Context, baseURL, tenantID string) error {
	systemBearer, err := AuthSystemAdminContext(ctx, baseURL)
	if err != nil {
		return err
	}
	return DeleteTenant(ctx, baseURL, tenantID, systemBearer)
}

// enrollTenantPIVUser has the fixture CA issue an ephemeral tenant-scoped
// client cert, creates a PIV user in tenantID and activates it through the
// invite flow, binding the cert. adminBearer must be bound to tenantID (the
// tenant's admin, see ProvisionTenant): the users route refuses a bearer of
// any other tenant, the system tenant included. The user holds no
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
		return "", nil, nil, &TenantProvisionError{Step: "create user", TenantID: tenantID, Cause: err}
	}

	if err := ActivateInvitePIV(ctx, baseURL, tenantID, inviteToken, certPEM); err != nil {
		return "", nil, nil, &TenantProvisionError{Step: "activate user " + userID, TenantID: tenantID, Cause: err}
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

// MissingResponseFieldError reports a 2xx response that lacks a field the
// caller needs.
type MissingResponseFieldError struct {
	// URL is the endpoint that answered.
	URL string

	// Field is the JSON field that was missing or empty.
	Field string

	// Body is the raw response.
	Body string
}

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
func (e *MissingResponseFieldError) Error() string {
	return e.URL + ": response missing " + e.Field + ": " + e.Body
}

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
// tenant admin's bearer and returns the user ID and the one-time invite token.
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

	endpoint := baseURL + "/api/v1/tenants/" + tenantID + "/users"
	body, err := postJSON(ctx, endpoint, adminBearer, reqBody)
	if err != nil {
		return "", "", err
	}

	var resp struct {
		ID          string `json:"id"`
		InviteToken string `json:"invite_token"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return "", "", &AuthError{Op: "decode create-user response", URL: endpoint, Cause: err}
	}
	if resp.ID == "" {
		return "", "", &MissingResponseFieldError{URL: endpoint, Field: "id", Body: string(body)}
	}
	if resp.InviteToken == "" {
		return "", "", &MissingResponseFieldError{URL: endpoint, Field: "invite_token", Body: string(body)}
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
	bearer, _, err := LoginWithCertSession(ctx, baseURL, certPEM, keyPEM)
	return bearer, err
}

// LoginWithCertSession is LoginWithCert that also reports when the bearer
// expires, for callers that persist it as a CLI session (with the same
// one-minute safety margin the data-plane principal's cached bearer uses).
func LoginWithCertSession(ctx context.Context, baseURL string, certPEM, keyPEM []byte) (string, time.Time, error) {
	bearer, expiresIn, err := postLoginWithCert(ctx, strings.TrimRight(baseURL, "/"), certPEM, keyPEM, pivCAFile())
	if err != nil {
		return "", time.Time{}, err
	}
	return bearer, bearerExpiry(expiresIn), nil
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

	// A tenant-scoped cert login is served by quicbaas's OIDC-session
	// PIVLogin at /api/v1/iam/login/piv (pkg/api/rest/routes_iam.go). On a
	// composed node /api/v1/auth/login/piv belongs to xkms's module, which
	// does not resolve a tenant-scoped user and rejects it with 401.
	endpoint := baseURL + "/api/v1/iam/login/piv"
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
		return nil, &AuthError{Op: "marshal request", URL: rawURL, Cause: err}
	}

	client := NewHTTPClient(30 * time.Second)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, rawURL, bytes.NewReader(data))
	if err != nil {
		return nil, &AuthError{Op: "build request", URL: rawURL, Cause: err}
	}
	req.Header.Set("Content-Type", "application/json")
	if bearer != "" {
		req.Header.Set("Authorization", "Bearer "+bearer)
	}

	resp, err := client.Do(req)
	if err != nil {
		return nil, &AuthError{Op: "POST", URL: rawURL, Cause: err}
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, &AuthError{Op: "read response", URL: rawURL, Cause: err}
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
