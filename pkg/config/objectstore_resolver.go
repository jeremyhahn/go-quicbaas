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
	"context"
	"fmt"
	"sort"
	"sync"
)

// ResolvedObjectStoreConfig represents the effective configuration for a tenant/shard
// after resolving the 3-layer configuration hierarchy.
type ResolvedObjectStoreConfig struct {
	// PrimaryBackend is the backend to use for storing objects.
	PrimaryBackend string

	// AllowedBackends lists backends the tenant may use.
	AllowedBackends []string

	// EnabledBackends lists backends enabled on this node.
	EnabledBackends []string

	// Compression specifies the compression algorithm.
	Compression string

	// ValueThreshold is the size threshold for offloading to object storage.
	ValueThreshold int64

	// Encryption contains the effective encryption configuration.
	Encryption *ObjectStoreEncryption

	// Replication contains the effective replication configuration.
	Replication *ObjectStoreReplication

	// Archive contains the effective archive policy configuration.
	Archive *ObjectStoreArchive

	// Retention contains the effective retention policy configuration.
	Retention *ObjectStoreRetention

	// Quotas contains the effective quota configuration (nil if no quotas).
	Quotas *ObjectStoreQuotas
}

// TenantUsage represents current resource usage for a tenant.
type TenantUsage struct {
	// TotalBytes is the total storage bytes used by the tenant.
	TotalBytes int64

	// ObjectCount is the number of objects stored by the tenant.
	ObjectCount int64
}

// TenantQuotas represents quota settings for a tenant.
type TenantQuotas struct {
	// MaxStorageBytes is the maximum storage quota (0 = unlimited).
	MaxStorageBytes int64

	// MaxObjectSize is the maximum object size in bytes (0 = unlimited).
	MaxObjectSize int64

	// MaxObjectCount is the maximum number of objects (0 = unlimited).
	MaxObjectCount int64
}

// QuotaErrorCode represents the type of quota violation.
type QuotaErrorCode int

const (
	// QuotaErrorCodeStorage indicates storage limit exceeded.
	QuotaErrorCodeStorage QuotaErrorCode = iota + 1
	// QuotaErrorCodeObjectSize indicates object size limit exceeded.
	QuotaErrorCodeObjectSize
	// QuotaErrorCodeObjectCount indicates object count limit exceeded.
	QuotaErrorCodeObjectCount
)

// QuotaError represents a quota violation error.
type QuotaError struct {
	TenantID string
	Code     QuotaErrorCode
	Reason   string
	Limit    int64
	Current  int64
}

// Error implements the error interface.
func (e *QuotaError) Error() string {
	return fmt.Sprintf("quota exceeded for tenant %q: %s (limit: %d, current: %d)",
		e.TenantID, e.Reason, e.Limit, e.Current)
}

// ObjectStoreConfigResolver resolves the effective configuration for tenants and shards
// by merging the 3-layer configuration hierarchy.
type ObjectStoreConfigResolver interface {
	// ResolveConfig returns the effective config for a tenant/shard.
	ResolveConfig(tenantID string, shardID uint64) (*ResolvedObjectStoreConfig, error)

	// ResolveBackend returns the primary backend for a tenant/shard.
	ResolveBackend(tenantID string, shardID uint64) (string, error)

	// ResolveEncryption returns the effective encryption config.
	ResolveEncryption(tenantID string, shardID uint64) (*ObjectStoreEncryption, error)

	// ResolveReplication returns the effective replication config.
	ResolveReplication(tenantID string, shardID uint64) (*ObjectStoreReplication, error)

	// CheckQuotas validates an operation against tenant quotas.
	CheckQuotas(tenantID string, objectSize int64, currentUsage *TenantUsage) error

	// GetTenantConfig returns raw tenant config (nil if not found).
	GetTenantConfig(tenantID string) *ObjectStoreTenantConfig

	// ListTenants returns all configured tenant IDs.
	ListTenants() []string

	// SetTenantQuotas sets or updates quotas for a tenant.
	SetTenantQuotas(ctx context.Context, tenantID string, quotas *TenantQuotas) error

	// Reload reloads configuration (for hot reload support).
	Reload(fullConfig *ObjectStoreFullConfig, nodeConfig *NodeObjectStorageConfig) error
}

