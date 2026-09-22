// Copyright (c) 2026 Jeremy Hahn
// Copyright (c) 2026 Automate The Things, LLC
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

package encryption

import (
	"encoding/hex"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The AES-128 field path used to build its GCM instance with cipher.NewGCM and
// prepend a nonce it generated itself. crypto/cipher calls that an arbitrary
// IV and the Go Cryptographic Module refuses it under GODEBUG=fips140=only, so
// envelope encryption failed closed on exactly the regulated hosts the FIPS
// build exists for. cipher.NewGCMWithRandomNonce is the approved construction
// and, because it also prepends its nonce to the ciphertext, it writes and
// reads the identical wire format — the tests below pin both halves of that
// claim.

// aes128LegacyKey and aes128LegacyCiphertext are a field encrypted by the
// pre-change implementation: a 12-byte nonce followed by the GCM output.
// Decoding it proves ciphertext already at rest is still readable.
const (
	aes128LegacyKey        = "0123456789abcdef"
	aes128LegacyPlaintext  = "hello envelope"
	aes128LegacyCiphertext = "0102030405060708090a0b0c" +
		"a8f54aa34f29b380e7acdb1289ffc5a1bc2a3882f87ae6f2a7ca09ebbe9e"
)

func TestDecryptFieldReadsPreFIPSAES128Ciphertext(t *testing.T) {
	stored, err := hex.DecodeString(aes128LegacyCiphertext)
	require.NoError(t, err)

	plaintext, err := DecryptField(stored, []byte(aes128LegacyKey), AlgorithmAES128)

	require.NoError(t, err, "a field written before the FIPS-approved construction must still decrypt")
	assert.Equal(t, aes128LegacyPlaintext, string(plaintext))
}

func TestEncryptFieldAES128KeepsTheNoncePrefixedWireFormat(t *testing.T) {
	plaintext := []byte(aes128LegacyPlaintext)

	ciphertext, err := EncryptField(plaintext, []byte(aes128LegacyKey), AlgorithmAES128)
	require.NoError(t, err)

	assert.Len(t, ciphertext, GCMNonceSize+len(plaintext)+GCMTagSize,
		"the stored form is nonce || ciphertext || tag; changing it would orphan every encrypted field")

	roundTripped, err := DecryptField(ciphertext, []byte(aes128LegacyKey), AlgorithmAES128)
	require.NoError(t, err)
	assert.Equal(t, plaintext, roundTripped)
}

func TestEncryptFieldAES128RejectsTamperedCiphertext(t *testing.T) {
	ciphertext, err := EncryptField([]byte(aes128LegacyPlaintext), []byte(aes128LegacyKey), AlgorithmAES128)
	require.NoError(t, err)

	tests := []struct {
		name   string
		mutate func(ct []byte) []byte
	}{
		{
			name:   "flipped nonce byte",
			mutate: func(ct []byte) []byte { ct[0] ^= 0xff; return ct },
		},
		{
			name:   "flipped ciphertext byte",
			mutate: func(ct []byte) []byte { ct[GCMNonceSize] ^= 0xff; return ct },
		},
		{
			name:   "flipped tag byte",
			mutate: func(ct []byte) []byte { ct[len(ct)-1] ^= 0xff; return ct },
		},
		{
			name:   "truncated below the minimum length",
			mutate: func(ct []byte) []byte { return ct[:GCMNonceSize+GCMTagSize-1] },
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tampered := tt.mutate(append([]byte(nil), ciphertext...))

			plaintext, err := DecryptField(tampered, []byte(aes128LegacyKey), AlgorithmAES128)

			assert.Error(t, err, "authentication must fail rather than return plaintext")
			assert.Nil(t, plaintext)
		})
	}
}

// TestEncryptFieldIsFIPSApproved is the conformance assertion the fips140=only
// job exists for: under a FIPS-activated build the module itself refuses an
// unapproved construction, so a green round trip here IS the proof. Outside
// FIPS it still guards the round trip for both key sizes.
func TestEncryptFieldIsFIPSApproved(t *testing.T) {
	tests := []struct {
		name      string
		algorithm string
		key       string
	}{
		{name: "aes128", algorithm: AlgorithmAES128, key: aes128LegacyKey},
		{name: "aes256", algorithm: AlgorithmAES256, key: "0123456789abcdef0123456789abcdef"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ciphertext, err := EncryptField([]byte("envelope payload"), []byte(tt.key), tt.algorithm)
			require.NoError(t, err)

			plaintext, err := DecryptField(ciphertext, []byte(tt.key), tt.algorithm)
			require.NoError(t, err)
			assert.Equal(t, "envelope payload", string(plaintext))
		})
	}
}
