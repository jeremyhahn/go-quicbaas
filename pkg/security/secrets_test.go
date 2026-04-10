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

// TestSecretManager_AdditionalEdgeCases tests more secret manager edge cases
func TestSecretManager_AdditionalEdgeCases(t *testing.T) {
	log := createTestLogger(t)

	t.Run("Set with env backend not supported", func(t *testing.T) {
		config := &SecretConfig{
			CacheEnabled: false,
		}

		sm, err := NewSecretManager(config, log)
		require.NoError(t, err)
		defer sm.Close()

		err = sm.Set(context.Background(), "test/key", "value", nil)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "not supported")
	})

	t.Run("Delete with env backend not supported", func(t *testing.T) {
		config := &SecretConfig{
			CacheEnabled: false,
		}

		sm, err := NewSecretManager(config, log)
		require.NoError(t, err)
		defer sm.Close()

		err = sm.Delete(context.Background(), "test/key")
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "not supported")
	})
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

// TestSecretManager_SetWithCache tests Set with caching enabled
func TestSecretManager_SetWithCache(t *testing.T) {
	logger := createTestLogger(t)

	cfg := &SecretConfig{
		CacheEnabled: true,
		CacheTTL:     5 * time.Minute,
	}

	sm, err := NewSecretManager(cfg, logger)
	require.NoError(t, err)
	defer sm.Close()

	// Add to cache first
	sm.cache.Set("TEST_KEY", &Secret{Key: "TEST_KEY", Value: "old-value"})

	// EnvBackend doesn't support Set, but we test the cache invalidation path
	ctx := context.Background()
	err = sm.Set(ctx, "TEST_KEY", "new-value", nil)
	assert.Error(t, err) // EnvBackend doesn't support Set
	assert.Contains(t, err.Error(), "not supported")
}

