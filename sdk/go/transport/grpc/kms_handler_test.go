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
	"reflect"
	"testing"

	"google.golang.org/grpc"

	pb "github.com/jeremyhahn/go-quicbaas/api/proto"
	kmsclient "github.com/jeremyhahn/go-quicbaas/sdk/go/client/kms"
)

// generateRecordingKMSServer keeps the whole GenerateKey request it received
// so a test can assert every field the SDK put on the wire.
type generateRecordingKMSServer struct {
	pb.UnimplementedKeyManagementServer
	got *pb.GenerateKeyRequest
}

func (s *generateRecordingKMSServer) GenerateKey(_ context.Context, req *pb.GenerateKeyRequest) (*pb.GenerateKeyResponse, error) {
	s.got = req
	return &pb.GenerateKeyResponse{
		Key:      &pb.KeyHandle{KeyId: "key-m", Version: 1, Algorithm: pb.KeyAlgorithm_KEY_ALG_AES_256_GCM},
		Metadata: &pb.KeyMetadata{KeyId: "key-m", Name: req.Name, Algorithm: pb.KeyAlgorithm_KEY_ALG_AES_256_GCM, Version: 1},
	}, nil
}

func newGenerateRecordingKMSHandler(t *testing.T) (*kmsHandler, *generateRecordingKMSServer) {
	t.Helper()
	fake := &generateRecordingKMSServer{}
	srv := newHandlerServer(t, func(s *grpc.Server) {
		pb.RegisterKeyManagementServer(s, fake)
	})
	h, err := newKMSHandler(srv.dial(t))
	if err != nil {
		t.Fatalf("newKMSHandler: %v", err)
	}
	return h, fake
}

func TestKMSHandler_GenerateKey_CarriesDescriptionTagsOwner(t *testing.T) {
	h, fake := newGenerateRecordingKMSHandler(t)

	_, err := h.handleRequest(context.Background(), "quicbaas.KMS", "GenerateKey",
		&kmsclient.GenerateKeyRequest{
			Name:        "key-m",
			Algorithm:   kmsclient.AlgorithmAES256GCM,
			Description: "d",
			Tags:        map[string]string{"env": "prod"},
			Owner:       "svc-a",
		})
	if err != nil {
		t.Fatalf("GenerateKey: unexpected error: %v", err)
	}
	if fake.got == nil {
		t.Fatal("server received no GenerateKey request")
	}
	if fake.got.Description != "d" {
		t.Errorf("description = %q, want %q", fake.got.Description, "d")
	}
	if !reflect.DeepEqual(fake.got.Tags, map[string]string{"env": "prod"}) {
		t.Errorf("tags = %v, want env=prod", fake.got.Tags)
	}
	if fake.got.Owner != "svc-a" {
		t.Errorf("owner = %q, want %q", fake.got.Owner, "svc-a")
	}
}

func TestKMSHandler_GenerateKey_NilRequest(t *testing.T) {
	h, fake := newGenerateRecordingKMSHandler(t)

	var req *kmsclient.GenerateKeyRequest
	if _, err := h.handleRequest(context.Background(), "quicbaas.KMS", "GenerateKey", req); err == nil {
		t.Fatal("expected an error for a nil request")
	}
	if fake.got != nil {
		t.Error("a nil request reached the server")
	}
}
