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
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	pb "github.com/jeremyhahn/go-quicbaas/api/proto"
	"github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
)

// mockCryptoService implements service.CryptoService for testing.
type mockCryptoService struct {
	encryptFunc                func(ctx context.Context, req *service.EncryptRequest) (*service.EncryptResponse, error)
	decryptFunc                func(ctx context.Context, req *service.DecryptRequest) (*service.DecryptResponse, error)
	rewrapFunc                 func(ctx context.Context, req *service.RewrapRequest) (*service.RewrapResponse, error)
	encryptBatchFunc           func(ctx context.Context, req *service.EncryptBatchRequest) (*service.EncryptBatchResponse, error)
	decryptBatchFunc           func(ctx context.Context, req *service.DecryptBatchRequest) (*service.DecryptBatchResponse, error)
	generateDataKeyFunc        func(ctx context.Context, req *service.GenerateDataKeyRequest) (*service.GenerateDataKeyResponse, error)
	generateWrappedDataKeyFunc func(ctx context.Context, req *service.GenerateWrappedDataKeyRequest) (*service.GenerateWrappedDataKeyResponse, error)
	signFunc                   func(ctx context.Context, req *service.SignRequest) (*service.SignResponse, error)
	verifyFunc                 func(ctx context.Context, req *service.VerifyRequest) (*service.VerifyResponse, error)
	generateHMACFunc           func(ctx context.Context, req *service.HMACRequest) (*service.HMACResponse, error)
	verifyHMACFunc             func(ctx context.Context, req *service.VerifyHMACRequest) (*service.VerifyHMACResponse, error)
	generateRandomFunc         func(ctx context.Context, req *service.GenerateRandomRequest) (*service.GenerateRandomResponse, error)
	hashFunc                   func(ctx context.Context, req *service.HashRequest) (*service.HashResponse, error)
}

func (m *mockCryptoService) Encrypt(ctx context.Context, req *service.EncryptRequest) (*service.EncryptResponse, error) {
	if m.encryptFunc != nil {
		return m.encryptFunc(ctx, req)
	}
	return nil, nil
}

func (m *mockCryptoService) Decrypt(ctx context.Context, req *service.DecryptRequest) (*service.DecryptResponse, error) {
	if m.decryptFunc != nil {
		return m.decryptFunc(ctx, req)
	}
	return nil, nil
}

func (m *mockCryptoService) Rewrap(ctx context.Context, req *service.RewrapRequest) (*service.RewrapResponse, error) {
	if m.rewrapFunc != nil {
		return m.rewrapFunc(ctx, req)
	}
	return nil, nil
}

func (m *mockCryptoService) EncryptBatch(ctx context.Context, req *service.EncryptBatchRequest) (*service.EncryptBatchResponse, error) {
	if m.encryptBatchFunc != nil {
		return m.encryptBatchFunc(ctx, req)
	}
	return nil, nil
}

func (m *mockCryptoService) DecryptBatch(ctx context.Context, req *service.DecryptBatchRequest) (*service.DecryptBatchResponse, error) {
	if m.decryptBatchFunc != nil {
		return m.decryptBatchFunc(ctx, req)
	}
	return nil, nil
}

func (m *mockCryptoService) GenerateDataKey(ctx context.Context, req *service.GenerateDataKeyRequest) (*service.GenerateDataKeyResponse, error) {
	if m.generateDataKeyFunc != nil {
		return m.generateDataKeyFunc(ctx, req)
	}
	return nil, nil
}

func (m *mockCryptoService) GenerateWrappedDataKey(ctx context.Context, req *service.GenerateWrappedDataKeyRequest) (*service.GenerateWrappedDataKeyResponse, error) {
	if m.generateWrappedDataKeyFunc != nil {
		return m.generateWrappedDataKeyFunc(ctx, req)
	}
	return nil, nil
}

func (m *mockCryptoService) Sign(ctx context.Context, req *service.SignRequest) (*service.SignResponse, error) {
	if m.signFunc != nil {
		return m.signFunc(ctx, req)
	}
	return nil, nil
}

func (m *mockCryptoService) Verify(ctx context.Context, req *service.VerifyRequest) (*service.VerifyResponse, error) {
	if m.verifyFunc != nil {
		return m.verifyFunc(ctx, req)
	}
	return nil, nil
}

func (m *mockCryptoService) GenerateHMAC(ctx context.Context, req *service.HMACRequest) (*service.HMACResponse, error) {
	if m.generateHMACFunc != nil {
		return m.generateHMACFunc(ctx, req)
	}
	return nil, nil
}

func (m *mockCryptoService) VerifyHMAC(ctx context.Context, req *service.VerifyHMACRequest) (*service.VerifyHMACResponse, error) {
	if m.verifyHMACFunc != nil {
		return m.verifyHMACFunc(ctx, req)
	}
	return nil, nil
}

func (m *mockCryptoService) GenerateRandom(ctx context.Context, req *service.GenerateRandomRequest) (*service.GenerateRandomResponse, error) {
	if m.generateRandomFunc != nil {
		return m.generateRandomFunc(ctx, req)
	}
	return nil, nil
}

func (m *mockCryptoService) Hash(ctx context.Context, req *service.HashRequest) (*service.HashResponse, error) {
	if m.hashFunc != nil {
		return m.hashFunc(ctx, req)
	}
	return nil, nil
}

// TestNewCryptoServer tests the NewCryptoServer constructor.
func TestNewCryptoServer(t *testing.T) {
	log, err := logger.New(logger.LogConfig{Level: "debug", Format: "json", Output: "stdout"})
	require.NoError(t, err)

	mockSvc := &mockCryptoService{}
	srv := NewCryptoServer(mockSvc, log)

	assert.NotNil(t, srv)
	assert.NotNil(t, srv.cryptoSvc)
	assert.NotNil(t, srv.logger)
}

