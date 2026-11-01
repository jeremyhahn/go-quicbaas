//go:build !integration

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

package security

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewSecretManager(t *testing.T) {
	log := createTestLogger(t)

	tests := []struct {
		name    string
		config  *SecretConfig
		wantErr bool
	}{
		{
			name:    "nil config uses defaults",
			config:  nil,
			wantErr: false,
		},
		{
			name:    "empty config",
			config:  &SecretConfig{},
			wantErr: false,
		},
		{
			name: "with caching",
			config: &SecretConfig{
				CacheEnabled: true,
				CacheTTL:     1 * time.Minute,
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sm, err := NewSecretManager(tt.config, log)
			if tt.wantErr {
				assert.Error(t, err)
				assert.Nil(t, sm)
			} else {
				assert.NoError(t, err)
				assert.NotNil(t, sm)
				if sm != nil {
					sm.Close()
				}
			}
		})
	}
}

func TestSecretManager_EnvBackend(t *testing.T) {
	log := createTestLogger(t)

	config := &SecretConfig{}

	sm, err := NewSecretManager(config, log)
	require.NoError(t, err)
	defer sm.Close()

	// Set environment variable
	testKey := "TEST_SECRET_KEY"
	testValue := "secret-value-123"
	os.Setenv(testKey, testValue)
	defer os.Unsetenv(testKey)

	// Get secret
	secret, err := sm.Get(context.Background(), testKey)
	require.NoError(t, err)
	assert.Equal(t, testKey, secret.Key)
	assert.Equal(t, testValue, secret.Value)
}

func TestSecretManager_GetString(t *testing.T) {
	log := createTestLogger(t)

	config := &SecretConfig{}

	sm, err := NewSecretManager(config, log)
	require.NoError(t, err)
	defer sm.Close()

	testKey := "TEST_SECRET_STRING"
	testValue := "string-value"
	os.Setenv(testKey, testValue)
	defer os.Unsetenv(testKey)

	value, err := sm.GetString(context.Background(), testKey)
	require.NoError(t, err)
	assert.Equal(t, testValue, value)
}

func TestSecretManager_GetWithDefault(t *testing.T) {
	log := createTestLogger(t)

	config := &SecretConfig{}

	sm, err := NewSecretManager(config, log)
	require.NoError(t, err)
	defer sm.Close()

	// Non-existent key should return default
	value := sm.GetWithDefault(context.Background(), "NON_EXISTENT_KEY", "default-value")
	assert.Equal(t, "default-value", value)

	// Existing key should return actual value
	testKey := "EXISTING_KEY"
	testValue := "actual-value"
	os.Setenv(testKey, testValue)
	defer os.Unsetenv(testKey)

	value = sm.GetWithDefault(context.Background(), testKey, "default-value")
	assert.Equal(t, testValue, value)
}

func TestSecretManager_CacheHit(t *testing.T) {
	log := createTestLogger(t)

	config := &SecretConfig{
		CacheEnabled: true,
		CacheTTL:     1 * time.Minute,
	}

	sm, err := NewSecretManager(config, log)
	require.NoError(t, err)
	defer sm.Close()

	testKey := "CACHED_SECRET"
	testValue := "cached-value"
	os.Setenv(testKey, testValue)
	defer os.Unsetenv(testKey)

	// First get - cache miss
	secret1, err := sm.Get(context.Background(), testKey)
	require.NoError(t, err)
	assert.Equal(t, testValue, secret1.Value)

	// Second get - should hit cache
	secret2, err := sm.Get(context.Background(), testKey)
	require.NoError(t, err)
	assert.Equal(t, testValue, secret2.Value)
}

func TestSecretManager_CacheExpiration(t *testing.T) {
	log := createTestLogger(t)

	config := &SecretConfig{
		CacheEnabled: true,
		CacheTTL:     100 * time.Millisecond, // Very short TTL for testing
	}

	sm, err := NewSecretManager(config, log)
	require.NoError(t, err)
	defer sm.Close()

	testKey := "EXPIRING_SECRET"
	testValue := "expiring-value"
	os.Setenv(testKey, testValue)
	defer os.Unsetenv(testKey)

	// First get
	secret1, err := sm.Get(context.Background(), testKey)
	require.NoError(t, err)
	assert.Equal(t, testValue, secret1.Value)

	// Wait for cache to expire
	time.Sleep(150 * time.Millisecond)

	// Should fetch from backend again
	secret2, err := sm.Get(context.Background(), testKey)
	require.NoError(t, err)
	assert.Equal(t, testValue, secret2.Value)
}

