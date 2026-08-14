//go:build !integration && hashicorpmetrics && local

// Copyright (c) 2025 Jeremy Hahn
// Copyright (c) 2025 Automate The Things, LLC
//
// This file is part of go-quicbaas.

package main

import (
	"context"
	"crypto/x509"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	kctypes "github.com/jeremyhahn/go-xkms/pkg/types"

	quicbaas "github.com/jeremyhahn/go-quicbaas/sdk/go"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/service"
)

// =============================================================================
// Factory Dispatch Function Tests
// Tests the transport switch branches in createXxxClient functions.
// Note: The MCP transport tests for KMS, DEK, Backend, and Safety are in
// coverage_gap_test.go. These tests cover Crypto, Seal, Backup MCP branches
// and the default/REST branches for all factory functions.
// =============================================================================

// TestCreateCryptoClient_MCPTransport tests MCP branch of createCryptoClient.
func TestCreateCryptoClient_MCPTransport(t *testing.T) {
	origTransport := transport
	origFactory := sdkClientFactory
	defer func() {
		transport = origTransport
		sdkClientFactory = origFactory
	}()

	transport = "mcp"
	sdkClientFactory = func() (quicbaas.Client, error) {
		return newTestSDKClient(nil, nil), nil
	}

	client, err := createCryptoClient()
	require.NoError(t, err)
	assert.NotNil(t, client)
}

// TestCreateCryptoClient_DefaultTransport tests default (SDK) branch.
func TestCreateCryptoClient_DefaultTransport(t *testing.T) {
	origTransport := transport
	origFactory := sdkClientFactory
	defer func() {
		transport = origTransport
		sdkClientFactory = origFactory
	}()

	transport = "rest"
	sdkClientFactory = func() (quicbaas.Client, error) {
		return newTestSDKClient(nil, nil), nil
	}

	client, err := createCryptoClient()
	require.NoError(t, err)
	assert.NotNil(t, client)
}

// TestCreateSealClient_MCPTransport tests MCP branch of createSealClient.
func TestCreateSealClient_MCPTransport(t *testing.T) {
	origTransport := transport
	origFactory := sdkClientFactory
	defer func() {
		transport = origTransport
		sdkClientFactory = origFactory
	}()

	transport = "mcp"
	sdkClientFactory = func() (quicbaas.Client, error) {
		return newTestSDKClient(nil, nil), nil
	}

	client, err := createSealClient()
	require.NoError(t, err)
	assert.NotNil(t, client)
}

// TestCreateSealClient_DefaultTransport tests default (SDK) branch.
func TestCreateSealClient_DefaultTransport(t *testing.T) {
	origTransport := transport
	origFactory := sdkClientFactory
	defer func() {
		transport = origTransport
		sdkClientFactory = origFactory
	}()

	transport = "rest"
	sdkClientFactory = func() (quicbaas.Client, error) {
		return newTestSDKClient(nil, nil), nil
	}

	client, err := createSealClient()
	require.NoError(t, err)
	assert.NotNil(t, client)
}

// TestCreateBackupClient_MCPTransport tests MCP branch of createBackupClient.
func TestCreateBackupClient_MCPTransport(t *testing.T) {
	origTransport := transport
	origFactory := sdkClientFactory
	defer func() {
		transport = origTransport
		sdkClientFactory = origFactory
	}()

	transport = "mcp"
	sdkClientFactory = func() (quicbaas.Client, error) {
		return newTestSDKClient(nil, nil), nil
	}

	client, err := createBackupClient()
	require.NoError(t, err)
	assert.NotNil(t, client)
}

// TestCreateBackupClient_DefaultTransport tests default (SDK) branch.
func TestCreateBackupClient_DefaultTransport(t *testing.T) {
	origTransport := transport
	origFactory := sdkClientFactory
	defer func() {
		transport = origTransport
		sdkClientFactory = origFactory
	}()

	transport = "rest"
	sdkClientFactory = func() (quicbaas.Client, error) {
		return newTestSDKClient(nil, nil), nil
	}

	client, err := createBackupClient()
	require.NoError(t, err)
	assert.NotNil(t, client)
}

// TestCreateKMSClient_DefaultRESTTransport tests default (SDK) branch for KMS.
func TestCreateKMSClient_DefaultRESTTransport(t *testing.T) {
	origTransport := transport
	origFactory := sdkClientFactory
	defer func() {
		transport = origTransport
		sdkClientFactory = origFactory
	}()

	transport = "rest"
	sdkClientFactory = func() (quicbaas.Client, error) {
		return newTestSDKClient(nil, nil), nil
	}

	client, err := createKMSClient()
	require.NoError(t, err)
	assert.NotNil(t, client)
}

// TestCreateBackendClient_DefaultRESTTransport tests default (SDK) branch for Backend.
func TestCreateBackendClient_DefaultRESTTransport(t *testing.T) {
	origTransport := transport
	origFactory := sdkClientFactory
	defer func() {
		transport = origTransport
		sdkClientFactory = origFactory
	}()

	transport = "rest"
	sdkClientFactory = func() (quicbaas.Client, error) {
		return newTestSDKClient(nil, nil), nil
	}

	client, err := createBackendClient()
	require.NoError(t, err)
	assert.NotNil(t, client)
}

// TestCreateSafetyClient_DefaultRESTTransport tests default (SDK) branch for Safety.
func TestCreateSafetyClient_DefaultRESTTransport(t *testing.T) {
	origTransport := transport
	origFactory := sdkClientFactory
	defer func() {
		transport = origTransport
		sdkClientFactory = origFactory
	}()

	transport = "rest"
	sdkClientFactory = func() (quicbaas.Client, error) {
		return newTestSDKClient(nil, nil), nil
	}

	client, err := createSafetyClient()
	require.NoError(t, err)
	assert.NotNil(t, client)
}

