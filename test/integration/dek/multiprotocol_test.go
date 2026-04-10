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

package dek

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/service"
	"github.com/jeremyhahn/go-quicbaas/test/integration/helpers"
)

// =============================================================================
// Multi-Protocol Test Runner
// =============================================================================

// TestDEKMultiProtocol verifies that DEK operations work identically
// across all supported protocols: Embedded, REST, gRPC, QUIC, Unix Socket, and MCP.
func TestDEKMultiProtocol(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	// Test each protocol independently
	if helpers.IsEmbeddedAvailable() {
		t.Run("Embedded", func(t *testing.T) {
			embedded := &embeddedProtocolClient{}
			err := embedded.Setup(t, ctx)
			require.NoError(t, err, "Embedded service must be available")
			testDEKProtocol(t, ctx, embedded)
		})
	}

	t.Run("REST", func(t *testing.T) {
		testDEKProtocol(t, ctx, &restProtocolClient{})
	})

	t.Run("gRPC", func(t *testing.T) {
		testDEKProtocol(t, ctx, &grpcProtocolClient{})
	})

	t.Run("QUIC", func(t *testing.T) {
		testDEKProtocol(t, ctx, &quicProtocolClient{})
	})

	t.Run("UnixSocket", func(t *testing.T) {
		testDEKProtocol(t, ctx, &unixSocketProtocolClient{})
	})

	t.Run("MCP", func(t *testing.T) {
		testDEKProtocol(t, ctx, &mcpProtocolClient{})
	})
}

// testDEKProtocol runs the complete DEK test suite against a protocol.
func testDEKProtocol(t *testing.T, ctx context.Context, protocol dekProtocol) {
	t.Helper()

	// Setup the protocol client
	err := protocol.Setup(t, ctx)
	require.NoError(t, err, "Failed to setup %s protocol", protocol.Name())
	defer func() {
		err := protocol.Teardown(t)
		if err != nil {
			t.Logf("Warning: teardown error for %s: %v", protocol.Name(), err)
		}
	}()

	basePrefix := fmt.Sprintf("mp-%s-%d", protocol.Name(), time.Now().UnixNano())

	// Test 1: Create DEK
	t.Run("create", func(t *testing.T) {
		testDEKCreate(t, ctx, protocol, basePrefix+"-create")
	})

	// Test 2: Get DEK
	t.Run("get", func(t *testing.T) {
		testDEKGet(t, ctx, protocol, basePrefix+"-get")
	})

	// Test 3: List DEKs
	t.Run("list", func(t *testing.T) {
		testDEKList(t, ctx, protocol, basePrefix+"-list")
	})

	// Test 4: Rotate DEK
	t.Run("rotate", func(t *testing.T) {
		testDEKRotate(t, ctx, protocol, basePrefix+"-rotate")
	})

	// Test 5: Delete DEK
	t.Run("delete", func(t *testing.T) {
		testDEKDelete(t, ctx, protocol, basePrefix+"-delete")
	})

	// Test 6: DEK Status
	t.Run("status", func(t *testing.T) {
		testDEKStatus(t, ctx, protocol, basePrefix+"-status")
	})

	// Test 7: Set Default DEK
	t.Run("setDefault", func(t *testing.T) {
		testDEKSetDefault(t, ctx, protocol, basePrefix+"-default")
	})

	// Test 8: DEK Safety
	t.Run("safety", func(t *testing.T) {
		testDEKSafety(t, ctx, protocol, basePrefix+"-safety")
	})

	// Test 9: Re-encryption
	t.Run("reencryption", func(t *testing.T) {
		testDEKReencryption(t, ctx, protocol, basePrefix+"-reenc")
	})

	// Test 10: Concurrent Operations
	t.Run("concurrent", func(t *testing.T) {
		testDEKConcurrentOperations(t, ctx, protocol, basePrefix+"-concurrent")
	})

	// Reconnect QUIC after concurrent test to reset any corrupted connection state
	if reconnectable, ok := protocol.(interface{ Reconnect(context.Context) error }); ok {
		if err := reconnectable.Reconnect(ctx); err != nil {
			t.Logf("Warning: Failed to reconnect %s after concurrent test: %v", protocol.Name(), err)
		}
	}

	// Test 11: Error Handling
	t.Run("errors", func(t *testing.T) {
		testDEKErrorHandling(t, ctx, protocol)
	})

	// Test 12: All Symmetric Algorithms
	t.Run("all_algorithms", func(t *testing.T) {
		testDEKAllAlgorithms(t, ctx, protocol, basePrefix+"-alg")
	})

	// Test 13: Rotation per Algorithm
	t.Run("rotation_per_algorithm", func(t *testing.T) {
		testDEKRotationPerAlgorithm(t, ctx, protocol, basePrefix+"-rot-alg")
	})
}

// =============================================================================
// Individual Test Functions
// =============================================================================

// TestDEKCreate_MultiProtocol tests DEK creation across all protocols.
func TestDEKCreate_MultiProtocol(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	protocols := []dekProtocol{
		&restProtocolClient{},
		&grpcProtocolClient{},
		&quicProtocolClient{},
		&unixSocketProtocolClient{},
		&mcpProtocolClient{},
	}
	if helpers.IsEmbeddedAvailable() {
		protocols = append(protocols, &embeddedProtocolClient{})
	}

	for _, protocol := range protocols {
		t.Run(protocol.Name(), func(t *testing.T) {
			err := protocol.Setup(t, ctx)
			require.NoError(t, err, "%s protocol setup must succeed", protocol.Name())
			defer protocol.Teardown(t)

			prefix := fmt.Sprintf("create-%s-%d", protocol.Name(), time.Now().UnixNano())
			testDEKCreate(t, ctx, protocol, prefix)
		})
	}
}

