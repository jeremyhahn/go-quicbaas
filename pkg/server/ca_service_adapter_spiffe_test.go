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

package server

import (
	"context"
	"crypto/x509"
	"encoding/pem"
	"testing"

	qrdbspiffe "github.com/jeremyhahn/go-qrdb/pkg/auth/spiffe"
	xkmstransport "github.com/jeremyhahn/go-xkms/sdk/go/transport"
)

// TestCAServiceAdapter_SignCSR_StampsTenantSPIFFE proves that when a trust
// domain is configured and the request carries a tenant, the embedded adapter
// passes the SPIFFE options to the CA and the issued client certificate carries
// the tenant-scoped SPIFFE URI SAN that login derives the tenant from.
func TestCAServiceAdapter_SignCSR_StampsTenantSPIFFE(t *testing.T) {
	ca := newFakeTrustedCA(t)
	adapter, err := newCAServiceAdapter(ca)
	if err != nil {
		t.Fatalf("newCAServiceAdapter: %v", err)
	}
	adapter.SetSPIFFETrustDomain("example.com")

	csrPEM := newCSR(t, "alice")

	resp, err := adapter.SignCSR(context.Background(), &xkmstransport.SignCSRRequest{
		CSRPEM:   csrPEM,
		Profile:  "client",
		TenantID: "acme-corp",
	})
	if err != nil {
		t.Fatalf("SignCSR: %v", err)
	}

	// Opts boundary: adapter set trust domain, tenant, and the user kind for a
	// client profile.
	if ca.lastSignOpts == nil {
		t.Fatal("CA received nil sign options")
	}
	if ca.lastSignOpts.SPIFFETrustDomain != "example.com" {
		t.Fatalf("SPIFFETrustDomain = %q, want example.com", ca.lastSignOpts.SPIFFETrustDomain)
	}
	if ca.lastSignOpts.TenantID != "acme-corp" {
		t.Fatalf("TenantID = %q, want acme-corp", ca.lastSignOpts.TenantID)
	}
	if ca.lastSignOpts.SPIFFEKind != qrdbspiffe.KindUser {
		t.Fatalf("SPIFFEKind = %q, want %q", ca.lastSignOpts.SPIFFEKind, qrdbspiffe.KindUser)
	}

	// End-to-end: the issued leaf carries the SPIFFE URI SAN, and
	// TenantFromCert resolves the tenant exactly as login does.
	leaf := parseLeaf(t, resp.CertificatePEM)
	id, terr := qrdbspiffe.TenantFromCert(leaf, "example.com")
	if terr != nil {
		t.Fatalf("TenantFromCert: %v", terr)
	}
	if id.TenantID != "acme-corp" {
		t.Fatalf("derived tenant = %q, want acme-corp", id.TenantID)
	}
	if id.Kind != qrdbspiffe.KindUser {
		t.Fatalf("derived kind = %q, want user", id.Kind)
	}
}

// TestCAServiceAdapter_SignCSR_ServerProfileIsSvc proves a server/device
// profile is stamped as the svc kind, never as a human login subject.
func TestCAServiceAdapter_SignCSR_ServerProfileIsSvc(t *testing.T) {
	ca := newFakeTrustedCA(t)
	adapter, _ := newCAServiceAdapter(ca)
	adapter.SetSPIFFETrustDomain("example.com")

	resp, err := adapter.SignCSR(context.Background(), &xkmstransport.SignCSRRequest{
		CSRPEM:   newCSR(t, "billing.svc"),
		Profile:  "server",
		TenantID: "acme-corp",
	})
	if err != nil {
		t.Fatalf("SignCSR: %v", err)
	}
	if ca.lastSignOpts.SPIFFEKind != qrdbspiffe.KindService {
		t.Fatalf("SPIFFEKind = %q, want %q", ca.lastSignOpts.SPIFFEKind, qrdbspiffe.KindService)
	}

	leaf := parseLeaf(t, resp.CertificatePEM)
	id, terr := qrdbspiffe.TenantFromCert(leaf, "example.com")
	if terr != nil {
		t.Fatalf("TenantFromCert: %v", terr)
	}
	if id.Kind != qrdbspiffe.KindService {
		t.Fatalf("derived kind = %q, want svc", id.Kind)
	}
}

// TestCAServiceAdapter_SignCSR_NoTrustDomainOmitsSAN proves that without a
// configured trust domain the adapter does not request a SPIFFE SAN, even when
// a tenant is supplied (single-domain / pre-broker mode).
func TestCAServiceAdapter_SignCSR_NoTrustDomainOmitsSAN(t *testing.T) {
	ca := newFakeTrustedCA(t)
	adapter, _ := newCAServiceAdapter(ca)
	// No SetSPIFFETrustDomain call.

	if _, err := adapter.SignCSR(context.Background(), &xkmstransport.SignCSRRequest{
		CSRPEM:   newCSR(t, "node"),
		Profile:  "client",
		TenantID: "acme-corp",
	}); err != nil {
		t.Fatalf("SignCSR: %v", err)
	}
	if ca.lastSignOpts != nil && ca.lastSignOpts.SPIFFETrustDomain != "" {
		t.Fatalf("SPIFFETrustDomain = %q, want empty when no trust domain configured", ca.lastSignOpts.SPIFFETrustDomain)
	}
}

// parseLeaf decodes the first CERTIFICATE block of a PEM bundle into an
// x509.Certificate.
func parseLeaf(t *testing.T, pemBytes []byte) *x509.Certificate {
	t.Helper()
	block, _ := pem.Decode(pemBytes)
	if block == nil || block.Type != "CERTIFICATE" {
		t.Fatalf("expected CERTIFICATE PEM, got %q", pemBytes)
	}
	leaf, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		t.Fatalf("parse leaf: %v", err)
	}
	return leaf
}
