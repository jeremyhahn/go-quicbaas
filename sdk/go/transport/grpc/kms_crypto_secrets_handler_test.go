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
	"testing"

	"google.golang.org/grpc"

	pb "github.com/jeremyhahn/go-quicbaas/api/proto"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
	dekclient "github.com/jeremyhahn/go-quicbaas/sdk/go/client/dek"
	kmsclient "github.com/jeremyhahn/go-quicbaas/sdk/go/client/kms"
	secretsclient "github.com/jeremyhahn/go-quicbaas/sdk/go/client/secrets"
)

// =============================================================================
// Service classifier tests
// =============================================================================

func TestKMSCryptoSecretsServiceClassifiers(t *testing.T) {
	cases := []struct {
		name string
		fn   func(string) bool
		svc  string
		want bool
	}{
		{"kms-match", isKMSService, "quicbaas.KMS", true},
		{"kms-no-match", isKMSService, "quicbaas.Crypto", false},
		{"dek-match", isDEKService, "quicbaas.DEK", true},
		{"dek-no-match", isDEKService, "quicbaas.KMS", false},
		{"secrets-match", isSecretsService, "quicbaas.Secrets", true},
		{"secrets-no-match", isSecretsService, "quicbaas.KMS", false},
		{"crypto-match", isCryptoService, "quicbaas.Crypto", true},
		{"crypto-no-match", isCryptoService, "quicbaas.Secrets", false},
		{"empty", isKMSService, "", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.fn(tc.svc); got != tc.want {
				t.Errorf("%s(%q) = %v, want %v", tc.name, tc.svc, got, tc.want)
			}
		})
	}
}

// =============================================================================
// KMS: KeyManagement / DEKManagement fakes
// =============================================================================

// fakeKeyManagementServer implements the KeyManagement proto service. It
// records the most recent GenerateKey request so the test can assert the
// correct proto message was produced from the SDK struct.
type fakeKeyManagementServer struct {
	pb.UnimplementedKeyManagementServer
	fail        bool
	lastGenName string
	lastGenAlg  pb.KeyAlgorithm
}

func (f *fakeKeyManagementServer) GenerateKey(_ context.Context, req *pb.GenerateKeyRequest) (*pb.GenerateKeyResponse, error) {
	if err := failErr(f.fail); err != nil {
		return nil, err
	}
	f.lastGenName = req.Name
	if req.KeySpec != nil {
		f.lastGenAlg = req.KeySpec.Algorithm
	}
	return &pb.GenerateKeyResponse{
		Key: &pb.KeyHandle{
			KeyId:     "key-123",
			Version:   1,
			Algorithm: pb.KeyAlgorithm_KEY_ALG_AES_256_GCM,
			Status:    pb.KeyStatus_KEY_STATUS_ENABLED,
		},
		Metadata: &pb.KeyMetadata{
			KeyId:     "key-123",
			Name:      req.Name,
			Algorithm: pb.KeyAlgorithm_KEY_ALG_AES_256_GCM,
			Status:    pb.KeyStatus_KEY_STATUS_ENABLED,
			Version:   1,
		},
		Timestamp: 1,
	}, nil
}

func newKMSHandlerWithServer(t *testing.T, fail bool) (*kmsHandler, *fakeKeyManagementServer) {
	t.Helper()
	fake := &fakeKeyManagementServer{fail: fail}
	srv := newHandlerServer(t, func(s *grpc.Server) {
		pb.RegisterKeyManagementServer(s, fake)
	})
	h, err := newKMSHandler(srv.dial(t))
	if err != nil {
		t.Fatalf("newKMSHandler: %v", err)
	}
	return h, fake
}

func TestKMSHandler_GenerateKey_Success(t *testing.T) {
	h, fake := newKMSHandlerWithServer(t, false)

	resp, err := h.handleRequest(context.Background(), "quicbaas.KMS", "GenerateKey",
		&kmsclient.GenerateKeyRequest{
			Name:      "my-key",
			Algorithm: kmsclient.AlgorithmAES256GCM,
			Usage:     []kmsclient.KeyUsage{kmsclient.KeyUsageEncrypt, kmsclient.KeyUsageDecrypt},
		})
	if err != nil {
		t.Fatalf("GenerateKey: unexpected error: %v", err)
	}

	// The handler must hit the KeyManagement proto service, not the generic
	// fallback. Assert the fake recorded the converted request.
	if fake.lastGenName != "my-key" {
		t.Errorf("server saw name %q, want %q", fake.lastGenName, "my-key")
	}
	if fake.lastGenAlg != pb.KeyAlgorithm_KEY_ALG_AES_256_GCM {
		t.Errorf("server saw algorithm %v, want AES_256_GCM", fake.lastGenAlg)
	}

	// The response must round-trip as the canonical service type the SDK
	// client accepts (case *service.GenerateKeyResponse in kms.GenerateKey).
	gen, ok := resp.(*service.GenerateKeyResponse)
	if !ok {
		t.Fatalf("unexpected response type %T", resp)
	}
	if gen.Metadata == nil || gen.Metadata.KeyID != "key-123" {
		t.Errorf("response metadata = %+v, want KeyID key-123", gen.Metadata)
	}
}

