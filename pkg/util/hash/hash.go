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

// Package hash provides high-performance hash functions for consistent hashing
// and general-purpose hashing operations.
package hash

import (
	"encoding/binary"
	"hash/fnv"

	"github.com/minio/blake2b-simd"
)

// HashFunc defines the signature for hash functions used in consistent hashing.
// It takes a key and returns a 64-bit hash value.
type HashFunc func(key string) uint64

// Blake2b is a cryptographic-quality hash function optimized for speed.
// It provides strong collision resistance and is suitable for security-sensitive
// applications. Performance: ~3GB/s on modern hardware.
//
// Use this when:
//   - Cryptographic properties are desired
//   - Strong collision resistance is needed
//   - Security is a concern
func Blake2b(key string) uint64 {
	out := blake2b.Sum512([]byte(key))
	return binary.LittleEndian.Uint64(out[:8])
}

// FNV1a is a non-cryptographic hash function optimized for pure speed.
// It provides good distribution but is not suitable for security applications.
// Performance: ~5GB/s on modern hardware, ~40% faster than Blake2b.
//
// Use this when:
//   - Maximum performance is required
//   - Security properties are not needed
//   - Load balancing/distribution is the primary goal
func FNV1a(key string) uint64 {
	h := fnv.New64a()
	_, _ = h.Write([]byte(key)) //nolint:gosec // G104: hash.Hash.Write never returns error
	return h.Sum64()
}

// DefaultHash is the default hash function used when none is specified.
// Currently set to Blake2b for a good balance of security and performance.
var DefaultHash HashFunc = Blake2b

// Hash applies the default hash function to a key.
func Hash(key string) uint64 {
	return DefaultHash(key)
}

// HashBytes applies the default hash function to a byte slice.
// This is a convenience function for working with binary data.
func HashBytes(data []byte) uint64 {
	return DefaultHash(string(data))
}
