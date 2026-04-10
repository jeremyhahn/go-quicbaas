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

package grpc

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	pb "github.com/jeremyhahn/go-quicbaas/api/proto"
	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
)

func TestNewVectorDBWrapper(t *testing.T) {
	// Create a mock server (nil for this basic test)
	wrapper := NewVectorDBWrapper(nil)
	if wrapper == nil {
		t.Fatal("expected non-nil wrapper")
	}
}

func TestNewVectorAdminWrapper(t *testing.T) {
	wrapper := NewVectorAdminWrapper(nil)
	if wrapper == nil {
		t.Fatal("expected non-nil wrapper")
	}
}

func TestVectorDBWrapper_Health(t *testing.T) {
	// Create a minimal server for the test
	server := &Server{
		nodeID:  12345,
		version: "test-version",
	}

	wrapper := NewVectorDBWrapper(server)

	resp, err := wrapper.Health(context.Background(), &pb.VectorHealthRequest{})
	if err != nil {
		t.Fatalf("Health returned error: %v", err)
	}

	if resp.NodeId != 12345 {
		t.Errorf("expected nodeId 12345, got %d", resp.NodeId)
	}

	if resp.Version != "test-version" {
		t.Errorf("expected version 'test-version', got %s", resp.Version)
	}

	// Service is nil, so health check correctly returns UNHEALTHY
	if resp.Status != pb.VectorHealthStatus_VECTOR_HEALTH_STATUS_UNHEALTHY {
		t.Errorf("expected UNHEALTHY status (service is nil), got %v", resp.Status)
	}
}

// TestVectorDBWrapper_CreateNamespace tests the CreateNamespace delegation.
func TestVectorDBWrapper_CreateNamespace_Delegation(t *testing.T) {
	log, _ := logger.New(logger.LogConfig{Level: "error", Format: "json", Output: "stdout"})
	server := &Server{
		logger:  log,
		nodeID:  1,
		version: "1.0.0",
	}

	wrapper := NewVectorDBWrapper(server)
	ctx := context.Background()

	resp, err := wrapper.CreateNamespace(ctx, &pb.CreateNamespaceRequest{
		Config: &pb.NamespaceConfig{
			Namespace:  "test-namespace",
			Dimensions: 128,
		},
	})

	// Server should return an error since vectorService is nil
	assert.Error(t, err)
	assert.Nil(t, resp)
}

// TestVectorDBWrapper_UpdateNamespace tests the UpdateNamespace delegation.
func TestVectorDBWrapper_UpdateNamespace_Delegation(t *testing.T) {
	log, _ := logger.New(logger.LogConfig{Level: "error", Format: "json", Output: "stdout"})
	server := &Server{
		logger:  log,
		nodeID:  1,
		version: "1.0.0",
	}

	wrapper := NewVectorDBWrapper(server)
	ctx := context.Background()

	resp, err := wrapper.UpdateNamespace(ctx, &pb.UpdateNamespaceRequest{
		Namespace: "test-namespace",
	})

	assert.Error(t, err)
	assert.Nil(t, resp)
}

// TestVectorDBWrapper_DeleteNamespace tests the DeleteNamespace delegation.
func TestVectorDBWrapper_DeleteNamespace_Delegation(t *testing.T) {
	log, _ := logger.New(logger.LogConfig{Level: "error", Format: "json", Output: "stdout"})
	server := &Server{
		logger:  log,
		nodeID:  1,
		version: "1.0.0",
	}

	wrapper := NewVectorDBWrapper(server)
	ctx := context.Background()

	resp, err := wrapper.DeleteNamespace(ctx, &pb.DeleteNamespaceRequest{
		Namespace: "test-namespace",
	})

	assert.Error(t, err)
	assert.Nil(t, resp)
}

// TestVectorDBWrapper_ListNamespaces tests the ListNamespaces delegation.
func TestVectorDBWrapper_ListNamespaces_Delegation(t *testing.T) {
	log, _ := logger.New(logger.LogConfig{Level: "error", Format: "json", Output: "stdout"})
	server := &Server{
		logger:  log,
		nodeID:  1,
		version: "1.0.0",
	}

	wrapper := NewVectorDBWrapper(server)
	ctx := context.Background()

	resp, err := wrapper.ListNamespaces(ctx, &pb.ListNamespacesRequest{})

	assert.Error(t, err)
	assert.Nil(t, resp)
}

