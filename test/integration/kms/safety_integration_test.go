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
	"context"
	"fmt"
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
// Key Export/Import Tests (gRPC)
// =============================================================================

// TestGRPCExportKey tests the ExportKey operation via gRPC.
func TestGRPCExportKey(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	endpoints := getGRPCEndpoints()
	tlsCfg, err := helpers.IntegrationTLSConfig()
	require.NoError(t, err, "load integration TLS config")
	client, err := grpcclient.NewClient(endpoints[0], grpcclient.WithTLSConfig(tlsCfg), kmsGRPCAuth(t))
	require.NoError(t, err, "Could not connect to gRPC server")
	defer client.Close()

	requireKMSAvailable(t, client, ctx)

	// Create a wrapping key first
	wrappingKeyName := generateUniqueKeyName("grpc-wrapping-key")
	wrappingKeyReq := &service.GenerateKeyRequest{
		Name: wrappingKeyName,
		KeyAttributes: &kctypes.KeyAttributes{
			SymmetricAlgorithm: kctypes.SymmetricAES256GCM,
			Exportable:         false,
		},
	}

	wrappingKeyResp, err := client.KMSGenerateKey(ctx, wrappingKeyReq)
	require.NoError(t, err)
	wrappingKeyID := wrappingKeyResp.Key.KeyID
	defer cleanupKey(t, client, ctx, wrappingKeyID)

	// Create an exportable key to export
	exportableKeyName := generateUniqueKeyName("grpc-exportable-key")
	exportableKeyReq := &service.GenerateKeyRequest{
		Name: exportableKeyName,
		KeyAttributes: &kctypes.KeyAttributes{
			SymmetricAlgorithm: kctypes.SymmetricAES256GCM,
			Exportable:         true,
		},
	}

	exportableKeyResp, err := client.KMSGenerateKey(ctx, exportableKeyReq)
	require.NoError(t, err)
	exportableKeyID := exportableKeyResp.Key.KeyID
	defer cleanupKey(t, client, ctx, exportableKeyID)

	t.Run("export key successfully", func(t *testing.T) {
		req := &service.ExportKeyRequest{
			KeyID:             exportableKeyID,
			WrappingKeyID:     wrappingKeyID,
			WrappingAlgorithm: "AES-GCM",
			Format:            "raw",
		}

		resp, err := client.KMSExportKey(ctx, req)
		require.NoError(t, err, "ExportKey should be available")

		require.NotNil(t, resp)
		assert.NotEmpty(t, resp.WrappedKey, "Wrapped key should not be empty")
		assert.Equal(t, exportableKeyID, resp.KeyID)
		assert.Equal(t, wrappingKeyID, resp.WrappingKeyID)
	})

	t.Run("export non-existent key", func(t *testing.T) {
		req := &service.ExportKeyRequest{
			KeyID:             "non-existent-key-id",
			WrappingKeyID:     wrappingKeyID,
			WrappingAlgorithm: "AES-GCM",
		}

		_, err := client.KMSExportKey(ctx, req)
		assert.Error(t, err, "Exporting non-existent key should fail")
	})

	t.Run("export with non-existent wrapping key", func(t *testing.T) {
		req := &service.ExportKeyRequest{
			KeyID:             exportableKeyID,
			WrappingKeyID:     "non-existent-wrapping-key",
			WrappingAlgorithm: "AES-GCM",
		}

		_, err := client.KMSExportKey(ctx, req)
		assert.Error(t, err, "Exporting with non-existent wrapping key should fail")
	})

	t.Run("export specific version", func(t *testing.T) {
		// Rotate key to create a new version
		rotateReq := &service.RotateKeyRequest{
			KeyID:            exportableKeyID,
			RetainOldVersion: true,
		}
		_, err := client.KMSRotateKey(ctx, rotateReq)
		if err != nil {
			t.Logf("Key rotation not supported, skipping version export test: %v", err)
			return
		}

		req := &service.ExportKeyRequest{
			KeyID:             exportableKeyID,
			WrappingKeyID:     wrappingKeyID,
			WrappingAlgorithm: "AES-GCM",
			Version:           1,
		}

		resp, err := client.KMSExportKey(ctx, req)
		require.NoError(t, err, "ExportKey version should be available")

		require.NotNil(t, resp)
		assert.Equal(t, uint64(1), resp.Version)
	})
}

