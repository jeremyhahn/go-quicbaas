package service

import (
	"context"
	"crypto/x509"
	"errors"
	"math/big"
	"reflect"
	"testing"

	xkmssdk "github.com/jeremyhahn/go-xkms/sdk/go"

	"github.com/jeremyhahn/go-quicbaas/pkg/tenantctx"
)

// TestFilterCertificateIDsForCaller pins #1998: a tenant listing the go-xkms
// key store sees only the certificates the CA issued for it, the system
// tenant sees every one, and a caller without a tenant is refused.
func TestFilterCertificateIDsForCaller(t *testing.T) {
	certs := NewCertificateService(newTenantListerCAService(issuingTenantCA{1: "acme", 2: "globex"}))
	stored := map[string]int64{"acme-leaf": 1, "globex-leaf": 2, "unissued": 7}
	lookup := func(_ context.Context, keyID string) (*x509.Certificate, error) {
		serial, ok := stored[keyID]
		if !ok {
			return nil, xkmssdk.ErrStorageNotFound
		}
		return &x509.Certificate{SerialNumber: big.NewInt(serial)}, nil
	}
	ids := []string{"acme-leaf", "globex-leaf", "unissued", "vanished"}

	tests := []struct {
		name string
		ctx  context.Context
		want []string
	}{
		{name: "tenant sees only its own certificates", ctx: tenantctx.WithTenant(context.Background(), "acme"), want: []string{"acme-leaf"}},
		{name: "system sees every certificate", ctx: tenantctx.WithSystem(context.Background()), want: ids},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := FilterCertificateIDsForCaller(tt.ctx, certs, ids, lookup)
			if err != nil {
				t.Fatalf("FilterCertificateIDsForCaller: %v", err)
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("FilterCertificateIDsForCaller = %v, want %v", got, tt.want)
			}
		})
	}

	t.Run("caller without a tenant is refused", func(t *testing.T) {
		got, err := FilterCertificateIDsForCaller(context.Background(), certs, ids, lookup)
		var refusal *CertificateTenantError
		if !errors.As(err, &refusal) || got != nil {
			t.Fatalf("FilterCertificateIDsForCaller = %v, %v, want a *CertificateTenantError", got, err)
		}
	})
}

// TestFilterCertificateIDsForCaller_LookupFailures pins that only a confirmed
// not-found certificate is skipped: any other lookup failure is the caller's
// to see, so an unreadable store never shrinks the listing silently.
func TestFilterCertificateIDsForCaller_LookupFailures(t *testing.T) {
	certs := NewCertificateService(newTenantListerCAService(issuingTenantCA{1: "acme"}))
	acme := tenantctx.WithTenant(context.Background(), "acme")
	storeDown := errors.New("certificate store unreadable")
	lookup := func(ctx context.Context, keyID string) (*x509.Certificate, error) {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		switch keyID {
		case "acme-leaf":
			return &x509.Certificate{SerialNumber: big.NewInt(1)}, nil
		case "vanished":
			// go-xkms wraps its storage not-found in a certificate-operation error.
			return nil, errors.Join(errors.New("get certificate"), xkmssdk.ErrStorageNotFound)
		case "remote-vanished":
			return nil, xkmssdk.ErrCertificateNotFound
		default:
			return nil, storeDown
		}
	}

	t.Run("a not-found certificate is skipped", func(t *testing.T) {
		got, err := FilterCertificateIDsForCaller(acme, certs, []string{"vanished", "acme-leaf", "remote-vanished"}, lookup)
		if err != nil {
			t.Fatalf("FilterCertificateIDsForCaller: %v", err)
		}
		if !reflect.DeepEqual(got, []string{"acme-leaf"}) {
			t.Fatalf("FilterCertificateIDsForCaller = %v, want [acme-leaf]", got)
		}
	})

	t.Run("an unreadable store returns an error", func(t *testing.T) {
		got, err := FilterCertificateIDsForCaller(acme, certs, []string{"acme-leaf", "unreadable"}, lookup)
		var lookupErr *CertificateLookupError
		if got != nil || !errors.As(err, &lookupErr) || !errors.Is(err, storeDown) {
			t.Fatalf("FilterCertificateIDsForCaller = %v, %v, want a *CertificateLookupError wrapping the store failure", got, err)
		}
		if lookupErr.KeyID != "unreadable" {
			t.Fatalf("CertificateLookupError.KeyID = %q, want unreadable", lookupErr.KeyID)
		}
	})

	t.Run("a cancelled context returns an error", func(t *testing.T) {
		cancelled, cancel := context.WithCancel(acme)
		cancel()
		got, err := FilterCertificateIDsForCaller(cancelled, certs, []string{"acme-leaf"}, lookup)
		var lookupErr *CertificateLookupError
		if got != nil || !errors.As(err, &lookupErr) || !errors.Is(err, context.Canceled) {
			t.Fatalf("FilterCertificateIDsForCaller = %v, %v, want a *CertificateLookupError wrapping context.Canceled", got, err)
		}
	})
}

// TestRequireSystemKeyStoreWriter pins #1998: raw go-xkms key-store writes
// record no tenant, so only the system tenant may make them.
func TestRequireSystemKeyStoreWriter(t *testing.T) {
	if err := RequireSystemKeyStoreWriter(tenantctx.WithSystem(context.Background()), "saveCert"); err != nil {
		t.Fatalf("system caller refused: %v", err)
	}

	tests := []struct {
		name       string
		ctx        context.Context
		wantTenant string
	}{
		{name: "tenant caller is refused", ctx: tenantctx.WithTenant(context.Background(), "acme"), wantTenant: "acme"},
		{name: "caller without a tenant is refused", ctx: context.Background()},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := RequireSystemKeyStoreWriter(tt.ctx, "saveCert")
			var refusal *KeyStoreWriteRefusedError
			if !errors.As(err, &refusal) {
				t.Fatalf("RequireSystemKeyStoreWriter err = %v, want *KeyStoreWriteRefusedError", err)
			}
			if refusal.CallerTenantID != tt.wantTenant || refusal.Op != "saveCert" {
				t.Fatalf("refusal = %+v, want tenant %q op saveCert", refusal, tt.wantTenant)
			}
			if refusal.Error() == "" {
				t.Fatal("refusal has an empty message")
			}
		})
	}
}
