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

import (
	"errors"
	"testing"
)

func TestIsPlatformShard(t *testing.T) {
	if !IsPlatformShard(PlatformShardID) {
		t.Error("IsPlatformShard(PlatformShardID) = false, want true")
	}
}

func TestIsPlatformShard_NonPlatform(t *testing.T) {
	if IsPlatformShard(2) {
		t.Error("IsPlatformShard(2) = true, want false")
	}
	if IsPlatformShard(100) {
		t.Error("IsPlatformShard(100) = true, want false")
	}
	if IsPlatformShard(0) {
		t.Error("IsPlatformShard(0) = true, want false")
	}
}

func TestIsReservedShard(t *testing.T) {
	tests := []struct {
		id   uint64
		want bool
	}{
		{0, false},
		{1, false},    // platform, not reserved
		{2, true},     // first reserved
		{50, true},    // mid reserved
		{99, true},    // last reserved
		{100, false},  // tenant range
		{1000, false}, // tenant range
	}
	for _, tt := range tests {
		if got := IsReservedShard(tt.id); got != tt.want {
			t.Errorf("IsReservedShard(%d) = %v, want %v", tt.id, got, tt.want)
		}
	}
}

func TestIsTenantShard(t *testing.T) {
	tests := []struct {
		id   uint64
		want bool
	}{
		{0, false},
		{1, false},
		{99, false},
		{100, true},
		{101, true},
		{10000, true},
	}
	for _, tt := range tests {
		if got := IsTenantShard(tt.id); got != tt.want {
			t.Errorf("IsTenantShard(%d) = %v, want %v", tt.id, got, tt.want)
		}
	}
}

func TestTierForShard(t *testing.T) {
	if got := TierForShard(PlatformShardID); got != AccessPlatform {
		t.Errorf("TierForShard(PlatformShardID) = %q, want %q", got, AccessPlatform)
	}
}

func TestTierForShard_Tenant(t *testing.T) {
	if got := TierForShard(100); got != AccessTenant {
		t.Errorf("TierForShard(100) = %q, want %q", got, AccessTenant)
	}
	if got := TierForShard(50); got != AccessTenant {
		t.Errorf("TierForShard(50) = %q, want %q", got, AccessTenant)
	}
}

func TestGuardPlatformShard_AllowsAdmin(t *testing.T) {
	if err := GuardPlatformShard(PlatformShardID, true); err != nil {
		t.Errorf("GuardPlatformShard(platform, admin=true) = %v, want nil", err)
	}
}

func TestGuardPlatformShard_DeniesNonAdmin(t *testing.T) {
	err := GuardPlatformShard(PlatformShardID, false)
	if err == nil {
		t.Fatal("GuardPlatformShard(platform, admin=false) = nil, want error")
	}
	var accessErr *PlatformShardAccessDeniedError
	if !errors.As(err, &accessErr) {
		t.Fatalf("error type = %T, want *PlatformShardAccessDeniedError", err)
	}
	if accessErr.ShardID != PlatformShardID {
		t.Errorf("ShardID = %d, want %d", accessErr.ShardID, PlatformShardID)
	}
}

func TestGuardPlatformShard_AllowsNonPlatformShard(t *testing.T) {
	// Non-platform shards should always be allowed regardless of admin role.
	if err := GuardPlatformShard(100, false); err != nil {
		t.Errorf("GuardPlatformShard(100, admin=false) = %v, want nil", err)
	}
	if err := GuardPlatformShard(50, false); err != nil {
		t.Errorf("GuardPlatformShard(50, admin=false) = %v, want nil", err)
	}
}

func TestShardMeta_Fields(t *testing.T) {
	meta := ShardMeta{
		ShardID:  100,
		TenantID: "tenant-abc",
		Database: "my_app",
		Purpose:  "user",
	}
	if meta.ShardID != 100 {
		t.Errorf("ShardID = %d, want 100", meta.ShardID)
	}
	if meta.TenantID != "tenant-abc" {
		t.Errorf("TenantID = %q, want %q", meta.TenantID, "tenant-abc")
	}
	if meta.Database != "my_app" {
		t.Errorf("Database = %q, want %q", meta.Database, "my_app")
	}
	if meta.Purpose != "user" {
		t.Errorf("Purpose = %q, want %q", meta.Purpose, "user")
	}
}

func TestShardMeta_ZeroValue(t *testing.T) {
	var meta ShardMeta
	if meta.ShardID != 0 {
		t.Errorf("zero ShardMeta.ShardID = %d, want 0", meta.ShardID)
	}
	if meta.TenantID != "" {
		t.Errorf("zero ShardMeta.TenantID = %q, want empty", meta.TenantID)
	}
}

func TestPlatformShardAccessDeniedError_Message(t *testing.T) {
	err := &PlatformShardAccessDeniedError{ShardID: 1}
	want := "shards: platform shard access denied for shard 1"
	if got := err.Error(); got != want {
		t.Errorf("Error() = %q, want %q", got, want)
	}
}

func TestReservedShardError_Message(t *testing.T) {
	err := &ReservedShardError{ShardID: 50}
	want := "shards: shard 50 is reserved"
	if got := err.Error(); got != want {
		t.Errorf("Error() = %q, want %q", got, want)
	}
}

func TestInvalidShardIDError_Message(t *testing.T) {
	err := &InvalidShardIDError{}
	want := "shards: invalid shard ID: 0"
	if got := err.Error(); got != want {
		t.Errorf("Error() = %q, want %q", got, want)
	}
}

func TestConstants(t *testing.T) {
	if PlatformShardID != 1 {
		t.Errorf("PlatformShardID = %d, want 1", PlatformShardID)
	}
	if ReservedMax != 100 {
		t.Errorf("ReservedMax = %d, want 100", ReservedMax)
	}
	if TenantShardMin != 100 {
		t.Errorf("TenantShardMin = %d, want 100", TenantShardMin)
	}
}
