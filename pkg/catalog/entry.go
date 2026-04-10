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
	"encoding/hex"
	"time"

	"github.com/vmihailenco/msgpack/v5"
)

// EntryStatus represents the state of a catalog entry.
type EntryStatus string

const (
	// StatusActive indicates the entry is active and accessible.
	StatusActive EntryStatus = "active"

	// StatusPending indicates the entry is being created or updated.
	StatusPending EntryStatus = "pending"

	// StatusArchived indicates the entry has been moved to archive storage.
	StatusArchived EntryStatus = "archived"

	// StatusDeleted indicates the entry is marked for deletion.
	StatusDeleted EntryStatus = "deleted"
)

// String returns the string representation of the entry status.
func (s EntryStatus) String() string {
	return string(s)
}

// IsValid checks if the entry status is valid.
func (s EntryStatus) IsValid() bool {
	switch s {
	case StatusActive, StatusPending, StatusArchived, StatusDeleted:
		return true
	default:
		return false
	}
}

// CatalogEntry represents the full metadata for an object in the catalog.
type CatalogEntry struct {
	// === IDENTITY (immutable) ===

	// Key is the logical key in format "namespace:path".
	Key string `msgpack:"key"`

	// ObjectID is the internal unique ID (snowflake).
	ObjectID uint64 `msgpack:"object_id"`

	// === CONTENT INFO ===

	// Checksum is the SHA-256 hash for integrity/dedup.
	Checksum []byte `msgpack:"checksum,omitempty"`

	// Size is the original uncompressed size in bytes.
	Size int64 `msgpack:"size"`

	// ContentType is the MIME type of the object.
	ContentType string `msgpack:"content_type,omitempty"`

	// === LOCATION TRACKING ===

	// PrimaryStore is the current primary backend location.
	PrimaryStore StoreLocation `msgpack:"primary_store"`

	// Replicas contains additional copies of the object.
	Replicas []StoreLocation `msgpack:"replicas,omitempty"`

	// ArchiveStore is the cold storage copy (nil if not archived).
	ArchiveStore *StoreLocation `msgpack:"archive_store,omitempty"`

	// === POLICIES ===

	// ReplicationPolicy defines how the object is replicated.
	ReplicationPolicy *ReplicationPolicy `msgpack:"replication_policy,omitempty"`

	// ArchivePolicy defines when and how the object is archived.
	ArchivePolicy *ArchivePolicy `msgpack:"archive_policy,omitempty"`

	// RetentionPolicy defines how long the object is kept.
	RetentionPolicy *RetentionPolicy `msgpack:"retention_policy,omitempty"`

	// === ENCRYPTION ===

	// Encryption describes encryption for the object.
	Encryption *ObjectEncryption `msgpack:"encryption,omitempty"`

	// === MULTI-TENANCY ===

	// TenantID identifies the tenant that owns the object.
	TenantID string `msgpack:"tenant_id,omitempty"`

	// ShardID identifies the shard containing the object.
	ShardID uint64 `msgpack:"shard_id"`

	// Owner is the user/service that created the object.
	Owner string `msgpack:"owner,omitempty"`

	// === ACCESS CONTROL ===

	// ACL describes access control for the object.
	ACL *ObjectACL `msgpack:"acl,omitempty"`

	// === METADATA ===

	// Tags are user-defined key-value tags.
	Tags map[string]string `msgpack:"tags,omitempty"`

	// Labels are system-defined key-value labels.
	Labels map[string]string `msgpack:"labels,omitempty"`

	// CustomMeta is custom metadata.
	CustomMeta map[string]string `msgpack:"custom_meta,omitempty"`

	// === TIMESTAMPS (Unix nanoseconds) ===

	// CreatedAt is the creation time.
	CreatedAt int64 `msgpack:"created_at"`

	// ModifiedAt is the last modification time.
	ModifiedAt int64 `msgpack:"modified_at"`

	// LastAccessAt is the last read access time.
	LastAccessAt int64 `msgpack:"last_access_at"`

	// ArchivedAt is when the object was archived (0 if not).
	ArchivedAt int64 `msgpack:"archived_at"`

	// === STATUS ===

	// Status is the current state of the entry.
	Status EntryStatus `msgpack:"status"`

	// Version is for optimistic concurrency control.
	Version uint64 `msgpack:"version"`
}

