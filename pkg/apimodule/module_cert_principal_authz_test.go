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
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"math/big"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	qrdbauth "github.com/jeremyhahn/go-qrdb/pkg/auth"
	"github.com/jeremyhahn/go-qrdb/pkg/auth/middleware"
	"github.com/jeremyhahn/go-qrdb/pkg/auth/rbac"
	"github.com/jeremyhahn/go-qrdb/pkg/permission"
	"github.com/jeremyhahn/go-qrdb/pkg/user"

	restapi "github.com/jeremyhahn/go-quicbaas/pkg/api/rest"
	"github.com/jeremyhahn/go-quicbaas/pkg/iam"
	"github.com/jeremyhahn/go-quicbaas/pkg/iam/qrdbadapter"
	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
	"github.com/jeremyhahn/go-quicbaas/pkg/testutil"
)

// =============================================================================
// go-quicbaas#663 — which principal a request carrying BOTH credentials is
// authorized as.
//
// A fleet node daemon presents its mTLS client certificate on every call, because
// it needs one for /oauth2/token-mtls, and a bearer minted from its OAuth2
// application. go-qrdb's authenticator chain is first-success-wins with mTLS
// FIRST, and a capability module's bearer fallback is consulted ONLY when that
// chain produces no identity, so the principal is the CERTIFICATE's and the
// application grant #653 stamped never applies to it.
//
// That is not a defect in the chain — it is what a client certificate means —
// so the fix is to enroll the identity the certificate authenticates as
// (iam.IdentityStore.EnrollFleetNodeCertIdentity). These tests wire go-qrdb's REAL
// mTLS authenticator, RBAC authorizer and REST middleware against the record
// that enrollment produces, and pin both halves: an enrolled certificate is
// admitted, and a bearer NEVER lends its authority to a certificate principal.
// =============================================================================

const (
	certPrincipalTenant = "automatethethings"
	certPrincipalCN     = "agent-agent-04"
	// heartbeatRoute is the control-plane route the whole fleet was refused on.
	heartbeatRouteMethod  = "POST"
	heartbeatRoutePattern = "/api/v1/fleet/{nodeID}/heartbeat"
	heartbeatRoutePath    = "/api/v1/fleet/agent-automatethethings-agent-agent-04/heartbeat"
)

// agentPeerCert mints an agent client certificate shaped exactly like the one
// the deploy service issues: Organization = tenant, and the SPIFFE SAN
// spiffe://<tenant>/user/<CN> paired with OU = tenant, as go-xkms's CA stamps
// them. go-qrdb derives the identity's tenant from that agreeing pair.
func agentPeerCert(t *testing.T, tenantID, commonName string) *x509.Certificate {
	t.Helper()

	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	spiffeID, err := url.Parse("spiffe://" + tenantID + "/user/" + commonName)
	if err != nil {
		t.Fatalf("parse spiffe id: %v", err)
	}
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(20260908),
		Subject: pkix.Name{
			CommonName:         commonName,
			Organization:       []string{tenantID},
			OrganizationalUnit: []string{tenantID},
		},
		NotBefore:   time.Now().Add(-time.Hour),
		NotAfter:    time.Now().Add(24 * time.Hour),
		ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth},
		URIs:        []*url.URL{spiffeID},
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatalf("create certificate: %v", err)
	}
	cert, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatalf("parse certificate: %v", err)
	}
	return cert
}

// enrolledAgentUser runs the REAL enrollment the register endpoint performs
// and projects the record it wrote into go-qrdb's user type through the
// production converter, so this test is driven by what enrollment actually
// writes rather than by a hand-built approximation of it.
func enrolledAgentUser(t *testing.T, cert *x509.Certificate) *user.User {
	t.Helper()

	records := iam.NewMemoryStorage()
	store := iam.NewIdentityStore(iam.IdentityStoreOptions{
		UserStorage: records,
		RoleStorage: records,
	})
	enrolled, err := store.EnrollFleetNodeCertIdentity(t.Context(), certPrincipalTenant,
		iam.FleetNodeCertIdentityOptions{
			Certificate: cert,
			Permissions: iam.FleetRuntimeGrant(),
		})
	if err != nil {
		t.Fatalf("EnrollFleetNodeCertIdentity() error = %v", err)
	}
	core, err := qrdbadapter.FromIAMUser(enrolled)
	if err != nil {
		t.Fatalf("FromIAMUser() error = %v", err)
	}
	return &core
}

