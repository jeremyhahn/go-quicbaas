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
	"bytes"
	"context"
	"sort"

	qrdbtransport "github.com/jeremyhahn/go-qrdb/sdk/go/transport"

	"github.com/jeremyhahn/go-quicbaas/pkg/backup"
	dberrors "github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
	qbshards "github.com/jeremyhahn/go-quicbaas/pkg/shards"
	"github.com/jeremyhahn/go-quicbaas/pkg/storage"
)

// BackupStoreUnavailableError is returned by the backup manager's live
// storage when no qrdb node is running, so there is no keyspace to back
// up or restore into.
type BackupStoreUnavailableError struct {
	Cause error
}

// Error implements the error interface.
func (e *BackupStoreUnavailableError) Error() string {
	if e.Cause != nil {
		return "backup store unavailable: qrdb platform shard: " + e.Cause.Error()
	}
	return "backup store unavailable: no qrdb node is running"
}

// Unwrap returns the underlying cause.
func (e *BackupStoreUnavailableError) Unwrap() error {
	return e.Cause
}

// backupStoreResolver returns the key/value store the backup manager reads
// and writes, resolved at call time.
type backupStoreResolver func() (interfaces.KVStore, error)

// backupLiveStorage is the backup manager's data store: an
// interfaces.Storage over the embedded qrdb node's platform-shard
// keyspace. It resolves the key/value store on every call, so the manager
// can be built before the node runs and always sees the running node.
type backupLiveStorage struct {
	resolve backupStoreResolver
}

var _ interfaces.Storage = (*backupLiveStorage)(nil)

func newBackupLiveStorage(resolve backupStoreResolver) *backupLiveStorage {
	return &backupLiveStorage{resolve: resolve}
}

// backupLiveStore resolves the platform shard's raw key/value store. It
// fails closed with BackupStoreUnavailableError when the qrdb client is
// not initialised, instead of panicking as GetKVStore does.
func (s *Server) backupLiveStore() (interfaces.KVStore, error) {
	if s.qrdbClient == nil {
		return nil, &BackupStoreUnavailableError{}
	}
	client, err := s.qrdbClient.ForShard(qbshards.PlatformShardID)
	if err != nil {
		return nil, &BackupStoreUnavailableError{Cause: err}
	}
	return newSDKKVStore(client, qbshards.PlatformShardID), nil
}

// Get implements interfaces.Storage. A missing key returns the store's
// not-found error.
func (a *backupLiveStorage) Get(key []byte) ([]byte, error) {
	return a.GetWithContext(context.Background(), key)
}

// Set implements interfaces.Storage.
func (a *backupLiveStorage) Set(key, value []byte) error {
	return a.SetWithContext(context.Background(), key, value)
}

// Delete implements interfaces.Storage.
func (a *backupLiveStorage) Delete(key []byte) error {
	kv, err := a.resolve()
	if err != nil {
		return err
	}
	return kv.Delete(context.Background(), string(key))
}

// GetWithContext implements interfaces.Storage.
func (a *backupLiveStorage) GetWithContext(ctx context.Context, key []byte) ([]byte, error) {
	kv, err := a.resolve()
	if err != nil {
		return nil, err
	}
	return kv.Get(ctx, string(key))
}

// SetWithContext implements interfaces.Storage.
func (a *backupLiveStorage) SetWithContext(ctx context.Context, key, value []byte) error {
	kv, err := a.resolve()
	if err != nil {
		return err
	}
	return kv.Put(ctx, string(key), value)
}

// Restore batch bounds. Each chunk WriteBatch sends is one Raft proposal,
// and both bounds keep it well under the QuicRaft entry limit (see
// batchProposalMaxBytes in pkg/service).
const (
	backupRestoreBatchMaxBytes = 2 << 20
	backupRestoreBatchMaxOps   = 512
	// backupRestoreOpOverhead is the per-op framing allowance counted
	// against backupRestoreBatchMaxBytes.
	backupRestoreOpOverhead = 64
)

// backupBatchExecutor is the kv store capability that applies many ops as
// one atomic qrdb batch.
type backupBatchExecutor interface {
	ExecuteBatch(ctx context.Context, ops []qrdbtransport.Op) error
}