func TestKMSHandler_GenerateKey_ServerError(t *testing.T) {
	h, _ := newKMSHandlerWithServer(t, true)
	if _, err := h.handleRequest(context.Background(), "quicbaas.KMS", "GenerateKey",
		&kmsclient.GenerateKeyRequest{Name: "k", Algorithm: kmsclient.AlgorithmAES256GCM}); err == nil {
		t.Fatal("expected error from failing server")
	}
}

func TestKMSHandler_GenerateKey_InvalidRequestType(t *testing.T) {
	h, _ := newKMSHandlerWithServer(t, false)
	if _, err := h.handleRequest(context.Background(), "quicbaas.KMS", "GenerateKey", "not-a-request"); err == nil {
		t.Fatal("expected error for invalid request type")
	}
}

func TestKMSHandler_UnimplementedMethod(t *testing.T) {
	h, _ := newKMSHandlerWithServer(t, false)
	// A method absent from kmsHandlerMap must report unimplemented rather
	// than falling through to the untyped conn.Invoke path.
	if _, err := h.handleRequest(context.Background(), "quicbaas.KMS", "NoSuchMethod", nil); err == nil {
		t.Fatal("expected unimplemented error for unknown method")
	}
}

func TestNewKMSHandler_NilConn(t *testing.T) {
	if _, err := newKMSHandler(nil); err == nil {
		t.Fatal("expected error wrapping a nil connection")
	}
}

// fakeDEKManagementServer implements the DEKManagement proto service.
type fakeDEKManagementServer struct {
	pb.UnimplementedDEKManagementServer
	lastPath string
}

func (f *fakeDEKManagementServer) CreateDEK(_ context.Context, req *pb.CreateDEKRequest) (*pb.CreateDEKResponse, error) {
	f.lastPath = req.Path
	return &pb.CreateDEKResponse{
		Dek:       &pb.DEKMetadata{Path: req.Path, Version: 1},
		Timestamp: 1,
	}, nil
}

func TestKMSHandler_CreateDEK_Success_ViaDEKService(t *testing.T) {
	fake := &fakeDEKManagementServer{}
	srv := newHandlerServer(t, func(s *grpc.Server) {
		pb.RegisterDEKManagementServer(s, fake)
	})
	h, err := newKMSHandler(srv.dial(t))
	if err != nil {
		t.Fatalf("newKMSHandler: %v", err)
	}

	if _, err := h.handleRequest(context.Background(), "quicbaas.DEK", "CreateDEK",
		&dekclient.CreateDEKRequest{Path: "/dek/a", Algorithm: "AES-256-GCM"}); err != nil {
		t.Fatalf("CreateDEK: unexpected error: %v", err)
	}
	if fake.lastPath != "/dek/a" {
		t.Errorf("server saw path %q, want %q", fake.lastPath, "/dek/a")
	}
}

// =============================================================================
// Crypto fakes
// =============================================================================

type fakeCryptoServer struct {
	pb.UnimplementedCryptoServer
	fail        bool
	lastNumByte int32
}

func (f *fakeCryptoServer) GenerateRandom(_ context.Context, req *pb.GenerateRandomRequest) (*pb.GenerateRandomResponse, error) {
	if err := failErr(f.fail); err != nil {
		return nil, err
	}
	f.lastNumByte = req.NumBytes
	return &pb.GenerateRandomResponse{RandomBytes: []byte("0123456789abcdef"), Timestamp: 1}, nil
}

func newCryptoHandlerWithServer(t *testing.T, fail bool) (*cryptoHandler, *fakeCryptoServer) {
	t.Helper()
	fake := &fakeCryptoServer{fail: fail}
	srv := newHandlerServer(t, func(s *grpc.Server) {
		pb.RegisterCryptoServer(s, fake)
	})
	h, err := newCryptoHandler(srv.dial(t))
	if err != nil {
		t.Fatalf("newCryptoHandler: %v", err)
	}
	return h, fake
}

func TestCryptoHandler_GenerateRandom_Success(t *testing.T) {
	h, fake := newCryptoHandlerWithServer(t, false)

	// The crypto SDK client converts its input to the canonical service type
	// before reaching the transport, so the handler receives *service.X.
	resp, err := h.handleRequest(context.Background(), "quicbaas.Crypto", "GenerateRandom",
		&service.GenerateRandomRequest{NumBytes: 16})
	if err != nil {
		t.Fatalf("GenerateRandom: unexpected error: %v", err)
	}
	if fake.lastNumByte != 16 {
		t.Errorf("server saw num_bytes %d, want 16", fake.lastNumByte)
	}
	if resp == nil {
		t.Fatal("expected non-nil response")
	}
}

