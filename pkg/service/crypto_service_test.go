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

// Package service provides unit tests for the Crypto service implementation.
package service

import (
	"context"
	"errors"
	"testing"
	"time"

	xkmssdk "github.com/jeremyhahn/go-xkms/sdk/go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	quicbaasErrors "github.com/jeremyhahn/go-quicbaas/pkg/errors"
)

// =============================================================================
// Mock Crypto Manager for Unit Testing
// =============================================================================

// mockCryptoManager implements interfaces.CryptoManager for testing.
type mockCryptoManager struct {
	encryptFn        func(ctx context.Context, keyID string, plaintext, encContext []byte) ([]byte, error)
	decryptFn        func(ctx context.Context, keyID string, ciphertext, encContext []byte) ([]byte, error)
	rewrapFn         func(ctx context.Context, keyID string, ciphertext, encContext []byte) ([]byte, error)
	encryptBatchFn   func(ctx context.Context, keyID string, items [][]byte, encContext []byte) ([][]byte, error)
	decryptBatchFn   func(ctx context.Context, keyID string, items [][]byte, encContext []byte) ([][]byte, error)
	signFn           func(ctx context.Context, keyID string, input []byte, hashAlgorithm string, preHashed bool) ([]byte, error)
	verifyFn         func(ctx context.Context, keyID string, input, signature []byte, hashAlgorithm string) (bool, error)
	generateHMACFn   func(ctx context.Context, keyID string, input []byte, algorithm string) ([]byte, error)
	verifyHMACFn     func(ctx context.Context, keyID string, input, hmac []byte, algorithm string) (bool, error)
	generateRandomFn func(ctx context.Context, numBytes int) ([]byte, error)
	hashFn           func(ctx context.Context, input []byte, algorithm string) ([]byte, error)
}

func newMockCryptoManager() *mockCryptoManager {
	return &mockCryptoManager{}
}

func (m *mockCryptoManager) Encrypt(ctx context.Context, keyID string, plaintext, encContext []byte) ([]byte, error) {
	if m.encryptFn != nil {
		return m.encryptFn(ctx, keyID, plaintext, encContext)
	}
	// Default behavior: simple mock encryption
	return append([]byte("crypto:encrypted:"), plaintext...), nil
}

func (m *mockCryptoManager) Decrypt(ctx context.Context, keyID string, ciphertext, encContext []byte) ([]byte, error) {
	if m.decryptFn != nil {
		return m.decryptFn(ctx, keyID, ciphertext, encContext)
	}
	// Default behavior: simple mock decryption
	// "crypto:encrypted:" prefix is 17 bytes
	if len(ciphertext) > 17 {
		return ciphertext[17:], nil
	}
	return nil, errors.New("invalid ciphertext")
}

func (m *mockCryptoManager) Rewrap(ctx context.Context, keyID string, ciphertext, encContext []byte) ([]byte, error) {
	if m.rewrapFn != nil {
		return m.rewrapFn(ctx, keyID, ciphertext, encContext)
	}
	// Default behavior: rewrap by adding version marker
	return append([]byte("v2:"), ciphertext...), nil
}

func (m *mockCryptoManager) EncryptBatch(ctx context.Context, keyID string, items [][]byte, encContext []byte) ([][]byte, error) {
	if m.encryptBatchFn != nil {
		return m.encryptBatchFn(ctx, keyID, items, encContext)
	}
	// Default behavior: encrypt each item
	result := make([][]byte, len(items))
	for i, item := range items {
		result[i] = append([]byte("batch:encrypted:"), item...)
	}
	return result, nil
}

func (m *mockCryptoManager) DecryptBatch(ctx context.Context, keyID string, items [][]byte, encContext []byte) ([][]byte, error) {
	if m.decryptBatchFn != nil {
		return m.decryptBatchFn(ctx, keyID, items, encContext)
	}
	// Default behavior: decrypt each item
	result := make([][]byte, len(items))
	for i, item := range items {
		if len(item) > 16 {
			result[i] = item[16:]
		}
	}
	return result, nil
}

func (m *mockCryptoManager) Sign(ctx context.Context, keyID string, input []byte, hashAlgorithm string, preHashed bool) ([]byte, error) {
	if m.signFn != nil {
		return m.signFn(ctx, keyID, input, hashAlgorithm, preHashed)
	}
	// Default behavior: mock signature
	return append([]byte("sig:"), input...), nil
}

func (m *mockCryptoManager) Verify(ctx context.Context, keyID string, input, signature []byte, hashAlgorithm string) (bool, error) {
	if m.verifyFn != nil {
		return m.verifyFn(ctx, keyID, input, signature, hashAlgorithm)
	}
	// Default behavior: verify mock signature
	expected := append([]byte("sig:"), input...)
	return string(expected) == string(signature), nil
}

func (m *mockCryptoManager) GenerateHMAC(ctx context.Context, keyID string, input []byte, algorithm string) ([]byte, error) {
	if m.generateHMACFn != nil {
		return m.generateHMACFn(ctx, keyID, input, algorithm)
	}
	// Default behavior: mock HMAC
	return append([]byte("hmac:"), input...), nil
}

func (m *mockCryptoManager) VerifyHMAC(ctx context.Context, keyID string, input, hmac []byte, algorithm string) (bool, error) {
	if m.verifyHMACFn != nil {
		return m.verifyHMACFn(ctx, keyID, input, hmac, algorithm)
	}
	// Default behavior: verify mock HMAC
	expected := append([]byte("hmac:"), input...)
	return string(expected) == string(hmac), nil
}

func (m *mockCryptoManager) GenerateRandom(ctx context.Context, numBytes int) ([]byte, error) {
	if m.generateRandomFn != nil {
		return m.generateRandomFn(ctx, numBytes)
	}
	// Default behavior: return pseudo-random bytes
	randomBytes := make([]byte, numBytes)
	for i := range randomBytes {
		randomBytes[i] = byte(i % 256)
	}
	return randomBytes, nil
}

func (m *mockCryptoManager) Hash(ctx context.Context, input []byte, algorithm string) ([]byte, error) {
	if m.hashFn != nil {
		return m.hashFn(ctx, input, algorithm)
	}
	// Default behavior: mock hash
	return append([]byte("hash:"), input...), nil
}

func (m *mockCryptoManager) Close() error {
	return nil
}

// =============================================================================
// Constructor Tests
// =============================================================================

func TestNewCryptoServiceImpl_Success(t *testing.T) {
	cryptoMgr := newMockCryptoManager()
	kmsMgr := newMockKMSManager()
	logger := newMockLogger()

	svc := newCryptoService(cryptoMgr, kmsMgr, logger, nil)

	require.NotNil(t, svc)
	assert.NotNil(t, svc.cryptoManager)
	assert.NotNil(t, svc.kmsManager)
	assert.NotNil(t, svc.logger)
	assert.NotNil(t, svc.metrics)
	assert.NotNil(t, svc.metrics.encryptOperationsTotal)
	assert.NotNil(t, svc.metrics.decryptOperationsTotal)
	assert.NotNil(t, svc.metrics.signOperationsTotal)
	assert.NotNil(t, svc.metrics.verifyOperationsTotal)
	assert.NotNil(t, svc.metrics.hmacOperationsTotal)
	assert.NotNil(t, svc.metrics.operationDuration)
	assert.NotNil(t, svc.metrics.errorsByType)
}

func TestNewCryptoServiceImpl_NilLogger(t *testing.T) {
	cryptoMgr := newMockCryptoManager()
	kmsMgr := newMockKMSManager()

	svc := newCryptoService(cryptoMgr, kmsMgr, nil, nil)

	require.NotNil(t, svc)
	assert.Nil(t, svc.logger)
}

// =============================================================================
// Encrypt Tests
// =============================================================================

func TestCryptoService_Encrypt_Success(t *testing.T) {
	cryptoMgr := newMockCryptoManager()
	kmsMgr := newMockKMSManager()
	logger := newMockLogger()
	svc := newCryptoService(cryptoMgr, kmsMgr, logger, nil)

	req := &EncryptRequest{
		KeyID:     "test-key-1",
		Plaintext: []byte("secret data"),
		Context:   []byte("additional context"),
	}

	resp, err := svc.Encrypt(context.Background(), req)

	require.NoError(t, err)
	assert.NotNil(t, resp)
	assert.Equal(t, "test-key-1", resp.KeyID)
	assert.NotEmpty(t, resp.Ciphertext)
	assert.NotZero(t, resp.Timestamp)
}

