//go:build !integration

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
	"bytes"
	"context"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ---------------------------------------------------------------------------
// EncryptionManager - comprehensive coverage
// ---------------------------------------------------------------------------

func TestEncMgr_NilConfigReturnsError(t *testing.T) {
	log := createTestLogger(t)
	em, err := NewEncryptionManager(nil, log)
	assert.Error(t, err)
	assert.Nil(t, em)
	assert.Contains(t, err.Error(), "nil")
}

func TestEncMgr_DisabledReturnsNilKeyring(t *testing.T) {
	log := createTestLogger(t)
	cfg := &EncryptionConfig{Enabled: false}
	em, err := NewEncryptionManager(cfg, log)
	require.NoError(t, err)
	assert.NotNil(t, em)
	assert.Nil(t, em.keyring)
}

func TestEncMgr_DefaultsAreApplied(t *testing.T) {
	log := createTestLogger(t)
	cfg := &EncryptionConfig{Enabled: true}
	em, err := NewEncryptionManager(cfg, log)
	require.NoError(t, err)
	assert.Equal(t, DefaultAlgorithm, em.config.Algorithm)
	assert.Equal(t, 90, em.config.RotateDays)
	assert.Equal(t, "local", em.config.KeyProvider)
	assert.Equal(t, "quicbaas-master-key", em.config.KeyID)
	em.keyring.Close()
}

func TestEncMgr_UnsupportedAlgoFails(t *testing.T) {
	log := createTestLogger(t)
	cfg := &EncryptionConfig{
		Enabled:     true,
		Algorithm:   "INVALID-ALGO",
		KeyProvider: "local",
	}
	em, err := NewEncryptionManager(cfg, log)
	assert.Error(t, err)
	assert.Nil(t, em)
}

func TestEncMgr_EncryptDecryptRoundTrip(t *testing.T) {
	log := createTestLogger(t)
	cfg := &EncryptionConfig{
		Enabled:     true,
		KeyProvider: "local",
		Algorithm:   AlgorithmAES256GCM,
	}
	em, err := NewEncryptionManager(cfg, log)
	require.NoError(t, err)
	defer em.keyring.Close()

	plaintext := []byte("hello world - coverage test")
	ciphertext, err := em.Encrypt(plaintext)
	require.NoError(t, err)
	assert.NotEqual(t, plaintext, ciphertext)

	decrypted, err := em.Decrypt(ciphertext)
	require.NoError(t, err)
	assert.Equal(t, plaintext, decrypted)
}

func TestEncMgr_EncryptPassthroughDisabled(t *testing.T) {
	log := createTestLogger(t)
	em, err := NewEncryptionManager(&EncryptionConfig{Enabled: false}, log)
	require.NoError(t, err)

	data := []byte("passthrough")
	result, err := em.Encrypt(data)
	require.NoError(t, err)
	assert.Equal(t, data, result)
}

func TestEncMgr_DecryptPassthroughDisabled(t *testing.T) {
	log := createTestLogger(t)
	em, err := NewEncryptionManager(&EncryptionConfig{Enabled: false}, log)
	require.NoError(t, err)

	data := []byte("passthrough")
	result, err := em.Decrypt(data)
	require.NoError(t, err)
	assert.Equal(t, data, result)
}

func TestEncMgr_RotateKeysSucceeds(t *testing.T) {
	log := createTestLogger(t)
	cfg := &EncryptionConfig{
		Enabled:     true,
		KeyProvider: "local",
		Algorithm:   AlgorithmAES256GCM,
	}
	em, err := NewEncryptionManager(cfg, log)
	require.NoError(t, err)
	defer em.keyring.Close()

	assert.Equal(t, byte(1), em.GetKeyVersion())

	err = em.RotateKeys(context.Background())
	require.NoError(t, err)
	assert.Equal(t, byte(2), em.GetKeyVersion())
}

func TestEncMgr_RotateKeysNoopDisabled(t *testing.T) {
	log := createTestLogger(t)
	em, err := NewEncryptionManager(&EncryptionConfig{Enabled: false}, log)
	require.NoError(t, err)
	assert.NoError(t, em.RotateKeys(context.Background()))
}

func TestEncMgr_GetKeyVersionZeroDisabled(t *testing.T) {
	log := createTestLogger(t)
	em, err := NewEncryptionManager(&EncryptionConfig{Enabled: false}, log)
	require.NoError(t, err)
	assert.Equal(t, byte(0), em.GetKeyVersion())
}

func TestEncMgr_GetKeyVersionZeroNoActiveKey(t *testing.T) {
	log := createTestLogger(t)
	cfg := &EncryptionConfig{Enabled: true}
	em := &EncryptionManager{
		config: cfg,
		logger: log,
		keyring: &Keyring{
			config:     cfg,
			logger:     log,
			currentKey: nil,
		},
	}
	assert.Equal(t, byte(0), em.GetKeyVersion())
}

func TestEncMgr_EncryptBatchPassthroughDisabled(t *testing.T) {
	log := createTestLogger(t)
	em, err := NewEncryptionManager(&EncryptionConfig{Enabled: false}, log)
	require.NoError(t, err)

	data := [][]byte{[]byte("a"), []byte("b")}
	result, err := em.EncryptBatch(data)
	require.NoError(t, err)
	assert.Equal(t, data, result)
}

func TestEncMgr_DecryptBatchPassthroughDisabled(t *testing.T) {
	log := createTestLogger(t)
	em, err := NewEncryptionManager(&EncryptionConfig{Enabled: false}, log)
	require.NoError(t, err)

	data := [][]byte{[]byte("a"), []byte("b")}
	result, err := em.DecryptBatch(data)
	require.NoError(t, err)
	assert.Equal(t, data, result)
}

func TestEncMgr_EncryptBatchRoundTrip(t *testing.T) {
	log := createTestLogger(t)
	cfg := &EncryptionConfig{
		Enabled:     true,
		KeyProvider: "local",
		Algorithm:   AlgorithmAES256GCM,
	}
	em, err := NewEncryptionManager(cfg, log)
	require.NoError(t, err)
	defer em.keyring.Close()

	plaintexts := [][]byte{[]byte("hello"), []byte("world"), []byte("test")}
	ciphertexts, err := em.EncryptBatch(plaintexts)
	require.NoError(t, err)
	assert.Len(t, ciphertexts, 3)

	decrypted, err := em.DecryptBatch(ciphertexts)
	require.NoError(t, err)
	assert.Equal(t, plaintexts, decrypted)
}

func TestEncMgr_DecryptBatchInvalidReturnsError(t *testing.T) {
	log := createTestLogger(t)
	cfg := &EncryptionConfig{
		Enabled:     true,
		KeyProvider: "local",
		Algorithm:   AlgorithmAES256GCM,
	}
	em, err := NewEncryptionManager(cfg, log)
	require.NoError(t, err)
	defer em.keyring.Close()

	invalid := [][]byte{[]byte("too short")}
	result, err := em.DecryptBatch(invalid)
	assert.Error(t, err)
	assert.Nil(t, result)
}

