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
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
	"github.com/jeremyhahn/go-quicbaas/pkg/tenantctx"
)

func TestNewManager(t *testing.T) {
	t.Run("ValidConfig", func(t *testing.T) {
		backend := NewMemoryBackend()
		manager, err := NewManager(&ManagerOptions{
			Backend: backend,
		})
		require.NoError(t, err)
		require.NotNil(t, manager)
		defer manager.Close()
	})

	t.Run("NilOptions", func(t *testing.T) {
		_, err := NewManager(nil)
		require.Error(t, err)
		var qbErr *errors.QBError
		require.True(t, errors.As(err, &qbErr))
		assert.Equal(t, errors.ErrInvalidInput, qbErr.Code)
	})

	t.Run("NilBackend", func(t *testing.T) {
		_, err := NewManager(&ManagerOptions{})
		require.Error(t, err)
		var qbErr *errors.QBError
		require.True(t, errors.As(err, &qbErr))
		assert.Equal(t, errors.ErrInvalidInput, qbErr.Code)
	})
}

func TestManager_Store(t *testing.T) {
	t.Run("ValidStore", func(t *testing.T) {
		backend := NewMemoryBackend()
		manager, _ := NewManager(&ManagerOptions{Backend: backend})
		defer manager.Close()

		ctx := context.Background()
		data := map[string][]byte{"password": []byte("secret123")}
		metadata := map[string]string{"owner": "test"}

		err := manager.Store(ctx, "app/db/password", data, metadata)
		require.NoError(t, err)
	})

	t.Run("EmptyPath", func(t *testing.T) {
		backend := NewMemoryBackend()
		manager, _ := NewManager(&ManagerOptions{Backend: backend})
		defer manager.Close()

		ctx := context.Background()
		data := map[string][]byte{"password": []byte("secret123")}

		err := manager.Store(ctx, "", data, nil)
		require.Error(t, err)
		var qbErr *errors.QBError
		require.True(t, errors.As(err, &qbErr))
		assert.Equal(t, errors.ErrInvalidInput, qbErr.Code)
	})

	t.Run("NilData", func(t *testing.T) {
		backend := NewMemoryBackend()
		manager, _ := NewManager(&ManagerOptions{Backend: backend})
		defer manager.Close()

		ctx := context.Background()

		err := manager.Store(ctx, "test/path", nil, nil)
		require.Error(t, err)
		var qbErr *errors.QBError
		require.True(t, errors.As(err, &qbErr))
		assert.Equal(t, errors.ErrInvalidInput, qbErr.Code)
	})

	t.Run("EmptyData", func(t *testing.T) {
		backend := NewMemoryBackend()
		manager, _ := NewManager(&ManagerOptions{Backend: backend})
		defer manager.Close()

		ctx := context.Background()
		data := map[string][]byte{}

		err := manager.Store(ctx, "test/path", data, nil)
		require.Error(t, err)
		var qbErr *errors.QBError
		require.True(t, errors.As(err, &qbErr))
		assert.Equal(t, errors.ErrInvalidInput, qbErr.Code)
	})

	t.Run("CanceledContext", func(t *testing.T) {
		backend := NewMemoryBackend()
		manager, _ := NewManager(&ManagerOptions{Backend: backend})
		defer manager.Close()

		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		data := map[string][]byte{"key": []byte("value")}

		err := manager.Store(ctx, "test/path", data, nil)
		require.Error(t, err)
		var qbErr *errors.QBError
		require.True(t, errors.As(err, &qbErr))
		assert.Equal(t, errors.ErrTimeout, qbErr.Code)
	})

	t.Run("ContextTimeout", func(t *testing.T) {
		backend := NewMemoryBackend()
		manager, _ := NewManager(&ManagerOptions{Backend: backend})
		defer manager.Close()

		ctx, cancel := context.WithTimeout(context.Background(), 1*time.Nanosecond)
		defer cancel()
		time.Sleep(10 * time.Millisecond) // Ensure context is expired

		data := map[string][]byte{"key": []byte("value")}

		err := manager.Store(ctx, "test/path", data, nil)
		require.Error(t, err)
		var qbErr *errors.QBError
		require.True(t, errors.As(err, &qbErr))
		assert.Equal(t, errors.ErrTimeout, qbErr.Code)
	})

	t.Run("BackendError", func(t *testing.T) {
		backend := NewMockBackend()
		backend.On("Store", testAny, "test/path", testAny, testAny).Return(assert.AnError)
		manager, _ := NewManager(&ManagerOptions{Backend: backend})
		defer manager.Close()

		ctx := context.Background()
		data := map[string][]byte{"key": []byte("value")}

		err := manager.Store(ctx, "test/path", data, nil)
		require.Error(t, err)
		var qbErr *errors.QBError
		require.True(t, errors.As(err, &qbErr))
		assert.Equal(t, errors.ErrInternal, qbErr.Code)
	})
}

