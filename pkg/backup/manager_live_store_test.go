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

package backup

import (
	"bytes"
	"context"
	"errors"
	"io"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
	"github.com/jeremyhahn/go-quicbaas/pkg/storage/memory"
)

// newNilRaftManager builds a backup manager with no raft node over an
// in-memory store and a local backup target.
func newNilRaftManager(t *testing.T) (*BackupManager, *mockStorage) {
	t.Helper()

	store := &mockStorage{MemoryStorage: memory.New(nil)}

	log, err := logger.New(logger.LogConfig{Level: "error", Format: "json", Output: "stdout"})
	require.NoError(t, err)

	config := &Config{
		StorageBackend:     "local",
		DefaultCompression: CompressionNone,
		DefaultEncryption:  EncryptionNone,
		WorkDir:            t.TempDir(),
		Timeout:            5 * time.Minute,
		StorageConfig: map[string]string{
			"path":   t.TempDir(),
			"prefix": "backups",
		},
	}

	mgr, err := NewBackupManager(config, store, nil, log)
	require.NoError(t, err)
	t.Cleanup(func() { _ = mgr.Close() })
	return mgr, store
}

// snapshotKeys decodes the length-prefixed records of a shard snapshot.
func snapshotKeys(t *testing.T, snap *ShardSnapshot) map[string]string {
	t.Helper()
	out := make(map[string]string)
	reader := bytes.NewReader(snap.Data)
	for {
		key, value, err := readKeyValue(reader)
		if errors.Is(err, io.EOF) {
			break
		}
		require.NoError(t, err)
		out[string(key)] = string(value)
	}
	return out
}

func TestBackupManager_NilRaftBacksUpWholeStore(t *testing.T) {
	mgr, store := newNilRaftManager(t)
	require.NoError(t, store.Set([]byte("backup-test/k1"), []byte("v1")))
	require.NoError(t, store.Set([]byte("backup-test/k2"), []byte("v2")))

	b, err := mgr.CreateBackup(context.Background(), BackupTypeFull)
	require.NoError(t, err)

	assert.Equal(t, 1, b.Metadata.NumShards)
	require.Len(t, b.Shards, 1)
	snap, ok := b.Shards[wholeStoreShardID]
	require.True(t, ok, "whole-store shard must be present")
	assert.Equal(t, wholeStoreShardID, snap.ShardID)
	assert.Equal(t, map[string]string{
		"backup-test/k1": "v1",
		"backup-test/k2": "v2",
	}, snapshotKeys(t, snap))
}

func TestBackupManager_NilRaftRestoreRecoversValues(t *testing.T) {
	mgr, store := newNilRaftManager(t)
	require.NoError(t, store.Set([]byte("backup-test/k1"), []byte("v1")))
	require.NoError(t, store.Set([]byte("backup-test/k2"), []byte("v2")))

	ctx := context.Background()
	b, err := mgr.CreateBackup(ctx, BackupTypeFull)
	require.NoError(t, err)

	require.NoError(t, store.Set([]byte("backup-test/k1"), []byte("changed")))
	require.NoError(t, store.Set([]byte("backup-test/k3"), []byte("v3")))

	require.NoError(t, mgr.RestoreBackup(ctx, b.ID, nil))

	got, err := store.Get([]byte("backup-test/k1"))
	require.NoError(t, err)
	assert.Equal(t, "v1", string(got))

	got, err = store.Get([]byte("backup-test/k2"))
	require.NoError(t, err)
	assert.Equal(t, "v2", string(got))

	// Keys created after the backup are left in place.
	got, err = store.Get([]byte("backup-test/k3"))
	require.NoError(t, err)
	assert.Equal(t, "v3", string(got))
}
