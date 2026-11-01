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

package config

import (
	"testing"
)

func TestDefaultObjectStoreDefaults(t *testing.T) {
	defaults := DefaultObjectStoreDefaults()

	if defaults == nil {
		t.Fatal("DefaultObjectStoreDefaults returned nil")
	}

	if defaults.ValueThreshold != DefaultValueThreshold {
		t.Errorf("expected ValueThreshold %d, got %d", DefaultValueThreshold, defaults.ValueThreshold)
	}

	if defaults.Compression != DefaultCompression {
		t.Errorf("expected Compression %q, got %q", DefaultCompression, defaults.Compression)
	}

	if !defaults.Encryption.Enabled {
		t.Error("expected Encryption.Enabled to be true")
	}

	if !defaults.Encryption.Enforce {
		t.Error("expected Encryption.Enforce to be true")
	}

	if defaults.Encryption.Algorithm != DefaultEncryptionAlgo {
		t.Errorf("expected Algorithm %q, got %q", DefaultEncryptionAlgo, defaults.Encryption.Algorithm)
	}

	if !defaults.Replication.Enabled {
		t.Error("expected Replication.Enabled to be true")
	}

	if defaults.Replication.MinReplicas != DefaultMinReplicas {
		t.Errorf("expected MinReplicas %d, got %d", DefaultMinReplicas, defaults.Replication.MinReplicas)
	}

	if defaults.Replication.Mode != DefaultReplicationMode {
		t.Errorf("expected Mode %q, got %q", DefaultReplicationMode, defaults.Replication.Mode)
	}

	if defaults.Replication.Priority != DefaultReplicationPriority {
		t.Errorf("expected Priority %d, got %d", DefaultReplicationPriority, defaults.Replication.Priority)
	}

	if defaults.Archive.Enabled {
		t.Error("expected Archive.Enabled to be false")
	}

	if defaults.Archive.ArchiveAfterDays != DefaultArchiveAfterDays {
		t.Errorf("expected ArchiveAfterDays %d, got %d", DefaultArchiveAfterDays, defaults.Archive.ArchiveAfterDays)
	}

	if defaults.Retention.Enabled {
		t.Error("expected Retention.Enabled to be false")
	}
}

func TestObjectStoreDefaults_Clone(t *testing.T) {
	t.Run("nil receiver", func(t *testing.T) {
		var d *ObjectStoreDefaults
		clone := d.Clone()
		if clone != nil {
			t.Error("expected nil clone for nil receiver")
		}
	})

	t.Run("full clone", func(t *testing.T) {
		original := &ObjectStoreDefaults{
			PrimaryBackend:   "s3-primary",
			RequiredBackends: []string{"s3-primary", "gcs-backup"},
			ValueThreshold:   2048000,
			Compression:      CompressionGzip,
			Encryption: ObjectStoreEncryption{
				Enabled:    true,
				Enforce:    true,
				DefaultKey: "dek:test",
				Algorithm:  "AES-128-GCM",
			},
			Replication: ObjectStoreReplication{
				Enabled:      true,
				MinReplicas:  3,
				TargetStores: []string{"s3-primary", "gcs-backup"},
				Mode:         ReplicationModeQuorum,
				Priority:     8,
			},
			Archive: ObjectStoreArchive{
				Enabled:           true,
				ArchiveAfterDays:  60,
				TargetStore:       "glacier",
				DeleteFromPrimary: true,
				CompressionLevel:  9,
			},
			Retention: ObjectStoreRetention{
				Enabled:       true,
				RetentionDays: 365,
				MaxVersions:   10,
			},
		}

		clone := original.Clone()

		if clone.PrimaryBackend != original.PrimaryBackend {
			t.Errorf("PrimaryBackend mismatch: got %q, want %q", clone.PrimaryBackend, original.PrimaryBackend)
		}

		if len(clone.RequiredBackends) != len(original.RequiredBackends) {
			t.Errorf("RequiredBackends length mismatch: got %d, want %d", len(clone.RequiredBackends), len(original.RequiredBackends))
		}

		// Verify deep copy of slice
		clone.RequiredBackends[0] = "modified"
		if original.RequiredBackends[0] == "modified" {
			t.Error("RequiredBackends was not deep copied")
		}

		// Verify replication target stores deep copy
		clone.Replication.TargetStores[0] = "modified"
		if original.Replication.TargetStores[0] == "modified" {
			t.Error("Replication.TargetStores was not deep copied")
		}
	})
}

func TestObjectStoreDefaults_Merge(t *testing.T) {
	t.Run("merge nil", func(t *testing.T) {
		d := DefaultObjectStoreDefaults()
		d.Merge(nil)
		// Should not panic
	})

	t.Run("merge values", func(t *testing.T) {
		base := DefaultObjectStoreDefaults()
		other := &ObjectStoreDefaults{
			PrimaryBackend:   "new-primary",
			RequiredBackends: []string{"backend1", "backend2"},
			ValueThreshold:   5000000,
			Compression:      CompressionLZ4,
		}

		base.Merge(other)

		if base.PrimaryBackend != "new-primary" {
			t.Errorf("expected PrimaryBackend %q, got %q", "new-primary", base.PrimaryBackend)
		}

		if len(base.RequiredBackends) != 2 {
			t.Errorf("expected 2 RequiredBackends, got %d", len(base.RequiredBackends))
		}

		if base.ValueThreshold != 5000000 {
			t.Errorf("expected ValueThreshold 5000000, got %d", base.ValueThreshold)
		}

		if base.Compression != CompressionLZ4 {
			t.Errorf("expected Compression %q, got %q", CompressionLZ4, base.Compression)
		}
	})

	t.Run("merge does not override with empty values", func(t *testing.T) {
		base := &ObjectStoreDefaults{
			PrimaryBackend: "original",
			ValueThreshold: 1000,
			Compression:    CompressionZstd,
		}

		other := &ObjectStoreDefaults{
			// All empty/zero values
		}

		base.Merge(other)

		if base.PrimaryBackend != "original" {
			t.Errorf("PrimaryBackend should not have been overridden")
		}

		if base.ValueThreshold != 1000 {
			t.Errorf("ValueThreshold should not have been overridden")
		}

		if base.Compression != CompressionZstd {
			t.Errorf("Compression should not have been overridden")
		}
	})
}

func TestObjectStoreDefaults_Validate(t *testing.T) {
	t.Run("valid config", func(t *testing.T) {
		d := DefaultObjectStoreDefaults()
		if err := d.Validate(); err != nil {
			t.Errorf("unexpected validation error: %v", err)
		}
	})

	t.Run("invalid compression", func(t *testing.T) {
		d := &ObjectStoreDefaults{
			Compression: "invalid-compression",
		}
		err := d.Validate()
		if err == nil {
			t.Error("expected validation error for invalid compression")
		}
		verrs, ok := err.(ValidationErrors)
		if !ok {
			t.Fatalf("expected ValidationErrors, got %T", err)
		}
		if len(verrs) == 0 {
			t.Error("expected at least one validation error")
		}
	})

	t.Run("invalid replication mode", func(t *testing.T) {
		d := &ObjectStoreDefaults{
			Compression: CompressionZstd,
			Replication: ObjectStoreReplication{
				Mode: "invalid-mode",
			},
		}
		err := d.Validate()
		if err == nil {
			t.Error("expected validation error for invalid replication mode")
		}
	})

	t.Run("invalid replication priority", func(t *testing.T) {
		d := &ObjectStoreDefaults{
			Compression: CompressionZstd,
			Replication: ObjectStoreReplication{
				Mode:     ReplicationModeAsync,
				Priority: 15, // Invalid: must be 1-10
			},
		}
		err := d.Validate()
		if err == nil {
			t.Error("expected validation error for invalid priority")
		}
	})

	t.Run("invalid archive compression level", func(t *testing.T) {
		d := &ObjectStoreDefaults{
			Compression: CompressionZstd,
			Archive: ObjectStoreArchive{
				CompressionLevel: 15, // Invalid: must be 0-9
			},
		}
		err := d.Validate()
		if err == nil {
			t.Error("expected validation error for invalid compression level")
		}
	})

	t.Run("negative values", func(t *testing.T) {
		d := &ObjectStoreDefaults{
			Compression: CompressionZstd,
			Replication: ObjectStoreReplication{
				MinReplicas: -1,
			},
			Archive: ObjectStoreArchive{
				ArchiveAfterDays: -1,
				CompressionLevel: -1,
			},
			Retention: ObjectStoreRetention{
				RetentionDays: -1,
				MaxVersions:   -1,
			},
		}
		err := d.Validate()
		if err == nil {
			t.Error("expected validation errors for negative values")
		}
		verrs, ok := err.(ValidationErrors)
		if !ok {
			t.Fatalf("expected ValidationErrors, got %T", err)
		}
		// Should have errors for: min_replicas, archive_after_days, compression_level, retention_days, max_versions
		if len(verrs) < 5 {
			t.Errorf("expected at least 5 validation errors, got %d", len(verrs))
		}
	})
}