// TestCreateSDKAuthClient tests the SDK auth client factory.
func TestCreateSDKAuthClient(t *testing.T) {
	origFactory := sdkClientFactory
	defer func() { sdkClientFactory = origFactory }()

	t.Run("Success", func(t *testing.T) {
		sdkClientFactory = func() (quicbaas.Client, error) {
			// createSDKAuthClient resolves the auth client via the canonical
			// Security().Auth() accessor, so the fake must expose a non-nil
			// SecurityService -> AuthService chain.
			return newFakeFactorsClient(&fakeAuthFactorsClient{}), nil
		}
		got, err := createSDKAuthClient()
		require.NoError(t, err)
		require.NotNil(t, got)
	})

	t.Run("FactoryError", func(t *testing.T) {
		sdkClientFactory = func() (quicbaas.Client, error) {
			return nil, fmt.Errorf("factory error")
		}
		client, err := createSDKAuthClient()
		assert.Error(t, err)
		assert.Nil(t, client)
	})
}

// TestCreateCryptoClient_MCPFactoryError tests MCP factory error for crypto.
func TestCreateCryptoClient_MCPFactoryError(t *testing.T) {
	origTransport := transport
	origFactory := sdkClientFactory
	defer func() {
		transport = origTransport
		sdkClientFactory = origFactory
	}()

	transport = "mcp"
	sdkClientFactory = func() (quicbaas.Client, error) {
		return nil, fmt.Errorf("factory error")
	}

	client, err := createCryptoClient()
	assert.Error(t, err)
	assert.Nil(t, client)
}

// TestCreateSealClient_MCPFactoryError tests MCP factory error for seal.
func TestCreateSealClient_MCPFactoryError(t *testing.T) {
	origTransport := transport
	origFactory := sdkClientFactory
	defer func() {
		transport = origTransport
		sdkClientFactory = origFactory
	}()

	transport = "mcp"
	sdkClientFactory = func() (quicbaas.Client, error) {
		return nil, fmt.Errorf("factory error")
	}

	client, err := createSealClient()
	assert.Error(t, err)
	assert.Nil(t, client)
}

// TestCreateBackupClient_MCPFactoryError tests MCP factory error for backup.
func TestCreateBackupClient_MCPFactoryError(t *testing.T) {
	origTransport := transport
	origFactory := sdkClientFactory
	defer func() {
		transport = origTransport
		sdkClientFactory = origFactory
	}()

	transport = "mcp"
	sdkClientFactory = func() (quicbaas.Client, error) {
		return nil, fmt.Errorf("factory error")
	}

	client, err := createBackupClient()
	assert.Error(t, err)
	assert.Nil(t, client)
}

// =============================================================================
// MCP KMS Client Method Tests (Optional Branch Coverage)
// =============================================================================

// TestMCPKMSClient_KMSGenerateKey_AllBranches tests all optional branches.
func TestMCPKMSClient_KMSGenerateKey_AllBranches(t *testing.T) {
	ctx := context.Background()
	keyResp := map[string]interface{}{
		"key_id":    "gen-key",
		"algorithm": "AES-256-GCM",
		"status":    "active",
		"version":   float64(1),
	}

	t.Run("WithSymmetricAlgorithm", func(t *testing.T) {
		c := &mcpKMSClient{sdk: newTestSDKClient(keyResp, nil)}
		resp, err := c.KMSGenerateKey(ctx, &service.GenerateKeyRequest{
			KeyAttributes: &kctypes.KeyAttributes{
				SymmetricAlgorithm: kctypes.SymmetricAES256GCM,
			},
		})
		require.NoError(t, err)
		assert.NotNil(t, resp.Key)
		assert.Equal(t, "gen-key", resp.Key.KeyID)
	})

	t.Run("WithKeyAlgorithm", func(t *testing.T) {
		c := &mcpKMSClient{sdk: newTestSDKClient(keyResp, nil)}
		resp, err := c.KMSGenerateKey(ctx, &service.GenerateKeyRequest{
			KeyAttributes: &kctypes.KeyAttributes{
				KeyAlgorithm: x509.RSA,
			},
		})
		require.NoError(t, err)
		assert.NotNil(t, resp.Key)
	})

	t.Run("WithNameAndDescription", func(t *testing.T) {
		c := &mcpKMSClient{sdk: newTestSDKClient(keyResp, nil)}
		resp, err := c.KMSGenerateKey(ctx, &service.GenerateKeyRequest{
			Name:        "my-key",
			Description: "Test key",
		})
		require.NoError(t, err)
		assert.NotNil(t, resp.Key)
	})

	t.Run("WithDeletionProtection", func(t *testing.T) {
		c := &mcpKMSClient{sdk: newTestSDKClient(keyResp, nil)}
		resp, err := c.KMSGenerateKey(ctx, &service.GenerateKeyRequest{
			DeletionProtection: true,
		})
		require.NoError(t, err)
		assert.NotNil(t, resp.Key)
	})

	t.Run("WithUsageAndTags", func(t *testing.T) {
		c := &mcpKMSClient{sdk: newTestSDKClient(keyResp, nil)}
		resp, err := c.KMSGenerateKey(ctx, &service.GenerateKeyRequest{
			Usage: []service.KeyUsage{service.KeyUsageEncrypt, service.KeyUsageSign},
			Tags:  map[string]string{"env": "test"},
		})
		require.NoError(t, err)
		assert.NotNil(t, resp.Key)
	})

	t.Run("TransportError", func(t *testing.T) {
		c := &mcpKMSClient{sdk: newTestSDKClient(nil, fmt.Errorf("fail"))}
		resp, err := c.KMSGenerateKey(ctx, &service.GenerateKeyRequest{})
		assert.Error(t, err)
		assert.Nil(t, resp)
	})
}

