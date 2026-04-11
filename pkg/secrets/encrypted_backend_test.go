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

// Package secrets provides secret storage and retrieval functionality for QuicBaaS.
package secrets

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	dberrors "github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
)

// createTestEncryptedBackend creates an encrypted backend with mock components for testing.
func createTestEncryptedBackend(t *testing.T) *EncryptedBackend {
	t.Helper()

	store := newTestMockKVStore()
	encryptor := newTestMockSecretsEncryptor()
	logger := newTestMockLogger()

	eb, err := NewEncryptedBackend(&EncryptedBackendOptions{
		Store:                store,
		Encryptor:            encryptor,
		DefaultKeyIdentifier: string(DEKIDGlobal),
		Logger:               logger,
	})
	require.NoError(t, err)

	return eb
}

func TestNewEncryptedBackend(t *testing.T) {
	t.Run("nil options", func(t *testing.T) {
		_, err := NewEncryptedBackend(nil)
		assert.ErrorIs(t, err, ErrNilOptions)
	})

	t.Run("nil store", func(t *testing.T) {
		_, err := NewEncryptedBackend(&EncryptedBackendOptions{
			Store:     nil,
			Encryptor: newTestMockSecretsEncryptor(),
		})
		assert.ErrorIs(t, err, ErrNilStore)
	})

	t.Run("nil encryptor", func(t *testing.T) {
		_, err := NewEncryptedBackend(&EncryptedBackendOptions{
			Store:     newTestMockKVStore(),
			Encryptor: nil,
		})
		assert.ErrorIs(t, err, ErrNilEncryptor)
	})

	t.Run("invalid algorithm", func(t *testing.T) {
		_, err := NewEncryptedBackend(&EncryptedBackendOptions{
			Store:            newTestMockKVStore(),
			Encryptor:        newTestMockSecretsEncryptor(),
			DefaultAlgorithm: "invalid-algorithm",
		})
		assert.ErrorIs(t, err, ErrUnsupportedAlgorithm)
	})

	t.Run("valid options with defaults", func(t *testing.T) {
		eb, err := NewEncryptedBackend(&EncryptedBackendOptions{
			Store:     newTestMockKVStore(),
			Encryptor: newTestMockSecretsEncryptor(),
		})
		require.NoError(t, err)
		assert.NotNil(t, eb)
		assert.Equal(t, "encrypted", eb.Type())
		assert.Equal(t, string(DEKIDGlobal), eb.defaultKeyIdentifier)
		assert.Equal(t, DefaultAlgorithm, eb.defaultAlgorithm)
	})

	t.Run("valid options with custom settings", func(t *testing.T) {
		customKeyID := "custom-key"
		customAlgo := "AES-128-GCM"

		eb, err := NewEncryptedBackend(&EncryptedBackendOptions{
			Store:                newTestMockKVStore(),
			Encryptor:            newTestMockSecretsEncryptor(),
			DefaultKeyIdentifier: customKeyID,
			DefaultAlgorithm:     customAlgo,
		})
		require.NoError(t, err)
		assert.Equal(t, customKeyID, eb.defaultKeyIdentifier)
		assert.Equal(t, customAlgo, eb.defaultAlgorithm)
	})
}

func TestEncryptedBackend_StoreAndGet(t *testing.T) {
	eb := createTestEncryptedBackend(t)
	ctx := context.Background()

	t.Run("store and retrieve secret", func(t *testing.T) {
		data := map[string][]byte{
			"username": []byte("admin"),
			"password": []byte("secret123"),
		}
		metadata := map[string]string{
			"created_by": "test",
		}

		// Store
		err := eb.Store(ctx, "myapp/db/creds", data, metadata)
		require.NoError(t, err)

		// Retrieve
		retrieved, retrievedMeta, err := eb.Get(ctx, "myapp/db/creds")
		require.NoError(t, err)
		assert.Equal(t, data, retrieved)
		// Check that original metadata is preserved (version is added by backend)
		assert.Equal(t, "test", retrievedMeta["created_by"])
		assert.Contains(t, retrievedMeta, "version")
	})

	t.Run("empty path", func(t *testing.T) {
		err := eb.Store(ctx, "", map[string][]byte{"key": []byte("value")}, nil)
		assert.ErrorIs(t, err, ErrInvalidPath)
	})

	t.Run("empty data", func(t *testing.T) {
		err := eb.Store(ctx, "test/empty", nil, nil)
		assert.ErrorIs(t, err, ErrEmptyData)
	})

	t.Run("get non-existent secret", func(t *testing.T) {
		_, _, err := eb.Get(ctx, "does/not/exist")
		assert.ErrorIs(t, err, ErrSecretNotFound)
	})

	t.Run("get with empty path", func(t *testing.T) {
		_, _, err := eb.Get(ctx, "")
		assert.ErrorIs(t, err, ErrInvalidPath)
	})
}

