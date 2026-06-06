// Copyright (c) 2025 Jeremy Hahn
// Copyright (c) 2025 Automate The Things, LLC

package crypto

import (
	"context"
	"testing"

	svc "github.com/jeremyhahn/go-quicbaas/pkg/service"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/errors"
)

// errTransport returns a fixed transport error for every request.
func errTransport() *mockTransport {
	return &mockTransport{
		requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
			return nil, errors.NewUnavailable("test", "server down")
		},
	}
}

// nilTransport returns a nil response with no error for every request.
func nilTransport() *mockTransport {
	return &mockTransport{
		requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
			return nil, nil
		},
	}
}

// badTypeTransport returns a response that is not a recognized type.
func badTypeTransport() *mockTransport {
	return &mockTransport{
		requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
			return "unexpected", nil
		},
	}
}

// respTransport returns a fixed response (with method assertion) for every request.
func respTransport(t *testing.T, wantMethod string, resp interface{}) *mockTransport {
	return &mockTransport{
		requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
			if service != serviceName {
				t.Errorf("expected service %q, got %q", serviceName, service)
			}
			if method != wantMethod {
				t.Errorf("expected method %q, got %q", wantMethod, method)
			}
			return resp, nil
		},
	}
}

// =============================================================================
// Rewrap
// =============================================================================

func TestClient_Rewrap_Validation(t *testing.T) {
	client := NewClient(respTransport(t, "Rewrap", &RewrapResponse{KeyID: "key-1"}))
	ctx := context.Background()

	tests := []struct {
		name    string
		ctx     context.Context
		req     *RewrapRequest
		wantErr bool
	}{
		{name: "nil request", ctx: ctx, req: nil, wantErr: true},
		{name: "empty key ID", ctx: ctx, req: &RewrapRequest{Ciphertext: []byte("ct")}, wantErr: true},
		{name: "empty ciphertext", ctx: ctx, req: &RewrapRequest{KeyID: "key-1"}, wantErr: true},
		{name: "nil context", ctx: nil, req: &RewrapRequest{KeyID: "key-1", Ciphertext: []byte("ct")}, wantErr: true},
		{name: "valid", ctx: ctx, req: &RewrapRequest{KeyID: "key-1", Ciphertext: []byte("ct")}, wantErr: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := client.Rewrap(tt.ctx, tt.req)
			if (err != nil) != tt.wantErr {
				t.Errorf("Rewrap() error = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.wantErr && err != nil && !errors.IsInvalidInput(err) {
				t.Errorf("expected InvalidInput error, got %v", err)
			}
		})
	}
}

func TestClient_Rewrap(t *testing.T) {
	ctx := context.Background()
	validReq := &RewrapRequest{KeyID: "key-1", Ciphertext: []byte("ct"), Context: []byte("aad"), Nonce: []byte("nonce")}

	t.Run("sdk response type", func(t *testing.T) {
		client := NewClient(respTransport(t, "Rewrap", &RewrapResponse{KeyID: "key-1", NewKeyVersion: 2, Ciphertext: []byte("new")}))
		resp, err := client.Rewrap(ctx, validReq)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if resp.NewKeyVersion != 2 || string(resp.Ciphertext) != "new" {
			t.Errorf("unexpected response: %+v", resp)
		}
	})

	t.Run("service response type", func(t *testing.T) {
		client := NewClient(respTransport(t, "Rewrap", &svc.RewrapResponse{
			KeyID:         "key-1",
			OldKeyVersion: 1,
			NewKeyVersion: 2,
			Ciphertext:    []byte("new"),
			Nonce:         []byte("nonce2"),
		}))
		resp, err := client.Rewrap(ctx, validReq)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if resp.OldKeyVersion != 1 || resp.NewKeyVersion != 2 || string(resp.Nonce) != "nonce2" {
			t.Errorf("unexpected converted response: %+v", resp)
		}
	})

	t.Run("transport error", func(t *testing.T) {
		client := NewClient(errTransport())
		_, err := client.Rewrap(ctx, validReq)
		if errors.Code(err) != errors.CodeUnavailable {
			t.Errorf("expected Unavailable, got %v", err)
		}
	})

	t.Run("nil response", func(t *testing.T) {
		client := NewClient(nilTransport())
		_, err := client.Rewrap(ctx, validReq)
		if errors.Code(err) != errors.CodeInternal {
			t.Errorf("expected Internal, got %v", err)
		}
	})

	t.Run("invalid response type", func(t *testing.T) {
		client := NewClient(badTypeTransport())
		_, err := client.Rewrap(ctx, validReq)
		if errors.Code(err) != errors.CodeInternal {
			t.Errorf("expected Internal, got %v", err)
		}
	})
}

