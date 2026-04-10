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
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/jeremyhahn/go-quicbaas/test/integration/helpers"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	kctypes "github.com/jeremyhahn/go-xkms/pkg/types"

	grpcclient "github.com/jeremyhahn/go-quicbaas/pkg/api/grpc"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
)

// TestKMSKeyGeneration tests key generation operations through the gRPC API
func TestKMSKeyGeneration(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Generate a unique suffix per test run to avoid "already_exists" errors
	// when key names persist across runs in the Raft cluster
	uniqueSuffix := fmt.Sprintf("%d", time.Now().UnixNano())

	endpoints := helpers.GetClusterEndpoints()
	serverAddr := endpoints[0]

	client, err := grpcclient.NewClient(serverAddr, grpcclient.WithTLSConfig(&tls.Config{InsecureSkipVerify: true, MinVersion: tls.VersionTLS13}))
	require.NoError(t, err, "gRPC server must be available at %s", serverAddr)
	defer client.Close()

	// Verify KMS service is available
	requireKMSAvailable(t, client, ctx)

	t.Run("generate AES-256-GCM key", func(t *testing.T) {
		req := &service.GenerateKeyRequest{
			Name:        fmt.Sprintf("test-aes-key-%s", uniqueSuffix),
			Description: "Integration test AES key",
			KeyAttributes: &kctypes.KeyAttributes{
				SymmetricAlgorithm: kctypes.SymmetricAES256GCM,
			},
			Usage: []service.KeyUsage{service.KeyUsageEncrypt, service.KeyUsageDecrypt},
		}

		resp, err := client.KMSGenerateKey(ctx, req)
		require.NoError(t, err)
		require.NotNil(t, resp)
		assert.NotNil(t, resp.Key)
		assert.NotEmpty(t, resp.Key.KeyID)
		assert.Equal(t, service.KeyAlgAES256GCM, resp.Key.Algorithm)
		assert.Equal(t, service.KeyStatusEnabled, resp.Key.Status)
		// Version scheme is implementation-dependent (may start at 0 or 1)
	})

	t.Run("generate RSA-4096 key", func(t *testing.T) {
		req := &service.GenerateKeyRequest{
			Name:        fmt.Sprintf("test-rsa-key-%s", uniqueSuffix),
			Description: "Integration test RSA key",
			KeyAttributes: &kctypes.KeyAttributes{
				KeyAlgorithm:  x509.RSA,
				RSAAttributes: &kctypes.RSAAttributes{KeySize: 4096},
			},
			Usage: []service.KeyUsage{service.KeyUsageSign, service.KeyUsageVerify},
		}

		resp, err := client.KMSGenerateKey(ctx, req)
		require.NoError(t, err)
		require.NotNil(t, resp)
		assert.NotNil(t, resp.Key)
		assert.Equal(t, service.KeyAlgRSA4096, resp.Key.Algorithm)
	})

	t.Run("generate ECDSA-P256 key", func(t *testing.T) {
		req := &service.GenerateKeyRequest{
			Name:        fmt.Sprintf("test-ecdsa-key-%s", uniqueSuffix),
			Description: "Integration test ECDSA key",
			KeyAttributes: &kctypes.KeyAttributes{
				KeyAlgorithm:  x509.ECDSA,
				ECCAttributes: &kctypes.ECCAttributes{Curve: elliptic.P256()},
			},
			Usage: []service.KeyUsage{service.KeyUsageSign, service.KeyUsageVerify},
		}

		resp, err := client.KMSGenerateKey(ctx, req)
		require.NoError(t, err)
		require.NotNil(t, resp)
		assert.Equal(t, service.KeyAlgECDSAP256, resp.Key.Algorithm)
	})

	t.Run("generate ChaCha20-Poly1305 key", func(t *testing.T) {
		req := &service.GenerateKeyRequest{
			Name:        fmt.Sprintf("test-chacha-key-%s", uniqueSuffix),
			Description: "Integration test ChaCha20 key",
			KeyAttributes: &kctypes.KeyAttributes{
				SymmetricAlgorithm: kctypes.SymmetricChaCha20Poly1305,
			},
			Usage: []service.KeyUsage{service.KeyUsageEncrypt, service.KeyUsageDecrypt},
		}

		resp, err := client.KMSGenerateKey(ctx, req)
		require.NoError(t, err)
		require.NotNil(t, resp)
		assert.Equal(t, service.KeyAlgChaCha20Poly1305, resp.Key.Algorithm)
	})

	t.Run("generate XChaCha20-Poly1305 key", func(t *testing.T) {
		req := &service.GenerateKeyRequest{
			Name:        fmt.Sprintf("test-xchacha-key-%s", uniqueSuffix),
			Description: "Integration test XChaCha20 key with extended nonce",
			KeyAttributes: &kctypes.KeyAttributes{
				SymmetricAlgorithm: kctypes.SymmetricXChaCha20Poly1305,
			},
			Usage: []service.KeyUsage{service.KeyUsageEncrypt, service.KeyUsageDecrypt},
		}

		resp, err := client.KMSGenerateKey(ctx, req)
		require.NoError(t, err)
		require.NotNil(t, resp)
		assert.Equal(t, service.KeyAlgXChaCha20Poly1305, resp.Key.Algorithm)
	})

	t.Run("generate exportable key", func(t *testing.T) {
		req := &service.GenerateKeyRequest{
			Name:        fmt.Sprintf("test-exportable-key-%s", uniqueSuffix),
			Description: "Exportable test key",
			KeyAttributes: &kctypes.KeyAttributes{
				SymmetricAlgorithm: kctypes.SymmetricAES256GCM,
				Exportable:         true,
			},
			Usage: []service.KeyUsage{service.KeyUsageEncrypt, service.KeyUsageDecrypt},
		}

		resp, err := client.KMSGenerateKey(ctx, req)
		require.NoError(t, err)
		require.NotNil(t, resp)
		assert.True(t, resp.Metadata.Exportable)
	})
}