func TestEncryptedBackend_StoreWithOptions(t *testing.T) {
	eb := createTestEncryptedBackend(t)
	ctx := context.Background()

	// Add a tenant-specific key to the encryptor
	tenantKey := make([]byte, 32)
	for i := range tenantKey {
		tenantKey[i] = byte(i + 100)
	}
	tenantID := "acme-corp"
	tenantDEKID := string(NewTenantDEKID(tenantID))
	eb.encryptor.(*testMockSecretsEncryptor).AddKey(tenantDEKID, tenantKey)

	t.Run("store with tenant ID", func(t *testing.T) {
		data := map[string][]byte{"secret": []byte("tenant-data")}
		metadata := map[string]string{"tenant": tenantID}

		err := eb.StoreWithOptions(ctx, "tenant/secret", data, metadata, &StoreOptions{
			TenantID: tenantID,
		})
		require.NoError(t, err)

		// Verify it was encrypted with tenant key
		keyID, err := eb.GetKeyIdentifier(ctx, "tenant/secret")
		require.NoError(t, err)
		assert.Equal(t, tenantDEKID, keyID)

		// Verify we can retrieve it
		retrieved, _, err := eb.Get(ctx, "tenant/secret")
		require.NoError(t, err)
		assert.Equal(t, data, retrieved)
	})

	t.Run("store with custom key identifier", func(t *testing.T) {
		customKey := make([]byte, 32)
		for i := range customKey {
			customKey[i] = byte(i + 200)
		}
		customKeyID := "custom-key-123"
		eb.encryptor.(*testMockSecretsEncryptor).AddKey(customKeyID, customKey)

		data := map[string][]byte{"secret": []byte("custom-key-data")}

		err := eb.StoreWithOptions(ctx, "custom/secret", data, nil, &StoreOptions{
			KeyIdentifier: customKeyID,
		})
		require.NoError(t, err)

		keyID, err := eb.GetKeyIdentifier(ctx, "custom/secret")
		require.NoError(t, err)
		assert.Equal(t, customKeyID, keyID)
	})

	t.Run("key identifier takes precedence over tenant ID", func(t *testing.T) {
		customKey := make([]byte, 32)
		for i := range customKey {
			customKey[i] = byte(i + 50)
		}
		customKeyID := "precedence-key"
		eb.encryptor.(*testMockSecretsEncryptor).AddKey(customKeyID, customKey)

		data := map[string][]byte{"secret": []byte("precedence-data")}

		err := eb.StoreWithOptions(ctx, "precedence/secret", data, nil, &StoreOptions{
			TenantID:      tenantID,
			KeyIdentifier: customKeyID,
		})
		require.NoError(t, err)

		keyID, err := eb.GetKeyIdentifier(ctx, "precedence/secret")
		require.NoError(t, err)
		assert.Equal(t, customKeyID, keyID)
	})

	t.Run("invalid tenant ID", func(t *testing.T) {
		data := map[string][]byte{"secret": []byte("data")}

		err := eb.StoreWithOptions(ctx, "test/secret", data, nil, &StoreOptions{
			TenantID: "", // Empty tenant ID
		})
		assert.NoError(t, err) // Should use default key instead
	})
}

func TestEncryptedBackend_Versioning(t *testing.T) {
	eb := createTestEncryptedBackend(t)
	ctx := context.Background()

	path := "app/config"

	// Store initial version
	err := eb.Store(ctx, path, map[string][]byte{"key": []byte("value1")}, nil)
	require.NoError(t, err)

	// Store second version
	err = eb.Store(ctx, path, map[string][]byte{"key": []byte("value2")}, nil)
	require.NoError(t, err)

	// Store third version
	err = eb.Store(ctx, path, map[string][]byte{"key": []byte("value3")}, nil)
	require.NoError(t, err)

	t.Run("get latest version", func(t *testing.T) {
		data, _, err := eb.Get(ctx, path)
		require.NoError(t, err)
		assert.Equal(t, []byte("value3"), data["key"])
	})

	t.Run("get specific versions", func(t *testing.T) {
		data, _, err := eb.GetVersion(ctx, path, 1)
		require.NoError(t, err)
		assert.Equal(t, []byte("value1"), data["key"])

		data, _, err = eb.GetVersion(ctx, path, 2)
		require.NoError(t, err)
		assert.Equal(t, []byte("value2"), data["key"])

		data, _, err = eb.GetVersion(ctx, path, 3)
		require.NoError(t, err)
		assert.Equal(t, []byte("value3"), data["key"])
	})

	t.Run("list versions", func(t *testing.T) {
		versions, err := eb.ListVersions(ctx, path)
		require.NoError(t, err)
		assert.Len(t, versions, 3)
		assert.Equal(t, 1, versions[0].Version)
		assert.Equal(t, 2, versions[1].Version)
		assert.Equal(t, 3, versions[2].Version)
	})

	t.Run("invalid version number", func(t *testing.T) {
		_, _, err := eb.GetVersion(ctx, path, 0)
		assert.ErrorIs(t, err, ErrVersionNotFound)

		_, _, err = eb.GetVersion(ctx, path, 99)
		assert.ErrorIs(t, err, ErrVersionNotFound)
	})

	t.Run("invalid path for versioning", func(t *testing.T) {
		_, _, err := eb.GetVersion(ctx, "", 1)
		assert.ErrorIs(t, err, ErrInvalidPath)

		_, err = eb.ListVersions(ctx, "")
		assert.ErrorIs(t, err, ErrInvalidPath)
	})
}

func TestEncryptedBackend_DeleteAndDestroy(t *testing.T) {
	eb := createTestEncryptedBackend(t)
	ctx := context.Background()

	t.Run("soft delete", func(t *testing.T) {
		path := "app/to-delete"
		err := eb.Store(ctx, path, map[string][]byte{"key": []byte("value")}, nil)
		require.NoError(t, err)

		// Soft delete
		err = eb.Delete(ctx, path)
		require.NoError(t, err)

		// Get should return deleted error
		_, _, err = eb.Get(ctx, path)
		assert.ErrorIs(t, err, ErrSecretDeleted)

		// But versions should still exist
		versions, err := eb.ListVersions(ctx, path)
		require.NoError(t, err)
		assert.Len(t, versions, 1)
		assert.True(t, versions[0].Deleted)
	})

	t.Run("delete already deleted secret", func(t *testing.T) {
		path := "app/delete-twice"
		err := eb.Store(ctx, path, map[string][]byte{"key": []byte("value")}, nil)
		require.NoError(t, err)

		err = eb.Delete(ctx, path)
		require.NoError(t, err)

		// Try to delete again
		err = eb.Delete(ctx, path)
		assert.ErrorIs(t, err, ErrSecretDeleted)
	})

	t.Run("permanent destroy", func(t *testing.T) {
		path := "app/to-destroy"
		err := eb.Store(ctx, path, map[string][]byte{"key": []byte("value")}, nil)
		require.NoError(t, err)

		// Destroy
		err = eb.Destroy(ctx, path)
		require.NoError(t, err)

		// Everything should be gone
		_, _, err = eb.Get(ctx, path)
		assert.ErrorIs(t, err, ErrSecretNotFound)

		_, err = eb.ListVersions(ctx, path)
		assert.ErrorIs(t, err, ErrSecretNotFound)
	})

	t.Run("delete with invalid path", func(t *testing.T) {
		err := eb.Delete(ctx, "")
		assert.ErrorIs(t, err, ErrInvalidPath)
	})

	t.Run("destroy with invalid path", func(t *testing.T) {
		err := eb.Destroy(ctx, "")
		assert.ErrorIs(t, err, ErrInvalidPath)
	})
}