// TestMCPKMSClient_KMSUnwrapKey_AllBranches tests all optional branches.
func TestMCPKMSClient_KMSUnwrapKey_AllBranches(t *testing.T) {
	ctx := context.Background()
	unwrapResp := map[string]interface{}{
		"key_id":    "unwrapped-key",
		"algorithm": "AES-256-GCM",
		"status":    "active",
		"version":   float64(1),
	}

	t.Run("WithSourceKeyIDAndName", func(t *testing.T) {
		c := &mcpKMSClient{sdk: newTestSDKClient(unwrapResp, nil)}
		resp, err := c.KMSUnwrapKey(ctx, &service.UnwrapKeyRequest{
			KeyID:             "key-1",
			WrappedKey:        []byte("wrapped-data"),
			WrappingKeyID:     "wrapper-key",
			WrappingAlgorithm: "RSA-OAEP",
			Algorithm:         "AES-256-GCM",
			SourceKeyID:       "source-key-1",
			Name:              "my-unwrapped",
		})
		require.NoError(t, err)
		assert.Equal(t, "unwrapped-key", resp.KeyID)
	})

	t.Run("MinimalArgs", func(t *testing.T) {
		c := &mcpKMSClient{sdk: newTestSDKClient(unwrapResp, nil)}
		resp, err := c.KMSUnwrapKey(ctx, &service.UnwrapKeyRequest{
			KeyID:             "key-1",
			WrappedKey:        []byte("wrapped-data"),
			WrappingKeyID:     "wrapper-key",
			WrappingAlgorithm: "RSA-OAEP",
			Algorithm:         "AES-256-GCM",
		})
		require.NoError(t, err)
		assert.Equal(t, "unwrapped-key", resp.KeyID)
	})

	t.Run("TransportError", func(t *testing.T) {
		c := &mcpKMSClient{sdk: newTestSDKClient(nil, fmt.Errorf("fail"))}
		resp, err := c.KMSUnwrapKey(ctx, &service.UnwrapKeyRequest{
			KeyID:      "k",
			WrappedKey: []byte("w"),
		})
		assert.Error(t, err)
		assert.Nil(t, resp)
	})
}

// TestMCPKMSClient_KMSImportKey_AllBranches tests optional branches.
func TestMCPKMSClient_KMSImportKey_AllBranches(t *testing.T) {
	ctx := context.Background()
	importResp := map[string]interface{}{
		"key_id":    "imported-key",
		"algorithm": "AES-256-GCM",
		"status":    "active",
		"version":   float64(1),
	}

	t.Run("WithAllOptionalFields", func(t *testing.T) {
		c := &mcpKMSClient{sdk: newTestSDKClient(importResp, nil)}
		resp, err := c.KMSImportKey(ctx, &service.ImportKeyRequest{
			KeyMaterial:   []byte("key-data"),
			WrappingKeyID: "wrapper-1",
			Name:          "imported",
			Description:   "An imported key",
			KeyAttributes: &kctypes.KeyAttributes{
				SymmetricAlgorithm: kctypes.SymmetricAES256GCM,
			},
		})
		require.NoError(t, err)
		assert.NotNil(t, resp.Key)
	})

	t.Run("WithKeyAlgorithm", func(t *testing.T) {
		c := &mcpKMSClient{sdk: newTestSDKClient(importResp, nil)}
		resp, err := c.KMSImportKey(ctx, &service.ImportKeyRequest{
			KeyMaterial: []byte("key-data"),
			KeyAttributes: &kctypes.KeyAttributes{
				KeyAlgorithm: x509.RSA,
			},
		})
		require.NoError(t, err)
		assert.NotNil(t, resp.Key)
	})

	t.Run("TransportError", func(t *testing.T) {
		c := &mcpKMSClient{sdk: newTestSDKClient(nil, fmt.Errorf("fail"))}
		resp, err := c.KMSImportKey(ctx, &service.ImportKeyRequest{KeyMaterial: []byte("k")})
		assert.Error(t, err)
		assert.Nil(t, resp)
	})
}

// TestParseBackendStatusResponse_WithCapabilities tests the capabilities branch.
func TestParseBackendStatusResponse_WithCapabilities(t *testing.T) {
	t.Run("WithCapabilities", func(t *testing.T) {
		data := map[string]interface{}{
			"backend_type": "tpm",
			"status":       "active",
			"device":       "/dev/tpm0",
			"capabilities": map[string]interface{}{
				"encrypt": true,
				"sign":    true,
				"wrap":    false,
			},
		}
		resp, err := parseBackendStatusResponse(data)
		require.NoError(t, err)
		assert.Equal(t, "tpm", resp.BackendType)
		assert.Len(t, resp.Capabilities, 3)
		assert.True(t, resp.Capabilities["encrypt"])
		assert.False(t, resp.Capabilities["wrap"])
	})

	t.Run("WithNonBoolCapabilities", func(t *testing.T) {
		data := map[string]interface{}{
			"backend_type": "tpm",
			"capabilities": map[string]interface{}{
				"encrypt":  true,
				"version":  "1.0",
				"max_keys": float64(100),
			},
		}
		resp, err := parseBackendStatusResponse(data)
		require.NoError(t, err)
		// Only bool values are included
		assert.Len(t, resp.Capabilities, 1)
		assert.True(t, resp.Capabilities["encrypt"])
	})

	t.Run("WithoutCapabilities", func(t *testing.T) {
		data := map[string]interface{}{
			"backend_type": "local",
		}
		resp, err := parseBackendStatusResponse(data)
		require.NoError(t, err)
		assert.Nil(t, resp.Capabilities)
	})
}

// =============================================================================
// Additional MCP KMS Method Branch Coverage
// =============================================================================

