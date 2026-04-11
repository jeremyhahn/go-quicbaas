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

package catalog

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"errors"
	"testing"
	"time"

	xkmssdk "github.com/jeremyhahn/go-xkms/sdk/go"

	"github.com/jeremyhahn/go-quicbaas/pkg/config"
	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
)

// dekProviderMockKMSManager implements interfaces.KMSManager for testing.
type dekProviderMockKMSManager struct {
	getKeyFunc          func(ctx context.Context, keyID string) (interface{}, error)
	encryptFunc         func(ctx context.Context, keyID string, plaintext, aad []byte) ([]byte, error)
	decryptFunc         func(ctx context.Context, keyID string, ciphertext, aad []byte) ([]byte, error)
	generateDataKeyFunc func(ctx context.Context, attrs *xkmssdk.KeyAttributes, keyBytes int) ([]byte, []byte, error)
	getKeyError         error
	keyMetadata         interface{}
	getKeyCalled        bool
	lastKeyID           string
}

func (m *dekProviderMockKMSManager) GenerateKey(ctx context.Context, attrs *xkmssdk.KeyAttributes) (interface{}, error) {
	return nil, nil
}

func (m *dekProviderMockKMSManager) ImportKey(ctx context.Context, keyID string, material []byte, algorithm string) (interface{}, error) {
	return nil, nil
}

func (m *dekProviderMockKMSManager) GetKey(ctx context.Context, keyID string) (interface{}, error) {
	m.getKeyCalled = true
	m.lastKeyID = keyID

	if m.getKeyFunc != nil {
		return m.getKeyFunc(ctx, keyID)
	}

	if m.getKeyError != nil {
		return nil, m.getKeyError
	}

	if m.keyMetadata != nil {
		return m.keyMetadata, nil
	}

	// Return a default map-based metadata
	return map[string]interface{}{
		"key_id":  keyID,
		"version": uint64(1),
		"status":  "active",
	}, nil
}

func (m *dekProviderMockKMSManager) GetPublicKey(ctx context.Context, keyID string) ([]byte, error) {
	return nil, nil
}

func (m *dekProviderMockKMSManager) UpdateStoredMetadata(metadata interface{}) error {
	return nil
}

func (m *dekProviderMockKMSManager) ListKeys(ctx context.Context, prefix string) ([]interface{}, error) {
	return nil, nil
}

func (m *dekProviderMockKMSManager) RotateKey(ctx context.Context, keyID string) (interface{}, error) {
	return nil, nil
}

func (m *dekProviderMockKMSManager) RevokeKey(ctx context.Context, keyID string) error {
	return nil
}

func (m *dekProviderMockKMSManager) EnableKey(ctx context.Context, keyID string) error {
	return nil
}

func (m *dekProviderMockKMSManager) DisableKey(ctx context.Context, keyID string) error {
	return nil
}

func (m *dekProviderMockKMSManager) DeleteKey(ctx context.Context, keyID string) error {
	return nil
}

func (m *dekProviderMockKMSManager) Encrypt(ctx context.Context, keyID string, plaintext, aad []byte) ([]byte, error) {
	if m.encryptFunc != nil {
		return m.encryptFunc(ctx, keyID, plaintext, aad)
	}
	return plaintext, nil
}

func (m *dekProviderMockKMSManager) Decrypt(ctx context.Context, keyID string, ciphertext, aad []byte) ([]byte, error) {
	if m.decryptFunc != nil {
		return m.decryptFunc(ctx, keyID, ciphertext, aad)
	}
	return ciphertext, nil
}

func (m *dekProviderMockKMSManager) Sign(ctx context.Context, keyID string, digest []byte, algorithm string) ([]byte, error) {
	return nil, nil
}

func (m *dekProviderMockKMSManager) Verify(ctx context.Context, keyID string, digest, signature []byte, algorithm string) (bool, error) {
	return true, nil
}

func (m *dekProviderMockKMSManager) GenerateDataKey(ctx context.Context, attrs *xkmssdk.KeyAttributes, keyBytes int) (plaintext, wrapped []byte, err error) {
	if m.generateDataKeyFunc != nil {
		return m.generateDataKeyFunc(ctx, attrs, keyBytes)
	}
	// Default: generate random plaintext DEK and a deterministic wrapped key
	plaintext = make([]byte, keyBytes)
	if _, err := rand.Read(plaintext); err != nil {
		return nil, nil, err
	}
	// For testing, the "wrapped" key is a SHA-256 hash of the plaintext
	h := sha256.Sum256(plaintext)
	wrapped = h[:]
	return plaintext, wrapped, nil
}

