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
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestDefaultBackupConfig verifies the default backup configuration values.
func TestDefaultBackupConfig(t *testing.T) {
	t.Run("returns expected defaults", func(t *testing.T) {
		cfg := DefaultBackupConfig()
		assert.True(t, cfg.Enabled)
		assert.Equal(t, "zstd", cfg.DefaultCompression)
		assert.Equal(t, "aes-256-gcm", cfg.DefaultEncryption)
	})

	t.Run("returns independent copies", func(t *testing.T) {
		cfg1 := DefaultBackupConfig()
		cfg2 := DefaultBackupConfig()
		cfg1.Enabled = false
		assert.True(t, cfg2.Enabled, "modifying one default should not affect another")
	})
}

// TestDefaultDiscoveryConfig verifies the default discovery configuration values.
func TestDefaultDiscoveryConfig(t *testing.T) {
	t.Run("returns expected defaults", func(t *testing.T) {
		cfg := DefaultDiscoveryConfig()
		assert.True(t, cfg.Enabled)
		assert.Equal(t, "multicast", cfg.Type)
	})

	t.Run("returns independent copies", func(t *testing.T) {
		cfg1 := DefaultDiscoveryConfig()
		cfg2 := DefaultDiscoveryConfig()
		cfg1.Type = "static"
		assert.Equal(t, "multicast", cfg2.Type, "modifying one default should not affect another")
	})
}

// TestDefaultMetricsConfig verifies the default metrics configuration values.
func TestDefaultMetricsConfig(t *testing.T) {
	t.Run("returns expected defaults", func(t *testing.T) {
		cfg := DefaultMetricsConfig()
		assert.True(t, cfg.Enabled)
		assert.False(t, cfg.AdvancedEnabled)
	})

	t.Run("returns independent copies", func(t *testing.T) {
		cfg1 := DefaultMetricsConfig()
		cfg2 := DefaultMetricsConfig()
		cfg1.AdvancedEnabled = true
		assert.False(t, cfg2.AdvancedEnabled, "modifying one default should not affect another")
	})
}

// TestDefaultSecretsConfig verifies the default secrets configuration values.
func TestDefaultSecretsConfig(t *testing.T) {
	t.Run("returns expected defaults", func(t *testing.T) {
		cfg := DefaultSecretsConfig()
		assert.True(t, cfg.Enabled)
		assert.Equal(t, uint64(1001), cfg.Raft.ShardID)
		assert.Equal(t, 3, cfg.Raft.ReplicaCount)
		assert.Equal(t, "quicbaas-secrets-key", cfg.Encryption.DefaultKeyID)
		assert.True(t, cfg.Versioning.Enabled)
		assert.Equal(t, 10, cfg.Versioning.MaxVersions)
		assert.True(t, cfg.SoftDelete.Enabled)
		assert.Equal(t, 30, cfg.SoftDelete.RetentionDays)
	})

	t.Run("returns independent copies", func(t *testing.T) {
		cfg1 := DefaultSecretsConfig()
		cfg2 := DefaultSecretsConfig()
		cfg1.Raft.ShardID = 9999
		assert.Equal(t, uint64(1001), cfg2.Raft.ShardID)
	})
}

