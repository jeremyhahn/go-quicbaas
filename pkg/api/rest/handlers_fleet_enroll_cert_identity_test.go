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

package rest

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"math/big"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"

	"github.com/jeremyhahn/go-quicbaas/pkg/iam"
	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
)

// =============================================================================
// go-quicbaas#663 — registration must confer the grant on the identity the
// daemon's CERTIFICATE authenticates as, not only on its OAuth2 application.
//
// go-qrdb authenticates any request carrying a client certificate as the
// certificate's subject and resolves that principal's authority from its user
// record. The daemon presents its certificate on every call, so the #653
// application grant never reached the principal that actually arrived, and
// every heartbeat was refused 403.
// =============================================================================

// agentSPIFFEPeerCert mints an agent certificate shaped like the deploy
// service's: OU = tenant (CA m779 62(a)), ClientAuth EKU, and the
// tenant-scoped service-kind SPIFFE SAN go-qrdb derives the identity's tenant
// from.
func agentSPIFFEPeerCert(t *testing.T, ouTenant, spiffeTenant, commonName string) *x509.Certificate {
	t.Helper()
	return agentSPIFFEPeerCertOfKind(t, ouTenant, spiffeTenant, qrdbsdk.SpiffeKindService, commonName)
}

// agentSPIFFEPeerCertOfKind mints a client certificate whose first OU names
// ouTenant (go-xkms v0.57 stamps the tenant there, CA m779 62(a)) and whose
// SPIFFE SAN names spiffeTenant with the given identity kind.
func agentSPIFFEPeerCertOfKind(t *testing.T, ouTenant, spiffeTenant, kind, commonName string) *x509.Certificate {
	t.Helper()
	return selfSignedPeerCert(t, ouTenant, commonName, spiffeURIs(t, spiffeTenant, kind, commonName))
}

// agentSPIFFEURIs returns the agent SPIFFE SAN (spiffe://<tenant>/svc/<name>)
// a fleet node certificate carries, the proof of kind enrolment requires.
func agentSPIFFEURIs(t *testing.T, tenantID, commonName string) []*url.URL {
	t.Helper()
	return spiffeURIs(t, tenantID, fleetNodeSPIFFEKind, commonName)
}

// spiffeURIs returns the single SPIFFE SAN spiffe://<tenant>/<kind>/<name>.
func spiffeURIs(t *testing.T, tenantID, kind, commonName string) []*url.URL {
	t.Helper()
	spiffeID, err := url.Parse("spiffe://" + tenantID + "/" + kind + "/" + commonName)
	require.NoError(t, err)
	return []*url.URL{spiffeID}
}

// peerCertWithoutSPIFFE mints a client certificate whose subject OU names the
// tenant but which carries no SPIFFE SAN, so nothing in it proves the agent
// kind.
func peerCertWithoutSPIFFE(t *testing.T, ouTenant, commonName string) *x509.Certificate {
	t.Helper()
	return selfSignedPeerCert(t, ouTenant, commonName, nil)
}

// selfSignedPeerCert mints a self-signed client-auth certificate with the given
// subject OU, common name and URI SANs.
func selfSignedPeerCert(t *testing.T, ouTenant, commonName string, uris []*url.URL) *x509.Certificate {
	t.Helper()
	cert := selfSignedPeerCertWithOUs(t, []string{ouTenant}, commonName, uris)
	recordFleetTestCert(t, ouTenant, cert)
	return cert
}

// selfSignedPeerCertWithOUs mints a client certificate whose Subject carries
// the given Organizational Units, round-tripped through DER so the OU order is
// the one a verifier actually sees.
func selfSignedPeerCertWithOUs(t *testing.T, ous []string, commonName string, uris []*url.URL) *x509.Certificate {
	t.Helper()

	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	require.NoError(t, err)

	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	require.NoError(t, err)

	tmpl := &x509.Certificate{
		SerialNumber: serial,
		Subject: pkix.Name{
			CommonName:         commonName,
			OrganizationalUnit: ous,
		},
		NotBefore:   time.Now().Add(-time.Hour),
		NotAfter:    time.Now().Add(24 * time.Hour),
		ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth},
		URIs:        uris,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, pub, priv)
	require.NoError(t, err)

	cert, err := x509.ParseCertificate(der)
	require.NoError(t, err)
	return cert
}