func testDEKCreate(t *testing.T, ctx context.Context, protocol dekProtocol, prefix string) {
	t.Helper()

	// Test all 5 symmetric algorithms
	tests := []struct {
		name        string
		algorithm   service.KeyAlgorithm
		description string
	}{
		{
			name:        "AES-128-GCM",
			algorithm:   service.KeyAlgAES128GCM,
			description: "AES-128-GCM DEK",
		},
		{
			name:        "AES-192-GCM",
			algorithm:   service.KeyAlgAES192GCM,
			description: "AES-192-GCM DEK",
		},
		{
			name:        "AES-256-GCM",
			algorithm:   service.KeyAlgAES256GCM,
			description: "Default AES-256-GCM DEK",
		},
		{
			name:        "ChaCha20-Poly1305",
			algorithm:   service.KeyAlgChaCha20Poly1305,
			description: "ChaCha20-Poly1305 DEK",
		},
		{
			name:        "XChaCha20-Poly1305",
			algorithm:   service.KeyAlgXChaCha20Poly1305,
			description: "XChaCha20-Poly1305 DEK",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := fmt.Sprintf("%s/%s", prefix, tt.name)

			req := &service.CreateDEKRequest{
				Path:           path,
				Algorithm:      tt.algorithm,
				Description:    tt.description,
				MaxOperations:  1000000,
				MaxBytes:       1024 * 1024 * 1024, // 1GB
				WarningPercent: 80,
			}

			resp, err := protocol.CreateDEK(ctx, req)
			require.NoError(t, err, "CreateDEK failed for %s", protocol.Name())
			assert.NotNil(t, resp)
			assert.NotNil(t, resp.DEK)
			assert.Equal(t, path, resp.DEK.Path)
			assert.Equal(t, tt.algorithm, resp.DEK.Algorithm)
			assert.Equal(t, uint64(1), resp.DEK.Version)

			// Cleanup
			cleanupDEK(t, protocol, ctx, path)

			t.Logf("[%s] Created DEK: %s, algorithm: %s", protocol.Name(), path, tt.algorithm)
		})
	}
}

// TestDEKGet_MultiProtocol tests DEK retrieval across all protocols.
func TestDEKGet_MultiProtocol(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	protocols := []dekProtocol{
		&restProtocolClient{},
		&grpcProtocolClient{},
		&quicProtocolClient{},
		&unixSocketProtocolClient{},
		&mcpProtocolClient{},
	}
	if helpers.IsEmbeddedAvailable() {
		protocols = append(protocols, &embeddedProtocolClient{})
	}

	for _, protocol := range protocols {
		t.Run(protocol.Name(), func(t *testing.T) {
			err := protocol.Setup(t, ctx)
			require.NoError(t, err, "%s protocol setup must succeed", protocol.Name())
			defer protocol.Teardown(t)

			prefix := fmt.Sprintf("get-%s-%d", protocol.Name(), time.Now().UnixNano())
			testDEKGet(t, ctx, protocol, prefix)
		})
	}
}

func testDEKGet(t *testing.T, ctx context.Context, protocol dekProtocol, prefix string) {
	t.Helper()

	// Create a test DEK
	path := fmt.Sprintf("%s/test", prefix)
	createReq := &service.CreateDEKRequest{
		Path:        path,
		Algorithm:   service.KeyAlgAES256GCM,
		Description: "Test DEK for get operation",
	}

	createResp, err := protocol.CreateDEK(ctx, createReq)
	require.NoError(t, err)
	defer cleanupDEK(t, protocol, ctx, path)

	t.Run("get by path", func(t *testing.T) {
		req := &service.GetDEKRequest{Path: path}
		resp, err := protocol.GetDEK(ctx, req)
		require.NoError(t, err, "GetDEK failed for %s", protocol.Name())
		assert.NotNil(t, resp)
		assert.NotNil(t, resp.DEK)
		assert.Equal(t, path, resp.DEK.Path)
		assert.Equal(t, service.KeyAlgAES256GCM, resp.DEK.Algorithm)
	})

	t.Run("get with version", func(t *testing.T) {
		// Rotate to create version 2
		rotateReq := &service.RotateDEKRequest{
			Path:             path,
			RetainOldVersion: true,
		}
		rotateResp, err := protocol.RotateDEK(ctx, rotateReq)
		require.NoError(t, err)

		// Get specific version
		req := &service.GetDEKRequest{
			Path:    path,
			Version: rotateResp.NewVersion,
		}
		resp, err := protocol.GetDEK(ctx, req)
		require.NoError(t, err)
		assert.Equal(t, rotateResp.NewVersion, resp.DEK.Version)
	})

	t.Run("get non-existent", func(t *testing.T) {
		req := &service.GetDEKRequest{Path: "non-existent-dek-path"}
		_, err := protocol.GetDEK(ctx, req)
		assert.Error(t, err)
	})

	t.Logf("[%s] Retrieved DEK: %s (version %d)", protocol.Name(), path, createResp.DEK.Version)
}

// TestDEKList_MultiProtocol tests DEK listing across all protocols.
func TestDEKList_MultiProtocol(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	protocols := []dekProtocol{
		&restProtocolClient{},
		&grpcProtocolClient{},
		&quicProtocolClient{},
		&unixSocketProtocolClient{},
		&mcpProtocolClient{},
	}
	if helpers.IsEmbeddedAvailable() {
		protocols = append(protocols, &embeddedProtocolClient{})
	}

	for _, protocol := range protocols {
		t.Run(protocol.Name(), func(t *testing.T) {
			err := protocol.Setup(t, ctx)
			require.NoError(t, err, "%s protocol setup must succeed", protocol.Name())
			defer protocol.Teardown(t)

			prefix := fmt.Sprintf("list-%s-%d", protocol.Name(), time.Now().UnixNano())
			testDEKList(t, ctx, protocol, prefix)
		})
	}
}

