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
	"bytes"
	"context"
	"encoding/base64"
	"encoding/hex"
)

// IndexManager manages secondary indexes for the catalog.
// Index keys have empty values - only their existence matters.
// All index writes are designed to be called within one KVBatch, so they
// are applied atomically with the main catalog entry.
type IndexManager interface {
	// WriteIndexes writes all secondary indexes for a catalog entry.
	// This should be called when creating or updating an entry.
	WriteIndexes(batch *KVBatch, entry *CatalogEntry) error

	// DeleteIndexes removes all secondary indexes for a catalog entry.
	// This should be called when deleting an entry.
	DeleteIndexes(batch *KVBatch, entry *CatalogEntry) error

	// UpdateIndexes updates indexes when an entry changes.
	// It removes old indexes and writes new ones atomically.
	UpdateIndexes(batch *KVBatch, oldEntry, newEntry *CatalogEntry) error

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

// indexManager implements IndexManager over a KV.
type indexManager struct {
	kv KV

	// gate keeps the KV open for the duration of an index read. When the
	// index manager belongs to a Storage this is that store's gate, so
	// closing the store waits for index reads in flight and fails later ones
	// with a catalog-closed error instead of reading a closed KV.
	gate *storeGate
}

// NewIndexManager creates a new index manager over the given KV.
// The caller owns the KV: closing it while index reads are in flight is
// the caller's responsibility. Index managers obtained from a Storage via
// GetPebbleIndexManager are tied to that store's lifecycle instead.
func NewIndexManager(kv KV) (IndexManager, error) {
	return newGatedIndexManager(kv, newStoreGate())
}

// newGatedIndexManager creates an index manager whose reads are guarded by the
// given gate, which is the gate of the storage that owns the KV.
func newGatedIndexManager(kv KV, gate *storeGate) (IndexManager, error) {
	if kv == nil {
		return nil, NewInvalidEntryError("kv", "KV store is required")
	}
	if gate == nil {
		return nil, NewInvalidEntryError("gate", "store gate is required")
	}

	return &indexManager{kv: kv, gate: gate}, nil
}

// WriteIndexes writes all secondary indexes for a catalog entry.
func (m *indexManager) WriteIndexes(batch *KVBatch, entry *CatalogEntry) error {
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
		batch.Set(indexKey, nil)
	}

	// Index by tenant
	if entry.TenantID != "" {
		indexKey := objKey.TenantIndexKey(entry.TenantID)
		batch.Set(indexKey, nil)
	}

	// Index by checksum
	if len(entry.Checksum) > 0 {
		checksumHex := hex.EncodeToString(entry.Checksum)
		indexKey := objKey.ChecksumIndexKey(checksumHex)
		batch.Set(indexKey, nil)
	}

	// Index by status
	if entry.Status != "" {
		indexKey := objKey.StatusIndexKey(string(entry.Status))
		batch.Set(indexKey, nil)
	}

	// Index by tags
	for tag, value := range entry.Tags {
		indexKey := objKey.TagIndexKey(tag, value)
		batch.Set(indexKey, nil)
	}

	// Index by encryption key
	if keyID := entry.GetEncryptionKeyID(); keyID != "" {
		indexKey := objKey.EncryptionIndexKey(keyID)
		batch.Set(indexKey, nil)
	}

	return nil
}

// DeleteIndexes removes all secondary indexes for a catalog entry.
func (m *indexManager) DeleteIndexes(batch *KVBatch, entry *CatalogEntry) error {
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
		batch.Delete(indexKey)
	}

	// Delete tenant index
	if entry.TenantID != "" {
		indexKey := objKey.TenantIndexKey(entry.TenantID)
		batch.Delete(indexKey)
	}

	// Delete checksum index
	if len(entry.Checksum) > 0 {
		checksumHex := hex.EncodeToString(entry.Checksum)
		indexKey := objKey.ChecksumIndexKey(checksumHex)
		batch.Delete(indexKey)
	}

	// Delete status index
	if entry.Status != "" {
		indexKey := objKey.StatusIndexKey(string(entry.Status))
		batch.Delete(indexKey)
	}

	// Delete tag indexes
	for tag, value := range entry.Tags {
		indexKey := objKey.TagIndexKey(tag, value)
		batch.Delete(indexKey)
	}

	// Delete encryption key index
	if keyID := entry.GetEncryptionKeyID(); keyID != "" {
		indexKey := objKey.EncryptionIndexKey(keyID)
		batch.Delete(indexKey)
	}

	return nil
}

// UpdateIndexes updates indexes when an entry changes.
func (m *indexManager) UpdateIndexes(batch *KVBatch, oldEntry, newEntry *CatalogEntry) error {
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

	// Hold the store open for the life of the scan: a closed KV must never be
	// read (PebbleDB panics when an iterator is created on a closed database).
	if err := m.gate.enter(); err != nil {
		return nil, err
	}
	defer m.gate.leave()

	result := &ListResult{
		Keys:  make([]string, 0, limit),
		Total: -1, // Unknown without counting
	}

	// Keys up to and including the cursor were returned by earlier pages, so
	// they are skipped before the offset is applied.
	skipped := 0
	count := 0
	var lastKey []byte
	var scanErr error
	err := m.kv.Scan(ctx, prefix, func(key, _ []byte) bool {
		if err := ctx.Err(); err != nil {
			scanErr = err
			return false
		}
		if len(startCursor) > 0 && bytes.Compare(key, startCursor) <= 0 {
			return true
		}
		if skipped < offset {
			skipped++
			return true
		}
		if count >= limit {
			// Found one more item, so there are more results
			result.HasMore = true
			return false
		}

		keyCopy := cloneBytes(key)
		objKey, err := parser(keyCopy)
		if err == nil {
			result.Keys = append(result.Keys, objKey.String())
			lastKey = keyCopy
		}
		count++
		return true
	})
	if scanErr != nil {
		return nil, NewStorageError("list", scanErr)
	}
	if err != nil {
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

	// Hold the store open for the life of the iterator; see listByPrefix.
	if err := m.gate.enter(); err != nil {
		return 0, err
	}
	defer m.gate.leave()

	var count int64
	var scanErr error
	err := m.kv.Scan(ctx, prefix, func(_, _ []byte) bool {
		if err := ctx.Err(); err != nil {
			scanErr = err
			return false
		}
		count++
		return true
	})
	if scanErr != nil {
		return 0, NewStorageError("count", scanErr)
	}
	if err != nil {
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
