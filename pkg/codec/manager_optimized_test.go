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

func TestCodecManager_EncodeForStorageTo(t *testing.T) {
	mgr, err := NewCodecManager(DefaultConfig())
	require.NoError(t, err)

	t.Run("success with provided buffer", func(t *testing.T) {
		entity := &EntityWithJSONTags{ID: 42, Name: "test"}
		buf := make([]byte, 0, 1024)

		result, err := mgr.EncodeForStorageTo(buf, entity)
		require.NoError(t, err)
		assert.NotEmpty(t, result)

		// Verify buffer was reused (same capacity)
		assert.Equal(t, cap(buf), cap(result))
	})

	t.Run("success with small buffer", func(t *testing.T) {
		entity := &EntityWithJSONTags{ID: 42, Name: "test"}
		buf := make([]byte, 0, 10) // Too small

		result, err := mgr.EncodeForStorageTo(buf, entity)
		require.NoError(t, err)
		assert.NotEmpty(t, result)

		// New buffer allocated (different capacity)
		assert.NotEqual(t, cap(buf), cap(result))
	})

	t.Run("nil entity", func(t *testing.T) {
		buf := make([]byte, 0, 1024)
		result, err := mgr.EncodeForStorageTo(buf, nil)
		assert.Error(t, err)
		assert.Nil(t, result)
		assert.Contains(t, err.Error(), "cannot encode nil entity")
	})
}

func TestCodecManager_DecodeFromStorageWith(t *testing.T) {
	mgr, err := NewCodecManager(DefaultConfig())
	require.NoError(t, err)

	t.Run("success without scratch buffer", func(t *testing.T) {
		original := &EntityWithJSONTags{ID: 100, Name: "scratch-test"}
		data, err := mgr.EncodeForStorage(original)
		require.NoError(t, err)

		decoded := &EntityWithJSONTags{}
		err = mgr.DecodeFromStorageWith(data, decoded, nil)
		require.NoError(t, err)
		assert.Equal(t, original.ID, decoded.ID)
		assert.Equal(t, original.Name, decoded.Name)
	})

	t.Run("empty data", func(t *testing.T) {
		entity := &EntityWithJSONTags{}
		err := mgr.DecodeFromStorageWith([]byte{}, entity, nil)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "cannot decode empty data")
	})

	t.Run("nil entity", func(t *testing.T) {
		data := []byte{1, 2, 3}
		err := mgr.DecodeFromStorageWith(data, nil, nil)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "cannot decode into nil entity")
	})
}

func TestCodecManager_EncodeForStoragePooled(t *testing.T) {
	mgr, err := NewCodecManager(DefaultConfig())
	require.NoError(t, err)

	t.Run("success with cleanup", func(t *testing.T) {
		entity := &EntityWithJSONTags{ID: 42, Name: "pooled"}

		data, cleanup, err := mgr.EncodeForStoragePooled(entity)
		require.NoError(t, err)
		require.NotNil(t, cleanup)
		assert.NotEmpty(t, data)

		// Verify we can decode
		decoded := &EntityWithJSONTags{}
		err = mgr.DecodeFromStorage(data, decoded)
		require.NoError(t, err)
		assert.Equal(t, entity.ID, decoded.ID)

		// Call cleanup
		cleanup()
	})

	t.Run("multiple pooled operations", func(t *testing.T) {
		for i := 0; i < 100; i++ {
			entity := &EntityWithJSONTags{ID: i, Name: "multi-pooled"}

			data, cleanup, err := mgr.EncodeForStoragePooled(entity)
			require.NoError(t, err)
			assert.NotEmpty(t, data)

			// Should be able to use data before cleanup
			decoded := &EntityWithJSONTags{}
			err = mgr.DecodeFromStorage(data, decoded)
			require.NoError(t, err)
			assert.Equal(t, entity.ID, decoded.ID)

			cleanup()
		}
	})

	t.Run("nil entity", func(t *testing.T) {
		data, cleanup, err := mgr.EncodeForStoragePooled(nil)
		assert.Error(t, err)
		assert.Nil(t, data)
		assert.Nil(t, cleanup)
		assert.Contains(t, err.Error(), "cannot encode nil entity")
	})
}

