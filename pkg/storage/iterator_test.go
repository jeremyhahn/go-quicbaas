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

// TestIterator_Interface verifies the Iterator interface contract.
func TestIterator_Interface(t *testing.T) {
	t.Run("iterator methods", func(t *testing.T) {
		data := []KV{
			{Key: []byte("a"), Value: []byte("value-a")},
			{Key: []byte("b"), Value: []byte("value-b")},
			{Key: []byte("c"), Value: []byte("value-c")},
		}

		iter := newMockIterator(data, false)
		defer iter.Close()

		// Test SeekToFirst
		assert.True(t, iter.SeekToFirst())
		assert.True(t, iter.Valid())
		assert.Equal(t, []byte("a"), iter.Key())
		assert.Equal(t, []byte("value-a"), iter.Value())

		// Test Next
		assert.True(t, iter.Next())
		assert.True(t, iter.Valid())
		assert.Equal(t, []byte("b"), iter.Key())

		// Test SeekToLast
		assert.True(t, iter.SeekToLast())
		assert.True(t, iter.Valid())
		assert.Equal(t, []byte("c"), iter.Key())

		// Test Prev
		assert.True(t, iter.Prev())
		assert.True(t, iter.Valid())
		assert.Equal(t, []byte("b"), iter.Key())

		// Test Error
		assert.NoError(t, iter.Error())
	})

	t.Run("seek operations", func(t *testing.T) {
		data := []KV{
			{Key: []byte("a"), Value: []byte("1")},
			{Key: []byte("c"), Value: []byte("2")},
			{Key: []byte("e"), Value: []byte("3")},
			{Key: []byte("g"), Value: []byte("4")},
		}

		iter := newMockIterator(data, false)
		defer iter.Close()

		// Seek to exact match
		assert.True(t, iter.Seek([]byte("c")))
		assert.Equal(t, []byte("c"), iter.Key())

		// Seek to non-existent key (should find next greater)
		assert.True(t, iter.Seek([]byte("d")))
		assert.Equal(t, []byte("e"), iter.Key())

		// Seek beyond all keys
		assert.False(t, iter.Seek([]byte("z")))
		assert.False(t, iter.Valid())
	})

	t.Run("empty iterator", func(t *testing.T) {
		iter := newMockIterator([]KV{}, false)
		defer iter.Close()

		assert.False(t, iter.SeekToFirst())
		assert.False(t, iter.Valid())
		assert.Nil(t, iter.Key())
		assert.Nil(t, iter.Value())
	})

	t.Run("single item iterator", func(t *testing.T) {
		data := []KV{
			{Key: []byte("only"), Value: []byte("value")},
		}

		iter := newMockIterator(data, false)
		defer iter.Close()

		assert.True(t, iter.SeekToFirst())
		assert.True(t, iter.Valid())
		assert.Equal(t, []byte("only"), iter.Key())

		// Next should invalidate
		assert.False(t, iter.Next())
		assert.False(t, iter.Valid())
	})

	t.Run("iteration boundaries", func(t *testing.T) {
		data := []KV{
			{Key: []byte("a"), Value: []byte("1")},
			{Key: []byte("b"), Value: []byte("2")},
			{Key: []byte("c"), Value: []byte("3")},
		}

		iter := newMockIterator(data, false)
		defer iter.Close()

		// Forward iteration to end
		assert.True(t, iter.SeekToFirst())
		for i := 0; i < 2; i++ {
			assert.True(t, iter.Next())
		}
		assert.True(t, iter.Valid())
		assert.Equal(t, []byte("c"), iter.Key())

		// One more Next should invalidate
		assert.False(t, iter.Next())
		assert.False(t, iter.Valid())

		// Backward iteration from end
		assert.True(t, iter.SeekToLast())
		for i := 0; i < 2; i++ {
			assert.True(t, iter.Prev())
		}
		assert.True(t, iter.Valid())
		assert.Equal(t, []byte("a"), iter.Key())

		// One more Prev should invalidate
		assert.False(t, iter.Prev())
		assert.False(t, iter.Valid())
	})

	t.Run("close idempotency", func(t *testing.T) {
		data := []KV{
			{Key: []byte("key"), Value: []byte("value")},
		}

		iter := newMockIterator(data, false)

		// First close
		err := iter.Close()
		assert.NoError(t, err)

		// Second close should also succeed
		err = iter.Close()
		assert.NoError(t, err)

		// Operations after close should fail gracefully
		assert.False(t, iter.Valid())
		assert.Nil(t, iter.Key())
		assert.Nil(t, iter.Value())
	})
}

