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
	"testing"

	"google.golang.org/grpc"

	xkmstransport "github.com/jeremyhahn/go-xkms/sdk/go/transport"

	pb "github.com/jeremyhahn/go-quicbaas/api/proto"
)

// fakePIVServer is an in-memory pb.PIVServiceServer. It distinguishes the
// Get/Export and Store/Import verb pairs so the tests can prove the handler
// routes each SDK method to its own RPC.
type fakePIVServer struct {
	fail     bool
	exported bool
	imported bool
}

func (f *fakePIVServer) ListPIVSlots(_ context.Context, req *pb.PIVListSlotsRequest) (*pb.PIVListSlotsResponse, error) {
	if err := failErr(f.fail); err != nil {
		return nil, err
	}
	return &pb.PIVListSlotsResponse{
		Slots: []*pb.PIVSlotStatus{{
			Slot: "9a", Name: "PIV Authentication", HasCert: true, HasKey: true,
			Subject: "CN=alice", Algorithm: "ECDSA-P256", KeySize: 256, Backend: req.Backend,
		}},
		Page: &pb.PIVPageResponse{Page: int32(1), PageSize: int32(25), Total: int32(1)},
	}, nil
}

func (f *fakePIVServer) GetPIVCertificate(_ context.Context, req *pb.PIVGetCertificateRequest) (*pb.PIVGetCertificateResponse, error) {
	if err := failErr(f.fail); err != nil {
		return nil, err
	}
	return &pb.PIVGetCertificateResponse{Slot: req.Slot, Certificate: []byte("cert-pem"), Format: req.Format}, nil
}

func (f *fakePIVServer) ExportPIVCertificate(ctx context.Context, req *pb.PIVGetCertificateRequest) (*pb.PIVGetCertificateResponse, error) {
	f.exported = true
	return f.GetPIVCertificate(ctx, req)
}

func (f *fakePIVServer) StorePIVCertificate(_ context.Context, req *pb.PIVStoreCertificateRequest) (*pb.PIVStoreCertificateResponse, error) {
	if err := failErr(f.fail); err != nil {
		return nil, err
	}
	return &pb.PIVStoreCertificateResponse{}, nil
}

func (f *fakePIVServer) ImportPIVCertificate(ctx context.Context, req *pb.PIVStoreCertificateRequest) (*pb.PIVStoreCertificateResponse, error) {
	f.imported = true
	return f.StorePIVCertificate(ctx, req)
}

func (f *fakePIVServer) DeletePIVCertificate(_ context.Context, req *pb.PIVDeleteCertificateRequest) (*pb.PIVDeleteCertificateResponse, error) {
	if err := failErr(f.fail); err != nil {
		return nil, err
	}
	return &pb.PIVDeleteCertificateResponse{}, nil
}

func (f *fakePIVServer) GeneratePIVKey(_ context.Context, req *pb.PIVGenerateKeyRequest) (*pb.PIVGenerateKeyResponse, error) {
	if err := failErr(f.fail); err != nil {
		return nil, err
	}
	return &pb.PIVGenerateKeyResponse{
		Slot: req.Slot, Certificate: []byte("cert"), PublicKey: []byte("pub"),
		Csr: []byte("csr"), ChainPem: []byte("chain"),
	}, nil
}

func (f *fakePIVServer) GeneratePIVCSR(_ context.Context, req *pb.PIVGenerateCSRRequest) (*pb.PIVGenerateCSRResponse, error) {
	if err := failErr(f.fail); err != nil {
		return nil, err
	}
	return &pb.PIVGenerateCSRResponse{Slot: req.Slot, Csr: []byte("csr-pem")}, nil
}

// newPIVTestConn starts an in-memory PIVService and returns a dialed
// connection plus the fake for assertions.
func newPIVTestConn(t *testing.T, fail bool) (*grpc.ClientConn, *fakePIVServer) {
	t.Helper()
	fake := &fakePIVServer{fail: fail}
	hs := newHandlerServer(t, func(s *grpc.Server) {
		pb.RegisterPIVServiceServer(s, fake)
	})
	return hs.dial(t), fake
}

