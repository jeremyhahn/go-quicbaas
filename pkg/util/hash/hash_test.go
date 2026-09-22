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

package hash

import (
	"strconv"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestBlake2b(t *testing.T) {
	tests := []struct {
		name string
		key  string
	}{
		{
			name: "simple key",
			key:  "test-key",
		},
		{
			name: "empty key",
			key:  "",
		},
		{
			name: "long key",
			key:  "this-is-a-very-long-key-that-should-still-work-correctly-with-blake2b-hash-function",
		},
		{
			name: "special characters",
			key:  "key-with-!@#$%^&*()_+-=[]{}|;:,.<>?",
		},
		{
			name: "unicode characters",
			key:  "key-with-unicode-世界-🌍",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			hash1 := Blake2b(tt.key)
			hash2 := Blake2b(tt.key)

			// Same key should produce same hash (deterministic)
			assert.Equal(t, hash1, hash2, "Blake2b should be deterministic")

			// Hash should be non-zero for non-empty keys
			if tt.key != "" {
				assert.NotZero(t, hash1, "Hash should be non-zero")
			}
		})
	}
}

func TestBlake2b_Collision(t *testing.T) {
	// Different keys should produce different hashes (collision resistance)
	key1 := "test-key-1"
	key2 := "test-key-2"

	hash1 := Blake2b(key1)
	hash2 := Blake2b(key2)

	assert.NotEqual(t, hash1, hash2, "Different keys should produce different hashes")
}

func TestFNV1a(t *testing.T) {
	tests := []struct {
		name string
		key  string
	}{
		{
			name: "simple key",
			key:  "test-key",
		},
		{
			name: "empty key",
			key:  "",
		},
		{
			name: "long key",
			key:  "this-is-a-very-long-key-that-should-still-work-correctly-with-fnv1a-hash-function",
		},
		{
			name: "special characters",
			key:  "key-with-!@#$%^&*()_+-=[]{}|;:,.<>?",
		},
		{
			name: "unicode characters",
			key:  "key-with-unicode-世界-🌍",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			hash1 := FNV1a(tt.key)
			hash2 := FNV1a(tt.key)

			// Same key should produce same hash (deterministic)
			assert.Equal(t, hash1, hash2, "FNV1a should be deterministic")

			// Hash should be non-zero for non-empty keys
			if tt.key != "" {
				assert.NotZero(t, hash1, "Hash should be non-zero")
			}
		})
	}
}

func TestFNV1a_Collision(t *testing.T) {
	// Different keys should produce different hashes (collision resistance)
	key1 := "test-key-1"
	key2 := "test-key-2"

	hash1 := FNV1a(key1)
	hash2 := FNV1a(key2)

	assert.NotEqual(t, hash1, hash2, "Different keys should produce different hashes")
}

func TestHash(t *testing.T) {
	tests := []struct {
		name string
		key  string
	}{
		{
			name: "simple key",
			key:  "test-key",
		},
		{
			name: "empty key",
			key:  "",
		},
		{
			name: "complex key",
			key:  "complex-key-with-many-characters-123456789",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			hash1 := Hash(tt.key)
			hash2 := Hash(tt.key)

			// Same key should produce same hash (deterministic)
			assert.Equal(t, hash1, hash2, "Hash should be deterministic")

			// Hash should match DefaultHash
			defaultHash := DefaultHash(tt.key)
			assert.Equal(t, defaultHash, hash1, "Hash should use DefaultHash")
		})
	}
}

func TestHashBytes(t *testing.T) {
	tests := []struct {
		name string
		data []byte
	}{
		{
			name: "simple bytes",
			data: []byte("test-data"),
		},
		{
			name: "empty bytes",
			data: []byte{},
		},
		{
			name: "binary data",
			data: []byte{0x00, 0x01, 0x02, 0x03, 0xFF, 0xFE, 0xFD},
		},
		{
			name: "long data",
			data: []byte("this-is-a-very-long-byte-array-that-should-be-hashed-correctly"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			hash1 := HashBytes(tt.data)
			hash2 := HashBytes(tt.data)

			// Same data should produce same hash (deterministic)
			assert.Equal(t, hash1, hash2, "HashBytes should be deterministic")

			// HashBytes should match Hash with string conversion
			expectedHash := Hash(string(tt.data))
			assert.Equal(t, expectedHash, hash1, "HashBytes should match Hash(string(data))")
		})
	}
}