func TestCryptoHandler_GenerateRandom_ServerError(t *testing.T) {
	h, _ := newCryptoHandlerWithServer(t, true)
	if _, err := h.handleRequest(context.Background(), "quicbaas.Crypto", "GenerateRandom",
		&service.GenerateRandomRequest{NumBytes: 16}); err == nil {
		t.Fatal("expected error from failing server")
	}
}

func TestCryptoHandler_InvalidRequestType(t *testing.T) {
	h, _ := newCryptoHandlerWithServer(t, false)
	if _, err := h.handleRequest(context.Background(), "quicbaas.Crypto", "Encrypt", "bad"); err == nil {
		t.Fatal("expected error for invalid request type")
	}
}

func TestCryptoHandler_UnimplementedMethod(t *testing.T) {
	h, _ := newCryptoHandlerWithServer(t, false)
	if _, err := h.handleRequest(context.Background(), "quicbaas.Crypto", "EncryptBatch", nil); err == nil {
		t.Fatal("expected unimplemented error for EncryptBatch")
	}
}

func TestNewCryptoHandler_NilConn(t *testing.T) {
	if _, err := newCryptoHandler(nil); err == nil {
		t.Fatal("expected error wrapping a nil connection")
	}
}

// =============================================================================
// Secrets fakes
// =============================================================================

type fakeSecretsServer struct {
	pb.UnimplementedSecretsManagementServer
	fail     bool
	lastName string
	lastData []byte
}

func (f *fakeSecretsServer) StoreSecret(_ context.Context, req *pb.StoreSecretRequest) (*pb.StoreSecretResponse, error) {
	if err := failErr(f.fail); err != nil {
		return nil, err
	}
	f.lastName = req.Name
	f.lastData = req.Data
	return &pb.StoreSecretResponse{SecretId: req.Name, Version: 1, Timestamp: 1}, nil
}

func newSecretsHandlerWithServer(t *testing.T, fail bool) (*secretsHandler, *fakeSecretsServer) {
	t.Helper()
	fake := &fakeSecretsServer{fail: fail}
	srv := newHandlerServer(t, func(s *grpc.Server) {
		pb.RegisterSecretsManagementServer(s, fake)
	})
	h, err := newSecretsHandler(srv.dial(t))
	if err != nil {
		t.Fatalf("newSecretsHandler: %v", err)
	}
	return h, fake
}

func TestSecretsHandler_Store_Success(t *testing.T) {
	h, fake := newSecretsHandlerWithServer(t, false)

	resp, err := h.handleRequest(context.Background(), "quicbaas.Secrets", "Store",
		&secretsclient.StoreSecretRequest{
			Path: "/app/db",
			Data: map[string]interface{}{"password": "s3cr3t"},
		})
	if err != nil {
		t.Fatalf("Store: unexpected error: %v", err)
	}

	if fake.lastName != "/app/db" {
		t.Errorf("server saw name %q, want %q", fake.lastName, "/app/db")
	}
	if len(fake.lastData) == 0 {
		t.Error("server received empty secret data")
	}

	storeResp, ok := resp.(*secretsclient.StoreSecretResponse)
	if !ok {
		t.Fatalf("unexpected response type %T", resp)
	}
	if storeResp.Version != 1 {
		t.Errorf("version = %d, want 1", storeResp.Version)
	}
}

func TestSecretsHandler_Store_ServerError(t *testing.T) {
	h, _ := newSecretsHandlerWithServer(t, true)
	if _, err := h.handleRequest(context.Background(), "quicbaas.Secrets", "Store",
		&secretsclient.StoreSecretRequest{Path: "/p", Data: map[string]interface{}{"k": "v"}}); err == nil {
		t.Fatal("expected error from failing server")
	}
}

func TestSecretsHandler_Store_InvalidRequestType(t *testing.T) {
	h, _ := newSecretsHandlerWithServer(t, false)
	if _, err := h.handleRequest(context.Background(), "quicbaas.Secrets", "Store", "bad"); err == nil {
		t.Fatal("expected error for invalid request type")
	}
}

func TestSecretsHandler_UnimplementedMethod(t *testing.T) {
	h, _ := newSecretsHandlerWithServer(t, false)
	if _, err := h.handleRequest(context.Background(), "quicbaas.Secrets", "GetVersion", nil); err == nil {
		t.Fatal("expected unimplemented error for GetVersion")
	}
}

func TestNewSecretsHandler_NilConn(t *testing.T) {
	if _, err := newSecretsHandler(nil); err == nil {
		t.Fatal("expected error wrapping a nil connection")
	}
}