// =============================================================================
// EncryptBatch
// =============================================================================

func TestClient_EncryptBatch_Validation(t *testing.T) {
	client := NewClient(respTransport(t, "EncryptBatch", &EncryptBatchResponse{KeyID: "key-1"}))
	ctx := context.Background()
	items := []BatchEncryptItem{{Plaintext: []byte("a")}}

	tests := []struct {
		name    string
		ctx     context.Context
		req     *EncryptBatchRequest
		wantErr bool
	}{
		{name: "nil request", ctx: ctx, req: nil, wantErr: true},
		{name: "empty key ID", ctx: ctx, req: &EncryptBatchRequest{Items: items}, wantErr: true},
		{name: "empty items", ctx: ctx, req: &EncryptBatchRequest{KeyID: "key-1"}, wantErr: true},
		{name: "nil context", ctx: nil, req: &EncryptBatchRequest{KeyID: "key-1", Items: items}, wantErr: true},
		{name: "valid", ctx: ctx, req: &EncryptBatchRequest{KeyID: "key-1", Items: items}, wantErr: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := client.EncryptBatch(tt.ctx, tt.req)
			if (err != nil) != tt.wantErr {
				t.Errorf("EncryptBatch() error = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.wantErr && err != nil && !errors.IsInvalidInput(err) {
				t.Errorf("expected InvalidInput error, got %v", err)
			}
		})
	}
}

func TestClient_EncryptBatch(t *testing.T) {
	ctx := context.Background()
	validReq := &EncryptBatchRequest{
		KeyID: "key-1",
		Items: []BatchEncryptItem{
			{Plaintext: []byte("a"), Context: []byte("aad")},
			{Plaintext: []byte("b")},
		},
	}

	t.Run("sdk response type", func(t *testing.T) {
		client := NewClient(respTransport(t, "EncryptBatch", &EncryptBatchResponse{
			KeyID:      "key-1",
			KeyVersion: 1,
			Results:    []BatchEncryptResult{{Ciphertext: []byte("ct")}},
		}))
		resp, err := client.EncryptBatch(ctx, validReq)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(resp.Results) != 1 {
			t.Errorf("expected 1 result, got %d", len(resp.Results))
		}
	})

	t.Run("service response type", func(t *testing.T) {
		client := NewClient(respTransport(t, "EncryptBatch", &svc.EncryptBatchResponse{
			KeyID:      "key-1",
			KeyVersion: 3,
			Results: []svc.BatchEncryptResult{
				{Ciphertext: []byte("ct1"), Nonce: []byte("n1")},
				{Error: "boom"},
			},
		}))
		resp, err := client.EncryptBatch(ctx, validReq)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if resp.KeyVersion != 3 || len(resp.Results) != 2 {
			t.Errorf("unexpected converted response: %+v", resp)
		}
		if string(resp.Results[0].Nonce) != "n1" || resp.Results[1].Error != "boom" {
			t.Errorf("unexpected result conversion: %+v", resp.Results)
		}
	})

	t.Run("transport error", func(t *testing.T) {
		client := NewClient(errTransport())
		_, err := client.EncryptBatch(ctx, validReq)
		if errors.Code(err) != errors.CodeUnavailable {
			t.Errorf("expected Unavailable, got %v", err)
		}
	})

	t.Run("nil response", func(t *testing.T) {
		client := NewClient(nilTransport())
		_, err := client.EncryptBatch(ctx, validReq)
		if errors.Code(err) != errors.CodeInternal {
			t.Errorf("expected Internal, got %v", err)
		}
	})

	t.Run("invalid response type", func(t *testing.T) {
		client := NewClient(badTypeTransport())
		_, err := client.EncryptBatch(ctx, validReq)
		if errors.Code(err) != errors.CodeInternal {
			t.Errorf("expected Internal, got %v", err)
		}
	})
}

// =============================================================================
// DecryptBatch
// =============================================================================

