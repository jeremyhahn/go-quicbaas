package server

import (
	"context"
	"crypto/x509"
	"errors"
	"math/big"
	"testing"
)

type caLookupFailedError struct{}

func (e *caLookupFailedError) Error() string { return "ca lookup failed" }

func TestCARevocationChecker_ReportsTheCAAnswer(t *testing.T) {
	ca := newFakeTrustedCA(t)
	checker := &caRevocationChecker{server: &Server{xkmsCA: ca}}
	peer := &x509.Certificate{SerialNumber: big.NewInt(0x1991)}

	revoked, err := checker.IsCertRevoked(context.Background(), peer)
	if err != nil || revoked {
		t.Fatalf("unrevoked certificate: revoked=%v err=%v", revoked, err)
	}

	if err := ca.Revoke(context.Background(), peer.SerialNumber, 0); err != nil {
		t.Fatalf("revoke: %v", err)
	}
	revoked, err = checker.IsCertRevoked(context.Background(), peer)
	if err != nil || !revoked {
		t.Fatalf("revoked certificate: revoked=%v err=%v", revoked, err)
	}
}

func TestCARevocationChecker_NoCAIsRefused(t *testing.T) {
	checker := &caRevocationChecker{server: &Server{}}

	revoked, err := checker.IsCertRevoked(context.Background(), &x509.Certificate{SerialNumber: big.NewInt(1)})

	var want *CertRevocationAuthorityUnavailableError
	if !errors.As(err, &want) {
		t.Fatalf("want CertRevocationAuthorityUnavailableError, got %v", err)
	}
	if revoked {
		t.Fatal("an error must not report the certificate as revoked")
	}
}

func TestCARevocationChecker_MissingSerialIsRefused(t *testing.T) {
	checker := &caRevocationChecker{server: &Server{xkmsCA: newFakeTrustedCA(t)}}

	for name, peer := range map[string]*x509.Certificate{
		"nil certificate": nil,
		"no serial":       {},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := checker.IsCertRevoked(context.Background(), peer)

			var want *CertRevocationSerialMissingError
			if !errors.As(err, &want) {
				t.Fatalf("want CertRevocationSerialMissingError, got %v", err)
			}
		})
	}
}

func TestCARevocationChecker_LookupErrorIsRefused(t *testing.T) {
	ca := newFakeTrustedCA(t)
	cause := &caLookupFailedError{}
	ca.isRevokeErr = cause
	checker := &caRevocationChecker{server: &Server{xkmsCA: ca}}

	_, err := checker.IsCertRevoked(context.Background(), &x509.Certificate{SerialNumber: big.NewInt(1)})

	var want *CertRevocationLookupError
	if !errors.As(err, &want) {
		t.Fatalf("want CertRevocationLookupError, got %v", err)
	}
	if !errors.Is(err, cause) {
		t.Fatalf("the CA error must be wrapped, got %v", err)
	}
}
