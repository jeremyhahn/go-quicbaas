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

//go:build integration
// +build integration

package iam_test

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"encoding/pem"
	"errors"
	"io"
	"net/http"
	"net/url"
	"testing"
	"time"

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/test/integration/helpers"
)

// provisionResponse mirrors the fields of the provision response the tests
// assert on.
type provisionResponse struct {
	TenantID        string     `json:"tenant_id"`
	AdminUserID     string     `json:"admin_user_id"`
	AdminUsername   string     `json:"admin_username"`
	AdminRoleID     string     `json:"admin_role_id"`
	AdminPending    bool       `json:"admin_pending"`
	AdminCertPEM    string     `json:"admin_cert_pem"`
	InviteToken     string     `json:"invite_token"`
	InviteExpiresAt *time.Time `json:"invite_expires_at"`
}

// TestTenantProvision_CertMode provisions a tenant whose admin enrolls by
// CSR, then logs in over mTLS with the returned certificate and reads the
// tenant's IAM data as its admin.
func TestTenantProvision_CertMode(t *testing.T) {
	cluster := setupTestCluster(t)
	defer cluster.Close()
	systemBearer := helpers.AuthPIV(t, cluster.httpEndpoint)

	tenantID := generateUniqueTenantID()
	csrPEM, keyPEM := newAdminCSR(t)
	resp := provisionTenant(t, cluster, systemBearer, tenantID, map[string]interface{}{
		"isolation_type":   "prefix",
		"admin_enrollment": "cert",
		"admin_csr_pem":    string(csrPEM),
	})

	assert.Equal(t, tenantID, resp.TenantID)
	assert.NotEmpty(t, resp.AdminUserID, "admin user id")
	assert.NotEmpty(t, resp.AdminRoleID, "admin role id")
	assert.False(t, resp.AdminPending, "a cert-enrolled admin is active at once")
	assert.Empty(t, resp.InviteToken, "cert mode issues no invite")
	require.NotEmpty(t, resp.AdminCertPEM, "admin certificate")

	ctx, cancel := context.WithTimeout(context.Background(), defaultTimeout)
	defer cancel()
	adminBearer, err := helpers.LoginWithCert(ctx, cluster.httpEndpoint, []byte(resp.AdminCertPEM), keyPEM)
	require.NoError(t, err, "the provisioned admin certificate must log in over mTLS")

	status, body := doBearerRequest(t, cluster, http.MethodGet, tenantUsersPath(tenantID), adminBearer, nil)
	require.Equal(t, http.StatusOK, status, "tenant admin lists the tenant's users: %s", body)
	var users struct {
		Users []json.RawMessage `json:"users"`
	}
	require.NoError(t, json.Unmarshal(body, &users))
	assert.NotEmpty(t, users.Users, "the tenant holds at least its admin")
}

// TestTenantProvision_InviteMode provisions a tenant whose admin is left
// pending behind a one-time invite, then redeems the invite over PIV.
func TestTenantProvision_InviteMode(t *testing.T) {
	cluster := setupTestCluster(t)
	defer cluster.Close()
	systemBearer := helpers.AuthPIV(t, cluster.httpEndpoint)

	tenantID := generateUniqueTenantID()
	resp := provisionTenant(t, cluster, systemBearer, tenantID, map[string]interface{}{
		"isolation_type":   "prefix",
		"admin_enrollment": "invite",
	})

	assert.Equal(t, tenantID, resp.TenantID)
	assert.NotEmpty(t, resp.AdminUserID, "admin user id")
	assert.NotEmpty(t, resp.AdminRoleID, "admin role id")
	assert.True(t, resp.AdminPending, "an invited admin is pending until activation")
	assert.Empty(t, resp.AdminCertPEM, "invite mode issues no certificate")
	require.NotEmpty(t, resp.InviteToken, "invite token")
	require.NotNil(t, resp.InviteExpiresAt, "invite expiry")
	assert.True(t, resp.InviteExpiresAt.After(time.Now()), "invite must not be born expired")

	ctx, cancel := context.WithTimeout(context.Background(), defaultTimeout)
	defer cancel()
	certPEM, keyPEM, err := helpers.IssueTenantClientCert(ctx, tenantID)
	require.NoError(t, err, "issue tenant client cert")

	// Pending: the certificate is not yet bound to the admin, so it cannot
	// log in.
	_, err = helpers.LoginWithCert(ctx, cluster.httpEndpoint, certPEM, keyPEM)
	requireRefused(t, err, "login before invite activation")

	require.NoError(t, helpers.ActivateInvitePIV(ctx, cluster.httpEndpoint, tenantID, resp.InviteToken, certPEM),
		"activate the admin invite over PIV")

	adminBearer, err := helpers.LoginWithCert(ctx, cluster.httpEndpoint, certPEM, keyPEM)
	require.NoError(t, err, "the activated admin must log in over mTLS")

	status, body := doBearerRequest(t, cluster, http.MethodGet, tenantUsersPath(tenantID), adminBearer, nil)
	assert.Equal(t, http.StatusOK, status, "activated tenant admin lists the tenant's users: %s", body)

	// The invite is one-time: a second redemption is refused.
	err = helpers.ActivateInvitePIV(ctx, cluster.httpEndpoint, tenantID, resp.InviteToken, certPEM)
	requireRefused(t, err, "second activation with the same invite token")
}