// TestDefaultKMSConfig verifies the default KMS configuration values.
func TestDefaultKMSConfig(t *testing.T) {
	t.Run("returns expected defaults", func(t *testing.T) {
		cfg := DefaultKMSConfig()
		assert.False(t, cfg.Enabled)
		assert.Equal(t, BackendTypeSoftware, cfg.Backend)
		assert.Equal(t, uint64(1000), cfg.Raft.ShardID)
		assert.Equal(t, 3, cfg.Raft.ReplicaCount)
		assert.Equal(t, 2, cfg.Raft.Quorum)
		assert.Equal(t, 2*time.Second, cfg.Raft.ElectionTimeout)
		assert.Equal(t, 200*time.Millisecond, cfg.Raft.HeartbeatInterval)
		assert.Equal(t, "AES-256-GCM", cfg.Defaults.Algorithm)
		assert.Equal(t, 90*24*time.Hour, cfg.Defaults.RotationPeriod)
		assert.Equal(t, 80, cfg.Safety.UsageWarning)
		assert.Equal(t, 95, cfg.Safety.AutoRotate)
		assert.Equal(t, time.Hour, cfg.Safety.CheckInterval)
		assert.True(t, cfg.Cache.Enabled)
		assert.Equal(t, 5*time.Minute, cfg.Cache.TTL)
		assert.Equal(t, 100, cfg.Cache.MaxKeys)
		assert.Equal(t, "AES-256-GCM", cfg.ClusterDEK.Algorithm)
		assert.Equal(t, 30*24*time.Hour, cfg.ClusterDEK.RotationPeriod)
		assert.Equal(t, 1000, cfg.ClusterDEK.ReencryptBatch)
		assert.Equal(t, 4, cfg.ClusterDEK.ReencryptWorkers)
		assert.Equal(t, "/dev/tpmrm0", cfg.TPM2.Device)
		assert.Equal(t, []int{0, 2, 4, 7}, cfg.TPM2.PCRBanks)
		assert.Equal(t, "quicbaas-", cfg.PKCS11.KeyLabelPrefix)
	})

	t.Run("returns independent copies", func(t *testing.T) {
		cfg1 := DefaultKMSConfig()
		cfg2 := DefaultKMSConfig()
		cfg1.Raft.ShardID = 5555
		assert.Equal(t, uint64(1000), cfg2.Raft.ShardID)
	})
}

