// Copyright (c) 2026 Jeremy Hahn
// Copyright (c) 2026 Automate The Things, LLC
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

package secrets

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"
)

// kvEntry is the on-disk representation of a single secret version.
type kvEntry struct {
	Path      string            `json:"path"`
	Data      map[string][]byte `json:"data"`
	Metadata  map[string]string `json:"metadata,omitempty"`
	Version   int               `json:"version"`
	Deleted   bool              `json:"deleted"`
	Destroyed bool              `json:"destroyed"`
	CreatedAt time.Time         `json:"created_at"`
	UpdatedAt time.Time         `json:"updated_at"`
	DeletedAt *time.Time        `json:"deleted_at,omitempty"`
}

// kvCurrentEntry tracks the current version and soft-delete state of a path.
type kvCurrentEntry struct {
	Version   int        `json:"version"`
	Deleted   bool       `json:"deleted"`
	DeletedAt *time.Time `json:"deleted_at,omitempty"`
}

// KVStoreWriter is the subset of the qrdb KVStore interface needed by KVBackend.
// This avoids importing the kvstore package directly from the secrets package.
type KVStoreWriter interface {
	Put(ctx context.Context, key string, value []byte) error
	Get(ctx context.Context, key string) ([]byte, error)
	Delete(ctx context.Context, key string) error
	Scan(ctx context.Context, prefix string, fn func(key string, value []byte) error) error
	List(ctx context.Context, prefix string) ([]string, error)
}

const (
	// kvBackendKeyPrefix is the root prefix for all secrets in the KV store.
	kvBackendKeyPrefix = "_qb/secrets/"

	// kvBackendType is the type identifier for this backend.
	kvBackendType = "internal"
)

// KVBackend implements secrets.Backend over a qrdb KVStore. Secrets are
// stored as versioned JSON entries so that version history, soft-delete,
// and rollback all work without an external database. The backend is
// safe for concurrent use.
//
// Key layout:
//
//	_qb/secrets/<path>/current   → kvCurrentEntry (current version pointer)
//	_qb/secrets/<path>/v/<N>     → kvEntry (version N data)
type KVBackend struct {
	store KVStoreWriter
	mu    sync.Mutex // serialises writes to the same path
}

// NewKVBackend creates a KVBackend backed by the given KVStoreWriter.
func NewKVBackend(store KVStoreWriter) *KVBackend {
	return &KVBackend{store: store}
}

// Store saves a secret at the specified path, creating a new version.
func (b *KVBackend) Store(ctx context.Context, path string, data map[string][]byte, metadata map[string]string) error {
	if path == "" {
		return ErrInvalidPath
	}
	if len(data) == 0 {
		return ErrEmptyData
	}

	b.mu.Lock()
	defer b.mu.Unlock()

	cur, err := b.loadCurrent(ctx, path)
	if err != nil {
		return err
	}

	now := time.Now()
	nextVer := cur.Version + 1

	// Write the versioned entry.
	entry := &kvEntry{
		Path:      path,
		Data:      data,
		Metadata:  metadata,
		Version:   nextVer,
		CreatedAt: now,
		UpdatedAt: now,
	}
	if err := b.writeEntry(ctx, path, nextVer, entry); err != nil {
		return err
	}

	// Update the current pointer.
	cur.Version = nextVer
	cur.Deleted = false
	cur.DeletedAt = nil
	return b.writeCurrent(ctx, path, cur)
}

// Get retrieves the current version of a secret.
func (b *KVBackend) Get(ctx context.Context, path string) (map[string][]byte, map[string]string, error) {
	if path == "" {
		return nil, nil, ErrInvalidPath
	}
	cur, err := b.loadCurrent(ctx, path)
	if err != nil {
		return nil, nil, err
	}
	if cur.Version == 0 {
		return nil, nil, ErrSecretNotFound
	}
	if cur.Deleted {
		return nil, nil, ErrSecretDeleted
	}
	return b.getVersion(ctx, path, cur.Version)
}