// TestTenantProvision_SystemPrincipalDeniedTenantData asserts the platform
// operator holds no implicit read on a provisioned tenant's data.
func TestTenantProvision_SystemPrincipalDeniedTenantData(t *testing.T) {
	cluster := setupTestCluster(t)
	defer cluster.Close()
	systemBearer := helpers.AuthPIV(t, cluster.httpEndpoint)

	tenantID := generateUniqueTenantID()
	csrPEM, _ := newAdminCSR(t)
	provisionTenant(t, cluster, systemBearer, tenantID, map[string]interface{}{
		"isolation_type":   "prefix",
		"admin_enrollment": "cert",
		"admin_csr_pem":    string(csrPEM),
	})

	status, body := doBearerRequest(t, cluster, http.MethodGet, tenantUsersPath(tenantID), systemBearer, nil)
	assert.Contains(t, []int{http.StatusForbidden, http.StatusNotFound}, status,
		"system principal reading tenant %s users must be refused: %s", tenantID, body)
}

// TestTenantProvision_SystemTenantInviteRefused asserts invite enrollment of
// the system tenant is refused as invalid input rather than failing inside.
func TestTenantProvision_SystemTenantInviteRefused(t *testing.T) {
	cluster := setupTestCluster(t)
	defer cluster.Close()
	systemBearer := helpers.AuthPIV(t, cluster.httpEndpoint)

	status, body := doBearerRequest(t, cluster, http.MethodPost, provisionPath(qrdbsdk.SystemTenantID), systemBearer,
		map[string]interface{}{"admin_enrollment": "invite"})
	assert.Equal(t, http.StatusBadRequest, status, "system tenant invite provisioning: %s", body)
}

// provisionTenant provisions tenantID as the system admin, requires 201 and
// decodes the response.
func provisionTenant(t *testing.T, cluster *testCluster, bearer, tenantID string, body map[string]interface{}) provisionResponse {
	t.Helper()
	status, raw := doBearerRequest(t, cluster, http.MethodPost, provisionPath(tenantID), bearer, body)
	require.Equal(t, http.StatusCreated, status, "provision tenant %s: %s", tenantID, raw)

	var resp provisionResponse
	require.NoError(t, json.Unmarshal(raw, &resp), "decode provision response: %s", raw)
	return resp
}

// doBearerRequest sends one request with the given bearer and returns the
// status and body. An empty bearer sends the request with no credential.
func doBearerRequest(t *testing.T, cluster *testCluster, method, path, bearer string, body interface{}) (int, []byte) {
	t.Helper()
	var reqBody io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		require.NoError(t, err, "marshal request body")
		reqBody = bytes.NewReader(data)
	}

	ctx, cancel := context.WithTimeout(context.Background(), defaultTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, method, cluster.httpEndpoint+path, reqBody)
	require.NoError(t, err, "build %s %s", method, path)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if bearer != "" {
		req.Header.Set("Authorization", "Bearer "+bearer)
	}

	resp, err := cluster.client.Do(req)
	require.NoError(t, err, "%s %s", method, path)
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	require.NoError(t, err, "read %s %s response", method, path)
	return resp.StatusCode, respBody
}

// requireRefused requires err to be a client-error HTTP refusal: not a
// success, and not a transport failure or server error that would mask one.
func requireRefused(t *testing.T, err error, what string) {
	t.Helper()
	require.Error(t, err, "%s must be refused", what)
	var status *helpers.AuthBadStatusError
	require.True(t, errors.As(err, &status), "%s: want an HTTP refusal, got %v", what, err)
	assert.GreaterOrEqual(t, status.Status, 400, "%s: %s", what, status.Body)
	assert.Less(t, status.Status, 500, "%s: %s", what, status.Body)
}

// newAdminCSR returns a P-256 admin CSR and its private key, both PEM.
func newAdminCSR(t *testing.T) (csrPEM, keyPEM []byte) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err, "generate admin key")
	der, err := x509.CreateCertificateRequest(rand.Reader, &x509.CertificateRequest{
		Subject: pkix.Name{CommonName: "admin"},
	}, key)
	require.NoError(t, err, "create admin CSR")
	keyDER, err := x509.MarshalECPrivateKey(key)
	require.NoError(t, err, "marshal admin key")
	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE REQUEST", Bytes: der}),
		pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER})
}

func provisionPath(tenantID string) string {
	return "/api/v1/tenants/" + url.PathEscape(tenantID) + "/provision"
}

func tenantUsersPath(tenantID string) string {
	return "/api/v1/tenants/" + url.PathEscape(tenantID) + "/users/"
}