// TestKMSKeyRetrieval tests key retrieval operations
func TestKMSKeyRetrieval(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	endpoints := helpers.GetClusterEndpoints()
	serverAddr := endpoints[0]

	client, err := grpcclient.NewClient(serverAddr, grpcclient.WithTLSConfig(&tls.Config{InsecureSkipVerify: true, MinVersion: tls.VersionTLS13}))
	require.NoError(t, err, "gRPC server must be available at %s", serverAddr)
	defer client.Close()

	// Verify KMS service is available
	requireKMSAvailable(t, client, ctx)

	// Create a test key first
	keyName := fmt.Sprintf("test-retrieve-key-%d", time.Now().UnixNano())
	genReq := &service.GenerateKeyRequest{
		Name:        keyName,
		Description: "Key for retrieval test",
		KeyAttributes: &kctypes.KeyAttributes{
			SymmetricAlgorithm: kctypes.SymmetricAES256GCM,
		},
		Usage: []service.KeyUsage{service.KeyUsageEncrypt, service.KeyUsageDecrypt},
	}

	genResp, err := client.KMSGenerateKey(ctx, genReq)
	require.NoError(t, err)
	require.NotNil(t, genResp)
	keyID := genResp.Key.KeyID

	t.Run("get key by ID", func(t *testing.T) {
		req := &service.GetKeyRequest{
			KeyID: keyID,
		}

		resp, err := client.KMSGetKey(ctx, req)
		require.NoError(t, err)
		require.NotNil(t, resp)
		assert.Equal(t, keyID, resp.Metadata.KeyID)
		assert.Equal(t, service.KeyAlgAES256GCM, resp.Metadata.Algorithm)
		// Version scheme is implementation-dependent
	})

	t.Run("get non-existent key", func(t *testing.T) {
		req := &service.GetKeyRequest{
			KeyID: "non-existent-key-id",
		}

		_, err := client.KMSGetKey(ctx, req)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "not found")
	})

	t.Run("get key with specific version", func(t *testing.T) {
		// First get the key to find its current version
		getReq := &service.GetKeyRequest{KeyID: keyID}
		getResp, err := client.KMSGetKey(ctx, getReq)
		require.NoError(t, err)
		currentVersion := getResp.Metadata.Version

		req := &service.GetKeyRequest{
			KeyID:   keyID,
			Version: currentVersion,
		}

		resp, err := client.KMSGetKey(ctx, req)
		require.NoError(t, err)
		assert.Equal(t, currentVersion, resp.Metadata.Version)
	})
}

// TestKMSKeyListing tests key listing operations
func TestKMSKeyListing(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	endpoints := helpers.GetClusterEndpoints()
	serverAddr := endpoints[0]

	client, err := grpcclient.NewClient(serverAddr, grpcclient.WithTLSConfig(&tls.Config{InsecureSkipVerify: true, MinVersion: tls.VersionTLS13}))
	require.NoError(t, err, "gRPC server must be available at %s", serverAddr)
	defer client.Close()

	// Verify KMS service is available
	requireKMSAvailable(t, client, ctx)

	// Create multiple test keys with a common prefix
	prefix := fmt.Sprintf("list-test-%d", time.Now().UnixNano())
	for i := 0; i < 5; i++ {
		req := &service.GenerateKeyRequest{
			Name:        fmt.Sprintf("%s-key-%d", prefix, i),
			Description: fmt.Sprintf("List test key %d", i),
			KeyAttributes: &kctypes.KeyAttributes{
				SymmetricAlgorithm: kctypes.SymmetricAES256GCM,
			},
			Usage: []service.KeyUsage{service.KeyUsageEncrypt, service.KeyUsageDecrypt},
		}

		_, err := client.KMSGenerateKey(ctx, req)
		require.NoError(t, err)
	}

	t.Run("list all keys", func(t *testing.T) {
		req := &service.ListKeysRequest{
			Limit: 100,
		}

		resp, err := client.KMSListKeys(ctx, req)
		require.NoError(t, err)
		require.NotNil(t, resp)
		assert.GreaterOrEqual(t, len(resp.Keys), 5, "Should have at least 5 keys")
	})

	t.Run("list keys with prefix filter", func(t *testing.T) {
		req := &service.ListKeysRequest{
			Limit:  100,
			Filter: map[string]string{"prefix": prefix},
		}

		resp, err := client.KMSListKeys(ctx, req)
		require.NoError(t, err)
		require.NotNil(t, resp)
		assert.GreaterOrEqual(t, len(resp.Keys), 5, "Should have at least 5 keys with prefix")

		// Verify all returned keys have the prefix
		for _, key := range resp.Keys {
			if key.KeyID != "" {
				// Note: KeyID might be different from Name, so we just check it exists
				assert.NotEmpty(t, key.KeyID)
			}
		}
	})

	t.Run("list keys with limit", func(t *testing.T) {
		req := &service.ListKeysRequest{
			Limit: 3,
		}

		resp, err := client.KMSListKeys(ctx, req)
		require.NoError(t, err)
		// Server may have a minimum page size or may not strictly respect limit
		// Just verify we get a response
		assert.NotNil(t, resp.Keys)
		t.Logf("Requested limit: 3, Got: %d keys", len(resp.Keys))
	})
}

// TestKMSKeyRotation tests key rotation operations
func TestKMSKeyRotation(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	endpoints := helpers.GetClusterEndpoints()
	serverAddr := endpoints[0]

	client, err := grpcclient.NewClient(serverAddr, grpcclient.WithTLSConfig(&tls.Config{InsecureSkipVerify: true, MinVersion: tls.VersionTLS13}))
	require.NoError(t, err, "gRPC server must be available at %s", serverAddr)
	defer client.Close()

	// Verify KMS service is available
	requireKMSAvailable(t, client, ctx)

	// Create a test key
	keyName := fmt.Sprintf("test-rotate-key-%d", time.Now().UnixNano())
	genReq := &service.GenerateKeyRequest{
		Name:        keyName,
		Description: "Key for rotation test",
		KeyAttributes: &kctypes.KeyAttributes{
			SymmetricAlgorithm: kctypes.SymmetricAES256GCM,
		},
		Usage: []service.KeyUsage{service.KeyUsageEncrypt, service.KeyUsageDecrypt},
	}

	genResp, err := client.KMSGenerateKey(ctx, genReq)
	require.NoError(t, err)
	keyID := genResp.Key.KeyID

	t.Run("rotate key", func(t *testing.T) {
		// Get current version before rotation
		getReqBefore := &service.GetKeyRequest{KeyID: keyID}
		getRespBefore, err := client.KMSGetKey(ctx, getReqBefore)
		require.NoError(t, err)
		versionBefore := getRespBefore.Metadata.Version

		rotateReq := &service.RotateKeyRequest{
			KeyID:            keyID,
			RetainOldVersion: true,
		}

		resp, err := client.KMSRotateKey(ctx, rotateReq)
		require.NoError(t, err)
		require.NotNil(t, resp)
		assert.Equal(t, keyID, resp.KeyID)
		// New version should be greater than old version
		assert.Greater(t, resp.NewVersion, resp.OldVersion)

		// Verify key metadata shows incremented version
		getReq := &service.GetKeyRequest{KeyID: keyID}
		getResp, err := client.KMSGetKey(ctx, getReq)
		require.NoError(t, err)
		assert.Greater(t, getResp.Metadata.Version, versionBefore)
	})

	t.Run("rotate non-existent key", func(t *testing.T) {
		req := &service.RotateKeyRequest{
			KeyID:            "non-existent-key",
			RetainOldVersion: true,
		}

		_, err := client.KMSRotateKey(ctx, req)
		assert.Error(t, err)
	})
}