// TestEncrypt tests the Encrypt RPC method.
func TestEncrypt(t *testing.T) {
	log, _ := logger.New(logger.LogConfig{Level: "debug", Format: "json", Output: "stdout"})
	now := time.Now()

	tests := []struct {
		name        string
		request     *pb.CryptoEncryptRequest
		mockFunc    func(ctx context.Context, req *service.EncryptRequest) (*service.EncryptResponse, error)
		expectError bool
		errorCode   codes.Code
		checkResp   func(t *testing.T, resp *pb.CryptoEncryptResponse)
	}{
		{
			name: "successful_encryption",
			request: &pb.CryptoEncryptRequest{
				KeyId:     "test-key",
				Plaintext: []byte("hello world"),
				Context:   []byte("test-context"),
			},
			mockFunc: func(ctx context.Context, req *service.EncryptRequest) (*service.EncryptResponse, error) {
				assert.Equal(t, "test-key", req.KeyID)
				assert.Equal(t, []byte("hello world"), req.Plaintext)
				return &service.EncryptResponse{
					KeyID:      "test-key",
					KeyVersion: 1,
					Ciphertext: []byte("encrypted-data"),
					Nonce:      []byte("nonce-123"),
					Timestamp:  now,
				}, nil
			},
			expectError: false,
			checkResp: func(t *testing.T, resp *pb.CryptoEncryptResponse) {
				assert.Equal(t, "test-key", resp.KeyId)
				assert.Equal(t, uint64(1), resp.KeyVersion)
				assert.Equal(t, []byte("encrypted-data"), resp.Ciphertext)
				assert.Equal(t, []byte("nonce-123"), resp.Nonce)
			},
		},
		{
			name: "missing_key_id",
			request: &pb.CryptoEncryptRequest{
				KeyId:     "",
				Plaintext: []byte("hello world"),
			},
			mockFunc:    nil,
			expectError: true,
			errorCode:   codes.InvalidArgument,
		},
		{
			name: "missing_plaintext",
			request: &pb.CryptoEncryptRequest{
				KeyId:     "test-key",
				Plaintext: nil,
			},
			mockFunc:    nil,
			expectError: true,
			errorCode:   codes.InvalidArgument,
		},
		{
			name: "service_error",
			request: &pb.CryptoEncryptRequest{
				KeyId:     "test-key",
				Plaintext: []byte("hello world"),
			},
			mockFunc: func(ctx context.Context, req *service.EncryptRequest) (*service.EncryptResponse, error) {
				return nil, &errors.DragonError{
					Code: errors.ErrNotFound,
					Op:   "crypto.Encrypt",
					Err:  assert.AnError,
				}
			},
			expectError: true,
			errorCode:   codes.NotFound,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockSvc := &mockCryptoService{
				encryptFunc: tt.mockFunc,
			}
			srv := NewCryptoServer(mockSvc, log)

			ctx := context.Background()
			resp, err := srv.Encrypt(ctx, tt.request)

			if tt.expectError {
				assert.Error(t, err)
				st, ok := status.FromError(err)
				assert.True(t, ok)
				assert.Equal(t, tt.errorCode, st.Code())
			} else {
				require.NoError(t, err)
				assert.NotNil(t, resp)
				if tt.checkResp != nil {
					tt.checkResp(t, resp)
				}
			}
		})
	}
}

// TestDecrypt tests the Decrypt RPC method.
func TestDecrypt(t *testing.T) {
	log, _ := logger.New(logger.LogConfig{Level: "debug", Format: "json", Output: "stdout"})
	now := time.Now()

	tests := []struct {
		name        string
		request     *pb.CryptoDecryptRequest
		mockFunc    func(ctx context.Context, req *service.DecryptRequest) (*service.DecryptResponse, error)
		expectError bool
		errorCode   codes.Code
		checkResp   func(t *testing.T, resp *pb.CryptoDecryptResponse)
	}{
		{
			name: "successful_decryption",
			request: &pb.CryptoDecryptRequest{
				KeyId:      "test-key",
				Ciphertext: []byte("encrypted-data"),
				Context:    []byte("test-context"),
			},
			mockFunc: func(ctx context.Context, req *service.DecryptRequest) (*service.DecryptResponse, error) {
				assert.Equal(t, "test-key", req.KeyID)
				assert.Equal(t, []byte("encrypted-data"), req.Ciphertext)
				return &service.DecryptResponse{
					KeyID:      "test-key",
					KeyVersion: 1,
					Plaintext:  []byte("hello world"),
					Timestamp:  now,
				}, nil
			},
			expectError: false,
			checkResp: func(t *testing.T, resp *pb.CryptoDecryptResponse) {
				assert.Equal(t, "test-key", resp.KeyId)
				assert.Equal(t, uint64(1), resp.KeyVersion)
				assert.Equal(t, []byte("hello world"), resp.Plaintext)
			},
		},
		{
			name: "missing_key_id",
			request: &pb.CryptoDecryptRequest{
				KeyId:      "",
				Ciphertext: []byte("encrypted-data"),
			},
			mockFunc:    nil,
			expectError: true,
			errorCode:   codes.InvalidArgument,
		},
		{
			name: "missing_ciphertext",
			request: &pb.CryptoDecryptRequest{
				KeyId:      "test-key",
				Ciphertext: nil,
			},
			mockFunc:    nil,
			expectError: true,
			errorCode:   codes.InvalidArgument,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockSvc := &mockCryptoService{
				decryptFunc: tt.mockFunc,
			}
			srv := NewCryptoServer(mockSvc, log)

			ctx := context.Background()
			resp, err := srv.Decrypt(ctx, tt.request)

			if tt.expectError {
				assert.Error(t, err)
				st, ok := status.FromError(err)
				assert.True(t, ok)
				assert.Equal(t, tt.errorCode, st.Code())
			} else {
				require.NoError(t, err)
				assert.NotNil(t, resp)
				if tt.checkResp != nil {
					tt.checkResp(t, resp)
				}
			}
		})
	}
}

