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

// Package file provides a file-based storage implementation for QuicBaaS.
//
// This package implements the Storage interface with a file-system backend.
// Each key-value pair is stored as a separate file, with the key determining
// the file path and the value stored as the file content.
//
// Features:
//   - Thread-safe: Uses RWMutex for concurrent access
//   - Atomic writes: Uses temp file + sync + rename pattern
//   - Hierarchical keys: Key "users/123" maps to file "users/123.ext"
//   - Configurable durability: Optional fsync after writes
//   - Sorted scans: Scan results are always returned in lexicographic order
//   - Snapshots: Create point-in-time copies for backup
//   - Full storage.Storage interface: Batch, Iterator, Snapshot support
//
// Use cases:
//   - Persistent local storage
//   - Development and testing with durability
//   - Simple deployments without external dependencies
//   - Configuration and state persistence
//
// Example:
//
//	log, _ := logger.New(logger.Default())
//	config := file.DefaultConfig()
//	config.DataDir = "/var/data/quicbaas"
//	storage, _ := file.New(config, log)
//	defer storage.Close()
//
//	// Basic operations
//	storage.Set([]byte("key"), []byte("value"))
//	value, _ := storage.Get([]byte("key"))
//	storage.Delete([]byte("key"))
//
//	// Range scan
//	results, _ := storage.Scan([]byte("start"), []byte("end"))
//
//	// Batch operations
//	batch := storage.Batch()
//	batch.Set([]byte("key1"), []byte("value1"))
//	batch.Set([]byte("key2"), []byte("value2"))
//	batch.Commit()
//
//	// Iteration
//	iter := storage.NewIterator(nil)
//	for iter.SeekToFirst(); iter.Valid(); iter.Next() {
//	    fmt.Printf("key=%s value=%s\n", iter.Key(), iter.Value())
//	}
//	iter.Close()
//
//	// Point-in-time snapshot
//	snap, _ := storage.CreateSnapshot()
//	defer snap.Close()
//	value, _ = snap.Get([]byte("key"))
package file

import (
	"bytes"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
	"github.com/jeremyhahn/go-quicbaas/pkg/storage"
)

// FileStorage implements a thread-safe file-based key-value store.
// Each key is stored as a separate file with the value as file content.
// Keys containing "/" create subdirectories in the file system.
// Implements storage.Storage interface for unified partition layer access.
type FileStorage struct {
	config    *Config
	extension string
	logger    *logger.Logger
	mu        sync.RWMutex
	closed    bool
}

// Compile-time interface check.
var _ storage.Storage = (*FileStorage)(nil)

// New creates a new file-based storage instance.
// It validates the configuration and creates the data directory if needed.
func New(config *Config, log *logger.Logger) (*FileStorage, error) {
	if config == nil {
		return nil, errors.InvalidInput("file.New", "config cannot be nil")
	}

	if err := config.Validate(); err != nil {
		return nil, err
	}

	if log == nil {
		var err error
		log, err = logger.New(logger.Default())
		if err != nil {
			return nil, errors.Wrap(err, errors.ErrInternal, "file.New")
		}
	}

	// Get extension from codec type
	extension := ExtensionForCodec(config.CodecType)

	// Create data directory if it doesn't exist
	if err := os.MkdirAll(config.DataDir, config.DirPermissions); err != nil {
		return nil, errors.Wrap(err, errors.ErrInternal, "file.New")
	}

	return &FileStorage{
		config:    config,
		extension: extension,
		logger:    log.WithField("component", "file-storage"),
		closed:    false,
	}, nil
}

// keyToPath converts a key to a file path.
// Key "users/123" -> "{DataDir}/users/123{extension}".
func (f *FileStorage) keyToPath(key []byte) string {
	return filepath.Join(f.config.DataDir, string(key)+f.extension)
}

// pathToKey converts a file path back to a key.
// Path "{DataDir}/users/123{extension}" -> "users/123".
func (f *FileStorage) pathToKey(path string) string {
	// Remove data dir prefix
	rel, err := filepath.Rel(f.config.DataDir, path)
	if err != nil {
		return ""
	}

	// Remove extension suffix
	return strings.TrimSuffix(rel, f.extension)
}

// Get retrieves a value by key.
// Returns errors.NotFound if key doesn't exist.
// Returns errors.InvalidInput if key is empty.
// Returns errors.Internal if store is closed.
func (f *FileStorage) Get(key []byte) ([]byte, error) {
	f.mu.RLock()
	defer f.mu.RUnlock()

	if f.closed {
		return nil, errors.New(errors.ErrStorage, "file.Get", "storage is closed")
	}

	if len(key) == 0 {
		return nil, errors.New(errors.ErrInvalidInput, "file.Get", "key cannot be empty")
	}

	path := f.keyToPath(key)
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, errors.New(errors.ErrNotFound, "file.Get", "key not found")
		}
		return nil, errors.Wrap(err, errors.ErrStorage, "file.Get")
	}

	return data, nil
}