// postRegister drives one POST /api/v1/fleet/enroll and returns the
// recorder, so a test can assert a refusal as well as a success.
func (f *agentOAuthFixture) postRegister(t *testing.T, peer *x509.Certificate) *httptest.ResponseRecorder {
	t.Helper()

	body, err := json.Marshal(map[string]any{
		"tenant_id": agentOAuthTestTenant,
		"hostname":  "burnin-node",
		"bind_addr": "0.0.0.0:19091",
	})
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/fleet/enroll", strings.NewReader(string(body)))
	req.TLS = &tls.ConnectionState{PeerCertificates: []*x509.Certificate{peer}}
	w := httptest.NewRecorder()
	f.server.HandleFleetEnroll(w, req)
	return w
}

// TestHandleAgentRegister_EnrollsTheCertificatePrincipal is the #663
// regression: after registration the identity the certificate authenticates as
// exists, is active, is restricted to mTLS and holds the agent runtime grant.
func TestHandleAgentRegister_EnrollsTheCertificatePrincipal(t *testing.T) {
	f := newAgentOAuthFixture(t)
	cert := agentSPIFFEPeerCert(t, agentOAuthTestTenant, agentOAuthTestTenant, "agent-agent-04")

	w := f.postRegister(t, cert)
	require.Equal(t, http.StatusCreated, w.Code, "register body: %s", w.Body.String())

	ctx := context.Background()
	enrolled, err := f.identity.GetUserByUsername(ctx, agentOAuthTestTenant, "agent-agent-04")
	require.NoError(t, err, "registration left the certificate principal with no identity at all")
	require.True(t, enrolled.CanAuthenticate())
	require.Equal(t, []string{iam.AuthMethodMTLS}, enrolled.AuthMethods)
	require.Equal(t, []string{iam.FleetRuntimeRoleID(agentOAuthTestTenant)}, enrolled.Roles)
	require.Len(t, enrolled.CertBindings, 1)
	require.Equal(t, qrdbsdk.ComputeSPKIThumbprint(cert), enrolled.CertBindings[0].Thumbprint,
		"the identity must name the exact credential it was enrolled with")

	role, err := f.identity.GetRole(ctx, agentOAuthTestTenant, iam.FleetRuntimeRoleID(agentOAuthTestTenant))
	require.NoError(t, err)
	require.ElementsMatch(t, iam.FleetRuntimeGrant(), role.Permissions,
		"the certificate principal must hold the same grant as the daemon's application, and nothing else")
}

// TestHandleAgentRegister_EnrollmentIsIdempotent proves a daemon that
// re-registers on every restart converges rather than accumulating identities.
func TestHandleAgentRegister_EnrollmentIsIdempotent(t *testing.T) {
	f := newAgentOAuthFixture(t)
	cert := agentSPIFFEPeerCert(t, agentOAuthTestTenant, agentOAuthTestTenant, "agent-agent-04")

	require.Equal(t, http.StatusCreated, f.postRegister(t, cert).Code)
	require.Equal(t, http.StatusCreated, f.postRegister(t, cert).Code)

	users, err := f.identity.ListUsers(context.Background(), agentOAuthTestTenant, iam.ListOptions{})
	require.NoError(t, err)
	require.Len(t, users, 1, "re-registration minted a second identity for one daemon")
}

// TestHandleAgentRegister_RefusesACertificateThatAuthenticatesElsewhere is the
// negative half of the enrollment. A certificate whose SPIFFE SAN authenticates
// it into a DIFFERENT tenant than the registration would be enrolled where the
// authenticator never looks — and would put an identity in a tenant its holder
// never proved a claim to. It is refused, and nothing is written.
func TestHandleAgentRegister_RefusesACertificateThatAuthenticatesElsewhere(t *testing.T) {
	f := newAgentOAuthFixture(t)

	// The OU tenant says the registering tenant (so the request cross-check
	// passes) while the SPIFFE SAN — the value go-qrdb actually
	// authenticates with — names another.
	cert := agentSPIFFEPeerCert(t, agentOAuthTestTenant, "other-tenant", "agent-agent-04")

	w := f.postRegister(t, cert)
	require.Equal(t, http.StatusForbidden, w.Code, "register body: %s", w.Body.String())
	require.Contains(t, w.Body.String(), "tenant_mismatch")

	users, err := f.identity.ListUsers(context.Background(), agentOAuthTestTenant, iam.ListOptions{})
	require.NoError(t, err)
	require.Empty(t, users, "a refused registration must enroll no identity")
}

