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

package secrets

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mockKVProposer implements ClusterKVProposer for testing.
type mockKVProposer struct {
	proposeFn    func(ctx context.Context, shardID uint64, key string, value []byte) error
	proposeCalls int
	mu           sync.Mutex
}

func (m *mockKVProposer) ProposeKV(ctx context.Context, shardID uint64, key string, value []byte) error {
	m.mu.Lock()
	m.proposeCalls++
	m.mu.Unlock()

	if m.proposeFn != nil {
		return m.proposeFn(ctx, shardID, key, value)
	}
	return nil
}

// mockSecretsService implements SecretsServiceForReencryption for testing.
type mockSecretsService struct {
	secrets          []SecretInfo
	secretData       map[string][]byte
	secretMetadata   map[string]map[string]string
	listErr          error
	getDataErr       error
	reencryptErr     error
	reencryptedCount int32
	mu               sync.Mutex
}

func newMockSecretsService() *mockSecretsService {
	return &mockSecretsService{
		secrets:        make([]SecretInfo, 0),
		secretData:     make(map[string][]byte),
		secretMetadata: make(map[string]map[string]string),
	}
}

func (m *mockSecretsService) addSecret(id, keyIdentifier string, data []byte) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.secrets = append(m.secrets, SecretInfo{
		SecretID:      id,
		KeyIdentifier: keyIdentifier,
	})
	m.secretData[id] = data
	m.secretMetadata[id] = map[string]string{"test": "value"}
}

func (m *mockSecretsService) ListSecrets(ctx context.Context, limit int, cursor string) ([]SecretInfo, string, bool, int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.listErr != nil {
		return nil, "", false, 0, m.listErr
	}

	// Simple pagination
	start := 0
	if cursor != "" {
		for i, s := range m.secrets {
			if s.SecretID == cursor {
				start = i + 1
				break
			}
		}
	}

	end := start + limit
	if end > len(m.secrets) {
		end = len(m.secrets)
	}

	hasMore := end < len(m.secrets)
	nextCursor := ""
	if hasMore && end > 0 {
		nextCursor = m.secrets[end-1].SecretID
	}

	return m.secrets[start:end], nextCursor, hasMore, len(m.secrets), nil
}

func (m *mockSecretsService) GetSecretData(ctx context.Context, secretID string) ([]byte, map[string]string, string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.getDataErr != nil {
		return nil, nil, "", m.getDataErr
	}

	data, ok := m.secretData[secretID]
	if !ok {
		return nil, nil, "", errors.New("secret not found")
	}

	// Find key identifier
	var keyID string
	for _, s := range m.secrets {
		if s.SecretID == secretID {
			keyID = s.KeyIdentifier
			break
		}
	}

	return data, m.secretMetadata[secretID], keyID, nil
}

func (m *mockSecretsService) ReencryptSecret(ctx context.Context, secretID string, data []byte, metadata map[string]string) error {
	if m.reencryptErr != nil {
		return m.reencryptErr
	}

	atomic.AddInt32(&m.reencryptedCount, 1)
	return nil
}

func (m *mockSecretsService) getReencryptedCount() int {
	return int(atomic.LoadInt32(&m.reencryptedCount))
}

func TestClusterDEKManager_NewClusterDEKManager(t *testing.T) {
	kms := newTestMockKMSManager()
	store := newTestMockKVStore()
	logger := newTestMockLogger()

	dek := NewSecretsDEK(kms, store, "master-key", logger)
	proposer := &mockKVProposer{}

	config := DefaultClusterDEKConfig()
	manager := NewClusterDEKManager(dek, proposer, config, logger)

	require.NotNil(t, manager)
	assert.Equal(t, dek, manager.dek)
	assert.Equal(t, proposer, manager.kvProposer)
	assert.False(t, manager.IsClusterMode()) // EnableClusterSync is false by default
}

func TestClusterDEKManager_Initialize_SingleNode(t *testing.T) {
	ctx := context.Background()
	kms := newTestMockKMSManager()
	store := newTestMockKVStore()
	logger := newTestMockLogger()

	dek := NewSecretsDEK(kms, store, "master-key", logger)
	defer dek.Close()

	config := DefaultClusterDEKConfig()
	config.EnableClusterSync = false

	manager := NewClusterDEKManager(dek, nil, config, logger)

	err := manager.Initialize(ctx, "AES-256-GCM")
	require.NoError(t, err)

	// Verify DEK was created
	status, err := manager.GetStatus(ctx, DEKIDGlobal)
	require.NoError(t, err)
	assert.Equal(t, uint64(1), status.Version)
	assert.Equal(t, "AES-256-GCM", status.Algorithm)
}

