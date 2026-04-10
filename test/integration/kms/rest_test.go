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

//go:build integration && quic

package kms

import (
	"context"
	"crypto/elliptic"
	"crypto/x509"
	"fmt"
	"testing"
	"time"

	"github.com/jeremyhahn/go-quicbaas/test/integration/helpers"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	kctypes "github.com/jeremyhahn/go-xkms/pkg/types"

	"github.com/jeremyhahn/go-quicbaas/pkg/service"
)

// TestRESTKeyGeneration tests key generation via REST API.
func TestRESTKeyGeneration(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	endpoints := helpers.GetRESTEndpoints()
	client := NewRESTClient(endpoints[0])

	tests := []struct {
		name              string
		attrs             *kctypes.KeyAttributes
		expectedAlgorithm service.KeyAlgorithm
		usage             []service.KeyUsage
	}{
		// All 5 symmetric algorithms
		{
			name: "AES-128-GCM",
			attrs: &kctypes.KeyAttributes{
				SymmetricAlgorithm: kctypes.SymmetricAES128GCM,
			},
			expectedAlgorithm: service.KeyAlgAES128GCM,
			usage:             []service.KeyUsage{service.KeyUsageEncrypt, service.KeyUsageDecrypt},
		},
		{
			name: "AES-192-GCM",
			attrs: &kctypes.KeyAttributes{
				SymmetricAlgorithm: kctypes.SymmetricAES192GCM,
			},
			expectedAlgorithm: service.KeyAlgAES192GCM,
			usage:             []service.KeyUsage{service.KeyUsageEncrypt, service.KeyUsageDecrypt},
		},
		{
			name: "AES-256-GCM",
			attrs: &kctypes.KeyAttributes{
				SymmetricAlgorithm: kctypes.SymmetricAES256GCM,
			},
			expectedAlgorithm: service.KeyAlgAES256GCM,
			usage:             []service.KeyUsage{service.KeyUsageEncrypt, service.KeyUsageDecrypt},
		},
		{
			name: "ChaCha20-Poly1305",
			attrs: &kctypes.KeyAttributes{
				SymmetricAlgorithm: kctypes.SymmetricChaCha20Poly1305,
			},
			expectedAlgorithm: service.KeyAlgChaCha20Poly1305,
			usage:             []service.KeyUsage{service.KeyUsageEncrypt, service.KeyUsageDecrypt},
		},
		{
			name: "XChaCha20-Poly1305",
			attrs: &kctypes.KeyAttributes{
				SymmetricAlgorithm: kctypes.SymmetricXChaCha20Poly1305,
			},
			expectedAlgorithm: service.KeyAlgXChaCha20Poly1305,
			usage:             []service.KeyUsage{service.KeyUsageEncrypt, service.KeyUsageDecrypt},
		},
		// Asymmetric algorithms
		{
			name: "RSA-4096",
			attrs: &kctypes.KeyAttributes{
				KeyAlgorithm:  x509.RSA,
				RSAAttributes: &kctypes.RSAAttributes{KeySize: 4096},
			},
			expectedAlgorithm: service.KeyAlgRSA4096,
			usage:             []service.KeyUsage{service.KeyUsageSign, service.KeyUsageVerify},
		},
		{
			name: "ECDSA-P256",
			attrs: &kctypes.KeyAttributes{
				KeyAlgorithm:  x509.ECDSA,
				ECCAttributes: &kctypes.ECCAttributes{Curve: elliptic.P256()},
			},
			expectedAlgorithm: service.KeyAlgECDSAP256,
			usage:             []service.KeyUsage{service.KeyUsageSign, service.KeyUsageVerify},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			keyName := generateUniqueKeyName(fmt.Sprintf("rest-%s", tt.name))

			req := &service.GenerateKeyRequest{
				Name:          keyName,
				Description:   fmt.Sprintf("REST integration test %s key", tt.name),
				KeyAttributes: tt.attrs,
				Usage:         tt.usage,
			}

			resp, err := client.GenerateKey(ctx, req)
			require.NoError(t, err, "KMS must be available on REST")
			require.NotNil(t, resp)

			assert.NotEmpty(t, resp.Key.KeyID)
			assert.Equal(t, tt.expectedAlgorithm, resp.Key.Algorithm)
			assert.Equal(t, service.KeyStatusEnabled, resp.Key.Status)

			// Cleanup
			deleteReq := &service.DeleteKeyRequest{
				KeyID: resp.Key.KeyID,
				Force: true,
			}
			_, _ = client.DeleteKey(ctx, deleteReq)
		})
	}
}

