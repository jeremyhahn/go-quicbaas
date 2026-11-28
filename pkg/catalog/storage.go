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
)

// Storage provides low-level catalog storage operations: entries, their
// secondary indexes and iteration, over a KV. Every write commits an entry
// together with its index keys in one atomic KV batch.
type Storage interface {
	// Entry operations
	GetEntry(ctx context.Context, key string) (*CatalogEntry, error)
	PutEntry(ctx context.Context, entry *CatalogEntry) error
	DeleteEntry(ctx context.Context, key string) error

	// Batch operations
	GetEntries(ctx context.Context, keys []string) ([]*CatalogEntry, error)
	PutEntries(ctx context.Context, entries []*CatalogEntry) error
	DeleteEntries(ctx context.Context, keys []string) error

	// Iteration
	Iterate(ctx context.Context, prefix string, fn func(*CatalogEntry) bool) error
	IterateKeys(ctx context.Context, prefix string, fn func(string) bool) error

	// Index operations
	Indexes() IndexManager

	// Lifecycle
	Close() error
	Sync() error
}

// storageInputError describes an argument the storage layer refuses.
type storageInputError struct {
	field string
	msg   string
}

func (e *storageInputError) Error() string {
	return e.field + ": " + e.msg
}

// kvStorage implements Storage over a KV.
type kvStorage struct {
	kv           KV
	indexManager IndexManager

	// gate keeps the KV open for the operations running against it and makes
	// Close wait for them, so no call is ever issued on a closed KV.
	gate *storeGate
}

// NewKVStorage returns a Storage over kv. The storage owns kv: Close closes it.
func NewKVStorage(kv KV) (Storage, error) {
	if kv == nil {
		return nil, NewInvalidEntryError("kv", "KV store is required")
	}
	// The index manager shares the storage's gate so that index reads observe
	// the close of the store exactly as entry reads do.
	// kv and gate are both set here, so the index manager is built directly
	// rather than through newGatedIndexManager's nil checks.
	gate := newStoreGate()
	indexMgr := &indexManager{kv: kv, gate: gate}
	return &kvStorage{kv: kv, indexManager: indexMgr, gate: gate}, nil
}

func (s *kvStorage) GetEntry(ctx context.Context, key string) (*CatalogEntry, error) {
	if err := s.gate.enter(); err != nil {
		return nil, err
	}
	defer s.gate.leave()
	if err := ctx.Err(); err != nil {
		return nil, NewStorageError("get", err)
	}

	objKey, err := ParseKey(key)
	if err != nil {
		return nil, err
	}

	data, err := s.kv.Get(ctx, objKey.CatalogKey())
	if err != nil {
		if isKVKeyNotFound(err) {
			return nil, NewEntryNotFoundError(key)
		}
		return nil, NewStorageError("get", err)
	}

	entry, err := DecodeCatalogEntry(data)
	if err != nil {
		return nil, NewDecodingError(key, err)
	}
	return entry, nil
}

func (s *kvStorage) PutEntry(ctx context.Context, entry *CatalogEntry) error {
	if err := s.gate.enter(); err != nil {
		return err
	}
	defer s.gate.leave()
	if err := ctx.Err(); err != nil {
		return NewStorageError("put", err)
	}
	if entry == nil {
		return NewStorageError("put", &storageInputError{field: "entry", msg: "entry cannot be nil"})
	}
	if entry.Key == "" {
		return NewKeyEmptyError()
	}

	objKey, err := ParseKey(entry.Key)
	if err != nil {
		return err
	}

	data, err := entry.Encode()
	if err != nil {
		return NewEncodingError(entry.Key, err)
	}

	batch := &KVBatch{}
	batch.Set(objKey.CatalogKey(), data)
	if err := s.indexManager.WriteIndexes(batch, entry); err != nil {
		return err
	}
	if err := s.kv.Apply(ctx, batch); err != nil {
		return NewStorageError("put", err)
	}
	return nil
}

func (s *kvStorage) DeleteEntry(ctx context.Context, key string) error {
	if err := s.gate.enter(); err != nil {
		return err
	}
	defer s.gate.leave()
	if err := ctx.Err(); err != nil {
		return NewStorageError("delete", err)
	}

	objKey, err := ParseKey(key)
	if err != nil {
		return err
	}

	existing, err := s.GetEntry(ctx, key)
	if err != nil {
		if IsNotFound(err) {
			return nil
		}
		return err
	}

	batch := &KVBatch{}
	batch.Delete(objKey.CatalogKey())
	if err := s.indexManager.DeleteIndexes(batch, existing); err != nil {
		return err
	}
	if err := s.kv.Apply(ctx, batch); err != nil {
		return NewStorageError("delete", err)
	}
	return nil
}

func (s *kvStorage) GetEntries(ctx context.Context, keys []string) ([]*CatalogEntry, error) {
	if err := s.gate.enter(); err != nil {
		return nil, err
	}
	defer s.gate.leave()
	if err := ctx.Err(); err != nil {
		return nil, NewStorageError("get_entries", err)
	}

	entries := make([]*CatalogEntry, 0, len(keys))
	for _, key := range keys {
		entry, err := s.GetEntry(ctx, key)
		if err != nil {
			if IsNotFound(err) {
				continue
			}
			return nil, err
		}
		entries = append(entries, entry)
	}
	return entries, nil
}

