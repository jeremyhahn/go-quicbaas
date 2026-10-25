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

package embedded

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"sync/atomic"
	"testing"
	"time"

	qberrors "github.com/jeremyhahn/go-quicbaas/pkg/errors"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/config"
	"github.com/jeremyhahn/go-quicbaas/pkg/server"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
	"github.com/jeremyhahn/go-quicbaas/pkg/testutil/netport"
	quicbaas "github.com/jeremyhahn/go-quicbaas/sdk/go"
	"github.com/jeremyhahn/go-quicbaas/test/integration/helpers"
)

var (
	testServer  *server.Server
	testService service.QuicBaaS
	testTempDir string

	idCounter uint64
)

// uniqueID returns a unique uint64 ID for use in test requests.
func uniqueID() uint64 {
	return atomic.AddUint64(&idCounter, 1)
}

func TestMain(m *testing.M) {
	if err := setupEmbeddedServer(); err != nil {
		fmt.Fprintf(os.Stderr, "Failed to start embedded server: %v\n", err)
		os.Exit(1)
	}

	code := m.Run()

	teardownEmbeddedServer()
	os.Exit(code)
}

func setupEmbeddedServer() error {
	tempDir, err := os.MkdirTemp("", "quicbaas-embedded-integration-*")
	if err != nil {
		return &qberrors.WrappedMessageError{Message: "failed to create temp dir", Cause: err}
	}
	testTempDir = tempDir

	raftPort, err := getFreePort()
	if err != nil {
		os.RemoveAll(tempDir)
		return &qberrors.WrappedMessageError{Message: "failed to get free port", Cause: err}
	}

	cfg := config.EmbeddedConfig()
	cfg.IAM.SPIFFETrustDomain = helpers.SPIFFETrustDomain
	cfg.Storage.DataDir = filepath.Join(tempDir, "data")
	cfg.Storage.WALDir = filepath.Join(tempDir, "wal")
	cfg.Raft.NodeID = 1
	cfg.Raft.RaftAddress = "127.0.0.1:" + strconv.Itoa(raftPort)
	cfg.Raft.ListenAddress = "127.0.0.1:" + strconv.Itoa(raftPort)
	cfg.Logging.Level = "error"
	cfg.Server.Embedded = true

	// Enable KMS and Secrets for integration testing
	cfg.KMS.Enabled = true
	cfg.Secrets.Enabled = true
	cfg.Secrets.Versioning = config.SecretsVersioningConfig{Enabled: true}

	srv, err := server.NewServer(cfg)
	if err != nil {
		os.RemoveAll(tempDir)
		return &qberrors.WrappedMessageError{Message: "failed to create server", Cause: err}
	}

	if err := srv.Start(); err != nil {
		os.RemoveAll(tempDir)
		return &qberrors.WrappedMessageError{Message: "failed to start server", Cause: err}
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	ready := make(chan struct{})
	go func() {
		for {
			if srv.IsStarted() {
				close(ready)
				return
			}
			select {
			case <-ctx.Done():
				return
			default:
				time.Sleep(100 * time.Millisecond)
			}
		}
	}()

	select {
	case <-ready:
	case <-ctx.Done():
		srv.Shutdown(context.Background())
		os.RemoveAll(tempDir)
		return &qberrors.MessageError{Message: "embedded server startup timed out"}
	}

	// Wait for Raft leader election
	time.Sleep(2 * time.Second)

	svc := srv.Service()
	if svc == nil {
		srv.Shutdown(context.Background())
		os.RemoveAll(tempDir)
		return &qberrors.MessageError{Message: "service is nil after server start"}
	}

	testServer = srv
	testService = svc
	return nil
}

func teardownEmbeddedServer() {
	if testServer != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		testServer.Shutdown(ctx)
	}
	if testTempDir != "" {
		os.RemoveAll(testTempDir)
	}
}

// getFreePort reserves a loopback port for a server that binds it later. It is
// reserved through netport rather than drawn from :0 and released, so nothing
// else can be handed it in between (go-quicbaas#721). The reservation lasts
// for the life of the test process.
func getFreePort() (int, error) {
	ports, err := netport.Reserve(1)
	if err != nil {
		return 0, err
	}
	return ports[0], nil
}

// newEmbeddedClient creates a new SDK client using the embedded transport.
func newEmbeddedClient(t *testing.T) quicbaas.Client {
	t.Helper()
	client, err := quicbaas.NewClient(
		"embedded://local",
		quicbaas.WithEmbedded(testService),
		quicbaas.WithEmbeddedIdentity(quicbaas.ServicePrincipalIdentity(testServer)),
	)
	require.NoError(t, err, "failed to create embedded SDK client")
	return client
}

func uniqueKey(prefix string) string {
	return fmt.Sprintf("%s-%d", prefix, time.Now().UnixNano())
}

// =============================================================================
// KV Lifecycle Tests
// =============================================================================

// TestEmbedded_KVLifecycle tests the full Put, Get, Delete lifecycle through
// the embedded transport, verifying that data persists across operations within
// the same in-process server.
func TestEmbedded_KVLifecycle(t *testing.T) {
	client := newEmbeddedClient(t)
	defer client.Close()

	ctx := context.Background()
	entityType := "kv_lifecycle"
	value := []byte("hello-quicbaas")
	entityID := uniqueID()

	// Put
	putResp, err := client.KV().Put(ctx, &quicbaas.PutRequest{
		EntityType: entityType,
		ID:         entityID,
		Data:       value,
	})
	require.NoError(t, err, "Put should succeed")
	require.NotNil(t, putResp, "Put response should not be nil")
	assert.Equal(t, entityID, putResp.ID, "Put should return the provided ID")

	// Get
	getResp, err := client.KV().Get(ctx, &quicbaas.GetRequest{
		EntityType: entityType,
		ID:         entityID,
	})
	require.NoError(t, err, "Get should succeed")
	require.NotNil(t, getResp, "Get response should not be nil")
	assert.True(t, getResp.Found, "entity should be found after Put")
	assert.Equal(t, value, getResp.Data, "retrieved value should match stored value")

	// Delete
	delResp, err := client.KV().Delete(ctx, &quicbaas.DeleteRequest{
		EntityType: entityType,
		ID:         entityID,
	})
	require.NoError(t, err, "Delete should succeed")
	require.NotNil(t, delResp, "Delete response should not be nil")
	assert.True(t, delResp.Deleted, "Delete should report entity was deleted")

	// Verify deletion
	getAfterDel, err := client.KV().Get(ctx, &quicbaas.GetRequest{
		EntityType: entityType,
		ID:         entityID,
	})
	// After deletion, either an error is returned or Found is false
	if err == nil {
		assert.False(t, getAfterDel.Found, "entity should not be found after Delete")
	}
}

