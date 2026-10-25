// Copyright (c) 2026 Jeremy Hahn
// Copyright (c) 2026 Automate The Things, LLC
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

package iam

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"errors"
	"math/big"
	"net/url"
	"testing"
	"time"

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"
)

// spiffeLeaf builds a self-signed leaf carrying the given SPIFFE URI SAN (or no
// URI SAN when spiffeURI is empty).
// spiffeLeaf issues a self-signed leaf carrying spiffeURI as its URI SAN (when
// set) and ou as its first Subject OU (when set).
func spiffeLeaf(t *testing.T, spiffeURI, ou string) *x509.Certificate {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("key: %v", err)
	}
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(42),
		Subject:      pkix.Name{CommonName: "leaf"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
	}
	if ou != "" {
		tmpl.Subject.OrganizationalUnit = []string{ou}
	}
	if spiffeURI != "" {
		u, err := url.Parse(spiffeURI)
		if err != nil {
			t.Fatalf("uri: %v", err)
		}
		tmpl.URIs = []*url.URL{u}
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatalf("create cert: %v", err)
	}
	cert, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatalf("parse cert: %v", err)
	}
	return cert
}

// stubResolver is an in-memory HomeRealmResolver for the core tests.
type stubResolver struct {
	byHost   map[string]string
	byDomain map[string]string
}

func (r *stubResolver) Resolve(_ context.Context, hints RealmHints) (string, error) {
	if hints.SNIHost != "" {
		if t, ok := r.byHost[hints.SNIHost]; ok {
			return t, nil
		}
	}
	if hints.Email != "" {
		for i := len(hints.Email) - 1; i >= 0; i-- {
			if hints.Email[i] == '@' {
				if t, ok := r.byDomain[hints.Email[i+1:]]; ok {
					return t, nil
				}
				break
			}
		}
	}
	return "", &HRDNotFoundError{}
}

// A SPIFFE cert resolves to its tenant when its trust domain and OU agree.
func TestTenantFromPeerCert_SPIFFESAN(t *testing.T) {
	cert := spiffeLeaf(t, "spiffe://tenant-acme/user/alice", "tenant-acme")

	got, err := TenantFromPeerCert(context.Background(), cert, "example.com", nil, RealmHints{})
	if err != nil {
		t.Fatalf("derive: %v", err)
	}
	if got != "tenant-acme" {
		t.Fatalf("tenant = %q, want tenant-acme", got)
	}
}

// A cert with no SPIFFE SAN falls through to HRD, which resolves via the host.
func TestTenantFromPeerCert_NoSANHRDHint(t *testing.T) {
	cert := spiffeLeaf(t, "", "") // no SPIFFE SAN
	hrd := &stubResolver{byHost: map[string]string{"acme.example.com": "tenant-acme"}}

	got, err := TenantFromPeerCert(context.Background(), cert, "example.com", hrd, RealmHints{SNIHost: "acme.example.com"})
	if err != nil {
		t.Fatalf("derive: %v", err)
	}
	if got != "tenant-acme" {
		t.Fatalf("tenant = %q, want tenant-acme", got)
	}
}

// No cert and no resolvable hint yields a typed unresolved error with no cause.
func TestTenantFromPeerCert_Unresolved(t *testing.T) {
	hrd := &stubResolver{} // empty directory

	_, err := TenantFromPeerCert(context.Background(), nil, "example.com", hrd, RealmHints{Email: "x@nobody.example"})
	var unresolved *TenantUnresolvedError
	if !errors.As(err, &unresolved) {
		t.Fatalf("expected *TenantUnresolvedError, got %v", err)
	}
	if unresolved.Cause != nil {
		t.Fatalf("cause should be nil for a no-cert miss, got %v", unresolved.Cause)
	}
}

// A presented cert with no SAN that HRD cannot resolve surfaces the
// ErrNoTenantSPIFFEID cause so a fail-closed caller can demand reissuance.
func TestTenantFromPeerCert_NoSANNoHintCarriesCause(t *testing.T) {
	cert := spiffeLeaf(t, "", "")
	hrd := &stubResolver{}

	_, err := TenantFromPeerCert(context.Background(), cert, "example.com", hrd, RealmHints{})
	var unresolved *TenantUnresolvedError
	if !errors.As(err, &unresolved) {
		t.Fatalf("expected *TenantUnresolvedError, got %v", err)
	}
	if !errors.Is(unresolved.Cause, qrdbsdk.ErrSpiffeNoTenantID) {
		t.Fatalf("cause = %v, want ErrNoTenantSPIFFEID", unresolved.Cause)
	}
}

// A foreign trust domain presents as an OU naming a different tenant: it fails
// closed immediately and does NOT fall through to HRD, even when HRD could
// resolve a tenant.
func TestTenantFromPeerCert_ForeignTrustDomain(t *testing.T) {
	cert := spiffeLeaf(t, "spiffe://evil.example/user/m", "tenant-acme")
	hrd := &stubResolver{byHost: map[string]string{"acme.example.com": "tenant-acme"}}

	_, err := TenantFromPeerCert(context.Background(), cert, "example.com", hrd, RealmHints{SNIHost: "acme.example.com"})
	var unresolved *TenantUnresolvedError
	if !errors.As(err, &unresolved) {
		t.Fatalf("expected *TenantUnresolvedError, got %v", err)
	}
	var mismatch *qrdbsdk.ClientCertificateTenantNameMismatchError
	if !errors.As(unresolved.Cause, &mismatch) {
		t.Fatalf("cause = %v, want *ClientCertificateTenantNameMismatchError", unresolved.Cause)
	}
}

// A nil cert with no HRD yields an unresolved error (nothing to resolve from).
func TestTenantFromPeerCert_NilCertNoHRD(t *testing.T) {
	_, err := TenantFromPeerCert(context.Background(), nil, "example.com", nil, RealmHints{})
	var unresolved *TenantUnresolvedError
	if !errors.As(err, &unresolved) {
		t.Fatalf("expected *TenantUnresolvedError, got %v", err)
	}
}

// TestTenantUnresolvedError_ErrorAndUnwrap exercises the typed error's message
// formatting (with and without a cause) and its Unwrap contract directly, since
// the message text is surfaced to operators on a fail-closed login.
func TestTenantUnresolvedError_ErrorAndUnwrap(t *testing.T) {
	// Without a cause: the bare message is returned and Unwrap is nil.
	bare := &TenantUnresolvedError{}
	if got := bare.Error(); got != "tenant could not be resolved from peer certificate or routing hints" {
		t.Fatalf("bare Error() = %q", got)
	}
	if bare.Unwrap() != nil {
		t.Fatalf("bare Unwrap() = %v, want nil", bare.Unwrap())
	}

	// With a cause: the cause text is appended and Unwrap exposes it for errors.Is.
	cause := errors.New("spiffe san malformed")
	wrapped := &TenantUnresolvedError{Cause: cause}
	want := "tenant could not be resolved from peer certificate or routing hints: spiffe san malformed"
	if got := wrapped.Error(); got != want {
		t.Fatalf("wrapped Error() = %q, want %q", got, want)
	}
	if !errors.Is(wrapped, cause) {
		t.Fatal("Unwrap must expose the underlying cause for errors.Is")
	}
}