func (m *dekProviderMockKMSManager) GetBackendType() config.BackendType {
	return config.BackendTypeSoftware
}

func (m *dekProviderMockKMSManager) IsAvailable(ctx context.Context) bool {
	return true
}

func (m *dekProviderMockKMSManager) GetSupportedAlgorithms(ctx context.Context) ([]string, error) {
	return []string{"AES-256-GCM"}, nil
}

func (m *dekProviderMockKMSManager) Close() error {
	return nil
}

// dekProviderMockLogger implements interfaces.Logger for testing.
type dekProviderMockLogger struct {
	debugCalled bool
	fields      map[string]interface{}
}

func (m *dekProviderMockLogger) Debug(msg string, args ...any)                          { m.debugCalled = true }
func (m *dekProviderMockLogger) Debugf(format string, args ...interface{}) {}
func (m *dekProviderMockLogger) Info(msg string, args ...any)                           {}
func (m *dekProviderMockLogger) Infof(format string, args ...interface{})  {}
func (m *dekProviderMockLogger) Warn(msg string, args ...any)                           {}
func (m *dekProviderMockLogger) Warnf(format string, args ...interface{})  {}
func (m *dekProviderMockLogger) Error(msg string, args ...any)                          {}
func (m *dekProviderMockLogger) Errorf(format string, args ...interface{}) {}
func (m *dekProviderMockLogger) Fatal(msg string)                          {}
func (m *dekProviderMockLogger) Fatalf(format string, args ...interface{}) {}
func (m *dekProviderMockLogger) WithField(key string, value interface{}) interfaces.Logger {
	return m
}
func (m *dekProviderMockLogger) WithFields(fields map[string]interface{}) interfaces.Logger {
	m.fields = fields
	return m
}
func (m *dekProviderMockLogger) WithError(err error) interfaces.Logger {
	return m
}

// Tests for NewKMSDEKProvider

func TestNewKMSDEKProvider_Success(t *testing.T) {
	kms := &dekProviderMockKMSManager{}
	logger := &dekProviderMockLogger{}

	provider, err := NewKMSDEKProvider(&KMSDEKProviderConfig{
		KMSManager: kms,
		Logger:     logger,
	})

	if err != nil {
		t.Fatalf("Expected no error, got: %v", err)
	}

	if provider == nil {
		t.Fatal("Expected provider to be non-nil")
	}
}

func TestNewKMSDEKProvider_NilConfig(t *testing.T) {
	_, err := NewKMSDEKProvider(nil)

	if err == nil {
		t.Fatal("Expected error for nil config")
	}

	var providerErr *KMSDEKProviderError
	if !errors.As(err, &providerErr) {
		t.Fatalf("Expected KMSDEKProviderError, got: %T", err)
	}

	if providerErr.Op != "NewKMSDEKProvider" {
		t.Errorf("Expected Op 'NewKMSDEKProvider', got: %s", providerErr.Op)
	}
}

func TestNewKMSDEKProvider_NilKMSManager(t *testing.T) {
	_, err := NewKMSDEKProvider(&KMSDEKProviderConfig{
		KMSManager: nil,
	})

	if err == nil {
		t.Fatal("Expected error for nil KMS manager")
	}

	var providerErr *KMSDEKProviderError
	if !errors.As(err, &providerErr) {
		t.Fatalf("Expected KMSDEKProviderError, got: %T", err)
	}

	if !errors.Is(providerErr.Err, ErrKMSManagerNil) {
		t.Errorf("Expected ErrKMSManagerNil, got: %v", providerErr.Err)
	}
}

func TestNewKMSDEKProvider_WithoutLogger(t *testing.T) {
	kms := &dekProviderMockKMSManager{}

	provider, err := NewKMSDEKProvider(&KMSDEKProviderConfig{
		KMSManager: kms,
		Logger:     nil,
	})

	if err != nil {
		t.Fatalf("Expected no error, got: %v", err)
	}

	if provider == nil {
		t.Fatal("Expected provider to be non-nil")
	}

	if provider.logger != nil {
		t.Error("Expected logger to be nil")
	}
}

// Tests for GetDEK

