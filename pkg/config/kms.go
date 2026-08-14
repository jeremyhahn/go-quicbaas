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

// Package config provides configuration types for QuicBaaS components.
package config

import (
	"time"

	kctypes "github.com/jeremyhahn/go-xkms/sdk/go"
)

// BackendType is an alias for go-xkms's BackendType.
// Applications embedding QuicBaaS should use these constants for type-safe
// backend configuration without needing to import go-xkms directly.
type BackendType = kctypes.BackendType

// Re-export go-xkms backend type variables for embedding applications.
// These provide type safety - no error-prone string literals.
// Note: These are var (not const) because the SDK re-exports them as var.
var (
	// BackendTypeSoftware uses go-xkms's software backend with KVStore persistence.
	BackendTypeSoftware = kctypes.BackendTypeSoftware
	// BackendTypePKCS11 uses PKCS#11 (HSM) for key management.
	BackendTypePKCS11 = kctypes.BackendTypePKCS11
	// BackendTypeTPM2 uses TPM 2.0 hardware for key protection.
	BackendTypeTPM2 = kctypes.BackendTypeTPM2
	// BackendTypeAWSKMS uses AWS Key Management Service.
	BackendTypeAWSKMS = kctypes.BackendTypeAWSKMS
	// BackendTypeGCPKMS uses Google Cloud KMS.
	BackendTypeGCPKMS = kctypes.BackendTypeGCPKMS
	// BackendTypeAzureKV uses Azure Key Vault.
	BackendTypeAzureKV = kctypes.BackendTypeAzureKV
	// BackendTypeVault uses HashiCorp Vault.
	BackendTypeVault = kctypes.BackendTypeVault
	// BackendTypeQuantum uses quantum-safe cryptography (ML-DSA, ML-KEM).
	BackendTypeQuantum = kctypes.BackendTypeQuantum
	// BackendTypeThreshold uses threshold cryptography (Shamir, threshold ECDSA).
	BackendTypeThreshold = kctypes.BackendTypeThreshold
	// BackendTypeSymmetric uses symmetric encryption (AES-GCM, ChaCha20-Poly1305).
	BackendTypeSymmetric = kctypes.BackendTypeSymmetric
)

// validBackendTypes is a set of all valid backend types for O(1) validation.
var validBackendTypes = map[BackendType]struct{}{
	BackendTypeSoftware:  {},
	BackendTypePKCS11:    {},
	BackendTypeTPM2:      {},
	BackendTypeAWSKMS:    {},
	BackendTypeGCPKMS:    {},
	BackendTypeAzureKV:   {},
	BackendTypeVault:     {},
	BackendTypeQuantum:   {},
	BackendTypeThreshold: {},
	BackendTypeSymmetric: {},
}

// backendValidator is a function that validates backend-specific configuration.
type backendValidator func(c *KMSConfig) error

// backendValidators maps backend types to their validation functions.
var backendValidators = map[BackendType]backendValidator{
	BackendTypeTPM2: func(c *KMSConfig) error {
		if c.TPM2.Device == "" {
			return &ValidationError{
				Field:   "kms.tpm2.device",
				Message: "TPM device path is required",
			}
		}
		return nil
	},
	BackendTypePKCS11: func(c *KMSConfig) error {
		if c.PKCS11.Library == "" {
			return &ValidationError{
				Field:   "kms.pkcs11.library",
				Message: "PKCS#11 library path is required",
			}
		}
		return nil
	},
	BackendTypeAWSKMS: func(c *KMSConfig) error {
		if c.AWSKMS.Region == "" {
			return &ValidationError{
				Field:   "kms.aws_kms.region",
				Message: "AWS region is required",
			}
		}
		if c.AWSKMS.KeyID == "" {
			return &ValidationError{
				Field:   "kms.aws_kms.key_id",
				Message: "AWS KMS key ID is required",
			}
		}
		return nil
	},
	BackendTypeAzureKV: func(c *KMSConfig) error {
		if c.AzureKV.VaultURL == "" {
			return &ValidationError{
				Field:   "kms.azure_kv.vault_url",
				Message: "Azure Key Vault URL is required",
			}
		}
		if c.AzureKV.KeyName == "" {
			return &ValidationError{
				Field:   "kms.azure_kv.key_name",
				Message: "Azure key name is required",
			}
		}
		return nil
	},
	BackendTypeGCPKMS: func(c *KMSConfig) error {
		if c.GCPKMS.ProjectID == "" {
			return &ValidationError{
				Field:   "kms.gcp_kms.project_id",
				Message: "GCP project ID is required",
			}
		}
		if c.GCPKMS.KeyRing == "" {
			return &ValidationError{
				Field:   "kms.gcp_kms.key_ring",
				Message: "GCP key ring is required",
			}
		}
		if c.GCPKMS.CryptoKey == "" {
			return &ValidationError{
				Field:   "kms.gcp_kms.crypto_key",
				Message: "GCP crypto key is required",
			}
		}
		return nil
	},
}

