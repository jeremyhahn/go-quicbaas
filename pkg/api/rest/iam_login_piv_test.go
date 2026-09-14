//go:build rest

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
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/iam"
	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
	"github.com/jeremyhahn/go-quicbaas/pkg/testutil"
)

// pivLoginTestCA creates a self-signed CA for PIV login handler tests.
func pivLoginTestCA(t *testing.T, cn string) (*x509.Certificate, *ecdsa.PrivateKey) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)
	tmpl := &x509.Certificate{
		SerialNumber:          big.NewInt(10),
		Subject:               pkix.Name{CommonName: cn},
		NotBefore:             time.Now().Add(-time.Minute),
		NotAfter:              time.Now().Add(time.Hour),
		IsCA:                  true,
		BasicConstraintsValid: true,
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageCRLSign,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	require.NoError(t, err)
	cert, err := x509.ParseCertificate(der)
	require.NoError(t, err)
	return cert, key
}

// pivLoginTestLeaf creates a client leaf certificate signed by the given CA.
func pivLoginTestLeaf(t *testing.T, ca *x509.Certificate, caKey *ecdsa.PrivateKey) *x509.Certificate {
	t.Helper()
	leafKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(20),
		Subject:      pkix.Name{CommonName: "PIV Test Client"},
		NotBefore:    time.Now().Add(-time.Minute),
		NotAfter:     time.Now().Add(time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth},
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, ca, &leafKey.PublicKey, caKey)
	require.NoError(t, err)
	cert, err := x509.ParseCertificate(der)
	require.NoError(t, err)
	return cert
}

// pemEncodeCA returns a PEM-encoded CA certificate.
func pemEncodeCA(t *testing.T, ca *x509.Certificate) []byte {
	t.Helper()
	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: ca.Raw})
}

// newPIVLoginHandlerWithTrustedIssuers creates an IAMHandler with PIV enabled and
// a specific TrustedIssuers list for testing the second-gate DN check.
func newPIVLoginHandlerWithTrustedIssuers(t *testing.T, trustedIssuers []string) (*IAMHandler, func()) {
	t.Helper()
	_, server, cleanup := setupIAMHandler(t)
	server.config.PIVClientCert.Enabled = true
	server.config.PIVClientCert.TrustedIssuers = trustedIssuers
	handler := &IAMHandler{server: server}
	return handler, cleanup
}

// buildRequestWithPeerCert builds an httptest.Request with a fake TLS state
// containing the supplied leaf certificate as PeerCertificates[0].
func buildRequestWithPeerCert(leaf *x509.Certificate) *http.Request {
	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login/piv", nil)
	req.TLS = &tls.ConnectionState{
		PeerCertificates: []*x509.Certificate{leaf},
	}
	return req
}

// captureAuditLogger is a minimal AuditLogger that records emitted events.
// It implements only the subset required by handler tests.
type captureAuditLogger struct {
	events []*iam.AuditEvent
}

func (l *captureAuditLogger) LogAsync(event *iam.AuditEvent) {
	l.events = append(l.events, event)
}

func (l *captureAuditLogger) Log(_ context.Context, event *iam.AuditEvent) error {
	l.events = append(l.events, event)
	return nil
}

func (l *captureAuditLogger) Query(_ context.Context, _ *iam.AuditFilter) ([]*iam.AuditEvent, int, error) {
	return nil, 0, nil
}

func (l *captureAuditLogger) GetEvent(_ context.Context, _, _ string) (*iam.AuditEvent, error) {
	return nil, nil
}

func (l *captureAuditLogger) DeleteOlderThan(_ context.Context, _ string, _ time.Time) (int, error) {
	return 0, nil
}

func (l *captureAuditLogger) Close() error { return nil }

// TestPIVLogin_Disabled verifies that PIV login returns 401 when disabled.
func TestPIVLogin_Disabled(t *testing.T) {
	handler, _, cleanup := setupIAMHandler(t)
	defer cleanup()

	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login/piv", nil)
	w := httptest.NewRecorder()
	handler.PIVLogin(w, req)

	assert.Equal(t, http.StatusUnauthorized, w.Code)
	assert.Contains(t, w.Body.String(), ErrPIVLoginDisabled)
}

// TestPIVLogin_MissingClientCert verifies that 401 is returned when no TLS cert is presented.
func TestPIVLogin_MissingClientCert(t *testing.T) {
	handler, cleanup := newPIVLoginHandlerWithTrustedIssuers(t, nil)
	defer cleanup()

	// No TLS state → missing client cert.
	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login/piv", nil)
	w := httptest.NewRecorder()
	handler.PIVLogin(w, req)

	assert.Equal(t, http.StatusUnauthorized, w.Code)
	assert.Contains(t, w.Body.String(), ErrClientCertRequired)
}

// TestPIVLogin_RejectsUntrustedIssuer verifies that a cert whose issuer DN is not
// in the TrustedIssuers list is rejected with piv_cert_issuer_rejected.
func TestPIVLogin_RejectsUntrustedIssuer(t *testing.T) {
	ca, caKey := pivLoginTestCA(t, "CN=Some Other CA")
	leaf := pivLoginTestLeaf(t, ca, caKey)

	// Allow only "CN=Trusted Agency CA" — the leaf's issuer is "CN=Some Other CA".
	handler, cleanup := newPIVLoginHandlerWithTrustedIssuers(t, []string{"CN=Trusted Agency CA"})
	defer cleanup()

	req := buildRequestWithPeerCert(leaf)
	w := httptest.NewRecorder()
	handler.PIVLogin(w, req)

	assert.Equal(t, http.StatusUnauthorized, w.Code)
	assert.Contains(t, w.Body.String(), ErrPIVCertIssuerRejected)
}

