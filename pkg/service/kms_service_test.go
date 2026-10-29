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

// Package service provides unit tests for the KMS service implementation.
package service

import (
	"context"
	"crypto"
	stderrors "errors"
	"fmt"
	"testing"
	"time"

	xkmssdk "github.com/jeremyhahn/go-xkms/sdk/go"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/pkg/kms"

	"encoding/json"

	"github.com/jeremyhahn/go-quicbaas/pkg/config"
	quicbaasErrors "github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
	"github.com/jeremyhahn/go-quicbaas/pkg/scheduler"
)

// =============================================================================
// Mock KMS Manager for Unit Testing
// =============================================================================

// mockKMSManager implements interfaces.KMSManager for testing.
type mockKMSManager struct {
	keys              map[string]*mockKeyData
	generateKeyFn     func(ctx context.Context, attrs *xkmssdk.KeyAttributes) (interface{}, error)
	importKeyFn       func(ctx context.Context, keyID string, material []byte, algorithm string) (interface{}, error)
	getKeyFn          func(ctx context.Context, keyID string) (interface{}, error)
	listKeysFn        func(ctx context.Context, prefix string) ([]interface{}, error)
	rotateKeyFn       func(ctx context.Context, keyID string) (interface{}, error)
	revokeKeyFn       func(ctx context.Context, keyID string) error
	deleteKeyFn       func(ctx context.Context, keyID string) error
	encryptFn         func(ctx context.Context, keyID string, plaintext, aad []byte) ([]byte, error)
	decryptFn         func(ctx context.Context, keyID string, ciphertext, aad []byte) ([]byte, error)
	signFn            func(ctx context.Context, keyID string, digest []byte, algorithm string) ([]byte, error)
	verifyFn          func(ctx context.Context, keyID string, digest, signature []byte, algorithm string) (bool, error)
	generateDataKeyFn func(ctx context.Context, attrs *xkmssdk.KeyAttributes, keyBytes int) ([]byte, []byte, error)
}

type mockKeyData struct {
	keyID     string
	algorithm string
	version   uint64
	status    string
	createdAt time.Time
}

func newMockKMSManager() *mockKMSManager {
	return &mockKMSManager{
		keys: make(map[string]*mockKeyData),
	}
}

func (m *mockKMSManager) GenerateKey(ctx context.Context, attrs *xkmssdk.KeyAttributes) (interface{}, error) {
	if m.generateKeyFn != nil {
		return m.generateKeyFn(ctx, attrs)
	}
	// Default behavior: store key
	keyID := attrs.CN
	algorithm := string(attrs.SymmetricAlgorithm)
	if algorithm == "" {
		algorithm = attrs.KeyAlgorithm.String()
	}
	m.keys[keyID] = &mockKeyData{
		keyID:     keyID,
		algorithm: algorithm,
		version:   1,
		status:    "enabled",
		createdAt: time.Now(),
	}
	return m.keys[keyID], nil
}

func (m *mockKMSManager) ImportKey(ctx context.Context, keyID string, material []byte, algorithm string) (interface{}, error) {
	if m.importKeyFn != nil {
		return m.importKeyFn(ctx, keyID, material, algorithm)
	}
	// Default behavior: store key
	m.keys[keyID] = &mockKeyData{
		keyID:     keyID,
		algorithm: algorithm,
		version:   1,
		status:    "enabled",
		createdAt: time.Now(),
	}
	return m.keys[keyID], nil
}

func (m *mockKMSManager) GetKey(ctx context.Context, keyID string) (interface{}, error) {
	if m.getKeyFn != nil {
		return m.getKeyFn(ctx, keyID)
	}
	// Default behavior: return key if exists as kms.KeyMetadata
	if key, ok := m.keys[keyID]; ok {
		return &kms.KeyMetadata{
			KeyID:     key.keyID,
			Name:      key.keyID,
			Algorithm: key.algorithm,
			Version:   int(key.version),
			State:     kms.KeyState(key.status),
			Created:   key.createdAt,
			Updated:   key.createdAt,
		}, nil
	}
	return nil, stderrors.New("key not found")
}

func (m *mockKMSManager) UpdateStoredMetadata(metadata interface{}) error {
	// No-op for mock - the metadata is already stored during key creation
	return nil
}

func (m *mockKMSManager) ListKeys(ctx context.Context, prefix string) ([]interface{}, error) {
	if m.listKeysFn != nil {
		return m.listKeysFn(ctx, prefix)
	}
	// Default behavior: return all keys as KeyMetadata
	result := make([]interface{}, 0, len(m.keys))
	for _, key := range m.keys {
		// Convert mockKeyData to KeyMetadata for proper type assertion in service
		km := &KeyMetadata{
			KeyID:     key.keyID,
			Name:      key.keyID, // Use keyID as name for mock
			Algorithm: KeyAlgorithm(key.algorithm),
			Version:   key.version,
			Status:    KeyStatus(key.status),
			CreatedAt: key.createdAt,
			UpdatedAt: key.createdAt,
		}
		result = append(result, km)
	}
	return result, nil
}

func (m *mockKMSManager) RotateKey(ctx context.Context, keyID string) (interface{}, error) {
	if m.rotateKeyFn != nil {
		return m.rotateKeyFn(ctx, keyID)
	}
	// Default behavior: increment version and return as kms.KeyMetadata
	if key, ok := m.keys[keyID]; ok {
		key.version++
		return &kms.KeyMetadata{
			KeyID:     key.keyID,
			Name:      key.keyID,
			Algorithm: key.algorithm,
			Version:   int(key.version),
			State:     kms.KeyState(key.status),
			Created:   key.createdAt,
			Updated:   time.Now(),
		}, nil
	}
	return nil, stderrors.New("key not found")
}

func (m *mockKMSManager) RevokeKey(ctx context.Context, keyID string) error {
	if m.revokeKeyFn != nil {
		return m.revokeKeyFn(ctx, keyID)
	}
	// Default behavior: mark as revoked
	if key, ok := m.keys[keyID]; ok {
		key.status = "compromised"
		return nil
	}
	return stderrors.New("key not found")
}

func (m *mockKMSManager) EnableKey(ctx context.Context, keyID string) error {
	// Default behavior: mark as enabled
	if key, ok := m.keys[keyID]; ok {
		key.status = "enabled"
		return nil
	}
	return stderrors.New("key not found")
}

func (m *mockKMSManager) DisableKey(ctx context.Context, keyID string) error {
	// Default behavior: mark as disabled
	if key, ok := m.keys[keyID]; ok {
		key.status = "disabled"
		return nil
	}
	return stderrors.New("key not found")
}

func (m *mockKMSManager) DeleteKey(ctx context.Context, keyID string) error {
	if m.deleteKeyFn != nil {
		return m.deleteKeyFn(ctx, keyID)
	}
	// Default behavior: delete key
	if _, ok := m.keys[keyID]; ok {
		delete(m.keys, keyID)
		return nil
	}
	return stderrors.New("key not found")
}

func (m *mockKMSManager) Encrypt(ctx context.Context, keyID string, plaintext, aad []byte) ([]byte, error) {
	if m.encryptFn != nil {
		return m.encryptFn(ctx, keyID, plaintext, aad)
	}
	// Default behavior: simple mock encryption
	return append([]byte("encrypted:"), plaintext...), nil
}

func (m *mockKMSManager) Decrypt(ctx context.Context, keyID string, ciphertext, aad []byte) ([]byte, error) {
	if m.decryptFn != nil {
		return m.decryptFn(ctx, keyID, ciphertext, aad)
	}
	// Default behavior: simple mock decryption
	if len(ciphertext) > 10 {
		return ciphertext[10:], nil
	}
	return nil, stderrors.New("invalid ciphertext")
}

func (m *mockKMSManager) Sign(ctx context.Context, keyID string, digest []byte, algorithm string) ([]byte, error) {
	if m.signFn != nil {
		return m.signFn(ctx, keyID, digest, algorithm)
	}
	// Default behavior: mock signature
	return append([]byte("signature:"), digest...), nil
}

func (m *mockKMSManager) Verify(ctx context.Context, keyID string, digest, signature []byte, algorithm string) (bool, error) {
	if m.verifyFn != nil {
		return m.verifyFn(ctx, keyID, digest, signature, algorithm)
	}
	// Default behavior: verify mock signature
	expected := append([]byte("signature:"), digest...)
	return string(expected) == string(signature), nil
}

func (m *mockKMSManager) GenerateDataKey(ctx context.Context, attrs *xkmssdk.KeyAttributes, keyBytes int) ([]byte, []byte, error) {
	if m.generateDataKeyFn != nil {
		return m.generateDataKeyFn(ctx, attrs, keyBytes)
	}
	// Default behavior: return mock plaintext and wrapped key
	plaintext := make([]byte, keyBytes)
	wrapped := append([]byte("wrapped:"), plaintext...)
	return plaintext, wrapped, nil
}

func (m *mockKMSManager) GetBackendType() config.BackendType {
	return config.BackendTypeSoftware
}

func (m *mockKMSManager) IsAvailable(ctx context.Context) bool {
	return true
}

func (m *mockKMSManager) Close() error {
	return nil
}

func (m *mockKMSManager) GetSupportedAlgorithms(ctx context.Context) ([]string, error) {
	return []string{
		"AES-128-GCM", "AES-192-GCM", "AES-256-GCM",
		"CHACHA20-POLY1305",
		"RSA-2048", "RSA-3072", "RSA-4096",
		"ECDSA-P256", "ECDSA-P384", "ECDSA-P521",
		"ED25519", "ED448",
	}, nil
}

func (m *mockKMSManager) GetPublicKey(ctx context.Context, keyID string) ([]byte, error) {
	// Default behavior: return mock public key for asymmetric keys
	if key, ok := m.keys[keyID]; ok {
		// Return mock public key bytes
		mockPubKey := []byte("mock-public-key-" + key.keyID)
		return mockPubKey, nil
	}
	return nil, stderrors.New("key not found")
}

// =============================================================================
// Mock Logger for Unit Testing
// =============================================================================

type mockLogger struct {
	fields map[string]interface{}
}

func newMockLogger() *mockLogger {
	return &mockLogger{
		fields: make(map[string]interface{}),
	}
}

func (l *mockLogger) Debug(msg string, args ...any)                              {}
func (l *mockLogger) Debugf(format string, args ...interface{})                  {}
func (l *mockLogger) Info(msg string, args ...any)                               {}
func (l *mockLogger) Infof(format string, args ...interface{})                   {}
func (l *mockLogger) Warn(msg string, args ...any)                               {}
func (l *mockLogger) Warnf(format string, args ...interface{})                   {}
func (l *mockLogger) Error(msg string, args ...any)                              {}
func (l *mockLogger) Errorf(format string, args ...interface{})                  {}
func (l *mockLogger) Fatal(msg string)                                           {}
func (l *mockLogger) Fatalf(format string, args ...interface{})                  {}
func (l *mockLogger) WithField(key string, value interface{}) interfaces.Logger  { return l }
func (l *mockLogger) WithFields(fields map[string]interface{}) interfaces.Logger { return l }
func (l *mockLogger) WithError(err error) interfaces.Logger                      { return l }

// =============================================================================
// GenerateKey Tests
// =============================================================================

func TestKMSService_GenerateKey_Success(t *testing.T) {
	mockManager := newMockKMSManager()
	logger := newMockLogger()
	svc := newKMSService(mockManager, logger)

	req := &GenerateKeyRequest{
		Name:        "test-key",
		Description: "Test key for unit tests",
		KeyAttributes: &xkmssdk.KeyAttributes{
			SymmetricAlgorithm: xkmssdk.SymmetricAES256GCM,
		},
		Usage: []KeyUsage{KeyUsageEncrypt, KeyUsageDecrypt},
		Owner: "test-user",
		Tags:  map[string]string{"env": "test"},
	}

	resp, err := svc.GenerateKey(systemTenantCtx(), req)

	require.NoError(t, err)
	assert.NotNil(t, resp)
	assert.NotEmpty(t, resp.Key.KeyID)
	assert.Equal(t, uint64(1), resp.Key.Version)
	assert.Equal(t, KeyAlgAES256GCM, resp.Key.Algorithm)
	assert.Equal(t, KeyStatusEnabled, resp.Key.Status)
	assert.Equal(t, "test-key", resp.Metadata.Name)
	assert.Equal(t, "Test key for unit tests", resp.Metadata.Description)
	assert.Equal(t, "test-user", resp.Metadata.Owner)
}

func TestKMSService_GenerateKey_NilRequest(t *testing.T) {
	mockManager := newMockKMSManager()
	logger := newMockLogger()
	svc := newKMSService(mockManager, logger)

	resp, err := svc.GenerateKey(systemTenantCtx(), nil)

	require.Error(t, err)
	assert.Nil(t, resp)
	assert.Contains(t, err.Error(), "request is nil")
}

func TestKMSService_GenerateKey_MissingAlgorithm(t *testing.T) {
	mockManager := newMockKMSManager()
	logger := newMockLogger()
	svc := newKMSService(mockManager, logger)

	req := &GenerateKeyRequest{
		Name:          "test-key",
		KeyAttributes: &xkmssdk.KeyAttributes{},
		Usage:         []KeyUsage{KeyUsageEncrypt},
	}

	resp, err := svc.GenerateKey(systemTenantCtx(), req)

	require.Error(t, err)
	assert.Nil(t, resp)
	assert.Contains(t, err.Error(), "algorithm is required")
}

func TestKMSService_GenerateKey_MissingUsage(t *testing.T) {
	mockManager := newMockKMSManager()
	logger := newMockLogger()
	svc := newKMSService(mockManager, logger)

	req := &GenerateKeyRequest{
		Name: "test-key",
		KeyAttributes: &xkmssdk.KeyAttributes{
			SymmetricAlgorithm: xkmssdk.SymmetricAES256GCM,
		},
		Usage: []KeyUsage{},
	}

	resp, err := svc.GenerateKey(systemTenantCtx(), req)

	require.Error(t, err)
	assert.Nil(t, resp)
	assert.Contains(t, err.Error(), "at least one key usage is required")
}

func TestKMSService_GenerateKey_ManagerError(t *testing.T) {
	mockManager := newMockKMSManager()
	mockManager.generateKeyFn = func(ctx context.Context, attrs *xkmssdk.KeyAttributes) (interface{}, error) {
		return nil, stderrors.New("HSM connection failed")
	}
	logger := newMockLogger()
	svc := newKMSService(mockManager, logger)

	req := &GenerateKeyRequest{
		Name: "test-key",
		KeyAttributes: &xkmssdk.KeyAttributes{
			SymmetricAlgorithm: xkmssdk.SymmetricAES256GCM,
		},
		Usage: []KeyUsage{KeyUsageEncrypt},
	}

	resp, err := svc.GenerateKey(systemTenantCtx(), req)

	require.Error(t, err)
	assert.Nil(t, resp)
	var dErr *quicbaasErrors.QBError
	if errors.As(err, &dErr) {
		assert.Equal(t, quicbaasErrors.ErrInternal, dErr.Code)
	}
}

// =============================================================================
// ImportKey Tests
// =============================================================================

func TestKMSService_ImportKey_Success(t *testing.T) {
	mockManager := newMockKMSManager()
	// Pre-create wrapping key
	mockManager.keys["wrapping-key-1"] = &mockKeyData{
		keyID:     "wrapping-key-1",
		algorithm: "RSA-2048",
		version:   1,
		status:    "enabled",
		createdAt: time.Now(),
	}
	logger := newMockLogger()
	svc := newKMSService(mockManager, logger)

	req := &ImportKeyRequest{
		Name:          "imported-key",
		Description:   "Imported test key",
		KeyMaterial:   []byte("mock-encrypted-key-material"),
		WrappingKeyID: "wrapping-key-1",
		KeyAttributes: &xkmssdk.KeyAttributes{
			SymmetricAlgorithm: xkmssdk.SymmetricAES256GCM,
		},
		Usage: []KeyUsage{KeyUsageEncrypt, KeyUsageDecrypt},
	}

	resp, err := svc.ImportKey(systemTenantCtx(), req)

	require.NoError(t, err)
	assert.NotNil(t, resp)
	assert.NotEmpty(t, resp.Key.KeyID)
	assert.Equal(t, uint64(1), resp.Key.Version)
	assert.Equal(t, KeyAlgAES256GCM, resp.Key.Algorithm)
	assert.Equal(t, KeyStatusEnabled, resp.Key.Status)
}

func TestKMSService_ImportKey_NilRequest(t *testing.T) {
	mockManager := newMockKMSManager()
	logger := newMockLogger()
	svc := newKMSService(mockManager, logger)

	resp, err := svc.ImportKey(systemTenantCtx(), nil)

	require.Error(t, err)
	assert.Nil(t, resp)
	assert.Contains(t, err.Error(), "request is nil")
}

func TestKMSService_ImportKey_MissingKeyMaterial(t *testing.T) {
	mockManager := newMockKMSManager()
	logger := newMockLogger()
	svc := newKMSService(mockManager, logger)

	req := &ImportKeyRequest{
		Name:          "imported-key",
		WrappingKeyID: "wrapping-key-1",
		KeyAttributes: &xkmssdk.KeyAttributes{
			SymmetricAlgorithm: xkmssdk.SymmetricAES256GCM,
		},
		Usage: []KeyUsage{KeyUsageEncrypt},
	}

	resp, err := svc.ImportKey(systemTenantCtx(), req)

	require.Error(t, err)
	assert.Nil(t, resp)
	assert.Contains(t, err.Error(), "key material is required")
}

func TestKMSService_ImportKey_WithoutWrappingKey(t *testing.T) {
	// WrappingKeyID is optional - if not provided, key material is assumed to be raw/unwrapped
	// This allows testing with raw key material while production can enforce wrapping
	mockManager := newMockKMSManager()
	logger := newMockLogger()
	svc := newKMSService(mockManager, logger)

	req := &ImportKeyRequest{
		Name:        "imported-key",
		KeyMaterial: []byte("mock-key-material-32bytes!!!!!!!"), // 32 bytes for AES-256
		KeyAttributes: &xkmssdk.KeyAttributes{
			SymmetricAlgorithm: xkmssdk.SymmetricAES256GCM,
		},
		Usage: []KeyUsage{KeyUsageEncrypt},
	}

	resp, err := svc.ImportKey(systemTenantCtx(), req)

	// Import without wrapping key should succeed (key material is raw)
	require.NoError(t, err)
	assert.NotNil(t, resp)
	assert.NotNil(t, resp.Key)
	assert.Contains(t, resp.Key.KeyID, "imported-key")
}

func TestKMSService_ImportKey_ManagerError(t *testing.T) {
	mockManager := newMockKMSManager()
	mockManager.importKeyFn = func(ctx context.Context, keyID string, material []byte, algorithm string) (interface{}, error) {
		return nil, stderrors.New("unwrap failed")
	}
	logger := newMockLogger()
	svc := newKMSService(mockManager, logger)

	req := &ImportKeyRequest{
		Name:          "imported-key",
		KeyMaterial:   []byte("mock-key"),
		WrappingKeyID: "wrapping-key-1",
		KeyAttributes: &xkmssdk.KeyAttributes{
			SymmetricAlgorithm: xkmssdk.SymmetricAES256GCM,
		},
		Usage: []KeyUsage{KeyUsageEncrypt},
	}

	resp, err := svc.ImportKey(systemTenantCtx(), req)

	require.Error(t, err)
	assert.Nil(t, resp)
}

