//go:build !integration

// Copyright (c) 2026 Jeremy Hahn
// Copyright (c) 2026 Automate The Things, LLC
//
// This file is part of go-quicbaas.
//
// go-quicbaas is dual-licensed:
//
// 1. GNU Affero General Public License v3.0 (AGPL-3.0)
//    See LICENSE file or visit https://www.gnu.org/licenses/agpl-3.0.html
//
// 2. Commercial License
//    Contact licensing@automatethethings.com for commercial licensing options.

package service

import (
	"bytes"
	"context"
	"errors"
	"testing"

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"
	xkmssdk "github.com/jeremyhahn/go-xkms/sdk/go"

	xkmstransport "github.com/jeremyhahn/go-xkms/sdk/go/transport"

	"github.com/jeremyhahn/go-quicbaas/pkg/tenantctx"
)

// TestNewPIVService verifies the constructor returns a non-nil service that
// satisfies the xkms PIVService contract.
func TestNewPIVService(t *testing.T) {
	svc := NewPIVService(composedXKMSService)
	if svc == nil {
		t.Fatal("NewPIVService returned nil")
	}
	var _ xkmstransport.PIVService = svc
}

// TestPIVService_NilRequests verifies every verb fails closed with the typed
// nil-request error instead of reaching the composed go-xkms service.
func TestPIVService_NilRequests(t *testing.T) {
	svc := NewPIVService(composedXKMSService)
	ctx := context.Background()

	if _, err := svc.ListPIVSlots(ctx, nil); !errors.Is(err, ErrPIVNilRequest) {
		t.Errorf("ListPIVSlots(nil) = %v, want ErrPIVNilRequest", err)
	}
	if _, err := svc.GetPIVCertificate(ctx, nil); !errors.Is(err, ErrPIVNilRequest) {
		t.Errorf("GetPIVCertificate(nil) = %v, want ErrPIVNilRequest", err)
	}
	if err := svc.StorePIVCertificate(ctx, nil); !errors.Is(err, ErrPIVNilRequest) {
		t.Errorf("StorePIVCertificate(nil) = %v, want ErrPIVNilRequest", err)
	}
	if err := svc.DeletePIVCertificate(ctx, nil); !errors.Is(err, ErrPIVNilRequest) {
		t.Errorf("DeletePIVCertificate(nil) = %v, want ErrPIVNilRequest", err)
	}
	if _, err := svc.GeneratePIVKey(ctx, nil); !errors.Is(err, ErrPIVNilRequest) {
		t.Errorf("GeneratePIVKey(nil) = %v, want ErrPIVNilRequest", err)
	}
	if err := svc.ImportPIVCertificate(ctx, nil); !errors.Is(err, ErrPIVNilRequest) {
		t.Errorf("ImportPIVCertificate(nil) = %v, want ErrPIVNilRequest", err)
	}
	if _, err := svc.ExportPIVCertificate(ctx, nil); !errors.Is(err, ErrPIVNilRequest) {
		t.Errorf("ExportPIVCertificate(nil) = %v, want ErrPIVNilRequest", err)
	}
	if _, err := svc.GeneratePIVCSR(ctx, nil); !errors.Is(err, ErrPIVNilRequest) {
		t.Errorf("GeneratePIVCSR(nil) = %v, want ErrPIVNilRequest", err)
	}
}

