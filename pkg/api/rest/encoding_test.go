//go:build rest

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

package rest

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestEncodePutBinary(t *testing.T) {
	tests := []struct {
		name     string
		key      []byte
		value    []byte
		validate func(*testing.T, []byte)
	}{
		{
			name:  "simple put",
			key:   []byte("users:1"),
			value: []byte("alice"),
			validate: func(t *testing.T, result []byte) {
				// Verify opcode
				assert.Equal(t, opPut, result[0])

				// Verify key length (2 bytes big-endian)
				keyLen := int(result[1])<<8 | int(result[2])
				assert.Equal(t, len("users:1"), keyLen)

				// Verify key
				key := result[3 : 3+keyLen]
				assert.Equal(t, []byte("users:1"), key)

				// Verify value length (4 bytes big-endian)
				valueOffset := 3 + keyLen
				valueLen := int(result[valueOffset])<<24 | int(result[valueOffset+1])<<16 |
					int(result[valueOffset+2])<<8 | int(result[valueOffset+3])
				assert.Equal(t, len("alice"), valueLen)

				// Verify value
				value := result[valueOffset+4:]
				assert.Equal(t, []byte("alice"), value)
			},
		},
		{
			name:  "empty value",
			key:   []byte("key1"),
			value: []byte{},
			validate: func(t *testing.T, result []byte) {
				assert.Equal(t, opPut, result[0])
				keyLen := int(result[1])<<8 | int(result[2])
				assert.Equal(t, 4, keyLen)

				valueOffset := 3 + keyLen
				valueLen := int(result[valueOffset])<<24 | int(result[valueOffset+1])<<16 |
					int(result[valueOffset+2])<<8 | int(result[valueOffset+3])
				assert.Equal(t, 0, valueLen)
			},
		},
		{
			name:  "large key and value",
			key:   make([]byte, 1000),
			value: make([]byte, 10000),
			validate: func(t *testing.T, result []byte) {
				assert.Equal(t, opPut, result[0])
				keyLen := int(result[1])<<8 | int(result[2])
				assert.Equal(t, 1000, keyLen)

				valueOffset := 3 + keyLen
				valueLen := int(result[valueOffset])<<24 | int(result[valueOffset+1])<<16 |
					int(result[valueOffset+2])<<8 | int(result[valueOffset+3])
				assert.Equal(t, 10000, valueLen)

				// Verify total length
				expectedLen := 1 + 2 + 1000 + 4 + 10000
				assert.Equal(t, expectedLen, len(result))
			},
		},
		{
			name:  "binary data",
			key:   []byte{0x00, 0x01, 0x02, 0xFF},
			value: []byte{0xDE, 0xAD, 0xBE, 0xEF},
			validate: func(t *testing.T, result []byte) {
				assert.Equal(t, opPut, result[0])
				keyLen := int(result[1])<<8 | int(result[2])
				assert.Equal(t, 4, keyLen)

				key := result[3:7]
				assert.Equal(t, []byte{0x00, 0x01, 0x02, 0xFF}, key)

				value := result[11:]
				assert.Equal(t, []byte{0xDE, 0xAD, 0xBE, 0xEF}, value)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := encodePutBinary(tt.key, tt.value)
			tt.validate(t, result)
		})
	}
}

func TestEncodeDeleteBinary(t *testing.T) {
	tests := []struct {
		name     string
		key      []byte
		validate func(*testing.T, []byte)
	}{
		{
			name: "simple delete",
			key:  []byte("users:1"),
			validate: func(t *testing.T, result []byte) {
				// Verify opcode
				assert.Equal(t, opDelete, result[0])

				// Verify key length (2 bytes big-endian)
				keyLen := int(result[1])<<8 | int(result[2])
				assert.Equal(t, len("users:1"), keyLen)

				// Verify key
				key := result[3:]
				assert.Equal(t, []byte("users:1"), key)

				// Verify total length
				expectedLen := 1 + 2 + len("users:1")
				assert.Equal(t, expectedLen, len(result))
			},
		},
		{
			name: "single byte key",
			key:  []byte("k"),
			validate: func(t *testing.T, result []byte) {
				assert.Equal(t, opDelete, result[0])
				keyLen := int(result[1])<<8 | int(result[2])
				assert.Equal(t, 1, keyLen)
				assert.Equal(t, []byte("k"), result[3:])
			},
		},
		{
			name: "large key",
			key:  make([]byte, 5000),
			validate: func(t *testing.T, result []byte) {
				assert.Equal(t, opDelete, result[0])
				keyLen := int(result[1])<<8 | int(result[2])
				assert.Equal(t, 5000, keyLen)

				expectedLen := 1 + 2 + 5000
				assert.Equal(t, expectedLen, len(result))
			},
		},
		{
			name: "binary key",
			key:  []byte{0x00, 0xFF, 0xAA, 0x55},
			validate: func(t *testing.T, result []byte) {
				assert.Equal(t, opDelete, result[0])
				keyLen := int(result[1])<<8 | int(result[2])
				assert.Equal(t, 4, keyLen)
				assert.Equal(t, []byte{0x00, 0xFF, 0xAA, 0x55}, result[3:])
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := encodeDeleteBinary(tt.key)
			tt.validate(t, result)
		})
	}
}

