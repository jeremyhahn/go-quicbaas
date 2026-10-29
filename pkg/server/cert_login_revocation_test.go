package server

import (
	"context"
	"crypto/x509"
	"errors"
	"math/big"
	"sync"
	"testing"

	xkmssdk "github.com/jeremyhahn/go-xkms/sdk/go"
)

type caLookupFailedError struct{}

func (e *caLookupFailedError) Error() string { return "ca lookup failed" }

func TestCARevocationChecker_ReportsTheCAAnswer(t *testing.T) {
	ca := newFakeTrustedCA(t)
	checker := &caRevocationChecker{server: serverWithCA(ca)}
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
	checker := &caRevocationChecker{server: serverWithCA(newFakeTrustedCA(t))}

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
	checker := &caRevocationChecker{server: serverWithCA(ca)}

	_, err := checker.IsCertRevoked(context.Background(), &x509.Certificate{SerialNumber: big.NewInt(1)})

	var want *CertRevocationLookupError
	if !errors.As(err, &want) {
		t.Fatalf("want CertRevocationLookupError, got %v", err)
	}
	if !errors.Is(err, cause) {
		t.Fatalf("the CA error must be wrapped, got %v", err)
	}
}

// serverWithCA returns a Server holding ca as its Certificate Authority.
func serverWithCA(ca xkmssdk.TrustedCA) *Server {
	s := &Server{}
	s.setCA(ca)
	return s
}

// TestCARevocationChecker_ConcurrentUpdateCA runs logins against a CA that
// UpdateCA swaps at the same time; under -race it fails if CA() and
// UpdateCA share the field without synchronisation.
func TestCARevocationChecker_ConcurrentUpdateCA(t *testing.T) {
	adapter, err := newCAServiceAdapter(newFakeTrustedCA(t))
	if err != nil {
		t.Fatalf("newCAServiceAdapter: %v", err)
	}
	srv := serverWithCA(newFakeTrustedCA(t))
	srv.caAdapter = adapter
	checker := &caRevocationChecker{server: srv}
	peer := &x509.Certificate{SerialNumber: big.NewInt(7)}
	next := newFakeTrustedCA(t)

	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		for range 200 {
			if err := srv.UpdateCA(next); err != nil {
				t.Errorf("UpdateCA: %v", err)
				return
			}
		}
	}()
	go func() {
		defer wg.Done()
		for range 200 {
			if _, err := checker.IsCertRevoked(context.Background(), peer); err != nil {
				t.Errorf("IsCertRevoked: %v", err)
				return
			}
		}
	}()
	wg.Wait()
	if srv.CA() != next {
		t.Error("CA() does not return the CA UpdateCA stored")
	}
}