// TestRewrap tests the Rewrap RPC method.
func TestRewrap(t *testing.T) {
	log, _ := logger.New(logger.LogConfig{Level: "debug", Format: "json", Output: "stdout"})
	now := time.Now()

	tests := []struct {
		name        string
		request     *pb.RewrapRequest
		mockFunc    func(ctx context.Context, req *service.RewrapRequest) (*service.RewrapResponse, error)
		expectError bool
		errorCode   codes.Code
		checkResp   func(t *testing.T, resp *pb.RewrapResponse)
	}{
		{
			name: "successful_rewrap",
			request: &pb.RewrapRequest{
				KeyId:      "test-key",
				Ciphertext: []byte("old-encrypted-data"),
			},
			mockFunc: func(ctx context.Context, req *service.RewrapRequest) (*service.RewrapResponse, error) {
				return &service.RewrapResponse{
					KeyID:         "test-key",
					OldKeyVersion: 1,
					NewKeyVersion: 2,
					Ciphertext:    []byte("new-encrypted-data"),
					Timestamp:     now,
				}, nil
			},
			expectError: false,
			checkResp: func(t *testing.T, resp *pb.RewrapResponse) {
				assert.Equal(t, "test-key", resp.KeyId)
				assert.Equal(t, uint64(1), resp.OldKeyVersion)
				assert.Equal(t, uint64(2), resp.NewKeyVersion)
				assert.Equal(t, []byte("new-encrypted-data"), resp.Ciphertext)
			},
		},
		{
			name: "missing_key_id",
			request: &pb.RewrapRequest{
				KeyId:      "",
				Ciphertext: []byte("encrypted-data"),
			},
			mockFunc:    nil,
			expectError: true,
			errorCode:   codes.InvalidArgument,
		},
		{
			name: "missing_ciphertext",
			request: &pb.RewrapRequest{
				KeyId:      "test-key",
				Ciphertext: nil,
			},
			mockFunc:    nil,
			expectError: true,
			errorCode:   codes.InvalidArgument,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockSvc := &mockCryptoService{
				rewrapFunc: tt.mockFunc,
			}
			srv := NewCryptoServer(mockSvc, log)

			ctx := context.Background()
			resp, err := srv.Rewrap(ctx, tt.request)

			if tt.expectError {
				assert.Error(t, err)
				st, ok := status.FromError(err)
				assert.True(t, ok)
				assert.Equal(t, tt.errorCode, st.Code())
			} else {
				require.NoError(t, err)
				assert.NotNil(t, resp)
				if tt.checkResp != nil {
					tt.checkResp(t, resp)
				}
			}
		})
	}
}

// TestEncryptBatch tests the EncryptBatch RPC method.
func TestEncryptBatch(t *testing.T) {
	log, _ := logger.New(logger.LogConfig{Level: "debug", Format: "json", Output: "stdout"})
	now := time.Now()

	tests := []struct {
		name        string
		request     *pb.EncryptBatchRequest
		mockFunc    func(ctx context.Context, req *service.EncryptBatchRequest) (*service.EncryptBatchResponse, error)
		expectError bool
		errorCode   codes.Code
		checkResp   func(t *testing.T, resp *pb.EncryptBatchResponse)
	}{
		{
			name: "successful_batch_encryption",
			request: &pb.EncryptBatchRequest{
				KeyId: "test-key",
				Items: []*pb.BatchEncryptItem{
					{Plaintext: []byte("data1")},
					{Plaintext: []byte("data2")},
				},
			},
			mockFunc: func(ctx context.Context, req *service.EncryptBatchRequest) (*service.EncryptBatchResponse, error) {
				return &service.EncryptBatchResponse{
					KeyID:      "test-key",
					KeyVersion: 1,
					Results: []service.BatchEncryptResult{
						{Ciphertext: []byte("encrypted1")},
						{Ciphertext: []byte("encrypted2")},
					},
					Timestamp: now,
				}, nil
			},
			expectError: false,
			checkResp: func(t *testing.T, resp *pb.EncryptBatchResponse) {
				assert.Equal(t, "test-key", resp.KeyId)
				assert.Len(t, resp.Results, 2)
			},
		},
		{
			name: "missing_key_id",
			request: &pb.EncryptBatchRequest{
				KeyId: "",
				Items: []*pb.BatchEncryptItem{{Plaintext: []byte("data1")}},
			},
			mockFunc:    nil,
			expectError: true,
			errorCode:   codes.InvalidArgument,
		},
		{
			name: "empty_items",
			request: &pb.EncryptBatchRequest{
				KeyId: "test-key",
				Items: []*pb.BatchEncryptItem{},
			},
			mockFunc:    nil,
			expectError: true,
			errorCode:   codes.InvalidArgument,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockSvc := &mockCryptoService{
				encryptBatchFunc: tt.mockFunc,
			}
			srv := NewCryptoServer(mockSvc, log)

			ctx := context.Background()
			resp, err := srv.EncryptBatch(ctx, tt.request)

			if tt.expectError {
				assert.Error(t, err)
				st, ok := status.FromError(err)
				assert.True(t, ok)
				assert.Equal(t, tt.errorCode, st.Code())
			} else {
				require.NoError(t, err)
				assert.NotNil(t, resp)
				if tt.checkResp != nil {
					tt.checkResp(t, resp)
				}
			}
		})
	}
}