func TestGetDEK_Success_GenerateNewDEK(t *testing.T) {
	kms := &dekProviderMockKMSManager{
		keyMetadata: map[string]interface{}{
			"key_id":  "test/key/v1",
			"version": uint64(2),
			"status":  "active",
		},
	}

	provider, err := NewKMSDEKProvider(&KMSDEKProviderConfig{
		KMSManager: kms,
	})
	if err != nil {
		t.Fatalf("Failed to create provider: %v", err)
	}

	ctx := context.Background()
	resp, err := provider.GetDEK(ctx, &GetDEKRequest{
		Path: "test/key/v1",
	})

	if err != nil {
		t.Fatalf("Expected no error, got: %v", err)
	}

	if resp == nil {
		t.Fatal("Expected response to be non-nil")
	}

	if resp.DEK == nil {
		t.Fatal("Expected DEK to be non-nil")
	}

	if resp.DEK.Path != "test/key/v1" {
		t.Errorf("Expected path 'test/key/v1', got: %s", resp.DEK.Path)
	}

	if resp.DEK.Version != 2 {
		t.Errorf("Expected version 2, got: %d", resp.DEK.Version)
	}

	if resp.DEK.Status != DEKStatusActive {
		t.Errorf("Expected status 'active', got: %s", resp.DEK.Status)
	}

	if resp.Timestamp.IsZero() {
		t.Error("Expected timestamp to be set")
	}

	if len(resp.KeyMaterial) != defaultDEKSizeBytes {
		t.Errorf("Expected key material of %d bytes, got: %d", defaultDEKSizeBytes, len(resp.KeyMaterial))
	}

	if len(resp.WrappedKey) == 0 {
		t.Error("Expected non-empty wrapped key")
	}

	if !kms.getKeyCalled {
		t.Error("Expected GetKey to be called on KMS")
	}

	if kms.lastKeyID != "test/key/v1" {
		t.Errorf("Expected last key ID 'test/key/v1', got: %s", kms.lastKeyID)
	}
}

func TestGetDEK_Success_UnwrapExistingDEK(t *testing.T) {
	wrappedKey := []byte("wrapped-dek-material-for-test")
	expectedPlaintext := []byte("unwrapped-plaintext-32-byte-key!")

	kms := &dekProviderMockKMSManager{
		keyMetadata: map[string]interface{}{
			"key_id":  "test/key/v1",
			"version": uint64(2),
			"status":  "active",
		},
		decryptFunc: func(ctx context.Context, keyID string, ciphertext, aad []byte) ([]byte, error) {
			if keyID != "test/key/v1" {
				t.Errorf("Expected keyID 'test/key/v1', got: %s", keyID)
			}
			return expectedPlaintext, nil
		},
	}

	provider, err := NewKMSDEKProvider(&KMSDEKProviderConfig{
		KMSManager: kms,
	})
	if err != nil {
		t.Fatalf("Failed to create provider: %v", err)
	}

	ctx := context.Background()
	resp, err := provider.GetDEK(ctx, &GetDEKRequest{
		Path:       "test/key/v1",
		WrappedKey: wrappedKey,
	})

	if err != nil {
		t.Fatalf("Expected no error, got: %v", err)
	}

	if resp == nil {
		t.Fatal("Expected response to be non-nil")
	}

	if string(resp.KeyMaterial) != string(expectedPlaintext) {
		t.Errorf("Expected key material to match unwrapped plaintext")
	}

	if string(resp.WrappedKey) != string(wrappedKey) {
		t.Errorf("Expected wrapped key to be preserved in response")
	}
}

func TestGetDEK_NilRequest(t *testing.T) {
	kms := &dekProviderMockKMSManager{}

	provider, err := NewKMSDEKProvider(&KMSDEKProviderConfig{
		KMSManager: kms,
	})
	if err != nil {
		t.Fatalf("Failed to create provider: %v", err)
	}

	ctx := context.Background()
	_, err = provider.GetDEK(ctx, nil)

	if err == nil {
		t.Fatal("Expected error for nil request")
	}

	var providerErr *KMSDEKProviderError
	if !errors.As(err, &providerErr) {
		t.Fatalf("Expected KMSDEKProviderError, got: %T", err)
	}

	if providerErr.Op != "GetDEK" {
		t.Errorf("Expected Op 'GetDEK', got: %s", providerErr.Op)
	}
}