func TestSecretCache_SetGet(t *testing.T) {
	cache := NewSecretCache(1 * time.Minute)

	secret := &Secret{
		Key:       "test-key",
		Value:     "test-value",
		Version:   1,
		CreatedAt: time.Now(),
	}

	cache.Set("test-key", secret)

	retrieved := cache.Get("test-key")
	require.NotNil(t, retrieved)
	assert.Equal(t, secret.Key, retrieved.Key)
	assert.Equal(t, secret.Value, retrieved.Value)
}

func TestSecretCache_Expiration(t *testing.T) {
	cache := NewSecretCache(100 * time.Millisecond)

	secret := &Secret{
		Key:       "test-key",
		Value:     "test-value",
		Version:   1,
		CreatedAt: time.Now(),
	}

	cache.Set("test-key", secret)

	// Should exist immediately
	assert.NotNil(t, cache.Get("test-key"))

	// Wait for expiration
	time.Sleep(150 * time.Millisecond)

	// Should be expired
	assert.Nil(t, cache.Get("test-key"))
}

func TestSecretCache_Delete(t *testing.T) {
	cache := NewSecretCache(1 * time.Minute)

	secret := &Secret{
		Key:       "test-key",
		Value:     "test-value",
		Version:   1,
		CreatedAt: time.Now(),
	}

	cache.Set("test-key", secret)
	assert.NotNil(t, cache.Get("test-key"))

	cache.Delete("test-key")
	assert.Nil(t, cache.Get("test-key"))
}

func TestSecretCache_Cleanup(t *testing.T) {
	cache := NewSecretCache(50 * time.Millisecond)

	// Add multiple secrets
	for i := 0; i < 10; i++ {
		secret := &Secret{
			Key:       string(rune('a' + i)),
			Value:     "value",
			Version:   1,
			CreatedAt: time.Now(),
		}
		cache.Set(secret.Key, secret)
	}

	// All should exist
	for i := 0; i < 10; i++ {
		assert.NotNil(t, cache.Get(string(rune('a'+i))))
	}

	// Wait for expiration
	time.Sleep(100 * time.Millisecond)

	// Trigger cleanup
	cache.cleanup()

	// All should be removed
	for i := 0; i < 10; i++ {
		assert.Nil(t, cache.Get(string(rune('a'+i))))
	}
}

func TestEnvBackend_Get(t *testing.T) {
	log := createTestLogger(t)

	backend, err := NewEnvBackend(log)
	require.NoError(t, err)
	defer backend.Close()

	testKey := "TEST_ENV_BACKEND"
	testValue := "backend-value"
	os.Setenv(testKey, testValue)
	defer os.Unsetenv(testKey)

	secret, err := backend.Get(context.Background(), testKey)
	require.NoError(t, err)
	assert.Equal(t, testKey, secret.Key)
	assert.Equal(t, testValue, secret.Value)
}

func TestEnvBackend_GetNotFound(t *testing.T) {
	log := createTestLogger(t)

	backend, err := NewEnvBackend(log)
	require.NoError(t, err)
	defer backend.Close()

	_, err = backend.Get(context.Background(), "NON_EXISTENT_VAR")
	assert.Error(t, err)
}

func TestEnvBackend_SetNotSupported(t *testing.T) {
	log := createTestLogger(t)

	backend, err := NewEnvBackend(log)
	require.NoError(t, err)
	defer backend.Close()

	secret := &Secret{
		Key:   "test",
		Value: "value",
	}

	err = backend.Set(context.Background(), "test", secret)
	assert.Error(t, err)
}

func TestEnvBackend_DeleteNotSupported(t *testing.T) {
	log := createTestLogger(t)

	backend, err := NewEnvBackend(log)
	require.NoError(t, err)
	defer backend.Close()

	err = backend.Delete(context.Background(), "test")
	assert.Error(t, err)
}