func TestEncMgr_EncryptDecryptAfterRotation(t *testing.T) {
	log := createTestLogger(t)
	cfg := &EncryptionConfig{
		Enabled:     true,
		KeyProvider: "local",
		Algorithm:   AlgorithmAES256GCM,
	}
	em, err := NewEncryptionManager(cfg, log)
	require.NoError(t, err)
	defer em.keyring.Close()

	// Rotate to v2
	require.NoError(t, em.RotateKeys(context.Background()))
	assert.Equal(t, byte(2), em.GetKeyVersion())

	// Encrypt and decrypt with v2
	pt := []byte("data after rotation")
	ct, err := em.Encrypt(pt)
	require.NoError(t, err)

	dec, err := em.Decrypt(ct)
	require.NoError(t, err)
	assert.Equal(t, pt, dec)

	// Verify ciphertext has v2 version byte
	assert.Equal(t, byte(2), ct[0])
}

// ---------------------------------------------------------------------------
// Keyring - comprehensive coverage
// ---------------------------------------------------------------------------

func TestKR_GetCurrentKeyActive(t *testing.T) {
	log := createTestLogger(t)
	cfg := &EncryptionConfig{KeyProvider: "local", Algorithm: AlgorithmAES256GCM}
	kr, err := NewKeyring(cfg, log)
	require.NoError(t, err)
	defer kr.Close()

	key := kr.GetCurrentKey()
	assert.NotNil(t, key)
	assert.Equal(t, byte(1), key.Version)
	assert.True(t, key.Active)
}

func TestKR_GetKeyNonExistentReturnsNil(t *testing.T) {
	log := createTestLogger(t)
	cfg := &EncryptionConfig{KeyProvider: "local", Algorithm: AlgorithmAES256GCM}
	kr, err := NewKeyring(cfg, log)
	require.NoError(t, err)
	defer kr.Close()

	assert.Nil(t, kr.GetKey(99))
}

func TestKR_EncryptDecryptSuccess(t *testing.T) {
	log := createTestLogger(t)
	cfg := &EncryptionConfig{KeyProvider: "local", Algorithm: AlgorithmAES256GCM}
	kr, err := NewKeyring(cfg, log)
	require.NoError(t, err)
	defer kr.Close()

	encrypted, err := kr.Encrypt([]byte("keyring data"))
	require.NoError(t, err)
	assert.NotNil(t, encrypted)

	decrypted, err := kr.Decrypt(encrypted)
	require.NoError(t, err)
	assert.Equal(t, []byte("keyring data"), decrypted)
}

func TestKR_RotateKeyDeactivatesOld(t *testing.T) {
	log := createTestLogger(t)
	cfg := &EncryptionConfig{KeyProvider: "local", Algorithm: AlgorithmAES256GCM}
	kr, err := NewKeyring(cfg, log)
	require.NoError(t, err)
	defer kr.Close()

	newKey, err := kr.RotateKey(context.Background())
	require.NoError(t, err)
	assert.Equal(t, byte(2), newKey.Version)
	assert.True(t, newKey.Active)

	oldKey := kr.GetKey(1)
	assert.False(t, oldKey.Active)
}

func TestKR_GetKeyMetadataAfterRotation(t *testing.T) {
	log := createTestLogger(t)
	cfg := &EncryptionConfig{KeyProvider: "local", Algorithm: AlgorithmAES256GCM}
	kr, err := NewKeyring(cfg, log)
	require.NoError(t, err)
	defer kr.Close()

	assert.Len(t, kr.GetKeyMetadata(), 1)
	_, err = kr.RotateKey(context.Background())
	require.NoError(t, err)
	assert.Len(t, kr.GetKeyMetadata(), 2)
}

func TestKR_InitXKMSLocalWithBadKeyStorage(t *testing.T) {
	log := createTestLogger(t)
	cfg := &EncryptionConfig{
		KeyProvider: "local",
		Algorithm:   AlgorithmAES256GCM,
		KeyStorage:  "not-a-storage-backend",
	}
	kr := &Keyring{config: cfg, logger: log}
	err := kr.initializeXKMS()
	assert.NoError(t, err)
	assert.NotNil(t, kr.backend)
}

func TestKR_CloseWithoutTimerNoPanic(t *testing.T) {
	log := createTestLogger(t)
	cfg := &EncryptionConfig{
		KeyProvider: "local",
		Algorithm:   AlgorithmAES256GCM,
		RotateDays:  0,
	}
	kr, err := NewKeyring(cfg, log)
	require.NoError(t, err)
	kr.Close()
}

func TestKR_AutoRotateSuccess(t *testing.T) {
	log := createTestLogger(t)
	cfg := &EncryptionConfig{
		KeyProvider: "local",
		Algorithm:   AlgorithmAES256GCM,
		RotateDays:  1,
	}
	kr, err := NewKeyring(cfg, log)
	require.NoError(t, err)
	defer kr.Close()

	err = kr.autoRotate()
	require.NoError(t, err)
	assert.Equal(t, byte(2), kr.GetCurrentKey().Version)
}

// ---------------------------------------------------------------------------
// SecretManager - comprehensive coverage
// ---------------------------------------------------------------------------

func TestSM_NilConfigGetsDefaults(t *testing.T) {
	log := createTestLogger(t)
	sm, err := NewSecretManager(nil, log)
	require.NoError(t, err)
	assert.Equal(t, 5*time.Minute, sm.config.CacheTTL)
	sm.Close()
}

func TestSM_WithCacheCreatesCache(t *testing.T) {
	log := createTestLogger(t)
	sm, err := NewSecretManager(&SecretConfig{CacheEnabled: true, CacheTTL: 30 * time.Second}, log)
	require.NoError(t, err)
	assert.NotNil(t, sm.cache)
	sm.Close()
}

func TestSM_GetRetrievesEnvVar(t *testing.T) {
	log := createTestLogger(t)
	sm, err := NewSecretManager(&SecretConfig{}, log)
	require.NoError(t, err)
	defer sm.Close()

	t.Setenv("COV_TEST_GET", "cov-value")
	ctx := context.Background()
	secret, err := sm.Get(ctx, "COV_TEST_GET")
	require.NoError(t, err)
	assert.Equal(t, "cov-value", secret.Value)
}

func TestSM_GetMissingReturnsError(t *testing.T) {
	log := createTestLogger(t)
	sm, err := NewSecretManager(&SecretConfig{}, log)
	require.NoError(t, err)
	defer sm.Close()

	ctx := context.Background()
	_, err = sm.Get(ctx, "COV_NONEXIST_XYZ")
	assert.Error(t, err)
}

func TestSM_GetCacheHitSecondCall(t *testing.T) {
	log := createTestLogger(t)
	sm, err := NewSecretManager(&SecretConfig{CacheEnabled: true, CacheTTL: 1 * time.Minute}, log)
	require.NoError(t, err)
	defer sm.Close()

	t.Setenv("COV_CACHE_HIT", "cached")
	ctx := context.Background()

	s1, err := sm.Get(ctx, "COV_CACHE_HIT")
	require.NoError(t, err)
	assert.Equal(t, "cached", s1.Value)

	s2, err := sm.Get(ctx, "COV_CACHE_HIT")
	require.NoError(t, err)
	assert.Equal(t, "cached", s2.Value)
}

func TestSM_SetErrorOnEnvBackend(t *testing.T) {
	log := createTestLogger(t)
	sm, err := NewSecretManager(&SecretConfig{CacheEnabled: true, CacheTTL: 1 * time.Minute}, log)
	require.NoError(t, err)
	defer sm.Close()

	err = sm.Set(context.Background(), "KEY", "val", nil)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not supported")
}

