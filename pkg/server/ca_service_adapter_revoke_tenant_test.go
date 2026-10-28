package server

import (
	"context"
	"errors"
	"testing"

	"github.com/jeremyhahn/go-quicbaas/pkg/tenantctx"

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"
	xkmstransport "github.com/jeremyhahn/go-xkms/sdk/go/transport"
)

// TestCAServiceAdapter_RevokeBindsCallerToIssuingTenant proves a
// tenant-scoped caller can revoke only a certificate issued for its own
// tenant, while a system caller may revoke any certificate (go-quicbaas#1998).
func TestCAServiceAdapter_RevokeBindsCallerToIssuingTenant(t *testing.T) {
	adapter, err := newCAServiceAdapter(newRealSoftwareCA(t))
	if err != nil {
		t.Fatalf("newCAServiceAdapter: %v", err)
	}

	issue := func(t *testing.T, tenantID string) string {
		t.Helper()
		resp, err := adapter.SignCSR(context.Background(), &xkmstransport.SignCSRRequest{
			CSRPEM:   newCSR(t, tenantID+"-user"),
			Profile:  "client",
			TenantID: tenantID,
		})
		if err != nil {
			t.Fatalf("SignCSR(%s): %v", tenantID, err)
		}
		return parseLeaf(t, resp.CertificatePEM).SerialNumber.Text(16)
	}
	isRevoked := func(t *testing.T, serial string) bool {
		t.Helper()
		resp, err := adapter.IsRevoked(context.Background(), &xkmstransport.IsRevokedRequest{SerialNumber: serial})
		if err != nil {
			t.Fatalf("IsRevoked(%s): %v", serial, err)
		}
		return resp.Revoked
	}

	acmeCtx := qrdbsdk.WithIdentity(context.Background(),
		&qrdbsdk.Identity{Subject: "acme-admin", TenantID: "acme"})
	systemCtx := qrdbsdk.WithIdentity(context.Background(),
		&qrdbsdk.Identity{Subject: "platform-admin", TenantID: qrdbsdk.SystemTenantID})

	tests := []struct {
		name       string
		ctx        context.Context
		tenantID   string
		wantRefuse bool
	}{
		{name: "tenant caller cannot revoke another tenant's certificate", ctx: acmeCtx, tenantID: "globex", wantRefuse: true},
		{name: "tenant caller revokes its own certificate", ctx: acmeCtx, tenantID: "acme"},
		{name: "system caller revokes any tenant's certificate", ctx: systemCtx, tenantID: "globex"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			serial := issue(t, tt.tenantID)

			resp, err := adapter.RevokeCertificate(tt.ctx, &xkmstransport.RevokeCertificateRequest{
				SerialNumber: serial,
				Reason:       1,
			})

			if tt.wantRefuse {
				if !errors.Is(err, qrdbsdk.ErrTenantMissing) {
					t.Fatalf("RevokeCertificate err = %v, want ErrTenantMissing", err)
				}
				var refusal *CAServiceAdapterRevokeTenantError
				if !errors.As(err, &refusal) {
					t.Fatalf("RevokeCertificate err = %T, want *CAServiceAdapterRevokeTenantError", err)
				}
				if refusal.IssuingTenantID != tt.tenantID {
					t.Fatalf("refusal issuing tenant = %q (cause %v), want %q", refusal.IssuingTenantID, refusal.Cause, tt.tenantID)
				}
				if isRevoked(t, serial) {
					t.Fatalf("serial %s was revoked by a foreign tenant", serial)
				}
				return
			}
			if err != nil {
				t.Fatalf("RevokeCertificate: %v", err)
			}
			if !resp.Success {
				t.Fatalf("RevokeCertificate success = false")
			}
			if !isRevoked(t, serial) {
				t.Fatalf("serial %s not revoked", serial)
			}
		})
	}
}