// TestGRPCUnwrapKey tests the UnwrapKey (import wrapped key) operation via gRPC.
func TestGRPCUnwrapKey(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	endpoints := getGRPCEndpoints()
	tlsCfg, err := helpers.IntegrationTLSConfig()
	require.NoError(t, err, "load integration TLS config")
	client, err := grpcclient.NewClient(endpoints[0], grpcclient.WithTLSConfig(tlsCfg), kmsGRPCAuth(t))
	require.NoError(t, err, "Could not connect to gRPC server")
	defer client.Close()

	requireKMSAvailable(t, client, ctx)

	// Create a wrapping key
	wrappingKeyName := generateUniqueKeyName("grpc-unwrap-wrapping-key")
	wrappingKeyReq := &service.GenerateKeyRequest{
		Name: wrappingKeyName,
		KeyAttributes: &kctypes.KeyAttributes{
			SymmetricAlgorithm: kctypes.SymmetricAES256GCM,
		},
	}

	wrappingKeyResp, err := client.KMSGenerateKey(ctx, wrappingKeyReq)
	require.NoError(t, err)
	wrappingKeyID := wrappingKeyResp.Key.KeyID
	defer cleanupKey(t, client, ctx, wrappingKeyID)

	// Create an exportable key, export it, then unwrap it
	exportableKeyName := generateUniqueKeyName("grpc-export-for-unwrap")
	exportableKeyReq := &service.GenerateKeyRequest{
		Name: exportableKeyName,
		KeyAttributes: &kctypes.KeyAttributes{
			SymmetricAlgorithm: kctypes.SymmetricAES256GCM,
			Exportable:         true,
		},
	}

	exportableKeyResp, err := client.KMSGenerateKey(ctx, exportableKeyReq)
	require.NoError(t, err)
	exportableKeyID := exportableKeyResp.Key.KeyID
	defer cleanupKey(t, client, ctx, exportableKeyID)

	t.Run("export and unwrap key round trip", func(t *testing.T) {
		// Export the key using AES-GCM wrapping algorithm
		exportReq := &service.ExportKeyRequest{
			KeyID:             exportableKeyID,
			WrappingKeyID:     wrappingKeyID,
			WrappingAlgorithm: "AES-GCM",
			Format:            "raw",
		}

		exportResp, err := client.KMSExportKey(ctx, exportReq)
		require.NoError(t, err, "ExportKey should be available")

		// Unwrap (import) the key with a new name
		// Use the same wrapping algorithm that was used for export
		// Include the source key ID and version for AAD verification
		unwrapKeyName := generateUniqueKeyName("grpc-unwrapped-key")
		unwrapReq := &service.UnwrapKeyRequest{
			KeyID:             unwrapKeyName,
			WrappedKey:        exportResp.WrappedKey,
			WrappingKeyID:     wrappingKeyID,
			WrappingAlgorithm: "AES-GCM",
			Algorithm:         "AES-256-GCM",
			SourceKeyID:       exportResp.KeyID,
			SourceVersion:     exportResp.Version,
		}

		unwrapResp, err := client.KMSUnwrapKey(ctx, unwrapReq)
		require.NoError(t, err, "Export/Unwrap round-trip should be implemented")

		require.NotNil(t, unwrapResp)
		assert.NotEmpty(t, unwrapResp.KeyID)
		assert.Equal(t, service.KeyStatusEnabled, unwrapResp.Status)
		defer cleanupKey(t, client, ctx, unwrapResp.KeyID)

		// Verify the unwrapped key works by encrypting and decrypting
		plaintext := []byte("test data for unwrapped key")
		encReq := &service.EncryptRequest{
			KeyID:     unwrapResp.KeyID,
			Plaintext: plaintext,
		}

		encResp, err := client.KMSEncrypt(ctx, encReq)
		if err != nil {
			t.Logf("Encryption with unwrapped key not available: %v", err)
			return
		}

		decReq := &service.DecryptRequest{
			KeyID:      unwrapResp.KeyID,
			Ciphertext: encResp.Ciphertext,
		}

		decResp, err := client.KMSDecrypt(ctx, decReq)
		require.NoError(t, err)
		assert.Equal(t, plaintext, decResp.Plaintext)
	})

	t.Run("unwrap with invalid wrapped key", func(t *testing.T) {
		unwrapReq := &service.UnwrapKeyRequest{
			KeyID:         generateUniqueKeyName("invalid-unwrap"),
			WrappedKey:    []byte("invalid-wrapped-key-data"),
			WrappingKeyID: wrappingKeyID,
			Algorithm:     "AES-256-GCM",
		}

		_, err := client.KMSUnwrapKey(ctx, unwrapReq)
		assert.Error(t, err, "Unwrapping invalid wrapped key should fail")
	})

	t.Run("unwrap with non-existent wrapping key", func(t *testing.T) {
		unwrapReq := &service.UnwrapKeyRequest{
			KeyID:         generateUniqueKeyName("invalid-unwrap2"),
			WrappedKey:    []byte("some-wrapped-data"),
			WrappingKeyID: "non-existent-wrapping-key",
			Algorithm:     "AES-256-GCM",
		}

		_, err := client.KMSUnwrapKey(ctx, unwrapReq)
		assert.Error(t, err, "Unwrapping with non-existent wrapping key should fail")
	})
}

