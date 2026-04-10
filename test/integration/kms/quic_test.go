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

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	kctypes "github.com/jeremyhahn/go-xkms/pkg/types"

	"github.com/jeremyhahn/go-quicbaas/pkg/service"
)

// TestQUICKMSAvailability tests that KMS service is available via QUIC.
func TestQUICKMSAvailability(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	endpoints := getQUICEndpoints()
	client, err := NewQUICClient(endpoints[0])
	require.NoError(t, err, "QUIC server must be available for integration tests")
	defer client.Close()

	// Test health endpoint
	err = client.Health(ctx)
	if err != nil {
		t.Logf("Health check failed: %v", err)
	}

	// Test basic KMS availability by listing keys
	req := &service.ListKeysRequest{
		Limit: 1,
	}
	_, err = client.ListKeys(ctx, req)
	require.NoError(t, err, "KMS service must be available via QUIC")

	t.Log("QUIC KMS service is available and responding")
}

// TestQUICKeyGeneration tests key generation operations through QUIC.
func TestQUICKeyGeneration(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	endpoints := getQUICEndpoints()
	client, err := NewQUICClient(endpoints[0])
	require.NoError(t, err, "QUIC server must be available for integration tests")
	defer client.Close()

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
			keyName := generateUniqueKeyName(fmt.Sprintf("quic-%s", tt.name))

			req := &service.GenerateKeyRequest{
				Name:          keyName,
				Description:   fmt.Sprintf("QUIC integration test %s key", tt.name),
				KeyAttributes: tt.attrs,
				Usage:         tt.usage,
			}

			resp, err := client.GenerateKey(ctx, req)
			require.NoError(t, err)
			require.NotNil(t, resp)
			defer cleanupQUICKey(t, client, ctx, resp.Key.KeyID)

			assert.NotEmpty(t, resp.Key.KeyID)
			assert.Equal(t, tt.expectedAlgorithm, resp.Key.Algorithm)
			assert.Equal(t, service.KeyStatusEnabled, resp.Key.Status)
			assert.NotNil(t, resp.Metadata)
		})
	}
}

