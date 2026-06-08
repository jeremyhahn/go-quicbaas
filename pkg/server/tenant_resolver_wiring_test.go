//go:build iam

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

package server

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"math/big"
	"net/url"
	"testing"
	"time"

	qrdbspiffe "github.com/jeremyhahn/go-qrdb/pkg/auth/spiffe"

	"github.com/jeremyhahn/go-quicbaas/pkg/config"
	"github.com/jeremyhahn/go-quicbaas/pkg/iam"
)

// buildSPIFFELeaf builds a self-signed leaf certificate carrying a tenant-scoped
// SPIFFE URI SAN for the given trust domain + tenant, so TenantFromPeerCert can
// derive the tenant from the verified peer cert.
func buildSPIFFELeaf(t *testing.T, trustDomain, tenantID string) *x509.Certificate {
	t.Helper()
	id := qrdbspiffe.ID{
		TrustDomain: trustDomain,
		TenantID:    tenantID,
		Kind:        qrdbspiffe.KindUser,
		Name:        "alice",
	}
	u, err := url.Parse(id.String())
	if err != nil {
		t.Fatalf("parse spiffe id: %v", err)
	}
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(time.Now().UnixNano()),
		Subject:      pkix.Name{CommonName: "alice"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
		URIs:         []*url.URL{u},
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

// tenantContextFunc derives the tenant from a SPIFFE peer cert and stamps it
// onto the context; it is permissive and never rejects.
func TestTenantContextFunc(t *testing.T) {
	const trustDomain = "example.com"

	newServer := func() *Server {
		cfg := &config.Config{}
		cfg.IAM.SPIFFETrustDomain = trustDomain
		return &Server{logger: buildTestLogger(t), config: cfg}
	}

	t.Run("nil cert returns context unchanged", func(t *testing.T) {
		fn := newServer().tenantContextFunc()
		ctx := context.Background()
		got := fn(ctx, nil)
		if iam.TenantFromContext(got) != "" {
			t.Fatal("nil cert must not stamp a tenant")
		}
	})

	t.Run("matching trust domain stamps tenant", func(t *testing.T) {
		fn := newServer().tenantContextFunc()
		cert := buildSPIFFELeaf(t, trustDomain, "acme")
		got := fn(context.Background(), cert)
		if tenant := iam.TenantFromContext(got); tenant != "acme" {
			t.Fatalf("tenant = %q, want acme", tenant)
		}
	})

	t.Run("foreign trust domain is not rejected and stamps nothing", func(t *testing.T) {
		fn := newServer().tenantContextFunc()
		// A cert pinned to a different trust domain fails closed inside
		// TenantFromPeerCert; the permissive hook returns the input ctx unchanged.
		cert := buildSPIFFELeaf(t, "other.example", "acme")
		ctx := context.Background()
		got := fn(ctx, cert)
		if iam.TenantFromContext(got) != "" {
			t.Fatal("foreign cert must not stamp a tenant")
		}
	})

	t.Run("cert without SPIFFE SAN stamps nothing", func(t *testing.T) {
		fn := newServer().tenantContextFunc()
		cert := buildSelfSignedRoot(t, "no-spiffe")
		got := fn(context.Background(), cert)
		if iam.TenantFromContext(got) != "" {
			t.Fatal("non-SPIFFE cert must not stamp a tenant")
		}
	})
}
