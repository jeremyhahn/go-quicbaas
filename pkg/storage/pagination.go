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

package storage

import (
	"encoding/base64"

	"github.com/jeremyhahn/go-quicbaas/pkg/errors"
)

// SortOrder defines the direction of iteration.
type SortOrder int

const (
	// Ascending indicates forward iteration from lower to higher keys.
	Ascending SortOrder = iota

	// Descending indicates reverse iteration from higher to lower keys.
	Descending
)

// String returns the string representation of the sort order.
func (s SortOrder) String() string {
	switch s {
	case Ascending:
		return "ascending"
	case Descending:
		return "descending"
	default:
		return "unknown"
	}
}

// KV represents a key-value pair.
type KV struct {
	Key   []byte
	Value []byte
}

// PageIterator provides cursor-based pagination over an iterator.
// It allows efficient retrieval of data in pages with resumable cursors.
type PageIterator struct {
	iter     Iterator
	pageSize int
	cursor   []byte
	hasMore  bool
	err      error

	// reusable buffers to minimize per-item allocations
	keyBuf []byte
	valBuf []byte
	kvs    []KV
}

// NewPageIterator creates a new page iterator with the given page size.
// If cursor is non-nil, iteration starts from that cursor position.
// The cursor should be obtained from a previous call to NextPage.
func NewPageIterator(iter Iterator, pageSize int, cursor []byte) *PageIterator {
	if pageSize <= 0 {
		pageSize = 100 // default page size
	}

	pi := &PageIterator{
		iter:     iter,
		pageSize: pageSize,
		cursor:   cursor,
		hasMore:  true,
	}

	// Position iterator at cursor if provided
	if len(cursor) > 0 {
		// Decode cursor (it's base64 encoded key)
		key, err := decodeCursor(cursor)
		if err != nil {
			pi.err = &errors.QBError{
				Code: errors.ErrInvalidInput,
				Op:   "PageIterator.New",
				Err:  err,
			}
			pi.hasMore = false
			return pi
		}

		// Seek to the cursor position (exclusive - start from next key)
		if !iter.Seek(key) {
			pi.hasMore = false
		}
	} else {
		// Start from the beginning
		if !iter.SeekToFirst() {
			pi.hasMore = false
		}
	}

	return pi
}

// NextPage retrieves the next page of key-value pairs.
// Returns the page data, a cursor for the next page, and any error.
// If the returned cursor is nil, there are no more pages.
// The cursor should be passed to NewPageIterator to resume iteration.
func (pi *PageIterator) NextPage() ([]KV, []byte, error) {
	if pi.err != nil {
		return nil, nil, pi.err
	}

	if !pi.hasMore {
		return nil, nil, nil
	}

	// reuse KV slice with preallocated capacity
	// reset reusable buffers; previous page slices remain valid until this call
	pi.keyBuf = pi.keyBuf[:0]
	pi.valBuf = pi.valBuf[:0]

	if cap(pi.kvs) < pi.pageSize {
		pi.kvs = make([]KV, 0, pi.pageSize)
	} else {
		pi.kvs = pi.kvs[:0]
	}
	count := 0

	for pi.iter.Valid() && count < pi.pageSize {
		// Prefer zero-copy view methods when available
		type viewIter interface {
			KeyView() []byte
			ValueView() []byte
		}
		var key, val []byte
		if vi, ok := pi.iter.(viewIter); ok {
			key = vi.KeyView()
			val = vi.ValueView()
		} else {
			key = pi.iter.Key()
			val = pi.iter.Value()
		}

		// grow keyBuf and valBuf once per element, but reuse backing arrays across pages
		kb := len(pi.keyBuf)
		pi.keyBuf = append(pi.keyBuf, key...)
		keyView := pi.keyBuf[kb : kb+len(key)]

		vb := len(pi.valBuf)
		pi.valBuf = append(pi.valBuf, val...)
		valView := pi.valBuf[vb : vb+len(val)]

		pi.kvs = append(pi.kvs, KV{Key: keyView, Value: valView})
		count++

		if !pi.iter.Next() {
			break
		}
	}

	// Check for iteration errors
	if err := pi.iter.Error(); err != nil {
		pi.err = &errors.QBError{
			Code: errors.ErrStorage,
			Op:   "PageIterator.NextPage",
			Err:  err,
		}
		return nil, nil, pi.err
	}

	// Determine if there are more pages
	var nextCursor []byte
	if pi.iter.Valid() {
		// Encode the current key as the cursor for the next page
		nextCursor = encodeCursor(pi.iter.Key())
	} else {
		pi.hasMore = false
	}

	return pi.kvs, nextCursor, nil
}

// HasMore returns true if there are potentially more pages available.
// This is a hint and should be verified by calling NextPage.
func (pi *PageIterator) HasMore() bool {
	return pi.hasMore && pi.err == nil
}

// Close releases the underlying iterator resources.
func (pi *PageIterator) Close() error {
	if pi.iter != nil {
		return pi.iter.Close()
	}
	return nil
}

// encodeCursor encodes a key as a base64 cursor string.
func encodeCursor(key []byte) []byte {
	if len(key) == 0 {
		return nil
	}
	enc := make([]byte, base64.StdEncoding.EncodedLen(len(key)))
	base64.StdEncoding.Encode(enc, key)
	return enc
}

// decodeCursor decodes a base64 cursor string to a key.
func decodeCursor(cursor []byte) ([]byte, error) {
	if len(cursor) == 0 {
		return nil, nil
	}
	dec := make([]byte, base64.StdEncoding.DecodedLen(len(cursor)))
	n, err := base64.StdEncoding.Decode(dec, cursor)
	if err != nil {
		return nil, err
	}
	return dec[:n], nil
}

// StreamPage iterates up to pageSize items from the given iterator starting at the
// provided cursor. For each item, fn is called with key and value slices that are
// only valid until the next iterator movement. Return false from fn to stop early.
// Returns the next cursor (nil if no more pages) and error if any.
func StreamPage(iter Iterator, pageSize int, cursor []byte, fn func(key, value []byte) bool) ([]byte, error) {
	if pageSize <= 0 {
		pageSize = 100
	}

	// Position iterator
	if len(cursor) > 0 {
		key, err := decodeCursor(cursor)
		if err != nil {
			return nil, err
		}
		if !iter.Seek(key) {
			return nil, nil
		}
	} else {
		if !iter.SeekToFirst() {
			return nil, nil
		}
	}

	count := 0
	for iter.Valid() && count < pageSize {
		// Prefer zero-copy views when available
		type viewIter interface {
			KeyView() []byte
			ValueView() []byte
		}
		var key, val []byte
		if vi, ok := iter.(viewIter); ok {
			key = vi.KeyView()
			val = vi.ValueView()
		} else {
			key = iter.Key()
			val = iter.Value()
		}

		if !fn(key, val) {
			break
		}
		count++

		if !iter.Next() {
			break
		}
	}

	if err := iter.Error(); err != nil {
		return nil, err
	}

	var nextCursor []byte
	if iter.Valid() {
		nextCursor = encodeCursor(iter.Key())
	}
	return nextCursor, nil
}

// cloneBytes creates a copy of the byte slice to ensure it remains valid
// after iterator operations.
func cloneBytes(b []byte) []byte {
	if b == nil {
		return nil
	}
	clone := make([]byte, len(b))
	copy(clone, b)
	return clone
}
