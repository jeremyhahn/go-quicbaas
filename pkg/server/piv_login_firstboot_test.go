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
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"
	xkmslib "github.com/jeremyhahn/go-xkms/pkg/xkms"
	xkmssdk "github.com/jeremyhahn/go-xkms/sdk/go"

	"github.com/jeremyhahn/go-quicbaas/pkg/service"
)

// xkmsPIVLoginPath is the xkms module's own certificate login route, the one
// the integration helpers log the system admin in through.
const xkmsPIVLoginPath = "/api/v1/auth/login/piv"

// newIntegrationLikeNode boots a fresh embedded node with the integration
// cluster's IAM settings (test/integration/docker-compose.integration.yml):
// a bootstrap admin PIV certificate, PIV login enabled and unattested login
// allowed. It then runs IAM initialization, and so first boot, exactly as the
// daemon does, and returns the server and the bound certificate.
func newIntegrationLikeNode(t *testing.T, configure ...func(*Server)) (*Server, service.IAMComponents, *x509.Certificate) {
	t.Helper()
	setupOIDCTestXKMS(t)
	srv := newFreshInstallNode(t)
	srv.xkmsCA = newFakeTrustedCA(t)
	srv.config.IAM.SPIFFETrustDomain = testTrustDomain
	srv.config.IAM.PIVLogin.Enabled = true
	srv.config.IAM.PIVLogin.AllowUnattested = true
	certPath, _ := writeBootstrapPIVCert(t)
	srv.config.Auth.BootstrapAdminPIVCertPath = certPath
	for _, fn := range configure {
		fn(srv)
	}

	components, err := srv.initializeIAMComponents()
	require.NoError(t, err, "first boot")

	certPEM, err := os.ReadFile(certPath)
	require.NoError(t, err)
	block, _ := pem.Decode(certPEM)
	require.NotNil(t, block)
	cert, err := x509.ParseCertificate(block.Bytes)
	require.NoError(t, err)
	return srv, components, cert
}

// xkmsPIVLoginRouter mounts the xkms module the way unifiedModuleServerOptions
// composes it: the server's own PIV login options and node client, wrapped in
// the composer's route exclusions.
func xkmsPIVLoginRouter(t *testing.T, srv *Server) http.Handler {
	t.Helper()
	hosted, err := srv.hostXKMSModule(newXKMSPIVLoginModule(t, srv))
	require.NoError(t, err)
	return moduleRouter(hosted)
}

// newXKMSPIVLoginModule builds the go-xkms service module with the PIV login
// options srv derives from its configuration, as the qrdb node builds it.
func newXKMSPIVLoginModule(t *testing.T, srv *Server) qrdbsdk.ServiceModule {
	t.Helper()
	module, err := xkmssdk.NewServiceModule(
		func() *xkmslib.XKMSService {
			svc, getErr := xkmssdk.Get()
			if getErr != nil {
				return nil
			}
			return svc
		},
		xkmssdk.WithServiceModuleQRDBClient(srv.GetQRDBNodeClient),
		xkmssdk.WithServiceModuleSession(&xkmssdk.ServiceModuleSessionOptions{}),
		xkmssdk.WithServiceModulePIVLogin(srv.pivLoginModuleOptions()),
	)
	require.NoError(t, err)
	return module
}

// moduleRouter mounts every HTTP route module registers, guarded and public,
// on a bare router.
func moduleRouter(module qrdbsdk.ServiceModule) http.Handler {
	router := chi.NewRouter()
	module.RegisterHTTP(qrdbsdk.NewModuleHTTPRegistrar(router,
		func(method, pattern string, _ qrdbsdk.Permission, h http.HandlerFunc) {
			router.MethodFunc(method, pattern, h)
		},
		func(method, pattern string, h http.HandlerFunc) {
			router.MethodFunc(method, pattern, h)
		},
	))
	return router
}

// postXKMSPIVLogin sends the login with cert as the TLS-verified peer
// certificate, as the unified server's listener presents it.
func postXKMSPIVLogin(router http.Handler, cert *x509.Certificate) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, xkmsPIVLoginPath, nil)
	req.TLS = &tls.ConnectionState{
		PeerCertificates: []*x509.Certificate{cert},
		VerifiedChains:   [][]*x509.Certificate{{cert}},
	}
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	return rec
}

// TestFirstBoot_BootstrapAdminPIVCertLogsInThroughXKMS is go-quicbaas#1727.
// The integration cluster's system admin logs in with the certificate first
// boot bound, through the xkms module's PIV login. That route enforces the
// account's permitted-method policy for mtls, and first boot left the admin
// on the webauthn default, so every data-plane helper got 401.
func TestFirstBoot_BootstrapAdminPIVCertLogsInThroughXKMS(t *testing.T) {
	srv, _, cert := newIntegrationLikeNode(t)

	rec := postXKMSPIVLogin(xkmsPIVLoginRouter(t, srv), cert)

	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.Contains(t, rec.Body.String(), "access_token")
}

// TestFirstBoot_UnboundCertIsRefusedByXKMSPIVLogin is the refusal beside it:
// a certificate first boot did not bind must not log in.
func TestFirstBoot_UnboundCertIsRefusedByXKMSPIVLogin(t *testing.T) {
	srv, _, _ := newIntegrationLikeNode(t)
	otherPath, _ := writeBootstrapPIVCert(t)
	otherPEM, err := os.ReadFile(otherPath)
	require.NoError(t, err)
	block, _ := pem.Decode(otherPEM)
	require.NotNil(t, block)
	other, err := x509.ParseCertificate(block.Bytes)
	require.NoError(t, err)

	rec := postXKMSPIVLogin(xkmsPIVLoginRouter(t, srv), other)

	assert.Equal(t, http.StatusUnauthorized, rec.Code, rec.Body.String())
}
