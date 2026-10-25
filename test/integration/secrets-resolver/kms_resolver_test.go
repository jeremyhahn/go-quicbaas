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

package secretsresolver

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	qberrors "github.com/jeremyhahn/go-quicbaas/pkg/errors"

	"github.com/jeremyhahn/go-quicbaas/pkg/config/secrets"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
	"github.com/jeremyhahn/go-quicbaas/test/integration/helpers"
)

// restKMSClient implements secrets.SecretsClient using the REST API.
type restKMSClient struct {
	endpoint string
	client   *http.Client
}

// newRESTKMSClient creates a new REST-based KMS client acting as the tenant
// principal.
func newRESTKMSClient(t *testing.T, endpoint string) *restKMSClient {
	t.Helper()
	return &restKMSClient{
		endpoint: endpoint,
		client:   helpers.NewTenantHTTPClient(t, 10*time.Second),
	}
}

// GetSecret retrieves a secret by its ID/path from the local KMS via REST API.
func (c *restKMSClient) GetSecret(ctx context.Context, secretID string) ([]byte, error) {
	url := fmt.Sprintf("%s/api/v1/secrets/%s", c.endpoint, secretID)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, &qberrors.WrappedMessageError{Message: "failed to create request", Cause: err}
	}

	resp, err := c.client.Do(req)
	if err != nil {
		return nil, &qberrors.WrappedMessageError{Message: "failed to execute request", Cause: err}
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, &qberrors.WrappedMessageError{Message: "failed to read response body", Cause: err}
	}

	if resp.StatusCode == http.StatusNotFound {
		return nil, &secrets.SecretNotFoundError{
			Path:     secretID,
			Provider: "local_kms",
		}
	}

	if resp.StatusCode != http.StatusOK {
		return nil, &qberrors.MessageError{Message: fmt.Sprintf("unexpected status %d: %s", resp.StatusCode, string(respBody))}
	}

	var result service.GetSecretResponse
	if err := json.Unmarshal(respBody, &result); err != nil {
		return nil, &qberrors.WrappedMessageError{Message: "failed to unmarshal response", Cause: err}
	}

	return result.Data, nil
}

// storeTestSecret stores a secret via REST API for testing.
func storeTestSecret(t *testing.T, endpoint, path string, data map[string]interface{}) *service.StoreSecretResponse {
	t.Helper()

	reqBody := map[string]interface{}{
		"data": data,
	}

	bodyBytes, err := json.Marshal(reqBody)
	require.NoError(t, err)

	url := fmt.Sprintf("%s/api/v1/secrets/%s", endpoint, path)
	req, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(bodyBytes))
	require.NoError(t, err)

	req.Header.Set("Content-Type", "application/json")

	client := helpers.NewTenantHTTPClient(t, 10*time.Second)
	resp, err := client.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	require.NoError(t, err)

	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("Expected status 201, got %d. Body: %s", resp.StatusCode, string(respBody))
	}

	var result service.StoreSecretResponse
	err = json.Unmarshal(respBody, &result)
	require.NoError(t, err)

	return &result
}

