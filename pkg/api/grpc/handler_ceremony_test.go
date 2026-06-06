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
	"testing"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/protobuf/types/known/emptypb"

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
	initFn           func(ctx context.Context, req *service.CeremonyInitRequest) (*service.CeremonyInitInfo, error)
	statusFn         func(ctx context.Context) (*service.CeremonyStatusInfo, error)
	beginFn          func(ctx context.Context, username string) (*service.CeremonyChallenge, error)
	completeFn       func(ctx context.Context, username, nonceHex, sigB64 string) (*service.CeremonyClaimCertResult, error)
	claimShareFn     func(ctx context.Context, username string) (string, error)
	signCSRFn        func(ctx context.Context, username, soPin, csrPEM, role string) (*service.CeremonySignCSRResult, error)
	enrollExternalFn func(ctx context.Context, req *service.CeremonyEnrollExternalOfficerRequest) (*service.CeremonyEnrollExternalOfficerResponse, error)
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

func (m *mockCeremonyService) CeremonyBeginClaimCert(ctx context.Context, username string) (*service.CeremonyChallenge, error) {
	if m.beginFn != nil {
		return m.beginFn(ctx, username)
	}
	return &service.CeremonyChallenge{}, nil
}

func (m *mockCeremonyService) CeremonyCompleteClaimCert(ctx context.Context, username, nonceHex, sigB64 string) (*service.CeremonyClaimCertResult, error) {
	if m.completeFn != nil {
		return m.completeFn(ctx, username, nonceHex, sigB64)
	}
	return &service.CeremonyClaimCertResult{}, nil
}

func (m *mockCeremonyService) CeremonyClaimShare(ctx context.Context, username string) (string, error) {
	if m.claimShareFn != nil {
		return m.claimShareFn(ctx, username)
	}
	return "", nil
}

func (m *mockCeremonyService) CeremonySignCSR(ctx context.Context, username, soPin, csrPEM, role string) (*service.CeremonySignCSRResult, error) {
	if m.signCSRFn != nil {
		return m.signCSRFn(ctx, username, soPin, csrPEM, role)
	}
	return &service.CeremonySignCSRResult{}, nil
}

