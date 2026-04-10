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

package secrets

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"
)

// setupInternalStore creates a test InternalSecretStore with all dependencies.
func setupInternalStore(t *testing.T) (InternalSecretStore, *SecretsDEK, *testMockKVStore, func()) {
	t.Helper()

	// Create mock KVStore
	store := newTestMockKVStore()

	// Create mock KMS
	kms := newTestMockKMSManager()

	// Create mock logger
	logger := newTestMockLogger()

	// Create SecretsDEK
	dek := NewSecretsDEK(kms, store, "master-key", logger)

	// Initialize global DEK
	if err := dek.Initialize(context.Background(), internalSecretAlgorithm); err != nil {
		t.Fatalf("Failed to initialize global DEK: %v", err)
	}

	// Create InternalSecretStore
	internalStore, err := NewInternalSecretStore(&InternalSecretStoreOptions{
		DEK:    dek,
		Store:  store,
		Logger: logger,
	})
	if err != nil {
		t.Fatalf("Failed to create internal secret store: %v", err)
	}

	cleanup := func() {
		if err := internalStore.Close(); err != nil {
			t.Logf("Failed to close internal store: %v", err)
		}
		if err := dek.Close(); err != nil {
			t.Logf("Failed to close DEK: %v", err)
		}
	}

	return internalStore, dek, store, cleanup
}

// TestInternalSecretStoreBasicOperations tests basic Store/Get/Delete/Exists operations.
func TestInternalSecretStoreBasicOperations(t *testing.T) {
	store, _, _, cleanup := setupInternalStore(t)
	defer cleanup()

	ctx := context.Background()

	// Test Store
	testKey := "test/secret"
	testValue := []byte("super-secret-password")

	err := store.Store(ctx, testKey, testValue)
	if err != nil {
		t.Fatalf("Store failed: %v", err)
	}

	// Test Get
	retrieved, err := store.Get(ctx, testKey)
	if err != nil {
		t.Fatalf("Get failed: %v", err)
	}

	if string(retrieved) != string(testValue) {
		t.Errorf("Retrieved value = %q, want %q", retrieved, testValue)
	}

	// Test Exists
	exists, err := store.Exists(ctx, testKey)
	if err != nil {
		t.Fatalf("Exists failed: %v", err)
	}
	if !exists {
		t.Error("Exists returned false for existing secret")
	}

	// Test Delete
	err = store.Delete(ctx, testKey)
	if err != nil {
		t.Fatalf("Delete failed: %v", err)
	}

	// Verify deleted
	exists, err = store.Exists(ctx, testKey)
	if err != nil {
		t.Fatalf("Exists failed after delete: %v", err)
	}
	if exists {
		t.Error("Exists returned true for deleted secret")
	}

	// Get should return error
	_, err = store.Get(ctx, testKey)
	if err != ErrInternalSecretNotFound {
		t.Errorf("Get after delete returned error %v, want %v", err, ErrInternalSecretNotFound)
	}
}

// TestInternalSecretStoreWellKnownKeys tests the well-known secret key constants.
func TestInternalSecretStoreWellKnownKeys(t *testing.T) {
	store, _, _, cleanup := setupInternalStore(t)
	defer cleanup()

	ctx := context.Background()

	wellKnownKeys := []string{
		InternalSecretRedisPassword,
		InternalSecretRedisUsername,
		InternalSecretSMTPPassword,
		InternalSecretBackupEncKey,
		InternalSecretAPIKeyOpenAI,
		InternalSecretTLSServerKey,
		InternalSecretJWTPrivateKey,
	}

	// Store secrets using well-known keys
	for i, key := range wellKnownKeys {
		value := []byte("secret-value-" + string(rune('A'+i)))
		if err := store.Store(ctx, key, value); err != nil {
			t.Errorf("Failed to store secret with key %q: %v", key, err)
		}
	}

	// Retrieve and verify
	for i, key := range wellKnownKeys {
		expectedValue := []byte("secret-value-" + string(rune('A'+i)))
		retrieved, err := store.Get(ctx, key)
		if err != nil {
			t.Errorf("Failed to get secret with key %q: %v", key, err)
			continue
		}
		if string(retrieved) != string(expectedValue) {
			t.Errorf("Secret with key %q: got %q, want %q", key, retrieved, expectedValue)
		}
	}
}

