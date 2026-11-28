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

type YAMLTestEntity struct {
	ID    uint64   `yaml:"id"`
	Name  string   `yaml:"name"`
	Email string   `yaml:"email"`
	Score float64  `yaml:"score"`
	Tags  []string `yaml:"tags"`
}

type YAMLNestedEntity struct {
	ID      uint64                 `yaml:"id"`
	Name    string                 `yaml:"name"`
	Address YAMLAddress            `yaml:"address"`
	Meta    map[string]interface{} `yaml:"meta"`
}

type YAMLAddress struct {
	Street string `yaml:"street"`
	City   string `yaml:"city"`
	Zip    string `yaml:"zip"`
}

func TestYAMLCodec_Name(t *testing.T) {
	codec := NewYAMLCodec()
	assert.Equal(t, "yaml", codec.Name())
}

func TestYAMLCodec_Priority(t *testing.T) {
	codec := NewYAMLCodec()
	assert.Equal(t, 30, codec.Priority())
}

func TestYAMLCodec_CanEncode(t *testing.T) {
	codec := NewYAMLCodec()

	tests := []struct {
		name     string
		entity   interface{}
		expected bool
	}{
		{"struct", &YAMLTestEntity{ID: 1, Name: "test"}, true},
		{"map", map[string]interface{}{"key": "value"}, true},
		{"slice", []string{"a", "b", "c"}, true},
		{"int", 42, true},
		{"string", "hello", true},
		{"bool", true, true},
		{"nil", nil, false},
		{"channel", make(chan int), false},
		{"function", func() {}, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := codec.CanEncode(tt.entity)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestYAMLCodec_Encode(t *testing.T) {
	codec := NewYAMLCodec()

	tests := []struct {
		name        string
		entity      interface{}
		expectError bool
	}{
		{
			name: "simple struct",
			entity: &YAMLTestEntity{
				ID:    123,
				Name:  "John Doe",
				Email: "john@example.com",
				Score: 95.5,
				Tags:  []string{"golang", "testing"},
			},
			expectError: false,
		},
		{"nil", nil, true},
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

func TestYAMLCodec_Decode(t *testing.T) {
	codec := NewYAMLCodec()

	t.Run("simple struct", func(t *testing.T) {
		original := &YAMLTestEntity{
			ID:    123,
			Name:  "John Doe",
			Email: "john@example.com",
			Score: 95.5,
			Tags:  []string{"golang", "testing"},
		}

		data, err := codec.Encode(original)
		require.NoError(t, err)

		var decoded YAMLTestEntity
		err = codec.Decode(data, &decoded)
		require.NoError(t, err)

		assert.Equal(t, original.ID, decoded.ID)
		assert.Equal(t, original.Name, decoded.Name)
		assert.Equal(t, original.Email, decoded.Email)
		assert.Equal(t, original.Score, decoded.Score)
		assert.Equal(t, original.Tags, decoded.Tags)
	})

	t.Run("nested struct", func(t *testing.T) {
		original := &YAMLNestedEntity{
			ID:   456,
			Name: "Jane Doe",
			Address: YAMLAddress{
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

		var decoded YAMLNestedEntity
		err = codec.Decode(data, &decoded)
		require.NoError(t, err)

		assert.Equal(t, original.ID, decoded.ID)
		assert.Equal(t, original.Name, decoded.Name)
		assert.Equal(t, original.Address.Street, decoded.Address.Street)
		assert.True(t, decoded.Meta["verified"].(bool))
	})

	t.Run("errors", func(t *testing.T) {
		var entity YAMLTestEntity

		// Empty data
		err := codec.Decode([]byte{}, &entity)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "cannot decode empty data")

		// Nil entity
		err = codec.Decode([]byte("id: 123"), nil)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "cannot decode into nil entity")

		// Non-pointer
		err = codec.Decode([]byte("id: 123"), entity)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "entity must be a pointer")

		// Invalid YAML
		err = codec.Decode([]byte("invalid: [unclosed"), &entity)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "failed to decode YAML")
	})
}

func BenchmarkYAMLCodec_Encode(b *testing.B) {
	codec := NewYAMLCodec()
	entity := &YAMLTestEntity{
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

func BenchmarkYAMLCodec_Decode(b *testing.B) {
	codec := NewYAMLCodec()
	entity := &YAMLTestEntity{
		ID:    123,
		Name:  "John Doe",
		Email: "john@example.com",
		Score: 95.5,
		Tags:  []string{"golang", "testing"},
	}

	data, _ := codec.Encode(entity)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		var decoded YAMLTestEntity
		_ = codec.Decode(data, &decoded)
	}
}
