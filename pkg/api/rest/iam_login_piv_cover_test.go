//go:build rest && iam

// Copyright (c) 2025 Jeremy Hahn
// Copyright (c) 2025 Automate The Things, LLC
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
	"crypto/ecdsa"
	"crypto/elliptic"
	cryptorand "crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/hex"
	"encoding/json"
	"errors"
	"math/big"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/iam"
	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
	"github.com/jeremyhahn/go-quicbaas/pkg/testutil"
)

const pivTrustDomain = "broker.example.com"

// pivFullMockService exposes a real identity store, OIDC provider and audit
// logger so the PIVLogin handler can run end to end.
type pivFullMockService struct {
	*testutil.MockQuicBaaS
	store       *iam.IdentityStore
	provider    *iam.OIDCProvider
	auditLogger iam.AuditLogger
}

func (m *pivFullMockService) GetIdentityStore(_ context.Context) (*iam.IdentityStore, error) {
	return m.store, nil
}

func (m *pivFullMockService) GetOIDCProvider(_ context.Context) (*iam.OIDCProvider, error) {
	return m.provider, nil
}

func (m *pivFullMockService) GetAuditLogger(_ context.Context) (iam.AuditLogger, error) {
	return m.auditLogger, nil
}

// pivSpiffeLeaf creates a client leaf cert carrying a tenant-scoped SPIFFE SAN
// signed by the supplied CA.
func pivSpiffeLeaf(t *testing.T, ca *x509.Certificate, caKey *ecdsa.PrivateKey, tenant string) *x509.Certificate {
	t.Helper()
	leafKey, err := ecdsa.GenerateKey(elliptic.P256(), cryptorand.Reader)
	require.NoError(t, err)
	spiffeURI, err := url.Parse("spiffe://" + pivTrustDomain + "/tenant/" + tenant + "/user/piv-user")
	require.NoError(t, err)
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(99),
		Subject:      pkix.Name{CommonName: "PIV User"},
		URIs:         []*url.URL{spiffeURI},
		NotBefore:    time.Now().Add(-time.Minute),
		NotAfter:     time.Now().Add(time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth},
	}
	der, err := x509.CreateCertificate(cryptorand.Reader, tmpl, ca, &leafKey.PublicKey, caKey)
	require.NoError(t, err)
	cert, err := x509.ParseCertificate(der)
	require.NoError(t, err)
	return cert
}

// setupPIVFullHandler wires a PIV-enabled handler with a registered user whose
// PIV thumbprint matches the supplied leaf certificate.
func setupPIVFullHandler(t *testing.T, leaf *x509.Certificate, tenant string, registerUser bool) (*IAMHandler, *captureAuditLogger, func()) {
	t.Helper()

	memStorage := iam.NewMemoryStorage()
	store := iam.NewIdentityStore(iam.IdentityStoreOptions{
		UserStorage:  memStorage,
		GroupStorage: memStorage,
		RoleStorage:  memStorage,
		Tenants:      &testTenantManager{storage: memStorage},
	})

	if registerUser {
		thumb := sha256.Sum256(leaf.Raw)
		require.NoError(t, memStorage.CreateUser(context.Background(), &iam.User{
			ID:                "piv-user",
			TenantID:          tenant,
			Email:             "piv@example.com",
			PIVCertThumbprint: hex.EncodeToString(thumb[:]),
		}))
	}

	privateKey, err := rsa.GenerateKey(cryptorand.Reader, 2048)
	require.NoError(t, err)
	provider, err := iam.NewOIDCProvider(&iam.OIDCConfig{
		Issuer:           "https://" + pivTrustDomain,
		SigningAlgorithm: "RS256",
		SigningKey:       privateKey,
		AccessTokenTTL:   15 * time.Minute,
		RefreshTokenTTL:  24 * time.Hour,
		IDTokenTTL:       15 * time.Minute,
		AuthCodeTTL:      10 * time.Minute,
	}, iam.OIDCDependencies{
		IdentityStore: store,
		AppStorage:    iam.NewBackendStorage(newIntrospectMemKV()),
		TokenStorage:  iam.NewBackendStorage(newIntrospectMemKV()),
	})
	require.NoError(t, err)

	auditLogger := &captureAuditLogger{}

	log, err := logger.New(logger.LogConfig{Level: "error", Format: "json", Output: "stdout"})
	require.NoError(t, err)

	mockSvc := &pivFullMockService{
		MockQuicBaaS: testutil.NewMockQuicBaaS(),
		store:        store,
		provider:     provider,
		auditLogger:  auditLogger,
	}
	cfg := DefaultConfig()
	cfg.Address = ":0"
	cfg.SPIFFETrustDomain = pivTrustDomain
	cfg.PIVClientCert.Enabled = true
	server, err := NewServer(mockSvc.MockQuicBaaS, cfg, log)
	require.NoError(t, err)
	server.service = mockSvc

	handler := NewIAMHandler(IAMHandlerDependencies{Server: server})
	cleanup := func() {
		_ = auditLogger.Close()
		server.Close()
	}
	return handler, auditLogger, cleanup
}