// TestInternalSecretStoreList tests the List operation.
func TestInternalSecretStoreList(t *testing.T) {
	store, _, _, cleanup := setupInternalStore(t)
	defer cleanup()

	ctx := context.Background()

	// Store multiple secrets
	secrets := map[string][]byte{
		"secret1": []byte("value1"),
		"secret2": []byte("value2"),
		"secret3": []byte("value3"),
	}

	for key, value := range secrets {
		if err := store.Store(ctx, key, value); err != nil {
			t.Fatalf("Failed to store secret %q: %v", key, err)
		}
	}

	// List all secrets
	keys, err := store.List(ctx)
	if err != nil {
		t.Fatalf("List failed: %v", err)
	}

	if len(keys) != len(secrets) {
		t.Errorf("List returned %d keys, want %d", len(keys), len(secrets))
	}

	// Verify all keys are present
	keyMap := make(map[string]bool)
	for _, key := range keys {
		keyMap[key] = true
	}

	for key := range secrets {
		if !keyMap[key] {
			t.Errorf("List did not return key %q", key)
		}
	}
}

// TestInternalSecretStoreUpdate tests updating an existing secret.
func TestInternalSecretStoreUpdate(t *testing.T) {
	store, _, _, cleanup := setupInternalStore(t)
	defer cleanup()

	ctx := context.Background()

	key := "updateable/secret"
	value1 := []byte("original-value")
	value2 := []byte("updated-value")

	// Store initial value
	if err := store.Store(ctx, key, value1); err != nil {
		t.Fatalf("Initial Store failed: %v", err)
	}

	// Update with new value
	if err := store.Store(ctx, key, value2); err != nil {
		t.Fatalf("Update Store failed: %v", err)
	}

	// Verify updated value
	retrieved, err := store.Get(ctx, key)
	if err != nil {
		t.Fatalf("Get after update failed: %v", err)
	}

	if string(retrieved) != string(value2) {
		t.Errorf("After update: got %q, want %q", retrieved, value2)
	}
}

// TestInternalSecretStoreValidation tests input validation.
func TestInternalSecretStoreValidation(t *testing.T) {
	store, _, _, cleanup := setupInternalStore(t)
	defer cleanup()

	ctx := context.Background()

	tests := []struct {
		name      string
		key       string
		value     []byte
		wantError error
	}{
		{
			name:      "empty key",
			key:       "",
			value:     []byte("value"),
			wantError: ErrInternalSecretInvalidKey,
		},
		{
			name:      "whitespace key",
			key:       "   ",
			value:     []byte("value"),
			wantError: ErrInternalSecretInvalidKey,
		},
		{
			name:      "key with path prefix",
			key:       internalSecretsPrefix + "key",
			value:     []byte("value"),
			wantError: ErrInternalSecretInvalidKey,
		},
		{
			name:      "key with path traversal",
			key:       "../../etc/passwd",
			value:     []byte("value"),
			wantError: ErrInternalSecretInvalidKey,
		},
		{
			name:      "empty value",
			key:       "valid/key",
			value:     []byte{},
			wantError: ErrInternalSecretEmpty,
		},
		{
			name:      "nil value",
			key:       "valid/key",
			value:     nil,
			wantError: ErrInternalSecretEmpty,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := store.Store(ctx, tt.key, tt.value)
			if err == nil {
				t.Fatal("Expected error, got nil")
			}
			if err != tt.wantError && !containsError(err, tt.wantError) {
				t.Errorf("Got error %v, want %v", err, tt.wantError)
			}
		})
	}
}

// TestInternalSecretStoreGetNonExistent tests getting non-existent secrets.
func TestInternalSecretStoreGetNonExistent(t *testing.T) {
	store, _, _, cleanup := setupInternalStore(t)
	defer cleanup()

	ctx := context.Background()

	_, err := store.Get(ctx, "non/existent/secret")
	if err != ErrInternalSecretNotFound {
		t.Errorf("Get non-existent returned error %v, want %v", err, ErrInternalSecretNotFound)
	}
}

// TestInternalSecretStoreDeleteNonExistent tests deleting non-existent secrets.
func TestInternalSecretStoreDeleteNonExistent(t *testing.T) {
	store, _, _, cleanup := setupInternalStore(t)
	defer cleanup()

	ctx := context.Background()

	err := store.Delete(ctx, "non/existent/secret")
	if err != ErrInternalSecretNotFound {
		t.Errorf("Delete non-existent returned error %v, want %v", err, ErrInternalSecretNotFound)
	}
}

// TestInternalSecretStoreExistsNonExistent tests Exists for non-existent secrets.
func TestInternalSecretStoreExistsNonExistent(t *testing.T) {
	store, _, _, cleanup := setupInternalStore(t)
	defer cleanup()

	ctx := context.Background()

	exists, err := store.Exists(ctx, "non/existent/secret")
	if err != nil {
		t.Fatalf("Exists failed: %v", err)
	}
	if exists {
		t.Error("Exists returned true for non-existent secret")
	}
}

