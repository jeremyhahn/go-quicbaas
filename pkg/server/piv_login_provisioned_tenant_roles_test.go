//go:build mcp && rest && !integration

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

package server

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"io"
	"net/http"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// roleTestTenant is the cert-mode tenant the test provisions.
const roleTestTenant = "acme"

// unifiedTLSClient returns a client that pins root and, when cert is non-nil,
// presents it in the handshake.
func unifiedTLSClient(t *testing.T, root *x509.Certificate, cert *tls.Certificate) *http.Client {
	t.Helper()
	roots := x509.NewCertPool()
	roots.AddCert(root)
	cfg := &tls.Config{RootCAs: roots, ServerName: "localhost", MinVersion: tls.VersionTLS13}
	if cert != nil {
		cfg.Certificates = []tls.Certificate{*cert}
	}
	client := &http.Client{Timeout: 30 * time.Second, Transport: &http.Transport{TLSClientConfig: cfg}}
	t.Cleanup(client.CloseIdleConnections)
	return client
}

// doUnified sends one request and returns the status and body.
func doUnified(t *testing.T, client *http.Client, method, url, bearer string, body []byte) (int, string) {
	t.Helper()
	req, err := http.NewRequestWithContext(context.Background(), method, url, bytes.NewReader(body))
	require.NoError(t, err)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if bearer != "" {
		req.Header.Set("Authorization", "Bearer "+bearer)
	}
	resp, err := client.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	return resp.StatusCode, string(raw)
}

// TestPIVLogin_CertProvisionedTenantAdminCreatesRoleOverUnifiedREST boots the
// node the way the integration cluster runs it (unified server, REST on, TLS
// with an operator cert, PIV login on), provisions a cert-mode tenant, logs in
// as its admin over the real listener and sends the integration helper's
// create-role request with that bearer. The docker suites see
// 403 "tenantctx: rest.IAM: no tenant is bound to the request" here.
func TestPIVLogin_CertProvisionedTenantAdminCreatesRoleOverUnifiedREST(t *testing.T) {
	cfg := productionPostureConfig(t, false)
	cfg.Server.Embedded = false // startRESTServer skips embedded mode (lifecycle_rest.go:77)
	cfg.Server.REST.Enabled = true
	certFile, keyFile := writeOperatorPair(t, t.TempDir())
	cfg.Security.TLSEnabled = true
	cfg.Security.TLSClientAuth = false
	cfg.Security.TLSCertFile = certFile
	cfg.Security.TLSKeyFile = keyFile
	cfg.Security.TLSCAFile = certFile
	cfg.CA.KeysDir = filepath.Join(t.TempDir(), "ca-keys")
	cfg.IAM.PIVLogin.Enabled = true
	cfg.IAM.PIVLogin.AllowUnattested = true
	adminCertPath, _ := writeBootstrapPIVCert(t)
	cfg.Auth.BootstrapAdminPIVCertPath = adminCertPath

	srv := bootProductionPostureServer(t, cfg)
	require.NotNil(t, srv.iamComponents, "IAM components composed at boot")
	adminCert := provisionCertModeTenant(t, srv.iamComponents, roleTestTenant)

	root := diskLeaf(t, certFile)
	base := "https://localhost:" + strconv.Itoa(cfg.Server.REST.Port)
	login := unifiedTLSClient(t, root, &adminCert)

	// The module surface answers 503 until the REST wiring composes.
	var status int
	var body string
	require.Eventually(t, func() bool {
		status, body = doUnified(t, login, http.MethodPost, base+xkmsPIVLoginPath, "", nil)
		return status != http.StatusServiceUnavailable
	}, 30*time.Second, 250*time.Millisecond)
	require.Equal(t, http.StatusOK, status, "tenant admin PIV login: %s", body)
	var tok struct {
		AccessToken string `json:"access_token"`
	}
	require.NoError(t, json.Unmarshal([]byte(body), &tok), body)
	require.NotEmpty(t, tok.AccessToken, body)

	// Body of enrollDataPlanePrincipal (auth_data_plane.go:104-108).
	role, err := json.Marshal(map[string]any{
		"id":   "integration-data-plane",
		"name": "integration-data-plane",
		"permissions": []string{
			"kv:get", "kv:put", "kv:delete", "kv:list", "kv:range", "batch:execute",
		},
	})
	require.NoError(t, err)
	url := base + "/api/v1/tenants/" + roleTestTenant + "/roles"

	// Two client shapes: bearer over a connection that also presents the admin
	// certificate (qrdb mTLS decides first; platform_validator.go:121 declines
	// the bearer when a peer cert is present), and bearer alone (module
	// fallback, auth_support.go:234). Each role id is distinct.
	for name, cert := range map[string]*tls.Certificate{"with-client-cert": &adminCert, "bearer-only": nil} {
		t.Run(name, func(t *testing.T) {
			var r map[string]any
			require.NoError(t, json.Unmarshal(role, &r))
			r["id"], r["name"] = "dp-"+name, "dp-"+name
			b, err := json.Marshal(r)
			require.NoError(t, err)
			status, body := doUnified(t, unifiedTLSClient(t, root, cert), http.MethodPost, url, tok.AccessToken, b)
			require.Truef(t, status >= 200 && status < 300,
				"POST %s as the tenant admin: status %d body %s", url, status, body)
		})
	}
}