func TestClient_DecryptBatch_Validation(t *testing.T) {
	client := NewClient(respTransport(t, "DecryptBatch", &DecryptBatchResponse{KeyID: "key-1"}))
	ctx := context.Background()
	items := []BatchDecryptItem{{Ciphertext: []byte("a")}}

	tests := []struct {
		name    string
		ctx     context.Context
		req     *DecryptBatchRequest
		wantErr bool
	}{
		{name: "nil request", ctx: ctx, req: nil, wantErr: true},
		{name: "empty key ID", ctx: ctx, req: &DecryptBatchRequest{Items: items}, wantErr: true},
		{name: "empty items", ctx: ctx, req: &DecryptBatchRequest{KeyID: "key-1"}, wantErr: true},
		{name: "nil context", ctx: nil, req: &DecryptBatchRequest{KeyID: "key-1", Items: items}, wantErr: true},
		{name: "valid", ctx: ctx, req: &DecryptBatchRequest{KeyID: "key-1", Items: items}, wantErr: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := client.DecryptBatch(tt.ctx, tt.req)
			if (err != nil) != tt.wantErr {
				t.Errorf("DecryptBatch() error = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.wantErr && err != nil && !errors.IsInvalidInput(err) {
				t.Errorf("expected InvalidInput error, got %v", err)
			}
		})
	}
}

func TestClient_DecryptBatch(t *testing.T) {
	ctx := context.Background()
	validReq := &DecryptBatchRequest{
		KeyID: "key-1",
		Items: []BatchDecryptItem{
			{Ciphertext: []byte("a"), Context: []byte("aad"), Nonce: []byte("n")},
			{Ciphertext: []byte("b")},
		},
	}

	t.Run("sdk response type", func(t *testing.T) {
		client := NewClient(respTransport(t, "DecryptBatch", &DecryptBatchResponse{
			KeyID:   "key-1",
			Results: []BatchDecryptResult{{Plaintext: []byte("pt")}},
		}))
		resp, err := client.DecryptBatch(ctx, validReq)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(resp.Results) != 1 {
			t.Errorf("expected 1 result, got %d", len(resp.Results))
		}
	})

	t.Run("service response type", func(t *testing.T) {
		client := NewClient(respTransport(t, "DecryptBatch", &svc.DecryptBatchResponse{
			KeyID: "key-1",
			Results: []svc.BatchDecryptResult{
				{Plaintext: []byte("pt1")},
				{Error: "boom"},
			},
		}))
		resp, err := client.DecryptBatch(ctx, validReq)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(resp.Results) != 2 || string(resp.Results[0].Plaintext) != "pt1" || resp.Results[1].Error != "boom" {
			t.Errorf("unexpected converted response: %+v", resp.Results)
		}
	})

	t.Run("transport error", func(t *testing.T) {
		client := NewClient(errTransport())
		_, err := client.DecryptBatch(ctx, validReq)
		if errors.Code(err) != errors.CodeUnavailable {
			t.Errorf("expected Unavailable, got %v", err)
		}
	})

	t.Run("nil response", func(t *testing.T) {
		client := NewClient(nilTransport())
		_, err := client.DecryptBatch(ctx, validReq)
		if errors.Code(err) != errors.CodeInternal {
			t.Errorf("expected Internal, got %v", err)
		}
	})

	t.Run("invalid response type", func(t *testing.T) {
		client := NewClient(badTypeTransport())
		_, err := client.DecryptBatch(ctx, validReq)
		if errors.Code(err) != errors.CodeInternal {
			t.Errorf("expected Internal, got %v", err)
		}
	})
}

// =============================================================================
// GenerateDataKey
// =============================================================================

func TestClient_GenerateDataKey_Validation(t *testing.T) {
	client := NewClient(respTransport(t, "GenerateDataKey", &GenerateDataKeyResponse{KeyID: "key-1"}))
	ctx := context.Background()

	tests := []struct {
		name    string
		ctx     context.Context
		req     *GenerateDataKeyRequest
		wantErr bool
	}{
		{name: "nil request", ctx: ctx, req: nil, wantErr: true},
		{name: "empty key ID", ctx: ctx, req: &GenerateDataKeyRequest{KeySize: 256}, wantErr: true},
		{name: "nil context", ctx: nil, req: &GenerateDataKeyRequest{KeyID: "key-1"}, wantErr: true},
		{name: "valid", ctx: ctx, req: &GenerateDataKeyRequest{KeyID: "key-1", KeySize: 256, Context: []byte("aad")}, wantErr: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := client.GenerateDataKey(tt.ctx, tt.req)
			if (err != nil) != tt.wantErr {
				t.Errorf("GenerateDataKey() error = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.wantErr && err != nil && !errors.IsInvalidInput(err) {
				t.Errorf("expected InvalidInput error, got %v", err)
			}
		})
	}
}

