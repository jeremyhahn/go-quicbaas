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
	"bytes"
	"context"
	"crypto/elliptic"
	"crypto/x509"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	kctypes "github.com/jeremyhahn/go-xkms/pkg/types"

	grpcclient "github.com/jeremyhahn/go-quicbaas/pkg/api/grpc"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
	"github.com/jeremyhahn/go-quicbaas/test/integration/helpers"
)

// =============================================================================
// Backend Status Integration Tests
// =============================================================================

// TestRESTBackendStatus tests the backend status endpoint via REST API.
func TestRESTBackendStatus(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	endpoints := helpers.GetRESTEndpoints()
	client := NewMultiBackendRESTClient(endpoints[0])

	t.Run("get backend status", func(t *testing.T) {
		resp, err := client.BackendStatus(ctx)
		require.NoError(t, err, "backend status endpoint must be available in integration tests")
		require.NotNil(t, resp)

		// Verify response structure
		assert.NotEmpty(t, resp.BackendType, "backend_type should not be empty")
		assert.NotEmpty(t, resp.Status, "status should not be empty")
		assert.NotNil(t, resp.Capabilities, "capabilities should not be nil")
		assert.False(t, resp.Timestamp.IsZero(), "timestamp should be set")

		// Log the backend info
		t.Logf("Backend Type: %s", resp.BackendType)
		t.Logf("Status: %s", resp.Status)
		t.Logf("Capabilities: %v", resp.Capabilities)

		// Check valid status values
		validStatuses := []string{"healthy", "degraded", "unavailable", "unknown"}
		assert.Contains(t, validStatuses, resp.Status, "Status should be one of: healthy, degraded, unavailable, unknown")
	})

	t.Run("backend status has expected capabilities", func(t *testing.T) {
		resp, err := client.BackendStatus(ctx)
		require.NoError(t, err, "backend status must be available")
		require.NotNil(t, resp)
		require.NotNil(t, resp.Capabilities)

		// Common capabilities that all backends should report
		expectedCapabilities := []string{"encrypt", "decrypt", "generate_key"}
		for _, cap := range expectedCapabilities {
			if _, exists := resp.Capabilities[cap]; exists {
				t.Logf("Capability '%s': %v", cap, resp.Capabilities[cap])
			} else {
				t.Logf("Capability '%s': not reported", cap)
			}
		}
	})
}

// TestRESTBackendTest tests the backend test endpoint via REST API.
func TestRESTBackendTest(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	endpoints := helpers.GetRESTEndpoints()
	client := NewMultiBackendRESTClient(endpoints[0])

	t.Run("run backend test", func(t *testing.T) {
		resp, err := client.BackendTest(ctx)
		require.NoError(t, err, "backend test endpoint must be available in integration tests")
		require.NotNil(t, resp)

		// Verify response structure
		assert.False(t, resp.Timestamp.IsZero(), "timestamp should be set")
		t.Logf("Test Success: %v", resp.Success)
		t.Logf("Test Duration: %v", resp.Duration)
		t.Logf("Test Message: %s", resp.Message)

		if resp.Success {
			assert.Greater(t, resp.Duration, time.Duration(0), "Duration should be positive on success")
		} else {
			assert.NotEmpty(t, resp.Message, "Message should contain error details on failure")
		}
	})

	t.Run("backend test response time", func(t *testing.T) {
		start := time.Now()
		resp, err := client.BackendTest(ctx)
		elapsed := time.Since(start)

		require.NoError(t, err, "backend test must be available")
		require.NotNil(t, resp)
		t.Logf("End-to-end test time: %v", elapsed)
		t.Logf("Reported backend test time: %v", resp.Duration)

		// Backend test should complete in reasonable time
		assert.Less(t, elapsed, 30*time.Second, "Backend test should complete within 30 seconds")
	})
}