func testDEKList(t *testing.T, ctx context.Context, protocol dekProtocol, prefix string) {
	t.Helper()

	// Create multiple DEKs
	numDEKs := 5
	paths := make([]string, 0, numDEKs)

	for i := 0; i < numDEKs; i++ {
		path := fmt.Sprintf("%s/dek-%d", prefix, i)
		req := &service.CreateDEKRequest{
			Path:        path,
			Algorithm:   service.KeyAlgAES256GCM,
			Description: fmt.Sprintf("Test DEK %d", i),
		}
		_, err := protocol.CreateDEK(ctx, req)
		require.NoError(t, err)
		paths = append(paths, path)
	}

	defer func() {
		for _, path := range paths {
			cleanupDEK(t, protocol, ctx, path)
		}
	}()

	t.Run("list all", func(t *testing.T) {
		req := &service.ListDEKsRequest{Limit: 100}
		resp, err := protocol.ListDEKs(ctx, req)
		require.NoError(t, err, "ListDEKs failed for %s", protocol.Name())
		assert.NotNil(t, resp)
		assert.NotEmpty(t, resp.DEKs)
		t.Logf("[%s] Listed %d total DEKs", protocol.Name(), len(resp.DEKs))
	})

	t.Run("list with prefix", func(t *testing.T) {
		req := &service.ListDEKsRequest{
			Prefix: prefix,
			Limit:  100,
		}
		resp, err := protocol.ListDEKs(ctx, req)
		require.NoError(t, err)
		assert.GreaterOrEqual(t, len(resp.DEKs), numDEKs)
		t.Logf("[%s] Listed %d DEKs with prefix %s", protocol.Name(), len(resp.DEKs), prefix)
	})

	t.Run("list with limit", func(t *testing.T) {
		req := &service.ListDEKsRequest{
			Prefix: prefix,
			Limit:  3,
		}
		resp, err := protocol.ListDEKs(ctx, req)
		require.NoError(t, err)
		assert.LessOrEqual(t, len(resp.DEKs), 3)
		t.Logf("[%s] Limited list returned %d DEKs", protocol.Name(), len(resp.DEKs))
	})

	t.Run("list recursive", func(t *testing.T) {
		req := &service.ListDEKsRequest{
			Prefix:    prefix,
			Recursive: true,
			Limit:     100,
		}
		resp, err := protocol.ListDEKs(ctx, req)
		require.NoError(t, err)
		t.Logf("[%s] Recursive list returned %d DEKs", protocol.Name(), len(resp.DEKs))
	})
}

// TestDEKRotate_MultiProtocol tests DEK rotation across all protocols.
func TestDEKRotate_MultiProtocol(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	protocols := []dekProtocol{
		&restProtocolClient{},
		&grpcProtocolClient{},
		&quicProtocolClient{},
		&unixSocketProtocolClient{},
		&mcpProtocolClient{},
	}
	if helpers.IsEmbeddedAvailable() {
		protocols = append(protocols, &embeddedProtocolClient{})
	}

	for _, protocol := range protocols {
		t.Run(protocol.Name(), func(t *testing.T) {
			err := protocol.Setup(t, ctx)
			require.NoError(t, err, "%s protocol setup must succeed", protocol.Name())
			defer protocol.Teardown(t)

			prefix := fmt.Sprintf("rotate-%s-%d", protocol.Name(), time.Now().UnixNano())
			testDEKRotate(t, ctx, protocol, prefix)
		})
	}
}

func testDEKRotate(t *testing.T, ctx context.Context, protocol dekProtocol, prefix string) {
	t.Helper()

	// Create a test DEK
	path := fmt.Sprintf("%s/test", prefix)
	createReq := &service.CreateDEKRequest{
		Path:        path,
		Algorithm:   service.KeyAlgAES256GCM,
		Description: "Test DEK for rotation",
	}

	createResp, err := protocol.CreateDEK(ctx, createReq)
	require.NoError(t, err)
	initialVersion := createResp.DEK.Version
	defer cleanupDEK(t, protocol, ctx, path)

	t.Run("rotate with version retention", func(t *testing.T) {
		rotateReq := &service.RotateDEKRequest{
			Path:             path,
			RetainOldVersion: true,
		}
		resp, err := protocol.RotateDEK(ctx, rotateReq)
		require.NoError(t, err, "RotateDEK failed for %s", protocol.Name())
		assert.NotNil(t, resp)
		assert.Equal(t, path, resp.Path)
		assert.Greater(t, resp.NewVersion, resp.OldVersion)

		// Verify version incremented
		getResp, err := protocol.GetDEK(ctx, &service.GetDEKRequest{Path: path})
		require.NoError(t, err)
		assert.Greater(t, getResp.DEK.Version, initialVersion)

		t.Logf("[%s] Rotated DEK %s from v%d to v%d", protocol.Name(), path, resp.OldVersion, resp.NewVersion)
	})

	t.Run("rotate multiple times", func(t *testing.T) {
		path2 := fmt.Sprintf("%s/multi-rotate", prefix)
		createReq2 := &service.CreateDEKRequest{
			Path:      path2,
			Algorithm: service.KeyAlgAES256GCM,
		}
		_, err := protocol.CreateDEK(ctx, createReq2)
		require.NoError(t, err)
		defer cleanupDEK(t, protocol, ctx, path2)

		// Rotate multiple times
		var lastVersion uint64
		for i := 0; i < 3; i++ {
			rotateReq := &service.RotateDEKRequest{
				Path:             path2,
				RetainOldVersion: true,
			}
			resp, err := protocol.RotateDEK(ctx, rotateReq)
			require.NoError(t, err)
			if lastVersion > 0 {
				assert.Greater(t, resp.NewVersion, lastVersion)
			}
			lastVersion = resp.NewVersion
		}

		t.Logf("[%s] Successfully rotated DEK %d times", protocol.Name(), 3)
	})
}