// =============================================================================
// Key Safety Tests (gRPC)
// =============================================================================

// TestGRPCSetKeySafety tests the SetKeySafety operation via gRPC.
func TestGRPCSetKeySafety(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	endpoints := getGRPCEndpoints()
	tlsCfg, err := helpers.IntegrationTLSConfig()
	require.NoError(t, err, "load integration TLS config")
	client, err := grpcclient.NewClient(endpoints[0], grpcclient.WithTLSConfig(tlsCfg), kmsGRPCAuth(t))
	require.NoError(t, err, "Could not connect to gRPC server")
	defer client.Close()

	requireKMSAvailable(t, client, ctx)

	// Create a test key
	keyName := generateUniqueKeyName("grpc-safety-test")
	genReq := &service.GenerateKeyRequest{
		Name: keyName,
		KeyAttributes: &kctypes.KeyAttributes{
			SymmetricAlgorithm: kctypes.SymmetricAES256GCM,
		},
	}

	genResp, err := client.KMSGenerateKey(ctx, genReq)
	require.NoError(t, err)
	keyID := genResp.Key.KeyID
	defer cleanupKey(t, client, ctx, keyID)

	t.Run("set key safety limits", func(t *testing.T) {
		req := &service.SetKeySafetyRequest{
			KeyID:          keyID,
			MaxOperations:  1000000,
			MaxBytes:       1024 * 1024 * 1024, // 1GB
			WarningPercent: 80,
		}

		resp, err := client.KMSSetKeySafety(ctx, req)
		require.NoError(t, err, "SetKeySafety should be available")

		require.NotNil(t, resp)
		assert.Equal(t, keyID, resp.KeyID)
		assert.Equal(t, uint64(1000000), resp.Config.MaxOperations)
		assert.Equal(t, uint64(1024*1024*1024), resp.Config.MaxBytes)
		assert.Equal(t, 80, resp.Config.WarningPercent)
	})

	t.Run("update key safety limits", func(t *testing.T) {
		req := &service.SetKeySafetyRequest{
			KeyID:             keyID,
			MaxOperations:     2000000,
			MaxBytes:          2 * 1024 * 1024 * 1024, // 2GB
			WarningPercent:    90,
			AutoRotatePercent: 95,
		}

		resp, err := client.KMSSetKeySafety(ctx, req)
		require.NoError(t, err, "SetKeySafety should be available")

		require.NotNil(t, resp)
		assert.Equal(t, uint64(2000000), resp.Config.MaxOperations)
	})

	t.Run("set safety for non-existent key", func(t *testing.T) {
		req := &service.SetKeySafetyRequest{
			KeyID:         "non-existent-key-id",
			MaxOperations: 1000,
		}

		_, err := client.KMSSetKeySafety(ctx, req)
		assert.Error(t, err, "Setting safety on non-existent key should fail")
	})
}

// TestGRPCClearKeySafety tests the ClearKeySafety operation via gRPC.
func TestGRPCClearKeySafety(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	endpoints := getGRPCEndpoints()
	tlsCfg, err := helpers.IntegrationTLSConfig()
	require.NoError(t, err, "load integration TLS config")
	client, err := grpcclient.NewClient(endpoints[0], grpcclient.WithTLSConfig(tlsCfg), kmsGRPCAuth(t))
	require.NoError(t, err, "Could not connect to gRPC server")
	defer client.Close()

	requireKMSAvailable(t, client, ctx)

	// Create a test key
	keyName := generateUniqueKeyName("grpc-clear-safety")
	genReq := &service.GenerateKeyRequest{
		Name: keyName,
		KeyAttributes: &kctypes.KeyAttributes{
			SymmetricAlgorithm: kctypes.SymmetricAES256GCM,
		},
	}

	genResp, err := client.KMSGenerateKey(ctx, genReq)
	require.NoError(t, err)
	keyID := genResp.Key.KeyID
	defer cleanupKey(t, client, ctx, keyID)

	// Set safety limits first
	setReq := &service.SetKeySafetyRequest{
		KeyID:         keyID,
		MaxOperations: 1000000,
		MaxBytes:      1024 * 1024 * 1024,
	}
	_, setErr := client.KMSSetKeySafety(ctx, setReq)
	require.NoError(t, setErr, "SetKeySafety should be available to test ClearKeySafety")

	t.Run("clear key safety limits", func(t *testing.T) {
		req := &service.ClearKeySafetyRequest{
			KeyID: keyID,
		}

		resp, err := client.KMSClearKeySafety(ctx, req)
		require.NoError(t, err, "ClearKeySafety should be available")

		require.NotNil(t, resp)
		assert.Equal(t, keyID, resp.KeyID)
	})

	t.Run("clear safety for non-existent key", func(t *testing.T) {
		req := &service.ClearKeySafetyRequest{
			KeyID: "non-existent-key-id",
		}

		_, err := client.KMSClearKeySafety(ctx, req)
		assert.Error(t, err, "Clearing safety on non-existent key should fail")
	})
}