var _ backup.BatchWriter = (*backupLiveStorage)(nil)

// WriteBatch writes kvs to the live keyspace. When the kv store can execute
// qrdb batches it sends them in size-bounded chunks, one proposal each;
// otherwise it puts them one by one. It stops at the first error or when
// ctx is done.
func (a *backupLiveStorage) WriteBatch(ctx context.Context, kvs []backup.KeyValue) error {
	kv, err := a.resolve()
	if err != nil {
		return err
	}

	exec, ok := kv.(backupBatchExecutor)
	if !ok {
		for _, k := range kvs {
			if err := ctx.Err(); err != nil {
				return err
			}
			if err := kv.Put(ctx, string(k.Key), k.Value); err != nil {
				return err
			}
		}
		return nil
	}

	flush := func(ops []qrdbtransport.Op) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		return exec.ExecuteBatch(ctx, ops)
	}

	ops := make([]qrdbtransport.Op, 0, min(len(kvs), backupRestoreBatchMaxOps))
	size := 0
	for _, k := range kvs {
		opSize := len(k.Key) + len(k.Value) + backupRestoreOpOverhead
		if len(ops) > 0 && (len(ops) >= backupRestoreBatchMaxOps || size+opSize > backupRestoreBatchMaxBytes) {
			if err := flush(ops); err != nil {
				return err
			}
			ops = make([]qrdbtransport.Op, 0, min(len(kvs), backupRestoreBatchMaxOps))
			size = 0
		}
		ops = append(ops, qrdbtransport.Op{Type: qrdbtransport.OpPut, Key: k.Key, Value: k.Value})
		size += opSize
	}
	if len(ops) > 0 {
		return flush(ops)
	}
	return nil
}

// Batch implements interfaces.Storage. Writes are staged and applied in
// order on Commit.
func (a *backupLiveStorage) Batch() storage.Batch {
	return &backupLiveBatch{store: a}
}

// NewIterator implements interfaces.Storage. It scans the keyspace at call
// time into a sorted slice; resolver and scan errors surface from the
// iterator's Error method.
func (a *backupLiveStorage) NewIterator(opts *storage.IteratorOptions) storage.Iterator {
	entries, err := a.scan()
	if err != nil {
		return &backupSliceIterator{pos: -1, err: err}
	}
	return newBackupSliceIterator(entries, opts)
}

// Snapshot implements interfaces.Storage with a scan taken at call time.
func (a *backupLiveStorage) Snapshot() (storage.Snapshot, error) {
	entries, err := a.scan()
	if err != nil {
		return nil, err
	}
	return &backupLiveSnapshot{entries: entries}, nil
}

// Compact is a no-op: compaction is the qrdb node's concern.
func (a *backupLiveStorage) Compact(_, _ []byte) error { return nil }

// Flush is a no-op: writes go through the qrdb node.
func (a *backupLiveStorage) Flush() error { return nil }

// Close is a no-op: the store belongs to the qrdb node, which this
// adapter must not close.
func (a *backupLiveStorage) Close() error { return nil }

// backupKV is one key/value pair captured by a scan.
type backupKV struct {
	key   []byte
	value []byte
}

