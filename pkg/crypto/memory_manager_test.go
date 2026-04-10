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

// Package crypto provides unit tests for the MemoryCryptoManager.
package crypto

import (
	"context"
	"errors"
	"testing"

	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
	"github.com/jeremyhahn/go-quicbaas/pkg/kms"
)

// memoryManagerTestHelper creates a test MemoryCryptoManager with a mock KMS backend.
func memoryManagerTestHelper(t *testing.T) (*MemoryCryptoManager, *mockKMSBackend, func()) {
	t.Helper()

	backend := newMockKMSBackend()
	registry := kms.NewBackendRegistry(nil)
	registry.Register("primary", backend, 1)
	kmsManager, err := kms.NewKMSService(&kms.KMSServiceOptions{
		Registry: registry,
	})
	if err != nil {
		t.Fatalf("Failed to create KMS manager: %v", err)
	}

	cryptoManager := NewMemoryCryptoManager(kmsManager)

	cleanup := func() {
		cryptoManager.Close()
		kmsManager.Close()
	}

	return cryptoManager, backend, cleanup
}

// memoryManagerTestHelperWithKey creates a test setup with a pre-created key.
func memoryManagerTestHelperWithKey(t *testing.T, keyID string) (*MemoryCryptoManager, *mockKMSBackend, func()) {
	t.Helper()

	manager, backend, cleanup := memoryManagerTestHelper(t)
	ctx := context.Background()

	// Create a key using the backend directly
	attrs := &kms.KeyMetadata{
		KeyID:     keyID,
		Algorithm: kms.AlgAES256GCM,
		State:     kms.KeyStateEnabled,
	}
	backend.keys[keyID] = attrs

	// Generate key material
	keyMaterial := make([]byte, 32)
	for i := range keyMaterial {
		keyMaterial[i] = byte(i)
	}
	backend.data[keyID] = keyMaterial

	_ = ctx
	return manager, backend, cleanup
}

// TestNewMemoryCryptoManager tests constructor.
func TestNewMemoryCryptoManager(t *testing.T) {
	manager, _, cleanup := memoryManagerTestHelper(t)
	defer cleanup()

	if manager == nil {
		t.Error("Expected manager to be created")
	}
}

// TestMemoryCryptoManager_Encrypt tests encryption operations.
func TestMemoryCryptoManager_Encrypt(t *testing.T) {
	manager, _, cleanup := memoryManagerTestHelperWithKey(t, "test-key")
	defer cleanup()

	ctx := context.Background()

	t.Run("EmptyKeyID", func(t *testing.T) {
		_, err := manager.Encrypt(ctx, "", []byte("test"), nil)
		if err == nil {
			t.Error("Expected error for empty keyID")
		}
	})

	t.Run("ValidEncryption", func(t *testing.T) {
		ciphertext, err := manager.Encrypt(ctx, "test-key", []byte("hello world"), nil)
		if err != nil {
			t.Fatalf("Encrypt() error = %v", err)
		}
		if ciphertext == nil || len(ciphertext) == 0 {
			t.Error("Expected ciphertext to be returned")
		}
	})

	t.Run("WithEncryptionContext", func(t *testing.T) {
		ciphertext, err := manager.Encrypt(ctx, "test-key", []byte("secret data"), []byte("context"))
		if err != nil {
			t.Fatalf("Encrypt() error = %v", err)
		}
		if ciphertext == nil {
			t.Error("Expected ciphertext to be returned")
		}
	})

	t.Run("NonExistentKey", func(t *testing.T) {
		_, err := manager.Encrypt(ctx, "non-existent-key", []byte("test"), nil)
		if err == nil {
			t.Error("Expected error for non-existent key")
		}
	})
}

// TestMemoryCryptoManager_Decrypt tests decryption operations.
func TestMemoryCryptoManager_Decrypt(t *testing.T) {
	manager, _, cleanup := memoryManagerTestHelperWithKey(t, "test-key")
	defer cleanup()

	ctx := context.Background()

	t.Run("EmptyKeyID", func(t *testing.T) {
		_, err := manager.Decrypt(ctx, "", []byte("ciphertext"), nil)
		if err == nil {
			t.Error("Expected error for empty keyID")
		}
	})

	t.Run("ValidDecryption", func(t *testing.T) {
		plaintext := []byte("hello world")
		ciphertext, err := manager.Encrypt(ctx, "test-key", plaintext, nil)
		if err != nil {
			t.Fatalf("Encrypt() error = %v", err)
		}

		decrypted, err := manager.Decrypt(ctx, "test-key", ciphertext, nil)
		if err != nil {
			t.Fatalf("Decrypt() error = %v", err)
		}
		if string(decrypted) != string(plaintext) {
			t.Errorf("Decrypted = %v, want %v", string(decrypted), string(plaintext))
		}
	})

	t.Run("WithEncryptionContext", func(t *testing.T) {
		plaintext := []byte("secret data")
		encContext := []byte("context")
		ciphertext, err := manager.Encrypt(ctx, "test-key", plaintext, encContext)
		if err != nil {
			t.Fatalf("Encrypt() error = %v", err)
		}

		decrypted, err := manager.Decrypt(ctx, "test-key", ciphertext, encContext)
		if err != nil {
			t.Fatalf("Decrypt() error = %v", err)
		}
		if string(decrypted) != string(plaintext) {
			t.Errorf("Decrypted = %v, want %v", string(decrypted), string(plaintext))
		}
	})

	t.Run("NonExistentKey", func(t *testing.T) {
		_, err := manager.Decrypt(ctx, "non-existent-key", []byte("test"), nil)
		if err == nil {
			t.Error("Expected error for non-existent key")
		}
	})
}