func TestClient_GenerateDataKey(t *testing.T) {
	ctx := context.Background()
	validReq := &GenerateDataKeyRequest{KeyID: "key-1", KeySize: 256}

	t.Run("sdk response type", func(t *testing.T) {
		client := NewClient(respTransport(t, "GenerateDataKey", &GenerateDataKeyResponse{
			KeyID: "key-1", Plaintext: []byte("pt"), CiphertextBlob: []byte("blob"),
		}))
		resp, err := client.GenerateDataKey(ctx, validReq)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if string(resp.Plaintext) != "pt" || string(resp.CiphertextBlob) != "blob" {
			t.Errorf("unexpected response: %+v", resp)
		}
	})

	t.Run("service response type", func(t *testing.T) {
		client := NewClient(respTransport(t, "GenerateDataKey", &svc.GenerateDataKeyResponse{
			KeyID: "key-1", Plaintext: []byte("pt"), CiphertextBlob: []byte("blob"),
		}))
		resp, err := client.GenerateDataKey(ctx, validReq)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if resp.KeyID != "key-1" || string(resp.CiphertextBlob) != "blob" {
			t.Errorf("unexpected converted response: %+v", resp)
		}
	})

	t.Run("transport error", func(t *testing.T) {
		client := NewClient(errTransport())
		_, err := client.GenerateDataKey(ctx, validReq)
		if errors.Code(err) != errors.CodeUnavailable {
			t.Errorf("expected Unavailable, got %v", err)
		}
	})

	t.Run("nil response", func(t *testing.T) {
		client := NewClient(nilTransport())
		_, err := client.GenerateDataKey(ctx, validReq)
		if errors.Code(err) != errors.CodeInternal {
			t.Errorf("expected Internal, got %v", err)
		}
	})

	t.Run("invalid response type", func(t *testing.T) {
		client := NewClient(badTypeTransport())
		_, err := client.GenerateDataKey(ctx, validReq)
		if errors.Code(err) != errors.CodeInternal {
			t.Errorf("expected Internal, got %v", err)
		}
	})
}

// =============================================================================
// GenerateWrappedDataKey
// =============================================================================

func TestClient_GenerateWrappedDataKey_Validation(t *testing.T) {
	client := NewClient(respTransport(t, "GenerateWrappedDataKey", &GenerateWrappedDataKeyResponse{KeyID: "key-1"}))
	ctx := context.Background()

	tests := []struct {
		name    string
		ctx     context.Context
		req     *GenerateWrappedDataKeyRequest
		wantErr bool
	}{
		{name: "nil request", ctx: ctx, req: nil, wantErr: true},
		{name: "empty key ID", ctx: ctx, req: &GenerateWrappedDataKeyRequest{KeySize: 256}, wantErr: true},
		{name: "nil context", ctx: nil, req: &GenerateWrappedDataKeyRequest{KeyID: "key-1"}, wantErr: true},
		{name: "valid", ctx: ctx, req: &GenerateWrappedDataKeyRequest{KeyID: "key-1", KeySize: 256, Context: []byte("aad")}, wantErr: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := client.GenerateWrappedDataKey(tt.ctx, tt.req)
			if (err != nil) != tt.wantErr {
				t.Errorf("GenerateWrappedDataKey() error = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.wantErr && err != nil && !errors.IsInvalidInput(err) {
				t.Errorf("expected InvalidInput error, got %v", err)
			}
		})
	}
}

