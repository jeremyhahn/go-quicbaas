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

package server

import (
	"context"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"sync"

	dberrors "github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
)

// FileKVStore implements interfaces.KVStore using the filesystem.
// Each key-value pair is stored as a separate file in a directory.
// Keys are hex-encoded to create safe filenames.
//
// This provides simple persistence without the complexity of PebbleDB,
// suitable for development, testing, and low-volume deployments.
//
// Thread-safe using sync.RWMutex for concurrent access.
type FileKVStore struct {
	dataDir string
	mu      sync.RWMutex
}

// NewFileKVStore creates a new file-based KV store.
// The dataDir must be an existing directory or will be created.
func NewFileKVStore(dataDir string) (*FileKVStore, error) {
	if dataDir == "" {
		return nil, &dberrors.DragonError{
			Code: dberrors.ErrInvalidInput,
			Op:   "file_kv_store.new",
			Err:  ErrDataDirRequired,
		}
	}

	// Create the data directory if it doesn't exist
	if err := os.MkdirAll(dataDir, 0750); err != nil {
		return nil, &dberrors.DragonError{
			Code: dberrors.ErrInternal,
			Op:   "file_kv_store.new",
			Err:  err,
		}
	}

	return &FileKVStore{
		dataDir: dataDir,
	}, nil
}

// keyToFilename converts a string key to a safe filename using hex encoding.
func (f *FileKVStore) keyToFilename(key string) string {
	return hex.EncodeToString([]byte(key))
}

// filenameToKey converts a hex-encoded filename back to the original string key.
func (f *FileKVStore) filenameToKey(filename string) (string, error) {
	raw, err := hex.DecodeString(filename)
	if err != nil {
		return "", err
	}
	return string(raw), nil
}

// Put stores a key-value pair as a file.
func (f *FileKVStore) Put(_ context.Context, key string, value []byte) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	filename := f.keyToFilename(key)
	filePath := filepath.Join(f.dataDir, filename)

	// Write atomically using temp file + rename
	tmpPath := filePath + ".tmp"
	if err := os.WriteFile(tmpPath, value, 0600); err != nil {
		return &dberrors.DragonError{
			Code: dberrors.ErrInternal,
			Op:   "file_kv_store.put",
			Err:  err,
		}
	}

	if err := os.Rename(tmpPath, filePath); err != nil {
		// Clean up temp file on failure (ignore cleanup errors)
		_ = os.Remove(tmpPath)
		return &dberrors.DragonError{
			Code: dberrors.ErrInternal,
			Op:   "file_kv_store.put",
			Err:  err,
		}
	}

	return nil
}

// Get retrieves a value by key from the filesystem.
func (f *FileKVStore) Get(_ context.Context, key string) ([]byte, error) {
	f.mu.RLock()
	defer f.mu.RUnlock()

	filename := f.keyToFilename(key)
	filePath := filepath.Join(f.dataDir, filename)

	data, err := os.ReadFile(filePath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, &dberrors.DragonError{Code: dberrors.ErrNotFound, Op: "file_kv_store.get"}
		}
		return nil, &dberrors.DragonError{
			Code: dberrors.ErrInternal,
			Op:   "file_kv_store.get",
			Err:  err,
		}
	}

	return data, nil
}

// Delete removes a key-value pair from the filesystem.
func (f *FileKVStore) Delete(_ context.Context, key string) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	filename := f.keyToFilename(key)
	filePath := filepath.Join(f.dataDir, filename)

	err := os.Remove(filePath)
	if err != nil && !os.IsNotExist(err) {
		return &dberrors.DragonError{
			Code: dberrors.ErrInternal,
			Op:   "file_kv_store.delete",
			Err:  err,
		}
	}

	return nil
}

// Scan iterates over all key-value pairs matching the given prefix.
func (f *FileKVStore) Scan(_ context.Context, prefix string, fn func(key string, value []byte) error) error {
	f.mu.RLock()
	defer f.mu.RUnlock()

	entries, err := os.ReadDir(f.dataDir)
	if err != nil {
		return &dberrors.DragonError{
			Code: dberrors.ErrInternal,
			Op:   "file_kv_store.scan",
			Err:  err,
		}
	}

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}

		// Skip temp files
		if strings.HasSuffix(entry.Name(), ".tmp") {
			continue
		}

		key, err := f.filenameToKey(entry.Name())
		if err != nil {
			// Skip files that aren't valid hex-encoded keys
			continue
		}

		// Check prefix match
		if !strings.HasPrefix(key, prefix) {
			continue
		}

		// Read the value
		filePath := filepath.Join(f.dataDir, entry.Name())
		value, err := os.ReadFile(filePath)
		if err != nil {
			continue // Skip files we can't read
		}

		if err := fn(key, value); err != nil {
			return err
		}
	}

	return nil
}

// List returns all keys matching the given prefix.
func (f *FileKVStore) List(_ context.Context, prefix string) ([]string, error) {
	f.mu.RLock()
	defer f.mu.RUnlock()

	var keys []string

	entries, err := os.ReadDir(f.dataDir)
	if err != nil {
		return nil, &dberrors.DragonError{
			Code: dberrors.ErrInternal,
			Op:   "file_kv_store.list",
			Err:  err,
		}
	}

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}

		// Skip temp files
		if strings.HasSuffix(entry.Name(), ".tmp") {
			continue
		}

		key, err := f.filenameToKey(entry.Name())
		if err != nil {
			// Skip files that aren't valid hex-encoded keys
			continue
		}

		// Check prefix match
		if strings.HasPrefix(key, prefix) {
			keys = append(keys, key)
		}
	}

	return keys, nil
}

// Exists checks whether a key exists in the filesystem.
func (f *FileKVStore) Exists(_ context.Context, key string) (bool, error) {
	f.mu.RLock()
	defer f.mu.RUnlock()

	filename := f.keyToFilename(key)
	filePath := filepath.Join(f.dataDir, filename)

	_, err := os.Stat(filePath)
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, &dberrors.DragonError{
			Code: dberrors.ErrInternal,
			Op:   "file_kv_store.exists",
			Err:  err,
		}
	}

	return true, nil
}

// Close is a no-op for FileKVStore (files are closed after each operation).
func (f *FileKVStore) Close() error {
	return nil
}

// DataDir returns the data directory path.
func (f *FileKVStore) DataDir() string {
	return f.dataDir
}

var _ interfaces.KVStore = (*FileKVStore)(nil)