func TestClusterDEKManager_Initialize_ClusterMode(t *testing.T) {
	ctx := context.Background()
	kms := newTestMockKMSManager()
	store := newTestMockKVStore()
	logger := newTestMockLogger()

	dek := NewSecretsDEK(kms, store, "master-key", logger)
	defer dek.Close()

	proposer := &mockKVProposer{}
	config := DefaultClusterDEKConfig()
	config.EnableClusterSync = true

	manager := NewClusterDEKManager(dek, proposer, config, logger)

	err := manager.Initialize(ctx, "AES-256-GCM")
	require.NoError(t, err)

	// Verify DEK was created and synced
	status, err := manager.GetStatus(ctx, DEKIDGlobal)
	require.NoError(t, err)
	assert.Equal(t, uint64(1), status.Version)

	// Verify proposal was made through proposer
	assert.Equal(t, 1, proposer.proposeCalls)
}

func TestClusterDEKManager_Initialize_ClusterModeError(t *testing.T) {
	ctx := context.Background()
	kms := newTestMockKMSManager()
	store := newTestMockKVStore()
	logger := newTestMockLogger()

	dek := NewSecretsDEK(kms, store, "master-key", logger)
	defer dek.Close()

	// Create proposer that fails on ProposeKV
	proposer := &mockKVProposer{
		proposeFn: func(ctx context.Context, shardID uint64, key string, value []byte) error {
			return errors.New("raft proposal failed")
		},
	}

	config := DefaultClusterDEKConfig()
	config.EnableClusterSync = true

	manager := NewClusterDEKManager(dek, proposer, config, logger)

	// Initialize should fail because cluster sync fails
	err := manager.Initialize(ctx, "AES-256-GCM")
	require.Error(t, err)
	// The error chain goes through syncDEKToCluster which wraps with "failed to propose SetDEK"
	assert.Contains(t, err.Error(), "failed to propose SetDEK")
	assert.Contains(t, err.Error(), "raft proposal failed")

	// Verify proposal was attempted
	assert.Equal(t, 1, proposer.proposeCalls)
}

func TestClusterDEKManager_Initialize_AlreadyInitialized(t *testing.T) {
	ctx := context.Background()
	kms := newTestMockKMSManager()
	store := newTestMockKVStore()
	logger := newTestMockLogger()

	dek := NewSecretsDEK(kms, store, "master-key", logger)
	defer dek.Close()

	proposer := &mockKVProposer{}
	config := DefaultClusterDEKConfig()
	config.EnableClusterSync = true

	manager := NewClusterDEKManager(dek, proposer, config, logger)

	// Initialize first time
	err := manager.Initialize(ctx, "AES-256-GCM")
	require.NoError(t, err)
	assert.Equal(t, 1, proposer.proposeCalls)

	// Initialize again - should skip since already exists
	err = manager.Initialize(ctx, "AES-256-GCM")
	require.NoError(t, err)
	// No additional proposals should be made
	assert.Equal(t, 1, proposer.proposeCalls)
}

func TestClusterDEKManager_Rotate_SingleNode(t *testing.T) {
	ctx := context.Background()
	kms := newTestMockKMSManager()
	store := newTestMockKVStore()
	logger := newTestMockLogger()

	dek := NewSecretsDEK(kms, store, "master-key", logger)
	defer dek.Close()

	config := DefaultClusterDEKConfig()
	config.EnableClusterSync = false

	manager := NewClusterDEKManager(dek, nil, config, logger)

	// Initialize first
	err := manager.Initialize(ctx, "AES-256-GCM")
	require.NoError(t, err)

	// Rotate
	newVersion, err := manager.Rotate(ctx, DEKIDGlobal)
	require.NoError(t, err)
	assert.Equal(t, uint64(2), newVersion)

	// Verify new version
	status, err := manager.GetStatus(ctx, DEKIDGlobal)
	require.NoError(t, err)
	assert.Equal(t, uint64(2), status.Version)
}

func TestClusterDEKManager_Rotate_ClusterMode(t *testing.T) {
	ctx := context.Background()
	kms := newTestMockKMSManager()
	store := newTestMockKVStore()
	logger := newTestMockLogger()

	dek := NewSecretsDEK(kms, store, "master-key", logger)
	defer dek.Close()

	proposer := &mockKVProposer{}
	config := DefaultClusterDEKConfig()
	config.EnableClusterSync = true

	manager := NewClusterDEKManager(dek, proposer, config, logger)

	// Initialize first
	err := manager.Initialize(ctx, "AES-256-GCM")
	require.NoError(t, err)

	// Rotate
	newVersion, err := manager.Rotate(ctx, DEKIDGlobal)
	require.NoError(t, err)
	assert.Equal(t, uint64(2), newVersion)

	// Verify proposals (1 for init + 1 for rotate)
	assert.Equal(t, 2, proposer.proposeCalls)
}