func TestClient_GenerateWrappedDataKey(t *testing.T) {
	ctx := context.Background()
	validReq := &GenerateWrappedDataKeyRequest{KeyID: "key-1", KeySize: 256}

	t.Run("sdk response type", func(t *testing.T) {
		client := NewClient(respTransport(t, "GenerateWrappedDataKey", &GenerateWrappedDataKeyResponse{
			KeyID: "key-1", CiphertextBlob: []byte("blob"),
		}))
		resp, err := client.GenerateWrappedDataKey(ctx, validReq)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if string(resp.CiphertextBlob) != "blob" {
			t.Errorf("unexpected response: %+v", resp)
		}
	})

	t.Run("service response type", func(t *testing.T) {
		client := NewClient(respTransport(t, "GenerateWrappedDataKey", &svc.GenerateWrappedDataKeyResponse{
			KeyID: "key-1", CiphertextBlob: []byte("blob"),
		}))
		resp, err := client.GenerateWrappedDataKey(ctx, validReq)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if resp.KeyID != "key-1" || string(resp.CiphertextBlob) != "blob" {
			t.Errorf("unexpected converted response: %+v", resp)
		}
	})

	t.Run("transport error", func(t *testing.T) {
		client := NewClient(errTransport())
		_, err := client.GenerateWrappedDataKey(ctx, validReq)
		if errors.Code(err) != errors.CodeUnavailable {
			t.Errorf("expected Unavailable, got %v", err)
		}
	})

	t.Run("nil response", func(t *testing.T) {
		client := NewClient(nilTransport())
		_, err := client.GenerateWrappedDataKey(ctx, validReq)
		if errors.Code(err) != errors.CodeInternal {
			t.Errorf("expected Internal, got %v", err)
		}
	})

	t.Run("invalid response type", func(t *testing.T) {
		client := NewClient(badTypeTransport())
		_, err := client.GenerateWrappedDataKey(ctx, validReq)
		if errors.Code(err) != errors.CodeInternal {
			t.Errorf("expected Internal, got %v", err)
		}
	})
}

// =============================================================================
// GenerateHMAC
// =============================================================================

func TestClient_GenerateHMAC_Validation(t *testing.T) {
	client := NewClient(respTransport(t, "GenerateHMAC", &GenerateHMACResponse{KeyID: "key-1"}))
	ctx := context.Background()

	tests := []struct {
		name    string
		ctx     context.Context
		req     *GenerateHMACRequest
		wantErr bool
	}{
		{name: "nil request", ctx: ctx, req: nil, wantErr: true},
		{name: "empty key ID", ctx: ctx, req: &GenerateHMACRequest{Message: []byte("m")}, wantErr: true},
		{name: "empty message", ctx: ctx, req: &GenerateHMACRequest{KeyID: "key-1"}, wantErr: true},
		{name: "nil context", ctx: nil, req: &GenerateHMACRequest{KeyID: "key-1", Message: []byte("m")}, wantErr: true},
		{name: "valid", ctx: ctx, req: &GenerateHMACRequest{KeyID: "key-1", Message: []byte("m"), HashAlg: HashSHA256}, wantErr: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := client.GenerateHMAC(tt.ctx, tt.req)
			if (err != nil) != tt.wantErr {
				t.Errorf("GenerateHMAC() error = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.wantErr && err != nil && !errors.IsInvalidInput(err) {
				t.Errorf("expected InvalidInput error, got %v", err)
			}
		})
	}
}

func TestClient_GenerateHMAC(t *testing.T) {
	ctx := context.Background()
	validReq := &GenerateHMACRequest{KeyID: "key-1", Message: []byte("m"), HashAlg: HashSHA384}

	t.Run("sdk response type", func(t *testing.T) {
		client := NewClient(respTransport(t, "GenerateHMAC", &GenerateHMACResponse{
			KeyID: "key-1", KeyVersion: 1, HMAC: []byte("mac"), HashAlg: HashSHA384,
		}))
		resp, err := client.GenerateHMAC(ctx, validReq)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if string(resp.HMAC) != "mac" {
			t.Errorf("unexpected response: %+v", resp)
		}
	})

	t.Run("service response type", func(t *testing.T) {
		client := NewClient(respTransport(t, "GenerateHMAC", &svc.HMACResponse{
			KeyID: "key-1", KeyVersion: 2, HMAC: []byte("mac"), HashAlgorithm: svc.HashAlgSHA512,
		}))
		resp, err := client.GenerateHMAC(ctx, validReq)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if resp.KeyVersion != 2 || resp.HashAlg != HashSHA512 {
			t.Errorf("unexpected converted response: %+v", resp)
		}
	})

	t.Run("transport error", func(t *testing.T) {
		client := NewClient(errTransport())
		_, err := client.GenerateHMAC(ctx, validReq)
		if errors.Code(err) != errors.CodeUnavailable {
			t.Errorf("expected Unavailable, got %v", err)
		}
	})

	t.Run("nil response", func(t *testing.T) {
		client := NewClient(nilTransport())
		_, err := client.GenerateHMAC(ctx, validReq)
		if errors.Code(err) != errors.CodeInternal {
			t.Errorf("expected Internal, got %v", err)
		}
	})

	t.Run("invalid response type", func(t *testing.T) {
		client := NewClient(badTypeTransport())
		_, err := client.GenerateHMAC(ctx, validReq)
		if errors.Code(err) != errors.CodeInternal {
			t.Errorf("expected Internal, got %v", err)
		}
	})
}