func TestHashBytes_Consistency(t *testing.T) {
	// Verify that HashBytes and Hash produce the same result for equivalent data
	testString := "consistency-test"
	testBytes := []byte(testString)

	hashFromString := Hash(testString)
	hashFromBytes := HashBytes(testBytes)

	assert.Equal(t, hashFromString, hashFromBytes, "Hash and HashBytes should be consistent")
}

func TestHashFunc_AsType(t *testing.T) {
	// Test that Blake2b and FNV1a can be assigned to HashFunc type
	var hashFunc HashFunc

	hashFunc = Blake2b
	assert.NotNil(t, hashFunc, "Blake2b should be assignable to HashFunc")
	result1 := hashFunc("test")
	assert.NotZero(t, result1)

	hashFunc = FNV1a
	assert.NotNil(t, hashFunc, "FNV1a should be assignable to HashFunc")
	result2 := hashFunc("test")
	assert.NotZero(t, result2)
}

func TestDefaultHash(t *testing.T) {
	// Verify DefaultHash is set
	assert.NotNil(t, DefaultHash, "DefaultHash should be initialized")

	// Verify it produces valid hashes
	hash := DefaultHash("test-key")
	assert.NotZero(t, hash, "DefaultHash should produce non-zero hash")

	// Verify it's deterministic
	hash2 := DefaultHash("test-key")
	assert.Equal(t, hash, hash2, "DefaultHash should be deterministic")
}

// Benchmark tests to compare hash function performance

func BenchmarkBlake2b(b *testing.B) {
	key := "benchmark-test-key-12345678"
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = Blake2b(key)
	}
}

func BenchmarkFNV1a(b *testing.B) {
	key := "benchmark-test-key-12345678"
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = FNV1a(key)
	}
}

func BenchmarkHash(b *testing.B) {
	key := "benchmark-test-key-12345678"
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = Hash(key)
	}
}

func BenchmarkHashBytes(b *testing.B) {
	data := []byte("benchmark-test-key-12345678")
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = HashBytes(data)
	}
}

func BenchmarkHashFunctions_Comparison(b *testing.B) {
	sizes := []int{16, 64, 256, 1024}

	for _, size := range sizes {
		data := make([]byte, size)
		for i := range data {
			data[i] = byte(i % 256)
		}
		key := string(data)

		b.Run("Blake2b_"+string(rune(size)), func(b *testing.B) {
			for i := 0; i < b.N; i++ {
				_ = Blake2b(key)
			}
		})

		b.Run("FNV1a_"+string(rune(size)), func(b *testing.B) {
			for i := 0; i < b.N; i++ {
				_ = FNV1a(key)
			}
		})
	}
}

func TestSHA256(t *testing.T) {
	tests := []struct {
		name string
		key  string
	}{
		{name: "simple key", key: "test-key"},
		{name: "empty key", key: ""},
		{name: "long key", key: "this-is-a-very-long-key-that-should-still-work-correctly-with-the-sha256-hash-function"},
		{name: "special characters", key: "key-with-!@#$%^&*()_+-=[]{}|;:,.<>?"},
		{name: "unicode characters", key: "key-with-unicode-世界-🌍"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			first := SHA256(tt.key)
			second := SHA256(tt.key)

			assert.Equal(t, first, second, "SHA256 should be deterministic")
			assert.NotEqual(t, Blake2b(tt.key), first,
				"SHA256 should not agree with Blake2b, or the ring would not actually change algorithm")
		})
	}
}

