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

package security

import (
	"context"
	"crypto/rand"
	"errors"
	"io"
	"math"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	dberrors "github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
)

// mockTenantDEKManagerForVectors implements TenantDEKManager for vector encryption testing.
type mockTenantDEKManagerForVectors struct {
	mu            sync.RWMutex
	tenantDEKs    map[string][]byte
	failGetDEK    bool
	failCreateDEK bool
	closed        bool
}

func newMockTenantDEKManagerForVectors() *mockTenantDEKManagerForVectors {
	return &mockTenantDEKManagerForVectors{
		tenantDEKs: make(map[string][]byte),
	}
}

func (m *mockTenantDEKManagerForVectors) GetOrCreateTenantDEK(ctx context.Context, tenantID string) ([]byte, error) {
	if m.failCreateDEK {
		return nil, &dberrors.QBError{
			Code: dberrors.ErrInternal,
			Op:   "mockTenantDEKManager.GetOrCreateTenantDEK",
		}
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	if dek, ok := m.tenantDEKs[tenantID]; ok {
		result := make([]byte, len(dek))
		copy(result, dek)
		return result, nil
	}

	// Generate new DEK
	dek := make([]byte, 32)
	if _, err := io.ReadFull(rand.Reader, dek); err != nil {
		return nil, err
	}
	m.tenantDEKs[tenantID] = dek

	result := make([]byte, len(dek))
	copy(result, dek)
	return result, nil
}

func (m *mockTenantDEKManagerForVectors) GetTenantDEK(ctx context.Context, tenantID string) ([]byte, error) {
	if m.failGetDEK {
		return nil, &dberrors.QBError{
			Code: dberrors.ErrInternal,
			Op:   "mockTenantDEKManager.GetTenantDEK",
		}
	}

	m.mu.RLock()
	defer m.mu.RUnlock()

	dek, ok := m.tenantDEKs[tenantID]
	if !ok {
		return nil, &dberrors.QBError{
			Code: dberrors.ErrNotFound,
			Op:   "mockTenantDEKManager.GetTenantDEK",
		}
	}

	result := make([]byte, len(dek))
	copy(result, dek)
	return result, nil
}

func (m *mockTenantDEKManagerForVectors) RotateTenantDEK(ctx context.Context, tenantID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	dek := make([]byte, 32)
	if _, err := io.ReadFull(rand.Reader, dek); err != nil {
		return err
	}
	m.tenantDEKs[tenantID] = dek
	return nil
}

func (m *mockTenantDEKManagerForVectors) DeleteTenantDEK(ctx context.Context, tenantID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.tenantDEKs, tenantID)
	return nil
}

func (m *mockTenantDEKManagerForVectors) GetTenantDEKStatus(ctx context.Context, tenantID string) (*TenantDEKStatus, error) {
	return nil, nil
}

func (m *mockTenantDEKManagerForVectors) ListTenantDEKs(ctx context.Context) ([]string, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	result := make([]string, 0, len(m.tenantDEKs))
	for id := range m.tenantDEKs {
		result = append(result, id)
	}
	return result, nil
}

func (m *mockTenantDEKManagerForVectors) Close() error {
	m.closed = true
	return nil
}

// vectorTestLogger implements interfaces.Logger for testing.
type vectorTestLogger struct {
	debugMessages []string
	infoMessages  []string
	warnMessages  []string
	errorMessages []string
	fields        map[string]interface{}
	mu            sync.Mutex
}

func newVectorTestLogger() *vectorTestLogger {
	return &vectorTestLogger{
		fields: make(map[string]interface{}),
	}
}

func (l *vectorTestLogger) Debug(msg string, args ...any) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.debugMessages = append(l.debugMessages, msg)
}

func (l *vectorTestLogger) Debugf(format string, args ...interface{}) {
	l.Debug(format)
}

func (l *vectorTestLogger) Info(msg string, args ...any) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.infoMessages = append(l.infoMessages, msg)
}

func (l *vectorTestLogger) Infof(format string, args ...interface{}) {
	l.Info(format)
}

func (l *vectorTestLogger) Warn(msg string, args ...any) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.warnMessages = append(l.warnMessages, msg)
}

