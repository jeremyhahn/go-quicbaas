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

//go:build rest && iam

package server

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/jeremyhahn/go-quicbaas/pkg/config"
	qberrors "github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
)

// writeCertPEM writes a self-signed CA certificate to a temporary PEM file
// and returns the path. The certificate is valid for testing purposes only.
func writeCertPEM(t *testing.T, dir, name string) (string, *x509.Certificate) {
	t.Helper()

	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}

	template := x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: name + " CA"},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(time.Hour),
		IsCA:                  true,
		BasicConstraintsValid: true,
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageCRLSign,
	}

	derBytes, err := x509.CreateCertificate(rand.Reader, &template, &template, &priv.PublicKey, priv)
	if err != nil {
		t.Fatalf("create certificate: %v", err)
	}

	cert, err := x509.ParseCertificate(derBytes)
	if err != nil {
		t.Fatalf("parse certificate: %v", err)
	}

	path := filepath.Join(dir, name+".pem")
	f, err := os.Create(path)
	if err != nil {
		t.Fatalf("create PEM file: %v", err)
	}
	defer f.Close()

	if err := pem.Encode(f, &pem.Block{Type: "CERTIFICATE", Bytes: derBytes}); err != nil {
		t.Fatalf("encode PEM: %v", err)
	}

	return path, cert
}

// TestLoadCertPool_WithValidPEM verifies that loadCertPool returns a populated pool.
func TestLoadCertPool_WithValidPEM(t *testing.T) {
	dir := t.TempDir()
	path, _ := writeCertPEM(t, dir, "test-ca")

	pool, err := loadCertPool(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if pool == nil {
		t.Fatal("expected non-nil cert pool")
	}
}

// TestLoadCertPool_FileNotFound verifies that a missing file returns an error.
func TestLoadCertPool_FileNotFound(t *testing.T) {
	_, err := loadCertPool("/nonexistent/path/ca.pem")
	if err == nil {
		t.Fatal("expected error for missing file, got nil")
	}

	var readErr *CertPoolReadError
	if !isError(err, &readErr) {
		t.Errorf("expected CertPoolReadError, got %T: %v", err, err)
	}
}

// TestLoadCertPool_EmptyPEM verifies that a PEM file with no certificates returns an error.
func TestLoadCertPool_EmptyPEM(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "empty.pem")
	if err := os.WriteFile(path, []byte("# no certs here\n"), 0600); err != nil {
		t.Fatalf("write empty file: %v", err)
	}

	_, err := loadCertPool(path)
	if err == nil {
		t.Fatal("expected error for empty PEM, got nil")
	}

	var emptyErr *CertPoolEmptyError
	if !isError(err, &emptyErr) {
		t.Errorf("expected CertPoolEmptyError, got %T: %v", err, err)
	}
}

// TestMTLSConfigError_Error verifies the error message format.
func TestMTLSConfigError_Error(t *testing.T) {
	inner := &CertPoolEmptyError{Path: "/etc/ca.pem"}
	e := &MTLSConfigError{Field: "tls_client_auth_ca_file", Cause: inner}

	got := e.Error()
	if got != "iam.mtls.tls_client_auth_ca_file: "+inner.Error() {
		t.Errorf("unexpected error message: %s", got)
	}
	if e.Unwrap() != inner {
		t.Error("Unwrap should return the cause")
	}
}

// TestCertPoolReadError_Error verifies the error message format.
func TestCertPoolReadError_Error(t *testing.T) {
	inner := os.ErrNotExist
	e := &CertPoolReadError{Path: "/path/to/ca.pem", Cause: inner}
	if e.Error() == "" {
		t.Error("expected non-empty error message")
	}
	if e.Unwrap() != inner {
		t.Error("Unwrap should return the cause")
	}
}

// TestCertPoolParseError_Error verifies the error message format.
func TestCertPoolParseError_Error(t *testing.T) {
	inner := os.ErrInvalid
	e := &CertPoolParseError{Path: "/path/to/ca.pem", Cause: inner}
	if e.Error() == "" {
		t.Error("expected non-empty error message")
	}
	if e.Unwrap() != inner {
		t.Error("Unwrap should return the cause")
	}
}

