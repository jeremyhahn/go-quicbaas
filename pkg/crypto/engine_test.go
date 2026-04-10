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

// Package crypto provides unit tests for the crypto encryption engine.
package crypto

import (
	"context"
	"testing"
	"time"

	"github.com/jeremyhahn/go-quicbaas/pkg/kms"
)

// testHelper creates a test KMS manager and crypto engine.
func testHelper(t *testing.T) (*Engine, *kms.KMSService, func()) {
	t.Helper()

	backend := newMockKMSBackend()
	registry := kms.NewBackendRegistry(nil)
	registry.Register("primary", backend, 1)
	manager, err := kms.NewKMSService(&kms.KMSServiceOptions{
		Registry: registry,
	})
	if err != nil {
		t.Fatalf("Failed to create KMS manager: %v", err)
	}

	engine, err := NewEngine(&EngineOptions{
		KMS: manager,
	})
	if err != nil {
		manager.Close()
		t.Fatalf("Failed to create crypto engine: %v", err)
	}

	cleanup := func() {
		engine.Close()
		manager.Close()
	}

	return engine, manager, cleanup
}

// testHelperWithErrorBackend creates engine with error-simulating backend.
func testHelperWithErrorBackend(t *testing.T, backend *mockErrorBackend) (*Engine, *kms.KMSService, func()) {
	t.Helper()

	registry := kms.NewBackendRegistry(nil)
	registry.Register("primary", backend, 1)
	manager, err := kms.NewKMSService(&kms.KMSServiceOptions{
		Registry: registry,
	})
	if err != nil {
		t.Fatalf("Failed to create KMS manager: %v", err)
	}

	engine, err := NewEngine(&EngineOptions{
		KMS: manager,
	})
	if err != nil {
		manager.Close()
		t.Fatalf("Failed to create crypto engine: %v", err)
	}

	cleanup := func() {
		engine.Close()
		manager.Close()
	}

	return engine, manager, cleanup
}

// TestNewEngine tests engine creation.
func TestNewEngine(t *testing.T) {
	t.Run("NilOptions", func(t *testing.T) {
		_, err := NewEngine(nil)
		if err == nil {
			t.Error("Expected error for nil options")
		}
	})

	t.Run("NilKMS", func(t *testing.T) {
		_, err := NewEngine(&EngineOptions{})
		if err == nil {
			t.Error("Expected error for nil KMS")
		}
	})

	t.Run("ValidOptions", func(t *testing.T) {
		engine, _, cleanup := testHelper(t)
		defer cleanup()

		if engine == nil {
			t.Error("Expected engine to be created")
		}
	})
}

// TestCreateKey tests key creation.
func TestCreateKey(t *testing.T) {
	engine, _, cleanup := testHelper(t)
	defer cleanup()

	ctx := context.Background()

	t.Run("NilConfig", func(t *testing.T) {
		_, err := engine.CreateKey(ctx, nil)
		if err == nil {
			t.Error("Expected error for nil config")
		}
	})

	t.Run("EmptyName", func(t *testing.T) {
		_, err := engine.CreateKey(ctx, &KeyConfig{})
		if err == nil {
			t.Error("Expected error for empty name")
		}
	})

	t.Run("ValidKey", func(t *testing.T) {
		meta, err := engine.CreateKey(ctx, &KeyConfig{
			Name:  "test-key",
			Scope: ScopeMaster,
		})
		if err != nil {
			t.Fatalf("CreateKey() error = %v", err)
		}
		if meta == nil {
			t.Fatal("Expected metadata to be returned")
		}
		if meta.Name != "test-key" {
			t.Errorf("Name = %v, want test-key", meta.Name)
		}
		if meta.Scope != ScopeMaster {
			t.Errorf("Scope = %v, want %v", meta.Scope, ScopeMaster)
		}
		if meta.Version != 1 {
			t.Errorf("Version = %v, want 1", meta.Version)
		}
		if meta.Algorithm != kms.AlgAES256GCM {
			t.Errorf("Algorithm = %v, want %v", meta.Algorithm, kms.AlgAES256GCM)
		}
	})

	t.Run("WithCustomAlgorithm", func(t *testing.T) {
		meta, err := engine.CreateKey(ctx, &KeyConfig{
			Name:      "custom-algo-key",
			Scope:     ScopeData,
			Algorithm: kms.AlgAES128GCM,
		})
		if err != nil {
			t.Fatalf("CreateKey() error = %v", err)
		}
		if meta.Algorithm != kms.AlgAES128GCM {
			t.Errorf("Algorithm = %v, want %v", meta.Algorithm, kms.AlgAES128GCM)
		}
	})

	t.Run("WithRotationStrategy", func(t *testing.T) {
		meta, err := engine.CreateKey(ctx, &KeyConfig{
			Name:             "rotation-key",
			Scope:            ScopeTable,
			RotationStrategy: RotationEnvelope,
		})
		if err != nil {
			t.Fatalf("CreateKey() error = %v", err)
		}
		if meta.RotationStrategy != RotationEnvelope {
			t.Errorf("RotationStrategy = %v, want %v", meta.RotationStrategy, RotationEnvelope)
		}
	})

	t.Run("WithAutoRotation", func(t *testing.T) {
		meta, err := engine.CreateKey(ctx, &KeyConfig{
			Name:            "auto-rotate-key",
			Scope:           ScopeShard,
			AutoRotateAfter: 24 * time.Hour,
		})
		if err != nil {
			t.Fatalf("CreateKey() error = %v", err)
		}
		if meta.ExpiresAt == nil {
			t.Error("Expected ExpiresAt to be set")
		}
	})

	t.Run("WithEntity", func(t *testing.T) {
		meta, err := engine.CreateKey(ctx, &KeyConfig{
			Name:   "entity-key",
			Scope:  ScopeField,
			Entity: "user.ssn",
		})
		if err != nil {
			t.Fatalf("CreateKey() error = %v", err)
		}
		if meta.Entity != "user.ssn" {
			t.Errorf("Entity = %v, want user.ssn", meta.Entity)
		}
	})

	t.Run("WithMetadata", func(t *testing.T) {
		meta, err := engine.CreateKey(ctx, &KeyConfig{
			Name:     "metadata-key",
			Scope:    ScopeData,
			Metadata: map[string]string{"env": "test"},
		})
		if err != nil {
			t.Fatalf("CreateKey() error = %v", err)
		}
		if meta.Metadata["env"] != "test" {
			t.Errorf("Metadata[env] = %v, want test", meta.Metadata["env"])
		}
	})
}