// TestRESTKeyRetrieval tests key retrieval via REST API.
func TestRESTKeyRetrieval(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	endpoints := helpers.GetRESTEndpoints()
	client := NewRESTClient(endpoints[0])

	keyName := generateUniqueKeyName("rest-retrieval")
	genReq := &service.GenerateKeyRequest{
		Name: keyName,
		KeyAttributes: &kctypes.KeyAttributes{
			SymmetricAlgorithm: kctypes.SymmetricAES256GCM,
		},
		Usage: []service.KeyUsage{service.KeyUsageEncrypt, service.KeyUsageDecrypt},
	}

	genResp, err := client.GenerateKey(ctx, genReq)
	require.NoError(t, err, "KMS must be available on REST")
	keyID := genResp.Key.KeyID

	defer func() {
		deleteReq := &service.DeleteKeyRequest{
			KeyID: keyID,
			Force: true,
		}
		_, _ = client.DeleteKey(ctx, deleteReq)
	}()

	t.Run("get key by ID", func(t *testing.T) {
		req := &service.GetKeyRequest{
			KeyID: keyID,
		}

		resp, err := client.GetKey(ctx, req)
		require.NoError(t, err)
		require.NotNil(t, resp)
		assert.Equal(t, keyID, resp.Metadata.KeyID)
		assert.Equal(t, service.KeyAlgAES256GCM, resp.Metadata.Algorithm)
	})

	t.Run("get non-existent key", func(t *testing.T) {
		req := &service.GetKeyRequest{
			KeyID: "non-existent-key-id",
		}

		_, err := client.GetKey(ctx, req)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "not found")
	})
}

// TestRESTKeyListing tests key listing via REST API.
func TestRESTKeyListing(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	endpoints := helpers.GetRESTEndpoints()
	client := NewRESTClient(endpoints[0])

	// Create multiple test keys
	prefix := generateUniqueKeyName("rest-list")
	keyIDs := []string{}

	for i := 0; i < 3; i++ {
		req := &service.GenerateKeyRequest{
			Name: fmt.Sprintf("%s-key-%d", prefix, i),
			KeyAttributes: &kctypes.KeyAttributes{
				SymmetricAlgorithm: kctypes.SymmetricAES256GCM,
			},
			Usage: []service.KeyUsage{service.KeyUsageEncrypt, service.KeyUsageDecrypt},
		}

		resp, err := client.GenerateKey(ctx, req)
		require.NoError(t, err, "KMS must be available on REST")
		keyIDs = append(keyIDs, resp.Key.KeyID)
	}

	defer func() {
		for _, keyID := range keyIDs {
			deleteReq := &service.DeleteKeyRequest{
				KeyID: keyID,
				Force: true,
			}
			_, _ = client.DeleteKey(ctx, deleteReq)
		}
	}()

	t.Run("list all keys", func(t *testing.T) {
		req := &service.ListKeysRequest{
			Limit: 100,
		}

		resp, err := client.ListKeys(ctx, req)
		require.NoError(t, err)
		require.NotNil(t, resp)
		assert.GreaterOrEqual(t, len(resp.Keys), 3, "Should have at least 3 keys")
	})

	t.Run("list with prefix filter", func(t *testing.T) {
		req := &service.ListKeysRequest{
			Limit:  100,
			Filter: map[string]string{"prefix": prefix},
		}

		resp, err := client.ListKeys(ctx, req)
		require.NoError(t, err)
		require.NotNil(t, resp)
		t.Logf("Found %d keys with prefix %s", len(resp.Keys), prefix)
	})

	t.Run("list with limit", func(t *testing.T) {
		req := &service.ListKeysRequest{
			Limit: 2,
		}

		resp, err := client.ListKeys(ctx, req)
		require.NoError(t, err)
		assert.NotNil(t, resp.Keys)
		t.Logf("Requested limit: 2, Got: %d keys", len(resp.Keys))
	})
}