// A certificate issued under go-xkms v0.57 carries its tenant in the first OU
// and an agent (service-kind) SPIFFE SAN for the same tenant. Registration
// must accept it (CA m779 62(a)).
func TestHandleAgentRegister_AcceptsTheTenantInOUAgentCertificate(t *testing.T) {
	f := newAgentOAuthFixture(t)
	cert := agentSPIFFEPeerCert(t, agentOAuthTestTenant, agentOAuthTestTenant, "agent-agent-04")
	require.Equal(t, agentOAuthTestTenant, cert.Subject.OrganizationalUnit[0])

	w := f.postRegister(t, cert)
	require.Equal(t, http.StatusCreated, w.Code, "register body: %s", w.Body.String())
}

// A user-kind certificate for the right tenant is not an agent's: it must
// not register as a fleet node.
func TestHandleAgentRegister_RefusesANonAgentSPIFFEKind(t *testing.T) {
	f := newAgentOAuthFixture(t)
	cert := agentSPIFFEPeerCertOfKind(t, agentOAuthTestTenant, agentOAuthTestTenant,
		qrdbsdk.SpiffeKindUser, "agent-agent-04")

	w := f.postRegister(t, cert)
	require.Equal(t, http.StatusUnauthorized, w.Code, "register body: %s", w.Body.String())
	require.Contains(t, w.Body.String(), "invalid_client")

	users, err := f.identity.ListUsers(context.Background(), agentOAuthTestTenant, iam.ListOptions{})
	require.NoError(t, err)
	require.Empty(t, users, "a refused registration must enroll no identity")
}

// A trusted certificate whose OU names the tenant but which carries no SPIFFE
// SAN proves no agent kind (ruling 62(a)): it is refused 401 invalid_client,
// and neither an identity nor the runtime grant is written.
func TestHandleAgentRegister_RefusesACertificateWithNoSPIFFESAN(t *testing.T) {
	f := newAgentOAuthFixture(t)
	cert := peerCertWithoutSPIFFE(t, agentOAuthTestTenant, "agent-agent-04")

	w := f.postRegister(t, cert)
	require.Equal(t, http.StatusUnauthorized, w.Code, "register body: %s", w.Body.String())
	require.Contains(t, w.Body.String(), "invalid_client")

	ctx := context.Background()
	users, err := f.identity.ListUsers(ctx, agentOAuthTestTenant, iam.ListOptions{})
	require.NoError(t, err)
	require.Empty(t, users, "a refused registration must enroll no identity")

	_, err = f.identity.GetRole(ctx, agentOAuthTestTenant, iam.FleetRuntimeRoleID(agentOAuthTestTenant))
	require.Error(t, err, "a refused registration must write no runtime grant")
}

// A trusted agent certificate whose OU names the cluster trust domain names no
// tenant: go-qrdb refuses to authenticate it, so registration refuses it 401
// invalid_client before anything is enrolled or granted.
func TestHandleAgentRegister_RefusesACertificateWhoseOUIsTheTrustDomain(t *testing.T) {
	f := newAgentOAuthFixture(t)
	f.server.config.SPIFFETrustDomain = agentOAuthTestTenant
	cert := agentSPIFFEPeerCert(t, agentOAuthTestTenant, agentOAuthTestTenant, "agent-agent-04")

	w := f.postRegister(t, cert)
	require.Equal(t, http.StatusUnauthorized, w.Code, "register body: %s", w.Body.String())
	require.Contains(t, w.Body.String(), "invalid_client")

	ctx := context.Background()
	users, err := f.identity.ListUsers(ctx, agentOAuthTestTenant, iam.ListOptions{})
	require.NoError(t, err)
	require.Empty(t, users, "a refused registration must enroll no identity")

	_, err = f.identity.GetRole(ctx, agentOAuthTestTenant, iam.FleetRuntimeRoleID(agentOAuthTestTenant))
	require.Error(t, err, "a refused registration must write no runtime grant")
}