// KMSConfig contains all KMS-related configuration.
type KMSConfig struct {
	// Enabled controls whether KMS is enabled.
	Enabled bool `yaml:"enabled" json:"enabled" mapstructure:"enabled"`

	// Backend specifies the default/preferred KMS backend to use.
	// Options: software, pkcs11, tpm2, awskms, gcpkms, azurekv, vault, quantum, threshold, symmetric
	Backend BackendType `yaml:"backend" json:"backend" mapstructure:"backend"`

	// EnabledBackends is an explicit list of backend types to enable.
	// If empty, uses all backends available from go-xkms based on build tags.
	// This allows filtering to a specific subset of available backends.
	// Example: ["software", "awskms"] to enable only software and AWS KMS backends.
	EnabledBackends []BackendType `yaml:"enabled_backends" json:"enabled_backends" mapstructure:"enabled_backends"`

	// Raft contains Raft-specific settings for the KMS state machine.
	Raft KMSRaftConfig `yaml:"raft" json:"raft" mapstructure:"raft"`

	// Defaults contains default settings for key operations.
	Defaults KMSDefaultsConfig `yaml:"defaults" json:"defaults" mapstructure:"defaults"`

	// Safety contains AEAD safety threshold settings.
	Safety KMSSafetyConfig `yaml:"safety" json:"safety" mapstructure:"safety"`

	// Cache contains key caching settings.
	Cache KMSCacheConfig `yaml:"cache" json:"cache" mapstructure:"cache"`

	// Backend-specific configurations
	Software SoftwareBackendConfig `yaml:"software" json:"software" mapstructure:"software"`
	TPM2     TPM2BackendConfig     `yaml:"tpm2" json:"tpm2" mapstructure:"tpm2"`
	PKCS11   PKCS11BackendConfig   `yaml:"pkcs11" json:"pkcs11" mapstructure:"pkcs11"`
	AWSKMS   AWSKMSBackendConfig   `yaml:"aws_kms" json:"aws_kms" mapstructure:"aws_kms"`
	AzureKV  AzureKVBackendConfig  `yaml:"azure_kv" json:"azure_kv" mapstructure:"azure_kv"`
	GCPKMS   GCPKMSBackendConfig   `yaml:"gcp_kms" json:"gcp_kms" mapstructure:"gcp_kms"`
}

// KMSRaftConfig contains Raft-specific settings for KMS.
type KMSRaftConfig struct {
	// ShardID is the dedicated shard ID for the KMS state machine.
	ShardID uint64 `yaml:"shard_id" json:"shard_id" mapstructure:"shard_id"`
	// ReplicaCount is the number of replicas for the KMS shard.
	ReplicaCount int `yaml:"replica_count" json:"replica_count" mapstructure:"replica_count"`
	// Quorum is the minimum replicas required for key operations.
	Quorum int `yaml:"quorum" json:"quorum" mapstructure:"quorum"`
	// ElectionTimeout is the Raft election timeout.
	ElectionTimeout time.Duration `yaml:"election_timeout" json:"election_timeout" mapstructure:"election_timeout"`
	// HeartbeatInterval is the Raft heartbeat interval.
	HeartbeatInterval time.Duration `yaml:"heartbeat_interval" json:"heartbeat_interval" mapstructure:"heartbeat_interval"`
}

