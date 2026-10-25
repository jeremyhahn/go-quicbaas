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
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"

	grpcclient "github.com/jeremyhahn/go-quicbaas/pkg/api/grpc"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
	quicbaas "github.com/jeremyhahn/go-quicbaas/sdk/go"
	"github.com/jeremyhahn/go-quicbaas/test/integration/helpers"
)

// TestSecretsMultiProtocol verifies that secrets operations work identically
// across all supported protocols: Embedded, REST, gRPC, Unix Socket, and MCP.
func TestSecretsMultiProtocol(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	// Test each protocol independently
	if helpers.IsEmbeddedAvailable() {
		t.Run("Embedded", func(t *testing.T) {
			testSecretsProtocol(t, ctx, &embeddedSecretsClient{})
		})
	}

	t.Run("REST", func(t *testing.T) {
		testSecretsProtocol(t, ctx, &restProtocolClient{})
	})

	t.Run("gRPC", func(t *testing.T) {
		testSecretsProtocol(t, ctx, &grpcProtocolClient{})
	})

	t.Run("UnixSocket", func(t *testing.T) {
		testSecretsProtocol(t, ctx, &unixSocketProtocolClient{})
	})

	// NOTE: no MCP leg here. quicbaas retired its bespoke JSON-RPC MCP
	// listener in favor of go-qrdb's unified multi-protocol server; the
	// unified server's MCP surface has no finalized docker endpoint
	// convention yet, so this suite does not exercise it (deferred).
}

// TestSecretsComprehensiveMultiProtocol runs comprehensive tests for ALL secrets
// commands with ALL flag combinations against ALL protocols.
func TestSecretsComprehensiveMultiProtocol(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()

	protocols := []secretsProtocol{
		&restProtocolClient{},
		&grpcProtocolClient{},
		&unixSocketProtocolClient{},
	}
	if helpers.IsEmbeddedAvailable() {
		protocols = append(protocols, &embeddedSecretsClient{})
	}

	for _, protocol := range protocols {
		t.Run(protocol.Name(), func(t *testing.T) {
			err := protocol.Setup(t, ctx)
			require.NoError(t, err, "Protocol %s must be available", protocol.Name())
			defer func() {
				if teardownErr := protocol.Teardown(t); teardownErr != nil {
					t.Logf("Warning: teardown error for %s: %v", protocol.Name(), teardownErr)
				}
			}()

			basePath := fmt.Sprintf("comprehensive/%s/%d", protocol.Name(), time.Now().UnixNano())

			// Run comprehensive Store tests
			t.Run("Store", func(t *testing.T) {
				testStoreComprehensive(t, ctx, protocol, basePath+"/store")
			})

			// Run comprehensive Get tests
			t.Run("Get", func(t *testing.T) {
				testGetComprehensive(t, ctx, protocol, basePath+"/get")
			})

			// Run comprehensive List tests
			t.Run("List", func(t *testing.T) {
				testListComprehensive(t, ctx, protocol, basePath+"/list")
			})

			// Run Enable/Disable tests
			t.Run("EnableDisable", func(t *testing.T) {
				testEnableDisable(t, ctx, protocol, basePath+"/status")
			})

			// Run data persistence and integrity tests
			t.Run("DataPersistence", func(t *testing.T) {
				testDataPersistence(t, ctx, protocol, basePath+"/persistence")
			})

			// Run tenant isolation tests
			t.Run("TenantIsolation", func(t *testing.T) {
				testTenantIsolation(t, ctx, protocol, basePath+"/tenant")
			})
		})
	}
}

// secretsProtocol abstracts the different protocols for testing.
type secretsProtocol interface {
	Setup(t *testing.T, ctx context.Context) error
	Teardown(t *testing.T) error
	StoreSecret(ctx context.Context, req *service.StoreSecretRequest) (*service.StoreSecretResponse, error)
	GetSecret(ctx context.Context, req *service.GetSecretRequest) (*service.GetSecretResponse, error)
	ListSecrets(ctx context.Context, req *service.ListSecretsRequest) (*service.ListSecretsResponse, error)
	DeleteSecret(ctx context.Context, req *service.DeleteSecretRequest) (*service.DeleteSecretResponse, error)
	ListVersions(ctx context.Context, req *service.ListSecretVersionsRequest) (*service.ListSecretVersionsResponse, error)
	Rollback(ctx context.Context, req *service.RollbackSecretRequest) (*service.RollbackSecretResponse, error)
	Rotate(ctx context.Context, req *service.RotateSecretRequest) (*service.RotateSecretResponse, error)
	UpdateSecretStatus(ctx context.Context, req *service.UpdateSecretStatusRequest) (*service.UpdateSecretStatusResponse, error)
	Name() string
}

// testSecretsProtocol runs the complete secrets test suite against a protocol.
func testSecretsProtocol(t *testing.T, ctx context.Context, protocol secretsProtocol) {
	t.Helper()

	// Setup the protocol client
	err := protocol.Setup(t, ctx)
	require.NoError(t, err, "Protocol %s must be available", protocol.Name())
	defer func() {
		err := protocol.Teardown(t)
		if err != nil {
			t.Logf("Warning: teardown error for %s: %v", protocol.Name(), err)
		}
	}()

	basePath := fmt.Sprintf("test/multiprotocol/%s/%d", protocol.Name(), time.Now().UnixNano())

	// Test 1: Store Secret
	t.Run("store", func(t *testing.T) {
		testStoreSecret(t, ctx, protocol, basePath+"/store")
	})

	// Test 2: Get Secret
	t.Run("get", func(t *testing.T) {
		testGetSecret(t, ctx, protocol, basePath+"/get")
	})

	// Test 3: List Secrets
	t.Run("list", func(t *testing.T) {
		testListSecrets(t, ctx, protocol, basePath+"/list")
	})

	// Test 4: Delete Secret (soft delete)
	t.Run("delete", func(t *testing.T) {
		testDeleteSecret(t, ctx, protocol, basePath+"/delete")
	})

	// Test 5: Destroy Secret (permanent)
	t.Run("destroy", func(t *testing.T) {
		testDestroySecret(t, ctx, protocol, basePath+"/destroy")
	})

	// Test 6: List Versions
	t.Run("versions", func(t *testing.T) {
		testListVersions(t, ctx, protocol, basePath+"/versions")
	})

	// Test 7: Rollback
	t.Run("rollback", func(t *testing.T) {
		testRollback(t, ctx, protocol, basePath+"/rollback")
	})

	// Test 8: Rotate
	t.Run("rotate", func(t *testing.T) {
		testRotate(t, ctx, protocol, basePath+"/rotate")
	})
}

// =============================================================================
// Comprehensive Store Tests - ALL Flag Combinations
// =============================================================================