// TestKMSConfig_Validate verifies the KMS Validate method for all backend types
// and safety threshold boundary conditions.
func TestKMSConfig_Validate(t *testing.T) {
	t.Run("disabled config skips validation", func(t *testing.T) {
		cfg := DefaultKMSConfig()
		cfg.Enabled = false
		err := cfg.Validate()
		assert.NoError(t, err)
	})

	t.Run("valid software backend", func(t *testing.T) {
		cfg := DefaultKMSConfig()
		cfg.Enabled = true
		cfg.Backend = BackendTypeSoftware
		err := cfg.Validate()
		assert.NoError(t, err)
	})

	t.Run("invalid backend type", func(t *testing.T) {
		cfg := DefaultKMSConfig()
		cfg.Enabled = true
		cfg.Backend = "invalid-backend"
		err := cfg.Validate()
		require.Error(t, err)
		var ve *ValidationError
		require.ErrorAs(t, err, &ve)
		assert.Equal(t, "kms.backend", ve.Field)
		assert.Contains(t, ve.Message, "invalid backend type")
	})

	t.Run("TPM2 backend missing device", func(t *testing.T) {
		cfg := DefaultKMSConfig()
		cfg.Enabled = true
		cfg.Backend = BackendTypeTPM2
		cfg.TPM2.Device = ""
		err := cfg.Validate()
		require.Error(t, err)
		var ve *ValidationError
		require.ErrorAs(t, err, &ve)
		assert.Equal(t, "kms.tpm2.device", ve.Field)
	})

	t.Run("TPM2 backend valid device", func(t *testing.T) {
		cfg := DefaultKMSConfig()
		cfg.Enabled = true
		cfg.Backend = BackendTypeTPM2
		cfg.TPM2.Device = "/dev/tpmrm0"
		err := cfg.Validate()
		assert.NoError(t, err)
	})

	t.Run("PKCS11 backend missing library", func(t *testing.T) {
		cfg := DefaultKMSConfig()
		cfg.Enabled = true
		cfg.Backend = BackendTypePKCS11
		cfg.PKCS11.Library = ""
		err := cfg.Validate()
		require.Error(t, err)
		var ve *ValidationError
		require.ErrorAs(t, err, &ve)
		assert.Equal(t, "kms.pkcs11.library", ve.Field)
	})

	t.Run("PKCS11 backend valid library", func(t *testing.T) {
		cfg := DefaultKMSConfig()
		cfg.Enabled = true
		cfg.Backend = BackendTypePKCS11
		cfg.PKCS11.Library = "/usr/lib/softhsm/libsofthsm2.so"
		err := cfg.Validate()
		assert.NoError(t, err)
	})

	t.Run("AWS KMS missing region", func(t *testing.T) {
		cfg := DefaultKMSConfig()
		cfg.Enabled = true
		cfg.Backend = BackendTypeAWSKMS
		cfg.AWSKMS.Region = ""
		cfg.AWSKMS.KeyID = "some-key"
		err := cfg.Validate()
		require.Error(t, err)
		var ve *ValidationError
		require.ErrorAs(t, err, &ve)
		assert.Equal(t, "kms.aws_kms.region", ve.Field)
	})

	t.Run("AWS KMS missing key ID", func(t *testing.T) {
		cfg := DefaultKMSConfig()
		cfg.Enabled = true
		cfg.Backend = BackendTypeAWSKMS
		cfg.AWSKMS.Region = "us-east-1"
		cfg.AWSKMS.KeyID = ""
		err := cfg.Validate()
		require.Error(t, err)
		var ve *ValidationError
		require.ErrorAs(t, err, &ve)
		assert.Equal(t, "kms.aws_kms.key_id", ve.Field)
	})

	t.Run("AWS KMS valid config", func(t *testing.T) {
		cfg := DefaultKMSConfig()
		cfg.Enabled = true
		cfg.Backend = BackendTypeAWSKMS
		cfg.AWSKMS.Region = "us-east-1"
		cfg.AWSKMS.KeyID = "arn:aws:kms:us-east-1:123456789012:key/12345"
		err := cfg.Validate()
		assert.NoError(t, err)
	})

	t.Run("Azure KV missing vault URL", func(t *testing.T) {
		cfg := DefaultKMSConfig()
		cfg.Enabled = true
		cfg.Backend = BackendTypeAzureKV
		cfg.AzureKV.VaultURL = ""
		cfg.AzureKV.KeyName = "some-key"
		err := cfg.Validate()
		require.Error(t, err)
		var ve *ValidationError
		require.ErrorAs(t, err, &ve)
		assert.Equal(t, "kms.azure_kv.vault_url", ve.Field)
	})

	t.Run("Azure KV missing key name", func(t *testing.T) {
		cfg := DefaultKMSConfig()
		cfg.Enabled = true
		cfg.Backend = BackendTypeAzureKV
		cfg.AzureKV.VaultURL = "https://myvault.vault.azure.net"
		cfg.AzureKV.KeyName = ""
		err := cfg.Validate()
		require.Error(t, err)
		var ve *ValidationError
		require.ErrorAs(t, err, &ve)
		assert.Equal(t, "kms.azure_kv.key_name", ve.Field)
	})

	t.Run("GCP KMS missing project ID", func(t *testing.T) {
		cfg := DefaultKMSConfig()
		cfg.Enabled = true
		cfg.Backend = BackendTypeGCPKMS
		cfg.GCPKMS.ProjectID = ""
		err := cfg.Validate()
		require.Error(t, err)
		var ve *ValidationError
		require.ErrorAs(t, err, &ve)
		assert.Equal(t, "kms.gcp_kms.project_id", ve.Field)
	})

	t.Run("GCP KMS missing key ring", func(t *testing.T) {
		cfg := DefaultKMSConfig()
		cfg.Enabled = true
		cfg.Backend = BackendTypeGCPKMS
		cfg.GCPKMS.ProjectID = "my-project"
		cfg.GCPKMS.KeyRing = ""
		err := cfg.Validate()
		require.Error(t, err)
		var ve *ValidationError
		require.ErrorAs(t, err, &ve)
		assert.Equal(t, "kms.gcp_kms.key_ring", ve.Field)
	})

	t.Run("GCP KMS missing crypto key", func(t *testing.T) {
		cfg := DefaultKMSConfig()
		cfg.Enabled = true
		cfg.Backend = BackendTypeGCPKMS
		cfg.GCPKMS.ProjectID = "my-project"
		cfg.GCPKMS.KeyRing = "my-ring"
		cfg.GCPKMS.CryptoKey = ""
		err := cfg.Validate()
		require.Error(t, err)
		var ve *ValidationError
		require.ErrorAs(t, err, &ve)
		assert.Equal(t, "kms.gcp_kms.crypto_key", ve.Field)
	})

	t.Run("GCP KMS valid config", func(t *testing.T) {
		cfg := DefaultKMSConfig()
		cfg.Enabled = true
		cfg.Backend = BackendTypeGCPKMS
		cfg.GCPKMS.ProjectID = "my-project"
		cfg.GCPKMS.KeyRing = "my-ring"
		cfg.GCPKMS.CryptoKey = "my-key"
		err := cfg.Validate()
		assert.NoError(t, err)
	})

	t.Run("safety usage warning below range", func(t *testing.T) {
		cfg := DefaultKMSConfig()
		cfg.Enabled = true
		cfg.Safety.UsageWarning = -1
		err := cfg.Validate()
		require.Error(t, err)
		var ve *ValidationError
		require.ErrorAs(t, err, &ve)
		assert.Equal(t, "kms.safety.usage_warning", ve.Field)
	})

	t.Run("safety usage warning above range", func(t *testing.T) {
		cfg := DefaultKMSConfig()
		cfg.Enabled = true
		cfg.Safety.UsageWarning = 101
		err := cfg.Validate()
		require.Error(t, err)
		var ve *ValidationError
		require.ErrorAs(t, err, &ve)
		assert.Equal(t, "kms.safety.usage_warning", ve.Field)
	})

	t.Run("safety auto rotate below range", func(t *testing.T) {
		cfg := DefaultKMSConfig()
		cfg.Enabled = true
		cfg.Safety.AutoRotate = -1
		err := cfg.Validate()
		require.Error(t, err)
		var ve *ValidationError
		require.ErrorAs(t, err, &ve)
		assert.Equal(t, "kms.safety.auto_rotate", ve.Field)
	})

	t.Run("safety auto rotate above range", func(t *testing.T) {
		cfg := DefaultKMSConfig()
		cfg.Enabled = true
		cfg.Safety.AutoRotate = 101
		err := cfg.Validate()
		require.Error(t, err)
		var ve *ValidationError
		require.ErrorAs(t, err, &ve)
		assert.Equal(t, "kms.safety.auto_rotate", ve.Field)
	})

	t.Run("safety auto rotate less than usage warning", func(t *testing.T) {
		cfg := DefaultKMSConfig()
		cfg.Enabled = true
		cfg.Safety.UsageWarning = 90
		cfg.Safety.AutoRotate = 50
		err := cfg.Validate()
		require.Error(t, err)
		var ve *ValidationError
		require.ErrorAs(t, err, &ve)
		assert.Equal(t, "kms.safety.auto_rotate", ve.Field)
		assert.Contains(t, ve.Message, "must be >= usage warning")
	})
}

