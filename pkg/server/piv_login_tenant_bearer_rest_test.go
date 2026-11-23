//go:build !integration && rest

package server

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The node registers quicbaas REST routes only in rest builds (pkg/apimodule/module_http.go), so this test needs the tag.
//
// TestPIVLogin_TenantBearerCreatesRoleThroughComposedNode sends the bearer the
// way the integration helpers do: bearer only, no client certificate, to the
// node's own REST listener. The IAM role route has to bind the bearer's tenant
// rather than refuse it 403 "no tenant is bound to the request" (#2134).
func TestPIVLogin_TenantBearerCreatesRoleThroughComposedNode(t *testing.T) {
	srv, components, root := newOperatorTLSIntegrationNode(t)
	adminCert := provisionCertModeTenant(t, components, "acme")
	hosted := hostedXKMSPIVLoginModule(t, srv)

	status, body := postPIVLoginOverTLS(t, srv, moduleRouter(hosted), root, adminCert)
	require.Equal(t, http.StatusOK, status, body)

	var tokens struct {
		AccessToken string `json:"access_token"`
	}
	require.NoError(t, json.Unmarshal([]byte(body), &tokens))
	require.NotEmpty(t, tokens.AccessToken)

	// Compose the service layer so the node's REST module serves the IAM
	// routes rather than answering 503 "still composing".
	require.NoError(t, srv.startService(), "compose the quicbaas service")

	// The node-level authenticator is what projects a qrdb identity for a
	// bearer with no client certificate; without one the IAM handler sees
	// an anonymous request and urlTenant refuses it 403.
	identity, err := newPlatformBearerAuthenticator(srv).(*platformBearerAuthenticator).
		authenticateAuthorization(context.Background(), "Bearer "+tokens.AccessToken)
	if assert.NoError(t, err, "the node authenticator must accept the PIV login bearer") {
		assert.Equal(t, "acme", identity.TenantID)
	}

	status, body = postBearerOnlyToNodeREST(t, srv, root, "/api/v1/tenants/acme/roles",
		tokens.AccessToken, `{"name":"itest-role"}`)
	t.Logf("POST /api/v1/tenants/acme/roles: %d %s", status, body)
	require.Equal(t, http.StatusCreated, status, body)

	var role struct {
		Name     string `json:"name"`
		TenantID string `json:"tenant_id"`
	}
	require.NoError(t, json.Unmarshal([]byte(body), &role))
	assert.Equal(t, "itest-role", role.Name)
	assert.Equal(t, "acme", role.TenantID)
}

// postBearerOnlyToNodeREST posts body to path on the qrdb node's own REST
// listener with the bearer and no client certificate: the TLS config carries
// RootCAs only, as test/integration/helpers NewHTTPClient does.
func postBearerOnlyToNodeREST(t *testing.T, srv *Server, root *x509.Certificate, path, bearer, body string) (int, string) {
	t.Helper()
	roots := x509.NewCertPool()
	roots.AddCert(root)
	client := &http.Client{
		Timeout: 30 * time.Second,
		Transport: &http.Transport{TLSClientConfig: &tls.Config{
			RootCAs:    roots,
			ServerName: "localhost",
			MinVersion: tls.VersionTLS13,
		}},
	}
	t.Cleanup(client.CloseIdleConnections)

	url := fmt.Sprintf("https://127.0.0.1:%d%s", srv.config.Server.REST.Port, path)
	req, err := http.NewRequestWithContext(context.Background(), http.MethodPost, url, strings.NewReader(body))
	require.NoError(t, err)
	req.Header.Set("Authorization", "Bearer "+bearer)
	req.Header.Set("Content-Type", "application/json")
	resp, err := client.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	respBody, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	return resp.StatusCode, string(respBody)
}
