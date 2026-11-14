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
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestPageIterator_Comprehensive provides comprehensive pagination tests.
func TestPageIterator_Comprehensive(t *testing.T) {
	t.Run("large dataset pagination", func(t *testing.T) {
		// Create a large dataset
		const totalItems = 1000
		const pageSize = 50

		data := make([]KV, totalItems)
		for i := 0; i < totalItems; i++ {
			data[i] = KV{
				Key:   []byte{byte(i / 256), byte(i % 256)},
				Value: []byte{byte(i)},
			}
		}

		iter := newMockIterator(data, false)
		pageIter := NewPageIterator(iter, pageSize, nil)
		defer pageIter.Close()

		totalRetrieved := 0
		pageCount := 0
		var cursor []byte

		for {
			page, nextCursor, err := pageIter.NextPage()
			require.NoError(t, err)

			if len(page) == 0 {
				break
			}

			pageCount++
			totalRetrieved += len(page)

			// Verify page size (last page may be smaller)
			if nextCursor != nil {
				assert.Equal(t, pageSize, len(page))
			} else {
				assert.LessOrEqual(t, len(page), pageSize)
			}

			cursor = nextCursor
			if cursor == nil {
				break
			}

			// Create new iterator for next page
			iter = newMockIterator(data, false)
			pageIter = NewPageIterator(iter, pageSize, cursor)
		}

		assert.Equal(t, totalItems, totalRetrieved)
		assert.Equal(t, totalItems/pageSize, pageCount)
	})

	t.Run("cursor consistency", func(t *testing.T) {
		data := []KV{
			{Key: []byte("k1"), Value: []byte("v1")},
			{Key: []byte("k2"), Value: []byte("v2")},
			{Key: []byte("k3"), Value: []byte("v3")},
			{Key: []byte("k4"), Value: []byte("v4")},
		}

		iter := newMockIterator(data, false)
		pageIter := NewPageIterator(iter, 2, nil)

		page1, cursor1, err := pageIter.NextPage()
		require.NoError(t, err)
		require.NotNil(t, cursor1)

		// Use cursor to start new page iterator
		iter2 := newMockIterator(data, false)
		pageIter2 := NewPageIterator(iter2, 2, cursor1)

		page2, cursor2, err := pageIter2.NextPage()
		require.NoError(t, err)

		// Verify no overlap
		assert.NotEqual(t, page1[0].Key, page2[0].Key)
		assert.NotEqual(t, page1[1].Key, page2[0].Key)

		// Verify all keys are covered
		allKeys := append(page1, page2...)
		assert.Len(t, allKeys, 4)
		assert.Equal(t, []byte("k1"), allKeys[0].Key)
		assert.Equal(t, []byte("k2"), allKeys[1].Key)
		assert.Equal(t, []byte("k3"), allKeys[2].Key)
		assert.Equal(t, []byte("k4"), allKeys[3].Key)

		// Last cursor should be nil
		assert.Nil(t, cursor2)
	})

	t.Run("single item per page", func(t *testing.T) {
		data := []KV{
			{Key: []byte("a"), Value: []byte("1")},
			{Key: []byte("b"), Value: []byte("2")},
			{Key: []byte("c"), Value: []byte("3")},
		}

		iter := newMockIterator(data, false)
		pageIter := NewPageIterator(iter, 1, nil)
		defer pageIter.Close()

		for i := 0; i < 3; i++ {
			page, cursor, err := pageIter.NextPage()
			require.NoError(t, err)
			assert.Len(t, page, 1)

			if i < 2 {
				assert.NotNil(t, cursor)
				// Create new iterator for next page
				iter = newMockIterator(data, false)
				pageIter = NewPageIterator(iter, 1, cursor)
			} else {
				assert.Nil(t, cursor)
			}
		}
	})

	t.Run("concurrent cursor safety", func(t *testing.T) {
		data := []KV{
			{Key: []byte("a"), Value: []byte("1")},
			{Key: []byte("b"), Value: []byte("2")},
			{Key: []byte("c"), Value: []byte("3")},
			{Key: []byte("d"), Value: []byte("4")},
		}

		iter := newMockIterator(data, false)
		pageIter := NewPageIterator(iter, 2, nil)

		page1, cursor1, err := pageIter.NextPage()
		require.NoError(t, err)
		require.NotNil(t, cursor1)

		// Cursor should be safe to use even after pageIter is closed
		pageIter.Close()

		// Create new iterator with saved cursor
		iter2 := newMockIterator(data, false)
		pageIter2 := NewPageIterator(iter2, 2, cursor1)
		defer pageIter2.Close()

		page2, _, err := pageIter2.NextPage()
		require.NoError(t, err)
		assert.Len(t, page2, 2)
		assert.NotEqual(t, page1[0].Key, page2[0].Key)
	})
}

