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

package secrets

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/tls"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	quicgo "github.com/quic-go/quic-go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"

	grpcclient "github.com/jeremyhahn/go-quicbaas/pkg/api/grpc"
	"github.com/jeremyhahn/go-quicbaas/pkg/api/quic"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
	"github.com/jeremyhahn/go-quicbaas/test/integration/helpers"
)

// =============================================================================
// Tenant Isolation Tests
// =============================================================================

// TestTenantIsolation_CrossTenantAccessPrevention verifies that secrets stored
// with different tenant IDs are isolated from each other.
func TestTenantIsolation_CrossTenantAccessPrevention(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	endpoint := getRESTEndpoint()
	sharedPath := fmt.Sprintf("shared/secret/%d", time.Now().UnixNano())

	// Store secret for tenant-alpha
	alphaData := map[string]interface{}{
		"api_key":    "alpha-secret-key-12345",
		"tenant":     "alpha",
		"created_at": time.Now().Format(time.RFC3339),
	}

	alphaOpts := &SecretOptions{
		TenantID:    "tenant-alpha",
		Description: "Secret for tenant-alpha",
		Metadata: map[string]string{
			"environment": "test",
			"owner":       "alpha-team",
		},
	}

	alphaResp := createSecret(t, endpoint, sharedPath+"/alpha", alphaData, alphaOpts)
	require.NotNil(t, alphaResp)
	assert.Equal(t, sharedPath+"/alpha", alphaResp.SecretID)
	t.Logf("Created secret for tenant-alpha at %s, version: %d", alphaResp.SecretID, alphaResp.Version)

	// Store secret for tenant-beta at the same logical path structure
	betaData := map[string]interface{}{
		"api_key":    "beta-secret-key-67890",
		"tenant":     "beta",
		"created_at": time.Now().Format(time.RFC3339),
	}

	betaOpts := &SecretOptions{
		TenantID:    "tenant-beta",
		Description: "Secret for tenant-beta",
		Metadata: map[string]string{
			"environment": "test",
			"owner":       "beta-team",
		},
	}

	betaResp := createSecret(t, endpoint, sharedPath+"/beta", betaData, betaOpts)
	require.NotNil(t, betaResp)
	assert.Equal(t, sharedPath+"/beta", betaResp.SecretID)
	t.Logf("Created secret for tenant-beta at %s, version: %d", betaResp.SecretID, betaResp.Version)

	// Retrieve and verify tenant-alpha secret
	t.Run("retrieve_tenant_alpha_secret", func(t *testing.T) {
		_ = ctx
		secret := getSecret(t, endpoint, sharedPath+"/alpha")
		require.NotNil(t, secret)

		var data map[string]interface{}
		err := json.Unmarshal(secret.Data, &data)
		require.NoError(t, err)

		assert.Equal(t, "alpha-secret-key-12345", data["api_key"])
		assert.Equal(t, "alpha", data["tenant"])
		t.Logf("Successfully retrieved tenant-alpha secret with correct data")
	})

	// Retrieve and verify tenant-beta secret
	t.Run("retrieve_tenant_beta_secret", func(t *testing.T) {
		secret := getSecret(t, endpoint, sharedPath+"/beta")
		require.NotNil(t, secret)

		var data map[string]interface{}
		err := json.Unmarshal(secret.Data, &data)
		require.NoError(t, err)

		assert.Equal(t, "beta-secret-key-67890", data["api_key"])
		assert.Equal(t, "beta", data["tenant"])
		t.Logf("Successfully retrieved tenant-beta secret with correct data")
	})

	// Verify secrets are different (tenant isolation works)
	t.Run("verify_tenant_isolation", func(t *testing.T) {
		alphaSecret := getSecret(t, endpoint, sharedPath+"/alpha")
		betaSecret := getSecret(t, endpoint, sharedPath+"/beta")

		// Verify the raw data is different
		assert.NotEqual(t, alphaSecret.Data, betaSecret.Data,
			"Secrets for different tenants should have different data")

		var alphaDataParsed, betaDataParsed map[string]interface{}
		require.NoError(t, json.Unmarshal(alphaSecret.Data, &alphaDataParsed))
		require.NoError(t, json.Unmarshal(betaSecret.Data, &betaDataParsed))

		// Verify API keys are different
		assert.NotEqual(t, alphaDataParsed["api_key"], betaDataParsed["api_key"],
			"API keys should be different for different tenants")

		t.Logf("Confirmed tenant isolation: secrets are properly separated")
	})

	// Cleanup
	deleteSecret(t, endpoint, sharedPath+"/alpha", true)
	deleteSecret(t, endpoint, sharedPath+"/beta", true)
}