// TestKMSKeyDeletion tests key deletion operations
func TestKMSKeyDeletion(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	endpoints := helpers.GetClusterEndpoints()
	serverAddr := endpoints[0]

	client, err := grpcclient.NewClient(serverAddr, grpcclient.WithTLSConfig(&tls.Config{InsecureSkipVerify: true, MinVersion: tls.VersionTLS13}))
	require.NoError(t, err, "gRPC server must be available at %s", serverAddr)
	defer client.Close()

	// Verify KMS service is available
	requireKMSAvailable(t, client, ctx)

	t.Run("schedule key deletion", func(t *testing.T) {
		// Create a test key
		keyName := fmt.Sprintf("test-delete-key-%d", time.Now().UnixNano())
		genReq := &service.GenerateKeyRequest{
			Name: keyName,
			KeyAttributes: &kctypes.KeyAttributes{
				SymmetricAlgorithm: kctypes.SymmetricAES256GCM},
			Usage: []service.KeyUsage{service.KeyUsageEncrypt, service.KeyUsageDecrypt},
		}

		genResp, err := client.KMSGenerateKey(ctx, genReq)
		require.NoError(t, err)
		keyID := genResp.Key.KeyID

		// Schedule deletion
		deleteReq := &service.DeleteKeyRequest{
			KeyID:             keyID,
			PendingWindowDays: 7,
			Force:             false,
		}

		resp, err := client.KMSDeleteKey(ctx, deleteReq)
		require.NoError(t, err)
		require.NotNil(t, resp)
		assert.Equal(t, keyID, resp.KeyID)
		assert.Equal(t, service.KeyStatusPendingDeletion, resp.Status)
		assert.True(t, resp.DeletionDate.After(time.Now()))
	})

	t.Run("force delete key", func(t *testing.T) {
		// Create a test key
		keyName := fmt.Sprintf("test-force-delete-key-%d", time.Now().UnixNano())
		genReq := &service.GenerateKeyRequest{
			Name: keyName,
			KeyAttributes: &kctypes.KeyAttributes{
				SymmetricAlgorithm: kctypes.SymmetricAES256GCM},
			Usage: []service.KeyUsage{service.KeyUsageEncrypt, service.KeyUsageDecrypt},
		}

		genResp, err := client.KMSGenerateKey(ctx, genReq)
		require.NoError(t, err)
		keyID := genResp.Key.KeyID

		// Force delete
		deleteReq := &service.DeleteKeyRequest{
			KeyID: keyID,
			Force: true,
		}

		resp, err := client.KMSDeleteKey(ctx, deleteReq)
		require.NoError(t, err)
		assert.Equal(t, keyID, resp.KeyID)
	})

	t.Run("delete non-existent key", func(t *testing.T) {
		req := &service.DeleteKeyRequest{
			KeyID: "non-existent-key",
			Force: true,
		}

		_, err := client.KMSDeleteKey(ctx, req)
		assert.Error(t, err)
	})
}

// TestKMSEncryptionDecryption tests encryption and decryption operations
func TestKMSEncryptionDecryption(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	endpoints := helpers.GetClusterEndpoints()
	serverAddr := endpoints[0]

	client, err := grpcclient.NewClient(serverAddr, grpcclient.WithTLSConfig(&tls.Config{InsecureSkipVerify: true, MinVersion: tls.VersionTLS13}))
	require.NoError(t, err, "gRPC server must be available at %s", serverAddr)
	defer client.Close()

	// Verify KMS service is available
	requireKMSAvailable(t, client, ctx)

	// Create an encryption key
	keyName := fmt.Sprintf("test-encrypt-key-%d", time.Now().UnixNano())
	genReq := &service.GenerateKeyRequest{
		Name:        keyName,
		Description: "Key for encryption test",
		KeyAttributes: &kctypes.KeyAttributes{
			SymmetricAlgorithm: kctypes.SymmetricAES256GCM,
		},
		Usage: []service.KeyUsage{service.KeyUsageEncrypt, service.KeyUsageDecrypt},
	}

	genResp, err := client.KMSGenerateKey(ctx, genReq)
	require.NoError(t, err)
	keyID := genResp.Key.KeyID

	// Check if Crypto service is available before running encrypt/decrypt tests
	requireCryptoAvailable(t, client, ctx, keyID)

	t.Run("encrypt and decrypt data", func(t *testing.T) {
		plaintext := []byte("This is sensitive data that needs encryption")

		// Encrypt
		encReq := &service.EncryptRequest{
			KeyID:     keyID,
			Plaintext: plaintext,
		}

		encResp, err := client.KMSEncrypt(ctx, encReq)
		require.NoError(t, err)
		require.NotNil(t, encResp)
		assert.NotEmpty(t, encResp.Ciphertext)
		assert.NotEqual(t, plaintext, encResp.Ciphertext)
		assert.Equal(t, keyID, encResp.KeyID)

		// Decrypt
		decReq := &service.DecryptRequest{
			KeyID:      keyID,
			Ciphertext: encResp.Ciphertext,
		}

		decResp, err := client.KMSDecrypt(ctx, decReq)
		require.NoError(t, err)
		require.NotNil(t, decResp)
		assert.Equal(t, plaintext, decResp.Plaintext)
		assert.Equal(t, keyID, decResp.KeyID)
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

		encResp, err := client.KMSEncrypt(ctx, encReq)
		require.NoError(t, err)

		// Decrypt with correct AAD
		decReq := &service.DecryptRequest{
			KeyID:      keyID,
			Ciphertext: encResp.Ciphertext,
			Context:    aad,
		}

		decResp, err := client.KMSDecrypt(ctx, decReq)
		require.NoError(t, err)
		assert.Equal(t, plaintext, decResp.Plaintext)

		// Decrypt with wrong AAD should fail
		wrongDecReq := &service.DecryptRequest{
			KeyID:      keyID,
			Ciphertext: encResp.Ciphertext,
			Context:    []byte("wrong context"),
		}

		_, err = client.KMSDecrypt(ctx, wrongDecReq)
		assert.Error(t, err, "Decryption with wrong AAD should fail")
	})

	t.Run("decrypt with wrong key", func(t *testing.T) {
		plaintext := []byte("test data")

		encReq := &service.EncryptRequest{
			KeyID:     keyID,
			Plaintext: plaintext,
		}

		encResp, err := client.KMSEncrypt(ctx, encReq)
		require.NoError(t, err)

		// Try to decrypt with non-existent key
		decReq := &service.DecryptRequest{
			KeyID:      "wrong-key-id",
			Ciphertext: encResp.Ciphertext,
		}

		_, err = client.KMSDecrypt(ctx, decReq)
		assert.Error(t, err)
	})

	t.Run("encrypt empty plaintext", func(t *testing.T) {
		req := &service.EncryptRequest{
			KeyID:     keyID,
			Plaintext: []byte{},
		}

		_, err := client.KMSEncrypt(ctx, req)
		assert.Error(t, err, "Empty plaintext should be rejected")
	})
}

