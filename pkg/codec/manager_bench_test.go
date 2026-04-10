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

	"github.com/stretchr/testify/require"
)

// BenchmarkCodecManager_DualCodecRoundTripOptimized benchmarks the optimized
// dual-codec round-trip using pooled buffers and optimized encode/decode methods.
// This should show significant allocation reduction compared to standard methods.
func BenchmarkCodecManager_DualCodecRoundTripOptimized(b *testing.B) {
	mgr, err := NewCodecManager(DefaultConfig())
	require.NoError(b, err)

	entity := &EntityWithJSONTags{ID: 1, Name: "benchmark"}

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		// Storage encode with pooled buffer
		storageData, cleanupStorage, err := mgr.EncodeForStoragePooled(entity)
		if err != nil {
			b.Fatal(err)
		}

		// Decode from storage with scratch buffer
		fromStorage := &EntityWithJSONTags{}
		err = mgr.DecodeFromStorageWith(storageData, fromStorage, nil)
		if err != nil {
			cleanupStorage()
			b.Fatal(err)
		}

		// Wire encode with pooled buffer (auto-detected as json)
		wireData, _, cleanupWire, err := mgr.EncodeForWirePooled(fromStorage)
		if err != nil {
			cleanupStorage()
			b.Fatal(err)
		}

		// Client decodes (wire decode doesn't have optimized version yet,
		// but encode side is optimized which is the hot path)
		clientEntity := &EntityWithJSONTags{}
		err = mgr.DecodeFromWire(wireData, clientEntity, "json")
		if err != nil {
			cleanupStorage()
			cleanupWire()
			b.Fatal(err)
		}

		// Clean up pooled buffers
		cleanupStorage()
		cleanupWire()
	}
}

// BenchmarkCodecManager_EncodeForStoragePooled benchmarks storage encoding with pooled buffers.
func BenchmarkCodecManager_EncodeForStoragePooled(b *testing.B) {
	mgr, err := NewCodecManager(DefaultConfig())
	require.NoError(b, err)

	entity := &EntityWithJSONTags{ID: 1, Name: "benchmark"}

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		data, cleanup, err := mgr.EncodeForStoragePooled(entity)
		if err != nil {
			b.Fatal(err)
		}
		_ = data
		cleanup()
	}
}

// BenchmarkCodecManager_EncodeForStorageTo benchmarks storage encoding with buffer reuse.
func BenchmarkCodecManager_EncodeForStorageTo(b *testing.B) {
	mgr, err := NewCodecManager(DefaultConfig())
	require.NoError(b, err)

	entity := &EntityWithJSONTags{ID: 1, Name: "benchmark"}
	buf := make([]byte, 0, 1024) // Reusable buffer

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		data, err := mgr.EncodeForStorageTo(buf[:0], entity)
		if err != nil {
			b.Fatal(err)
		}
		buf = data // Keep the buffer for next iteration
	}
}

// BenchmarkCodecManager_DecodeFromStorageWith benchmarks storage decoding with scratch buffer.
func BenchmarkCodecManager_DecodeFromStorageWith(b *testing.B) {
	mgr, err := NewCodecManager(DefaultConfig())
	require.NoError(b, err)

	entity := &EntityWithJSONTags{ID: 1, Name: "benchmark"}
	data, err := mgr.EncodeForStorage(entity)
	require.NoError(b, err)

	scratch := make([]byte, 4096) // Reusable scratch buffer

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		decoded := &EntityWithJSONTags{}
		err := mgr.DecodeFromStorageWith(data, decoded, scratch)
		if err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkCodecManager_EncodeForWirePooled benchmarks wire encoding with pooled buffers.
func BenchmarkCodecManager_EncodeForWirePooled(b *testing.B) {
	mgr, err := NewCodecManager(DefaultConfig())
	require.NoError(b, err)

	entity := &EntityWithJSONTags{ID: 1, Name: "benchmark"}

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		data, codecName, cleanup, err := mgr.EncodeForWirePooled(entity)
		if err != nil {
			b.Fatal(err)
		}
		_ = data
		_ = codecName
		cleanup()
	}
}

// Benchmark comparison: Standard vs Optimized

func BenchmarkComparison_Storage_Standard(b *testing.B) {
	mgr, err := NewCodecManager(DefaultConfig())
	require.NoError(b, err)

	entity := &EntityWithJSONTags{ID: 1, Name: "benchmark"}

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		// Standard encode
		data, err := mgr.EncodeForStorage(entity)
		if err != nil {
			b.Fatal(err)
		}

		// Standard decode
		decoded := &EntityWithJSONTags{}
		err = mgr.DecodeFromStorage(data, decoded)
		if err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkComparison_Storage_Optimized(b *testing.B) {
	mgr, err := NewCodecManager(DefaultConfig())
	require.NoError(b, err)

	entity := &EntityWithJSONTags{ID: 1, Name: "benchmark"}

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		// Optimized pooled encode
		data, cleanup, err := mgr.EncodeForStoragePooled(entity)
		if err != nil {
			b.Fatal(err)
		}

		// Optimized decode with scratch buffer
		decoded := &EntityWithJSONTags{}
		err = mgr.DecodeFromStorageWith(data, decoded, nil)
		if err != nil {
			cleanup()
			b.Fatal(err)
		}

		cleanup()
	}
}

func BenchmarkComparison_Wire_Standard(b *testing.B) {
	mgr, err := NewCodecManager(DefaultConfig())
	require.NoError(b, err)

	entity := &EntityWithJSONTags{ID: 1, Name: "benchmark"}

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		// Standard wire encode
		data, codecName, err := mgr.EncodeForWire(entity)
		if err != nil {
			b.Fatal(err)
		}

		// Standard wire decode
		decoded := &EntityWithJSONTags{}
		err = mgr.DecodeFromWire(data, decoded, codecName)
		if err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkComparison_Wire_Optimized(b *testing.B) {
	mgr, err := NewCodecManager(DefaultConfig())
	require.NoError(b, err)

	entity := &EntityWithJSONTags{ID: 1, Name: "benchmark"}

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		// Optimized pooled wire encode
		data, codecName, cleanup, err := mgr.EncodeForWirePooled(entity)
		if err != nil {
			b.Fatal(err)
		}

		// Standard wire decode (optimization here would require changes to entity interface)
		decoded := &EntityWithJSONTags{}
		err = mgr.DecodeFromWire(data, decoded, codecName)
		if err != nil {
			cleanup()
			b.Fatal(err)
		}

		cleanup()
	}
}