func TestSM_DeleteErrorOnEnvBackend(t *testing.T) {
	log := createTestLogger(t)
	sm, err := NewSecretManager(&SecretConfig{CacheEnabled: true, CacheTTL: 1 * time.Minute}, log)
	require.NoError(t, err)
	defer sm.Close()

	err = sm.Delete(context.Background(), "KEY")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not supported")
}

func TestSM_CloseNoError(t *testing.T) {
	log := createTestLogger(t)
	sm, err := NewSecretManager(&SecretConfig{CacheEnabled: true, CacheTTL: 1 * time.Minute}, log)
	require.NoError(t, err)
	assert.NoError(t, sm.Close())
}

func TestSM_GetStringReturnsValue(t *testing.T) {
	log := createTestLogger(t)
	sm, err := NewSecretManager(&SecretConfig{}, log)
	require.NoError(t, err)
	defer sm.Close()

	t.Setenv("COV_GETSTR", "str-val")
	val, err := sm.GetString(context.Background(), "COV_GETSTR")
	require.NoError(t, err)
	assert.Equal(t, "str-val", val)
}

func TestSM_GetStringMissingReturnsError(t *testing.T) {
	log := createTestLogger(t)
	sm, err := NewSecretManager(&SecretConfig{}, log)
	require.NoError(t, err)
	defer sm.Close()

	val, err := sm.GetString(context.Background(), "COV_NONEXIST_STR")
	assert.Error(t, err)
	assert.Empty(t, val)
}

func TestSM_GetWithDefaultReturnsActual(t *testing.T) {
	log := createTestLogger(t)
	sm, err := NewSecretManager(&SecretConfig{}, log)
	require.NoError(t, err)
	defer sm.Close()

	t.Setenv("COV_GETDEF", "actual")
	val := sm.GetWithDefault(context.Background(), "COV_GETDEF", "fallback")
	assert.Equal(t, "actual", val)
}

func TestSM_GetWithDefaultReturnsFallback(t *testing.T) {
	log := createTestLogger(t)
	sm, err := NewSecretManager(&SecretConfig{}, log)
	require.NoError(t, err)
	defer sm.Close()

	val := sm.GetWithDefault(context.Background(), "COV_NONEXIST_DEF", "fallback")
	assert.Equal(t, "fallback", val)
}

// ---------------------------------------------------------------------------
// EnvBackend - comprehensive coverage
// ---------------------------------------------------------------------------

func TestEB_GetSuccessReturnsSecret(t *testing.T) {
	log := createTestLogger(t)
	eb, err := NewEnvBackend(log)
	require.NoError(t, err)

	t.Setenv("COV_EB_GET", "env-val")
	s, err := eb.Get(context.Background(), "COV_EB_GET")
	require.NoError(t, err)
	assert.Equal(t, "env-val", s.Value)
}

func TestEB_GetMissingReturnsError(t *testing.T) {
	log := createTestLogger(t)
	eb, err := NewEnvBackend(log)
	require.NoError(t, err)

	_, err = eb.Get(context.Background(), "COV_EB_NONEXIST")
	assert.Error(t, err)
}

func TestEB_SetReturnsUnsupported(t *testing.T) {
	log := createTestLogger(t)
	eb, err := NewEnvBackend(log)
	require.NoError(t, err)
	assert.Error(t, eb.Set(context.Background(), "k", &Secret{}))
}

func TestEB_DeleteReturnsUnsupported(t *testing.T) {
	log := createTestLogger(t)
	eb, err := NewEnvBackend(log)
	require.NoError(t, err)
	assert.Error(t, eb.Delete(context.Background(), "k"))
}

func TestEB_ListReturnsUnsupported(t *testing.T) {
	log := createTestLogger(t)
	eb, err := NewEnvBackend(log)
	require.NoError(t, err)
	keys, err := eb.List(context.Background(), "prefix")
	assert.Error(t, err)
	assert.Nil(t, keys)
}

// ---------------------------------------------------------------------------
// SecretCache - comprehensive coverage
// ---------------------------------------------------------------------------

func TestSC_SetGetDeleteCycle(t *testing.T) {
	cache := NewSecretCache(1 * time.Minute)
	cache.Set("k1", &Secret{Key: "k1", Value: "v1", Version: 1, CreatedAt: time.Now()})

	got := cache.Get("k1")
	assert.NotNil(t, got)
	assert.Equal(t, "v1", got.Value)

	cache.Delete("k1")
	assert.Nil(t, cache.Get("k1"))
}

func TestSC_GetNonExistentReturnsNil(t *testing.T) {
	cache := NewSecretCache(1 * time.Minute)
	assert.Nil(t, cache.Get("nope"))
}

func TestSC_CleanupRemovesExpired(t *testing.T) {
	cache := NewSecretCache(1 * time.Millisecond)
	cache.Set("k", &Secret{Key: "k", Value: "v", Version: 1, CreatedAt: time.Now()})

	time.Sleep(5 * time.Millisecond)
	cache.cleanup()
	assert.Nil(t, cache.Get("k"))
}

func TestSC_CloseClearsAll(t *testing.T) {
	cache := NewSecretCache(1 * time.Minute)
	cache.Set("k", &Secret{Key: "k", Value: "v", Version: 1, CreatedAt: time.Now()})
	cache.Close()
	assert.Nil(t, cache.Get("k"))
}

// ---------------------------------------------------------------------------
// AuditLogger convenience methods - comprehensive coverage
// ---------------------------------------------------------------------------

func newTestAuditLogger(t *testing.T) (*AuditLogger, *bytes.Buffer) {
	t.Helper()
	log := createTestLogger(t)
	var buf bytes.Buffer
	al := &AuditLogger{
		config:   &AuditConfig{Enabled: true},
		logger:   log,
		writer:   &buf,
		shutdown: make(chan struct{}),
	}
	return al, &buf
}

func TestAL_LogAuthSuccessOutputsCorrectFields(t *testing.T) {
	al, buf := newTestAuditLogger(t)
	require.NoError(t, al.LogAuthSuccess(context.Background(), "admin", "uid-1", "10.0.0.1"))
	assert.Contains(t, buf.String(), "authenticate")
	assert.Contains(t, buf.String(), "success")
	assert.Contains(t, buf.String(), "uid-1")
}

func TestAL_LogAuthFailureOutputsCorrectFields(t *testing.T) {
	al, buf := newTestAuditLogger(t)
	require.NoError(t, al.LogAuthFailure(context.Background(), "baduser", "10.0.0.1", "bad creds"))
	assert.Contains(t, buf.String(), "authenticate")
	assert.Contains(t, buf.String(), "failure")
	assert.Contains(t, buf.String(), "bad creds")
}

func TestAL_LogAuthzDeniedOutputsCorrectFields(t *testing.T) {
	al, buf := newTestAuditLogger(t)
	require.NoError(t, al.LogAuthzDenied(context.Background(), "user1", "uid1", "db", "drop", "10.0.0.1"))
	assert.Contains(t, buf.String(), "authorize")
	assert.Contains(t, buf.String(), "denied")
}

