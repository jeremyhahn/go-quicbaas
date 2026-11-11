package server

import (
	"context"
	"testing"

	"github.com/jeremyhahn/go-quicbaas/pkg/service"
	"github.com/jeremyhahn/go-quicbaas/pkg/tenantctx"

	xkmstransport "github.com/jeremyhahn/go-xkms/sdk/go/transport"
)

// TestCAServiceAdapter_ListCertificatesShowsTheCallersTenant drives
// CertificateService.ListCertificates over the production CA adapter: a
// tenant sees the certificates issued for it and none issued for another
// tenant. Without the adapter resolving the issuing tenant the tenant's list
// comes back empty.
func TestCAServiceAdapter_ListCertificatesShowsTheCallersTenant(t *testing.T) {
	adapter, err := newCAServiceAdapter(newRealSoftwareCA(t))
	if err != nil {
		t.Fatalf("newCAServiceAdapter: %v", err)
	}
	for _, tenantID := range []string{"acme", "globex"} {
		if _, err := adapter.SignCSR(context.Background(), &xkmstransport.SignCSRRequest{
			CSRPEM:   newCSR(t, tenantID+"-user"),
			Profile:  "client",
			TenantID: tenantID,
		}); err != nil {
			t.Fatalf("SignCSR(%s): %v", tenantID, err)
		}
	}
	certs := service.NewCertificateService(adapter)

	tests := []struct {
		tenantID string
		want     string
	}{
		{tenantID: "acme", want: "acme-user"},
		{tenantID: "globex", want: "globex-user"},
	}
	for _, tt := range tests {
		t.Run(tt.tenantID, func(t *testing.T) {
			ctx := tenantctx.WithTenant(context.Background(), tt.tenantID)
			resp, err := certs.ListCertificates(ctx, &service.ListCertificatesRequest{})
			if err != nil {
				t.Fatalf("ListCertificates: %v", err)
			}
			if len(resp.Certificates) != 1 || resp.Certificates[0].CommonName != tt.want {
				t.Fatalf("certificates = %+v, want only %q", resp.Certificates, tt.want)
			}
		})
	}
}

func TestCAServiceAdapter_CertificateTenantWithoutCA(t *testing.T) {
	adapter := &caServiceAdapter{}
	if _, err := adapter.CertificateTenant(context.Background(), nil); err != ErrCAServiceAdapterNilCA {
		t.Fatalf("CertificateTenant err = %v, want ErrCAServiceAdapterNilCA", err)
	}
}
