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

// Test entities for codec manager testing

// EntityWithJSONTags has JSON tags, should auto-detect to json codec.
type EntityWithJSONTags struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
}

// EntityWithYAMLTags has YAML tags, should auto-detect to yaml codec.
type EntityWithYAMLTags struct {
	Key   string `yaml:"key"`
	Value string `yaml:"value"`
}

// EntityWithMsgPackTags has msgpack tags, should auto-detect to msgpack codec.
type EntityWithMsgPackTags struct {
	Data []byte `msgpack:"data"`
	Size int    `msgpack:"size"`
}

// EntityWithExplicitCodec has explicit quicbaas:"codec=yaml" tag.
type EntityWithExplicitCodec struct {
	ID   int    `quicbaas:"codec=yaml" json:"id"`
	Name string `json:"name"`
}

// EntityWithNoTags has no codec-related tags.
type EntityWithNoTags struct {
	Field1 string
	Field2 int
}

// EntityWithMixedTags has multiple tag types (protobuf should win).
type EntityWithMixedTags struct {
	ID   int    `protobuf:"varint,1,opt,name=id" json:"id"`
	Name string `protobuf:"bytes,2,opt,name=name" json:"name"`
}

func TestNewCodecManager(t *testing.T) {
	t.Run("default config", func(t *testing.T) {
		cfg := DefaultConfig()
		require.NotNil(t, cfg)
		assert.Equal(t, "msgpack", cfg.StorageCodec)
		assert.Equal(t, "json", cfg.DefaultWireCodec)
		assert.True(t, cfg.AutoDetect)
		assert.True(t, cfg.DualCodecMode)

		mgr, err := NewCodecManager(cfg)
		require.NoError(t, err)
		require.NotNil(t, mgr)
		assert.Equal(t, "msgpack", mgr.GetStorageCodecName())
		assert.Equal(t, "json", mgr.GetDefaultWireCodecName())
		assert.True(t, mgr.IsDualCodecMode())
		assert.True(t, mgr.IsAutoDetectEnabled())
	})

	t.Run("nil config uses defaults", func(t *testing.T) {
		mgr, err := NewCodecManager(nil)
		require.NoError(t, err)
		require.NotNil(t, mgr)
		assert.Equal(t, "msgpack", mgr.GetStorageCodecName())
		assert.Equal(t, "json", mgr.GetDefaultWireCodecName())
	})

	t.Run("custom config", func(t *testing.T) {
		cfg := &Config{
			StorageCodec:     "json",
			DefaultWireCodec: "msgpack",
			AutoDetect:       false,
			DualCodecMode:    false,
		}

		mgr, err := NewCodecManager(cfg)
		require.NoError(t, err)
		require.NotNil(t, mgr)
		assert.Equal(t, "json", mgr.GetStorageCodecName())
		assert.Equal(t, "msgpack", mgr.GetDefaultWireCodecName())
		assert.False(t, mgr.IsDualCodecMode())
		assert.False(t, mgr.IsAutoDetectEnabled())
	})

	t.Run("invalid storage codec", func(t *testing.T) {
		cfg := &Config{
			StorageCodec:     "invalid",
			DefaultWireCodec: "json",
			AutoDetect:       true,
			DualCodecMode:    true,
		}

		mgr, err := NewCodecManager(cfg)
		assert.Error(t, err)
		assert.Nil(t, mgr)
		assert.Contains(t, err.Error(), "codec invalid not found in registry")
	})

	t.Run("invalid wire codec", func(t *testing.T) {
		cfg := &Config{
			StorageCodec:     "msgpack",
			DefaultWireCodec: "invalid",
			AutoDetect:       true,
			DualCodecMode:    true,
		}

		mgr, err := NewCodecManager(cfg)
		assert.Error(t, err)
		assert.Nil(t, mgr)
		assert.Contains(t, err.Error(), "codec invalid not found in registry")
	})
}

// TestCodecManager_StorageErrors covers the storage error paths; the storage
// success path is covered by TestCodecManager_DualCodecRoundTrip.
func TestCodecManager_StorageErrors(t *testing.T) {
	mgr, err := NewCodecManager(DefaultConfig())
	require.NoError(t, err)

	t.Run("encode nil entity", func(t *testing.T) {
		data, err := mgr.EncodeForStorage(nil)
		assert.Error(t, err)
		assert.Nil(t, data)
		assert.Contains(t, err.Error(), "cannot encode nil entity")
	})

	t.Run("decode empty data", func(t *testing.T) {
		entity := &EntityWithJSONTags{}
		err := mgr.DecodeFromStorage([]byte{}, entity)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "cannot decode empty data")
	})

	t.Run("decode nil entity", func(t *testing.T) {
		err := mgr.DecodeFromStorage([]byte{1, 2, 3}, nil)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "cannot decode into nil entity")
	})
}

