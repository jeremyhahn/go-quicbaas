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
	"encoding/base64"
	"encoding/hex"

	"github.com/cockroachdb/pebble"
)

// IndexManager manages secondary indexes for the catalog.
// Index keys have empty values - only their existence matters.
// All index operations are designed to be called within a PebbleDB batch
// for atomic updates with the main catalog entry.
type IndexManager interface {
	// WriteIndexes writes all secondary indexes for a catalog entry.
	// This should be called when creating or updating an entry.
	WriteIndexes(batch *pebble.Batch, entry *CatalogEntry) error

	// DeleteIndexes removes all secondary indexes for a catalog entry.
	// This should be called when deleting an entry.
	DeleteIndexes(batch *pebble.Batch, entry *CatalogEntry) error

	// UpdateIndexes updates indexes when an entry changes.
	// It removes old indexes and writes new ones atomically.
	UpdateIndexes(batch *pebble.Batch, oldEntry, newEntry *CatalogEntry) error

	// ListByStore returns object keys for entries stored in a specific backend store.
	ListByStore(ctx context.Context, store string, opts *ListOptions) (*ListResult, error)

	// ListByTenant returns object keys for entries owned by a specific tenant.
	ListByTenant(ctx context.Context, tenant string, opts *ListOptions) (*ListResult, error)

	// ListByChecksum returns object keys for entries with a specific checksum.
	ListByChecksum(ctx context.Context, checksum []byte, opts *ListOptions) (*ListResult, error)

	// ListByStatus returns object keys for entries with a specific status.
	ListByStatus(ctx context.Context, status EntryStatus, opts *ListOptions) (*ListResult, error)

	// ListByTag returns object keys for entries with a specific tag and value.
	ListByTag(ctx context.Context, tag, value string, opts *ListOptions) (*ListResult, error)

	// ListByEncryptionKey returns object keys for entries encrypted with a specific key.
	ListByEncryptionKey(ctx context.Context, keyID string, opts *ListOptions) (*ListResult, error)

	// CountByStore returns the count of entries in a specific store.
	CountByStore(ctx context.Context, store string) (int64, error)

	// CountByTenant returns the count of entries owned by a specific tenant.
	CountByTenant(ctx context.Context, tenant string) (int64, error)

	// CountByStatus returns the count of entries with a specific status.
	CountByStatus(ctx context.Context, status EntryStatus) (int64, error)
}

// indexManager implements IndexManager using PebbleDB.
type indexManager struct {
	db *pebble.DB
}

// NewIndexManager creates a new index manager for the given PebbleDB instance.
func NewIndexManager(db *pebble.DB) (IndexManager, error) {
	if db == nil {
		return nil, NewInvalidEntryError("db", "PebbleDB instance is required")
	}

	return &indexManager{db: db}, nil
}

// WriteIndexes writes all secondary indexes for a catalog entry.
func (m *indexManager) WriteIndexes(batch *pebble.Batch, entry *CatalogEntry) error {
	if batch == nil {
		return NewInvalidEntryError("batch", "batch is required")
	}
	if entry == nil {
		return NewInvalidEntryError("entry", "entry is required")
	}

	objKey, err := ParseKey(entry.Key)
	if err != nil {
		return err
	}

	// Index by store (primary store)
	if store := entry.GetPrimaryStoreName(); store != "" {
		indexKey := objKey.StoreIndexKey(store)
		if err := batch.Set(indexKey, nil, nil); err != nil {
			return NewIndexError("store", entry.Key, err)
		}
	}

	// Index by tenant
	if entry.TenantID != "" {
		indexKey := objKey.TenantIndexKey(entry.TenantID)
		if err := batch.Set(indexKey, nil, nil); err != nil {
			return NewIndexError("tenant", entry.Key, err)
		}
	}

	// Index by checksum
	if len(entry.Checksum) > 0 {
		checksumHex := hex.EncodeToString(entry.Checksum)
		indexKey := objKey.ChecksumIndexKey(checksumHex)
		if err := batch.Set(indexKey, nil, nil); err != nil {
			return NewIndexError("checksum", entry.Key, err)
		}
	}

	// Index by status
	if entry.Status != "" {
		indexKey := objKey.StatusIndexKey(string(entry.Status))
		if err := batch.Set(indexKey, nil, nil); err != nil {
			return NewIndexError("status", entry.Key, err)
		}
	}

	// Index by tags
	for tag, value := range entry.Tags {
		indexKey := objKey.TagIndexKey(tag, value)
		if err := batch.Set(indexKey, nil, nil); err != nil {
			return NewIndexError("tag", entry.Key, err)
		}
	}

	// Index by encryption key
	if keyID := entry.GetEncryptionKeyID(); keyID != "" {
		indexKey := objKey.EncryptionIndexKey(keyID)
		if err := batch.Set(indexKey, nil, nil); err != nil {
			return NewIndexError("encryption", entry.Key, err)
		}
	}

	return nil
}

