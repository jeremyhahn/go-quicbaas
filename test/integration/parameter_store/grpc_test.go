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

package parameter_store

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/connectivity"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/status"

	pb "github.com/jeremyhahn/go-quicbaas/api/proto"
	"github.com/jeremyhahn/go-quicbaas/test/integration/helpers"
)

// setupGRPCConnection creates a gRPC connection to the test cluster.
func setupGRPCConnection(t *testing.T, ctx context.Context) *grpc.ClientConn {
	t.Helper()
	endpoints := helpers.GetClusterEndpoints()
	if len(endpoints) == 0 {
		t.Fatal("No gRPC endpoints configured")
	}
	endpoint := endpoints[0]

	tlsCfg, err := helpers.IntegrationTLSConfig()
	require.NoError(t, err, "load integration TLS config")
	conn, err := grpc.DialContext(ctx, endpoint,
		grpc.WithTransportCredentials(credentials.NewTLS(tlsCfg)),
		grpc.WithDefaultCallOptions(grpc.WaitForReady(true)),
		grpc.WithPerRPCCredentials(helpers.TenantGRPCCredentials(t)),
	)
	require.NoError(t, err, "Failed to dial gRPC server at %s", endpoint)

	// Wait for connection to be ready
	dialCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	for {
		state := conn.GetState()
		if state == connectivity.Ready {
			return conn
		}
		if state == connectivity.TransientFailure || state == connectivity.Shutdown {
			t.Fatalf("gRPC connection failed with state: %v", state)
		}
		if !conn.WaitForStateChange(dialCtx, state) {
			t.Fatalf("gRPC connection timeout waiting for ready state")
		}
	}
}

// TestGRPC_SetAndGetParameter tests basic parameter set and get via gRPC.
func TestGRPC_SetAndGetParameter(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	conn := setupGRPCConnection(t, ctx)
	defer conn.Close()

	client := pb.NewParameterStoreClient(conn)

	// Unique path for this test
	path := fmt.Sprintf("/integration-test/grpc/basic/%d", time.Now().UnixNano())
	testValue := []byte("test-value-for-grpc-api")

	// Set parameter
	setResp, err := client.SetParameter(ctx, &pb.SetParameterRequest{
		Path:        path,
		Value:       testValue,
		Type:        pb.ParameterType_PARAMETER_TYPE_STRING,
		Description: "Integration test parameter",
		Tags: map[string]string{
			"env":     "integration",
			"service": "parameter-store",
		},
		Overwrite: true,
	})
	require.NoError(t, err)
	assert.Equal(t, path, setResp.Path)
	assert.True(t, setResp.Created, "Expected created=true for new parameter")

	// Get parameter
	getResp, err := client.GetParameter(ctx, &pb.GetParameterRequest{
		Path:           path,
		WithDecryption: true,
	})
	require.NoError(t, err)
	assert.Equal(t, path, getResp.Parameter.Path)
	assert.Equal(t, testValue, getResp.Parameter.Value)
	assert.Equal(t, pb.ParameterType_PARAMETER_TYPE_STRING, getResp.Parameter.Type)
	assert.Equal(t, "Integration test parameter", getResp.Parameter.Description)
	assert.Equal(t, "integration", getResp.Parameter.Tags["env"])
	assert.Equal(t, "parameter-store", getResp.Parameter.Tags["service"])

	// Cleanup
	_, err = client.DeleteParameter(ctx, &pb.DeleteParameterRequest{Path: path})
	require.NoError(t, err)
}

// TestGRPC_DeleteParameter tests parameter deletion via gRPC.
func TestGRPC_DeleteParameter(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	conn := setupGRPCConnection(t, ctx)
	defer conn.Close()

	client := pb.NewParameterStoreClient(conn)

	// Create parameter
	path := fmt.Sprintf("/integration-test/grpc/delete/%d", time.Now().UnixNano())
	_, err := client.SetParameter(ctx, &pb.SetParameterRequest{
		Path:      path,
		Value:     []byte("delete-me"),
		Type:      pb.ParameterType_PARAMETER_TYPE_STRING,
		Overwrite: true,
	})
	require.NoError(t, err)

	// Delete parameter
	deleteResp, err := client.DeleteParameter(ctx, &pb.DeleteParameterRequest{
		Path: path,
	})
	require.NoError(t, err)
	assert.Equal(t, path, deleteResp.Path)
	assert.True(t, deleteResp.Deleted)

	// Verify parameter is gone
	_, err = client.GetParameter(ctx, &pb.GetParameterRequest{Path: path})
	require.Error(t, err)
	st, ok := status.FromError(err)
	require.True(t, ok)
	assert.Equal(t, codes.NotFound, st.Code())
}