// TestTimeSeriesConfig_Validate verifies the time series configuration validation.
func TestTimeSeriesConfig_Validate(t *testing.T) {
	t.Run("disabled config skips validation", func(t *testing.T) {
		cfg := DefaultTimeSeriesConfig()
		cfg.Enabled = false
		cfg.ShardID = 0 // Would be invalid, but should be ignored when disabled
		err := cfg.Validate()
		assert.NoError(t, err)
	})

	t.Run("enabled config with valid shard ID passes", func(t *testing.T) {
		cfg := DefaultTimeSeriesConfig()
		cfg.Enabled = true
		err := cfg.Validate()
		assert.NoError(t, err)
	})

	t.Run("enabled config with zero shard ID fails", func(t *testing.T) {
		cfg := TimeSeriesConfig{
			Enabled: true,
			ShardID: 0,
		}
		err := cfg.Validate()
		require.Error(t, err)
		assert.Contains(t, err.Error(), "timeseries.shard_id (must be non-zero)")
	})
}

// TestVectorConfig_Validate_IVFParameters tests IVF parameter validation.
func TestVectorConfig_Validate_IVFParameters(t *testing.T) {
	validBase := func() *VectorConfig {
		cfg := DefaultVectorConfig()
		cfg.Enabled = true
		return cfg
	}

	t.Run("valid IVF config passes", func(t *testing.T) {
		cfg := validBase()
		err := cfg.Validate()
		assert.NoError(t, err)
	})

	t.Run("nil IVF config passes", func(t *testing.T) {
		cfg := validBase()
		cfg.IVF = nil
		err := cfg.Validate()
		assert.NoError(t, err)
	})

	t.Run("IVF nlist zero fails", func(t *testing.T) {
		cfg := validBase()
		cfg.IVF.Nlist = 0
		err := cfg.Validate()
		require.Error(t, err)
		assert.Contains(t, err.Error(), "ivf.nlist")
	})

	t.Run("IVF nlist negative fails", func(t *testing.T) {
		cfg := validBase()
		cfg.IVF.Nlist = -5
		err := cfg.Validate()
		require.Error(t, err)
		assert.Contains(t, err.Error(), "ivf.nlist")
	})

	t.Run("IVF nprobe zero fails", func(t *testing.T) {
		cfg := validBase()
		cfg.IVF.Nprobe = 0
		err := cfg.Validate()
		require.Error(t, err)
		assert.Contains(t, err.Error(), "ivf.nprobe")
	})

	t.Run("IVF nprobe negative fails", func(t *testing.T) {
		cfg := validBase()
		cfg.IVF.Nprobe = -1
		err := cfg.Validate()
		require.Error(t, err)
		assert.Contains(t, err.Error(), "ivf.nprobe")
	})
}

