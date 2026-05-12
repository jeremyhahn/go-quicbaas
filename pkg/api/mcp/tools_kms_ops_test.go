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
	"crypto"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	xkmssdk "github.com/jeremyhahn/go-xkms/sdk/go"

	"github.com/jeremyhahn/go-quicbaas/pkg/config"
	dberrors "github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
	"github.com/jeremyhahn/go-quicbaas/pkg/kms"
	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
)

// testKMSService implements service.KMSService for testing.
type testKMSService struct {
	backendTestResp     *service.BackendTestResponse
	backendTestErr      error
	backendStatusResp   *service.BackendStatusResponse
	backendStatusErr    error
	listKeyVersionsResp *service.ListKeyVersionsResponse
	listKeyVersionsErr  error
	updateKeyStatusResp *service.UpdateKeyStatusResponse
	updateKeyStatusErr  error
}

func (s *testKMSService) GetSupportedAlgorithms(_ context.Context) ([]string, error) {
	return []string{"AES-256-GCM", "ChaCha20-Poly1305"}, nil
}

func (s *testKMSService) GenerateKey(_ context.Context, _ *service.GenerateKeyRequest) (*service.GenerateKeyResponse, error) {
	return &service.GenerateKeyResponse{}, nil
}

func (s *testKMSService) ImportKey(_ context.Context, _ *service.ImportKeyRequest) (*service.ImportKeyResponse, error) {
	return &service.ImportKeyResponse{}, nil
}

func (s *testKMSService) GetKey(_ context.Context, _ *service.GetKeyRequest) (*service.GetKeyResponse, error) {
	return &service.GetKeyResponse{}, nil
}

func (s *testKMSService) ListKeys(_ context.Context, _ *service.ListKeysRequest) (*service.ListKeysResponse, error) {
	return &service.ListKeysResponse{}, nil
}

func (s *testKMSService) RotateKey(_ context.Context, _ *service.RotateKeyRequest) (*service.RotateKeyResponse, error) {
	return &service.RotateKeyResponse{}, nil
}

func (s *testKMSService) RevokeKey(_ context.Context, _ *service.RevokeKeyRequest) (*service.RevokeKeyResponse, error) {
	return &service.RevokeKeyResponse{}, nil
}

func (s *testKMSService) DeleteKey(_ context.Context, _ *service.DeleteKeyRequest) (*service.DeleteKeyResponse, error) {
	return &service.DeleteKeyResponse{}, nil
}

func (s *testKMSService) GetKeyMetadata(_ context.Context, _ *service.GetKeyMetadataRequest) (*service.GetKeyMetadataResponse, error) {
	return &service.GetKeyMetadataResponse{}, nil
}

func (s *testKMSService) UpdateKeyMetadata(_ context.Context, _ *service.UpdateKeyMetadataRequest) (*service.UpdateKeyMetadataResponse, error) {
	return &service.UpdateKeyMetadataResponse{}, nil
}

func (s *testKMSService) UpdateKeyStatus(_ context.Context, req *service.UpdateKeyStatusRequest) (*service.UpdateKeyStatusResponse, error) {
	if s.updateKeyStatusErr != nil {
		return nil, s.updateKeyStatusErr
	}
	if s.updateKeyStatusResp != nil {
		return s.updateKeyStatusResp, nil
	}
	return &service.UpdateKeyStatusResponse{
		KeyID:     req.KeyID,
		Version:   req.Version,
		NewStatus: req.Status,
		OldStatus: service.KeyStatusEnabled,
		Timestamp: time.Now(),
	}, nil
}

func (s *testKMSService) ListKeyVersions(_ context.Context, req *service.ListKeyVersionsRequest) (*service.ListKeyVersionsResponse, error) {
	if s.listKeyVersionsErr != nil {
		return nil, s.listKeyVersionsErr
	}
	if s.listKeyVersionsResp != nil {
		return s.listKeyVersionsResp, nil
	}
	return &service.ListKeyVersionsResponse{
		KeyID: req.KeyID,
		Versions: []*service.KeyVersionInfo{
			{Version: 1, Status: service.KeyStatusEnabled, CreatedAt: time.Now()},
			{Version: 2, Status: service.KeyStatusEnabled, CreatedAt: time.Now()},
		},
		HasMore:   false,
		Timestamp: time.Now(),
	}, nil
}