// TestKMSService_ImportKey_ManagerClassifiedErrorKeepsItsCode pins that the
// service keeps the manager's classified failure: importing a key that
// already exists must stay AlreadyExists, not be re-stamped as Internal.
func TestKMSService_ImportKey_ManagerClassifiedErrorKeepsItsCode(t *testing.T) {
	tests := []struct {
		name     string
		mgrErr   error
		wantCode errors.ErrorCode
	}{
		{"already exists", errors.New(errors.ErrAlreadyExists, "ImportKey", "key already exists"), errors.ErrAlreadyExists},
		{"invalid input", errors.New(errors.ErrInvalidInput, "ImportKey", "bad material"), errors.ErrInvalidInput},
		{"key not found sentinel", kms.ErrKeyNotFound, errors.ErrNotFound},
		{"unclassified", stderrors.New("backend unavailable"), errors.ErrInternal},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockManager := newMockKMSManager()
			mockManager.importKeyFn = func(ctx context.Context, keyID string, material []byte, algorithm string) (interface{}, error) {
				return nil, tt.mgrErr
			}
			svc := newKMSService(mockManager, newMockLogger())

			resp, err := svc.ImportKey(systemTenantCtx(), &ImportKeyRequest{
				Name:        "imported-key",
				KeyMaterial: []byte("mock-key"),
				KeyAttributes: &xkmssdk.KeyAttributes{
					SymmetricAlgorithm: xkmssdk.SymmetricAES256GCM,
				},
				Usage: []KeyUsage{KeyUsageEncrypt},
			})

			require.Error(t, err)
			assert.Nil(t, resp)
			var qbErr *errors.QBError
			require.True(t, stderrors.As(err, &qbErr), "service error must be a QBError, got %T", err)
			assert.Equal(t, tt.wantCode, qbErr.Code)
		})
	}
}

// =============================================================================
// GetKey Tests
// =============================================================================

func TestKMSService_GetKey_Success(t *testing.T) {
	mockManager := newMockKMSManager()
	// Pre-create a key
	mockManager.keys["test-key-123"] = &mockKeyData{
		keyID:     "test-key-123",
		algorithm: "AES-256-GCM",
		version:   1,
		status:    "enabled",
		createdAt: time.Now(),
	}
	logger := newMockLogger()
	svc := newKMSService(mockManager, logger)

	req := &GetKeyRequest{
		KeyID:   "test-key-123",
		Version: 1,
	}

	resp, err := svc.GetKey(systemTenantCtx(), req)

	require.NoError(t, err)
	assert.NotNil(t, resp)
	assert.Equal(t, "test-key-123", resp.Key.KeyID)
}

func TestKMSService_GetKey_NilRequest(t *testing.T) {
	mockManager := newMockKMSManager()
	logger := newMockLogger()
	svc := newKMSService(mockManager, logger)

	resp, err := svc.GetKey(systemTenantCtx(), nil)

	require.Error(t, err)
	assert.Nil(t, resp)
	assert.Contains(t, err.Error(), "request is nil")
}

func TestKMSService_GetKey_MissingKeyID(t *testing.T) {
	mockManager := newMockKMSManager()
	logger := newMockLogger()
	svc := newKMSService(mockManager, logger)

	req := &GetKeyRequest{
		KeyID: "",
	}

	resp, err := svc.GetKey(systemTenantCtx(), req)

	require.Error(t, err)
	assert.Nil(t, resp)
	assert.Contains(t, err.Error(), "key ID is required")
}

func TestKMSService_GetKey_NotFound(t *testing.T) {
	mockManager := newMockKMSManager()
	logger := newMockLogger()
	svc := newKMSService(mockManager, logger)

	req := &GetKeyRequest{
		KeyID: "nonexistent-key",
	}

	resp, err := svc.GetKey(systemTenantCtx(), req)

	require.Error(t, err)
	assert.Nil(t, resp)
	var dErr *quicbaasErrors.QBError
	if errors.As(err, &dErr) {
		assert.Equal(t, quicbaasErrors.ErrNotFound, dErr.Code)
	}
}

// =============================================================================
// ListKeys Tests
// =============================================================================

func TestKMSService_ListKeys_Success(t *testing.T) {
	mockManager := newMockKMSManager()
	// Pre-create some keys
	for i := 0; i < 5; i++ {
		keyID := "test-key-" + string(rune('a'+i))
		mockManager.keys[keyID] = &mockKeyData{
			keyID:     keyID,
			algorithm: "AES-256-GCM",
			version:   1,
			status:    "enabled",
			createdAt: time.Now(),
		}
	}
	logger := newMockLogger()
	svc := newKMSService(mockManager, logger)

	req := &ListKeysRequest{
		Limit: 10,
	}

	resp, err := svc.ListKeys(systemTenantCtx(), req)

	require.NoError(t, err)
	assert.NotNil(t, resp)
	assert.Equal(t, 5, resp.TotalCount)
	assert.Len(t, resp.Keys, 5)
	assert.False(t, resp.HasMore)
}

func TestKMSService_ListKeys_Empty(t *testing.T) {
	mockManager := newMockKMSManager()
	logger := newMockLogger()
	svc := newKMSService(mockManager, logger)

	req := &ListKeysRequest{}

	resp, err := svc.ListKeys(systemTenantCtx(), req)

	require.NoError(t, err)
	assert.NotNil(t, resp)
	assert.Equal(t, 0, resp.TotalCount)
	assert.Empty(t, resp.Keys)
}

func TestKMSService_ListKeys_ManagerError(t *testing.T) {
	mockManager := newMockKMSManager()
	mockManager.listKeysFn = func(ctx context.Context, prefix string) ([]interface{}, error) {
		return nil, stderrors.New("database error")
	}
	logger := newMockLogger()
	svc := newKMSService(mockManager, logger)

	req := &ListKeysRequest{}

	resp, err := svc.ListKeys(systemTenantCtx(), req)

	require.Error(t, err)
	assert.Nil(t, resp)
}

// =============================================================================
// RotateKey Tests
// =============================================================================

func TestKMSService_RotateKey_Success(t *testing.T) {
	mockManager := newMockKMSManager()
	mockManager.keys["test-key-123"] = &mockKeyData{
		keyID:     "test-key-123",
		algorithm: "AES-256-GCM",
		version:   1,
		status:    "enabled",
		createdAt: time.Now(),
	}
	logger := newMockLogger()
	svc := newKMSService(mockManager, logger)

	req := &RotateKeyRequest{
		KeyID: "test-key-123",
	}

	resp, err := svc.RotateKey(systemTenantCtx(), req)

	require.NoError(t, err)
	assert.NotNil(t, resp)
	assert.Equal(t, "test-key-123", resp.KeyID)
	assert.Equal(t, uint64(1), resp.OldVersion)
	assert.Equal(t, uint64(2), resp.NewVersion)
}

func TestKMSService_RotateKey_NilRequest(t *testing.T) {
	mockManager := newMockKMSManager()
	logger := newMockLogger()
	svc := newKMSService(mockManager, logger)

	resp, err := svc.RotateKey(systemTenantCtx(), nil)

	require.Error(t, err)
	assert.Nil(t, resp)
	assert.Contains(t, err.Error(), "request is nil")
}

func TestKMSService_RotateKey_MissingKeyID(t *testing.T) {
	mockManager := newMockKMSManager()
	logger := newMockLogger()
	svc := newKMSService(mockManager, logger)

	req := &RotateKeyRequest{
		KeyID: "",
	}

	resp, err := svc.RotateKey(systemTenantCtx(), req)

	require.Error(t, err)
	assert.Nil(t, resp)
	assert.Contains(t, err.Error(), "key ID is required")
}

func TestKMSService_RotateKey_NotFound(t *testing.T) {
	mockManager := newMockKMSManager()
	mockManager.rotateKeyFn = func(ctx context.Context, keyID string) (interface{}, error) {
		return nil, stderrors.New("key not found")
	}
	logger := newMockLogger()
	svc := newKMSService(mockManager, logger)

	req := &RotateKeyRequest{
		KeyID: "nonexistent-key",
	}

	resp, err := svc.RotateKey(systemTenantCtx(), req)

	require.Error(t, err)
	assert.Nil(t, resp)
}

// =============================================================================
// RevokeKey Tests
// =============================================================================

func TestKMSService_RevokeKey_Success(t *testing.T) {
	mockManager := newMockKMSManager()
	mockManager.keys["test-key-123"] = &mockKeyData{
		keyID:     "test-key-123",
		algorithm: "AES-256-GCM",
		version:   1,
		status:    "enabled",
		createdAt: time.Now(),
	}
	logger := newMockLogger()
	svc := newKMSService(mockManager, logger)

	req := &RevokeKeyRequest{
		KeyID:  "test-key-123",
		Reason: "key compromised in breach",
	}

	resp, err := svc.RevokeKey(systemTenantCtx(), req)

	require.NoError(t, err)
	assert.NotNil(t, resp)
	assert.Equal(t, "test-key-123", resp.KeyID)
	assert.Equal(t, KeyStatusCompromised, resp.Status)
}

func TestKMSService_RevokeKey_NilRequest(t *testing.T) {
	mockManager := newMockKMSManager()
	logger := newMockLogger()
	svc := newKMSService(mockManager, logger)

	resp, err := svc.RevokeKey(systemTenantCtx(), nil)

	require.Error(t, err)
	assert.Nil(t, resp)
	assert.Contains(t, err.Error(), "request is nil")
}

func TestKMSService_RevokeKey_MissingKeyID(t *testing.T) {
	mockManager := newMockKMSManager()
	logger := newMockLogger()
	svc := newKMSService(mockManager, logger)

	req := &RevokeKeyRequest{
		KeyID: "",
	}

	resp, err := svc.RevokeKey(systemTenantCtx(), req)

	require.Error(t, err)
	assert.Nil(t, resp)
	assert.Contains(t, err.Error(), "key ID is required")
}

// =============================================================================
// DeleteKey Tests
// =============================================================================

func TestKMSService_DeleteKey_Success(t *testing.T) {
	mockManager := newMockKMSManager()
	mockManager.keys["test-key-123"] = &mockKeyData{
		keyID:     "test-key-123",
		algorithm: "AES-256-GCM",
		version:   1,
		status:    "enabled",
		createdAt: time.Now(),
	}
	logger := newMockLogger()
	svc := newKMSService(mockManager, logger)

	req := &DeleteKeyRequest{
		KeyID:             "test-key-123",
		PendingWindowDays: 14,
	}

	resp, err := svc.DeleteKey(systemTenantCtx(), req)

	require.NoError(t, err)
	assert.NotNil(t, resp)
	assert.Equal(t, "test-key-123", resp.KeyID)
	assert.Equal(t, KeyStatusPendingDeletion, resp.Status)
	// Check deletion date is ~14 days in the future
	expectedDate := time.Now().Add(14 * 24 * time.Hour)
	assert.WithinDuration(t, expectedDate, resp.DeletionDate, time.Minute)
}

func TestKMSService_DeleteKey_DefaultPendingWindow(t *testing.T) {
	mockManager := newMockKMSManager()
	mockManager.keys["test-key-123"] = &mockKeyData{
		keyID:     "test-key-123",
		algorithm: "AES-256-GCM",
		version:   1,
		status:    "enabled",
		createdAt: time.Now(),
	}
	logger := newMockLogger()
	svc := newKMSService(mockManager, logger)

	req := &DeleteKeyRequest{
		KeyID:             "test-key-123",
		PendingWindowDays: 0, // Should default to 7 days
	}

	resp, err := svc.DeleteKey(systemTenantCtx(), req)

	require.NoError(t, err)
	assert.NotNil(t, resp)
	// Check deletion date is ~7 days in the future
	expectedDate := time.Now().Add(7 * 24 * time.Hour)
	assert.WithinDuration(t, expectedDate, resp.DeletionDate, time.Minute)
}

func TestKMSService_DeleteKey_MinimumPendingWindow(t *testing.T) {
	mockManager := newMockKMSManager()
	mockManager.keys["test-key-123"] = &mockKeyData{
		keyID:     "test-key-123",
		algorithm: "AES-256-GCM",
		version:   1,
		status:    "enabled",
		createdAt: time.Now(),
	}
	logger := newMockLogger()
	svc := newKMSService(mockManager, logger)

	req := &DeleteKeyRequest{
		KeyID:             "test-key-123",
		PendingWindowDays: 3, // Should be clamped to minimum of 7 days
	}

	resp, err := svc.DeleteKey(systemTenantCtx(), req)

	require.NoError(t, err)
	assert.NotNil(t, resp)
	// Check deletion date is ~7 days in the future (minimum)
	expectedDate := time.Now().Add(7 * 24 * time.Hour)
	assert.WithinDuration(t, expectedDate, resp.DeletionDate, time.Minute)
}

func TestKMSService_DeleteKey_NilRequest(t *testing.T) {
	mockManager := newMockKMSManager()
	logger := newMockLogger()
	svc := newKMSService(mockManager, logger)

	resp, err := svc.DeleteKey(systemTenantCtx(), nil)

	require.Error(t, err)
	assert.Nil(t, resp)
	assert.Contains(t, err.Error(), "request is nil")
}

func TestKMSService_DeleteKey_MissingKeyID(t *testing.T) {
	mockManager := newMockKMSManager()
	logger := newMockLogger()
	svc := newKMSService(mockManager, logger)

	req := &DeleteKeyRequest{
		KeyID: "",
	}

	resp, err := svc.DeleteKey(systemTenantCtx(), req)

	require.Error(t, err)
	assert.Nil(t, resp)
	assert.Contains(t, err.Error(), "key ID is required")
}

func TestKMSService_DeleteKey_InvalidPendingWindow(t *testing.T) {
	mockManager := newMockKMSManager()
	logger := newMockLogger()
	svc := newKMSService(mockManager, logger)

	req := &DeleteKeyRequest{
		KeyID:             "test-key-123",
		PendingWindowDays: 45, // Invalid: > 30
	}

	resp, err := svc.DeleteKey(systemTenantCtx(), req)

	require.Error(t, err)
	assert.Nil(t, resp)
	assert.Contains(t, err.Error(), "pending window days must be between 0 and 30")
}

// =============================================================================
// GetKeyMetadata Tests
// =============================================================================

func TestKMSService_GetKeyMetadata_Success(t *testing.T) {
	mockManager := newMockKMSManager()
	mockManager.keys["test-key-123"] = &mockKeyData{
		keyID:     "test-key-123",
		algorithm: "AES-256-GCM",
		version:   1,
		status:    "enabled",
		createdAt: time.Now(),
	}
	logger := newMockLogger()
	svc := newKMSService(mockManager, logger)

	req := &GetKeyMetadataRequest{
		KeyID: "test-key-123",
	}

	resp, err := svc.GetKeyMetadata(systemTenantCtx(), req)

	require.NoError(t, err)
	assert.NotNil(t, resp)
	assert.Equal(t, "test-key-123", resp.Metadata.KeyID)
	assert.Equal(t, KeyStatusEnabled, resp.Metadata.Status)
}

func TestKMSService_GetKeyMetadata_NilRequest(t *testing.T) {
	mockManager := newMockKMSManager()
	logger := newMockLogger()
	svc := newKMSService(mockManager, logger)

	resp, err := svc.GetKeyMetadata(systemTenantCtx(), nil)

	require.Error(t, err)
	assert.Nil(t, resp)
	assert.Contains(t, err.Error(), "request is nil")
}

func TestKMSService_GetKeyMetadata_NotFound(t *testing.T) {
	mockManager := newMockKMSManager()
	logger := newMockLogger()
	svc := newKMSService(mockManager, logger)

	req := &GetKeyMetadataRequest{
		KeyID: "nonexistent-key",
	}

	resp, err := svc.GetKeyMetadata(systemTenantCtx(), req)

	require.Error(t, err)
	assert.Nil(t, resp)
}

// =============================================================================
// UpdateKeyMetadata Tests
// =============================================================================

func TestKMSService_UpdateKeyMetadata_Success(t *testing.T) {
	mockManager := newMockKMSManager()
	mockManager.keys["test-key-123"] = &mockKeyData{
		keyID:     "test-key-123",
		algorithm: "AES-256-GCM",
		version:   1,
		status:    "enabled",
		createdAt: time.Now(),
	}
	logger := newMockLogger()
	svc := newKMSService(mockManager, logger)

	newName := "updated-key-name"
	newDesc := "Updated description"
	req := &UpdateKeyMetadataRequest{
		KeyID:       "test-key-123",
		Name:        &newName,
		Description: &newDesc,
		Tags:        map[string]string{"env": "production"},
	}

	resp, err := svc.UpdateKeyMetadata(systemTenantCtx(), req)

	require.NoError(t, err)
	assert.NotNil(t, resp)
	assert.Equal(t, "test-key-123", resp.Metadata.KeyID)
	assert.Equal(t, "updated-key-name", resp.Metadata.Name)
	assert.Equal(t, "Updated description", resp.Metadata.Description)
	assert.Equal(t, "production", resp.Metadata.Tags["env"])
}

func TestKMSService_UpdateKeyMetadata_NilRequest(t *testing.T) {
	mockManager := newMockKMSManager()
	logger := newMockLogger()
	svc := newKMSService(mockManager, logger)

	resp, err := svc.UpdateKeyMetadata(systemTenantCtx(), nil)

	require.Error(t, err)
	assert.Nil(t, resp)
	assert.Contains(t, err.Error(), "request is nil")
}

func TestKMSService_UpdateKeyMetadata_NoFieldsToUpdate(t *testing.T) {
	mockManager := newMockKMSManager()
	logger := newMockLogger()
	svc := newKMSService(mockManager, logger)

	req := &UpdateKeyMetadataRequest{
		KeyID: "test-key-123",
		// No fields to update
	}

	resp, err := svc.UpdateKeyMetadata(systemTenantCtx(), req)

	require.Error(t, err)
	assert.Nil(t, resp)
	assert.Contains(t, err.Error(), "at least one field must be updated")
}

// =============================================================================
// SetKeyPolicy Tests
// =============================================================================

func TestKMSService_SetKeyPolicy_Success(t *testing.T) {
	mockManager := newMockKMSManager()
	mockManager.keys["test-key-123"] = &mockKeyData{
		keyID:     "test-key-123",
		algorithm: "AES-256-GCM",
		version:   1,
		status:    "enabled",
		createdAt: time.Now(),
	}
	logger := newMockLogger()
	svc := newKMSService(mockManager, logger)

	req := &SetKeyPolicyRequest{
		KeyID: "test-key-123",
		Policy: &KeyPolicy{
			KeyID:             "test-key-123",
			Version:           1,
			AllowedOperations: []KeyUsage{KeyUsageEncrypt, KeyUsageDecrypt},
			AllowedPrincipals: []string{"user-1", "service-account-1"},
		},
	}

	resp, err := svc.SetKeyPolicy(systemTenantCtx(), req)

	require.NoError(t, err)
	assert.NotNil(t, resp)
	assert.Equal(t, "test-key-123", resp.KeyID)
	assert.Equal(t, uint64(2), resp.PolicyVersion) // Version incremented
}

func TestKMSService_SetKeyPolicy_NilRequest(t *testing.T) {
	mockManager := newMockKMSManager()
	logger := newMockLogger()
	svc := newKMSService(mockManager, logger)

	resp, err := svc.SetKeyPolicy(systemTenantCtx(), nil)

	require.Error(t, err)
	assert.Nil(t, resp)
	assert.Contains(t, err.Error(), "request is nil")
}

func TestKMSService_SetKeyPolicy_NilPolicy(t *testing.T) {
	mockManager := newMockKMSManager()
	logger := newMockLogger()
	svc := newKMSService(mockManager, logger)

	req := &SetKeyPolicyRequest{
		KeyID:  "test-key-123",
		Policy: nil,
	}

	resp, err := svc.SetKeyPolicy(systemTenantCtx(), req)

	require.Error(t, err)
	assert.Nil(t, resp)
	assert.Contains(t, err.Error(), "policy is required")
}

func TestKMSService_SetKeyPolicy_NoAllowedOperations(t *testing.T) {
	// Empty AllowedOperations is now valid: it means "all operations are permitted
	// for any principal passing AllowedPrincipals checks". A key-not-found error
	// is expected because the mock manager has no key "test-key-123" by default.
	mockManager := newMockKMSManager()
	logger := newMockLogger()
	svc := newKMSService(mockManager, logger)

	req := &SetKeyPolicyRequest{
		KeyID: "test-key-123",
		Policy: &KeyPolicy{
			KeyID:             "test-key-123",
			AllowedOperations: []KeyUsage{},
		},
	}

	_, err := svc.SetKeyPolicy(systemTenantCtx(), req)
	// The validator now accepts empty AllowedOperations; the error comes from
	// the key lookup failing (key does not exist in the mock manager).
	require.Error(t, err)
	assert.Contains(t, err.Error(), "key not found")
}