func (l *vectorTestLogger) Warnf(format string, args ...interface{}) {
	l.Warn(format)
}

func (l *vectorTestLogger) Error(msg string, args ...any) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.errorMessages = append(l.errorMessages, msg)
}

func (l *vectorTestLogger) Errorf(format string, args ...interface{}) {
	l.Error(format)
}

func (l *vectorTestLogger) Fatal(msg string) {}

func (l *vectorTestLogger) Fatalf(format string, args ...interface{}) {}

func (l *vectorTestLogger) WithField(key string, value interface{}) interfaces.Logger {
	l.mu.Lock()
	defer l.mu.Unlock()
	newLogger := &vectorTestLogger{
		fields: make(map[string]interface{}),
	}
	for k, v := range l.fields {
		newLogger.fields[k] = v
	}
	newLogger.fields[key] = value
	return newLogger
}

func (l *vectorTestLogger) WithFields(fields map[string]interface{}) interfaces.Logger {
	l.mu.Lock()
	defer l.mu.Unlock()
	newLogger := &vectorTestLogger{
		fields: make(map[string]interface{}),
	}
	for k, v := range l.fields {
		newLogger.fields[k] = v
	}
	for k, v := range fields {
		newLogger.fields[k] = v
	}
	return newLogger
}

func (l *vectorTestLogger) WithError(err error) interfaces.Logger {
	return l.WithField("error", err)
}

// Helper function to create test setup.
func setupVectorEncryptionTest(t *testing.T) (*mockTenantDEKManagerForVectors, *vectorTestLogger, VectorEncryptionService) {
	t.Helper()

	dekMgr := newMockTenantDEKManagerForVectors()
	logger := newVectorTestLogger()

	svc, err := NewVectorEncryptionService(dekMgr, logger, nil)
	require.NoError(t, err)
	require.NotNil(t, svc)

	return dekMgr, logger, svc
}

// TestNewVectorEncryptionService tests service creation.
func TestNewVectorEncryptionService(t *testing.T) {
	t.Run("success with valid parameters", func(t *testing.T) {
		dekMgr := newMockTenantDEKManagerForVectors()
		logger := newVectorTestLogger()

		svc, err := NewVectorEncryptionService(dekMgr, logger, nil)
		require.NoError(t, err)
		require.NotNil(t, svc)
	})

	t.Run("failure with nil DEK manager", func(t *testing.T) {
		logger := newVectorTestLogger()

		svc, err := NewVectorEncryptionService(nil, logger, nil)
		require.Error(t, err)
		require.Nil(t, svc)
		assert.True(t, errors.Is(err, ErrNilTenantDEKManager))
	})

	t.Run("failure with nil logger", func(t *testing.T) {
		dekMgr := newMockTenantDEKManagerForVectors()

		svc, err := NewVectorEncryptionService(dekMgr, nil, nil)
		require.Error(t, err)
		require.Nil(t, svc)
		assert.True(t, errors.Is(err, ErrNilLogger))
	})
}

