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

//go:build rest

package server

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"os"
	"path/filepath"
	"testing"
	"time"
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
	e := &MTLSConfigError{Field: "tpm_ek_roots_file", Cause: inner}

	got := e.Error()
	if got != "iam.mtls.tpm_ek_roots_file: "+inner.Error() {
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
