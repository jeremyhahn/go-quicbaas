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
	"testing"
)

// createTestFullConfig creates a test ObjectStoreFullConfig with defaults and tenants.
func createTestFullConfig() *ObjectStoreFullConfig {
	return &ObjectStoreFullConfig{
		Backends: map[string]*StorageBackendConfig{
			"local": {Type: "local", Config: map[string]string{"path": "/data"}},
			"s3":    {Type: "s3", Config: map[string]string{"bucket": "test-bucket"}},
			"gcs":   {Type: "gcs", Config: map[string]string{"bucket": "test-gcs"}},
		},
		Defaults: ObjectStoreDefaults{
			PrimaryBackend:   "local",
			RequiredBackends: []string{"local"},
			ValueThreshold:   1024 * 1024, // 1MB
			Compression:      CompressionZstd,
			Encryption: ObjectStoreEncryption{
				Enabled:    true,
				Enforce:    true,
				DefaultKey: "dek:cluster/default",
				Algorithm:  "AES-256-GCM",
			},
			Replication: ObjectStoreReplication{
				Enabled:      true,
				MinReplicas:  2,
				TargetStores: []string{"s3"},
				Mode:         ReplicationModeAsync,
				Priority:     5,
			},
			Archive: ObjectStoreArchive{
				Enabled:          false,
				ArchiveAfterDays: 90,
				TargetStore:      "gcs",
				CompressionLevel: 6,
			},
			Retention: ObjectStoreRetention{
				Enabled:       false,
				RetentionDays: 365,
				MaxVersions:   10,
			},
		},
		Tenants: map[string]*ObjectStoreTenantConfig{
			"tenant-a": {
				PrimaryBackend:  "s3",
				AllowedBackends: []string{"s3", "gcs"},
				Encryption: &ObjectStoreEncryption{
					Enabled:    true,
					DefaultKey: "dek:tenant-a/key",
				},
				Replication: &ObjectStoreReplication{
					Enabled:      true,
					MinReplicas:  3,
					TargetStores: []string{"gcs"},
					Mode:         ReplicationModeSync,
				},
				Archive: &ObjectStoreArchive{
					Enabled:          true,
					ArchiveAfterDays: 30,
				},
				Quotas: ObjectStoreQuotas{
					MaxStorageBytes: 10 * 1024 * 1024 * 1024, // 10GB
					MaxObjectSize:   100 * 1024 * 1024,       // 100MB
					MaxObjectCount:  10000,
				},
				ShardConfigs: map[uint64]*ObjectStoreShardConfig{
					1: {
						PrimaryBackend: "gcs",
						Replication: &ObjectStoreReplication{
							MinReplicas: 4,
							Mode:        ReplicationModeQuorum,
						},
					},
				},
			},
			"tenant-b": {
				PrimaryBackend: "local",
				Quotas: ObjectStoreQuotas{
					MaxStorageBytes: 1024 * 1024 * 1024, // 1GB
					MaxObjectSize:   50 * 1024 * 1024,   // 50MB
					MaxObjectCount:  1000,
				},
			},
		},
	}
}

// createTestNodeConfig creates a test NodeObjectStorageConfig.
func createTestNodeConfig() *NodeObjectStorageConfig {
	return &NodeObjectStorageConfig{
		InternalBackend: "local",
		CacheBackend:    "local",
		CacheMaxSize:    1024 * 1024 * 1024,
		EnabledBackends: []string{"local", "s3"},
		Encryption: &ObjectStoreEncryption{
			Enabled:    true,
			DefaultKey: "dek:node/key",
		},
	}
}

func TestNewObjectStoreResolver(t *testing.T) {
	t.Run("creates resolver with full config", func(t *testing.T) {
		fullCfg := createTestFullConfig()
		nodeCfg := createTestNodeConfig()

		resolver := NewObjectStoreResolver(fullCfg, nodeCfg)

		if resolver == nil {
			t.Fatal("expected resolver to be non-nil")
		}

		if resolver.defaults == nil {
			t.Error("expected defaults to be set")
		}

		if resolver.nodeConfig == nil {
			t.Error("expected nodeConfig to be set")
		}

		if len(resolver.tenants) != 2 {
			t.Errorf("expected 2 tenants, got %d", len(resolver.tenants))
		}
	})

	t.Run("creates resolver with nil full config", func(t *testing.T) {
		resolver := NewObjectStoreResolver(nil, nil)

		if resolver == nil {
			t.Fatal("expected resolver to be non-nil")
		}

		if resolver.defaults == nil {
			t.Error("expected default defaults to be set")
		}

		// Should have sensible defaults
		if resolver.defaults.Compression != CompressionZstd {
			t.Errorf("expected default compression %s, got %s", CompressionZstd, resolver.defaults.Compression)
		}
	})

	t.Run("creates resolver with nil node config", func(t *testing.T) {
		fullCfg := createTestFullConfig()
		resolver := NewObjectStoreResolver(fullCfg, nil)

		if resolver == nil {
			t.Fatal("expected resolver to be non-nil")
		}

		if resolver.nodeConfig != nil {
			t.Error("expected nodeConfig to be nil")
		}
	})

	t.Run("creates resolver with empty tenants map", func(t *testing.T) {
		fullCfg := &ObjectStoreFullConfig{
			Defaults: *DefaultObjectStoreDefaults(),
			Tenants:  nil,
		}
		resolver := NewObjectStoreResolver(fullCfg, nil)

		if len(resolver.tenants) != 0 {
			t.Errorf("expected 0 tenants, got %d", len(resolver.tenants))
		}
	})

	t.Run("creates resolver with nil tenant in map", func(t *testing.T) {
		fullCfg := &ObjectStoreFullConfig{
			Defaults: *DefaultObjectStoreDefaults(),
			Tenants: map[string]*ObjectStoreTenantConfig{
				"nil-tenant": nil,
			},
		}
		resolver := NewObjectStoreResolver(fullCfg, nil)

		// Should skip nil tenant
		if _, exists := resolver.tenants["nil-tenant"]; exists {
			t.Error("should not include nil tenant")
		}
	})
}

