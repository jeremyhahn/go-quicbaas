//go:build !integration

package server

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	dberrors "github.com/jeremyhahn/go-quicbaas/pkg/errors"
)

// ---------------------------------------------------------------------------
// PebbleKVStoreError
// ---------------------------------------------------------------------------

func TestPebbleKVStoreError(t *testing.T) {
	t.Run("formats error with operation and cause", func(t *testing.T) {
		inner := &ServerMessageError{Message: "disk error"}
		err := &PebbleKVStoreError{Op: "put", Err: inner}
		assert.Contains(t, err.Error(), "pebble kv store: put:")
		assert.Contains(t, err.Error(), "disk error")
	})

	t.Run("unwrap returns inner error", func(t *testing.T) {
		inner := &ServerMessageError{Message: "cause"}
		err := &PebbleKVStoreError{Op: "get", Err: inner}
		assert.Equal(t, inner, err.Unwrap())
	})
}

// ---------------------------------------------------------------------------
// NewPebbleKVStore
// ---------------------------------------------------------------------------

func TestNewPebbleKVStore_EmptyDataDir(t *testing.T) {
	t.Run("returns error for empty data directory", func(t *testing.T) {
		store, err := NewPebbleKVStore("")
		assert.Nil(t, store)
		require.Error(t, err)
		var pErr *PebbleKVStoreError
		assert.True(t, errors.As(err, &pErr))
		assert.Equal(t, "new", pErr.Op)
	})
}

func TestNewPebbleKVStore_Success(t *testing.T) {
	tmpDir := t.TempDir()

	t.Run("creates store with valid directory", func(t *testing.T) {
		store, err := NewPebbleKVStore(tmpDir + "/pebble-test")
		require.NoError(t, err)
		require.NotNil(t, store)
		t.Cleanup(func() { store.Close() })
	})
}

// ---------------------------------------------------------------------------
// PebbleKVStore CRUD operations
// ---------------------------------------------------------------------------

func TestPebbleKVStore_PutGetDeleteExistsListScan(t *testing.T) {
	tmpDir := t.TempDir()
	store, err := NewPebbleKVStore(tmpDir + "/pebble-crud")
	require.NoError(t, err)
	t.Cleanup(func() { store.Close() })

	ctx := context.Background()

	t.Run("put and get round-trip", func(t *testing.T) {
		require.NoError(t, store.Put(ctx, "key1", []byte("value1")))
		val, err := store.Get(ctx, "key1")
		require.NoError(t, err)
		assert.Equal(t, []byte("value1"), val)
	})

	t.Run("get missing key returns ErrNotFound", func(t *testing.T) {
		_, err := store.Get(ctx, "nonexistent")
		require.Error(t, err)
		var de *dberrors.QBError
		assert.True(t, errors.As(err, &de))
		assert.Equal(t, dberrors.ErrNotFound, de.Code)
	})

	t.Run("exists returns true for existing key", func(t *testing.T) {
		ok, err := store.Exists(ctx, "key1")
		require.NoError(t, err)
		assert.True(t, ok)
	})

	t.Run("exists returns false for missing key", func(t *testing.T) {
		ok, err := store.Exists(ctx, "missing")
		require.NoError(t, err)
		assert.False(t, ok)
	})

	t.Run("delete removes key", func(t *testing.T) {
		require.NoError(t, store.Put(ctx, "delme", []byte("tmp")))
		require.NoError(t, store.Delete(ctx, "delme"))
		_, err := store.Get(ctx, "delme")
		require.Error(t, err)
	})

	t.Run("list returns keys with prefix", func(t *testing.T) {
		require.NoError(t, store.Put(ctx, "ls/a", []byte("1")))
		require.NoError(t, store.Put(ctx, "ls/b", []byte("2")))
		require.NoError(t, store.Put(ctx, "other/c", []byte("3")))

		keys, err := store.List(ctx, "ls/")
		require.NoError(t, err)
		assert.Len(t, keys, 2)
	})

	t.Run("scan iterates over matching key-value pairs", func(t *testing.T) {
		var found int
		err := store.Scan(ctx, "ls/", func(key string, value []byte) error {
			found++
			return nil
		})
		require.NoError(t, err)
		assert.Equal(t, 2, found)
	})
}

// ---------------------------------------------------------------------------
// PebbleKVStore closed state
// ---------------------------------------------------------------------------

func TestPebbleKVStore_ClosedOperations(t *testing.T) {
	tmpDir := t.TempDir()
	store, err := NewPebbleKVStore(tmpDir + "/pebble-closed")
	require.NoError(t, err)
	require.NoError(t, store.Close())

	ctx := context.Background()

	t.Run("put on closed store returns error", func(t *testing.T) {
		err := store.Put(ctx, "k", []byte("v"))
		require.Error(t, err)
		var pErr *PebbleKVStoreError
		assert.True(t, errors.As(err, &pErr))
	})

	t.Run("get on closed store returns error", func(t *testing.T) {
		_, err := store.Get(ctx, "k")
		require.Error(t, err)
	})

	t.Run("delete on closed store returns error", func(t *testing.T) {
		err := store.Delete(ctx, "k")
		require.Error(t, err)
	})

	t.Run("scan on closed store returns error", func(t *testing.T) {
		err := store.Scan(ctx, "", func(key string, value []byte) error { return nil })
		require.Error(t, err)
	})

	t.Run("list on closed store returns error", func(t *testing.T) {
		_, err := store.List(ctx, "")
		require.Error(t, err)
	})

	t.Run("exists on closed store returns error", func(t *testing.T) {
		_, err := store.Exists(ctx, "k")
		require.Error(t, err)
	})

	t.Run("close is idempotent", func(t *testing.T) {
		err := store.Close()
		require.NoError(t, err)
	})
}