// certPrincipalHandler wires go-qrdb's REAL mTLS authenticator, RBAC
// authorizer and REST middleware over the heartbeat route, with the quicbaas
// module's bearer fallback attached exactly as the unified server attaches it.
func certPrincipalHandler(t *testing.T, users map[string]*user.User) http.Handler {
	t.Helper()

	grant := make([]permission.Permission, 0, len(iam.FleetRuntimeGrant()))
	for _, granted := range iam.FleetRuntimeGrant() {
		grant = append(grant, permission.Permission(granted))
	}
	roles := &fakeRoleServicer{perms: map[string][]permission.Permission{
		iam.FleetRuntimeRoleID(certPrincipalTenant): grant,
	}}

	log, err := logger.New(logger.LogConfig{Level: "error", Format: "json", Output: "stdout"})
	if err != nil {
		t.Fatal(err)
	}
	restSrv, err := restapi.NewServer(testutil.NewMockQuicBaaS(), restapi.DefaultConfig(), log)
	if err != nil {
		t.Fatal(err)
	}
	// A bearer for a DIFFERENT principal — another tenant's application —
	// carrying the full agent runtime grant. It is the credential a caller
	// would present to try to lend authority to a certificate principal.
	restSrv.SetModuleTokenAuthorizer(&fakeTokenAuthorizer{tokens: map[string]fakeToken{
		"other-app-token": {
			subject: "app_automatethethings_other-agent",
			tenant:  certPrincipalTenant,
			scopes:  iam.FleetRuntimeGrant(),
		},
	}})

	m, err := NewModule(
		func() service.QuicBaaS { return nil },
		log,
		WithRESTServer(func() *restapi.Server { return restSrv }),
	)
	if err != nil {
		t.Fatal(err)
	}

	am := middleware.New(rbac.New(roles),
		middleware.WithAuthenticator(qrdbauth.NewMTLSAuthenticator()),
		middleware.WithRoleResolver(roles),
		middleware.WithUserService(&fakeUserServicer{users: users}),
	)
	routes := middleware.NewRESTRouteRegistry()
	routes.Register(heartbeatRouteMethod, heartbeatRoutePattern,
		permission.Permission(iam.FleetScopeControlHeartbeat))
	routes.SetFallback(heartbeatRouteMethod, heartbeatRoutePattern, m)

	return am.RESTMiddleware(routes, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
}

// heartbeatRequest builds a heartbeat exactly as the daemon sends it.
func heartbeatRequest(peer *x509.Certificate, bearer string) *http.Request {
	req := httptest.NewRequest(heartbeatRouteMethod, heartbeatRoutePath, nil)
	if peer != nil {
		req.TLS = &tls.ConnectionState{
			PeerCertificates: []*x509.Certificate{peer},
			VerifiedChains:   [][]*x509.Certificate{{peer}},
		}
	}
	if bearer != "" {
		req.Header.Set("Authorization", "Bearer "+bearer)
	}
	return req
}

// TestCertPrincipal_EnrolledCertificateIsAuthorized proves the fix end to end
// through go-qrdb's own pipeline: the daemon presents its certificate, qrdb
// authenticates it as the certificate's subject, resolves that principal's
// roles from the record enrollment wrote, and admits the heartbeat.
func TestCertPrincipal_EnrolledCertificateIsAuthorized(t *testing.T) {
	cert := agentPeerCert(t, certPrincipalTenant, certPrincipalCN)
	handler := certPrincipalHandler(t, map[string]*user.User{
		certPrincipalTenant + "/" + certPrincipalCN: enrolledAgentUser(t, cert),
	})

	w := httptest.NewRecorder()
	handler.ServeHTTP(w, heartbeatRequest(cert, ""))
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body: %s)", w.Code, w.Body.String())
	}
}

// TestCertPrincipal_UnenrolledCertificateIsRefused reproduces the live defect:
// with no record for the certificate's subject, go-qrdb v0.47's mTLS
// authenticator establishes no identity at all, and with no other credential
// the request is refused as unauthenticated. It is the neutralization guard
// for the enrollment — remove it and this is what the fleet gets.
func TestCertPrincipal_UnenrolledCertificateIsRefused(t *testing.T) {
	cert := agentPeerCert(t, certPrincipalTenant, certPrincipalCN)
	handler := certPrincipalHandler(t, map[string]*user.User{})

	w := httptest.NewRecorder()
	handler.ServeHTTP(w, heartbeatRequest(cert, ""))
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401 for a certificate with no enrolled principal (body: %s)",
			w.Code, w.Body.String())
	}
}