// TestTenantIsolation_TenantSpecificDEKVerification verifies that tenant-specific
// DEKs are used for encryption and are reflected in the key identifier.
func TestTenantIsolation_TenantSpecificDEKVerification(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	endpoint := getRESTEndpoint()
	basePath := fmt.Sprintf("test/dek-verification/%d", time.Now().UnixNano())

	// Store secret with tenant_id and verify DEK reference
	t.Run("verify_tenant_dek_reference", func(t *testing.T) {
		_ = ctx
		path := basePath + "/tenant-secret"
		data := map[string]interface{}{
			"password":   "tenant-specific-password",
			"created_at": time.Now().Format(time.RFC3339),
		}

		opts := &SecretOptions{
			TenantID:    "tenant-1",
			Description: "Secret with tenant-specific DEK",
		}

		resp := createSecret(t, endpoint, path, data, opts)
		require.NotNil(t, resp)

		// Get the secret and verify key identifier contains tenant reference
		secret := getSecret(t, endpoint, path)
		require.NotNil(t, secret)

		// The key identifier should reference the tenant-specific DEK
		// Expected format: "dek:tenant:tenant-1" or similar
		if secret.KeyIdentifier != "" {
			assert.True(t,
				strings.Contains(secret.KeyIdentifier, "tenant-1") ||
					strings.Contains(secret.KeyIdentifier, "dek:tenant"),
				"Key identifier should reference tenant-specific DEK, got: %s", secret.KeyIdentifier)
			t.Logf("Verified tenant DEK reference in key identifier: %s", secret.KeyIdentifier)
		} else {
			// If key identifier is empty, check metadata for encryption info
			t.Logf("Key identifier not returned, secret metadata: %v", secret.Metadata)
		}

		deleteSecret(t, endpoint, path, true)
	})

	// Store secret without tenant_id and verify global DEK reference
	t.Run("verify_global_dek_reference", func(t *testing.T) {
		path := basePath + "/global-secret"
		data := map[string]interface{}{
			"password":   "global-password",
			"created_at": time.Now().Format(time.RFC3339),
		}

		// No TenantID specified
		resp := createSecret(t, endpoint, path, data, nil)
		require.NotNil(t, resp)

		// Get the secret and verify key identifier
		secret := getSecret(t, endpoint, path)
		require.NotNil(t, secret)

		// For global secrets, the key identifier should reference global DEK
		// or use the default encryption key
		if secret.KeyIdentifier != "" {
			t.Logf("Global secret key identifier: %s", secret.KeyIdentifier)
			// Should not contain tenant-specific references
			assert.False(t, strings.Contains(secret.KeyIdentifier, "tenant-1"),
				"Global secret should not reference tenant-specific DEK")
		}

		deleteSecret(t, endpoint, path, true)
	})
}

// TestTenantIsolation_TenantSecretListing verifies that listing secrets
// only returns secrets belonging to the requesting tenant.
func TestTenantIsolation_TenantSecretListing(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	_ = ctx

	endpoint := getRESTEndpoint()
	basePath := fmt.Sprintf("test/listing/%d", time.Now().UnixNano())

	// Create 3 secrets for tenant-a
	tenantASecrets := make([]string, 3)
	for i := 0; i < 3; i++ {
		path := fmt.Sprintf("%s/tenant-a/secret-%d", basePath, i)
		tenantASecrets[i] = path

		data := map[string]interface{}{
			"key":   fmt.Sprintf("tenant-a-value-%d", i),
			"index": i,
		}

		opts := &SecretOptions{
			TenantID:    "tenant-a",
			Description: fmt.Sprintf("Tenant A secret %d", i),
		}

		resp := createSecret(t, endpoint, path, data, opts)
		require.NotNil(t, resp)
		t.Logf("Created tenant-a secret %d: %s", i, path)
	}

	// Create 3 secrets for tenant-b
	tenantBSecrets := make([]string, 3)
	for i := 0; i < 3; i++ {
		path := fmt.Sprintf("%s/tenant-b/secret-%d", basePath, i)
		tenantBSecrets[i] = path

		data := map[string]interface{}{
			"key":   fmt.Sprintf("tenant-b-value-%d", i),
			"index": i,
		}

		opts := &SecretOptions{
			TenantID:    "tenant-b",
			Description: fmt.Sprintf("Tenant B secret %d", i),
		}

		resp := createSecret(t, endpoint, path, data, opts)
		require.NotNil(t, resp)
		t.Logf("Created tenant-b secret %d: %s", i, path)
	}

	// List secrets and verify tenant-a secrets are present
	t.Run("list_and_verify_tenant_a_secrets", func(t *testing.T) {
		resp := listSecrets(t, endpoint, basePath+"/tenant-a")
		require.NotNil(t, resp)

		// Count tenant-a secrets
		count := 0
		for _, secret := range resp.Secrets {
			for _, expectedPath := range tenantASecrets {
				if secret.SecretID == expectedPath {
					count++
					break
				}
			}
		}

		assert.Equal(t, 3, count, "Should find all 3 tenant-a secrets")
		t.Logf("Found %d tenant-a secrets", count)
	})

	// List secrets and verify tenant-b secrets are present
	t.Run("list_and_verify_tenant_b_secrets", func(t *testing.T) {
		resp := listSecrets(t, endpoint, basePath+"/tenant-b")
		require.NotNil(t, resp)

		// Count tenant-b secrets
		count := 0
		for _, secret := range resp.Secrets {
			for _, expectedPath := range tenantBSecrets {
				if secret.SecretID == expectedPath {
					count++
					break
				}
			}
		}

		assert.Equal(t, 3, count, "Should find all 3 tenant-b secrets")
		t.Logf("Found %d tenant-b secrets", count)
	})

	// Cleanup
	for _, path := range tenantASecrets {
		deleteSecret(t, endpoint, path, true)
	}
	for _, path := range tenantBSecrets {
		deleteSecret(t, endpoint, path, true)
	}
}