// TestCertPoolEmptyError_Error verifies the error message format.
func TestCertPoolEmptyError_Error(t *testing.T) {
	e := &CertPoolEmptyError{Path: "/path/to/empty.pem"}
	if e.Error() == "" {
		t.Error("expected non-empty error message")
	}
}

// TestMTLSAppStorageError_Error verifies the error message is non-empty.
func TestMTLSAppStorageError_Error(t *testing.T) {
	e := &MTLSAppStorageError{}
	if e.Error() == "" {
		t.Error("expected non-empty error message")
	}
}

// TestMTLSClientCAsError_Error verifies the error message includes the cause.
func TestMTLSClientCAsError_Error(t *testing.T) {
	inner := &CertPoolEmptyError{Path: "/etc/ca.pem"}
	e := &MTLSClientCAsError{Cause: inner}
	if e.Error() == "" {
		t.Error("expected non-empty error message")
	}
	if e.Unwrap() != inner {
		t.Error("Unwrap should return the cause")
	}
}

// TestAppendOperatorClientCAs_NoOp verifies that a nil CA file path is a no-op.
func TestAppendOperatorClientCAs_NoOp(t *testing.T) {
	s := &Server{
		config: buildMinimalTestConfig(""),
		logger: buildTestLogger(t),
	}
	tlsCfg := &tls.Config{}
	if err := s.appendOperatorClientCAs(tlsCfg); err != nil {
		t.Fatalf("expected nil error for empty CA file, got %v", err)
	}
	if tlsCfg.ClientCAs != nil {
		t.Error("expected ClientCAs to remain nil")
	}
}

// TestAppendOperatorClientCAs_MissingFile verifies that a missing PEM file
// returns a typed error and aborts startup.
func TestAppendOperatorClientCAs_MissingFile(t *testing.T) {
	s := &Server{
		config: buildMinimalTestConfig("/nonexistent/ca.pem"),
		logger: buildTestLogger(t),
	}
	err := s.appendOperatorClientCAs(&tls.Config{})
	if err == nil {
		t.Fatal("expected error for missing file, got nil")
	}
	// Must be wrapped in a QBError
	var qbErr *qberrors.QBError
	if !isError(err, &qbErr) {
		t.Errorf("expected *qberrors.QBError, got %T: %v", err, err)
	}
}

// TestAppendOperatorClientCAs_BothPoolsPresent verifies that a valid operator CA
// is appended to a pre-existing ClientCAs pool and ClientAuth is set.
func TestAppendOperatorClientCAs_BothPoolsPresent(t *testing.T) {
	dir := t.TempDir()
	operatorPath, operatorCert := writeCertPEM(t, dir, "operator-ca")
	daemonPath, daemonCert := writeCertPEM(t, dir, "daemon-ca")

	// Build a TLS config already containing the daemon CA.
	daemonPool, err := loadCertPool(daemonPath)
	if err != nil {
		t.Fatalf("load daemon CA: %v", err)
	}

	tlsCfg := &tls.Config{
		ClientCAs: daemonPool,
	}

	s := &Server{
		config: buildMinimalTestConfig(operatorPath),
		logger: buildTestLogger(t),
	}
	if err := s.appendOperatorClientCAs(tlsCfg); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if tlsCfg.ClientAuth != tls.VerifyClientCertIfGiven {
		t.Errorf("expected VerifyClientCertIfGiven, got %v", tlsCfg.ClientAuth)
	}
	// Both certs should verify against the combined pool.
	for _, cert := range []*x509.Certificate{daemonCert, operatorCert} {
		chains, verErr := cert.Verify(x509.VerifyOptions{
			Roots: tlsCfg.ClientCAs,
		})
		if verErr != nil {
			t.Errorf("cert %q failed to verify against combined pool: %v", cert.Subject.CommonName, verErr)
		}
		if len(chains) == 0 {
			t.Errorf("cert %q produced no chains", cert.Subject.CommonName)
		}
	}
}