// TestPIVService_DelegatesToXKMS verifies the verbs reach the composed
// go-xkms service: with no PIV manager initialized in this process every
// verb must surface go-xkms's own typed error (proof of delegation — the
// error originates in go-xkms, not in QuicBaaS).
func TestPIVService_DelegatesToXKMS(t *testing.T) {
	svc := NewPIVService(composedXKMSService)
	ctx := context.Background()

	if _, err := svc.ListPIVSlots(ctx, &xkmstransport.ListPIVSlotsRequest{Backend: "file"}); err == nil {
		t.Error("ListPIVSlots with uninitialized xkms PIV manager should error")
	} else if errors.Is(err, ErrPIVNilRequest) {
		t.Errorf("ListPIVSlots error should originate from go-xkms, got %v", err)
	}
	if _, err := svc.GetPIVCertificate(ctx, &xkmstransport.GetPIVCertificateRequest{Backend: "file", Slot: "9a", Format: "pem"}); err == nil {
		t.Error("GetPIVCertificate with uninitialized xkms PIV manager should error")
	}
	if err := svc.StorePIVCertificate(ctx, &xkmstransport.StorePIVCertificateRequest{Backend: "file", Slot: "9a"}); err == nil {
		t.Error("StorePIVCertificate with uninitialized xkms PIV manager should error")
	}
	if err := svc.DeletePIVCertificate(ctx, &xkmstransport.DeletePIVCertificateRequest{Backend: "file", Slot: "9a"}); err == nil {
		t.Error("DeletePIVCertificate with uninitialized xkms PIV manager should error")
	}
	if _, err := svc.GeneratePIVKey(ctx, &xkmstransport.GeneratePIVKeyRequest{Backend: "file", Slot: "9a"}); err == nil {
		t.Error("GeneratePIVKey with uninitialized xkms PIV manager should error")
	}
	if err := svc.ImportPIVCertificate(ctx, &xkmstransport.StorePIVCertificateRequest{Backend: "file", Slot: "9a"}); err == nil {
		t.Error("ImportPIVCertificate with uninitialized xkms PIV manager should error")
	}
	if _, err := svc.ExportPIVCertificate(ctx, &xkmstransport.GetPIVCertificateRequest{Backend: "file", Slot: "9a", Format: "pem"}); err == nil {
		t.Error("ExportPIVCertificate with uninitialized xkms PIV manager should error")
	}
	if _, err := svc.GeneratePIVCSR(ctx, &xkmstransport.GeneratePIVCSRRequest{Backend: "file", Slot: "9a"}); err == nil {
		t.Error("GeneratePIVCSR with uninitialized xkms PIV manager should error")
	}
}