// TestInternalSecretStoreConcurrentAccess tests concurrent operations.
func TestInternalSecretStoreConcurrentAccess(t *testing.T) {
	store, _, _, cleanup := setupInternalStore(t)
	defer cleanup()

	ctx := context.Background()
	const numGoroutines = 10
	const numOpsPerGoroutine = 20

	var wg sync.WaitGroup
	wg.Add(numGoroutines)

	for i := 0; i < numGoroutines; i++ {
		go func(id int) {
			defer wg.Done()

			for j := 0; j < numOpsPerGoroutine; j++ {
				key := "concurrent/secret"
				value := []byte("value-from-goroutine")

				// Store
				if err := store.Store(ctx, key, value); err != nil {
					t.Errorf("Concurrent Store failed: %v", err)
					return
				}

				// Get
				if _, err := store.Get(ctx, key); err != nil {
					t.Errorf("Concurrent Get failed: %v", err)
					return
				}

				// Exists
				if _, err := store.Exists(ctx, key); err != nil {
					t.Errorf("Concurrent Exists failed: %v", err)
					return
				}
			}
		}(i)
	}

	wg.Wait()
}

// TestInternalSecretStoreContextCancellation tests context cancellation handling.
func TestInternalSecretStoreContextCancellation(t *testing.T) {
	store, _, _, cleanup := setupInternalStore(t)
	defer cleanup()

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	// All operations should respect context cancellation
	err := store.Store(ctx, "test/key", []byte("value"))
	if err == nil || err.Error() != "context canceled" {
		t.Errorf("Store with canceled context: got error %v, want context canceled", err)
	}

	_, err = store.Get(ctx, "test/key")
	if err == nil || err.Error() != "context canceled" {
		t.Errorf("Get with canceled context: got error %v, want context canceled", err)
	}

	err = store.Delete(ctx, "test/key")
	if err == nil || err.Error() != "context canceled" {
		t.Errorf("Delete with canceled context: got error %v, want context canceled", err)
	}

	_, err = store.List(ctx)
	if err == nil || err.Error() != "context canceled" {
		t.Errorf("List with canceled context: got error %v, want context canceled", err)
	}

	_, err = store.Exists(ctx, "test/key")
	if err == nil || err.Error() != "context canceled" {
		t.Errorf("Exists with canceled context: got error %v, want context canceled", err)
	}
}

// TestInternalSecretStoreListEmpty tests listing when no secrets exist.
func TestInternalSecretStoreListEmpty(t *testing.T) {
	store, _, _, cleanup := setupInternalStore(t)
	defer cleanup()

	ctx := context.Background()

	keys, err := store.List(ctx)
	if err != nil {
		t.Fatalf("List failed: %v", err)
	}

	if len(keys) != 0 {
		t.Errorf("List returned %d keys, want 0", len(keys))
	}
}

// TestInternalSecretStoreLargeValue tests storing and retrieving large values.
func TestInternalSecretStoreLargeValue(t *testing.T) {
	store, _, _, cleanup := setupInternalStore(t)
	defer cleanup()

	ctx := context.Background()

	// Create a large value (1MB)
	largeValue := make([]byte, 1024*1024)
	for i := range largeValue {
		largeValue[i] = byte(i % 256)
	}

	key := "large/secret"

	// Store
	if err := store.Store(ctx, key, largeValue); err != nil {
		t.Fatalf("Store large value failed: %v", err)
	}

	// Get
	retrieved, err := store.Get(ctx, key)
	if err != nil {
		t.Fatalf("Get large value failed: %v", err)
	}

	if len(retrieved) != len(largeValue) {
		t.Errorf("Retrieved value length = %d, want %d", len(retrieved), len(largeValue))
	}

	// Verify content
	for i := range retrieved {
		if retrieved[i] != largeValue[i] {
			t.Errorf("Byte mismatch at index %d: got %d, want %d", i, retrieved[i], largeValue[i])
			break
		}
	}
}

// TestInternalSecretStoreSpecialCharacters tests keys with special characters.
func TestInternalSecretStoreSpecialCharacters(t *testing.T) {
	store, _, _, cleanup := setupInternalStore(t)
	defer cleanup()

	ctx := context.Background()

	specialKeys := []string{
		"key/with/slashes",
		"key-with-dashes",
		"key_with_underscores",
		"key.with.dots",
		"key:with:colons",
		"key@with@at",
	}

	for _, key := range specialKeys {
		value := []byte("test-value")

		if err := store.Store(ctx, key, value); err != nil {
			t.Errorf("Failed to store key %q: %v", key, err)
			continue
		}

		retrieved, err := store.Get(ctx, key)
		if err != nil {
			t.Errorf("Failed to get key %q: %v", key, err)
			continue
		}

		if string(retrieved) != string(value) {
			t.Errorf("Key %q: got %q, want %q", key, retrieved, value)
		}
	}
}