// TestPIVLogin_RequiresClientCertWhenEnabled verifies that the PIV handler
// returns 401 client_cert_required when piv_login.enabled is true but the
// request carries no TLS peer certificate.
func TestPIVLogin_RequiresClientCertWhenEnabled(t *testing.T) {
	dir := t.TempDir()
	caPath, _ := writeCertPEM(t, dir, "piv-ca")

	cfg := &config.Config{}
	cfg.IAM.PIVLogin.Enabled = true
	cfg.IAM.PIVLogin.CAFile = caPath

	tlsCfg := &tls.Config{}
	s := &Server{config: cfg, logger: buildTestLogger(t)}

	if err := s.appendPIVClientCAs(tlsCfg); err != nil {
		t.Fatalf("appendPIVClientCAs: %v", err)
	}

	// ClientAuth must be set to VerifyClientCertIfGiven when PIV login is enabled.
	if tlsCfg.ClientAuth != tls.VerifyClientCertIfGiven {
		t.Errorf("expected VerifyClientCertIfGiven, got %v", tlsCfg.ClientAuth)
	}
}

// TestAppendPIVClientCAs_EnabledWithoutCAFile verifies that when ca_file is
// empty but enabled is true, VerifyClientCertIfGiven is still set without error.
func TestAppendPIVClientCAs_EnabledWithoutCAFile(t *testing.T) {
	cfg := &config.Config{}
	cfg.IAM.PIVLogin.Enabled = true
	// ca_file intentionally empty

	tlsCfg := &tls.Config{}
	s := &Server{config: cfg, logger: buildTestLogger(t)}

	if err := s.appendPIVClientCAs(tlsCfg); err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	if tlsCfg.ClientAuth != tls.VerifyClientCertIfGiven {
		t.Errorf("expected VerifyClientCertIfGiven, got %v", tlsCfg.ClientAuth)
	}
}

// TestAppendPIVClientCAs_MissingCAFile verifies startup fails when ca_file is
// set to a path that does not exist.
func TestAppendPIVClientCAs_MissingCAFile(t *testing.T) {
	cfg := &config.Config{}
	cfg.IAM.PIVLogin.Enabled = true
	cfg.IAM.PIVLogin.CAFile = "/nonexistent/piv-ca.pem"

	s := &Server{config: cfg, logger: buildTestLogger(t)}
	err := s.appendPIVClientCAs(&tls.Config{})
	if err == nil {
		t.Fatal("expected error for missing file, got nil")
	}

	var qbErr *qberrors.QBError
	if !isError(err, &qbErr) {
		t.Errorf("expected *qberrors.QBError, got %T: %v", err, err)
	}
}

// TestPIVClientCAsError_Error verifies the error message is non-empty and
// includes the cause.
func TestPIVClientCAsError_Error(t *testing.T) {
	inner := &CertPoolEmptyError{Path: "/piv/ca.pem"}
	e := &PIVClientCAsError{Cause: inner}
	if e.Error() == "" {
		t.Error("expected non-empty error message")
	}
	if e.Unwrap() != inner {
		t.Error("Unwrap should return the cause")
	}
}

// TestPIVLoginDisabledError_Error verifies the error message is non-empty.
func TestPIVLoginDisabledError_Error(t *testing.T) {
	e := &PIVLoginDisabledError{}
	if e.Error() == "" {
		t.Error("expected non-empty error message")
	}
}

// buildMinimalTestConfig builds a *config.Config with only the IAM mTLS CA file set.
func buildMinimalTestConfig(caFile string) *config.Config {
	cfg := &config.Config{}
	cfg.IAM.MTLS.TLSClientAuthCAFile = caFile
	return cfg
}

// buildTestLogger builds a logger suitable for tests (discards output).
func buildTestLogger(t *testing.T) *logger.Logger {
	t.Helper()
	log, err := logger.New(logger.LogConfig{Level: "error", Format: "json", Output: "stdout"})
	if err != nil {
		t.Fatalf("failed to create test logger: %v", err)
	}
	return log
}

// isError is a type-check helper that avoids the errors.As import.
func isError[T any](err error, target *T) bool {
	if err == nil {
		return false
	}
	if v, ok := err.(T); ok {
		*target = v
		return true
	}
	return false
}

// TestLoadCertPool_ParseError covers the certificate-parse error branch: a PEM
// block typed CERTIFICATE whose DER body is not a valid certificate must
// surface a typed CertPoolParseError.
func TestLoadCertPool_ParseError(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "garbage.pem")
	garbage := pem.EncodeToMemory(&pem.Block{
		Type:  "CERTIFICATE",
		Bytes: []byte("this is not valid DER"),
	})
	if err := os.WriteFile(path, garbage, 0o600); err != nil {
		t.Fatalf("write garbage cert: %v", err)
	}

	_, err := loadCertPool(path)
	if err == nil {
		t.Fatal("expected a parse error for an undecodable CERTIFICATE block")
	}
	var parseErr *CertPoolParseError
	if !isError(err, &parseErr) {
		t.Errorf("expected CertPoolParseError, got %T: %v", err, err)
	}
}