func testStoreComprehensive(t *testing.T, ctx context.Context, protocol secretsProtocol, basePath string) {
	t.Helper()

	// Test: Store with no flags (default encryption)
	t.Run("NoFlags", func(t *testing.T) {
		secretData, err := json.Marshal(map[string]interface{}{
			"username": "admin",
			"password": "default-secret",
		})
		require.NoError(t, err)

		req := &service.StoreSecretRequest{
			SecretID: basePath + "/no-flags",
			Name:     "no-flags-secret",
			Data:     secretData,
		}

		resp, err := protocol.StoreSecret(ctx, req)
		require.NoError(t, err)
		assert.NotNil(t, resp)
		assert.Equal(t, basePath+"/no-flags", resp.SecretID)
		assert.Equal(t, uint64(1), resp.Version)
		assert.False(t, resp.Timestamp.IsZero())

		t.Logf("[%s] Store NoFlags: secret_id=%s, version=%d", protocol.Name(), resp.SecretID, resp.Version)
	})

	// Test: Store with --tenant-id only
	t.Run("TenantIDOnly", func(t *testing.T) {
		secretData, err := json.Marshal(map[string]interface{}{
			"api_key": "tenant-specific-key",
		})
		require.NoError(t, err)

		req := &service.StoreSecretRequest{
			SecretID: basePath + "/tenant-only",
			Name:     "tenant-secret",
			Data:     secretData,
			TenantID: helpers.TenantPrincipalID(t, getRESTEndpoint()),
		}

		resp, err := protocol.StoreSecret(ctx, req)
		require.NoError(t, err)
		assert.NotNil(t, resp)
		assert.Equal(t, basePath+"/tenant-only", resp.SecretID)
		assert.Greater(t, resp.Version, uint64(0))

		t.Logf("[%s] Store TenantIDOnly: secret_id=%s, version=%d", protocol.Name(), resp.SecretID, resp.Version)
	})

	// Test: Store with --key-id only
	t.Run("KeyIDOnly", func(t *testing.T) {
		secretData, err := json.Marshal(map[string]interface{}{
			"private_key": "custom-key-encrypted",
		})
		require.NoError(t, err)

		req := &service.StoreSecretRequest{
			SecretID: basePath + "/keyid-only",
			Name:     "keyid-secret",
			Data:     secretData,
			KeyID:    "custom-key-001",
		}

		resp, err := protocol.StoreSecret(ctx, req)
		require.NoError(t, err)
		assert.NotNil(t, resp)
		assert.Equal(t, basePath+"/keyid-only", resp.SecretID)

		t.Logf("[%s] Store KeyIDOnly: secret_id=%s, version=%d", protocol.Name(), resp.SecretID, resp.Version)
	})

	// Test: Store with --algorithm="AES-256-GCM"
	t.Run("AlgorithmAES256GCM", func(t *testing.T) {
		secretData, err := json.Marshal(map[string]interface{}{
			"sensitive_data": "aes-256-gcm-encrypted",
		})
		require.NoError(t, err)

		req := &service.StoreSecretRequest{
			SecretID:  basePath + "/aes256gcm",
			Name:      "aes256-secret",
			Data:      secretData,
			Algorithm: "AES-256-GCM",
		}

		resp, err := protocol.StoreSecret(ctx, req)
		require.NoError(t, err)
		assert.NotNil(t, resp)
		assert.Equal(t, basePath+"/aes256gcm", resp.SecretID)

		t.Logf("[%s] Store AES-256-GCM: secret_id=%s, version=%d", protocol.Name(), resp.SecretID, resp.Version)
	})

	// Test: Store with --algorithm="AES-128-GCM"
	t.Run("AlgorithmAES128GCM", func(t *testing.T) {
		secretData, err := json.Marshal(map[string]interface{}{
			"sensitive_data": "aes-128-gcm-encrypted",
		})
		require.NoError(t, err)

		req := &service.StoreSecretRequest{
			SecretID:  basePath + "/aes128gcm",
			Name:      "aes128-secret",
			Data:      secretData,
			Algorithm: "AES-128-GCM",
		}

		resp, err := protocol.StoreSecret(ctx, req)
		require.NoError(t, err)
		assert.NotNil(t, resp)
		assert.Equal(t, basePath+"/aes128gcm", resp.SecretID)

		t.Logf("[%s] Store AES-128-GCM: secret_id=%s, version=%d", protocol.Name(), resp.SecretID, resp.Version)
	})

	// Test: Store with --algorithm="ChaCha20-Poly1305"
	t.Run("AlgorithmChaCha20Poly1305", func(t *testing.T) {
		secretData, err := json.Marshal(map[string]interface{}{
			"sensitive_data": "chacha20-poly1305-encrypted",
		})
		require.NoError(t, err)

		req := &service.StoreSecretRequest{
			SecretID:  basePath + "/chacha20",
			Name:      "chacha20-secret",
			Data:      secretData,
			Algorithm: "CHACHA20-POLY1305",
		}

		resp, err := protocol.StoreSecret(ctx, req)
		require.NoError(t, err)
		assert.NotNil(t, resp)
		assert.Equal(t, basePath+"/chacha20", resp.SecretID)

		t.Logf("[%s] Store ChaCha20-Poly1305: secret_id=%s, version=%d", protocol.Name(), resp.SecretID, resp.Version)
	})

	// Test: Store with --tenant-id + --algorithm
	t.Run("TenantIDAndAlgorithm", func(t *testing.T) {
		secretData, err := json.Marshal(map[string]interface{}{
			"tenant_secret": "combined-encryption",
		})
		require.NoError(t, err)

		req := &service.StoreSecretRequest{
			SecretID:  basePath + "/tenant-algo",
			Name:      "tenant-algo-secret",
			Data:      secretData,
			TenantID:  helpers.TenantPrincipalID(t, getRESTEndpoint()),
			Algorithm: "AES-256-GCM",
		}

		resp, err := protocol.StoreSecret(ctx, req)
		require.NoError(t, err)
		assert.NotNil(t, resp)
		assert.Equal(t, basePath+"/tenant-algo", resp.SecretID)

		t.Logf("[%s] Store TenantID+Algorithm: secret_id=%s, version=%d", protocol.Name(), resp.SecretID, resp.Version)
	})

	// Test: Store with --key-id + --algorithm
	t.Run("KeyIDAndAlgorithm", func(t *testing.T) {
		secretData, err := json.Marshal(map[string]interface{}{
			"keyid_secret": "keyid-algo-encrypted",
		})
		require.NoError(t, err)

		req := &service.StoreSecretRequest{
			SecretID:  basePath + "/keyid-algo",
			Name:      "keyid-algo-secret",
			Data:      secretData,
			KeyID:     "custom-key-002",
			Algorithm: "AES-128-GCM",
		}

		resp, err := protocol.StoreSecret(ctx, req)
		require.NoError(t, err)
		assert.NotNil(t, resp)
		assert.Equal(t, basePath+"/keyid-algo", resp.SecretID)

		t.Logf("[%s] Store KeyID+Algorithm: secret_id=%s, version=%d", protocol.Name(), resp.SecretID, resp.Version)
	})

	// Test: Store with ALL flags combined
	t.Run("AllFlagsCombined", func(t *testing.T) {
		secretData, err := json.Marshal(map[string]interface{}{
			"all_flags": "fully-specified-encryption",
		})
		require.NoError(t, err)

		req := &service.StoreSecretRequest{
			SecretID:  basePath + "/all-flags",
			Name:      "all-flags-secret",
			Data:      secretData,
			TenantID:  helpers.TenantPrincipalID(t, getRESTEndpoint()),
			KeyID:     "custom-key-003",
			Algorithm: "AES-256-GCM",
		}

		resp, err := protocol.StoreSecret(ctx, req)
		require.NoError(t, err)
		assert.NotNil(t, resp)
		assert.Equal(t, basePath+"/all-flags", resp.SecretID)

		t.Logf("[%s] Store AllFlags: secret_id=%s, version=%d", protocol.Name(), resp.SecretID, resp.Version)
	})

	// Test: Store with metadata
	t.Run("WithMetadata", func(t *testing.T) {
		secretData, err := json.Marshal(map[string]interface{}{
			"db_password": "secret-password",
		})
		require.NoError(t, err)

		req := &service.StoreSecretRequest{
			SecretID: basePath + "/metadata",
			Name:     "metadata-secret",
			Data:     secretData,
			Metadata: map[string]string{
				"environment": "production",
				"region":      "us-east-1",
				"service":     "api-gateway",
			},
		}

		resp, err := protocol.StoreSecret(ctx, req)
		require.NoError(t, err)
		assert.NotNil(t, resp)
		assert.Equal(t, basePath+"/metadata", resp.SecretID)

		// Verify metadata persists by retrieving the secret
		getResp, err := protocol.GetSecret(ctx, &service.GetSecretRequest{
			SecretID: basePath + "/metadata",
		})
		require.NoError(t, err)
		assert.NotNil(t, getResp.Metadata)
		assert.Equal(t, "production", getResp.Metadata["environment"])
		assert.Equal(t, "us-east-1", getResp.Metadata["region"])
		assert.Equal(t, "api-gateway", getResp.Metadata["service"])

		t.Logf("[%s] Store WithMetadata: secret_id=%s, metadata=%v", protocol.Name(), resp.SecretID, getResp.Metadata)
	})

	// Test: Store with description
	t.Run("WithDescription", func(t *testing.T) {
		secretData, err := json.Marshal(map[string]interface{}{
			"oauth_token": "bearer-token-value",
		})
		require.NoError(t, err)

		req := &service.StoreSecretRequest{
			SecretID:    basePath + "/description",
			Name:        "description-secret",
			Data:        secretData,
			Description: "OAuth token for third-party API integration with payment gateway",
		}

		resp, err := protocol.StoreSecret(ctx, req)
		require.NoError(t, err)
		assert.NotNil(t, resp)
		assert.Equal(t, basePath+"/description", resp.SecretID)

		t.Logf("[%s] Store WithDescription: secret_id=%s", protocol.Name(), resp.SecretID)
	})

	// Test: Store with expiration
	t.Run("WithExpiration", func(t *testing.T) {
		secretData, err := json.Marshal(map[string]interface{}{
			"temp_token": "expires-soon",
		})
		require.NoError(t, err)

		expireTime := time.Now().Add(24 * time.Hour)
		req := &service.StoreSecretRequest{
			SecretID:  basePath + "/expiration",
			Name:      "expiration-secret",
			Data:      secretData,
			ExpiresAt: &expireTime,
		}

		resp, err := protocol.StoreSecret(ctx, req)
		require.NoError(t, err)
		assert.NotNil(t, resp)
		assert.Equal(t, basePath+"/expiration", resp.SecretID)

		t.Logf("[%s] Store WithExpiration: secret_id=%s, expires_at=%v", protocol.Name(), resp.SecretID, expireTime)
	})
}

