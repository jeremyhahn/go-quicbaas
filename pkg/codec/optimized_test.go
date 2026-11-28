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
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestJSONOptimizedCodec_EncodeTo tests the optimized EncodeTo method.
func TestJSONOptimizedCodec_EncodeTo(t *testing.T) {
	codec := NewJSONOptimized()

	t.Run("success with provided buffer", func(t *testing.T) {
		entity := &JSONTestEntity{ID: 42, Name: "test"}
		buf := make([]byte, 0, 1024)

		result, err := codec.EncodeTo(buf, entity)
		require.NoError(t, err)
		assert.NotEmpty(t, result)

		// Verify buffer was reused (same capacity)
		assert.Equal(t, cap(buf), cap(result))
	})

	t.Run("success with small buffer", func(t *testing.T) {
		entity := &JSONTestEntity{ID: 42, Name: "test"}
		buf := make([]byte, 0, 10) // Too small

		result, err := codec.EncodeTo(buf, entity)
		require.NoError(t, err)
		assert.NotEmpty(t, result)

		// New buffer allocated (different capacity)
		assert.NotEqual(t, cap(buf), cap(result))
	})

	t.Run("nil entity", func(t *testing.T) {
		buf := make([]byte, 0, 1024)
		result, err := codec.EncodeTo(buf, nil)
		assert.Error(t, err)
		assert.Nil(t, result)
		assert.Contains(t, err.Error(), "cannot encode nil entity")
	})
}

// TestJSONOptimizedCodec_DecodeWith tests the optimized DecodeWith method.
func TestJSONOptimizedCodec_DecodeWith(t *testing.T) {
	codec := NewJSONOptimized()

	// The success path is covered by TestOptimizedCodecs_RoundTrip.
	t.Run("empty data", func(t *testing.T) {
		entity := &JSONTestEntity{}
		err := codec.DecodeWith([]byte{}, entity, nil)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "cannot decode empty data")
	})

	t.Run("nil entity", func(t *testing.T) {
		data := []byte(`{"id":123}`)
		err := codec.DecodeWith(data, nil, nil)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "cannot decode into nil entity")
	})
}

// TestJSONOptimizedCodec_EncodePooled tests the pooled encoding method.
func TestJSONOptimizedCodec_EncodePooled(t *testing.T) {
	codec := NewJSONOptimized()

	t.Run("success with cleanup", func(t *testing.T) {
		entity := &JSONTestEntity{ID: 42, Name: "pooled"}

		data, cleanup, err := codec.EncodePooled(entity)
		require.NoError(t, err)
		require.NotNil(t, cleanup)
		assert.NotEmpty(t, data)

		// Verify we can decode
		decoded := &JSONTestEntity{}
		err = codec.Decode(data, decoded)
		require.NoError(t, err)
		assert.Equal(t, entity.ID, decoded.ID)

		// Call cleanup
		cleanup()
	})

	t.Run("multiple pooled operations", func(t *testing.T) {
		for i := 0; i < 100; i++ {
			entity := &JSONTestEntity{ID: uint64(i), Name: "multi-pooled"}

			data, cleanup, err := codec.EncodePooled(entity)
			require.NoError(t, err)
			assert.NotEmpty(t, data)

			// Should be able to use data before cleanup
			decoded := &JSONTestEntity{}
			err = codec.Decode(data, decoded)
			require.NoError(t, err)
			assert.Equal(t, entity.ID, decoded.ID)

			cleanup()
		}
	})

	t.Run("nil entity", func(t *testing.T) {
		data, cleanup, err := codec.EncodePooled(nil)
		assert.Error(t, err)
		assert.Nil(t, data)
		assert.Nil(t, cleanup)
		assert.Contains(t, err.Error(), "cannot encode nil entity")
	})
}

// TestMsgPackOptimizedCodec_EncodeTo tests the optimized EncodeTo method.
func TestMsgPackOptimizedCodec_EncodeTo(t *testing.T) {
	codec := NewMsgPackOptimized()

	t.Run("success with provided buffer", func(t *testing.T) {
		entity := &MsgPackTestEntity{ID: 42, Name: "test"}
		buf := make([]byte, 0, 1024)

		result, err := codec.EncodeTo(buf, entity)
		require.NoError(t, err)
		assert.NotEmpty(t, result)

		// Verify buffer was reused (same capacity)
		assert.Equal(t, cap(buf), cap(result))
	})

	t.Run("success with small buffer", func(t *testing.T) {
		entity := &MsgPackTestEntity{ID: 42, Name: "test"}
		buf := make([]byte, 0, 10) // Too small

		result, err := codec.EncodeTo(buf, entity)
		require.NoError(t, err)
		assert.NotEmpty(t, result)

		// New buffer allocated (different capacity)
		assert.NotEqual(t, cap(buf), cap(result))
	})

	t.Run("nil entity", func(t *testing.T) {
		buf := make([]byte, 0, 1024)
		result, err := codec.EncodeTo(buf, nil)
		assert.Error(t, err)
		assert.Nil(t, result)
		assert.Contains(t, err.Error(), "cannot encode nil entity")
	})
}

