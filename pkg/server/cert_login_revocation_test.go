package server

import (
	"context"
	"crypto/x509"
	"errors"
	"math/big"
	"sync"
	"sync/atomic"
	"testing"
)

type caLookupFailedError struct{}

func (e *caLookupFailedError) Error() string { return "ca lookup failed" }

// newServerWithCA returns a Server whose CA adapter holds ca, the way Start
// wires it when TLS is enabled.
func newServerWithCA(t *testing.T, ca *fakeTrustedCA) *Server {
	t.Helper()
	srv := &Server{xkmsCA: ca}
	wireCAAdapter(t, srv)
	return srv
}

// wireCAAdapter wires srv's CA adapter over srv.xkmsCA, as Start does, so the
// certificate-login revocation check has the CA to ask.
func wireCAAdapter(t *testing.T, srv *Server) {
	t.Helper()
	adapter, err := newCAServiceAdapter(srv.xkmsCA)
	if err != nil {
		t.Fatalf("newCAServiceAdapter: %v", err)
	}
	srv.caAdapter = adapter
	srv.caWired.Store(true)
}

func TestCARevocationChecker_ReportsTheCAAnswer(t *testing.T) {
	ca := newFakeTrustedCA(t)
	checker := &caRevocationChecker{server: newServerWithCA(t, ca)}
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
	// No CA adapter is wired: TLS is disabled.
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
	checker := &caRevocationChecker{server: newServerWithCA(t, newFakeTrustedCA(t))}

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
	checker := &caRevocationChecker{server: newServerWithCA(t, ca)}

	_, err := checker.IsCertRevoked(context.Background(), &x509.Certificate{SerialNumber: big.NewInt(1)})

	var want *CertRevocationLookupError
	if !errors.As(err, &want) {
		t.Fatalf("want CertRevocationLookupError, got %v", err)
	}
	if !errors.Is(err, cause) {
		t.Fatalf("the CA error must be wrapped, got %v", err)
	}
}

// newOpposingCAs returns two CAs that give opposite answers for peer: a has
// not revoked it, b has.
func newOpposingCAs(t *testing.T, peer *x509.Certificate) (a, b *fakeTrustedCA) {
	t.Helper()
	a = newFakeTrustedCA(t)
	b = newFakeTrustedCA(t)
	if err := b.Revoke(context.Background(), peer.SerialNumber, 0); err != nil {
		t.Fatalf("revoke: %v", err)
	}
	return a, b
}

func TestCARevocationChecker_ConsultsCAAdoptedByUpdateCA(t *testing.T) {
	peer := &x509.Certificate{SerialNumber: big.NewInt(0x2120)}
	a, b := newOpposingCAs(t, peer)
	srv := newServerWithCA(t, a)
	checker := &caRevocationChecker{server: srv}

	revoked, err := checker.IsCertRevoked(context.Background(), peer)
	if err != nil || revoked {
		t.Fatalf("CA A: revoked=%v err=%v, want false and nil", revoked, err)
	}

	if err := srv.UpdateCA(b); err != nil {
		t.Fatalf("UpdateCA: %v", err)
	}
	revoked, err = checker.IsCertRevoked(context.Background(), peer)
	if err != nil || !revoked {
		t.Fatalf("CA B adopted by UpdateCA: revoked=%v err=%v, want true and nil", revoked, err)
	}
}

func TestCARevocationChecker_ConcurrentUpdateCAIsRaceFree(t *testing.T) {
	const n = 64
	peer := &x509.Certificate{SerialNumber: big.NewInt(0x2140)}
	a, b := newOpposingCAs(t, peer)
	srv := newServerWithCA(t, a)
	checker := &caRevocationChecker{server: srv}

	errs := make(chan error, 2*n)
	var wg sync.WaitGroup
	wg.Add(n + 1)
	go func() {
		defer wg.Done()
		for i := range n {
			next := a
			if i%2 == 0 {
				next = b
			}
			if err := srv.UpdateCA(next); err != nil {
				errs <- err
			}
		}
	}()
	for range n {
		go func() {
			defer wg.Done()
			// Both answers are valid: A reports false, B reports true.
			if _, err := checker.IsCertRevoked(context.Background(), peer); err != nil {
				errs <- err
			}
		}()
	}
	wg.Wait()
	close(errs)

	for err := range errs {
		t.Errorf("concurrent call: %v", err)
	}
}

// TestCARevocationChecker_LoginBeforeCAIsRefused covers a certificate login
// served by the unified listeners before initializeCA has published the CA:
// the adapter NewServer built holds no CA, so the check fails closed.
func TestCARevocationChecker_LoginBeforeCAIsRefused(t *testing.T) {
	checker := &caRevocationChecker{server: &Server{caAdapter: newPendingCAServiceAdapter()}}

	revoked, err := checker.IsCertRevoked(context.Background(), &x509.Certificate{SerialNumber: big.NewInt(1)})

	var want *CertRevocationAuthorityUnavailableError
	if !errors.As(err, &want) {
		t.Fatalf("want CertRevocationAuthorityUnavailableError, got %v", err)
	}
	if revoked {
		t.Fatal("an error must not report the certificate as revoked")
	}
}

// TestCARevocationChecker_LoginsDuringStartupAreRaceFree runs certificate
// logins while initializeCA publishes the CA. Each login sees either no CA
// and is refused, or the CA's answer; none may see a torn adapter.
func TestCARevocationChecker_LoginsDuringStartupAreRaceFree(t *testing.T) {
	const (
		readers = 16
		calls   = 200
	)
	peer := &x509.Certificate{SerialNumber: big.NewInt(0x2140)}
	ca := newFakeTrustedCA(t)
	if err := ca.Revoke(context.Background(), peer.SerialNumber, 0); err != nil {
		t.Fatalf("revoke: %v", err)
	}
	srv := &Server{caAdapter: newPendingCAServiceAdapter()}
	checker := &caRevocationChecker{server: srv}

	var unexpected atomic.Int64
	var wg sync.WaitGroup
	wg.Add(readers)
	for range readers {
		go func() {
			defer wg.Done()
			for range calls {
				revoked, err := checker.IsCertRevoked(context.Background(), peer)
				var unavailable *CertRevocationAuthorityUnavailableError
				refused := errors.As(err, &unavailable) && !revoked
				answered := err == nil && revoked
				if !refused && !answered {
					unexpected.Add(1)
				}
			}
		}()
	}
	srv.adoptCA(ca)
	wg.Wait()

	if n := unexpected.Load(); n != 0 {
		t.Fatalf("%d logins saw neither a refusal nor the CA's answer", n)
	}
	revoked, err := checker.IsCertRevoked(context.Background(), peer)
	if err != nil || !revoked {
		t.Fatalf("after adoptCA: revoked=%v err=%v", revoked, err)
	}
}
