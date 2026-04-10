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
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mockIterator implements Iterator interface for testing.
type mockIterator struct {
	data     []KV
	pos      int
	valid    bool
	err      error
	reversed bool
}

func newMockIterator(data []KV, reversed bool) *mockIterator {
	return &mockIterator{
		data:     data,
		pos:      -1,
		valid:    false,
		reversed: reversed,
	}
}

func (m *mockIterator) Valid() bool {
	return m.valid && m.pos >= 0 && m.pos < len(m.data)
}

func (m *mockIterator) Next() bool {
	if m.reversed {
		return m.Prev()
	}
	if m.pos < len(m.data)-1 {
		m.pos++
		m.valid = true
		return true
	}
	m.valid = false
	return false
}

func (m *mockIterator) Prev() bool {
	if m.reversed {
		if m.pos < len(m.data)-1 {
			m.pos++
			m.valid = true
			return true
		}
		m.valid = false
		return false
	}
	if m.pos > 0 {
		m.pos--
		m.valid = true
		return true
	}
	m.valid = false
	return false
}

func (m *mockIterator) Seek(key []byte) bool {
	for i, kv := range m.data {
		if string(kv.Key) >= string(key) {
			m.pos = i
			m.valid = true
			return true
		}
	}
	m.valid = false
	return false
}

func (m *mockIterator) SeekToFirst() bool {
	if len(m.data) > 0 {
		m.pos = 0
		m.valid = true
		return true
	}
	m.valid = false
	return false
}

func (m *mockIterator) SeekToLast() bool {
	if len(m.data) > 0 {
		m.pos = len(m.data) - 1
		m.valid = true
		return true
	}
	m.valid = false
	return false
}

func (m *mockIterator) Key() []byte {
	if m.Valid() {
		return m.data[m.pos].Key
	}
	return nil
}

func (m *mockIterator) Value() []byte {
	if m.Valid() {
		return m.data[m.pos].Value
	}
	return nil
}

func (m *mockIterator) Error() error {
	return m.err
}

func (m *mockIterator) Close() error {
	m.valid = false
	m.pos = -1
	return nil
}

func TestPageIterator_EmptyIterator(t *testing.T) {
	iter := newMockIterator([]KV{}, false)
	pageIter := NewPageIterator(iter, 10, nil)

	page, cursor, err := pageIter.NextPage()
	require.NoError(t, err)
	assert.Nil(t, page)
	assert.Nil(t, cursor)
	assert.False(t, pageIter.HasMore())
}

func TestPageIterator_SinglePage(t *testing.T) {
	data := []KV{
		{Key: []byte("key1"), Value: []byte("value1")},
		{Key: []byte("key2"), Value: []byte("value2")},
		{Key: []byte("key3"), Value: []byte("value3")},
	}

	iter := newMockIterator(data, false)
	pageIter := NewPageIterator(iter, 10, nil)

	// First page should contain all data
	page, cursor, err := pageIter.NextPage()
	require.NoError(t, err)
	assert.Len(t, page, 3)
	assert.Nil(t, cursor)
	assert.False(t, pageIter.HasMore())

	// Verify data
	for i, kv := range page {
		assert.Equal(t, data[i].Key, kv.Key)
		assert.Equal(t, data[i].Value, kv.Value)
	}
}

