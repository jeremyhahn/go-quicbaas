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
	"time"
)

// LocationStatus represents the state of a storage location.
type LocationStatus string

const (
	// LocationStatusActive indicates the location is healthy and accessible.
	LocationStatusActive LocationStatus = "active"

	// LocationStatusSyncing indicates the location is being synchronized.
	LocationStatusSyncing LocationStatus = "syncing"

	// LocationStatusFailed indicates the location has failed and is not accessible.
	LocationStatusFailed LocationStatus = "failed"

	// LocationStatusStale indicates the location data may be out of date.
	LocationStatusStale LocationStatus = "stale"
)

// String returns the string representation of the location status.
func (s LocationStatus) String() string {
	return string(s)
}

// IsValid checks if the location status is valid.
func (s LocationStatus) IsValid() bool {
	switch s {
	case LocationStatusActive, LocationStatusSyncing, LocationStatusFailed, LocationStatusStale:
		return true
	default:
		return false
	}
}

// StoreLocation represents where an object is stored within a backend.
type StoreLocation struct {
	// Store is the backend name (e.g., "s3-primary", "gcs-west").
	Store string `msgpack:"store"`

	// ObjectRef is the path/key within the backend.
	ObjectRef string `msgpack:"object_ref"`

	// Status is the current state of this location.
	Status LocationStatus `msgpack:"status"`

	// Checksum is the per-location checksum for integrity verification.
	Checksum []byte `msgpack:"checksum,omitempty"`

	// StoredSize is the actual stored size (may differ if compressed).
	StoredSize int64 `msgpack:"stored_size"`

	// Compressed indicates whether the stored data is compressed.
	Compressed bool `msgpack:"compressed"`

	// Encryption contains encryption details for this location.
	Encryption *LocationEncryption `msgpack:"encryption,omitempty"`

	// SyncedAt is the Unix nanosecond timestamp of the last successful sync.
	SyncedAt int64 `msgpack:"synced_at"`
}

// LocationEncryption describes encryption for a specific storage location.
type LocationEncryption struct {
	// KeyID is the KMS key path used for encryption.
	KeyID string `msgpack:"key_id"`

	// Algorithm is the encryption algorithm (e.g., "AES-256-GCM").
	Algorithm string `msgpack:"algorithm"`

	// Nonce is the encryption nonce/IV.
	Nonce []byte `msgpack:"nonce,omitempty"`

	// WrappedKey is the wrapped (encrypted) DEK used for envelope encryption.
	// The DEK is encrypted by the KEK identified by KeyID.
	WrappedKey []byte `msgpack:"wrapped_key,omitempty"`
}

// ObjectEncryption describes encryption for the object itself.
type ObjectEncryption struct {
	// Enabled indicates whether encryption is active for this object.
	Enabled bool `msgpack:"enabled"`

	// KeyID is the KMS key path used for encryption.
	KeyID string `msgpack:"key_id"`

	// Algorithm is the encryption algorithm (e.g., "AES-256-GCM").
	Algorithm string `msgpack:"algorithm"`
}

// ObjectACL describes access control for an object.
type ObjectACL struct {
	// Owner is the user/service ID that owns the object.
	Owner string `msgpack:"owner"`

	// Readers is a list of users/groups with read access.
	Readers []string `msgpack:"readers,omitempty"`

	// Writers is a list of users/groups with write access.
	Writers []string `msgpack:"writers,omitempty"`

	// Admins is a list of users/groups with admin access.
	Admins []string `msgpack:"admins,omitempty"`

	// PublicRead allows unauthenticated reads when true.
	PublicRead bool `msgpack:"public_read"`

	// Inherit indicates whether to inherit from parent/tenant ACL.
	Inherit bool `msgpack:"inherit"`
}

// NewStoreLocation creates a new store location with default values.
func NewStoreLocation(store, objectRef string) *StoreLocation {
	return &StoreLocation{
		Store:     store,
		ObjectRef: objectRef,
		Status:    LocationStatusActive,
		SyncedAt:  time.Now().UnixNano(),
	}
}

