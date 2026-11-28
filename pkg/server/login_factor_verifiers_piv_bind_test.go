//go:build !integration

// Copyright (c) 2026 Jeremy Hahn
// Copyright (c) 2026 Automate The Things, LLC
//
// This file is part of go-quicbaas.

package server

import (
	"context"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"os"
	"path/filepath"
	"testing"

	qrdbauth "github.com/jeremyhahn/go-qrdb/pkg/auth"
	qrdbtransport "github.com/jeremyhahn/go-qrdb/sdk/go/transport"
	xkmssdk "github.com/jeremyhahn/go-xkms/sdk/go"

	"github.com/jeremyhahn/go-quicbaas/pkg/config"
)

// pivBindFakeCA is a minimal TrustedCA whose CABundleCertPool result is fully
// controllable so bindPIVLoginVerifier's branches can be exercised. Only that
// one method is overridden; every other TrustedCA method is left nil and must
// not be called by bindPIVLoginVerifier.
type pivBindFakeCA struct {
	xkmssdk.TrustedCA
	pool *x509.CertPool
	err  error
}

func (c *pivBindFakeCA) CABundleCertPool() (*x509.CertPool, error) {
	return c.pool, c.err
}

// pivVerifierFromServer extracts the late-bound PIV verifier shell.
func pivVerifierFromServer(t *testing.T, s *Server) *lateBoundPIVVerifier {
	t.Helper()
	s.buildLoginFactorVerifiers()
	for _, v := range s.loginFactorVerifiers {
		if pv, ok := v.(*lateBoundPIVVerifier); ok {
			return pv
		}
	}
	t.Fatal("PIV verifier shell not found")
	return nil
}

// assertPIVUnbound proves the shell fails closed (verifier not attached).
func assertPIVUnbound(t *testing.T, v *lateBoundPIVVerifier) {
	t.Helper()
	err := v.Verify(context.Background(), &qrdbauth.AuthFactor{Type: qrdbauth.AuthFactorPIV},
		qrdbtransport.VerifyChallenge{Type: qrdbtransport.FactorTypePIV})
	var unbound *UnboundPIVVerifierError
	if !errors.As(err, &unbound) {
		t.Fatalf("expected UnboundPIVVerifierError, got %T: %v", err, err)
	}
}

func TestBindPIVLoginVerifier_NilCA(t *testing.T) {
	s := newTestServer(t)
	v := pivVerifierFromServer(t, s)
	s.xkmsCA = nil
	s.bindPIVLoginVerifier(v)
	assertPIVUnbound(t, v)
}

func TestBindPIVLoginVerifier_CertPoolError(t *testing.T) {
	s := newTestServer(t)
	v := pivVerifierFromServer(t, s)
	s.xkmsCA = &pivBindFakeCA{err: errors.New("roots unavailable")}
	s.bindPIVLoginVerifier(v)
	assertPIVUnbound(t, v)
}

func TestBindPIVLoginVerifier_NilPool(t *testing.T) {
	s := newTestServer(t)
	v := pivVerifierFromServer(t, s)
	s.xkmsCA = &pivBindFakeCA{pool: nil}
	s.bindPIVLoginVerifier(v)
	assertPIVUnbound(t, v)
}

func TestBindPIVLoginVerifier_Success(t *testing.T) {
	s := newTestServer(t)
	v := pivVerifierFromServer(t, s)

	root, _ := newTestPIVRoot(t)
	pool := x509.NewCertPool()
	pool.AddCert(root)
	s.xkmsCA = &pivBindFakeCA{pool: pool}

	s.bindPIVLoginVerifier(v)

	// The verifier is now bound: a Verify call must reach the inner xkms
	// verifier (and reject the missing fields) rather than fail closed as
	// unbound.
	err := v.Verify(context.Background(), &qrdbauth.AuthFactor{Type: qrdbauth.AuthFactorPIV},
		qrdbtransport.VerifyChallenge{Type: qrdbtransport.FactorTypePIV})
	if err == nil {
		t.Fatal("expected delegated rejection from bound verifier")
	}
	var unbound *UnboundPIVVerifierError
	if errors.As(err, &unbound) {
		t.Fatalf("verifier should be bound, but got unbound error: %v", err)
	}
}

// bindLoginFactorVerifiers must route the PIV shell through bindPIVLoginVerifier
// when iterating the verifier set.
func TestBindLoginFactorVerifiers_RoutesPIV(t *testing.T) {
	s := newTestServer(t)
	s.buildLoginFactorVerifiers()

	root, _ := newTestPIVRoot(t)
	pool := x509.NewCertPool()
	pool.AddCert(root)
	s.xkmsCA = &pivBindFakeCA{pool: pool}

	s.bindLoginFactorVerifiers(&iamComponents{})

	for _, v := range s.loginFactorVerifiers {
		if pv, ok := v.(*lateBoundPIVVerifier); ok {
			err := pv.Verify(context.Background(), &qrdbauth.AuthFactor{Type: qrdbauth.AuthFactorPIV},
				qrdbtransport.VerifyChallenge{Type: qrdbtransport.FactorTypePIV})
			var unbound *UnboundPIVVerifierError
			if errors.As(err, &unbound) {
				t.Fatal("PIV verifier should have been bound by bindLoginFactorVerifiers")
			}
			return
		}
	}
	t.Fatal("PIV verifier shell not found")
}