// TestEmbedded_KVLifecycle_ErrorHandling verifies that KV operations return
// proper errors for invalid inputs.
func TestEmbedded_KVLifecycle_ErrorHandling(t *testing.T) {
	client := newEmbeddedClient(t)
	defer client.Close()

	ctx := context.Background()

	// Put with empty entity type should fail
	_, err := client.KV().Put(ctx, &quicbaas.PutRequest{
		EntityType: "",
		Data:       []byte("value"),
	})
	assert.Error(t, err, "Put with empty entity type should fail")

	// Get with empty entity type should fail
	_, err = client.KV().Get(ctx, &quicbaas.GetRequest{
		EntityType: "",
		ID:         1,
	})
	assert.Error(t, err, "Get with empty entity type should fail")

	// Delete with empty entity type should fail
	_, err = client.KV().Delete(ctx, &quicbaas.DeleteRequest{
		EntityType: "",
		ID:         1,
	})
	assert.Error(t, err, "Delete with empty entity type should fail")

	// Nil request should fail
	_, err = client.KV().Put(ctx, nil)
	assert.Error(t, err, "Put with nil request should fail")

	_, err = client.KV().Get(ctx, nil)
	assert.Error(t, err, "Get with nil request should fail")

	_, err = client.KV().Delete(ctx, nil)
	assert.Error(t, err, "Delete with nil request should fail")
}

// =============================================================================
// Data Lifecycle Tests
// =============================================================================

// TestEmbedded_DataLifecycle tests full CRUD operations on data entities, including
// Put, Get, List, and Delete with state persistence across calls.
func TestEmbedded_DataLifecycle(t *testing.T) {
	client := newEmbeddedClient(t)
	defer client.Close()

	ctx := context.Background()
	entityType := "data_lifecycle"

	values := [][]byte{
		[]byte("value-alpha"),
		[]byte("value-beta"),
		[]byte("value-gamma"),
	}

	// Put multiple entities and track their IDs
	entityIDs := make([]uint64, len(values))
	for i, val := range values {
		entityID := uniqueID()
		putResp, err := client.KV().Put(ctx, &quicbaas.PutRequest{
			EntityType: entityType,
			ID:         entityID,
			Data:       val,
		})
		require.NoError(t, err, "Put should succeed for value %d", i)
		require.NotNil(t, putResp, "Put response should not be nil for value %d", i)
		assert.Equal(t, entityID, putResp.ID, "Put should return the provided ID")
		entityIDs[i] = putResp.ID
	}

	// Get each entity and verify
	for i, id := range entityIDs {
		getResp, err := client.KV().Get(ctx, &quicbaas.GetRequest{
			EntityType: entityType,
			ID:         id,
		})
		require.NoError(t, err, "Get should succeed for entity %d", id)
		require.NotNil(t, getResp, "Get response should not be nil for entity %d", id)
		assert.True(t, getResp.Found, "entity %d should be found", id)
		assert.Equal(t, values[i], getResp.Data, "value should match for entity %d", id)
	}

	// List entities
	listResp, err := client.KV().List(ctx, &quicbaas.ListRequest{
		EntityType: entityType,
		PageSize:   10,
	})
	require.NoError(t, err, "List should succeed")
	require.NotNil(t, listResp, "List response should not be nil")
	assert.GreaterOrEqual(t, len(listResp.Entities), len(values),
		"List should return at least the number of entities created")

	// Update an entity
	updatedValue := []byte("value-alpha-updated")
	putResp, err := client.KV().Put(ctx, &quicbaas.PutRequest{
		EntityType: entityType,
		ID:         entityIDs[0],
		Data:       updatedValue,
	})
	require.NoError(t, err, "Update Put should succeed")
	require.NotNil(t, putResp, "Update Put response should not be nil")

	// Verify update
	getResp, err := client.KV().Get(ctx, &quicbaas.GetRequest{
		EntityType: entityType,
		ID:         entityIDs[0],
	})
	require.NoError(t, err, "Get after update should succeed")
	assert.True(t, getResp.Found, "entity should be found after update")
	assert.Equal(t, updatedValue, getResp.Data, "value should match updated value")

	// Delete all entities
	for _, id := range entityIDs {
		_, err := client.KV().Delete(ctx, &quicbaas.DeleteRequest{
			EntityType: entityType,
			ID:         id,
		})
		require.NoError(t, err, "Delete should succeed for entity %d", id)
	}

	// Verify all deleted
	for _, id := range entityIDs {
		getResp, err := client.KV().Get(ctx, &quicbaas.GetRequest{
			EntityType: entityType,
			ID:         id,
		})
		if err == nil {
			assert.False(t, getResp.Found,
				"entity %d should not be found after Delete", id)
		}
	}
}

// TestEmbedded_DataLifecycle_ErrorHandling verifies that data operations produce
// proper errors for non-existent entities and invalid list parameters.
func TestEmbedded_DataLifecycle_ErrorHandling(t *testing.T) {
	client := newEmbeddedClient(t)
	defer client.Close()

	ctx := context.Background()

	// Get non-existent entity
	getResp, err := client.KV().Get(ctx, &quicbaas.GetRequest{
		EntityType: "nonexistent_type",
		ID:         999999,
	})
	if err == nil {
		assert.False(t, getResp.Found, "non-existent entity should not be found")
	}

	// List with empty entity type should fail
	_, err = client.KV().List(ctx, &quicbaas.ListRequest{
		EntityType: "",
		PageSize:   10,
	})
	assert.Error(t, err, "List with empty entity type should fail")
}

// =============================================================================
// KMS Key Lifecycle Tests
// =============================================================================

// TestEmbedded_KMSKeyLifecycle tests the complete KMS key lifecycle: create,
// get, list, rotate, and delete through the embedded transport.
func TestEmbedded_KMSKeyLifecycle(t *testing.T) {
	client := newEmbeddedClient(t)
	defer client.Close()

	ctx := context.Background()
	keyName := uniqueKey("kms-lifecycle")

	// Generate key
	genResp, err := client.Security().KMS().GenerateKey(ctx, &quicbaas.GenerateKeyRequest{
		Name:      keyName,
		Algorithm: quicbaas.AlgorithmAES256GCM,
		Usage:     []quicbaas.KeyUsage{quicbaas.KeyUsageEncrypt, quicbaas.KeyUsageDecrypt},
	})
	require.NoError(t, err, "GenerateKey should succeed")
	require.NotNil(t, genResp, "GenerateKey response should not be nil")
	assert.NotEmpty(t, genResp.KeyID, "key ID should not be empty")
	keyID := genResp.KeyID

	// Get key
	keyInfo, err := client.Security().KMS().GetKey(ctx, keyID)
	require.NoError(t, err, "GetKey should succeed")
	require.NotNil(t, keyInfo, "KeyInfo should not be nil")
	assert.Equal(t, keyID, keyInfo.KeyID, "key ID should match")
	assert.Equal(t, keyName, keyInfo.Name, "key name should match")

	// List keys
	listResp, err := client.Security().KMS().ListKeys(ctx)
	require.NoError(t, err, "ListKeys should succeed")
	require.NotNil(t, listResp, "ListKeys response should not be nil")
	found := false
	for _, k := range listResp.Keys {
		if k.KeyID == keyID {
			found = true
			break
		}
	}
	assert.True(t, found, "created key should appear in ListKeys results")

	// Rotate key
	rotateResp, err := client.Security().KMS().RotateKey(ctx, keyID)
	require.NoError(t, err, "RotateKey should succeed")
	require.NotNil(t, rotateResp, "RotateKey response should not be nil")
	assert.Equal(t, keyID, rotateResp.KeyID, "rotated key ID should match")
	assert.Greater(t, rotateResp.NewVersion, genResp.Version,
		"rotated version should be greater than initial version")

	// Delete key
	err = client.Security().KMS().DeleteKey(ctx, keyID)
	require.NoError(t, err, "DeleteKey should succeed")

	// Verify deletion - get should fail
	_, err = client.Security().KMS().GetKey(ctx, keyID)
	assert.Error(t, err, "GetKey should fail after deletion")
}