func TestEncryptedBackend_Rollback(t *testing.T) {
	eb := createTestEncryptedBackend(t)
	ctx := context.Background()

	path := "app/rollback-test"

	// Create multiple versions
	err := eb.Store(ctx, path, map[string][]byte{"key": []byte("v1")}, nil)
	require.NoError(t, err)
	err = eb.Store(ctx, path, map[string][]byte{"key": []byte("v2")}, nil)
	require.NoError(t, err)
	err = eb.Store(ctx, path, map[string][]byte{"key": []byte("v3")}, nil)
	require.NoError(t, err)

	t.Run("rollback to version 1", func(t *testing.T) {
		err := eb.Rollback(ctx, path, 1)
		require.NoError(t, err)

		// Latest should now be v1's data
		data, _, err := eb.Get(ctx, path)
		require.NoError(t, err)
		assert.Equal(t, []byte("v1"), data["key"])

		// Should now have 4 versions
		versions, err := eb.ListVersions(ctx, path)
		require.NoError(t, err)
		assert.Len(t, versions, 4)
	})

	t.Run("invalid rollback version", func(t *testing.T) {
		err := eb.Rollback(ctx, path, 99)
		assert.ErrorIs(t, err, ErrVersionNotFound)
	})

	t.Run("rollback with invalid path", func(t *testing.T) {
		err := eb.Rollback(ctx, "", 1)
		assert.ErrorIs(t, err, ErrInvalidPath)
	})
}

func TestEncryptedBackend_List(t *testing.T) {
	eb := createTestEncryptedBackend(t)
	ctx := context.Background()

	// Create some secrets
	err := eb.Store(ctx, "prod/db/mysql", map[string][]byte{"pass": []byte("1")}, nil)
	require.NoError(t, err)
	err = eb.Store(ctx, "prod/db/postgres", map[string][]byte{"pass": []byte("2")}, nil)
	require.NoError(t, err)
	err = eb.Store(ctx, "prod/api/key", map[string][]byte{"key": []byte("3")}, nil)
	require.NoError(t, err)
	err = eb.Store(ctx, "dev/db/mysql", map[string][]byte{"pass": []byte("4")}, nil)
	require.NoError(t, err)

	t.Run("list all", func(t *testing.T) {
		paths, err := eb.List(ctx, "")
		require.NoError(t, err)
		assert.Len(t, paths, 4)
	})

	t.Run("list with prefix", func(t *testing.T) {
		paths, err := eb.List(ctx, "prod/")
		require.NoError(t, err)
		assert.Len(t, paths, 3)

		paths, err = eb.List(ctx, "prod/db/")
		require.NoError(t, err)
		assert.Len(t, paths, 2)
	})

	t.Run("list excludes deleted secrets", func(t *testing.T) {
		// Delete one secret
		err := eb.Delete(ctx, "dev/db/mysql")
		require.NoError(t, err)

		// Should not appear in list
		paths, err := eb.List(ctx, "")
		require.NoError(t, err)
		assert.Len(t, paths, 3)
	})
}

func TestEncryptedBackend_GetKeyIdentifier(t *testing.T) {
	eb := createTestEncryptedBackend(t)
	ctx := context.Background()

	t.Run("get key identifier for secret", func(t *testing.T) {
		path := "app/test-keyid"
		err := eb.Store(ctx, path, map[string][]byte{"key": []byte("value")}, nil)
		require.NoError(t, err)

		keyID, err := eb.GetKeyIdentifier(ctx, path)
		require.NoError(t, err)
		assert.Equal(t, string(DEKIDGlobal), keyID)
	})

	t.Run("get key identifier for non-existent secret", func(t *testing.T) {
		_, err := eb.GetKeyIdentifier(ctx, "does/not/exist")
		assert.ErrorIs(t, err, ErrSecretNotFound)
	})

	t.Run("get key identifier with invalid path", func(t *testing.T) {
		_, err := eb.GetKeyIdentifier(ctx, "")
		assert.ErrorIs(t, err, ErrInvalidPath)
	})
}

func TestEncryptedBackend_ReencryptWithKey(t *testing.T) {
	eb := createTestEncryptedBackend(t)
	ctx := context.Background()

	// Add a new key for re-encryption
	newKey := make([]byte, 32)
	for i := range newKey {
		newKey[i] = byte(i + 150)
	}
	newKeyID := "new-encryption-key"
	eb.encryptor.(*testMockSecretsEncryptor).AddKey(newKeyID, newKey)

	t.Run("re-encrypt with new key", func(t *testing.T) {
		path := "app/reencrypt-test"

		// Store with default key
		err := eb.Store(ctx, path, map[string][]byte{"key": []byte("sensitive-data")}, nil)
		require.NoError(t, err)

		// Verify original key
		keyID, err := eb.GetKeyIdentifier(ctx, path)
		require.NoError(t, err)
		assert.Equal(t, string(DEKIDGlobal), keyID)

		// Re-encrypt with new key
		err = eb.ReencryptWithKey(ctx, path, newKeyID)
		require.NoError(t, err)

		// Verify key ID changed
		keyID, err = eb.GetKeyIdentifier(ctx, path)
		require.NoError(t, err)
		assert.Equal(t, newKeyID, keyID)

		// Verify data is still accessible
		data, _, err := eb.Get(ctx, path)
		require.NoError(t, err)
		assert.Equal(t, []byte("sensitive-data"), data["key"])

		// Verify we now have 2 versions
		versions, err := eb.ListVersions(ctx, path)
		require.NoError(t, err)
		assert.Len(t, versions, 2)
	})

	t.Run("re-encrypt with invalid path", func(t *testing.T) {
		err := eb.ReencryptWithKey(ctx, "", newKeyID)
		assert.ErrorIs(t, err, ErrInvalidPath)
	})

	t.Run("re-encrypt with missing key identifier", func(t *testing.T) {
		err := eb.ReencryptWithKey(ctx, "test/path", "")
		assert.ErrorIs(t, err, ErrMissingKeyIdentifier)
	})
}

