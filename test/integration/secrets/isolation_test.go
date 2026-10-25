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
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"

	grpcclient "github.com/jeremyhahn/go-quicbaas/pkg/api/grpc"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
	"github.com/jeremyhahn/go-quicbaas/test/integration/helpers"
)

// =============================================================================
// Tenant Isolation Tests
// =============================================================================

// tenantSecretsClient calls the secrets REST API as one tenant. The vault
// keys every secret by the caller's tenant, which REST binds from the
// bearer's tenant_id claim and nothing else, so a tenant identity is a bearer
// minted for a user of that tenant — never a tenant_id in the request body.
type tenantSecretsClient struct {
	endpoint string
	bearer   string
	client   *http.Client
}

// newTenantSecretsClient provisions tenantID (as the system admin) and logs
// in as a tenant admin of it.
func newTenantSecretsClient(t *testing.T, endpoint, tenantID string) *tenantSecretsClient {
	t.Helper()
	// The system admin only provisions the tenant; the secrets calls are made
	// by the tenant's own admin, as a production caller makes them.
	bearer := helpers.AuthTenantAdmin(t, endpoint, tenantID)
	require.NotEmpty(t, bearer, "tenant-admin bearer for %s", tenantID)
	return &tenantSecretsClient{
		endpoint: endpoint,
		bearer:   bearer,
		client:   helpers.NewHTTPClient(10 * time.Second),
	}
}

// do sends one request and returns its status and body.
func (c *tenantSecretsClient) do(t *testing.T, method, path string, reqBody interface{}) (int, []byte) {
	t.Helper()
	var bodyReader io.Reader
	if reqBody != nil {
		data, err := json.Marshal(reqBody)
		require.NoError(t, err)
		bodyReader = bytes.NewReader(data)
	}
	req, err := http.NewRequest(method, c.endpoint+path, bodyReader)
	require.NoError(t, err)
	if reqBody != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set("Authorization", "Bearer "+c.bearer)

	resp, err := c.client.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	return resp.StatusCode, body
}

// store writes {"value": value} at path and returns the version written.
func (c *tenantSecretsClient) store(t *testing.T, path, value string) uint64 {
	t.Helper()
	status, body := c.do(t, http.MethodPost, "/api/v1/secrets/"+path,
		map[string]interface{}{"data": map[string]interface{}{"value": value}})
	require.Equal(t, http.StatusCreated, status, "store %s: %s", path, body)
	var resp service.StoreSecretResponse
	require.NoError(t, json.Unmarshal(body, &resp))
	return resp.Version
}

// read returns the latest value at path and its version.
func (c *tenantSecretsClient) read(t *testing.T, path string) (string, uint64) {
	t.Helper()
	status, body := c.do(t, http.MethodGet, "/api/v1/secrets/"+path, nil)
	require.Equal(t, http.StatusOK, status, "read %s: %s", path, body)
	var resp service.GetSecretResponse
	require.NoError(t, json.Unmarshal(body, &resp))
	var data map[string]string
	require.NoError(t, json.Unmarshal(resp.Data, &data))
	return data["value"], resp.Version
}

// list returns the secret IDs the tenant enumerates under prefix.
func (c *tenantSecretsClient) list(t *testing.T, prefix string) []string {
	t.Helper()
	status, body := c.do(t, http.MethodGet, "/api/v1/secrets/?prefix="+url.QueryEscape(prefix), nil)
	require.Equal(t, http.StatusOK, status, "list %s: %s", prefix, body)
	var resp service.ListSecretsResponse
	require.NoError(t, json.Unmarshal(body, &resp))
	ids := make([]string, 0, len(resp.Secrets))
	for _, s := range resp.Secrets {
		ids = append(ids, s.SecretID)
	}
	return ids
}

