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

	xkmssdk "github.com/jeremyhahn/go-xkms/sdk/go"

	"github.com/jeremyhahn/go-quicbaas/pkg/service"
	kmsclient "github.com/jeremyhahn/go-quicbaas/sdk/go/client/kms"
)

// generateRecordingKMSServer is the go-xkms xkms.v1 KeystoreService the node
// serves for KMS keys. It keeps the whole GenerateKey request it received so a
// test can assert every field the SDK put on the wire.
type generateRecordingKMSServer struct {
	xkmssdk.ProtoUnimplementedKeystoreServiceServer
	got *xkmssdk.ProtoGenerateKeyRequest
}

func (s *generateRecordingKMSServer) GenerateKey(_ context.Context, req *xkmssdk.ProtoGenerateKeyRequest) (*xkmssdk.ProtoGenerateKeyResponse, error) {
	s.got = req
	return &xkmssdk.ProtoGenerateKeyResponse{KeyId: req.GetKeyId(), KeyType: req.GetKeyType()}, nil
}

func newGenerateRecordingKMSHandler(t *testing.T) (*kmsHandler, *generateRecordingKMSServer) {
	t.Helper()
	fake := &generateRecordingKMSServer{}
	srv := newHandlerServer(t, func(s *grpc.Server) {
		xkmssdk.ProtoRegisterKeystoreServiceServer(s, fake)
	})
	h, err := newKMSHandler(srv.dial(t))
	if err != nil {
		t.Fatalf("newKMSHandler: %v", err)
	}
	return h, fake
}

// xkms.v1 GenerateKey carries no description, tags or owner, so the handler
// returns the caller's values on the response metadata.
func TestKMSHandler_GenerateKey_CarriesDescriptionTagsOwner(t *testing.T) {
	h, fake := newGenerateRecordingKMSHandler(t)

	resp, err := h.handleRequest(context.Background(), "quicbaas.KMS", "GenerateKey",
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
	if fake.got.GetKeyId() != "key-m" {
		t.Errorf("key id = %q, want %q", fake.got.GetKeyId(), "key-m")
	}
	if fake.got.GetAlgorithm() != "aes256-gcm" {
		t.Errorf("algorithm = %q, want %q", fake.got.GetAlgorithm(), "aes256-gcm")
	}
	gen, ok := resp.(*service.GenerateKeyResponse)
	if !ok {
		t.Fatalf("response type = %T, want *service.GenerateKeyResponse", resp)
	}
	if gen.Metadata == nil {
		t.Fatal("response metadata is nil")
	}
	if gen.Metadata.Description != "d" {
		t.Errorf("description = %q, want %q", gen.Metadata.Description, "d")
	}
	if !reflect.DeepEqual(gen.Metadata.Tags, map[string]string{"env": "prod"}) {
		t.Errorf("tags = %v, want env=prod", gen.Metadata.Tags)
	}
	if gen.Metadata.Owner != "svc-a" {
		t.Errorf("owner = %q, want %q", gen.Metadata.Owner, "svc-a")
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