// TestVectorDBWrapper_GetNamespaceInfo tests the GetNamespaceInfo delegation.
func TestVectorDBWrapper_GetNamespaceInfo_Delegation(t *testing.T) {
	log, _ := logger.New(logger.LogConfig{Level: "error", Format: "json", Output: "stdout"})
	server := &Server{
		logger:  log,
		nodeID:  1,
		version: "1.0.0",
	}

	wrapper := NewVectorDBWrapper(server)
	ctx := context.Background()

	resp, err := wrapper.GetNamespaceInfo(ctx, &pb.GetNamespaceInfoRequest{
		Namespace: "test-namespace",
	})

	assert.Error(t, err)
	assert.Nil(t, resp)
}

// TestVectorDBWrapper_UpsertVector tests the UpsertVector delegation.
func TestVectorDBWrapper_UpsertVector_Delegation(t *testing.T) {
	log, _ := logger.New(logger.LogConfig{Level: "error", Format: "json", Output: "stdout"})
	server := &Server{
		logger:  log,
		nodeID:  1,
		version: "1.0.0",
	}

	wrapper := NewVectorDBWrapper(server)
	ctx := context.Background()

	resp, err := wrapper.UpsertVector(ctx, &pb.UpsertVectorRequest{
		Vector: &pb.VectorEntity{
			Id:        "vec-1",
			Namespace: "test-namespace",
			Vector:    []float32{1.0, 2.0, 3.0},
		},
	})

	assert.Error(t, err)
	assert.Nil(t, resp)
}

// TestVectorDBWrapper_UpdateMetadata tests the UpdateMetadata delegation.
func TestVectorDBWrapper_UpdateMetadata_Delegation(t *testing.T) {
	log, _ := logger.New(logger.LogConfig{Level: "error", Format: "json", Output: "stdout"})
	server := &Server{
		logger:  log,
		nodeID:  1,
		version: "1.0.0",
	}

	wrapper := NewVectorDBWrapper(server)
	ctx := context.Background()

	resp, err := wrapper.UpdateMetadata(ctx, &pb.UpdateMetadataRequest{
		Namespace: "test-namespace",
		Id:        "vec-1",
		Metadata:  map[string]string{"key": "value"},
	})

	assert.Error(t, err)
	assert.Nil(t, resp)
}

// TestVectorDBWrapper_GetVector tests the GetVector delegation.
func TestVectorDBWrapper_GetVector_Delegation(t *testing.T) {
	log, _ := logger.New(logger.LogConfig{Level: "error", Format: "json", Output: "stdout"})
	server := &Server{
		logger:  log,
		nodeID:  1,
		version: "1.0.0",
	}

	wrapper := NewVectorDBWrapper(server)
	ctx := context.Background()

	resp, err := wrapper.GetVector(ctx, &pb.GetVectorRequest{
		Namespace: "test-namespace",
		Id:        "vec-1",
	})

	assert.Error(t, err)
	assert.Nil(t, resp)
}

// TestVectorDBWrapper_DeleteVector tests the DeleteVector delegation.
func TestVectorDBWrapper_DeleteVector_Delegation(t *testing.T) {
	log, _ := logger.New(logger.LogConfig{Level: "error", Format: "json", Output: "stdout"})
	server := &Server{
		logger:  log,
		nodeID:  1,
		version: "1.0.0",
	}

	wrapper := NewVectorDBWrapper(server)
	ctx := context.Background()

	resp, err := wrapper.DeleteVector(ctx, &pb.DeleteVectorRequest{
		Namespace: "test-namespace",
		Id:        "vec-1",
	})

	assert.Error(t, err)
	assert.Nil(t, resp)
}

// TestVectorDBWrapper_BatchDeleteVectors tests the BatchDeleteVectors delegation.
func TestVectorDBWrapper_BatchDeleteVectors_Delegation(t *testing.T) {
	log, _ := logger.New(logger.LogConfig{Level: "error", Format: "json", Output: "stdout"})
	server := &Server{
		logger:  log,
		nodeID:  1,
		version: "1.0.0",
	}

	wrapper := NewVectorDBWrapper(server)
	ctx := context.Background()

	resp, err := wrapper.BatchDeleteVectors(ctx, &pb.BatchDeleteVectorsRequest{
		Namespace: "test-namespace",
		Ids:       []string{"vec-1", "vec-2"},
	})

	assert.Error(t, err)
	assert.Nil(t, resp)
}

// TestVectorDBWrapper_GetVectorStats tests the GetVectorStats delegation.
func TestVectorDBWrapper_GetVectorStats_Delegation(t *testing.T) {
	log, _ := logger.New(logger.LogConfig{Level: "error", Format: "json", Output: "stdout"})
	server := &Server{
		logger:  log,
		nodeID:  1,
		version: "1.0.0",
	}

	wrapper := NewVectorDBWrapper(server)
	ctx := context.Background()

	resp, err := wrapper.GetVectorStats(ctx, &pb.GetVectorStatsRequest{
		Namespace: "test-namespace",
	})

	assert.Error(t, err)
	assert.Nil(t, resp)
}

