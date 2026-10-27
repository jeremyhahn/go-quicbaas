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
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"

	"github.com/jeremyhahn/go-quicbaas/pkg/service"
)

// newOperatorTLSIntegrationNode boots a node in the integration cluster's TLS
// posture: an operator-supplied server cert and CA (QUICBAAS_SECURITY_TLS_*),
// optional mTLS, the platform xkms CA the node generates for itself, and the
// IAM settings of newIntegrationLikeNode. It returns the server, its IAM
// components and the operator cert, which clients pin as their root.
func newOperatorTLSIntegrationNode(t *testing.T) (*Server, service.IAMComponents, *x509.Certificate) {
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
	srv.config.IAM.SPIFFETrustDomain = testTrustDomain
	srv.config.IAM.PIVLogin.Enabled = true
	srv.config.IAM.PIVLogin.AllowUnattested = true
	require.NoError(t, srv.initializeCA(), "initializeCA")

	require.NoError(t, srv.initStandaloneQRDBNode(), "qrdb node init")
	t.Cleanup(func() { assert.NoError(t, srv.stopQRDBNode(), "stopping the qrdb node") })
	srv.qrdbClient = newNodeBackedShardedClient(srv.qrdbNode)

	adminCertPath, _ := writeBootstrapPIVCert(t)
	srv.config.Auth.BootstrapAdminPIVCertPath = adminCertPath
	components, err := srv.initializeIAMComponents()
	require.NoError(t, err, "first boot")

	return srv, components, diskLeaf(t, certFile)
}

// provisionCertModeTenant provisions a tenant the way the integration helper
// ProvisionTenant does: the admin's CSR is signed by the node and the
// returned certificate is the admin's login credential.
func provisionCertModeTenant(t *testing.T, components service.IAMComponents, tenantID string) tls.Certificate {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)
	der, err := x509.CreateCertificateRequest(rand.Reader, &x509.CertificateRequest{
		Subject: pkix.Name{CommonName: "admin"},
	}, key)
	require.NoError(t, err)

	resp, err := components.GetTenantProvisioner().Provision(context.Background(), &service.IAMProvisionTenantRequest{
		IsolationType: qrdbsdk.TenantIsolationPrefix,
		TenantID:      tenantID,
		AdminCSRPEM:   string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE REQUEST", Bytes: der})),
	})
	require.NoError(t, err, "provision")

	keyDER, err := x509.MarshalPKCS8PrivateKey(key)
	require.NoError(t, err)
	pair, err := tls.X509KeyPair([]byte(resp.AdminCertPEM),
		pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: keyDER}))
	require.NoError(t, err)
	return pair
}

// postPIVLoginOverTLS serves router behind the unified server's own TLS config
// and logs in with clientCert over a real handshake, as the integration
// helpers do against the cluster.
func postPIVLoginOverTLS(t *testing.T, srv *Server, router http.Handler, root *x509.Certificate, clientCert tls.Certificate) (int, string) {
	t.Helper()
	ts := httptest.NewUnstartedServer(router)
	ts.TLS = srv.unifiedServerTLSConfig()
	ts.StartTLS()
	t.Cleanup(ts.Close)
	return postPIVLogin(t, ts.URL, root, clientCert)
}

// postPIVLogin presents clientCert over mTLS to the go-xkms PIV login at
// baseURL, trusting only root, and returns the status and body.
func postPIVLogin(t *testing.T, baseURL string, root *x509.Certificate, clientCert tls.Certificate) (int, string) {
	t.Helper()
	roots := x509.NewCertPool()
	roots.AddCert(root)
	client := &http.Client{
		Timeout: 30 * time.Second,
		Transport: &http.Transport{TLSClientConfig: &tls.Config{
			Certificates: []tls.Certificate{clientCert},
			RootCAs:      roots,
			ServerName:   "localhost",
			MinVersion:   tls.VersionTLS13,
		}},
	}
	t.Cleanup(client.CloseIdleConnections)

	req, err := http.NewRequestWithContext(context.Background(), http.MethodPost, baseURL+xkmsPIVLoginPath, nil)
	require.NoError(t, err)
	resp, err := client.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	return resp.StatusCode, string(body)
}

// TestOperatorTLS_ProvisionedTenantAdminCertReachesLogin is the second half of
// go-quicbaas#1727. With an operator server cert, the listener's ClientCAs held
// only the operator CA, so a client holding a certificate the platform CA
// signed (every cert-mode tenant admin) was never asked for it: Go's client
// sends a certificate only when its issuer is among the acceptable CAs, and the
// PIV login answered "client certificate required".
func TestOperatorTLS_ProvisionedTenantAdminCertReachesLogin(t *testing.T) {
	srv, components, root := newOperatorTLSIntegrationNode(t)
	adminCert := provisionCertModeTenant(t, components, "acme")

	_, body := postPIVLoginOverTLS(t, srv, xkmsPIVLoginRouter(t, srv), root, adminCert)

	assert.NotContains(t, body, "client certificate required")
}

// TestOperatorTLS_ProvisionedTenantAdminLogsInOverTLS proves the rest of the
// login the provisioner owns: with the tenant bound, the admin's certificate
// resolves to the admin and its method policy permits mtls.
func TestOperatorTLS_ProvisionedTenantAdminLogsInOverTLS(t *testing.T) {
	srv, components, root := newOperatorTLSIntegrationNode(t)
	adminCert := provisionCertModeTenant(t, components, "acme")

	status, body := postPIVLoginOverTLS(t, srv, xkmsPIVLoginRouter(t, srv), root, adminCert)

	require.Equal(t, http.StatusOK, status, body)
	assert.Contains(t, body, "access_token")
}