// TestEncryptDecryptRoundtrip tests basic encrypt/decrypt operations.
func TestEncryptDecryptRoundtrip(t *testing.T) {
	_, _, svc := setupVectorEncryptionTest(t)
	ctx := context.Background()

	t.Run("single vector roundtrip", func(t *testing.T) {
		tenantID := "tenant-1"
		namespace := "test-ns"
		vectorID := "vec-1"
		embedding := []float32{1.0, 2.0, 3.0, 4.0, 5.0}

		// Encrypt
		encrypted, err := svc.EncryptVector(ctx, tenantID, namespace, vectorID, embedding)
		require.NoError(t, err)
		require.NotNil(t, encrypted)
		require.Greater(t, len(encrypted), vectorMinEncryptedSize)

		// Verify data is actually encrypted
		assert.Equal(t, VectorEncryptionVersion, encrypted[0])

		// Decrypt
		decrypted, err := svc.DecryptVector(ctx, tenantID, namespace, vectorID, encrypted)
		require.NoError(t, err)
		require.NotNil(t, decrypted)
		require.Len(t, decrypted, len(embedding))

		// Verify values match
		for i := range embedding {
			assert.Equal(t, embedding[i], decrypted[i])
		}
	})

	t.Run("large vector roundtrip", func(t *testing.T) {
		tenantID := "tenant-2"
		namespace := "large-ns"
		vectorID := "large-vec"
		embedding := make([]float32, 1536) // OpenAI embedding size
		for i := range embedding {
			embedding[i] = float32(i) * 0.001
		}

		// Encrypt
		encrypted, err := svc.EncryptVector(ctx, tenantID, namespace, vectorID, embedding)
		require.NoError(t, err)

		// Decrypt
		decrypted, err := svc.DecryptVector(ctx, tenantID, namespace, vectorID, encrypted)
		require.NoError(t, err)
		require.Len(t, decrypted, len(embedding))

		// Verify values match
		for i := range embedding {
			assert.InDelta(t, embedding[i], decrypted[i], 0.0001)
		}
	})

	t.Run("special float values", func(t *testing.T) {
		tenantID := "tenant-special"
		namespace := "special-ns"
		vectorID := "special-vec"
		embedding := []float32{
			0.0,
			-0.0,
			1.0,
			-1.0,
			float32(math.SmallestNonzeroFloat32),
			float32(math.MaxFloat32),
			float32(math.Inf(1)),
			float32(math.Inf(-1)),
		}

		// Encrypt
		encrypted, err := svc.EncryptVector(ctx, tenantID, namespace, vectorID, embedding)
		require.NoError(t, err)

		// Decrypt
		decrypted, err := svc.DecryptVector(ctx, tenantID, namespace, vectorID, encrypted)
		require.NoError(t, err)

		// Verify special values
		assert.Equal(t, float32(0.0), decrypted[0])
		assert.Equal(t, float32(1.0), decrypted[2])
		assert.Equal(t, float32(-1.0), decrypted[3])
		assert.True(t, math.IsInf(float64(decrypted[6]), 1))
		assert.True(t, math.IsInf(float64(decrypted[7]), -1))
	})
}

// TestVectorTenantIsolation tests that different tenants cannot decrypt each other's data.
func TestVectorTenantIsolation(t *testing.T) {
	_, _, svc := setupVectorEncryptionTest(t)
	ctx := context.Background()

	tenantA := "tenant-A"
	tenantB := "tenant-B"
	namespace := "shared-ns"
	vectorID := "vec-1"
	embedding := []float32{1.0, 2.0, 3.0, 4.0}

	// Encrypt with tenant A
	encryptedA, err := svc.EncryptVector(ctx, tenantA, namespace, vectorID, embedding)
	require.NoError(t, err)

	// Encrypt with tenant B
	encryptedB, err := svc.EncryptVector(ctx, tenantB, namespace, vectorID, embedding)
	require.NoError(t, err)

	// Verify ciphertexts are different (different keys)
	assert.NotEqual(t, encryptedA[1+vectorNonceSize:], encryptedB[1+vectorNonceSize:])

	// Tenant A can decrypt their own data
	decryptedA, err := svc.DecryptVector(ctx, tenantA, namespace, vectorID, encryptedA)
	require.NoError(t, err)
	assert.Equal(t, embedding, decryptedA)

	// Tenant B cannot decrypt tenant A's data (different DEK)
	_, err = svc.DecryptVector(ctx, tenantB, namespace, vectorID, encryptedA)
	require.Error(t, err)
}