// Exists reports whether the key exists in storage.
func (f *FileStorage) Exists(key []byte) (bool, error) {
	f.mu.RLock()
	defer f.mu.RUnlock()

	if f.closed {
		return false, errors.New(errors.ErrStorage, "file.Exists", "storage is closed")
	}

	if len(key) == 0 {
		return false, errors.New(errors.ErrInvalidInput, "file.Exists", "key cannot be empty")
	}

	path := f.keyToPath(key)
	_, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, errors.Wrap(err, errors.ErrStorage, "file.Exists")
	}
	return true, nil
}

// List returns all keys matching the given prefix.
func (f *FileStorage) List(prefix []byte) ([][]byte, error) {
	f.mu.RLock()
	defer f.mu.RUnlock()

	if f.closed {
		return nil, errors.New(errors.ErrStorage, "file.List", "storage is closed")
	}

	keys, err := f.listKeys()
	if err != nil {
		return nil, err
	}

	pfx := string(prefix)
	var result [][]byte
	for _, k := range keys {
		if len(pfx) == 0 || strings.HasPrefix(k, pfx) {
			result = append(result, []byte(k))
		}
	}
	return result, nil
}

// Set stores a key-value pair atomically.
// Creates parent directories as needed.
// Uses temp file + sync + rename for atomic writes.
// Returns errors.InvalidInput if key is empty.
// Returns errors.Internal if store is closed.
func (f *FileStorage) Set(key, value []byte) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	return f.setUnlocked(key, value)
}

// setUnlocked performs the set operation without acquiring the lock.
// Used internally by batch commit.
func (f *FileStorage) setUnlocked(key, value []byte) error {
	if f.closed {
		return errors.New(errors.ErrStorage, "file.Set", "storage is closed")
	}

	if len(key) == 0 {
		return errors.New(errors.ErrInvalidInput, "file.Set", "key cannot be empty")
	}

	path := f.keyToPath(key)
	dir := filepath.Dir(path)

	// Create parent directories if needed
	if err := os.MkdirAll(dir, f.config.DirPermissions); err != nil {
		return errors.Wrap(err, errors.ErrStorage, "file.Set")
	}

	// Write to temp file first for atomic operation
	tmpPath := path + ".tmp"
	tmpFile, err := os.OpenFile(tmpPath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, f.config.FilePermissions)
	if err != nil {
		return errors.Wrap(err, errors.ErrStorage, "file.Set")
	}

	// Write content
	_, writeErr := tmpFile.Write(value)
	if writeErr != nil {
		_ = tmpFile.Close() // Best-effort cleanup
		_ = os.Remove(tmpPath)
		return errors.Wrap(writeErr, errors.ErrStorage, "file.Set")
	}

	// Sync to disk if configured
	if f.config.SyncWrites {
		if syncErr := tmpFile.Sync(); syncErr != nil {
			_ = tmpFile.Close() // Best-effort cleanup
			_ = os.Remove(tmpPath)
			return errors.Wrap(syncErr, errors.ErrStorage, "file.Set")
		}
	}

	// Close the temp file
	if closeErr := tmpFile.Close(); closeErr != nil {
		_ = os.Remove(tmpPath) // Best-effort cleanup
		return errors.Wrap(closeErr, errors.ErrStorage, "file.Set")
	}

	// Atomic rename
	if renameErr := os.Rename(tmpPath, path); renameErr != nil {
		_ = os.Remove(tmpPath) // Best-effort cleanup
		return errors.Wrap(renameErr, errors.ErrStorage, "file.Set")
	}

	return nil
}

// Delete removes a key-value pair.
// This operation is idempotent - returns nil if key doesn't exist.
// Returns errors.InvalidInput if key is empty.
// Returns errors.Internal if store is closed.
func (f *FileStorage) Delete(key []byte) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	return f.deleteUnlocked(key)
}

// deleteUnlocked performs the delete operation without acquiring the lock.
// Used internally by batch commit.
func (f *FileStorage) deleteUnlocked(key []byte) error {
	if f.closed {
		return errors.New(errors.ErrStorage, "file.Delete", "storage is closed")
	}

	if len(key) == 0 {
		return errors.New(errors.ErrInvalidInput, "file.Delete", "key cannot be empty")
	}

	path := f.keyToPath(key)
	err := os.Remove(path)
	if err != nil {
		// Delete is idempotent - no error if file doesn't exist
		if os.IsNotExist(err) {
			return nil
		}
		return errors.Wrap(err, errors.ErrStorage, "file.Delete")
	}

	return nil
}