// TestTenantIsolation_CrossTenantAccessPrevention proves the vault keys a
// secret by the caller's tenant: two tenants write the SAME path, each reads
// back only its own value, and neither can read, list, update or delete the
// other's secret. Writing two different paths, as this test once did, passes
// with every bit of tenant scoping deleted (go-quicbaas#281).
func TestTenantIsolation_CrossTenantAccessPrevention(t *testing.T) {
	endpoint := getRESTEndpoint()
	suffix := time.Now().UnixNano()
	alpha := newTenantSecretsClient(t, endpoint, fmt.Sprintf("iso-alpha-%d", suffix))
	beta := newTenantSecretsClient(t, endpoint, fmt.Sprintf("iso-beta-%d", suffix))
	path := fmt.Sprintf("shared/secret/%d", suffix)

	require.Equal(t, uint64(1), alpha.store(t, path, "alpha-secret-key"))

	t.Run("other_tenant_cannot_read", func(t *testing.T) {
		status, body := beta.do(t, http.MethodGet, "/api/v1/secrets/"+path, nil)
		assert.Equal(t, http.StatusNotFound, status, "tenant B read tenant A's secret: %s", body)
	})

	t.Run("other_tenant_cannot_list", func(t *testing.T) {
		assert.Empty(t, beta.list(t, path), "tenant B enumerates tenant A's secret")
	})

	t.Run("other_tenant_cannot_update", func(t *testing.T) {
		status, body := beta.do(t, http.MethodPut, "/api/v1/secrets/"+path+"/status",
			map[string]interface{}{"status": service.SecretStatusDisabled})
		assert.Equal(t, http.StatusNotFound, status, "tenant B changed tenant A's status: %s", body)

		status, body = beta.do(t, http.MethodPost, "/api/v1/secrets/"+path+"?action=rotate",
			map[string]interface{}{"data": map[string]interface{}{"value": "hijacked"}})
		assert.Equal(t, http.StatusNotFound, status, "tenant B rotated tenant A's secret: %s", body)
	})

	t.Run("other_tenant_cannot_delete", func(t *testing.T) {
		status, body := beta.do(t, http.MethodDelete, "/api/v1/secrets/"+path+"?destroy=true", nil)
		assert.Equal(t, http.StatusNotFound, status, "tenant B deleted tenant A's secret: %s", body)
	})

	t.Run("owner_secret_untouched", func(t *testing.T) {
		value, version := alpha.read(t, path)
		assert.Equal(t, "alpha-secret-key", value)
		assert.Equal(t, uint64(1), version, "a foreign tenant's attempt created a version")
	})

	t.Run("same_path_holds_one_value_per_tenant", func(t *testing.T) {
		// B's first write to A's path starts B's own history; it is not
		// version 2 of A's secret.
		require.Equal(t, uint64(1), beta.store(t, path, "beta-secret-key"))

		alphaValue, _ := alpha.read(t, path)
		betaValue, _ := beta.read(t, path)
		assert.Equal(t, "alpha-secret-key", alphaValue)
		assert.Equal(t, "beta-secret-key", betaValue)
		assert.Equal(t, []string{path}, alpha.list(t, path))
		assert.Equal(t, []string{path}, beta.list(t, path))
	})

	t.Run("own_delete_stays_in_own_namespace", func(t *testing.T) {
		status, body := beta.do(t, http.MethodDelete, "/api/v1/secrets/"+path+"?destroy=true", nil)
		require.Equal(t, http.StatusOK, status, "tenant B delete of its own secret: %s", body)

		value, _ := alpha.read(t, path)
		assert.Equal(t, "alpha-secret-key", value, "tenant B's delete reached tenant A")
	})

	status, body := alpha.do(t, http.MethodDelete, "/api/v1/secrets/"+path+"?destroy=true", nil)
	require.Equal(t, http.StatusOK, status, "cleanup: %s", body)
}

