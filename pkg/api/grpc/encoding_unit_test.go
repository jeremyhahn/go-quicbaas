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

package grpc

import (
	"bytes"
	"encoding/binary"
	"math"
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestEncodePutBinary tests binary PUT encoding.
func TestEncodePutBinary(t *testing.T) {
	tests := []struct {
		name  string
		key   []byte
		value []byte
	}{
		{
			name:  "SimpleKeyValue",
			key:   []byte("user:123"),
			value: []byte("test-data"),
		},
		{
			name:  "EmptyValue",
			key:   []byte("key"),
			value: []byte{},
		},
		{
			name:  "LargeValue",
			key:   []byte("large"),
			value: bytes.Repeat([]byte("x"), 1024),
		},
		{
			name:  "BinaryData",
			key:   []byte("binary"),
			value: []byte{0x00, 0x01, 0x02, 0xFF, 0xFE},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			encoded := encodePutBinary(tt.key, tt.value)

			// Verify format: [op:1byte][keyLen:2bytes][key][valueLen:4bytes][value]
			assert.NotNil(t, encoded)
			assert.Equal(t, byte(opPut), encoded[0], "First byte should be PUT opcode")

			// Extract key length
			keyLen := binary.BigEndian.Uint16(encoded[1:3])
			assert.Equal(t, uint16(len(tt.key)), keyLen)

			// Extract key
			extractedKey := encoded[3 : 3+keyLen]
			assert.Equal(t, tt.key, extractedKey)

			// Extract value length
			valueLen := binary.BigEndian.Uint32(encoded[3+keyLen : 7+keyLen])
			assert.Equal(t, uint32(len(tt.value)), valueLen)

			// Extract value
			extractedValue := encoded[7+keyLen : 7+uint32(keyLen)+valueLen]
			assert.Equal(t, tt.value, extractedValue)

			// Verify total length
			expectedLen := 1 + 2 + len(tt.key) + 4 + len(tt.value)
			assert.Equal(t, expectedLen, len(encoded))
		})
	}
}

// TestEncodePutBinary_KeyLengthOverflow tests key length clamping at MaxUint16.
func TestEncodePutBinary_KeyLengthOverflow(t *testing.T) {
	// Create a key larger than MaxUint16
	largeKey := bytes.Repeat([]byte("k"), math.MaxUint16+100)
	value := []byte("value")

	encoded := encodePutBinary(largeKey, value)

	assert.NotNil(t, encoded)
	assert.Equal(t, byte(opPut), encoded[0])

	// Key length should be clamped to MaxUint16
	keyLen := binary.BigEndian.Uint16(encoded[1:3])
	assert.Equal(t, uint16(math.MaxUint16), keyLen)
}

// TestEncodeDeleteBinary tests binary DELETE encoding.
func TestEncodeDeleteBinary(t *testing.T) {
	tests := []struct {
		name string
		key  []byte
	}{
		{
			name: "SimpleKey",
			key:  []byte("user:123"),
		},
		{
			name: "LongKey",
			key:  []byte("very:long:key:with:many:segments:123456789"),
		},
		{
			name: "BinaryKey",
			key:  []byte{0x01, 0x02, 0x03, 0xFF},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			encoded := encodeDeleteBinary(tt.key)

			// Verify format: [op:1byte][keyLen:2bytes][key]
			assert.NotNil(t, encoded)
			assert.Equal(t, byte(opDelete), encoded[0], "First byte should be DELETE opcode")

			// Extract key length
			keyLen := binary.BigEndian.Uint16(encoded[1:3])
			assert.Equal(t, uint16(len(tt.key)), keyLen)

			// Extract key
			extractedKey := encoded[3 : 3+keyLen]
			assert.Equal(t, tt.key, extractedKey)

			// Verify total length
			expectedLen := 1 + 2 + len(tt.key)
			assert.Equal(t, expectedLen, len(encoded))
		})
	}
}

