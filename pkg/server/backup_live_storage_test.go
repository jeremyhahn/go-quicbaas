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

package server

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	qrdbtransport "github.com/jeremyhahn/go-qrdb/sdk/go/transport"

	"github.com/jeremyhahn/go-quicbaas/pkg/backup"
	dberrors "github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
	qbshards "github.com/jeremyhahn/go-quicbaas/pkg/shards"
	"github.com/jeremyhahn/go-quicbaas/pkg/storage"
)

// newBackupTestServer builds a server with backups enabled to the default
// local target under a temp data directory: backups go to
// <DataDir>/backups and the manager's scratch space is
// <DataDir>/backup-work.
func newBackupTestServer(t *testing.T) *Server {
	t.Helper()
	srv := withTempServer(t)
	srv.config.Backup.Enabled = true
	srv.config.Backup.DefaultEncryption = "none"
	srv.config.Backup.StorageBackend = "local"
	srv.config.Backup.WorkDir = ""
	srv.config.Storage.DataDir = t.TempDir()
	return srv
}

// startBackupTestNode runs a standalone qrdb node and wires the server's
// qrdb client to it.
func startBackupTestNode(t *testing.T, srv *Server) {
	t.Helper()
	require.NoError(t, srv.initStandaloneQRDBNode(), "qrdb node init must succeed")
	t.Cleanup(func() { _ = srv.stopQRDBNode() })
	srv.qrdbClient = newNodeBackedShardedClient(srv.qrdbNode)
}

func TestServerBackup_RoundTripRestoresLiveKey(t *testing.T) {
	srv := newBackupTestServer(t)
	startBackupTestNode(t, srv)
	require.NoError(t, srv.startBackupManager())
	t.Cleanup(func() { _ = srv.stopBackupManager() })

	ctx := context.Background()
	kv := srv.GetKVStore(qbshards.PlatformShardID)
	require.NoError(t, kv.Put(ctx, "backup-test/k1", []byte("v1")))

	b, err := srv.backupManager.CreateBackup(ctx, backup.BackupTypeFull)
	require.NoError(t, err)
	assert.Equal(t, 1, b.Metadata.NumShards)

	require.NoError(t, kv.Put(ctx, "backup-test/k1", []byte("v2")))
	require.NoError(t, kv.Put(ctx, "backup-test/k2", []byte("later")))

	require.NoError(t, srv.backupManager.RestoreBackup(ctx, b.ID, nil))

	got, err := kv.Get(ctx, "backup-test/k1")
	require.NoError(t, err)
	assert.Equal(t, "v1", string(got))

	// Keys created after the backup stay in place.
	got, err = kv.Get(ctx, "backup-test/k2")
	require.NoError(t, err)
	assert.Equal(t, "later", string(got))
}

func TestServerBackup_BackupSurvivesManagerRestart(t *testing.T) {
	srv := newBackupTestServer(t)
	startBackupTestNode(t, srv)
	require.NoError(t, srv.startBackupManager())

	ctx := context.Background()
	kv := srv.GetKVStore(qbshards.PlatformShardID)
	require.NoError(t, kv.Put(ctx, "backup-test/k1", []byte("v1")))

	b, err := srv.backupManager.CreateBackup(ctx, backup.BackupTypeFull)
	require.NoError(t, err)

	require.NoError(t, srv.stopBackupManager())
	require.Nil(t, srv.backupManager)

	// A new manager over the same target path sees the backup.
	require.NoError(t, srv.startBackupManager())
	t.Cleanup(func() { _ = srv.stopBackupManager() })

	list, err := srv.backupManager.ListBackups(ctx, 0)
	require.NoError(t, err)
	ids := make([]string, 0, len(list.Backups))
	for _, item := range list.Backups {
		ids = append(ids, item.ID)
	}
	assert.Contains(t, ids, b.ID)

	require.NoError(t, kv.Put(ctx, "backup-test/k1", []byte("v2")))
	require.NoError(t, srv.backupManager.RestoreBackup(ctx, b.ID, nil))
	got, err := kv.Get(ctx, "backup-test/k1")
	require.NoError(t, err)
	assert.Equal(t, "v1", string(got))
}