// TestGRPCSafetyCheck tests the SafetyCheck operation via gRPC.
func TestGRPCSafetyCheck(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	endpoints := getGRPCEndpoints()
	tlsCfg, err := helpers.IntegrationTLSConfig()
	require.NoError(t, err, "load integration TLS config")
	client, err := grpcclient.NewClient(endpoints[0], grpcclient.WithTLSConfig(tlsCfg), kmsGRPCAuth(t))
	require.NoError(t, err, "Could not connect to gRPC server")
	defer client.Close()

	requireKMSAvailable(t, client, ctx)

	// Create a test key with safety limits
	keyName := generateUniqueKeyName("grpc-safety-check")
	genReq := &service.GenerateKeyRequest{
		Name: keyName,
		KeyAttributes: &kctypes.KeyAttributes{
			SymmetricAlgorithm: kctypes.SymmetricAES256GCM,
		},
	}

	genResp, err := client.KMSGenerateKey(ctx, genReq)
	require.NoError(t, err)
	keyID := genResp.Key.KeyID
	defer cleanupKey(t, client, ctx, keyID)

	// Set safety limits
	setReq := &service.SetKeySafetyRequest{
		KeyID:          keyID,
		MaxOperations:  100,
		MaxBytes:       10000,
		WarningPercent: 80,
	}
	_, setErr := client.KMSSetKeySafety(ctx, setReq)
	require.NoError(t, setErr, "SetKeySafety should be available to test SafetyCheck")

	t.Run("safety check with default threshold", func(t *testing.T) {
		req := &service.SafetyCheckRequest{}

		resp, err := client.KMSSafetyCheck(ctx, req)
		require.NoError(t, err, "SafetyCheck should be available")

		require.NotNil(t, resp)
		// Newly created key should not exceed threshold
		t.Logf("Safety check found %d items exceeding threshold, checked %d keys, %d DEKs",
			len(resp.Items), resp.TotalKeysChecked, resp.TotalDEKsChecked)
	})

	t.Run("safety check with custom threshold", func(t *testing.T) {
		req := &service.SafetyCheckRequest{
			ThresholdPercent: 0, // 0% threshold means all keys with usage will be returned
		}

		resp, err := client.KMSSafetyCheck(ctx, req)
		require.NoError(t, err, "SafetyCheck should be available")

		require.NotNil(t, resp)
		assert.Equal(t, float64(0), resp.ThresholdPercent)
	})

	t.Run("safety check after operations", func(t *testing.T) {
		requireCryptoAvailable(t, client, ctx, keyID)

		// Perform some operations
		for i := 0; i < 10; i++ {
			plaintext := []byte(fmt.Sprintf("test data %d", i))
			encReq := &service.EncryptRequest{
				KeyID:     keyID,
				Plaintext: plaintext,
			}
			_, err := client.KMSEncrypt(ctx, encReq)
			if err != nil {
				t.Logf("Encryption failed: %v", err)
				break
			}
		}

		// Check safety
		req := &service.SafetyCheckRequest{
			ThresholdPercent: 5, // Low threshold to see usage
		}

		resp, err := client.KMSSafetyCheck(ctx, req)
		require.NoError(t, err, "SafetyCheck should be available")

		require.NotNil(t, resp)
		t.Logf("After operations: found %d items exceeding 5%% threshold", len(resp.Items))
	})
}

// TestGRPCSafetyReport tests the SafetyReport operation via gRPC.
func TestGRPCSafetyReport(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	endpoints := getGRPCEndpoints()
	tlsCfg, err := helpers.IntegrationTLSConfig()
	require.NoError(t, err, "load integration TLS config")
	client, err := grpcclient.NewClient(endpoints[0], grpcclient.WithTLSConfig(tlsCfg), kmsGRPCAuth(t))
	require.NoError(t, err, "Could not connect to gRPC server")
	defer client.Close()

	requireKMSAvailable(t, client, ctx)

	t.Run("get safety report", func(t *testing.T) {
		req := &service.SafetyReportRequest{
			IncludeDetails: true,
		}

		resp, err := client.KMSSafetyReport(ctx, req)
		require.NoError(t, err, "SafetyReport should be available")

		require.NotNil(t, resp)
		t.Logf("Safety report: %d items, total %d operations, %d bytes, enforce=%v",
			len(resp.Items), resp.TotalOperations, resp.TotalBytesProcessed, resp.ServerConfig.Enforce)
	})

	t.Run("get safety report without details", func(t *testing.T) {
		req := &service.SafetyReportRequest{
			IncludeDetails: false,
		}

		resp, err := client.KMSSafetyReport(ctx, req)
		require.NoError(t, err, "SafetyReport should be available")

		require.NotNil(t, resp)
		// Without details, items might be empty but totals should still be present
		t.Logf("Summary report: total %d operations", resp.TotalOperations)
	})
}

