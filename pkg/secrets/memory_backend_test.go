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
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewMemoryBackend(t *testing.T) {
	backend := NewMemoryBackend()
	require.NotNil(t, backend)
	assert.Equal(t, "memory", backend.Type())
}

func TestMemoryBackend_Store(t *testing.T) {
	t.Run("ValidStore", func(t *testing.T) {
		backend := NewMemoryBackend()
		defer backend.Close()

		ctx := context.Background()
		data := map[string][]byte{"password": []byte("secret123")}
		metadata := map[string]string{"owner": "test"}

		err := backend.Store(ctx, "app/db/password", data, metadata)
		require.NoError(t, err)
	})

	t.Run("EmptyPath", func(t *testing.T) {
		backend := NewMemoryBackend()
		defer backend.Close()

		ctx := context.Background()
		data := map[string][]byte{"password": []byte("secret123")}

		err := backend.Store(ctx, "", data, nil)
		assert.Equal(t, ErrInvalidPath, err)
	})

	t.Run("NilData", func(t *testing.T) {
		backend := NewMemoryBackend()
		defer backend.Close()

		ctx := context.Background()

		err := backend.Store(ctx, "test/path", nil, nil)
		assert.Equal(t, ErrEmptyData, err)
	})

	t.Run("EmptyData", func(t *testing.T) {
		backend := NewMemoryBackend()
		defer backend.Close()

		ctx := context.Background()
		data := map[string][]byte{}

		err := backend.Store(ctx, "test/path", data, nil)
		assert.Equal(t, ErrEmptyData, err)
	})

	t.Run("CanceledContext", func(t *testing.T) {
		backend := NewMemoryBackend()
		defer backend.Close()

		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		data := map[string][]byte{"password": []byte("secret123")}

		err := backend.Store(ctx, "test/path", data, nil)
		require.Error(t, err)
	})

	t.Run("MultipleVersions", func(t *testing.T) {
		backend := NewMemoryBackend()
		defer backend.Close()

		ctx := context.Background()
		path := "app/secret"

		// Store version 1
		data1 := map[string][]byte{"key": []byte("value1")}
		err := backend.Store(ctx, path, data1, nil)
		require.NoError(t, err)

		// Store version 2
		data2 := map[string][]byte{"key": []byte("value2")}
		err = backend.Store(ctx, path, data2, nil)
		require.NoError(t, err)

		// Verify latest version
		retrieved, _, err := backend.Get(ctx, path)
		require.NoError(t, err)
		assert.Equal(t, "value2", string(retrieved["key"]))
	})

	t.Run("DataIsolation", func(t *testing.T) {
		backend := NewMemoryBackend()
		defer backend.Close()

		ctx := context.Background()
		path := "app/secret"
		originalData := map[string][]byte{"key": []byte("original")}

		require.NoError(t, backend.Store(ctx, path, originalData, nil))

		// Modify original data after storing
		originalData["key"] = []byte("modified")

		// Retrieved data should still be original
		data, _, err := backend.Get(ctx, path)
		require.NoError(t, err)
		assert.Equal(t, "original", string(data["key"]))
	})

	t.Run("NilMetadata", func(t *testing.T) {
		backend := NewMemoryBackend()
		defer backend.Close()

		ctx := context.Background()
		data := map[string][]byte{"key": []byte("value")}

		err := backend.Store(ctx, "test/path", data, nil)
		require.NoError(t, err)

		_, meta, err := backend.Get(ctx, "test/path")
		require.NoError(t, err)
		// Even with nil input metadata, version is always added
		assert.NotNil(t, meta)
		assert.Equal(t, "1", meta["version"])
	})
}