func TestServerBackup_NoNodeFailsClosed(t *testing.T) {
	srv := newBackupTestServer(t)
	srv.qrdbClient = nil

	require.NoError(t, srv.startBackupManager(), "startup without a node stays allowed")
	t.Cleanup(func() { _ = srv.stopBackupManager() })
	require.NotNil(t, srv.backupManager)

	var b *backup.Backup
	var err error
	require.NotPanics(t, func() {
		b, err = srv.backupManager.CreateBackup(context.Background(), backup.BackupTypeFull)
	})
	require.Error(t, err)
	assert.Nil(t, b)
	var unavailable *BackupStoreUnavailableError
	assert.True(t, errors.As(err, &unavailable), "want BackupStoreUnavailableError, got %v", err)
}

// backupFakeKV is a small in-memory interfaces.KVStore for adapter tests.
type backupFakeKV struct {
	mu      sync.Mutex
	data    map[string][]byte
	puts    []string
	scanErr error
}

func newBackupFakeKV() *backupFakeKV {
	return &backupFakeKV{data: make(map[string][]byte)}
}

func (f *backupFakeKV) Put(_ context.Context, key string, value []byte) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.data[key] = append([]byte(nil), value...)
	f.puts = append(f.puts, key+"="+string(value))
	return nil
}

func (f *backupFakeKV) Get(_ context.Context, key string) ([]byte, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	v, ok := f.data[key]
	if !ok {
		return nil, &dberrors.QBError{Code: dberrors.ErrNotFound, Op: "fake.get"}
	}
	return v, nil
}

func (f *backupFakeKV) Delete(_ context.Context, key string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.data, key)
	f.puts = append(f.puts, "-"+key)
	return nil
}

func (f *backupFakeKV) Scan(_ context.Context, prefix string, fn func(string, []byte) error) error {
	f.mu.Lock()
	if f.scanErr != nil {
		f.mu.Unlock()
		return f.scanErr
	}
	// Deliberately unsorted: the adapter must sort.
	keys := make([]string, 0, len(f.data))
	for k := range f.data {
		if strings.HasPrefix(k, prefix) {
			keys = append(keys, k)
		}
	}
	sort.Sort(sort.Reverse(sort.StringSlice(keys)))
	snapshot := make(map[string][]byte, len(keys))
	for _, k := range keys {
		snapshot[k] = f.data[k]
	}
	f.mu.Unlock()
	for _, k := range keys {
		if err := fn(k, snapshot[k]); err != nil {
			return err
		}
	}
	return nil
}

func (f *backupFakeKV) List(ctx context.Context, prefix string) ([]string, error) {
	var keys []string
	err := f.Scan(ctx, prefix, func(k string, _ []byte) error {
		keys = append(keys, k)
		return nil
	})
	return keys, err
}

func (f *backupFakeKV) Exists(ctx context.Context, key string) (bool, error) {
	_, err := f.Get(ctx, key)
	return err == nil, nil
}

func iterKeys(it storage.Iterator) []string {
	var keys []string
	for it.SeekToFirst(); it.Valid(); it.Next() {
		keys = append(keys, string(it.Key()))
	}
	return keys
}

