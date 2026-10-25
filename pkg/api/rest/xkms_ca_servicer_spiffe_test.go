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
	"testing"

	qrdbspiffe "github.com/jeremyhahn/go-qrdb/pkg/auth/spiffe"
	xkmssdk "github.com/jeremyhahn/go-xkms/sdk/go"
)

// TestXKMSCAServicer_SignOptionsWithSPIFFE verifies the REST servicer folds the
// operator-configured trust domain, the request tenant, and the profile-derived
// SPIFFE kind into the CA sign options so the issued client certificate carries
// the tenant-scoped SPIFFE URI SAN.
func TestXKMSCAServicer_SignOptionsWithSPIFFE(t *testing.T) {
	t.Run("client profile stamps user kind", func(t *testing.T) {
		a := &xkmsCAServicer{spiffeTrustDomain: "example.com"}
		opts := a.signOptionsWithSPIFFE("client", 30, "acme-corp")
		if opts.SPIFFETrustDomain != "example.com" {
			t.Fatalf("SPIFFETrustDomain = %q", opts.SPIFFETrustDomain)
		}
		if opts.TenantID != "acme-corp" {
			t.Fatalf("TenantID = %q", opts.TenantID)
		}
		if opts.SPIFFEKind != qrdbspiffe.KindUser {
			t.Fatalf("SPIFFEKind = %q, want user", opts.SPIFFEKind)
		}
		if opts.ValidityDays != 30 {
			t.Fatalf("ValidityDays = %d", opts.ValidityDays)
		}
	})

	t.Run("server profile stamps svc kind", func(t *testing.T) {
		a := &xkmsCAServicer{spiffeTrustDomain: "example.com"}
		opts := a.signOptionsWithSPIFFE("server", 0, "acme-corp")
		if opts.SPIFFEKind != qrdbspiffe.KindService {
			t.Fatalf("SPIFFEKind = %q, want svc", opts.SPIFFEKind)
		}
	})

	t.Run("no trust domain omits SPIFFE", func(t *testing.T) {
		a := &xkmsCAServicer{}
		opts := a.signOptionsWithSPIFFE("client", 0, "acme-corp")
		if opts.SPIFFETrustDomain != "" || opts.SPIFFEKind != "" || opts.TenantID != "acme-corp" {
			t.Fatalf("expected no SPIFFE fields and the tenant kept for its OU, got %+v", opts)
		}
	})

	t.Run("no tenant omits SPIFFE", func(t *testing.T) {
		a := &xkmsCAServicer{spiffeTrustDomain: "example.com"}
		opts := a.signOptionsWithSPIFFE("client", 0, "")
		if opts.SPIFFETrustDomain != "" || opts.TenantID != "" {
			t.Fatalf("expected no SPIFFE fields without tenant, got %+v", opts)
		}
	})
}

// TestXKMSCAServicer_ApplyKeypairSPIFFE verifies the REST servicer folds the
// operator-configured trust domain, the request tenant, and the profile-derived
// SPIFFE kind onto the generate-and-distribute (keypair) CertificateRequest, so
// the issued client certificate carries the tenant-scoped SPIFFE URI SAN
// symmetric with the CSR-sign path.
func TestXKMSCAServicer_ApplyKeypairSPIFFE(t *testing.T) {
	t.Run("client profile stamps user kind", func(t *testing.T) {
		a := &xkmsCAServicer{spiffeTrustDomain: "example.com"}
		req := &xkmssdk.TrustedCACertificateRequest{}
		a.applyKeypairSPIFFE(req, "client", "acme-corp")
		if req.SPIFFETrustDomain != "example.com" {
			t.Fatalf("SPIFFETrustDomain = %q", req.SPIFFETrustDomain)
		}
		if req.TenantID != "acme-corp" {
			t.Fatalf("TenantID = %q", req.TenantID)
		}
		if req.SPIFFEKind != qrdbspiffe.KindUser {
			t.Fatalf("SPIFFEKind = %q, want user", req.SPIFFEKind)
		}
	})

	t.Run("server profile stamps svc kind", func(t *testing.T) {
		a := &xkmsCAServicer{spiffeTrustDomain: "example.com"}
		req := &xkmssdk.TrustedCACertificateRequest{}
		a.applyKeypairSPIFFE(req, "server", "acme-corp")
		if req.SPIFFEKind != qrdbspiffe.KindService {
			t.Fatalf("SPIFFEKind = %q, want svc", req.SPIFFEKind)
		}
	})

	t.Run("no trust domain omits SPIFFE", func(t *testing.T) {
		a := &xkmsCAServicer{}
		req := &xkmssdk.TrustedCACertificateRequest{}
		a.applyKeypairSPIFFE(req, "client", "acme-corp")
		if req.SPIFFETrustDomain != "" || req.SPIFFEKind != "" || req.TenantID != "acme-corp" {
			t.Fatalf("expected no SPIFFE fields and the tenant kept for its OU, got %+v", req)
		}
	})

	t.Run("no tenant omits SPIFFE", func(t *testing.T) {
		a := &xkmsCAServicer{spiffeTrustDomain: "example.com"}
		req := &xkmssdk.TrustedCACertificateRequest{}
		a.applyKeypairSPIFFE(req, "client", "")
		if req.SPIFFETrustDomain != "" || req.TenantID != "" || req.SPIFFEKind != "" {
			t.Fatalf("expected no SPIFFE fields without tenant, got %+v", req)
		}
	})
}