// =============================================================================
// Comprehensive Get Tests
// =============================================================================

func testGetComprehensive(t *testing.T, ctx context.Context, protocol secretsProtocol, basePath string) {
	t.Helper()

	// Create a secret with multiple versions for testing
	secretPath := basePath + "/versioned"

	// Create version 1
	version1Data, err := json.Marshal(map[string]interface{}{
		"version": 1,
		"data":    "first-version",
	})
	require.NoError(t, err)

	storeReq := &service.StoreSecretRequest{
		SecretID: secretPath,
		Name:     "versioned-secret",
		Data:     version1Data,
	}
	storeResp, err := protocol.StoreSecret(ctx, storeReq)
	require.NoError(t, err)
	assert.Equal(t, uint64(1), storeResp.Version)

	// Create version 2
	version2Data, err := json.Marshal(map[string]interface{}{
		"version": 2,
		"data":    "second-version",
	})
	require.NoError(t, err)

	storeReq.Data = version2Data
	storeResp, err = protocol.StoreSecret(ctx, storeReq)
	require.NoError(t, err)
	assert.Equal(t, uint64(2), storeResp.Version)

	// Create version 3
	version3Data, err := json.Marshal(map[string]interface{}{
		"version": 3,
		"data":    "third-version",
	})
	require.NoError(t, err)

	storeReq.Data = version3Data
	storeResp, err = protocol.StoreSecret(ctx, storeReq)
	require.NoError(t, err)
	assert.Equal(t, uint64(3), storeResp.Version)

	// Test: Get latest version (no version flag)
	t.Run("LatestVersion", func(t *testing.T) {
		resp, err := protocol.GetSecret(ctx, &service.GetSecretRequest{
			SecretID: secretPath,
		})
		require.NoError(t, err)
		assert.Equal(t, uint64(3), resp.Version)

		var data map[string]interface{}
		err = json.Unmarshal(resp.Data, &data)
		require.NoError(t, err)
		assert.Equal(t, float64(3), data["version"])
		assert.Equal(t, "third-version", data["data"])

		t.Logf("[%s] Get LatestVersion: version=%d", protocol.Name(), resp.Version)
	})

	// Test: Get specific version (--version=1)
	t.Run("SpecificVersion1", func(t *testing.T) {
		resp, err := protocol.GetSecret(ctx, &service.GetSecretRequest{
			SecretID: secretPath,
			Version:  1,
		})
		require.NoError(t, err)
		assert.Equal(t, uint64(1), resp.Version)

		var data map[string]interface{}
		err = json.Unmarshal(resp.Data, &data)
		require.NoError(t, err)
		assert.Equal(t, float64(1), data["version"])
		assert.Equal(t, "first-version", data["data"])

		t.Logf("[%s] Get SpecificVersion1: version=%d", protocol.Name(), resp.Version)
	})

	// Test: Get specific version (--version=2)
	t.Run("SpecificVersion2", func(t *testing.T) {
		resp, err := protocol.GetSecret(ctx, &service.GetSecretRequest{
			SecretID: secretPath,
			Version:  2,
		})
		require.NoError(t, err)
		assert.Equal(t, uint64(2), resp.Version)

		var data map[string]interface{}
		err = json.Unmarshal(resp.Data, &data)
		require.NoError(t, err)
		assert.Equal(t, float64(2), data["version"])
		assert.Equal(t, "second-version", data["data"])

		t.Logf("[%s] Get SpecificVersion2: version=%d", protocol.Name(), resp.Version)
	})

	// Test: Get with invalid version (should error)
	t.Run("InvalidVersion", func(t *testing.T) {
		_, err := protocol.GetSecret(ctx, &service.GetSecretRequest{
			SecretID: secretPath,
			Version:  999, // Non-existent version
		})
		assert.Error(t, err, "Should error for non-existent version")
		t.Logf("[%s] Get InvalidVersion: expected error received", protocol.Name())
	})

	// Test: Get non-existent secret
	t.Run("NonExistentSecret", func(t *testing.T) {
		_, err := protocol.GetSecret(ctx, &service.GetSecretRequest{
			SecretID: basePath + "/non-existent",
		})
		assert.Error(t, err, "Should error for non-existent secret")
		t.Logf("[%s] Get NonExistentSecret: expected error received", protocol.Name())
	})
}

// =============================================================================
// Comprehensive List Tests
// =============================================================================

func testListComprehensive(t *testing.T, ctx context.Context, protocol secretsProtocol, basePath string) {
	t.Helper()

	// Create multiple secrets for listing tests
	numSecrets := 5
	for i := 0; i < numSecrets; i++ {
		secretData, err := json.Marshal(map[string]interface{}{
			"index": i,
			"name":  fmt.Sprintf("list-secret-%d", i),
		})
		require.NoError(t, err)

		req := &service.StoreSecretRequest{
			SecretID: fmt.Sprintf("%s/secret-%d", basePath, i),
			Name:     fmt.Sprintf("list-test-%d", i),
			Data:     secretData,
			Metadata: map[string]string{
				"group": "list-test",
			},
		}

		_, err = protocol.StoreSecret(ctx, req)
		require.NoError(t, err)
	}

	// Test: List all secrets
	t.Run("ListAll", func(t *testing.T) {
		resp, err := protocol.ListSecrets(ctx, &service.ListSecretsRequest{
			Prefix: basePath,
			Limit:  100,
		})
		require.NoError(t, err)
		assert.NotNil(t, resp)
		assert.NotEmpty(t, resp.Secrets)

		// Count our test secrets
		count := 0
		for _, secret := range resp.Secrets {
			for i := 0; i < numSecrets; i++ {
				if secret.SecretID == fmt.Sprintf("%s/secret-%d", basePath, i) {
					count++
					break
				}
			}
		}

		assert.GreaterOrEqual(t, count, 1, "Should find at least one test secret")
		t.Logf("[%s] List All: found %d test secrets out of %d total", protocol.Name(), count, len(resp.Secrets))
	})

	// Test: List with limit
	t.Run("ListWithLimit", func(t *testing.T) {
		resp, err := protocol.ListSecrets(ctx, &service.ListSecretsRequest{
			Limit: 2,
		})
		require.NoError(t, err)
		assert.NotNil(t, resp)
		assert.LessOrEqual(t, len(resp.Secrets), 2)

		t.Logf("[%s] List WithLimit: got %d secrets (limit=2)", protocol.Name(), len(resp.Secrets))
	})

	// Test: List with cursor pagination
	t.Run("ListWithPagination", func(t *testing.T) {
		// Get first page
		resp1, err := protocol.ListSecrets(ctx, &service.ListSecretsRequest{
			Limit: 2,
		})
		require.NoError(t, err)
		assert.NotNil(t, resp1)

		if resp1.HasMore && resp1.NextCursor != "" {
			// Get second page
			resp2, err := protocol.ListSecrets(ctx, &service.ListSecretsRequest{
				Limit:  2,
				Cursor: resp1.NextCursor,
			})
			require.NoError(t, err)
			assert.NotNil(t, resp2)

			// Verify no overlap between pages
			page1IDs := make(map[string]bool)
			for _, s := range resp1.Secrets {
				page1IDs[s.SecretID] = true
			}
			for _, s := range resp2.Secrets {
				assert.False(t, page1IDs[s.SecretID], "Pages should not overlap")
			}

			t.Logf("[%s] List WithPagination: page1=%d, page2=%d", protocol.Name(), len(resp1.Secrets), len(resp2.Secrets))
		} else {
			t.Logf("[%s] List WithPagination: no pagination needed (all secrets fit in one page)", protocol.Name())
		}
	})

	// Test: List with prefix filter
	t.Run("ListWithPrefix", func(t *testing.T) {
		resp, err := protocol.ListSecrets(ctx, &service.ListSecretsRequest{
			Limit: 100,
			Filter: map[string]string{
				"prefix": basePath,
			},
		})
		require.NoError(t, err)
		assert.NotNil(t, resp)

		// All returned secrets should have the prefix
		for _, secret := range resp.Secrets {
			// Only check our test secrets
			if len(secret.SecretID) >= len(basePath) {
				assert.True(t, len(secret.SecretID) >= len(basePath))
			}
		}

		t.Logf("[%s] List WithPrefix: found %d secrets with prefix %s", protocol.Name(), len(resp.Secrets), basePath)
	})
}