// TestDecryptBatch tests the DecryptBatch RPC method.
func TestDecryptBatch(t *testing.T) {
	log, _ := logger.New(logger.LogConfig{Level: "debug", Format: "json", Output: "stdout"})
	now := time.Now()

	tests := []struct {
		name        string
		request     *pb.DecryptBatchRequest
		mockFunc    func(ctx context.Context, req *service.DecryptBatchRequest) (*service.DecryptBatchResponse, error)
		expectError bool
		errorCode   codes.Code
		checkResp   func(t *testing.T, resp *pb.DecryptBatchResponse)
	}{
		{
			name: "successful_batch_decryption",
			request: &pb.DecryptBatchRequest{
				KeyId: "test-key",
				Items: []*pb.BatchDecryptItem{
					{Ciphertext: []byte("encrypted1")},
					{Ciphertext: []byte("encrypted2")},
				},
			},
			mockFunc: func(ctx context.Context, req *service.DecryptBatchRequest) (*service.DecryptBatchResponse, error) {
				return &service.DecryptBatchResponse{
					KeyID: "test-key",
					Results: []service.BatchDecryptResult{
						{Plaintext: []byte("data1")},
						{Plaintext: []byte("data2")},
					},
					Timestamp: now,
				}, nil
			},
			expectError: false,
			checkResp: func(t *testing.T, resp *pb.DecryptBatchResponse) {
				assert.Equal(t, "test-key", resp.KeyId)
				assert.Len(t, resp.Results, 2)
			},
		},
		{
			name: "missing_key_id",
			request: &pb.DecryptBatchRequest{
				KeyId: "",
				Items: []*pb.BatchDecryptItem{{Ciphertext: []byte("encrypted1")}},
			},
			mockFunc:    nil,
			expectError: true,
			errorCode:   codes.InvalidArgument,
		},
		{
			name: "empty_items",
			request: &pb.DecryptBatchRequest{
				KeyId: "test-key",
				Items: []*pb.BatchDecryptItem{},
			},
			mockFunc:    nil,
			expectError: true,
			errorCode:   codes.InvalidArgument,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockSvc := &mockCryptoService{
				decryptBatchFunc: tt.mockFunc,
			}
			srv := NewCryptoServer(mockSvc, log)

			ctx := context.Background()
			resp, err := srv.DecryptBatch(ctx, tt.request)

			if tt.expectError {
				assert.Error(t, err)
				st, ok := status.FromError(err)
				assert.True(t, ok)
				assert.Equal(t, tt.errorCode, st.Code())
			} else {
				require.NoError(t, err)
				assert.NotNil(t, resp)
				if tt.checkResp != nil {
					tt.checkResp(t, resp)
				}
			}
		})
	}
}

// TestGenerateDataKey tests the GenerateDataKey RPC method.
func TestGenerateDataKey(t *testing.T) {
	log, _ := logger.New(logger.LogConfig{Level: "debug", Format: "json", Output: "stdout"})
	now := time.Now()

	tests := []struct {
		name        string
		request     *pb.GenerateDataKeyRequest
		mockFunc    func(ctx context.Context, req *service.GenerateDataKeyRequest) (*service.GenerateDataKeyResponse, error)
		expectError bool
		errorCode   codes.Code
		checkResp   func(t *testing.T, resp *pb.GenerateDataKeyResponse)
	}{
		{
			name: "successful_data_key_generation",
			request: &pb.GenerateDataKeyRequest{
				KeyId: "master-key",
				KeySpec: &pb.KeySpec{
					Algorithm: pb.KeyAlgorithm_KEY_ALG_AES_256_GCM,
					KeySize:   256,
					Usage:     []pb.KeyUsage{pb.KeyUsage_KEY_USAGE_ENCRYPT},
				},
			},
			mockFunc: func(ctx context.Context, req *service.GenerateDataKeyRequest) (*service.GenerateDataKeyResponse, error) {
				return &service.GenerateDataKeyResponse{
					KeyID:          "master-key",
					Plaintext:      []byte("plaintext-dek"),
					CiphertextBlob: []byte("encrypted-dek"),
					Timestamp:      now,
				}, nil
			},
			expectError: false,
			checkResp: func(t *testing.T, resp *pb.GenerateDataKeyResponse) {
				assert.Equal(t, "master-key", resp.KeyId)
				assert.Equal(t, []byte("plaintext-dek"), resp.Plaintext)
				assert.Equal(t, []byte("encrypted-dek"), resp.CiphertextBlob)
			},
		},
		{
			name: "missing_key_id",
			request: &pb.GenerateDataKeyRequest{
				KeyId: "",
				KeySpec: &pb.KeySpec{
					Algorithm: pb.KeyAlgorithm_KEY_ALG_AES_256_GCM,
				},
			},
			mockFunc:    nil,
			expectError: true,
			errorCode:   codes.InvalidArgument,
		},
		{
			name: "missing_key_spec",
			request: &pb.GenerateDataKeyRequest{
				KeyId:   "master-key",
				KeySpec: nil,
			},
			mockFunc:    nil,
			expectError: true,
			errorCode:   codes.InvalidArgument,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockSvc := &mockCryptoService{
				generateDataKeyFunc: tt.mockFunc,
			}
			srv := NewCryptoServer(mockSvc, log)

			ctx := context.Background()
			resp, err := srv.GenerateDataKey(ctx, tt.request)

			if tt.expectError {
				assert.Error(t, err)
				st, ok := status.FromError(err)
				assert.True(t, ok)
				assert.Equal(t, tt.errorCode, st.Code())
			} else {
				require.NoError(t, err)
				assert.NotNil(t, resp)
				if tt.checkResp != nil {
					tt.checkResp(t, resp)
				}
			}
		})
	}
}