// deleteTestSecret deletes a secret via REST API for cleanup.
func deleteTestSecret(t *testing.T, endpoint, path string) {
	t.Helper()

	url := fmt.Sprintf("%s/api/v1/secrets/%s?destroy=true", endpoint, path)
	req, err := http.NewRequest(http.MethodDelete, url, nil)
	require.NoError(t, err)

	client := helpers.NewTenantHTTPClient(t, 10*time.Second)
	resp, err := client.Do(req)
	if err != nil {
		t.Logf("Failed to delete test secret %s: %v", path, err)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusNotFound {
		respBody, _ := io.ReadAll(resp.Body)
		t.Logf("Unexpected status deleting secret %s: %d, body: %s", path, resp.StatusCode, string(respBody))
	}
}

// TestLocalKMSResolver_ResolveExistingSecret tests resolving a secret that exists in the KMS.
func TestLocalKMSResolver_ResolveExistingSecret(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	endpoint := getRESTEndpoint()
	path := fmt.Sprintf("test/resolver/existing/%d", time.Now().UnixNano())

	// Store a secret via REST API
	secretData := map[string]interface{}{
		"username": "test-user",
		"password": "super-secret-password-12345",
		"host":     "db.example.com",
	}
	storeResp := storeTestSecret(t, endpoint, path, secretData)
	require.NotNil(t, storeResp)
	require.Equal(t, path, storeResp.SecretID)

	// Cleanup after test
	defer deleteTestSecret(t, endpoint, path)

	// Create the KMS client and resolver
	kmsClient := newRESTKMSClient(t, endpoint)
	resolver := secrets.NewLocalKMSResolver(kmsClient)
	require.NotNil(t, resolver)

	// Resolve the secret using kms:// URI
	kmsURI := fmt.Sprintf("kms://%s", path)
	result, err := resolver.Resolve(ctx, kmsURI)

	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Equal(t, kmsURI, result.Raw)
	assert.Equal(t, secrets.StrategyLocalKMS, result.Strategy)

	// Verify the resolved data contains our secret
	var resolvedData map[string]interface{}
	err = json.Unmarshal([]byte(plaintextOf(t, result)), &resolvedData)
	require.NoError(t, err)
	assert.Equal(t, "test-user", resolvedData["username"])
	assert.Equal(t, "super-secret-password-12345", resolvedData["password"])
	assert.Equal(t, "db.example.com", resolvedData["host"])

	t.Logf("Successfully resolved secret at kms://%s", path)
}

// TestLocalKMSResolver_ResolveNonExistent tests resolving a secret that does not exist.
func TestLocalKMSResolver_ResolveNonExistent(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	endpoint := getRESTEndpoint()

	// Create the KMS client and resolver
	kmsClient := newRESTKMSClient(t, endpoint)
	resolver := secrets.NewLocalKMSResolver(kmsClient)
	require.NotNil(t, resolver)

	// Try to resolve a non-existent secret
	nonExistentPath := fmt.Sprintf("test/resolver/nonexistent/%d", time.Now().UnixNano())
	kmsURI := fmt.Sprintf("kms://%s", nonExistentPath)

	result, err := resolver.Resolve(ctx, kmsURI)

	require.Error(t, err)
	assert.Nil(t, result)
	assert.True(t, secrets.IsSecretNotFound(err), "Expected SecretNotFoundError, got: %v", err)

	t.Logf("Correctly received error for non-existent secret: %v", err)
}

// TestLocalKMSResolver_CanResolve tests the CanResolve method.
func TestLocalKMSResolver_CanResolve(t *testing.T) {
	endpoint := getRESTEndpoint()

	kmsClient := newRESTKMSClient(t, endpoint)
	resolver := secrets.NewLocalKMSResolver(kmsClient)
	require.NotNil(t, resolver)

	testCases := []struct {
		name     string
		uri      string
		expected bool
	}{
		{
			name:     "valid kms URI",
			uri:      "kms://secrets/myapp/password",
			expected: true,
		},
		{
			name:     "valid kms URI with simple path",
			uri:      "kms://password",
			expected: true,
		},
		{
			name:     "valid kms URI with nested path",
			uri:      "kms://config/app/database/password",
			expected: true,
		},
		{
			name:     "invalid - empty path",
			uri:      "kms://",
			expected: false,
		},
		{
			name:     "invalid - wrong scheme vault",
			uri:      "vault://secrets/password",
			expected: false,
		},
		{
			name:     "invalid - wrong scheme env",
			uri:      "env://MY_SECRET",
			expected: false,
		},
		{
			name:     "invalid - wrong scheme file",
			uri:      "file:///etc/secrets/password",
			expected: false,
		},
		{
			name:     "invalid - plain value",
			uri:      "just-a-password",
			expected: false,
		},
		{
			name:     "invalid - partial scheme",
			uri:      "kms:/",
			expected: false,
		},
		{
			name:     "invalid - no scheme",
			uri:      "//secrets/password",
			expected: false,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			result := resolver.CanResolve(tc.uri)
			assert.Equal(t, tc.expected, result, "CanResolve(%q) = %v, expected %v", tc.uri, result, tc.expected)
		})
	}
}

// TestLocalKMSResolver_Scheme tests the Scheme method.
func TestLocalKMSResolver_Scheme(t *testing.T) {
	endpoint := getRESTEndpoint()

	kmsClient := newRESTKMSClient(t, endpoint)
	resolver := secrets.NewLocalKMSResolver(kmsClient)
	require.NotNil(t, resolver)

	scheme := resolver.Scheme()
	assert.Equal(t, "kms", scheme)

	t.Logf("Resolver scheme: %s", scheme)
}