// TestCAServiceAdapter_RevokeReadsCallerTenantLikeTheServices pins that the
// revoke binding resolves the caller's tenant the way every service does
// (tenantctx.FromContext): the gRPC authz interceptor, the REST principal
// injector and the embedded transport bind the tenant under go-qrdb's tenant
// key without a qrdb Identity, and a caller with no tenant at all is refused
// rather than read as cross-tenant.
func TestCAServiceAdapter_RevokeReadsCallerTenantLikeTheServices(t *testing.T) {
	adapter, err := newCAServiceAdapter(newRealSoftwareCA(t))
	if err != nil {
		t.Fatalf("newCAServiceAdapter: %v", err)
	}

	tests := []struct {
		name       string
		ctx        context.Context
		tenantID   string
		wantRefuse bool
	}{
		{name: "tenant bound under the tenant key cannot revoke another tenant's certificate", ctx: tenantctx.WithTenant(context.Background(), "acme"), tenantID: "globex", wantRefuse: true},
		{name: "caller with no tenant is refused", ctx: context.Background(), tenantID: "acme", wantRefuse: true},
		{name: "tenant bound under the tenant key revokes its own certificate", ctx: tenantctx.WithTenant(context.Background(), "acme"), tenantID: "acme"},
		{name: "system bound under the tenant key revokes any certificate", ctx: tenantctx.WithSystem(context.Background()), tenantID: "globex"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			signed, err := adapter.SignCSR(context.Background(), &xkmstransport.SignCSRRequest{
				CSRPEM:   newCSR(t, tt.tenantID+"-user"),
				Profile:  "client",
				TenantID: tt.tenantID,
			})
			if err != nil {
				t.Fatalf("SignCSR(%s): %v", tt.tenantID, err)
			}
			serial := parseLeaf(t, signed.CertificatePEM).SerialNumber.Text(16)

			_, err = adapter.RevokeCertificate(tt.ctx, &xkmstransport.RevokeCertificateRequest{SerialNumber: serial, Reason: 1})

			revoked, statusErr := adapter.IsRevoked(context.Background(), &xkmstransport.IsRevokedRequest{SerialNumber: serial})
			if statusErr != nil {
				t.Fatalf("IsRevoked(%s): %v", serial, statusErr)
			}
			if tt.wantRefuse {
				if !errors.Is(err, qrdbsdk.ErrTenantMissing) {
					t.Fatalf("RevokeCertificate err = %v, want ErrTenantMissing", err)
				}
				if revoked.Revoked {
					t.Fatalf("serial %s was revoked by a caller outside its tenant", serial)
				}
				return
			}
			if err != nil {
				t.Fatalf("RevokeCertificate: %v", err)
			}
			if !revoked.Revoked {
				t.Fatalf("serial %s not revoked", serial)
			}
		})
	}
}

// TestCAServiceAdapter_SignCSRRecordsTenantWithoutTrustDomain pins the
// issuance half of the revoke binding: with no SPIFFE trust domain
// configured, the CA still records the requesting tenant as the issuing
// tenant. Before #1998 the tenant was recorded only alongside a trust domain,
// so every such certificate read back as system scope.
func TestCAServiceAdapter_SignCSRRecordsTenantWithoutTrustDomain(t *testing.T) {
	adapter, err := newCAServiceAdapter(newRealSoftwareCA(t))
	if err != nil {
		t.Fatalf("newCAServiceAdapter: %v", err)
	}
	resolver, ok := adapter.getCA().(certificateTenantResolver)
	if !ok {
		t.Fatalf("CA %T does not resolve certificate tenants", adapter.getCA())
	}

	tests := []struct {
		name     string
		tenantID string
		profile  string
	}{
		{name: "tenant with a profile", tenantID: "acme", profile: "client"},
		{name: "tenant without a profile", tenantID: "acme"},
		{name: "no tenant", tenantID: ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resp, err := adapter.SignCSR(context.Background(), &xkmstransport.SignCSRRequest{
				CSRPEM:   newCSR(t, "user-"+tt.tenantID),
				Profile:  tt.profile,
				TenantID: tt.tenantID,
			})
			if err != nil {
				t.Fatalf("SignCSR: %v", err)
			}
			got, err := resolver.CertificateTenant(context.Background(), parseLeaf(t, resp.CertificatePEM).SerialNumber)
			if err != nil {
				t.Fatalf("CertificateTenant: %v", err)
			}
			if got != tt.tenantID {
				t.Fatalf("CertificateTenant = %q, want %q", got, tt.tenantID)
			}
		})
	}
}