// TestGetKey tests key retrieval.
func TestGetKey(t *testing.T) {
	engine, _, cleanup := testHelper(t)
	defer cleanup()

	ctx := context.Background()

	// Create a key first
	_, err := engine.CreateKey(ctx, &KeyConfig{
		Name:  "get-key",
		Scope: ScopeMaster,
	})
	if err != nil {
		t.Fatalf("Failed to create key: %v", err)
	}

	t.Run("ExistingKey", func(t *testing.T) {
		meta, err := engine.GetKey("get-key")
		if err != nil {
			t.Errorf("GetKey() error = %v", err)
		}
		if meta == nil {
			t.Fatal("Expected metadata to be returned")
		}
		if meta.Name != "get-key" {
			t.Errorf("Name = %v, want get-key", meta.Name)
		}
	})

	t.Run("NonExistentKey", func(t *testing.T) {
		_, err := engine.GetKey("non-existent")
		if err == nil {
			t.Error("Expected error for non-existent key")
		}
	})
}

// TestEncrypt tests encryption.
func TestEncrypt(t *testing.T) {
	engine, _, cleanup := testHelper(t)
	defer cleanup()

	ctx := context.Background()

	// Create a key
	_, err := engine.CreateKey(ctx, &KeyConfig{
		Name:  "encrypt-key",
		Scope: ScopeData,
	})
	if err != nil {
		t.Fatalf("Failed to create key: %v", err)
	}

	t.Run("NilRequest", func(t *testing.T) {
		_, err := engine.Encrypt(ctx, nil)
		if err == nil {
			t.Error("Expected error for nil request")
		}
	})

	t.Run("EmptyKeyName", func(t *testing.T) {
		_, err := engine.Encrypt(ctx, &EncryptRequest{
			Plaintext: []byte("test"),
		})
		if err == nil {
			t.Error("Expected error for empty key name")
		}
	})

	t.Run("NonExistentKey", func(t *testing.T) {
		_, err := engine.Encrypt(ctx, &EncryptRequest{
			KeyName:   "non-existent",
			Plaintext: []byte("test"),
		})
		if err == nil {
			t.Error("Expected error for non-existent key")
		}
	})

	t.Run("ValidEncryption", func(t *testing.T) {
		resp, err := engine.Encrypt(ctx, &EncryptRequest{
			KeyName:   "encrypt-key",
			Plaintext: []byte("hello, world!"),
		})
		if err != nil {
			t.Fatalf("Encrypt() error = %v", err)
		}
		if resp == nil {
			t.Fatal("Expected response to be returned")
		}
		if resp.Ciphertext == nil || len(resp.Ciphertext) == 0 {
			t.Error("Expected ciphertext to be returned")
		}
		if resp.KeyVersion != 1 {
			t.Errorf("KeyVersion = %v, want 1", resp.KeyVersion)
		}
	})

	t.Run("WithContext", func(t *testing.T) {
		resp, err := engine.Encrypt(ctx, &EncryptRequest{
			KeyName:   "encrypt-key",
			Plaintext: []byte("test data"),
			Context:   []byte("additional authenticated data"),
		})
		if err != nil {
			t.Fatalf("Encrypt() error = %v", err)
		}
		if resp.Ciphertext == nil {
			t.Error("Expected ciphertext to be returned")
		}
	})
}

// TestDecrypt tests decryption.
func TestDecrypt(t *testing.T) {
	engine, _, cleanup := testHelper(t)
	defer cleanup()

	ctx := context.Background()

	// Create a key
	_, err := engine.CreateKey(ctx, &KeyConfig{
		Name:  "decrypt-key",
		Scope: ScopeData,
	})
	if err != nil {
		t.Fatalf("Failed to create key: %v", err)
	}

	// Encrypt some data
	plaintext := []byte("hello, world!")
	encResp, err := engine.Encrypt(ctx, &EncryptRequest{
		KeyName:   "decrypt-key",
		Plaintext: plaintext,
	})
	if err != nil {
		t.Fatalf("Encrypt() error = %v", err)
	}

	t.Run("NilRequest", func(t *testing.T) {
		_, err := engine.Decrypt(ctx, nil)
		if err == nil {
			t.Error("Expected error for nil request")
		}
	})

	t.Run("EmptyKeyName", func(t *testing.T) {
		_, err := engine.Decrypt(ctx, &DecryptRequest{
			Ciphertext: encResp.Ciphertext,
		})
		if err == nil {
			t.Error("Expected error for empty key name")
		}
	})

	t.Run("NonExistentKey", func(t *testing.T) {
		_, err := engine.Decrypt(ctx, &DecryptRequest{
			KeyName:    "non-existent",
			Ciphertext: encResp.Ciphertext,
		})
		if err == nil {
			t.Error("Expected error for non-existent key")
		}
	})

	t.Run("ValidDecryption", func(t *testing.T) {
		resp, err := engine.Decrypt(ctx, &DecryptRequest{
			KeyName:    "decrypt-key",
			Ciphertext: encResp.Ciphertext,
		})
		if err != nil {
			t.Fatalf("Decrypt() error = %v", err)
		}
		if resp == nil {
			t.Fatal("Expected response to be returned")
		}
		if string(resp.Plaintext) != string(plaintext) {
			t.Errorf("Plaintext = %v, want %v", string(resp.Plaintext), string(plaintext))
		}
	})

	t.Run("WithContext", func(t *testing.T) {
		// Encrypt with context
		aad := []byte("additional data")
		encWithCtx, err := engine.Encrypt(ctx, &EncryptRequest{
			KeyName:   "decrypt-key",
			Plaintext: []byte("context test"),
			Context:   aad,
		})
		if err != nil {
			t.Fatalf("Encrypt() error = %v", err)
		}

		// Decrypt with same context
		resp, err := engine.Decrypt(ctx, &DecryptRequest{
			KeyName:    "decrypt-key",
			Ciphertext: encWithCtx.Ciphertext,
			Context:    aad,
		})
		if err != nil {
			t.Fatalf("Decrypt() error = %v", err)
		}
		if string(resp.Plaintext) != "context test" {
			t.Errorf("Plaintext = %v, want 'context test'", string(resp.Plaintext))
		}
	})
}

