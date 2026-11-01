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

package backup

import (
	"bytes"
	"context"
	"io"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	dberrors "github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
	"github.com/jeremyhahn/go-quicbaas/pkg/storage"
	"github.com/jeremyhahn/go-quicbaas/pkg/storage/memory"
	"github.com/jeremyhahn/go-quicbaas/pkg/util/conv"
)

// mockRaftNode implements interfaces.RaftNode for testing.
type mockRaftNode struct {
	shardCount int
}

func (m *mockRaftNode) ID() string          { return "mock-backup" }
func (m *mockRaftNode) RaftAddress() string { return "" }
func (m *mockRaftNode) ShardCount() int     { return m.shardCount }
func (m *mockRaftNode) HealthCheck() *interfaces.HealthStatus {
	return &interfaces.HealthStatus{Status: "ok"}
}
func (m *mockRaftNode) Drain() error     { return nil }
func (m *mockRaftNode) IsDraining() bool { return false }

func (m *mockRaftNode) SyncPropose(_ context.Context, _ uint64, _ []byte) (interfaces.Result, error) {
	return interfaces.Result{}, nil
}

func (m *mockRaftNode) ProposeWithMode(_ context.Context, _ uint64, _ []byte, _ interfaces.WriteMode) (interfaces.Result, error) {
	return interfaces.Result{}, nil
}

func (m *mockRaftNode) SyncRead(_ context.Context, _ uint64, _ interface{}) (interface{}, error) {
	return nil, nil
}

func (m *mockRaftNode) StaleRead(_ context.Context, _ uint64, _ interface{}) (interface{}, error) {
	return nil, nil
}

func (m *mockRaftNode) ReadWithConsistency(_ context.Context, _ uint64, _ interface{}, _ interfaces.ReadOptions) (interface{}, error) {
	return nil, nil
}

func (m *mockRaftNode) GetLeaderID(_ uint64) (uint64, bool, error) {
	return 1, true, nil
}

func (m *mockRaftNode) GetShardRaftState(_ uint64) (uint64, uint64, error) {
	// Return mock Raft state for testing
	return 100, 5, nil
}

func (m *mockRaftNode) GetNodeHostInfo(_ interfaces.NodeHostInfoOption) *interfaces.NodeHostInfo {
	// Return a mock NodeHostInfo structure
	shards := make([]interfaces.ShardInfo, m.shardCount)
	for i := 0; i < m.shardCount; i++ {
		shards[i] = interfaces.ShardInfo{
			ShardID: uint64(i + 1),
		}
	}

	return &interfaces.NodeHostInfo{
		ShardInfoList: shards,
	}
}

func (m *mockRaftNode) RequestAddNode(_ context.Context, _, _ uint64, _ string, _ uint64) (interfaces.RequestState, error) {
	return nil, nil
}

func (m *mockRaftNode) RequestRemoveNode(_ context.Context, _, _ uint64, _ uint64) (interfaces.RequestState, error) {
	return nil, nil
}

func (m *mockRaftNode) RequestSnapshot(_ context.Context, _ uint64) (interfaces.RequestState, error) {
	return nil, nil
}

func (m *mockRaftNode) StartShard(_ map[uint64]string, _ bool, _ interfaces.CreateFunc, _ interfaces.ShardConfig) error {
	return nil
}

func (m *mockRaftNode) StopShard(_ uint64) error {
	return nil
}

func (m *mockRaftNode) Close() error {
	return nil
}

// mockStorage implements interfaces.Storage for testing.
type mockStorage struct {
	*memory.MemoryStorage
}

func (m *mockStorage) Batch() storage.Batch {
	// Return a mock batch
	return &mockBatch{storage: m}
}

func (m *mockStorage) NewIterator(opts *storage.IteratorOptions) storage.Iterator {
	// Return a mock iterator
	snapshot := m.MemoryStorage.Snapshot()
	keys := make([]string, 0, len(snapshot))
	for k := range snapshot {
		keys = append(keys, k)
	}
	return &mockIterator{
		snapshot: snapshot,
		keys:     keys,
		index:    -1,
	}
}

func (m *mockStorage) Snapshot() (storage.Snapshot, error) {
	// Return a mock snapshot
	return &mockSnapshot{data: m.MemoryStorage.Snapshot()}, nil
}

func (m *mockStorage) Compact(start, end []byte) error {
	return nil
}

func (m *mockStorage) Flush() error {
	return nil
}

func (m *mockStorage) GetWithContext(ctx context.Context, key []byte) ([]byte, error) {
	return m.Get(key)
}

func (m *mockStorage) SetWithContext(ctx context.Context, key, value []byte) error {
	return m.Set(key, value)
}

// mockBatch implements storage.Batch.
type mockBatch struct {
	storage *mockStorage
	ops     []func() error
}

func (b *mockBatch) Set(key, value []byte) error {
	b.ops = append(b.ops, func() error {
		return b.storage.Set(key, value)
	})
	return nil
}

func (b *mockBatch) Delete(key []byte) error {
	b.ops = append(b.ops, func() error {
		return b.storage.Delete(key)
	})
	return nil
}