// TestMCPKMSClient_KMSUpdateKeyStatus tests the update key status method.
func TestMCPKMSClient_KMSUpdateKeyStatus(t *testing.T) {
	ctx := context.Background()
	respMap := map[string]interface{}{
		"key_id": "upd-key",
		"status": "disabled",
	}

	t.Run("WithReason", func(t *testing.T) {
		c := &mcpKMSClient{sdk: newTestSDKClient(respMap, nil)}
		resp, err := c.KMSUpdateKeyStatus(ctx, &service.UpdateKeyStatusRequest{
			KeyID:  "upd-key",
			Status: "disabled",
			Reason: "maintenance",
		})
		require.NoError(t, err)
		assert.NotNil(t, resp)
	})

	t.Run("WithoutReason", func(t *testing.T) {
		c := &mcpKMSClient{sdk: newTestSDKClient(respMap, nil)}
		resp, err := c.KMSUpdateKeyStatus(ctx, &service.UpdateKeyStatusRequest{
			KeyID:  "upd-key",
			Status: "active",
		})
		require.NoError(t, err)
		assert.NotNil(t, resp)
	})

	t.Run("WithVersion", func(t *testing.T) {
		c := &mcpKMSClient{sdk: newTestSDKClient(respMap, nil)}
		resp, err := c.KMSUpdateKeyStatus(ctx, &service.UpdateKeyStatusRequest{
			KeyID:   "upd-key",
			Status:  "disabled",
			Version: 3,
		})
		require.NoError(t, err)
		assert.NotNil(t, resp)
	})
}

// TestMCPKMSClient_KMSListKeyVersions tests the list key versions method.
func TestMCPKMSClient_KMSListKeyVersions(t *testing.T) {
	ctx := context.Background()
	respMap := map[string]interface{}{
		"key_id": "ver-key",
		"versions": []interface{}{
			map[string]interface{}{"version": float64(1), "status": "active"},
		},
	}

	t.Run("WithLimitAndCursor", func(t *testing.T) {
		c := &mcpKMSClient{sdk: newTestSDKClient(respMap, nil)}
		resp, err := c.KMSListKeyVersions(ctx, &service.ListKeyVersionsRequest{
			KeyID:  "ver-key",
			Limit:  10,
			Cursor: "next-page",
		})
		require.NoError(t, err)
		assert.NotNil(t, resp)
		assert.Equal(t, "ver-key", resp.KeyID)
		assert.Len(t, resp.Versions, 1)
	})

	t.Run("MinimalArgs", func(t *testing.T) {
		c := &mcpKMSClient{sdk: newTestSDKClient(respMap, nil)}
		resp, err := c.KMSListKeyVersions(ctx, &service.ListKeyVersionsRequest{
			KeyID: "ver-key",
		})
		require.NoError(t, err)
		assert.NotNil(t, resp)
	})

	t.Run("TransportError", func(t *testing.T) {
		c := &mcpKMSClient{sdk: newTestSDKClient(nil, fmt.Errorf("fail"))}
		resp, err := c.KMSListKeyVersions(ctx, &service.ListKeyVersionsRequest{
			KeyID: "ver-key",
		})
		assert.Error(t, err)
		assert.Nil(t, resp)
	})
}

// TestMCPKMSClient_KMSExportKey tests the export key method.
func TestMCPKMSClient_KMSExportKey(t *testing.T) {
	ctx := context.Background()
	respMap := map[string]interface{}{
		"key_id":      "exp-key",
		"version":     float64(1),
		"wrapped_key": "wrapped-data",
	}

	t.Run("WithAllOptionalFields", func(t *testing.T) {
		c := &mcpKMSClient{sdk: newTestSDKClient(respMap, nil)}
		resp, err := c.KMSExportKey(ctx, &service.ExportKeyRequest{
			KeyID:             "exp-key",
			WrappingKeyID:     "wrap-key",
			WrappingAlgorithm: "RSA-OAEP",
			Format:            "pkcs8",
		})
		require.NoError(t, err)
		assert.Equal(t, "exp-key", resp.KeyID)
	})

	t.Run("MinimalArgs", func(t *testing.T) {
		c := &mcpKMSClient{sdk: newTestSDKClient(respMap, nil)}
		resp, err := c.KMSExportKey(ctx, &service.ExportKeyRequest{
			KeyID: "exp-key",
		})
		require.NoError(t, err)
		assert.Equal(t, "exp-key", resp.KeyID)
	})

	t.Run("TransportError", func(t *testing.T) {
		c := &mcpKMSClient{sdk: newTestSDKClient(nil, fmt.Errorf("fail"))}
		resp, err := c.KMSExportKey(ctx, &service.ExportKeyRequest{KeyID: "k"})
		assert.Error(t, err)
		assert.Nil(t, resp)
	})
}

// TestMCPKMSClient_KMSGetKey tests the get key method.
func TestMCPKMSClient_KMSGetKey(t *testing.T) {
	ctx := context.Background()

	t.Run("WithVersion", func(t *testing.T) {
		respMap := map[string]interface{}{
			"key_id":    "get-key",
			"algorithm": "AES-256-GCM",
			"status":    "active",
			"version":   float64(2),
		}
		c := &mcpKMSClient{sdk: newTestSDKClient(respMap, nil)}
		resp, err := c.KMSGetKey(ctx, &service.GetKeyRequest{
			KeyID:   "get-key",
			Version: 2,
		})
		require.NoError(t, err)
		assert.Equal(t, "get-key", resp.Key.KeyID)
		assert.Equal(t, uint64(2), resp.Key.Version)
	})

	t.Run("WithNestedMetadata", func(t *testing.T) {
		respMap := map[string]interface{}{
			"metadata": map[string]interface{}{
				"key_id":    "nested-key",
				"algorithm": "RSA",
				"status":    "active",
				"version":   float64(1),
				"name":      "test-key",
			},
		}
		c := &mcpKMSClient{sdk: newTestSDKClient(respMap, nil)}
		resp, err := c.KMSGetKey(ctx, &service.GetKeyRequest{
			KeyID: "nested-key",
		})
		require.NoError(t, err)
		assert.Equal(t, "nested-key", resp.Key.KeyID)
		assert.Equal(t, "test-key", resp.Metadata.Name)
	})

	t.Run("TransportError", func(t *testing.T) {
		c := &mcpKMSClient{sdk: newTestSDKClient(nil, fmt.Errorf("fail"))}
		resp, err := c.KMSGetKey(ctx, &service.GetKeyRequest{KeyID: "k"})
		assert.Error(t, err)
		assert.Nil(t, resp)
	})
}

