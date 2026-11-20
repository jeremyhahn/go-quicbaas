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

// Package secrets provides secret storage and retrieval functionality for QuicBaaS.
package secrets

import (
	"context"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/jeremyhahn/go-quicbaas/pkg/auth"
)

// MemoryBackend is an in-memory secrets backend for development and testing.
// It supports versioning, soft deletes, and rollback operations.
// Note: This backend does NOT persist data - use for testing only.
type MemoryBackend struct {
	secrets map[string]*secretStore
	mu      sync.RWMutex
}

// secretStore holds all versions of a secret.
type secretStore struct {
	versions []*SecretEntry
	deleted  bool
	// latest is the last version number issued in this lifetime. Pruning
	// drops versions from versions, so a version number is not a slice
	// position.
	latest int
}

// find returns the entry numbered version, or nil when it was dropped.
func (s *secretStore) find(version int) *SecretEntry {
	for _, e := range s.versions {
		if e.Version == version {
			return e
		}
	}
	return nil
}

// isMemoryTombstone reports whether e is a pruned version: destroyed and
// holding no payload.
func isMemoryTombstone(e *SecretEntry) bool {
	return e.Deleted && e.Data == nil && e.Metadata == nil
}

// NewMemoryBackend creates a new in-memory secrets backend.
func NewMemoryBackend() *MemoryBackend {
	return &MemoryBackend{
		secrets: make(map[string]*secretStore),
	}
}

// Store saves a secret at the specified path.
func (mb *MemoryBackend) Store(ctx context.Context, path string, data map[string][]byte, metadata map[string]string) error {
	_, err := mb.StoreVersion(ctx, path, data, metadata)
	return err
}

// StoreVersion saves a secret as Store does and returns the version it
// assigned.
func (mb *MemoryBackend) StoreVersion(ctx context.Context, path string, data map[string][]byte, metadata map[string]string) (int, error) {
	return mb.storeVersion(ctx, path, nil, data, metadata)
}

// StoreVersionIfCurrent stores a new version only while expected is the
// path's current, undeleted version; otherwise it stores nothing and returns
// ErrWriteConflict wrapping a *StaleVersionError.
func (mb *MemoryBackend) StoreVersionIfCurrent(ctx context.Context, path string, expected ReadVersion, data map[string][]byte, metadata map[string]string) (int, error) {
	if expected.Version <= 0 {
		return 0, staleReadError(expected, 0, false, false)
	}
	return mb.storeVersion(ctx, path, &expected, data, metadata)
}

// storeVersion is StoreVersion, conditional on the current version when
// expected is positive.
func (mb *MemoryBackend) storeVersion(ctx context.Context, path string, expected *ReadVersion, data map[string][]byte, metadata map[string]string) (int, error) {
	if err := ctx.Err(); err != nil {
		return 0, err
	}

	if path == "" {
		return 0, ErrInvalidPath
	}
	if len(data) == 0 {
		return 0, ErrEmptyData
	}

	mb.mu.Lock()
	defer mb.mu.Unlock()

	if expected != nil {
		current, deleted := 0, false
		s, ok := mb.secrets[path]
		if ok {
			current, deleted = s.latest, s.deleted
		}
		if current != expected.Version || deleted {
			return 0, staleReadError(*expected, current, deleted, false)
		}
		// Destroy restarts the count: the record at the number must be the
		// one read, not a recreation's.
		if last := s.versions[len(s.versions)-1]; last.Deleted || !expected.holds(last.Data, last.Metadata) {
			return 0, staleReadError(*expected, current, false, true)
		}
	}

	// Create deep copy of data
	dataCopy := make(map[string][]byte, len(data))
	for k, v := range data {
		vCopy := make([]byte, len(v))
		copy(vCopy, v)
		dataCopy[k] = vCopy
	}

	// Create metadata copy
	var metadataCopy map[string]string
	if metadata != nil {
		metadataCopy = make(map[string]string, len(metadata))
		for k, v := range metadata {
			metadataCopy[k] = v
		}
	}

	now := time.Now()
	store, exists := mb.secrets[path]
	if !exists {
		store = &secretStore{
			versions: make([]*SecretEntry, 0, 1),
		}
		mb.secrets[path] = store
	}

	// Create new version
	newVersion := store.latest + 1
	entry := &SecretEntry{
		Path:      path,
		Data:      dataCopy,
		Metadata:  metadataCopy,
		Version:   newVersion,
		Deleted:   false,
		CreatedAt: now,
		UpdatedAt: now,
		CreatedBy: auth.PrincipalFromContext(ctx),
	}

	store.versions = append(store.versions, entry)
	store.latest = newVersion
	store.deleted = false

	return newVersion, nil
}

