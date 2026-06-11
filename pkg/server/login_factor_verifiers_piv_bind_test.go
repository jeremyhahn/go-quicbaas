// Copyright (c) 2026 Jeremy Hahn
// Copyright (c) 2026 Automate The Things, LLC
//
// This file is part of go-quicbaas.

package server

import (
	"context"
	"crypto/x509"
	"errors"
	"testing"

	qrdbauth "github.com/jeremyhahn/go-qrdb/pkg/auth"
	qrdbtransport "github.com/jeremyhahn/go-qrdb/sdk/go/transport"
	xkmssdk "github.com/jeremyhahn/go-xkms/sdk/go"
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
