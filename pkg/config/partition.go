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

// Package config provides configuration structures for QuicBaaS.
package config

// PartitionConfig defines a storage partition with its own data directory.
type PartitionConfig struct {
	// DataDir is the absolute path to this partition's data directory.
	DataDir string `mapstructure:"data_dir"`

	// WALDir is optional separate WAL directory (defaults to DataDir/wal).
	WALDir string `mapstructure:"wal_dir"`

	// CacheSize overrides the default cache size for this partition (bytes).
	CacheSize int64 `mapstructure:"cache_size"`
}

// PartitionsConfig maps partition names to their configurations.
type PartitionsConfig map[string]*PartitionConfig

// DefaultPartitionConfig returns a PartitionConfig with sensible defaults.
func DefaultPartitionConfig(dataDir string) *PartitionConfig {
	return &PartitionConfig{
		DataDir:   dataDir,
		WALDir:    "", // Will default to DataDir/wal
		CacheSize: 0,  // Will use default cache size
	}
}