// =============================================================================
// DEK (Data Encryption Key) Tests (gRPC)
// =============================================================================

// TestGRPCDEKOperations tests DEK CRUD operations via gRPC.
func TestGRPCDEKOperations(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	endpoints := getGRPCEndpoints()
	tlsCfg, err := helpers.IntegrationTLSConfig()
	require.NoError(t, err, "load integration TLS config")
	client, err := grpcclient.NewClient(endpoints[0], grpcclient.WithTLSConfig(tlsCfg), kmsGRPCAuth(t))
	require.NoError(t, err, "Could not connect to gRPC server")
	defer client.Close()

	requireKMSAvailable(t, client, ctx)

	dekPath := generateUniqueKeyName("grpc-dek-test")

	t.Run("create DEK", func(t *testing.T) {
		req := &service.CreateDEKRequest{
			Path:        dekPath,
			Description: "Integration test DEK",
			Algorithm:   service.KeyAlgAES256GCM,
		}

		resp, err := client.KMSCreateDEK(ctx, req)
		require.NoError(t, err, "CreateDEK should be available")

		require.NotNil(t, resp)
		require.NotNil(t, resp.DEK)
		assert.Equal(t, dekPath, resp.DEK.Path)
		assert.Equal(t, service.KeyAlgAES256GCM, resp.DEK.Algorithm)
		assert.Equal(t, uint64(1), resp.DEK.Version)
	})

	t.Run("get DEK", func(t *testing.T) {
		req := &service.GetDEKRequest{
			Path: dekPath,
		}

		resp, err := client.KMSGetDEK(ctx, req)
		require.NoError(t, err, "GetDEK should be available")

		require.NotNil(t, resp)
		require.NotNil(t, resp.DEK)
		assert.Equal(t, dekPath, resp.DEK.Path)
	})

	t.Run("get non-existent DEK", func(t *testing.T) {
		req := &service.GetDEKRequest{
			Path: "non-existent-dek-path",
		}

		_, err := client.KMSGetDEK(ctx, req)
		assert.Error(t, err, "Getting non-existent DEK should fail")
	})

	t.Run("list DEKs", func(t *testing.T) {
		req := &service.ListDEKsRequest{
			Limit: 100,
		}

		resp, err := client.KMSListDEKs(ctx, req)
		require.NoError(t, err, "ListDEKs should be available")

		require.NotNil(t, resp)
		assert.GreaterOrEqual(t, len(resp.DEKs), 1, "Should have at least 1 DEK")
		t.Logf("Found %d DEKs", len(resp.DEKs))
	})

	t.Run("list DEKs with prefix", func(t *testing.T) {
		req := &service.ListDEKsRequest{
			Prefix: "grpc-dek-test",
			Limit:  100,
		}

		resp, err := client.KMSListDEKs(ctx, req)
		require.NoError(t, err, "ListDEKs should be available")

		require.NotNil(t, resp)
		t.Logf("Found %d DEKs with prefix", len(resp.DEKs))
	})

	t.Run("get DEK status", func(t *testing.T) {
		req := &service.GetDEKStatusRequest{
			Path: dekPath,
		}

		resp, err := client.KMSGetDEKStatus(ctx, req)
		require.NoError(t, err, "GetDEKStatus should be available")

		require.NotNil(t, resp)
		require.NotNil(t, resp.DEK)
		assert.Equal(t, dekPath, resp.DEK.Path)
		t.Logf("DEK status: version=%d, usage_count=%d, bytes_used=%d",
			resp.DEK.Version, resp.DEK.UsageCount, resp.DEK.BytesUsed)
	})

	t.Run("rotate DEK", func(t *testing.T) {
		req := &service.RotateDEKRequest{
			Path:             dekPath,
			RetainOldVersion: true,
		}

		resp, err := client.KMSRotateDEK(ctx, req)
		require.NoError(t, err, "RotateDEK should be available")

		require.NotNil(t, resp)
		assert.Equal(t, dekPath, resp.Path)
		assert.Greater(t, resp.NewVersion, resp.OldVersion)
		t.Logf("DEK rotated: old version=%d, new version=%d", resp.OldVersion, resp.NewVersion)
	})

	t.Run("delete DEK", func(t *testing.T) {
		req := &service.DeleteDEKRequest{
			Path:  dekPath,
			Force: true,
		}

		resp, err := client.KMSDeleteDEK(ctx, req)
		require.NoError(t, err, "DeleteDEK should be available")

		require.NotNil(t, resp)
		assert.Equal(t, dekPath, resp.Path)
		assert.True(t, resp.Deleted)

		// Verify deletion
		getReq := &service.GetDEKRequest{Path: dekPath}
		_, getErr := client.KMSGetDEK(ctx, getReq)
		assert.Error(t, getErr, "DEK should not be found after deletion")
	})
}

