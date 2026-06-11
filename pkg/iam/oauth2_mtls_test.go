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

package iam_test

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"testing"
	"time"

	"github.com/jeremyhahn/go-quicbaas/pkg/iam"
)

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

// makeSelfSigned generates a self-signed ECDSA cert and returns the parsed
// *x509.Certificate and the raw PEM bytes.
func makeSelfSigned(t *testing.T, subject pkix.Name, dnsNames []string) (*x509.Certificate, []byte) {
	t.Helper()

	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}

	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      subject,
		DNSNames:     dnsNames,
		NotBefore:    time.Now().Add(-time.Minute),
		NotAfter:     time.Now().Add(time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth},
	}

	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &priv.PublicKey, priv)
	if err != nil {
		t.Fatalf("create certificate: %v", err)
	}

	cert, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatalf("parse certificate: %v", err)
	}

	pemBytes := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	return cert, pemBytes
}

// makeCA generates a minimal CA cert and returns the cert, key, and cert pool.
func makeCA(t *testing.T) (*x509.Certificate, *ecdsa.PrivateKey, *x509.CertPool) {
	t.Helper()

	caKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generate CA key: %v", err)
	}

	caTmpl := &x509.Certificate{
		SerialNumber:          big.NewInt(100),
		Subject:               pkix.Name{CommonName: "Test CA"},
		NotBefore:             time.Now().Add(-time.Minute),
		NotAfter:              time.Now().Add(time.Hour),
		IsCA:                  true,
		BasicConstraintsValid: true,
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageCRLSign,
	}

	der, err := x509.CreateCertificate(rand.Reader, caTmpl, caTmpl, &caKey.PublicKey, caKey)
	if err != nil {
		t.Fatalf("create CA cert: %v", err)
	}

	caCert, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatalf("parse CA cert: %v", err)
	}

	pool := x509.NewCertPool()
	pool.AddCert(caCert)

	return caCert, caKey, pool
}

// makeIssuedCert generates a leaf cert signed by the given CA.
func makeIssuedCert(t *testing.T, caCert *x509.Certificate, caKey *ecdsa.PrivateKey, subject pkix.Name, dnsNames []string) *x509.Certificate {
	t.Helper()

	leafKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generate leaf key: %v", err)
	}

	leafTmpl := &x509.Certificate{
		SerialNumber: big.NewInt(2),
		Subject:      subject,
		DNSNames:     dnsNames,
		NotBefore:    time.Now().Add(-time.Minute),
		NotAfter:     time.Now().Add(time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth},
	}

	der, err := x509.CreateCertificate(rand.Reader, leafTmpl, caCert, &leafKey.PublicKey, caKey)
	if err != nil {
		t.Fatalf("create leaf cert: %v", err)
	}

	cert, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatalf("parse leaf cert: %v", err)
	}

	return cert
}

// ---------------------------------------------------------------------------
// CertThumbprintS256 tests
// ---------------------------------------------------------------------------

func TestCertThumbprintS256_Deterministic(t *testing.T) {
	cert, _ := makeSelfSigned(t, pkix.Name{CommonName: "test"}, nil)
	first := iam.CertThumbprintS256(cert)
	second := iam.CertThumbprintS256(cert)
	if first != second {
		t.Fatalf("thumbprint not deterministic: %q vs %q", first, second)
	}
	if len(first) == 0 {
		t.Fatal("thumbprint must not be empty")
	}
}

func TestCertThumbprintS256_DistinctCerts(t *testing.T) {
	certA, _ := makeSelfSigned(t, pkix.Name{CommonName: "a"}, nil)
	certB, _ := makeSelfSigned(t, pkix.Name{CommonName: "b"}, nil)
	if iam.CertThumbprintS256(certA) == iam.CertThumbprintS256(certB) {
		t.Fatal("different certs must have different thumbprints")
	}
}

// ---------------------------------------------------------------------------
// selfSignedClientAuth tests
// ---------------------------------------------------------------------------