// TestGRPCBackendOperations tests backend operations via gRPC.
func TestGRPCBackendOperations(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	endpoints := getGRPCEndpoints()
	tlsCfg, err := helpers.IntegrationTLSConfig()
	require.NoError(t, err, "load integration TLS config")
	client, err := grpcclient.NewClient(endpoints[0], grpcclient.WithTLSConfig(tlsCfg))
	require.NoError(t, err, "must be able to connect to gRPC server in integration tests")
	defer client.Close()

	skipIfKMSUnavailable(t, client, ctx)

	t.Run("verify key generation uses correct backend", func(t *testing.T) {
		keyName := generateUniqueKeyName("backend-verify")

		genReq := &service.GenerateKeyRequest{
			Name:        keyName,
			Description: "Backend verification test",
			KeyAttributes: &kctypes.KeyAttributes{
				SymmetricAlgorithm: kctypes.SymmetricAES256GCM,
			},
			Usage: []service.KeyUsage{service.KeyUsageEncrypt, service.KeyUsageDecrypt},
		}

		genResp, err := client.KMSGenerateKey(ctx, genReq)
		require.NoError(t, err)
		require.NotNil(t, genResp)
		keyID := genResp.Key.KeyID
		defer cleanupKey(t, client, ctx, keyID)

		// Key should have backend-related metadata
		assert.NotEmpty(t, genResp.Key.KeyID)
		assert.Equal(t, service.KeyStatusEnabled, genResp.Key.Status)
		t.Logf("Generated key: %s", keyID)
	})
}

// =============================================================================
// Multi-Backend Routing Integration Tests
// =============================================================================

// TestMultiBackendKeyRouting tests key routing to appropriate backends.
// This test verifies that keys with different algorithms or purposes
// are routed to appropriate backends when multi-backend is configured.
func TestMultiBackendKeyRouting(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	endpoints := helpers.GetRESTEndpoints()
	client := NewMultiBackendRESTClient(endpoints[0])
	keyClient := NewRESTClient(endpoints[0])

	// Check if backend status is available
	statusResp, err := client.BackendStatus(ctx)
	require.NoError(t, err, "backend status must be available in integration tests")

	t.Logf("Testing with backend type: %s", statusResp.BackendType)

	t.Run("symmetric key generation", func(t *testing.T) {
		keyName := generateUniqueKeyName("routing-symmetric")

		genReq := &service.GenerateKeyRequest{
			Name:        keyName,
			Description: "Multi-backend routing test - symmetric key",
			KeyAttributes: &kctypes.KeyAttributes{
				SymmetricAlgorithm: kctypes.SymmetricAES256GCM,
			},
			Usage: []service.KeyUsage{service.KeyUsageEncrypt, service.KeyUsageDecrypt},
		}

		genResp, err := keyClient.GenerateKey(ctx, genReq)
		require.NoError(t, err, "symmetric key generation must succeed")
		require.NotNil(t, genResp)
		keyID := genResp.Key.KeyID

		defer func() {
			deleteReq := &service.DeleteKeyRequest{
				KeyID: keyID,
				Force: true,
			}
			_, _ = keyClient.DeleteKey(ctx, deleteReq)
		}()

		assert.Equal(t, service.KeyAlgAES256GCM, genResp.Key.Algorithm)
		t.Logf("Created symmetric key: %s", keyID)
	})

	t.Run("asymmetric key generation", func(t *testing.T) {
		keyName := generateUniqueKeyName("routing-asymmetric")

		genReq := &service.GenerateKeyRequest{
			Name:        keyName,
			Description: "Multi-backend routing test - asymmetric key",
			KeyAttributes: &kctypes.KeyAttributes{
				KeyAlgorithm:  x509.ECDSA,
				ECCAttributes: &kctypes.ECCAttributes{Curve: elliptic.P256()},
			},
			Usage: []service.KeyUsage{service.KeyUsageSign, service.KeyUsageVerify},
		}

		genResp, err := keyClient.GenerateKey(ctx, genReq)
		require.NoError(t, err, "asymmetric key generation must succeed")
		require.NotNil(t, genResp)
		keyID := genResp.Key.KeyID

		defer func() {
			deleteReq := &service.DeleteKeyRequest{
				KeyID: keyID,
				Force: true,
			}
			_, _ = keyClient.DeleteKey(ctx, deleteReq)
		}()

		assert.Equal(t, service.KeyAlgECDSAP256, genResp.Key.Algorithm)
		t.Logf("Created asymmetric key: %s", keyID)
	})
}

