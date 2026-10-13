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
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/google/uuid"
	"github.com/robfig/cron/v3"

	"github.com/jeremyhahn/go-objstore/pkg/common"

	api "github.com/jeremyhahn/go-quicbaas/pkg/api"
	dberrors "github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
	"github.com/jeremyhahn/go-quicbaas/pkg/storage"
	"github.com/jeremyhahn/go-quicbaas/pkg/util/conv"
	"github.com/jeremyhahn/go-quicbaas/pkg/util/metrics"
	"github.com/jeremyhahn/go-quicbaas/pkg/version"
)

// BackupManager manages backup and restore operations.
type BackupManager struct {
	config  *Config
	storage interfaces.Storage
	raft    interfaces.RaftNode
	backend common.Storage // go-objstore storage facade
	prefix  string         // key prefix for backup storage
	logger  *logger.Logger
	metrics *backupMetrics

	// Cron scheduler for scheduled backups
	cronScheduler *cron.Cron
	cronEntryID   cron.EntryID
	cronRunning   atomic.Bool
	cronStopCh    chan struct{}

	mu     sync.RWMutex
	closed bool
}

// backupMetrics contains Prometheus metrics for backup operations.
type backupMetrics struct {
	backupsTotal        *metrics.Counter
	restoresTotal       *metrics.Counter
	backupDuration      *metrics.Histogram
	restoreDuration     *metrics.Histogram
	backupSize          *metrics.Histogram
	backupErrors        *metrics.Counter
	restoreErrors       *metrics.Counter
	activeBackups       *metrics.Gauge
	lastBackupTime      *metrics.Gauge
	scheduledBackups    *metrics.Counter
	scheduledErrors     *metrics.Counter
	retentionExecutions *metrics.Counter
}

var (
	globalBackupMetrics     *backupMetrics
	globalBackupMetricsOnce sync.Once
)

// NewBackupManager creates a new backup manager.
func NewBackupManager(
	config *Config,
	storage interfaces.Storage,
	raft interfaces.RaftNode,
	log *logger.Logger,
) (*BackupManager, error) {
	if config == nil {
		return nil, &InvalidConfigError{Field: "config", Message: "config is required"}
	}
	if storage == nil {
		return nil, &InvalidConfigError{Field: "storage", Message: "storage is required"}
	}
	// raft may be nil when the caller has a real qrdb engine node
	// providing consensus (s.qrdbNode path). Backup snapshots degrade
	// gracefully: GetNodeHostInfo returns an empty info so no shard
	// snapshots are captured; GetShardRaftState returns (0, 0) via
	// the nil-safe guard in createShardSnapshot. A nil raft is therefore
	// accepted so the backup manager can still manage metadata, scheduling,
	// and restore without requiring a standalone no-op raft node.
	if log == nil {
		return nil, &InvalidConfigError{Field: "logger", Message: "logger is required"}
	}

	// Validate and apply defaults
	if err := config.Validate(); err != nil {
		return nil, &InvalidConfigError{Field: "config", Message: "validation failed", Cause: err}
	}
	config = config.WithDefaults()

	// Create work directory first (before storage backend) with secure permissions
	if err := os.MkdirAll(config.WorkDir, 0o750); err != nil {
		return nil, api.NewOperationFailedError("create work directory", err)
	}

	// Create storage backend using go-objstore factory
	backend, err := NewStorageBackend(config)
	if err != nil {
		return nil, api.NewOperationFailedError("create storage backend", err)
	}

	// Get prefix from config
	prefix := config.StorageConfig["prefix"]
	if prefix == "" {
		prefix = "backups"
	}

	bm := &BackupManager{
		config:     config,
		storage:    storage,
		raft:       raft,
		backend:    backend,
		prefix:     prefix,
		logger:     log.WithField("component", "backup-manager"),
		metrics:    initBackupMetrics(),
		cronStopCh: make(chan struct{}),
	}

	// Start scheduled backups if enabled
	if config.Schedule != nil && config.Schedule.Enabled {
		if err := bm.startScheduledBackups(); err != nil {
			// Clean up on failure
			if closer, ok := backend.(io.Closer); ok {
				if closeErr := closer.Close(); closeErr != nil {
					bm.logger.WithError(closeErr).Warn("Failed to close backend during cleanup")
				}
			}
			return nil, err
		}
	}

	bm.logger.Info("Backup manager initialized")

	return bm, nil
}

