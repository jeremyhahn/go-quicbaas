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

package config

import (
	"errors"
	"fmt"
	"strings"
)

// Compression algorithm constants.
const (
	CompressionNone = "none"
	CompressionZstd = "zstd"
	CompressionGzip = "gzip"
	CompressionLZ4  = "lz4"
)

// Replication mode constants.
const (
	ReplicationModeSync   = "sync"
	ReplicationModeAsync  = "async"
	ReplicationModeQuorum = "quorum"
)

// Default values for object storage configuration.
const (
	DefaultValueThreshold      = int64(1048576) // 1MB
	DefaultCompression         = CompressionZstd
	DefaultEncryptionAlgo      = "AES-256-GCM"
	DefaultReplicationMode     = ReplicationModeAsync
	DefaultReplicationPriority = 5
	DefaultArchiveAfterDays    = 90
	DefaultCompressionLevel    = 6
	DefaultMinReplicas         = 2
	DefaultCacheMaxSize        = int64(10737418240) // 10GB
)

// ObjectStoreDefaults represents cluster-wide default settings for object storage.
type ObjectStoreDefaults struct {
	// PrimaryBackend is the default backend for storing objects.
	PrimaryBackend string `mapstructure:"primary_backend"`

	// RequiredBackends lists backends that must be available for operations.
	RequiredBackends []string `mapstructure:"required_backends"`

	// ValueThreshold is the size threshold for offloading values to object storage.
	// Objects larger than this threshold will be stored in object storage.
	ValueThreshold int64 `mapstructure:"value_threshold"`

	// Compression specifies the compression algorithm: none, zstd, gzip, lz4.
	Compression string `mapstructure:"compression"`

	// Encryption contains encryption settings for stored objects.
	Encryption ObjectStoreEncryption `mapstructure:"encryption"`

	// Replication contains replication settings for stored objects.
	Replication ObjectStoreReplication `mapstructure:"replication"`

	// Archive contains archive policy settings.
	Archive ObjectStoreArchive `mapstructure:"archive"`

	// Retention contains retention policy settings.
	Retention ObjectStoreRetention `mapstructure:"retention"`
}

// ObjectStoreEncryption contains encryption configuration for object storage.
type ObjectStoreEncryption struct {
	// Enabled indicates if encryption is available for this context.
	Enabled bool `mapstructure:"enabled"`

	// Enforce indicates if encryption should be applied when not explicitly specified.
	// When true, objects without explicit encryption settings will use this config.
	Enforce bool `mapstructure:"enforce"`

	// DefaultKey is the KMS key path for encryption (e.g., "dek:cluster/objects").
	DefaultKey string `mapstructure:"default_key"`

	// Algorithm is the encryption algorithm (e.g., "AES-256-GCM").
	Algorithm string `mapstructure:"algorithm"`
}

// ObjectStoreReplication contains replication configuration for object storage.
type ObjectStoreReplication struct {
	// Enabled indicates if replication is active.
	Enabled bool `mapstructure:"enabled"`

	// MinReplicas is the minimum number of replicas required.
	MinReplicas int `mapstructure:"min_replicas"`

	// TargetStores lists the backend names to replicate to.
	TargetStores []string `mapstructure:"target_stores"`

	// Mode is the replication mode: sync, async, or quorum.
	Mode string `mapstructure:"mode"`

	// Priority is the replication priority (1-10). Higher values replicate sooner.
	Priority int `mapstructure:"priority"`
}

// ObjectStoreArchive contains archive policy configuration.
type ObjectStoreArchive struct {
	// Enabled indicates if archival is active.
	Enabled bool `mapstructure:"enabled"`

	// ArchiveAfterDays is the number of days after last access before archiving.
	ArchiveAfterDays int `mapstructure:"archive_after_days"`

	// TargetStore is the backend name to archive to.
	TargetStore string `mapstructure:"target_store"`

	// DeleteFromPrimary indicates if objects should be deleted from primary after archiving.
	DeleteFromPrimary bool `mapstructure:"delete_from_primary"`

	// CompressionLevel is the compression level (0-9) for archived objects.
	CompressionLevel int `mapstructure:"compression_level"`
}

// ObjectStoreRetention contains retention policy configuration.
type ObjectStoreRetention struct {
	// Enabled indicates if retention policy is active.
	Enabled bool `mapstructure:"enabled"`

	// RetentionDays is the number of days to retain objects. 0 means infinite.
	RetentionDays int `mapstructure:"retention_days"`

	// MaxVersions is the maximum number of versions to keep. 0 means unlimited.
	MaxVersions int `mapstructure:"max_versions"`
}

// ObjectStoreQuotas contains quota settings for a tenant.
type ObjectStoreQuotas struct {
	// MaxStorageBytes is the maximum total storage in bytes. 0 means unlimited.
	MaxStorageBytes int64 `mapstructure:"max_storage_bytes"`

	// MaxObjectSize is the maximum size of a single object in bytes. 0 means unlimited.
	MaxObjectSize int64 `mapstructure:"max_object_size"`

	// MaxObjectCount is the maximum number of objects. 0 means unlimited.
	MaxObjectCount int64 `mapstructure:"max_object_count"`
}