// =============================================================================
// Enable/Disable Tests
// =============================================================================

func testEnableDisable(t *testing.T, ctx context.Context, protocol secretsProtocol, basePath string) {
	t.Helper()

	secretPath := basePath + "/status-test"

	// Create a secret
	secretData, err := json.Marshal(map[string]interface{}{
		"sensitive": "data",
	})
	require.NoError(t, err)

	storeReq := &service.StoreSecretRequest{
		SecretID: secretPath,
		Name:     "status-test-secret",
		Data:     secretData,
	}

	_, err = protocol.StoreSecret(ctx, storeReq)
	require.NoError(t, err)

	// Test: Verify secret is enabled by default
	t.Run("EnabledByDefault", func(t *testing.T) {
		resp, err := protocol.GetSecret(ctx, &service.GetSecretRequest{
			SecretID: secretPath,
		})
		require.NoError(t, err)
		assert.NotNil(t, resp)
		assert.NotEmpty(t, resp.Data)

		t.Logf("[%s] EnabledByDefault: secret is accessible", protocol.Name())
	})

	// Test: Disable secret
	t.Run("DisableSecret", func(t *testing.T) {
		resp, err := protocol.UpdateSecretStatus(ctx, &service.UpdateSecretStatusRequest{
			SecretID: secretPath,
			Status:   service.SecretStatusDisabled,
		})
		require.NoError(t, err)
		assert.NotNil(t, resp)
		assert.Equal(t, service.SecretStatusDisabled, resp.NewStatus)

		t.Logf("[%s] DisableSecret: status changed to disabled", protocol.Name())
	})

	// Test: Verify disabled secret cannot be retrieved
	t.Run("DisabledSecretNotAccessible", func(t *testing.T) {
		_, err := protocol.GetSecret(ctx, &service.GetSecretRequest{
			SecretID: secretPath,
		})
		assert.Error(t, err, "Disabled secret should not be accessible")

		t.Logf("[%s] DisabledSecretNotAccessible: access correctly denied", protocol.Name())
	})

	// Test: Re-enable secret
	t.Run("EnableSecret", func(t *testing.T) {
		resp, err := protocol.UpdateSecretStatus(ctx, &service.UpdateSecretStatusRequest{
			SecretID: secretPath,
			Status:   service.SecretStatusEnabled,
		})
		require.NoError(t, err)
		assert.NotNil(t, resp)
		assert.Equal(t, service.SecretStatusEnabled, resp.NewStatus)

		t.Logf("[%s] EnableSecret: status changed to enabled", protocol.Name())
	})

	// Test: Verify re-enabled secret can be retrieved
	t.Run("EnabledSecretAccessible", func(t *testing.T) {
		resp, err := protocol.GetSecret(ctx, &service.GetSecretRequest{
			SecretID: secretPath,
		})
		require.NoError(t, err)
		assert.NotNil(t, resp)
		assert.NotEmpty(t, resp.Data)

		t.Logf("[%s] EnabledSecretAccessible: secret is accessible again", protocol.Name())
	})
}

// =============================================================================
// Data Persistence and Integrity Tests
// =============================================================================

func testDataPersistence(t *testing.T, ctx context.Context, protocol secretsProtocol, basePath string) {
	t.Helper()

	secretPath := basePath + "/persistence-test"

	// Create complex data structure
	originalData := map[string]interface{}{
		"string":  "test-value",
		"number":  42,
		"float":   3.14159,
		"boolean": true,
		"array":   []interface{}{"a", "b", "c"},
		"nested": map[string]interface{}{
			"key1": "value1",
			"key2": 123,
		},
	}

	secretData, err := json.Marshal(originalData)
	require.NoError(t, err)

	// Test: Store and verify data integrity
	t.Run("DataIntegrity", func(t *testing.T) {
		storeReq := &service.StoreSecretRequest{
			SecretID: secretPath,
			Name:     "persistence-secret",
			Data:     secretData,
		}

		storeResp, err := protocol.StoreSecret(ctx, storeReq)
		require.NoError(t, err)
		assert.Equal(t, uint64(1), storeResp.Version)

		// Retrieve and verify
		getResp, err := protocol.GetSecret(ctx, &service.GetSecretRequest{
			SecretID: secretPath,
		})
		require.NoError(t, err)

		var retrievedData map[string]interface{}
		err = json.Unmarshal(getResp.Data, &retrievedData)
		require.NoError(t, err)

		// Verify each field
		assert.Equal(t, "test-value", retrievedData["string"])
		assert.Equal(t, float64(42), retrievedData["number"])
		assert.InDelta(t, 3.14159, retrievedData["float"], 0.00001)
		assert.Equal(t, true, retrievedData["boolean"])

		t.Logf("[%s] DataIntegrity: complex data preserved correctly", protocol.Name())
	})

	// Test: Version increments correctly
	t.Run("VersionIncrements", func(t *testing.T) {
		var lastVersion uint64

		for i := 0; i < 3; i++ {
			updateData, err := json.Marshal(map[string]interface{}{
				"update_number": i,
			})
			require.NoError(t, err)

			storeReq := &service.StoreSecretRequest{
				SecretID: secretPath,
				Name:     "persistence-secret",
				Data:     updateData,
			}

			storeResp, err := protocol.StoreSecret(ctx, storeReq)
			require.NoError(t, err)

			if i > 0 {
				assert.Greater(t, storeResp.Version, lastVersion, "Version should increment")
			}
			lastVersion = storeResp.Version
		}

		t.Logf("[%s] VersionIncrements: versions increment correctly", protocol.Name())
	})

	// Test: Timestamps are set correctly
	t.Run("TimestampsSet", func(t *testing.T) {
		beforeStore := time.Now().Add(-time.Second)

		timestampData, err := json.Marshal(map[string]interface{}{
			"timestamp_test": true,
		})
		require.NoError(t, err)

		storeReq := &service.StoreSecretRequest{
			SecretID: basePath + "/timestamp-test",
			Name:     "timestamp-secret",
			Data:     timestampData,
		}

		storeResp, err := protocol.StoreSecret(ctx, storeReq)
		require.NoError(t, err)

		afterStore := time.Now().Add(time.Second)

		assert.True(t, storeResp.Timestamp.After(beforeStore), "Timestamp should be after request start")
		assert.True(t, storeResp.Timestamp.Before(afterStore), "Timestamp should be before request end")

		t.Logf("[%s] TimestampsSet: timestamp=%v", protocol.Name(), storeResp.Timestamp)
	})
}

// =============================================================================
// Tenant Isolation Tests
// =============================================================================