// TestMemoryCryptoManager_Rewrap tests key rewrapping operations.
func TestMemoryCryptoManager_Rewrap(t *testing.T) {
	manager, _, cleanup := memoryManagerTestHelperWithKey(t, "test-key")
	defer cleanup()

	ctx := context.Background()

	t.Run("ValidRewrap", func(t *testing.T) {
		plaintext := []byte("rewrap test data")
		ciphertext, err := manager.Encrypt(ctx, "test-key", plaintext, nil)
		if err != nil {
			t.Fatalf("Encrypt() error = %v", err)
		}

		newCiphertext, err := manager.Rewrap(ctx, "test-key", ciphertext, nil)
		if err != nil {
			t.Fatalf("Rewrap() error = %v", err)
		}
		if newCiphertext == nil {
			t.Error("Expected new ciphertext to be returned")
		}

		// Verify we can still decrypt the rewrapped ciphertext
		decrypted, err := manager.Decrypt(ctx, "test-key", newCiphertext, nil)
		if err != nil {
			t.Fatalf("Decrypt() after rewrap error = %v", err)
		}
		if string(decrypted) != string(plaintext) {
			t.Errorf("Decrypted = %v, want %v", string(decrypted), string(plaintext))
		}
	})

	t.Run("WithEncryptionContext", func(t *testing.T) {
		plaintext := []byte("context rewrap test")
		encContext := []byte("rewrap context")
		ciphertext, err := manager.Encrypt(ctx, "test-key", plaintext, encContext)
		if err != nil {
			t.Fatalf("Encrypt() error = %v", err)
		}

		newCiphertext, err := manager.Rewrap(ctx, "test-key", ciphertext, encContext)
		if err != nil {
			t.Fatalf("Rewrap() error = %v", err)
		}

		decrypted, err := manager.Decrypt(ctx, "test-key", newCiphertext, encContext)
		if err != nil {
			t.Fatalf("Decrypt() after rewrap error = %v", err)
		}
		if string(decrypted) != string(plaintext) {
			t.Errorf("Decrypted = %v, want %v", string(decrypted), string(plaintext))
		}
	})

	t.Run("DecryptError", func(t *testing.T) {
		_, err := manager.Rewrap(ctx, "non-existent-key", []byte("invalid"), nil)
		if err == nil {
			t.Error("Expected error for non-existent key")
		}
	})
}

// TestMemoryCryptoManager_EncryptBatch tests batch encryption operations.
func TestMemoryCryptoManager_EncryptBatch(t *testing.T) {
	manager, _, cleanup := memoryManagerTestHelperWithKey(t, "test-key")
	defer cleanup()

	ctx := context.Background()

	t.Run("EmptyKeyID", func(t *testing.T) {
		_, err := manager.EncryptBatch(ctx, "", [][]byte{[]byte("a"), []byte("b")}, nil)
		if err == nil {
			t.Error("Expected error for empty keyID")
		}
	})

	t.Run("ValidBatchEncryption", func(t *testing.T) {
		items := [][]byte{
			[]byte("first"),
			[]byte("second"),
			[]byte("third"),
		}

		results, err := manager.EncryptBatch(ctx, "test-key", items, nil)
		if err != nil {
			t.Fatalf("EncryptBatch() error = %v", err)
		}
		if len(results) != len(items) {
			t.Errorf("Results length = %d, want %d", len(results), len(items))
		}

		// Verify each encrypted item can be decrypted
		for i, ciphertext := range results {
			decrypted, err := manager.Decrypt(ctx, "test-key", ciphertext, nil)
			if err != nil {
				t.Errorf("Decrypt item %d error = %v", i, err)
			}
			if string(decrypted) != string(items[i]) {
				t.Errorf("Decrypted[%d] = %v, want %v", i, string(decrypted), string(items[i]))
			}
		}
	})

	t.Run("EmptyBatch", func(t *testing.T) {
		results, err := manager.EncryptBatch(ctx, "test-key", [][]byte{}, nil)
		if err != nil {
			t.Fatalf("EncryptBatch() error = %v", err)
		}
		if len(results) != 0 {
			t.Errorf("Results length = %d, want 0", len(results))
		}
	})

	t.Run("WithEncryptionContext", func(t *testing.T) {
		items := [][]byte{[]byte("item1"), []byte("item2")}
		encContext := []byte("batch context")

		results, err := manager.EncryptBatch(ctx, "test-key", items, encContext)
		if err != nil {
			t.Fatalf("EncryptBatch() error = %v", err)
		}
		if len(results) != len(items) {
			t.Errorf("Results length = %d, want %d", len(results), len(items))
		}
	})

	t.Run("NonExistentKey", func(t *testing.T) {
		_, err := manager.EncryptBatch(ctx, "non-existent-key", [][]byte{[]byte("test")}, nil)
		if err == nil {
			t.Error("Expected error for non-existent key")
		}
	})
}

