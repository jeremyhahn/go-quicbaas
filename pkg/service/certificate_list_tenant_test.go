package service

import (
	"context"
	"crypto/x509"
	"crypto/x509/pkix"
	"math/big"
	"testing"
	"time"

	"github.com/jeremyhahn/go-quicbaas/pkg/tenantctx"

	xkmstransport "github.com/jeremyhahn/go-xkms/sdk/go/transport"
)

// tenantListerCAService lists a fixed set of certificates and reports the
// tenant each was issued for, as the caServiceAdapter does over go-xkms.
type tenantListerCAService struct {
	mockListerCAService
	issuingTenantCA
}

func newTenantListerCAService(tenants issuingTenantCA) *tenantListerCAService {
	certs := make([]*x509.Certificate, 0, len(tenants)+1)
	// Serial 9 is unattributed: the CA cannot report its tenant.
	for _, serial := range []int64{1, 2, 3, 9} {
		certs = append(certs, &x509.Certificate{
			SerialNumber: big.NewInt(serial),
			Subject:      pkix.Name{CommonName: "leaf"},
			NotAfter:     time.Now().Add(time.Hour),
		})
	}
	return &tenantListerCAService{
		mockListerCAService: mockListerCAService{
			mockCAService: mockCAService{
				isRevokedFunc: func(_ context.Context, _ *xkmstransport.IsRevokedRequest) (*xkmstransport.IsRevokedResponse, error) {
					return &xkmstransport.IsRevokedResponse{}, nil
				},
			},
			listFunc: func(_ context.Context) ([]*x509.Certificate, error) {
				return certs, nil
			},
		},
		issuingTenantCA: tenants,
	}
}

// TestCertificateService_ListCertificatesShowsOnlyTheCallersTenant pins that a
// tenant lists only the certificates issued for it, while the system tenant
// lists every certificate.
func TestCertificateService_ListCertificatesShowsOnlyTheCallersTenant(t *testing.T) {
	ca := newTenantListerCAService(issuingTenantCA{1: "acme", 2: "globex", 3: ""})

	tests := []struct {
		name        string
		ctx         context.Context
		wantSerials []string
	}{
		{name: "tenant lists only its own certificates", ctx: tenantctx.WithTenant(context.Background(), "acme"), wantSerials: []string{"1"}},
		{name: "system lists every certificate", ctx: tenantctx.WithSystem(context.Background()), wantSerials: []string{"1", "2", "3", "9"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resp, err := NewCertificateService(ca).ListCertificates(tt.ctx, &ListCertificatesRequest{})
			if err != nil {
				t.Fatalf("ListCertificates: %v", err)
			}
			got := make([]string, 0, len(resp.Certificates))
			for _, c := range resp.Certificates {
				got = append(got, c.SerialNumber)
			}
			if len(got) != len(tt.wantSerials) || resp.TotalCount != len(tt.wantSerials) {
				t.Fatalf("serials = %v (total %d), want %v", got, resp.TotalCount, tt.wantSerials)
			}
			for i := range got {
				if got[i] != tt.wantSerials[i] {
					t.Fatalf("serials = %v, want %v", got, tt.wantSerials)
				}
			}
		})
	}
}

// TestCertificateService_ListCertificatesRefusesAnUnknownCaller pins that a
// caller with no tenant lists nothing.
func TestCertificateService_ListCertificatesRefusesAnUnknownCaller(t *testing.T) {
	ca := newTenantListerCAService(issuingTenantCA{1: "acme"})

	if _, err := NewCertificateService(ca).ListCertificates(context.Background(), &ListCertificatesRequest{}); err == nil {
		t.Fatal("ListCertificates allowed a caller with no tenant")
	}
}