// TestSHA256KnownVector pins the mapping from digest to ring position. A change
// here silently re-places every key on every ring built with this algorithm.
func TestSHA256KnownVector(t *testing.T) {
	// sha256("abc") = ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad,
	// whose first eight bytes read little-endian are 0xeacf018fbf1678ba.
	assert.Equal(t, uint64(0xeacf018fbf1678ba), SHA256("abc"))
}

func TestSHA256DistributesAcrossTheRing(t *testing.T) {
	ring, err := New(Config{VirtualNodes: 150, HashFunc: SHA256})
	assert.NoError(t, err)

	for _, host := range []string{"node-a", "node-b", "node-c"} {
		_, err := ring.Add(host)
		assert.NoError(t, err)
	}

	counts := make(map[string]int)
	for i := 0; i < 3000; i++ {
		host, err := ring.Get("key-" + strconv.Itoa(i))
		assert.NoError(t, err)
		counts[host]++
	}

	assert.Len(t, counts, 3, "every host should own part of the keyspace")
	for host, count := range counts {
		assert.Greater(t, count, 500, "host %s took too small a share: %d of 3000", host, count)
	}
}

func TestSelect(t *testing.T) {
	tests := []struct {
		name      string
		algorithm Algorithm
		want      HashFunc
	}{
		{name: "blake2b by name", algorithm: AlgorithmBlake2b, want: Blake2b},
		{name: "sha256 by name", algorithm: AlgorithmSHA256, want: SHA256},
		{name: "fnv1a by name", algorithm: AlgorithmFNV1a, want: FNV1a},
		{name: "empty selects the default", algorithm: "", want: DefaultHash},
		{name: "names are case-insensitive", algorithm: "SHA256", want: SHA256},
		{name: "surrounding whitespace is ignored", algorithm: " sha256 ", want: SHA256},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := Select(tt.algorithm)
			assert.NoError(t, err)
			assert.NotNil(t, got)
			// Functions are not comparable, so compare their output instead.
			assert.Equal(t, tt.want("compare-me"), got("compare-me"))
		})
	}
}

func TestSelectRejectsUnknownAlgorithm(t *testing.T) {
	tests := []struct {
		name      string
		algorithm Algorithm
	}{
		{name: "misspelled", algorithm: "blake2"},
		{name: "unsupported algorithm", algorithm: "md5"},
		{name: "not an algorithm at all", algorithm: "!"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := Select(tt.algorithm)
			assert.Nil(t, got, "no hash function may be returned beside an error")

			var unknown *UnknownAlgorithmError
			assert.ErrorAs(t, err, &unknown)
			assert.Equal(t, string(tt.algorithm), unknown.Algorithm)
			assert.Equal(t, SupportedAlgorithms(), unknown.Supported,
				"the error should tell the operator what it could have accepted")
			assert.Contains(t, unknown.Error(), string(tt.algorithm))
			assert.Contains(t, unknown.Error(), string(AlgorithmSHA256))
		})
	}
}

func TestSupportedAlgorithmsIsStableAndComplete(t *testing.T) {
	supported := SupportedAlgorithms()

	assert.Equal(t, []Algorithm{AlgorithmBlake2b, AlgorithmFNV1a, AlgorithmSHA256}, supported,
		"the list is sorted so operator-facing output and error messages do not reorder between runs")

	for _, algorithm := range supported {
		fn, err := Select(algorithm)
		assert.NoError(t, err, "advertised algorithm %q must be selectable", algorithm)
		assert.NotNil(t, fn)
	}
}

// TestDefaultAlgorithmIsBlake2b holds the owner's ruling in place: blake2b
// stays the placement-hashing default and SHA-256 is the selectable
// alternative, not a replacement.
func TestDefaultAlgorithmIsBlake2b(t *testing.T) {
	assert.Equal(t, AlgorithmBlake2b, DefaultAlgorithm)

	fn, err := Select("")
	assert.NoError(t, err)
	assert.Equal(t, Blake2b("ruling-8"), fn("ruling-8"))
	assert.Equal(t, Blake2b("ruling-8"), Hash("ruling-8"))
}