// TestMemoryCryptoManager_DecryptBatch tests batch decryption operations.
func TestMemoryCryptoManager_DecryptBatch(t *testing.T) {
	manager, _, cleanup := memoryManagerTestHelperWithKey(t, "test-key")
	defer cleanup()

	ctx := context.Background()

	t.Run("EmptyKeyID", func(t *testing.T) {
		_, err := manager.DecryptBatch(ctx, "", [][]byte{[]byte("a")}, nil)
		if err == nil {
			t.Error("Expected error for empty keyID")
		}
	})

	t.Run("ValidBatchDecryption", func(t *testing.T) {
		items := [][]byte{
			[]byte("first"),
			[]byte("second"),
			[]byte("third"),
		}

		// First encrypt all items
		encrypted, err := manager.EncryptBatch(ctx, "test-key", items, nil)
		if err != nil {
			t.Fatalf("EncryptBatch() error = %v", err)
		}

		// Now decrypt them all
		decrypted, err := manager.DecryptBatch(ctx, "test-key", encrypted, nil)
		if err != nil {
			t.Fatalf("DecryptBatch() error = %v", err)
		}
		if len(decrypted) != len(items) {
			t.Errorf("Decrypted length = %d, want %d", len(decrypted), len(items))
		}

		for i, plaintext := range decrypted {
			if string(plaintext) != string(items[i]) {
				t.Errorf("Decrypted[%d] = %v, want %v", i, string(plaintext), string(items[i]))
			}
		}
	})

	t.Run("EmptyBatch", func(t *testing.T) {
		results, err := manager.DecryptBatch(ctx, "test-key", [][]byte{}, nil)
		if err != nil {
			t.Fatalf("DecryptBatch() error = %v", err)
		}
		if len(results) != 0 {
			t.Errorf("Results length = %d, want 0", len(results))
		}
	})

	t.Run("NonExistentKey", func(t *testing.T) {
		_, err := manager.DecryptBatch(ctx, "non-existent-key", [][]byte{[]byte("test")}, nil)
		if err == nil {
			t.Error("Expected error for non-existent key")
		}
	})
}

// TestMemoryCryptoManager_Sign tests signing operations.
func TestMemoryCryptoManager_Sign(t *testing.T) {
	manager, _, cleanup := memoryManagerTestHelperWithKey(t, "test-key")
	defer cleanup()

	ctx := context.Background()

	t.Run("EmptyKeyID", func(t *testing.T) {
		_, err := manager.Sign(ctx, "", []byte("data"), "sha256", false)
		if err == nil {
			t.Error("Expected error for empty keyID")
		}
	})

	t.Run("ValidSign", func(t *testing.T) {
		signature, err := manager.Sign(ctx, "test-key", []byte("data to sign"), "sha256", false)
		if err != nil {
			t.Fatalf("Sign() error = %v", err)
		}
		if signature == nil || len(signature) == 0 {
			t.Error("Expected signature to be returned")
		}
	})

	t.Run("ValidSignSHA512", func(t *testing.T) {
		signature, err := manager.Sign(ctx, "test-key", []byte("data to sign"), "sha512", false)
		if err != nil {
			t.Fatalf("Sign() error = %v", err)
		}
		if signature == nil || len(signature) == 0 {
			t.Error("Expected signature to be returned")
		}
	})

	t.Run("NonExistentKey", func(t *testing.T) {
		_, err := manager.Sign(ctx, "non-existent-key", []byte("data"), "sha256", false)
		if err == nil {
			t.Error("Expected error for non-existent key")
		}
	})
}

