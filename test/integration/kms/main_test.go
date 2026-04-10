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
	"crypto/tls"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/jeremyhahn/go-quicbaas/test/integration/helpers"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	kctypes "github.com/jeremyhahn/go-xkms/pkg/types"

	grpcclient "github.com/jeremyhahn/go-quicbaas/pkg/api/grpc"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
)

// TestMain runs before all tests in this package.
func TestMain(m *testing.M) {
	// Boot embedded server for multi-protocol tests
	_, srv, err := helpers.SetupEmbeddedServer()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Warning: Failed to start embedded server: %v\n", err)
	}

	code := m.Run()

	// Cleanup
	if srv != nil {
		helpers.TeardownEmbeddedServer(srv)
	}
	os.Exit(code)
}

// TestKMSAvailability tests that KMS service is available on all protocols.
func TestKMSAvailability(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	t.Run("gRPC availability", func(t *testing.T) {
		endpoints := getGRPCEndpoints()
		for _, endpoint := range endpoints {
			t.Run(endpoint, func(t *testing.T) {
				client, err := grpcclient.NewClient(endpoint, grpcclient.WithTLSConfig(&tls.Config{InsecureSkipVerify: true, MinVersion: tls.VersionTLS13}))
				require.NoError(t, err, "gRPC service must be available at %s", endpoint)
				defer client.Close()

				requireKMSAvailable(t, client, ctx)

				// Try a simple list operation
				req := &service.ListKeysRequest{
					Limit: 1,
				}
				resp, err := client.KMSListKeys(ctx, req)
				require.NoError(t, err)
				assert.NotNil(t, resp)
			})
		}
	})

	t.Run("REST availability", func(t *testing.T) {
		endpoints := helpers.GetRESTEndpoints()
		for _, endpoint := range endpoints {
			t.Run(endpoint, func(t *testing.T) {
				client := NewRESTClient(endpoint)

				req := &service.ListKeysRequest{
					Limit: 1,
				}
				resp, err := client.ListKeys(ctx, req)
				require.NoError(t, err, "KMS must be available on REST at %s", endpoint)
				assert.NotNil(t, resp)
			})
		}
	})
}

// TestKMSHealthCheck verifies KMS service health across protocols.
func TestKMSHealthCheck(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	t.Run("gRPC health check", func(t *testing.T) {
		endpoints := getGRPCEndpoints()
		client, err := grpcclient.NewClient(endpoints[0], grpcclient.WithTLSConfig(&tls.Config{InsecureSkipVerify: true, MinVersion: tls.VersionTLS13}))
		require.NoError(t, err, "gRPC service must be available")
		defer client.Close()

		requireKMSAvailable(t, client, ctx)

		// List keys should work
		req := &service.ListKeysRequest{Limit: 1}
		_, err = client.KMSListKeys(ctx, req)
		assert.NoError(t, err)
	})
}

// TestKMSKeyLifecycle tests complete key lifecycle across all protocols.
func TestKMSKeyLifecycle(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	// Test across multiple protocols
	protocols := []struct {
		name     string
		testFunc func(t *testing.T, ctx context.Context)
	}{
		{"gRPC", testGRPCKeyLifecycle},
		{"REST", testRESTKeyLifecycle},
	}

	for _, p := range protocols {
		t.Run(p.name, func(t *testing.T) {
			p.testFunc(t, ctx)
		})
	}
}