// TestInternalSecretStoreNewWithNilOptions tests error handling for nil options.
func TestInternalSecretStoreNewWithNilOptions(t *testing.T) {
	_, err := NewInternalSecretStore(nil)
	if err != ErrNilOptions {
		t.Errorf("NewInternalSecretStore with nil options: got error %v, want %v", err, ErrNilOptions)
	}
}

// TestInternalSecretStoreNewWithNilDEK tests error handling for nil DEK.
func TestInternalSecretStoreNewWithNilDEK(t *testing.T) {
	store := newTestMockKVStore()

	_, err := NewInternalSecretStore(&InternalSecretStoreOptions{
		DEK:   nil,
		Store: store,
	})
	if err == nil || !strings.Contains(err.Error(), "DEK cannot be nil") {
		t.Errorf("NewInternalSecretStore with nil DEK: got error %v, want DEK cannot be nil", err)
	}
}

// TestInternalSecretStoreNewWithNilStore tests error handling for nil store.
func TestInternalSecretStoreNewWithNilStore(t *testing.T) {
	store := newTestMockKVStore()
	kms := newTestMockKMSManager()
	logger := newTestMockLogger()

	dek := NewSecretsDEK(kms, store, "master-key", logger)
	defer dek.Close()

	_, err := NewInternalSecretStore(&InternalSecretStoreOptions{
		DEK:   dek,
		Store: nil,
	})
	if err != ErrNilStore {
		t.Errorf("NewInternalSecretStore with nil store: got error %v, want %v", err, ErrNilStore)
	}
}

// TestInternalSecretStoreMetadataPreservation tests that metadata is preserved on updates.
func TestInternalSecretStoreMetadataPreservation(t *testing.T) {
	store, _, kvStore, cleanup := setupInternalStore(t)
	defer cleanup()

	ctx := context.Background()

	key := "metadata/test"
	value1 := []byte("value1")
	value2 := []byte("value2")

	// Store initial value
	if err := store.Store(ctx, key, value1); err != nil {
		t.Fatalf("Initial Store failed: %v", err)
	}

	// Get the raw entry to check CreatedAt
	storageKey := internalSecretsPrefix + key
	rawData1, err := kvStore.Get(ctx, storageKey)
	if err != nil {
		t.Fatalf("Failed to get raw entry: %v", err)
	}

	var entry1 internalSecretEntry
	if err := json.Unmarshal(rawData1, &entry1); err != nil {
		t.Fatalf("Failed to unmarshal entry: %v", err)
	}

	// Wait a bit to ensure different timestamps
	time.Sleep(10 * time.Millisecond)

	// Update the value
	if err := store.Store(ctx, key, value2); err != nil {
		t.Fatalf("Update Store failed: %v", err)
	}

	// Get the raw entry again
	rawData2, err := kvStore.Get(ctx, storageKey)
	if err != nil {
		t.Fatalf("Failed to get raw entry after update: %v", err)
	}

	var entry2 internalSecretEntry
	if err := json.Unmarshal(rawData2, &entry2); err != nil {
		t.Fatalf("Failed to unmarshal updated entry: %v", err)
	}

	// CreatedAt should be the same
	if !entry1.CreatedAt.Equal(entry2.CreatedAt) {
		t.Errorf("CreatedAt changed: before %v, after %v", entry1.CreatedAt, entry2.CreatedAt)
	}

	// UpdatedAt should be different
	if entry1.UpdatedAt.Equal(entry2.UpdatedAt) {
		t.Error("UpdatedAt did not change on update")
	}
}