func TestMemoryBackend_Get(t *testing.T) {
	t.Run("ValidGet", func(t *testing.T) {
		backend := NewMemoryBackend()
		defer backend.Close()

		ctx := context.Background()
		path := "app/secret"
		originalData := map[string][]byte{
			"username": []byte("admin"),
			"password": []byte("secret123"),
		}
		metadata := map[string]string{"created_by": "test"}

		require.NoError(t, backend.Store(ctx, path, originalData, metadata))

		data, meta, err := backend.Get(ctx, path)
		require.NoError(t, err)
		assert.Equal(t, "admin", string(data["username"]))
		assert.Equal(t, "secret123", string(data["password"]))
		assert.Equal(t, "test", meta["created_by"])
	})

	t.Run("NotFound", func(t *testing.T) {
		backend := NewMemoryBackend()
		defer backend.Close()

		ctx := context.Background()
		_, _, err := backend.Get(ctx, "nonexistent/path")
		assert.Equal(t, ErrSecretNotFound, err)
	})

	t.Run("EmptyPath", func(t *testing.T) {
		backend := NewMemoryBackend()
		defer backend.Close()

		ctx := context.Background()
		_, _, err := backend.Get(ctx, "")
		assert.Equal(t, ErrInvalidPath, err)
	})

	t.Run("CanceledContext", func(t *testing.T) {
		backend := NewMemoryBackend()
		defer backend.Close()

		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		_, _, err := backend.Get(ctx, "test/path")
		require.Error(t, err)
	})

	t.Run("DeletedSecret", func(t *testing.T) {
		backend := NewMemoryBackend()
		defer backend.Close()

		ctx := context.Background()
		path := "app/secret"
		data := map[string][]byte{"key": []byte("value")}

		require.NoError(t, backend.Store(ctx, path, data, nil))
		require.NoError(t, backend.Delete(ctx, path))

		_, _, err := backend.Get(ctx, path)
		assert.Equal(t, ErrSecretDeleted, err)
	})

	t.Run("DataIsolation", func(t *testing.T) {
		backend := NewMemoryBackend()
		defer backend.Close()

		ctx := context.Background()
		path := "app/secret"
		originalData := map[string][]byte{"key": []byte("original")}

		require.NoError(t, backend.Store(ctx, path, originalData, nil))

		// Modify original data
		originalData["key"] = []byte("modified")

		// Retrieved data should still be original
		data, _, err := backend.Get(ctx, path)
		require.NoError(t, err)
		assert.Equal(t, "original", string(data["key"]))
	})

	t.Run("MetadataIsolation", func(t *testing.T) {
		backend := NewMemoryBackend()
		defer backend.Close()

		ctx := context.Background()
		path := "app/secret"
		data := map[string][]byte{"key": []byte("value")}
		metadata := map[string]string{"owner": "test"}

		require.NoError(t, backend.Store(ctx, path, data, metadata))

		// Modify original metadata
		metadata["owner"] = "modified"

		// Retrieved metadata should still be original
		_, meta, err := backend.Get(ctx, path)
		require.NoError(t, err)
		assert.Equal(t, "test", meta["owner"])
	})
}