func (a *backupLiveStorage) scan() ([]backupKV, error) {
	kv, err := a.resolve()
	if err != nil {
		return nil, err
	}
	var entries []backupKV
	err = kv.Scan(context.Background(), "", func(key string, value []byte) error {
		entries = append(entries, backupKV{key: []byte(key), value: append([]byte(nil), value...)})
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Slice(entries, func(i, j int) bool {
		return bytes.Compare(entries[i].key, entries[j].key) < 0
	})
	return entries, nil
}

// backupLiveBatch stages writes for backupLiveStorage.
type backupLiveBatch struct {
	store *backupLiveStorage
	ops   []backupBatchOp
}

type backupBatchOp struct {
	key    []byte
	value  []byte
	delete bool
}

func (b *backupLiveBatch) Set(key, value []byte) error {
	b.ops = append(b.ops, backupBatchOp{
		key:   append([]byte(nil), key...),
		value: append([]byte(nil), value...),
	})
	return nil
}

func (b *backupLiveBatch) Delete(key []byte) error {
	b.ops = append(b.ops, backupBatchOp{key: append([]byte(nil), key...), delete: true})
	return nil
}

// Commit applies the staged writes in order. It stops at the first error.
func (b *backupLiveBatch) Commit() error {
	ops := b.ops
	b.ops = nil
	for _, op := range ops {
		var err error
		if op.delete {
			err = b.store.Delete(op.key)
		} else {
			err = b.store.Set(op.key, op.value)
		}
		if err != nil {
			return err
		}
	}
	return nil
}

func (b *backupLiveBatch) Rollback() error {
	b.ops = nil
	return nil
}

func (b *backupLiveBatch) Close() error {
	b.ops = nil
	return nil
}

// backupLiveSnapshot is a point-in-time scan of the keyspace.
type backupLiveSnapshot struct {
	entries []backupKV
}

func (s *backupLiveSnapshot) Get(key []byte) ([]byte, error) {
	i := sort.Search(len(s.entries), func(i int) bool {
		return bytes.Compare(s.entries[i].key, key) >= 0
	})
	if i < len(s.entries) && bytes.Equal(s.entries[i].key, key) {
		return s.entries[i].value, nil
	}
	return nil, &dberrors.QBError{Code: dberrors.ErrNotFound, Op: "backup.snapshot.get"}
}

func (s *backupLiveSnapshot) NewIterator(opts *storage.IteratorOptions) storage.Iterator {
	return newBackupSliceIterator(s.entries, opts)
}

func (s *backupLiveSnapshot) Close() error { return nil }

// backupSliceIterator iterates a sorted slice of entries within the
// options' bounds, in reverse when asked.
type backupSliceIterator struct {
	entries  []backupKV
	reverse  bool
	onlyKeys bool
	pos      int
	err      error
}

func newBackupSliceIterator(sorted []backupKV, opts *storage.IteratorOptions) *backupSliceIterator {
	it := &backupSliceIterator{pos: -1}
	if opts == nil {
		opts = &storage.IteratorOptions{}
	}
	it.reverse = opts.Reverse
	it.onlyKeys = opts.OnlyKeys
	for _, e := range sorted {
		if opts.LowerBound != nil && bytes.Compare(e.key, opts.LowerBound) < 0 {
			continue
		}
		if opts.UpperBound != nil && bytes.Compare(e.key, opts.UpperBound) >= 0 {
			continue
		}
		it.entries = append(it.entries, e)
	}
	if it.reverse {
		for i, j := 0, len(it.entries)-1; i < j; i, j = i+1, j-1 {
			it.entries[i], it.entries[j] = it.entries[j], it.entries[i]
		}
	}
	return it
}

func (it *backupSliceIterator) SeekToFirst() bool {
	it.pos = 0
	return it.Valid()
}

func (it *backupSliceIterator) SeekToLast() bool {
	it.pos = len(it.entries) - 1
	return it.Valid()
}

// Seek positions at the first entry at or after target in iteration
// order: key >= target forwards, key <= target in reverse.
func (it *backupSliceIterator) Seek(target []byte) bool {
	it.pos = sort.Search(len(it.entries), func(i int) bool {
		c := bytes.Compare(it.entries[i].key, target)
		if it.reverse {
			return c <= 0
		}
		return c >= 0
	})
	return it.Valid()
}

func (it *backupSliceIterator) Next() bool {
	if it.pos < len(it.entries) {
		it.pos++
	}
	return it.Valid()
}

func (it *backupSliceIterator) Prev() bool {
	if it.pos >= 0 {
		it.pos--
	}
	return it.Valid()
}

func (it *backupSliceIterator) Valid() bool {
	return it.err == nil && it.pos >= 0 && it.pos < len(it.entries)
}

func (it *backupSliceIterator) Key() []byte {
	if !it.Valid() {
		return nil
	}
	return it.entries[it.pos].key
}

func (it *backupSliceIterator) Value() []byte {
	if !it.Valid() || it.onlyKeys {
		return nil
	}
	return it.entries[it.pos].value
}

func (it *backupSliceIterator) Error() error { return it.err }

func (it *backupSliceIterator) Close() error { return nil }
