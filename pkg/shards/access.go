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

// AccessTier identifies the access level required for a shard.
type AccessTier string

const (
	// AccessPlatform requires db:admin role (platform shard only).
	AccessPlatform AccessTier = "platform"

	// AccessTenant requires authenticated tenant context.
	AccessTenant AccessTier = "tenant"
)

// TierForShard returns the access tier required for the given shard ID.
func TierForShard(id uint64) AccessTier {
	if IsPlatformShard(id) {
		return AccessPlatform
	}
	return AccessTenant
}

// GuardPlatformShard checks whether access to the platform shard is permitted.
// It returns a *PlatformShardAccessDeniedError when the shard is the platform
// shard and the caller does not have admin role.
func GuardPlatformShard(id uint64, hasAdminRole bool) error {
	if !IsPlatformShard(id) {
		return nil
	}
	if !hasAdminRole {
		return &PlatformShardAccessDeniedError{ShardID: id}
	}
	return nil
}