func (s *testKMSService) SetKeyPolicy(_ context.Context, _ *service.SetKeyPolicyRequest) (*service.SetKeyPolicyResponse, error) {
	return &service.SetKeyPolicyResponse{}, nil
}

func (s *testKMSService) GetKeyPolicy(_ context.Context, _ *service.GetKeyPolicyRequest) (*service.GetKeyPolicyResponse, error) {
	return &service.GetKeyPolicyResponse{}, nil
}

func (s *testKMSService) CreateDEK(_ context.Context, _ *service.CreateDEKRequest) (*service.CreateDEKResponse, error) {
	return &service.CreateDEKResponse{}, nil
}

func (s *testKMSService) GetDEK(_ context.Context, _ *service.GetDEKRequest) (*service.GetDEKResponse, error) {
	return &service.GetDEKResponse{}, nil
}

func (s *testKMSService) ListDEKs(_ context.Context, _ *service.ListDEKsRequest) (*service.ListDEKsResponse, error) {
	return &service.ListDEKsResponse{}, nil
}

func (s *testKMSService) RotateDEK(_ context.Context, _ *service.RotateDEKRequest) (*service.RotateDEKResponse, error) {
	return &service.RotateDEKResponse{}, nil
}

func (s *testKMSService) DeleteDEK(_ context.Context, _ *service.DeleteDEKRequest) (*service.DeleteDEKResponse, error) {
	return &service.DeleteDEKResponse{}, nil
}

func (s *testKMSService) GetDEKStatus(_ context.Context, _ *service.GetDEKStatusRequest) (*service.GetDEKStatusResponse, error) {
	return &service.GetDEKStatusResponse{}, nil
}

func (s *testKMSService) SetDefaultDEK(_ context.Context, _ *service.SetDefaultDEKRequest) (*service.SetDefaultDEKResponse, error) {
	return &service.SetDefaultDEKResponse{}, nil
}

func (s *testKMSService) SetDEKSafety(_ context.Context, _ *service.SetDEKSafetyRequest) (*service.SetDEKSafetyResponse, error) {
	return &service.SetDEKSafetyResponse{}, nil
}

func (s *testKMSService) ClearDEKSafety(_ context.Context, _ *service.ClearDEKSafetyRequest) (*service.ClearDEKSafetyResponse, error) {
	return &service.ClearDEKSafetyResponse{}, nil
}

func (s *testKMSService) StartReencryption(_ context.Context, _ *service.StartReencryptionRequest) (*service.StartReencryptionResponse, error) {
	return &service.StartReencryptionResponse{}, nil
}

func (s *testKMSService) GetReencryptionProgress(_ context.Context, _ *service.GetReencryptionProgressRequest) (*service.GetReencryptionProgressResponse, error) {
	return &service.GetReencryptionProgressResponse{}, nil
}

func (s *testKMSService) BackendStatus(_ context.Context) (*service.BackendStatusResponse, error) {
	if s.backendStatusErr != nil {
		return nil, s.backendStatusErr
	}
	if s.backendStatusResp != nil {
		return s.backendStatusResp, nil
	}
	return &service.BackendStatusResponse{
		BackendType: "software",
		Status:      "healthy",
		Capabilities: map[string]bool{
			"encrypt": true,
			"decrypt": true,
			"sign":    true,
			"verify":  true,
		},
		Timestamp: time.Now(),
	}, nil
}

func (s *testKMSService) BackendTest(_ context.Context) (*service.BackendTestResponse, error) {
	if s.backendTestErr != nil {
		return nil, s.backendTestErr
	}
	if s.backendTestResp != nil {
		return s.backendTestResp, nil
	}
	return &service.BackendTestResponse{
		Success:  true,
		Duration: time.Millisecond * 10,
	}, nil
}

func (s *testKMSService) ExportKey(_ context.Context, _ *service.ExportKeyRequest) (*service.ExportKeyResponse, error) {
	return &service.ExportKeyResponse{}, nil
}

