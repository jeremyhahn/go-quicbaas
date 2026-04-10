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
	"testing"
	"time"
)

func TestLocationStatus_String(t *testing.T) {
	tests := []struct {
		name   string
		status LocationStatus
		want   string
	}{
		{"active", LocationStatusActive, "active"},
		{"syncing", LocationStatusSyncing, "syncing"},
		{"failed", LocationStatusFailed, "failed"},
		{"stale", LocationStatusStale, "stale"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.status.String(); got != tt.want {
				t.Errorf("LocationStatus.String() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestLocationStatus_IsValid(t *testing.T) {
	tests := []struct {
		name   string
		status LocationStatus
		want   bool
	}{
		{"active is valid", LocationStatusActive, true},
		{"syncing is valid", LocationStatusSyncing, true},
		{"failed is valid", LocationStatusFailed, true},
		{"stale is valid", LocationStatusStale, true},
		{"unknown is invalid", LocationStatus("unknown"), false},
		{"empty is invalid", LocationStatus(""), false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.status.IsValid(); got != tt.want {
				t.Errorf("LocationStatus.IsValid() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestNewStoreLocation(t *testing.T) {
	store := "s3-primary"
	objectRef := "objects/123/v1"

	loc := NewStoreLocation(store, objectRef)

	if loc == nil {
		t.Fatal("NewStoreLocation returned nil")
	}

	if loc.Store != store {
		t.Errorf("Store = %v, want %v", loc.Store, store)
	}

	if loc.ObjectRef != objectRef {
		t.Errorf("ObjectRef = %v, want %v", loc.ObjectRef, objectRef)
	}

	if loc.Status != LocationStatusActive {
		t.Errorf("Status = %v, want %v", loc.Status, LocationStatusActive)
	}

	if loc.SyncedAt == 0 {
		t.Error("SyncedAt should be set")
	}
}

func TestStoreLocation_IsHealthy(t *testing.T) {
	tests := []struct {
		name string
		loc  *StoreLocation
		want bool
	}{
		{
			name: "nil location",
			loc:  nil,
			want: false,
		},
		{
			name: "active is healthy",
			loc:  &StoreLocation{Status: LocationStatusActive},
			want: true,
		},
		{
			name: "syncing is healthy",
			loc:  &StoreLocation{Status: LocationStatusSyncing},
			want: true,
		},
		{
			name: "failed is not healthy",
			loc:  &StoreLocation{Status: LocationStatusFailed},
			want: false,
		},
		{
			name: "stale is not healthy",
			loc:  &StoreLocation{Status: LocationStatusStale},
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.loc.IsHealthy(); got != tt.want {
				t.Errorf("StoreLocation.IsHealthy() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestStoreLocation_IsActive(t *testing.T) {
	tests := []struct {
		name string
		loc  *StoreLocation
		want bool
	}{
		{"nil", nil, false},
		{"active", &StoreLocation{Status: LocationStatusActive}, true},
		{"syncing", &StoreLocation{Status: LocationStatusSyncing}, false},
		{"failed", &StoreLocation{Status: LocationStatusFailed}, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.loc.IsActive(); got != tt.want {
				t.Errorf("StoreLocation.IsActive() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestStoreLocation_IsFailed(t *testing.T) {
	tests := []struct {
		name string
		loc  *StoreLocation
		want bool
	}{
		{"nil", nil, false},
		{"active", &StoreLocation{Status: LocationStatusActive}, false},
		{"failed", &StoreLocation{Status: LocationStatusFailed}, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.loc.IsFailed(); got != tt.want {
				t.Errorf("StoreLocation.IsFailed() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestStoreLocation_StatusMarkers(t *testing.T) {
	t.Run("MarkActive", func(t *testing.T) {
		loc := &StoreLocation{Status: LocationStatusFailed}
		loc.MarkActive()
		if loc.Status != LocationStatusActive {
			t.Errorf("Status = %v, want %v", loc.Status, LocationStatusActive)
		}
		if loc.SyncedAt == 0 {
			t.Error("SyncedAt should be updated")
		}
	})

	t.Run("MarkActive on nil", func(t *testing.T) {
		var loc *StoreLocation
		loc.MarkActive() // Should not panic
	})

	t.Run("MarkSyncing", func(t *testing.T) {
		loc := &StoreLocation{Status: LocationStatusActive}
		loc.MarkSyncing()
		if loc.Status != LocationStatusSyncing {
			t.Errorf("Status = %v, want %v", loc.Status, LocationStatusSyncing)
		}
	})

	t.Run("MarkSyncing on nil", func(t *testing.T) {
		var loc *StoreLocation
		loc.MarkSyncing() // Should not panic
	})

	t.Run("MarkFailed", func(t *testing.T) {
		loc := &StoreLocation{Status: LocationStatusActive}
		loc.MarkFailed()
		if loc.Status != LocationStatusFailed {
			t.Errorf("Status = %v, want %v", loc.Status, LocationStatusFailed)
		}
	})

	t.Run("MarkFailed on nil", func(t *testing.T) {
		var loc *StoreLocation
		loc.MarkFailed() // Should not panic
	})

	t.Run("MarkStale", func(t *testing.T) {
		loc := &StoreLocation{Status: LocationStatusActive}
		loc.MarkStale()
		if loc.Status != LocationStatusStale {
			t.Errorf("Status = %v, want %v", loc.Status, LocationStatusStale)
		}
	})

	t.Run("MarkStale on nil", func(t *testing.T) {
		var loc *StoreLocation
		loc.MarkStale() // Should not panic
	})
}

func TestStoreLocation_Clone(t *testing.T) {
	t.Run("clone nil", func(t *testing.T) {
		var loc *StoreLocation
		clone := loc.Clone()
		if clone != nil {
			t.Error("Clone of nil should be nil")
		}
	})

	t.Run("clone with all fields", func(t *testing.T) {
		loc := &StoreLocation{
			Store:      "s3-primary",
			ObjectRef:  "objects/123/v1",
			Status:     LocationStatusActive,
			Checksum:   []byte{1, 2, 3, 4},
			StoredSize: 1024,
			Compressed: true,
			Encryption: &LocationEncryption{
				KeyID:     "key-123",
				Algorithm: "AES-256-GCM",
				Nonce:     []byte{5, 6, 7, 8},
			},
			SyncedAt: time.Now().UnixNano(),
		}

		clone := loc.Clone()

		if clone == loc {
			t.Error("Clone should be a different instance")
		}

		if clone.Store != loc.Store {
			t.Errorf("Store = %v, want %v", clone.Store, loc.Store)
		}

		if clone.ObjectRef != loc.ObjectRef {
			t.Errorf("ObjectRef = %v, want %v", clone.ObjectRef, loc.ObjectRef)
		}

		// Verify checksum is copied
		if &clone.Checksum[0] == &loc.Checksum[0] {
			t.Error("Checksum should be a copy, not same slice")
		}

		// Verify encryption is copied
		if clone.Encryption == loc.Encryption {
			t.Error("Encryption should be a different instance")
		}

		// Modify original and verify clone is unaffected
		loc.Store = "modified"
		loc.Checksum[0] = 99
		loc.Encryption.KeyID = "modified"

		if clone.Store == "modified" {
			t.Error("Clone Store was modified")
		}
		if clone.Checksum[0] == 99 {
			t.Error("Clone Checksum was modified")
		}
		if clone.Encryption.KeyID == "modified" {
			t.Error("Clone Encryption was modified")
		}
	})
}

func TestStoreLocation_Validate(t *testing.T) {
	tests := []struct {
		name    string
		loc     *StoreLocation
		wantErr bool
	}{
		{
			name:    "nil location",
			loc:     nil,
			wantErr: true,
		},
		{
			name:    "empty store",
			loc:     &StoreLocation{ObjectRef: "obj/123", Status: LocationStatusActive},
			wantErr: true,
		},
		{
			name:    "empty object ref",
			loc:     &StoreLocation{Store: "s3", Status: LocationStatusActive},
			wantErr: true,
		},
		{
			name:    "invalid status",
			loc:     &StoreLocation{Store: "s3", ObjectRef: "obj/123", Status: "invalid"},
			wantErr: true,
		},
		{
			name:    "negative stored size",
			loc:     &StoreLocation{Store: "s3", ObjectRef: "obj/123", Status: LocationStatusActive, StoredSize: -1},
			wantErr: true,
		},
		{
			name:    "valid location",
			loc:     NewStoreLocation("s3", "obj/123"),
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.loc.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("StoreLocation.Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestLocationEncryption_Clone(t *testing.T) {
	t.Run("clone nil", func(t *testing.T) {
		var enc *LocationEncryption
		clone := enc.Clone()
		if clone != nil {
			t.Error("Clone of nil should be nil")
		}
	})

	t.Run("clone with all fields", func(t *testing.T) {
		enc := &LocationEncryption{
			KeyID:     "key-123",
			Algorithm: "AES-256-GCM",
			Nonce:     []byte{1, 2, 3, 4},
		}

		clone := enc.Clone()

		if clone == enc {
			t.Error("Clone should be a different instance")
		}

		if clone.KeyID != enc.KeyID {
			t.Errorf("KeyID = %v, want %v", clone.KeyID, enc.KeyID)
		}

		// Modify original
		enc.Nonce[0] = 99

		if clone.Nonce[0] == 99 {
			t.Error("Clone Nonce was modified")
		}
	})
}

func TestLocationEncryption_Validate(t *testing.T) {
	tests := []struct {
		name    string
		enc     *LocationEncryption
		wantErr bool
	}{
		{"nil is valid", nil, false},
		{"empty key ID", &LocationEncryption{Algorithm: "AES-256-GCM"}, true},
		{"empty algorithm", &LocationEncryption{KeyID: "key-123"}, true},
		{"valid", &LocationEncryption{KeyID: "key-123", Algorithm: "AES-256-GCM"}, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.enc.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("LocationEncryption.Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestObjectEncryption_Clone(t *testing.T) {
	t.Run("clone nil", func(t *testing.T) {
		var enc *ObjectEncryption
		clone := enc.Clone()
		if clone != nil {
			t.Error("Clone of nil should be nil")
		}
	})

	t.Run("clone with all fields", func(t *testing.T) {
		enc := &ObjectEncryption{
			Enabled:   true,
			KeyID:     "key-123",
			Algorithm: "AES-256-GCM",
		}

		clone := enc.Clone()

		if clone == enc {
			t.Error("Clone should be a different instance")
		}

		if clone.Enabled != enc.Enabled {
			t.Errorf("Enabled = %v, want %v", clone.Enabled, enc.Enabled)
		}

		if clone.KeyID != enc.KeyID {
			t.Errorf("KeyID = %v, want %v", clone.KeyID, enc.KeyID)
		}
	})
}

func TestObjectEncryption_Validate(t *testing.T) {
	tests := []struct {
		name    string
		enc     *ObjectEncryption
		wantErr bool
	}{
		{"nil is valid", nil, false},
		{"disabled is valid", &ObjectEncryption{Enabled: false}, false},
		{"enabled with empty key ID", &ObjectEncryption{Enabled: true, Algorithm: "AES"}, true},
		{"enabled with empty algorithm", &ObjectEncryption{Enabled: true, KeyID: "key"}, true},
		{"enabled with all fields", &ObjectEncryption{Enabled: true, KeyID: "key", Algorithm: "AES"}, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.enc.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("ObjectEncryption.Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestObjectACL_Clone(t *testing.T) {
	t.Run("clone nil", func(t *testing.T) {
		var acl *ObjectACL
		clone := acl.Clone()
		if clone != nil {
			t.Error("Clone of nil should be nil")
		}
	})

	t.Run("clone with all fields", func(t *testing.T) {
		acl := &ObjectACL{
			Owner:      "user1",
			Readers:    []string{"reader1", "reader2"},
			Writers:    []string{"writer1"},
			Admins:     []string{"admin1"},
			PublicRead: true,
			Inherit:    true,
		}

		clone := acl.Clone()

		if clone == acl {
			t.Error("Clone should be a different instance")
		}

		if clone.Owner != acl.Owner {
			t.Errorf("Owner = %v, want %v", clone.Owner, acl.Owner)
		}

		// Modify original
		acl.Readers[0] = "modified"

		if clone.Readers[0] == "modified" {
			t.Error("Clone Readers was modified")
		}
	})
}

func TestObjectACL_HasReadAccess(t *testing.T) {
	acl := &ObjectACL{
		Owner:      "owner",
		Readers:    []string{"reader1", "reader2"},
		Writers:    []string{"writer1"},
		Admins:     []string{"admin1"},
		PublicRead: false,
	}

	tests := []struct {
		name     string
		acl      *ObjectACL
		identity string
		want     bool
	}{
		{"nil ACL", nil, "anyone", false},
		{"owner has access", acl, "owner", true},
		{"reader has access", acl, "reader1", true},
		{"writer has access", acl, "writer1", true},
		{"admin has access", acl, "admin1", true},
		{"unknown has no access", acl, "unknown", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.acl.HasReadAccess(tt.identity); got != tt.want {
				t.Errorf("HasReadAccess() = %v, want %v", got, tt.want)
			}
		})
	}

	t.Run("public read", func(t *testing.T) {
		publicACL := &ObjectACL{PublicRead: true}
		if !publicACL.HasReadAccess("anyone") {
			t.Error("PublicRead should allow any identity")
		}
	})
}

func TestObjectACL_HasWriteAccess(t *testing.T) {
	acl := &ObjectACL{
		Owner:   "owner",
		Readers: []string{"reader1"},
		Writers: []string{"writer1"},
		Admins:  []string{"admin1"},
	}

	tests := []struct {
		name     string
		acl      *ObjectACL
		identity string
		want     bool
	}{
		{"nil ACL", nil, "anyone", false},
		{"owner has access", acl, "owner", true},
		{"reader has no write access", acl, "reader1", false},
		{"writer has access", acl, "writer1", true},
		{"admin has access", acl, "admin1", true},
		{"unknown has no access", acl, "unknown", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.acl.HasWriteAccess(tt.identity); got != tt.want {
				t.Errorf("HasWriteAccess() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestObjectACL_HasAdminAccess(t *testing.T) {
	acl := &ObjectACL{
		Owner:   "owner",
		Readers: []string{"reader1"},
		Writers: []string{"writer1"},
		Admins:  []string{"admin1"},
	}

	tests := []struct {
		name     string
		acl      *ObjectACL
		identity string
		want     bool
	}{
		{"nil ACL", nil, "anyone", false},
		{"owner has access", acl, "owner", true},
		{"reader has no admin access", acl, "reader1", false},
		{"writer has no admin access", acl, "writer1", false},
		{"admin has access", acl, "admin1", true},
		{"unknown has no access", acl, "unknown", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.acl.HasAdminAccess(tt.identity); got != tt.want {
				t.Errorf("HasAdminAccess() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestObjectACL_AddRemove(t *testing.T) {
	t.Run("AddReader", func(t *testing.T) {
		acl := &ObjectACL{}
		acl.AddReader("reader1")
		if len(acl.Readers) != 1 || acl.Readers[0] != "reader1" {
			t.Error("Reader not added")
		}

		// Add duplicate
		acl.AddReader("reader1")
		if len(acl.Readers) != 1 {
			t.Error("Duplicate reader should not be added")
		}
	})

	t.Run("AddReader on nil", func(t *testing.T) {
		var acl *ObjectACL
		acl.AddReader("reader1") // Should not panic
	})

	t.Run("AddWriter", func(t *testing.T) {
		acl := &ObjectACL{}
		acl.AddWriter("writer1")
		if len(acl.Writers) != 1 || acl.Writers[0] != "writer1" {
			t.Error("Writer not added")
		}

		// Add duplicate
		acl.AddWriter("writer1")
		if len(acl.Writers) != 1 {
			t.Error("Duplicate writer should not be added")
		}
	})

	t.Run("AddWriter on nil", func(t *testing.T) {
		var acl *ObjectACL
		acl.AddWriter("writer1") // Should not panic
	})

	t.Run("AddAdmin", func(t *testing.T) {
		acl := &ObjectACL{}
		acl.AddAdmin("admin1")
		if len(acl.Admins) != 1 || acl.Admins[0] != "admin1" {
			t.Error("Admin not added")
		}

		// Add duplicate
		acl.AddAdmin("admin1")
		if len(acl.Admins) != 1 {
			t.Error("Duplicate admin should not be added")
		}
	})

	t.Run("AddAdmin on nil", func(t *testing.T) {
		var acl *ObjectACL
		acl.AddAdmin("admin1") // Should not panic
	})

	t.Run("RemoveReader", func(t *testing.T) {
		acl := &ObjectACL{Readers: []string{"reader1", "reader2"}}
		acl.RemoveReader("reader1")
		if len(acl.Readers) != 1 || acl.Readers[0] != "reader2" {
			t.Error("Reader not removed correctly")
		}

		// Remove non-existent
		acl.RemoveReader("reader3")
		if len(acl.Readers) != 1 {
			t.Error("Removing non-existent reader should not change list")
		}
	})

	t.Run("RemoveReader on nil", func(t *testing.T) {
		var acl *ObjectACL
		acl.RemoveReader("reader1") // Should not panic
	})

	t.Run("RemoveWriter", func(t *testing.T) {
		acl := &ObjectACL{Writers: []string{"writer1", "writer2"}}
		acl.RemoveWriter("writer1")
		if len(acl.Writers) != 1 || acl.Writers[0] != "writer2" {
			t.Error("Writer not removed correctly")
		}
	})

	t.Run("RemoveWriter on nil", func(t *testing.T) {
		var acl *ObjectACL
		acl.RemoveWriter("writer1") // Should not panic
	})

	t.Run("RemoveAdmin", func(t *testing.T) {
		acl := &ObjectACL{Admins: []string{"admin1", "admin2"}}
		acl.RemoveAdmin("admin1")
		if len(acl.Admins) != 1 || acl.Admins[0] != "admin2" {
			t.Error("Admin not removed correctly")
		}
	})

	t.Run("RemoveAdmin on nil", func(t *testing.T) {
		var acl *ObjectACL
		acl.RemoveAdmin("admin1") // Should not panic
	})
}