// =============================================================================
// VerifyHMAC
// =============================================================================

func TestClient_VerifyHMAC_Validation(t *testing.T) {
	client := NewClient(respTransport(t, "VerifyHMAC", &VerifyHMACResponse{Valid: true}))
	ctx := context.Background()

	tests := []struct {
		name    string
		ctx     context.Context
		req     *VerifyHMACRequest
		wantErr bool
	}{
		{name: "nil request", ctx: ctx, req: nil, wantErr: true},
		{name: "empty key ID", ctx: ctx, req: &VerifyHMACRequest{Message: []byte("m"), HMAC: []byte("mac")}, wantErr: true},
		{name: "empty message", ctx: ctx, req: &VerifyHMACRequest{KeyID: "key-1", HMAC: []byte("mac")}, wantErr: true},
		{name: "empty hmac", ctx: ctx, req: &VerifyHMACRequest{KeyID: "key-1", Message: []byte("m")}, wantErr: true},
		{name: "nil context", ctx: nil, req: &VerifyHMACRequest{KeyID: "key-1", Message: []byte("m"), HMAC: []byte("mac")}, wantErr: true},
		{name: "valid", ctx: ctx, req: &VerifyHMACRequest{KeyID: "key-1", Message: []byte("m"), HMAC: []byte("mac"), KeyVersion: 1}, wantErr: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := client.VerifyHMAC(tt.ctx, tt.req)
			if (err != nil) != tt.wantErr {
				t.Errorf("VerifyHMAC() error = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.wantErr && err != nil && !errors.IsInvalidInput(err) {
				t.Errorf("expected InvalidInput error, got %v", err)
			}
		})
	}
}

func TestClient_VerifyHMAC(t *testing.T) {
	ctx := context.Background()
	validReq := &VerifyHMACRequest{KeyID: "key-1", Message: []byte("m"), HMAC: []byte("mac"), HashAlg: HashSHA256, KeyVersion: 1}

	t.Run("sdk response type valid", func(t *testing.T) {
		client := NewClient(respTransport(t, "VerifyHMAC", &VerifyHMACResponse{Valid: true}))
		resp, err := client.VerifyHMAC(ctx, validReq)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !resp.Valid {
			t.Error("expected Valid=true")
		}
	})

	t.Run("service response type invalid", func(t *testing.T) {
		client := NewClient(respTransport(t, "VerifyHMAC", &svc.VerifyHMACResponse{Valid: false}))
		resp, err := client.VerifyHMAC(ctx, validReq)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if resp.Valid {
			t.Error("expected Valid=false")
		}
	})

	t.Run("transport error", func(t *testing.T) {
		client := NewClient(errTransport())
		_, err := client.VerifyHMAC(ctx, validReq)
		if errors.Code(err) != errors.CodeUnavailable {
			t.Errorf("expected Unavailable, got %v", err)
		}
	})

	t.Run("nil response", func(t *testing.T) {
		client := NewClient(nilTransport())
		_, err := client.VerifyHMAC(ctx, validReq)
		if errors.Code(err) != errors.CodeInternal {
			t.Errorf("expected Internal, got %v", err)
		}
	})

	t.Run("invalid response type", func(t *testing.T) {
		client := NewClient(badTypeTransport())
		_, err := client.VerifyHMAC(ctx, validReq)
		if errors.Code(err) != errors.CodeInternal {
			t.Errorf("expected Internal, got %v", err)
		}
	})
}