// TestAADTamperDetection tests that tampering with AAD (namespace/tenant/vectorID) is detected.
func TestAADTamperDetection(t *testing.T) {
	_, _, svc := setupVectorEncryptionTest(t)
	ctx := context.Background()

	tenantID := "tenant-1"
	namespace := "test-ns"
	vectorID := "vec-1"
	embedding := []float32{1.0, 2.0, 3.0}

	// Encrypt
	encrypted, err := svc.EncryptVector(ctx, tenantID, namespace, vectorID, embedding)
	require.NoError(t, err)

	t.Run("wrong namespace fails", func(t *testing.T) {
		_, err := svc.DecryptVector(ctx, tenantID, "wrong-ns", vectorID, encrypted)
		require.Error(t, err)
		assert.True(t, errors.Is(err, ErrDecryptionFailed))
	})

	t.Run("wrong tenant fails", func(t *testing.T) {
		// First create DEK for wrong tenant
		_, err := svc.EncryptVector(ctx, "wrong-tenant", namespace, vectorID, embedding)
		require.NoError(t, err)

		// Now try to decrypt with wrong tenant - will fail due to different key
		_, err = svc.DecryptVector(ctx, "wrong-tenant", namespace, vectorID, encrypted)
		require.Error(t, err)
	})

	t.Run("wrong vectorID fails", func(t *testing.T) {
		_, err := svc.DecryptVector(ctx, tenantID, namespace, "wrong-vec", encrypted)
		require.Error(t, err)
		assert.True(t, errors.Is(err, ErrDecryptionFailed))
	})
}

// TestBatchOperations tests batch encrypt/decrypt.
func TestBatchOperations(t *testing.T) {
	_, _, svc := setupVectorEncryptionTest(t)
	ctx := context.Background()

	t.Run("batch encrypt decrypt", func(t *testing.T) {
		tenantID := "tenant-batch"
		namespace := "batch-ns"
		vectors := map[string][]float32{
			"vec-1": {1.0, 2.0, 3.0},
			"vec-2": {4.0, 5.0, 6.0},
			"vec-3": {7.0, 8.0, 9.0},
		}

		// Batch encrypt
		encrypted, err := svc.EncryptVectorBatch(ctx, tenantID, namespace, vectors)
		require.NoError(t, err)
		require.Len(t, encrypted, 3)

		// Batch decrypt
		decrypted, err := svc.DecryptVectorBatch(ctx, tenantID, namespace, encrypted)
		require.NoError(t, err)
		require.Len(t, decrypted, 3)

		// Verify all vectors match
		for id, original := range vectors {
			assert.Equal(t, original, decrypted[id])
		}
	})

	t.Run("empty batch returns empty map", func(t *testing.T) {
		tenantID := "tenant-empty"
		namespace := "empty-ns"

		encrypted, err := svc.EncryptVectorBatch(ctx, tenantID, namespace, map[string][]float32{})
		require.NoError(t, err)
		assert.Len(t, encrypted, 0)

		decrypted, err := svc.DecryptVectorBatch(ctx, tenantID, namespace, map[string][]byte{})
		require.NoError(t, err)
		assert.Len(t, decrypted, 0)
	})

	t.Run("batch with nil embedding skips entry", func(t *testing.T) {
		tenantID := "tenant-nil"
		namespace := "nil-ns"
		vectors := map[string][]float32{
			"vec-1": {1.0, 2.0},
			"vec-2": nil,
			"vec-3": {3.0, 4.0},
		}

		encrypted, err := svc.EncryptVectorBatch(ctx, tenantID, namespace, vectors)
		require.NoError(t, err)
		require.Len(t, encrypted, 2)
		assert.Contains(t, encrypted, "vec-1")
		assert.Contains(t, encrypted, "vec-3")
		assert.NotContains(t, encrypted, "vec-2")
	})
}

// TestIsEncrypted tests the IsEncrypted detection method.
func TestIsEncrypted(t *testing.T) {
	_, _, svc := setupVectorEncryptionTest(t)
	ctx := context.Background()

	t.Run("encrypted data returns true", func(t *testing.T) {
		encrypted, err := svc.EncryptVector(ctx, "tenant", "ns", "vec", []float32{1.0, 2.0})
		require.NoError(t, err)

		assert.True(t, svc.IsEncrypted(encrypted))
	})

	t.Run("raw float data returns false", func(t *testing.T) {
		// Raw float32 bytes (not encrypted)
		rawBytes := float32SliceToBytes([]float32{1.0, 2.0, 3.0})
		assert.False(t, svc.IsEncrypted(rawBytes))
	})

	t.Run("too short data returns false", func(t *testing.T) {
		assert.False(t, svc.IsEncrypted([]byte{1, 2, 3}))
	})

	t.Run("invalid version returns false", func(t *testing.T) {
		data := make([]byte, vectorMinEncryptedSize)
		data[0] = 99 // Invalid version
		assert.False(t, svc.IsEncrypted(data))
	})

	t.Run("nil data returns false", func(t *testing.T) {
		assert.False(t, svc.IsEncrypted(nil))
	})

	t.Run("empty data returns false", func(t *testing.T) {
		assert.False(t, svc.IsEncrypted([]byte{}))
	})
}

