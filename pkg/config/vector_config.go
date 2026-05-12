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

// Package config provides Vector Database configuration structures
package config

import (
	"path/filepath"
	"strconv"
	"time"

	"github.com/jeremyhahn/go-quicbaas/pkg/api"
)

// Default values applied by VectorConfig.ApplyDefaults when the operator has
// enabled the vector subsystem but supplied no HNSW block. These match the
// canonical values used by the in-tree lifecycle tests (see
// pkg/server/lifecycle_seams_test.go) so that exercising the env-var fast path
// (QUICBAAS_VECTOR_ENABLED=true) yields the same boot behavior as the tests.
//
// MaxElements is intentionally a conservative 10k: it is sized for first-run
// experimentation, not production. Operators with real workloads should supply
// an HNSW block in their config file with sizing tuned to their corpus.
const (
	defaultHNSWM              = 16
	defaultHNSWEfConstruction = 200
	defaultHNSWMaxElements    = 10000
	defaultHNSWMl             = 1.0
	defaultVectorShardID      = 200
	defaultVectorIndexType    = "hnsw"
	defaultVectorSubdir       = "vector"
)

// VectorConfig contains complete Vector Database configuration.
type VectorConfig struct {
	// Global vector database settings
	Enabled     bool   `mapstructure:"enabled" json:"enabled" yaml:"enabled"`
	ShardID     uint64 `mapstructure:"shard_id" json:"shard_id" yaml:"shard_id"`
	StoragePath string `mapstructure:"storage_path" json:"storage_path" yaml:"storage_path"`

	// DefaultIndexType specifies the default index type for new namespaces.
	// Valid values: "hnsw", "flat", "ivf_flat", "ivf_pq", "gpu_cagra", "gpu_ivf_flat", "gpu_ivf_pq", "gpu_brute_force"
	DefaultIndexType string `mapstructure:"default_index_type" json:"default_index_type" yaml:"default_index_type"`

	// HNSW index configuration
	HNSW *HNSWConfig `mapstructure:"hnsw" json:"hnsw" yaml:"hnsw"`

	// Flat index configuration
	Flat *FlatConfig `mapstructure:"flat" json:"flat" yaml:"flat"`

	// IVF index configuration
	IVF *IVFConfig `mapstructure:"ivf" json:"ivf" yaml:"ivf"`

	// GPU acceleration configuration
	GPU *GPUConfig `mapstructure:"gpu" json:"gpu" yaml:"gpu"`

	// Quantization configuration for vector compression.
	Quantization *QuantizationConfig `mapstructure:"quantization" json:"quantization" yaml:"quantization"`

	// Cache configuration
	CacheEnabled bool          `mapstructure:"cache_enabled" json:"cache_enabled" yaml:"cache_enabled"`
	CacheTTL     time.Duration `mapstructure:"cache_ttl" json:"cache_ttl" yaml:"cache_ttl"`
	CacheMaxSize int64         `mapstructure:"cache_max_size" json:"cache_max_size" yaml:"cache_max_size"`

	// FilterCacheSize is the maximum number of parsed filter expressions to cache.
	FilterCacheSize int `mapstructure:"filter_cache_size" json:"filter_cache_size" yaml:"filter_cache_size"`

	// ScalarIndexes defines scalar field indexes for accelerating metadata filtering.
	ScalarIndexes []ScalarIndexConfig `mapstructure:"scalar_indexes" json:"scalar_indexes" yaml:"scalar_indexes"`
}

// HNSWConfig configures the HNSW index parameters.
type HNSWConfig struct {
	M              int     `mapstructure:"m" json:"m" yaml:"m"`                                           // Max connections per node
	EfConstruction int     `mapstructure:"ef_construction" json:"ef_construction" yaml:"ef_construction"` // Build-time search parameter
	MaxElements    int     `mapstructure:"max_elements" json:"max_elements" yaml:"max_elements"`          // Maximum vectors in index
	Ml             float64 `mapstructure:"ml" json:"ml" yaml:"ml"`                                        // Layer multiplier
}

// FlatConfig configures the FLAT brute-force index parameters.
type FlatConfig struct {
	MaxElements int `mapstructure:"max_elements" json:"max_elements" yaml:"max_elements"` // Maximum vectors in index
}

// IVFConfig configures the IVF index parameters.
type IVFConfig struct {
	Nlist       int `mapstructure:"nlist" json:"nlist" yaml:"nlist"`                      // Number of clusters
	Nprobe      int `mapstructure:"nprobe" json:"nprobe" yaml:"nprobe"`                   // Default search probes
	MaxElements int `mapstructure:"max_elements" json:"max_elements" yaml:"max_elements"` // Maximum vectors in index
}

