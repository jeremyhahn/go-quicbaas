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
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/iam"
	"github.com/jeremyhahn/go-quicbaas/pkg/iam/iamtest"
	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
	"github.com/jeremyhahn/go-quicbaas/pkg/testutil"
)

// =============================================================================
// go-quicbaas #644 — the credentials POST /api/v1/fleet/enroll hands out
// must be the credentials POST /oauth2/token accepts.
//
// The burn-in fleet could enroll and then never authenticate: every agent got
// 401 invalid_client on its first token request, on /oauth2/token and
// /oauth2/token-mtls alike. These tests run the real register handler over the
// real BackendStorage on top of iamtest.TenantRoutingKV -- the double that
// mirrors production's per-tenant KVStore routing -- and then resolve the
// issued credential exactly the way the token endpoint does. A flat store
// cannot reproduce the defect, because the defect IS the two namespaces.
// =============================================================================

const agentOAuthTestTenant = "automatethethings"

// agentOAuthFixture is a register handler wired to a tenant-routed IAM store.
type agentOAuthFixture struct {
	server  *Server
	storage iam.AppStorage
	kv      *iamtest.TenantRoutingKV

	// identity is the storage the certificate-principal enrollment writes
	// through (go-quicbaas#663).
	identity *iam.MemoryStorage
}

// newAgentOAuthFixture builds the fixture with the tenant barrier-isolated, the
// production shape for a burn-in tenant.
func newAgentOAuthFixture(t *testing.T) *agentOAuthFixture {
	t.Helper()

	kv := iamtest.NewTenantRoutingKV()
	kv.BarrierTenant(agentOAuthTestTenant)
	storage := iam.NewBackendStorage(kv)
	appMgr := iam.NewAppManager(storage, &agentRegisterTestTenantManager{})

	identityStore, identityRecords := newAgentRegisterTestIdentityStore()
	wrapper := &agentRegisterTestService{
		MockQuicBaaS:    testutil.NewMockQuicBaaS(),
		appManager:      appMgr,
		appStorage:      storage,
		identityStore:   identityStore,
		identityRecords: identityRecords,
	}

	log, err := logger.New(logger.LogConfig{Level: "error", Format: "json"})
	require.NoError(t, err)

	cfg := DefaultConfig()
	cfg.Address = ":0"
	srv, err := NewServer(wrapper, cfg, log)
	require.NoError(t, err)
	srv.SetEventIngestURL("https://198.51.100.91:19090")

	return &agentOAuthFixture{server: srv, storage: storage, kv: kv, identity: identityRecords}
}

// agentOAuthSignedPeerCert mints a REAL self-signed agent certificate, parsed
// from its DER so cert.Raw is populated. The mTLS client is pinned by the RFC
// 8705 thumbprint, which is a digest of exactly those bytes, so a template-only
// certificate cannot tell two enrollments apart.
func agentOAuthSignedPeerCert(t *testing.T, tenantID, commonName string) *x509.Certificate {
	t.Helper()

	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	require.NoError(t, err)

	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	require.NoError(t, err)

	tmpl := &x509.Certificate{
		SerialNumber: serial,
		Subject: pkix.Name{
			CommonName:         commonName,
			Organization:       []string{tenantID},
			OrganizationalUnit: []string{fleetRegistrarOU},
		},
		NotBefore:   time.Now().Add(-time.Hour),
		NotAfter:    time.Now().Add(time.Hour),
		ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth},
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, pub, priv)
	require.NoError(t, err)

	cert, err := x509.ParseCertificate(der)
	require.NoError(t, err)
	return cert
}

// registerWithCert drives one POST /api/v1/fleet/enroll with a caller-supplied
// peer certificate.
func (f *agentOAuthFixture) registerWithCert(t *testing.T, peer *x509.Certificate) *fleetEnrollResponse {
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
	require.Equal(t, http.StatusCreated, w.Code, "register body: %s", w.Body.String())

	var resp fleetEnrollResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	return &resp
}

// TestHandleAgentRegister_RepinsTheMTLSClientToTheCurrentCertificate covers the
// certificate-renewal path. The mTLS client is pinned by thumbprint; leaving the
// FIRST enrollment's thumbprint pinned locks a daemon that renewed its
// certificate out of /oauth2/token-mtls for good.
func TestHandleAgentRegister_RepinsTheMTLSClientToTheCurrentCertificate(t *testing.T) {
	f := newAgentOAuthFixture(t)

	firstCert := agentOAuthSignedPeerCert(t, agentOAuthTestTenant, "agent-gpu-host")
	renewedCert := agentOAuthSignedPeerCert(t, agentOAuthTestTenant, "agent-gpu-host")
	require.NotEqual(t, iam.CertThumbprintS256(firstCert), iam.CertThumbprintS256(renewedCert),
		"the two certificates must differ for this test to mean anything")

	first := f.registerWithCert(t, firstCert)
	second := f.registerWithCert(t, renewedCert)
	require.Equal(t, first.MTLSClientID, second.MTLSClientID)

	ctx := loginContextWithTenant(context.Background(), agentOAuthTestTenant)
	app, err := f.storage.GetAppByTenant(ctx, agentOAuthTestTenant, second.MTLSClientID)
	require.NoError(t, err)
	require.Equal(t, iam.CertThumbprintS256(renewedCert), app.MTLSCertThumbprint,
		"the mTLS client is still pinned to the superseded certificate")
}

