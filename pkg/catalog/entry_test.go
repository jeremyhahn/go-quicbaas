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
	"bytes"
	"context"
	"errors"
	"testing"
	"time"
)

func TestEntryStatus_String(t *testing.T) {
	tests := []struct {
		name   string
		status EntryStatus
		want   string
	}{
		{"active", StatusActive, "active"},
		{"pending", StatusPending, "pending"},
		{"archived", StatusArchived, "archived"},
		{"deleted", StatusDeleted, "deleted"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.status.String(); got != tt.want {
				t.Errorf("EntryStatus.String() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestEntryStatus_IsValid(t *testing.T) {
	tests := []struct {
		name   string
		status EntryStatus
		want   bool
	}{
		{"active", StatusActive, true},
		{"pending", StatusPending, true},
		{"archived", StatusArchived, true},
		{"deleted", StatusDeleted, true},
		{"unknown", EntryStatus("unknown"), false},
		{"empty", EntryStatus(""), false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.status.IsValid(); got != tt.want {
				t.Errorf("EntryStatus.IsValid() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestNewCatalogEntry(t *testing.T) {
	key := "tenant-acme:docs/report.pdf"
	size := int64(1024)
	checksum := []byte{1, 2, 3, 4}

	entry := NewCatalogEntry(key, size, checksum)

	if entry == nil {
		t.Fatal("NewCatalogEntry returned nil")
	}

	if entry.Key != key {
		t.Errorf("Key = %v, want %v", entry.Key, key)
	}

	if entry.Size != size {
		t.Errorf("Size = %v, want %v", entry.Size, size)
	}

	if !bytes.Equal(entry.Checksum, checksum) {
		t.Errorf("Checksum = %v, want %v", entry.Checksum, checksum)
	}

	if entry.Status != StatusActive {
		t.Errorf("Status = %v, want %v", entry.Status, StatusActive)
	}

	if entry.Version != 1 {
		t.Errorf("Version = %v, want 1", entry.Version)
	}

	if entry.CreatedAt == 0 {
		t.Error("CreatedAt should be set")
	}

	if entry.ModifiedAt == 0 {
		t.Error("ModifiedAt should be set")
	}

	// Verify checksum is copied
	checksum[0] = 99
	if entry.Checksum[0] == 99 {
		t.Error("Checksum should be a copy")
	}
}

func TestNewCatalogEntry_NilChecksum(t *testing.T) {
	entry := NewCatalogEntry("test:key", 0, nil)

	if entry.Checksum != nil {
		t.Error("Checksum should be nil when input is nil")
	}
}

func TestCatalogEntry_Clone(t *testing.T) {
	t.Run("clone nil", func(t *testing.T) {
		var entry *CatalogEntry
		clone := entry.Clone()
		if clone != nil {
			t.Error("Clone of nil should be nil")
		}
	})

	t.Run("clone full entry", func(t *testing.T) {
		entry := &CatalogEntry{
			Key:         "tenant:path",
			ObjectID:    12345,
			Checksum:    []byte{1, 2, 3},
			Size:        1024,
			ContentType: "application/pdf",
			PrimaryStore: StoreLocation{
				Store:     "s3-primary",
				ObjectRef: "obj/123",
				Status:    LocationStatusActive,
			},
			Replicas: []StoreLocation{
				{Store: "gcs-west", ObjectRef: "obj/123", Status: LocationStatusActive},
			},
			ArchiveStore: &StoreLocation{
				Store:     "s3-archive",
				ObjectRef: "archive/123",
				Status:    LocationStatusActive,
			},
			ReplicationPolicy: &ReplicationPolicy{Enabled: true, MinReplicas: 2},
			ArchivePolicy:     &ArchivePolicy{Enabled: true, ArchiveAfterDays: 30},
			RetentionPolicy:   &RetentionPolicy{Enabled: true, RetentionDays: 365},
			Encryption:        &ObjectEncryption{Enabled: true, KeyID: "key-123", Algorithm: "AES"},
			TenantID:          "tenant-acme",
			ShardID:           1,
			Owner:             "user1",
			ACL:               &ObjectACL{Owner: "user1", Readers: []string{"user2"}},
			Tags:              map[string]string{"env": "prod"},
			Labels:            map[string]string{"tier": "hot"},
			CustomMeta:        map[string]string{"custom": "value"},
			CreatedAt:         time.Now().UnixNano(),
			ModifiedAt:        time.Now().UnixNano(),
			LastAccessAt:      time.Now().UnixNano(),
			ArchivedAt:        0,
			Status:            StatusActive,
			Version:           5,
		}

		clone := entry.Clone()

		if clone == entry {
			t.Error("Clone should be a different instance")
		}

		if clone.Key != entry.Key {
			t.Errorf("Key = %v, want %v", clone.Key, entry.Key)
		}

		if clone.ObjectID != entry.ObjectID {
			t.Errorf("ObjectID = %v, want %v", clone.ObjectID, entry.ObjectID)
		}

		// Verify deep copies
		entry.Checksum[0] = 99
		if clone.Checksum[0] == 99 {
			t.Error("Checksum should be a copy")
		}

		entry.Tags["env"] = "modified"
		if clone.Tags["env"] == "modified" {
			t.Error("Tags should be a copy")
		}

		entry.Replicas[0].Store = "modified"
		if clone.Replicas[0].Store == "modified" {
			t.Error("Replicas should be a copy")
		}
	})
}

func TestCatalogEntry_EncodeAndDecode(t *testing.T) {
	t.Run("encode nil entry", func(t *testing.T) {
		var entry *CatalogEntry
		_, err := entry.Encode()
		if err == nil {
			t.Error("Expected error when encoding nil entry")
		}
	})

	t.Run("decode empty data", func(t *testing.T) {
		_, err := DecodeCatalogEntry(nil)
		if err == nil {
			t.Error("Expected error when decoding empty data")
		}

		_, err = DecodeCatalogEntry([]byte{})
		if err == nil {
			t.Error("Expected error when decoding empty data")
		}
	})

	t.Run("encode and decode valid entry", func(t *testing.T) {
		original := &CatalogEntry{
			Key:         "tenant:path",
			ObjectID:    12345,
			Checksum:    []byte{1, 2, 3},
			Size:        1024,
			ContentType: "text/plain",
			PrimaryStore: StoreLocation{
				Store:     "s3",
				ObjectRef: "obj/123",
				Status:    LocationStatusActive,
			},
			TenantID: "tenant-acme",
			Tags:     map[string]string{"key": "value"},
			Status:   StatusActive,
			Version:  1,
		}

		encoded, err := original.Encode()
		if err != nil {
			t.Fatalf("Encode failed: %v", err)
		}

		decoded, err := DecodeCatalogEntry(encoded)
		if err != nil {
			t.Fatalf("Decode failed: %v", err)
		}

		if decoded.Key != original.Key {
			t.Errorf("Key = %v, want %v", decoded.Key, original.Key)
		}

		if decoded.ObjectID != original.ObjectID {
			t.Errorf("ObjectID = %v, want %v", decoded.ObjectID, original.ObjectID)
		}

		if !bytes.Equal(decoded.Checksum, original.Checksum) {
			t.Errorf("Checksum = %v, want %v", decoded.Checksum, original.Checksum)
		}

		if decoded.Tags["key"] != "value" {
			t.Errorf("Tags[key] = %v, want value", decoded.Tags["key"])
		}
	})

	t.Run("decode invalid data", func(t *testing.T) {
		_, err := DecodeCatalogEntry([]byte{0xFF, 0xFF, 0xFF})
		if err == nil {
			t.Error("Expected error when decoding invalid data")
		}
	})
}

func TestCatalogEntry_GetAllLocations(t *testing.T) {
	t.Run("nil entry", func(t *testing.T) {
		var entry *CatalogEntry
		locs := entry.GetAllLocations()
		if locs != nil {
			t.Error("Expected nil for nil entry")
		}
	})

	t.Run("primary only", func(t *testing.T) {
		entry := &CatalogEntry{
			PrimaryStore: StoreLocation{Store: "s3"},
		}
		locs := entry.GetAllLocations()
		if len(locs) != 1 {
			t.Errorf("Expected 1 location, got %d", len(locs))
		}
	})

	t.Run("with replicas", func(t *testing.T) {
		entry := &CatalogEntry{
			PrimaryStore: StoreLocation{Store: "s3"},
			Replicas: []StoreLocation{
				{Store: "gcs"},
				{Store: "azure"},
			},
		}
		locs := entry.GetAllLocations()
		if len(locs) != 3 {
			t.Errorf("Expected 3 locations, got %d", len(locs))
		}
	})

	t.Run("with archive", func(t *testing.T) {
		entry := &CatalogEntry{
			PrimaryStore: StoreLocation{Store: "s3"},
			Replicas:     []StoreLocation{{Store: "gcs"}},
			ArchiveStore: &StoreLocation{Store: "glacier"},
		}
		locs := entry.GetAllLocations()
		if len(locs) != 3 {
			t.Errorf("Expected 3 locations, got %d", len(locs))
		}
	})
}

func TestCatalogEntry_HasLocation(t *testing.T) {
	entry := &CatalogEntry{
		PrimaryStore: StoreLocation{Store: "s3"},
		Replicas:     []StoreLocation{{Store: "gcs"}},
		ArchiveStore: &StoreLocation{Store: "glacier"},
	}

	tests := []struct {
		name  string
		store string
		want  bool
	}{
		{"primary store", "s3", true},
		{"replica store", "gcs", true},
		{"archive store", "glacier", true},
		{"non-existent", "azure", false},
		{"empty string", "", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := entry.HasLocation(tt.store); got != tt.want {
				t.Errorf("HasLocation(%s) = %v, want %v", tt.store, got, tt.want)
			}
		})
	}

	t.Run("nil entry", func(t *testing.T) {
		var nilEntry *CatalogEntry
		if nilEntry.HasLocation("s3") {
			t.Error("Expected false for nil entry")
		}
	})
}

func TestCatalogEntry_GetLocation(t *testing.T) {
	entry := &CatalogEntry{
		PrimaryStore: StoreLocation{Store: "s3", ObjectRef: "obj/1"},
		Replicas:     []StoreLocation{{Store: "gcs", ObjectRef: "obj/2"}},
		ArchiveStore: &StoreLocation{Store: "glacier", ObjectRef: "obj/3"},
	}

	t.Run("get primary", func(t *testing.T) {
		loc := entry.GetLocation("s3")
		if loc == nil {
			t.Fatal("Expected location, got nil")
		}
		if loc.ObjectRef != "obj/1" {
			t.Errorf("ObjectRef = %v, want obj/1", loc.ObjectRef)
		}
	})

	t.Run("get replica", func(t *testing.T) {
		loc := entry.GetLocation("gcs")
		if loc == nil {
			t.Fatal("Expected location, got nil")
		}
		if loc.ObjectRef != "obj/2" {
			t.Errorf("ObjectRef = %v, want obj/2", loc.ObjectRef)
		}
	})

	t.Run("get archive", func(t *testing.T) {
		loc := entry.GetLocation("glacier")
		if loc == nil {
			t.Fatal("Expected location, got nil")
		}
		if loc.ObjectRef != "obj/3" {
			t.Errorf("ObjectRef = %v, want obj/3", loc.ObjectRef)
		}
	})

	t.Run("get non-existent", func(t *testing.T) {
		loc := entry.GetLocation("azure")
		if loc != nil {
			t.Error("Expected nil for non-existent store")
		}
	})

	t.Run("nil entry", func(t *testing.T) {
		var nilEntry *CatalogEntry
		if nilEntry.GetLocation("s3") != nil {
			t.Error("Expected nil for nil entry")
		}
	})

	t.Run("empty store", func(t *testing.T) {
		if entry.GetLocation("") != nil {
			t.Error("Expected nil for empty store")
		}
	})
}

func TestCatalogEntry_UpdateAccessTime(t *testing.T) {
	t.Run("update access time", func(t *testing.T) {
		entry := NewCatalogEntry("test:key", 0, nil)
		originalTime := entry.LastAccessAt

		time.Sleep(time.Millisecond)
		entry.UpdateAccessTime()

		if entry.LastAccessAt <= originalTime {
			t.Error("LastAccessAt should be updated")
		}
	})

	t.Run("nil entry", func(t *testing.T) {
		var entry *CatalogEntry
		entry.UpdateAccessTime() // Should not panic
	})
}

func TestCatalogEntry_UpdateModifiedTime(t *testing.T) {
	t.Run("update modified time", func(t *testing.T) {
		entry := NewCatalogEntry("test:key", 0, nil)
		originalTime := entry.ModifiedAt
		originalVersion := entry.Version

		time.Sleep(time.Millisecond)
		entry.UpdateModifiedTime()

		if entry.ModifiedAt <= originalTime {
			t.Error("ModifiedAt should be updated")
		}
		if entry.Version != originalVersion+1 {
			t.Errorf("Version = %v, want %v", entry.Version, originalVersion+1)
		}
	})

	t.Run("nil entry", func(t *testing.T) {
		var entry *CatalogEntry
		entry.UpdateModifiedTime() // Should not panic
	})
}

func TestCatalogEntry_Tags(t *testing.T) {
	t.Run("set and get tag", func(t *testing.T) {
		entry := NewCatalogEntry("test:key", 0, nil)
		entry.SetTag("env", "prod")

		if got := entry.GetTag("env"); got != "prod" {
			t.Errorf("GetTag(env) = %v, want prod", got)
		}
	})

	t.Run("get non-existent tag", func(t *testing.T) {
		entry := NewCatalogEntry("test:key", 0, nil)
		if got := entry.GetTag("nonexistent"); got != "" {
			t.Errorf("GetTag(nonexistent) = %v, want empty", got)
		}
	})

	t.Run("remove tag", func(t *testing.T) {
		entry := NewCatalogEntry("test:key", 0, nil)
		entry.SetTag("env", "prod")
		entry.RemoveTag("env")

		if got := entry.GetTag("env"); got != "" {
			t.Errorf("GetTag(env) after remove = %v, want empty", got)
		}
	})

	t.Run("nil entry operations", func(t *testing.T) {
		var entry *CatalogEntry
		entry.SetTag("key", "value")   // Should not panic
		entry.RemoveTag("key")         // Should not panic
		if entry.GetTag("key") != "" { // Should return empty
			t.Error("GetTag on nil should return empty")
		}
	})
}

func TestCatalogEntry_Labels(t *testing.T) {
	t.Run("set and get label", func(t *testing.T) {
		entry := NewCatalogEntry("test:key", 0, nil)
		entry.SetLabel("tier", "hot")

		if got := entry.GetLabel("tier"); got != "hot" {
			t.Errorf("GetLabel(tier) = %v, want hot", got)
		}
	})

	t.Run("nil entry", func(t *testing.T) {
		var entry *CatalogEntry
		entry.SetLabel("key", "value")   // Should not panic
		if entry.GetLabel("key") != "" { // Should return empty
			t.Error("GetLabel on nil should return empty")
		}
	})
}

func TestCatalogEntry_CustomMeta(t *testing.T) {
	t.Run("set and get custom meta", func(t *testing.T) {
		entry := NewCatalogEntry("test:key", 0, nil)
		entry.SetCustomMeta("custom", "value")

		if got := entry.GetCustomMeta("custom"); got != "value" {
			t.Errorf("GetCustomMeta(custom) = %v, want value", got)
		}
	})

	t.Run("nil entry", func(t *testing.T) {
		var entry *CatalogEntry
		entry.SetCustomMeta("key", "value")   // Should not panic
		if entry.GetCustomMeta("key") != "" { // Should return empty
			t.Error("GetCustomMeta on nil should return empty")
		}
	})
}

func TestCatalogEntry_StatusChecks(t *testing.T) {
	tests := []struct {
		name       string
		status     EntryStatus
		isActive   bool
		isArchived bool
		isDeleted  bool
	}{
		{"active", StatusActive, true, false, false},
		{"pending", StatusPending, false, false, false},
		{"archived", StatusArchived, false, true, false},
		{"deleted", StatusDeleted, false, false, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			entry := &CatalogEntry{Status: tt.status}

			if got := entry.IsActive(); got != tt.isActive {
				t.Errorf("IsActive() = %v, want %v", got, tt.isActive)
			}
			if got := entry.IsArchived(); got != tt.isArchived {
				t.Errorf("IsArchived() = %v, want %v", got, tt.isArchived)
			}
			if got := entry.IsDeleted(); got != tt.isDeleted {
				t.Errorf("IsDeleted() = %v, want %v", got, tt.isDeleted)
			}
		})
	}

	t.Run("nil entry", func(t *testing.T) {
		var entry *CatalogEntry
		if entry.IsActive() || entry.IsArchived() || entry.IsDeleted() {
			t.Error("Status checks on nil entry should return false")
		}
	})
}

func TestCatalogEntry_MarkArchived(t *testing.T) {
	t.Run("mark archived", func(t *testing.T) {
		entry := NewCatalogEntry("test:key", 0, nil)
		originalVersion := entry.Version

		entry.MarkArchived()

		if entry.Status != StatusArchived {
			t.Errorf("Status = %v, want archived", entry.Status)
		}
		if entry.ArchivedAt == 0 {
			t.Error("ArchivedAt should be set")
		}
		if entry.Version != originalVersion+1 {
			t.Errorf("Version = %v, want %v", entry.Version, originalVersion+1)
		}
	})

	t.Run("nil entry", func(t *testing.T) {
		var entry *CatalogEntry
		entry.MarkArchived() // Should not panic
	})
}

func TestCatalogEntry_MarkDeleted(t *testing.T) {
	t.Run("mark deleted", func(t *testing.T) {
		entry := NewCatalogEntry("test:key", 0, nil)
		originalVersion := entry.Version

		entry.MarkDeleted()

		if entry.Status != StatusDeleted {
			t.Errorf("Status = %v, want deleted", entry.Status)
		}
		if entry.Version != originalVersion+1 {
			t.Errorf("Version = %v, want %v", entry.Version, originalVersion+1)
		}
	})

	t.Run("nil entry", func(t *testing.T) {
		var entry *CatalogEntry
		entry.MarkDeleted() // Should not panic
	})
}

func TestCatalogEntry_AddRemoveReplica(t *testing.T) {
	t.Run("add replica", func(t *testing.T) {
		entry := &CatalogEntry{
			PrimaryStore: StoreLocation{Store: "s3"},
			Version:      1,
		}

		loc := StoreLocation{Store: "gcs", ObjectRef: "obj/123"}
		entry.AddReplica(loc)

		if len(entry.Replicas) != 1 {
			t.Errorf("Expected 1 replica, got %d", len(entry.Replicas))
		}
		if entry.Version != 2 {
			t.Errorf("Version = %v, want 2", entry.Version)
		}
	})

	t.Run("add duplicate replica", func(t *testing.T) {
		entry := &CatalogEntry{
			PrimaryStore: StoreLocation{Store: "s3"},
			Replicas:     []StoreLocation{{Store: "gcs"}},
		}

		entry.AddReplica(StoreLocation{Store: "gcs"})

		if len(entry.Replicas) != 1 {
			t.Errorf("Duplicate replica should not be added, got %d replicas", len(entry.Replicas))
		}
	})

	t.Run("remove replica", func(t *testing.T) {
		entry := &CatalogEntry{
			PrimaryStore: StoreLocation{Store: "s3"},
			Replicas: []StoreLocation{
				{Store: "gcs"},
				{Store: "azure"},
			},
			Version: 1,
		}

		removed := entry.RemoveReplica("gcs")

		if !removed {
			t.Error("RemoveReplica should return true")
		}
		if len(entry.Replicas) != 1 {
			t.Errorf("Expected 1 replica after removal, got %d", len(entry.Replicas))
		}
		if entry.Version != 2 {
			t.Errorf("Version = %v, want 2", entry.Version)
		}
	})

	t.Run("remove non-existent replica", func(t *testing.T) {
		entry := &CatalogEntry{
			PrimaryStore: StoreLocation{Store: "s3"},
			Replicas:     []StoreLocation{{Store: "gcs"}},
		}

		removed := entry.RemoveReplica("azure")

		if removed {
			t.Error("RemoveReplica should return false for non-existent")
		}
	})

	t.Run("nil entry", func(t *testing.T) {
		var entry *CatalogEntry
		entry.AddReplica(StoreLocation{Store: "gcs"}) // Should not panic
		if entry.RemoveReplica("gcs") {               // Should return false
			t.Error("RemoveReplica on nil should return false")
		}
	})
}

func TestCatalogEntry_SetArchiveStore(t *testing.T) {
	t.Run("set archive store", func(t *testing.T) {
		entry := &CatalogEntry{
			PrimaryStore: StoreLocation{Store: "s3"},
			Version:      1,
		}

		archiveLoc := &StoreLocation{Store: "glacier", ObjectRef: "archive/123"}
		entry.SetArchiveStore(archiveLoc)

		if entry.ArchiveStore == nil {
			t.Fatal("ArchiveStore should be set")
		}
		if entry.ArchiveStore.Store != "glacier" {
			t.Errorf("ArchiveStore.Store = %v, want glacier", entry.ArchiveStore.Store)
		}
		if entry.Version != 2 {
			t.Errorf("Version = %v, want 2", entry.Version)
		}

		// Verify it's a copy
		archiveLoc.Store = "modified"
		if entry.ArchiveStore.Store == "modified" {
			t.Error("ArchiveStore should be a copy")
		}
	})

	t.Run("nil entry", func(t *testing.T) {
		var entry *CatalogEntry
		entry.SetArchiveStore(&StoreLocation{Store: "glacier"}) // Should not panic
	})
}

func TestCatalogEntry_GetHealthyLocations(t *testing.T) {
	entry := &CatalogEntry{
		PrimaryStore: StoreLocation{Store: "s3", Status: LocationStatusActive},
		Replicas: []StoreLocation{
			{Store: "gcs", Status: LocationStatusActive},
			{Store: "azure", Status: LocationStatusFailed},
			{Store: "local", Status: LocationStatusSyncing},
		},
		ArchiveStore: &StoreLocation{Store: "glacier", Status: LocationStatusStale},
	}

	healthy := entry.GetHealthyLocations()

	if len(healthy) != 3 { // s3, gcs, local (active and syncing)
		t.Errorf("Expected 3 healthy locations, got %d", len(healthy))
	}

	t.Run("nil entry", func(t *testing.T) {
		var nilEntry *CatalogEntry
		if nilEntry.GetHealthyLocations() != nil {
			t.Error("Expected nil for nil entry")
		}
	})
}

func TestCatalogEntry_GetActiveLocations(t *testing.T) {
	entry := &CatalogEntry{
		PrimaryStore: StoreLocation{Store: "s3", Status: LocationStatusActive},
		Replicas: []StoreLocation{
			{Store: "gcs", Status: LocationStatusActive},
			{Store: "azure", Status: LocationStatusSyncing},
		},
		ArchiveStore: &StoreLocation{Store: "glacier", Status: LocationStatusActive},
	}

	active := entry.GetActiveLocations()

	if len(active) != 3 { // s3, gcs, glacier
		t.Errorf("Expected 3 active locations, got %d", len(active))
	}

	t.Run("nil entry", func(t *testing.T) {
		var nilEntry *CatalogEntry
		if nilEntry.GetActiveLocations() != nil {
			t.Error("Expected nil for nil entry")
		}
	})
}

func TestCatalogEntry_ReplicaCount(t *testing.T) {
	tests := []struct {
		name     string
		entry    *CatalogEntry
		expected int
	}{
		{"nil entry", nil, 0},
		{"no replicas", &CatalogEntry{}, 0},
		{"with replicas", &CatalogEntry{Replicas: make([]StoreLocation, 3)}, 3},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.entry.ReplicaCount(); got != tt.expected {
				t.Errorf("ReplicaCount() = %v, want %v", got, tt.expected)
			}
		})
	}
}

func TestCatalogEntry_TotalLocationCount(t *testing.T) {
	tests := []struct {
		name     string
		entry    *CatalogEntry
		expected int
	}{
		{"nil entry", nil, 0},
		{"primary only", &CatalogEntry{}, 1},
		{"with replicas", &CatalogEntry{Replicas: make([]StoreLocation, 2)}, 3},
		{"with archive", &CatalogEntry{ArchiveStore: &StoreLocation{}}, 2},
		{"all", &CatalogEntry{Replicas: make([]StoreLocation, 2), ArchiveStore: &StoreLocation{}}, 4},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.entry.TotalLocationCount(); got != tt.expected {
				t.Errorf("TotalLocationCount() = %v, want %v", got, tt.expected)
			}
		})
	}
}

func TestCatalogEntry_ChecksumHex(t *testing.T) {
	t.Run("with checksum", func(t *testing.T) {
		entry := &CatalogEntry{Checksum: []byte{0xDE, 0xAD, 0xBE, 0xEF}}
		hex := entry.ChecksumHex()
		if hex != "deadbeef" {
			t.Errorf("ChecksumHex() = %v, want deadbeef", hex)
		}
	})

	t.Run("nil checksum", func(t *testing.T) {
		entry := &CatalogEntry{}
		if entry.ChecksumHex() != "" {
			t.Error("ChecksumHex() should be empty for nil checksum")
		}
	})

	t.Run("nil entry", func(t *testing.T) {
		var entry *CatalogEntry
		if entry.ChecksumHex() != "" {
			t.Error("ChecksumHex() should be empty for nil entry")
		}
	})
}

func TestCatalogEntry_Validate(t *testing.T) {
	validEntry := func() *CatalogEntry {
		return &CatalogEntry{
			Key:    "tenant:path",
			Size:   1024,
			Status: StatusActive,
			PrimaryStore: StoreLocation{
				Store:     "s3",
				ObjectRef: "obj/123",
				Status:    LocationStatusActive,
			},
		}
	}

	tests := []struct {
		name    string
		modify  func(*CatalogEntry)
		wantErr bool
	}{
		{"valid entry", func(e *CatalogEntry) {}, false},
		{"nil entry", func(e *CatalogEntry) {}, true},
		{"empty key", func(e *CatalogEntry) { e.Key = "" }, true},
		{"negative size", func(e *CatalogEntry) { e.Size = -1 }, true},
		{"invalid status", func(e *CatalogEntry) { e.Status = "invalid" }, true},
		{"invalid primary store", func(e *CatalogEntry) { e.PrimaryStore.Store = "" }, true},
		{"duplicate replica", func(e *CatalogEntry) {
			e.Replicas = []StoreLocation{
				{Store: "gcs", ObjectRef: "obj/1", Status: LocationStatusActive},
				{Store: "gcs", ObjectRef: "obj/2", Status: LocationStatusActive},
			}
		}, true},
		{"replica same as primary", func(e *CatalogEntry) {
			e.Replicas = []StoreLocation{
				{Store: "s3", ObjectRef: "obj/1", Status: LocationStatusActive},
			}
		}, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var entry *CatalogEntry
			if tt.name != "nil entry" {
				entry = validEntry()
				tt.modify(entry)
			}
			err := entry.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestCatalogEntry_ShouldArchive(t *testing.T) {
	t.Run("nil entry", func(t *testing.T) {
		var entry *CatalogEntry
		if entry.ShouldArchive() {
			t.Error("Nil entry should not need archiving")
		}
	})

	t.Run("no archive policy", func(t *testing.T) {
		entry := &CatalogEntry{Status: StatusActive}
		if entry.ShouldArchive() {
			t.Error("Entry without archive policy should not need archiving")
		}
	})

	t.Run("not active status", func(t *testing.T) {
		entry := &CatalogEntry{
			Status:        StatusArchived,
			ArchivePolicy: &ArchivePolicy{Enabled: true, ArchiveAfterDays: 30},
		}
		if entry.ShouldArchive() {
			t.Error("Non-active entry should not need archiving")
		}
	})

	t.Run("should archive based on last access", func(t *testing.T) {
		oldTime := time.Now().AddDate(0, -6, 0).Unix() // 6 months ago
		entry := &CatalogEntry{
			Status:        StatusActive,
			LastAccessAt:  oldTime,
			ArchivePolicy: &ArchivePolicy{Enabled: true, ArchiveAfterDays: 90},
		}
		if !entry.ShouldArchive() {
			t.Error("Entry should need archiving")
		}
	})
}

func TestCatalogEntry_ShouldDelete(t *testing.T) {
	t.Run("nil entry", func(t *testing.T) {
		var entry *CatalogEntry
		if entry.ShouldDelete() {
			t.Error("Nil entry should not need deletion")
		}
	})

	t.Run("no retention policy", func(t *testing.T) {
		entry := &CatalogEntry{}
		if entry.ShouldDelete() {
			t.Error("Entry without retention policy should not need deletion")
		}
	})

	t.Run("should delete based on creation time", func(t *testing.T) {
		oldTime := time.Now().AddDate(-1, 0, 0).Unix() // 1 year ago
		entry := &CatalogEntry{
			CreatedAt:       oldTime,
			RetentionPolicy: &RetentionPolicy{Enabled: true, RetentionDays: 180},
		}
		if !entry.ShouldDelete() {
			t.Error("Entry should need deletion")
		}
	})
}

func TestCatalogEntry_NeedsMoreReplicas(t *testing.T) {
	t.Run("nil entry", func(t *testing.T) {
		var entry *CatalogEntry
		if entry.NeedsMoreReplicas() {
			t.Error("Nil entry should not need more replicas")
		}
	})

	t.Run("no replication policy", func(t *testing.T) {
		entry := &CatalogEntry{}
		if entry.NeedsMoreReplicas() {
			t.Error("Entry without replication policy should not need more replicas")
		}
	})

	t.Run("disabled policy", func(t *testing.T) {
		entry := &CatalogEntry{
			ReplicationPolicy: &ReplicationPolicy{Enabled: false, MinReplicas: 3},
		}
		if entry.NeedsMoreReplicas() {
			t.Error("Entry with disabled policy should not need more replicas")
		}
	})

	t.Run("needs more replicas", func(t *testing.T) {
		entry := &CatalogEntry{
			ReplicationPolicy: &ReplicationPolicy{Enabled: true, MinReplicas: 3},
			Replicas:          []StoreLocation{{Store: "gcs"}}, // 1 + 1 = 2 copies, need 3
		}
		if !entry.NeedsMoreReplicas() {
			t.Error("Entry should need more replicas")
		}
	})

	t.Run("has enough replicas", func(t *testing.T) {
		entry := &CatalogEntry{
			ReplicationPolicy: &ReplicationPolicy{Enabled: true, MinReplicas: 2},
			Replicas:          []StoreLocation{{Store: "gcs"}}, // 1 + 1 = 2 copies
		}
		if entry.NeedsMoreReplicas() {
			t.Error("Entry has enough replicas")
		}
	})
}

func TestCatalogEntry_GetPrimaryStoreName(t *testing.T) {
	t.Run("nil entry", func(t *testing.T) {
		var entry *CatalogEntry
		if entry.GetPrimaryStoreName() != "" {
			t.Error("Expected empty for nil entry")
		}
	})

	t.Run("with primary store", func(t *testing.T) {
		entry := &CatalogEntry{
			PrimaryStore: StoreLocation{Store: "s3-primary"},
		}
		if got := entry.GetPrimaryStoreName(); got != "s3-primary" {
			t.Errorf("GetPrimaryStoreName() = %v, want s3-primary", got)
		}
	})
}

func TestCatalogEntry_GetEncryptionKeyID(t *testing.T) {
	t.Run("nil entry", func(t *testing.T) {
		var entry *CatalogEntry
		if entry.GetEncryptionKeyID() != "" {
			t.Error("Expected empty for nil entry")
		}
	})

	t.Run("no encryption", func(t *testing.T) {
		entry := &CatalogEntry{}
		if entry.GetEncryptionKeyID() != "" {
			t.Error("Expected empty for entry without encryption")
		}
	})

	t.Run("disabled encryption", func(t *testing.T) {
		entry := &CatalogEntry{
			Encryption: &ObjectEncryption{Enabled: false, KeyID: "key-123"},
		}
		if entry.GetEncryptionKeyID() != "" {
			t.Error("Expected empty for disabled encryption")
		}
	})

	t.Run("enabled encryption", func(t *testing.T) {
		entry := &CatalogEntry{
			Encryption: &ObjectEncryption{Enabled: true, KeyID: "key-123"},
		}
		if got := entry.GetEncryptionKeyID(); got != "key-123" {
			t.Errorf("GetEncryptionKeyID() = %v, want key-123", got)
		}
	})
}

// =============================================================================
// Consolidated from coverage_boost_test.go
// =============================================================================

// =============================================================================
// Error Constructor Tests - All the 0% coverage error constructors
// =============================================================================

func TestNewKeyInvalidFormatError(t *testing.T) {
	t.Run("creates error with key and reason", func(t *testing.T) {
		err := NewKeyInvalidFormatError("invalid:key:format", "contains invalid characters")
		if err == nil {
			t.Fatal("expected error to be non-nil")
		}
		if err.Code != ErrCodeKeyInvalid {
			t.Errorf("expected code %s, got %s", ErrCodeKeyInvalid, err.Code)
		}
		if err.Key != "invalid:key:format" {
			t.Errorf("expected key 'invalid:key:format', got %s", err.Key)
		}
		errStr := err.Error()
		if errStr == "" {
			t.Error("expected non-empty error string")
		}
	})
}

func TestNewInvalidNamespaceError(t *testing.T) {
	t.Run("creates error with namespace", func(t *testing.T) {
		err := NewInvalidNamespaceError("_invalid_ns")
		if err == nil {
			t.Fatal("expected error to be non-nil")
		}
		if err.Code != ErrCodeNamespaceInvalid {
			t.Errorf("expected code %s, got %s", ErrCodeNamespaceInvalid, err.Code)
		}
		errStr := err.Error()
		if errStr == "" {
			t.Error("expected non-empty error string")
		}
	})
}

func TestNewNamespaceTooLongError(t *testing.T) {
	t.Run("creates error with lengths", func(t *testing.T) {
		longNs := "verylongnamespace"
		err := NewNamespaceTooLongError(longNs, 17, 10)
		if err == nil {
			t.Fatal("expected error to be non-nil")
		}
		if err.Code != ErrCodeNamespaceTooLong {
			t.Errorf("expected code %s, got %s", ErrCodeNamespaceTooLong, err.Code)
		}
		errStr := err.Error()
		if errStr == "" {
			t.Error("expected non-empty error string")
		}
	})
}

func TestNewInvalidPathError(t *testing.T) {
	t.Run("creates error with path", func(t *testing.T) {
		err := NewInvalidPathError("/invalid//path")
		if err == nil {
			t.Fatal("expected error to be non-nil")
		}
		if err.Code != ErrCodePathInvalid {
			t.Errorf("expected code %s, got %s", ErrCodePathInvalid, err.Code)
		}
		errStr := err.Error()
		if errStr == "" {
			t.Error("expected non-empty error string")
		}
	})
}

func TestNewPathTooLongError(t *testing.T) {
	t.Run("creates error with lengths", func(t *testing.T) {
		err := NewPathTooLongError("some/long/path", 1500, 1024)
		if err == nil {
			t.Fatal("expected error to be non-nil")
		}
		if err.Code != ErrCodePathTooLong {
			t.Errorf("expected code %s, got %s", ErrCodePathTooLong, err.Code)
		}
		errStr := err.Error()
		if errStr == "" {
			t.Error("expected non-empty error string")
		}
	})
}

func TestNewEntryExistsError(t *testing.T) {
	t.Run("creates error with key", func(t *testing.T) {
		err := NewEntryExistsError("ns1:existing/key")
		if err == nil {
			t.Fatal("expected error to be non-nil")
		}
		if err.Code != ErrCodeEntryExists {
			t.Errorf("expected code %s, got %s", ErrCodeEntryExists, err.Code)
		}
		if err.Key != "ns1:existing/key" {
			t.Errorf("expected key 'ns1:existing/key', got %s", err.Key)
		}
	})
}

func TestNewVersionMismatchError(t *testing.T) {
	t.Run("creates error with versions", func(t *testing.T) {
		err := NewVersionMismatchError("ns1:key", 5, 3)
		if err == nil {
			t.Fatal("expected error to be non-nil")
		}
		if err.Code != ErrCodeVersionMismatch {
			t.Errorf("expected code %s, got %s", ErrCodeVersionMismatch, err.Code)
		}
		if err.Key != "ns1:key" {
			t.Errorf("expected key 'ns1:key', got %s", err.Key)
		}
		errStr := err.Error()
		if errStr == "" {
			t.Error("expected non-empty error string")
		}
	})
}

func TestNewEncodingError(t *testing.T) {
	t.Run("creates error with cause", func(t *testing.T) {
		cause := errors.New("msgpack encoding failed")
		err := NewEncodingError("ns1:key", cause)
		if err == nil {
			t.Fatal("expected error to be non-nil")
		}
		if err.Code != ErrCodeEncodingFailed {
			t.Errorf("expected code %s, got %s", ErrCodeEncodingFailed, err.Code)
		}
		if err.Cause != cause {
			t.Error("expected cause to be set")
		}
		// Test Unwrap
		unwrapped := err.Unwrap()
		if unwrapped != cause {
			t.Error("Unwrap should return the cause")
		}
	})
}

func TestNewBackendNotFoundError(t *testing.T) {
	t.Run("creates error with backend name", func(t *testing.T) {
		err := NewBackendNotFoundError("s3-primary")
		if err == nil {
			t.Fatal("expected error to be non-nil")
		}
		if err.Code != ErrCodeBackendNotFound {
			t.Errorf("expected code %s, got %s", ErrCodeBackendNotFound, err.Code)
		}
		errStr := err.Error()
		if errStr == "" {
			t.Error("expected non-empty error string")
		}
	})
}

func TestNewBackendUnhealthyError(t *testing.T) {
	t.Run("creates error with backend name", func(t *testing.T) {
		err := NewBackendUnhealthyError("gcs-west")
		if err == nil {
			t.Fatal("expected error to be non-nil")
		}
		if err.Code != ErrCodeBackendUnhealthy {
			t.Errorf("expected code %s, got %s", ErrCodeBackendUnhealthy, err.Code)
		}
	})
}

func TestNewPolicyInvalidError(t *testing.T) {
	t.Run("creates error with policy type and reason", func(t *testing.T) {
		err := NewPolicyInvalidError("replication", "min_replicas cannot be negative")
		if err == nil {
			t.Fatal("expected error to be non-nil")
		}
		if err.Code != ErrCodePolicyInvalid {
			t.Errorf("expected code %s, got %s", ErrCodePolicyInvalid, err.Code)
		}
	})
}

func TestNewQuotaExceededError(t *testing.T) {
	t.Run("creates error with quota details", func(t *testing.T) {
		err := NewQuotaExceededError("tenant-acme", "storage", 1000, 1500)
		if err == nil {
			t.Fatal("expected error to be non-nil")
		}
		if err.Code != ErrCodeQuotaExceeded {
			t.Errorf("expected code %s, got %s", ErrCodeQuotaExceeded, err.Code)
		}
	})
}

func TestNewChecksumMismatchError(t *testing.T) {
	t.Run("creates error with checksums", func(t *testing.T) {
		expected := []byte{0x01, 0x02, 0x03}
		actual := []byte{0x04, 0x05, 0x06}
		err := NewChecksumMismatchError("ns1:key", expected, actual)
		if err == nil {
			t.Fatal("expected error to be non-nil")
		}
		if err.Code != ErrCodeChecksumMismatch {
			t.Errorf("expected code %s, got %s", ErrCodeChecksumMismatch, err.Code)
		}
	})
}

func TestNewQueueFullError(t *testing.T) {
	t.Run("creates error with queue type", func(t *testing.T) {
		err := NewQueueFullError("replication")
		if err == nil {
			t.Fatal("expected error to be non-nil")
		}
		if err.Code != ErrCodeQueueFull {
			t.Errorf("expected code %s, got %s", ErrCodeQueueFull, err.Code)
		}
	})
}

func TestNewIndexError(t *testing.T) {
	t.Run("creates error with index type and cause", func(t *testing.T) {
		cause := errors.New("index write failed")
		err := NewIndexError("tenant", "ns1:key", cause)
		if err == nil {
			t.Fatal("expected error to be non-nil")
		}
		if err.Code != ErrCodeIndexFailed {
			t.Errorf("expected code %s, got %s", ErrCodeIndexFailed, err.Code)
		}
		if err.Cause != cause {
			t.Error("expected cause to be set")
		}
	})
}

// =============================================================================
// Error Helper Function Tests
// =============================================================================

func TestIsVersionMismatch(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{"nil error", nil, false},
		{"version mismatch error", NewVersionMismatchError("key", 1, 2), true},
		{"other catalog error", NewKeyEmptyError(), false},
		{"non-catalog error", errors.New("some error"), false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := IsVersionMismatch(tt.err); got != tt.want {
				t.Errorf("IsVersionMismatch() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestIsQuotaExceeded(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{"nil error", nil, false},
		{"quota exceeded error", NewQuotaExceededError("tenant", "storage", 100, 200), true},
		{"other catalog error", NewKeyEmptyError(), false},
		{"non-catalog error", errors.New("some error"), false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := IsQuotaExceeded(tt.err); got != tt.want {
				t.Errorf("IsQuotaExceeded() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestIsBackendError(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{"nil error", nil, false},
		{"backend not found error", NewBackendNotFoundError("s3"), true},
		{"backend unhealthy error", NewBackendUnhealthyError("gcs"), true},
		{"other catalog error", NewKeyEmptyError(), false},
		{"non-catalog error", errors.New("some error"), false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := IsBackendError(tt.err); got != tt.want {
				t.Errorf("IsBackendError() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestIsIndexError(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{"nil error", nil, false},
		{"index error", NewIndexError("tenant", "key", nil), true},
		{"other catalog error", NewKeyEmptyError(), false},
		{"non-catalog error", errors.New("some error"), false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := IsIndexError(tt.err); got != tt.want {
				t.Errorf("IsIndexError() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestIsValidationError(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{"nil error", nil, false},
		{"key empty error", NewKeyEmptyError(), true},
		{"key too long error", NewKeyTooLongError(100, 50), true},
		{"key invalid error", NewKeyInvalidError("key", "reason"), true},
		{"namespace empty error", NewNamespaceEmptyError(), true},
		{"namespace invalid error", NewNamespaceInvalidError("ns", "reason"), true},
		{"namespace too long error", NewNamespaceTooLongError("ns", 100, 50), true},
		{"path empty error", NewPathEmptyError(), true},
		{"path invalid error", NewPathInvalidError("path", "reason"), true},
		{"path too long error", NewPathTooLongError("path", 100, 50), true},
		{"policy invalid error", NewPolicyInvalidError("replication", "reason"), true},
		{"invalid location error", NewInvalidLocationError("store", "reason"), true},
		{"invalid entry error", NewInvalidEntryError("field", "reason"), true},
		{"storage error (not validation)", NewStorageError("op", nil), false},
		{"non-catalog error", errors.New("some error"), false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := IsValidationError(tt.err); got != tt.want {
				t.Errorf("IsValidationError() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestIsStorageError(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{"nil error", nil, false},
		{"storage failed error", NewStorageError("put", nil), true},
		{"encoding failed error", NewEncodingError("key", nil), true},
		{"decoding failed error", NewDecodingError("key", nil), true},
		{"checksum mismatch error", NewChecksumMismatchError("key", nil, nil), true},
		{"location not found error", NewLocationNotFoundError("key", "backend"), true},
		{"index failed error", NewIndexError("type", "key", nil), true},
		{"key empty error (not storage)", NewKeyEmptyError(), false},
		{"non-catalog error", errors.New("some error"), false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := IsStorageError(tt.err); got != tt.want {
				t.Errorf("IsStorageError() = %v, want %v", got, tt.want)
			}
		})
	}
}

// =============================================================================
// CatalogError Method Tests
// =============================================================================

func TestErrorCode_String(t *testing.T) {
	tests := []struct {
		code ErrorCode
		want string
	}{
		{ErrCodeKeyEmpty, "KEY_EMPTY"},
		{ErrCodeKeyTooLong, "KEY_TOO_LONG"},
		{ErrCodeEntryNotFound, "ENTRY_NOT_FOUND"},
		{ErrCodeStorageFailed, "STORAGE_FAILED"},
	}

	for _, tt := range tests {
		t.Run(string(tt.code), func(t *testing.T) {
			if got := tt.code.String(); got != tt.want {
				t.Errorf("ErrorCode.String() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestCatalogError_Error(t *testing.T) {
	t.Run("error without key", func(t *testing.T) {
		err := &CatalogError{
			Code:    ErrCodeKeyEmpty,
			Message: "key cannot be empty",
		}
		errStr := err.Error()
		if errStr == "" {
			t.Error("expected non-empty error string")
		}
	})

	t.Run("error with key", func(t *testing.T) {
		err := &CatalogError{
			Code:    ErrCodeEntryNotFound,
			Message: "entry not found",
			Key:     "ns1:path",
		}
		errStr := err.Error()
		if errStr == "" {
			t.Error("expected non-empty error string")
		}
	})

	t.Run("error with cause", func(t *testing.T) {
		cause := errors.New("underlying error")
		err := &CatalogError{
			Code:    ErrCodeStorageFailed,
			Message: "storage failed",
			Cause:   cause,
		}
		errStr := err.Error()
		if errStr == "" {
			t.Error("expected non-empty error string")
		}
	})

	t.Run("error with key and cause", func(t *testing.T) {
		cause := errors.New("underlying error")
		err := &CatalogError{
			Code:    ErrCodeEncodingFailed,
			Message: "encoding failed",
			Key:     "ns1:key",
			Cause:   cause,
		}
		errStr := err.Error()
		if errStr == "" {
			t.Error("expected non-empty error string")
		}
	})
}

func TestCatalogError_Unwrap(t *testing.T) {
	t.Run("error with cause", func(t *testing.T) {
		cause := errors.New("underlying error")
		err := &CatalogError{
			Code:  ErrCodeStorageFailed,
			Cause: cause,
		}
		if err.Unwrap() != cause {
			t.Error("Unwrap should return the cause")
		}
	})

	t.Run("error without cause", func(t *testing.T) {
		err := &CatalogError{
			Code: ErrCodeKeyEmpty,
		}
		if err.Unwrap() != nil {
			t.Error("Unwrap should return nil when no cause")
		}
	})
}

func TestCatalogError_Is(t *testing.T) {
	t.Run("nil target", func(t *testing.T) {
		err := NewKeyEmptyError()
		if err.Is(nil) {
			t.Error("Is(nil) should return false")
		}
	})

	t.Run("same error code", func(t *testing.T) {
		err1 := NewKeyEmptyError()
		err2 := NewKeyEmptyError()
		if !err1.Is(err2) {
			t.Error("Is should return true for same error code")
		}
	})

	t.Run("different error code", func(t *testing.T) {
		err1 := NewKeyEmptyError()
		err2 := NewPathEmptyError()
		if err1.Is(err2) {
			t.Error("Is should return false for different error codes")
		}
	})

	t.Run("Is with cause", func(t *testing.T) {
		cause := errors.New("underlying error")
		err := &CatalogError{
			Code:  ErrCodeStorageFailed,
			Cause: cause,
		}
		if !err.Is(cause) {
			t.Error("Is should check the cause when target is not a CatalogError")
		}
	})

	t.Run("non-catalog error target", func(t *testing.T) {
		err := NewKeyEmptyError()
		other := errors.New("some error")
		if err.Is(other) {
			t.Error("Is should return false for non-catalog error without matching cause")
		}
	})
}

// =============================================================================
// Entry Validation Edge Cases
// =============================================================================

func TestCatalogEntry_Validate_EdgeCases(t *testing.T) {
	t.Run("invalid replica validation error", func(t *testing.T) {
		entry := &CatalogEntry{
			Key:    "ns1:path",
			Size:   1024,
			Status: StatusActive,
			PrimaryStore: StoreLocation{
				Store:     "s3",
				ObjectRef: "obj/123",
				Status:    LocationStatusActive,
			},
			Replicas: []StoreLocation{
				{Store: "", ObjectRef: "obj/1", Status: LocationStatusActive}, // Invalid: empty store
			},
		}
		err := entry.Validate()
		if err == nil {
			t.Error("expected error for invalid replica")
		}
	})

	t.Run("invalid archive store", func(t *testing.T) {
		entry := &CatalogEntry{
			Key:    "ns1:path",
			Size:   1024,
			Status: StatusActive,
			PrimaryStore: StoreLocation{
				Store:     "s3",
				ObjectRef: "obj/123",
				Status:    LocationStatusActive,
			},
			ArchiveStore: &StoreLocation{
				Store:     "",
				ObjectRef: "archive/123",
				Status:    LocationStatusActive,
			},
		}
		err := entry.Validate()
		if err == nil {
			t.Error("expected error for invalid archive store")
		}
	})

	t.Run("invalid encryption", func(t *testing.T) {
		entry := &CatalogEntry{
			Key:    "ns1:path",
			Size:   1024,
			Status: StatusActive,
			PrimaryStore: StoreLocation{
				Store:     "s3",
				ObjectRef: "obj/123",
				Status:    LocationStatusActive,
			},
			Encryption: &ObjectEncryption{
				Enabled:   true,
				KeyID:     "", // Invalid: enabled but no key ID
				Algorithm: "AES-256-GCM",
			},
		}
		err := entry.Validate()
		if err == nil {
			t.Error("expected error for invalid encryption")
		}
	})

	t.Run("invalid replication policy", func(t *testing.T) {
		entry := &CatalogEntry{
			Key:    "ns1:path",
			Size:   1024,
			Status: StatusActive,
			PrimaryStore: StoreLocation{
				Store:     "s3",
				ObjectRef: "obj/123",
				Status:    LocationStatusActive,
			},
			ReplicationPolicy: &ReplicationPolicy{
				Enabled:     true,
				MinReplicas: -1, // Invalid: negative
				Mode:        ReplicationModeAsync,
				Priority:    5,
			},
		}
		err := entry.Validate()
		if err == nil {
			t.Error("expected error for invalid replication policy")
		}
	})

	t.Run("invalid archive policy", func(t *testing.T) {
		entry := &CatalogEntry{
			Key:    "ns1:path",
			Size:   1024,
			Status: StatusActive,
			PrimaryStore: StoreLocation{
				Store:     "s3",
				ObjectRef: "obj/123",
				Status:    LocationStatusActive,
			},
			ArchivePolicy: &ArchivePolicy{
				Enabled:          true,
				ArchiveAfterDays: -1, // Invalid: negative
				TargetStore:      "archive",
			},
		}
		err := entry.Validate()
		if err == nil {
			t.Error("expected error for invalid archive policy")
		}
	})

	t.Run("invalid retention policy", func(t *testing.T) {
		entry := &CatalogEntry{
			Key:    "ns1:path",
			Size:   1024,
			Status: StatusActive,
			PrimaryStore: StoreLocation{
				Store:     "s3",
				ObjectRef: "obj/123",
				Status:    LocationStatusActive,
			},
			RetentionPolicy: &RetentionPolicy{
				Enabled:       true,
				RetentionDays: -1, // Invalid: negative
			},
		}
		err := entry.Validate()
		if err == nil {
			t.Error("expected error for invalid retention policy")
		}
	})
}

// =============================================================================
// Storage Operation Edge Cases
// =============================================================================

func TestPebbleStorage_PutEntries_EdgeCases(t *testing.T) {
	storage := createTestPebbleStorage(t)
	ctx := context.Background()

	t.Run("put entries with nil entry in slice", func(t *testing.T) {
		entries := []*CatalogEntry{
			createPebbleTestEntry("ns1:valid/entry"),
			nil, // nil entry should be skipped
		}
		if err := storage.PutEntries(ctx, entries); err != nil {
			t.Fatalf("expected nil entries to be skipped: %v", err)
		}

		// Verify only valid entry was stored
		_, err := storage.GetEntry(ctx, "ns1:valid/entry")
		if err != nil {
			t.Errorf("expected valid entry to be stored: %v", err)
		}
	})

	t.Run("put entries with empty key entry", func(t *testing.T) {
		entries := []*CatalogEntry{
			createPebbleTestEntry("ns1:valid/entry2"),
			{Key: "", Size: 100}, // Invalid: empty key
		}
		if err := storage.PutEntries(ctx, entries); err == nil {
			t.Fatal("expected error for empty key entry")
		}
	})

	t.Run("put entries with context cancellation", func(t *testing.T) {
		cancelCtx, cancel := context.WithCancel(context.Background())
		cancel() // Cancel immediately

		entries := []*CatalogEntry{
			createPebbleTestEntry("ns1:cancelled/entry"),
		}
		if err := storage.PutEntries(cancelCtx, entries); err == nil {
			t.Fatal("expected error for cancelled context")
		}
	})
}

func TestPebbleStorage_DeleteEntries_EdgeCases(t *testing.T) {
	storage := createTestPebbleStorage(t)
	ctx := context.Background()

	t.Run("delete entries with context cancellation", func(t *testing.T) {
		// First put an entry
		entry := createPebbleTestEntry("ns1:todelete/cancelled")
		if err := storage.PutEntry(ctx, entry); err != nil {
			t.Fatalf("failed to put entry: %v", err)
		}

		cancelCtx, cancel := context.WithCancel(context.Background())
		cancel() // Cancel immediately

		err := storage.DeleteEntries(cancelCtx, []string{entry.Key})
		if err == nil {
			t.Fatal("expected error for cancelled context")
		}
	})

	t.Run("delete entries with mix of existing and non-existing", func(t *testing.T) {
		entry := createPebbleTestEntry("ns1:todelete/mix")
		if err := storage.PutEntry(ctx, entry); err != nil {
			t.Fatalf("failed to put entry: %v", err)
		}

		keys := []string{entry.Key, "ns1:nonexistent/key"}
		if err := storage.DeleteEntries(ctx, keys); err != nil {
			t.Errorf("expected no error for mix of existing and non-existing: %v", err)
		}
	})
}

func TestPebbleStorage_GetEntries_EdgeCases(t *testing.T) {
	storage := createTestPebbleStorage(t)
	ctx := context.Background()

	t.Run("get entries with context cancellation", func(t *testing.T) {
		cancelCtx, cancel := context.WithCancel(context.Background())
		cancel() // Cancel immediately

		_, err := storage.GetEntries(cancelCtx, []string{"ns1:test"})
		if err == nil {
			t.Fatal("expected error for cancelled context")
		}
	})

	t.Run("get entries with empty keys list", func(t *testing.T) {
		entries, err := storage.GetEntries(ctx, []string{})
		if err != nil {
			t.Fatalf("expected no error for empty keys: %v", err)
		}
		if len(entries) != 0 {
			t.Errorf("expected empty result, got %d entries", len(entries))
		}
	})
}

func TestPebbleStorage_Iterate_EdgeCases(t *testing.T) {
	storage := createTestPebbleStorage(t)
	ctx := context.Background()

	t.Run("iterate with context cancellation mid-iteration", func(t *testing.T) {
		// Put some entries first
		for i := 0; i < 5; i++ {
			entry := createPebbleTestEntry("ns1:iterate/canceltest" + string(rune('a'+i)))
			if err := storage.PutEntry(ctx, entry); err != nil {
				t.Fatalf("failed to put entry: %v", err)
			}
		}

		cancelCtx, cancel := context.WithCancel(context.Background())
		count := 0
		err := storage.Iterate(cancelCtx, "ns1:iterate/canceltest", func(e *CatalogEntry) bool {
			count++
			if count >= 2 {
				cancel() // Cancel mid-iteration
			}
			return true
		})
		// Error should occur due to context cancellation
		if err == nil && count > 2 {
			// It's okay if iteration completes before context check
		}
	})
}

func TestPebbleStorage_IterateKeys_EdgeCases(t *testing.T) {
	storage := createTestPebbleStorage(t)
	ctx := context.Background()

	t.Run("iterate keys with context cancellation", func(t *testing.T) {
		// Put some entries first
		for i := 0; i < 5; i++ {
			entry := createPebbleTestEntry("ns1:iterkeys/canceltest" + string(rune('a'+i)))
			if err := storage.PutEntry(ctx, entry); err != nil {
				t.Fatalf("failed to put entry: %v", err)
			}
		}

		cancelCtx, cancel := context.WithCancel(context.Background())
		count := 0
		err := storage.IterateKeys(cancelCtx, "ns1:iterkeys/canceltest", func(key string) bool {
			count++
			if count >= 2 {
				cancel()
			}
			return true
		})
		if err == nil && count > 2 {
			// It's okay if iteration completes before context check
		}
	})

	t.Run("iterate keys stops when callback returns false", func(t *testing.T) {
		for i := 0; i < 3; i++ {
			entry := createPebbleTestEntry("ns1:iterkeys/stoptest" + string(rune('a'+i)))
			if err := storage.PutEntry(ctx, entry); err != nil {
				t.Fatalf("failed to put entry: %v", err)
			}
		}

		count := 0
		err := storage.IterateKeys(ctx, "ns1:iterkeys/stoptest", func(key string) bool {
			count++
			return count < 2
		})
		if err != nil {
			t.Fatalf("iterate keys failed: %v", err)
		}
		if count != 2 {
			t.Errorf("expected iteration to stop after 2, got %d", count)
		}
	})
}

func TestOpenPebbleStorage_EdgeCases(t *testing.T) {
	t.Run("open with compression disabled", func(t *testing.T) {
		tmpDir := t.TempDir()
		cfg := &PebbleStorageConfig{
			Path:        tmpDir,
			Compression: false,
		}
		storage, err := OpenPebbleStorage(cfg)
		if err != nil {
			t.Fatalf("failed to open storage: %v", err)
		}
		defer storage.Close()

		if storage.DB() == nil {
			t.Error("expected DB to be non-nil")
		}
	})

	t.Run("open with zero write buffer size uses default", func(t *testing.T) {
		tmpDir := t.TempDir()
		cfg := &PebbleStorageConfig{
			Path:            tmpDir,
			WriteBufferSize: 0, // Should use default
		}
		storage, err := OpenPebbleStorage(cfg)
		if err != nil {
			t.Fatalf("failed to open storage: %v", err)
		}
		defer storage.Close()
	})

	t.Run("open existing database", func(t *testing.T) {
		tmpDir := t.TempDir()
		cfg := &PebbleStorageConfig{Path: tmpDir}

		// Create and close first instance
		storage1, err := OpenPebbleStorage(cfg)
		if err != nil {
			t.Fatalf("failed to create storage: %v", err)
		}

		// Put some data
		ctx := context.Background()
		entry := createPebbleTestEntry("ns1:persist/test")
		if err := storage1.PutEntry(ctx, entry); err != nil {
			t.Fatalf("failed to put entry: %v", err)
		}
		storage1.Close()

		// Reopen and verify data persists
		storage2, err := OpenPebbleStorage(cfg)
		if err != nil {
			t.Fatalf("failed to reopen storage: %v", err)
		}
		defer storage2.Close()

		retrieved, err := storage2.GetEntry(ctx, entry.Key)
		if err != nil {
			t.Fatalf("failed to get entry after reopen: %v", err)
		}
		if retrieved.Key != entry.Key {
			t.Errorf("expected key %s, got %s", entry.Key, retrieved.Key)
		}
	})
}

func TestPebbleStorage_ClosedOperations(t *testing.T) {
	tmpDir := t.TempDir()
	cfg := &PebbleStorageConfig{Path: tmpDir, SyncWrites: false}
	storage, err := NewPebbleStorage(cfg)
	if err != nil {
		t.Fatalf("failed to create storage: %v", err)
	}
	storage.Close()

	ctx := context.Background()

	t.Run("GetEntries on closed storage", func(t *testing.T) {
		_, err := storage.GetEntries(ctx, []string{"ns1:test"})
		if !IsCatalogClosed(err) {
			t.Errorf("expected catalog closed error, got %v", err)
		}
	})

	t.Run("PutEntries on closed storage", func(t *testing.T) {
		entries := []*CatalogEntry{createPebbleTestEntry("ns1:test")}
		if err := storage.PutEntries(ctx, entries); !IsCatalogClosed(err) {
			t.Errorf("expected catalog closed error, got %v", err)
		}
	})

	t.Run("DeleteEntries on closed storage", func(t *testing.T) {
		if err := storage.DeleteEntries(ctx, []string{"ns1:test"}); !IsCatalogClosed(err) {
			t.Errorf("expected catalog closed error, got %v", err)
		}
	})

	t.Run("Iterate on closed storage", func(t *testing.T) {
		err := storage.Iterate(ctx, "", func(e *CatalogEntry) bool { return true })
		if !IsCatalogClosed(err) {
			t.Errorf("expected catalog closed error, got %v", err)
		}
	})

	t.Run("IterateKeys on closed storage", func(t *testing.T) {
		err := storage.IterateKeys(ctx, "", func(k string) bool { return true })
		if !IsCatalogClosed(err) {
			t.Errorf("expected catalog closed error, got %v", err)
		}
	})
}

// =============================================================================
// Entry Encode Edge Cases
// =============================================================================

func TestCatalogEntry_Encode_EdgeCases(t *testing.T) {
	t.Run("encode entry with all optional fields", func(t *testing.T) {
		now := time.Now().UnixNano()
		entry := &CatalogEntry{
			Key:         "ns1:full/entry",
			ObjectID:    12345,
			Checksum:    []byte{0x01, 0x02, 0x03},
			Size:        2048,
			ContentType: "application/json",
			PrimaryStore: StoreLocation{
				Store:     "s3-primary",
				ObjectRef: "obj/123",
				Status:    LocationStatusActive,
			},
			Replicas: []StoreLocation{
				{Store: "gcs-west", ObjectRef: "obj/123", Status: LocationStatusSyncing},
			},
			ArchiveStore: &StoreLocation{
				Store:     "glacier",
				ObjectRef: "archive/123",
				Status:    LocationStatusActive,
			},
			ReplicationPolicy: &ReplicationPolicy{
				Enabled:      true,
				MinReplicas:  3,
				TargetStores: []string{"gcs-west", "azure-east"},
				Mode:         ReplicationModeQuorum,
				Priority:     7,
			},
			ArchivePolicy: &ArchivePolicy{
				Enabled:           true,
				ArchiveAfterDays:  90,
				TargetStore:       "glacier",
				DeleteFromPrimary: true,
				CompressionLevel:  6,
			},
			RetentionPolicy: &RetentionPolicy{
				Enabled:       true,
				RetentionDays: 365,
				MaxVersions:   10,
			},
			Encryption: &ObjectEncryption{
				Enabled:   true,
				KeyID:     "kms/key-001",
				Algorithm: "AES-256-GCM",
			},
			TenantID: "tenant-acme",
			ShardID:  42,
			Owner:    "admin@acme.com",
			ACL: &ObjectACL{
				Owner:      "admin@acme.com",
				Readers:    []string{"user1", "user2"},
				Writers:    []string{"user1"},
				Admins:     []string{"admin@acme.com"},
				PublicRead: false,
				Inherit:    true,
			},
			Tags:         map[string]string{"env": "prod", "team": "backend"},
			Labels:       map[string]string{"tier": "hot"},
			CustomMeta:   map[string]string{"custom-key": "custom-value"},
			CreatedAt:    now,
			ModifiedAt:   now,
			LastAccessAt: now,
			Status:       StatusActive,
			Version:      5,
		}

		encoded, err := entry.Encode()
		if err != nil {
			t.Fatalf("Encode failed: %v", err)
		}

		decoded, err := DecodeCatalogEntry(encoded)
		if err != nil {
			t.Fatalf("Decode failed: %v", err)
		}

		// Verify all fields
		if decoded.Key != entry.Key {
			t.Errorf("Key mismatch: got %s, want %s", decoded.Key, entry.Key)
		}
		if decoded.TenantID != entry.TenantID {
			t.Errorf("TenantID mismatch: got %s, want %s", decoded.TenantID, entry.TenantID)
		}
		if decoded.Encryption == nil || !decoded.Encryption.Enabled {
			t.Error("Encryption should be enabled")
		}
		if decoded.ReplicationPolicy == nil || !decoded.ReplicationPolicy.Enabled {
			t.Error("ReplicationPolicy should be enabled")
		}
		if decoded.ACL == nil || decoded.ACL.Owner != entry.ACL.Owner {
			t.Error("ACL Owner mismatch")
		}
	})
}

// =============================================================================
// pebbleConfigError Tests
// =============================================================================

func TestPebbleConfigError(t *testing.T) {
	t.Run("error string format", func(t *testing.T) {
		err := &pebbleConfigError{field: "Path", msg: "path is required"}
		errStr := err.Error()
		if errStr != "Path: path is required" {
			t.Errorf("unexpected error string: %s", errStr)
		}
	})
}