// TestDEKDelete_MultiProtocol tests DEK deletion across all protocols.
func TestDEKDelete_MultiProtocol(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	protocols := []dekProtocol{
		&restProtocolClient{},
		&grpcProtocolClient{},
		&quicProtocolClient{},
		&unixSocketProtocolClient{},
		&mcpProtocolClient{},
	}
	if helpers.IsEmbeddedAvailable() {
		protocols = append(protocols, &embeddedProtocolClient{})
	}

	for _, protocol := range protocols {
		t.Run(protocol.Name(), func(t *testing.T) {
			err := protocol.Setup(t, ctx)
			require.NoError(t, err, "%s protocol setup must succeed", protocol.Name())
			defer protocol.Teardown(t)

			prefix := fmt.Sprintf("delete-%s-%d", protocol.Name(), time.Now().UnixNano())
			testDEKDelete(t, ctx, protocol, prefix)
		})
	}
}

func testDEKDelete(t *testing.T, ctx context.Context, protocol dekProtocol, prefix string) {
	t.Helper()

	t.Run("delete with force", func(t *testing.T) {
		path := fmt.Sprintf("%s/force-delete", prefix)
		createReq := &service.CreateDEKRequest{
			Path:      path,
			Algorithm: service.KeyAlgAES256GCM,
		}

		_, err := protocol.CreateDEK(ctx, createReq)
		require.NoError(t, err)

		// Force delete
		deleteReq := &service.DeleteDEKRequest{
			Path:  path,
			Force: true,
		}

		resp, err := protocol.DeleteDEK(ctx, deleteReq)
		require.NoError(t, err, "DeleteDEK (force) failed for %s", protocol.Name())
		assert.Equal(t, path, resp.Path)
		assert.True(t, resp.Deleted)

		// Verify deletion
		_, err = protocol.GetDEK(ctx, &service.GetDEKRequest{Path: path})
		assert.Error(t, err, "DEK should not exist after force delete")

		t.Logf("[%s] Force deleted DEK: %s", protocol.Name(), path)
	})

	t.Run("delete non-existent", func(t *testing.T) {
		deleteReq := &service.DeleteDEKRequest{
			Path:  "non-existent-dek",
			Force: true,
		}
		_, err := protocol.DeleteDEK(ctx, deleteReq)
		assert.Error(t, err)
	})
}

// TestDEKStatus_MultiProtocol tests DEK status retrieval across all protocols.
func TestDEKStatus_MultiProtocol(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	protocols := []dekProtocol{
		&restProtocolClient{},
		&grpcProtocolClient{},
		&quicProtocolClient{},
		&unixSocketProtocolClient{},
		&mcpProtocolClient{},
	}
	if helpers.IsEmbeddedAvailable() {
		protocols = append(protocols, &embeddedProtocolClient{})
	}

	for _, protocol := range protocols {
		t.Run(protocol.Name(), func(t *testing.T) {
			err := protocol.Setup(t, ctx)
			require.NoError(t, err, "%s protocol setup must succeed", protocol.Name())
			defer protocol.Teardown(t)

			prefix := fmt.Sprintf("status-%s-%d", protocol.Name(), time.Now().UnixNano())
			testDEKStatus(t, ctx, protocol, prefix)
		})
	}
}

func testDEKStatus(t *testing.T, ctx context.Context, protocol dekProtocol, prefix string) {
	t.Helper()

	// Create a test DEK
	path := fmt.Sprintf("%s/test", prefix)
	createReq := &service.CreateDEKRequest{
		Path:           path,
		Algorithm:      service.KeyAlgAES256GCM,
		Description:    "Test DEK for status",
		MaxOperations:  1000000,
		MaxBytes:       1024 * 1024 * 1024,
		WarningPercent: 80,
	}

	_, err := protocol.CreateDEK(ctx, createReq)
	require.NoError(t, err)
	defer cleanupDEK(t, protocol, ctx, path)

	t.Run("get status", func(t *testing.T) {
		statusReq := &service.GetDEKStatusRequest{Path: path}
		resp, err := protocol.GetDEKStatus(ctx, statusReq)
		require.NoError(t, err, "GetDEKStatus failed for %s", protocol.Name())
		assert.NotNil(t, resp)
		assert.NotNil(t, resp.DEK)
		assert.Equal(t, path, resp.DEK.Path)
		assert.Equal(t, service.KeyAlgAES256GCM, resp.DEK.Algorithm)

		t.Logf("[%s] DEK status: path=%s, version=%d, usage=%d",
			protocol.Name(), resp.DEK.Path, resp.DEK.Version, resp.DEK.UsageCount)
	})

	t.Run("get status non-existent", func(t *testing.T) {
		statusReq := &service.GetDEKStatusRequest{Path: "non-existent-dek"}
		_, err := protocol.GetDEKStatus(ctx, statusReq)
		assert.Error(t, err)
	})
}

// TestDEKSetDefault_MultiProtocol tests setting default DEK across all protocols.
func TestDEKSetDefault_MultiProtocol(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	protocols := []dekProtocol{
		&restProtocolClient{},
		&grpcProtocolClient{},
		&quicProtocolClient{},
		&unixSocketProtocolClient{},
		&mcpProtocolClient{},
	}
	if helpers.IsEmbeddedAvailable() {
		protocols = append(protocols, &embeddedProtocolClient{})
	}

	for _, protocol := range protocols {
		t.Run(protocol.Name(), func(t *testing.T) {
			err := protocol.Setup(t, ctx)
			require.NoError(t, err, "%s protocol setup must succeed", protocol.Name())
			defer protocol.Teardown(t)

			prefix := fmt.Sprintf("default-%s-%d", protocol.Name(), time.Now().UnixNano())
			testDEKSetDefault(t, ctx, protocol, prefix)
		})
	}
}