func TestCryptoService_Encrypt_NilRequest(t *testing.T) {
	cryptoMgr := newMockCryptoManager()
	kmsMgr := newMockKMSManager()
	logger := newMockLogger()
	svc := newCryptoService(cryptoMgr, kmsMgr, logger, nil)

	resp, err := svc.Encrypt(context.Background(), nil)

	require.Error(t, err)
	assert.Nil(t, resp)
	assert.Contains(t, err.Error(), "request cannot be nil")
}

func TestCryptoService_Encrypt_EmptyKeyID(t *testing.T) {
	cryptoMgr := newMockCryptoManager()
	kmsMgr := newMockKMSManager()
	logger := newMockLogger()
	svc := newCryptoService(cryptoMgr, kmsMgr, logger, nil)

	req := &EncryptRequest{
		KeyID:     "",
		Plaintext: []byte("secret data"),
	}

	resp, err := svc.Encrypt(context.Background(), req)

	require.Error(t, err)
	assert.Nil(t, resp)
	assert.Contains(t, err.Error(), "key_id is required")
}

func TestCryptoService_Encrypt_EmptyPlaintext(t *testing.T) {
	cryptoMgr := newMockCryptoManager()
	kmsMgr := newMockKMSManager()
	logger := newMockLogger()
	svc := newCryptoService(cryptoMgr, kmsMgr, logger, nil)

	req := &EncryptRequest{
		KeyID:     "test-key-1",
		Plaintext: []byte{},
	}

	resp, err := svc.Encrypt(context.Background(), req)

	require.Error(t, err)
	assert.Nil(t, resp)
	assert.Contains(t, err.Error(), "plaintext cannot be empty")
}

func TestCryptoService_Encrypt_ManagerError(t *testing.T) {
	cryptoMgr := newMockCryptoManager()
	cryptoMgr.encryptFn = func(ctx context.Context, keyID string, plaintext, encContext []byte) ([]byte, error) {
		return nil, errors.New("encryption failed")
	}
	kmsMgr := newMockKMSManager()
	logger := newMockLogger()
	svc := newCryptoService(cryptoMgr, kmsMgr, logger, nil)

	req := &EncryptRequest{
		KeyID:     "test-key-1",
		Plaintext: []byte("secret data"),
	}

	resp, err := svc.Encrypt(context.Background(), req)

	require.Error(t, err)
	assert.Nil(t, resp)
	var dErr *quicbaasErrors.QBError
	if errors.As(err, &dErr) {
		assert.Equal(t, quicbaasErrors.ErrEncryption, dErr.Code)
	}
}

// =============================================================================
// Decrypt Tests
// =============================================================================

func TestCryptoService_Decrypt_Success(t *testing.T) {
	cryptoMgr := newMockCryptoManager()
	kmsMgr := newMockKMSManager()
	logger := newMockLogger()
	svc := newCryptoService(cryptoMgr, kmsMgr, logger, nil)

	req := &DecryptRequest{
		KeyID:      "test-key-1",
		Ciphertext: []byte("crypto:encrypted:secret data"),
		Context:    []byte("additional context"),
	}

	resp, err := svc.Decrypt(context.Background(), req)

	require.NoError(t, err)
	assert.NotNil(t, resp)
	assert.Equal(t, "test-key-1", resp.KeyID)
	assert.NotEmpty(t, resp.Plaintext)
	assert.NotZero(t, resp.Timestamp)
}

func TestCryptoService_Decrypt_NilRequest(t *testing.T) {
	cryptoMgr := newMockCryptoManager()
	kmsMgr := newMockKMSManager()
	logger := newMockLogger()
	svc := newCryptoService(cryptoMgr, kmsMgr, logger, nil)

	resp, err := svc.Decrypt(context.Background(), nil)

	require.Error(t, err)
	assert.Nil(t, resp)
	assert.Contains(t, err.Error(), "request cannot be nil")
}

func TestCryptoService_Decrypt_EmptyKeyID(t *testing.T) {
	cryptoMgr := newMockCryptoManager()
	kmsMgr := newMockKMSManager()
	logger := newMockLogger()
	svc := newCryptoService(cryptoMgr, kmsMgr, logger, nil)

	req := &DecryptRequest{
		KeyID:      "",
		Ciphertext: []byte("ciphertext"),
	}

	resp, err := svc.Decrypt(context.Background(), req)

	require.Error(t, err)
	assert.Nil(t, resp)
	assert.Contains(t, err.Error(), "key_id is required")
}

func TestCryptoService_Decrypt_EmptyCiphertext(t *testing.T) {
	cryptoMgr := newMockCryptoManager()
	kmsMgr := newMockKMSManager()
	logger := newMockLogger()
	svc := newCryptoService(cryptoMgr, kmsMgr, logger, nil)

	req := &DecryptRequest{
		KeyID:      "test-key-1",
		Ciphertext: []byte{},
	}

	resp, err := svc.Decrypt(context.Background(), req)

	require.Error(t, err)
	assert.Nil(t, resp)
	assert.Contains(t, err.Error(), "ciphertext cannot be empty")
}

func TestCryptoService_Decrypt_ManagerError(t *testing.T) {
	cryptoMgr := newMockCryptoManager()
	cryptoMgr.decryptFn = func(ctx context.Context, keyID string, ciphertext, encContext []byte) ([]byte, error) {
		return nil, errors.New("decryption failed")
	}
	kmsMgr := newMockKMSManager()
	logger := newMockLogger()
	svc := newCryptoService(cryptoMgr, kmsMgr, logger, nil)

	req := &DecryptRequest{
		KeyID:      "test-key-1",
		Ciphertext: []byte("ciphertext"),
	}

	resp, err := svc.Decrypt(context.Background(), req)

	require.Error(t, err)
	assert.Nil(t, resp)
	var dErr *quicbaasErrors.QBError
	if errors.As(err, &dErr) {
		assert.Equal(t, quicbaasErrors.ErrDecryption, dErr.Code)
	}
}

// =============================================================================
// Rewrap Tests
// =============================================================================

func TestCryptoService_Rewrap_Success(t *testing.T) {
	cryptoMgr := newMockCryptoManager()
	kmsMgr := newMockKMSManager()
	logger := newMockLogger()
	svc := newCryptoService(cryptoMgr, kmsMgr, logger, nil)

	req := &RewrapRequest{
		KeyID:      "test-key-1",
		Ciphertext: []byte("old-ciphertext"),
	}

	resp, err := svc.Rewrap(context.Background(), req)

	require.NoError(t, err)
	assert.NotNil(t, resp)
	assert.Equal(t, "test-key-1", resp.KeyID)
	assert.NotEmpty(t, resp.Ciphertext)
	assert.NotZero(t, resp.Timestamp)
}

func TestCryptoService_Rewrap_NilRequest(t *testing.T) {
	cryptoMgr := newMockCryptoManager()
	kmsMgr := newMockKMSManager()
	logger := newMockLogger()
	svc := newCryptoService(cryptoMgr, kmsMgr, logger, nil)

	resp, err := svc.Rewrap(context.Background(), nil)

	require.Error(t, err)
	assert.Nil(t, resp)
	assert.Contains(t, err.Error(), "request cannot be nil")
}

func TestCryptoService_Rewrap_EmptyKeyID(t *testing.T) {
	cryptoMgr := newMockCryptoManager()
	kmsMgr := newMockKMSManager()
	logger := newMockLogger()
	svc := newCryptoService(cryptoMgr, kmsMgr, logger, nil)

	req := &RewrapRequest{
		KeyID:      "",
		Ciphertext: []byte("ciphertext"),
	}

	resp, err := svc.Rewrap(context.Background(), req)

	require.Error(t, err)
	assert.Nil(t, resp)
	assert.Contains(t, err.Error(), "key_id is required")
}