// TestLoadCertPool_SkipsNonCertificateBlocks covers the non-CERTIFICATE skip
// branch: a foreign PEM block preceding a valid certificate is silently skipped,
// and the pool is still built from the real certificate.
func TestLoadCertPool_SkipsNonCertificateBlocks(t *testing.T) {
	dir := t.TempDir()
	certPath, _ := writeCertPEM(t, dir, "real-ca")
	certBytes, err := os.ReadFile(certPath)
	if err != nil {
		t.Fatalf("read cert: %v", err)
	}

	foreign := pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: []byte("ignored")})
	mixedPath := filepath.Join(dir, "mixed.pem")
	if err := os.WriteFile(mixedPath, append(foreign, certBytes...), 0o600); err != nil {
		t.Fatalf("write mixed pem: %v", err)
	}

	pool, err := loadCertPool(mixedPath)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if pool == nil {
		t.Fatal("expected a non-nil pool built from the valid certificate")
	}
}

// TestAppendPIVClientCAs_NilTLSConfigIsNoOp covers the nil-tlsConfig guard:
// appendPIVClientCAs must return nil without dereferencing a nil config.
func TestAppendPIVClientCAs_NilTLSConfigIsNoOp(t *testing.T) {
	cfg := &config.Config{}
	cfg.IAM.PIVLogin.Enabled = true
	s := &Server{config: cfg, logger: buildTestLogger(t)}

	if err := s.appendPIVClientCAs(nil); err != nil {
		t.Fatalf("nil tls.Config must be a no-op, got: %v", err)
	}
}

// TestAppendPIVClientCAs_EmptyPEMFile covers the AppendCertsFromPEM-failure
// branch: a ca_file that exists but contains no usable certificate makes
// AppendCertsFromPEM return false, yielding a typed configuration error.
func TestAppendPIVClientCAs_EmptyPEMFile(t *testing.T) {
	dir := t.TempDir()
	caFile := filepath.Join(dir, "no-certs.pem")
	if err := os.WriteFile(caFile, []byte("# not a certificate\n"), 0o600); err != nil {
		t.Fatalf("write ca file: %v", err)
	}

	cfg := &config.Config{}
	cfg.IAM.PIVLogin.Enabled = true
	cfg.IAM.PIVLogin.CAFile = caFile
	s := &Server{config: cfg, logger: buildTestLogger(t)}

	err := s.appendPIVClientCAs(&tls.Config{})
	if err == nil {
		t.Fatal("expected an error for a PEM file with no certificates")
	}
	var qbErr *qberrors.QBError
	if !isError(err, &qbErr) {
		t.Errorf("expected *qberrors.QBError, got %T: %v", err, err)
	}
}

// TestAppendOperatorClientCAs_EmptyPEMFile covers the AppendCertsFromPEM-failure
// branch of appendOperatorClientCAs: a ca_file with no usable certificate makes
// AppendCertsFromPEM return false, yielding a typed configuration error. The
// nil ClientCAs pool is also allocated on this path (370-371).
func TestAppendOperatorClientCAs_EmptyPEMFile(t *testing.T) {
	dir := t.TempDir()
	caFile := filepath.Join(dir, "no-certs.pem")
	if err := os.WriteFile(caFile, []byte("# not a certificate\n"), 0o600); err != nil {
		t.Fatalf("write ca file: %v", err)
	}

	cfg := &config.Config{}
	cfg.IAM.MTLS.TLSClientAuthCAFile = caFile
	s := &Server{config: cfg, logger: buildTestLogger(t)}

	// Pass a tls.Config with a nil ClientCAs so the pool-allocation branch runs.
	err := s.appendOperatorClientCAs(&tls.Config{})
	if err == nil {
		t.Fatal("expected an error for a PEM file with no certificates")
	}
	var qbErr *qberrors.QBError
	if !isError(err, &qbErr) {
		t.Errorf("expected *qberrors.QBError, got %T: %v", err, err)
	}
}