// TestOperatorTLS_ProvisionedTenantAdminLogsInThroughNodeREST is go-quicbaas#1959:
// the integration suites' data-plane helper provisions a cert-mode tenant and
// logs its admin in at the node's own REST listener. No middleware binds a
// tenant on that public route, so go-xkms looked the admin's certificate up in
// the system tenant and answered 401 "authentication failed".
func TestOperatorTLS_ProvisionedTenantAdminLogsInThroughNodeREST(t *testing.T) {
	srv, components, root := newOperatorTLSIntegrationNode(t)
	adminCert := provisionCertModeTenant(t, components, "acme")
	addr := srv.RESTAddr()
	require.NotNil(t, addr, "the qrdb node serves REST")

	status, body := postPIVLogin(t, "https://"+addr.String(), root, adminCert)

	require.Equal(t, http.StatusOK, status, body)
	assert.Contains(t, body, "access_token")
}

// TestOperatorTLS_RevokedTenantAdminCertIsRefusedAtNodeREST pins go-quicbaas#1991:
// a tenant admin certificate revoked at the platform CA no longer logs in, even
// though its user binding stays active. The same certificate logs in before
// the revocation, so the refusal comes from the CA's revocation store alone.
func TestOperatorTLS_RevokedTenantAdminCertIsRefusedAtNodeREST(t *testing.T) {
	srv, components, root := newOperatorTLSIntegrationNode(t)
	adminCert := provisionCertModeTenant(t, components, "acme")
	addr := srv.RESTAddr()
	require.NotNil(t, addr, "the qrdb node serves REST")
	baseURL := "https://" + addr.String()

	status, body := postPIVLogin(t, baseURL, root, adminCert)
	require.Equal(t, http.StatusOK, status, "an unrevoked certificate logs in: %s", body)

	leaf, err := x509.ParseCertificate(adminCert.Certificate[0])
	require.NoError(t, err)
	require.NoError(t, srv.CA().Revoke(context.Background(), leaf.SerialNumber, 0))

	status, body = postPIVLogin(t, baseURL, root, adminCert)
	require.Equal(t, http.StatusUnauthorized, status, body)
	assert.NotContains(t, body, "access_token")
}

// TestOperatorTLS_ForeignTrustDomainTenantCertIsRefusedAtNodeREST proves the
// tenant binding pins the configured trust domain: a certificate the platform
// CA signed whose SPIFFE SAN names another trust domain chooses no tenant and
// never reaches the login.
func TestOperatorTLS_ForeignTrustDomainTenantCertIsRefusedAtNodeREST(t *testing.T) {
	srv, components, root := newOperatorTLSIntegrationNode(t)
	adminCert := provisionCertModeTenant(t, components, "acme")
	srv.config.IAM.SPIFFETrustDomain = "someone.else.test"
	module, err := srv.hostXKMSModule(newXKMSPIVLoginModule(t, srv))
	require.NoError(t, err)

	status, body := postPIVLoginOverTLS(t, srv, moduleRouter(module), root, adminCert)

	require.Equal(t, http.StatusUnauthorized, status, body)
	assert.NotContains(t, body, "access_token")
}

// TestOperatorTLS_UnknownCACertIsNotAccepted is the refusal beside it: a
// certificate from a CA the node does not trust still never reaches the login.
func TestOperatorTLS_UnknownCACertIsNotAccepted(t *testing.T) {
	srv, _, root := newOperatorTLSIntegrationNode(t)
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)
	now := time.Now()
	certPEM := mustIssueSelfSignedCert(t, key, "stranger", now.Add(-time.Hour), now.Add(time.Hour))
	keyDER, err := x509.MarshalPKCS8PrivateKey(key)
	require.NoError(t, err)
	stranger, err := tls.X509KeyPair(certPEM, pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: keyDER}))
	require.NoError(t, err)

	status, body := postPIVLoginOverTLS(t, srv, xkmsPIVLoginRouter(t, srv), root, stranger)

	assert.Equal(t, http.StatusUnauthorized, status, body)
	assert.Contains(t, body, "client certificate required")
}

// TestInitializeCA_OperatorCert_ClientCAsTrustOperatorAndPlatform pins the
// invariant lifecycle_client_ca_test.go states for the internal-CA path on the
// operator-cert path too: in optional mTLS the listener trusts both the
// operator's CA and the platform CA, and solicits but does not require a cert.
func TestInitializeCA_OperatorCert_ClientCAsTrustOperatorAndPlatform(t *testing.T) {
	srv, _, root := newOperatorTLSIntegrationNode(t)

	bundle, err := srv.xkmsCA.CABundle()
	require.NoError(t, err)
	block, _ := pem.Decode(bundle)
	require.NotNil(t, block, "the platform CA bundle holds a certificate")
	platformCA, err := x509.ParseCertificate(block.Bytes)
	require.NoError(t, err)
	_, err = platformCA.Verify(x509.VerifyOptions{Roots: srv.tlsConfig.ClientCAs})
	require.NoError(t, err, "the platform CA must be trusted for client certs")

	_, err = root.Verify(x509.VerifyOptions{Roots: srv.tlsConfig.ClientCAs})
	require.NoError(t, err, "the operator CA must stay trusted")
	assert.Equal(t, tls.VerifyClientCertIfGiven, srv.tlsConfig.ClientAuth)
}
