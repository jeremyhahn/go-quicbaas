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
	"bytes"
	"context"
	"sort"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
	"github.com/jeremyhahn/go-quicbaas/pkg/storage"
)

// mockStorageReader implements StorageReader for testing.
type mockStorageReader struct {
	keys     map[string][]byte
	getErrs  map[string]error
	mu       sync.RWMutex
	iterOpts *storage.IteratorOptions
}

func newMockStorageReader() *mockStorageReader {
	return &mockStorageReader{
		keys:    make(map[string][]byte),
		getErrs: make(map[string]error),
	}
}

func (m *mockStorageReader) AddKey(key, value string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.keys[key] = []byte(value)
}

func (m *mockStorageReader) AddError(key string, err error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.getErrs[key] = err
}

func (m *mockStorageReader) NewIterator(opts *storage.IteratorOptions) storage.Iterator {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.iterOpts = opts

	// Collect matching keys
	var matchingKeys []string
	for k := range m.keys {
		keyBytes := []byte(k)

		// Check lower bound
		if opts.LowerBound != nil && bytes.Compare(keyBytes, opts.LowerBound) < 0 {
			continue
		}

		// Check upper bound
		if opts.UpperBound != nil && bytes.Compare(keyBytes, opts.UpperBound) >= 0 {
			continue
		}

		matchingKeys = append(matchingKeys, k)
	}

	// Sort keys
	sort.Strings(matchingKeys)

	// Convert to byte slices
	keys := make([][]byte, len(matchingKeys))
	values := make([][]byte, len(matchingKeys))
	for i, k := range matchingKeys {
		keys[i] = []byte(k)
		values[i] = m.keys[k]
	}

	return newMockIterator(keys, values, opts.Reverse)
}

func (m *mockStorageReader) Get(key []byte) ([]byte, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	keyStr := string(key)

	// Check for configured error
	if err, exists := m.getErrs[keyStr]; exists {
		return nil, err
	}

	// Get value
	value, exists := m.keys[keyStr]
	if !exists {
		return nil, errors.NotFound("mockStorageReader.Get", keyStr)
	}

	return value, nil
}

// mockIterator implements storage.Iterator for testing.
type mockIterator struct {
	keys    [][]byte
	values  [][]byte
	current int
	reverse bool
	err     error
}

func newMockIterator(keys, values [][]byte, reverse bool) *mockIterator {
	current := -1
	if reverse && len(keys) > 0 {
		current = len(keys)
	}

	return &mockIterator{
		keys:    keys,
		values:  values,
		current: current,
		reverse: reverse,
	}
}

func (m *mockIterator) SeekToFirst() bool {
	if m.reverse {
		m.current = len(m.keys) - 1
	} else {
		m.current = 0
	}
	return m.Valid()
}

func (m *mockIterator) SeekToLast() bool {
	if m.reverse {
		m.current = 0
	} else {
		m.current = len(m.keys) - 1
	}
	return m.Valid()
}

func (m *mockIterator) Seek(target []byte) bool {
	for i, key := range m.keys {
		if bytes.Compare(key, target) >= 0 {
			m.current = i
			return true
		}
	}
	m.current = len(m.keys)
	return false
}

func (m *mockIterator) Next() bool {
	if m.reverse {
		m.current--
	} else {
		m.current++
	}
	return m.Valid()
}

func (m *mockIterator) Prev() bool {
	if m.reverse {
		m.current++
	} else {
		m.current--
	}
	return m.Valid()
}

func (m *mockIterator) Valid() bool {
	return m.current >= 0 && m.current < len(m.keys)
}

func (m *mockIterator) Key() []byte {
	if !m.Valid() {
		return nil
	}
	return m.keys[m.current]
}

func (m *mockIterator) Value() []byte {
	if !m.Valid() {
		return nil
	}
	return m.values[m.current]
}

func (m *mockIterator) Error() error {
	return m.err
}

func (m *mockIterator) Close() error {
	return nil
}