// GetVersion retrieves a specific version of a secret.
func (b *KVBackend) GetVersion(ctx context.Context, path string, version int) (map[string][]byte, map[string]string, error) {
	if path == "" {
		return nil, nil, ErrInvalidPath
	}
	if version <= 0 {
		return nil, nil, ErrVersionMustBePositive
	}
	return b.getVersion(ctx, path, version)
}

// List returns all secret paths matching the prefix.
func (b *KVBackend) List(ctx context.Context, prefix string) ([]string, error) {
	fullPrefix := kvBackendKeyPrefix + prefix
	var paths []string
	seen := make(map[string]struct{})
	err := b.store.Scan(ctx, fullPrefix, func(key string, _ []byte) error {
		// Keys are: _qb/secrets/<path>/current or _qb/secrets/<path>/v/<N>
		rel := strings.TrimPrefix(key, kvBackendKeyPrefix)
		// Extract the path (everything before the last slash component that is
		// either "current" or starts with "v/").
		parts := strings.Split(rel, "/")
		if len(parts) < 2 {
			return nil
		}
		var secretPath string
		if parts[len(parts)-1] == "current" {
			secretPath = strings.Join(parts[:len(parts)-1], "/")
		} else if len(parts) >= 3 && parts[len(parts)-2] == "v" {
			secretPath = strings.Join(parts[:len(parts)-2], "/")
		} else {
			return nil
		}
		if _, ok := seen[secretPath]; !ok {
			seen[secretPath] = struct{}{}
			paths = append(paths, secretPath)
		}
		return nil
	})
	return paths, err
}

// Delete soft-deletes a secret (preserves versions).
func (b *KVBackend) Delete(ctx context.Context, path string) error {
	if path == "" {
		return ErrInvalidPath
	}
	b.mu.Lock()
	defer b.mu.Unlock()

	cur, err := b.loadCurrent(ctx, path)
	if err != nil {
		return err
	}
	if cur.Version == 0 {
		return ErrSecretNotFound
	}
	now := time.Now()
	cur.Deleted = true
	cur.DeletedAt = &now
	return b.writeCurrent(ctx, path, cur)
}

// Destroy permanently removes all versions of a secret.
func (b *KVBackend) Destroy(ctx context.Context, path string) error {
	if path == "" {
		return ErrInvalidPath
	}
	b.mu.Lock()
	defer b.mu.Unlock()

	cur, err := b.loadCurrent(ctx, path)
	if err != nil {
		return err
	}
	if cur.Version == 0 {
		return ErrSecretNotFound
	}
	// Delete all versioned entries.
	for v := 1; v <= cur.Version; v++ {
		vKey := versionKey(path, v)
		if delErr := b.store.Delete(ctx, vKey); delErr != nil {
			return &SecretsCauseError{Message: "destroy: failed to delete version", Cause: delErr}
		}
	}
	// Delete the current pointer.
	return b.store.Delete(ctx, currentKey(path))
}

// ListVersions returns metadata for all versions of a secret.
func (b *KVBackend) ListVersions(ctx context.Context, path string) ([]*VersionMetadata, error) {
	if path == "" {
		return nil, ErrInvalidPath
	}
	cur, err := b.loadCurrent(ctx, path)
	if err != nil {
		return nil, err
	}
	if cur.Version == 0 {
		return nil, ErrSecretNotFound
	}
	var versions []*VersionMetadata
	for v := 1; v <= cur.Version; v++ {
		raw, err := b.store.Get(ctx, versionKey(path, v))
		if err != nil || raw == nil {
			continue
		}
		var entry kvEntry
		if err := json.Unmarshal(raw, &entry); err != nil {
			continue
		}
		vm := &VersionMetadata{
			Version:   entry.Version,
			CreatedAt: entry.CreatedAt,
			Deleted:   entry.Destroyed,
		}
		if entry.DeletedAt != nil {
			vm.DeletedAt = entry.DeletedAt
		}
		versions = append(versions, vm)
	}
	return versions, nil
}