func (m *mockCeremonyService) CeremonyEnrollExternalOfficer(ctx context.Context, req *service.CeremonyEnrollExternalOfficerRequest) (*service.CeremonyEnrollExternalOfficerResponse, error) {
	if m.enrollExternalFn != nil {
		return m.enrollExternalFn(ctx, req)
	}
	return &service.CeremonyEnrollExternalOfficerResponse{}, nil
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
				SOCertPEM:  []byte("so"),
				SOKeyPEM:   []byte("sokey"),
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
// CeremonyClaimCertBegin / Complete
// ---------------------------------------------------------------------------

func TestCeremonyClaimCertBegin_Success(t *testing.T) {
	expires := time.Date(2025, 5, 6, 7, 8, 9, 0, time.UTC)
	mock := &mockCeremonyService{
		beginFn: func(_ context.Context, username string) (*service.CeremonyChallenge, error) {
			if username != "alice" {
				t.Fatalf("expected username 'alice', got %q", username)
			}
			return &service.CeremonyChallenge{NonceHex: "deadbeef", ExpiresAt: expires}, nil
		},
	}
	srv := newTestCeremonyServer(mock)
	resp, err := srv.CeremonyClaimCertBegin(context.Background(), &pb.CeremonyClaimCertBeginRequest{Username: "alice"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.GetNonceHex() != "deadbeef" {
		t.Fatalf("unexpected nonce: %q", resp.GetNonceHex())
	}
	if resp.GetExpiresAt() != expires.UTC().Format(time.RFC3339) {
		t.Fatalf("unexpected expires_at: %s", resp.GetExpiresAt())
	}
}

func TestCeremonyClaimCertBegin_NilRequest(t *testing.T) {
	srv := newTestCeremonyServer(&mockCeremonyService{})
	_, err := srv.CeremonyClaimCertBegin(context.Background(), nil)
	assertGRPCCode(t, err, codes.InvalidArgument)
}

func TestCeremonyClaimCertBegin_MissingUsername(t *testing.T) {
	srv := newTestCeremonyServer(&mockCeremonyService{})
	_, err := srv.CeremonyClaimCertBegin(context.Background(), &pb.CeremonyClaimCertBeginRequest{Username: ""})
	assertGRPCCode(t, err, codes.InvalidArgument)
}

func TestCeremonyClaimCertComplete_Success(t *testing.T) {
	var sawUser, sawNonce, sawSig string
	mock := &mockCeremonyService{
		completeFn: func(_ context.Context, username, nonceHex, sigB64 string) (*service.CeremonyClaimCertResult, error) {
			sawUser, sawNonce, sawSig = username, nonceHex, sigB64
			return &service.CeremonyClaimCertResult{
				CertPEM:   []byte("CERT"),
				CACertPEM: []byte("CACERT"),
			}, nil
		},
	}
	srv := newTestCeremonyServer(mock)
	resp, err := srv.CeremonyClaimCertComplete(context.Background(), &pb.CeremonyClaimCertCompleteRequest{
		Username:  "u",
		NonceHex:  "abcd",
		Signature: "c2lnLXNpZw==",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if sawUser != "u" || sawNonce != "abcd" || sawSig != "c2lnLXNpZw==" {
		t.Fatalf("unexpected propagation: %s, %s, %s", sawUser, sawNonce, sawSig)
	}
	if string(resp.GetCertPem()) != "CERT" || string(resp.GetCaCertPem()) != "CACERT" {
		t.Fatalf("unexpected response: %#v", resp)
	}
}

func TestCeremonyClaimCertComplete_Validation(t *testing.T) {
	srv := newTestCeremonyServer(&mockCeremonyService{})
	cases := []struct {
		name string
		req  *pb.CeremonyClaimCertCompleteRequest
	}{
		{"nil request", nil},
		{"missing username", &pb.CeremonyClaimCertCompleteRequest{NonceHex: "x", Signature: "s"}},
		{"missing nonce_hex", &pb.CeremonyClaimCertCompleteRequest{Username: "u", Signature: "s"}},
		{"missing signature", &pb.CeremonyClaimCertCompleteRequest{Username: "u", NonceHex: "x"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := srv.CeremonyClaimCertComplete(context.Background(), c.req)
			assertGRPCCode(t, err, codes.InvalidArgument)
		})
	}
}

// ---------------------------------------------------------------------------
// CeremonyClaimShare
// ---------------------------------------------------------------------------

func TestCeremonyClaimShare_Success(t *testing.T) {
	mock := &mockCeremonyService{
		claimShareFn: func(_ context.Context, username string) (string, error) {
			if username != "officer" {
				t.Fatalf("expected officer, got %q", username)
			}
			return "share-b64", nil
		},
	}
	srv := newTestCeremonyServer(mock)
	resp, err := srv.CeremonyClaimShare(context.Background(), &pb.CeremonyClaimShareRequest{Username: "officer"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.GetShare() != "share-b64" {
		t.Fatalf("expected share-b64, got %q", resp.GetShare())
	}
}

func TestCeremonyClaimShare_Validation(t *testing.T) {
	srv := newTestCeremonyServer(&mockCeremonyService{})
	_, err := srv.CeremonyClaimShare(context.Background(), nil)
	assertGRPCCode(t, err, codes.InvalidArgument)
	_, err = srv.CeremonyClaimShare(context.Background(), &pb.CeremonyClaimShareRequest{Username: ""})
	assertGRPCCode(t, err, codes.InvalidArgument)
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
// CeremonyListOfficers
// ---------------------------------------------------------------------------

func TestCeremonyListOfficers_EmptyRosterWhenWired(t *testing.T) {
	srv := newTestCeremonyServer(&mockCeremonyService{})
	resp, err := srv.CeremonyListOfficers(context.Background(), &emptypb.Empty{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(resp.GetOfficers()) != 0 {
		t.Fatalf("expected empty roster, got %d entries", len(resp.GetOfficers()))
	}
}

func TestCeremonyListOfficers_UnavailableWhenUnwired(t *testing.T) {
	srv := newTestCeremonyServer(nil)
	_, err := srv.CeremonyListOfficers(context.Background(), &emptypb.Empty{})
	assertGRPCCode(t, err, codes.Unavailable)
}

// ---------------------------------------------------------------------------
// CeremonyEnrollExternalOfficer
// ---------------------------------------------------------------------------

func TestCeremonyEnrollExternalOfficer_Success(t *testing.T) {
	var seen *service.CeremonyEnrollExternalOfficerRequest
	mock := &mockCeremonyService{
		enrollExternalFn: func(_ context.Context, req *service.CeremonyEnrollExternalOfficerRequest) (*service.CeremonyEnrollExternalOfficerResponse, error) {
			seen = req
			return &service.CeremonyEnrollExternalOfficerResponse{SealedShare: "sealed-b64"}, nil
		},
	}
	srv := newTestCeremonyServer(mock)
	resp, err := srv.CeremonyEnrollExternalOfficer(context.Background(), &pb.CeremonyEnrollExternalOfficerRequest{
		Username:   "u",
		CertPem:    "CERTPEM",
		ShareIndex: 3,
		AuthFactor: "mtls",
		ShareBytes: []byte{0x01, 0x02},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if seen == nil || seen.Username != "u" || seen.CertPEM != "CERTPEM" || seen.ShareIndex != 3 || seen.AuthFactor != "mtls" {
		t.Fatalf("unexpected propagation: %#v", seen)
	}
	if resp.GetSealedShare() != "sealed-b64" {
		t.Fatalf("unexpected sealed_share: %q", resp.GetSealedShare())
	}
}

func TestCeremonyEnrollExternalOfficer_Validation(t *testing.T) {
	srv := newTestCeremonyServer(&mockCeremonyService{})
	cases := []struct {
		name string
		req  *pb.CeremonyEnrollExternalOfficerRequest
	}{
		{"nil request", nil},
		{"missing username", &pb.CeremonyEnrollExternalOfficerRequest{CertPem: "c", AuthFactor: "mtls"}},
		{"missing both cert and pubkey", &pb.CeremonyEnrollExternalOfficerRequest{Username: "u", AuthFactor: "mtls"}},
		{"both cert and pubkey", &pb.CeremonyEnrollExternalOfficerRequest{Username: "u", CertPem: "c", PubkeyPem: "p", AuthFactor: "mtls"}},
		{"missing auth_factor", &pb.CeremonyEnrollExternalOfficerRequest{Username: "u", CertPem: "c"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := srv.CeremonyEnrollExternalOfficer(context.Background(), c.req)
			assertGRPCCode(t, err, codes.InvalidArgument)
		})
	}
}

func TestCeremonyEnrollExternalOfficer_PubKeyVariant(t *testing.T) {
	mock := &mockCeremonyService{
		enrollExternalFn: func(_ context.Context, req *service.CeremonyEnrollExternalOfficerRequest) (*service.CeremonyEnrollExternalOfficerResponse, error) {
			if req.PubKeyPEM != "PUB" {
				t.Fatalf("expected pubkey_pem propagation, got %q", req.PubKeyPEM)
			}
			return &service.CeremonyEnrollExternalOfficerResponse{}, nil
		},
	}
	srv := newTestCeremonyServer(mock)
	_, err := srv.CeremonyEnrollExternalOfficer(context.Background(), &pb.CeremonyEnrollExternalOfficerRequest{
		Username:   "u",
		PubkeyPem:  "PUB",
		AuthFactor: "webauthn",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
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
		{"CeremonyClaimCertBegin", func() error {
			_, err := srv.CeremonyClaimCertBegin(ctx, &pb.CeremonyClaimCertBeginRequest{Username: "u"})
			return err
		}},
		{"CeremonyClaimCertComplete", func() error {
			_, err := srv.CeremonyClaimCertComplete(ctx, &pb.CeremonyClaimCertCompleteRequest{Username: "u", NonceHex: "x", Signature: "s"})
			return err
		}},
		{"CeremonyClaimShare", func() error {
			_, err := srv.CeremonyClaimShare(ctx, &pb.CeremonyClaimShareRequest{Username: "u"})
			return err
		}},
		{"CeremonySignCSR", func() error {
			_, err := srv.CeremonySignCSR(ctx, &pb.CeremonySignCSRRequest{Username: "u", CsrPem: "csr"})
			return err
		}},
		{"CeremonyListOfficers", func() error {
			_, err := srv.CeremonyListOfficers(ctx, &emptypb.Empty{})
			return err
		}},
		{"CeremonyEnrollExternalOfficer", func() error {
			_, err := srv.CeremonyEnrollExternalOfficer(ctx, &pb.CeremonyEnrollExternalOfficerRequest{Username: "u", CertPem: "c", AuthFactor: "mtls"})
			return err
		}},
	}
	for _, c := range checks {
		t.Run(c.name, func(t *testing.T) {
			assertGRPCCode(t, c.call(), codes.Unavailable)
		})
	}
}