func TestCryptoService_Rewrap_EmptyCiphertext(t *testing.T) {
	cryptoMgr := newMockCryptoManager()
	kmsMgr := newMockKMSManager()
	logger := newMockLogger()
	svc := newCryptoService(cryptoMgr, kmsMgr, logger, nil)

	req := &RewrapRequest{
		KeyID:      "test-key-1",
		Ciphertext: []byte{},
	}

	resp, err := svc.Rewrap(context.Background(), req)

	require.Error(t, err)
	assert.Nil(t, resp)
	assert.Contains(t, err.Error(), "ciphertext cannot be empty")
}

func TestCryptoService_Rewrap_ManagerError(t *testing.T) {
	cryptoMgr := newMockCryptoManager()
	cryptoMgr.rewrapFn = func(ctx context.Context, keyID string, ciphertext, encContext []byte) ([]byte, error) {
		return nil, errors.New("rewrap failed")
	}
	kmsMgr := newMockKMSManager()
	logger := newMockLogger()
	svc := newCryptoService(cryptoMgr, kmsMgr, logger, nil)

	req := &RewrapRequest{
		KeyID:      "test-key-1",
		Ciphertext: []byte("ciphertext"),
	}

	resp, err := svc.Rewrap(context.Background(), req)

	require.Error(t, err)
	assert.Nil(t, resp)
	var dErr *quicbaasErrors.QBError
	if errors.As(err, &dErr) {
		assert.Equal(t, quicbaasErrors.ErrEncryption, dErr.Code)
	}
}

// =============================================================================
// EncryptBatch Tests
// =============================================================================

func TestCryptoService_EncryptBatch_Success(t *testing.T) {
	cryptoMgr := newMockCryptoManager()
	kmsMgr := newMockKMSManager()
	logger := newMockLogger()
	svc := newCryptoService(cryptoMgr, kmsMgr, logger, nil)

	req := &EncryptBatchRequest{
		KeyID: "test-key-1",
		Items: []BatchEncryptItem{
			{Plaintext: []byte("data1")},
			{Plaintext: []byte("data2")},
			{Plaintext: []byte("data3")},
		},
	}

	resp, err := svc.EncryptBatch(context.Background(), req)

	require.NoError(t, err)
	assert.NotNil(t, resp)
	assert.Equal(t, "test-key-1", resp.KeyID)
	assert.Len(t, resp.Results, 3)
	for _, result := range resp.Results {
		assert.NotEmpty(t, result.Ciphertext)
	}
}

func TestCryptoService_EncryptBatch_NilRequest(t *testing.T) {
	cryptoMgr := newMockCryptoManager()
	kmsMgr := newMockKMSManager()
	logger := newMockLogger()
	svc := newCryptoService(cryptoMgr, kmsMgr, logger, nil)

	resp, err := svc.EncryptBatch(context.Background(), nil)

	require.Error(t, err)
	assert.Nil(t, resp)
	assert.Contains(t, err.Error(), "request cannot be nil")
}

func TestCryptoService_EncryptBatch_EmptyKeyID(t *testing.T) {
	cryptoMgr := newMockCryptoManager()
	kmsMgr := newMockKMSManager()
	logger := newMockLogger()
	svc := newCryptoService(cryptoMgr, kmsMgr, logger, nil)

	req := &EncryptBatchRequest{
		KeyID: "",
		Items: []BatchEncryptItem{{Plaintext: []byte("data")}},
	}

	resp, err := svc.EncryptBatch(context.Background(), req)

	require.Error(t, err)
	assert.Nil(t, resp)
	assert.Contains(t, err.Error(), "key_id is required")
}

func TestCryptoService_EncryptBatch_EmptyItems(t *testing.T) {
	cryptoMgr := newMockCryptoManager()
	kmsMgr := newMockKMSManager()
	logger := newMockLogger()
	svc := newCryptoService(cryptoMgr, kmsMgr, logger, nil)

	req := &EncryptBatchRequest{
		KeyID: "test-key-1",
		Items: []BatchEncryptItem{},
	}

	resp, err := svc.EncryptBatch(context.Background(), req)

	require.Error(t, err)
	assert.Nil(t, resp)
	assert.Contains(t, err.Error(), "items cannot be empty")
}

func TestCryptoService_EncryptBatch_ManagerError(t *testing.T) {
	cryptoMgr := newMockCryptoManager()
	cryptoMgr.encryptBatchFn = func(ctx context.Context, keyID string, items [][]byte, encContext []byte) ([][]byte, error) {
		return nil, errors.New("batch encryption failed")
	}
	kmsMgr := newMockKMSManager()
	logger := newMockLogger()
	svc := newCryptoService(cryptoMgr, kmsMgr, logger, nil)

	req := &EncryptBatchRequest{
		KeyID: "test-key-1",
		Items: []BatchEncryptItem{{Plaintext: []byte("data")}},
	}

	resp, err := svc.EncryptBatch(context.Background(), req)

	require.Error(t, err)
	assert.Nil(t, resp)
}

// =============================================================================
// DecryptBatch Tests
// =============================================================================

func TestCryptoService_DecryptBatch_Success(t *testing.T) {
	cryptoMgr := newMockCryptoManager()
	kmsMgr := newMockKMSManager()
	logger := newMockLogger()
	svc := newCryptoService(cryptoMgr, kmsMgr, logger, nil)

	req := &DecryptBatchRequest{
		KeyID: "test-key-1",
		Items: []BatchDecryptItem{
			{Ciphertext: []byte("batch:encrypted:data1")},
			{Ciphertext: []byte("batch:encrypted:data2")},
		},
	}

	resp, err := svc.DecryptBatch(context.Background(), req)

	require.NoError(t, err)
	assert.NotNil(t, resp)
	assert.Equal(t, "test-key-1", resp.KeyID)
	assert.Len(t, resp.Results, 2)
}

func TestCryptoService_DecryptBatch_NilRequest(t *testing.T) {
	cryptoMgr := newMockCryptoManager()
	kmsMgr := newMockKMSManager()
	logger := newMockLogger()
	svc := newCryptoService(cryptoMgr, kmsMgr, logger, nil)

	resp, err := svc.DecryptBatch(context.Background(), nil)

	require.Error(t, err)
	assert.Nil(t, resp)
	assert.Contains(t, err.Error(), "request cannot be nil")
}

func TestCryptoService_DecryptBatch_EmptyKeyID(t *testing.T) {
	cryptoMgr := newMockCryptoManager()
	kmsMgr := newMockKMSManager()
	logger := newMockLogger()
	svc := newCryptoService(cryptoMgr, kmsMgr, logger, nil)

	req := &DecryptBatchRequest{
		KeyID: "",
		Items: []BatchDecryptItem{{Ciphertext: []byte("data")}},
	}

	resp, err := svc.DecryptBatch(context.Background(), req)

	require.Error(t, err)
	assert.Nil(t, resp)
	assert.Contains(t, err.Error(), "key_id is required")
}

func TestCryptoService_DecryptBatch_EmptyItems(t *testing.T) {
	cryptoMgr := newMockCryptoManager()
	kmsMgr := newMockKMSManager()
	logger := newMockLogger()
	svc := newCryptoService(cryptoMgr, kmsMgr, logger, nil)

	req := &DecryptBatchRequest{
		KeyID: "test-key-1",
		Items: []BatchDecryptItem{},
	}

	resp, err := svc.DecryptBatch(context.Background(), req)

	require.Error(t, err)
	assert.Nil(t, resp)
	assert.Contains(t, err.Error(), "items cannot be empty")
}

func TestCryptoService_DecryptBatch_ManagerError(t *testing.T) {
	cryptoMgr := newMockCryptoManager()
	cryptoMgr.decryptBatchFn = func(ctx context.Context, keyID string, items [][]byte, encContext []byte) ([][]byte, error) {
		return nil, errors.New("batch decryption failed")
	}
	kmsMgr := newMockKMSManager()
	logger := newMockLogger()
	svc := newCryptoService(cryptoMgr, kmsMgr, logger, nil)

	req := &DecryptBatchRequest{
		KeyID: "test-key-1",
		Items: []BatchDecryptItem{{Ciphertext: []byte("data")}},
	}

	resp, err := svc.DecryptBatch(context.Background(), req)

	require.Error(t, err)
	assert.Nil(t, resp)
}

// =============================================================================
// GenerateDataKey Tests
// =============================================================================