func TestObjectStoreResolver_ResolveConfig(t *testing.T) {
	t.Run("resolves defaults only for unknown tenant", func(t *testing.T) {
		fullCfg := createTestFullConfig()
		resolver := NewObjectStoreResolver(fullCfg, nil)

		resolved, err := resolver.ResolveConfig("unknown-tenant", 0)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if resolved.PrimaryBackend != "local" {
			t.Errorf("expected primary backend 'local', got %q", resolved.PrimaryBackend)
		}

		if resolved.Compression != CompressionZstd {
			t.Errorf("expected compression %s, got %s", CompressionZstd, resolved.Compression)
		}

		if resolved.ValueThreshold != 1024*1024 {
			t.Errorf("expected value threshold 1048576, got %d", resolved.ValueThreshold)
		}

		if resolved.Quotas != nil {
			t.Error("expected nil quotas for unknown tenant")
		}
	})

	t.Run("applies node config layer", func(t *testing.T) {
		fullCfg := createTestFullConfig()
		nodeCfg := createTestNodeConfig()
		resolver := NewObjectStoreResolver(fullCfg, nodeCfg)

		resolved, err := resolver.ResolveConfig("unknown-tenant", 0)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		// Check enabled backends from node config
		if len(resolved.EnabledBackends) != 2 {
			t.Errorf("expected 2 enabled backends, got %d", len(resolved.EnabledBackends))
		}

		// Check encryption key merged from node config
		if resolved.Encryption.DefaultKey != "dek:node/key" {
			t.Errorf("expected node encryption key, got %q", resolved.Encryption.DefaultKey)
		}
	})

	t.Run("applies tenant config layer", func(t *testing.T) {
		fullCfg := createTestFullConfig()
		resolver := NewObjectStoreResolver(fullCfg, nil)

		resolved, err := resolver.ResolveConfig("tenant-a", 0)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		// Tenant overrides primary backend
		if resolved.PrimaryBackend != "s3" {
			t.Errorf("expected primary backend 's3', got %q", resolved.PrimaryBackend)
		}

		// Tenant overrides allowed backends
		if len(resolved.AllowedBackends) != 2 {
			t.Errorf("expected 2 allowed backends, got %d", len(resolved.AllowedBackends))
		}

		// Tenant merges encryption
		if resolved.Encryption.DefaultKey != "dek:tenant-a/key" {
			t.Errorf("expected tenant encryption key, got %q", resolved.Encryption.DefaultKey)
		}

		// Tenant merges replication
		if resolved.Replication.MinReplicas != 3 {
			t.Errorf("expected 3 min replicas, got %d", resolved.Replication.MinReplicas)
		}
		if resolved.Replication.Mode != ReplicationModeSync {
			t.Errorf("expected sync replication mode, got %s", resolved.Replication.Mode)
		}

		// Tenant merges archive
		if !resolved.Archive.Enabled {
			t.Error("expected archive enabled")
		}
		if resolved.Archive.ArchiveAfterDays != 30 {
			t.Errorf("expected archive after 30 days, got %d", resolved.Archive.ArchiveAfterDays)
		}

		// Tenant quotas
		if resolved.Quotas == nil {
			t.Fatal("expected quotas to be set")
		}
		if resolved.Quotas.MaxStorageBytes != 10*1024*1024*1024 {
			t.Errorf("expected max storage 10GB, got %d", resolved.Quotas.MaxStorageBytes)
		}
	})

	t.Run("applies shard config layer", func(t *testing.T) {
		fullCfg := createTestFullConfig()
		resolver := NewObjectStoreResolver(fullCfg, nil)

		resolved, err := resolver.ResolveConfig("tenant-a", 1)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		// Shard overrides primary backend
		if resolved.PrimaryBackend != "gcs" {
			t.Errorf("expected primary backend 'gcs', got %q", resolved.PrimaryBackend)
		}

		// Shard merges replication
		if resolved.Replication.MinReplicas != 4 {
			t.Errorf("expected 4 min replicas from shard, got %d", resolved.Replication.MinReplicas)
		}
		if resolved.Replication.Mode != ReplicationModeQuorum {
			t.Errorf("expected quorum replication mode, got %s", resolved.Replication.Mode)
		}
	})

	t.Run("shard without config uses tenant config", func(t *testing.T) {
		fullCfg := createTestFullConfig()
		resolver := NewObjectStoreResolver(fullCfg, nil)

		resolved, err := resolver.ResolveConfig("tenant-a", 999)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		// Should use tenant config (no shard override)
		if resolved.PrimaryBackend != "s3" {
			t.Errorf("expected primary backend 's3', got %q", resolved.PrimaryBackend)
		}
	})

	t.Run("all layers combined", func(t *testing.T) {
		fullCfg := createTestFullConfig()
		nodeCfg := createTestNodeConfig()
		resolver := NewObjectStoreResolver(fullCfg, nodeCfg)

		resolved, err := resolver.ResolveConfig("tenant-a", 1)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		// Shard override (highest priority)
		if resolved.PrimaryBackend != "gcs" {
			t.Errorf("expected shard primary backend 'gcs', got %q", resolved.PrimaryBackend)
		}

		// Node config (enabled backends)
		if len(resolved.EnabledBackends) != 2 {
			t.Errorf("expected 2 enabled backends from node, got %d", len(resolved.EnabledBackends))
		}

		// Tenant config (allowed backends)
		if len(resolved.AllowedBackends) != 2 {
			t.Errorf("expected 2 allowed backends from tenant, got %d", len(resolved.AllowedBackends))
		}

		// Defaults (compression)
		if resolved.Compression != CompressionZstd {
			t.Errorf("expected compression from defaults, got %s", resolved.Compression)
		}
	})

	t.Run("handles tenant with retention override", func(t *testing.T) {
		fullCfg := &ObjectStoreFullConfig{
			Defaults: *DefaultObjectStoreDefaults(),
			Tenants: map[string]*ObjectStoreTenantConfig{
				"retention-tenant": {
					Retention: &ObjectStoreRetention{
						Enabled:       true,
						RetentionDays: 60,
						MaxVersions:   5,
					},
				},
			},
		}
		resolver := NewObjectStoreResolver(fullCfg, nil)

		resolved, err := resolver.ResolveConfig("retention-tenant", 0)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if !resolved.Retention.Enabled {
			t.Error("expected retention enabled")
		}
		if resolved.Retention.RetentionDays != 60 {
			t.Errorf("expected 60 retention days, got %d", resolved.Retention.RetentionDays)
		}
	})

	t.Run("handles empty required backends", func(t *testing.T) {
		fullCfg := &ObjectStoreFullConfig{
			Defaults: ObjectStoreDefaults{
				PrimaryBackend:   "local",
				RequiredBackends: nil,
				Compression:      CompressionZstd,
			},
		}
		resolver := NewObjectStoreResolver(fullCfg, nil)

		resolved, err := resolver.ResolveConfig("tenant", 0)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if resolved.AllowedBackends != nil {
			t.Errorf("expected nil allowed backends, got %v", resolved.AllowedBackends)
		}
	})

	t.Run("handles node config without encryption", func(t *testing.T) {
		fullCfg := createTestFullConfig()
		nodeCfg := &NodeObjectStorageConfig{
			EnabledBackends: []string{"local"},
			Encryption:      nil,
		}
		resolver := NewObjectStoreResolver(fullCfg, nodeCfg)

		resolved, err := resolver.ResolveConfig("unknown", 0)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		// Should use cluster defaults encryption
		if resolved.Encryption.DefaultKey != "dek:cluster/default" {
			t.Errorf("expected cluster default key, got %q", resolved.Encryption.DefaultKey)
		}
	})

	t.Run("handles shard config without replication", func(t *testing.T) {
		fullCfg := &ObjectStoreFullConfig{
			Defaults: *DefaultObjectStoreDefaults(),
			Tenants: map[string]*ObjectStoreTenantConfig{
				"shard-tenant": {
					PrimaryBackend: "s3",
					ShardConfigs: map[uint64]*ObjectStoreShardConfig{
						1: {
							PrimaryBackend: "gcs",
							Replication:    nil, // No replication override
						},
					},
				},
			},
		}
		resolver := NewObjectStoreResolver(fullCfg, nil)

		resolved, err := resolver.ResolveConfig("shard-tenant", 1)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		// Primary backend from shard
		if resolved.PrimaryBackend != "gcs" {
			t.Errorf("expected 'gcs', got %q", resolved.PrimaryBackend)
		}

		// Replication from defaults (not overridden)
		if resolved.Replication.MinReplicas != 2 {
			t.Errorf("expected 2 min replicas from defaults, got %d", resolved.Replication.MinReplicas)
		}
	})

	t.Run("handles shard with only replication override", func(t *testing.T) {
		fullCfg := &ObjectStoreFullConfig{
			Defaults: *DefaultObjectStoreDefaults(),
			Tenants: map[string]*ObjectStoreTenantConfig{
				"shard-rep-tenant": {
					PrimaryBackend: "s3",
					ShardConfigs: map[uint64]*ObjectStoreShardConfig{
						1: {
							PrimaryBackend: "", // No primary override
							Replication: &ObjectStoreReplication{
								MinReplicas: 5,
							},
						},
					},
				},
			},
		}
		resolver := NewObjectStoreResolver(fullCfg, nil)

		resolved, err := resolver.ResolveConfig("shard-rep-tenant", 1)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		// Primary backend from tenant
		if resolved.PrimaryBackend != "s3" {
			t.Errorf("expected 's3', got %q", resolved.PrimaryBackend)
		}

		// Replication from shard
		if resolved.Replication.MinReplicas != 5 {
			t.Errorf("expected 5 min replicas, got %d", resolved.Replication.MinReplicas)
		}
	})

	t.Run("handles nil shard configs map", func(t *testing.T) {
		fullCfg := &ObjectStoreFullConfig{
			Defaults: *DefaultObjectStoreDefaults(),
			Tenants: map[string]*ObjectStoreTenantConfig{
				"nil-shards": {
					PrimaryBackend: "s3",
					ShardConfigs:   nil,
				},
			},
		}
		resolver := NewObjectStoreResolver(fullCfg, nil)

		resolved, err := resolver.ResolveConfig("nil-shards", 1)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if resolved.PrimaryBackend != "s3" {
			t.Errorf("expected 's3', got %q", resolved.PrimaryBackend)
		}
	})

	t.Run("handles empty node enabled backends", func(t *testing.T) {
		fullCfg := createTestFullConfig()
		nodeCfg := &NodeObjectStorageConfig{
			EnabledBackends: nil,
		}
		resolver := NewObjectStoreResolver(fullCfg, nodeCfg)

		resolved, err := resolver.ResolveConfig("unknown", 0)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if resolved.EnabledBackends != nil {
			t.Errorf("expected nil enabled backends, got %v", resolved.EnabledBackends)
		}
	})
}

