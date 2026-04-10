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
	"sync"
	"sync/atomic"

	"github.com/cockroachdb/pebble"
)

// PebbleStorage provides low-level catalog storage operations backed by PebbleDB.
// This is a direct PebbleDB storage layer for high-performance catalog operations.
type PebbleStorage interface {
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
	GetPebbleIndexManager() IndexManager

	// Lifecycle
	Close() error
	Sync() error

	// PebbleDB access for advanced operations
	DB() *pebble.DB
}

// PebbleStorageConfig configures the PebbleDB storage layer.
type PebbleStorageConfig struct {
	Path            string
	CacheSize       int64
	WriteBufferSize int
	MaxOpenFiles    int
	Compression     bool
	SyncWrites      bool
}

// DefaultPebbleStorageConfig returns sensible defaults for PebbleDB storage configuration.
func DefaultPebbleStorageConfig() *PebbleStorageConfig {
	return &PebbleStorageConfig{
		CacheSize:       64 * 1024 * 1024,
		WriteBufferSize: 16 * 1024 * 1024,
		MaxOpenFiles:    1000,
		Compression:     true,
		SyncWrites:      true,
	}
}

// Validate validates the PebbleDB storage configuration.
func (c *PebbleStorageConfig) Validate() error {
	if c.Path == "" {
		return NewStorageError("validate", &pebbleConfigError{field: "Path", msg: "path is required"})
	}
	if c.CacheSize < 0 {
		return NewStorageError("validate", &pebbleConfigError{field: "CacheSize", msg: "must be non-negative"})
	}
	if c.WriteBufferSize < 0 {
		return NewStorageError("validate", &pebbleConfigError{field: "WriteBufferSize", msg: "must be non-negative"})
	}
	if c.MaxOpenFiles < 0 {
		return NewStorageError("validate", &pebbleConfigError{field: "MaxOpenFiles", msg: "must be non-negative"})
	}
	return nil
}

// WithDefaults returns a config with defaults applied for zero values.
func (c *PebbleStorageConfig) WithDefaults() *PebbleStorageConfig {
	cfg := *c
	defaults := DefaultPebbleStorageConfig()
	if cfg.CacheSize == 0 {
		cfg.CacheSize = defaults.CacheSize
	}
	if cfg.WriteBufferSize == 0 {
		cfg.WriteBufferSize = defaults.WriteBufferSize
	}
	if cfg.MaxOpenFiles == 0 {
		cfg.MaxOpenFiles = defaults.MaxOpenFiles
	}
	return &cfg
}

type pebbleConfigError struct {
	field string
	msg   string
}

func (e *pebbleConfigError) Error() string {
	return e.field + ": " + e.msg
}

type pebbleStorage struct {
	db           *pebble.DB
	config       *PebbleStorageConfig
	indexManager IndexManager
	closed       atomic.Bool
	mu           sync.RWMutex
}

// NewPebbleStorage creates a new PebbleDB-backed storage.
func NewPebbleStorage(config *PebbleStorageConfig) (PebbleStorage, error) {
	return OpenPebbleStorage(config)
}

// OpenPebbleStorage opens an existing PebbleDB storage or creates a new one.
func OpenPebbleStorage(config *PebbleStorageConfig) (PebbleStorage, error) {
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

	indexMgr, err := NewIndexManager(db)
	if err != nil {
		//nolint:gosec // G104: db.Close() error is intentionally ignored during cleanup on initialization failure
		db.Close()
		return nil, err
	}

	return &pebbleStorage{db: db, config: cfg, indexManager: indexMgr}, nil
}

func (s *pebbleStorage) DB() *pebble.DB { return s.db }

func (s *pebbleStorage) GetEntry(ctx context.Context, key string) (*CatalogEntry, error) {
	if err := s.checkClosed(); err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, NewStorageError("get", err)
	}

	objKey, err := ParseKey(key)
	if err != nil {
		return nil, err
	}

	data, closer, err := s.db.Get(objKey.CatalogKey())
	if err != nil {
		if errors.Is(err, pebble.ErrNotFound) {
			return nil, NewEntryNotFoundError(key)
		}
		return nil, NewStorageError("get", err)
	}
	defer closer.Close()

	dataCopy := make([]byte, len(data))
	copy(dataCopy, data)

	entry, err := DecodeCatalogEntry(dataCopy)
	if err != nil {
		return nil, NewDecodingError(key, err)
	}
	return entry, nil
}

func (s *pebbleStorage) PutEntry(ctx context.Context, entry *CatalogEntry) error {
	if err := s.checkClosed(); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return NewStorageError("put", err)
	}
	if entry == nil {
		return NewStorageError("put", &pebbleConfigError{field: "entry", msg: "entry cannot be nil"})
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

	batch := s.db.NewBatch()
	defer batch.Close()

	if err := batch.Set(objKey.CatalogKey(), data, nil); err != nil {
		return NewStorageError("put", err)
	}
	if err := s.indexManager.WriteIndexes(batch, entry); err != nil {
		return err
	}

	writeOpts := pebble.NoSync
	if s.config.SyncWrites {
		writeOpts = pebble.Sync
	}
	if err := batch.Commit(writeOpts); err != nil {
		return NewStorageError("put", err)
	}
	return nil
}