func (b *mockBatch) Commit() error {
	for _, op := range b.ops {
		if err := op(); err != nil {
			return err
		}
	}
	b.ops = nil
	return nil
}

func (b *mockBatch) Rollback() error {
	b.ops = nil
	return nil
}

func (b *mockBatch) Close() error {
	return nil
}

// mockIterator implements storage.Iterator.
type mockIterator struct {
	snapshot map[string][]byte
	keys     []string
	index    int
}

func (i *mockIterator) SeekToFirst() bool {
	i.index = 0
	return i.Valid()
}

func (i *mockIterator) Next() bool {
	i.index++
	return i.Valid()
}

func (i *mockIterator) Valid() bool {
	return i.index >= 0 && i.index < len(i.keys)
}

func (i *mockIterator) Prev() bool {
	i.index--
	return i.Valid()
}

func (i *mockIterator) SeekToLast() bool {
	if len(i.keys) == 0 {
		i.index = -1
	} else {
		i.index = len(i.keys) - 1
	}
	return i.Valid()
}

func (i *mockIterator) Seek(target []byte) bool {
	targetStr := string(target)
	for idx, key := range i.keys {
		if key >= targetStr {
			i.index = idx
			return i.Valid()
		}
	}
	i.index = len(i.keys)
	return false
}

func (i *mockIterator) Key() []byte {
	if !i.Valid() {
		return nil
	}
	return []byte(i.keys[i.index])
}

func (i *mockIterator) Value() []byte {
	if !i.Valid() {
		return nil
	}
	return i.snapshot[i.keys[i.index]]
}

func (i *mockIterator) Error() error {
	return nil
}

func (i *mockIterator) Close() error {
	return nil
}

// mockSnapshot implements storage.Snapshot.
type mockSnapshot struct {
	data map[string][]byte
}

func (s *mockSnapshot) Get(key []byte) ([]byte, error) {
	val, ok := s.data[string(key)]
	if !ok {
		return nil, &dberrors.MessageError{Message: "key not found"}
	}
	return val, nil
}

func (s *mockSnapshot) NewIterator(opts *storage.IteratorOptions) storage.Iterator {
	keys := make([]string, 0, len(s.data))
	for k := range s.data {
		keys = append(keys, k)
	}
	return &mockIterator{
		snapshot: s.data,
		keys:     keys,
		index:    -1,
	}
}

func (s *mockSnapshot) Close() error {
	return nil
}

// setupTestBackupManager creates a test backup manager.
func setupTestBackupManager(t *testing.T) (*BackupManager, *mockStorage, *MockStorage) {
	// Create in-memory storage
	memStorage := &mockStorage{
		MemoryStorage: memory.New(nil),
	}

	// Populate with test data
	require.NoError(t, memStorage.Set([]byte("key1"), []byte("value1")))
	require.NoError(t, memStorage.Set([]byte("key2"), []byte("value2")))
	require.NoError(t, memStorage.Set([]byte("key3"), []byte("value3")))

	// Create mock Raft node
	mockRaft := &mockRaftNode{shardCount: 1}

	// Create mock storage backend
	mockBackend := NewMockStorage()

	// Create logger
	log, err := logger.New(logger.LogConfig{
		Level:  "debug",
		Format: "json",
		Output: "stdout",
	})
	require.NoError(t, err)

	// Create backup manager config
	config := &Config{
		StorageBackend:     "mock",
		DefaultCompression: CompressionNone,
		DefaultEncryption:  EncryptionNone,
		WorkDir:            t.TempDir(),
		Timeout:            5 * time.Minute,
		StorageConfig: map[string]string{
			"path": t.TempDir(),
		},
	}

	// Create backup manager
	manager := &BackupManager{
		config:  config,
		storage: memStorage,
		raft:    mockRaft,
		backend: mockBackend,
		logger:  log.WithField("component", "backup-manager"),
		metrics: initBackupMetrics(),
	}

	return manager, memStorage, mockBackend
}

func TestBackupManager_CreateBackup(t *testing.T) {
	tests := []struct {
		name        string
		backupType  BackupType
		compression CompressionType
		encryption  EncryptionType
		wantErr     bool
	}{
		{
			name:        "full backup without compression or encryption",
			backupType:  BackupTypeFull,
			compression: CompressionNone,
			encryption:  EncryptionNone,
			wantErr:     false,
		},
		{
			name:        "full backup with gzip compression",
			backupType:  BackupTypeFull,
			compression: CompressionGzip,
			encryption:  EncryptionNone,
			wantErr:     false,
		},
		{
			name:        "full backup with zstd compression",
			backupType:  BackupTypeFull,
			compression: CompressionZstd,
			encryption:  EncryptionNone,
			wantErr:     false,
		},
		{
			name:        "incremental backup",
			backupType:  BackupTypeIncremental,
			compression: CompressionNone,
			encryption:  EncryptionNone,
			wantErr:     false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			manager, _, _ := setupTestBackupManager(t)
			manager.config.DefaultCompression = tt.compression
			manager.config.DefaultEncryption = tt.encryption

			ctx := context.Background()
			backup, err := manager.CreateBackup(ctx, tt.backupType)

			if tt.wantErr {
				assert.Error(t, err)
				return
			}

			require.NoError(t, err)
			assert.NotNil(t, backup)
			assert.NotEmpty(t, backup.ID)
			assert.Equal(t, tt.backupType, backup.Type)
			assert.Equal(t, tt.compression, backup.Compression)
			assert.Equal(t, tt.encryption, backup.Encryption)
			assert.NotEmpty(t, backup.Checksum)
			assert.Greater(t, backup.Size, int64(0))
			assert.NotNil(t, backup.Metadata)
			assert.NotEmpty(t, backup.Shards)
		})
	}
}

