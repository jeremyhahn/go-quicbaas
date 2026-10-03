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
	"context"
	stderrors "errors"
	"net"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"
	"google.golang.org/protobuf/types/known/timestamppb"

	pb "github.com/jeremyhahn/go-quicbaas/api/proto"
	attestationclient "github.com/jeremyhahn/go-quicbaas/sdk/go/client/attestation"
	sdkerrors "github.com/jeremyhahn/go-quicbaas/sdk/go/errors"
)

// recordingVerifierServer records what each Verifier RPC received.
type recordingVerifierServer struct {
	pb.UnimplementedAttestationServiceServer
	received map[string]interface{}
	fail     error
}

func (s *recordingVerifierServer) record(method string, req interface{}) error {
	s.received[method] = req
	return s.fail
}

var verifierStamp = timestamppb.New(time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC))

func (s *recordingVerifierServer) PutEndorsement(_ context.Context, req *pb.PutEndorsementRequest) (*pb.Endorsement, error) {
	if err := s.record("PutEndorsement", req); err != nil {
		return nil, err
	}
	return &pb.Endorsement{Name: req.Name, Kind: req.Kind, CertificatePem: req.CertificatePem, CreatedAt: verifierStamp}, nil
}

func (s *recordingVerifierServer) ListEndorsements(_ context.Context, req *pb.ListEndorsementsRequest) (*pb.ListEndorsementsResponse, error) {
	if err := s.record("ListEndorsements", req); err != nil {
		return nil, err
	}
	return &pb.ListEndorsementsResponse{Endorsements: []*pb.Endorsement{{Name: "a", Kind: req.Kind}}}, nil
}

func (s *recordingVerifierServer) DeleteEndorsement(_ context.Context, req *pb.DeleteEndorsementRequest) (*pb.DeleteEndorsementResponse, error) {
	return &pb.DeleteEndorsementResponse{}, s.record("DeleteEndorsement", req)
}

func (s *recordingVerifierServer) PutReferenceValues(_ context.Context, req *pb.PutReferenceValuesRequest) (*pb.ReferenceValues, error) {
	if err := s.record("PutReferenceValues", req); err != nil {
		return nil, err
	}
	return &pb.ReferenceValues{Name: req.Name, PcrIndices: []uint32{0, 7}, StoredAt: verifierStamp}, nil
}

func (s *recordingVerifierServer) GetReferenceValues(_ context.Context, req *pb.GetReferenceValuesRequest) (*pb.ReferenceValues, error) {
	if err := s.record("GetReferenceValues", req); err != nil {
		return nil, err
	}
	return &pb.ReferenceValues{Name: req.Name, SignedCorim: []byte{0xd2}}, nil
}

func (s *recordingVerifierServer) ListReferenceValues(_ context.Context, req *pb.ListReferenceValuesRequest) (*pb.ListReferenceValuesResponse, error) {
	if err := s.record("ListReferenceValues", req); err != nil {
		return nil, err
	}
	return &pb.ListReferenceValuesResponse{ReferenceValues: []*pb.ReferenceValues{{Name: "golden"}}}, nil
}

func (s *recordingVerifierServer) DeleteReferenceValues(_ context.Context, req *pb.DeleteReferenceValuesRequest) (*pb.DeleteReferenceValuesResponse, error) {
	return &pb.DeleteReferenceValuesResponse{}, s.record("DeleteReferenceValues", req)
}

func (s *recordingVerifierServer) AppraiseQuote(_ context.Context, req *pb.AppraiseQuoteRequest) (*pb.AttestationResult, error) {
	if err := s.record("AppraiseQuote", req); err != nil {
		return nil, err
	}
	return &pb.AttestationResult{
		Verified: true, BaselineSource: "signed", AppraisedAt: verifierStamp,
		Checks: []*pb.AppraisalCheck{{Name: "quote", Outcome: "pass"}},
	}, nil
}

func verifierClient(t *testing.T, srv *recordingVerifierServer) pb.AttestationServiceClient {
	t.Helper()
	lis := bufconn.Listen(1 << 20)
	server := grpc.NewServer()
	pb.RegisterAttestationServiceServer(server, srv)
	served := make(chan error, 1)
	go func() { served <- server.Serve(lis) }()
	conn, err := grpc.NewClient("passthrough:///bufnet",
		grpc.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) { return lis.DialContext(ctx) }),
		grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := conn.Close(); err != nil {
			t.Errorf("close: %v", err)
		}
		server.Stop()
		if err := <-served; err != nil && !stderrors.Is(err, grpc.ErrServerStopped) {
			t.Errorf("serve: %v", err)
		}
	})
	return pb.NewAttestationServiceClient(conn)
}

