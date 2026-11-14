// Copyright (c) 2025 Jeremy Hahn
// Copyright (c) 2025 Automate The Things, LLC
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
	"testing"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	gproto "google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/emptypb"

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"

	"github.com/jeremyhahn/go-qrdb/sdk/go/transport"
	pb "github.com/jeremyhahn/go-quicbaas/api/proto"
	dberrors "github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
)

// ---------------------------------------------------------------------------
// Mock BarrierServicer
// ---------------------------------------------------------------------------

// mockBarrierService implements service.BarrierServicer for unit tests. Each
// method dispatches to an injected closure when set, otherwise it returns
// zero values.
type mockBarrierService struct {
	statusFn            func(ctx context.Context) (*transport.BarrierStatusResponse, error)
	initFn              func(ctx context.Context, secret []byte) error
	unsealFn            func(ctx context.Context, secret []byte) error
	sealFn              func(ctx context.Context, req *service.BarrierSealRequest) error
	initShamirFn        func(ctx context.Context, secret []byte, threshold, total int) (*transport.BarrierInitializeShamirResponse, error)
	unsealShareFn       func(ctx context.Context, share string) (*transport.BarrierUnsealShareResponse, error)
	unsealSharesFn      func(ctx context.Context, shares []string) error
	rekeyFn             func(ctx context.Context, threshold, total int) (*transport.BarrierRekeyResponse, error)
	generateRecoveryFn  func(ctx context.Context, threshold, total int) (*transport.BarrierRecoveryKeysResponse, error)
	recoverWithKeysFn   func(ctx context.Context, keys []string) error
	deleteRecoveryFn    func(ctx context.Context) error
	hasRecoveryFn       func(ctx context.Context) (bool, error)
	generateRootTokenFn func(ctx context.Context, shares []string) (*transport.BarrierRootTokenResponse, error)
	enableEncryptionFn  func(ctx context.Context, tenantID string, opts qrdbsdk.MigrationOptions) error
}

var _ service.BarrierServicer = (*mockBarrierService)(nil)

func (m *mockBarrierService) BarrierStatus(ctx context.Context) (*transport.BarrierStatusResponse, error) {
	if m.statusFn != nil {
		return m.statusFn(ctx)
	}
	return &transport.BarrierStatusResponse{}, nil
}

func (m *mockBarrierService) BarrierInitialize(ctx context.Context, secret []byte) error {
	if m.initFn != nil {
		return m.initFn(ctx, secret)
	}
	return nil
}

func (m *mockBarrierService) BarrierUnseal(ctx context.Context, secret []byte) error {
	if m.unsealFn != nil {
		return m.unsealFn(ctx, secret)
	}
	return nil
}

func (m *mockBarrierService) BarrierSeal(ctx context.Context, req *service.BarrierSealRequest) error {
	if m.sealFn != nil {
		return m.sealFn(ctx, req)
	}
	return nil
}

func (m *mockBarrierService) BarrierInitializeShamir(ctx context.Context, secret []byte, threshold, total int) (*transport.BarrierInitializeShamirResponse, error) {
	if m.initShamirFn != nil {
		return m.initShamirFn(ctx, secret, threshold, total)
	}
	return &transport.BarrierInitializeShamirResponse{Threshold: threshold, TotalShares: total}, nil
}

func (m *mockBarrierService) BarrierUnsealWithShare(ctx context.Context, share string) (*transport.BarrierUnsealShareResponse, error) {
	if m.unsealShareFn != nil {
		return m.unsealShareFn(ctx, share)
	}
	return &transport.BarrierUnsealShareResponse{}, nil
}

func (m *mockBarrierService) BarrierUnsealWithShares(ctx context.Context, shares []string) error {
	if m.unsealSharesFn != nil {
		return m.unsealSharesFn(ctx, shares)
	}
	return nil
}

func (m *mockBarrierService) BarrierRekey(ctx context.Context, threshold, total int) (*transport.BarrierRekeyResponse, error) {
	if m.rekeyFn != nil {
		return m.rekeyFn(ctx, threshold, total)
	}
	return &transport.BarrierRekeyResponse{Threshold: threshold, TotalShares: total}, nil
}

func (m *mockBarrierService) BarrierGenerateRecoveryKeys(ctx context.Context, threshold, total int) (*transport.BarrierRecoveryKeysResponse, error) {
	if m.generateRecoveryFn != nil {
		return m.generateRecoveryFn(ctx, threshold, total)
	}
	return &transport.BarrierRecoveryKeysResponse{Threshold: threshold, Total: total}, nil
}