func TestAL_LogDataAccessSuccessAndFailure(t *testing.T) {
	al, buf := newTestAuditLogger(t)

	require.NoError(t, al.LogDataAccess(context.Background(), "admin", "uid1", ActionRead, "users/1", "10.0.0.1", true))
	assert.Contains(t, buf.String(), "success")

	buf.Reset()
	require.NoError(t, al.LogDataAccess(context.Background(), "admin", "uid1", ActionWrite, "users/1", "10.0.0.1", false))
	assert.Contains(t, buf.String(), "failure")
}

func TestAL_LogClusterChangeOutputsCorrectFields(t *testing.T) {
	al, buf := newTestAuditLogger(t)
	require.NoError(t, al.LogClusterChange(context.Background(), ActionClusterJoin, 42, map[string]string{"reason": "scaling"}))
	assert.Contains(t, buf.String(), "cluster.join")
}

func TestAL_LogBackupOperationOutputsCorrectFields(t *testing.T) {
	al, buf := newTestAuditLogger(t)
	require.NoError(t, al.LogBackupOperation(context.Background(), "admin", "uid1", ActionBackup, map[string]string{"path": "/data"}))
	assert.Contains(t, buf.String(), "backup")
}

func TestAL_LogKeyRotationOutputsCorrectFields(t *testing.T) {
	al, buf := newTestAuditLogger(t)
	require.NoError(t, al.LogKeyRotation(context.Background(), 1, 2))
	assert.Contains(t, buf.String(), "key.rotate")
	assert.Contains(t, buf.String(), "old_version")
}

func TestAL_LogKeyExportSuccessAndFailure(t *testing.T) {
	al, buf := newTestAuditLogger(t)

	require.NoError(t, al.LogKeyExport(context.Background(), "admin", "uid1", "key-1", "AES-256", "wrap-1", "RSA", "10.0.0.1", true))
	assert.Contains(t, buf.String(), "key.export")
	assert.Contains(t, buf.String(), "success")

	buf.Reset()
	require.NoError(t, al.LogKeyExport(context.Background(), "admin", "uid1", "key-1", "AES-256", "wrap-1", "RSA", "10.0.0.1", false))
	assert.Contains(t, buf.String(), "failure")
}

func TestAL_LogKeyGenerateOutputsCorrectFields(t *testing.T) {
	al, buf := newTestAuditLogger(t)

	require.NoError(t, al.LogKeyGenerate(context.Background(), "admin", "uid1", "key-1", "test-key", "AES-256", "10.0.0.1", true))
	assert.Contains(t, buf.String(), "key.generate")

	buf.Reset()
	require.NoError(t, al.LogKeyGenerate(context.Background(), "admin", "uid1", "key-1", "test-key", "AES-256", "10.0.0.1", false))
	assert.Contains(t, buf.String(), "failure")
}

func TestAL_LogKeyImportSuccessAndFailure(t *testing.T) {
	al, buf := newTestAuditLogger(t)

	require.NoError(t, al.LogKeyImport(context.Background(), "admin", "uid1", "key-1", "imported", "AES-256", "10.0.0.1", true))
	assert.Contains(t, buf.String(), "key.import")

	buf.Reset()
	require.NoError(t, al.LogKeyImport(context.Background(), "admin", "uid1", "key-1", "imported", "AES-256", "10.0.0.1", false))
	assert.Contains(t, buf.String(), "failure")
}

func TestAL_LogKeyDeletionSuccessAndFailure(t *testing.T) {
	al, buf := newTestAuditLogger(t)

	require.NoError(t, al.LogKeyDeletion(context.Background(), "admin", "uid1", "key-1", "10.0.0.1", true))
	assert.Contains(t, buf.String(), "key.delete")

	buf.Reset()
	require.NoError(t, al.LogKeyDeletion(context.Background(), "admin", "uid1", "key-1", "10.0.0.1", false))
	assert.Contains(t, buf.String(), "failure")
}

func TestAL_LogTenantSwitchWithAssignment(t *testing.T) {
	al, buf := newTestAuditLogger(t)
	require.NoError(t, al.LogTenantSwitch(context.Background(), "admin", "uid1", "t-a", "t-b", "maintenance", "assign-123", "10.0.0.1"))
	assert.Contains(t, buf.String(), "tenant.switch")
	assert.Contains(t, buf.String(), "assign-123")
}

func TestAL_LogTenantSwitchWithoutAssignment(t *testing.T) {
	al, buf := newTestAuditLogger(t)
	require.NoError(t, al.LogTenantSwitch(context.Background(), "admin", "uid1", "t-a", "t-b", "maintenance", "", "10.0.0.1"))
	assert.Contains(t, buf.String(), "tenant.switch")
	assert.NotContains(t, buf.String(), "assignment_id")
}

func TestAL_LogCrossTenantReadOutputsCorrectFields(t *testing.T) {
	al, buf := newTestAuditLogger(t)
	require.NoError(t, al.LogCrossTenantRead(context.Background(), "admin", "uid1", "t-a", "t-b", "users", "audit", "10.0.0.1"))
	assert.Contains(t, buf.String(), "cross_tenant.read")
}

func TestAL_LogCrossTenantWriteOutputsCorrectFields(t *testing.T) {
	al, buf := newTestAuditLogger(t)
	require.NoError(t, al.LogCrossTenantWrite(context.Background(), "admin", "uid1", "t-a", "t-b", "data", "migration", "10.0.0.1"))
	assert.Contains(t, buf.String(), "cross_tenant.write")
}

func TestAL_LogCrossTenantDeleteOutputsCorrectFields(t *testing.T) {
	al, buf := newTestAuditLogger(t)
	require.NoError(t, al.LogCrossTenantDelete(context.Background(), "admin", "uid1", "t-a", "t-b", "records", "cleanup", "10.0.0.1"))
	assert.Contains(t, buf.String(), "cross_tenant.delete")
}

func TestAL_LogTenantAccessDeniedOutputsCorrectFields(t *testing.T) {
	al, buf := newTestAuditLogger(t)
	require.NoError(t, al.LogTenantAccessDenied(context.Background(), "user1", "uid1", "t-a", "t-b", "secrets", "10.0.0.1", "not authorized"))
	assert.Contains(t, buf.String(), "tenant.access.denied")
	assert.Contains(t, buf.String(), "denied")
}

func TestAL_LogTenantDEKOperationSuccessAndFailure(t *testing.T) {
	al, buf := newTestAuditLogger(t)

	require.NoError(t, al.LogTenantDEKOperation(context.Background(), ActionTenantDEKCreate, "t-1", "admin", "uid1", "10.0.0.1", true))
	assert.Contains(t, buf.String(), "tenant.dek.create")
	assert.Contains(t, buf.String(), "success")

	buf.Reset()
	require.NoError(t, al.LogTenantDEKOperation(context.Background(), ActionTenantDEKRotate, "t-1", "admin", "uid1", "10.0.0.1", false))
	assert.Contains(t, buf.String(), "failure")
}

func TestAL_SignEventDeterministic(t *testing.T) {
	log := createTestLogger(t)
	al := &AuditLogger{
		config:   &AuditConfig{Enabled: true, SignLogs: true, SignKey: "my-secret"},
		logger:   log,
		writer:   &bytes.Buffer{},
		signKey:  []byte("my-secret"),
		shutdown: make(chan struct{}),
	}

	event := &AuditEvent{
		Timestamp: time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC),
		Username:  "admin",
		Action:    ActionRead,
		Resource:  "data",
		Result:    ResultSuccess,
		IPAddress: "10.0.0.1",
	}

	sig1, err := al.signEvent(event)
	require.NoError(t, err)
	assert.NotEmpty(t, sig1)

	sig2, err := al.signEvent(event)
	require.NoError(t, err)
	assert.Equal(t, sig1, sig2)
}