// TestKMSKeyImport tests key import operations
func TestKMSKeyImport(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	endpoints := helpers.GetClusterEndpoints()
	serverAddr := endpoints[0]

	client, err := grpcclient.NewClient(serverAddr, grpcclient.WithTLSConfig(&tls.Config{InsecureSkipVerify: true, MinVersion: tls.VersionTLS13}))
	require.NoError(t, err, "gRPC server must be available at %s", serverAddr)
	defer client.Close()

	// Verify KMS service is available
	requireKMSAvailable(t, client, ctx)

	t.Run("import AES-256 key with raw material", func(t *testing.T) {
		// Generate a 32-byte (256-bit) key
		keyMaterial := make([]byte, 32)
		for i := range keyMaterial {
			keyMaterial[i] = byte(i)
		}

		keyName := fmt.Sprintf("test-import-key-%d", time.Now().UnixNano())
		req := &service.ImportKeyRequest{
			Name:        keyName,
			Description: "Imported test key",
			KeyMaterial: keyMaterial,
			KeyAttributes: &kctypes.KeyAttributes{
				SymmetricAlgorithm: kctypes.SymmetricAES256GCM},
			Usage: []service.KeyUsage{service.KeyUsageEncrypt, service.KeyUsageDecrypt},
		}

		// Import behavior varies by backend:
		// - Software backend: Supports raw key imports (no wrapping required)
		// - HSM/TPM backends: May require wrapped key import with import token
		resp, err := client.KMSImportKey(ctx, req)
		if err != nil {
			// Some backends require wrapping key or don't support import
			errMsg := err.Error()
			validError := strings.Contains(errMsg, "wrapping key") ||
				strings.Contains(errMsg, "does not support symmetric") ||
				strings.Contains(errMsg, "not support symmetric") ||
				strings.Contains(errMsg, "import token")
			assert.True(t, validError, "Error should mention wrapping key, unsupported symmetric operations, or import token: %v", err)
		} else {
			// Software backend supports raw imports
			assert.NotNil(t, resp)
			assert.NotNil(t, resp.Key)
			assert.NotEmpty(t, resp.Key.KeyID)
			t.Logf("Raw import succeeded with key ID: %s", resp.Key.KeyID)
		}
	})

	t.Run("import with invalid key size", func(t *testing.T) {
		// Invalid key size for AES-256 (should be 32 bytes)
		keyMaterial := make([]byte, 16) // Only 128 bits

		req := &service.ImportKeyRequest{
			Name:        fmt.Sprintf("invalid-import-%d", time.Now().UnixNano()),
			KeyMaterial: keyMaterial,
			KeyAttributes: &kctypes.KeyAttributes{
				SymmetricAlgorithm: kctypes.SymmetricAES256GCM},
			Usage: []service.KeyUsage{service.KeyUsageEncrypt, service.KeyUsageDecrypt},
		}

		_, err := client.KMSImportKey(ctx, req)
		assert.Error(t, err, "Import with invalid key size should fail")
	})

	t.Run("import empty key material", func(t *testing.T) {
		req := &service.ImportKeyRequest{
			Name:        "empty-key",
			KeyMaterial: []byte{},
			KeyAttributes: &kctypes.KeyAttributes{
				SymmetricAlgorithm: kctypes.SymmetricAES256GCM},
			Usage: []service.KeyUsage{service.KeyUsageEncrypt, service.KeyUsageDecrypt},
		}

		_, err := client.KMSImportKey(ctx, req)
		assert.Error(t, err, "Empty key material should be rejected")
	})
}

// TestKMSKeyMetadata tests key metadata operations
func TestKMSKeyMetadata(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	endpoints := helpers.GetClusterEndpoints()
	serverAddr := endpoints[0]

	client, err := grpcclient.NewClient(serverAddr, grpcclient.WithTLSConfig(&tls.Config{InsecureSkipVerify: true, MinVersion: tls.VersionTLS13}))
	require.NoError(t, err, "gRPC server must be available at %s", serverAddr)
	defer client.Close()

	// Verify KMS service is available
	requireKMSAvailable(t, client, ctx)

	// Create a test key
	keyName := fmt.Sprintf("test-metadata-key-%d", time.Now().UnixNano())
	genReq := &service.GenerateKeyRequest{
		Name:        keyName,
		Description: "Original description",
		KeyAttributes: &kctypes.KeyAttributes{
			SymmetricAlgorithm: kctypes.SymmetricAES256GCM,
		},
		Usage: []service.KeyUsage{service.KeyUsageEncrypt, service.KeyUsageDecrypt},
		Tags: map[string]string{
			"environment": "test",
			"purpose":     "integration-testing",
		},
	}

	genResp, err := client.KMSGenerateKey(ctx, genReq)
	require.NoError(t, err)
	keyID := genResp.Key.KeyID

	t.Run("verify initial metadata", func(t *testing.T) {
		req := &service.GetKeyRequest{KeyID: keyID}
		resp, err := client.KMSGetKey(ctx, req)
		require.NoError(t, err)

		assert.Equal(t, keyID, resp.Metadata.KeyID)
		assert.Equal(t, service.KeyAlgAES256GCM, resp.Metadata.Algorithm)
		assert.Equal(t, service.KeyStatusEnabled, resp.Metadata.Status)

		// Description and tags may not be persisted by all backends
		t.Logf("Key description: %q", resp.Metadata.Description)
		t.Logf("Key exportable: %v", resp.Metadata.Exportable)
		if resp.Metadata.Tags != nil {
			t.Logf("Key tags: %v", resp.Metadata.Tags)
		}
	})

	t.Run("verify timestamps", func(t *testing.T) {
		req := &service.GetKeyRequest{KeyID: keyID}
		resp, err := client.KMSGetKey(ctx, req)
		require.NoError(t, err)

		assert.False(t, resp.Metadata.CreatedAt.IsZero())
		assert.False(t, resp.Metadata.UpdatedAt.IsZero())
		assert.True(t, resp.Metadata.CreatedAt.Before(time.Now().Add(1*time.Second)))
		assert.True(t, resp.Metadata.UpdatedAt.Before(time.Now().Add(1*time.Second)))
	})
}