func TestPageIterator_MultiplePages(t *testing.T) {
	data := []KV{
		{Key: []byte("key1"), Value: []byte("value1")},
		{Key: []byte("key2"), Value: []byte("value2")},
		{Key: []byte("key3"), Value: []byte("value3")},
		{Key: []byte("key4"), Value: []byte("value4")},
		{Key: []byte("key5"), Value: []byte("value5")},
	}

	iter := newMockIterator(data, false)
	pageIter := NewPageIterator(iter, 2, nil)

	// First page
	page1, cursor1, err := pageIter.NextPage()
	require.NoError(t, err)
	assert.Len(t, page1, 2)
	assert.NotNil(t, cursor1)
	assert.True(t, pageIter.HasMore())
	assert.Equal(t, []byte("key1"), page1[0].Key)
	assert.Equal(t, []byte("key2"), page1[1].Key)

	// Second page - need new iterator positioned at cursor
	iter2 := newMockIterator(data, false)
	pageIter2 := NewPageIterator(iter2, 2, cursor1)

	page2, cursor2, err := pageIter2.NextPage()
	require.NoError(t, err)
	assert.Len(t, page2, 2)
	assert.NotNil(t, cursor2)
	assert.Equal(t, []byte("key3"), page2[0].Key)
	assert.Equal(t, []byte("key4"), page2[1].Key)

	// Third page
	iter3 := newMockIterator(data, false)
	pageIter3 := NewPageIterator(iter3, 2, cursor2)

	page3, cursor3, err := pageIter3.NextPage()
	require.NoError(t, err)
	assert.Len(t, page3, 1)
	assert.Nil(t, cursor3)
	assert.Equal(t, []byte("key5"), page3[0].Key)
}

func TestPageIterator_WithCursor(t *testing.T) {
	data := []KV{
		{Key: []byte("key1"), Value: []byte("value1")},
		{Key: []byte("key2"), Value: []byte("value2")},
		{Key: []byte("key3"), Value: []byte("value3")},
		{Key: []byte("key4"), Value: []byte("value4")},
	}

	// Create cursor for key2
	cursor := encodeCursor([]byte("key2"))

	iter := newMockIterator(data, false)
	pageIter := NewPageIterator(iter, 2, cursor)

	page, nextCursor, err := pageIter.NextPage()
	require.NoError(t, err)
	assert.Len(t, page, 2)
	assert.NotNil(t, nextCursor)

	// Should start from key2 (inclusive in seek)
	// The mock Seek will position at key2
	assert.Equal(t, []byte("key2"), page[0].Key)
	assert.Equal(t, []byte("key3"), page[1].Key)
}

func TestPageIterator_InvalidCursor(t *testing.T) {
	data := []KV{
		{Key: []byte("key1"), Value: []byte("value1")},
	}

	iter := newMockIterator(data, false)
	pageIter := NewPageIterator(iter, 10, []byte("invalid-base64!!!"))

	_, _, err := pageIter.NextPage()
	require.Error(t, err)
	assert.False(t, pageIter.HasMore())
}

func TestPageIterator_DefaultPageSize(t *testing.T) {
	data := make([]KV, 150)
	for i := 0; i < 150; i++ {
		data[i] = KV{
			Key:   []byte{byte(i)},
			Value: []byte{byte(i)},
		}
	}

	iter := newMockIterator(data, false)
	pageIter := NewPageIterator(iter, 0, nil) // 0 should default to 100

	page, cursor, err := pageIter.NextPage()
	require.NoError(t, err)
	assert.Len(t, page, 100)
	assert.NotNil(t, cursor)
	assert.True(t, pageIter.HasMore())
}

func TestPageIterator_Close(t *testing.T) {
	data := []KV{
		{Key: []byte("key1"), Value: []byte("value1")},
	}

	iter := newMockIterator(data, false)
	pageIter := NewPageIterator(iter, 10, nil)

	err := pageIter.Close()
	require.NoError(t, err)

	// Iterator should be closed
	assert.False(t, iter.Valid())
}

func TestCursorEncoding(t *testing.T) {
	tests := []struct {
		name string
		key  []byte
	}{
		{
			name: "simple key",
			key:  []byte("test-key"),
		},
		{
			name: "binary key",
			key:  []byte{0x01, 0x02, 0x03, 0xFF},
		},
		{
			name: "empty key",
			key:  []byte{},
		},
		{
			name: "nil key",
			key:  nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			encoded := encodeCursor(tt.key)
			if tt.key == nil || len(tt.key) == 0 {
				assert.Nil(t, encoded)
				return
			}

			decoded, err := decodeCursor(encoded)
			require.NoError(t, err)
			assert.Equal(t, tt.key, decoded)
		})
	}
}