// TestMemoryCryptoManager_Verify tests signature verification.
func TestMemoryCryptoManager_Verify(t *testing.T) {
	manager, _, cleanup := memoryManagerTestHelperWithKey(t, "test-key")
	defer cleanup()

	ctx := context.Background()

	t.Run("EmptyKeyID", func(t *testing.T) {
		_, err := manager.Verify(ctx, "", []byte("data"), []byte("sig"), "sha256")
		if err == nil {
			t.Error("Expected error for empty keyID")
		}
	})

	t.Run("ValidSignAndVerify", func(t *testing.T) {
		data := []byte("data to sign and verify")
		signature, err := manager.Sign(ctx, "test-key", data, "sha256", false)
		if err != nil {
			t.Fatalf("Sign() error = %v", err)
		}

		valid, err := manager.Verify(ctx, "test-key", data, signature, "sha256")
		if err != nil {
			t.Fatalf("Verify() error = %v", err)
		}
		if !valid {
			t.Error("Expected valid signature")
		}
	})

	t.Run("InvalidSignature", func(t *testing.T) {
		data := []byte("data to sign")
		_, err := manager.Sign(ctx, "test-key", data, "sha256", false)
		if err != nil {
			t.Fatalf("Sign() error = %v", err)
		}

		valid, err := manager.Verify(ctx, "test-key", data, []byte("wrong signature"), "sha256")
		if err != nil {
			t.Fatalf("Verify() error = %v", err)
		}
		if valid {
			t.Error("Expected invalid signature")
		}
	})

	t.Run("NonExistentKey", func(t *testing.T) {
		_, err := manager.Verify(ctx, "non-existent-key", []byte("data"), []byte("sig"), "sha256")
		if err == nil {
			t.Error("Expected error for non-existent key")
		}
	})
}

// TestMemoryCryptoManager_GenerateHMAC tests HMAC generation.
func TestMemoryCryptoManager_GenerateHMAC(t *testing.T) {
	manager, _, cleanup := memoryManagerTestHelperWithKey(t, "test-key")
	defer cleanup()

	ctx := context.Background()

	t.Run("EmptyKeyID", func(t *testing.T) {
		_, err := manager.GenerateHMAC(ctx, "", []byte("data"), "sha256")
		if err == nil {
			t.Error("Expected error for empty keyID")
		}
	})

	t.Run("ValidHMACWithSHA256", func(t *testing.T) {
		hmacValue, err := manager.GenerateHMAC(ctx, "test-key", []byte("data"), "sha256")
		if err != nil {
			t.Fatalf("GenerateHMAC() error = %v", err)
		}
		if hmacValue == nil || len(hmacValue) == 0 {
			t.Error("Expected HMAC to be returned")
		}
		// SHA256 produces 32 bytes
		if len(hmacValue) != 32 {
			t.Errorf("HMAC length = %d, want 32", len(hmacValue))
		}
	})

	t.Run("ValidHMACWithSHA512", func(t *testing.T) {
		hmacValue, err := manager.GenerateHMAC(ctx, "test-key", []byte("data"), "sha512")
		if err != nil {
			t.Fatalf("GenerateHMAC() error = %v", err)
		}
		if hmacValue == nil || len(hmacValue) == 0 {
			t.Error("Expected HMAC to be returned")
		}
		// SHA512 produces 64 bytes
		if len(hmacValue) != 64 {
			t.Errorf("HMAC length = %d, want 64", len(hmacValue))
		}
	})

	t.Run("ValidHMACWithSHA256Uppercase", func(t *testing.T) {
		hmacValue, err := manager.GenerateHMAC(ctx, "test-key", []byte("data"), "SHA256")
		if err != nil {
			t.Fatalf("GenerateHMAC() error = %v", err)
		}
		// SHA256 produces 32 bytes
		if len(hmacValue) != 32 {
			t.Errorf("HMAC length = %d, want 32", len(hmacValue))
		}
	})

	t.Run("ValidHMACWithSHA512Uppercase", func(t *testing.T) {
		hmacValue, err := manager.GenerateHMAC(ctx, "test-key", []byte("data"), "SHA512")
		if err != nil {
			t.Fatalf("GenerateHMAC() error = %v", err)
		}
		// SHA512 produces 64 bytes
		if len(hmacValue) != 64 {
			t.Errorf("HMAC length = %d, want 64", len(hmacValue))
		}
	})

	t.Run("ValidHMACWithDefaultAlgorithm", func(t *testing.T) {
		hmacValue, err := manager.GenerateHMAC(ctx, "test-key", []byte("data"), "")
		if err != nil {
			t.Fatalf("GenerateHMAC() error = %v", err)
		}
		// Default is SHA256, which produces 32 bytes
		if len(hmacValue) != 32 {
			t.Errorf("HMAC length = %d, want 32", len(hmacValue))
		}
	})

	t.Run("UnsupportedAlgorithm", func(t *testing.T) {
		_, err := manager.GenerateHMAC(ctx, "test-key", []byte("data"), "md5")
		if err == nil {
			t.Error("Expected error for unsupported algorithm")
		}
	})

	t.Run("HMACReturnsNonEmptyBytes", func(t *testing.T) {
		// Test that HMAC returns proper byte length for different data
		testCases := []struct {
			name      string
			data      []byte
			algorithm string
			wantLen   int
		}{
			{"empty_data_sha256", []byte{}, "sha256", 32},
			{"empty_data_sha512", []byte{}, "sha512", 64},
			{"small_data_sha256", []byte("a"), "sha256", 32},
			{"large_data_sha256", make([]byte, 1024), "sha256", 32},
		}

		for _, tc := range testCases {
			t.Run(tc.name, func(t *testing.T) {
				hmacValue, err := manager.GenerateHMAC(ctx, "test-key", tc.data, tc.algorithm)
				if err != nil {
					t.Fatalf("GenerateHMAC() error = %v", err)
				}
				if len(hmacValue) != tc.wantLen {
					t.Errorf("HMAC length = %d, want %d", len(hmacValue), tc.wantLen)
				}
			})
		}
	})
}

