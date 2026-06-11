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

package iam

import (
	"hash/fnv"
	"sync"
)

// BloomFilter implements a simple bloom filter for quick deny checks.
type BloomFilter struct {
	bits []uint64
	size uint64
	k    uint32 // number of hash functions
	mu   sync.RWMutex
}

// NewBloomFilter creates a new bloom filter.
// size: number of bits in the filter
// k: number of hash functions
func NewBloomFilter(size uint64, k uint32) *BloomFilter {
	return &BloomFilter{
		bits: make([]uint64, (size+63)/64),
		size: size,
		k:    k,
	}
}

// Add adds an item to the bloom filter.
func (b *BloomFilter) Add(data []byte) {
	b.mu.Lock()
	defer b.mu.Unlock()

	for i := uint32(0); i < b.k; i++ {
		hash := b.hash(data, i)
		idx := hash / 64
		bit := hash % 64
		b.bits[idx] |= 1 << bit
	}
}

// Contains checks if an item might be in the bloom filter.
// Returns false if definitely not in the set.
// Returns true if possibly in the set (may have false positives).
func (b *BloomFilter) Contains(data []byte) bool {
	b.mu.RLock()
	defer b.mu.RUnlock()

	for i := uint32(0); i < b.k; i++ {
		hash := b.hash(data, i)
		idx := hash / 64
		bit := hash % 64
		if b.bits[idx]&(1<<bit) == 0 {
			return false
		}
	}
	return true
}

// Clear resets the bloom filter.
func (b *BloomFilter) Clear() {
	b.mu.Lock()
	defer b.mu.Unlock()

	for i := range b.bits {
		b.bits[i] = 0
	}
}

// hash computes a hash value for the data using the i-th hash function.
func (b *BloomFilter) hash(data []byte, i uint32) uint64 {
	h := fnv.New64a()
	h.Write(data)                                                        //nolint:gosec // G104: hash.Write never returns error
	h.Write([]byte{byte(i), byte(i >> 8), byte(i >> 16), byte(i >> 24)}) //nolint:gosec // G104: hash.Write never returns error
	return h.Sum64() % b.size
}