func TestObjectStoreResolver_ResolveBackend(t *testing.T) {
	t.Run("returns primary backend for tenant", func(t *testing.T) {
		fullCfg := createTestFullConfig()
		resolver := NewObjectStoreResolver(fullCfg, nil)

		backend, err := resolver.ResolveBackend("tenant-a", 0)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if backend != "s3" {
			t.Errorf("expected backend 's3', got %q", backend)
		}
	})

	t.Run("returns shard backend override", func(t *testing.T) {
		fullCfg := createTestFullConfig()
		resolver := NewObjectStoreResolver(fullCfg, nil)

		backend, err := resolver.ResolveBackend("tenant-a", 1)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if backend != "gcs" {
			t.Errorf("expected backend 'gcs', got %q", backend)
		}
	})

	t.Run("returns default backend for unknown tenant", func(t *testing.T) {
		fullCfg := createTestFullConfig()
		resolver := NewObjectStoreResolver(fullCfg, nil)

		backend, err := resolver.ResolveBackend("unknown", 0)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if backend != "local" {
			t.Errorf("expected backend 'local', got %q", backend)
		}
	})
}

func TestObjectStoreResolver_ResolveEncryption(t *testing.T) {
	t.Run("returns merged encryption config", func(t *testing.T) {
		fullCfg := createTestFullConfig()
		nodeCfg := createTestNodeConfig()
		resolver := NewObjectStoreResolver(fullCfg, nodeCfg)

		enc, err := resolver.ResolveEncryption("tenant-a", 0)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if !enc.Enabled {
			t.Error("expected encryption enabled")
		}

		// Tenant key should override node key
		if enc.DefaultKey != "dek:tenant-a/key" {
			t.Errorf("expected tenant key, got %q", enc.DefaultKey)
		}
	})

	t.Run("returns default encryption for unknown tenant", func(t *testing.T) {
		fullCfg := createTestFullConfig()
		resolver := NewObjectStoreResolver(fullCfg, nil)

		enc, err := resolver.ResolveEncryption("unknown", 0)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if enc.DefaultKey != "dek:cluster/default" {
			t.Errorf("expected default key, got %q", enc.DefaultKey)
		}
	})
}

