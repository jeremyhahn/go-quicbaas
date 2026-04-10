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

package catalog

import (
	"errors"
	"testing"
	"time"
)

func TestReplicationMode_String(t *testing.T) {
	tests := []struct {
		mode     ReplicationMode
		expected string
	}{
		{ReplicationModeSync, "sync"},
		{ReplicationModeAsync, "async"},
		{ReplicationModeQuorum, "quorum"},
		{ReplicationMode("custom"), "custom"},
	}

	for _, tt := range tests {
		t.Run(tt.expected, func(t *testing.T) {
			if got := tt.mode.String(); got != tt.expected {
				t.Errorf("ReplicationMode.String() = %s, want %s", got, tt.expected)
			}
		})
	}
}

func TestReplicationMode_IsValid(t *testing.T) {
	tests := []struct {
		mode  ReplicationMode
		valid bool
	}{
		{ReplicationModeSync, true},
		{ReplicationModeAsync, true},
		{ReplicationModeQuorum, true},
		{ReplicationMode("invalid"), false},
		{ReplicationMode(""), false},
	}

	for _, tt := range tests {
		t.Run(string(tt.mode), func(t *testing.T) {
			if got := tt.mode.IsValid(); got != tt.valid {
				t.Errorf("ReplicationMode.IsValid() = %v, want %v", got, tt.valid)
			}
		})
	}
}

func TestStorageTier_String(t *testing.T) {
	tests := []struct {
		tier     StorageTier
		expected string
	}{
		{TierHot, "hot"},
		{TierWarm, "warm"},
		{TierCold, "cold"},
		{StorageTier("custom"), "custom"},
	}

	for _, tt := range tests {
		t.Run(tt.expected, func(t *testing.T) {
			if got := tt.tier.String(); got != tt.expected {
				t.Errorf("StorageTier.String() = %s, want %s", got, tt.expected)
			}
		})
	}
}

func TestStorageTier_IsValid(t *testing.T) {
	tests := []struct {
		tier  StorageTier
		valid bool
	}{
		{TierHot, true},
		{TierWarm, true},
		{TierCold, true},
		{StorageTier("invalid"), false},
		{StorageTier(""), false},
	}

	for _, tt := range tests {
		t.Run(string(tt.tier), func(t *testing.T) {
			if got := tt.tier.IsValid(); got != tt.valid {
				t.Errorf("StorageTier.IsValid() = %v, want %v", got, tt.valid)
			}
		})
	}
}

func TestDefaultReplicationPolicy(t *testing.T) {
	p := DefaultReplicationPolicy()

	if !p.Enabled {
		t.Error("Default policy should be enabled")
	}
	if p.MinReplicas != DefaultMinReplicas {
		t.Errorf("MinReplicas = %d, want %d", p.MinReplicas, DefaultMinReplicas)
	}
	if p.Mode != ReplicationModeAsync {
		t.Errorf("Mode = %s, want %s", p.Mode, ReplicationModeAsync)
	}
	if p.Priority != DefaultPriority {
		t.Errorf("Priority = %d, want %d", p.Priority, DefaultPriority)
	}
	if p.TargetStores != nil {
		t.Error("TargetStores should be nil by default")
	}
}