func TestManager_Get(t *testing.T) {
	t.Run("ValidGet", func(t *testing.T) {
		backend := NewMemoryBackend()
		manager, _ := NewManager(&ManagerOptions{Backend: backend})
		defer manager.Close()

		ctx := context.Background()
		path := "app/secret"
		originalData := map[string][]byte{
			"username": []byte("admin"),
			"password": []byte("secret123"),
		}
		metadata := map[string]string{"created_by": "test"}

		require.NoError(t, manager.Store(ctx, path, originalData, metadata))

		data, meta, err := manager.Get(ctx, path)
		require.NoError(t, err)
		assert.Equal(t, "admin", string(data["username"]))
		assert.Equal(t, "test", meta["created_by"])
	})

	t.Run("NotFound", func(t *testing.T) {
		backend := NewMemoryBackend()
		manager, _ := NewManager(&ManagerOptions{Backend: backend})
		defer manager.Close()

		ctx := context.Background()
		_, _, err := manager.Get(ctx, "nonexistent/path")
		require.Error(t, err)
		var qbErr *errors.QBError
		require.True(t, errors.As(err, &qbErr))
		assert.Equal(t, errors.ErrNotFound, qbErr.Code)
	})

	t.Run("EmptyPath", func(t *testing.T) {
		backend := NewMemoryBackend()
		manager, _ := NewManager(&ManagerOptions{Backend: backend})
		defer manager.Close()

		ctx := context.Background()
		_, _, err := manager.Get(ctx, "")
		require.Error(t, err)
		var qbErr *errors.QBError
		require.True(t, errors.As(err, &qbErr))
		assert.Equal(t, errors.ErrInvalidInput, qbErr.Code)
	})

	t.Run("ContextTimeout", func(t *testing.T) {
		backend := NewMemoryBackend()
		manager, _ := NewManager(&ManagerOptions{Backend: backend})
		defer manager.Close()

		ctx, cancel := context.WithTimeout(context.Background(), 1*time.Nanosecond)
		defer cancel()
		time.Sleep(10 * time.Millisecond) // Ensure context is expired

		_, _, err := manager.Get(ctx, "test/path")
		require.Error(t, err)
		var qbErr *errors.QBError
		require.True(t, errors.As(err, &qbErr))
		assert.Equal(t, errors.ErrTimeout, qbErr.Code)
	})

	t.Run("SecretDeleted", func(t *testing.T) {
		backend := NewMemoryBackend()
		manager, _ := NewManager(&ManagerOptions{Backend: backend})
		defer manager.Close()

		ctx := context.Background()
		path := "app/secret"
		data := map[string][]byte{"key": []byte("value")}

		require.NoError(t, manager.Store(ctx, path, data, nil))
		require.NoError(t, manager.Delete(ctx, path))

		_, _, err := manager.Get(ctx, path)
		require.Error(t, err)
		var qbErr *errors.QBError
		require.True(t, errors.As(err, &qbErr))
		assert.Equal(t, errors.ErrNotFound, qbErr.Code)
	})

	t.Run("BackendError", func(t *testing.T) {
		backend := NewMockBackend()
		backend.On("Get", testAny, "test/path").Return(nil, nil, assert.AnError)
		manager, _ := NewManager(&ManagerOptions{Backend: backend})
		defer manager.Close()

		ctx := context.Background()
		_, _, err := manager.Get(ctx, "test/path")
		require.Error(t, err)
		var qbErr *errors.QBError
		require.True(t, errors.As(err, &qbErr))
		assert.Equal(t, errors.ErrInternal, qbErr.Code)
	})
}