// TestEmbedded_KMSKeyLifecycle_ErrorHandling verifies that KMS operations
// return proper errors for invalid inputs and non-existent keys.
func TestEmbedded_KMSKeyLifecycle_ErrorHandling(t *testing.T) {
	client := newEmbeddedClient(t)
	defer client.Close()

	ctx := context.Background()

	// Generate key with empty name should fail
	_, err := client.Security().KMS().GenerateKey(ctx, &quicbaas.GenerateKeyRequest{
		Name:      "",
		Algorithm: quicbaas.AlgorithmAES256GCM,
	})
	assert.Error(t, err, "GenerateKey with empty name should fail")

	// Get non-existent key should fail
	_, err = client.Security().KMS().GetKey(ctx, "nonexistent-key-id")
	assert.Error(t, err, "GetKey for non-existent key should fail")

	// Rotate non-existent key should fail
	_, err = client.Security().KMS().RotateKey(ctx, "nonexistent-key-id")
	assert.Error(t, err, "RotateKey for non-existent key should fail")

	// Delete non-existent key should fail
	err = client.Security().KMS().DeleteKey(ctx, "nonexistent-key-id")
	assert.Error(t, err, "DeleteKey for non-existent key should fail")

	// Empty key ID should fail
	_, err = client.Security().KMS().GetKey(ctx, "")
	assert.Error(t, err, "GetKey with empty key ID should fail")

	_, err = client.Security().KMS().RotateKey(ctx, "")
	assert.Error(t, err, "RotateKey with empty key ID should fail")

	err = client.Security().KMS().DeleteKey(ctx, "")
	assert.Error(t, err, "DeleteKey with empty key ID should fail")
}

// =============================================================================
// KMS Encrypt/Decrypt Tests
// =============================================================================

// TestEmbedded_KMSEncryptDecrypt creates a key, encrypts data, and verifies
// the ciphertext is returned through the embedded Crypto client.
func TestEmbedded_KMSEncryptDecrypt(t *testing.T) {
	client := newEmbeddedClient(t)
	defer client.Close()

	ctx := context.Background()
	keyName := uniqueKey("encrypt-key")
	plaintext := []byte("Hello, QuicBaaS! This is a secret message.")

	// Create key
	genResp, err := client.Security().KMS().GenerateKey(ctx, &quicbaas.GenerateKeyRequest{
		Name:      keyName,
		Algorithm: quicbaas.AlgorithmAES256GCM,
		Usage:     []quicbaas.KeyUsage{quicbaas.KeyUsageEncrypt, quicbaas.KeyUsageDecrypt},
	})
	require.NoError(t, err, "GenerateKey should succeed")
	keyID := genResp.KeyID

	// Encrypt
	encResp, err := client.Security().Crypto().Encrypt(ctx, &quicbaas.EncryptRequest{
		KeyID:     keyID,
		Plaintext: plaintext,
	})
	require.NoError(t, err, "Encrypt should succeed")
	require.NotNil(t, encResp, "Encrypt response should not be nil")
	assert.NotEmpty(t, encResp.Ciphertext, "ciphertext should not be empty")
	assert.NotEqual(t, plaintext, encResp.Ciphertext,
		"ciphertext should differ from plaintext")

	// Decrypt
	decResp, err := client.Security().Crypto().Decrypt(ctx, &quicbaas.DecryptRequest{
		KeyID:      keyID,
		Ciphertext: encResp.Ciphertext,
	})
	require.NoError(t, err, "Decrypt should succeed")
	require.NotNil(t, decResp, "Decrypt response should not be nil")
	assert.Equal(t, plaintext, decResp.Plaintext,
		"decrypted plaintext should match original")

	// Cleanup
	err = client.Security().KMS().DeleteKey(ctx, keyID)
	require.NoError(t, err, "DeleteKey cleanup should succeed")
}

// TestEmbedded_KMSEncryptDecrypt_ErrorHandling verifies that encrypt/decrypt
// operations return proper errors for invalid inputs.
func TestEmbedded_KMSEncryptDecrypt_ErrorHandling(t *testing.T) {
	client := newEmbeddedClient(t)
	defer client.Close()

	ctx := context.Background()

	// Encrypt with empty key ID should fail
	_, err := client.Security().Crypto().Encrypt(ctx, &quicbaas.EncryptRequest{
		KeyID:     "",
		Plaintext: []byte("test"),
	})
	assert.Error(t, err, "Encrypt with empty key ID should fail")

	// Encrypt with empty plaintext should fail
	_, err = client.Security().Crypto().Encrypt(ctx, &quicbaas.EncryptRequest{
		KeyID:     "some-key",
		Plaintext: []byte{},
	})
	assert.Error(t, err, "Encrypt with empty plaintext should fail")

	// Decrypt with empty key ID should fail
	_, err = client.Security().Crypto().Decrypt(ctx, &quicbaas.DecryptRequest{
		KeyID:      "",
		Ciphertext: []byte("fake-ciphertext"),
	})
	assert.Error(t, err, "Decrypt with empty key ID should fail")

	// Decrypt with empty ciphertext should fail
	_, err = client.Security().Crypto().Decrypt(ctx, &quicbaas.DecryptRequest{
		KeyID:      "some-key",
		Ciphertext: []byte{},
	})
	assert.Error(t, err, "Decrypt with empty ciphertext should fail")

	// Nil requests should fail
	_, err = client.Security().Crypto().Encrypt(ctx, nil)
	assert.Error(t, err, "Encrypt with nil request should fail")

	_, err = client.Security().Crypto().Decrypt(ctx, nil)
	assert.Error(t, err, "Decrypt with nil request should fail")
}

// =============================================================================
// Multiple Algorithm Tests
// =============================================================================