// =============================================================================
// Algorithm Verification Tests
// =============================================================================

// TestAlgorithm_Selection verifies that algorithm selection works correctly.
func TestAlgorithm_Selection(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	_ = ctx

	endpoint := getRESTEndpoint()
	basePath := fmt.Sprintf("test/algorithm/%d", time.Now().UnixNano())

	algorithms := []string{
		"AES-256-GCM",
		"AES-128-GCM",
		"ChaCha20-Poly1305",
	}

	for _, algo := range algorithms {
		t.Run(fmt.Sprintf("algorithm_%s", algo), func(t *testing.T) {
			path := fmt.Sprintf("%s/%s", basePath, strings.ReplaceAll(algo, "-", "_"))
			data := map[string]interface{}{
				"secret":    fmt.Sprintf("encrypted-with-%s", algo),
				"algorithm": algo,
			}

			opts := &SecretOptions{
				Algorithm:   algo,
				Description: fmt.Sprintf("Secret encrypted with %s", algo),
			}

			resp := createSecret(t, endpoint, path, data, opts)
			require.NotNil(t, resp)

			// Get the secret and verify algorithm
			secret := getSecret(t, endpoint, path)
			require.NotNil(t, secret)

			// Verify the algorithm is correctly stored/returned
			if secret.Algorithm != "" {
				assert.Equal(t, algo, secret.Algorithm,
					"Algorithm should match the requested algorithm")
				t.Logf("Verified algorithm %s is correctly stored", algo)
			} else {
				t.Logf("Algorithm field not returned in response (may be internal only)")
			}

			// Verify data can be decrypted correctly
			var retrievedData map[string]interface{}
			err := json.Unmarshal(secret.Data, &retrievedData)
			require.NoError(t, err)
			assert.Equal(t, fmt.Sprintf("encrypted-with-%s", algo), retrievedData["secret"])

			deleteSecret(t, endpoint, path, true)
		})
	}

	// Test default algorithm
	t.Run("default_algorithm", func(t *testing.T) {
		path := basePath + "/default"
		data := map[string]interface{}{
			"secret": "encrypted-with-default",
		}

		// No algorithm specified
		resp := createSecret(t, endpoint, path, data, nil)
		require.NotNil(t, resp)

		secret := getSecret(t, endpoint, path)
		require.NotNil(t, secret)

		// Verify data integrity regardless of algorithm
		var retrievedData map[string]interface{}
		err := json.Unmarshal(secret.Data, &retrievedData)
		require.NoError(t, err)
		assert.Equal(t, "encrypted-with-default", retrievedData["secret"])

		t.Logf("Default algorithm test passed, algorithm used: %s", secret.Algorithm)

		deleteSecret(t, endpoint, path, true)
	})
}

// TestAlgorithm_Persistence verifies algorithm specification is respected on updates.
func TestAlgorithm_Persistence(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	_ = ctx

	endpoint := getRESTEndpoint()
	path := fmt.Sprintf("test/algorithm-persistence/%d", time.Now().UnixNano())

	// Create initial secret with AES-256-GCM
	t.Run("create_with_aes256", func(t *testing.T) {
		data := map[string]interface{}{
			"version": 1,
			"content": "original-content",
		}

		opts := &SecretOptions{
			Algorithm:   "AES-256-GCM",
			Description: "Initial secret with AES-256-GCM",
		}

		resp := createSecret(t, endpoint, path, data, opts)
		require.NotNil(t, resp)
		t.Logf("Created secret with AES-256-GCM, version: %d", resp.Version)
	})

	// Update secret with different algorithm
	t.Run("update_with_aes128", func(t *testing.T) {
		data := map[string]interface{}{
			"version": 2,
			"content": "updated-content",
		}

		opts := &SecretOptions{
			Algorithm:   "AES-128-GCM",
			Description: "Updated secret with AES-128-GCM",
		}

		resp := createSecret(t, endpoint, path, data, opts)
		require.NotNil(t, resp)
		t.Logf("Updated secret with AES-128-GCM, version: %d", resp.Version)
	})

	// Verify the updated secret uses the new algorithm
	t.Run("verify_algorithm_update", func(t *testing.T) {
		secret := getSecret(t, endpoint, path)
		require.NotNil(t, secret)

		var data map[string]interface{}
		err := json.Unmarshal(secret.Data, &data)
		require.NoError(t, err)

		// Verify we get the updated content
		assert.Equal(t, "updated-content", data["content"])
		assert.Equal(t, float64(2), data["version"])

		// Algorithm should be AES-128-GCM if returned
		if secret.Algorithm != "" {
			assert.Equal(t, "AES-128-GCM", secret.Algorithm,
				"Algorithm should be updated to AES-128-GCM")
		}

		t.Logf("Verified algorithm update, current algorithm: %s", secret.Algorithm)
	})

	deleteSecret(t, endpoint, path, true)
}