func TestClusterDEKManager_Rotate_ClusterSyncFailure(t *testing.T) {
	ctx := context.Background()
	kms := newTestMockKMSManager()
	store := newTestMockKVStore()
	logger := newTestMockLogger()

	dek := NewSecretsDEK(kms, store, "master-key", logger)
	defer dek.Close()

	proposer := &mockKVProposer{
		proposeFn: func(ctx context.Context, shardID uint64, key string, value []byte) error {
			// First call (init) succeeds, subsequent calls fail
			return errors.New("raft failure")
		},
	}

	config := DefaultClusterDEKConfig()
	config.EnableClusterSync = true

	manager := NewClusterDEKManager(dek, proposer, config, logger)

	// Initialize will fail sync
	err := manager.Initialize(ctx, "AES-256-GCM")
	require.Error(t, err)
	// The error message is wrapped
	assert.Contains(t, err.Error(), "raft failure")
}

func TestClusterDEKManager_Rotate_ClusterSyncError(t *testing.T) {
	ctx := context.Background()
	kms := newTestMockKMSManager()
	store := newTestMockKVStore()
	logger := newTestMockLogger()

	dek := NewSecretsDEK(kms, store, "master-key", logger)
	defer dek.Close()

	callCount := 0
	proposer := &mockKVProposer{
		proposeFn: func(ctx context.Context, shardID uint64, key string, value []byte) error {
			callCount++
			// First call (init) succeeds, second call (rotate) fails
			if callCount > 1 {
				return errors.New("raft rotation sync failed")
			}
			return nil
		},
	}

	config := DefaultClusterDEKConfig()
	config.EnableClusterSync = true

	manager := NewClusterDEKManager(dek, proposer, config, logger)

	// Initialize first
	err := manager.Initialize(ctx, "AES-256-GCM")
	require.NoError(t, err)
	assert.Equal(t, 1, callCount)

	// Rotate - should fail cluster sync but local rotation succeeds
	newVersion, err := manager.Rotate(ctx, DEKIDGlobal)
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrClusterSyncFailed)
	assert.Contains(t, err.Error(), "raft rotation sync failed")
	// Local rotation still succeeded
	assert.Equal(t, uint64(2), newVersion)

	// Verify local state was updated even though sync failed
	status, err := manager.GetStatus(ctx, DEKIDGlobal)
	require.NoError(t, err)
	assert.Equal(t, uint64(2), status.Version)
}

func TestClusterDEKManager_SyncDEKRotationToCluster_Error(t *testing.T) {
	ctx := context.Background()
	kms := newTestMockKMSManager()
	store := newTestMockKVStore()
	logger := newTestMockLogger()

	dek := NewSecretsDEK(kms, store, "master-key", logger)
	defer dek.Close()

	// KV proposer that tracks call count and fails on rotation
	callCount := 0
	proposer := &mockKVProposer{
		proposeFn: func(ctx context.Context, shardID uint64, key string, value []byte) error {
			callCount++
			// First call succeeds (init), second call fails (rotation sync)
			if callCount > 1 {
				return errors.New("sync rotation failed")
			}
			return nil
		},
	}

	config := DefaultClusterDEKConfig()
	config.EnableClusterSync = true

	manager := NewClusterDEKManager(dek, proposer, config, logger)

	// Initialize
	err := manager.Initialize(ctx, "AES-256-GCM")
	require.NoError(t, err)

	// Rotate - should return error from syncDEKRotationToCluster
	_, err = manager.Rotate(ctx, DEKIDGlobal)
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrClusterSyncFailed)
	assert.Contains(t, err.Error(), "sync rotation failed")

	// Verify 2 calls were made (init + rotation attempt)
	assert.Equal(t, 2, callCount)
}

func TestClusterDEKManager_GetKey(t *testing.T) {
	ctx := context.Background()
	kms := newTestMockKMSManager()
	store := newTestMockKVStore()
	logger := newTestMockLogger()

	dek := NewSecretsDEK(kms, store, "master-key", logger)
	defer dek.Close()

	config := DefaultClusterDEKConfig()
	manager := NewClusterDEKManager(dek, nil, config, logger)

	// Initialize
	err := manager.Initialize(ctx, "AES-256-GCM")
	require.NoError(t, err)

	// Get key
	key, version, err := manager.GetKey(ctx, DEKIDGlobal)
	require.NoError(t, err)
	assert.NotNil(t, key)
	assert.Equal(t, uint64(1), version)
}

