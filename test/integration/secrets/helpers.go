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
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	qberrors "github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
	"github.com/jeremyhahn/go-quicbaas/test/integration/helpers"
)

// getRESTEndpoint returns the REST API endpoint for testing.
func getRESTEndpoint() string {
	endpoint := os.Getenv("QUICBAAS_NODE1_URL")
	if endpoint == "" {
		endpoint = "https://test-node1:8080"
	}
	return endpoint
}

// getRESTEndpoints returns all REST API endpoints for testing.
func getRESTEndpoints() []string {
	node1 := os.Getenv("QUICBAAS_NODE1_URL")
	if node1 == "" {
		node1 = "https://test-node1:8080"
	}

	node2 := os.Getenv("QUICBAAS_NODE2_URL")
	if node2 == "" {
		node2 = "https://test-node2:8080"
	}

	node3 := os.Getenv("QUICBAAS_NODE3_URL")
	if node3 == "" {
		node3 = "https://test-node3:8080"
	}

	return []string{node1, node2, node3}
}

// SecretOptions contains optional parameters for secret operations.
type SecretOptions struct {
	TenantID    string
	KeyID       string
	Algorithm   string
	Metadata    map[string]string
	Description string
}

// createSecret creates a secret at the given path with the specified data.
func createSecret(t *testing.T, endpoint, path string, data map[string]interface{}, opts *SecretOptions) *service.StoreSecretResponse {
	t.Helper()

	reqBody := map[string]interface{}{
		"data": data,
	}

	if opts != nil {
		if opts.Metadata != nil {
			reqBody["metadata"] = opts.Metadata
		}
		if opts.Description != "" {
			reqBody["description"] = opts.Description
		}
		if opts.TenantID != "" {
			reqBody["tenant_id"] = opts.TenantID
		}
		if opts.KeyID != "" {
			reqBody["key_id"] = opts.KeyID
		}
		if opts.Algorithm != "" {
			reqBody["algorithm"] = opts.Algorithm
		}
	}

	bodyBytes, err := json.Marshal(reqBody)
	require.NoError(t, err)

	url := fmt.Sprintf("%s/api/v1/secrets/%s", endpoint, path)
	req, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(bodyBytes))
	require.NoError(t, err)

	req.Header.Set("Content-Type", "application/json")
	if opts != nil && opts.TenantID != "" {
		req.Header.Set("X-Tenant-ID", opts.TenantID)
	}

	client := helpers.NewHTTPClient(10 * time.Second)
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

// getSecret retrieves a secret at the given path.
func getSecret(t *testing.T, endpoint, path string) *service.GetSecretResponse {
	t.Helper()

	url := fmt.Sprintf("%s/api/v1/secrets/%s", endpoint, path)
	req, err := http.NewRequest(http.MethodGet, url, nil)
	require.NoError(t, err)

	client := helpers.NewHTTPClient(10 * time.Second)
	resp, err := client.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	require.NoError(t, err)

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("Expected status 200, got %d. Body: %s", resp.StatusCode, string(respBody))
	}

	var result service.GetSecretResponse
	err = json.Unmarshal(respBody, &result)
	require.NoError(t, err)

	return &result
}

// getSecretVersion retrieves a specific version of a secret.
func getSecretVersion(t *testing.T, endpoint, path string, version uint64) *service.GetSecretResponse {
	t.Helper()

	url := fmt.Sprintf("%s/api/v1/secrets/%s?version=%d", endpoint, path, version)
	req, err := http.NewRequest(http.MethodGet, url, nil)
	require.NoError(t, err)

	client := helpers.NewHTTPClient(10 * time.Second)
	resp, err := client.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	require.NoError(t, err)

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("Expected status 200, got %d. Body: %s", resp.StatusCode, string(respBody))
	}

	var result service.GetSecretResponse
	err = json.Unmarshal(respBody, &result)
	require.NoError(t, err)

	return &result
}

// deleteSecret deletes a secret at the given path.
func deleteSecret(t *testing.T, endpoint, path string, permanent bool) *service.DeleteSecretResponse {
	t.Helper()

	url := fmt.Sprintf("%s/api/v1/secrets/%s", endpoint, path)
	if permanent {
		url += "?destroy=true"
	}

	req, err := http.NewRequest(http.MethodDelete, url, nil)
	require.NoError(t, err)

	client := helpers.NewHTTPClient(10 * time.Second)
	resp, err := client.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	require.NoError(t, err)

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("Expected status 200, got %d. Body: %s", resp.StatusCode, string(respBody))
	}

	var result service.DeleteSecretResponse
	err = json.Unmarshal(respBody, &result)
	require.NoError(t, err)

	return &result
}

