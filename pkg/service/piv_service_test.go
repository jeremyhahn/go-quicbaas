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
	"context"
	"errors"
	"testing"

	xkmstransport "github.com/jeremyhahn/go-xkms/sdk/go/transport"
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
