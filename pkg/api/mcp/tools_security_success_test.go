//go:build mcp

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

package mcp

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/kms"
	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
	"github.com/jeremyhahn/go-quicbaas/pkg/testutil"
)

// TestKMSToolsSuccess tests successful execution paths for all KMS tools
// using the KMSManager interface.
func TestKMSToolsSuccess(t *testing.T) {
	tests := []struct {
		name       string
		toolName   string
		args       map[string]interface{}
		setupMock  func(*MockKMSManager)
		validateFn func(*testing.T, interface{}, error)
	}{
		{
			name:     "GenerateKey success",
			toolName: "quicbaas_kms_generate_key",
			args: map[string]interface{}{
				"algorithm": "AES-256-GCM",
				"name":      "test-key",
				"usage": []interface{}{
					"encrypt", "decrypt",
				},
				"exportable":          false,
				"deletion_protection": true,
			},
			setupMock: func(m *MockKMSManager) {
				m.On("GenerateKey", mock.Anything, mock.MatchedBy(func(attrs interface{}) bool {
					// KeyAttributes is passed to GenerateKey
					return attrs != nil
				})).Return(createTestKeyMetadata("key-123", "test-key", "AES-256-GCM"), nil)
			},
			validateFn: func(t *testing.T, result interface{}, err error) {
				assert.NoError(t, err)
				assert.NotNil(t, result)
			},
		},
		{
			name:     "ImportKey success",
			toolName: "quicbaas_kms_import_key",
			args: map[string]interface{}{
				"key_material":    "base64encodedkey",
				"wrapping_key_id": "wrap-key-456",
				"algorithm":       "RSA-2048",
				"name":            "imported-key",
			},
			setupMock: func(m *MockKMSManager) {
				m.On("ImportKey", mock.Anything, mock.AnythingOfType("string"),
					mock.Anything, "RSA-2048").Return(
					createTestKeyMetadata("key-789", "imported-key", "RSA-2048"), nil)
			},
			validateFn: func(t *testing.T, result interface{}, err error) {
				assert.NoError(t, err)
				assert.NotNil(t, result)
			},
		},
		{
			name:     "GetKey success",
			toolName: "quicbaas_kms_get_key",
			args: map[string]interface{}{
				"key_id": "key-123",
			},
			setupMock: func(m *MockKMSManager) {
				m.On("GetKey", mock.Anything, "key-123").Return(
					createTestKeyMetadata("key-123", "test-key", "AES-256-GCM"), nil)
			},
			validateFn: func(t *testing.T, result interface{}, err error) {
				assert.NoError(t, err)
				assert.NotNil(t, result)
			},
		},
		{
			name:     "ListKeys success",
			toolName: "quicbaas_kms_list_keys",
			args: map[string]interface{}{
				"limit": float64(50),
			},
			setupMock: func(m *MockKMSManager) {
				m.On("ListKeys", mock.Anything, "").Return(
					[]interface{}{
						createTestKeyMetadata("key-1", "key-one", "AES-256-GCM"),
						createTestKeyMetadata("key-2", "key-two", "RSA-2048"),
					}, nil)
			},
			validateFn: func(t *testing.T, result interface{}, err error) {
				assert.NoError(t, err)
				assert.NotNil(t, result)
			},
		},
		{
			name:     "RotateKey success",
			toolName: "quicbaas_kms_rotate_key",
			args: map[string]interface{}{
				"key_id":             "key-123",
				"retain_old_version": true,
			},
			setupMock: func(m *MockKMSManager) {
				meta := createTestKeyMetadata("key-123", "test-key", "AES-256-GCM")
				meta.Version = 1
				m.On("GetKey", mock.Anything, "key-123").Return(meta, nil).Once()
				rotatedMeta := createTestKeyMetadata("key-123", "test-key", "AES-256-GCM")
				rotatedMeta.Version = 2
				m.On("RotateKey", mock.Anything, "key-123").Return(rotatedMeta, nil)
			},
			validateFn: func(t *testing.T, result interface{}, err error) {
				assert.NoError(t, err)
				assert.NotNil(t, result)
			},
		},
		{
			name:     "RevokeKey success",
			toolName: "quicbaas_kms_revoke_key",
			args: map[string]interface{}{
				"key_id": "key-123",
				"reason": "compromised",
			},
			setupMock: func(m *MockKMSManager) {
				m.On("RevokeKey", mock.Anything, "key-123").Return(nil)
			},
			validateFn: func(t *testing.T, result interface{}, err error) {
				assert.NoError(t, err)
				assert.NotNil(t, result)
			},
		},
		{
			name:     "DeleteKey success",
			toolName: "quicbaas_kms_delete_key",
			args: map[string]interface{}{
				"key_id":              "key-123",
				"pending_window_days": float64(7),
				"force":               false,
			},
			setupMock: func(m *MockKMSManager) {
				m.On("DisableKey", mock.Anything, "key-123").Return(nil)
				m.On("DeleteKey", mock.Anything, "key-123").Return(nil)
			},
			validateFn: func(t *testing.T, result interface{}, err error) {
				assert.NoError(t, err)
				assert.NotNil(t, result)
			},
		},
		{
			name:     "GetKeyMetadata success",
			toolName: "quicbaas_kms_get_metadata",
			args: map[string]interface{}{
				"key_id": "key-123",
			},
			setupMock: func(m *MockKMSManager) {
				m.On("GetKey", mock.Anything, "key-123").Return(
					createTestKeyMetadata("key-123", "test-key", "AES-256-GCM"), nil)
			},
			validateFn: func(t *testing.T, result interface{}, err error) {
				assert.NoError(t, err)
				assert.NotNil(t, result)
			},
		},
		{
			name:     "UpdateKeyMetadata success",
			toolName: "quicbaas_kms_update_metadata",
			args: map[string]interface{}{
				"key_id": "key-123",
				"tags": map[string]interface{}{
					"env": "staging",
				},
			},
			setupMock: func(m *MockKMSManager) {
				m.On("GetKey", mock.Anything, "key-123").Return(
					createTestKeyMetadata("key-123", "test-key", "AES-256-GCM"), nil)
				m.On("UpdateStoredMetadata", mock.Anything).Return(nil)
			},
			validateFn: func(t *testing.T, result interface{}, err error) {
				assert.NoError(t, err)
				assert.NotNil(t, result)
			},
		},
		{
			name:     "SetKeyPolicy success",
			toolName: "quicbaas_kms_set_policy",
			args: map[string]interface{}{
				"key_id": "key-123",
				"policy": map[string]interface{}{
					"version": "1.0",
				},
			},
			setupMock: func(m *MockKMSManager) {
				m.On("GetKey", mock.Anything, "key-123").Return(
					createTestKeyMetadata("key-123", "test-key", "AES-256-GCM"), nil)
			},
			validateFn: func(t *testing.T, result interface{}, err error) {
				assert.NoError(t, err)
				assert.NotNil(t, result)
			},
		},
		{
			name:     "GetKeyPolicy success",
			toolName: "quicbaas_kms_get_policy",
			args: map[string]interface{}{
				"key_id": "key-123",
			},
			setupMock: func(m *MockKMSManager) {
				m.On("GetKey", mock.Anything, "key-123").Return(
					createTestKeyMetadata("key-123", "test-key", "AES-256-GCM"), nil)
			},
			validateFn: func(t *testing.T, result interface{}, err error) {
				assert.NoError(t, err)
				assert.NotNil(t, result)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			log, err := logger.New(logger.LogConfig{
				Level:     "error",
				Format:    "json",
				Output:    "stdout",
				Component: "mcp-test",
			})
			require.NoError(t, err)

			mockKMS := NewMockKMSManager()
			tt.setupMock(mockKMS)

			mockSvc := newTestQuicBaaSWithKMSManager(mockKMS)
			registry := NewToolRegistry(mockSvc, log)

			ctx := context.Background()
			result, err := registry.ExecuteTool(ctx, tt.toolName, tt.args)
			tt.validateFn(t, result, err)

			mockKMS.AssertExpectations(t)
		})
	}
}

