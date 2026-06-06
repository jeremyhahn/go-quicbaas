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

package service

import (
	"context"
	"testing"

	"github.com/jeremyhahn/go-quicbaas/pkg/iam"

	qrdbspiffe "github.com/jeremyhahn/go-qrdb/pkg/auth/spiffe"
	xkmstransport "github.com/jeremyhahn/go-xkms/sdk/go/transport"
)

// TestSignCSR_PropagatesTenantToTransport proves that a tenant present on the
// request context is carried through to the xkms transport SignCSRRequest as
// TenantID, so the issuing CA can stamp the tenant-scoped SPIFFE URI SAN. This
// is the linchpin that lets login derive the tenant from the verified peer
// certificate. Previously the service discarded the tenant with "_".
func TestSignCSR_PropagatesTenantToTransport(t *testing.T) {
	var captured *xkmstransport.SignCSRRequest
	mock := &mockCAService{
		signCSRFunc: func(_ context.Context, req *xkmstransport.SignCSRRequest) (*xkmstransport.SignCSRResponse, error) {
			captured = req
			return &xkmstransport.SignCSRResponse{
				CertificatePEM: []byte("-----BEGIN CERTIFICATE-----\nx\n-----END CERTIFICATE-----"),
				SerialNumber:   "01",
			}, nil
		},
	}
	svc := NewCertificateService(mock)

	ctx := iam.ContextWithTenant(context.Background(), "acme-corp")
	_, err := svc.SignCSR(ctx, &SignCSRRequest{
		CSRPEM: "-----BEGIN CERTIFICATE REQUEST-----\ncsr\n-----END CERTIFICATE REQUEST-----",
		Usage:  "client",
	})
	if err != nil {
		t.Fatalf("SignCSR: %v", err)
	}
	if captured == nil {
		t.Fatal("transport SignCSR was never invoked")
	}
	if captured.TenantID != "acme-corp" {
		t.Fatalf("TenantID = %q, want %q", captured.TenantID, "acme-corp")
	}
	if captured.Profile != "client" {
		t.Fatalf("Profile = %q, want client", captured.Profile)
	}
}

// TestSignCSR_NoTenantSingleDomain confirms that in single-domain mode (no
// tenant on the context) the request carries an empty TenantID, so the CA
// omits the SPIFFE SAN and behaviour is unchanged.
func TestSignCSR_NoTenantSingleDomain(t *testing.T) {
	var captured *xkmstransport.SignCSRRequest
	mock := &mockCAService{
		signCSRFunc: func(_ context.Context, req *xkmstransport.SignCSRRequest) (*xkmstransport.SignCSRResponse, error) {
			captured = req
			return &xkmstransport.SignCSRResponse{SerialNumber: "02"}, nil
		},
	}
	svc := NewCertificateService(mock)

	_, err := svc.SignCSR(context.Background(), &SignCSRRequest{
		CSRPEM: "-----BEGIN CERTIFICATE REQUEST-----\ncsr\n-----END CERTIFICATE REQUEST-----",
		Usage:  "server",
	})
	if err != nil {
		t.Fatalf("SignCSR: %v", err)
	}
	if captured.TenantID != "" {
		t.Fatalf("TenantID = %q, want empty in single-domain mode", captured.TenantID)
	}
}

// TestSignCSR_InvalidTenantRejected proves the tenant validation is no longer
// silently discarded: an invalid tenant id surfaces as an error rather than
// being dropped.
func TestSignCSR_InvalidTenantRejected(t *testing.T) {
	mock := &mockCAService{}
	svc := NewCertificateService(mock)

	ctx := iam.ContextWithTenant(context.Background(), "bad/tenant")
	_, err := svc.SignCSR(ctx, &SignCSRRequest{
		CSRPEM: "-----BEGIN CERTIFICATE REQUEST-----\ncsr\n-----END CERTIFICATE REQUEST-----",
	})
	if err == nil {
		t.Fatal("expected error for invalid tenant id, got nil")
	}
}

// TestIssueCertificate_PropagatesTenant proves the generate-keypair issuance
// path also carries the tenant to the transport request.
func TestIssueCertificate_PropagatesTenant(t *testing.T) {
	var captured *xkmstransport.IssueCertificateRequest
	mock := &mockCAService{
		issueCertificateFunc: func(_ context.Context, req *xkmstransport.IssueCertificateRequest) (*xkmstransport.IssueCertificateResponse, error) {
			captured = req
			return &xkmstransport.IssueCertificateResponse{SerialNumber: "03"}, nil
		},
	}
	svc := NewCertificateService(mock)

	ctx := iam.ContextWithTenant(context.Background(), "globex")
	_, err := svc.IssueCertificate(ctx, &IssueCertificateRequest{
		CommonName: "svc.globex.internal",
		Usage:      "server",
	})
	if err != nil {
		t.Fatalf("IssueCertificate: %v", err)
	}
	if captured.TenantID != "globex" {
		t.Fatalf("TenantID = %q, want globex", captured.TenantID)
	}
}

// TestSPIFFEKindForProfile verifies the deterministic profile -> SPIFFE kind
// mapping: only the human-login client profile resolves to KindUser; every
// other profile resolves to KindService so a service/device certificate is
// never misclassified as a human login subject.
func TestSPIFFEKindForProfile(t *testing.T) {
	cases := []struct {
		profile string
		want    string
	}{
		{"client", qrdbspiffe.KindUser},
		{"Client", qrdbspiffe.KindUser},
		{" client ", qrdbspiffe.KindUser},
		{"server", qrdbspiffe.KindService},
		{"codesigning", qrdbspiffe.KindService},
		{"email", qrdbspiffe.KindService},
		{"ocsp", qrdbspiffe.KindService},
		{"timestamping", qrdbspiffe.KindService},
		{"ca", qrdbspiffe.KindService},
		{"", qrdbspiffe.KindService},
		{"unknown", qrdbspiffe.KindService},
	}
	for _, tc := range cases {
		if got := SPIFFEKindForProfile(tc.profile); got != tc.want {
			t.Errorf("SPIFFEKindForProfile(%q) = %q, want %q", tc.profile, got, tc.want)
		}
	}
}