func TestObjectStoreEncryption_Clone(t *testing.T) {
	t.Run("nil receiver", func(t *testing.T) {
		var e *ObjectStoreEncryption
		clone := e.Clone()
		if clone != nil {
			t.Error("expected nil clone for nil receiver")
		}
	})

	t.Run("full clone", func(t *testing.T) {
		original := &ObjectStoreEncryption{
			Enabled:    true,
			Enforce:    true,
			DefaultKey: "dek:test/key",
			Algorithm:  "AES-256-GCM",
		}

		clone := original.Clone()

		if clone.Enabled != original.Enabled {
			t.Errorf("Enabled mismatch")
		}
		if clone.Enforce != original.Enforce {
			t.Errorf("Enforce mismatch")
		}
		if clone.DefaultKey != original.DefaultKey {
			t.Errorf("DefaultKey mismatch")
		}
		if clone.Algorithm != original.Algorithm {
			t.Errorf("Algorithm mismatch")
		}

		// Verify independence
		clone.DefaultKey = "modified"
		if original.DefaultKey == "modified" {
			t.Error("clone is not independent")
		}
	})
}

func TestObjectStoreEncryption_Merge(t *testing.T) {
	t.Run("merge nil", func(t *testing.T) {
		e := &ObjectStoreEncryption{Enabled: true}
		e.Merge(nil)
		if !e.Enabled {
			t.Error("merge nil should not change values")
		}
	})

	t.Run("merge values", func(t *testing.T) {
		base := &ObjectStoreEncryption{
			DefaultKey: "original-key",
		}

		other := &ObjectStoreEncryption{
			Enabled:    true,
			Enforce:    true,
			DefaultKey: "new-key",
			Algorithm:  "AES-256-GCM",
		}

		base.Merge(other)

		if !base.Enabled {
			t.Error("Enabled should be true")
		}
		if !base.Enforce {
			t.Error("Enforce should be true")
		}
		if base.DefaultKey != "new-key" {
			t.Errorf("expected DefaultKey %q, got %q", "new-key", base.DefaultKey)
		}
		if base.Algorithm != "AES-256-GCM" {
			t.Errorf("expected Algorithm %q, got %q", "AES-256-GCM", base.Algorithm)
		}
	})

	t.Run("merge does not override with empty values", func(t *testing.T) {
		base := &ObjectStoreEncryption{
			Enabled:    true,
			Enforce:    true,
			DefaultKey: "original",
			Algorithm:  "AES-256-GCM",
		}

		base.Merge(&ObjectStoreEncryption{})

		// Booleans with false don't override true
		if !base.Enabled {
			t.Error("Enabled should still be true")
		}
		if !base.Enforce {
			t.Error("Enforce should still be true")
		}
		if base.DefaultKey != "original" {
			t.Error("DefaultKey should not have been overridden")
		}
		if base.Algorithm != "AES-256-GCM" {
			t.Error("Algorithm should not have been overridden")
		}
	})
}

func TestObjectStoreEncryption_Validate(t *testing.T) {
	e := &ObjectStoreEncryption{
		Enabled:    true,
		Algorithm:  "AES-256-GCM",
		DefaultKey: "dek:test",
	}
	if err := e.Validate("test"); err != nil {
		t.Errorf("unexpected validation error: %v", err)
	}
}

func TestObjectStoreReplication_Clone(t *testing.T) {
	t.Run("nil receiver", func(t *testing.T) {
		var r *ObjectStoreReplication
		clone := r.Clone()
		if clone != nil {
			t.Error("expected nil clone for nil receiver")
		}
	})

	t.Run("full clone", func(t *testing.T) {
		original := &ObjectStoreReplication{
			Enabled:      true,
			MinReplicas:  3,
			TargetStores: []string{"store1", "store2"},
			Mode:         ReplicationModeQuorum,
			Priority:     7,
		}

		clone := original.Clone()

		if clone.Enabled != original.Enabled {
			t.Error("Enabled mismatch")
		}
		if clone.MinReplicas != original.MinReplicas {
			t.Error("MinReplicas mismatch")
		}
		if len(clone.TargetStores) != len(original.TargetStores) {
			t.Error("TargetStores length mismatch")
		}
		if clone.Mode != original.Mode {
			t.Error("Mode mismatch")
		}
		if clone.Priority != original.Priority {
			t.Error("Priority mismatch")
		}

		// Verify deep copy
		clone.TargetStores[0] = "modified"
		if original.TargetStores[0] == "modified" {
			t.Error("TargetStores was not deep copied")
		}
	})
}

func TestObjectStoreReplication_Merge(t *testing.T) {
	t.Run("merge nil", func(t *testing.T) {
		r := &ObjectStoreReplication{Enabled: true}
		r.Merge(nil)
		if !r.Enabled {
			t.Error("merge nil should not change values")
		}
	})

	t.Run("merge values", func(t *testing.T) {
		base := &ObjectStoreReplication{
			MinReplicas: 2,
		}

		other := &ObjectStoreReplication{
			Enabled:      true,
			MinReplicas:  3,
			TargetStores: []string{"store1", "store2"},
			Mode:         ReplicationModeSync,
			Priority:     9,
		}

		base.Merge(other)

		if !base.Enabled {
			t.Error("Enabled should be true")
		}
		if base.MinReplicas != 3 {
			t.Errorf("expected MinReplicas 3, got %d", base.MinReplicas)
		}
		if len(base.TargetStores) != 2 {
			t.Errorf("expected 2 TargetStores, got %d", len(base.TargetStores))
		}
		if base.Mode != ReplicationModeSync {
			t.Errorf("expected Mode %q, got %q", ReplicationModeSync, base.Mode)
		}
		if base.Priority != 9 {
			t.Errorf("expected Priority 9, got %d", base.Priority)
		}
	})

	t.Run("merge does not override with empty values", func(t *testing.T) {
		base := &ObjectStoreReplication{
			Enabled:      true,
			MinReplicas:  3,
			TargetStores: []string{"store1"},
			Mode:         ReplicationModeSync,
			Priority:     8,
		}

		base.Merge(&ObjectStoreReplication{})

		if !base.Enabled {
			t.Error("Enabled should still be true")
		}
		if base.MinReplicas != 3 {
			t.Error("MinReplicas should not have been overridden")
		}
		if len(base.TargetStores) != 1 {
			t.Error("TargetStores should not have been overridden")
		}
		if base.Mode != ReplicationModeSync {
			t.Error("Mode should not have been overridden")
		}
		if base.Priority != 8 {
			t.Error("Priority should not have been overridden")
		}
	})
}

func TestObjectStoreReplication_Validate(t *testing.T) {
	t.Run("valid config", func(t *testing.T) {
		r := &ObjectStoreReplication{
			Mode:     ReplicationModeAsync,
			Priority: 5,
		}
		if err := r.Validate("test"); err != nil {
			t.Errorf("unexpected validation error: %v", err)
		}
	})

	t.Run("all valid modes", func(t *testing.T) {
		modes := []string{ReplicationModeSync, ReplicationModeAsync, ReplicationModeQuorum}
		for _, mode := range modes {
			r := &ObjectStoreReplication{Mode: mode, Priority: 1}
			if err := r.Validate("test"); err != nil {
				t.Errorf("unexpected error for mode %q: %v", mode, err)
			}
		}
	})

	t.Run("invalid mode", func(t *testing.T) {
		r := &ObjectStoreReplication{Mode: "invalid"}
		err := r.Validate("test")
		if err == nil {
			t.Error("expected validation error for invalid mode")
		}
	})

	t.Run("priority out of range", func(t *testing.T) {
		testCases := []int{0, 11, -1, 100}
		for _, priority := range testCases {
			r := &ObjectStoreReplication{Priority: priority}
			err := r.Validate("test")
			if priority != 0 && err == nil {
				t.Errorf("expected validation error for priority %d", priority)
			}
		}
	})

	t.Run("negative min replicas", func(t *testing.T) {
		r := &ObjectStoreReplication{MinReplicas: -1}
		err := r.Validate("test")
		if err == nil {
			t.Error("expected validation error for negative min_replicas")
		}
	})
}