// GPUConfig configures GPU acceleration settings for vector indexes.
type GPUConfig struct {
	// DeviceID is the CUDA device ordinal to use.
	DeviceID int `mapstructure:"device_id" json:"device_id" yaml:"device_id"`

	// MemoryLimitBytes is the maximum GPU memory to use (0 = unlimited).
	MemoryLimitBytes int64 `mapstructure:"memory_limit_bytes" json:"memory_limit_bytes" yaml:"memory_limit_bytes"`

	// BuildBatchSize is the number of vectors to transfer per batch during index build.
	BuildBatchSize int `mapstructure:"build_batch_size" json:"build_batch_size" yaml:"build_batch_size"`

	// Enabled indicates whether GPU acceleration is active.
	Enabled bool `mapstructure:"enabled" json:"enabled" yaml:"enabled"`
}

// QuantizationConfig configures vector quantization for storage compression.
type QuantizationConfig struct {
	// Type is the quantization algorithm: "none", "sq8", "pq", "float16", "bfloat16".
	Type string `mapstructure:"type" json:"type" yaml:"type"`

	// CalibrationSize is the number of vectors used to calibrate SQ8 min/max ranges.
	// Only applicable when Type is "sq8". If zero, all vectors are used.
	CalibrationSize int `mapstructure:"calibration_size" json:"calibration_size" yaml:"calibration_size"`

	// M is the number of sub-quantizers for Product Quantization.
	// Only applicable when Type is "pq". Dimensions must be evenly divisible by M.
	M int `mapstructure:"m" json:"m" yaml:"m"`

	// Nbits is the bits per sub-quantizer code for Product Quantization (typically 8).
	// Only applicable when Type is "pq".
	Nbits int `mapstructure:"nbits" json:"nbits" yaml:"nbits"`

	// TrainingSize is the number of vectors used for PQ codebook training.
	// Only applicable when Type is "pq". If zero, all vectors are used.
	TrainingSize int `mapstructure:"training_size" json:"training_size" yaml:"training_size"`
}

// validQuantizationTypes maps valid quantization type strings to a boolean flag.
var validQuantizationTypes = map[string]bool{
	"none":     true,
	"sq8":      true,
	"pq":       true,
	"float16":  true,
	"bfloat16": true,
}

// ScalarIndexConfig describes a scalar field index for metadata filtering acceleration.
type ScalarIndexConfig struct {
	// Field is the metadata field name to index.
	Field string `mapstructure:"field" json:"field" yaml:"field"`
	// IndexType is the scalar index algorithm: "bitmap", "inverted", "sorted", or "auto".
	IndexType string `mapstructure:"index_type" json:"index_type" yaml:"index_type"`
}

// validScalarIndexTypes maps valid scalar index type strings to a boolean flag.
var validScalarIndexTypes = map[string]bool{
	"bitmap":   true,
	"inverted": true,
	"sorted":   true,
	"auto":     true,
}

// DefaultVectorConfig returns default Vector Database configuration.
func DefaultVectorConfig() *VectorConfig {
	return &VectorConfig{
		Enabled:          false, // Disabled by default
		ShardID:          defaultVectorShardID,
		StoragePath:      "./data/" + defaultVectorSubdir,
		DefaultIndexType: defaultVectorIndexType,
		HNSW: &HNSWConfig{
			M:              defaultHNSWM,
			EfConstruction: defaultHNSWEfConstruction,
			MaxElements:    1000000, // 1M vectors when explicitly requesting full defaults
			Ml:             defaultHNSWMl,
		},
		Flat: &FlatConfig{
			MaxElements: 100000, // 100K vectors default for flat
		},
		IVF: &IVFConfig{
			Nlist:       256,     // Default number of clusters
			Nprobe:      10,      // Default search probes
			MaxElements: 1000000, // 1M vectors default
		},
		GPU: &GPUConfig{
			DeviceID:         0,
			MemoryLimitBytes: 0, // unlimited
			BuildBatchSize:   100000,
			Enabled:          false,
		},
		CacheEnabled:    true,
		CacheTTL:        5 * time.Minute,
		CacheMaxSize:    1 * 1024 * 1024 * 1024, // 1GB
		FilterCacheSize: 1024,
	}
}

