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

import "time"

// SecretsConfig contains configuration for the secrets engine.
type SecretsConfig struct {
	// Enabled controls whether the secrets engine is enabled.
	Enabled bool `yaml:"enabled" json:"enabled" mapstructure:"enabled"`

	// Raft contains Raft-specific configuration for secrets replication.
	Raft SecretsRaftConfig `yaml:"raft" json:"raft" mapstructure:"raft"`

	// Encryption contains encryption settings for secrets.
	Encryption SecretsEncryptionConfig `yaml:"encryption" json:"encryption" mapstructure:"encryption"`

	// Versioning contains versioning settings.
	Versioning SecretsVersioningConfig `yaml:"versioning" json:"versioning" mapstructure:"versioning"`

	// SoftDelete contains soft delete settings.
	SoftDelete SecretsSoftDeleteConfig `yaml:"soft_delete" json:"soft_delete" mapstructure:"soft_delete"`
}

// SecretsRaftConfig contains Raft settings for secrets.
type SecretsRaftConfig struct {
	// ShardID is the dedicated shard ID for secrets storage.
	// Default: 1001 (separate from KMS shard 1000)
	ShardID uint64 `yaml:"shard_id" json:"shard_id" mapstructure:"shard_id"`

	// ReplicaCount is the number of replicas for secrets.
	ReplicaCount int `yaml:"replica_count" json:"replica_count" mapstructure:"replica_count"`
}

// SecretsEncryptionConfig contains encryption settings.
type SecretsEncryptionConfig struct {
	// DefaultKeyID is the default KMS key for encrypting secrets.
	// If empty, uses "quicbaas-secrets-key".
	DefaultKeyID string `yaml:"default_key_id" json:"default_key_id" mapstructure:"default_key_id"`
}

// SecretsVersioningConfig contains versioning settings.
type SecretsVersioningConfig struct {
	// Enabled controls whether versioning is enabled.
	Enabled bool `yaml:"enabled" json:"enabled" mapstructure:"enabled"`

	// MaxVersions is the maximum versions to retain (0 = unlimited).
	MaxVersions int `yaml:"max_versions" json:"max_versions" mapstructure:"max_versions"`
}

// SecretsSoftDeleteConfig contains soft delete settings.
type SecretsSoftDeleteConfig struct {
	// Enabled controls whether soft delete is enabled.
	Enabled bool `yaml:"enabled" json:"enabled" mapstructure:"enabled"`

	// RetentionDays is the retention period for soft-deleted secrets.
	RetentionDays int `yaml:"retention_days" json:"retention_days" mapstructure:"retention_days"`
}

// DefaultSecretsConfig returns the default secrets configuration.
func DefaultSecretsConfig() SecretsConfig {
	return SecretsConfig{
		Enabled: true,
		Raft: SecretsRaftConfig{
			ShardID:      1001, // Separate from KMS (1000) for ABAC isolation
			ReplicaCount: 3,
		},
		Encryption: SecretsEncryptionConfig{
			DefaultKeyID: "quicbaas-secrets-key",
		},
		Versioning: SecretsVersioningConfig{
			Enabled:     true,
			MaxVersions: 10,
		},
		SoftDelete: SecretsSoftDeleteConfig{
			Enabled:       true,
			RetentionDays: 30,
		},
	}
}

// Ensure time import is used.
var _ = time.Second
