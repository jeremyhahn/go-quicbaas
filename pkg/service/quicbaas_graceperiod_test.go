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
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// =============================================================================
// UpdateKeyStatusForGracePeriod Tests
// =============================================================================

func TestGracePeriodAdapter_UpdateKeyStatusForGracePeriod_Success(t *testing.T) {
	mockMgr := newMockKMSManager()
	logger := newMockLogger()
	kmsImpl := newKMSService(mockMgr, logger)

	// Set up a key that the mock manager can process
	mockMgr.keys["grace-key"] = &mockKeyData{
		keyID:     "grace-key",
		algorithm: "AES-256-GCM",
		version:   5,
		status:    "enabled",
		createdAt: time.Now(),
	}

	adapter := &kmsGracePeriodAdapter{
		kms:   kmsImpl,
		jobID: "grace-job-001",
	}

	result, err := adapter.UpdateKeyStatusForGracePeriod(context.Background(), "grace-key", 3)
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Equal(t, "grace-key", result.KeyID)
	assert.Equal(t, uint64(3), result.Version)
	assert.Equal(t, string(KeyStatusDisabled), result.NewStatus)
}

func TestGracePeriodAdapter_UpdateKeyStatusForGracePeriod_WithAuditLogger(t *testing.T) {
	mockMgr := newMockKMSManager()
	logger := newMockLogger()
	kmsImpl := newKMSService(mockMgr, logger)

	auditLogger := &gracePeriodMockAuditLogger{}
	kmsImpl.auditLogger = auditLogger

	mockMgr.keys["audit-key"] = &mockKeyData{
		keyID:     "audit-key",
		algorithm: "AES-256-GCM",
		version:   2,
		status:    "enabled",
		createdAt: time.Now(),
	}

	adapter := &kmsGracePeriodAdapter{
		kms:   kmsImpl,
		jobID: "audit-job-123",
	}

	result, err := adapter.UpdateKeyStatusForGracePeriod(context.Background(), "audit-key", 1)
	require.NoError(t, err)
	require.NotNil(t, result)

	// Verify audit logger was called
	assert.True(t, auditLogger.gracePeriodDisableCalled)
	assert.Equal(t, "audit-key", auditLogger.keyID)
	assert.Equal(t, uint64(1), auditLogger.version)
	assert.Equal(t, "audit-job-123", auditLogger.jobID)
	assert.True(t, auditLogger.success)
	assert.Empty(t, auditLogger.errorMsg)
}

func TestGracePeriodAdapter_UpdateKeyStatusForGracePeriod_NonexistentKey_WithAuditLogger(t *testing.T) {
	mockMgr := newMockKMSManager()
	logger := newMockLogger()
	kmsImpl := newKMSService(mockMgr, logger)

	auditLogger := &gracePeriodMockAuditLogger{}
	kmsImpl.auditLogger = auditLogger

	// Key does not exist in the mock manager, but the grace period handler
	// uses disableKeyVersion which operates on version-specific status tracking
	// without requiring the key to exist in the KMS manager. This is important
	// for multi-node clusters where the XKMS cache may not have the key
	// on the node executing the scheduler job.
	adapter := &kmsGracePeriodAdapter{
		kms:   kmsImpl,
		jobID: "job-456",
	}

	result, err := adapter.UpdateKeyStatusForGracePeriod(context.Background(), "nonexistent-key", 1)
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Equal(t, "nonexistent-key", result.KeyID)
	assert.Equal(t, uint64(1), result.Version)
	assert.Equal(t, string(KeyStatusDisabled), result.NewStatus)

	// Verify audit logger was called with success
	assert.True(t, auditLogger.gracePeriodDisableCalled)
	assert.Equal(t, "nonexistent-key", auditLogger.keyID)
	assert.True(t, auditLogger.success)
	assert.Empty(t, auditLogger.errorMsg)
}

func TestGracePeriodAdapter_UpdateKeyStatusForGracePeriod_Error_EmptyKeyID(t *testing.T) {
	mockMgr := newMockKMSManager()
	logger := newMockLogger()
	kmsImpl := newKMSService(mockMgr, logger)

	auditLogger := &gracePeriodMockAuditLogger{}
	kmsImpl.auditLogger = auditLogger

	adapter := &kmsGracePeriodAdapter{
		kms:   kmsImpl,
		jobID: "fail-job-789",
	}

	result, err := adapter.UpdateKeyStatusForGracePeriod(context.Background(), "", 1)
	require.Error(t, err)
	assert.Nil(t, result)

	// Verify audit logger was called with error info
	assert.True(t, auditLogger.gracePeriodDisableCalled)
	assert.Equal(t, "", auditLogger.keyID)
	assert.False(t, auditLogger.success)
	assert.NotEmpty(t, auditLogger.errorMsg)
}