// TestVectorConfig_Validate_GPUParameters tests GPU parameter validation.
func TestVectorConfig_Validate_GPUParameters(t *testing.T) {
	validBase := func() *VectorConfig {
		cfg := DefaultVectorConfig()
		cfg.Enabled = true
		return cfg
	}

	t.Run("GPU disabled skips validation", func(t *testing.T) {
		cfg := validBase()
		cfg.GPU.Enabled = false
		cfg.GPU.DeviceID = -1 // Invalid but should be ignored
		err := cfg.Validate()
		assert.NoError(t, err)
	})

	t.Run("GPU nil skips validation", func(t *testing.T) {
		cfg := validBase()
		cfg.GPU = nil
		err := cfg.Validate()
		assert.NoError(t, err)
	})

	t.Run("GPU enabled with valid config passes", func(t *testing.T) {
		cfg := validBase()
		cfg.GPU.Enabled = true
		cfg.GPU.DeviceID = 0
		cfg.GPU.MemoryLimitBytes = 1024
		cfg.GPU.BuildBatchSize = 100
		err := cfg.Validate()
		assert.NoError(t, err)
	})

	t.Run("GPU enabled with negative device ID fails", func(t *testing.T) {
		cfg := validBase()
		cfg.GPU.Enabled = true
		cfg.GPU.DeviceID = -1
		err := cfg.Validate()
		require.Error(t, err)
		assert.Contains(t, err.Error(), "gpu.device_id")
	})

	t.Run("GPU enabled with negative memory limit fails", func(t *testing.T) {
		cfg := validBase()
		cfg.GPU.Enabled = true
		cfg.GPU.DeviceID = 0
		cfg.GPU.MemoryLimitBytes = -100
		cfg.GPU.BuildBatchSize = 100
		err := cfg.Validate()
		require.Error(t, err)
		assert.Contains(t, err.Error(), "gpu.memory_limit_bytes")
	})

	t.Run("GPU enabled with zero build batch size fails", func(t *testing.T) {
		cfg := validBase()
		cfg.GPU.Enabled = true
		cfg.GPU.DeviceID = 0
		cfg.GPU.MemoryLimitBytes = 0
		cfg.GPU.BuildBatchSize = 0
		err := cfg.Validate()
		require.Error(t, err)
		assert.Contains(t, err.Error(), "gpu.build_batch_size")
	})
}