func (m *mockBarrierService) BarrierRecoverWithKeys(ctx context.Context, keys []string) error {
	if m.recoverWithKeysFn != nil {
		return m.recoverWithKeysFn(ctx, keys)
	}
	return nil
}

func (m *mockBarrierService) BarrierDeleteRecoveryKeys(ctx context.Context) error {
	if m.deleteRecoveryFn != nil {
		return m.deleteRecoveryFn(ctx)
	}
	return nil
}

func (m *mockBarrierService) BarrierHasRecoveryKeys(ctx context.Context) (bool, error) {
	if m.hasRecoveryFn != nil {
		return m.hasRecoveryFn(ctx)
	}
	return false, nil
}

func (m *mockBarrierService) BarrierGenerateRootToken(ctx context.Context, shares []string) (*transport.BarrierRootTokenResponse, error) {
	if m.generateRootTokenFn != nil {
		return m.generateRootTokenFn(ctx, shares)
	}
	return &transport.BarrierRootTokenResponse{}, nil
}

func (m *mockBarrierService) EnableEncryption(ctx context.Context, tenantID string, opts qrdbsdk.MigrationOptions) error {
	if m.enableEncryptionFn != nil {
		return m.enableEncryptionFn(ctx, tenantID, opts)
	}
	return nil
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// newTestBarrierServer constructs a BarrierServer over the supplied mock.
// Passing nil yields an unwired server used to exercise the
// requireBarrierService short-circuit.
func newTestBarrierServer(svc service.BarrierServicer) *BarrierServer {
	log, _ := logger.New(logger.LogConfig{Level: "error"})
	return NewBarrierServer(svc, log)
}

// assertGRPCCode fails the test when the status code on err does not
// match the expected code.
func assertGRPCCode(t *testing.T, err error, want codes.Code) {
	t.Helper()
	if err == nil {
		t.Fatalf("expected error with code %s, got nil", want)
	}
	st, ok := status.FromError(err)
	if !ok {
		t.Fatalf("expected gRPC status error, got %T: %v", err, err)
	}
	if st.Code() != want {
		t.Fatalf("expected gRPC code %s, got %s (%s)", want, st.Code(), st.Message())
	}
}

// ---------------------------------------------------------------------------
// Construction / registration
// ---------------------------------------------------------------------------

func TestNewBarrierServer_NilLoggerAllowed(t *testing.T) {
	srv := NewBarrierServer(&mockBarrierService{}, nil)
	if srv == nil {
		t.Fatal("expected non-nil server")
	}
	if srv.logger != nil {
		t.Fatal("expected nil logger when none supplied")
	}
}

func TestNewBarrierServer_WithLogger(t *testing.T) {
	log, _ := logger.New(logger.LogConfig{Level: "error"})
	srv := NewBarrierServer(&mockBarrierService{}, log)
	if srv == nil {
		t.Fatal("expected non-nil server")
	}
	if srv.logger == nil {
		t.Fatal("expected non-nil logger")
	}
}

// ---------------------------------------------------------------------------
// requireBarrierService / mapBarrierError
// ---------------------------------------------------------------------------

func TestRequireBarrierService_NilSvcReturnsUnavailable(t *testing.T) {
	srv := newTestBarrierServer(nil)
	_, err := srv.BarrierStatus(context.Background(), &emptypb.Empty{})
	assertGRPCCode(t, err, codes.Unavailable)
}

func TestMapBarrierError_NilReturnsNil(t *testing.T) {
	if got := mapBarrierError(nil); got != nil {
		t.Fatalf("expected nil, got %v", got)
	}
}

func TestMapBarrierError_MissingErrorMapsToUnavailable(t *testing.T) {
	err := mapBarrierError(&service.BarrierServiceMissingError{})
	assertGRPCCode(t, err, codes.Unavailable)
}

func TestMapBarrierError_GenericErrorMapsViaToGRPCError(t *testing.T) {
	err := mapBarrierError(stderrors.New("boom"))
	if err == nil {
		t.Fatal("expected non-nil error")
	}
	if _, ok := status.FromError(err); !ok {
		t.Fatalf("expected gRPC status error, got %T", err)
	}
}

// ---------------------------------------------------------------------------
// BarrierStatus
// ---------------------------------------------------------------------------

func TestBarrierStatus_Success(t *testing.T) {
	initAt := time.Date(2025, 1, 2, 3, 4, 5, 0, time.UTC).Format(time.RFC3339)
	mock := &mockBarrierService{
		statusFn: func(ctx context.Context) (*transport.BarrierStatusResponse, error) {
			return &transport.BarrierStatusResponse{
				Sealed:         true,
				Strategy:       "shamir",
				HardwareBacked: true,
				InitializedAt:  initAt,
			}, nil
		},
	}
	srv := newTestBarrierServer(mock)
	resp, err := srv.BarrierStatus(context.Background(), &emptypb.Empty{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !resp.GetSealed() || resp.GetStrategy() != "shamir" || !resp.GetHardwareBacked() {
		t.Fatalf("unexpected response: %#v", resp)
	}
	if resp.GetInitializedAt() != initAt {
		t.Fatalf("unexpected initialized_at: %s", resp.GetInitializedAt())
	}
}

func TestBarrierStatus_FacadeErrorBubblesUp(t *testing.T) {
	mock := &mockBarrierService{
		statusFn: func(ctx context.Context) (*transport.BarrierStatusResponse, error) {
			return nil, stderrors.New("disk failure")
		},
	}
	srv := newTestBarrierServer(mock)
	_, err := srv.BarrierStatus(context.Background(), &emptypb.Empty{})
	if err == nil {
		t.Fatal("expected error")
	}
}

// ---------------------------------------------------------------------------
// BarrierInitialize / BarrierUnseal
// ---------------------------------------------------------------------------

func TestBarrierInitialize_Success(t *testing.T) {
	var captured string
	mock := &mockBarrierService{
		initFn: func(_ context.Context, secret []byte) error {
			// Copy: the handler wipes this slice once the call returns, so a
			// double that kept the header would assert against zeros.
			captured = string(secret)
			return nil
		},
	}
	srv := newTestBarrierServer(mock)
	_, err := srv.BarrierInitialize(context.Background(), &pb.BarrierInitializeRequest{Secret: []byte("topsecret")})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if captured != "topsecret" {
		t.Fatalf("expected secret propagation, got %q", captured)
	}
}

func TestBarrierInitialize_NilRequestRejected(t *testing.T) {
	srv := newTestBarrierServer(&mockBarrierService{})
	_, err := srv.BarrierInitialize(context.Background(), nil)
	assertGRPCCode(t, err, codes.InvalidArgument)
}

func TestBarrierUnseal_NilRequestRejected(t *testing.T) {
	srv := newTestBarrierServer(&mockBarrierService{})
	_, err := srv.BarrierUnseal(context.Background(), nil)
	assertGRPCCode(t, err, codes.InvalidArgument)
}

func TestBarrierUnseal_Success(t *testing.T) {
	mock := &mockBarrierService{
		unsealFn: func(_ context.Context, secret []byte) error {
			if string(secret) != "key" {
				t.Fatalf("expected secret 'key', got %q", secret)
			}
			return nil
		},
	}
	srv := newTestBarrierServer(mock)
	_, err := srv.BarrierUnseal(context.Background(), &pb.BarrierUnsealRequest{Secret: []byte("key")})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestBarrierSeal_Success(t *testing.T) {
	called := false
	mock := &mockBarrierService{
		sealFn: func(_ context.Context, _ *service.BarrierSealRequest) error {
			called = true
			return nil
		},
	}
	srv := newTestBarrierServer(mock)
	_, err := srv.BarrierSeal(context.Background(), &pb.BarrierSealRequest{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !called {
		t.Fatal("expected sealFn to be called")
	}
}

func TestBarrierSeal_FacadeErrorBubblesUp(t *testing.T) {
	mock := &mockBarrierService{
		sealFn: func(_ context.Context, _ *service.BarrierSealRequest) error { return stderrors.New("nope") },
	}
	srv := newTestBarrierServer(mock)
	_, err := srv.BarrierSeal(context.Background(), &pb.BarrierSealRequest{})
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestBarrierSeal_SOPIN(t *testing.T) {
	const soPIN = "so-pin-1234"
	tests := []struct {
		name     string
		pin      string
		wantCode codes.Code
	}{
		{name: "right PIN seals", pin: soPIN, wantCode: codes.OK},
		{name: "wrong PIN is refused", pin: "wrong", wantCode: codes.PermissionDenied},
		{name: "missing PIN is refused", pin: "", wantCode: codes.PermissionDenied},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var got []byte
			mock := &mockBarrierService{
				sealFn: func(_ context.Context, req *service.BarrierSealRequest) error {
					got = req.SOPIN
					if string(req.SOPIN) != soPIN {
						return &dberrors.QBError{Code: dberrors.ErrForbidden, Op: "barrier.Seal", Err: qrdbsdk.ErrPINInvalid}
					}
					return nil
				},
			}
			srv := newTestBarrierServer(mock)
			_, err := srv.BarrierSeal(context.Background(), &pb.BarrierSealRequest{SoPin: tt.pin})
			if code := status.Code(err); code != tt.wantCode {
				t.Fatalf("expected %v, got %v (%v)", tt.wantCode, code, err)
			}
			for _, b := range got {
				if b != 0 {
					t.Fatalf("handler must wipe the SO PIN after the call, got %q", got)
				}
			}
		})
	}
}

// ---------------------------------------------------------------------------
// BarrierShamirInitialize
// ---------------------------------------------------------------------------

func TestBarrierShamirInitialize_Success(t *testing.T) {
	mock := &mockBarrierService{
		initShamirFn: func(_ context.Context, secret []byte, threshold, total int) (*transport.BarrierInitializeShamirResponse, error) {
			return &transport.BarrierInitializeShamirResponse{
				Threshold:   threshold,
				TotalShares: total,
				ShareCount:  total,
			}, nil
		},
	}
	srv := newTestBarrierServer(mock)
	resp, err := srv.BarrierShamirInitialize(context.Background(), &pb.BarrierShamirInitializeRequest{
		Secret:      []byte("s"),
		Threshold:   2,
		TotalShares: 3,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.GetThreshold() != 2 || resp.GetTotalShares() != 3 {
		t.Fatalf("unexpected response: %#v", resp)
	}
	// Share-free contract: share material must never appear on the wire.
	if len(resp.GetShares()) != 0 {
		t.Fatalf("shares leaked on the wire: %#v", resp)
	}
}

func TestBarrierShamirInitialize_NilRequest(t *testing.T) {
	srv := newTestBarrierServer(&mockBarrierService{})
	_, err := srv.BarrierShamirInitialize(context.Background(), nil)
	assertGRPCCode(t, err, codes.InvalidArgument)
}

func TestBarrierShamirInitialize_ValidationErrors(t *testing.T) {
	srv := newTestBarrierServer(&mockBarrierService{})
	cases := []struct {
		name string
		req  *pb.BarrierShamirInitializeRequest
	}{
		{"zero threshold", &pb.BarrierShamirInitializeRequest{Threshold: 0, TotalShares: 3}},
		{"zero total", &pb.BarrierShamirInitializeRequest{Threshold: 2, TotalShares: 0}},
		{"threshold > total", &pb.BarrierShamirInitializeRequest{Threshold: 4, TotalShares: 3}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := srv.BarrierShamirInitialize(context.Background(), c.req)
			assertGRPCCode(t, err, codes.InvalidArgument)
		})
	}
}

// ---------------------------------------------------------------------------
// BarrierShamirUnsealShare / BarrierShamirUnsealShares
// ---------------------------------------------------------------------------

func TestBarrierShamirUnsealShare_Success(t *testing.T) {
	mock := &mockBarrierService{
		unsealShareFn: func(_ context.Context, share string) (*transport.BarrierUnsealShareResponse, error) {
			return &transport.BarrierUnsealShareResponse{Required: 3, Submitted: 1, Complete: false}, nil
		},
	}
	srv := newTestBarrierServer(mock)
	resp, err := srv.BarrierShamirUnsealShare(context.Background(), &pb.BarrierShamirUnsealShareRequest{Share: "abc"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.GetRequired() != 3 || resp.GetSubmitted() != 1 || resp.GetComplete() {
		t.Fatalf("unexpected response: %#v", resp)
	}
}

func TestBarrierShamirUnsealShare_EmptyShareRejected(t *testing.T) {
	srv := newTestBarrierServer(&mockBarrierService{})
	_, err := srv.BarrierShamirUnsealShare(context.Background(), &pb.BarrierShamirUnsealShareRequest{Share: ""})
	assertGRPCCode(t, err, codes.InvalidArgument)
}

func TestBarrierShamirUnsealShare_NilRequest(t *testing.T) {
	srv := newTestBarrierServer(&mockBarrierService{})
	_, err := srv.BarrierShamirUnsealShare(context.Background(), nil)
	assertGRPCCode(t, err, codes.InvalidArgument)
}

func TestBarrierShamirUnsealShares_Success(t *testing.T) {
	got := []string{}
	mock := &mockBarrierService{
		unsealSharesFn: func(_ context.Context, shares []string) error {
			got = shares
			return nil
		},
	}
	srv := newTestBarrierServer(mock)
	_, err := srv.BarrierShamirUnsealShares(context.Background(), &pb.BarrierShamirUnsealSharesRequest{Shares: []string{"a", "b"}})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("expected 2 shares forwarded, got %d", len(got))
	}
}

func TestBarrierShamirUnsealShares_EmptyRejected(t *testing.T) {
	srv := newTestBarrierServer(&mockBarrierService{})
	_, err := srv.BarrierShamirUnsealShares(context.Background(), &pb.BarrierShamirUnsealSharesRequest{Shares: nil})
	assertGRPCCode(t, err, codes.InvalidArgument)
}

func TestBarrierShamirUnsealShares_NilRequest(t *testing.T) {
	srv := newTestBarrierServer(&mockBarrierService{})
	_, err := srv.BarrierShamirUnsealShares(context.Background(), nil)
	assertGRPCCode(t, err, codes.InvalidArgument)
}

// ---------------------------------------------------------------------------
// Shamir share metadata operations
// ---------------------------------------------------------------------------

// ---------------------------------------------------------------------------
// BarrierRekey / Recovery / RootToken
// ---------------------------------------------------------------------------

func TestBarrierRekey_Success(t *testing.T) {
	mock := &mockBarrierService{
		rekeyFn: func(_ context.Context, threshold, total int) (*transport.BarrierRekeyResponse, error) {
			return &transport.BarrierRekeyResponse{Threshold: threshold, TotalShares: total, ShareCount: total}, nil
		},
	}
	srv := newTestBarrierServer(mock)
	resp, err := srv.BarrierRekey(context.Background(), &pb.BarrierRekeyRequest{Threshold: 2, Total: 3})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.GetThreshold() != 2 || resp.GetTotalShares() != 3 {
		t.Fatalf("unexpected response: %#v", resp)
	}
	// Share-free contract: share material must never appear on the wire.
	if len(resp.GetShares()) != 0 {
		t.Fatalf("shares leaked on the wire: %#v", resp)
	}
}

func TestBarrierRekey_ValidationErrors(t *testing.T) {
	srv := newTestBarrierServer(&mockBarrierService{})
	cases := []struct {
		name string
		req  *pb.BarrierRekeyRequest
	}{
		{"nil request", nil},
		{"zero threshold", &pb.BarrierRekeyRequest{Threshold: 0, Total: 3}},
		{"zero total", &pb.BarrierRekeyRequest{Threshold: 2, Total: 0}},
		{"threshold > total", &pb.BarrierRekeyRequest{Threshold: 5, Total: 3}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := srv.BarrierRekey(context.Background(), c.req)
			assertGRPCCode(t, err, codes.InvalidArgument)
		})
	}
}

func TestBarrierRecoveryGenerate_Success(t *testing.T) {
	mock := &mockBarrierService{
		generateRecoveryFn: func(_ context.Context, threshold, total int) (*transport.BarrierRecoveryKeysResponse, error) {
			return &transport.BarrierRecoveryKeysResponse{Threshold: threshold, Total: total, ShareCount: total}, nil
		},
	}
	srv := newTestBarrierServer(mock)
	resp, err := srv.BarrierRecoveryGenerate(context.Background(), &pb.BarrierRecoveryGenerateRequest{Threshold: 2, Total: 3})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.GetThreshold() != 2 || resp.GetTotal() != 3 {
		t.Fatalf("unexpected response: %#v", resp)
	}
	// Share-free contract: recovery key material must never appear on the wire.
	if len(resp.GetKeys()) != 0 {
		t.Fatalf("recovery keys leaked on the wire: %#v", resp)
	}
}

func TestBarrierRecoveryGenerate_ValidationErrors(t *testing.T) {
	srv := newTestBarrierServer(&mockBarrierService{})
	cases := []struct {
		name string
		req  *pb.BarrierRecoveryGenerateRequest
	}{
		{"nil request", nil},
		{"zero threshold", &pb.BarrierRecoveryGenerateRequest{Threshold: 0, Total: 3}},
		{"zero total", &pb.BarrierRecoveryGenerateRequest{Threshold: 2, Total: 0}},
		{"threshold > total", &pb.BarrierRecoveryGenerateRequest{Threshold: 5, Total: 3}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := srv.BarrierRecoveryGenerate(context.Background(), c.req)
			assertGRPCCode(t, err, codes.InvalidArgument)
		})
	}
}

func TestBarrierRecoveryRecover_Success(t *testing.T) {
	got := []string{}
	mock := &mockBarrierService{
		recoverWithKeysFn: func(_ context.Context, keys []string) error {
			got = keys
			return nil
		},
	}
	srv := newTestBarrierServer(mock)
	_, err := srv.BarrierRecoveryRecover(context.Background(), &pb.BarrierRecoveryRecoverRequest{Keys: []string{"k1", "k2"}})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("expected 2 keys propagated, got %d", len(got))
	}
}

func TestBarrierRecoveryRecover_NilRequest(t *testing.T) {
	srv := newTestBarrierServer(&mockBarrierService{})
	_, err := srv.BarrierRecoveryRecover(context.Background(), nil)
	assertGRPCCode(t, err, codes.InvalidArgument)
}

func TestBarrierRecoveryRecover_EmptyKeysRejected(t *testing.T) {
	srv := newTestBarrierServer(&mockBarrierService{})
	_, err := srv.BarrierRecoveryRecover(context.Background(), &pb.BarrierRecoveryRecoverRequest{Keys: nil})
	assertGRPCCode(t, err, codes.InvalidArgument)
}

func TestBarrierRecoveryDelete_Success(t *testing.T) {
	called := false
	mock := &mockBarrierService{
		deleteRecoveryFn: func(_ context.Context) error {
			called = true
			return nil
		},
	}
	srv := newTestBarrierServer(mock)
	_, err := srv.BarrierRecoveryDelete(context.Background(), &emptypb.Empty{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !called {
		t.Fatal("expected deleteRecoveryFn invocation")
	}
}

func TestBarrierRecoveryStatus_Success(t *testing.T) {
	mock := &mockBarrierService{
		hasRecoveryFn: func(_ context.Context) (bool, error) { return true, nil },
	}
	srv := newTestBarrierServer(mock)
	resp, err := srv.BarrierRecoveryStatus(context.Background(), &emptypb.Empty{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !resp.GetHasRecoveryKeys() {
		t.Fatal("expected has_recovery_keys=true")
	}
}

func TestBarrierRecoveryStatus_FacadeError(t *testing.T) {
	mock := &mockBarrierService{
		hasRecoveryFn: func(_ context.Context) (bool, error) { return false, stderrors.New("nope") },
	}
	srv := newTestBarrierServer(mock)
	_, err := srv.BarrierRecoveryStatus(context.Background(), &emptypb.Empty{})
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestBarrierRootToken_Success(t *testing.T) {
	created := time.Date(2025, 5, 6, 7, 8, 9, 0, time.UTC).Format(time.RFC3339)
	mock := &mockBarrierService{
		generateRootTokenFn: func(_ context.Context, shares []string) (*transport.BarrierRootTokenResponse, error) {
			return &transport.BarrierRootTokenResponse{Token: "tok", CreatedAt: created}, nil
		},
	}
	srv := newTestBarrierServer(mock)
	resp, err := srv.BarrierRootToken(context.Background(), &pb.BarrierRootTokenRequest{Shares: []string{"a", "b"}})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.GetToken() != "tok" {
		t.Fatalf("expected token 'tok', got %q", resp.GetToken())
	}
	if resp.GetCreatedAt() != created {
		t.Fatalf("unexpected created_at: %s", resp.GetCreatedAt())
	}
}

func TestBarrierRootToken_NilRequest(t *testing.T) {
	srv := newTestBarrierServer(&mockBarrierService{})
	_, err := srv.BarrierRootToken(context.Background(), nil)
	assertGRPCCode(t, err, codes.InvalidArgument)
}

func TestBarrierRootToken_EmptySharesRejected(t *testing.T) {
	srv := newTestBarrierServer(&mockBarrierService{})
	_, err := srv.BarrierRootToken(context.Background(), &pb.BarrierRootTokenRequest{Shares: nil})
	assertGRPCCode(t, err, codes.InvalidArgument)
}

// ---------------------------------------------------------------------------
// formatTime helper
// ---------------------------------------------------------------------------

func TestFormatTime_ZeroReturnsEmptyString(t *testing.T) {
	if got := formatTime(time.Time{}); got != "" {
		t.Fatalf("expected empty string, got %q", got)
	}
}

func TestFormatTime_NonZeroFormatsRFC3339UTC(t *testing.T) {
	in := time.Date(2025, 12, 31, 23, 59, 59, 0, time.UTC)
	got := formatTime(in)
	if got != "2025-12-31T23:59:59Z" {
		t.Fatalf("unexpected format: %s", got)
	}
}

// ---------------------------------------------------------------------------
// Unavailable propagation across every handler
//
// This pins the contract that every RPC short-circuits with
// codes.Unavailable when the facade is unwired, matching the REST 503
// contract.
// ---------------------------------------------------------------------------

func TestBarrier_AllHandlersReturnUnavailableWhenUnwired(t *testing.T) {
	srv := newTestBarrierServer(nil)
	ctx := context.Background()
	checks := []struct {
		name string
		call func() error
	}{
		{"BarrierInitialize", func() error {
			_, err := srv.BarrierInitialize(ctx, &pb.BarrierInitializeRequest{Secret: []byte("x")})
			return err
		}},
		{"BarrierUnseal", func() error {
			_, err := srv.BarrierUnseal(ctx, &pb.BarrierUnsealRequest{Secret: []byte("x")})
			return err
		}},
		{"BarrierSeal", func() error {
			_, err := srv.BarrierSeal(ctx, &pb.BarrierSealRequest{})
			return err
		}},
		{"BarrierStatus", func() error {
			_, err := srv.BarrierStatus(ctx, &emptypb.Empty{})
			return err
		}},
		{"BarrierShamirInitialize", func() error {
			_, err := srv.BarrierShamirInitialize(ctx, &pb.BarrierShamirInitializeRequest{Secret: []byte("x"), Threshold: 2, TotalShares: 3})
			return err
		}},
		{"BarrierShamirUnsealShare", func() error {
			_, err := srv.BarrierShamirUnsealShare(ctx, &pb.BarrierShamirUnsealShareRequest{Share: "abc"})
			return err
		}},
		{"BarrierShamirUnsealShares", func() error {
			_, err := srv.BarrierShamirUnsealShares(ctx, &pb.BarrierShamirUnsealSharesRequest{Shares: []string{"a"}})
			return err
		}},
		{"BarrierRekey", func() error {
			_, err := srv.BarrierRekey(ctx, &pb.BarrierRekeyRequest{Threshold: 2, Total: 3})
			return err
		}},
		{"BarrierRecoveryGenerate", func() error {
			_, err := srv.BarrierRecoveryGenerate(ctx, &pb.BarrierRecoveryGenerateRequest{Threshold: 2, Total: 3})
			return err
		}},
		{"BarrierRecoveryRecover", func() error {
			_, err := srv.BarrierRecoveryRecover(ctx, &pb.BarrierRecoveryRecoverRequest{Keys: []string{"k"}})
			return err
		}},
		{"BarrierRecoveryDelete", func() error {
			_, err := srv.BarrierRecoveryDelete(ctx, &emptypb.Empty{})
			return err
		}},
		{"BarrierRecoveryStatus", func() error {
			_, err := srv.BarrierRecoveryStatus(ctx, &emptypb.Empty{})
			return err
		}},
		{"BarrierRootToken", func() error {
			_, err := srv.BarrierRootToken(ctx, &pb.BarrierRootTokenRequest{Shares: []string{"s"}})
			return err
		}},
	}
	for _, c := range checks {
		t.Run(c.name, func(t *testing.T) {
			assertGRPCCode(t, c.call(), codes.Unavailable)
		})
	}
}

// TestBarrier_MissingErrorFromFacadeMapsToUnavailable confirms that a
// facade returning BarrierServiceMissingError (e.g. late wiring race) is
// surfaced as codes.Unavailable rather than a generic Internal status.
func TestBarrier_MissingErrorFromFacadeMapsToUnavailable(t *testing.T) {
	mock := &mockBarrierService{
		statusFn: func(_ context.Context) (*transport.BarrierStatusResponse, error) {
			return nil, &service.BarrierServiceMissingError{}
		},
	}
	srv := newTestBarrierServer(mock)
	_, err := srv.BarrierStatus(context.Background(), &emptypb.Empty{})
	assertGRPCCode(t, err, codes.Unavailable)
}

// TestBarrierInitialize_WipesConvertedSecret pins the borrow contract at the
// gRPC edge. The handler converts the protobuf string into a slice precisely
// so the plaintext copy can be destroyed; a double that RETAINS the header
// must therefore read back as zeros once the call returns. This goes red the
// moment the wipe is dropped.
func TestBarrierInitialize_WipesConvertedSecret(t *testing.T) {
	var retained []byte
	mock := &mockBarrierService{
		initFn: func(_ context.Context, secret []byte) error {
			retained = secret
			if string(retained) != "topsecret" {
				t.Fatalf("service saw %q, want the plaintext", retained)
			}
			return nil
		},
	}
	srv := newTestBarrierServer(mock)
	if _, err := srv.BarrierInitialize(context.Background(), &pb.BarrierInitializeRequest{Secret: []byte("topsecret")}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(retained) == 0 {
		t.Fatal("the double retained nothing; the test cannot prove the wipe")
	}
	for i, b := range retained {
		if b != 0 {
			t.Fatalf("byte %d is %#x, want 0: the handler did not wipe the secret", i, b)
		}
	}
}

// nonUTF8Credential is a credential a proto3 `string` field could not carry.
var nonUTF8Credential = []byte{0x00, 0xff, 0xfe, 0x80, 0x7f}

// overTheWire marshals msg and decodes the frame into a fresh message of the
// same type, as a gRPC client and server would.
func overTheWire[M gproto.Message](t *testing.T, msg M) M {
	t.Helper()
	frame, err := gproto.Marshal(msg)
	if err != nil {
		t.Fatalf("marshal %T: %v", msg, err)
	}
	decoded := msg.ProtoReflect().New().Interface().(M)
	if err := gproto.Unmarshal(frame, decoded); err != nil {
		t.Fatalf("unmarshal %T: %v", msg, err)
	}
	return decoded
}

func TestBarrierCredentialRPCs_NonUTF8SecretReachesServiceIntact(t *testing.T) {
	var seen [][]byte
	record := func(secret []byte) { seen = append(seen, bytes.Clone(secret)) }
	mock := &mockBarrierService{
		initFn: func(_ context.Context, secret []byte) error {
			record(secret)
			return nil
		},
		unsealFn: func(_ context.Context, secret []byte) error {
			record(secret)
			return nil
		},
		initShamirFn: func(_ context.Context, secret []byte, threshold, total int) (*transport.BarrierInitializeShamirResponse, error) {
			record(secret)
			return &transport.BarrierInitializeShamirResponse{Threshold: threshold, TotalShares: total}, nil
		},
	}
	srv := newTestBarrierServer(mock)
	ctx := context.Background()

	if _, err := srv.BarrierInitialize(ctx, overTheWire(t, &pb.BarrierInitializeRequest{Secret: bytes.Clone(nonUTF8Credential)})); err != nil {
		t.Fatalf("BarrierInitialize: %v", err)
	}
	if _, err := srv.BarrierUnseal(ctx, overTheWire(t, &pb.BarrierUnsealRequest{Secret: bytes.Clone(nonUTF8Credential)})); err != nil {
		t.Fatalf("BarrierUnseal: %v", err)
	}
	shamirReq := &pb.BarrierShamirInitializeRequest{Secret: bytes.Clone(nonUTF8Credential), Threshold: 2, TotalShares: 3}
	if _, err := srv.BarrierShamirInitialize(ctx, overTheWire(t, shamirReq)); err != nil {
		t.Fatalf("BarrierShamirInitialize: %v", err)
	}

	if len(seen) != 3 {
		t.Fatalf("service saw %d credentials, want 3", len(seen))
	}
	for i, got := range seen {
		if !bytes.Equal(got, nonUTF8Credential) {
			t.Fatalf("call %d: service saw %x, want %x", i, got, nonUTF8Credential)
		}
	}
}

func TestBarrierEnableEncryption_NonUTF8PassphraseReachesServiceIntact(t *testing.T) {
	var matched bool
	mock := &mockBarrierService{
		enableEncryptionFn: func(_ context.Context, _ string, opts qrdbsdk.MigrationOptions) error {
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
	srv := newTestBarrierServer(mock)
	req := &pb.BarrierEnableEncryptionRequest{TargetStrategy: "passphrase", Passphrase: bytes.Clone(nonUTF8Credential)}

	if _, err := srv.BarrierEnableEncryption(context.Background(), overTheWire(t, req)); err != nil {
		t.Fatalf("BarrierEnableEncryption: %v", err)
	}
	if !matched {
		t.Fatal("the service did not receive the passphrase bytes the client sent")
	}
}