func TestCryptoService_GenerateDataKey_Success(t *testing.T) {
	cryptoMgr := newMockCryptoManager()
	kmsMgr := newMockKMSManager()
	logger := newMockLogger()
	svc := newCryptoService(cryptoMgr, kmsMgr, logger, nil)

	req := &GenerateDataKeyRequest{
		KeyID: "test-key-1",
		KeyAttributes: &xkmssdk.KeyAttributes{
			SymmetricAlgorithm: xkmssdk.SymmetricAES256GCM,
		},
	}

	resp, err := svc.GenerateDataKey(context.Background(), req)

	require.NoError(t, err)
	assert.NotNil(t, resp)
	assert.Equal(t, "test-key-1", resp.KeyID)
	assert.NotEmpty(t, resp.Plaintext)
	assert.NotEmpty(t, resp.CiphertextBlob)
	assert.NotZero(t, resp.Timestamp)
}

func TestCryptoService_GenerateDataKey_NilRequest(t *testing.T) {
	cryptoMgr := newMockCryptoManager()
	kmsMgr := newMockKMSManager()
	logger := newMockLogger()
	svc := newCryptoService(cryptoMgr, kmsMgr, logger, nil)

	resp, err := svc.GenerateDataKey(context.Background(), nil)

	require.Error(t, err)
	assert.Nil(t, resp)
	assert.Contains(t, err.Error(), "request cannot be nil")
}

func TestCryptoService_GenerateDataKey_EmptyKeyID(t *testing.T) {
	cryptoMgr := newMockCryptoManager()
	kmsMgr := newMockKMSManager()
	logger := newMockLogger()
	svc := newCryptoService(cryptoMgr, kmsMgr, logger, nil)

	req := &GenerateDataKeyRequest{
		KeyID: "",
		KeyAttributes: &xkmssdk.KeyAttributes{
			SymmetricAlgorithm: xkmssdk.SymmetricAES256GCM,
		},
	}

	resp, err := svc.GenerateDataKey(context.Background(), req)

	require.Error(t, err)
	assert.Nil(t, resp)
	assert.Contains(t, err.Error(), "key_id is required")
}

func TestCryptoService_GenerateDataKey_DefaultKeySize(t *testing.T) {
	cryptoMgr := newMockCryptoManager()
	kmsMgr := newMockKMSManager()
	logger := newMockLogger()
	svc := newCryptoService(cryptoMgr, kmsMgr, logger, nil)

	req := &GenerateDataKeyRequest{
		KeyID: "test-key-1",
		KeyAttributes: &xkmssdk.KeyAttributes{
			SymmetricAlgorithm: xkmssdk.SymmetricAES256GCM,
		},
	}

	resp, err := svc.GenerateDataKey(context.Background(), req)

	require.NoError(t, err)
	assert.NotNil(t, resp)
	assert.Len(t, resp.Plaintext, 32) // 32 bytes for AES-256
}

func TestCryptoService_GenerateDataKey_ManagerError(t *testing.T) {
	cryptoMgr := newMockCryptoManager()
	kmsMgr := newMockKMSManager()
	kmsMgr.generateDataKeyFn = func(ctx context.Context, attrs *xkmssdk.KeyAttributes, keyBytes int) ([]byte, []byte, error) {
		return nil, nil, errors.New("data key generation failed")
	}
	logger := newMockLogger()
	svc := newCryptoService(cryptoMgr, kmsMgr, logger, nil)

	req := &GenerateDataKeyRequest{
		KeyID: "test-key-1",
		KeyAttributes: &xkmssdk.KeyAttributes{
			SymmetricAlgorithm: xkmssdk.SymmetricAES256GCM,
		},
	}

	resp, err := svc.GenerateDataKey(context.Background(), req)

	require.Error(t, err)
	assert.Nil(t, resp)
}

// =============================================================================
// GenerateWrappedDataKey Tests
// =============================================================================

func TestCryptoService_GenerateWrappedDataKey_Success(t *testing.T) {
	cryptoMgr := newMockCryptoManager()
	kmsMgr := newMockKMSManager()
	logger := newMockLogger()
	svc := newCryptoService(cryptoMgr, kmsMgr, logger, nil)

	req := &GenerateWrappedDataKeyRequest{
		KeyID: "test-key-1",
		KeyAttributes: &xkmssdk.KeyAttributes{
			SymmetricAlgorithm: xkmssdk.SymmetricAES256GCM,
		},
	}

	resp, err := svc.GenerateWrappedDataKey(context.Background(), req)

	require.NoError(t, err)
	assert.NotNil(t, resp)
	assert.Equal(t, "test-key-1", resp.KeyID)
	assert.NotEmpty(t, resp.CiphertextBlob)
	assert.NotZero(t, resp.Timestamp)
}

func TestCryptoService_GenerateWrappedDataKey_NilRequest(t *testing.T) {
	cryptoMgr := newMockCryptoManager()
	kmsMgr := newMockKMSManager()
	logger := newMockLogger()
	svc := newCryptoService(cryptoMgr, kmsMgr, logger, nil)

	resp, err := svc.GenerateWrappedDataKey(context.Background(), nil)

	require.Error(t, err)
	assert.Nil(t, resp)
	assert.Contains(t, err.Error(), "request cannot be nil")
}

func TestCryptoService_GenerateWrappedDataKey_EmptyKeyID(t *testing.T) {
	cryptoMgr := newMockCryptoManager()
	kmsMgr := newMockKMSManager()
	logger := newMockLogger()
	svc := newCryptoService(cryptoMgr, kmsMgr, logger, nil)

	req := &GenerateWrappedDataKeyRequest{
		KeyID: "",
		KeyAttributes: &xkmssdk.KeyAttributes{
			SymmetricAlgorithm: xkmssdk.SymmetricAES256GCM,
		},
	}

	resp, err := svc.GenerateWrappedDataKey(context.Background(), req)

	require.Error(t, err)
	assert.Nil(t, resp)
	assert.Contains(t, err.Error(), "key_id is required")
}

func TestCryptoService_GenerateWrappedDataKey_ManagerError(t *testing.T) {
	cryptoMgr := newMockCryptoManager()
	kmsMgr := newMockKMSManager()
	kmsMgr.generateDataKeyFn = func(ctx context.Context, attrs *xkmssdk.KeyAttributes, keyBytes int) ([]byte, []byte, error) {
		return nil, nil, errors.New("wrapped data key generation failed")
	}
	logger := newMockLogger()
	svc := newCryptoService(cryptoMgr, kmsMgr, logger, nil)

	req := &GenerateWrappedDataKeyRequest{
		KeyID: "test-key-1",
		KeyAttributes: &xkmssdk.KeyAttributes{
			SymmetricAlgorithm: xkmssdk.SymmetricAES256GCM,
		},
	}

	resp, err := svc.GenerateWrappedDataKey(context.Background(), req)

	require.Error(t, err)
	assert.Nil(t, resp)
}

// =============================================================================
// Sign Tests
// =============================================================================

func TestCryptoService_Sign_Success(t *testing.T) {
	cryptoMgr := newMockCryptoManager()
	kmsMgr := newMockKMSManager()
	logger := newMockLogger()
	svc := newCryptoService(cryptoMgr, kmsMgr, logger, nil)

	req := &SignRequest{
		KeyID:         "test-signing-key",
		Message:       []byte("message to sign"),
		HashAlgorithm: HashAlgSHA256,
	}

	resp, err := svc.Sign(context.Background(), req)

	require.NoError(t, err)
	assert.NotNil(t, resp)
	assert.Equal(t, "test-signing-key", resp.KeyID)
	assert.NotEmpty(t, resp.Signature)
	assert.Equal(t, HashAlgSHA256, resp.HashAlgorithm)
	assert.NotZero(t, resp.Timestamp)
}

func TestCryptoService_Sign_DefaultHashAlgorithm(t *testing.T) {
	cryptoMgr := newMockCryptoManager()
	kmsMgr := newMockKMSManager()
	logger := newMockLogger()
	svc := newCryptoService(cryptoMgr, kmsMgr, logger, nil)

	req := &SignRequest{
		KeyID:   "test-signing-key",
		Message: []byte("message to sign"),
		// HashAlgorithm not specified, should default to SHA256
	}

	resp, err := svc.Sign(context.Background(), req)

	require.NoError(t, err)
	assert.NotNil(t, resp)
}