// TestValidationErrors tests input validation.
func TestValidationErrors(t *testing.T) {
	_, _, svc := setupVectorEncryptionTest(t)
	ctx := context.Background()

	t.Run("encrypt empty tenant ID", func(t *testing.T) {
		_, err := svc.EncryptVector(ctx, "", "ns", "vec", []float32{1.0})
		require.Error(t, err)
		assert.True(t, errors.Is(err, ErrEmptyTenantID))
	})

	t.Run("encrypt empty namespace", func(t *testing.T) {
		_, err := svc.EncryptVector(ctx, "tenant", "", "vec", []float32{1.0})
		require.Error(t, err)
		assert.True(t, errors.Is(err, ErrEmptyNamespace))
	})

	t.Run("encrypt empty vector ID", func(t *testing.T) {
		_, err := svc.EncryptVector(ctx, "tenant", "ns", "", []float32{1.0})
		require.Error(t, err)
		assert.True(t, errors.Is(err, ErrEmptyVectorID))
	})

	t.Run("encrypt nil embedding", func(t *testing.T) {
		_, err := svc.EncryptVector(ctx, "tenant", "ns", "vec", nil)
		require.Error(t, err)
		assert.True(t, errors.Is(err, ErrEmptyEmbedding))
	})

	t.Run("encrypt empty embedding", func(t *testing.T) {
		_, err := svc.EncryptVector(ctx, "tenant", "ns", "vec", []float32{})
		require.Error(t, err)
		assert.True(t, errors.Is(err, ErrEmptyEmbedding))
	})

	t.Run("decrypt empty tenant ID", func(t *testing.T) {
		validEncrypted := make([]byte, vectorMinEncryptedSize)
		validEncrypted[0] = VectorEncryptionVersion
		_, err := svc.DecryptVector(ctx, "", "ns", "vec", validEncrypted)
		require.Error(t, err)
		assert.True(t, errors.Is(err, ErrEmptyTenantID))
	})

	t.Run("decrypt nil encrypted data", func(t *testing.T) {
		_, err := svc.DecryptVector(ctx, "tenant", "ns", "vec", nil)
		require.Error(t, err)
		assert.True(t, errors.Is(err, ErrInvalidEncryptedData))
	})

	t.Run("decrypt too short encrypted data", func(t *testing.T) {
		_, err := svc.DecryptVector(ctx, "tenant", "ns", "vec", []byte{1, 2, 3})
		require.Error(t, err)
		assert.True(t, errors.Is(err, ErrInvalidEncryptedData))
	})

	t.Run("decrypt unsupported version", func(t *testing.T) {
		encrypted := make([]byte, vectorMinEncryptedSize)
		encrypted[0] = 99 // Invalid version
		_, err := svc.DecryptVector(ctx, "tenant", "ns", "vec", encrypted)
		require.Error(t, err)
		assert.True(t, errors.Is(err, ErrUnsupportedEncryptionVersion))
	})

	t.Run("batch encrypt empty tenant ID", func(t *testing.T) {
		_, err := svc.EncryptVectorBatch(ctx, "", "ns", map[string][]float32{"v": {1.0}})
		require.Error(t, err)
		assert.True(t, errors.Is(err, ErrEmptyTenantID))
	})

	t.Run("batch decrypt empty namespace", func(t *testing.T) {
		_, err := svc.DecryptVectorBatch(ctx, "tenant", "", map[string][]byte{"v": {}})
		require.Error(t, err)
		assert.True(t, errors.Is(err, ErrEmptyNamespace))
	})
}

