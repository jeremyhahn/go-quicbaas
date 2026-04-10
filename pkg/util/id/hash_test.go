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
	"testing"
)

// TestHashBytes tests the public hash API.
func TestHashBytes(t *testing.T) {
	tests := []struct {
		name string
		data []byte
		want uint64
		same bool
	}{
		{
			name: "simple data",
			data: []byte("hello"),
		},
		{
			name: "empty data",
			data: []byte{},
		},
		{
			name: "binary data",
			data: []byte{0x00, 0xFF, 0xAB, 0xCD},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			hash1 := HashBytes(tt.data)
			hash2 := HashBytes(tt.data)

			// Same input should produce same hash
			if hash1 != hash2 {
				t.Errorf("HashBytes() not deterministic: %d != %d", hash1, hash2)
			}

			// Hash should not be zero for non-empty input
			if len(tt.data) > 0 && hash1 == 0 {
				t.Errorf("HashBytes() produced zero hash for non-empty input")
			}
		})
	}
}

// TestHashString tests the string hash API.
func TestHashString(t *testing.T) {
	tests := []struct {
		name string
		data string
	}{
		{"simple string", "hello"},
		{"empty string", ""},
		{"unicode string", "こんにちは"},
		{"long string", "this is a longer string with many characters to hash"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			hash1 := HashString(tt.data)
			hash2 := HashString(tt.data)

			// Same input should produce same hash
			if hash1 != hash2 {
				t.Errorf("HashString() not deterministic: %d != %d", hash1, hash2)
			}

			// Should match HashBytes
			expectedHash := HashBytes([]byte(tt.data))
			if hash1 != expectedHash {
				t.Errorf("HashString() = %d, want %d", hash1, expectedHash)
			}
		})
	}
}

// TestHashConsistency tests that hash functions are consistent.
func TestHashConsistency(t *testing.T) {
	testData := "test-consistency-data"

	hashBytes := HashBytes([]byte(testData))
	hashString := HashString(testData)
	hashFNV := fnv1a64([]byte(testData))

	if hashBytes != hashString {
		t.Errorf("HashBytes != HashString: %d != %d", hashBytes, hashString)
	}

	if hashBytes != hashFNV {
		t.Errorf("HashBytes != fnv1a64: %d != %d", hashBytes, hashFNV)
	}
}

// TestHashDistribution tests hash distribution quality.
func TestHashDistribution(t *testing.T) {
	count := 10000
	buckets := 100
	dist := make([]int, buckets)

	for i := 0; i < count; i++ {
		data := []byte{byte(i), byte(i >> 8)}
		hash := HashBytes(data)
		bucket := hash % uint64(buckets)
		dist[bucket]++
	}

	// Check that distribution is reasonably even
	expected := count / buckets
	tolerance := expected / 2 // Allow 50% variance

	for i, c := range dist {
		if c < expected-tolerance || c > expected+tolerance {
			t.Logf("Bucket %d: %d items (expected ~%d)", i, c, expected)
		}
	}

	// Calculate chi-square statistic (basic check)
	var chiSquare float64
	for _, c := range dist {
		diff := float64(c - expected)
		chiSquare += (diff * diff) / float64(expected)
	}

	t.Logf("Chi-square statistic: %.2f (lower is better)", chiSquare)

	// Very basic check - chi-square should be reasonably low
	// For good distribution with 100 buckets, expect chi-square around 100
	if chiSquare > 200 {
		t.Errorf("Hash distribution appears poor, chi-square = %.2f", chiSquare)
	}
}

// TestHashDifferentInputs tests that different inputs produce different hashes.
func TestHashDifferentInputs(t *testing.T) {
	inputs := []string{
		"test1",
		"test2",
		"test3",
		"different",
		"unique",
	}

	hashes := make(map[uint64]string)

	for _, input := range inputs {
		hash := HashString(input)

		if prev, exists := hashes[hash]; exists {
			t.Errorf("Hash collision: %q and %q both hash to %d", prev, input, hash)
		}

		hashes[hash] = input
	}
}

// TestFNV1a64EmptyInput tests hash of empty input.
func TestFNV1a64EmptyInput(t *testing.T) {
	hash := fnv1a64([]byte{})

	// FNV-1a of empty input should equal the offset
	if hash != offset64 {
		t.Errorf("fnv1a64([]) = %d, want %d (offset64)", hash, uint64(offset64))
	}
}

// TestFNV1a64Deterministic tests that hash is deterministic.
func TestFNV1a64Deterministic(t *testing.T) {
	tests := []string{
		"",
		"a",
		"test",
		"hello world",
		"unicode: こんにちは",
	}

	for _, input := range tests {
		t.Run(input, func(t *testing.T) {
			hash1 := fnv1a64([]byte(input))
			hash2 := fnv1a64([]byte(input))
			if hash1 != hash2 {
				t.Errorf("fnv1a64(%q) not deterministic: %d != %d", input, hash1, hash2)
			}
		})
	}
}