// TestInternalSecretStoreDEKVersionTracking tests that DEK version is tracked.
func TestInternalSecretStoreDEKVersionTracking(t *testing.T) {
	store, dek, kvStore, cleanup := setupInternalStore(t)
	defer cleanup()

	ctx := context.Background()

	key := "version/test"
	value := []byte("test-value")

	// Store with initial DEK version
	if err := store.Store(ctx, key, value); err != nil {
		t.Fatalf("Store failed: %v", err)
	}

	// Verify DEK version is tracked
	storageKey := internalSecretsPrefix + key
	rawData, err := kvStore.Get(ctx, storageKey)
	if err != nil {
		t.Fatalf("Failed to get raw entry: %v", err)
	}

	var entry internalSecretEntry
	if err := json.Unmarshal(rawData, &entry); err != nil {
		t.Fatalf("Failed to unmarshal entry: %v", err)
	}

	if entry.DEKVersion == 0 {
		t.Error("DEK version not tracked (should be > 0)")
	}

	// Rotate the DEK
	newVersion, err := dek.Rotate(ctx, DEKIDGlobal)
	if err != nil {
		t.Fatalf("DEK rotation failed: %v", err)
	}

	// Store a new value with rotated DEK
	if err := store.Store(ctx, key, value); err != nil {
		t.Fatalf("Store with rotated DEK failed: %v", err)
	}

	// Verify new DEK version is used
	rawData, err = kvStore.Get(ctx, storageKey)
	if err != nil {
		t.Fatalf("Failed to get raw entry after rotation: %v", err)
	}

	var entryAfterRotation internalSecretEntry
	if err := json.Unmarshal(rawData, &entryAfterRotation); err != nil {
		t.Fatalf("Failed to unmarshal entry after rotation: %v", err)
	}

	if entryAfterRotation.DEKVersion != newVersion {
		t.Errorf("DEK version after rotation = %d, want %d", entryAfterRotation.DEKVersion, newVersion)
	}
}

// Helper function to check if an error contains another error.
func containsError(err, target error) bool {
	if err == nil || target == nil {
		return false
	}
	return err == target || errors.Is(err, target) || (err.Error() != "" && target.Error() != "" &&
		strings.Contains(err.Error(), target.Error()))
}

// TestInternalSecretStoreDecryptionWithOldVersion tests decrypting secrets encrypted with an old DEK version.
func TestInternalSecretStoreDecryptionWithOldVersion(t *testing.T) {
	store, dek, _, cleanup := setupInternalStore(t)
	defer cleanup()

	ctx := context.Background()

	key := "old-version/secret"
	value := []byte("original-value")

	// Store with initial DEK version
	if err := store.Store(ctx, key, value); err != nil {
		t.Fatalf("Store failed: %v", err)
	}

	// Rotate the DEK
	_, err := dek.Rotate(ctx, DEKIDGlobal)
	if err != nil {
		t.Fatalf("DEK rotation failed: %v", err)
	}

	// Should still be able to retrieve the secret encrypted with old version
	retrieved, err := store.Get(ctx, key)
	if err != nil {
		t.Fatalf("Get after DEK rotation failed: %v", err)
	}

	if string(retrieved) != string(value) {
		t.Errorf("Retrieved value = %q, want %q", retrieved, value)
	}
}

// TestInternalSecretStoreGetInvalidJSON tests handling of corrupted storage entries.
func TestInternalSecretStoreGetInvalidJSON(t *testing.T) {
	store, _, kvStore, cleanup := setupInternalStore(t)
	defer cleanup()

	ctx := context.Background()

	key := "corrupted/secret"

	// Manually store invalid JSON
	storageKey := internalSecretsPrefix + key
	if err := kvStore.Put(ctx, storageKey, []byte("{invalid json}")); err != nil {
		t.Fatalf("Failed to store invalid JSON: %v", err)
	}

	// Get should return decryption error
	_, err := store.Get(ctx, key)
	if err == nil {
		t.Fatal("Expected error for invalid JSON, got nil")
	}
	if !containsError(err, ErrInternalSecretDecryptionFailed) {
		t.Errorf("Expected ErrInternalSecretDecryptionFailed, got %v", err)
	}
}

// TestInternalSecretStoreExtractSecretKeyEmptyPrefix tests extractSecretKey with invalid input.
func TestInternalSecretStoreExtractSecretKeyEmptyPrefix(t *testing.T) {
	store, _, _, cleanup := setupInternalStore(t)
	defer cleanup()

	impl := store.(*internalSecretStoreImpl)

	// Test with key that doesn't have the prefix
	result := impl.extractSecretKey("no-prefix/key")
	if result != "" {
		t.Errorf("extractSecretKey with no prefix: got %q, want empty string", result)
	}
}

