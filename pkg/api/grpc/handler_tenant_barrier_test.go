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
	"math"
	"reflect"
	"testing"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"
	qrdbtransport "github.com/jeremyhahn/go-qrdb/sdk/go/transport"

	pb "github.com/jeremyhahn/go-quicbaas/api/proto"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
)

// errTenantBarrierGRPCBoom drives the generic (non-sentinel) error branch.
var errTenantBarrierGRPCBoom = stderrors.New("tenant barrier grpc boom")

// fakeTenantBarrierServicer is a hand-written test double for
// service.TenantBarrierServicer. Methods delegate to optional function fields
// so a test can shape each path. The officer and quorum verbs the gRPC server
// does not serve come from the embedded interface and are never called here.
type fakeTenantBarrierServicer struct {
	service.TenantBarrierServicer

	initFn func(ctx context.Context, tenantID string, passphrase []byte, threshold, shares int, strategy string) (*qrdbtransport.TenantBarrierInitResponse, error)
	// initReq records the request the last TenantBarrierInit call received.
	initReq  *qrdbtransport.TenantBarrierInitRequest
	unsealFn func(ctx context.Context, tenantID string, key []byte) error
	sealFn   func(ctx context.Context, tenantID string) error
	statusFn func(ctx context.Context, tenantID string) (*qrdbtransport.TenantBarrierStatusResponse, error)
}

func (f *fakeTenantBarrierServicer) TenantBarrierInit(ctx context.Context, req *qrdbtransport.TenantBarrierInitRequest) (*qrdbtransport.TenantBarrierInitResponse, error) {
	f.initReq = req
	if f.initFn != nil {
		return f.initFn(ctx, req.TenantID, req.Passphrase, req.Threshold, req.Shares, req.Strategy)
	}
	return &qrdbtransport.TenantBarrierInitResponse{TenantID: req.TenantID}, nil
}

func (f *fakeTenantBarrierServicer) TenantBarrierUnseal(ctx context.Context, tenantID string, key []byte) error {
	if f.unsealFn != nil {
		return f.unsealFn(ctx, tenantID, key)
	}
	return nil
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
		sealFn:   func(context.Context, string) error { return sentinel },
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

// TestTenantBarrierRPCs_ResponseOutOfRange proves that a share count too
// large for its int32 wire field becomes codes.Internal instead of a wrapped
// integer.
func TestTenantBarrierRPCs_ResponseOutOfRange(t *testing.T) {
	const tooBig = math.MaxInt32 + 1
	ctx := context.Background()
	initResult := func(threshold, total int) *fakeTenantBarrierServicer {
		return &fakeTenantBarrierServicer{
			initFn: func(context.Context, string, []byte, int, int, string) (*qrdbtransport.TenantBarrierInitResponse, error) {
				return &qrdbtransport.TenantBarrierInitResponse{TenantID: "t1", Threshold: threshold, TotalShares: total}, nil
			},
		}
	}
	statusInfo := func(threshold, total int) *fakeTenantBarrierServicer {
		return &fakeTenantBarrierServicer{
			statusFn: func(context.Context, string) (*qrdbtransport.TenantBarrierStatusResponse, error) {
				return &qrdbtransport.TenantBarrierStatusResponse{TenantID: "t1", ShamirThreshold: threshold, ShamirTotal: total}, nil
			},
		}
	}
	initCall := func(srv *TenantBarrierGRPCServer) error {
		_, err := srv.Init(ctx, &pb.TenantBarrierInitRequest{TenantId: "t1", Strategy: "shamir"})
		return err
	}
	statusCall := func(srv *TenantBarrierGRPCServer) error {
		_, err := srv.Status(ctx, &pb.TenantBarrierStatusRequest{TenantId: "t1"})
		return err
	}

	cases := []struct {
		name  string
		field string
		svc   *fakeTenantBarrierServicer
		call  func(srv *TenantBarrierGRPCServer) error
	}{
		{name: "init threshold", field: "threshold", svc: initResult(tooBig, 5), call: initCall},
		{name: "init total shares", field: "total_shares", svc: initResult(3, tooBig), call: initCall},
		{name: "status shamir threshold", field: "shamir_threshold", svc: statusInfo(tooBig, 5), call: statusCall},
		{name: "status shamir total", field: "shamir_total", svc: statusInfo(3, tooBig), call: statusCall},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			srv := NewTenantBarrierGRPCServer(c.svc, nil)
			requireRangeStatus(t, c.call(srv), codes.Internal, c.field)
		})
	}
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