// ObjectStoreShardConfig contains shard-specific configuration overrides.
type ObjectStoreShardConfig struct {
	// PrimaryBackend overrides the primary backend for this shard.
	PrimaryBackend string `mapstructure:"primary_backend"`

	// Replication overrides replication settings for this shard.
	Replication *ObjectStoreReplication `mapstructure:"replication"`
}

// ObjectStoreTenantConfig contains tenant-specific configuration.
type ObjectStoreTenantConfig struct {
	// PrimaryBackend overrides the default primary backend for this tenant.
	PrimaryBackend string `mapstructure:"primary_backend"`

	// AllowedBackends lists which backends this tenant may use.
	AllowedBackends []string `mapstructure:"allowed_backends"`

	// Encryption overrides encryption settings for this tenant.
	Encryption *ObjectStoreEncryption `mapstructure:"encryption"`

	// Replication overrides replication settings for this tenant.
	Replication *ObjectStoreReplication `mapstructure:"replication"`

	// Archive overrides archive settings for this tenant.
	Archive *ObjectStoreArchive `mapstructure:"archive"`

	// Retention overrides retention settings for this tenant.
	Retention *ObjectStoreRetention `mapstructure:"retention"`

	// Quotas contains quota settings for this tenant.
	Quotas ObjectStoreQuotas `mapstructure:"quotas"`

	// ShardConfigs contains per-shard configuration overrides.
	ShardConfigs map[uint64]*ObjectStoreShardConfig `mapstructure:"shard_configs"`
}

// NodeObjectStorageConfig contains node-specific object storage configuration.
type NodeObjectStorageConfig struct {
	// InternalBackend is the backend for Raft snapshots and internal data.
	InternalBackend string `mapstructure:"internal_backend"`

	// CacheBackend is the backend used for local caching.
	CacheBackend string `mapstructure:"cache_backend"`

	// CacheMaxSize is the maximum cache size in bytes.
	CacheMaxSize int64 `mapstructure:"cache_max_size"`

	// Encryption contains node-specific encryption settings.
	Encryption *ObjectStoreEncryption `mapstructure:"encryption"`

	// BackendOverrides contains node-specific backend configuration overrides.
	BackendOverrides map[string]*BackendOverride `mapstructure:"backend_overrides"`

	// EnabledBackends limits which backends this node can access.
	EnabledBackends []string `mapstructure:"enabled_backends"`
}

// BackendOverride contains node-specific overrides for a backend.
type BackendOverride struct {
	// Config contains backend-specific configuration overrides.
	Config map[string]string `mapstructure:"config"`
}

// ObjectStoreFullConfig extends ObjectStorageConfig with defaults and tenants.
type ObjectStoreFullConfig struct {
	// Backends is a map of backend name to backend configuration.
	Backends map[string]*StorageBackendConfig `mapstructure:"backends"`

	// Defaults contains cluster-wide default settings.
	Defaults ObjectStoreDefaults `mapstructure:"defaults"`

	// Tenants contains per-tenant configuration.
	Tenants map[string]*ObjectStoreTenantConfig `mapstructure:"tenants"`
}

// DefaultObjectStoreDefaults returns sensible default settings.
func DefaultObjectStoreDefaults() *ObjectStoreDefaults {
	return &ObjectStoreDefaults{
		ValueThreshold: DefaultValueThreshold,
		Compression:    DefaultCompression,
		Encryption: ObjectStoreEncryption{
			Enabled:   true,
			Enforce:   true,
			Algorithm: DefaultEncryptionAlgo,
		},
		Replication: ObjectStoreReplication{
			Enabled:     true,
			MinReplicas: DefaultMinReplicas,
			Mode:        DefaultReplicationMode,
			Priority:    DefaultReplicationPriority,
		},
		Archive: ObjectStoreArchive{
			Enabled:          false,
			ArchiveAfterDays: DefaultArchiveAfterDays,
			CompressionLevel: DefaultCompressionLevel,
		},
		Retention: ObjectStoreRetention{
			Enabled: false,
		},
	}
}

// Clone creates a deep copy of ObjectStoreDefaults.
func (d *ObjectStoreDefaults) Clone() *ObjectStoreDefaults {
	if d == nil {
		return nil
	}

	clone := &ObjectStoreDefaults{
		PrimaryBackend: d.PrimaryBackend,
		ValueThreshold: d.ValueThreshold,
		Compression:    d.Compression,
		Encryption:     *d.Encryption.Clone(),
		Replication:    *d.Replication.Clone(),
		Archive:        *d.Archive.Clone(),
		Retention:      *d.Retention.Clone(),
	}

	if d.RequiredBackends != nil {
		clone.RequiredBackends = make([]string, len(d.RequiredBackends))
		copy(clone.RequiredBackends, d.RequiredBackends)
	}

	return clone
}