func TestMemoryBackend_GetVersion(t *testing.T) {
	t.Run("ValidVersion", func(t *testing.T) {
		backend := NewMemoryBackend()
		defer backend.Close()

		ctx := context.Background()
		path := "app/secret"

		// Store multiple versions
		for i := 1; i <= 3; i++ {
			data := map[string][]byte{"version": {byte('0' + i)}}
			require.NoError(t, backend.Store(ctx, path, data, nil))
		}

		// Get version 2
		data, _, err := backend.GetVersion(ctx, path, 2)
		require.NoError(t, err)
		assert.Equal(t, "2", string(data["version"]))
	})

	t.Run("InvalidVersion", func(t *testing.T) {
		backend := NewMemoryBackend()
		defer backend.Close()

		ctx := context.Background()
		path := "app/secret"

		data := map[string][]byte{"key": []byte("value")}
		require.NoError(t, backend.Store(ctx, path, data, nil))

		// Version 0
		_, _, err := backend.GetVersion(ctx, path, 0)
		assert.Equal(t, ErrVersionNotFound, err)

		// Negative version
		_, _, err = backend.GetVersion(ctx, path, -1)
		assert.Equal(t, ErrVersionNotFound, err)

		// Version too high
		_, _, err = backend.GetVersion(ctx, path, 99)
		assert.Equal(t, ErrVersionNotFound, err)
	})

	t.Run("SecretNotFound", func(t *testing.T) {
		backend := NewMemoryBackend()
		defer backend.Close()

		ctx := context.Background()
		_, _, err := backend.GetVersion(ctx, "nonexistent", 1)
		assert.Equal(t, ErrSecretNotFound, err)
	})

	t.Run("EmptyPath", func(t *testing.T) {
		backend := NewMemoryBackend()
		defer backend.Close()

		ctx := context.Background()
		_, _, err := backend.GetVersion(ctx, "", 1)
		assert.Equal(t, ErrInvalidPath, err)
	})

	t.Run("DeletedVersion", func(t *testing.T) {
		backend := NewMemoryBackend()
		defer backend.Close()

		ctx := context.Background()
		path := "app/secret"

		// Store version 1
		data := map[string][]byte{"key": []byte("value1")}
		require.NoError(t, backend.Store(ctx, path, data, nil))

		// Delete marks latest version as deleted
		require.NoError(t, backend.Delete(ctx, path))

		// Getting the deleted version should return error
		_, _, err := backend.GetVersion(ctx, path, 1)
		assert.Equal(t, ErrVersionDestroyed, err)
	})

	t.Run("CanceledContext", func(t *testing.T) {
		backend := NewMemoryBackend()
		defer backend.Close()

		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		_, _, err := backend.GetVersion(ctx, "test/path", 1)
		require.Error(t, err)
	})
}

func TestMemoryBackend_List(t *testing.T) {
	t.Run("ListAll", func(t *testing.T) {
		backend := NewMemoryBackend()
		defer backend.Close()

		ctx := context.Background()

		paths := []string{"app/db/user", "app/db/pass", "service/api/key"}
		for _, path := range paths {
			data := map[string][]byte{"key": []byte("value")}
			require.NoError(t, backend.Store(ctx, path, data, nil))
		}

		listed, err := backend.List(ctx, "")
		require.NoError(t, err)
		assert.Equal(t, 3, len(listed))
	})

	t.Run("ListWithPrefix", func(t *testing.T) {
		backend := NewMemoryBackend()
		defer backend.Close()

		ctx := context.Background()

		paths := []string{"app/db/user", "app/db/pass", "service/api/key"}
		for _, path := range paths {
			data := map[string][]byte{"key": []byte("value")}
			require.NoError(t, backend.Store(ctx, path, data, nil))
		}

		listed, err := backend.List(ctx, "app/")
		require.NoError(t, err)
		assert.Equal(t, 2, len(listed))
	})

	t.Run("ListEmpty", func(t *testing.T) {
		backend := NewMemoryBackend()
		defer backend.Close()

		ctx := context.Background()

		listed, err := backend.List(ctx, "")
		require.NoError(t, err)
		assert.Equal(t, 0, len(listed))
	})

	t.Run("CanceledContext", func(t *testing.T) {
		backend := NewMemoryBackend()
		defer backend.Close()

		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		_, err := backend.List(ctx, "")
		require.Error(t, err)
	})

	t.Run("DeletedNotInList", func(t *testing.T) {
		backend := NewMemoryBackend()
		defer backend.Close()

		ctx := context.Background()
		path := "app/secret"

		data := map[string][]byte{"key": []byte("value")}
		require.NoError(t, backend.Store(ctx, path, data, nil))

		require.NoError(t, backend.Delete(ctx, path))

		listed, err := backend.List(ctx, "")
		require.NoError(t, err)

		for _, p := range listed {
			assert.NotEqual(t, path, p, "Deleted secret should not appear in list")
		}
	})
}

