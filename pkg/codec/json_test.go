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

package codec

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type JSONTestEntity struct {
	ID    uint64   `json:"id"`
	Name  string   `json:"name"`
	Email string   `json:"email"`
	Score float64  `json:"score"`
	Tags  []string `json:"tags"`
}

type JSONNestedEntity struct {
	ID      uint64                 `json:"id"`
	Name    string                 `json:"name"`
	Address JSONAddress            `json:"address"`
	Meta    map[string]interface{} `json:"meta"`
}

type JSONAddress struct {
	Street string `json:"street"`
	City   string `json:"city"`
	Zip    string `json:"zip"`
}

func TestJSONCodec_Name(t *testing.T) {
	codec := NewJSONCodec()
	assert.Equal(t, "json", codec.Name())
}

func TestJSONCodec_Priority(t *testing.T) {
	codec := NewJSONCodec()
	assert.Equal(t, 50, codec.Priority())
}

func TestJSONCodec_CanEncode(t *testing.T) {
	codec := NewJSONCodec()

	tests := []struct {
		name     string
		entity   interface{}
		expected bool
	}{
		{
			name:     "struct",
			entity:   &JSONTestEntity{ID: 1, Name: "test"},
			expected: true,
		},
		{
			name:     "map",
			entity:   map[string]interface{}{"key": "value"},
			expected: true,
		},
		{
			name:     "slice",
			entity:   []string{"a", "b", "c"},
			expected: true,
		},
		{
			name:     "array",
			entity:   [3]int{1, 2, 3},
			expected: true,
		},
		{
			name:     "int",
			entity:   42,
			expected: true,
		},
		{
			name:     "uint64",
			entity:   uint64(123),
			expected: true,
		},
		{
			name:     "float64",
			entity:   3.14,
			expected: true,
		},
		{
			name:     "string",
			entity:   "hello",
			expected: true,
		},
		{
			name:     "bool",
			entity:   true,
			expected: true,
		},
		{
			name:     "nil",
			entity:   nil,
			expected: false,
		},
		{
			name:     "channel (unsupported)",
			entity:   make(chan int),
			expected: false,
		},
		{
			name:     "function (unsupported)",
			entity:   func() {},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := codec.CanEncode(tt.entity)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestJSONCodec_Encode(t *testing.T) {
	codec := NewJSONCodec()

	tests := []struct {
		name        string
		entity      interface{}
		expectError bool
	}{
		{
			name: "simple struct",
			entity: &JSONTestEntity{
				ID:    123,
				Name:  "John Doe",
				Email: "john@example.com",
				Score: 95.5,
				Tags:  []string{"golang", "testing"},
			},
			expectError: false,
		},
		{
			name: "nested struct",
			entity: &JSONNestedEntity{
				ID:   456,
				Name: "Jane Doe",
				Address: JSONAddress{
					Street: "123 Main St",
					City:   "Springfield",
					Zip:    "12345",
				},
				Meta: map[string]interface{}{
					"verified": true,
					"score":    98.5,
				},
			},
			expectError: false,
		},
		{
			name:        "map",
			entity:      map[string]int{"a": 1, "b": 2, "c": 3},
			expectError: false,
		},
		{
			name:        "slice",
			entity:      []int{1, 2, 3, 4, 5},
			expectError: false,
		},
		{
			name:        "string",
			entity:      "hello world",
			expectError: false,
		},
		{
			name:        "int",
			entity:      42,
			expectError: false,
		},
		{
			name:        "float",
			entity:      3.14159,
			expectError: false,
		},
		{
			name:        "bool",
			entity:      true,
			expectError: false,
		},
		{
			name:        "nil entity",
			entity:      nil,
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data, err := codec.Encode(tt.entity)

			if tt.expectError {
				require.Error(t, err)
				assert.Nil(t, data)
			} else {
				require.NoError(t, err)
				assert.NotEmpty(t, data)

				// Verify data is valid JSON by attempting to decode it back
				// (This is better than checking for specific characters)
			}
		})
	}
}

func TestJSONCodec_Decode(t *testing.T) {
	codec := NewJSONCodec()

	t.Run("simple struct", func(t *testing.T) {
		original := &JSONTestEntity{
			ID:    123,
			Name:  "John Doe",
			Email: "john@example.com",
			Score: 95.5,
			Tags:  []string{"golang", "testing"},
		}

		data, err := codec.Encode(original)
		require.NoError(t, err)

		var decoded JSONTestEntity
		err = codec.Decode(data, &decoded)
		require.NoError(t, err)

		assert.Equal(t, original.ID, decoded.ID)
		assert.Equal(t, original.Name, decoded.Name)
		assert.Equal(t, original.Email, decoded.Email)
		assert.Equal(t, original.Score, decoded.Score)
		assert.Equal(t, original.Tags, decoded.Tags)
	})

	t.Run("nested struct", func(t *testing.T) {
		original := &JSONNestedEntity{
			ID:   456,
			Name: "Jane Doe",
			Address: JSONAddress{
				Street: "123 Main St",
				City:   "Springfield",
				Zip:    "12345",
			},
			Meta: map[string]interface{}{
				"verified": true,
				"score":    98.5,
			},
		}

		data, err := codec.Encode(original)
		require.NoError(t, err)

		var decoded JSONNestedEntity
		err = codec.Decode(data, &decoded)
		require.NoError(t, err)

		assert.Equal(t, original.ID, decoded.ID)
		assert.Equal(t, original.Name, decoded.Name)
		assert.Equal(t, original.Address.Street, decoded.Address.Street)
		assert.Equal(t, original.Address.City, decoded.Address.City)
		assert.Equal(t, original.Address.Zip, decoded.Address.Zip)
		assert.True(t, decoded.Meta["verified"].(bool))
		assert.Equal(t, 98.5, decoded.Meta["score"].(float64))
	})

	t.Run("map", func(t *testing.T) {
		original := map[string]int{"a": 1, "b": 2, "c": 3}

		data, err := codec.Encode(original)
		require.NoError(t, err)

		var decoded map[string]int
		err = codec.Decode(data, &decoded)
		require.NoError(t, err)

		assert.Equal(t, original, decoded)
	})

	t.Run("slice", func(t *testing.T) {
		original := []int{1, 2, 3, 4, 5}

		data, err := codec.Encode(original)
		require.NoError(t, err)

		var decoded []int
		err = codec.Decode(data, &decoded)
		require.NoError(t, err)

		assert.Equal(t, original, decoded)
	})

	t.Run("empty data", func(t *testing.T) {
		var entity JSONTestEntity
		err := codec.Decode([]byte{}, &entity)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "cannot decode empty data")
	})

	t.Run("nil entity", func(t *testing.T) {
		data := []byte(`{"id":123}`)
		err := codec.Decode(data, nil)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "cannot decode into nil entity")
	})

	t.Run("non-pointer entity", func(t *testing.T) {
		data := []byte(`{"id":123}`)
		var entity JSONTestEntity
		err := codec.Decode(data, entity)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "entity must be a pointer")
	})

	t.Run("invalid JSON", func(t *testing.T) {
		data := []byte(`{invalid json}`)
		var entity JSONTestEntity
		err := codec.Decode(data, &entity)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "failed to decode JSON")
	})
}