// Merge merges another ObjectStoreDefaults into this one.
// Non-zero values from other override values in this.
func (d *ObjectStoreDefaults) Merge(other *ObjectStoreDefaults) {
	if other == nil {
		return
	}

	if other.PrimaryBackend != "" {
		d.PrimaryBackend = other.PrimaryBackend
	}
	if other.RequiredBackends != nil {
		d.RequiredBackends = make([]string, len(other.RequiredBackends))
		copy(d.RequiredBackends, other.RequiredBackends)
	}
	if other.ValueThreshold != 0 {
		d.ValueThreshold = other.ValueThreshold
	}
	if other.Compression != "" {
		d.Compression = other.Compression
	}

	d.Encryption.Merge(&other.Encryption)
	d.Replication.Merge(&other.Replication)
	d.Archive.Merge(&other.Archive)
	d.Retention.Merge(&other.Retention)
}

// Validate validates the ObjectStoreDefaults configuration.
func (d *ObjectStoreDefaults) Validate() error {
	var errs ValidationErrors

	// Validate compression algorithm
	if d.Compression != "" {
		validCompression := map[string]bool{
			CompressionNone: true,
			CompressionZstd: true,
			CompressionGzip: true,
			CompressionLZ4:  true,
		}
		if !validCompression[d.Compression] {
			errs = append(errs, ValidationError{
				Field:   "defaults.compression",
				Message: fmt.Sprintf("invalid compression algorithm: %s (valid: none, zstd, gzip, lz4)", d.Compression),
			})
		}
	}

	if err := d.Encryption.Validate("defaults.encryption"); err != nil {
		var ve ValidationErrors
		if errors.As(err, &ve) {
			errs = append(errs, ve...)
		}
	}

	if err := d.Replication.Validate("defaults.replication"); err != nil {
		var ve ValidationErrors
		if errors.As(err, &ve) {
			errs = append(errs, ve...)
		}
	}

	if err := d.Archive.Validate("defaults.archive"); err != nil {
		var ve ValidationErrors
		if errors.As(err, &ve) {
			errs = append(errs, ve...)
		}
	}

	if err := d.Retention.Validate("defaults.retention"); err != nil {
		var ve ValidationErrors
		if errors.As(err, &ve) {
			errs = append(errs, ve...)
		}
	}

	if len(errs) > 0 {
		return errs
	}
	return nil
}

// Clone creates a deep copy of ObjectStoreEncryption.
func (e *ObjectStoreEncryption) Clone() *ObjectStoreEncryption {
	if e == nil {
		return nil
	}
	return &ObjectStoreEncryption{
		Enabled:    e.Enabled,
		Enforce:    e.Enforce,
		DefaultKey: e.DefaultKey,
		Algorithm:  e.Algorithm,
	}
}

// Merge merges another ObjectStoreEncryption into this one.
func (e *ObjectStoreEncryption) Merge(other *ObjectStoreEncryption) {
	if other == nil {
		return
	}

	// Booleans are always merged if set (even false is meaningful)
	// For encryption, we only override if the other has explicit values
	if other.Enabled {
		e.Enabled = other.Enabled
	}
	if other.Enforce {
		e.Enforce = other.Enforce
	}
	if other.DefaultKey != "" {
		e.DefaultKey = other.DefaultKey
	}
	if other.Algorithm != "" {
		e.Algorithm = other.Algorithm
	}
}

// Validate validates the ObjectStoreEncryption configuration.
func (e *ObjectStoreEncryption) Validate(prefix string) error {
	// No strict validation required for encryption config
	// Algorithm validation would be done by the encryption provider
	return nil
}

// Clone creates a deep copy of ObjectStoreReplication.
func (r *ObjectStoreReplication) Clone() *ObjectStoreReplication {
	if r == nil {
		return nil
	}

	clone := &ObjectStoreReplication{
		Enabled:     r.Enabled,
		MinReplicas: r.MinReplicas,
		Mode:        r.Mode,
		Priority:    r.Priority,
	}

	if r.TargetStores != nil {
		clone.TargetStores = make([]string, len(r.TargetStores))
		copy(clone.TargetStores, r.TargetStores)
	}

	return clone
}

// Merge merges another ObjectStoreReplication into this one.
func (r *ObjectStoreReplication) Merge(other *ObjectStoreReplication) {
	if other == nil {
		return
	}

	if other.Enabled {
		r.Enabled = other.Enabled
	}
	if other.MinReplicas != 0 {
		r.MinReplicas = other.MinReplicas
	}
	if other.TargetStores != nil {
		r.TargetStores = make([]string, len(other.TargetStores))
		copy(r.TargetStores, other.TargetStores)
	}
	if other.Mode != "" {
		r.Mode = other.Mode
	}
	if other.Priority != 0 {
		r.Priority = other.Priority
	}
}