// TestLocalKMSResolver_NestedPath tests resolution of secrets with nested paths.
func TestLocalKMSResolver_NestedPath(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	endpoint := getRESTEndpoint()
	basePath := fmt.Sprintf("test/resolver/nested/%d", time.Now().UnixNano())
	path := fmt.Sprintf("%s/config/app/database/password", basePath)

	// Store a secret with a deeply nested path
	secretData := map[string]interface{}{
		"password":       "nested-db-password-xyz",
		"connection_url": "postgresql://user:pass@localhost:5432/mydb",
	}
	storeResp := storeTestSecret(t, endpoint, path, secretData)
	require.NotNil(t, storeResp)
	require.Equal(t, path, storeResp.SecretID)

	// Cleanup after test
	defer deleteTestSecret(t, endpoint, path)

	// Create the KMS client and resolver
	kmsClient := newRESTKMSClient(t, endpoint)
	resolver := secrets.NewLocalKMSResolver(kmsClient)
	require.NotNil(t, resolver)

	// Resolve the nested secret
	kmsURI := fmt.Sprintf("kms://%s", path)
	result, err := resolver.Resolve(ctx, kmsURI)

	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Equal(t, kmsURI, result.Raw)
	assert.Equal(t, secrets.StrategyLocalKMS, result.Strategy)

	// Verify the resolved data
	var resolvedData map[string]interface{}
	err = json.Unmarshal([]byte(plaintextOf(t, result)), &resolvedData)
	require.NoError(t, err)
	assert.Equal(t, "nested-db-password-xyz", resolvedData["password"])
	assert.Equal(t, "postgresql://user:pass@localhost:5432/mydb", resolvedData["connection_url"])

	t.Logf("Successfully resolved nested secret at kms://%s", path)
}

// TestLocalKMSResolver_VersionedSecret tests resolution of versioned secrets.
func TestLocalKMSResolver_VersionedSecret(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	endpoint := getRESTEndpoint()
	path := fmt.Sprintf("test/resolver/versioned/%d", time.Now().UnixNano())

	// Create version 1
	v1Data := map[string]interface{}{
		"api_key": "api-key-version-1",
		"version": "1.0.0",
	}
	storeResp := storeTestSecret(t, endpoint, path, v1Data)
	require.NotNil(t, storeResp)
	require.Equal(t, uint64(1), storeResp.Version)

	// Create version 2
	v2Data := map[string]interface{}{
		"api_key": "api-key-version-2",
		"version": "2.0.0",
	}
	storeResp = storeTestSecret(t, endpoint, path, v2Data)
	require.NotNil(t, storeResp)
	require.GreaterOrEqual(t, storeResp.Version, uint64(1))

	// Create version 3 (latest)
	v3Data := map[string]interface{}{
		"api_key": "api-key-version-3-latest",
		"version": "3.0.0",
	}
	storeResp = storeTestSecret(t, endpoint, path, v3Data)
	require.NotNil(t, storeResp)
	require.GreaterOrEqual(t, storeResp.Version, uint64(1))

	// Cleanup after test
	defer deleteTestSecret(t, endpoint, path)

	// Create the KMS client and resolver
	kmsClient := newRESTKMSClient(t, endpoint)
	resolver := secrets.NewLocalKMSResolver(kmsClient)
	require.NotNil(t, resolver)

	// Resolve the secret (should get latest version)
	kmsURI := fmt.Sprintf("kms://%s", path)
	result, err := resolver.Resolve(ctx, kmsURI)

	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Equal(t, kmsURI, result.Raw)
	assert.Equal(t, secrets.StrategyLocalKMS, result.Strategy)

	// Verify we got the latest version (v3)
	var resolvedData map[string]interface{}
	err = json.Unmarshal([]byte(plaintextOf(t, result)), &resolvedData)
	require.NoError(t, err)
	assert.Equal(t, "api-key-version-3-latest", resolvedData["api_key"])
	assert.Equal(t, "3.0.0", resolvedData["version"])

	t.Logf("Successfully resolved latest version of secret at kms://%s", path)
}

