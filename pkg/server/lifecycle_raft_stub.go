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

package server

import (
	"context"
	"strconv"
)

// validateRouting validates that all entity shard pins reference shards
// that exist in the configured shard list.
func validateRouting(shards []uint64, pins map[string]uint64) error {
	if len(pins) == 0 {
		return nil
	}
	shardSet := make(map[uint64]struct{}, len(shards))
	for _, s := range shards {
		shardSet[s] = struct{}{}
	}
	for entityType, shardID := range pins {
		if _, ok := shardSet[shardID]; !ok {
			return &ServerMessageError{
				Message: "entity " + entityType + " pinned to shard " +
					strconv.FormatUint(shardID, 10) + " which is not in configured shards",
			}
		}
	}
	return nil
}

// kvShards filters out specialized shards from the given shard list.
// The excludeShards parameter contains shard IDs to exclude (vector, timeseries, kms, etc).
func kvShards(shards []uint64, excludeShards ...uint64) []uint64 {
	if len(shards) == 0 {
		return []uint64{1}
	}
	excludeSet := make(map[uint64]struct{}, len(excludeShards))
	for _, id := range excludeShards {
		if id != 0 {
			excludeSet[id] = struct{}{}
		}
	}
	result := make([]uint64, 0, len(shards))
	for _, s := range shards {
		if _, excluded := excludeSet[s]; !excluded {
			result = append(result, s)
		}
	}
	if len(result) == 0 {
		return []uint64{1}
	}
	return result
}

// stopBootstrap is a no-op. QuicBaaS operates as a BaaS client.
func (s *Server) stopBootstrap() error {
	return nil
}

// stopDevCluster is a no-op. QuicBaaS operates as a BaaS client.
func (s *Server) stopDevCluster(ctx context.Context) error {
	return nil
}