// Validate validates the ObjectStoreReplication configuration.
func (r *ObjectStoreReplication) Validate(prefix string) error {
	var errs ValidationErrors

	if r.Mode != "" {
		validModes := map[string]bool{
			ReplicationModeSync:   true,
			ReplicationModeAsync:  true,
			ReplicationModeQuorum: true,
		}
		if !validModes[r.Mode] {
			errs = append(errs, ValidationError{
				Field:   prefix + ".mode",
				Message: fmt.Sprintf("invalid replication mode: %s (valid: sync, async, quorum)", r.Mode),
			})
		}
	}

	if r.Priority != 0 && (r.Priority < 1 || r.Priority > 10) {
		errs = append(errs, ValidationError{
			Field:   prefix + ".priority",
			Message: fmt.Sprintf("replication priority must be between 1 and 10, got %d", r.Priority),
		})
	}

	if r.MinReplicas < 0 {
		errs = append(errs, ValidationError{
			Field:   prefix + ".min_replicas",
			Message: fmt.Sprintf("min_replicas cannot be negative, got %d", r.MinReplicas),
		})
	}

	if len(errs) > 0 {
		return errs
	}
	return nil
}

// Clone creates a deep copy of ObjectStoreArchive.
func (a *ObjectStoreArchive) Clone() *ObjectStoreArchive {
	if a == nil {
		return nil
	}
	return &ObjectStoreArchive{
		Enabled:           a.Enabled,
		ArchiveAfterDays:  a.ArchiveAfterDays,
		TargetStore:       a.TargetStore,
		DeleteFromPrimary: a.DeleteFromPrimary,
		CompressionLevel:  a.CompressionLevel,
	}
}

// Merge merges another ObjectStoreArchive into this one.
func (a *ObjectStoreArchive) Merge(other *ObjectStoreArchive) {
	if other == nil {
		return
	}

	if other.Enabled {
		a.Enabled = other.Enabled
	}
	if other.ArchiveAfterDays != 0 {
		a.ArchiveAfterDays = other.ArchiveAfterDays
	}
	if other.TargetStore != "" {
		a.TargetStore = other.TargetStore
	}
	if other.DeleteFromPrimary {
		a.DeleteFromPrimary = other.DeleteFromPrimary
	}
	if other.CompressionLevel != 0 {
		a.CompressionLevel = other.CompressionLevel
	}
}

// Validate validates the ObjectStoreArchive configuration.
func (a *ObjectStoreArchive) Validate(prefix string) error {
	var errs ValidationErrors

	if a.CompressionLevel < 0 || a.CompressionLevel > 9 {
		errs = append(errs, ValidationError{
			Field:   prefix + ".compression_level",
			Message: fmt.Sprintf("compression level must be between 0 and 9, got %d", a.CompressionLevel),
		})
	}

	if a.ArchiveAfterDays < 0 {
		errs = append(errs, ValidationError{
			Field:   prefix + ".archive_after_days",
			Message: fmt.Sprintf("archive_after_days cannot be negative, got %d", a.ArchiveAfterDays),
		})
	}

	if len(errs) > 0 {
		return errs
	}
	return nil
}

// Clone creates a deep copy of ObjectStoreRetention.
func (r *ObjectStoreRetention) Clone() *ObjectStoreRetention {
	if r == nil {
		return nil
	}
	return &ObjectStoreRetention{
		Enabled:       r.Enabled,
		RetentionDays: r.RetentionDays,
		MaxVersions:   r.MaxVersions,
	}
}

// Merge merges another ObjectStoreRetention into this one.
func (r *ObjectStoreRetention) Merge(other *ObjectStoreRetention) {
	if other == nil {
		return
	}

	if other.Enabled {
		r.Enabled = other.Enabled
	}
	if other.RetentionDays != 0 {
		r.RetentionDays = other.RetentionDays
	}
	if other.MaxVersions != 0 {
		r.MaxVersions = other.MaxVersions
	}
}

// Validate validates the ObjectStoreRetention configuration.
func (r *ObjectStoreRetention) Validate(prefix string) error {
	var errs ValidationErrors

	if r.RetentionDays < 0 {
		errs = append(errs, ValidationError{
			Field:   prefix + ".retention_days",
			Message: fmt.Sprintf("retention_days cannot be negative, got %d", r.RetentionDays),
		})
	}

	if r.MaxVersions < 0 {
		errs = append(errs, ValidationError{
			Field:   prefix + ".max_versions",
			Message: fmt.Sprintf("max_versions cannot be negative, got %d", r.MaxVersions),
		})
	}

	if len(errs) > 0 {
		return errs
	}
	return nil
}

// Clone creates a deep copy of ObjectStoreQuotas.
func (q *ObjectStoreQuotas) Clone() *ObjectStoreQuotas {
	if q == nil {
		return nil
	}
	return &ObjectStoreQuotas{
		MaxStorageBytes: q.MaxStorageBytes,
		MaxObjectSize:   q.MaxObjectSize,
		MaxObjectCount:  q.MaxObjectCount,
	}
}