func TestEnvBackend_ListNotSupported(t *testing.T) {
	log := createTestLogger(t)

	backend, err := NewEnvBackend(log)
	require.NoError(t, err)
	defer backend.Close()

	_, err = backend.List(context.Background(), "test")
	assert.Error(t, err)
}

func TestSecretManager_ConcurrentAccess(t *testing.T) {
	log := createTestLogger(t)

	config := &SecretConfig{
		CacheEnabled: true,
	}

	sm, err := NewSecretManager(config, log)
	require.NoError(t, err)
	defer sm.Close()

	testKey := "CONCURRENT_SECRET"
	testValue := "concurrent-value"
	os.Setenv(testKey, testValue)
	defer os.Unsetenv(testKey)

	const numGoroutines = 10
	const numOperations = 100

	done := make(chan bool, numGoroutines)

	for i := 0; i < numGoroutines; i++ {
		go func() {
			for j := 0; j < numOperations; j++ {
				secret, err := sm.Get(context.Background(), testKey)
				assert.NoError(t, err)
				assert.Equal(t, testValue, secret.Value)
			}
			done <- true
		}()
	}

	// Wait for all goroutines
	for i := 0; i < numGoroutines; i++ {
		<-done
	}
}

func TestSecret_Metadata(t *testing.T) {
	secret := &Secret{
		Key:   "test-key",
		Value: "test-value",
		Metadata: map[string]string{
			"type":        "database-password",
			"environment": "production",
		},
		Version:   1,
		CreatedAt: time.Now(),
	}

	assert.Equal(t, "test-key", secret.Key)
	assert.Equal(t, "test-value", secret.Value)
	assert.Equal(t, "database-password", secret.Metadata["type"])
	assert.Equal(t, "production", secret.Metadata["environment"])
}

func TestSecret_WithExpiration(t *testing.T) {
	expiresAt := time.Now().Add(1 * time.Hour)
	secret := &Secret{
		Key:       "expiring-key",
		Value:     "expiring-value",
		Version:   1,
		CreatedAt: time.Now(),
		ExpiresAt: &expiresAt,
	}

	assert.NotNil(t, secret.ExpiresAt)
	assert.True(t, time.Now().Before(*secret.ExpiresAt))
}

func BenchmarkSecretManager_Get(b *testing.B) {
	log := createTestLogger(b)

	config := &SecretConfig{
		CacheEnabled: false,
	}

	sm, _ := NewSecretManager(config, log)
	defer sm.Close()

	testKey := "BENCHMARK_SECRET"
	os.Setenv(testKey, "benchmark-value")
	defer os.Unsetenv(testKey)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		sm.Get(context.Background(), testKey)
	}
}

func BenchmarkSecretManager_GetWithCache(b *testing.B) {
	log := createTestLogger(b)

	config := &SecretConfig{
		CacheEnabled: true,
		CacheTTL:     10 * time.Minute,
	}

	sm, _ := NewSecretManager(config, log)
	defer sm.Close()

	testKey := "BENCHMARK_CACHED_SECRET"
	os.Setenv(testKey, "benchmark-value")
	defer os.Unsetenv(testKey)

	// Prime the cache
	sm.Get(context.Background(), testKey)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		sm.Get(context.Background(), testKey)
	}
}

func BenchmarkSecretCache_Get(b *testing.B) {
	cache := NewSecretCache(10 * time.Minute)

	secret := &Secret{
		Key:       "benchmark-key",
		Value:     "benchmark-value",
		Version:   1,
		CreatedAt: time.Now(),
	}

	cache.Set("benchmark-key", secret)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		cache.Get("benchmark-key")
	}
}

// TestSecretCache_AdditionalCases tests more cache scenarios
func TestSecretCache_AdditionalCases(t *testing.T) {
	t.Run("startCleanup goroutine", func(t *testing.T) {
		cache := NewSecretCache(50 * time.Millisecond)

		secret := &Secret{
			Key:   "test",
			Value: "value",
		}

		cache.Set("test1", secret)
		cache.Set("test2", secret)

		// Wait for expiration and cleanup
		time.Sleep(100 * time.Millisecond)

		// Items should eventually expire
		cache.cleanup()

		assert.Nil(t, cache.Get("test1"))
		assert.Nil(t, cache.Get("test2"))

		cache.Close()
	})
}