func TestAL_VerifySignatureCorrectSig(t *testing.T) {
	log := createTestLogger(t)
	al := &AuditLogger{
		config:   &AuditConfig{Enabled: true, SignLogs: true, SignKey: "key"},
		logger:   log,
		writer:   &bytes.Buffer{},
		signKey:  []byte("key"),
		shutdown: make(chan struct{}),
	}

	event := &AuditEvent{
		Timestamp: time.Date(2025, 6, 1, 0, 0, 0, 0, time.UTC),
		Username:  "admin",
		Action:    ActionRead,
		Resource:  "data",
		Result:    ResultSuccess,
		IPAddress: "10.0.0.1",
	}

	sig, err := al.signEvent(event)
	require.NoError(t, err)
	event.Signature = sig

	assert.True(t, al.VerifySignature(event))
}

func TestAL_LogWithSigningInjectsSignature(t *testing.T) {
	log := createTestLogger(t)
	var buf bytes.Buffer
	al := &AuditLogger{
		config:   &AuditConfig{Enabled: true, SignLogs: true, SignKey: "sign"},
		logger:   log,
		writer:   &buf,
		signKey:  []byte("sign"),
		shutdown: make(chan struct{}),
	}

	require.NoError(t, al.Log(context.Background(), &AuditEvent{
		Username: "admin",
		Action:   ActionRead,
		Resource: "data",
		Result:   ResultSuccess,
	}))
	assert.Contains(t, buf.String(), "signature")
}

func TestAL_LogDisabledNoOp(t *testing.T) {
	log := createTestLogger(t)
	al := &AuditLogger{
		config:   &AuditConfig{Enabled: false},
		logger:   log,
		shutdown: make(chan struct{}),
	}
	assert.NoError(t, al.Log(context.Background(), &AuditEvent{Action: ActionRead}))
}

func TestAL_FlushNilFileNoError(t *testing.T) {
	log := createTestLogger(t)
	al := &AuditLogger{
		config:   &AuditConfig{Enabled: true},
		logger:   log,
		file:     nil,
		shutdown: make(chan struct{}),
	}
	assert.NoError(t, al.Flush())
}

func TestAL_FlushWithRealFile(t *testing.T) {
	log := createTestLogger(t)
	f, err := os.Create(filepath.Join(t.TempDir(), "flush.log"))
	require.NoError(t, err)
	defer f.Close()

	al := &AuditLogger{
		config:   &AuditConfig{Enabled: true},
		logger:   log,
		file:     f,
		shutdown: make(chan struct{}),
	}
	assert.NoError(t, al.Flush())
}

func TestAL_GetSyslogExporterNil(t *testing.T) {
	log := createTestLogger(t)
	al := &AuditLogger{
		config:   &AuditConfig{Enabled: true},
		logger:   log,
		shutdown: make(chan struct{}),
	}
	assert.Nil(t, al.GetSyslogExporter())
}

func TestAL_CloseNoExportersNoError(t *testing.T) {
	log := createTestLogger(t)
	al := &AuditLogger{
		config:   &AuditConfig{Enabled: true},
		logger:   log,
		writer:   &bytes.Buffer{},
		shutdown: make(chan struct{}),
	}
	assert.NoError(t, al.Close())
}

// ---------------------------------------------------------------------------
// SyslogExporter - comprehensive coverage
// ---------------------------------------------------------------------------

func TestSysExp_NilConfigError(t *testing.T) {
	log := createTestLogger(t)
	exp, err := NewSyslogExporter(nil, log)
	assert.Error(t, err)
	assert.Nil(t, exp)
	var sysErr *SyslogExporterError
	assert.True(t, errors.As(err, &sysErr))
}

func TestSysExp_RemoteUDPExport(t *testing.T) {
	addr, err := net.ResolveUDPAddr("udp", "127.0.0.1:0")
	require.NoError(t, err)
	conn, err := net.ListenUDP("udp", addr)
	require.NoError(t, err)
	defer conn.Close()

	log := createTestLogger(t)
	exp, err := NewSyslogExporter(&SyslogExporterConfig{
		Address:  conn.LocalAddr().String(),
		Network:  "udp",
		Facility: SyslogFacilityLocal0,
		Tag:      "cov-test",
		Hostname: "cov-host",
	}, log)
	require.NoError(t, err)
	defer exp.Close()

	assert.True(t, exp.IsConnected())

	require.NoError(t, exp.Export(&AuditEvent{
		Timestamp: time.Now(),
		EventID:   "evt-cov",
		Username:  "admin",
		Action:    ActionAuthenticate,
		Resource:  "auth",
		Result:    ResultSuccess,
		IPAddress: "10.0.0.1",
		NodeID:    "node-1",
		ShardID:   42,
		UserID:    "uid-1",
	}))

	buf := make([]byte, 4096)
	conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	n, _, readErr := conn.ReadFromUDP(buf)
	require.NoError(t, readErr)
	assert.Greater(t, n, 0)
	assert.Contains(t, string(buf[:n]), "QuicBaaS Audit")
}

func TestSysExp_RemoteTCPExport(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	defer ln.Close()

	var received []byte
	var mu sync.Mutex
	done := make(chan struct{})
	go func() {
		defer close(done)
		c, err := ln.Accept()
		if err != nil {
			return
		}
		defer c.Close()
		buf := make([]byte, 4096)
		n, _ := c.Read(buf)
		mu.Lock()
		received = buf[:n]
		mu.Unlock()
	}()

	log := createTestLogger(t)
	exp, err := NewSyslogExporter(&SyslogExporterConfig{
		Address:  ln.Addr().String(),
		Network:  "tcp",
		Facility: SyslogFacilityAuth,
		Tag:      "cov-tcp",
		Hostname: "host1",
	}, log)
	require.NoError(t, err)
	defer exp.Close()

	require.NoError(t, exp.Export(&AuditEvent{
		Timestamp: time.Now(),
		EventID:   "evt-tcp",
		Username:  "user1",
		Action:    ActionWrite,
		Resource:  "data",
		Result:    ResultFailure,
		IPAddress: "192.168.1.1",
		ErrorMsg:  "denied",
		Details:   map[string]string{"key": "value"},
	}))

	<-done
	mu.Lock()
	assert.NotEmpty(t, received)
	mu.Unlock()
}

func TestSysExp_ReconnectCycle(t *testing.T) {
	addr, err := net.ResolveUDPAddr("udp", "127.0.0.1:0")
	require.NoError(t, err)
	conn, err := net.ListenUDP("udp", addr)
	require.NoError(t, err)
	defer conn.Close()

	log := createTestLogger(t)
	exp, err := NewSyslogExporter(&SyslogExporterConfig{
		Address: conn.LocalAddr().String(),
		Network: "udp",
		Tag:     "cov",
	}, log)
	require.NoError(t, err)

	exp.Close()
	assert.False(t, exp.IsConnected())

	require.NoError(t, exp.Reconnect())
	assert.True(t, exp.IsConnected())
	exp.Close()
}