func TestBackupManager_RestoreBackup(t *testing.T) {
	manager, memStorage, _ := setupTestBackupManager(t)

	ctx := context.Background()

	// Create a backup
	backup, err := manager.CreateBackup(ctx, BackupTypeFull)
	require.NoError(t, err)

	// Clear storage
	require.NoError(t, memStorage.Delete([]byte("key1")))
	require.NoError(t, memStorage.Delete([]byte("key2")))
	require.NoError(t, memStorage.Delete([]byte("key3")))

	// Verify keys are deleted
	_, err = memStorage.Get([]byte("key1"))
	assert.Error(t, err)

	// Restore backup
	err = manager.RestoreBackup(ctx, backup.ID, &RestoreOptions{
		VerifyChecksum: true,
	})
	require.NoError(t, err)

	// Verify data is restored
	value, err := memStorage.Get([]byte("key1"))
	require.NoError(t, err)
	assert.Equal(t, []byte("value1"), value)

	value, err = memStorage.Get([]byte("key2"))
	require.NoError(t, err)
	assert.Equal(t, []byte("value2"), value)

	value, err = memStorage.Get([]byte("key3"))
	require.NoError(t, err)
	assert.Equal(t, []byte("value3"), value)
}

func TestBackupManager_RestoreBackupWithEncryption(t *testing.T) {
	manager, memStorage, _ := setupTestBackupManager(t)

	// Set encryption key
	key := make([]byte, 32)
	for i := range key {
		key[i] = byte(i)
	}
	manager.config.EncryptionKey = key
	manager.config.DefaultEncryption = EncryptionAES256GCM

	ctx := context.Background()

	// Create encrypted backup
	backup, err := manager.CreateBackup(ctx, BackupTypeFull)
	require.NoError(t, err)
	assert.Equal(t, EncryptionAES256GCM, backup.Encryption)

	// Clear storage
	require.NoError(t, memStorage.Delete([]byte("key1")))

	// Restore backup
	err = manager.RestoreBackup(ctx, backup.ID, &RestoreOptions{
		VerifyChecksum: true,
	})
	require.NoError(t, err)

	// Verify data is restored
	value, err := memStorage.Get([]byte("key1"))
	require.NoError(t, err)
	assert.Equal(t, []byte("value1"), value)
}

func TestBackupManager_RestoreBackupSkipExisting(t *testing.T) {
	manager, memStorage, _ := setupTestBackupManager(t)

	ctx := context.Background()

	// Create backup
	backup, err := manager.CreateBackup(ctx, BackupTypeFull)
	require.NoError(t, err)

	// Modify one key
	require.NoError(t, memStorage.Set([]byte("key1"), []byte("modified")))

	// Restore with skip existing
	err = manager.RestoreBackup(ctx, backup.ID, &RestoreOptions{
		VerifyChecksum: true,
		SkipExisting:   true,
	})
	require.NoError(t, err)

	// Verify key1 is still modified
	value, err := memStorage.Get([]byte("key1"))
	require.NoError(t, err)
	assert.Equal(t, []byte("modified"), value)

	// Verify other keys are unchanged
	value, err = memStorage.Get([]byte("key2"))
	require.NoError(t, err)
	assert.Equal(t, []byte("value2"), value)
}

// TestBackupManager_ListBackups moved to manager_integration_test.go

func TestBackupManager_DeleteBackup(t *testing.T) {
	manager, _, mockBackend := setupTestBackupManager(t)

	ctx := context.Background()

	// Create backup
	backup, err := manager.CreateBackup(ctx, BackupTypeFull)
	require.NoError(t, err)

	// Verify backup exists using correct key format
	exists, err := mockBackend.Exists(ctx, backup.ID+"/metadata.json")
	require.NoError(t, err)
	require.True(t, exists, "backup metadata should exist after creation")

	// Delete backup
	err = manager.DeleteBackup(ctx, backup.ID)
	require.NoError(t, err)

	// Verify backup is deleted
	exists, err = mockBackend.Exists(ctx, backup.ID+"/metadata.json")
	require.NoError(t, err)
	assert.False(t, exists, "backup metadata should not exist after deletion")
}

func TestBackupManager_ChecksumValidation(t *testing.T) {
	manager, _, mockBackend := setupTestBackupManager(t)

	ctx := context.Background()

	// Create backup
	backup, err := manager.CreateBackup(ctx, BackupTypeFull)
	require.NoError(t, err)

	// Corrupt the backup data using correct key format
	mockBackend.data[backup.ID+"/data"] = []byte("corrupted data")

	// Attempt to restore - should fail checksum validation
	err = manager.RestoreBackup(ctx, backup.ID, &RestoreOptions{
		VerifyChecksum: true,
	})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "checksum")
}

