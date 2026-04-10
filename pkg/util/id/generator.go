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

package id

import (
	"encoding/binary"
	"sync/atomic"
	"time"

	"github.com/jeremyhahn/go-quicbaas/pkg/util/conv"
)

// IDGenerator defines the interface for ID generation strategies.
// Implementations must be thread-safe and support high-throughput operations.
type IDGenerator interface {
	// Generate creates a new unique ID.
	// Returns an error if ID generation fails.
	Generate() (uint64, error)

	// GenerateFromKey creates a deterministic ID from a given key.
	// Returns the same ID for the same input key.
	GenerateFromKey(key []byte) (uint64, error)
}

// HashGenerator generates IDs using FNV-1a 64-bit hash algorithm.
// It combines a hash of the input with a counter to ensure uniqueness.
type HashGenerator struct {
	counter atomic.Uint64
}

// NewHashGenerator creates a new hash-based ID generator.
// Thread-safe and suitable for high-throughput scenarios.
func NewHashGenerator() *HashGenerator {
	return &HashGenerator{}
}

// Generate creates a new ID by hashing the current timestamp and counter.
// Thread-safe using atomic operations.
func (g *HashGenerator) Generate() (uint64, error) {
	// Use timestamp + counter for uniqueness
	ts := time.Now().UnixNano()
	count := g.counter.Add(1)

	// Combine timestamp and counter
	buf := make([]byte, 16)
	binary.BigEndian.PutUint64(buf[:8], conv.ClampInt64ToUint64(ts))
	binary.BigEndian.PutUint64(buf[8:], count)

	return fnv1a64(buf), nil
}

// GenerateFromKey creates a deterministic ID from the given key using FNV-1a hash.
func (g *HashGenerator) GenerateFromKey(key []byte) (uint64, error) {
	if len(key) == 0 {
		// For empty keys, generate a unique ID
		return g.Generate()
	}
	return fnv1a64(key), nil
}

// TimeSeriesGenerator generates IDs based on Unix microsecond timestamps.
// Suitable for time-series data where temporal ordering is important.
type TimeSeriesGenerator struct {
	lastID atomic.Uint64
}

// NewTimeSeriesGenerator creates a new time-series ID generator.
// IDs are based on Unix microsecond timestamps with collision prevention.
func NewTimeSeriesGenerator() *TimeSeriesGenerator {
	return &TimeSeriesGenerator{}
}

// Generate creates a new ID based on the current Unix microsecond timestamp.
// Ensures monotonically increasing IDs even when called in rapid succession.
func (g *TimeSeriesGenerator) Generate() (uint64, error) {
	for {
		// UnixMicro returns int64, conversion to uint64 for monotonic ID generation
		now := uint64(time.Now().UnixMicro()) //nolint:gosec // G115: time value conversion
		last := g.lastID.Load()

		// Ensure we always produce a larger ID than the last one
		if now <= last {
			now = last + 1
		}

		if g.lastID.CompareAndSwap(last, now) {
			return now, nil
		}
		// CAS failed, retry
	}
}

// GenerateFromKey creates a deterministic timestamp-based ID from the key.
// This is useful for time-series data with known timestamps.
func (g *TimeSeriesGenerator) GenerateFromKey(key []byte) (uint64, error) {
	if len(key) == 0 {
		return g.Generate()
	}

	// Hash the key to create a deterministic but unique ID
	hash := fnv1a64(key)

	// Ensure it's larger than any previously generated ID
	for {
		last := g.lastID.Load()
		if hash <= last {
			hash = last + 1
		}

		if g.lastID.CompareAndSwap(last, hash) {
			return hash, nil
		}
	}
}

// SequentialGenerator generates auto-incrementing IDs using atomic operations.
// Provides simple, sequential IDs starting from 1.
type SequentialGenerator struct {
	counter atomic.Uint64
}

// NewSequentialGenerator creates a new sequential ID generator.
// Thread-safe using atomic operations.
func NewSequentialGenerator() *SequentialGenerator {
	return &SequentialGenerator{}
}

// NewSequentialGeneratorWithStart creates a sequential generator starting from a specific value.
func NewSequentialGeneratorWithStart(start uint64) *SequentialGenerator {
	g := &SequentialGenerator{}
	g.counter.Store(start)
	return g
}

// Generate creates a new sequential ID by incrementing an atomic counter.
func (g *SequentialGenerator) Generate() (uint64, error) {
	return g.counter.Add(1), nil
}

// GenerateFromKey creates a deterministic ID from the key.
// For sequential generator, this hashes the key to produce a deterministic ID.
func (g *SequentialGenerator) GenerateFromKey(key []byte) (uint64, error) {
	if len(key) == 0 {
		return g.Generate()
	}
	// Return hash of the key for deterministic IDs
	return fnv1a64(key), nil
}