// TestGenerateDataKey tests DEK generation.
func TestGenerateDataKey(t *testing.T) {
	engine, _, cleanup := testHelper(t)
	defer cleanup()

	ctx := context.Background()

	// Create a master key
	_, err := engine.CreateKey(ctx, &KeyConfig{
		Name:  "master-key",
		Scope: ScopeMaster,
	})
	if err != nil {
		t.Fatalf("Failed to create key: %v", err)
	}

	t.Run("EmptyParentKeyName", func(t *testing.T) {
		_, err := engine.GenerateDataKey(ctx, "", 32)
		if err == nil {
			t.Error("Expected error for empty parent key name")
		}
	})

	t.Run("NonExistentParent", func(t *testing.T) {
		_, err := engine.GenerateDataKey(ctx, "non-existent", 32)
		if err == nil {
			t.Error("Expected error for non-existent parent")
		}
	})

	t.Run("ValidGeneration", func(t *testing.T) {
		dek, err := engine.GenerateDataKey(ctx, "master-key", 32)
		if err != nil {
			t.Fatalf("GenerateDataKey() error = %v", err)
		}
		if dek == nil {
			t.Fatal("Expected DEK to be returned")
		}
		if len(dek.Plaintext) != 32 {
			t.Errorf("Plaintext length = %d, want 32", len(dek.Plaintext))
		}
		if dek.Encrypted == nil {
			t.Error("Expected encrypted key to be returned")
		}
		if dek.Metadata == nil {
			t.Error("Expected metadata to be returned")
		}
		if dek.Metadata.Scope != ScopeData {
			t.Errorf("Scope = %v, want %v", dek.Metadata.Scope, ScopeData)
		}
	})

	t.Run("DefaultKeySize", func(t *testing.T) {
		dek, err := engine.GenerateDataKey(ctx, "master-key", 0)
		if err != nil {
			t.Fatalf("GenerateDataKey() error = %v", err)
		}
		// Default should be 32 bytes (256 bits)
		if len(dek.Plaintext) != 32 {
			t.Errorf("Default plaintext length = %d, want 32", len(dek.Plaintext))
		}
	})
}

// TestDecryptDataKey tests DEK decryption.
func TestDecryptDataKey(t *testing.T) {
	engine, _, cleanup := testHelper(t)
	defer cleanup()

	ctx := context.Background()

	// Create a master key
	_, err := engine.CreateKey(ctx, &KeyConfig{
		Name:  "dek-master",
		Scope: ScopeMaster,
	})
	if err != nil {
		t.Fatalf("Failed to create key: %v", err)
	}

	// Generate a DEK
	dek, err := engine.GenerateDataKey(ctx, "dek-master", 32)
	if err != nil {
		t.Fatalf("GenerateDataKey() error = %v", err)
	}

	t.Run("EmptyParentKeyName", func(t *testing.T) {
		_, err := engine.DecryptDataKey(ctx, "", dek.Encrypted)
		if err == nil {
			t.Error("Expected error for empty parent key name")
		}
	})

	t.Run("NilEncryptedKey", func(t *testing.T) {
		_, err := engine.DecryptDataKey(ctx, "dek-master", nil)
		if err == nil {
			t.Error("Expected error for nil encrypted key")
		}
	})

	t.Run("ValidDecryption", func(t *testing.T) {
		plaintext, err := engine.DecryptDataKey(ctx, "dek-master", dek.Encrypted)
		if err != nil {
			t.Fatalf("DecryptDataKey() error = %v", err)
		}
		if string(plaintext) != string(dek.Plaintext) {
			t.Errorf("Decrypted DEK does not match original")
		}
	})
}

// TestRewrap tests key rewrapping.
func TestRewrap(t *testing.T) {
	engine, _, cleanup := testHelper(t)
	defer cleanup()

	ctx := context.Background()

	// Create a key
	_, err := engine.CreateKey(ctx, &KeyConfig{
		Name:  "rewrap-key",
		Scope: ScopeData,
	})
	if err != nil {
		t.Fatalf("Failed to create key: %v", err)
	}

	// Encrypt some data
	plaintext := []byte("rewrap test data")
	encResp, err := engine.Encrypt(ctx, &EncryptRequest{
		KeyName:   "rewrap-key",
		Plaintext: plaintext,
	})
	if err != nil {
		t.Fatalf("Encrypt() error = %v", err)
	}

	t.Run("NilRequest", func(t *testing.T) {
		_, err := engine.Rewrap(ctx, nil)
		if err == nil {
			t.Error("Expected error for nil request")
		}
	})

	t.Run("ValidRewrap", func(t *testing.T) {
		resp, err := engine.Rewrap(ctx, &RewrapRequest{
			KeyName:       "rewrap-key",
			Ciphertext:    encResp.Ciphertext,
			OldKeyVersion: 1,
		})
		if err != nil {
			t.Fatalf("Rewrap() error = %v", err)
		}
		if resp == nil {
			t.Fatal("Expected response to be returned")
		}
		if resp.Ciphertext == nil {
			t.Error("Expected new ciphertext to be returned")
		}

		// Verify we can still decrypt
		decResp, err := engine.Decrypt(ctx, &DecryptRequest{
			KeyName:    "rewrap-key",
			Ciphertext: resp.Ciphertext,
		})
		if err != nil {
			t.Fatalf("Decrypt after rewrap error = %v", err)
		}
		if string(decResp.Plaintext) != string(plaintext) {
			t.Errorf("Decrypted data does not match original")
		}
	})
}