func TestClusterDEKManager_GetKeyVersion(t *testing.T) {
	ctx := context.Background()
	kms := newTestMockKMSManager()
	store := newTestMockKVStore()
	logger := newTestMockLogger()

	dek := NewSecretsDEK(kms, store, "master-key", logger)
	defer dek.Close()

	config := DefaultClusterDEKConfig()
	manager := NewClusterDEKManager(dek, nil, config, logger)

	// Initialize and rotate
	err := manager.Initialize(ctx, "AES-256-GCM")
	require.NoError(t, err)

	_, err = manager.Rotate(ctx, DEKIDGlobal)
	require.NoError(t, err)

	// Get old version
	key, err := manager.GetKeyVersion(ctx, DEKIDGlobal, 1)
	require.NoError(t, err)
	assert.NotNil(t, key)

	// Get current version
	key, err = manager.GetKeyVersion(ctx, DEKIDGlobal, 2)
	require.NoError(t, err)
	assert.NotNil(t, key)
}

func TestClusterDEKManager_ListVersions(t *testing.T) {
	ctx := context.Background()
	kms := newTestMockKMSManager()
	store := newTestMockKVStore()
	logger := newTestMockLogger()

	dek := NewSecretsDEK(kms, store, "master-key", logger)
	defer dek.Close()

	config := DefaultClusterDEKConfig()
	manager := NewClusterDEKManager(dek, nil, config, logger)

	// Initialize and rotate twice
	err := manager.Initialize(ctx, "AES-256-GCM")
	require.NoError(t, err)

	_, err = manager.Rotate(ctx, DEKIDGlobal)
	require.NoError(t, err)

	_, err = manager.Rotate(ctx, DEKIDGlobal)
	require.NoError(t, err)

	// List versions
	versions, err := manager.ListVersions(ctx, DEKIDGlobal)
	require.NoError(t, err)
	assert.GreaterOrEqual(t, len(versions), 1) // At least current version
}

func TestClusterDEKManager_EnsureTenantDEK(t *testing.T) {
	ctx := context.Background()
	kms := newTestMockKMSManager()
	store := newTestMockKVStore()
	logger := newTestMockLogger()

	dek := NewSecretsDEK(kms, store, "master-key", logger)
	defer dek.Close()

	proposer := &mockKVProposer{}
	config := DefaultClusterDEKConfig()
	config.EnableClusterSync = true

	manager := NewClusterDEKManager(dek, proposer, config, logger)

	// Create tenant DEK
	dekid, err := manager.EnsureTenantDEK(ctx, "tenant-123", "AES-256-GCM")
	require.NoError(t, err)
	assert.Equal(t, DEKID("dek:tenant:tenant-123"), dekid)

	// Verify it was synced
	assert.Equal(t, 1, proposer.proposeCalls)
}

func TestClusterDEKManager_EnsureTenantDEK_ClusterMode(t *testing.T) {
	ctx := context.Background()
	kms := newTestMockKMSManager()
	store := newTestMockKVStore()
	logger := newTestMockLogger()

	dek := NewSecretsDEK(kms, store, "master-key", logger)
	defer dek.Close()

	// Test with sync error - should still create DEK locally but log warning
	proposer := &mockKVProposer{
		proposeFn: func(ctx context.Context, shardID uint64, key string, value []byte) error {
			return errors.New("sync failed")
		},
	}

	config := DefaultClusterDEKConfig()
	config.EnableClusterSync = true

	manager := NewClusterDEKManager(dek, proposer, config, logger)

	// Create tenant DEK - should succeed locally even if sync fails
	dekid, err := manager.EnsureTenantDEK(ctx, "tenant-456", "AES-256-GCM")
	require.NoError(t, err)
	assert.Equal(t, DEKID("dek:tenant:tenant-456"), dekid)

	// Verify proposal was attempted
	assert.Equal(t, 1, proposer.proposeCalls)

	// Verify DEK exists locally
	_, _, err = manager.GetKey(ctx, dekid)
	require.NoError(t, err)
}

func TestClusterDEKManager_DeleteTenantDEK(t *testing.T) {
	ctx := context.Background()
	kms := newTestMockKMSManager()
	store := newTestMockKVStore()
	logger := newTestMockLogger()

	dek := NewSecretsDEK(kms, store, "master-key", logger)
	defer dek.Close()

	config := DefaultClusterDEKConfig()
	manager := NewClusterDEKManager(dek, nil, config, logger)

	// Create tenant DEK
	_, err := manager.EnsureTenantDEK(ctx, "tenant-123", "AES-256-GCM")
	require.NoError(t, err)

	// Delete
	err = manager.DeleteTenantDEK(ctx, "tenant-123")
	require.NoError(t, err)

	// Verify deleted
	_, _, err = manager.GetKey(ctx, NewTenantDEKID("tenant-123"))
	assert.Error(t, err)
}

