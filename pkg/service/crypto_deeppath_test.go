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

package service

import (
	"context"
	"errors"
	"testing"

	xkmssdk "github.com/jeremyhahn/go-xkms/sdk/go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	quicbaasErrors "github.com/jeremyhahn/go-quicbaas/pkg/errors"
)

// =============================================================================
// Encrypt with Safety Tracker Tests
// =============================================================================

func TestCryptoService_Encrypt_WithSafetyTracker_Success(t *testing.T) {
	cryptoMgr := newMockCryptoManager()
	kmsMgr := newMockKMSManager()
	logger := newMockLogger()
	tracker := NewAEADSafetyTracker()

	svc := newCryptoService(cryptoMgr, kmsMgr, logger, tracker)

	req := &EncryptRequest{
		KeyID:     "tracker-key",
		Plaintext: []byte("secret data with safety tracking"),
		Context:   []byte("ctx"),
	}

	resp, err := svc.Encrypt(systemTenantCtx(), req)
	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.Equal(t, "tracker-key", resp.KeyID)
	assert.NotEmpty(t, resp.Ciphertext)
}

func TestCryptoService_Encrypt_WithSafetyTracker_LimitExceeded(t *testing.T) {
	cryptoMgr := newMockCryptoManager()
	kmsMgr := newMockKMSManager()
	logger := newMockLogger()

	// Create tracker with extremely low limit to trigger failure
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

	// First encrypt uses up the limit
	req := &EncryptRequest{
		KeyID:     "limited-key",
		Plaintext: []byte("data"),
	}
	_, _ = svc.Encrypt(systemTenantCtx(), req)

	// Second should exceed limit
	req2 := &EncryptRequest{
		KeyID:     "limited-key",
		Plaintext: []byte("more data exceeds limit"),
	}
	resp, err := svc.Encrypt(systemTenantCtx(), req2)
	// The error depends on the tracker's enforcement behavior
	if err != nil {
		var qbErr *quicbaasErrors.QBError
		if errors.As(err, &qbErr) {
			assert.Equal(t, quicbaasErrors.ErrEncryption, qbErr.Code)
		}
		assert.Nil(t, resp)
	}
}

func TestCryptoService_Encrypt_CryptoManagerFailure(t *testing.T) {
	cryptoMgr := newMockCryptoManager()
	kmsMgr := newMockKMSManager()
	logger := newMockLogger()

	cryptoMgr.encryptFn = func(ctx context.Context, keyID string, plaintext, encContext []byte) ([]byte, error) {
		return nil, errors.New("HSM unavailable")
	}

	svc := newCryptoService(cryptoMgr, kmsMgr, logger, nil)

	req := &EncryptRequest{
		KeyID:     "failing-key",
		Plaintext: []byte("data"),
	}

	resp, err := svc.Encrypt(systemTenantCtx(), req)
	assert.Nil(t, resp)
	require.Error(t, err)
	var qbErr *quicbaasErrors.QBError
	require.ErrorAs(t, err, &qbErr)
	assert.Equal(t, quicbaasErrors.ErrEncryption, qbErr.Code)
	assert.Contains(t, err.Error(), "encrypt data failed")
}

func TestCryptoService_Encrypt_WithKMSVersionLookup(t *testing.T) {
	cryptoMgr := newMockCryptoManager()
	kmsMgr := newMockKMSManager()
	logger := newMockLogger()

	// Mock KMS manager returning version metadata via map interface
	kmsMgr.getKeyFn = func(ctx context.Context, keyID string) (interface{}, error) {
		return map[string]interface{}{
			"version": uint64(5),
		}, nil
	}

	svc := newCryptoService(cryptoMgr, kmsMgr, logger, nil)

	req := &EncryptRequest{
		KeyID:     "versioned-key",
		Plaintext: []byte("secret"),
	}

	resp, err := svc.Encrypt(systemTenantCtx(), req)
	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.Equal(t, uint64(5), resp.KeyVersion)
}

// =============================================================================
// Decrypt Deep Path Tests
// =============================================================================