// initBackupMetrics initializes backup metrics.
func initBackupMetrics() *backupMetrics {
	globalBackupMetricsOnce.Do(func() {
		globalBackupMetrics = &backupMetrics{
			backupsTotal: metrics.NewCounter(metrics.CounterOpts{
				Namespace: "quicbaas",
				Subsystem: "backup",
				Name:      "backups_total",
				Help:      "Total number of backups created",
			}),
			restoresTotal: metrics.NewCounter(metrics.CounterOpts{
				Namespace: "quicbaas",
				Subsystem: "backup",
				Name:      "restores_total",
				Help:      "Total number of restores performed",
			}),
			backupDuration: metrics.NewHistogram(metrics.HistogramOpts{
				Namespace: "quicbaas",
				Subsystem: "backup",
				Name:      "backup_duration_seconds",
				Help:      "Histogram of backup operation durations",
				Buckets:   []float64{1, 5, 10, 30, 60, 300, 600, 1800, 3600},
			}),
			restoreDuration: metrics.NewHistogram(metrics.HistogramOpts{
				Namespace: "quicbaas",
				Subsystem: "backup",
				Name:      "restore_duration_seconds",
				Help:      "Histogram of restore operation durations",
				Buckets:   []float64{1, 5, 10, 30, 60, 300, 600, 1800, 3600},
			}),
			backupSize: metrics.NewHistogram(metrics.HistogramOpts{
				Namespace: "quicbaas",
				Subsystem: "backup",
				Name:      "backup_size_bytes",
				Help:      "Histogram of backup sizes",
				Buckets:   []float64{1048576, 10485760, 104857600, 1048576000}, // 1MB to 10GB
			}),
			backupErrors: metrics.NewCounter(metrics.CounterOpts{
				Namespace: "quicbaas",
				Subsystem: "backup",
				Name:      "backup_errors_total",
				Help:      "Total number of backup errors",
			}),
			restoreErrors: metrics.NewCounter(metrics.CounterOpts{
				Namespace: "quicbaas",
				Subsystem: "backup",
				Name:      "restore_errors_total",
				Help:      "Total number of restore errors",
			}),
			activeBackups: metrics.NewGauge(metrics.GaugeOpts{
				Namespace: "quicbaas",
				Subsystem: "backup",
				Name:      "active_backups",
				Help:      "Number of backups currently in progress",
			}),
			lastBackupTime: metrics.NewGauge(metrics.GaugeOpts{
				Namespace: "quicbaas",
				Subsystem: "backup",
				Name:      "last_backup_timestamp_seconds",
				Help:      "Timestamp of the last successful backup",
			}),
			scheduledBackups: metrics.NewCounter(metrics.CounterOpts{
				Namespace: "quicbaas",
				Subsystem: "backup",
				Name:      "scheduled_backups_total",
				Help:      "Total number of scheduled backups executed",
			}),
			scheduledErrors: metrics.NewCounter(metrics.CounterOpts{
				Namespace: "quicbaas",
				Subsystem: "backup",
				Name:      "scheduled_backup_errors_total",
				Help:      "Total number of scheduled backup errors",
			}),
			retentionExecutions: metrics.NewCounter(metrics.CounterOpts{
				Namespace: "quicbaas",
				Subsystem: "backup",
				Name:      "retention_executions_total",
				Help:      "Total number of retention policy executions",
			}),
		}
	})
	return globalBackupMetrics
}

// CreateBackup creates a new backup.
func (m *BackupManager) CreateBackup(ctx context.Context, backupType BackupType) (*Backup, error) {
	m.mu.RLock()
	if m.closed {
		m.mu.RUnlock()
		return nil, &ManagerClosedError{Op: "CreateBackup"}
	}
	m.mu.RUnlock()

	start := time.Now()
	m.metrics.activeBackups.Inc()
	defer m.metrics.activeBackups.Dec()

	m.logger.WithFields(map[string]interface{}{
		"type": backupType,
	}).Info("Starting backup")

	// Create backup ID
	backupID := fmt.Sprintf("backup-%s", uuid.New().String())

	// Create backup metadata
	backup := &Backup{
		ID:          backupID,
		Type:        backupType,
		Timestamp:   time.Now(),
		Shards:      make(map[uint64]*ShardSnapshot),
		Compression: m.config.DefaultCompression,
		Encryption:  m.config.DefaultEncryption,
		Metadata: &BackupMetadata{
			Version:        version.Version,
			StorageBackend: m.config.StorageBackend,
			Tags:           make(map[string]string),
		},
	}

	// Create Raft snapshots for each shard. When raft is nil the engine
	// is backed by a real qrdb node whose shard topology is not
	// accessible via this interface; treat it as zero shards so the
	// backup still persists KV metadata and scheduling state.
	var nodeInfo *interfaces.NodeHostInfo
	if m.raft != nil {
		nodeInfo = m.raft.GetNodeHostInfo(interfaces.NodeHostInfoOption{})
	}
	if nodeInfo == nil {
		nodeInfo = &interfaces.NodeHostInfo{}
	}

	backup.Metadata.NumShards = len(nodeInfo.ShardInfoList)

	for _, shardInfo := range nodeInfo.ShardInfoList {
		snapshot, err := m.createShardSnapshot(ctx, shardInfo.ShardID)
		if err != nil {
			m.metrics.backupErrors.Inc()
			return nil, api.NewShardOperationError("backup.CreateBackup", dberrors.ErrSnapshot, shardInfo.ShardID, "create snapshot", err)
		}
		backup.Shards[shardInfo.ShardID] = snapshot
	}

	// Serialize backup to buffer
	var buf bytes.Buffer

	// Apply compression
	compressor, err := NewCompressor(backup.Compression)
	if err != nil {
		m.metrics.backupErrors.Inc()
		return nil, api.NewOperationFailedError("create compressor", err)
	}

	compressedWriter, err := compressor.Compress(&buf)
	if err != nil {
		m.metrics.backupErrors.Inc()
		return nil, api.NewOperationFailedError("create compressed writer", err)
	}

	// Apply encryption
	encryptor, err := NewEncryptor(backup.Encryption, m.config.EncryptionKey)
	if err != nil {
		m.metrics.backupErrors.Inc()
		return nil, api.NewOperationFailedError("create encryptor", err)
	}

	encryptedWriter, err := encryptor.Encrypt(compressedWriter)
	if err != nil {
		m.metrics.backupErrors.Inc()
		return nil, api.NewOperationFailedError("create encrypted writer", err)
	}

	// Write backup data
	encoder := json.NewEncoder(encryptedWriter)
	if err := encoder.Encode(backup); err != nil {
		m.metrics.backupErrors.Inc()
		return nil, api.NewEncodeError("backup.CreateBackup", "backup", err)
	}

	if err := encryptedWriter.Close(); err != nil {
		m.metrics.backupErrors.Inc()
		return nil, api.NewOperationFailedError("close encrypted writer", err)
	}

	if err := compressedWriter.Close(); err != nil {
		m.metrics.backupErrors.Inc()
		return nil, api.NewOperationFailedError("close compressed writer", err)
	}

	// Compute checksum
	backup.Checksum = ComputeChecksum(buf.Bytes())
	backup.Size = int64(buf.Len())

	// Upload to storage backend using go-objstore
	if err := m.uploadBackup(ctx, backup, &buf); err != nil {
		m.metrics.backupErrors.Inc()
		return nil, api.NewOperationFailedError("upload backup", err)
	}

	// Update metrics
	duration := time.Since(start)
	backup.Metadata.Duration = duration
	m.metrics.backupsTotal.Inc()
	m.metrics.backupDuration.Observe(duration.Seconds())
	m.metrics.backupSize.Observe(float64(backup.Size))
	m.metrics.lastBackupTime.Set(float64(time.Now().Unix()))

	m.logger.WithFields(map[string]interface{}{
		"backup_id": backupID,
		"type":      backupType,
		"size":      backup.Size,
		"duration":  duration,
	}).Info("Backup completed successfully")

	return backup, nil
}