func pivRequestWithSpiffe(leaf *x509.Certificate) *http.Request {
	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login/piv", nil)
	req.TLS = &tls.ConnectionState{PeerCertificates: []*x509.Certificate{leaf}}
	return req
}

// A PIV smartcard login with a tenant-scoped SPIFFE cert resolving to a
// registered user mints hardware-attested tokens.
func TestPIVLogin_Success(t *testing.T) {
	ca, caKey := pivLoginTestCA(t, "PIV Root CA")
	leaf := pivSpiffeLeaf(t, ca, caKey, "tenant-piv")
	handler, audit, cleanup := setupPIVFullHandler(t, leaf, "tenant-piv", true)
	defer cleanup()

	w := httptest.NewRecorder()
	handler.PIVLogin(w, pivRequestWithSpiffe(leaf))

	require.Equal(t, http.StatusOK, w.Code, "body: %s", w.Body.String())
	var resp map[string]interface{}
	require.NoError(t, json.NewDecoder(w.Body).Decode(&resp))
	assert.NotEmpty(t, resp["access_token"])

	require.NotEmpty(t, audit.events)
	assert.Equal(t, "auth.login.piv.success", audit.events[len(audit.events)-1].Action)
}

// A presented cert whose thumbprint matches no registered user fails closed.
func TestPIVLogin_NoUserForCert(t *testing.T) {
	ca, caKey := pivLoginTestCA(t, "PIV Root CA")
	leaf := pivSpiffeLeaf(t, ca, caKey, "tenant-piv")
	handler, _, cleanup := setupPIVFullHandler(t, leaf, "tenant-piv", false) // no user
	defer cleanup()

	w := httptest.NewRecorder()
	handler.PIVLogin(w, pivRequestWithSpiffe(leaf))

	require.Equal(t, http.StatusUnauthorized, w.Code)
	assert.Contains(t, w.Body.String(), "no user found")
}

// A resolved user whose tenant differs from the cert-derived login tenant is
// rejected (defense-in-depth tenant binding).
func TestPIVLogin_TenantMismatch(t *testing.T) {
	ca, caKey := pivLoginTestCA(t, "PIV Root CA")
	leaf := pivSpiffeLeaf(t, ca, caKey, "tenant-piv")
	// Register the user under a DIFFERENT tenant than the cert's SPIFFE SAN.
	handler, _, cleanup := setupPIVFullHandler(t, leaf, "tenant-other", true)
	defer cleanup()

	w := httptest.NewRecorder()
	handler.PIVLogin(w, pivRequestWithSpiffe(leaf))

	require.Equal(t, http.StatusUnauthorized, w.Code)
	assert.Contains(t, w.Body.String(), "no user found")
}

// A cert with no SPIFFE SAN (tenant unresolvable, no HRD) fails closed.
func TestPIVLogin_TenantUnresolved(t *testing.T) {
	ca, caKey := pivLoginTestCA(t, "PIV Root CA")
	leaf := pivLoginTestLeaf(t, ca, caKey) // plain leaf, no SPIFFE SAN
	handler, _, cleanup := setupPIVFullHandler(t, leaf, "tenant-piv", true)
	defer cleanup()

	w := httptest.NewRecorder()
	handler.PIVLogin(w, pivRequestWithSpiffe(leaf))

	require.Equal(t, http.StatusUnauthorized, w.Code)
	assert.Contains(t, w.Body.String(), "login tenant could not be resolved")
}

// The identity store being unavailable (mock without GetIdentityStore) yields
// a 500.
func TestPIVLogin_IdentityStoreUnavailable(t *testing.T) {
	handler, cleanup := newPIVLoginHandlerWithTrustedIssuers(t, nil)
	defer cleanup()

	ca, caKey := pivLoginTestCA(t, "PIV Root CA")
	leaf := pivLoginTestLeaf(t, ca, caKey)
	w := httptest.NewRecorder()
	handler.PIVLogin(w, buildRequestWithPeerCert(leaf))

	require.Equal(t, http.StatusInternalServerError, w.Code)
	assert.Contains(t, w.Body.String(), "identity store not available")
}