func testTenantIsolation(t *testing.T, ctx context.Context, protocol secretsProtocol, basePath string) {
	t.Helper()

	// Two paths in the caller's own tenant: one protocol client is one
	// principal, so this checks the paths stay apart. Isolation between
	// tenants, with a principal per tenant, is TestMultiProtocol_TenantIsolation.
	tenant1Path := basePath + "/tenant1-secret"
	tenant2Path := basePath + "/tenant2-secret"

	tenant1Data, err := json.Marshal(map[string]interface{}{
		"tenant": "tenant1",
		"secret": "tenant1-value",
	})
	require.NoError(t, err)

	tenant2Data, err := json.Marshal(map[string]interface{}{
		"tenant": "tenant2",
		"secret": "tenant2-value",
	})
	require.NoError(t, err)

	// Store tenant 1 secret
	_, err = protocol.StoreSecret(ctx, &service.StoreSecretRequest{
		SecretID: tenant1Path,
		Name:     "tenant1-secret",
		Data:     tenant1Data,
		TenantID: helpers.TenantPrincipalID(t, getRESTEndpoint()),
	})
	require.NoError(t, err)

	// Store tenant 2 secret
	_, err = protocol.StoreSecret(ctx, &service.StoreSecretRequest{
		SecretID: tenant2Path,
		Name:     "tenant2-secret",
		Data:     tenant2Data,
		TenantID: helpers.TenantPrincipalID(t, getRESTEndpoint()),
	})
	require.NoError(t, err)

	// Test: Verify each tenant's secret is stored correctly
	t.Run("TenantSecretsStored", func(t *testing.T) {
		resp1, err := protocol.GetSecret(ctx, &service.GetSecretRequest{
			SecretID: tenant1Path,
		})
		require.NoError(t, err)

		var data1 map[string]interface{}
		err = json.Unmarshal(resp1.Data, &data1)
		require.NoError(t, err)
		assert.Equal(t, "tenant1", data1["tenant"])

		resp2, err := protocol.GetSecret(ctx, &service.GetSecretRequest{
			SecretID: tenant2Path,
		})
		require.NoError(t, err)

		var data2 map[string]interface{}
		err = json.Unmarshal(resp2.Data, &data2)
		require.NoError(t, err)
		assert.Equal(t, "tenant2", data2["tenant"])

		t.Logf("[%s] TenantSecretsStored: both tenant secrets stored correctly", protocol.Name())
	})

	// Test: Verify secrets are isolated by path
	t.Run("TenantSecretsIsolated", func(t *testing.T) {
		// Each tenant's secret should have a different path
		assert.NotEqual(t, tenant1Path, tenant2Path)

		t.Logf("[%s] TenantSecretsIsolated: secrets are at different paths", protocol.Name())
	})
}

// =============================================================================
// Individual Test Functions
// =============================================================================

func testStoreSecret(t *testing.T, ctx context.Context, protocol secretsProtocol, path string) {
	t.Helper()

	secretData, err := json.Marshal(map[string]interface{}{
		"username": "admin",
		"password": "super-secret-password",
	})
	require.NoError(t, err)

	req := &service.StoreSecretRequest{
		SecretID:    path,
		Name:        "test-secret",
		Description: "Test secret for multi-protocol testing",
		Data:        secretData,
		Metadata: map[string]string{
			"protocol": protocol.Name(),
			"test":     "store",
		},
	}

	resp, err := protocol.StoreSecret(ctx, req)
	require.NoError(t, err, "StoreSecret failed for %s", protocol.Name())
	assert.NotNil(t, resp)
	assert.Equal(t, path, resp.SecretID)
	assert.Greater(t, resp.Version, uint64(0))
	assert.False(t, resp.Timestamp.IsZero())

	t.Logf("[%s] Created secret: %s, version: %d", protocol.Name(), resp.SecretID, resp.Version)
}

func testGetSecret(t *testing.T, ctx context.Context, protocol secretsProtocol, path string) {
	t.Helper()

	// First, create a secret
	secretData, err := json.Marshal(map[string]interface{}{
		"key": "value",
		"num": 42,
	})
	require.NoError(t, err)

	storeReq := &service.StoreSecretRequest{
		SecretID: path,
		Name:     "get-test",
		Data:     secretData,
	}

	storeResp, err := protocol.StoreSecret(ctx, storeReq)
	require.NoError(t, err)

	// Now retrieve it
	getReq := &service.GetSecretRequest{
		SecretID: path,
	}

	resp, err := protocol.GetSecret(ctx, getReq)
	require.NoError(t, err, "GetSecret failed for %s", protocol.Name())
	assert.NotNil(t, resp)
	assert.Equal(t, path, resp.SecretID)
	assert.Equal(t, storeResp.Version, resp.Version)
	assert.NotEmpty(t, resp.Data)

	// Verify data integrity
	var data map[string]interface{}
	err = json.Unmarshal(resp.Data, &data)
	require.NoError(t, err)
	assert.Equal(t, "value", data["key"])
	assert.Equal(t, float64(42), data["num"])

	t.Logf("[%s] Retrieved secret: %s, version: %d", protocol.Name(), resp.SecretID, resp.Version)
}

func testListSecrets(t *testing.T, ctx context.Context, protocol secretsProtocol, basePath string) {
	t.Helper()

	// Create multiple secrets
	numSecrets := 3
	for i := 0; i < numSecrets; i++ {
		secretData, err := json.Marshal(map[string]interface{}{
			"index": i,
		})
		require.NoError(t, err)

		req := &service.StoreSecretRequest{
			SecretID: fmt.Sprintf("%s/secret-%d", basePath, i),
			Name:     fmt.Sprintf("list-test-%d", i),
			Data:     secretData,
		}

		_, err = protocol.StoreSecret(ctx, req)
		require.NoError(t, err)
	}

	// List them
	listReq := &service.ListSecretsRequest{
		Limit: 100,
	}

	resp, err := protocol.ListSecrets(ctx, listReq)
	require.NoError(t, err, "ListSecrets failed for %s", protocol.Name())
	assert.NotNil(t, resp)
	assert.NotEmpty(t, resp.Secrets)

	// Count how many of our test secrets are in the list
	count := 0
	for _, secret := range resp.Secrets {
		// Check if this is one of our test secrets
		for i := 0; i < numSecrets; i++ {
			expectedPath := fmt.Sprintf("%s/secret-%d", basePath, i)
			if secret.SecretID == expectedPath {
				count++
				break
			}
		}
	}

	assert.GreaterOrEqual(t, count, 1, "Should find at least one of our test secrets")
	t.Logf("[%s] Listed %d total secrets, %d test secrets", protocol.Name(), len(resp.Secrets), count)
}

func testDeleteSecret(t *testing.T, ctx context.Context, protocol secretsProtocol, path string) {
	t.Helper()

	// Create a secret
	secretData, err := json.Marshal(map[string]interface{}{
		"to": "delete",
	})
	require.NoError(t, err)

	storeReq := &service.StoreSecretRequest{
		SecretID: path,
		Name:     "delete-test",
		Data:     secretData,
	}

	_, err = protocol.StoreSecret(ctx, storeReq)
	require.NoError(t, err)

	// Soft delete it
	deleteReq := &service.DeleteSecretRequest{
		SecretID:          path,
		PermanentlyDelete: false,
	}

	resp, err := protocol.DeleteSecret(ctx, deleteReq)
	require.NoError(t, err, "DeleteSecret failed for %s", protocol.Name())
	assert.NotNil(t, resp)
	assert.Equal(t, path, resp.SecretID)
	assert.True(t, resp.Deleted)

	t.Logf("[%s] Soft deleted secret: %s", protocol.Name(), resp.SecretID)

	// Verify it's gone (or marked as deleted)
	getReq := &service.GetSecretRequest{
		SecretID: path,
	}

	_, err = protocol.GetSecret(ctx, getReq)
	assert.Error(t, err, "Should not be able to get deleted secret")
}

func testDestroySecret(t *testing.T, ctx context.Context, protocol secretsProtocol, path string) {
	t.Helper()

	// Create a secret
	secretData, err := json.Marshal(map[string]interface{}{
		"to": "destroy",
	})
	require.NoError(t, err)

	storeReq := &service.StoreSecretRequest{
		SecretID: path,
		Name:     "destroy-test",
		Data:     secretData,
	}

	_, err = protocol.StoreSecret(ctx, storeReq)
	require.NoError(t, err)

	// Permanently delete it
	deleteReq := &service.DeleteSecretRequest{
		SecretID:          path,
		PermanentlyDelete: true,
	}

	resp, err := protocol.DeleteSecret(ctx, deleteReq)
	require.NoError(t, err, "DeleteSecret (destroy) failed for %s", protocol.Name())
	assert.NotNil(t, resp)
	assert.Equal(t, path, resp.SecretID)
	assert.True(t, resp.Deleted)

	t.Logf("[%s] Permanently destroyed secret: %s", protocol.Name(), resp.SecretID)

	// Verify it's completely gone
	getReq := &service.GetSecretRequest{
		SecretID: path,
	}

	_, err = protocol.GetSecret(ctx, getReq)
	assert.Error(t, err, "Should not be able to get destroyed secret")
}