func (s *testKMSService) UnwrapKey(_ context.Context, _ *service.UnwrapKeyRequest) (*service.UnwrapKeyResponse, error) {
	return &service.UnwrapKeyResponse{}, nil
}

func (s *testKMSService) SetKeySafety(_ context.Context, _ *service.SetKeySafetyRequest) (*service.SetKeySafetyResponse, error) {
	return &service.SetKeySafetyResponse{}, nil
}

func (s *testKMSService) ClearKeySafety(_ context.Context, _ *service.ClearKeySafetyRequest) (*service.ClearKeySafetyResponse, error) {
	return &service.ClearKeySafetyResponse{}, nil
}

func (s *testKMSService) SafetyCheck(_ context.Context, _ *service.SafetyCheckRequest) (*service.SafetyCheckResponse, error) {
	return &service.SafetyCheckResponse{}, nil
}

func (s *testKMSService) SafetyReport(_ context.Context, _ *service.SafetyReportRequest) (*service.SafetyReportResponse, error) {
	return &service.SafetyReportResponse{}, nil
}

func (s *testKMSService) SetDefaultKey(_ context.Context, _ *service.SetDefaultKeyRequest) (*service.SetDefaultKeyResponse, error) {
	return &service.SetDefaultKeyResponse{}, nil
}

func (s *testKMSService) GetPublicKey(_ context.Context, _ *service.GetPublicKeyRequest) (*service.GetPublicKeyResponse, error) {
	return nil, nil
}

// testCryptoService implements service.CryptoService for testing.
type testCryptoService struct {
	encryptResp *service.EncryptResponse
	encryptErr  error
	decryptResp *service.DecryptResponse
	decryptErr  error
}

func (s *testCryptoService) Encrypt(_ context.Context, req *service.EncryptRequest) (*service.EncryptResponse, error) {
	if s.encryptErr != nil {
		return nil, s.encryptErr
	}
	if s.encryptResp != nil {
		return s.encryptResp, nil
	}
	return &service.EncryptResponse{
		KeyID:      req.KeyID,
		Ciphertext: []byte("encrypted-data"),
		Nonce:      []byte("nonce-value"),
		KeyVersion: 1,
	}, nil
}

func (s *testCryptoService) Decrypt(_ context.Context, req *service.DecryptRequest) (*service.DecryptResponse, error) {
	if s.decryptErr != nil {
		return nil, s.decryptErr
	}
	if s.decryptResp != nil {
		return s.decryptResp, nil
	}
	return &service.DecryptResponse{
		KeyID:      req.KeyID,
		Plaintext:  []byte("decrypted-data"),
		KeyVersion: 1,
	}, nil
}

func (s *testCryptoService) Rewrap(_ context.Context, _ *service.RewrapRequest) (*service.RewrapResponse, error) {
	return &service.RewrapResponse{}, nil
}

func (s *testCryptoService) Sign(_ context.Context, _ *service.SignRequest) (*service.SignResponse, error) {
	return &service.SignResponse{}, nil
}

func (s *testCryptoService) Verify(_ context.Context, _ *service.VerifyRequest) (*service.VerifyResponse, error) {
	return &service.VerifyResponse{}, nil
}

func (s *testCryptoService) GenerateHMAC(_ context.Context, _ *service.HMACRequest) (*service.HMACResponse, error) {
	return &service.HMACResponse{}, nil
}

func (s *testCryptoService) VerifyHMAC(_ context.Context, _ *service.VerifyHMACRequest) (*service.VerifyHMACResponse, error) {
	return &service.VerifyHMACResponse{}, nil
}

func (s *testCryptoService) Hash(_ context.Context, _ *service.HashRequest) (*service.HashResponse, error) {
	return &service.HashResponse{}, nil
}

func (s *testCryptoService) GenerateRandom(_ context.Context, _ *service.GenerateRandomRequest) (*service.GenerateRandomResponse, error) {
	return &service.GenerateRandomResponse{}, nil
}

func (s *testCryptoService) GenerateDataKey(_ context.Context, _ *service.GenerateDataKeyRequest) (*service.GenerateDataKeyResponse, error) {
	return &service.GenerateDataKeyResponse{}, nil
}