// DeleteIndexes removes all secondary indexes for a catalog entry.
func (m *indexManager) DeleteIndexes(batch *pebble.Batch, entry *CatalogEntry) error {
	if batch == nil {
		return NewInvalidEntryError("batch", "batch is required")
	}
	if entry == nil {
		return NewInvalidEntryError("entry", "entry is required")
	}

	objKey, err := ParseKey(entry.Key)
	if err != nil {
		return err
	}

	// Delete store index
	if store := entry.GetPrimaryStoreName(); store != "" {
		indexKey := objKey.StoreIndexKey(store)
		if err := batch.Delete(indexKey, nil); err != nil {
			return NewIndexError("store", entry.Key, err)
		}
	}

	// Delete tenant index
	if entry.TenantID != "" {
		indexKey := objKey.TenantIndexKey(entry.TenantID)
		if err := batch.Delete(indexKey, nil); err != nil {
			return NewIndexError("tenant", entry.Key, err)
		}
	}

	// Delete checksum index
	if len(entry.Checksum) > 0 {
		checksumHex := hex.EncodeToString(entry.Checksum)
		indexKey := objKey.ChecksumIndexKey(checksumHex)
		if err := batch.Delete(indexKey, nil); err != nil {
			return NewIndexError("checksum", entry.Key, err)
		}
	}

	// Delete status index
	if entry.Status != "" {
		indexKey := objKey.StatusIndexKey(string(entry.Status))
		if err := batch.Delete(indexKey, nil); err != nil {
			return NewIndexError("status", entry.Key, err)
		}
	}

	// Delete tag indexes
	for tag, value := range entry.Tags {
		indexKey := objKey.TagIndexKey(tag, value)
		if err := batch.Delete(indexKey, nil); err != nil {
			return NewIndexError("tag", entry.Key, err)
		}
	}

	// Delete encryption key index
	if keyID := entry.GetEncryptionKeyID(); keyID != "" {
		indexKey := objKey.EncryptionIndexKey(keyID)
		if err := batch.Delete(indexKey, nil); err != nil {
			return NewIndexError("encryption", entry.Key, err)
		}
	}

	return nil
}

// UpdateIndexes updates indexes when an entry changes.
func (m *indexManager) UpdateIndexes(batch *pebble.Batch, oldEntry, newEntry *CatalogEntry) error {
	if batch == nil {
		return NewInvalidEntryError("batch", "batch is required")
	}
	if oldEntry == nil {
		return NewInvalidEntryError("oldEntry", "old entry is required")
	}
	if newEntry == nil {
		return NewInvalidEntryError("newEntry", "new entry is required")
	}

	// Delete old indexes first
	if err := m.DeleteIndexes(batch, oldEntry); err != nil {
		return err
	}

	// Write new indexes
	return m.WriteIndexes(batch, newEntry)
}

// keyParser extracts the object key from an index key.
type keyParser func([]byte) (*ObjectKey, error)

// standardKeyParser parses standard index keys (store, tenant, checksum, status, encryption).
func standardKeyParser(indexKey []byte) (*ObjectKey, error) {
	objKey, _, err := ParseIndexKey(indexKey)
	return objKey, err
}

// tagKeyParser parses tag index keys.
func tagKeyParser(indexKey []byte) (*ObjectKey, error) {
	objKey, _, _, err := ParseTagIndexKey(indexKey)
	return objKey, err
}

