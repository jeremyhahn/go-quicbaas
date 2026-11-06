//go:build !integration

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
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/service"
)

// newOperatorTLSServingNode boots the node newOperatorTLSIntegrationNode
// boots, with the node's own REST listener turned on, so a test reaches the
// routes through the node's real HTTP auth path. It also returns the
// listener's base URL. The port is drawn, never 0: qrdb_node.go copies it
// into the node's AdminPort, where 0 becomes a fixed shared port (see
// NewSealedBootTestConfig).
func newOperatorTLSServingNode(t *testing.T) (*Server, service.IAMComponents, *x509.Certificate, string) {
	t.Helper()
	setupOIDCTestXKMS(t)
	srv := withTempServer(t)
	certFile, keyFile := writeOperatorPair(t, t.TempDir())
	srv.config.Security.TLSEnabled = true
	srv.config.Security.TLSClientAuth = false
	srv.config.Security.TLSCertFile = certFile
	srv.config.Security.TLSKeyFile = keyFile
	srv.config.Security.TLSCAFile = certFile
	srv.config.CA.KeysDir = filepath.Join(t.TempDir(), "ca-keys")
	require.NoError(t, srv.initializeCA(), "initializeCA")
	wireCAAdapter(t, srv)

	port := getFreePort(t)
	srv.config.Server.REST.Enabled = true
	srv.config.Server.REST.Port = port
	srv.config.Server.REST.BindAddress = "127.0.0.1"
	srv.config.Server.GRPC.Enabled = false
	srv.config.Server.MCP.Enabled = false
	// The node's PIV login module reads these when the node is built, so
	// they are set before it, not after as newOperatorTLSIntegrationNode does.
	srv.config.IAM.SPIFFETrustDomain = testTrustDomain
	srv.config.IAM.PIVLogin.Enabled = true
	srv.config.IAM.PIVLogin.AllowUnattested = true
	adminCertPath, _ := writeBootstrapPIVCert(t)
	srv.config.Auth.BootstrapAdminPIVCertPath = adminCertPath

	require.NoError(t, srv.initStandaloneQRDBNode(), "qrdb node init")
	t.Cleanup(func() { assert.NoError(t, srv.stopQRDBNode(), "stopping the qrdb node") })
	srv.qrdbClient = newNodeBackedShardedClient(srv.qrdbNode)

	components, err := srv.initializeIAMComponents()
	require.NoError(t, err, "first boot")

	return srv, components, diskLeaf(t, certFile), fmt.Sprintf("https://127.0.0.1:%d", port)
}

// TestTenantAdminBearer_NodeListenerBindsTenant reproduces #2212 in the unit
// tier: a provisioned tenant admin logs in with PIV through the node's own
// listener, then creates a role with the bearer alone, as the integration
// suite's tenant admin does.
func TestTenantAdminBearer_NodeListenerBindsTenant(t *testing.T) {
	_, components, root, baseURL := newOperatorTLSServingNode(t)
	adminCert := provisionCertModeTenant(t, components, "acme")

	roots := x509.NewCertPool()
	roots.AddCert(root)
	loginClient := &http.Client{
		Timeout: 30 * time.Second,
		Transport: &http.Transport{TLSClientConfig: &tls.Config{
			Certificates: []tls.Certificate{adminCert},
			RootCAs:      roots,
			ServerName:   "localhost",
			MinVersion:   tls.VersionTLS13,
		}},
	}
	t.Cleanup(loginClient.CloseIdleConnections)

	loginReq, err := http.NewRequestWithContext(context.Background(), http.MethodPost, baseURL+xkmsPIVLoginPath, nil)
	require.NoError(t, err)
	loginResp, err := loginClient.Do(loginReq)
	require.NoError(t, err)
	defer loginResp.Body.Close()
	loginBody, err := io.ReadAll(loginResp.Body)
	require.NoError(t, err)

	var login map[string]any
	accessToken := ""
	if json.Unmarshal(loginBody, &login) == nil {
		if token, ok := login["access_token"].(string); ok {
			accessToken = token
			login["access_token"] = "<redacted>"
		}
		if _, ok := login["refresh_token"]; ok {
			login["refresh_token"] = "<redacted>"
		}
		t.Logf("PIV login status %d, body %v", loginResp.StatusCode, login)
	} else {
		t.Logf("PIV login status %d, body %s", loginResp.StatusCode, loginBody)
	}
	require.Equal(t, http.StatusOK, loginResp.StatusCode, "PIV login")
	require.NotEmpty(t, accessToken, "PIV login returned no access_token")

	bearerClient := &http.Client{
		Timeout: 30 * time.Second,
		Transport: &http.Transport{TLSClientConfig: &tls.Config{
			RootCAs:    roots,
			ServerName: "localhost",
			MinVersion: tls.VersionTLS13,
		}},
	}
	t.Cleanup(bearerClient.CloseIdleConnections)

	roleReq, err := http.NewRequestWithContext(context.Background(), http.MethodPost,
		baseURL+"/api/v1/tenants/acme/roles",
		strings.NewReader(`{"id":"repro","name":"repro","permissions":["kv:read"]}`))
	require.NoError(t, err)
	roleReq.Header.Set("Authorization", "Bearer "+accessToken)
	roleReq.Header.Set("Content-Type", "application/json")
	roleResp, err := bearerClient.Do(roleReq)
	require.NoError(t, err)
	defer roleResp.Body.Close()
	roleBody, err := io.ReadAll(roleResp.Body)
	require.NoError(t, err)
	t.Logf("role create status %d, body %s", roleResp.StatusCode, roleBody)

	assert.Contains(t, []int{http.StatusCreated, http.StatusOK}, roleResp.StatusCode,
		"role create with the bearer alone: %s", roleBody)
}
