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

package shards

import "strconv"

// PlatformShardAccessDeniedError is returned when a non-admin caller
// attempts to access the platform shard.
type PlatformShardAccessDeniedError struct {
	ShardID uint64
}

// Error returns a human-readable description of the access denial.
func (e *PlatformShardAccessDeniedError) Error() string {
	return "shards: platform shard access denied for shard " + strconv.FormatUint(e.ShardID, 10)
}

// ReservedShardError is returned when an operation attempts to use
// a reserved shard ID.
type ReservedShardError struct {
	ShardID uint64
}

// Error returns a human-readable description of the reserved shard violation.
func (e *ReservedShardError) Error() string {
	return "shards: shard " + strconv.FormatUint(e.ShardID, 10) + " is reserved"
}

// InvalidShardIDError is returned when a shard ID of 0 is provided.
type InvalidShardIDError struct{}

// Error returns a human-readable description of the invalid shard ID.
func (e *InvalidShardIDError) Error() string {
	return "shards: invalid shard ID: 0"
}
