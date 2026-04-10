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
	"fmt"
	"time"
)

// Policy-specific error codes for catalog operations.
const (
	// ErrCodeReplicationPolicyInvalid indicates an invalid replication policy.
	ErrCodeReplicationPolicyInvalid ErrorCode = "REPLICATION_POLICY_INVALID"

	// ErrCodeArchivePolicyInvalid indicates an invalid archive policy.
	ErrCodeArchivePolicyInvalid ErrorCode = "ARCHIVE_POLICY_INVALID"

	// ErrCodeRetentionPolicyInvalid indicates an invalid retention policy.
	ErrCodeRetentionPolicyInvalid ErrorCode = "RETENTION_POLICY_INVALID"

	// ErrCodePolicyPriorityInvalid indicates an invalid priority value.
	ErrCodePolicyPriorityInvalid ErrorCode = "POLICY_PRIORITY_INVALID"

	// ErrCodeCompressionLevelInvalid indicates an invalid compression level.
	ErrCodeCompressionLevelInvalid ErrorCode = "COMPRESSION_LEVEL_INVALID"

	// ErrCodeTargetStoreInvalid indicates an invalid target store.
	ErrCodeTargetStoreInvalid ErrorCode = "TARGET_STORE_INVALID"

	// ErrCodeReplicaCountInvalid indicates an invalid replica count.
	ErrCodeReplicaCountInvalid ErrorCode = "REPLICA_COUNT_INVALID"

	// ErrCodeRetentionDaysInvalid indicates an invalid retention days value.
	ErrCodeRetentionDaysInvalid ErrorCode = "RETENTION_DAYS_INVALID"

	// ErrCodeMaxVersionsInvalid indicates an invalid max versions value.
	ErrCodeMaxVersionsInvalid ErrorCode = "MAX_VERSIONS_INVALID"

	// ErrCodeArchiveDaysInvalid indicates an invalid archive days value.
	ErrCodeArchiveDaysInvalid ErrorCode = "ARCHIVE_DAYS_INVALID"
)

// ReplicationMode defines how replication is performed.
type ReplicationMode string

const (
	// ReplicationModeSync waits for all replicas before returning success.
	ReplicationModeSync ReplicationMode = "sync"

	// ReplicationModeAsync returns immediately, replicates in background.
	ReplicationModeAsync ReplicationMode = "async"

	// ReplicationModeQuorum waits for majority of replicas.
	ReplicationModeQuorum ReplicationMode = "quorum"
)

// String returns the string representation of the replication mode.
func (m ReplicationMode) String() string {
	return string(m)
}

// IsValid checks if the replication mode is valid.
func (m ReplicationMode) IsValid() bool {
	switch m {
	case ReplicationModeSync, ReplicationModeAsync, ReplicationModeQuorum:
		return true
	default:
		return false
	}
}

// StorageTier represents the storage tier for an object.
type StorageTier string

const (
	// TierHot is for frequently accessed objects.
	TierHot StorageTier = "hot"

	// TierWarm is for occasionally accessed objects.
	TierWarm StorageTier = "warm"

	// TierCold is for rarely accessed objects (archive).
	TierCold StorageTier = "cold"
)

// String returns the string representation of the storage tier.
func (t StorageTier) String() string {
	return string(t)
}

// IsValid checks if the storage tier is valid.
func (t StorageTier) IsValid() bool {
	switch t {
	case TierHot, TierWarm, TierCold:
		return true
	default:
		return false
	}
}

// Policy constants for validation bounds.
const (
	// MinPriority is the minimum priority value for replication.
	MinPriority = 1

	// MaxPriority is the maximum priority value for replication.
	MaxPriority = 10

	// DefaultPriority is the default priority for replication.
	DefaultPriority = 5

	// MinCompressionLevel is the minimum compression level (no compression).
	MinCompressionLevel = 0

	// MaxCompressionLevel is the maximum compression level.
	MaxCompressionLevel = 9

	// DefaultCompressionLevel is the default compression level.
	DefaultCompressionLevel = 0

	// MinReplicaCount is the minimum replica count.
	MinReplicaCount = 0

	// MaxReplicaCount is the maximum replica count.
	MaxReplicaCount = 100

	// DefaultMinReplicas is the default minimum replica count.
	DefaultMinReplicas = 2
)

// ReplicationPolicy defines how objects are replicated across backends.
type ReplicationPolicy struct {
	// Enabled indicates whether replication is active.
	Enabled bool `json:"enabled" yaml:"enabled" mapstructure:"enabled"`

	// MinReplicas is the minimum number of replicas required (including primary).
	MinReplicas int `json:"min_replicas" yaml:"min_replicas" mapstructure:"min_replicas"`

	// TargetStores contains the backend names to replicate to.
	TargetStores []string `json:"target_stores" yaml:"target_stores" mapstructure:"target_stores"`

	// Mode specifies the replication mode (sync, async, quorum).
	Mode ReplicationMode `json:"mode" yaml:"mode" mapstructure:"mode"`

	// Priority determines replication order (1-10, higher = sooner).
	Priority int `json:"priority" yaml:"priority" mapstructure:"priority"`
}