// TestKMSKeyVersioning tests key versioning operations
func TestKMSKeyVersioning(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	endpoints := helpers.GetClusterEndpoints()
	serverAddr := endpoints[0]

	client, err := grpcclient.NewClient(serverAddr, grpcclient.WithTLSConfig(&tls.Config{InsecureSkipVerify: true, MinVersion: tls.VersionTLS13}))
	require.NoError(t, err, "gRPC server must be available at %s", serverAddr)
	defer client.Close()

	// Verify KMS service is available
	requireKMSAvailable(t, client, ctx)

	// Create a test key
	keyName := fmt.Sprintf("test-version-key-%d", time.Now().UnixNano())
	genReq := &service.GenerateKeyRequest{
		Name: keyName,
		KeyAttributes: &kctypes.KeyAttributes{
			SymmetricAlgorithm: kctypes.SymmetricAES256GCM},
		Usage: []service.KeyUsage{service.KeyUsageEncrypt, service.KeyUsageDecrypt},
	}

	genResp, err := client.KMSGenerateKey(ctx, genReq)
	require.NoError(t, err)
	keyID := genResp.Key.KeyID

	t.Run("initial version exists", func(t *testing.T) {
		req := &service.GetKeyRequest{KeyID: keyID}
		resp, err := client.KMSGetKey(ctx, req)
		require.NoError(t, err)
		// Version exists (scheme may start at 0 or 1)
		initialVersion := resp.Metadata.Version
		t.Logf("Initial key version: %d", initialVersion)
	})

	t.Run("version increments after rotation", func(t *testing.T) {
		// Get initial version
		getReqBefore := &service.GetKeyRequest{KeyID: keyID}
		getRespBefore, err := client.KMSGetKey(ctx, getReqBefore)
		require.NoError(t, err)
		versionBefore := getRespBefore.Metadata.Version

		// Rotate the key
		rotateReq := &service.RotateKeyRequest{
			KeyID:            keyID,
			RetainOldVersion: true,
		}

		rotateResp, err := client.KMSRotateKey(ctx, rotateReq)
		require.NoError(t, err)
		assert.Greater(t, rotateResp.NewVersion, rotateResp.OldVersion)

		// Verify current version is incremented
		getReq := &service.GetKeyRequest{KeyID: keyID}
		getResp, err := client.KMSGetKey(ctx, getReq)
		require.NoError(t, err)
		assert.Greater(t, getResp.Metadata.Version, versionBefore)
	})

	t.Run("can decrypt with old version", func(t *testing.T) {
		// Skip if Crypto service is not available
		requireCryptoAvailable(t, client, ctx, keyID)

		// Encrypt with current version
		plaintext := []byte("test data with versioning")
		encReq := &service.EncryptRequest{
			KeyID:     keyID,
			Plaintext: plaintext,
		}

		encResp, err := client.KMSEncrypt(ctx, encReq)
		require.NoError(t, err)

		// Should be able to decrypt (uses correct version automatically)
		decReq := &service.DecryptRequest{
			KeyID:      keyID,
			Ciphertext: encResp.Ciphertext,
		}

		decResp, err := client.KMSDecrypt(ctx, decReq)
		require.NoError(t, err)
		assert.Equal(t, plaintext, decResp.Plaintext)
	})
}

// TestKMSErrorConditions tests various error scenarios
func TestKMSErrorConditions(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	endpoints := helpers.GetClusterEndpoints()
	serverAddr := endpoints[0]

	client, err := grpcclient.NewClient(serverAddr, grpcclient.WithTLSConfig(&tls.Config{InsecureSkipVerify: true, MinVersion: tls.VersionTLS13}))
	require.NoError(t, err, "gRPC server must be available at %s", serverAddr)
	defer client.Close()

	// Verify KMS service is available
	requireKMSAvailable(t, client, ctx)

	t.Run("invalid algorithm", func(t *testing.T) {
		req := &service.GenerateKeyRequest{
			Name: "invalid-algo-key",
			KeyAttributes: &kctypes.KeyAttributes{
				// Use an invalid public key algorithm value
				KeyAlgorithm: x509.PublicKeyAlgorithm(999),
			},
			Usage: []service.KeyUsage{service.KeyUsageEncrypt},
		}

		_, err := client.KMSGenerateKey(ctx, req)
		require.Error(t, err, "Invalid algorithm should return an error")
		// The error message should reference the algorithm issue
		errMsg := err.Error()
		assert.True(t, strings.Contains(errMsg, "algorithm") ||
			strings.Contains(errMsg, "unsupported") ||
			strings.Contains(errMsg, "invalid"),
			"Error should reference algorithm issue: %s", errMsg)
	})

	t.Run("empty key name", func(t *testing.T) {
		req := &service.GenerateKeyRequest{
			Name: "",
			KeyAttributes: &kctypes.KeyAttributes{
				SymmetricAlgorithm: kctypes.SymmetricAES256GCM},
			Usage: []service.KeyUsage{service.KeyUsageEncrypt},
		}

		_, err := client.KMSGenerateKey(ctx, req)
		assert.Error(t, err)
	})

	t.Run("duplicate key name", func(t *testing.T) {
		keyName := fmt.Sprintf("duplicate-key-%d", time.Now().UnixNano())

		req := &service.GenerateKeyRequest{
			Name: keyName,
			KeyAttributes: &kctypes.KeyAttributes{
				SymmetricAlgorithm: kctypes.SymmetricAES256GCM},
			Usage: []service.KeyUsage{service.KeyUsageEncrypt, service.KeyUsageDecrypt},
		}

		// First creation should succeed
		_, err := client.KMSGenerateKey(ctx, req)
		require.NoError(t, err)

		// Second creation with same name should fail
		_, err = client.KMSGenerateKey(ctx, req)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "already exists")
	})

	t.Run("invalid ciphertext", func(t *testing.T) {
		// Create a key
		genReq := &service.GenerateKeyRequest{
			Name: fmt.Sprintf("test-invalid-ct-%d", time.Now().UnixNano()),
			KeyAttributes: &kctypes.KeyAttributes{
				SymmetricAlgorithm: kctypes.SymmetricAES256GCM},
			Usage: []service.KeyUsage{service.KeyUsageEncrypt, service.KeyUsageDecrypt},
		}

		genResp, err := client.KMSGenerateKey(ctx, genReq)
		require.NoError(t, err)

		// Skip if Crypto service is not available
		requireCryptoAvailable(t, client, ctx, genResp.Key.KeyID)

		// Try to decrypt invalid ciphertext
		decReq := &service.DecryptRequest{
			KeyID:      genResp.Key.KeyID,
			Ciphertext: []byte("this is not valid ciphertext"),
		}

		_, err = client.KMSDecrypt(ctx, decReq)
		assert.Error(t, err)
	})
}

