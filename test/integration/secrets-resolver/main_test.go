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
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"testing"
	"time"

	qberrors "github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/test/integration/helpers"
)

const (
	// secretsEncryptionKeyName is the default key name for secrets encryption.
	secretsEncryptionKeyName = "quicbaas-secrets-key"

	// testSecretPrefix is the prefix for test secrets.
	testSecretPrefix = "test-resolver-secret-"
)

// testSecrets holds the test secrets created during setup.
var testSecrets = []string{
	"database-password",
	"api-key",
	"jwt-secret",
	"encryption-key",
}

// TestMain sets up the test environment before running secrets resolver tests.
// It creates the required KMS encryption key and test secrets for resolution testing.
func TestMain(m *testing.M) {
	// Set up the secrets encryption key before running tests
	if err := setupSecretsEncryptionKey(); err != nil {
		fmt.Fprintf(os.Stderr, "Failed to set up secrets encryption key: %v\n", err)
		os.Exit(1)
	}

	// Set up test secrets for resolution testing
	if err := setupTestSecrets(); err != nil {
		fmt.Fprintf(os.Stderr, "Failed to set up test secrets: %v\n", err)
		os.Exit(1)
	}

	// Run tests
	code := m.Run()

	// Clean up test secrets after tests
	if err := cleanupTestSecrets(); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: Failed to clean up test secrets: %v\n", err)
	}

	// Exit with the test result code
	os.Exit(code)
}

// getRESTEndpoint returns the REST API endpoint for integration testing.
func getRESTEndpoint() string {
	endpoint := os.Getenv("QUICBAAS_NODE1_URL")
	if endpoint == "" {
		endpoint = "https://test-node1:8080"
	}
	return endpoint
}

// getGRPCEndpoint returns the gRPC endpoint for integration testing.
func getGRPCEndpoint() string {
	endpoint := os.Getenv("QUICBAAS_NODE1_GRPC")
	if endpoint == "" {
		endpoint = "test-node1:50051"
	}
	return endpoint
}

// getMCPEndpoint returns the MCP endpoint for integration testing.
func getMCPEndpoint() string {
	endpoint := os.Getenv("QUICBAAS_NODE1_MCP")
	if endpoint == "" {
		endpoint = "https://test-node1:8888/mcp"
	}
	return endpoint
}

// getUnixSocket returns the gRPC Unix socket path for integration testing.
func getUnixSocket() string {
	socketPath := os.Getenv("QUICBAAS_NODE1_UNIX")
	if socketPath == "" {
		socketPath = "/var/run/quicbaas/grpc-node1.sock"
	}
	return socketPath
}

// setupSecretsEncryptionKey creates the default encryption key used for secrets.
func setupSecretsEncryptionKey() error {
	endpoint := getRESTEndpoint()

	// Check if key already exists
	exists, err := keyExists(endpoint, secretsEncryptionKeyName)
	if err != nil {
		return &qberrors.WrappedMessageError{Message: "checking key existence", Cause: err}
	}

	if exists {
		fmt.Printf("Secrets encryption key '%s' already exists\n", secretsEncryptionKeyName)
		return nil
	}

	// Create the encryption key
	reqBody := map[string]interface{}{
		"name": secretsEncryptionKeyName,
		"key_attributes": map[string]interface{}{
			"algorithm": "AES-256-GCM",
			"key_usage": []string{"encrypt", "decrypt"},
		},
		"description": "Default encryption key for secrets",
		"tags": map[string]string{
			"purpose": "secrets-encryption",
			"managed": "auto-created",
		},
	}

	bodyBytes, err := json.Marshal(reqBody)
	if err != nil {
		return &qberrors.WrappedMessageError{Message: "marshaling request", Cause: err}
	}

	url := fmt.Sprintf("%s/api/v1/kms/keys", endpoint)
	req, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(bodyBytes))
	if err != nil {
		return &qberrors.WrappedMessageError{Message: "creating request", Cause: err}
	}
	req.Header.Set("Content-Type", "application/json")

	client := helpers.NewHTTPClient(30 * time.Second)
	resp, err := client.Do(req)
	if err != nil {
		return &qberrors.WrappedMessageError{Message: "sending request", Cause: err}
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return &qberrors.WrappedMessageError{Message: "reading response", Cause: err}
	}

	if resp.StatusCode != http.StatusCreated && resp.StatusCode != http.StatusOK {
		return &qberrors.MessageError{Message: fmt.Sprintf("failed to create key (status %d): %s", resp.StatusCode, string(respBody))}
	}

	fmt.Printf("Created secrets encryption key '%s'\n", secretsEncryptionKeyName)
	return nil
}

