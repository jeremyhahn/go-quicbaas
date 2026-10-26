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
	"context"
	stderrors "errors"
	"reflect"
	"testing"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/emptypb"

	qrdbtransport "github.com/jeremyhahn/go-qrdb/sdk/go/transport"

	pb "github.com/jeremyhahn/go-quicbaas/api/proto"
	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
)

// ---------------------------------------------------------------------------
// Mock CeremonyServicer
// ---------------------------------------------------------------------------

// mockCeremonyService implements service.CeremonyServicer for unit tests.
// Each method dispatches to an injected closure when set, otherwise it
// returns zero values.
type mockCeremonyService struct {
	initFn    func(ctx context.Context, req *service.CeremonyInitRequest) (*service.CeremonyInitInfo, error)
	statusFn  func(ctx context.Context) (*service.CeremonyStatusInfo, error)
	signCSRFn func(ctx context.Context, username, soPin, csrPEM, role string) (*service.CeremonySignCSRResult, error)
}

var _ service.CeremonyServicer = (*mockCeremonyService)(nil)

func (m *mockCeremonyService) CeremonyInitialize(ctx context.Context, req *service.CeremonyInitRequest) (*service.CeremonyInitInfo, error) {
	if m.initFn != nil {
		return m.initFn(ctx, req)
	}
	return &service.CeremonyInitInfo{}, nil
}

func (m *mockCeremonyService) CeremonyStatus(ctx context.Context) (*service.CeremonyStatusInfo, error) {
	if m.statusFn != nil {
		return m.statusFn(ctx)
	}
	return &service.CeremonyStatusInfo{}, nil
}