func TestNewCacheWarmer(t *testing.T) {
	log := testLogger(t)

	tests := []struct {
		name        string
		cache       *CacheManager
		storage     StorageReader
		config      WarmingConfig
		logger      interfaces.Logger
		expectError bool
		errorMsg    string
	}{
		{
			name:    "valid configuration",
			cache:   mustCreateCache(t, log),
			storage: newMockStorageReader(),
			config: WarmingConfig{
				Enabled:            true,
				BatchSize:          100,
				RateLimitPerSecond: 0,
				MaxKeys:            0,
			},
			logger:      log,
			expectError: false,
		},
		{
			name:        "nil cache",
			cache:       nil,
			storage:     newMockStorageReader(),
			config:      WarmingConfig{},
			logger:      log,
			expectError: true,
			errorMsg:    "cache cannot be nil",
		},
		{
			name:        "nil storage",
			cache:       mustCreateCache(t, log),
			storage:     nil,
			config:      WarmingConfig{},
			logger:      log,
			expectError: true,
			errorMsg:    "storage cannot be nil",
		},
		{
			name:        "nil logger",
			cache:       mustCreateCache(t, log),
			storage:     newMockStorageReader(),
			config:      WarmingConfig{},
			logger:      nil,
			expectError: true,
			errorMsg:    "logger cannot be nil",
		},
		{
			name:    "negative rate limit",
			cache:   mustCreateCache(t, log),
			storage: newMockStorageReader(),
			config: WarmingConfig{
				RateLimitPerSecond: -1,
			},
			logger:      log,
			expectError: true,
			errorMsg:    "RateLimitPerSecond cannot be negative",
		},
		{
			name:    "negative max keys",
			cache:   mustCreateCache(t, log),
			storage: newMockStorageReader(),
			config: WarmingConfig{
				MaxKeys: -1,
			},
			logger:      log,
			expectError: true,
			errorMsg:    "MaxKeys cannot be negative",
		},
		{
			name:    "zero batch size defaults to 100",
			cache:   mustCreateCache(t, log),
			storage: newMockStorageReader(),
			config: WarmingConfig{
				BatchSize: 0,
			},
			logger:      log,
			expectError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			warmer, err := NewCacheWarmer(tt.cache, tt.storage, tt.config, tt.logger)

			if tt.expectError {
				assert.Error(t, err)
				assert.Contains(t, err.Error(), tt.errorMsg)
				assert.Nil(t, warmer)
			} else {
				assert.NoError(t, err)
				assert.NotNil(t, warmer)
				assert.Equal(t, "idle", warmer.GetStatus().State)

				// Check default batch size
				if tt.config.BatchSize == 0 {
					assert.Equal(t, 100, warmer.config.BatchSize)
				}
			}
		})
	}
}

func TestWarmPrefix(t *testing.T) {
	log := testLogger(t)
	cache := mustCreateCache(t, log)
	storage := newMockStorageReader()

	// Add test data
	storage.AddKey("user:1", "alice")
	storage.AddKey("user:2", "bob")
	storage.AddKey("user:3", "charlie")
	storage.AddKey("product:1", "laptop")
	storage.AddKey("product:2", "phone")

	config := WarmingConfig{
		BatchSize: 10,
	}

	warmer, err := NewCacheWarmer(cache, storage, config, log)
	require.NoError(t, err)

	ctx := context.Background()

	// Warm user prefix
	status, err := warmer.WarmPrefix(ctx, "user:")
	require.NoError(t, err)
	assert.Equal(t, "completed", status.State)
	assert.Equal(t, int64(3), status.KeysWarmed)
	assert.Equal(t, int64(0), status.KeysFailed)

	// Verify keys are in cache
	val, err := cache.Get(ctx, "user:1")
	assert.NoError(t, err)
	assert.Equal(t, []byte("alice"), val)

	val, err = cache.Get(ctx, "user:2")
	assert.NoError(t, err)
	assert.Equal(t, []byte("bob"), val)

	val, err = cache.Get(ctx, "user:3")
	assert.NoError(t, err)
	assert.Equal(t, []byte("charlie"), val)

	// Product keys should not be in cache
	_, err = cache.Get(ctx, "product:1")
	assert.Error(t, err)
}