// =============================================================================
// GetKeyPolicy Tests
// =============================================================================

func TestKMSService_GetKeyPolicy_Success(t *testing.T) {
	mockManager := newMockKMSManager()
	mockManager.keys["test-key-123"] = &mockKeyData{
		keyID:     "test-key-123",
		algorithm: "AES-256-GCM",
		version:   1,
		status:    "enabled",
		createdAt: time.Now(),
	}
	logger := newMockLogger()
	svc := newKMSService(mockManager, logger)

	req := &GetKeyPolicyRequest{
		KeyID: "test-key-123",
	}

	resp, err := svc.GetKeyPolicy(systemTenantCtx(), req)

	require.NoError(t, err)
	assert.NotNil(t, resp)
	assert.Equal(t, "test-key-123", resp.Policy.KeyID)
	// No policy stored → default open policy has empty AllowedOperations (allow all).
	// AllowedOperations being empty means all operations are permitted.
	assert.NotNil(t, resp.Policy)
}

func TestKMSService_GetKeyPolicy_NilRequest(t *testing.T) {
	mockManager := newMockKMSManager()
	logger := newMockLogger()
	svc := newKMSService(mockManager, logger)

	resp, err := svc.GetKeyPolicy(systemTenantCtx(), nil)

	require.Error(t, err)
	assert.Nil(t, resp)
	assert.Contains(t, err.Error(), "request is nil")
}

func TestKMSService_GetKeyPolicy_NotFound(t *testing.T) {
	mockManager := newMockKMSManager()
	logger := newMockLogger()
	svc := newKMSService(mockManager, logger)

	req := &GetKeyPolicyRequest{
		KeyID: "nonexistent-key",
	}

	resp, err := svc.GetKeyPolicy(systemTenantCtx(), req)

	require.Error(t, err)
	assert.Nil(t, resp)
}

// =============================================================================
// Service Construction Tests
// =============================================================================

func TestNewKMSServiceImpl_NilLogger(t *testing.T) {
	mockManager := newMockKMSManager()
	svc := newKMSService(mockManager, nil)

	// Service should still be usable without logger
	req := &GenerateKeyRequest{
		Name: "test-key",
		KeyAttributes: &xkmssdk.KeyAttributes{
			SymmetricAlgorithm: xkmssdk.SymmetricAES256GCM,
		},
		Usage: []KeyUsage{KeyUsageEncrypt},
	}

	resp, err := svc.GenerateKey(systemTenantCtx(), req)

	require.NoError(t, err)
	assert.NotNil(t, resp)
}

func TestKMSService_MetricsInitialized(t *testing.T) {
	mockManager := newMockKMSManager()
	logger := newMockLogger()
	svc := newKMSService(mockManager, logger)

	// Verify metrics are initialized
	assert.NotNil(t, svc.metrics)
	assert.NotNil(t, svc.metrics.keysGeneratedTotal)
	assert.NotNil(t, svc.metrics.keysRotatedTotal)
	assert.NotNil(t, svc.metrics.encryptionOperationsTotal)
	assert.NotNil(t, svc.metrics.decryptionOperationsTotal)
	assert.NotNil(t, svc.metrics.keyOperationDuration)
	assert.NotNil(t, svc.metrics.activeKeys)
	assert.NotNil(t, svc.metrics.errorsByType)
}

// =============================================================================
// Context Cancellation Tests
// =============================================================================

func TestKMSService_GenerateKey_ContextCanceled(t *testing.T) {
	mockManager := newMockKMSManager()
	mockManager.generateKeyFn = func(ctx context.Context, attrs *xkmssdk.KeyAttributes) (interface{}, error) {
		return nil, ctx.Err()
	}
	logger := newMockLogger()
	svc := newKMSService(mockManager, logger)

	ctx, cancel := context.WithCancel(systemTenantCtx())
	cancel() // Cancel immediately

	req := &GenerateKeyRequest{
		Name: "test-key",
		KeyAttributes: &xkmssdk.KeyAttributes{
			SymmetricAlgorithm: xkmssdk.SymmetricAES256GCM,
		},
		Usage: []KeyUsage{KeyUsageEncrypt},
	}

	resp, err := svc.GenerateKey(ctx, req)

	require.Error(t, err)
	assert.Nil(t, resp)
}

// =============================================================================
// Algorithm Validation Tests (Future Enhancement)
// =============================================================================

func TestKeyAlgorithm_AllSupportedAlgorithms(t *testing.T) {
	supportedAlgorithms := []KeyAlgorithm{
		KeyAlgAES128GCM,
		KeyAlgAES192GCM,
		KeyAlgAES256GCM,
		KeyAlgChaCha20Poly1305,
		KeyAlgRSA2048,
		KeyAlgRSA3072,
		KeyAlgRSA4096,
		KeyAlgECDSAP256,
		KeyAlgECDSAP384,
		KeyAlgECDSAP521,
		KeyAlgEd25519,
		KeyAlgEd448,
		KeyAlgX25519,
		KeyAlgX448,
		KeyAlgHMACSHA256,
		KeyAlgHMACSHA384,
		KeyAlgHMACSHA512,
	}

	for _, alg := range supportedAlgorithms {
		t.Run(string(alg), func(t *testing.T) {
			assert.NotEmpty(t, alg, "Algorithm should not be empty")
		})
	}
}

func TestKeyUsage_AllSupportedUsages(t *testing.T) {
	supportedUsages := []KeyUsage{
		KeyUsageEncrypt,
		KeyUsageDecrypt,
		KeyUsageSign,
		KeyUsageVerify,
		KeyUsageWrapKey,
		KeyUsageDeriveKey,
		KeyUsageGenerateMAC,
		KeyUsageVerifyMAC,
	}

	for _, usage := range supportedUsages {
		t.Run(string(usage), func(t *testing.T) {
			assert.NotEmpty(t, usage, "Usage should not be empty")
		})
	}
}

func TestKeyStatus_AllSupportedStatuses(t *testing.T) {
	supportedStatuses := []KeyStatus{
		KeyStatusEnabled,
		KeyStatusDisabled,
		KeyStatusPendingDeletion,
		KeyStatusDeleted,
		KeyStatusRotating,
		KeyStatusCompromised,
	}

	for _, status := range supportedStatuses {
		t.Run(string(status), func(t *testing.T) {
			assert.NotEmpty(t, status, "Status should not be empty")
		})
	}
}

// =============================================================================
// Backend Status and Test Tests
// =============================================================================

func TestKMSService_BackendStatus_Success(t *testing.T) {
	mockMgr := newMockKMSManager()
	logger := newMockLogger()

	svc := newKMSService(mockMgr, logger)
	require.NotNil(t, svc)

	ctx := systemTenantCtx()
	resp, err := svc.BackendStatus(ctx)
	require.NoError(t, err)
	require.NotNil(t, resp)

	assert.Equal(t, "software", resp.BackendType)
	assert.Equal(t, "healthy", resp.Status)
	assert.NotNil(t, resp.Capabilities)
	assert.True(t, resp.Capabilities["encryption"])
	assert.True(t, resp.Capabilities["decryption"])
	assert.True(t, resp.Capabilities["key_generation"])
	assert.True(t, resp.Capabilities["key_rotation"])
	assert.False(t, resp.Timestamp.IsZero())
}

func TestKMSService_BackendTest_Success(t *testing.T) {
	mockMgr := newMockKMSManager()
	logger := newMockLogger()

	svc := newKMSService(mockMgr, logger)
	require.NotNil(t, svc)

	ctx := systemTenantCtx()
	resp, err := svc.BackendTest(ctx)
	require.NoError(t, err)
	require.NotNil(t, resp)

	assert.True(t, resp.Success)
	assert.True(t, resp.Duration > 0)
	assert.Contains(t, resp.Message, "successfully")
	assert.False(t, resp.Timestamp.IsZero())
}

func TestKMSService_BackendTest_KeyGenerationFailure(t *testing.T) {
	mockMgr := newMockKMSManager()
	mockMgr.generateKeyFn = func(ctx context.Context, attrs *xkmssdk.KeyAttributes) (interface{}, error) {
		return nil, stderrors.New("key generation failed")
	}
	logger := newMockLogger()

	svc := newKMSService(mockMgr, logger)
	require.NotNil(t, svc)

	ctx := systemTenantCtx()
	resp, err := svc.BackendTest(ctx)
	require.NoError(t, err) // Should return response, not error
	require.NotNil(t, resp)

	assert.False(t, resp.Success)
	assert.Contains(t, resp.Message, "key generation failed")
}

func TestKMSService_BackendTest_EncryptionFailure(t *testing.T) {
	mockMgr := newMockKMSManager()
	mockMgr.encryptFn = func(ctx context.Context, keyID string, plaintext, aad []byte) ([]byte, error) {
		return nil, stderrors.New("encryption failed")
	}
	logger := newMockLogger()

	svc := newKMSService(mockMgr, logger)
	require.NotNil(t, svc)

	ctx := systemTenantCtx()
	resp, err := svc.BackendTest(ctx)
	require.NoError(t, err) // Should return response, not error
	require.NotNil(t, resp)

	assert.False(t, resp.Success)
	assert.Contains(t, resp.Message, "encryption failed")
}

func TestKMSService_BackendTest_DecryptionFailure(t *testing.T) {
	mockMgr := newMockKMSManager()
	mockMgr.decryptFn = func(ctx context.Context, keyID string, ciphertext, aad []byte) ([]byte, error) {
		return nil, stderrors.New("decryption failed")
	}
	logger := newMockLogger()

	svc := newKMSService(mockMgr, logger)
	require.NotNil(t, svc)

	ctx := systemTenantCtx()
	resp, err := svc.BackendTest(ctx)
	require.NoError(t, err) // Should return response, not error
	require.NotNil(t, resp)

	assert.False(t, resp.Success)
	assert.Contains(t, resp.Message, "decryption failed")
}

func TestKMSService_SetBackendRegistry(t *testing.T) {
	mockMgr := newMockKMSManager()
	logger := newMockLogger()

	svc := newKMSService(mockMgr, logger)
	require.NotNil(t, svc)

	// Create a real BackendRegistry
	registry := kms.NewBackendRegistry(kms.DefaultRegistryConfig())
	require.NotNil(t, registry)

	svc.SetBackendRegistry(registry)

	// Verify the registry was set by checking it does not panic
	assert.NotPanics(t, func() {
		svc.SetBackendRegistry(registry)
	})
}

func TestKMSService_SetAuditLogger(t *testing.T) {
	mockMgr := newMockKMSManager()
	logger := newMockLogger()

	svc := newKMSService(mockMgr, logger)
	require.NotNil(t, svc)

	mockAudit := &mockKMSAuditLogger{}
	svc.SetAuditLogger(mockAudit)

	// The audit logger is set - we can't verify without calling a method that uses it
	// but at least we verify it doesn't panic
	assert.NotPanics(t, func() {
		svc.SetAuditLogger(mockAudit)
	})
}

func TestWithBackendRegistry(t *testing.T) {
	// Create a real BackendRegistry
	registry := kms.NewBackendRegistry(kms.DefaultRegistryConfig())
	require.NotNil(t, registry)

	opt := WithBackendRegistry(registry)
	assert.NotNil(t, opt)
}

func TestWithKMSAuditLogger(t *testing.T) {
	mockAudit := &mockKMSAuditLogger{}

	opt := WithKMSAuditLogger(mockAudit)
	assert.NotNil(t, opt)
}

func TestWithAuditContext(t *testing.T) {
	ctx := systemTenantCtx()

	ctx = WithAuditContext(ctx, "testuser", "user-123", "192.168.1.1")

	// Verify the context values
	username, userID, ipAddr := extractAuditContext(ctx)
	assert.Equal(t, "testuser", username)
	assert.Equal(t, "user-123", userID)
	assert.Equal(t, "192.168.1.1", ipAddr)
}

func TestExtractAuditContext_Empty(t *testing.T) {
	ctx := systemTenantCtx()

	username, userID, ipAddr := extractAuditContext(ctx)
	assert.Empty(t, username)
	assert.Empty(t, userID)
	assert.Empty(t, ipAddr)
}

func TestExtractAuditContext_NonStringValues(t *testing.T) {
	ctx := context.WithValue(systemTenantCtx(), contextKeyUsername, 42)
	ctx = context.WithValue(ctx, contextKeyUserID, []byte("user"))
	ctx = context.WithValue(ctx, contextKeyIPAddr, struct{}{})

	username, userID, ipAddr := extractAuditContext(ctx)
	assert.Empty(t, username, "a non-string username must be ignored, not panic")
	assert.Empty(t, userID)
	assert.Empty(t, ipAddr)
}

// =============================================================================
// Mock Backend Registry
// =============================================================================

type mockBackendRegistry struct {
	backends []*kms.BackendInfo
	stats    *kms.RegistryStats
	health   map[string]*kms.BackendHealth
}

func (m *mockBackendRegistry) Get(name string) (kms.Backend, error) {
	return nil, stderrors.New("not implemented")
}

func (m *mockBackendRegistry) GetHealthy(ctx context.Context) (kms.Backend, error) {
	return nil, stderrors.New("not implemented")
}

func (m *mockBackendRegistry) GetByAlgorithm(ctx context.Context, algorithm string) (kms.Backend, error) {
	return nil, stderrors.New("not implemented")
}

func (m *mockBackendRegistry) List() []*kms.BackendInfo {
	return m.backends
}

func (m *mockBackendRegistry) GetHealth(name string) (*kms.BackendHealth, error) {
	if m.health == nil {
		return &kms.BackendHealth{Status: "healthy"}, nil
	}
	if h, ok := m.health[name]; ok {
		return h, nil
	}
	return nil, stderrors.New("not found")
}

func (m *mockBackendRegistry) CheckHealth(ctx context.Context, name string) (*kms.BackendHealth, error) {
	return m.GetHealth(name)
}

func (m *mockBackendRegistry) Stats() *kms.RegistryStats {
	if m.stats == nil {
		return &kms.RegistryStats{}
	}
	return m.stats
}

// =============================================================================
// Mock KMS Audit Logger
// =============================================================================

type mockKMSAuditLogger struct {
	exports []string
}

func (m *mockKMSAuditLogger) LogKeyExport(ctx context.Context, username, userID, keyID, algorithm, wrappingKeyID, wrappingAlgorithm, ipAddr string, success bool) error {
	m.exports = append(m.exports, keyID)
	return nil
}

func (m *mockKMSAuditLogger) LogKeyGenerate(ctx context.Context, username, userID, keyID, keyName, algorithm, ipAddr string, success bool) error {
	return nil
}

func (m *mockKMSAuditLogger) LogKeyImport(ctx context.Context, username, userID, keyID, keyName, algorithm, ipAddr string, success bool) error {
	return nil
}

func (m *mockKMSAuditLogger) LogKeyDeletion(ctx context.Context, username, userID, keyID, ipAddr string, success bool) error {
	return nil
}

func (m *mockKMSAuditLogger) LogGracePeriodDisable(ctx context.Context, keyID string, version uint64, jobID string, success bool, errorMsg string) error {
	return nil
}

// =============================================================================
// DEK Operations Tests
// =============================================================================

func TestKMSService_CreateDEK_Success(t *testing.T) {
	mockMgr := newMockKMSManager()
	logger := newMockLogger()

	svc := newKMSService(mockMgr, logger)
	require.NotNil(t, svc)

	ctx := systemTenantCtx()
	req := &CreateDEKRequest{
		Path:           "/data/encryption",
		Algorithm:      KeyAlgAES256GCM,
		Description:    "Data encryption key",
		MaxOperations:  1000000,
		MaxBytes:       1024 * 1024 * 1024, // 1GB
		WarningPercent: 80,
	}

	resp, err := svc.CreateDEK(ctx, req)
	require.NoError(t, err)
	require.NotNil(t, resp)
	require.NotNil(t, resp.DEK)

	assert.Equal(t, "data/encryption", resp.DEK.Path)
	assert.Equal(t, KeyAlgAES256GCM, resp.DEK.Algorithm)
	assert.Equal(t, uint64(1), resp.DEK.Version)
	assert.Equal(t, string(DEKStatusActive), resp.DEK.Status)
	assert.False(t, resp.Timestamp.IsZero())
}

func TestKMSService_CreateDEK_EmptyPath(t *testing.T) {
	mockMgr := newMockKMSManager()
	logger := newMockLogger()

	svc := newKMSService(mockMgr, logger)

	ctx := systemTenantCtx()
	req := &CreateDEKRequest{
		Path:      "",
		Algorithm: KeyAlgAES256GCM,
	}

	resp, err := svc.CreateDEK(ctx, req)
	require.Error(t, err)
	require.Nil(t, resp)
	assert.Contains(t, err.Error(), "path")
}

func TestKMSService_CreateDEK_InvalidAlgorithm(t *testing.T) {
	mockMgr := newMockKMSManager()
	logger := newMockLogger()

	svc := newKMSService(mockMgr, logger)

	ctx := systemTenantCtx()
	req := &CreateDEKRequest{
		Path:      "/data/test",
		Algorithm: "INVALID-ALG",
	}

	resp, err := svc.CreateDEK(ctx, req)
	require.Error(t, err)
	require.Nil(t, resp)
	assert.Contains(t, err.Error(), "algorithm")
}

func TestKMSService_GetDEK_Success(t *testing.T) {
	mockMgr := newMockKMSManager()
	logger := newMockLogger()

	svc := newKMSService(mockMgr, logger)

	ctx := systemTenantCtx()

	// First create the DEK
	createReq := &CreateDEKRequest{
		Path:      "/data/encryption",
		Algorithm: "AES-256-GCM",
	}
	_, createErr := svc.CreateDEK(ctx, createReq)
	require.NoError(t, createErr)

	// Now get it
	req := &GetDEKRequest{
		Path: "/data/encryption",
	}

	resp, err := svc.GetDEK(ctx, req)
	require.NoError(t, err)
	require.NotNil(t, resp)
	require.NotNil(t, resp.DEK)

	assert.Equal(t, "data/encryption", resp.DEK.Path)
	assert.False(t, resp.Timestamp.IsZero())
}

func TestKMSService_GetDEK_EmptyPath(t *testing.T) {
	mockMgr := newMockKMSManager()
	logger := newMockLogger()

	svc := newKMSService(mockMgr, logger)

	ctx := systemTenantCtx()
	req := &GetDEKRequest{
		Path: "",
	}

	resp, err := svc.GetDEK(ctx, req)
	require.Error(t, err)
	require.Nil(t, resp)
}

func TestKMSService_ListDEKs_Success(t *testing.T) {
	mockMgr := newMockKMSManager()
	logger := newMockLogger()

	svc := newKMSService(mockMgr, logger)

	ctx := systemTenantCtx()
	req := &ListDEKsRequest{
		Prefix:    "/data/",
		Recursive: true,
		Limit:     100,
	}

	resp, err := svc.ListDEKs(ctx, req)
	require.NoError(t, err)
	require.NotNil(t, resp)

	assert.NotNil(t, resp.DEKs)
	assert.False(t, resp.Timestamp.IsZero())
}

func TestKMSService_ListDEKs_EmptyPrefix(t *testing.T) {
	mockMgr := newMockKMSManager()
	logger := newMockLogger()

	svc := newKMSService(mockMgr, logger)

	ctx := systemTenantCtx()
	req := &ListDEKsRequest{
		Prefix: "",
		Limit:  50,
	}

	resp, err := svc.ListDEKs(ctx, req)
	require.NoError(t, err)
	require.NotNil(t, resp)
}

func TestKMSService_RotateDEK_Success(t *testing.T) {
	mockMgr := newMockKMSManager()
	logger := newMockLogger()

	svc := newKMSService(mockMgr, logger)

	ctx := systemTenantCtx()

	// First create the DEK
	createReq := &CreateDEKRequest{
		Path:      "/data/encryption",
		Algorithm: "AES-256-GCM",
	}
	_, createErr := svc.CreateDEK(ctx, createReq)
	require.NoError(t, createErr)

	// Now rotate it
	req := &RotateDEKRequest{
		Path:       "/data/encryption",
		Background: true,
	}

	resp, err := svc.RotateDEK(ctx, req)
	require.NoError(t, err)
	require.NotNil(t, resp)

	assert.Equal(t, "data/encryption", resp.Path)
	assert.Equal(t, uint64(1), resp.OldVersion)
	assert.Equal(t, uint64(2), resp.NewVersion)
	assert.False(t, resp.Timestamp.IsZero())
}