func TestObjectStoreArchive_Clone(t *testing.T) {
	t.Run("nil receiver", func(t *testing.T) {
		var a *ObjectStoreArchive
		clone := a.Clone()
		if clone != nil {
			t.Error("expected nil clone for nil receiver")
		}
	})

	t.Run("full clone", func(t *testing.T) {
		original := &ObjectStoreArchive{
			Enabled:           true,
			ArchiveAfterDays:  30,
			TargetStore:       "glacier",
			DeleteFromPrimary: true,
			CompressionLevel:  7,
		}

		clone := original.Clone()

		if clone.Enabled != original.Enabled {
			t.Error("Enabled mismatch")
		}
		if clone.ArchiveAfterDays != original.ArchiveAfterDays {
			t.Error("ArchiveAfterDays mismatch")
		}
		if clone.TargetStore != original.TargetStore {
			t.Error("TargetStore mismatch")
		}
		if clone.DeleteFromPrimary != original.DeleteFromPrimary {
			t.Error("DeleteFromPrimary mismatch")
		}
		if clone.CompressionLevel != original.CompressionLevel {
			t.Error("CompressionLevel mismatch")
		}
	})
}

func TestObjectStoreArchive_Merge(t *testing.T) {
	t.Run("merge nil", func(t *testing.T) {
		a := &ObjectStoreArchive{Enabled: true}
		a.Merge(nil)
		if !a.Enabled {
			t.Error("merge nil should not change values")
		}
	})

	t.Run("merge values", func(t *testing.T) {
		base := &ObjectStoreArchive{
			ArchiveAfterDays: 60,
		}

		other := &ObjectStoreArchive{
			Enabled:           true,
			ArchiveAfterDays:  30,
			TargetStore:       "cold-storage",
			DeleteFromPrimary: true,
			CompressionLevel:  8,
		}

		base.Merge(other)

		if !base.Enabled {
			t.Error("Enabled should be true")
		}
		if base.ArchiveAfterDays != 30 {
			t.Errorf("expected ArchiveAfterDays 30, got %d", base.ArchiveAfterDays)
		}
		if base.TargetStore != "cold-storage" {
			t.Errorf("expected TargetStore %q, got %q", "cold-storage", base.TargetStore)
		}
		if !base.DeleteFromPrimary {
			t.Error("DeleteFromPrimary should be true")
		}
		if base.CompressionLevel != 8 {
			t.Errorf("expected CompressionLevel 8, got %d", base.CompressionLevel)
		}
	})

	t.Run("merge does not override with empty values", func(t *testing.T) {
		base := &ObjectStoreArchive{
			Enabled:           true,
			ArchiveAfterDays:  60,
			TargetStore:       "archive",
			DeleteFromPrimary: true,
			CompressionLevel:  7,
		}

		base.Merge(&ObjectStoreArchive{})

		if !base.Enabled {
			t.Error("Enabled should still be true")
		}
		if base.ArchiveAfterDays != 60 {
			t.Error("ArchiveAfterDays should not have been overridden")
		}
		if base.TargetStore != "archive" {
			t.Error("TargetStore should not have been overridden")
		}
		if !base.DeleteFromPrimary {
			t.Error("DeleteFromPrimary should still be true")
		}
		if base.CompressionLevel != 7 {
			t.Error("CompressionLevel should not have been overridden")
		}
	})
}

func TestObjectStoreArchive_Validate(t *testing.T) {
	t.Run("valid config", func(t *testing.T) {
		a := &ObjectStoreArchive{
			CompressionLevel: 6,
			ArchiveAfterDays: 90,
		}
		if err := a.Validate("test"); err != nil {
			t.Errorf("unexpected validation error: %v", err)
		}
	})

	t.Run("compression level out of range", func(t *testing.T) {
		testCases := []int{-1, 10, 15, 100}
		for _, level := range testCases {
			a := &ObjectStoreArchive{CompressionLevel: level}
			err := a.Validate("test")
			if err == nil {
				t.Errorf("expected validation error for compression level %d", level)
			}
		}
	})

	t.Run("valid compression levels", func(t *testing.T) {
		for level := 0; level <= 9; level++ {
			a := &ObjectStoreArchive{CompressionLevel: level}
			if err := a.Validate("test"); err != nil {
				t.Errorf("unexpected error for compression level %d: %v", level, err)
			}
		}
	})

	t.Run("negative archive days", func(t *testing.T) {
		a := &ObjectStoreArchive{ArchiveAfterDays: -1}
		err := a.Validate("test")
		if err == nil {
			t.Error("expected validation error for negative archive_after_days")
		}
	})
}

func TestObjectStoreRetention_Clone(t *testing.T) {
	t.Run("nil receiver", func(t *testing.T) {
		var r *ObjectStoreRetention
		clone := r.Clone()
		if clone != nil {
			t.Error("expected nil clone for nil receiver")
		}
	})

	t.Run("full clone", func(t *testing.T) {
		original := &ObjectStoreRetention{
			Enabled:       true,
			RetentionDays: 365,
			MaxVersions:   10,
		}

		clone := original.Clone()

		if clone.Enabled != original.Enabled {
			t.Error("Enabled mismatch")
		}
		if clone.RetentionDays != original.RetentionDays {
			t.Error("RetentionDays mismatch")
		}
		if clone.MaxVersions != original.MaxVersions {
			t.Error("MaxVersions mismatch")
		}
	})
}

func TestObjectStoreRetention_Merge(t *testing.T) {
	t.Run("merge nil", func(t *testing.T) {
		r := &ObjectStoreRetention{Enabled: true}
		r.Merge(nil)
		if !r.Enabled {
			t.Error("merge nil should not change values")
		}
	})

	t.Run("merge values", func(t *testing.T) {
		base := &ObjectStoreRetention{
			RetentionDays: 30,
		}

		other := &ObjectStoreRetention{
			Enabled:       true,
			RetentionDays: 90,
			MaxVersions:   5,
		}

		base.Merge(other)

		if !base.Enabled {
			t.Error("Enabled should be true")
		}
		if base.RetentionDays != 90 {
			t.Errorf("expected RetentionDays 90, got %d", base.RetentionDays)
		}
		if base.MaxVersions != 5 {
			t.Errorf("expected MaxVersions 5, got %d", base.MaxVersions)
		}
	})

	t.Run("merge does not override with empty values", func(t *testing.T) {
		base := &ObjectStoreRetention{
			Enabled:       true,
			RetentionDays: 365,
			MaxVersions:   10,
		}

		base.Merge(&ObjectStoreRetention{})

		if !base.Enabled {
			t.Error("Enabled should still be true")
		}
		if base.RetentionDays != 365 {
			t.Error("RetentionDays should not have been overridden")
		}
		if base.MaxVersions != 10 {
			t.Error("MaxVersions should not have been overridden")
		}
	})
}

func TestObjectStoreRetention_Validate(t *testing.T) {
	t.Run("valid config", func(t *testing.T) {
		r := &ObjectStoreRetention{
			RetentionDays: 365,
			MaxVersions:   10,
		}
		if err := r.Validate("test"); err != nil {
			t.Errorf("unexpected validation error: %v", err)
		}
	})

	t.Run("zero values are valid", func(t *testing.T) {
		r := &ObjectStoreRetention{
			RetentionDays: 0,
			MaxVersions:   0,
		}
		if err := r.Validate("test"); err != nil {
			t.Errorf("unexpected validation error: %v", err)
		}
	})

	t.Run("negative retention days", func(t *testing.T) {
		r := &ObjectStoreRetention{RetentionDays: -1}
		err := r.Validate("test")
		if err == nil {
			t.Error("expected validation error for negative retention_days")
		}
	})

	t.Run("negative max versions", func(t *testing.T) {
		r := &ObjectStoreRetention{MaxVersions: -1}
		err := r.Validate("test")
		if err == nil {
			t.Error("expected validation error for negative max_versions")
		}
	})
}

func TestObjectStoreQuotas_Clone(t *testing.T) {
	t.Run("nil receiver", func(t *testing.T) {
		var q *ObjectStoreQuotas
		clone := q.Clone()
		if clone != nil {
			t.Error("expected nil clone for nil receiver")
		}
	})

	t.Run("full clone", func(t *testing.T) {
		original := &ObjectStoreQuotas{
			MaxStorageBytes: 1073741824,
			MaxObjectSize:   104857600,
			MaxObjectCount:  10000,
		}

		clone := original.Clone()

		if clone.MaxStorageBytes != original.MaxStorageBytes {
			t.Error("MaxStorageBytes mismatch")
		}
		if clone.MaxObjectSize != original.MaxObjectSize {
			t.Error("MaxObjectSize mismatch")
		}
		if clone.MaxObjectCount != original.MaxObjectCount {
			t.Error("MaxObjectCount mismatch")
		}
	})
}

