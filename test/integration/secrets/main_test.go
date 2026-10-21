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

// TestMain sets up the test environment before running secrets tests.
// It creates the required KMS encryption key for secrets encryption.
func TestMain(m *testing.M) {
	// Set up the secrets encryption key before running tests
	if err := setupSecretsEncryptionKey(); err != nil {
		fmt.Fprintf(os.Stderr, "Failed to set up secrets encryption key: %v\n", err)
		os.Exit(1)
	}

	// Run tests
	code := m.Run()
	if err := helpers.ReleaseDataPlaneTenants(); err != nil {
		fmt.Fprintf(os.Stderr, "release data-plane tenants: %v\n", err)
	}

	// Exit with the test result code
	os.Exit(code)
}

// setupSecretsEncryptionKey creates the default encryption key used for secrets.
func setupSecretsEncryptionKey() error {
	endpoint := os.Getenv("QUICBAAS_NODE1_URL")
	if endpoint == "" {
		endpoint = "https://test-node1:8080"
	}

	// Default key name used by secrets encryption
	keyName := "quicbaas-secrets-key"

	// Check if key already exists
	exists, err := keyExists(endpoint, keyName)
	if err != nil {
		return &qberrors.WrappedMessageError{Message: "checking key existence", Cause: err}
	}

	if exists {
		fmt.Printf("Secrets encryption key '%s' already exists\n", keyName)
		return nil
	}

	// Create the encryption key
	reqBody := map[string]interface{}{
		"name": keyName,
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

	client, err := helpers.TenantHTTPClient(context.Background(), 30*time.Second)
	if err != nil {
		return &qberrors.WrappedMessageError{Message: "tenant principal client", Cause: err}
	}
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

	fmt.Printf("Created secrets encryption key '%s'\n", keyName)
	return nil
}

// keyExists checks if a KMS key already exists.
func keyExists(endpoint, keyID string) (bool, error) {
	url := fmt.Sprintf("%s/api/v1/kms/keys/%s", endpoint, keyID)

	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return false, err
	}

	client, err := helpers.TenantHTTPClient(context.Background(), 10*time.Second)
	if err != nil {
		return false, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return false, err
	}
	defer resp.Body.Close()

	return resp.StatusCode == http.StatusOK, nil
}