// writeTestPIVRootPEM writes the given certificate as a PEM CA bundle file
// and returns its path.
func writeTestPIVRootPEM(t *testing.T, cert *x509.Certificate) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "piv-ca.crt")
	pemData := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: cert.Raw})
	if err := os.WriteFile(path, pemData, 0o600); err != nil {
		t.Fatalf("write PIV CA pem: %v", err)
	}
	return path
}

// loadTestTLSCAFile points security.tls_ca_file at root and loads the shared
// client trust-anchor set, as initializeCA does at boot.
func loadTestTLSCAFile(t *testing.T, s *Server, root *x509.Certificate) {
	t.Helper()
	s.config.Security.TLSCAFile = writeTestPIVRootPEM(t, root)
	if err := s.loadOperatorClientTrustAnchors(); err != nil {
		t.Fatalf("loadOperatorClientTrustAnchors: %v", err)
	}
}

// TestBindPIVLoginVerifier_TrustsTLSCAFile proves the PIV login verifier
// trusts security.tls_ca_file: a certificate chaining to it verifies against
// the bound roots, and one from an unlisted CA does not.
func TestBindPIVLoginVerifier_TrustsTLSCAFile(t *testing.T) {
	s := newTestServer(t)
	s.config = &config.Config{}
	v := pivVerifierFromServer(t, s)

	daemonRoot, _ := newTestPIVRoot(t)
	pool := x509.NewCertPool()
	pool.AddCert(daemonRoot)
	s.xkmsCA = &pivBindFakeCA{pool: pool}

	operatorRoot, _ := newTestPIVRoot(t)
	unlistedRoot, _ := newTestPIVRoot(t)
	s.config.IAM.PIVLogin.Enabled = true
	loadTestTLSCAFile(t, s, operatorRoot)

	s.bindPIVLoginVerifier(v)

	opts := x509.VerifyOptions{Roots: pool, KeyUsages: []x509.ExtKeyUsage{x509.ExtKeyUsageAny}}
	if _, err := operatorRoot.Verify(opts); err != nil {
		t.Fatalf("a certificate from security.tls_ca_file must verify against the PIV roots: %v", err)
	}
	if _, err := unlistedRoot.Verify(opts); err == nil {
		t.Fatal("a certificate from an unlisted CA must not verify against the PIV roots")
	}
}

// TestBindPIVLoginVerifier_DisabledIgnoresTLSCAFile proves the PIV roots are
// left at the daemon CA when PIV login is disabled.
func TestBindPIVLoginVerifier_DisabledIgnoresTLSCAFile(t *testing.T) {
	s := newTestServer(t)
	s.config = &config.Config{}
	v := pivVerifierFromServer(t, s)

	daemonRoot, _ := newTestPIVRoot(t)
	pool := x509.NewCertPool()
	pool.AddCert(daemonRoot)
	s.xkmsCA = &pivBindFakeCA{pool: pool}

	operatorRoot, _ := newTestPIVRoot(t)
	loadTestTLSCAFile(t, s, operatorRoot)

	before := pool.Clone()
	s.bindPIVLoginVerifier(v)
	if !pool.Equal(before) {
		t.Fatal("disabled PIV login must not add security.tls_ca_file to the PIV roots")
	}
}

// TestBindPIVLoginVerifier_IncludesOperatorRoots proves the bound verifier's
// pool includes the operator PIV root: binding succeeds and the inner
// verifier is attached with the merged pool (delegated rejection, not
// unbound).
func TestBindPIVLoginVerifier_IncludesOperatorRoots(t *testing.T) {
	s := newTestServer(t)
	s.config = &config.Config{}
	v := pivVerifierFromServer(t, s)

	daemonRoot, _ := newTestPIVRoot(t)
	pool := x509.NewCertPool()
	pool.AddCert(daemonRoot)
	s.xkmsCA = &pivBindFakeCA{pool: pool}

	operatorRoot, _ := newTestPIVRoot(t)
	s.config.IAM.PIVLogin.Enabled = true
	loadTestTLSCAFile(t, s, operatorRoot)

	before := pool.Clone()
	s.bindPIVLoginVerifier(v)

	if pool.Equal(before) {
		t.Fatal("bindPIVLoginVerifier must merge the operator PIV root into the trust pool")
	}

	err := v.Verify(context.Background(), &qrdbauth.AuthFactor{Type: qrdbauth.AuthFactorPIV},
		qrdbtransport.VerifyChallenge{Type: qrdbtransport.FactorTypePIV})
	var unbound *UnboundPIVVerifierError
	if errors.As(err, &unbound) {
		t.Fatalf("verifier should be bound with merged roots, got unbound: %v", err)
	}
}