func TestBackupManager_ConcurrentBackups(t *testing.T) {
	manager, _, _ := setupTestBackupManager(t)

	ctx := context.Background()
	numBackups := 5

	// Create backups concurrently
	errChan := make(chan error, numBackups)
	for i := 0; i < numBackups; i++ {
		go func() {
			_, err := manager.CreateBackup(ctx, BackupTypeFull)
			errChan <- err
		}()
	}

	// Wait for all backups to complete
	for i := 0; i < numBackups; i++ {
		err := <-errChan
		assert.NoError(t, err)
	}

	// Verify all backups created
	list, err := manager.ListBackups(ctx, 100)
	require.NoError(t, err)
	assert.Len(t, list.Backups, numBackups)
}

func TestCompressionTypes(t *testing.T) {
	tests := []struct {
		name            string
		compressionType CompressionType
		testData        string
	}{
		{
			name:            "no compression",
			compressionType: CompressionNone,
			testData:        "test data without compression",
		},
		{
			name:            "gzip compression",
			compressionType: CompressionGzip,
			testData:        "test data with gzip compression",
		},
		{
			name:            "zstd compression",
			compressionType: CompressionZstd,
			testData:        "test data with zstd compression",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			compressor, err := NewCompressor(tt.compressionType)
			require.NoError(t, err)

			// Compress data
			var compressed bytes.Buffer
			writer, err := compressor.Compress(&compressed)
			require.NoError(t, err)

			_, err = writer.Write([]byte(tt.testData))
			require.NoError(t, err)

			err = writer.Close()
			require.NoError(t, err)

			// Decompress data
			reader, err := compressor.Decompress(&compressed)
			require.NoError(t, err)

			decompressed, err := io.ReadAll(reader)
			require.NoError(t, err)

			assert.Equal(t, tt.testData, string(decompressed))
		})
	}
}

func TestEncryptionTypes(t *testing.T) {
	tests := []struct {
		name           string
		encryptionType EncryptionType
		key            []byte
		testData       string
		wantErr        bool
	}{
		{
			name:           "no encryption",
			encryptionType: EncryptionNone,
			key:            nil,
			testData:       "test data without encryption",
			wantErr:        false,
		},
		{
			name:           "AES-256-GCM encryption",
			encryptionType: EncryptionAES256GCM,
			key:            make([]byte, 32),
			testData:       "test data with AES-256-GCM encryption",
			wantErr:        false,
		},
		{
			name:           "AES-256-GCM empty payload",
			encryptionType: EncryptionAES256GCM,
			key:            make([]byte, 32),
			testData:       "",
			wantErr:        false,
		},
		{
			name:           "AES-256-GCM large payload",
			encryptionType: EncryptionAES256GCM,
			key:            make([]byte, 32),
			testData:       strings.Repeat("encrypt this large block of data ", 100),
			wantErr:        false,
		},
		{
			name:           "AES-256-GCM with invalid key size",
			encryptionType: EncryptionAES256GCM,
			key:            make([]byte, 16),
			testData:       "test data",
			wantErr:        true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			encryptor, err := NewEncryptor(tt.encryptionType, tt.key)
			if tt.wantErr {
				assert.Error(t, err)
				return
			}
			require.NoError(t, err)

			// Encrypt data
			var encrypted bytes.Buffer
			writer, err := encryptor.Encrypt(&encrypted)
			require.NoError(t, err)

			_, err = writer.Write([]byte(tt.testData))
			require.NoError(t, err)

			err = writer.Close()
			require.NoError(t, err)

			// Decrypt data
			reader, err := encryptor.Decrypt(&encrypted)
			require.NoError(t, err)

			decrypted, err := io.ReadAll(reader)
			require.NoError(t, err)

			assert.Equal(t, tt.testData, string(decrypted))
		})
	}
}

func TestBackupManager_ClosedOperations(t *testing.T) {
	manager, _, _ := setupTestBackupManager(t)

	err := manager.Close()
	require.NoError(t, err)

	ctx := context.Background()

	// All operations should fail when closed
	_, err = manager.CreateBackup(ctx, BackupTypeFull)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "closed")

	err = manager.RestoreBackup(ctx, "backup-id", nil)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "closed")

	_, err = manager.ListBackups(ctx, 10)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "closed")

	err = manager.DeleteBackup(ctx, "backup-id")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "closed")

	// Close is idempotent
	require.NoError(t, manager.Close())
}