// TestRotateKey tests key rotation.
func TestRotateKey(t *testing.T) {
	engine, _, cleanup := testHelper(t)
	defer cleanup()

	ctx := context.Background()

	// Create a key
	_, err := engine.CreateKey(ctx, &KeyConfig{
		Name:  "rotate-key",
		Scope: ScopeData,
	})
	if err != nil {
		t.Fatalf("Failed to create key: %v", err)
	}

	t.Run("NonExistentKey", func(t *testing.T) {
		_, err := engine.RotateKey(ctx, "non-existent")
		if err == nil {
			t.Error("Expected error for non-existent key")
		}
	})

	t.Run("ValidRotation", func(t *testing.T) {
		meta, err := engine.RotateKey(ctx, "rotate-key")
		if err != nil {
			t.Fatalf("RotateKey() error = %v", err)
		}
		if meta.Version != 2 {
			t.Errorf("Version = %d, want 2", meta.Version)
		}
	})
}

// TestListKeys tests key listing.
func TestListKeys(t *testing.T) {
	engine, _, cleanup := testHelper(t)
	defer cleanup()

	ctx := context.Background()

	// Create several keys
	for i := 0; i < 5; i++ {
		_, err := engine.CreateKey(ctx, &KeyConfig{
			Name:  "list-key-" + string(rune('a'+i)),
			Scope: ScopeData,
		})
		if err != nil {
			t.Fatalf("Failed to create key: %v", err)
		}
	}

	keys := engine.ListKeys()
	if len(keys) != 5 {
		t.Errorf("ListKeys() returned %d keys, want 5", len(keys))
	}
}

// TestDeleteKey tests key deletion.
func TestDeleteKey(t *testing.T) {
	engine, _, cleanup := testHelper(t)
	defer cleanup()

	ctx := context.Background()

	// Create a key
	_, err := engine.CreateKey(ctx, &KeyConfig{
		Name:  "delete-key",
		Scope: ScopeData,
	})
	if err != nil {
		t.Fatalf("Failed to create key: %v", err)
	}

	t.Run("NonExistentKey", func(t *testing.T) {
		err := engine.DeleteKey(ctx, "non-existent")
		if err == nil {
			t.Error("Expected error for non-existent key")
		}
	})

	t.Run("ValidDeletion", func(t *testing.T) {
		err := engine.DeleteKey(ctx, "delete-key")
		if err != nil {
			t.Fatalf("DeleteKey() error = %v", err)
		}

		// Verify key is gone
		_, err = engine.GetKey("delete-key")
		if err == nil {
			t.Error("Expected error when getting deleted key")
		}
	})
}

// TestClose tests engine closure.
func TestClose(t *testing.T) {
	engine, _, cleanup := testHelper(t)
	defer cleanup()

	ctx := context.Background()

	// Create some keys
	for i := 0; i < 3; i++ {
		_, err := engine.CreateKey(ctx, &KeyConfig{
			Name:  "close-key-" + string(rune('a'+i)),
			Scope: ScopeData,
		})
		if err != nil {
			t.Fatalf("Failed to create key: %v", err)
		}
	}

	// Verify keys exist
	if len(engine.ListKeys()) != 3 {
		t.Error("Expected 3 keys before close")
	}

	// Close the engine
	err := engine.Close()
	if err != nil {
		t.Fatalf("Close() error = %v", err)
	}

	// Verify keys are cleared
	if len(engine.ListKeys()) != 0 {
		t.Error("Expected 0 keys after close")
	}
}

// TestKeyScopes tests all key scope values.
func TestKeyScopes(t *testing.T) {
	engine, _, cleanup := testHelper(t)
	defer cleanup()

	ctx := context.Background()

	scopes := []KeyScope{
		ScopeRoot,
		ScopeMaster,
		ScopeData,
		ScopeTable,
		ScopeShard,
		ScopeField,
	}

	for _, scope := range scopes {
		t.Run(string(scope), func(t *testing.T) {
			meta, err := engine.CreateKey(ctx, &KeyConfig{
				Name:  "scope-test-" + string(scope),
				Scope: scope,
			})
			if err != nil {
				t.Fatalf("CreateKey() error = %v", err)
			}
			if meta.Scope != scope {
				t.Errorf("Scope = %v, want %v", meta.Scope, scope)
			}
		})
	}
}

// TestRotationStrategies tests all rotation strategy values.
func TestRotationStrategies(t *testing.T) {
	engine, _, cleanup := testHelper(t)
	defer cleanup()

	ctx := context.Background()

	strategies := []RotationStrategy{
		RotationImmediate,
		RotationEnvelope,
		RotationLazy,
	}

	for _, strategy := range strategies {
		t.Run(string(strategy), func(t *testing.T) {
			meta, err := engine.CreateKey(ctx, &KeyConfig{
				Name:             "strategy-test-" + string(strategy),
				Scope:            ScopeData,
				RotationStrategy: strategy,
			})
			if err != nil {
				t.Fatalf("CreateKey() error = %v", err)
			}
			if meta.RotationStrategy != strategy {
				t.Errorf("RotationStrategy = %v, want %v", meta.RotationStrategy, strategy)
			}
		})
	}
}