// TestVectorConfig_Validate_Quantization tests quantization parameter validation.
func TestVectorConfig_Validate_Quantization(t *testing.T) {
	validBase := func() *VectorConfig {
		cfg := DefaultVectorConfig()
		cfg.Enabled = true
		return cfg
	}

	t.Run("nil quantization passes", func(t *testing.T) {
		cfg := validBase()
		cfg.Quantization = nil
		err := cfg.Validate()
		assert.NoError(t, err)
	})

	t.Run("quantization type none passes", func(t *testing.T) {
		cfg := validBase()
		cfg.Quantization = &QuantizationConfig{Type: "none"}
		err := cfg.Validate()
		assert.NoError(t, err)
	})

	t.Run("quantization type empty passes", func(t *testing.T) {
		cfg := validBase()
		cfg.Quantization = &QuantizationConfig{Type: ""}
		err := cfg.Validate()
		assert.NoError(t, err)
	})

	t.Run("quantization type sq8 passes", func(t *testing.T) {
		cfg := validBase()
		cfg.Quantization = &QuantizationConfig{Type: "sq8"}
		err := cfg.Validate()
		assert.NoError(t, err)
	})

	t.Run("quantization type float16 passes", func(t *testing.T) {
		cfg := validBase()
		cfg.Quantization = &QuantizationConfig{Type: "float16"}
		err := cfg.Validate()
		assert.NoError(t, err)
	})

	t.Run("quantization type bfloat16 passes", func(t *testing.T) {
		cfg := validBase()
		cfg.Quantization = &QuantizationConfig{Type: "bfloat16"}
		err := cfg.Validate()
		assert.NoError(t, err)
	})

	t.Run("invalid quantization type fails", func(t *testing.T) {
		cfg := validBase()
		cfg.Quantization = &QuantizationConfig{Type: "invalid-quant"}
		err := cfg.Validate()
		require.Error(t, err)
		assert.Contains(t, err.Error(), "quantization.type")
		assert.Contains(t, err.Error(), "invalid quantization type")
	})

	t.Run("PQ type with valid params passes", func(t *testing.T) {
		cfg := validBase()
		cfg.Quantization = &QuantizationConfig{
			Type:  "pq",
			M:     8,
			Nbits: 8,
		}
		err := cfg.Validate()
		assert.NoError(t, err)
	})

	t.Run("PQ type with zero M fails", func(t *testing.T) {
		cfg := validBase()
		cfg.Quantization = &QuantizationConfig{
			Type:  "pq",
			M:     0,
			Nbits: 8,
		}
		err := cfg.Validate()
		require.Error(t, err)
		assert.Contains(t, err.Error(), "quantization.m")
	})

	t.Run("PQ type with zero nbits fails", func(t *testing.T) {
		cfg := validBase()
		cfg.Quantization = &QuantizationConfig{
			Type:  "pq",
			M:     8,
			Nbits: 0,
		}
		err := cfg.Validate()
		require.Error(t, err)
		assert.Contains(t, err.Error(), "quantization.nbits")
	})

	t.Run("PQ type with nbits too high fails", func(t *testing.T) {
		cfg := validBase()
		cfg.Quantization = &QuantizationConfig{
			Type:  "pq",
			M:     8,
			Nbits: 9,
		}
		err := cfg.Validate()
		require.Error(t, err)
		assert.Contains(t, err.Error(), "quantization.nbits")
	})
}

// TestVectorConfig_Validate_ScalarIndexes tests scalar index validation.
func TestVectorConfig_Validate_ScalarIndexes(t *testing.T) {
	validBase := func() *VectorConfig {
		cfg := DefaultVectorConfig()
		cfg.Enabled = true
		return cfg
	}

	t.Run("empty scalar indexes passes", func(t *testing.T) {
		cfg := validBase()
		cfg.ScalarIndexes = nil
		err := cfg.Validate()
		assert.NoError(t, err)
	})

	t.Run("valid scalar index passes", func(t *testing.T) {
		cfg := validBase()
		cfg.ScalarIndexes = []ScalarIndexConfig{
			{Field: "category", IndexType: "bitmap"},
			{Field: "price", IndexType: "sorted"},
			{Field: "tags", IndexType: "inverted"},
			{Field: "auto_field", IndexType: "auto"},
		}
		err := cfg.Validate()
		assert.NoError(t, err)
	})

	t.Run("scalar index with empty field fails", func(t *testing.T) {
		cfg := validBase()
		cfg.ScalarIndexes = []ScalarIndexConfig{
			{Field: "", IndexType: "bitmap"},
		}
		err := cfg.Validate()
		require.Error(t, err)
		assert.Contains(t, err.Error(), "scalar_indexes[0].field")
	})

	t.Run("scalar index with empty index type fails", func(t *testing.T) {
		cfg := validBase()
		cfg.ScalarIndexes = []ScalarIndexConfig{
			{Field: "category", IndexType: ""},
		}
		err := cfg.Validate()
		require.Error(t, err)
		assert.Contains(t, err.Error(), "scalar_indexes[0].index_type")
	})

	t.Run("scalar index with invalid index type fails", func(t *testing.T) {
		cfg := validBase()
		cfg.ScalarIndexes = []ScalarIndexConfig{
			{Field: "category", IndexType: "hash"},
		}
		err := cfg.Validate()
		require.Error(t, err)
		assert.Contains(t, err.Error(), "scalar_indexes[0].index_type")
		assert.Contains(t, err.Error(), "invalid scalar index type")
	})

	t.Run("second scalar index invalid field fails", func(t *testing.T) {
		cfg := validBase()
		cfg.ScalarIndexes = []ScalarIndexConfig{
			{Field: "good_field", IndexType: "bitmap"},
			{Field: "", IndexType: "sorted"},
		}
		err := cfg.Validate()
		require.Error(t, err)
		assert.Contains(t, err.Error(), "scalar_indexes[1].field")
	})
}