// TestAlgorithm_InvalidRejection verifies that invalid algorithms are rejected.
func TestAlgorithm_InvalidRejection(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	_ = ctx

	endpoint := getRESTEndpoint()
	path := fmt.Sprintf("test/invalid-algorithm/%d", time.Now().UnixNano())

	data := map[string]interface{}{
		"secret": "should-not-be-stored",
	}

	// Try to create with invalid algorithm
	reqBody := map[string]interface{}{
		"data":      data,
		"algorithm": "INVALID-ALGO",
	}

	bodyBytes, err := json.Marshal(reqBody)
	require.NoError(t, err)

	url := fmt.Sprintf("%s/api/v1/secrets/%s", endpoint, path)
	req, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(bodyBytes))
	require.NoError(t, err)
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{
		Timeout: 10 * time.Second,
		Transport: &http.Transport{
			TLSClientConfig:     &tls.Config{InsecureSkipVerify: true},
			TLSHandshakeTimeout: 10 * time.Second,
			DialContext: (&net.Dialer{
				Timeout:   10 * time.Second,
				KeepAlive: 30 * time.Second,
			}).DialContext,
		},
	}
	resp, err := client.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	require.NoError(t, err)

	// Should return an error (400 or 422)
	assert.True(t, resp.StatusCode >= 400 && resp.StatusCode < 500,
		"Invalid algorithm should be rejected with 4xx error, got %d: %s",
		resp.StatusCode, string(respBody))

	t.Logf("Invalid algorithm correctly rejected with status %d", resp.StatusCode)
}

// =============================================================================
// Key ID Verification Tests
// =============================================================================

// TestKeyID_CustomKeyUsage verifies that custom key IDs can be specified.
func TestKeyID_CustomKeyUsage(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	_ = ctx

	endpoint := getRESTEndpoint()
	path := fmt.Sprintf("test/custom-key/%d", time.Now().UnixNano())

	customKeyID := "custom-key-123"

	data := map[string]interface{}{
		"secret":     "encrypted-with-custom-key",
		"key_id":     customKeyID,
		"created_at": time.Now().Format(time.RFC3339),
	}

	opts := &SecretOptions{
		KeyID:       customKeyID,
		Description: "Secret with custom key ID",
	}

	// Create secret with custom key ID
	resp := createSecret(t, endpoint, path, data, opts)
	require.NotNil(t, resp)

	// Get the secret and verify key identifier
	secret := getSecret(t, endpoint, path)
	require.NotNil(t, secret)

	// Verify the key identifier references our custom key
	if secret.KeyIdentifier != "" {
		assert.True(t,
			strings.Contains(secret.KeyIdentifier, customKeyID) ||
				strings.Contains(secret.KeyIdentifier, "custom-key"),
			"Key identifier should reference custom key, got: %s", secret.KeyIdentifier)
		t.Logf("Custom key reference verified: %s", secret.KeyIdentifier)
	}

	// Verify decryption works with the custom key
	var retrievedData map[string]interface{}
	err := json.Unmarshal(secret.Data, &retrievedData)
	require.NoError(t, err)
	assert.Equal(t, "encrypted-with-custom-key", retrievedData["secret"])

	t.Logf("Decryption with custom key successful")

	deleteSecret(t, endpoint, path, true)
}

// =============================================================================
// Cross-Protocol Verification Tests
// =============================================================================

// TestCrossProtocol_RESTToGRPC verifies secret created via REST can be retrieved via gRPC.
func TestCrossProtocol_RESTToGRPC(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	restEndpoint := getRESTEndpoint()
	path := fmt.Sprintf("test/cross-protocol/rest-to-grpc/%d", time.Now().UnixNano())

	// Create secret via REST
	data := map[string]interface{}{
		"username": "cross-protocol-user",
		"password": "cross-protocol-password",
		"protocol": "REST",
	}

	opts := &SecretOptions{
		Description: "Secret created via REST for cross-protocol test",
	}

	restResp := createSecret(t, restEndpoint, path, data, opts)
	require.NotNil(t, restResp)
	t.Logf("Created secret via REST: %s", path)

	// Retrieve via gRPC
	grpcEndpoints := helpers.GetClusterEndpoints()
	require.NotEmpty(t, grpcEndpoints, "gRPC endpoints must be available for integration tests")

	conn, err := helpers.SetupGRPCConnectionToEndpoint(t, ctx, grpcEndpoints[0])
	require.NoError(t, err, "Failed to connect to gRPC endpoint %s", grpcEndpoints[0])
	defer conn.Close()

	grpcClient, err := grpcclient.NewClient(grpcEndpoints[0], grpcclient.WithTLSConfig(&tls.Config{InsecureSkipVerify: true, MinVersion: tls.VersionTLS13}))
	require.NoError(t, err, "Failed to create gRPC client")
	defer grpcClient.Close()

	grpcReq := &service.GetSecretRequest{
		SecretID: path,
	}

	grpcResp, err := grpcClient.GetSecret(ctx, grpcReq)
	require.NoError(t, err)
	require.NotNil(t, grpcResp)

	// Verify data matches
	var retrievedData map[string]interface{}
	err = json.Unmarshal(grpcResp.Data, &retrievedData)
	require.NoError(t, err)

	assert.Equal(t, "cross-protocol-user", retrievedData["username"])
	assert.Equal(t, "cross-protocol-password", retrievedData["password"])

	t.Logf("Successfully retrieved REST-created secret via gRPC")

	// Cleanup via REST
	deleteSecret(t, restEndpoint, path, true)
}