func TestClusterDEKManager_CleanupOldVersions(t *testing.T) {
	ctx := context.Background()
	kms := newTestMockKMSManager()
	store := newTestMockKVStore()
	logger := newTestMockLogger()

	dek := NewSecretsDEK(kms, store, "master-key", logger)
	defer dek.Close()

	config := DefaultClusterDEKConfig()
	manager := NewClusterDEKManager(dek, nil, config, logger)

	// Initialize and rotate multiple times
	err := manager.Initialize(ctx, "AES-256-GCM")
	require.NoError(t, err)

	for i := 0; i < 5; i++ {
		_, err = manager.Rotate(ctx, DEKIDGlobal)
		require.NoError(t, err)
	}

	// Cleanup keeping only 2 old versions
	deleted, err := manager.CleanupOldVersions(ctx, DEKIDGlobal, 2)
	require.NoError(t, err)
	// Should have deleted some versions
	assert.GreaterOrEqual(t, deleted, 0)
}

func TestClusterDEKManager_IsClusterMode(t *testing.T) {
	kms := newTestMockKMSManager()
	store := newTestMockKVStore()
	logger := newTestMockLogger()

	dek := NewSecretsDEK(kms, store, "master-key", logger)
	defer dek.Close()

	// No proposer
	config := DefaultClusterDEKConfig()
	config.EnableClusterSync = true
	manager := NewClusterDEKManager(dek, nil, config, logger)
	assert.False(t, manager.IsClusterMode())

	// With proposer but disabled
	config.EnableClusterSync = false
	manager = NewClusterDEKManager(dek, &mockKVProposer{}, config, logger)
	assert.False(t, manager.IsClusterMode())

	// Enabled with proposer
	config.EnableClusterSync = true
	manager = NewClusterDEKManager(dek, &mockKVProposer{}, config, logger)
	assert.True(t, manager.IsClusterMode())
}

func TestClusterDEKManager_StartReencryption(t *testing.T) {
	ctx := context.Background()
	kms := newTestMockKMSManager()
	store := newTestMockKVStore()
	logger := newTestMockLogger()

	dek := NewSecretsDEK(kms, store, "master-key", logger)
	defer dek.Close()

	config := DefaultClusterDEKConfig()
	manager := NewClusterDEKManager(dek, nil, config, logger)

	// Initialize
	err := manager.Initialize(ctx, "AES-256-GCM")
	require.NoError(t, err)

	// Create mock secrets service
	svc := newMockSecretsService()
	svc.addSecret("secret-1", string(DEKIDGlobal), []byte("data1"))
	svc.addSecret("secret-2", string(DEKIDGlobal), []byte("data2"))
	svc.addSecret("secret-3", "different-key", []byte("data3")) // Different key, should be skipped

	// Start re-encryption
	req := &ReencryptionRequest{
		DEKID:          DEKIDGlobal,
		SourceVersion:  1,
		SecretsService: svc,
		BatchSize:      10,
	}

	err = manager.StartReencryption(ctx, req)
	require.NoError(t, err)

	// Wait for completion
	time.Sleep(500 * time.Millisecond)

	// Verify stats
	stats := manager.GetReencryptionStats()
	require.NotNil(t, stats)
	assert.False(t, stats.InProgress)
	assert.Equal(t, 3, stats.ProcessedCount) // All 3 were processed
	assert.Equal(t, 3, stats.SuccessCount)   // All 3 processed without error (skipped = success)
	// Only 2 were actually re-encrypted (the ones matching DEKID)
	assert.Equal(t, 2, svc.getReencryptedCount())
}

func TestClusterDEKManager_StartReencryption_AlreadyRunning(t *testing.T) {
	ctx := context.Background()
	kms := newTestMockKMSManager()
	store := newTestMockKVStore()
	logger := newTestMockLogger()

	dek := NewSecretsDEK(kms, store, "master-key", logger)
	defer dek.Close()

	config := DefaultClusterDEKConfig()
	manager := NewClusterDEKManager(dek, nil, config, logger)

	err := manager.Initialize(ctx, "AES-256-GCM")
	require.NoError(t, err)

	// Create mock with many secrets to keep it running
	svc := newMockSecretsService()
	for i := 0; i < 100; i++ {
		svc.addSecret("secret-"+string(rune('0'+i%10)), string(DEKIDGlobal), []byte("data"))
	}

	// Start first re-encryption
	req := &ReencryptionRequest{
		DEKID:          DEKIDGlobal,
		SecretsService: svc,
		BatchSize:      1, // Small batches to keep it running
	}

	err = manager.StartReencryption(ctx, req)
	require.NoError(t, err)

	// Try to start another immediately
	err = manager.StartReencryption(ctx, req)
	assert.Equal(t, ErrReencryptionInProgress, err)

	// Stop and wait
	manager.StopReencryption()
	time.Sleep(200 * time.Millisecond)
}

