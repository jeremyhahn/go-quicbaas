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

// file_kv_store_basic_test.go covers FileKVStore methods that the
// existing raft-tagged tests already exercise but the iam build does not
// see. Keeping the basic surface non-tagged ensures the stub backends and
// the file-backed KV store remain healthy under every build variant.

package server

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFileKVStore_RoundTrip(t *testing.T) {
	store, err := NewFileKVStore(t.TempDir())
	require.NoError(t, err)
	ctx := context.Background()

	t.Run("put-get-delete-exists", func(t *testing.T) {
		require.NoError(t, store.Put(ctx, "alpha/one", []byte("v1")))
		require.NoError(t, store.Put(ctx, "alpha/two", []byte("v2")))
		require.NoError(t, store.Put(ctx, "beta/x", []byte("v3")))

		got, err := store.Get(ctx, "alpha/one")
		require.NoError(t, err)
		assert.Equal(t, []byte("v1"), got)

		ok, err := store.Exists(ctx, "alpha/one")
		require.NoError(t, err)
		assert.True(t, ok)

		ok, err = store.Exists(ctx, "missing")
		require.NoError(t, err)
		assert.False(t, ok)

		keys, err := store.List(ctx, "alpha/")
		require.NoError(t, err)
		assert.ElementsMatch(t, []string{"alpha/one", "alpha/two"}, keys)

		require.NoError(t, store.Delete(ctx, "alpha/one"))
		ok, err = store.Exists(ctx, "alpha/one")
		require.NoError(t, err)
		assert.False(t, ok)
	})

	t.Run("delete missing key is a no-op", func(t *testing.T) {
		require.NoError(t, store.Delete(ctx, "never-existed"))
	})

	t.Run("DataDir reports the configured directory", func(t *testing.T) {
		assert.NotEmpty(t, store.DataDir())
	})

	require.NoError(t, store.Close())
}

func TestFileBackend_Stub(t *testing.T) {
	// NewFileBackend returns nil (BaaS no longer hosts a local DB engine).
	got := NewFileBackend(nil, 0, nil)
	assert.Nil(t, got)

	// Close on the nil-valued type is safe via a typed receiver.
	var fb *FileBackend
	assert.NoError(t, fb.Close())
}

func TestLocalBackend_Stub(t *testing.T) {
	got := NewLocalBackend(0, nil)
	assert.Nil(t, got)

	var lb *LocalBackend
	assert.NoError(t, lb.Close())
}

func TestMemoryBackend_Stub(t *testing.T) {
	got := NewMemoryBackend(nil, 0, nil)
	assert.Nil(t, got)

	var mb *MemoryBackend
	assert.NoError(t, mb.Close())
}
