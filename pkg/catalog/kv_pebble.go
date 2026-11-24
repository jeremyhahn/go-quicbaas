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

package catalog

import (
	"context"
	"errors"

	"github.com/cockroachdb/pebble"
)

// pebbleKV is a KV over a local PebbleDB instance.
type pebbleKV struct {
	db         *pebble.DB
	syncWrites bool
}

// newPebbleKV opens a PebbleDB instance with the given configuration.
func newPebbleKV(config *PebbleStorageConfig) (*pebbleKV, error) {
	if config == nil {
		config = DefaultPebbleStorageConfig()
	}
	if err := config.Validate(); err != nil {
		return nil, err
	}
	cfg := config.WithDefaults()

	opts := &pebble.Options{
		Cache:        pebble.NewCache(cfg.CacheSize),
		MaxOpenFiles: cfg.MaxOpenFiles,
	}
	if cfg.WriteBufferSize > 0 {
		opts.MemTableSize = uint64(cfg.WriteBufferSize)
	}
	if cfg.Compression {
		opts.Levels = []pebble.LevelOptions{{Compression: pebble.SnappyCompression}}
	}

	db, err := pebble.Open(cfg.Path, opts)
	if err != nil {
		return nil, NewStorageError("open", err)
	}
	return &pebbleKV{db: db, syncWrites: cfg.SyncWrites}, nil
}

func (p *pebbleKV) Get(ctx context.Context, key []byte) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, NewStorageError("get", err)
	}
	data, closer, err := p.db.Get(key)
	if err != nil {
		if errors.Is(err, pebble.ErrNotFound) {
			return nil, &KVKeyNotFoundError{Key: string(key)}
		}
		return nil, NewStorageError("get", err)
	}
	defer closer.Close()
	return cloneBytes(data), nil
}

func (p *pebbleKV) Scan(ctx context.Context, prefix []byte, fn func(key, value []byte) bool) error {
	iter, err := p.db.NewIter(&pebble.IterOptions{
		LowerBound: prefix,
		UpperBound: pebbleUpperBound(prefix),
	})
	if err != nil {
		return NewStorageError("scan", err)
	}
	defer iter.Close()

	for iter.First(); iter.Valid(); iter.Next() {
		if err := ctx.Err(); err != nil {
			return NewStorageError("scan", err)
		}
		if !fn(iter.Key(), iter.Value()) {
			break
		}
	}
	if err := iter.Error(); err != nil {
		return NewStorageError("scan", err)
	}
	return nil
}

func (p *pebbleKV) Apply(ctx context.Context, batch *KVBatch) error {
	if err := ctx.Err(); err != nil {
		return NewStorageError("apply", err)
	}
	if batch == nil || batch.Len() == 0 {
		return nil
	}
	b := p.db.NewBatch()
	defer b.Close()
	for _, op := range batch.Ops() {
		var err error
		if op.Delete {
			err = b.Delete(op.Key, nil)
		} else {
			err = b.Set(op.Key, op.Value, nil)
		}
		if err != nil {
			return NewStorageError("apply", err)
		}
	}
	writeOpts := pebble.NoSync
	if p.syncWrites {
		writeOpts = pebble.Sync
	}
	if err := b.Commit(writeOpts); err != nil {
		return NewStorageError("apply", err)
	}
	return nil
}

// Flush flushes the memtable to disk; Storage.Sync calls it when the KV
// offers it.
func (p *pebbleKV) Flush() error {
	return p.db.Flush()
}

func (p *pebbleKV) Close() error {
	if p.db == nil {
		return nil
	}
	return p.db.Close()
}

func pebbleUpperBound(prefix []byte) []byte {
	if len(prefix) == 0 {
		return nil
	}
	upperBound := make([]byte, len(prefix))
	copy(upperBound, prefix)
	for i := len(upperBound) - 1; i >= 0; i-- {
		if upperBound[i] < 0xFF {
			upperBound[i]++
			return upperBound[:i+1]
		}
	}
	return nil
}