// Merge merges another ObjectStoreQuotas into this one.
func (q *ObjectStoreQuotas) Merge(other *ObjectStoreQuotas) {
	if other == nil {
		return
	}

	if other.MaxStorageBytes != 0 {
		q.MaxStorageBytes = other.MaxStorageBytes
	}
	if other.MaxObjectSize != 0 {
		q.MaxObjectSize = other.MaxObjectSize
	}
	if other.MaxObjectCount != 0 {
		q.MaxObjectCount = other.MaxObjectCount
	}
}

// Validate validates the ObjectStoreQuotas configuration.
func (q *ObjectStoreQuotas) Validate(prefix string) error {
	var errs ValidationErrors

	if q.MaxStorageBytes < 0 {
		errs = append(errs, ValidationError{
			Field:   prefix + ".max_storage_bytes",
			Message: fmt.Sprintf("max_storage_bytes cannot be negative, got %d", q.MaxStorageBytes),
		})
	}

	if q.MaxObjectSize < 0 {
		errs = append(errs, ValidationError{
			Field:   prefix + ".max_object_size",
			Message: fmt.Sprintf("max_object_size cannot be negative, got %d", q.MaxObjectSize),
		})
	}

	if q.MaxObjectCount < 0 {
		errs = append(errs, ValidationError{
			Field:   prefix + ".max_object_count",
			Message: fmt.Sprintf("max_object_count cannot be negative, got %d", q.MaxObjectCount),
		})
	}

	if len(errs) > 0 {
		return errs
	}
	return nil
}

// Clone creates a deep copy of ObjectStoreShardConfig.
func (s *ObjectStoreShardConfig) Clone() *ObjectStoreShardConfig {
	if s == nil {
		return nil
	}

	clone := &ObjectStoreShardConfig{
		PrimaryBackend: s.PrimaryBackend,
	}

	if s.Replication != nil {
		clone.Replication = s.Replication.Clone()
	}

	return clone
}

// Merge merges another ObjectStoreShardConfig into this one.
func (s *ObjectStoreShardConfig) Merge(other *ObjectStoreShardConfig) {
	if other == nil {
		return
	}

	if other.PrimaryBackend != "" {
		s.PrimaryBackend = other.PrimaryBackend
	}

	if other.Replication != nil {
		if s.Replication == nil {
			s.Replication = &ObjectStoreReplication{}
		}
		s.Replication.Merge(other.Replication)
	}
}

// Validate validates the ObjectStoreShardConfig configuration.
func (s *ObjectStoreShardConfig) Validate(prefix string) error {
	if s.Replication != nil {
		return s.Replication.Validate(prefix + ".replication")
	}
	return nil
}

// Clone creates a deep copy of ObjectStoreTenantConfig.
func (t *ObjectStoreTenantConfig) Clone() *ObjectStoreTenantConfig {
	if t == nil {
		return nil
	}

	clone := &ObjectStoreTenantConfig{
		PrimaryBackend: t.PrimaryBackend,
		Quotas:         *t.Quotas.Clone(),
	}

	if t.AllowedBackends != nil {
		clone.AllowedBackends = make([]string, len(t.AllowedBackends))
		copy(clone.AllowedBackends, t.AllowedBackends)
	}

	if t.Encryption != nil {
		clone.Encryption = t.Encryption.Clone()
	}

	if t.Replication != nil {
		clone.Replication = t.Replication.Clone()
	}

	if t.Archive != nil {
		clone.Archive = t.Archive.Clone()
	}

	if t.Retention != nil {
		clone.Retention = t.Retention.Clone()
	}

	if t.ShardConfigs != nil {
		clone.ShardConfigs = make(map[uint64]*ObjectStoreShardConfig, len(t.ShardConfigs))
		for k, v := range t.ShardConfigs {
			clone.ShardConfigs[k] = v.Clone()
		}
	}

	return clone
}

// Merge merges another ObjectStoreTenantConfig into this one.
func (t *ObjectStoreTenantConfig) Merge(other *ObjectStoreTenantConfig) {
	if other == nil {
		return
	}

	if other.PrimaryBackend != "" {
		t.PrimaryBackend = other.PrimaryBackend
	}

	if other.AllowedBackends != nil {
		t.AllowedBackends = make([]string, len(other.AllowedBackends))
		copy(t.AllowedBackends, other.AllowedBackends)
	}

	if other.Encryption != nil {
		if t.Encryption == nil {
			t.Encryption = &ObjectStoreEncryption{}
		}
		t.Encryption.Merge(other.Encryption)
	}

	if other.Replication != nil {
		if t.Replication == nil {
			t.Replication = &ObjectStoreReplication{}
		}
		t.Replication.Merge(other.Replication)
	}

	if other.Archive != nil {
		if t.Archive == nil {
			t.Archive = &ObjectStoreArchive{}
		}
		t.Archive.Merge(other.Archive)
	}

	if other.Retention != nil {
		if t.Retention == nil {
			t.Retention = &ObjectStoreRetention{}
		}
		t.Retention.Merge(other.Retention)
	}

	t.Quotas.Merge(&other.Quotas)

	if other.ShardConfigs != nil {
		if t.ShardConfigs == nil {
			t.ShardConfigs = make(map[uint64]*ObjectStoreShardConfig)
		}
		for k, v := range other.ShardConfigs {
			if existing, ok := t.ShardConfigs[k]; ok {
				existing.Merge(v)
			} else {
				t.ShardConfigs[k] = v.Clone()
			}
		}
	}
}