func TestCryptoService_Sign_NilRequest(t *testing.T) {
	cryptoMgr := newMockCryptoManager()
	kmsMgr := newMockKMSManager()
	logger := newMockLogger()
	svc := newCryptoService(cryptoMgr, kmsMgr, logger, nil)

	resp, err := svc.Sign(context.Background(), nil)

	require.Error(t, err)
	assert.Nil(t, resp)
	assert.Contains(t, err.Error(), "request cannot be nil")
}

func TestCryptoService_Sign_EmptyKeyID(t *testing.T) {
	cryptoMgr := newMockCryptoManager()
	kmsMgr := newMockKMSManager()
	logger := newMockLogger()
	svc := newCryptoService(cryptoMgr, kmsMgr, logger, nil)

	req := &SignRequest{
		KeyID:   "",
		Message: []byte("message"),
	}

	resp, err := svc.Sign(context.Background(), req)

	require.Error(t, err)
	assert.Nil(t, resp)
	assert.Contains(t, err.Error(), "key_id is required")
}

func TestCryptoService_Sign_EmptyMessage(t *testing.T) {
	cryptoMgr := newMockCryptoManager()
	kmsMgr := newMockKMSManager()
	logger := newMockLogger()
	svc := newCryptoService(cryptoMgr, kmsMgr, logger, nil)

	req := &SignRequest{
		KeyID:   "test-key",
		Message: []byte{},
	}

	resp, err := svc.Sign(context.Background(), req)

	require.Error(t, err)
	assert.Nil(t, resp)
	assert.Contains(t, err.Error(), "message cannot be empty")
}

func TestCryptoService_Sign_ManagerError(t *testing.T) {
	cryptoMgr := newMockCryptoManager()
	cryptoMgr.signFn = func(ctx context.Context, keyID string, input []byte, hashAlgorithm string, preHashed bool) ([]byte, error) {
		return nil, errors.New("signing failed")
	}
	kmsMgr := newMockKMSManager()
	logger := newMockLogger()
	svc := newCryptoService(cryptoMgr, kmsMgr, logger, nil)

	req := &SignRequest{
		KeyID:   "test-key",
		Message: []byte("message"),
	}

	resp, err := svc.Sign(context.Background(), req)

	require.Error(t, err)
	assert.Nil(t, resp)
}

// =============================================================================
// Verify Tests
// =============================================================================

func TestCryptoService_Verify_Success(t *testing.T) {
	cryptoMgr := newMockCryptoManager()
	kmsMgr := newMockKMSManager()
	logger := newMockLogger()
	svc := newCryptoService(cryptoMgr, kmsMgr, logger, nil)

	message := []byte("message to verify")
	signature := append([]byte("sig:"), message...)

	req := &VerifyRequest{
		KeyID:         "test-signing-key",
		Message:       message,
		Signature:     signature,
		HashAlgorithm: HashAlgSHA256,
	}

	resp, err := svc.Verify(context.Background(), req)

	require.NoError(t, err)
	assert.NotNil(t, resp)
	assert.Equal(t, "test-signing-key", resp.KeyID)
	assert.True(t, resp.Valid)
	assert.NotZero(t, resp.Timestamp)
}

func TestCryptoService_Verify_InvalidSignature(t *testing.T) {
	cryptoMgr := newMockCryptoManager()
	kmsMgr := newMockKMSManager()
	logger := newMockLogger()
	svc := newCryptoService(cryptoMgr, kmsMgr, logger, nil)

	req := &VerifyRequest{
		KeyID:     "test-signing-key",
		Message:   []byte("message"),
		Signature: []byte("invalid-signature"),
	}

	resp, err := svc.Verify(context.Background(), req)

	require.NoError(t, err)
	assert.NotNil(t, resp)
	assert.False(t, resp.Valid)
}

func TestCryptoService_Verify_NilRequest(t *testing.T) {
	cryptoMgr := newMockCryptoManager()
	kmsMgr := newMockKMSManager()
	logger := newMockLogger()
	svc := newCryptoService(cryptoMgr, kmsMgr, logger, nil)

	resp, err := svc.Verify(context.Background(), nil)

	require.Error(t, err)
	assert.Nil(t, resp)
	assert.Contains(t, err.Error(), "request cannot be nil")
}

func TestCryptoService_Verify_EmptyKeyID(t *testing.T) {
	cryptoMgr := newMockCryptoManager()
	kmsMgr := newMockKMSManager()
	logger := newMockLogger()
	svc := newCryptoService(cryptoMgr, kmsMgr, logger, nil)

	req := &VerifyRequest{
		KeyID:     "",
		Message:   []byte("message"),
		Signature: []byte("signature"),
	}

	resp, err := svc.Verify(context.Background(), req)

	require.Error(t, err)
	assert.Nil(t, resp)
	assert.Contains(t, err.Error(), "key_id is required")
}

func TestCryptoService_Verify_EmptyMessage(t *testing.T) {
	cryptoMgr := newMockCryptoManager()
	kmsMgr := newMockKMSManager()
	logger := newMockLogger()
	svc := newCryptoService(cryptoMgr, kmsMgr, logger, nil)

	req := &VerifyRequest{
		KeyID:     "test-key",
		Message:   []byte{},
		Signature: []byte("signature"),
	}

	resp, err := svc.Verify(context.Background(), req)

	require.Error(t, err)
	assert.Nil(t, resp)
	assert.Contains(t, err.Error(), "message cannot be empty")
}

func TestCryptoService_Verify_EmptySignature(t *testing.T) {
	cryptoMgr := newMockCryptoManager()
	kmsMgr := newMockKMSManager()
	logger := newMockLogger()
	svc := newCryptoService(cryptoMgr, kmsMgr, logger, nil)

	req := &VerifyRequest{
		KeyID:     "test-key",
		Message:   []byte("message"),
		Signature: []byte{},
	}

	resp, err := svc.Verify(context.Background(), req)

	require.Error(t, err)
	assert.Nil(t, resp)
	assert.Contains(t, err.Error(), "signature cannot be empty")
}

func TestCryptoService_Verify_ManagerError(t *testing.T) {
	cryptoMgr := newMockCryptoManager()
	cryptoMgr.verifyFn = func(ctx context.Context, keyID string, input, signature []byte, hashAlgorithm string) (bool, error) {
		return false, errors.New("verification failed")
	}
	kmsMgr := newMockKMSManager()
	logger := newMockLogger()
	svc := newCryptoService(cryptoMgr, kmsMgr, logger, nil)

	req := &VerifyRequest{
		KeyID:     "test-key",
		Message:   []byte("message"),
		Signature: []byte("signature"),
	}

	resp, err := svc.Verify(context.Background(), req)

	require.Error(t, err)
	assert.Nil(t, resp)
}

// =============================================================================
// GenerateHMAC Tests
// =============================================================================

func TestCryptoService_GenerateHMAC_Success(t *testing.T) {
	cryptoMgr := newMockCryptoManager()
	kmsMgr := newMockKMSManager()
	logger := newMockLogger()
	svc := newCryptoService(cryptoMgr, kmsMgr, logger, nil)

	req := &HMACRequest{
		KeyID:         "test-hmac-key",
		Message:       []byte("message for hmac"),
		HashAlgorithm: HashAlgSHA256,
	}

	resp, err := svc.GenerateHMAC(context.Background(), req)

	require.NoError(t, err)
	assert.NotNil(t, resp)
	assert.Equal(t, "test-hmac-key", resp.KeyID)
	assert.NotEmpty(t, resp.HMAC)
	assert.Equal(t, HashAlgSHA256, resp.HashAlgorithm)
	assert.NotZero(t, resp.Timestamp)
}

func TestCryptoService_GenerateHMAC_NilRequest(t *testing.T) {
	cryptoMgr := newMockCryptoManager()
	kmsMgr := newMockKMSManager()
	logger := newMockLogger()
	svc := newCryptoService(cryptoMgr, kmsMgr, logger, nil)

	resp, err := svc.GenerateHMAC(context.Background(), nil)

	require.Error(t, err)
	assert.Nil(t, resp)
	assert.Contains(t, err.Error(), "request cannot be nil")
}