// createShardSnapshot creates a snapshot for a single shard.
func (m *BackupManager) createShardSnapshot(ctx context.Context, shardID uint64) (*ShardSnapshot, error) {
	// Get the current Raft state (index and term) for this shard.
	// When raft is nil (qrdb node path) or the call fails, fall back
	// to zero values — the backup is still valid without Raft metadata.
	var raftIndex, raftTerm uint64
	var err error
	if m.raft != nil {
		raftIndex, raftTerm, err = m.raft.GetShardRaftState(shardID)
	}
	if err != nil {
		// Log warning but continue - we can still create the backup with zero values
		m.logger.WithFields(map[string]interface{}{
			"shard_id": shardID,
			"error":    err.Error(),
		}).Warn("Failed to get Raft state for shard, using zero values")
		raftIndex = 0
		raftTerm = 0
	}

	// Export all data from the shard using storage iterator
	var data bytes.Buffer

	// Create iterator for all keys
	iter := m.storage.NewIterator(&storage.IteratorOptions{
		Reverse: false,
	})
	defer iter.Close()

	keyCount := int64(0)
	for iter.SeekToFirst(); iter.Valid(); iter.Next() {
		key := iter.Key()
		value := iter.Value()

		// Write key-value pair (simple format: length-prefixed)
		if err := writeKeyValue(&data, key, value); err != nil {
			return nil, api.NewOperationFailedError("write key-value", err)
		}

		keyCount++
	}

	if err := iter.Error(); err != nil {
		return nil, api.NewIteratorError("backup.createShardSnapshot", err)
	}

	snapshotData := data.Bytes()

	snapshot := &ShardSnapshot{
		ShardID:  shardID,
		Index:    raftIndex,
		Term:     raftTerm,
		Data:     snapshotData,
		Checksum: ComputeChecksum(snapshotData),
		Size:     int64(len(snapshotData)),
	}

	m.logger.WithFields(map[string]interface{}{
		"shard_id":  shardID,
		"index":     raftIndex,
		"term":      raftTerm,
		"key_count": keyCount,
		"size":      snapshot.Size,
	}).Debug("Created shard snapshot")

	return snapshot, nil
}