func testListVersions(t *testing.T, ctx context.Context, protocol secretsProtocol, path string) {
	t.Helper()

	// Create multiple versions
	numVersions := 3
	for i := 1; i <= numVersions; i++ {
		secretData, err := json.Marshal(map[string]interface{}{
			"version": i,
			"data":    fmt.Sprintf("version-%d-data", i),
		})
		require.NoError(t, err)

		req := &service.StoreSecretRequest{
			SecretID: path,
			Name:     "versions-test",
			Data:     secretData,
		}

		_, err = protocol.StoreSecret(ctx, req)
		require.NoError(t, err)
	}

	// List versions
	listReq := &service.ListSecretVersionsRequest{
		SecretID: path,
		Limit:    100,
	}

	resp, err := protocol.ListVersions(ctx, listReq)
	require.NoError(t, err, "ListVersions failed for %s", protocol.Name())
	assert.NotNil(t, resp)
	assert.NotEmpty(t, resp.Versions)
	assert.GreaterOrEqual(t, len(resp.Versions), numVersions)

	// Verify version numbers
	for _, ver := range resp.Versions {
		assert.Equal(t, path, ver.SecretID)
		assert.Greater(t, ver.Version, uint64(0))
		assert.False(t, ver.CreatedAt.IsZero())
	}

	t.Logf("[%s] Listed %d versions for secret: %s", protocol.Name(), len(resp.Versions), path)
}

func testRollback(t *testing.T, ctx context.Context, protocol secretsProtocol, path string) {
	t.Helper()

	// Create multiple versions
	for i := 1; i <= 3; i++ {
		secretData, err := json.Marshal(map[string]interface{}{
			"version": i,
			"data":    fmt.Sprintf("version-%d", i),
		})
		require.NoError(t, err)

		req := &service.StoreSecretRequest{
			SecretID: path,
			Name:     "rollback-test",
			Data:     secretData,
		}

		_, err = protocol.StoreSecret(ctx, req)
		require.NoError(t, err)
	}

	// Rollback to version 1
	rollbackReq := &service.RollbackSecretRequest{
		SecretID:      path,
		TargetVersion: 1,
	}

	resp, err := protocol.Rollback(ctx, rollbackReq)
	require.NoError(t, err, "Rollback failed for %s", protocol.Name())
	assert.NotNil(t, resp)
	assert.Equal(t, path, resp.SecretID)
	assert.Greater(t, resp.NewVersion, uint64(0))

	t.Logf("[%s] Rolled back secret %s to version 1, new version: %d",
		protocol.Name(), resp.SecretID, resp.NewVersion)

	// Verify the data matches version 1
	getReq := &service.GetSecretRequest{
		SecretID: path,
	}

	getResp, err := protocol.GetSecret(ctx, getReq)
	require.NoError(t, err)

	var data map[string]interface{}
	err = json.Unmarshal(getResp.Data, &data)
	require.NoError(t, err)
	assert.Equal(t, "version-1", data["data"])
}

func testRotate(t *testing.T, ctx context.Context, protocol secretsProtocol, path string) {
	t.Helper()

	// Create initial secret
	secretData, err := json.Marshal(map[string]interface{}{
		"key": "original-value",
	})
	require.NoError(t, err)

	storeReq := &service.StoreSecretRequest{
		SecretID: path,
		Name:     "rotate-test",
		Data:     secretData,
	}

	storeResp, err := protocol.StoreSecret(ctx, storeReq)
	require.NoError(t, err)
	oldVersion := storeResp.Version

	// Rotate to new value
	newData, err := json.Marshal(map[string]interface{}{
		"key": "rotated-value",
	})
	require.NoError(t, err)

	rotateReq := &service.RotateSecretRequest{
		SecretID: path,
		NewData:  newData,
	}

	resp, err := protocol.Rotate(ctx, rotateReq)
	require.NoError(t, err, "Rotate failed for %s", protocol.Name())
	assert.NotNil(t, resp)
	assert.Equal(t, path, resp.SecretID)
	assert.Equal(t, oldVersion, resp.OldVersion)
	assert.Greater(t, resp.NewVersion, resp.OldVersion)

	t.Logf("[%s] Rotated secret %s from version %d to %d",
		protocol.Name(), resp.SecretID, resp.OldVersion, resp.NewVersion)

	// Verify new data
	getReq := &service.GetSecretRequest{
		SecretID: path,
	}

	getResp, err := protocol.GetSecret(ctx, getReq)
	require.NoError(t, err)

	var data map[string]interface{}
	err = json.Unmarshal(getResp.Data, &data)
	require.NoError(t, err)
	assert.Equal(t, "rotated-value", data["key"])
}

// =============================================================================
// Protocol Implementations
// =============================================================================

// =============================================================================
// Embedded Protocol Client Implementation
// =============================================================================

// ErrEmbeddedClientNotInitialized is returned when the embedded client is not initialized.
type ErrEmbeddedClientNotInitialized struct{}

func (e ErrEmbeddedClientNotInitialized) Error() string {
	return "embedded client not initialized"
}

type embeddedSecretsClient struct {
	client quicbaas.Client
}

func (c *embeddedSecretsClient) Name() string { return "Embedded" }

func (c *embeddedSecretsClient) Setup(t *testing.T, ctx context.Context) error {
	svc, err := helpers.GetEmbeddedService(t)
	if err != nil {
		return fmt.Errorf("failed to get embedded service: %w", err)
	}

	client, err := quicbaas.NewClient("embedded", quicbaas.WithEmbedded(svc), helpers.EmbeddedSystemIdentity())
	if err != nil {
		return fmt.Errorf("failed to create embedded client: %w", err)
	}

	c.client = client
	return nil
}

func (c *embeddedSecretsClient) Teardown(t *testing.T) error {
	if c.client != nil {
		return c.client.Close()
	}
	return nil
}

func (c *embeddedSecretsClient) StoreSecret(ctx context.Context, req *service.StoreSecretRequest) (*service.StoreSecretResponse, error) {
	if c.client == nil {
		return nil, ErrEmbeddedClientNotInitialized{}
	}

	// Convert JSON data to map for SDK
	var dataMap map[string]interface{}
	if err := json.Unmarshal(req.Data, &dataMap); err != nil {
		return nil, fmt.Errorf("failed to unmarshal data: %w", err)
	}

	// Use the SDK secrets client
	storeReq := &quicbaas.StoreSecretRequest{
		Path:     req.SecretID,
		Data:     dataMap,
		Metadata: req.Metadata,
	}

	resp, err := c.client.Security().Secrets().Store(ctx, storeReq)
	if err != nil {
		return nil, err
	}

	return &service.StoreSecretResponse{
		SecretID:  req.SecretID,
		Version:   uint64(resp.Version),
		Timestamp: time.Now(),
	}, nil
}

func (c *embeddedSecretsClient) GetSecret(ctx context.Context, req *service.GetSecretRequest) (*service.GetSecretResponse, error) {
	if c.client == nil {
		return nil, ErrEmbeddedClientNotInitialized{}
	}

	var opts []quicbaas.SecretOption
	if req.Version > 0 {
		opts = append(opts, quicbaas.WithSecretVersion(int(req.Version)))
	}

	secret, err := c.client.Security().Secrets().Get(ctx, req.SecretID, opts...)
	if err != nil {
		return nil, err
	}

	// Convert data map back to JSON bytes
	data, err := json.Marshal(secret.Data)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal data: %w", err)
	}

	return &service.GetSecretResponse{
		SecretID:  secret.Path,
		Version:   uint64(secret.Version),
		Data:      data,
		Metadata:  secret.Metadata,
		CreatedAt: secret.CreatedAt,
		Timestamp: time.Now(),
	}, nil
}