// Rollback restores a previous version as the current version.
func (b *KVBackend) Rollback(ctx context.Context, path string, version int) error {
	if path == "" {
		return ErrInvalidPath
	}
	if version <= 0 {
		return ErrVersionMustBePositive
	}
	b.mu.Lock()
	defer b.mu.Unlock()

	cur, err := b.loadCurrent(ctx, path)
	if err != nil {
		return err
	}
	if cur.Version == 0 {
		return ErrSecretNotFound
	}
	if version > cur.Version {
		return ErrVersionNotFound
	}

	// Load the target version to get its data.
	raw, err := b.store.Get(ctx, versionKey(path, version))
	if err != nil || raw == nil {
		return ErrVersionNotFound
	}
	var oldEntry kvEntry
	if err := json.Unmarshal(raw, &oldEntry); err != nil {
		return &SecretsSentinelCauseError{Sentinel: ErrDeserializationFailed, Cause: err}
	}
	if oldEntry.Destroyed {
		return ErrVersionDestroyed
	}

	// Write as a new version.
	now := time.Now()
	nextVer := cur.Version + 1
	newEntry := &kvEntry{
		Path:      path,
		Data:      oldEntry.Data,
		Metadata:  oldEntry.Metadata,
		Version:   nextVer,
		CreatedAt: now,
		UpdatedAt: now,
	}
	if err := b.writeEntry(ctx, path, nextVer, newEntry); err != nil {
		return err
	}
	cur.Version = nextVer
	cur.Deleted = false
	cur.DeletedAt = nil
	return b.writeCurrent(ctx, path, cur)
}

// Type returns the backend type identifier.
func (b *KVBackend) Type() string { return kvBackendType }

// Close is a no-op; the underlying KV store's lifecycle is owned by the caller.
func (b *KVBackend) Close() error { return nil }

// ---------------------------------------------------------------------------
// internal helpers
// ---------------------------------------------------------------------------

func currentKey(path string) string {
	return kvBackendKeyPrefix + path + "/current"
}

func versionKey(path string, version int) string {
	return fmt.Sprintf("%s%s/v/%d", kvBackendKeyPrefix, path, version)
}

func (b *KVBackend) loadCurrent(ctx context.Context, path string) (*kvCurrentEntry, error) {
	raw, err := b.store.Get(ctx, currentKey(path))
	if err != nil {
		return nil, &SecretsCauseError{Message: "load current pointer", Cause: err}
	}
	if raw == nil {
		return &kvCurrentEntry{}, nil
	}
	var cur kvCurrentEntry
	if err := json.Unmarshal(raw, &cur); err != nil {
		return nil, &SecretsSentinelCauseError{Sentinel: ErrDeserializationFailed, Cause: err}
	}
	return &cur, nil
}

func (b *KVBackend) writeCurrent(ctx context.Context, path string, cur *kvCurrentEntry) error {
	raw, err := json.Marshal(cur)
	if err != nil {
		return &SecretsSentinelCauseError{Sentinel: ErrSerializationFailed, Cause: err}
	}
	if err := b.store.Put(ctx, currentKey(path), raw); err != nil {
		return &SecretsCauseError{Message: "write current pointer", Cause: err}
	}
	return nil
}

func (b *KVBackend) writeEntry(ctx context.Context, path string, version int, entry *kvEntry) error {
	raw, err := json.Marshal(entry)
	if err != nil {
		return &SecretsSentinelCauseError{Sentinel: ErrSerializationFailed, Cause: err}
	}
	if err := b.store.Put(ctx, versionKey(path, version), raw); err != nil {
		return &SecretsCauseError{Message: "write version entry", Cause: err}
	}
	return nil
}

func (b *KVBackend) getVersion(ctx context.Context, path string, version int) (map[string][]byte, map[string]string, error) {
	raw, err := b.store.Get(ctx, versionKey(path, version))
	if err != nil {
		return nil, nil, &SecretsCauseError{Message: "get version entry", Cause: err}
	}
	if raw == nil {
		return nil, nil, ErrVersionNotFound
	}
	var entry kvEntry
	if err := json.Unmarshal(raw, &entry); err != nil {
		return nil, nil, &SecretsSentinelCauseError{Sentinel: ErrDeserializationFailed, Cause: err}
	}
	if entry.Destroyed {
		return nil, nil, ErrVersionDestroyed
	}
	return entry.Data, entry.Metadata, nil
}