func TestSysExp_SendRemoteNilConnError(t *testing.T) {
	log := createTestLogger(t)
	exp := &SyslogExporter{
		config:   &SyslogExporterConfig{Tag: "t", Hostname: "h"},
		logger:   log,
		isRemote: true,
	}
	err := exp.sendRemote(&AuditEvent{Timestamp: time.Now(), Action: ActionRead, Resource: "d", Result: ResultSuccess}, SyslogSeverityInformational, "msg")
	assert.Error(t, err)
}

func TestSysExp_SendLocalNilConnError(t *testing.T) {
	log := createTestLogger(t)
	exp := &SyslogExporter{
		config:   &SyslogExporterConfig{Tag: "t", Hostname: "h"},
		logger:   log,
		isRemote: false,
	}
	assert.Error(t, exp.sendLocal(SyslogSeverityInformational, "msg"))
}

func TestSysExp_ConnectionToUnreachableAddressFails(t *testing.T) {
	log := createTestLogger(t)
	_, err := NewSyslogExporter(&SyslogExporterConfig{
		Address: "192.0.2.1:514",
		Network: "tcp",
		Tag:     "test",
	}, log)
	assert.Error(t, err)
	var connErr *SyslogConnectionError
	assert.True(t, errors.As(err, &connErr))
}

// ---------------------------------------------------------------------------
// SyslogExporter format and map helpers - comprehensive coverage
// ---------------------------------------------------------------------------

func TestSysExp_FormatRFC5424AllFields(t *testing.T) {
	log := createTestLogger(t)
	exp := &SyslogExporter{
		config: &SyslogExporterConfig{Tag: "quicbaas", Hostname: "myhost", Facility: SyslogFacilityLocal0},
		logger: log,
	}

	event := &AuditEvent{
		Timestamp: time.Date(2025, 6, 15, 12, 0, 0, 0, time.UTC),
		EventID:   "evt-cov",
		Username:  "admin",
		UserID:    "uid-1",
		Action:    ActionWrite,
		Resource:  "data/keys",
		Result:    ResultSuccess,
		IPAddress: "10.0.0.1",
		NodeID:    "node-5",
		ShardID:   7,
		ErrorMsg:  "test error",
		Details:   map[string]string{"detail1": "val1"},
	}

	msg := exp.formatRFC5424(event, SyslogSeverityInformational, "test message")
	assert.Contains(t, msg, "myhost")
	assert.Contains(t, msg, "quicbaas")
	assert.Contains(t, msg, "evt-cov")
	assert.Contains(t, msg, "audit@quicbaas")
	assert.Contains(t, msg, "test message")
	assert.Contains(t, msg, "nodeID")
	assert.Contains(t, msg, "shardID")
	assert.Contains(t, msg, "errorMsg")
}

func TestSysExp_FormatRFC5424EmptyEventIDUsesDash(t *testing.T) {
	log := createTestLogger(t)
	exp := &SyslogExporter{
		config: &SyslogExporterConfig{Tag: "q", Hostname: "h", Facility: SyslogFacilityLocal0},
		logger: log,
	}
	msg := exp.formatRFC5424(&AuditEvent{
		Timestamp: time.Now(),
		EventID:   "",
		Action:    ActionRead,
		Resource:  "d",
		Result:    ResultSuccess,
	}, SyslogSeverityInformational, "test")
	assert.Contains(t, msg, " - ")
}

func TestEscapeSDParam_AllSpecialChars(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"hello", "hello"},
		{`foo\bar`, `foo\\bar`},
		{`foo"bar`, `foo\"bar`},
		{"foo]bar", "foo\\]bar"},
		{`a\b"c]d`, `a\\b\"c\]d`},
	}
	for _, tt := range tests {
		assert.Equal(t, tt.expected, escapeSDParam(tt.input))
	}
}

func TestSysExp_MapEventToSeverityAllBranches(t *testing.T) {
	log := createTestLogger(t)
	exp := &SyslogExporter{
		config: &SyslogExporterConfig{Tag: "t", Hostname: "h"},
		logger: log,
	}

	tests := []struct {
		name     string
		event    *AuditEvent
		expected SyslogSeverity
	}{
		{"success_normal", &AuditEvent{Action: ActionRead, Result: ResultSuccess}, SyslogSeverityInformational},
		{"success_key_rotate", &AuditEvent{Action: ActionKeyRotate, Result: ResultSuccess}, SyslogSeverityNotice},
		{"success_key_generate", &AuditEvent{Action: ActionKeyGenerate, Result: ResultSuccess}, SyslogSeverityNotice},
		{"success_key_export", &AuditEvent{Action: ActionKeyExport, Result: ResultSuccess}, SyslogSeverityNotice},
		{"success_key_import", &AuditEvent{Action: ActionKeyImport, Result: ResultSuccess}, SyslogSeverityNotice},
		{"success_key_delete", &AuditEvent{Action: ActionKeyDelete, Result: ResultSuccess}, SyslogSeverityNotice},
		{"success_user_create", &AuditEvent{Action: ActionUserCreate, Result: ResultSuccess}, SyslogSeverityNotice},
		{"success_user_delete", &AuditEvent{Action: ActionUserDelete, Result: ResultSuccess}, SyslogSeverityNotice},
		{"success_role_create", &AuditEvent{Action: ActionRoleCreate, Result: ResultSuccess}, SyslogSeverityNotice},
		{"success_role_update", &AuditEvent{Action: ActionRoleUpdate, Result: ResultSuccess}, SyslogSeverityNotice},
		{"success_config_change", &AuditEvent{Action: ActionConfigChange, Result: ResultSuccess}, SyslogSeverityNotice},
		{"success_schema_change", &AuditEvent{Action: ActionSchemaChange, Result: ResultSuccess}, SyslogSeverityNotice},
		{"success_tenant_switch", &AuditEvent{Action: ActionTenantSwitch, Result: ResultSuccess}, SyslogSeverityNotice},
		{"success_cross_tenant_read", &AuditEvent{Action: ActionCrossTenantRead, Result: ResultSuccess}, SyslogSeverityNotice},
		{"success_cross_tenant_write", &AuditEvent{Action: ActionCrossTenantWrite, Result: ResultSuccess}, SyslogSeverityNotice},
		{"success_cross_tenant_delete", &AuditEvent{Action: ActionCrossTenantDelete, Result: ResultSuccess}, SyslogSeverityNotice},
		{"failure_auth", &AuditEvent{Action: ActionAuthenticate, Result: ResultFailure}, SyslogSeverityWarning},
		{"failure_other", &AuditEvent{Action: ActionWrite, Result: ResultFailure}, SyslogSeverityError},
		{"denied_auth", &AuditEvent{Action: ActionAuthenticate, Result: ResultDenied}, SyslogSeverityWarning},
		{"denied_authorize", &AuditEvent{Action: ActionAuthorize, Result: ResultDenied}, SyslogSeverityWarning},
		{"denied_tenant_access", &AuditEvent{Action: ActionTenantAccessDenied, Result: ResultDenied}, SyslogSeverityWarning},
		{"denied_other", &AuditEvent{Action: ActionDelete, Result: ResultDenied}, SyslogSeverityWarning},
		{"unknown_result", &AuditEvent{Action: ActionRead, Result: "xyz"}, SyslogSeverityInformational},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, exp.mapEventToSeverity(tt.event))
		})
	}
}