func TestEncodeBatchBinary(t *testing.T) {
	tests := []struct {
		name       string
		operations [][]byte
		validate   func(*testing.T, []byte)
	}{
		{
			name:       "empty batch",
			operations: [][]byte{},
			validate: func(t *testing.T, result []byte) {
				assert.Equal(t, opBatch, result[0])
				assert.Equal(t, 1, len(result))
			},
		},
		{
			name: "single operation",
			operations: [][]byte{
				encodePutBinary([]byte("key1"), []byte("value1")),
			},
			validate: func(t *testing.T, result []byte) {
				assert.Equal(t, opBatch, result[0])

				// First operation should be a PUT
				assert.Equal(t, opPut, result[1])
			},
		},
		{
			name: "multiple operations",
			operations: [][]byte{
				encodePutBinary([]byte("key1"), []byte("value1")),
				encodeDeleteBinary([]byte("key2")),
				encodePutBinary([]byte("key3"), []byte("value3")),
			},
			validate: func(t *testing.T, result []byte) {
				assert.Equal(t, opBatch, result[0])

				// Calculate expected size
				expectedSize := 1 // batch opcode
				for _, op := range [][]byte{
					encodePutBinary([]byte("key1"), []byte("value1")),
					encodeDeleteBinary([]byte("key2")),
					encodePutBinary([]byte("key3"), []byte("value3")),
				} {
					expectedSize += len(op)
				}
				assert.Equal(t, expectedSize, len(result))
			},
		},
		{
			name: "mixed operations",
			operations: [][]byte{
				encodePutBinary([]byte("users:1"), []byte(`{"name":"Alice"}`)),
				encodePutBinary([]byte("users:2"), []byte(`{"name":"Bob"}`)),
				encodeDeleteBinary([]byte("users:3")),
			},
			validate: func(t *testing.T, result []byte) {
				assert.Equal(t, opBatch, result[0])

				offset := 1
				// First PUT
				assert.Equal(t, opPut, result[offset])

				// Skip to verify structure integrity
				putOp1 := encodePutBinary([]byte("users:1"), []byte(`{"name":"Alice"}`))
				offset += len(putOp1)

				// Second PUT
				assert.Equal(t, opPut, result[offset])

				putOp2 := encodePutBinary([]byte("users:2"), []byte(`{"name":"Bob"}`))
				offset += len(putOp2)

				// DELETE
				assert.Equal(t, opDelete, result[offset])
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := encodeBatchBinary(tt.operations)
			tt.validate(t, result)
		})
	}
}

func TestEncodingRoundTrip(t *testing.T) {
	// Test that encoding produces consistent results
	key := []byte("test:key:123")
	value := []byte("test value with special chars: !@#")

	// Encode PUT
	encoded := encodePutBinary(key, value)

	// Verify we can extract the opcode
	assert.Equal(t, opPut, encoded[0])

	// Verify key length
	keyLen := int(encoded[1])<<8 | int(encoded[2])
	assert.Equal(t, len(key), keyLen)

	// Extract and verify key
	extractedKey := encoded[3 : 3+keyLen]
	assert.Equal(t, key, extractedKey)

	// Verify value length
	valueOffset := 3 + keyLen
	valueLen := int(encoded[valueOffset])<<24 | int(encoded[valueOffset+1])<<16 |
		int(encoded[valueOffset+2])<<8 | int(encoded[valueOffset+3])
	assert.Equal(t, len(value), valueLen)

	// Extract and verify value
	extractedValue := encoded[valueOffset+4:]
	assert.Equal(t, value, extractedValue)
}

func TestEncodingOpcodes(t *testing.T) {
	// Verify opcodes are distinct
	assert.NotEqual(t, opPut, opDelete)
	assert.NotEqual(t, opPut, opBatch)
	assert.NotEqual(t, opDelete, opBatch)

	// Verify opcode values
	assert.Equal(t, byte(0x01), opPut)
	assert.Equal(t, byte(0x02), opDelete)
	assert.Equal(t, byte(0x03), opBatch)
}