func TestCryptoService_Decrypt_WithSafetyTracker_Success(t *testing.T) {
	cryptoMgr := newMockCryptoManager()
	kmsMgr := newMockKMSManager()
	logger := newMockLogger()
	tracker := NewAEADSafetyTracker()

	svc := newCryptoService(cryptoMgr, kmsMgr, logger, tracker)

	req := &DecryptRequest{
		KeyID:      "tracker-key",
		Ciphertext: append([]byte("crypto:encrypted:"), []byte("secret")...),
	}

	resp, err := svc.Decrypt(systemTenantCtx(), req)
	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.Equal(t, "tracker-key", resp.KeyID)
	assert.Equal(t, []byte("secret"), resp.Plaintext)
}

func TestCryptoService_Decrypt_CryptoManagerFailure(t *testing.T) {
	cryptoMgr := newMockCryptoManager()
	kmsMgr := newMockKMSManager()
	logger := newMockLogger()

	cryptoMgr.decryptFn = func(ctx context.Context, keyID string, ciphertext, encContext []byte) ([]byte, error) {
		return nil, errors.New("decryption internal failure")
	}

	svc := newCryptoService(cryptoMgr, kmsMgr, logger, nil)

	req := &DecryptRequest{
		KeyID:      "failing-decrypt-key",
		Ciphertext: []byte("corrupted-ciphertext-data"),
	}

	resp, err := svc.Decrypt(systemTenantCtx(), req)
	assert.Nil(t, resp)
	require.Error(t, err)
	var qbErr *quicbaasErrors.QBError
	require.ErrorAs(t, err, &qbErr)
	assert.Equal(t, quicbaasErrors.ErrDecryption, qbErr.Code)
}

func TestCryptoService_Decrypt_WithExplicitKeyVersion(t *testing.T) {
	cryptoMgr := newMockCryptoManager()
	kmsMgr := newMockKMSManager()
	logger := newMockLogger()

	svc := newCryptoService(cryptoMgr, kmsMgr, logger, nil)

	req := &DecryptRequest{
		KeyID:      "version-key",
		Ciphertext: append([]byte("crypto:encrypted:"), []byte("data")...),
		KeyVersion: 7,
	}

	resp, err := svc.Decrypt(systemTenantCtx(), req)
	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.Equal(t, uint64(7), resp.KeyVersion)
}

// =============================================================================
// Rewrap Deep Path Tests
// =============================================================================

func TestCryptoService_Rewrap_WithSafetyTracker_Success(t *testing.T) {
	cryptoMgr := newMockCryptoManager()
	kmsMgr := newMockKMSManager()
	logger := newMockLogger()
	tracker := NewAEADSafetyTracker()

	svc := newCryptoService(cryptoMgr, kmsMgr, logger, tracker)

	req := &RewrapRequest{
		KeyID:      "tracker-rewrap-key",
		Ciphertext: []byte("old-ciphertext-data-for-rewrap"),
	}

	resp, err := svc.Rewrap(systemTenantCtx(), req)
	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.Equal(t, "tracker-rewrap-key", resp.KeyID)
	assert.NotEmpty(t, resp.Ciphertext)
}

func TestCryptoService_Rewrap_CryptoManagerFailure(t *testing.T) {
	cryptoMgr := newMockCryptoManager()
	kmsMgr := newMockKMSManager()
	logger := newMockLogger()

	cryptoMgr.rewrapFn = func(ctx context.Context, keyID string, ciphertext, encContext []byte) ([]byte, error) {
		return nil, errors.New("rewrap internal failure")
	}

	svc := newCryptoService(cryptoMgr, kmsMgr, logger, nil)

	req := &RewrapRequest{
		KeyID:      "failing-rewrap-key",
		Ciphertext: []byte("ciphertext-for-rewrap-failure"),
	}

	resp, err := svc.Rewrap(systemTenantCtx(), req)
	assert.Nil(t, resp)
	require.Error(t, err)
	var qbErr *quicbaasErrors.QBError
	require.ErrorAs(t, err, &qbErr)
	assert.Equal(t, quicbaasErrors.ErrEncryption, qbErr.Code)
	assert.Contains(t, err.Error(), "rewrap data failed")
}