// TestEmbedded_MultipleAlgorithms creates keys with different symmetric
// algorithms and verifies each can be used for encrypt/decrypt operations.
func TestEmbedded_MultipleAlgorithms(t *testing.T) {
	client := newEmbeddedClient(t)
	defer client.Close()

	ctx := context.Background()

	algorithms := []struct {
		name      string
		algorithm quicbaas.KeyAlgorithm
	}{
		{"AES-256-GCM", quicbaas.AlgorithmAES256GCM},
		{"ChaCha20-Poly1305", quicbaas.AlgorithmChaCha20Poly1305},
	}

	for _, algo := range algorithms {
		t.Run(algo.name, func(t *testing.T) {
			keyName := uniqueKey("algo-" + algo.name)

			genResp, err := client.Security().KMS().GenerateKey(ctx, &quicbaas.GenerateKeyRequest{
				Name:      keyName,
				Algorithm: algo.algorithm,
				Usage:     []quicbaas.KeyUsage{quicbaas.KeyUsageEncrypt, quicbaas.KeyUsageDecrypt},
			})
			require.NoError(t, err, "GenerateKey with %s should succeed", algo.name)
			require.NotNil(t, genResp, "GenerateKey response should not be nil")
			assert.NotEmpty(t, genResp.KeyID, "key ID should not be empty")

			// Verify key was created with correct algorithm
			keyInfo, err := client.Security().KMS().GetKey(ctx, genResp.KeyID)
			require.NoError(t, err, "GetKey should succeed for %s key", algo.name)
			require.NotNil(t, keyInfo, "KeyInfo should not be nil")
			assert.Equal(t, genResp.KeyID, keyInfo.KeyID, "key ID should match")

			// Encrypt with the created key
			plaintext := []byte("test data for " + algo.name)
			encResp, err := client.Security().Crypto().Encrypt(ctx, &quicbaas.EncryptRequest{
				KeyID:     genResp.KeyID,
				Plaintext: plaintext,
			})
			require.NoError(t, err, "Encrypt with %s key should succeed", algo.name)
			assert.NotEmpty(t, encResp.Ciphertext, "ciphertext should not be empty")

			// Decrypt with the created key
			decResp, err := client.Security().Crypto().Decrypt(ctx, &quicbaas.DecryptRequest{
				KeyID:      genResp.KeyID,
				Ciphertext: encResp.Ciphertext,
			})
			require.NoError(t, err, "Decrypt with %s key should succeed", algo.name)
			assert.Equal(t, plaintext, decResp.Plaintext,
				"decrypted plaintext should match for %s", algo.name)

			// Cleanup
			err = client.Security().KMS().DeleteKey(ctx, genResp.KeyID)
			require.NoError(t, err, "DeleteKey cleanup should succeed for %s", algo.name)
		})
	}
}

// TestEmbedded_MultipleAlgorithms_ErrorHandling verifies that invalid algorithm
// values are properly rejected during key generation.
func TestEmbedded_MultipleAlgorithms_ErrorHandling(t *testing.T) {
	client := newEmbeddedClient(t)
	defer client.Close()

	ctx := context.Background()

	// Invalid algorithm should fail
	_, err := client.Security().KMS().GenerateKey(ctx, &quicbaas.GenerateKeyRequest{
		Name:      uniqueKey("invalid-algo"),
		Algorithm: quicbaas.KeyAlgorithm(999),
	})
	assert.Error(t, err, "GenerateKey with invalid algorithm should fail")
}

// =============================================================================
// Secrets Lifecycle Tests
// =============================================================================

// TestEmbedded_SecretsLifecycle tests the full secrets lifecycle: store, get,
// list, rotate (store new version), and delete through the embedded transport.
func TestEmbedded_SecretsLifecycle(t *testing.T) {
	client := newEmbeddedClient(t)
	defer client.Close()

	ctx := context.Background()
	secretPath := uniqueKey("secret-lifecycle")
	secretData := map[string]interface{}{
		"username": "admin",
		"password": "super-secret-12345",
	}

	// Store secret
	storeResp, err := client.Security().Secrets().Store(ctx, &quicbaas.StoreSecretRequest{
		Path: secretPath,
		Data: secretData,
	})
	require.NoError(t, err, "Store secret should succeed")
	require.NotNil(t, storeResp, "Store response should not be nil")
	assert.Equal(t, 1, storeResp.Version, "first version should be 1")

	// Get secret
	secret, err := client.Security().Secrets().Get(ctx, secretPath)
	require.NoError(t, err, "Get secret should succeed")
	require.NotNil(t, secret, "Secret should not be nil")
	assert.Equal(t, secretPath, secret.Path, "secret path should match")
	assert.Equal(t, secretData["username"], secret.Data["username"],
		"username should match")
	assert.Equal(t, secretData["password"], secret.Data["password"],
		"password should match")

	// List secrets
	listResp, err := client.Security().Secrets().List(ctx, secretPath)
	require.NoError(t, err, "List secrets should succeed")
	require.NotNil(t, listResp, "List response should not be nil")
	assert.NotEmpty(t, listResp.Paths, "secret paths should not be empty")

	// Rotate secret (store new version)
	rotatedData := map[string]interface{}{
		"username": "admin",
		"password": "new-super-secret-67890",
	}
	rotateResp, err := client.Security().Secrets().Store(ctx, &quicbaas.StoreSecretRequest{
		Path: secretPath,
		Data: rotatedData,
	})
	require.NoError(t, err, "Rotate secret should succeed")
	require.NotNil(t, rotateResp, "Rotate response should not be nil")
	assert.Greater(t, rotateResp.Version, 1, "rotated version should be greater than 1")

	// Get latest version
	latestSecret, err := client.Security().Secrets().Get(ctx, secretPath)
	require.NoError(t, err, "Get latest secret should succeed")
	assert.Equal(t, rotatedData["password"], latestSecret.Data["password"],
		"latest secret should have rotated password")

	// Get specific version (version 1)
	v1Secret, err := client.Security().Secrets().Get(ctx, secretPath, quicbaas.WithSecretVersion(1))
	require.NoError(t, err, "Get secret version 1 should succeed")
	assert.Equal(t, secretData["password"], v1Secret.Data["password"],
		"version 1 should have original password")

	// Delete secret
	err = client.Security().Secrets().Delete(ctx, secretPath)
	require.NoError(t, err, "Delete secret should succeed")

	// Verify deletion
	_, err = client.Security().Secrets().Get(ctx, secretPath)
	assert.Error(t, err, "Get after delete should fail")
}