// TestMsgPackOptimizedCodec_DecodeWith tests the optimized DecodeWith method.
func TestMsgPackOptimizedCodec_DecodeWith(t *testing.T) {
	codec := NewMsgPackOptimized()

	// The success path is covered by TestOptimizedCodecs_RoundTrip.
	t.Run("empty data", func(t *testing.T) {
		entity := &MsgPackTestEntity{}
		err := codec.DecodeWith([]byte{}, entity, nil)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "cannot decode empty data")
	})

	t.Run("nil entity", func(t *testing.T) {
		data := []byte{0x81, 0xA2, 0x69, 0x64, 0x7B} // msgpack encoded
		err := codec.DecodeWith(data, nil, nil)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "cannot decode into nil entity")
	})
}

// TestMsgPackOptimizedCodec_EncodePooled tests the pooled encoding method.
func TestMsgPackOptimizedCodec_EncodePooled(t *testing.T) {
	codec := NewMsgPackOptimized()

	t.Run("success with cleanup", func(t *testing.T) {
		entity := &MsgPackTestEntity{ID: 42, Name: "pooled"}

		data, cleanup, err := codec.EncodePooled(entity)
		require.NoError(t, err)
		require.NotNil(t, cleanup)
		assert.NotEmpty(t, data)

		// Verify we can decode
		decoded := &MsgPackTestEntity{}
		err = codec.Decode(data, decoded)
		require.NoError(t, err)
		assert.Equal(t, entity.ID, decoded.ID)

		// Call cleanup
		cleanup()
	})

	t.Run("multiple pooled operations", func(t *testing.T) {
		for i := 0; i < 100; i++ {
			entity := &MsgPackTestEntity{ID: uint64(i), Name: "multi-pooled"}

			data, cleanup, err := codec.EncodePooled(entity)
			require.NoError(t, err)
			assert.NotEmpty(t, data)

			// Should be able to use data before cleanup
			decoded := &MsgPackTestEntity{}
			err = codec.Decode(data, decoded)
			require.NoError(t, err)
			assert.Equal(t, entity.ID, decoded.ID)

			cleanup()
		}
	})

	t.Run("nil entity", func(t *testing.T) {
		data, cleanup, err := codec.EncodePooled(nil)
		assert.Error(t, err)
		assert.Nil(t, data)
		assert.Nil(t, cleanup)
		assert.Contains(t, err.Error(), "cannot encode nil entity")
	})
}

// TestOptimizedCodecs_Concurrent tests concurrent access to optimized codecs.
func TestOptimizedCodecs_Concurrent(t *testing.T) {
	jsonCodec := NewJSONOptimized()
	msgpackCodec := NewMsgPackOptimized()

	const goroutines = 50
	const iterations = 100

	t.Run("JSON optimized concurrent", func(t *testing.T) {
		var wg sync.WaitGroup
		wg.Add(goroutines)

		for i := 0; i < goroutines; i++ {
			go func(id int) {
				defer wg.Done()

				for j := 0; j < iterations; j++ {
					entity := &JSONTestEntity{ID: uint64(id*iterations + j), Name: "concurrent"}

					// Test EncodeTo
					buf := make([]byte, 0, 1024)
					buf, err := jsonCodec.EncodeTo(buf, entity)
					assert.NoError(t, err)

					// Test DecodeWith
					decoded := &JSONTestEntity{}
					err = jsonCodec.DecodeWith(buf, decoded, nil)
					assert.NoError(t, err)
					assert.Equal(t, entity.ID, decoded.ID)

					// Test EncodePooled
					data, cleanup, err := jsonCodec.EncodePooled(entity)
					assert.NoError(t, err)
					assert.NotEmpty(t, data)
					cleanup()
				}
			}(i)
		}

		wg.Wait()
	})

	t.Run("MsgPack optimized concurrent", func(t *testing.T) {
		var wg sync.WaitGroup
		wg.Add(goroutines)

		for i := 0; i < goroutines; i++ {
			go func(id int) {
				defer wg.Done()

				for j := 0; j < iterations; j++ {
					entity := &MsgPackTestEntity{ID: uint64(id*iterations + j), Name: "concurrent"}

					// Test EncodeTo
					buf := make([]byte, 0, 1024)
					buf, err := msgpackCodec.EncodeTo(buf, entity)
					assert.NoError(t, err)

					// Test DecodeWith
					decoded := &MsgPackTestEntity{}
					err = msgpackCodec.DecodeWith(buf, decoded, nil)
					assert.NoError(t, err)
					assert.Equal(t, entity.ID, decoded.ID)

					// Test EncodePooled
					data, cleanup, err := msgpackCodec.EncodePooled(entity)
					assert.NoError(t, err)
					assert.NotEmpty(t, data)
					cleanup()
				}
			}(i)
		}

		wg.Wait()
	})
}