func TestReplicationPolicy_Validate(t *testing.T) {
	tests := []struct {
		name    string
		policy  *ReplicationPolicy
		wantErr bool
		errCode ErrorCode
	}{
		{
			name:    "nil policy",
			policy:  nil,
			wantErr: true,
			errCode: ErrCodeReplicationPolicyInvalid,
		},
		{
			name: "disabled policy passes",
			policy: &ReplicationPolicy{
				Enabled:     false,
				MinReplicas: -1, // Invalid, but should pass because disabled
			},
			wantErr: false,
		},
		{
			name: "valid policy",
			policy: &ReplicationPolicy{
				Enabled:      true,
				MinReplicas:  2,
				TargetStores: []string{"s3-primary", "gcs-west"},
				Mode:         ReplicationModeAsync,
				Priority:     5,
			},
			wantErr: false,
		},
		{
			name: "negative min replicas",
			policy: &ReplicationPolicy{
				Enabled:     true,
				MinReplicas: -1,
				Mode:        ReplicationModeAsync,
				Priority:    5,
			},
			wantErr: true,
			errCode: ErrCodeReplicaCountInvalid,
		},
		{
			name: "min replicas too high",
			policy: &ReplicationPolicy{
				Enabled:     true,
				MinReplicas: 101,
				Mode:        ReplicationModeAsync,
				Priority:    5,
			},
			wantErr: true,
			errCode: ErrCodeReplicaCountInvalid,
		},
		{
			name: "priority too low",
			policy: &ReplicationPolicy{
				Enabled:     true,
				MinReplicas: 2,
				Mode:        ReplicationModeAsync,
				Priority:    0,
			},
			wantErr: true,
			errCode: ErrCodePolicyPriorityInvalid,
		},
		{
			name: "priority too high",
			policy: &ReplicationPolicy{
				Enabled:     true,
				MinReplicas: 2,
				Mode:        ReplicationModeAsync,
				Priority:    11,
			},
			wantErr: true,
			errCode: ErrCodePolicyPriorityInvalid,
		},
		{
			name: "invalid mode",
			policy: &ReplicationPolicy{
				Enabled:     true,
				MinReplicas: 2,
				Mode:        ReplicationMode("invalid"),
				Priority:    5,
			},
			wantErr: true,
			errCode: ErrCodeReplicationPolicyInvalid,
		},
		{
			name: "empty target store in list",
			policy: &ReplicationPolicy{
				Enabled:      true,
				MinReplicas:  2,
				TargetStores: []string{"s3-primary", ""},
				Mode:         ReplicationModeAsync,
				Priority:     5,
			},
			wantErr: true,
			errCode: ErrCodeTargetStoreInvalid,
		},
		{
			name: "not enough target stores for min replicas",
			policy: &ReplicationPolicy{
				Enabled:      true,
				MinReplicas:  3,
				TargetStores: []string{"s3-primary", "gcs-west"},
				Mode:         ReplicationModeAsync,
				Priority:     5,
			},
			wantErr: true,
			errCode: ErrCodeReplicaCountInvalid,
		},
		{
			name: "valid with sync mode",
			policy: &ReplicationPolicy{
				Enabled:      true,
				MinReplicas:  2,
				TargetStores: []string{"s3-primary", "gcs-west"},
				Mode:         ReplicationModeSync,
				Priority:     10,
			},
			wantErr: false,
		},
		{
			name: "valid with quorum mode",
			policy: &ReplicationPolicy{
				Enabled:      true,
				MinReplicas:  3,
				TargetStores: []string{"s3-primary", "gcs-west", "s3-backup"},
				Mode:         ReplicationModeQuorum,
				Priority:     1,
			},
			wantErr: false,
		},
		{
			name: "valid zero min replicas",
			policy: &ReplicationPolicy{
				Enabled:     true,
				MinReplicas: 0,
				Mode:        ReplicationModeAsync,
				Priority:    5,
			},
			wantErr: false,
		},
		{
			name: "valid empty target stores with min replicas zero",
			policy: &ReplicationPolicy{
				Enabled:      true,
				MinReplicas:  0,
				TargetStores: []string{},
				Mode:         ReplicationModeAsync,
				Priority:     5,
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.policy.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("Validate() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if tt.wantErr && err != nil {
				var ce *CatalogError
				if errors.As(err, &ce) {
					if ce.Code != tt.errCode {
						t.Errorf("Error code = %s, want %s", ce.Code, tt.errCode)
					}
				} else {
					t.Error("Expected CatalogError type")
				}
			}
		})
	}
}