func TestManager_GetVersion(t *testing.T) {
	t.Run("ValidVersion", func(t *testing.T) {
		backend := NewMemoryBackend()
		manager, _ := NewManager(&ManagerOptions{Backend: backend})
		defer manager.Close()

		ctx := context.Background()
		path := "app/secret"

		// Store multiple versions
		for i := 1; i <= 3; i++ {
			data := map[string][]byte{"version": {byte('0' + i)}}
			require.NoError(t, manager.Store(ctx, path, data, nil))
		}

		// Get version 2
		data, _, err := manager.GetVersion(ctx, path, 2)
		require.NoError(t, err)
		assert.Equal(t, "2", string(data["version"]))
	})

	t.Run("InvalidVersion", func(t *testing.T) {
		backend := NewMemoryBackend()
		manager, _ := NewManager(&ManagerOptions{Backend: backend})
		defer manager.Close()

		ctx := context.Background()
		path := "app/secret"

		data := map[string][]byte{"key": []byte("value")}
		require.NoError(t, manager.Store(ctx, path, data, nil))

		// Version 0
		_, _, err := manager.GetVersion(ctx, path, 0)
		require.Error(t, err)
		var qbErr *errors.QBError
		require.True(t, errors.As(err, &qbErr))
		assert.Equal(t, errors.ErrInvalidInput, qbErr.Code)

		// Negative version
		_, _, err = manager.GetVersion(ctx, path, -1)
		require.Error(t, err)
	})

	t.Run("SecretNotFound", func(t *testing.T) {
		backend := NewMemoryBackend()
		manager, _ := NewManager(&ManagerOptions{Backend: backend})
		defer manager.Close()

		ctx := context.Background()
		_, _, err := manager.GetVersion(ctx, "nonexistent", 1)
		require.Error(t, err)
		var qbErr *errors.QBError
		require.True(t, errors.As(err, &qbErr))
		assert.Equal(t, errors.ErrNotFound, qbErr.Code)
	})

	t.Run("VersionNotFound", func(t *testing.T) {
		backend := NewMemoryBackend()
		manager, _ := NewManager(&ManagerOptions{Backend: backend})
		defer manager.Close()

		ctx := context.Background()
		path := "app/secret"
		data := map[string][]byte{"key": []byte("value")}
		require.NoError(t, manager.Store(ctx, path, data, nil))

		_, _, err := manager.GetVersion(ctx, path, 99)
		require.Error(t, err)
		var qbErr *errors.QBError
		require.True(t, errors.As(err, &qbErr))
		assert.Equal(t, errors.ErrNotFound, qbErr.Code)
	})

	t.Run("EmptyPath", func(t *testing.T) {
		backend := NewMemoryBackend()
		manager, _ := NewManager(&ManagerOptions{Backend: backend})
		defer manager.Close()

		ctx := context.Background()
		_, _, err := manager.GetVersion(ctx, "", 1)
		require.Error(t, err)
		var qbErr *errors.QBError
		require.True(t, errors.As(err, &qbErr))
		assert.Equal(t, errors.ErrInvalidInput, qbErr.Code)
	})

	t.Run("ContextTimeout", func(t *testing.T) {
		backend := NewMemoryBackend()
		manager, _ := NewManager(&ManagerOptions{Backend: backend})
		defer manager.Close()

		ctx, cancel := context.WithTimeout(context.Background(), 1*time.Nanosecond)
		defer cancel()
		time.Sleep(10 * time.Millisecond)

		_, _, err := manager.GetVersion(ctx, "test/path", 1)
		require.Error(t, err)
		var qbErr *errors.QBError
		require.True(t, errors.As(err, &qbErr))
		assert.Equal(t, errors.ErrTimeout, qbErr.Code)
	})
}

func TestManager_List(t *testing.T) {
	t.Run("ListAll", func(t *testing.T) {
		backend := NewMemoryBackend()
		manager, _ := NewManager(&ManagerOptions{Backend: backend})
		defer manager.Close()

		ctx := context.Background()

		paths := []string{"app/db/user", "app/db/pass", "service/api/key"}
		for _, path := range paths {
			data := map[string][]byte{"key": []byte("value")}
			require.NoError(t, manager.Store(ctx, path, data, nil))
		}

		listed, err := manager.List(ctx, "")
		require.NoError(t, err)
		assert.Equal(t, 3, len(listed))
	})

	t.Run("ListWithPrefix", func(t *testing.T) {
		backend := NewMemoryBackend()
		manager, _ := NewManager(&ManagerOptions{Backend: backend})
		defer manager.Close()

		ctx := context.Background()

		paths := []string{"app/db/user", "app/db/pass", "service/api/key"}
		for _, path := range paths {
			data := map[string][]byte{"key": []byte("value")}
			require.NoError(t, manager.Store(ctx, path, data, nil))
		}

		listed, err := manager.List(ctx, "app/")
		require.NoError(t, err)
		assert.Equal(t, 2, len(listed))
	})

	t.Run("ContextTimeout", func(t *testing.T) {
		backend := NewMemoryBackend()
		manager, _ := NewManager(&ManagerOptions{Backend: backend})
		defer manager.Close()

		ctx, cancel := context.WithTimeout(context.Background(), 1*time.Nanosecond)
		defer cancel()
		time.Sleep(10 * time.Millisecond)

		_, err := manager.List(ctx, "")
		require.Error(t, err)
		var qbErr *errors.QBError
		require.True(t, errors.As(err, &qbErr))
		assert.Equal(t, errors.ErrTimeout, qbErr.Code)
	})

	t.Run("BackendError", func(t *testing.T) {
		backend := NewMockBackend()
		backend.On("List", testAny, "").Return(nil, assert.AnError)
		manager, _ := NewManager(&ManagerOptions{Backend: backend})
		defer manager.Close()

		ctx := context.Background()
		_, err := manager.List(ctx, "")
		require.Error(t, err)
		var qbErr *errors.QBError
		require.True(t, errors.As(err, &qbErr))
		assert.Equal(t, errors.ErrInternal, qbErr.Code)
	})
}

