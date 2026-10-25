//go:build rest

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

package rest

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"math/big"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
	"github.com/jeremyhahn/go-quicbaas/pkg/testutil"
)

// fakeRealmResolver is an in-memory home-realm resolver for the login tenant
// derivation tests. It mirrors the shared-store directory behavior without a
// KVStore.
type fakeRealmResolver struct {
	byDomain map[string]string
	byHost   map[string]string
}

func (f *fakeRealmResolver) Resolve(_ context.Context, hints RealmHints) (string, error) {
	if hints.SNIHost != "" {
		if t, ok := f.byHost[hints.SNIHost]; ok {
			return t, nil
		}
	}
	if hints.Email != "" {
		if at := lastAt(hints.Email); at >= 0 {
			if t, ok := f.byDomain[hints.Email[at+1:]]; ok {
				return t, nil
			}
		}
	}
	return "", ErrLoginTenantUnresolved
}

func lastAt(s string) int {
	for i := len(s) - 1; i >= 0; i-- {
		if s[i] == '@' {
			return i
		}
	}
	return -1
}

func newDeriveTestServer(t *testing.T, trustDomain string, hrd HomeRealmResolver) *Server {
	t.Helper()
	log, err := logger.New(logger.LogConfig{Level: "error", Format: "json", Output: "stdout"})
	if err != nil {
		t.Fatalf("logger: %v", err)
	}
	cfg := DefaultConfig()
	cfg.Address = ":0"
	cfg.SPIFFETrustDomain = trustDomain
	cfg.HomeRealmResolver = hrd
	srv, err := NewServer(testutil.NewMockQuicBaaS(), cfg, log)
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	return srv
}

// spiffeLeafCert mints a self-signed leaf with the given SPIFFE SAN and, when
// ou is non-empty, a first OU naming the tenant go-qrdb checks the SAN against.
func spiffeLeafCert(t *testing.T, cn, spiffeURI, ou string) *x509.Certificate {
	t.Helper()
	key, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(7),
		Subject:      pkix.Name{CommonName: cn},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
	}
	if spiffeURI != "" {
		u, err := url.Parse(spiffeURI)
		if err != nil {
			t.Fatalf("uri: %v", err)
		}
		tmpl.URIs = []*url.URL{u}
	}
	if ou != "" {
		tmpl.Subject.OrganizationalUnit = []string{ou}
	}
	der, _ := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	cert, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	return cert
}

// Scenario (c): mTLS login resolves the tenant from the SPIFFE SAN.
func TestDeriveLoginTenant_FromSPIFFESAN(t *testing.T) {
	srv := newDeriveTestServer(t, "example.com", nil)
	cert := spiffeLeafCert(t, "alice", "spiffe://tenant-acme/user/alice", "tenant-acme")
	req := httptest.NewRequest(http.MethodPost, "/login", nil)

	got, err := srv.deriveLoginTenant(context.Background(), req, cert, "")
	if err != nil {
		t.Fatalf("derive: %v", err)
	}
	if got != "tenant-acme" {
		t.Fatalf("tenant = %q, want tenant-acme", got)
	}
}

// The SO / bootstrap cert (tenant/system) resolves the system tenant.
func TestDeriveLoginTenant_SystemBootstrap(t *testing.T) {
	srv := newDeriveTestServer(t, "example.com", nil)
	cert := spiffeLeafCert(t, "so", "spiffe://system/user/so", "system")
	req := httptest.NewRequest(http.MethodPost, "/login", nil)

	got, err := srv.deriveLoginTenant(context.Background(), req, cert, "")
	if err != nil {
		t.Fatalf("derive: %v", err)
	}
	if got != "system" {
		t.Fatalf("tenant = %q, want system", got)
	}
}

// Scenario (d): a SPIFFE-less (browser) login resolves via home-realm
// discovery from the email domain.
func TestDeriveLoginTenant_HRDBrowser(t *testing.T) {
	hrd := &fakeRealmResolver{byDomain: map[string]string{"acme.example": "tenant-acme"}}
	srv := newDeriveTestServer(t, "example.com", hrd)
	req := httptest.NewRequest(http.MethodPost, "/login", nil)

	got, err := srv.deriveLoginTenant(context.Background(), req, nil, "alice@acme.example")
	if err != nil {
		t.Fatalf("derive: %v", err)
	}
	if got != "tenant-acme" {
		t.Fatalf("tenant = %q, want tenant-acme", got)
	}
}

// Scenario (f): a broker cert with NO SPIFFE SAN and no resolvable hint fails
// closed — it is never defaulted to the system tenant.
func TestDeriveLoginTenant_FailClosed_NoSANNoHint(t *testing.T) {
	hrd := &fakeRealmResolver{} // empty directory
	srv := newDeriveTestServer(t, "example.com", hrd)
	cert := spiffeLeafCert(t, "broker", "", "") // no SPIFFE SAN
	req := httptest.NewRequest(http.MethodPost, "/login", nil)

	got, err := srv.deriveLoginTenant(context.Background(), req, cert, "")
	if err == nil {
		t.Fatalf("expected fail-closed, got tenant %q", got)
	}
	if got == "system" {
		t.Fatal("must NEVER default a non-bootstrap login to the system tenant")
	}
}

// A foreign trust domain on the cert fails closed.
func TestDeriveLoginTenant_ForeignTrustDomain_FailsClosed(t *testing.T) {
	srv := newDeriveTestServer(t, "example.com", nil)
	cert := spiffeLeafCert(t, "m", "spiffe://evil.example/user/m", "")
	req := httptest.NewRequest(http.MethodPost, "/login", nil)

	if _, err := srv.deriveLoginTenant(context.Background(), req, cert, ""); err == nil {
		t.Fatal("expected fail-closed on foreign trust domain")
	}
}