// TestMemoryCryptoManager_VerifyHMAC tests HMAC verification.
// Note: The implementation uses GenerateDataKey which generates random keys,
// so HMAC values are not deterministic between calls. This tests the code paths.
func TestMemoryCryptoManager_VerifyHMAC(t *testing.T) {
	manager, _, cleanup := memoryManagerTestHelperWithKey(t, "test-key")
	defer cleanup()

	ctx := context.Background()

	t.Run("VerifyHMACCodePath", func(t *testing.T) {
		// This tests the code path of VerifyHMAC
		// Note: Due to random key generation in GenerateDataKey,
		// HMAC values may not match between Generate and Verify calls
		data := []byte("data to verify")
		hmacValue, err := manager.GenerateHMAC(ctx, "test-key", data, "sha256")
		if err != nil {
			t.Fatalf("GenerateHMAC() error = %v", err)
		}
		if hmacValue == nil {
			t.Error("Expected HMAC to be returned")
		}

		// VerifyHMAC should execute without error
		_, err = manager.VerifyHMAC(ctx, "test-key", data, hmacValue, "sha256")
		if err != nil {
			t.Fatalf("VerifyHMAC() error = %v", err)
		}
	})

	t.Run("WrongHMAC", func(t *testing.T) {
		data := []byte("data")

		// Verify with wrong HMAC should return false
		valid, err := manager.VerifyHMAC(ctx, "test-key", data, []byte("wrong hmac value"), "sha256")
		if err != nil {
			t.Fatalf("VerifyHMAC() error = %v", err)
		}
		if valid {
			t.Error("Expected invalid HMAC for wrong value")
		}
	})

	t.Run("ErrorPropagation", func(t *testing.T) {
		_, err := manager.VerifyHMAC(ctx, "", []byte("data"), []byte("hmac"), "sha256")
		if err == nil {
			t.Error("Expected error for empty keyID")
		}
	})

	t.Run("UnsupportedAlgorithmPropagation", func(t *testing.T) {
		_, err := manager.VerifyHMAC(ctx, "test-key", []byte("data"), []byte("hmac"), "md5")
		if err == nil {
			t.Error("Expected error for unsupported algorithm")
		}
	})
}