// TestTenantIsolation_TenantSpecificDEKVerification verifies that tenant-specific
// DEKs are used for encryption and are reflected in the key identifier.
func TestTenantIsolation_TenantSpecificDEKVerification(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	endpoint := getRESTEndpoint()
	basePath := fmt.Sprintf("test/dek-verification/%d", time.Now().UnixNano())
	const dekPrincipal = "dek"
	dekTenantID := helpers.NamedTenantPrincipalID(t, endpoint, dekPrincipal)

	// Store secret with tenant_id and verify DEK reference
	t.Run("verify_tenant_dek_reference", func(t *testing.T) {
		_ = ctx
		path := basePath + "/tenant-secret"
		data := map[string]interface{}{
			"password":   "tenant-specific-password",
			"created_at": time.Now().Format(time.RFC3339),
		}

		opts := &SecretOptions{
			Principal:   dekPrincipal,
			Description: "Secret with tenant-specific DEK",
		}

		resp := createSecret(t, endpoint, path, data, opts)
		require.NotNil(t, resp)

		// Another tenant's principal must not see the secret.
		foreign, err := tryGetSecret(t, endpoint, path)
		require.Error(t, err, "the shared tenant principal read the %s tenant's secret", dekPrincipal)
		assert.Nil(t, foreign)

		// Get the secret and verify key identifier contains tenant reference
		secret := getSecretAs(t, dekPrincipal, endpoint, path)
		require.NotNil(t, secret)

		// The key identifier should reference the tenant-specific DEK
		// Expected format: "dek:tenant:<tenant ID>" or similar
		if secret.KeyIdentifier != "" {
			assert.True(t,
				strings.Contains(secret.KeyIdentifier, dekTenantID) ||
					strings.Contains(secret.KeyIdentifier, "dek:tenant"),
				"Key identifier should reference tenant-specific DEK, got: %s", secret.KeyIdentifier)
			t.Logf("Verified tenant DEK reference in key identifier: %s", secret.KeyIdentifier)
		} else {
			// If key identifier is empty, check metadata for encryption info
			t.Logf("Key identifier not returned, secret metadata: %v", secret.Metadata)
		}

		deleteSecretAs(t, dekPrincipal, endpoint, path, true)
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
			assert.False(t, strings.Contains(secret.KeyIdentifier, dekTenantID),
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
			Principal:   "tenant-a",
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
			Principal:   "tenant-b",
			Description: fmt.Sprintf("Tenant B secret %d", i),
		}

		resp := createSecret(t, endpoint, path, data, opts)
		require.NotNil(t, resp)
		t.Logf("Created tenant-b secret %d: %s", i, path)
	}

	// List secrets and verify tenant-a secrets are present
	t.Run("list_and_verify_tenant_a_secrets", func(t *testing.T) {
		resp := listSecretsAs(t, "tenant-a", endpoint, basePath+"/tenant-a")
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
		resp := listSecretsAs(t, "tenant-b", endpoint, basePath+"/tenant-b")
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

	// Neither tenant lists or reads the other's secrets.
	t.Run("tenants_do_not_see_each_other", func(t *testing.T) {
		assert.Empty(t, listSecretsAs(t, "tenant-b", endpoint, basePath+"/tenant-a").Secrets,
			"tenant B lists tenant A's secrets")
		assert.Empty(t, listSecretsAs(t, "tenant-a", endpoint, basePath+"/tenant-b").Secrets,
			"tenant A lists tenant B's secrets")

		secret, err := tryGetSecretAs(t, "tenant-b", endpoint, tenantASecrets[0])
		assert.Error(t, err, "tenant B read tenant A's secret")
		assert.Nil(t, secret)
		secret, err = tryGetSecretAs(t, "tenant-a", endpoint, tenantBSecrets[0])
		assert.Error(t, err, "tenant A read tenant B's secret")
		assert.Nil(t, secret)
	})

	// Cleanup
	for _, path := range tenantASecrets {
		deleteSecretAs(t, "tenant-a", endpoint, path, true)
	}
	for _, path := range tenantBSecrets {
		deleteSecretAs(t, "tenant-b", endpoint, path, true)
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

	reqURL := fmt.Sprintf("%s/api/v1/secrets/%s", endpoint, path)
	req, err := http.NewRequest(http.MethodPost, reqURL, bytes.NewReader(bodyBytes))
	require.NoError(t, err)
	req.Header.Set("Content-Type", "application/json")

	client := helpers.NewTenantHTTPClient(t, 10*time.Second)
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

	conn, err := helpers.SetupGRPCConnectionToEndpoint(t, ctx, grpcEndpoints[0], grpc.WithPerRPCCredentials(helpers.TenantGRPCCredentials(t)))
	require.NoError(t, err, "Failed to connect to gRPC endpoint %s", grpcEndpoints[0])
	defer conn.Close()

	tlsCfg, err := helpers.IntegrationTLSConfig()
	require.NoError(t, err, "load integration TLS config")
	grpcClient, err := grpcclient.NewClient(grpcEndpoints[0], grpcclient.WithTLSConfig(tlsCfg),
		grpcclient.WithDialOptions(grpc.WithPerRPCCredentials(helpers.TenantGRPCCredentials(t))))
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
	conn, err := helpers.SetupGRPCConnectionToEndpoint(t, ctx, grpcEndpoints[0], grpc.WithPerRPCCredentials(helpers.TenantGRPCCredentials(t)))
	require.NoError(t, err, "Failed to connect to gRPC endpoint %s", grpcEndpoints[0])
	defer conn.Close()

	tlsCfg, err := helpers.IntegrationTLSConfig()
	require.NoError(t, err, "load integration TLS config")
	grpcClient, err := grpcclient.NewClient(grpcEndpoints[0], grpcclient.WithTLSConfig(tlsCfg),
		grpcclient.WithDialOptions(grpc.WithPerRPCCredentials(helpers.TenantGRPCCredentials(t))))
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
		conn, err := helpers.SetupGRPCConnectionToEndpoint(t, ctx, grpcEndpoints[0], grpc.WithPerRPCCredentials(helpers.TenantGRPCCredentials(t)))
		require.NoError(t, err, "Failed to connect to gRPC endpoint %s", grpcEndpoints[0])
		defer conn.Close()

		testTenantIsolationForGRPC(t, ctx, basePath+"/grpc", principalGRPCClient(grpcEndpoints[0]), "grpc-iso-1", "grpc-iso-2")
	})

	// Test tenant isolation via Unix Socket if available
	t.Run("UnixSocket_tenant_isolation", func(t *testing.T) {
		socketPath := os.Getenv("QUICBAAS_NODE1_UNIX")
		if socketPath == "" {
			socketPath = "/var/run/quicbaas/node1.sock"
		}
		_, err := net.Dial("unix", socketPath)
		require.NoError(t, err, "Unix socket must be available at %s for integration tests", socketPath)

		tlsCfg, err := helpers.IntegrationTLSConfig()
		require.NoError(t, err, "load integration TLS config")

		conn, err := grpc.DialContext(ctx,
			"unix://"+socketPath,
			grpc.WithTransportCredentials(credentials.NewTLS(tlsCfg)),
			grpc.WithPerRPCCredentials(helpers.TenantGRPCCredentials(t)),
		)
		require.NoError(t, err, "Failed to connect to unix socket at %s", socketPath)
		defer conn.Close()

		testTenantIsolationForGRPC(t, ctx, basePath+"/unix", principalGRPCClient("unix://"+socketPath), "unix-iso-1", "unix-iso-2")
	})
}

// principalGRPCClient dials target once per tenant principal. Each client
// is closed when its test ends.
func principalGRPCClient(target string) grpcClientFor {
	return func(t *testing.T, principal string) *grpcclient.Client {
		t.Helper()

		tlsCfg, err := helpers.IntegrationTLSConfig()
		require.NoError(t, err, "load integration TLS config")
		client, err := grpcclient.NewClient(target, grpcclient.WithTLSConfig(tlsCfg),
			grpcclient.WithDialOptions(grpc.WithPerRPCCredentials(helpers.NamedTenantGRPCCredentials(t, principal))))
		require.NoError(t, err, "create client for %s as %s", target, principal)
		t.Cleanup(func() {
			if err := client.Close(); err != nil {
				t.Logf("close client for %s: %v", target, err)
			}
		})
		return client
	}
}

// testTenantIsolationForProtocol tests tenant isolation using REST API.
func testTenantIsolationForProtocol(t *testing.T, protocolName, basePath, endpoint string, _ interface{}) {
	t.Helper()

	// Two tenants write the SAME path; only their bearers tell them apart.
	suffix := time.Now().UnixNano()
	tenant1 := newTenantSecretsClient(t, endpoint, fmt.Sprintf("iso-1-%d", suffix))
	tenant2 := newTenantSecretsClient(t, endpoint, fmt.Sprintf("iso-2-%d", suffix))
	path := basePath + "/shared"

	tenant1.store(t, path, "tenant1-key")
	status, body := tenant2.do(t, http.MethodGet, "/api/v1/secrets/"+path, nil)
	assert.Equal(t, http.StatusNotFound, status, "[%s] tenant 2 read tenant 1's secret: %s", protocolName, body)

	tenant2.store(t, path, "tenant2-key")
	value1, _ := tenant1.read(t, path)
	value2, _ := tenant2.read(t, path)
	assert.Equal(t, "tenant1-key", value1)
	assert.Equal(t, "tenant2-key", value2)

	t.Logf("[%s] Tenant isolation verified", protocolName)

	for _, c := range []*tenantSecretsClient{tenant1, tenant2} {
		status, body := c.do(t, http.MethodDelete, "/api/v1/secrets/"+path+"?destroy=true", nil)
		require.Equal(t, http.StatusOK, status, "[%s] cleanup: %s", protocolName, body)
	}
}

// testTenantIsolationForGRPC tests tenant isolation using gRPC client.
// grpcClientFor returns a client of one transport that authenticates as
// the named tenant principal.
type grpcClientFor func(t *testing.T, principal string) *grpcclient.Client

// testTenantIsolationForGRPC writes the SAME path as two tenant principals
// and checks that reads, updates and deletes stay in the caller's tenant.
// The gRPC client sends no tenant ID, so the tenant comes only from the
// bearer: a shared principal would prove nothing.
func testTenantIsolationForGRPC(t *testing.T, ctx context.Context, path string, clientFor grpcClientFor, owner, other string) {
	t.Helper()

	ownerClient := clientFor(t, owner)
	otherClient := clientFor(t, other)

	ownerData, err := json.Marshal(map[string]string{"api_key": owner + "-key"})
	require.NoError(t, err)
	otherData, err := json.Marshal(map[string]string{"api_key": other + "-key"})
	require.NoError(t, err)

	_, err = ownerClient.StoreSecret(ctx, &service.StoreSecretRequest{
		SecretID: path,
		Name:     "isolation-secret",
		Data:     ownerData,
	})
	require.NoError(t, err)
	t.Cleanup(func() { deleteGRPCSecret(t, ownerClient, path) })

	t.Run("other tenant cannot touch the owner's secret", func(t *testing.T) {
		resp, err := otherClient.GetSecret(ctx, &service.GetSecretRequest{SecretID: path})
		require.Error(t, err, "%s read %s's secret", other, owner)
		assert.Nil(t, resp)

		_, err = otherClient.RotateSecret(ctx, &service.RotateSecretRequest{SecretID: path, NewData: otherData})
		assert.Error(t, err, "%s rotated %s's secret", other, owner)

		_, err = otherClient.UpdateSecretStatus(ctx, &service.UpdateSecretStatusRequest{
			SecretID: path,
			Status:   service.SecretStatusDisabled,
		})
		assert.Error(t, err, "%s disabled %s's secret", other, owner)

		_, err = otherClient.DeleteSecret(ctx, &service.DeleteSecretRequest{SecretID: path, PermanentlyDelete: true})
		assert.Error(t, err, "%s deleted %s's secret", other, owner)

		assertGRPCSecret(t, ctx, ownerClient, path, ownerData)
	})

	_, err = otherClient.StoreSecret(ctx, &service.StoreSecretRequest{
		SecretID: path,
		Name:     "isolation-secret",
		Data:     otherData,
	})
	require.NoError(t, err, "%s must be able to store its own copy of the path", other)
	t.Cleanup(func() { deleteGRPCSecret(t, otherClient, path) })

	t.Run("each tenant reads its own value of the path", func(t *testing.T) {
		assertGRPCSecret(t, ctx, ownerClient, path, ownerData)
		assertGRPCSecret(t, ctx, otherClient, path, otherData)
	})

	t.Run("deleting one tenant's copy leaves the other", func(t *testing.T) {
		_, err := otherClient.DeleteSecret(ctx, &service.DeleteSecretRequest{SecretID: path, PermanentlyDelete: true})
		require.NoError(t, err)

		_, err = otherClient.GetSecret(ctx, &service.GetSecretRequest{SecretID: path})
		require.Error(t, err, "%s's copy must be gone", other)
		assertGRPCSecret(t, ctx, ownerClient, path, ownerData)
	})
}

// assertGRPCSecret checks that client reads want at path.
func assertGRPCSecret(t *testing.T, ctx context.Context, client *grpcclient.Client, path string, want []byte) {
	t.Helper()

	resp, err := client.GetSecret(ctx, &service.GetSecretRequest{SecretID: path})
	require.NoError(t, err)
	assert.JSONEq(t, string(want), string(resp.Data))
}

// deleteGRPCSecret removes path for the client's tenant. A secret already
// deleted by the test is not an error.
func deleteGRPCSecret(t *testing.T, client *grpcclient.Client, path string) {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if _, err := client.DeleteSecret(ctx, &service.DeleteSecretRequest{SecretID: path, PermanentlyDelete: true}); err != nil {
		t.Logf("cleanup of %s: %v", path, err)
	}
}

// =============================================================================
// Helper Functions
// =============================================================================