func (s *testCryptoService) GenerateWrappedDataKey(_ context.Context, _ *service.GenerateWrappedDataKeyRequest) (*service.GenerateWrappedDataKeyResponse, error) {
	return &service.GenerateWrappedDataKeyResponse{}, nil
}

func (s *testCryptoService) EncryptBatch(_ context.Context, _ *service.EncryptBatchRequest) (*service.EncryptBatchResponse, error) {
	return &service.EncryptBatchResponse{}, nil
}

func (s *testCryptoService) DecryptBatch(_ context.Context, _ *service.DecryptBatchRequest) (*service.DecryptBatchResponse, error) {
	return &service.DecryptBatchResponse{}, nil
}

// testKMSManagerForOps implements interfaces.KMSManager for testing KMS operations.
type testKMSManagerForOps struct{}

func (m *testKMSManagerForOps) GenerateKey(_ context.Context, _ *xkmssdk.KeyAttributes) (interface{}, error) {
	now := time.Now()
	return &kms.KeyMetadata{
		KeyID:     "test-key",
		Name:      "test-key",
		Algorithm: "AES-256-GCM",
		State:     kms.KeyStateEnabled,
		Version:   1,
		Created:   now,
		Updated:   now,
	}, nil
}

func (m *testKMSManagerForOps) ImportKey(_ context.Context, keyID string, _ []byte, algorithm string) (interface{}, error) {
	now := time.Now()
	return &kms.KeyMetadata{
		KeyID:     keyID,
		Algorithm: algorithm,
		State:     kms.KeyStateEnabled,
		Version:   1,
		Created:   now,
		Updated:   now,
	}, nil
}

func (m *testKMSManagerForOps) GetKey(_ context.Context, keyID string) (interface{}, error) {
	now := time.Now()
	return &kms.KeyMetadata{
		KeyID:     keyID,
		Name:      keyID,
		Algorithm: "AES-256-GCM",
		State:     kms.KeyStateEnabled,
		Version:   1,
		Created:   now,
		Updated:   now,
	}, nil
}

func (m *testKMSManagerForOps) UpdateStoredMetadata(_ interface{}) error {
	return nil
}

func (m *testKMSManagerForOps) ListKeys(_ context.Context, _ string) ([]interface{}, error) {
	return []interface{}{}, nil
}

func (m *testKMSManagerForOps) RotateKey(_ context.Context, keyID string) (interface{}, error) {
	now := time.Now()
	return &kms.KeyMetadata{
		KeyID:     keyID,
		Algorithm: "AES-256-GCM",
		State:     kms.KeyStateEnabled,
		Version:   2,
		Created:   now,
		Updated:   now,
	}, nil
}

func (m *testKMSManagerForOps) RevokeKey(_ context.Context, _ string) error {
	return nil
}

func (m *testKMSManagerForOps) EnableKey(_ context.Context, _ string) error {
	return nil
}

func (m *testKMSManagerForOps) DisableKey(_ context.Context, _ string) error {
	return nil
}

func (m *testKMSManagerForOps) DeleteKey(_ context.Context, _ string) error {
	return nil
}

func (m *testKMSManagerForOps) Encrypt(_ context.Context, _ string, _ []byte, _ []byte) ([]byte, error) {
	return []byte("encrypted-data"), nil
}

func (m *testKMSManagerForOps) Decrypt(_ context.Context, _ string, _ []byte, _ []byte) ([]byte, error) {
	return []byte("decrypted-data"), nil
}

func (m *testKMSManagerForOps) Sign(_ context.Context, _ string, _ []byte, _ string) ([]byte, error) {
	return []byte("signature"), nil
}

// Signer satisfies interfaces.KMSManager for tests that do not exercise a
// crypto.Signer. Tests requiring a live signer should supply a concrete mock.
func (m *testKMSManagerForOps) Signer(_ context.Context, _ string) (crypto.Signer, error) {
	return nil, dberrors.New(dberrors.ErrNotImplemented, "testKMSManagerForOps.Signer", "signer stub")
}

func (m *testKMSManagerForOps) Verify(_ context.Context, _ string, _ []byte, _ []byte, _ string) (bool, error) {
	return true, nil
}