func (s *kvStorage) PutEntries(ctx context.Context, entries []*CatalogEntry) error {
	if err := s.gate.enter(); err != nil {
		return err
	}
	defer s.gate.leave()
	if err := ctx.Err(); err != nil {
		return NewStorageError("put_entries", err)
	}
	if len(entries) == 0 {
		return nil
	}

	batch := &KVBatch{}
	for _, entry := range entries {
		if entry == nil {
			continue
		}
		if entry.Key == "" {
			return NewKeyEmptyError()
		}

		objKey, err := ParseKey(entry.Key)
		if err != nil {
			return err
		}
		data, err := entry.Encode()
		if err != nil {
			return NewEncodingError(entry.Key, err)
		}
		batch.Set(objKey.CatalogKey(), data)
		if err := s.indexManager.WriteIndexes(batch, entry); err != nil {
			return err
		}
	}

	if err := s.kv.Apply(ctx, batch); err != nil {
		return NewStorageError("put_entries", err)
	}
	return nil
}

func (s *kvStorage) DeleteEntries(ctx context.Context, keys []string) error {
	if err := s.gate.enter(); err != nil {
		return err
	}
	defer s.gate.leave()
	if err := ctx.Err(); err != nil {
		return NewStorageError("delete_entries", err)
	}
	if len(keys) == 0 {
		return nil
	}

	existingEntries := make([]*CatalogEntry, 0, len(keys))
	for _, key := range keys {
		entry, err := s.GetEntry(ctx, key)
		if err != nil {
			if IsNotFound(err) {
				continue
			}
			return err
		}
		existingEntries = append(existingEntries, entry)
	}

	batch := &KVBatch{}
	for _, entry := range existingEntries {
		objKey, err := ParseKey(entry.Key)
		if err != nil {
			continue
		}
		batch.Delete(objKey.CatalogKey())
		if err := s.indexManager.DeleteIndexes(batch, entry); err != nil {
			return err
		}
	}

	if err := s.kv.Apply(ctx, batch); err != nil {
		return NewStorageError("delete_entries", err)
	}
	return nil
}

func (s *kvStorage) Iterate(ctx context.Context, prefix string, fn func(*CatalogEntry) bool) error {
	if err := s.gate.enter(); err != nil {
		return err
	}
	defer s.gate.leave()
	if err := ctx.Err(); err != nil {
		return NewStorageError("iterate", err)
	}

	var scanErr error
	err := s.kv.Scan(ctx, []byte(PrefixCatalog+prefix), func(_, value []byte) bool {
		if err := ctx.Err(); err != nil {
			scanErr = err
			return false
		}
		entry, err := DecodeCatalogEntry(cloneBytes(value))
		if err != nil {
			return true
		}
		return fn(entry)
	})
	if scanErr != nil {
		return NewStorageError("iterate", scanErr)
	}
	if err != nil {
		return NewStorageError("iterate", err)
	}
	return nil
}

func (s *kvStorage) IterateKeys(ctx context.Context, prefix string, fn func(string) bool) error {
	if err := s.gate.enter(); err != nil {
		return err
	}
	defer s.gate.leave()
	if err := ctx.Err(); err != nil {
		return NewStorageError("iterate_keys", err)
	}

	var scanErr error
	err := s.kv.Scan(ctx, []byte(PrefixCatalog+prefix), func(key, _ []byte) bool {
		if err := ctx.Err(); err != nil {
			scanErr = err
			return false
		}
		objKey, err := ParseCatalogKey(key)
		if err != nil {
			return true
		}
		return fn(objKey.String())
	})
	if scanErr != nil {
		return NewStorageError("iterate_keys", scanErr)
	}
	if err != nil {
		return NewStorageError("iterate_keys", err)
	}
	return nil
}

func (s *kvStorage) Indexes() IndexManager { return s.indexManager }

// Close shuts the store down. It waits for the reads and writes already in
// flight to finish before closing the KV, so a background scan can never be
// left running over a closed store. Operations that arrive after Close
// receive a catalog-closed error.
func (s *kvStorage) Close() error {
	return s.gate.close(func() error {
		if err := s.kv.Close(); err != nil {
			return NewStorageError("close", err)
		}
		return nil
	})
}

// kvFlusher is implemented by a KV that buffers writes in memory and can
// flush them to durable storage on request.
type kvFlusher interface {
	Flush() error
}

// Sync flushes buffered writes when the KV buffers them; a KV whose writes
// are durable once Apply returns has nothing to flush.
func (s *kvStorage) Sync() error {
	if err := s.gate.enter(); err != nil {
		return err
	}
	defer s.gate.leave()
	if f, ok := s.kv.(kvFlusher); ok {
		if err := f.Flush(); err != nil {
			return NewStorageError("sync", err)
		}
	}
	return nil
}