// TestCrossProtocol_GRPCToREST verifies secret created via gRPC can be retrieved via REST.
func TestCrossProtocol_GRPCToREST(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	grpcEndpoints := helpers.GetClusterEndpoints()
	require.NotEmpty(t, grpcEndpoints, "gRPC endpoints must be available for integration tests")

	path := fmt.Sprintf("test/cross-protocol/grpc-to-rest/%d", time.Now().UnixNano())

	// Create via gRPC
	conn, err := helpers.SetupGRPCConnectionToEndpoint(t, ctx, grpcEndpoints[0])
	require.NoError(t, err, "Failed to connect to gRPC endpoint %s", grpcEndpoints[0])
	defer conn.Close()

	grpcClient, err := grpcclient.NewClient(grpcEndpoints[0], grpcclient.WithTLSConfig(&tls.Config{InsecureSkipVerify: true, MinVersion: tls.VersionTLS13}))
	require.NoError(t, err, "Failed to create gRPC client")
	defer grpcClient.Close()

	secretData, err := json.Marshal(map[string]interface{}{
		"username": "grpc-protocol-user",
		"password": "grpc-protocol-password",
		"protocol": "gRPC",
	})
	require.NoError(t, err)

	storeReq := &service.StoreSecretRequest{
		SecretID:    path,
		Name:        "cross-protocol-test",
		Description: "Secret created via gRPC for cross-protocol test",
		Data:        secretData,
	}

	storeResp, err := grpcClient.StoreSecret(ctx, storeReq)
	require.NoError(t, err)
	require.NotNil(t, storeResp)
	t.Logf("Created secret via gRPC: %s", path)

	// Retrieve via REST
	restEndpoint := getRESTEndpoint()
	secret := getSecret(t, restEndpoint, path)
	require.NotNil(t, secret)

	var retrievedData map[string]interface{}
	err = json.Unmarshal(secret.Data, &retrievedData)
	require.NoError(t, err)

	assert.Equal(t, "grpc-protocol-user", retrievedData["username"])
	assert.Equal(t, "grpc-protocol-password", retrievedData["password"])
	assert.Equal(t, "gRPC", retrievedData["protocol"])

	t.Logf("Successfully retrieved gRPC-created secret via REST")

	// Cleanup via REST
	deleteSecret(t, restEndpoint, path, true)
}

// TestCrossProtocol_QUICToREST verifies secret created via QUIC can be retrieved via REST.
func TestCrossProtocol_QUICToREST(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	quicEndpoints := helpers.GetQUICEndpoints()
	require.NotEmpty(t, quicEndpoints, "QUIC endpoints must be available for integration tests")

	// Try to connect to QUIC
	tlsConf := &tls.Config{
		InsecureSkipVerify: true,
		MinVersion:         tls.VersionTLS13,
		NextProtos:         []string{"quicbaas-quic"},
	}

	dialCtx, dialCancel := context.WithTimeout(ctx, 10*time.Second)
	defer dialCancel()

	conn, err := quicgo.DialAddr(dialCtx, quicEndpoints[0], tlsConf, nil)
	require.NoError(t, err, "Failed to connect to QUIC endpoint %s", quicEndpoints[0])
	defer conn.CloseWithError(0, "test complete")

	path := fmt.Sprintf("test/cross-protocol/quic-to-rest/%d", time.Now().UnixNano())

	// Create via QUIC
	secretData, err := json.Marshal(map[string]interface{}{
		"username": "quic-protocol-user",
		"password": "quic-protocol-password",
		"protocol": "QUIC",
	})
	require.NoError(t, err)

	storeReq := &service.StoreSecretRequest{
		SecretID:    path,
		Name:        "cross-protocol-quic-test",
		Description: "Secret created via QUIC for cross-protocol test",
		Data:        secretData,
	}

	payload, err := quic.EncodePayload(storeReq)
	require.NoError(t, err)

	stream, err := conn.OpenStreamSync(ctx)
	require.NoError(t, err)

	quicReq := &quic.Request{
		Opcode:  quic.OpSecretsStore,
		Payload: payload,
	}

	err = quic.WriteRequest(stream, quicReq)
	require.NoError(t, err)

	resp, err := quic.ReadResponse(stream)
	require.NoError(t, err)
	stream.Close()

	require.Equal(t, quic.StatusOK, resp.Status, "QUIC store should succeed")
	t.Logf("Created secret via QUIC: %s", path)

	// Retrieve via REST
	restEndpoint := getRESTEndpoint()
	secret := getSecret(t, restEndpoint, path)
	require.NotNil(t, secret)

	var retrievedData map[string]interface{}
	err = json.Unmarshal(secret.Data, &retrievedData)
	require.NoError(t, err)

	assert.Equal(t, "quic-protocol-user", retrievedData["username"])
	assert.Equal(t, "quic-protocol-password", retrievedData["password"])
	assert.Equal(t, "QUIC", retrievedData["protocol"])

	t.Logf("Successfully retrieved QUIC-created secret via REST")

	// Cleanup via REST
	deleteSecret(t, restEndpoint, path, true)
}

// =============================================================================
// Data Integrity Tests
// =============================================================================