// TestMCPKMSClient_KMSListKeys tests the list keys method.
func TestMCPKMSClient_KMSListKeys(t *testing.T) {
	ctx := context.Background()

	t.Run("WithAllOptionalFields", func(t *testing.T) {
		respMap := map[string]interface{}{
			"keys": []interface{}{
				map[string]interface{}{
					"key_id":    "k-1",
					"algorithm": "AES-256-GCM",
					"status":    "active",
					"version":   float64(1),
				},
			},
			"next_cursor": "page2",
			"has_more":    true,
		}
		c := &mcpKMSClient{sdk: newTestSDKClient(respMap, nil)}
		resp, err := c.KMSListKeys(ctx, &service.ListKeysRequest{
			Limit:  10,
			Cursor: "page1",
			Filter: map[string]string{"status": "active"},
		})
		require.NoError(t, err)
		assert.Len(t, resp.Keys, 1)
		assert.Equal(t, "page2", resp.NextCursor)
		assert.True(t, resp.HasMore)
	})

	t.Run("MinimalArgs", func(t *testing.T) {
		respMap := map[string]interface{}{
			"keys": []interface{}{},
		}
		c := &mcpKMSClient{sdk: newTestSDKClient(respMap, nil)}
		resp, err := c.KMSListKeys(ctx, &service.ListKeysRequest{})
		require.NoError(t, err)
		assert.Empty(t, resp.Keys)
	})

	t.Run("TransportError", func(t *testing.T) {
		c := &mcpKMSClient{sdk: newTestSDKClient(nil, fmt.Errorf("fail"))}
		resp, err := c.KMSListKeys(ctx, &service.ListKeysRequest{})
		assert.Error(t, err)
		assert.Nil(t, resp)
	})
}

// TestMCPKMSClient_KMSRotateKey tests the rotate key method.
func TestMCPKMSClient_KMSRotateKey(t *testing.T) {
	ctx := context.Background()
	respMap := map[string]interface{}{
		"key_id":      "rot-key",
		"old_version": float64(1),
		"new_version": float64(2),
	}

	t.Run("Success", func(t *testing.T) {
		c := &mcpKMSClient{sdk: newTestSDKClient(respMap, nil)}
		resp, err := c.KMSRotateKey(ctx, &service.RotateKeyRequest{
			KeyID: "rot-key",
		})
		require.NoError(t, err)
		assert.Equal(t, "rot-key", resp.KeyID)
		assert.Equal(t, uint64(2), resp.NewVersion)
	})

	t.Run("TransportError", func(t *testing.T) {
		c := &mcpKMSClient{sdk: newTestSDKClient(nil, fmt.Errorf("fail"))}
		resp, err := c.KMSRotateKey(ctx, &service.RotateKeyRequest{KeyID: "k"})
		assert.Error(t, err)
		assert.Nil(t, resp)
	})
}

// TestMCPKMSClient_KMSDeleteKey tests the delete key method.
func TestMCPKMSClient_KMSDeleteKey(t *testing.T) {
	ctx := context.Background()
	respMap := map[string]interface{}{
		"key_id": "del-key",
		"status": "pending_deletion",
	}

	t.Run("WithAllOptionalFields", func(t *testing.T) {
		c := &mcpKMSClient{sdk: newTestSDKClient(respMap, nil)}
		resp, err := c.KMSDeleteKey(ctx, &service.DeleteKeyRequest{
			KeyID:             "del-key",
			PendingWindowDays: 30,
			Force:             true,
		})
		require.NoError(t, err)
		assert.Equal(t, "del-key", resp.KeyID)
	})

	t.Run("MinimalArgs", func(t *testing.T) {
		c := &mcpKMSClient{sdk: newTestSDKClient(respMap, nil)}
		resp, err := c.KMSDeleteKey(ctx, &service.DeleteKeyRequest{
			KeyID: "del-key",
		})
		require.NoError(t, err)
		assert.Equal(t, "del-key", resp.KeyID)
	})

	t.Run("TransportError", func(t *testing.T) {
		c := &mcpKMSClient{sdk: newTestSDKClient(nil, fmt.Errorf("fail"))}
		resp, err := c.KMSDeleteKey(ctx, &service.DeleteKeyRequest{KeyID: "k"})
		assert.Error(t, err)
		assert.Nil(t, resp)
	})
}

// TestMCPKMSClient_KMSSetKeySafety tests the set key safety method.
func TestMCPKMSClient_KMSSetKeySafety(t *testing.T) {
	ctx := context.Background()
	respMap := map[string]interface{}{
		"key_id": "safety-key",
		"config": map[string]interface{}{
			"max_operations":      float64(1000),
			"max_bytes":           float64(1048576),
			"warning_percent":     float64(80),
			"auto_rotate_percent": float64(90),
		},
	}

	t.Run("WithAllFields", func(t *testing.T) {
		c := &mcpKMSClient{sdk: newTestSDKClient(respMap, nil)}
		resp, err := c.KMSSetKeySafety(ctx, &service.SetKeySafetyRequest{
			KeyID:             "safety-key",
			MaxOperations:     1000,
			MaxBytes:          1048576,
			WarningPercent:    80,
			AutoRotatePercent: 90,
		})
		require.NoError(t, err)
		assert.Equal(t, "safety-key", resp.KeyID)
		assert.Equal(t, uint64(1000), resp.Config.MaxOperations)
	})

	t.Run("MinimalArgs", func(t *testing.T) {
		respNoConfig := map[string]interface{}{
			"key_id": "safety-key",
		}
		c := &mcpKMSClient{sdk: newTestSDKClient(respNoConfig, nil)}
		resp, err := c.KMSSetKeySafety(ctx, &service.SetKeySafetyRequest{
			KeyID: "safety-key",
		})
		require.NoError(t, err)
		assert.Equal(t, "safety-key", resp.KeyID)
	})

	t.Run("TransportError", func(t *testing.T) {
		c := &mcpKMSClient{sdk: newTestSDKClient(nil, fmt.Errorf("fail"))}
		resp, err := c.KMSSetKeySafety(ctx, &service.SetKeySafetyRequest{KeyID: "k"})
		assert.Error(t, err)
		assert.Nil(t, resp)
	})
}