// setupTestSecrets creates test secrets in KMS for resolution testing.
func setupTestSecrets() error {
	endpoint := getRESTEndpoint()
	client := helpers.NewHTTPClient(30 * time.Second)

	for _, secretName := range testSecrets {
		fullName := testSecretPrefix + secretName
		secretValue := fmt.Sprintf("test-value-for-%s", secretName)

		// Check if secret already exists
		exists, err := secretExists(endpoint, fullName)
		if err != nil {
			return &qberrors.WrappedMessageError{Message: "checking secret existence for " + fullName, Cause: err}
		}

		if exists {
			fmt.Printf("Test secret '%s' already exists\n", fullName)
			continue
		}

		// Create the secret
		// The REST API expects the path in the URL, and data in the body as a JSON object
		// Sending data as a JSON object (not a string) avoids base64 encoding requirements
		reqBody := map[string]interface{}{
			"data": map[string]interface{}{
				"value": secretValue,
			},
			"description": fmt.Sprintf("Test secret for resolver integration tests: %s", secretName),
			"metadata": map[string]string{
				"purpose":    "integration-test",
				"managed":    "auto-created",
				"created_at": time.Now().Format(time.RFC3339),
			},
		}

		bodyBytes, err := json.Marshal(reqBody)
		if err != nil {
			return &qberrors.WrappedMessageError{Message: "marshaling request for " + fullName, Cause: err}
		}

		// Include the secret path in the URL
		url := fmt.Sprintf("%s/api/v1/secrets/%s", endpoint, fullName)
		req, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(bodyBytes))
		if err != nil {
			return &qberrors.WrappedMessageError{Message: "creating request for " + fullName, Cause: err}
		}
		req.Header.Set("Content-Type", "application/json")

		resp, err := client.Do(req)
		if err != nil {
			return &qberrors.WrappedMessageError{Message: "sending request for " + fullName, Cause: err}
		}

		respBody, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil {
			return &qberrors.WrappedMessageError{Message: "reading response for " + fullName, Cause: err}
		}

		if resp.StatusCode != http.StatusCreated && resp.StatusCode != http.StatusOK {
			return &qberrors.MessageError{Message: fmt.Sprintf("failed to create secret %s (status %d): %s", fullName, resp.StatusCode, string(respBody))}
		}

		fmt.Printf("Created test secret '%s'\n", fullName)
	}

	return nil
}

// cleanupTestSecrets removes test secrets after tests complete.
func cleanupTestSecrets() error {
	endpoint := getRESTEndpoint()
	client := helpers.NewHTTPClient(30 * time.Second)

	var lastErr error
	for _, secretName := range testSecrets {
		fullName := testSecretPrefix + secretName

		// Check if secret exists before attempting deletion
		exists, err := secretExists(endpoint, fullName)
		if err != nil {
			lastErr = &qberrors.WrappedMessageError{Message: "checking secret existence for " + fullName, Cause: err}
			continue
		}

		if !exists {
			fmt.Printf("Test secret '%s' already cleaned up\n", fullName)
			continue
		}

		// Delete the secret
		url := fmt.Sprintf("%s/api/v1/secrets/%s", endpoint, fullName)
		req, err := http.NewRequest(http.MethodDelete, url, nil)
		if err != nil {
			lastErr = &qberrors.WrappedMessageError{Message: "creating delete request for " + fullName, Cause: err}
			continue
		}

		resp, err := client.Do(req)
		if err != nil {
			lastErr = &qberrors.WrappedMessageError{Message: "sending delete request for " + fullName, Cause: err}
			continue
		}
		resp.Body.Close()

		if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusNoContent && resp.StatusCode != http.StatusNotFound {
			lastErr = &qberrors.MessageError{Message: fmt.Sprintf("failed to delete secret %s (status %d)", fullName, resp.StatusCode)}
			continue
		}

		fmt.Printf("Cleaned up test secret '%s'\n", fullName)
	}

	return lastErr
}

// keyExists checks if a KMS key already exists.
func keyExists(endpoint, keyID string) (bool, error) {
	url := fmt.Sprintf("%s/api/v1/kms/keys/%s", endpoint, keyID)

	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return false, &qberrors.WrappedMessageError{Message: "creating request", Cause: err}
	}

	client := helpers.NewHTTPClient(10 * time.Second)
	resp, err := client.Do(req)
	if err != nil {
		return false, &qberrors.WrappedMessageError{Message: "sending request", Cause: err}
	}
	defer resp.Body.Close()

	return resp.StatusCode == http.StatusOK, nil
}

// secretExists checks if a secret already exists.
func secretExists(endpoint, secretID string) (bool, error) {
	url := fmt.Sprintf("%s/api/v1/secrets/%s", endpoint, secretID)

	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return false, &qberrors.WrappedMessageError{Message: "creating request", Cause: err}
	}

	client := helpers.NewHTTPClient(10 * time.Second)
	resp, err := client.Do(req)
	if err != nil {
		return false, &qberrors.WrappedMessageError{Message: "sending request", Cause: err}
	}
	defer resp.Body.Close()

	return resp.StatusCode == http.StatusOK, nil
}

// getTestSecretName returns the full name of a test secret.
func getTestSecretName(baseName string) string {
	return testSecretPrefix + baseName
}

// getTestSecretExpectedValue returns the expected value for a test secret.
func getTestSecretExpectedValue(baseName string) string {
	return fmt.Sprintf("test-value-for-%s", baseName)
}