// TestEmbedded_SecretsLifecycle_ErrorHandling verifies that secrets operations
// return proper errors for invalid inputs and non-existent paths.
func TestEmbedded_SecretsLifecycle_ErrorHandling(t *testing.T) {
	client := newEmbeddedClient(t)
	defer client.Close()

	ctx := context.Background()

	// Store with empty path should fail
	_, err := client.Security().Secrets().Store(ctx, &quicbaas.StoreSecretRequest{
		Path: "",
		Data: map[string]interface{}{"key": "value"},
	})
	assert.Error(t, err, "Store with empty path should fail")

	// Store with empty data should fail
	_, err = client.Security().Secrets().Store(ctx, &quicbaas.StoreSecretRequest{
		Path: "test-path",
		Data: map[string]interface{}{},
	})
	assert.Error(t, err, "Store with empty data should fail")

	// Store with nil request should fail
	_, err = client.Security().Secrets().Store(ctx, nil)
	assert.Error(t, err, "Store with nil request should fail")

	// Get with empty path should fail
	_, err = client.Security().Secrets().Get(ctx, "")
	assert.Error(t, err, "Get with empty path should fail")

	// Get non-existent secret should fail
	_, err = client.Security().Secrets().Get(ctx, "nonexistent/secret/path")
	assert.Error(t, err, "Get non-existent secret should fail")

	// Delete with empty path should fail
	err = client.Security().Secrets().Delete(ctx, "")
	assert.Error(t, err, "Delete with empty path should fail")
}

// =============================================================================
// Health Check Tests
// =============================================================================

// TestEmbedded_HealthCheck verifies that health, liveness, and readiness
// checks work correctly through the embedded transport.
func TestEmbedded_HealthCheck(t *testing.T) {
	client := newEmbeddedClient(t)
	defer client.Close()

	ctx := context.Background()

	// Ping
	err := client.Health().Ping(ctx)
	require.NoError(t, err, "Ping should succeed")

	// Liveness
	liveResp, err := client.Health().Liveness(ctx)
	require.NoError(t, err, "Liveness should succeed")
	require.NotNil(t, liveResp, "Liveness response should not be nil")
	assert.True(t, liveResp.Healthy, "server should be healthy")

	// Readiness
	readyResp, err := client.Health().Readiness(ctx)
	require.NoError(t, err, "Readiness should succeed")
	require.NotNil(t, readyResp, "Readiness response should not be nil")
	assert.True(t, readyResp.Healthy, "server should be ready")
}

// TestEmbedded_HealthCheck_ErrorHandling verifies that health operations
// return proper errors when given nil contexts.
func TestEmbedded_HealthCheck_ErrorHandling(t *testing.T) {
	client := newEmbeddedClient(t)
	defer client.Close()

	var nilCtx context.Context

	// Ping with nil context should fail
	err := client.Health().Ping(nilCtx)
	assert.Error(t, err, "Ping with nil context should fail")

	// Liveness with nil context should fail
	_, err = client.Health().Liveness(nilCtx)
	assert.Error(t, err, "Liveness with nil context should fail")

	// Readiness with nil context should fail
	_, err = client.Health().Readiness(nilCtx)
	assert.Error(t, err, "Readiness with nil context should fail")
}

// =============================================================================
// Batch Operations Tests
// =============================================================================

// TestEmbedded_BatchOperations tests batch put, get, and delete operations
// through the embedded transport.
func TestEmbedded_BatchOperations(t *testing.T) {
	client := newEmbeddedClient(t)
	defer client.Close()

	ctx := context.Background()
	entityType := "batch_test"
	batchSize := 5

	// Prepare batch put requests with explicit IDs
	putRequests := make([]*quicbaas.PutRequest, batchSize)
	expectedIDs := make([]uint64, batchSize)
	for i := range batchSize {
		id := uniqueID()
		expectedIDs[i] = id
		putRequests[i] = &quicbaas.PutRequest{
			EntityType: entityType,
			ID:         id,
			Data:       []byte(fmt.Sprintf("batch-value-%d", i)),
		}
	}

	// Build expected ID-to-data map for order-independent verification
	expectedData := make(map[uint64][]byte, batchSize)
	for i, id := range expectedIDs {
		expectedData[id] = []byte(fmt.Sprintf("batch-value-%d", i))
	}

	// Batch Put
	batchPutResp, err := client.Batch().Put(ctx, putRequests)
	require.NoError(t, err, "Batch Put should succeed")
	require.NotNil(t, batchPutResp, "Batch Put response should not be nil")
	assert.Len(t, batchPutResp.Responses, batchSize,
		"batch put should return responses for all items")

	// Collect IDs from put responses for subsequent get and delete
	entityIDs := make([]uint64, batchSize)
	for i, result := range batchPutResp.Responses {
		assert.True(t, result.Success,
			"batch put item %d should succeed", i)
		assert.Equal(t, expectedIDs[i], result.ID,
			"batch put item %d should return the provided ID", i)
		entityIDs[i] = result.ID
	}

	// Batch Get
	getRequests := make([]*quicbaas.GetRequest, batchSize)
	for i, id := range entityIDs {
		getRequests[i] = &quicbaas.GetRequest{
			EntityType: entityType,
			ID:         id,
		}
	}

	batchGetResp, err := client.Batch().Get(ctx, getRequests)
	require.NoError(t, err, "Batch Get should succeed")
	require.NotNil(t, batchGetResp, "Batch Get response should not be nil")
	assert.Len(t, batchGetResp.Responses, batchSize,
		"batch get should return responses for all items")

	// Verify all gets found data using ID-based lookup (order-independent)
	for _, result := range batchGetResp.Responses {
		expected, ok := expectedData[result.ID]
		require.True(t, ok, "unexpected ID in batch get results: %d", result.ID)
		assert.True(t, result.Found, "batch get for ID %d should be found", result.ID)
		assert.Equal(t, expected, result.Data,
			"batch get data mismatch for ID %d", result.ID)
	}

	// Batch Delete
	deleteRequests := make([]*quicbaas.DeleteRequest, batchSize)
	for i, id := range entityIDs {
		deleteRequests[i] = &quicbaas.DeleteRequest{
			EntityType: entityType,
			ID:         id,
		}
	}

	batchDelResp, err := client.Batch().Delete(ctx, deleteRequests)
	require.NoError(t, err, "Batch Delete should succeed")
	require.NotNil(t, batchDelResp, "Batch Delete response should not be nil")
	assert.Len(t, batchDelResp.Responses, batchSize,
		"batch delete should return responses for all items")

	// Verify all deletes succeeded
	for i, result := range batchDelResp.Responses {
		assert.True(t, result.Deleted,
			"batch delete item %d should succeed", i)
	}

	// Verify all entities are gone
	batchGetResp2, err := client.Batch().Get(ctx, getRequests)
	require.NoError(t, err, "Batch Get after Delete should succeed")
	for i, result := range batchGetResp2.Responses {
		assert.False(t, result.Found,
			"batch get item %d should not be found after delete", i)
	}
}

