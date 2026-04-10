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
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	dberrors "github.com/jeremyhahn/go-quicbaas/pkg/errors"
)

// Mock codec implementation for testing.
type mockCodec struct {
	name     string
	priority int
	canEnc   func(interface{}) bool
	enc      func(interface{}) ([]byte, error)
	dec      func([]byte, interface{}) error
}

func (m *mockCodec) Name() string {
	return m.name
}

func (m *mockCodec) Priority() int {
	return m.priority
}

func (m *mockCodec) CanEncode(entity interface{}) bool {
	if m.canEnc != nil {
		return m.canEnc(entity)
	}
	return true
}

func (m *mockCodec) Encode(entity interface{}) ([]byte, error) {
	if m.enc != nil {
		return m.enc(entity)
	}
	return []byte(fmt.Sprintf("encoded-%v", entity)), nil
}

func (m *mockCodec) Decode(data []byte, entity interface{}) error {
	if m.dec != nil {
		return m.dec(data, entity)
	}
	return nil
}

// Test entities.
type TestEntity struct {
	ID   uint64 `json:"id"`
	Name string `json:"name"`
}

type ProtobufEntity struct {
	ID   uint64 `protobuf:"varint,1,opt,name=id" json:"id"`
	Name string `protobuf:"bytes,2,opt,name=name" json:"name"`
}

type MsgPackEntity struct {
	ID   uint64 `msgpack:"id"`
	Name string `msgpack:"name"`
}

type YAMLEntity struct {
	ID   uint64 `yaml:"id"`
	Name string `yaml:"name"`
}

type ExplicitCodecEntity struct {
	ID    uint64 `json:"id" quicbaas:"codec=msgpack"`
	Name  string `json:"name"`
	Email string `json:"email"`
}

type NoTagsEntity struct {
	ID   uint64
	Name string
}

func TestRegistry_Register(t *testing.T) {
	r := NewRegistry()
	assert.NotNil(t, r)
	assert.Empty(t, r.codecs)

	codec := &mockCodec{name: "test", priority: 50}
	r.Register(codec)

	assert.Len(t, r.codecs, 1)
	assert.Equal(t, codec, r.Get("test"))
}

func TestRegistry_RegisterReplaces(t *testing.T) {
	r := NewRegistry()

	codec1 := &mockCodec{name: "test", priority: 50}
	codec2 := &mockCodec{name: "test", priority: 100}

	r.Register(codec1)
	assert.Equal(t, codec1, r.Get("test"))

	r.Register(codec2)
	assert.Equal(t, codec2, r.Get("test"))
	assert.Len(t, r.codecs, 1)
}

func TestRegistry_Get(t *testing.T) {
	r := NewRegistry()

	codec := &mockCodec{name: "test", priority: 50}
	r.Register(codec)

	assert.Equal(t, codec, r.Get("test"))
	assert.Nil(t, r.Get("nonexistent"))
}

func TestRegistry_ListCodecs(t *testing.T) {
	r := NewRegistry()

	r.Register(&mockCodec{name: "json", priority: 50})
	r.Register(&mockCodec{name: "protobuf", priority: 100})
	r.Register(&mockCodec{name: "msgpack", priority: 40})

	names := r.ListCodecs()
	assert.Len(t, names, 3)
	assert.Contains(t, names, "json")
	assert.Contains(t, names, "protobuf")
	assert.Contains(t, names, "msgpack")
}

func TestRegistry_Encode(t *testing.T) {
	r := NewRegistry()

	encoded := []byte("test-data")
	codec := &mockCodec{
		name:     "test",
		priority: 50,
		enc: func(entity interface{}) ([]byte, error) {
			return encoded, nil
		},
	}
	r.Register(codec)

	entity := &TestEntity{ID: 1, Name: "test"}
	data, err := r.Encode(entity, "test")

	require.NoError(t, err)
	assert.Equal(t, encoded, data)
}

func TestRegistry_Encode_CodecNotFound(t *testing.T) {
	r := NewRegistry()

	entity := &TestEntity{ID: 1, Name: "test"}
	_, err := r.Encode(entity, "nonexistent")

	require.Error(t, err)
	assert.Contains(t, err.Error(), "codec nonexistent not found in registry")
}

func TestRegistry_Encode_Error(t *testing.T) {
	r := NewRegistry()

	expectedErr := &dberrors.MessageError{Message: "encode failed"}
	codec := &mockCodec{
		name:     "test",
		priority: 50,
		enc: func(entity interface{}) ([]byte, error) {
			return nil, expectedErr
		},
	}
	r.Register(codec)

	entity := &TestEntity{ID: 1, Name: "test"}
	_, err := r.Encode(entity, "test")

	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to encode test")
}