func TestObjectStoreResolver_ResolveReplication(t *testing.T) {
	t.Run("returns merged replication config", func(t *testing.T) {
		fullCfg := createTestFullConfig()
		resolver := NewObjectStoreResolver(fullCfg, nil)

		rep, err := resolver.ResolveReplication("tenant-a", 1)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		// Shard overrides
		if rep.MinReplicas != 4 {
			t.Errorf("expected 4 min replicas, got %d", rep.MinReplicas)
		}
		if rep.Mode != ReplicationModeQuorum {
			t.Errorf("expected quorum mode, got %s", rep.Mode)
		}
	})

	t.Run("returns default replication for unknown tenant", func(t *testing.T) {
		fullCfg := createTestFullConfig()
		resolver := NewObjectStoreResolver(fullCfg, nil)

		rep, err := resolver.ResolveReplication("unknown", 0)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if rep.MinReplicas != 2 {
			t.Errorf("expected 2 min replicas, got %d", rep.MinReplicas)
		}
	})
}

func TestObjectStoreResolver_CheckQuotas(t *testing.T) {
	t.Run("allows operation within storage limit", func(t *testing.T) {
		fullCfg := createTestFullConfig()
		resolver := NewObjectStoreResolver(fullCfg, nil)

		usage := &TenantUsage{
			TotalBytes:  5 * 1024 * 1024 * 1024, // 5GB
			ObjectCount: 5000,
		}

		err := resolver.CheckQuotas("tenant-a", 1024*1024, usage) // 1MB
		if err != nil {
			t.Errorf("expected no error, got: %v", err)
		}
	})

	t.Run("denies operation exceeding storage limit", func(t *testing.T) {
		fullCfg := createTestFullConfig()
		resolver := NewObjectStoreResolver(fullCfg, nil)

		usage := &TenantUsage{
			TotalBytes:  10*1024*1024*1024 - 1024, // 10GB - 1KB
			ObjectCount: 5000,
		}

		err := resolver.CheckQuotas("tenant-a", 2*1024, usage) // 2KB would exceed
		if err == nil {
			t.Fatal("expected quota error")
		}

		quotaErr, ok := err.(*QuotaError)
		if !ok {
			t.Fatalf("expected QuotaError, got %T", err)
		}

		if quotaErr.Code != QuotaErrorCodeStorage {
			t.Errorf("expected storage quota error code, got %d", quotaErr.Code)
		}

		if quotaErr.TenantID != "tenant-a" {
			t.Errorf("expected tenant-a, got %s", quotaErr.TenantID)
		}
	})

	t.Run("denies operation exceeding object size limit", func(t *testing.T) {
		fullCfg := createTestFullConfig()
		resolver := NewObjectStoreResolver(fullCfg, nil)

		usage := &TenantUsage{TotalBytes: 0, ObjectCount: 0}

		err := resolver.CheckQuotas("tenant-a", 200*1024*1024, usage) // 200MB > 100MB limit
		if err == nil {
			t.Fatal("expected quota error")
		}

		quotaErr, ok := err.(*QuotaError)
		if !ok {
			t.Fatalf("expected QuotaError, got %T", err)
		}

		if quotaErr.Code != QuotaErrorCodeObjectSize {
			t.Errorf("expected object size quota error code, got %d", quotaErr.Code)
		}
	})

	t.Run("denies operation exceeding object count limit", func(t *testing.T) {
		fullCfg := createTestFullConfig()
		resolver := NewObjectStoreResolver(fullCfg, nil)

		usage := &TenantUsage{
			TotalBytes:  1024,
			ObjectCount: 10000, // Already at limit
		}

		err := resolver.CheckQuotas("tenant-a", 1024, usage)
		if err == nil {
			t.Fatal("expected quota error")
		}

		quotaErr, ok := err.(*QuotaError)
		if !ok {
			t.Fatalf("expected QuotaError, got %T", err)
		}

		if quotaErr.Code != QuotaErrorCodeObjectCount {
			t.Errorf("expected object count quota error code, got %d", quotaErr.Code)
		}
	})

	t.Run("allows operation for tenant without quotas", func(t *testing.T) {
		fullCfg := createTestFullConfig()
		resolver := NewObjectStoreResolver(fullCfg, nil)

		usage := &TenantUsage{
			TotalBytes:  1024 * 1024 * 1024 * 1024, // 1TB
			ObjectCount: 1000000,
		}

		err := resolver.CheckQuotas("unknown-tenant", 1024*1024*1024, usage) // 1GB
		if err != nil {
			t.Errorf("expected no error for unknown tenant, got: %v", err)
		}
	})

	t.Run("handles nil usage gracefully", func(t *testing.T) {
		fullCfg := createTestFullConfig()
		resolver := NewObjectStoreResolver(fullCfg, nil)

		err := resolver.CheckQuotas("tenant-a", 1024, nil)
		if err != nil {
			t.Errorf("expected no error with nil usage, got: %v", err)
		}
	})

	t.Run("handles zero quotas as unlimited", func(t *testing.T) {
		fullCfg := &ObjectStoreFullConfig{
			Defaults: *DefaultObjectStoreDefaults(),
			Tenants: map[string]*ObjectStoreTenantConfig{
				"unlimited": {
					Quotas: ObjectStoreQuotas{
						MaxStorageBytes: 0, // 0 = unlimited
						MaxObjectSize:   0,
						MaxObjectCount:  0,
					},
				},
			},
		}
		resolver := NewObjectStoreResolver(fullCfg, nil)

		usage := &TenantUsage{
			TotalBytes:  1024 * 1024 * 1024 * 1024, // 1TB
			ObjectCount: 1000000,
		}

		err := resolver.CheckQuotas("unlimited", 1024*1024*1024, usage)
		if err != nil {
			t.Errorf("expected no error with zero quotas, got: %v", err)
		}
	})

	t.Run("checks storage limit exactly at boundary", func(t *testing.T) {
		fullCfg := &ObjectStoreFullConfig{
			Defaults: *DefaultObjectStoreDefaults(),
			Tenants: map[string]*ObjectStoreTenantConfig{
				"boundary": {
					Quotas: ObjectStoreQuotas{
						MaxStorageBytes: 1000,
					},
				},
			},
		}
		resolver := NewObjectStoreResolver(fullCfg, nil)

		// Exactly at limit should be OK
		usage := &TenantUsage{TotalBytes: 900}
		err := resolver.CheckQuotas("boundary", 100, usage)
		if err != nil {
			t.Errorf("expected no error at boundary, got: %v", err)
		}

		// One over should fail
		err = resolver.CheckQuotas("boundary", 101, usage)
		if err == nil {
			t.Error("expected error when exceeding boundary")
		}
	})
}

