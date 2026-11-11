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
	"crypto"
	stderrors "errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	xkmssdk "github.com/jeremyhahn/go-xkms/sdk/go"

	pb "github.com/jeremyhahn/go-quicbaas/api/proto"
	"github.com/jeremyhahn/go-quicbaas/pkg/config"
	dberrors "github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/pkg/kms"
	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
	"github.com/jeremyhahn/go-quicbaas/pkg/service/mocks"
)

// mockKMSManager implements interfaces.KMSManager for testing.
type mockKMSManager struct {
	generateKeyFunc          func(ctx context.Context, attrs *xkmssdk.KeyAttributes) (interface{}, error)
	importKeyFunc            func(ctx context.Context, keyID string, material []byte, algorithm string) (interface{}, error)
	getKeyFunc               func(ctx context.Context, keyID string) (interface{}, error)
	getPublicKeyFunc         func(ctx context.Context, keyID string) ([]byte, error)
	listKeysFunc             func(ctx context.Context, prefix string) ([]interface{}, error)
	rotateKeyFunc            func(ctx context.Context, keyID string) (interface{}, error)
	revokeKeyFunc            func(ctx context.Context, keyID string) error
	enableKeyFunc            func(ctx context.Context, keyID string) error
	disableKeyFunc           func(ctx context.Context, keyID string) error
	deleteKeyFunc            func(ctx context.Context, keyID string) error
	encryptFunc              func(ctx context.Context, keyID string, plaintext, aad []byte) ([]byte, error)
	decryptFunc              func(ctx context.Context, keyID string, ciphertext, aad []byte) ([]byte, error)
	signFunc                 func(ctx context.Context, keyID string, digest []byte, algorithm string) ([]byte, error)
	verifyFunc               func(ctx context.Context, keyID string, digest, signature []byte, algorithm string) (bool, error)
	generateDataKeyFunc      func(ctx context.Context, attrs *xkmssdk.KeyAttributes, keyBytes int) (plaintext, wrapped []byte, err error)
	updateStoredMetadataFunc func(metadata interface{}) error
	getBackendTypeFunc       func() config.BackendType
	isAvailableFunc          func(ctx context.Context) bool
	getSupportedAlgorithms   func(ctx context.Context) ([]string, error)
	closeFunc                func() error
}

func (m *mockKMSManager) GenerateKey(ctx context.Context, attrs *xkmssdk.KeyAttributes) (interface{}, error) {
	if m.generateKeyFunc != nil {
		return m.generateKeyFunc(ctx, attrs)
	}
	return nil, nil
}

func (m *mockKMSManager) ImportKey(ctx context.Context, keyID string, material []byte, algorithm string, _ bool) (interface{}, error) {
	if m.importKeyFunc != nil {
		return m.importKeyFunc(ctx, keyID, material, algorithm)
	}
	return nil, nil
}

func (m *mockKMSManager) GetKey(ctx context.Context, keyID string) (interface{}, error) {
	if m.getKeyFunc != nil {
		return m.getKeyFunc(ctx, keyID)
	}
	return nil, nil
}

func (m *mockKMSManager) GetPublicKey(ctx context.Context, keyID string) ([]byte, error) {
	if m.getPublicKeyFunc != nil {
		return m.getPublicKeyFunc(ctx, keyID)
	}
	return nil, nil
}

func (m *mockKMSManager) ListKeys(ctx context.Context, prefix string) ([]interface{}, error) {
	if m.listKeysFunc != nil {
		return m.listKeysFunc(ctx, prefix)
	}
	return nil, nil
}

func (m *mockKMSManager) RotateKey(ctx context.Context, keyID string) (interface{}, error) {
	if m.rotateKeyFunc != nil {
		return m.rotateKeyFunc(ctx, keyID)
	}
	return nil, nil
}

func (m *mockKMSManager) RevokeKey(ctx context.Context, keyID string) error {
	if m.revokeKeyFunc != nil {
		return m.revokeKeyFunc(ctx, keyID)
	}
	return nil
}

func (m *mockKMSManager) EnableKey(ctx context.Context, keyID string) error {
	if m.enableKeyFunc != nil {
		return m.enableKeyFunc(ctx, keyID)
	}
	return nil
}

func (m *mockKMSManager) DisableKey(ctx context.Context, keyID string) error {
	if m.disableKeyFunc != nil {
		return m.disableKeyFunc(ctx, keyID)
	}
	return nil
}

func (m *mockKMSManager) DeleteKey(ctx context.Context, keyID string) error {
	if m.deleteKeyFunc != nil {
		return m.deleteKeyFunc(ctx, keyID)
	}
	return nil
}

func (m *mockKMSManager) Encrypt(ctx context.Context, keyID string, plaintext, aad []byte) ([]byte, error) {
	if m.encryptFunc != nil {
		return m.encryptFunc(ctx, keyID, plaintext, aad)
	}
	return nil, nil
}

func (m *mockKMSManager) Decrypt(ctx context.Context, keyID string, ciphertext, aad []byte) ([]byte, error) {
	if m.decryptFunc != nil {
		return m.decryptFunc(ctx, keyID, ciphertext, aad)
	}
	return nil, nil
}

func (m *mockKMSManager) Sign(ctx context.Context, keyID string, digest []byte, algorithm string) ([]byte, error) {
	if m.signFunc != nil {
		return m.signFunc(ctx, keyID, digest, algorithm)
	}
	return nil, nil
}

func (m *mockKMSManager) Verify(ctx context.Context, keyID string, digest, signature []byte, algorithm string) (bool, error) {
	if m.verifyFunc != nil {
		return m.verifyFunc(ctx, keyID, digest, signature, algorithm)
	}
	return false, nil
}

func (m *mockKMSManager) GenerateDataKey(ctx context.Context, attrs *xkmssdk.KeyAttributes, keyBytes int) (plaintext, wrapped []byte, err error) {
	if m.generateDataKeyFunc != nil {
		return m.generateDataKeyFunc(ctx, attrs, keyBytes)
	}
	return nil, nil, nil
}

func (m *mockKMSManager) UpdateStoredMetadata(metadata interface{}) error {
	if m.updateStoredMetadataFunc != nil {
		return m.updateStoredMetadataFunc(metadata)
	}
	return nil
}

func (m *mockKMSManager) GetBackendType() config.BackendType {
	if m.getBackendTypeFunc != nil {
		return m.getBackendTypeFunc()
	}
	return config.BackendTypeSoftware
}

func (m *mockKMSManager) IsAvailable(ctx context.Context) bool {
	if m.isAvailableFunc != nil {
		return m.isAvailableFunc(ctx)
	}
	return true
}

func (m *mockKMSManager) GetSupportedAlgorithms(ctx context.Context) ([]string, error) {
	if m.getSupportedAlgorithms != nil {
		return m.getSupportedAlgorithms(ctx)
	}
	return []string{
		"AES-128-GCM", "AES-192-GCM", "AES-256-GCM",
		"RSA-2048", "RSA-3072", "RSA-4096",
		"ECDSA-P256", "ECDSA-P384", "ECDSA-P521",
		"ED25519",
	}, nil
}

func (m *mockKMSManager) Close() error {
	if m.closeFunc != nil {
		return m.closeFunc()
	}
	return nil
}

