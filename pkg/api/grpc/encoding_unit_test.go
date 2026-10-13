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
	"testing"

	"github.com/stretchr/testify/assert"
)

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