// ListByStore returns object keys for entries in a specific store.
func (m *indexManager) ListByStore(ctx context.Context, store string, opts *ListOptions) (*ListResult, error) {
	if store == "" {
		return nil, NewInvalidEntryError("store", "store is required")
	}
	prefix := []byte(PrefixIndex + IndexTypeStore + KeySeparator + store + KeySeparator)
	return m.listByPrefix(ctx, prefix, opts, standardKeyParser)
}

// ListByTenant returns object keys for entries owned by a specific tenant.
func (m *indexManager) ListByTenant(ctx context.Context, tenant string, opts *ListOptions) (*ListResult, error) {
	if tenant == "" {
		return nil, NewInvalidEntryError("tenant", "tenant is required")
	}
	prefix := []byte(PrefixIndex + IndexTypeTenant + KeySeparator + tenant + KeySeparator)
	return m.listByPrefix(ctx, prefix, opts, standardKeyParser)
}

// ListByChecksum returns object keys for entries with a specific checksum.
func (m *indexManager) ListByChecksum(ctx context.Context, checksum []byte, opts *ListOptions) (*ListResult, error) {
	if len(checksum) == 0 {
		return nil, NewInvalidEntryError("checksum", "checksum is required")
	}
	checksumHex := hex.EncodeToString(checksum)
	prefix := []byte(PrefixIndex + IndexTypeChecksum + KeySeparator + checksumHex + KeySeparator)
	return m.listByPrefix(ctx, prefix, opts, standardKeyParser)
}

// ListByStatus returns object keys for entries with a specific status.
func (m *indexManager) ListByStatus(ctx context.Context, status EntryStatus, opts *ListOptions) (*ListResult, error) {
	if status == "" {
		return nil, NewInvalidEntryError("status", "status is required")
	}
	prefix := []byte(PrefixIndex + IndexTypeStatus + KeySeparator + string(status) + KeySeparator)
	return m.listByPrefix(ctx, prefix, opts, standardKeyParser)
}

// ListByTag returns object keys for entries with a specific tag and value.
func (m *indexManager) ListByTag(ctx context.Context, tag, value string, opts *ListOptions) (*ListResult, error) {
	if tag == "" {
		return nil, NewInvalidEntryError("tag", "tag is required")
	}
	// Tag index format: idx:tag:{tag}:{value}:{namespace}:{path}
	prefix := []byte(PrefixIndex + IndexTypeTag + KeySeparator + tag + KeySeparator + value + KeySeparator)
	return m.listByPrefix(ctx, prefix, opts, tagKeyParser)
}

// ListByEncryptionKey returns object keys for entries encrypted with a specific key.
func (m *indexManager) ListByEncryptionKey(ctx context.Context, keyID string, opts *ListOptions) (*ListResult, error) {
	if keyID == "" {
		return nil, NewInvalidEntryError("keyID", "encryption key ID is required")
	}
	prefix := []byte(PrefixIndex + IndexTypeEncryption + KeySeparator + keyID + KeySeparator)
	return m.listByPrefix(ctx, prefix, opts, standardKeyParser)
}

// CountByStore returns the count of entries in a specific store.
func (m *indexManager) CountByStore(ctx context.Context, store string) (int64, error) {
	if store == "" {
		return 0, NewInvalidEntryError("store", "store is required")
	}
	prefix := []byte(PrefixIndex + IndexTypeStore + KeySeparator + store + KeySeparator)
	return m.countByPrefix(ctx, prefix)
}

// CountByTenant returns the count of entries owned by a specific tenant.
func (m *indexManager) CountByTenant(ctx context.Context, tenant string) (int64, error) {
	if tenant == "" {
		return 0, NewInvalidEntryError("tenant", "tenant is required")
	}
	prefix := []byte(PrefixIndex + IndexTypeTenant + KeySeparator + tenant + KeySeparator)
	return m.countByPrefix(ctx, prefix)
}

// CountByStatus returns the count of entries with a specific status.
func (m *indexManager) CountByStatus(ctx context.Context, status EntryStatus) (int64, error) {
	if status == "" {
		return 0, NewInvalidEntryError("status", "status is required")
	}
	prefix := []byte(PrefixIndex + IndexTypeStatus + KeySeparator + string(status) + KeySeparator)
	return m.countByPrefix(ctx, prefix)
}

