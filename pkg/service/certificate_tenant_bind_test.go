package service

import (
	"context"
	"crypto/x509"
	"errors"
	"math/big"
	"testing"

	"github.com/jeremyhahn/go-quicbaas/pkg/tenantctx"

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"
)

func TestCertificateService_CertificateTenantDelegatesToTheCA(t *testing.T) {
	svc := NewCertificateService(newTenantListerCAService(issuingTenantCA{1: "acme"}))
	resolver, ok := svc.(CertificateTenantResolver)
	if !ok {
		t.Fatalf("certificate service %T does not resolve certificate tenants", svc)
	}
	got, err := resolver.CertificateTenant(context.Background(), big.NewInt(1))
	if err != nil || got != "acme" {
		t.Fatalf("CertificateTenant = %q, %v; want acme", got, err)
	}
}

func TestCertificateService_CertificateTenantWithoutTenantRecordingCA(t *testing.T) {
	resolver := NewCertificateService(&mockCAService{}).(CertificateTenantResolver)
	if _, err := resolver.CertificateTenant(context.Background(), big.NewInt(1)); !errors.Is(err, ErrCertificateTenantNotSupported) {
		t.Fatalf("CertificateTenant err = %v, want ErrCertificateTenantNotSupported", err)
	}
}

func TestBindCertificateToCaller(t *testing.T) {
	certs := NewCertificateService(newTenantListerCAService(issuingTenantCA{1: "acme", 2: "globex"}))
	acme := tenantctx.WithTenant(context.Background(), "acme")
	system := tenantctx.WithSystem(context.Background())
	cert := func(serial int64) *x509.Certificate {
		return &x509.Certificate{SerialNumber: big.NewInt(serial)}
	}

	tests := []struct {
		name       string
		ctx        context.Context
		certs      CertificateService
		cert       *x509.Certificate
		wantRefuse bool
	}{
		{name: "tenant reaches a certificate issued for it", ctx: acme, certs: certs, cert: cert(1)},
		{name: "tenant is refused another tenant's certificate", ctx: acme, certs: certs, cert: cert(2), wantRefuse: true},
		{name: "tenant is refused a certificate the CA did not issue", ctx: acme, certs: certs, cert: cert(7), wantRefuse: true},
		{name: "tenant is refused when no certificate service is wired", ctx: acme, cert: cert(1), wantRefuse: true},
		{name: "tenant is refused a nil certificate", ctx: acme, certs: certs, wantRefuse: true},
		{name: "caller without a tenant is refused", ctx: context.Background(), certs: certs, cert: cert(1), wantRefuse: true},
		{name: "system reaches any certificate", ctx: system, certs: certs, cert: cert(2)},
		{name: "system reaches a certificate without a certificate service", ctx: system, cert: cert(7)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := BindCertificateToCaller(tt.ctx, tt.certs, tt.cert)
			if !tt.wantRefuse {
				if err != nil {
					t.Fatalf("BindCertificateToCaller: %v", err)
				}
				return
			}
			var refusal *CertificateTenantError
			if !errors.As(err, &refusal) || !errors.Is(err, qrdbsdk.ErrTenantMissing) {
				t.Fatalf("BindCertificateToCaller err = %v, want *CertificateTenantError", err)
			}
		})
	}
}