// TestOptimizedCodecs_RoundTrip tests full round trip with optimized methods.
func TestOptimizedCodecs_RoundTrip(t *testing.T) {
	t.Run("JSON optimized round trip", func(t *testing.T) {
		codec := NewJSONOptimized()
		original := &JSONTestEntity{
			ID:    12345,
			Name:  "Round Trip Test",
			Email: "test@example.com",
			Score: 99.9,
			Tags:  []string{"optimized", "test"},
		}

		// EncodeTo
		buf := make([]byte, 0, 1024)
		data, err := codec.EncodeTo(buf, original)
		require.NoError(t, err)

		// DecodeWith
		decoded := &JSONTestEntity{}
		err = codec.DecodeWith(data, decoded, nil)
		require.NoError(t, err)

		assert.Equal(t, original.ID, decoded.ID)
		assert.Equal(t, original.Name, decoded.Name)
		assert.Equal(t, original.Email, decoded.Email)
		assert.Equal(t, original.Score, decoded.Score)
		assert.Equal(t, original.Tags, decoded.Tags)
	})

	t.Run("MsgPack optimized round trip", func(t *testing.T) {
		codec := NewMsgPackOptimized()
		original := &MsgPackTestEntity{
			ID:    12345,
			Name:  "Round Trip Test",
			Email: "test@example.com",
			Score: 99.9,
			Tags:  []string{"optimized", "test"},
		}

		// EncodeTo
		buf := make([]byte, 0, 1024)
		data, err := codec.EncodeTo(buf, original)
		require.NoError(t, err)

		// DecodeWith
		decoded := &MsgPackTestEntity{}
		err = codec.DecodeWith(data, decoded, nil)
		require.NoError(t, err)

		assert.Equal(t, original.ID, decoded.ID)
		assert.Equal(t, original.Name, decoded.Name)
		assert.Equal(t, original.Email, decoded.Email)
		assert.Equal(t, original.Score, decoded.Score)
		assert.Equal(t, original.Tags, decoded.Tags)
	})
}

// Benchmark optimized codec methods.
func BenchmarkJSONOptimizedCodec_EncodeTo(b *testing.B) {
	codec := NewJSONOptimized()
	entity := &JSONTestEntity{
		ID:    123,
		Name:  "John Doe",
		Email: "john@example.com",
		Score: 95.5,
		Tags:  []string{"golang", "testing"},
	}
	buf := make([]byte, 0, 1024)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		buf, _ = codec.EncodeTo(buf[:0], entity)
	}
}

func BenchmarkJSONOptimizedCodec_DecodeWith(b *testing.B) {
	codec := NewJSONOptimized()
	entity := &JSONTestEntity{
		ID:    123,
		Name:  "John Doe",
		Email: "john@example.com",
		Score: 95.5,
		Tags:  []string{"golang", "testing"},
	}
	data, _ := codec.Encode(entity)
	scratch := make([]byte, 4096)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		var decoded JSONTestEntity
		_ = codec.DecodeWith(data, &decoded, scratch)
	}
}

func BenchmarkJSONOptimizedCodec_EncodePooled(b *testing.B) {
	codec := NewJSONOptimized()
	entity := &JSONTestEntity{
		ID:    123,
		Name:  "John Doe",
		Email: "john@example.com",
		Score: 95.5,
		Tags:  []string{"golang", "testing"},
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		data, cleanup, _ := codec.EncodePooled(entity)
		cleanup()
		_ = data
	}
}

func BenchmarkMsgPackOptimizedCodec_EncodeTo(b *testing.B) {
	codec := NewMsgPackOptimized()
	entity := &MsgPackTestEntity{
		ID:    123,
		Name:  "John Doe",
		Email: "john@example.com",
		Score: 95.5,
		Tags:  []string{"golang", "testing"},
	}
	buf := make([]byte, 0, 1024)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		buf, _ = codec.EncodeTo(buf[:0], entity)
	}
}

func BenchmarkMsgPackOptimizedCodec_DecodeWith(b *testing.B) {
	codec := NewMsgPackOptimized()
	entity := &MsgPackTestEntity{
		ID:    123,
		Name:  "John Doe",
		Email: "john@example.com",
		Score: 95.5,
		Tags:  []string{"golang", "testing"},
	}
	data, _ := codec.Encode(entity)
	scratch := make([]byte, 4096)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		var decoded MsgPackTestEntity
		_ = codec.DecodeWith(data, &decoded, scratch)
	}
}

func BenchmarkMsgPackOptimizedCodec_EncodePooled(b *testing.B) {
	codec := NewMsgPackOptimized()
	entity := &MsgPackTestEntity{
		ID:    123,
		Name:  "John Doe",
		Email: "john@example.com",
		Score: 95.5,
		Tags:  []string{"golang", "testing"},
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		data, cleanup, _ := codec.EncodePooled(entity)
		cleanup()
		_ = data
	}
}
