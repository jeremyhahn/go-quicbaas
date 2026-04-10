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

const (
	// FNV-1a 64-bit constants.
	offset64 = 14695981039346656037
	prime64  = 1099511628211
)

// fnv1a64 implements the FNV-1a 64-bit hash algorithm.
// This is a fast, non-cryptographic hash function suitable for ID generation.
//
// FNV-1a is chosen for:
// - Excellent distribution properties
// - Very fast computation (no allocations)
// - Good collision resistance for ID generation
// - Simple implementation with no external dependencies.
func fnv1a64(data []byte) uint64 {
	hash := uint64(offset64)
	for _, b := range data {
		hash ^= uint64(b)
		hash *= prime64
	}
	return hash
}

// HashBytes provides a public API for hashing arbitrary byte slices.
// Returns a 64-bit FNV-1a hash of the input data.
func HashBytes(data []byte) uint64 {
	return fnv1a64(data)
}

// HashString provides a convenient API for hashing strings.
// Returns a 64-bit FNV-1a hash of the input string.
func HashString(s string) uint64 {
	return fnv1a64([]byte(s))
}
