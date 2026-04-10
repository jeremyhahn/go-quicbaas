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

// BenchmarkBatchEncoding_Standard benchmarks batch encoding without buffer reuse.
func BenchmarkBatchEncoding_Standard(b *testing.B) {
	mgr, err := NewCodecManager(DefaultConfig())
	require.NoError(b, err)

	// Create batch of entities
	entities := make([]*EntityWithJSONTags, 100)
	for i := 0; i < 100; i++ {
		entities[i] = &EntityWithJSONTags{ID: i, Name: "benchmark"}
	}

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		// Encode each entity (allocates new buffer each time)
		for _, entity := range entities {
			_, err := mgr.EncodeForStorage(entity)
			if err != nil {
				b.Fatal(err)
			}
		}
	}
}

// BenchmarkBatchEncoding_Optimized benchmarks batch encoding WITH buffer reuse.
func BenchmarkBatchEncoding_Optimized(b *testing.B) {
	mgr, err := NewCodecManager(DefaultConfig())
	require.NoError(b, err)

	// Create batch of entities
	entities := make([]*EntityWithJSONTags, 100)
	for i := 0; i < 100; i++ {
		entities[i] = &EntityWithJSONTags{ID: i, Name: "benchmark"}
	}

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		// Reusable buffer for all encodings
		buf := make([]byte, 0, 1024)

		// Encode each entity (reuses buffer)
		for _, entity := range entities {
			buf, err = mgr.EncodeForStorageTo(buf[:0], entity)
			if err != nil {
				b.Fatal(err)
			}
		}
	}
}

// BenchmarkBatchDecoding_Standard benchmarks batch decoding without scratch buffer.
func BenchmarkBatchDecoding_Standard(b *testing.B) {
	mgr, err := NewCodecManager(DefaultConfig())
	require.NoError(b, err)

	// Pre-encode batch of entities
	encoded := make([][]byte, 100)
	for i := 0; i < 100; i++ {
		entity := &EntityWithJSONTags{ID: i, Name: "benchmark"}
		data, err := mgr.EncodeForStorage(entity)
		require.NoError(b, err)
		encoded[i] = data
	}

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		for _, data := range encoded {
			decoded := &EntityWithJSONTags{}
			err := mgr.DecodeFromStorage(data, decoded)
			if err != nil {
				b.Fatal(err)
			}
		}
	}
}

// BenchmarkBatchDecoding_Optimized benchmarks batch decoding WITH scratch buffer.
func BenchmarkBatchDecoding_Optimized(b *testing.B) {
	mgr, err := NewCodecManager(DefaultConfig())
	require.NoError(b, err)

	// Pre-encode batch of entities
	encoded := make([][]byte, 100)
	for i := 0; i < 100; i++ {
		entity := &EntityWithJSONTags{ID: i, Name: "benchmark"}
		data, err := mgr.EncodeForStorage(entity)
		require.NoError(b, err)
		encoded[i] = data
	}

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		// Reusable scratch buffer
		scratch := make([]byte, 4096)

		for _, data := range encoded {
			decoded := &EntityWithJSONTags{}
			err := mgr.DecodeFromStorageWith(data, decoded, scratch)
			if err != nil {
				b.Fatal(err)
			}
		}
	}
}

// BenchmarkSingleOperation_EncodeTo shows the best case for buffer reuse.
func BenchmarkSingleOperation_EncodeTo(b *testing.B) {
	mgr, err := NewCodecManager(DefaultConfig())
	require.NoError(b, err)

	entity := &EntityWithJSONTags{ID: 1, Name: "benchmark"}
	buf := make([]byte, 0, 1024)

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		buf, err = mgr.EncodeForStorageTo(buf[:0], entity)
		if err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkSingleOperation_EncodeStandard for comparison.
func BenchmarkSingleOperation_EncodeStandard(b *testing.B) {
	mgr, err := NewCodecManager(DefaultConfig())
	require.NoError(b, err)

	entity := &EntityWithJSONTags{ID: 1, Name: "benchmark"}

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		_, err := mgr.EncodeForStorage(entity)
		if err != nil {
			b.Fatal(err)
		}
	}
}