// RestoreBackup restores from a backup.
func (m *BackupManager) RestoreBackup(ctx context.Context, backupID string, opts *RestoreOptions) error {
	m.mu.RLock()
	if m.closed {
		m.mu.RUnlock()
		return &ManagerClosedError{Op: "RestoreBackup"}
	}
	m.mu.RUnlock()

	if opts == nil {
		opts = &RestoreOptions{
			VerifyChecksum: true,
		}
	}

	start := time.Now()

	m.logger.WithFields(map[string]interface{}{
		"backup_id": backupID,
	}).Info("Starting restore")

	// Download backup using go-objstore
	reader, err := m.downloadBackup(ctx, backupID)
	if err != nil {
		m.metrics.restoreErrors.Inc()
		return api.NewOperationFailedError("download backup", err)
	}
	defer reader.Close()

	// Read all data
	data, err := io.ReadAll(reader)
	if err != nil {
		m.metrics.restoreErrors.Inc()
		return api.NewOperationFailedError("read backup data", err)
	}

	// Get metadata to know compression and encryption
	metadata, err := m.getBackupMetadata(ctx, backupID)
	if err != nil {
		m.metrics.restoreErrors.Inc()
		return api.NewOperationFailedError("get backup metadata", err)
	}

	// Verify checksum
	if opts.VerifyChecksum {
		if !VerifyChecksum(data, metadata.Checksum) {
			m.metrics.restoreErrors.Inc()
			return api.NewChecksumError("backup.RestoreBackup", "backup")
		}
	}

	// Decrypt data
	encryptor, err := NewEncryptor(metadata.Encryption, m.config.EncryptionKey)
	if err != nil {
		m.metrics.restoreErrors.Inc()
		return api.NewOperationFailedError("create encryptor", err)
	}

	decryptedReader, err := encryptor.Decrypt(bytes.NewReader(data))
	if err != nil {
		m.metrics.restoreErrors.Inc()
		return api.NewOperationFailedError("decrypt backup", err)
	}
	defer decryptedReader.Close()

	// Decompress data
	compressor, err := NewCompressor(metadata.Compression)
	if err != nil {
		m.metrics.restoreErrors.Inc()
		return api.NewOperationFailedError("create compressor", err)
	}

	decompressedReader, err := compressor.Decompress(decryptedReader)
	if err != nil {
		m.metrics.restoreErrors.Inc()
		return api.NewOperationFailedError("decompress backup", err)
	}
	defer decompressedReader.Close()

	// Decode backup
	var backup Backup
	decoder := json.NewDecoder(decompressedReader)
	if err := decoder.Decode(&backup); err != nil {
		m.metrics.restoreErrors.Inc()
		return api.NewDecodeError("backup.RestoreBackup", "backup", err)
	}

	// Restore each shard
	for shardID, snapshot := range backup.Shards {
		// Skip if not in target shards
		if len(opts.TargetShards) > 0 {
			found := false
			for _, target := range opts.TargetShards {
				if target == shardID {
					found = true
					break
				}
			}
			if !found {
				continue
			}
		}

		if err := m.restoreShardSnapshot(ctx, snapshot, opts); err != nil {
			m.metrics.restoreErrors.Inc()
			return api.NewShardOperationError("backup.RestoreBackup", dberrors.ErrSnapshot, shardID, "restore shard", err)
		}
	}

	// Update metrics
	duration := time.Since(start)
	m.metrics.restoresTotal.Inc()
	m.metrics.restoreDuration.Observe(duration.Seconds())

	m.logger.WithFields(map[string]interface{}{
		"backup_id": backupID,
		"duration":  duration,
	}).Info("Restore completed successfully")

	return nil
}

// restoreShardSnapshot restores a single shard snapshot.
func (m *BackupManager) restoreShardSnapshot(ctx context.Context, snapshot *ShardSnapshot, opts *RestoreOptions) error {
	// Verify snapshot checksum
	if opts.VerifyChecksum {
		if !VerifyChecksum(snapshot.Data, snapshot.Checksum) {
			return api.NewChecksumError("backup.restoreShardSnapshot", "shard snapshot")
		}
	}

	// Parse and restore key-value pairs
	reader := bytes.NewReader(snapshot.Data)

	for {
		key, value, err := readKeyValue(reader)
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return api.NewOperationFailedError("read key-value", err)
		}

		// Skip existing keys if requested
		if opts.SkipExisting {
			if _, err := m.storage.Get(key); err == nil {
				continue
			}
		}

		// Restore key-value pair
		if err := m.storage.Set(key, value); err != nil {
			return api.NewOperationFailedError("restore key", err)
		}
	}

	return nil
}