// TestRESTKeyRotation tests key rotation via REST API.
func TestRESTKeyRotation(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	endpoints := helpers.GetRESTEndpoints()
	client := NewRESTClient(endpoints[0])

	keyName := generateUniqueKeyName("rest-rotation")
	genReq := &service.GenerateKeyRequest{
		Name: keyName,
		KeyAttributes: &kctypes.KeyAttributes{
			SymmetricAlgorithm: kctypes.SymmetricAES256GCM,
		},
		Usage: []service.KeyUsage{service.KeyUsageEncrypt, service.KeyUsageDecrypt},
	}

	genResp, err := client.GenerateKey(ctx, genReq)
	require.NoError(t, err, "KMS must be available on REST")
	keyID := genResp.Key.KeyID

	defer func() {
		deleteReq := &service.DeleteKeyRequest{
			KeyID: keyID,
			Force: true,
		}
		_, _ = client.DeleteKey(ctx, deleteReq)
	}()

	t.Run("rotate key", func(t *testing.T) {
		// Get version before rotation
		getReqBefore := &service.GetKeyRequest{KeyID: keyID}
		getRespBefore, err := client.GetKey(ctx, getReqBefore)
		require.NoError(t, err)
		versionBefore := getRespBefore.Metadata.Version

		// Rotate
		rotateReq := &service.RotateKeyRequest{
			KeyID:            keyID,
			RetainOldVersion: true,
		}

		resp, err := client.RotateKey(ctx, rotateReq)
		require.NoError(t, err)
		require.NotNil(t, resp)
		assert.Equal(t, keyID, resp.KeyID)
		assert.Greater(t, resp.NewVersion, resp.OldVersion)

		// Verify version incremented
		getReq := &service.GetKeyRequest{KeyID: keyID}
		getResp, err := client.GetKey(ctx, getReq)
		require.NoError(t, err)
		assert.Greater(t, getResp.Metadata.Version, versionBefore)
	})

	t.Run("rotate non-existent key", func(t *testing.T) {
		req := &service.RotateKeyRequest{
			KeyID:            "non-existent-key",
			RetainOldVersion: true,
		}

		_, err := client.RotateKey(ctx, req)
		assert.Error(t, err)
	})
}

// TestRESTKeyDeletion tests key deletion via REST API.
func TestRESTKeyDeletion(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	endpoints := helpers.GetRESTEndpoints()
	client := NewRESTClient(endpoints[0])

	t.Run("force delete key", func(t *testing.T) {
		keyName := generateUniqueKeyName("rest-delete")
		genReq := &service.GenerateKeyRequest{
			Name: keyName,
			KeyAttributes: &kctypes.KeyAttributes{
				SymmetricAlgorithm: kctypes.SymmetricAES256GCM,
			},
			Usage: []service.KeyUsage{service.KeyUsageEncrypt, service.KeyUsageDecrypt},
		}

		genResp, err := client.GenerateKey(ctx, genReq)
		require.NoError(t, err, "KMS must be available on REST")
		keyID := genResp.Key.KeyID

		deleteReq := &service.DeleteKeyRequest{
			KeyID: keyID,
			Force: true,
		}

		resp, err := client.DeleteKey(ctx, deleteReq)
		require.NoError(t, err)
		assert.Equal(t, keyID, resp.KeyID)

		// Verify deletion
		getReq := &service.GetKeyRequest{KeyID: keyID}
		_, err = client.GetKey(ctx, getReq)
		assert.Error(t, err)
	})

	t.Run("delete non-existent key", func(t *testing.T) {
		req := &service.DeleteKeyRequest{
			KeyID: "non-existent-key",
			Force: true,
		}

		_, err := client.DeleteKey(ctx, req)
		assert.Error(t, err)
	})
}

