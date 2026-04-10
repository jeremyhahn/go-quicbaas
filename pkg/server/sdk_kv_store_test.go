//go:build !integration

package server

import (
	"context"
	"testing"

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	dberrors "github.com/jeremyhahn/go-quicbaas/pkg/errors"
)

// createTestSDKKVStore creates an sdkKVStore backed by an in-memory
// baasTransport for unit testing. This exercises the real sdkKVStore code
// path through the qrdb SDK without requiring a running server.
func createTestSDKKVStore(t *testing.T) *sdkKVStore {
	t.Helper()
	memStore := NewMemoryKVStore()
	bt := newBaasTransport(memStore, nil, nil, nil, nil)
	client, err := qrdbsdk.NewClient(bt)
	require.NoError(t, err)
	store := newSDKKVStore(client, 1)
	return store.(*sdkKVStore)
}

// ---------------------------------------------------------------------------
// sdkKVStore.Put / Get round-trip
// ---------------------------------------------------------------------------

func TestSDKKVStore_PutGetRoundTrip(t *testing.T) {
	store := createTestSDKKVStore(t)
	ctx := context.Background()

	t.Run("stores and retrieves value", func(t *testing.T) {
		err := store.Put(ctx, "sdk-key", []byte("sdk-value"))
		require.NoError(t, err)

		val, err := store.Get(ctx, "sdk-key")
		require.NoError(t, err)
		assert.Equal(t, []byte("sdk-value"), val)
	})

	t.Run("get missing key returns DragonError ErrNotFound", func(t *testing.T) {
		_, err := store.Get(ctx, "no-such-key")
		require.Error(t, err)
		var de *dberrors.DragonError
		assert.ErrorAs(t, err, &de)
		assert.Equal(t, dberrors.ErrNotFound, de.Code)
	})
}

// ---------------------------------------------------------------------------
// sdkKVStore.Delete
// ---------------------------------------------------------------------------

func TestSDKKVStore_Delete(t *testing.T) {
	store := createTestSDKKVStore(t)
	ctx := context.Background()

	t.Run("deletes existing key", func(t *testing.T) {
		require.NoError(t, store.Put(ctx, "del-key", []byte("v")))

		err := store.Delete(ctx, "del-key")
		require.NoError(t, err)

		_, err = store.Get(ctx, "del-key")
		require.Error(t, err)
	})

	t.Run("delete non-existent key is no-op", func(t *testing.T) {
		err := store.Delete(ctx, "nope")
		require.NoError(t, err)
	})
}

// ---------------------------------------------------------------------------
// sdkKVStore.Scan
// ---------------------------------------------------------------------------

func TestSDKKVStore_Scan(t *testing.T) {
	store := createTestSDKKVStore(t)
	ctx := context.Background()

	require.NoError(t, store.Put(ctx, "scan/a", []byte("1")))
	require.NoError(t, store.Put(ctx, "scan/b", []byte("2")))
	require.NoError(t, store.Put(ctx, "other/c", []byte("3")))

	t.Run("iterates matching keys in sorted order", func(t *testing.T) {
		var keys []string
		err := store.Scan(ctx, "scan/", func(key string, value []byte) error {
			keys = append(keys, key)
			return nil
		})
		require.NoError(t, err)
		assert.Len(t, keys, 2)
		// Verify sorted order
		assert.Equal(t, "scan/a", keys[0])
		assert.Equal(t, "scan/b", keys[1])
	})

	t.Run("propagates callback error", func(t *testing.T) {
		cbErr := &ServerMessageError{Message: "stop"}
		err := store.Scan(ctx, "scan/", func(key string, value []byte) error {
			return cbErr
		})
		require.Error(t, err)
		assert.Equal(t, cbErr, err)
	})

	t.Run("empty result for non-matching prefix", func(t *testing.T) {
		var count int
		err := store.Scan(ctx, "zzz/", func(key string, value []byte) error {
			count++
			return nil
		})
		require.NoError(t, err)
		assert.Equal(t, 0, count)
	})
}

// ---------------------------------------------------------------------------
// sdkKVStore.List
// ---------------------------------------------------------------------------

func TestSDKKVStore_List(t *testing.T) {
	store := createTestSDKKVStore(t)
	ctx := context.Background()

	require.NoError(t, store.Put(ctx, "list/a", []byte("1")))
	require.NoError(t, store.Put(ctx, "list/b", []byte("2")))
	require.NoError(t, store.Put(ctx, "nope/c", []byte("3")))

	t.Run("returns keys matching prefix", func(t *testing.T) {
		keys, err := store.List(ctx, "list/")
		require.NoError(t, err)
		assert.Len(t, keys, 2)
	})

	t.Run("returns empty for non-matching prefix", func(t *testing.T) {
		keys, err := store.List(ctx, "zzz/")
		require.NoError(t, err)
		assert.Empty(t, keys)
	})
}

// ---------------------------------------------------------------------------
// sdkKVStore.Exists
// ---------------------------------------------------------------------------

func TestSDKKVStore_Exists(t *testing.T) {
	store := createTestSDKKVStore(t)
	ctx := context.Background()

	require.NoError(t, store.Put(ctx, "exists-key", []byte("v")))

	t.Run("returns true for existing key", func(t *testing.T) {
		ok, err := store.Exists(ctx, "exists-key")
		require.NoError(t, err)
		assert.True(t, ok)
	})

	t.Run("returns false for non-existent key", func(t *testing.T) {
		ok, err := store.Exists(ctx, "missing-key")
		require.NoError(t, err)
		assert.False(t, ok)
	})
}