// listByPrefix performs prefix-based iteration and returns results with pagination.
func (m *indexManager) listByPrefix(ctx context.Context, prefix []byte, opts *ListOptions, parser keyParser) (*ListResult, error) {
	if err := ctx.Err(); err != nil {
		return nil, NewStorageError("list", err)
	}

	// Apply defaults for options
	limit := 100
	offset := 0
	var startCursor []byte

	if opts != nil {
		if opts.Limit > 0 {
			limit = opts.Limit
		}
		offset = opts.Offset
		if opts.Cursor != "" {
			decoded, err := base64.StdEncoding.DecodeString(opts.Cursor)
			if err != nil {
				return nil, NewInvalidEntryError("cursor", "invalid cursor format")
			}
			startCursor = decoded
		}
	}

	// Create iterator with prefix bounds
	iterOpts := &pebble.IterOptions{
		LowerBound: prefix,
		UpperBound: indexPrefixUpperBound(prefix),
	}

	iter, err := m.db.NewIter(iterOpts)
	if err != nil {
		return nil, NewStorageError("list", err)
	}
	defer iter.Close()

	result := &ListResult{
		Keys:  make([]string, 0, limit),
		Total: -1, // Unknown without counting
	}

	// Start from cursor position if provided
	if len(startCursor) > 0 {
		iter.SeekGE(startCursor)
		// Skip the cursor key itself (we already returned it)
		if iter.Valid() && string(iter.Key()) == string(startCursor) {
			iter.Next()
		}
	} else {
		iter.First()
	}

	// Skip offset items
	for i := 0; i < offset && iter.Valid(); i++ {
		iter.Next()
	}

	// Collect results
	count := 0
	var lastKey []byte
	for iter.Valid() && count < limit+1 {
		if err := ctx.Err(); err != nil {
			return nil, NewStorageError("list", err)
		}

		key := iter.Key()
		keyCopy := make([]byte, len(key))
		copy(keyCopy, key)

		if count < limit {
			objKey, err := parser(keyCopy)
			if err == nil {
				result.Keys = append(result.Keys, objKey.String())
				lastKey = keyCopy
			}
		} else {
			// Found one more item, so there are more results
			result.HasMore = true
		}

		count++
		iter.Next()
	}

	if err := iter.Error(); err != nil {
		return nil, NewStorageError("list", err)
	}

	// Set cursor for next page
	if result.HasMore && len(lastKey) > 0 {
		result.NextCursor = base64.StdEncoding.EncodeToString(lastKey)
	}

	return result, nil
}

// countByPrefix counts all entries with the given prefix.
func (m *indexManager) countByPrefix(ctx context.Context, prefix []byte) (int64, error) {
	if err := ctx.Err(); err != nil {
		return 0, NewStorageError("count", err)
	}

	iterOpts := &pebble.IterOptions{
		LowerBound: prefix,
		UpperBound: indexPrefixUpperBound(prefix),
	}

	iter, err := m.db.NewIter(iterOpts)
	if err != nil {
		return 0, NewStorageError("count", err)
	}
	defer iter.Close()

	var count int64
	for iter.First(); iter.Valid(); iter.Next() {
		if err := ctx.Err(); err != nil {
			return 0, NewStorageError("count", err)
		}
		count++
	}

	if err := iter.Error(); err != nil {
		return 0, NewStorageError("count", err)
	}

	return count, nil
}

// indexPrefixUpperBound returns the upper bound for prefix scanning.
// This works by incrementing the last byte of the prefix.
func indexPrefixUpperBound(prefix []byte) []byte {
	if len(prefix) == 0 {
		return nil
	}

	upperBound := make([]byte, len(prefix))
	copy(upperBound, prefix)

	// Find the last non-0xFF byte and increment it
	for i := len(upperBound) - 1; i >= 0; i-- {
		if upperBound[i] < 0xFF {
			upperBound[i]++
			return upperBound[:i+1]
		}
	}

	// All bytes are 0xFF, no upper bound
	return nil
}