func TestGetDEK_EmptyPath(t *testing.T) {
	kms := &dekProviderMockKMSManager{}

	provider, err := NewKMSDEKProvider(&KMSDEKProviderConfig{
		KMSManager: kms,
	})
	if err != nil {
		t.Fatalf("Failed to create provider: %v", err)
	}

	ctx := context.Background()
	_, err = provider.GetDEK(ctx, &GetDEKRequest{
		Path: "",
	})

	if err == nil {
		t.Fatal("Expected error for empty path")
	}

	var providerErr *KMSDEKProviderError
	if !errors.As(err, &providerErr) {
		t.Fatalf("Expected KMSDEKProviderError, got: %T", err)
	}

	if !errors.Is(providerErr.Err, ErrKeyPathEmpty) {
		t.Errorf("Expected ErrKeyPathEmpty, got: %v", providerErr.Err)
	}
}

func TestGetDEK_ContextCanceled(t *testing.T) {
	kms := &dekProviderMockKMSManager{}

	provider, err := NewKMSDEKProvider(&KMSDEKProviderConfig{
		KMSManager: kms,
	})
	if err != nil {
		t.Fatalf("Failed to create provider: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	_, err = provider.GetDEK(ctx, &GetDEKRequest{
		Path: "test/key",
	})

	if err == nil {
		t.Fatal("Expected error for canceled context")
	}

	var providerErr *KMSDEKProviderError
	if !errors.As(err, &providerErr) {
		t.Fatalf("Expected KMSDEKProviderError, got: %T", err)
	}

	if !errors.Is(providerErr.Err, context.Canceled) {
		t.Errorf("Expected context.Canceled, got: %v", providerErr.Err)
	}
}

func TestGetDEK_KMSError(t *testing.T) {
	expectedErr := errors.New("KMS unavailable")
	kms := &dekProviderMockKMSManager{
		getKeyError: expectedErr,
	}

	provider, err := NewKMSDEKProvider(&KMSDEKProviderConfig{
		KMSManager: kms,
	})
	if err != nil {
		t.Fatalf("Failed to create provider: %v", err)
	}

	ctx := context.Background()
	_, err = provider.GetDEK(ctx, &GetDEKRequest{
		Path: "test/key",
	})

	if err == nil {
		t.Fatal("Expected error from KMS")
	}

	var providerErr *KMSDEKProviderError
	if !errors.As(err, &providerErr) {
		t.Fatalf("Expected KMSDEKProviderError, got: %T", err)
	}

	if !errors.Is(providerErr.Err, expectedErr) {
		t.Errorf("Expected underlying error, got: %v", providerErr.Err)
	}

	if providerErr.KeyPath != "test/key" {
		t.Errorf("Expected key path 'test/key', got: %s", providerErr.KeyPath)
	}
}

func TestGetDEK_NilKeyMetadata(t *testing.T) {
	kms := &dekProviderMockKMSManager{
		getKeyFunc: func(ctx context.Context, keyID string) (interface{}, error) {
			return nil, nil
		},
	}

	provider, err := NewKMSDEKProvider(&KMSDEKProviderConfig{
		KMSManager: kms,
	})
	if err != nil {
		t.Fatalf("Failed to create provider: %v", err)
	}

	ctx := context.Background()
	_, err = provider.GetDEK(ctx, &GetDEKRequest{
		Path: "test/key",
	})

	if err == nil {
		t.Fatal("Expected error for nil key metadata")
	}

	var providerErr *KMSDEKProviderError
	if !errors.As(err, &providerErr) {
		t.Fatalf("Expected KMSDEKProviderError, got: %T", err)
	}

	if !errors.Is(providerErr.Err, ErrKeyMetadataNil) {
		t.Errorf("Expected ErrKeyMetadataNil, got: %v", providerErr.Err)
	}
}

func TestGetDEK_WithLogger(t *testing.T) {
	kms := &dekProviderMockKMSManager{}
	logger := &dekProviderMockLogger{}

	provider, err := NewKMSDEKProvider(&KMSDEKProviderConfig{
		KMSManager: kms,
		Logger:     logger,
	})
	if err != nil {
		t.Fatalf("Failed to create provider: %v", err)
	}

	ctx := context.Background()
	resp, err := provider.GetDEK(ctx, &GetDEKRequest{
		Path: "test/key",
	})

	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	if !logger.debugCalled {
		t.Error("Expected logger.Debug to be called")
	}

	if len(resp.KeyMaterial) == 0 {
		t.Error("Expected non-empty key material")
	}
}