func TestGenerateKey(t *testing.T) {
	log, _ := logger.New(logger.LogConfig{Level: "debug", Format: "json", Output: "stdout"})
	now := time.Now()

	tests := []struct {
		name        string
		request     *pb.GenerateKeyRequest
		mockFunc    func(ctx context.Context, attrs *xkmssdk.KeyAttributes) (interface{}, error)
		expectError bool
		errorCode   codes.Code
	}{
		{
			name: "successful_key_generation",
			request: &pb.GenerateKeyRequest{
				Name:        "test-key",
				Description: "Test key for unit tests",
				KeySpec: &pb.KeySpec{
					Algorithm:          pb.KeyAlgorithm_KEY_ALG_AES_256_GCM,
					KeySize:            256,
					Usage:              []pb.KeyUsage{pb.KeyUsage_KEY_USAGE_ENCRYPT, pb.KeyUsage_KEY_USAGE_DECRYPT},
					Exportable:         false,
					DeletionProtection: true,
				},
				Tags:  map[string]string{"env": "test"},
				Owner: "test-user",
			},
			mockFunc: func(ctx context.Context, attrs *xkmssdk.KeyAttributes) (interface{}, error) {
				return &kms.KeyMetadata{
					KeyID:     attrs.CN,
					Name:      "test-key",
					Algorithm: kms.AlgAES256GCM,
					State:     kms.KeyStateEnabled,
					Version:   2,
					Created:   now,
					Updated:   now,
					Usage:     []kms.KeyUsage{kms.KeyUsageEncrypt, kms.KeyUsageDecrypt},
				}, nil
			},
			expectError: false,
		},
		{
			name: "missing_key_spec",
			request: &pb.GenerateKeyRequest{
				Name:        "test-key",
				Description: "Test key without spec",
			},
			mockFunc:    nil,
			expectError: true,
			errorCode:   codes.InvalidArgument,
		},
		{
			name: "service_error_unauthorized",
			request: &pb.GenerateKeyRequest{
				Name: "test-key",
				KeySpec: &pb.KeySpec{
					Algorithm: pb.KeyAlgorithm_KEY_ALG_AES_256_GCM,
					KeySize:   256,
				},
			},
			mockFunc: func(ctx context.Context, attrs *xkmssdk.KeyAttributes) (interface{}, error) {
				return nil, dberrors.New(dberrors.ErrUnauthorized, "GenerateKey", "unauthorized access")
			},
			expectError: true,
			errorCode:   codes.Unauthenticated,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockMgr := &mockKMSManager{
				generateKeyFunc: tt.mockFunc,
			}
			server := NewKMSServer(mockMgr, log)

			resp, err := server.GenerateKey(context.Background(), tt.request)

			if tt.expectError {
				require.Error(t, err)
				st, ok := status.FromError(err)
				require.True(t, ok, "error should be a gRPC status error")
				assert.Equal(t, tt.errorCode, st.Code())
			} else {
				require.NoError(t, err)
				require.NotNil(t, resp)
				assert.NotEmpty(t, resp.Key.KeyId)
				assert.NotNil(t, resp.Metadata)
			}
		})
	}
}

func TestImportKey(t *testing.T) {
	now := time.Now()

	tests := []struct {
		name        string
		request     *pb.ImportKeyRequest
		mockFunc    func(ctx context.Context, keyID string, material []byte, algorithm string) (interface{}, error)
		expectError bool
		errorCode   codes.Code
	}{
		{
			name: "successful_key_import",
			request: &pb.ImportKeyRequest{
				Name:          "imported-key",
				Description:   "Imported test key",
				KeyMaterial:   []byte("encrypted-key-material"),
				WrappingKeyId: "wrapping-key-123",
				KeySpec: &pb.KeySpec{
					Algorithm:  pb.KeyAlgorithm_KEY_ALG_RSA_2048,
					Usage:      []pb.KeyUsage{pb.KeyUsage_KEY_USAGE_SIGN, pb.KeyUsage_KEY_USAGE_VERIFY},
					Exportable: true,
				},
				Tags: map[string]string{"imported": "true"},
			},
			mockFunc: func(ctx context.Context, keyID string, material []byte, algorithm string) (interface{}, error) {
				return &kms.KeyMetadata{
					KeyID:      keyID,
					Name:       "imported-key",
					Algorithm:  kms.AlgRSA2048,
					State:      kms.KeyStateEnabled,
					Version:    1,
					Exportable: true,
					Created:    now,
					Updated:    now,
					Usage:      []kms.KeyUsage{kms.KeyUsageSign, kms.KeyUsageVerify},
				}, nil
			},
			expectError: false,
		},
		{
			name: "missing_key_material",
			request: &pb.ImportKeyRequest{
				Name: "test-key",
				KeySpec: &pb.KeySpec{
					Algorithm: pb.KeyAlgorithm_KEY_ALG_RSA_2048,
				},
			},
			mockFunc:    nil,
			expectError: true,
			errorCode:   codes.InvalidArgument,
		},
		{
			name: "missing_key_spec",
			request: &pb.ImportKeyRequest{
				Name:        "test-key",
				KeyMaterial: []byte("encrypted-key-material"),
			},
			mockFunc:    nil,
			expectError: true,
			errorCode:   codes.InvalidArgument,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockMgr := &mockKMSManager{
				importKeyFunc: tt.mockFunc,
			}
			server := newServiceBackedKMSServer(t, mockMgr)

			resp, err := server.ImportKey(importTenantContext(), tt.request)

			if tt.expectError {
				require.Error(t, err)
				st, ok := status.FromError(err)
				require.True(t, ok)
				assert.Equal(t, tt.errorCode, st.Code())
			} else {
				require.NoError(t, err)
				require.NotNil(t, resp)
				assert.NotEmpty(t, resp.Key.KeyId)
			}
		})
	}
}

func TestGetKey(t *testing.T) {
	log, _ := logger.New(logger.LogConfig{Level: "debug", Format: "json", Output: "stdout"})
	now := time.Now()

	tests := []struct {
		name        string
		request     *pb.GetKeyRequest
		mockFunc    func(ctx context.Context, keyID string) (interface{}, error)
		expectError bool
		errorCode   codes.Code
	}{
		{
			name: "successful_get_key",
			request: &pb.GetKeyRequest{
				KeyId:   "key-12345",
				Version: 1,
			},
			mockFunc: func(ctx context.Context, keyID string) (interface{}, error) {
				return &kms.KeyMetadata{
					KeyID:     keyID,
					Name:      "test-key",
					Algorithm: kms.AlgAES256GCM,
					State:     kms.KeyStateEnabled,
					Version:   2,
					Created:   now,
					Updated:   now,
				}, nil
			},
			expectError: false,
		},
		{
			name:        "missing_key_id",
			request:     &pb.GetKeyRequest{},
			mockFunc:    nil,
			expectError: true,
			errorCode:   codes.InvalidArgument,
		},
		{
			name: "key_not_found",
			request: &pb.GetKeyRequest{
				KeyId: "non-existent-key",
			},
			mockFunc: func(ctx context.Context, keyID string) (interface{}, error) {
				return nil, dberrors.New(dberrors.ErrNotFound, "GetKey", "key not found")
			},
			expectError: true,
			errorCode:   codes.NotFound,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockMgr := &mockKMSManager{
				getKeyFunc: tt.mockFunc,
			}
			server := NewKMSServer(mockMgr, log)

			resp, err := server.GetKey(context.Background(), tt.request)

			if tt.expectError {
				require.Error(t, err)
				st, ok := status.FromError(err)
				require.True(t, ok)
				assert.Equal(t, tt.errorCode, st.Code())
			} else {
				require.NoError(t, err)
				require.NotNil(t, resp)
				assert.Equal(t, "key-12345", resp.Key.KeyId)
			}
		})
	}
}