func TestNewBackupManager_Validation(t *testing.T) {
	log, err := logger.New(logger.LogConfig{
		Level:  "debug",
		Format: "json",
		Output: "stdout",
	})
	require.NoError(t, err)

	memStorage := &mockStorage{
		MemoryStorage: memory.New(nil),
	}

	mockRaft := &mockRaftNode{shardCount: 1}

	tests := []struct {
		name        string
		config      *Config
		storage     interfaces.Storage
		raft        interfaces.RaftNode
		logger      *logger.Logger
		wantErr     bool
		errContains string
	}{
		{
			name:        "nil config",
			config:      nil,
			storage:     memStorage,
			raft:        mockRaft,
			logger:      log,
			wantErr:     true,
			errContains: "config is required",
		},
		{
			name: "nil storage",
			config: &Config{
				StorageBackend: "mock",
				WorkDir:        t.TempDir(),
				StorageConfig:  map[string]string{"path": t.TempDir()},
			},
			storage:     nil,
			raft:        mockRaft,
			logger:      log,
			wantErr:     true,
			errContains: "storage is required",
		},
		{
			// nil raft is accepted: on the qrdb-node path the backup manager
			// does not require a standalone raft node. The backup will have
			// zero shards but construction must succeed.
			name: "nil raft accepted",
			config: &Config{
				StorageBackend: "mock",
				WorkDir:        t.TempDir(),
				StorageConfig:  map[string]string{"path": t.TempDir()},
			},
			storage:     memStorage,
			raft:        nil,
			logger:      log,
			wantErr:     false,
			errContains: "",
		},
		{
			name: "nil logger",
			config: &Config{
				StorageBackend: "mock",
				WorkDir:        t.TempDir(),
				StorageConfig:  map[string]string{"path": t.TempDir()},
			},
			storage:     memStorage,
			raft:        mockRaft,
			logger:      nil,
			wantErr:     true,
			errContains: "logger is required",
		},
		{
			name: "invalid config",
			config: &Config{
				StorageBackend: "invalid",
				WorkDir:        t.TempDir(),
			},
			storage:     memStorage,
			raft:        mockRaft,
			logger:      log,
			wantErr:     true,
			errContains: "unsupported storage backend",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			manager, err := NewBackupManager(tt.config, tt.storage, tt.raft, tt.logger)
			if tt.wantErr {
				assert.Error(t, err)
				if tt.errContains != "" {
					assert.Contains(t, err.Error(), tt.errContains)
				}
				assert.Nil(t, manager)
				return
			}
			require.NoError(t, err)
			assert.NotNil(t, manager)
			manager.Close()
		})
	}
}

func TestBackupManager_EmptyStorage(t *testing.T) {
	manager, _, _ := setupTestBackupManager(t)

	// Clear all data
	memStorage := &mockStorage{
		MemoryStorage: memory.New(nil),
	}
	manager.storage = memStorage

	ctx := context.Background()
	backup, err := manager.CreateBackup(ctx, BackupTypeFull)
	require.NoError(t, err)

	assert.NotNil(t, backup)
	assert.NotEmpty(t, backup.ID)

	// Backup of empty storage should work
	for _, shard := range backup.Shards {
		assert.NotNil(t, shard)
	}
}

func TestBackupManager_CreateBackupShardCount(t *testing.T) {
	tests := []struct {
		name       string
		shardCount int
	}{
		{name: "no shards", shardCount: 0},
		{name: "five shards", shardCount: 5},
	}

	for _, tt := range tests {
		shardCount := tt.shardCount
		t.Run(tt.name, func(t *testing.T) {
			memStorage := &mockStorage{
				MemoryStorage: memory.New(nil),
			}
			require.NoError(t, memStorage.Set([]byte("key1"), []byte("value1")))

			log, err := logger.New(logger.LogConfig{
				Level:  "debug",
				Format: "json",
				Output: "stdout",
			})
			require.NoError(t, err)

			config := &Config{
				StorageBackend:     "mock",
				DefaultCompression: CompressionNone,
				DefaultEncryption:  EncryptionNone,
				WorkDir:            t.TempDir(),
				Timeout:            5 * time.Minute,
				StorageConfig: map[string]string{
					"path": t.TempDir(),
				},
			}

			manager := &BackupManager{
				config:  config,
				storage: memStorage,
				raft:    &mockRaftNode{shardCount: shardCount},
				backend: NewMockStorage(),
				logger:  log.WithField("component", "backup-manager"),
				metrics: initBackupMetrics(),
			}
			defer manager.Close()

			backup, err := manager.CreateBackup(context.Background(), BackupTypeFull)
			require.NoError(t, err)

			assert.Len(t, backup.Shards, shardCount)
			assert.Equal(t, shardCount, backup.Metadata.NumShards)
		})
	}
}

func TestBackupManager_RestoreOptions(t *testing.T) {
	tests := []struct {
		name string
		opts *RestoreOptions
	}{
		{name: "target shards", opts: &RestoreOptions{VerifyChecksum: true, TargetShards: []uint64{1}}},
		{name: "nil options use defaults", opts: nil},
		{name: "checksum verification disabled", opts: &RestoreOptions{VerifyChecksum: false}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			manager, memStorage, _ := setupTestBackupManager(t)
			ctx := context.Background()

			backup, err := manager.CreateBackup(ctx, BackupTypeFull)
			require.NoError(t, err)

			require.NoError(t, memStorage.Delete([]byte("key1")))

			require.NoError(t, manager.RestoreBackup(ctx, backup.ID, tt.opts))

			value, err := memStorage.Get([]byte("key1"))
			require.NoError(t, err)
			assert.Equal(t, []byte("value1"), value)
		})
	}
}