func TestGracePeriodAdapter_UpdateKeyStatusForGracePeriod_NoAuditLogger(t *testing.T) {
	mockMgr := newMockKMSManager()
	logger := newMockLogger()
	kmsImpl := newKMSService(mockMgr, logger)

	// Ensure no audit logger is set
	kmsImpl.auditLogger = nil

	mockMgr.keys["noaudit-key"] = &mockKeyData{
		keyID:     "noaudit-key",
		algorithm: "AES-256-GCM",
		version:   1,
		status:    "enabled",
		createdAt: time.Now(),
	}

	adapter := &kmsGracePeriodAdapter{
		kms:   kmsImpl,
		jobID: "noaudit-job",
	}

	result, err := adapter.UpdateKeyStatusForGracePeriod(context.Background(), "noaudit-key", 1)
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Equal(t, "noaudit-key", result.KeyID)
}

// =============================================================================
// disableKeyVersion Tests
// =============================================================================

func TestDisableKeyVersion_Success(t *testing.T) {
	mockMgr := newMockKMSManager()
	logger := newMockLogger()
	kmsImpl := newKMSService(mockMgr, logger)

	resp, err := kmsImpl.disableKeyVersion(context.Background(), "test-key", 1)
	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.Equal(t, "test-key", resp.KeyID)
	assert.Equal(t, uint64(1), resp.Version)
	assert.Equal(t, KeyStatusEnabled, resp.OldStatus)
	assert.Equal(t, KeyStatusDisabled, resp.NewStatus)
}

func TestDisableKeyVersion_AlreadyDisabled(t *testing.T) {
	mockMgr := newMockKMSManager()
	logger := newMockLogger()
	kmsImpl := newKMSService(mockMgr, logger)

	// Disable once
	_, err := kmsImpl.disableKeyVersion(context.Background(), "test-key", 1)
	require.NoError(t, err)

	// Disable again - should be idempotent
	resp, err := kmsImpl.disableKeyVersion(context.Background(), "test-key", 1)
	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.Equal(t, KeyStatusDisabled, resp.OldStatus)
	assert.Equal(t, KeyStatusDisabled, resp.NewStatus)
}

func TestDisableKeyVersion_EmptyKeyID(t *testing.T) {
	mockMgr := newMockKMSManager()
	logger := newMockLogger()
	kmsImpl := newKMSService(mockMgr, logger)

	resp, err := kmsImpl.disableKeyVersion(context.Background(), "", 1)
	require.Error(t, err)
	assert.Nil(t, resp)
}

func TestDisableKeyVersion_ZeroVersion(t *testing.T) {
	mockMgr := newMockKMSManager()
	logger := newMockLogger()
	kmsImpl := newKMSService(mockMgr, logger)

	resp, err := kmsImpl.disableKeyVersion(context.Background(), "test-key", 0)
	require.Error(t, err)
	assert.Nil(t, resp)
}

// =============================================================================
// checkAuth Tests
// =============================================================================

func TestQuicBaaS_CheckAuth_AuthDisabled(t *testing.T) {
	db := &quicBaaS{
		config: &ServiceConfig{
			EnableAuth: false,
		},
	}

	err := db.checkAuth(context.Background(), "read")
	require.NoError(t, err)
}

func TestQuicBaaS_CheckAuth_AuthEnabled_NoToken(t *testing.T) {
	db := &quicBaaS{
		config: &ServiceConfig{
			EnableAuth: true,
		},
	}

	err := db.checkAuth(context.Background(), "write")
	require.Error(t, err)
	// Should fail because no token in context
}

// =============================================================================
// Delegation Success Path Tests
// These test the case where the KMS/Crypto service IS available
// =============================================================================

func TestQuicBaaS_GenerateKey_WithKMSService(t *testing.T) {
	mockMgr := newMockKMSManager()
	logger := newMockLogger()
	kmsImpl := newKMSService(mockMgr, logger)

	db := &quicBaaS{kmsService: kmsImpl}

	// Nil request fails validation in the KMS service
	resp, err := db.GenerateKey(context.Background(), nil)
	assert.Nil(t, resp)
	require.Error(t, err)
}