func TestCodecManager_EncodeForWire_DualMode(t *testing.T) {
	cfg := DefaultConfig()
	cfg.DualCodecMode = true
	cfg.AutoDetect = true
	mgr, err := NewCodecManager(cfg)
	require.NoError(t, err)

	t.Run("explicit codec tag", func(t *testing.T) {
		entity := &EntityWithExplicitCodec{ID: 1, Name: "explicit"}
		data, codecName, err := mgr.EncodeForWire(entity)
		require.NoError(t, err)
		assert.NotEmpty(t, data)
		assert.Equal(t, "yaml", codecName) // Should use explicit tag
	})

	t.Run("auto-detect json", func(t *testing.T) {
		entity := &EntityWithJSONTags{ID: 2, Name: "json"}
		data, codecName, err := mgr.EncodeForWire(entity)
		require.NoError(t, err)
		assert.NotEmpty(t, data)
		assert.Equal(t, "json", codecName) // Should auto-detect from json tags
	})

	t.Run("auto-detect yaml", func(t *testing.T) {
		entity := &EntityWithYAMLTags{Key: "k", Value: "v"}
		data, codecName, err := mgr.EncodeForWire(entity)
		require.NoError(t, err)
		assert.NotEmpty(t, data)
		assert.Equal(t, "yaml", codecName) // Should auto-detect from yaml tags
	})

	t.Run("auto-detect msgpack", func(t *testing.T) {
		entity := &EntityWithMsgPackTags{Data: []byte{1, 2, 3}, Size: 3}
		data, codecName, err := mgr.EncodeForWire(entity)
		require.NoError(t, err)
		assert.NotEmpty(t, data)
		assert.Equal(t, "msgpack", codecName) // Should auto-detect from msgpack tags
	})

	t.Run("auto-detect protobuf priority", func(t *testing.T) {
		entity := &EntityWithMixedTags{ID: 3, Name: "mixed"}
		data, codecName, err := mgr.EncodeForWire(entity)
		require.NoError(t, err)
		assert.NotEmpty(t, data)
		assert.Equal(t, "protobuf", codecName) // Protobuf should have highest priority
	})

	t.Run("fallback to default", func(t *testing.T) {
		entity := &EntityWithNoTags{Field1: "test", Field2: 42}
		data, codecName, err := mgr.EncodeForWire(entity)
		require.NoError(t, err)
		assert.NotEmpty(t, data)
		assert.Equal(t, "json", codecName) // Should fallback to default wire codec
	})

	t.Run("non-struct falls back to default", func(t *testing.T) {
		data, codecName, err := mgr.EncodeForWire(42)
		require.NoError(t, err)
		assert.NotEmpty(t, data)
		assert.Equal(t, "json", codecName)
	})

	t.Run("nil entity", func(t *testing.T) {
		data, codecName, err := mgr.EncodeForWire(nil)
		assert.Error(t, err)
		assert.Nil(t, data)
		assert.Empty(t, codecName)
		assert.Contains(t, err.Error(), "cannot encode nil entity")
	})
}

func TestCodecManager_EncodeForWire_Modes(t *testing.T) {
	tests := []struct {
		name          string
		dualCodecMode bool
		autoDetect    bool
		entity        interface{}
		wantCodec     string
	}{
		{
			name:          "single mode uses storage codec",
			dualCodecMode: false,
			autoDetect:    true,
			entity:        &EntityWithJSONTags{ID: 1, Name: "single"},
			wantCodec:     "msgpack",
		},
		{
			name:          "no auto-detect keeps explicit tag",
			dualCodecMode: true,
			autoDetect:    false,
			entity:        &EntityWithExplicitCodec{ID: 1, Name: "explicit"},
			wantCodec:     "yaml",
		},
		{
			name:          "no auto-detect ignores struct tags",
			dualCodecMode: true,
			autoDetect:    false,
			entity:        &EntityWithYAMLTags{Key: "k", Value: "v"},
			wantCodec:     "json",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := DefaultConfig()
			cfg.DualCodecMode = tt.dualCodecMode
			cfg.AutoDetect = tt.autoDetect
			mgr, err := NewCodecManager(cfg)
			require.NoError(t, err)

			data, codecName, err := mgr.EncodeForWire(tt.entity)
			require.NoError(t, err)
			assert.NotEmpty(t, data)
			assert.Equal(t, tt.wantCodec, codecName)
		})
	}

	t.Run("single mode wire data decodes from storage", func(t *testing.T) {
		cfg := DefaultConfig()
		cfg.DualCodecMode = false
		mgr, err := NewCodecManager(cfg)
		require.NoError(t, err)

		entity := &EntityWithJSONTags{ID: 1, Name: "single"}
		data, _, err := mgr.EncodeForWire(entity)
		require.NoError(t, err)

		decoded := &EntityWithJSONTags{}
		require.NoError(t, mgr.DecodeFromStorage(data, decoded))
		assert.Equal(t, entity.ID, decoded.ID)
		assert.Equal(t, entity.Name, decoded.Name)
	})
}