// testGRPCKeyLifecycle tests key lifecycle via gRPC.
func testGRPCKeyLifecycle(t *testing.T, ctx context.Context) {
	endpoints := getGRPCEndpoints()
	client, err := grpcclient.NewClient(endpoints[0], grpcclient.WithTLSConfig(&tls.Config{InsecureSkipVerify: true, MinVersion: tls.VersionTLS13}))
	require.NoError(t, err, "gRPC service must be available")
	defer client.Close()

	requireKMSAvailable(t, client, ctx)

	keyName := generateUniqueKeyName("lifecycle-test")

	// 1. Create key
	genReq := &service.GenerateKeyRequest{
		Name:        keyName,
		Description: "Integration test key lifecycle",
		KeyAttributes: &kctypes.KeyAttributes{
			SymmetricAlgorithm: kctypes.SymmetricAES256GCM,
		},
		Usage: []service.KeyUsage{service.KeyUsageEncrypt, service.KeyUsageDecrypt},
		Tags: map[string]string{
			"test":    "lifecycle",
			"created": time.Now().Format(time.RFC3339),
		},
	}

	genResp, err := client.KMSGenerateKey(ctx, genReq)
	require.NoError(t, err)
	require.NotNil(t, genResp)
	keyID := genResp.Key.KeyID
	defer cleanupKey(t, client, ctx, keyID)

	assert.NotEmpty(t, keyID)
	assert.Equal(t, service.KeyAlgAES256GCM, genResp.Key.Algorithm)
	assert.Equal(t, service.KeyStatusEnabled, genResp.Key.Status)

	// 2. Get key
	assertKeyExists(t, client, ctx, keyID)

	// 3. List keys (should include our key)
	listReq := &service.ListKeysRequest{
		Limit: 100,
	}
	listResp, err := client.KMSListKeys(ctx, listReq)
	require.NoError(t, err)
	assert.NotNil(t, listResp)

	// 4. Disable key
	updateReq := &service.UpdateKeyStatusRequest{
		KeyID:  keyID,
		Status: service.KeyStatusDisabled,
	}
	updateResp, err := client.KMSUpdateKeyStatus(ctx, updateReq)
	require.NoError(t, err)
	assert.Equal(t, service.KeyStatusDisabled, updateResp.Status)

	// 5. Enable key
	updateReq.Status = service.KeyStatusEnabled
	updateResp, err = client.KMSUpdateKeyStatus(ctx, updateReq)
	require.NoError(t, err)
	assert.Equal(t, service.KeyStatusEnabled, updateResp.Status)

	// 6. Rotate key
	rotateReq := &service.RotateKeyRequest{
		KeyID:            keyID,
		RetainOldVersion: true,
	}
	rotateResp, err := client.KMSRotateKey(ctx, rotateReq)
	require.NoError(t, err)
	assert.Greater(t, rotateResp.NewVersion, rotateResp.OldVersion)

	// 7. List key versions
	versionsReq := &service.ListKeyVersionsRequest{
		KeyID: keyID,
	}
	versionsResp, err := client.KMSListKeyVersions(ctx, versionsReq)
	require.NoError(t, err)
	assert.GreaterOrEqual(t, len(versionsResp.Versions), 2, "Should have at least 2 versions after rotation")

	// 8. Delete key
	deleteReq := &service.DeleteKeyRequest{
		KeyID: keyID,
		Force: true,
	}
	deleteResp, err := client.KMSDeleteKey(ctx, deleteReq)
	require.NoError(t, err)
	assert.Equal(t, keyID, deleteResp.KeyID)

	// 9. Verify key is deleted
	assertKeyNotFound(t, client, ctx, keyID)
}

// testRESTKeyLifecycle tests key lifecycle via REST.
func testRESTKeyLifecycle(t *testing.T, ctx context.Context) {
	endpoints := helpers.GetRESTEndpoints()
	client := NewRESTClient(endpoints[0])

	keyName := generateUniqueKeyName("rest-lifecycle-test")

	// 1. Create key
	genReq := &service.GenerateKeyRequest{
		Name:        keyName,
		Description: "REST integration test key lifecycle",
		KeyAttributes: &kctypes.KeyAttributes{
			SymmetricAlgorithm: kctypes.SymmetricAES256GCM,
		},
		Usage: []service.KeyUsage{service.KeyUsageEncrypt, service.KeyUsageDecrypt},
	}

	genResp, err := client.GenerateKey(ctx, genReq)
	require.NoError(t, err, "KMS must be available on REST")
	require.NotNil(t, genResp)
	keyID := genResp.Key.KeyID

	assert.NotEmpty(t, keyID)
	assert.Equal(t, service.KeyAlgAES256GCM, genResp.Key.Algorithm)
	assert.Equal(t, service.KeyStatusEnabled, genResp.Key.Status)

	// 2. Get key
	getReq := &service.GetKeyRequest{
		KeyID: keyID,
	}
	getResp, err := client.GetKey(ctx, getReq)
	require.NoError(t, err)
	assert.Equal(t, keyID, getResp.Metadata.KeyID)

	// 3. List keys
	listReq := &service.ListKeysRequest{
		Limit: 100,
	}
	listResp, err := client.ListKeys(ctx, listReq)
	require.NoError(t, err)
	assert.NotNil(t, listResp)

	// 4. Disable key
	updateReq := &service.UpdateKeyStatusRequest{
		KeyID:  keyID,
		Status: service.KeyStatusDisabled,
	}
	updateResp, err := client.UpdateKeyStatus(ctx, updateReq)
	require.NoError(t, err)
	assert.Equal(t, service.KeyStatusDisabled, updateResp.Status)

	// 5. Enable key
	updateReq.Status = service.KeyStatusEnabled
	updateResp, err = client.UpdateKeyStatus(ctx, updateReq)
	require.NoError(t, err)
	assert.Equal(t, service.KeyStatusEnabled, updateResp.Status)

	// 6. Rotate key
	rotateReq := &service.RotateKeyRequest{
		KeyID:            keyID,
		RetainOldVersion: true,
	}
	rotateResp, err := client.RotateKey(ctx, rotateReq)
	require.NoError(t, err)
	assert.Greater(t, rotateResp.NewVersion, rotateResp.OldVersion)

	// 7. Delete key
	deleteReq := &service.DeleteKeyRequest{
		KeyID: keyID,
		Force: true,
	}
	deleteResp, err := client.DeleteKey(ctx, deleteReq)
	require.NoError(t, err)
	assert.Equal(t, keyID, deleteResp.KeyID)

	// 8. Verify key is deleted
	_, err = client.GetKey(ctx, getReq)
	assert.Error(t, err)
}