func TestEncryptedBackend_DeprecatedMethods(t *testing.T) {
	eb := createTestEncryptedBackend(t)
	ctx := context.Background()

	t.Run("StoreWithKey deprecated method", func(t *testing.T) {
		customKey := make([]byte, 32)
		for i := range customKey {
			customKey[i] = byte(i + 75)
		}
		customKeyID := "deprecated-key"
		eb.encryptor.(*testMockSecretsEncryptor).AddKey(customKeyID, customKey)

		data := map[string][]byte{"secret": []byte("data")}
		err := eb.StoreWithKey(ctx, "deprecated/test", data, nil, customKeyID)
		require.NoError(t, err)

		keyID, err := eb.GetKeyIdentifier(ctx, "deprecated/test")
		require.NoError(t, err)
		assert.Equal(t, customKeyID, keyID)
	})

	t.Run("GetKeyID deprecated method", func(t *testing.T) {
		path := "test/deprecated-get"
		err := eb.Store(ctx, path, map[string][]byte{"key": []byte("value")}, nil)
		require.NoError(t, err)

		keyID, err := eb.GetKeyID(ctx, path)
		require.NoError(t, err)
		assert.Equal(t, string(DEKIDGlobal), keyID)
	})
}

func TestEncryptedBackend_StoreWithAlgorithm(t *testing.T) {
	eb := createTestEncryptedBackend(t)
	ctx := context.Background()

	t.Run("Success - store with explicit algorithm", func(t *testing.T) {
		// Add a custom key for testing
		customKey := make([]byte, 32)
		for i := range customKey {
			customKey[i] = byte(i + 42)
		}
		customKeyID := "algo-test-key"
		eb.encryptor.(*testMockSecretsEncryptor).AddKey(customKeyID, customKey)

		// Store with explicit AES-256-GCM algorithm
		data := map[string][]byte{
			"api_key":    []byte("secret-api-key-12345"),
			"api_secret": []byte("ultra-secret-value"),
		}
		metadata := map[string]string{
			"service": "payment-gateway",
			"env":     "production",
		}

		err := eb.StoreWithAlgorithm(ctx, "services/payment/creds", data, metadata, customKeyID, "AES-256-GCM")
		require.NoError(t, err)

		// Verify the secret was stored correctly
		retrieved, retrievedMeta, err := eb.Get(ctx, "services/payment/creds")
		require.NoError(t, err)
		assert.Equal(t, data, retrieved)
		// Check expected metadata fields (version is added by GetVersion)
		assert.Equal(t, metadata["service"], retrievedMeta["service"])
		assert.Equal(t, metadata["env"], retrievedMeta["env"])

		// Verify the correct key identifier was used
		keyID, err := eb.GetKeyIdentifier(ctx, "services/payment/creds")
		require.NoError(t, err)
		assert.Equal(t, customKeyID, keyID)
	})

	t.Run("ChaCha20 - store with ChaCha20-Poly1305 algorithm", func(t *testing.T) {
		// Add a key for ChaCha20 testing
		chachaKey := make([]byte, 32)
		for i := range chachaKey {
			chachaKey[i] = byte(i + 100)
		}
		chachaKeyID := "chacha-key"
		eb.encryptor.(*testMockSecretsEncryptor).AddKey(chachaKeyID, chachaKey)

		// Store with CHACHA20-POLY1305 algorithm
		data := map[string][]byte{
			"database_password": []byte("super-secure-password"),
			"connection_string": []byte("postgres://user:pass@host:5432/db"),
		}
		metadata := map[string]string{
			"algorithm": "CHACHA20-POLY1305",
			"purpose":   "database-credentials",
		}

		err := eb.StoreWithAlgorithm(ctx, "db/primary/credentials", data, metadata, chachaKeyID, "CHACHA20-POLY1305")
		require.NoError(t, err)

		// Verify retrieval works
		retrieved, retrievedMeta, err := eb.Get(ctx, "db/primary/credentials")
		require.NoError(t, err)
		assert.Equal(t, data, retrieved)
		// Check expected metadata fields (version is added by GetVersion)
		assert.Equal(t, metadata["algorithm"], retrievedMeta["algorithm"])
		assert.Equal(t, metadata["purpose"], retrievedMeta["purpose"])

		// Verify the correct key was used
		keyID, err := eb.GetKeyIdentifier(ctx, "db/primary/credentials")
		require.NoError(t, err)
		assert.Equal(t, chachaKeyID, keyID)
	})

	t.Run("EmptyPath - error case with empty path", func(t *testing.T) {
		data := map[string][]byte{"key": []byte("value")}
		err := eb.StoreWithAlgorithm(ctx, "", data, nil, string(DEKIDGlobal), "AES-256-GCM")
		assert.ErrorIs(t, err, ErrInvalidPath)
	})

	t.Run("EmptyData - error case with empty data", func(t *testing.T) {
		err := eb.StoreWithAlgorithm(ctx, "test/empty-data", nil, nil, string(DEKIDGlobal), "AES-256-GCM")
		assert.ErrorIs(t, err, ErrEmptyData)

		// Also test with empty map
		err = eb.StoreWithAlgorithm(ctx, "test/empty-map", map[string][]byte{}, nil, string(DEKIDGlobal), "AES-256-GCM")
		assert.ErrorIs(t, err, ErrEmptyData)
	})

	t.Run("UnsupportedAlgorithm - error case with invalid algorithm", func(t *testing.T) {
		data := map[string][]byte{"key": []byte("value")}
		err := eb.StoreWithAlgorithm(ctx, "test/invalid-algo", data, nil, string(DEKIDGlobal), "INVALID-ALGORITHM")
		assert.ErrorIs(t, err, ErrUnsupportedAlgorithm)
	})

	t.Run("ContextCancellation - error case with cancelled context", func(t *testing.T) {
		cancelledCtx, cancel := context.WithCancel(context.Background())
		cancel() // Cancel immediately

		data := map[string][]byte{"key": []byte("value")}
		err := eb.StoreWithAlgorithm(cancelledCtx, "test/cancelled", data, nil, string(DEKIDGlobal), "AES-256-GCM")
		assert.Error(t, err)
		assert.Equal(t, context.Canceled, err)
	})

	t.Run("VersionCreation - verify version is created", func(t *testing.T) {
		// Store initial version with default algorithm
		data1 := map[string][]byte{"version": []byte("1")}
		err := eb.Store(ctx, "test/versioning", data1, nil)
		require.NoError(t, err)

		// Store second version with explicit algorithm
		data2 := map[string][]byte{"version": []byte("2")}
		err = eb.StoreWithAlgorithm(ctx, "test/versioning", data2, nil, string(DEKIDGlobal), "AES-128-GCM")
		require.NoError(t, err)

		// Verify we have 2 versions
		versions, err := eb.ListVersions(ctx, "test/versioning")
		require.NoError(t, err)
		assert.Len(t, versions, 2)

		// Verify the latest version has the new data
		retrieved, _, err := eb.Get(ctx, "test/versioning")
		require.NoError(t, err)
		assert.Equal(t, data2, retrieved)
	})
}