func TestWarmPrefixes(t *testing.T) {
	log := testLogger(t)
	cache := mustCreateCache(t, log)
	storage := newMockStorageReader()

	// Add test data
	storage.AddKey("user:1", "alice")
	storage.AddKey("user:2", "bob")
	storage.AddKey("product:1", "laptop")
	storage.AddKey("product:2", "phone")

	config := WarmingConfig{
		BatchSize: 10,
	}

	warmer, err := NewCacheWarmer(cache, storage, config, log)
	require.NoError(t, err)

	ctx := context.Background()

	// Warm multiple prefixes
	status, err := warmer.WarmPrefixes(ctx, []string{"user:", "product:"})
	require.NoError(t, err)
	assert.Equal(t, "completed", status.State)
	assert.Equal(t, int64(4), status.KeysWarmed)
	assert.Equal(t, int64(0), status.KeysFailed)

	// Verify all keys are in cache
	val, err := cache.Get(ctx, "user:1")
	assert.NoError(t, err)
	assert.Equal(t, []byte("alice"), val)

	val, err = cache.Get(ctx, "product:1")
	assert.NoError(t, err)
	assert.Equal(t, []byte("laptop"), val)
}

func TestRateLimiting(t *testing.T) {
	log := testLogger(t)
	cache := mustCreateCache(t, log)
	storage := newMockStorageReader()

	// Add test data
	for i := 1; i <= 10; i++ {
		storage.AddKey("key:"+string(rune('0'+i)), "value")
	}

	config := WarmingConfig{
		BatchSize:          5,
		RateLimitPerSecond: 10, // 10 keys per second, batch of 5 = 500ms per batch
	}

	warmer, err := NewCacheWarmer(cache, storage, config, log)
	require.NoError(t, err)

	ctx := context.Background()
	start := time.Now()

	status, err := warmer.WarmPrefix(ctx, "key:")
	require.NoError(t, err)

	elapsed := time.Since(start)

	// Should have processed 10 keys in 2 batches
	// Each batch takes 500ms, so minimum time is ~500ms
	assert.Equal(t, "completed", status.State)
	assert.Equal(t, int64(10), status.KeysWarmed)
	assert.GreaterOrEqual(t, elapsed.Milliseconds(), int64(450)) // Allow 10% tolerance
}

func TestBatchProcessing(t *testing.T) {
	log := testLogger(t)
	cache := mustCreateCache(t, log)
	storage := newMockStorageReader()

	// Add 25 test keys
	for i := 1; i <= 25; i++ {
		storage.AddKey("batch:"+string(rune('a'+i-1)), "value")
	}

	config := WarmingConfig{
		BatchSize: 10,
	}

	warmer, err := NewCacheWarmer(cache, storage, config, log)
	require.NoError(t, err)

	ctx := context.Background()

	status, err := warmer.WarmPrefix(ctx, "batch:")
	require.NoError(t, err)

	assert.Equal(t, "completed", status.State)
	assert.Equal(t, int64(25), status.KeysWarmed)
}

func TestContextCancellation(t *testing.T) {
	log := testLogger(t)
	cache := mustCreateCache(t, log)
	storage := newMockStorageReader()

	// Add many keys
	for i := 1; i <= 100; i++ {
		storage.AddKey("cancel:"+string(rune('a'+i%26)), "value")
	}

	config := WarmingConfig{
		BatchSize:          5,
		RateLimitPerSecond: 10, // Slow it down
	}

	warmer, err := NewCacheWarmer(cache, storage, config, log)
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.Background())

	// Cancel after a short delay
	go func() {
		time.Sleep(100 * time.Millisecond)
		cancel()
	}()

	status, err := warmer.WarmPrefix(ctx, "cancel:")

	assert.Error(t, err)
	assert.Equal(t, context.Canceled, err)
	assert.Equal(t, "failed", status.State)
	assert.Less(t, status.KeysWarmed, int64(100))
}