func TestKMSService_RotateDEK_EmptyPath(t *testing.T) {
	mockMgr := newMockKMSManager()
	logger := newMockLogger()

	svc := newKMSService(mockMgr, logger)

	ctx := systemTenantCtx()
	req := &RotateDEKRequest{
		Path: "",
	}

	resp, err := svc.RotateDEK(ctx, req)
	require.Error(t, err)
	require.Nil(t, resp)
}

func TestKMSService_DeleteDEK_Success(t *testing.T) {
	mockMgr := newMockKMSManager()
	logger := newMockLogger()

	svc := newKMSService(mockMgr, logger)

	ctx := systemTenantCtx()

	// First create the DEK
	createReq := &CreateDEKRequest{
		Path:      "/data/encryption",
		Algorithm: "AES-256-GCM",
	}
	_, createErr := svc.CreateDEK(ctx, createReq)
	require.NoError(t, createErr)

	// Now delete it
	req := &DeleteDEKRequest{
		Path:  "/data/encryption",
		Force: true,
	}

	resp, err := svc.DeleteDEK(ctx, req)
	require.NoError(t, err)
	require.NotNil(t, resp)

	assert.Equal(t, "data/encryption", resp.Path)
	assert.True(t, resp.Deleted)
	assert.False(t, resp.Timestamp.IsZero())
}

func TestKMSService_DeleteDEK_EmptyPath(t *testing.T) {
	mockMgr := newMockKMSManager()
	logger := newMockLogger()

	svc := newKMSService(mockMgr, logger)

	ctx := systemTenantCtx()
	req := &DeleteDEKRequest{
		Path: "",
	}

	resp, err := svc.DeleteDEK(ctx, req)
	require.Error(t, err)
	require.Nil(t, resp)
}

func TestKMSService_GetDEKStatus_Success(t *testing.T) {
	mockMgr := newMockKMSManager()
	logger := newMockLogger()

	svc := newKMSService(mockMgr, logger)

	ctx := systemTenantCtx()

	// First create the DEK
	createReq := &CreateDEKRequest{
		Path:      "/data/encryption",
		Algorithm: "AES-256-GCM",
	}
	_, createErr := svc.CreateDEK(ctx, createReq)
	require.NoError(t, createErr)

	// Now get status
	req := &GetDEKStatusRequest{
		Path: "/data/encryption",
	}

	resp, err := svc.GetDEKStatus(ctx, req)
	require.NoError(t, err)
	require.NotNil(t, resp)
	require.NotNil(t, resp.DEK)

	assert.Equal(t, "data/encryption", resp.DEK.Path)
	assert.Equal(t, "active", resp.DEK.Status)
	assert.False(t, resp.Timestamp.IsZero())
}

func TestKMSService_GetDEKStatus_EmptyPath(t *testing.T) {
	mockMgr := newMockKMSManager()
	logger := newMockLogger()

	svc := newKMSService(mockMgr, logger)

	ctx := systemTenantCtx()
	req := &GetDEKStatusRequest{
		Path: "",
	}

	resp, err := svc.GetDEKStatus(ctx, req)
	require.Error(t, err)
	require.Nil(t, resp)
}

func TestKMSService_SetDefaultDEK_Success(t *testing.T) {
	mockMgr := newMockKMSManager()
	logger := newMockLogger()

	svc := newKMSService(mockMgr, logger)

	ctx := systemTenantCtx()

	// First create the DEK
	createReq := &CreateDEKRequest{
		Path:      "/data/primary",
		Algorithm: "AES-256-GCM",
	}
	_, createErr := svc.CreateDEK(ctx, createReq)
	require.NoError(t, createErr)

	// Now set it as default
	req := &SetDefaultDEKRequest{
		Path: "/data/primary",
	}

	resp, err := svc.SetDefaultDEK(ctx, req)
	require.NoError(t, err)
	require.NotNil(t, resp)

	assert.Equal(t, "data/primary", resp.Path)
	assert.False(t, resp.Timestamp.IsZero())
}

func TestKMSService_SetDefaultDEK_EmptyPath(t *testing.T) {
	mockMgr := newMockKMSManager()
	logger := newMockLogger()

	svc := newKMSService(mockMgr, logger)

	ctx := systemTenantCtx()
	req := &SetDefaultDEKRequest{
		Path: "",
	}

	resp, err := svc.SetDefaultDEK(ctx, req)
	require.Error(t, err)
	require.Nil(t, resp)
}

// =============================================================================
// Safety Operations Tests
// =============================================================================

func TestKMSService_SafetyCheck_Success(t *testing.T) {
	mockMgr := newMockKMSManager()
	logger := newMockLogger()

	svc := newKMSService(mockMgr, logger)

	ctx := systemTenantCtx()
	req := &SafetyCheckRequest{
		ThresholdPercent: 80.0,
	}

	resp, err := svc.SafetyCheck(ctx, req)
	require.NoError(t, err)
	require.NotNil(t, resp)

	assert.NotNil(t, resp.Items)
	assert.False(t, resp.Timestamp.IsZero())
}

func TestKMSService_SafetyReport_Success(t *testing.T) {
	mockMgr := newMockKMSManager()
	logger := newMockLogger()

	svc := newKMSService(mockMgr, logger)

	ctx := systemTenantCtx()
	req := &SafetyReportRequest{}

	resp, err := svc.SafetyReport(ctx, req)
	require.NoError(t, err)
	require.NotNil(t, resp)

	assert.NotNil(t, resp.Items)
	assert.False(t, resp.Timestamp.IsZero())
}

func TestKMSService_GetSafetyTracker(t *testing.T) {
	mockMgr := newMockKMSManager()
	logger := newMockLogger()

	svc := newKMSService(mockMgr, logger)

	tracker := svc.GetSafetyTracker()
	// The tracker should not be nil (it's initialized in newKMSService)
	assert.NotNil(t, tracker)
}

func TestKMSService_SetDEKSafety_Success(t *testing.T) {
	mockMgr := newMockKMSManager()
	logger := newMockLogger()

	svc := newKMSService(mockMgr, logger)

	ctx := systemTenantCtx()

	// First create the DEK
	createReq := &CreateDEKRequest{
		Path:      "/data/encryption",
		Algorithm: KeyAlgAES256GCM,
	}
	_, err := svc.CreateDEK(ctx, createReq)
	require.NoError(t, err)

	// Now set the safety limits
	req := &SetDEKSafetyRequest{
		Path:           "/data/encryption",
		MaxOperations:  1000000,
		MaxBytes:       1024 * 1024 * 1024,
		WarningPercent: 80,
	}

	resp, err := svc.SetDEKSafety(ctx, req)
	require.NoError(t, err)
	require.NotNil(t, resp)

	assert.NotEmpty(t, resp.Path)
	assert.False(t, resp.Timestamp.IsZero())
}

func TestKMSService_SetDEKSafety_EmptyPath(t *testing.T) {
	mockMgr := newMockKMSManager()
	logger := newMockLogger()

	svc := newKMSService(mockMgr, logger)

	ctx := systemTenantCtx()
	req := &SetDEKSafetyRequest{
		Path: "",
	}

	resp, err := svc.SetDEKSafety(ctx, req)
	require.Error(t, err)
	require.Nil(t, resp)
}

func TestKMSService_ClearDEKSafety_Success(t *testing.T) {
	mockMgr := newMockKMSManager()
	logger := newMockLogger()

	svc := newKMSService(mockMgr, logger)

	ctx := systemTenantCtx()

	// First create the DEK
	createReq := &CreateDEKRequest{
		Path:      "/data/encryption",
		Algorithm: KeyAlgAES256GCM,
	}
	_, err := svc.CreateDEK(ctx, createReq)
	require.NoError(t, err)

	// Now clear the safety limits
	req := &ClearDEKSafetyRequest{
		Path: "/data/encryption",
	}

	resp, err := svc.ClearDEKSafety(ctx, req)
	require.NoError(t, err)
	require.NotNil(t, resp)

	assert.NotEmpty(t, resp.Path)
	assert.False(t, resp.Timestamp.IsZero())
}

func TestKMSService_ClearDEKSafety_EmptyPath(t *testing.T) {
	mockMgr := newMockKMSManager()
	logger := newMockLogger()

	svc := newKMSService(mockMgr, logger)

	ctx := systemTenantCtx()
	req := &ClearDEKSafetyRequest{
		Path: "",
	}

	resp, err := svc.ClearDEKSafety(ctx, req)
	require.Error(t, err)
	require.Nil(t, resp)
}

// =============================================================================
// Key Status and Versions Tests
// =============================================================================

func TestKMSService_GetSupportedAlgorithms(t *testing.T) {
	mockMgr := newMockKMSManager()
	logger := newMockLogger()

	svc := newKMSService(mockMgr, logger)

	ctx := systemTenantCtx()

	algorithms, err := svc.GetSupportedAlgorithms(ctx)
	require.NoError(t, err)
	require.NotNil(t, algorithms)

	assert.NotEmpty(t, algorithms)
	assert.Contains(t, algorithms, string(KeyAlgAES256GCM))
}

func TestKMSService_ListKeyVersions_KeyNotFound(t *testing.T) {
	mockMgr := newMockKMSManager()
	logger := newMockLogger()

	svc := newKMSService(mockMgr, logger)

	ctx := systemTenantCtx()
	req := &ListKeyVersionsRequest{
		KeyID: "non-existent-key",
		Limit: 10,
	}

	resp, err := svc.ListKeyVersions(ctx, req)
	require.Error(t, err)
	require.Nil(t, resp)
}

func TestKMSService_UpdateKeyStatus_KeyNotFound(t *testing.T) {
	mockMgr := newMockKMSManager()
	logger := newMockLogger()

	svc := newKMSService(mockMgr, logger)

	ctx := systemTenantCtx()
	req := &UpdateKeyStatusRequest{
		KeyID:  "non-existent-key",
		Status: KeyStatusEnabled,
	}

	resp, err := svc.UpdateKeyStatus(ctx, req)
	require.Error(t, err)
	require.Nil(t, resp)
}

// =============================================================================
// Key Safety Operations Tests
// =============================================================================

func TestKMSService_SetKeySafety_Success(t *testing.T) {
	mockMgr := newMockKMSManager()
	logger := newMockLogger()

	// Pre-create the key so SetKeySafety can find it
	mockMgr.keys["test-key-123"] = &mockKeyData{
		keyID:     "test-key-123",
		algorithm: "AES-256-GCM",
		version:   1,
		status:    "enabled",
		createdAt: time.Now(),
	}

	svc := newKMSService(mockMgr, logger)

	ctx := systemTenantCtx()
	req := &SetKeySafetyRequest{
		KeyID:          "test-key-123",
		MaxOperations:  1000000,
		MaxBytes:       1024 * 1024 * 1024,
		WarningPercent: 80,
	}

	resp, err := svc.SetKeySafety(ctx, req)
	require.NoError(t, err)
	require.NotNil(t, resp)

	assert.Equal(t, "test-key-123", resp.KeyID)
	assert.False(t, resp.Timestamp.IsZero())
}

func TestKMSService_SetKeySafety_KeyNotFound(t *testing.T) {
	mockMgr := newMockKMSManager()
	logger := newMockLogger()

	svc := newKMSService(mockMgr, logger)

	ctx := systemTenantCtx()
	req := &SetKeySafetyRequest{
		KeyID:         "non-existent-key",
		MaxOperations: 1000000,
	}

	resp, err := svc.SetKeySafety(ctx, req)
	require.Error(t, err)
	require.Nil(t, resp)

	// Verify it's a not found error
	var qbErr *quicbaasErrors.QBError
	require.True(t, errors.As(err, &qbErr))
	assert.Equal(t, quicbaasErrors.ErrNotFound, qbErr.Code)
}

func TestKMSService_SetKeySafety_EmptyKeyID(t *testing.T) {
	mockMgr := newMockKMSManager()
	logger := newMockLogger()

	svc := newKMSService(mockMgr, logger)

	ctx := systemTenantCtx()
	req := &SetKeySafetyRequest{
		KeyID: "",
	}

	resp, err := svc.SetKeySafety(ctx, req)
	require.Error(t, err)
	require.Nil(t, resp)
}

func TestKMSService_ClearKeySafety_Success(t *testing.T) {
	mockMgr := newMockKMSManager()
	logger := newMockLogger()

	// Pre-create the key so ClearKeySafety can find it
	mockMgr.keys["test-key-123"] = &mockKeyData{
		keyID:     "test-key-123",
		algorithm: "AES-256-GCM",
		version:   1,
		status:    "enabled",
		createdAt: time.Now(),
	}

	svc := newKMSService(mockMgr, logger)

	ctx := systemTenantCtx()
	req := &ClearKeySafetyRequest{
		KeyID: "test-key-123",
	}

	resp, err := svc.ClearKeySafety(ctx, req)
	require.NoError(t, err)
	require.NotNil(t, resp)

	assert.Equal(t, "test-key-123", resp.KeyID)
	assert.False(t, resp.Timestamp.IsZero())
}

func TestKMSService_ClearKeySafety_KeyNotFound(t *testing.T) {
	mockMgr := newMockKMSManager()
	logger := newMockLogger()

	svc := newKMSService(mockMgr, logger)

	ctx := systemTenantCtx()
	req := &ClearKeySafetyRequest{
		KeyID: "non-existent-key",
	}

	resp, err := svc.ClearKeySafety(ctx, req)
	require.Error(t, err)
	require.Nil(t, resp)

	// Verify it's a not found error
	var qbErr *quicbaasErrors.QBError
	require.True(t, errors.As(err, &qbErr))
	assert.Equal(t, quicbaasErrors.ErrNotFound, qbErr.Code)
}

func TestKMSService_ClearKeySafety_EmptyKeyID(t *testing.T) {
	mockMgr := newMockKMSManager()
	logger := newMockLogger()

	svc := newKMSService(mockMgr, logger)

	ctx := systemTenantCtx()
	req := &ClearKeySafetyRequest{
		KeyID: "",
	}

	resp, err := svc.ClearKeySafety(ctx, req)
	require.Error(t, err)
	require.Nil(t, resp)
}

// =============================================================================
// Re-encryption Operations Tests
// =============================================================================

func TestKMSService_StartReencryption_Success(t *testing.T) {
	mockMgr := newMockKMSManager()
	logger := newMockLogger()

	svc := newKMSService(mockMgr, logger)

	ctx := systemTenantCtx()
	req := &StartReencryptionRequest{
		FromVersion: 1,
		ToVersion:   2,
		BatchSize:   100,
		Concurrency: 4,
	}

	resp, err := svc.StartReencryption(ctx, req)
	require.NoError(t, err)
	require.NotNil(t, resp)

	assert.NotEmpty(t, resp.OperationID)
	assert.False(t, resp.StartedAt.IsZero())
}

func TestKMSService_GetReencryptionProgress_Success(t *testing.T) {
	mockMgr := newMockKMSManager()
	logger := newMockLogger()

	svc := newKMSService(mockMgr, logger)

	ctx := systemTenantCtx()
	req := &GetReencryptionProgressRequest{
		Verbose: true,
	}

	resp, err := svc.GetReencryptionProgress(ctx, req)
	require.NoError(t, err)
	require.NotNil(t, resp)

	assert.NotNil(t, resp.Progress)
	assert.False(t, resp.Timestamp.IsZero())
}

// =============================================================================
// Helper Function Tests
// =============================================================================

func TestNormalizeDEKPath(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{"no leading slash", "data/encryption", "data/encryption"},
		{"single leading slash", "/data/encryption", "data/encryption"},
		{"double leading slash", "//data/encryption", "data/encryption"},
		{"trailing slash already", "/data/encryption/", "data/encryption"},
		{"multiple segments", "/a/b/c/d", "a/b/c/d"},
		{"empty", "", ""},
		{"root path", "/", ""},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := normalizeDEKPath(tc.input)
			assert.Equal(t, tc.expected, result)
		})
	}
}

func TestIsValidWrappingAlgorithm(t *testing.T) {
	tests := []struct {
		name      string
		algorithm string
		expected  bool
	}{
		{"AES-KW", "AES-KW", true},
		{"AES-KWP", "AES-KWP", true},
		{"AES-GCM", "AES-GCM", true},
		{"RSA-OAEP-SHA256", "RSA-OAEP-SHA256", true},
		{"RSA-OAEP-SHA384", "RSA-OAEP-SHA384", true},
		{"RSA-OAEP-SHA512", "RSA-OAEP-SHA512", true},
		{"ECDH-ES+A128KW", "ECDH-ES+A128KW", true},
		{"ECDH-ES+A192KW", "ECDH-ES+A192KW", true},
		{"ECDH-ES+A256KW", "ECDH-ES+A256KW", true},
		{"invalid", "INVALID", false},
		{"empty", "", false},
		{"ECDH-ES without KW", "ECDH-ES", false},
		{"ECDH-ES with hyphen instead of plus", "ECDH-ES-A256KW", false},
		{"wrong RSA-OAEP format", "RSA-OAEP-256", false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := isValidWrappingAlgorithm(tc.algorithm)
			assert.Equal(t, tc.expected, result)
		})
	}
}

// =============================================================================
// ExportKey Tests
// =============================================================================

func TestKMSService_ExportKey_EmptyKeyID(t *testing.T) {
	mockMgr := newMockKMSManager()
	logger := &mockLogger{}
	svc := newKMSService(mockMgr, logger)
	require.NotNil(t, svc)

	ctx := systemTenantCtx()
	resp, err := svc.ExportKey(ctx, &ExportKeyRequest{
		KeyID:             "",
		WrappingKeyID:     "wrapper-key",
		WrappingAlgorithm: "AES-KW",
	})

	assert.Nil(t, resp)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "key_id is required")
}

func TestKMSService_ExportKey_EmptyWrappingKeyID(t *testing.T) {
	mockMgr := newMockKMSManager()
	logger := &mockLogger{}
	svc := newKMSService(mockMgr, logger)
	require.NotNil(t, svc)

	ctx := systemTenantCtx()
	resp, err := svc.ExportKey(ctx, &ExportKeyRequest{
		KeyID:             "test-key",
		WrappingKeyID:     "",
		WrappingAlgorithm: "AES-KW",
	})

	assert.Nil(t, resp)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "wrapping_key_id is required")
}

func TestKMSService_ExportKey_EmptyWrappingAlgorithm(t *testing.T) {
	mockMgr := newMockKMSManager()
	logger := &mockLogger{}
	svc := newKMSService(mockMgr, logger)
	require.NotNil(t, svc)

	ctx := systemTenantCtx()
	resp, err := svc.ExportKey(ctx, &ExportKeyRequest{
		KeyID:             "test-key",
		WrappingKeyID:     "wrapper-key",
		WrappingAlgorithm: "",
	})

	assert.Nil(t, resp)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "wrapping_algorithm is required")
}

func TestKMSService_ExportKey_InvalidWrappingAlgorithm(t *testing.T) {
	mockMgr := newMockKMSManager()
	logger := &mockLogger{}
	svc := newKMSService(mockMgr, logger)
	require.NotNil(t, svc)

	ctx := systemTenantCtx()
	resp, err := svc.ExportKey(ctx, &ExportKeyRequest{
		KeyID:             "test-key",
		WrappingKeyID:     "wrapper-key",
		WrappingAlgorithm: "INVALID-ALGO",
	})

	assert.Nil(t, resp)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "unsupported wrapping algorithm")
}

// Note: ExportKey tests that go beyond validation require proper metrics initialization
// These tests cover the validation paths which are the primary coverage targets

// =============================================================================
// UnwrapKey Tests
// =============================================================================