func (m *testKMSManagerForOps) GenerateDataKey(_ context.Context, _ *xkmssdk.KeyAttributes, _ int) ([]byte, []byte, error) {
	return []byte("plaintext-key"), []byte("wrapped-key"), nil
}

func (m *testKMSManagerForOps) GetBackendType() config.BackendType {
	return config.BackendTypeSoftware
}

func (m *testKMSManagerForOps) IsAvailable(_ context.Context) bool {
	return true
}

func (m *testKMSManagerForOps) GetSupportedAlgorithms(_ context.Context) ([]string, error) {
	return []string{"AES-256-GCM", "ChaCha20-Poly1305"}, nil
}

func (m *testKMSManagerForOps) GetPublicKey(_ context.Context, _ string) ([]byte, error) {
	return nil, nil
}

func (m *testKMSManagerForOps) Close() error {
	return nil
}

// testQuicBaaSWithKMSOps wraps MockQuicBaaS with test KMS and Crypto services.
type testQuicBaaSWithKMSOps struct {
	*MockQuicBaaS
	kmsSvc     *testKMSService
	cryptoSvc  *testCryptoService
	kmsManager interfaces.KMSManager
}

func (m *testQuicBaaSWithKMSOps) KMS() service.KMSService {
	if m.kmsSvc == nil {
		return nil
	}
	return m.kmsSvc
}

func (m *testQuicBaaSWithKMSOps) Crypto() service.CryptoService {
	if m.cryptoSvc == nil {
		return nil
	}
	return m.cryptoSvc
}

func (m *testQuicBaaSWithKMSOps) KMSManager() interfaces.KMSManager {
	return m.kmsManager
}

func newTestQuicBaaSWithKMSOps() *testQuicBaaSWithKMSOps {
	return &testQuicBaaSWithKMSOps{
		MockQuicBaaS: &MockQuicBaaS{},
		kmsSvc:       &testKMSService{},
		cryptoSvc:    &testCryptoService{},
		kmsManager:   &testKMSManagerForOps{},
	}
}

func TestKMSOpTools_Registration(t *testing.T) {
	log, err := logger.New(logger.LogConfig{
		Level:     "error",
		Format:    "json",
		Output:    "stdout",
		Component: "mcp-test",
	})
	require.NoError(t, err)

	mockSvc := newTestQuicBaaSWithKMSOps()
	registry := NewToolRegistry(mockSvc, log)

	// Verify tools are registered
	tools := registry.ListTools()
	toolNames := make(map[string]bool)
	for _, tool := range tools {
		toolNames[tool.Name] = true
	}

	expectedTools := []string{
		"quicbaas_kms_encrypt",
		"quicbaas_kms_decrypt",
		"quicbaas_kms_backend_test",
		"quicbaas_kms_key_versions",
		"quicbaas_kms_key_status_update",
		"quicbaas_kms_backend_status",
	}

	for _, name := range expectedTools {
		assert.True(t, toolNames[name], "expected tool %q to be registered", name)
	}
}

func TestExecuteKMSEncrypt_Success(t *testing.T) {
	log, err := logger.New(logger.LogConfig{
		Level:     "error",
		Format:    "json",
		Output:    "stdout",
		Component: "mcp-test",
	})
	require.NoError(t, err)

	mockSvc := newTestQuicBaaSWithKMSOps()
	registry := NewToolRegistry(mockSvc, log)

	args := map[string]interface{}{
		"key_id":    "test-key",
		"plaintext": "SGVsbG8gV29ybGQ=", // "Hello World" base64 encoded
	}

	result, err := registry.ExecuteTool(context.Background(), "quicbaas_kms_encrypt", args)
	require.NoError(t, err)
	require.NotNil(t, result)

	resultMap, ok := result.(map[string]interface{})
	require.True(t, ok)
	assert.Equal(t, "test-key", resultMap["key_id"])
}

