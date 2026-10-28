//go:build rest

package rest

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
		wantRefuse  bool
	}{
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

			signed, err := a.SignCSR(tt.issuer, &xkmstransport.SignCSRRequest{
				CSRPEM:  newTestCSR(t, "leaf.example.test"),
				Profile: "client",
			})
			if err != nil {
				t.Fatalf("SignCSR: %v", err)
			}

			_, err = a.RevokeCertificate(tt.revoker, &xkmstransport.RevokeCertificateRequest{
				SerialNumber: signed.SerialNumber,
				Reason:       1,
			})

			status, statusErr := a.IsRevoked(context.Background(), &xkmstransport.IsRevokedRequest{SerialNumber: signed.SerialNumber})
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