func TestMemoryBackend_Delete(t *testing.T) {
	t.Run("ValidDelete", func(t *testing.T) {
		backend := NewMemoryBackend()
		defer backend.Close()

		ctx := context.Background()
		path := "app/secret"

		data := map[string][]byte{"key": []byte("value")}
		require.NoError(t, backend.Store(ctx, path, data, nil))

		err := backend.Delete(ctx, path)
		require.NoError(t, err)

		// Should return deleted error
		_, _, err = backend.Get(ctx, path)
		assert.Equal(t, ErrSecretDeleted, err)
	})

	t.Run("NotFound", func(t *testing.T) {
		backend := NewMemoryBackend()
		defer backend.Close()

		ctx := context.Background()
		err := backend.Delete(ctx, "nonexistent")
		assert.Equal(t, ErrSecretNotFound, err)
	})

	t.Run("AlreadyDeleted", func(t *testing.T) {
		backend := NewMemoryBackend()
		defer backend.Close()

		ctx := context.Background()
		path := "app/secret"

		data := map[string][]byte{"key": []byte("value")}
		require.NoError(t, backend.Store(ctx, path, data, nil))

		require.NoError(t, backend.Delete(ctx, path))

		err := backend.Delete(ctx, path)
		assert.Equal(t, ErrSecretDeleted, err)
	})

	t.Run("EmptyPath", func(t *testing.T) {
		backend := NewMemoryBackend()
		defer backend.Close()

		ctx := context.Background()
		err := backend.Delete(ctx, "")
		assert.Equal(t, ErrInvalidPath, err)
	})

	t.Run("DeletedNotInList", func(t *testing.T) {
		backend := NewMemoryBackend()
		defer backend.Close()

		ctx := context.Background()
		path := "app/secret"

		data := map[string][]byte{"key": []byte("value")}
		require.NoError(t, backend.Store(ctx, path, data, nil))

		require.NoError(t, backend.Delete(ctx, path))

		listed, err := backend.List(ctx, "")
		require.NoError(t, err)

		for _, p := range listed {
			assert.NotEqual(t, path, p, "Deleted secret should not appear in list")
		}
	})

	t.Run("DeleteMarksLatestVersion", func(t *testing.T) {
		backend := NewMemoryBackend()
		defer backend.Close()

		ctx := context.Background()
		path := "app/secret"

		// Store 2 versions
		for i := 1; i <= 2; i++ {
			data := map[string][]byte{"version": {byte('0' + i)}}
			require.NoError(t, backend.Store(ctx, path, data, nil))
		}

		require.NoError(t, backend.Delete(ctx, path))

		versions, err := backend.ListVersions(ctx, path)
		require.NoError(t, err)
		// Latest version (v2) should be marked as deleted
		assert.True(t, versions[1].Deleted)
		assert.NotNil(t, versions[1].DeletedAt)
	})

	t.Run("CanceledContext", func(t *testing.T) {
		backend := NewMemoryBackend()
		defer backend.Close()

		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		err := backend.Delete(ctx, "test/path")
		require.Error(t, err)
	})
}

func TestMemoryBackend_Destroy(t *testing.T) {
	t.Run("ValidDestroy", func(t *testing.T) {
		backend := NewMemoryBackend()
		defer backend.Close()

		ctx := context.Background()
		path := "app/secret"

		// Store multiple versions
		for i := 0; i < 3; i++ {
			data := map[string][]byte{"key": []byte("value")}
			require.NoError(t, backend.Store(ctx, path, data, nil))
		}

		err := backend.Destroy(ctx, path)
		require.NoError(t, err)

		// Should return not found
		_, _, err = backend.Get(ctx, path)
		assert.Equal(t, ErrSecretNotFound, err)
	})

	t.Run("NotFound", func(t *testing.T) {
		backend := NewMemoryBackend()
		defer backend.Close()

		ctx := context.Background()
		err := backend.Destroy(ctx, "nonexistent")
		assert.Equal(t, ErrSecretNotFound, err)
	})

	t.Run("EmptyPath", func(t *testing.T) {
		backend := NewMemoryBackend()
		defer backend.Close()

		ctx := context.Background()
		err := backend.Destroy(ctx, "")
		assert.Equal(t, ErrInvalidPath, err)
	})

	t.Run("DestroyRemovesAllVersions", func(t *testing.T) {
		backend := NewMemoryBackend()
		defer backend.Close()

		ctx := context.Background()
		path := "app/secret"

		// Store 3 versions
		for i := 0; i < 3; i++ {
			data := map[string][]byte{"key": []byte("value")}
			require.NoError(t, backend.Store(ctx, path, data, nil))
		}

		require.NoError(t, backend.Destroy(ctx, path))

		// ListVersions should also return not found
		_, err := backend.ListVersions(ctx, path)
		assert.Equal(t, ErrSecretNotFound, err)
	})

	t.Run("CanceledContext", func(t *testing.T) {
		backend := NewMemoryBackend()
		defer backend.Close()

		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		err := backend.Destroy(ctx, "test/path")
		require.Error(t, err)
	})
}