func TestQuicBaaS_ImportKey_WithKMSService(t *testing.T) {
	mockMgr := newMockKMSManager()
	logger := newMockLogger()
	kmsImpl := newKMSService(mockMgr, logger)

	db := &quicBaaS{kmsService: kmsImpl}

	resp, err := db.ImportKey(context.Background(), nil)
	assert.Nil(t, resp)
	require.Error(t, err)
}

func TestQuicBaaS_GetKey_WithKMSService(t *testing.T) {
	mockMgr := newMockKMSManager()
	logger := newMockLogger()
	kmsImpl := newKMSService(mockMgr, logger)

	db := &quicBaaS{kmsService: kmsImpl}

	resp, err := db.GetKey(context.Background(), nil)
	assert.Nil(t, resp)
	require.Error(t, err)
}

func TestQuicBaaS_ListKeys_WithKMSService(t *testing.T) {
	mockMgr := newMockKMSManager()
	logger := newMockLogger()
	kmsImpl := newKMSService(mockMgr, logger)

	db := &quicBaaS{kmsService: kmsImpl}

	// ListKeys does not validate nil request early, so use empty request
	// which exercises the delegation path and returns an empty result
	resp, err := db.ListKeys(context.Background(), &ListKeysRequest{})
	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.Equal(t, 0, resp.TotalCount)
}

func TestQuicBaaS_RotateKey_WithKMSService(t *testing.T) {
	mockMgr := newMockKMSManager()
	logger := newMockLogger()
	kmsImpl := newKMSService(mockMgr, logger)

	db := &quicBaaS{kmsService: kmsImpl}

	resp, err := db.RotateKey(context.Background(), nil)
	assert.Nil(t, resp)
	require.Error(t, err)
}

func TestQuicBaaS_RevokeKey_WithKMSService(t *testing.T) {
	mockMgr := newMockKMSManager()
	logger := newMockLogger()
	kmsImpl := newKMSService(mockMgr, logger)

	db := &quicBaaS{kmsService: kmsImpl}

	resp, err := db.RevokeKey(context.Background(), nil)
	assert.Nil(t, resp)
	require.Error(t, err)
}

func TestQuicBaaS_DeleteKey_WithKMSService(t *testing.T) {
	mockMgr := newMockKMSManager()
	logger := newMockLogger()
	kmsImpl := newKMSService(mockMgr, logger)

	db := &quicBaaS{kmsService: kmsImpl}

	resp, err := db.DeleteKey(context.Background(), nil)
	assert.Nil(t, resp)
	require.Error(t, err)
}

func TestQuicBaaS_GetKeyMetadata_WithKMSService(t *testing.T) {
	mockMgr := newMockKMSManager()
	logger := newMockLogger()
	kmsImpl := newKMSService(mockMgr, logger)

	db := &quicBaaS{kmsService: kmsImpl}

	resp, err := db.GetKeyMetadata(context.Background(), nil)
	assert.Nil(t, resp)
	require.Error(t, err)
}

func TestQuicBaaS_Encrypt_WithCryptoService(t *testing.T) {
	cryptoMgr := newMockCryptoManager()
	kmsMgr := newMockKMSManager()
	logger := newMockLogger()
	cryptoSvc := newCryptoService(cryptoMgr, kmsMgr, logger, nil)

	db := &quicBaaS{cryptoService: cryptoSvc}

	// Nil request fails validation
	resp, err := db.Encrypt(context.Background(), nil)
	assert.Nil(t, resp)
	require.Error(t, err)
}

func TestQuicBaaS_Decrypt_WithCryptoService(t *testing.T) {
	cryptoMgr := newMockCryptoManager()
	kmsMgr := newMockKMSManager()
	logger := newMockLogger()
	cryptoSvc := newCryptoService(cryptoMgr, kmsMgr, logger, nil)

	db := &quicBaaS{cryptoService: cryptoSvc}

	// Nil request fails validation
	resp, err := db.Decrypt(context.Background(), nil)
	assert.Nil(t, resp)
	require.Error(t, err)
}

// =============================================================================
// Close Method Deep Path Tests
// =============================================================================

func TestQuicBaaS_Close_WithLogger(t *testing.T) {
	logger := newMockLogger()
	db := &quicBaaS{
		closed: false,
		logger: logger,
	}

	err := db.Close()
	require.NoError(t, err)
	assert.True(t, db.closed)
}

func TestQuicBaaS_Close_MultipleCallsIdempotent(t *testing.T) {
	db := &quicBaaS{
		closed: false,
	}

	err1 := db.Close()
	require.NoError(t, err1)
	assert.True(t, db.closed)

	// Second call should be no-op
	err2 := db.Close()
	require.NoError(t, err2)
}