func TestEncryptedBackend_Close(t *testing.T) {
	eb := createTestEncryptedBackend(t)
	err := eb.Close()
	assert.NoError(t, err)
}

func TestEncryptedBackend_Type(t *testing.T) {
	eb := createTestEncryptedBackend(t)
	assert.Equal(t, "encrypted", eb.Type())
}

func TestEncryptedBackend_ContextCancellation(t *testing.T) {
	eb := createTestEncryptedBackend(t)

	t.Run("store with cancelled context", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel() // Cancel immediately

		data := map[string][]byte{"key": []byte("value")}
		err := eb.Store(ctx, "test/cancelled", data, nil)
		assert.Error(t, err)
		assert.Equal(t, context.Canceled, err)
	})

	t.Run("get with cancelled context", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel() // Cancel immediately

		_, _, err := eb.Get(ctx, "test/path")
		assert.Error(t, err)
		assert.Equal(t, context.Canceled, err)
	})
}

// TestEncryptedBackend_DecryptEntry_OldFormat tests backward compatibility with legacy KeyID field.
func TestEncryptedBackend_DecryptEntry_OldFormat(t *testing.T) {
	eb := createTestEncryptedBackend(t)
	ctx := context.Background()

	t.Run("legacy KeyID field migration", func(t *testing.T) {
		// Create a secret data manually using the old format with KeyID instead of KeyIdentifier
		data := map[string][]byte{"key": []byte("legacy-value")}
		plaintext, err := json.Marshal(data)
		require.NoError(t, err)

		// Encrypt using the encryptor directly
		blob, err := eb.encryptor.Encrypt(ctx, string(DEKIDGlobal), plaintext, &interfaces.EncryptOptions{
			Algorithm: DefaultAlgorithm,
			AAD:       []byte("legacy/path"),
		})
		require.NoError(t, err)

		// Create an old-format encryptedEntry with KeyID field instead of KeyIdentifier
		now := time.Now()
		oldFormatEntry := &encryptedEntry{
			// KeyIdentifier is intentionally empty to test backward compatibility
			KeyID:       string(DEKIDGlobal), // Old field
			KeyVersion:  blob.KeyVersion,
			Algorithm:   blob.Algorithm,
			Nonce:       blob.Nonce,
			Ciphertext:  blob.Ciphertext,
			Tag:         blob.Tag,
			AAD:         blob.AAD,
			EncryptedAt: blob.EncryptedAt,
			Metadata:    map[string]string{"format": "legacy"},
			Version:     1,
			Deleted:     false,
			CreatedAt:   now,
		}

		// Test decryption of old format entry
		decrypted, err := eb.decryptEntry(ctx, oldFormatEntry)
		require.NoError(t, err, "decryptEntry should handle legacy KeyID field")
		assert.Equal(t, data, decrypted)
	})

	t.Run("KeyIdentifier takes precedence over legacy KeyID", func(t *testing.T) {
		// Create a test entry with both fields set
		data := map[string][]byte{"key": []byte("test-value")}
		plaintext, err := json.Marshal(data)
		require.NoError(t, err)

		blob, err := eb.encryptor.Encrypt(ctx, string(DEKIDGlobal), plaintext, nil)
		require.NoError(t, err)

		now := time.Now()
		entry := &encryptedEntry{
			KeyIdentifier: string(DEKIDGlobal), // New field (should be used)
			KeyID:         "old-wrong-key",     // Old field (should be ignored)
			KeyVersion:    blob.KeyVersion,
			Algorithm:     blob.Algorithm,
			Nonce:         blob.Nonce,
			Ciphertext:    blob.Ciphertext,
			Tag:           blob.Tag,
			AAD:           blob.AAD,
			EncryptedAt:   blob.EncryptedAt,
			Version:       1,
			CreatedAt:     now,
		}

		// Should use KeyIdentifier and succeed
		decrypted, err := eb.decryptEntry(ctx, entry)
		require.NoError(t, err)
		assert.Equal(t, data, decrypted)
	})
}