func TestListKeys(t *testing.T) {
	log, _ := logger.New(logger.LogConfig{Level: "debug", Format: "json", Output: "stdout"})
	now := time.Now()

	tests := []struct {
		name     string
		request  *pb.ListKeysRequest
		mockFunc func(ctx context.Context, prefix string) ([]interface{}, error)
	}{
		{
			name: "successful_list_keys",
			request: &pb.ListKeysRequest{
				Limit:  10,
				Cursor: "",
				Filter: map[string]string{"status": "enabled"},
			},
			mockFunc: func(ctx context.Context, prefix string) ([]interface{}, error) {
				return []interface{}{
					&kms.KeyMetadata{
						KeyID:     "key-1",
						Name:      "key-1",
						Algorithm: kms.AlgAES256GCM,
						State:     kms.KeyStateEnabled,
						Version:   2,
						Created:   now,
						Updated:   now,
					},
					&kms.KeyMetadata{
						KeyID:     "key-2",
						Name:      "key-2",
						Algorithm: kms.AlgRSA2048,
						State:     kms.KeyStateEnabled,
						Version:   2,
						Created:   now,
						Updated:   now,
					},
				}, nil
			},
		},
		{
			name: "empty_list",
			request: &pb.ListKeysRequest{
				Limit: 10,
			},
			mockFunc: func(ctx context.Context, prefix string) ([]interface{}, error) {
				return []interface{}{}, nil
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockMgr := &mockKMSManager{
				listKeysFunc: tt.mockFunc,
			}
			server := NewKMSServer(mockMgr, log)

			resp, err := server.ListKeys(context.Background(), tt.request)

			require.NoError(t, err)
			require.NotNil(t, resp)
			if tt.name == "successful_list_keys" {
				assert.Len(t, resp.Keys, 2)
			} else {
				assert.Empty(t, resp.Keys)
			}
		})
	}
}

func TestRotateKey(t *testing.T) {
	log, _ := logger.New(logger.LogConfig{Level: "debug", Format: "json", Output: "stdout"})
	now := time.Now()

	tests := []struct {
		name           string
		request        *pb.RotateKeyRequest
		getKeyFunc     func(ctx context.Context, keyID string) (interface{}, error)
		rotateKeyFunc  func(ctx context.Context, keyID string) (interface{}, error)
		expectError    bool
		errorCode      codes.Code
		expectedOldVer uint64
		expectedNewVer uint64
	}{
		{
			name: "successful_key_rotation",
			request: &pb.RotateKeyRequest{
				KeyId:            "key-12345",
				RetainOldVersion: true,
			},
			getKeyFunc: func(ctx context.Context, keyID string) (interface{}, error) {
				return &kms.KeyMetadata{
					KeyID:     keyID,
					Name:      "test-key",
					Algorithm: kms.AlgAES256GCM,
					State:     kms.KeyStateEnabled,
					Version:   1, // Old version before rotation
					Created:   now,
					Updated:   now,
				}, nil
			},
			rotateKeyFunc: func(ctx context.Context, keyID string) (interface{}, error) {
				return &kms.KeyMetadata{
					KeyID:     keyID,
					Name:      "test-key",
					Algorithm: kms.AlgAES256GCM,
					State:     kms.KeyStateEnabled,
					Version:   2, // New version after rotation
					Created:   now,
					Updated:   now,
				}, nil
			},
			expectError:    false,
			expectedOldVer: 1,
			expectedNewVer: 2,
		},
		{
			name:        "missing_key_id",
			request:     &pb.RotateKeyRequest{},
			expectError: true,
			errorCode:   codes.InvalidArgument,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockMgr := &mockKMSManager{
				getKeyFunc:    tt.getKeyFunc,
				rotateKeyFunc: tt.rotateKeyFunc,
			}
			server := NewKMSServer(mockMgr, log)

			resp, err := server.RotateKey(context.Background(), tt.request)

			if tt.expectError {
				require.Error(t, err)
				st, ok := status.FromError(err)
				require.True(t, ok)
				assert.Equal(t, tt.errorCode, st.Code())
			} else {
				require.NoError(t, err)
				require.NotNil(t, resp)
				assert.Equal(t, tt.expectedOldVer, resp.OldVersion)
				assert.Equal(t, tt.expectedNewVer, resp.NewVersion)
			}
		})
	}
}

func TestRevokeKey(t *testing.T) {
	log, _ := logger.New(logger.LogConfig{Level: "debug", Format: "json", Output: "stdout"})

	tests := []struct {
		name        string
		request     *pb.RevokeKeyRequest
		mockFunc    func(ctx context.Context, keyID string) error
		expectError bool
		errorCode   codes.Code
	}{
		{
			name: "successful_key_revocation",
			request: &pb.RevokeKeyRequest{
				KeyId:  "key-12345",
				Reason: "suspected compromise",
			},
			mockFunc: func(ctx context.Context, keyID string) error {
				return nil
			},
			expectError: false,
		},
		{
			name:        "missing_key_id",
			request:     &pb.RevokeKeyRequest{},
			mockFunc:    nil,
			expectError: true,
			errorCode:   codes.InvalidArgument,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockMgr := &mockKMSManager{
				revokeKeyFunc: tt.mockFunc,
			}
			server := NewKMSServer(mockMgr, log)

			resp, err := server.RevokeKey(context.Background(), tt.request)

			if tt.expectError {
				require.Error(t, err)
				st, ok := status.FromError(err)
				require.True(t, ok)
				assert.Equal(t, tt.errorCode, st.Code())
			} else {
				require.NoError(t, err)
				require.NotNil(t, resp)
				assert.Equal(t, pb.KeyStatus_KEY_STATUS_DISABLED, resp.Status)
			}
		})
	}
}

func TestDeleteKey(t *testing.T) {
	log, _ := logger.New(logger.LogConfig{Level: "debug", Format: "json", Output: "stdout"})

	tests := []struct {
		name        string
		request     *pb.DeleteKeyRequest
		disableFunc func(ctx context.Context, keyID string) error
		deleteFunc  func(ctx context.Context, keyID string) error
		expectError bool
		errorCode   codes.Code
	}{
		{
			name: "successful_key_deletion",
			request: &pb.DeleteKeyRequest{
				KeyId:             "key-12345",
				PendingWindowDays: 7,
				Force:             false,
			},
			disableFunc: func(ctx context.Context, keyID string) error {
				return nil
			},
			deleteFunc: func(ctx context.Context, keyID string) error {
				return nil
			},
			expectError: false,
		},
		{
			name:        "missing_key_id",
			request:     &pb.DeleteKeyRequest{},
			expectError: true,
			errorCode:   codes.InvalidArgument,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockMgr := &mockKMSManager{
				disableKeyFunc: tt.disableFunc,
				deleteKeyFunc:  tt.deleteFunc,
			}
			server := NewKMSServer(mockMgr, log)

			resp, err := server.DeleteKey(context.Background(), tt.request)

			if tt.expectError {
				require.Error(t, err)
				st, ok := status.FromError(err)
				require.True(t, ok)
				assert.Equal(t, tt.errorCode, st.Code())
			} else {
				require.NoError(t, err)
				require.NotNil(t, resp)
				assert.Equal(t, pb.KeyStatus_KEY_STATUS_PENDING_DELETION, resp.Status)
			}
		})
	}
}