// ApplyDefaults populates missing required fields on an enabled VectorConfig
// so that the documented one-env-var enable path
// (QUICBAAS_VECTOR_ENABLED=true) produces a config that passes Validate.
//
// It only fills fields that are zero / nil — any operator-supplied values are
// preserved. When Enabled is false, ApplyDefaults is a no-op so disabled
// configs stay untouched. The function is idempotent and safe to call multiple
// times.
//
// storageDataDir is the daemon's storage data directory; if non-empty and
// StoragePath is unset, the vector subsystem is rooted at
// <storageDataDir>/vector so all on-disk state lives under the same root the
// operator pointed --data-dir at.
func (c *VectorConfig) ApplyDefaults(storageDataDir string) {
	if !c.Enabled {
		return
	}

	if c.ShardID == 0 {
		c.ShardID = defaultVectorShardID
	}

	if c.DefaultIndexType == "" {
		c.DefaultIndexType = defaultVectorIndexType
	}

	if c.StoragePath == "" {
		if storageDataDir != "" {
			c.StoragePath = filepath.Join(storageDataDir, defaultVectorSubdir)
		} else {
			c.StoragePath = "./data/" + defaultVectorSubdir
		}
	}

	if c.HNSW == nil {
		c.HNSW = &HNSWConfig{
			M:              defaultHNSWM,
			EfConstruction: defaultHNSWEfConstruction,
			MaxElements:    defaultHNSWMaxElements,
			Ml:             defaultHNSWMl,
		}
	}
}

// Validate validates the Vector Database configuration.
func (c *VectorConfig) Validate() error {
	if !c.Enabled {
		return nil // Skip validation if disabled
	}

	if c.ShardID == 0 {
		return api.NewRequiredFieldError("shard_id (must be non-zero)")
	}

	if c.StoragePath == "" {
		return api.NewRequiredFieldError("storage_path")
	}

	if c.HNSW == nil {
		return api.NewRequiredFieldError("hnsw configuration")
	}

	// Validate HNSW parameters
	if c.HNSW.M < 2 || c.HNSW.M > 100 {
		return api.NewIntRangeError("config.VectorConfig.Validate", "hnsw.m", c.HNSW.M, 2, 100)
	}

	if c.HNSW.EfConstruction < 100 {
		return api.NewMinimumValueError("config.VectorConfig.Validate", "hnsw.ef_construction", "100")
	}

	if c.HNSW.MaxElements < 1 {
		return api.NewPositiveIntRequiredError("config.VectorConfig.Validate", "hnsw.max_elements", c.HNSW.MaxElements)
	}

	if c.HNSW.Ml <= 0 {
		return api.NewPositiveFloatRequiredError("config.VectorConfig.Validate", "hnsw.ml", c.HNSW.Ml)
	}

	// Validate IVF parameters if configured
	if c.IVF != nil {
		if c.IVF.Nlist < 1 {
			return api.NewPositiveIntRequiredError("config.VectorConfig.Validate", "ivf.nlist", c.IVF.Nlist)
		}
		if c.IVF.Nprobe < 1 {
			return api.NewPositiveIntRequiredError("config.VectorConfig.Validate", "ivf.nprobe", c.IVF.Nprobe)
		}
	}

	// Validate GPU parameters if configured and enabled
	if c.GPU != nil && c.GPU.Enabled {
		if c.GPU.DeviceID < 0 {
			return api.NewIntRangeError("config.VectorConfig.Validate", "gpu.device_id", c.GPU.DeviceID, 0, 255)
		}
		if c.GPU.MemoryLimitBytes < 0 {
			return api.NewIntRangeError("config.VectorConfig.Validate", "gpu.memory_limit_bytes", int(c.GPU.MemoryLimitBytes), 0, int(^uint(0)>>1))
		}
		if c.GPU.BuildBatchSize < 1 {
			return api.NewPositiveIntRequiredError("config.VectorConfig.Validate", "gpu.build_batch_size", c.GPU.BuildBatchSize)
		}
	}

	// Validate quantization parameters if configured
	if c.Quantization != nil && c.Quantization.Type != "" && c.Quantization.Type != "none" {
		if !validQuantizationTypes[c.Quantization.Type] {
			return api.NewInvalidFormatMessageError(
				"quantization.type",
				"invalid quantization type: "+c.Quantization.Type+", valid types: none, sq8, pq, float16, bfloat16",
			)
		}

		if c.Quantization.Type == "pq" {
			if c.Quantization.M < 1 {
				return api.NewPositiveIntRequiredError("config.VectorConfig.Validate", "quantization.m", c.Quantization.M)
			}
			if c.Quantization.Nbits < 1 || c.Quantization.Nbits > 8 {
				return api.NewIntRangeError("config.VectorConfig.Validate", "quantization.nbits", c.Quantization.Nbits, 1, 8)
			}
		}
	}

	// Validate scalar index configurations
	for i, si := range c.ScalarIndexes {
		idxStr := strconv.Itoa(i)
		if si.Field == "" {
			return api.NewRequiredFieldError("scalar_indexes[" + idxStr + "].field")
		}
		if si.IndexType == "" {
			return api.NewRequiredFieldError("scalar_indexes[" + idxStr + "].index_type")
		}
		if !validScalarIndexTypes[si.IndexType] {
			return api.NewInvalidFormatMessageError(
				"scalar_indexes["+idxStr+"].index_type",
				"invalid scalar index type: "+si.IndexType+", valid types: bitmap, inverted, sorted, auto",
			)
		}
	}

	return nil
}