// TestEncryptedBackend_DecryptEntry_InvalidBlob tests handling of corrupted ciphertext.
func TestEncryptedBackend_DecryptEntry_InvalidBlob(t *testing.T) {
	eb := createTestEncryptedBackend(t)
	ctx := context.Background()

	t.Run("corrupted ciphertext", func(t *testing.T) {
		// Create a valid encrypted entry first
		data := map[string][]byte{"key": []byte("value")}
		plaintext, err := json.Marshal(data)
		require.NoError(t, err)

		blob, err := eb.encryptor.Encrypt(ctx, string(DEKIDGlobal), plaintext, nil)
		require.NoError(t, err)

		// Corrupt the ciphertext
		corruptedCiphertext := make([]byte, len(blob.Ciphertext))
		copy(corruptedCiphertext, blob.Ciphertext)
		// Flip some bits to corrupt the data
		if len(corruptedCiphertext) > 0 {
			corruptedCiphertext[0] ^= 0xFF
		}
		if len(corruptedCiphertext) > 1 {
			corruptedCiphertext[1] ^= 0xFF
		}

		now := time.Now()
		corruptedEntry := &encryptedEntry{
			KeyIdentifier: blob.KeyIdentifier,
			KeyVersion:    blob.KeyVersion,
			Algorithm:     blob.Algorithm,
			Nonce:         blob.Nonce,
			Ciphertext:    corruptedCiphertext, // Corrupted
			Tag:           blob.Tag,
			AAD:           blob.AAD,
			EncryptedAt:   blob.EncryptedAt,
			Version:       1,
			CreatedAt:     now,
		}

		// Decryption should fail
		_, err = eb.decryptEntry(ctx, corruptedEntry)
		assert.Error(t, err, "decryption should fail with corrupted ciphertext")
		assert.Contains(t, err.Error(), "decryption failed")
	})

	t.Run("corrupted authentication tag", func(t *testing.T) {
		data := map[string][]byte{"key": []byte("value")}
		plaintext, err := json.Marshal(data)
		require.NoError(t, err)

		blob, err := eb.encryptor.Encrypt(ctx, string(DEKIDGlobal), plaintext, nil)
		require.NoError(t, err)

		// Corrupt the tag
		corruptedTag := make([]byte, len(blob.Tag))
		copy(corruptedTag, blob.Tag)
		if len(corruptedTag) > 0 {
			corruptedTag[0] ^= 0xFF
		}

		now := time.Now()
		entry := &encryptedEntry{
			KeyIdentifier: blob.KeyIdentifier,
			KeyVersion:    blob.KeyVersion,
			Algorithm:     blob.Algorithm,
			Nonce:         blob.Nonce,
			Ciphertext:    blob.Ciphertext,
			Tag:           corruptedTag, // Corrupted
			AAD:           blob.AAD,
			EncryptedAt:   blob.EncryptedAt,
			Version:       1,
			CreatedAt:     now,
		}

		// Decryption should fail due to authentication tag mismatch
		_, err = eb.decryptEntry(ctx, entry)
		assert.Error(t, err, "decryption should fail with corrupted tag")
	})

	t.Run("invalid JSON after decryption", func(t *testing.T) {
		// Create encrypted entry with invalid JSON data
		invalidJSON := []byte("{invalid json")

		blob, err := eb.encryptor.Encrypt(ctx, string(DEKIDGlobal), invalidJSON, nil)
		require.NoError(t, err)

		now := time.Now()
		entry := &encryptedEntry{
			KeyIdentifier: blob.KeyIdentifier,
			KeyVersion:    blob.KeyVersion,
			Algorithm:     blob.Algorithm,
			Nonce:         blob.Nonce,
			Ciphertext:    blob.Ciphertext,
			Tag:           blob.Tag,
			AAD:           blob.AAD,
			EncryptedAt:   blob.EncryptedAt,
			Version:       1,
			CreatedAt:     now,
		}

		// Decryption succeeds but JSON unmarshaling should fail
		_, err = eb.decryptEntry(ctx, entry)
		assert.Error(t, err)
		assert.ErrorIs(t, err, ErrDeserializationFailed)
	})
}

// TestEncryptedBackend_DecryptEntry_EmptyNonce tests handling of missing or empty nonce.
func TestEncryptedBackend_DecryptEntry_EmptyNonce(t *testing.T) {
	eb := createTestEncryptedBackend(t)
	ctx := context.Background()

	t.Run("empty nonce", func(t *testing.T) {
		data := map[string][]byte{"key": []byte("value")}
		plaintext, err := json.Marshal(data)
		require.NoError(t, err)

		blob, err := eb.encryptor.Encrypt(ctx, string(DEKIDGlobal), plaintext, nil)
		require.NoError(t, err)

		now := time.Now()
		entry := &encryptedEntry{
			KeyIdentifier: blob.KeyIdentifier,
			KeyVersion:    blob.KeyVersion,
			Algorithm:     blob.Algorithm,
			Nonce:         nil, // Empty nonce
			Ciphertext:    blob.Ciphertext,
			Tag:           blob.Tag,
			AAD:           blob.AAD,
			EncryptedAt:   blob.EncryptedAt,
			Version:       1,
			CreatedAt:     now,
		}

		// Should fail blob validation or decryption
		_, err = eb.decryptEntry(ctx, entry)
		assert.Error(t, err, "decryption should fail with empty nonce")
	})

	t.Run("zero-length nonce", func(t *testing.T) {
		data := map[string][]byte{"key": []byte("value")}
		plaintext, err := json.Marshal(data)
		require.NoError(t, err)

		blob, err := eb.encryptor.Encrypt(ctx, string(DEKIDGlobal), plaintext, nil)
		require.NoError(t, err)

		now := time.Now()
		entry := &encryptedEntry{
			KeyIdentifier: blob.KeyIdentifier,
			KeyVersion:    blob.KeyVersion,
			Algorithm:     blob.Algorithm,
			Nonce:         []byte{}, // Zero-length nonce
			Ciphertext:    blob.Ciphertext,
			Tag:           blob.Tag,
			AAD:           blob.AAD,
			EncryptedAt:   blob.EncryptedAt,
			Version:       1,
			CreatedAt:     now,
		}

		_, err = eb.decryptEntry(ctx, entry)
		assert.Error(t, err, "decryption should fail with zero-length nonce")
	})
}

// TestEncryptedBackend_DecryptEntry_KeyNotFound tests handling when key identifier doesn't exist.
func TestEncryptedBackend_DecryptEntry_KeyNotFound(t *testing.T) {
	eb := createTestEncryptedBackend(t)
	ctx := context.Background()

	t.Run("non-existent key identifier", func(t *testing.T) {
		data := map[string][]byte{"key": []byte("value")}
		plaintext, err := json.Marshal(data)
		require.NoError(t, err)

		// Encrypt with valid key
		blob, err := eb.encryptor.Encrypt(ctx, string(DEKIDGlobal), plaintext, nil)
		require.NoError(t, err)

		now := time.Now()
		entry := &encryptedEntry{
			KeyIdentifier: "non-existent-key-id", // Key that doesn't exist
			KeyVersion:    blob.KeyVersion,
			Algorithm:     blob.Algorithm,
			Nonce:         blob.Nonce,
			Ciphertext:    blob.Ciphertext,
			Tag:           blob.Tag,
			AAD:           blob.AAD,
			EncryptedAt:   blob.EncryptedAt,
			Version:       1,
			CreatedAt:     now,
		}

		// Decryption should fail with key not found error
		_, err = eb.decryptEntry(ctx, entry)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "decryption failed")
		// The underlying error should be ErrKeyNotFound from interfaces
	})

	t.Run("empty key identifier and empty legacy KeyID", func(t *testing.T) {
		now := time.Now()
		entry := &encryptedEntry{
			KeyIdentifier: "", // Empty
			KeyID:         "", // Also empty
			KeyVersion:    1,
			Algorithm:     DefaultAlgorithm,
			Nonce:         []byte("test-nonce"),
			Ciphertext:    []byte("test-ciphertext"),
			Tag:           []byte("test-tag"),
			EncryptedAt:   now,
			Version:       1,
			CreatedAt:     now,
		}

		// Should fail with missing key identifier error
		_, err := eb.decryptEntry(ctx, entry)
		assert.Error(t, err)
		assert.ErrorIs(t, err, ErrMissingKeyIdentifier)
	})
}