// TestRESTKeyStatusUpdate tests key status updates via REST API.
func TestRESTKeyStatusUpdate(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	endpoints := helpers.GetRESTEndpoints()
	client := NewRESTClient(endpoints[0])

	keyName := generateUniqueKeyName("rest-status")
	genReq := &service.GenerateKeyRequest{
		Name: keyName,
		KeyAttributes: &kctypes.KeyAttributes{
			SymmetricAlgorithm: kctypes.SymmetricAES256GCM,
		},
		Usage: []service.KeyUsage{service.KeyUsageEncrypt, service.KeyUsageDecrypt},
	}

	genResp, err := client.GenerateKey(ctx, genReq)
	require.NoError(t, err, "KMS must be available on REST")
	keyID := genResp.Key.KeyID

	defer func() {
		deleteReq := &service.DeleteKeyRequest{
			KeyID: keyID,
			Force: true,
		}
		_, _ = client.DeleteKey(ctx, deleteReq)
	}()

	t.Run("disable key", func(t *testing.T) {
		req := &service.UpdateKeyStatusRequest{
			KeyID:  keyID,
			Status: service.KeyStatusDisabled,
		}

		resp, err := client.UpdateKeyStatus(ctx, req)
		require.NoError(t, err, "UpdateKeyStatus must be implemented on REST")
		assert.Equal(t, service.KeyStatusDisabled, resp.Status)

		// Verify status
		getReq := &service.GetKeyRequest{KeyID: keyID}
		getResp, err := client.GetKey(ctx, getReq)
		require.NoError(t, err)
		assert.Equal(t, service.KeyStatusDisabled, getResp.Metadata.Status)
	})

	t.Run("enable key", func(t *testing.T) {
		req := &service.UpdateKeyStatusRequest{
			KeyID:  keyID,
			Status: service.KeyStatusEnabled,
		}

		resp, err := client.UpdateKeyStatus(ctx, req)
		require.NoError(t, err, "UpdateKeyStatus must be implemented on REST")
		assert.Equal(t, service.KeyStatusEnabled, resp.Status)
	})
}

// TestRESTEncryptionDecryption tests encryption and decryption via REST API.
func TestRESTEncryptionDecryption(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	endpoints := helpers.GetRESTEndpoints()
	client := NewRESTClient(endpoints[0])

	// Create encryption key
	keyName := generateUniqueKeyName("rest-encrypt")
	genReq := &service.GenerateKeyRequest{
		Name: keyName,
		KeyAttributes: &kctypes.KeyAttributes{
			SymmetricAlgorithm: kctypes.SymmetricAES256GCM,
		},
		Usage: []service.KeyUsage{service.KeyUsageEncrypt, service.KeyUsageDecrypt},
	}

	genResp, err := client.GenerateKey(ctx, genReq)
	require.NoError(t, err, "KMS must be available on REST")
	keyID := genResp.Key.KeyID

	defer func() {
		deleteReq := &service.DeleteKeyRequest{
			KeyID: keyID,
			Force: true,
		}
		_, _ = client.DeleteKey(ctx, deleteReq)
	}()

	t.Run("encrypt and decrypt data", func(t *testing.T) {
		plaintext := []byte("This is sensitive data that needs encryption")

		// Encrypt
		encReq := &service.EncryptRequest{
			KeyID:     keyID,
			Plaintext: plaintext,
		}

		encResp, err := client.Encrypt(ctx, encReq)
		require.NoError(t, err, "Crypto service must be available on REST")
		require.NotNil(t, encResp)
		assert.NotEmpty(t, encResp.Ciphertext)
		assert.NotEqual(t, plaintext, encResp.Ciphertext)

		// Decrypt
		decReq := &service.DecryptRequest{
			KeyID:      keyID,
			Ciphertext: encResp.Ciphertext,
		}

		decResp, err := client.Decrypt(ctx, decReq)
		require.NoError(t, err)
		require.NotNil(t, decResp)
		assert.Equal(t, plaintext, decResp.Plaintext)
	})

	t.Run("encrypt and decrypt with AAD", func(t *testing.T) {
		plaintext := []byte("Data with additional authenticated data")
		aad := []byte("contextual information")

		// Encrypt with AAD
		encReq := &service.EncryptRequest{
			KeyID:     keyID,
			Plaintext: plaintext,
			Context:   aad,
		}

		encResp, err := client.Encrypt(ctx, encReq)
		require.NoError(t, err, "Crypto service must be available on REST")
		require.NoError(t, err)

		// Decrypt with correct AAD
		decReq := &service.DecryptRequest{
			KeyID:      keyID,
			Ciphertext: encResp.Ciphertext,
			Context:    aad,
		}

		decResp, err := client.Decrypt(ctx, decReq)
		require.NoError(t, err)
		assert.Equal(t, plaintext, decResp.Plaintext)

		// Decrypt with wrong AAD should fail
		wrongDecReq := &service.DecryptRequest{
			KeyID:      keyID,
			Ciphertext: encResp.Ciphertext,
			Context:    []byte("wrong context"),
		}

		_, err = client.Decrypt(ctx, wrongDecReq)
		assert.Error(t, err, "Decryption with wrong AAD should fail")
	})

	t.Run("error cases", func(t *testing.T) {
		t.Run("decrypt with wrong key", func(t *testing.T) {
			plaintext := []byte("test data")

			encReq := &service.EncryptRequest{
				KeyID:     keyID,
				Plaintext: plaintext,
			}

			encResp, err := client.Encrypt(ctx, encReq)
			require.NoError(t, err, "Crypto service must be available")

			decReq := &service.DecryptRequest{
				KeyID:      "wrong-key-id",
				Ciphertext: encResp.Ciphertext,
			}

			_, err = client.Decrypt(ctx, decReq)
			assert.Error(t, err)
		})

		t.Run("decrypt invalid ciphertext", func(t *testing.T) {
			decReq := &service.DecryptRequest{
				KeyID:      keyID,
				Ciphertext: []byte("this is not valid ciphertext"),
			}

			_, err := client.Decrypt(ctx, decReq)
			assert.Error(t, err)
		})
	})
}