// TestSealToolsSuccess tests successful execution paths for all Seal tools.
func TestSealToolsSuccess(t *testing.T) {
	tests := []struct {
		name       string
		toolName   string
		args       map[string]interface{}
		setupMock  func(*testutil.MockQuicBaaS)
		validateFn func(*testing.T, interface{}, error)
	}{
		{
			name:     "SealStatus success",
			toolName: "quicbaas_seal_status",
			args:     map[string]interface{}{},
			setupMock: func(m *testutil.MockQuicBaaS) {
				m.On("Status", mock.Anything).Return(&service.SealStatusResponse{
					Sealed:      false,
					Initialized: true,
					Mode:        "manual",
					Threshold:   3,
					TotalShares: 5,
				}, nil)
			},
			validateFn: func(t *testing.T, result interface{}, err error) {
				assert.NoError(t, err)
				assert.NotNil(t, result)
			},
		},
		{
			name:     "SealInit success",
			toolName: "quicbaas_seal_init",
			args: map[string]interface{}{
				"threshold":    float64(3),
				"total_shares": float64(5),
			},
			setupMock: func(m *testutil.MockQuicBaaS) {
				m.On("Initialize", mock.Anything, mock.MatchedBy(func(req *service.SealInitRequest) bool {
					return req.Threshold == 3 && req.TotalShares == 5
				})).Return(&service.SealInitResponse{
					Shares:    [][]byte{[]byte("s1"), []byte("s2"), []byte("s3"), []byte("s4"), []byte("s5")},
					RootToken: "root-token",
					Version:   1,
					Timestamp: time.Now(),
				}, nil)
			},
			validateFn: func(t *testing.T, result interface{}, err error) {
				assert.NoError(t, err)
				assert.NotNil(t, result)
			},
		},
		{
			name:     "Unseal success",
			toolName: "quicbaas_unseal",
			args: map[string]interface{}{
				"share": "test-share",
			},
			setupMock: func(m *testutil.MockQuicBaaS) {
				m.On("Unseal", mock.Anything, mock.MatchedBy(func(req *service.UnsealRequest) bool {
					return string(req.Share) == "test-share"
				})).Return(&service.UnsealResponse{
					Sealed:         false,
					SharesProvided: 3,
					Threshold:      3,
					Timestamp:      time.Now(),
				}, nil)
			},
			validateFn: func(t *testing.T, result interface{}, err error) {
				assert.NoError(t, err)
				assert.NotNil(t, result)
			},
		},
		{
			name:     "Seal success",
			toolName: "quicbaas_seal",
			args:     map[string]interface{}{},
			setupMock: func(m *testutil.MockQuicBaaS) {
				m.On("Seal", mock.Anything).Return(nil)
			},
			validateFn: func(t *testing.T, result interface{}, err error) {
				assert.NoError(t, err)
				assert.NotNil(t, result)
				resultMap, ok := result.(map[string]interface{})
				assert.True(t, ok)
				assert.True(t, resultMap["sealed"].(bool))
			},
		},
		{
			name:     "Rekey success",
			toolName: "quicbaas_seal_rekey",
			args: map[string]interface{}{
				"new_threshold":    float64(4),
				"new_total_shares": float64(7),
			},
			setupMock: func(m *testutil.MockQuicBaaS) {
				m.On("Rekey", mock.Anything, mock.MatchedBy(func(req *service.RekeyRequest) bool {
					return req.NewThreshold == 4 && req.NewTotalShares == 7
				})).Return(&service.RekeyResponse{
					Shares:    [][]byte{[]byte("n1"), []byte("n2"), []byte("n3"), []byte("n4"), []byte("n5"), []byte("n6"), []byte("n7")},
					Version:   2,
					Timestamp: time.Now(),
				}, nil)
			},
			validateFn: func(t *testing.T, result interface{}, err error) {
				assert.NoError(t, err)
				assert.NotNil(t, result)
			},
		},
		{
			name:     "GenerateRecoveryKey success",
			toolName: "quicbaas_seal_generate_recovery_key",
			args: map[string]interface{}{
				"recovery_threshold":  float64(2),
				"recovery_total_keys": float64(3),
			},
			setupMock: func(m *testutil.MockQuicBaaS) {
				m.On("GenerateRecoveryKey", mock.Anything, mock.MatchedBy(func(req *service.GenerateRecoveryKeyRequest) bool {
					return req.RecoveryThreshold == 2 && req.RecoveryTotalKeys == 3
				})).Return(&service.GenerateRecoveryKeyResponse{
					RecoveryKeys: [][]byte{[]byte("rec1"), []byte("rec2"), []byte("rec3")},
					Timestamp:    time.Now(),
				}, nil)
			},
			validateFn: func(t *testing.T, result interface{}, err error) {
				assert.NoError(t, err)
				assert.NotNil(t, result)
			},
		},
		{
			name:     "DeleteRecoveryKey success",
			toolName: "quicbaas_seal_delete_recovery_key",
			args: map[string]interface{}{
				"confirm": true,
			},
			setupMock: func(m *testutil.MockQuicBaaS) {
				m.On("DeleteRecoveryKey", mock.Anything, mock.MatchedBy(func(req *service.DeleteRecoveryKeyRequest) bool {
					return req.Confirm
				})).Return(&service.DeleteRecoveryKeyResponse{
					Deleted:   true,
					Timestamp: time.Now(),
				}, nil)
			},
			validateFn: func(t *testing.T, result interface{}, err error) {
				assert.NoError(t, err)
				assert.NotNil(t, result)
			},
		},
		{
			name:     "GenerateRootToken success",
			toolName: "quicbaas_seal_generate_root_token",
			args: map[string]interface{}{
				"strategy": "recovery_keys",
			},
			setupMock: func(m *testutil.MockQuicBaaS) {
				m.On("GenerateRootToken", mock.Anything, mock.MatchedBy(func(req *service.GenerateRootTokenRequest) bool {
					return req.Strategy == "recovery_keys"
				})).Return(&service.GenerateRootTokenResponse{
					RootToken: "root-token-xyz",
					Timestamp: time.Now(),
				}, nil)
			},
			validateFn: func(t *testing.T, result interface{}, err error) {
				assert.NoError(t, err)
				assert.NotNil(t, result)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockSvc := testutil.NewMockQuicBaaS()
			tt.setupMock(mockSvc)

			registry := &ToolRegistry{
				service: mockSvc,
				tools:   nil,
				logger: func() *logger.Logger {
					l, _ := logger.New(logger.LogConfig{Level: "error", Output: "stdout"})
					return l
				}(),
			}
			registry.registerSealTools()

			ctx := context.Background()
			result, err := registry.ExecuteTool(ctx, tt.toolName, tt.args)
			tt.validateFn(t, result, err)

			mockSvc.AssertExpectations(t)
		})
	}
}

