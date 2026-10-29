// Copyright (c) 2026 Jeremy Hahn
// Copyright (c) 2026 Automate The Things, LLC
//
// This file is part of go-quicbaas.

package server

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"errors"
	"math/big"
	"testing"
	"time"

	xkmsca "github.com/jeremyhahn/go-xkms/pkg/ca"
	xkmstypes "github.com/jeremyhahn/go-xkms/pkg/types"
	xkmssdk "github.com/jeremyhahn/go-xkms/sdk/go"

	"github.com/jeremyhahn/go-quicbaas/pkg/config"
)

// externalCAMock is a controllable TrustedCA for issueTLSCertFromExternalCA.
// Only IssueCertificate and ServerTLSConfig are exercised; every other method
// is the embedded nil interface and must not be called.
type externalCAMock struct {
	xkmsca.TrustedCA
	issueErr  error
	serverCfg *tls.Config
	serverErr error
	certs     xkmssdk.CertStore
}

func (m *externalCAMock) CertStore() xkmssdk.CertStore { return m.certs }

func (m *externalCAMock) IssueCertificate(_ context.Context, _ *xkmsca.CertificateRequest) (*xkmsca.IssuedCertificate, error) {
	if m.issueErr != nil {
		return nil, m.issueErr
	}
	return &xkmsca.IssuedCertificate{}, nil
}

func (m *externalCAMock) ServerTLSConfig(_ context.Context, _ *xkmstypes.KeyAttributes, _ bool) (*tls.Config, error) {
	return m.serverCfg, m.serverErr
}

// buildSelfSignedTLSCert returns a self-signed leaf certificate + key as a
// tls.Certificate for the pre-issued-certificate branch.
func buildSelfSignedTLSCert(t *testing.T) tls.Certificate {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("GenerateKey: %v", err)
	}
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "localhost"},
		NotBefore:    time.Now().Add(-time.Minute),
		NotAfter:     time.Now().Add(time.Hour),
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatalf("CreateCertificate: %v", err)
	}
	return tls.Certificate{Certificate: [][]byte{der}, PrivateKey: key}
}

// A pre-issued TLS certificate short-circuits issuance and is installed
// directly onto the server's TLS config.
func TestIssueTLSCertFromExternalCA_PreIssuedCert(t *testing.T) {
	cert := buildSelfSignedTLSCert(t)
	s := &Server{
		config: &config.Config{},
		logger: buildTestLogger(t),
	}
	s.opts.tlsCertificate = &cert

	if err := s.issueTLSCertFromExternalCA(); err != nil {
		t.Fatalf("issueTLSCertFromExternalCA: %v", err)
	}
	if s.tlsConfig == nil || len(s.tlsConfig.Certificates) != 1 {
		t.Fatalf("pre-issued certificate not installed: %+v", s.tlsConfig)
	}
	if s.tlsConfig.MinVersion != tls.VersionTLS13 {
		t.Fatalf("expected TLS 1.3 minimum")
	}
}

// The CA-issuance path issues a server certificate and builds the server TLS
// config from the external CA.
func TestIssueTLSCertFromExternalCA_Issues(t *testing.T) {
	s := &Server{
		config: &config.Config{},
		logger: buildTestLogger(t),
	}
	s.config.Server.Host = "127.0.0.1"
	s.config.CA.RootOrganization = "Test Org"
	s.config.CA.DefaultValidityDays = 365
	s.setCA(&externalCAMock{serverCfg: &tls.Config{}})

	if err := s.issueTLSCertFromExternalCA(); err != nil {
		t.Fatalf("issueTLSCertFromExternalCA: %v", err)
	}
	if s.tlsConfig == nil {
		t.Fatal("tlsConfig not set")
	}
	if s.tlsConfig.MinVersion != tls.VersionTLS13 {
		t.Fatalf("expected TLS 1.3 minimum")
	}
	if len(s.tlsConfig.NextProtos) == 0 {
		t.Fatalf("expected ALPN protocols advertised")
	}
}

// An issuance failure (other than already-exists) is surfaced.
func TestIssueTLSCertFromExternalCA_IssueError(t *testing.T) {
	s := &Server{
		config: &config.Config{},
		logger: buildTestLogger(t),
	}
	s.setCA(&externalCAMock{issueErr: errors.New("ca offline")})

	if err := s.issueTLSCertFromExternalCA(); err == nil {
		t.Fatal("expected error when CA issuance fails")
	}
}

// An already-exists issuance result is treated as success and the TLS config is
// still built. The RSA key-algorithm branch is exercised here.
func TestIssueTLSCertFromExternalCA_AlreadyExistsRSA(t *testing.T) {
	s := &Server{
		config: &config.Config{},
		logger: buildTestLogger(t),
	}
	s.config.CA.RootKeyAlgorithm = "RSA"
	certs := newSANCertStoreMock(t)
	certs.certs["localhost"] = newSANTestLeaf(t, "localhost", s.serverCertSANs())
	s.setCA(&externalCAMock{
		issueErr:  xkmssdk.ErrCertificateAlreadyExists,
		serverCfg: &tls.Config{},
		certs:     certs,
	})

	if err := s.issueTLSCertFromExternalCA(); err != nil {
		t.Fatalf("already-exists must be treated as success: %v", err)
	}
	if s.tlsConfig == nil {
		t.Fatal("tlsConfig not set")
	}
}

// A ServerTLSConfig failure is surfaced as an operation error.
func TestIssueTLSCertFromExternalCA_ServerConfigError(t *testing.T) {
	s := &Server{
		config: &config.Config{},
		logger: buildTestLogger(t),
	}
	s.setCA(&externalCAMock{serverErr: errors.New("no key")})

	if err := s.issueTLSCertFromExternalCA(); err == nil {
		t.Fatal("expected error when ServerTLSConfig fails")
	}
}