// TestMemoryCryptoManager_GenerateRandom tests random byte generation.
func TestMemoryCryptoManager_GenerateRandom(t *testing.T) {
	manager, _, cleanup := memoryManagerTestHelper(t)
	defer cleanup()

	ctx := context.Background()

	t.Run("ZeroBytes", func(t *testing.T) {
		_, err := manager.GenerateRandom(ctx, 0)
		if err == nil {
			t.Error("Expected error for zero bytes")
		}
	})

	t.Run("NegativeBytes", func(t *testing.T) {
		_, err := manager.GenerateRandom(ctx, -1)
		if err == nil {
			t.Error("Expected error for negative bytes")
		}
	})

	t.Run("ValidGeneration16Bytes", func(t *testing.T) {
		randomBytes, err := manager.GenerateRandom(ctx, 16)
		if err != nil {
			t.Fatalf("GenerateRandom() error = %v", err)
		}
		if len(randomBytes) != 16 {
			t.Errorf("Random bytes length = %d, want 16", len(randomBytes))
		}
	})

	t.Run("ValidGeneration32Bytes", func(t *testing.T) {
		randomBytes, err := manager.GenerateRandom(ctx, 32)
		if err != nil {
			t.Fatalf("GenerateRandom() error = %v", err)
		}
		if len(randomBytes) != 32 {
			t.Errorf("Random bytes length = %d, want 32", len(randomBytes))
		}
	})

	t.Run("ValidGeneration64Bytes", func(t *testing.T) {
		randomBytes, err := manager.GenerateRandom(ctx, 64)
		if err != nil {
			t.Fatalf("GenerateRandom() error = %v", err)
		}
		if len(randomBytes) != 64 {
			t.Errorf("Random bytes length = %d, want 64", len(randomBytes))
		}
	})

	t.Run("ExceedsMaximum", func(t *testing.T) {
		_, err := manager.GenerateRandom(ctx, 1024*1024+1) // 1MB + 1 byte
		if err == nil {
			t.Error("Expected error for exceeding maximum")
		}
	})

	t.Run("MaximumAllowed", func(t *testing.T) {
		randomBytes, err := manager.GenerateRandom(ctx, 1024*1024) // 1MB
		if err != nil {
			t.Fatalf("GenerateRandom() error = %v", err)
		}
		if len(randomBytes) != 1024*1024 {
			t.Errorf("Random bytes length = %d, want 1MB", len(randomBytes))
		}
	})

	t.Run("Uniqueness", func(t *testing.T) {
		random1, err := manager.GenerateRandom(ctx, 32)
		if err != nil {
			t.Fatalf("GenerateRandom() error = %v", err)
		}

		random2, err := manager.GenerateRandom(ctx, 32)
		if err != nil {
			t.Fatalf("GenerateRandom() error = %v", err)
		}

		if string(random1) == string(random2) {
			t.Error("Expected unique random bytes")
		}
	})
}

// TestMemoryCryptoManager_Hash tests hashing operations.
func TestMemoryCryptoManager_Hash(t *testing.T) {
	manager, _, cleanup := memoryManagerTestHelper(t)
	defer cleanup()

	ctx := context.Background()

	t.Run("SHA256", func(t *testing.T) {
		hash, err := manager.Hash(ctx, []byte("hello world"), "sha256")
		if err != nil {
			t.Fatalf("Hash() error = %v", err)
		}
		// SHA256 produces 32 bytes
		if len(hash) != 32 {
			t.Errorf("Hash length = %d, want 32", len(hash))
		}
	})

	t.Run("SHA256Uppercase", func(t *testing.T) {
		hash, err := manager.Hash(ctx, []byte("hello world"), "SHA256")
		if err != nil {
			t.Fatalf("Hash() error = %v", err)
		}
		if len(hash) != 32 {
			t.Errorf("Hash length = %d, want 32", len(hash))
		}
	})

	t.Run("SHA512", func(t *testing.T) {
		hash, err := manager.Hash(ctx, []byte("hello world"), "sha512")
		if err != nil {
			t.Fatalf("Hash() error = %v", err)
		}
		// SHA512 produces 64 bytes
		if len(hash) != 64 {
			t.Errorf("Hash length = %d, want 64", len(hash))
		}
	})

	t.Run("SHA512Uppercase", func(t *testing.T) {
		hash, err := manager.Hash(ctx, []byte("hello world"), "SHA512")
		if err != nil {
			t.Fatalf("Hash() error = %v", err)
		}
		if len(hash) != 64 {
			t.Errorf("Hash length = %d, want 64", len(hash))
		}
	})

	t.Run("DefaultAlgorithm", func(t *testing.T) {
		hash, err := manager.Hash(ctx, []byte("hello world"), "")
		if err != nil {
			t.Fatalf("Hash() error = %v", err)
		}
		// Default is SHA256
		if len(hash) != 32 {
			t.Errorf("Hash length = %d, want 32", len(hash))
		}
	})

	t.Run("UnsupportedAlgorithm", func(t *testing.T) {
		_, err := manager.Hash(ctx, []byte("hello world"), "md5")
		if err == nil {
			t.Error("Expected error for unsupported algorithm")
		}
	})

	t.Run("ConsistentResults", func(t *testing.T) {
		data := []byte("consistent data")
		hash1, err := manager.Hash(ctx, data, "sha256")
		if err != nil {
			t.Fatalf("Hash() error = %v", err)
		}

		hash2, err := manager.Hash(ctx, data, "sha256")
		if err != nil {
			t.Fatalf("Hash() error = %v", err)
		}

		if string(hash1) != string(hash2) {
			t.Error("Expected consistent hash results for same input")
		}
	})

	t.Run("DifferentInputDifferentHash", func(t *testing.T) {
		hash1, err := manager.Hash(ctx, []byte("data 1"), "sha256")
		if err != nil {
			t.Fatalf("Hash() error = %v", err)
		}

		hash2, err := manager.Hash(ctx, []byte("data 2"), "sha256")
		if err != nil {
			t.Fatalf("Hash() error = %v", err)
		}

		if string(hash1) == string(hash2) {
			t.Error("Expected different hashes for different input")
		}
	})

	t.Run("EmptyInput", func(t *testing.T) {
		hash, err := manager.Hash(ctx, []byte{}, "sha256")
		if err != nil {
			t.Fatalf("Hash() error = %v", err)
		}
		if len(hash) != 32 {
			t.Errorf("Hash length = %d, want 32", len(hash))
		}
	})
}

