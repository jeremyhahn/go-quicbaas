//go:build vector && qrdb_vector

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

package embedding

import (
	"context"
	"testing"
)

// These benchmarks measure the *provider's own* overhead -- request encoding,
// batch splitting, response decoding and validation -- against a loopback
// sidecar. They deliberately exclude the device: real inference runs at about
// 264 embeddings/s on the NPU, so the provider's contribution should be
// negligible beside it. The property being watched is that the provider never
// becomes the bottleneck in front of a 264/s backend.

func BenchmarkNPUEmbedSingle(b *testing.B) {
	srv := startSidecar(b, newRecordingSidecar(b))
	p := newTestProvider(b, srv, nil)

	ctx := context.Background()
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		if _, err := p.Embed(ctx, "a representative sentence for embedding"); err != nil {
			b.Fatalf("Embed: %v", err)
		}
	}
}

func BenchmarkNPUEmbedBatch8(b *testing.B) {
	benchmarkBatch(b, 8, DefaultNPUMaxBatchSize)
}

func BenchmarkNPUEmbedBatch64(b *testing.B) {
	benchmarkBatch(b, 64, DefaultNPUMaxBatchSize)
}

// BenchmarkNPUEmbedBatch256Split8 measures the request-splitting path: a batch
// well above MaxBatchSize, which the provider must divide across several
// requests and reassemble in order.
func BenchmarkNPUEmbedBatch256Split8(b *testing.B) {
	benchmarkBatch(b, 256, 8)
}

func benchmarkBatch(b *testing.B, size, maxBatch int) {
	b.Helper()

	srv := startSidecar(b, newRecordingSidecar(b))
	p := newTestProvider(b, srv, func(c *NPUConfig) { c.MaxBatchSize = maxBatch })

	texts := make([]string, size)
	for i := range texts {
		texts[i] = "a representative sentence for embedding"
	}

	ctx := context.Background()
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		if _, err := p.EmbedBatch(ctx, texts); err != nil {
			b.Fatalf("EmbedBatch: %v", err)
		}
	}
}