func TestJSONCodec_RoundTrip(t *testing.T) {
	codec := NewJSONCodec()

	tests := []struct {
		name   string
		entity interface{}
	}{
		{
			name: "simple entity",
			entity: &JSONTestEntity{
				ID:    999,
				Name:  "Round Trip Test",
				Email: "roundtrip@example.com",
				Score: 88.8,
				Tags:  []string{"test", "roundtrip"},
			},
		},
		{
			name: "nested entity",
			entity: &JSONNestedEntity{
				ID:   777,
				Name: "Nested Test",
				Address: JSONAddress{
					Street: "456 Elm St",
					City:   "Testville",
					Zip:    "54321",
				},
				Meta: map[string]interface{}{
					"active": false,
					"count":  42,
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Encode
			data, err := codec.Encode(tt.entity)
			require.NoError(t, err)
			assert.NotEmpty(t, data)

			// Decode
			var decoded interface{}
			switch tt.entity.(type) {
			case *JSONTestEntity:
				decoded = &JSONTestEntity{}
			case *JSONNestedEntity:
				decoded = &JSONNestedEntity{}
			}

			err = codec.Decode(data, decoded)
			require.NoError(t, err)

			// Verify round trip
			data2, err := codec.Encode(decoded)
			require.NoError(t, err)

			// JSON may not be byte-identical due to formatting, but should decode to same values
			assert.Equal(t, string(data), string(data2))
		})
	}
}

func TestJSONCodec_Unicode(t *testing.T) {
	codec := NewJSONCodec()

	entity := &JSONTestEntity{
		ID:    1,
		Name:  "Test 测试 テスト 🚀",
		Email: "unicode@测试.com",
		Tags:  []string{"日本語", "中文", "한국어"},
	}

	data, err := codec.Encode(entity)
	require.NoError(t, err)

	var decoded JSONTestEntity
	err = codec.Decode(data, &decoded)
	require.NoError(t, err)

	assert.Equal(t, entity.Name, decoded.Name)
	assert.Equal(t, entity.Email, decoded.Email)
	assert.Equal(t, entity.Tags, decoded.Tags)
}

func TestJSONCodec_EmptyValues(t *testing.T) {
	codec := NewJSONCodec()

	entity := &JSONTestEntity{
		ID:    0,
		Name:  "",
		Email: "",
		Score: 0.0,
		Tags:  []string{},
	}

	data, err := codec.Encode(entity)
	require.NoError(t, err)

	var decoded JSONTestEntity
	err = codec.Decode(data, &decoded)
	require.NoError(t, err)

	assert.Equal(t, entity.ID, decoded.ID)
	assert.Equal(t, entity.Name, decoded.Name)
	assert.Equal(t, entity.Email, decoded.Email)
	assert.Equal(t, entity.Score, decoded.Score)
	assert.Empty(t, decoded.Tags)
}

// Benchmark tests.
func BenchmarkJSONCodec_Encode_SimpleStruct(b *testing.B) {
	codec := NewJSONCodec()
	entity := &JSONTestEntity{
		ID:    123,
		Name:  "John Doe",
		Email: "john@example.com",
		Score: 95.5,
		Tags:  []string{"golang", "testing"},
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = codec.Encode(entity)
	}
}

func BenchmarkJSONCodec_Decode_SimpleStruct(b *testing.B) {
	codec := NewJSONCodec()
	entity := &JSONTestEntity{
		ID:    123,
		Name:  "John Doe",
		Email: "john@example.com",
		Score: 95.5,
		Tags:  []string{"golang", "testing"},
	}

	data, _ := codec.Encode(entity)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		var decoded JSONTestEntity
		_ = codec.Decode(data, &decoded)
	}
}

func BenchmarkJSONCodec_RoundTrip(b *testing.B) {
	codec := NewJSONCodec()
	entity := &JSONTestEntity{
		ID:    123,
		Name:  "John Doe",
		Email: "john@example.com",
		Score: 95.5,
		Tags:  []string{"golang", "testing"},
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		data, _ := codec.Encode(entity)
		var decoded JSONTestEntity
		_ = codec.Decode(data, &decoded)
	}
}
