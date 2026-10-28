package service

import (
	"context"
	"errors"
	"math/big"
	"testing"

	"github.com/jeremyhahn/go-quicbaas/pkg/tenantctx"

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"
)

var errOwnershipStoreDown = errors.New("ownership store unavailable")

// issuingTenantCA answers CertificateTenant from a fixed serial -> tenant map,
// as go-xkms ca.CA does from its issuance records: a serial with no record
// is unattributed, an empty tenant and a nil error.
type issuingTenantCA map[int64]string

func (c issuingTenantCA) CertificateTenant(_ context.Context, serial *big.Int) (string, error) {
	return c[serial.Int64()], nil
}

// failingOwnershipCA is a CA whose ownership records cannot be read.
type failingOwnershipCA struct{}

func (failingOwnershipCA) CertificateTenant(context.Context, *big.Int) (string, error) {
	return "", errOwnershipStoreDown
}

func TestBindToIssuingTenant(t *testing.T) {
	ca := issuingTenantCA{1: "acme", 2: "globex", 3: ""}
	acme := tenantctx.WithTenant(context.Background(), "acme")

	tests := []struct {
		name        string
		ctx         context.Context
		ca          any
		serial      int64
		wantRefuse  bool
		wantIssuing string
		wantCause   bool
	}{
		{name: "tenant acts on its own certificate", ctx: acme, ca: ca, serial: 1},
		{name: "tenant from the qrdb identity acts on its own certificate", ctx: qrdbsdk.WithIdentity(context.Background(), &qrdbsdk.Identity{Subject: "u", TenantID: "acme"}), ca: ca, serial: 1},
		{name: "system acts on any certificate", ctx: tenantctx.WithSystem(context.Background()), ca: ca, serial: 2},
		{name: "system acts on a serial the CA cannot attribute", ctx: tenantctx.WithSystem(context.Background()), ca: ca, serial: 9},
		{name: "tenant is refused another tenant's certificate", ctx: acme, ca: ca, serial: 2, wantRefuse: true, wantIssuing: "globex"},
		{name: "tenant is refused a system certificate", ctx: acme, ca: ca, serial: 3, wantRefuse: true},
		{name: "tenant is refused a serial the CA cannot attribute", ctx: acme, ca: ca, serial: 9, wantRefuse: true},
		{name: "tenant is refused when the CA cannot read its ownership records", ctx: acme, ca: failingOwnershipCA{}, serial: 1, wantRefuse: true, wantCause: true},
		{name: "tenant is refused when the CA cannot report issuing tenants", ctx: acme, ca: struct{}{}, serial: 1, wantRefuse: true},
		{name: "caller with no tenant is refused", ctx: context.Background(), ca: ca, serial: 1, wantRefuse: true, wantCause: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := BindToIssuingTenant(tt.ctx, tt.ca, big.NewInt(tt.serial))
			if !tt.wantRefuse {
				if err != nil {
					t.Fatalf("BindToIssuingTenant: %v", err)
				}
				return
			}
			var refusal *CertificateTenantError
			if !errors.As(err, &refusal) {
				t.Fatalf("BindToIssuingTenant err = %v, want *CertificateTenantError", err)
			}
			if !errors.Is(err, qrdbsdk.ErrTenantMissing) {
				t.Fatalf("refusal does not unwrap to ErrTenantMissing")
			}
			if refusal.IssuingTenantID != tt.wantIssuing {
				t.Fatalf("refusal issuing tenant = %q, want %q", refusal.IssuingTenantID, tt.wantIssuing)
			}
			if (refusal.Cause != nil) != tt.wantCause {
				t.Fatalf("refusal cause = %v, want cause %v", refusal.Cause, tt.wantCause)
			}
		})
	}
}

func TestCertificateTenantError_HidesTheCause(t *testing.T) {
	err := &CertificateTenantError{CallerTenantID: "acme", IssuingTenantID: "globex", Cause: errOwnershipStoreDown}

	if err.Error() != qrdbsdk.ErrTenantMissing.Error() {
		t.Fatalf("Error() = %q, want %q", err.Error(), qrdbsdk.ErrTenantMissing.Error())
	}
	if errors.Is(err, errOwnershipStoreDown) {
		t.Fatalf("the cause leaks through the error chain")
	}
}
