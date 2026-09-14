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
	"crypto/x509"
	"encoding/pem"
	"errors"
	"testing"

	"github.com/jeremyhahn/go-quicbaas/pkg/testutil/xkmsca"

	xkmstransport "github.com/jeremyhahn/go-xkms/sdk/go/transport"
)

// The exported constructor returns the SAME adapter the daemon serves
// certificate operations through, and it issues: a real CA, a real requester
// key, a real signature.
//
// The credential's private key MUST come back. That is the one thing a mock of
// this interface always gets wrong (go-quicbaas#910) and the one thing the
// integration fixtures cannot work without, because a fixture is a certificate
// AND the key its holder presents it with.
func TestNewCAService_IssuesThroughTheProductPath(t *testing.T) {
	svc, err := NewCAService(xkmsca.New(t, "exported ca service"), "")
	if err != nil {
		t.Fatalf("NewCAService: %v", err)
	}

	resp, err := svc.IssueCertificate(context.Background(), &xkmstransport.IssueCertificateRequest{
		CommonName:   "node.example",
		Organization: "Fixtures",
		SANs:         []string{"DNS:node.example", "IP:127.0.0.1"},
		ValidityDays: 30,
	})
	if err != nil {
		t.Fatalf("IssueCertificate: %v", err)
	}
	if len(resp.PrivateKeyPEM) == 0 {
		t.Fatal("issuance returned no private key; a fixture cannot use the certificate")
	}

	block, _ := pem.Decode(resp.CertificatePEM)
	if block == nil {
		t.Fatal("certificate PEM did not decode")
	}
	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		t.Fatalf("parse issued certificate: %v", err)
	}
	if cert.Subject.CommonName != "node.example" {
		t.Errorf("CN = %q, want node.example", cert.Subject.CommonName)
	}
	if len(cert.DNSNames) != 1 || cert.DNSNames[0] != "node.example" {
		t.Errorf("DNS SANs = %v, want [node.example]", cert.DNSNames)
	}
	if len(cert.IPAddresses) != 1 || cert.IPAddresses[0].String() != "127.0.0.1" {
		t.Errorf("IP SANs = %v, want [127.0.0.1]", cert.IPAddresses)
	}
	if len(cert.URIs) != 0 {
		t.Errorf("URI SANs = %v, want none without a trust domain and tenant", cert.URIs)
	}
}

// A trust domain plus a request tenant makes the adapter stamp the
// tenant-scoped SPIFFE URI SAN login derives the tenant from. Passing the trust
// domain is the whole reason the constructor takes one.
func TestNewCAService_StampsTheTenantSPIFFESAN(t *testing.T) {
	svc, err := NewCAService(xkmsca.New(t, "spiffe ca service"), "fixtures.test")
	if err != nil {
		t.Fatalf("NewCAService: %v", err)
	}

	resp, err := svc.IssueCertificate(context.Background(), &xkmstransport.IssueCertificateRequest{
		Profile:      "client",
		CommonName:   "admin@fixtures.test",
		ValidityDays: 30,
		TenantID:     "system",
	})
	if err != nil {
		t.Fatalf("IssueCertificate: %v", err)
	}
	block, _ := pem.Decode(resp.CertificatePEM)
	if block == nil {
		t.Fatal("certificate PEM did not decode")
	}
	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		t.Fatalf("parse issued certificate: %v", err)
	}

	want := "spiffe://fixtures.test/tenant/system/user/admin@fixtures.test"
	var got []string
	for _, u := range cert.URIs {
		got = append(got, u.String())
	}
	if len(got) != 1 || got[0] != want {
		t.Fatalf("URI SANs = %v, want [%s]", got, want)
	}
	if len(cert.ExtKeyUsage) != 1 || cert.ExtKeyUsage[0] != x509.ExtKeyUsageClientAuth {
		t.Errorf("EKU = %v, want ClientAuth only under the client profile", cert.ExtKeyUsage)
	}
}

// A nil CA is refused with the adapter's own sentinel, so a caller that forgot
// to bring the CA up learns that rather than panicking on first issuance.
func TestNewCAService_RejectsANilCA(t *testing.T) {
	svc, err := NewCAService(nil, "fixtures.test")
	if !errors.Is(err, ErrCAServiceAdapterNilCA) {
		t.Fatalf("error = %v, want ErrCAServiceAdapterNilCA", err)
	}
	if svc != nil {
		t.Error("NewCAService returned a service alongside the error")
	}
}

// A CA that is not up refuses issuance rather than returning something
// unusable. NewUninitialized is a REAL CA that has not been initialized, not a
// stub that returns an invented error.
func TestNewCAService_OverACAThatIsNotUpRefusesIssuance(t *testing.T) {
	svc, err := NewCAService(xkmsca.NewUninitialized(t, "down ca service"), "")
	if err != nil {
		t.Fatalf("NewCAService: %v", err)
	}
	if _, err := svc.IssueCertificate(context.Background(), &xkmstransport.IssueCertificateRequest{
		CommonName: "node.example",
	}); err == nil {
		t.Fatal("a CA that is not up issued a certificate")
	}
}
