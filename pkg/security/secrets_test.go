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
	assert.Equal(t, 1, secret.Version)
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
	sm.cache.Set("delete/me", &Secret{Key: "delete/me", Value: "gone"})
	err := sm.Delete(ctx, "delete/me")
	require.NoError(t, err)

	// Verify it's gone from the backend and the cache
	_, err = sm.backend.Get(ctx, "delete/me")
	require.Error(t, err)
	assert.Nil(t, sm.cache.Get("delete/me"))
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
	assert.Equal(t, 5*time.Minute, sm.config.CacheTTL, "nil config gets the default cache TTL")
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
// SecretManager
// ---------------------------------------------------------------------------

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