// TestInternalSecretStoreErrorPaths tests various error paths for better coverage.
func TestInternalSecretStoreErrorPaths(t *testing.T) {
	store, _, kvStore, cleanup := setupInternalStore(t)
	defer cleanup()

	ctx := context.Background()

	// Test Get with storage error (non-NotFound)
	t.Run("Get storage error", func(t *testing.T) {
		key := "error/test"

		// Store a valid secret first
		if err := store.Store(ctx, key, []byte("value")); err != nil {
			t.Fatalf("Store failed: %v", err)
		}

		// Make Get return an error
		originalGetFn := kvStore.getFn
		kvStore.getFn = func(ctx context.Context, keyParam string) ([]byte, error) {
			if string(keyParam) == internalSecretsPrefix+key {
				return nil, errors.New("storage error")
			}
			return kvStore.data[string(keyParam)], nil
		}
		defer func() { kvStore.getFn = originalGetFn }()

		_, err := store.Get(ctx, key)
		if err == nil {
			t.Fatal("Expected error, got nil")
		}
	})

	// Test Delete with storage error
	t.Run("Delete storage error on exists check", func(t *testing.T) {
		key := "delete/error"

		// Make Get return a storage error
		originalGetFn := kvStore.getFn
		kvStore.getFn = func(ctx context.Context, keyParam string) ([]byte, error) {
			if string(keyParam) == internalSecretsPrefix+key {
				return nil, errors.New("storage error")
			}
			return kvStore.data[string(keyParam)], nil
		}
		defer func() { kvStore.getFn = originalGetFn }()

		err := store.Delete(ctx, key)
		if err == nil {
			t.Fatal("Expected error, got nil")
		}
	})

	// Test Exists with storage error
	t.Run("Exists storage error", func(t *testing.T) {
		key := "exists/error"

		// Make Get return a storage error
		originalGetFn := kvStore.getFn
		kvStore.getFn = func(ctx context.Context, keyParam string) ([]byte, error) {
			return nil, errors.New("storage error")
		}
		defer func() { kvStore.getFn = originalGetFn }()

		_, err := store.Exists(ctx, key)
		if err == nil {
			t.Fatal("Expected error, got nil")
		}
	})

	// Test List with storage error
	t.Run("List storage error", func(t *testing.T) {
		// Make List return an error
		originalListFn := kvStore.listFn
		kvStore.listFn = func(ctx context.Context, prefix string) ([]string, error) {
			return nil, errors.New("storage error")
		}
		defer func() { kvStore.listFn = originalListFn }()

		_, err := store.List(ctx)
		if err == nil {
			t.Fatal("Expected error, got nil")
		}
	})
}

// TestInternalSecretStoreStorePutError tests Store when Put fails.
func TestInternalSecretStoreStorePutError(t *testing.T) {
	store, _, kvStore, cleanup := setupInternalStore(t)
	defer cleanup()

	ctx := context.Background()
	key := "store/put/error"
	value := []byte("test-value")

	// Make Put return an error
	originalPutFn := kvStore.putFn
	kvStore.putFn = func(ctx context.Context, k string, v []byte) error {
		if string(k) == internalSecretsPrefix+key {
			return errors.New("put failed")
		}
		kvStore.data[string(k)] = v
		return nil
	}
	defer func() { kvStore.putFn = originalPutFn }()

	err := store.Store(ctx, key, value)
	if err == nil {
		t.Fatal("Expected error, got nil")
	}
	if !containsError(err, errors.New("failed to store internal secret")) {
		t.Errorf("Expected 'failed to store internal secret' error, got %v", err)
	}
}

// TestInternalSecretStoreDeleteStorageError tests Delete when Delete storage operation fails.
func TestInternalSecretStoreDeleteStorageError(t *testing.T) {
	store, _, kvStore, cleanup := setupInternalStore(t)
	defer cleanup()

	ctx := context.Background()
	key := "delete/storage/error"
	value := []byte("test-value")

	// Store a secret first
	if err := store.Store(ctx, key, value); err != nil {
		t.Fatalf("Store failed: %v", err)
	}

	// Make Delete return an error
	originalDeleteFn := kvStore.deleteFn
	kvStore.deleteFn = func(ctx context.Context, k string) error {
		if string(k) == internalSecretsPrefix+key {
			return errors.New("delete failed")
		}
		return nil
	}
	defer func() { kvStore.deleteFn = originalDeleteFn }()

	err := store.Delete(ctx, key)
	if err == nil {
		t.Fatal("Expected error, got nil")
	}
	if !containsError(err, errors.New("failed to delete internal secret")) {
		t.Errorf("Expected 'failed to delete internal secret' error, got %v", err)
	}
}