// TestGenerateWrappedDataKey tests the GenerateWrappedDataKey RPC method.
func TestGenerateWrappedDataKey(t *testing.T) {
	log, _ := logger.New(logger.LogConfig{Level: "debug", Format: "json", Output: "stdout"})
	now := time.Now()

	tests := []struct {
		name        string
		request     *pb.GenerateWrappedDataKeyRequest
		mockFunc    func(ctx context.Context, req *service.GenerateWrappedDataKeyRequest) (*service.GenerateWrappedDataKeyResponse, error)
		expectError bool
		errorCode   codes.Code
		checkResp   func(t *testing.T, resp *pb.GenerateWrappedDataKeyResponse)
	}{
		{
			name: "successful_wrapped_key_generation",
			request: &pb.GenerateWrappedDataKeyRequest{
				KeyId: "master-key",
				KeySpec: &pb.KeySpec{
					Algorithm: pb.KeyAlgorithm_KEY_ALG_AES_256_GCM,
					KeySize:   256,
				},
			},
			mockFunc: func(ctx context.Context, req *service.GenerateWrappedDataKeyRequest) (*service.GenerateWrappedDataKeyResponse, error) {
				return &service.GenerateWrappedDataKeyResponse{
					KeyID:          "master-key",
					CiphertextBlob: []byte("encrypted-dek-only"),
					Timestamp:      now,
				}, nil
			},
			expectError: false,
			checkResp: func(t *testing.T, resp *pb.GenerateWrappedDataKeyResponse) {
				assert.Equal(t, "master-key", resp.KeyId)
				assert.Equal(t, []byte("encrypted-dek-only"), resp.CiphertextBlob)
			},
		},
		{
			name: "missing_key_id",
			request: &pb.GenerateWrappedDataKeyRequest{
				KeyId: "",
				KeySpec: &pb.KeySpec{
					Algorithm: pb.KeyAlgorithm_KEY_ALG_AES_256_GCM,
				},
			},
			mockFunc:    nil,
			expectError: true,
			errorCode:   codes.InvalidArgument,
		},
		{
			name: "missing_key_spec",
			request: &pb.GenerateWrappedDataKeyRequest{
				KeyId:   "master-key",
				KeySpec: nil,
			},
			mockFunc:    nil,
			expectError: true,
			errorCode:   codes.InvalidArgument,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockSvc := &mockCryptoService{
				generateWrappedDataKeyFunc: tt.mockFunc,
			}
			srv := NewCryptoServer(mockSvc, log)

			ctx := context.Background()
			resp, err := srv.GenerateWrappedDataKey(ctx, tt.request)

			if tt.expectError {
				assert.Error(t, err)
				st, ok := status.FromError(err)
				assert.True(t, ok)
				assert.Equal(t, tt.errorCode, st.Code())
			} else {
				require.NoError(t, err)
				assert.NotNil(t, resp)
				if tt.checkResp != nil {
					tt.checkResp(t, resp)
				}
			}
		})
	}
}

// TestSign tests the Sign RPC method.
func TestSign(t *testing.T) {
	log, _ := logger.New(logger.LogConfig{Level: "debug", Format: "json", Output: "stdout"})
	now := time.Now()

	tests := []struct {
		name        string
		request     *pb.SignRequest
		mockFunc    func(ctx context.Context, req *service.SignRequest) (*service.SignResponse, error)
		expectError bool
		errorCode   codes.Code
		checkResp   func(t *testing.T, resp *pb.SignResponse)
	}{
		{
			name: "successful_signing",
			request: &pb.SignRequest{
				KeyId:         "signing-key",
				Message:       []byte("message to sign"),
				HashAlgorithm: pb.HashAlgorithm_HASH_ALG_SHA256,
			},
			mockFunc: func(ctx context.Context, req *service.SignRequest) (*service.SignResponse, error) {
				return &service.SignResponse{
					KeyID:         "signing-key",
					KeyVersion:    1,
					Signature:     []byte("signature-data"),
					HashAlgorithm: service.HashAlgSHA256,
					Timestamp:     now,
				}, nil
			},
			expectError: false,
			checkResp: func(t *testing.T, resp *pb.SignResponse) {
				assert.Equal(t, "signing-key", resp.KeyId)
				assert.Equal(t, uint64(1), resp.KeyVersion)
				assert.Equal(t, []byte("signature-data"), resp.Signature)
			},
		},
		{
			name: "missing_key_id",
			request: &pb.SignRequest{
				KeyId:   "",
				Message: []byte("message"),
			},
			mockFunc:    nil,
			expectError: true,
			errorCode:   codes.InvalidArgument,
		},
		{
			name: "missing_message",
			request: &pb.SignRequest{
				KeyId:   "signing-key",
				Message: nil,
			},
			mockFunc:    nil,
			expectError: true,
			errorCode:   codes.InvalidArgument,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockSvc := &mockCryptoService{
				signFunc: tt.mockFunc,
			}
			srv := NewCryptoServer(mockSvc, log)

			ctx := context.Background()
			resp, err := srv.Sign(ctx, tt.request)

			if tt.expectError {
				assert.Error(t, err)
				st, ok := status.FromError(err)
				assert.True(t, ok)
				assert.Equal(t, tt.errorCode, st.Code())
			} else {
				require.NoError(t, err)
				assert.NotNil(t, resp)
				if tt.checkResp != nil {
					tt.checkResp(t, resp)
				}
			}
		})
	}
}