// Validate validates the ObjectStoreTenantConfig configuration.
func (t *ObjectStoreTenantConfig) Validate(prefix string) error {
	var errs ValidationErrors

	// Validate primary backend is in allowed backends if both are set
	if t.PrimaryBackend != "" && len(t.AllowedBackends) > 0 {
		found := false
		for _, backend := range t.AllowedBackends {
			if backend == t.PrimaryBackend {
				found = true
				break
			}
		}
		if !found {
			errs = append(errs, ValidationError{
				Field:   prefix + ".primary_backend",
				Message: fmt.Sprintf("primary backend '%s' must be in allowed_backends", t.PrimaryBackend),
			})
		}
	}

	if t.Encryption != nil {
		if err := t.Encryption.Validate(prefix + ".encryption"); err != nil {
			var ve ValidationErrors
			if errors.As(err, &ve) {
				errs = append(errs, ve...)
			}
		}
	}

	if t.Replication != nil {
		if err := t.Replication.Validate(prefix + ".replication"); err != nil {
			var ve ValidationErrors
			if errors.As(err, &ve) {
				errs = append(errs, ve...)
			}
		}
	}

	if t.Archive != nil {
		if err := t.Archive.Validate(prefix + ".archive"); err != nil {
			var ve ValidationErrors
			if errors.As(err, &ve) {
				errs = append(errs, ve...)
			}
		}
	}

	if t.Retention != nil {
		if err := t.Retention.Validate(prefix + ".retention"); err != nil {
			var ve ValidationErrors
			if errors.As(err, &ve) {
				errs = append(errs, ve...)
			}
		}
	}

	if err := t.Quotas.Validate(prefix + ".quotas"); err != nil {
		var ve ValidationErrors
		if errors.As(err, &ve) {
			errs = append(errs, ve...)
		}
	}

	for shardID, shardCfg := range t.ShardConfigs {
		if err := shardCfg.Validate(fmt.Sprintf("%s.shard_configs.%d", prefix, shardID)); err != nil {
			var ve ValidationErrors
			if errors.As(err, &ve) {
				errs = append(errs, ve...)
			}
		}
	}

	if len(errs) > 0 {
		return errs
	}
	return nil
}

// Clone creates a deep copy of BackendOverride.
func (b *BackendOverride) Clone() *BackendOverride {
	if b == nil {
		return nil
	}

	clone := &BackendOverride{}

	if b.Config != nil {
		clone.Config = make(map[string]string, len(b.Config))
		for k, v := range b.Config {
			clone.Config[k] = v
		}
	}

	return clone
}

// Merge merges another BackendOverride into this one.
func (b *BackendOverride) Merge(other *BackendOverride) {
	if other == nil {
		return
	}

	if other.Config != nil {
		if b.Config == nil {
			b.Config = make(map[string]string)
		}
		for k, v := range other.Config {
			b.Config[k] = v
		}
	}
}

// Clone creates a deep copy of NodeObjectStorageConfig.
func (n *NodeObjectStorageConfig) Clone() *NodeObjectStorageConfig {
	if n == nil {
		return nil
	}

	clone := &NodeObjectStorageConfig{
		InternalBackend: n.InternalBackend,
		CacheBackend:    n.CacheBackend,
		CacheMaxSize:    n.CacheMaxSize,
	}

	if n.Encryption != nil {
		clone.Encryption = n.Encryption.Clone()
	}

	if n.BackendOverrides != nil {
		clone.BackendOverrides = make(map[string]*BackendOverride, len(n.BackendOverrides))
		for k, v := range n.BackendOverrides {
			clone.BackendOverrides[k] = v.Clone()
		}
	}

	if n.EnabledBackends != nil {
		clone.EnabledBackends = make([]string, len(n.EnabledBackends))
		copy(clone.EnabledBackends, n.EnabledBackends)
	}

	return clone
}