// NewCatalogEntry creates a new entry with defaults.
func NewCatalogEntry(key string, size int64, checksum []byte) *CatalogEntry {
	now := time.Now().UnixNano()
	entry := &CatalogEntry{
		Key:          key,
		Size:         size,
		CreatedAt:    now,
		ModifiedAt:   now,
		LastAccessAt: now,
		Status:       StatusActive,
		Version:      1,
	}

	if checksum != nil {
		entry.Checksum = make([]byte, len(checksum))
		copy(entry.Checksum, checksum)
	}

	return entry
}

// Clone creates a deep copy of the catalog entry.
func (e *CatalogEntry) Clone() *CatalogEntry {
	if e == nil {
		return nil
	}

	clone := &CatalogEntry{
		Key:          e.Key,
		ObjectID:     e.ObjectID,
		Size:         e.Size,
		ContentType:  e.ContentType,
		PrimaryStore: *e.PrimaryStore.Clone(),
		TenantID:     e.TenantID,
		ShardID:      e.ShardID,
		Owner:        e.Owner,
		CreatedAt:    e.CreatedAt,
		ModifiedAt:   e.ModifiedAt,
		LastAccessAt: e.LastAccessAt,
		ArchivedAt:   e.ArchivedAt,
		Status:       e.Status,
		Version:      e.Version,
	}

	// Clone checksum
	if e.Checksum != nil {
		clone.Checksum = make([]byte, len(e.Checksum))
		copy(clone.Checksum, e.Checksum)
	}

	// Clone replicas
	if e.Replicas != nil {
		clone.Replicas = make([]StoreLocation, len(e.Replicas))
		for i, r := range e.Replicas {
			if cloned := r.Clone(); cloned != nil {
				clone.Replicas[i] = *cloned
			}
		}
	}

	// Clone archive store
	clone.ArchiveStore = e.ArchiveStore.Clone()

	// Clone policies
	clone.ReplicationPolicy = e.ReplicationPolicy.Clone()
	clone.ArchivePolicy = e.ArchivePolicy.Clone()
	clone.RetentionPolicy = e.RetentionPolicy.Clone()

	// Clone encryption
	clone.Encryption = e.Encryption.Clone()

	// Clone ACL
	clone.ACL = e.ACL.Clone()

	// Clone maps
	if e.Tags != nil {
		clone.Tags = make(map[string]string, len(e.Tags))
		for k, v := range e.Tags {
			clone.Tags[k] = v
		}
	}

	if e.Labels != nil {
		clone.Labels = make(map[string]string, len(e.Labels))
		for k, v := range e.Labels {
			clone.Labels[k] = v
		}
	}

	if e.CustomMeta != nil {
		clone.CustomMeta = make(map[string]string, len(e.CustomMeta))
		for k, v := range e.CustomMeta {
			clone.CustomMeta[k] = v
		}
	}

	return clone
}

// Encode serializes the entry to msgpack bytes.
func (e *CatalogEntry) Encode() ([]byte, error) {
	if e == nil {
		return nil, NewInvalidEntryError("entry", "cannot encode nil entry")
	}

	data, err := msgpack.Marshal(e)
	if err != nil {
		return nil, NewEncodingError(e.Key, err)
	}

	return data, nil
}

// DecodeCatalogEntry deserializes msgpack bytes to a CatalogEntry.
func DecodeCatalogEntry(data []byte) (*CatalogEntry, error) {
	if len(data) == 0 {
		return nil, NewDecodingError("", NewInvalidEntryError("data", "cannot decode empty data"))
	}

	var entry CatalogEntry
	if err := msgpack.Unmarshal(data, &entry); err != nil {
		return nil, NewDecodingError("", err)
	}

	return &entry, nil
}