func TestSelfSignedClientAuth_HappyPath(t *testing.T) {
	cert, certPEM := makeSelfSigned(t, pkix.Name{CommonName: "svc-agent"}, nil)

	app := &iam.Application{
		ClientID:                "app_test_abc",
		TenantID:                "tenant1",
		GrantTypes:              []string{iam.GrantTypeClientCredentials},
		TokenEndpointAuthMethod: iam.AuthMethodSelfSignedTLSClientAuth,
	}
	if err := app.PinMTLSCert(certPEM); err != nil {
		t.Fatalf("PinMTLSCert: %v", err)
	}

	storage := newMemAppStorage(app)
	auth := iam.NewSelfSignedClientAuth(storage)

	got, err := auth.AuthenticateClient(context.Background(), cert, app.ClientID)
	if err != nil {
		t.Fatalf("expected success, got: %v", err)
	}
	if got.ClientID != app.ClientID {
		t.Fatalf("client ID mismatch: got %q want %q", got.ClientID, app.ClientID)
	}
}

func TestSelfSignedClientAuth_ThumbprintMismatch(t *testing.T) {
	cert, _ := makeSelfSigned(t, pkix.Name{CommonName: "svc-agent"}, nil)
	otherCert, otherPEM := makeSelfSigned(t, pkix.Name{CommonName: "other"}, nil)
	_ = otherCert

	app := &iam.Application{
		ClientID:                "app_test_abc",
		TenantID:                "tenant1",
		GrantTypes:              []string{iam.GrantTypeClientCredentials},
		TokenEndpointAuthMethod: iam.AuthMethodSelfSignedTLSClientAuth,
	}
	// Pin the OTHER cert's thumbprint.
	if err := app.PinMTLSCert(otherPEM); err != nil {
		t.Fatalf("PinMTLSCert: %v", err)
	}

	storage := newMemAppStorage(app)
	auth := iam.NewSelfSignedClientAuth(storage)

	_, err := auth.AuthenticateClient(context.Background(), cert, app.ClientID)
	if err == nil {
		t.Fatal("expected thumbprint mismatch error, got nil")
	}
}

func TestSelfSignedClientAuth_NilCert(t *testing.T) {
	app := &iam.Application{
		ClientID:                "app_test_abc",
		TokenEndpointAuthMethod: iam.AuthMethodSelfSignedTLSClientAuth,
		MTLSCertThumbprint:      "irrelevant",
	}
	storage := newMemAppStorage(app)
	auth := iam.NewSelfSignedClientAuth(storage)

	_, err := auth.AuthenticateClient(context.Background(), nil, app.ClientID)
	if err == nil {
		t.Fatal("expected error for nil cert")
	}
}

func TestSelfSignedClientAuth_ClientNotFound(t *testing.T) {
	cert, _ := makeSelfSigned(t, pkix.Name{CommonName: "svc"}, nil)
	storage := newMemAppStorage() // empty
	auth := iam.NewSelfSignedClientAuth(storage)

	_, err := auth.AuthenticateClient(context.Background(), cert, "nonexistent")
	if err == nil {
		t.Fatal("expected not-found error, got nil")
	}
}

// ---------------------------------------------------------------------------
// pkiClientAuth tests
// ---------------------------------------------------------------------------

func TestPKIClientAuth_HappyPath_SubjectDN(t *testing.T) {
	caCert, caKey, pool := makeCA(t)
	subject := pkix.Name{CommonName: "fleet-agent-1", Organization: []string{"Acme Corp"}}
	cert := makeIssuedCert(t, caCert, caKey, subject, nil)

	app := &iam.Application{
		ClientID:                "app_fleet_001",
		TenantID:                "tenant1",
		GrantTypes:              []string{iam.GrantTypeClientCredentials},
		TokenEndpointAuthMethod: iam.AuthMethodTLSClientAuth,
		MTLSSubjectDN:           cert.Subject.String(),
	}
	storage := newMemAppStorage(app)
	auth := iam.NewPKIClientAuth(pool, storage)

	got, err := auth.AuthenticateClient(context.Background(), cert, app.ClientID)
	if err != nil {
		t.Fatalf("expected success, got: %v", err)
	}
	if got.ClientID != app.ClientID {
		t.Fatalf("client ID mismatch")
	}
}