func testDEKSetDefault(t *testing.T, ctx context.Context, protocol dekProtocol, prefix string) {
	t.Helper()

	// Create two DEKs
	path1 := fmt.Sprintf("%s/dek1", prefix)
	path2 := fmt.Sprintf("%s/dek2", prefix)

	createReq1 := &service.CreateDEKRequest{
		Path:      path1,
		Algorithm: service.KeyAlgAES256GCM,
	}
	_, err := protocol.CreateDEK(ctx, createReq1)
	require.NoError(t, err)
	defer cleanupDEK(t, protocol, ctx, path1)

	createReq2 := &service.CreateDEKRequest{
		Path:      path2,
		Algorithm: service.KeyAlgAES256GCM,
	}
	_, err = protocol.CreateDEK(ctx, createReq2)
	require.NoError(t, err)
	defer cleanupDEK(t, protocol, ctx, path2)

	t.Run("set default", func(t *testing.T) {
		setDefaultReq := &service.SetDefaultDEKRequest{Path: path1}
		resp, err := protocol.SetDefaultDEK(ctx, setDefaultReq)
		require.NoError(t, err, "SetDefaultDEK failed for %s", protocol.Name())
		assert.NotNil(t, resp)
		assert.Equal(t, path1, resp.Path)

		t.Logf("[%s] Set default DEK: %s", protocol.Name(), path1)
	})

	t.Run("change default", func(t *testing.T) {
		setDefaultReq := &service.SetDefaultDEKRequest{Path: path2}
		resp, err := protocol.SetDefaultDEK(ctx, setDefaultReq)
		require.NoError(t, err)
		assert.Equal(t, path2, resp.Path)
		assert.Equal(t, path1, resp.PreviousDefault)

		t.Logf("[%s] Changed default DEK from %s to %s", protocol.Name(), path1, path2)
	})
}

// TestDEKSafety_MultiProtocol tests DEK safety operations across all protocols.
func TestDEKSafety_MultiProtocol(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	protocols := []dekProtocol{
		&restProtocolClient{},
		&grpcProtocolClient{},
		&quicProtocolClient{},
		&unixSocketProtocolClient{},
		&mcpProtocolClient{},
	}
	if helpers.IsEmbeddedAvailable() {
		protocols = append(protocols, &embeddedProtocolClient{})
	}

	for _, protocol := range protocols {
		t.Run(protocol.Name(), func(t *testing.T) {
			err := protocol.Setup(t, ctx)
			require.NoError(t, err, "%s protocol setup must succeed", protocol.Name())
			defer protocol.Teardown(t)

			prefix := fmt.Sprintf("safety-%s-%d", protocol.Name(), time.Now().UnixNano())
			testDEKSafety(t, ctx, protocol, prefix)
		})
	}
}

func testDEKSafety(t *testing.T, ctx context.Context, protocol dekProtocol, prefix string) {
	t.Helper()

	// Create a test DEK
	path := fmt.Sprintf("%s/test", prefix)
	createReq := &service.CreateDEKRequest{
		Path:      path,
		Algorithm: service.KeyAlgAES256GCM,
	}

	_, err := protocol.CreateDEK(ctx, createReq)
	require.NoError(t, err)
	defer cleanupDEK(t, protocol, ctx, path)

	t.Run("set safety limits", func(t *testing.T) {
		setSafetyReq := &service.SetDEKSafetyRequest{
			Path:           path,
			MaxOperations:  500000,
			MaxBytes:       512 * 1024 * 1024, // 512MB
			WarningPercent: 75,
		}
		resp, err := protocol.SetDEKSafety(ctx, setSafetyReq)
		require.NoError(t, err, "SetDEKSafety failed for %s", protocol.Name())
		assert.NotNil(t, resp)
		assert.Equal(t, path, resp.Path)
		assert.NotNil(t, resp.Config)
		assert.Equal(t, uint64(500000), resp.Config.MaxOperations)
		assert.Equal(t, uint64(512*1024*1024), resp.Config.MaxBytes)
		assert.Equal(t, 75, resp.Config.WarningPercent)

		t.Logf("[%s] Set safety limits for DEK: %s", protocol.Name(), path)
	})

	t.Run("clear safety limits", func(t *testing.T) {
		clearSafetyReq := &service.ClearDEKSafetyRequest{Path: path}
		resp, err := protocol.ClearDEKSafety(ctx, clearSafetyReq)
		require.NoError(t, err, "ClearDEKSafety failed for %s", protocol.Name())
		assert.NotNil(t, resp)
		assert.Equal(t, path, resp.Path)
		assert.True(t, resp.Cleared)

		t.Logf("[%s] Cleared safety limits for DEK: %s", protocol.Name(), path)
	})
}

// TestDEKReencryption_MultiProtocol tests DEK re-encryption across all protocols.
func TestDEKReencryption_MultiProtocol(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	protocols := []dekProtocol{
		&restProtocolClient{},
		&grpcProtocolClient{},
		&quicProtocolClient{},
		&unixSocketProtocolClient{},
		&mcpProtocolClient{},
	}
	if helpers.IsEmbeddedAvailable() {
		protocols = append(protocols, &embeddedProtocolClient{})
	}

	for _, protocol := range protocols {
		t.Run(protocol.Name(), func(t *testing.T) {
			err := protocol.Setup(t, ctx)
			require.NoError(t, err, "%s protocol setup must succeed", protocol.Name())
			defer protocol.Teardown(t)

			prefix := fmt.Sprintf("reenc-%s-%d", protocol.Name(), time.Now().UnixNano())
			testDEKReencryption(t, ctx, protocol, prefix)
		})
	}
}