func TestMemoryBackend_ListVersions(t *testing.T) {
	t.Run("ValidListVersions", func(t *testing.T) {
		backend := NewMemoryBackend()
		defer backend.Close()

		ctx := context.Background()
		path := "app/secret"

		// Store multiple versions
		for i := 0; i < 3; i++ {
			data := map[string][]byte{"key": []byte("value")}
			require.NoError(t, backend.Store(ctx, path, data, nil))
			time.Sleep(time.Millisecond) // Ensure different timestamps
		}

		versions, err := backend.ListVersions(ctx, path)
		require.NoError(t, err)
		assert.Equal(t, 3, len(versions))

		for i, v := range versions {
			assert.Equal(t, i+1, v.Version)
		}
	})

	t.Run("NotFound", func(t *testing.T) {
		backend := NewMemoryBackend()
		defer backend.Close()

		ctx := context.Background()
		_, err := backend.ListVersions(ctx, "nonexistent")
		assert.Equal(t, ErrSecretNotFound, err)
	})

	t.Run("EmptyPath", func(t *testing.T) {
		backend := NewMemoryBackend()
		defer backend.Close()

		ctx := context.Background()
		_, err := backend.ListVersions(ctx, "")
		assert.Equal(t, ErrInvalidPath, err)
	})

	t.Run("DeletedVersionMetadata", func(t *testing.T) {
		backend := NewMemoryBackend()
		defer backend.Close()

		ctx := context.Background()
		path := "app/secret"

		data := map[string][]byte{"key": []byte("value")}
		require.NoError(t, backend.Store(ctx, path, data, nil))

		require.NoError(t, backend.Delete(ctx, path))

		versions, err := backend.ListVersions(ctx, path)
		require.NoError(t, err)
		require.Equal(t, 1, len(versions))
		assert.True(t, versions[0].Deleted)
		assert.NotNil(t, versions[0].DeletedAt)
	})

	t.Run("CanceledContext", func(t *testing.T) {
		backend := NewMemoryBackend()
		defer backend.Close()

		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		_, err := backend.ListVersions(ctx, "test/path")
		require.Error(t, err)
	})
}