// KMSDefaultsConfig contains default settings for key operations.
type KMSDefaultsConfig struct {
	// Algorithm is the default encryption algorithm.
	Algorithm string `yaml:"algorithm" json:"algorithm"`
	// RotationPeriod is the automatic rotation period for keys.
	RotationPeriod time.Duration `yaml:"rotation_period" json:"rotation_period"`
}

// KMSSafetyConfig contains AEAD safety threshold settings.
type KMSSafetyConfig struct {
	// UsageWarning is the percentage of AEAD limit to trigger warning.
	UsageWarning int `yaml:"usage_warning" json:"usage_warning"`
	// AutoRotate is the percentage to trigger automatic rotation.
	AutoRotate int `yaml:"auto_rotate" json:"auto_rotate"`
	// CheckInterval is how often to check usage thresholds.
	CheckInterval time.Duration `yaml:"check_interval" json:"check_interval"`
}

// KMSCacheConfig contains key caching settings.
type KMSCacheConfig struct {
	// Enabled controls whether key caching is enabled.
	Enabled bool `yaml:"enabled" json:"enabled"`
	// TTL is the time-to-live for cached keys.
	TTL time.Duration `yaml:"ttl" json:"ttl"`
	// MaxKeys is the maximum number of keys to cache.
	MaxKeys int `yaml:"max_keys" json:"max_keys"`
}

// SoftwareBackendConfig contains configuration for the software backend.
type SoftwareBackendConfig struct {
	// KeyDir is the key storage directory (for file-based storage).
	// If empty, uses KVStore for persistence.
	KeyDir string `yaml:"key_dir" json:"key_dir"`
	// KeyPerms is the file permissions for stored keys.
	KeyPerms int `yaml:"key_perms" json:"key_perms"`
	// Encryption is the encryption algorithm for stored keys.
	Encryption string `yaml:"encryption" json:"encryption"`
}

// TPM2BackendConfig contains configuration for the TPM 2.0 backend.
type TPM2BackendConfig struct {
	// Device is the TPM device path.
	Device string `yaml:"device" json:"device"`
	// PCRBanks are the PCR banks for sealing.
	PCRBanks []int `yaml:"pcr_banks" json:"pcr_banks"`
	// OwnerAuth is the owner authorization password.
	OwnerAuth string `yaml:"owner_auth" json:"owner_auth"`
	// SRKHandle is the Storage Root Key handle.
	SRKHandle uint32 `yaml:"srk_handle" json:"srk_handle"`
}

// PKCS11BackendConfig contains configuration for the PKCS#11 backend.
type PKCS11BackendConfig struct {
	// Library is the PKCS#11 library path.
	Library string `yaml:"library" json:"library"`
	// TokenLabel is the token label.
	TokenLabel string `yaml:"token_label" json:"token_label"`
	// SlotID is the slot ID (alternative to label).
	SlotID int `yaml:"slot_id" json:"slot_id"`
	// PIN is the token PIN.
	PIN string `yaml:"pin" json:"pin"`
	// KeyLabelPrefix is the key label prefix.
	KeyLabelPrefix string `yaml:"key_label_prefix" json:"key_label_prefix"`
}

// AWSKMSBackendConfig contains configuration for AWS KMS.
type AWSKMSBackendConfig struct {
	// Region is the AWS region.
	Region string `yaml:"region" json:"region"`
	// KeyID is the KMS key ID or ARN.
	KeyID string `yaml:"key_id" json:"key_id"`
	// RoleARN is the role to assume (optional).
	RoleARN string `yaml:"role_arn" json:"role_arn"`
	// Endpoint is the endpoint override (for Floci and other emulators).
	Endpoint string `yaml:"endpoint" json:"endpoint"`
}