// TestPIVService_TenantCallerCannotTouchPlatformSlots pins Codex 7955
// (go-quicbaas#1996): the server's PIV card is platform-global state, so
// every PIV verb must run go-xkms's platform-scope check and refuse a
// tenant-scoped caller before it reaches the backend.
func TestPIVService_TenantCallerCannotTouchPlatformSlots(t *testing.T) {
	const backend = "file"
	const slot = "9a"

	memory, err := xkmssdk.NewMemoryBackend()
	if err != nil {
		t.Fatalf("NewMemoryBackend: %v", err)
	}
	store, err := xkmssdk.NewPIVFileBackend(&xkmssdk.PIVFileStorageConfig{
		Backend:    memory,
		DEREnabled: true,
	})
	if err != nil {
		t.Fatalf("NewPIVFileBackend: %v", err)
	}
	if err := xkmssdk.InitializePIV(&xkmssdk.PIVManagerConfig{
		Stores: map[string]xkmssdk.PIVCertificateStorage{backend: store},
	}); err != nil {
		t.Fatalf("InitializePIV: %v", err)
	}
	t.Cleanup(xkmssdk.XKMSResetPIV)

	svc := NewPIVService(func() *xkmssdk.XKMSService { return &xkmssdk.XKMSService{} })
	systemCtx := qrdbsdk.WithIdentity(context.Background(),
		&qrdbsdk.Identity{Subject: "platform-admin", TenantID: qrdbsdk.SystemTenantID})
	tenantCtx := qrdbsdk.WithIdentity(context.Background(),
		&qrdbsdk.Identity{Subject: "acme-admin", TenantID: "acme"})

	stored := selfSignedDER(t)
	if err := svc.StorePIVCertificate(systemCtx, &xkmstransport.StorePIVCertificateRequest{
		Backend: backend, Slot: slot, Certificate: stored, Format: "der",
	}); err != nil {
		t.Fatalf("system caller StorePIVCertificate: %v", err)
	}

	readBack := func() []byte {
		t.Helper()
		resp, err := svc.GetPIVCertificate(systemCtx, &xkmstransport.GetPIVCertificateRequest{
			Backend: backend, Slot: slot, Format: "der",
		})
		if err != nil {
			t.Fatalf("system caller GetPIVCertificate: %v", err)
		}
		return resp.Certificate
	}
	if got := readBack(); !bytes.Equal(got, stored) {
		t.Fatal("system caller should read back the certificate it stored")
	}

	tests := []struct {
		name string
		call func(ctx context.Context) error
	}{
		{"StorePIVCertificate", func(ctx context.Context) error {
			return svc.StorePIVCertificate(ctx, &xkmstransport.StorePIVCertificateRequest{
				Backend: backend, Slot: slot, Certificate: selfSignedDER(t), Format: "der",
			})
		}},
		{"DeletePIVCertificate", func(ctx context.Context) error {
			return svc.DeletePIVCertificate(ctx, &xkmstransport.DeletePIVCertificateRequest{Backend: backend, Slot: slot})
		}},
		{"GeneratePIVKey", func(ctx context.Context) error {
			_, err := svc.GeneratePIVKey(ctx, &xkmstransport.GeneratePIVKeyRequest{
				Backend: backend, Slot: slot, Algorithm: "ecdsap256", Subject: "acme",
			})
			return err
		}},
		{"ImportPIVCertificate", func(ctx context.Context) error {
			return svc.ImportPIVCertificate(ctx, &xkmstransport.StorePIVCertificateRequest{
				Backend: backend, Slot: slot, Certificate: selfSignedDER(t), Format: "der",
			})
		}},
		{"GeneratePIVCSR", func(ctx context.Context) error {
			_, err := svc.GeneratePIVCSR(ctx, &xkmstransport.GeneratePIVCSRRequest{Backend: backend, Slot: slot, Subject: "acme"})
			return err
		}},
		{"ListPIVSlots", func(ctx context.Context) error {
			_, err := svc.ListPIVSlots(ctx, &xkmstransport.ListPIVSlotsRequest{Backend: backend})
			return err
		}},
		{"GetPIVCertificate", func(ctx context.Context) error {
			_, err := svc.GetPIVCertificate(ctx, &xkmstransport.GetPIVCertificateRequest{Backend: backend, Slot: slot, Format: "der"})
			return err
		}},
		{"ExportPIVCertificate", func(ctx context.Context) error {
			_, err := svc.ExportPIVCertificate(ctx, &xkmstransport.GetPIVCertificateRequest{Backend: backend, Slot: slot, Format: "der"})
			return err
		}},
	}
	// #2026: the embedded transport binds an in-process caller's tenant only
	// through tenantctx, with no qrdb identity; it must be refused the same.
	callers := map[string]context.Context{
		"identity":       tenantCtx,
		"tenantctx-only": tenantctx.WithTenant(context.Background(), "acme"),
	}
	for caller, ctx := range callers {
		for _, tt := range tests {
			t.Run(caller+"/"+tt.name, func(t *testing.T) {
				err := tt.call(ctx)
				if !errors.Is(err, qrdbsdk.ErrTenantMissing) {
					t.Errorf("Codex 7955 / #1996 / #2026: %s tenant caller %s must be refused by the platform-scope check, got %v", caller, tt.name, err)
				}
				if got := readBack(); !bytes.Equal(got, stored) {
					t.Errorf("Codex 7955 / #1996 / #2026: %s tenant caller %s changed the platform PIV slot %s", caller, tt.name, slot)
				}
			})
		}
	}

	// A tenantctx-only system caller keeps platform reach.
	systemOnly := tenantctx.WithSystem(context.Background())
	replacement := selfSignedDER(t)
	if err := svc.StorePIVCertificate(systemOnly, &xkmstransport.StorePIVCertificateRequest{
		Backend: backend, Slot: slot, Certificate: replacement, Format: "der",
	}); err != nil {
		t.Fatalf("tenantctx-only system caller StorePIVCertificate: %v", err)
	}
	if got := readBack(); !bytes.Equal(got, replacement) {
		t.Fatal("tenantctx-only system caller should overwrite the platform PIV slot")
	}
	if err := svc.DeletePIVCertificate(systemOnly, &xkmstransport.DeletePIVCertificateRequest{Backend: backend, Slot: slot}); err != nil {
		t.Fatalf("tenantctx-only system caller DeletePIVCertificate: %v", err)
	}
}