func TestAttestationVerifierGRPC_RoundTrip(t *testing.T) {
	srv := &recordingVerifierServer{received: map[string]interface{}{}}
	client := verifierClient(t, srv)
	ctx := context.Background()
	call := func(method string, req interface{}) interface{} {
		t.Helper()
		out, err := attestationVerifierHandlers[method](ctx, client, req)
		if err != nil {
			t.Fatalf("%s: %v", method, err)
		}
		return out
	}

	e := call("PutEndorsement", &attestationclient.PutEndorsementRequest{Name: "ak", Kind: "ak-root", CertificatePEM: []byte("pem")}).(*attestationclient.Endorsement)
	if got := srv.received["PutEndorsement"].(*pb.PutEndorsementRequest); got.Name != "ak" || got.Kind != "ak-root" || string(got.CertificatePem) != "pem" {
		t.Fatalf("server received %+v", got)
	}
	if e.CreatedAt != "2026-10-01T12:00:00Z" {
		t.Fatalf("CreatedAt = %q", e.CreatedAt)
	}
	if l := call("ListEndorsements", &attestationclient.ListEndorsementsRequest{Kind: "ak-root"}).(*attestationclient.ListEndorsementsResponse); len(l.Endorsements) != 1 || srv.received["ListEndorsements"].(*pb.ListEndorsementsRequest).Kind != "ak-root" {
		t.Fatalf("ListEndorsements = %+v", l)
	}
	call("DeleteEndorsement", &attestationclient.NameRequest{Name: "ak"})
	if srv.received["DeleteEndorsement"].(*pb.DeleteEndorsementRequest).Name != "ak" {
		t.Fatal("DeleteEndorsement name not received")
	}
	rv := call("PutReferenceValues", &attestationclient.PutReferenceValuesRequest{Name: "golden", SignedCoRIM: []byte{0xd2}}).(*attestationclient.ReferenceValues)
	if got := srv.received["PutReferenceValues"].(*pb.PutReferenceValuesRequest); got.Name != "golden" || len(got.SignedCorim) != 1 || len(rv.PCRIndices) != 2 || rv.NotBefore != "" {
		t.Fatalf("PutReferenceValues sent %+v got %+v", got, rv)
	}
	if got := call("GetReferenceValues", &attestationclient.NameRequest{Name: "golden"}).(*attestationclient.ReferenceValues); len(got.SignedCoRIM) != 1 {
		t.Fatalf("GetReferenceValues = %+v", got)
	}
	if l := call("ListReferenceValues", nil).(*attestationclient.ListReferenceValuesResponse); len(l.ReferenceValues) != 1 {
		t.Fatalf("ListReferenceValues = %+v", l)
	}
	call("DeleteReferenceValues", &attestationclient.NameRequest{Name: "golden"})

	result := call("AppraiseQuote", &attestationclient.AppraiseQuoteRequest{
		AKCertificatePEM: []byte("ak"), Quote: []byte("q"), Signature: []byte("s"), Nonce: []byte("n"),
		PCRs: map[uint32][]byte{7: {1}}, EventLog: []byte("log"), ReferenceValues: "golden",
	}).(*attestationclient.AttestationResult)
	got := srv.received["AppraiseQuote"].(*pb.AppraiseQuoteRequest)
	if string(got.Quote) != "q" || string(got.EventLog) != "log" || got.ReferenceValues != "golden" || len(got.Pcrs[7]) != 1 {
		t.Fatalf("server received %+v", got)
	}
	if !result.Verified || result.Checks[0].Outcome != "pass" || result.AppraisedAt == "" {
		t.Fatalf("result = %+v", result)
	}
}

func TestAttestationVerifierGRPC_Errors(t *testing.T) {
	srv := &recordingVerifierServer{received: map[string]interface{}{}, fail: status.Error(codes.NotFound, "missing")}
	client := verifierClient(t, srv)
	ctx := context.Background()
	requests := map[string]interface{}{
		"PutEndorsement":        &attestationclient.PutEndorsementRequest{Name: "x"},
		"ListEndorsements":      &attestationclient.ListEndorsementsRequest{},
		"DeleteEndorsement":     &attestationclient.NameRequest{Name: "x"},
		"PutReferenceValues":    &attestationclient.PutReferenceValuesRequest{Name: "x"},
		"GetReferenceValues":    &attestationclient.NameRequest{Name: "x"},
		"ListReferenceValues":   nil,
		"DeleteReferenceValues": &attestationclient.NameRequest{Name: "x"},
		"AppraiseQuote":         &attestationclient.AppraiseQuoteRequest{},
	}
	for method, req := range requests {
		if _, err := attestationVerifierHandlers[method](ctx, client, req); !sdkerrors.IsNotFound(err) {
			t.Fatalf("%s = %v, want not found", method, err)
		}
		if method == "ListReferenceValues" {
			continue
		}
		if _, err := attestationVerifierHandlers[method](ctx, client, "wrong"); !sdkerrors.IsInvalidInput(err) {
			t.Fatalf("%s wrong type = %v", method, err)
		}
	}
	h := newAttestationHandler(nil)
	if _, err := h.handleRequest(ctx, "quicbaas.Attestation", "Nope", nil); sdkerrors.Code(err) != sdkerrors.CodeUnimplemented {
		t.Fatalf("unknown = %v", err)
	}
}