func TestObjectStoreResolver_GetTenantConfig(t *testing.T) {
	t.Run("returns tenant config if exists", func(t *testing.T) {
		fullCfg := createTestFullConfig()
		resolver := NewObjectStoreResolver(fullCfg, nil)

		cfg := resolver.GetTenantConfig("tenant-a")
		if cfg == nil {
			t.Fatal("expected tenant config")
		}

		if cfg.PrimaryBackend != "s3" {
			t.Errorf("expected primary backend 's3', got %q", cfg.PrimaryBackend)
		}
	})

	t.Run("returns nil for unknown tenant", func(t *testing.T) {
		fullCfg := createTestFullConfig()
		resolver := NewObjectStoreResolver(fullCfg, nil)

		cfg := resolver.GetTenantConfig("unknown")
		if cfg != nil {
			t.Error("expected nil for unknown tenant")
		}
	})

	t.Run("returns clone of tenant config", func(t *testing.T) {
		fullCfg := createTestFullConfig()
		resolver := NewObjectStoreResolver(fullCfg, nil)

		cfg1 := resolver.GetTenantConfig("tenant-a")
		cfg1.PrimaryBackend = "modified"

		cfg2 := resolver.GetTenantConfig("tenant-a")
		if cfg2.PrimaryBackend == "modified" {
			t.Error("config should be a clone, not shared")
		}
	})
}

func TestObjectStoreResolver_ListTenants(t *testing.T) {
	t.Run("returns all tenant IDs sorted", func(t *testing.T) {
		fullCfg := createTestFullConfig()
		resolver := NewObjectStoreResolver(fullCfg, nil)

		tenants := resolver.ListTenants()
		if len(tenants) != 2 {
			t.Fatalf("expected 2 tenants, got %d", len(tenants))
		}

		// Should be sorted
		if tenants[0] != "tenant-a" || tenants[1] != "tenant-b" {
			t.Errorf("expected sorted tenants [tenant-a, tenant-b], got %v", tenants)
		}
	})

	t.Run("returns empty slice for no tenants", func(t *testing.T) {
		fullCfg := &ObjectStoreFullConfig{
			Defaults: *DefaultObjectStoreDefaults(),
		}
		resolver := NewObjectStoreResolver(fullCfg, nil)

		tenants := resolver.ListTenants()
		if len(tenants) != 0 {
			t.Errorf("expected 0 tenants, got %d", len(tenants))
		}
	})
}