// ObjectStoreResolver implements ObjectStoreConfigResolver.
type ObjectStoreResolver struct {
	mu         sync.RWMutex
	defaults   *ObjectStoreDefaults
	nodeConfig *NodeObjectStorageConfig
	tenants    map[string]*ObjectStoreTenantConfig
}

// NewObjectStoreResolver creates a new ObjectStoreConfigResolver.
func NewObjectStoreResolver(
	fullConfig *ObjectStoreFullConfig,
	nodeConfig *NodeObjectStorageConfig,
) *ObjectStoreResolver {
	resolver := &ObjectStoreResolver{
		tenants: make(map[string]*ObjectStoreTenantConfig),
	}

	resolver.loadConfig(fullConfig, nodeConfig)
	return resolver
}

// loadConfig loads configuration into the resolver (internal, not thread-safe).
func (r *ObjectStoreResolver) loadConfig(
	fullConfig *ObjectStoreFullConfig,
	nodeConfig *NodeObjectStorageConfig,
) {
	// Set defaults
	if fullConfig != nil {
		r.defaults = fullConfig.Defaults.Clone()
	}
	if r.defaults == nil {
		r.defaults = DefaultObjectStoreDefaults()
	}

	// Set node config
	if nodeConfig != nil {
		r.nodeConfig = nodeConfig.Clone()
	} else {
		r.nodeConfig = nil
	}

	// Set tenant configs
	r.tenants = make(map[string]*ObjectStoreTenantConfig)
	if fullConfig != nil && fullConfig.Tenants != nil {
		for tenantID, tenantCfg := range fullConfig.Tenants {
			if tenantCfg != nil {
				r.tenants[tenantID] = tenantCfg.Clone()
			}
		}
	}
}

// ResolveConfig returns the effective config for a tenant/shard.
func (r *ObjectStoreResolver) ResolveConfig(tenantID string, shardID uint64) (*ResolvedObjectStoreConfig, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	// Start with cluster defaults
	resolved := &ResolvedObjectStoreConfig{
		PrimaryBackend: r.defaults.PrimaryBackend,
		Compression:    r.defaults.Compression,
		ValueThreshold: r.defaults.ValueThreshold,
		Encryption:     r.defaults.Encryption.Clone(),
		Replication:    r.defaults.Replication.Clone(),
		Archive:        r.defaults.Archive.Clone(),
		Retention:      r.defaults.Retention.Clone(),
	}

	// Copy required backends as allowed backends (defaults)
	if len(r.defaults.RequiredBackends) > 0 {
		resolved.AllowedBackends = make([]string, len(r.defaults.RequiredBackends))
		copy(resolved.AllowedBackends, r.defaults.RequiredBackends)
	}

	// Apply node config
	if r.nodeConfig != nil {
		// Copy enabled backends
		if len(r.nodeConfig.EnabledBackends) > 0 {
			resolved.EnabledBackends = make([]string, len(r.nodeConfig.EnabledBackends))
			copy(resolved.EnabledBackends, r.nodeConfig.EnabledBackends)
		}

		// Apply node encryption if set
		if r.nodeConfig.Encryption != nil {
			if resolved.Encryption == nil {
				resolved.Encryption = &ObjectStoreEncryption{}
			}
			resolved.Encryption.Merge(r.nodeConfig.Encryption)
		}
	}

	// Apply tenant config if exists
	tenantCfg := r.tenants[tenantID]
	if tenantCfg != nil {
		// Override primary backend
		if tenantCfg.PrimaryBackend != "" {
			resolved.PrimaryBackend = tenantCfg.PrimaryBackend
		}

		// Override allowed backends
		if len(tenantCfg.AllowedBackends) > 0 {
			resolved.AllowedBackends = make([]string, len(tenantCfg.AllowedBackends))
			copy(resolved.AllowedBackends, tenantCfg.AllowedBackends)
		}

		// Merge tenant encryption
		if tenantCfg.Encryption != nil {
			if resolved.Encryption == nil {
				resolved.Encryption = &ObjectStoreEncryption{}
			}
			resolved.Encryption.Merge(tenantCfg.Encryption)
		}

		// Merge tenant replication
		if tenantCfg.Replication != nil {
			if resolved.Replication == nil {
				resolved.Replication = &ObjectStoreReplication{}
			}
			resolved.Replication.Merge(tenantCfg.Replication)
		}

		// Merge tenant archive
		if tenantCfg.Archive != nil {
			if resolved.Archive == nil {
				resolved.Archive = &ObjectStoreArchive{}
			}
			resolved.Archive.Merge(tenantCfg.Archive)
		}

		// Merge tenant retention
		if tenantCfg.Retention != nil {
			if resolved.Retention == nil {
				resolved.Retention = &ObjectStoreRetention{}
			}
			resolved.Retention.Merge(tenantCfg.Retention)
		}

		// Apply shard config if exists
		if tenantCfg.ShardConfigs != nil {
			if shardCfg := tenantCfg.ShardConfigs[shardID]; shardCfg != nil {
				// Override primary backend for shard
				if shardCfg.PrimaryBackend != "" {
					resolved.PrimaryBackend = shardCfg.PrimaryBackend
				}

				// Merge shard replication
				if shardCfg.Replication != nil {
					if resolved.Replication == nil {
						resolved.Replication = &ObjectStoreReplication{}
					}
					resolved.Replication.Merge(shardCfg.Replication)
				}
			}
		}

		// Copy quotas (always from tenant, not merged)
		resolved.Quotas = tenantCfg.Quotas.Clone()
	}

	return resolved, nil
}