func TestIsPIVService(t *testing.T) {
	tests := []struct {
		service  string
		expected bool
	}{
		{"quicbaas.PIV", true},
		{"PIV", true},
		{"quicbaas.FIDO2", false},
		{"quicbaas.KMS", false},
		{"", false},
	}
	for _, tt := range tests {
		if got := isPIVService(tt.service); got != tt.expected {
			t.Errorf("isPIVService(%q) = %v, want %v", tt.service, got, tt.expected)
		}
	}
}

func TestPIVHandler_ListPIVSlots_RoundTrip(t *testing.T) {
	conn, _ := newPIVTestConn(t, false)
	handler := newPIVHandler(conn)

	resp, err := handler.handleRequest(context.Background(), "quicbaas.PIV", "ListPIVSlots",
		&xkmstransport.ListPIVSlotsRequest{Backend: "file"})
	if err != nil {
		t.Fatalf("ListPIVSlots: %v", err)
	}
	out, ok := resp.(*xkmstransport.ListPIVSlotsResponse)
	if !ok {
		t.Fatalf("unexpected response type %T", resp)
	}
	if len(out.Slots) != 1 {
		t.Fatalf("slots = %d, want 1", len(out.Slots))
	}
	slot := out.Slots[0]
	if slot.Slot != "9a" || !slot.HasCert || !slot.HasKey || slot.Backend != "file" || slot.KeySize != 256 {
		t.Errorf("unexpected slot: %+v", slot)
	}
	if out.Total != 1 || out.PageSize != 25 {
		t.Errorf("pagination not decoded: %+v", out.PageResponse)
	}
}

func TestPIVHandler_GetAndExportPIVCertificate_RoundTrip(t *testing.T) {
	conn, fake := newPIVTestConn(t, false)
	handler := newPIVHandler(conn)

	resp, err := handler.handleRequest(context.Background(), "quicbaas.PIV", "GetPIVCertificate",
		&xkmstransport.GetPIVCertificateRequest{Backend: "file", Slot: "9a", Format: "pem"})
	if err != nil {
		t.Fatalf("GetPIVCertificate: %v", err)
	}
	out, ok := resp.(*xkmstransport.GetPIVCertificateResponse)
	if !ok {
		t.Fatalf("unexpected response type %T", resp)
	}
	if !bytes.Equal(out.Certificate, []byte("cert-pem")) || out.Slot != "9a" {
		t.Errorf("unexpected response: %+v", out)
	}
	if fake.exported {
		t.Error("GetPIVCertificate must not route to ExportPIVCertificate")
	}

	if _, err := handler.handleRequest(context.Background(), "quicbaas.PIV", "ExportPIVCertificate",
		&xkmstransport.GetPIVCertificateRequest{Backend: "file", Slot: "9a", Format: "pem"}); err != nil {
		t.Fatalf("ExportPIVCertificate: %v", err)
	}
	if !fake.exported {
		t.Error("ExportPIVCertificate did not route to the export RPC")
	}
}

func TestPIVHandler_StoreAndImportPIVCertificate_RoundTrip(t *testing.T) {
	conn, fake := newPIVTestConn(t, false)
	handler := newPIVHandler(conn)

	if _, err := handler.handleRequest(context.Background(), "quicbaas.PIV", "StorePIVCertificate",
		&xkmstransport.StorePIVCertificateRequest{Backend: "file", Slot: "9c", Certificate: []byte("cert"), Format: "pem"}); err != nil {
		t.Fatalf("StorePIVCertificate: %v", err)
	}
	if fake.imported {
		t.Error("StorePIVCertificate must not route to ImportPIVCertificate")
	}

	if _, err := handler.handleRequest(context.Background(), "quicbaas.PIV", "ImportPIVCertificate",
		&xkmstransport.StorePIVCertificateRequest{Backend: "file", Slot: "9c", Certificate: []byte("cert"), Format: "pem"}); err != nil {
		t.Fatalf("ImportPIVCertificate: %v", err)
	}
	if !fake.imported {
		t.Error("ImportPIVCertificate did not route to the import RPC")
	}
}