func TestObjectStoreResolver_Reload(t *testing.T) {
	t.Run("reloads configuration", func(t *testing.T) {
		fullCfg := createTestFullConfig()
		resolver := NewObjectStoreResolver(fullCfg, nil)

		// Initial state
		backend, _ := resolver.ResolveBackend("tenant-a", 0)
		if backend != "s3" {
			t.Errorf("expected initial backend 's3', got %q", backend)
		}

		// Create new config with different values
		newCfg := &ObjectStoreFullConfig{
			Defaults: *DefaultObjectStoreDefaults(),
			Tenants: map[string]*ObjectStoreTenantConfig{
				"tenant-a": {
					PrimaryBackend: "gcs",
				},
			},
		}

		err := resolver.Reload(newCfg, nil)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		// Verify new config
		backend, _ = resolver.ResolveBackend("tenant-a", 0)
		if backend != "gcs" {
			t.Errorf("expected reloaded backend 'gcs', got %q", backend)
		}
	})

	t.Run("reloads with nil config uses defaults", func(t *testing.T) {
		fullCfg := createTestFullConfig()
		resolver := NewObjectStoreResolver(fullCfg, nil)

		err := resolver.Reload(nil, nil)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		// Should use defaults
		if resolver.defaults.Compression != CompressionZstd {
			t.Errorf("expected default compression, got %s", resolver.defaults.Compression)
		}
	})

	t.Run("reloads with new node config", func(t *testing.T) {
		fullCfg := createTestFullConfig()
		resolver := NewObjectStoreResolver(fullCfg, nil)

		newNodeCfg := &NodeObjectStorageConfig{
			EnabledBackends: []string{"gcs", "s3", "local"},
		}

		err := resolver.Reload(fullCfg, newNodeCfg)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		resolved, _ := resolver.ResolveConfig("tenant-a", 0)
		if len(resolved.EnabledBackends) != 3 {
			t.Errorf("expected 3 enabled backends, got %d", len(resolved.EnabledBackends))
		}
	})

	t.Run("reload removes old tenants", func(t *testing.T) {
		fullCfg := createTestFullConfig()
		resolver := NewObjectStoreResolver(fullCfg, nil)

		// Verify tenant-b exists
		if resolver.GetTenantConfig("tenant-b") == nil {
			t.Fatal("expected tenant-b to exist")
		}

		// Reload without tenant-b
		newCfg := &ObjectStoreFullConfig{
			Defaults: *DefaultObjectStoreDefaults(),
			Tenants: map[string]*ObjectStoreTenantConfig{
				"tenant-a": {PrimaryBackend: "local"},
			},
		}

		err := resolver.Reload(newCfg, nil)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		// tenant-b should no longer exist
		if resolver.GetTenantConfig("tenant-b") != nil {
			t.Error("expected tenant-b to be removed after reload")
		}
	})
}

func TestQuotaError_Error(t *testing.T) {
	t.Run("formats error message correctly", func(t *testing.T) {
		err := &QuotaError{
			TenantID: "test-tenant",
			Code:     QuotaErrorCodeStorage,
			Reason:   "storage limit exceeded",
			Limit:    1024,
			Current:  512,
		}

		msg := err.Error()
		expected := `quota exceeded for tenant "test-tenant": storage limit exceeded (limit: 1024, current: 512)`
		if msg != expected {
			t.Errorf("expected %q, got %q", expected, msg)
		}
	})
}

func TestResolvedObjectStoreConfig_Clone(t *testing.T) {
	t.Run("clones nil config", func(t *testing.T) {
		var cfg *ResolvedObjectStoreConfig
		clone := cfg.Clone()
		if clone != nil {
			t.Error("expected nil clone")
		}
	})

	t.Run("clones full config", func(t *testing.T) {
		cfg := &ResolvedObjectStoreConfig{
			PrimaryBackend:  "s3",
			AllowedBackends: []string{"s3", "gcs"},
			EnabledBackends: []string{"local", "s3"},
			Compression:     CompressionZstd,
			ValueThreshold:  1024,
			Encryption:      &ObjectStoreEncryption{Enabled: true},
			Replication:     &ObjectStoreReplication{MinReplicas: 2},
			Archive:         &ObjectStoreArchive{Enabled: true},
			Retention:       &ObjectStoreRetention{RetentionDays: 30},
			Quotas:          &ObjectStoreQuotas{MaxStorageBytes: 1024},
		}

		clone := cfg.Clone()

		if clone.PrimaryBackend != cfg.PrimaryBackend {
			t.Error("primary backend not cloned")
		}

		// Modify original
		cfg.AllowedBackends[0] = "modified"
		if clone.AllowedBackends[0] == "modified" {
			t.Error("allowed backends not deep cloned")
		}

		cfg.Encryption.Enabled = false
		if !clone.Encryption.Enabled {
			t.Error("encryption not deep cloned")
		}
	})

	t.Run("clones config with nil fields", func(t *testing.T) {
		cfg := &ResolvedObjectStoreConfig{
			PrimaryBackend:  "s3",
			AllowedBackends: nil,
			EnabledBackends: nil,
			Encryption:      nil,
			Replication:     nil,
			Archive:         nil,
			Retention:       nil,
			Quotas:          nil,
		}

		clone := cfg.Clone()

		if clone.PrimaryBackend != "s3" {
			t.Error("primary backend not cloned")
		}
		if clone.AllowedBackends != nil {
			t.Error("expected nil allowed backends")
		}
		if clone.Encryption != nil {
			t.Error("expected nil encryption")
		}
	})
}