func TestObjectStoreQuotas_Merge(t *testing.T) {
	t.Run("merge nil", func(t *testing.T) {
		q := &ObjectStoreQuotas{MaxStorageBytes: 1000}
		q.Merge(nil)
		if q.MaxStorageBytes != 1000 {
			t.Error("merge nil should not change values")
		}
	})

	t.Run("merge values", func(t *testing.T) {
		base := &ObjectStoreQuotas{
			MaxStorageBytes: 1000,
		}

		other := &ObjectStoreQuotas{
			MaxStorageBytes: 2000,
			MaxObjectSize:   500,
			MaxObjectCount:  100,
		}

		base.Merge(other)

		if base.MaxStorageBytes != 2000 {
			t.Errorf("expected MaxStorageBytes 2000, got %d", base.MaxStorageBytes)
		}
		if base.MaxObjectSize != 500 {
			t.Errorf("expected MaxObjectSize 500, got %d", base.MaxObjectSize)
		}
		if base.MaxObjectCount != 100 {
			t.Errorf("expected MaxObjectCount 100, got %d", base.MaxObjectCount)
		}
	})

	t.Run("merge does not override with empty values", func(t *testing.T) {
		base := &ObjectStoreQuotas{
			MaxStorageBytes: 1000,
			MaxObjectSize:   500,
			MaxObjectCount:  100,
		}

		base.Merge(&ObjectStoreQuotas{})

		if base.MaxStorageBytes != 1000 {
			t.Error("MaxStorageBytes should not have been overridden")
		}
		if base.MaxObjectSize != 500 {
			t.Error("MaxObjectSize should not have been overridden")
		}
		if base.MaxObjectCount != 100 {
			t.Error("MaxObjectCount should not have been overridden")
		}
	})
}

func TestObjectStoreQuotas_Validate(t *testing.T) {
	t.Run("valid config", func(t *testing.T) {
		q := &ObjectStoreQuotas{
			MaxStorageBytes: 1073741824,
			MaxObjectSize:   104857600,
			MaxObjectCount:  10000,
		}
		if err := q.Validate("test"); err != nil {
			t.Errorf("unexpected validation error: %v", err)
		}
	})

	t.Run("zero values are valid", func(t *testing.T) {
		q := &ObjectStoreQuotas{}
		if err := q.Validate("test"); err != nil {
			t.Errorf("unexpected validation error: %v", err)
		}
	})

	t.Run("negative storage bytes", func(t *testing.T) {
		q := &ObjectStoreQuotas{MaxStorageBytes: -1}
		err := q.Validate("test")
		if err == nil {
			t.Error("expected validation error for negative max_storage_bytes")
		}
	})

	t.Run("negative object size", func(t *testing.T) {
		q := &ObjectStoreQuotas{MaxObjectSize: -1}
		err := q.Validate("test")
		if err == nil {
			t.Error("expected validation error for negative max_object_size")
		}
	})

	t.Run("negative object count", func(t *testing.T) {
		q := &ObjectStoreQuotas{MaxObjectCount: -1}
		err := q.Validate("test")
		if err == nil {
			t.Error("expected validation error for negative max_object_count")
		}
	})
}

func TestObjectStoreShardConfig_Clone(t *testing.T) {
	t.Run("nil receiver", func(t *testing.T) {
		var s *ObjectStoreShardConfig
		clone := s.Clone()
		if clone != nil {
			t.Error("expected nil clone for nil receiver")
		}
	})

	t.Run("full clone", func(t *testing.T) {
		original := &ObjectStoreShardConfig{
			PrimaryBackend: "fast-storage",
			Replication: &ObjectStoreReplication{
				Enabled:      true,
				MinReplicas:  2,
				TargetStores: []string{"store1", "store2"},
			},
		}

		clone := original.Clone()

		if clone.PrimaryBackend != original.PrimaryBackend {
			t.Error("PrimaryBackend mismatch")
		}
		if clone.Replication == nil {
			t.Fatal("Replication should not be nil")
		}
		if clone.Replication.MinReplicas != original.Replication.MinReplicas {
			t.Error("Replication.MinReplicas mismatch")
		}

		// Verify deep copy
		clone.Replication.TargetStores[0] = "modified"
		if original.Replication.TargetStores[0] == "modified" {
			t.Error("Replication was not deep copied")
		}
	})

	t.Run("clone with nil replication", func(t *testing.T) {
		original := &ObjectStoreShardConfig{
			PrimaryBackend: "storage",
		}

		clone := original.Clone()

		if clone.Replication != nil {
			t.Error("Replication should be nil")
		}
	})
}

func TestObjectStoreShardConfig_Merge(t *testing.T) {
	t.Run("merge nil", func(t *testing.T) {
		s := &ObjectStoreShardConfig{PrimaryBackend: "test"}
		s.Merge(nil)
		if s.PrimaryBackend != "test" {
			t.Error("merge nil should not change values")
		}
	})

	t.Run("merge values", func(t *testing.T) {
		base := &ObjectStoreShardConfig{
			PrimaryBackend: "old",
		}

		other := &ObjectStoreShardConfig{
			PrimaryBackend: "new",
			Replication: &ObjectStoreReplication{
				Enabled:     true,
				MinReplicas: 3,
			},
		}

		base.Merge(other)

		if base.PrimaryBackend != "new" {
			t.Errorf("expected PrimaryBackend %q, got %q", "new", base.PrimaryBackend)
		}
		if base.Replication == nil {
			t.Fatal("Replication should not be nil")
		}
		if base.Replication.MinReplicas != 3 {
			t.Errorf("expected MinReplicas 3, got %d", base.Replication.MinReplicas)
		}
	})

	t.Run("merge with existing replication", func(t *testing.T) {
		base := &ObjectStoreShardConfig{
			Replication: &ObjectStoreReplication{
				MinReplicas: 2,
				Mode:        ReplicationModeAsync,
			},
		}

		other := &ObjectStoreShardConfig{
			Replication: &ObjectStoreReplication{
				MinReplicas: 3,
			},
		}

		base.Merge(other)

		if base.Replication.MinReplicas != 3 {
			t.Errorf("expected MinReplicas 3, got %d", base.Replication.MinReplicas)
		}
	})

	t.Run("merge does not override with empty values", func(t *testing.T) {
		base := &ObjectStoreShardConfig{PrimaryBackend: "primary"}

		base.Merge(&ObjectStoreShardConfig{})

		if base.PrimaryBackend != "primary" {
			t.Error("PrimaryBackend should not have been overridden")
		}
	})
}

func TestObjectStoreShardConfig_Validate(t *testing.T) {
	t.Run("valid config", func(t *testing.T) {
		s := &ObjectStoreShardConfig{
			PrimaryBackend: "storage",
			Replication: &ObjectStoreReplication{
				Mode:     ReplicationModeSync,
				Priority: 5,
			},
		}
		if err := s.Validate("test"); err != nil {
			t.Errorf("unexpected validation error: %v", err)
		}
	})

	t.Run("nil replication is valid", func(t *testing.T) {
		s := &ObjectStoreShardConfig{
			PrimaryBackend: "storage",
		}
		if err := s.Validate("test"); err != nil {
			t.Errorf("unexpected validation error: %v", err)
		}
	})

	t.Run("invalid replication", func(t *testing.T) {
		s := &ObjectStoreShardConfig{
			Replication: &ObjectStoreReplication{
				Mode:     "invalid",
				Priority: 15,
			},
		}
		err := s.Validate("test")
		if err == nil {
			t.Error("expected validation error for invalid replication")
		}
	})
}

