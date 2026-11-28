//go:build rest

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

package apimodule

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"
)

const (
	certLoginRoute = "/api/v1/auth/login/piv"
	otherPublic    = "/api/v1/auth/other"
	noTenant       = "<none>"
)

// tenantProbeModule registers public routes that answer with the tenant bound
// into their request context, so a test sees what the wrapper bound.
type tenantProbeModule struct {
	recordingModule
}

func (m *tenantProbeModule) RegisterHTTP(r *qrdbsdk.ModuleHTTPRegistrar) {
	probe := func(w http.ResponseWriter, req *http.Request) {
		tenant, ok := qrdbsdk.TenantFromContext(req.Context())
		if !ok {
			tenant = noTenant
		}
		_, err := w.Write([]byte(tenant))
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}
	}
	r.HandlePublic(http.MethodPost, certLoginRoute, probe)
	r.HandlePublic(http.MethodPost, otherPublic, probe)
}

// revocationAnswer is a qrdbsdk.RevocationChecker that gives one fixed answer
// and records the certificate it was asked about.
type revocationAnswer struct {
	revoked bool
	err     error
	asked   *x509.Certificate
}

func (a *revocationAnswer) IsCertRevoked(_ context.Context, peer *x509.Certificate) (bool, error) {
	a.asked = peer
	return a.revoked, a.err
}

// errRevocationLookup stands in for a revocation store that cannot be read.
var errRevocationLookup = errors.New("revocation store unreadable")

// certLoginRouter mounts the probe module, wrapped for trustDomain with a
// revocation checker that clears every certificate, on a bare router.
func certLoginRouter(t *testing.T, trustDomain string) http.Handler {
	t.Helper()
	return certLoginRouterWith(t, trustDomain, &revocationAnswer{})
}

// certLoginRouterWith mounts the probe module, wrapped for trustDomain and
// revocation, on a bare router.
func certLoginRouterWith(t *testing.T, trustDomain string, revocation qrdbsdk.RevocationChecker) http.Handler {
	t.Helper()
	hosted, err := NewCertLoginTenantModule(&tenantProbeModule{}, trustDomain, XKMSCertLoginRoutes, revocation)
	require.NoError(t, err)
	router := chi.NewRouter()
	hosted.RegisterHTTP(qrdbsdk.NewModuleHTTPRegistrar(router,
		func(method, pattern string, _ qrdbsdk.Permission, h http.HandlerFunc) {
			router.MethodFunc(method, pattern, h)
		},
		func(method, pattern string, h http.HandlerFunc) {
			router.MethodFunc(method, pattern, h)
		},
	))
	return router
}

// verifiedLeaf returns TLS state whose verified chain is a leaf carrying
// spiffeID as its URI SAN.
func verifiedLeaf(t *testing.T, spiffeID string) *tls.ConnectionState {
	t.Helper()
	uri, err := url.Parse(spiffeID)
	require.NoError(t, err)
	leaf := &x509.Certificate{Subject: pkix.Name{CommonName: "admin"}, URIs: []*url.URL{uri}}
	return &tls.ConnectionState{VerifiedChains: [][]*x509.Certificate{{leaf}}}
}

func serveCertLogin(router http.Handler, path string, state *tls.ConnectionState) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, path, nil)
	req.TLS = state
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	return rec
}

func TestCertLoginTenantModule_BindsTheTenantTheSANNames(t *testing.T) {
	router := certLoginRouter(t, fixtureTrustDomain)
	state := verifiedLeaf(t, "spiffe://"+fixtureTrustDomain+"/tenant/acme/user/admin")

	rec := serveCertLogin(router, certLoginRoute, state)

	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.Equal(t, qrdbsdk.NormalizeTenantID("acme"), rec.Body.String())
}

func TestCertLoginTenantModule_RefusesAForeignTrustDomain(t *testing.T) {
	router := certLoginRouter(t, fixtureTrustDomain)
	state := verifiedLeaf(t, "spiffe://other.example/tenant/acme/user/admin")

	rec := serveCertLogin(router, certLoginRoute, state)

	assert.Equal(t, http.StatusUnauthorized, rec.Code)
	assert.NotContains(t, rec.Body.String(), "acme")
}

// The subject OU carries the tenant (#1388): an OU naming a tenant other
// than the SAN's is refused.
func TestCertLoginTenantModule_RefusesAnOUNamingAnotherTenant(t *testing.T) {
	router := certLoginRouter(t, fixtureTrustDomain)
	state := verifiedLeaf(t, "spiffe://"+fixtureTrustDomain+"/tenant/acme/user/admin")
	state.VerifiedChains[0][0].Subject.OrganizationalUnit = []string{"globex"}

	rec := serveCertLogin(router, certLoginRoute, state)

	assert.Equal(t, http.StatusUnauthorized, rec.Code)
}