// TestMCPKMSClient_KMSClearKeySafety tests the clear key safety method.
func TestMCPKMSClient_KMSClearKeySafety(t *testing.T) {
	ctx := context.Background()
	respMap := map[string]interface{}{
		"key_id": "clear-key",
	}

	t.Run("Success", func(t *testing.T) {
		c := &mcpKMSClient{sdk: newTestSDKClient(respMap, nil)}
		resp, err := c.KMSClearKeySafety(ctx, &service.ClearKeySafetyRequest{
			KeyID: "clear-key",
		})
		require.NoError(t, err)
		assert.Equal(t, "clear-key", resp.KeyID)
	})

	t.Run("TransportError", func(t *testing.T) {
		c := &mcpKMSClient{sdk: newTestSDKClient(nil, fmt.Errorf("fail"))}
		resp, err := c.KMSClearKeySafety(ctx, &service.ClearKeySafetyRequest{KeyID: "k"})
		assert.Error(t, err)
		assert.Nil(t, resp)
	})
}

// =============================================================================
// MCP Backend Client Method Tests
// =============================================================================

// TestMCPBackendClient_KMSBackendStatus tests the backend status method.
func TestMCPBackendClient_KMSBackendStatus(t *testing.T) {
	ctx := context.Background()

	t.Run("Success", func(t *testing.T) {
		respMap := map[string]interface{}{
			"backend_type": "tpm",
			"status":       "active",
			"device":       "/dev/tpm0",
			"region":       "us-east-1",
			"key_id":       "master-key",
		}
		c := &mcpBackendClient{sdk: newTestSDKClient(respMap, nil)}
		resp, err := c.KMSBackendStatus(ctx)
		require.NoError(t, err)
		assert.Equal(t, "tpm", resp.BackendType)
		assert.Equal(t, "active", resp.Status)
	})

	t.Run("TransportError", func(t *testing.T) {
		c := &mcpBackendClient{sdk: newTestSDKClient(nil, fmt.Errorf("fail"))}
		resp, err := c.KMSBackendStatus(ctx)
		assert.Error(t, err)
		assert.Nil(t, resp)
	})
}

// TestMCPBackendClient_KMSBackendTest tests the backend test method.
func TestMCPBackendClient_KMSBackendTest(t *testing.T) {
	ctx := context.Background()

	t.Run("Success", func(t *testing.T) {
		respMap := map[string]interface{}{
			"success":  true,
			"duration": "150ms",
			"message":  "all checks passed",
		}
		c := &mcpBackendClient{sdk: newTestSDKClient(respMap, nil)}
		resp, err := c.KMSBackendTest(ctx)
		require.NoError(t, err)
		assert.True(t, resp.Success)
		assert.Equal(t, "all checks passed", resp.Message)
	})

	t.Run("TransportError", func(t *testing.T) {
		c := &mcpBackendClient{sdk: newTestSDKClient(nil, fmt.Errorf("fail"))}
		resp, err := c.KMSBackendTest(ctx)
		assert.Error(t, err)
		assert.Nil(t, resp)
	})
}

// TestMCPBackendClient_Close tests the close method.
func TestMCPBackendClient_Close(t *testing.T) {
	c := &mcpBackendClient{sdk: newTestSDKClient(nil, nil)}
	err := c.Close()
	assert.NoError(t, err)
}

// TestMCPBackendClient_CallTool tests the internal callTool method.
func TestMCPBackendClient_CallTool(t *testing.T) {
	ctx := context.Background()

	t.Run("Success", func(t *testing.T) {
		expected := map[string]interface{}{"ok": true}
		c := &mcpBackendClient{sdk: newTestSDKClient(expected, nil)}
		result, err := c.callTool(ctx, "test_tool", map[string]interface{}{"arg": "val"})
		require.NoError(t, err)
		assert.Equal(t, expected, result)
	})

	t.Run("Error", func(t *testing.T) {
		c := &mcpBackendClient{sdk: newTestSDKClient(nil, fmt.Errorf("transport error"))}
		result, err := c.callTool(ctx, "test_tool", nil)
		assert.Error(t, err)
		assert.Nil(t, result)
	})
}

// =============================================================================
// MCP Safety Client Method Tests
// =============================================================================

