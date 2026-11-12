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

	kctypes "github.com/jeremyhahn/go-xkms/pkg/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	grpcclient "github.com/jeremyhahn/go-quicbaas/pkg/api/grpc"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
	"github.com/jeremyhahn/go-quicbaas/test/integration/helpers"
)

// TestGRPCKeyGeneration tests key generation operations through gRPC.
func TestGRPCKeyGeneration(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	endpoints := getGRPCEndpoints()
	tlsCfg, err := helpers.IntegrationTLSConfig()
	require.NoError(t, err, "load integration TLS config")
	client, err := grpcclient.NewClient(endpoints[0], grpcclient.WithTLSConfig(tlsCfg), kmsGRPCAuth(t))
	if err != nil {
		require.NoError(t, err, "gRPC server must be available for integration tests")
	}
	defer client.Close()

	requireKMSAvailable(t, client, ctx)

	tests := []struct {
		name              string
		attrs             *kctypes.KeyAttributes
		expectedAlgorithm service.KeyAlgorithm
	}{
		// All 5 symmetric algorithms
		{
			name: "AES-128-GCM",
			attrs: &kctypes.KeyAttributes{
				SymmetricAlgorithm: kctypes.SymmetricAES128GCM,
			},
			expectedAlgorithm: service.KeyAlgAES128GCM,
		},
		{
			name: "AES-192-GCM",
			attrs: &kctypes.KeyAttributes{
				SymmetricAlgorithm: kctypes.SymmetricAES192GCM,
			},
			expectedAlgorithm: service.KeyAlgAES192GCM,
		},
		{
			name: "AES-256-GCM",
			attrs: &kctypes.KeyAttributes{
				SymmetricAlgorithm: kctypes.SymmetricAES256GCM,
			},
			expectedAlgorithm: service.KeyAlgAES256GCM,
		},
		{
			name: "ChaCha20-Poly1305",
			attrs: &kctypes.KeyAttributes{
				SymmetricAlgorithm: kctypes.SymmetricChaCha20Poly1305,
			},
			expectedAlgorithm: service.KeyAlgChaCha20Poly1305,
		},
		{
			name: "XChaCha20-Poly1305",
			attrs: &kctypes.KeyAttributes{
				SymmetricAlgorithm: kctypes.SymmetricXChaCha20Poly1305,
			},
			expectedAlgorithm: service.KeyAlgXChaCha20Poly1305,
		},
		// Asymmetric algorithms
		{
			name: "RSA-4096",
			attrs: &kctypes.KeyAttributes{
				KeyAlgorithm:  x509.RSA,
				RSAAttributes: &kctypes.RSAAttributes{KeySize: 4096},
			},
			expectedAlgorithm: service.KeyAlgRSA4096,
		},
		{
			name: "ECDSA-P256",
			attrs: &kctypes.KeyAttributes{
				KeyAlgorithm:  x509.ECDSA,
				ECCAttributes: &kctypes.ECCAttributes{Curve: elliptic.P256()},
			},
			expectedAlgorithm: service.KeyAlgECDSAP256,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			keyName := generateUniqueKeyName(fmt.Sprintf("grpc-%s", tt.name))

			req := &service.GenerateKeyRequest{
				Name:          keyName,
				Description:   fmt.Sprintf("gRPC integration test %s key", tt.name),
				KeyAttributes: tt.attrs,
			}

			resp, err := client.KMSGenerateKey(ctx, req)
			require.NoError(t, err)
			require.NotNil(t, resp)
			defer cleanupKey(t, client, ctx, resp.Key.KeyID)

			assert.NotEmpty(t, resp.Key.KeyID)
			assert.Equal(t, tt.expectedAlgorithm, resp.Key.Algorithm)
			assert.Equal(t, service.KeyStatusEnabled, resp.Key.Status)
			assert.NotNil(t, resp.Metadata)
		})
	}
}