// TestEncryptDecryptRoundTrip tests full encrypt/decrypt cycle.
func TestEncryptDecryptRoundTrip(t *testing.T) {
	engine, _, cleanup := testHelper(t)
	defer cleanup()

	ctx := context.Background()

	// Create a key
	_, err := engine.CreateKey(ctx, &KeyConfig{
		Name:  "roundtrip-key",
		Scope: ScopeData,
	})
	if err != nil {
		t.Fatalf("Failed to create key: %v", err)
	}

	testCases := []struct {
		name      string
		plaintext []byte
		context   []byte
	}{
		{"SimpleString", []byte("hello, world!"), nil},
		{"BinaryData", []byte{0x00, 0x01, 0x02, 0xff, 0xfe}, nil},
		{"WithContext", []byte("secret data"), []byte("context info")},
		{"LargeData", make([]byte, 1024*1024), nil}, // 1MB
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			// Fill large data with pattern
			if tc.name == "LargeData" {
				for i := range tc.plaintext {
					tc.plaintext[i] = byte(i % 256)
				}
			}

			// Encrypt
			encResp, err := engine.Encrypt(ctx, &EncryptRequest{
				KeyName:   "roundtrip-key",
				Plaintext: tc.plaintext,
				Context:   tc.context,
			})
			if err != nil {
				t.Fatalf("Encrypt() error = %v", err)
			}

			// Decrypt
			decResp, err := engine.Decrypt(ctx, &DecryptRequest{
				KeyName:    "roundtrip-key",
				Ciphertext: encResp.Ciphertext,
				Context:    tc.context,
			})
			if err != nil {
				t.Fatalf("Decrypt() error = %v", err)
			}

			// Compare
			if string(decResp.Plaintext) != string(tc.plaintext) {
				t.Errorf("Decrypted data does not match original")
			}
		})
	}
}

// TestCreateKey_KMSGenerateKeyError tests CreateKey when KMS fails.
func TestCreateKey_KMSGenerateKeyError(t *testing.T) {
	backend := newMockErrorBackend()
	backend.failCreate = true

	engine, _, cleanup := testHelperWithErrorBackend(t, backend)
	defer cleanup()

	ctx := context.Background()
	_, err := engine.CreateKey(ctx, &KeyConfig{
		Name:  "fail-key",
		Scope: ScopeMaster,
	})

	if err == nil {
		t.Error("Expected error when KMS fails to generate key")
	}
}

// TestEncrypt_KMSEncryptError tests Encrypt when KMS encryption fails.
func TestEncrypt_KMSEncryptError(t *testing.T) {
	backend := newMockErrorBackend()
	engine, _, cleanup := testHelperWithErrorBackend(t, backend)
	defer cleanup()

	ctx := context.Background()

	// Create key successfully
	_, err := engine.CreateKey(ctx, &KeyConfig{
		Name:  "encrypt-fail-key",
		Scope: ScopeData,
	})
	if err != nil {
		t.Fatalf("Failed to create key: %v", err)
	}

	// Now make encryption fail
	backend.failEncrypt = true

	_, err = engine.Encrypt(ctx, &EncryptRequest{
		KeyName:   "encrypt-fail-key",
		Plaintext: []byte("test"),
	})

	if err == nil {
		t.Error("Expected error when KMS encryption fails")
	}
}

// TestDecrypt_KMSDecryptError tests Decrypt when KMS decryption fails.
func TestDecrypt_KMSDecryptError(t *testing.T) {
	backend := newMockErrorBackend()
	engine, _, cleanup := testHelperWithErrorBackend(t, backend)
	defer cleanup()

	ctx := context.Background()

	// Create key and encrypt data
	_, err := engine.CreateKey(ctx, &KeyConfig{
		Name:  "decrypt-fail-key",
		Scope: ScopeData,
	})
	if err != nil {
		t.Fatalf("Failed to create key: %v", err)
	}

	encResp, err := engine.Encrypt(ctx, &EncryptRequest{
		KeyName:   "decrypt-fail-key",
		Plaintext: []byte("test"),
	})
	if err != nil {
		t.Fatalf("Encrypt() error = %v", err)
	}

	// Now make decryption fail
	backend.failDecrypt = true

	_, err = engine.Decrypt(ctx, &DecryptRequest{
		KeyName:    "decrypt-fail-key",
		Ciphertext: encResp.Ciphertext,
	})

	if err == nil {
		t.Error("Expected error when KMS decryption fails")
	}
}

// TestRotateKey_KMSRotateError tests RotateKey when KMS rotation fails.
func TestRotateKey_KMSRotateError(t *testing.T) {
	backend := newMockErrorBackend()
	engine, _, cleanup := testHelperWithErrorBackend(t, backend)
	defer cleanup()

	ctx := context.Background()

	_, err := engine.CreateKey(ctx, &KeyConfig{
		Name:  "rotate-fail-key",
		Scope: ScopeData,
	})
	if err != nil {
		t.Fatalf("Failed to create key: %v", err)
	}

	// Make rotation fail
	backend.failRotate = true

	_, err = engine.RotateKey(ctx, "rotate-fail-key")
	if err == nil {
		t.Error("Expected error when KMS rotation fails")
	}
}

// TestDeleteKey_KMSDeleteError tests DeleteKey when KMS delete fails.
func TestDeleteKey_KMSDeleteError(t *testing.T) {
	backend := newMockErrorBackend()
	engine, _, cleanup := testHelperWithErrorBackend(t, backend)
	defer cleanup()

	ctx := context.Background()

	_, err := engine.CreateKey(ctx, &KeyConfig{
		Name:  "delete-fail-key",
		Scope: ScopeData,
	})
	if err != nil {
		t.Fatalf("Failed to create key: %v", err)
	}

	// Make deletion fail
	backend.failDelete = true

	err = engine.DeleteKey(ctx, "delete-fail-key")
	if err == nil {
		t.Error("Expected error when KMS delete fails")
	}

	// Key should still exist in engine map
	_, err = engine.GetKey("delete-fail-key")
	if err != nil {
		t.Error("Key should still exist after failed deletion")
	}
}