// tryDeleteSecret attempts to delete a secret at the given path.
// Unlike deleteSecret, it handles 404 (Not Found) gracefully for cleanup scenarios.
// Returns true if deleted, false if not found, and fails the test on other errors.
func tryDeleteSecret(t *testing.T, endpoint, path string, permanent bool) bool {
	t.Helper()

	url := fmt.Sprintf("%s/api/v1/secrets/%s", endpoint, path)
	if permanent {
		url += "?destroy=true"
	}

	req, err := http.NewRequest(http.MethodDelete, url, nil)
	require.NoError(t, err)

	client := helpers.NewHTTPClient(10 * time.Second)
	resp, err := client.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()

	// Handle not found gracefully - secret may not exist, which is OK during cleanup
	if resp.StatusCode == http.StatusNotFound {
		t.Logf("Secret at %s not found (already deleted or never existed)", path)
		return false
	}

	respBody, err := io.ReadAll(resp.Body)
	require.NoError(t, err)

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("Expected status 200 or 404, got %d. Body: %s", resp.StatusCode, string(respBody))
	}

	return true
}

// listSecrets lists secrets with optional prefix filter.
func listSecrets(t *testing.T, endpoint, prefix string) *service.ListSecretsResponse {
	t.Helper()

	url := fmt.Sprintf("%s/api/v1/secrets", endpoint)
	if prefix != "" {
		url += fmt.Sprintf("?prefix=%s", prefix)
	}

	req, err := http.NewRequest(http.MethodGet, url, nil)
	require.NoError(t, err)

	client := helpers.NewHTTPClient(10 * time.Second)
	resp, err := client.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	require.NoError(t, err)

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("Expected status 200, got %d. Body: %s", resp.StatusCode, string(respBody))
	}

	var result service.ListSecretsResponse
	err = json.Unmarshal(respBody, &result)
	require.NoError(t, err)

	return &result
}

// rollbackSecret rolls back a secret to a specific version.
func rollbackSecret(t *testing.T, endpoint, path string, version uint64) *service.RollbackSecretResponse {
	t.Helper()

	reqBody := map[string]interface{}{
		"version": version,
	}

	bodyBytes, err := json.Marshal(reqBody)
	require.NoError(t, err)

	url := fmt.Sprintf("%s/api/v1/secrets/%s?action=rollback", endpoint, path)
	req, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(bodyBytes))
	require.NoError(t, err)

	req.Header.Set("Content-Type", "application/json")

	client := helpers.NewHTTPClient(10 * time.Second)
	resp, err := client.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	require.NoError(t, err)

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("Expected status 200, got %d. Body: %s", resp.StatusCode, string(respBody))
	}

	var result service.RollbackSecretResponse
	err = json.Unmarshal(respBody, &result)
	require.NoError(t, err)

	return &result
}

// rotateSecret rotates a secret to new data.
func rotateSecret(t *testing.T, endpoint, path string, newData map[string]interface{}) *service.RotateSecretResponse {
	t.Helper()

	reqBody := map[string]interface{}{
		"data": newData,
	}

	bodyBytes, err := json.Marshal(reqBody)
	require.NoError(t, err)

	url := fmt.Sprintf("%s/api/v1/secrets/%s?action=rotate", endpoint, path)
	req, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(bodyBytes))
	require.NoError(t, err)

	req.Header.Set("Content-Type", "application/json")

	client := helpers.NewHTTPClient(10 * time.Second)
	resp, err := client.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	require.NoError(t, err)

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("Expected status 200, got %d. Body: %s", resp.StatusCode, string(respBody))
	}

	var result service.RotateSecretResponse
	err = json.Unmarshal(respBody, &result)
	require.NoError(t, err)

	return &result
}

// listSecretVersions lists all versions of a secret.
func listSecretVersions(t *testing.T, endpoint, path string) *service.ListSecretVersionsResponse {
	t.Helper()

	url := fmt.Sprintf("%s/api/v1/secrets/%s?versions=true", endpoint, path)
	req, err := http.NewRequest(http.MethodGet, url, nil)
	require.NoError(t, err)

	client := helpers.NewHTTPClient(10 * time.Second)
	resp, err := client.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	require.NoError(t, err)

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("Expected status 200, got %d. Body: %s", resp.StatusCode, string(respBody))
	}

	var result service.ListSecretVersionsResponse
	err = json.Unmarshal(respBody, &result)
	require.NoError(t, err)

	return &result
}

// tryGetSecret attempts to get a secret and returns the error if it fails.
func tryGetSecret(t *testing.T, endpoint, path string) (*service.GetSecretResponse, error) {
	t.Helper()

	url := fmt.Sprintf("%s/api/v1/secrets/%s", endpoint, path)
	req, err := http.NewRequest(http.MethodGet, url, nil)
	require.NoError(t, err)

	client := helpers.NewHTTPClient(10 * time.Second)
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode != http.StatusOK {
		return nil, &qberrors.MessageError{Message: fmt.Sprintf("status %d: %s", resp.StatusCode, string(respBody))}
	}

	var result service.GetSecretResponse
	err = json.Unmarshal(respBody, &result)
	if err != nil {
		return nil, err
	}

	return &result, nil
}