// TestSecretsToolsSuccess tests successful execution paths for all Secrets tools.
func TestSecretsToolsSuccess(t *testing.T) {
	tests := []struct {
		name       string
		toolName   string
		args       map[string]interface{}
		setupMock  func(*testutil.MockQuicBaaS)
		validateFn func(*testing.T, interface{}, error)
	}{
		{
			name:     "StoreSecret success",
			toolName: "quicbaas_secrets_store",
			args: map[string]interface{}{
				"name": "db-password",
				"data": "c3VwZXJzZWNyZXQ=", // base64 of "supersecret"
			},
			setupMock: func(m *testutil.MockQuicBaaS) {
				m.On("StoreSecret", mock.Anything, mock.MatchedBy(func(req *service.StoreSecretRequest) bool {
					return req.Name == "db-password"
				})).Return(&service.StoreSecretResponse{
					SecretID:  "secret-123",
					Version:   1,
					Timestamp: time.Now(),
				}, nil)
			},
			validateFn: func(t *testing.T, result interface{}, err error) {
				assert.NoError(t, err)
				assert.NotNil(t, result)
			},
		},
		{
			name:     "GetSecret success",
			toolName: "quicbaas_secrets_get",
			args: map[string]interface{}{
				"secret_id": "secret-123",
			},
			setupMock: func(m *testutil.MockQuicBaaS) {
				m.On("GetSecret", mock.Anything, mock.MatchedBy(func(req *service.GetSecretRequest) bool {
					return req.SecretID == "secret-123"
				})).Return(&service.GetSecretResponse{
					SecretID:  "secret-123",
					Data:      []byte("supersecret"),
					Version:   1,
					Metadata:  map[string]string{},
					Timestamp: time.Now(),
				}, nil)
			},
			validateFn: func(t *testing.T, result interface{}, err error) {
				assert.NoError(t, err)
				assert.NotNil(t, result)
			},
		},
		{
			name:     "ListSecrets success",
			toolName: "quicbaas_secrets_list",
			args:     map[string]interface{}{},
			setupMock: func(m *testutil.MockQuicBaaS) {
				m.On("ListSecrets", mock.Anything, mock.Anything).Return(&service.ListSecretsResponse{
					Secrets: []*service.Secret{
						{SecretID: "secret-1"},
						{SecretID: "secret-2"},
					},
					HasMore: false,
				}, nil)
			},
			validateFn: func(t *testing.T, result interface{}, err error) {
				assert.NoError(t, err)
				assert.NotNil(t, result)
			},
		},
		{
			name:     "DeleteSecret success",
			toolName: "quicbaas_secrets_delete",
			args: map[string]interface{}{
				"secret_id": "secret-123",
			},
			setupMock: func(m *testutil.MockQuicBaaS) {
				m.On("DeleteSecret", mock.Anything, mock.MatchedBy(func(req *service.DeleteSecretRequest) bool {
					return req.SecretID == "secret-123"
				})).Return(&service.DeleteSecretResponse{
					SecretID:  "secret-123",
					Deleted:   true,
					Timestamp: time.Now(),
				}, nil)
			},
			validateFn: func(t *testing.T, result interface{}, err error) {
				assert.NoError(t, err)
				assert.NotNil(t, result)
			},
		},
		{
			name:     "ListSecretVersions success",
			toolName: "quicbaas_secrets_versions",
			args: map[string]interface{}{
				"secret_id": "secret-123",
			},
			setupMock: func(m *testutil.MockQuicBaaS) {
				m.On("ListSecretVersions", mock.Anything, mock.MatchedBy(func(req *service.ListSecretVersionsRequest) bool {
					return req.SecretID == "secret-123"
				})).Return(&service.ListSecretVersionsResponse{
					Versions:  []*service.SecretVersion{{Version: 1}, {Version: 2}},
					HasMore:   false,
					Timestamp: time.Now(),
				}, nil)
			},
			validateFn: func(t *testing.T, result interface{}, err error) {
				assert.NoError(t, err)
				assert.NotNil(t, result)
			},
		},
		{
			name:     "RollbackSecret success",
			toolName: "quicbaas_secrets_rollback",
			args: map[string]interface{}{
				"secret_id":      "secret-123",
				"target_version": float64(1),
			},
			setupMock: func(m *testutil.MockQuicBaaS) {
				m.On("RollbackSecret", mock.Anything, mock.MatchedBy(func(req *service.RollbackSecretRequest) bool {
					return req.SecretID == "secret-123" && req.TargetVersion == 1
				})).Return(&service.RollbackSecretResponse{
					SecretID:   "secret-123",
					NewVersion: 3,
					Timestamp:  time.Now(),
				}, nil)
			},
			validateFn: func(t *testing.T, result interface{}, err error) {
				assert.NoError(t, err)
				assert.NotNil(t, result)
			},
		},
		{
			name:     "RotateSecret success",
			toolName: "quicbaas_secrets_rotate",
			args: map[string]interface{}{
				"secret_id": "secret-123",
				"new_data":  "bmV3c2VjcmV0", // base64 of "newsecret"
			},
			setupMock: func(m *testutil.MockQuicBaaS) {
				m.On("RotateSecret", mock.Anything, mock.MatchedBy(func(req *service.RotateSecretRequest) bool {
					return req.SecretID == "secret-123"
				})).Return(&service.RotateSecretResponse{
					SecretID:   "secret-123",
					OldVersion: 1,
					NewVersion: 2,
					Timestamp:  time.Now(),
				}, nil)
			},
			validateFn: func(t *testing.T, result interface{}, err error) {
				assert.NoError(t, err)
				assert.NotNil(t, result)
			},
		},
		{
			name:     "SetRotationPolicy success",
			toolName: "quicbaas_secrets_set_rotation_policy",
			args: map[string]interface{}{
				"secret_id": "secret-123",
				"policy": map[string]interface{}{
					"enabled": true,
				},
			},
			setupMock: func(m *testutil.MockQuicBaaS) {
				m.On("SetRotationPolicy", mock.Anything, mock.MatchedBy(func(req *service.SetRotationPolicyRequest) bool {
					return req.SecretID == "secret-123"
				})).Return(&service.SetRotationPolicyResponse{
					SecretID:  "secret-123",
					Policy:    &service.SecretRotationPolicy{},
					Timestamp: time.Now(),
				}, nil)
			},
			validateFn: func(t *testing.T, result interface{}, err error) {
				assert.NoError(t, err)
				assert.NotNil(t, result)
			},
		},
		{
			name:     "GetRotationPolicy success",
			toolName: "quicbaas_secrets_get_rotation_policy",
			args: map[string]interface{}{
				"secret_id": "secret-123",
			},
			setupMock: func(m *testutil.MockQuicBaaS) {
				m.On("GetRotationPolicy", mock.Anything, mock.MatchedBy(func(req *service.GetRotationPolicyRequest) bool {
					return req.SecretID == "secret-123"
				})).Return(&service.GetRotationPolicyResponse{
					SecretID:  "secret-123",
					Policy:    &service.SecretRotationPolicy{},
					Timestamp: time.Now(),
				}, nil)
			},
			validateFn: func(t *testing.T, result interface{}, err error) {
				assert.NoError(t, err)
				assert.NotNil(t, result)
			},
		},
		{
			name:     "GetSecretsByPrefix success",
			toolName: "quicbaas_secrets_get_prefix",
			args: map[string]interface{}{
				"prefix":       "apps/myapp/",
				"include_data": true,
				"limit":        50,
			},
			setupMock: func(m *testutil.MockQuicBaaS) {
				m.On("GetSecretsByPrefix", mock.Anything, mock.MatchedBy(func(req *service.GetSecretsByPrefixRequest) bool {
					return req.Prefix == "apps/myapp/" && req.IncludeData && req.Limit == 50
				})).Return(&service.GetSecretsByPrefixResponse{
					Secrets: map[string]map[string][]byte{
						"apps/myapp/db": {"password": []byte("secret123")},
					},
					TotalCount: 1,
					Timestamp:  time.Now(),
				}, nil)
			},
			validateFn: func(t *testing.T, result interface{}, err error) {
				assert.NoError(t, err)
				assert.NotNil(t, result)
			},
		},
		{
			name:     "WatchSecretsByPrefix success",
			toolName: "quicbaas_secrets_watch",
			args: map[string]interface{}{
				"prefix":          "apps/myapp/",
				"timeout_seconds": 1, // Short timeout for test
			},
			setupMock: func(m *testutil.MockQuicBaaS) {
				ch := make(chan *service.SecretChange, 1)
				ch <- &service.SecretChange{
					Path:       "apps/myapp/db",
					ChangeType: service.SecretChangeUpdated,
					Version:    1,
					Timestamp:  time.Now(),
				}
				close(ch)
				m.On("WatchSecretsByPrefix", mock.Anything, mock.MatchedBy(func(req *service.WatchSecretsRequest) bool {
					return req.Prefix == "apps/myapp/"
				})).Return((<-chan *service.SecretChange)(ch), nil)
			},
			validateFn: func(t *testing.T, result interface{}, err error) {
				assert.NoError(t, err)
				assert.NotNil(t, result)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockSvc := testutil.NewMockQuicBaaS()
			tt.setupMock(mockSvc)

			registry := &ToolRegistry{
				service: mockSvc,
				tools:   nil,
				logger: func() *logger.Logger {
					l, _ := logger.New(logger.LogConfig{Level: "error", Output: "stdout"})
					return l
				}(),
			}
			registry.registerSecretsTools()

			ctx := context.Background()
			result, err := registry.ExecuteTool(ctx, tt.toolName, tt.args)
			tt.validateFn(t, result, err)

			mockSvc.AssertExpectations(t)
		})
	}
}