// TestMCPSafetyClient_KMSSafetyCheck tests the safety check method.
func TestMCPSafetyClient_KMSSafetyCheck(t *testing.T) {
	ctx := context.Background()

	t.Run("WithThreshold", func(t *testing.T) {
		respMap := map[string]interface{}{
			"threshold_percent":  float64(80),
			"total_keys_checked": float64(10),
			"total_deks_checked": float64(5),
			"items": []interface{}{
				map[string]interface{}{
					"id":              "key-1",
					"type":            "key",
					"operation_count": float64(800),
					"bytes_processed": float64(1024),
					"usage_percent":   float64(80),
					"max_operations":  float64(1000),
					"max_bytes":       float64(2048),
				},
			},
		}
		c := &mcpSafetyClient{sdk: newTestSDKClient(respMap, nil)}
		resp, err := c.KMSSafetyCheck(ctx, &service.SafetyCheckRequest{
			ThresholdPercent: 80,
		})
		require.NoError(t, err)
		assert.Equal(t, float64(80), resp.ThresholdPercent)
		assert.Equal(t, 10, resp.TotalKeysChecked)
		assert.Len(t, resp.Items, 1)
	})

	t.Run("WithoutThreshold", func(t *testing.T) {
		respMap := map[string]interface{}{
			"threshold_percent":  float64(0),
			"total_keys_checked": float64(0),
		}
		c := &mcpSafetyClient{sdk: newTestSDKClient(respMap, nil)}
		resp, err := c.KMSSafetyCheck(ctx, &service.SafetyCheckRequest{})
		require.NoError(t, err)
		assert.NotNil(t, resp)
	})

	t.Run("TransportError", func(t *testing.T) {
		c := &mcpSafetyClient{sdk: newTestSDKClient(nil, fmt.Errorf("fail"))}
		resp, err := c.KMSSafetyCheck(ctx, &service.SafetyCheckRequest{})
		assert.Error(t, err)
		assert.Nil(t, resp)
	})
}

// TestMCPSafetyClient_KMSSafetyReport tests the safety report method.
func TestMCPSafetyClient_KMSSafetyReport(t *testing.T) {
	ctx := context.Background()

	t.Run("WithDetails", func(t *testing.T) {
		respMap := map[string]interface{}{
			"total_operations":      float64(5000),
			"total_bytes_processed": float64(10485760),
			"server_config": map[string]interface{}{
				"enforce":             true,
				"max_operations":      float64(100000),
				"max_bytes":           float64(104857600),
				"warning_percent":     float64(80),
				"auto_rotate_percent": float64(90),
			},
			"items": []interface{}{
				map[string]interface{}{
					"id":   "key-1",
					"type": "key",
				},
			},
		}
		c := &mcpSafetyClient{sdk: newTestSDKClient(respMap, nil)}
		resp, err := c.KMSSafetyReport(ctx, &service.SafetyReportRequest{
			IncludeDetails: true,
		})
		require.NoError(t, err)
		assert.Equal(t, uint64(5000), resp.TotalOperations)
		assert.True(t, resp.ServerConfig.Enforce)
		assert.Len(t, resp.Items, 1)
	})

	t.Run("WithoutDetails", func(t *testing.T) {
		respMap := map[string]interface{}{
			"total_operations": float64(0),
		}
		c := &mcpSafetyClient{sdk: newTestSDKClient(respMap, nil)}
		resp, err := c.KMSSafetyReport(ctx, &service.SafetyReportRequest{})
		require.NoError(t, err)
		assert.NotNil(t, resp)
	})

	t.Run("TransportError", func(t *testing.T) {
		c := &mcpSafetyClient{sdk: newTestSDKClient(nil, fmt.Errorf("fail"))}
		resp, err := c.KMSSafetyReport(ctx, &service.SafetyReportRequest{})
		assert.Error(t, err)
		assert.Nil(t, resp)
	})
}

// TestMCPSafetyClient_Close tests the close method.
func TestMCPSafetyClient_Close(t *testing.T) {
	c := &mcpSafetyClient{sdk: newTestSDKClient(nil, nil)}
	err := c.Close()
	assert.NoError(t, err)
}

// TestMCPSafetyClient_CallTool tests the internal callTool method.
func TestMCPSafetyClient_CallTool(t *testing.T) {
	ctx := context.Background()

	t.Run("Success", func(t *testing.T) {
		expected := map[string]interface{}{"ok": true}
		c := &mcpSafetyClient{sdk: newTestSDKClient(expected, nil)}
		result, err := c.callTool(ctx, "test_tool", map[string]interface{}{"arg": "val"})
		require.NoError(t, err)
		assert.Equal(t, expected, result)
	})

	t.Run("Error", func(t *testing.T) {
		c := &mcpSafetyClient{sdk: newTestSDKClient(nil, fmt.Errorf("transport error"))}
		result, err := c.callTool(ctx, "test_tool", nil)
		assert.Error(t, err)
		assert.Nil(t, result)
	})
}

// TestMCPKMSClient_Close tests the close method.
func TestMCPKMSClient_Close(t *testing.T) {
	c := &mcpKMSClient{sdk: newTestSDKClient(nil, nil)}
	err := c.Close()
	assert.NoError(t, err)
}

// TestMCPKMSClient_CallTool tests the internal callTool method.
func TestMCPKMSClient_CallTool(t *testing.T) {
	ctx := context.Background()

	t.Run("Success", func(t *testing.T) {
		expected := map[string]interface{}{"ok": true}
		c := &mcpKMSClient{sdk: newTestSDKClient(expected, nil)}
		result, err := c.callTool(ctx, "test_tool", map[string]interface{}{"arg": "val"})
		require.NoError(t, err)
		assert.Equal(t, expected, result)
	})

	t.Run("Error", func(t *testing.T) {
		c := &mcpKMSClient{sdk: newTestSDKClient(nil, fmt.Errorf("transport error"))}
		result, err := c.callTool(ctx, "test_tool", nil)
		assert.Error(t, err)
		assert.Nil(t, result)
	})
}

// =============================================================================
// Helper Function Tests
// =============================================================================

// TestToJSONMap tests the toJSONMap shared helper function.
func TestToJSONMap_Dispatch(t *testing.T) {
	t.Run("NilInput", func(t *testing.T) {
		result, err := toJSONMap(nil)
		require.NoError(t, err)
		assert.NotNil(t, result)
		assert.Empty(t, result)
	})

	t.Run("AlreadyMap", func(t *testing.T) {
		m := map[string]interface{}{"key": "value"}
		result, err := toJSONMap(m)
		require.NoError(t, err)
		assert.Equal(t, "value", result["key"])
	})

	t.Run("StructType", func(t *testing.T) {
		type testStruct struct {
			Name string `json:"name"`
		}
		s := testStruct{Name: "test"}
		result, err := toJSONMap(s)
		require.NoError(t, err)
		assert.Equal(t, "test", result["name"])
	})

	t.Run("UnmarshalableType", func(t *testing.T) {
		_, err := toJSONMap(make(chan int))
		assert.Error(t, err)
	})
}

