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

type MsgPackTestEntity struct {
	ID    uint64   `msgpack:"id"`
	Name  string   `msgpack:"name"`
	Email string   `msgpack:"email"`
	Score float64  `msgpack:"score"`
	Tags  []string `msgpack:"tags"`
}

type MsgPackNestedEntity struct {
	ID      uint64                 `msgpack:"id"`
	Name    string                 `msgpack:"name"`
	Address MsgPackAddress         `msgpack:"address"`
	Meta    map[string]interface{} `msgpack:"meta"`
}

type MsgPackAddress struct {
	Street string `msgpack:"street"`
	City   string `msgpack:"city"`
	Zip    string `msgpack:"zip"`
}

func TestMsgPackCodec_Name(t *testing.T) {
	codec := NewMsgPackCodec()
	assert.Equal(t, "msgpack", codec.Name())
}

func TestMsgPackCodec_Priority(t *testing.T) {
	codec := NewMsgPackCodec()
	assert.Equal(t, 40, codec.Priority())
}

func TestMsgPackCodec_CanEncode(t *testing.T) {
	codec := NewMsgPackCodec()

	tests := []struct {
		name     string
		entity   interface{}
		expected bool
	}{
		{
			name:     "struct",
			entity:   &MsgPackTestEntity{ID: 1, Name: "test"},
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

func TestMsgPackCodec_Encode(t *testing.T) {
	codec := NewMsgPackCodec()

	tests := []struct {
		name        string
		entity      interface{}
		expectError bool
	}{
		{
			name: "simple struct",
			entity: &MsgPackTestEntity{
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
			entity: &MsgPackNestedEntity{
				ID:   456,
				Name: "Jane Doe",
				Address: MsgPackAddress{
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
			}
		})
	}
}

func TestMsgPackCodec_Decode(t *testing.T) {
	codec := NewMsgPackCodec()

	t.Run("simple struct", func(t *testing.T) {
		original := &MsgPackTestEntity{
			ID:    123,
			Name:  "John Doe",
			Email: "john@example.com",
			Score: 95.5,
			Tags:  []string{"golang", "testing"},
		}

		data, err := codec.Encode(original)
		require.NoError(t, err)

		var decoded MsgPackTestEntity
		err = codec.Decode(data, &decoded)
		require.NoError(t, err)

		assert.Equal(t, original.ID, decoded.ID)
		assert.Equal(t, original.Name, decoded.Name)
		assert.Equal(t, original.Email, decoded.Email)
		assert.Equal(t, original.Score, decoded.Score)
		assert.Equal(t, original.Tags, decoded.Tags)
	})

	t.Run("nested struct", func(t *testing.T) {
		original := &MsgPackNestedEntity{
			ID:   456,
			Name: "Jane Doe",
			Address: MsgPackAddress{
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

		var decoded MsgPackNestedEntity
		err = codec.Decode(data, &decoded)
		require.NoError(t, err)

		assert.Equal(t, original.ID, decoded.ID)
		assert.Equal(t, original.Name, decoded.Name)
		assert.Equal(t, original.Address.Street, decoded.Address.Street)
		assert.Equal(t, original.Address.City, decoded.Address.City)
		assert.Equal(t, original.Address.Zip, decoded.Address.Zip)
		assert.True(t, decoded.Meta["verified"].(bool))
		assert.InDelta(t, 98.5, decoded.Meta["score"].(float64), 0.01)
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
		var entity MsgPackTestEntity
		err := codec.Decode([]byte{}, &entity)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "cannot decode empty data")
	})

	t.Run("nil entity", func(t *testing.T) {
		data := []byte{0x81, 0xA2, 0x69, 0x64, 0x7B} // msgpack encoded
		err := codec.Decode(data, nil)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "cannot decode into nil entity")
	})

	t.Run("non-pointer entity", func(t *testing.T) {
		data := []byte{0x81, 0xA2, 0x69, 0x64, 0x7B} // msgpack encoded
		var entity MsgPackTestEntity
		err := codec.Decode(data, entity)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "entity must be a pointer")
	})

	t.Run("invalid msgpack", func(t *testing.T) {
		data := []byte{0xFF, 0xFF, 0xFF} // invalid msgpack
		var entity MsgPackTestEntity
		err := codec.Decode(data, &entity)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "failed to decode msgpack")
	})
}

func TestMsgPackCodec_RoundTrip(t *testing.T) {
	codec := NewMsgPackCodec()

	tests := []struct {
		name   string
		entity interface{}
	}{
		{
			name: "simple entity",
			entity: &MsgPackTestEntity{
				ID:    999,
				Name:  "Round Trip Test",
				Email: "roundtrip@example.com",
				Score: 88.8,
				Tags:  []string{"test", "roundtrip"},
			},
		},
		{
			name: "nested entity",
			entity: &MsgPackNestedEntity{
				ID:   777,
				Name: "Nested Test",
				Address: MsgPackAddress{
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
			case *MsgPackTestEntity:
				decoded = &MsgPackTestEntity{}
			case *MsgPackNestedEntity:
				decoded = &MsgPackNestedEntity{}
			}

			err = codec.Decode(data, decoded)
			require.NoError(t, err)

			// Verify round trip by comparing decoded values
			// Note: Map key ordering may differ in binary representation
			// but the decoded values should be equivalent
			switch original := tt.entity.(type) {
			case *MsgPackTestEntity:
				dec := decoded.(*MsgPackTestEntity)
				assert.Equal(t, original.ID, dec.ID)
				assert.Equal(t, original.Name, dec.Name)
				assert.Equal(t, original.Email, dec.Email)
				assert.Equal(t, original.Score, dec.Score)
				assert.Equal(t, original.Tags, dec.Tags)
			case *MsgPackNestedEntity:
				dec := decoded.(*MsgPackNestedEntity)
				assert.Equal(t, original.ID, dec.ID)
				assert.Equal(t, original.Name, dec.Name)
				assert.Equal(t, original.Address, dec.Address)
				// Compare map contents instead of binary representation
				for k, v := range original.Meta {
					// MsgPack may deserialize ints as int64
					if expected, ok := v.(int); ok {
						actual := dec.Meta[k].(int64)
						assert.Equal(t, int64(expected), actual, "map key %s should match", k)
					} else {
						assert.Equal(t, v, dec.Meta[k], "map key %s should match", k)
					}
				}
			}
		})
	}
}

func TestMsgPackCodec_Unicode(t *testing.T) {
	codec := NewMsgPackCodec()

	entity := &MsgPackTestEntity{
		ID:    1,
		Name:  "Test 测试 テスト 🚀",
		Email: "unicode@测试.com",
		Tags:  []string{"日本語", "中文", "한국어"},
	}

	data, err := codec.Encode(entity)
	require.NoError(t, err)

	var decoded MsgPackTestEntity
	err = codec.Decode(data, &decoded)
	require.NoError(t, err)

	assert.Equal(t, entity.Name, decoded.Name)
	assert.Equal(t, entity.Email, decoded.Email)
	assert.Equal(t, entity.Tags, decoded.Tags)
}

func TestMsgPackCodec_EmptyValues(t *testing.T) {
	codec := NewMsgPackCodec()

	entity := &MsgPackTestEntity{
		ID:    0,
		Name:  "",
		Email: "",
		Score: 0.0,
		Tags:  []string{},
	}

	data, err := codec.Encode(entity)
	require.NoError(t, err)

	var decoded MsgPackTestEntity
	err = codec.Decode(data, &decoded)
	require.NoError(t, err)

	assert.Equal(t, entity.ID, decoded.ID)
	assert.Equal(t, entity.Name, decoded.Name)
	assert.Equal(t, entity.Email, decoded.Email)
	assert.Equal(t, entity.Score, decoded.Score)
	// Empty slice may be nil after round trip - this is expected behavior
	if entity.Tags != nil {
		assert.NotNil(t, decoded.Tags)
	}
}

func TestMsgPackCodec_CompactnessVsJSON(t *testing.T) {
	msgpackCodec := NewMsgPackCodec()
	jsonCodec := NewJSONCodec()

	entity := &MsgPackTestEntity{
		ID:    123456789,
		Name:  "This is a long name to test compactness",
		Email: "verylongemailaddress@example.com",
		Score: 12345.67890,
		Tags:  []string{"tag1", "tag2", "tag3", "tag4", "tag5"},
	}

	msgpackData, err := msgpackCodec.Encode(entity)
	require.NoError(t, err)

	jsonData, err := jsonCodec.Encode(entity)
	require.NoError(t, err)

	// MsgPack should be more compact than JSON
	t.Logf("MsgPack size: %d bytes", len(msgpackData))
	t.Logf("JSON size: %d bytes", len(jsonData))
	assert.Less(t, len(msgpackData), len(jsonData), "MsgPack should be more compact than JSON")
}

// Benchmark tests.
func BenchmarkMsgPackCodec_Encode_SimpleStruct(b *testing.B) {
	codec := NewMsgPackCodec()
	entity := &MsgPackTestEntity{
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

func BenchmarkMsgPackCodec_Decode_SimpleStruct(b *testing.B) {
	codec := NewMsgPackCodec()
	entity := &MsgPackTestEntity{
		ID:    123,
		Name:  "John Doe",
		Email: "john@example.com",
		Score: 95.5,
		Tags:  []string{"golang", "testing"},
	}

	data, _ := codec.Encode(entity)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		var decoded MsgPackTestEntity
		_ = codec.Decode(data, &decoded)
	}
}

func BenchmarkMsgPackCodec_RoundTrip(b *testing.B) {
	codec := NewMsgPackCodec()
	entity := &MsgPackTestEntity{
		ID:    123,
		Name:  "John Doe",
		Email: "john@example.com",
		Score: 95.5,
		Tags:  []string{"golang", "testing"},
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		data, _ := codec.Encode(entity)
		var decoded MsgPackTestEntity
		_ = codec.Decode(data, &decoded)
	}
}