func TestCodecManager_EncodeForWirePooled(t *testing.T) {
	mgr, err := NewCodecManager(DefaultConfig())
	require.NoError(t, err)

	t.Run("success with json codec", func(t *testing.T) {
		entity := &EntityWithJSONTags{ID: 42, Name: "wire-pooled"}

		data, codecName, cleanup, err := mgr.EncodeForWirePooled(entity)
		require.NoError(t, err)
		require.NotNil(t, cleanup)
		assert.NotEmpty(t, data)
		assert.Equal(t, "json", codecName) // Auto-detected

		// Verify we can decode
		decoded := &EntityWithJSONTags{}
		err = mgr.DecodeFromWire(data, decoded, codecName)
		require.NoError(t, err)
		assert.Equal(t, entity.ID, decoded.ID)

		cleanup()
	})

	t.Run("explicit codec selection", func(t *testing.T) {
		entity := &EntityWithExplicitCodec{ID: 99, Name: "explicit"}

		data, codecName, cleanup, err := mgr.EncodeForWirePooled(entity)
		require.NoError(t, err)
		require.NotNil(t, cleanup)
		assert.NotEmpty(t, data)
		assert.Equal(t, "yaml", codecName) // Explicitly specified

		cleanup()
	})

	t.Run("single codec mode", func(t *testing.T) {
		cfg := &Config{
			StorageCodec:       "msgpack",
			DefaultWireCodec:   "json",
			AutoDetect:         true,
			DualCodecMode:      false, // Single codec mode
			UseOptimizedCodecs: true,
		}
		singleMgr, err := NewCodecManager(cfg)
		require.NoError(t, err)

		entity := &EntityWithJSONTags{ID: 50, Name: "single"}

		data, codecName, cleanup, err := singleMgr.EncodeForWirePooled(entity)
		require.NoError(t, err)
		require.NotNil(t, cleanup)
		assert.NotEmpty(t, data)
		assert.Equal(t, "msgpack", codecName) // Uses storage codec in single mode

		cleanup()
	})

	t.Run("nil entity", func(t *testing.T) {
		data, codecName, cleanup, err := mgr.EncodeForWirePooled(nil)
		assert.Error(t, err)
		assert.Nil(t, data)
		assert.Empty(t, codecName)
		assert.Nil(t, cleanup)
		assert.Contains(t, err.Error(), "cannot encode nil entity")
	})
}

func TestCodecManager_OptimizedMethodsConcurrent(t *testing.T) {
	mgr, err := NewCodecManager(DefaultConfig())
	require.NoError(t, err)

	const goroutines = 50
	const iterations = 100

	var wg sync.WaitGroup
	wg.Add(goroutines)

	for i := 0; i < goroutines; i++ {
		go func(id int) {
			defer wg.Done()

			for j := 0; j < iterations; j++ {
				entity := &EntityWithJSONTags{ID: id*iterations + j, Name: "concurrent"}

				// Test EncodeForStorageTo
				buf := make([]byte, 0, 1024)
				buf, err := mgr.EncodeForStorageTo(buf, entity)
				assert.NoError(t, err)

				// Test DecodeFromStorageWith
				decoded := &EntityWithJSONTags{}
				err = mgr.DecodeFromStorageWith(buf, decoded, nil)
				assert.NoError(t, err)
				assert.Equal(t, entity.ID, decoded.ID)

				// Test EncodeForStoragePooled
				data, cleanup, err := mgr.EncodeForStoragePooled(entity)
				assert.NoError(t, err)
				assert.NotEmpty(t, data)
				cleanup()

				// Test EncodeForWirePooled
				wireData, codecName, cleanupWire, err := mgr.EncodeForWirePooled(entity)
				assert.NoError(t, err)
				assert.NotEmpty(t, wireData)
				assert.NotEmpty(t, codecName)
				cleanupWire()
			}
		}(i)
	}

	wg.Wait()
}

func TestCodecManager_OptimizedCodecsDisabled(t *testing.T) {
	// Create manager without optimized codecs
	cfg := &Config{
		StorageCodec:       "msgpack",
		DefaultWireCodec:   "json",
		AutoDetect:         true,
		DualCodecMode:      true,
		UseOptimizedCodecs: false, // Disable optimization
	}
	mgr, err := NewCodecManager(cfg)
	require.NoError(t, err)

	entity := &EntityWithJSONTags{ID: 42, Name: "no-opt"}

	t.Run("EncodeForStorageTo falls back to standard", func(t *testing.T) {
		buf := make([]byte, 0, 1024)
		result, err := mgr.EncodeForStorageTo(buf, entity)
		require.NoError(t, err)
		assert.NotEmpty(t, result)
		// Falls back to standard encode (new allocation, different buffer)
	})

	t.Run("DecodeFromStorageWith falls back to standard", func(t *testing.T) {
		data, err := mgr.EncodeForStorage(entity)
		require.NoError(t, err)

		decoded := &EntityWithJSONTags{}
		err = mgr.DecodeFromStorageWith(data, decoded, nil)
		require.NoError(t, err)
		assert.Equal(t, entity.ID, decoded.ID)
	})

	t.Run("EncodeForStoragePooled returns noop cleanup", func(t *testing.T) {
		data, cleanup, err := mgr.EncodeForStoragePooled(entity)
		require.NoError(t, err)
		require.NotNil(t, cleanup)
		assert.NotEmpty(t, data)

		// Cleanup should be noop but safe to call
		cleanup()
	})

	t.Run("EncodeForWirePooled returns noop cleanup", func(t *testing.T) {
		data, codecName, cleanup, err := mgr.EncodeForWirePooled(entity)
		require.NoError(t, err)
		require.NotNil(t, cleanup)
		assert.NotEmpty(t, data)
		assert.Equal(t, "json", codecName)

		// Cleanup should be noop but safe to call
		cleanup()
	})
}