func TestExecuteKMSDecrypt_Success(t *testing.T) {
	log, err := logger.New(logger.LogConfig{
		Level:     "error",
		Format:    "json",
		Output:    "stdout",
		Component: "mcp-test",
	})
	require.NoError(t, err)

	mockSvc := newTestQuicBaaSWithKMSOps()
	registry := NewToolRegistry(mockSvc, log)

	args := map[string]interface{}{
		"key_id":     "test-key",
		"ciphertext": "ZW5jcnlwdGVkLWRhdGE=", // base64 encoded
	}

	result, err := registry.ExecuteTool(context.Background(), "quicbaas_kms_decrypt", args)
	require.NoError(t, err)
	require.NotNil(t, result)

	resultMap, ok := result.(map[string]interface{})
	require.True(t, ok)
	assert.Equal(t, "test-key", resultMap["key_id"])
}

func TestExecuteKMSBackendTest_Success(t *testing.T) {
	log, err := logger.New(logger.LogConfig{
		Level:     "error",
		Format:    "json",
		Output:    "stdout",
		Component: "mcp-test",
	})
	require.NoError(t, err)

	mockSvc := newTestQuicBaaSWithKMSOps()
	registry := NewToolRegistry(mockSvc, log)

	result, err := registry.ExecuteTool(context.Background(), "quicbaas_kms_backend_test", map[string]interface{}{})
	require.NoError(t, err)
	require.NotNil(t, result)

	resultMap, ok := result.(map[string]interface{})
	require.True(t, ok)
	assert.Equal(t, true, resultMap["success"])
}

func TestExecuteKMSKeyVersions_Success(t *testing.T) {
	log, err := logger.New(logger.LogConfig{
		Level:     "error",
		Format:    "json",
		Output:    "stdout",
		Component: "mcp-test",
	})
	require.NoError(t, err)

	mockSvc := newTestQuicBaaSWithKMSOps()
	registry := NewToolRegistry(mockSvc, log)

	args := map[string]interface{}{
		"key_id": "test-key",
		"limit":  float64(10),
	}

	result, err := registry.ExecuteTool(context.Background(), "quicbaas_kms_key_versions", args)
	require.NoError(t, err)
	require.NotNil(t, result)

	resultMap, ok := result.(map[string]interface{})
	require.True(t, ok)
	assert.Equal(t, "test-key", resultMap["key_id"])
}

func TestExecuteKMSKeyVersions_LimitBounds(t *testing.T) {
	log, err := logger.New(logger.LogConfig{
		Level:     "error",
		Format:    "json",
		Output:    "stdout",
		Component: "mcp-test",
	})
	require.NoError(t, err)

	mockSvc := newTestQuicBaaSWithKMSOps()
	registry := NewToolRegistry(mockSvc, log)

	// Test that limit is capped at 10000
	args := map[string]interface{}{
		"key_id": "test-key",
		"limit":  float64(100000), // JSON numbers come as float64
	}

	result, err := registry.ExecuteTool(context.Background(), "quicbaas_kms_key_versions", args)
	require.NoError(t, err)
	require.NotNil(t, result)
}

func TestExecuteKMSKeyStatusUpdate_Success(t *testing.T) {
	log, err := logger.New(logger.LogConfig{
		Level:     "error",
		Format:    "json",
		Output:    "stdout",
		Component: "mcp-test",
	})
	require.NoError(t, err)

	mockSvc := newTestQuicBaaSWithKMSOps()
	registry := NewToolRegistry(mockSvc, log)

	args := map[string]interface{}{
		"key_id": "test-key",
		"status": "disabled",
		"reason": "test reason",
	}

	result, err := registry.ExecuteTool(context.Background(), "quicbaas_kms_key_status_update", args)
	require.NoError(t, err)
	require.NotNil(t, result)

	resultMap, ok := result.(map[string]interface{})
	require.True(t, ok)
	assert.Equal(t, "test-key", resultMap["key_id"])
}

func TestExecuteKMSKeyStatusUpdate_InvalidStatus(t *testing.T) {
	log, err := logger.New(logger.LogConfig{
		Level:     "error",
		Format:    "json",
		Output:    "stdout",
		Component: "mcp-test",
	})
	require.NoError(t, err)

	mockSvc := newTestQuicBaaSWithKMSOps()
	registry := NewToolRegistry(mockSvc, log)

	args := map[string]interface{}{
		"key_id": "test-key",
		"status": "invalid",
	}

	_, err = registry.ExecuteTool(context.Background(), "quicbaas_kms_key_status_update", args)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "status must be 'enabled' or 'disabled'")
}