// TestGRPC_ListParameters tests parameter listing via gRPC.
func TestGRPC_ListParameters(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	conn := setupGRPCConnection(t, ctx)
	defer conn.Close()

	client := pb.NewParameterStoreClient(conn)

	// Create test prefix
	prefix := fmt.Sprintf("/integration-test/grpc/list/%d", time.Now().UnixNano())

	// Create multiple parameters
	paths := []string{
		prefix + "/param1",
		prefix + "/param2",
		prefix + "/nested/param3",
	}

	for i, path := range paths {
		_, err := client.SetParameter(ctx, &pb.SetParameterRequest{
			Path:      path,
			Value:     []byte(fmt.Sprintf("value-%d", i)),
			Type:      pb.ParameterType_PARAMETER_TYPE_STRING,
			Overwrite: true,
		})
		require.NoError(t, err)
	}

	// List parameters with prefix
	listResp, err := client.ListParameters(ctx, &pb.ListParametersRequest{
		Prefix:    prefix,
		Recursive: true,
		Limit:     100,
	})
	require.NoError(t, err)
	assert.GreaterOrEqual(t, len(listResp.Parameters), 3)

	// Cleanup
	for _, path := range paths {
		_, _ = client.DeleteParameter(ctx, &pb.DeleteParameterRequest{Path: path})
	}
}

// TestGRPC_BatchOperations tests batch get, set, and delete via gRPC.
func TestGRPC_BatchOperations(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	conn := setupGRPCConnection(t, ctx)
	defer conn.Close()

	client := pb.NewParameterStoreClient(conn)

	// Create test prefix
	prefix := fmt.Sprintf("/integration-test/grpc/batch/%d", time.Now().UnixNano())

	// Batch set parameters
	setResp, err := client.SetParameters(ctx, &pb.SetParametersRequest{
		Parameters: []*pb.SetParameterItem{
			{
				Path:      prefix + "/batch1",
				Value:     []byte("batch-value-1"),
				Type:      pb.ParameterType_PARAMETER_TYPE_STRING,
				Tags:      map[string]string{"batch": "true"},
				Overwrite: true,
			},
			{
				Path:      prefix + "/batch2",
				Value:     []byte("batch-value-2"),
				Type:      pb.ParameterType_PARAMETER_TYPE_STRING,
				Tags:      map[string]string{"batch": "true"},
				Overwrite: true,
			},
		},
	})
	require.NoError(t, err)
	assert.Len(t, setResp.Results, 2)
	for _, result := range setResp.Results {
		assert.True(t, result.Success)
	}

	// Batch get parameters
	getResp, err := client.GetParameters(ctx, &pb.GetParametersRequest{
		Paths:          []string{prefix + "/batch1", prefix + "/batch2"},
		WithDecryption: true,
	})
	require.NoError(t, err)
	assert.Len(t, getResp.Parameters, 2)

	// Batch delete parameters
	deleteResp, err := client.DeleteParameters(ctx, &pb.DeleteParametersRequest{
		Paths: []string{prefix + "/batch1", prefix + "/batch2"},
	})
	require.NoError(t, err)
	assert.Equal(t, int32(2), deleteResp.DeletedCount)
}

// TestGRPC_SearchByTags tests tag-based search via gRPC.
func TestGRPC_SearchByTags(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	conn := setupGRPCConnection(t, ctx)
	defer conn.Close()

	client := pb.NewParameterStoreClient(conn)

	// Create test parameters with unique tags
	prefix := fmt.Sprintf("/integration-test/grpc/search/%d", time.Now().UnixNano())
	uniqueTag := fmt.Sprintf("grpc-search-tag-%d", time.Now().UnixNano())

	paths := []string{prefix + "/search1", prefix + "/search2"}
	for i, path := range paths {
		_, err := client.SetParameter(ctx, &pb.SetParameterRequest{
			Path:  path,
			Value: []byte(fmt.Sprintf("search-value-%d", i)),
			Type:  pb.ParameterType_PARAMETER_TYPE_STRING,
			Tags: map[string]string{
				"search":    uniqueTag,
				"index":     fmt.Sprintf("%d", i),
				"component": "parameter-store",
			},
			Overwrite: true,
		})
		require.NoError(t, err)
	}

	// Search by tags
	searchResp, err := client.SearchByTags(ctx, &pb.SearchByTagsRequest{
		Tags: map[string]string{
			"search":    uniqueTag,
			"component": "parameter-store",
		},
		Limit: 100,
	})
	require.NoError(t, err)
	assert.GreaterOrEqual(t, len(searchResp.Parameters), 2)

	// Cleanup
	for _, path := range paths {
		_, _ = client.DeleteParameter(ctx, &pb.DeleteParameterRequest{Path: path})
	}
}