func TestManager_Delete(t *testing.T) {
	t.Run("ValidDelete", func(t *testing.T) {
		backend := NewMemoryBackend()
		manager, _ := NewManager(&ManagerOptions{Backend: backend})
		defer manager.Close()

		ctx := context.Background()
		path := "app/secret"

		data := map[string][]byte{"key": []byte("value")}
		require.NoError(t, manager.Store(ctx, path, data, nil))

		err := manager.Delete(ctx, path)
		require.NoError(t, err)

		// Should return not found error
		_, _, err = manager.Get(ctx, path)
		require.Error(t, err)
	})

	t.Run("NotFound", func(t *testing.T) {
		backend := NewMemoryBackend()
		manager, _ := NewManager(&ManagerOptions{Backend: backend})
		defer manager.Close()

		ctx := context.Background()
		err := manager.Delete(ctx, "nonexistent")
		require.Error(t, err)
		var qbErr *errors.QBError
		require.True(t, errors.As(err, &qbErr))
		assert.Equal(t, errors.ErrNotFound, qbErr.Code)
	})

	t.Run("EmptyPath", func(t *testing.T) {
		backend := NewMemoryBackend()
		manager, _ := NewManager(&ManagerOptions{Backend: backend})
		defer manager.Close()

		ctx := context.Background()
		err := manager.Delete(ctx, "")
		require.Error(t, err)
		var qbErr *errors.QBError
		require.True(t, errors.As(err, &qbErr))
		assert.Equal(t, errors.ErrInvalidInput, qbErr.Code)
	})

	t.Run("ContextTimeout", func(t *testing.T) {
		backend := NewMemoryBackend()
		manager, _ := NewManager(&ManagerOptions{Backend: backend})
		defer manager.Close()

		ctx, cancel := context.WithTimeout(context.Background(), 1*time.Nanosecond)
		defer cancel()
		time.Sleep(10 * time.Millisecond)

		err := manager.Delete(ctx, "test/path")
		require.Error(t, err)
		var qbErr *errors.QBError
		require.True(t, errors.As(err, &qbErr))
		assert.Equal(t, errors.ErrTimeout, qbErr.Code)
	})

	t.Run("BackendError", func(t *testing.T) {
		backend := NewMockBackend()
		backend.On("Delete", testAny, "test/path").Return(assert.AnError)
		manager, _ := NewManager(&ManagerOptions{Backend: backend})
		defer manager.Close()

		ctx := context.Background()
		err := manager.Delete(ctx, "test/path")
		require.Error(t, err)
		var qbErr *errors.QBError
		require.True(t, errors.As(err, &qbErr))
		assert.Equal(t, errors.ErrInternal, qbErr.Code)
	})
}

func TestManager_Destroy(t *testing.T) {
	t.Run("ValidDestroy", func(t *testing.T) {
		backend := NewMemoryBackend()
		manager, _ := NewManager(&ManagerOptions{Backend: backend})
		defer manager.Close()

		ctx := context.Background()
		path := "app/secret"

		// Store multiple versions
		for i := 0; i < 3; i++ {
			data := map[string][]byte{"key": []byte("value")}
			require.NoError(t, manager.Store(ctx, path, data, nil))
		}

		err := manager.Destroy(ctx, path)
		require.NoError(t, err)

		// Should return not found
		_, _, err = manager.Get(ctx, path)
		require.Error(t, err)
	})

	t.Run("NotFound", func(t *testing.T) {
		backend := NewMemoryBackend()
		manager, _ := NewManager(&ManagerOptions{Backend: backend})
		defer manager.Close()

		ctx := context.Background()
		err := manager.Destroy(ctx, "nonexistent")
		require.Error(t, err)
		var qbErr *errors.QBError
		require.True(t, errors.As(err, &qbErr))
		assert.Equal(t, errors.ErrNotFound, qbErr.Code)
	})

	t.Run("EmptyPath", func(t *testing.T) {
		backend := NewMemoryBackend()
		manager, _ := NewManager(&ManagerOptions{Backend: backend})
		defer manager.Close()

		ctx := context.Background()
		err := manager.Destroy(ctx, "")
		require.Error(t, err)
		var qbErr *errors.QBError
		require.True(t, errors.As(err, &qbErr))
		assert.Equal(t, errors.ErrInvalidInput, qbErr.Code)
	})

	t.Run("ContextTimeout", func(t *testing.T) {
		backend := NewMemoryBackend()
		manager, _ := NewManager(&ManagerOptions{Backend: backend})
		defer manager.Close()

		ctx, cancel := context.WithTimeout(context.Background(), 1*time.Nanosecond)
		defer cancel()
		time.Sleep(10 * time.Millisecond)

		err := manager.Destroy(ctx, "test/path")
		require.Error(t, err)
		var qbErr *errors.QBError
		require.True(t, errors.As(err, &qbErr))
		assert.Equal(t, errors.ErrTimeout, qbErr.Code)
	})

	t.Run("BackendError", func(t *testing.T) {
		backend := NewMockBackend()
		backend.On("Destroy", testAny, "test/path").Return(assert.AnError)
		manager, _ := NewManager(&ManagerOptions{Backend: backend})
		defer manager.Close()

		ctx := context.Background()
		err := manager.Destroy(ctx, "test/path")
		require.Error(t, err)
		var qbErr *errors.QBError
		require.True(t, errors.As(err, &qbErr))
		assert.Equal(t, errors.ErrInternal, qbErr.Code)
	})
}

