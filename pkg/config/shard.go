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

// ShardConfig defines a Raft shard and its partition mapping.
type ShardConfig struct {
	// ID is the Raft ShardID (must be > 0).
	ID uint64 `mapstructure:"id"`

	// Partition is the name of the partition this shard uses for storage.
	Partition string `mapstructure:"partition"`

	// StateMachine is the name of the state machine type (default: "fast").
	// Options: "fast", "vector", "minimal", or custom registered names.
	StateMachine string `mapstructure:"state_machine"`
}

// ShardsConfig is a list of shard configurations.
type ShardsConfig []*ShardConfig

// DefaultStateMachine is the default state machine type for shards.
const DefaultStateMachine = "fast"

// DefaultShardConfig returns a ShardConfig with sensible defaults.
func DefaultShardConfig(id uint64, partition string) *ShardConfig {
	return &ShardConfig{
		ID:           id,
		Partition:    partition,
		StateMachine: DefaultStateMachine,
	}
}