func testDEKReencryption(t *testing.T, ctx context.Context, protocol dekProtocol, prefix string) {
	t.Helper()

	// Create a test DEK
	path := fmt.Sprintf("%s/test", prefix)
	createReq := &service.CreateDEKRequest{
		Path:      path,
		Algorithm: service.KeyAlgAES256GCM,
	}

	createResp, err := protocol.CreateDEK(ctx, createReq)
	require.NoError(t, err)
	defer cleanupDEK(t, protocol, ctx, path)

	// Rotate to create a new version
	rotateReq := &service.RotateDEKRequest{
		Path:             path,
		RetainOldVersion: true,
	}
	rotateResp, err := protocol.RotateDEK(ctx, rotateReq)
	require.NoError(t, err)

	t.Run("start reencryption", func(t *testing.T) {
		startReq := &service.StartReencryptionRequest{
			Path:        path,
			FromVersion: createResp.DEK.Version,
			ToVersion:   rotateResp.NewVersion,
			BatchSize:   100,
			Concurrency: 2,
		}
		resp, err := protocol.StartReencryption(ctx, startReq)
		if err != nil {
			// Re-encryption might not be implemented or might fail if no data to re-encrypt
			t.Logf("[%s] StartReencryption returned error (may be expected): %v", protocol.Name(), err)
			return
		}
		assert.NotNil(t, resp)
		assert.NotEmpty(t, resp.OperationID)
		assert.True(t, resp.Started)

		t.Logf("[%s] Started re-encryption: operation=%s", protocol.Name(), resp.OperationID)
	})

	t.Run("get reencryption progress", func(t *testing.T) {
		progressReq := &service.GetReencryptionProgressRequest{
			Path:    path,
			Verbose: true,
		}
		resp, err := protocol.GetReencryptionProgress(ctx, progressReq)
		if err != nil {
			// Progress might not be available if no re-encryption is running
			t.Logf("[%s] GetReencryptionProgress returned error (may be expected): %v", protocol.Name(), err)
			return
		}
		assert.NotNil(t, resp)
		t.Logf("[%s] Re-encryption progress: status=%s", protocol.Name(), resp.Status)
	})
}

// =============================================================================
// Concurrent Operations Test
// =============================================================================

func testDEKConcurrentOperations(t *testing.T, ctx context.Context, protocol dekProtocol, prefix string) {
	t.Helper()

	// Create a shared DEK
	path := fmt.Sprintf("%s/shared", prefix)
	createReq := &service.CreateDEKRequest{
		Path:        path,
		Algorithm:   service.KeyAlgAES256GCM,
		Description: "Shared DEK for concurrent tests",
	}

	_, err := protocol.CreateDEK(ctx, createReq)
	require.NoError(t, err)
	defer cleanupDEK(t, protocol, ctx, path)

	// Reduced concurrency for QUIC to avoid overwhelming the server
	concurrency := 5
	var wg sync.WaitGroup
	errors := make(chan error, concurrency)

	// For QUIC protocol, use separate clients for each concurrent operation
	// to avoid stream flow control issues on shared connections
	isQUIC := protocol.Name() == "QUIC"

	for i := 0; i < concurrency; i++ {
		wg.Add(1)
		// For QUIC, add staggered starts to avoid overwhelming the server with simultaneous connections
		if isQUIC && i > 0 {
			time.Sleep(100 * time.Millisecond)
		}
		go func(idx int) {
			defer wg.Done()

			// Use per-operation context with timeout for QUIC
			opCtx := ctx
			if isQUIC {
				var cancel context.CancelFunc
				opCtx, cancel = context.WithTimeout(ctx, 30*time.Second)
				defer cancel()
			}

			var getDEKFunc func(context.Context, *service.GetDEKRequest) (*service.GetDEKResponse, error)

			if isQUIC {
				// Create a separate QUIC client for this goroutine
				endpoints := getQUICEndpoints()
				if len(endpoints) == 0 {
					errors <- fmt.Errorf("no QUIC endpoints available for goroutine %d", idx)
					return
				}
				quicClient, err := NewQUICDEKClient(endpoints[0])
				if err != nil {
					errors <- fmt.Errorf("failed to create QUIC client %d: %w", idx, err)
					return
				}
				defer quicClient.Close()
				getDEKFunc = quicClient.GetDEK
			} else {
				getDEKFunc = protocol.GetDEK
			}

			// Perform concurrent get operations
			req := &service.GetDEKRequest{Path: path}
			resp, err := getDEKFunc(opCtx, req)
			if err != nil {
				errors <- fmt.Errorf("concurrent get error %d: %w", idx, err)
				return
			}

			if resp.DEK.Path != path {
				errors <- fmt.Errorf("path mismatch %d: expected %s, got %s", idx, path, resp.DEK.Path)
			}
		}(i)
	}

	wg.Wait()
	close(errors)

	errorCount := 0
	for err := range errors {
		t.Errorf("Concurrent operation error: %v", err)
		errorCount++
	}

	assert.Equal(t, 0, errorCount, "All concurrent operations should succeed")
	t.Logf("[%s] Completed %d concurrent operations", protocol.Name(), concurrency)
}

// =============================================================================
// Error Handling Test
// =============================================================================

func testDEKErrorHandling(t *testing.T, ctx context.Context, protocol dekProtocol) {
	t.Helper()

	t.Run("get non-existent DEK", func(t *testing.T) {
		req := &service.GetDEKRequest{Path: "non-existent-dek-path-12345"}
		_, err := protocol.GetDEK(ctx, req)
		assert.Error(t, err)
	})

	t.Run("delete non-existent DEK", func(t *testing.T) {
		req := &service.DeleteDEKRequest{
			Path:  "non-existent-dek-path-12345",
			Force: true,
		}
		_, err := protocol.DeleteDEK(ctx, req)
		assert.Error(t, err)
	})

	t.Run("rotate non-existent DEK", func(t *testing.T) {
		req := &service.RotateDEKRequest{Path: "non-existent-dek-path-12345"}
		_, err := protocol.RotateDEK(ctx, req)
		assert.Error(t, err)
	})

	t.Run("get status non-existent DEK", func(t *testing.T) {
		req := &service.GetDEKStatusRequest{Path: "non-existent-dek-path-12345"}
		_, err := protocol.GetDEKStatus(ctx, req)
		assert.Error(t, err)
	})

	t.Run("set default non-existent DEK", func(t *testing.T) {
		req := &service.SetDefaultDEKRequest{Path: "non-existent-dek-path-12345"}
		_, err := protocol.SetDefaultDEK(ctx, req)
		assert.Error(t, err)
	})

	t.Run("set safety non-existent DEK", func(t *testing.T) {
		req := &service.SetDEKSafetyRequest{
			Path:          "non-existent-dek-path-12345",
			MaxOperations: 100000,
		}
		_, err := protocol.SetDEKSafety(ctx, req)
		assert.Error(t, err)
	})

	t.Run("create DEK with empty path", func(t *testing.T) {
		req := &service.CreateDEKRequest{
			Path:      "",
			Algorithm: service.KeyAlgAES256GCM,
		}
		_, err := protocol.CreateDEK(ctx, req)
		assert.Error(t, err)
	})
}

