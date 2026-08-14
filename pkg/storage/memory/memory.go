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

// Package memory provides a pure in-memory storage implementation for QuicBaaS.
//
// This package implements the Storage interface defined in pkg/statemachine/storage.go
// with a thread-safe, ephemeral in-memory backend. All data is stored in RAM and is
// lost when the storage is closed.
//
// Features:
//   - Thread-safe: Uses RWMutex for concurrent access
//   - Defensive copies: All keys and values are copied to prevent external modification
//   - Sorted scans: Scan results are always returned in lexicographic order
//   - Snapshots: Create point-in-time copies for backup or testing
//   - High performance: ~65ns Get, ~377ns Set operations
//
// Use cases:
//   - Testing and development
//   - Caching layer
//   - Temporary data storage
//   - In-memory state machines
//
// Example:
//
//	log, _ := logger.New(logger.Default())
//	storage := memory.New(log)
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
//	// Snapshot and restore
//	snapshot := storage.Snapshot()
//	storage2 := memory.New(log)
//	storage2.Restore(snapshot)
package memory

import (
	"bytes"
	"sort"
	"sync"

	"github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
)

// MemoryStorage implements a thread-safe in-memory key-value store.
// Data is stored in a map with RWMutex for concurrent access.
// All data is ephemeral and lost when the storage is closed.
type MemoryStorage struct {
	data   map[string][]byte
	mu     sync.RWMutex
	logger *logger.Logger
	closed bool
}

// New creates a new in-memory storage instance.
// The logger parameter is optional; if nil, a default logger will be created.
func New(log *logger.Logger) *MemoryStorage {
	if log == nil {
		log, _ = logger.New(logger.Default())
	}

	return &MemoryStorage{
		data:   make(map[string][]byte),
		logger: log.WithField("component", "memory-storage"),
		closed: false,
	}
}

// Get retrieves a value by key.
// Returns ErrNotFound if key doesn't exist.
// Returns a copy of the value to prevent external modification.
func (m *MemoryStorage) Get(key []byte) ([]byte, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if m.closed {
		return nil, errors.Internal("memory.Get", "storage is closed")
	}

	if len(key) == 0 {
		return nil, errors.InvalidInput("memory.Get", "key cannot be empty")
	}

	value, exists := m.data[string(key)]
	if !exists {
		return nil, errors.NotFound("memory.Get", "key")
	}

	// Return a copy to prevent external modification
	result := make([]byte, len(value))
	copy(result, value)

	return result, nil
}

// Set stores a key-value pair.
// Overwrites existing value if key exists.
// Stores a copy of the value to prevent external modification.
func (m *MemoryStorage) Set(key, value []byte) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.closed {
		return errors.Internal("memory.Set", "storage is closed")
	}

	if len(key) == 0 {
		return errors.InvalidInput("memory.Set", "key cannot be empty")
	}

	// Store a copy to prevent external modification
	valueCopy := make([]byte, len(value))
	copy(valueCopy, value)

	m.data[string(key)] = valueCopy

	return nil
}

// Delete removes a key-value pair.
// Does not return error if key doesn't exist (idempotent).
func (m *MemoryStorage) Delete(key []byte) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.closed {
		return errors.Internal("memory.Delete", "storage is closed")
	}

	if len(key) == 0 {
		return errors.InvalidInput("memory.Delete", "key cannot be empty")
	}

	// Delete is idempotent - no error if key doesn't exist
	delete(m.data, string(key))

	return nil
}

// Scan returns all key-value pairs in range [start, end).
// If end is nil, scans to the end of the keyspace.
// Results are sorted by key for consistent ordering.
// Returns copies of all keys and values.
func (m *MemoryStorage) Scan(start, end []byte) ([][2][]byte, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if m.closed {
		return nil, errors.Internal("memory.Scan", "storage is closed")
	}

	// Collect matching keys
	var keys []string
	for key := range m.data {
		// Check if key is in range [start, end)
		keyBytes := []byte(key)

		// Check lower bound
		if len(start) > 0 && bytes.Compare(keyBytes, start) < 0 {
			continue
		}

		// Check upper bound
		if len(end) > 0 && bytes.Compare(keyBytes, end) >= 0 {
			continue
		}

		keys = append(keys, key)
	}

	// Sort keys for consistent ordering
	sort.Strings(keys)

	// Build result with copies
	result := make([][2][]byte, 0, len(keys))
	for _, key := range keys {
		value := m.data[key]

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

// Close clears all data and marks storage as closed.
// This operation is idempotent.
func (m *MemoryStorage) Close() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.closed {
		return nil
	}

	// Clear all data
	m.data = nil
	m.closed = true

	m.logger.Info("Memory storage closed")

	return nil
}

// Snapshot creates a point-in-time copy of all data.
// Useful for backup or testing.
// Returns a map with copies of all key-value pairs.
func (m *MemoryStorage) Snapshot() map[string][]byte {
	m.mu.RLock()
	defer m.mu.RUnlock()

	snapshot := make(map[string][]byte, len(m.data))
	for key, value := range m.data {
		// Deep copy value
		valueCopy := make([]byte, len(value))
		copy(valueCopy, value)
		snapshot[key] = valueCopy
	}

	return snapshot
}

// Restore loads data from a snapshot.
// Overwrites all existing data.
// Copies all values from the snapshot to prevent external modification.
func (m *MemoryStorage) Restore(snapshot map[string][]byte) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.closed {
		return errors.Internal("memory.Restore", "storage is closed")
	}

	// Clear existing data
	m.data = make(map[string][]byte, len(snapshot))

	// Copy all data from snapshot
	for key, value := range snapshot {
		valueCopy := make([]byte, len(value))
		copy(valueCopy, value)
		m.data[key] = valueCopy
	}

	m.logger.Infof("Restored %d key-value pairs from snapshot", len(snapshot))

	return nil
}

// Len returns the number of key-value pairs in storage.
func (m *MemoryStorage) Len() int {
	m.mu.RLock()
	defer m.mu.RUnlock()

	return len(m.data)
}