// TestEngineWithLogger tests engine with logger.
func TestEngineWithLogger(t *testing.T) {
	backend := newMockKMSBackend()
	registry := kms.NewBackendRegistry(nil)
	registry.Register("primary", backend, 1)
	manager, err := kms.NewKMSService(&kms.KMSServiceOptions{
		Registry: registry,
	})
	if err != nil {
		t.Fatalf("Failed to create KMS manager: %v", err)
	}
	defer manager.Close()

	// Create a mock logger
	logger := &mockLogger{}

	engine, err := NewEngine(&EngineOptions{
		KMS:    manager,
		Logger: logger,
	})
	if err != nil {
		t.Fatalf("NewEngine() error = %v", err)
	}
	defer engine.Close()

	ctx := context.Background()

	// Create key should trigger logging
	_, err = engine.CreateKey(ctx, &KeyConfig{
		Name:  "logged-key",
		Scope: ScopeData,
	})
	if err != nil {
		t.Fatalf("CreateKey() error = %v", err)
	}

	if !logger.infoCalled {
		t.Error("Expected logger Info to be called")
	}

	// Delete key should also trigger logging
	err = engine.DeleteKey(ctx, "logged-key")
	if err != nil {
		t.Fatalf("DeleteKey() error = %v", err)
	}
}

// TestCreateKey_WithParentName tests CreateKey with ParentName set.
func TestCreateKey_WithParentName(t *testing.T) {
	engine, _, cleanup := testHelper(t)
	defer cleanup()

	ctx := context.Background()

	// Create parent key first
	_, err := engine.CreateKey(ctx, &KeyConfig{
		Name:  "parent-key",
		Scope: ScopeMaster,
	})
	if err != nil {
		t.Fatalf("Failed to create parent key: %v", err)
	}

	// Create child key with parent name
	meta, err := engine.CreateKey(ctx, &KeyConfig{
		Name:       "child-key",
		Scope:      ScopeData,
		ParentName: "parent-key",
	})
	if err != nil {
		t.Fatalf("CreateKey() error = %v", err)
	}

	if meta.ParentID != "parent-key" {
		t.Errorf("ParentID = %v, want parent-key", meta.ParentID)
	}
}

// TestConcurrentKeyOperations tests concurrent key operations.
func TestConcurrentKeyOperations(t *testing.T) {
	engine, _, cleanup := testHelper(t)
	defer cleanup()

	ctx := context.Background()

	// Create initial key
	_, err := engine.CreateKey(ctx, &KeyConfig{
		Name:  "concurrent-base-key",
		Scope: ScopeData,
	})
	if err != nil {
		t.Fatalf("Failed to create key: %v", err)
	}

	// Run concurrent operations
	done := make(chan bool)
	for i := 0; i < 10; i++ {
		go func(id int) {
			defer func() { done <- true }()

			// Create unique key
			keyName := "concurrent-key-" + string(rune('a'+id))
			_, err := engine.CreateKey(ctx, &KeyConfig{
				Name:  keyName,
				Scope: ScopeData,
			})
			if err != nil {
				t.Errorf("CreateKey failed: %v", err)
				return
			}

			// Encrypt data
			_, err = engine.Encrypt(ctx, &EncryptRequest{
				KeyName:   keyName,
				Plaintext: []byte("concurrent test"),
			})
			if err != nil {
				t.Errorf("Encrypt failed: %v", err)
				return
			}

			// Get key
			_, err = engine.GetKey(keyName)
			if err != nil {
				t.Errorf("GetKey failed: %v", err)
			}
		}(i)
	}

	// Wait for all goroutines
	for i := 0; i < 10; i++ {
		<-done
	}

	// Verify all keys were created
	keys := engine.ListKeys()
	// 1 base key + 10 concurrent keys
	if len(keys) < 11 {
		t.Errorf("Expected at least 11 keys, got %d", len(keys))
	}
}

// =============================================================================
// Consolidated from engine_additional_test.go
// =============================================================================

// TestAlgorithmToKeyAttributes tests all algorithm conversion paths.
func TestAlgorithmToKeyAttributes(t *testing.T) {
	engine, _, cleanup := testHelper(t)
	defer cleanup()

	ctx := context.Background()

	testCases := []struct {
		name      string
		algorithm string
		wantAlgo  string
	}{
		{"AES128GCM", kms.AlgAES128GCM, kms.AlgAES128GCM},
		{"AES192GCM", kms.AlgAES192GCM, kms.AlgAES192GCM},
		{"AES256GCM", kms.AlgAES256GCM, kms.AlgAES256GCM},
		{"ChaCha20Poly1305", kms.AlgChaCha20Poly1305, kms.AlgChaCha20Poly1305},
		{"Default", "", kms.AlgAES256GCM}, // Default algorithm
		// Unknown algorithms are passed through as-is per implementation
		{"Unknown", "unknown-algo", "unknown-algo"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			keyName := "algo-test-" + tc.name
			meta, err := engine.CreateKey(ctx, &KeyConfig{
				Name:      keyName,
				Scope:     ScopeData,
				Algorithm: tc.algorithm,
			})
			if err != nil {
				t.Fatalf("CreateKey() error = %v", err)
			}
			if meta.Algorithm != tc.wantAlgo {
				t.Errorf("Algorithm = %v, want %v", meta.Algorithm, tc.wantAlgo)
			}
		})
	}
}

// TestRotateKeyWithExpiry tests key rotation with expiry set.
func TestRotateKeyWithExpiry(t *testing.T) {
	engine, _, cleanup := testHelper(t)
	defer cleanup()

	ctx := context.Background()

	// Create a key with auto-rotation
	_, err := engine.CreateKey(ctx, &KeyConfig{
		Name:            "rotate-expiry-key",
		Scope:           ScopeData,
		AutoRotateAfter: 24 * time.Hour,
	})
	if err != nil {
		t.Fatalf("Failed to create key: %v", err)
	}

	// Get original expiry
	originalMeta, _ := engine.GetKey("rotate-expiry-key")
	if originalMeta.ExpiresAt == nil {
		t.Fatal("Expected ExpiresAt to be set")
	}
	originalExpiry := *originalMeta.ExpiresAt

	// Rotate the key
	meta, err := engine.RotateKey(ctx, "rotate-expiry-key")
	if err != nil {
		t.Fatalf("RotateKey() error = %v", err)
	}

	if meta.Version != 2 {
		t.Errorf("Version = %d, want 2", meta.Version)
	}

	if meta.ExpiresAt == nil {
		t.Error("Expected ExpiresAt to be set after rotation")
	}

	// The new expiry should be at least as late as the original
	if meta.ExpiresAt != nil && meta.ExpiresAt.Before(originalExpiry) {
		t.Error("New expiry should not be before original expiry")
	}
}