func TestBackupManager_RestoreBackupNotFound(t *testing.T) {
	manager, _, _ := setupTestBackupManager(t)

	ctx := context.Background()

	err := manager.RestoreBackup(ctx, "nonexistent-backup", &RestoreOptions{
		VerifyChecksum: true,
	})
	assert.Error(t, err)
}

// TestBackupManager_ApplyRetentionPolicy moved to manager_integration_test.go
// TestBackupManager_ApplyRetentionPolicyMaxAge moved to manager_integration_test.go

func TestBackupManager_ApplyRetentionPolicyNoPolicy(t *testing.T) {
	tests := []struct {
		name     string
		schedule *ScheduleConfig
	}{
		{name: "nil schedule", schedule: nil},
		{name: "nil retention policy", schedule: &ScheduleConfig{CronExpression: "0 0 * * *"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			manager, _, _ := setupTestBackupManager(t)
			defer manager.Close()

			manager.config.Schedule = tt.schedule

			// With no retention policy there is nothing to apply.
			require.NoError(t, manager.ApplyRetentionPolicy(context.Background()))
		})
	}
}

func TestBackupManager_CreateBackupAllCompressionTypes(t *testing.T) {
	tests := []struct {
		compression CompressionType
		wantErr     bool
	}{
		{compression: CompressionNone},
		{compression: CompressionGzip},
		{compression: CompressionZstd},
		{compression: CompressionType("snappy"), wantErr: true},
	}

	for _, tt := range tests {
		t.Run(string(tt.compression), func(t *testing.T) {
			manager, memStorage, _ := setupTestBackupManager(t)
			manager.config.DefaultCompression = tt.compression

			ctx := context.Background()
			backup, err := manager.CreateBackup(ctx, BackupTypeFull)
			if tt.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), "unsupported compression type")
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.compression, backup.Compression)

			require.NoError(t, memStorage.Delete([]byte("key1")))

			err = manager.RestoreBackup(ctx, backup.ID, &RestoreOptions{
				VerifyChecksum: true,
			})
			require.NoError(t, err)

			value, err := memStorage.Get([]byte("key1"))
			require.NoError(t, err)
			assert.Equal(t, []byte("value1"), value)
		})
	}
}

// TestBackupManager_LargeBackup moved to manager_integration_test.go

func TestKeyValueSerializationHelpers(t *testing.T) {
	var buf bytes.Buffer

	// Write key-value pairs
	err := writeKeyValue(&buf, []byte("key1"), []byte("value1"))
	require.NoError(t, err)

	err = writeKeyValue(&buf, []byte("key2"), []byte("value2"))
	require.NoError(t, err)

	// Read them back
	reader := bytes.NewReader(buf.Bytes())

	key, value, err := readKeyValue(reader)
	require.NoError(t, err)
	assert.Equal(t, []byte("key1"), key)
	assert.Equal(t, []byte("value1"), value)

	key, value, err = readKeyValue(reader)
	require.NoError(t, err)
	assert.Equal(t, []byte("key2"), key)
	assert.Equal(t, []byte("value2"), value)

	// Should be EOF now
	_, _, err = readKeyValue(reader)
	assert.Equal(t, io.EOF, err)
}

func TestKeyValueSerialization_EmptyKeyValue(t *testing.T) {
	var buf bytes.Buffer

	// Write empty key and value
	err := writeKeyValue(&buf, []byte{}, []byte{})
	require.NoError(t, err)

	// Read back
	reader := bytes.NewReader(buf.Bytes())
	key, value, err := readKeyValue(reader)
	require.NoError(t, err)
	assert.Empty(t, key)
	assert.Empty(t, value)
}

func TestKeyValueSerialization_LargeValues(t *testing.T) {
	var buf bytes.Buffer

	largeKey := make([]byte, 10000)
	largeValue := make([]byte, 100000)

	for i := range largeKey {
		largeKey[i] = byte(i % 256)
	}
	for i := range largeValue {
		largeValue[i] = byte(i % 256)
	}

	err := writeKeyValue(&buf, largeKey, largeValue)
	require.NoError(t, err)

	reader := bytes.NewReader(buf.Bytes())
	key, value, err := readKeyValue(reader)
	require.NoError(t, err)
	assert.Equal(t, largeKey, key)
	assert.Equal(t, largeValue, value)
}

// TestNewBackupManager_WithSchedule moved to manager_integration_test.go

func TestNewBackupManager_WorkDirCreation(t *testing.T) {
	memStorage := &mockStorage{
		MemoryStorage: memory.New(nil),
	}

	mockRaft := &mockRaftNode{shardCount: 1}

	log, err := logger.New(logger.LogConfig{
		Level:  "debug",
		Format: "json",
		Output: "stdout",
	})
	require.NoError(t, err)

	workDir := filepath.Join(t.TempDir(), "backup-workdir")

	config := &Config{
		StorageBackend:     "mock",
		DefaultCompression: CompressionNone,
		DefaultEncryption:  EncryptionNone,
		WorkDir:            workDir,
		Timeout:            5 * time.Minute,
		StorageConfig: map[string]string{
			"path": t.TempDir(),
		},
	}

	manager, err := NewBackupManager(config, memStorage, mockRaft, log)
	require.NoError(t, err)

	// Verify work directory was created
	info, err := os.Stat(workDir)
	require.NoError(t, err)
	assert.True(t, info.IsDir())

	err = manager.Close()
	require.NoError(t, err)
}