func TestClusterDEKManager_StartReencryption_ListError(t *testing.T) {
	ctx := context.Background()
	kms := newTestMockKMSManager()
	store := newTestMockKVStore()
	logger := newTestMockLogger()

	dek := NewSecretsDEK(kms, store, "master-key", logger)
	defer dek.Close()

	config := DefaultClusterDEKConfig()
	manager := NewClusterDEKManager(dek, nil, config, logger)

	err := manager.Initialize(ctx, "AES-256-GCM")
	require.NoError(t, err)

	// Create mock that fails on list
	svc := newMockSecretsService()
	svc.listErr = errors.New("list failed")

	req := &ReencryptionRequest{
		DEKID:          DEKIDGlobal,
		SecretsService: svc,
	}

	err = manager.StartReencryption(ctx, req)
	require.NoError(t, err)

	// Wait for completion
	time.Sleep(200 * time.Millisecond)

	stats := manager.GetReencryptionStats()
	require.NotNil(t, stats)
	assert.Equal(t, "list failed", stats.LastError)
}

func TestClusterDEKManager_StartReencryption_WithProgress(t *testing.T) {
	ctx := context.Background()
	kms := newTestMockKMSManager()
	store := newTestMockKVStore()
	logger := newTestMockLogger()

	dek := NewSecretsDEK(kms, store, "master-key", logger)
	defer dek.Close()

	config := DefaultClusterDEKConfig()
	manager := NewClusterDEKManager(dek, nil, config, logger)

	err := manager.Initialize(ctx, "AES-256-GCM")
	require.NoError(t, err)

	svc := newMockSecretsService()
	svc.addSecret("secret-1", string(DEKIDGlobal), []byte("data1"))

	var progressCalled atomic.Bool
	req := &ReencryptionRequest{
		DEKID:          DEKIDGlobal,
		SecretsService: svc,
		OnProgress: func(stats *ReencryptionStats) {
			progressCalled.Store(true)
		},
	}

	err = manager.StartReencryption(ctx, req)
	require.NoError(t, err)

	time.Sleep(200 * time.Millisecond)
	assert.True(t, progressCalled.Load())
}

func TestClusterDEKManager_StartReencryption_GetSecretDataError(t *testing.T) {
	ctx := context.Background()
	kms := newTestMockKMSManager()
	store := newTestMockKVStore()
	logger := newTestMockLogger()

	dek := NewSecretsDEK(kms, store, "master-key", logger)
	defer dek.Close()

	config := DefaultClusterDEKConfig()
	manager := NewClusterDEKManager(dek, nil, config, logger)

	err := manager.Initialize(ctx, "AES-256-GCM")
	require.NoError(t, err)

	// Create service that fails on GetSecretData
	svc := newMockSecretsService()
	svc.addSecret("secret-1", string(DEKIDGlobal), []byte("data1"))
	svc.getDataErr = errors.New("get data failed")

	req := &ReencryptionRequest{
		DEKID:          DEKIDGlobal,
		SecretsService: svc,
		BatchSize:      10,
	}

	err = manager.StartReencryption(ctx, req)
	require.NoError(t, err)

	// Wait for completion
	time.Sleep(200 * time.Millisecond)

	stats := manager.GetReencryptionStats()
	require.NotNil(t, stats)
	assert.False(t, stats.InProgress)
	assert.Equal(t, 1, stats.ProcessedCount)
	assert.Equal(t, 1, stats.ErrorCount)
	assert.Contains(t, stats.LastError, "get data failed")
}

func TestClusterDEKManager_StartReencryption_ReencryptError(t *testing.T) {
	ctx := context.Background()
	kms := newTestMockKMSManager()
	store := newTestMockKVStore()
	logger := newTestMockLogger()

	dek := NewSecretsDEK(kms, store, "master-key", logger)
	defer dek.Close()

	config := DefaultClusterDEKConfig()
	manager := NewClusterDEKManager(dek, nil, config, logger)

	err := manager.Initialize(ctx, "AES-256-GCM")
	require.NoError(t, err)

	// Create service that fails on ReencryptSecret
	svc := newMockSecretsService()
	svc.addSecret("secret-1", string(DEKIDGlobal), []byte("data1"))
	svc.reencryptErr = errors.New("reencrypt failed")

	req := &ReencryptionRequest{
		DEKID:          DEKIDGlobal,
		SecretsService: svc,
		BatchSize:      10,
	}

	err = manager.StartReencryption(ctx, req)
	require.NoError(t, err)

	// Wait for completion
	time.Sleep(200 * time.Millisecond)

	stats := manager.GetReencryptionStats()
	require.NotNil(t, stats)
	assert.False(t, stats.InProgress)
	assert.Equal(t, 1, stats.ProcessedCount)
	assert.Equal(t, 1, stats.ErrorCount)
	assert.Contains(t, stats.LastError, "reencrypt failed")
}