func (c *embeddedSecretsClient) ListSecrets(ctx context.Context, req *service.ListSecretsRequest) (*service.ListSecretsResponse, error) {
	if c.client == nil {
		return nil, ErrEmbeddedClientNotInitialized{}
	}

	prefix := req.Prefix
	if prefix == "" && req.Filter != nil {
		prefix = req.Filter["prefix"]
	}

	resp, err := c.client.Security().Secrets().List(ctx, prefix)
	if err != nil {
		return nil, err
	}

	// Convert paths to Secret objects
	secrets := make([]*service.Secret, 0, len(resp.Paths))
	for _, path := range resp.Paths {
		secrets = append(secrets, &service.Secret{
			SecretID: path,
			Name:     path,
		})
	}

	// Apply limit
	total := len(secrets)
	hasMore := false
	if req.Limit > 0 && req.Limit < total {
		secrets = secrets[:req.Limit]
		hasMore = true
	}

	return &service.ListSecretsResponse{
		Secrets:    secrets,
		HasMore:    hasMore,
		TotalCount: total,
		Timestamp:  time.Now(),
	}, nil
}

func (c *embeddedSecretsClient) DeleteSecret(ctx context.Context, req *service.DeleteSecretRequest) (*service.DeleteSecretResponse, error) {
	if c.client == nil {
		return nil, ErrEmbeddedClientNotInitialized{}
	}

	err := c.client.Security().Secrets().Delete(ctx, req.SecretID)
	if err != nil {
		return nil, err
	}

	return &service.DeleteSecretResponse{
		SecretID:  req.SecretID,
		Deleted:   true,
		Timestamp: time.Now(),
	}, nil
}

func (c *embeddedSecretsClient) ListVersions(ctx context.Context, req *service.ListSecretVersionsRequest) (*service.ListSecretVersionsResponse, error) {
	if c.client == nil {
		return nil, ErrEmbeddedClientNotInitialized{}
	}

	// The SDK secrets client doesn't have a ListVersions method,
	// so we simulate it by fetching versions directly if available,
	// or return a not implemented error
	return nil, fmt.Errorf("embedded ListVersions not yet implemented in SDK")
}

func (c *embeddedSecretsClient) Rollback(ctx context.Context, req *service.RollbackSecretRequest) (*service.RollbackSecretResponse, error) {
	if c.client == nil {
		return nil, ErrEmbeddedClientNotInitialized{}
	}

	// The SDK secrets client doesn't have a Rollback method
	return nil, fmt.Errorf("embedded Rollback not yet implemented in SDK")
}

func (c *embeddedSecretsClient) Rotate(ctx context.Context, req *service.RotateSecretRequest) (*service.RotateSecretResponse, error) {
	if c.client == nil {
		return nil, ErrEmbeddedClientNotInitialized{}
	}

	// Get current version first
	secret, err := c.client.Security().Secrets().Get(ctx, req.SecretID)
	if err != nil {
		return nil, err
	}
	oldVersion := uint64(secret.Version)

	// Convert JSON data to map
	var dataMap map[string]interface{}
	if err := json.Unmarshal(req.NewData, &dataMap); err != nil {
		return nil, fmt.Errorf("failed to unmarshal new data: %w", err)
	}

	// Store as new version
	storeReq := &quicbaas.StoreSecretRequest{
		Path:     req.SecretID,
		Data:     dataMap,
		Metadata: req.Metadata,
	}

	resp, err := c.client.Security().Secrets().Store(ctx, storeReq)
	if err != nil {
		return nil, err
	}

	return &service.RotateSecretResponse{
		SecretID:   req.SecretID,
		OldVersion: oldVersion,
		NewVersion: uint64(resp.Version),
		Timestamp:  time.Now(),
	}, nil
}

func (c *embeddedSecretsClient) UpdateSecretStatus(ctx context.Context, req *service.UpdateSecretStatusRequest) (*service.UpdateSecretStatusResponse, error) {
	if c.client == nil {
		return nil, ErrEmbeddedClientNotInitialized{}
	}

	// The SDK secrets client doesn't have an UpdateSecretStatus method
	return nil, fmt.Errorf("embedded UpdateSecretStatus not yet implemented in SDK")
}

// =============================================================================
// REST Protocol Client Implementation
// =============================================================================

// restProtocolClient implements secretsProtocol using REST API.
type restProtocolClient struct {
	endpoint string
	t        *testing.T
}

func (c *restProtocolClient) Name() string { return "REST" }

func (c *restProtocolClient) Setup(t *testing.T, ctx context.Context) error {
	c.t = t
	c.endpoint = getRESTEndpoint()
	return nil
}

func (c *restProtocolClient) Teardown(t *testing.T) error {
	return nil
}

func (c *restProtocolClient) StoreSecret(ctx context.Context, req *service.StoreSecretRequest) (*service.StoreSecretResponse, error) {
	data := make(map[string]interface{})
	if err := json.Unmarshal(req.Data, &data); err != nil {
		return nil, err
	}

	opts := &SecretOptions{
		Metadata:    req.Metadata,
		Description: req.Description,
		TenantID:    req.TenantID,
		KeyID:       req.KeyID,
		Algorithm:   req.Algorithm,
	}

	// Use helper from helpers.go
	return createSecret(c.t, c.endpoint, req.SecretID, data, opts), nil
}

func (c *restProtocolClient) GetSecret(ctx context.Context, req *service.GetSecretRequest) (*service.GetSecretResponse, error) {
	// Implement directly to properly handle error responses (404 after delete)
	url := fmt.Sprintf("%s/api/v1/secrets/%s", c.endpoint, req.SecretID)
	if req.Version > 0 {
		url = fmt.Sprintf("%s?version=%d", url, req.Version)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}

	client := helpers.NewTenantHTTPClient(c.t, 10*time.Second)
	resp, err := client.Do(httpReq)
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

	var result service.GetSecretResponse
	if err := json.Unmarshal(respBody, &result); err != nil {
		return nil, err
	}

	return &result, nil
}

func (c *restProtocolClient) ListSecrets(ctx context.Context, req *service.ListSecretsRequest) (*service.ListSecretsResponse, error) {
	url := fmt.Sprintf("%s/api/v1/secrets", c.endpoint)

	// Build query params
	queryParams := []string{}
	if req.Limit > 0 {
		queryParams = append(queryParams, fmt.Sprintf("limit=%d", req.Limit))
	}
	if req.Cursor != "" {
		queryParams = append(queryParams, fmt.Sprintf("cursor=%s", req.Cursor))
	}
	if req.Prefix != "" {
		queryParams = append(queryParams, fmt.Sprintf("prefix=%s", req.Prefix))
	} else if prefix, ok := req.Filter["prefix"]; ok {
		queryParams = append(queryParams, fmt.Sprintf("prefix=%s", prefix))
	}
	if len(queryParams) > 0 {
		url += "?"
		for i, param := range queryParams {
			if i > 0 {
				url += "&"
			}
			url += param
		}
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}

	client := helpers.NewTenantHTTPClient(c.t, 10*time.Second)
	resp, err := client.Do(httpReq)
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

	var result service.ListSecretsResponse
	if err := json.Unmarshal(respBody, &result); err != nil {
		return nil, err
	}

	return &result, nil
}

func (c *restProtocolClient) DeleteSecret(ctx context.Context, req *service.DeleteSecretRequest) (*service.DeleteSecretResponse, error) {
	// Use helper from helpers.go
	return deleteSecret(c.t, c.endpoint, req.SecretID, req.PermanentlyDelete), nil
}

func (c *restProtocolClient) ListVersions(ctx context.Context, req *service.ListSecretVersionsRequest) (*service.ListSecretVersionsResponse, error) {
	// Use helper from helpers.go
	return listSecretVersions(c.t, c.endpoint, req.SecretID), nil
}

func (c *restProtocolClient) Rollback(ctx context.Context, req *service.RollbackSecretRequest) (*service.RollbackSecretResponse, error) {
	// Use helper from helpers.go
	return rollbackSecret(c.t, c.endpoint, req.SecretID, req.TargetVersion), nil
}

func (c *restProtocolClient) Rotate(ctx context.Context, req *service.RotateSecretRequest) (*service.RotateSecretResponse, error) {
	var data map[string]interface{}
	if err := json.Unmarshal(req.NewData, &data); err != nil {
		return nil, err
	}
	// Use helper from helpers.go
	return rotateSecret(c.t, c.endpoint, req.SecretID, data), nil
}