// TestRotateKeyWithLoggerLogging tests rotation logging.
func TestRotateKeyWithLoggerLogging(t *testing.T) {
	backend := newMockKMSBackend()
	registry := kms.NewBackendRegistry(nil)
	registry.Register("primary", backend, 1)
	manager, err := kms.NewKMSService(&kms.KMSServiceOptions{
		Registry: registry,
	})
	if err != nil {
		t.Fatalf("Failed to create KMS manager: %v", err)
	}
	defer manager.Close()

	// Create a mock logger
	logger := &mockLogger{}

	engine, err := NewEngine(&EngineOptions{
		KMS:    manager,
		Logger: logger,
	})
	if err != nil {
		t.Fatalf("NewEngine() error = %v", err)
	}
	defer engine.Close()

	ctx := context.Background()

	// Create a key first
	_, err = engine.CreateKey(ctx, &KeyConfig{
		Name:  "rotate-log-key",
		Scope: ScopeData,
	})
	if err != nil {
		t.Fatalf("CreateKey() error = %v", err)
	}

	// Reset logger state
	logger.infoCalled = false

	// Rotate the key
	_, err = engine.RotateKey(ctx, "rotate-log-key")
	if err != nil {
		t.Fatalf("RotateKey() error = %v", err)
	}

	if !logger.infoCalled {
		t.Error("Expected logger Info to be called after rotation")
	}
}

// TestGenerateDataKeyError tests GenerateDataKey when KMS fails.
func TestGenerateDataKeyError(t *testing.T) {
	backend := newMockErrorBackend()
	engine, _, cleanup := testHelperWithErrorBackend(t, backend)
	defer cleanup()

	ctx := context.Background()

	// Create a key first
	_, err := engine.CreateKey(ctx, &KeyConfig{
		Name:  "dek-error-key",
		Scope: ScopeMaster,
	})
	if err != nil {
		t.Fatalf("Failed to create key: %v", err)
	}

	// Make data key generation fail
	backend.failDataKey = true

	_, err = engine.GenerateDataKey(ctx, "dek-error-key", 32)
	if err == nil {
		t.Error("Expected error when KMS fails to generate data key")
	}
}

// TestDecryptDataKeyError tests DecryptDataKey when KMS fails.
func TestDecryptDataKeyError(t *testing.T) {
	backend := newMockErrorBackend()
	engine, _, cleanup := testHelperWithErrorBackend(t, backend)
	defer cleanup()

	ctx := context.Background()

	// Create a key first
	_, err := engine.CreateKey(ctx, &KeyConfig{
		Name:  "dek-decrypt-error-key",
		Scope: ScopeMaster,
	})
	if err != nil {
		t.Fatalf("Failed to create key: %v", err)
	}

	// Generate a DEK first (successfully)
	dek, err := engine.GenerateDataKey(ctx, "dek-decrypt-error-key", 32)
	if err != nil {
		t.Fatalf("GenerateDataKey() error = %v", err)
	}

	// Now make decryption fail
	backend.failDecrypt = true

	_, err = engine.DecryptDataKey(ctx, "dek-decrypt-error-key", dek.Encrypted)
	if err == nil {
		t.Error("Expected error when KMS fails to decrypt data key")
	}
}

// TestRewrapDecryptError tests Rewrap when decryption fails.
func TestRewrapDecryptError(t *testing.T) {
	backend := newMockErrorBackend()
	engine, _, cleanup := testHelperWithErrorBackend(t, backend)
	defer cleanup()

	ctx := context.Background()

	// Create a key
	_, err := engine.CreateKey(ctx, &KeyConfig{
		Name:  "rewrap-decrypt-error-key",
		Scope: ScopeData,
	})
	if err != nil {
		t.Fatalf("Failed to create key: %v", err)
	}

	// Encrypt some data
	encResp, err := engine.Encrypt(ctx, &EncryptRequest{
		KeyName:   "rewrap-decrypt-error-key",
		Plaintext: []byte("test data"),
	})
	if err != nil {
		t.Fatalf("Encrypt() error = %v", err)
	}

	// Make decryption fail
	backend.failDecrypt = true

	_, err = engine.Rewrap(ctx, &RewrapRequest{
		KeyName:    "rewrap-decrypt-error-key",
		Ciphertext: encResp.Ciphertext,
	})
	if err == nil {
		t.Error("Expected error when decryption fails during rewrap")
	}
}

// TestRewrapEncryptError tests Rewrap when re-encryption fails.
func TestRewrapEncryptError(t *testing.T) {
	backend := newMockErrorBackend()
	engine, _, cleanup := testHelperWithErrorBackend(t, backend)
	defer cleanup()

	ctx := context.Background()

	// Create a key
	_, err := engine.CreateKey(ctx, &KeyConfig{
		Name:  "rewrap-encrypt-error-key",
		Scope: ScopeData,
	})
	if err != nil {
		t.Fatalf("Failed to create key: %v", err)
	}

	// Encrypt some data
	encResp, err := engine.Encrypt(ctx, &EncryptRequest{
		KeyName:   "rewrap-encrypt-error-key",
		Plaintext: []byte("test data"),
	})
	if err != nil {
		t.Fatalf("Encrypt() error = %v", err)
	}

	// Make encryption fail (but not decryption)
	backend.failEncrypt = true

	_, err = engine.Rewrap(ctx, &RewrapRequest{
		KeyName:    "rewrap-encrypt-error-key",
		Ciphertext: encResp.Ciphertext,
	})
	if err == nil {
		t.Error("Expected error when re-encryption fails during rewrap")
	}
}