func TestCryptoService_GenerateHMAC_EmptyKeyID(t *testing.T) {
	cryptoMgr := newMockCryptoManager()
	kmsMgr := newMockKMSManager()
	logger := newMockLogger()
	svc := newCryptoService(cryptoMgr, kmsMgr, logger, nil)

	req := &HMACRequest{
		KeyID:   "",
		Message: []byte("message"),
	}

	resp, err := svc.GenerateHMAC(context.Background(), req)

	require.Error(t, err)
	assert.Nil(t, resp)
	assert.Contains(t, err.Error(), "key_id is required")
}

func TestCryptoService_GenerateHMAC_EmptyMessage(t *testing.T) {
	cryptoMgr := newMockCryptoManager()
	kmsMgr := newMockKMSManager()
	logger := newMockLogger()
	svc := newCryptoService(cryptoMgr, kmsMgr, logger, nil)

	req := &HMACRequest{
		KeyID:   "test-key",
		Message: []byte{},
	}

	resp, err := svc.GenerateHMAC(context.Background(), req)

	require.Error(t, err)
	assert.Nil(t, resp)
	assert.Contains(t, err.Error(), "message cannot be empty")
}

func TestCryptoService_GenerateHMAC_ManagerError(t *testing.T) {
	cryptoMgr := newMockCryptoManager()
	cryptoMgr.generateHMACFn = func(ctx context.Context, keyID string, input []byte, algorithm string) ([]byte, error) {
		return nil, errors.New("HMAC generation failed")
	}
	kmsMgr := newMockKMSManager()
	logger := newMockLogger()
	svc := newCryptoService(cryptoMgr, kmsMgr, logger, nil)

	req := &HMACRequest{
		KeyID:   "test-key",
		Message: []byte("message"),
	}

	resp, err := svc.GenerateHMAC(context.Background(), req)

	require.Error(t, err)
	assert.Nil(t, resp)
}

// =============================================================================
// VerifyHMAC Tests
// =============================================================================

func TestCryptoService_VerifyHMAC_Success(t *testing.T) {
	cryptoMgr := newMockCryptoManager()
	kmsMgr := newMockKMSManager()
	logger := newMockLogger()
	svc := newCryptoService(cryptoMgr, kmsMgr, logger, nil)

	message := []byte("message to verify")
	hmac := append([]byte("hmac:"), message...)

	req := &VerifyHMACRequest{
		KeyID:         "test-hmac-key",
		Message:       message,
		HMAC:          hmac,
		HashAlgorithm: HashAlgSHA256,
	}

	resp, err := svc.VerifyHMAC(context.Background(), req)

	require.NoError(t, err)
	assert.NotNil(t, resp)
	assert.Equal(t, "test-hmac-key", resp.KeyID)
	assert.True(t, resp.Valid)
	assert.NotZero(t, resp.Timestamp)
}

func TestCryptoService_VerifyHMAC_InvalidHMAC(t *testing.T) {
	cryptoMgr := newMockCryptoManager()
	kmsMgr := newMockKMSManager()
	logger := newMockLogger()
	svc := newCryptoService(cryptoMgr, kmsMgr, logger, nil)

	req := &VerifyHMACRequest{
		KeyID:   "test-hmac-key",
		Message: []byte("message"),
		HMAC:    []byte("invalid-hmac"),
	}

	resp, err := svc.VerifyHMAC(context.Background(), req)

	require.NoError(t, err)
	assert.NotNil(t, resp)
	assert.False(t, resp.Valid)
}

func TestCryptoService_VerifyHMAC_NilRequest(t *testing.T) {
	cryptoMgr := newMockCryptoManager()
	kmsMgr := newMockKMSManager()
	logger := newMockLogger()
	svc := newCryptoService(cryptoMgr, kmsMgr, logger, nil)

	resp, err := svc.VerifyHMAC(context.Background(), nil)

	require.Error(t, err)
	assert.Nil(t, resp)
	assert.Contains(t, err.Error(), "request cannot be nil")
}

func TestCryptoService_VerifyHMAC_EmptyKeyID(t *testing.T) {
	cryptoMgr := newMockCryptoManager()
	kmsMgr := newMockKMSManager()
	logger := newMockLogger()
	svc := newCryptoService(cryptoMgr, kmsMgr, logger, nil)

	req := &VerifyHMACRequest{
		KeyID:   "",
		Message: []byte("message"),
		HMAC:    []byte("hmac"),
	}

	resp, err := svc.VerifyHMAC(context.Background(), req)

	require.Error(t, err)
	assert.Nil(t, resp)
	assert.Contains(t, err.Error(), "key_id is required")
}

func TestCryptoService_VerifyHMAC_EmptyMessage(t *testing.T) {
	cryptoMgr := newMockCryptoManager()
	kmsMgr := newMockKMSManager()
	logger := newMockLogger()
	svc := newCryptoService(cryptoMgr, kmsMgr, logger, nil)

	req := &VerifyHMACRequest{
		KeyID:   "test-key",
		Message: []byte{},
		HMAC:    []byte("hmac"),
	}

	resp, err := svc.VerifyHMAC(context.Background(), req)

	require.Error(t, err)
	assert.Nil(t, resp)
	assert.Contains(t, err.Error(), "message cannot be empty")
}

func TestCryptoService_VerifyHMAC_EmptyHMAC(t *testing.T) {
	cryptoMgr := newMockCryptoManager()
	kmsMgr := newMockKMSManager()
	logger := newMockLogger()
	svc := newCryptoService(cryptoMgr, kmsMgr, logger, nil)

	req := &VerifyHMACRequest{
		KeyID:   "test-key",
		Message: []byte("message"),
		HMAC:    []byte{},
	}

	resp, err := svc.VerifyHMAC(context.Background(), req)

	require.Error(t, err)
	assert.Nil(t, resp)
	assert.Contains(t, err.Error(), "hmac cannot be empty")
}

func TestCryptoService_VerifyHMAC_ManagerError(t *testing.T) {
	cryptoMgr := newMockCryptoManager()
	cryptoMgr.verifyHMACFn = func(ctx context.Context, keyID string, input, hmac []byte, algorithm string) (bool, error) {
		return false, errors.New("HMAC verification failed")
	}
	kmsMgr := newMockKMSManager()
	logger := newMockLogger()
	svc := newCryptoService(cryptoMgr, kmsMgr, logger, nil)

	req := &VerifyHMACRequest{
		KeyID:   "test-key",
		Message: []byte("message"),
		HMAC:    []byte("hmac"),
	}

	resp, err := svc.VerifyHMAC(context.Background(), req)

	require.Error(t, err)
	assert.Nil(t, resp)
}

// =============================================================================
// GenerateRandom Tests
// =============================================================================

func TestCryptoService_GenerateRandom_Success(t *testing.T) {
	cryptoMgr := newMockCryptoManager()
	kmsMgr := newMockKMSManager()
	logger := newMockLogger()
	svc := newCryptoService(cryptoMgr, kmsMgr, logger, nil)

	req := &GenerateRandomRequest{
		NumBytes: 32,
	}

	resp, err := svc.GenerateRandom(context.Background(), req)

	require.NoError(t, err)
	assert.NotNil(t, resp)
	assert.Len(t, resp.RandomBytes, 32)
	assert.NotZero(t, resp.Timestamp)
}

func TestCryptoService_GenerateRandom_NilRequest(t *testing.T) {
	cryptoMgr := newMockCryptoManager()
	kmsMgr := newMockKMSManager()
	logger := newMockLogger()
	svc := newCryptoService(cryptoMgr, kmsMgr, logger, nil)

	resp, err := svc.GenerateRandom(context.Background(), nil)

	require.Error(t, err)
	assert.Nil(t, resp)
	assert.Contains(t, err.Error(), "request cannot be nil")
}

func TestCryptoService_GenerateRandom_ZeroBytes(t *testing.T) {
	cryptoMgr := newMockCryptoManager()
	kmsMgr := newMockKMSManager()
	logger := newMockLogger()
	svc := newCryptoService(cryptoMgr, kmsMgr, logger, nil)

	req := &GenerateRandomRequest{
		NumBytes: 0,
	}

	resp, err := svc.GenerateRandom(context.Background(), req)

	require.Error(t, err)
	assert.Nil(t, resp)
	assert.Contains(t, err.Error(), "num_bytes must be positive")
}