// TestIteratorOptions verifies iterator options behavior.
func TestIteratorOptions(t *testing.T) {
	t.Run("nil options", func(t *testing.T) {
		var opts *IteratorOptions
		assert.Nil(t, opts)
		// Should be safe to pass nil options to iterator creation
	})

	t.Run("options with bounds", func(t *testing.T) {
		opts := &IteratorOptions{
			LowerBound: []byte("b"),
			UpperBound: []byte("e"),
			Reverse:    false,
			OnlyKeys:   false,
		}

		assert.Equal(t, []byte("b"), opts.LowerBound)
		assert.Equal(t, []byte("e"), opts.UpperBound)
		assert.False(t, opts.Reverse)
		assert.False(t, opts.OnlyKeys)
	})

	t.Run("reverse iteration", func(t *testing.T) {
		opts := &IteratorOptions{
			Reverse: true,
		}

		assert.True(t, opts.Reverse)
	})

	t.Run("keys only optimization", func(t *testing.T) {
		opts := &IteratorOptions{
			OnlyKeys: true,
		}

		assert.True(t, opts.OnlyKeys)
	})
}

// TestKV verifies the KV struct.
func TestKV(t *testing.T) {
	t.Run("basic kv pair", func(t *testing.T) {
		kv := KV{
			Key:   []byte("test-key"),
			Value: []byte("test-value"),
		}

		assert.Equal(t, []byte("test-key"), kv.Key)
		assert.Equal(t, []byte("test-value"), kv.Value)
	})

	t.Run("empty kv pair", func(t *testing.T) {
		kv := KV{
			Key:   []byte{},
			Value: []byte{},
		}

		assert.Equal(t, []byte{}, kv.Key)
		assert.Equal(t, []byte{}, kv.Value)
	})

	t.Run("nil values", func(t *testing.T) {
		kv := KV{
			Key:   nil,
			Value: nil,
		}

		assert.Nil(t, kv.Key)
		assert.Nil(t, kv.Value)
	})
}

// TestSnapshot_Interface verifies the Snapshot interface contract.
func TestSnapshot_Interface(t *testing.T) {
	t.Run("snapshot methods", func(t *testing.T) {
		// This is a placeholder test - actual snapshot tests are in pebble package
		// This just validates the interface exists
		var _ Snapshot = (*mockSnapshot)(nil)
	})
}

// mockSnapshot is a minimal mock for interface validation.
type mockSnapshot struct {
	data map[string][]byte
}

func (m *mockSnapshot) Get(key []byte) ([]byte, error) {
	if val, ok := m.data[string(key)]; ok {
		return val, nil
	}
	return nil, nil
}

func (m *mockSnapshot) NewIterator(opts *IteratorOptions) Iterator {
	var kvs []KV
	for k, v := range m.data {
		kvs = append(kvs, KV{Key: []byte(k), Value: v})
	}
	return newMockIterator(kvs, false)
}

func (m *mockSnapshot) Close() error {
	return nil
}

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

	t.Run("page size larger than dataset", func(t *testing.T) {
		data := []KV{
			{Key: []byte("x"), Value: []byte("1")},
			{Key: []byte("y"), Value: []byte("2")},
		}

		iter := newMockIterator(data, false)
		pageIter := NewPageIterator(iter, 100, nil)
		defer pageIter.Close()

		page, cursor, err := pageIter.NextPage()
		require.NoError(t, err)
		assert.Len(t, page, 2)
		assert.Nil(t, cursor)
		assert.False(t, pageIter.HasMore())
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

	t.Run("malformed cursor", func(t *testing.T) {
		data := []KV{
			{Key: []byte("a"), Value: []byte("1")},
		}

		iter := newMockIterator(data, false)
		// Invalid base64 cursor
		pageIter := NewPageIterator(iter, 10, []byte("not-valid-base64!@#$"))

		_, _, err := pageIter.NextPage()
		assert.Error(t, err)
		assert.False(t, pageIter.HasMore())
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