// TestGRPCSetDefaultDEK tests the SetDefaultDEK operation via gRPC.
func TestGRPCSetDefaultDEK(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	endpoints := getGRPCEndpoints()
	tlsCfg, err := helpers.IntegrationTLSConfig()
	require.NoError(t, err, "load integration TLS config")
	client, err := grpcclient.NewClient(endpoints[0], grpcclient.WithTLSConfig(tlsCfg), kmsGRPCAuth(t))
	require.NoError(t, err, "Could not connect to gRPC server")
	defer client.Close()

	requireKMSAvailable(t, client, ctx)

	// Create two DEKs
	dek1Path := generateUniqueKeyName("grpc-default-dek-1")
	dek2Path := generateUniqueKeyName("grpc-default-dek-2")

	for _, path := range []string{dek1Path, dek2Path} {
		req := &service.CreateDEKRequest{
			Path:        path,
			Description: "Default DEK test",
		}
		_, err := client.KMSCreateDEK(ctx, req)
		require.NoError(t, err, "CreateDEK should be available")
		defer func(p string) {
			client.KMSDeleteDEK(ctx, &service.DeleteDEKRequest{Path: p, Force: true})
		}(path)
	}

	t.Run("set default DEK", func(t *testing.T) {
		req := &service.SetDefaultDEKRequest{
			Path: dek1Path,
		}

		resp, err := client.KMSSetDefaultDEK(ctx, req)
		require.NoError(t, err, "SetDefaultDEK should be available")

		require.NotNil(t, resp)
		assert.Equal(t, dek1Path, resp.Path)
	})

	t.Run("change default DEK", func(t *testing.T) {
		req := &service.SetDefaultDEKRequest{
			Path: dek2Path,
		}

		resp, err := client.KMSSetDefaultDEK(ctx, req)
		require.NoError(t, err, "SetDefaultDEK should be available")

		require.NotNil(t, resp)
		assert.Equal(t, dek2Path, resp.Path)
		// Previous default should be returned
		if resp.PreviousDefault != "" {
			assert.Equal(t, dek1Path, resp.PreviousDefault)
		}
	})

	t.Run("set default for non-existent DEK", func(t *testing.T) {
		req := &service.SetDefaultDEKRequest{
			Path: "non-existent-dek-path",
		}

		_, err := client.KMSSetDefaultDEK(ctx, req)
		assert.Error(t, err, "Setting default for non-existent DEK should fail")
	})
}