// Batch creates a new batch for atomic write operations.
// The batch must be committed or rolled back explicitly.
func (f *FileStorage) Batch() storage.Batch {
	return newFileBatch(f)
}

// NewIterator creates a new iterator with the given options.
// The iterator must be closed when done to release resources.
func (f *FileStorage) NewIterator(opts *storage.IteratorOptions) storage.Iterator {
	return newFileIterator(f, opts)
}

// Snapshot creates a consistent point-in-time view of the storage.
// The snapshot must be closed when done to release resources.
// This implements the storage.Storage interface.
func (f *FileStorage) Snapshot() (storage.Snapshot, error) {
	f.mu.RLock()
	defer f.mu.RUnlock()

	if f.closed {
		return nil, errors.New(errors.ErrStorage, "file.Snapshot", "storage is closed")
	}

	return newFileSnapshot(f)
}

// CreateSnapshotMap creates a point-in-time copy of all data as a map.
// Returns a map with all key-value pairs.
// This is the legacy method for backward compatibility.
func (f *FileStorage) CreateSnapshotMap() (map[string][]byte, error) {
	f.mu.RLock()
	defer f.mu.RUnlock()

	if f.closed {
		return nil, errors.New(errors.ErrStorage, "file.CreateSnapshotMap", "storage is closed")
	}

	snapshot := make(map[string][]byte)

	err := filepath.WalkDir(f.config.DataDir, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}

		// Skip directories
		if d.IsDir() {
			return nil
		}

		// Skip temp files (check first since temp files are named .ext.tmp)
		if strings.HasSuffix(path, ".tmp") {
			return nil
		}

		// Skip files without correct extension
		if !strings.HasSuffix(path, f.extension) {
			return nil
		}

		key := f.pathToKey(path)
		if key == "" {
			return nil
		}

		// Read file content
		data, readErr := os.ReadFile(path) // #nosec G122 -- internal storage paths from WalkDir, not user-supplied
		if readErr != nil {
			// Skip files that can't be read (might have been deleted)
			if os.IsNotExist(readErr) {
				return nil
			}
			return readErr
		}

		// Deep copy value
		valueCopy := make([]byte, len(data))
		copy(valueCopy, data)
		snapshot[key] = valueCopy

		return nil
	})

	if err != nil {
		return nil, errors.Wrap(err, errors.ErrStorage, "file.CreateSnapshotMap")
	}

	return snapshot, nil
}

// Scan returns all key-value pairs in range [start, end).
// If end is nil or empty, scans to the end of the keyspace.
// Results are sorted by key for consistent ordering.
func (f *FileStorage) Scan(start, end []byte) ([][2][]byte, error) {
	f.mu.RLock()
	defer f.mu.RUnlock()

	if f.closed {
		return nil, errors.New(errors.ErrStorage, "file.Scan", "storage is closed")
	}

	// Collect all keys and values
	var keys []string
	keyValues := make(map[string][]byte)

	err := filepath.WalkDir(f.config.DataDir, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}

		// Skip directories
		if d.IsDir() {
			return nil
		}

		// Skip temp files (check first since temp files are named .ext.tmp)
		if strings.HasSuffix(path, ".tmp") {
			return nil
		}

		// Skip files without correct extension
		if !strings.HasSuffix(path, f.extension) {
			return nil
		}

		key := f.pathToKey(path)
		if key == "" {
			return nil
		}

		keyBytes := []byte(key)

		// Check if key is in range [start, end)
		if len(start) > 0 && bytes.Compare(keyBytes, start) < 0 {
			return nil
		}
		if len(end) > 0 && bytes.Compare(keyBytes, end) >= 0 {
			return nil
		}

		// Read file content
		data, readErr := os.ReadFile(path) // #nosec G122 -- internal storage paths from WalkDir, not user-supplied
		if readErr != nil {
			// Skip files that can't be read (might have been deleted)
			if os.IsNotExist(readErr) {
				return nil
			}
			return readErr
		}

		keys = append(keys, key)
		keyValues[key] = data

		return nil
	})

	if err != nil {
		return nil, errors.Wrap(err, errors.ErrStorage, "file.Scan")
	}

	// Sort keys for consistent ordering
	sort.Strings(keys)

	// Build result
	result := make([][2][]byte, 0, len(keys))
	for _, key := range keys {
		value := keyValues[key]

		// Copy key
		keyCopy := make([]byte, len(key))
		copy(keyCopy, key)

		// Copy value
		valueCopy := make([]byte, len(value))
		copy(valueCopy, value)

		result = append(result, [2][]byte{keyCopy, valueCopy})
	}

	return result, nil
}

