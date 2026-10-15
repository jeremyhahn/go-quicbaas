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

package mock

import (
	"context"
	"testing"
	"time"

	"github.com/jeremyhahn/go-quicbaas/sdk/go/client/crypto"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/client/kms"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/client/seal"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/client/secrets"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/errors"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/service"
)

// =============================================================================
// Seal Client Tests
// =============================================================================

func TestSealClient_Status_Unsealed(t *testing.T) {
	transport := NewTransport()
	client := seal.NewClient(transport)

	transport.SetResponse("quicbaas.Seal", "Status", &seal.SealStatus{
		Sealed:         false,
		Initialized:    true,
		Progress:       0,
		Required:       3,
		ClusterName:    "test-cluster",
		ClusterVersion: "1.0.0",
	}, nil)

	ctx := context.Background()
	status, err := client.Status(ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if status.Sealed {
		t.Error("expected unsealed status")
	}
	if !status.Initialized {
		t.Error("expected initialized status")
	}
	if status.Required != 3 {
		t.Errorf("expected required=3, got %d", status.Required)
	}
	if status.ClusterName != "test-cluster" {
		t.Errorf("expected cluster name 'test-cluster', got '%s'", status.ClusterName)
	}

	// Verify the request was captured
	requests := transport.Requests()
	if len(requests) != 1 {
		t.Fatalf("expected 1 request, got %d", len(requests))
	}
	if requests[0].Service != "quicbaas.Seal" || requests[0].Method != "Status" {
		t.Errorf("unexpected request: %s/%s", requests[0].Service, requests[0].Method)
	}
}

func TestSealClient_Status_Sealed(t *testing.T) {
	transport := NewTransport()
	client := seal.NewClient(transport)

	transport.SetResponse("quicbaas.Seal", "Status", &seal.SealStatus{
		Sealed:      true,
		Initialized: true,
		Progress:    1,
		Required:    3,
	}, nil)

	ctx := context.Background()
	status, err := client.Status(ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !status.Sealed {
		t.Error("expected sealed status")
	}
	if status.Progress != 1 {
		t.Errorf("expected progress=1, got %d", status.Progress)
	}
}

func TestSealClient_Init_Success(t *testing.T) {
	transport := NewTransport()
	client := seal.NewClient(transport)

	expectedKeys := []string{"key1", "key2", "key3", "key4", "key5"}
	transport.SetResponse("quicbaas.Seal", "Init", &seal.InitResponse{
		Keys:      expectedKeys,
		RootToken: "root-token-12345",
	}, nil)

	ctx := context.Background()
	resp, err := client.Init(ctx, &seal.InitRequest{
		SecretShares:    5,
		SecretThreshold: 3,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(resp.Keys) != 5 {
		t.Errorf("expected 5 keys, got %d", len(resp.Keys))
	}
	if resp.RootToken != "root-token-12345" {
		t.Errorf("expected root token 'root-token-12345', got '%s'", resp.RootToken)
	}

	// Verify request payload
	req := transport.LastRequest()
	if req == nil {
		t.Fatal("expected a captured request")
	}
	initReq, ok := req.Payload.(*seal.InitRequest)
	if !ok {
		t.Fatal("expected InitRequest payload")
	}
	if initReq.SecretShares != 5 || initReq.SecretThreshold != 3 {
		t.Errorf("unexpected init params: shares=%d, threshold=%d", initReq.SecretShares, initReq.SecretThreshold)
	}
}

func TestSealClient_Init_InvalidThreshold(t *testing.T) {
	transport := NewTransport()
	client := seal.NewClient(transport)

	ctx := context.Background()
	_, err := client.Init(ctx, &seal.InitRequest{
		SecretShares:    3,
		SecretThreshold: 5, // threshold > shares
	})
	if err == nil {
		t.Fatal("expected error for invalid threshold")
	}
	if !errors.IsInvalidInput(err) {
		t.Errorf("expected invalid input error, got: %v", err)
	}
}

func TestSealClient_Init_ZeroShares(t *testing.T) {
	transport := NewTransport()
	client := seal.NewClient(transport)

	ctx := context.Background()
	_, err := client.Init(ctx, &seal.InitRequest{
		SecretShares:    0,
		SecretThreshold: 0,
	})
	if err == nil {
		t.Fatal("expected error for zero shares")
	}
	if !errors.IsInvalidInput(err) {
		t.Errorf("expected invalid input error, got: %v", err)
	}
}

func TestSealClient_Unseal_ProgressiveUnlock(t *testing.T) {
	transport := NewTransport()
	client := seal.NewClient(transport)

	ctx := context.Background()

	// First key - still sealed
	transport.SetResponse("quicbaas.Seal", "Unseal", &seal.UnsealResponse{
		Sealed:   true,
		Progress: 1,
		Required: 3,
	}, nil)

	resp, err := client.Unseal(ctx, &seal.UnsealRequest{Key: "key1"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !resp.Sealed {
		t.Error("expected still sealed after first key")
	}
	if resp.Progress != 1 {
		t.Errorf("expected progress=1, got %d", resp.Progress)
	}

	// Second key - still sealed
	transport.SetResponse("quicbaas.Seal", "Unseal", &seal.UnsealResponse{
		Sealed:   true,
		Progress: 2,
		Required: 3,
	}, nil)

	resp, err = client.Unseal(ctx, &seal.UnsealRequest{Key: "key2"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Progress != 2 {
		t.Errorf("expected progress=2, got %d", resp.Progress)
	}

	// Third key - now unsealed
	transport.SetResponse("quicbaas.Seal", "Unseal", &seal.UnsealResponse{
		Sealed:   false,
		Progress: 3,
		Required: 3,
	}, nil)

	resp, err = client.Unseal(ctx, &seal.UnsealRequest{Key: "key3"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Sealed {
		t.Error("expected unsealed after third key")
	}
}

func TestSealClient_Unseal_Reset(t *testing.T) {
	transport := NewTransport()
	client := seal.NewClient(transport)

	transport.SetResponse("quicbaas.Seal", "Unseal", &seal.UnsealResponse{
		Sealed:   true,
		Progress: 0,
		Required: 3,
	}, nil)

	ctx := context.Background()
	resp, err := client.Unseal(ctx, &seal.UnsealRequest{Reset: true})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Progress != 0 {
		t.Errorf("expected progress=0 after reset, got %d", resp.Progress)
	}
}

func TestSealClient_Unseal_EmptyKeyNoReset(t *testing.T) {
	transport := NewTransport()
	client := seal.NewClient(transport)

	ctx := context.Background()
	_, err := client.Unseal(ctx, &seal.UnsealRequest{Key: "", Reset: false})
	if err == nil {
		t.Fatal("expected error for empty key without reset")
	}
	if !errors.IsInvalidInput(err) {
		t.Errorf("expected invalid input error, got: %v", err)
	}
}

func TestSealClient_Seal_Success(t *testing.T) {
	transport := NewTransport()
	client := seal.NewClient(transport)

	transport.SetResponse("quicbaas.Seal", "Seal", nil, nil)

	ctx := context.Background()
	err := client.Seal(ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify request was sent
	requests := transport.Requests()
	if len(requests) != 1 {
		t.Fatalf("expected 1 request, got %d", len(requests))
	}
	if requests[0].Method != "Seal" {
		t.Errorf("expected Seal method, got %s", requests[0].Method)
	}
}

func TestSealClient_TransportError(t *testing.T) {
	transport := NewTransport()
	client := seal.NewClient(transport)

	transportErr := errors.NewConnection("test", "connection refused")
	transport.SetResponse("quicbaas.Seal", "Status", nil, transportErr)

	ctx := context.Background()
	_, err := client.Status(ctx)
	if err == nil {
		t.Fatal("expected error")
	}
	if errors.Code(err) != errors.CodeConnection {
		t.Errorf("expected connection error, got: %v", err)
	}
}

func TestSealClient_NilContext(t *testing.T) {
	transport := NewTransport()
	client := seal.NewClient(transport)

	var nilCtx context.Context
	_, err := client.Status(nilCtx)
	if err == nil {
		t.Fatal("expected error for nil context")
	}
	if !errors.IsInvalidInput(err) {
		t.Errorf("expected invalid input error, got: %v", err)
	}
}

// =============================================================================
// KMS Client Tests
// =============================================================================

func TestKMSClient_GenerateKey_AES256(t *testing.T) {
	transport := NewTransport()
	client := kms.NewClient(transport)

	transport.SetResponse("quicbaas.KMS", "GenerateKey", &kms.GenerateKeyResponse{
		KeyID:     "key-aes-123",
		Version:   1,
		Algorithm: kms.AlgorithmAES256GCM,
	}, nil)

	ctx := context.Background()
	resp, err := client.GenerateKey(ctx, &kms.GenerateKeyRequest{
		Name:      "my-aes-key",
		Algorithm: kms.AlgorithmAES256GCM,
		Usage:     []kms.KeyUsage{kms.KeyUsageEncrypt, kms.KeyUsageDecrypt},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if resp.KeyID != "key-aes-123" {
		t.Errorf("expected key ID 'key-aes-123', got '%s'", resp.KeyID)
	}
	if resp.Algorithm != kms.AlgorithmAES256GCM {
		t.Errorf("expected AES256GCM algorithm, got %v", resp.Algorithm)
	}
}

func TestKMSClient_GenerateKey_RSA(t *testing.T) {
	transport := NewTransport()
	client := kms.NewClient(transport)

	transport.SetResponse("quicbaas.KMS", "GenerateKey", &kms.GenerateKeyResponse{
		KeyID:     "key-rsa-456",
		Version:   1,
		Algorithm: kms.AlgorithmRSA4096,
	}, nil)

	ctx := context.Background()
	resp, err := client.GenerateKey(ctx, &kms.GenerateKeyRequest{
		Name:      "my-rsa-key",
		Algorithm: kms.AlgorithmRSA4096,
		Usage:     []kms.KeyUsage{kms.KeyUsageSign, kms.KeyUsageVerify},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if resp.Algorithm != kms.AlgorithmRSA4096 {
		t.Errorf("expected RSA4096 algorithm, got %v", resp.Algorithm)
	}
}

func TestKMSClient_GenerateKey_ECDSA(t *testing.T) {
	transport := NewTransport()
	client := kms.NewClient(transport)

	transport.SetResponse("quicbaas.KMS", "GenerateKey", &kms.GenerateKeyResponse{
		KeyID:     "key-ecdsa-789",
		Version:   1,
		Algorithm: kms.AlgorithmECDSAP256,
	}, nil)

	ctx := context.Background()
	resp, err := client.GenerateKey(ctx, &kms.GenerateKeyRequest{
		Name:      "my-ecdsa-key",
		Algorithm: kms.AlgorithmECDSAP256,
		Usage:     []kms.KeyUsage{kms.KeyUsageSign, kms.KeyUsageVerify},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if resp.Algorithm != kms.AlgorithmECDSAP256 {
		t.Errorf("expected ECDSAP256 algorithm, got %v", resp.Algorithm)
	}
}

func TestKMSClient_GenerateKey_ED25519(t *testing.T) {
	transport := NewTransport()
	client := kms.NewClient(transport)

	transport.SetResponse("quicbaas.KMS", "GenerateKey", &kms.GenerateKeyResponse{
		KeyID:     "key-ed25519-abc",
		Version:   1,
		Algorithm: kms.AlgorithmED25519,
	}, nil)

	ctx := context.Background()
	resp, err := client.GenerateKey(ctx, &kms.GenerateKeyRequest{
		Name:      "my-ed25519-key",
		Algorithm: kms.AlgorithmED25519,
		Usage:     []kms.KeyUsage{kms.KeyUsageSign, kms.KeyUsageVerify},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if resp.Algorithm != kms.AlgorithmED25519 {
		t.Errorf("expected ED25519 algorithm, got %v", resp.Algorithm)
	}
}

func TestKMSClient_GenerateKey_InvalidAlgorithm(t *testing.T) {
	transport := NewTransport()
	client := kms.NewClient(transport)

	ctx := context.Background()
	_, err := client.GenerateKey(ctx, &kms.GenerateKeyRequest{
		Name:      "invalid-key",
		Algorithm: kms.KeyAlgorithm(999), // Invalid algorithm
		Usage:     []kms.KeyUsage{kms.KeyUsageEncrypt},
	})
	if err == nil {
		t.Fatal("expected error for invalid algorithm")
	}
	if !errors.IsInvalidInput(err) {
		t.Errorf("expected invalid input error, got: %v", err)
	}
}

func TestKMSClient_GenerateKey_EmptyName(t *testing.T) {
	transport := NewTransport()
	client := kms.NewClient(transport)

	ctx := context.Background()
	_, err := client.GenerateKey(ctx, &kms.GenerateKeyRequest{
		Name:      "",
		Algorithm: kms.AlgorithmAES256GCM,
	})
	if err == nil {
		t.Fatal("expected error for empty name")
	}
	if !errors.IsInvalidInput(err) {
		t.Errorf("expected invalid input error, got: %v", err)
	}
}

func TestKMSClient_GetKey_Success(t *testing.T) {
	transport := NewTransport()
	client := kms.NewClient(transport)

	now := time.Now()
	transport.SetResponse("quicbaas.KMS", "GetKey", &kms.KeyInfo{
		KeyID:     "key-123",
		Name:      "my-test-key",
		Algorithm: kms.AlgorithmAES256GCM,
		Version:   2,
		Usage:     []kms.KeyUsage{kms.KeyUsageEncrypt, kms.KeyUsageDecrypt},
		CreatedAt: now.Add(-24 * time.Hour),
		RotatedAt: now.Add(-1 * time.Hour),
	}, nil)

	ctx := context.Background()
	info, err := client.GetKey(ctx, "key-123")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if info.KeyID != "key-123" {
		t.Errorf("expected key ID 'key-123', got '%s'", info.KeyID)
	}
	if info.Version != 2 {
		t.Errorf("expected version 2, got %d", info.Version)
	}
	if len(info.Usage) != 2 {
		t.Errorf("expected 2 usages, got %d", len(info.Usage))
	}
}

func TestKMSClient_GetKey_NotFound(t *testing.T) {
	transport := NewTransport()
	client := kms.NewClient(transport)

	transport.SetResponse("quicbaas.KMS", "GetKey", nil, errors.NewNotFound("kms", "key not found"))

	ctx := context.Background()
	_, err := client.GetKey(ctx, "nonexistent-key")
	if err == nil {
		t.Fatal("expected error for nonexistent key")
	}
	if !errors.IsNotFound(err) {
		t.Errorf("expected not found error, got: %v", err)
	}
}

func TestKMSClient_GetKey_EmptyKeyID(t *testing.T) {
	transport := NewTransport()
	client := kms.NewClient(transport)

	ctx := context.Background()
	_, err := client.GetKey(ctx, "")
	if err == nil {
		t.Fatal("expected error for empty key ID")
	}
	if !errors.IsInvalidInput(err) {
		t.Errorf("expected invalid input error, got: %v", err)
	}
}

func TestKMSClient_ListKeys_Success(t *testing.T) {
	transport := NewTransport()
	client := kms.NewClient(transport)

	transport.SetResponse("quicbaas.KMS", "ListKeys", &kms.ListKeysResponse{
		Keys: []kms.KeyInfo{
			{KeyID: "key-1", Name: "key-one", Algorithm: kms.AlgorithmAES256GCM},
			{KeyID: "key-2", Name: "key-two", Algorithm: kms.AlgorithmRSA4096},
			{KeyID: "key-3", Name: "key-three", Algorithm: kms.AlgorithmED25519},
		},
	}, nil)

	ctx := context.Background()
	resp, err := client.ListKeys(ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(resp.Keys) != 3 {
		t.Errorf("expected 3 keys, got %d", len(resp.Keys))
	}
}

func TestKMSClient_ListKeys_Empty(t *testing.T) {
	transport := NewTransport()
	client := kms.NewClient(transport)

	transport.SetResponse("quicbaas.KMS", "ListKeys", nil, nil)

	ctx := context.Background()
	resp, err := client.ListKeys(ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(resp.Keys) != 0 {
		t.Errorf("expected empty keys list, got %d", len(resp.Keys))
	}
}

func TestKMSClient_RotateKey_Success(t *testing.T) {
	transport := NewTransport()
	client := kms.NewClient(transport)

	transport.SetResponse("quicbaas.KMS", "RotateKey", &kms.RotateKeyResponse{
		KeyID:      "key-123",
		NewVersion: 3,
	}, nil)

	ctx := context.Background()
	resp, err := client.RotateKey(ctx, "key-123")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if resp.NewVersion != 3 {
		t.Errorf("expected new version 3, got %d", resp.NewVersion)
	}
}

func TestKMSClient_RotateKey_EmptyKeyID(t *testing.T) {
	transport := NewTransport()
	client := kms.NewClient(transport)

	ctx := context.Background()
	_, err := client.RotateKey(ctx, "")
	if err == nil {
		t.Fatal("expected error for empty key ID")
	}
	if !errors.IsInvalidInput(err) {
		t.Errorf("expected invalid input error, got: %v", err)
	}
}

func TestKMSClient_DeleteKey_Success(t *testing.T) {
	transport := NewTransport()
	client := kms.NewClient(transport)

	transport.SetResponse("quicbaas.KMS", "DeleteKey", nil, nil)

	ctx := context.Background()
	err := client.DeleteKey(ctx, "key-to-delete")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestKMSClient_DeleteKey_EmptyKeyID(t *testing.T) {
	transport := NewTransport()
	client := kms.NewClient(transport)

	ctx := context.Background()
	err := client.DeleteKey(ctx, "")
	if err == nil {
		t.Fatal("expected error for empty key ID")
	}
	if !errors.IsInvalidInput(err) {
		t.Errorf("expected invalid input error, got: %v", err)
	}
}

func TestKMSClient_TransportError(t *testing.T) {
	transport := NewTransport()
	client := kms.NewClient(transport)

	transportErr := errors.NewUnavailable("kms", "service unavailable")
	transport.SetResponse("quicbaas.KMS", "GenerateKey", nil, transportErr)

	ctx := context.Background()
	_, err := client.GenerateKey(ctx, &kms.GenerateKeyRequest{
		Name:      "test-key",
		Algorithm: kms.AlgorithmAES256GCM,
	})
	if err == nil {
		t.Fatal("expected error")
	}
	if !errors.IsUnavailable(err) {
		t.Errorf("expected unavailable error, got: %v", err)
	}
}

func TestKMSClient_NilContext(t *testing.T) {
	transport := NewTransport()
	client := kms.NewClient(transport)

	var nilCtx context.Context
	_, err := client.ListKeys(nilCtx)
	if err == nil {
		t.Fatal("expected error for nil context")
	}
	if !errors.IsInvalidInput(err) {
		t.Errorf("expected invalid input error, got: %v", err)
	}
}

// =============================================================================
// Secrets Client Tests
// =============================================================================

func TestSecretsClient_Store_Success(t *testing.T) {
	transport := NewTransport()
	client := secrets.NewClient(transport)

	transport.SetResponse("quicbaas.Secrets", "Store", &secrets.StoreSecretResponse{
		Version: 1,
	}, nil)

	ctx := context.Background()
	resp, err := client.Store(ctx, &secrets.StoreSecretRequest{
		Path: "secret/data/myapp/config",
		Data: map[string]interface{}{
			"username": "admin",
			"password": "s3cr3t",
		},
		Metadata: map[string]string{
			"owner": "myteam",
			"env":   "production",
		},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if resp.Version != 1 {
		t.Errorf("expected version 1, got %d", resp.Version)
	}

	// Verify request payload
	req := transport.LastRequest()
	storeReq, ok := req.Payload.(*secrets.StoreSecretRequest)
	if !ok {
		t.Fatal("expected StoreSecretRequest payload")
	}
	if storeReq.Path != "secret/data/myapp/config" {
		t.Errorf("unexpected path: %s", storeReq.Path)
	}
}

func TestSecretsClient_Store_WithUpdate(t *testing.T) {
	transport := NewTransport()
	client := secrets.NewClient(transport)

	transport.SetResponse("quicbaas.Secrets", "Store", &secrets.StoreSecretResponse{
		Version: 5, // Newer version indicates update
	}, nil)

	ctx := context.Background()
	resp, err := client.Store(ctx, &secrets.StoreSecretRequest{
		Path: "secret/data/existing",
		Data: map[string]interface{}{"key": "updated-value"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if resp.Version != 5 {
		t.Errorf("expected version 5, got %d", resp.Version)
	}
}

func TestSecretsClient_Store_EmptyPath(t *testing.T) {
	transport := NewTransport()
	client := secrets.NewClient(transport)

	ctx := context.Background()
	_, err := client.Store(ctx, &secrets.StoreSecretRequest{
		Path: "",
		Data: map[string]interface{}{"key": "value"},
	})
	if err == nil {
		t.Fatal("expected error for empty path")
	}
	if !errors.IsInvalidInput(err) {
		t.Errorf("expected invalid input error, got: %v", err)
	}
}

func TestSecretsClient_Store_EmptyData(t *testing.T) {
	transport := NewTransport()
	client := secrets.NewClient(transport)

	ctx := context.Background()
	_, err := client.Store(ctx, &secrets.StoreSecretRequest{
		Path: "secret/data/test",
		Data: map[string]interface{}{},
	})
	if err == nil {
		t.Fatal("expected error for empty data")
	}
	if !errors.IsInvalidInput(err) {
		t.Errorf("expected invalid input error, got: %v", err)
	}
}

func TestSecretsClient_Get_LatestVersion(t *testing.T) {
	transport := NewTransport()
	client := secrets.NewClient(transport)

	now := time.Now()
	transport.SetResponse("quicbaas.Secrets", "Get", &secrets.Secret{
		Path: "secret/data/myapp/config",
		Data: map[string]interface{}{
			"username": "admin",
			"password": "s3cr3t",
		},
		Metadata: map[string]string{
			"owner": "myteam",
		},
		Version:   3,
		CreatedAt: now,
	}, nil)

	ctx := context.Background()
	secret, err := client.Get(ctx, "secret/data/myapp/config")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if secret.Version != 3 {
		t.Errorf("expected version 3, got %d", secret.Version)
	}
	if secret.Data["username"] != "admin" {
		t.Errorf("unexpected username: %v", secret.Data["username"])
	}
}

func TestSecretsClient_Get_SpecificVersion(t *testing.T) {
	transport := NewTransport()
	client := secrets.NewClient(transport)

	transport.SetResponse("quicbaas.Secrets", "Get", &secrets.Secret{
		Path:    "secret/data/myapp/config",
		Data:    map[string]interface{}{"key": "old-value"},
		Version: 2,
	}, nil)

	ctx := context.Background()
	secret, err := client.Get(ctx, "secret/data/myapp/config", secrets.WithVersion(2))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if secret.Version != 2 {
		t.Errorf("expected version 2, got %d", secret.Version)
	}
}

func TestSecretsClient_Get_NotFound(t *testing.T) {
	transport := NewTransport()
	client := secrets.NewClient(transport)

	transport.SetResponse("quicbaas.Secrets", "Get", nil, errors.NewNotFound("secrets", "secret not found"))

	ctx := context.Background()
	_, err := client.Get(ctx, "secret/data/nonexistent")
	if err == nil {
		t.Fatal("expected error for nonexistent secret")
	}
	if !errors.IsNotFound(err) {
		t.Errorf("expected not found error, got: %v", err)
	}
}

func TestSecretsClient_Get_EmptyPath(t *testing.T) {
	transport := NewTransport()
	client := secrets.NewClient(transport)

	ctx := context.Background()
	_, err := client.Get(ctx, "")
	if err == nil {
		t.Fatal("expected error for empty path")
	}
	if !errors.IsInvalidInput(err) {
		t.Errorf("expected invalid input error, got: %v", err)
	}
}

func TestSecretsClient_List_Success(t *testing.T) {
	transport := NewTransport()
	client := secrets.NewClient(transport)

	transport.SetResponse("quicbaas.Secrets", "List", &secrets.ListSecretsResponse{
		Paths: []string{
			"secret/data/myapp/config",
			"secret/data/myapp/credentials",
			"secret/data/myapp/keys",
		},
	}, nil)

	ctx := context.Background()
	resp, err := client.List(ctx, "secret/data/myapp/")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(resp.Paths) != 3 {
		t.Errorf("expected 3 paths, got %d", len(resp.Paths))
	}
}

func TestSecretsClient_List_Empty(t *testing.T) {
	transport := NewTransport()
	client := secrets.NewClient(transport)

	transport.SetResponse("quicbaas.Secrets", "List", nil, nil)

	ctx := context.Background()
	resp, err := client.List(ctx, "secret/data/empty/")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(resp.Paths) != 0 {
		t.Errorf("expected empty paths list, got %d", len(resp.Paths))
	}
}

func TestSecretsClient_Delete_Success(t *testing.T) {
	transport := NewTransport()
	client := secrets.NewClient(transport)

	transport.SetResponse("quicbaas.Secrets", "Delete", nil, nil)

	ctx := context.Background()
	err := client.Delete(ctx, "secret/data/to-delete")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestSecretsClient_Delete_EmptyPath(t *testing.T) {
	transport := NewTransport()
	client := secrets.NewClient(transport)

	ctx := context.Background()
	err := client.Delete(ctx, "")
	if err == nil {
		t.Fatal("expected error for empty path")
	}
	if !errors.IsInvalidInput(err) {
		t.Errorf("expected invalid input error, got: %v", err)
	}
}

func TestSecretsClient_TransportError(t *testing.T) {
	transport := NewTransport()
	client := secrets.NewClient(transport)

	transportErr := errors.NewTimeout("secrets", "request timed out")
	transport.SetResponse("quicbaas.Secrets", "Get", nil, transportErr)

	ctx := context.Background()
	_, err := client.Get(ctx, "secret/data/test")
	if err == nil {
		t.Fatal("expected error")
	}
	if !errors.IsTimeout(err) {
		t.Errorf("expected timeout error, got: %v", err)
	}
}

func TestSecretsClient_NilContext(t *testing.T) {
	transport := NewTransport()
	client := secrets.NewClient(transport)

	var nilCtx context.Context
	_, err := client.List(nilCtx, "prefix")
	if err == nil {
		t.Fatal("expected error for nil context")
	}
	if !errors.IsInvalidInput(err) {
		t.Errorf("expected invalid input error, got: %v", err)
	}
}

// =============================================================================
// Crypto Client Tests
// =============================================================================

func TestCryptoClient_Encrypt_Success(t *testing.T) {
	transport := NewTransport()
	client := crypto.NewClient(transport)

	transport.SetResponse("quicbaas.Crypto", "Encrypt", &crypto.EncryptResponse{
		Ciphertext: []byte("encrypted-data-here"),
		KeyVersion: 1,
	}, nil)

	ctx := context.Background()
	resp, err := client.Encrypt(ctx, &crypto.EncryptRequest{
		KeyID:     "key-123",
		Plaintext: []byte("hello, world"),
		Context:   []byte("aad-context"),
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(resp.Ciphertext) == 0 {
		t.Error("expected non-empty ciphertext")
	}
	if resp.KeyVersion != 1 {
		t.Errorf("expected key version 1, got %d", resp.KeyVersion)
	}
}

func TestCryptoClient_Encrypt_WithoutContext(t *testing.T) {
	transport := NewTransport()
	client := crypto.NewClient(transport)

	transport.SetResponse("quicbaas.Crypto", "Encrypt", &crypto.EncryptResponse{
		Ciphertext: []byte("encrypted-without-aad"),
		KeyVersion: 1,
	}, nil)

	ctx := context.Background()
	resp, err := client.Encrypt(ctx, &crypto.EncryptRequest{
		KeyID:     "key-123",
		Plaintext: []byte("data"),
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(resp.Ciphertext) == 0 {
		t.Error("expected non-empty ciphertext")
	}
}

func TestCryptoClient_Encrypt_EmptyKeyID(t *testing.T) {
	transport := NewTransport()
	client := crypto.NewClient(transport)

	ctx := context.Background()
	_, err := client.Encrypt(ctx, &crypto.EncryptRequest{
		KeyID:     "",
		Plaintext: []byte("data"),
	})
	if err == nil {
		t.Fatal("expected error for empty key ID")
	}
	if !errors.IsInvalidInput(err) {
		t.Errorf("expected invalid input error, got: %v", err)
	}
}

func TestCryptoClient_Encrypt_EmptyPlaintext(t *testing.T) {
	transport := NewTransport()
	client := crypto.NewClient(transport)

	ctx := context.Background()
	_, err := client.Encrypt(ctx, &crypto.EncryptRequest{
		KeyID:     "key-123",
		Plaintext: []byte{},
	})
	if err == nil {
		t.Fatal("expected error for empty plaintext")
	}
	if !errors.IsInvalidInput(err) {
		t.Errorf("expected invalid input error, got: %v", err)
	}
}

func TestCryptoClient_Decrypt_Success(t *testing.T) {
	transport := NewTransport()
	client := crypto.NewClient(transport)

	transport.SetResponse("quicbaas.Crypto", "Decrypt", &crypto.DecryptResponse{
		Plaintext: []byte("decrypted-data"),
	}, nil)

	ctx := context.Background()
	resp, err := client.Decrypt(ctx, &crypto.DecryptRequest{
		KeyID:      "key-123",
		Ciphertext: []byte("encrypted-data"),
		Context:    []byte("aad-context"),
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if string(resp.Plaintext) != "decrypted-data" {
		t.Errorf("unexpected plaintext: %s", resp.Plaintext)
	}
}

func TestCryptoClient_Decrypt_EmptyKeyID(t *testing.T) {
	transport := NewTransport()
	client := crypto.NewClient(transport)

	ctx := context.Background()
	_, err := client.Decrypt(ctx, &crypto.DecryptRequest{
		KeyID:      "",
		Ciphertext: []byte("data"),
	})
	if err == nil {
		t.Fatal("expected error for empty key ID")
	}
	if !errors.IsInvalidInput(err) {
		t.Errorf("expected invalid input error, got: %v", err)
	}
}

func TestCryptoClient_Decrypt_EmptyCiphertext(t *testing.T) {
	transport := NewTransport()
	client := crypto.NewClient(transport)

	ctx := context.Background()
	_, err := client.Decrypt(ctx, &crypto.DecryptRequest{
		KeyID:      "key-123",
		Ciphertext: []byte{},
	})
	if err == nil {
		t.Fatal("expected error for empty ciphertext")
	}
	if !errors.IsInvalidInput(err) {
		t.Errorf("expected invalid input error, got: %v", err)
	}
}

func TestCryptoClient_Sign_SHA256(t *testing.T) {
	transport := NewTransport()
	client := crypto.NewClient(transport)

	transport.SetResponse("quicbaas.Crypto", "Sign", &crypto.SignResponse{
		Signature:  []byte("signature-bytes"),
		KeyVersion: 2,
	}, nil)

	ctx := context.Background()
	resp, err := client.Sign(ctx, &crypto.SignRequest{
		KeyID:   "signing-key",
		Data:    []byte("data to sign"),
		HashAlg: crypto.HashSHA256,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(resp.Signature) == 0 {
		t.Error("expected non-empty signature")
	}
	if resp.KeyVersion != 2 {
		t.Errorf("expected key version 2, got %d", resp.KeyVersion)
	}
}

func TestCryptoClient_Sign_SHA384(t *testing.T) {
	transport := NewTransport()
	client := crypto.NewClient(transport)

	transport.SetResponse("quicbaas.Crypto", "Sign", &crypto.SignResponse{
		Signature:  []byte("sha384-signature"),
		KeyVersion: 1,
	}, nil)

	ctx := context.Background()
	resp, err := client.Sign(ctx, &crypto.SignRequest{
		KeyID:   "signing-key",
		Data:    []byte("data"),
		HashAlg: crypto.HashSHA384,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(resp.Signature) == 0 {
		t.Error("expected non-empty signature")
	}
}

func TestCryptoClient_Sign_SHA512(t *testing.T) {
	transport := NewTransport()
	client := crypto.NewClient(transport)

	transport.SetResponse("quicbaas.Crypto", "Sign", &crypto.SignResponse{
		Signature:  []byte("sha512-signature"),
		KeyVersion: 1,
	}, nil)

	ctx := context.Background()
	resp, err := client.Sign(ctx, &crypto.SignRequest{
		KeyID:   "signing-key",
		Data:    []byte("data"),
		HashAlg: crypto.HashSHA512,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(resp.Signature) == 0 {
		t.Error("expected non-empty signature")
	}
}

func TestCryptoClient_Sign_InvalidHashAlgorithm(t *testing.T) {
	transport := NewTransport()
	client := crypto.NewClient(transport)

	ctx := context.Background()
	_, err := client.Sign(ctx, &crypto.SignRequest{
		KeyID:   "signing-key",
		Data:    []byte("data"),
		HashAlg: crypto.HashAlgorithm(999), // Invalid
	})
	if err == nil {
		t.Fatal("expected error for invalid hash algorithm")
	}
	if !errors.IsInvalidInput(err) {
		t.Errorf("expected invalid input error, got: %v", err)
	}
}

func TestCryptoClient_Sign_EmptyKeyID(t *testing.T) {
	transport := NewTransport()
	client := crypto.NewClient(transport)

	ctx := context.Background()
	_, err := client.Sign(ctx, &crypto.SignRequest{
		KeyID:   "",
		Data:    []byte("data"),
		HashAlg: crypto.HashSHA256,
	})
	if err == nil {
		t.Fatal("expected error for empty key ID")
	}
	if !errors.IsInvalidInput(err) {
		t.Errorf("expected invalid input error, got: %v", err)
	}
}

func TestCryptoClient_Sign_EmptyData(t *testing.T) {
	transport := NewTransport()
	client := crypto.NewClient(transport)

	ctx := context.Background()
	_, err := client.Sign(ctx, &crypto.SignRequest{
		KeyID:   "key",
		Data:    []byte{},
		HashAlg: crypto.HashSHA256,
	})
	if err == nil {
		t.Fatal("expected error for empty data")
	}
	if !errors.IsInvalidInput(err) {
		t.Errorf("expected invalid input error, got: %v", err)
	}
}

func TestCryptoClient_Verify_Valid(t *testing.T) {
	transport := NewTransport()
	client := crypto.NewClient(transport)

	transport.SetResponse("quicbaas.Crypto", "Verify", &crypto.VerifyResponse{
		Valid: true,
	}, nil)

	ctx := context.Background()
	resp, err := client.Verify(ctx, &crypto.VerifyRequest{
		KeyID:     "signing-key",
		Data:      []byte("original data"),
		Signature: []byte("valid-signature"),
		HashAlg:   crypto.HashSHA256,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !resp.Valid {
		t.Error("expected valid signature")
	}
}

func TestCryptoClient_Verify_Invalid(t *testing.T) {
	transport := NewTransport()
	client := crypto.NewClient(transport)

	transport.SetResponse("quicbaas.Crypto", "Verify", &crypto.VerifyResponse{
		Valid: false,
	}, nil)

	ctx := context.Background()
	resp, err := client.Verify(ctx, &crypto.VerifyRequest{
		KeyID:     "signing-key",
		Data:      []byte("tampered data"),
		Signature: []byte("original-signature"),
		HashAlg:   crypto.HashSHA256,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if resp.Valid {
		t.Error("expected invalid signature")
	}
}

func TestCryptoClient_Verify_EmptyKeyID(t *testing.T) {
	transport := NewTransport()
	client := crypto.NewClient(transport)

	ctx := context.Background()
	_, err := client.Verify(ctx, &crypto.VerifyRequest{
		KeyID:     "",
		Data:      []byte("data"),
		Signature: []byte("sig"),
		HashAlg:   crypto.HashSHA256,
	})
	if err == nil {
		t.Fatal("expected error for empty key ID")
	}
	if !errors.IsInvalidInput(err) {
		t.Errorf("expected invalid input error, got: %v", err)
	}
}

func TestCryptoClient_Verify_EmptyData(t *testing.T) {
	transport := NewTransport()
	client := crypto.NewClient(transport)

	ctx := context.Background()
	_, err := client.Verify(ctx, &crypto.VerifyRequest{
		KeyID:     "key",
		Data:      []byte{},
		Signature: []byte("sig"),
		HashAlg:   crypto.HashSHA256,
	})
	if err == nil {
		t.Fatal("expected error for empty data")
	}
	if !errors.IsInvalidInput(err) {
		t.Errorf("expected invalid input error, got: %v", err)
	}
}

func TestCryptoClient_Verify_EmptySignature(t *testing.T) {
	transport := NewTransport()
	client := crypto.NewClient(transport)

	ctx := context.Background()
	_, err := client.Verify(ctx, &crypto.VerifyRequest{
		KeyID:     "key",
		Data:      []byte("data"),
		Signature: []byte{},
		HashAlg:   crypto.HashSHA256,
	})
	if err == nil {
		t.Fatal("expected error for empty signature")
	}
	if !errors.IsInvalidInput(err) {
		t.Errorf("expected invalid input error, got: %v", err)
	}
}

func TestCryptoClient_Verify_InvalidHashAlgorithm(t *testing.T) {
	transport := NewTransport()
	client := crypto.NewClient(transport)

	ctx := context.Background()
	_, err := client.Verify(ctx, &crypto.VerifyRequest{
		KeyID:     "key",
		Data:      []byte("data"),
		Signature: []byte("sig"),
		HashAlg:   crypto.HashAlgorithm(999),
	})
	if err == nil {
		t.Fatal("expected error for invalid hash algorithm")
	}
	if !errors.IsInvalidInput(err) {
		t.Errorf("expected invalid input error, got: %v", err)
	}
}

func TestCryptoClient_TransportError(t *testing.T) {
	transport := NewTransport()
	client := crypto.NewClient(transport)

	transportErr := errors.NewInternal("crypto", "internal server error")
	transport.SetResponse("quicbaas.Crypto", "Encrypt", nil, transportErr)

	ctx := context.Background()
	_, err := client.Encrypt(ctx, &crypto.EncryptRequest{
		KeyID:     "key-123",
		Plaintext: []byte("data"),
	})
	if err == nil {
		t.Fatal("expected error")
	}
	if errors.Code(err) != errors.CodeInternal {
		t.Errorf("expected internal error, got: %v", err)
	}
}

func TestCryptoClient_NilContext(t *testing.T) {
	transport := NewTransport()
	client := crypto.NewClient(transport)

	var nilCtx context.Context
	_, err := client.Encrypt(nilCtx, &crypto.EncryptRequest{
		KeyID:     "key",
		Plaintext: []byte("data"),
	})
	if err == nil {
		t.Fatal("expected error for nil context")
	}
	if !errors.IsInvalidInput(err) {
		t.Errorf("expected invalid input error, got: %v", err)
	}
}

// =============================================================================
// Cross-Client Error Handling Tests
// =============================================================================

func TestAllClients_NilTransport(t *testing.T) {
	if seal.NewClient(nil) != nil {
		t.Error("seal.NewClient should return nil for nil transport")
	}
	if kms.NewClient(nil) != nil {
		t.Error("kms.NewClient should return nil for nil transport")
	}
	if secrets.NewClient(nil) != nil {
		t.Error("secrets.NewClient should return nil for nil transport")
	}
	if crypto.NewClient(nil) != nil {
		t.Error("crypto.NewClient should return nil for nil transport")
	}
}

func TestAllClients_ClosedTransport(t *testing.T) {
	testCases := []struct {
		name string
		test func(t *testing.T, transport *Transport)
	}{
		{
			name: "seal_status",
			test: func(t *testing.T, transport *Transport) {
				client := seal.NewClient(transport)
				_, err := client.Status(context.Background())
				if err == nil {
					t.Error("expected error for closed transport")
				}
			},
		},
		{
			name: "kms_list",
			test: func(t *testing.T, transport *Transport) {
				client := kms.NewClient(transport)
				_, err := client.ListKeys(context.Background())
				if err == nil {
					t.Error("expected error for closed transport")
				}
			},
		},
		{
			name: "secrets_list",
			test: func(t *testing.T, transport *Transport) {
				client := secrets.NewClient(transport)
				_, err := client.List(context.Background(), "prefix")
				if err == nil {
					t.Error("expected error for closed transport")
				}
			},
		},
		{
			name: "crypto_encrypt",
			test: func(t *testing.T, transport *Transport) {
				client := crypto.NewClient(transport)
				_, err := client.Encrypt(context.Background(), &crypto.EncryptRequest{
					KeyID:     "key",
					Plaintext: []byte("data"),
				})
				if err == nil {
					t.Error("expected error for closed transport")
				}
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			transport := NewTransport()
			if err := transport.Close(); err != nil {
				t.Fatalf("failed to close transport: %v", err)
			}
			tc.test(t, transport)
		})
	}
}

func TestAllClients_CanceledContext(t *testing.T) {
	testCases := []struct {
		name string
		test func(t *testing.T, ctx context.Context, transport *Transport)
	}{
		{
			name: "seal_status",
			test: func(t *testing.T, ctx context.Context, transport *Transport) {
				client := seal.NewClient(transport)
				_, err := client.Status(ctx)
				if err == nil {
					t.Error("expected error for canceled context")
				}
			},
		},
		{
			name: "kms_generate",
			test: func(t *testing.T, ctx context.Context, transport *Transport) {
				client := kms.NewClient(transport)
				_, err := client.GenerateKey(ctx, &kms.GenerateKeyRequest{
					Name:      "test",
					Algorithm: kms.AlgorithmAES256GCM,
				})
				if err == nil {
					t.Error("expected error for canceled context")
				}
			},
		},
		{
			name: "secrets_store",
			test: func(t *testing.T, ctx context.Context, transport *Transport) {
				client := secrets.NewClient(transport)
				_, err := client.Store(ctx, &secrets.StoreSecretRequest{
					Path: "test",
					Data: map[string]interface{}{"key": "value"},
				})
				if err == nil {
					t.Error("expected error for canceled context")
				}
			},
		},
		{
			name: "crypto_sign",
			test: func(t *testing.T, ctx context.Context, transport *Transport) {
				client := crypto.NewClient(transport)
				_, err := client.Sign(ctx, &crypto.SignRequest{
					KeyID:   "key",
					Data:    []byte("data"),
					HashAlg: crypto.HashSHA256,
				})
				if err == nil {
					t.Error("expected error for canceled context")
				}
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			transport := NewTransport()
			ctx, cancel := context.WithCancel(context.Background())
			cancel() // Cancel immediately
			tc.test(t, ctx, transport)
		})
	}
}

// =============================================================================
// Request Verification Tests
// =============================================================================

func TestSealClient_RequestCapture(t *testing.T) {
	transport := NewTransport()
	client := seal.NewClient(transport)

	transport.SetResponse("quicbaas.Seal", "Init", &seal.InitResponse{
		Keys:      []string{"k1", "k2", "k3"},
		RootToken: "token",
	}, nil)

	ctx := context.Background()
	_, err := client.Init(ctx, &seal.InitRequest{
		SecretShares:    3,
		SecretThreshold: 2,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify request was captured correctly
	if transport.RequestCount() != 1 {
		t.Errorf("expected 1 request, got %d", transport.RequestCount())
	}

	req := transport.LastRequest()
	if req.Service != "quicbaas.Seal" {
		t.Errorf("expected service 'quicbaas.Seal', got '%s'", req.Service)
	}
	if req.Method != "Init" {
		t.Errorf("expected method 'Init', got '%s'", req.Method)
	}
}

func TestKMSClient_RequestCapture(t *testing.T) {
	transport := NewTransport()
	client := kms.NewClient(transport)

	transport.SetResponse("quicbaas.KMS", "GenerateKey", &kms.GenerateKeyResponse{
		KeyID:     "new-key",
		Version:   1,
		Algorithm: kms.AlgorithmED25519,
	}, nil)

	ctx := context.Background()
	_, err := client.GenerateKey(ctx, &kms.GenerateKeyRequest{
		Name:      "test-key",
		Algorithm: kms.AlgorithmED25519,
		Usage:     []kms.KeyUsage{kms.KeyUsageSign},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	req := transport.LastRequest()
	genReq, ok := req.Payload.(*kms.GenerateKeyRequest)
	if !ok {
		t.Fatal("expected GenerateKeyRequest payload")
	}
	if genReq.Algorithm != kms.AlgorithmED25519 {
		t.Errorf("expected ED25519 algorithm, got %v", genReq.Algorithm)
	}
}

func TestSecretsClient_RequestCapture(t *testing.T) {
	transport := NewTransport()
	client := secrets.NewClient(transport)

	transport.SetResponse("quicbaas.Secrets", "Store", &secrets.StoreSecretResponse{
		Version: 1,
	}, nil)

	ctx := context.Background()
	_, err := client.Store(ctx, &secrets.StoreSecretRequest{
		Path: "secret/test",
		Data: map[string]interface{}{
			"api_key": "secret-value",
		},
		Metadata: map[string]string{
			"env": "test",
		},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	req := transport.LastRequest()
	storeReq, ok := req.Payload.(*secrets.StoreSecretRequest)
	if !ok {
		t.Fatal("expected StoreSecretRequest payload")
	}
	if storeReq.Data["api_key"] != "secret-value" {
		t.Error("expected api_key in request data")
	}
}

func TestCryptoClient_RequestCapture(t *testing.T) {
	transport := NewTransport()
	client := crypto.NewClient(transport)

	transport.SetResponse("quicbaas.Crypto", "Encrypt", &crypto.EncryptResponse{
		Ciphertext: []byte("encrypted"),
		KeyVersion: 1,
	}, nil)

	ctx := context.Background()
	plaintext := []byte("sensitive data")
	aadContext := []byte("additional context")

	_, err := client.Encrypt(ctx, &crypto.EncryptRequest{
		KeyID:     "encryption-key",
		Plaintext: plaintext,
		Context:   aadContext,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	req := transport.LastRequest()
	encReq, ok := req.Payload.(*service.EncryptRequest)
	if !ok {
		t.Fatal("expected EncryptRequest payload")
	}
	if string(encReq.Plaintext) != string(plaintext) {
		t.Error("plaintext mismatch in captured request")
	}
	if string(encReq.Context) != string(aadContext) {
		t.Error("context mismatch in captured request")
	}
}

// =============================================================================
// Transport Reset Tests
// =============================================================================

func TestSecurityClients_TransportReset(t *testing.T) {
	transport := NewTransport()
	client := kms.NewClient(transport)

	// Configure first response
	transport.SetResponse("quicbaas.KMS", "GenerateKey", &kms.GenerateKeyResponse{
		KeyID: "first-key",
	}, nil)

	ctx := context.Background()
	resp, err := client.GenerateKey(ctx, &kms.GenerateKeyRequest{
		Name:      "test",
		Algorithm: kms.AlgorithmAES256GCM,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.KeyID != "first-key" {
		t.Errorf("expected 'first-key', got '%s'", resp.KeyID)
	}

	// Reset transport
	transport.Reset()

	// Configure new response
	transport.SetResponse("quicbaas.KMS", "GenerateKey", &kms.GenerateKeyResponse{
		KeyID: "second-key",
	}, nil)

	resp, err = client.GenerateKey(ctx, &kms.GenerateKeyRequest{
		Name:      "test2",
		Algorithm: kms.AlgorithmAES256GCM,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.KeyID != "second-key" {
		t.Errorf("expected 'second-key', got '%s'", resp.KeyID)
	}

	// Verify request count is reset
	if transport.RequestCount() != 1 {
		t.Errorf("expected 1 request after reset, got %d", transport.RequestCount())
	}
}

func TestSecurityClients_MultipleRequests(t *testing.T) {
	transport := NewTransport()
	sealClient := seal.NewClient(transport)
	kmsClient := kms.NewClient(transport)
	secretsClient := secrets.NewClient(transport)

	transport.SetResponse("quicbaas.Seal", "Status", &seal.SealStatus{Sealed: false}, nil)
	transport.SetResponse("quicbaas.KMS", "ListKeys", &kms.ListKeysResponse{Keys: []kms.KeyInfo{}}, nil)
	transport.SetResponse("quicbaas.Secrets", "List", &secrets.ListSecretsResponse{Paths: []string{}}, nil)

	ctx := context.Background()

	_, err := sealClient.Status(ctx)
	if err != nil {
		t.Fatalf("seal status error: %v", err)
	}

	_, err = kmsClient.ListKeys(ctx)
	if err != nil {
		t.Fatalf("kms list error: %v", err)
	}

	_, err = secretsClient.List(ctx, "")
	if err != nil {
		t.Fatalf("secrets list error: %v", err)
	}

	// Verify all requests were captured
	requests := transport.Requests()
	if len(requests) != 3 {
		t.Errorf("expected 3 requests, got %d", len(requests))
	}

	// Verify request order
	expectedServices := []string{"quicbaas.Seal", "quicbaas.KMS", "quicbaas.Secrets"}
	for i, req := range requests {
		if req.Service != expectedServices[i] {
			t.Errorf("request %d: expected service '%s', got '%s'", i, expectedServices[i], req.Service)
		}
	}
}