func TestRegistry_Decode(t *testing.T) {
	r := NewRegistry()

	codec := &mockCodec{
		name:     "test",
		priority: 50,
		dec: func(data []byte, entity interface{}) error {
			e := entity.(*TestEntity)
			e.ID = 123
			e.Name = "decoded"
			return nil
		},
	}
	r.Register(codec)

	var entity TestEntity
	err := r.Decode([]byte("test-data"), &entity, "test")

	require.NoError(t, err)
	assert.Equal(t, uint64(123), entity.ID)
	assert.Equal(t, "decoded", entity.Name)
}

func TestRegistry_Decode_CodecNotFound(t *testing.T) {
	r := NewRegistry()

	var entity TestEntity
	err := r.Decode([]byte("test-data"), &entity, "nonexistent")

	require.Error(t, err)
	assert.Contains(t, err.Error(), "codec nonexistent not found in registry")
}

func TestRegistry_Decode_Error(t *testing.T) {
	r := NewRegistry()

	expectedErr := &dberrors.MessageError{Message: "decode failed"}
	codec := &mockCodec{
		name:     "test",
		priority: 50,
		dec: func(data []byte, entity interface{}) error {
			return expectedErr
		},
	}
	r.Register(codec)

	var entity TestEntity
	err := r.Decode([]byte("test-data"), &entity, "test")

	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to decode test")
}