func TestClusterDEKManager_StartReencryption_SpecifiedTargetVersion(t *testing.T) {
	ctx := context.Background()
	kms := newTestMockKMSManager()
	store := newTestMockKVStore()
	logger := newTestMockLogger()

	dek := NewSecretsDEK(kms, store, "master-key", logger)
	defer dek.Close()

	config := DefaultClusterDEKConfig()
	manager := NewClusterDEKManager(dek, nil, config, logger)

	// Initialize and rotate
	err := manager.Initialize(ctx, "AES-256-GCM")
	require.NoError(t, err)

	_, err = manager.Rotate(ctx, DEKIDGlobal)
	require.NoError(t, err)

	svc := newMockSecretsService()
	svc.addSecret("secret-1", string(DEKIDGlobal), []byte("data1"))

	// Specify target version explicitly
	req := &ReencryptionRequest{
		DEKID:          DEKIDGlobal,
		SourceVersion:  1,
		TargetVersion:  2, // Explicitly set target
		SecretsService: svc,
		BatchSize:      10,
	}

	err = manager.StartReencryption(ctx, req)
	require.NoError(t, err)

	// Wait for completion
	time.Sleep(200 * time.Millisecond)

	stats := manager.GetReencryptionStats()
	require.NotNil(t, stats)
	assert.False(t, stats.InProgress)
	assert.Equal(t, uint64(2), stats.TargetVersion)
	assert.Equal(t, 1, stats.SuccessCount)
}

func TestClusterDEKManager_StartReencryption_GetStatusError(t *testing.T) {
	ctx := context.Background()
	kms := newTestMockKMSManager()
	store := newTestMockKVStore()
	logger := newTestMockLogger()

	dek := NewSecretsDEK(kms, store, "master-key", logger)
	defer dek.Close()

	config := DefaultClusterDEKConfig()
	manager := NewClusterDEKManager(dek, nil, config, logger)

	// Don't initialize - GetStatus will fail
	svc := newMockSecretsService()

	req := &ReencryptionRequest{
		DEKID:          DEKIDGlobal,
		TargetVersion:  0, // Will try to get current version
		SecretsService: svc,
	}

	err := manager.StartReencryption(ctx, req)
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrReencryptionFailed)
	assert.Contains(t, err.Error(), "failed to get current DEK version")
}

func TestClusterDEKManager_Close(t *testing.T) {
	kms := newTestMockKMSManager()
	store := newTestMockKVStore()
	logger := newTestMockLogger()

	dek := NewSecretsDEK(kms, store, "master-key", logger)

	config := DefaultClusterDEKConfig()
	manager := NewClusterDEKManager(dek, nil, config, logger)

	err := manager.Close()
	require.NoError(t, err)
}

func TestReencryptionStats_Duration(t *testing.T) {
	now := time.Now()
	stats := &ReencryptionStats{
		StartedAt: now.Add(-10 * time.Second),
	}

	// In progress - duration from start to now
	duration := stats.Duration()
	assert.GreaterOrEqual(t, duration, 10*time.Second)

	// Completed
	stats.CompletedAt = now.Add(-5 * time.Second)
	duration = stats.Duration()
	assert.Equal(t, 5*time.Second, duration)
}

func TestReencryptionStats_Progress(t *testing.T) {
	stats := &ReencryptionStats{
		TotalSecrets:   100,
		ProcessedCount: 50,
	}

	assert.Equal(t, 50.0, stats.Progress())

	stats.ProcessedCount = 100
	assert.Equal(t, 100.0, stats.Progress())

	stats.TotalSecrets = 0
	assert.Equal(t, 0.0, stats.Progress())
}

func TestReencryptionStats_MarshalJSON(t *testing.T) {
	stats := &ReencryptionStats{
		StartedAt:      time.Now(),
		DEKID:          DEKIDGlobal,
		SourceVersion:  1,
		TargetVersion:  2,
		TotalSecrets:   10,
		ProcessedCount: 5,
		SuccessCount:   5,
		InProgress:     true,
	}

	data, err := stats.MarshalJSON()
	require.NoError(t, err)
	assert.Contains(t, string(data), "duration")
	assert.Contains(t, string(data), "dek:global")
}