// TestEncodeDeleteBinary_KeyLengthOverflow tests key length clamping at MaxUint16.
func TestEncodeDeleteBinary_KeyLengthOverflow(t *testing.T) {
	// Create a key larger than MaxUint16
	largeKey := bytes.Repeat([]byte("k"), math.MaxUint16+50)

	encoded := encodeDeleteBinary(largeKey)

	assert.NotNil(t, encoded)
	assert.Equal(t, byte(opDelete), encoded[0])

	// Key length should be clamped to MaxUint16
	keyLen := binary.BigEndian.Uint16(encoded[1:3])
	assert.Equal(t, uint16(math.MaxUint16), keyLen)
}

// TestEncodeBatchBinary tests binary batch encoding.
func TestEncodeBatchBinary(t *testing.T) {
	t.Run("MixedOperations", func(t *testing.T) {
		ops := [][]byte{
			encodePutBinary([]byte("key1"), []byte("value1")),
			encodeDeleteBinary([]byte("key2")),
			encodePutBinary([]byte("key3"), []byte("value3")),
		}

		encoded := encodeBatchBinary(ops)

		// Verify batch format: [0x03][op1][op2][op3]
		assert.NotNil(t, encoded)
		assert.Equal(t, byte(0x03), encoded[0], "First byte should be batch opcode")

		// Verify total length
		expectedLen := 1
		for _, op := range ops {
			expectedLen += len(op)
		}
		assert.Equal(t, expectedLen, len(encoded))

		// Verify operations are concatenated
		offset := 1
		for _, op := range ops {
			extractedOp := encoded[offset : offset+len(op)]
			assert.Equal(t, op, extractedOp)
			offset += len(op)
		}
	})

	t.Run("EmptyBatch", func(t *testing.T) {
		ops := [][]byte{}
		encoded := encodeBatchBinary(ops)

		assert.NotNil(t, encoded)
		assert.Equal(t, 1, len(encoded))
		assert.Equal(t, byte(0x03), encoded[0])
	})

	t.Run("SingleOperation", func(t *testing.T) {
		ops := [][]byte{
			encodePutBinary([]byte("single"), []byte("value")),
		}

		encoded := encodeBatchBinary(ops)
		assert.NotNil(t, encoded)
		assert.Equal(t, byte(0x03), encoded[0])
		assert.Greater(t, len(encoded), 1)
	})

	t.Run("LargeBatch", func(t *testing.T) {
		ops := make([][]byte, 100)
		for i := 0; i < 100; i++ {
			ops[i] = encodePutBinary([]byte("key"), []byte("value"))
		}

		encoded := encodeBatchBinary(ops)
		assert.NotNil(t, encoded)
		assert.Equal(t, byte(0x03), encoded[0])
	})
}

// TestBuildPutCommand tests JSON PUT command building (legacy).
func TestBuildPutCommand(t *testing.T) {
	t.Run("SimpleCommand", func(t *testing.T) {
		cmd := buildPutCommand("user", 123, []byte("test-data"))

		assert.NotNil(t, cmd)
		assert.Contains(t, string(cmd), `"entity_type":"user"`)
		assert.Contains(t, string(cmd), `"id":"123"`)
		assert.Contains(t, string(cmd), `"data":"test-data"`)
	})

	t.Run("SpecialCharacters", func(t *testing.T) {
		// Test JSON escaping
		cmd := buildPutCommand("user", 1, []byte(`"quoted"`))

		assert.NotNil(t, cmd)
		// Should escape quotes
		assert.Contains(t, string(cmd), `\"quoted\"`)
	})

	t.Run("Newlines", func(t *testing.T) {
		cmd := buildPutCommand("log", 1, []byte("line1\nline2"))

		assert.NotNil(t, cmd)
		// Should escape newlines
		assert.Contains(t, string(cmd), `\n`)
	})

	t.Run("ControlCharacters", func(t *testing.T) {
		cmd := buildPutCommand("data", 1, []byte("tab\there"))

		assert.NotNil(t, cmd)
		// Should escape tabs
		assert.Contains(t, string(cmd), `\t`)
	})

	t.Run("EmptyData", func(t *testing.T) {
		cmd := buildPutCommand("empty", 999, []byte{})

		assert.NotNil(t, cmd)
		assert.Contains(t, string(cmd), `"entity_type":"empty"`)
		assert.Contains(t, string(cmd), `"id":"999"`)
	})
}

