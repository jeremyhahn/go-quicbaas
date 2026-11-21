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

	"github.com/jeremyhahn/go-quicbaas/pkg/codec/proto"
)

// Test entity for protobuf codec testing.
type ProtobufTestEntity struct {
	ID    uint64 `json:"id" protobuf:"varint,1,opt,name=id"`
	Name  string `json:"name" protobuf:"bytes,2,opt,name=name"`
	Value int    `json:"value" protobuf:"varint,3,opt,name=value"`
}

// Simple entity without protobuf tags (will use JSON wrapper).
type SimpleEntity struct {
	ID    uint64 `json:"id"`
	Name  string `json:"name"`
	Value int    `json:"value"`
}

func TestProtobufCodec_Name(t *testing.T) {
	codec := NewProtobufCodec()
	assert.Equal(t, "protobuf", codec.Name())
}

func TestProtobufCodec_Priority(t *testing.T) {
	codec := NewProtobufCodec()
	assert.Equal(t, 100, codec.Priority(), "Protobuf should have highest priority")
}

func TestProtobufCodec_CanEncode(t *testing.T) {
	codec := NewProtobufCodec()

	tests := []struct {
		name     string
		entity   interface{}
		expected bool
	}{
		{
			name:     "nil entity",
			entity:   nil,
			expected: false,
		},
		{
			name:     "entity with protobuf tags",
			entity:   &ProtobufTestEntity{},
			expected: true,
		},
		{
			name:     "entity without protobuf tags",
			entity:   &SimpleEntity{},
			expected: true, // Can encode via JSON wrapper
		},
		{
			name:     "native protobuf message",
			entity:   &proto.GenericMessage{},
			expected: true,
		},
		{
			name:     "struct value",
			entity:   ProtobufTestEntity{},
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
			name:     "primitive type",
			entity:   42,
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := codec.CanEncode(tt.entity)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestProtobufCodec_EncodeDecodeSimple(t *testing.T) {
	codec := NewProtobufCodec()

	entity := &SimpleEntity{
		ID:    1,
		Name:  "test",
		Value: 42,
	}

	// Encode
	data, err := codec.Encode(entity)
	require.NoError(t, err)
	assert.NotEmpty(t, data)

	// Decode
	decoded := &SimpleEntity{}
	err = codec.Decode(data, decoded)
	require.NoError(t, err)

	// Verify
	assert.Equal(t, entity.ID, decoded.ID)
	assert.Equal(t, entity.Name, decoded.Name)
	assert.Equal(t, entity.Value, decoded.Value)
}

func TestProtobufCodec_EncodeDecodeNativeMessage(t *testing.T) {
	codec := NewProtobufCodec()

	entity := &proto.GenericMessage{
		JsonData: []byte(`{"test":"data"}`),
		TypeName: "TestType",
	}

	// Encode
	data, err := codec.Encode(entity)
	require.NoError(t, err)
	assert.NotEmpty(t, data)

	// Decode
	decoded := &proto.GenericMessage{}
	err = codec.Decode(data, decoded)
	require.NoError(t, err)

	// Verify
	assert.Equal(t, entity.JsonData, decoded.JsonData)
	assert.Equal(t, entity.TypeName, decoded.TypeName)
}

func TestProtobufCodec_EncodeNil(t *testing.T) {
	codec := NewProtobufCodec()

	_, err := codec.Encode(nil)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "nil entity")
}

func TestProtobufCodec_DecodeEmpty(t *testing.T) {
	codec := NewProtobufCodec()

	entity := &SimpleEntity{}
	err := codec.Decode([]byte{}, entity)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "empty data")
}

func TestProtobufCodec_DecodeNil(t *testing.T) {
	codec := NewProtobufCodec()

	data := []byte{0x0a, 0x02, 0x7b, 0x7d} // Valid protobuf data
	err := codec.Decode(data, nil)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "nil entity")
}

func TestProtobufCodec_DecodeInvalidData(t *testing.T) {
	codec := NewProtobufCodec()

	entity := &SimpleEntity{}
	err := codec.Decode([]byte("invalid protobuf data"), entity)
	assert.Error(t, err)
}

func TestProtobufCodec_EncodeJSON(t *testing.T) {
	codec := NewProtobufCodec()

	msg := &proto.GenericMessage{
		JsonData: []byte(`{"test":"data"}`),
		TypeName: "TestType",
	}

	// Encode to JSON
	jsonData, err := codec.EncodeJSON(msg)
	require.NoError(t, err)
	assert.NotEmpty(t, jsonData)
	// jsonData field will be base64 encoded, but typeName should be present
	assert.Contains(t, string(jsonData), "TestType")
}

func TestProtobufCodec_EncodeJSON_NotProtoMessage(t *testing.T) {
	codec := NewProtobufCodec()

	entity := &SimpleEntity{ID: 1, Name: "test"}

	_, err := codec.EncodeJSON(entity)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "must be a protobuf message")
}

func TestProtobufCodec_DecodeJSON(t *testing.T) {
	codec := NewProtobufCodec()

	jsonData := []byte(`{"jsonData":"eyJ0ZXN0IjoiZGF0YSJ9","typeName":"TestType"}`)

	msg := &proto.GenericMessage{}
	err := codec.DecodeJSON(jsonData, msg)
	require.NoError(t, err)
	assert.Equal(t, "TestType", msg.TypeName)
}

func TestProtobufCodec_DecodeJSON_NotProtoMessage(t *testing.T) {
	codec := NewProtobufCodec()

	jsonData := []byte(`{"id":1,"name":"test"}`)
	entity := &SimpleEntity{}

	err := codec.DecodeJSON(jsonData, entity)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "must be a protobuf message")
}

// Benchmark tests.
func BenchmarkProtobufCodec_Encode(b *testing.B) {
	codec := NewProtobufCodec()
	entity := &SimpleEntity{ID: 1, Name: "benchmark", Value: 42}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = codec.Encode(entity)
	}
}

func BenchmarkProtobufCodec_Decode(b *testing.B) {
	codec := NewProtobufCodec()
	entity := &SimpleEntity{ID: 1, Name: "benchmark", Value: 42}
	data, _ := codec.Encode(entity)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		decoded := &SimpleEntity{}
		_ = codec.Decode(data, decoded)
	}
}

func BenchmarkProtobufCodec_RoundTrip(b *testing.B) {
	codec := NewProtobufCodec()
	entity := &SimpleEntity{ID: 1, Name: "benchmark", Value: 42}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		data, _ := codec.Encode(entity)
		decoded := &SimpleEntity{}
		_ = codec.Decode(data, decoded)
	}
}