// GetAllLocations returns all storage locations (primary + replicas + archive).
func (e *CatalogEntry) GetAllLocations() []StoreLocation {
	if e == nil {
		return nil
	}

	// Estimate capacity
	capacity := 1 + len(e.Replicas)
	if e.ArchiveStore != nil {
		capacity++
	}

	locations := make([]StoreLocation, 0, capacity)

	// Add primary store
	locations = append(locations, e.PrimaryStore)

	// Add replicas
	locations = append(locations, e.Replicas...)

	// Add archive store if present
	if e.ArchiveStore != nil {
		locations = append(locations, *e.ArchiveStore)
	}

	return locations
}

// HasLocation checks if the entry has a location in the specified store.
func (e *CatalogEntry) HasLocation(store string) bool {
	if e == nil || store == "" {
		return false
	}

	if e.PrimaryStore.Store == store {
		return true
	}

	for _, r := range e.Replicas {
		if r.Store == store {
			return true
		}
	}

	if e.ArchiveStore != nil && e.ArchiveStore.Store == store {
		return true
	}

	return false
}

// GetPrimaryStoreName returns the name of the primary store, or empty string if not set.
func (e *CatalogEntry) GetPrimaryStoreName() string {
	if e == nil {
		return ""
	}
	return e.PrimaryStore.Store
}

// GetEncryptionKeyID returns the encryption key ID, or empty string if not encrypted.
func (e *CatalogEntry) GetEncryptionKeyID() string {
	if e == nil || e.Encryption == nil || !e.Encryption.Enabled {
		return ""
	}
	return e.Encryption.KeyID
}

// GetLocation returns the location for a specific store, or nil if not found.
func (e *CatalogEntry) GetLocation(store string) *StoreLocation {
	if e == nil || store == "" {
		return nil
	}

	if e.PrimaryStore.Store == store {
		return e.PrimaryStore.Clone()
	}

	for i := range e.Replicas {
		if e.Replicas[i].Store == store {
			return e.Replicas[i].Clone()
		}
	}

	if e.ArchiveStore != nil && e.ArchiveStore.Store == store {
		return e.ArchiveStore.Clone()
	}

	return nil
}

// UpdateAccessTime updates the LastAccessAt timestamp.
func (e *CatalogEntry) UpdateAccessTime() {
	if e == nil {
		return
	}
	e.LastAccessAt = time.Now().UnixNano()
}

// UpdateModifiedTime updates the ModifiedAt timestamp and increments version.
func (e *CatalogEntry) UpdateModifiedTime() {
	if e == nil {
		return
	}
	e.ModifiedAt = time.Now().UnixNano()
	e.Version++
}

// SetTag sets a user-defined tag.
func (e *CatalogEntry) SetTag(key, value string) {
	if e == nil {
		return
	}
	if e.Tags == nil {
		e.Tags = make(map[string]string)
	}
	e.Tags[key] = value
}

// GetTag returns a tag value, or empty string if not found.
func (e *CatalogEntry) GetTag(key string) string {
	if e == nil || e.Tags == nil {
		return ""
	}
	return e.Tags[key]
}

// RemoveTag removes a user-defined tag.
func (e *CatalogEntry) RemoveTag(key string) {
	if e == nil || e.Tags == nil {
		return
	}
	delete(e.Tags, key)
}

// SetLabel sets a system-defined label.
func (e *CatalogEntry) SetLabel(key, value string) {
	if e == nil {
		return
	}
	if e.Labels == nil {
		e.Labels = make(map[string]string)
	}
	e.Labels[key] = value
}

// GetLabel returns a label value, or empty string if not found.
func (e *CatalogEntry) GetLabel(key string) string {
	if e == nil || e.Labels == nil {
		return ""
	}
	return e.Labels[key]
}

// SetCustomMeta sets custom metadata.
func (e *CatalogEntry) SetCustomMeta(key, value string) {
	if e == nil {
		return
	}
	if e.CustomMeta == nil {
		e.CustomMeta = make(map[string]string)
	}
	e.CustomMeta[key] = value
}