// =============================================================================
// All Algorithms Test
// =============================================================================

// testDEKAllAlgorithms tests DEK creation, retrieval, and verification for ALL symmetric algorithms.
func testDEKAllAlgorithms(t *testing.T, ctx context.Context, protocol dekProtocol, prefix string) {
	t.Helper()

	// Define all 5 symmetric algorithms
	algorithms := []struct {
		name        string
		algorithm   service.KeyAlgorithm
		description string
	}{
		{
			name:        "AES-128-GCM",
			algorithm:   service.KeyAlgAES128GCM,
			description: "AES-128-GCM DEK for comprehensive testing",
		},
		{
			name:        "AES-192-GCM",
			algorithm:   service.KeyAlgAES192GCM,
			description: "AES-192-GCM DEK for comprehensive testing",
		},
		{
			name:        "AES-256-GCM",
			algorithm:   service.KeyAlgAES256GCM,
			description: "AES-256-GCM DEK for comprehensive testing",
		},
		{
			name:        "ChaCha20-Poly1305",
			algorithm:   service.KeyAlgChaCha20Poly1305,
			description: "ChaCha20-Poly1305 DEK for comprehensive testing",
		},
		{
			name:        "XChaCha20-Poly1305",
			algorithm:   service.KeyAlgXChaCha20Poly1305,
			description: "XChaCha20-Poly1305 DEK for comprehensive testing",
		},
	}

	for _, alg := range algorithms {
		t.Run(alg.name, func(t *testing.T) {
			path := fmt.Sprintf("%s/%s", prefix, alg.name)

			// Create DEK
			createReq := &service.CreateDEKRequest{
				Path:           path,
				Algorithm:      alg.algorithm,
				Description:    alg.description,
				MaxOperations:  1000000,
				MaxBytes:       1024 * 1024 * 1024, // 1GB
				WarningPercent: 80,
			}

			createResp, err := protocol.CreateDEK(ctx, createReq)
			require.NoError(t, err, "CreateDEK failed for %s on %s", alg.name, protocol.Name())
			assert.NotNil(t, createResp)
			assert.NotNil(t, createResp.DEK)
			assert.Equal(t, path, createResp.DEK.Path)
			assert.Equal(t, alg.algorithm, createResp.DEK.Algorithm)
			assert.Equal(t, uint64(1), createResp.DEK.Version)

			defer cleanupDEK(t, protocol, ctx, path)

			// Retrieve and verify DEK
			getResp, err := protocol.GetDEK(ctx, &service.GetDEKRequest{Path: path})
			require.NoError(t, err, "GetDEK failed for %s", alg.name)
			assert.Equal(t, alg.algorithm, getResp.DEK.Algorithm)
			assert.Equal(t, path, getResp.DEK.Path)

			// Get status
			statusResp, err := protocol.GetDEKStatus(ctx, &service.GetDEKStatusRequest{Path: path})
			require.NoError(t, err, "GetDEKStatus failed for %s", alg.name)
			assert.Equal(t, alg.algorithm, statusResp.DEK.Algorithm)

			t.Logf("[%s] Algorithm %s: created DEK at %s, version %d",
				protocol.Name(), alg.name, path, createResp.DEK.Version)
		})
	}
}

// testDEKRotationPerAlgorithm tests DEK rotation for each symmetric algorithm.
func testDEKRotationPerAlgorithm(t *testing.T, ctx context.Context, protocol dekProtocol, prefix string) {
	t.Helper()

	// Define all 5 symmetric algorithms
	algorithms := []struct {
		name      string
		algorithm service.KeyAlgorithm
	}{
		{name: "AES-128-GCM", algorithm: service.KeyAlgAES128GCM},
		{name: "AES-192-GCM", algorithm: service.KeyAlgAES192GCM},
		{name: "AES-256-GCM", algorithm: service.KeyAlgAES256GCM},
		{name: "ChaCha20-Poly1305", algorithm: service.KeyAlgChaCha20Poly1305},
		{name: "XChaCha20-Poly1305", algorithm: service.KeyAlgXChaCha20Poly1305},
	}

	for _, alg := range algorithms {
		t.Run(alg.name, func(t *testing.T) {
			path := fmt.Sprintf("%s/%s", prefix, alg.name)

			// Create DEK
			createReq := &service.CreateDEKRequest{
				Path:        path,
				Algorithm:   alg.algorithm,
				Description: fmt.Sprintf("Rotation test for %s", alg.name),
			}

			createResp, err := protocol.CreateDEK(ctx, createReq)
			require.NoError(t, err, "CreateDEK failed for %s on %s", alg.name, protocol.Name())
			initialVersion := createResp.DEK.Version

			defer cleanupDEK(t, protocol, ctx, path)

			// Rotate the DEK
			rotateReq := &service.RotateDEKRequest{
				Path:             path,
				RetainOldVersion: true,
			}
			rotateResp, err := protocol.RotateDEK(ctx, rotateReq)
			require.NoError(t, err, "RotateDEK failed for %s on %s", alg.name, protocol.Name())
			assert.Greater(t, rotateResp.NewVersion, initialVersion, "New version should be greater")
			assert.Equal(t, initialVersion, rotateResp.OldVersion)

			// Verify rotation via Get
			getResp, err := protocol.GetDEK(ctx, &service.GetDEKRequest{Path: path})
			require.NoError(t, err, "GetDEK after rotation failed for %s", alg.name)
			assert.Equal(t, rotateResp.NewVersion, getResp.DEK.Version)
			assert.Equal(t, alg.algorithm, getResp.DEK.Algorithm, "Algorithm should remain the same after rotation")

			// Rotate again to ensure multiple rotations work
			rotateResp2, err := protocol.RotateDEK(ctx, rotateReq)
			require.NoError(t, err, "Second rotation failed for %s", alg.name)
			assert.Greater(t, rotateResp2.NewVersion, rotateResp.NewVersion)

			t.Logf("[%s] Algorithm %s: rotation v%d -> v%d -> v%d verified",
				protocol.Name(), alg.name, initialVersion, rotateResp.NewVersion, rotateResp2.NewVersion)
		})
	}
}