func TestObjectStoreTenantConfig_Clone(t *testing.T) {
	t.Run("nil receiver", func(t *testing.T) {
		var tc *ObjectStoreTenantConfig
		clone := tc.Clone()
		if clone != nil {
			t.Error("expected nil clone for nil receiver")
		}
	})

	t.Run("full clone", func(t *testing.T) {
		original := &ObjectStoreTenantConfig{
			PrimaryBackend:  "s3-primary",
			AllowedBackends: []string{"s3-primary", "gcs-backup"},
			Encryption: &ObjectStoreEncryption{
				Enabled:    true,
				DefaultKey: "dek:tenant/test",
			},
			Replication: &ObjectStoreReplication{
				Enabled:      true,
				MinReplicas:  3,
				TargetStores: []string{"s3-primary"},
			},
			Archive: &ObjectStoreArchive{
				Enabled:          true,
				ArchiveAfterDays: 60,
			},
			Retention: &ObjectStoreRetention{
				Enabled:       true,
				RetentionDays: 365,
			},
			Quotas: ObjectStoreQuotas{
				MaxStorageBytes: 1073741824,
			},
			ShardConfigs: map[uint64]*ObjectStoreShardConfig{
				1001: {PrimaryBackend: "fast"},
				1002: {PrimaryBackend: "slow"},
			},
		}

		clone := original.Clone()

		if clone.PrimaryBackend != original.PrimaryBackend {
			t.Error("PrimaryBackend mismatch")
		}
		if len(clone.AllowedBackends) != len(original.AllowedBackends) {
			t.Error("AllowedBackends length mismatch")
		}
		if clone.Encryption == nil || clone.Encryption.DefaultKey != original.Encryption.DefaultKey {
			t.Error("Encryption mismatch")
		}
		if clone.Replication == nil || clone.Replication.MinReplicas != original.Replication.MinReplicas {
			t.Error("Replication mismatch")
		}
		if clone.Archive == nil || clone.Archive.ArchiveAfterDays != original.Archive.ArchiveAfterDays {
			t.Error("Archive mismatch")
		}
		if clone.Retention == nil || clone.Retention.RetentionDays != original.Retention.RetentionDays {
			t.Error("Retention mismatch")
		}
		if clone.Quotas.MaxStorageBytes != original.Quotas.MaxStorageBytes {
			t.Error("Quotas mismatch")
		}
		if len(clone.ShardConfigs) != len(original.ShardConfigs) {
			t.Error("ShardConfigs length mismatch")
		}

		// Verify deep copy
		clone.AllowedBackends[0] = "modified"
		if original.AllowedBackends[0] == "modified" {
			t.Error("AllowedBackends was not deep copied")
		}

		clone.ShardConfigs[1001].PrimaryBackend = "modified"
		if original.ShardConfigs[1001].PrimaryBackend == "modified" {
			t.Error("ShardConfigs was not deep copied")
		}
	})

	t.Run("clone with nil optional fields", func(t *testing.T) {
		original := &ObjectStoreTenantConfig{
			PrimaryBackend: "storage",
		}

		clone := original.Clone()

		if clone.Encryption != nil {
			t.Error("Encryption should be nil")
		}
		if clone.Replication != nil {
			t.Error("Replication should be nil")
		}
		if clone.Archive != nil {
			t.Error("Archive should be nil")
		}
		if clone.Retention != nil {
			t.Error("Retention should be nil")
		}
	})
}

func TestObjectStoreTenantConfig_Merge(t *testing.T) {
	t.Run("merge nil", func(t *testing.T) {
		tc := &ObjectStoreTenantConfig{PrimaryBackend: "test"}
		tc.Merge(nil)
		if tc.PrimaryBackend != "test" {
			t.Error("merge nil should not change values")
		}
	})

	t.Run("merge values", func(t *testing.T) {
		base := &ObjectStoreTenantConfig{
			PrimaryBackend: "old",
		}

		other := &ObjectStoreTenantConfig{
			PrimaryBackend:  "new",
			AllowedBackends: []string{"backend1", "backend2"},
			Encryption: &ObjectStoreEncryption{
				Enabled: true,
			},
			Replication: &ObjectStoreReplication{
				MinReplicas: 3,
			},
			Archive: &ObjectStoreArchive{
				Enabled: true,
			},
			Retention: &ObjectStoreRetention{
				RetentionDays: 30,
			},
			Quotas: ObjectStoreQuotas{
				MaxStorageBytes: 5000,
			},
			ShardConfigs: map[uint64]*ObjectStoreShardConfig{
				1001: {PrimaryBackend: "fast"},
			},
		}

		base.Merge(other)

		if base.PrimaryBackend != "new" {
			t.Errorf("expected PrimaryBackend %q, got %q", "new", base.PrimaryBackend)
		}
		if len(base.AllowedBackends) != 2 {
			t.Errorf("expected 2 AllowedBackends, got %d", len(base.AllowedBackends))
		}
		if base.Encryption == nil || !base.Encryption.Enabled {
			t.Error("Encryption should be enabled")
		}
		if base.Replication == nil || base.Replication.MinReplicas != 3 {
			t.Error("Replication.MinReplicas should be 3")
		}
		if base.Archive == nil || !base.Archive.Enabled {
			t.Error("Archive should be enabled")
		}
		if base.Retention == nil || base.Retention.RetentionDays != 30 {
			t.Error("Retention.RetentionDays should be 30")
		}
		if base.Quotas.MaxStorageBytes != 5000 {
			t.Errorf("expected MaxStorageBytes 5000, got %d", base.Quotas.MaxStorageBytes)
		}
		if len(base.ShardConfigs) != 1 {
			t.Errorf("expected 1 ShardConfig, got %d", len(base.ShardConfigs))
		}
	})

	t.Run("merge shard configs with existing", func(t *testing.T) {
		base := &ObjectStoreTenantConfig{
			ShardConfigs: map[uint64]*ObjectStoreShardConfig{
				1001: {PrimaryBackend: "old"},
			},
		}

		other := &ObjectStoreTenantConfig{
			ShardConfigs: map[uint64]*ObjectStoreShardConfig{
				1001: {PrimaryBackend: "new"},
				1002: {PrimaryBackend: "added"},
			},
		}

		base.Merge(other)

		if base.ShardConfigs[1001].PrimaryBackend != "new" {
			t.Error("existing shard config should be merged")
		}
		if base.ShardConfigs[1002] == nil {
			t.Error("new shard config should be added")
		}
	})

	t.Run("merge does not override with empty values", func(t *testing.T) {
		base := &ObjectStoreTenantConfig{
			PrimaryBackend:  "primary",
			AllowedBackends: []string{"backend1"},
		}

		base.Merge(&ObjectStoreTenantConfig{})

		if base.PrimaryBackend != "primary" {
			t.Error("PrimaryBackend should not have been overridden")
		}
		if len(base.AllowedBackends) != 1 {
			t.Error("AllowedBackends should not have been overridden")
		}
	})
}

func TestObjectStoreTenantConfig_Validate(t *testing.T) {
	t.Run("valid config", func(t *testing.T) {
		tc := &ObjectStoreTenantConfig{
			PrimaryBackend:  "s3-primary",
			AllowedBackends: []string{"s3-primary", "gcs-backup"},
			Encryption: &ObjectStoreEncryption{
				Enabled: true,
			},
			Replication: &ObjectStoreReplication{
				Mode:     ReplicationModeAsync,
				Priority: 5,
			},
			Archive: &ObjectStoreArchive{
				CompressionLevel: 6,
			},
			Retention: &ObjectStoreRetention{
				RetentionDays: 365,
			},
			Quotas: ObjectStoreQuotas{
				MaxStorageBytes: 1073741824,
			},
		}
		if err := tc.Validate("test"); err != nil {
			t.Errorf("unexpected validation error: %v", err)
		}
	})

	t.Run("primary not in allowed", func(t *testing.T) {
		tc := &ObjectStoreTenantConfig{
			PrimaryBackend:  "s3-primary",
			AllowedBackends: []string{"gcs-only"}, // Primary not in list
		}
		err := tc.Validate("test")
		if err == nil {
			t.Error("expected validation error when primary not in allowed backends")
		}
	})

	t.Run("invalid nested configs", func(t *testing.T) {
		tc := &ObjectStoreTenantConfig{
			Replication: &ObjectStoreReplication{
				Mode:     "invalid",
				Priority: 20,
			},
			Archive: &ObjectStoreArchive{
				CompressionLevel: 15,
			},
			Retention: &ObjectStoreRetention{
				RetentionDays: -1,
			},
			Quotas: ObjectStoreQuotas{
				MaxStorageBytes: -1,
			},
			ShardConfigs: map[uint64]*ObjectStoreShardConfig{
				1001: {
					Replication: &ObjectStoreReplication{
						Priority: 15,
					},
				},
			},
		}
		err := tc.Validate("test")
		if err == nil {
			t.Error("expected validation errors for invalid nested configs")
		}
		verrs, ok := err.(ValidationErrors)
		if !ok {
			t.Fatalf("expected ValidationErrors, got %T", err)
		}
		if len(verrs) < 5 {
			t.Errorf("expected at least 5 validation errors, got %d", len(verrs))
		}
	})
}