// TestSecretCache_CleanupExpiredEntries tests cleanup of expired entries
func TestSecretCache_CleanupExpiredEntries(t *testing.T) {
	cache := NewSecretCache(50 * time.Millisecond)

	// Add entries
	cache.Set("key1", &Secret{Key: "key1", Value: "value1"})
	cache.Set("key2", &Secret{Key: "key2", Value: "value2"})

	// Wait for expiration
	time.Sleep(100 * time.Millisecond)

	// Manually call cleanup
	cache.cleanup()

	// Both should be removed
	assert.Nil(t, cache.Get("key1"))
	assert.Nil(t, cache.Get("key2"))
}

// ---------------------------------------------------------------------------
// SecretManager.GetString error (secrets.go:313 -- 75%)
// ---------------------------------------------------------------------------

func TestSecretManager_GetStringNotFound(t *testing.T) {
	log := createTestLogger(t)

	sm, err := NewSecretManager(&SecretConfig{}, log)
	require.NoError(t, err)
	defer sm.Close()

	_, err = sm.GetString(context.Background(), "NONEXISTENT_VAR_12345")
	require.Error(t, err)
}

// ---------------------------------------------------------------------------
// SecretManager with a mock backend that supports Set/Delete
// ---------------------------------------------------------------------------

// mockSecretBackend implements SecretBackend for testing.
type mockSecretBackend struct {
	secrets map[string]*Secret
}

func newMockSecretBackend() *mockSecretBackend {
	return &mockSecretBackend{
		secrets: make(map[string]*Secret),
	}
}

// ---------------------------------------------------------------------------
// SecretManager.GetString with mock backend
// ---------------------------------------------------------------------------

func TestSecretManager_GetStringFromMockBackend(t *testing.T) {
	log := createTestLogger(t)

	mockBE := newMockSecretBackend()
	mockBE.secrets["key"] = &Secret{Key: "key", Value: "my-secret"}

	sm := &SecretManager{
		backend: mockBE,
		config:  &SecretConfig{},
		logger:  log,
	}
	defer sm.Close()

	val, err := sm.GetString(context.Background(), "key")
	require.NoError(t, err)
	assert.Equal(t, "my-secret", val)
}

// ---------------------------------------------------------------------------
// NewSecretManager defaults (secrets.go:85 -- 84.6%)
// ---------------------------------------------------------------------------

func TestNewSecretManager_WithCache(t *testing.T) {
	log := createTestLogger(t)

	sm, err := NewSecretManager(&SecretConfig{
		CacheEnabled: true,
		CacheTTL:     10 * time.Minute,
	}, log)
	require.NoError(t, err)
	defer sm.Close()
	assert.NotNil(t, sm.cache)
}

func TestNewSecretManager_NilConfig(t *testing.T) {
	log := createTestLogger(t)

	sm, err := NewSecretManager(nil, log)
	require.NoError(t, err)
	defer sm.Close()
}

// ---------------------------------------------------------------------------
// SecretCache.startCleanup coverage (secrets.go:282 -- 75%)
// ---------------------------------------------------------------------------

func TestSecretCache_SetAndGet(t *testing.T) {
	cache := &SecretCache{
		entries: make(map[string]*cachedSecret),
		ttl:     5 * time.Minute,
	}

	secret := &Secret{Key: "k", Value: "v"}
	cache.Set("k", secret)

	got := cache.Get("k")
	require.NotNil(t, got)
	assert.Equal(t, "v", got.Value)
}

func TestSecretCache_GetExpired(t *testing.T) {
	cache := &SecretCache{
		entries: make(map[string]*cachedSecret),
		ttl:     1 * time.Nanosecond, // Expire immediately
	}

	secret := &Secret{Key: "k", Value: "v"}
	cache.Set("k", secret)

	// The entry is already expired
	got := cache.Get("k")
	assert.Nil(t, got)
}

// ---------------------------------------------------------------------------
// NewSecretManager - cover more config paths
// ---------------------------------------------------------------------------

func TestNewSecretManager_DefaultBackend(t *testing.T) {
	cfg := &SecretConfig{
		CacheTTL: 0, // no cache
	}

	log := createTestLogger(t)

	mgr, err := NewSecretManager(cfg, log)
	assert.NoError(t, err)
	assert.NotNil(t, mgr)
}