func TestGetKeyMetadata(t *testing.T) {
	log, _ := logger.New(logger.LogConfig{Level: "debug", Format: "json", Output: "stdout"})
	now := time.Now()

	tests := []struct {
		name        string
		request     *pb.GetKeyMetadataRequest
		mockFunc    func(ctx context.Context, keyID string) (interface{}, error)
		expectError bool
		errorCode   codes.Code
	}{
		{
			name: "successful_get_metadata",
			request: &pb.GetKeyMetadataRequest{
				KeyId: "key-12345",
			},
			mockFunc: func(ctx context.Context, keyID string) (interface{}, error) {
				return &kms.KeyMetadata{
					KeyID:       keyID,
					Name:        "test-key",
					Description: "Test key metadata",
					Algorithm:   kms.AlgAES256GCM,
					State:       kms.KeyStateEnabled,
					Version:     1,
					Created:     now,
					Updated:     now,
					Tags:        map[string]string{"env": "test"},
				}, nil
			},
			expectError: false,
		},
		{
			name:        "missing_key_id",
			request:     &pb.GetKeyMetadataRequest{},
			mockFunc:    nil,
			expectError: true,
			errorCode:   codes.InvalidArgument,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockMgr := &mockKMSManager{
				getKeyFunc: tt.mockFunc,
			}
			server := NewKMSServer(mockMgr, log)

			resp, err := server.GetKeyMetadata(context.Background(), tt.request)

			if tt.expectError {
				require.Error(t, err)
				st, ok := status.FromError(err)
				require.True(t, ok)
				assert.Equal(t, tt.errorCode, st.Code())
			} else {
				require.NoError(t, err)
				require.NotNil(t, resp)
				assert.Equal(t, "test-key", resp.Metadata.Name)
			}
		})
	}
}

func TestUpdateKeyMetadata(t *testing.T) {
	log, _ := logger.New(logger.LogConfig{Level: "debug", Format: "json", Output: "stdout"})
	now := time.Now()

	tests := []struct {
		name                     string
		request                  *pb.UpdateKeyMetadataRequest
		getKeyFunc               func(ctx context.Context, keyID string) (interface{}, error)
		updateStoredMetadataFunc func(metadata interface{}) error
		expectError              bool
		errorCode                codes.Code
	}{
		{
			name: "successful_update_metadata",
			request: &pb.UpdateKeyMetadataRequest{
				KeyId:       "key-12345",
				Name:        "updated-key-name",
				Description: "Updated description",
				Tags:        map[string]string{"env": "production"},
			},
			getKeyFunc: func(ctx context.Context, keyID string) (interface{}, error) {
				return &kms.KeyMetadata{
					KeyID:       keyID,
					Name:        "original-name",
					Description: "Original description",
					Algorithm:   kms.AlgAES256GCM,
					State:       kms.KeyStateEnabled,
					Version:     1,
					Created:     now,
					Updated:     now,
					Tags:        map[string]string{"env": "test"},
				}, nil
			},
			updateStoredMetadataFunc: func(metadata interface{}) error {
				return nil
			},
			expectError: false,
		},
		{
			name:        "missing_key_id",
			request:     &pb.UpdateKeyMetadataRequest{},
			expectError: true,
			errorCode:   codes.InvalidArgument,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockMgr := &mockKMSManager{
				getKeyFunc:               tt.getKeyFunc,
				updateStoredMetadataFunc: tt.updateStoredMetadataFunc,
			}
			server := NewKMSServer(mockMgr, log)

			resp, err := server.UpdateKeyMetadata(context.Background(), tt.request)

			if tt.expectError {
				require.Error(t, err)
				st, ok := status.FromError(err)
				require.True(t, ok)
				assert.Equal(t, tt.errorCode, st.Code())
			} else {
				require.NoError(t, err)
				require.NotNil(t, resp)
				assert.Equal(t, "updated-key-name", resp.Metadata.Name)
			}
		})
	}
}

func TestSetKeyPolicy(t *testing.T) {
	log, _ := logger.New(logger.LogConfig{Level: "debug", Format: "json", Output: "stdout"})

	tests := []struct {
		name        string
		request     *pb.SetKeyPolicyRequest
		expectError bool
		errorCode   codes.Code
	}{
		{
			// SetKeyPolicy requires the service layer. Without it the server
			// returns Unimplemented rather than a silent stub.
			name: "successful_set_policy",
			request: &pb.SetKeyPolicyRequest{
				KeyId: "key-12345",
				Policy: &pb.KeyPolicy{
					KeyId:             "key-12345",
					Version:           1,
					AllowedOperations: []pb.KeyUsage{pb.KeyUsage_KEY_USAGE_ENCRYPT, pb.KeyUsage_KEY_USAGE_DECRYPT},
					AllowedPrincipals: []string{"user1", "user2"},
					RequireMfa:        true,
				},
			},
			expectError: true,
			errorCode:   codes.Unimplemented,
		},
		{
			name:        "missing_key_id",
			request:     &pb.SetKeyPolicyRequest{},
			expectError: true,
			errorCode:   codes.InvalidArgument,
		},
		{
			name: "missing_policy",
			request: &pb.SetKeyPolicyRequest{
				KeyId: "key-12345",
			},
			expectError: true,
			errorCode:   codes.InvalidArgument,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockMgr := &mockKMSManager{}
			server := NewKMSServer(mockMgr, log)

			resp, err := server.SetKeyPolicy(context.Background(), tt.request)

			if tt.expectError {
				require.Error(t, err)
				st, ok := status.FromError(err)
				require.True(t, ok)
				assert.Equal(t, tt.errorCode, st.Code())
			} else {
				require.NoError(t, err)
				require.NotNil(t, resp)
			}
		})
	}
}

func TestGetKeyPolicy(t *testing.T) {
	log, _ := logger.New(logger.LogConfig{Level: "debug", Format: "json", Output: "stdout"})
	now := time.Now()

	tests := []struct {
		name        string
		request     *pb.GetKeyPolicyRequest
		mockFunc    func(ctx context.Context, keyID string) (interface{}, error)
		expectError bool
		errorCode   codes.Code
	}{
		{
			name: "successful_get_policy",
			request: &pb.GetKeyPolicyRequest{
				KeyId: "key-12345",
			},
			mockFunc: func(ctx context.Context, keyID string) (interface{}, error) {
				return &kms.KeyMetadata{
					KeyID:     keyID,
					Name:      "test-key",
					Algorithm: kms.AlgAES256GCM,
					State:     kms.KeyStateEnabled,
					Version:   2,
					Created:   now,
					Updated:   now,
				}, nil
			},
			expectError: false,
		},
		{
			name:        "missing_key_id",
			request:     &pb.GetKeyPolicyRequest{},
			expectError: true,
			errorCode:   codes.InvalidArgument,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockMgr := &mockKMSManager{
				getKeyFunc: tt.mockFunc,
			}
			server := NewKMSServer(mockMgr, log)

			resp, err := server.GetKeyPolicy(context.Background(), tt.request)

			if tt.expectError {
				require.Error(t, err)
				st, ok := status.FromError(err)
				require.True(t, ok)
				assert.Equal(t, tt.errorCode, st.Code())
			} else {
				require.NoError(t, err)
				require.NotNil(t, resp)
				assert.Equal(t, uint64(1), resp.Policy.Version)
				assert.Len(t, resp.Policy.AllowedOperations, 2)
			}
		})
	}
}