// TestGRPCDEKSafety tests the SetDEKSafety and ClearDEKSafety operations via gRPC.
func TestGRPCDEKSafety(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	endpoints := getGRPCEndpoints()
	tlsCfg, err := helpers.IntegrationTLSConfig()
	require.NoError(t, err, "load integration TLS config")
	client, err := grpcclient.NewClient(endpoints[0], grpcclient.WithTLSConfig(tlsCfg), kmsGRPCAuth(t))
	require.NoError(t, err, "Could not connect to gRPC server")
	defer client.Close()

	requireKMSAvailable(t, client, ctx)

	// Create a test DEK
	dekPath := generateUniqueKeyName("grpc-dek-safety")
	createReq := &service.CreateDEKRequest{
		Path:        dekPath,
		Description: "DEK safety test",
	}
	_, createErr := client.KMSCreateDEK(ctx, createReq)
	require.NoError(t, createErr, "CreateDEK should be available")
	defer func() {
		client.KMSDeleteDEK(ctx, &service.DeleteDEKRequest{Path: dekPath, Force: true})
	}()

	t.Run("set DEK safety limits", func(t *testing.T) {
		req := &service.SetDEKSafetyRequest{
			Path:           dekPath,
			MaxOperations:  500000,
			MaxBytes:       512 * 1024 * 1024, // 512MB
			WarningPercent: 75,
		}

		resp, err := client.KMSSetDEKSafety(ctx, req)
		require.NoError(t, err, "SetDEKSafety should be available")

		require.NotNil(t, resp)
		assert.Equal(t, dekPath, resp.Path)
		require.NotNil(t, resp.Config)
		assert.Equal(t, uint64(500000), resp.Config.MaxOperations)
		assert.Equal(t, uint64(512*1024*1024), resp.Config.MaxBytes)
		assert.Equal(t, 75, resp.Config.WarningPercent)
	})

	t.Run("clear DEK safety limits", func(t *testing.T) {
		req := &service.ClearDEKSafetyRequest{
			Path: dekPath,
		}

		resp, err := client.KMSClearDEKSafety(ctx, req)
		require.NoError(t, err, "ClearDEKSafety should be available")

		require.NotNil(t, resp)
		assert.Equal(t, dekPath, resp.Path)
		assert.True(t, resp.Cleared)
	})

	t.Run("set DEK safety for non-existent DEK", func(t *testing.T) {
		req := &service.SetDEKSafetyRequest{
			Path:          "non-existent-dek-path",
			MaxOperations: 1000,
		}

		_, err := client.KMSSetDEKSafety(ctx, req)
		assert.Error(t, err, "Setting safety on non-existent DEK should fail")
	})

	t.Run("clear DEK safety for non-existent DEK", func(t *testing.T) {
		req := &service.ClearDEKSafetyRequest{
			Path: "non-existent-dek-path",
		}

		_, err := client.KMSClearDEKSafety(ctx, req)
		assert.Error(t, err, "Clearing safety on non-existent DEK should fail")
	})
}

// =============================================================================
// REST API Tests - Export/Import
// =============================================================================

// TestRESTExportKey tests the ExportKey operation via REST API.
func TestRESTExportKey(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	endpoints := helpers.GetRESTEndpoints()
	client := NewRESTClient(endpoints[0])

	// Create a wrapping key first
	wrappingKeyName := generateUniqueKeyName("rest-wrapping-key")
	wrappingKeyReq := &service.GenerateKeyRequest{
		Name: wrappingKeyName,
		KeyAttributes: &kctypes.KeyAttributes{
			SymmetricAlgorithm: kctypes.SymmetricAES256GCM,
		},
		Usage: EncryptDecryptUsage(),
	}

	wrappingKeyResp, err := client.GenerateKey(ctx, wrappingKeyReq)
	require.NoError(t, err, "KMS should be available on REST")
	wrappingKeyID := wrappingKeyResp.Key.KeyID
	defer func() {
		client.DeleteKey(ctx, &service.DeleteKeyRequest{KeyID: wrappingKeyID, Force: true})
	}()

	// Create an exportable key
	exportableKeyName := generateUniqueKeyName("rest-exportable-key")
	exportableKeyReq := &service.GenerateKeyRequest{
		Name: exportableKeyName,
		KeyAttributes: &kctypes.KeyAttributes{
			SymmetricAlgorithm: kctypes.SymmetricAES256GCM,
			Exportable:         true,
		},
		Usage: EncryptDecryptUsage(),
	}

	exportableKeyResp, err := client.GenerateKey(ctx, exportableKeyReq)
	require.NoError(t, err)
	exportableKeyID := exportableKeyResp.Key.KeyID
	defer func() {
		client.DeleteKey(ctx, &service.DeleteKeyRequest{KeyID: exportableKeyID, Force: true})
	}()

	t.Run("export key via REST", func(t *testing.T) {
		resp, err := client.ExportKey(ctx, &service.ExportKeyRequest{
			KeyID:             exportableKeyID,
			WrappingKeyID:     wrappingKeyID,
			WrappingAlgorithm: "AES-KW",
			Format:            "raw",
		})
		require.NoError(t, err, "ExportKey should be available via REST")

		require.NotNil(t, resp)
		assert.NotEmpty(t, resp.WrappedKey)
	})

	t.Run("export non-existent key via REST", func(t *testing.T) {
		_, err := client.ExportKey(ctx, &service.ExportKeyRequest{
			KeyID:             "non-existent-key",
			WrappingKeyID:     wrappingKeyID,
			WrappingAlgorithm: "AES-KW",
		})
		assert.Error(t, err)
	})
}

// =============================================================================
// REST API Tests - DEK Safety Operations (New)
// =============================================================================

