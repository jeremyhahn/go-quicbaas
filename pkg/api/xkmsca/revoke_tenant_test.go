package xkmsca

import (
	"context"
	stderrors "errors"
	"testing"

	"github.com/jeremyhahn/go-quicbaas/pkg/tenantctx"
	testca "github.com/jeremyhahn/go-quicbaas/pkg/testutil/xkmsca"

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"
	xkmstransport "github.com/jeremyhahn/go-xkms/sdk/go/transport"
)

// TestXKMSCAServicer_RevokeBindsCallerToIssuingTenant pins that
// POST /api/v1/xkms/ca/revoke applies the same issuing-tenant binding as the
// certificate service: a tenant revokes only a certificate issued for it, with
// or without a SPIFFE trust domain, the system tenant revokes any, and a caller
// with no tenant is refused. A refusal unwraps to qrdbsdk.ErrTenantMissing and
// leaves the certificate unrevoked.
func TestXKMSCAServicer_RevokeBindsCallerToIssuingTenant(t *testing.T) {
	tenantA := tenantctx.WithTenant(context.Background(), "tenant-a")
	tenantB := tenantctx.WithTenant(context.Background(), "tenant-b")
	system := tenantctx.WithSystem(context.Background())

	tests := []struct {
		name        string
		trustDomain string
		issuer      context.Context
		revoker     context.Context
		keypair     bool
		wantRefuse  bool
	}{
		{name: "tenant cannot revoke another tenant's keypair certificate", trustDomain: testTrustDomain, issuer: tenantB, revoker: tenantA, keypair: true, wantRefuse: true},
		{name: "tenant revokes its own keypair certificate without a trust domain", issuer: tenantA, revoker: tenantA, keypair: true},
		{name: "tenant cannot revoke another tenant's certificate", trustDomain: testTrustDomain, issuer: tenantB, revoker: tenantA, wantRefuse: true},
		{name: "caller with no tenant is refused", trustDomain: testTrustDomain, issuer: tenantA, revoker: context.Background(), wantRefuse: true},
		{name: "tenant cannot revoke a system certificate", trustDomain: testTrustDomain, issuer: system, revoker: tenantA, wantRefuse: true},
		{name: "tenant revokes its own certificate", trustDomain: testTrustDomain, issuer: tenantA, revoker: tenantA},
		{name: "tenant revokes its own certificate without a trust domain", issuer: tenantA, revoker: tenantA},
		{name: "system revokes any tenant's certificate", trustDomain: testTrustDomain, issuer: tenantB, revoker: system},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			a := newXKMSCAServicer(testca.New(t, "xkmsca-revoke-ca"), tt.trustDomain)

			serial := issueForRevoke(t, a, tt.issuer, tt.keypair)

			_, err := a.RevokeCertificate(tt.revoker, &xkmstransport.RevokeCertificateRequest{
				SerialNumber: serial,
				Reason:       1,
			})

			status, statusErr := a.IsRevoked(context.Background(), &xkmstransport.IsRevokedRequest{SerialNumber: serial})
			if statusErr != nil {
				t.Fatalf("IsRevoked: %v", statusErr)
			}
			if tt.wantRefuse {
				if !stderrors.Is(err, qrdbsdk.ErrTenantMissing) {
					t.Fatalf("RevokeCertificate err = %v, want ErrTenantMissing", err)
				}
				if status.Revoked {
					t.Fatal("certificate was revoked by a caller outside its tenant")
				}
				return
			}
			if err != nil {
				t.Fatalf("RevokeCertificate: %v", err)
			}
			if !status.Revoked {
				t.Fatal("certificate not revoked")
			}
		})
	}
}

// issueForRevoke issues a client certificate for the tenant bound to ctx, on
// the CSR-sign path or the generate-and-distribute (keypair) path, and returns
// its serial.
func issueForRevoke(t *testing.T, a *xkmsCAServicer, ctx context.Context, keypair bool) string {
	t.Helper()
	if keypair {
		issued, err := a.IssueCertificate(ctx, &xkmstransport.IssueCertificateRequest{
			CommonName:   "leaf.example.test",
			Profile:      "client",
			ValidityDays: 1,
		})
		if err != nil {
			t.Fatalf("IssueCertificate: %v", err)
		}
		return issued.SerialNumber
	}
	signed, err := a.SignCSR(ctx, &xkmstransport.SignCSRRequest{
		CSRPEM:  newTestCSR(t, "leaf.example.test"),
		Profile: "client",
	})
	if err != nil {
		t.Fatalf("SignCSR: %v", err)
	}
	return signed.SerialNumber
}
