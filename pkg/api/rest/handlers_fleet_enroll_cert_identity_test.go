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
// service's: Organization = tenant, OU = agents, ClientAuth EKU, and the
// tenant-scoped SPIFFE SAN go-qrdb derives the identity's tenant from.
func agentSPIFFEPeerCert(t *testing.T, certOrg, spiffeTenant, commonName string) *x509.Certificate {
	t.Helper()

	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	require.NoError(t, err)

	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	require.NoError(t, err)

	spiffeID, err := url.Parse("spiffe://burnin.example.internal/tenant/" +
		spiffeTenant + "/user/" + commonName)
	require.NoError(t, err)

	tmpl := &x509.Certificate{
		SerialNumber: serial,
		Subject: pkix.Name{
			CommonName:         commonName,
			Organization:       []string{certOrg},
			OrganizationalUnit: []string{fleetRegistrarOU},
		},
		NotBefore:   time.Now().Add(-time.Hour),
		NotAfter:    time.Now().Add(24 * time.Hour),
		ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth},
		URIs:        []*url.URL{spiffeID},
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

	// The Organization claim says the registering tenant (so the existing
	// cross-check passes) while the SPIFFE SAN — the value go-qrdb actually
	// authenticates with — names another.
	cert := agentSPIFFEPeerCert(t, agentOAuthTestTenant, "other-tenant", "agent-agent-04")

	w := f.postRegister(t, cert)
	require.Equal(t, http.StatusForbidden, w.Code, "register body: %s", w.Body.String())
	require.Contains(t, w.Body.String(), "tenant_mismatch")

	users, err := f.identity.ListUsers(context.Background(), agentOAuthTestTenant, iam.ListOptions{})
	require.NoError(t, err)
	require.Empty(t, users, "a refused registration must enroll no identity")
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