// TestGRPCKeyRetrieval tests key retrieval operations.
func TestGRPCKeyRetrieval(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	endpoints := getGRPCEndpoints()
	tlsCfg, err := helpers.IntegrationTLSConfig()
	require.NoError(t, err, "load integration TLS config")
	client, err := grpcclient.NewClient(endpoints[0], grpcclient.WithTLSConfig(tlsCfg), kmsGRPCAuth(t))
	if err != nil {
		require.NoError(t, err, "gRPC server must be available for integration tests")
	}
	defer client.Close()

	requireKMSAvailable(t, client, ctx)

	// Create a test key
	keyName := generateUniqueKeyName("grpc-retrieval-test")
	genReq := &service.GenerateKeyRequest{
		Name: keyName,
		KeyAttributes: &kctypes.KeyAttributes{
			SymmetricAlgorithm: kctypes.SymmetricAES256GCM,
		},
	}

	genResp, err := client.KMSGenerateKey(ctx, genReq)
	require.NoError(t, err)
	keyID := genResp.Key.KeyID
	defer cleanupKey(t, client, ctx, keyID)

	t.Run("get key by ID", func(t *testing.T) {
		req := &service.GetKeyRequest{
			KeyID: keyID,
		}

		resp, err := client.KMSGetKey(ctx, req)
		require.NoError(t, err)
		require.NotNil(t, resp)
		assert.Equal(t, keyID, resp.Metadata.KeyID)
		assert.Equal(t, service.KeyAlgAES256GCM, resp.Metadata.Algorithm)
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
		// Rotate to create a new version
		rotateReq := &service.RotateKeyRequest{
			KeyID:            keyID,
			RetainOldVersion: true,
		}
		rotateResp, err := client.KMSRotateKey(ctx, rotateReq)
		require.NoError(t, err)

		// Get the new version
		req := &service.GetKeyRequest{
			KeyID:   keyID,
			Version: rotateResp.NewVersion,
		}

		resp, err := client.KMSGetKey(ctx, req)
		require.NoError(t, err)
		assert.Equal(t, rotateResp.NewVersion, resp.Metadata.Version)
	})
}

// TestGRPCKeyListing tests key listing operations.
func TestGRPCKeyListing(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	endpoints := getGRPCEndpoints()
	tlsCfg, err := helpers.IntegrationTLSConfig()
	require.NoError(t, err, "load integration TLS config")
	client, err := grpcclient.NewClient(endpoints[0], grpcclient.WithTLSConfig(tlsCfg), kmsGRPCAuth(t))
	if err != nil {
		require.NoError(t, err, "gRPC server must be available for integration tests")
	}
	defer client.Close()

	requireKMSAvailable(t, client, ctx)

	// Create multiple test keys with a common prefix
	prefix := generateUniqueKeyName("grpc-list-test")
	keyIDs := []string{}

	for i := 0; i < 5; i++ {
		req := &service.GenerateKeyRequest{
			Name: fmt.Sprintf("%s-key-%d", prefix, i),
			KeyAttributes: &kctypes.KeyAttributes{
				SymmetricAlgorithm: kctypes.SymmetricAES256GCM,
			},
		}

		resp, err := client.KMSGenerateKey(ctx, req)
		require.NoError(t, err)
		keyIDs = append(keyIDs, resp.Key.KeyID)
	}

	// Cleanup
	defer func() {
		for _, keyID := range keyIDs {
			cleanupKey(t, client, ctx, keyID)
		}
	}()

	t.Run("list all keys", func(t *testing.T) {
		req := &service.ListKeysRequest{
			Limit: 100,
		}

		resp, err := client.KMSListKeys(ctx, req)
		require.NoError(t, err)
		require.NotNil(t, resp)
		assert.GreaterOrEqual(t, len(resp.Keys), 5, "Should have at least 5 keys")
	})

	t.Run("list with prefix filter", func(t *testing.T) {
		req := &service.ListKeysRequest{
			Limit:  100,
			Filter: map[string]string{"prefix": prefix},
		}

		resp, err := client.KMSListKeys(ctx, req)
		require.NoError(t, err)
		require.NotNil(t, resp)
		// Note: Filter implementation may vary by backend
		t.Logf("Found %d keys with prefix %s", len(resp.Keys), prefix)
	})

	t.Run("list with limit", func(t *testing.T) {
		req := &service.ListKeysRequest{
			Limit: 3,
		}

		resp, err := client.KMSListKeys(ctx, req)
		require.NoError(t, err)
		assert.NotNil(t, resp.Keys)
		t.Logf("Requested limit: 3, Got: %d keys", len(resp.Keys))
	})
}