func TestBackendOverride_Clone(t *testing.T) {
	t.Run("nil receiver", func(t *testing.T) {
		var b *BackendOverride
		clone := b.Clone()
		if clone != nil {
			t.Error("expected nil clone for nil receiver")
		}
	})

	t.Run("full clone", func(t *testing.T) {
		original := &BackendOverride{
			Config: map[string]string{
				"path":     "/custom/path",
				"endpoint": "custom-endpoint",
			},
		}

		clone := original.Clone()

		if len(clone.Config) != len(original.Config) {
			t.Error("Config length mismatch")
		}
		if clone.Config["path"] != original.Config["path"] {
			t.Error("Config value mismatch")
		}

		// Verify deep copy
		clone.Config["path"] = "modified"
		if original.Config["path"] == "modified" {
			t.Error("Config was not deep copied")
		}
	})

	t.Run("clone with nil config", func(t *testing.T) {
		original := &BackendOverride{}
		clone := original.Clone()
		if clone.Config != nil {
			t.Error("Config should be nil")
		}
	})
}

func TestBackendOverride_Merge(t *testing.T) {
	t.Run("merge nil", func(t *testing.T) {
		b := &BackendOverride{Config: map[string]string{"key": "value"}}
		b.Merge(nil)
		if b.Config["key"] != "value" {
			t.Error("merge nil should not change values")
		}
	})

	t.Run("merge values", func(t *testing.T) {
		base := &BackendOverride{
			Config: map[string]string{
				"key1": "value1",
			},
		}

		other := &BackendOverride{
			Config: map[string]string{
				"key1": "new-value1",
				"key2": "value2",
			},
		}

		base.Merge(other)

		if base.Config["key1"] != "new-value1" {
			t.Errorf("expected key1 %q, got %q", "new-value1", base.Config["key1"])
		}
		if base.Config["key2"] != "value2" {
			t.Errorf("expected key2 %q, got %q", "value2", base.Config["key2"])
		}
	})

	t.Run("merge into nil config", func(t *testing.T) {
		base := &BackendOverride{}

		other := &BackendOverride{
			Config: map[string]string{
				"key": "value",
			},
		}

		base.Merge(other)

		if base.Config == nil {
			t.Fatal("Config should not be nil after merge")
		}
		if base.Config["key"] != "value" {
			t.Errorf("expected key %q, got %q", "value", base.Config["key"])
		}
	})

	t.Run("merge nil config leaves config nil", func(t *testing.T) {
		base := &BackendOverride{}

		base.Merge(&BackendOverride{})

		if base.Config != nil {
			t.Error("Config should still be nil")
		}
	})
}

func TestNodeObjectStorageConfig_Clone(t *testing.T) {
	t.Run("nil receiver", func(t *testing.T) {
		var n *NodeObjectStorageConfig
		clone := n.Clone()
		if clone != nil {
			t.Error("expected nil clone for nil receiver")
		}
	})

	t.Run("full clone", func(t *testing.T) {
		original := &NodeObjectStorageConfig{
			InternalBackend: "local-storage",
			CacheBackend:    "cache-backend",
			CacheMaxSize:    10737418240,
			Encryption: &ObjectStoreEncryption{
				Enabled:    true,
				DefaultKey: "dek:node/test",
			},
			BackendOverrides: map[string]*BackendOverride{
				"local-cache": {
					Config: map[string]string{"path": "/nvme/cache"},
				},
			},
			EnabledBackends: []string{"s3-primary", "local-cache"},
		}

		clone := original.Clone()

		if clone.InternalBackend != original.InternalBackend {
			t.Error("InternalBackend mismatch")
		}
		if clone.CacheBackend != original.CacheBackend {
			t.Error("CacheBackend mismatch")
		}
		if clone.CacheMaxSize != original.CacheMaxSize {
			t.Error("CacheMaxSize mismatch")
		}
		if clone.Encryption == nil || clone.Encryption.DefaultKey != original.Encryption.DefaultKey {
			t.Error("Encryption mismatch")
		}
		if len(clone.BackendOverrides) != len(original.BackendOverrides) {
			t.Error("BackendOverrides length mismatch")
		}
		if len(clone.EnabledBackends) != len(original.EnabledBackends) {
			t.Error("EnabledBackends length mismatch")
		}

		// Verify deep copy
		clone.EnabledBackends[0] = "modified"
		if original.EnabledBackends[0] == "modified" {
			t.Error("EnabledBackends was not deep copied")
		}

		clone.BackendOverrides["local-cache"].Config["path"] = "modified"
		if original.BackendOverrides["local-cache"].Config["path"] == "modified" {
			t.Error("BackendOverrides was not deep copied")
		}
	})

	t.Run("clone with nil optional fields", func(t *testing.T) {
		original := &NodeObjectStorageConfig{
			InternalBackend: "storage",
		}

		clone := original.Clone()

		if clone.Encryption != nil {
			t.Error("Encryption should be nil")
		}
		if clone.BackendOverrides != nil {
			t.Error("BackendOverrides should be nil")
		}
		if clone.EnabledBackends != nil {
			t.Error("EnabledBackends should be nil")
		}
	})
}

func TestNodeObjectStorageConfig_Merge(t *testing.T) {
	t.Run("merge nil", func(t *testing.T) {
		n := &NodeObjectStorageConfig{InternalBackend: "test"}
		n.Merge(nil)
		if n.InternalBackend != "test" {
			t.Error("merge nil should not change values")
		}
	})

	t.Run("merge values", func(t *testing.T) {
		base := &NodeObjectStorageConfig{
			InternalBackend: "old",
		}

		other := &NodeObjectStorageConfig{
			InternalBackend: "new",
			CacheBackend:    "cache",
			CacheMaxSize:    5000,
			Encryption: &ObjectStoreEncryption{
				Enabled: true,
			},
			BackendOverrides: map[string]*BackendOverride{
				"backend1": {Config: map[string]string{"key": "value"}},
			},
			EnabledBackends: []string{"backend1", "backend2"},
		}

		base.Merge(other)

		if base.InternalBackend != "new" {
			t.Errorf("expected InternalBackend %q, got %q", "new", base.InternalBackend)
		}
		if base.CacheBackend != "cache" {
			t.Errorf("expected CacheBackend %q, got %q", "cache", base.CacheBackend)
		}
		if base.CacheMaxSize != 5000 {
			t.Errorf("expected CacheMaxSize 5000, got %d", base.CacheMaxSize)
		}
		if base.Encryption == nil || !base.Encryption.Enabled {
			t.Error("Encryption should be enabled")
		}
		if len(base.BackendOverrides) != 1 {
			t.Errorf("expected 1 BackendOverride, got %d", len(base.BackendOverrides))
		}
		if len(base.EnabledBackends) != 2 {
			t.Errorf("expected 2 EnabledBackends, got %d", len(base.EnabledBackends))
		}
	})

	t.Run("merge backend overrides with existing", func(t *testing.T) {
		base := &NodeObjectStorageConfig{
			BackendOverrides: map[string]*BackendOverride{
				"backend1": {Config: map[string]string{"key1": "old"}},
			},
		}

		other := &NodeObjectStorageConfig{
			BackendOverrides: map[string]*BackendOverride{
				"backend1": {Config: map[string]string{"key1": "new", "key2": "added"}},
				"backend2": {Config: map[string]string{"key": "value"}},
			},
		}

		base.Merge(other)

		if base.BackendOverrides["backend1"].Config["key1"] != "new" {
			t.Error("existing override should be merged")
		}
		if base.BackendOverrides["backend1"].Config["key2"] != "added" {
			t.Error("new key should be added to existing override")
		}
		if base.BackendOverrides["backend2"] == nil {
			t.Error("new override should be added")
		}
	})

	t.Run("merge does not override with empty values", func(t *testing.T) {
		base := &NodeObjectStorageConfig{
			InternalBackend: "local",
			CacheBackend:    "cache",
			CacheMaxSize:    5000,
		}

		base.Merge(&NodeObjectStorageConfig{})

		if base.InternalBackend != "local" {
			t.Error("InternalBackend should not have been overridden")
		}
		if base.CacheBackend != "cache" {
			t.Error("CacheBackend should not have been overridden")
		}
		if base.CacheMaxSize != 5000 {
			t.Error("CacheMaxSize should not have been overridden")
		}
	})
}