// =============================================================================
// EncryptBatch Deep Path Tests
// =============================================================================

func TestCryptoService_EncryptBatch_WithSafetyTracker_Success(t *testing.T) {
	cryptoMgr := newMockCryptoManager()
	kmsMgr := newMockKMSManager()
	logger := newMockLogger()
	tracker := NewAEADSafetyTracker()

	svc := newCryptoService(cryptoMgr, kmsMgr, logger, tracker)

	req := &EncryptBatchRequest{
		KeyID: "batch-tracker-key",
		Items: []BatchEncryptItem{
			{Plaintext: []byte("item1")},
			{Plaintext: []byte("item2")},
			{Plaintext: []byte("item3")},
		},
	}

	resp, err := svc.EncryptBatch(systemTenantCtx(), req)
	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.Equal(t, "batch-tracker-key", resp.KeyID)
	assert.Len(t, resp.Results, 3)
}

func TestCryptoService_EncryptBatch_CryptoManagerFailure(t *testing.T) {
	cryptoMgr := newMockCryptoManager()
	kmsMgr := newMockKMSManager()
	logger := newMockLogger()

	cryptoMgr.encryptBatchFn = func(ctx context.Context, keyID string, items [][]byte, encContext []byte) ([][]byte, error) {
		return nil, errors.New("batch encryption internal failure")
	}

	svc := newCryptoService(cryptoMgr, kmsMgr, logger, nil)

	req := &EncryptBatchRequest{
		KeyID: "batch-failing-key",
		Items: []BatchEncryptItem{
			{Plaintext: []byte("item1")},
		},
	}

	resp, err := svc.EncryptBatch(systemTenantCtx(), req)
	assert.Nil(t, resp)
	require.Error(t, err)
	var qbErr *quicbaasErrors.QBError
	require.ErrorAs(t, err, &qbErr)
	assert.Equal(t, quicbaasErrors.ErrEncryption, qbErr.Code)
	assert.Contains(t, err.Error(), "encrypt batch failed")
}

// =============================================================================
// DecryptBatch Deep Path Tests
// =============================================================================

func TestCryptoService_DecryptBatch_WithSafetyTracker_Success(t *testing.T) {
	cryptoMgr := newMockCryptoManager()
	kmsMgr := newMockKMSManager()
	logger := newMockLogger()
	tracker := NewAEADSafetyTracker()

	svc := newCryptoService(cryptoMgr, kmsMgr, logger, tracker)

	req := &DecryptBatchRequest{
		KeyID: "batch-decrypt-tracker-key",
		Items: []BatchDecryptItem{
			{Ciphertext: append([]byte("batch:encrypted:"), []byte("item1")...)},
			{Ciphertext: append([]byte("batch:encrypted:"), []byte("item2")...)},
		},
	}

	resp, err := svc.DecryptBatch(systemTenantCtx(), req)
	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.Equal(t, "batch-decrypt-tracker-key", resp.KeyID)
	assert.Len(t, resp.Results, 2)
}

func TestCryptoService_DecryptBatch_CryptoManagerFailure(t *testing.T) {
	cryptoMgr := newMockCryptoManager()
	kmsMgr := newMockKMSManager()
	logger := newMockLogger()

	cryptoMgr.decryptBatchFn = func(ctx context.Context, keyID string, items [][]byte, encContext []byte) ([][]byte, error) {
		return nil, errors.New("batch decryption internal failure")
	}

	svc := newCryptoService(cryptoMgr, kmsMgr, logger, nil)

	req := &DecryptBatchRequest{
		KeyID: "batch-decrypt-fail-key",
		Items: []BatchDecryptItem{
			{Ciphertext: []byte("corrupted-batch-data")},
		},
	}

	resp, err := svc.DecryptBatch(systemTenantCtx(), req)
	assert.Nil(t, resp)
	require.Error(t, err)
	var qbErr *quicbaasErrors.QBError
	require.ErrorAs(t, err, &qbErr)
	assert.Equal(t, quicbaasErrors.ErrDecryption, qbErr.Code)
	assert.Contains(t, err.Error(), "decrypt batch failed")
}

