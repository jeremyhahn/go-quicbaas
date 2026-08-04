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

// TestAppendOperatorPIVLoginRoots_AppendsConfiguredCA proves the operator's
// iam.piv_login.ca_file roots are appended to the verifier trust pool so PIV
// factor logins verify operator-PKI certificates — the same posture as the
// REST PIV login path.
func TestAppendOperatorPIVLoginRoots_AppendsConfiguredCA(t *testing.T) {
	s := newTestServer(t)
	s.config = &config.Config{}
	operatorRoot, _ := newTestPIVRoot(t)
	s.config.IAM.PIVLogin.Enabled = true
	s.config.IAM.PIVLogin.CAFile = writeTestPIVRootPEM(t, operatorRoot)

	pool := x509.NewCertPool()
	s.appendOperatorPIVLoginRoots(pool)

	if pool.Equal(x509.NewCertPool()) {
		t.Fatal("operator PIV root must be appended to the trust pool")
	}
}

// TestAppendOperatorPIVLoginRoots_NoOpWhenDisabledOrUnset proves the pool is
// untouched when PIV login is disabled or no ca_file is configured.
func TestAppendOperatorPIVLoginRoots_NoOpWhenDisabledOrUnset(t *testing.T) {
	s := newTestServer(t)
	s.config = &config.Config{}
	operatorRoot, _ := newTestPIVRoot(t)
	caFile := writeTestPIVRootPEM(t, operatorRoot)

	// Disabled with a configured file.
	s.config.IAM.PIVLogin.Enabled = false
	s.config.IAM.PIVLogin.CAFile = caFile
	pool := x509.NewCertPool()
	s.appendOperatorPIVLoginRoots(pool)
	if !pool.Equal(x509.NewCertPool()) {
		t.Fatal("disabled PIV login must not modify the trust pool")
	}

	// Enabled without a file.
	s.config.IAM.PIVLogin.Enabled = true
	s.config.IAM.PIVLogin.CAFile = ""
	s.appendOperatorPIVLoginRoots(pool)
	if !pool.Equal(x509.NewCertPool()) {
		t.Fatal("empty ca_file must not modify the trust pool")
	}
}

// TestAppendOperatorPIVLoginRoots_BadFileLeavesPoolUnchanged proves an
// unreadable or non-PEM ca_file is skipped defensively (daemon roots remain
// in effect; startup validation is the enforcement point).
func TestAppendOperatorPIVLoginRoots_BadFileLeavesPoolUnchanged(t *testing.T) {
	s := newTestServer(t)
	s.config = &config.Config{}
	s.config.IAM.PIVLogin.Enabled = true

	// Missing file.
	s.config.IAM.PIVLogin.CAFile = filepath.Join(t.TempDir(), "missing.crt")
	pool := x509.NewCertPool()
	s.appendOperatorPIVLoginRoots(pool)
	if !pool.Equal(x509.NewCertPool()) {
		t.Fatal("missing ca_file must not modify the trust pool")
	}

	// Garbage PEM.
	garbage := filepath.Join(t.TempDir(), "garbage.crt")
	if err := os.WriteFile(garbage, []byte("not a pem"), 0o600); err != nil {
		t.Fatalf("write garbage: %v", err)
	}
	s.config.IAM.PIVLogin.CAFile = garbage
	s.appendOperatorPIVLoginRoots(pool)
	if !pool.Equal(x509.NewCertPool()) {
		t.Fatal("non-PEM ca_file must not modify the trust pool")
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
	s.config.IAM.PIVLogin.CAFile = writeTestPIVRootPEM(t, operatorRoot)

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