func TestReplicationPolicy_Clone(t *testing.T) {
	t.Run("nil policy", func(t *testing.T) {
		var p *ReplicationPolicy
		clone := p.Clone()
		if clone != nil {
			t.Error("Clone of nil should be nil")
		}
	})

	t.Run("full policy", func(t *testing.T) {
		p := &ReplicationPolicy{
			Enabled:      true,
			MinReplicas:  3,
			TargetStores: []string{"s3-primary", "gcs-west"},
			Mode:         ReplicationModeSync,
			Priority:     8,
		}

		clone := p.Clone()

		if clone == p {
			t.Error("Clone should be a different pointer")
		}
		if clone.Enabled != p.Enabled {
			t.Errorf("Enabled = %v, want %v", clone.Enabled, p.Enabled)
		}
		if clone.MinReplicas != p.MinReplicas {
			t.Errorf("MinReplicas = %d, want %d", clone.MinReplicas, p.MinReplicas)
		}
		if clone.Mode != p.Mode {
			t.Errorf("Mode = %s, want %s", clone.Mode, p.Mode)
		}
		if clone.Priority != p.Priority {
			t.Errorf("Priority = %d, want %d", clone.Priority, p.Priority)
		}
		if len(clone.TargetStores) != len(p.TargetStores) {
			t.Errorf("TargetStores length = %d, want %d", len(clone.TargetStores), len(p.TargetStores))
		}

		// Verify deep copy of slice
		if &clone.TargetStores[0] == &p.TargetStores[0] {
			t.Error("TargetStores should be a deep copy")
		}

		// Modify clone and verify original unchanged
		clone.TargetStores[0] = "modified"
		if p.TargetStores[0] == "modified" {
			t.Error("Modifying clone should not affect original")
		}
	})

	t.Run("nil target stores", func(t *testing.T) {
		p := &ReplicationPolicy{
			Enabled:      true,
			TargetStores: nil,
		}

		clone := p.Clone()
		if clone.TargetStores != nil {
			t.Error("Clone should preserve nil TargetStores")
		}
	})
}

func TestDefaultArchivePolicy(t *testing.T) {
	p := DefaultArchivePolicy()

	if p.Enabled {
		t.Error("Default archive policy should be disabled")
	}
	if p.ArchiveAfterDays != 90 {
		t.Errorf("ArchiveAfterDays = %d, want 90", p.ArchiveAfterDays)
	}
	if p.TargetStore != "" {
		t.Error("TargetStore should be empty by default")
	}
	if p.DeleteFromPrimary {
		t.Error("DeleteFromPrimary should be false by default")
	}
	if p.CompressionLevel != DefaultCompressionLevel {
		t.Errorf("CompressionLevel = %d, want %d", p.CompressionLevel, DefaultCompressionLevel)
	}
}

func TestArchivePolicy_Validate(t *testing.T) {
	tests := []struct {
		name    string
		policy  *ArchivePolicy
		wantErr bool
		errCode ErrorCode
	}{
		{
			name:    "nil policy",
			policy:  nil,
			wantErr: true,
			errCode: ErrCodeArchivePolicyInvalid,
		},
		{
			name: "disabled policy passes",
			policy: &ArchivePolicy{
				Enabled:          false,
				ArchiveAfterDays: -1, // Invalid, but should pass because disabled
			},
			wantErr: false,
		},
		{
			name: "valid policy",
			policy: &ArchivePolicy{
				Enabled:           true,
				ArchiveAfterDays:  30,
				TargetStore:       "s3-archive",
				DeleteFromPrimary: true,
				CompressionLevel:  6,
			},
			wantErr: false,
		},
		{
			name: "negative archive days",
			policy: &ArchivePolicy{
				Enabled:          true,
				ArchiveAfterDays: -1,
				TargetStore:      "s3-archive",
			},
			wantErr: true,
			errCode: ErrCodeArchiveDaysInvalid,
		},
		{
			name: "empty target store when enabled",
			policy: &ArchivePolicy{
				Enabled:          true,
				ArchiveAfterDays: 30,
				TargetStore:      "",
			},
			wantErr: true,
			errCode: ErrCodeTargetStoreInvalid,
		},
		{
			name: "compression level too low",
			policy: &ArchivePolicy{
				Enabled:          true,
				ArchiveAfterDays: 30,
				TargetStore:      "s3-archive",
				CompressionLevel: -1,
			},
			wantErr: true,
			errCode: ErrCodeCompressionLevelInvalid,
		},
		{
			name: "compression level too high",
			policy: &ArchivePolicy{
				Enabled:          true,
				ArchiveAfterDays: 30,
				TargetStore:      "s3-archive",
				CompressionLevel: 10,
			},
			wantErr: true,
			errCode: ErrCodeCompressionLevelInvalid,
		},
		{
			name: "zero archive days is valid",
			policy: &ArchivePolicy{
				Enabled:          true,
				ArchiveAfterDays: 0,
				TargetStore:      "s3-archive",
			},
			wantErr: false,
		},
		{
			name: "max compression level",
			policy: &ArchivePolicy{
				Enabled:          true,
				ArchiveAfterDays: 30,
				TargetStore:      "s3-archive",
				CompressionLevel: 9,
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.policy.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("Validate() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if tt.wantErr && err != nil {
				var ce *CatalogError
				if errors.As(err, &ce) {
					if ce.Code != tt.errCode {
						t.Errorf("Error code = %s, want %s", ce.Code, tt.errCode)
					}
				} else {
					t.Error("Expected CatalogError type")
				}
			}
		})
	}
}