func TestCertLoginTenantModule_RefusesARevokedCertificate(t *testing.T) {
	tenantSAN := "spiffe://" + fixtureTrustDomain + "/tenant/acme/user/admin"
	for _, trustDomain := range []string{fixtureTrustDomain, ""} {
		t.Run("trust domain "+trustDomain, func(t *testing.T) {
			revocation := &revocationAnswer{revoked: true}
			state := verifiedLeaf(t, tenantSAN)

			rec := serveCertLogin(certLoginRouterWith(t, trustDomain, revocation), certLoginRoute, state)

			assert.Equal(t, http.StatusUnauthorized, rec.Code)
			assert.NotContains(t, rec.Body.String(), "acme")
			assert.Same(t, state.VerifiedChains[0][0], revocation.asked, "the verified leaf is the certificate checked")
		})
	}
}

func TestCertLoginTenantModule_FailsClosedWhenRevocationLookupFails(t *testing.T) {
	revocation := &revocationAnswer{err: errRevocationLookup}
	state := verifiedLeaf(t, "spiffe://"+fixtureTrustDomain+"/tenant/acme/user/admin")

	rec := serveCertLogin(certLoginRouterWith(t, fixtureTrustDomain, revocation), certLoginRoute, state)

	assert.Equal(t, http.StatusServiceUnavailable, rec.Code)
	assert.NotContains(t, rec.Body.String(), "acme")
}

func TestCertLoginTenantModule_ChecksRevocationOnlyOnCertLoginRoutes(t *testing.T) {
	revocation := &revocationAnswer{revoked: true}
	state := verifiedLeaf(t, "spiffe://"+fixtureTrustDomain+"/tenant/acme/user/admin")

	rec := serveCertLogin(certLoginRouterWith(t, fixtureTrustDomain, revocation), otherPublic, state)

	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.Nil(t, revocation.asked)
}

func TestCertLoginTenantModule_PassesThrough(t *testing.T) {
	tenantSAN := "spiffe://" + fixtureTrustDomain + "/tenant/acme/user/admin"
	tests := []struct {
		name        string
		trustDomain string
		path        string
		state       *tls.ConnectionState
	}{
		{name: "no TLS", trustDomain: fixtureTrustDomain, path: certLoginRoute},
		{name: "unverified client", trustDomain: fixtureTrustDomain, path: certLoginRoute, state: &tls.ConnectionState{}},
		{name: "route not listed", trustDomain: fixtureTrustDomain, path: otherPublic, state: verifiedLeaf(t, tenantSAN)},
		{name: "no trust domain", path: certLoginRoute, state: verifiedLeaf(t, tenantSAN)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := serveCertLogin(certLoginRouter(t, tt.trustDomain), tt.path, tt.state)

			require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
			assert.Equal(t, noTenant, rec.Body.String())
		})
	}
}

func TestNewCertLoginTenantModule_RejectsBadInput(t *testing.T) {
	t.Run("nil module", func(t *testing.T) {
		got, err := NewCertLoginTenantModule(nil, fixtureTrustDomain, XKMSCertLoginRoutes, &revocationAnswer{})
		require.Nil(t, got)
		var want *NilModuleError
		require.ErrorAs(t, err, &want)
	})

	t.Run("nil revocation checker", func(t *testing.T) {
		got, err := NewCertLoginTenantModule(&tenantProbeModule{}, fixtureTrustDomain, XKMSCertLoginRoutes, nil)
		require.Nil(t, got)
		var want *NilRevocationCheckerError
		require.ErrorAs(t, err, &want)
	})

	t.Run("module missing an optional extension", func(t *testing.T) {
		got, err := NewCertLoginTenantModule(&bareModule{}, fixtureTrustDomain, XKMSCertLoginRoutes, &revocationAnswer{})
		require.Nil(t, got)
		var want *ModuleExtensionMissingError
		require.ErrorAs(t, err, &want)
		require.Equal(t, "ModuleMCPToolPermissioner", want.Extension)
	})
}

func TestCertLoginTenantModule_ForwardsEveryOtherHook(t *testing.T) {
	inner := &tenantProbeModule{recordingModule: recordingModule{publicGRPC: []string{"/pkg.Service/Method"}}}
	hosted, err := NewCertLoginTenantModule(inner, fixtureTrustDomain, XKMSCertLoginRoutes, &revocationAnswer{})
	require.NoError(t, err)

	hosted.RegisterHTTP(nil)

	lister, ok := hosted.(qrdbsdk.ModuleGRPCPublicMethodLister)
	require.True(t, ok)
	assert.Equal(t, []string{"/pkg.Service/Method"}, lister.PublicGRPCMethods())
	_, ok = hosted.(qrdbsdk.ModuleAuthFallback)
	assert.True(t, ok)
}