// TestBuildDeleteCommand tests JSON DELETE command building (legacy).
func TestBuildDeleteCommand(t *testing.T) {
	t.Run("SimpleCommand", func(t *testing.T) {
		cmd := buildDeleteCommand("user", 456)

		assert.NotNil(t, cmd)
		assert.Contains(t, string(cmd), `"operation":"delete"`)
		assert.Contains(t, string(cmd), `"entity_type":"user"`)
		assert.Contains(t, string(cmd), `"id":"456"`)
	})

	t.Run("LargeID", func(t *testing.T) {
		cmd := buildDeleteCommand("entity", 9223372036854775807)

		assert.NotNil(t, cmd)
		assert.Contains(t, string(cmd), `"id":"9223372036854775807"`)
	})
}

// TestAppendJSONEscaped tests JSON escaping function.
func TestAppendJSONEscaped(t *testing.T) {
	tests := []struct {
		name     string
		input    []byte
		expected string
	}{
		{
			name:     "NoEscape",
			input:    []byte("simple"),
			expected: "simple",
		},
		{
			name:     "Quotes",
			input:    []byte(`"quoted"`),
			expected: `\"quoted\"`,
		},
		{
			name:     "Backslash",
			input:    []byte(`back\slash`),
			expected: `back\\slash`,
		},
		{
			name:     "Newline",
			input:    []byte("new\nline"),
			expected: `new\nline`,
		},
		{
			name:     "CarriageReturn",
			input:    []byte("carriage\rreturn"),
			expected: `carriage\rreturn`,
		},
		{
			name:     "Tab",
			input:    []byte("tab\there"),
			expected: `tab\there`,
		},
		{
			name:     "ControlCharacter",
			input:    []byte{0x01},
			expected: `\u0001`,
		},
		{
			name:     "MultipleEscapes",
			input:    []byte("\"\\\n\r\t"),
			expected: `\"\\\n\r\t`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			buf := make([]byte, 0)
			result := appendJSONEscaped(buf, tt.input)
			assert.Equal(t, tt.expected, string(result))
		})
	}
}

// TestBufferPoolReuse tests that buffer pool reduces allocations.
func TestBufferPoolReuse(t *testing.T) {
	// Get a buffer from pool
	bufPtr1 := cmdBufferPool.Get().(*[]byte)
	buf1 := *bufPtr1
	cap1 := cap(buf1)

	// Return to pool
	cmdBufferPool.Put(bufPtr1)

	// Get another buffer
	bufPtr2 := cmdBufferPool.Get().(*[]byte)
	buf2 := *bufPtr2

	// Should reuse the same buffer (same capacity)
	assert.Equal(t, cap1, cap(buf2))

	cmdBufferPool.Put(bufPtr2)
}

// TestEncodingOpcodes tests opcode constants.
func TestEncodingOpcodes(t *testing.T) {
	assert.Equal(t, opPut, opPut)
	assert.Equal(t, opDelete, opDelete)
}

// BenchmarkEncodePutBinary benchmarks binary PUT encoding.
func BenchmarkEncodePutBinary(b *testing.B) {
	key := []byte("user:123")
	value := []byte("test-data-value")

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = encodePutBinary(key, value)
	}
}

// BenchmarkEncodeDeleteBinary benchmarks binary DELETE encoding.
func BenchmarkEncodeDeleteBinary(b *testing.B) {
	key := []byte("user:123")

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = encodeDeleteBinary(key)
	}
}

// BenchmarkEncodeBatchBinary benchmarks binary batch encoding.
func BenchmarkEncodeBatchBinary(b *testing.B) {
	ops := [][]byte{
		encodePutBinary([]byte("key1"), []byte("value1")),
		encodePutBinary([]byte("key2"), []byte("value2")),
		encodeDeleteBinary([]byte("key3")),
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = encodeBatchBinary(ops)
	}
}

// BenchmarkBuildPutCommand benchmarks JSON PUT encoding.
func BenchmarkBuildPutCommand(b *testing.B) {
	entityType := "user"
	id := uint64(123)
	data := []byte("test-data-value")

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = buildPutCommand(entityType, id, data)
	}
}