// AzureKVBackendConfig contains configuration for Azure Key Vault.
type AzureKVBackendConfig struct {
	// VaultURL is the Key Vault URL.
	VaultURL string `yaml:"vault_url" json:"vault_url"`
	// KeyName is the key name for sealing.
	KeyName string `yaml:"key_name" json:"key_name"`
	// KeyVersion is the key version (empty = latest).
	KeyVersion string `yaml:"key_version" json:"key_version"`
	// ClientID is the managed identity client ID (optional).
	ClientID string `yaml:"client_id" json:"client_id"`
}

// GCPKMSBackendConfig contains configuration for GCP Cloud KMS.
type GCPKMSBackendConfig struct {
	// ProjectID is the GCP project ID.
	ProjectID string `yaml:"project_id" json:"project_id"`
	// Location is the GCP location.
	Location string `yaml:"location" json:"location"`
	// KeyRing is the key ring name.
	KeyRing string `yaml:"key_ring" json:"key_ring"`
	// CryptoKey is the crypto key name for sealing.
	CryptoKey string `yaml:"crypto_key" json:"crypto_key"`
	// KeyVersion is the key version (optional).
	KeyVersion int `yaml:"key_version" json:"key_version"`
}

// DefaultKMSConfig returns the default KMS configuration.
func DefaultKMSConfig() KMSConfig {
	return KMSConfig{
		Enabled: false,
		Backend: BackendTypeSoftware,
		Raft: KMSRaftConfig{
			ShardID:           1000,
			ReplicaCount:      3,
			Quorum:            2,
			ElectionTimeout:   2 * time.Second,
			HeartbeatInterval: 200 * time.Millisecond,
		},
		Defaults: KMSDefaultsConfig{
			Algorithm:      "AES-256-GCM",
			RotationPeriod: 90 * 24 * time.Hour, // 90 days
		},
		Safety: KMSSafetyConfig{
			UsageWarning:  80,
			AutoRotate:    95,
			CheckInterval: time.Hour,
		},
		Cache: KMSCacheConfig{
			Enabled: true,
			TTL:     5 * time.Minute,
			MaxKeys: 100,
		},
		Software: SoftwareBackendConfig{
			KeyPerms:   0600,
			Encryption: "AES-256-GCM",
		},
		TPM2: TPM2BackendConfig{
			Device:    "/dev/tpmrm0",
			PCRBanks:  []int{0, 2, 4, 7},
			SRKHandle: 0x81000001,
		},
		PKCS11: PKCS11BackendConfig{
			SlotID:         0,
			KeyLabelPrefix: "quicbaas-",
		},
	}
}

// Validate validates the KMS configuration.
func (c *KMSConfig) Validate() error {
	if !c.Enabled {
		return nil
	}

	// Validate backend type using map-based lookup
	if _, ok := validBackendTypes[c.Backend]; !ok {
		return &ValidationError{
			Field:   "kms.backend",
			Message: "invalid backend type: " + string(c.Backend),
		}
	}

	// Validate backend-specific configuration using map-based dispatch
	if validate, ok := backendValidators[c.Backend]; ok {
		if err := validate(c); err != nil {
			return err
		}
	}

	// Validate safety thresholds
	if c.Safety.UsageWarning < 0 || c.Safety.UsageWarning > 100 {
		return &ValidationError{
			Field:   "kms.safety.usage_warning",
			Message: "usage warning must be between 0 and 100",
		}
	}
	if c.Safety.AutoRotate < 0 || c.Safety.AutoRotate > 100 {
		return &ValidationError{
			Field:   "kms.safety.auto_rotate",
			Message: "auto rotate must be between 0 and 100",
		}
	}
	if c.Safety.AutoRotate < c.Safety.UsageWarning {
		return &ValidationError{
			Field:   "kms.safety.auto_rotate",
			Message: "auto rotate threshold must be >= usage warning threshold",
		}
	}

	return nil
}