func TestRegistry_DetectExplicitCodec(t *testing.T) {
	r := NewRegistry()

	tests := []struct {
		name     string
		entity   interface{}
		expected string
	}{
		{
			name:     "explicit codec tag",
			entity:   &ExplicitCodecEntity{ID: 1, Name: "test"},
			expected: "msgpack",
		},
		{
			name:     "no explicit codec",
			entity:   &TestEntity{ID: 1, Name: "test"},
			expected: "",
		},
		{
			name:     "nil entity",
			entity:   nil,
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := r.detectExplicitCodec(tt.entity)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestRegistry_DetectCodecFromTags(t *testing.T) {
	r := NewRegistry()

	tests := []struct {
		name     string
		entity   interface{}
		expected string
	}{
		{
			name:     "protobuf tags (highest priority)",
			entity:   &ProtobufEntity{ID: 1, Name: "test"},
			expected: "protobuf",
		},
		{
			name:     "json tags",
			entity:   &TestEntity{ID: 1, Name: "test"},
			expected: "json",
		},
		{
			name:     "msgpack tags",
			entity:   &MsgPackEntity{ID: 1, Name: "test"},
			expected: "msgpack",
		},
		{
			name:     "yaml tags",
			entity:   &YAMLEntity{ID: 1, Name: "test"},
			expected: "yaml",
		},
		{
			name:     "no tags",
			entity:   &NoTagsEntity{ID: 1, Name: "test"},
			expected: "",
		},
		{
			name:     "nil entity",
			entity:   nil,
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := r.detectCodecFromTags(tt.entity)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestRegistry_EncodeAuto_ExplicitCodec(t *testing.T) {
	r := NewRegistry()

	// Register codecs
	r.Register(&mockCodec{
		name:     "msgpack",
		priority: 40,
		enc: func(entity interface{}) ([]byte, error) {
			return []byte("msgpack-encoded"), nil
		},
	})
	r.Register(&mockCodec{
		name:     "json",
		priority: 50,
		enc: func(entity interface{}) ([]byte, error) {
			return []byte("json-encoded"), nil
		},
	})

	// Entity has explicit codec=msgpack tag
	entity := &ExplicitCodecEntity{ID: 1, Name: "test"}
	data, codecName, err := r.EncodeAuto(entity)

	require.NoError(t, err)
	assert.Equal(t, "msgpack", codecName)
	assert.Equal(t, []byte("msgpack-encoded"), data)
}

func TestRegistry_EncodeAuto_TagDetection(t *testing.T) {
	r := NewRegistry()

	// Register codecs
	r.Register(&mockCodec{
		name:     "json",
		priority: 50,
		enc: func(entity interface{}) ([]byte, error) {
			return []byte("json-encoded"), nil
		},
	})
	r.Register(&mockCodec{
		name:     "protobuf",
		priority: 100,
		enc: func(entity interface{}) ([]byte, error) {
			return []byte("protobuf-encoded"), nil
		},
	})

	tests := []struct {
		name         string
		entity       interface{}
		expectedName string
		expectedData []byte
	}{
		{
			name:         "protobuf tags detected",
			entity:       &ProtobufEntity{ID: 1, Name: "test"},
			expectedName: "protobuf",
			expectedData: []byte("protobuf-encoded"),
		},
		{
			name:         "json tags detected",
			entity:       &TestEntity{ID: 1, Name: "test"},
			expectedName: "json",
			expectedData: []byte("json-encoded"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data, codecName, err := r.EncodeAuto(tt.entity)
			require.NoError(t, err)
			assert.Equal(t, tt.expectedName, codecName)
			assert.Equal(t, tt.expectedData, data)
		})
	}
}

func TestRegistry_EncodeAuto_PriorityFallback(t *testing.T) {
	r := NewRegistry()

	// Register codecs with different priorities
	r.Register(&mockCodec{
		name:     "low-priority",
		priority: 10,
		canEnc:   func(entity interface{}) bool { return true },
		enc: func(entity interface{}) ([]byte, error) {
			return []byte("low-priority-encoded"), nil
		},
	})
	r.Register(&mockCodec{
		name:     "high-priority",
		priority: 100,
		canEnc:   func(entity interface{}) bool { return true },
		enc: func(entity interface{}) ([]byte, error) {
			return []byte("high-priority-encoded"), nil
		},
	})

	// Entity with no tags should use highest priority codec
	entity := &NoTagsEntity{ID: 1, Name: "test"}
	data, codecName, err := r.EncodeAuto(entity)

	require.NoError(t, err)
	assert.Equal(t, "high-priority", codecName)
	assert.Equal(t, []byte("high-priority-encoded"), data)
}

func TestRegistry_EncodeAuto_NoSuitableCodec(t *testing.T) {
	r := NewRegistry()

	// Register codec that can't encode the entity
	r.Register(&mockCodec{
		name:     "picky",
		priority: 100,
		canEnc:   func(entity interface{}) bool { return false },
	})

	entity := &NoTagsEntity{ID: 1, Name: "test"}
	_, _, err := r.EncodeAuto(entity)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "no suitable codec found")
}

func TestRegistry_EncodeAuto_ExplicitCodecNotFound(t *testing.T) {
	r := NewRegistry()

	// Register a different codec
	r.Register(&mockCodec{name: "json", priority: 50})

	// Entity requests msgpack which isn't registered
	entity := &ExplicitCodecEntity{ID: 1, Name: "test"}
	_, _, err := r.EncodeAuto(entity)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "codec msgpack not found in registry")
}

func TestRegistry_EncodeAuto_EncodeError(t *testing.T) {
	r := NewRegistry()

	expectedErr := &dberrors.MessageError{Message: "encoding failed"}
	r.Register(&mockCodec{
		name:     "json",
		priority: 50,
		enc: func(entity interface{}) ([]byte, error) {
			return nil, expectedErr
		},
	})

	entity := &TestEntity{ID: 1, Name: "test"}
	_, _, err := r.EncodeAuto(entity)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to encode json")
}

// Benchmark tests.
func BenchmarkRegistry_EncodeAuto_ExplicitCodec(b *testing.B) {
	r := NewRegistry()
	r.Register(&mockCodec{
		name:     "msgpack",
		priority: 40,
		enc: func(entity interface{}) ([]byte, error) {
			return []byte("msgpack-encoded"), nil
		},
	})

	entity := &ExplicitCodecEntity{ID: 1, Name: "test"}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _, _ = r.EncodeAuto(entity)
	}
}

func BenchmarkRegistry_EncodeAuto_TagDetection(b *testing.B) {
	r := NewRegistry()
	r.Register(&mockCodec{
		name:     "json",
		priority: 50,
		enc: func(entity interface{}) ([]byte, error) {
			return []byte("json-encoded"), nil
		},
	})

	entity := &TestEntity{ID: 1, Name: "test"}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _, _ = r.EncodeAuto(entity)
	}
}

func BenchmarkRegistry_EncodeAuto_PriorityFallback(b *testing.B) {
	r := NewRegistry()
	r.Register(&mockCodec{
		name:     "high-priority",
		priority: 100,
		canEnc:   func(entity interface{}) bool { return true },
		enc: func(entity interface{}) ([]byte, error) {
			return []byte("high-priority-encoded"), nil
		},
	})

	entity := &NoTagsEntity{ID: 1, Name: "test"}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _, _ = r.EncodeAuto(entity)
	}
}