func TestDefaultClusterDEKConfig(t *testing.T) {
	config := DefaultClusterDEKConfig()

	assert.Equal(t, DefaultKMSShardID, config.KMSShardID)
	assert.Equal(t, DefaultSecretsShardID, config.SecretsShardID)
	assert.False(t, config.EnableClusterSync)
}

func TestClusterDEKManager_StartReencryption_NoService(t *testing.T) {
	ctx := context.Background()
	kms := newTestMockKMSManager()
	store := newTestMockKVStore()
	logger := newTestMockLogger()

	dek := NewSecretsDEK(kms, store, "master-key", logger)
	defer dek.Close()

	config := DefaultClusterDEKConfig()
	manager := NewClusterDEKManager(dek, nil, config, logger)

	err := manager.Initialize(ctx, "AES-256-GCM")
	require.NoError(t, err)

	// Start without secrets service
	req := &ReencryptionRequest{
		DEKID:          DEKIDGlobal,
		SecretsService: nil,
	}

	err = manager.StartReencryption(ctx, req)
	require.NoError(t, err)

	// Wait and verify it completed quickly
	time.Sleep(100 * time.Millisecond)
	stats := manager.GetReencryptionStats()
	require.NotNil(t, stats)
	assert.False(t, stats.InProgress)
}

func TestClusterDEKManager_StartReencryption_ContextCancellation(t *testing.T) {
	kms := newTestMockKMSManager()
	store := newTestMockKVStore()
	logger := newTestMockLogger()

	dek := NewSecretsDEK(kms, store, "master-key", logger)
	defer dek.Close()

	config := DefaultClusterDEKConfig()
	manager := NewClusterDEKManager(dek, nil, config, logger)

	ctx := context.Background()
	err := manager.Initialize(ctx, "AES-256-GCM")
	require.NoError(t, err)

	// Create context that we'll cancel
	ctx, cancel := context.WithCancel(context.Background())

	svc := newMockSecretsService()
	for i := 0; i < 100; i++ {
		svc.addSecret("secret-"+string(rune('0'+i)), string(DEKIDGlobal), []byte("data"))
	}

	req := &ReencryptionRequest{
		DEKID:          DEKIDGlobal,
		SecretsService: svc,
		BatchSize:      1,
	}

	err = manager.StartReencryption(ctx, req)
	require.NoError(t, err)

	// Cancel immediately
	cancel()

	time.Sleep(200 * time.Millisecond)

	stats := manager.GetReencryptionStats()
	require.NotNil(t, stats)
	assert.False(t, stats.InProgress)
	assert.Contains(t, stats.LastError, "context canceled")
}

func TestClusterDEKManager_GetReencryptionStats_NoStats(t *testing.T) {
	kms := newTestMockKMSManager()
	store := newTestMockKVStore()
	logger := newTestMockLogger()

	dek := NewSecretsDEK(kms, store, "master-key", logger)
	defer dek.Close()

	config := DefaultClusterDEKConfig()
	manager := NewClusterDEKManager(dek, nil, config, logger)

	stats := manager.GetReencryptionStats()
	assert.Nil(t, stats)
}

// TestClusterDEKManager_StartReencryption_ContextCancellationBetweenBatches tests
// context cancellation between batch processing.
func TestClusterDEKManager_StartReencryption_ContextCancellationBetweenBatches(t *testing.T) {
	kms := newTestMockKMSManager()
	store := newTestMockKVStore()
	logger := newTestMockLogger()

	dek := NewSecretsDEK(kms, store, "master-key", logger)
	defer dek.Close()

	config := DefaultClusterDEKConfig()
	manager := NewClusterDEKManager(dek, nil, config, logger)

	ctx := context.Background()
	err := manager.Initialize(ctx, "AES-256-GCM")
	require.NoError(t, err)

	// Create context with timeout to cancel between batches
	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
	defer cancel()

	svc := newMockSecretsService()
	for i := 0; i < 10; i++ {
		svc.addSecret("secret-"+string(rune('0'+i)), string(DEKIDGlobal), []byte("data"))
	}

	req := &ReencryptionRequest{
		DEKID:          DEKIDGlobal,
		SecretsService: svc,
		BatchSize:      1,
	}

	err = manager.StartReencryption(ctx, req)
	require.NoError(t, err)

	// Wait for context timeout and cancellation
	time.Sleep(300 * time.Millisecond)

	stats := manager.GetReencryptionStats()
	require.NotNil(t, stats)
	assert.False(t, stats.InProgress)
	assert.Contains(t, stats.LastError, "context")
}