func TestCodecManager_DecodeFromWire(t *testing.T) {
	mgr, err := NewCodecManager(DefaultConfig())
	require.NoError(t, err)

	t.Run("success with json", func(t *testing.T) {
		original := &EntityWithJSONTags{ID: 1, Name: "wire"}
		jsonCodec := NewJSONCodec()
		data, err := jsonCodec.Encode(original)
		require.NoError(t, err)

		decoded := &EntityWithJSONTags{}
		err = mgr.DecodeFromWire(data, decoded, "json")
		require.NoError(t, err)
		assert.Equal(t, original.ID, decoded.ID)
		assert.Equal(t, original.Name, decoded.Name)
	})

	t.Run("success with msgpack", func(t *testing.T) {
		original := &EntityWithMsgPackTags{Data: []byte{1, 2}, Size: 2}
		msgpackCodec := NewMsgPackCodec()
		data, err := msgpackCodec.Encode(original)
		require.NoError(t, err)

		decoded := &EntityWithMsgPackTags{}
		err = mgr.DecodeFromWire(data, decoded, "msgpack")
		require.NoError(t, err)
		assert.Equal(t, original.Data, decoded.Data)
		assert.Equal(t, original.Size, decoded.Size)
	})

	t.Run("empty data", func(t *testing.T) {
		entity := &EntityWithJSONTags{}
		err := mgr.DecodeFromWire([]byte{}, entity, "json")
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "cannot decode empty data")
	})

	t.Run("nil entity", func(t *testing.T) {
		err := mgr.DecodeFromWire([]byte{1, 2}, nil, "json")
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "cannot decode into nil entity")
	})

	t.Run("empty codec name", func(t *testing.T) {
		entity := &EntityWithJSONTags{}
		err := mgr.DecodeFromWire([]byte{1, 2}, entity, "")
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "codec name is required")
	})

	t.Run("unknown codec", func(t *testing.T) {
		entity := &EntityWithJSONTags{}
		err := mgr.DecodeFromWire([]byte{1, 2}, entity, "unknown")
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "codec unknown not found in registry")
	})
}

func TestCodecManager_DualCodecRoundTrip(t *testing.T) {
	mgr, err := NewCodecManager(DefaultConfig())
	require.NoError(t, err)

	t.Run("storage msgpack, wire json", func(t *testing.T) {
		original := &EntityWithJSONTags{ID: 100, Name: "dual-codec"}

		// Store with msgpack
		storageData, err := mgr.EncodeForStorage(original)
		require.NoError(t, err)

		// Retrieve from storage
		fromStorage := &EntityWithJSONTags{}
		err = mgr.DecodeFromStorage(storageData, fromStorage)
		require.NoError(t, err)
		assert.Equal(t, original.ID, fromStorage.ID)
		assert.Equal(t, original.Name, fromStorage.Name)

		// Send over wire with json (auto-detected)
		wireData, codecName, err := mgr.EncodeForWire(fromStorage)
		require.NoError(t, err)
		assert.Equal(t, "json", codecName)

		// Client receives and decodes
		clientEntity := &EntityWithJSONTags{}
		err = mgr.DecodeFromWire(wireData, clientEntity, codecName)
		require.NoError(t, err)
		assert.Equal(t, original.ID, clientEntity.ID)
		assert.Equal(t, original.Name, clientEntity.Name)

		// Verify storage and wire data are different (different codecs)
		assert.NotEqual(t, storageData, wireData)
	})
}