// TestVectorDEKNotFound tests behavior when tenant DEK doesn't exist.
func TestVectorDEKNotFound(t *testing.T) {
	dekMgr := newMockTenantDEKManagerForVectors()
	logger := newVectorTestLogger()
	svc, err := NewVectorEncryptionService(dekMgr, logger, nil)
	require.NoError(t, err)

	ctx := context.Background()

	// Try to decrypt for a tenant that has no DEK
	validEncrypted := make([]byte, vectorMinEncryptedSize+16) // Add some payload
	validEncrypted[0] = VectorEncryptionVersion

	_, err = svc.DecryptVector(ctx, "nonexistent-tenant", "ns", "vec", validEncrypted)
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrTenantDEKNotFound))
}

// TestVectorDEKManagerErrors tests error handling when DEK manager fails.
func TestVectorDEKManagerErrors(t *testing.T) {
	dekMgr := newMockTenantDEKManagerForVectors()
	logger := newVectorTestLogger()
	svc, err := NewVectorEncryptionService(dekMgr, logger, nil)
	require.NoError(t, err)

	ctx := context.Background()

	t.Run("encrypt fails when DEK creation fails", func(t *testing.T) {
		dekMgr.failCreateDEK = true
		defer func() { dekMgr.failCreateDEK = false }()

		_, err := svc.EncryptVector(ctx, "tenant", "ns", "vec", []float32{1.0})
		require.Error(t, err)

		var qbErr *dberrors.QBError
		require.True(t, errors.As(err, &qbErr))
		assert.Equal(t, dberrors.ErrEncryption, qbErr.Code)
	})

	t.Run("decrypt fails when DEK retrieval fails", func(t *testing.T) {
		// First create the DEK
		dekMgr.failCreateDEK = false
		encrypted, err := svc.EncryptVector(ctx, "tenant-get-fail", "ns", "vec", []float32{1.0})
		require.NoError(t, err)

		// Now make get fail
		dekMgr.failGetDEK = true
		defer func() { dekMgr.failGetDEK = false }()

		_, err = svc.DecryptVector(ctx, "tenant-get-fail", "ns", "vec", encrypted)
		require.Error(t, err)

		var qbErr *dberrors.QBError
		require.True(t, errors.As(err, &qbErr))
		assert.Equal(t, dberrors.ErrDecryption, qbErr.Code)
	})
}

// TestVersionByteHandling tests encryption version byte handling.
func TestVersionByteHandling(t *testing.T) {
	_, _, svc := setupVectorEncryptionTest(t)
	ctx := context.Background()

	t.Run("current version is embedded in ciphertext", func(t *testing.T) {
		encrypted, err := svc.EncryptVector(ctx, "tenant", "ns", "vec", []float32{1.0, 2.0})
		require.NoError(t, err)
		assert.Equal(t, VectorEncryptionVersion, encrypted[0])
	})

	t.Run("version 0 rejected", func(t *testing.T) {
		encrypted := make([]byte, vectorMinEncryptedSize+4)
		encrypted[0] = 0 // Version 0

		_, err := svc.DecryptVector(ctx, "tenant", "ns", "vec", encrypted)
		require.Error(t, err)
		assert.True(t, errors.Is(err, ErrUnsupportedEncryptionVersion))
	})

	t.Run("future version rejected", func(t *testing.T) {
		encrypted := make([]byte, vectorMinEncryptedSize+4)
		encrypted[0] = VectorEncryptionVersion + 1 // Future version

		_, err := svc.DecryptVector(ctx, "tenant", "ns", "vec", encrypted)
		require.Error(t, err)
		assert.True(t, errors.Is(err, ErrUnsupportedEncryptionVersion))
	})
}

// TestFloat32Conversion tests float32 <-> bytes conversion.
func TestFloat32Conversion(t *testing.T) {
	t.Run("roundtrip conversion", func(t *testing.T) {
		original := []float32{0.0, 1.0, -1.0, 3.14159, -2.71828, 1e10, 1e-10}

		bytes := float32SliceToBytes(original)
		require.Len(t, bytes, len(original)*4)

		result := bytesToFloat32Slice(bytes)
		require.Len(t, result, len(original))

		for i := range original {
			assert.InDelta(t, original[i], result[i], 0.00001)
		}
	})

	t.Run("empty slice", func(t *testing.T) {
		bytes := float32SliceToBytes([]float32{})
		assert.Len(t, bytes, 0)

		result := bytesToFloat32Slice([]byte{})
		assert.Len(t, result, 0)
	})

	t.Run("invalid bytes length returns nil", func(t *testing.T) {
		// 5 bytes is not a multiple of 4
		result := bytesToFloat32Slice([]byte{1, 2, 3, 4, 5})
		assert.Nil(t, result)
	})
}