// Restore loads data from a snapshot.
// Clears all existing data and writes all snapshot entries.
func (f *FileStorage) Restore(snapshot map[string][]byte) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	if f.closed {
		return errors.New(errors.ErrStorage, "file.Restore", "storage is closed")
	}

	// Clear existing data by removing all files
	err := filepath.WalkDir(f.config.DataDir, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}

		// Skip the root directory itself
		if path == f.config.DataDir {
			return nil
		}

		// Skip directories on first pass - we'll clean them up after
		if d.IsDir() {
			return nil
		}

		// Remove files with correct extension
		if strings.HasSuffix(path, f.extension) {
			return os.Remove(path) // #nosec G122 -- internal storage paths from WalkDir, not user-supplied
		}

		return nil
	})

	if err != nil {
		return errors.Wrap(err, errors.ErrStorage, "file.Restore")
	}

	// Write all snapshot entries
	for key, value := range snapshot {
		path := f.keyToPath([]byte(key))
		dir := filepath.Dir(path)

		// Create parent directories if needed
		if mkdirErr := os.MkdirAll(dir, f.config.DirPermissions); mkdirErr != nil {
			return errors.Wrap(mkdirErr, errors.ErrStorage, "file.Restore")
		}

		// Write file atomically
		tmpPath := path + ".tmp"
		tmpFile, createErr := os.OpenFile(tmpPath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, f.config.FilePermissions)
		if createErr != nil {
			return errors.Wrap(createErr, errors.ErrStorage, "file.Restore")
		}

		if _, writeErr := tmpFile.Write(value); writeErr != nil {
			_ = tmpFile.Close() // Best-effort cleanup
			_ = os.Remove(tmpPath)
			return errors.Wrap(writeErr, errors.ErrStorage, "file.Restore")
		}

		if f.config.SyncWrites {
			if syncErr := tmpFile.Sync(); syncErr != nil {
				_ = tmpFile.Close() // Best-effort cleanup
				_ = os.Remove(tmpPath)
				return errors.Wrap(syncErr, errors.ErrStorage, "file.Restore")
			}
		}

		if closeErr := tmpFile.Close(); closeErr != nil {
			_ = os.Remove(tmpPath) // Best-effort cleanup
			return errors.Wrap(closeErr, errors.ErrStorage, "file.Restore")
		}

		if renameErr := os.Rename(tmpPath, path); renameErr != nil {
			_ = os.Remove(tmpPath) // Best-effort cleanup
			return errors.Wrap(renameErr, errors.ErrStorage, "file.Restore")
		}
	}

	f.logger.Infof("Restored %d key-value pairs from snapshot", len(snapshot))

	return nil
}

// Len returns the number of key-value pairs in storage.
func (f *FileStorage) Len() (int, error) {
	f.mu.RLock()
	defer f.mu.RUnlock()

	if f.closed {
		return 0, errors.New(errors.ErrStorage, "file.Len", "storage is closed")
	}

	count := 0
	err := filepath.WalkDir(f.config.DataDir, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}

		// Skip directories
		if d.IsDir() {
			return nil
		}

		// Count only files with correct extension (excluding temp files)
		if strings.HasSuffix(path, f.extension) && !strings.HasSuffix(path, ".tmp") {
			count++
		}

		return nil
	})

	if err != nil {
		return 0, errors.Wrap(err, errors.ErrStorage, "file.Len")
	}

	return count, nil
}

// listKeys returns all keys in sorted order.
// Used internally by iterator and snapshot.
func (f *FileStorage) listKeys() ([]string, error) {
	var keys []string

	err := filepath.WalkDir(f.config.DataDir, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}

		// Skip directories
		if d.IsDir() {
			return nil
		}

		// Skip temp files
		if strings.HasSuffix(path, ".tmp") {
			return nil
		}

		// Skip files without correct extension
		if !strings.HasSuffix(path, f.extension) {
			return nil
		}

		key := f.pathToKey(path)
		if key != "" {
			keys = append(keys, key)
		}

		return nil
	})

	if err != nil {
		return nil, errors.Wrap(err, errors.ErrStorage, "file.listKeys")
	}

	sort.Strings(keys)
	return keys, nil
}

// Close marks the storage as closed.
// This operation is idempotent.
func (f *FileStorage) Close() error {
	f.mu.Lock()
	defer f.mu.Unlock()

	if f.closed {
		return nil
	}

	f.closed = true
	f.logger.Info("File storage closed")

	return nil
}
