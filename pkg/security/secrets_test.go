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

// TestSecretCache_Get covers each Get path: a live entry, a missing key and
// an entry past its expiry.
func TestSecretCache_Get(t *testing.T) {
	tests := []struct {
		name    string
		key     string
		expired bool
		want    *Secret
	}{
		{name: "live entry", key: "k", want: &Secret{Key: "k", Value: "v"}},
		{name: "missing key", key: "other"},
		{name: "expired entry", key: "k", expired: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cache := NewSecretCache(5 * time.Minute)
			cache.Set("k", &Secret{Key: "k", Value: "v"})
			if tt.expired {
				cache.entries["k"].expiresAt = time.Now().Add(-time.Second)
			}

			assert.Equal(t, tt.want, cache.Get(tt.key))
		})
	}
}

// TestSecretCache_Cleanup checks that cleanup removes expired entries and
// keeps live ones.
func TestSecretCache_Cleanup(t *testing.T) {
	cache := NewSecretCache(5 * time.Minute)
	cache.Set("expired", &Secret{Key: "expired", Value: "old"})
	cache.Set("live", &Secret{Key: "live", Value: "new"})
	cache.entries["expired"].expiresAt = time.Now().Add(-time.Second)

	cache.cleanup()

	assert.NotContains(t, cache.entries, "expired")
	require.Contains(t, cache.entries, "live")
	assert.Equal(t, "new", cache.Get("live").Value)
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

// TestSecretManager_NilConfig tests nil config uses defaults.
func TestSecretManager_NilConfig(t *testing.T) {
	log := createTestLogger(t)

	sm, err := NewSecretManager(nil, log)
	assert.NoError(t, err)
	assert.NotNil(t, sm)
	defer sm.Close()
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