func TestCursorDecoding_Invalid(t *testing.T) {
	tests := []struct {
		name   string
		cursor []byte
	}{
		{
			name:   "invalid base64",
			cursor: []byte("not-valid-base64!!!"),
		},
		{
			name:   "empty cursor",
			cursor: []byte{},
		},
		{
			name:   "nil cursor",
			cursor: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			decoded, err := decodeCursor(tt.cursor)
			if tt.cursor == nil || len(tt.cursor) == 0 {
				assert.NoError(t, err)
				assert.Nil(t, decoded)
			} else {
				assert.Error(t, err)
			}
		})
	}
}

func TestCloneBytes(t *testing.T) {
	tests := []struct {
		name  string
		input []byte
		want  []byte
	}{
		{
			name:  "normal bytes",
			input: []byte("test"),
			want:  []byte("test"),
		},
		{
			name:  "empty bytes",
			input: []byte{},
			want:  []byte{},
		},
		{
			name:  "nil bytes",
			input: nil,
			want:  nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			clone := cloneBytes(tt.input)
			assert.Equal(t, tt.want, clone)

			// Verify it's a different slice (unless nil)
			if tt.input != nil && len(tt.input) > 0 {
				// Modify original, clone should be unchanged
				original := make([]byte, len(tt.input))
				copy(original, tt.input)
				tt.input[0] = 'X'
				assert.NotEqual(t, tt.input, clone)
				assert.Equal(t, original, clone)
			}
		})
	}
}

func TestPageIterator_BoundsChecking(t *testing.T) {
	data := []KV{
		{Key: []byte("a"), Value: []byte("value-a")},
		{Key: []byte("b"), Value: []byte("value-b")},
		{Key: []byte("c"), Value: []byte("value-c")},
		{Key: []byte("d"), Value: []byte("value-d")},
		{Key: []byte("e"), Value: []byte("value-e")},
	}

	iter := newMockIterator(data, false)
	pageIter := NewPageIterator(iter, 2, nil)

	// Get first page
	page1, cursor1, err := pageIter.NextPage()
	require.NoError(t, err)
	assert.Len(t, page1, 2)
	assert.NotNil(t, cursor1)

	// Decode cursor to verify it's correct
	decodedKey, err := decodeCursor(cursor1)
	require.NoError(t, err)
	assert.Equal(t, []byte("c"), decodedKey) // Should be at position after last item
}

func TestSortOrder_String(t *testing.T) {
	tests := []struct {
		order SortOrder
		want  string
	}{
		{Ascending, "ascending"},
		{Descending, "descending"},
		{SortOrder(999), "unknown"},
	}

	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			assert.Equal(t, tt.want, tt.order.String())
		})
	}
}

func TestPageIterator_ExactPageBoundary(t *testing.T) {
	// Test when data size is exact multiple of page size
	data := []KV{
		{Key: []byte("key1"), Value: []byte("value1")},
		{Key: []byte("key2"), Value: []byte("value2")},
		{Key: []byte("key3"), Value: []byte("value3")},
		{Key: []byte("key4"), Value: []byte("value4")},
	}

	iter := newMockIterator(data, false)
	pageIter := NewPageIterator(iter, 2, nil)

	// First page
	page1, cursor1, err := pageIter.NextPage()
	require.NoError(t, err)
	assert.Len(t, page1, 2)
	assert.NotNil(t, cursor1)

	// Second page - new iterator
	iter2 := newMockIterator(data, false)
	pageIter2 := NewPageIterator(iter2, 2, cursor1)

	page2, cursor2, err := pageIter2.NextPage()
	require.NoError(t, err)
	assert.Len(t, page2, 2)
	assert.Nil(t, cursor2) // Should be nil since we're at the end
}

func TestEncodeCursor_Roundtrip(t *testing.T) {
	key := []byte("test-key-123")
	cursor := encodeCursor(key)

	// Verify it's valid base64
	_, err := base64.StdEncoding.DecodeString(string(cursor))
	require.NoError(t, err)

	// Decode and verify
	decoded, err := decodeCursor(cursor)
	require.NoError(t, err)
	assert.Equal(t, key, decoded)
}