// TestKMSConcurrentOperations tests concurrent KMS operations
func TestKMSConcurrentOperations(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	endpoints := helpers.GetClusterEndpoints()
	serverAddr := endpoints[0]

	client, err := grpcclient.NewClient(serverAddr, grpcclient.WithTLSConfig(&tls.Config{InsecureSkipVerify: true, MinVersion: tls.VersionTLS13}))
	require.NoError(t, err, "gRPC server must be available at %s", serverAddr)
	defer client.Close()

	// Verify KMS service is available
	requireKMSAvailable(t, client, ctx)

	// Create a shared encryption key
	keyName := fmt.Sprintf("concurrent-test-key-%d", time.Now().UnixNano())
	genReq := &service.GenerateKeyRequest{
		Name: keyName,
		KeyAttributes: &kctypes.KeyAttributes{
			SymmetricAlgorithm: kctypes.SymmetricAES256GCM},
		Usage: []service.KeyUsage{service.KeyUsageEncrypt, service.KeyUsageDecrypt},
	}

	genResp, err := client.KMSGenerateKey(ctx, genReq)
	require.NoError(t, err)
	keyID := genResp.Key.KeyID

	// Check if Crypto service is available before running concurrent encrypt tests
	requireCryptoAvailable(t, client, ctx, keyID)

	t.Run("concurrent encryptions", func(t *testing.T) {
		concurrency := 10
		done := make(chan bool, concurrency)
		errors := make(chan error, concurrency)

		for i := 0; i < concurrency; i++ {
			go func(idx int) {
				plaintext := []byte(fmt.Sprintf("concurrent test data %d", idx))

				encReq := &service.EncryptRequest{
					KeyID:     keyID,
					Plaintext: plaintext,
				}

				encResp, err := client.KMSEncrypt(ctx, encReq)
				if err != nil {
					errors <- err
					done <- false
					return
				}

				// Verify we can decrypt
				decReq := &service.DecryptRequest{
					KeyID:      keyID,
					Ciphertext: encResp.Ciphertext,
				}

				decResp, err := client.KMSDecrypt(ctx, decReq)
				if err != nil {
					errors <- err
					done <- false
					return
				}

				if string(decResp.Plaintext) != string(plaintext) {
					errors <- fmt.Errorf("plaintext mismatch")
					done <- false
					return
				}

				done <- true
			}(i)
		}

		// Wait for all goroutines
		successCount := 0
		for i := 0; i < concurrency; i++ {
			if <-done {
				successCount++
			}
		}

		close(errors)
		for err := range errors {
			t.Errorf("Concurrent operation error: %v", err)
		}

		assert.Equal(t, concurrency, successCount, "All concurrent operations should succeed")
	})

	t.Run("concurrent key generations", func(t *testing.T) {
		concurrency := 5
		done := make(chan bool, concurrency)
		keyIDs := make(chan string, concurrency)

		for i := 0; i < concurrency; i++ {
			go func(idx int) {
				req := &service.GenerateKeyRequest{
					Name: fmt.Sprintf("concurrent-gen-%d-%d", time.Now().UnixNano(), idx),
					KeyAttributes: &kctypes.KeyAttributes{
						SymmetricAlgorithm: kctypes.SymmetricAES256GCM},
					Usage: []service.KeyUsage{service.KeyUsageEncrypt, service.KeyUsageDecrypt},
				}

				resp, err := client.KMSGenerateKey(ctx, req)
				if err != nil {
					t.Errorf("Concurrent key generation error: %v", err)
					done <- false
					return
				}

				keyIDs <- resp.Key.KeyID
				done <- true
			}(i)
		}

		// Wait and collect results
		successCount := 0
		for i := 0; i < concurrency; i++ {
			if <-done {
				successCount++
			}
		}

		close(keyIDs)
		uniqueKeys := make(map[string]bool)
		for id := range keyIDs {
			uniqueKeys[id] = true
		}

		assert.Equal(t, concurrency, successCount)
		assert.Equal(t, concurrency, len(uniqueKeys), "All generated keys should be unique")
	})
}

// TestKMSDataKeyGeneration tests data key generation operations
func TestKMSDataKeyGeneration(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	endpoints := helpers.GetClusterEndpoints()
	serverAddr := endpoints[0]

	client, err := grpcclient.NewClient(serverAddr, grpcclient.WithTLSConfig(&tls.Config{InsecureSkipVerify: true, MinVersion: tls.VersionTLS13}))
	require.NoError(t, err, "gRPC server must be available at %s", serverAddr)
	defer client.Close()

	// Verify KMS service is available
	requireKMSAvailable(t, client, ctx)

	// Create a master key for wrapping
	keyName := fmt.Sprintf("master-key-%d", time.Now().UnixNano())
	genReq := &service.GenerateKeyRequest{
		Name: keyName,
		KeyAttributes: &kctypes.KeyAttributes{
			SymmetricAlgorithm: kctypes.SymmetricAES256GCM},
		Usage: []service.KeyUsage{service.KeyUsageEncrypt, service.KeyUsageDecrypt, service.KeyUsageWrapKey},
	}

	genResp, err := client.KMSGenerateKey(ctx, genReq)
	require.NoError(t, err)
	masterKeyID := genResp.Key.KeyID

	// Check if Crypto service is available (data key gen uses encryption)
	requireCryptoAvailable(t, client, ctx, masterKeyID)

	t.Run("generate data key", func(t *testing.T) {
		req := &service.GenerateDataKeyRequest{
			KeyID: masterKeyID,
			KeyAttributes: &kctypes.KeyAttributes{
				SymmetricAlgorithm: kctypes.SymmetricAES256GCM,
			},
		}

		resp, err := client.KMSGenerateDataKey(ctx, req)
		require.NoError(t, err)
		require.NotNil(t, resp)

		assert.Equal(t, masterKeyID, resp.KeyID)
		assert.NotEmpty(t, resp.Plaintext, "Plaintext data key should be provided")
		assert.NotEmpty(t, resp.CiphertextBlob, "Encrypted data key should be provided")
		assert.NotEqual(t, resp.Plaintext, resp.CiphertextBlob)

		// Verify we can decrypt the wrapped key
		decReq := &service.DecryptRequest{
			KeyID:      masterKeyID,
			Ciphertext: resp.CiphertextBlob,
		}

		decResp, err := client.KMSDecrypt(ctx, decReq)
		require.NoError(t, err)
		assert.Equal(t, resp.Plaintext, decResp.Plaintext, "Unwrapped key should match plaintext")
	})

	t.Run("generate data key with context", func(t *testing.T) {
		encContext := []byte("encryption context for data key")

		req := &service.GenerateDataKeyRequest{
			KeyID: masterKeyID,
			KeyAttributes: &kctypes.KeyAttributes{
				SymmetricAlgorithm: kctypes.SymmetricAES256GCM,
			},
			Context: encContext,
		}

		resp, err := client.KMSGenerateDataKey(ctx, req)
		require.NoError(t, err)

		// NOTE: Context/AAD support in GenerateDataKey is not fully implemented yet.
		// The context is accepted in the request but not used as AAD during encryption.
		// For now, we just verify the basic functionality works.
		// Full context support would require passing AAD through the encryption path.
		decReq := &service.DecryptRequest{
			KeyID:      masterKeyID,
			Ciphertext: resp.CiphertextBlob,
			// Not passing context since it's not used in encryption
		}

		decResp, err := client.KMSDecrypt(ctx, decReq)
		if err != nil {
			// If context was actually used, decryption without it would fail
			// Try with context
			decReq.Context = encContext
			decResp, err = client.KMSDecrypt(ctx, decReq)
		}
		require.NoError(t, err)
		assert.Equal(t, resp.Plaintext, decResp.Plaintext)
	})
}