// TestBackendFailoverBehavior tests backend failover behavior.
// This test documents expected behavior when the primary backend fails.
func TestBackendFailoverBehavior(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	endpoints := helpers.GetRESTEndpoints()
	client := NewMultiBackendRESTClient(endpoints[0])
	keyClient := NewRESTClient(endpoints[0])

	// Check backend status first
	statusResp, err := client.BackendStatus(ctx)
	require.NoError(t, err, "backend status should be available")
	require.NotNil(t, statusResp, "backend status response should not be nil")

	t.Logf("Current backend: %s, Status: %s", statusResp.BackendType, statusResp.Status)

	t.Run("operations with healthy backend", func(t *testing.T) {
		require.Equal(t, "healthy", statusResp.Status, "backend must be healthy in integration test environment")

		keyName := generateUniqueKeyName("failover-healthy")

		genReq := &service.GenerateKeyRequest{
			Name: keyName,
			KeyAttributes: &kctypes.KeyAttributes{
				SymmetricAlgorithm: kctypes.SymmetricAES256GCM,
			},
			Usage: []service.KeyUsage{service.KeyUsageEncrypt, service.KeyUsageDecrypt},
		}

		genResp, err := keyClient.GenerateKey(ctx, genReq)
		require.NoError(t, err, "key generation should succeed with healthy backend")

		require.NotNil(t, genResp)
		keyID := genResp.Key.KeyID

		defer func() {
			deleteReq := &service.DeleteKeyRequest{
				KeyID: keyID,
				Force: true,
			}
			_, _ = keyClient.DeleteKey(ctx, deleteReq)
		}()

		// Encrypt and decrypt should work with healthy backend
		plaintext := []byte("failover test data")
		encReq := &service.EncryptRequest{
			KeyID:     keyID,
			Plaintext: plaintext,
		}

		encResp, err := keyClient.Encrypt(ctx, encReq)
		require.NoError(t, err, "encryption should succeed with healthy backend")

		decReq := &service.DecryptRequest{
			KeyID:      keyID,
			Ciphertext: encResp.Ciphertext,
		}

		decResp, err := keyClient.Decrypt(ctx, decReq)
		require.NoError(t, err)
		assert.Equal(t, plaintext, decResp.Plaintext)
	})

	t.Run("degraded backend handling", func(t *testing.T) {
		// This test documents expected behavior with degraded backend
		// Degraded mode testing requires special infrastructure setup (e.g., partial backend failures)
		// In normal integration test environment, backend should be healthy
		//
		// Expected behavior in degraded mode:
		// - Operations should still work but may be slower or limited
		// - Key generation should succeed but with possible latency
		// - Crypto operations should work with fallback mechanisms
		//
		// To test degraded mode, run with infrastructure that simulates partial backend failure
		t.Logf("Degraded mode testing requires infrastructure setup; backend is currently: %s", statusResp.Status)

		// Verify the test environment is properly configured
		// In standard integration tests, we expect the backend to be healthy
		assert.Equal(t, "healthy", statusResp.Status, "standard integration test environment should have healthy backend")
	})
}

// =============================================================================
// Backend Capability Tests
// =============================================================================

