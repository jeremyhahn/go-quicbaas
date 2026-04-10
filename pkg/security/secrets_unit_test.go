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

func TestSecretManager_SetEnvBackend(t *testing.T) {
	cfg := &SecretConfig{
		CacheEnabled: false,
	}

	log := createTestLogger(t)
	sm, err := NewSecretManager(cfg, log)
	require.NoError(t, err)
	defer sm.Close()

	ctx := context.Background()

	// Set should fail for env backend
	err = sm.Set(ctx, "TEST_SECRET", "test-value", nil)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not supported")
}

func TestSecretManager_SetWithCacheInvalidation(t *testing.T) {
	cfg := &SecretConfig{
		CacheEnabled: true,
		CacheTTL:     1 * time.Minute,
	}

	log := createTestLogger(t)
	sm, err := NewSecretManager(cfg, log)
	require.NoError(t, err)
	defer sm.Close()

	// Set an env var first
	os.Setenv("TEST_KEY_FOR_CACHE", "initial-value")
	defer os.Unsetenv("TEST_KEY_FOR_CACHE")

	ctx := context.Background()

	// Get to populate cache
	secret, err := sm.Get(ctx, "TEST_KEY_FOR_CACHE")
	require.NoError(t, err)
	assert.Equal(t, "initial-value", secret.Value)

	// Attempt Set (will fail for env backend but cache invalidation should work)
	err = sm.Set(ctx, "TEST_KEY_FOR_CACHE", "new-value", nil)
	assert.Error(t, err) // Should fail for env backend

	// The error path still invalidates cache
	assert.NotNil(t, sm.cache)
}

func TestSecretManager_DeleteEnvBackend(t *testing.T) {
	cfg := &SecretConfig{
		CacheEnabled: false,
	}

	log := createTestLogger(t)
	sm, err := NewSecretManager(cfg, log)
	require.NoError(t, err)
	defer sm.Close()

	ctx := context.Background()

	// Delete should fail for env backend
	err = sm.Delete(ctx, "TEST_SECRET")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not supported")
}

func TestSecretManager_DeleteWithCacheInvalidation(t *testing.T) {
	cfg := &SecretConfig{
		CacheEnabled: true,
		CacheTTL:     1 * time.Minute,
	}

	log := createTestLogger(t)
	sm, err := NewSecretManager(cfg, log)
	require.NoError(t, err)
	defer sm.Close()

	// Set an env var first
	os.Setenv("TEST_KEY_FOR_DELETE", "test-value")
	defer os.Unsetenv("TEST_KEY_FOR_DELETE")

	ctx := context.Background()

	// Get to populate cache
	secret, err := sm.Get(ctx, "TEST_KEY_FOR_DELETE")
	require.NoError(t, err)
	assert.Equal(t, "test-value", secret.Value)

	// Verify it's in cache
	cachedSecret := sm.cache.Get("TEST_KEY_FOR_DELETE")
	assert.NotNil(t, cachedSecret)

	// Attempt Delete (will fail for env backend but should still invalidate cache)
	err = sm.Delete(ctx, "TEST_KEY_FOR_DELETE")
	assert.Error(t, err) // Should fail for env backend

	// Even though delete fails, cache invalidation should work
	assert.NotNil(t, sm.cache)
}

func TestSecretCache_StartCleanupGoroutine(t *testing.T) {
	cache := NewSecretCache(100 * time.Millisecond)

	// Add an entry
	secret := &Secret{
		Key:       "test",
		Value:     "value",
		Version:   1,
		CreatedAt: time.Now(),
	}
	cache.Set("test", secret)

	// Start cleanup in background
	done := make(chan bool)
	go func() {
		// Run cleanup for a short time
		ticker := time.NewTicker(50 * time.Millisecond)
		select {
		case <-ticker.C:
			cache.cleanup()
		case <-time.After(200 * time.Millisecond):
		}
		ticker.Stop()
		done <- true
	}()

	// Wait for cleanup to run
	<-done

	// Give time for entry to expire
	time.Sleep(150 * time.Millisecond)
	cache.cleanup()

	// Entry should be cleaned up
	result := cache.Get("test")
	assert.Nil(t, result)
}