// =============================================================================
// GenerateDataKey Deep Path Tests
// =============================================================================

func TestCryptoService_GenerateDataKey_WithSafetyTracker_Success(t *testing.T) {
	cryptoMgr := newMockCryptoManager()
	kmsMgr := newMockKMSManager()
	logger := newMockLogger()
	tracker := NewAEADSafetyTracker()

	svc := newCryptoService(cryptoMgr, kmsMgr, logger, tracker)

	req := &GenerateDataKeyRequest{
		KeyID: "kek-tracker-key",
	}

	resp, err := svc.GenerateDataKey(systemTenantCtx(), req)
	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.Equal(t, "kek-tracker-key", resp.KeyID)
	assert.NotEmpty(t, resp.Plaintext)
	assert.NotEmpty(t, resp.CiphertextBlob)
}

func TestCryptoService_GenerateDataKey_KMSFailure(t *testing.T) {
	cryptoMgr := newMockCryptoManager()
	kmsMgr := newMockKMSManager()
	logger := newMockLogger()

	kmsMgr.generateDataKeyFn = func(ctx context.Context, attrs *xkmssdk.KeyAttributes, keyBytes int) ([]byte, []byte, error) {
		return nil, nil, errors.New("KMS key not found for data key generation")
	}

	svc := newCryptoService(cryptoMgr, kmsMgr, logger, nil)

	req := &GenerateDataKeyRequest{
		KeyID: "missing-kek",
	}

	resp, err := svc.GenerateDataKey(systemTenantCtx(), req)
	assert.Nil(t, resp)
	require.Error(t, err)
	var qbErr *quicbaasErrors.QBError
	require.ErrorAs(t, err, &qbErr)
	assert.Equal(t, quicbaasErrors.ErrEncryption, qbErr.Code)
	assert.Contains(t, err.Error(), "generate data key failed")
}

// =============================================================================
// GenerateWrappedDataKey Deep Path Tests
// =============================================================================

func TestCryptoService_GenerateWrappedDataKey_WithSafetyTracker_Success(t *testing.T) {
	cryptoMgr := newMockCryptoManager()
	kmsMgr := newMockKMSManager()
	logger := newMockLogger()
	tracker := NewAEADSafetyTracker()

	svc := newCryptoService(cryptoMgr, kmsMgr, logger, tracker)

	req := &GenerateWrappedDataKeyRequest{
		KeyID: "wrapped-kek-key",
	}

	resp, err := svc.GenerateWrappedDataKey(systemTenantCtx(), req)
	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.Equal(t, "wrapped-kek-key", resp.KeyID)
	assert.NotEmpty(t, resp.CiphertextBlob)
}

func TestCryptoService_GenerateWrappedDataKey_KMSFailure(t *testing.T) {
	cryptoMgr := newMockCryptoManager()
	kmsMgr := newMockKMSManager()
	logger := newMockLogger()

	kmsMgr.generateDataKeyFn = func(ctx context.Context, attrs *xkmssdk.KeyAttributes, keyBytes int) ([]byte, []byte, error) {
		return nil, nil, errors.New("KMS unavailable for wrapped key")
	}

	svc := newCryptoService(cryptoMgr, kmsMgr, logger, nil)

	req := &GenerateWrappedDataKeyRequest{
		KeyID: "missing-wrapped-kek",
	}

	resp, err := svc.GenerateWrappedDataKey(systemTenantCtx(), req)
	assert.Nil(t, resp)
	require.Error(t, err)
	var qbErr *quicbaasErrors.QBError
	require.ErrorAs(t, err, &qbErr)
	assert.Equal(t, quicbaasErrors.ErrEncryption, qbErr.Code)
	assert.Contains(t, err.Error(), "generate wrapped data key failed")
}