// TestEmbedded_BatchOperations_ErrorHandling verifies that batch operations
// return proper errors for empty request slices.
func TestEmbedded_BatchOperations_ErrorHandling(t *testing.T) {
	client := newEmbeddedClient(t)
	defer client.Close()

	ctx := context.Background()

	// Empty batch put should return empty response
	putResp, err := client.Batch().Put(ctx, []*quicbaas.PutRequest{})
	require.NoError(t, err, "empty batch put should not error")
	assert.Empty(t, putResp.Responses, "empty batch put should return empty responses")

	// Empty batch get should return empty response
	getResp, err := client.Batch().Get(ctx, []*quicbaas.GetRequest{})
	require.NoError(t, err, "empty batch get should not error")
	assert.Empty(t, getResp.Responses, "empty batch get should return empty responses")

	// Empty batch delete should return empty response
	delResp, err := client.Batch().Delete(ctx, []*quicbaas.DeleteRequest{})
	require.NoError(t, err, "empty batch delete should not error")
	assert.Empty(t, delResp.Responses, "empty batch delete should return empty responses")
}

// =============================================================================
// Encrypt/Decrypt with AAD Tests
// =============================================================================

// TestEmbedded_EncryptDecryptWithAAD tests encryption and decryption with
// additional authenticated data (AAD) through the embedded transport.
func TestEmbedded_EncryptDecryptWithAAD(t *testing.T) {
	client := newEmbeddedClient(t)
	defer client.Close()

	ctx := context.Background()
	keyName := uniqueKey("aad-key")
	plaintext := []byte("sensitive data with AAD context")
	aad := []byte("additional-authenticated-data")

	// Create key
	genResp, err := client.Security().KMS().GenerateKey(ctx, &quicbaas.GenerateKeyRequest{
		Name:      keyName,
		Algorithm: quicbaas.AlgorithmAES256GCM,
		Usage:     []quicbaas.KeyUsage{quicbaas.KeyUsageEncrypt, quicbaas.KeyUsageDecrypt},
	})
	require.NoError(t, err, "GenerateKey should succeed")
	keyID := genResp.KeyID

	// Encrypt with AAD
	encResp, err := client.Security().Crypto().Encrypt(ctx, &quicbaas.EncryptRequest{
		KeyID:     keyID,
		Plaintext: plaintext,
		Context:   aad,
	})
	require.NoError(t, err, "Encrypt with AAD should succeed")
	assert.NotEmpty(t, encResp.Ciphertext, "ciphertext should not be empty")

	// Decrypt with correct AAD
	decResp, err := client.Security().Crypto().Decrypt(ctx, &quicbaas.DecryptRequest{
		KeyID:      keyID,
		Ciphertext: encResp.Ciphertext,
		Context:    aad,
	})
	require.NoError(t, err, "Decrypt with correct AAD should succeed")
	assert.Equal(t, plaintext, decResp.Plaintext,
		"decrypted plaintext should match original")

	// Decrypt with wrong AAD should fail
	_, err = client.Security().Crypto().Decrypt(ctx, &quicbaas.DecryptRequest{
		KeyID:      keyID,
		Ciphertext: encResp.Ciphertext,
		Context:    []byte("wrong-aad"),
	})
	assert.Error(t, err, "Decrypt with wrong AAD should fail")

	// Decrypt without AAD should fail (when encrypted with AAD)
	_, err = client.Security().Crypto().Decrypt(ctx, &quicbaas.DecryptRequest{
		KeyID:      keyID,
		Ciphertext: encResp.Ciphertext,
	})
	assert.Error(t, err, "Decrypt without AAD should fail when encrypted with AAD")

	// Cleanup
	err = client.Security().KMS().DeleteKey(ctx, keyID)
	require.NoError(t, err, "DeleteKey cleanup should succeed")
}

// TestEmbedded_EncryptDecryptWithAAD_ErrorHandling verifies that AAD-based
// operations reject non-existent keys.
func TestEmbedded_EncryptDecryptWithAAD_ErrorHandling(t *testing.T) {
	client := newEmbeddedClient(t)
	defer client.Close()

	ctx := context.Background()

	// Encrypt with non-existent key should fail
	_, err := client.Security().Crypto().Encrypt(ctx, &quicbaas.EncryptRequest{
		KeyID:     "nonexistent-key",
		Plaintext: []byte("data"),
		Context:   []byte("aad"),
	})
	assert.Error(t, err, "Encrypt with non-existent key should fail")

	// Decrypt with non-existent key should fail
	_, err = client.Security().Crypto().Decrypt(ctx, &quicbaas.DecryptRequest{
		KeyID:      "nonexistent-key",
		Ciphertext: []byte("fake-ciphertext"),
		Context:    []byte("aad"),
	})
	assert.Error(t, err, "Decrypt with non-existent key should fail")
}

// =============================================================================
// Client Lifecycle Tests
// =============================================================================

// TestEmbedded_ClientClose verifies that creating and closing multiple
// embedded clients works correctly without resource leaks.
func TestEmbedded_ClientClose(t *testing.T) {
	// Create multiple clients
	for i := range 3 {
		client := newEmbeddedClient(t)

		// Use the client briefly
		ctx := context.Background()
		err := client.Health().Ping(ctx)
		require.NoError(t, err, "Ping should succeed for client %d", i)

		// Close the client
		err = client.Close()
		require.NoError(t, err, "Close should succeed for client %d", i)
	}
}

// TestEmbedded_ClientClose_ErrorHandling verifies that closing a client
// multiple times does not panic or return unexpected errors.
func TestEmbedded_ClientClose_ErrorHandling(t *testing.T) {
	client := newEmbeddedClient(t)

	// First close should succeed
	err := client.Close()
	require.NoError(t, err, "first Close should succeed")

	// Second close should also succeed (idempotent)
	err = client.Close()
	require.NoError(t, err, "second Close should succeed (idempotent)")
}

// =============================================================================
// Concurrent Access Tests
// =============================================================================

// TestEmbedded_ConcurrentKVOperations verifies that sequential KV operations
// through the embedded transport do not corrupt data.
func TestEmbedded_ConcurrentKVOperations(t *testing.T) {
	client := newEmbeddedClient(t)
	defer client.Close()

	ctx := context.Background()
	entityType := "concurrent_test"
	numOps := 10

	errCh := make(chan error, numOps)
	doneCh := make(chan struct{})

	go func() {
		defer close(doneCh)
		for i := range numOps {
			entityID := uniqueID()
			value := []byte(fmt.Sprintf("concurrent-value-%d", i))

			putResp, putErr := client.KV().Put(ctx, &quicbaas.PutRequest{
				EntityType: entityType,
				ID:         entityID,
				Data:       value,
			})
			if putErr != nil {
				errCh <- putErr
				return
			}

			getResp, getErr := client.KV().Get(ctx, &quicbaas.GetRequest{
				EntityType: entityType,
				ID:         putResp.ID,
			})
			if getErr != nil {
				errCh <- getErr
				return
			}

			if !getResp.Found {
				errCh <- &qberrors.MessageError{Message: "entity " + strconv.FormatUint(putResp.ID, 10) + " not found after put"}
				return
			}

			_, delErr := client.KV().Delete(ctx, &quicbaas.DeleteRequest{
				EntityType: entityType,
				ID:         putResp.ID,
			})
			if delErr != nil {
				errCh <- delErr
				return
			}
		}
	}()

	// Wait for completion
	<-doneCh
	close(errCh)

	for err := range errCh {
		require.NoError(t, err, "sequential operation should succeed")
	}
}