// VerifyBackup verifies the integrity of a backup.
func (m *BackupManager) VerifyBackup(ctx context.Context, backupID string) (*VerificationResult, error) {
	m.mu.RLock()
	if m.closed {
		m.mu.RUnlock()
		return nil, &ManagerClosedError{Op: "VerifyBackup"}
	}
	m.mu.RUnlock()

	m.logger.WithField("backup_id", backupID).Info("Starting backup verification")

	result := &VerificationResult{
		Valid:         true,
		ChecksumValid: true,
		Errors:        make([]string, 0),
	}

	// Step 1: Load backup metadata from storage using go-objstore
	metadata, err := m.getBackupMetadata(ctx, backupID)
	if err != nil {
		result.Valid = false
		result.ChecksumValid = false
		result.Message = "Failed to load backup metadata"
		result.Errors = append(result.Errors, fmt.Sprintf("metadata error: %v", err))
		return result, nil
	}

	// Step 2: Verify backup files exist using go-objstore
	reader, err := m.downloadBackup(ctx, backupID)
	if err != nil {
		result.Valid = false
		result.ChecksumValid = false
		result.Message = "Backup files not found or inaccessible"
		result.Errors = append(result.Errors, fmt.Sprintf("download error: %v", err))
		return result, nil
	}
	defer reader.Close()

	// Read backup data
	data, err := io.ReadAll(reader)
	if err != nil {
		result.Valid = false
		result.ChecksumValid = false
		result.Message = "Failed to read backup data"
		result.Errors = append(result.Errors, fmt.Sprintf("read error: %v", err))
		return result, nil
	}

	// Step 3: Check main backup checksum
	if !VerifyChecksum(data, metadata.Checksum) {
		result.Valid = false
		result.ChecksumValid = false
		result.Message = "Backup checksum verification failed"
		result.Errors = append(result.Errors, "main backup checksum mismatch")
		return result, nil
	}

	// Step 4: Verify compression format is valid
	compressor, err := NewCompressor(metadata.Compression)
	if err != nil {
		result.Valid = false
		result.Message = "Invalid compression format"
		result.Errors = append(result.Errors, fmt.Sprintf("compression error: %v", err))
		return result, nil
	}

	// Step 5: Verify encryption headers if encrypted
	if metadata.Encryption != EncryptionNone {
		encryptor, err := NewEncryptor(metadata.Encryption, m.config.EncryptionKey)
		if err != nil {
			result.Valid = false
			result.Message = "Invalid encryption configuration"
			result.Errors = append(result.Errors, fmt.Sprintf("encryption error: %v", err))
			return result, nil
		}

		// Try to decrypt to verify encryption headers
		decryptedReader, err := encryptor.Decrypt(bytes.NewReader(data))
		if err != nil {
			result.Valid = false
			result.Message = "Encryption verification failed"
			result.Errors = append(result.Errors, fmt.Sprintf("decryption error: %v", err))
			return result, nil
		}
		defer decryptedReader.Close()

		// Decompress the decrypted data
		decompressedReader, err := compressor.Decompress(decryptedReader)
		if err != nil {
			result.Valid = false
			result.Message = "Decompression failed"
			result.Errors = append(result.Errors, fmt.Sprintf("decompression error: %v", err))
			return result, nil
		}
		defer decompressedReader.Close()

		// Try to decode backup structure
		var backup Backup
		decoder := json.NewDecoder(decompressedReader)
		if err := decoder.Decode(&backup); err != nil {
			result.Valid = false
			result.Message = "Failed to decode backup structure"
			result.Errors = append(result.Errors, fmt.Sprintf("decode error: %v", err))
			return result, nil
		}

		// Verify checksums for each shard
		for shardID, snapshot := range backup.Shards {
			if !VerifyChecksum(snapshot.Data, snapshot.Checksum) {
				result.Valid = false
				result.ChecksumValid = false
				result.Errors = append(result.Errors, fmt.Sprintf("shard %d checksum mismatch", shardID))
			}
		}
	} else {
		// No encryption - decompress directly
		decompressedReader, err := compressor.Decompress(bytes.NewReader(data))
		if err != nil {
			result.Valid = false
			result.Message = "Decompression failed"
			result.Errors = append(result.Errors, fmt.Sprintf("decompression error: %v", err))
			return result, nil
		}
		defer decompressedReader.Close()

		// Try to decode backup structure
		var backup Backup
		decoder := json.NewDecoder(decompressedReader)
		if err := decoder.Decode(&backup); err != nil {
			result.Valid = false
			result.Message = "Failed to decode backup structure"
			result.Errors = append(result.Errors, fmt.Sprintf("decode error: %v", err))
			return result, nil
		}

		// Verify checksums for each shard
		for shardID, snapshot := range backup.Shards {
			if !VerifyChecksum(snapshot.Data, snapshot.Checksum) {
				result.Valid = false
				result.ChecksumValid = false
				result.Errors = append(result.Errors, fmt.Sprintf("shard %d checksum mismatch", shardID))
			}
		}
	}

	// Set final message
	if result.Valid {
		result.Message = "Backup verification successful - all checks passed"
	} else if len(result.Errors) > 0 {
		result.Message = fmt.Sprintf("Backup verification failed with %d errors", len(result.Errors))
	}

	m.logger.WithFields(map[string]interface{}{
		"backup_id":      backupID,
		"valid":          result.Valid,
		"checksum_valid": result.ChecksumValid,
		"error_count":    len(result.Errors),
	}).Info("Backup verification completed")

	return result, nil
}

// ListBackups returns a list of available backups.
func (m *BackupManager) ListBackups(ctx context.Context, limit int) (*BackupList, error) {
	m.mu.RLock()
	if m.closed {
		m.mu.RUnlock()
		return nil, &ManagerClosedError{Op: "ListBackups"}
	}
	m.mu.RUnlock()

	return m.listBackups(ctx, limit, "")
}

// DeleteBackup deletes a backup.
func (m *BackupManager) DeleteBackup(ctx context.Context, backupID string) error {
	m.mu.RLock()
	if m.closed {
		m.mu.RUnlock()
		return &ManagerClosedError{Op: "DeleteBackup"}
	}
	m.mu.RUnlock()

	m.logger.WithField("backup_id", backupID).Info("Deleting backup")

	if err := m.deleteBackup(ctx, backupID); err != nil {
		return api.NewOperationFailedError("delete backup", err)
	}

	return nil
}