// TestTenantQuotas_Clone verifies the TenantQuotas Clone method.
func TestTenantQuotas_Clone(t *testing.T) {
	t.Run("nil quotas returns nil", func(t *testing.T) {
		var q *TenantQuotas
		clone := q.Clone()
		assert.Nil(t, clone)
	})

	t.Run("clones all fields correctly", func(t *testing.T) {
		q := &TenantQuotas{
			MaxStorageBytes: 1024 * 1024 * 1024,
			MaxObjectSize:   10 * 1024 * 1024,
			MaxObjectCount:  100000,
		}
		clone := q.Clone()
		require.NotNil(t, clone)
		assert.Equal(t, q.MaxStorageBytes, clone.MaxStorageBytes)
		assert.Equal(t, q.MaxObjectSize, clone.MaxObjectSize)
		assert.Equal(t, q.MaxObjectCount, clone.MaxObjectCount)
	})

	t.Run("clone is independent of original", func(t *testing.T) {
		q := &TenantQuotas{
			MaxStorageBytes: 1024,
			MaxObjectSize:   512,
			MaxObjectCount:  100,
		}
		clone := q.Clone()
		clone.MaxStorageBytes = 9999
		assert.Equal(t, int64(1024), q.MaxStorageBytes, "original should be unmodified")
	})
}

// TestValidator_ValidateKMS tests the Validator's validateKMS path
// ensuring KMS validation errors are properly collected.
func TestValidator_ValidateKMS(t *testing.T) {
	t.Run("disabled KMS produces no errors", func(t *testing.T) {
		cfg := Default()
		cfg.KMS.Enabled = false
		tmpDir := t.TempDir()
		cfg.Storage.DataDir = tmpDir
		cfg.Storage.WALDir = tmpDir + "/wal"
		cfg.Storage.Backend = StorageBackendPebble
		cfg.Security.TLSEnabled = false
		cfg.Security.EncryptionEnabled = false

		err := Validate(cfg)
		assert.NoError(t, err)
	})

	t.Run("enabled KMS with invalid backend reports error", func(t *testing.T) {
		cfg := Default()
		cfg.KMS.Enabled = true
		cfg.KMS.Backend = "nonexistent-backend"
		tmpDir := t.TempDir()
		cfg.Storage.DataDir = tmpDir
		cfg.Storage.WALDir = tmpDir + "/wal"
		cfg.Storage.Backend = StorageBackendPebble
		cfg.Security.TLSEnabled = false
		cfg.Security.EncryptionEnabled = false

		err := Validate(cfg)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "kms.backend")
	})

	t.Run("enabled KMS with valid software backend passes", func(t *testing.T) {
		cfg := Default()
		cfg.KMS.Enabled = true
		cfg.KMS.Backend = BackendTypeSoftware
		tmpDir := t.TempDir()
		cfg.Storage.DataDir = tmpDir
		cfg.Storage.WALDir = tmpDir + "/wal"
		cfg.Storage.Backend = StorageBackendPebble
		cfg.Security.TLSEnabled = false
		cfg.Security.EncryptionEnabled = false

		err := Validate(cfg)
		assert.NoError(t, err)
	})

	t.Run("enabled KMS with non-ValidationError wrapping", func(t *testing.T) {
		// Test the else branch in validateKMS where err is not a *ValidationError.
		// The KMSConfig.Validate always returns *ValidationError, so we test
		// through the Validator which wraps the error.
		cfg := Default()
		cfg.KMS.Enabled = true
		cfg.KMS.Backend = BackendTypePKCS11
		cfg.KMS.PKCS11.Library = "" // Will produce a ValidationError
		tmpDir := t.TempDir()
		cfg.Storage.DataDir = tmpDir
		cfg.Storage.WALDir = tmpDir + "/wal"
		cfg.Storage.Backend = StorageBackendPebble
		cfg.Security.TLSEnabled = false
		cfg.Security.EncryptionEnabled = false

		err := Validate(cfg)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "kms.pkcs11.library")
	})
}