// TestLocalKMSResolver_InvalidURI tests error handling for invalid URIs.
func TestLocalKMSResolver_InvalidURI(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	endpoint := getRESTEndpoint()

	kmsClient := newRESTKMSClient(t, endpoint)
	resolver := secrets.NewLocalKMSResolver(kmsClient)
	require.NotNil(t, resolver)

	testCases := []struct {
		name        string
		uri         string
		expectError bool
	}{
		{
			name:        "empty path after scheme",
			uri:         "kms://",
			expectError: true,
		},
		{
			name:        "wrong scheme",
			uri:         "vault://secrets/password",
			expectError: true,
		},
		{
			name:        "no scheme",
			uri:         "secrets/password",
			expectError: true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			result, err := resolver.Resolve(ctx, tc.uri)

			if tc.expectError {
				assert.Error(t, err, "Expected error for URI %q", tc.uri)
				assert.Nil(t, result)
				t.Logf("Correctly received error for invalid URI %q: %v", tc.uri, err)
			} else {
				assert.NoError(t, err)
				assert.NotNil(t, result)
			}
		})
	}
}

// TestLocalKMSResolver_ContextCancellation tests behavior when context is cancelled.
func TestLocalKMSResolver_ContextCancellation(t *testing.T) {
	endpoint := getRESTEndpoint()

	kmsClient := newRESTKMSClient(t, endpoint)
	resolver := secrets.NewLocalKMSResolver(kmsClient)
	require.NotNil(t, resolver)

	// Create a cancelled context
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	kmsURI := "kms://test/secret"
	result, err := resolver.Resolve(ctx, kmsURI)

	assert.Error(t, err)
	assert.Nil(t, result)
	assert.Contains(t, err.Error(), "context cancelled")

	t.Logf("Correctly handled cancelled context: %v", err)
}

// TestLocalKMSResolver_NilClient tests that NewLocalKMSResolver returns nil for nil client.
func TestLocalKMSResolver_NilClient(t *testing.T) {
	resolver := secrets.NewLocalKMSResolver(nil)
	assert.Nil(t, resolver)

	t.Logf("Correctly returned nil for nil client")
}

// TestLocalKMSResolver_ResolverRegistry tests the resolver works with ResolverRegistry.
func TestLocalKMSResolver_ResolverRegistry(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	endpoint := getRESTEndpoint()
	path := fmt.Sprintf("test/resolver/registry/%d", time.Now().UnixNano())

	// Store a test secret
	secretData := map[string]interface{}{
		"token": "registry-test-token-abc123",
	}
	storeResp := storeTestSecret(t, endpoint, path, secretData)
	require.NotNil(t, storeResp)

	// Cleanup after test
	defer deleteTestSecret(t, endpoint, path)

	// Create resolver and register with registry
	kmsClient := newRESTKMSClient(t, endpoint)
	resolver := secrets.NewLocalKMSResolver(kmsClient)
	require.NotNil(t, resolver)

	registry := secrets.NewResolverRegistry()
	registry.Register(resolver)

	// Test IsSecretURI
	kmsURI := fmt.Sprintf("kms://%s", path)
	assert.True(t, registry.IsSecretURI(kmsURI))
	assert.False(t, registry.IsSecretURI("plain-value"))
	assert.False(t, registry.IsSecretURI("vault://other/secret"))

	// Resolve through registry
	result, err := registry.Resolve(ctx, kmsURI)
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Equal(t, secrets.StrategyLocalKMS, result.Strategy)

	// Verify resolved data
	var resolvedData map[string]interface{}
	err = json.Unmarshal([]byte(plaintextOf(t, result)), &resolvedData)
	require.NoError(t, err)
	assert.Equal(t, "registry-test-token-abc123", resolvedData["token"])

	// Test literal fallback
	literalResult, err := registry.Resolve(ctx, "literal-value")
	require.NoError(t, err)
	require.NotNil(t, literalResult)
	assert.Equal(t, secrets.StrategyLiteral, literalResult.Strategy)
	assert.Equal(t, "literal-value", plaintextOf(t, literalResult))

	t.Logf("Successfully tested resolver with ResolverRegistry")
}