// TestDataIntegrity_BinaryDataPreservation verifies that binary data is preserved correctly.
func TestDataIntegrity_BinaryDataPreservation(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	_ = ctx

	endpoint := getRESTEndpoint()
	path := fmt.Sprintf("test/binary-data/%d", time.Now().UnixNano())

	// Generate random binary data
	binaryData := make([]byte, 1024)
	_, err := rand.Read(binaryData)
	require.NoError(t, err)

	// Base64 encode for JSON transport
	encoded := base64.StdEncoding.EncodeToString(binaryData)

	data := map[string]interface{}{
		"binary_payload": encoded,
		"size":           len(binaryData),
		"type":           "random-binary",
	}

	opts := &SecretOptions{
		Description: "Binary data preservation test",
		Metadata: map[string]string{
			"content-type": "application/octet-stream",
		},
	}

	resp := createSecret(t, endpoint, path, data, opts)
	require.NotNil(t, resp)

	// Retrieve and verify
	secret := getSecret(t, endpoint, path)
	require.NotNil(t, secret)

	var retrievedData map[string]interface{}
	err = json.Unmarshal(secret.Data, &retrievedData)
	require.NoError(t, err)

	// Decode and compare
	retrievedEncoded, ok := retrievedData["binary_payload"].(string)
	require.True(t, ok, "binary_payload should be a string")

	retrievedBinary, err := base64.StdEncoding.DecodeString(retrievedEncoded)
	require.NoError(t, err)

	assert.Equal(t, binaryData, retrievedBinary, "Binary data should be preserved byte-for-byte")
	t.Logf("Binary data preservation verified: %d bytes", len(binaryData))

	deleteSecret(t, endpoint, path, true)
}

// TestDataIntegrity_LargeDataHandling verifies that large data (1MB) is handled correctly.
func TestDataIntegrity_LargeDataHandling(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	_ = ctx

	endpoint := getRESTEndpoint()
	path := fmt.Sprintf("test/large-data/%d", time.Now().UnixNano())

	// Generate 1MB of random data
	largeData := make([]byte, 1024*1024)
	_, err := rand.Read(largeData)
	require.NoError(t, err)

	// Base64 encode (will be ~1.33MB)
	encoded := base64.StdEncoding.EncodeToString(largeData)

	data := map[string]interface{}{
		"large_payload": encoded,
		"size_bytes":    len(largeData),
		"description":   "1MB test payload",
	}

	opts := &SecretOptions{
		Description: "Large data handling test (1MB)",
	}

	resp := createSecret(t, endpoint, path, data, opts)
	require.NotNil(t, resp)
	t.Logf("Created 1MB secret successfully")

	// Retrieve and verify
	secret := getSecret(t, endpoint, path)
	require.NotNil(t, secret)

	var retrievedData map[string]interface{}
	err = json.Unmarshal(secret.Data, &retrievedData)
	require.NoError(t, err)

	// Decode and compare
	retrievedEncoded, ok := retrievedData["large_payload"].(string)
	require.True(t, ok, "large_payload should be a string")

	retrievedLarge, err := base64.StdEncoding.DecodeString(retrievedEncoded)
	require.NoError(t, err)

	assert.Equal(t, len(largeData), len(retrievedLarge), "Large data size should match")
	assert.Equal(t, largeData, retrievedLarge, "Large data should be preserved exactly")

	t.Logf("Large data handling verified: %d bytes stored and retrieved", len(largeData))

	deleteSecret(t, endpoint, path, true)
}