// TestBuildAAD tests AAD construction.
func TestBuildAAD(t *testing.T) {
	t.Run("correct format", func(t *testing.T) {
		aad := buildAAD("namespace", "tenant", "vector")
		assert.Equal(t, []byte("namespace:tenant:vector"), aad)
	})

	t.Run("empty components", func(t *testing.T) {
		aad := buildAAD("", "", "")
		assert.Equal(t, []byte("::"), aad)
	})

	t.Run("special characters preserved", func(t *testing.T) {
		aad := buildAAD("ns/with/slashes", "tenant-with-dashes", "vec_with_underscores")
		assert.Equal(t, []byte("ns/with/slashes:tenant-with-dashes:vec_with_underscores"), aad)
	})
}

// TestWipeDEK tests secure memory wiping.
func TestWipeDEK(t *testing.T) {
	t.Run("wipes all bytes to zero", func(t *testing.T) {
		dek := []byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16,
			17, 18, 19, 20, 21, 22, 23, 24, 25, 26, 27, 28, 29, 30, 31, 32}

		wipeDEK(dek)

		for i, b := range dek {
			assert.Equal(t, byte(0), b, "byte %d should be zero", i)
		}
	})

	t.Run("empty slice safe", func(t *testing.T) {
		dek := []byte{}
		wipeDEK(dek) // Should not panic
	})

	t.Run("nil slice safe", func(t *testing.T) {
		var dek []byte
		wipeDEK(dek) // Should not panic
	})
}

// TestVectorEncryptionError tests the error type.
func TestVectorEncryptionError(t *testing.T) {
	t.Run("error with underlying error", func(t *testing.T) {
		underlyingErr := errors.New("underlying")
		err := &VectorEncryptionError{
			Op:       "TestOp",
			TenantID: "tenant-1",
			VectorID: "vec-1",
			Err:      underlyingErr,
		}

		assert.Equal(t, "TestOp: underlying", err.Error())
		assert.Equal(t, underlyingErr, err.Unwrap())
	})

	t.Run("error without underlying error", func(t *testing.T) {
		err := &VectorEncryptionError{
			Op:       "TestOp",
			TenantID: "tenant-1",
			VectorID: "vec-1",
		}

		assert.Equal(t, "TestOp", err.Error())
		assert.Nil(t, err.Unwrap())
	})
}

// TestConcurrentVectorOperations tests thread safety.
func TestConcurrentVectorOperations(t *testing.T) {
	_, _, svc := setupVectorEncryptionTest(t)
	ctx := context.Background()

	const numGoroutines = 50
	const numOperations = 20

	var wg sync.WaitGroup
	errChan := make(chan error, numGoroutines*numOperations)

	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func(tenantNum int) {
			defer wg.Done()

			tenantID := "tenant-" + string(rune('A'+tenantNum))
			namespace := "ns"

			for j := 0; j < numOperations; j++ {
				vectorID := "vec-" + string(rune('0'+j))
				embedding := []float32{float32(j), float32(j + 1), float32(j + 2)}

				// Encrypt
				encrypted, err := svc.EncryptVector(ctx, tenantID, namespace, vectorID, embedding)
				if err != nil {
					errChan <- err
					continue
				}

				// Decrypt
				decrypted, err := svc.DecryptVector(ctx, tenantID, namespace, vectorID, encrypted)
				if err != nil {
					errChan <- err
					continue
				}

				// Verify
				if len(decrypted) != len(embedding) {
					errChan <- errors.New("length mismatch")
				}
			}
		}(i)
	}

	wg.Wait()
	close(errChan)

	var errs []error
	for err := range errChan {
		errs = append(errs, err)
	}
	assert.Empty(t, errs, "concurrent operations should not produce errors")
}