// =============================================================================
// GenerateRandom
// =============================================================================

func TestClient_GenerateRandom_Validation(t *testing.T) {
	client := NewClient(respTransport(t, "GenerateRandom", &GenerateRandomResponse{RandomBytes: []byte("rnd")}))
	ctx := context.Background()

	tests := []struct {
		name    string
		ctx     context.Context
		req     *GenerateRandomRequest
		wantErr bool
	}{
		{name: "nil request", ctx: ctx, req: nil, wantErr: true},
		{name: "zero bytes", ctx: ctx, req: &GenerateRandomRequest{NumBytes: 0}, wantErr: true},
		{name: "negative bytes", ctx: ctx, req: &GenerateRandomRequest{NumBytes: -1}, wantErr: true},
		{name: "nil context", ctx: nil, req: &GenerateRandomRequest{NumBytes: 16}, wantErr: true},
		{name: "valid", ctx: ctx, req: &GenerateRandomRequest{NumBytes: 16}, wantErr: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := client.GenerateRandom(tt.ctx, tt.req)
			if (err != nil) != tt.wantErr {
				t.Errorf("GenerateRandom() error = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.wantErr && err != nil && !errors.IsInvalidInput(err) {
				t.Errorf("expected InvalidInput error, got %v", err)
			}
		})
	}
}

func TestClient_GenerateRandom(t *testing.T) {
	ctx := context.Background()
	validReq := &GenerateRandomRequest{NumBytes: 16}

	t.Run("sdk response type", func(t *testing.T) {
		client := NewClient(respTransport(t, "GenerateRandom", &GenerateRandomResponse{RandomBytes: []byte("0123456789abcdef")}))
		resp, err := client.GenerateRandom(ctx, validReq)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(resp.RandomBytes) != 16 {
			t.Errorf("expected 16 bytes, got %d", len(resp.RandomBytes))
		}
	})

	t.Run("service response type", func(t *testing.T) {
		client := NewClient(respTransport(t, "GenerateRandom", &svc.GenerateRandomResponse{RandomBytes: []byte("0123456789abcdef")}))
		resp, err := client.GenerateRandom(ctx, validReq)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if string(resp.RandomBytes) != "0123456789abcdef" {
			t.Errorf("unexpected converted response: %+v", resp)
		}
	})

	t.Run("transport error", func(t *testing.T) {
		client := NewClient(errTransport())
		_, err := client.GenerateRandom(ctx, validReq)
		if errors.Code(err) != errors.CodeUnavailable {
			t.Errorf("expected Unavailable, got %v", err)
		}
	})

	t.Run("nil response", func(t *testing.T) {
		client := NewClient(nilTransport())
		_, err := client.GenerateRandom(ctx, validReq)
		if errors.Code(err) != errors.CodeInternal {
			t.Errorf("expected Internal, got %v", err)
		}
	})

	t.Run("invalid response type", func(t *testing.T) {
		client := NewClient(badTypeTransport())
		_, err := client.GenerateRandom(ctx, validReq)
		if errors.Code(err) != errors.CodeInternal {
			t.Errorf("expected Internal, got %v", err)
		}
	})
}

// =============================================================================
// Hash
// =============================================================================

func TestClient_Hash_Validation(t *testing.T) {
	client := NewClient(respTransport(t, "Hash", &HashResponse{Hash: []byte("h")}))
	ctx := context.Background()

	tests := []struct {
		name    string
		ctx     context.Context
		req     *HashRequest
		wantErr bool
	}{
		{name: "nil request", ctx: ctx, req: nil, wantErr: true},
		{name: "empty data", ctx: ctx, req: &HashRequest{HashAlg: HashSHA256}, wantErr: true},
		{name: "nil context", ctx: nil, req: &HashRequest{Data: []byte("d")}, wantErr: true},
		{name: "valid", ctx: ctx, req: &HashRequest{Data: []byte("d"), HashAlg: HashSHA256}, wantErr: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := client.Hash(tt.ctx, tt.req)
			if (err != nil) != tt.wantErr {
				t.Errorf("Hash() error = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.wantErr && err != nil && !errors.IsInvalidInput(err) {
				t.Errorf("expected InvalidInput error, got %v", err)
			}
		})
	}
}

