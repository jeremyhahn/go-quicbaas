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

// Package config provides Time Series configuration structures.
package config

import (
	"time"

	"github.com/jeremyhahn/go-quicbaas/pkg/api"
	"github.com/jeremyhahn/go-quicbaas/pkg/shards"
)

// TimeSeriesConfig contains time series storage configuration.
type TimeSeriesConfig struct {
	// Enabled controls whether the time series subsystem is active.
	Enabled bool `mapstructure:"enabled" json:"enabled" yaml:"enabled"`

	// ShardID is the dedicated Raft shard for time series data.
	ShardID uint64 `mapstructure:"shard_id" json:"shard_id" yaml:"shard_id"`

	// StoragePath is the directory for time series PebbleDB storage.
	// When empty the server derives a path from the main storage data directory.
	StoragePath string `mapstructure:"storage_path" json:"storage_path" yaml:"storage_path"`

	// Retention holds default retention policy settings.
	Retention TimeSeriesRetentionConfig `mapstructure:"retention" json:"retention" yaml:"retention"`

	// Query holds query-related limits and defaults.
	Query TimeSeriesQueryConfig `mapstructure:"query" json:"query" yaml:"query"`

	// Aggregation holds aggregation buffer configuration.
	Aggregation TimeSeriesAggregationConfig `mapstructure:"aggregation" json:"aggregation" yaml:"aggregation"`
}

// TimeSeriesRetentionConfig configures default retention behaviour.
type TimeSeriesRetentionConfig struct {
	// DefaultDays is the default number of days to retain data.
	DefaultDays int `mapstructure:"default_days" json:"default_days" yaml:"default_days"`

	// CheckInterval is how often the retention enforcer runs.
	CheckInterval time.Duration `mapstructure:"check_interval" json:"check_interval" yaml:"check_interval"`
}

// TimeSeriesQueryConfig configures query limits.
type TimeSeriesQueryConfig struct {
	// MaxPoints is the maximum number of data points a single query may return.
	MaxPoints int `mapstructure:"max_points" json:"max_points" yaml:"max_points"`

	// DefaultLimit is the default result limit when the caller does not specify one.
	DefaultLimit int `mapstructure:"default_limit" json:"default_limit" yaml:"default_limit"`
}

// TimeSeriesAggregationConfig configures aggregation buffering.
type TimeSeriesAggregationConfig struct {
	// BufferSize is the number of points buffered before flushing.
	BufferSize int `mapstructure:"buffer_size" json:"buffer_size" yaml:"buffer_size"`
}

// DefaultTimeSeriesConfig returns the default time series configuration.
func DefaultTimeSeriesConfig() TimeSeriesConfig {
	return TimeSeriesConfig{
		Enabled:     false,
		ShardID:     shards.PlatformShardID,
		StoragePath: "",
		Retention: TimeSeriesRetentionConfig{
			DefaultDays:   30,
			CheckInterval: time.Hour,
		},
		Query: TimeSeriesQueryConfig{
			MaxPoints:    100000,
			DefaultLimit: 10000,
		},
		Aggregation: TimeSeriesAggregationConfig{
			BufferSize: 1000,
		},
	}
}

// Validate validates the time series configuration.
func (c *TimeSeriesConfig) Validate() error {
	if !c.Enabled {
		return nil
	}

	if c.ShardID == 0 {
		return api.NewRequiredFieldError("timeseries.shard_id (must be non-zero)")
	}

	return nil
}