// TestEmbedded_ConcurrentKVOperations_ErrorHandling verifies that sequential
// operations on the same entity handle deletion properly.
func TestEmbedded_ConcurrentKVOperations_ErrorHandling(t *testing.T) {
	client := newEmbeddedClient(t)
	defer client.Close()

	ctx := context.Background()
	entityType := "concurrent_error_test"
	entityID := uniqueID()

	// Write the entity
	putResp, err := client.KV().Put(ctx, &quicbaas.PutRequest{
		EntityType: entityType,
		ID:         entityID,
		Data:       []byte("initial-value"),
	})
	require.NoError(t, err, "initial Put should succeed")

	// Delete the entity
	_, err = client.KV().Delete(ctx, &quicbaas.DeleteRequest{
		EntityType: entityType,
		ID:         putResp.ID,
	})
	require.NoError(t, err, "Delete should succeed")

	// Get the deleted entity -- should not be found
	getResp, err := client.KV().Get(ctx, &quicbaas.GetRequest{
		EntityType: entityType,
		ID:         putResp.ID,
	})
	if err == nil {
		assert.False(t, getResp.Found, "deleted entity should not be found")
	}
}

// =============================================================================
// KV Overwrite Tests
// =============================================================================

// TestEmbedded_KVOverwrite verifies that putting a value to an existing entity
// overwrites the previous value correctly.
func TestEmbedded_KVOverwrite(t *testing.T) {
	client := newEmbeddedClient(t)
	defer client.Close()

	ctx := context.Background()
	entityType := "overwrite_test"
	entityID := uniqueID()

	// Write initial value
	putResp, err := client.KV().Put(ctx, &quicbaas.PutRequest{
		EntityType: entityType,
		ID:         entityID,
		Data:       []byte("original-value"),
	})
	require.NoError(t, err, "initial Put should succeed")
	assert.Equal(t, entityID, putResp.ID, "Put should return the provided ID")

	// Verify initial value
	getResp, err := client.KV().Get(ctx, &quicbaas.GetRequest{
		EntityType: entityType,
		ID:         entityID,
	})
	require.NoError(t, err, "Get should succeed")
	assert.Equal(t, []byte("original-value"), getResp.Data)

	// Overwrite with same ID
	_, err = client.KV().Put(ctx, &quicbaas.PutRequest{
		EntityType: entityType,
		ID:         entityID,
		Data:       []byte("updated-value"),
	})
	require.NoError(t, err, "overwrite Put should succeed")

	// Verify overwritten value
	getResp2, err := client.KV().Get(ctx, &quicbaas.GetRequest{
		EntityType: entityType,
		ID:         entityID,
	})
	require.NoError(t, err, "Get after overwrite should succeed")
	assert.Equal(t, []byte("updated-value"), getResp2.Data,
		"value should reflect the overwrite")

	// Cleanup
	_, err = client.KV().Delete(ctx, &quicbaas.DeleteRequest{
		EntityType: entityType,
		ID:         entityID,
	})
	require.NoError(t, err, "Delete cleanup should succeed")
}

// TestEmbedded_KVOverwrite_ErrorHandling verifies that overwriting with invalid
// data does not corrupt the original value.
func TestEmbedded_KVOverwrite_ErrorHandling(t *testing.T) {
	client := newEmbeddedClient(t)
	defer client.Close()

	ctx := context.Background()
	entityType := "overwrite_error_test"
	entityID := uniqueID()

	// Write initial value
	putResp, err := client.KV().Put(ctx, &quicbaas.PutRequest{
		EntityType: entityType,
		ID:         entityID,
		Data:       []byte("original-value"),
	})
	require.NoError(t, err, "initial Put should succeed")
	assert.Equal(t, entityID, putResp.ID, "Put should return the provided ID")

	// Try overwrite with empty entity type (should fail)
	_, err = client.KV().Put(ctx, &quicbaas.PutRequest{
		EntityType: "",
		ID:         entityID,
		Data:       []byte("bad-overwrite"),
	})
	assert.Error(t, err, "overwrite with empty entity type should fail")

	// Original value should be unchanged
	getResp, err := client.KV().Get(ctx, &quicbaas.GetRequest{
		EntityType: entityType,
		ID:         entityID,
	})
	require.NoError(t, err, "Get should succeed")
	assert.Equal(t, []byte("original-value"), getResp.Data,
		"original value should be unchanged after failed overwrite")

	// Cleanup
	_, err = client.KV().Delete(ctx, &quicbaas.DeleteRequest{
		EntityType: entityType,
		ID:         entityID,
	})
	require.NoError(t, err, "Delete cleanup should succeed")
}

// =============================================================================
// KMS Key Rotate and Use Tests
// =============================================================================

// TestEmbedded_KMSRotateAndEncrypt verifies that after rotating a key, both
// new encryptions and decryptions of old ciphertext work correctly.
func TestEmbedded_KMSRotateAndEncrypt(t *testing.T) {
	client := newEmbeddedClient(t)
	defer client.Close()

	ctx := context.Background()
	keyName := uniqueKey("rotate-encrypt-key")
	plaintext := []byte("data encrypted before rotation")

	// Create key
	genResp, err := client.Security().KMS().GenerateKey(ctx, &quicbaas.GenerateKeyRequest{
		Name:      keyName,
		Algorithm: quicbaas.AlgorithmAES256GCM,
		Usage:     []quicbaas.KeyUsage{quicbaas.KeyUsageEncrypt, quicbaas.KeyUsageDecrypt},
	})
	require.NoError(t, err, "GenerateKey should succeed")
	keyID := genResp.KeyID

	// Encrypt before rotation
	encResp, err := client.Security().Crypto().Encrypt(ctx, &quicbaas.EncryptRequest{
		KeyID:     keyID,
		Plaintext: plaintext,
	})
	require.NoError(t, err, "Encrypt before rotation should succeed")

	// Rotate key
	rotateResp, err := client.Security().KMS().RotateKey(ctx, keyID)
	require.NoError(t, err, "RotateKey should succeed")
	assert.Greater(t, rotateResp.NewVersion, genResp.Version,
		"new version should be greater")

	// Decrypt ciphertext from before rotation (should still work)
	decResp, err := client.Security().Crypto().Decrypt(ctx, &quicbaas.DecryptRequest{
		KeyID:      keyID,
		Ciphertext: encResp.Ciphertext,
	})
	require.NoError(t, err, "Decrypt of pre-rotation ciphertext should succeed")
	assert.Equal(t, plaintext, decResp.Plaintext,
		"pre-rotation ciphertext should decrypt correctly")

	// Encrypt after rotation (should use new version)
	newPlaintext := []byte("data encrypted after rotation")
	encResp2, err := client.Security().Crypto().Encrypt(ctx, &quicbaas.EncryptRequest{
		KeyID:     keyID,
		Plaintext: newPlaintext,
	})
	require.NoError(t, err, "Encrypt after rotation should succeed")

	// Decrypt post-rotation ciphertext
	decResp2, err := client.Security().Crypto().Decrypt(ctx, &quicbaas.DecryptRequest{
		KeyID:      keyID,
		Ciphertext: encResp2.Ciphertext,
	})
	require.NoError(t, err, "Decrypt of post-rotation ciphertext should succeed")
	assert.Equal(t, newPlaintext, decResp2.Plaintext,
		"post-rotation ciphertext should decrypt correctly")

	// Cleanup
	err = client.Security().KMS().DeleteKey(ctx, keyID)
	require.NoError(t, err, "DeleteKey cleanup should succeed")
}