func TestBackupLiveStorage_IteratorAndBatch(t *testing.T) {
	fake := newBackupFakeKV()
	ctx := context.Background()
	for _, k := range []string{"backup-test/c", "backup-test/a", "backup-test/b", "other/x"} {
		require.NoError(t, fake.Put(ctx, k, []byte("v-"+k)))
	}
	fake.puts = nil
	store := newBackupLiveStorage(func() (interfaces.KVStore, error) { return fake, nil })

	t.Run("sorted", func(t *testing.T) {
		it := store.NewIterator(&storage.IteratorOptions{})
		defer it.Close()
		assert.Equal(t, []string{"backup-test/a", "backup-test/b", "backup-test/c", "other/x"}, iterKeys(it))
		require.NoError(t, it.Error())
	})

	t.Run("bounds", func(t *testing.T) {
		it := store.NewIterator(&storage.IteratorOptions{
			LowerBound: []byte("backup-test/b"),
			UpperBound: []byte("other/"),
		})
		defer it.Close()
		assert.Equal(t, []string{"backup-test/b", "backup-test/c"}, iterKeys(it))
	})

	t.Run("prefix as bounds", func(t *testing.T) {
		it := store.NewIterator(&storage.IteratorOptions{
			LowerBound: []byte("backup-test/"),
			UpperBound: []byte("backup-test0"),
		})
		defer it.Close()
		assert.Equal(t, []string{"backup-test/a", "backup-test/b", "backup-test/c"}, iterKeys(it))
	})

	t.Run("reverse and seek", func(t *testing.T) {
		it := store.NewIterator(&storage.IteratorOptions{Reverse: true})
		defer it.Close()
		assert.Equal(t, []string{"other/x", "backup-test/c", "backup-test/b", "backup-test/a"}, iterKeys(it))
		require.True(t, it.Seek([]byte("backup-test/bb")))
		assert.Equal(t, "backup-test/b", string(it.Key()))

		fwd := store.NewIterator(nil)
		defer fwd.Close()
		require.True(t, fwd.Seek([]byte("backup-test/bb")))
		assert.Equal(t, "backup-test/c", string(fwd.Key()))
		assert.Equal(t, "v-backup-test/c", string(fwd.Value()))
		require.True(t, fwd.Prev())
		assert.Equal(t, "backup-test/b", string(fwd.Key()))
	})

	t.Run("get and not found", func(t *testing.T) {
		v, err := store.Get([]byte("backup-test/a"))
		require.NoError(t, err)
		assert.Equal(t, "v-backup-test/a", string(v))
		_, err = store.Get([]byte("backup-test/missing"))
		assert.Error(t, err)
	})

	t.Run("snapshot", func(t *testing.T) {
		snap, err := store.Snapshot()
		require.NoError(t, err)
		defer snap.Close()
		require.NoError(t, fake.Put(ctx, "backup-test/a", []byte("after")))
		v, err := snap.Get([]byte("backup-test/a"))
		require.NoError(t, err)
		assert.Equal(t, "v-backup-test/a", string(v), "snapshot is taken at call time")
		_, err = snap.Get([]byte("backup-test/missing"))
		assert.Error(t, err)
		fake.puts = nil
	})

	t.Run("batch commit order", func(t *testing.T) {
		b := store.Batch()
		require.NoError(t, b.Set([]byte("backup-test/k1"), []byte("v1")))
		require.NoError(t, b.Delete([]byte("backup-test/k1")))
		require.NoError(t, b.Set([]byte("backup-test/k1"), []byte("v2")))
		assert.Empty(t, fake.puts, "nothing applied before Commit")
		require.NoError(t, b.Commit())
		require.NoError(t, b.Close())
		assert.Equal(t, []string{"backup-test/k1=v1", "-backup-test/k1", "backup-test/k1=v2"}, fake.puts)
		v, err := store.Get([]byte("backup-test/k1"))
		require.NoError(t, err)
		assert.Equal(t, "v2", string(v))

		rb := store.Batch()
		require.NoError(t, rb.Set([]byte("backup-test/k9"), []byte("v9")))
		require.NoError(t, rb.Rollback())
		require.NoError(t, rb.Commit())
		_, err = store.Get([]byte("backup-test/k9"))
		assert.Error(t, err, "rolled back write is not applied")
	})

	t.Run("maintenance no-ops", func(t *testing.T) {
		assert.NoError(t, store.Compact(nil, nil))
		assert.NoError(t, store.Flush())
		assert.NoError(t, store.Close())
	})

	t.Run("scan error surfaces", func(t *testing.T) {
		fake.scanErr = errors.New("scan failed")
		defer func() { fake.scanErr = nil }()
		it := store.NewIterator(nil)
		defer it.Close()
		assert.False(t, it.SeekToFirst())
		assert.EqualError(t, it.Error(), "scan failed")
		_, err := store.Snapshot()
		assert.Error(t, err)
	})

	t.Run("resolver error surfaces", func(t *testing.T) {
		down := newBackupLiveStorage(func() (interfaces.KVStore, error) {
			return nil, &BackupStoreUnavailableError{}
		})
		var unavailable *BackupStoreUnavailableError
		it := down.NewIterator(nil)
		defer it.Close()
		assert.False(t, it.SeekToFirst())
		assert.True(t, errors.As(it.Error(), &unavailable))
		_, err := down.Get([]byte("k"))
		assert.True(t, errors.As(err, &unavailable))
		assert.True(t, errors.As(down.Set([]byte("k"), []byte("v")), &unavailable))
		assert.True(t, errors.As(down.Delete([]byte("k")), &unavailable))
		b := down.Batch()
		require.NoError(t, b.Set([]byte("k"), []byte("v")))
		assert.True(t, errors.As(b.Commit(), &unavailable))
	})

	t.Run("server resolver without node", func(t *testing.T) {
		srv := &Server{}
		_, err := srv.backupLiveStore()
		var unavailable *BackupStoreUnavailableError
		require.True(t, errors.As(err, &unavailable))
		assert.Nil(t, unavailable.Unwrap())
		assert.Contains(t, unavailable.Error(), "no qrdb node")
		wrapped := &BackupStoreUnavailableError{Cause: errors.New("boom")}
		assert.Contains(t, wrapped.Error(), "boom")
	})
}