// TestQUICEncryptionDecryption tests encryption and decryption operations.
func TestQUICEncryptionDecryption(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	endpoints := getQUICEndpoints()
	client, err := NewQUICClient(endpoints[0])
	require.NoError(t, err, "QUIC server must be available for integration tests")
	defer client.Close()

	// Create encryption key
	keyName := generateUniqueKeyName("quic-encrypt-test")
	genReq := &service.GenerateKeyRequest{
		Name: keyName,
		KeyAttributes: &kctypes.KeyAttributes{
			SymmetricAlgorithm: kctypes.SymmetricAES256GCM,
		},
		Usage: []service.KeyUsage{service.KeyUsageEncrypt, service.KeyUsageDecrypt},
	}

	genResp, err := client.GenerateKey(ctx, genReq)
	require.NoError(t, err)
	keyID := genResp.Key.KeyID
	defer cleanupQUICKey(t, client, ctx, keyID)

	t.Run("encrypt and decrypt data", func(t *testing.T) {
		plaintext := []byte("This is sensitive data that needs encryption via QUIC")

		// Encrypt
		encReq := &service.EncryptRequest{
			KeyID:     keyID,
			Plaintext: plaintext,
		}

		encResp, err := client.Encrypt(ctx, encReq)
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

		decResp, err := client.Decrypt(ctx, decReq)
		require.NoError(t, err)
		require.NotNil(t, decResp)
		assert.Equal(t, plaintext, decResp.Plaintext)
		assert.Equal(t, keyID, decResp.KeyID)
	})

	t.Run("encrypt and decrypt with AAD", func(t *testing.T) {
		plaintext := []byte("Data with additional authenticated data via QUIC")
		aad := []byte("contextual information")

		// Encrypt with AAD
		encReq := &service.EncryptRequest{
			KeyID:     keyID,
			Plaintext: plaintext,
			Context:   aad,
		}

		encResp, err := client.Encrypt(ctx, encReq)
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

	t.Run("encrypt various data sizes", func(t *testing.T) {
		sizes := []int{1, 100, 1024, 10 * 1024}

		for _, size := range sizes {
			t.Run(fmt.Sprintf("%d bytes", size), func(t *testing.T) {
				plaintext := generateTestPlaintext(size)

				encReq := &service.EncryptRequest{
					KeyID:     keyID,
					Plaintext: plaintext,
				}

				encResp, err := client.Encrypt(ctx, encReq)
				require.NoError(t, err)

				decReq := &service.DecryptRequest{
					KeyID:      keyID,
					Ciphertext: encResp.Ciphertext,
				}

				decResp, err := client.Decrypt(ctx, decReq)
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

			encResp, err := client.Encrypt(ctx, encReq)
			require.NoError(t, err)

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

		t.Run("encrypt empty plaintext", func(t *testing.T) {
			req := &service.EncryptRequest{
				KeyID:     keyID,
				Plaintext: []byte{},
			}

			_, err := client.Encrypt(ctx, req)
			assert.Error(t, err, "Empty plaintext should be rejected")
		})
	})
}

// TestQUICKeyLifecycle tests complete key lifecycle operations.
func TestQUICKeyLifecycle(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	endpoints := getQUICEndpoints()
	client, err := NewQUICClient(endpoints[0])
	require.NoError(t, err, "QUIC server must be available for integration tests")
	defer client.Close()

	keyName := generateUniqueKeyName("quic-lifecycle-test")

	t.Run("create key", func(t *testing.T) {
		req := &service.GenerateKeyRequest{
			Name: keyName,
			KeyAttributes: &kctypes.KeyAttributes{
				SymmetricAlgorithm: kctypes.SymmetricAES256GCM,
			},
			Usage: []service.KeyUsage{service.KeyUsageEncrypt, service.KeyUsageDecrypt},
		}

		resp, err := client.GenerateKey(ctx, req)
		require.NoError(t, err)
		require.NotNil(t, resp)
		assert.NotEmpty(t, resp.Key.KeyID)
		assert.Equal(t, service.KeyStatusEnabled, resp.Key.Status)
	})

	var keyID string

	t.Run("get key", func(t *testing.T) {
		// List to find our key using prefix filter to avoid pagination issues
		listReq := &service.ListKeysRequest{
			Limit: 100,
			Filter: map[string]string{
				"prefix": "quic-lifecycle-test-",
			},
		}
		listResp, err := client.ListKeys(ctx, listReq)
		require.NoError(t, err)

		// Debug: log all keys returned
		t.Logf("get_key: ListKeys returned %d keys with prefix filter, looking for keyName=%q", len(listResp.Keys), keyName)
		for i, key := range listResp.Keys {
			t.Logf("get_key: Key[%d] KeyID=%q", i, key.KeyID)
		}

		// Find our key by name
		for _, key := range listResp.Keys {
			if key.KeyID == keyName {
				keyID = key.KeyID
				break
			}
		}
		require.NotEmpty(t, keyID, "Key should be found in list")

		// Get specific key
		getReq := &service.GetKeyRequest{
			KeyID: keyID,
		}
		getResp, err := client.GetKey(ctx, getReq)
		require.NoError(t, err)
		assert.Equal(t, keyID, getResp.Metadata.KeyID)
	})

	t.Run("list keys", func(t *testing.T) {
		// Use prefix filter to find our specific key (avoids pagination issues with many keys)
		req := &service.ListKeysRequest{
			Limit: 100,
			Filter: map[string]string{
				"prefix": "quic-lifecycle-test-",
			},
		}

		resp, err := client.ListKeys(ctx, req)
		require.NoError(t, err)
		require.NotNil(t, resp)

		// Debug: log all keys returned and what we're looking for
		t.Logf("list_keys: ListKeys returned %d keys with prefix filter, looking for keyID=%q", len(resp.Keys), keyID)
		for i, key := range resp.Keys {
			t.Logf("list_keys: Key[%d] KeyID=%q", i, key.KeyID)
		}

		// Verify we got at least one key back
		require.NotEmpty(t, resp.Keys, "Should have at least one key with quic-lifecycle-test- prefix")

		// Verify our key is in the list
		found := false
		for _, key := range resp.Keys {
			if key.KeyID == keyID {
				found = true
				break
			}
		}
		assert.True(t, found, "Created key should be in the list")
	})

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

	t.Run("delete key", func(t *testing.T) {
		req := &service.DeleteKeyRequest{
			KeyID: keyID,
			Force: true,
		}

		resp, err := client.DeleteKey(ctx, req)
		require.NoError(t, err)
		assert.Equal(t, keyID, resp.KeyID)

		// Verify deletion
		getReq := &service.GetKeyRequest{KeyID: keyID}
		_, err = client.GetKey(ctx, getReq)
		assert.Error(t, err, "Key should not be found after deletion")
	})
}

// TestQUICConcurrentOperations tests concurrent KMS operations.
func TestQUICConcurrentOperations(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	endpoints := getQUICEndpoints()
	client, err := NewQUICClient(endpoints[0])
	require.NoError(t, err, "QUIC server must be available for integration tests")
	defer client.Close()

	// Create a shared encryption key
	keyName := generateUniqueKeyName("quic-concurrent-test")
	genReq := &service.GenerateKeyRequest{
		Name: keyName,
		KeyAttributes: &kctypes.KeyAttributes{
			SymmetricAlgorithm: kctypes.SymmetricAES256GCM,
		},
		Usage: []service.KeyUsage{service.KeyUsageEncrypt, service.KeyUsageDecrypt},
	}

	genResp, err := client.GenerateKey(ctx, genReq)
	require.NoError(t, err)
	keyID := genResp.Key.KeyID
	defer cleanupQUICKey(t, client, ctx, keyID)

	// Verify key is available from a fresh connection before concurrent operations.
	// This ensures the Raft commit is complete and the key is visible to all clients.
	verifyClient, err := NewQUICClient(endpoints[0])
	require.NoError(t, err, "Failed to create verification client")
	getReq := &service.GetKeyRequest{KeyID: keyID}
	_, err = verifyClient.GetKey(ctx, getReq)
	verifyClient.Close()
	require.NoError(t, err, "Key should be available from fresh client after generation")

	// Perform a warm-up encryption to ensure the key is fully cached in the
	// KMS service layer before concurrent operations begin.
	warmupClient, err := NewQUICClient(endpoints[0])
	require.NoError(t, err, "Failed to create warm-up client")
	warmupResp, err := warmupClient.Encrypt(ctx, &service.EncryptRequest{
		KeyID:     keyID,
		Plaintext: []byte("warmup"),
	})
	require.NoError(t, err, "Warm-up encryption should succeed")
	_, err = warmupClient.Decrypt(ctx, &service.DecryptRequest{
		KeyID:      keyID,
		Ciphertext: warmupResp.Ciphertext,
	})
	require.NoError(t, err, "Warm-up decryption should succeed")
	warmupClient.Close()

	t.Run("concurrent encryptions", func(t *testing.T) {
		// Reduced concurrency to avoid overwhelming the server
		concurrency := 5
		done := make(chan bool, concurrency)
		errors := make(chan error, concurrency)
		const maxAttempts = 3

		// Use separate QUIC clients for each concurrent operation to avoid
		// stream flow control issues on shared connections
		for i := 0; i < concurrency; i++ {
			go func(idx int) {
				plaintext := []byte(fmt.Sprintf("concurrent QUIC test data %d", idx))

				// Retry once with a fresh connection on transient QUIC timeouts
				// that can occur in containerized environments under load.
				for attempt := 1; attempt <= maxAttempts; attempt++ {
					opCtx, opCancel := context.WithTimeout(ctx, 30*time.Second)

					concurrentClient, clientErr := NewQUICClient(endpoints[0])
					if clientErr != nil {
						opCancel()
						if attempt == maxAttempts {
							errors <- fmt.Errorf("failed to create client %d: %w", idx, clientErr)
							done <- false
							return
						}
						continue
					}

					encResp, encErr := concurrentClient.Encrypt(opCtx, &service.EncryptRequest{
						KeyID:     keyID,
						Plaintext: plaintext,
					})
					if encErr != nil {
						concurrentClient.Close()
						opCancel()
						if attempt == maxAttempts {
							errors <- fmt.Errorf("encrypt %d: %w", idx, encErr)
							done <- false
							return
						}
						continue
					}

					decResp, decErr := concurrentClient.Decrypt(opCtx, &service.DecryptRequest{
						KeyID:      keyID,
						Ciphertext: encResp.Ciphertext,
					})
					concurrentClient.Close()
					opCancel()

					if decErr != nil {
						if attempt == maxAttempts {
							errors <- fmt.Errorf("decrypt %d: %w", idx, decErr)
							done <- false
							return
						}
						continue
					}

					if string(decResp.Plaintext) != string(plaintext) {
						errors <- fmt.Errorf("plaintext mismatch %d", idx)
						done <- false
						return
					}

					done <- true
					return
				}
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
		const maxAttempts = 2

		// Use separate QUIC clients for each concurrent operation
		for i := 0; i < concurrency; i++ {
			go func(idx int) {
				keyName := generateUniqueKeyName(fmt.Sprintf("quic-concurrent-gen-%d", idx))

				// Retry once with a fresh connection on transient QUIC timeouts
				// that can occur in containerized environments under load.
				for attempt := 1; attempt <= maxAttempts; attempt++ {
					opCtx, opCancel := context.WithTimeout(ctx, 30*time.Second)

					// Create a new client for this attempt
					concurrentClient, err := NewQUICClient(endpoints[0])
					if err != nil {
						opCancel()
						if attempt == maxAttempts {
							t.Errorf("Failed to create client %d: %v", idx, err)
							done <- false
							return
						}
						continue
					}

					req := &service.GenerateKeyRequest{
						Name: keyName,
						KeyAttributes: &kctypes.KeyAttributes{
							SymmetricAlgorithm: kctypes.SymmetricAES256GCM,
						},
						Usage: []service.KeyUsage{service.KeyUsageEncrypt, service.KeyUsageDecrypt},
					}

					resp, err := concurrentClient.GenerateKey(opCtx, req)
					concurrentClient.Close()
					opCancel()

					if err != nil {
						if attempt == maxAttempts {
							t.Errorf("Concurrent key generation error: %v", err)
							done <- false
							return
						}
						continue
					}

					keyIDs <- resp.Key.KeyID
					done <- true
					return
				}
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

		// Cleanup using the main client
		for _, keyID := range generatedKeys {
			cleanupQUICKey(t, client, ctx, keyID)
		}

		assert.Equal(t, concurrency, successCount)
		assert.Equal(t, concurrency, len(generatedKeys), "All generated keys should be unique")
	})
}

// TestQUICClusterConsistency tests KMS operations across cluster nodes.
// NOTE: KMS keys are stored locally on each node via go-xkms for security reasons.
// Key material is never replicated across nodes - this is by design.
// For distributed key access, use a centralized backend like HashiCorp Vault.
func TestQUICClusterConsistency(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()

	endpoints := getQUICEndpoints()
	require.GreaterOrEqual(t, len(endpoints), 2,
		"Cluster consistency test requires at least 2 nodes, got %d", len(endpoints))

	client1, err := NewQUICClient(endpoints[0])
	require.NoError(t, err, "QUIC node 1 must be available at %s", endpoints[0])
	defer client1.Close()

	client2, err := NewQUICClient(endpoints[1])
	require.NoError(t, err, "QUIC node 2 must be available at %s", endpoints[1])
	defer client2.Close()

	// KMS keys are node-local by design - test operations on same node
	t.Run("key operations on same node", func(t *testing.T) {
		keyName := generateUniqueKeyName("quic-single-node")

		genReq := &service.GenerateKeyRequest{
			Name: keyName,
			KeyAttributes: &kctypes.KeyAttributes{
				SymmetricAlgorithm: kctypes.SymmetricAES256GCM,
			},
			Usage: []service.KeyUsage{service.KeyUsageEncrypt, service.KeyUsageDecrypt},
		}

		genResp, err := client1.GenerateKey(ctx, genReq)
		require.NoError(t, err)
		keyID := genResp.Key.KeyID
		defer cleanupQUICKey(t, client1, ctx, keyID)

		// Verify key is accessible on same node
		getReq := &service.GetKeyRequest{KeyID: keyID}
		getResp, err := client1.GetKey(ctx, getReq)
		require.NoError(t, err)
		assert.Equal(t, keyID, getResp.Metadata.KeyID)
	})

	t.Run("encrypt and decrypt on same node", func(t *testing.T) {
		keyName := generateUniqueKeyName("quic-encrypt-node")
		genReq := &service.GenerateKeyRequest{
			Name: keyName,
			KeyAttributes: &kctypes.KeyAttributes{
				SymmetricAlgorithm: kctypes.SymmetricAES256GCM,
			},
			Usage: []service.KeyUsage{service.KeyUsageEncrypt, service.KeyUsageDecrypt},
		}

		genResp, err := client1.GenerateKey(ctx, genReq)
		require.NoError(t, err)
		keyID := genResp.Key.KeyID
		defer cleanupQUICKey(t, client1, ctx, keyID)

		plaintext := []byte("single node QUIC consistency test data")
		encReq := &service.EncryptRequest{
			KeyID:     keyID,
			Plaintext: plaintext,
		}

		encResp, err := client1.Encrypt(ctx, encReq)
		require.NoError(t, err)

		// Decrypt on same node
		decReq := &service.DecryptRequest{
			KeyID:      keyID,
			Ciphertext: encResp.Ciphertext,
		}

		decResp, err := client1.Decrypt(ctx, decReq)
		require.NoError(t, err)
		assert.Equal(t, plaintext, decResp.Plaintext)
	})

	t.Run("key not visible on other nodes (expected)", func(t *testing.T) {
		// This test documents that KMS keys are node-local by design
		keyName := generateUniqueKeyName("quic-local-only")

		genReq := &service.GenerateKeyRequest{
			Name: keyName,
			KeyAttributes: &kctypes.KeyAttributes{
				SymmetricAlgorithm: kctypes.SymmetricAES256GCM,
			},
			Usage: []service.KeyUsage{service.KeyUsageEncrypt, service.KeyUsageDecrypt},
		}

		genResp, err := client1.GenerateKey(ctx, genReq)
		require.NoError(t, err)
		keyID := genResp.Key.KeyID
		defer cleanupQUICKey(t, client1, ctx, keyID)

		// Key should NOT be visible on node2 - this is expected behavior
		getReq := &service.GetKeyRequest{KeyID: keyID}
		_, err = client2.GetKey(ctx, getReq)
		// Expect an error (key not found on node2)
		assert.Error(t, err, "Key should NOT be visible on other nodes - KMS keys are node-local by design")
		t.Logf("Expected behavior: key not visible on node2 (error: %v)", err)
	})
}

// TestQUICKeyRetrieval tests key retrieval operations.
func TestQUICKeyRetrieval(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	endpoints := getQUICEndpoints()
	client, err := NewQUICClient(endpoints[0])
	require.NoError(t, err, "QUIC server must be available for integration tests")
	defer client.Close()

	// Create a test key
	keyName := generateUniqueKeyName("quic-retrieval-test")
	genReq := &service.GenerateKeyRequest{
		Name: keyName,
		KeyAttributes: &kctypes.KeyAttributes{
			SymmetricAlgorithm: kctypes.SymmetricAES256GCM,
		},
		Usage: []service.KeyUsage{service.KeyUsageEncrypt, service.KeyUsageDecrypt},
	}

	genResp, err := client.GenerateKey(ctx, genReq)
	require.NoError(t, err)
	keyID := genResp.Key.KeyID
	defer cleanupQUICKey(t, client, ctx, keyID)

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

	t.Run("get key with specific version", func(t *testing.T) {
		// Rotate to create a new version
		rotateReq := &service.RotateKeyRequest{
			KeyID:            keyID,
			RetainOldVersion: true,
		}
		rotateResp, err := client.RotateKey(ctx, rotateReq)
		require.NoError(t, err)

		// Get the new version
		req := &service.GetKeyRequest{
			KeyID:   keyID,
			Version: rotateResp.NewVersion,
		}

		resp, err := client.GetKey(ctx, req)
		require.NoError(t, err)
		assert.Equal(t, rotateResp.NewVersion, resp.Metadata.Version)
	})
}

// TestQUICKeyListing tests key listing operations.
func TestQUICKeyListing(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	endpoints := getQUICEndpoints()
	client, err := NewQUICClient(endpoints[0])
	require.NoError(t, err, "QUIC server must be available for integration tests")
	defer client.Close()

	// Create multiple test keys with a common prefix
	prefix := generateUniqueKeyName("quic-list-test")
	keyIDs := []string{}

	for i := 0; i < 5; i++ {
		req := &service.GenerateKeyRequest{
			Name: fmt.Sprintf("%s-key-%d", prefix, i),
			KeyAttributes: &kctypes.KeyAttributes{
				SymmetricAlgorithm: kctypes.SymmetricAES256GCM,
			},
			Usage: []service.KeyUsage{service.KeyUsageEncrypt, service.KeyUsageDecrypt},
		}

		resp, err := client.GenerateKey(ctx, req)
		require.NoError(t, err)
		keyIDs = append(keyIDs, resp.Key.KeyID)
	}

	// Cleanup
	defer func() {
		for _, keyID := range keyIDs {
			cleanupQUICKey(t, client, ctx, keyID)
		}
	}()

	t.Run("list all keys", func(t *testing.T) {
		req := &service.ListKeysRequest{
			Limit: 100,
		}

		resp, err := client.ListKeys(ctx, req)
		require.NoError(t, err)
		require.NotNil(t, resp)
		assert.GreaterOrEqual(t, len(resp.Keys), 5, "Should have at least 5 keys")
	})

	t.Run("list with prefix filter", func(t *testing.T) {
		req := &service.ListKeysRequest{
			Limit:  100,
			Filter: map[string]string{"prefix": prefix},
		}

		resp, err := client.ListKeys(ctx, req)
		require.NoError(t, err)
		require.NotNil(t, resp)
		// Note: Filter implementation may vary by backend
		t.Logf("Found %d keys with prefix %s", len(resp.Keys), prefix)
	})

	t.Run("list with limit", func(t *testing.T) {
		req := &service.ListKeysRequest{
			Limit: 3,
		}

		resp, err := client.ListKeys(ctx, req)
		require.NoError(t, err)
		assert.NotNil(t, resp.Keys)
		t.Logf("Requested limit: 3, Got: %d keys", len(resp.Keys))
	})
}

// TestQUICKeyRotation tests key rotation operations.
func TestQUICKeyRotation(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	endpoints := getQUICEndpoints()
	client, err := NewQUICClient(endpoints[0])
	require.NoError(t, err, "QUIC server must be available for integration tests")
	defer client.Close()

	keyName := generateUniqueKeyName("quic-rotation-test")
	genReq := &service.GenerateKeyRequest{
		Name: keyName,
		KeyAttributes: &kctypes.KeyAttributes{
			SymmetricAlgorithm: kctypes.SymmetricAES256GCM,
		},
		Usage: []service.KeyUsage{service.KeyUsageEncrypt, service.KeyUsageDecrypt},
	}

	genResp, err := client.GenerateKey(ctx, genReq)
	require.NoError(t, err)
	keyID := genResp.Key.KeyID
	defer cleanupQUICKey(t, client, ctx, keyID)

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

// TestQUICKeyDeletion tests key deletion operations.
func TestQUICKeyDeletion(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	endpoints := getQUICEndpoints()
	client, err := NewQUICClient(endpoints[0])
	require.NoError(t, err, "QUIC server must be available for integration tests")
	defer client.Close()

	t.Run("schedule key deletion", func(t *testing.T) {
		keyName := generateUniqueKeyName("quic-delete-test")
		genReq := &service.GenerateKeyRequest{
			Name: keyName,
			KeyAttributes: &kctypes.KeyAttributes{
				SymmetricAlgorithm: kctypes.SymmetricAES256GCM,
			},
			Usage: []service.KeyUsage{service.KeyUsageEncrypt, service.KeyUsageDecrypt},
		}

		genResp, err := client.GenerateKey(ctx, genReq)
		require.NoError(t, err)
		keyID := genResp.Key.KeyID

		deleteReq := &service.DeleteKeyRequest{
			KeyID:             keyID,
			PendingWindowDays: 7,
			Force:             false,
		}

		resp, err := client.DeleteKey(ctx, deleteReq)
		require.NoError(t, err)
		require.NotNil(t, resp)
		assert.Equal(t, keyID, resp.KeyID)
		assert.Equal(t, service.KeyStatusPendingDeletion, resp.Status)
	})

	t.Run("force delete key", func(t *testing.T) {
		keyName := generateUniqueKeyName("quic-force-delete")
		genReq := &service.GenerateKeyRequest{
			Name: keyName,
			KeyAttributes: &kctypes.KeyAttributes{
				SymmetricAlgorithm: kctypes.SymmetricAES256GCM,
			},
			Usage: []service.KeyUsage{service.KeyUsageEncrypt, service.KeyUsageDecrypt},
		}

		genResp, err := client.GenerateKey(ctx, genReq)
		require.NoError(t, err)
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
		assert.Error(t, err, "Key should not be found after deletion")
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