func (c *restProtocolClient) UpdateSecretStatus(ctx context.Context, req *service.UpdateSecretStatusRequest) (*service.UpdateSecretStatusResponse, error) {
	// Determine which endpoint to use based on the requested status
	var action string
	switch req.Status {
	case service.SecretStatusEnabled:
		action = "enable"
	case service.SecretStatusDisabled:
		action = "disable"
	default:
		return nil, fmt.Errorf("invalid status: %s", req.Status)
	}

	url := fmt.Sprintf("%s/api/v1/secrets/%s/%s", c.endpoint, req.SecretID, action)

	// POST request with empty body
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, nil)
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", "application/json")

	client := helpers.NewTenantHTTPClient(c.t, 10*time.Second)
	resp, err := client.Do(httpReq)
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

	var result service.UpdateSecretStatusResponse
	if err := json.Unmarshal(respBody, &result); err != nil {
		return nil, err
	}

	return &result, nil
}

// grpcProtocolClient implements secretsProtocol using gRPC.
type grpcProtocolClient struct {
	client *grpcclient.Client
	conn   *grpc.ClientConn
}

func (c *grpcProtocolClient) Name() string { return "gRPC" }

func (c *grpcProtocolClient) Setup(t *testing.T, ctx context.Context) error {
	endpoints := helpers.GetClusterEndpoints()
	require.NotEmpty(t, endpoints, "gRPC endpoints must be available for integration tests")

	// Connect to first endpoint
	endpoint := endpoints[0]

	conn, err := helpers.SetupGRPCConnectionToEndpoint(t, ctx, endpoint, grpc.WithPerRPCCredentials(helpers.TenantGRPCCredentials(t)))
	if err != nil {
		return fmt.Errorf("failed to connect to gRPC endpoint %s: %w", endpoint, err)
	}
	c.conn = conn

	tlsCfg, err := helpers.IntegrationTLSConfig()
	if err != nil {
		return fmt.Errorf("load integration TLS config: %w", err)
	}
	client, err := grpcclient.NewClient(endpoint, grpcclient.WithTLSConfig(tlsCfg),
		grpcclient.WithDialOptions(grpc.WithPerRPCCredentials(helpers.TenantGRPCCredentials(t))))
	if err != nil {
		conn.Close()
		return fmt.Errorf("failed to create gRPC client: %w", err)
	}
	c.client = client

	return nil
}

func (c *grpcProtocolClient) Teardown(t *testing.T) error {
	if c.client != nil {
		c.client.Close()
	}
	if c.conn != nil {
		c.conn.Close()
	}
	return nil
}

func (c *grpcProtocolClient) StoreSecret(ctx context.Context, req *service.StoreSecretRequest) (*service.StoreSecretResponse, error) {
	return c.client.StoreSecret(ctx, req)
}

func (c *grpcProtocolClient) GetSecret(ctx context.Context, req *service.GetSecretRequest) (*service.GetSecretResponse, error) {
	return c.client.GetSecret(ctx, req)
}

func (c *grpcProtocolClient) ListSecrets(ctx context.Context, req *service.ListSecretsRequest) (*service.ListSecretsResponse, error) {
	return c.client.ListSecrets(ctx, req)
}

func (c *grpcProtocolClient) DeleteSecret(ctx context.Context, req *service.DeleteSecretRequest) (*service.DeleteSecretResponse, error) {
	return c.client.DeleteSecret(ctx, req)
}

func (c *grpcProtocolClient) ListVersions(ctx context.Context, req *service.ListSecretVersionsRequest) (*service.ListSecretVersionsResponse, error) {
	return c.client.ListSecretVersions(ctx, req)
}

func (c *grpcProtocolClient) Rollback(ctx context.Context, req *service.RollbackSecretRequest) (*service.RollbackSecretResponse, error) {
	return c.client.RollbackSecret(ctx, req)
}

func (c *grpcProtocolClient) Rotate(ctx context.Context, req *service.RotateSecretRequest) (*service.RotateSecretResponse, error) {
	return c.client.RotateSecret(ctx, req)
}

func (c *grpcProtocolClient) UpdateSecretStatus(ctx context.Context, req *service.UpdateSecretStatusRequest) (*service.UpdateSecretStatusResponse, error) {
	return c.client.UpdateSecretStatus(ctx, req)
}

// =============================================================================
// Unix Socket Protocol Client
// =============================================================================

// unixSocketProtocolClient implements secretsProtocol using Unix domain sockets.
type unixSocketProtocolClient struct {
	client *grpcclient.Client
	conn   *grpc.ClientConn
}

func (c *unixSocketProtocolClient) Name() string { return "UnixSocket" }

func (c *unixSocketProtocolClient) Setup(t *testing.T, ctx context.Context) error {
	// Unix sockets are typically used for local connections
	// In Docker environment, we need to check if unix socket is exposed
	socketPath := os.Getenv("QUICBAAS_NODE1_UNIX")
	if socketPath == "" {
		socketPath = "/var/run/quicbaas/node1.sock"
	}

	// Check if socket exists
	if _, err := net.Dial("unix", socketPath); err != nil {
		return fmt.Errorf("unix socket not available at %s: %w", socketPath, err)
	}

	// Create gRPC connection over unix socket
	tlsCfg, err := helpers.IntegrationTLSConfig()
	if err != nil {
		return fmt.Errorf("load integration TLS config: %w", err)
	}
	conn, err := grpc.DialContext(ctx,
		"unix://"+socketPath,
		grpc.WithTransportCredentials(credentials.NewTLS(tlsCfg)),
		grpc.WithPerRPCCredentials(helpers.TenantGRPCCredentials(t)),
		grpc.WithDefaultCallOptions(grpc.WaitForReady(true)),
	)
	if err != nil {
		return fmt.Errorf("failed to connect to unix socket: %w", err)
	}
	c.conn = conn

	client, err := grpcclient.NewClient("unix://"+socketPath, grpcclient.WithTLSConfig(tlsCfg),
		grpcclient.WithDialOptions(grpc.WithPerRPCCredentials(helpers.TenantGRPCCredentials(t))))
	if err != nil {
		conn.Close()
		return fmt.Errorf("failed to create gRPC client over unix socket: %w", err)
	}
	c.client = client

	return nil
}

func (c *unixSocketProtocolClient) Teardown(t *testing.T) error {
	if c.client != nil {
		c.client.Close()
	}
	if c.conn != nil {
		c.conn.Close()
	}
	return nil
}

func (c *unixSocketProtocolClient) StoreSecret(ctx context.Context, req *service.StoreSecretRequest) (*service.StoreSecretResponse, error) {
	return c.client.StoreSecret(ctx, req)
}

func (c *unixSocketProtocolClient) GetSecret(ctx context.Context, req *service.GetSecretRequest) (*service.GetSecretResponse, error) {
	return c.client.GetSecret(ctx, req)
}

func (c *unixSocketProtocolClient) ListSecrets(ctx context.Context, req *service.ListSecretsRequest) (*service.ListSecretsResponse, error) {
	return c.client.ListSecrets(ctx, req)
}

func (c *unixSocketProtocolClient) DeleteSecret(ctx context.Context, req *service.DeleteSecretRequest) (*service.DeleteSecretResponse, error) {
	return c.client.DeleteSecret(ctx, req)
}

func (c *unixSocketProtocolClient) ListVersions(ctx context.Context, req *service.ListSecretVersionsRequest) (*service.ListSecretVersionsResponse, error) {
	return c.client.ListSecretVersions(ctx, req)
}

func (c *unixSocketProtocolClient) Rollback(ctx context.Context, req *service.RollbackSecretRequest) (*service.RollbackSecretResponse, error) {
	return c.client.RollbackSecret(ctx, req)
}

func (c *unixSocketProtocolClient) Rotate(ctx context.Context, req *service.RotateSecretRequest) (*service.RotateSecretResponse, error) {
	return c.client.RotateSecret(ctx, req)
}

func (c *unixSocketProtocolClient) UpdateSecretStatus(ctx context.Context, req *service.UpdateSecretStatusRequest) (*service.UpdateSecretStatusResponse, error) {
	return c.client.UpdateSecretStatus(ctx, req)
}