// IsHealthy returns true if the location is active or syncing.
func (l *StoreLocation) IsHealthy() bool {
	if l == nil {
		return false
	}
	return l.Status == LocationStatusActive || l.Status == LocationStatusSyncing
}

// IsActive returns true if the location is active.
func (l *StoreLocation) IsActive() bool {
	if l == nil {
		return false
	}
	return l.Status == LocationStatusActive
}

// IsFailed returns true if the location has failed.
func (l *StoreLocation) IsFailed() bool {
	if l == nil {
		return false
	}
	return l.Status == LocationStatusFailed
}

// MarkActive marks the location as active and updates the sync time.
func (l *StoreLocation) MarkActive() {
	if l == nil {
		return
	}
	l.Status = LocationStatusActive
	l.SyncedAt = time.Now().UnixNano()
}

// MarkSyncing marks the location as syncing.
func (l *StoreLocation) MarkSyncing() {
	if l == nil {
		return
	}
	l.Status = LocationStatusSyncing
}

// MarkFailed marks the location as failed.
func (l *StoreLocation) MarkFailed() {
	if l == nil {
		return
	}
	l.Status = LocationStatusFailed
}

// MarkStale marks the location as stale.
func (l *StoreLocation) MarkStale() {
	if l == nil {
		return
	}
	l.Status = LocationStatusStale
}

// Clone creates a deep copy of the store location.
func (l *StoreLocation) Clone() *StoreLocation {
	if l == nil {
		return nil
	}

	clone := &StoreLocation{
		Store:      l.Store,
		ObjectRef:  l.ObjectRef,
		Status:     l.Status,
		StoredSize: l.StoredSize,
		Compressed: l.Compressed,
		SyncedAt:   l.SyncedAt,
	}

	if l.Checksum != nil {
		clone.Checksum = make([]byte, len(l.Checksum))
		copy(clone.Checksum, l.Checksum)
	}

	if l.Encryption != nil {
		clone.Encryption = l.Encryption.Clone()
	}

	return clone
}

// Validate validates the store location.
func (l *StoreLocation) Validate() error {
	if l == nil {
		return NewInvalidLocationError("", "location is nil")
	}

	if l.Store == "" {
		return NewInvalidLocationError("", "store name is required")
	}

	if l.ObjectRef == "" {
		return NewInvalidLocationError(l.Store, "object reference is required")
	}

	if !l.Status.IsValid() {
		return NewInvalidLocationError(l.Store, "invalid status")
	}

	if l.StoredSize < 0 {
		return NewInvalidLocationError(l.Store, "stored size cannot be negative")
	}

	return nil
}

// Clone creates a deep copy of the location encryption.
func (e *LocationEncryption) Clone() *LocationEncryption {
	if e == nil {
		return nil
	}

	clone := &LocationEncryption{
		KeyID:     e.KeyID,
		Algorithm: e.Algorithm,
	}

	if e.Nonce != nil {
		clone.Nonce = make([]byte, len(e.Nonce))
		copy(clone.Nonce, e.Nonce)
	}

	if e.WrappedKey != nil {
		clone.WrappedKey = make([]byte, len(e.WrappedKey))
		copy(clone.WrappedKey, e.WrappedKey)
	}

	return clone
}

// Validate validates the location encryption.
func (e *LocationEncryption) Validate() error {
	if e == nil {
		return nil
	}

	if e.KeyID == "" {
		return NewInvalidLocationError("", "encryption key ID is required")
	}

	if e.Algorithm == "" {
		return NewInvalidLocationError("", "encryption algorithm is required")
	}

	return nil
}

// Clone creates a deep copy of the object encryption.
func (e *ObjectEncryption) Clone() *ObjectEncryption {
	if e == nil {
		return nil
	}

	return &ObjectEncryption{
		Enabled:   e.Enabled,
		KeyID:     e.KeyID,
		Algorithm: e.Algorithm,
	}
}