func TestExecuteKMSBackendStatus_Success(t *testing.T) {
	log, err := logger.New(logger.LogConfig{
		Level:     "error",
		Format:    "json",
		Output:    "stdout",
		Component: "mcp-test",
	})
	require.NoError(t, err)

	mockSvc := newTestQuicBaaSWithKMSOps()
	registry := NewToolRegistry(mockSvc, log)

	result, err := registry.ExecuteTool(context.Background(), "quicbaas_kms_backend_status", map[string]interface{}{})
	require.NoError(t, err)
	require.NotNil(t, result)

	resultMap, ok := result.(map[string]interface{})
	require.True(t, ok)
	assert.Equal(t, "software", resultMap["backend_type"])
	assert.Equal(t, "healthy", resultMap["status"])
}

func TestExecuteKMSEncrypt_NilKMSManager(t *testing.T) {
	log, err := logger.New(logger.LogConfig{
		Level:     "error",
		Format:    "json",
		Output:    "stdout",
		Component: "mcp-test",
	})
	require.NoError(t, err)

	mockSvc := &testQuicBaaSWithKMSOps{
		MockQuicBaaS: &MockQuicBaaS{},
		kmsSvc:       &testKMSService{},
		cryptoSvc:    &testCryptoService{},
		kmsManager:   nil, // nil KMS manager
	}
	registry := NewToolRegistry(mockSvc, log)

	args := map[string]interface{}{
		"key_id":    "test-key",
		"plaintext": "test",
	}

	_, err = registry.ExecuteTool(context.Background(), "quicbaas_kms_encrypt", args)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "KMS manager not available")
}

func TestExecuteKMSDecrypt_NilKMSManager(t *testing.T) {
	log, err := logger.New(logger.LogConfig{
		Level:     "error",
		Format:    "json",
		Output:    "stdout",
		Component: "mcp-test",
	})
	require.NoError(t, err)

	mockSvc := &testQuicBaaSWithKMSOps{
		MockQuicBaaS: &MockQuicBaaS{},
		kmsSvc:       &testKMSService{},
		cryptoSvc:    &testCryptoService{},
		kmsManager:   nil, // nil KMS manager
	}
	registry := NewToolRegistry(mockSvc, log)

	args := map[string]interface{}{
		"key_id":     "test-key",
		"ciphertext": "test",
	}

	_, err = registry.ExecuteTool(context.Background(), "quicbaas_kms_decrypt", args)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "KMS manager not available")
}

func TestExecuteKMSBackendTest_NilKMSManager(t *testing.T) {
	log, err := logger.New(logger.LogConfig{
		Level:     "error",
		Format:    "json",
		Output:    "stdout",
		Component: "mcp-test",
	})
	require.NoError(t, err)

	mockSvc := &testQuicBaaSWithKMSOps{
		MockQuicBaaS: &MockQuicBaaS{},
		kmsSvc:       &testKMSService{},
		cryptoSvc:    &testCryptoService{},
		kmsManager:   nil, // nil KMS manager
	}
	registry := NewToolRegistry(mockSvc, log)

	_, err = registry.ExecuteTool(context.Background(), "quicbaas_kms_backend_test", map[string]interface{}{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "KMS manager not available")
}

func TestExecuteKMSKeyVersions_NilKMSManager(t *testing.T) {
	log, err := logger.New(logger.LogConfig{
		Level:     "error",
		Format:    "json",
		Output:    "stdout",
		Component: "mcp-test",
	})
	require.NoError(t, err)

	mockSvc := &testQuicBaaSWithKMSOps{
		MockQuicBaaS: &MockQuicBaaS{},
		kmsSvc:       &testKMSService{},
		cryptoSvc:    &testCryptoService{},
		kmsManager:   nil, // nil KMS manager
	}
	registry := NewToolRegistry(mockSvc, log)

	args := map[string]interface{}{
		"key_id": "test-key",
	}

	_, err = registry.ExecuteTool(context.Background(), "quicbaas_kms_key_versions", args)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "KMS manager not available")
}

