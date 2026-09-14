//go:build !integration

// Copyright (c) 2026 Jeremy Hahn
// Copyright (c) 2026 Automate The Things, LLC
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

package server

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"
	qrdbnode "github.com/jeremyhahn/go-qrdb/sdk/go/node"

	dberrors "github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
)

// createTestSDKKVStore creates an sdkKVStore backed by a real qrdb
// standalone node (memory backend) for unit testing. This exercises the
// real sdkKVStore code path through the qrdb SDK without requiring a
// running networked server.
//
// TODO Phase F: consolidate with the real-engine integration tests once the
// Phase F test harness is in place.
func createTestSDKKVStore(t *testing.T) interfaces.KVStore {
	t.Helper()

	ctx := context.Background()
	cfg := &qrdbsdk.DaemonConfig{}
	cfg.Raft.DataDir = t.TempDir() // vector storage requires a data dir
	cfg.Databases = []qrdbsdk.DaemonDatabaseConfig{
		{Name: "default", Engine: "mvcc", Backend: "memory"},
	}
	node, err := qrdbnode.NewStandaloneNode(ctx, cfg, nil)
	require.NoError(t, err, "standalone node construction must succeed for sdkKVStore unit tests")
	t.Cleanup(func() { _ = node.Close() })

	client := node.Client()

	// Returned as the INTERFACE, not as *sdkKVStore. newSDKKVStore chooses its
	// concrete type from what the transport carries: a transport offering the
	// guarded verbs yields *guardedSDKKVStore, and asserting the base type
	// panicked the moment qrdb's transport grew them — a test failing on a
	// capability being ADDED, while every verb it exercises kept working.
	//
	// These tests are about the KVStore contract, so the contract is what they
	// hold. A test that needs a specific concrete type should assert it itself
	// and say why.
	return newSDKKVStore(client, 1)
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

	t.Run("get missing key returns QBError ErrNotFound", func(t *testing.T) {
		_, err := store.Get(ctx, "no-such-key")
		require.Error(t, err)
		var de *dberrors.QBError
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

// compiletimeCheck ensures sdkKVStore still satisfies the KV interface expected
// by callers that receive it as an interfaces.KVStore.
var _ = (*qrdbsdk.Client)(nil) // verify import is live