func TestWriteUint32(t *testing.T) {
	tests := []struct {
		name  string
		value uint32
	}{
		{"zero", 0},
		{"max", 0xFFFFFFFF},
		{"mid", 0x12345678},
		{"one", 1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var buf bytes.Buffer
			err := writeUint32(&buf, tt.value)
			require.NoError(t, err)

			assert.Equal(t, 4, buf.Len())

			// Read back
			result, err := readUint32(&buf)
			require.NoError(t, err)
			assert.Equal(t, tt.value, result)
		})
	}
}

func TestWriteLength(t *testing.T) {
	var buf bytes.Buffer
	require.NoError(t, writeLength(&buf, math.MaxUint32))
	result, err := readUint32(&buf)
	require.NoError(t, err)
	assert.Equal(t, uint32(math.MaxUint32), result)
}

func TestWriteLength_RefusesLengthBeyondPrefix(t *testing.T) {
	var buf bytes.Buffer
	err := writeLength(&buf, math.MaxUint32+1)

	var overflow *conv.ConversionOverflowError
	require.ErrorAs(t, err, &overflow)
	assert.Zero(t, buf.Len(), "nothing may be written for a refused length")
}

func TestReadUint32_InsufficientData(t *testing.T) {
	// Test with less than 4 bytes
	buf := bytes.NewReader([]byte{0x01, 0x02})
	_, err := readUint32(buf)
	assert.Error(t, err)
}

func TestReadKeyValue_TruncatedKey(t *testing.T) {
	var buf bytes.Buffer

	// Write a key length but no key data
	err := writeUint32(&buf, 10)
	require.NoError(t, err)

	reader := bytes.NewReader(buf.Bytes())
	_, _, err = readKeyValue(reader)
	assert.Error(t, err)
}

func TestReadKeyValue_TruncatedValue(t *testing.T) {
	var buf bytes.Buffer

	// Write key
	err := writeUint32(&buf, 4)
	require.NoError(t, err)
	buf.Write([]byte("test"))

	// Write value length but no value data
	err = writeUint32(&buf, 100)
	require.NoError(t, err)

	reader := bytes.NewReader(buf.Bytes())
	_, _, err = readKeyValue(reader)
	assert.Error(t, err)
}

// TestBackupManager_ListBackupsLimit moved to manager_integration_test.go

func TestNoneEncryptor_ReturnReadCloser(t *testing.T) {
	encryptor := &noneEncryptor{}

	// Test decrypt with io.ReadCloser
	data := []byte("test")
	rc := io.NopCloser(bytes.NewReader(data))

	result, err := encryptor.Decrypt(rc)
	require.NoError(t, err)
	assert.Equal(t, rc, result)
}

func TestBackupManager_CreateBackupErrorPaths(t *testing.T) {
	t.Run("nil node host info produces zero-shard backup", func(t *testing.T) {
		// nil GetNodeHostInfo no longer produces an error; the backup manager
		// treats it as zero shards (graceful degradation for the qrdb-node path).
		memStorage := &mockStorage{MemoryStorage: memory.New(nil)}
		mockRaft := &mockRaftNodeNilInfo{}

		log, err := logger.New(logger.LogConfig{
			Level:  "debug",
			Format: "json",
			Output: "stdout",
		})
		require.NoError(t, err)

		config := &Config{
			StorageBackend:     "mock",
			DefaultCompression: CompressionNone,
			DefaultEncryption:  EncryptionNone,
			WorkDir:            t.TempDir(),
			StorageConfig: map[string]string{
				"path": t.TempDir(),
			},
		}

		manager := &BackupManager{
			config:  config,
			storage: memStorage,
			raft:    mockRaft,
			backend: NewMockStorage(),
			logger:  log.WithField("component", "backup-manager"),
			metrics: initBackupMetrics(),
		}

		ctx := context.Background()
		backup, err := manager.CreateBackup(ctx, BackupTypeFull)
		assert.NoError(t, err, "nil NodeHostInfo should succeed with zero shards")
		if err == nil {
			assert.Equal(t, 0, backup.Metadata.NumShards)
		}
	})
}

// mockRaftNodeNilInfo returns nil for GetNodeHostInfo.
type mockRaftNodeNilInfo struct{}

func (m *mockRaftNodeNilInfo) ID() string          { return "mock-nil-info" }
func (m *mockRaftNodeNilInfo) RaftAddress() string { return "" }
func (m *mockRaftNodeNilInfo) ShardCount() int     { return 0 }
func (m *mockRaftNodeNilInfo) HealthCheck() *interfaces.HealthStatus {
	return &interfaces.HealthStatus{Status: "unhealthy"}
}
func (m *mockRaftNodeNilInfo) Drain() error     { return nil }
func (m *mockRaftNodeNilInfo) IsDraining() bool { return false }

func (m *mockRaftNodeNilInfo) SyncPropose(_ context.Context, _ uint64, _ []byte) (interfaces.Result, error) {
	return interfaces.Result{}, nil
}

