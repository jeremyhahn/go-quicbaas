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
	"crypto/sha256"
	"encoding/binary"
	"sort"
	"strconv"
	"strings"

	"github.com/minio/blake2b-simd"

	"github.com/jeremyhahn/go-quicbaas/pkg/util/id"
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
	return id.HashBytes([]byte(key))
}

// SHA256 is the FIPS 140-3 approved hash function on the ring. It is served by
// the Go Cryptographic Module, so a FIPS-activated process can place keys with
// it; Blake2b comes from a third-party implementation and is outside that
// module. It is the slowest of the three, which is the price of the approval.
//
// Use this when:
//   - The deployment requires every hash to come from an approved module
//   - An external system places keys on the same ring using SHA-256
func SHA256(key string) uint64 {
	sum := sha256.Sum256([]byte(key))
	return binary.LittleEndian.Uint64(sum[:8])
}

// Algorithm names a hash function so it can be selected from configuration
// rather than by holding a Go function value.
type Algorithm string

const (
	// AlgorithmBlake2b selects Blake2b, the default.
	AlgorithmBlake2b Algorithm = "blake2b"

	// AlgorithmSHA256 selects SHA-256, the FIPS-approved alternative.
	AlgorithmSHA256 Algorithm = "sha256"

	// AlgorithmFNV1a selects FNV-1a, the fastest and non-cryptographic option.
	AlgorithmFNV1a Algorithm = "fnv1a"
)

// DefaultAlgorithm is the algorithm Select resolves the empty name to.
//
// Blake2b stays the default because placing a key on the ring is not a
// cryptographic security function: the ring decides which node owns a key, and
// nothing about that placement is a secret, a signature or a key. Owner ruling
// 8 (handbook, software/docs/04-security-model.md) keeps blake2b as the
// default on exactly that reasoning and makes SHA-256 selectable for
// deployments that would rather not explain the distinction to an auditor.
const DefaultAlgorithm = AlgorithmBlake2b

// hashFuncsByAlgorithm dispatches a name to its hash function in constant time.
var hashFuncsByAlgorithm = map[Algorithm]HashFunc{
	AlgorithmBlake2b: Blake2b,
	AlgorithmSHA256:  SHA256,
	AlgorithmFNV1a:   FNV1a,
}

// UnknownAlgorithmError reports a hash algorithm name that no ring can be
// built from. It names what was asked for and what would have been accepted,
// so a typo in a configuration file is a one-line fix rather than a search.
type UnknownAlgorithmError struct {
	// Algorithm is the name as it was given, before normalization.
	Algorithm string

	// Supported lists the names Select accepts.
	Supported []Algorithm
}

// Error implements the error interface.
func (e *UnknownAlgorithmError) Error() string {
	names := make([]string, 0, len(e.Supported))
	for _, algorithm := range e.Supported {
		names = append(names, string(algorithm))
	}
	return "hash: unknown hash algorithm " + strconv.Quote(e.Algorithm) +
		"; supported: " + strings.Join(names, ", ")
}

// SupportedAlgorithms returns the selectable algorithm names in a stable
// order, so operator-facing output does not reorder between runs.
func SupportedAlgorithms() []Algorithm {
	algorithms := make([]Algorithm, 0, len(hashFuncsByAlgorithm))
	for algorithm := range hashFuncsByAlgorithm {
		algorithms = append(algorithms, algorithm)
	}
	sort.Slice(algorithms, func(i, j int) bool { return algorithms[i] < algorithms[j] })
	return algorithms
}

// Select resolves a configured algorithm name to its hash function. The empty
// name resolves to DefaultAlgorithm; names are matched case-insensitively and
// surrounding whitespace is ignored. An unknown name yields a typed
// *UnknownAlgorithmError and no hash function, so a misconfigured ring fails
// at construction rather than placing keys with something other than what was
// asked for.
//
// Pass the result to New as Config.HashFunc:
//
//	hashFunc, err := hash.Select(cfg.Algorithm)
//	if err != nil {
//	    return err
//	}
//	ring, err := hash.New(hash.Config{VirtualNodes: 150, HashFunc: hashFunc})
func Select(algorithm Algorithm) (HashFunc, error) {
	normalized := Algorithm(strings.ToLower(strings.TrimSpace(string(algorithm))))
	if normalized == "" {
		normalized = DefaultAlgorithm
	}

	hashFunc, ok := hashFuncsByAlgorithm[normalized]
	if !ok {
		return nil, &UnknownAlgorithmError{
			Algorithm: string(algorithm),
			Supported: SupportedAlgorithms(),
		}
	}
	return hashFunc, nil
}

// DefaultHash is the default hash function used when none is specified.
// Currently set to Blake2b for a good balance of security and performance.
// It is the same function DefaultAlgorithm names, which is what Select
// resolves an empty name to; the two are kept in step by
// TestDefaultAlgorithmIsBlake2b.
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
