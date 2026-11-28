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
	"fmt"
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

// batchCountingStorage is a mockStorage that implements BatchWriter,
// recording the number of WriteBatch calls and the pairs they carry.
type batchCountingStorage struct {
	*mockStorage
	calls int
	pairs int
}

func (s *batchCountingStorage) WriteBatch(ctx context.Context, kvs []KeyValue) error {
	s.calls++
	s.pairs += len(kvs)
	for _, kv := range kvs {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := s.Set(kv.Key, kv.Value); err != nil {
			return err
		}
	}
	return nil
}

func TestBackupManager_RestoreUsesBatchWriter(t *testing.T) {
	store := &batchCountingStorage{mockStorage: &mockStorage{MemoryStorage: memory.New(nil)}}

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

	for i := 0; i < 500; i++ {
		require.NoError(t, store.Set([]byte(fmt.Sprintf("backup-test/k%03d", i)), []byte(fmt.Sprintf("v%d", i))))
	}

	ctx := context.Background()
	b, err := mgr.CreateBackup(ctx, BackupTypeFull)
	require.NoError(t, err)

	require.NoError(t, store.Set([]byte("backup-test/k042"), []byte("changed")))

	require.NoError(t, mgr.RestoreBackup(ctx, b.ID, nil))

	assert.Equal(t, 1, store.calls, "restore writes the shard in one batch")
	assert.Equal(t, 500, store.pairs)

	got, err := store.Get([]byte("backup-test/k042"))
	require.NoError(t, err)
	assert.Equal(t, "v42", string(got))
}

func TestBackupManager_RestoreShardStopsOnCancelledContext(t *testing.T) {
	mgr, store := newNilRaftManager(t)

	var buf bytes.Buffer
	require.NoError(t, writeKeyValue(&buf, []byte("backup-test/cancelled"), []byte("v")))
	data := buf.Bytes()
	snap := &ShardSnapshot{
		ShardID:  1,
		Data:     data,
		Checksum: ComputeChecksum(data),
		Size:     int64(len(data)),
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := mgr.restoreShardSnapshot(ctx, snap, &RestoreOptions{})
	require.Error(t, err)

	_, err = store.Get([]byte("backup-test/cancelled"))
	assert.Error(t, err, "no key is written after the context is cancelled")
}

// blockingWriteStorage is a store without BatchWriter whose context-aware
// write blocks until its context is cancelled, like a Raft proposal that
// waits for a quorum.
type blockingWriteStorage struct {
	*mockStorage
	started chan struct{}
	writes  int
}

func (s *blockingWriteStorage) Set(key, value []byte) error {
	s.writes++
	return s.mockStorage.Set(key, value)
}

func (s *blockingWriteStorage) SetWithContext(ctx context.Context, key, value []byte) error {
	s.writes++
	if s.writes == 1 {
		close(s.started)
	}
	<-ctx.Done()
	return ctx.Err()
}

// TestBackupManager_RestoreShardPassesContextToWrites proves the per-key
// restore path hands the request context to the store: a write already in
// flight returns when the caller cancels, and no later key is written.
func TestBackupManager_RestoreShardPassesContextToWrites(t *testing.T) {
	mgr, base := newNilRaftManager(t)
	store := &blockingWriteStorage{mockStorage: base, started: make(chan struct{})}
	mgr.storage = store

	var buf bytes.Buffer
	require.NoError(t, writeKeyValue(&buf, []byte("backup-test/inflight-1"), []byte("v1")))
	require.NoError(t, writeKeyValue(&buf, []byte("backup-test/inflight-2"), []byte("v2")))
	data := buf.Bytes()
	snap := &ShardSnapshot{
		ShardID:  1,
		Data:     data,
		Checksum: ComputeChecksum(data),
		Size:     int64(len(data)),
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	done := make(chan error, 1)
	go func() { done <- mgr.restoreShardSnapshot(ctx, snap, &RestoreOptions{}) }()

	select {
	case <-store.started:
	case <-time.After(5 * time.Second):
		t.Fatal("restore never started a write")
	}
	cancel()

	select {
	case err := <-done:
		require.Error(t, err)
	case <-time.After(5 * time.Second):
		t.Fatal("restore kept waiting on a write after the context was cancelled")
	}

	assert.Equal(t, 1, store.writes, "no key is written after the context is cancelled")
	for _, k := range []string{"backup-test/inflight-1", "backup-test/inflight-2"} {
		_, err := base.Get([]byte(k))
		assert.Error(t, err, "key %s must not be written", k)
	}
}