func TestPKIClientAuth_HappyPath_SANDNS(t *testing.T) {
	caCert, caKey, pool := makeCA(t)
	cert := makeIssuedCert(t, caCert, caKey, pkix.Name{CommonName: "agent"}, []string{"agent.example.com"})

	app := &iam.Application{
		ClientID:                "app_fleet_002",
		TenantID:                "tenant1",
		GrantTypes:              []string{iam.GrantTypeClientCredentials},
		TokenEndpointAuthMethod: iam.AuthMethodTLSClientAuth,
		MTLSSANDNS:              "agent.example.com",
	}
	storage := newMemAppStorage(app)
	auth := iam.NewPKIClientAuth(pool, storage)

	got, err := auth.AuthenticateClient(context.Background(), cert, app.ClientID)
	if err != nil {
		t.Fatalf("expected success, got: %v", err)
	}
	if got.ClientID != app.ClientID {
		t.Fatalf("client ID mismatch")
	}
}

func TestPKIClientAuth_UntrustedCert(t *testing.T) {
	_, _, pool := makeCA(t) // CA that did NOT issue the cert
	// Issue cert from a different CA
	caCert2, caKey2, _ := makeCA(t)
	cert := makeIssuedCert(t, caCert2, caKey2, pkix.Name{CommonName: "agent"}, nil)

	app := &iam.Application{
		ClientID:                "app_fleet_003",
		TokenEndpointAuthMethod: iam.AuthMethodTLSClientAuth,
		MTLSSubjectDN:           cert.Subject.String(),
	}
	storage := newMemAppStorage(app)
	auth := iam.NewPKIClientAuth(pool, storage)

	_, err := auth.AuthenticateClient(context.Background(), cert, app.ClientID)
	if err == nil {
		t.Fatal("expected chain verification failure, got nil")
	}
}

func TestPKIClientAuth_DNMismatch(t *testing.T) {
	caCert, caKey, pool := makeCA(t)
	cert := makeIssuedCert(t, caCert, caKey, pkix.Name{CommonName: "agent"}, nil)

	app := &iam.Application{
		ClientID:                "app_fleet_004",
		TokenEndpointAuthMethod: iam.AuthMethodTLSClientAuth,
		MTLSSubjectDN:           "CN=other-service",
	}
	storage := newMemAppStorage(app)
	auth := iam.NewPKIClientAuth(pool, storage)

	_, err := auth.AuthenticateClient(context.Background(), cert, app.ClientID)
	if err == nil {
		t.Fatal("expected cert mismatch error, got nil")
	}
}

// ---------------------------------------------------------------------------
// PinMTLSCert tests
// ---------------------------------------------------------------------------

func TestPinMTLSCert_InvalidPEM(t *testing.T) {
	app := &iam.Application{ClientID: "x"}
	if err := app.PinMTLSCert([]byte("not-a-pem")); err == nil {
		t.Fatal("expected error for invalid PEM")
	}
}

func TestPinMTLSCert_SetsThumbprintAndAuthMethod(t *testing.T) {
	_, certPEM := makeSelfSigned(t, pkix.Name{CommonName: "svc"}, nil)
	app := &iam.Application{ClientID: "x"}
	if err := app.PinMTLSCert(certPEM); err != nil {
		t.Fatalf("PinMTLSCert: %v", err)
	}
	if app.MTLSCertThumbprint == "" {
		t.Fatal("thumbprint not set")
	}
	if app.TokenEndpointAuthMethod != iam.AuthMethodSelfSignedTLSClientAuth {
		t.Fatalf("auth method not set: got %q", app.TokenEndpointAuthMethod)
	}
}