// TestPIVLogin_AcceptsPermissiveIssuer verifies that when TrustedIssuers is empty,
// the issuer check is skipped (permissive default). The flow falls through to the
// identity store lookup which returns not-found (identity store not wired in test).
func TestPIVLogin_AcceptsPermissiveIssuer(t *testing.T) {
	ca, caKey := pivLoginTestCA(t, "CN=Any CA")
	leaf := pivLoginTestLeaf(t, ca, caKey)

	// Empty TrustedIssuers = permissive; cert passes issuer check.
	handler, cleanup := newPIVLoginHandlerWithTrustedIssuers(t, nil)
	defer cleanup()

	req := buildRequestWithPeerCert(leaf)
	w := httptest.NewRecorder()
	handler.PIVLogin(w, req)

	// The identity store is not wired in the test handler so we fall through to
	// an internal-error (identity store not available) — not issuer rejected.
	body := w.Body.String()
	assert.NotContains(t, body, ErrPIVCertIssuerRejected,
		"empty TrustedIssuers must not trigger issuer rejection")
}

// TestPIVLogin_CaseInsensitiveIssuerMatch verifies that issuer DN matching is
// case-insensitive per RFC 4514.
func TestPIVLogin_CaseInsensitiveIssuerMatch(t *testing.T) {
	ca, caKey := pivLoginTestCA(t, "Trusted Agency CA")
	leaf := pivLoginTestLeaf(t, ca, caKey)

	// Supply the DN in different case from what x509 will produce.
	handler, cleanup := newPIVLoginHandlerWithTrustedIssuers(t,
		[]string{"cn=trusted agency ca"}) // lowercase
	defer cleanup()

	req := buildRequestWithPeerCert(leaf)
	w := httptest.NewRecorder()
	handler.PIVLogin(w, req)

	body := w.Body.String()
	assert.NotContains(t, body, ErrPIVCertIssuerRejected,
		"case-insensitive DN match must not trigger issuer rejection")
}

// pivAuditMockService embeds MockQuicBaaS (which implements service.QuicBaaS)
// and adds GetAuditLogger so the audit handler functions resolve it at runtime.
type pivAuditMockService struct {
	*testutil.MockQuicBaaS
	logger iam.AuditLogger
}

// GetAuditLogger satisfies the inline interface assertion in audit handler methods.
func (s *pivAuditMockService) GetAuditLogger(_ context.Context) (iam.AuditLogger, error) {
	return s.logger, nil
}

// newPIVAuditServer creates a Server with a pivAuditMockService pre-wired.
func newPIVAuditServer(t *testing.T, capture *captureAuditLogger) *Server {
	t.Helper()
	log, err := logger.New(logger.LogConfig{Level: "error", Format: "json", Output: "stdout"})
	require.NoError(t, err)
	mock := &pivAuditMockService{
		MockQuicBaaS: testutil.NewMockQuicBaaS(),
		logger:       capture,
	}
	cfg := DefaultConfig()
	cfg.Address = ":0"
	srv, err := NewServer(mock, cfg, log)
	require.NoError(t, err)
	return srv
}

// TestPIVLogin_LogFailureAudit_IssuerRejected verifies that logPIVLoginFailureAudit
// emits an event with Action = "auth.login.piv.failure" and the correct fields.
func TestPIVLogin_LogFailureAudit_IssuerRejected(t *testing.T) {
	capture := &captureAuditLogger{}
	srv := newPIVAuditServer(t, capture)

	h := &IAMHandler{server: srv}
	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login/piv", nil)
	req.Header.Set("X-Request-ID", "req-test-123")

	h.logPIVLoginFailureAudit(context.Background(), "tenant-1", "thumbprint-abc",
		ErrPIVCertIssuerRejected, "issuer DN rejected", req)

	require.Len(t, capture.events, 1)
	ev := capture.events[0]
	assert.Equal(t, "auth.login.piv.failure", ev.Action)
	assert.Equal(t, iam.AuditOutcomeFailure, ev.Outcome)
	assert.Equal(t, ErrPIVCertIssuerRejected, ev.Details["error_code"])
	assert.Equal(t, "req-test-123", ev.Details["request_id"])
	assert.Equal(t, "thumbprint-abc", ev.Details["cert_thumbprint"])
}

// TestPIVLogin_LogSuccessAudit verifies that logPIVLoginAudit emits
// event_type "auth.login.piv.success" with the expected fields.
func TestPIVLogin_LogSuccessAudit(t *testing.T) {
	capture := &captureAuditLogger{}
	srv := newPIVAuditServer(t, capture)

	h := &IAMHandler{server: srv}
	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login/piv", nil)
	req.Header.Set("X-Request-ID", "req-success-456")

	h.logPIVLoginAudit(context.Background(), "tenant-1", "user-1", "user@example.com",
		"thumbprint-xyz", "CN=User", "CN=Issuer CA", "smartcard", "hardware", req)

	require.Len(t, capture.events, 1)
	ev := capture.events[0]
	assert.Equal(t, "auth.login.piv.success", ev.Action)
	assert.Equal(t, iam.AuditOutcomeSuccess, ev.Outcome)
	assert.Equal(t, "thumbprint-xyz", ev.Details["cert_thumbprint"])
	assert.Equal(t, "CN=User", ev.Details["cert_subject"])
	assert.Equal(t, "CN=Issuer CA", ev.Details["cert_issuer"])
	assert.Equal(t, "req-success-456", ev.Details["request_id"])
}