// TestInternalSecretStoreDeleteNilData tests Delete when Get returns nil data.
func TestInternalSecretStoreDeleteNilData(t *testing.T) {
	store, _, kvStore, cleanup := setupInternalStore(t)
	defer cleanup()

	ctx := context.Background()
	key := "delete/nil/data"

	// Make Get return nil data
	originalGetFn := kvStore.getFn
	kvStore.getFn = func(ctx context.Context, k string) ([]byte, error) {
		if string(k) == internalSecretsPrefix+key {
			return nil, nil
		}
		return kvStore.data[string(k)], nil
	}
	defer func() { kvStore.getFn = originalGetFn }()

	err := store.Delete(ctx, key)
	if err != ErrInternalSecretNotFound {
		t.Errorf("Expected ErrInternalSecretNotFound, got %v", err)
	}
}

// TestInternalSecretStoreGetWithOldDEKVersion tests Get with DEK version 0 (old format).
func TestInternalSecretStoreGetWithOldDEKVersion(t *testing.T) {
	store, _, kvStore, cleanup := setupInternalStore(t)
	defer cleanup()

	ctx := context.Background()
	key := "old-format/secret"
	value := []byte("test-value")

	// Store a secret normally
	if err := store.Store(ctx, key, value); err != nil {
		t.Fatalf("Store failed: %v", err)
	}

	// Manually modify the entry to have DEK version 0
	storageKey := internalSecretsPrefix + key
	rawData, err := kvStore.Get(ctx, storageKey)
	if err != nil {
		t.Fatalf("Failed to get raw entry: %v", err)
	}

	var entry internalSecretEntry
	if err := json.Unmarshal(rawData, &entry); err != nil {
		t.Fatalf("Failed to unmarshal entry: %v", err)
	}

	// Set DEK version to 0 to simulate old format
	entry.DEKVersion = 0

	modifiedData, err := json.Marshal(entry)
	if err != nil {
		t.Fatalf("Failed to marshal modified entry: %v", err)
	}

	if err := kvStore.Put(ctx, storageKey, modifiedData); err != nil {
		t.Fatalf("Failed to store modified entry: %v", err)
	}

	// Get should still work
	retrieved, err := store.Get(ctx, key)
	if err != nil {
		t.Fatalf("Get with old format failed: %v", err)
	}

	if string(retrieved) != string(value) {
		t.Errorf("Retrieved value = %q, want %q", retrieved, value)
	}
}

// TestInternalSecretStoreStorePreserveCreatedAtError tests Store when preserving CreatedAt fails.
func TestInternalSecretStoreStorePreserveCreatedAtError(t *testing.T) {
	store, _, kvStore, cleanup := setupInternalStore(t)
	defer cleanup()

	ctx := context.Background()
	key := "preserve/error"
	value := []byte("test-value")

	// Store initial value
	if err := store.Store(ctx, key, value); err != nil {
		t.Fatalf("Initial Store failed: %v", err)
	}

	// Make Get return invalid JSON for the second store (update)
	callCount := 0
	originalGetFn := kvStore.getFn
	kvStore.getFn = func(ctx context.Context, k string) ([]byte, error) {
		// First call is inside Store to preserve CreatedAt
		// Second call is the normal Get
		data := kvStore.data[string(k)]
		if string(k) == internalSecretsPrefix+key && data != nil {
			callCount++
			// On second call (update), return invalid JSON
			if callCount > 1 {
				return []byte("{invalid json}"), nil
			}
		}
		if data == nil {
			return nil, ErrSecretNotFound
		}
		return data, nil
	}
	defer func() { kvStore.getFn = originalGetFn }()

	// Update - should still succeed even if CreatedAt can't be preserved
	value2 := []byte("updated-value")
	if err := store.Store(ctx, key, value2); err != nil {
		t.Fatalf("Update Store failed: %v", err)
	}
}