func TestCryptoService_GenerateRandom_NegativeBytes(t *testing.T) {
	cryptoMgr := newMockCryptoManager()
	kmsMgr := newMockKMSManager()
	logger := newMockLogger()
	svc := newCryptoService(cryptoMgr, kmsMgr, logger, nil)

	req := &GenerateRandomRequest{
		NumBytes: -10,
	}

	resp, err := svc.GenerateRandom(context.Background(), req)

	require.Error(t, err)
	assert.Nil(t, resp)
	assert.Contains(t, err.Error(), "num_bytes must be positive")
}

func TestCryptoService_GenerateRandom_ExceedsMaximum(t *testing.T) {
	cryptoMgr := newMockCryptoManager()
	kmsMgr := newMockKMSManager()
	logger := newMockLogger()
	svc := newCryptoService(cryptoMgr, kmsMgr, logger, nil)

	req := &GenerateRandomRequest{
		NumBytes: 1024*1024 + 1, // 1MB + 1 byte
	}

	resp, err := svc.GenerateRandom(context.Background(), req)

	require.Error(t, err)
	assert.Nil(t, resp)
	assert.Contains(t, err.Error(), "num_bytes cannot exceed 1MB")
}

func TestCryptoService_GenerateRandom_ManagerError(t *testing.T) {
	cryptoMgr := newMockCryptoManager()
	cryptoMgr.generateRandomFn = func(ctx context.Context, numBytes int) ([]byte, error) {
		return nil, errors.New("random generation failed")
	}
	kmsMgr := newMockKMSManager()
	logger := newMockLogger()
	svc := newCryptoService(cryptoMgr, kmsMgr, logger, nil)

	req := &GenerateRandomRequest{
		NumBytes: 32,
	}

	resp, err := svc.GenerateRandom(context.Background(), req)

	require.Error(t, err)
	assert.Nil(t, resp)
}

// =============================================================================
// Hash Tests
// =============================================================================

func TestCryptoService_Hash_Success(t *testing.T) {
	cryptoMgr := newMockCryptoManager()
	kmsMgr := newMockKMSManager()
	logger := newMockLogger()
	svc := newCryptoService(cryptoMgr, kmsMgr, logger, nil)

	req := &HashRequest{
		Data:      []byte("data to hash"),
		Algorithm: HashAlgSHA256,
	}

	resp, err := svc.Hash(context.Background(), req)

	require.NoError(t, err)
	assert.NotNil(t, resp)
	assert.Equal(t, HashAlgSHA256, resp.Algorithm)
	assert.NotEmpty(t, resp.Hash)
	assert.NotZero(t, resp.Timestamp)
}

func TestCryptoService_Hash_NilRequest(t *testing.T) {
	cryptoMgr := newMockCryptoManager()
	kmsMgr := newMockKMSManager()
	logger := newMockLogger()
	svc := newCryptoService(cryptoMgr, kmsMgr, logger, nil)

	resp, err := svc.Hash(context.Background(), nil)

	require.Error(t, err)
	assert.Nil(t, resp)
	assert.Contains(t, err.Error(), "request cannot be nil")
}

func TestCryptoService_Hash_EmptyData(t *testing.T) {
	cryptoMgr := newMockCryptoManager()
	kmsMgr := newMockKMSManager()
	logger := newMockLogger()
	svc := newCryptoService(cryptoMgr, kmsMgr, logger, nil)

	req := &HashRequest{
		Data:      []byte{},
		Algorithm: HashAlgSHA256,
	}

	resp, err := svc.Hash(context.Background(), req)

	require.Error(t, err)
	assert.Nil(t, resp)
	assert.Contains(t, err.Error(), "data cannot be empty")
}

func TestCryptoService_Hash_EmptyAlgorithm(t *testing.T) {
	cryptoMgr := newMockCryptoManager()
	kmsMgr := newMockKMSManager()
	logger := newMockLogger()
	svc := newCryptoService(cryptoMgr, kmsMgr, logger, nil)

	req := &HashRequest{
		Data:      []byte("data"),
		Algorithm: "",
	}

	resp, err := svc.Hash(context.Background(), req)

	require.Error(t, err)
	assert.Nil(t, resp)
	assert.Contains(t, err.Error(), "algorithm is required")
}

func TestCryptoService_Hash_ManagerError(t *testing.T) {
	cryptoMgr := newMockCryptoManager()
	cryptoMgr.hashFn = func(ctx context.Context, input []byte, algorithm string) ([]byte, error) {
		return nil, errors.New("hash computation failed")
	}
	kmsMgr := newMockKMSManager()
	logger := newMockLogger()
	svc := newCryptoService(cryptoMgr, kmsMgr, logger, nil)

	req := &HashRequest{
		Data:      []byte("data"),
		Algorithm: HashAlgSHA256,
	}

	resp, err := svc.Hash(context.Background(), req)

	require.Error(t, err)
	assert.Nil(t, resp)
}

// =============================================================================
// Context Cancellation Tests
// =============================================================================

func TestCryptoService_Encrypt_ContextCanceled(t *testing.T) {
	cryptoMgr := newMockCryptoManager()
	cryptoMgr.encryptFn = func(ctx context.Context, keyID string, plaintext, encContext []byte) ([]byte, error) {
		return nil, ctx.Err()
	}
	kmsMgr := newMockKMSManager()
	logger := newMockLogger()
	svc := newCryptoService(cryptoMgr, kmsMgr, logger, nil)

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	req := &EncryptRequest{
		KeyID:     "test-key",
		Plaintext: []byte("data"),
	}

	resp, err := svc.Encrypt(ctx, req)

	require.Error(t, err)
	assert.Nil(t, resp)
}

func TestCryptoService_Sign_ContextTimeout(t *testing.T) {
	cryptoMgr := newMockCryptoManager()
	// Wait for the deadline to actually fire; do NOT sleep past it. ctx.Err()
	// is not derived from the clock -- it is a field set by the goroutine
	// context.WithTimeout schedules for the deadline, and Err() only reads it.
	// A mock that sleeps 100ms on a 10ms deadline therefore races that
	// goroutine rather than waiting for it, and when the sleeper is scheduled
	// first it reads a nil error and the whole Sign call succeeds (go#493:
	// measured at 4/600 on a loaded single-P runtime, 1/1500 on an idle one).
	// Done() is the ordering the context contract does guarantee: err is
	// assigned before the channel closes, under the same mutex Err() takes.
	cryptoMgr.signFn = func(ctx context.Context, keyID string, input []byte, hashAlgorithm string, preHashed bool) ([]byte, error) {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(30 * time.Second):
			// Only reachable if Sign stopped propagating the caller's
			// context. Fail loudly instead of returning a generic error that
			// would satisfy the assertion below for the wrong reason.
			return nil, errors.New("caller context was not propagated to the crypto manager")
		}
	}
	kmsMgr := newMockKMSManager()
	logger := newMockLogger()
	svc := newCryptoService(cryptoMgr, kmsMgr, logger, nil)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()

	req := &SignRequest{
		KeyID:   "test-key",
		Message: []byte("message"),
	}

	resp, err := svc.Sign(ctx, req)

	// ErrorIs, not Error: the expiry must be what surfaced. A bare Error()
	// also passes on the not-propagated branch above, which is the failure
	// this test exists to catch.
	require.ErrorIs(t, err, context.DeadlineExceeded)
	assert.Nil(t, resp)
}

// =============================================================================
// Hash Algorithm Tests
// =============================================================================

func TestHashAlgorithm_AllSupportedAlgorithms(t *testing.T) {
	supportedAlgorithms := []HashAlgorithm{
		HashAlgSHA256,
		HashAlgSHA384,
		HashAlgSHA512,
		HashAlgSHA3_256,
		HashAlgSHA3_384,
		HashAlgSHA3_512,
		HashAlgBLAKE2b256,
		HashAlgBLAKE2b512,
	}

	for _, alg := range supportedAlgorithms {
		t.Run(string(alg), func(t *testing.T) {
			assert.NotEmpty(t, alg, "Algorithm should not be empty")
		})
	}
}