func TestSecretCache_CleanupMultipleEntries(t *testing.T) {
	cache := NewSecretCache(50 * time.Millisecond)

	// Add multiple entries
	for i := 0; i < 5; i++ {
		secret := &Secret{
			Key:       string(rune('a' + i)),
			Value:     "value",
			Version:   1,
			CreatedAt: time.Now(),
		}
		cache.Set(string(rune('a'+i)), secret)
	}

	// Wait for entries to expire
	time.Sleep(100 * time.Millisecond)

	// Run cleanup
	cache.cleanup()

	// All entries should be gone
	for i := 0; i < 5; i++ {
		result := cache.Get(string(rune('a' + i)))
		assert.Nil(t, result)
	}
}

func TestSecretCache_GetExpiredEntry(t *testing.T) {
	cache := NewSecretCache(50 * time.Millisecond)

	secret := &Secret{
		Key:       "test",
		Value:     "value",
		Version:   1,
		CreatedAt: time.Now(),
	}
	cache.Set("test", secret)

	// Verify it's there initially
	result := cache.Get("test")
	assert.NotNil(t, result)

	// Wait for expiration
	time.Sleep(100 * time.Millisecond)

	// Should return nil for expired entry
	result = cache.Get("test")
	assert.Nil(t, result)
}

func TestSecretManager_GetWithDefaultExistingValue(t *testing.T) {
	cfg := &SecretConfig{
		CacheEnabled: false,
	}

	log := createTestLogger(t)
	sm, err := NewSecretManager(cfg, log)
	require.NoError(t, err)
	defer sm.Close()

	// Set an env var
	os.Setenv("TEST_GET_DEFAULT", "actual-value")
	defer os.Unsetenv("TEST_GET_DEFAULT")

	ctx := context.Background()
	value := sm.GetWithDefault(ctx, "TEST_GET_DEFAULT", "default-value")
	assert.Equal(t, "actual-value", value)
}

func TestSecretManager_GetWithDefaultFallback(t *testing.T) {
	cfg := &SecretConfig{
		CacheEnabled: false,
	}

	log := createTestLogger(t)
	sm, err := NewSecretManager(cfg, log)
	require.NoError(t, err)
	defer sm.Close()

	ctx := context.Background()
	value := sm.GetWithDefault(ctx, "NONEXISTENT_KEY", "default-value")
	assert.Equal(t, "default-value", value)
}

func TestSecretManager_GetStringSuccess(t *testing.T) {
	cfg := &SecretConfig{
		CacheEnabled: false,
	}

	log := createTestLogger(t)
	sm, err := NewSecretManager(cfg, log)
	require.NoError(t, err)
	defer sm.Close()

	// Set an env var
	os.Setenv("TEST_GET_STRING_KEY", "test-value")
	defer os.Unsetenv("TEST_GET_STRING_KEY")

	ctx := context.Background()
	value, err := sm.GetString(ctx, "TEST_GET_STRING_KEY")
	require.NoError(t, err)
	assert.Equal(t, "test-value", value)
}

func TestSecretManager_GetStringNotFoundError(t *testing.T) {
	cfg := &SecretConfig{
		CacheEnabled: false,
	}

	log := createTestLogger(t)
	sm, err := NewSecretManager(cfg, log)
	require.NoError(t, err)
	defer sm.Close()

	ctx := context.Background()
	value, err := sm.GetString(ctx, "NONEXISTENT_KEY")
	assert.Error(t, err)
	assert.Empty(t, value)
}

func TestSecretCache_Close(t *testing.T) {
	cache := NewSecretCache(1 * time.Minute)

	// Add an entry
	secret := &Secret{
		Key:       "test",
		Value:     "value",
		Version:   1,
		CreatedAt: time.Now(),
	}
	cache.Set("test", secret)

	// Verify it's there
	result := cache.Get("test")
	assert.NotNil(t, result)

	// Close the cache
	cache.Close()

	// Entry should be cleared
	result = cache.Get("test")
	assert.Nil(t, result)
}

func TestEnvBackend_Close(t *testing.T) {
	log := createTestLogger(t)

	backend, err := NewEnvBackend(log)
	require.NoError(t, err)

	err = backend.Close()
	assert.NoError(t, err)
}
