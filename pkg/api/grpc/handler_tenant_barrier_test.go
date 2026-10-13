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

package grpc

import (
	"bytes"
	"context"
	stderrors "errors"
	"fmt"
	"testing"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"
	qrdbtransport "github.com/jeremyhahn/go-qrdb/sdk/go/transport"

	pb "github.com/jeremyhahn/go-quicbaas/api/proto"
)

// errTenantBarrierGRPCBoom drives the generic (non-sentinel) error branch.
var errTenantBarrierGRPCBoom = stderrors.New("tenant barrier grpc boom")

// fakeTenantBarrierServicer is a hand-written test double for
// service.TenantBarrierServicer. Methods delegate to optional function fields
// so a test can shape each path.
type fakeTenantBarrierServicer struct {
	initFn        func(ctx context.Context, tenantID string, passphrase []byte, threshold, shares int, strategy string) (*qrdbtransport.TenantBarrierInitResponse, error)
	unsealFn      func(ctx context.Context, tenantID string, key []byte) error
	unsealShareFn func(ctx context.Context, tenantID, share string) (*qrdbtransport.TenantBarrierUnsealProgress, error)
	sealFn        func(ctx context.Context, tenantID string) error
	statusFn      func(ctx context.Context, tenantID string) (*qrdbtransport.TenantBarrierStatusResponse, error)
}

func (f *fakeTenantBarrierServicer) TenantBarrierInit(ctx context.Context, tenantID string, passphrase []byte, threshold, shares int, strategy string) (*qrdbtransport.TenantBarrierInitResponse, error) {
	if f.initFn != nil {
		return f.initFn(ctx, tenantID, passphrase, threshold, shares, strategy)
	}
	return &qrdbtransport.TenantBarrierInitResponse{TenantID: tenantID}, nil
}

func (f *fakeTenantBarrierServicer) TenantBarrierUnseal(ctx context.Context, tenantID string, key []byte) error {
	if f.unsealFn != nil {
		return f.unsealFn(ctx, tenantID, key)
	}
	return nil
}

func (f *fakeTenantBarrierServicer) TenantBarrierUnsealShare(ctx context.Context, tenantID, share string) (*qrdbtransport.TenantBarrierUnsealProgress, error) {
	if f.unsealShareFn != nil {
		return f.unsealShareFn(ctx, tenantID, share)
	}
	return &qrdbtransport.TenantBarrierUnsealProgress{TenantID: tenantID}, nil
}

func (f *fakeTenantBarrierServicer) TenantBarrierSeal(ctx context.Context, tenantID string) error {
	if f.sealFn != nil {
		return f.sealFn(ctx, tenantID)
	}
	return nil
}

func (f *fakeTenantBarrierServicer) TenantBarrierStatus(ctx context.Context, tenantID string) (*qrdbtransport.TenantBarrierStatusResponse, error) {
	if f.statusFn != nil {
		return f.statusFn(ctx, tenantID)
	}
	return &qrdbtransport.TenantBarrierStatusResponse{TenantID: tenantID}, nil
}

// requireGRPCCode asserts err is a gRPC status error carrying the expected code.
func requireGRPCCode(t *testing.T, err error, want codes.Code) *status.Status {
	t.Helper()
	if err == nil {
		t.Fatalf("expected a gRPC status error with code %s, got nil", want)
	}
	st, ok := status.FromError(err)
	if !ok {
		t.Fatalf("expected a gRPC status error, got %v", err)
	}
	if st.Code() != want {
		t.Fatalf("expected code %s, got %s (%s)", want, st.Code(), st.Message())
	}
	return st
}

// =============================================================================
// mapTenantBarrierError: qrdb canon — tenant.ErrBarrierNotEnabled maps to
// codes.FailedPrecondition with the canonical message preserved. Everything
// else keeps the shared ToGRPCError classification.
// =============================================================================

func TestMapTenantBarrierError_Nil(t *testing.T) {
	if got := mapTenantBarrierError(nil); got != nil {
		t.Fatalf("expected nil, got %v", got)
	}
}

func TestMapTenantBarrierError_BarrierNotEnabled(t *testing.T) {
	st := requireGRPCCode(t, mapTenantBarrierError(qrdbsdk.ErrTenantBarrierNotEnabled), codes.FailedPrecondition)
	if st.Message() != qrdbsdk.ErrTenantBarrierNotEnabled.Error() {
		t.Fatalf("message not preserved: %q", st.Message())
	}
}

func TestMapTenantBarrierError_BarrierNotEnabledWrapped(t *testing.T) {
	// A %w-wrapped chain must classify identically via errors.Is traversal,
	// with the full wrapped message preserved.
	wrapped := fmt.Errorf("tenant barrier unseal: %w", qrdbsdk.ErrTenantBarrierNotEnabled)
	st := requireGRPCCode(t, mapTenantBarrierError(wrapped), codes.FailedPrecondition)
	if st.Message() != wrapped.Error() {
		t.Fatalf("wrapped message not preserved: %q", st.Message())
	}
}

func TestMapTenantBarrierError_GenericErrorStaysInternal(t *testing.T) {
	// The FailedPrecondition classification is scoped to the sentinel; a plain
	// error keeps the ToGRPCError default (Internal).
	requireGRPCCode(t, mapTenantBarrierError(errTenantBarrierGRPCBoom), codes.Internal)
}

// =============================================================================
// RPC-level contract: every TenantBarrier verb surfaces FailedPrecondition
// when the facade fails with the barrier-not-enabled sentinel.
// =============================================================================