func TestKMSService_UnwrapKey_EmptyKeyID(t *testing.T) {
	mockMgr := newMockKMSManager()
	logger := &mockLogger{}
	svc := newKMSService(mockMgr, logger)
	require.NotNil(t, svc)

	ctx := systemTenantCtx()
	resp, err := svc.UnwrapKey(ctx, &UnwrapKeyRequest{
		KeyID:             "",
		WrappedKey:        []byte("wrapped-data"),
		WrappingKeyID:     "wrapper-key",
		WrappingAlgorithm: "AES-KW",
		Algorithm:         "AES-256-GCM",
	})

	assert.Nil(t, resp)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "key_id is required")
}

func TestKMSService_UnwrapKey_EmptyWrappedKey(t *testing.T) {
	mockMgr := newMockKMSManager()
	logger := &mockLogger{}
	svc := newKMSService(mockMgr, logger)
	require.NotNil(t, svc)

	ctx := systemTenantCtx()
	resp, err := svc.UnwrapKey(ctx, &UnwrapKeyRequest{
		KeyID:             "test-key",
		WrappedKey:        []byte{},
		WrappingKeyID:     "wrapper-key",
		WrappingAlgorithm: "AES-KW",
		Algorithm:         "AES-256-GCM",
	})

	assert.Nil(t, resp)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "wrapped_key is required")
}

func TestKMSService_UnwrapKey_EmptyWrappingKeyID(t *testing.T) {
	mockMgr := newMockKMSManager()
	logger := &mockLogger{}
	svc := newKMSService(mockMgr, logger)
	require.NotNil(t, svc)

	ctx := systemTenantCtx()
	resp, err := svc.UnwrapKey(ctx, &UnwrapKeyRequest{
		KeyID:             "test-key",
		WrappedKey:        []byte("wrapped-data"),
		WrappingKeyID:     "",
		WrappingAlgorithm: "AES-KW",
		Algorithm:         "AES-256-GCM",
	})

	assert.Nil(t, resp)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "wrapping_key_id is required")
}

func TestKMSService_UnwrapKey_EmptyWrappingAlgorithm(t *testing.T) {
	mockMgr := newMockKMSManager()
	logger := &mockLogger{}
	svc := newKMSService(mockMgr, logger)
	require.NotNil(t, svc)

	ctx := systemTenantCtx()
	resp, err := svc.UnwrapKey(ctx, &UnwrapKeyRequest{
		KeyID:             "test-key",
		WrappedKey:        []byte("wrapped-data"),
		WrappingKeyID:     "wrapper-key",
		WrappingAlgorithm: "",
		Algorithm:         "AES-256-GCM",
	})

	assert.Nil(t, resp)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "wrapping_algorithm is required")
}

func TestKMSService_UnwrapKey_EmptyAlgorithm(t *testing.T) {
	mockMgr := newMockKMSManager()
	logger := &mockLogger{}
	svc := newKMSService(mockMgr, logger)
	require.NotNil(t, svc)

	ctx := systemTenantCtx()
	resp, err := svc.UnwrapKey(ctx, &UnwrapKeyRequest{
		KeyID:             "test-key",
		WrappedKey:        []byte("wrapped-data"),
		WrappingKeyID:     "wrapper-key",
		WrappingAlgorithm: "AES-KW",
		Algorithm:         "",
	})

	assert.Nil(t, resp)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "algorithm is required")
}

func TestKMSService_UnwrapKey_InvalidWrappingAlgorithm(t *testing.T) {
	mockMgr := newMockKMSManager()
	logger := &mockLogger{}
	svc := newKMSService(mockMgr, logger)
	require.NotNil(t, svc)

	ctx := systemTenantCtx()
	resp, err := svc.UnwrapKey(ctx, &UnwrapKeyRequest{
		KeyID:             "test-key",
		WrappedKey:        []byte("wrapped-data"),
		WrappingKeyID:     "wrapper-key",
		WrappingAlgorithm: "INVALID-ALGO",
		Algorithm:         "AES-256-GCM",
	})

	assert.Nil(t, resp)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "unsupported wrapping algorithm")
}

// =============================================================================
// algorithmToKeyAttributes Tests
// =============================================================================

func TestKMSService_AlgorithmToKeyAttributes(t *testing.T) {
	mockMgr := newMockKMSManager()
	logger := &mockLogger{}
	svc := newKMSService(mockMgr, logger)
	require.NotNil(t, svc)

	tests := []struct {
		name       string
		keyID      string
		algorithm  KeyAlgorithm
		exportable bool
	}{
		{"AES-256-GCM", "key1", "AES-256-GCM", true},
		{"AES256-GCM", "key2", "AES256-GCM", false},
		{"AES-256", "key3", "AES-256", true},
		{"AES-192-GCM", "key4", "AES-192-GCM", false},
		{"AES-128-GCM", "key5", "AES-128-GCM", true},
		{"XCHACHA20-POLY1305", "key6", "XCHACHA20-POLY1305", false},
		{"CHACHA20-POLY1305", "key7", "CHACHA20-POLY1305", true},
		{"RSA-2048", "key8", "RSA-2048", false},
		{"RSA-3072", "key9", "RSA-3072", true},
		{"RSA-4096", "key10", "RSA-4096", false},
		{"ECDSA-P256", "key11", "ECDSA-P256", true},
		{"ECDSA-P384", "key12", "ECDSA-P384", false},
		{"ECDSA-P521", "key13", "ECDSA-P521", true},
		{"ED25519", "key14", "ED25519", false},
		{"Unknown", "key15", "UNKNOWN-ALG", true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			attrs := svc.algorithmToKeyAttributes(tc.keyID, tc.algorithm, tc.exportable)
			assert.NotNil(t, attrs)
			assert.Equal(t, tc.keyID, attrs.CN)
			assert.Equal(t, tc.exportable, attrs.Exportable)
		})
	}
}

// =============================================================================
// Validation Tests for improved coverage
// =============================================================================

func TestKMSService_ValidateGenerateKeyRequest_MissingUsage(t *testing.T) {
	mockMgr := newMockKMSManager()
	logger := &mockLogger{}
	svc := newKMSService(mockMgr, logger)
	require.NotNil(t, svc)

	ctx := systemTenantCtx()
	resp, err := svc.GenerateKey(ctx, &GenerateKeyRequest{
		Name: "test-key",
		KeyAttributes: &xkmssdk.KeyAttributes{
			SymmetricAlgorithm: xkmssdk.SymmetricAES256GCM,
		},
		Usage: []KeyUsage{}, // Empty usage - should fail,
	})

	assert.Nil(t, resp)
	assert.Error(t, err)
}

func TestKMSService_ValidateListDEKsRequest_EmptyLimit(t *testing.T) {
	mockMgr := newMockKMSManager()
	logger := &mockLogger{}
	svc := newKMSService(mockMgr, logger)
	require.NotNil(t, svc)

	ctx := systemTenantCtx()
	resp, err := svc.ListDEKs(ctx, &ListDEKsRequest{
		Prefix: "/data/",
		Limit:  0, // Zero limit should use default
	})

	// Should succeed with default limit
	assert.NotNil(t, resp)
	assert.NoError(t, err)
}

func TestKMSService_ValidateDeleteDEKRequest_Force(t *testing.T) {
	mockMgr := newMockKMSManager()
	logger := &mockLogger{}
	svc := newKMSService(mockMgr, logger)
	require.NotNil(t, svc)

	// First create a DEK
	ctx := systemTenantCtx()
	_, err := svc.CreateDEK(ctx, &CreateDEKRequest{
		Path:      "/data/test",
		Algorithm: "AES-256-GCM",
	})
	require.NoError(t, err)

	// Delete with force flag
	resp, err := svc.DeleteDEK(ctx, &DeleteDEKRequest{
		Path:  "/data/test",
		Force: true,
	})

	assert.NotNil(t, resp)
	assert.NoError(t, err)
}

func TestKMSService_SafetyCheck_ThresholdValidation(t *testing.T) {
	mockMgr := newMockKMSManager()
	logger := &mockLogger{}
	svc := newKMSService(mockMgr, logger)
	require.NotNil(t, svc)

	ctx := systemTenantCtx()
	resp, err := svc.SafetyCheck(ctx, &SafetyCheckRequest{
		ThresholdPercent: 150, // Invalid threshold over 100%
	})

	// Should still work (threshold is just a filter)
	assert.NotNil(t, resp)
	assert.NoError(t, err)
}

func TestKMSService_WithKMSAuditLogger_Nil(t *testing.T) {
	mockMgr := newMockKMSManager()
	logger := &mockLogger{}
	svc := newKMSService(mockMgr, logger)
	require.NotNil(t, svc)

	// Apply nil audit logger option
	opt := WithKMSAuditLogger(nil)
	opt(svc)

	// Verify no panic and audit logger is nil
	assert.Nil(t, svc.auditLogger)
}

func TestKMSService_WithBackendRegistry_Nil(t *testing.T) {
	mockMgr := newMockKMSManager()
	logger := &mockLogger{}
	svc := newKMSService(mockMgr, logger)
	require.NotNil(t, svc)

	// Apply nil backend registry option
	opt := WithBackendRegistry(nil)
	opt(svc)

	// Verify no panic and registry is nil
	assert.Nil(t, svc.backendRegistry)
}

func TestKMSService_GetSupportedAlgorithms_AllTypes(t *testing.T) {
	mockMgr := newMockKMSManager()
	logger := &mockLogger{}
	svc := newKMSService(mockMgr, logger)
	require.NotNil(t, svc)

	ctx := systemTenantCtx()
	algorithms, err := svc.GetSupportedAlgorithms(ctx)
	require.NoError(t, err)
	assert.NotEmpty(t, algorithms)

	// Verify key algorithm types are present
	found := make(map[string]bool)
	for _, alg := range algorithms {
		found[alg] = true
	}

	// Check for major algorithm families
	assert.True(t, found["AES-256-GCM"] || found["AES256-GCM"], "Should have AES-256")
	assert.True(t, found["RSA-2048"] || found["RSA-4096"], "Should have RSA")
}

// TestKMSService_ListKeyVersions_EmptyKeyID is skipped because
// the WrapError function returns nil when the input error is nil.
// The implementation should use NewError for validation errors.

// Note: Many validation tests that use WrapError(nil, ...) are skipped because
// WrapError returns nil when the input error is nil. The implementation should
// use NewError for validation errors.

func TestKMSService_ListKeys_WithPagination(t *testing.T) {
	mockMgr := newMockKMSManager()
	// Create some keys
	for i := 0; i < 5; i++ {
		mockMgr.keys[fmt.Sprintf("key-%d", i)] = &mockKeyData{
			keyID:     fmt.Sprintf("key-%d", i),
			algorithm: "AES-256-GCM",
			version:   1,
			status:    "enabled",
			createdAt: time.Now(),
		}
	}
	logger := &mockLogger{}
	svc := newKMSService(mockMgr, logger)
	require.NotNil(t, svc)

	ctx := systemTenantCtx()
	resp, err := svc.ListKeys(ctx, &ListKeysRequest{
		Limit:  2,
		Cursor: "",
	})

	assert.NotNil(t, resp)
	assert.NoError(t, err)
	assert.GreaterOrEqual(t, len(resp.Keys), 1)
}

func TestKMSService_UpdateKeyMetadata_KeyNotFound(t *testing.T) {
	mockMgr := newMockKMSManager()
	logger := newMockLogger()
	svc := newKMSService(mockMgr, logger)

	newName := "updated-name"
	resp, err := svc.UpdateKeyMetadata(systemTenantCtx(), &UpdateKeyMetadataRequest{
		KeyID: "nonexistent-key",
		Name:  &newName,
	})
	require.Error(t, err)
	assert.Nil(t, resp)
	assert.Contains(t, err.Error(), "key not found")
}

func TestKMSService_SetKeyPolicy_KeyNotFound(t *testing.T) {
	mockMgr := newMockKMSManager()
	logger := newMockLogger()
	svc := newKMSService(mockMgr, logger)

	resp, err := svc.SetKeyPolicy(systemTenantCtx(), &SetKeyPolicyRequest{
		KeyID: "nonexistent-key",
		Policy: &KeyPolicy{
			Version:           1,
			AllowedOperations: []KeyUsage{KeyUsageEncrypt, KeyUsageDecrypt},
		},
	})
	require.Error(t, err)
	assert.Nil(t, resp)
	assert.Contains(t, err.Error(), "key not found")
}

func TestKMSService_ListKeyVersions_WithMultipleVersionsPaginated(t *testing.T) {
	mockMgr := newMockKMSManager()
	mockMgr.keys["versioned-key"] = &mockKeyData{
		keyID:     "versioned-key",
		algorithm: "AES-256-GCM",
		version:   5,
		status:    "enabled",
		createdAt: time.Now(),
	}
	logger := newMockLogger()
	svc := newKMSService(mockMgr, logger)

	resp, err := svc.ListKeyVersions(systemTenantCtx(), &ListKeyVersionsRequest{
		KeyID: "versioned-key",
		Limit: 2,
	})
	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.Len(t, resp.Versions, 2)
	assert.True(t, resp.HasMore)
	assert.NotEmpty(t, resp.NextCursor)
}

func TestKMSService_CreateDEK_AlreadyExists(t *testing.T) {
	mockMgr := newMockKMSManager()
	logger := newMockLogger()
	svc := newKMSService(mockMgr, logger)

	_, err := svc.CreateDEK(systemTenantCtx(), &CreateDEKRequest{
		Path:      "transit/dek/test",
		Algorithm: KeyAlgAES256GCM,
	})
	require.NoError(t, err)

	resp, err := svc.CreateDEK(systemTenantCtx(), &CreateDEKRequest{
		Path:      "transit/dek/test",
		Algorithm: KeyAlgAES256GCM,
	})
	require.Error(t, err)
	assert.Nil(t, resp)
	assert.Contains(t, err.Error(), "already exists")
}

func TestKMSService_CreateDEK_DefaultAlgorithm(t *testing.T) {
	mockMgr := newMockKMSManager()
	logger := newMockLogger()
	svc := newKMSService(mockMgr, logger)

	resp, err := svc.CreateDEK(systemTenantCtx(), &CreateDEKRequest{
		Path: "transit/dek/default-algo",
	})
	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.Equal(t, KeyAlgAES256GCM, resp.DEK.Algorithm)
}

func TestKMSService_RotateDEK_DEKNotFound(t *testing.T) {
	mockMgr := newMockKMSManager()
	logger := newMockLogger()
	svc := newKMSService(mockMgr, logger)

	resp, err := svc.RotateDEK(systemTenantCtx(), &RotateDEKRequest{
		Path: "transit/dek/nonexistent",
	})
	require.Error(t, err)
	assert.Nil(t, resp)
	assert.Contains(t, err.Error(), "DEK not found")
}

func TestKMSService_DeleteDEK_DEKNotFound(t *testing.T) {
	mockMgr := newMockKMSManager()
	logger := newMockLogger()
	svc := newKMSService(mockMgr, logger)

	resp, err := svc.DeleteDEK(systemTenantCtx(), &DeleteDEKRequest{
		Path: "transit/dek/nonexistent",
	})
	require.Error(t, err)
	assert.Nil(t, resp)
	assert.Contains(t, err.Error(), "DEK not found")
}

func TestKMSService_DeleteDEK_DefaultWithoutForce(t *testing.T) {
	mockMgr := newMockKMSManager()
	logger := newMockLogger()
	svc := newKMSService(mockMgr, logger)

	_, err := svc.CreateDEK(systemTenantCtx(), &CreateDEKRequest{
		Path:      "transit/dek/default-key",
		Algorithm: KeyAlgAES256GCM,
	})
	require.NoError(t, err)

	_, err = svc.SetDefaultDEK(systemTenantCtx(), &SetDefaultDEKRequest{
		Path: "transit/dek/default-key",
	})
	require.NoError(t, err)

	resp, err := svc.DeleteDEK(systemTenantCtx(), &DeleteDEKRequest{
		Path:  "transit/dek/default-key",
		Force: false,
	})
	require.Error(t, err)
	assert.Nil(t, resp)
	assert.Contains(t, err.Error(), "cannot delete default DEK")
}

func TestKMSService_BackendTest_NilManagerPath(t *testing.T) {
	svc := &kmsServiceImpl{
		reencryptionState: &reencryptionState{status: "idle"},
		dekStore:          newDEKStore(),
		safetyTracker:     NewAEADSafetyTracker(),
	}

	resp, err := svc.BackendTest(systemTenantCtx())
	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.False(t, resp.Success)
	assert.Contains(t, resp.Message, "not initialized")
}

func TestKMSService_BackendTest_UnavailableBackend(t *testing.T) {
	mockMgr := &unavailableMockKMSManager{mockKMSManager: *newMockKMSManager()}
	logger := newMockLogger()
	svc := newKMSService(mockMgr, logger)

	resp, err := svc.BackendTest(systemTenantCtx())
	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.False(t, resp.Success)
	assert.Contains(t, resp.Message, "not available")
}

func TestKMSService_GetPublicKey_GetKeyMetadataError(t *testing.T) {
	mockMgr := newMockKMSManager()
	// Add key so GetPublicKey (mock) succeeds
	mockMgr.keys["asymmetric-key"] = &mockKeyData{
		keyID:     "asymmetric-key",
		algorithm: "ECDSA-P256",
		version:   1,
		status:    "enabled",
		createdAt: time.Now(),
	}
	// Override GetKey to fail for metadata retrieval
	mockMgr.getKeyFn = func(ctx context.Context, keyID string) (interface{}, error) {
		return nil, stderrors.New("key metadata retrieval failed")
	}
	logger := newMockLogger()
	svc := newKMSService(mockMgr, logger)

	resp, err := svc.GetPublicKey(systemTenantCtx(), &GetPublicKeyRequest{
		KeyID: "asymmetric-key",
	})
	require.Error(t, err)
	assert.Nil(t, resp)
	assert.Contains(t, err.Error(), "key not found")
}

func TestKMSService_GetPublicKey_ServiceKeyMetadataType(t *testing.T) {
	mockMgr := newMockKMSManager()
	// Add key so GetPublicKey (mock) succeeds
	mockMgr.keys["svc-meta-key"] = &mockKeyData{
		keyID:     "svc-meta-key",
		algorithm: "ECDSA-P256",
		version:   3,
		status:    "enabled",
		createdAt: time.Now(),
	}
	// Override GetKey to return service.KeyMetadata instead of kms.KeyMetadata
	mockMgr.getKeyFn = func(ctx context.Context, keyID string) (interface{}, error) {
		return &KeyMetadata{
			KeyID:     keyID,
			Algorithm: KeyAlgECDSAP256,
			Version:   3,
			Status:    KeyStatusEnabled,
			CreatedAt: time.Now(),
		}, nil
	}
	logger := newMockLogger()
	svc := newKMSService(mockMgr, logger)

	resp, err := svc.GetPublicKey(systemTenantCtx(), &GetPublicKeyRequest{
		KeyID: "svc-meta-key",
	})
	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.Equal(t, KeyAlgECDSAP256, resp.Algorithm)
	assert.Equal(t, uint64(3), resp.Version)
}

func TestKMSService_BackendStatus_UnavailableBackendPath(t *testing.T) {
	mockMgr := &unavailableMockKMSManager{mockKMSManager: *newMockKMSManager()}
	logger := newMockLogger()
	svc := newKMSService(mockMgr, logger)

	resp, err := svc.BackendStatus(systemTenantCtx())
	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.Equal(t, "unavailable", resp.Status)
}

func TestKMSService_SafetyCheck_WithDEKResults(t *testing.T) {
	mockMgr := newMockKMSManager()
	logger := newMockLogger()
	svc := newKMSService(mockMgr, logger)

	_, err := svc.CreateDEK(systemTenantCtx(), &CreateDEKRequest{
		Path:      "transit/dek/heavy-use",
		Algorithm: KeyAlgAES256GCM,
	})
	require.NoError(t, err)

	svc.safetyTracker.SetDEKConfig("transit/dek/heavy-use", AEADSafetyConfig{
		MaxOperations:  100,
		MaxBytes:       10000,
		WarningPercent: 50,
	})

	for i := 0; i < 90; i++ {
		_ = svc.safetyTracker.RecordDEKEncrypt(systemTenantCtx(), "transit/dek/heavy-use", 100)
	}

	resp, err := svc.SafetyCheck(systemTenantCtx(), &SafetyCheckRequest{
		ThresholdPercent: 50,
	})
	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.Greater(t, len(resp.Items), 0)
}