// ApplyRetentionPolicy applies the configured retention policy.
func (m *BackupManager) ApplyRetentionPolicy(ctx context.Context) error {
	if m.config.Schedule == nil || m.config.Schedule.RetentionPolicy == nil {
		return nil
	}

	m.metrics.retentionExecutions.Inc()
	policy := m.config.Schedule.RetentionPolicy

	m.logger.WithFields(map[string]interface{}{
		"max_backups": policy.MaxBackups,
		"max_age":     policy.MaxAge,
	}).Info("Applying retention policy")

	// List all backups using go-objstore
	backups, err := m.listBackups(ctx, 0, "")
	if err != nil {
		return &RetentionPolicyError{
			Op:      "list",
			Message: "failed to list backups",
			Cause:   err,
		}
	}

	deletedCount := 0

	// Apply max backups limit
	if policy.MaxBackups > 0 && len(backups.Backups) > policy.MaxBackups {
		toDelete := backups.Backups[policy.MaxBackups:]
		for _, backup := range toDelete {
			if err := m.DeleteBackup(ctx, backup.ID); err != nil {
				m.logger.WithError(err).WithField("backup_id", backup.ID).Warn("Failed to delete old backup")
			} else {
				deletedCount++
			}
		}
	}

	// Apply max age limit
	if policy.MaxAge > 0 {
		cutoff := time.Now().Add(-policy.MaxAge)
		for _, backup := range backups.Backups {
			if backup.Timestamp.Before(cutoff) {
				if err := m.DeleteBackup(ctx, backup.ID); err != nil {
					m.logger.WithError(err).WithField("backup_id", backup.ID).Warn("Failed to delete expired backup")
				} else {
					deletedCount++
				}
			}
		}
	}

	m.logger.WithField("deleted_count", deletedCount).Info("Retention policy applied")

	return nil
}

// startScheduledBackups initializes and starts the cron scheduler for backups.
func (m *BackupManager) startScheduledBackups() error {
	if m.config.Schedule == nil || !m.config.Schedule.Enabled {
		return nil
	}

	schedule := m.config.Schedule

	// Validate cron expression
	if schedule.CronExpression == "" {
		return &SchedulerError{
			Op:      "parse",
			Message: "cron expression is empty",
		}
	}

	m.logger.WithFields(map[string]interface{}{
		"cron_expression": schedule.CronExpression,
		"backup_type":     schedule.BackupType,
	}).Info("Starting scheduled backups")

	// Create cron scheduler with seconds support and recovery from panics
	m.cronScheduler = cron.New(
		cron.WithParser(cron.NewParser(
			cron.SecondOptional|cron.Minute|cron.Hour|cron.Dom|cron.Month|cron.Dow|cron.Descriptor,
		)),
		cron.WithChain(
			cron.Recover(cron.DefaultLogger),
		),
	)

	// Add the backup job
	entryID, err := m.cronScheduler.AddFunc(schedule.CronExpression, m.executeScheduledBackup)
	if err != nil {
		return &SchedulerError{
			Op:      "schedule",
			Message: "failed to add backup job",
			Cause:   err,
		}
	}

	m.cronEntryID = entryID
	m.cronScheduler.Start()
	m.cronRunning.Store(true)

	// Log next scheduled run
	entry := m.cronScheduler.Entry(entryID)
	m.logger.WithFields(map[string]interface{}{
		"next_run":        entry.Next,
		"cron_expression": schedule.CronExpression,
	}).Info("Backup scheduler started")

	return nil
}

// runScheduledBackups is kept for backward compatibility but now delegates to startScheduledBackups.
// This method is called from NewBackupManager in a goroutine for existing code paths.
func (m *BackupManager) runScheduledBackups() {
	// This is now a no-op as scheduling is handled by startScheduledBackups()
	// The goroutine call in NewBackupManager has been replaced with direct initialization
	m.logger.Debug("runScheduledBackups called - scheduling handled by startScheduledBackups")
}

// executeScheduledBackup is the job function called by the cron scheduler.
func (m *BackupManager) executeScheduledBackup() {
	m.mu.RLock()
	if m.closed {
		m.mu.RUnlock()
		return
	}
	schedule := m.config.Schedule
	m.mu.RUnlock()

	backupType := schedule.BackupType
	if backupType == "" {
		backupType = BackupTypeFull
	}

	m.logger.WithFields(map[string]interface{}{
		"backup_type":     backupType,
		"cron_expression": schedule.CronExpression,
	}).Info("Executing scheduled backup")

	// Create context with timeout from config
	ctx, cancel := context.WithTimeout(context.Background(), m.config.Timeout)
	defer cancel()

	// Execute the backup
	backup, err := m.CreateBackup(ctx, backupType)
	if err != nil {
		m.metrics.scheduledErrors.Inc()
		m.logger.WithError(err).WithFields(map[string]interface{}{
			"backup_type":     backupType,
			"cron_expression": schedule.CronExpression,
		}).Error("Scheduled backup failed")
		return
	}

	m.metrics.scheduledBackups.Inc()
	m.logger.WithFields(map[string]interface{}{
		"backup_id":   backup.ID,
		"backup_type": backupType,
		"size":        backup.Size,
		"duration":    backup.Metadata.Duration,
	}).Info("Scheduled backup completed successfully")

	// Apply retention policy after successful backup
	if schedule.RetentionPolicy != nil {
		if err := m.ApplyRetentionPolicy(ctx); err != nil {
			m.logger.WithError(err).Warn("Failed to apply retention policy after scheduled backup")
		}
	}
}