func TestUpdateKeyStatus(t *testing.T) {
	log, _ := logger.New(logger.LogConfig{Level: "debug", Format: "json", Output: "stdout"})

	tests := []struct {
		name        string
		request     *pb.UpdateKeyStatusRequest
		enableFunc  func(ctx context.Context, keyID string) error
		disableFunc func(ctx context.Context, keyID string) error
		expectError bool
		errorCode   codes.Code
	}{
		{
			name: "successful_enable_key",
			request: &pb.UpdateKeyStatusRequest{
				KeyId:   "key-12345",
				Version: 1,
				Status:  pb.KeyStatus_KEY_STATUS_ENABLED,
			},
			enableFunc: func(ctx context.Context, keyID string) error {
				return nil
			},
			expectError: false,
		},
		{
			name: "successful_disable_key",
			request: &pb.UpdateKeyStatusRequest{
				KeyId:   "key-12345",
				Version: 1,
				Status:  pb.KeyStatus_KEY_STATUS_DISABLED,
			},
			disableFunc: func(ctx context.Context, keyID string) error {
				return nil
			},
			expectError: false,
		},
		{
			name:        "missing_key_id",
			request:     &pb.UpdateKeyStatusRequest{},
			expectError: true,
			errorCode:   codes.InvalidArgument,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockMgr := &mockKMSManager{
				enableKeyFunc:  tt.enableFunc,
				disableKeyFunc: tt.disableFunc,
			}
			server := NewKMSServer(mockMgr, log)

			resp, err := server.UpdateKeyStatus(context.Background(), tt.request)

			if tt.expectError {
				require.Error(t, err)
				st, ok := status.FromError(err)
				require.True(t, ok)
				assert.Equal(t, tt.errorCode, st.Code())
			} else {
				require.NoError(t, err)
				require.NotNil(t, resp)
				assert.Equal(t, tt.request.Status, resp.Status)
			}
		})
	}
}

func TestListKeyVersions(t *testing.T) {
	log, _ := logger.New(logger.LogConfig{Level: "debug", Format: "json", Output: "stdout"})
	now := time.Now()

	tests := []struct {
		name        string
		request     *pb.ListKeyVersionsRequest
		mockFunc    func(ctx context.Context, keyID string) (interface{}, error)
		expectError bool
		errorCode   codes.Code
	}{
		{
			name: "successful_list_versions",
			request: &pb.ListKeyVersionsRequest{
				KeyId: "key-12345",
			},
			mockFunc: func(ctx context.Context, keyID string) (interface{}, error) {
				return &kms.KeyMetadata{
					KeyID:     keyID,
					Name:      "test-key",
					Algorithm: kms.AlgAES256GCM,
					State:     kms.KeyStateEnabled,
					Version:   2,
					Created:   now,
					Updated:   now,
				}, nil
			},
			expectError: false,
		},
		{
			name:        "missing_key_id",
			request:     &pb.ListKeyVersionsRequest{},
			expectError: true,
			errorCode:   codes.InvalidArgument,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockMgr := &mockKMSManager{
				getKeyFunc: tt.mockFunc,
			}
			server := NewKMSServer(mockMgr, log)

			resp, err := server.ListKeyVersions(context.Background(), tt.request)

			if tt.expectError {
				require.Error(t, err)
				st, ok := status.FromError(err)
				require.True(t, ok)
				assert.Equal(t, tt.errorCode, st.Code())
			} else {
				require.NoError(t, err)
				require.NotNil(t, resp)
				assert.Equal(t, "key-12345", resp.KeyId)
				// The implementation generates version entries from 1 to currentVersion,
				// so with Version: 2, we expect 2 versions (version 1 and version 2)
				assert.Len(t, resp.Versions, 2)
				// Verify version 1 is enabled (older versions remain enabled during grace period
				// until explicitly disabled by the scheduler)
				assert.Equal(t, uint64(1), resp.Versions[0].Version)
				assert.Equal(t, pb.KeyStatus_KEY_STATUS_ENABLED, resp.Versions[0].Status)
				// Verify version 2 is enabled (current version uses current state)
				assert.Equal(t, uint64(2), resp.Versions[1].Version)
				assert.Equal(t, pb.KeyStatus_KEY_STATUS_ENABLED, resp.Versions[1].Status)
			}
		})
	}
}

func TestGetSupportedAlgorithms(t *testing.T) {
	log, _ := logger.New(logger.LogConfig{Level: "debug", Format: "json", Output: "stdout"})

	mockMgr := &mockKMSManager{
		getSupportedAlgorithms: func(ctx context.Context) ([]string, error) {
			return []string{"AES-256-GCM", "RSA-2048", "ED25519"}, nil
		},
	}
	server := NewKMSServer(mockMgr, log)

	resp, err := server.GetSupportedAlgorithms(context.Background(), &pb.GetSupportedAlgorithmsRequest{})

	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.Len(t, resp.Algorithms, 3)
	assert.Contains(t, resp.Algorithms, "AES-256-GCM")
}

func TestBackendStatus(t *testing.T) {
	log, _ := logger.New(logger.LogConfig{Level: "debug", Format: "json", Output: "stdout"})

	tests := []struct {
		name           string
		isAvailable    bool
		expectedStatus string
	}{
		{
			name:           "backend_healthy",
			isAvailable:    true,
			expectedStatus: "healthy",
		},
		{
			name:           "backend_unavailable",
			isAvailable:    false,
			expectedStatus: "unavailable",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockMgr := &mockKMSManager{
				getBackendTypeFunc: func() config.BackendType {
					return config.BackendTypeSoftware
				},
				isAvailableFunc: func(ctx context.Context) bool {
					return tt.isAvailable
				},
			}
			server := NewKMSServer(mockMgr, log)

			resp, err := server.BackendStatus(context.Background(), &pb.BackendStatusRequest{})

			require.NoError(t, err)
			require.NotNil(t, resp)
			assert.Equal(t, tt.expectedStatus, resp.Status)
			assert.Equal(t, string(config.BackendTypeSoftware), resp.BackendType)
		})
	}
}

func TestBackendTest(t *testing.T) {
	log, _ := logger.New(logger.LogConfig{Level: "debug", Format: "json", Output: "stdout"})

	tests := []struct {
		name            string
		isAvailable     bool
		expectedSuccess bool
	}{
		{
			name:            "backend_test_success",
			isAvailable:     true,
			expectedSuccess: true,
		},
		{
			name:            "backend_test_failure",
			isAvailable:     false,
			expectedSuccess: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockMgr := &mockKMSManager{
				isAvailableFunc: func(ctx context.Context) bool {
					return tt.isAvailable
				},
			}
			server := NewKMSServer(mockMgr, log)

			resp, err := server.BackendTest(context.Background(), &pb.BackendTestRequest{})

			require.NoError(t, err)
			require.NotNil(t, resp)
			assert.Equal(t, tt.expectedSuccess, resp.Success)
		})
	}
}

// --- Merged from server_kms_coverage_test.go ---

// =============================================================================
// kmsStateToProtoStatus Tests
// =============================================================================

