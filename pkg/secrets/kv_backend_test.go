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

package secrets

import (
	"context"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// inMemKV is a simple in-memory KVStoreWriter for testing.
type inMemKV struct {
	mu   sync.RWMutex
	data map[string][]byte
}

func newInMemKV() *inMemKV {
	return &inMemKV{data: make(map[string][]byte)}
}

func (k *inMemKV) Put(_ context.Context, key string, value []byte) error {
	k.mu.Lock()
	k.data[key] = value
	k.mu.Unlock()
	return nil
}

func (k *inMemKV) Get(_ context.Context, key string) ([]byte, error) {
	k.mu.RLock()
	v := k.data[key]
	k.mu.RUnlock()
	return v, nil
}

func (k *inMemKV) Delete(_ context.Context, key string) error {
	k.mu.Lock()
	delete(k.data, key)
	k.mu.Unlock()
	return nil
}

func (k *inMemKV) Scan(_ context.Context, prefix string, fn func(key string, value []byte) error) error {
	k.mu.RLock()
	defer k.mu.RUnlock()
	for key, val := range k.data {
		if len(key) >= len(prefix) && key[:len(prefix)] == prefix {
			if err := fn(key, val); err != nil {
				return err
			}
		}
	}
	return nil
}

func (k *inMemKV) List(_ context.Context, prefix string) ([]string, error) {
	k.mu.RLock()
	defer k.mu.RUnlock()
	var keys []string
	for key := range k.data {
		if len(key) >= len(prefix) && key[:len(prefix)] == prefix {
			keys = append(keys, key)
		}
	}
	return keys, nil
}

func TestKVBackend_StoreAndGet(t *testing.T) {
	b := NewKVBackend(newInMemKV())
	ctx := context.Background()

	data := map[string][]byte{"key": []byte("value")}
	meta := map[string]string{"env": "test"}

	require.NoError(t, b.Store(ctx, "myapp/db-password", data, meta))

	gotData, gotMeta, err := b.Get(ctx, "myapp/db-password")
	require.NoError(t, err)
	assert.Equal(t, data, gotData)
	assert.Equal(t, meta, gotMeta)
}

func TestKVBackend_StoreVersioning(t *testing.T) {
	b := NewKVBackend(newInMemKV())
	ctx := context.Background()

	v1 := map[string][]byte{"pass": []byte("v1")}
	v2 := map[string][]byte{"pass": []byte("v2")}

	require.NoError(t, b.Store(ctx, "secret/pass", v1, nil))
	require.NoError(t, b.Store(ctx, "secret/pass", v2, nil))

	// Current should be v2
	cur, _, err := b.Get(ctx, "secret/pass")
	require.NoError(t, err)
	assert.Equal(t, v2, cur)

	// Version 1 should still be readable
	old, _, err := b.GetVersion(ctx, "secret/pass", 1)
	require.NoError(t, err)
	assert.Equal(t, v1, old)
}

func TestKVBackend_Delete(t *testing.T) {
	b := NewKVBackend(newInMemKV())
	ctx := context.Background()

	require.NoError(t, b.Store(ctx, "to/delete", map[string][]byte{"x": []byte("1")}, nil))
	require.NoError(t, b.Delete(ctx, "to/delete"))

	_, _, err := b.Get(ctx, "to/delete")
	assert.ErrorIs(t, err, ErrSecretDeleted)
}

func TestKVBackend_Rollback(t *testing.T) {
	b := NewKVBackend(newInMemKV())
	ctx := context.Background()

	v1 := map[string][]byte{"pass": []byte("original")}
	v2 := map[string][]byte{"pass": []byte("changed")}

	require.NoError(t, b.Store(ctx, "secret/x", v1, nil))
	require.NoError(t, b.Store(ctx, "secret/x", v2, nil))
	require.NoError(t, b.Rollback(ctx, "secret/x", 1))

	cur, _, err := b.Get(ctx, "secret/x")
	require.NoError(t, err)
	assert.Equal(t, v1, cur, "rollback should restore v1 as a new version")
}

func TestKVBackend_Destroy(t *testing.T) {
	b := NewKVBackend(newInMemKV())
	ctx := context.Background()

	require.NoError(t, b.Store(ctx, "sec/key", map[string][]byte{"k": []byte("v")}, nil))
	require.NoError(t, b.Destroy(ctx, "sec/key"))

	_, _, err := b.Get(ctx, "sec/key")
	assert.ErrorIs(t, err, ErrSecretNotFound)
}

func TestKVBackend_ListVersions(t *testing.T) {
	b := NewKVBackend(newInMemKV())
	ctx := context.Background()

	for i := 0; i < 3; i++ {
		require.NoError(t, b.Store(ctx, "multi/ver", map[string][]byte{"n": []byte("x")}, nil))
	}

	versions, err := b.ListVersions(ctx, "multi/ver")
	require.NoError(t, err)
	assert.Len(t, versions, 3)
	for i, v := range versions {
		assert.Equal(t, i+1, v.Version)
	}
}

func TestKVBackend_List(t *testing.T) {
	b := NewKVBackend(newInMemKV())
	ctx := context.Background()

	require.NoError(t, b.Store(ctx, "app/secret1", map[string][]byte{"k": []byte("1")}, nil))
	require.NoError(t, b.Store(ctx, "app/secret2", map[string][]byte{"k": []byte("2")}, nil))
	require.NoError(t, b.Store(ctx, "other/secret3", map[string][]byte{"k": []byte("3")}, nil))

	paths, err := b.List(ctx, "app/")
	require.NoError(t, err)
	assert.Len(t, paths, 2)
}

func TestKVBackend_Type(t *testing.T) {
	b := NewKVBackend(newInMemKV())
	assert.Equal(t, "internal", b.Type())
}

func TestKVBackend_Close(t *testing.T) {
	b := NewKVBackend(newInMemKV())
	assert.NoError(t, b.Close())
}

func TestKVBackend_ErrCases(t *testing.T) {
	b := NewKVBackend(newInMemKV())
	ctx := context.Background()

	// Empty path
	assert.ErrorIs(t, b.Store(ctx, "", map[string][]byte{"k": []byte("v")}, nil), ErrInvalidPath)
	_, _, err := b.Get(ctx, "")
	assert.ErrorIs(t, err, ErrInvalidPath)

	// Empty data
	assert.ErrorIs(t, b.Store(ctx, "a/b", map[string][]byte{}, nil), ErrEmptyData)

	// Not found
	_, _, err = b.Get(ctx, "nonexistent/path")
	assert.ErrorIs(t, err, ErrSecretNotFound)

	// Version must be positive
	_, _, err = b.GetVersion(ctx, "a/b", 0)
	assert.ErrorIs(t, err, ErrVersionMustBePositive)

	// Delete nonexistent
	assert.ErrorIs(t, b.Delete(ctx, "nonexistent/key"), ErrSecretNotFound)

	// Destroy nonexistent
	assert.ErrorIs(t, b.Destroy(ctx, "nonexistent/key"), ErrSecretNotFound)

	// Rollback nonexistent
	assert.ErrorIs(t, b.Rollback(ctx, "nonexistent/key", 1), ErrSecretNotFound)
}