// TestVerify tests the Verify RPC method.
func TestVerify(t *testing.T) {
	log, _ := logger.New(logger.LogConfig{Level: "debug", Format: "json", Output: "stdout"})
	now := time.Now()

	tests := []struct {
		name        string
		request     *pb.VerifyRequest
		mockFunc    func(ctx context.Context, req *service.VerifyRequest) (*service.VerifyResponse, error)
		expectError bool
		errorCode   codes.Code
		checkResp   func(t *testing.T, resp *pb.VerifyResponse)
	}{
		{
			name: "successful_verification",
			request: &pb.VerifyRequest{
				KeyId:     "signing-key",
				Message:   []byte("message to verify"),
				Signature: []byte("signature-data"),
			},
			mockFunc: func(ctx context.Context, req *service.VerifyRequest) (*service.VerifyResponse, error) {
				return &service.VerifyResponse{
					KeyID:      "signing-key",
					KeyVersion: 1,
					Valid:      true,
					Timestamp:  now,
				}, nil
			},
			expectError: false,
			checkResp: func(t *testing.T, resp *pb.VerifyResponse) {
				assert.Equal(t, "signing-key", resp.KeyId)
				assert.True(t, resp.Valid)
			},
		},
		{
			name: "missing_key_id",
			request: &pb.VerifyRequest{
				KeyId:     "",
				Message:   []byte("message"),
				Signature: []byte("signature"),
			},
			mockFunc:    nil,
			expectError: true,
			errorCode:   codes.InvalidArgument,
		},
		{
			name: "missing_message",
			request: &pb.VerifyRequest{
				KeyId:     "signing-key",
				Message:   nil,
				Signature: []byte("signature"),
			},
			mockFunc:    nil,
			expectError: true,
			errorCode:   codes.InvalidArgument,
		},
		{
			name: "missing_signature",
			request: &pb.VerifyRequest{
				KeyId:     "signing-key",
				Message:   []byte("message"),
				Signature: nil,
			},
			mockFunc:    nil,
			expectError: true,
			errorCode:   codes.InvalidArgument,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockSvc := &mockCryptoService{
				verifyFunc: tt.mockFunc,
			}
			srv := NewCryptoServer(mockSvc, log)

			ctx := context.Background()
			resp, err := srv.Verify(ctx, tt.request)

			if tt.expectError {
				assert.Error(t, err)
				st, ok := status.FromError(err)
				assert.True(t, ok)
				assert.Equal(t, tt.errorCode, st.Code())
			} else {
				require.NoError(t, err)
				assert.NotNil(t, resp)
				if tt.checkResp != nil {
					tt.checkResp(t, resp)
				}
			}
		})
	}
}

// TestGenerateHMAC tests the GenerateHMAC RPC method.
func TestGenerateHMAC(t *testing.T) {
	log, _ := logger.New(logger.LogConfig{Level: "debug", Format: "json", Output: "stdout"})
	now := time.Now()

	tests := []struct {
		name        string
		request     *pb.HMACRequest
		mockFunc    func(ctx context.Context, req *service.HMACRequest) (*service.HMACResponse, error)
		expectError bool
		errorCode   codes.Code
		checkResp   func(t *testing.T, resp *pb.HMACResponse)
	}{
		{
			name: "successful_hmac_generation",
			request: &pb.HMACRequest{
				KeyId:         "hmac-key",
				Message:       []byte("message for hmac"),
				HashAlgorithm: pb.HashAlgorithm_HASH_ALG_SHA256,
			},
			mockFunc: func(ctx context.Context, req *service.HMACRequest) (*service.HMACResponse, error) {
				return &service.HMACResponse{
					KeyID:         "hmac-key",
					KeyVersion:    1,
					HMAC:          []byte("hmac-value"),
					HashAlgorithm: service.HashAlgSHA256,
					Timestamp:     now,
				}, nil
			},
			expectError: false,
			checkResp: func(t *testing.T, resp *pb.HMACResponse) {
				assert.Equal(t, "hmac-key", resp.KeyId)
				assert.Equal(t, []byte("hmac-value"), resp.Hmac)
			},
		},
		{
			name: "missing_key_id",
			request: &pb.HMACRequest{
				KeyId:   "",
				Message: []byte("message"),
			},
			mockFunc:    nil,
			expectError: true,
			errorCode:   codes.InvalidArgument,
		},
		{
			name: "missing_message",
			request: &pb.HMACRequest{
				KeyId:   "hmac-key",
				Message: nil,
			},
			mockFunc:    nil,
			expectError: true,
			errorCode:   codes.InvalidArgument,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockSvc := &mockCryptoService{
				generateHMACFunc: tt.mockFunc,
			}
			srv := NewCryptoServer(mockSvc, log)

			ctx := context.Background()
			resp, err := srv.GenerateHMAC(ctx, tt.request)

			if tt.expectError {
				assert.Error(t, err)
				st, ok := status.FromError(err)
				assert.True(t, ok)
				assert.Equal(t, tt.errorCode, st.Code())
			} else {
				require.NoError(t, err)
				assert.NotNil(t, resp)
				if tt.checkResp != nil {
					tt.checkResp(t, resp)
				}
			}
		})
	}
}