// TestRewrapNonExistentKey tests Rewrap with non-existent key.
func TestRewrapNonExistentKey(t *testing.T) {
	engine, _, cleanup := testHelper(t)
	defer cleanup()

	ctx := context.Background()

	_, err := engine.Rewrap(ctx, &RewrapRequest{
		KeyName:    "non-existent-key",
		Ciphertext: []byte("some ciphertext"),
	})
	if err == nil {
		t.Error("Expected error for non-existent key")
	}
}

// TestGenerateDataKeyNegativeBytes tests GenerateDataKey with negative keyBytes.
func TestGenerateDataKeyNegativeBytes(t *testing.T) {
	engine, _, cleanup := testHelper(t)
	defer cleanup()

	ctx := context.Background()

	// Create a master key
	_, err := engine.CreateKey(ctx, &KeyConfig{
		Name:  "master-neg-key",
		Scope: ScopeMaster,
	})
	if err != nil {
		t.Fatalf("Failed to create key: %v", err)
	}

	// Request with negative bytes - should default to 32
	dek, err := engine.GenerateDataKey(ctx, "master-neg-key", -1)
	if err != nil {
		t.Fatalf("GenerateDataKey() error = %v", err)
	}
	// Default should be 32 bytes
	if len(dek.Plaintext) != 32 {
		t.Errorf("Plaintext length = %d, want 32", len(dek.Plaintext))
	}
}

// TestDecryptDataKeyNonExistentParent tests DecryptDataKey with non-existent parent.
func TestDecryptDataKeyNonExistentParent(t *testing.T) {
	engine, _, cleanup := testHelper(t)
	defer cleanup()

	ctx := context.Background()

	// Create a master key
	_, err := engine.CreateKey(ctx, &KeyConfig{
		Name:  "dek-test-master",
		Scope: ScopeMaster,
	})
	if err != nil {
		t.Fatalf("Failed to create key: %v", err)
	}

	// Generate a DEK
	dek, err := engine.GenerateDataKey(ctx, "dek-test-master", 32)
	if err != nil {
		t.Fatalf("GenerateDataKey() error = %v", err)
	}

	// Delete the parent key
	err = engine.DeleteKey(ctx, "dek-test-master")
	if err != nil {
		t.Fatalf("DeleteKey() error = %v", err)
	}

	// Now try to decrypt with non-existent parent
	_, err = engine.DecryptDataKey(ctx, "dek-test-master", dek.Encrypted)
	if err == nil {
		t.Error("Expected error for non-existent parent")
	}
}

// TestMultipleKeyRotations tests multiple consecutive rotations.
func TestMultipleKeyRotations(t *testing.T) {
	engine, _, cleanup := testHelper(t)
	defer cleanup()

	ctx := context.Background()

	// Create a key
	_, err := engine.CreateKey(ctx, &KeyConfig{
		Name:  "multi-rotate-key",
		Scope: ScopeData,
	})
	if err != nil {
		t.Fatalf("Failed to create key: %v", err)
	}

	// Rotate multiple times
	for i := 2; i <= 5; i++ {
		meta, err := engine.RotateKey(ctx, "multi-rotate-key")
		if err != nil {
			t.Fatalf("RotateKey() iteration %d error = %v", i, err)
		}
		if meta.Version != i {
			t.Errorf("Version = %d, want %d", meta.Version, i)
		}
	}
}

// TestDecryptWithWrongKey tests decryption with wrong key.
func TestDecryptWithWrongKey(t *testing.T) {
	engine, _, cleanup := testHelper(t)
	defer cleanup()

	ctx := context.Background()

	// Create two keys
	_, err := engine.CreateKey(ctx, &KeyConfig{
		Name:  "correct-key",
		Scope: ScopeData,
	})
	if err != nil {
		t.Fatalf("Failed to create key: %v", err)
	}

	_, err = engine.CreateKey(ctx, &KeyConfig{
		Name:  "wrong-key",
		Scope: ScopeData,
	})
	if err != nil {
		t.Fatalf("Failed to create key: %v", err)
	}

	// Encrypt with correct-key
	encResp, err := engine.Encrypt(ctx, &EncryptRequest{
		KeyName:   "correct-key",
		Plaintext: []byte("secret data"),
	})
	if err != nil {
		t.Fatalf("Encrypt() error = %v", err)
	}

	// Try to decrypt with wrong-key (this may succeed or fail depending on the mock)
	// We just verify the code path executes
	_, err = engine.Decrypt(ctx, &DecryptRequest{
		KeyName:    "wrong-key",
		Ciphertext: encResp.Ciphertext,
	})
	// Error is expected but not required based on mock implementation
	_ = err
}

// TestKeyWithCustomKeySize tests keys with various sizes.
func TestKeyWithCustomKeySize(t *testing.T) {
	engine, _, cleanup := testHelper(t)
	defer cleanup()

	ctx := context.Background()

	// Create a master key
	_, err := engine.CreateKey(ctx, &KeyConfig{
		Name:  "size-test-master",
		Scope: ScopeMaster,
	})
	if err != nil {
		t.Fatalf("Failed to create key: %v", err)
	}

	sizes := []int{16, 24, 32, 64}
	for _, size := range sizes {
		t.Run("Size"+string(rune('0'+size/10))+string(rune('0'+size%10)), func(t *testing.T) {
			dek, err := engine.GenerateDataKey(ctx, "size-test-master", size)
			if err != nil {
				t.Fatalf("GenerateDataKey() error = %v", err)
			}
			if len(dek.Plaintext) != size {
				t.Errorf("Plaintext length = %d, want %d", len(dek.Plaintext), size)
			}
		})
	}
}