func (s *pebbleStorage) DeleteEntry(ctx context.Context, key string) error {
	if err := s.checkClosed(); err != nil {
		return err
	}
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

	batch := s.db.NewBatch()
	defer batch.Close()

	if err := batch.Delete(objKey.CatalogKey(), nil); err != nil {
		return NewStorageError("delete", err)
	}
	if err := s.indexManager.DeleteIndexes(batch, existing); err != nil {
		return err
	}

	writeOpts := pebble.NoSync
	if s.config.SyncWrites {
		writeOpts = pebble.Sync
	}
	if err := batch.Commit(writeOpts); err != nil {
		return NewStorageError("delete", err)
	}
	return nil
}

func (s *pebbleStorage) GetEntries(ctx context.Context, keys []string) ([]*CatalogEntry, error) {
	if err := s.checkClosed(); err != nil {
		return nil, err
	}
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

func (s *pebbleStorage) PutEntries(ctx context.Context, entries []*CatalogEntry) error {
	if err := s.checkClosed(); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return NewStorageError("put_entries", err)
	}
	if len(entries) == 0 {
		return nil
	}

	batch := s.db.NewBatch()
	defer batch.Close()

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
		if err := batch.Set(objKey.CatalogKey(), data, nil); err != nil {
			return NewStorageError("put_entries", err)
		}
		if err := s.indexManager.WriteIndexes(batch, entry); err != nil {
			return err
		}
	}

	writeOpts := pebble.NoSync
	if s.config.SyncWrites {
		writeOpts = pebble.Sync
	}
	if err := batch.Commit(writeOpts); err != nil {
		return NewStorageError("put_entries", err)
	}
	return nil
}

func (s *pebbleStorage) DeleteEntries(ctx context.Context, keys []string) error {
	if err := s.checkClosed(); err != nil {
		return err
	}
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

	batch := s.db.NewBatch()
	defer batch.Close()

	for _, entry := range existingEntries {
		objKey, err := ParseKey(entry.Key)
		if err != nil {
			continue
		}
		if err := batch.Delete(objKey.CatalogKey(), nil); err != nil {
			return NewStorageError("delete_entries", err)
		}
		if err := s.indexManager.DeleteIndexes(batch, entry); err != nil {
			return err
		}
	}

	writeOpts := pebble.NoSync
	if s.config.SyncWrites {
		writeOpts = pebble.Sync
	}
	if err := batch.Commit(writeOpts); err != nil {
		return NewStorageError("delete_entries", err)
	}
	return nil
}

func (s *pebbleStorage) Iterate(ctx context.Context, prefix string, fn func(*CatalogEntry) bool) error {
	if err := s.checkClosed(); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return NewStorageError("iterate", err)
	}

	var iterPrefix []byte
	if prefix == "" {
		iterPrefix = []byte(PrefixCatalog)
	} else {
		iterPrefix = []byte(PrefixCatalog + prefix)
	}

	iter, err := s.db.NewIter(&pebble.IterOptions{
		LowerBound: iterPrefix,
		UpperBound: pebbleUpperBound(iterPrefix),
	})
	if err != nil {
		return NewStorageError("iterate", err)
	}
	defer iter.Close()

	for iter.First(); iter.Valid(); iter.Next() {
		if err := ctx.Err(); err != nil {
			return NewStorageError("iterate", err)
		}
		data := iter.Value()
		dataCopy := make([]byte, len(data))
		copy(dataCopy, data)

		entry, err := DecodeCatalogEntry(dataCopy)
		if err != nil {
			continue
		}
		if !fn(entry) {
			break
		}
	}
	if err := iter.Error(); err != nil {
		return NewStorageError("iterate", err)
	}
	return nil
}

func (s *pebbleStorage) IterateKeys(ctx context.Context, prefix string, fn func(string) bool) error {
	if err := s.checkClosed(); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return NewStorageError("iterate_keys", err)
	}

	var iterPrefix []byte
	if prefix == "" {
		iterPrefix = []byte(PrefixCatalog)
	} else {
		iterPrefix = []byte(PrefixCatalog + prefix)
	}

	iter, err := s.db.NewIter(&pebble.IterOptions{
		LowerBound: iterPrefix,
		UpperBound: pebbleUpperBound(iterPrefix),
	})
	if err != nil {
		return NewStorageError("iterate_keys", err)
	}
	defer iter.Close()

	for iter.First(); iter.Valid(); iter.Next() {
		if err := ctx.Err(); err != nil {
			return NewStorageError("iterate_keys", err)
		}
		key := iter.Key()
		objKey, err := ParseCatalogKey(key)
		if err != nil {
			continue
		}
		if !fn(objKey.String()) {
			break
		}
	}
	if err := iter.Error(); err != nil {
		return NewStorageError("iterate_keys", err)
	}
	return nil
}

func (s *pebbleStorage) GetPebbleIndexManager() IndexManager { return s.indexManager }

func (s *pebbleStorage) Close() error {
	if s.closed.Swap(true) {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.db != nil {
		if err := s. //nolint:gosec // G104: db.Close() error is intentionally ignored during cleanup on initialization failure
				db.Close(); err != nil {
			return NewStorageError("close", err)
		}
	}
	return nil
}

func (s *pebbleStorage) Sync() error {
	if err := s.checkClosed(); err != nil {
		return err
	}
	if s.db != nil {
		if err := s.db.Flush(); err != nil {
			return NewStorageError("sync", err)
		}
	}
	return nil
}

func (s *pebbleStorage) checkClosed() error {
	if s.closed.Load() {
		return NewCatalogClosedError()
	}
	return nil
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