// TestVerifyHMAC tests the VerifyHMAC RPC method.
func TestVerifyHMAC(t *testing.T) {
	log, _ := logger.New(logger.LogConfig{Level: "debug", Format: "json", Output: "stdout"})
	now := time.Now()

	tests := []struct {
		name        string
		request     *pb.VerifyHMACRequest
		mockFunc    func(ctx context.Context, req *service.VerifyHMACRequest) (*service.VerifyHMACResponse, error)
		expectError bool
		errorCode   codes.Code
		checkResp   func(t *testing.T, resp *pb.VerifyHMACResponse)
	}{
		{
			name: "successful_hmac_verification",
			request: &pb.VerifyHMACRequest{
				KeyId:   "hmac-key",
				Message: []byte("message"),
				Hmac:    []byte("hmac-value"),
			},
			mockFunc: func(ctx context.Context, req *service.VerifyHMACRequest) (*service.VerifyHMACResponse, error) {
				return &service.VerifyHMACResponse{
					KeyID:      "hmac-key",
					KeyVersion: 1,
					Valid:      true,
					Timestamp:  now,
				}, nil
			},
			expectError: false,
			checkResp: func(t *testing.T, resp *pb.VerifyHMACResponse) {
				assert.Equal(t, "hmac-key", resp.KeyId)
				assert.True(t, resp.Valid)
			},
		},
		{
			name: "missing_key_id",
			request: &pb.VerifyHMACRequest{
				KeyId:   "",
				Message: []byte("message"),
				Hmac:    []byte("hmac"),
			},
			mockFunc:    nil,
			expectError: true,
			errorCode:   codes.InvalidArgument,
		},
		{
			name: "missing_message",
			request: &pb.VerifyHMACRequest{
				KeyId:   "hmac-key",
				Message: nil,
				Hmac:    []byte("hmac"),
			},
			mockFunc:    nil,
			expectError: true,
			errorCode:   codes.InvalidArgument,
		},
		{
			name: "missing_hmac",
			request: &pb.VerifyHMACRequest{
				KeyId:   "hmac-key",
				Message: []byte("message"),
				Hmac:    nil,
			},
			mockFunc:    nil,
			expectError: true,
			errorCode:   codes.InvalidArgument,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockSvc := &mockCryptoService{
				verifyHMACFunc: tt.mockFunc,
			}
			srv := NewCryptoServer(mockSvc, log)

			ctx := context.Background()
			resp, err := srv.VerifyHMAC(ctx, tt.request)

			if tt.expectError {
				assert.Error(t, err)
				st, ok := status.FromError(err)
				assert.True(t, ok)
				assert.Equal(t, tt.errorCode, st.Code())
			} else {
				require.NoError(t, err)
				assert.NotNil(t, resp)
				if tt.checkResp != nil {
					tt.checkResp(t, resp)
				}
			}
		})
	}
}

// TestGenerateRandom tests the GenerateRandom RPC method.
func TestGenerateRandom(t *testing.T) {
	log, _ := logger.New(logger.LogConfig{Level: "debug", Format: "json", Output: "stdout"})
	now := time.Now()

	tests := []struct {
		name        string
		request     *pb.GenerateRandomRequest
		mockFunc    func(ctx context.Context, req *service.GenerateRandomRequest) (*service.GenerateRandomResponse, error)
		expectError bool
		errorCode   codes.Code
		checkResp   func(t *testing.T, resp *pb.GenerateRandomResponse)
	}{
		{
			name: "successful_random_generation",
			request: &pb.GenerateRandomRequest{
				NumBytes: 32,
			},
			mockFunc: func(ctx context.Context, req *service.GenerateRandomRequest) (*service.GenerateRandomResponse, error) {
				return &service.GenerateRandomResponse{
					RandomBytes: make([]byte, 32),
					Timestamp:   now,
				}, nil
			},
			expectError: false,
			checkResp: func(t *testing.T, resp *pb.GenerateRandomResponse) {
				assert.Len(t, resp.RandomBytes, 32)
			},
		},
		{
			name: "invalid_num_bytes",
			request: &pb.GenerateRandomRequest{
				NumBytes: 0,
			},
			mockFunc:    nil,
			expectError: true,
			errorCode:   codes.InvalidArgument,
		},
		{
			name: "negative_num_bytes",
			request: &pb.GenerateRandomRequest{
				NumBytes: -1,
			},
			mockFunc:    nil,
			expectError: true,
			errorCode:   codes.InvalidArgument,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockSvc := &mockCryptoService{
				generateRandomFunc: tt.mockFunc,
			}
			srv := NewCryptoServer(mockSvc, log)

			ctx := context.Background()
			resp, err := srv.GenerateRandom(ctx, tt.request)

			if tt.expectError {
				assert.Error(t, err)
				st, ok := status.FromError(err)
				assert.True(t, ok)
				assert.Equal(t, tt.errorCode, st.Code())
			} else {
				require.NoError(t, err)
				assert.NotNil(t, resp)
				if tt.checkResp != nil {
					tt.checkResp(t, resp)
				}
			}
		})
	}
}

// TestHash tests the Hash RPC method.
func TestHash(t *testing.T) {
	log, _ := logger.New(logger.LogConfig{Level: "debug", Format: "json", Output: "stdout"})
	now := time.Now()

	tests := []struct {
		name        string
		request     *pb.HashRequest
		mockFunc    func(ctx context.Context, req *service.HashRequest) (*service.HashResponse, error)
		expectError bool
		errorCode   codes.Code
		checkResp   func(t *testing.T, resp *pb.HashResponse)
	}{
		{
			name: "successful_hash",
			request: &pb.HashRequest{
				Data:      []byte("data to hash"),
				Algorithm: pb.HashAlgorithm_HASH_ALG_SHA256,
			},
			mockFunc: func(ctx context.Context, req *service.HashRequest) (*service.HashResponse, error) {
				return &service.HashResponse{
					Algorithm: service.HashAlgSHA256,
					Hash:      []byte("hash-value"),
					Timestamp: now,
				}, nil
			},
			expectError: false,
			checkResp: func(t *testing.T, resp *pb.HashResponse) {
				assert.Equal(t, pb.HashAlgorithm_HASH_ALG_SHA256, resp.Algorithm)
				assert.Equal(t, []byte("hash-value"), resp.Hash)
			},
		},
		{
			name: "missing_data",
			request: &pb.HashRequest{
				Data:      nil,
				Algorithm: pb.HashAlgorithm_HASH_ALG_SHA256,
			},
			mockFunc:    nil,
			expectError: true,
			errorCode:   codes.InvalidArgument,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockSvc := &mockCryptoService{
				hashFunc: tt.mockFunc,
			}
			srv := NewCryptoServer(mockSvc, log)

			ctx := context.Background()
			resp, err := srv.Hash(ctx, tt.request)

			if tt.expectError {
				assert.Error(t, err)
				st, ok := status.FromError(err)
				assert.True(t, ok)
				assert.Equal(t, tt.errorCode, st.Code())
			} else {
				require.NoError(t, err)
				assert.NotNil(t, resp)
				if tt.checkResp != nil {
					tt.checkResp(t, resp)
				}
			}
		})
	}
}