// TestVectorDBWrapper_RebuildIndex tests the RebuildIndex delegation.
func TestVectorDBWrapper_RebuildIndex_Delegation(t *testing.T) {
	log, _ := logger.New(logger.LogConfig{Level: "error", Format: "json", Output: "stdout"})
	server := &Server{
		logger:  log,
		nodeID:  1,
		version: "1.0.0",
	}

	wrapper := NewVectorDBWrapper(server)
	ctx := context.Background()

	resp, err := wrapper.RebuildIndex(ctx, &pb.RebuildIndexRequest{
		Namespace: "test-namespace",
	})

	assert.Error(t, err)
	assert.Nil(t, resp)
}

// TestVectorDBWrapper_GetIndexProgress tests the GetIndexProgress delegation.
func TestVectorDBWrapper_GetIndexProgress_Delegation(t *testing.T) {
	log, _ := logger.New(logger.LogConfig{Level: "error", Format: "json", Output: "stdout"})
	server := &Server{
		logger:  log,
		nodeID:  1,
		version: "1.0.0",
	}

	wrapper := NewVectorDBWrapper(server)
	ctx := context.Background()

	resp, err := wrapper.GetIndexProgress(ctx, &pb.GetIndexProgressRequest{
		Namespace: "test-namespace",
	})

	assert.Error(t, err)
	assert.Nil(t, resp)
}

// TestVectorAdminWrapper_GetClusterStats tests the GetClusterStats delegation.
func TestVectorAdminWrapper_GetClusterStats_Delegation(t *testing.T) {
	log, _ := logger.New(logger.LogConfig{Level: "error", Format: "json", Output: "stdout"})
	server := &Server{
		logger:  log,
		nodeID:  1,
		version: "1.0.0",
	}

	wrapper := NewVectorAdminWrapper(server)
	ctx := context.Background()

	resp, err := wrapper.GetClusterStats(ctx, &pb.GetClusterStatsRequest{})

	assert.Error(t, err)
	assert.Nil(t, resp)
}

// TestVectorAdminWrapper_CompactNamespace tests the CompactNamespace delegation.
func TestVectorAdminWrapper_CompactNamespace_Delegation(t *testing.T) {
	log, _ := logger.New(logger.LogConfig{Level: "error", Format: "json", Output: "stdout"})
	server := &Server{
		logger:  log,
		nodeID:  1,
		version: "1.0.0",
	}

	wrapper := NewVectorAdminWrapper(server)
	ctx := context.Background()

	resp, err := wrapper.CompactNamespace(ctx, &pb.CompactNamespaceRequest{
		Namespace: "test-namespace",
	})

	assert.Error(t, err)
	assert.Nil(t, resp)
}

// TestVectorAdminWrapper_GetCompactionProgress tests the GetCompactionProgress delegation.
func TestVectorAdminWrapper_GetCompactionProgress_Delegation(t *testing.T) {
	log, _ := logger.New(logger.LogConfig{Level: "error", Format: "json", Output: "stdout"})
	server := &Server{
		logger:  log,
		nodeID:  1,
		version: "1.0.0",
	}

	wrapper := NewVectorAdminWrapper(server)
	ctx := context.Background()

	resp, err := wrapper.GetCompactionProgress(ctx, &pb.GetCompactionProgressRequest{
		Namespace: "test-namespace",
	})

	assert.Error(t, err)
	assert.Nil(t, resp)
}

// TestVectorDBWrapper_Health_Comprehensive tests the Health method with various inputs.
func TestVectorDBWrapper_Health_Comprehensive(t *testing.T) {
	t.Run("with nil request", func(t *testing.T) {
		server := &Server{
			nodeID:  12345,
			version: "1.0.0",
		}
		wrapper := NewVectorDBWrapper(server)

		resp, err := wrapper.Health(context.Background(), nil)
		require.NoError(t, err)
		require.NotNil(t, resp)
		// Service is nil, so health check correctly returns UNHEALTHY
		assert.Equal(t, pb.VectorHealthStatus_VECTOR_HEALTH_STATUS_UNHEALTHY, resp.Status)
	})

	t.Run("with valid request", func(t *testing.T) {
		server := &Server{
			nodeID:  99999,
			version: "2.5.0",
		}
		wrapper := NewVectorDBWrapper(server)

		resp, err := wrapper.Health(context.Background(), &pb.VectorHealthRequest{})
		require.NoError(t, err)
		require.NotNil(t, resp)
		assert.Equal(t, uint64(99999), resp.NodeId)
		assert.Equal(t, "2.5.0", resp.Version)
	})
}