// TestSecretCache_Expiry tests cache expiration.
func TestSecretCache_Expiry(t *testing.T) {
	cache := NewSecretCache(100 * time.Millisecond)

	secret := &Secret{
		Key:   "test",
		Value: "value",
	}

	cache.Set("test", secret)

	// Should be available immediately
	retrieved := cache.Get("test")
	assert.NotNil(t, retrieved)

	// Wait for expiration
	time.Sleep(150 * time.Millisecond)

	// Should be expired
	retrieved = cache.Get("test")
	assert.Nil(t, retrieved)
}

// TestSecretCache_StartCleanup_NonBlocking tests cache cleanup doesn't block.
func TestSecretCache_StartCleanup_NonBlocking(t *testing.T) {
	cache := NewSecretCache(50 * time.Millisecond)

	secret := &Secret{
		Key:   "test",
		Value: "value",
	}

	cache.Set("test1", secret)
	cache.Set("test2", secret)

	// Manually run cleanup after expiration
	time.Sleep(100 * time.Millisecond)
	cache.cleanup()

	// Both should be cleaned up
	assert.Nil(t, cache.Get("test1"))
	assert.Nil(t, cache.Get("test2"))
}

// TestSecretManager_NilConfig tests nil config uses defaults.
func TestSecretManager_NilConfig(t *testing.T) {
	log := createTestLogger(t)

	sm, err := NewSecretManager(nil, log)
	assert.NoError(t, err)
	assert.NotNil(t, sm)
	defer sm.Close()
}

// TestSecretCache_StartCleanup_Channel tests cleanup with channel.
func TestSecretCache_StartCleanup_Channel(t *testing.T) {
	cache := NewSecretCache(100 * time.Millisecond)

	secret := &Secret{
		Key:   "test",
		Value: "value",
	}

	cache.Set("test1", secret)

	// Start cleanup goroutine
	done := make(chan bool)
	go func() {
		// Run cleanup for a short time
		time.Sleep(150 * time.Millisecond)
		done <- true
	}()

	<-done

	// Secret should be cleaned up by now
	retrieved := cache.Get("test1")
	assert.Nil(t, retrieved)
}

// ---------------------------------------------------------------------------
// secrets.go -- SecretManager Set, Delete, GetString
// ---------------------------------------------------------------------------

func TestSecretManager_GetStringMissing(t *testing.T) {
	cfg := &SecretConfig{}
	log := createTestLogger(t)
	sm, err := NewSecretManager(cfg, log)
	require.NoError(t, err)
	defer sm.Close()

	ctx := context.Background()
	_, err = sm.GetString(ctx, "NONEXISTENT_GET_STRING_KEY_99999")
	require.Error(t, err)
}

func TestSecretManager_GetError(t *testing.T) {
	cfg := &SecretConfig{}
	log := createTestLogger(t)
	sm, err := NewSecretManager(cfg, log)
	require.NoError(t, err)
	defer sm.Close()

	ctx := context.Background()
	_, err = sm.Get(ctx, "NONEXISTENT_KEY_FOR_GET_ERR_12345")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "get secret NONEXISTENT_KEY_FOR_GET_ERR_12345 failed")
}

// ---------------------------------------------------------------------------
// secrets.go -- SecretCache close
// ---------------------------------------------------------------------------

func TestSecretCache_CloseEmptiesEntries(t *testing.T) {
	cache := NewSecretCache(5 * time.Minute)
	cache.Set("a", &Secret{Key: "a", Value: "1"})
	cache.Set("b", &Secret{Key: "b", Value: "2"})

	assert.NotNil(t, cache.Get("a"))
	assert.NotNil(t, cache.Get("b"))

	cache.Close()

	assert.Nil(t, cache.Get("a"))
	assert.Nil(t, cache.Get("b"))
}

// ---------------------------------------------------------------------------
// EnvBackend tests
// ---------------------------------------------------------------------------

func TestEnvBackend_GetReturnsSecret(t *testing.T) {
	log := createTestLogger(t)
	backend, err := NewEnvBackend(log)
	require.NoError(t, err)

	t.Setenv("TEST_ENV_BACKEND_GET", "secret-value")

	ctx := context.Background()
	secret, err := backend.Get(ctx, "TEST_ENV_BACKEND_GET")
	require.NoError(t, err)
	assert.Equal(t, "secret-value", secret.Value)
	assert.Equal(t, "TEST_ENV_BACKEND_GET", secret.Key)
	assert.Equal(t, 1, secret.Version)
}