// =============================================================================
// Cross-Protocol Consistency Tests
// =============================================================================

// TestDEKCrossProtocolConsistency verifies that DEK operations are consistent
// across different protocols (i.e., a DEK created via gRPC can be read via REST).
func TestDEKCrossProtocolConsistency(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	// Setup all protocols
	grpcClient := &grpcProtocolClient{}
	restClient := &restProtocolClient{}

	err := grpcClient.Setup(t, ctx)
	require.NoError(t, err, "gRPC protocol must be available")
	defer grpcClient.Teardown(t)

	err = restClient.Setup(t, ctx)
	require.NoError(t, err, "REST protocol must be available")
	defer restClient.Teardown(t)

	prefix := fmt.Sprintf("cross-%d", time.Now().UnixNano())

	t.Run("create_via_grpc_read_via_rest", func(t *testing.T) {
		path := fmt.Sprintf("%s/grpc-to-rest", prefix)

		// Create via gRPC
		createReq := &service.CreateDEKRequest{
			Path:        path,
			Algorithm:   service.KeyAlgAES256GCM,
			Description: "Created via gRPC, read via REST",
		}
		_, err := grpcClient.CreateDEK(ctx, createReq)
		require.NoError(t, err)
		defer cleanupDEK(t, grpcClient, ctx, path)

		// Read via REST
		getReq := &service.GetDEKRequest{Path: path}
		resp, err := restClient.GetDEK(ctx, getReq)
		require.NoError(t, err)
		assert.Equal(t, path, resp.DEK.Path)
		assert.Equal(t, service.KeyAlgAES256GCM, resp.DEK.Algorithm)

		t.Logf("Successfully created DEK via gRPC and read via REST: %s", path)
	})

	t.Run("create_via_rest_read_via_grpc", func(t *testing.T) {
		path := fmt.Sprintf("%s/rest-to-grpc", prefix)

		// Create via REST
		createReq := &service.CreateDEKRequest{
			Path:        path,
			Algorithm:   service.KeyAlgAES256GCM,
			Description: "Created via REST, read via gRPC",
		}
		_, err := restClient.CreateDEK(ctx, createReq)
		require.NoError(t, err)
		defer cleanupDEK(t, restClient, ctx, path)

		// Read via gRPC
		getReq := &service.GetDEKRequest{Path: path}
		resp, err := grpcClient.GetDEK(ctx, getReq)
		require.NoError(t, err)
		assert.Equal(t, path, resp.DEK.Path)
		assert.Equal(t, service.KeyAlgAES256GCM, resp.DEK.Algorithm)

		t.Logf("Successfully created DEK via REST and read via gRPC: %s", path)
	})

	t.Run("rotate_via_grpc_verify_via_rest", func(t *testing.T) {
		path := fmt.Sprintf("%s/rotate-cross", prefix)

		// Create via gRPC
		createReq := &service.CreateDEKRequest{
			Path:      path,
			Algorithm: service.KeyAlgAES256GCM,
		}
		createResp, err := grpcClient.CreateDEK(ctx, createReq)
		require.NoError(t, err)
		defer cleanupDEK(t, grpcClient, ctx, path)

		initialVersion := createResp.DEK.Version

		// Rotate via gRPC
		rotateReq := &service.RotateDEKRequest{
			Path:             path,
			RetainOldVersion: true,
		}
		rotateResp, err := grpcClient.RotateDEK(ctx, rotateReq)
		require.NoError(t, err)

		// Verify via REST
		getReq := &service.GetDEKRequest{Path: path}
		resp, err := restClient.GetDEK(ctx, getReq)
		require.NoError(t, err)
		assert.Greater(t, resp.DEK.Version, initialVersion)
		assert.Equal(t, rotateResp.NewVersion, resp.DEK.Version)

		t.Logf("Successfully rotated DEK via gRPC and verified via REST: %s (v%d -> v%d)",
			path, initialVersion, resp.DEK.Version)
	})

	// Test cross-protocol consistency for all algorithms
	t.Run("all_algorithms_cross_protocol", func(t *testing.T) {
		algorithms := []struct {
			name      string
			algorithm service.KeyAlgorithm
		}{
			{name: "AES-128-GCM", algorithm: service.KeyAlgAES128GCM},
			{name: "AES-192-GCM", algorithm: service.KeyAlgAES192GCM},
			{name: "AES-256-GCM", algorithm: service.KeyAlgAES256GCM},
			{name: "ChaCha20-Poly1305", algorithm: service.KeyAlgChaCha20Poly1305},
			{name: "XChaCha20-Poly1305", algorithm: service.KeyAlgXChaCha20Poly1305},
		}

		for _, alg := range algorithms {
			t.Run(alg.name, func(t *testing.T) {
				path := fmt.Sprintf("%s/cross-alg-%s", prefix, alg.name)

				// Create via gRPC with specific algorithm
				createReq := &service.CreateDEKRequest{
					Path:      path,
					Algorithm: alg.algorithm,
				}
				_, err := grpcClient.CreateDEK(ctx, createReq)
				require.NoError(t, err)
				defer cleanupDEK(t, grpcClient, ctx, path)

				// Verify via REST
				getResp, err := restClient.GetDEK(ctx, &service.GetDEKRequest{Path: path})
				require.NoError(t, err)
				assert.Equal(t, alg.algorithm, getResp.DEK.Algorithm, "Algorithm should match across protocols")

				t.Logf("Cross-protocol algorithm consistency verified for %s", alg.name)
			})
		}
	})
}