// TestCryptoToolsSuccess tests successful execution paths for all Crypto tools.
func TestCryptoToolsSuccess(t *testing.T) {
	tests := []struct {
		name       string
		toolName   string
		args       map[string]interface{}
		setupMock  func(*testutil.MockQuicBaaS)
		validateFn func(*testing.T, interface{}, error)
	}{
		{
			name:     "Encrypt success",
			toolName: "quicbaas_crypto_encrypt",
			args: map[string]interface{}{
				"key_id":    "key-123",
				"plaintext": "c2Vuc2l0aXZlIGRhdGE=", // base64 of "sensitive data"
			},
			setupMock: func(m *testutil.MockQuicBaaS) {
				m.On("Encrypt", mock.Anything, mock.MatchedBy(func(req *service.EncryptRequest) bool {
					return req.KeyID == "key-123"
				})).Return(&service.EncryptResponse{
					KeyID:      "key-123",
					KeyVersion: 1,
					Ciphertext: []byte("encrypted-data"),
					Timestamp:  time.Now(),
				}, nil)
			},
			validateFn: func(t *testing.T, result interface{}, err error) {
				assert.NoError(t, err)
				assert.NotNil(t, result)
			},
		},
		{
			name:     "Decrypt success",
			toolName: "quicbaas_crypto_decrypt",
			args: map[string]interface{}{
				"key_id":     "key-123",
				"ciphertext": "ZW5jcnlwdGVkLWRhdGE=", // base64 of "encrypted-data"
			},
			setupMock: func(m *testutil.MockQuicBaaS) {
				m.On("Decrypt", mock.Anything, mock.MatchedBy(func(req *service.DecryptRequest) bool {
					return req.KeyID == "key-123"
				})).Return(&service.DecryptResponse{
					KeyID:      "key-123",
					KeyVersion: 1,
					Plaintext:  []byte("sensitive data"),
					Timestamp:  time.Now(),
				}, nil)
			},
			validateFn: func(t *testing.T, result interface{}, err error) {
				assert.NoError(t, err)
				assert.NotNil(t, result)
			},
		},
		{
			name:     "Rewrap success",
			toolName: "quicbaas_crypto_rewrap",
			args: map[string]interface{}{
				"key_id":     "key-123",
				"ciphertext": "b2xkLWVuY3J5cHRlZC1kYXRh", // base64 of "old-encrypted-data"
			},
			setupMock: func(m *testutil.MockQuicBaaS) {
				m.On("Rewrap", mock.Anything, mock.MatchedBy(func(req *service.RewrapRequest) bool {
					return req.KeyID == "key-123"
				})).Return(&service.RewrapResponse{
					KeyID:         "key-123",
					OldKeyVersion: 1,
					NewKeyVersion: 2,
					Ciphertext:    []byte("new-encrypted-data"),
					Timestamp:     time.Now(),
				}, nil)
			},
			validateFn: func(t *testing.T, result interface{}, err error) {
				assert.NoError(t, err)
				assert.NotNil(t, result)
			},
		},
		{
			name:     "Sign success",
			toolName: "quicbaas_crypto_sign",
			args: map[string]interface{}{
				"key_id":  "key-123",
				"message": "bWVzc2FnZSB0byBzaWdu", // base64 of "message to sign"
			},
			setupMock: func(m *testutil.MockQuicBaaS) {
				m.On("Sign", mock.Anything, mock.MatchedBy(func(req *service.SignRequest) bool {
					return req.KeyID == "key-123"
				})).Return(&service.SignResponse{
					KeyID:      "key-123",
					KeyVersion: 1,
					Signature:  []byte("signature-data"),
					Timestamp:  time.Now(),
				}, nil)
			},
			validateFn: func(t *testing.T, result interface{}, err error) {
				assert.NoError(t, err)
				assert.NotNil(t, result)
			},
		},
		{
			name:     "Verify success",
			toolName: "quicbaas_crypto_verify",
			args: map[string]interface{}{
				"key_id":    "key-123",
				"message":   "bWVzc2FnZSB0byB2ZXJpZnk=", // base64 of "message to verify"
				"signature": "c2lnbmF0dXJlLWRhdGE=",     // base64 of "signature-data"
			},
			setupMock: func(m *testutil.MockQuicBaaS) {
				m.On("Verify", mock.Anything, mock.MatchedBy(func(req *service.VerifyRequest) bool {
					return req.KeyID == "key-123"
				})).Return(&service.VerifyResponse{
					KeyID:      "key-123",
					KeyVersion: 1,
					Valid:      true,
					Timestamp:  time.Now(),
				}, nil)
			},
			validateFn: func(t *testing.T, result interface{}, err error) {
				assert.NoError(t, err)
				assert.NotNil(t, result)
			},
		},
		{
			name:     "GenerateHMAC success",
			toolName: "quicbaas_crypto_hmac",
			args: map[string]interface{}{
				"key_id":  "key-123",
				"message": "bWVzc2FnZSBmb3IgaG1hYw==", // base64 of "message for hmac"
			},
			setupMock: func(m *testutil.MockQuicBaaS) {
				m.On("GenerateHMAC", mock.Anything, mock.MatchedBy(func(req *service.HMACRequest) bool {
					return req.KeyID == "key-123"
				})).Return(&service.HMACResponse{
					KeyID:      "key-123",
					KeyVersion: 1,
					HMAC:       []byte("hmac-value"),
					Timestamp:  time.Now(),
				}, nil)
			},
			validateFn: func(t *testing.T, result interface{}, err error) {
				assert.NoError(t, err)
				assert.NotNil(t, result)
			},
		},
		{
			name:     "VerifyHMAC success",
			toolName: "quicbaas_crypto_verify_hmac",
			args: map[string]interface{}{
				"key_id":  "key-123",
				"message": "bWVzc2FnZSB0byB2ZXJpZnk=", // base64 of "message to verify"
				"hmac":    "aG1hYy12YWx1ZQ==",         // base64 of "hmac-value"
			},
			setupMock: func(m *testutil.MockQuicBaaS) {
				m.On("VerifyHMAC", mock.Anything, mock.MatchedBy(func(req *service.VerifyHMACRequest) bool {
					return req.KeyID == "key-123"
				})).Return(&service.VerifyHMACResponse{
					KeyID:      "key-123",
					KeyVersion: 1,
					Valid:      true,
					Timestamp:  time.Now(),
				}, nil)
			},
			validateFn: func(t *testing.T, result interface{}, err error) {
				assert.NoError(t, err)
				assert.NotNil(t, result)
			},
		},
		{
			name:     "Hash success",
			toolName: "quicbaas_crypto_hash",
			args: map[string]interface{}{
				"data":      "ZGF0YSB0byBoYXNo", // base64 of "data to hash"
				"algorithm": "SHA256",
			},
			setupMock: func(m *testutil.MockQuicBaaS) {
				m.On("Hash", mock.Anything, mock.MatchedBy(func(req *service.HashRequest) bool {
					return req.Algorithm == "SHA256"
				})).Return(&service.HashResponse{
					Algorithm: "SHA256",
					Hash:      []byte("hash-value"),
					Timestamp: time.Now(),
				}, nil)
			},
			validateFn: func(t *testing.T, result interface{}, err error) {
				assert.NoError(t, err)
				assert.NotNil(t, result)
			},
		},
		{
			name:     "GenerateRandom success",
			toolName: "quicbaas_crypto_random",
			args: map[string]interface{}{
				"num_bytes": float64(32),
			},
			setupMock: func(m *testutil.MockQuicBaaS) {
				m.On("GenerateRandom", mock.Anything, mock.MatchedBy(func(req *service.GenerateRandomRequest) bool {
					return req.NumBytes == 32
				})).Return(&service.GenerateRandomResponse{
					RandomBytes: make([]byte, 32),
					Timestamp:   time.Now(),
				}, nil)
			},
			validateFn: func(t *testing.T, result interface{}, err error) {
				assert.NoError(t, err)
				assert.NotNil(t, result)
			},
		},
		{
			name:     "GenerateDataKey success",
			toolName: "quicbaas_crypto_datakey",
			args: map[string]interface{}{
				"key_id": "key-123",
			},
			setupMock: func(m *testutil.MockQuicBaaS) {
				m.On("GenerateDataKey", mock.Anything, mock.MatchedBy(func(req *service.GenerateDataKeyRequest) bool {
					return req.KeyID == "key-123"
				})).Return(&service.GenerateDataKeyResponse{
					KeyID:          "key-123",
					Plaintext:      []byte("plaintext-key"),
					CiphertextBlob: []byte("encrypted-key"),
					Timestamp:      time.Now(),
				}, nil)
			},
			validateFn: func(t *testing.T, result interface{}, err error) {
				assert.NoError(t, err)
				assert.NotNil(t, result)
			},
		},
		{
			name:     "GenerateWrappedDataKey success",
			toolName: "quicbaas_crypto_datakey_wrapped",
			args: map[string]interface{}{
				"key_id": "key-123",
			},
			setupMock: func(m *testutil.MockQuicBaaS) {
				m.On("GenerateWrappedDataKey", mock.Anything, mock.MatchedBy(func(req *service.GenerateWrappedDataKeyRequest) bool {
					return req.KeyID == "key-123"
				})).Return(&service.GenerateWrappedDataKeyResponse{
					KeyID:          "key-123",
					CiphertextBlob: []byte("encrypted-key"),
					Timestamp:      time.Now(),
				}, nil)
			},
			validateFn: func(t *testing.T, result interface{}, err error) {
				assert.NoError(t, err)
				assert.NotNil(t, result)
			},
		},
		{
			name:     "EncryptBatch success",
			toolName: "quicbaas_crypto_encrypt_batch",
			args: map[string]interface{}{
				"key_id": "key-123",
				"items": []interface{}{
					map[string]interface{}{"plaintext": "ZGF0YTE="}, // base64 of "data1"
					map[string]interface{}{"plaintext": "ZGF0YTI="}, // base64 of "data2"
				},
			},
			setupMock: func(m *testutil.MockQuicBaaS) {
				m.On("EncryptBatch", mock.Anything, mock.MatchedBy(func(req *service.EncryptBatchRequest) bool {
					return req.KeyID == "key-123" && len(req.Items) == 2
				})).Return(&service.EncryptBatchResponse{
					KeyID:      "key-123",
					KeyVersion: 1,
					Results: []service.BatchEncryptResult{
						{Ciphertext: []byte("enc1")},
						{Ciphertext: []byte("enc2")},
					},
					Timestamp: time.Now(),
				}, nil)
			},
			validateFn: func(t *testing.T, result interface{}, err error) {
				assert.NoError(t, err)
				assert.NotNil(t, result)
			},
		},
		{
			name:     "DecryptBatch success",
			toolName: "quicbaas_crypto_decrypt_batch",
			args: map[string]interface{}{
				"key_id": "key-123",
				"items": []interface{}{
					map[string]interface{}{"ciphertext": "enc1"},
					map[string]interface{}{"ciphertext": "enc2"},
				},
			},
			setupMock: func(m *testutil.MockQuicBaaS) {
				m.On("DecryptBatch", mock.Anything, mock.MatchedBy(func(req *service.DecryptBatchRequest) bool {
					return req.KeyID == "key-123" && len(req.Items) == 2
				})).Return(&service.DecryptBatchResponse{
					KeyID: "key-123",
					Results: []service.BatchDecryptResult{
						{Plaintext: []byte("data1")},
						{Plaintext: []byte("data2")},
					},
					Timestamp: time.Now(),
				}, nil)
			},
			validateFn: func(t *testing.T, result interface{}, err error) {
				assert.NoError(t, err)
				assert.NotNil(t, result)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockSvc := testutil.NewMockQuicBaaS()
			tt.setupMock(mockSvc)

			registry := &ToolRegistry{
				service: mockSvc,
				tools:   nil,
				logger: func() *logger.Logger {
					l, _ := logger.New(logger.LogConfig{Level: "error", Output: "stdout"})
					return l
				}(),
			}
			registry.registerCryptoTools()

			ctx := context.Background()
			result, err := registry.ExecuteTool(ctx, tt.toolName, tt.args)
			tt.validateFn(t, result, err)

			mockSvc.AssertExpectations(t)
		})
	}
}

// createTestKeyMetadataForSuccessTests creates a KeyMetadata for these tests.
func createTestKeyMetadataForSuccessTests(keyID, name, algorithm string) *kms.KeyMetadata {
	now := time.Now()
	return &kms.KeyMetadata{
		KeyID:              keyID,
		Name:               name,
		Description:        "Test key",
		Algorithm:          algorithm,
		State:              kms.KeyStateEnabled,
		Version:            1,
		Exportable:         false,
		DeletionProtection: true,
		Created:            now,
		Updated:            now,
		Usage:              []kms.KeyUsage{kms.KeyUsageEncrypt, kms.KeyUsageDecrypt},
		Tags:               map[string]string{"env": "test"},
		Owner:              "test-user",
	}
}