// Tests for SecretManager with cache

// ---------------------------------------------------------------------------
// SecretManager
// ---------------------------------------------------------------------------

func TestSecretManager_GetPopulatesCache(t *testing.T) {
	cfg := &SecretConfig{
		CacheEnabled: true,
		CacheTTL:     5 * time.Minute,
	}

	log := createTestLogger(t)
	sm, err := NewSecretManager(cfg, log)
	require.NoError(t, err)
	defer sm.Close()

	t.Setenv("TEST_SECRET_CACHE_HIT_CT", "cached-value")

	ctx := context.Background()

	secret, err := sm.Get(ctx, "TEST_SECRET_CACHE_HIT_CT")
	require.NoError(t, err)
	assert.Equal(t, "cached-value", secret.Value)

	secret2, err := sm.Get(ctx, "TEST_SECRET_CACHE_HIT_CT")
	require.NoError(t, err)
	assert.Equal(t, "cached-value", secret2.Value)
}

func TestSecretManager_GetWithDefaultReturnsDefault(t *testing.T) {
	cfg := &SecretConfig{}
	log := createTestLogger(t)

	sm, err := NewSecretManager(cfg, log)
	require.NoError(t, err)
	defer sm.Close()

	ctx := context.Background()

	t.Run("returns default", func(t *testing.T) {
		val := sm.GetWithDefault(ctx, "NONEXISTENT_VAR_CT_999", "fallback")
		assert.Equal(t, "fallback", val)
	})

	t.Run("returns value", func(t *testing.T) {
		t.Setenv("EXISTING_TEST_VAR_CT", "real-value")
		val := sm.GetWithDefault(ctx, "EXISTING_TEST_VAR_CT", "fallback")
		assert.Equal(t, "real-value", val)
	})
}

func TestSecretCache_DeleteRemovesEntry(t *testing.T) {
	cache := NewSecretCache(5 * time.Minute)

	cache.Set("key1", &Secret{Key: "key1", Value: "v1"})
	cache.Set("key2", &Secret{Key: "key2", Value: "v2"})

	got := cache.Get("key1")
	require.NotNil(t, got)
	assert.Equal(t, "v1", got.Value)

	cache.Delete("key1")

	got = cache.Get("key1")
	assert.Nil(t, got)

	got = cache.Get("key2")
	require.NotNil(t, got)
	assert.Equal(t, "v2", got.Value)
}

func TestSecretCache_CleanupRemovesExpired(t *testing.T) {
	cache := NewSecretCache(1 * time.Millisecond)

	cache.Set("expired", &Secret{Key: "expired", Value: "old"})

	cache.mu.Lock()
	for _, entry := range cache.entries {
		entry.expiresAt = time.Now().Add(-1 * time.Second)
	}
	cache.mu.Unlock()

	cache.cleanup()

	got := cache.Get("expired")
	assert.Nil(t, got)
}

func TestSecretCache_GetReturnsNilForExpired(t *testing.T) {
	cache := NewSecretCache(1 * time.Millisecond)
	cache.Set("key", &Secret{Key: "key", Value: "val"})

	cache.mu.Lock()
	cache.entries["key"].expiresAt = time.Now().Add(-1 * time.Second)
	cache.mu.Unlock()

	got := cache.Get("key")
	assert.Nil(t, got)
}

// TestSecretManager_Set tests setting secrets.
// newSecretManagerForWrite builds a SecretManager for the Set and Delete
// tables. With backendOK the backend is a mock that accepts writes and
// already holds path; otherwise it is the EnvBackend, which rejects writes.
// With cached the cache holds an entry for path. The returned mock is nil
// for the EnvBackend.
func newSecretManagerForWrite(t *testing.T, cached, backendOK bool, path string) (*mockSecretBackend, *SecretManager) {
	t.Helper()
	log := createTestLogger(t)

	sm := &SecretManager{config: &SecretConfig{CacheEnabled: cached}, logger: log}
	var mock *mockSecretBackend
	if backendOK {
		mock = newMockSecretBackend()
		mock.secrets[path] = &Secret{Key: path, Value: "old-value"}
		sm.backend = mock
	} else {
		backend, err := NewEnvBackend(log)
		require.NoError(t, err)
		sm.backend = backend
	}
	if cached {
		sm.cache = &SecretCache{entries: make(map[string]*cachedSecret), ttl: 5 * time.Minute}
		sm.cache.Set(path, &Secret{Key: path, Value: "old-value"})
	}
	t.Cleanup(func() { require.NoError(t, sm.Close()) })
	return mock, sm
}