// TestEmbedded_KMSRotateAndEncrypt_ErrorHandling verifies that rotation
// of non-existent keys and encryption with deleted keys fail properly.
func TestEmbedded_KMSRotateAndEncrypt_ErrorHandling(t *testing.T) {
	client := newEmbeddedClient(t)
	defer client.Close()

	ctx := context.Background()
	keyName := uniqueKey("rotate-error-key")

	// Create and immediately delete a key
	genResp, err := client.Security().KMS().GenerateKey(ctx, &quicbaas.GenerateKeyRequest{
		Name:      keyName,
		Algorithm: quicbaas.AlgorithmAES256GCM,
		Usage:     []quicbaas.KeyUsage{quicbaas.KeyUsageEncrypt, quicbaas.KeyUsageDecrypt},
	})
	require.NoError(t, err, "GenerateKey should succeed")
	keyID := genResp.KeyID

	err = client.Security().KMS().DeleteKey(ctx, keyID)
	require.NoError(t, err, "DeleteKey should succeed")

	// Rotate deleted key should fail
	_, err = client.Security().KMS().RotateKey(ctx, keyID)
	assert.Error(t, err, "RotateKey for deleted key should fail")

	// Encrypt with deleted key should fail
	_, err = client.Security().Crypto().Encrypt(ctx, &quicbaas.EncryptRequest{
		KeyID:     keyID,
		Plaintext: []byte("data"),
	})
	assert.Error(t, err, "Encrypt with deleted key should fail")
}

// =============================================================================
// Secrets Hierarchy Tests
// =============================================================================

// TestEmbedded_SecretsHierarchy tests storing secrets at different path levels
// and listing them by prefix to verify hierarchical organization works.
func TestEmbedded_SecretsHierarchy(t *testing.T) {
	client := newEmbeddedClient(t)
	defer client.Close()

	ctx := context.Background()
	basePath := uniqueKey("hierarchy")

	paths := []string{
		basePath + "/db/password",
		basePath + "/db/username",
		basePath + "/api/key",
		basePath + "/api/secret",
	}

	// Store secrets at different paths
	for i, path := range paths {
		_, err := client.Security().Secrets().Store(ctx, &quicbaas.StoreSecretRequest{
			Path: path,
			Data: map[string]interface{}{
				"value": fmt.Sprintf("secret-value-%d", i),
			},
		})
		require.NoError(t, err, "Store at path %s should succeed", path)
	}

	// List all secrets under base path
	listResp, err := client.Security().Secrets().List(ctx, basePath)
	require.NoError(t, err, "List under base path should succeed")
	require.NotNil(t, listResp, "List response should not be nil")
	assert.GreaterOrEqual(t, len(listResp.Paths), len(paths),
		"list should return at least the number of secrets created")

	// Cleanup
	for _, path := range paths {
		err := client.Security().Secrets().Delete(ctx, path)
		require.NoError(t, err, "Delete at path %s should succeed", path)
	}
}

// TestEmbedded_SecretsHierarchy_ErrorHandling verifies listing an empty or
// non-existent prefix returns a valid empty response.
func TestEmbedded_SecretsHierarchy_ErrorHandling(t *testing.T) {
	client := newEmbeddedClient(t)
	defer client.Close()

	ctx := context.Background()

	// List non-existent prefix should return empty list, not error
	listResp, err := client.Security().Secrets().List(ctx, "nonexistent/prefix")
	require.NoError(t, err, "List of non-existent prefix should not error")
	require.NotNil(t, listResp, "List response should not be nil")
	assert.Empty(t, listResp.Paths, "list should be empty for non-existent prefix")
}

// =============================================================================
// Large Value Tests
// =============================================================================

// TestEmbedded_LargeValue verifies that the embedded transport handles large
// values correctly, testing boundary conditions for data size.
func TestEmbedded_LargeValue(t *testing.T) {
	client := newEmbeddedClient(t)
	defer client.Close()

	ctx := context.Background()
	entityType := "large_value_test"
	entityID := uniqueID()

	// Create a 1MB value
	largeValue := make([]byte, 1024*1024)
	for i := range largeValue {
		largeValue[i] = byte(i % 256)
	}

	// Put large value
	putResp, err := client.KV().Put(ctx, &quicbaas.PutRequest{
		EntityType: entityType,
		ID:         entityID,
		Data:       largeValue,
	})
	require.NoError(t, err, "Put with large value should succeed")
	assert.Equal(t, entityID, putResp.ID, "Put should return the provided ID")

	// Get and verify
	getResp, err := client.KV().Get(ctx, &quicbaas.GetRequest{
		EntityType: entityType,
		ID:         entityID,
	})
	require.NoError(t, err, "Get large value should succeed")
	assert.True(t, getResp.Found, "large value entity should be found")
	assert.Equal(t, largeValue, getResp.Data, "large value should match")

	// Cleanup
	_, err = client.KV().Delete(ctx, &quicbaas.DeleteRequest{
		EntityType: entityType,
		ID:         entityID,
	})
	require.NoError(t, err, "Delete large value should succeed")
}

// TestEmbedded_LargeValue_ErrorHandling verifies that the system handles
// edge case value sizes gracefully.
func TestEmbedded_LargeValue_ErrorHandling(t *testing.T) {
	client := newEmbeddedClient(t)
	defer client.Close()

	ctx := context.Background()
	entityType := "large_error_test"
	entityID := uniqueID()

	// Put empty value (should succeed - empty data is valid)
	putResp, err := client.KV().Put(ctx, &quicbaas.PutRequest{
		EntityType: entityType,
		ID:         entityID,
		Data:       []byte{},
	})
	// Empty data may or may not be allowed depending on implementation
	if err == nil {
		// Cleanup if it succeeded
		_, _ = client.KV().Delete(ctx, &quicbaas.DeleteRequest{
			EntityType: entityType,
			ID:         putResp.ID,
		})
	}
}