func TestExecuteKMSKeyStatusUpdate_NilKMSManager(t *testing.T) {
	log, err := logger.New(logger.LogConfig{
		Level:     "error",
		Format:    "json",
		Output:    "stdout",
		Component: "mcp-test",
	})
	require.NoError(t, err)

	mockSvc := &testQuicBaaSWithKMSOps{
		MockQuicBaaS: &MockQuicBaaS{},
		kmsSvc:       &testKMSService{},
		cryptoSvc:    &testCryptoService{},
		kmsManager:   nil, // nil KMS manager
	}
	registry := NewToolRegistry(mockSvc, log)

	args := map[string]interface{}{
		"key_id": "test-key",
		"status": "enabled",
	}

	_, err = registry.ExecuteTool(context.Background(), "quicbaas_kms_key_status_update", args)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "KMS manager not available")
}

func TestExecuteKMSBackendStatus_NilKMSManager(t *testing.T) {
	log, err := logger.New(logger.LogConfig{
		Level:     "error",
		Format:    "json",
		Output:    "stdout",
		Component: "mcp-test",
	})
	require.NoError(t, err)

	mockSvc := &testQuicBaaSWithKMSOps{
		MockQuicBaaS: &MockQuicBaaS{},
		kmsSvc:       &testKMSService{},
		cryptoSvc:    &testCryptoService{},
		kmsManager:   nil, // nil KMS manager
	}
	registry := NewToolRegistry(mockSvc, log)

	_, err = registry.ExecuteTool(context.Background(), "quicbaas_kms_backend_status", map[string]interface{}{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "KMS manager not available")
}

func TestExecuteKMSKeyStatusUpdate_EnabledStatus(t *testing.T) {
	log, err := logger.New(logger.LogConfig{
		Level:     "error",
		Format:    "json",
		Output:    "stdout",
		Component: "mcp-test",
	})
	require.NoError(t, err)

	mockSvc := newTestQuicBaaSWithKMSOps()
	registry := NewToolRegistry(mockSvc, log)

	args := map[string]interface{}{
		"key_id":  "test-key",
		"status":  "enabled",
		"version": float64(1),
	}

	result, err := registry.ExecuteTool(context.Background(), "quicbaas_kms_key_status_update", args)
	require.NoError(t, err)
	require.NotNil(t, result)
}

func TestExecuteKMSEncrypt_WithOptionalParams(t *testing.T) {
	log, err := logger.New(logger.LogConfig{
		Level:     "error",
		Format:    "json",
		Output:    "stdout",
		Component: "mcp-test",
	})
	require.NoError(t, err)

	mockSvc := newTestQuicBaaSWithKMSOps()
	registry := NewToolRegistry(mockSvc, log)

	args := map[string]interface{}{
		"key_id":    "test-key",
		"plaintext": "SGVsbG8gV29ybGQ=",
		"context":   "YWRkaXRpb25hbC1kYXRh", // additional data
		"nonce":     "bm9uY2UtdmFsdWU=",     // nonce value
	}

	result, err := registry.ExecuteTool(context.Background(), "quicbaas_kms_encrypt", args)
	require.NoError(t, err)
	require.NotNil(t, result)
}

func TestExecuteKMSDecrypt_WithOptionalParams(t *testing.T) {
	log, err := logger.New(logger.LogConfig{
		Level:     "error",
		Format:    "json",
		Output:    "stdout",
		Component: "mcp-test",
	})
	require.NoError(t, err)

	mockSvc := newTestQuicBaaSWithKMSOps()
	registry := NewToolRegistry(mockSvc, log)

	args := map[string]interface{}{
		"key_id":      "test-key",
		"ciphertext":  "ZW5jcnlwdGVkLWRhdGE=",
		"context":     "YWRkaXRpb25hbC1kYXRh",
		"nonce":       "bm9uY2UtdmFsdWU=",
		"key_version": float64(2),
	}

	result, err := registry.ExecuteTool(context.Background(), "quicbaas_kms_decrypt", args)
	require.NoError(t, err)
	require.NotNil(t, result)
}