// register drives one POST /api/v1/fleet/enroll with the agent's mTLS peer
// certificate and returns the decoded response.
func (f *agentOAuthFixture) register(t *testing.T, commonName string) *fleetEnrollResponse {
	t.Helper()

	body, err := json.Marshal(map[string]any{
		"tenant_id": agentOAuthTestTenant,
		"hostname":  "burnin-node",
		"bind_addr": "0.0.0.0:19091",
	})
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/fleet/enroll", strings.NewReader(string(body)))
	req.TLS = &tls.ConnectionState{
		PeerCertificates: []*x509.Certificate{
			agentRegisterTestPeerCert(t, agentOAuthTestTenant, commonName),
		},
	}
	w := httptest.NewRecorder()
	f.server.HandleFleetEnroll(w, req)
	require.Equal(t, http.StatusCreated, w.Code, "register body: %s", w.Body.String())

	var resp fleetEnrollResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	return &resp
}

// tokenEndpointContext reproduces the storage context POST /oauth2/token binds
// before it resolves a client: the tenant encoded in the presented client id.
// This is production code (iam_oidc.go Token), not a test approximation.
func tokenEndpointContext(clientID string) context.Context {
	ctx := context.Background()
	if cidTenant := tenantFromOAuthClientID(clientID); cidTenant != "" {
		ctx = loginContextWithTenant(ctx, cidTenant)
	}
	return ctx
}

// TestHandleAgentRegister_IssuesCredentialsTheTokenEndpointAccepts is the #644
// regression: the round trip an enrolling agent performs, in order.
func TestHandleAgentRegister_IssuesCredentialsTheTokenEndpointAccepts(t *testing.T) {
	f := newAgentOAuthFixture(t)
	resp := f.register(t, "agent-gpu-host")

	require.NotEmpty(t, resp.ClientID, "register returned no client_id")
	require.NotEmpty(t, resp.ClientSecret, "register returned no client_secret")

	ctx := tokenEndpointContext(resp.ClientID)
	valid, err := f.storage.ValidateClientSecret(ctx, resp.ClientID, resp.ClientSecret)
	require.NoError(t, err, "the token endpoint could not resolve the client it was just handed")
	require.True(t, valid, "the token endpoint refused the secret the register endpoint issued")
}

// TestHandleAgentRegister_IssuesAnMTLSClientTheTokenEndpointResolves covers the
// second credential the same request mints: /oauth2/token-mtls scopes its
// context from the certificate's tenant, not from the client id.
func TestHandleAgentRegister_IssuesAnMTLSClientTheTokenEndpointResolves(t *testing.T) {
	f := newAgentOAuthFixture(t)
	resp := f.register(t, "agent-gpu-host")

	require.NotEmpty(t, resp.MTLSClientID, "register returned no mtls_client_id")

	ctx := loginContextWithTenant(context.Background(), agentOAuthTestTenant)
	app, err := f.storage.GetAppByTenant(ctx, agentOAuthTestTenant, resp.MTLSClientID)
	require.NoError(t, err, "the mTLS token endpoint could not resolve the client it was just handed")
	require.Equal(t, iam.AuthMethodSelfSignedTLSClientAuth, app.TokenEndpointAuthMethod)
}

// TestHandleAgentRegister_IsIdempotentForTheSameIdentity is the second half of
// #644: an agent that retries enrollment minted a NEW client id every time
// while the mTLS app and the fleet registry both reported already_exists, so a
// looping daemon accumulated orphan OAuth2 records that all failed. A repeat
// registration must hand back the SAME client, with credentials that work.
func TestHandleAgentRegister_IsIdempotentForTheSameIdentity(t *testing.T) {
	f := newAgentOAuthFixture(t)

	first := f.register(t, "agent-gpu-host")
	second := f.register(t, "agent-gpu-host")

	require.Equal(t, first.ClientID, second.ClientID,
		"re-registration minted a new client id, orphaning the first")
	require.Equal(t, first.MTLSClientID, second.MTLSClientID,
		"re-registration dropped the mtls client id")
	require.NotEmpty(t, second.ClientSecret, "re-registration returned no client_secret")

	// The credential from the LATEST registration is the usable one: the agent
	// has just overwritten its stored secret with it.
	ctx := tokenEndpointContext(second.ClientID)
	valid, err := f.storage.ValidateClientSecret(ctx, second.ClientID, second.ClientSecret)
	require.NoError(t, err)
	require.True(t, valid, "the credential from a repeat registration is not usable")

	// Exactly one OAuth2 client per credential kind, no orphans.
	apps, err := f.storage.ListApps(ctx, agentOAuthTestTenant, iam.ListOptions{})
	require.NoError(t, err)
	require.Len(t, apps, 2, "expected the secret client and the mTLS client, got %d", len(apps))
}

// TestHandleAgentRegister_KeepsAgentsOfOtherTenantsOut is the error path: a
// certificate claiming one tenant cannot enroll into another, and nothing is
// written when it tries.
func TestHandleAgentRegister_KeepsAgentsOfOtherTenantsOut(t *testing.T) {
	f := newAgentOAuthFixture(t)

	body, err := json.Marshal(map[string]any{
		"tenant_id": agentOAuthTestTenant,
		"hostname":  "burnin-node",
		"bind_addr": "0.0.0.0:19091",
	})
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/fleet/enroll", strings.NewReader(string(body)))
	req.TLS = &tls.ConnectionState{
		PeerCertificates: []*x509.Certificate{
			agentRegisterTestPeerCert(t, "some-other-tenant", "agent-gpu-host"),
		},
	}
	w := httptest.NewRecorder()
	f.server.HandleFleetEnroll(w, req)

	require.Equal(t, http.StatusForbidden, w.Code, "body: %s", w.Body.String())

	ctx := loginContextWithTenant(context.Background(), agentOAuthTestTenant)
	apps, err := f.storage.ListApps(ctx, agentOAuthTestTenant, iam.ListOptions{})
	require.NoError(t, err)
	require.Empty(t, apps, "a refused registration still wrote OAuth2 clients")
}