// Validate validates the object encryption.
func (e *ObjectEncryption) Validate() error {
	if e == nil {
		return nil
	}

	if !e.Enabled {
		return nil
	}

	if e.KeyID == "" {
		return NewInvalidEntryError("encryption.key_id", "encryption key ID is required when encryption is enabled")
	}

	if e.Algorithm == "" {
		return NewInvalidEntryError("encryption.algorithm", "encryption algorithm is required when encryption is enabled")
	}

	return nil
}

// Clone creates a deep copy of the object ACL.
func (a *ObjectACL) Clone() *ObjectACL {
	if a == nil {
		return nil
	}

	clone := &ObjectACL{
		Owner:      a.Owner,
		PublicRead: a.PublicRead,
		Inherit:    a.Inherit,
	}

	if a.Readers != nil {
		clone.Readers = make([]string, len(a.Readers))
		copy(clone.Readers, a.Readers)
	}

	if a.Writers != nil {
		clone.Writers = make([]string, len(a.Writers))
		copy(clone.Writers, a.Writers)
	}

	if a.Admins != nil {
		clone.Admins = make([]string, len(a.Admins))
		copy(clone.Admins, a.Admins)
	}

	return clone
}

// HasReadAccess checks if a user/group has read access.
func (a *ObjectACL) HasReadAccess(identity string) bool {
	if a == nil {
		return false
	}

	if a.PublicRead {
		return true
	}

	if a.Owner == identity {
		return true
	}

	for _, reader := range a.Readers {
		if reader == identity {
			return true
		}
	}

	for _, writer := range a.Writers {
		if writer == identity {
			return true
		}
	}

	for _, admin := range a.Admins {
		if admin == identity {
			return true
		}
	}

	return false
}

// HasWriteAccess checks if a user/group has write access.
func (a *ObjectACL) HasWriteAccess(identity string) bool {
	if a == nil {
		return false
	}

	if a.Owner == identity {
		return true
	}

	for _, writer := range a.Writers {
		if writer == identity {
			return true
		}
	}

	for _, admin := range a.Admins {
		if admin == identity {
			return true
		}
	}

	return false
}

// HasAdminAccess checks if a user/group has admin access.
func (a *ObjectACL) HasAdminAccess(identity string) bool {
	if a == nil {
		return false
	}

	if a.Owner == identity {
		return true
	}

	for _, admin := range a.Admins {
		if admin == identity {
			return true
		}
	}

	return false
}

// AddReader adds a reader to the ACL.
func (a *ObjectACL) AddReader(identity string) {
	if a == nil {
		return
	}
	for _, r := range a.Readers {
		if r == identity {
			return
		}
	}
	a.Readers = append(a.Readers, identity)
}

// AddWriter adds a writer to the ACL.
func (a *ObjectACL) AddWriter(identity string) {
	if a == nil {
		return
	}
	for _, w := range a.Writers {
		if w == identity {
			return
		}
	}
	a.Writers = append(a.Writers, identity)
}

// AddAdmin adds an admin to the ACL.
func (a *ObjectACL) AddAdmin(identity string) {
	if a == nil {
		return
	}
	for _, admin := range a.Admins {
		if admin == identity {
			return
		}
	}
	a.Admins = append(a.Admins, identity)
}

// RemoveReader removes a reader from the ACL.
func (a *ObjectACL) RemoveReader(identity string) {
	if a == nil {
		return
	}
	for i, r := range a.Readers {
		if r == identity {
			a.Readers = append(a.Readers[:i], a.Readers[i+1:]...)
			return
		}
	}
}

// RemoveWriter removes a writer from the ACL.
func (a *ObjectACL) RemoveWriter(identity string) {
	if a == nil {
		return
	}
	for i, w := range a.Writers {
		if w == identity {
			a.Writers = append(a.Writers[:i], a.Writers[i+1:]...)
			return
		}
	}
}

// RemoveAdmin removes an admin from the ACL.
func (a *ObjectACL) RemoveAdmin(identity string) {
	if a == nil {
		return
	}
	for i, admin := range a.Admins {
		if admin == identity {
			a.Admins = append(a.Admins[:i], a.Admins[i+1:]...)
			return
		}
	}
}
