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

// Package backup provides backup and restore functionality for QuicBaaS.
// It supports full and incremental backups with compression and encryption.
package backup

import (
	"time"
)

// BackupType represents the type of backup.
type BackupType string

const (
	// BackupTypeFull represents a full backup of all data.
	BackupTypeFull BackupType = "full"

	// BackupTypeIncremental represents an incremental backup since the last backup.
	BackupTypeIncremental BackupType = "incremental"
)

// CompressionType represents the compression algorithm.
type CompressionType string

const (
	// CompressionNone means no compression.
	CompressionNone CompressionType = "none"

	// CompressionGzip uses gzip compression.
	CompressionGzip CompressionType = "gzip"

	// CompressionZstd uses Zstandard compression (recommended).
	CompressionZstd CompressionType = "zstd"
)

// EncryptionType represents the encryption algorithm.
type EncryptionType string

const (
	// EncryptionNone means no encryption.
	EncryptionNone EncryptionType = "none"

	// EncryptionAES256GCM uses AES-256-GCM encryption (FIPS 140-2 approved).
	EncryptionAES256GCM EncryptionType = "aes-256-gcm"
)

// Backup represents a complete backup with metadata.
type Backup struct {
	// ID is the unique identifier for this backup.
	ID string `json:"id"`

	// Type indicates whether this is a full or incremental backup.
	Type BackupType `json:"type"`

	// Timestamp when the backup was created.
	Timestamp time.Time `json:"timestamp"`

	// Shards contains the Raft snapshot for each shard.
	Shards map[uint64]*ShardSnapshot `json:"shards"`

	// Metadata contains additional backup information.
	Metadata *BackupMetadata `json:"metadata"`

	// Checksum for integrity validation (SHA-256).
	Checksum string `json:"checksum"`

	// Size in bytes.
	Size int64 `json:"size"`

	// Compression algorithm used.
	Compression CompressionType `json:"compression"`

	// Encryption algorithm used.
	Encryption EncryptionType `json:"encryption"`

	// BaseBackupID is the ID of the previous backup for incremental backups.
	BaseBackupID string `json:"base_backup_id,omitempty"`
}

// ShardSnapshot represents a Raft snapshot for a single shard.
type ShardSnapshot struct {
	// ShardID is the shard identifier.
	ShardID uint64 `json:"shard_id"`

	// Index is the Raft log index at the time of snapshot.
	Index uint64 `json:"index"`

	// Term is the Raft term at the time of snapshot.
	Term uint64 `json:"term"`

	// Data contains the serialized state machine data.
	Data []byte `json:"data"`

	// Checksum for integrity validation.
	Checksum string `json:"checksum"`

	// Size in bytes.
	Size int64 `json:"size"`
}

// BackupMetadata contains additional backup information.
type BackupMetadata struct {
	// Version of QuicBaaS that created the backup.
	Version string `json:"version"`

	// NodeID of the node that created the backup.
	NodeID uint64 `json:"node_id"`

	// ClusterID identifies the cluster.
	ClusterID string `json:"cluster_id"`

	// NumKeys is the total number of keys in the backup.
	NumKeys int64 `json:"num_keys"`

	// NumShards is the number of shards backed up.
	NumShards int `json:"num_shards"`

	// Duration of the backup operation.
	Duration time.Duration `json:"duration"`

	// StorageBackend identifies where the backup is stored.
	StorageBackend string `json:"storage_backend"`

	// Tags for categorization and filtering.
	Tags map[string]string `json:"tags,omitempty"`
}

// BackupList represents a list of backups with pagination.
type BackupList struct {
	// Backups contains the backup metadata.
	Backups []*Backup `json:"backups"`

	// TotalCount is the total number of backups available.
	TotalCount int `json:"total_count"`

	// NextToken for pagination (if applicable).
	NextToken string `json:"next_token,omitempty"`
}

// RestoreOptions contains options for restore operations.
type RestoreOptions struct {
	// VerifyChecksum enables checksum verification during restore.
	VerifyChecksum bool

	// TargetShards specifies which shards to restore (nil = all).
	TargetShards []uint64

	// SkipExisting skips restoring data for keys that already exist.
	SkipExisting bool
}

// VerificationResult contains the results of backup verification.
type VerificationResult struct {
	// Valid indicates whether the backup is valid overall.
	Valid bool

	// ChecksumValid indicates whether checksums are valid.
	ChecksumValid bool

	// Message provides a summary of the verification result.
	Message string

	// Errors contains any verification errors encountered.
	Errors []string
}

// StorageBackend is now deprecated. Use common.Storage from go-objstore directly.
// This type alias is kept for backward compatibility during migration.
// type StorageBackend = common.Storage

// RetentionPolicy defines backup retention rules.
type RetentionPolicy struct {
	// MaxBackups is the maximum number of backups to keep (0 = unlimited).
	MaxBackups int

	// MaxAge is the maximum age of backups to keep (0 = unlimited).
	MaxAge time.Duration

	// KeepDaily specifies how many daily backups to keep.
	KeepDaily int

	// KeepWeekly specifies how many weekly backups to keep.
	KeepWeekly int

	// KeepMonthly specifies how many monthly backups to keep.
	KeepMonthly int
}

// ScheduleConfig defines scheduled backup configuration.
type ScheduleConfig struct {
	// Enabled controls whether scheduled backups are active.
	Enabled bool

	// CronExpression defines the backup schedule (e.g., "0 2 * * *" for daily at 2am).
	CronExpression string

	// BackupType for scheduled backups.
	BackupType BackupType

	// RetentionPolicy for automated cleanup.
	RetentionPolicy *RetentionPolicy
}