// ---------------------------------------------------------------------------
// VerifyMTLSBoundToken tests
// ---------------------------------------------------------------------------

func TestVerifyMTLSBoundToken_Match(t *testing.T) {
	cert, _ := makeSelfSigned(t, pkix.Name{CommonName: "svc"}, nil)
	thumbprint := iam.CertThumbprintS256(cert)

	claims := map[string]interface{}{
		"cnf": map[string]interface{}{
			"x5t#S256": thumbprint,
		},
	}
	if err := iam.VerifyMTLSBoundToken(claims, cert); err != nil {
		t.Fatalf("expected success: %v", err)
	}
}

func TestVerifyMTLSBoundToken_Mismatch(t *testing.T) {
	certA, _ := makeSelfSigned(t, pkix.Name{CommonName: "a"}, nil)
	certB, _ := makeSelfSigned(t, pkix.Name{CommonName: "b"}, nil)

	claims := map[string]interface{}{
		"cnf": map[string]interface{}{
			"x5t#S256": iam.CertThumbprintS256(certA),
		},
	}
	if err := iam.VerifyMTLSBoundToken(claims, certB); err == nil {
		t.Fatal("expected mismatch error, got nil")
	}
}

func TestVerifyMTLSBoundToken_MissingCNF(t *testing.T) {
	cert, _ := makeSelfSigned(t, pkix.Name{CommonName: "svc"}, nil)
	claims := map[string]interface{}{}
	if err := iam.VerifyMTLSBoundToken(claims, cert); err == nil {
		t.Fatal("expected missing cnf error, got nil")
	}
}

func TestVerifyMTLSBoundToken_NilCert(t *testing.T) {
	claims := map[string]interface{}{
		"cnf": map[string]interface{}{"x5t#S256": "abc"},
	}
	if err := iam.VerifyMTLSBoundToken(claims, nil); err == nil {
		t.Fatal("expected error for nil cert, got nil")
	}
}

// ---------------------------------------------------------------------------
// minimal in-memory AppStorage for tests
// ---------------------------------------------------------------------------

// memAppStorage is a minimal in-memory AppStorage implementation for unit tests.
type memAppStorage struct {
	apps map[string]*iam.Application
}

func newMemAppStorage(initial ...*iam.Application) iam.AppStorage {
	m := &memAppStorage{apps: make(map[string]*iam.Application)}
	for _, a := range initial {
		m.apps[a.ClientID] = a
	}
	return m
}

func (m *memAppStorage) CreateApp(_ context.Context, app *iam.Application) error {
	m.apps[app.ClientID] = app
	return nil
}

func (m *memAppStorage) GetApp(_ context.Context, clientID string) (*iam.Application, error) {
	app, ok := m.apps[clientID]
	if !ok {
		return nil, iam.ErrMTLSClientNotFound
	}
	return app, nil
}

func (m *memAppStorage) GetAppByTenant(_ context.Context, _, clientID string) (*iam.Application, error) {
	return m.GetApp(context.Background(), clientID)
}

func (m *memAppStorage) UpdateApp(_ context.Context, app *iam.Application) error {
	m.apps[app.ClientID] = app
	return nil
}

func (m *memAppStorage) DeleteApp(_ context.Context, clientID string) error {
	delete(m.apps, clientID)
	return nil
}

func (m *memAppStorage) ListApps(_ context.Context, _ string, _ iam.ListOptions) ([]*iam.Application, error) {
	var out []*iam.Application
	for _, a := range m.apps {
		out = append(out, a)
	}
	return out, nil
}

func (m *memAppStorage) ValidateClientSecret(_ context.Context, clientID, secret string) (bool, error) {
	app, ok := m.apps[clientID]
	if !ok {
		return false, iam.ErrMTLSClientNotFound
	}
	return app.ClientSecret == secret, nil
}
