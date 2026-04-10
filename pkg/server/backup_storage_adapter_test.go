//go:build !integration

package server

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/storage"
)

// mockStorage implements storage.Storage for testing the backup adapter.
type mockStorage struct {
	getFunc      func(key []byte) ([]byte, error)
	setFunc      func(key, value []byte) error
	deleteFunc   func(key []byte) error
	batchFunc    func() storage.Batch
	iterFunc     func(opts *storage.IteratorOptions) storage.Iterator
	snapshotFunc func() (storage.Snapshot, error)
	closeFunc    func() error
}

func (m *mockStorage) Get(key []byte) ([]byte, error) {
	if m.getFunc != nil {
		return m.getFunc(key)
	}
	return nil, nil
}

func (m *mockStorage) Set(key, value []byte) error {
	if m.setFunc != nil {
		return m.setFunc(key, value)
	}
	return nil
}

func (m *mockStorage) Delete(key []byte) error {
	if m.deleteFunc != nil {
		return m.deleteFunc(key)
	}
	return nil
}

func (m *mockStorage) List(prefix []byte) ([][]byte, error) {
	return nil, nil
}

func (m *mockStorage) Exists(key []byte) (bool, error) {
	return false, nil
}

func (m *mockStorage) Batch() storage.Batch {
	if m.batchFunc != nil {
		return m.batchFunc()
	}
	return nil
}

func (m *mockStorage) NewIterator(opts *storage.IteratorOptions) storage.Iterator {
	if m.iterFunc != nil {
		return m.iterFunc(opts)
	}
	return nil
}

func (m *mockStorage) Snapshot() (storage.Snapshot, error) {
	if m.snapshotFunc != nil {
		return m.snapshotFunc()
	}
	return nil, nil
}

func (m *mockStorage) Close() error {
	if m.closeFunc != nil {
		return m.closeFunc()
	}
	return nil
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

func TestBackupStorageAdapter_NewAndGet(t *testing.T) {
	inner := &mockStorage{
		getFunc: func(key []byte) ([]byte, error) {
			return []byte("value"), nil
		},
	}
	adapter := newBackupStorageAdapter(inner)

	t.Run("get delegates to inner", func(t *testing.T) {
		val, err := adapter.Get([]byte("k"))
		require.NoError(t, err)
		assert.Equal(t, []byte("value"), val)
	})
}

func TestBackupStorageAdapter_Set(t *testing.T) {
	var called bool
	inner := &mockStorage{
		setFunc: func(key, value []byte) error {
			called = true
			return nil
		},
	}
	adapter := newBackupStorageAdapter(inner)

	t.Run("set delegates to inner", func(t *testing.T) {
		require.NoError(t, adapter.Set([]byte("k"), []byte("v")))
		assert.True(t, called)
	})
}

func TestBackupStorageAdapter_Delete(t *testing.T) {
	var called bool
	inner := &mockStorage{
		deleteFunc: func(key []byte) error {
			called = true
			return nil
		},
	}
	adapter := newBackupStorageAdapter(inner)

	t.Run("delete delegates to inner", func(t *testing.T) {
		require.NoError(t, adapter.Delete([]byte("k")))
		assert.True(t, called)
	})
}

func TestBackupStorageAdapter_GetWithContext(t *testing.T) {
	inner := &mockStorage{
		getFunc: func(key []byte) ([]byte, error) {
			return []byte("ctxval"), nil
		},
	}
	adapter := newBackupStorageAdapter(inner)

	t.Run("returns value with valid context", func(t *testing.T) {
		val, err := adapter.GetWithContext(context.Background(), []byte("k"))
		require.NoError(t, err)
		assert.Equal(t, []byte("ctxval"), val)
	})

	t.Run("returns error when context is cancelled", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		_, err := adapter.GetWithContext(ctx, []byte("k"))
		require.Error(t, err)
	})
}

func TestBackupStorageAdapter_SetWithContext(t *testing.T) {
	inner := &mockStorage{}
	adapter := newBackupStorageAdapter(inner)

	t.Run("succeeds with valid context", func(t *testing.T) {
		err := adapter.SetWithContext(context.Background(), []byte("k"), []byte("v"))
		require.NoError(t, err)
	})

	t.Run("returns error when context is cancelled", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		err := adapter.SetWithContext(ctx, []byte("k"), []byte("v"))
		require.Error(t, err)
	})
}

func TestBackupStorageAdapter_Batch(t *testing.T) {
	inner := &mockStorage{
		batchFunc: func() storage.Batch { return nil },
	}
	adapter := newBackupStorageAdapter(inner)

	t.Run("delegates to inner", func(t *testing.T) {
		result := adapter.Batch()
		assert.Nil(t, result)
	})
}

func TestBackupStorageAdapter_NewIterator(t *testing.T) {
	inner := &mockStorage{
		iterFunc: func(opts *storage.IteratorOptions) storage.Iterator { return nil },
	}
	adapter := newBackupStorageAdapter(inner)

	t.Run("delegates to inner", func(t *testing.T) {
		result := adapter.NewIterator(nil)
		assert.Nil(t, result)
	})
}

func TestBackupStorageAdapter_Snapshot(t *testing.T) {
	inner := &mockStorage{
		snapshotFunc: func() (storage.Snapshot, error) { return nil, nil },
	}
	adapter := newBackupStorageAdapter(inner)

	t.Run("delegates to inner", func(t *testing.T) {
		snap, err := adapter.Snapshot()
		require.NoError(t, err)
		assert.Nil(t, snap)
	})
}

func TestBackupStorageAdapter_CompactAndFlush(t *testing.T) {
	inner := &mockStorage{}
	adapter := newBackupStorageAdapter(inner)

	t.Run("compact is no-op", func(t *testing.T) {
		require.NoError(t, adapter.Compact(nil, nil))
	})

	t.Run("flush is no-op", func(t *testing.T) {
		require.NoError(t, adapter.Flush())
	})
}

func TestBackupStorageAdapter_Close(t *testing.T) {
	var called bool
	inner := &mockStorage{
		closeFunc: func() error {
			called = true
			return nil
		},
	}
	adapter := newBackupStorageAdapter(inner)

	t.Run("close delegates to inner", func(t *testing.T) {
		require.NoError(t, adapter.Close())
		assert.True(t, called)
	})
}