func TestNodeObjectStorageConfig_Validate(t *testing.T) {
	t.Run("valid config", func(t *testing.T) {
		n := &NodeObjectStorageConfig{
			InternalBackend: "local",
			CacheMaxSize:    10737418240,
		}
		if err := n.Validate(); err != nil {
			t.Errorf("unexpected validation error: %v", err)
		}
	})

	t.Run("negative cache size", func(t *testing.T) {
		n := &NodeObjectStorageConfig{
			CacheMaxSize: -1,
		}
		err := n.Validate()
		if err == nil {
			t.Error("expected validation error for negative cache_max_size")
		}
	})

	t.Run("zero cache size is valid", func(t *testing.T) {
		n := &NodeObjectStorageConfig{
			CacheMaxSize: 0,
		}
		if err := n.Validate(); err != nil {
			t.Errorf("unexpected validation error: %v", err)
		}
	})
}

func TestObjectStoreFullConfig_Clone(t *testing.T) {
	t.Run("nil receiver", func(t *testing.T) {
		var c *ObjectStoreFullConfig
		clone := c.Clone()
		if clone != nil {
			t.Error("expected nil clone for nil receiver")
		}
	})

	t.Run("full clone", func(t *testing.T) {
		original := &ObjectStoreFullConfig{
			Backends: map[string]*StorageBackendConfig{
				"s3-primary": {
					Type:        "s3",
					Description: "Primary S3 bucket",
					Config: map[string]string{
						"bucket": "my-bucket",
						"region": "us-east-1",
					},
				},
			},
			Defaults: ObjectStoreDefaults{
				PrimaryBackend: "s3-primary",
				Compression:    CompressionZstd,
			},
			Tenants: map[string]*ObjectStoreTenantConfig{
				"tenant-acme": {
					PrimaryBackend: "s3-primary",
				},
			},
		}

		clone := original.Clone()

		if len(clone.Backends) != len(original.Backends) {
			t.Error("Backends length mismatch")
		}
		if clone.Backends["s3-primary"].Type != original.Backends["s3-primary"].Type {
			t.Error("Backend type mismatch")
		}
		if clone.Defaults.PrimaryBackend != original.Defaults.PrimaryBackend {
			t.Error("Defaults.PrimaryBackend mismatch")
		}
		if len(clone.Tenants) != len(original.Tenants) {
			t.Error("Tenants length mismatch")
		}

		// Verify deep copy
		clone.Backends["s3-primary"].Config["bucket"] = "modified"
		if original.Backends["s3-primary"].Config["bucket"] == "modified" {
			t.Error("Backends was not deep copied")
		}

		clone.Tenants["tenant-acme"].PrimaryBackend = "modified"
		if original.Tenants["tenant-acme"].PrimaryBackend == "modified" {
			t.Error("Tenants was not deep copied")
		}
	})

	t.Run("clone with nil optional fields", func(t *testing.T) {
		original := &ObjectStoreFullConfig{
			Defaults: ObjectStoreDefaults{
				Compression: CompressionZstd,
			},
		}

		clone := original.Clone()

		if clone.Backends != nil {
			t.Error("Backends should be nil")
		}
		if clone.Tenants != nil {
			t.Error("Tenants should be nil")
		}
	})
}

func TestObjectStoreFullConfig_Merge(t *testing.T) {
	t.Run("merge nil", func(t *testing.T) {
		c := &ObjectStoreFullConfig{
			Defaults: ObjectStoreDefaults{PrimaryBackend: "test"},
		}
		c.Merge(nil)
		if c.Defaults.PrimaryBackend != "test" {
			t.Error("merge nil should not change values")
		}
	})

	t.Run("merge values", func(t *testing.T) {
		base := &ObjectStoreFullConfig{
			Defaults: ObjectStoreDefaults{
				PrimaryBackend: "old",
			},
		}

		other := &ObjectStoreFullConfig{
			Backends: map[string]*StorageBackendConfig{
				"new-backend": {Type: "s3"},
			},
			Defaults: ObjectStoreDefaults{
				PrimaryBackend: "new",
			},
			Tenants: map[string]*ObjectStoreTenantConfig{
				"tenant1": {PrimaryBackend: "new-backend"},
			},
		}

		base.Merge(other)

		if len(base.Backends) != 1 {
			t.Errorf("expected 1 backend, got %d", len(base.Backends))
		}
		if base.Defaults.PrimaryBackend != "new" {
			t.Errorf("expected PrimaryBackend %q, got %q", "new", base.Defaults.PrimaryBackend)
		}
		if len(base.Tenants) != 1 {
			t.Errorf("expected 1 tenant, got %d", len(base.Tenants))
		}
	})

	t.Run("merge tenants with existing", func(t *testing.T) {
		base := &ObjectStoreFullConfig{
			Tenants: map[string]*ObjectStoreTenantConfig{
				"tenant1": {PrimaryBackend: "old"},
			},
		}

		other := &ObjectStoreFullConfig{
			Tenants: map[string]*ObjectStoreTenantConfig{
				"tenant1": {PrimaryBackend: "new"},
				"tenant2": {PrimaryBackend: "added"},
			},
		}

		base.Merge(other)

		if base.Tenants["tenant1"].PrimaryBackend != "new" {
			t.Error("existing tenant should be merged")
		}
		if base.Tenants["tenant2"] == nil {
			t.Error("new tenant should be added")
		}
	})

	t.Run("merge backends replaces existing and copies config", func(t *testing.T) {
		base := &ObjectStoreFullConfig{
			Backends: map[string]*StorageBackendConfig{
				"existing": {Type: "s3", Description: "old"},
			},
		}

		other := &ObjectStoreFullConfig{
			Backends: map[string]*StorageBackendConfig{
				"existing": {Type: "gcs", Description: "new"},
				"added":    {Type: "local", Config: map[string]string{"path": "/data"}},
			},
		}

		base.Merge(other)

		// Backends are replaced, not merged
		if base.Backends["existing"].Type != "gcs" {
			t.Error("existing backend should be replaced")
		}
		if base.Backends["existing"].Config != nil {
			t.Error("a backend merged without Config should keep a nil Config")
		}
		added := base.Backends["added"]
		if added == nil {
			t.Fatal("new backend should be added")
		}
		other.Backends["added"].Config["path"] = "/changed"
		if added.Config["path"] != "/data" {
			t.Error("merged backend Config should be a copy")
		}
	})
}