// TestLocalKMSResolver_BinaryData tests resolution of binary secret data.
func TestLocalKMSResolver_BinaryData(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	endpoint := getRESTEndpoint()
	path := fmt.Sprintf("test/resolver/binary/%d", time.Now().UnixNano())

	// Store a secret with base64-encoded binary data
	secretData := map[string]interface{}{
		"certificate": "LS0tLS1CRUdJTiBDRVJUSUZJQ0FURS0tLS0t...",
		"private_key": "LS0tLS1CRUdJTiBQUklWQVRFIEtFWS0tLS0t...",
		"type":        "tls-certificate",
	}
	storeResp := storeTestSecret(t, endpoint, path, secretData)
	require.NotNil(t, storeResp)

	// Cleanup after test
	defer deleteTestSecret(t, endpoint, path)

	// Create the KMS client and resolver
	kmsClient := newRESTKMSClient(t, endpoint)
	resolver := secrets.NewLocalKMSResolver(kmsClient)
	require.NotNil(t, resolver)

	// Resolve the secret
	kmsURI := fmt.Sprintf("kms://%s", path)
	result, err := resolver.Resolve(ctx, kmsURI)

	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Equal(t, secrets.StrategyLocalKMS, result.Strategy)

	// Verify the resolved data
	var resolvedData map[string]interface{}
	err = json.Unmarshal([]byte(plaintextOf(t, result)), &resolvedData)
	require.NoError(t, err)
	assert.Equal(t, "LS0tLS1CRUdJTiBDRVJUSUZJQ0FURS0tLS0t...", resolvedData["certificate"])
	assert.Equal(t, "LS0tLS1CRUdJTiBQUklWQVRFIEtFWS0tLS0t...", resolvedData["private_key"])
	assert.Equal(t, "tls-certificate", resolvedData["type"])

	t.Logf("Successfully resolved binary data secret at kms://%s", path)
}

// TestLocalKMSResolver_SpecialCharacters tests secrets with special characters in path.
func TestLocalKMSResolver_SpecialCharacters(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	endpoint := getRESTEndpoint()

	// Test path with various allowed characters
	basePath := fmt.Sprintf("test/resolver/special/%d", time.Now().UnixNano())
	path := fmt.Sprintf("%s/my-app_v2.config", basePath)

	secretData := map[string]interface{}{
		"key": "value-with-special-chars",
	}
	storeResp := storeTestSecret(t, endpoint, path, secretData)
	require.NotNil(t, storeResp)

	// Cleanup after test
	defer deleteTestSecret(t, endpoint, path)

	// Create the KMS client and resolver
	kmsClient := newRESTKMSClient(t, endpoint)
	resolver := secrets.NewLocalKMSResolver(kmsClient)
	require.NotNil(t, resolver)

	// Resolve the secret
	kmsURI := fmt.Sprintf("kms://%s", path)
	result, err := resolver.Resolve(ctx, kmsURI)

	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Equal(t, secrets.StrategyLocalKMS, result.Strategy)

	t.Logf("Successfully resolved secret with special characters in path: %s", path)
}

// TestLocalKMSResolver_LargeSecret tests resolution of large secrets.
func TestLocalKMSResolver_LargeSecret(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	endpoint := getRESTEndpoint()
	path := fmt.Sprintf("test/resolver/large/%d", time.Now().UnixNano())

	// Create a large secret (8KB of data)
	largeData := make([]byte, 8192)
	for i := range largeData {
		largeData[i] = byte(i % 256)
	}

	secretData := map[string]interface{}{
		"large_certificate": string(largeData),
		"type":              "large-secret",
	}
	storeResp := storeTestSecret(t, endpoint, path, secretData)
	require.NotNil(t, storeResp)

	// Cleanup after test
	defer deleteTestSecret(t, endpoint, path)

	// Create the KMS client and resolver
	kmsClient := newRESTKMSClient(t, endpoint)
	resolver := secrets.NewLocalKMSResolver(kmsClient)
	require.NotNil(t, resolver)

	// Resolve the secret
	kmsURI := fmt.Sprintf("kms://%s", path)
	result, err := resolver.Resolve(ctx, kmsURI)

	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Equal(t, secrets.StrategyLocalKMS, result.Strategy)

	// Verify the data size
	assert.Greater(t, len(plaintextOf(t, result)), 8000)

	t.Logf("Successfully resolved large secret (%d bytes) at kms://%s", len(plaintextOf(t, result)), path)
}