// TestEncryptedBackend_DecryptEntry_ContextCancellation tests context handling in decryptEntry.
func TestEncryptedBackend_DecryptEntry_ContextCancellation(t *testing.T) {
	eb := createTestEncryptedBackend(t)

	t.Run("cancelled context", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel() // Cancel immediately

		data := map[string][]byte{"key": []byte("value")}
		plaintext, err := json.Marshal(data)
		require.NoError(t, err)

		// Create a valid blob structure
		now := time.Now()
		entry := &encryptedEntry{
			KeyIdentifier: string(DEKIDGlobal),
			KeyVersion:    1,
			Algorithm:     DefaultAlgorithm,
			Nonce:         []byte("test-nonce"),
			Ciphertext:    plaintext, // Use plaintext directly for simplicity
			Tag:           []byte("test-tag"),
			EncryptedAt:   now,
			Version:       1,
			CreatedAt:     now,
		}

		// Should return context canceled error
		_, err = eb.decryptEntry(ctx, entry)
		assert.Error(t, err)
		assert.Equal(t, context.Canceled, err)
	})
}

// TestEncryptedBackend_SaveVersions_ErrorPaths tests error handling in saveVersions.
func TestEncryptedBackend_SaveVersions_ErrorPaths(t *testing.T) {
	t.Run("store Put error", func(t *testing.T) {
		store := newTestMockKVStore()
		encryptor := newTestMockSecretsEncryptor()

		// Mock store to return error on Put
		putError := errors.New("storage unavailable")
		store.putFn = func(ctx context.Context, key string, value []byte) error {
			return putError
		}

		eb, err := NewEncryptedBackend(&EncryptedBackendOptions{
			Store:     store,
			Encryptor: encryptor,
		})
		require.NoError(t, err)

		ctx := context.Background()
		data := map[string][]byte{"key": []byte("value")}

		// Should fail with Put error
		err = eb.Store(ctx, "test/path", data, nil)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "failed to save secret")
	})

	t.Run("corrupted data in storage - invalid JSON", func(t *testing.T) {
		store := newTestMockKVStore()
		encryptor := newTestMockSecretsEncryptor()

		eb, err := NewEncryptedBackend(&EncryptedBackendOptions{
			Store:     store,
			Encryptor: encryptor,
		})
		require.NoError(t, err)

		ctx := context.Background()

		// Manually insert corrupted JSON data
		corruptedJSON := []byte("{this is not valid json")
		err = store.Put(ctx, secretKey("test/corrupted"), corruptedJSON)
		require.NoError(t, err)

		// Try to update the secret (which will try to load existing versions)
		data := map[string][]byte{"key": []byte("new-value")}
		err = eb.Store(ctx, "test/corrupted", data, nil)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "failed to load existing versions")
	})
}

// TestEncryptedBackend_LoadVersions_ErrorPaths tests error handling in loadVersions.
func TestEncryptedBackend_LoadVersions_ErrorPaths(t *testing.T) {
	t.Run("invalid JSON in storage", func(t *testing.T) {
		store := newTestMockKVStore()
		encryptor := newTestMockSecretsEncryptor()

		eb, err := NewEncryptedBackend(&EncryptedBackendOptions{
			Store:     store,
			Encryptor: encryptor,
		})
		require.NoError(t, err)

		ctx := context.Background()

		// Insert invalid JSON directly
		invalidJSON := []byte("not valid json at all")
		err = store.Put(ctx, secretKey("test/invalid"), invalidJSON)
		require.NoError(t, err)

		// Try to load the corrupted versions
		_, err = eb.loadVersions(ctx, "test/invalid")
		assert.Error(t, err)
		assert.ErrorIs(t, err, ErrDeserializationFailed)
	})

	t.Run("empty data in storage", func(t *testing.T) {
		store := newTestMockKVStore()
		encryptor := newTestMockSecretsEncryptor()

		eb, err := NewEncryptedBackend(&EncryptedBackendOptions{
			Store:     store,
			Encryptor: encryptor,
		})
		require.NoError(t, err)

		ctx := context.Background()

		// Insert empty data
		err = store.Put(ctx, secretKey("test/empty"), []byte(""))
		require.NoError(t, err)

		// Try to load the empty versions
		_, err = eb.loadVersions(ctx, "test/empty")
		assert.Error(t, err)
		assert.ErrorIs(t, err, ErrDeserializationFailed)
	})

	t.Run("QBError not found handling", func(t *testing.T) {
		store := newTestMockKVStore()
		encryptor := newTestMockSecretsEncryptor()

		// Mock Get to return QBError with ErrNotFound
		store.getFn = func(ctx context.Context, key string) ([]byte, error) {
			return nil, dberrors.New(dberrors.ErrNotFound, "Get", "key not found")
		}

		eb, err := NewEncryptedBackend(&EncryptedBackendOptions{
			Store:     store,
			Encryptor: encryptor,
		})
		require.NoError(t, err)

		ctx := context.Background()

		// Should return ErrSecretNotFound
		_, err = eb.loadVersions(ctx, "test/notfound")
		assert.Error(t, err)
		assert.ErrorIs(t, err, ErrSecretNotFound)
	})

	t.Run("generic store error handling", func(t *testing.T) {
		store := newTestMockKVStore()
		encryptor := newTestMockSecretsEncryptor()

		// Mock Get to return a generic error
		genericError := errors.New("database connection lost")
		store.getFn = func(ctx context.Context, key string) ([]byte, error) {
			return nil, genericError
		}

		eb, err := NewEncryptedBackend(&EncryptedBackendOptions{
			Store:     store,
			Encryptor: encryptor,
		})
		require.NoError(t, err)

		ctx := context.Background()

		// Should return ErrSecretNotFound for generic errors
		_, err = eb.loadVersions(ctx, "test/error")
		assert.Error(t, err)
		assert.ErrorIs(t, err, ErrSecretNotFound)
	})
}