func TestManager_ListVersions(t *testing.T) {
	t.Run("ValidListVersions", func(t *testing.T) {
		backend := NewMemoryBackend()
		manager, _ := NewManager(&ManagerOptions{Backend: backend})
		defer manager.Close()

		ctx := context.Background()
		path := "app/secret"

		// Store multiple versions
		for i := 0; i < 3; i++ {
			data := map[string][]byte{"key": []byte("value")}
			require.NoError(t, manager.Store(ctx, path, data, nil))
		}

		versions, err := manager.ListVersions(ctx, path)
		require.NoError(t, err)
		assert.Equal(t, 3, len(versions))
	})

	t.Run("NotFound", func(t *testing.T) {
		backend := NewMemoryBackend()
		manager, _ := NewManager(&ManagerOptions{Backend: backend})
		defer manager.Close()

		ctx := context.Background()
		_, err := manager.ListVersions(ctx, "nonexistent")
		require.Error(t, err)
		var qbErr *errors.QBError
		require.True(t, errors.As(err, &qbErr))
		assert.Equal(t, errors.ErrNotFound, qbErr.Code)
	})

	t.Run("EmptyPath", func(t *testing.T) {
		backend := NewMemoryBackend()
		manager, _ := NewManager(&ManagerOptions{Backend: backend})
		defer manager.Close()

		ctx := context.Background()
		_, err := manager.ListVersions(ctx, "")
		require.Error(t, err)
		var qbErr *errors.QBError
		require.True(t, errors.As(err, &qbErr))
		assert.Equal(t, errors.ErrInvalidInput, qbErr.Code)
	})

	t.Run("ContextTimeout", func(t *testing.T) {
		backend := NewMemoryBackend()
		manager, _ := NewManager(&ManagerOptions{Backend: backend})
		defer manager.Close()

		ctx, cancel := context.WithTimeout(context.Background(), 1*time.Nanosecond)
		defer cancel()
		time.Sleep(10 * time.Millisecond)

		_, err := manager.ListVersions(ctx, "test/path")
		require.Error(t, err)
		var qbErr *errors.QBError
		require.True(t, errors.As(err, &qbErr))
		assert.Equal(t, errors.ErrTimeout, qbErr.Code)
	})

	t.Run("BackendError", func(t *testing.T) {
		backend := NewMockBackend()
		backend.On("ListVersions", testAny, "test/path").Return(nil, assert.AnError)
		manager, _ := NewManager(&ManagerOptions{Backend: backend})
		defer manager.Close()

		ctx := context.Background()
		_, err := manager.ListVersions(ctx, "test/path")
		require.Error(t, err)
		var qbErr *errors.QBError
		require.True(t, errors.As(err, &qbErr))
		assert.Equal(t, errors.ErrInternal, qbErr.Code)
	})
}