func TestStopSignal(t *testing.T) {
	log := testLogger(t)
	cache := mustCreateCache(t, log)
	storage := newMockStorageReader()

	// Add many keys
	for i := 1; i <= 100; i++ {
		storage.AddKey("stop:"+string(rune('a'+i%26)), "value")
	}

	config := WarmingConfig{
		BatchSize:          5,
		RateLimitPerSecond: 10,
	}

	warmer, err := NewCacheWarmer(cache, storage, config, log)
	require.NoError(t, err)

	ctx := context.Background()

	// Stop after a short delay
	go func() {
		time.Sleep(100 * time.Millisecond)
		warmer.Stop()
	}()

	status, err := warmer.WarmPrefix(ctx, "stop:")

	assert.Error(t, err)
	assert.Equal(t, "failed", status.State)
	assert.Less(t, status.KeysWarmed, int64(100))
}

func TestStatusUpdates(t *testing.T) {
	log := testLogger(t)
	cache := mustCreateCache(t, log)
	storage := newMockStorageReader()

	storage.AddKey("status:1", "value1")
	storage.AddKey("status:2", "value2")

	config := WarmingConfig{
		BatchSize: 10,
	}

	warmer, err := NewCacheWarmer(cache, storage, config, log)
	require.NoError(t, err)

	// Initial status
	status := warmer.GetStatus()
	assert.Equal(t, "idle", status.State)
	assert.True(t, status.StartedAt.IsZero())

	ctx := context.Background()

	// Warm prefix
	status, err = warmer.WarmPrefix(ctx, "status:")
	require.NoError(t, err)

	assert.Equal(t, "completed", status.State)
	assert.False(t, status.StartedAt.IsZero())
	assert.False(t, status.CompletedAt.IsZero())
	assert.Equal(t, int64(2), status.KeysWarmed)
	assert.Equal(t, "status:", status.CurrentPrefix)
}

func TestErrorHandling(t *testing.T) {
	log := testLogger(t)
	cache := mustCreateCache(t, log)
	storage := newMockStorageReader()

	// Add keys with some errors
	storage.AddKey("error:1", "value1")
	storage.AddKey("error:2", "value2")
	storage.AddKey("error:3", "value3")
	storage.AddError("error:2", errors.Internal("cache.test", "simulated error"))

	config := WarmingConfig{
		BatchSize: 10,
	}

	warmer, err := NewCacheWarmer(cache, storage, config, log)
	require.NoError(t, err)

	ctx := context.Background()

	status, err := warmer.WarmPrefix(ctx, "error:")
	require.NoError(t, err) // Individual errors should not fail the operation

	assert.Equal(t, "completed", status.State)
	assert.Equal(t, int64(2), status.KeysWarmed) // error:1 and error:3
	assert.Equal(t, int64(1), status.KeysFailed) // error:2
}

func TestMaxKeys(t *testing.T) {
	log := testLogger(t)
	cache := mustCreateCache(t, log)
	storage := newMockStorageReader()

	// Add 10 keys
	for i := 1; i <= 10; i++ {
		storage.AddKey("max:"+string(rune('0'+i)), "value")
	}

	config := WarmingConfig{
		BatchSize: 5,
		MaxKeys:   5, // Limit to 5 keys
	}

	warmer, err := NewCacheWarmer(cache, storage, config, log)
	require.NoError(t, err)

	ctx := context.Background()

	status, err := warmer.WarmPrefix(ctx, "max:")
	require.NoError(t, err)

	assert.Equal(t, "completed", status.State)
	assert.Equal(t, int64(5), status.KeysWarmed) // Should stop at 5
}

func TestEmptyPrefix(t *testing.T) {
	log := testLogger(t)
	cache := mustCreateCache(t, log)
	storage := newMockStorageReader()

	config := WarmingConfig{
		BatchSize: 10,
	}

	warmer, err := NewCacheWarmer(cache, storage, config, log)
	require.NoError(t, err)

	ctx := context.Background()

	status, err := warmer.WarmPrefix(ctx, "")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "prefix cannot be empty")
	assert.Nil(t, status)
}