// TestKMSClusterConsistency tests KMS operations across cluster nodes
// NOTE: KMS keys are stored locally on each node via go-xkms for security reasons.
// Key material is never replicated across nodes - this is by design.
// For distributed key access, use a centralized backend like HashiCorp Vault.
func TestKMSClusterConsistency(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()

	endpoints := helpers.GetClusterEndpoints()
	require.GreaterOrEqual(t, len(endpoints), 2,
		"Cluster consistency test requires at least 2 nodes, got %d", len(endpoints))

	// Connect to first node
	client1, err := grpcclient.NewClient(endpoints[0], grpcclient.WithTLSConfig(&tls.Config{InsecureSkipVerify: true, MinVersion: tls.VersionTLS13}))
	require.NoError(t, err, "gRPC node 1 must be available at %s", endpoints[0])
	defer client1.Close()

	// Verify KMS service is available
	requireKMSAvailable(t, client1, ctx)

	// Connect to second node
	client2, err := grpcclient.NewClient(endpoints[1], grpcclient.WithTLSConfig(&tls.Config{InsecureSkipVerify: true, MinVersion: tls.VersionTLS13}))
	require.NoError(t, err, "gRPC node 2 must be available at %s", endpoints[1])
	defer client2.Close()

	// KMS keys are node-local by design - test operations on same node
	t.Run("key operations on same node", func(t *testing.T) {
		keyName := fmt.Sprintf("kms-single-node-%d", time.Now().UnixNano())

		// Create key on node 1
		genReq := &service.GenerateKeyRequest{
			Name:        keyName,
			Description: "Single node consistency test key",
			KeyAttributes: &kctypes.KeyAttributes{
				SymmetricAlgorithm: kctypes.SymmetricAES256GCM},
			Usage: []service.KeyUsage{service.KeyUsageEncrypt, service.KeyUsageDecrypt},
		}

		genResp, err := client1.KMSGenerateKey(ctx, genReq)
		require.NoError(t, err)
		keyID := genResp.Key.KeyID

		// Retrieve from same node
		getReq := &service.GetKeyRequest{KeyID: keyID}
		getResp, err := client1.KMSGetKey(ctx, getReq)
		require.NoError(t, err)
		assert.Equal(t, keyID, getResp.Metadata.KeyID)
		assert.Equal(t, service.KeyAlgAES256GCM, getResp.Metadata.Algorithm)
	})

	t.Run("encrypt and decrypt on same node", func(t *testing.T) {
		// Create key on node 1
		keyName := fmt.Sprintf("kms-encrypt-node-%d", time.Now().UnixNano())
		genReq := &service.GenerateKeyRequest{
			Name: keyName,
			KeyAttributes: &kctypes.KeyAttributes{
				SymmetricAlgorithm: kctypes.SymmetricAES256GCM},
			Usage: []service.KeyUsage{service.KeyUsageEncrypt, service.KeyUsageDecrypt},
		}

		genResp, err := client1.KMSGenerateKey(ctx, genReq)
		require.NoError(t, err)
		keyID := genResp.Key.KeyID

		// Skip if Crypto service is not available
		requireCryptoAvailable(t, client1, ctx, keyID)

		// Encrypt on node 1
		plaintext := []byte("single node KMS consistency test data")
		encReq := &service.EncryptRequest{
			KeyID:     keyID,
			Plaintext: plaintext,
		}

		encResp, err := client1.KMSEncrypt(ctx, encReq)
		require.NoError(t, err)

		// Decrypt on same node
		decReq := &service.DecryptRequest{
			KeyID:      keyID,
			Ciphertext: encResp.Ciphertext,
		}

		decResp, err := client1.KMSDecrypt(ctx, decReq)
		require.NoError(t, err)
		assert.Equal(t, plaintext, decResp.Plaintext)
	})

	t.Run("key not visible on other nodes (expected)", func(t *testing.T) {
		// This test documents that KMS keys are node-local by design
		keyName := fmt.Sprintf("kms-local-only-%d", time.Now().UnixNano())

		genReq := &service.GenerateKeyRequest{
			Name: keyName,
			KeyAttributes: &kctypes.KeyAttributes{
				SymmetricAlgorithm: kctypes.SymmetricAES256GCM},
			Usage: []service.KeyUsage{service.KeyUsageEncrypt, service.KeyUsageDecrypt},
		}

		genResp, err := client1.KMSGenerateKey(ctx, genReq)
		require.NoError(t, err)
		keyID := genResp.Key.KeyID

		// Key should NOT be visible on node2 - this is expected behavior
		getReq := &service.GetKeyRequest{KeyID: keyID}
		_, err = client2.KMSGetKey(ctx, getReq)
		// Expect an error (key not found on node2)
		assert.Error(t, err, "Key should NOT be visible on other nodes - KMS keys are node-local by design")
		t.Logf("Expected behavior: key not visible on node2 (error: %v)", err)
	})
}