// TestConvertProtoHashAlgorithm tests hash algorithm conversion.
func TestConvertProtoHashAlgorithm(t *testing.T) {
	tests := []struct {
		name     string
		proto    pb.HashAlgorithm
		expected service.HashAlgorithm
	}{
		{
			name:     "sha256",
			proto:    pb.HashAlgorithm_HASH_ALG_SHA256,
			expected: service.HashAlgSHA256,
		},
		{
			name:     "sha384",
			proto:    pb.HashAlgorithm_HASH_ALG_SHA384,
			expected: service.HashAlgSHA384,
		},
		{
			name:     "sha512",
			proto:    pb.HashAlgorithm_HASH_ALG_SHA512,
			expected: service.HashAlgSHA512,
		},
		{
			name:     "sha3_256",
			proto:    pb.HashAlgorithm_HASH_ALG_SHA3_256,
			expected: service.HashAlgSHA3_256,
		},
		{
			name:     "blake2b_256",
			proto:    pb.HashAlgorithm_HASH_ALG_BLAKE2B_256,
			expected: service.HashAlgBLAKE2b256,
		},
		{
			name:     "unspecified_defaults_to_sha256",
			proto:    pb.HashAlgorithm_HASH_ALG_UNSPECIFIED,
			expected: service.HashAlgSHA256,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := convertProtoHashAlgorithm(tt.proto)
			assert.Equal(t, tt.expected, result)
		})
	}
}

// TestConvertServiceHashAlgorithm tests service to proto hash algorithm conversion.
func TestConvertServiceHashAlgorithm(t *testing.T) {
	tests := []struct {
		name     string
		svc      service.HashAlgorithm
		expected pb.HashAlgorithm
	}{
		{
			name:     "sha256",
			svc:      service.HashAlgSHA256,
			expected: pb.HashAlgorithm_HASH_ALG_SHA256,
		},
		{
			name:     "sha384",
			svc:      service.HashAlgSHA384,
			expected: pb.HashAlgorithm_HASH_ALG_SHA384,
		},
		{
			name:     "sha512",
			svc:      service.HashAlgSHA512,
			expected: pb.HashAlgorithm_HASH_ALG_SHA512,
		},
		{
			name:     "blake2b_512",
			svc:      service.HashAlgBLAKE2b512,
			expected: pb.HashAlgorithm_HASH_ALG_BLAKE2B_512,
		},
		{
			name:     "unknown_defaults_to_sha256",
			svc:      service.HashAlgorithm("unknown"),
			expected: pb.HashAlgorithm_HASH_ALG_SHA256,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := convertServiceHashAlgorithm(tt.svc)
			assert.Equal(t, tt.expected, result)
		})
	}
}

// --- Merged from server_crypto_additional_test.go ---

// =============================================================================
// GenerateRandom Additional Tests
// =============================================================================

func TestCryptoServer_GenerateRandom_ServiceError(t *testing.T) {
	log, _ := logger.New(logger.LogConfig{Level: "debug", Format: "json", Output: "stdout"})

	mockSvc := &mockCryptoService{
		generateRandomFunc: func(ctx context.Context, req *service.GenerateRandomRequest) (*service.GenerateRandomResponse, error) {
			return nil, &errors.DragonError{
				Code: errors.ErrInternal,
				Op:   "generateRandom",
				Err:  assert.AnError,
			}
		},
	}
	srv := NewCryptoServer(mockSvc, log)

	resp, err := srv.GenerateRandom(context.Background(), &pb.GenerateRandomRequest{
		NumBytes: 32,
	})

	assert.Error(t, err)
	assert.Nil(t, resp)
	st, ok := status.FromError(err)
	assert.True(t, ok)
	assert.Equal(t, codes.Internal, st.Code())
}

// =============================================================================
// Hash Additional Tests
// =============================================================================

func TestCryptoServer_Hash_ServiceError(t *testing.T) {
	log, _ := logger.New(logger.LogConfig{Level: "debug", Format: "json", Output: "stdout"})

	mockSvc := &mockCryptoService{
		hashFunc: func(ctx context.Context, req *service.HashRequest) (*service.HashResponse, error) {
			return nil, &errors.DragonError{
				Code: errors.ErrInternal,
				Op:   "hash",
				Err:  assert.AnError,
			}
		},
	}
	srv := NewCryptoServer(mockSvc, log)

	resp, err := srv.Hash(context.Background(), &pb.HashRequest{
		Data:      []byte("test data"),
		Algorithm: pb.HashAlgorithm_HASH_ALG_SHA256,
	})

	assert.Error(t, err)
	assert.Nil(t, resp)
	st, ok := status.FromError(err)
	assert.True(t, ok)
	assert.Equal(t, codes.Internal, st.Code())
}