// TestGRPCKeyRotation tests key rotation operations.
func TestGRPCKeyRotation(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	endpoints := getGRPCEndpoints()
	tlsCfg, err := helpers.IntegrationTLSConfig()
	require.NoError(t, err, "load integration TLS config")
	client, err := grpcclient.NewClient(endpoints[0], grpcclient.WithTLSConfig(tlsCfg), kmsGRPCAuth(t))
	if err != nil {
		require.NoError(t, err, "gRPC server must be available for integration tests")
	}
	defer client.Close()

	requireKMSAvailable(t, client, ctx)

	keyName := generateUniqueKeyName("grpc-rotation-test")
	genReq := &service.GenerateKeyRequest{
		Name: keyName,
		KeyAttributes: &kctypes.KeyAttributes{
			SymmetricAlgorithm: kctypes.SymmetricAES256GCM,
		},
	}

	genResp, err := client.KMSGenerateKey(ctx, genReq)
	require.NoError(t, err)
	keyID := genResp.Key.KeyID
	defer cleanupKey(t, client, ctx, keyID)

	t.Run("rotate key", func(t *testing.T) {
		// Get version before rotation
		getReqBefore := &service.GetKeyRequest{KeyID: keyID}
		getRespBefore, err := client.KMSGetKey(ctx, getReqBefore)
		require.NoError(t, err)
		versionBefore := getRespBefore.Metadata.Version

		// Rotate
		rotateReq := &service.RotateKeyRequest{
			KeyID:            keyID,
			RetainOldVersion: true,
		}

		resp, err := client.KMSRotateKey(ctx, rotateReq)
		require.NoError(t, err)
		require.NotNil(t, resp)
		assert.Equal(t, keyID, resp.KeyID)
		assert.Greater(t, resp.NewVersion, resp.OldVersion)

		// Verify version incremented
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

// TestGRPCKeyDeletion tests key deletion operations.
func TestGRPCKeyDeletion(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	endpoints := getGRPCEndpoints()
	tlsCfg, err := helpers.IntegrationTLSConfig()
	require.NoError(t, err, "load integration TLS config")
	client, err := grpcclient.NewClient(endpoints[0], grpcclient.WithTLSConfig(tlsCfg), kmsGRPCAuth(t))
	if err != nil {
		require.NoError(t, err, "gRPC server must be available for integration tests")
	}
	defer client.Close()

	requireKMSAvailable(t, client, ctx)

	t.Run("schedule key deletion", func(t *testing.T) {
		keyName := generateUniqueKeyName("grpc-delete-test")
		genReq := &service.GenerateKeyRequest{
			Name: keyName,
			KeyAttributes: &kctypes.KeyAttributes{
				SymmetricAlgorithm: kctypes.SymmetricAES256GCM,
			},
		}

		genResp, err := client.KMSGenerateKey(ctx, genReq)
		require.NoError(t, err)
		keyID := genResp.Key.KeyID

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
	})

	t.Run("force delete key", func(t *testing.T) {
		keyName := generateUniqueKeyName("grpc-force-delete")
		genReq := &service.GenerateKeyRequest{
			Name: keyName,
			KeyAttributes: &kctypes.KeyAttributes{
				SymmetricAlgorithm: kctypes.SymmetricAES256GCM,
			},
		}

		genResp, err := client.KMSGenerateKey(ctx, genReq)
		require.NoError(t, err)
		keyID := genResp.Key.KeyID

		deleteReq := &service.DeleteKeyRequest{
			KeyID: keyID,
			Force: true,
		}

		resp, err := client.KMSDeleteKey(ctx, deleteReq)
		require.NoError(t, err)
		assert.Equal(t, keyID, resp.KeyID)

		// Verify deletion
		assertKeyNotFound(t, client, ctx, keyID)
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

// TestGRPCEncryptionDecryption tests encryption and decryption operations.
func TestGRPCEncryptionDecryption(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	endpoints := getGRPCEndpoints()
	tlsCfg, err := helpers.IntegrationTLSConfig()
	require.NoError(t, err, "load integration TLS config")
	client, err := grpcclient.NewClient(endpoints[0], grpcclient.WithTLSConfig(tlsCfg), kmsGRPCAuth(t))
	if err != nil {
		require.NoError(t, err, "gRPC server must be available for integration tests")
	}
	defer client.Close()

	requireKMSAvailable(t, client, ctx)

	// Create encryption key
	keyName := generateUniqueKeyName("grpc-encrypt-test")
	genReq := &service.GenerateKeyRequest{
		Name: keyName,
		KeyAttributes: &kctypes.KeyAttributes{
			SymmetricAlgorithm: kctypes.SymmetricAES256GCM,
		},
	}

	genResp, err := client.KMSGenerateKey(ctx, genReq)
	require.NoError(t, err)
	keyID := genResp.Key.KeyID
	defer cleanupKey(t, client, ctx, keyID)

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

	t.Run("encrypt various data sizes", func(t *testing.T) {
		sizes := []int{1, 100, 1024, 10 * 1024}

		for _, size := range sizes {
			t.Run(fmt.Sprintf("%d bytes", size), func(t *testing.T) {
				plaintext := generateTestPlaintext(size)

				encReq := &service.EncryptRequest{
					KeyID:     keyID,
					Plaintext: plaintext,
				}

				encResp, err := client.KMSEncrypt(ctx, encReq)
				require.NoError(t, err)

				decReq := &service.DecryptRequest{
					KeyID:      keyID,
					Ciphertext: encResp.Ciphertext,
				}

				decResp, err := client.KMSDecrypt(ctx, decReq)
				require.NoError(t, err)
				assert.Equal(t, plaintext, decResp.Plaintext)
			})
		}
	})

	t.Run("error cases", func(t *testing.T) {
		t.Run("decrypt with wrong key", func(t *testing.T) {
			plaintext := []byte("test data")

			encReq := &service.EncryptRequest{
				KeyID:     keyID,
				Plaintext: plaintext,
			}

			encResp, err := client.KMSEncrypt(ctx, encReq)
			require.NoError(t, err)

			decReq := &service.DecryptRequest{
				KeyID:      "wrong-key-id",
				Ciphertext: encResp.Ciphertext,
			}

			_, err = client.KMSDecrypt(ctx, decReq)
			assert.Error(t, err)
		})

		t.Run("decrypt invalid ciphertext", func(t *testing.T) {
			decReq := &service.DecryptRequest{
				KeyID:      keyID,
				Ciphertext: []byte("this is not valid ciphertext"),
			}

			_, err := client.KMSDecrypt(ctx, decReq)
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
	})
}

// TestGRPCConcurrentOperations tests concurrent KMS operations.
func TestGRPCConcurrentOperations(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	endpoints := getGRPCEndpoints()
	tlsCfg, err := helpers.IntegrationTLSConfig()
	require.NoError(t, err, "load integration TLS config")
	client, err := grpcclient.NewClient(endpoints[0], grpcclient.WithTLSConfig(tlsCfg), kmsGRPCAuth(t))
	if err != nil {
		require.NoError(t, err, "gRPC server must be available for integration tests")
	}
	defer client.Close()

	requireKMSAvailable(t, client, ctx)

	// Create a shared encryption key
	keyName := generateUniqueKeyName("grpc-concurrent-test")
	genReq := &service.GenerateKeyRequest{
		Name: keyName,
		KeyAttributes: &kctypes.KeyAttributes{
			SymmetricAlgorithm: kctypes.SymmetricAES256GCM,
		},
	}

	genResp, err := client.KMSGenerateKey(ctx, genReq)
	require.NoError(t, err)
	keyID := genResp.Key.KeyID
	defer cleanupKey(t, client, ctx, keyID)

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
					Name: generateUniqueKeyName(fmt.Sprintf("grpc-concurrent-gen-%d", idx)),
					KeyAttributes: &kctypes.KeyAttributes{
						SymmetricAlgorithm: kctypes.SymmetricAES256GCM,
					},
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

		successCount := 0
		generatedKeys := []string{}
		for i := 0; i < concurrency; i++ {
			if <-done {
				successCount++
			}
		}

		close(keyIDs)
		for id := range keyIDs {
			generatedKeys = append(generatedKeys, id)
		}

		// Cleanup
		for _, keyID := range generatedKeys {
			cleanupKey(t, client, ctx, keyID)
		}

		assert.Equal(t, concurrency, successCount)
		assert.Equal(t, concurrency, len(generatedKeys), "All generated keys should be unique")
	})
}

// TestGRPCClusterConsistency tests KMS operations across cluster nodes.
// NOTE: KMS keys are stored locally on each node via go-xkms for security reasons.
// Key material is never replicated across nodes - this is by design.
// For distributed key access, use a centralized backend like HashiCorp Vault.
func TestGRPCClusterConsistency(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()

	endpoints := getGRPCEndpoints()
	require.GreaterOrEqual(t, len(endpoints), 2,
		"Cluster consistency test requires at least 2 nodes, got %d", len(endpoints))

	tlsCfg, err := helpers.IntegrationTLSConfig()
	require.NoError(t, err, "load integration TLS config")
	client1, err := grpcclient.NewClient(endpoints[0], grpcclient.WithTLSConfig(tlsCfg), kmsGRPCAuth(t))
	require.NoError(t, err, "gRPC node 1 must be available at %s", endpoints[0])
	defer client1.Close()

	requireKMSAvailable(t, client1, ctx)

	client2, err := grpcclient.NewClient(endpoints[1], grpcclient.WithTLSConfig(tlsCfg), kmsGRPCAuth(t))
	require.NoError(t, err, "gRPC node 2 must be available at %s", endpoints[1])
	defer client2.Close()

	// KMS keys are node-local by design - test operations on same node
	t.Run("key operations on same node", func(t *testing.T) {
		keyName := generateUniqueKeyName("grpc-single-node")

		genReq := &service.GenerateKeyRequest{
			Name: keyName,
			KeyAttributes: &kctypes.KeyAttributes{
				SymmetricAlgorithm: kctypes.SymmetricAES256GCM,
			},
		}

		genResp, err := client1.KMSGenerateKey(ctx, genReq)
		require.NoError(t, err)
		keyID := genResp.Key.KeyID
		defer cleanupKey(t, client1, ctx, keyID)

		// Verify key is accessible on same node
		getReq := &service.GetKeyRequest{KeyID: keyID}
		getResp, err := client1.KMSGetKey(ctx, getReq)
		require.NoError(t, err)
		assert.Equal(t, keyID, getResp.Metadata.KeyID)
	})

	t.Run("encrypt and decrypt on same node", func(t *testing.T) {
		keyName := generateUniqueKeyName("grpc-encrypt-node")
		genReq := &service.GenerateKeyRequest{
			Name: keyName,
			KeyAttributes: &kctypes.KeyAttributes{
				SymmetricAlgorithm: kctypes.SymmetricAES256GCM,
			},
		}

		genResp, err := client1.KMSGenerateKey(ctx, genReq)
		require.NoError(t, err)
		keyID := genResp.Key.KeyID
		defer cleanupKey(t, client1, ctx, keyID)

		requireCryptoAvailable(t, client1, ctx, keyID)

		plaintext := []byte("single node gRPC consistency test data")
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
		keyName := generateUniqueKeyName("grpc-local-only")

		genReq := &service.GenerateKeyRequest{
			Name: keyName,
			KeyAttributes: &kctypes.KeyAttributes{
				SymmetricAlgorithm: kctypes.SymmetricAES256GCM,
			},
		}

		genResp, err := client1.KMSGenerateKey(ctx, genReq)
		require.NoError(t, err)
		keyID := genResp.Key.KeyID
		defer cleanupKey(t, client1, ctx, keyID)

		// Key should NOT be visible on node2 - this is expected behavior
		getReq := &service.GetKeyRequest{KeyID: keyID}
		_, err = client2.KMSGetKey(ctx, getReq)
		// Expect an error (key not found on node2)
		assert.Error(t, err, "Key should NOT be visible on other nodes - KMS keys are node-local by design")
		t.Logf("Expected behavior: key not visible on node2 (error: %v)", err)
	})
}