func (m *mockRaftNodeNilInfo) ProposeWithMode(_ context.Context, _ uint64, _ []byte, _ interfaces.WriteMode) (interfaces.Result, error) {
	return interfaces.Result{}, nil
}

func (m *mockRaftNodeNilInfo) SyncRead(_ context.Context, _ uint64, _ interface{}) (interface{}, error) {
	return nil, nil
}

func (m *mockRaftNodeNilInfo) StaleRead(_ context.Context, _ uint64, _ interface{}) (interface{}, error) {
	return nil, nil
}

func (m *mockRaftNodeNilInfo) ReadWithConsistency(_ context.Context, _ uint64, _ interface{}, _ interfaces.ReadOptions) (interface{}, error) {
	return nil, nil
}

func (m *mockRaftNodeNilInfo) GetLeaderID(_ uint64) (uint64, bool, error) {
	return 1, true, nil
}

func (m *mockRaftNodeNilInfo) GetShardRaftState(_ uint64) (uint64, uint64, error) {
	return 0, 0, &dberrors.MessageError{Message: "shard not found"}
}

func (m *mockRaftNodeNilInfo) GetNodeHostInfo(_ interfaces.NodeHostInfoOption) *interfaces.NodeHostInfo {
	return nil
}

func (m *mockRaftNodeNilInfo) RequestAddNode(_ context.Context, _, _ uint64, _ string, _ uint64) (interfaces.RequestState, error) {
	return nil, nil
}

func (m *mockRaftNodeNilInfo) RequestRemoveNode(_ context.Context, _, _ uint64, _ uint64) (interfaces.RequestState, error) {
	return nil, nil
}

func (m *mockRaftNodeNilInfo) RequestSnapshot(_ context.Context, _ uint64) (interfaces.RequestState, error) {
	return nil, nil
}

func (m *mockRaftNodeNilInfo) StartShard(_ map[uint64]string, _ bool, _ interfaces.CreateFunc, _ interfaces.ShardConfig) error {
	return nil
}

func (m *mockRaftNodeNilInfo) StopShard(_ uint64) error {
	return nil
}

func (m *mockRaftNodeNilInfo) Close() error {
	return nil
}

// TestAES256GCM_DecryptTruncatedData tests decrypting truncated ciphertext.
func TestAES256GCM_DecryptTruncatedData(t *testing.T) {
	key := make([]byte, 32)
	for i := range key {
		key[i] = byte(i)
	}

	encryptor, err := NewAES256GCMEncryptor(key)
	require.NoError(t, err)

	// Truncated data too short for nonce
	_, err = encryptor.Decrypt(bytes.NewReader([]byte{0x01, 0x02}))
	assert.Error(t, err)
}

// TestMockStorage_PutAndGetCoverage tests MockStorage put and get operations.
func TestMockStorage_PutAndGetCoverage(t *testing.T) {
	storage := NewMockStorage()

	data := bytes.NewReader([]byte("test payload"))
	err := storage.Put("put-get-key", data)
	require.NoError(t, err)

	// Get the stored data
	reader, err := storage.Get("put-get-key")
	require.NoError(t, err)
	defer reader.Close()

	result, err := io.ReadAll(reader)
	require.NoError(t, err)
	assert.Equal(t, []byte("test payload"), result)
}

// TestBackupManager_GetNextScheduledBackup_NoCron tests GetNextScheduledBackup
// when scheduler is nil by using setupTestBackupManager.
func TestBackupManager_GetNextScheduledBackup_NoCron(t *testing.T) {
	mgr, _, _ := setupTestBackupManager(t)
	defer mgr.Close()

	nextTime, ok := mgr.GetNextScheduledBackup()
	assert.False(t, ok)
	assert.True(t, nextTime.IsZero())
}

// TestBackupManager_IsSchedulerRunning_NotStarted tests IsSchedulerRunning
// when scheduler has not been started.
func TestBackupManager_IsSchedulerRunning_NotStarted(t *testing.T) {
	mgr, _, _ := setupTestBackupManager(t)
	defer mgr.Close()

	assert.False(t, mgr.IsSchedulerRunning())
}

// TestMockStorage_PutWithContextCoverage tests MockStorage PutWithContext.
func TestMockStorage_PutWithContextCoverage(t *testing.T) {
	storage := NewMockStorage()
	ctx := context.Background()

	data := bytes.NewReader([]byte("context-payload"))
	err := storage.PutWithContext(ctx, "ctx-key", data)
	require.NoError(t, err)

	reader, err := storage.GetWithContext(ctx, "ctx-key")
	require.NoError(t, err)
	defer reader.Close()

	result, err := io.ReadAll(reader)
	require.NoError(t, err)
	assert.Equal(t, []byte("context-payload"), result)
}

// TestMockStorage_DeleteCoverage tests MockStorage Delete.
func TestMockStorage_DeleteCoverage(t *testing.T) {
	storage := NewMockStorage()

	// Put then delete
	err := storage.Put("del-key", bytes.NewReader([]byte("value")))
	require.NoError(t, err)

	err = storage.Delete("del-key")
	require.NoError(t, err)

	// Get should fail after delete
	_, err = storage.Get("del-key")
	assert.Error(t, err)
}