func TestSysExp_FormatMessageAllFields(t *testing.T) {
	log := createTestLogger(t)
	exp := &SyslogExporter{
		config: &SyslogExporterConfig{Tag: "t", Hostname: "h"},
		logger: log,
	}
	msg := exp.formatMessage(&AuditEvent{
		Action: ActionRead, Resource: "data", Result: ResultSuccess,
		Username: "admin", IPAddress: "10.0.0.1", ErrorMsg: "some error",
	})
	assert.Contains(t, msg, "user=admin")
	assert.Contains(t, msg, "ip=10.0.0.1")
	assert.Contains(t, msg, "error=some error")
}

func TestSysExp_FormatMessageMinimalFields(t *testing.T) {
	log := createTestLogger(t)
	exp := &SyslogExporter{
		config: &SyslogExporterConfig{Tag: "t", Hostname: "h"},
		logger: log,
	}
	msg := exp.formatMessage(&AuditEvent{Action: ActionRead, Resource: "d", Result: ResultSuccess})
	assert.NotContains(t, msg, "user=")
	assert.NotContains(t, msg, "ip=")
}

// ---------------------------------------------------------------------------
// Error types - comprehensive coverage
// ---------------------------------------------------------------------------

func TestSysConnErr_LocalErrorMessage(t *testing.T) {
	err := &SyslogConnectionError{Network: "", Err: errors.New("socket")}
	assert.Contains(t, err.Error(), "failed to connect to local syslog")
}

func TestSysConnErr_RemoteErrorMessage(t *testing.T) {
	err := &SyslogConnectionError{Address: "10.0.0.1:514", Network: "udp", Err: errors.New("timeout")}
	assert.Contains(t, err.Error(), "udp://10.0.0.1:514")
}

func TestSysConnErr_UnwrapReturnsInner(t *testing.T) {
	inner := errors.New("inner")
	err := &SyslogConnectionError{Err: inner}
	assert.Equal(t, inner, err.Unwrap())
}

func TestSysWriteErr_MessageAndUnwrap(t *testing.T) {
	err := &SyslogWriteError{Err: errors.New("broken pipe")}
	assert.Contains(t, err.Error(), "failed to write to syslog")
	assert.Equal(t, "broken pipe", err.Unwrap().Error())
}

func TestSysExpErr_MessageFormat(t *testing.T) {
	err := &SyslogExporterError{Op: "op", Reason: "reason"}
	assert.Contains(t, err.Error(), "syslog exporter error: op: reason")
}

func TestSIEMErr_WithCauseFormat(t *testing.T) {
	cause := errors.New("net err")
	err := &SIEMError{Code: "C", Message: "m", Cause: cause}
	assert.Contains(t, err.Error(), "C: m: net err")
	assert.Equal(t, cause, err.Unwrap())
}

func TestSIEMErr_NoCauseFormat(t *testing.T) {
	err := &SIEMError{Code: "C", Message: "m"}
	assert.Equal(t, "C: m", err.Error())
	assert.Nil(t, err.Unwrap())
}

func TestAuditErr_WithCauseUnwrap(t *testing.T) {
	cause := errors.New("io err")
	err := &AuditError{Code: "C", Message: "m", Cause: cause}
	assert.Contains(t, err.Error(), "C: m: io err")
	assert.Equal(t, cause, err.Unwrap())
}

func TestAuditErr_NoCauseFormat(t *testing.T) {
	err := &AuditError{Code: "C", Message: "m"}
	assert.Equal(t, "C: m", err.Error())
	assert.Nil(t, err.Unwrap())
}

// ---------------------------------------------------------------------------
// AuditLogger write error
// ---------------------------------------------------------------------------

type covFailWriter struct{}

func (f *covFailWriter) Write(p []byte) (n int, err error) {
	return 0, errors.New("write failed")
}

func TestAL_LogWriteErrorReturnsAuditError(t *testing.T) {
	log := createTestLogger(t)
	al := &AuditLogger{
		config:   &AuditConfig{Enabled: true},
		logger:   log,
		writer:   &covFailWriter{},
		shutdown: make(chan struct{}),
	}

	err := al.Log(context.Background(), &AuditEvent{
		Action: ActionRead, Resource: "data", Result: ResultSuccess,
	})
	assert.Error(t, err)
	var auditErr *AuditError
	assert.True(t, errors.As(err, &auditErr))
	assert.Equal(t, ErrCodeAuditEventWrite, auditErr.Code)
}

// ---------------------------------------------------------------------------
// AuditLogger sendToSyslog paths
// ---------------------------------------------------------------------------

func TestAL_SendToSyslogWithExporterUsesExporter(t *testing.T) {
	addr, err := net.ResolveUDPAddr("udp", "127.0.0.1:0")
	require.NoError(t, err)
	conn, err := net.ListenUDP("udp", addr)
	require.NoError(t, err)
	defer conn.Close()

	log := createTestLogger(t)
	syslogExp, err := NewSyslogExporter(&SyslogExporterConfig{
		Address: conn.LocalAddr().String(), Network: "udp", Facility: SyslogFacilityLocal0, Tag: "cov",
	}, log)
	require.NoError(t, err)

	al := &AuditLogger{
		config:         &AuditConfig{Enabled: true, Syslog: true},
		logger:         log,
		writer:         &bytes.Buffer{},
		syslogExporter: syslogExp,
		shutdown:       make(chan struct{}),
	}

	al.sendToSyslog(&AuditEvent{
		Timestamp: time.Now(), Action: ActionRead, Resource: "d", Result: ResultSuccess,
	})

	buf := make([]byte, 4096)
	conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	n, _, readErr := conn.ReadFromUDP(buf)
	require.NoError(t, readErr)
	assert.Greater(t, n, 0)
	syslogExp.Close()
}

func TestAL_SendToSyslogLegacyNoAddrReturns(t *testing.T) {
	log := createTestLogger(t)
	al := &AuditLogger{
		config:   &AuditConfig{Enabled: true, Syslog: true, SyslogAddr: ""},
		logger:   log,
		writer:   &bytes.Buffer{},
		shutdown: make(chan struct{}),
	}
	// Should not panic
	al.sendToSyslog(&AuditEvent{Action: ActionRead, Resource: "d", Result: ResultSuccess})
}

func TestAL_SendToSyslogLegacyWithAddrDialsUDP(t *testing.T) {
	addr, err := net.ResolveUDPAddr("udp", "127.0.0.1:0")
	require.NoError(t, err)
	conn, err := net.ListenUDP("udp", addr)
	require.NoError(t, err)
	defer conn.Close()

	log := createTestLogger(t)
	al := &AuditLogger{
		config:   &AuditConfig{Enabled: true, Syslog: true, SyslogAddr: conn.LocalAddr().String()},
		logger:   log,
		writer:   &bytes.Buffer{},
		shutdown: make(chan struct{}),
	}

	al.sendToSyslog(&AuditEvent{
		Timestamp: time.Now(), Action: ActionRead, Resource: "d", Result: ResultSuccess, Username: "a", IPAddress: "1.2.3.4",
	})

	buf := make([]byte, 4096)
	conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	n, _, readErr := conn.ReadFromUDP(buf)
	require.NoError(t, readErr)
	assert.Greater(t, n, 0)
}