// GetCustomMeta returns custom metadata, or empty string if not found.
func (e *CatalogEntry) GetCustomMeta(key string) string {
	if e == nil || e.CustomMeta == nil {
		return ""
	}
	return e.CustomMeta[key]
}

// IsActive returns true if the entry is active.
func (e *CatalogEntry) IsActive() bool {
	if e == nil {
		return false
	}
	return e.Status == StatusActive
}

// IsArchived returns true if the entry is archived.
func (e *CatalogEntry) IsArchived() bool {
	if e == nil {
		return false
	}
	return e.Status == StatusArchived
}

// IsDeleted returns true if the entry is marked for deletion.
func (e *CatalogEntry) IsDeleted() bool {
	if e == nil {
		return false
	}
	return e.Status == StatusDeleted
}

// MarkArchived marks the entry as archived and sets the archived timestamp.
func (e *CatalogEntry) MarkArchived() {
	if e == nil {
		return
	}
	e.Status = StatusArchived
	e.ArchivedAt = time.Now().UnixNano()
	e.ModifiedAt = e.ArchivedAt
	e.Version++
}

// MarkDeleted marks the entry for deletion.
func (e *CatalogEntry) MarkDeleted() {
	if e == nil {
		return
	}
	e.Status = StatusDeleted
	e.ModifiedAt = time.Now().UnixNano()
	e.Version++
}

// AddReplica adds a replica location to the entry.
func (e *CatalogEntry) AddReplica(loc StoreLocation) {
	if e == nil {
		return
	}

	// Check for duplicate
	for _, r := range e.Replicas {
		if r.Store == loc.Store {
			return
		}
	}

	e.Replicas = append(e.Replicas, loc)
	e.ModifiedAt = time.Now().UnixNano()
	e.Version++
}

// RemoveReplica removes a replica location by store name.
func (e *CatalogEntry) RemoveReplica(store string) bool {
	if e == nil {
		return false
	}

	for i, r := range e.Replicas {
		if r.Store == store {
			e.Replicas = append(e.Replicas[:i], e.Replicas[i+1:]...)
			e.ModifiedAt = time.Now().UnixNano()
			e.Version++
			return true
		}
	}

	return false
}

// SetArchiveStore sets the archive store location.
func (e *CatalogEntry) SetArchiveStore(loc *StoreLocation) {
	if e == nil {
		return
	}
	e.ArchiveStore = loc.Clone()
	e.ModifiedAt = time.Now().UnixNano()
	e.Version++
}

// GetHealthyLocations returns all locations with active or syncing status.
func (e *CatalogEntry) GetHealthyLocations() []StoreLocation {
	if e == nil {
		return nil
	}

	var healthy []StoreLocation

	if e.PrimaryStore.IsHealthy() {
		healthy = append(healthy, e.PrimaryStore)
	}

	for _, r := range e.Replicas {
		if r.IsHealthy() {
			healthy = append(healthy, r)
		}
	}

	if e.ArchiveStore != nil && e.ArchiveStore.IsHealthy() {
		healthy = append(healthy, *e.ArchiveStore)
	}

	return healthy
}

// GetActiveLocations returns all locations with active status.
func (e *CatalogEntry) GetActiveLocations() []StoreLocation {
	if e == nil {
		return nil
	}

	var active []StoreLocation

	if e.PrimaryStore.IsActive() {
		active = append(active, e.PrimaryStore)
	}

	for _, r := range e.Replicas {
		if r.IsActive() {
			active = append(active, r)
		}
	}

	if e.ArchiveStore != nil && e.ArchiveStore.IsActive() {
		active = append(active, *e.ArchiveStore)
	}

	return active
}

// ReplicaCount returns the number of replica locations.
func (e *CatalogEntry) ReplicaCount() int {
	if e == nil {
		return 0
	}
	return len(e.Replicas)
}

