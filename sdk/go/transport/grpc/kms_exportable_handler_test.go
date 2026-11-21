// Copyright (c) 2026 Jeremy Hahn
// Copyright (c) 2026 Automate The Things, LLC
//
// This file is part of go-quicbaas.

package grpc

import (
	"context"
	"testing"

	"google.golang.org/grpc"

	pb "github.com/jeremyhahn/go-quicbaas/api/proto"
	kmsclient "github.com/jeremyhahn/go-quicbaas/sdk/go/client/kms"
)

// exportableKMSServer records the Exportable flag of the ImportKey and
// UnwrapKey requests it receives.
type exportableKMSServer struct {
	pb.UnimplementedKeyManagementServer
	importReq *pb.ImportKeyRequest
	unwrapReq *pb.UnwrapKeyRequest
}

func (s *exportableKMSServer) ImportKey(_ context.Context, req *pb.ImportKeyRequest) (*pb.ImportKeyResponse, error) {
	s.importReq = req
	return &pb.ImportKeyResponse{}, nil
}

func (s *exportableKMSServer) UnwrapKey(_ context.Context, req *pb.UnwrapKeyRequest) (*pb.UnwrapKeyResponse, error) {
	s.unwrapReq = req
	return &pb.UnwrapKeyResponse{}, nil
}

func newExportableKMSHandler(t *testing.T) (*kmsHandler, *exportableKMSServer) {
	t.Helper()
	fake := &exportableKMSServer{}
	srv := newHandlerServer(t, func(s *grpc.Server) {
		pb.RegisterKeyManagementServer(s, fake)
	})
	h, err := newKMSHandler(srv.dial(t))
	if err != nil {
		t.Fatalf("newKMSHandler: %v", err)
	}
	return h, fake
}

func TestKMSHandler_ImportKey_CarriesExportable(t *testing.T) {
	for _, exportable := range []bool{true, false} {
		h, fake := newExportableKMSHandler(t)
		_, err := h.handleRequest(context.Background(), "quicbaas.KMS", "ImportKey",
			&kmsclient.ImportKeyRequest{
				KeyMaterial: []byte("material"),
				Name:        "imported",
				Algorithm:   kmsclient.AlgorithmAES256GCM,
				Exportable:  exportable,
			})
		if err != nil {
			t.Fatalf("ImportKey(exportable=%v): %v", exportable, err)
		}
		if fake.importReq == nil {
			t.Fatalf("ImportKey(exportable=%v): server saw no request", exportable)
		}
		if got := fake.importReq.GetKeySpec().GetExportable(); got != exportable {
			t.Errorf("ImportKey: server saw exportable %v, want %v", got, exportable)
		}
	}
}

func TestKMSHandler_UnwrapKey_CarriesExportable(t *testing.T) {
	for _, exportable := range []bool{true, false} {
		h, fake := newExportableKMSHandler(t)
		_, err := h.handleRequest(context.Background(), "quicbaas.KMS", "UnwrapKey",
			&kmsclient.UnwrapKeyRequest{
				KeyID:             "unwrapped",
				WrappingKeyID:     "kek",
				WrappingAlgorithm: kmsclient.WrapAlgAESKWP,
				WrappedMaterial:   []byte("wrapped"),
				Algorithm:         kmsclient.AlgorithmAES256GCM,
				Exportable:        exportable,
			})
		if err != nil {
			t.Fatalf("UnwrapKey(exportable=%v): %v", exportable, err)
		}
		if fake.unwrapReq == nil {
			t.Fatalf("UnwrapKey(exportable=%v): server saw no request", exportable)
		}
		if got := fake.unwrapReq.GetExportable(); got != exportable {
			t.Errorf("UnwrapKey: server saw exportable %v, want %v", got, exportable)
		}
	}
}

func TestKMSHandler_ImportUnwrap_RejectInvalidRequestType(t *testing.T) {
	h, fake := newExportableKMSHandler(t)
	for _, method := range []string{"ImportKey", "UnwrapKey"} {
		if _, err := h.handleRequest(context.Background(), "quicbaas.KMS", method, "not-a-request"); err == nil {
			t.Errorf("%s: expected an error for an invalid request type", method)
		}
	}
	if fake.importReq != nil || fake.unwrapReq != nil {
		t.Error("server must not be called for an invalid request type")
	}
}