// ---------------------------------------------------------------------------
// NewAuditLogger paths
// ---------------------------------------------------------------------------

func TestNewAL_NilConfigReturnsAuditError(t *testing.T) {
	log := createTestLogger(t)
	al, err := NewAuditLogger(nil, log)
	assert.Error(t, err)
	assert.Nil(t, al)
	var auditErr *AuditError
	assert.True(t, errors.As(err, &auditErr))
	assert.Equal(t, ErrCodeAuditConfigNil, auditErr.Code)
}

func TestNewAL_DisabledSetsDiscardWriter(t *testing.T) {
	log := createTestLogger(t)
	al, err := NewAuditLogger(&AuditConfig{Enabled: false}, log)
	require.NoError(t, err)
	defer al.Close()
	assert.NotNil(t, al)
}

func TestNewAL_SignLogsNoKeyReturnsError(t *testing.T) {
	log := createTestLogger(t)
	al, err := NewAuditLogger(&AuditConfig{Enabled: true, SignLogs: true, SignKey: ""}, log)
	assert.Error(t, err)
	assert.Nil(t, al)
}

func TestNewAL_WithFileCreatesFile(t *testing.T) {
	logFile := filepath.Join(t.TempDir(), "audit.log")
	log := createTestLogger(t)
	al, err := NewAuditLogger(&AuditConfig{Enabled: true, File: logFile}, log)
	require.NoError(t, err)
	defer al.Close()
	assert.FileExists(t, logFile)
}

func TestNewAL_WithSIEMExporterCreatesExporter(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	log := createTestLogger(t)
	al, err := NewAuditLogger(&AuditConfig{
		Enabled: true,
		SIEM:    true,
		SIEMConfig: &SIEMConfig{
			Enabled:  true,
			Endpoint: server.URL,
			Format:   SIEMFormatJSON,
		},
	}, log)
	require.NoError(t, err)
	defer al.Close()
	assert.NotNil(t, al.siemExporter)
}

func TestNewAL_DefaultsAreSet(t *testing.T) {
	log := createTestLogger(t)
	al, err := NewAuditLogger(&AuditConfig{Enabled: true}, log)
	require.NoError(t, err)
	defer al.Close()
	assert.Equal(t, int64(100), al.config.MaxSize)
	assert.Equal(t, 10, al.config.MaxBackups)
	assert.Equal(t, 1024, al.config.BufferSize)
	assert.Equal(t, 5*time.Second, al.config.FlushInterval)
}

func TestNewAL_LegacySIEMConfigBackwardCompat(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	log := createTestLogger(t)
	al, err := NewAuditLogger(&AuditConfig{
		Enabled:      true,
		SIEM:         true,
		SIEMEndpoint: server.URL,
		// No SIEMConfig provided - should use legacy fields
	}, log)
	require.NoError(t, err)
	defer al.Close()
	assert.NotNil(t, al.siemExporter)
}

// ---------------------------------------------------------------------------
// AutoFlush shutdown
// ---------------------------------------------------------------------------

func TestAL_AutoFlushStopsOnClose(t *testing.T) {
	logFile := filepath.Join(t.TempDir(), "flush.log")
	log := createTestLogger(t)
	al, err := NewAuditLogger(&AuditConfig{
		Enabled: true, File: logFile, FlushInterval: 50 * time.Millisecond,
	}, log)
	require.NoError(t, err)

	require.NoError(t, al.Log(context.Background(), &AuditEvent{Action: ActionRead, Resource: "d", Result: ResultSuccess}))

	// Let autoFlush run at least once
	time.Sleep(100 * time.Millisecond)

	assert.NoError(t, al.Close())
}

// ---------------------------------------------------------------------------
// generateEventID uniqueness
// ---------------------------------------------------------------------------

func TestGenEvtID_UniqueAndPrefixed(t *testing.T) {
	id1 := generateEventID()
	assert.Contains(t, id1, "evt-")
	id2 := generateEventID()
	assert.NotEqual(t, id1, id2)
}

// ---------------------------------------------------------------------------
// AuditLogger with syslog enabled but unreachable
// ---------------------------------------------------------------------------

func TestNewAL_SyslogEnabledButUnreachableDoesNotFail(t *testing.T) {
	log := createTestLogger(t)
	al, err := NewAuditLogger(&AuditConfig{
		Enabled:       true,
		SyslogEnabled: true,
		SyslogAddress: "192.0.2.1:514",
		SyslogNetwork: "tcp",
	}, log)
	require.NoError(t, err) // Should not fail, just warn
	defer al.Close()
	assert.Nil(t, al.syslogExporter) // Connection failed, so nil
}

// ---------------------------------------------------------------------------
// AuditLogger Close with syslog exporter
// ---------------------------------------------------------------------------

func TestAL_CloseWithSyslogExporter(t *testing.T) {
	addr, err := net.ResolveUDPAddr("udp", "127.0.0.1:0")
	require.NoError(t, err)
	conn, err := net.ListenUDP("udp", addr)
	require.NoError(t, err)
	defer conn.Close()

	log := createTestLogger(t)
	syslogExp, err := NewSyslogExporter(&SyslogExporterConfig{
		Address: conn.LocalAddr().String(), Network: "udp", Tag: "cov",
	}, log)
	require.NoError(t, err)

	al := &AuditLogger{
		config:         &AuditConfig{Enabled: true},
		logger:         log,
		writer:         &bytes.Buffer{},
		syslogExporter: syslogExp,
		shutdown:       make(chan struct{}),
	}

	assert.NoError(t, al.Close())
}

func TestAL_CloseWithSIEMExporter(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	log := createTestLogger(t)
	siemExp, err := NewSIEMExporter(&SIEMConfig{
		Enabled: true, Endpoint: server.URL, Format: SIEMFormatJSON,
	}, log)
	require.NoError(t, err)
	siemExp.Start()

	al := &AuditLogger{
		config:       &AuditConfig{Enabled: true},
		logger:       log,
		writer:       &bytes.Buffer{},
		siemExporter: siemExp,
		shutdown:     make(chan struct{}),
	}

	assert.NoError(t, al.Close())
}

func TestAL_CloseWithFileAndBothExporters(t *testing.T) {
	tmpFile := filepath.Join(t.TempDir(), "close-test.log")
	f, err := os.Create(tmpFile)
	require.NoError(t, err)

	addr, err := net.ResolveUDPAddr("udp", "127.0.0.1:0")
	require.NoError(t, err)
	conn, err := net.ListenUDP("udp", addr)
	require.NoError(t, err)
	defer conn.Close()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	log := createTestLogger(t)
	syslogExp, err := NewSyslogExporter(&SyslogExporterConfig{
		Address: conn.LocalAddr().String(), Network: "udp", Tag: "cov",
	}, log)
	require.NoError(t, err)

	siemExp, err := NewSIEMExporter(&SIEMConfig{
		Enabled: true, Endpoint: server.URL, Format: SIEMFormatJSON,
	}, log)
	require.NoError(t, err)
	siemExp.Start()

	al := &AuditLogger{
		config:         &AuditConfig{Enabled: true},
		logger:         log,
		writer:         f,
		file:           f,
		syslogExporter: syslogExp,
		siemExporter:   siemExp,
		shutdown:       make(chan struct{}),
	}

	assert.NoError(t, al.Close())
}