func TestClient_Hash(t *testing.T) {
	ctx := context.Background()
	validReq := &HashRequest{Data: []byte("d"), HashAlg: HashSHA512}

	t.Run("sdk response type", func(t *testing.T) {
		client := NewClient(respTransport(t, "Hash", &HashResponse{HashAlg: HashSHA512, Hash: []byte("digest")}))
		resp, err := client.Hash(ctx, validReq)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if string(resp.Hash) != "digest" {
			t.Errorf("unexpected response: %+v", resp)
		}
	})

	t.Run("service response type", func(t *testing.T) {
		client := NewClient(respTransport(t, "Hash", &svc.HashResponse{Algorithm: svc.HashAlgSHA384, Hash: []byte("digest")}))
		resp, err := client.Hash(ctx, validReq)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if resp.HashAlg != HashSHA384 || string(resp.Hash) != "digest" {
			t.Errorf("unexpected converted response: %+v", resp)
		}
	})

	t.Run("transport error", func(t *testing.T) {
		client := NewClient(errTransport())
		_, err := client.Hash(ctx, validReq)
		if errors.Code(err) != errors.CodeUnavailable {
			t.Errorf("expected Unavailable, got %v", err)
		}
	})

	t.Run("nil response", func(t *testing.T) {
		client := NewClient(nilTransport())
		_, err := client.Hash(ctx, validReq)
		if errors.Code(err) != errors.CodeInternal {
			t.Errorf("expected Internal, got %v", err)
		}
	})

	t.Run("invalid response type", func(t *testing.T) {
		client := NewClient(badTypeTransport())
		_, err := client.Hash(ctx, validReq)
		if errors.Code(err) != errors.CodeInternal {
			t.Errorf("expected Internal, got %v", err)
		}
	})
}

// =============================================================================
// Conversion helpers
// =============================================================================

func TestHashAlgToServiceHashAlg(t *testing.T) {
	tests := []struct {
		in   HashAlgorithm
		want svc.HashAlgorithm
	}{
		{HashSHA256, svc.HashAlgSHA256},
		{HashSHA384, svc.HashAlgSHA384},
		{HashSHA512, svc.HashAlgSHA512},
		{HashAlgorithm(99), svc.HashAlgSHA256}, // default
	}
	for _, tt := range tests {
		if got := hashAlgToServiceHashAlg(tt.in); got != tt.want {
			t.Errorf("hashAlgToServiceHashAlg(%v) = %v, want %v", tt.in, got, tt.want)
		}
	}
}

func TestServiceHashAlgToHashAlg(t *testing.T) {
	tests := []struct {
		in   svc.HashAlgorithm
		want HashAlgorithm
	}{
		{svc.HashAlgSHA256, HashSHA256},
		{svc.HashAlgSHA384, HashSHA384},
		{svc.HashAlgSHA512, HashSHA512},
		{svc.HashAlgorithm("BOGUS"), HashSHA256}, // default
	}
	for _, tt := range tests {
		if got := serviceHashAlgToHashAlg(tt.in); got != tt.want {
			t.Errorf("serviceHashAlgToHashAlg(%v) = %v, want %v", tt.in, got, tt.want)
		}
	}
}

// =============================================================================
// Sign / Verify service-response conversion branches
// =============================================================================

func TestClient_Sign_ServiceResponse(t *testing.T) {
	ctx := context.Background()
	client := NewClient(respTransport(t, "Sign", &svc.SignResponse{
		KeyID: "key-1", KeyVersion: 4, Signature: []byte("sig"),
	}))
	resp, err := client.Sign(ctx, &SignRequest{KeyID: "key-1", Data: []byte("d"), HashAlg: HashSHA256})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.KeyVersion != 4 || string(resp.Signature) != "sig" {
		t.Errorf("unexpected converted response: %+v", resp)
	}
}

func TestClient_Verify_ServiceResponse(t *testing.T) {
	ctx := context.Background()
	client := NewClient(respTransport(t, "Verify", &svc.VerifyResponse{Valid: true}))
	resp, err := client.Verify(ctx, &VerifyRequest{KeyID: "key-1", Data: []byte("d"), Signature: []byte("sig"), HashAlg: HashSHA256})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !resp.Valid {
		t.Error("expected Valid=true from service response conversion")
	}
}
