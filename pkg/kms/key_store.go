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

// Package kms provides Key Management System functionality for QuicBaaS.
package kms

import (
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/jeremyhahn/go-quicbaas/pkg/errors"
)

// KeyStore provides thread-safe storage for key metadata.
// It's used by the KMS Manager to cache key metadata and track key state.
// All operations are concurrency-safe using a read-write mutex.
type KeyStore struct {
	mu sync.RWMutex

	// keys maps key IDs to their metadata
	keys map[string]*keyEntry

	// versionCounter tracks version numbers per key
	versionCounter map[string]*atomic.Int64

	// usageCounter tracks usage counts per key
	usageCounter map[string]*atomic.Uint64
}

// keyEntry wraps metadata with internal tracking fields.
type keyEntry struct {
	metadata *KeyMetadata
}

// NewKeyStore creates a new KeyStore instance.
func NewKeyStore() *KeyStore {
	return &KeyStore{
		keys:           make(map[string]*keyEntry),
		versionCounter: make(map[string]*atomic.Int64),
		usageCounter:   make(map[string]*atomic.Uint64),
	}
}

// Put stores or updates key metadata in the store.
// The metadata is deep-copied to prevent external mutation.
func (s *KeyStore) Put(metadata *KeyMetadata) error {
	if metadata == nil {
		return &errors.QBError{
			Code: errors.ErrInvalidInput,
			Op:   "KeyStore.Put",
			Err:  ErrMetadataNil,
		}
	}

	if metadata.KeyID == "" {
		return &errors.QBError{
			Code: errors.ErrInvalidInput,
			Op:   "KeyStore.Put",
			Err:  ErrKeyIDEmpty,
		}
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	// Deep copy metadata to prevent external mutation
	copied := s.copyMetadata(metadata)

	// Store the entry
	s.keys[metadata.KeyID] = &keyEntry{
		metadata: copied,
	}

	// Initialize version counter if not exists
	if _, exists := s.versionCounter[metadata.KeyID]; !exists {
		counter := &atomic.Int64{}
		//nolint:gosec // G115: version is bounded, safe conversion
		counter.Store(int64(metadata.Version))
		s.versionCounter[metadata.KeyID] = counter
	}

	// Initialize usage counter if not exists
	if _, exists := s.usageCounter[metadata.KeyID]; !exists {
		s.usageCounter[metadata.KeyID] = &atomic.Uint64{}
	}

	return nil
}

// Get retrieves key metadata by key ID.
// Returns a deep copy to prevent external mutation.
func (s *KeyStore) Get(keyID string) (*KeyMetadata, error) {
	if keyID == "" {
		return nil, &errors.QBError{
			Code: errors.ErrInvalidInput,
			Op:   "KeyStore.Get",
			Err:  ErrKeyIDEmpty,
		}
	}

	s.mu.RLock()
	defer s.mu.RUnlock()

	entry, exists := s.keys[keyID]
	if !exists {
		return nil, &errors.QBError{
			Code: errors.ErrNotFound,
			Op:   "KeyStore.Get",
			Err:  ErrKeyNotFound,
		}
	}

	// Return a deep copy
	return s.copyMetadata(entry.metadata), nil
}

// Delete removes a key from the store.
func (s *KeyStore) Delete(keyID string) error {
	if keyID == "" {
		return &errors.QBError{
			Code: errors.ErrInvalidInput,
			Op:   "KeyStore.Delete",
			Err:  ErrKeyIDEmpty,
		}
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if _, exists := s.keys[keyID]; !exists {
		return &errors.QBError{
			Code: errors.ErrNotFound,
			Op:   "KeyStore.Delete",
			Err:  ErrKeyNotFound,
		}
	}

	delete(s.keys, keyID)
	delete(s.versionCounter, keyID)
	delete(s.usageCounter, keyID)

	return nil
}

// List returns keys with optional prefix filtering and limit.
// If prefix is empty, all keys are returned.
// If limit is <= 0, no limit is applied.
func (s *KeyStore) List(prefix string, limit int) []*KeyMetadata {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var results []*KeyMetadata

	// Collect matching keys
	for keyID, entry := range s.keys {
		if prefix == "" || strings.HasPrefix(keyID, prefix) {
			results = append(results, s.copyMetadata(entry.metadata))
		}
	}

	// Sort by KeyID for consistent ordering
	sort.Slice(results, func(i, j int) bool {
		return results[i].KeyID < results[j].KeyID
	})

	// Apply limit if specified
	if limit > 0 && len(results) > limit {
		results = results[:limit]
	}

	return results
}

// GetByState returns all keys with the specified state.
func (s *KeyStore) GetByState(state KeyState) []*KeyMetadata {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var results []*KeyMetadata

	for _, entry := range s.keys {
		if entry.metadata.State == state {
			results = append(results, s.copyMetadata(entry.metadata))
		}
	}

	// Sort by KeyID for consistent ordering
	sort.Slice(results, func(i, j int) bool {
		return results[i].KeyID < results[j].KeyID
	})

	return results
}

// GetExpiring returns keys that will expire within the specified duration.
// Keys without rotation policy or NextRotation are excluded.
func (s *KeyStore) GetExpiring(within time.Duration) []*KeyMetadata {
	s.mu.RLock()
	defer s.mu.RUnlock()

	now := time.Now()
	threshold := now.Add(within)

	var results []*KeyMetadata

	for _, entry := range s.keys {
		if entry.metadata.RotationPolicy != nil &&
			!entry.metadata.RotationPolicy.NextRotation.IsZero() &&
			entry.metadata.RotationPolicy.NextRotation.Before(threshold) {
			results = append(results, s.copyMetadata(entry.metadata))
		}
	}

	// Sort by NextRotation time
	sort.Slice(results, func(i, j int) bool {
		return results[i].RotationPolicy.NextRotation.Before(results[j].RotationPolicy.NextRotation)
	})

	return results
}

// GetPendingDeletion returns all keys marked for deletion.
func (s *KeyStore) GetPendingDeletion() []*KeyMetadata {
	return s.GetByState(KeyStatePendingDeletion)
}

// IncrementVersion atomically increments the version counter for a key.
// Returns the new version number.
func (s *KeyStore) IncrementVersion(keyID string) (int, error) {
	if keyID == "" {
		return 0, &errors.QBError{
			Code: errors.ErrInvalidInput,
			Op:   "KeyStore.IncrementVersion",
			Err:  ErrKeyIDEmpty,
		}
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if _, exists := s.keys[keyID]; !exists {
		return 0, &errors.QBError{
			Code: errors.ErrNotFound,
			Op:   "KeyStore.IncrementVersion",
			Err:  ErrKeyNotFound,
		}
	}

	counter, exists := s.versionCounter[keyID]
	if !exists {
		counter = &atomic.Int64{}
		counter.Store(int64(s.keys[keyID].metadata.Version)) //nolint:gosec // G115: version counter conversion
		s.versionCounter[keyID] = counter
	}

	newVersion := counter.Add(1)

	// Update metadata version
	s.keys[keyID].metadata.Version = int(newVersion) //nolint:gosec // G115: int64 to int conversion for version number

	return int(newVersion), nil
}

// GetVersion returns the current version number for a key.
func (s *KeyStore) GetVersion(keyID string) (int, error) {
	if keyID == "" {
		return 0, &errors.QBError{
			Code: errors.ErrInvalidInput,
			Op:   "KeyStore.GetVersion",
			Err:  ErrKeyIDEmpty,
		}
	}

	s.mu.RLock()
	defer s.mu.RUnlock()

	entry, exists := s.keys[keyID]
	if !exists {
		return 0, &errors.QBError{
			Code: errors.ErrNotFound,
			Op:   "KeyStore.GetVersion",
			Err:  ErrKeyNotFound,
		}
	}

	return int(entry.metadata.Version), nil //nolint:gosec // G115: uint64 to int conversion for version number
}

// IncrementUsage atomically increments the usage counter for a key.
func (s *KeyStore) IncrementUsage(keyID string) error {
	if keyID == "" {
		return &errors.QBError{
			Code: errors.ErrInvalidInput,
			Op:   "KeyStore.IncrementUsage",
			Err:  ErrKeyIDEmpty,
		}
	}

	s.mu.RLock()
	counter, exists := s.usageCounter[keyID]
	keyExists := s.keys[keyID] != nil
	s.mu.RUnlock()

	if !keyExists {
		return &errors.QBError{
			Code: errors.ErrNotFound,
			Op:   "KeyStore.IncrementUsage",
			Err:  ErrKeyNotFound,
		}
	}

	if !exists {
		// Initialize counter if it doesn't exist (shouldn't happen if Put was called)
		s.mu.Lock()
		counter = &atomic.Uint64{}
		s.usageCounter[keyID] = counter
		s.mu.Unlock()
	}

	counter.Add(1)
	return nil
}

// GetUsage returns the current usage count for a key.
func (s *KeyStore) GetUsage(keyID string) (uint64, error) {
	if keyID == "" {
		return 0, &errors.QBError{
			Code: errors.ErrInvalidInput,
			Op:   "KeyStore.GetUsage",
			Err:  ErrKeyIDEmpty,
		}
	}

	s.mu.RLock()
	defer s.mu.RUnlock()

	if _, exists := s.keys[keyID]; !exists {
		return 0, &errors.QBError{
			Code: errors.ErrNotFound,
			Op:   "KeyStore.GetUsage",
			Err:  ErrKeyNotFound,
		}
	}

	counter, exists := s.usageCounter[keyID]
	if !exists {
		return 0, nil
	}

	return counter.Load(), nil
}

// SetState updates the state of a key.
func (s *KeyStore) SetState(keyID string, state KeyState) error {
	if keyID == "" {
		return &errors.QBError{
			Code: errors.ErrInvalidInput,
			Op:   "KeyStore.SetState",
			Err:  ErrKeyIDEmpty,
		}
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	entry, exists := s.keys[keyID]
	if !exists {
		return &errors.QBError{
			Code: errors.ErrNotFound,
			Op:   "KeyStore.SetState",
			Err:  ErrKeyNotFound,
		}
	}

	entry.metadata.State = state
	entry.metadata.Updated = time.Now()

	return nil
}

// GetState returns the current state of a key.
func (s *KeyStore) GetState(keyID string) (KeyState, error) {
	if keyID == "" {
		return "", &errors.QBError{
			Code: errors.ErrInvalidInput,
			Op:   "KeyStore.GetState",
			Err:  ErrKeyIDEmpty,
		}
	}

	s.mu.RLock()
	defer s.mu.RUnlock()

	entry, exists := s.keys[keyID]
	if !exists {
		return "", &errors.QBError{
			Code: errors.ErrNotFound,
			Op:   "KeyStore.GetState",
			Err:  ErrKeyNotFound,
		}
	}

	return entry.metadata.State, nil
}

// copyMetadata creates a deep copy of KeyMetadata to prevent external mutation.
func (s *KeyStore) copyMetadata(src *KeyMetadata) *KeyMetadata {
	if src == nil {
		return nil
	}

	dst := &KeyMetadata{
		KeyID:              src.KeyID,
		Name:               src.Name,
		Description:        src.Description,
		Algorithm:          src.Algorithm,
		State:              src.State,
		Version:            src.Version,
		Exportable:         src.Exportable,
		DeletionProtection: src.DeletionProtection,
		Created:            src.Created,
		Updated:            src.Updated,
		UsageCount:         src.UsageCount,
		Owner:              src.Owner,
	}

	// Copy Usage slice
	if src.Usage != nil {
		dst.Usage = make([]KeyUsage, len(src.Usage))
		copy(dst.Usage, src.Usage)
	}

	// Copy LastUsedAt pointer
	if src.LastUsedAt != nil {
		t := *src.LastUsedAt
		dst.LastUsedAt = &t
	}

	// Copy RotationPolicy
	if src.RotationPolicy != nil {
		dst.RotationPolicy = &RotationPolicy{
			Enabled:        src.RotationPolicy.Enabled,
			Interval:       src.RotationPolicy.Interval,
			NextRotation:   src.RotationPolicy.NextRotation,
			RetainVersions: src.RotationPolicy.RetainVersions,
			AutoRotate:     src.RotationPolicy.AutoRotate,
		}
	}

	// Copy Tags map
	if src.Tags != nil {
		dst.Tags = make(map[string]string, len(src.Tags))
		for k, v := range src.Tags {
			dst.Tags[k] = v
		}
	}

	return dst
}
