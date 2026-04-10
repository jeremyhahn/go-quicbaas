// Copyright 2026 Jeremy Hahn
// SPDX-License-Identifier: Apache-2.0
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

// Package shards provides the central shard registry for QuicBaaS,
// defining shard allocation ranges, metadata types, and classification
// helpers used across the platform.
package shards

// Shard allocation ranges.
const (
	// PlatformShardID is the fixed shard ID for the platform database.
	PlatformShardID uint64 = 1

	// ReservedMax is the upper bound (exclusive) of the reserved shard range.
	// Shards 2-99 are reserved for future platform use.
	ReservedMax uint64 = 100

	// TenantShardMin is the minimum shard ID for tenant databases.
	TenantShardMin uint64 = 100
)

// ShardMeta holds metadata about a shard allocation.
type ShardMeta struct {
	ShardID  uint64 `json:"shard_id"`
	TenantID string `json:"tenant_id,omitempty"`
	Database string `json:"database,omitempty"`
	Purpose  string `json:"purpose,omitempty"` // "system", "user"
}

// IsPlatformShard reports whether the shard ID is the platform shard.
func IsPlatformShard(id uint64) bool {
	return id == PlatformShardID
}

// IsReservedShard reports whether the shard ID falls in the reserved range (2-99).
func IsReservedShard(id uint64) bool {
	return id > PlatformShardID && id < ReservedMax
}

// IsTenantShard reports whether the shard ID is in the tenant range (>=100).
func IsTenantShard(id uint64) bool {
	return id >= TenantShardMin
}