// TestGRPC_ParameterNotFound tests NotFound response for non-existent parameter.
func TestGRPC_ParameterNotFound(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	conn := setupGRPCConnection(t, ctx)
	defer conn.Close()

	client := pb.NewParameterStoreClient(conn)

	path := "/integration-test/grpc/nonexistent/parameter"

	_, err := client.GetParameter(ctx, &pb.GetParameterRequest{
		Path: path,
	})
	require.Error(t, err)
	st, ok := status.FromError(err)
	require.True(t, ok)
	assert.Equal(t, codes.NotFound, st.Code())
}

// TestGRPC_ParameterTypes tests different parameter types via gRPC.
func TestGRPC_ParameterTypes(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	conn := setupGRPCConnection(t, ctx)
	defer conn.Close()

	client := pb.NewParameterStoreClient(conn)

	prefix := fmt.Sprintf("/integration-test/grpc/types/%d", time.Now().UnixNano())

	testCases := []struct {
		name  string
		path  string
		ptype pb.ParameterType
		value []byte
	}{
		{
			name:  "string",
			path:  prefix + "/string",
			ptype: pb.ParameterType_PARAMETER_TYPE_STRING,
			value: []byte("hello world"),
		},
		{
			name:  "number",
			path:  prefix + "/number",
			ptype: pb.ParameterType_PARAMETER_TYPE_NUMBER,
			value: []byte("12345"),
		},
		{
			name:  "json",
			path:  prefix + "/json",
			ptype: pb.ParameterType_PARAMETER_TYPE_JSON,
			value: []byte(`{"key":"value","nested":{"a":1}}`),
		},
		{
			name:  "binary",
			path:  prefix + "/binary",
			ptype: pb.ParameterType_PARAMETER_TYPE_BINARY,
			value: []byte{0x00, 0x01, 0x02, 0xFF},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			// Set parameter
			_, err := client.SetParameter(ctx, &pb.SetParameterRequest{
				Path:      tc.path,
				Value:     tc.value,
				Type:      tc.ptype,
				Overwrite: true,
			})
			require.NoError(t, err)

			// Get and verify type
			getResp, err := client.GetParameter(ctx, &pb.GetParameterRequest{
				Path:           tc.path,
				WithDecryption: true,
			})
			require.NoError(t, err)
			assert.Equal(t, tc.ptype, getResp.Parameter.Type)
			assert.Equal(t, tc.value, getResp.Parameter.Value)

			// Cleanup
			_, _ = client.DeleteParameter(ctx, &pb.DeleteParameterRequest{Path: tc.path})
		})
	}
}

// TestGRPC_UpdateParameter tests parameter update (overwrite) via gRPC.
func TestGRPC_UpdateParameter(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	conn := setupGRPCConnection(t, ctx)
	defer conn.Close()

	client := pb.NewParameterStoreClient(conn)

	path := fmt.Sprintf("/integration-test/grpc/update/%d", time.Now().UnixNano())

	// Create initial parameter
	setResp, err := client.SetParameter(ctx, &pb.SetParameterRequest{
		Path:        path,
		Value:       []byte("initial-value"),
		Type:        pb.ParameterType_PARAMETER_TYPE_STRING,
		Description: "Initial description",
		Overwrite:   true,
	})
	require.NoError(t, err)
	assert.True(t, setResp.Created)

	// Update parameter
	setResp, err = client.SetParameter(ctx, &pb.SetParameterRequest{
		Path:        path,
		Value:       []byte("updated-value"),
		Type:        pb.ParameterType_PARAMETER_TYPE_STRING,
		Description: "Updated description",
		Overwrite:   true,
	})
	require.NoError(t, err)
	assert.False(t, setResp.Created, "Expected created=false for update")

	// Verify update
	getResp, err := client.GetParameter(ctx, &pb.GetParameterRequest{
		Path:           path,
		WithDecryption: true,
	})
	require.NoError(t, err)
	assert.Equal(t, []byte("updated-value"), getResp.Parameter.Value)
	assert.Equal(t, "Updated description", getResp.Parameter.Description)

	// Cleanup
	_, _ = client.DeleteParameter(ctx, &pb.DeleteParameterRequest{Path: path})
}