// ResolveBackend returns the primary backend for a tenant/shard.
func (r *ObjectStoreResolver) ResolveBackend(tenantID string, shardID uint64) (string, error) {
	resolved, err := r.ResolveConfig(tenantID, shardID)
	if err != nil {
		return "", err
	}
	return resolved.PrimaryBackend, nil
}

// ResolveEncryption returns the effective encryption config.
func (r *ObjectStoreResolver) ResolveEncryption(tenantID string, shardID uint64) (*ObjectStoreEncryption, error) {
	resolved, err := r.ResolveConfig(tenantID, shardID)
	if err != nil {
		return nil, err
	}
	return resolved.Encryption, nil
}

// ResolveReplication returns the effective replication config.
func (r *ObjectStoreResolver) ResolveReplication(tenantID string, shardID uint64) (*ObjectStoreReplication, error) {
	resolved, err := r.ResolveConfig(tenantID, shardID)
	if err != nil {
		return nil, err
	}
	return resolved.Replication, nil
}

// CheckQuotas validates an operation against tenant quotas.
func (r *ObjectStoreResolver) CheckQuotas(tenantID string, objectSize int64, usage *TenantUsage) error {
	r.mu.RLock()
	tenantCfg := r.tenants[tenantID]
	// No quotas configured for tenant
	if tenantCfg == nil {
		r.mu.RUnlock()
		return nil
	}
	// Copy quotas while holding lock to avoid race with SetTenantQuotas
	quotas := tenantCfg.Quotas
	r.mu.RUnlock()

	// Handle nil usage gracefully
	if usage == nil {
		usage = &TenantUsage{}
	}

	// Check storage limit
	if quotas.MaxStorageBytes > 0 {
		newTotal := usage.TotalBytes + objectSize
		if newTotal > quotas.MaxStorageBytes {
			return &QuotaError{
				TenantID: tenantID,
				Code:     QuotaErrorCodeStorage,
				Reason:   "storage limit exceeded",
				Limit:    quotas.MaxStorageBytes,
				Current:  usage.TotalBytes,
			}
		}
	}

	// Check object size limit
	if quotas.MaxObjectSize > 0 && objectSize > quotas.MaxObjectSize {
		return &QuotaError{
			TenantID: tenantID,
			Code:     QuotaErrorCodeObjectSize,
			Reason:   "object size limit exceeded",
			Limit:    quotas.MaxObjectSize,
			Current:  objectSize,
		}
	}

	// Check object count limit
	if quotas.MaxObjectCount > 0 {
		newCount := usage.ObjectCount + 1
		if newCount > quotas.MaxObjectCount {
			return &QuotaError{
				TenantID: tenantID,
				Code:     QuotaErrorCodeObjectCount,
				Reason:   "object count limit exceeded",
				Limit:    quotas.MaxObjectCount,
				Current:  usage.ObjectCount,
			}
		}
	}

	return nil
}