func TestManager_Rollback(t *testing.T) {
	t.Run("ValidRollback", func(t *testing.T) {
		backend := NewMemoryBackend()
		manager, _ := NewManager(&ManagerOptions{Backend: backend})
		defer manager.Close()

		ctx := context.Background()
		path := "app/secret"

		// Store 3 versions
		for i := 1; i <= 3; i++ {
			data := map[string][]byte{"value": {byte('0' + i)}}
			require.NoError(t, manager.Store(ctx, path, data, nil))
		}

		// Rollback to version 1
		err := manager.Rollback(ctx, path, 1)
		require.NoError(t, err)

		// Get should return version 1's data
		data, _, err := manager.Get(ctx, path)
		require.NoError(t, err)
		assert.Equal(t, "1", string(data["value"]))
	})

	t.Run("InvalidVersion", func(t *testing.T) {
		backend := NewMemoryBackend()
		manager, _ := NewManager(&ManagerOptions{Backend: backend})
		defer manager.Close()

		ctx := context.Background()
		path := "app/secret"

		data := map[string][]byte{"key": []byte("value")}
		require.NoError(t, manager.Store(ctx, path, data, nil))

		// Version 0
		err := manager.Rollback(ctx, path, 0)
		require.Error(t, err)
		var qbErr *errors.QBError
		require.True(t, errors.As(err, &qbErr))
		assert.Equal(t, errors.ErrInvalidInput, qbErr.Code)

		// Negative version
		err = manager.Rollback(ctx, path, -1)
		require.Error(t, err)
	})

	t.Run("SecretNotFound", func(t *testing.T) {
		backend := NewMemoryBackend()
		manager, _ := NewManager(&ManagerOptions{Backend: backend})
		defer manager.Close()

		ctx := context.Background()
		err := manager.Rollback(ctx, "nonexistent", 1)
		require.Error(t, err)
		var qbErr *errors.QBError
		require.True(t, errors.As(err, &qbErr))
		assert.Equal(t, errors.ErrNotFound, qbErr.Code)
	})

	t.Run("VersionNotFound", func(t *testing.T) {
		backend := NewMemoryBackend()
		manager, _ := NewManager(&ManagerOptions{Backend: backend})
		defer manager.Close()

		ctx := context.Background()
		path := "app/secret"
		data := map[string][]byte{"key": []byte("value")}
		require.NoError(t, manager.Store(ctx, path, data, nil))

		err := manager.Rollback(ctx, path, 99)
		require.Error(t, err)
		var qbErr *errors.QBError
		require.True(t, errors.As(err, &qbErr))
		assert.Equal(t, errors.ErrNotFound, qbErr.Code)
	})

	t.Run("EmptyPath", func(t *testing.T) {
		backend := NewMemoryBackend()
		manager, _ := NewManager(&ManagerOptions{Backend: backend})
		defer manager.Close()

		ctx := context.Background()
		err := manager.Rollback(ctx, "", 1)
		require.Error(t, err)
		var qbErr *errors.QBError
		require.True(t, errors.As(err, &qbErr))
		assert.Equal(t, errors.ErrInvalidInput, qbErr.Code)
	})

	t.Run("ContextTimeout", func(t *testing.T) {
		backend := NewMemoryBackend()
		manager, _ := NewManager(&ManagerOptions{Backend: backend})
		defer manager.Close()

		ctx, cancel := context.WithTimeout(context.Background(), 1*time.Nanosecond)
		defer cancel()
		time.Sleep(10 * time.Millisecond)

		err := manager.Rollback(ctx, "test/path", 1)
		require.Error(t, err)
		var qbErr *errors.QBError
		require.True(t, errors.As(err, &qbErr))
		assert.Equal(t, errors.ErrTimeout, qbErr.Code)
	})

	t.Run("BackendError", func(t *testing.T) {
		backend := NewMockBackend()
		backend.On("Rollback", testAny, "test/path", 1).Return(assert.AnError)
		manager, _ := NewManager(&ManagerOptions{Backend: backend})
		defer manager.Close()

		ctx := context.Background()
		err := manager.Rollback(ctx, "test/path", 1)
		require.Error(t, err)
		var qbErr *errors.QBError
		require.True(t, errors.As(err, &qbErr))
		assert.Equal(t, errors.ErrInternal, qbErr.Code)
	})
}

func TestManager_GetBackendType(t *testing.T) {
	backend := NewMemoryBackend()
	manager, _ := NewManager(&ManagerOptions{Backend: backend})
	defer manager.Close()

	assert.Equal(t, "memory", manager.GetBackendType())
}

func TestManager_Close(t *testing.T) {
	t.Run("SuccessfulClose", func(t *testing.T) {
		backend := NewMemoryBackend()
		manager, _ := NewManager(&ManagerOptions{Backend: backend})

		err := manager.Close()
		require.NoError(t, err)
	})

	t.Run("CloseWithBackendError", func(t *testing.T) {
		backend := &MockBackend{}
		backend.On("Type").Return("mock")
		backend.On("Close").Return(assert.AnError)
		manager, _ := NewManager(&ManagerOptions{Backend: backend})

		err := manager.Close()
		require.Error(t, err)
		var qbErr *errors.QBError
		require.True(t, errors.As(err, &qbErr))
		assert.Equal(t, errors.ErrInternal, qbErr.Code)
	})
}