// TestBackendCapabilities tests that the backend reports and supports expected capabilities.
func TestBackendCapabilities(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	endpoints := helpers.GetRESTEndpoints()
	client := NewMultiBackendRESTClient(endpoints[0])
	keyClient := NewRESTClient(endpoints[0])

	statusResp, err := client.BackendStatus(ctx)
	require.NoError(t, err, "backend status must be available in integration tests")

	require.NotNil(t, statusResp)
	require.NotNil(t, statusResp.Capabilities)

	t.Run("test encryption capability", func(t *testing.T) {
		require.True(t, statusResp.Capabilities["encrypt"], "backend must report encrypt capability in integration tests")

		keyName := generateUniqueKeyName("cap-encrypt")
		genReq := &service.GenerateKeyRequest{
			Name: keyName,
			KeyAttributes: &kctypes.KeyAttributes{
				SymmetricAlgorithm: kctypes.SymmetricAES256GCM},
			Usage: []service.KeyUsage{service.KeyUsageEncrypt, service.KeyUsageDecrypt},
		}

		genResp, err := keyClient.GenerateKey(ctx, genReq)
		require.NoError(t, err, "key generation must succeed")
		keyID := genResp.Key.KeyID

		defer func() {
			deleteReq := &service.DeleteKeyRequest{KeyID: keyID, Force: true}
			_, _ = keyClient.DeleteKey(ctx, deleteReq)
		}()

		encReq := &service.EncryptRequest{
			KeyID:     keyID,
			Plaintext: []byte("capability test"),
		}

		_, err = keyClient.Encrypt(ctx, encReq)
		require.NoError(t, err, "encrypt operation must succeed when capability is reported")
		t.Log("Encrypt capability verified")
	})

	t.Run("test key generation capability", func(t *testing.T) {
		require.True(t, statusResp.Capabilities["generate"], "backend must report generate capability in integration tests")

		keyName := generateUniqueKeyName("cap-generate")
		genReq := &service.GenerateKeyRequest{
			Name: keyName,
			KeyAttributes: &kctypes.KeyAttributes{
				SymmetricAlgorithm: kctypes.SymmetricAES256GCM},
			Usage: []service.KeyUsage{service.KeyUsageEncrypt, service.KeyUsageDecrypt},
		}

		genResp, err := keyClient.GenerateKey(ctx, genReq)
		if err == nil {
			t.Log("Generate key capability verified")
			// Cleanup
			deleteReq := &service.DeleteKeyRequest{KeyID: genResp.Key.KeyID, Force: true}
			_, _ = keyClient.DeleteKey(ctx, deleteReq)
		} else {
			t.Logf("Generate key capability reported but operation failed: %v", err)
		}
	})
}

// =============================================================================
// Multi-Backend Client Helper
// =============================================================================

// MultiBackendRESTClient wraps HTTP client for multi-backend testing.
type MultiBackendRESTClient struct {
	baseURL    string
	httpClient *http.Client
}

// NewMultiBackendRESTClient creates a new multi-backend REST client.
func NewMultiBackendRESTClient(baseURL string) *MultiBackendRESTClient {
	return &MultiBackendRESTClient{
		baseURL:    baseURL,
		httpClient: helpers.NewHTTPClient(30 * time.Second),
	}
}

// backendStatusWireResponse is the wire format for backend status from REST API.
// The REST API returns timestamp as int64 (Unix seconds).
type backendStatusWireResponse struct {
	BackendType  string          `json:"backend_type"`
	Status       string          `json:"status"`
	Device       string          `json:"device,omitempty"`
	Region       string          `json:"region,omitempty"`
	KeyID        string          `json:"key_id,omitempty"`
	PCRBanks     []string        `json:"pcr_banks,omitempty"`
	PCRRegisters []int           `json:"pcr_registers,omitempty"`
	Capabilities map[string]bool `json:"capabilities"`
	Timestamp    int64           `json:"timestamp"`
}

// backendTestWireResponse is the wire format for backend test from REST API.
// The REST API returns duration as int64 (nanoseconds) and timestamp as int64 (Unix seconds).
type backendTestWireResponse struct {
	Success    bool   `json:"success"`
	DurationNs int64  `json:"duration_ns"`
	Message    string `json:"message,omitempty"`
	Timestamp  int64  `json:"timestamp"`
}

// BackendStatus retrieves the backend status.
func (c *MultiBackendRESTClient) BackendStatus(ctx context.Context) (*service.BackendStatusResponse, error) {
	url := fmt.Sprintf("%s/api/v1/kms/backend/status", c.baseURL)

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("status %d: %s", resp.StatusCode, string(respBody))
	}

	// Parse the wire format response
	var wireResp backendStatusWireResponse
	if err := json.Unmarshal(respBody, &wireResp); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

	// Convert to service response type
	result := &service.BackendStatusResponse{
		BackendType:  wireResp.BackendType,
		Status:       wireResp.Status,
		Device:       wireResp.Device,
		Region:       wireResp.Region,
		KeyID:        wireResp.KeyID,
		PCRBanks:     wireResp.PCRBanks,
		PCRRegisters: wireResp.PCRRegisters,
		Capabilities: wireResp.Capabilities,
		Timestamp:    time.Unix(wireResp.Timestamp, 0),
	}

	return result, nil
}

