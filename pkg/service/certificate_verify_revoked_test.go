package service

import (
	"crypto/x509"
	"encoding/pem"
	"testing"

	"github.com/jeremyhahn/go-quicbaas/pkg/testutil/xkmsca"
)

// TestCertificateService_VerifyCertificateRefusesARevokedCertificate pins that
// VerifyCertificate asks the CA about the certificate's own serial. The CA
// spells serials in hex; a certificate whose decimal and hex spellings differ
// must still be found revoked once it is revoked.
func TestCertificateService_VerifyCertificateRefusesARevokedCertificate(t *testing.T) {
	ctx := systemTenantCtx()
	ca := xkmsca.NewCAService(t, "QuicBaaS #2016 Revocation Root CA")
	// The real CA answers every certificate verb VerifyCertificate and the
	// issue/revoke steps reach; it spells serials in hex, as the served CA does.
	svc := NewCertificateService(&mockCAService{
		issueCertificateFunc:  ca.IssueCertificate,
		revokeCertificateFunc: ca.RevokeCertificate,
		getCACertificateFunc:  ca.GetCACertificate,
		isRevokedFunc:         ca.IsRevoked,
	})

	issued, err := svc.IssueCertificate(ctx, &IssueCertificateRequest{CommonName: "revoked.example.com"})
	if err != nil {
		t.Fatalf("IssueCertificate: %v", err)
	}
	block, _ := pem.Decode([]byte(issued.CertificatePEM))
	if block == nil {
		t.Fatal("issued certificate is not PEM")
	}
	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		t.Fatalf("parse issued certificate: %v", err)
	}
	if cert.SerialNumber.Text(10) == cert.SerialNumber.Text(16) {
		t.Fatalf("serial %s reads the same in decimal and hex; the test needs one that differs", cert.SerialNumber.Text(16))
	}

	before, err := svc.VerifyCertificate(ctx, &VerifyCertificateRequest{CertificatePEM: issued.CertificatePEM})
	if err != nil {
		t.Fatalf("VerifyCertificate before revocation: %v", err)
	}
	if !before.Valid {
		t.Fatalf("VerifyCertificate before revocation = invalid (%s), want valid", before.ErrorMessage)
	}

	revoked, err := svc.RevokeCertificate(ctx, &RevokeCertificateRequest{SerialNumber: cert.SerialNumber.Text(16)})
	if err != nil {
		t.Fatalf("RevokeCertificate: %v", err)
	}
	if !revoked.Success {
		t.Fatal("RevokeCertificate reported failure")
	}

	after, err := svc.VerifyCertificate(ctx, &VerifyCertificateRequest{CertificatePEM: issued.CertificatePEM})
	if err != nil {
		t.Fatalf("VerifyCertificate after revocation: %v", err)
	}
	if after.Valid {
		t.Fatal("VerifyCertificate accepted a revoked certificate")
	}
	if after.ErrorMessage != "certificate is revoked" {
		t.Fatalf("VerifyCertificate error = %q, want %q", after.ErrorMessage, "certificate is revoked")
	}
}