func TestConcurrentStatus(t *testing.T) {
	log := testLogger(t)
	cache := mustCreateCache(t, log)
	storage := newMockStorageReader()

	// Add test data
	for i := 1; i <= 50; i++ {
		storage.AddKey("concurrent:"+string(rune('a'+i%26)), "value")
	}

	config := WarmingConfig{
		BatchSize:          5,
		RateLimitPerSecond: 50,
	}

	warmer, err := NewCacheWarmer(cache, storage, config, log)
	require.NoError(t, err)

	ctx := context.Background()

	var wg sync.WaitGroup

	// Start warming in background
	wg.Add(1)
	go func() {
		defer wg.Done()
		_, _ = warmer.WarmPrefix(ctx, "concurrent:")
	}()

	// Concurrently read status multiple times
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				status := warmer.GetStatus()
				assert.NotNil(t, status)
			}
		}()
	}

	wg.Wait()
}

func TestIncrementByteSlice(t *testing.T) {
	tests := []struct {
		name     string
		input    []byte
		expected []byte
	}{
		{
			name:     "simple increment",
			input:    []byte{0x01, 0x02, 0x03},
			expected: []byte{0x01, 0x02, 0x04},
		},
		{
			name:     "carry over",
			input:    []byte{0x01, 0x02, 0xFF},
			expected: []byte{0x01, 0x03, 0x00},
		},
		{
			name:     "all FF",
			input:    []byte{0xFF, 0xFF, 0xFF},
			expected: []byte{0x00, 0x00, 0x00, 0x00},
		},
		{
			name:     "empty",
			input:    []byte{},
			expected: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := incrementByteSlice(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestWarmPrefixesEmptyList(t *testing.T) {
	log := testLogger(t)
	cache := mustCreateCache(t, log)
	storage := newMockStorageReader()

	config := WarmingConfig{
		BatchSize: 10,
	}

	warmer, err := NewCacheWarmer(cache, storage, config, log)
	require.NoError(t, err)

	ctx := context.Background()

	status, err := warmer.WarmPrefixes(ctx, []string{})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "prefixes cannot be empty")
	assert.Nil(t, status)
}

func TestWarmPrefixesSkipsEmptyPrefixes(t *testing.T) {
	log := testLogger(t)
	cache := mustCreateCache(t, log)
	storage := newMockStorageReader()

	storage.AddKey("valid:1", "value1")
	storage.AddKey("valid:2", "value2")

	config := WarmingConfig{
		BatchSize: 10,
	}

	warmer, err := NewCacheWarmer(cache, storage, config, log)
	require.NoError(t, err)

	ctx := context.Background()

	status, err := warmer.WarmPrefixes(ctx, []string{"", "valid:", ""})
	require.NoError(t, err)
	assert.Equal(t, "completed", status.State)
	assert.Equal(t, int64(2), status.KeysWarmed)
}

func TestStopAfterAlreadyStopped(t *testing.T) {
	log := testLogger(t)
	cache := mustCreateCache(t, log)
	storage := newMockStorageReader()

	config := WarmingConfig{
		BatchSize: 10,
	}

	warmer, err := NewCacheWarmer(cache, storage, config, log)
	require.NoError(t, err)

	// Stop once
	warmer.Stop()

	// Stop again - should be idempotent
	warmer.Stop()

	// Try to warm after stop
	ctx := context.Background()
	status, err := warmer.WarmPrefix(ctx, "test:")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "cache warmer is stopped")
	assert.Nil(t, status)
}

// Helper functions

func mustCreateCache(t *testing.T, log interfaces.Logger) *CacheManager {
	config := Config{
		L1Size:          100,
		L1TTL:           5 * time.Minute,
		CleanupInterval: 1 * time.Minute,
	}

	cache, err := NewManager(config, log)
	require.NoError(t, err)
	return cache
}

func mustCreateLogger(t *testing.T) interfaces.Logger {
	return testLogger(t)
}