func TestGetDEK_GenerateDataKeyError(t *testing.T) {
	expectedErr := errors.New("generate data key failed")
	kms := &dekProviderMockKMSManager{
		generateDataKeyFunc: func(ctx context.Context, attrs *xkmssdk.KeyAttributes, keyBytes int) ([]byte, []byte, error) {
			return nil, nil, expectedErr
		},
	}

	provider, err := NewKMSDEKProvider(&KMSDEKProviderConfig{
		KMSManager: kms,
	})
	if err != nil {
		t.Fatalf("Failed to create provider: %v", err)
	}

	ctx := context.Background()
	_, err = provider.GetDEK(ctx, &GetDEKRequest{
		Path: "test/key",
	})

	if err == nil {
		t.Fatal("Expected error from GenerateDataKey failure")
	}

	var providerErr *KMSDEKProviderError
	if !errors.As(err, &providerErr) {
		t.Fatalf("Expected KMSDEKProviderError, got: %T", err)
	}

	if providerErr.Op != "GetDEK.GenerateDataKey" {
		t.Errorf("Expected Op 'GetDEK.GenerateDataKey', got: %s", providerErr.Op)
	}
}

func TestGetDEK_GenerateDataKeyEmptyResult(t *testing.T) {
	kms := &dekProviderMockKMSManager{
		generateDataKeyFunc: func(ctx context.Context, attrs *xkmssdk.KeyAttributes, keyBytes int) ([]byte, []byte, error) {
			return nil, nil, nil
		},
	}

	provider, err := NewKMSDEKProvider(&KMSDEKProviderConfig{
		KMSManager: kms,
	})
	if err != nil {
		t.Fatalf("Failed to create provider: %v", err)
	}

	ctx := context.Background()
	_, err = provider.GetDEK(ctx, &GetDEKRequest{
		Path: "test/key",
	})

	if err == nil {
		t.Fatal("Expected error for empty GenerateDataKey result")
	}

	var providerErr *KMSDEKProviderError
	if !errors.As(err, &providerErr) {
		t.Fatalf("Expected KMSDEKProviderError, got: %T", err)
	}

	if !errors.Is(providerErr.Err, ErrGenerateDataKeyFailed) {
		t.Errorf("Expected ErrGenerateDataKeyFailed, got: %v", providerErr.Err)
	}
}

func TestGetDEK_UnwrapDEKError(t *testing.T) {
	kms := &dekProviderMockKMSManager{
		decryptFunc: func(ctx context.Context, keyID string, ciphertext, aad []byte) ([]byte, error) {
			return nil, errors.New("decrypt failed")
		},
	}

	provider, err := NewKMSDEKProvider(&KMSDEKProviderConfig{
		KMSManager: kms,
	})
	if err != nil {
		t.Fatalf("Failed to create provider: %v", err)
	}

	ctx := context.Background()
	_, err = provider.GetDEK(ctx, &GetDEKRequest{
		Path:       "test/key",
		WrappedKey: []byte("some-wrapped-key"),
	})

	if err == nil {
		t.Fatal("Expected error from unwrap failure")
	}

	var providerErr *KMSDEKProviderError
	if !errors.As(err, &providerErr) {
		t.Fatalf("Expected KMSDEKProviderError, got: %T", err)
	}

	if providerErr.Op != "GetDEK.UnwrapDEK" {
		t.Errorf("Expected Op 'GetDEK.UnwrapDEK', got: %s", providerErr.Op)
	}

	if !errors.Is(providerErr.Err, ErrUnwrapDEKFailed) {
		t.Errorf("Expected ErrUnwrapDEKFailed, got: %v", providerErr.Err)
	}
}

// Tests for convertToDEKMetadata with various metadata types