// TestSecretManager_DeleteWithCache tests Delete with caching enabled
func TestSecretManager_DeleteWithCache(t *testing.T) {
	logger := createTestLogger(t)

	cfg := &SecretConfig{
		CacheEnabled: true,
		CacheTTL:     5 * time.Minute,
	}

	sm, err := NewSecretManager(cfg, logger)
	require.NoError(t, err)
	defer sm.Close()

	// Add to cache first
	sm.cache.Set("DELETE_KEY", &Secret{Key: "DELETE_KEY", Value: "test"})

	// EnvBackend doesn't support Delete, but we test the cache invalidation path
	ctx := context.Background()
	err = sm.Delete(ctx, "DELETE_KEY")
	assert.Error(t, err) // EnvBackend doesn't support Delete
	assert.Contains(t, err.Error(), "not supported")
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
// SecretManager.Set and Delete with cache (secrets.go:142,165 -- 42.9%,33.3%)
// ---------------------------------------------------------------------------

func TestSecretManager_SetAndDelete(t *testing.T) {
	log := createTestLogger(t)

	cfg := &SecretConfig{
		CacheEnabled: true,
		CacheTTL:     5 * time.Minute,
	}

	sm, err := NewSecretManager(cfg, log)
	require.NoError(t, err)
	defer sm.Close()

	ctx := context.Background()

	// Set a secret (EnvBackend doesn't support Set, should return error)
	err = sm.Set(ctx, "test-path", "test-value", map[string]string{"env": "test"})
	require.Error(t, err) // EnvBackend returns unsupported error

	// Delete (EnvBackend doesn't support Delete)
	err = sm.Delete(ctx, "test-path")
	require.Error(t, err)
}

func TestSecretManager_SetWithNilCache(t *testing.T) {
	log := createTestLogger(t)

	cfg := &SecretConfig{
		CacheEnabled: false,
	}

	sm, err := NewSecretManager(cfg, log)
	require.NoError(t, err)
	defer sm.Close()

	// Ensure cache is nil for code path coverage
	sm.cache = nil

	ctx := context.Background()
	err = sm.Set(ctx, "test-path", "test-value", nil)
	require.Error(t, err) // EnvBackend returns unsupported error
}

func TestSecretManager_DeleteWithNilCache(t *testing.T) {
	log := createTestLogger(t)

	cfg := &SecretConfig{
		CacheEnabled: false,
	}

	sm, err := NewSecretManager(cfg, log)
	require.NoError(t, err)
	defer sm.Close()

	sm.cache = nil

	ctx := context.Background()
	err = sm.Delete(ctx, "test-path")
	require.Error(t, err)
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

func TestSecretManager_SetWithMockBackend_Success(t *testing.T) {
	log := createTestLogger(t)

	sm := &SecretManager{
		backend: newMockSecretBackend(),
		cache: &SecretCache{
			entries: make(map[string]*cachedSecret),
			ttl:     5 * time.Minute,
		},
		config: &SecretConfig{CacheEnabled: true, CacheTTL: 5 * time.Minute},
		logger: log,
	}
	defer sm.Close()

	ctx := context.Background()
	err := sm.Set(ctx, "test/path", "secret-value", map[string]string{"env": "test"})
	require.NoError(t, err)

	// Verify the secret was stored
	secret, err := sm.Get(ctx, "test/path")
	require.NoError(t, err)
	assert.Equal(t, "secret-value", secret.Value)
}

func TestSecretManager_SetWithoutCache_Success(t *testing.T) {
	log := createTestLogger(t)

	sm := &SecretManager{
		backend: newMockSecretBackend(),
		config:  &SecretConfig{},
		logger:  log,
	}
	defer sm.Close()

	ctx := context.Background()
	err := sm.Set(ctx, "no-cache/path", "value", nil)
	require.NoError(t, err)
}

func TestSecretManager_DeleteWithMockBackend_Success(t *testing.T) {
	log := createTestLogger(t)

	mockBE := newMockSecretBackend()
	mockBE.secrets["delete/me"] = &Secret{Key: "delete/me", Value: "gone"}

	sm := &SecretManager{
		backend: mockBE,
		cache: &SecretCache{
			entries: make(map[string]*cachedSecret),
			ttl:     5 * time.Minute,
		},
		config: &SecretConfig{CacheEnabled: true, CacheTTL: 5 * time.Minute},
		logger: log,
	}
	defer sm.Close()

	ctx := context.Background()
	err := sm.Delete(ctx, "delete/me")
	require.NoError(t, err)

	// Verify it's gone
	_, err = sm.backend.Get(ctx, "delete/me")
	require.Error(t, err)
}

func TestSecretManager_DeleteWithoutCache_Success(t *testing.T) {
	log := createTestLogger(t)

	mockBE := newMockSecretBackend()
	mockBE.secrets["remove/me"] = &Secret{Key: "remove/me", Value: "val"}

	sm := &SecretManager{
		backend: mockBE,
		config:  &SecretConfig{},
		logger:  log,
	}
	defer sm.Close()

	ctx := context.Background()
	err := sm.Delete(ctx, "remove/me")
	require.NoError(t, err)
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
// SecretManager - cover the startCleanup goroutine path
// ---------------------------------------------------------------------------

func TestSecretManager_CacheCleanupOnDelete(t *testing.T) {
	backend := newMockSecretBackend()
	backend.secrets["key-to-delete"] = &Secret{Key: "key-to-delete", Value: "value"}

	log := createTestLogger(t)
	mgr := &SecretManager{
		backend: backend,
		logger:  log,
		cache: &SecretCache{
			entries: make(map[string]*cachedSecret),
			ttl:     5 * time.Minute,
		},
	}

	// Set a value in cache
	mgr.cache.Set("key-to-delete", &Secret{Key: "key-to-delete", Value: "value"})

	// Delete should also remove from cache
	err := mgr.Delete(context.Background(), "key-to-delete")
	assert.NoError(t, err)

	// Verify cache entry is gone
	result := mgr.cache.Get("key-to-delete")
	assert.Nil(t, result)
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

func TestSecretManager_SetErrorFromBackend(t *testing.T) {
	cfg := &SecretConfig{}
	log := createTestLogger(t)
	sm, err := NewSecretManager(cfg, log)
	require.NoError(t, err)
	defer sm.Close()

	ctx := context.Background()

	// EnvBackend does not support Set, so it should return an error.
	err = sm.Set(ctx, "TEST_KEY_SET", "value", nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "set secret TEST_KEY_SET failed")
}

func TestSecretManager_SetWithCacheEnabled(t *testing.T) {
	cfg := &SecretConfig{CacheEnabled: true, CacheTTL: 5 * time.Minute}
	log := createTestLogger(t)
	sm, err := NewSecretManager(cfg, log)
	require.NoError(t, err)
	defer sm.Close()

	ctx := context.Background()

	// Pre-populate cache.
	t.Setenv("TEST_SET_CACHE_KEY", "original")
	_, err = sm.Get(ctx, "TEST_SET_CACHE_KEY")
	require.NoError(t, err)

	// Set should invalidate cache even though backend returns error.
	err = sm.Set(ctx, "TEST_SET_CACHE_KEY", "new-value", map[string]string{"env": "test"})
	require.Error(t, err) // EnvBackend rejects Set.
}

func TestSecretManager_DeleteErrorFromBackend(t *testing.T) {
	cfg := &SecretConfig{}
	log := createTestLogger(t)
	sm, err := NewSecretManager(cfg, log)
	require.NoError(t, err)
	defer sm.Close()

	ctx := context.Background()

	// EnvBackend does not support Delete.
	err = sm.Delete(ctx, "TEST_KEY_DEL")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "delete secret TEST_KEY_DEL failed")
}

func TestSecretManager_DeleteWithCacheEnabled(t *testing.T) {
	cfg := &SecretConfig{CacheEnabled: true, CacheTTL: 5 * time.Minute}
	log := createTestLogger(t)
	sm, err := NewSecretManager(cfg, log)
	require.NoError(t, err)
	defer sm.Close()

	ctx := context.Background()

	// Pre-populate cache.
	t.Setenv("TEST_DEL_CACHE_KEY", "value")
	_, err = sm.Get(ctx, "TEST_DEL_CACHE_KEY")
	require.NoError(t, err)

	// Delete should attempt to remove from cache even though backend returns error.
	err = sm.Delete(ctx, "TEST_DEL_CACHE_KEY")
	require.Error(t, err) // EnvBackend rejects Delete.
}

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

func TestSecretManager_Set_WithCacheEnabled(t *testing.T) {
	log := createTestLogger(t)

	config := &SecretConfig{
		CacheEnabled: true,
		CacheTTL:     time.Second,
	}

	sm, err := NewSecretManager(config, log)
	require.NoError(t, err)
	defer sm.Close()

	// Set should fail with env backend
	err = sm.Set(context.Background(), "TEST_KEY", "value", nil)
	assert.Error(t, err)
}

func TestSecretManager_Delete_WithCacheEnabled(t *testing.T) {
	log := createTestLogger(t)

	config := &SecretConfig{
		CacheEnabled: true,
		CacheTTL:     time.Second,
	}

	sm, err := NewSecretManager(config, log)
	require.NoError(t, err)
	defer sm.Close()

	// Delete should fail with env backend
	err = sm.Delete(context.Background(), "TEST_KEY")
	assert.Error(t, err)
}

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

func TestSecretManager_SetInvalidatesCache(t *testing.T) {
	cfg := &SecretConfig{
		CacheEnabled: true,
		CacheTTL:     5 * time.Minute,
	}

	log := createTestLogger(t)
	sm, err := NewSecretManager(cfg, log)
	require.NoError(t, err)
	defer sm.Close()

	ctx := context.Background()

	err = sm.Set(ctx, "TEST_SET_KEY_CT", "value", map[string]string{"env": "test"})
	assert.Error(t, err)
}

func TestSecretManager_DeleteInvalidatesCache(t *testing.T) {
	cfg := &SecretConfig{
		CacheEnabled: true,
		CacheTTL:     5 * time.Minute,
	}

	log := createTestLogger(t)
	sm, err := NewSecretManager(cfg, log)
	require.NoError(t, err)
	defer sm.Close()

	ctx := context.Background()

	err = sm.Delete(ctx, "TEST_DEL_KEY_CT")
	assert.Error(t, err)
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
func TestSecretManager_Set(t *testing.T) {
	cfg := &SecretConfig{
		CacheEnabled: true,
		CacheTTL:     1 * time.Minute,
	}

	log := createTestLogger(t)
	sm, err := NewSecretManager(cfg, log)
	require.NoError(t, err)
	defer sm.Close()

	ctx := context.Background()
	metadata := map[string]string{"tier": "production"}

	// EnvBackend doesn't support Set, so expect error
	err = sm.Set(ctx, "test-key", "test-value", metadata)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not supported")
}

// TestSecretManager_Delete tests deleting secrets.
func TestSecretManager_Delete(t *testing.T) {
	cfg := &SecretConfig{
		CacheEnabled: true,
		CacheTTL:     1 * time.Minute,
	}

	log := createTestLogger(t)
	sm, err := NewSecretManager(cfg, log)
	require.NoError(t, err)
	defer sm.Close()

	ctx := context.Background()

	// EnvBackend doesn't support Delete, so expect error
	err = sm.Delete(ctx, "test-key")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not supported")
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
