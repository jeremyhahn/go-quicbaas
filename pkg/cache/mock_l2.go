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

package cache

import (
	"context"
	"sync"
	"time"

	"github.com/jeremyhahn/go-quicbaas/pkg/errors"
)

// MockL2Cache implements a mock L2 cache for testing.
type MockL2Cache struct {
	data      map[string]*mockEntry
	mu        sync.RWMutex
	pingError error
	closed    bool
}

// mockEntry represents a cached entry with expiration.
type mockEntry struct {
	value     []byte
	expiresAt time.Time
}

// NewMockL2Cache creates a new mock L2 cache.
func NewMockL2Cache() *MockL2Cache {
	return &MockL2Cache{
		data: make(map[string]*mockEntry),
	}
}

// Get retrieves a value from the mock L2 cache.
func (m *MockL2Cache) Get(ctx context.Context, key string) ([]byte, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if m.closed {
		return nil, errors.Internal("mock_l2.Get", "cache closed")
	}

	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
	}

	entry, exists := m.data[key]
	if !exists {
		return nil, errors.NotFound("mock_l2.Get", key)
	}

	// Check expiration
	if !entry.expiresAt.IsZero() && time.Now().After(entry.expiresAt) {
		delete(m.data, key)
		return nil, errors.NotFound("mock_l2.Get", key)
	}

	// Return a copy to prevent external modification
	value := make([]byte, len(entry.value))
	copy(value, entry.value)
	return value, nil
}

// Set stores a value in the mock L2 cache.
func (m *MockL2Cache) Set(ctx context.Context, key string, value []byte, ttl time.Duration) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.closed {
		return errors.Internal("mock_l2.Set", "cache closed")
	}

	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}

	// Make a copy to prevent external modification
	valueCopy := make([]byte, len(value))
	copy(valueCopy, value)

	entry := &mockEntry{
		value: valueCopy,
	}

	if ttl > 0 {
		entry.expiresAt = time.Now().Add(ttl)
	}

	m.data[key] = entry
	return nil
}

// Delete removes a value from the mock L2 cache.
func (m *MockL2Cache) Delete(ctx context.Context, key string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.closed {
		return errors.Internal("mock_l2.Delete", "cache closed")
	}

	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}

	delete(m.data, key)
	return nil
}

// Ping checks if the mock L2 cache is accessible.
func (m *MockL2Cache) Ping(ctx context.Context) error {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if m.closed {
		return errors.Internal("mock_l2.Ping", "cache closed")
	}

	if m.pingError != nil {
		return m.pingError
	}

	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}

	return nil
}

// Close closes the mock L2 cache.
func (m *MockL2Cache) Close() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.closed = true
	m.data = nil
	return nil
}

// SetPingError sets an error to be returned by Ping (for testing).
func (m *MockL2Cache) SetPingError(err error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.pingError = err
}

// Clear removes all entries from the mock cache.
func (m *MockL2Cache) Clear() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.data = make(map[string]*mockEntry)
}

// Len returns the number of entries in the mock cache.
func (m *MockL2Cache) Len() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.data)
}

// Keys returns all keys in the mock cache.
func (m *MockL2Cache) Keys() []string {
	m.mu.RLock()
	defer m.mu.RUnlock()

	keys := make([]string, 0, len(m.data))
	for k := range m.data {
		keys = append(keys, k)
	}
	return keys
}

// IsClosed returns whether the cache is closed.
func (m *MockL2Cache) IsClosed() bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.closed
}