// PruneVersions destroys every version of path older than its newest keep
// live versions. The earliest version is kept as a tombstone (data and
// metadata dropped, CreatedAt kept) so the secret's creation time survives;
// every other pruned version is removed, so the history stays at keep live
// versions plus that tombstone. The store tracks the latest version number
// separately, so numbering continues past the removed versions.
func (mb *MemoryBackend) PruneVersions(ctx context.Context, path string, keep int) (int, error) {
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	if path == "" {
		return 0, ErrInvalidPath
	}
	if keep < 1 {
		return 0, ErrVersionMustBePositive
	}

	mb.mu.Lock()
	defer mb.mu.Unlock()

	store, exists := mb.secrets[path]
	if !exists || len(store.versions) == 0 {
		return 0, ErrSecretNotFound
	}
	// The newest keep live entries are retained; a tombstone never counts
	// toward keep. cut is the index of the oldest retained entry.
	all := store.versions
	cut, live := -1, 0
	for i := len(all) - 1; i >= 0; i-- {
		if isMemoryTombstone(all[i]) {
			continue
		}
		live++
		if live == keep {
			cut = i
			break
		}
	}
	if cut <= 0 {
		return 0, nil
	}
	// The earliest entry becomes the creation tombstone; every other entry
	// below the retained set is dropped. An existing tombstone never counts.
	pruned := 0
	now := time.Now()
	first := all[0]
	if !isMemoryTombstone(first) {
		first.Deleted = true
		first.Data = nil
		first.Metadata = nil
		first.UpdatedAt = now
		if first.DeletedAt == nil {
			deletedAt := now
			first.DeletedAt = &deletedAt
		}
		pruned++
	}
	for _, entry := range all[1:cut] {
		if !isMemoryTombstone(entry) {
			pruned++
		}
	}
	kept := make([]*SecretEntry, 0, 1+len(all)-cut)
	kept = append(kept, first)
	store.versions = append(kept, all[cut:]...)
	return pruned, nil
}

// Get retrieves a secret from the specified path (current version).
func (mb *MemoryBackend) Get(ctx context.Context, path string) (map[string][]byte, map[string]string, error) {
	if err := ctx.Err(); err != nil {
		return nil, nil, err
	}

	if path == "" {
		return nil, nil, ErrInvalidPath
	}

	mb.mu.RLock()
	defer mb.mu.RUnlock()

	store, exists := mb.secrets[path]
	if !exists || len(store.versions) == 0 {
		return nil, nil, ErrSecretNotFound
	}

	if store.deleted {
		return nil, nil, ErrSecretDeleted
	}

	// Get the latest version
	entry := store.versions[len(store.versions)-1]
	if entry.Deleted {
		return nil, nil, ErrSecretDeleted
	}

	// Return deep copies, including version in metadata
	metadata := copyMetadata(entry.Metadata)
	if metadata == nil {
		metadata = make(map[string]string)
	}
	metadata["version"] = strconv.Itoa(entry.Version)
	return copyData(entry.Data), metadata, nil
}

// GetVersion retrieves a specific version of a secret.
func (mb *MemoryBackend) GetVersion(ctx context.Context, path string, version int) (map[string][]byte, map[string]string, error) {
	if err := ctx.Err(); err != nil {
		return nil, nil, err
	}

	if path == "" {
		return nil, nil, ErrInvalidPath
	}

	mb.mu.RLock()
	defer mb.mu.RUnlock()

	store, exists := mb.secrets[path]
	if !exists || len(store.versions) == 0 {
		return nil, nil, ErrSecretNotFound
	}

	if version < 1 || version > store.latest {
		return nil, nil, ErrVersionNotFound
	}

	entry := store.find(version)
	if entry == nil || entry.Deleted {
		return nil, nil, ErrVersionDestroyed
	}

	// Return deep copies, including version in metadata
	metadata := copyMetadata(entry.Metadata)
	if metadata == nil {
		metadata = make(map[string]string)
	}
	metadata["version"] = strconv.Itoa(entry.Version)
	return copyData(entry.Data), metadata, nil
}