// testFleetTrustDomain is the cluster trust domain the fleetNodeCertTenant
// tests run under; no test tenant is named after it.
const testFleetTrustDomain = "cluster.test"

func TestFleetNodeCertTenant_RefusesAnOUThatIsTheTrustDomain(t *testing.T) {
	cert := agentSPIFFEPeerCert(t, testFleetTrustDomain, testFleetTrustDomain, "agent01")

	tenantID, err := fleetNodeCertTenant(cert, testFleetTrustDomain)
	var clusterOU *FleetNodeCertOUIsTrustDomainError
	require.ErrorAs(t, err, &clusterOU)
	require.Equal(t, testFleetTrustDomain, clusterOU.OU)
	require.ErrorIs(t, err, qrdbsdk.ErrClientCertificateTenantIsClusterTrustDomain,
		"the refusal must match the cause go-qrdb gives the same certificate")
	require.Empty(t, tenantID)
}

func TestFleetNodeCertTenant_AcceptsMatchingOUAndSPIFFETenant(t *testing.T) {
	cert := agentSPIFFEPeerCert(t, "acme", "acme", "agent01")

	tenantID, err := fleetNodeCertTenant(cert, testFleetTrustDomain)
	require.NoError(t, err)
	require.Equal(t, "acme", tenantID)
}

func TestFleetNodeCertTenant_RefusesOUThatDisagreesWithSPIFFE(t *testing.T) {
	cert := agentSPIFFEPeerCert(t, "acme", "other-tenant", "agent01")

	_, err := fleetNodeCertTenant(cert, testFleetTrustDomain)
	var mismatch *FleetNodeCertOUTenantMismatchError
	require.ErrorAs(t, err, &mismatch)
	require.Equal(t, "acme", mismatch.OU)
	require.Equal(t, "other-tenant", mismatch.SPIFFE)
}

// The tenant must be the certificate's only OU, as go-xkms v0.57+ issues it.
// Subject OUs share one RDN SET, which DER sorts, so a second OU listed after
// the tenant can come first on the wire and name the wrong tenant.
func TestFleetNodeCertTenant_SecondOUCanSortAheadOfTheTenant(t *testing.T) {
	uris := spiffeURIs(t, "automatethethings", qrdbsdk.SpiffeKindService, "agent01")
	cert := selfSignedPeerCertWithOUs(t, []string{"automatethethings", "agents"}, "agent01", uris)
	require.Equal(t, "agents", cert.Subject.OrganizationalUnit[0],
		"DER no longer reorders the OU SET; this test's premise is gone")

	_, err := fleetNodeCertTenant(cert, testFleetTrustDomain)
	var mismatch *FleetNodeCertOUTenantMismatchError
	require.ErrorAs(t, err, &mismatch)
	require.Equal(t, "agents", mismatch.OU)
	require.Equal(t, "automatethethings", mismatch.SPIFFE)
}

func TestFleetNodeCertTenant_AcceptsTheTenantAsTheOnlyOU(t *testing.T) {
	uris := spiffeURIs(t, "automatethethings", qrdbsdk.SpiffeKindService, "agent01")
	cert := selfSignedPeerCertWithOUs(t, []string{"automatethethings"}, "agent01", uris)

	tenantID, err := fleetNodeCertTenant(cert, testFleetTrustDomain)
	require.NoError(t, err)
	require.Equal(t, "automatethethings", tenantID)
}

func TestFleetNodeCertTenant_RefusesANonAgentKind(t *testing.T) {
	cert := agentSPIFFEPeerCertOfKind(t, "acme", "acme", qrdbsdk.SpiffeKindUser, "agent01")

	_, err := fleetNodeCertTenant(cert, testFleetTrustDomain)
	var notAgent *FleetNodeCertNotAgentError
	require.ErrorAs(t, err, &notAgent)
	require.Equal(t, qrdbsdk.SpiffeKindUser, notAgent.Kind)
}