func TestManager_EnrichOptionsFromContext(t *testing.T) {
	backend := NewMemoryBackend()
	manager, _ := NewManager(&ManagerOptions{Backend: backend})
	defer manager.Close()

	t.Run("NilOptions_NoTenantContext", func(t *testing.T) {
		ctx := context.Background()
		opts := manager.enrichOptionsFromContext(ctx, nil)

		require.NotNil(t, opts)
		assert.Empty(t, opts.TenantID)
	})

	t.Run("NilOptions_WithTenantContext", func(t *testing.T) {
		ctx := tenantctx.WithTenant(context.Background(), "tenant-abc")
		opts := manager.enrichOptionsFromContext(ctx, nil)

		require.NotNil(t, opts)
		assert.Equal(t, "tenant-abc", opts.TenantID)
	})

	t.Run("EmptyTenantID_PopulatedFromContext", func(t *testing.T) {
		ctx := tenantctx.WithTenant(context.Background(), "tenant-xyz")
		inputOpts := &interfaces.StoreOptions{
			Algorithm: "AES-256-GCM", // other fields set
		}
		opts := manager.enrichOptionsFromContext(ctx, inputOpts)

		assert.Equal(t, "tenant-xyz", opts.TenantID)
		assert.Equal(t, "AES-256-GCM", opts.Algorithm) // other fields preserved
	})

	t.Run("ExplicitTenantID_NotOverwritten", func(t *testing.T) {
		ctx := tenantctx.WithTenant(context.Background(), "context-tenant")
		inputOpts := &interfaces.StoreOptions{
			TenantID: "explicit-tenant",
		}
		opts := manager.enrichOptionsFromContext(ctx, inputOpts)

		// Explicit TenantID should not be overwritten
		assert.Equal(t, "explicit-tenant", opts.TenantID)
	})

	t.Run("EmptyTenantContext_NoChange", func(t *testing.T) {
		ctx := context.Background()
		inputOpts := &interfaces.StoreOptions{
			KeyIdentifier: "some-key",
		}
		opts := manager.enrichOptionsFromContext(ctx, inputOpts)

		assert.Empty(t, opts.TenantID)
		assert.Equal(t, "some-key", opts.KeyIdentifier) // other fields preserved
	})
}

func TestManager_TenantIDFromContext(t *testing.T) {
	backend := NewMemoryBackend()
	manager, _ := NewManager(&ManagerOptions{Backend: backend})
	defer manager.Close()

	t.Run("NoTenantInContext", func(t *testing.T) {
		ctx := context.Background()
		tenantID := manager.TenantIDFromContext(ctx)
		assert.Empty(t, tenantID)
	})

	t.Run("TenantInContext", func(t *testing.T) {
		ctx := tenantctx.WithTenant(context.Background(), "my-tenant-id")
		tenantID := manager.TenantIDFromContext(ctx)
		assert.Equal(t, "my-tenant-id", tenantID)
	})
}

func TestManager_StoreWithOptions_AutoPopulateTenantID(t *testing.T) {
	t.Run("AutoPopulateFromContext", func(t *testing.T) {
		// Create a mock backend that implements BackendWithOptions
		backend := &MockBackendWithOptions{MockBackend: MockBackend{}}
		backend.On("Type").Return("mock")
		backend.On("Close").Return(nil)

		var capturedOpts *interfaces.StoreOptions
		backend.storeWithOptionsFn = func(ctx context.Context, path string, data map[string][]byte, metadata map[string]string, opts *interfaces.StoreOptions) error {
			capturedOpts = opts
			return nil
		}

		manager, err := NewManager(&ManagerOptions{Backend: backend})
		require.NoError(t, err)
		defer manager.Close()

		ctx := tenantctx.WithTenant(context.Background(), "auto-tenant")
		err = manager.StoreWithOptions(ctx, "test/path", map[string][]byte{"key": []byte("value")}, nil, nil)
		require.NoError(t, err)

		// Verify TenantID was auto-populated
		require.NotNil(t, capturedOpts)
		assert.Equal(t, "auto-tenant", capturedOpts.TenantID)
	})
}

// MockBackendWithOptions is a mock backend that implements BackendWithOptions
type MockBackendWithOptions struct {
	MockBackend
	storeWithOptionsFn func(ctx context.Context, path string, data map[string][]byte, metadata map[string]string, opts *interfaces.StoreOptions) error
}

func (m *MockBackendWithOptions) StoreWithOptions(ctx context.Context, path string, data map[string][]byte, metadata map[string]string, opts *interfaces.StoreOptions) error {
	if m.storeWithOptionsFn != nil {
		return m.storeWithOptionsFn(ctx, path, data, metadata, opts)
	}
	return nil
}