// Merge merges another NodeObjectStorageConfig into this one.
func (n *NodeObjectStorageConfig) Merge(other *NodeObjectStorageConfig) {
	if other == nil {
		return
	}

	if other.InternalBackend != "" {
		n.InternalBackend = other.InternalBackend
	}

	if other.CacheBackend != "" {
		n.CacheBackend = other.CacheBackend
	}

	if other.CacheMaxSize != 0 {
		n.CacheMaxSize = other.CacheMaxSize
	}

	if other.Encryption != nil {
		if n.Encryption == nil {
			n.Encryption = &ObjectStoreEncryption{}
		}
		n.Encryption.Merge(other.Encryption)
	}

	if other.BackendOverrides != nil {
		if n.BackendOverrides == nil {
			n.BackendOverrides = make(map[string]*BackendOverride)
		}
		for k, v := range other.BackendOverrides {
			if existing, ok := n.BackendOverrides[k]; ok {
				existing.Merge(v)
			} else {
				n.BackendOverrides[k] = v.Clone()
			}
		}
	}

	if other.EnabledBackends != nil {
		n.EnabledBackends = make([]string, len(other.EnabledBackends))
		copy(n.EnabledBackends, other.EnabledBackends)
	}
}

// Validate validates the NodeObjectStorageConfig configuration.
func (n *NodeObjectStorageConfig) Validate() error {
	var errs ValidationErrors

	if n.CacheMaxSize < 0 {
		errs = append(errs, ValidationError{
			Field:   "node_object_storage.cache_max_size",
			Message: fmt.Sprintf("cache_max_size cannot be negative, got %d", n.CacheMaxSize),
		})
	}

	if n.Encryption != nil {
		if err := n.Encryption.Validate("node_object_storage.encryption"); err != nil {
			var ve ValidationErrors
			if errors.As(err, &ve) {
				errs = append(errs, ve...)
			}
		}
	}

	if len(errs) > 0 {
		return errs
	}
	return nil
}

// Clone creates a deep copy of ObjectStoreFullConfig.
func (c *ObjectStoreFullConfig) Clone() *ObjectStoreFullConfig {
	if c == nil {
		return nil
	}

	clone := &ObjectStoreFullConfig{
		Defaults: *c.Defaults.Clone(),
	}

	if c.Backends != nil {
		clone.Backends = make(map[string]*StorageBackendConfig, len(c.Backends))
		for k, v := range c.Backends {
			if v != nil {
				backendClone := &StorageBackendConfig{
					Type:        v.Type,
					Description: v.Description,
				}
				if v.Config != nil {
					backendClone.Config = make(map[string]string, len(v.Config))
					for ck, cv := range v.Config {
						backendClone.Config[ck] = cv
					}
				}
				clone.Backends[k] = backendClone
			}
		}
	}

	if c.Tenants != nil {
		clone.Tenants = make(map[string]*ObjectStoreTenantConfig, len(c.Tenants))
		for k, v := range c.Tenants {
			clone.Tenants[k] = v.Clone()
		}
	}

	return clone
}

// Merge merges another ObjectStoreFullConfig into this one.
func (c *ObjectStoreFullConfig) Merge(other *ObjectStoreFullConfig) {
	if other == nil {
		return
	}

	if other.Backends != nil {
		if c.Backends == nil {
			c.Backends = make(map[string]*StorageBackendConfig)
		}
		for k, v := range other.Backends {
			if v != nil {
				backendClone := &StorageBackendConfig{
					Type:        v.Type,
					Description: v.Description,
				}
				if v.Config != nil {
					backendClone.Config = make(map[string]string, len(v.Config))
					for ck, cv := range v.Config {
						backendClone.Config[ck] = cv
					}
				}
				c.Backends[k] = backendClone
			}
		}
	}

	c.Defaults.Merge(&other.Defaults)

	if other.Tenants != nil {
		if c.Tenants == nil {
			c.Tenants = make(map[string]*ObjectStoreTenantConfig)
		}
		for k, v := range other.Tenants {
			if existing, ok := c.Tenants[k]; ok {
				existing.Merge(v)
			} else {
				c.Tenants[k] = v.Clone()
			}
		}
	}
}