// DefaultReplicationPolicy returns a default replication policy.
func DefaultReplicationPolicy() *ReplicationPolicy {
	return &ReplicationPolicy{
		Enabled:      true,
		MinReplicas:  DefaultMinReplicas,
		TargetStores: nil,
		Mode:         ReplicationModeAsync,
		Priority:     DefaultPriority,
	}
}

// Validate validates the replication policy.
func (p *ReplicationPolicy) Validate() error {
	if p == nil {
		return &CatalogError{
			Code:    ErrCodeReplicationPolicyInvalid,
			Message: "replication policy is nil",
		}
	}

	if !p.Enabled {
		return nil
	}

	if p.MinReplicas < MinReplicaCount {
		return &CatalogError{
			Code:    ErrCodeReplicaCountInvalid,
			Message: fmt.Sprintf("min_replicas must be >= %d, got %d", MinReplicaCount, p.MinReplicas),
		}
	}

	if p.MinReplicas > MaxReplicaCount {
		return &CatalogError{
			Code:    ErrCodeReplicaCountInvalid,
			Message: fmt.Sprintf("min_replicas must be <= %d, got %d", MaxReplicaCount, p.MinReplicas),
		}
	}

	if p.Priority < MinPriority || p.Priority > MaxPriority {
		return &CatalogError{
			Code:    ErrCodePolicyPriorityInvalid,
			Message: fmt.Sprintf("priority must be between %d and %d, got %d", MinPriority, MaxPriority, p.Priority),
		}
	}

	if !p.Mode.IsValid() {
		return &CatalogError{
			Code:    ErrCodeReplicationPolicyInvalid,
			Message: fmt.Sprintf("invalid replication mode: %s (must be sync, async, or quorum)", p.Mode),
		}
	}

	for i, store := range p.TargetStores {
		if store == "" {
			return &CatalogError{
				Code:    ErrCodeTargetStoreInvalid,
				Message: fmt.Sprintf("target_stores[%d] cannot be empty", i),
			}
		}
	}

	if p.MinReplicas > 0 && len(p.TargetStores) > 0 && len(p.TargetStores) < p.MinReplicas {
		return &CatalogError{
			Code:    ErrCodeReplicaCountInvalid,
			Message: fmt.Sprintf("not enough target stores (%d) to satisfy min_replicas (%d)", len(p.TargetStores), p.MinReplicas),
		}
	}

	return nil
}

// Clone creates a deep copy of the replication policy.
func (p *ReplicationPolicy) Clone() *ReplicationPolicy {
	if p == nil {
		return nil
	}

	clone := &ReplicationPolicy{
		Enabled:     p.Enabled,
		MinReplicas: p.MinReplicas,
		Mode:        p.Mode,
		Priority:    p.Priority,
	}

	if p.TargetStores != nil {
		clone.TargetStores = make([]string, len(p.TargetStores))
		copy(clone.TargetStores, p.TargetStores)
	}

	return clone
}

// ArchivePolicy defines when and how objects are archived to cold storage.
type ArchivePolicy struct {
	// Enabled indicates whether archival is active.
	Enabled bool `json:"enabled" yaml:"enabled" mapstructure:"enabled"`

	// ArchiveAfterDays specifies days since last access before archiving.
	ArchiveAfterDays int `json:"archive_after_days" yaml:"archive_after_days" mapstructure:"archive_after_days"`

	// TargetStore is the archive backend name.
	TargetStore string `json:"target_store" yaml:"target_store" mapstructure:"target_store"`

	// DeleteFromPrimary removes the object from primary after archiving.
	DeleteFromPrimary bool `json:"delete_from_primary" yaml:"delete_from_primary" mapstructure:"delete_from_primary"`

	// CompressionLevel specifies compression (0=none, 1-9=level).
	CompressionLevel int `json:"compression_level" yaml:"compression_level" mapstructure:"compression_level"`
}

// DefaultArchivePolicy returns a default archive policy.
func DefaultArchivePolicy() *ArchivePolicy {
	return &ArchivePolicy{
		Enabled:           false,
		ArchiveAfterDays:  90,
		TargetStore:       "",
		DeleteFromPrimary: false,
		CompressionLevel:  DefaultCompressionLevel,
	}
}

// Validate validates the archive policy.
func (p *ArchivePolicy) Validate() error {
	if p == nil {
		return &CatalogError{
			Code:    ErrCodeArchivePolicyInvalid,
			Message: "archive policy is nil",
		}
	}

	if !p.Enabled {
		return nil
	}

	if p.ArchiveAfterDays < 0 {
		return &CatalogError{
			Code:    ErrCodeArchiveDaysInvalid,
			Message: fmt.Sprintf("archive_after_days must be >= 0, got %d", p.ArchiveAfterDays),
		}
	}

	if p.TargetStore == "" {
		return &CatalogError{
			Code:    ErrCodeTargetStoreInvalid,
			Message: "target_store is required when archive is enabled",
		}
	}

	if p.CompressionLevel < MinCompressionLevel || p.CompressionLevel > MaxCompressionLevel {
		return &CatalogError{
			Code:    ErrCodeCompressionLevelInvalid,
			Message: fmt.Sprintf("compression_level must be between %d and %d, got %d", MinCompressionLevel, MaxCompressionLevel, p.CompressionLevel),
		}
	}

	return nil
}