func TestMemoryBackend_Rollback(t *testing.T) {
	t.Run("ValidRollback", func(t *testing.T) {
		backend := NewMemoryBackend()
		defer backend.Close()

		ctx := context.Background()
		path := "app/secret"

		// Store 3 versions
		for i := 1; i <= 3; i++ {
			data := map[string][]byte{"value": {byte('0' + i)}}
			require.NoError(t, backend.Store(ctx, path, data, nil))
		}

		// Rollback to version 1
		err := backend.Rollback(ctx, path, 1)
		require.NoError(t, err)

		// Get should return version 1's data (as version 4)
		data, _, err := backend.Get(ctx, path)
		require.NoError(t, err)
		assert.Equal(t, "1", string(data["value"]))

		// Should have 4 versions now
		versions, _ := backend.ListVersions(ctx, path)
		assert.Equal(t, 4, len(versions))
	})

	t.Run("InvalidVersion", func(t *testing.T) {
		backend := NewMemoryBackend()
		defer backend.Close()

		ctx := context.Background()
		path := "app/secret"

		data := map[string][]byte{"key": []byte("value")}
		require.NoError(t, backend.Store(ctx, path, data, nil))

		// Version 0
		err := backend.Rollback(ctx, path, 0)
		assert.Equal(t, ErrVersionNotFound, err)

		// Version too high
		err = backend.Rollback(ctx, path, 99)
		assert.Equal(t, ErrVersionNotFound, err)
	})

	t.Run("SecretNotFound", func(t *testing.T) {
		backend := NewMemoryBackend()
		defer backend.Close()

		ctx := context.Background()
		err := backend.Rollback(ctx, "nonexistent", 1)
		assert.Equal(t, ErrSecretNotFound, err)
	})

	t.Run("EmptyPath", func(t *testing.T) {
		backend := NewMemoryBackend()
		defer backend.Close()

		ctx := context.Background()
		err := backend.Rollback(ctx, "", 1)
		assert.Equal(t, ErrInvalidPath, err)
	})

	t.Run("RollbackRestoresDeletedSecret", func(t *testing.T) {
		backend := NewMemoryBackend()
		defer backend.Close()

		ctx := context.Background()
		path := "app/secret"

		// Store 2 versions
		data1 := map[string][]byte{"key": []byte("value1")}
		require.NoError(t, backend.Store(ctx, path, data1, nil))
		data2 := map[string][]byte{"key": []byte("value2")}
		require.NoError(t, backend.Store(ctx, path, data2, nil))

		// Delete marks the latest version as deleted
		require.NoError(t, backend.Delete(ctx, path))

		// Rollback to version 1 (not deleted)
		err := backend.Rollback(ctx, path, 1)
		require.NoError(t, err)

		// Get should work now - returns version 1's data as the new version 3
		retrieved, _, err := backend.Get(ctx, path)
		require.NoError(t, err)
		assert.Equal(t, "value1", string(retrieved["key"]))
	})

	t.Run("RollbackDeletedVersion", func(t *testing.T) {
		backend := NewMemoryBackend()
		defer backend.Close()

		ctx := context.Background()
		path := "app/secret"

		data := map[string][]byte{"key": []byte("value")}
		require.NoError(t, backend.Store(ctx, path, data, nil))

		require.NoError(t, backend.Delete(ctx, path))

		// Cannot rollback to a deleted version
		err := backend.Rollback(ctx, path, 1)
		assert.Equal(t, ErrVersionDestroyed, err)
	})

	t.Run("RollbackCreatesNewVersion", func(t *testing.T) {
		backend := NewMemoryBackend()
		defer backend.Close()

		ctx := context.Background()
		path := "app/secret"

		// Store 3 versions
		for i := 1; i <= 3; i++ {
			data := map[string][]byte{"value": {byte('0' + i)}}
			require.NoError(t, backend.Store(ctx, path, data, nil))
		}

		// Rollback to version 2
		require.NoError(t, backend.Rollback(ctx, path, 2))

		versions, err := backend.ListVersions(ctx, path)
		require.NoError(t, err)
		assert.Equal(t, 4, len(versions), "Rollback should create a new version")
	})

	t.Run("CanceledContext", func(t *testing.T) {
		backend := NewMemoryBackend()
		defer backend.Close()

		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		err := backend.Rollback(ctx, "test/path", 1)
		require.Error(t, err)
	})
}

func TestMemoryBackend_Close(t *testing.T) {
	backend := NewMemoryBackend()

	ctx := context.Background()
	data := map[string][]byte{"key": []byte("value")}
	require.NoError(t, backend.Store(ctx, "test", data, nil))

	err := backend.Close()
	require.NoError(t, err)

	// After close, backend should be empty
	listed, _ := backend.List(ctx, "")
	assert.Equal(t, 0, len(listed), "Backend should be empty after close")
}