// TestTenantBarrierRPCs_InitForwardsOfficersAndPolicy proves the inline
// Shamir roster and policy cross the gRPC wire into go-qrdb's request type
// and that the issued holder certificates come back on the response.
func TestTenantBarrierRPCs_InitForwardsOfficersAndPolicy(t *testing.T) {
	svc := &fakeTenantBarrierServicer{
		initFn: func(context.Context, string, []byte, int, int, string) (*qrdbtransport.TenantBarrierInitResponse, error) {
			return &qrdbtransport.TenantBarrierInitResponse{
				Holders: []qrdbtransport.TenantBarrierHolderCert{
					{Username: "alice", Role: "officer", CertPEM: "ALICE-CERT"},
				},
			}, nil
		},
	}
	srv := NewTenantBarrierGRPCServer(svc, nil)

	req := &pb.TenantBarrierInitRequest{
		TenantId:  "t1",
		Strategy:  "shamir",
		Threshold: 2,
		Shares:    3,
		Policy:    "designated",
		Officers: []*pb.TenantBarrierOfficer{
			{
				Username:      "alice",
				Role:          "officer",
				CsrPem:        "ALICE-CSR",
				EncryptionKem: "ML-KEM-768",
				EncryptionKey: []byte("ALICE-KEY"),
				Designated:    "piv",
				Factors: []*pb.TenantBarrierHolderFactor{
					{Name: "piv", ParamsJson: []byte(`{"slot":"9a"}`)},
					{Name: "passphrase"},
				},
			},
		},
	}
	resp, err := srv.Init(context.Background(), overTheWire(t, req))
	if err != nil {
		t.Fatalf("Init: %v", err)
	}

	want := []qrdbtransport.OfficerRegisterRequest{{
		Username:      "alice",
		Role:          "officer",
		CSRPEM:        "ALICE-CSR",
		EncryptionKEM: "ML-KEM-768",
		EncryptionKey: []byte("ALICE-KEY"),
		Designated:    "piv",
		Factors: []qrdbtransport.HolderFactorEnrolment{
			{Name: "piv", Params: map[string]any{"slot": "9a"}},
			{Name: "passphrase"},
		},
	}}
	if svc.initReq == nil {
		t.Fatal("service was not called")
	}
	if !reflect.DeepEqual(svc.initReq.Officers, want) {
		t.Fatalf("officers = %+v, want %+v", svc.initReq.Officers, want)
	}
	if svc.initReq.Policy != "designated" {
		t.Fatalf("policy = %q, want designated", svc.initReq.Policy)
	}
	if len(resp.GetHolders()) != 1 || resp.GetHolders()[0].GetUsername() != "alice" ||
		resp.GetHolders()[0].GetRole() != "officer" || resp.GetHolders()[0].GetCertPem() != "ALICE-CERT" {
		t.Fatalf("holders = %+v, want alice/officer/ALICE-CERT", resp.GetHolders())
	}
}

// TestTenantBarrierRPCs_InitMalformedFactorParamsRefused proves a factor whose
// params_json is not a JSON object is refused before go-qrdb is called.
func TestTenantBarrierRPCs_InitMalformedFactorParamsRefused(t *testing.T) {
	svc := &fakeTenantBarrierServicer{}
	srv := NewTenantBarrierGRPCServer(svc, nil)

	req := &pb.TenantBarrierInitRequest{
		TenantId: "t1",
		Strategy: "shamir",
		Officers: []*pb.TenantBarrierOfficer{{
			Username: "alice",
			Factors:  []*pb.TenantBarrierHolderFactor{{Name: "piv", ParamsJson: []byte(`["not","an","object"]`)}},
		}},
	}
	_, err := srv.Init(context.Background(), req)
	requireGRPCCode(t, err, codes.InvalidArgument)
	if svc.initReq != nil {
		t.Fatal("service was called with a malformed roster")
	}
}

// TestTenantBarrierRPCs_InitInvalidRosterErrorUnchanged proves go-qrdb's typed
// roster error reaches the caller through the standard error mapping, not a
// handler-specific rewrite.
func TestTenantBarrierRPCs_InitInvalidRosterErrorUnchanged(t *testing.T) {
	rosterErr := &qrdbtransport.FactorTypeUnsupportedError{Type: "bogus"}
	svc := &fakeTenantBarrierServicer{
		initFn: func(context.Context, string, []byte, int, int, string) (*qrdbtransport.TenantBarrierInitResponse, error) {
			return nil, rosterErr
		},
	}
	srv := NewTenantBarrierGRPCServer(svc, nil)

	req := &pb.TenantBarrierInitRequest{
		TenantId: "t1",
		Strategy: "shamir",
		Officers: []*pb.TenantBarrierOfficer{{Username: "alice", Factors: []*pb.TenantBarrierHolderFactor{{Name: "bogus"}}}},
	}
	_, err := srv.Init(context.Background(), req)
	want := ToGRPCError(rosterErr)
	if status.Code(err) != status.Code(want) || status.Convert(err).Message() != status.Convert(want).Message() {
		t.Fatalf("err = %v, want %v", err, want)
	}
}

// TestTenantBarrierRPCs_InitStatementUnsupportedIsUnimplemented confirms the
// raft-isolated init refusal (Codex 6757 finding 1) answers Unimplemented
// with its text: the gRPC request carries no go-qrdb Signer either.
func TestTenantBarrierRPCs_InitStatementUnsupportedIsUnimplemented(t *testing.T) {
	refusal := &service.TenantBarrierInitStatementUnsupportedError{TenantID: "t1"}
	svc := &fakeTenantBarrierServicer{
		initFn: func(context.Context, string, []byte, int, int, string) (*qrdbtransport.TenantBarrierInitResponse, error) {
			return nil, refusal
		},
	}
	srv := NewTenantBarrierGRPCServer(svc, nil)

	_, err := srv.Init(context.Background(), &pb.TenantBarrierInitRequest{TenantId: "t1", Strategy: "none"})
	if status.Code(err) != codes.Unimplemented || status.Convert(err).Message() != refusal.Error() {
		t.Fatalf("err = %v, want Unimplemented %q", err, refusal.Error())
	}
}

func TestTenantBarrierEnableEncryption_NonUTF8PassphraseReachesServiceIntact(t *testing.T) {
	var matched bool
	barrier := &mockBarrierService{
		enableEncryptionFn: func(_ context.Context, tenantID string, opts qrdbsdk.MigrationOptions) error {
			if tenantID != "t1" {
				t.Errorf("tenant = %q, want t1", tenantID)
			}
			plain, err := opts.Credentials.Secret.Open(context.Background())
			if err != nil {
				t.Errorf("open the forwarded credential: %v", err)
				return nil
			}
			defer plain.Destroy()
			equal, err := plain.EqualTo(nonUTF8Credential)
			if err != nil {
				t.Errorf("compare the forwarded credential: %v", err)
				return nil
			}
			matched = equal
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