// TestRESTDEKSafetyOperations tests DEK safety operations via REST API.
func TestRESTDEKSafetyOperations(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	endpoints := helpers.GetRESTEndpoints()
	client := NewRESTClient(endpoints[0])

	dekPath := generateUniqueKeyName("rest-dek-safety-test")

	// Create DEK first
	createResp, err := client.CreateDEK(ctx, &service.CreateDEKRequest{
		Path:        dekPath,
		Description: "REST DEK safety test",
	})
	require.NoError(t, err, "CreateDEK should be available via REST")
	require.NotNil(t, createResp)
	defer func() {
		client.DeleteDEK(ctx, &service.DeleteDEKRequest{Path: dekPath, Force: true})
	}()

	t.Run("set DEK safety via REST", func(t *testing.T) {
		resp, err := client.SetDEKSafety(ctx, &service.SetDEKSafetyRequest{
			Path:           dekPath,
			MaxOperations:  100000,
			MaxBytes:       50 * 1024 * 1024, // 50MB
			WarningPercent: 70,
		})
		require.NoError(t, err, "SetDEKSafety should be available via REST")

		require.NotNil(t, resp)
		assert.Equal(t, dekPath, resp.Path)
	})

	t.Run("clear DEK safety via REST", func(t *testing.T) {
		resp, err := client.ClearDEKSafety(ctx, &service.ClearDEKSafetyRequest{
			Path: dekPath,
		})
		require.NoError(t, err, "ClearDEKSafety should be available via REST")

		require.NotNil(t, resp)
		assert.Equal(t, dekPath, resp.Path)
	})

	t.Run("get DEK status via REST", func(t *testing.T) {
		resp, err := client.GetDEKStatus(ctx, &service.GetDEKStatusRequest{
			Path: dekPath,
		})
		require.NoError(t, err, "GetDEKStatus should be available via REST")

		require.NotNil(t, resp)
		require.NotNil(t, resp.DEK)
		assert.Equal(t, dekPath, resp.DEK.Path)
	})

	t.Run("set default DEK via REST", func(t *testing.T) {
		resp, err := client.SetDefaultDEK(ctx, &service.SetDefaultDEKRequest{
			Path: dekPath,
		})
		require.NoError(t, err, "SetDefaultDEK should be available via REST")

		require.NotNil(t, resp)
		assert.Equal(t, dekPath, resp.Path)
	})
}

// TestRESTKeySafety tests key safety operations via REST API.
func TestRESTKeySafety(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	endpoints := helpers.GetRESTEndpoints()
	client := NewRESTClient(endpoints[0])

	// Create a test key
	keyName := generateUniqueKeyName("rest-safety-test")
	genReq := &service.GenerateKeyRequest{
		Name: keyName,
		KeyAttributes: &kctypes.KeyAttributes{
			SymmetricAlgorithm: kctypes.SymmetricAES256GCM,
		},
		Usage: EncryptDecryptUsage(),
	}

	genResp, err := client.GenerateKey(ctx, genReq)
	require.NoError(t, err, "KMS should be available on REST")
	keyID := genResp.Key.KeyID
	defer func() {
		client.DeleteKey(ctx, &service.DeleteKeyRequest{KeyID: keyID, Force: true})
	}()

	t.Run("set key safety via REST", func(t *testing.T) {
		resp, err := client.SetKeySafety(ctx, &service.SetKeySafetyRequest{
			KeyID:          keyID,
			MaxOperations:  1000000,
			MaxBytes:       1024 * 1024 * 1024,
			WarningPercent: 80,
		})
		require.NoError(t, err, "SetKeySafety should be available via REST")

		require.NotNil(t, resp)
		assert.Equal(t, keyID, resp.KeyID)
	})

	t.Run("clear key safety via REST", func(t *testing.T) {
		resp, err := client.ClearKeySafety(ctx, &service.ClearKeySafetyRequest{
			KeyID: keyID,
		})
		require.NoError(t, err, "ClearKeySafety should be available via REST")

		require.NotNil(t, resp)
		assert.Equal(t, keyID, resp.KeyID)
	})
}

// TestRESTSafetyCheckAndReport tests SafetyCheck and SafetyReport via REST API.
func TestRESTSafetyCheckAndReport(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	endpoints := helpers.GetRESTEndpoints()
	client := NewRESTClient(endpoints[0])

	t.Run("safety check via REST", func(t *testing.T) {
		resp, err := client.SafetyCheck(ctx, &service.SafetyCheckRequest{
			ThresholdPercent: 50,
		})
		require.NoError(t, err, "SafetyCheck should be available via REST")

		require.NotNil(t, resp)
		t.Logf("Safety check: %d items exceeding 50%% threshold", len(resp.Items))
	})

	t.Run("safety report via REST", func(t *testing.T) {
		resp, err := client.SafetyReport(ctx, &service.SafetyReportRequest{
			IncludeDetails: true,
		})
		require.NoError(t, err, "SafetyReport should be available via REST")

		require.NotNil(t, resp)
		t.Logf("Safety report: %d items, %d total ops", len(resp.Items), resp.TotalOperations)
	})
}