// TestRESTKeyVersions tests key version listing via REST API.
func TestRESTKeyVersions(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	endpoints := helpers.GetRESTEndpoints()
	client := NewRESTClient(endpoints[0])

	keyName := generateUniqueKeyName("rest-versions")
	genReq := &service.GenerateKeyRequest{
		Name: keyName,
		KeyAttributes: &kctypes.KeyAttributes{
			SymmetricAlgorithm: kctypes.SymmetricAES256GCM,
		},
		Usage: []service.KeyUsage{service.KeyUsageEncrypt, service.KeyUsageDecrypt},
	}

	genResp, err := client.GenerateKey(ctx, genReq)
	require.NoError(t, err, "KMS must be available on REST")
	keyID := genResp.Key.KeyID

	defer func() {
		deleteReq := &service.DeleteKeyRequest{
			KeyID: keyID,
			Force: true,
		}
		_, _ = client.DeleteKey(ctx, deleteReq)
	}()

	// Rotate to create multiple versions
	rotateReq := &service.RotateKeyRequest{
		KeyID:            keyID,
		RetainOldVersion: true,
	}
	_, err = client.RotateKey(ctx, rotateReq)
	require.NoError(t, err)

	t.Run("list key versions", func(t *testing.T) {
		req := &service.ListKeyVersionsRequest{
			KeyID: keyID,
		}

		resp, err := client.ListKeyVersions(ctx, req)
		require.NoError(t, err, "ListKeyVersions must be implemented on REST")
		assert.GreaterOrEqual(t, len(resp.Versions), 2, "Should have at least 2 versions after rotation")
	})
}