// PIVLogin with a TrustedIssuers list that includes the leaf's issuer DN
// proceeds past the issuer gate to a successful token issuance.
func TestPIVLogin_TrustedIssuerAllowed(t *testing.T) {
	ca, caKey := pivLoginTestCA(t, "PIV Root CA")
	leaf := pivSpiffeLeaf(t, ca, caKey, "tenant-piv")
	handler, _, cleanup := setupPIVFullHandler(t, leaf, "tenant-piv", true)
	defer cleanup()

	// The issuer DN x509 produces for CN "PIV Root CA" is "CN=PIV Root CA".
	handler.server.config.PIVClientCert.TrustedIssuers = []string{leaf.Issuer.String()}

	w := httptest.NewRecorder()
	handler.PIVLogin(w, pivRequestWithSpiffe(leaf))
	require.Equal(t, http.StatusOK, w.Code, "body: %s", w.Body.String())
}

// logPIVLoginAudit / logPIVLoginFailureAudit no-op when the service exposes no
// audit logger (the !ok branch).
func TestPIVLoginAudit_NoAuditLogger(t *testing.T) {
	_, server, cleanup := setupIAMHandler(t)
	defer cleanup()
	h := &IAMHandler{server: server}
	req := httptest.NewRequest(http.MethodPost, "/x", nil)

	// Should not panic and silently return (service has no GetAuditLogger).
	h.logPIVLoginAudit(context.Background(), "t", "u", "e", "thumb", "subj", "iss", "smartcard", req)
	h.logPIVLoginFailureAudit(context.Background(), "t", "thumb", "err", "msg", req)
}

// pivAuditErrService returns a configurable (logger, err) pair from
// GetAuditLogger to drive the fail-soft branches of the PIV audit helpers.
type pivAuditErrService struct {
	*testutil.MockQuicBaaS
	auditLogger iam.AuditLogger
	auditErr    error
}

func (s *pivAuditErrService) GetAuditLogger(_ context.Context) (iam.AuditLogger, error) {
	return s.auditLogger, s.auditErr
}

func newPIVAuditErrHandler(t *testing.T, lg iam.AuditLogger, err error) *IAMHandler {
	t.Helper()
	log, lerr := logger.New(logger.LogConfig{Level: "error", Format: "json", Output: "stdout"})
	require.NoError(t, lerr)
	cfg := DefaultConfig()
	cfg.Address = ":0"
	srv, serr := NewServer(&pivAuditErrService{
		MockQuicBaaS: testutil.NewMockQuicBaaS(),
		auditLogger:  lg,
		auditErr:     err,
	}, cfg, log)
	require.NoError(t, serr)
	return &IAMHandler{server: srv}
}

// TestPIVLoginAudit_LookupErrorAndNilLogger covers the err != nil and the
// logger == nil short-circuits shared by both PIV audit helpers: neither must
// attempt LogAsync on a nil logger.
func TestPIVLoginAudit_LookupErrorAndNilLogger(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/x", nil)
	ctx := context.Background()

	t.Run("lookup error", func(t *testing.T) {
		h := newPIVAuditErrHandler(t, nil, errors.New("audit down"))
		assert.NotPanics(t, func() {
			h.logPIVLoginAudit(ctx, "t", "u", "e", "thumb", "subj", "iss", "smartcard", req)
			h.logPIVLoginFailureAudit(ctx, "t", "thumb", "err", "msg", req)
		})
	})

	t.Run("nil logger", func(t *testing.T) {
		h := newPIVAuditErrHandler(t, nil, nil)
		assert.NotPanics(t, func() {
			h.logPIVLoginAudit(ctx, "t", "u", "e", "thumb", "subj", "iss", "smartcard", req)
			h.logPIVLoginFailureAudit(ctx, "t", "thumb", "err", "msg", req)
		})
	})
}

// TestPIVLoginAudit_CapturesRemoteIPAndUserAgent asserts the IP/User-Agent
// enrichment lands on the emitted PIV success audit event.
func TestPIVLoginAudit_CapturesRemoteIPAndUserAgent(t *testing.T) {
	capture := &captureAuditLogger{}
	h := newPIVAuditErrHandler(t, capture, nil)

	req := httptest.NewRequest(http.MethodPost, "/x", nil)
	req.RemoteAddr = "198.51.100.9:44444"
	req.Header.Set("User-Agent", "piv-agent/1.0")

	h.logPIVLoginAudit(context.Background(), "t", "u", "e", "thumb", "subj", "iss", "smartcard", req)

	require.Len(t, capture.events, 1)
	ev := capture.events[0]
	assert.Equal(t, "198.51.100.9:44444", ev.IPAddress)
	assert.Equal(t, "piv-agent/1.0", ev.UserAgent)
}