func TestKMSService_GetDEKStatus_NilKMSManager(t *testing.T) {
	svc := &kmsServiceImpl{
		reencryptionState: &reencryptionState{status: "idle"},
		dekStore:          newDEKStore(),
		safetyTracker:     NewAEADSafetyTracker(),
	}

	svc.dekStore.set("transit/dek/no-kms", &dekEntry{
		metadata: &DEKMetadata{
			Path:      "transit/dek/no-kms",
			Algorithm: KeyAlgAES256GCM,
			Version:   1,
			Status:    "enabled",
			CreatedAt: time.Now(),
		},
		wrappedKey: []byte("test-key-material"),
	})

	resp, err := svc.GetDEKStatus(systemTenantCtx(), &GetDEKStatusRequest{
		Path: "transit/dek/no-kms",
	})
	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.Empty(t, resp.DEK.BackendType)
}

func TestKMSService_ListKeyVersions_ServiceKeyMetadataType(t *testing.T) {
	mockMgr := newMockKMSManager()
	mockMgr.getKeyFn = func(ctx context.Context, keyID string) (interface{}, error) {
		return &KeyMetadata{
			KeyID:     keyID,
			Algorithm: KeyAlgAES256GCM,
			Version:   3,
			Status:    KeyStatusEnabled,
			CreatedAt: time.Now(),
		}, nil
	}
	logger := newMockLogger()
	svc := newKMSService(mockMgr, logger)

	resp, err := svc.ListKeyVersions(systemTenantCtx(), &ListKeyVersionsRequest{
		KeyID: "svc-versioned-key",
	})
	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.Len(t, resp.Versions, 3)
	assert.Equal(t, KeyAlgAES256GCM, resp.Versions[0].Algorithm)
}

func TestKMSService_RotateDEK_SuccessfulRotation(t *testing.T) {
	mockMgr := newMockKMSManager()
	logger := newMockLogger()
	svc := newKMSService(mockMgr, logger)

	_, err := svc.CreateDEK(systemTenantCtx(), &CreateDEKRequest{
		Path:      "transit/dek/rotate-path",
		Algorithm: KeyAlgAES256GCM,
	})
	require.NoError(t, err)

	resp, err := svc.RotateDEK(systemTenantCtx(), &RotateDEKRequest{
		Path: "transit/dek/rotate-path",
	})
	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.Equal(t, uint64(2), resp.NewVersion)
	assert.Equal(t, uint64(1), resp.OldVersion)
}

func TestKMSService_DeleteDEK_DefaultWithForce(t *testing.T) {
	mockMgr := newMockKMSManager()
	logger := newMockLogger()
	svc := newKMSService(mockMgr, logger)

	_, err := svc.CreateDEK(systemTenantCtx(), &CreateDEKRequest{
		Path:      "transit/dek/force-delete",
		Algorithm: KeyAlgAES256GCM,
	})
	require.NoError(t, err)

	_, err = svc.SetDefaultDEK(systemTenantCtx(), &SetDefaultDEKRequest{
		Path: "transit/dek/force-delete",
	})
	require.NoError(t, err)

	resp, err := svc.DeleteDEK(systemTenantCtx(), &DeleteDEKRequest{
		Path:  "transit/dek/force-delete",
		Force: true,
	})
	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.True(t, resp.Deleted)
}

func TestKMSService_ClearKeySafety_NilRequest(t *testing.T) {
	mockMgr := newMockKMSManager()
	svc := newKMSService(mockMgr, createTestLogger(t))

	resp, err := svc.ClearKeySafety(systemTenantCtx(), nil)
	require.Error(t, err)
	assert.Nil(t, resp)
	var qbErr *quicbaasErrors.QBError
	require.ErrorAs(t, err, &qbErr)
	assert.Equal(t, quicbaasErrors.ErrInvalidInput, qbErr.Code)
}

func TestGetCompletedGracePeriodVersions_Success(t *testing.T) {
	now := time.Now()
	payload, _ := json.Marshal(gracePeriodPayload{
		KeyID:   "test-key",
		Version: 1,
	})

	mockEngine := &covMockSchedulerEngine{
		listJobsFn: func(ctx context.Context, query *scheduler.JobQuery) ([]*scheduler.Job, string, error) {
			return []*scheduler.Job{
				{
					ID:      1,
					Payload: payload,
					LastRun: now,
				},
			}, "", nil
		},
	}

	adapter := &SchedulerAdapter{scheduler: mockEngine}
	result, err := adapter.GetCompletedGracePeriodVersions(systemTenantCtx(), "test-key")
	require.NoError(t, err)
	require.Len(t, result, 1)
	assert.Equal(t, now, result[1])
}

func TestGetCompletedGracePeriodVersions_ListError(t *testing.T) {
	mockEngine := &covMockSchedulerEngine{
		listJobsFn: func(ctx context.Context, query *scheduler.JobQuery) ([]*scheduler.Job, string, error) {
			return nil, "", stderrors.New("list failed")
		},
	}

	adapter := &SchedulerAdapter{scheduler: mockEngine}
	result, err := adapter.GetCompletedGracePeriodVersions(systemTenantCtx(), "test-key")
	require.Error(t, err)
	assert.Nil(t, result)
}

func TestGetCompletedGracePeriodVersions_SkipsInvalidPayload(t *testing.T) {
	mockEngine := &covMockSchedulerEngine{
		listJobsFn: func(ctx context.Context, query *scheduler.JobQuery) ([]*scheduler.Job, string, error) {
			return []*scheduler.Job{
				{
					ID:      1,
					Payload: []byte(`not-valid-json`),
					LastRun: time.Now(),
				},
			}, "", nil
		},
	}

	adapter := &SchedulerAdapter{scheduler: mockEngine}
	result, err := adapter.GetCompletedGracePeriodVersions(systemTenantCtx(), "test-key")
	require.NoError(t, err)
	assert.Empty(t, result)
}

func TestGetCompletedGracePeriodVersions_FiltersByKeyID(t *testing.T) {
	now := time.Now()
	payload1, _ := json.Marshal(gracePeriodPayload{KeyID: "key-a", Version: 1})
	payload2, _ := json.Marshal(gracePeriodPayload{KeyID: "key-b", Version: 2})

	mockEngine := &covMockSchedulerEngine{
		listJobsFn: func(ctx context.Context, query *scheduler.JobQuery) ([]*scheduler.Job, string, error) {
			return []*scheduler.Job{
				{ID: 1, Payload: payload1, LastRun: now},
				{ID: 2, Payload: payload2, LastRun: now},
			}, "", nil
		},
	}

	adapter := &SchedulerAdapter{scheduler: mockEngine}
	result, err := adapter.GetCompletedGracePeriodVersions(systemTenantCtx(), "key-a")
	require.NoError(t, err)
	require.Len(t, result, 1)
	_, found := result[1]
	assert.True(t, found)
}

func TestDEKStore_Operations(t *testing.T) {
	t.Run("get returns false for missing entry", func(t *testing.T) {
		store := newDEKStore()
		entry, exists := store.get("nonexistent")
		assert.Nil(t, entry)
		assert.False(t, exists)
	})

	t.Run("set and get entry", func(t *testing.T) {
		store := newDEKStore()
		testEntry := &dekEntry{
			metadata: &DEKMetadata{
				Path:      "/test/dek",
				Algorithm: KeyAlgAES256GCM,
				Version:   1,
			},
			wrappedKey: []byte("wrapped-key-data"),
		}
		store.set("/test/dek", testEntry)

		entry, exists := store.get("/test/dek")
		assert.True(t, exists)
		assert.NotNil(t, entry)
		assert.Equal(t, "/test/dek", entry.metadata.Path)
	})

	t.Run("delete existing entry", func(t *testing.T) {
		store := newDEKStore()
		store.set("/test/dek", &dekEntry{
			metadata: &DEKMetadata{Path: "/test/dek"},
		})

		deleted := store.delete("/test/dek")
		assert.True(t, deleted)

		_, exists := store.get("/test/dek")
		assert.False(t, exists)
	})

	t.Run("delete nonexistent entry returns false", func(t *testing.T) {
		store := newDEKStore()
		deleted := store.delete("nonexistent")
		assert.False(t, deleted)
	})

	t.Run("list all entries", func(t *testing.T) {
		store := newDEKStore()
		store.set("/app1/dek1", &dekEntry{
			metadata: &DEKMetadata{Path: "/app1/dek1"},
		})
		store.set("/app1/dek2", &dekEntry{
			metadata: &DEKMetadata{Path: "/app1/dek2"},
		})
		store.set("/app2/dek1", &dekEntry{
			metadata: &DEKMetadata{Path: "/app2/dek1"},
		})

		allEntries := store.list("")
		assert.Len(t, allEntries, 3)
	})

	t.Run("list entries by prefix", func(t *testing.T) {
		store := newDEKStore()
		store.set("/app1/dek1", &dekEntry{
			metadata: &DEKMetadata{Path: "/app1/dek1"},
		})
		store.set("/app1/dek2", &dekEntry{
			metadata: &DEKMetadata{Path: "/app1/dek2"},
		})
		store.set("/app2/dek1", &dekEntry{
			metadata: &DEKMetadata{Path: "/app2/dek1"},
		})

		app1Entries := store.list("/app1")
		assert.Len(t, app1Entries, 2)
	})
}

func TestKMSService_SetScheduler(t *testing.T) {
	mockManager := newMockKMSManager()
	svc := newKMSService(mockManager, nil)

	// Verify scheduler is nil initially
	assert.Nil(t, svc.scheduler)

	// Create a mock scheduler (we just need to test the setter)
	mockScheduler := &mockKMSScheduler{}
	svc.SetScheduler(mockScheduler)

	// Verify scheduler is set
	assert.NotNil(t, svc.scheduler)
	assert.Equal(t, mockScheduler, svc.scheduler)
}

func TestKMSService_SetVersionStatus(t *testing.T) {
	mockManager := newMockKMSManager()
	svc := newKMSService(mockManager, nil)

	t.Run("set and get version status", func(t *testing.T) {
		keyID := "test-key-123"
		version := uint64(2)
		disabledAt := time.Now()

		// Set version status
		svc.setVersionStatus(keyID, version, KeyStatusDisabled, &disabledAt)

		// Get version status
		status, disabledTime := svc.getVersionStatus(systemTenantCtx(), keyID, version)
		assert.Equal(t, KeyStatusDisabled, status)
		require.NotNil(t, disabledTime)
		assert.WithinDuration(t, disabledAt, *disabledTime, time.Second)
	})

	t.Run("get version status for nonexistent key returns empty", func(t *testing.T) {
		status, disabledTime := svc.getVersionStatus(systemTenantCtx(), "nonexistent-key", 1)
		assert.Equal(t, KeyStatus(""), status)
		assert.Nil(t, disabledTime)
	})

	t.Run("set version status with nil disabled time", func(t *testing.T) {
		keyID := "test-key-456"
		version := uint64(1)

		svc.setVersionStatus(keyID, version, KeyStatusEnabled, nil)

		status, disabledTime := svc.getVersionStatus(systemTenantCtx(), keyID, version)
		assert.Equal(t, KeyStatusEnabled, status)
		assert.Nil(t, disabledTime)
	})
}

func TestKMSService_GetPublicKey(t *testing.T) {
	t.Run("nil request returns error", func(t *testing.T) {
		mockManager := newMockKMSManager()
		svc := newKMSService(mockManager, nil)

		resp, err := svc.GetPublicKey(systemTenantCtx(), nil)
		require.Error(t, err)
		assert.Nil(t, resp)
		assert.Contains(t, err.Error(), "request is required")
	})

	t.Run("empty key ID returns error", func(t *testing.T) {
		mockManager := newMockKMSManager()
		svc := newKMSService(mockManager, nil)

		resp, err := svc.GetPublicKey(systemTenantCtx(), &GetPublicKeyRequest{})
		require.Error(t, err)
		assert.Nil(t, resp)
		assert.Contains(t, err.Error(), "key ID is required")
	})

	t.Run("successful public key retrieval", func(t *testing.T) {
		mockManager := newMockKMSManager()
		// Pre-create the key
		mockManager.keys["test-key"] = &mockKeyData{
			keyID:     "test-key",
			algorithm: "ECDSA-P256",
			version:   2,
			status:    "enabled",
			createdAt: time.Now(),
		}
		svc := newKMSService(mockManager, nil)

		resp, err := svc.GetPublicKey(systemTenantCtx(), &GetPublicKeyRequest{
			KeyID: "test-key",
		})
		require.NoError(t, err)
		require.NotNil(t, resp)
		assert.Equal(t, "test-key", resp.KeyID)
		assert.NotEmpty(t, resp.PublicKey)
	})

	t.Run("nonexistent key returns error", func(t *testing.T) {
		mockManager := newMockKMSManager()
		svc := newKMSService(mockManager, nil)

		resp, err := svc.GetPublicKey(systemTenantCtx(), &GetPublicKeyRequest{
			KeyID: "nonexistent-key",
		})
		require.Error(t, err)
		assert.Nil(t, resp)
	})
}

func TestKMSService_CancelReencryption(t *testing.T) {
	t.Run("no reencryption in progress returns error", func(t *testing.T) {
		mockManager := newMockKMSManager()
		svc := newKMSService(mockManager, nil)

		err := svc.CancelReencryption(systemTenantCtx())
		require.Error(t, err)
		assert.Contains(t, err.Error(), "no re-encryption operation in progress")
	})
}

func TestKMSService_RotateKey_WithGracePeriod_Success(t *testing.T) {
	mockManager := newMockKMSManager()
	mockManager.keys["grace-key"] = &mockKeyData{
		keyID:     "grace-key",
		algorithm: "AES-256-GCM",
		version:   1,
		status:    "enabled",
		createdAt: time.Now(),
	}
	logger := newMockLogger()
	svc := newKMSService(mockManager, logger)

	scheduler := &rotateKeyTestScheduler{}
	svc.SetScheduler(scheduler)

	req := &RotateKeyRequest{
		KeyID:              "grace-key",
		GracePeriodSeconds: 3600,
	}

	resp, err := svc.RotateKey(systemTenantCtx(), req)
	require.NoError(t, err)
	require.NotNil(t, resp)

	assert.Equal(t, "grace-key", resp.KeyID)
	assert.Equal(t, uint64(1), resp.OldVersion)
	assert.Equal(t, uint64(2), resp.NewVersion)
	assert.NotZero(t, resp.GracePeriodDisableAt)

	require.NotNil(t, scheduler.lastJob)
	assert.Contains(t, scheduler.lastJob.Name, "kms-grace-grace-key-v1")
	assert.Equal(t, "kms.grace_period", scheduler.lastJob.Handler)
}

func TestKMSService_RotateKey_WithGracePeriod_SchedulerError(t *testing.T) {
	mockManager := newMockKMSManager()
	mockManager.keys["sched-err-key"] = &mockKeyData{
		keyID:     "sched-err-key",
		algorithm: "AES-256-GCM",
		version:   3,
		status:    "enabled",
		createdAt: time.Now(),
	}
	logger := newMockLogger()
	svc := newKMSService(mockManager, logger)

	scheduler := &rotateKeyTestScheduler{
		scheduleJobFn: func(ctx context.Context, job *SchedulerJob) error {
			return stderrors.New("scheduler unavailable")
		},
	}
	svc.SetScheduler(scheduler)

	req := &RotateKeyRequest{
		KeyID:              "sched-err-key",
		GracePeriodSeconds: 1800,
	}

	resp, err := svc.RotateKey(systemTenantCtx(), req)
	require.NoError(t, err)
	require.NotNil(t, resp)

	assert.Equal(t, "sched-err-key", resp.KeyID)
	assert.Equal(t, uint64(3), resp.OldVersion)
	assert.Equal(t, uint64(4), resp.NewVersion)
	assert.Zero(t, resp.GracePeriodDisableAt)
}

func TestKMSService_RotateKey_WithGracePeriod_SchedulerError_NilLogger(t *testing.T) {
	mockManager := newMockKMSManager()
	mockManager.keys["no-log-key"] = &mockKeyData{
		keyID:     "no-log-key",
		algorithm: "AES-256-GCM",
		version:   1,
		status:    "enabled",
		createdAt: time.Now(),
	}
	svc := newKMSService(mockManager, nil)

	scheduler := &rotateKeyTestScheduler{
		scheduleJobFn: func(ctx context.Context, job *SchedulerJob) error {
			return stderrors.New("scheduler error")
		},
	}
	svc.SetScheduler(scheduler)

	req := &RotateKeyRequest{
		KeyID:              "no-log-key",
		GracePeriodSeconds: 600,
	}

	resp, err := svc.RotateKey(systemTenantCtx(), req)
	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.Equal(t, "no-log-key", resp.KeyID)
}

func TestKMSService_RotateKey_NoGracePeriod(t *testing.T) {
	mockManager := newMockKMSManager()
	mockManager.keys["no-grace-key"] = &mockKeyData{
		keyID:     "no-grace-key",
		algorithm: "AES-256-GCM",
		version:   1,
		status:    "enabled",
		createdAt: time.Now(),
	}
	logger := newMockLogger()
	svc := newKMSService(mockManager, logger)

	scheduler := &rotateKeyTestScheduler{}
	svc.SetScheduler(scheduler)

	req := &RotateKeyRequest{
		KeyID:              "no-grace-key",
		GracePeriodSeconds: 0,
	}

	resp, err := svc.RotateKey(systemTenantCtx(), req)
	require.NoError(t, err)
	require.NotNil(t, resp)

	assert.Nil(t, scheduler.lastJob)
	assert.Zero(t, resp.GracePeriodDisableAt)
}

func TestKMSService_RotateKey_GracePeriod_NilScheduler(t *testing.T) {
	mockManager := newMockKMSManager()
	mockManager.keys["nil-sched-key"] = &mockKeyData{
		keyID:     "nil-sched-key",
		algorithm: "AES-256-GCM",
		version:   2,
		status:    "enabled",
		createdAt: time.Now(),
	}
	logger := newMockLogger()
	svc := newKMSService(mockManager, logger)

	req := &RotateKeyRequest{
		KeyID:              "nil-sched-key",
		GracePeriodSeconds: 3600,
	}

	resp, err := svc.RotateKey(systemTenantCtx(), req)
	require.NoError(t, err)
	require.NotNil(t, resp)

	assert.Equal(t, "nil-sched-key", resp.KeyID)
	assert.Zero(t, resp.GracePeriodDisableAt)
}

func TestKMSService_ListKeyVersions_Success(t *testing.T) {
	mockManager := newMockKMSManager()
	// Create a key with multiple versions
	mockManager.keys["test-key-123"] = &mockKeyData{
		keyID:     "test-key-123",
		algorithm: "AES-256-GCM",
		version:   3, // Has been rotated twice
		status:    "enabled",
		createdAt: time.Now(),
	}
	// Override GetKey to return KeyMetadata type
	mockManager.getKeyFn = func(ctx context.Context, keyID string) (interface{}, error) {
		if keyID == "test-key-123" {
			return &KeyMetadata{
				KeyID:     "test-key-123",
				Name:      "test-key-123",
				Algorithm: KeyAlgAES256GCM,
				Version:   3,
				Status:    KeyStatusEnabled,
				CreatedAt: time.Now(),
				UpdatedAt: time.Now(),
			}, nil
		}
		return nil, stderrors.New("key not found")
	}
	logger := newMockLogger()
	svc := newKMSService(mockManager, logger)

	req := &ListKeyVersionsRequest{
		KeyID: "test-key-123",
		Limit: 10,
	}

	resp, err := svc.ListKeyVersions(systemTenantCtx(), req)

	require.NoError(t, err)
	assert.NotNil(t, resp)
	assert.Equal(t, "test-key-123", resp.KeyID)
}

func TestKMSService_ListKeyVersions_NilRequest(t *testing.T) {
	mockManager := newMockKMSManager()
	logger := newMockLogger()
	svc := newKMSService(mockManager, logger)

	resp, err := svc.ListKeyVersions(systemTenantCtx(), nil)

	require.Error(t, err)
	assert.Nil(t, resp)
	assert.Contains(t, err.Error(), "request is nil")
}