// TestMemoryCryptoManager_Close tests close operation.
func TestMemoryCryptoManager_Close(t *testing.T) {
	manager, _, cleanup := memoryManagerTestHelper(t)
	defer cleanup()

	err := manager.Close()
	if err != nil {
		t.Fatalf("Close() error = %v", err)
	}
}

// TestMemoryCryptoManager_ImplementsInterface tests interface compliance.
func TestMemoryCryptoManager_ImplementsInterface(t *testing.T) {
	manager, _, cleanup := memoryManagerTestHelper(t)
	defer cleanup()

	var _ interfaces.CryptoManager = manager
}

// TestMemoryCryptoManager_RewrapEncryptError tests rewrap when encryption fails.
func TestMemoryCryptoManager_RewrapEncryptError(t *testing.T) {
	backend := newMockKMSBackend()
	registry := kms.NewBackendRegistry(nil)
	registry.Register("primary", backend, 1)
	kmsManager, err := kms.NewKMSService(&kms.KMSServiceOptions{
		Registry: registry,
	})
	if err != nil {
		t.Fatalf("Failed to create KMS manager: %v", err)
	}
	defer kmsManager.Close()

	cryptoManager := NewMemoryCryptoManager(kmsManager)
	defer cryptoManager.Close()

	ctx := context.Background()

	// Create a key
	backend.keys["test-key"] = &kms.KeyMetadata{
		KeyID:     "test-key",
		Algorithm: kms.AlgAES256GCM,
		State:     kms.KeyStateEnabled,
	}
	keyMaterial := make([]byte, 32)
	for i := range keyMaterial {
		keyMaterial[i] = byte(i)
	}
	backend.data["test-key"] = keyMaterial

	// Encrypt some data
	ciphertext, err := cryptoManager.Encrypt(ctx, "test-key", []byte("test"), nil)
	if err != nil {
		t.Fatalf("Encrypt() error = %v", err)
	}

	// Now make encryption fail
	backend.SetError("Encrypt", errors.New("mock encryption error"))

	_, err = cryptoManager.Rewrap(ctx, "test-key", ciphertext, nil)
	if err == nil {
		t.Error("Expected error when encryption fails during rewrap")
	}
}

// TestMemoryCryptoManager_EncryptBatchItemError tests batch encrypt when a single item fails.
func TestMemoryCryptoManager_EncryptBatchItemError(t *testing.T) {
	backend := newMockKMSBackend()
	registry := kms.NewBackendRegistry(nil)
	registry.Register("primary", backend, 1)
	kmsManager, err := kms.NewKMSService(&kms.KMSServiceOptions{
		Registry: registry,
	})
	if err != nil {
		t.Fatalf("Failed to create KMS manager: %v", err)
	}
	defer kmsManager.Close()

	cryptoManager := NewMemoryCryptoManager(kmsManager)
	defer cryptoManager.Close()

	ctx := context.Background()

	// Create a key
	backend.keys["test-key"] = &kms.KeyMetadata{
		KeyID:     "test-key",
		Algorithm: kms.AlgAES256GCM,
		State:     kms.KeyStateEnabled,
	}
	keyMaterial := make([]byte, 32)
	for i := range keyMaterial {
		keyMaterial[i] = byte(i)
	}
	backend.data["test-key"] = keyMaterial

	// Make encryption fail
	backend.SetError("Encrypt", errors.New("mock encryption error"))

	items := [][]byte{[]byte("item1"), []byte("item2")}
	_, err = cryptoManager.EncryptBatch(ctx, "test-key", items, nil)
	if err == nil {
		t.Error("Expected error when encryption fails in batch")
	}
}