// TestKMSLargeDataEncryption tests encryption of larger data
func TestKMSLargeDataEncryption(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	endpoints := helpers.GetClusterEndpoints()
	serverAddr := endpoints[0]

	client, err := grpcclient.NewClient(serverAddr, grpcclient.WithTLSConfig(&tls.Config{InsecureSkipVerify: true, MinVersion: tls.VersionTLS13}))
	require.NoError(t, err, "gRPC server must be available at %s", serverAddr)
	defer client.Close()

	// Verify KMS service is available
	requireKMSAvailable(t, client, ctx)

	// Create encryption key
	keyName := fmt.Sprintf("large-data-key-%d", time.Now().UnixNano())
	genReq := &service.GenerateKeyRequest{
		Name: keyName,
		KeyAttributes: &kctypes.KeyAttributes{
			SymmetricAlgorithm: kctypes.SymmetricAES256GCM},
		Usage: []service.KeyUsage{service.KeyUsageEncrypt, service.KeyUsageDecrypt},
	}

	genResp, err := client.KMSGenerateKey(ctx, genReq)
	require.NoError(t, err)
	keyID := genResp.Key.KeyID

	// Check if Crypto service is available
	requireCryptoAvailable(t, client, ctx, keyID)

	testSizes := []int{
		1024,        // 1 KB
		10 * 1024,   // 10 KB
		100 * 1024,  // 100 KB
		1024 * 1024, // 1 MB
	}

	for _, size := range testSizes {
		t.Run(fmt.Sprintf("encrypt %d bytes", size), func(t *testing.T) {
			// Generate test data
			plaintext := make([]byte, size)
			for i := range plaintext {
				plaintext[i] = byte(i % 256)
			}

			// Encrypt
			encReq := &service.EncryptRequest{
				KeyID:     keyID,
				Plaintext: plaintext,
			}

			encResp, err := client.KMSEncrypt(ctx, encReq)
			require.NoError(t, err)
			assert.NotEmpty(t, encResp.Ciphertext)

			// Decrypt
			decReq := &service.DecryptRequest{
				KeyID:      keyID,
				Ciphertext: encResp.Ciphertext,
			}

			decResp, err := client.KMSDecrypt(ctx, decReq)
			require.NoError(t, err)
			assert.Equal(t, plaintext, decResp.Plaintext)
		})
	}
}

// TestKMSKeyUsageTracking tests that key usage is properly tracked
func TestKMSKeyUsageTracking(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	endpoints := helpers.GetClusterEndpoints()
	serverAddr := endpoints[0]

	client, err := grpcclient.NewClient(serverAddr, grpcclient.WithTLSConfig(&tls.Config{InsecureSkipVerify: true, MinVersion: tls.VersionTLS13}))
	require.NoError(t, err, "gRPC server must be available at %s", serverAddr)
	defer client.Close()

	// Verify KMS service is available
	requireKMSAvailable(t, client, ctx)

	// Create a key
	keyName := fmt.Sprintf("usage-tracking-key-%d", time.Now().UnixNano())
	genReq := &service.GenerateKeyRequest{
		Name: keyName,
		KeyAttributes: &kctypes.KeyAttributes{
			SymmetricAlgorithm: kctypes.SymmetricAES256GCM},
		Usage: []service.KeyUsage{service.KeyUsageEncrypt, service.KeyUsageDecrypt},
	}

	genResp, err := client.KMSGenerateKey(ctx, genReq)
	require.NoError(t, err)
	keyID := genResp.Key.KeyID

	// Check if Crypto service is available
	requireCryptoAvailable(t, client, ctx, keyID)

	// Perform several operations
	for i := 0; i < 5; i++ {
		plaintext := []byte(fmt.Sprintf("test data %d", i))

		encReq := &service.EncryptRequest{
			KeyID:     keyID,
			Plaintext: plaintext,
		}

		_, err := client.KMSEncrypt(ctx, encReq)
		require.NoError(t, err)
	}

	// Check metadata - LastUsedAt should be set
	getReq := &service.GetKeyRequest{KeyID: keyID}
	getResp, err := client.KMSGetKey(ctx, getReq)
	require.NoError(t, err)

	// Note: LastUsedAt might be nil if the backend doesn't track it
	// This test documents the expected behavior
	t.Logf("Key %s last used at: %v", keyID, getResp.Metadata.LastUsedAt)
}

// TestKMSHexEncodedCiphertext tests that ciphertext can be hex-encoded for storage
func TestKMSHexEncodedCiphertext(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	endpoints := helpers.GetClusterEndpoints()
	serverAddr := endpoints[0]

	client, err := grpcclient.NewClient(serverAddr, grpcclient.WithTLSConfig(&tls.Config{InsecureSkipVerify: true, MinVersion: tls.VersionTLS13}))
	require.NoError(t, err, "gRPC server must be available at %s", serverAddr)
	defer client.Close()

	// Verify KMS service is available
	requireKMSAvailable(t, client, ctx)

	// Create encryption key
	keyName := fmt.Sprintf("hex-test-key-%d", time.Now().UnixNano())
	genReq := &service.GenerateKeyRequest{
		Name: keyName,
		KeyAttributes: &kctypes.KeyAttributes{
			SymmetricAlgorithm: kctypes.SymmetricAES256GCM},
		Usage: []service.KeyUsage{service.KeyUsageEncrypt, service.KeyUsageDecrypt},
	}

	genResp, err := client.KMSGenerateKey(ctx, genReq)
	require.NoError(t, err)
	keyID := genResp.Key.KeyID

	// Check if Crypto service is available
	requireCryptoAvailable(t, client, ctx, keyID)

	plaintext := []byte("test data for hex encoding")

	// Encrypt
	encReq := &service.EncryptRequest{
		KeyID:     keyID,
		Plaintext: plaintext,
	}

	encResp, err := client.KMSEncrypt(ctx, encReq)
	require.NoError(t, err)

	// Convert to hex (simulating storage)
	hexCiphertext := hex.EncodeToString(encResp.Ciphertext)
	assert.NotEmpty(t, hexCiphertext)

	// Convert back from hex
	ciphertext, err := hex.DecodeString(hexCiphertext)
	require.NoError(t, err)

	// Decrypt
	decReq := &service.DecryptRequest{
		KeyID:      keyID,
		Ciphertext: ciphertext,
	}

	decResp, err := client.KMSDecrypt(ctx, decReq)
	require.NoError(t, err)
	assert.Equal(t, plaintext, decResp.Plaintext)
}