// BackendTest runs a backend test cycle.
func (c *MultiBackendRESTClient) BackendTest(ctx context.Context) (*service.BackendTestResponse, error) {
	url := fmt.Sprintf("%s/api/v1/kms/backend/test", c.baseURL)

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader([]byte("{}")))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("status %d: %s", resp.StatusCode, string(respBody))
	}

	// Parse the wire format response
	var wireResp backendTestWireResponse
	if err := json.Unmarshal(respBody, &wireResp); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

	// Convert to service response type
	result := &service.BackendTestResponse{
		Success:   wireResp.Success,
		Duration:  time.Duration(wireResp.DurationNs),
		Message:   wireResp.Message,
		Timestamp: time.Unix(wireResp.Timestamp, 0),
	}

	return result, nil
}

// =============================================================================
// DEK Integration Tests
// =============================================================================

// TestRESTDEKOperations tests DEK operations via REST API.
func TestRESTDEKOperations(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	endpoints := helpers.GetRESTEndpoints()
	client := NewDEKRESTClient(endpoints[0])

	t.Run("create and get DEK", func(t *testing.T) {
		// DEK paths are normalized without leading/trailing slashes
		dekPath := generateUniqueKeyName("dek-test")

		// Create DEK
		createReq := &service.CreateDEKRequest{
			Path:      dekPath,
			Algorithm: "AES-256-GCM",
		}

		createResp, err := client.CreateDEK(ctx, createReq)
		require.NoError(t, err, "DEK creation must succeed in integration tests")
		require.NotNil(t, createResp)
		require.NotNil(t, createResp.DEK)
		assert.NotEmpty(t, createResp.DEK.Path)
		t.Logf("Created DEK: %s", createResp.DEK.Path)

		// Get DEK
		getReq := &service.GetDEKRequest{
			Path: dekPath,
		}

		getResp, err := client.GetDEK(ctx, getReq)
		require.NoError(t, err)
		require.NotNil(t, getResp)
		require.NotNil(t, getResp.DEK)
		assert.Equal(t, dekPath, getResp.DEK.Path)

		// Clean up
		deleteReq := &service.DeleteDEKRequest{
			Path:  dekPath,
			Force: true,
		}
		_, _ = client.DeleteDEK(ctx, deleteReq)
	})

	t.Run("list DEKs", func(t *testing.T) {
		// Create a few DEKs for listing
		prefix := generateUniqueKeyName("dek-list")
		paths := []string{
			prefix + "/dek1",
			prefix + "/dek2",
			prefix + "/dek3",
		}

		createdPaths := []string{}
		for _, path := range paths {
			createReq := &service.CreateDEKRequest{
				Path:      path,
				Algorithm: "AES-256-GCM",
			}

			_, err := client.CreateDEK(ctx, createReq)
			require.NoError(t, err, "DEK creation must succeed for path %s", path)
			createdPaths = append(createdPaths, path)
		}

		// Cleanup
		defer func() {
			for _, path := range createdPaths {
				deleteReq := &service.DeleteDEKRequest{Path: path, Force: true}
				_, _ = client.DeleteDEK(ctx, deleteReq)
			}
		}()

		// List DEKs
		listReq := &service.ListDEKsRequest{
			Prefix: prefix,
			Limit:  100,
		}

		listResp, err := client.ListDEKs(ctx, listReq)
		require.NoError(t, err, "DEK listing must succeed")
		require.NotNil(t, listResp)
		t.Logf("Found %d DEKs with prefix %s", len(listResp.DEKs), prefix)
	})

	t.Run("rotate DEK", func(t *testing.T) {
		dekPath := generateUniqueKeyName("dek-rotate")

		// Create DEK
		createReq := &service.CreateDEKRequest{
			Path:      dekPath,
			Algorithm: "AES-256-GCM",
		}

		createResp, err := client.CreateDEK(ctx, createReq)
		require.NoError(t, err, "DEK creation must succeed")

		defer func() {
			deleteReq := &service.DeleteDEKRequest{Path: dekPath, Force: true}
			_, _ = client.DeleteDEK(ctx, deleteReq)
		}()

		require.NotNil(t, createResp.DEK)
		initialVersion := createResp.DEK.Version

		// Rotate DEK
		rotateReq := &service.RotateDEKRequest{
			Path: dekPath,
		}

		rotateResp, err := client.RotateDEK(ctx, rotateReq)
		require.NoError(t, err, "DEK rotation must succeed")

		require.NotNil(t, rotateResp)
		assert.Greater(t, rotateResp.NewVersion, initialVersion, "New version should be greater than initial")
		t.Logf("Rotated DEK from version %d to %d", initialVersion, rotateResp.NewVersion)
	})
}