// GetTenantConfig returns raw tenant config (nil if not found).
func (r *ObjectStoreResolver) GetTenantConfig(tenantID string) *ObjectStoreTenantConfig {
	r.mu.RLock()
	defer r.mu.RUnlock()

	cfg := r.tenants[tenantID]
	if cfg == nil {
		return nil
	}
	return cfg.Clone()
}

// ListTenants returns all configured tenant IDs in sorted order.
func (r *ObjectStoreResolver) ListTenants() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()

	tenants := make([]string, 0, len(r.tenants))
	for id := range r.tenants {
		tenants = append(tenants, id)
	}
	sort.Strings(tenants)
	return tenants
}

// SetTenantQuotas sets or updates quotas for a tenant.
func (r *ObjectStoreResolver) SetTenantQuotas(_ context.Context, tenantID string, quotas *TenantQuotas) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	// Get or create tenant config
	tenantCfg := r.tenants[tenantID]
	if tenantCfg == nil {
		tenantCfg = &ObjectStoreTenantConfig{}
		r.tenants[tenantID] = tenantCfg
	}

	// Update quotas (handle nil quotas by setting all to zero/unlimited)
	if quotas == nil {
		tenantCfg.Quotas = ObjectStoreQuotas{}
	} else {
		tenantCfg.Quotas = ObjectStoreQuotas{
			MaxStorageBytes: quotas.MaxStorageBytes,
			MaxObjectSize:   quotas.MaxObjectSize,
			MaxObjectCount:  quotas.MaxObjectCount,
		}
	}

	return nil
}

// Reload reloads configuration (for hot reload support).
func (r *ObjectStoreResolver) Reload(fullConfig *ObjectStoreFullConfig, nodeConfig *NodeObjectStorageConfig) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.loadConfig(fullConfig, nodeConfig)
	return nil
}

// Clone creates a deep copy of ResolvedObjectStoreConfig.
func (c *ResolvedObjectStoreConfig) Clone() *ResolvedObjectStoreConfig {
	if c == nil {
		return nil
	}

	clone := &ResolvedObjectStoreConfig{
		PrimaryBackend: c.PrimaryBackend,
		Compression:    c.Compression,
		ValueThreshold: c.ValueThreshold,
	}

	if c.AllowedBackends != nil {
		clone.AllowedBackends = make([]string, len(c.AllowedBackends))
		copy(clone.AllowedBackends, c.AllowedBackends)
	}

	if c.EnabledBackends != nil {
		clone.EnabledBackends = make([]string, len(c.EnabledBackends))
		copy(clone.EnabledBackends, c.EnabledBackends)
	}

	if c.Encryption != nil {
		clone.Encryption = c.Encryption.Clone()
	}

	if c.Replication != nil {
		clone.Replication = c.Replication.Clone()
	}

	if c.Archive != nil {
		clone.Archive = c.Archive.Clone()
	}

	if c.Retention != nil {
		clone.Retention = c.Retention.Clone()
	}

	if c.Quotas != nil {
		clone.Quotas = c.Quotas.Clone()
	}

	return clone
}

// Clone creates a deep copy of TenantUsage.
func (u *TenantUsage) Clone() *TenantUsage {
	if u == nil {
		return nil
	}
	return &TenantUsage{
		TotalBytes:  u.TotalBytes,
		ObjectCount: u.ObjectCount,
	}
}

// Clone creates a deep copy of TenantQuotas.
func (q *TenantQuotas) Clone() *TenantQuotas {
	if q == nil {
		return nil
	}
	return &TenantQuotas{
		MaxStorageBytes: q.MaxStorageBytes,
		MaxObjectSize:   q.MaxObjectSize,
		MaxObjectCount:  q.MaxObjectCount,
	}
}