func TestObjectStoreFullConfig_Validate(t *testing.T) {
	t.Run("valid config", func(t *testing.T) {
		c := &ObjectStoreFullConfig{
			Backends: map[string]*StorageBackendConfig{
				"s3-primary": {Type: "s3"},
				"gcs-backup": {Type: "gcs"},
				"archive":    {Type: "s3"},
			},
			Defaults: ObjectStoreDefaults{
				PrimaryBackend:   "s3-primary",
				RequiredBackends: []string{"s3-primary"},
				Compression:      CompressionZstd,
				Replication: ObjectStoreReplication{
					TargetStores: []string{"s3-primary", "gcs-backup"},
				},
				Archive: ObjectStoreArchive{
					TargetStore: "archive",
				},
			},
			Tenants: map[string]*ObjectStoreTenantConfig{
				"tenant-acme": {
					PrimaryBackend:  "s3-primary",
					AllowedBackends: []string{"s3-primary", "gcs-backup"},
					Replication: &ObjectStoreReplication{
						TargetStores: []string{"s3-primary"},
					},
				},
			},
		}
		if err := c.Validate(); err != nil {
			t.Errorf("unexpected validation error: %v", err)
		}
	})

	t.Run("undefined primary backend", func(t *testing.T) {
		c := &ObjectStoreFullConfig{
			Backends: map[string]*StorageBackendConfig{
				"existing": {Type: "s3"},
			},
			Defaults: ObjectStoreDefaults{
				PrimaryBackend: "nonexistent",
				Compression:    CompressionZstd,
			},
		}
		err := c.Validate()
		if err == nil {
			t.Error("expected validation error for undefined primary backend")
		}
	})

	t.Run("undefined required backend", func(t *testing.T) {
		c := &ObjectStoreFullConfig{
			Backends: map[string]*StorageBackendConfig{
				"existing": {Type: "s3"},
			},
			Defaults: ObjectStoreDefaults{
				RequiredBackends: []string{"nonexistent"},
				Compression:      CompressionZstd,
			},
		}
		err := c.Validate()
		if err == nil {
			t.Error("expected validation error for undefined required backend")
		}
	})

	t.Run("undefined replication target", func(t *testing.T) {
		c := &ObjectStoreFullConfig{
			Backends: map[string]*StorageBackendConfig{
				"existing": {Type: "s3"},
			},
			Defaults: ObjectStoreDefaults{
				Compression: CompressionZstd,
				Replication: ObjectStoreReplication{
					TargetStores: []string{"nonexistent"},
				},
			},
		}
		err := c.Validate()
		if err == nil {
			t.Error("expected validation error for undefined replication target")
		}
	})

	t.Run("undefined archive target", func(t *testing.T) {
		c := &ObjectStoreFullConfig{
			Backends: map[string]*StorageBackendConfig{
				"existing": {Type: "s3"},
			},
			Defaults: ObjectStoreDefaults{
				Compression: CompressionZstd,
				Archive: ObjectStoreArchive{
					TargetStore: "nonexistent",
				},
			},
		}
		err := c.Validate()
		if err == nil {
			t.Error("expected validation error for undefined archive target")
		}
	})

	t.Run("undefined tenant primary backend", func(t *testing.T) {
		c := &ObjectStoreFullConfig{
			Backends: map[string]*StorageBackendConfig{
				"existing": {Type: "s3"},
			},
			Defaults: ObjectStoreDefaults{
				Compression: CompressionZstd,
			},
			Tenants: map[string]*ObjectStoreTenantConfig{
				"tenant1": {PrimaryBackend: "nonexistent"},
			},
		}
		err := c.Validate()
		if err == nil {
			t.Error("expected validation error for undefined tenant primary backend")
		}
	})

	t.Run("undefined tenant allowed backend", func(t *testing.T) {
		c := &ObjectStoreFullConfig{
			Backends: map[string]*StorageBackendConfig{
				"existing": {Type: "s3"},
			},
			Defaults: ObjectStoreDefaults{
				Compression: CompressionZstd,
			},
			Tenants: map[string]*ObjectStoreTenantConfig{
				"tenant1": {AllowedBackends: []string{"nonexistent"}},
			},
		}
		err := c.Validate()
		if err == nil {
			t.Error("expected validation error for undefined tenant allowed backend")
		}
	})

	t.Run("undefined tenant replication target", func(t *testing.T) {
		c := &ObjectStoreFullConfig{
			Backends: map[string]*StorageBackendConfig{
				"existing": {Type: "s3"},
			},
			Defaults: ObjectStoreDefaults{
				Compression: CompressionZstd,
			},
			Tenants: map[string]*ObjectStoreTenantConfig{
				"tenant1": {
					Replication: &ObjectStoreReplication{
						TargetStores: []string{"nonexistent"},
					},
				},
			},
		}
		err := c.Validate()
		if err == nil {
			t.Error("expected validation error for undefined tenant replication target")
		}
	})

	t.Run("undefined shard primary backend", func(t *testing.T) {
		c := &ObjectStoreFullConfig{
			Backends: map[string]*StorageBackendConfig{
				"existing": {Type: "s3"},
			},
			Defaults: ObjectStoreDefaults{
				Compression: CompressionZstd,
			},
			Tenants: map[string]*ObjectStoreTenantConfig{
				"tenant1": {
					ShardConfigs: map[uint64]*ObjectStoreShardConfig{
						1001: {PrimaryBackend: "nonexistent"},
					},
				},
			},
		}
		err := c.Validate()
		if err == nil {
			t.Error("expected validation error for undefined shard primary backend")
		}
	})

	t.Run("undefined shard replication target", func(t *testing.T) {
		c := &ObjectStoreFullConfig{
			Backends: map[string]*StorageBackendConfig{
				"existing": {Type: "s3"},
			},
			Defaults: ObjectStoreDefaults{
				Compression: CompressionZstd,
			},
			Tenants: map[string]*ObjectStoreTenantConfig{
				"tenant1": {
					ShardConfigs: map[uint64]*ObjectStoreShardConfig{
						1001: {
							Replication: &ObjectStoreReplication{
								TargetStores: []string{"nonexistent"},
							},
						},
					},
				},
			},
		}
		err := c.Validate()
		if err == nil {
			t.Error("expected validation error for undefined shard replication target")
		}
	})

	t.Run("invalid defaults propagate", func(t *testing.T) {
		c := &ObjectStoreFullConfig{
			Defaults: ObjectStoreDefaults{
				Compression: "invalid",
			},
		}
		err := c.Validate()
		if err == nil {
			t.Error("expected validation error for invalid compression")
		}
	})
}

func TestValidateBackendReferences(t *testing.T) {
	t.Run("valid references", func(t *testing.T) {
		cfg := &ObjectStoreFullConfig{
			Defaults: ObjectStoreDefaults{
				PrimaryBackend:   "s3-primary",
				RequiredBackends: []string{"s3-primary", "gcs-backup"},
			},
		}
		definedBackends := []string{"s3-primary", "gcs-backup", "archive"}
		if err := ValidateBackendReferences(cfg, definedBackends); err != nil {
			t.Errorf("unexpected validation error: %v", err)
		}
	})

	t.Run("undefined primary", func(t *testing.T) {
		cfg := &ObjectStoreFullConfig{
			Defaults: ObjectStoreDefaults{
				PrimaryBackend: "nonexistent",
			},
		}
		definedBackends := []string{"s3-primary"}
		err := ValidateBackendReferences(cfg, definedBackends)
		if err == nil {
			t.Error("expected validation error for undefined primary")
		}
	})

	t.Run("undefined required", func(t *testing.T) {
		cfg := &ObjectStoreFullConfig{
			Defaults: ObjectStoreDefaults{
				RequiredBackends: []string{"nonexistent"},
			},
		}
		definedBackends := []string{"s3-primary"}
		err := ValidateBackendReferences(cfg, definedBackends)
		if err == nil {
			t.Error("expected validation error for undefined required backend")
		}
	})

	t.Run("empty backends", func(t *testing.T) {
		cfg := &ObjectStoreFullConfig{
			Defaults: ObjectStoreDefaults{
				PrimaryBackend: "s3-primary",
			},
		}
		err := ValidateBackendReferences(cfg, nil)
		if err == nil {
			t.Error("expected validation error when no backends defined")
		}
	})
}

func TestObjectStoreDefaults_String(t *testing.T) {
	d := &ObjectStoreDefaults{
		PrimaryBackend: "s3-primary",
		ValueThreshold: 1048576,
		Compression:    CompressionZstd,
	}

	str := d.String()

	if str == "" {
		t.Error("String() returned empty string")
	}
	if !contains(str, "s3-primary") {
		t.Error("String() should contain PrimaryBackend")
	}
	if !contains(str, "1048576") {
		t.Error("String() should contain ValueThreshold")
	}
	if !contains(str, "zstd") {
		t.Error("String() should contain Compression")
	}
}

// Helper function for string contains check
func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsHelper(s, substr))
}

func containsHelper(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

func TestConstants(t *testing.T) {
	// Verify constants are defined correctly
	if CompressionNone != "none" {
		t.Errorf("CompressionNone should be %q", "none")
	}
	if CompressionZstd != "zstd" {
		t.Errorf("CompressionZstd should be %q", "zstd")
	}
	if CompressionGzip != "gzip" {
		t.Errorf("CompressionGzip should be %q", "gzip")
	}
	if CompressionLZ4 != "lz4" {
		t.Errorf("CompressionLZ4 should be %q", "lz4")
	}

	if ReplicationModeSync != "sync" {
		t.Errorf("ReplicationModeSync should be %q", "sync")
	}
	if ReplicationModeAsync != "async" {
		t.Errorf("ReplicationModeAsync should be %q", "async")
	}
	if ReplicationModeQuorum != "quorum" {
		t.Errorf("ReplicationModeQuorum should be %q", "quorum")
	}

	if DefaultValueThreshold != 1048576 {
		t.Errorf("DefaultValueThreshold should be %d", 1048576)
	}
	if DefaultCacheMaxSize != 10737418240 {
		t.Errorf("DefaultCacheMaxSize should be %d", 10737418240)
	}
}

func TestObjectStoreFullConfig_CloneBackendsWithNilConfig(t *testing.T) {
	original := &ObjectStoreFullConfig{
		Backends: map[string]*StorageBackendConfig{
			"test": {
				Type:        "s3",
				Description: "test backend",
				// Config is nil
			},
		},
	}

	clone := original.Clone()

	if clone.Backends["test"].Config != nil {
		t.Error("cloned backend Config should be nil")
	}
}