// BenchmarkEncryptVector benchmarks single vector encryption.
func BenchmarkEncryptVector(b *testing.B) {
	dekMgr := newMockTenantDEKManagerForVectors()
	logger := newVectorTestLogger()
	svc, _ := NewVectorEncryptionService(dekMgr, logger, nil)
	ctx := context.Background()

	embedding := make([]float32, 1536) // OpenAI embedding size
	for i := range embedding {
		embedding[i] = float32(i) * 0.001
	}

	// Pre-create DEK
	_, _ = svc.EncryptVector(ctx, "tenant", "ns", "warmup", embedding)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = svc.EncryptVector(ctx, "tenant", "ns", "vec", embedding)
	}
}

// BenchmarkDecryptVector benchmarks single vector decryption.
func BenchmarkDecryptVector(b *testing.B) {
	dekMgr := newMockTenantDEKManagerForVectors()
	logger := newVectorTestLogger()
	svc, _ := NewVectorEncryptionService(dekMgr, logger, nil)
	ctx := context.Background()

	embedding := make([]float32, 1536)
	for i := range embedding {
		embedding[i] = float32(i) * 0.001
	}

	encrypted, _ := svc.EncryptVector(ctx, "tenant", "ns", "vec", embedding)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = svc.DecryptVector(ctx, "tenant", "ns", "vec", encrypted)
	}
}

// BenchmarkEncryptVectorBatch benchmarks batch encryption.
func BenchmarkEncryptVectorBatch(b *testing.B) {
	dekMgr := newMockTenantDEKManagerForVectors()
	logger := newVectorTestLogger()
	svc, _ := NewVectorEncryptionService(dekMgr, logger, nil)
	ctx := context.Background()

	vectors := make(map[string][]float32, 100)
	for i := 0; i < 100; i++ {
		embedding := make([]float32, 1536)
		for j := range embedding {
			embedding[j] = float32(i+j) * 0.001
		}
		vectors["vec-"+string(rune('0'+i))] = embedding
	}

	// Pre-create DEK
	_, _ = svc.EncryptVectorBatch(ctx, "tenant", "ns", vectors)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = svc.EncryptVectorBatch(ctx, "tenant", "ns", vectors)
	}
}

func TestVectorEncryption_IsEncrypted_PayloadNotDivisible(t *testing.T) {
	data := make([]byte, 32)
	data[0] = VectorEncryptionVersion
	assert.False(t, (&vectorEncryptionService{}).IsEncrypted(data))
}

// ---------------------------------------------------------------------------
// vector_encryption.go -- service closed errors
// ---------------------------------------------------------------------------

func TestVectorEncryptionService_ClosedErrors(t *testing.T) {
	dekMgr := newMockTenantDEKManagerForVectors()
	logger := newVectorTestLogger()
	svc, err := NewVectorEncryptionService(dekMgr, logger, nil)
	require.NoError(t, err)

	// Access the underlying struct to set closed.
	impl := svc.(*vectorEncryptionService)
	impl.closed.Store(true)

	ctx := context.Background()

	t.Run("encrypt closed", func(t *testing.T) {
		_, err := svc.EncryptVector(ctx, "t", "ns", "v", []float32{1.0})
		require.Error(t, err)
		assert.True(t, errors.Is(err, ErrServiceClosed))
	})

	t.Run("decrypt closed", func(t *testing.T) {
		data := make([]byte, vectorMinEncryptedSize)
		data[0] = VectorEncryptionVersion
		_, err := svc.DecryptVector(ctx, "t", "ns", "v", data)
		require.Error(t, err)
		assert.True(t, errors.Is(err, ErrServiceClosed))
	})

	t.Run("batch encrypt closed", func(t *testing.T) {
		_, err := svc.EncryptVectorBatch(ctx, "t", "ns", map[string][]float32{"v": {1.0}})
		require.Error(t, err)
		assert.True(t, errors.Is(err, ErrServiceClosed))
	})

	t.Run("batch decrypt closed", func(t *testing.T) {
		_, err := svc.DecryptVectorBatch(ctx, "t", "ns", map[string][]byte{"v": make([]byte, vectorMinEncryptedSize)})
		require.Error(t, err)
		assert.True(t, errors.Is(err, ErrServiceClosed))
	})
}