// backupBatchFakeKV is a backupFakeKV that also implements ExecuteBatch,
// recording the op count and the payload size of every call.
type backupBatchFakeKV struct {
	*backupFakeKV
	calls []int
	bytes []int
}

func (f *backupBatchFakeKV) ExecuteBatch(ctx context.Context, ops []qrdbtransport.Op) error {
	size := 0
	for _, op := range ops {
		size += len(op.Key) + len(op.Value) + backupRestoreOpOverhead
	}
	f.calls = append(f.calls, len(ops))
	f.bytes = append(f.bytes, size)
	for _, op := range ops {
		if op.Type != qrdbtransport.OpPut {
			return errors.New("unexpected op type")
		}
		if err := f.Put(ctx, string(op.Key), op.Value); err != nil {
			return err
		}
	}
	return nil
}

func TestBackupLiveStorage_WriteBatchChunks(t *testing.T) {
	ctx := context.Background()

	t.Run("op bound", func(t *testing.T) {
		fake := &backupBatchFakeKV{backupFakeKV: newBackupFakeKV()}
		store := newBackupLiveStorage(func() (interfaces.KVStore, error) { return fake, nil })

		kvs := make([]backup.KeyValue, 0, 1200)
		for i := 0; i < 1200; i++ {
			kvs = append(kvs, backup.KeyValue{
				Key:   []byte(fmt.Sprintf("backup-test/k%04d", i)),
				Value: []byte(fmt.Sprintf("v%d", i)),
			})
		}
		require.NoError(t, store.WriteBatch(ctx, kvs))
		assert.Equal(t, []int{512, 512, 176}, fake.calls)
		require.Len(t, fake.data, 1200)
		for _, kv := range kvs {
			assert.Equal(t, string(kv.Value), string(fake.data[string(kv.Key)]))
		}
	})

	t.Run("byte bound", func(t *testing.T) {
		fake := &backupBatchFakeKV{backupFakeKV: newBackupFakeKV()}
		store := newBackupLiveStorage(func() (interfaces.KVStore, error) { return fake, nil })

		big := make([]byte, 1<<20)
		kvs := []backup.KeyValue{
			{Key: []byte("backup-test/big1"), Value: big},
			{Key: []byte("backup-test/big2"), Value: big},
			{Key: []byte("backup-test/big3"), Value: big},
		}
		require.NoError(t, store.WriteBatch(ctx, kvs))
		assert.GreaterOrEqual(t, len(fake.calls), 2)
		total := 0
		for i, n := range fake.calls {
			total += n
			if n > 1 {
				assert.LessOrEqual(t, fake.bytes[i], backupRestoreBatchMaxBytes, "call %d exceeds the byte bound", i)
			}
		}
		assert.Equal(t, 3, total)
		for _, kv := range kvs {
			assert.Len(t, fake.data[string(kv.Key)], len(big))
		}
	})
}

func TestBackupLiveStorage_WriteBatchFallsBackToPut(t *testing.T) {
	fake := newBackupFakeKV()
	store := newBackupLiveStorage(func() (interfaces.KVStore, error) { return fake, nil })

	kvs := []backup.KeyValue{
		{Key: []byte("backup-test/c"), Value: []byte("3")},
		{Key: []byte("backup-test/a"), Value: []byte("1")},
		{Key: []byte("backup-test/b"), Value: []byte("2")},
	}
	require.NoError(t, store.WriteBatch(context.Background(), kvs))
	assert.Equal(t, []string{"backup-test/c=3", "backup-test/a=1", "backup-test/b=2"}, fake.puts)
}