func TestConvertToDEKMetadata_MapMetadata(t *testing.T) {
	kms := &dekProviderMockKMSManager{}

	provider, err := NewKMSDEKProvider(&KMSDEKProviderConfig{
		KMSManager: kms,
	})
	if err != nil {
		t.Fatalf("Failed to create provider: %v", err)
	}

	testCases := []struct {
		name           string
		metadata       map[string]interface{}
		expectedStatus DEKStatus
		expectedVer    uint64
	}{
		{
			name: "active status lowercase",
			metadata: map[string]interface{}{
				"status":  "active",
				"version": uint64(3),
			},
			expectedStatus: DEKStatusActive,
			expectedVer:    3,
		},
		{
			name: "active status uppercase",
			metadata: map[string]interface{}{
				"status":  "ACTIVE",
				"version": int64(5),
			},
			expectedStatus: DEKStatusActive,
			expectedVer:    5,
		},
		{
			name: "enabled status",
			metadata: map[string]interface{}{
				"status":  "enabled",
				"version": int(7),
			},
			expectedStatus: DEKStatusActive,
			expectedVer:    7,
		},
		{
			name: "disabled status",
			metadata: map[string]interface{}{
				"status":  "disabled",
				"version": float64(10),
			},
			expectedStatus: DEKStatusDisabled,
			expectedVer:    10,
		},
		{
			name: "rotating status",
			metadata: map[string]interface{}{
				"status":  "ROTATING",
				"version": uint64(1),
			},
			expectedStatus: DEKStatusRotating,
			expectedVer:    1,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			dek := provider.convertToDEKMetadata("test/path", tc.metadata)

			if dek.Path != "test/path" {
				t.Errorf("Expected path 'test/path', got: %s", dek.Path)
			}

			if dek.Status != tc.expectedStatus {
				t.Errorf("Expected status %v, got: %v", tc.expectedStatus, dek.Status)
			}

			if dek.Version != tc.expectedVer {
				t.Errorf("Expected version %d, got: %d", tc.expectedVer, dek.Version)
			}
		})
	}
}

func TestConvertToDEKMetadata_WithTimestamps(t *testing.T) {
	kms := &dekProviderMockKMSManager{}

	provider, err := NewKMSDEKProvider(&KMSDEKProviderConfig{
		KMSManager: kms,
	})
	if err != nil {
		t.Fatalf("Failed to create provider: %v", err)
	}

	createdAt := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	rotatedAt := time.Date(2025, 1, 5, 0, 0, 0, 0, time.UTC)

	metadata := map[string]interface{}{
		"status":     "active",
		"version":    uint64(2),
		"created_at": createdAt,
		"rotated_at": rotatedAt,
	}

	dek := provider.convertToDEKMetadata("test/path", metadata)

	if !dek.CreatedAt.Equal(createdAt) {
		t.Errorf("Expected createdAt %v, got: %v", createdAt, dek.CreatedAt)
	}

	if !dek.RotatedAt.Equal(rotatedAt) {
		t.Errorf("Expected rotatedAt %v, got: %v", rotatedAt, dek.RotatedAt)
	}
}

func TestConvertToDEKMetadata_NonMapMetadata(t *testing.T) {
	kms := &dekProviderMockKMSManager{}

	provider, err := NewKMSDEKProvider(&KMSDEKProviderConfig{
		KMSManager: kms,
	})
	if err != nil {
		t.Fatalf("Failed to create provider: %v", err)
	}

	// Test with a non-map type (should use defaults)
	dek := provider.convertToDEKMetadata("test/path", "string metadata")

	if dek.Path != "test/path" {
		t.Errorf("Expected path 'test/path', got: %s", dek.Path)
	}

	if dek.Version != 1 {
		t.Errorf("Expected default version 1, got: %d", dek.Version)
	}

	if dek.Status != DEKStatusActive {
		t.Errorf("Expected default status 'active', got: %s", dek.Status)
	}
}

// Test error formatting

func TestKMSDEKProviderError_Error(t *testing.T) {
	testCases := []struct {
		name     string
		err      *KMSDEKProviderError
		expected string
	}{
		{
			name: "with key path",
			err: &KMSDEKProviderError{
				Op:      "GetDEK",
				KeyPath: "test/key",
				Err:     errors.New("some error"),
			},
			expected: "kms dek provider: GetDEK key=test/key: some error",
		},
		{
			name: "without key path",
			err: &KMSDEKProviderError{
				Op:  "NewKMSDEKProvider",
				Err: errors.New("config error"),
			},
			expected: "kms dek provider: NewKMSDEKProvider: config error",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.err.Error(); got != tc.expected {
				t.Errorf("Expected error message '%s', got: '%s'", tc.expected, got)
			}
		})
	}
}

func TestKMSDEKProviderError_Unwrap(t *testing.T) {
	underlying := errors.New("underlying error")
	err := &KMSDEKProviderError{
		Op:  "GetDEK",
		Err: underlying,
	}

	if !errors.Is(err, underlying) {
		t.Error("Expected Unwrap to return underlying error")
	}
}

// Test interface compliance

func TestKMSDEKProvider_ImplementsDEKProvider(t *testing.T) {
	var _ DEKProvider = (*KMSDEKProvider)(nil)
}