func (m *mockCeremonyService) CeremonySignCSR(ctx context.Context, username, soPin, csrPEM, role string) (*service.CeremonySignCSRResult, error) {
	if m.signCSRFn != nil {
		return m.signCSRFn(ctx, username, soPin, csrPEM, role)
	}
	return &service.CeremonySignCSRResult{}, nil
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// newTestCeremonyServer constructs a CeremonyServer over the supplied
// mock. Passing nil yields an unwired server used to exercise the
// requireCeremonyService short-circuit.
func newTestCeremonyServer(svc service.CeremonyServicer) *CeremonyServer {
	log, _ := logger.New(logger.LogConfig{Level: "error"})
	return NewCeremonyServer(svc, log)
}

// ---------------------------------------------------------------------------
// Construction
// ---------------------------------------------------------------------------

func TestNewCeremonyServer_NilLoggerAllowed(t *testing.T) {
	srv := NewCeremonyServer(&mockCeremonyService{}, nil)
	if srv == nil {
		t.Fatal("expected non-nil server")
	}
	if srv.logger != nil {
		t.Fatal("expected nil logger when none supplied")
	}
}

func TestNewCeremonyServer_WithLogger(t *testing.T) {
	log, _ := logger.New(logger.LogConfig{Level: "error"})
	srv := NewCeremonyServer(&mockCeremonyService{}, log)
	if srv.logger == nil {
		t.Fatal("expected logger to be set")
	}
}

// ---------------------------------------------------------------------------
// requireCeremonyService / mapCeremonyError
// ---------------------------------------------------------------------------

func TestRequireCeremonyService_NilSvcReturnsUnavailable(t *testing.T) {
	srv := newTestCeremonyServer(nil)
	_, err := srv.CeremonyStatus(context.Background(), &emptypb.Empty{})
	assertGRPCCode(t, err, codes.Unavailable)
}

func TestMapCeremonyError_NilReturnsNil(t *testing.T) {
	if got := mapCeremonyError(nil); got != nil {
		t.Fatalf("expected nil, got %v", got)
	}
}

func TestMapCeremonyError_MissingErrorMapsToUnavailable(t *testing.T) {
	err := mapCeremonyError(&service.CeremonyServiceMissingError{})
	assertGRPCCode(t, err, codes.Unavailable)
}

// ---------------------------------------------------------------------------
// CeremonyInitialize
// ---------------------------------------------------------------------------

func TestCeremonyInitialize_Success(t *testing.T) {
	var seen *service.CeremonyInitRequest
	mock := &mockCeremonyService{
		initFn: func(_ context.Context, req *service.CeremonyInitRequest) (*service.CeremonyInitInfo, error) {
			seen = req
			return &service.CeremonyInitInfo{
				SPKIPin:    "pin",
				CACertPEM:  []byte("ca"),
				TLSCertPEM: []byte("tls"),
				State:      "initialized",
				ShareCount: 3,
			}, nil
		},
	}
	srv := newTestCeremonyServer(mock)
	resp, err := srv.CeremonyInitialize(context.Background(), &pb.CeremonyInitializeRequest{
		SoPin:                  "sopin",
		UserPin:                "userpin",
		CredentialSealStrategy: "auto",
		Threshold:              2,
		Officers: []*pb.CeremonyOfficer{
			{Username: "alice", CsrPem: "csr-a"},
			{Username: "bob", CsrPem: "csr-b"},
		},
		Hostname: "host",
		Sans:     []string{"san1"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if seen == nil {
		t.Fatal("expected initFn invocation")
	}
	if seen.SOPin != "sopin" || seen.UserPin != "userpin" || seen.CredentialSealStrategy != "auto" {
		t.Fatalf("unexpected request: %#v", seen)
	}
	if seen.Threshold != 2 || len(seen.Officers) != 2 || seen.Officers[1].Username != "bob" {
		t.Fatalf("unexpected officers: %#v", seen.Officers)
	}
	if seen.Hostname != "host" || len(seen.SANs) != 1 {
		t.Fatalf("unexpected SANs: %#v", seen)
	}
	if resp.GetSpkiPin() != "pin" || resp.GetCaCertPem() != "ca" || resp.GetTlsCertPem() != "tls" {
		t.Fatalf("unexpected response: %#v", resp)
	}
	if resp.GetState() != "initialized" || resp.GetShareCount() != 3 {
		t.Fatalf("unexpected state/count: %#v", resp)
	}
}

func TestCeremonyInitialize_NilRequest(t *testing.T) {
	srv := newTestCeremonyServer(&mockCeremonyService{})
	_, err := srv.CeremonyInitialize(context.Background(), nil)
	assertGRPCCode(t, err, codes.InvalidArgument)
}

func TestCeremonyInitialize_FacadeError(t *testing.T) {
	mock := &mockCeremonyService{
		initFn: func(_ context.Context, _ *service.CeremonyInitRequest) (*service.CeremonyInitInfo, error) {
			return nil, stderrors.New("boom")
		},
	}
	srv := newTestCeremonyServer(mock)
	_, err := srv.CeremonyInitialize(context.Background(), &pb.CeremonyInitializeRequest{})
	if err == nil {
		t.Fatal("expected error")
	}
}

// TestCeremonyInitialize_DeliversIssuedCredentials pins Codex 6800 P1: the
// generated admin key, the admin certificate and the holder certificates
// reach the gRPC caller.
func TestCeremonyInitialize_DeliversIssuedCredentials(t *testing.T) {
	mock := &mockCeremonyService{
		initFn: func(_ context.Context, _ *service.CeremonyInitRequest) (*service.CeremonyInitInfo, error) {
			return &service.CeremonyInitInfo{
				State:                 "initialized",
				Holders:               []qrdbtransport.CeremonyHolderCert{{Username: "carol", Role: "custodian", CertPEM: "carol-cert"}},
				AdminCertPEM:          []byte("admin-cert"),
				AdminKeyPEM:           []byte("admin-key"),
				AdminCredentialSource: "generated",
			}, nil
		},
	}
	resp, err := newTestCeremonyServer(mock).CeremonyInitialize(context.Background(), &pb.CeremonyInitializeRequest{AdminGenerate: true})
	if err != nil {
		t.Fatalf("CeremonyInitialize: %v", err)
	}
	if string(resp.GetAdminKeyPem()) != "admin-key" || resp.GetAdminCertPem() != "admin-cert" || resp.GetAdminCredentialSource() != "generated" {
		t.Fatalf("admin credential not delivered: %v", resp)
	}
	holders := resp.GetHolders()
	if len(holders) != 1 || holders[0].GetUsername() != "carol" || holders[0].GetRole() != "custodian" || holders[0].GetCertPem() != "carol-cert" {
		t.Fatalf("holder certificates not delivered: %v", holders)
	}
}

// TestCeremonyInitialize_ForwardsEveryOfficerField pins that every officer
// field and the policy reach the service unchanged.
func TestCeremonyInitialize_ForwardsEveryOfficerField(t *testing.T) {
	var got *service.CeremonyInitRequest
	mock := &mockCeremonyService{
		initFn: func(_ context.Context, req *service.CeremonyInitRequest) (*service.CeremonyInitInfo, error) {
			got = req
			return &service.CeremonyInitInfo{}, nil
		},
	}
	_, err := newTestCeremonyServer(mock).CeremonyInitialize(context.Background(), &pb.CeremonyInitializeRequest{
		Threshold:     2,
		Policy:        "any-enrolled",
		AdminGenerate: true,
		Officers: []*pb.CeremonyOfficer{{
			Username:      "carol",
			Role:          "custodian",
			CsrPem:        "csr",
			EncryptionKem: "dhkem-x25519",
			EncryptionKey: []byte{1, 2, 3},
			Factors:       []*pb.CeremonyHolderFactor{{Name: "totp", ParamsJson: []byte(`{"digits":6}`)}},
			Designated:    "totp",
		}},
	})
	if err != nil {
		t.Fatalf("CeremonyInitialize: %v", err)
	}
	want := service.CeremonyOfficer{
		Username:      "carol",
		Role:          "custodian",
		CSRPEM:        "csr",
		EncryptionKEM: "dhkem-x25519",
		EncryptionKey: []byte{1, 2, 3},
		Factors:       []qrdbtransport.HolderFactorEnrolment{{Name: "totp", Params: map[string]any{"digits": float64(6)}}},
		Designated:    "totp",
	}
	if got.Policy != "any-enrolled" || len(got.Officers) != 1 || !reflect.DeepEqual(got.Officers[0], want) {
		t.Fatalf("officer request narrowed: policy=%q officers=%+v", got.Policy, got.Officers)
	}
}

// TestCeremonyInitialize_RefusesNonObjectFactorParams pins that a factor whose
// params_json is not a JSON object is InvalidArgument and never reaches the
// service.
func TestCeremonyInitialize_RefusesNonObjectFactorParams(t *testing.T) {
	called := false
	mock := &mockCeremonyService{
		initFn: func(_ context.Context, _ *service.CeremonyInitRequest) (*service.CeremonyInitInfo, error) {
			called = true
			return &service.CeremonyInitInfo{}, nil
		},
	}
	_, err := newTestCeremonyServer(mock).CeremonyInitialize(context.Background(), &pb.CeremonyInitializeRequest{
		AdminGenerate: true,
		Officers: []*pb.CeremonyOfficer{{
			Username: "carol",
			Factors:  []*pb.CeremonyHolderFactor{{Name: "totp", ParamsJson: []byte(`[1]`)}},
		}},
	})
	if status.Code(err) != codes.InvalidArgument {
		t.Fatalf("expected InvalidArgument, got %v", err)
	}
	if called {
		t.Fatal("service called with an undecodable factor")
	}
}

// TestCeremonyInitialize_NonObjectFactorParamsWipesSecret proves the ceremony
// secret is wiped even when the roster conversion refuses the request.
func TestCeremonyInitialize_NonObjectFactorParamsWipesSecret(t *testing.T) {
	secret := []byte("hunter2")
	_, err := newTestCeremonyServer(&mockCeremonyService{}).CeremonyInitialize(context.Background(), &pb.CeremonyInitializeRequest{
		AdminGenerate: true,
		Secret:        secret,
		Officers: []*pb.CeremonyOfficer{{
			Username: "carol",
			Factors:  []*pb.CeremonyHolderFactor{{Name: "totp", ParamsJson: []byte(`[1]`)}},
		}},
	})
	if status.Code(err) != codes.InvalidArgument {
		t.Fatalf("expected InvalidArgument, got %v", err)
	}
	for i, b := range secret {
		if b != 0 {
			t.Fatalf("secret byte %d not wiped after a refused roster", i)
		}
	}
}

// ---------------------------------------------------------------------------
// CeremonyStatus
// ---------------------------------------------------------------------------

func TestCeremonyStatus_Success(t *testing.T) {
	mock := &mockCeremonyService{
		statusFn: func(_ context.Context) (*service.CeremonyStatusInfo, error) {
			return &service.CeremonyStatusInfo{State: "armed", SPKIPin: "spki"}, nil
		},
	}
	srv := newTestCeremonyServer(mock)
	resp, err := srv.CeremonyStatus(context.Background(), &emptypb.Empty{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.GetState() != "armed" || resp.GetSpkiPin() != "spki" {
		t.Fatalf("unexpected response: %#v", resp)
	}
}

func TestCeremonyStatus_FacadeError(t *testing.T) {
	mock := &mockCeremonyService{
		statusFn: func(_ context.Context) (*service.CeremonyStatusInfo, error) { return nil, stderrors.New("x") },
	}
	srv := newTestCeremonyServer(mock)
	_, err := srv.CeremonyStatus(context.Background(), &emptypb.Empty{})
	if err == nil {
		t.Fatal("expected error")
	}
}

// ---------------------------------------------------------------------------
// CeremonySignCSR
// ---------------------------------------------------------------------------

func TestCeremonySignCSR_Success(t *testing.T) {
	mock := &mockCeremonyService{
		signCSRFn: func(_ context.Context, username, soPin, csrPEM, role string) (*service.CeremonySignCSRResult, error) {
			if username != "u" || csrPEM != "CSR" || soPin != "SO" || role != "officer" {
				t.Fatalf("unexpected params: %s %s %s %s", username, soPin, csrPEM, role)
			}
			return &service.CeremonySignCSRResult{CertPEM: []byte("ISSUED")}, nil
		},
	}
	srv := newTestCeremonyServer(mock)
	resp, err := srv.CeremonySignCSR(context.Background(), &pb.CeremonySignCSRRequest{
		Username: "u",
		SoPin:    "SO",
		CsrPem:   "CSR",
		Role:     "officer",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if string(resp.GetCertPem()) != "ISSUED" {
		t.Fatalf("unexpected cert_pem: %q", resp.GetCertPem())
	}
}

func TestCeremonySignCSR_Validation(t *testing.T) {
	srv := newTestCeremonyServer(&mockCeremonyService{})
	cases := []struct {
		name string
		req  *pb.CeremonySignCSRRequest
	}{
		{"nil request", nil},
		{"missing username", &pb.CeremonySignCSRRequest{CsrPem: "csr"}},
		{"missing csr", &pb.CeremonySignCSRRequest{Username: "u"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := srv.CeremonySignCSR(context.Background(), c.req)
			assertGRPCCode(t, err, codes.InvalidArgument)
		})
	}
}

// ---------------------------------------------------------------------------
// Unavailable propagation across every handler
// ---------------------------------------------------------------------------

func TestCeremony_AllHandlersReturnUnavailableWhenUnwired(t *testing.T) {
	srv := newTestCeremonyServer(nil)
	ctx := context.Background()
	checks := []struct {
		name string
		call func() error
	}{
		{"CeremonyInitialize", func() error {
			_, err := srv.CeremonyInitialize(ctx, &pb.CeremonyInitializeRequest{})
			return err
		}},
		{"CeremonyStatus", func() error {
			_, err := srv.CeremonyStatus(ctx, &emptypb.Empty{})
			return err
		}},
		{"CeremonySignCSR", func() error {
			_, err := srv.CeremonySignCSR(ctx, &pb.CeremonySignCSRRequest{Username: "u", CsrPem: "csr"})
			return err
		}},
	}
	for _, c := range checks {
		t.Run(c.name, func(t *testing.T) {
			assertGRPCCode(t, c.call(), codes.Unavailable)
		})
	}
}