// TestEncryptedBackend_GetVersion_ErrorPaths tests error handling in GetVersion.
func TestEncryptedBackend_GetVersion_ErrorPaths(t *testing.T) {
	eb := createTestEncryptedBackend(t)
	ctx := context.Background()

	// Setup: Create a secret with multiple versions
	path := "test/version-errors"
	err := eb.Store(ctx, path, map[string][]byte{"v": []byte("1")}, nil)
	require.NoError(t, err)
	err = eb.Store(ctx, path, map[string][]byte{"v": []byte("2")}, nil)
	require.NoError(t, err)

	t.Run("version less than 1", func(t *testing.T) {
		_, _, err := eb.GetVersion(ctx, path, 0)
		assert.Error(t, err)
		assert.ErrorIs(t, err, ErrVersionNotFound)
	})

	t.Run("version exceeds available versions", func(t *testing.T) {
		_, _, err := eb.GetVersion(ctx, path, 100)
		assert.Error(t, err)
		assert.ErrorIs(t, err, ErrVersionNotFound)
	})

	t.Run("get deleted version", func(t *testing.T) {
		// Create a secret and delete it
		deletePath := "test/deleted-version"
		err := eb.Store(ctx, deletePath, map[string][]byte{"key": []byte("value")}, nil)
		require.NoError(t, err)

		// Manually mark version as deleted
		versions, err := eb.loadVersions(ctx, deletePath)
		require.NoError(t, err)
		versions.Versions[0].Deleted = true
		now := time.Now()
		versions.Versions[0].DeletedAt = &now
		err = eb.saveVersions(ctx, deletePath, versions)
		require.NoError(t, err)

		// Try to get the deleted version
		_, _, err = eb.GetVersion(ctx, deletePath, 1)
		assert.Error(t, err)
		assert.ErrorIs(t, err, ErrVersionDestroyed)
	})

	t.Run("cancelled context", func(t *testing.T) {
		cancelledCtx, cancel := context.WithCancel(context.Background())
		cancel()

		_, _, err := eb.GetVersion(cancelledCtx, path, 1)
		assert.Error(t, err)
		assert.Equal(t, context.Canceled, err)
	})

	t.Run("secret not found", func(t *testing.T) {
		_, _, err := eb.GetVersion(ctx, "does/not/exist", 1)
		assert.Error(t, err)
		assert.ErrorIs(t, err, ErrSecretNotFound)
	})

	t.Run("decryption error in GetVersion", func(t *testing.T) {
		// Create a secret
		decryptPath := "test/decrypt-error"
		err := eb.Store(ctx, decryptPath, map[string][]byte{"key": []byte("value")}, nil)
		require.NoError(t, err)

		// Manually corrupt the ciphertext in storage
		versions, err := eb.loadVersions(ctx, decryptPath)
		require.NoError(t, err)
		if len(versions.Versions[0].Ciphertext) > 0 {
			versions.Versions[0].Ciphertext[0] ^= 0xFF
		}
		err = eb.saveVersions(ctx, decryptPath, versions)
		require.NoError(t, err)

		// Try to get the corrupted version
		_, _, err = eb.GetVersion(ctx, decryptPath, 1)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "decryption failed")
	})
}

// TestEncryptedBackend_List_ErrorPaths tests error handling in List.
func TestEncryptedBackend_List_ErrorPaths(t *testing.T) {
	t.Run("cancelled context", func(t *testing.T) {
		eb := createTestEncryptedBackend(t)
		cancelledCtx, cancel := context.WithCancel(context.Background())
		cancel()

		_, err := eb.List(cancelledCtx, "")
		assert.Error(t, err)
		assert.Equal(t, context.Canceled, err)
	})

	t.Run("store List error", func(t *testing.T) {
		store := newTestMockKVStore()
		encryptor := newTestMockSecretsEncryptor()

		// Mock store to return error on List
		listError := errors.New("storage failure")
		store.listFn = func(ctx context.Context, prefix string) ([]string, error) {
			return nil, listError
		}

		eb, err := NewEncryptedBackend(&EncryptedBackendOptions{
			Store:     store,
			Encryptor: encryptor,
		})
		require.NoError(t, err)

		ctx := context.Background()

		// Should return the list error
		_, err = eb.List(ctx, "")
		assert.Error(t, err)
		assert.Equal(t, listError, err)
	})

	t.Run("corrupted version data during list", func(t *testing.T) {
		store := newTestMockKVStore()
		encryptor := newTestMockSecretsEncryptor()

		eb, err := NewEncryptedBackend(&EncryptedBackendOptions{
			Store:     store,
			Encryptor: encryptor,
		})
		require.NoError(t, err)

		ctx := context.Background()

		// Create a valid secret
		err = eb.Store(ctx, "valid/path", map[string][]byte{"key": []byte("value")}, nil)
		require.NoError(t, err)

		// Create a corrupted secret directly in storage
		corruptedJSON := []byte("invalid json")
		err = store.Put(ctx, secretKey("corrupted/path"), corruptedJSON)
		require.NoError(t, err)

		// List should skip the corrupted entry
		paths, err := eb.List(ctx, "")
		require.NoError(t, err)
		// Should only contain the valid path, not the corrupted one
		assert.Contains(t, paths, "valid/path")
		assert.NotContains(t, paths, "corrupted/path")
	})

	t.Run("list with specific prefix", func(t *testing.T) {
		eb := createTestEncryptedBackend(t)
		ctx := context.Background()

		// Create secrets with different prefixes
		err := eb.Store(ctx, "app1/secret1", map[string][]byte{"key": []byte("1")}, nil)
		require.NoError(t, err)
		err = eb.Store(ctx, "app1/secret2", map[string][]byte{"key": []byte("2")}, nil)
		require.NoError(t, err)
		err = eb.Store(ctx, "app2/secret3", map[string][]byte{"key": []byte("3")}, nil)
		require.NoError(t, err)

		// List with prefix should only return matching paths
		paths, err := eb.List(ctx, "app1/")
		require.NoError(t, err)
		assert.Len(t, paths, 2)
		assert.Contains(t, paths, "app1/secret1")
		assert.Contains(t, paths, "app1/secret2")
		assert.NotContains(t, paths, "app2/secret3")
	})

	t.Run("list with non-existent prefix", func(t *testing.T) {
		eb := createTestEncryptedBackend(t)
		ctx := context.Background()

		// List with prefix that doesn't match any secrets
		paths, err := eb.List(ctx, "nonexistent/")
		require.NoError(t, err)
		assert.Len(t, paths, 0)
	})
}