// StopScheduledBackups stops the cron scheduler.
func (m *BackupManager) StopScheduledBackups() {
	if m.cronScheduler == nil || !m.cronRunning.Load() {
		return
	}

	m.logger.Info("Stopping scheduled backups")

	// Stop the cron scheduler and wait for running jobs to complete
	ctx := m.cronScheduler.Stop()
	<-ctx.Done()

	m.cronRunning.Store(false)
	m.logger.Info("Scheduled backups stopped")
}

// GetNextScheduledBackup returns the next scheduled backup time.
func (m *BackupManager) GetNextScheduledBackup() (time.Time, bool) {
	if m.cronScheduler == nil || !m.cronRunning.Load() {
		return time.Time{}, false
	}

	entry := m.cronScheduler.Entry(m.cronEntryID)
	if entry.ID == 0 {
		return time.Time{}, false
	}

	return entry.Next, true
}

// IsSchedulerRunning returns true if the backup scheduler is running.
func (m *BackupManager) IsSchedulerRunning() bool {
	return m.cronRunning.Load()
}

// Close closes the backup manager.
func (m *BackupManager) Close() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.closed {
		return nil
	}

	m.closed = true

	// Stop the cron scheduler first
	if m.cronScheduler != nil && m.cronRunning.Load() {
		m.logger.Info("Stopping cron scheduler")
		ctx := m.cronScheduler.Stop()
		<-ctx.Done()
		m.cronRunning.Store(false)
	}

	// Signal shutdown to any waiting goroutines (only if channel was initialized)
	if m.cronStopCh != nil {
		close(m.cronStopCh)
	}

	// Try to close the backend if it implements io.Closer
	if m.backend != nil {
		if closer, ok := m.backend.(io.Closer); ok {
			if err := closer.Close(); err != nil {
				return api.NewOperationFailedError("close storage backend", err)
			}
		}
	}

	// Clean up work directory
	if err := os.RemoveAll(m.config.WorkDir); err != nil {
		m.logger.WithError(err).Warn("Failed to clean up work directory")
	}

	m.logger.Info("Backup manager closed")

	return nil
}

// Helper functions for key-value serialization

func writeKeyValue(w io.Writer, key, value []byte) error {
	// Write key length (4 bytes)
	if err := writeLength(w, len(key)); err != nil {
		return err
	}

	// Write key
	if _, err := w.Write(key); err != nil {
		return err
	}

	// Write value length (4 bytes)
	if err := writeLength(w, len(value)); err != nil {
		return err
	}

	// Write value
	if _, err := w.Write(value); err != nil {
		return err
	}

	return nil
}

// writeLength writes a 4-byte length prefix, refusing a length the prefix
// cannot represent.
func writeLength(w io.Writer, n int) error {
	length, err := conv.SafeIntToUint32(n)
	if err != nil {
		return err
	}
	return writeUint32(w, length)
}

func readKeyValue(r io.Reader) ([]byte, []byte, error) {
	// Read key length
	keyLen, err := readUint32(r)
	if err != nil {
		return nil, nil, err
	}

	// Read key
	key := make([]byte, keyLen)
	if _, err := io.ReadFull(r, key); err != nil {
		return nil, nil, err
	}

	// Read value length
	valueLen, err := readUint32(r)
	if err != nil {
		return nil, nil, err
	}

	// Read value
	value := make([]byte, valueLen)
	if _, err := io.ReadFull(r, value); err != nil {
		return nil, nil, err
	}

	return key, value, nil
}

func writeUint32(w io.Writer, v uint32) error {
	buf := make([]byte, 4)
	buf[0] = byte(v >> 24)
	buf[1] = byte(v >> 16)
	buf[2] = byte(v >> 8)
	buf[3] = byte(v)
	_, err := w.Write(buf)
	return err
}

func readUint32(r io.Reader) (uint32, error) {
	buf := make([]byte, 4)
	if _, err := io.ReadFull(r, buf); err != nil {
		return 0, err
	}
	return uint32(buf[0])<<24 | uint32(buf[1])<<16 | uint32(buf[2])<<8 | uint32(buf[3]), nil
}

// Storage helper methods using go-objstore common.Storage directly

// dataKey returns the storage key for backup data.
func (m *BackupManager) dataKey(backupID string) string {
	return path.Join(m.prefix, backupID, "data")
}

