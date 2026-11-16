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
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// conditionalBackends are the backends whose StoreVersionIfCurrent the
// rotation relies on.
func conditionalBackends(t *testing.T) map[string]ConditionalVersionedBackend {
	t.Helper()
	return map[string]ConditionalVersionedBackend{
		"memory":    NewMemoryBackend(),
		"kv":        NewKVBackend(newInMemKV()),
		"encrypted": createTestEncryptedBackend(t),
	}
}

// readCurrent returns the record a conditional writer read at path.
func readCurrent(t *testing.T, b ConditionalVersionedBackend, path string, version int) ReadVersion {
	t.Helper()
	data, metadata, err := b.Get(context.Background(), path)
	require.NoError(t, err)
	return ReadVersion{Version: version, Data: data, Metadata: metadata}
}

func requireStale(t *testing.T, err error) *StaleVersionError {
	t.Helper()
	require.ErrorIs(t, err, ErrWriteConflict)
	var stale *StaleVersionError
	require.True(t, errors.As(err, &stale), "a conflict carries a *StaleVersionError: %v", err)
	return stale
}

func TestStoreVersionIfCurrent_WritesOverTheRecordRead(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	for name, b := range conditionalBackends(t) {
		t.Run(name, func(t *testing.T) {
			require.NoError(t, b.Store(ctx, "app/s", map[string][]byte{"k": []byte("v1")}, map[string]string{"owner": "a"}))
			read := readCurrent(t, b, "app/s", 1)

			v, err := b.StoreVersionIfCurrent(ctx, "app/s", read, read.Data, map[string]string{"owner": "a", "rotation_reason": "scheduled"})
			require.NoError(t, err)
			assert.Equal(t, 2, v)

			// The same read is now stale: version 2 superseded it.
			_, err = b.StoreVersionIfCurrent(ctx, "app/s", read, read.Data, nil)
			stale := requireStale(t, err)
			assert.Equal(t, 1, stale.Expected)
			assert.Equal(t, 2, stale.Current)
		})
	}
}

func TestStoreVersionIfCurrent_RefusesARecreationAtTheSameVersion(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	for name, b := range conditionalBackends(t) {
		t.Run(name, func(t *testing.T) {
			require.NoError(t, b.Store(ctx, "app/s", map[string][]byte{"k": []byte("old")}, nil))
			read := readCurrent(t, b, "app/s", 1)

			require.NoError(t, b.Destroy(ctx, "app/s"))
			require.NoError(t, b.Store(ctx, "app/s", map[string][]byte{"k": []byte("new")}, nil))

			_, err := b.StoreVersionIfCurrent(ctx, "app/s", read, read.Data, nil)
			stale := requireStale(t, err)
			assert.True(t, stale.Replaced, "the record at version 1 is not the one read")
			assert.Contains(t, stale.Error(), "destroyed and recreated")

			data, _, err := b.Get(ctx, "app/s")
			require.NoError(t, err)
			assert.Equal(t, []byte("new"), data["k"], "the recreation stands")
		})
	}
}

func TestStoreVersionIfCurrent_RefusesADeletedSecretAndNoVersion(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	for name, b := range conditionalBackends(t) {
		t.Run(name, func(t *testing.T) {
			require.NoError(t, b.Store(ctx, "app/s", map[string][]byte{"k": []byte("v")}, nil))
			read := readCurrent(t, b, "app/s", 1)

			_, err := b.StoreVersionIfCurrent(ctx, "app/s", ReadVersion{}, read.Data, nil)
			requireStale(t, err)

			require.NoError(t, b.Delete(ctx, "app/s"))
			_, err = b.StoreVersionIfCurrent(ctx, "app/s", read, read.Data, nil)
			requireStale(t, err)
		})
	}
}