func TestCodecManager_ConcurrentAccess(t *testing.T) {
	mgr, err := NewCodecManager(DefaultConfig())
	require.NoError(t, err)

	const goroutines = 50
	const iterations = 100

	var wg sync.WaitGroup
	wg.Add(goroutines)

	// Concurrent encodes and decodes
	for i := 0; i < goroutines; i++ {
		go func(id int) {
			defer wg.Done()

			for j := 0; j < iterations; j++ {
				entity := &EntityWithJSONTags{ID: id*iterations + j, Name: "concurrent"}

				// Storage operations
				data, err := mgr.EncodeForStorage(entity)
				assert.NoError(t, err)

				decoded := &EntityWithJSONTags{}
				err = mgr.DecodeFromStorage(data, decoded)
				assert.NoError(t, err)
				assert.Equal(t, entity.ID, decoded.ID)

				// Wire operations
				wireData, codecName, err := mgr.EncodeForWire(entity)
				assert.NoError(t, err)
				assert.NotEmpty(t, codecName)

				wireDecoded := &EntityWithJSONTags{}
				err = mgr.DecodeFromWire(wireData, wireDecoded, codecName)
				assert.NoError(t, err)
				assert.Equal(t, entity.ID, wireDecoded.ID)

				// Read-only operations
				_ = mgr.GetStorageCodecName()
				_ = mgr.GetDefaultWireCodecName()
				_ = mgr.ListCodecs()
				_ = mgr.IsDualCodecMode()
				_ = mgr.IsAutoDetectEnabled()
			}
		}(i)
	}

	wg.Wait()
}

func TestCodecManager_ListCodecs(t *testing.T) {
	mgr, err := NewCodecManager(DefaultConfig())
	require.NoError(t, err)

	codecs := mgr.ListCodecs()
	assert.NotEmpty(t, codecs)
	assert.Contains(t, codecs, "json")
	assert.Contains(t, codecs, "msgpack")
	assert.Contains(t, codecs, "yaml")
	assert.Contains(t, codecs, "protobuf")
	assert.Len(t, codecs, 4) // Should have all 4 standard codecs
}

// Benchmark tests

func BenchmarkCodecManager_EncodeForStorage(b *testing.B) {
	mgr, err := NewCodecManager(DefaultConfig())
	require.NoError(b, err)

	entity := &EntityWithJSONTags{ID: 1, Name: "benchmark"}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := mgr.EncodeForStorage(entity)
		if err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkCodecManager_DecodeFromStorage(b *testing.B) {
	mgr, err := NewCodecManager(DefaultConfig())
	require.NoError(b, err)

	entity := &EntityWithJSONTags{ID: 1, Name: "benchmark"}
	data, err := mgr.EncodeForStorage(entity)
	require.NoError(b, err)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		decoded := &EntityWithJSONTags{}
		err := mgr.DecodeFromStorage(data, decoded)
		if err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkCodecManager_EncodeForWire(b *testing.B) {
	mgr, err := NewCodecManager(DefaultConfig())
	require.NoError(b, err)

	entity := &EntityWithJSONTags{ID: 1, Name: "benchmark"}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _, err := mgr.EncodeForWire(entity)
		if err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkCodecManager_DecodeFromWire(b *testing.B) {
	mgr, err := NewCodecManager(DefaultConfig())
	require.NoError(b, err)

	entity := &EntityWithJSONTags{ID: 1, Name: "benchmark"}
	data, codecName, err := mgr.EncodeForWire(entity)
	require.NoError(b, err)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		decoded := &EntityWithJSONTags{}
		err := mgr.DecodeFromWire(data, decoded, codecName)
		if err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkCodecManager_DualCodecRoundTrip(b *testing.B) {
	mgr, err := NewCodecManager(DefaultConfig())
	require.NoError(b, err)

	entity := &EntityWithJSONTags{ID: 1, Name: "benchmark"}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		// Storage encode/decode
		storageData, err := mgr.EncodeForStorage(entity)
		if err != nil {
			b.Fatal(err)
		}

		fromStorage := &EntityWithJSONTags{}
		err = mgr.DecodeFromStorage(storageData, fromStorage)
		if err != nil {
			b.Fatal(err)
		}

		// Wire encode/decode
		wireData, codecName, err := mgr.EncodeForWire(fromStorage)
		if err != nil {
			b.Fatal(err)
		}

		clientEntity := &EntityWithJSONTags{}
		err = mgr.DecodeFromWire(wireData, clientEntity, codecName)
		if err != nil {
			b.Fatal(err)
		}
	}
}