// TotalLocationCount returns the total number of locations (primary + replicas + archive).
func (e *CatalogEntry) TotalLocationCount() int {
	if e == nil {
		return 0
	}
	count := 1 + len(e.Replicas) // Primary + replicas
	if e.ArchiveStore != nil {
		count++
	}
	return count
}

// ChecksumHex returns the checksum as a hex string.
func (e *CatalogEntry) ChecksumHex() string {
	if e == nil || e.Checksum == nil {
		return ""
	}
	return hex.EncodeToString(e.Checksum)
}

// Validate validates the catalog entry.
func (e *CatalogEntry) Validate() error {
	if e == nil {
		return NewInvalidEntryError("entry", "entry is nil")
	}

	if e.Key == "" {
		return NewInvalidEntryError("key", "key is required")
	}

	if e.Size < 0 {
		return NewInvalidEntryError("size", "size cannot be negative")
	}

	if !e.Status.IsValid() {
		return NewInvalidEntryError("status", "invalid status")
	}

	// Validate primary store
	if err := e.PrimaryStore.Validate(); err != nil {
		return err
	}

	// Validate replicas
	for i, r := range e.Replicas {
		if err := r.Validate(); err != nil {
			return NewInvalidEntryError("replicas", err.Error())
		}
		// Check for duplicate stores
		if r.Store == e.PrimaryStore.Store {
			return NewInvalidEntryError("replicas", "replica cannot be on the same store as primary")
		}
		for j := i + 1; j < len(e.Replicas); j++ {
			if r.Store == e.Replicas[j].Store {
				return NewInvalidEntryError("replicas", "duplicate replica stores")
			}
		}
	}

	// Validate archive store
	if e.ArchiveStore != nil {
		if err := e.ArchiveStore.Validate(); err != nil {
			return err
		}
	}

	// Validate encryption
	if e.Encryption != nil {
		if err := e.Encryption.Validate(); err != nil {
			return err
		}
	}

	// Validate policies
	if e.ReplicationPolicy != nil {
		if err := e.ReplicationPolicy.Validate(); err != nil {
			return err
		}
	}

	if e.ArchivePolicy != nil {
		if err := e.ArchivePolicy.Validate(); err != nil {
			return err
		}
	}

	if e.RetentionPolicy != nil {
		if err := e.RetentionPolicy.Validate(); err != nil {
			return err
		}
	}

	return nil
}

// ShouldArchive checks if the entry should be archived based on its archive policy.
func (e *CatalogEntry) ShouldArchive() bool {
	if e == nil || e.ArchivePolicy == nil || e.Status != StatusActive {
		return false
	}
	return e.ArchivePolicy.ShouldArchive(e.LastAccessAt)
}

// ShouldDelete checks if the entry should be deleted based on its retention policy.
func (e *CatalogEntry) ShouldDelete() bool {
	if e == nil || e.RetentionPolicy == nil {
		return false
	}
	return e.RetentionPolicy.ShouldDelete(e.CreatedAt)
}

// NeedsMoreReplicas checks if the entry needs more replicas based on its replication policy.
func (e *CatalogEntry) NeedsMoreReplicas() bool {
	if e == nil || e.ReplicationPolicy == nil || !e.ReplicationPolicy.Enabled {
		return false
	}
	// Total copies = 1 (primary) + len(replicas)
	totalCopies := 1 + len(e.Replicas)
	return totalCopies < e.ReplicationPolicy.MinReplicas
}

// ListOptions defines pagination and filtering options for list operations.
type ListOptions struct {
	// Limit is the maximum number of results to return (0 = no limit).
	Limit int

	// Cursor is an opaque pagination cursor for continuing from a previous query.
	Cursor string

	// Offset is the number of results to skip (alternative to cursor).
	Offset int
}

// ListResult holds the results of a list operation.
type ListResult struct {
	// Keys contains the matching object keys.
	Keys []string

	// NextCursor is the cursor for the next page (empty if no more results).
	NextCursor string

	// HasMore indicates whether additional results are available.
	HasMore bool

	// Total is the approximate total count (may be -1 if unknown).
	Total int64
}