// TestInternalSecretStoreGetDEKVersionError tests Get when getting specific DEK version fails.
func TestInternalSecretStoreGetDEKVersionError(t *testing.T) {
	// Create custom DEK with error injection
	kvStore := newTestMockKVStore()
	kms := newTestMockKMSManager()
	logger := newTestMockLogger()

	dek := NewSecretsDEK(kms, kvStore, "master-key", logger)
	if err := dek.Initialize(context.Background(), internalSecretAlgorithm); err != nil {
		t.Fatalf("Failed to initialize global DEK: %v", err)
	}
	defer dek.Close()

	store, err := NewInternalSecretStore(&InternalSecretStoreOptions{
		DEK:    dek,
		Store:  kvStore,
		Logger: logger,
	})
	if err != nil {
		t.Fatalf("Failed to create internal secret store: %v", err)
	}
	defer store.Close()

	ctx := context.Background()
	key := "dek-version/error"
	value := []byte("test-value")

	// Store a secret normally
	if err := store.Store(ctx, key, value); err != nil {
		t.Fatalf("Store failed: %v", err)
	}

	// Manually modify the entry to have a high DEK version that doesn't exist
	storageKey := internalSecretsPrefix + key
	rawData, err := kvStore.Get(ctx, storageKey)
	if err != nil {
		t.Fatalf("Failed to get raw entry: %v", err)
	}

	var entry internalSecretEntry
	if err := json.Unmarshal(rawData, &entry); err != nil {
		t.Fatalf("Failed to unmarshal entry: %v", err)
	}

	// Set DEK version to a non-existent high version
	entry.DEKVersion = 9999

	modifiedData, err := json.Marshal(entry)
	if err != nil {
		t.Fatalf("Failed to marshal modified entry: %v", err)
	}

	if err := kvStore.Put(ctx, storageKey, modifiedData); err != nil {
		t.Fatalf("Failed to store modified entry: %v", err)
	}

	// Get should fail with decryption error
	_, err = store.Get(ctx, key)
	if err == nil {
		t.Fatal("Expected error for non-existent DEK version, got nil")
	}
	if !containsError(err, ErrInternalSecretDecryptionFailed) {
		t.Errorf("Expected ErrInternalSecretDecryptionFailed, got %v", err)
	}
}

// TestInternalSecretStoreExistsValidationError tests Exists with validation error.
func TestInternalSecretStoreExistsValidationError(t *testing.T) {
	store, _, _, cleanup := setupInternalStore(t)
	defer cleanup()

	ctx := context.Background()

	// Test with invalid key (path traversal)
	_, err := store.Exists(ctx, "../invalid")
	if err == nil {
		t.Fatal("Expected error for invalid key, got nil")
	}
	if !containsError(err, ErrInternalSecretInvalidKey) {
		t.Errorf("Expected ErrInternalSecretInvalidKey, got %v", err)
	}
}

// TestInternalSecretStoreGetValidationError tests Get with validation error.
func TestInternalSecretStoreGetValidationError(t *testing.T) {
	store, _, _, cleanup := setupInternalStore(t)
	defer cleanup()

	ctx := context.Background()

	// Test with invalid key (empty)
	_, err := store.Get(ctx, "")
	if err == nil {
		t.Fatal("Expected error for empty key, got nil")
	}
	if !containsError(err, ErrInternalSecretInvalidKey) {
		t.Errorf("Expected ErrInternalSecretInvalidKey, got %v", err)
	}
}

// TestInternalSecretStoreDeleteValidationError tests Delete with validation error.
func TestInternalSecretStoreDeleteValidationError(t *testing.T) {
	store, _, _, cleanup := setupInternalStore(t)
	defer cleanup()

	ctx := context.Background()

	// Test with invalid key (path prefix)
	err := store.Delete(ctx, internalSecretsPrefix+"key")
	if err == nil {
		t.Fatal("Expected error for invalid key, got nil")
	}
	if !containsError(err, ErrInternalSecretInvalidKey) {
		t.Errorf("Expected ErrInternalSecretInvalidKey, got %v", err)
	}
}

// TestInternalSecretStoreWithoutLogger tests operations without a logger.
func TestInternalSecretStoreWithoutLogger(t *testing.T) {
	// Create store without logger
	kvStore := newTestMockKVStore()
	kms := newTestMockKMSManager()
	logger := newTestMockLogger() // DEK needs logger

	dek := NewSecretsDEK(kms, kvStore, "master-key", logger)
	if err := dek.Initialize(context.Background(), internalSecretAlgorithm); err != nil {
		t.Fatalf("Failed to initialize global DEK: %v", err)
	}
	defer dek.Close()

	store, err := NewInternalSecretStore(&InternalSecretStoreOptions{
		DEK:    dek,
		Store:  kvStore,
		Logger: nil, // No logger for internal store
	})
	if err != nil {
		t.Fatalf("Failed to create internal secret store: %v", err)
	}
	defer store.Close()

	ctx := context.Background()
	key := "test/secret"
	value := []byte("test-value")

	// Test Store without logger
	if err := store.Store(ctx, key, value); err != nil {
		t.Fatalf("Store failed: %v", err)
	}

	// Test Get without logger
	retrieved, err := store.Get(ctx, key)
	if err != nil {
		t.Fatalf("Get failed: %v", err)
	}

	if string(retrieved) != string(value) {
		t.Errorf("Retrieved value = %q, want %q", retrieved, value)
	}

	// Test Delete without logger
	if err := store.Delete(ctx, key); err != nil {
		t.Fatalf("Delete failed: %v", err)
	}

	// Test Close without logger
	if err := store.Close(); err != nil {
		t.Fatalf("Close failed: %v", err)
	}
}