// TestPageIterator_ErrorHandling tests error scenarios.
func TestPageIterator_ErrorHandling(t *testing.T) {
	t.Run("iterator error propagation", func(t *testing.T) {
		data := []KV{
			{Key: []byte("a"), Value: []byte("1")},
		}

		iter := newMockIterator(data, false)
		iter.err = assert.AnError // Inject error

		pageIter := NewPageIterator(iter, 10, nil)
		defer pageIter.Close()

		_, _, err := pageIter.NextPage()
		assert.Error(t, err)
	})

	t.Run("nil iterator handling", func(t *testing.T) {
		// PageIterator should handle nil iterator gracefully
		pageIter := &PageIterator{
			iter:     nil,
			pageSize: 10,
			hasMore:  false,
		}

		page, cursor, err := pageIter.NextPage()
		assert.NoError(t, err)
		assert.Nil(t, page)
		assert.Nil(t, cursor)
	})
}

// Benchmark tests for iterator performance.
func BenchmarkIterator_Sequential(b *testing.B) {
	const numItems = 10000
	data := make([]KV, numItems)
	for i := 0; i < numItems; i++ {
		data[i] = KV{
			Key:   []byte{byte(i / 256), byte(i % 256)},
			Value: []byte{byte(i)},
		}
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		iter := newMockIterator(data, false)
		count := 0
		for iter.SeekToFirst(); iter.Valid(); iter.Next() {
			_ = iter.Key()
			_ = iter.Value()
			count++
		}
		iter.Close()
	}
}

func BenchmarkIterator_SeekOperations(b *testing.B) {
	const numItems = 10000
	data := make([]KV, numItems)
	for i := 0; i < numItems; i++ {
		data[i] = KV{
			Key:   []byte{byte(i / 256), byte(i % 256)},
			Value: []byte{byte(i)},
		}
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		iter := newMockIterator(data, false)
		// Seek to random positions
		for j := 0; j < 100; j++ {
			seekKey := []byte{byte(j % 256), byte(j / 256)}
			iter.Seek(seekKey)
			if iter.Valid() {
				_ = iter.Key()
			}
		}
		iter.Close()
	}
}

func BenchmarkPageIterator_Pagination(b *testing.B) {
	const numItems = 10000
	const pageSize = 100

	data := make([]KV, numItems)
	for i := 0; i < numItems; i++ {
		data[i] = KV{
			Key:   []byte{byte(i / 256), byte(i % 256)},
			Value: []byte{byte(i)},
		}
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		var cursor []byte
		totalItems := 0

		for {
			iter := newMockIterator(data, false)
			pageIter := NewPageIterator(iter, pageSize, cursor)

			page, nextCursor, err := pageIter.NextPage()
			if err != nil {
				b.Fatal(err)
			}

			totalItems += len(page)
			pageIter.Close()

			if nextCursor == nil {
				break
			}
			cursor = nextCursor
		}

		if totalItems != numItems {
			b.Fatalf("expected %d items, got %d", numItems, totalItems)
		}
	}
}

func BenchmarkCursorEncoding(b *testing.B) {
	keys := [][]byte{
		[]byte("short"),
		[]byte("medium-length-key"),
		[]byte("very-long-key-with-lots-of-characters-to-encode"),
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		key := keys[i%len(keys)]
		cursor := encodeCursor(key)
		_, err := decodeCursor(cursor)
		if err != nil {
			b.Fatal(err)
		}
	}
}