func TestArchivePolicy_Clone(t *testing.T) {
	t.Run("nil policy", func(t *testing.T) {
		var p *ArchivePolicy
		clone := p.Clone()
		if clone != nil {
			t.Error("Clone of nil should be nil")
		}
	})

	t.Run("full policy", func(t *testing.T) {
		p := &ArchivePolicy{
			Enabled:           true,
			ArchiveAfterDays:  30,
			TargetStore:       "s3-archive",
			DeleteFromPrimary: true,
			CompressionLevel:  6,
		}

		clone := p.Clone()

		if clone == p {
			t.Error("Clone should be a different pointer")
		}
		if clone.Enabled != p.Enabled {
			t.Errorf("Enabled = %v, want %v", clone.Enabled, p.Enabled)
		}
		if clone.ArchiveAfterDays != p.ArchiveAfterDays {
			t.Errorf("ArchiveAfterDays = %d, want %d", clone.ArchiveAfterDays, p.ArchiveAfterDays)
		}
		if clone.TargetStore != p.TargetStore {
			t.Errorf("TargetStore = %s, want %s", clone.TargetStore, p.TargetStore)
		}
		if clone.DeleteFromPrimary != p.DeleteFromPrimary {
			t.Errorf("DeleteFromPrimary = %v, want %v", clone.DeleteFromPrimary, p.DeleteFromPrimary)
		}
		if clone.CompressionLevel != p.CompressionLevel {
			t.Errorf("CompressionLevel = %d, want %d", clone.CompressionLevel, p.CompressionLevel)
		}
	})
}

