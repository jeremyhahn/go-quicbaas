package server

import (
	"context"
	"errors"
	"testing"

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
				if !errors.As(err, &refusal) || refusal.IssuingTenantID != tt.tenantID {
					t.Fatalf("refusal issuing tenant = %q (cause %v, CA %T), want %q", refusal.IssuingTenantID, refusal.Cause, adapter.getCA(), tt.tenantID)
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