// Clone creates a deep copy of the archive policy.
func (p *ArchivePolicy) Clone() *ArchivePolicy {
	if p == nil {
		return nil
	}

	return &ArchivePolicy{
		Enabled:           p.Enabled,
		ArchiveAfterDays:  p.ArchiveAfterDays,
		TargetStore:       p.TargetStore,
		DeleteFromPrimary: p.DeleteFromPrimary,
		CompressionLevel:  p.CompressionLevel,
	}
}

// ShouldArchive checks if an object should be archived based on last access time.
// Returns true if the object has not been accessed within ArchiveAfterDays.
func (p *ArchivePolicy) ShouldArchive(lastAccessAt int64) bool {
	if p == nil || !p.Enabled {
		return false
	}

	if p.ArchiveAfterDays == 0 {
		return true
	}

	if lastAccessAt == 0 {
		return true
	}

	lastAccess := time.Unix(lastAccessAt, 0)
	threshold := time.Duration(p.ArchiveAfterDays) * 24 * time.Hour
	return time.Since(lastAccess) >= threshold
}

// RetentionPolicy defines how long objects are kept.
type RetentionPolicy struct {
	// Enabled indicates whether retention enforcement is active.
	Enabled bool `json:"enabled" yaml:"enabled" mapstructure:"enabled"`

	// RetentionDays specifies auto-delete after N days (0 = never).
	RetentionDays int `json:"retention_days" yaml:"retention_days" mapstructure:"retention_days"`

	// MaxVersions keeps last N versions (0 = unlimited).
	MaxVersions int `json:"max_versions" yaml:"max_versions" mapstructure:"max_versions"`

	// DeleteAfter is a Unix timestamp for deletion (0 = use RetentionDays).
	DeleteAfter int64 `json:"delete_after" yaml:"delete_after" mapstructure:"delete_after"`
}

// DefaultRetentionPolicy returns a default retention policy.
func DefaultRetentionPolicy() *RetentionPolicy {
	return &RetentionPolicy{
		Enabled:       false,
		RetentionDays: 0,
		MaxVersions:   0,
		DeleteAfter:   0,
	}
}

// Validate validates the retention policy.
func (p *RetentionPolicy) Validate() error {
	if p == nil {
		return &CatalogError{
			Code:    ErrCodeRetentionPolicyInvalid,
			Message: "retention policy is nil",
		}
	}

	if !p.Enabled {
		return nil
	}

	if p.RetentionDays < 0 {
		return &CatalogError{
			Code:    ErrCodeRetentionDaysInvalid,
			Message: fmt.Sprintf("retention_days must be >= 0, got %d", p.RetentionDays),
		}
	}

	if p.MaxVersions < 0 {
		return &CatalogError{
			Code:    ErrCodeMaxVersionsInvalid,
			Message: fmt.Sprintf("max_versions must be >= 0, got %d", p.MaxVersions),
		}
	}

	if p.DeleteAfter < 0 {
		return &CatalogError{
			Code:    ErrCodeRetentionPolicyInvalid,
			Message: fmt.Sprintf("delete_after must be >= 0, got %d", p.DeleteAfter),
		}
	}

	return nil
}

// Clone creates a deep copy of the retention policy.
func (p *RetentionPolicy) Clone() *RetentionPolicy {
	if p == nil {
		return nil
	}

	return &RetentionPolicy{
		Enabled:       p.Enabled,
		RetentionDays: p.RetentionDays,
		MaxVersions:   p.MaxVersions,
		DeleteAfter:   p.DeleteAfter,
	}
}

// ShouldDelete checks if an object should be deleted based on creation time.
// Returns true if the object has exceeded its retention period.
func (p *RetentionPolicy) ShouldDelete(createdAt int64) bool {
	if p == nil || !p.Enabled {
		return false
	}

	if p.DeleteAfter > 0 {
		return time.Now().Unix() >= p.DeleteAfter
	}

	if p.RetentionDays <= 0 {
		return false
	}

	if createdAt == 0 {
		return false
	}

	created := time.Unix(createdAt, 0)
	threshold := time.Duration(p.RetentionDays) * 24 * time.Hour
	return time.Since(created) >= threshold
}

// IsExpired checks if an object has exceeded its retention period.
// This is an alias for ShouldDelete for semantic clarity.
func (p *RetentionPolicy) IsExpired(createdAt int64) bool {
	return p.ShouldDelete(createdAt)
}