func TestFleetNodeCertTenant_RefusesACertificateWithNoSPIFFESAN(t *testing.T) {
	cert := peerCertWithoutSPIFFE(t, "acme", "agent01")

	tenantID, err := fleetNodeCertTenant(cert, testFleetTrustDomain)
	var noSPIFFE *FleetNodeCertNoSPIFFEError
	require.ErrorAs(t, err, &noSPIFFE)
	require.Equal(t, "acme", noSPIFFE.OU)
	require.Empty(t, tenantID)
}

func TestFleetNodeCertTenant_RefusesACertificateWithNoTenantOU(t *testing.T) {
	cert := agentSPIFFEPeerCert(t, "", "acme", "agent01")

	_, err := fleetNodeCertTenant(cert, testFleetTrustDomain)
	require.ErrorIs(t, err, errInvalidPeerOU)
}

// TestHandleAgentRegister_FailsClosedWithoutAnIdentityStore proves the
// enrollment is not best-effort: a server that cannot confer the grant must not
// hand back credentials the control plane will refuse, because the response
// advertises that exact grant.
func TestHandleAgentRegister_FailsClosedWithoutAnIdentityStore(t *testing.T) {
	f := newAgentOAuthFixture(t)
	service, ok := f.server.service.(*agentRegisterTestService)
	require.True(t, ok)
	service.identityStore = nil

	w := f.postRegister(t, agentSPIFFEPeerCert(t, agentOAuthTestTenant, agentOAuthTestTenant, "agent-agent-04"))
	require.Equal(t, http.StatusInternalServerError, w.Code)
	require.Contains(t, w.Body.String(), "identity store")
}

// TestHandleAgentRegister_EnrollsOnlyTheCertificateItWasGiven proves a
// registration confers authority on ONE identity: the subject of the
// certificate presented. A second daemon's identity is untouched, so a stolen
// certificate cannot enroll — or re-grant — another daemon.
func TestHandleAgentRegister_EnrollsOnlyTheCertificateItWasGiven(t *testing.T) {
	f := newAgentOAuthFixture(t)

	require.Equal(t, http.StatusCreated,
		f.postRegister(t, agentSPIFFEPeerCert(t, agentOAuthTestTenant, agentOAuthTestTenant, "agent-agent-04")).Code)

	ctx := context.Background()
	if _, err := f.identity.GetUserByUsername(ctx, agentOAuthTestTenant, "agent-gpu-host"); err == nil {
		t.Fatal("registering one agent must not enroll another")
	}
}

// tenantCapturingRegistrar records the tenant bound to the context the
// register handler publishes under, plus the record's own tenant.
type tenantCapturingRegistrar struct {
	contextTenant string
	recordTenant  string
}

// RegisterFleetNode implements iam.FleetRegistrar.
func (r *tenantCapturingRegistrar) RegisterFleetNode(ctx context.Context, req *iam.FleetRegistrarRequest) error {
	r.contextTenant = orchestration.TenantFromContext(ctx)
	if r.contextTenant == "" {
		r.contextTenant = boundTenant(ctx)
	}
	r.recordTenant = req.TenantID
	return nil
}

// TestHandleAgentRegister_PublishesUnderAContextWithNoTenant is the other half
// of go-quicbaas#663, stated where it originates. POST /api/v1/fleet/enroll
// is a public route — the mTLS handshake IS the authentication — so no
// identity, and therefore no tenant, is ever bound to its context. The fleet
// registry write is made from here, while every read of the registry is made
// under the tenant the request names; the store routes by the CONTEXT tenant,
// so the two resolved to different namespaces until the registry began scoping
// by the record's own tenant (orchestration.fleetTenantContext).
func TestHandleAgentRegister_PublishesUnderAContextWithNoTenant(t *testing.T) {
	f := newAgentOAuthFixture(t)
	registrar := &tenantCapturingRegistrar{}
	f.server.SetFleetRegistrar(registrar)

	w := f.postRegister(t, agentSPIFFEPeerCert(t, agentOAuthTestTenant, agentOAuthTestTenant, "agent-agent-04"))
	require.Equal(t, http.StatusCreated, w.Code, "register body: %s", w.Body.String())

	require.Empty(t, registrar.contextTenant,
		"a public route carries no tenant; the registry cannot rely on one being present")
	require.Equal(t, agentOAuthTestTenant, registrar.recordTenant,
		"the record names the tenant, so the record must decide the namespace it lands in")
}