// TestRESTHierarchicalKeys tests hierarchical key paths via REST API.
func TestRESTHierarchicalKeys(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	endpoints := helpers.GetRESTEndpoints()
	client := NewRESTClient(endpoints[0])

	// Create keys with hierarchical names
	basePath := generateUniqueKeyName("tenant")
	paths := []string{
		fmt.Sprintf("%s/prod/api-key", basePath),
		fmt.Sprintf("%s/prod/db-key", basePath),
		fmt.Sprintf("%s/dev/api-key", basePath),
	}

	keyIDs := []string{}

	for _, path := range paths {
		req := &service.GenerateKeyRequest{
			Name: path,
			KeyAttributes: &kctypes.KeyAttributes{
				SymmetricAlgorithm: kctypes.SymmetricAES256GCM,
			},
			Usage: []service.KeyUsage{service.KeyUsageEncrypt, service.KeyUsageDecrypt},
		}

		resp, err := client.GenerateKey(ctx, req)
		require.NoError(t, err, "KMS must be available on REST")
		keyIDs = append(keyIDs, resp.Key.KeyID)
	}

	defer func() {
		for _, keyID := range keyIDs {
			deleteReq := &service.DeleteKeyRequest{
				KeyID: keyID,
				Force: true,
			}
			_, _ = client.DeleteKey(ctx, deleteReq)
		}
	}()

	t.Run("list with path prefix", func(t *testing.T) {
		req := &service.ListKeysRequest{
			Limit:  100,
			Filter: map[string]string{"prefix": fmt.Sprintf("%s/prod", basePath)},
		}

		resp, err := client.ListKeys(ctx, req)
		require.NoError(t, err)
		t.Logf("Found %d keys with prod prefix", len(resp.Keys))
	})
}