func TestTenantBarrierRPCs_BarrierNotEnabled_FailedPrecondition(t *testing.T) {
	sentinel := qrdbsdk.ErrTenantBarrierNotEnabled
	svc := &fakeTenantBarrierServicer{
		initFn: func(context.Context, string, []byte, int, int, string) (*qrdbtransport.TenantBarrierInitResponse, error) {
			return nil, sentinel
		},
		unsealFn: func(context.Context, string, []byte) error { return sentinel },
		unsealShareFn: func(context.Context, string, string) (*qrdbtransport.TenantBarrierUnsealProgress, error) {
			return nil, sentinel
		},
		sealFn: func(context.Context, string) error { return sentinel },
		statusFn: func(context.Context, string) (*qrdbtransport.TenantBarrierStatusResponse, error) {
			return nil, sentinel
		},
	}
	srv := NewTenantBarrierGRPCServer(svc, nil)
	ctx := context.Background()

	cases := []struct {
		name string
		call func() error
	}{
		{"init", func() error {
			_, err := srv.Init(ctx, &pb.TenantBarrierInitRequest{TenantId: "t1", Strategy: "passphrase"})
			return err
		}},
		{"unseal", func() error {
			_, err := srv.Unseal(ctx, &pb.TenantBarrierUnsealRequest{TenantId: "t1", Key: []byte("k")})
			return err
		}},
		{"unseal_share", func() error {
			_, err := srv.UnsealShare(ctx, &pb.TenantBarrierUnsealShareRequest{TenantId: "t1", Share: "s"})
			return err
		}},
		{"seal", func() error {
			_, err := srv.Seal(ctx, &pb.TenantBarrierSealRequest{TenantId: "t1"})
			return err
		}},
		{"status", func() error {
			_, err := srv.Status(ctx, &pb.TenantBarrierStatusRequest{TenantId: "t1"})
			return err
		}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			st := requireGRPCCode(t, c.call(), codes.FailedPrecondition)
			if st.Message() != sentinel.Error() {
				t.Fatalf("canonical message not preserved: %q", st.Message())
			}
		})
	}
}

// TestTenantBarrierRPCs_GenericErrorNotFailedPrecondition pins the negative:
// a generic facade failure must NOT be classified as FailedPrecondition.
func TestTenantBarrierRPCs_GenericErrorNotFailedPrecondition(t *testing.T) {
	svc := &fakeTenantBarrierServicer{
		sealFn: func(context.Context, string) error { return errTenantBarrierGRPCBoom },
	}
	srv := NewTenantBarrierGRPCServer(svc, nil)
	_, err := srv.Seal(context.Background(), &pb.TenantBarrierSealRequest{TenantId: "t1"})
	requireGRPCCode(t, err, codes.Internal)
}

func TestTenantBarrierRPCs_NonUTF8CredentialReachesServiceIntactAndIsWiped(t *testing.T) {
	var initSeen, unsealSeen []byte
	svc := &fakeTenantBarrierServicer{
		initFn: func(_ context.Context, _ string, passphrase []byte, _, _ int, _ string) (*qrdbtransport.TenantBarrierInitResponse, error) {
			if !bytes.Equal(passphrase, nonUTF8Credential) {
				t.Errorf("init passphrase = %x, want %x", passphrase, nonUTF8Credential)
			}
			initSeen = passphrase
			return nil, nil
		},
		unsealFn: func(_ context.Context, _ string, key []byte) error {
			if !bytes.Equal(key, nonUTF8Credential) {
				t.Errorf("unseal key = %x, want %x", key, nonUTF8Credential)
			}
			unsealSeen = key
			return nil
		},
	}
	srv := NewTenantBarrierGRPCServer(svc, nil)
	ctx := context.Background()

	initReq := &pb.TenantBarrierInitRequest{TenantId: "t1", Strategy: "passphrase", Passphrase: bytes.Clone(nonUTF8Credential)}
	if _, err := srv.Init(ctx, overTheWire(t, initReq)); err != nil {
		t.Fatalf("Init: %v", err)
	}
	unsealReq := &pb.TenantBarrierUnsealRequest{TenantId: "t1", Key: bytes.Clone(nonUTF8Credential)}
	if _, err := srv.Unseal(ctx, overTheWire(t, unsealReq)); err != nil {
		t.Fatalf("Unseal: %v", err)
	}

	for name, seen := range map[string][]byte{"init passphrase": initSeen, "unseal key": unsealSeen} {
		if len(seen) == 0 {
			t.Fatalf("%s: the double retained nothing; the test cannot prove the wipe", name)
		}
		if !bytes.Equal(seen, make([]byte, len(seen))) {
			t.Fatalf("%s is %x after the call, want zeros: the handler did not wipe it", name, seen)
		}
	}
}

func TestTenantBarrierEnableEncryption_NonUTF8PassphraseReachesServiceIntact(t *testing.T) {
	var matched bool
	barrier := &mockBarrierService{
		enableEncryptionFn: func(_ context.Context, tenantID string, opts qrdbsdk.MigrationOptions) error {
			if tenantID != "t1" {
				t.Errorf("tenant = %q, want t1", tenantID)
			}
			plain, err := opts.Credentials.Secret.Open()
			if err != nil {
				t.Errorf("open the forwarded credential: %v", err)
				return nil
			}
			defer plain.Destroy()
			matched = plain.EqualTo(nonUTF8Credential)
			return nil
		},
	}
	srv := NewTenantBarrierGRPCServer(&fakeTenantBarrierServicer{}, nil)
	srv.SetBarrierService(barrier)
	req := &pb.TenantBarrierEnableEncryptionRequest{TenantId: "t1", TargetStrategy: "passphrase", Passphrase: bytes.Clone(nonUTF8Credential)}

	if _, err := srv.EnableEncryption(context.Background(), overTheWire(t, req)); err != nil {
		t.Fatalf("EnableEncryption: %v", err)
	}
	if !matched {
		t.Fatal("the service did not receive the passphrase bytes the client sent")
	}
}