// List returns all secret paths matching the prefix.
func (mb *MemoryBackend) List(ctx context.Context, prefix string) ([]string, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	mb.mu.RLock()
	defer mb.mu.RUnlock()

	var paths []string
	for path, store := range mb.secrets {
		if !store.deleted && (prefix == "" || strings.HasPrefix(path, prefix)) {
			paths = append(paths, path)
		}
	}

	return paths, nil
}

// Delete removes a secret (soft delete, keeps versions).
func (mb *MemoryBackend) Delete(ctx context.Context, path string) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	if path == "" {
		return ErrInvalidPath
	}

	mb.mu.Lock()
	defer mb.mu.Unlock()

	store, exists := mb.secrets[path]
	if !exists || len(store.versions) == 0 {
		return ErrSecretNotFound
	}

	if store.deleted {
		return ErrSecretDeleted
	}

	now := time.Now()
	store.deleted = true

	// Mark latest version as deleted
	latest := store.versions[len(store.versions)-1]
	latest.Deleted = true
	latest.DeletedAt = &now

	return nil
}

// Destroy permanently removes all versions of a secret.
func (mb *MemoryBackend) Destroy(ctx context.Context, path string) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	if path == "" {
		return ErrInvalidPath
	}

	mb.mu.Lock()
	defer mb.mu.Unlock()

	_, exists := mb.secrets[path]
	if !exists {
		return ErrSecretNotFound
	}

	delete(mb.secrets, path)
	return nil
}

// ListVersions returns metadata for all versions of a secret.
func (mb *MemoryBackend) ListVersions(ctx context.Context, path string) ([]*VersionMetadata, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	if path == "" {
		return nil, ErrInvalidPath
	}

	mb.mu.RLock()
	defer mb.mu.RUnlock()

	store, exists := mb.secrets[path]
	if !exists || len(store.versions) == 0 {
		return nil, ErrSecretNotFound
	}

	versions := make([]*VersionMetadata, len(store.versions))
	for i, entry := range store.versions {
		versions[i] = &VersionMetadata{
			Version:   entry.Version,
			CreatedAt: entry.CreatedAt,
			CreatedBy: entry.CreatedBy,
			Deleted:   entry.Deleted,
			DeletedAt: entry.DeletedAt,
		}
	}

	return versions, nil
}

// Rollback restores a previous version as the current version.
func (mb *MemoryBackend) Rollback(ctx context.Context, path string, version int) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	if path == "" {
		return ErrInvalidPath
	}

	mb.mu.Lock()
	defer mb.mu.Unlock()

	store, exists := mb.secrets[path]
	if !exists || len(store.versions) == 0 {
		return ErrSecretNotFound
	}

	if version < 1 || version > store.latest {
		return ErrVersionNotFound
	}

	oldEntry := store.find(version)
	if oldEntry == nil || oldEntry.Deleted {
		return ErrVersionDestroyed
	}

	// Create a new version with the old entry's data
	now := time.Now()
	newVersion := store.latest + 1
	newEntry := &SecretEntry{
		Path:      path,
		Data:      copyData(oldEntry.Data),
		Metadata:  copyMetadata(oldEntry.Metadata),
		Version:   newVersion,
		Deleted:   false,
		CreatedAt: now,
		UpdatedAt: now,
		CreatedBy: auth.PrincipalFromContext(ctx),
	}

	store.versions = append(store.versions, newEntry)
	store.latest = newVersion
	store.deleted = false

	return nil
}

// Type returns the backend type identifier.
func (mb *MemoryBackend) Type() string {
	return "memory"
}

// Close releases resources (no-op for memory backend).
func (mb *MemoryBackend) Close() error {
	mb.mu.Lock()
	defer mb.mu.Unlock()

	// Clear all secrets
	mb.secrets = make(map[string]*secretStore)
	return nil
}

// Helper functions

func copyData(data map[string][]byte) map[string][]byte {
	if data == nil {
		return nil
	}
	result := make(map[string][]byte, len(data))
	for k, v := range data {
		vCopy := make([]byte, len(v))
		copy(vCopy, v)
		result[k] = vCopy
	}
	return result
}

func copyMetadata(metadata map[string]string) map[string]string {
	if metadata == nil {
		return nil
	}
	result := make(map[string]string, len(metadata))
	for k, v := range metadata {
		result[k] = v
	}
	return result
}