func TestPIVHandler_DeletePIVCertificate_RoundTrip(t *testing.T) {
	conn, _ := newPIVTestConn(t, false)
	handler := newPIVHandler(conn)

	resp, err := handler.handleRequest(context.Background(), "quicbaas.PIV", "DeletePIVCertificate",
		&xkmstransport.DeletePIVCertificateRequest{Backend: "file", Slot: "9a"})
	if err != nil {
		t.Fatalf("DeletePIVCertificate: %v", err)
	}
	if resp != nil {
		t.Errorf("expected nil response for delete, got %T", resp)
	}
}

func TestPIVHandler_GeneratePIVKey_RoundTrip(t *testing.T) {
	conn, _ := newPIVTestConn(t, false)
	handler := newPIVHandler(conn)

	resp, err := handler.handleRequest(context.Background(), "quicbaas.PIV", "GeneratePIVKey",
		&xkmstransport.GeneratePIVKeyRequest{Backend: "tpm2", Slot: "9a", Algorithm: "ecdsap256", Subject: "CN=alice", OutputFormat: "ca-signed", ValidityDays: 30})
	if err != nil {
		t.Fatalf("GeneratePIVKey: %v", err)
	}
	out, ok := resp.(*xkmstransport.GeneratePIVKeyResponse)
	if !ok {
		t.Fatalf("unexpected response type %T", resp)
	}
	if !bytes.Equal(out.CSR, []byte("csr")) || !bytes.Equal(out.ChainPEM, []byte("chain")) {
		t.Errorf("csr/chain not decoded: %+v", out)
	}
}

func TestPIVHandler_GeneratePIVCSR_RoundTrip(t *testing.T) {
	conn, _ := newPIVTestConn(t, false)
	handler := newPIVHandler(conn)

	resp, err := handler.handleRequest(context.Background(), "quicbaas.PIV", "GeneratePIVCSR",
		&xkmstransport.GeneratePIVCSRRequest{Backend: "file", Slot: "9a", Subject: "CN=alice", Organization: "ACME"})
	if err != nil {
		t.Fatalf("GeneratePIVCSR: %v", err)
	}
	out, ok := resp.(*xkmstransport.GeneratePIVCSRResponse)
	if !ok {
		t.Fatalf("unexpected response type %T", resp)
	}
	if !bytes.Equal(out.CSR, []byte("csr-pem")) {
		t.Errorf("unexpected CSR: %q", out.CSR)
	}
}

func TestPIVHandler_ServerError(t *testing.T) {
	conn, _ := newPIVTestConn(t, true)
	handler := newPIVHandler(conn)

	_, err := handler.handleRequest(context.Background(), "quicbaas.PIV", "ListPIVSlots",
		&xkmstransport.ListPIVSlotsRequest{Backend: "file"})
	if err == nil {
		t.Fatal("expected server error to propagate")
	}
}

func TestPIVHandler_UnsupportedMethod(t *testing.T) {
	handler := newPIVHandler(nil)
	_, err := handler.handleRequest(context.Background(), "quicbaas.PIV", "Attest", nil)
	if err == nil {
		t.Fatal("expected error for method not exposed by PIVService proto")
	}
}

func TestPIVHandler_InvalidRequestTypes(t *testing.T) {
	conn, _ := newPIVTestConn(t, false)
	handler := newPIVHandler(conn)

	methods := []string{
		"ListPIVSlots", "GetPIVCertificate", "ExportPIVCertificate",
		"StorePIVCertificate", "ImportPIVCertificate", "DeletePIVCertificate",
		"GeneratePIVKey", "GeneratePIVCSR",
	}
	for _, method := range methods {
		if _, err := handler.handleRequest(context.Background(), "quicbaas.PIV", method, 42); err == nil {
			t.Errorf("%s: expected invalid request type error", method)
		}
	}
}