// secretWriteCases are the cache mode and backend result rows shared by the
// Set and Delete tables.
var secretWriteCases = []struct {
	name      string
	cached    bool
	backendOK bool
}{
	{name: "nil cache, backend success", backendOK: true},
	{name: "cache enabled, backend success", cached: true, backendOK: true},
	{name: "nil cache, backend error"},
	{name: "cache enabled, backend error", cached: true},
}

// TestSecretManager_Set checks that a successful Set stores the secret and
// drops the cached copy, and that a failed Set returns the wrapped backend
// error and leaves the cache untouched.
func TestSecretManager_Set(t *testing.T) {
	const path = "set/path"
	metadata := map[string]string{"tier": "production"}

	for _, tt := range secretWriteCases {
		t.Run(tt.name, func(t *testing.T) {
			mock, sm := newSecretManagerForWrite(t, tt.cached, tt.backendOK, path)

			err := sm.Set(context.Background(), path, "new-value", metadata)

			if tt.backendOK {
				require.NoError(t, err)
				stored := mock.secrets[path]
				require.NotNil(t, stored)
				assert.Equal(t, "new-value", stored.Value)
				assert.Equal(t, metadata, stored.Metadata)
				assert.Equal(t, 1, stored.Version)
			} else {
				require.Error(t, err)
				assert.Contains(t, err.Error(), "set secret "+path+" failed")
				assert.Contains(t, err.Error(), "not supported")
			}
			if tt.cached {
				assert.Equal(t, !tt.backendOK, sm.cache.Get(path) != nil,
					"the cache entry survives only a failed Set")
			}
		})
	}
}

// TestSecretManager_Delete checks that a successful Delete removes the
// secret from the backend and the cache, and that a failed Delete returns
// the wrapped backend error and leaves the cache untouched.
func TestSecretManager_Delete(t *testing.T) {
	const path = "delete/path"

	for _, tt := range secretWriteCases {
		t.Run(tt.name, func(t *testing.T) {
			mock, sm := newSecretManagerForWrite(t, tt.cached, tt.backendOK, path)

			err := sm.Delete(context.Background(), path)

			if tt.backendOK {
				require.NoError(t, err)
				assert.NotContains(t, mock.secrets, path)
			} else {
				require.Error(t, err)
				assert.Contains(t, err.Error(), "delete secret "+path+" failed")
				assert.Contains(t, err.Error(), "not supported")
			}
			if tt.cached {
				assert.Equal(t, !tt.backendOK, sm.cache.Get(path) != nil,
					"the cache entry survives only a failed Delete")
			}
		})
	}
}

// TestSecretManager_GetStringError tests GetString with error.
func TestSecretManager_GetStringError(t *testing.T) {
	cfg := &SecretConfig{}

	log := createTestLogger(t)
	sm, err := NewSecretManager(cfg, log)
	require.NoError(t, err)
	defer sm.Close()

	ctx := context.Background()

	// Try to get non-existent env var
	_, err = sm.GetString(ctx, "NONEXISTENT_SECRET_VAR_12345")
	assert.Error(t, err)
}

// TestSecretCache_StartCleanup tests cache cleanup.
func TestSecretCache_StartCleanup(t *testing.T) {
	cache := NewSecretCache(50 * time.Millisecond)

	// Add some secrets
	cache.Set("key1", &Secret{
		Key:   "key1",
		Value: "value1",
	})
	cache.Set("key2", &Secret{
		Key:   "key2",
		Value: "value2",
	})

	// Start cleanup in background
	go cache.startCleanup()

	// Wait for secrets to expire and cleanup to run
	time.Sleep(200 * time.Millisecond)

	// Secrets should be expired and cleaned up
	secret := cache.Get("key1")
	assert.Nil(t, secret)
}