func TestKMSService_ListKeyVersions_EmptyKeyID(t *testing.T) {
	mockManager := newMockKMSManager()
	logger := newMockLogger()
	svc := newKMSService(mockManager, logger)

	req := &ListKeyVersionsRequest{
		KeyID: "",
		Limit: 10,
	}

	resp, err := svc.ListKeyVersions(systemTenantCtx(), req)

	require.Error(t, err)
	assert.Nil(t, resp)
	assert.Contains(t, err.Error(), "key ID is required")
}

func TestKMSService_ListKeyVersions_SingleVersion(t *testing.T) {
	mockManager := newMockKMSManager()
	mockManager.getKeyFn = func(ctx context.Context, keyID string) (interface{}, error) {
		return &KeyMetadata{
			KeyID:     keyID,
			Name:      keyID,
			Algorithm: KeyAlgAES256GCM,
			Version:   1, // Single version
			Status:    KeyStatusEnabled,
			CreatedAt: time.Now(),
		}, nil
	}
	logger := newMockLogger()
	svc := newKMSService(mockManager, logger)

	req := &ListKeyVersionsRequest{
		KeyID: "single-version-key",
	}

	resp, err := svc.ListKeyVersions(systemTenantCtx(), req)

	require.NoError(t, err)
	assert.NotNil(t, resp)
}

func TestKMSService_ListKeyVersions_ZeroVersion(t *testing.T) {
	mockManager := newMockKMSManager()
	// Return a key with version 0 - should be normalized to 1
	mockManager.getKeyFn = func(ctx context.Context, keyID string) (interface{}, error) {
		return &KeyMetadata{
			KeyID:     keyID,
			Version:   0, // Zero version - edge case
			Status:    KeyStatusEnabled,
			CreatedAt: time.Now(),
		}, nil
	}
	logger := newMockLogger()
	svc := newKMSService(mockManager, logger)

	req := &ListKeyVersionsRequest{
		KeyID: "zero-version-key",
	}

	resp, err := svc.ListKeyVersions(systemTenantCtx(), req)

	require.NoError(t, err)
	assert.NotNil(t, resp)
}

func TestKMSService_UpdateKeyStatus_EnableKey(t *testing.T) {
	mockManager := newMockKMSManager()
	mockManager.keys["test-key-123"] = &mockKeyData{
		keyID:     "test-key-123",
		algorithm: "AES-256-GCM",
		version:   1,
		status:    "disabled",
		createdAt: time.Now(),
	}
	mockManager.getKeyFn = func(ctx context.Context, keyID string) (interface{}, error) {
		return &KeyMetadata{
			KeyID:     keyID,
			Version:   1,
			Status:    KeyStatusDisabled,
			Algorithm: KeyAlgAES256GCM,
		}, nil
	}
	logger := newMockLogger()
	svc := newKMSService(mockManager, logger)

	req := &UpdateKeyStatusRequest{
		KeyID:  "test-key-123",
		Status: KeyStatusEnabled,
	}

	resp, err := svc.UpdateKeyStatus(systemTenantCtx(), req)

	require.NoError(t, err)
	assert.NotNil(t, resp)
	assert.Equal(t, "test-key-123", resp.KeyID)
	assert.Equal(t, KeyStatusEnabled, resp.NewStatus)
	assert.Equal(t, KeyStatusEnabled, resp.Status)
}

func TestKMSService_UpdateKeyStatus_DisableKey(t *testing.T) {
	mockManager := newMockKMSManager()
	mockManager.keys["test-key-456"] = &mockKeyData{
		keyID:     "test-key-456",
		algorithm: "AES-256-GCM",
		version:   2,
		status:    "enabled",
		createdAt: time.Now(),
	}
	mockManager.getKeyFn = func(ctx context.Context, keyID string) (interface{}, error) {
		return &KeyMetadata{
			KeyID:     keyID,
			Version:   2,
			Status:    KeyStatusEnabled,
			Algorithm: KeyAlgAES256GCM,
		}, nil
	}
	logger := newMockLogger()
	svc := newKMSService(mockManager, logger)

	req := &UpdateKeyStatusRequest{
		KeyID:  "test-key-456",
		Status: KeyStatusDisabled,
	}

	resp, err := svc.UpdateKeyStatus(systemTenantCtx(), req)

	require.NoError(t, err)
	assert.NotNil(t, resp)
	assert.Equal(t, "test-key-456", resp.KeyID)
	assert.Equal(t, KeyStatusDisabled, resp.NewStatus)
}

func TestKMSService_UpdateKeyStatus_NilRequest(t *testing.T) {
	mockManager := newMockKMSManager()
	logger := newMockLogger()
	svc := newKMSService(mockManager, logger)

	resp, err := svc.UpdateKeyStatus(systemTenantCtx(), nil)

	require.Error(t, err)
	assert.Nil(t, resp)
	assert.Contains(t, err.Error(), "request is nil")
}

func TestKMSService_UpdateKeyStatus_EmptyKeyID(t *testing.T) {
	mockManager := newMockKMSManager()
	logger := newMockLogger()
	svc := newKMSService(mockManager, logger)

	req := &UpdateKeyStatusRequest{
		KeyID:  "",
		Status: KeyStatusEnabled,
	}

	resp, err := svc.UpdateKeyStatus(systemTenantCtx(), req)

	require.Error(t, err)
	assert.Nil(t, resp)
	assert.Contains(t, err.Error(), "key ID is required")
}

func TestKMSService_UpdateKeyStatus_WithNonMetadataResponse(t *testing.T) {
	mockManager := newMockKMSManager()
	// Return non-KeyMetadata type from GetKey
	mockManager.getKeyFn = func(ctx context.Context, keyID string) (interface{}, error) {
		return &mockKeyData{
			keyID:   keyID,
			version: 1,
			status:  "enabled",
		}, nil
	}
	mockManager.keys["test-key"] = &mockKeyData{
		keyID:   "test-key",
		version: 1,
		status:  "enabled",
	}
	logger := newMockLogger()
	svc := newKMSService(mockManager, logger)

	req := &UpdateKeyStatusRequest{
		KeyID:  "test-key",
		Status: KeyStatusDisabled,
	}

	resp, err := svc.UpdateKeyStatus(systemTenantCtx(), req)

	// Should still work with default values
	require.NoError(t, err)
	assert.NotNil(t, resp)
}

func TestKMSService_GetKey_WithKeyMetadata(t *testing.T) {
	mockManager := newMockKMSManager()
	mockManager.getKeyFn = func(ctx context.Context, keyID string) (interface{}, error) {
		return &KeyMetadata{
			KeyID:              keyID,
			Name:               "my-encryption-key",
			Description:        "Test encryption key",
			Algorithm:          KeyAlgAES256GCM,
			Version:            3,
			Status:             KeyStatusEnabled,
			Usage:              []KeyUsage{KeyUsageEncrypt, KeyUsageDecrypt},
			Exportable:         true,
			DeletionProtection: false,
			CreatedAt:          time.Now().Add(-24 * time.Hour),
			UpdatedAt:          time.Now(),
		}, nil
	}
	logger := newMockLogger()
	svc := newKMSService(mockManager, logger)

	req := &GetKeyRequest{
		KeyID: "test-key-123",
	}

	resp, err := svc.GetKey(systemTenantCtx(), req)

	require.NoError(t, err)
	assert.NotNil(t, resp)
	assert.Equal(t, "test-key-123", resp.Key.KeyID)
}

func TestKMSService_GetKey_SpecificVersion(t *testing.T) {
	mockManager := newMockKMSManager()
	mockManager.getKeyFn = func(ctx context.Context, keyID string) (interface{}, error) {
		return &KeyMetadata{
			KeyID:     keyID,
			Algorithm: KeyAlgAES256GCM,
			Version:   5, // Current version is 5
			Status:    KeyStatusEnabled,
			CreatedAt: time.Now(),
		}, nil
	}
	logger := newMockLogger()
	svc := newKMSService(mockManager, logger)

	req := &GetKeyRequest{
		KeyID:   "test-key-123",
		Version: 3, // Request specific version
	}

	resp, err := svc.GetKey(systemTenantCtx(), req)

	require.NoError(t, err)
	assert.NotNil(t, resp)
	assert.Equal(t, uint64(3), resp.Key.Version) // Should return requested version
}

func TestKMSService_GetKey_NonKeyMetadataResponse(t *testing.T) {
	mockManager := newMockKMSManager()
	// Return non-KeyMetadata type
	mockManager.getKeyFn = func(ctx context.Context, keyID string) (interface{}, error) {
		return &mockKeyData{
			keyID:     keyID,
			algorithm: "AES-256-GCM",
			version:   1,
			status:    "enabled",
			createdAt: time.Now(),
		}, nil
	}
	logger := newMockLogger()
	svc := newKMSService(mockManager, logger)

	req := &GetKeyRequest{
		KeyID: "test-key",
	}

	resp, err := svc.GetKey(systemTenantCtx(), req)

	// Should work with default values
	require.NoError(t, err)
	assert.NotNil(t, resp)
	assert.Equal(t, "test-key", resp.Key.KeyID)
}

func TestKMSService_GetKeyMetadata_WithFullMetadata(t *testing.T) {
	mockManager := newMockKMSManager()
	mockManager.getKeyFn = func(ctx context.Context, keyID string) (interface{}, error) {
		return &KeyMetadata{
			KeyID:       keyID,
			Name:        "encryption-key",
			Description: "Production encryption key",
			Algorithm:   KeyAlgAES256GCM,
			Version:     2,
			Status:      KeyStatusEnabled,
			Tags:        map[string]string{"env": "prod", "team": "security"},
			CreatedAt:   time.Now().Add(-48 * time.Hour),
			UpdatedAt:   time.Now(),
		}, nil
	}
	logger := newMockLogger()
	svc := newKMSService(mockManager, logger)

	req := &GetKeyMetadataRequest{
		KeyID: "test-key-123",
	}

	resp, err := svc.GetKeyMetadata(systemTenantCtx(), req)

	require.NoError(t, err)
	assert.NotNil(t, resp)
	assert.Equal(t, "test-key-123", resp.Metadata.KeyID)
}

func TestKMSService_GetKeyMetadata_NonKeyMetadataResponse(t *testing.T) {
	mockManager := newMockKMSManager()
	// Return non-KeyMetadata type
	mockManager.getKeyFn = func(ctx context.Context, keyID string) (interface{}, error) {
		return &mockKeyData{
			keyID:     keyID,
			algorithm: "AES-256-GCM",
			version:   1,
			status:    "enabled",
		}, nil
	}
	logger := newMockLogger()
	svc := newKMSService(mockManager, logger)

	req := &GetKeyMetadataRequest{
		KeyID: "test-key",
	}

	resp, err := svc.GetKeyMetadata(systemTenantCtx(), req)

	// Should work with default values
	require.NoError(t, err)
	assert.NotNil(t, resp)
	assert.Equal(t, "test-key", resp.Metadata.KeyID)
	// Default values should be used
	assert.Equal(t, KeyAlgAES256GCM, resp.Metadata.Algorithm)
	assert.Equal(t, KeyStatusEnabled, resp.Metadata.Status)
	assert.Equal(t, uint64(1), resp.Metadata.Version)
}

func TestKMSService_CreateDEK_NilRequest(t *testing.T) {
	mockManager := newMockKMSManager()
	logger := newMockLogger()
	svc := newKMSService(mockManager, logger)

	resp, err := svc.CreateDEK(systemTenantCtx(), nil)

	require.Error(t, err)
	assert.Nil(t, resp)
	assert.Contains(t, err.Error(), "request is nil")
}

func TestKMSService_GetDEK_NilRequest(t *testing.T) {
	mockManager := newMockKMSManager()
	logger := newMockLogger()
	svc := newKMSService(mockManager, logger)

	resp, err := svc.GetDEK(systemTenantCtx(), nil)

	require.Error(t, err)
	assert.Nil(t, resp)
	assert.Contains(t, err.Error(), "request is nil")
}

func TestKMSService_RotateDEK_NilRequest(t *testing.T) {
	mockManager := newMockKMSManager()
	logger := newMockLogger()
	svc := newKMSService(mockManager, logger)

	resp, err := svc.RotateDEK(systemTenantCtx(), nil)

	require.Error(t, err)
	assert.Nil(t, resp)
	assert.Contains(t, err.Error(), "request is nil")
}

func TestKMSService_DeleteDEK_NilRequest(t *testing.T) {
	mockManager := newMockKMSManager()
	logger := newMockLogger()
	svc := newKMSService(mockManager, logger)

	resp, err := svc.DeleteDEK(systemTenantCtx(), nil)

	require.Error(t, err)
	assert.Nil(t, resp)
	assert.Contains(t, err.Error(), "request is nil")
}

func TestKMSService_GetDEKStatus_NilRequest(t *testing.T) {
	mockManager := newMockKMSManager()
	logger := newMockLogger()
	svc := newKMSService(mockManager, logger)

	resp, err := svc.GetDEKStatus(systemTenantCtx(), nil)

	require.Error(t, err)
	assert.Nil(t, resp)
	assert.Contains(t, err.Error(), "request is nil")
}

func TestKMSService_SetDefaultDEK_NilRequest(t *testing.T) {
	mockManager := newMockKMSManager()
	logger := newMockLogger()
	svc := newKMSService(mockManager, logger)

	resp, err := svc.SetDefaultDEK(systemTenantCtx(), nil)

	require.Error(t, err)
	assert.Nil(t, resp)
	assert.Contains(t, err.Error(), "request is nil")
}

func TestKMSService_SetDEKSafety_NilRequest(t *testing.T) {
	mockManager := newMockKMSManager()
	logger := newMockLogger()
	svc := newKMSService(mockManager, logger)

	resp, err := svc.SetDEKSafety(systemTenantCtx(), nil)

	require.Error(t, err)
	assert.Nil(t, resp)
	assert.Contains(t, err.Error(), "request is nil")
}

func TestKMSService_ClearDEKSafety_NilRequest(t *testing.T) {
	mockManager := newMockKMSManager()
	logger := newMockLogger()
	svc := newKMSService(mockManager, logger)

	resp, err := svc.ClearDEKSafety(systemTenantCtx(), nil)

	require.Error(t, err)
	assert.Nil(t, resp)
	assert.Contains(t, err.Error(), "request is nil")
}

func TestKMSService_ListKeys_WithPrefixFilter(t *testing.T) {
	mockManager := newMockKMSManager()
	mockManager.listKeysFn = func(ctx context.Context, prefix string) ([]interface{}, error) {
		return []interface{}{
			&KeyMetadata{KeyID: "key-prod-1", Name: "prod-key-1", Algorithm: KeyAlgAES256GCM, Version: 1, Status: KeyStatusEnabled},
			&KeyMetadata{KeyID: "key-prod-2", Name: "prod-key-2", Algorithm: KeyAlgAES256GCM, Version: 1, Status: KeyStatusEnabled},
			&KeyMetadata{KeyID: "key-dev-1", Name: "dev-key-1", Algorithm: KeyAlgAES256GCM, Version: 1, Status: KeyStatusEnabled},
		}, nil
	}
	logger := newMockLogger()
	svc := newKMSService(mockManager, logger)

	req := &ListKeysRequest{
		Filter: map[string]string{"prefix": "prod-"},
		Limit:  10,
	}

	resp, err := svc.ListKeys(systemTenantCtx(), req)

	require.NoError(t, err)
	assert.NotNil(t, resp)
}

func TestKMSService_ListKeys_DefaultLimit(t *testing.T) {
	mockManager := newMockKMSManager()
	logger := newMockLogger()
	svc := newKMSService(mockManager, logger)

	req := &ListKeysRequest{
		Limit: 0, // Should use default limit
	}

	resp, err := svc.ListKeys(systemTenantCtx(), req)

	require.NoError(t, err)
	assert.NotNil(t, resp)
}

func TestKMSService_SafetyCheck_NilRequest(t *testing.T) {
	mockManager := newMockKMSManager()
	logger := newMockLogger()
	svc := newKMSService(mockManager, logger)

	resp, err := svc.SafetyCheck(systemTenantCtx(), nil)

	// SafetyCheck handles nil by using defaults
	require.NoError(t, err)
	assert.NotNil(t, resp)
}

func TestKMSService_SafetyCheck_ZeroThreshold(t *testing.T) {
	mockManager := newMockKMSManager()
	logger := newMockLogger()
	svc := newKMSService(mockManager, logger)

	req := &SafetyCheckRequest{
		ThresholdPercent: 0, // Zero threshold
	}

	resp, err := svc.SafetyCheck(systemTenantCtx(), req)

	require.NoError(t, err)
	assert.NotNil(t, resp)
}

func TestKMSService_SafetyReport_NilRequest(t *testing.T) {
	mockManager := newMockKMSManager()
	logger := newMockLogger()
	svc := newKMSService(mockManager, logger)

	resp, err := svc.SafetyReport(systemTenantCtx(), nil)

	require.NoError(t, err)
	assert.NotNil(t, resp)
}

func TestKMSService_GenerateKey_MissingUsage_Coverage(t *testing.T) {
	mockManager := newMockKMSManager()
	logger := newMockLogger()
	svc := newKMSService(mockManager, logger)

	req := &GenerateKeyRequest{
		Name: "test-key",
		KeyAttributes: &xkmssdk.KeyAttributes{
			SymmetricAlgorithm: xkmssdk.SymmetricAES256GCM,
		},
		Usage: []KeyUsage{}, // Missing usage
	}

	resp, err := svc.GenerateKey(systemTenantCtx(), req)

	require.Error(t, err)
	assert.Nil(t, resp)
	assert.Contains(t, err.Error(), "at least one key usage is required")
}

func TestKMSService_RotateKey_ManagerError(t *testing.T) {
	mockManager := newMockKMSManager()
	mockManager.keys["test-key"] = &mockKeyData{
		keyID:   "test-key",
		version: 1,
		status:  "enabled",
	}
	mockManager.getKeyFn = func(ctx context.Context, keyID string) (interface{}, error) {
		return &kms.KeyMetadata{KeyID: keyID, Version: 1, State: kms.KeyStateEnabled}, nil
	}
	mockManager.rotateKeyFn = func(ctx context.Context, keyID string) (interface{}, error) {
		return nil, stderrors.New("rotation failed")
	}
	logger := newMockLogger()
	svc := newKMSService(mockManager, logger)

	req := &RotateKeyRequest{
		KeyID: "test-key",
	}

	resp, err := svc.RotateKey(systemTenantCtx(), req)

	require.Error(t, err)
	assert.Nil(t, resp)
	assert.Contains(t, err.Error(), "failed to rotate key")
}

func TestKMSService_DeleteKey_ManagerError(t *testing.T) {
	mockManager := newMockKMSManager()
	mockManager.deleteKeyFn = func(ctx context.Context, keyID string) error {
		return stderrors.New("deletion failed")
	}
	logger := newMockLogger()
	svc := newKMSService(mockManager, logger)

	req := &DeleteKeyRequest{
		KeyID:             "test-key",
		PendingWindowDays: 7,
	}

	resp, err := svc.DeleteKey(systemTenantCtx(), req)

	require.Error(t, err)
	assert.Nil(t, resp)
	assert.Contains(t, err.Error(), "failed to delete key")
}