// TestManager_UseSecret covers the guarded-use read: the one way credential
// bytes are meant to leave the secrets service without the caller ever owning
// them (go-quicbaas#102 review 959; the wider custody move is #97).
func TestManager_UseSecret(t *testing.T) {
	newManager := func(t *testing.T) *Manager {
		t.Helper()
		manager, err := NewManager(&ManagerOptions{Backend: NewMemoryBackend()})
		require.NoError(t, err)
		t.Cleanup(func() { _ = manager.Close() })
		return manager
	}
	ctx := context.Background()

	t.Run("DeliversTheValueAndWipesEveryFetchedByte", func(t *testing.T) {
		manager := newManager(t)
		require.NoError(t, manager.Store(ctx, "app/token",
			map[string][]byte{"token": []byte("live-credential"), "note": []byte("sibling")}, nil))

		// The test deliberately violates fn's no-retain contract to OBSERVE
		// the wipe: production callers must never do this.
		var retained []byte
		err := manager.UseSecret(ctx, "app/token", "token", func(value []byte) error {
			assert.Equal(t, []byte("live-credential"), value,
				"fn must see the stored value during the call")
			retained = value
			return nil
		})
		require.NoError(t, err)
		require.NotNil(t, retained, "fn was never called")
		for i, b := range retained {
			if b != 0 {
				t.Fatalf("byte %d of the delivered value survived UseSecret's return: "+
					"the wipe-on-exit guarantee is the entire point of the guarded read", i)
			}
		}

		// The wipe reaches this CALL's copies only: the stored secret is
		// intact for the next reader.
		data, _, err := manager.Get(ctx, "app/token")
		require.NoError(t, err)
		assert.Equal(t, []byte("live-credential"), data["token"],
			"UseSecret wiped the STORED secret, not its own fetched copy")
	})

	t.Run("FnErrorPropagatesAndTheWipeStillRuns", func(t *testing.T) {
		manager := newManager(t)
		require.NoError(t, manager.Store(ctx, "app/token",
			map[string][]byte{"token": []byte("live-credential")}, nil))

		sentinel := errors.New(errors.ErrInternal, "Manager.UseSecret.test", "verification refused")
		var retained []byte
		err := manager.UseSecret(ctx, "app/token", "token", func(value []byte) error {
			retained = value
			return sentinel
		})
		require.ErrorIs(t, err, sentinel, "fn's error must reach the caller unreplaced")
		for i, b := range retained {
			if b != 0 {
				t.Fatalf("byte %d survived a failing fn: the wipe must not depend on success", i)
			}
		}
	})

	t.Run("AbsentKeyFailsClosedWithoutCallingFn", func(t *testing.T) {
		manager := newManager(t)
		require.NoError(t, manager.Store(ctx, "app/token",
			map[string][]byte{"other": []byte("present")}, nil))

		called := false
		err := manager.UseSecret(ctx, "app/token", "token", func([]byte) error {
			called = true
			return nil
		})
		require.ErrorIs(t, err, ErrSecretKeyAbsent)
		assert.False(t, called, "fn must never run over a record with no value under the key")
	})

	t.Run("EmptyValueIsAbsent", func(t *testing.T) {
		manager := newManager(t)
		require.NoError(t, manager.Store(ctx, "app/token",
			map[string][]byte{"token": {}, "pad": []byte("x")}, nil))

		err := manager.UseSecret(ctx, "app/token", "token", func([]byte) error {
			t.Fatal("fn ran over an empty credential")
			return nil
		})
		require.ErrorIs(t, err, ErrSecretKeyAbsent)
	})

	t.Run("AbsentPathReportsAsGetDoes", func(t *testing.T) {
		manager := newManager(t)
		err := manager.UseSecret(ctx, "app/absent", "token", func([]byte) error {
			t.Fatal("fn ran over an absent path")
			return nil
		})
		require.ErrorIs(t, err, ErrSecretNotFound)
	})
}

// TestManager_UseSecret_NilCallback is review 982's guard: malformed input
// on a credential surface is a typed refusal decided BEFORE the backend is
// touched — never a fetch-wipe-panic.
func TestManager_UseSecret_NilCallback(t *testing.T) {
	backend := NewMemoryBackend()
	manager, err := NewManager(&ManagerOptions{Backend: backend})
	require.NoError(t, err)
	t.Cleanup(func() { _ = manager.Close() })

	err = manager.UseSecret(context.Background(), "app/never-read", "token", nil)
	require.ErrorIs(t, err, ErrNilCallback)
	var qbErr *errors.QBError
	require.True(t, errors.As(err, &qbErr))
	assert.Equal(t, errors.ErrInvalidInput, qbErr.Code)
}