// metadataKey returns the storage key for backup metadata.
func (m *BackupManager) metadataKey(backupID string) string {
	return path.Join(m.prefix, backupID, "metadata.json")
}

// uploadBackup uploads a backup to the storage backend.
func (m *BackupManager) uploadBackup(ctx context.Context, backup *Backup, reader io.Reader) error {
	if backup == nil {
		return api.NewRequiredFieldError("backup")
	}
	if reader == nil {
		return api.NewRequiredFieldError("reader")
	}

	// Upload backup data
	if err := m.backend.PutWithContext(ctx, m.dataKey(backup.ID), reader); err != nil {
		return api.NewOperationFailedError("upload backup data", err)
	}

	// Upload metadata
	metadataBytes, err := json.Marshal(backup)
	if err != nil {
		return api.NewOperationFailedError("marshal backup metadata", err)
	}

	if err := m.backend.PutWithContext(ctx, m.metadataKey(backup.ID), bytes.NewReader(metadataBytes)); err != nil {
		// Try to clean up data if metadata upload fails
		if cleanupErr := m.backend.DeleteWithContext(ctx, m.dataKey(backup.ID)); cleanupErr != nil {
			m.logger.WithError(cleanupErr).Warn("Failed to clean up backup data after metadata upload failure")
		}
		return api.NewOperationFailedError("upload backup metadata", err)
	}

	return nil
}

// downloadBackup downloads a backup from the storage backend.
func (m *BackupManager) downloadBackup(ctx context.Context, backupID string) (io.ReadCloser, error) {
	if backupID == "" {
		return nil, api.NewRequiredFieldError("backup ID")
	}

	reader, err := m.backend.GetWithContext(ctx, m.dataKey(backupID))
	if err != nil {
		return nil, api.NewOperationFailedError("download backup", err)
	}

	return reader, nil
}

// deleteBackup removes a backup from the storage backend.
func (m *BackupManager) deleteBackup(ctx context.Context, backupID string) error {
	if backupID == "" {
		return api.NewRequiredFieldError("backup ID")
	}

	// Delete metadata first, then data
	if err := m.backend.DeleteWithContext(ctx, m.metadataKey(backupID)); err != nil {
		m.logger.WithError(err).Warn("Failed to delete backup metadata")
	}

	if err := m.backend.DeleteWithContext(ctx, m.dataKey(backupID)); err != nil {
		return api.NewOperationFailedError("delete backup data", err)
	}

	return nil
}

// listBackups returns a list of available backups.
func (m *BackupManager) listBackups(ctx context.Context, limit int, nextToken string) (*BackupList, error) {
	// List all metadata files
	prefix := m.prefix
	if prefix != "" && !strings.HasSuffix(prefix, "/") {
		prefix += "/"
	}

	keys, err := m.backend.ListWithContext(ctx, prefix)
	if err != nil {
		return nil, api.NewOperationFailedError("list backups", err)
	}

	// Extract unique backup IDs from metadata files
	backupIDs := make(map[string]bool)
	for _, key := range keys {
		if strings.HasSuffix(key, "/metadata.json") {
			// Extract backup ID from path like prefix/backupID/metadata.json
			parts := strings.Split(strings.TrimPrefix(key, prefix), "/")
			if len(parts) >= 2 {
				backupIDs[parts[0]] = true
			}
		}
	}

	// Convert to sorted slice
	ids := make([]string, 0, len(backupIDs))
	for id := range backupIDs {
		ids = append(ids, id)
	}
	sort.Strings(ids)

	// Apply pagination
	startIdx := 0
	if nextToken != "" {
		for i, id := range ids {
			if id == nextToken {
				startIdx = i + 1
				break
			}
		}
	}

	// Apply limit
	endIdx := len(ids)
	if limit > 0 && startIdx+limit < endIdx {
		endIdx = startIdx + limit
	}

	// Fetch metadata for each backup
	backups := make([]*Backup, 0, endIdx-startIdx)
	for i := startIdx; i < endIdx; i++ {
		backup, err := m.getBackupMetadata(ctx, ids[i])
		if err != nil {
			continue // Skip backups with invalid metadata
		}
		backups = append(backups, backup)
	}

	// Determine next token
	var nextTokenOut string
	if endIdx < len(ids) {
		nextTokenOut = ids[endIdx-1]
	}

	return &BackupList{
		Backups:    backups,
		TotalCount: len(ids),
		NextToken:  nextTokenOut,
	}, nil
}

// getBackupMetadata retrieves metadata for a specific backup.
func (m *BackupManager) getBackupMetadata(ctx context.Context, backupID string) (*Backup, error) {
	if backupID == "" {
		return nil, api.NewRequiredFieldError("backup ID")
	}

	reader, err := m.backend.GetWithContext(ctx, m.metadataKey(backupID))
	if err != nil {
		return nil, api.NewOperationFailedError("get backup metadata", err)
	}
	defer reader.Close()

	var backup Backup
	if err := json.NewDecoder(reader).Decode(&backup); err != nil {
		return nil, api.NewDecodeError("backup.getBackupMetadata", "backup metadata", err)
	}

	return &backup, nil
}