// TestKMSService_DeleteKey_NotFoundPreservesCode is a regression guard for the
// already-deleted-key 500 bug: when the manager reports the key as missing the
// service must surface ErrNotFound (-> HTTP 404), not clobber it with
// ErrInternal (-> HTTP 500). A genuine manager failure must stay ErrInternal.
func TestKMSService_DeleteKey_NotFoundPreservesCode(t *testing.T) {
	logger := newMockLogger()

	t.Run("missing key maps to not found", func(t *testing.T) {
		mockManager := newMockKMSManager()
		mockManager.deleteKeyFn = func(ctx context.Context, keyID string) error {
			return kms.ErrKeyNotFound
		}
		svc := newKMSService(mockManager, logger)

		resp, err := svc.DeleteKey(systemTenantCtx(), &DeleteKeyRequest{
			KeyID:             "already-deleted",
			PendingWindowDays: 7,
		})

		require.Error(t, err)
		assert.Nil(t, resp)
		var qbErr *quicbaasErrors.QBError
		require.True(t, stderrors.As(err, &qbErr), "expected a QBError")
		assert.Equal(t, quicbaasErrors.ErrNotFound, qbErr.Code)
	})

	t.Run("genuine failure stays internal", func(t *testing.T) {
		mockManager := newMockKMSManager()
		mockManager.deleteKeyFn = func(ctx context.Context, keyID string) error {
			return stderrors.New("backend exploded")
		}
		svc := newKMSService(mockManager, logger)

		resp, err := svc.DeleteKey(systemTenantCtx(), &DeleteKeyRequest{
			KeyID:             "boom",
			PendingWindowDays: 7,
		})

		require.Error(t, err)
		assert.Nil(t, resp)
		var qbErr *quicbaasErrors.QBError
		require.True(t, stderrors.As(err, &qbErr), "expected a QBError")
		assert.Equal(t, quicbaasErrors.ErrInternal, qbErr.Code)
	})
}

func TestKMSService_RevokeKey_ManagerError(t *testing.T) {
	mockManager := newMockKMSManager()
	mockManager.revokeKeyFn = func(ctx context.Context, keyID string) error {
		return stderrors.New("revocation failed")
	}
	logger := newMockLogger()
	svc := newKMSService(mockManager, logger)

	req := &RevokeKeyRequest{
		KeyID:  "test-key",
		Reason: "compromised",
	}

	resp, err := svc.RevokeKey(systemTenantCtx(), req)

	require.Error(t, err)
	assert.Nil(t, resp)
	assert.Contains(t, err.Error(), "failed to revoke key")
}

func TestKMSService_ValidateGenerateKeyRequest(t *testing.T) {
	tests := []struct {
		name        string
		setupFunc   func(*kmsServiceImpl)
		req         *GenerateKeyRequest
		wantErr     bool
		errContains string
	}{
		{
			name:        "nil request returns error",
			req:         nil,
			wantErr:     true,
			errContains: "request is nil",
		},
		{
			name:        "empty name returns error",
			req:         &GenerateKeyRequest{Name: ""},
			wantErr:     true,
			errContains: "key name is required",
		},
		{
			name:        "whitespace-only name returns error",
			req:         &GenerateKeyRequest{Name: "   "},
			wantErr:     true,
			errContains: "key name is required",
		},
		{
			name: "missing algorithm returns error",
			req: &GenerateKeyRequest{
				Name:          "test-key",
				KeyAttributes: nil,
			},
			wantErr:     true,
			errContains: "algorithm is required",
		},
		{
			name: "empty usage returns error",
			req: &GenerateKeyRequest{
				Name: "test-key",
				KeyAttributes: &xkmssdk.KeyAttributes{
					SymmetricAlgorithm: xkmssdk.SymmetricAES256GCM,
				},
				Usage: []KeyUsage{},
			},
			wantErr:     true,
			errContains: "at least one key usage is required",
		},
		{
			name: "valid request returns no error",
			req: &GenerateKeyRequest{
				Name: "test-key",
				KeyAttributes: &xkmssdk.KeyAttributes{
					SymmetricAlgorithm: xkmssdk.SymmetricAES256GCM,
				},
				Usage: []KeyUsage{KeyUsageEncrypt, KeyUsageDecrypt},
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			k := newTestKMSServiceForValidation()
			if tt.setupFunc != nil {
				tt.setupFunc(k)
			}
			err := k.validateGenerateKeyRequest(tt.req)
			if tt.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.errContains)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestKMSService_ValidateImportKeyRequest(t *testing.T) {
	tests := []struct {
		name        string
		req         *ImportKeyRequest
		wantErr     bool
		errContains string
	}{
		{
			name:        "nil request returns error",
			req:         nil,
			wantErr:     true,
			errContains: "request is nil",
		},
		{
			name: "blank key name returns error",
			req: &ImportKeyRequest{
				Name:        "  ",
				KeyMaterial: []byte("some-key-data"),
			},
			wantErr:     true,
			errContains: "key name is required",
		},
		{
			name: "reserved tenant namespace returns error",
			req: &ImportKeyRequest{
				Name:        kmsTenantKeyPrefix + "other-tenant" + kmsTenantKeySuffix + "planted",
				KeyMaterial: []byte("some-key-data"),
			},
			wantErr:     true,
			errContains: "reserved tenant namespace",
		},
		{
			name: "empty key material returns error",
			req: &ImportKeyRequest{
				Name:        "import-key",
				KeyMaterial: []byte{},
			},
			wantErr:     true,
			errContains: "key material is required",
		},
		{
			name: "missing algorithm returns error",
			req: &ImportKeyRequest{
				Name:          "import-key",
				KeyMaterial:   []byte("some-key-data"),
				KeyAttributes: nil,
			},
			wantErr:     true,
			errContains: "algorithm is required",
		},
		{
			name: "valid request returns no error",
			req: &ImportKeyRequest{
				Name:        "import-key",
				KeyMaterial: make([]byte, 32),
				KeyAttributes: &xkmssdk.KeyAttributes{
					SymmetricAlgorithm: xkmssdk.SymmetricAES256GCM,
				},
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			k := newTestKMSServiceForValidation()
			err := k.validateImportKeyRequest(tt.req)
			if tt.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.errContains)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestKMSService_ValidateGetKeyRequest(t *testing.T) {
	tests := []struct {
		name        string
		req         *GetKeyRequest
		wantErr     bool
		errContains string
	}{
		{
			name:        "nil request returns error",
			req:         nil,
			wantErr:     true,
			errContains: "request is nil",
		},
		{
			name:        "empty key ID returns error",
			req:         &GetKeyRequest{KeyID: ""},
			wantErr:     true,
			errContains: "key ID is required",
		},
		{
			name:    "valid request returns no error",
			req:     &GetKeyRequest{KeyID: "test-key-id"},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			k := newTestKMSServiceForValidation()
			err := k.validateGetKeyRequest(tt.req)
			if tt.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.errContains)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestKMSService_ValidateRotateKeyRequest(t *testing.T) {
	tests := []struct {
		name        string
		req         *RotateKeyRequest
		wantErr     bool
		errContains string
	}{
		{
			name:        "nil request returns error",
			req:         nil,
			wantErr:     true,
			errContains: "request is nil",
		},
		{
			name:        "empty key ID returns error",
			req:         &RotateKeyRequest{KeyID: ""},
			wantErr:     true,
			errContains: "key ID is required",
		},
		{
			name:    "valid request returns no error",
			req:     &RotateKeyRequest{KeyID: "test-key-id"},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			k := newTestKMSServiceForValidation()
			err := k.validateRotateKeyRequest(tt.req)
			if tt.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.errContains)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestKMSService_ValidateRevokeKeyRequest(t *testing.T) {
	tests := []struct {
		name        string
		req         *RevokeKeyRequest
		wantErr     bool
		errContains string
	}{
		{
			name:        "nil request returns error",
			req:         nil,
			wantErr:     true,
			errContains: "request is nil",
		},
		{
			name:        "empty key ID returns error",
			req:         &RevokeKeyRequest{KeyID: ""},
			wantErr:     true,
			errContains: "key ID is required",
		},
		{
			name:    "valid request returns no error",
			req:     &RevokeKeyRequest{KeyID: "test-key-id"},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			k := newTestKMSServiceForValidation()
			err := k.validateRevokeKeyRequest(tt.req)
			if tt.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.errContains)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestKMSService_ValidateDeleteKeyRequest(t *testing.T) {
	tests := []struct {
		name        string
		req         *DeleteKeyRequest
		wantErr     bool
		errContains string
	}{
		{
			name:        "nil request returns error",
			req:         nil,
			wantErr:     true,
			errContains: "request is nil",
		},
		{
			name:        "empty key ID returns error",
			req:         &DeleteKeyRequest{KeyID: ""},
			wantErr:     true,
			errContains: "key ID is required",
		},
		{
			name:        "negative pending window returns error",
			req:         &DeleteKeyRequest{KeyID: "test-key", PendingWindowDays: -1},
			wantErr:     true,
			errContains: "pending window days must be between 0 and 30",
		},
		{
			name:        "pending window over 30 days returns error",
			req:         &DeleteKeyRequest{KeyID: "test-key", PendingWindowDays: 31},
			wantErr:     true,
			errContains: "pending window days must be between 0 and 30",
		},
		{
			name:    "valid request with 0 pending window returns no error",
			req:     &DeleteKeyRequest{KeyID: "test-key-id", PendingWindowDays: 0},
			wantErr: false,
		},
		{
			name:    "valid request with 30 pending window returns no error",
			req:     &DeleteKeyRequest{KeyID: "test-key-id", PendingWindowDays: 30},
			wantErr: false,
		},
		{
			name:    "valid request with 7 pending window returns no error",
			req:     &DeleteKeyRequest{KeyID: "test-key-id", PendingWindowDays: 7},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			k := newTestKMSServiceForValidation()
			err := k.validateDeleteKeyRequest(tt.req)
			if tt.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.errContains)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestKMSService_ValidateGetKeyMetadataRequest(t *testing.T) {
	tests := []struct {
		name        string
		req         *GetKeyMetadataRequest
		wantErr     bool
		errContains string
	}{
		{
			name:        "nil request returns error",
			req:         nil,
			wantErr:     true,
			errContains: "request is nil",
		},
		{
			name:        "empty key ID returns error",
			req:         &GetKeyMetadataRequest{KeyID: ""},
			wantErr:     true,
			errContains: "key ID is required",
		},
		{
			name:    "valid request returns no error",
			req:     &GetKeyMetadataRequest{KeyID: "test-key-id"},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			k := newTestKMSServiceForValidation()
			err := k.validateGetKeyMetadataRequest(tt.req)
			if tt.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.errContains)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestKMSService_ValidateUpdateKeyMetadataRequest(t *testing.T) {
	strPtr := func(s string) *string { return &s }

	tests := []struct {
		name        string
		req         *UpdateKeyMetadataRequest
		wantErr     bool
		errContains string
	}{
		{
			name:        "nil request returns error",
			req:         nil,
			wantErr:     true,
			errContains: "request is nil",
		},
		{
			name:        "empty key ID returns error",
			req:         &UpdateKeyMetadataRequest{KeyID: ""},
			wantErr:     true,
			errContains: "key ID is required",
		},
		{
			name: "no fields to update returns error",
			req: &UpdateKeyMetadataRequest{
				KeyID:       "test-key",
				Name:        nil,
				Description: nil,
				Tags:        nil,
				RemoveTags:  nil,
			},
			wantErr:     true,
			errContains: "at least one field must be updated",
		},
		{
			name: "update name only returns no error",
			req: &UpdateKeyMetadataRequest{
				KeyID: "test-key",
				Name:  strPtr("new-name"),
			},
			wantErr: false,
		},
		{
			name: "update description only returns no error",
			req: &UpdateKeyMetadataRequest{
				KeyID:       "test-key",
				Description: strPtr("new description"),
			},
			wantErr: false,
		},
		{
			name: "add tags returns no error",
			req: &UpdateKeyMetadataRequest{
				KeyID: "test-key",
				Tags:  map[string]string{"env": "prod"},
			},
			wantErr: false,
		},
		{
			name: "remove tags returns no error",
			req: &UpdateKeyMetadataRequest{
				KeyID:      "test-key",
				RemoveTags: []string{"old-tag"},
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			k := newTestKMSServiceForValidation()
			err := k.validateUpdateKeyMetadataRequest(tt.req)
			if tt.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.errContains)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestKMSService_ValidateSetKeyPolicyRequest(t *testing.T) {
	tests := []struct {
		name        string
		req         *SetKeyPolicyRequest
		wantErr     bool
		errContains string
	}{
		{
			name:        "nil request returns error",
			req:         nil,
			wantErr:     true,
			errContains: "request is nil",
		},
		{
			name:        "empty key ID returns error",
			req:         &SetKeyPolicyRequest{KeyID: ""},
			wantErr:     true,
			errContains: "key ID is required",
		},
		{
			name: "nil policy returns error",
			req: &SetKeyPolicyRequest{
				KeyID:  "test-key",
				Policy: nil,
			},
			wantErr:     true,
			errContains: "policy is required",
		},
		{
			// Empty AllowedOperations is now valid: an empty slice means "permit all
			// operations for any principal passing AllowedPrincipals checks".
			name: "empty allowed operations returns no error",
			req: &SetKeyPolicyRequest{
				KeyID: "test-key",
				Policy: &KeyPolicy{
					AllowedOperations: []KeyUsage{},
				},
			},
			wantErr: false,
		},
		{
			name: "valid request returns no error",
			req: &SetKeyPolicyRequest{
				KeyID: "test-key",
				Policy: &KeyPolicy{
					AllowedOperations: []KeyUsage{KeyUsageEncrypt, KeyUsageDecrypt},
				},
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			k := newTestKMSServiceForValidation()
			err := k.validateSetKeyPolicyRequest(tt.req)
			if tt.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.errContains)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestKMSService_ValidateGetKeyPolicyRequest(t *testing.T) {
	tests := []struct {
		name        string
		req         *GetKeyPolicyRequest
		wantErr     bool
		errContains string
	}{
		{
			name:        "nil request returns error",
			req:         nil,
			wantErr:     true,
			errContains: "request is nil",
		},
		{
			name:        "empty key ID returns error",
			req:         &GetKeyPolicyRequest{KeyID: ""},
			wantErr:     true,
			errContains: "key ID is required",
		},
		{
			name:    "valid request returns no error",
			req:     &GetKeyPolicyRequest{KeyID: "test-key-id"},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			k := newTestKMSServiceForValidation()
			err := k.validateGetKeyPolicyRequest(tt.req)
			if tt.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.errContains)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestKMSService_ValidateUpdateKeyStatusRequest(t *testing.T) {
	tests := []struct {
		name        string
		req         *UpdateKeyStatusRequest
		wantErr     bool
		errContains string
	}{
		{
			name:        "nil request returns error",
			req:         nil,
			wantErr:     true,
			errContains: "request is nil",
		},
		{
			name:        "empty key ID returns error",
			req:         &UpdateKeyStatusRequest{KeyID: ""},
			wantErr:     true,
			errContains: "key ID is required",
		},
		{
			name: "enabled status is valid",
			req: &UpdateKeyStatusRequest{
				KeyID:  "test-key",
				Status: KeyStatusEnabled,
			},
			wantErr: false,
		},
		{
			name: "disabled status is valid",
			req: &UpdateKeyStatusRequest{
				KeyID:  "test-key",
				Status: KeyStatusDisabled,
			},
			wantErr: false,
		},
		{
			name: "pending_deletion status returns error",
			req: &UpdateKeyStatusRequest{
				KeyID:  "test-key",
				Status: KeyStatusPendingDeletion,
			},
			wantErr:     true,
			errContains: "use dedicated operations for deletion",
		},
		{
			name: "deleted status returns error",
			req: &UpdateKeyStatusRequest{
				KeyID:  "test-key",
				Status: KeyStatusDeleted,
			},
			wantErr:     true,
			errContains: "use dedicated operations for deletion",
		},
		{
			name: "rotating status returns error",
			req: &UpdateKeyStatusRequest{
				KeyID:  "test-key",
				Status: KeyStatusRotating,
			},
			wantErr:     true,
			errContains: "use dedicated operations",
		},
		{
			name: "compromised status returns error",
			req: &UpdateKeyStatusRequest{
				KeyID:  "test-key",
				Status: KeyStatusCompromised,
			},
			wantErr:     true,
			errContains: "use dedicated operations",
		},
		{
			name: "unknown status returns error",
			req: &UpdateKeyStatusRequest{
				KeyID:  "test-key",
				Status: KeyStatus("unknown"),
			},
			wantErr:     true,
			errContains: "invalid status value",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			k := newTestKMSServiceForValidation()
			err := k.validateUpdateKeyStatusRequest(tt.req)
			if tt.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.errContains)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestKMSService_ValidateListKeyVersionsRequest(t *testing.T) {
	tests := []struct {
		name        string
		req         *ListKeyVersionsRequest
		wantErr     bool
		errContains string
	}{
		{
			name:        "nil request returns error",
			req:         nil,
			wantErr:     true,
			errContains: "request is nil",
		},
		{
			name:        "empty key ID returns error",
			req:         &ListKeyVersionsRequest{KeyID: ""},
			wantErr:     true,
			errContains: "key ID is required",
		},
		{
			name:    "valid request returns no error",
			req:     &ListKeyVersionsRequest{KeyID: "test-key-id"},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			k := newTestKMSServiceForValidation()
			err := k.validateListKeyVersionsRequest(tt.req)
			if tt.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.errContains)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestKMSService_VersionStatusTracking(t *testing.T) {
	t.Run("get version status returns empty for unset version", func(t *testing.T) {
		k := newTestKMSServiceForValidation()
		status, disabledAt := k.getVersionStatus(nil, "test-key", 1)
		assert.Equal(t, KeyStatus(""), status)
		assert.Nil(t, disabledAt)
	})

	t.Run("set and get version status", func(t *testing.T) {
		k := newTestKMSServiceForValidation()

		// Set version as disabled
		k.setVersionStatus("test-key", 1, KeyStatusDisabled, nil)

		status, disabledAt := k.getVersionStatus(nil, "test-key", 1)
		assert.Equal(t, KeyStatusDisabled, status)
		assert.Nil(t, disabledAt)
	})

	t.Run("different versions have independent statuses", func(t *testing.T) {
		k := newTestKMSServiceForValidation()

		k.setVersionStatus("test-key", 1, KeyStatusDisabled, nil)
		k.setVersionStatus("test-key", 2, KeyStatusEnabled, nil)

		status1, _ := k.getVersionStatus(nil, "test-key", 1)
		status2, _ := k.getVersionStatus(nil, "test-key", 2)

		assert.Equal(t, KeyStatusDisabled, status1)
		assert.Equal(t, KeyStatusEnabled, status2)
	})

	t.Run("different keys have independent statuses", func(t *testing.T) {
		k := newTestKMSServiceForValidation()

		k.setVersionStatus("key-a", 1, KeyStatusDisabled, nil)
		k.setVersionStatus("key-b", 1, KeyStatusEnabled, nil)

		statusA, _ := k.getVersionStatus(nil, "key-a", 1)
		statusB, _ := k.getVersionStatus(nil, "key-b", 1)

		assert.Equal(t, KeyStatusDisabled, statusA)
		assert.Equal(t, KeyStatusEnabled, statusB)
	})
}

func TestKMSValidationErrorTypes(t *testing.T) {
	k := newTestKMSServiceForValidation()

	t.Run("nil request returns ErrInvalidInput", func(t *testing.T) {
		err := k.validateGenerateKeyRequest(nil)
		require.Error(t, err)

		var qbErr *quicbaasErrors.QBError
		require.ErrorAs(t, err, &qbErr)
		assert.Equal(t, quicbaasErrors.ErrInvalidInput, qbErr.Code)
	})

	t.Run("duplicate key returns ErrAlreadyExists", func(t *testing.T) {
		k.keyNames.Store("duplicate-key", "some-id")
		_, err := k.GenerateKey(systemTenantCtx(), &GenerateKeyRequest{
			Name: "duplicate-key",
			KeyAttributes: &xkmssdk.KeyAttributes{
				SymmetricAlgorithm: xkmssdk.SymmetricAES256GCM,
			},
			Usage: []KeyUsage{KeyUsageEncrypt},
		})
		require.Error(t, err)

		var qbErr *quicbaasErrors.QBError
		require.ErrorAs(t, err, &qbErr)
		assert.Equal(t, quicbaasErrors.ErrAlreadyExists, qbErr.Code)
	})
}

// Signer returns a crypto.Signer for the given keyID. Tests that don't
// exercise the signer path receive a nil signer and no error; tests
// that do can stub this by redefining the method on a wrapped type.
func (m *mockKMSManager) Signer(_ context.Context, _ string) (crypto.Signer, error) {
	return nil, nil
}