func TestKmsStateToProtoStatus(t *testing.T) {
	tests := []struct {
		name     string
		input    kms.KeyState
		expected pb.KeyStatus
	}{
		{
			name:     "enabled state",
			input:    kms.KeyStateEnabled,
			expected: pb.KeyStatus_KEY_STATUS_ENABLED,
		},
		{
			name:     "disabled state",
			input:    kms.KeyStateDisabled,
			expected: pb.KeyStatus_KEY_STATUS_DISABLED,
		},
		{
			name:     "pending deletion state",
			input:    kms.KeyStatePendingDeletion,
			expected: pb.KeyStatus_KEY_STATUS_PENDING_DELETION,
		},
		{
			name:     "deleted state",
			input:    kms.KeyStateDeleted,
			expected: pb.KeyStatus_KEY_STATUS_DELETED,
		},
		{
			name:     "unknown state defaults to enabled",
			input:    kms.KeyState("unknown"),
			expected: pb.KeyStatus_KEY_STATUS_ENABLED,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := kmsStateToProtoStatus(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

// =============================================================================
// kmsUsageToProtoUsage Tests
// =============================================================================

func TestKmsUsageToProtoUsage(t *testing.T) {
	tests := []struct {
		name     string
		input    kms.KeyUsage
		expected pb.KeyUsage
	}{
		{
			name:     "encrypt usage",
			input:    kms.KeyUsageEncrypt,
			expected: pb.KeyUsage_KEY_USAGE_ENCRYPT,
		},
		{
			name:     "decrypt usage",
			input:    kms.KeyUsageDecrypt,
			expected: pb.KeyUsage_KEY_USAGE_DECRYPT,
		},
		{
			name:     "sign usage",
			input:    kms.KeyUsageSign,
			expected: pb.KeyUsage_KEY_USAGE_SIGN,
		},
		{
			name:     "verify usage",
			input:    kms.KeyUsageVerify,
			expected: pb.KeyUsage_KEY_USAGE_VERIFY,
		},
		{
			name:     "wrap usage",
			input:    kms.KeyUsageWrap,
			expected: pb.KeyUsage_KEY_USAGE_WRAP_KEY,
		},
		{
			name:     "unwrap usage maps to wrap_key",
			input:    kms.KeyUsageUnwrap,
			expected: pb.KeyUsage_KEY_USAGE_WRAP_KEY,
		},
		{
			name:     "derive_key usage",
			input:    kms.KeyUsageDeriveKey,
			expected: pb.KeyUsage_KEY_USAGE_DERIVE_KEY,
		},
		{
			name:     "unknown usage defaults to encrypt",
			input:    kms.KeyUsage("unknown"),
			expected: pb.KeyUsage_KEY_USAGE_ENCRYPT,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := kmsUsageToProtoUsage(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

// =============================================================================
// kmsUsageListToProto Tests
// =============================================================================

func TestKmsUsageListToProto(t *testing.T) {
	tests := []struct {
		name     string
		input    []kms.KeyUsage
		expected []pb.KeyUsage
	}{
		{
			name:     "empty list",
			input:    []kms.KeyUsage{},
			expected: []pb.KeyUsage{},
		},
		{
			name:     "single usage",
			input:    []kms.KeyUsage{kms.KeyUsageEncrypt},
			expected: []pb.KeyUsage{pb.KeyUsage_KEY_USAGE_ENCRYPT},
		},
		{
			name:  "multiple usages",
			input: []kms.KeyUsage{kms.KeyUsageEncrypt, kms.KeyUsageDecrypt, kms.KeyUsageSign},
			expected: []pb.KeyUsage{
				pb.KeyUsage_KEY_USAGE_ENCRYPT,
				pb.KeyUsage_KEY_USAGE_DECRYPT,
				pb.KeyUsage_KEY_USAGE_SIGN,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := kmsUsageListToProto(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

// =============================================================================
// convertKMSKeyMetadataFull Tests
// =============================================================================

func TestConvertKMSKeyMetadataFull(t *testing.T) {
	now := time.Now()
	lastUsed := now.Add(-time.Hour)

	t.Run("with_last_used", func(t *testing.T) {
		metadata := &kms.KeyMetadata{
			KeyID:              "key-123",
			Name:               "test-key",
			Description:        "test description",
			Algorithm:          "AES-256-GCM",
			State:              kms.KeyStateEnabled,
			Usage:              []kms.KeyUsage{kms.KeyUsageEncrypt, kms.KeyUsageDecrypt},
			Version:            3,
			Exportable:         true,
			DeletionProtection: true,
			Created:            now.Add(-24 * time.Hour),
			Updated:            now,
			LastUsedAt:         &lastUsed,
			RotationPolicy: &kms.RotationPolicy{
				AutoRotate:     true,
				Enabled:        true,
				Interval:       90 * 24 * time.Hour,
				NextRotation:   now.Add(30 * 24 * time.Hour),
				RetainVersions: 3,
			},
			Tags:  map[string]string{"env": "test"},
			Owner: "test-owner",
		}

		result := convertKMSKeyMetadataFull(&responseNarrower{}, metadata)

		require.NotNil(t, result)
		assert.Equal(t, "key-123", result.KeyId)
		assert.Equal(t, "test-key", result.Name)
		assert.Equal(t, "test description", result.Description)
		assert.Equal(t, pb.KeyStatus_KEY_STATUS_ENABLED, result.Status)
		assert.Equal(t, uint64(3), result.Version)
		assert.True(t, result.Exportable)
		assert.True(t, result.DeletionProtection)
		assert.Equal(t, lastUsed.Unix(), result.LastUsedAt)
		assert.Equal(t, map[string]string{"env": "test"}, result.Tags)
		assert.Equal(t, "test-owner", result.Owner)
	})

	t.Run("without_last_used", func(t *testing.T) {
		metadata := &kms.KeyMetadata{
			KeyID:     "key-456",
			Name:      "test-key-2",
			Algorithm: "RSA-2048",
			State:     kms.KeyStateDisabled,
			Usage:     []kms.KeyUsage{kms.KeyUsageSign},
			Version:   1,
			Created:   now,
			Updated:   now,
		}

		result := convertKMSKeyMetadataFull(&responseNarrower{}, metadata)

		require.NotNil(t, result)
		assert.Equal(t, "key-456", result.KeyId)
		assert.Equal(t, pb.KeyStatus_KEY_STATUS_DISABLED, result.Status)
		assert.Equal(t, int64(0), result.LastUsedAt)
	})

	t.Run("nil_rotation_policy", func(t *testing.T) {
		metadata := &kms.KeyMetadata{
			KeyID:          "key-789",
			Name:           "test-key-3",
			Algorithm:      "ChaCha20-Poly1305",
			State:          kms.KeyStateEnabled,
			Usage:          []kms.KeyUsage{kms.KeyUsageEncrypt},
			Version:        1,
			Created:        now,
			Updated:        now,
			RotationPolicy: nil,
		}

		result := convertKMSKeyMetadataFull(&responseNarrower{}, metadata)

		require.NotNil(t, result)
		assert.Nil(t, result.RotationPolicy)
	})
}

// =============================================================================
// convertKMSRotationPolicy Tests
// =============================================================================

func TestConvertKMSRotationPolicy(t *testing.T) {
	now := time.Now()

	t.Run("nil_policy", func(t *testing.T) {
		result := convertKMSRotationPolicy(&responseNarrower{}, nil)
		assert.Nil(t, result)
	})

	t.Run("with_policy", func(t *testing.T) {
		policy := &kms.RotationPolicy{
			AutoRotate:     true,
			Enabled:        true,
			Interval:       90 * 24 * time.Hour,
			NextRotation:   now.Add(30 * 24 * time.Hour),
			RetainVersions: 5,
		}

		result := convertKMSRotationPolicy(&responseNarrower{}, policy)

		require.NotNil(t, result)
		assert.True(t, result.AutoRotate)
	})
}

// =============================================================================
// convertKMSError Tests
// =============================================================================

func TestConvertKMSError(t *testing.T) {
	t.Run("nil_error", func(t *testing.T) {
		result := convertKMSError(nil)
		assert.Nil(t, result)
	})

	t.Run("not_found_error", func(t *testing.T) {
		dErr := &dberrors.QBError{
			Code: dberrors.ErrNotFound,
			Op:   "kms.GetKey",
			Err:  stderrors.New("key not found"),
		}
		result := convertKMSError(dErr)
		require.Error(t, result)
	})

	t.Run("already_exists_error", func(t *testing.T) {
		dErr := &dberrors.QBError{
			Code: dberrors.ErrAlreadyExists,
			Op:   "kms.CreateKey",
			Err:  stderrors.New("key already exists"),
		}
		result := convertKMSError(dErr)
		require.Error(t, result)
	})

	t.Run("invalid_input_error", func(t *testing.T) {
		dErr := &dberrors.QBError{
			Code: dberrors.ErrInvalidInput,
			Op:   "kms.CreateKey",
			Err:  stderrors.New("invalid key id"),
		}
		result := convertKMSError(dErr)
		require.Error(t, result)
	})

	t.Run("unauthorized_error", func(t *testing.T) {
		dErr := &dberrors.QBError{
			Code: dberrors.ErrUnauthorized,
			Op:   "kms.GetKey",
			Err:  stderrors.New("not authorized"),
		}
		result := convertKMSError(dErr)
		require.Error(t, result)
	})

	t.Run("forbidden_error", func(t *testing.T) {
		dErr := &dberrors.QBError{
			Code: dberrors.ErrForbidden,
			Op:   "kms.DeleteKey",
			Err:  stderrors.New("forbidden"),
		}
		result := convertKMSError(dErr)
		require.Error(t, result)
	})

	t.Run("generic_error", func(t *testing.T) {
		result := convertKMSError(assert.AnError)
		require.Error(t, result)
	})
}

// --- Merged from server_kms_edge_coverage_test.go ---

// newMockKeyMetadata creates a minimal kms.KeyMetadata for testing.
func newMockKeyMetadata(keyID string, now time.Time) *kms.KeyMetadata {
	return &kms.KeyMetadata{
		KeyID:     keyID,
		Name:      keyID,
		Algorithm: kms.AlgAES256GCM,
		State:     kms.KeyStateEnabled,
		Version:   2,
		Created:   now,
		Updated:   now,
		Usage:     []kms.KeyUsage{kms.KeyUsageEncrypt, kms.KeyUsageDecrypt},
	}
}

func newTestKMSServer(t *testing.T, mgr *mockKMSManager) *KMSServer {
	t.Helper()
	log, err := logger.New(logger.LogConfig{Level: "error", Format: "json", Output: "stderr"})
	require.NoError(t, err)
	return NewKMSServer(mgr, log)
}

// --- GenerateKey edge cases ---

func TestGenerateKey_EmptyName(t *testing.T) {
	srv := newTestKMSServer(t, &mockKMSManager{})
	_, err := srv.GenerateKey(context.Background(), &pb.GenerateKeyRequest{
		Name: "   ",
		KeySpec: &pb.KeySpec{
			Algorithm: pb.KeyAlgorithm_KEY_ALG_AES_256_GCM,
		},
	})
	require.Error(t, err)
	assert.Equal(t, codes.InvalidArgument, status.Code(err))
}

// The duplicate-name check is tenant scoped and lives in the service layer;
// the transport maps the service's conflict to AlreadyExists.
func TestGenerateKey_DuplicateKeyName(t *testing.T) {
	log, err := logger.New(logger.LogConfig{Level: "error", Format: "json", Output: "stdout"})
	require.NoError(t, err)
	svc := &mocks.MockKMSService{
		GenerateKeyFunc: func(_ context.Context, _ *service.GenerateKeyRequest) (*service.GenerateKeyResponse, error) {
			return nil, dberrors.New(dberrors.ErrConflict, "kms.GenerateKey", "key already exists")
		},
	}
	srv := NewKMSServerWithService(&mockKMSManager{}, svc, log)

	_, err = srv.GenerateKey(context.Background(), &pb.GenerateKeyRequest{
		Name: "dup-key",
		KeySpec: &pb.KeySpec{
			Algorithm: pb.KeyAlgorithm_KEY_ALG_AES_256_GCM,
		},
	})
	require.Error(t, err)
	assert.Equal(t, codes.AlreadyExists, status.Code(err))
}

// The transport keeps no name registry of its own: a name the service accepts
// (for example the same name in two tenants) is never refused here.
func TestGenerateKey_SameNameTwiceDefersToService(t *testing.T) {
	log, err := logger.New(logger.LogConfig{Level: "error", Format: "json", Output: "stdout"})
	require.NoError(t, err)
	calls := 0
	svc := &mocks.MockKMSService{
		GenerateKeyFunc: func(_ context.Context, req *service.GenerateKeyRequest) (*service.GenerateKeyResponse, error) {
			calls++
			return &service.GenerateKeyResponse{
				Key:       &service.KeyHandle{KeyID: req.Name, Version: 1, Algorithm: service.KeyAlgAES256GCM, Status: service.KeyStatusEnabled},
				Metadata:  &service.KeyMetadata{KeyID: req.Name, Name: req.Name, Algorithm: service.KeyAlgAES256GCM, Status: service.KeyStatusEnabled, Version: 1},
				Timestamp: time.Now(),
			}, nil
		},
	}
	srv := NewKMSServerWithService(&mockKMSManager{}, svc, log)
	req := &pb.GenerateKeyRequest{
		Name:    "shared-name",
		KeySpec: &pb.KeySpec{Algorithm: pb.KeyAlgorithm_KEY_ALG_AES_256_GCM},
	}

	_, err = srv.GenerateKey(context.Background(), req)
	require.NoError(t, err)
	_, err = srv.GenerateKey(context.Background(), req)
	require.NoError(t, err)
	assert.Equal(t, 2, calls)
}

func TestGenerateKey_TypeAssertionFailure(t *testing.T) {
	mgr := &mockKMSManager{
		generateKeyFunc: func(_ context.Context, _ *xkmssdk.KeyAttributes) (interface{}, error) {
			return "not-a-key-metadata", nil
		},
	}
	srv := newTestKMSServer(t, mgr)

	_, err := srv.GenerateKey(context.Background(), &pb.GenerateKeyRequest{
		Name: "type-fail-key",
		KeySpec: &pb.KeySpec{
			Algorithm: pb.KeyAlgorithm_KEY_ALG_AES_256_GCM,
			KeySize:   256,
		},
	})
	require.Error(t, err)
	assert.Equal(t, codes.Internal, status.Code(err))
	assert.Contains(t, status.Convert(err).Message(), "unexpected response type")
}

func TestGenerateKey_UpdateMetadataError(t *testing.T) {
	now := time.Now()
	mgr := &mockKMSManager{
		generateKeyFunc: func(_ context.Context, attrs *xkmssdk.KeyAttributes) (interface{}, error) {
			return newMockKeyMetadata(attrs.CN, now), nil
		},
		updateStoredMetadataFunc: func(_ interface{}) error {
			return dberrors.New(dberrors.ErrInternal, "update", "persist failed")
		},
	}
	srv := newTestKMSServer(t, mgr)

	resp, err := srv.GenerateKey(context.Background(), &pb.GenerateKeyRequest{
		Name:        "metadata-err-key",
		Description: "test",
		KeySpec: &pb.KeySpec{
			Algorithm: pb.KeyAlgorithm_KEY_ALG_AES_256_GCM,
			KeySize:   256,
		},
	})
	// UpdateStoredMetadata error is logged but doesn't fail the request
	require.NoError(t, err)
	assert.NotNil(t, resp)
}

// --- ImportKey edge cases ---

// --- GetKey edge cases ---

func TestGetKey_TypeAssertionFailure(t *testing.T) {
	mgr := &mockKMSManager{
		getKeyFunc: func(_ context.Context, _ string) (interface{}, error) {
			return "not-a-key-metadata", nil
		},
	}
	srv := newTestKMSServer(t, mgr)

	_, err := srv.GetKey(context.Background(), &pb.GetKeyRequest{
		KeyId: "type-fail-key",
	})
	require.Error(t, err)
	assert.Equal(t, codes.Internal, status.Code(err))
}

func TestGetKey_LastUsedAt(t *testing.T) {
	now := time.Now()
	mgr := &mockKMSManager{
		getKeyFunc: func(_ context.Context, _ string) (interface{}, error) {
			meta := newMockKeyMetadata("test-key", now)
			meta.LastUsedAt = &now
			return meta, nil
		},
	}
	srv := newTestKMSServer(t, mgr)

	resp, err := srv.GetKey(context.Background(), &pb.GetKeyRequest{
		KeyId:   "test-key",
		Version: 0,
	})
	require.NoError(t, err)
	assert.NotNil(t, resp)
}

// --- RotateKey edge cases ---

func TestRotateKey_GetKeyError(t *testing.T) {
	mgr := &mockKMSManager{
		getKeyFunc: func(_ context.Context, _ string) (interface{}, error) {
			return nil, dberrors.New(dberrors.ErrNotFound, "GetKey", "key not found")
		},
	}
	srv := newTestKMSServer(t, mgr)

	_, err := srv.RotateKey(context.Background(), &pb.RotateKeyRequest{
		KeyId: "missing-key",
	})
	require.Error(t, err)
	assert.Equal(t, codes.NotFound, status.Code(err))
}

func TestRotateKey_OldKeyTypeAssertionFailure(t *testing.T) {
	mgr := &mockKMSManager{
		getKeyFunc: func(_ context.Context, _ string) (interface{}, error) {
			return "not-a-key-metadata", nil
		},
	}
	srv := newTestKMSServer(t, mgr)

	_, err := srv.RotateKey(context.Background(), &pb.RotateKeyRequest{
		KeyId: "rotate-fail-key",
	})
	require.Error(t, err)
	assert.Equal(t, codes.Internal, status.Code(err))
}

func TestRotateKey_NewKeyTypeAssertionFailure(t *testing.T) {
	now := time.Now()
	mgr := &mockKMSManager{
		getKeyFunc: func(_ context.Context, _ string) (interface{}, error) {
			return newMockKeyMetadata("rotate-key", now), nil
		},
		rotateKeyFunc: func(_ context.Context, _ string) (interface{}, error) {
			return "not-a-key-metadata", nil
		},
	}
	srv := newTestKMSServer(t, mgr)

	_, err := srv.RotateKey(context.Background(), &pb.RotateKeyRequest{
		KeyId: "rotate-key",
	})
	require.Error(t, err)
	assert.Equal(t, codes.Internal, status.Code(err))
}

// --- GetKeyMetadata edge cases ---

func TestGetKeyMetadata_TypeAssertionFailure(t *testing.T) {
	mgr := &mockKMSManager{
		getKeyFunc: func(_ context.Context, _ string) (interface{}, error) {
			return "not-a-key-metadata", nil
		},
	}
	srv := newTestKMSServer(t, mgr)

	_, err := srv.GetKeyMetadata(context.Background(), &pb.GetKeyMetadataRequest{
		KeyId: "meta-fail-key",
	})
	require.Error(t, err)
	assert.Equal(t, codes.Internal, status.Code(err))
}

// --- UpdateKeyMetadata edge cases ---

func TestUpdateKeyMetadata_GetKeyError(t *testing.T) {
	mgr := &mockKMSManager{
		getKeyFunc: func(_ context.Context, _ string) (interface{}, error) {
			return nil, dberrors.New(dberrors.ErrNotFound, "GetKey", "key not found")
		},
	}
	srv := newTestKMSServer(t, mgr)

	_, err := srv.UpdateKeyMetadata(context.Background(), &pb.UpdateKeyMetadataRequest{
		KeyId: "missing-key",
	})
	require.Error(t, err)
	assert.Equal(t, codes.NotFound, status.Code(err))
}

func TestUpdateKeyMetadata_TypeAssertionFailure(t *testing.T) {
	mgr := &mockKMSManager{
		getKeyFunc: func(_ context.Context, _ string) (interface{}, error) {
			return "not-a-key-metadata", nil
		},
	}
	srv := newTestKMSServer(t, mgr)

	_, err := srv.UpdateKeyMetadata(context.Background(), &pb.UpdateKeyMetadataRequest{
		KeyId: "update-meta-fail",
		Name:  "new-name",
	})
	require.Error(t, err)
	assert.Equal(t, codes.Internal, status.Code(err))
}

// --- ListKeyVersions edge cases ---

func TestListKeyVersions_TypeAssertionFailure(t *testing.T) {
	mgr := &mockKMSManager{
		getKeyFunc: func(_ context.Context, _ string) (interface{}, error) {
			return "not-a-key-metadata", nil
		},
	}
	srv := newTestKMSServer(t, mgr)

	_, err := srv.ListKeyVersions(context.Background(), &pb.ListKeyVersionsRequest{
		KeyId: "version-fail-key",
	})
	require.Error(t, err)
	assert.Equal(t, codes.Internal, status.Code(err))
}

func TestListKeyVersions_ManyVersionsPaginated(t *testing.T) {
	now := time.Now()
	mgr := &mockKMSManager{
		getKeyFunc: func(_ context.Context, _ string) (interface{}, error) {
			meta := newMockKeyMetadata("many-versions", now)
			meta.Version = 150
			return meta, nil
		},
	}
	srv := newTestKMSServer(t, mgr)

	resp, err := srv.ListKeyVersions(context.Background(), &pb.ListKeyVersionsRequest{
		KeyId: "many-versions",
		Limit: 0,
	})
	require.NoError(t, err)
	assert.Len(t, resp.Versions, 100)
	assert.True(t, resp.HasMore)
	assert.NotEmpty(t, resp.NextCursor)
}

// --- ListKeys fallback error ---

func TestListKeys_FallbackError(t *testing.T) {
	mgr := &mockKMSManager{
		listKeysFunc: func(_ context.Context, _ string) ([]interface{}, error) {
			return nil, dberrors.New(dberrors.ErrInternal, "ListKeys", "storage error")
		},
	}
	srv := newTestKMSServer(t, mgr)

	_, err := srv.ListKeys(context.Background(), &pb.ListKeysRequest{})
	require.Error(t, err)
}

// Signer returns a crypto.Signer for the given keyID. Tests that don't
// exercise the signer path receive a nil signer and no error; tests
// that do can stub this by redefining the method on a wrapped type.
func (m *mockKMSManager) Signer(_ context.Context, _ string) (crypto.Signer, error) {
	return nil, nil
}