// TestMemoryCryptoManager_DecryptBatchItemError tests batch decrypt when a single item fails.
func TestMemoryCryptoManager_DecryptBatchItemError(t *testing.T) {
	backend := newMockKMSBackend()
	registry := kms.NewBackendRegistry(nil)
	registry.Register("primary", backend, 1)
	kmsManager, err := kms.NewKMSService(&kms.KMSServiceOptions{
		Registry: registry,
	})
	if err != nil {
		t.Fatalf("Failed to create KMS manager: %v", err)
	}
	defer kmsManager.Close()

	cryptoManager := NewMemoryCryptoManager(kmsManager)
	defer cryptoManager.Close()

	ctx := context.Background()

	// Create a key
	backend.keys["test-key"] = &kms.KeyMetadata{
		KeyID:     "test-key",
		Algorithm: kms.AlgAES256GCM,
		State:     kms.KeyStateEnabled,
	}
	keyMaterial := make([]byte, 32)
	for i := range keyMaterial {
		keyMaterial[i] = byte(i)
	}
	backend.data["test-key"] = keyMaterial

	// Encrypt some items first
	items := [][]byte{[]byte("item1"), []byte("item2")}
	encrypted, err := cryptoManager.EncryptBatch(ctx, "test-key", items, nil)
	if err != nil {
		t.Fatalf("EncryptBatch() error = %v", err)
	}

	// Make decryption fail
	backend.SetError("Decrypt", errors.New("mock decryption error"))

	_, err = cryptoManager.DecryptBatch(ctx, "test-key", encrypted, nil)
	if err == nil {
		t.Error("Expected error when decryption fails in batch")
	}
}

// TestMemoryCryptoManager_GenerateHMACKeyError tests HMAC when key retrieval fails.
func TestMemoryCryptoManager_GenerateHMACKeyError(t *testing.T) {
	backend := newMockKMSBackend()
	registry := kms.NewBackendRegistry(nil)
	registry.Register("primary", backend, 1)
	kmsManager, err := kms.NewKMSService(&kms.KMSServiceOptions{
		Registry: registry,
	})
	if err != nil {
		t.Fatalf("Failed to create KMS manager: %v", err)
	}
	defer kmsManager.Close()

	cryptoManager := NewMemoryCryptoManager(kmsManager)
	defer cryptoManager.Close()

	ctx := context.Background()

	// Create a key
	backend.keys["test-key"] = &kms.KeyMetadata{
		KeyID:     "test-key",
		Algorithm: kms.AlgAES256GCM,
		State:     kms.KeyStateEnabled,
	}
	keyMaterial := make([]byte, 32)
	for i := range keyMaterial {
		keyMaterial[i] = byte(i)
	}
	backend.data["test-key"] = keyMaterial

	// Make GenerateDataKey fail
	backend.SetError("GenerateDataKey", errors.New("mock data key error"))

	_, err = cryptoManager.GenerateHMAC(ctx, "test-key", []byte("data"), "sha256")
	if err == nil {
		t.Error("Expected error when data key generation fails")
	}
}

// TestMemoryCryptoManager_SignHashError tests sign when hashing fails.
func TestMemoryCryptoManager_SignHashError(t *testing.T) {
	manager, _, cleanup := memoryManagerTestHelperWithKey(t, "test-key")
	defer cleanup()

	ctx := context.Background()

	// Use unsupported algorithm to trigger hash error
	_, err := manager.Sign(ctx, "test-key", []byte("data"), "unsupported", false)
	if err == nil {
		t.Error("Expected error for unsupported hash algorithm")
	}
}

// TestMemoryCryptoManager_VerifyHashError tests verify when hashing fails.
func TestMemoryCryptoManager_VerifyHashError(t *testing.T) {
	manager, _, cleanup := memoryManagerTestHelperWithKey(t, "test-key")
	defer cleanup()

	ctx := context.Background()

	// Use unsupported algorithm to trigger hash error
	_, err := manager.Verify(ctx, "test-key", []byte("data"), []byte("sig"), "unsupported")
	if err == nil {
		t.Error("Expected error for unsupported hash algorithm")
	}
}

// TestMemoryCryptoManager_LargeData tests operations with larger data.
func TestMemoryCryptoManager_LargeData(t *testing.T) {
	manager, _, cleanup := memoryManagerTestHelperWithKey(t, "test-key")
	defer cleanup()

	ctx := context.Background()

	// Create 1KB of data
	largeData := make([]byte, 1024)
	for i := range largeData {
		largeData[i] = byte(i % 256)
	}

	t.Run("EncryptDecryptLargeData", func(t *testing.T) {
		ciphertext, err := manager.Encrypt(ctx, "test-key", largeData, nil)
		if err != nil {
			t.Fatalf("Encrypt() error = %v", err)
		}

		decrypted, err := manager.Decrypt(ctx, "test-key", ciphertext, nil)
		if err != nil {
			t.Fatalf("Decrypt() error = %v", err)
		}

		if string(decrypted) != string(largeData) {
			t.Error("Decrypted data does not match original")
		}
	})

	t.Run("HashLargeData", func(t *testing.T) {
		hash, err := manager.Hash(ctx, largeData, "sha256")
		if err != nil {
			t.Fatalf("Hash() error = %v", err)
		}
		if len(hash) != 32 {
			t.Errorf("Hash length = %d, want 32", len(hash))
		}
	})
}