func TestArchivePolicy_ShouldArchive(t *testing.T) {
	now := time.Now()

	tests := []struct {
		name         string
		policy       *ArchivePolicy
		lastAccessAt int64
		want         bool
	}{
		{
			name:         "nil policy",
			policy:       nil,
			lastAccessAt: now.Unix(),
			want:         false,
		},
		{
			name: "disabled policy",
			policy: &ArchivePolicy{
				Enabled:          false,
				ArchiveAfterDays: 30,
			},
			lastAccessAt: now.Add(-60 * 24 * time.Hour).Unix(),
			want:         false,
		},
		{
			name: "zero archive days - always archive",
			policy: &ArchivePolicy{
				Enabled:          true,
				ArchiveAfterDays: 0,
			},
			lastAccessAt: now.Unix(),
			want:         true,
		},
		{
			name: "zero last access time - should archive",
			policy: &ArchivePolicy{
				Enabled:          true,
				ArchiveAfterDays: 30,
			},
			lastAccessAt: 0,
			want:         true,
		},
		{
			name: "accessed recently - should not archive",
			policy: &ArchivePolicy{
				Enabled:          true,
				ArchiveAfterDays: 30,
			},
			lastAccessAt: now.Add(-10 * 24 * time.Hour).Unix(),
			want:         false,
		},
		{
			name: "accessed long ago - should archive",
			policy: &ArchivePolicy{
				Enabled:          true,
				ArchiveAfterDays: 30,
			},
			lastAccessAt: now.Add(-60 * 24 * time.Hour).Unix(),
			want:         true,
		},
		{
			name: "exactly at threshold - should archive",
			policy: &ArchivePolicy{
				Enabled:          true,
				ArchiveAfterDays: 30,
			},
			lastAccessAt: now.Add(-30 * 24 * time.Hour).Unix(),
			want:         true,
		},
		{
			name: "one day before threshold - should not archive",
			policy: &ArchivePolicy{
				Enabled:          true,
				ArchiveAfterDays: 30,
			},
			lastAccessAt: now.Add(-29 * 24 * time.Hour).Unix(),
			want:         false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.policy.ShouldArchive(tt.lastAccessAt); got != tt.want {
				t.Errorf("ShouldArchive() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestDefaultRetentionPolicy(t *testing.T) {
	p := DefaultRetentionPolicy()

	if p.Enabled {
		t.Error("Default retention policy should be disabled")
	}
	if p.RetentionDays != 0 {
		t.Errorf("RetentionDays = %d, want 0", p.RetentionDays)
	}
	if p.MaxVersions != 0 {
		t.Errorf("MaxVersions = %d, want 0", p.MaxVersions)
	}
	if p.DeleteAfter != 0 {
		t.Errorf("DeleteAfter = %d, want 0", p.DeleteAfter)
	}
}

func TestRetentionPolicy_Validate(t *testing.T) {
	tests := []struct {
		name    string
		policy  *RetentionPolicy
		wantErr bool
		errCode ErrorCode
	}{
		{
			name:    "nil policy",
			policy:  nil,
			wantErr: true,
			errCode: ErrCodeRetentionPolicyInvalid,
		},
		{
			name: "disabled policy passes",
			policy: &RetentionPolicy{
				Enabled:       false,
				RetentionDays: -1, // Invalid, but should pass because disabled
			},
			wantErr: false,
		},
		{
			name: "valid policy",
			policy: &RetentionPolicy{
				Enabled:       true,
				RetentionDays: 365,
				MaxVersions:   10,
				DeleteAfter:   0,
			},
			wantErr: false,
		},
		{
			name: "negative retention days",
			policy: &RetentionPolicy{
				Enabled:       true,
				RetentionDays: -1,
			},
			wantErr: true,
			errCode: ErrCodeRetentionDaysInvalid,
		},
		{
			name: "negative max versions",
			policy: &RetentionPolicy{
				Enabled:     true,
				MaxVersions: -1,
			},
			wantErr: true,
			errCode: ErrCodeMaxVersionsInvalid,
		},
		{
			name: "negative delete after",
			policy: &RetentionPolicy{
				Enabled:     true,
				DeleteAfter: -1,
			},
			wantErr: true,
			errCode: ErrCodeRetentionPolicyInvalid,
		},
		{
			name: "zero values valid",
			policy: &RetentionPolicy{
				Enabled:       true,
				RetentionDays: 0,
				MaxVersions:   0,
				DeleteAfter:   0,
			},
			wantErr: false,
		},
		{
			name: "only delete after set",
			policy: &RetentionPolicy{
				Enabled:     true,
				DeleteAfter: time.Now().Add(24 * time.Hour).Unix(),
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.policy.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("Validate() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if tt.wantErr && err != nil {
				var ce *CatalogError
				if errors.As(err, &ce) {
					if ce.Code != tt.errCode {
						t.Errorf("Error code = %s, want %s", ce.Code, tt.errCode)
					}
				} else {
					t.Error("Expected CatalogError type")
				}
			}
		})
	}
}

func TestRetentionPolicy_Clone(t *testing.T) {
	t.Run("nil policy", func(t *testing.T) {
		var p *RetentionPolicy
		clone := p.Clone()
		if clone != nil {
			t.Error("Clone of nil should be nil")
		}
	})

	t.Run("full policy", func(t *testing.T) {
		p := &RetentionPolicy{
			Enabled:       true,
			RetentionDays: 365,
			MaxVersions:   10,
			DeleteAfter:   12345678,
		}

		clone := p.Clone()

		if clone == p {
			t.Error("Clone should be a different pointer")
		}
		if clone.Enabled != p.Enabled {
			t.Errorf("Enabled = %v, want %v", clone.Enabled, p.Enabled)
		}
		if clone.RetentionDays != p.RetentionDays {
			t.Errorf("RetentionDays = %d, want %d", clone.RetentionDays, p.RetentionDays)
		}
		if clone.MaxVersions != p.MaxVersions {
			t.Errorf("MaxVersions = %d, want %d", clone.MaxVersions, p.MaxVersions)
		}
		if clone.DeleteAfter != p.DeleteAfter {
			t.Errorf("DeleteAfter = %d, want %d", clone.DeleteAfter, p.DeleteAfter)
		}
	})
}

func TestRetentionPolicy_ShouldDelete(t *testing.T) {
	now := time.Now()

	tests := []struct {
		name      string
		policy    *RetentionPolicy
		createdAt int64
		want      bool
	}{
		{
			name:      "nil policy",
			policy:    nil,
			createdAt: now.Unix(),
			want:      false,
		},
		{
			name: "disabled policy",
			policy: &RetentionPolicy{
				Enabled:       false,
				RetentionDays: 30,
			},
			createdAt: now.Add(-60 * 24 * time.Hour).Unix(),
			want:      false,
		},
		{
			name: "delete after timestamp reached",
			policy: &RetentionPolicy{
				Enabled:     true,
				DeleteAfter: now.Add(-1 * time.Hour).Unix(),
			},
			createdAt: now.Unix(),
			want:      true,
		},
		{
			name: "delete after timestamp not reached",
			policy: &RetentionPolicy{
				Enabled:     true,
				DeleteAfter: now.Add(1 * time.Hour).Unix(),
			},
			createdAt: now.Unix(),
			want:      false,
		},
		{
			name: "zero retention days - never delete",
			policy: &RetentionPolicy{
				Enabled:       true,
				RetentionDays: 0,
			},
			createdAt: now.Add(-365 * 24 * time.Hour).Unix(),
			want:      false,
		},
		{
			name: "zero created at - never delete",
			policy: &RetentionPolicy{
				Enabled:       true,
				RetentionDays: 30,
			},
			createdAt: 0,
			want:      false,
		},
		{
			name: "created recently - should not delete",
			policy: &RetentionPolicy{
				Enabled:       true,
				RetentionDays: 30,
			},
			createdAt: now.Add(-10 * 24 * time.Hour).Unix(),
			want:      false,
		},
		{
			name: "created long ago - should delete",
			policy: &RetentionPolicy{
				Enabled:       true,
				RetentionDays: 30,
			},
			createdAt: now.Add(-60 * 24 * time.Hour).Unix(),
			want:      true,
		},
		{
			name: "exactly at threshold - should delete",
			policy: &RetentionPolicy{
				Enabled:       true,
				RetentionDays: 30,
			},
			createdAt: now.Add(-30 * 24 * time.Hour).Unix(),
			want:      true,
		},
		{
			name: "one day before threshold - should not delete",
			policy: &RetentionPolicy{
				Enabled:       true,
				RetentionDays: 30,
			},
			createdAt: now.Add(-29 * 24 * time.Hour).Unix(),
			want:      false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.policy.ShouldDelete(tt.createdAt); got != tt.want {
				t.Errorf("ShouldDelete() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestRetentionPolicy_IsExpired(t *testing.T) {
	now := time.Now()

	t.Run("is alias for ShouldDelete", func(t *testing.T) {
		policy := &RetentionPolicy{
			Enabled:       true,
			RetentionDays: 30,
		}

		createdAt := now.Add(-60 * 24 * time.Hour).Unix()

		shouldDelete := policy.ShouldDelete(createdAt)
		isExpired := policy.IsExpired(createdAt)

		if shouldDelete != isExpired {
			t.Errorf("IsExpired() = %v, ShouldDelete() = %v, they should be equal", isExpired, shouldDelete)
		}
	})

	t.Run("nil policy returns false", func(t *testing.T) {
		var p *RetentionPolicy
		if p.IsExpired(now.Unix()) {
			t.Error("IsExpired on nil policy should return false")
		}
	})
}

func TestPolicyConstants(t *testing.T) {
	if MinPriority != 1 {
		t.Errorf("MinPriority = %d, want 1", MinPriority)
	}
	if MaxPriority != 10 {
		t.Errorf("MaxPriority = %d, want 10", MaxPriority)
	}
	if DefaultPriority != 5 {
		t.Errorf("DefaultPriority = %d, want 5", DefaultPriority)
	}
	if MinCompressionLevel != 0 {
		t.Errorf("MinCompressionLevel = %d, want 0", MinCompressionLevel)
	}
	if MaxCompressionLevel != 9 {
		t.Errorf("MaxCompressionLevel = %d, want 9", MaxCompressionLevel)
	}
	if DefaultCompressionLevel != 0 {
		t.Errorf("DefaultCompressionLevel = %d, want 0", DefaultCompressionLevel)
	}
	if MinReplicaCount != 0 {
		t.Errorf("MinReplicaCount = %d, want 0", MinReplicaCount)
	}
	if MaxReplicaCount != 100 {
		t.Errorf("MaxReplicaCount = %d, want 100", MaxReplicaCount)
	}
	if DefaultMinReplicas != 2 {
		t.Errorf("DefaultMinReplicas = %d, want 2", DefaultMinReplicas)
	}
}

func TestPolicyErrorCodes(t *testing.T) {
	codes := []ErrorCode{
		ErrCodeReplicationPolicyInvalid,
		ErrCodeArchivePolicyInvalid,
		ErrCodeRetentionPolicyInvalid,
		ErrCodePolicyPriorityInvalid,
		ErrCodeCompressionLevelInvalid,
		ErrCodeTargetStoreInvalid,
		ErrCodeReplicaCountInvalid,
		ErrCodeRetentionDaysInvalid,
		ErrCodeMaxVersionsInvalid,
		ErrCodeArchiveDaysInvalid,
	}

	seen := make(map[ErrorCode]bool)
	for _, code := range codes {
		if code == "" {
			t.Error("Error code should not be empty")
		}
		if seen[code] {
			t.Errorf("Duplicate error code: %s", code)
		}
		seen[code] = true
	}
}

func TestReplicationModeConstants(t *testing.T) {
	if ReplicationModeSync != "sync" {
		t.Errorf("ReplicationModeSync = %s, want sync", ReplicationModeSync)
	}
	if ReplicationModeAsync != "async" {
		t.Errorf("ReplicationModeAsync = %s, want async", ReplicationModeAsync)
	}
	if ReplicationModeQuorum != "quorum" {
		t.Errorf("ReplicationModeQuorum = %s, want quorum", ReplicationModeQuorum)
	}
}

func TestStorageTierConstants(t *testing.T) {
	if TierHot != "hot" {
		t.Errorf("TierHot = %s, want hot", TierHot)
	}
	if TierWarm != "warm" {
		t.Errorf("TierWarm = %s, want warm", TierWarm)
	}
	if TierCold != "cold" {
		t.Errorf("TierCold = %s, want cold", TierCold)
	}
}

func TestReplicationPolicy_ValidateEdgeCases(t *testing.T) {
	t.Run("max allowed replicas", func(t *testing.T) {
		stores := make([]string, MaxReplicaCount)
		for i := range stores {
			stores[i] = "store"
		}

		p := &ReplicationPolicy{
			Enabled:      true,
			MinReplicas:  MaxReplicaCount,
			TargetStores: stores,
			Mode:         ReplicationModeAsync,
			Priority:     5,
		}

		if err := p.Validate(); err != nil {
			t.Errorf("Validate() should pass with max replicas, got error: %v", err)
		}
	})

	t.Run("min allowed priority", func(t *testing.T) {
		p := &ReplicationPolicy{
			Enabled:     true,
			MinReplicas: 1,
			Mode:        ReplicationModeAsync,
			Priority:    MinPriority,
		}

		if err := p.Validate(); err != nil {
			t.Errorf("Validate() should pass with min priority, got error: %v", err)
		}
	})

	t.Run("max allowed priority", func(t *testing.T) {
		p := &ReplicationPolicy{
			Enabled:     true,
			MinReplicas: 1,
			Mode:        ReplicationModeAsync,
			Priority:    MaxPriority,
		}

		if err := p.Validate(); err != nil {
			t.Errorf("Validate() should pass with max priority, got error: %v", err)
		}
	})
}

func TestArchivePolicy_ValidateEdgeCases(t *testing.T) {
	t.Run("min compression level", func(t *testing.T) {
		p := &ArchivePolicy{
			Enabled:          true,
			ArchiveAfterDays: 30,
			TargetStore:      "archive",
			CompressionLevel: MinCompressionLevel,
		}

		if err := p.Validate(); err != nil {
			t.Errorf("Validate() should pass with min compression, got error: %v", err)
		}
	})

	t.Run("max compression level", func(t *testing.T) {
		p := &ArchivePolicy{
			Enabled:          true,
			ArchiveAfterDays: 30,
			TargetStore:      "archive",
			CompressionLevel: MaxCompressionLevel,
		}

		if err := p.Validate(); err != nil {
			t.Errorf("Validate() should pass with max compression, got error: %v", err)
		}
	})
}

func TestRetentionPolicy_ValidateEdgeCases(t *testing.T) {
	t.Run("large retention days", func(t *testing.T) {
		p := &RetentionPolicy{
			Enabled:       true,
			RetentionDays: 36500, // 100 years
		}

		if err := p.Validate(); err != nil {
			t.Errorf("Validate() should pass with large retention days, got error: %v", err)
		}
	})

	t.Run("large max versions", func(t *testing.T) {
		p := &RetentionPolicy{
			Enabled:     true,
			MaxVersions: 1000000,
		}

		if err := p.Validate(); err != nil {
			t.Errorf("Validate() should pass with large max versions, got error: %v", err)
		}
	})
}