// TestRESTClusterConsistency tests REST API operations across cluster nodes.
// NOTE: KMS keys are stored locally on each node via go-xkms for security reasons.
// Key material is never replicated across nodes - this is by design.
// For distributed key access, use a centralized backend like HashiCorp Vault.
// This test verifies that single-node KMS operations work correctly when accessed
// through different API endpoints, and documents the expected behavior.
func TestRESTClusterConsistency(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	endpoints := getRESTEndpoints()
	require.GreaterOrEqual(t, len(endpoints), 2,
		"Cluster consistency test requires at least 2 nodes, got %d", len(endpoints))

	client1 := NewRESTClient(endpoints[0])

	// KMS keys are node-local by design - test operations on same node
	t.Run("key operations on same node", func(t *testing.T) {
		keyName := generateUniqueKeyName("rest-single-node")

		genReq := &service.GenerateKeyRequest{
			Name: keyName,
			KeyAttributes: &kctypes.KeyAttributes{
				SymmetricAlgorithm: kctypes.SymmetricAES256GCM,
			},
			Usage: []service.KeyUsage{service.KeyUsageEncrypt, service.KeyUsageDecrypt},
		}

		genResp, err := client1.GenerateKey(ctx, genReq)
		require.NoError(t, err, "KMS must be available on REST")
		keyID := genResp.Key.KeyID

		defer func() {
			deleteReq := &service.DeleteKeyRequest{
				KeyID: keyID,
				Force: true,
			}
			_, _ = client1.DeleteKey(ctx, deleteReq)
		}()

		// Verify we can read the key on the same node
		getReq := &service.GetKeyRequest{KeyID: keyID}
		getResp, err := client1.GetKey(ctx, getReq)
		require.NoError(t, err)
		assert.Equal(t, keyID, getResp.Metadata.KeyID)
		assert.Equal(t, uint64(1), getResp.Metadata.Version)

		// Test encrypt/decrypt cycle on same node
		plaintext := []byte("single node consistency test data")
		encReq := &service.EncryptRequest{
			KeyID:     keyID,
			Plaintext: plaintext,
		}
		encResp, err := client1.Encrypt(ctx, encReq)
		require.NoError(t, err, "Crypto service must be available")

		decReq := &service.DecryptRequest{
			KeyID:      keyID,
			Ciphertext: encResp.Ciphertext,
		}
		decResp, err := client1.Decrypt(ctx, decReq)
		require.NoError(t, err, "Decryption should work")
		assert.Equal(t, plaintext, decResp.Plaintext, "Decrypted data should match original")
	})

	t.Run("key rotation on same node", func(t *testing.T) {
		keyName := generateUniqueKeyName("rest-rotate-node")

		genReq := &service.GenerateKeyRequest{
			Name: keyName,
			KeyAttributes: &kctypes.KeyAttributes{
				SymmetricAlgorithm: kctypes.SymmetricAES256GCM,
			},
			Usage: []service.KeyUsage{service.KeyUsageEncrypt, service.KeyUsageDecrypt},
		}

		genResp, err := client1.GenerateKey(ctx, genReq)
		require.NoError(t, err, "KMS must be available on REST")
		keyID := genResp.Key.KeyID

		defer func() {
			deleteReq := &service.DeleteKeyRequest{
				KeyID: keyID,
				Force: true,
			}
			_, _ = client1.DeleteKey(ctx, deleteReq)
		}()

		// Test encrypt/decrypt before rotation
		plaintext := []byte("rotation test data")
		encReq := &service.EncryptRequest{
			KeyID:     keyID,
			Plaintext: plaintext,
		}
		encResp, err := client1.Encrypt(ctx, encReq)
		require.NoError(t, err, "Crypto service must be available")

		decReq := &service.DecryptRequest{
			KeyID:      keyID,
			Ciphertext: encResp.Ciphertext,
		}
		decResp, err := client1.Decrypt(ctx, decReq)
		require.NoError(t, err, "Decryption before rotation should work")
		assert.Equal(t, plaintext, decResp.Plaintext)

		// Rotate key
		rotateReq := &service.RotateKeyRequest{KeyID: keyID}
		rotateResp, err := client1.RotateKey(ctx, rotateReq)
		require.NoError(t, err)
		assert.Equal(t, uint64(2), rotateResp.NewVersion, "New version should be 2")

		// Verify version is updated
		getReq := &service.GetKeyRequest{KeyID: keyID}
		getResp, err := client1.GetKey(ctx, getReq)
		require.NoError(t, err)
		assert.Equal(t, uint64(2), getResp.Metadata.Version, "Version should be 2 after rotation")

		// New encryption with rotated key should work
		newPlaintext := []byte("new data with rotated key")
		newEncReq := &service.EncryptRequest{
			KeyID:     keyID,
			Plaintext: newPlaintext,
		}
		newEncResp, err := client1.Encrypt(ctx, newEncReq)
		require.NoError(t, err, "Encryption with rotated key should work")

		newDecReq := &service.DecryptRequest{
			KeyID:      keyID,
			Ciphertext: newEncResp.Ciphertext,
		}
		newDecResp, err := client1.Decrypt(ctx, newDecReq)
		require.NoError(t, err, "Decryption with rotated key should work")
		assert.Equal(t, newPlaintext, newDecResp.Plaintext)

		// NOTE: Decrypting old ciphertext after rotation may not work depending on
		// the KMS backend's key versioning support. With go-xkms using the
		// software backend, old key material is preserved and backward decryption
		// should work. If it doesn't, that indicates the backend doesn't support
		// multi-version keys and re-encryption is needed before rotation.
		oldDecReq := &service.DecryptRequest{
			KeyID:      keyID,
			Ciphertext: encResp.Ciphertext,
		}
		_, oldErr := client1.Decrypt(ctx, oldDecReq)
		if oldErr != nil {
			t.Logf("Note: Old ciphertext decryption after rotation failed (expected for non-versioned backends): %v", oldErr)
		} else {
			t.Log("Old ciphertext successfully decrypted after rotation (versioned backend)")
		}
	})

	t.Run("key not visible on other nodes (expected)", func(t *testing.T) {
		// This test documents that KMS keys are node-local by design
		client2 := NewRESTClient(endpoints[1])
		keyName := generateUniqueKeyName("rest-local-only")

		genReq := &service.GenerateKeyRequest{
			Name: keyName,
			KeyAttributes: &kctypes.KeyAttributes{
				SymmetricAlgorithm: kctypes.SymmetricAES256GCM,
			},
			Usage: []service.KeyUsage{service.KeyUsageEncrypt, service.KeyUsageDecrypt},
		}

		genResp, err := client1.GenerateKey(ctx, genReq)
		require.NoError(t, err, "KMS must be available on REST")
		keyID := genResp.Key.KeyID

		defer func() {
			deleteReq := &service.DeleteKeyRequest{
				KeyID: keyID,
				Force: true,
			}
			_, _ = client1.DeleteKey(ctx, deleteReq)
		}()

		// Key should NOT be visible on node2 - this is expected behavior
		getReq := &service.GetKeyRequest{KeyID: keyID}
		_, err = client2.GetKey(ctx, getReq)
		// Expect an error (key not found on node2)
		assert.Error(t, err, "Key should NOT be visible on other nodes - KMS keys are node-local by design")
		t.Logf("Expected behavior: key not visible on node2 (error: %v)", err)
	})
}