func TestTenantUsage_Clone(t *testing.T) {
	t.Run("clones nil usage", func(t *testing.T) {
		var usage *TenantUsage
		clone := usage.Clone()
		if clone != nil {
			t.Error("expected nil clone")
		}
	})

	t.Run("clones usage", func(t *testing.T) {
		usage := &TenantUsage{
			TotalBytes:  1024,
			ObjectCount: 10,
		}

		clone := usage.Clone()

		if clone.TotalBytes != usage.TotalBytes {
			t.Error("total bytes not cloned")
		}

		// Modify original
		usage.TotalBytes = 2048
		if clone.TotalBytes == 2048 {
			t.Error("clone should be independent")
		}
	})
}

func TestObjectStoreResolver_ConcurrentAccess(t *testing.T) {
	t.Run("handles concurrent reads", func(t *testing.T) {
		fullCfg := createTestFullConfig()
		resolver := NewObjectStoreResolver(fullCfg, nil)

		done := make(chan bool, 10)

		for i := 0; i < 10; i++ {
			go func() {
				for j := 0; j < 100; j++ {
					_, _ = resolver.ResolveConfig("tenant-a", 1)
					_ = resolver.ListTenants()
					_ = resolver.GetTenantConfig("tenant-a")
				}
				done <- true
			}()
		}

		for i := 0; i < 10; i++ {
			<-done
		}
	})

	t.Run("handles concurrent reload and reads", func(t *testing.T) {
		fullCfg := createTestFullConfig()
		resolver := NewObjectStoreResolver(fullCfg, nil)

		done := make(chan bool, 11)

		// Readers
		for i := 0; i < 10; i++ {
			go func() {
				for j := 0; j < 100; j++ {
					_, _ = resolver.ResolveConfig("tenant-a", 1)
					_ = resolver.CheckQuotas("tenant-a", 1024, &TenantUsage{})
				}
				done <- true
			}()
		}

		// Writer (reloader)
		go func() {
			for i := 0; i < 10; i++ {
				newCfg := createTestFullConfig()
				_ = resolver.Reload(newCfg, createTestNodeConfig())
			}
			done <- true
		}()

		for i := 0; i < 11; i++ {
			<-done
		}
	})
}

func TestObjectStoreResolver_EmptyConfigs(t *testing.T) {
	t.Run("handles empty tenant config", func(t *testing.T) {
		fullCfg := &ObjectStoreFullConfig{
			Defaults: *DefaultObjectStoreDefaults(),
			Tenants: map[string]*ObjectStoreTenantConfig{
				"empty": {},
			},
		}
		resolver := NewObjectStoreResolver(fullCfg, nil)

		resolved, err := resolver.ResolveConfig("empty", 0)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		// Should fall back to defaults
		if resolved.Compression != CompressionZstd {
			t.Errorf("expected default compression, got %s", resolved.Compression)
		}
	})

	t.Run("handles nil encryption in tenant", func(t *testing.T) {
		fullCfg := &ObjectStoreFullConfig{
			Defaults: ObjectStoreDefaults{
				PrimaryBackend: "local",
				Encryption: ObjectStoreEncryption{
					Enabled: true,
				},
			},
			Tenants: map[string]*ObjectStoreTenantConfig{
				"noenc": {
					Encryption: nil,
				},
			},
		}
		resolver := NewObjectStoreResolver(fullCfg, nil)

		enc, err := resolver.ResolveEncryption("noenc", 0)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		// Should use defaults
		if !enc.Enabled {
			t.Error("expected default encryption enabled")
		}
	})

	t.Run("handles empty shard configs", func(t *testing.T) {
		fullCfg := &ObjectStoreFullConfig{
			Defaults: *DefaultObjectStoreDefaults(),
			Tenants: map[string]*ObjectStoreTenantConfig{
				"noshards": {
					PrimaryBackend: "s3",
					ShardConfigs:   map[uint64]*ObjectStoreShardConfig{},
				},
			},
		}
		resolver := NewObjectStoreResolver(fullCfg, nil)

		resolved, err := resolver.ResolveConfig("noshards", 1)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		// Should use tenant config
		if resolved.PrimaryBackend != "s3" {
			t.Errorf("expected 's3', got %q", resolved.PrimaryBackend)
		}
	})
}

func TestObjectStoreResolver_InterfaceCompliance(t *testing.T) {
	// Verify ObjectStoreResolver implements ObjectStoreConfigResolver
	var _ ObjectStoreConfigResolver = (*ObjectStoreResolver)(nil)
}