// =============================================================================
// Integration-Style Tests (Using Both Managers)
// =============================================================================

func TestCryptoService_EncryptDecrypt_RoundTrip(t *testing.T) {
	cryptoMgr := newMockCryptoManager()
	kmsMgr := newMockKMSManager()
	logger := newMockLogger()
	svc := newCryptoService(cryptoMgr, kmsMgr, logger, nil)

	originalData := []byte("sensitive data to protect")

	// Encrypt
	encReq := &EncryptRequest{
		KeyID:     "test-key-1",
		Plaintext: originalData,
	}
	encResp, err := svc.Encrypt(context.Background(), encReq)
	require.NoError(t, err)
	assert.NotNil(t, encResp)

	// Decrypt
	decReq := &DecryptRequest{
		KeyID:      "test-key-1",
		Ciphertext: encResp.Ciphertext,
	}
	decResp, err := svc.Decrypt(context.Background(), decReq)
	require.NoError(t, err)
	assert.NotNil(t, decResp)
	assert.Equal(t, originalData, decResp.Plaintext)
}

func TestCryptoService_SignVerify_RoundTrip(t *testing.T) {
	cryptoMgr := newMockCryptoManager()
	kmsMgr := newMockKMSManager()
	logger := newMockLogger()
	svc := newCryptoService(cryptoMgr, kmsMgr, logger, nil)

	message := []byte("important document")

	// Sign
	signReq := &SignRequest{
		KeyID:   "test-signing-key",
		Message: message,
	}
	signResp, err := svc.Sign(context.Background(), signReq)
	require.NoError(t, err)
	assert.NotNil(t, signResp)

	// Verify
	verifyReq := &VerifyRequest{
		KeyID:     "test-signing-key",
		Message:   message,
		Signature: signResp.Signature,
	}
	verifyResp, err := svc.Verify(context.Background(), verifyReq)
	require.NoError(t, err)
	assert.NotNil(t, verifyResp)
	assert.True(t, verifyResp.Valid)
}

func TestCryptoService_GenerateVerifyHMAC_RoundTrip(t *testing.T) {
	cryptoMgr := newMockCryptoManager()
	kmsMgr := newMockKMSManager()
	logger := newMockLogger()
	svc := newCryptoService(cryptoMgr, kmsMgr, logger, nil)

	message := []byte("data requiring authentication")

	// Generate HMAC
	hmacReq := &HMACRequest{
		KeyID:   "test-hmac-key",
		Message: message,
	}
	hmacResp, err := svc.GenerateHMAC(context.Background(), hmacReq)
	require.NoError(t, err)
	assert.NotNil(t, hmacResp)

	// Verify HMAC
	verifyReq := &VerifyHMACRequest{
		KeyID:   "test-hmac-key",
		Message: message,
		HMAC:    hmacResp.HMAC,
	}
	verifyResp, err := svc.VerifyHMAC(context.Background(), verifyReq)
	require.NoError(t, err)
	assert.NotNil(t, verifyResp)
	assert.True(t, verifyResp.Valid)
}

func TestCryptoService_Decrypt_SafetyTracker_LimitExceeded(t *testing.T) {
	cryptoMgr := newMockCryptoManager()
	kmsMgr := newMockKMSManager()
	logger := newMockLogger()

	tracker := NewAEADSafetyTracker(
		WithServerConfig(AEADSafetyConfig{
			MaxOperations:     1,
			MaxBytes:          1,
			WarningPercent:    50,
			AutoRotatePercent: 90,
		}),
		WithEnforceDefaults(true),
	)

	svc := newCryptoService(cryptoMgr, kmsMgr, logger, tracker)

	// First decrypt to use up the limit
	req := &DecryptRequest{
		KeyID:      "limited-decrypt-key",
		Ciphertext: append([]byte("crypto:encrypted:"), []byte("data")...),
	}
	_, _ = svc.Decrypt(context.Background(), req)

	// Second should exceed the limit
	req2 := &DecryptRequest{
		KeyID:      "limited-decrypt-key",
		Ciphertext: append([]byte("crypto:encrypted:"), []byte("more data exceeds")...),
	}
	resp, err := svc.Decrypt(context.Background(), req2)
	if err != nil {
		var qbErr *quicbaasErrors.QBError
		if errors.As(err, &qbErr) {
			assert.Equal(t, quicbaasErrors.ErrDecryption, qbErr.Code)
		}
		assert.Nil(t, resp)
	}
}

func TestCryptoService_Rewrap_SafetyTracker_LimitExceeded(t *testing.T) {
	cryptoMgr := newMockCryptoManager()
	kmsMgr := newMockKMSManager()
	logger := newMockLogger()

	tracker := NewAEADSafetyTracker(
		WithServerConfig(AEADSafetyConfig{
			MaxOperations:     1,
			MaxBytes:          1,
			WarningPercent:    50,
			AutoRotatePercent: 90,
		}),
		WithEnforceDefaults(true),
	)

	svc := newCryptoService(cryptoMgr, kmsMgr, logger, tracker)

	// First rewrap to use up the limit
	req := &RewrapRequest{
		KeyID:      "limited-rewrap-key",
		Ciphertext: []byte("old-ciphertext"),
	}
	_, _ = svc.Rewrap(context.Background(), req)

	// Second should exceed the limit
	req2 := &RewrapRequest{
		KeyID:      "limited-rewrap-key",
		Ciphertext: []byte("another-old-ciphertext-data"),
	}
	resp, err := svc.Rewrap(context.Background(), req2)
	if err != nil {
		var qbErr *quicbaasErrors.QBError
		if errors.As(err, &qbErr) {
			assert.Equal(t, quicbaasErrors.ErrEncryption, qbErr.Code)
		}
		assert.Nil(t, resp)
	}
}

func TestCryptoService_GenerateDataKey_SafetyTracker_LimitExceeded(t *testing.T) {
	cryptoMgr := newMockCryptoManager()
	kmsMgr := newMockKMSManager()
	logger := newMockLogger()

	tracker := NewAEADSafetyTracker(
		WithServerConfig(AEADSafetyConfig{
			MaxOperations:     1,
			MaxBytes:          1,
			WarningPercent:    50,
			AutoRotatePercent: 90,
		}),
		WithEnforceDefaults(true),
	)

	svc := newCryptoService(cryptoMgr, kmsMgr, logger, tracker)

	// First generate to use up the limit
	req := &GenerateDataKeyRequest{
		KeyID: "limited-genkey",
	}
	_, _ = svc.GenerateDataKey(context.Background(), req)

	// Second should exceed the limit
	req2 := &GenerateDataKeyRequest{
		KeyID: "limited-genkey",
	}
	resp, err := svc.GenerateDataKey(context.Background(), req2)
	if err != nil {
		var qbErr *quicbaasErrors.QBError
		if errors.As(err, &qbErr) {
			assert.Equal(t, quicbaasErrors.ErrEncryption, qbErr.Code)
		}
		assert.Nil(t, resp)
	}
}

func TestCryptoService_GenerateWrappedDataKey_SafetyTracker_LimitExceeded(t *testing.T) {
	cryptoMgr := newMockCryptoManager()
	kmsMgr := newMockKMSManager()
	logger := newMockLogger()

	tracker := NewAEADSafetyTracker(
		WithServerConfig(AEADSafetyConfig{
			MaxOperations:     1,
			MaxBytes:          1,
			WarningPercent:    50,
			AutoRotatePercent: 90,
		}),
		WithEnforceDefaults(true),
	)

	svc := newCryptoService(cryptoMgr, kmsMgr, logger, tracker)

	// First generate to use up the limit
	req := &GenerateWrappedDataKeyRequest{
		KeyID: "limited-wrapped-genkey",
	}
	_, _ = svc.GenerateWrappedDataKey(context.Background(), req)

	// Second should exceed the limit
	req2 := &GenerateWrappedDataKeyRequest{
		KeyID: "limited-wrapped-genkey",
	}
	resp, err := svc.GenerateWrappedDataKey(context.Background(), req2)
	if err != nil {
		var qbErr *quicbaasErrors.QBError
		if errors.As(err, &qbErr) {
			assert.Equal(t, quicbaasErrors.ErrEncryption, qbErr.Code)
		}
		assert.Nil(t, resp)
	}
}