// TestCertPrincipal_ABearerNeverEscalatesACertificatePrincipal is the negative
// test the case never had. A request carrying BOTH an enrolled peer
// certificate whose principal holds no grant and a bearer for a DIFFERENT
// application — one holding the full agent runtime grant — is still refused:
// the certificate decides who the caller is, and the module's bearer fallback
// is not consulted at all when qrdb's own chain authenticates. No scope from a
// token another principal was issued can reach this decision.
func TestCertPrincipal_ABearerNeverEscalatesACertificatePrincipal(t *testing.T) {
	cert := agentPeerCert(t, certPrincipalTenant, certPrincipalCN)
	ungranted := enrolledAgentUser(t, cert)
	ungranted.RoleIDs = nil
	handler := certPrincipalHandler(t, map[string]*user.User{
		certPrincipalTenant + "/" + certPrincipalCN: ungranted,
	})

	w := httptest.NewRecorder()
	handler.ServeHTTP(w, heartbeatRequest(cert, "other-app-token"))
	if w.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403: another principal's bearer must not authorize a certificate principal (body: %s)",
			w.Code, w.Body.String())
	}
}

// TestCertPrincipal_TheCertificateDecidesEvenWhenABearerIsPresent is the same
// statement from the other side: an ENROLLED certificate is admitted while
// carrying a bearer whose own grant is irrelevant to the decision. Authority
// follows the certificate, in both directions, which is why the grant has to
// be conferred on the certificate principal.
func TestCertPrincipal_TheCertificateDecidesEvenWhenABearerIsPresent(t *testing.T) {
	cert := agentPeerCert(t, certPrincipalTenant, certPrincipalCN)
	handler := certPrincipalHandler(t, map[string]*user.User{
		certPrincipalTenant + "/" + certPrincipalCN: enrolledAgentUser(t, cert),
	})

	w := httptest.NewRecorder()
	handler.ServeHTTP(w, heartbeatRequest(cert, "other-app-token"))
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body: %s)", w.Code, w.Body.String())
	}
}

// TestCertPrincipal_BearerOnlyStillUsesTheApplicationGrant guards the path
// go-quicbaas#653 fixed: a caller presenting NO client certificate still
// reaches the module's bearer fallback and is authorized from the scopes its
// application was granted. The certificate enrollment must not have displaced
// it.
func TestCertPrincipal_BearerOnlyStillUsesTheApplicationGrant(t *testing.T) {
	handler := certPrincipalHandler(t, map[string]*user.User{})

	w := httptest.NewRecorder()
	handler.ServeHTTP(w, heartbeatRequest(nil, "other-app-token"))
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 for a bearer holding the grant and presenting no certificate (body: %s)",
			w.Code, w.Body.String())
	}
}

// TestCertPrincipal_NoCredentialIsRefused keeps the fail-closed floor.
func TestCertPrincipal_NoCredentialIsRefused(t *testing.T) {
	handler := certPrincipalHandler(t, map[string]*user.User{})

	w := httptest.NewRecorder()
	handler.ServeHTTP(w, heartbeatRequest(nil, ""))
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401 (body: %s)", w.Code, w.Body.String())
	}
}

// TestCertPrincipal_AuthMethodMatchesTheAuthenticator pins the permitted-method
// literal to the authenticator that has to satisfy it. go-qrdb enforces a
// user's AuthMethods policy against the NAME of the authenticator that
// authenticated the request, so an enrollment writing any other spelling would
// authenticate the fleet and then refuse it — a 401, not even a 403.
func TestCertPrincipal_AuthMethodMatchesTheAuthenticator(t *testing.T) {
	if got := qrdbauth.NewMTLSAuthenticator().Name(); got != iam.AuthMethodMTLS {
		t.Fatalf("iam.AuthMethodMTLS = %q, but go-qrdb's mTLS authenticator is named %q",
			iam.AuthMethodMTLS, got)
	}
}

// TestCertPrincipal_AWithdrawnMethodPolicyRefuses proves the policy the
// enrollment writes is enforced rather than decorative: the same enrolled
// identity, restricted to a method it did not present, is refused. That is
// what makes AuthMethods a control on a machine identity — it cannot be
// carried into a human login ceremony.
func TestCertPrincipal_AWithdrawnMethodPolicyRefuses(t *testing.T) {
	cert := agentPeerCert(t, certPrincipalTenant, certPrincipalCN)
	enrolled := enrolledAgentUser(t, cert)
	enrolled.AuthMethods = []string{"webauthn"}

	handler := certPrincipalHandler(t, map[string]*user.User{
		certPrincipalTenant + "/" + certPrincipalCN: enrolled,
	})

	w := httptest.NewRecorder()
	handler.ServeHTTP(w, heartbeatRequest(cert, ""))
	if w.Code == http.StatusOK {
		t.Fatal("an identity whose policy withholds mTLS must not be admitted on a certificate")
	}
}