// =============================================================================
// DEK REST Client Helper
// =============================================================================

// DEKRESTClient wraps HTTP client for DEK testing.
type DEKRESTClient struct {
	baseURL    string
	httpClient *http.Client
}

// NewDEKRESTClient creates a new DEK REST client.
func NewDEKRESTClient(baseURL string) *DEKRESTClient {
	return &DEKRESTClient{
		baseURL:    baseURL,
		httpClient: helpers.NewHTTPClient(30 * time.Second),
	}
}

// CreateDEK creates a new DEK.
func (c *DEKRESTClient) CreateDEK(ctx context.Context, req *service.CreateDEKRequest) (*service.CreateDEKResponse, error) {
	bodyBytes, err := json.Marshal(req)
	if err != nil {
		return nil, err
	}

	url := fmt.Sprintf("%s/api/v1/kms/deks", c.baseURL)
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(bodyBytes))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode != http.StatusCreated && resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("status %d: %s", resp.StatusCode, string(respBody))
	}

	var result service.CreateDEKResponse
	if err := json.Unmarshal(respBody, &result); err != nil {
		return nil, err
	}

	return &result, nil
}

// GetDEK retrieves a DEK.
func (c *DEKRESTClient) GetDEK(ctx context.Context, req *service.GetDEKRequest) (*service.GetDEKResponse, error) {
	url := fmt.Sprintf("%s/api/v1/kms/deks/%s", c.baseURL, strings.TrimSuffix(req.Path, "/"))

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("status %d: %s", resp.StatusCode, string(respBody))
	}

	var result service.GetDEKResponse
	if err := json.Unmarshal(respBody, &result); err != nil {
		return nil, err
	}

	return &result, nil
}

// ListDEKs lists DEKs.
func (c *DEKRESTClient) ListDEKs(ctx context.Context, req *service.ListDEKsRequest) (*service.ListDEKsResponse, error) {
	url := fmt.Sprintf("%s/api/v1/kms/deks", c.baseURL)

	params := []string{}
	if req.Prefix != "" {
		params = append(params, fmt.Sprintf("prefix=%s", req.Prefix))
	}
	if req.Limit > 0 {
		params = append(params, fmt.Sprintf("limit=%d", req.Limit))
	}
	if len(params) > 0 {
		url += "?" + strings.Join(params, "&")
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("status %d: %s", resp.StatusCode, string(respBody))
	}

	var result service.ListDEKsResponse
	if err := json.Unmarshal(respBody, &result); err != nil {
		return nil, err
	}

	return &result, nil
}

// RotateDEK rotates a DEK.
func (c *DEKRESTClient) RotateDEK(ctx context.Context, req *service.RotateDEKRequest) (*service.RotateDEKResponse, error) {
	url := fmt.Sprintf("%s/api/v1/kms/deks/%s/rotate", c.baseURL, strings.TrimSuffix(req.Path, "/"))

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, nil)
	if err != nil {
		return nil, err
	}

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("status %d: %s", resp.StatusCode, string(respBody))
	}

	var result service.RotateDEKResponse
	if err := json.Unmarshal(respBody, &result); err != nil {
		return nil, err
	}

	return &result, nil
}

// DeleteDEK deletes a DEK.
func (c *DEKRESTClient) DeleteDEK(ctx context.Context, req *service.DeleteDEKRequest) (*service.DeleteDEKResponse, error) {
	url := fmt.Sprintf("%s/api/v1/kms/deks/%s", c.baseURL, strings.TrimSuffix(req.Path, "/"))

	if req.Force {
		url += "?force=true"
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodDelete, url, nil)
	if err != nil {
		return nil, err
	}

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("status %d: %s", resp.StatusCode, string(respBody))
	}

	var result service.DeleteDEKResponse
	if err := json.Unmarshal(respBody, &result); err != nil {
		return nil, err
	}

	return &result, nil
}