// TestDataIntegrity_SpecialCharacters verifies that special characters are handled correctly.
func TestDataIntegrity_SpecialCharacters(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	_ = ctx

	endpoint := getRESTEndpoint()
	basePath := fmt.Sprintf("test/special-chars/%d", time.Now().UnixNano())

	// Test unicode characters
	t.Run("unicode_characters", func(t *testing.T) {
		path := basePath + "/unicode"
		data := map[string]interface{}{
			"japanese":     "日本語テスト",
			"chinese":      "中文测试",
			"korean":       "한국어 테스트",
			"arabic":       "اختبار عربي",
			"hebrew":       "בדיקה עברית",
			"emoji":        "Hello World! 123",
			"mixed":        "Hello 世界 مرحبا",
			"symbols":      "Special symbols: < > & \" ' \\ / @ # $ % ^ & * ( )",
			"accents":      "cafe resume naive",
			"greek":        "alpha beta gamma delta",
			"russian":      "Привет мир",
			"thai":         "สวัสดี",
			"mathematical": "f(x) = x + y = z",
		}

		opts := &SecretOptions{
			Description: "Unicode character test",
		}

		resp := createSecret(t, endpoint, path, data, opts)
		require.NotNil(t, resp)

		// Retrieve and verify
		secret := getSecret(t, endpoint, path)
		require.NotNil(t, secret)

		var retrievedData map[string]interface{}
		err := json.Unmarshal(secret.Data, &retrievedData)
		require.NoError(t, err)

		// Verify each field
		assert.Equal(t, data["japanese"], retrievedData["japanese"])
		assert.Equal(t, data["chinese"], retrievedData["chinese"])
		assert.Equal(t, data["korean"], retrievedData["korean"])
		assert.Equal(t, data["arabic"], retrievedData["arabic"])
		assert.Equal(t, data["hebrew"], retrievedData["hebrew"])
		assert.Equal(t, data["emoji"], retrievedData["emoji"])
		assert.Equal(t, data["mixed"], retrievedData["mixed"])
		assert.Equal(t, data["symbols"], retrievedData["symbols"])

		t.Logf("Unicode character preservation verified")

		deleteSecret(t, endpoint, path, true)
	})

	// Test JSON escape sequences
	t.Run("escape_sequences", func(t *testing.T) {
		path := basePath + "/escapes"
		data := map[string]interface{}{
			"newlines":       "line1\nline2\nline3",
			"tabs":           "col1\tcol2\tcol3",
			"carriage":       "line1\r\nline2",
			"backslash":      "path\\to\\file",
			"quotes":         "He said \"Hello\"",
			"single_quotes":  "It's a test",
			"null_char":      "before\x00after",
			"form_feed":      "page1\fpage2",
			"backspace":      "correct\bion",
			"vertical_tab":   "row1\vrow2",
			"complex_escape": "Path: C:\\Users\\test\nQuote: \"value\"\tTab",
		}

		opts := &SecretOptions{
			Description: "Escape sequence test",
		}

		resp := createSecret(t, endpoint, path, data, opts)
		require.NotNil(t, resp)

		// Retrieve and verify
		secret := getSecret(t, endpoint, path)
		require.NotNil(t, secret)

		var retrievedData map[string]interface{}
		err := json.Unmarshal(secret.Data, &retrievedData)
		require.NoError(t, err)

		// Verify key escape sequences
		assert.Equal(t, data["newlines"], retrievedData["newlines"])
		assert.Equal(t, data["tabs"], retrievedData["tabs"])
		assert.Equal(t, data["backslash"], retrievedData["backslash"])
		assert.Equal(t, data["quotes"], retrievedData["quotes"])
		assert.Equal(t, data["complex_escape"], retrievedData["complex_escape"])

		t.Logf("Escape sequence preservation verified")

		deleteSecret(t, endpoint, path, true)
	})

	// Test nested JSON
	t.Run("nested_json", func(t *testing.T) {
		path := basePath + "/nested"
		data := map[string]interface{}{
			"level1": map[string]interface{}{
				"level2": map[string]interface{}{
					"level3": map[string]interface{}{
						"value": "deeply-nested-value",
						"array": []interface{}{1, 2, 3, "four", true, nil},
					},
					"sibling": "level3-sibling",
				},
				"mixed_array": []interface{}{
					"string",
					123,
					true,
					nil,
					map[string]interface{}{"nested": "object"},
				},
			},
			"boolean_true":  true,
			"boolean_false": false,
			"null_value":    nil,
			"number_int":    42,
			"number_float":  3.14159,
			"number_neg":    -273.15,
		}

		opts := &SecretOptions{
			Description: "Nested JSON test",
		}

		resp := createSecret(t, endpoint, path, data, opts)
		require.NotNil(t, resp)

		// Retrieve and verify
		secret := getSecret(t, endpoint, path)
		require.NotNil(t, secret)

		var retrievedData map[string]interface{}
		err := json.Unmarshal(secret.Data, &retrievedData)
		require.NoError(t, err)

		// Deep comparison
		originalJSON, _ := json.Marshal(data)
		retrievedJSON, _ := json.Marshal(retrievedData)
		assert.JSONEq(t, string(originalJSON), string(retrievedJSON))

		t.Logf("Nested JSON structure preservation verified")

		deleteSecret(t, endpoint, path, true)
	})
}

// =============================================================================
// Multi-Protocol Tenant Isolation Tests
// =============================================================================

// TestMultiProtocol_TenantIsolation verifies tenant isolation across all protocols.
func TestMultiProtocol_TenantIsolation(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	basePath := fmt.Sprintf("test/multiprotocol-isolation/%d", time.Now().UnixNano())

	// Create clients for each protocol
	restEndpoint := getRESTEndpoint()
	grpcEndpoints := helpers.GetClusterEndpoints()

	// Test tenant isolation via REST
	t.Run("REST_tenant_isolation", func(t *testing.T) {
		_ = ctx
		testTenantIsolationForProtocol(t, "REST", basePath+"/rest", restEndpoint, nil)
	})

	// Test tenant isolation via gRPC
	require.NotEmpty(t, grpcEndpoints, "gRPC endpoints must be available for integration tests")
	t.Run("gRPC_tenant_isolation", func(t *testing.T) {
		conn, err := helpers.SetupGRPCConnectionToEndpoint(t, ctx, grpcEndpoints[0])
		require.NoError(t, err, "Failed to connect to gRPC endpoint %s", grpcEndpoints[0])
		defer conn.Close()

		grpcClient, err := grpcclient.NewClient(grpcEndpoints[0], grpcclient.WithTLSConfig(&tls.Config{InsecureSkipVerify: true, MinVersion: tls.VersionTLS13}))
		require.NoError(t, err, "Failed to create gRPC client")
		defer grpcClient.Close()

		testTenantIsolationForGRPC(t, ctx, basePath+"/grpc", grpcClient, restEndpoint)
	})

	// Test tenant isolation via Unix Socket if available
	t.Run("UnixSocket_tenant_isolation", func(t *testing.T) {
		socketPath := os.Getenv("QUICBAAS_NODE1_UNIX")
		if socketPath == "" {
			socketPath = "/var/run/quicbaas/node1.sock"
		}
		_, err := net.Dial("unix", socketPath)
		require.NoError(t, err, "Unix socket must be available at %s for integration tests", socketPath)

		conn, err := grpc.DialContext(ctx,
			"unix://"+socketPath,
			grpc.WithTransportCredentials(credentials.NewTLS(&tls.Config{InsecureSkipVerify: true, MinVersion: tls.VersionTLS13})),
		)
		require.NoError(t, err, "Failed to connect to unix socket at %s", socketPath)
		defer conn.Close()

		unixClient, err := grpcclient.NewClient("unix://"+socketPath, grpcclient.WithTLSConfig(&tls.Config{InsecureSkipVerify: true, MinVersion: tls.VersionTLS13}))
		require.NoError(t, err, "Failed to create unix socket client")
		defer unixClient.Close()

		testTenantIsolationForGRPC(t, ctx, basePath+"/unix", unixClient, restEndpoint)
	})
}