func TestMemoryBackend_ConcurrentAccess(t *testing.T) {
	backend := NewMemoryBackend()
	defer backend.Close()

	ctx := context.Background()
	var wg sync.WaitGroup
	goroutines := 10
	operationsPerGoroutine := 100

	// Concurrent writers
	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for j := 0; j < operationsPerGoroutine; j++ {
				path := "concurrent/test"
				data := map[string][]byte{"writer": {byte(id), byte(j)}}
				_ = backend.Store(ctx, path, data, nil)
			}
		}(i)
	}

	// Concurrent readers
	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < operationsPerGoroutine; j++ {
				_, _, _ = backend.Get(ctx, "concurrent/test")
				_, _ = backend.List(ctx, "")
			}
		}()
	}

	wg.Wait()
}

func TestMemoryBackend_CopyHelpers(t *testing.T) {
	t.Run("CopyDataNil", func(t *testing.T) {
		result := copyData(nil)
		assert.Nil(t, result)
	})

	t.Run("CopyDataEmpty", func(t *testing.T) {
		input := map[string][]byte{}
		result := copyData(input)
		assert.NotNil(t, result)
		assert.Equal(t, 0, len(result))
	})

	t.Run("CopyDataValid", func(t *testing.T) {
		input := map[string][]byte{"key": []byte("value")}
		result := copyData(input)
		assert.Equal(t, "value", string(result["key"]))

		// Modify input
		input["key"] = []byte("modified")
		// Result should be unchanged
		assert.Equal(t, "value", string(result["key"]))
	})

	t.Run("CopyMetadataNil", func(t *testing.T) {
		result := copyMetadata(nil)
		assert.Nil(t, result)
	})

	t.Run("CopyMetadataEmpty", func(t *testing.T) {
		input := map[string]string{}
		result := copyMetadata(input)
		assert.NotNil(t, result)
		assert.Equal(t, 0, len(result))
	})

	t.Run("CopyMetadataValid", func(t *testing.T) {
		input := map[string]string{"key": "value"}
		result := copyMetadata(input)
		assert.Equal(t, "value", result["key"])

		// Modify input
		input["key"] = "modified"
		// Result should be unchanged
		assert.Equal(t, "value", result["key"])
	})
}

func TestMemoryBackend_VersionUnaffectedByDelete(t *testing.T) {
	backend := NewMemoryBackend()
	defer backend.Close()

	ctx := context.Background()
	path := "app/secret"

	// Store 2 versions
	data1 := map[string][]byte{"version": []byte("1")}
	require.NoError(t, backend.Store(ctx, path, data1, nil))

	data2 := map[string][]byte{"version": []byte("2")}
	require.NoError(t, backend.Store(ctx, path, data2, nil))

	// Soft delete
	require.NoError(t, backend.Delete(ctx, path))

	// ListVersions should still show both versions
	versions, err := backend.ListVersions(ctx, path)
	require.NoError(t, err)
	assert.Equal(t, 2, len(versions))
}

func TestMemoryBackend_SoftDeleteVsDestroy(t *testing.T) {
	t.Run("SoftDeleteKeepsVersions", func(t *testing.T) {
		backend := NewMemoryBackend()
		defer backend.Close()

		ctx := context.Background()
		path := "app/secret"

		data := map[string][]byte{"key": []byte("value")}
		require.NoError(t, backend.Store(ctx, path, data, nil))

		// Soft delete
		require.NoError(t, backend.Delete(ctx, path))

		// Versions should still be accessible
		versions, err := backend.ListVersions(ctx, path)
		require.NoError(t, err)
		assert.Equal(t, 1, len(versions))
	})

	t.Run("DestroyRemovesEverything", func(t *testing.T) {
		backend := NewMemoryBackend()
		defer backend.Close()

		ctx := context.Background()
		path := "app/secret"

		data := map[string][]byte{"key": []byte("value")}
		require.NoError(t, backend.Store(ctx, path, data, nil))

		// Destroy
		require.NoError(t, backend.Destroy(ctx, path))

		// Everything should be gone
		_, err := backend.ListVersions(ctx, path)
		assert.Equal(t, ErrSecretNotFound, err)

		_, _, err = backend.Get(ctx, path)
		assert.Equal(t, ErrSecretNotFound, err)
	})
}