// TestGetHelpers tests the get* helper functions with nil/missing values.
func TestGetHelpers_Dispatch(t *testing.T) {
	t.Run("GetStringNilMap", func(t *testing.T) {
		assert.Equal(t, "", getString(nil, "key"))
	})

	t.Run("GetStringMissing", func(t *testing.T) {
		m := map[string]interface{}{"other": "val"}
		assert.Equal(t, "", getString(m, "key"))
	})

	t.Run("GetStringNonString", func(t *testing.T) {
		m := map[string]interface{}{"key": 123}
		assert.Equal(t, "", getString(m, "key"))
	})

	t.Run("GetInt64NilMap", func(t *testing.T) {
		assert.Equal(t, int64(0), getInt64(nil, "key"))
	})

	t.Run("GetInt64IntType", func(t *testing.T) {
		m := map[string]interface{}{"key": int64(42)}
		assert.Equal(t, int64(42), getInt64(m, "key"))
	})

	t.Run("GetInt64IntValue", func(t *testing.T) {
		m := map[string]interface{}{"key": int(99)}
		assert.Equal(t, int64(99), getInt64(m, "key"))
	})

	t.Run("GetFloat64NilMap", func(t *testing.T) {
		assert.Equal(t, float64(0), getFloat64(nil, "key"))
	})

	t.Run("GetBoolNilMap", func(t *testing.T) {
		assert.False(t, getBool(nil, "key"))
	})

	t.Run("GetMapNilMap", func(t *testing.T) {
		assert.Nil(t, getMap(nil, "key"))
	})

	t.Run("GetBytesNilMap", func(t *testing.T) {
		assert.Nil(t, getBytes(nil, "key"))
	})

	t.Run("GetBytesAsByteSlice", func(t *testing.T) {
		m := map[string]interface{}{"key": []byte("hello")}
		assert.Equal(t, []byte("hello"), getBytes(m, "key"))
	})

	t.Run("GetStringMulti", func(t *testing.T) {
		m := map[string]interface{}{"b": "val"}
		assert.Equal(t, "val", getStringMulti(m, "a", "b", "c"))
	})

	t.Run("GetStringMultiMissing", func(t *testing.T) {
		m := map[string]interface{}{"d": "val"}
		assert.Equal(t, "", getStringMulti(m, "a", "b", "c"))
	})

	t.Run("GetInt64Multi", func(t *testing.T) {
		m := map[string]interface{}{"b": float64(42)}
		assert.Equal(t, int64(42), getInt64Multi(m, "a", "b"))
	})

	t.Run("GetInt64MultiMissing", func(t *testing.T) {
		m := map[string]interface{}{"d": float64(42)}
		assert.Equal(t, int64(0), getInt64Multi(m, "a", "b"))
	})

	t.Run("GetBoolMulti", func(t *testing.T) {
		m := map[string]interface{}{"b": true}
		assert.True(t, getBoolMulti(m, "a", "b"))
	})

	t.Run("GetBoolMultiMissing", func(t *testing.T) {
		m := map[string]interface{}{"d": true}
		assert.False(t, getBoolMulti(m, "a", "b"))
	})

	t.Run("GetSliceMulti", func(t *testing.T) {
		m := map[string]interface{}{"b": []interface{}{"one", "two"}}
		result := getSliceMulti(m, "a", "b")
		assert.Len(t, result, 2)
	})

	t.Run("GetSliceMultiMissing", func(t *testing.T) {
		m := map[string]interface{}{"d": []interface{}{"one"}}
		assert.Nil(t, getSliceMulti(m, "a", "b"))
	})
}

// TestParseTimestamp_Dispatch tests the parseTimestamp function.
func TestParseTimestamp_Dispatch(t *testing.T) {
	t.Run("Zero", func(t *testing.T) {
		ts := parseTimestamp(0)
		assert.True(t, ts.IsZero())
	})

	t.Run("Seconds", func(t *testing.T) {
		ts := parseTimestamp(1700000000)
		assert.False(t, ts.IsZero())
		assert.Equal(t, int64(1700000000), ts.Unix())
	})

	t.Run("Nanoseconds", func(t *testing.T) {
		ns := int64(1700000000000000000)
		ts := parseTimestamp(ns)
		assert.False(t, ts.IsZero())
		assert.Equal(t, int64(1700000000), ts.Unix())
	})

	t.Run("NegativeNanoseconds", func(t *testing.T) {
		ns := int64(-1700000000000000000)
		ts := parseTimestamp(ns)
		assert.False(t, ts.IsZero())
	})
}

// TestParseDuration_Dispatch tests the parseDuration function.
func TestParseDuration_Dispatch(t *testing.T) {
	t.Run("Empty", func(t *testing.T) {
		d := parseDuration("")
		assert.Equal(t, 0, int(d))
	})

	t.Run("Valid", func(t *testing.T) {
		d := parseDuration("150ms")
		assert.Greater(t, int(d), 0)
	})

	t.Run("Invalid", func(t *testing.T) {
		d := parseDuration("not-a-duration")
		assert.Equal(t, 0, int(d))
	})
}

// TestParseKeyAlgorithm_Dispatch tests the parseKeyAlgorithm function.
func TestParseKeyAlgorithm_Dispatch(t *testing.T) {
	alg := parseKeyAlgorithm("AES-256-GCM")
	assert.Equal(t, service.KeyAlgorithm("AES-256-GCM"), alg)
}