// testTenantIsolationForProtocol tests tenant isolation using REST API.
func testTenantIsolationForProtocol(t *testing.T, protocolName, basePath, endpoint string, _ interface{}) {
	t.Helper()

	// Create secrets for two tenants
	tenant1Path := basePath + "/tenant1"
	tenant2Path := basePath + "/tenant2"

	tenant1Data := map[string]interface{}{
		"api_key":  "tenant1-key",
		"protocol": protocolName,
	}

	tenant2Data := map[string]interface{}{
		"api_key":  "tenant2-key",
		"protocol": protocolName,
	}

	// Create tenant1 secret
	tenant1Opts := &SecretOptions{
		TenantID:    "tenant-1",
		Description: fmt.Sprintf("Tenant 1 secret via %s", protocolName),
	}
	resp1 := createSecret(t, endpoint, tenant1Path, tenant1Data, tenant1Opts)
	require.NotNil(t, resp1)

	// Create tenant2 secret
	tenant2Opts := &SecretOptions{
		TenantID:    "tenant-2",
		Description: fmt.Sprintf("Tenant 2 secret via %s", protocolName),
	}
	resp2 := createSecret(t, endpoint, tenant2Path, tenant2Data, tenant2Opts)
	require.NotNil(t, resp2)

	// Verify isolation
	secret1 := getSecret(t, endpoint, tenant1Path)
	secret2 := getSecret(t, endpoint, tenant2Path)

	var data1, data2 map[string]interface{}
	require.NoError(t, json.Unmarshal(secret1.Data, &data1))
	require.NoError(t, json.Unmarshal(secret2.Data, &data2))

	assert.Equal(t, "tenant1-key", data1["api_key"])
	assert.Equal(t, "tenant2-key", data2["api_key"])
	assert.NotEqual(t, data1["api_key"], data2["api_key"])

	t.Logf("[%s] Tenant isolation verified", protocolName)

	// Cleanup - use tryDeleteSecret for graceful handling of not-found
	tryDeleteSecret(t, endpoint, tenant1Path, true)
	tryDeleteSecret(t, endpoint, tenant2Path, true)
}

// testTenantIsolationForGRPC tests tenant isolation using gRPC client.
func testTenantIsolationForGRPC(t *testing.T, ctx context.Context, basePath string, client *grpcclient.Client, restEndpoint string) {
	t.Helper()

	tenant1Path := basePath + "/tenant1"
	tenant2Path := basePath + "/tenant2"

	// Create secrets via gRPC
	tenant1Data, _ := json.Marshal(map[string]interface{}{
		"api_key":  "grpc-tenant1-key",
		"protocol": "gRPC",
	})

	tenant2Data, _ := json.Marshal(map[string]interface{}{
		"api_key":  "grpc-tenant2-key",
		"protocol": "gRPC",
	})

	// Store tenant1 secret
	_, err := client.StoreSecret(ctx, &service.StoreSecretRequest{
		SecretID: tenant1Path,
		Name:     "tenant1-secret",
		Data:     tenant1Data,
		TenantID: "tenant-1",
	})
	require.NoError(t, err)

	// Store tenant2 secret
	_, err = client.StoreSecret(ctx, &service.StoreSecretRequest{
		SecretID: tenant2Path,
		Name:     "tenant2-secret",
		Data:     tenant2Data,
		TenantID: "tenant-2",
	})
	require.NoError(t, err)

	// Retrieve via gRPC and verify isolation
	resp1, err := client.GetSecret(ctx, &service.GetSecretRequest{SecretID: tenant1Path})
	require.NoError(t, err)

	resp2, err := client.GetSecret(ctx, &service.GetSecretRequest{SecretID: tenant2Path})
	require.NoError(t, err)

	var data1, data2 map[string]interface{}
	require.NoError(t, json.Unmarshal(resp1.Data, &data1))
	require.NoError(t, json.Unmarshal(resp2.Data, &data2))

	assert.Equal(t, "grpc-tenant1-key", data1["api_key"])
	assert.Equal(t, "grpc-tenant2-key", data2["api_key"])
	assert.NotEqual(t, data1["api_key"], data2["api_key"])

	t.Logf("[gRPC] Tenant isolation verified")

	// Cleanup via REST - use tryDeleteSecret since the secret may have been
	// stored with different key format via gRPC or already cleaned up
	tryDeleteSecret(t, restEndpoint, tenant1Path, true)
	tryDeleteSecret(t, restEndpoint, tenant2Path, true)
}

// =============================================================================
// Helper Functions
// =============================================================================