// Validate validates the ObjectStoreFullConfig configuration.
func (c *ObjectStoreFullConfig) Validate() error {
	var errs ValidationErrors

	// Validate defaults
	if err := c.Defaults.Validate(); err != nil {
		var ve ValidationErrors
		if errors.As(err, &ve) {
			errs = append(errs, ve...)
		}
	}

	// Validate that primary backend in defaults references a defined backend
	if c.Defaults.PrimaryBackend != "" && c.Backends != nil {
		if _, ok := c.Backends[c.Defaults.PrimaryBackend]; !ok {
			errs = append(errs, ValidationError{
				Field:   "defaults.primary_backend",
				Message: fmt.Sprintf("primary backend '%s' is not defined in backends", c.Defaults.PrimaryBackend),
			})
		}
	}

	// Validate required backends reference defined backends
	if c.Backends != nil {
		for _, required := range c.Defaults.RequiredBackends {
			if _, ok := c.Backends[required]; !ok {
				errs = append(errs, ValidationError{
					Field:   "defaults.required_backends",
					Message: fmt.Sprintf("required backend '%s' is not defined in backends", required),
				})
			}
		}
	}

	// Validate replication target stores reference defined backends
	if c.Backends != nil {
		for _, target := range c.Defaults.Replication.TargetStores {
			if _, ok := c.Backends[target]; !ok {
				errs = append(errs, ValidationError{
					Field:   "defaults.replication.target_stores",
					Message: fmt.Sprintf("replication target '%s' is not defined in backends", target),
				})
			}
		}
	}

	// Validate archive target store references a defined backend
	if c.Defaults.Archive.TargetStore != "" && c.Backends != nil {
		if _, ok := c.Backends[c.Defaults.Archive.TargetStore]; !ok {
			errs = append(errs, ValidationError{
				Field:   "defaults.archive.target_store",
				Message: fmt.Sprintf("archive target '%s' is not defined in backends", c.Defaults.Archive.TargetStore),
			})
		}
	}

	// Validate tenant configurations
	for tenantID, tenantCfg := range c.Tenants {
		prefix := fmt.Sprintf("tenants.%s", tenantID)
		if err := tenantCfg.Validate(prefix); err != nil {
			var ve ValidationErrors
			if errors.As(err, &ve) {
				errs = append(errs, ve...)
			}
		}

		// Validate tenant backends reference defined backends
		if c.Backends != nil {
			if tenantCfg.PrimaryBackend != "" {
				if _, ok := c.Backends[tenantCfg.PrimaryBackend]; !ok {
					errs = append(errs, ValidationError{
						Field:   prefix + ".primary_backend",
						Message: fmt.Sprintf("tenant primary backend '%s' is not defined in backends", tenantCfg.PrimaryBackend),
					})
				}
			}

			for _, allowed := range tenantCfg.AllowedBackends {
				if _, ok := c.Backends[allowed]; !ok {
					errs = append(errs, ValidationError{
						Field:   prefix + ".allowed_backends",
						Message: fmt.Sprintf("allowed backend '%s' is not defined in backends", allowed),
					})
				}
			}

			// Validate tenant replication targets
			if tenantCfg.Replication != nil {
				for _, target := range tenantCfg.Replication.TargetStores {
					if _, ok := c.Backends[target]; !ok {
						errs = append(errs, ValidationError{
							Field:   prefix + ".replication.target_stores",
							Message: fmt.Sprintf("replication target '%s' is not defined in backends", target),
						})
					}
				}
			}

			// Validate shard backends
			for shardID, shardCfg := range tenantCfg.ShardConfigs {
				shardPrefix := fmt.Sprintf("%s.shard_configs.%d", prefix, shardID)
				if shardCfg.PrimaryBackend != "" {
					if _, ok := c.Backends[shardCfg.PrimaryBackend]; !ok {
						errs = append(errs, ValidationError{
							Field:   shardPrefix + ".primary_backend",
							Message: fmt.Sprintf("shard primary backend '%s' is not defined in backends", shardCfg.PrimaryBackend),
						})
					}
				}
				if shardCfg.Replication != nil {
					for _, target := range shardCfg.Replication.TargetStores {
						if _, ok := c.Backends[target]; !ok {
							errs = append(errs, ValidationError{
								Field:   shardPrefix + ".replication.target_stores",
								Message: fmt.Sprintf("shard replication target '%s' is not defined in backends", target),
							})
						}
					}
				}
			}
		}
	}

	if len(errs) > 0 {
		return errs
	}
	return nil
}

// ValidateBackendReferences validates that all backend references are valid.
// This is a helper for external validation when backends are defined elsewhere.
func ValidateBackendReferences(cfg *ObjectStoreFullConfig, definedBackends []string) error {
	var errs ValidationErrors
	backendSet := make(map[string]bool, len(definedBackends))
	for _, b := range definedBackends {
		backendSet[b] = true
	}

	// Check primary backend
	if cfg.Defaults.PrimaryBackend != "" && !backendSet[cfg.Defaults.PrimaryBackend] {
		errs = append(errs, ValidationError{
			Field:   "defaults.primary_backend",
			Message: fmt.Sprintf("backend '%s' is not defined", cfg.Defaults.PrimaryBackend),
		})
	}

	// Check required backends
	for _, required := range cfg.Defaults.RequiredBackends {
		if !backendSet[required] {
			errs = append(errs, ValidationError{
				Field:   "defaults.required_backends",
				Message: fmt.Sprintf("backend '%s' is not defined", required),
			})
		}
	}

	if len(errs) > 0 {
		return errs
	}
	return nil
}

// String returns a human-readable representation of ObjectStoreDefaults.
func (d *ObjectStoreDefaults) String() string {
	var sb strings.Builder
	sb.WriteString("ObjectStoreDefaults{")
	sb.WriteString(fmt.Sprintf("PrimaryBackend: %q, ", d.PrimaryBackend))
	sb.WriteString(fmt.Sprintf("ValueThreshold: %d, ", d.ValueThreshold))
	sb.WriteString(fmt.Sprintf("Compression: %q", d.Compression))
	sb.WriteString("}")
	return sb.String()
}