func TestObjectStoreResolver_SetTenantQuotas(t *testing.T) {
	t.Run("sets quotas for new tenant", func(t *testing.T) {
		fullCfg := createTestFullConfig()
		resolver := NewObjectStoreResolver(fullCfg, nil)

		quotas := &TenantQuotas{
			MaxStorageBytes: 5 * 1024 * 1024 * 1024, // 5GB
			MaxObjectSize:   50 * 1024 * 1024,       // 50MB
			MaxObjectCount:  5000,
		}

		err := resolver.SetTenantQuotas(nil, "new-tenant", quotas)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		// Verify quotas were set
		cfg := resolver.GetTenantConfig("new-tenant")
		if cfg == nil {
			t.Fatal("expected tenant config to exist")
		}
		if cfg.Quotas.MaxStorageBytes != 5*1024*1024*1024 {
			t.Errorf("expected max storage 5GB, got %d", cfg.Quotas.MaxStorageBytes)
		}
		if cfg.Quotas.MaxObjectSize != 50*1024*1024 {
			t.Errorf("expected max object size 50MB, got %d", cfg.Quotas.MaxObjectSize)
		}
		if cfg.Quotas.MaxObjectCount != 5000 {
			t.Errorf("expected max object count 5000, got %d", cfg.Quotas.MaxObjectCount)
		}
	})

	t.Run("updates quotas for existing tenant", func(t *testing.T) {
		fullCfg := createTestFullConfig()
		resolver := NewObjectStoreResolver(fullCfg, nil)

		// tenant-a exists with quotas
		oldCfg := resolver.GetTenantConfig("tenant-a")
		if oldCfg == nil {
			t.Fatal("expected tenant-a to exist")
		}
		oldMaxStorage := oldCfg.Quotas.MaxStorageBytes

		// Update quotas
		newQuotas := &TenantQuotas{
			MaxStorageBytes: 20 * 1024 * 1024 * 1024, // 20GB
			MaxObjectSize:   200 * 1024 * 1024,       // 200MB
			MaxObjectCount:  20000,
		}

		err := resolver.SetTenantQuotas(nil, "tenant-a", newQuotas)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		// Verify quotas were updated
		cfg := resolver.GetTenantConfig("tenant-a")
		if cfg.Quotas.MaxStorageBytes == oldMaxStorage {
			t.Error("expected quotas to be updated")
		}
		if cfg.Quotas.MaxStorageBytes != 20*1024*1024*1024 {
			t.Errorf("expected max storage 20GB, got %d", cfg.Quotas.MaxStorageBytes)
		}
	})

	t.Run("sets zero quotas for unlimited", func(t *testing.T) {
		fullCfg := createTestFullConfig()
		resolver := NewObjectStoreResolver(fullCfg, nil)

		quotas := &TenantQuotas{
			MaxStorageBytes: 0, // unlimited
			MaxObjectSize:   0,
			MaxObjectCount:  0,
		}

		err := resolver.SetTenantQuotas(nil, "unlimited-tenant", quotas)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		cfg := resolver.GetTenantConfig("unlimited-tenant")
		if cfg == nil {
			t.Fatal("expected tenant config to exist")
		}
		if cfg.Quotas.MaxStorageBytes != 0 {
			t.Errorf("expected max storage 0 (unlimited), got %d", cfg.Quotas.MaxStorageBytes)
		}
	})

	t.Run("preserves other tenant config settings when updating quotas", func(t *testing.T) {
		fullCfg := createTestFullConfig()
		resolver := NewObjectStoreResolver(fullCfg, nil)

		// Get original config for tenant-a
		origCfg := resolver.GetTenantConfig("tenant-a")
		if origCfg == nil {
			t.Fatal("expected tenant-a to exist")
		}
		origPrimaryBackend := origCfg.PrimaryBackend

		// Update only quotas
		newQuotas := &TenantQuotas{
			MaxStorageBytes: 99999,
			MaxObjectSize:   88888,
			MaxObjectCount:  77777,
		}

		err := resolver.SetTenantQuotas(nil, "tenant-a", newQuotas)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		// Verify other settings are preserved
		cfg := resolver.GetTenantConfig("tenant-a")
		if cfg.PrimaryBackend != origPrimaryBackend {
			t.Errorf("expected primary backend %q to be preserved, got %q", origPrimaryBackend, cfg.PrimaryBackend)
		}
		// Verify quotas were updated
		if cfg.Quotas.MaxStorageBytes != 99999 {
			t.Errorf("expected max storage 99999, got %d", cfg.Quotas.MaxStorageBytes)
		}
	})

	t.Run("handles nil quotas gracefully", func(t *testing.T) {
		fullCfg := createTestFullConfig()
		resolver := NewObjectStoreResolver(fullCfg, nil)

		err := resolver.SetTenantQuotas(nil, "nil-quota-tenant", nil)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		cfg := resolver.GetTenantConfig("nil-quota-tenant")
		if cfg == nil {
			t.Fatal("expected tenant config to exist")
		}
		// Quotas should be zero values
		if cfg.Quotas.MaxStorageBytes != 0 {
			t.Errorf("expected max storage 0, got %d", cfg.Quotas.MaxStorageBytes)
		}
	})
}

func TestObjectStoreResolver_SetTenantQuotas_ConcurrentAccess(t *testing.T) {
	t.Run("handles concurrent quota updates", func(t *testing.T) {
		fullCfg := createTestFullConfig()
		resolver := NewObjectStoreResolver(fullCfg, nil)

		done := make(chan bool, 20)

		// Writers - update quotas concurrently
		for i := 0; i < 10; i++ {
			go func(idx int) {
				for j := 0; j < 50; j++ {
					quotas := &TenantQuotas{
						MaxStorageBytes: int64(idx * 1000),
						MaxObjectSize:   int64(idx * 100),
						MaxObjectCount:  int64(idx * 10),
					}
					_ = resolver.SetTenantQuotas(nil, "concurrent-tenant", quotas)
				}
				done <- true
			}(i)
		}

		// Readers - read quotas concurrently
		for i := 0; i < 10; i++ {
			go func() {
				for j := 0; j < 50; j++ {
					_ = resolver.GetTenantConfig("concurrent-tenant")
					usage := &TenantUsage{TotalBytes: 100, ObjectCount: 1}
					_ = resolver.CheckQuotas("concurrent-tenant", 100, usage)
				}
				done <- true
			}()
		}

		// Wait for all goroutines
		for i := 0; i < 20; i++ {
			<-done
		}
	})
}
