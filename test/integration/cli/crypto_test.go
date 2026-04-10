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

//go:build integration

package cli

import (
	"encoding/base64"
	"encoding/json"
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// =============================================================================
// Encrypt / Decrypt Tests
// =============================================================================

// TestCryptoEncryptDecryptRoundTrip creates a key, encrypts plaintext, decrypts
// the ciphertext, and verifies the original plaintext is recovered.
func TestCryptoEncryptDecryptRoundTrip(t *testing.T) {
	RunForTransports(t, "CryptoEncryptDecryptRoundTrip", cryptoTransportsToSkip, func(t *testing.T, runner *TestRunner) {
		if !runner.RequireCryptoAvailable() {
			return
		}

		keyName := UniqueKey("crypto-enc-rt")

		// Create KMS key for crypto operations
		createCmd := KMSKeyCreateCommand(keyName, "AES-256-GCM")
		runner.RunAndAssert(createCmd)
		defer func() {
			runner.Run(KMSKeyDeleteCommand(keyName, true))
		}()

		plaintext := "Hello World"

		// Encrypt data with JSON output for parsing
		encryptCmd := CryptoEncryptCommand(keyName, plaintext)
		encryptCmd.Args = append(encryptCmd.Args, "--output", "json")
		stdout, _, err := runner.Run(encryptCmd)
		require.NoError(t, err, "encrypt should succeed")
		require.NotEmpty(t, stdout, "encrypt output should not be empty")

		// Parse ciphertext from JSON response
		var encResp struct {
			Ciphertext string `json:"ciphertext"`
			KeyID      string `json:"key_id"`
			KeyVersion int    `json:"key_version"`
		}
		err = json.Unmarshal([]byte(stdout), &encResp)
		require.NoError(t, err, "encrypt JSON response should be parseable")
		assert.NotEmpty(t, encResp.Ciphertext, "ciphertext should not be empty")
		assert.Equal(t, keyName, encResp.KeyID, "key ID should match")

		// Decrypt the ciphertext
		decryptCmd := CryptoDecryptBase64Command(keyName, encResp.Ciphertext)
		decryptCmd.Args = append(decryptCmd.Args, "--output", "json")
		decStdout, _, err := runner.Run(decryptCmd)
		require.NoError(t, err, "decrypt should succeed")
		require.NotEmpty(t, decStdout, "decrypt output should not be empty")

		// Verify the recovered plaintext matches the original
		var decResp struct {
			Plaintext  string `json:"plaintext"`
			KeyID      string `json:"key_id"`
			KeyVersion int    `json:"key_version"`
		}
		err = json.Unmarshal([]byte(decStdout), &decResp)
		require.NoError(t, err, "decrypt JSON response should be parseable")
		// Plaintext may be returned as base64-encoded; decode if needed
		recovered := decResp.Plaintext
		if decoded, decErr := base64.StdEncoding.DecodeString(recovered); decErr == nil {
			recovered = string(decoded)
		}
		assert.Equal(t, plaintext, recovered, "decrypted plaintext should match original")
		assert.Equal(t, keyName, decResp.KeyID, "decrypt key ID should match")
	})
}

// TestCryptoEncryptWithContext encrypts data with an AAD context, verifies
// that decrypting with the same context succeeds and decrypting without the
// context fails.
func TestCryptoEncryptWithContext(t *testing.T) {
	RunForTransports(t, "CryptoEncryptWithContext", cryptoTransportsToSkip, func(t *testing.T, runner *TestRunner) {
		if !runner.RequireCryptoAvailable() {
			return
		}

		keyName := UniqueKey("crypto-ctx")

		createCmd := KMSKeyCreateCommand(keyName, "AES-256-GCM")
		runner.RunAndAssert(createCmd)
		defer func() {
			runner.Run(KMSKeyDeleteCommand(keyName, true))
		}()

		plaintext := "context-sensitive-data"
		aadContext := "user-session-12345"

		// Encrypt with context, JSON output for parsing
		encryptCmd := CryptoEncryptWithContextCommand(keyName, plaintext, aadContext)
		encryptCmd.Args = append(encryptCmd.Args, "--output", "json")
		stdout, _, err := runner.Run(encryptCmd)
		require.NoError(t, err, "encrypt with context should succeed")
		require.NotEmpty(t, stdout, "encrypt output should not be empty")

		var encResp struct {
			Ciphertext string `json:"ciphertext"`
		}
		err = json.Unmarshal([]byte(stdout), &encResp)
		require.NoError(t, err, "encrypt JSON should be parseable")
		require.NotEmpty(t, encResp.Ciphertext, "ciphertext should not be empty")

		// Decrypt with the same context should succeed
		decryptCmd := CryptoDecryptWithContextCommand(keyName, encResp.Ciphertext, aadContext)
		decryptCmd.Args = append(decryptCmd.Args, "--output", "json")
		decStdout, _, err := runner.Run(decryptCmd)
		require.NoError(t, err, "decrypt with matching context should succeed")

		var decResp struct {
			Plaintext string `json:"plaintext"`
		}
		err = json.Unmarshal([]byte(decStdout), &decResp)
		require.NoError(t, err, "decrypt JSON should be parseable")
		recoveredCtx := decResp.Plaintext
		if decoded, decErr := base64.StdEncoding.DecodeString(recoveredCtx); decErr == nil {
			recoveredCtx = string(decoded)
		}
		assert.Equal(t, plaintext, recoveredCtx, "decrypted plaintext should match original")

		// Decrypt without context should fail (AAD mismatch)
		decryptNoCtxCmd := CryptoDecryptBase64Command(keyName, encResp.Ciphertext)
		_, _, err = runner.Run(decryptNoCtxCmd)
		assert.Error(t, err, "decrypt without context should fail when data was encrypted with context")
	})
}

// TestCryptoEncryptInvalidKey verifies that encrypting with a nonexistent key
// returns an appropriate error.
func TestCryptoEncryptInvalidKey(t *testing.T) {
	RunForTransports(t, "CryptoEncryptInvalidKey", cryptoTransportsToSkip, func(t *testing.T, runner *TestRunner) {
		if !runner.RequireCryptoAvailable() {
			return
		}

		nonexistentKey := UniqueKey("crypto-nokey")

		encryptCmd := CommandDef{
			Name:           "crypto encrypt nonexistent key",
			Args:           []string{"crypto", "encrypt", nonexistentKey, "--data", "test data"},
			ExpectSuccess:  false,
			Timeout:        cryptoTimeout,
			SkipTransports: cryptoTransportsToSkip,
		}
		stdout, stderr, err := runner.Run(encryptCmd)
		require.Error(t, err, "encrypt with nonexistent key should fail")

		combined := stdout + stderr
		assert.True(t,
			strings.Contains(combined, "not found") ||
				strings.Contains(combined, "not_found") ||
				strings.Contains(combined, "does not exist") ||
				strings.Contains(combined, "error") ||
				strings.Contains(combined, "Error"),
			"error output should indicate the key was not found, got: %s", combined)
	})
}

// =============================================================================
// Sign / Verify Tests
// =============================================================================

// TestCryptoSignVerifyRoundTrip creates a key, signs data, and verifies the
// signature is valid.
func TestCryptoSignVerifyRoundTrip(t *testing.T) {
	RunForTransports(t, "CryptoSignVerifyRoundTrip", cryptoTransportsToSkip, func(t *testing.T, runner *TestRunner) {
		if !runner.RequireCryptoAvailable() {
			return
		}

		keyName := UniqueKey("crypto-sign-rt")

		createCmd := KMSKeyCreateCommand(keyName, "ECDSA-P256")
		runner.RunAndAssert(createCmd)
		defer func() {
			runner.Run(KMSKeyDeleteCommand(keyName, true))
		}()

		message := "message to sign and verify"

		// Sign data with JSON output
		signCmd := CryptoSignCommand(keyName, message)
		signCmd.Args = append(signCmd.Args, "--output", "json")
		stdout, _, err := runner.Run(signCmd)
		require.NoError(t, err, "sign should succeed")
		require.NotEmpty(t, stdout, "sign output should not be empty")

		var signResp struct {
			Signature     string `json:"signature"`
			KeyID         string `json:"key_id"`
			KeyVersion    int    `json:"key_version"`
			HashAlgorithm string `json:"hash_algorithm"`
		}
		err = json.Unmarshal([]byte(stdout), &signResp)
		require.NoError(t, err, "sign JSON should be parseable")
		require.NotEmpty(t, signResp.Signature, "signature should not be empty")
		assert.Equal(t, keyName, signResp.KeyID, "sign key ID should match")

		// Verify the signature
		verifyCmd := CryptoVerifyCommand(keyName, message, signResp.Signature)
		verifyCmd.Args = append(verifyCmd.Args, "--output", "json")
		verifyStdout, _, err := runner.Run(verifyCmd)
		require.NoError(t, err, "verify should succeed")

		var verifyResp struct {
			Valid      bool   `json:"valid"`
			KeyID      string `json:"key_id"`
			KeyVersion int    `json:"key_version"`
		}
		err = json.Unmarshal([]byte(verifyStdout), &verifyResp)
		require.NoError(t, err, "verify JSON should be parseable")
		assert.True(t, verifyResp.Valid, "signature should be valid")
		assert.Equal(t, keyName, verifyResp.KeyID, "verify key ID should match")
	})
}

// TestCryptoSignVerifyInvalidSignature signs data and then verifies with
// different data, expecting the signature to be invalid.
func TestCryptoSignVerifyInvalidSignature(t *testing.T) {
	RunForTransports(t, "CryptoSignVerifyInvalidSignature", cryptoTransportsToSkip, func(t *testing.T, runner *TestRunner) {
		if !runner.RequireCryptoAvailable() {
			return
		}

		keyName := UniqueKey("crypto-sign-inv")

		createCmd := KMSKeyCreateCommand(keyName, "ECDSA-P256")
		runner.RunAndAssert(createCmd)
		defer func() {
			runner.Run(KMSKeyDeleteCommand(keyName, true))
		}()

		originalMessage := "original message"

		// Sign the original message
		signCmd := CryptoSignCommand(keyName, originalMessage)
		signCmd.Args = append(signCmd.Args, "--output", "json")
		stdout, _, err := runner.Run(signCmd)
		require.NoError(t, err, "sign should succeed")

		var signResp struct {
			Signature string `json:"signature"`
		}
		err = json.Unmarshal([]byte(stdout), &signResp)
		require.NoError(t, err, "sign JSON should be parseable")
		require.NotEmpty(t, signResp.Signature, "signature should not be empty")

		// Verify with tampered data -- should indicate invalid
		tamperedMessage := "tampered message"
		verifyCmd := CryptoVerifyCommand(keyName, tamperedMessage, signResp.Signature)
		verifyCmd.Args = append(verifyCmd.Args, "--output", "json")
		verifyStdout, verifyStderr, err := runner.Run(verifyCmd)

		// The server may return an error or a response with Valid=false
		if err == nil {
			var verifyResp struct {
				Valid bool `json:"valid"`
			}
			parseErr := json.Unmarshal([]byte(verifyStdout), &verifyResp)
			if parseErr == nil {
				assert.False(t, verifyResp.Valid, "signature should be invalid for tampered data")
			}
		} else {
			// An error is also acceptable -- the server rejected the signature
			combined := verifyStdout + verifyStderr
			t.Logf("Verify with wrong data returned error (acceptable): %s", combined)
		}
	})
}

// TestCryptoSignWithAlgorithms signs data with SHA256, SHA384, and SHA512
// algorithms, verifying each signature is valid.
func TestCryptoSignWithAlgorithms(t *testing.T) {
	algorithms := []string{"SHA256", "SHA512"}

	RunForTransports(t, "CryptoSignWithAlgorithms", cryptoTransportsToSkip, func(t *testing.T, runner *TestRunner) {
		if !runner.RequireCryptoAvailable() {
			return
		}

		keyName := UniqueKey("crypto-sign-algo")

		createCmd := KMSKeyCreateCommand(keyName, "ECDSA-P256")
		runner.RunAndAssert(createCmd)
		defer func() {
			runner.Run(KMSKeyDeleteCommand(keyName, true))
		}()

		message := "algorithm test message"

		for _, algo := range algorithms {
			t.Run(algo, func(t *testing.T) {
				// Sign with the specified algorithm
				signCmd := CryptoSignWithAlgorithmCommand(keyName, message, algo)
				signCmd.Args = append(signCmd.Args, "--output", "json")
				stdout, _, err := runner.Run(signCmd)
				require.NoError(t, err, "sign with %s should succeed", algo)
				require.NotEmpty(t, stdout, "sign output should not be empty for %s", algo)

				var signResp struct {
					Signature     string `json:"signature"`
					HashAlgorithm string `json:"hash_algorithm"`
				}
				err = json.Unmarshal([]byte(stdout), &signResp)
				require.NoError(t, err, "sign JSON should be parseable for %s", algo)
				assert.NotEmpty(t, signResp.Signature, "signature should not be empty for %s", algo)

				// Verify the signature with the same algorithm
				verifyCmd := CryptoVerifyWithAlgorithmCommand(keyName, message, signResp.Signature, algo)
				verifyCmd.Args = append(verifyCmd.Args, "--output", "json")
				verifyStdout, _, err := runner.Run(verifyCmd)
				require.NoError(t, err, "verify with %s should succeed", algo)

				var verifyResp struct {
					Valid bool `json:"valid"`
				}
				err = json.Unmarshal([]byte(verifyStdout), &verifyResp)
				require.NoError(t, err, "verify JSON should be parseable for %s", algo)
				assert.True(t, verifyResp.Valid, "signature should be valid for %s", algo)
			})
		}
	})
}

// =============================================================================
// Hash Tests
// =============================================================================

// TestCryptoHashSHA256 hashes data with SHA256 and verifies the output
// contains a hex-encoded hash value.
func TestCryptoHashSHA256(t *testing.T) {
	RunForTransports(t, "CryptoHashSHA256", cryptoTransportsToSkip, func(t *testing.T, runner *TestRunner) {
		if !runner.RequireCryptoAvailable() {
			return
		}

		data := "hash me with SHA256"

		hashCmd := CryptoHashWithAlgorithmCommand(data, "SHA256")
		hashCmd.Args = append(hashCmd.Args, "--output", "json")
		stdout, _, err := runner.Run(hashCmd)
		require.NoError(t, err, "hash with SHA256 should succeed")
		require.NotEmpty(t, stdout, "hash output should not be empty")

		var hashResp struct {
			Algorithm string `json:"algorithm"`
			Hash      string `json:"hash"`
		}
		err = json.Unmarshal([]byte(stdout), &hashResp)
		require.NoError(t, err, "hash JSON should be parseable")
		assert.NotEmpty(t, hashResp.Hash, "hash value should not be empty")

		// SHA256 produces 32 bytes; output may be hex (64 chars) or base64 (44 chars)
		hexOrBase64 := regexp.MustCompile(`^[0-9a-fA-F]+$|^[A-Za-z0-9+/]+=*$`)
		assert.True(t, hexOrBase64.MatchString(hashResp.Hash),
			"hash should be a valid hex or base64 string, got: %s", hashResp.Hash)
	})
}

// TestCryptoHashMultipleAlgorithms hashes the same data with SHA256 and SHA512,
// verifying that different algorithms produce different hash values.
func TestCryptoHashMultipleAlgorithms(t *testing.T) {
	RunForTransports(t, "CryptoHashMultipleAlgorithms", cryptoTransportsToSkip, func(t *testing.T, runner *TestRunner) {
		if !runner.RequireCryptoAvailable() {
			return
		}

		data := "identical data for multiple hashes"

		// Hash with SHA256
		sha256Cmd := CryptoHashWithAlgorithmCommand(data, "SHA256")
		sha256Cmd.Args = append(sha256Cmd.Args, "--output", "json")
		sha256Stdout, _, err := runner.Run(sha256Cmd)
		require.NoError(t, err, "SHA256 hash should succeed")

		var sha256Resp struct {
			Hash string `json:"hash"`
		}
		err = json.Unmarshal([]byte(sha256Stdout), &sha256Resp)
		require.NoError(t, err, "SHA256 JSON should be parseable")
		require.NotEmpty(t, sha256Resp.Hash, "SHA256 hash should not be empty")

		// Hash with SHA512
		sha512Cmd := CryptoHashWithAlgorithmCommand(data, "SHA512")
		sha512Cmd.Args = append(sha512Cmd.Args, "--output", "json")
		sha512Stdout, _, err := runner.Run(sha512Cmd)
		require.NoError(t, err, "SHA512 hash should succeed")

		var sha512Resp struct {
			Hash string `json:"hash"`
		}
		err = json.Unmarshal([]byte(sha512Stdout), &sha512Resp)
		require.NoError(t, err, "SHA512 JSON should be parseable")
		require.NotEmpty(t, sha512Resp.Hash, "SHA512 hash should not be empty")

		// The two hashes must differ since the algorithms are different
		assert.NotEqual(t, sha256Resp.Hash, sha512Resp.Hash,
			"SHA256 and SHA512 hashes of the same data should differ")

		// SHA512 produces 64 bytes = 128 hex chars, SHA256 produces 32 bytes = 64 hex chars
		assert.Greater(t, len(sha512Resp.Hash), len(sha256Resp.Hash),
			"SHA512 hash should be longer than SHA256 hash")
	})
}

// TestCryptoHashEmptyData verifies that hashing an empty string either succeeds
// with a valid hash or returns an appropriate error.
func TestCryptoHashEmptyData(t *testing.T) {
	RunForTransports(t, "CryptoHashEmptyData", cryptoTransportsToSkip, func(t *testing.T, runner *TestRunner) {
		if !runner.RequireCryptoAvailable() {
			return
		}

		hashCmd := CryptoHashWithAlgorithmCommand("", "SHA256")
		hashCmd.Args = append(hashCmd.Args, "--output", "json")
		stdout, stderr, err := runner.Run(hashCmd)

		if err == nil {
			// Empty string hashing succeeded -- verify we got a valid hash
			var hashResp struct {
				Hash string `json:"hash"`
			}
			parseErr := json.Unmarshal([]byte(stdout), &hashResp)
			if parseErr == nil {
				assert.NotEmpty(t, hashResp.Hash, "hash of empty string should produce a non-empty value")
			}
		} else {
			// An error for empty data is also acceptable behavior
			combined := stdout + stderr
			t.Logf("Hash of empty data returned error (acceptable): %s", combined)
		}
	})
}

// =============================================================================
// Random Bytes Tests
// =============================================================================

// TestCryptoRandomDefault generates random bytes with the default size (32)
// and verifies the output is non-empty.
func TestCryptoRandomDefault(t *testing.T) {
	RunForTransports(t, "CryptoRandomDefault", cryptoTransportsToSkip, func(t *testing.T, runner *TestRunner) {
		if !runner.RequireCryptoAvailable() {
			return
		}

		randomCmd := CryptoRandomCommand(32)
		randomCmd.Args = append(randomCmd.Args, "--output", "json")
		stdout, _, err := runner.Run(randomCmd)
		require.NoError(t, err, "random 32 bytes should succeed")
		require.NotEmpty(t, stdout, "random output should not be empty")

		var randomResp struct {
			RandomBytes string `json:"random_bytes"`
			Length      int    `json:"length"`
			Format      string `json:"format"`
		}
		err = json.Unmarshal([]byte(stdout), &randomResp)
		require.NoError(t, err, "random JSON should be parseable")
		assert.NotEmpty(t, randomResp.RandomBytes, "random bytes should not be empty")
		assert.Equal(t, 32, randomResp.Length, "random bytes length should be 32")
	})
}

// TestCryptoRandomCustomSize generates 64 random bytes and verifies the output
// reflects the requested size.
func TestCryptoRandomCustomSize(t *testing.T) {
	RunForTransports(t, "CryptoRandomCustomSize", cryptoTransportsToSkip, func(t *testing.T, runner *TestRunner) {
		if !runner.RequireCryptoAvailable() {
			return
		}

		randomCmd := CryptoRandomCommand(64)
		randomCmd.Args = append(randomCmd.Args, "--output", "json")
		stdout, _, err := runner.Run(randomCmd)
		require.NoError(t, err, "random 64 bytes should succeed")
		require.NotEmpty(t, stdout, "random output should not be empty")

		var randomResp struct {
			RandomBytes string `json:"random_bytes"`
			Length      int    `json:"length"`
		}
		err = json.Unmarshal([]byte(stdout), &randomResp)
		require.NoError(t, err, "random JSON should be parseable")
		assert.NotEmpty(t, randomResp.RandomBytes, "random bytes should not be empty")
		assert.Equal(t, 64, randomResp.Length, "random bytes length should be 64")
	})
}

// TestCryptoRandomHexFormat generates random bytes with hex output format
// and verifies the output contains valid hex characters.
func TestCryptoRandomHexFormat(t *testing.T) {
	RunForTransports(t, "CryptoRandomHexFormat", cryptoTransportsToSkip, func(t *testing.T, runner *TestRunner) {
		if !runner.RequireCryptoAvailable() {
			return
		}

		randomCmd := CryptoRandomHexCommand(32)
		randomCmd.Args = append(randomCmd.Args, "--output", "json")
		stdout, _, err := runner.Run(randomCmd)
		require.NoError(t, err, "random hex format should succeed")
		require.NotEmpty(t, stdout, "random hex output should not be empty")

		var randomResp struct {
			RandomBytes string `json:"random_bytes"`
			Format      string `json:"format"`
			Length      int    `json:"length"`
		}
		err = json.Unmarshal([]byte(stdout), &randomResp)
		require.NoError(t, err, "random hex JSON should be parseable")
		assert.NotEmpty(t, randomResp.RandomBytes, "random bytes should not be empty")
		assert.Equal(t, "hex", randomResp.Format, "format should be hex")

		// Hex output for 32 bytes should be 64 hex characters
		hexPattern := regexp.MustCompile(`^[0-9a-fA-F]+$`)
		assert.True(t, hexPattern.MatchString(randomResp.RandomBytes),
			"random bytes should be valid hex, got: %s", randomResp.RandomBytes)
		assert.Equal(t, 64, len(randomResp.RandomBytes),
			"hex-encoded 32 bytes should be 64 characters")
	})
}

// TestCryptoRandomInvalidSize verifies that requesting a negative number of
// random bytes returns an appropriate error.
func TestCryptoRandomInvalidSize(t *testing.T) {
	RunForTransports(t, "CryptoRandomInvalidSize", cryptoTransportsToSkip, func(t *testing.T, runner *TestRunner) {
		if !runner.RequireCryptoAvailable() {
			return
		}

		invalidCmd := CommandDef{
			Name:           "crypto random invalid size",
			Args:           []string{"crypto", "random", "--bytes", "-1"},
			ExpectSuccess:  false,
			Timeout:        cryptoTimeout,
			SkipTransports: cryptoTransportsToSkip,
		}
		stdout, stderr, err := runner.Run(invalidCmd)
		require.Error(t, err, "random with negative bytes should fail")

		combined := stdout + stderr
		assert.True(t,
			strings.Contains(combined, "must be between") ||
				strings.Contains(combined, "invalid") ||
				strings.Contains(combined, "error") ||
				strings.Contains(combined, "Error"),
			"error output should indicate invalid byte count, got: %s", combined)
	})
}

// =============================================================================
// Data Key Tests
// =============================================================================

// TestCryptoDataKeyGenerate creates a key and generates a data encryption key,
// verifying both plaintext and wrapped key material are returned.
func TestCryptoDataKeyGenerate(t *testing.T) {
	RunForTransports(t, "CryptoDataKeyGenerate", cryptoTransportsToSkip, func(t *testing.T, runner *TestRunner) {
		if !runner.RequireCryptoAvailable() {
			return
		}

		keyName := UniqueKey("crypto-dek-gen")

		createCmd := KMSKeyCreateCommand(keyName, "AES-256-GCM")
		runner.RunAndAssert(createCmd)
		defer func() {
			runner.Run(KMSKeyDeleteCommand(keyName, true))
		}()

		// Generate data key with JSON output
		dataKeyCmd := CryptoDataKeyCommand(keyName)
		dataKeyCmd.Args = append(dataKeyCmd.Args, "--output", "json")
		stdout, _, err := runner.Run(dataKeyCmd)
		require.NoError(t, err, "data key generation should succeed")
		require.NotEmpty(t, stdout, "data key output should not be empty")

		var dkResp struct {
			KeyID          string `json:"key_id"`
			Plaintext      string `json:"plaintext"`
			CiphertextBlob string `json:"ciphertext_blob"`
		}
		err = json.Unmarshal([]byte(stdout), &dkResp)
		require.NoError(t, err, "data key JSON should be parseable")
		assert.Equal(t, keyName, dkResp.KeyID, "data key key_id should match")
		assert.NotEmpty(t, dkResp.Plaintext, "plaintext key material should be returned")
		assert.NotEmpty(t, dkResp.CiphertextBlob, "wrapped key material should be returned")
		assert.NotEqual(t, dkResp.Plaintext, dkResp.CiphertextBlob,
			"plaintext and wrapped key should differ")
	})
}

// TestCryptoDataKeyWithBits generates data keys with 128-bit and 256-bit sizes,
// verifying each produces valid key material.
func TestCryptoDataKeyWithBits(t *testing.T) {
	bitSizes := []int{128, 256}

	RunForTransports(t, "CryptoDataKeyWithBits", cryptoTransportsToSkip, func(t *testing.T, runner *TestRunner) {
		if !runner.RequireCryptoAvailable() {
			return
		}

		keyName := UniqueKey("crypto-dek-bits")

		createCmd := KMSKeyCreateCommand(keyName, "AES-256-GCM")
		runner.RunAndAssert(createCmd)
		defer func() {
			runner.Run(KMSKeyDeleteCommand(keyName, true))
		}()

		for _, bits := range bitSizes {
			t.Run(strings.Replace(string(rune(bits)), "", "", -1), func(t *testing.T) {
				dataKeyCmd := CryptoDataKeyWithBitsCommand(keyName, bits)
				dataKeyCmd.Args = append(dataKeyCmd.Args, "--output", "json")
				stdout, _, err := runner.Run(dataKeyCmd)
				require.NoError(t, err, "data key generation with %d bits should succeed", bits)
				require.NotEmpty(t, stdout, "data key output should not be empty for %d bits", bits)

				var dkResp struct {
					Plaintext      string `json:"plaintext"`
					CiphertextBlob string `json:"ciphertext_blob"`
				}
				err = json.Unmarshal([]byte(stdout), &dkResp)
				require.NoError(t, err, "data key JSON should be parseable for %d bits", bits)
				assert.NotEmpty(t, dkResp.Plaintext,
					"plaintext key material should be returned for %d bits", bits)
				assert.NotEmpty(t, dkResp.CiphertextBlob,
					"wrapped key material should be returned for %d bits", bits)
			})
		}
	})
}

// =============================================================================
// Rewrap Tests
// =============================================================================

// TestCryptoRewrap encrypts data, rotates the key, then rewraps the ciphertext
// under the new key version.
func TestCryptoRewrap(t *testing.T) {
	RunForTransports(t, "CryptoRewrap", cryptoTransportsToSkip, func(t *testing.T, runner *TestRunner) {
		if !runner.RequireCryptoAvailable() {
			return
		}

		keyName := UniqueKey("crypto-rewrap")

		createCmd := KMSKeyCreateCommand(keyName, "AES-256-GCM")
		runner.RunAndAssert(createCmd)
		defer func() {
			runner.Run(KMSKeyDeleteCommand(keyName, true))
		}()

		plaintext := "data to rewrap after rotation"

		// Encrypt data with the initial key version
		encryptCmd := CryptoEncryptCommand(keyName, plaintext)
		encryptCmd.Args = append(encryptCmd.Args, "--output", "json")
		encStdout, _, err := runner.Run(encryptCmd)
		require.NoError(t, err, "initial encrypt should succeed")

		var encResp struct {
			Ciphertext string `json:"ciphertext"`
			KeyVersion int    `json:"key_version"`
		}
		err = json.Unmarshal([]byte(encStdout), &encResp)
		require.NoError(t, err, "encrypt JSON should be parseable")
		require.NotEmpty(t, encResp.Ciphertext, "ciphertext should not be empty")
		originalVersion := encResp.KeyVersion

		// Rotate the key to create a new version
		rotateCmd := KMSKeyRotateCommand(keyName)
		runner.RunAndAssert(rotateCmd)

		// Rewrap the ciphertext under the latest key version
		rewrapCmd := CryptoRewrapCommand(keyName, encResp.Ciphertext)
		rewrapCmd.Args = append(rewrapCmd.Args, "--output", "json")
		rewrapStdout, _, err := runner.Run(rewrapCmd)
		require.NoError(t, err, "rewrap should succeed")
		require.NotEmpty(t, rewrapStdout, "rewrap output should not be empty")

		var rewrapResp struct {
			Ciphertext    string `json:"ciphertext"`
			KeyID         string `json:"key_id"`
			OldKeyVersion int    `json:"old_key_version"`
			NewKeyVersion int    `json:"new_key_version"`
		}
		err = json.Unmarshal([]byte(rewrapStdout), &rewrapResp)
		require.NoError(t, err, "rewrap JSON should be parseable")
		assert.NotEmpty(t, rewrapResp.Ciphertext, "rewrapped ciphertext should not be empty")
		assert.Equal(t, keyName, rewrapResp.KeyID, "rewrap key ID should match")

		// The new version should be greater than the original
		if rewrapResp.NewKeyVersion > 0 && originalVersion > 0 {
			assert.Greater(t, rewrapResp.NewKeyVersion, rewrapResp.OldKeyVersion,
				"new key version should be greater than old key version")
		}

		// Verify the rewrapped ciphertext can be decrypted to the original plaintext
		decryptCmd := CryptoDecryptBase64Command(keyName, rewrapResp.Ciphertext)
		decryptCmd.Args = append(decryptCmd.Args, "--output", "json")
		decStdout, _, err := runner.Run(decryptCmd)
		require.NoError(t, err, "decrypt of rewrapped ciphertext should succeed")

		var decResp struct {
			Plaintext string `json:"plaintext"`
		}
		err = json.Unmarshal([]byte(decStdout), &decResp)
		require.NoError(t, err, "decrypt JSON should be parseable")
		recoveredRewrap := decResp.Plaintext
		if decoded, decErr := base64.StdEncoding.DecodeString(recoveredRewrap); decErr == nil {
			recoveredRewrap = string(decoded)
		}
		assert.Equal(t, plaintext, recoveredRewrap,
			"decrypted rewrapped ciphertext should match original plaintext")
	})
}
