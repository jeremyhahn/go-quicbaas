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
}

// NewMemoryBackend creates a new in-memory secrets backend.
func NewMemoryBackend() *MemoryBackend {
	return &MemoryBackend{
		secrets: make(map[string]*secretStore),
	}
}

// Store saves a secret at the specified path.
func (mb *MemoryBackend) Store(ctx context.Context, path string, data map[string][]byte, metadata map[string]string) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	if path == "" {
		return ErrInvalidPath
	}
	if len(data) == 0 {
		return ErrEmptyData
	}

	mb.mu.Lock()
	defer mb.mu.Unlock()

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
	newVersion := len(store.versions) + 1
	entry := &SecretEntry{
		Path:      path,
		Data:      dataCopy,
		Metadata:  metadataCopy,
		Version:   newVersion,
		Deleted:   false,
		CreatedAt: now,
		UpdatedAt: now,
	}

	store.versions = append(store.versions, entry)
	store.deleted = false

	return nil
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

	if version < 1 || version > len(store.versions) {
		return nil, nil, ErrVersionNotFound
	}

	entry := store.versions[version-1]
	if entry.Deleted {
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

	if version < 1 || version > len(store.versions) {
		return ErrVersionNotFound
	}

	oldEntry := store.versions[version-1]
	if oldEntry.Deleted {
		return ErrVersionDestroyed
	}

	// Create a new version with the old entry's data
	now := time.Now()
	newVersion := len(store.versions) + 1
	newEntry := &SecretEntry{
		Path:      path,
		Data:      copyData(oldEntry.Data),
		Metadata:  copyMetadata(oldEntry.Metadata),
		Version:   newVersion,
		Deleted:   false,
		CreatedAt: now,
		UpdatedAt: now,
	}

	store.versions = append(store.versions, newEntry)
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
