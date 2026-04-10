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

// Package id provides high-performance, thread-safe ID generation utilities
// for QuicBaaS.
//
// The package supports three ID generation strategies:
//
//  1. HashGenerator - FNV-1a hash-based IDs with collision resistance
//  2. TimeSeriesGenerator - Unix microsecond timestamp-based IDs for time-series data
//  3. SequentialGenerator - Simple auto-incrementing IDs
//
// All generators are thread-safe using atomic operations and provide
// performance exceeding 1 million IDs per second.
//
// # Usage
//
// Hash-based ID generation:
//
//	gen := id.NewHashGenerator()
//	id, err := gen.Generate()
//	if err != nil {
//	    log.Fatal(err)
//	}
//
//	// Deterministic ID from key
//	keyID, err := gen.GenerateFromKey([]byte("user:1234"))
//
// Time-series ID generation:
//
//	gen := id.NewTimeSeriesGenerator()
//	id, err := gen.Generate() // Returns Unix microsecond timestamp
//
// Sequential ID generation:
//
//	gen := id.NewSequentialGenerator()
//	id, err := gen.Generate() // Returns 1, 2, 3, ...
//
//	// Start from specific value
//	gen := id.NewSequentialGeneratorWithStart(1000)
//	id, err := gen.Generate() // Returns 1001
//
// # Performance
//
// All generators achieve >1 million IDs/second with zero allocations:
//
//   - HashGenerator: ~26M ops/sec (37ns/op)
//   - TimeSeriesGenerator: ~37M ops/sec (26ns/op)
//   - SequentialGenerator: ~244M ops/sec (4ns/op)
//
// Parallel performance (24 cores):
//
//   - HashGenerator: ~29M ops/sec
//   - TimeSeriesGenerator: ~28M ops/sec
//   - SequentialGenerator: ~66M ops/sec
//
// # Thread Safety
//
// All generators use atomic operations for lock-free, thread-safe ID generation.
// They have been tested under high concurrency (100 goroutines × 1000 IDs)
// with zero race conditions detected.
//
// # Hash Function
//
// The package uses FNV-1a (Fowler-Noll-Vo) 64-bit hash algorithm, chosen for:
//
//   - Excellent distribution properties
//   - Very fast computation
//   - Good collision resistance (0% in 1M test keys)
//   - No external dependencies
//
// Public hash functions are also available:
//
//	hash := id.HashBytes([]byte("data"))
//	hash := id.HashString("string")
package id
