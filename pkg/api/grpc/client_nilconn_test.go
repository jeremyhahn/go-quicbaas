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
)

// newNilConnClient creates a Client that passes ensureConnected but has no
// connection or pool, so every getXxxClient helper returns a "client closed"
// error. This exercises the error branch inside each RPC wrapper without
// requiring a live server.
func newNilConnClient() *Client {
	return &Client{
		connected: true,
		closed:    false,
		conn:      nil,
		pool:      nil,
		opts:      defaultClientOptions(),
	}
}

// =============================================================================
// ObjectStorage – nil-conn error paths
// =============================================================================

func TestClient_ObjectStoragePutObject_NilConn(t *testing.T) {
	t.Parallel()
	client := newNilConnClient()
	resp, err := client.ObjectStoragePutObject(context.Background(), &pb.PutObjectRequest{})
	require.Error(t, err)
	assert.Nil(t, resp)
	assert.Contains(t, err.Error(), "closed")
}

func TestClient_ObjectStorageGetObject_NilConn(t *testing.T) {
	t.Parallel()
	client := newNilConnClient()
	resp, err := client.ObjectStorageGetObject(context.Background(), &pb.GetObjectRequest{})
	require.Error(t, err)
	assert.Nil(t, resp)
	assert.Contains(t, err.Error(), "closed")
}

func TestClient_ObjectStorageDeleteObject_NilConn(t *testing.T) {
	t.Parallel()
	client := newNilConnClient()
	resp, err := client.ObjectStorageDeleteObject(context.Background(), &pb.DeleteObjectRequest{})
	require.Error(t, err)
	assert.Nil(t, resp)
	assert.Contains(t, err.Error(), "closed")
}

func TestClient_ObjectStorageHeadObject_NilConn(t *testing.T) {
	t.Parallel()
	client := newNilConnClient()
	resp, err := client.ObjectStorageHeadObject(context.Background(), &pb.HeadObjectRequest{})
	require.Error(t, err)
	assert.Nil(t, resp)
	assert.Contains(t, err.Error(), "closed")
}

func TestClient_ObjectStorageListObjects_NilConn(t *testing.T) {
	t.Parallel()
	client := newNilConnClient()
	resp, err := client.ObjectStorageListObjects(context.Background(), &pb.ListObjectsRequest{})
	require.Error(t, err)
	assert.Nil(t, resp)
	assert.Contains(t, err.Error(), "closed")
}

func TestClient_ObjectStorageListByTag_NilConn(t *testing.T) {
	t.Parallel()
	client := newNilConnClient()
	resp, err := client.ObjectStorageListByTag(context.Background(), &pb.ListByTagRequest{})
	require.Error(t, err)
	assert.Nil(t, resp)
	assert.Contains(t, err.Error(), "closed")
}

func TestClient_ObjectStorageListByTenant_NilConn(t *testing.T) {
	t.Parallel()
	client := newNilConnClient()
	resp, err := client.ObjectStorageListByTenant(context.Background(), &pb.ListByTenantRequest{})
	require.Error(t, err)
	assert.Nil(t, resp)
	assert.Contains(t, err.Error(), "closed")
}

func TestClient_ObjectStorageCopyObject_NilConn(t *testing.T) {
	t.Parallel()
	client := newNilConnClient()
	resp, err := client.ObjectStorageCopyObject(context.Background(), &pb.CopyObjectRequest{})
	require.Error(t, err)
	assert.Nil(t, resp)
	assert.Contains(t, err.Error(), "closed")
}

func TestClient_ObjectStorageReplicateObject_NilConn(t *testing.T) {
	t.Parallel()
	client := newNilConnClient()
	resp, err := client.ObjectStorageReplicateObject(context.Background(), &pb.ReplicateObjectRequest{})
	require.Error(t, err)
	assert.Nil(t, resp)
	assert.Contains(t, err.Error(), "closed")
}

func TestClient_ObjectStorageMigrateObject_NilConn(t *testing.T) {
	t.Parallel()
	client := newNilConnClient()
	resp, err := client.ObjectStorageMigrateObject(context.Background(), &pb.MigrateObjectRequest{})
	require.Error(t, err)
	assert.Nil(t, resp)
	assert.Contains(t, err.Error(), "closed")
}

func TestClient_ObjectStorageArchiveObject_NilConn(t *testing.T) {
	t.Parallel()
	client := newNilConnClient()
	resp, err := client.ObjectStorageArchiveObject(context.Background(), &pb.ArchiveObjectRequest{})
	require.Error(t, err)
	assert.Nil(t, resp)
	assert.Contains(t, err.Error(), "closed")
}

func TestClient_ObjectStorageRestoreObject_NilConn(t *testing.T) {
	t.Parallel()
	client := newNilConnClient()
	resp, err := client.ObjectStorageRestoreObject(context.Background(), &pb.RestoreObjectRequest{})
	require.Error(t, err)
	assert.Nil(t, resp)
	assert.Contains(t, err.Error(), "closed")
}

func TestClient_ObjectStorageVerifyObject_NilConn(t *testing.T) {
	t.Parallel()
	client := newNilConnClient()
	resp, err := client.ObjectStorageVerifyObject(context.Background(), &pb.VerifyObjectRequest{})
	require.Error(t, err)
	assert.Nil(t, resp)
	assert.Contains(t, err.Error(), "closed")
}

func TestClient_ObjectStorageSetObjectPolicy_NilConn(t *testing.T) {
	t.Parallel()
	client := newNilConnClient()
	resp, err := client.ObjectStorageSetObjectPolicy(context.Background(), &pb.SetObjectPolicyRequest{})
	require.Error(t, err)
	assert.Nil(t, resp)
	assert.Contains(t, err.Error(), "closed")
}

func TestClient_ObjectStorageGetObjectPolicy_NilConn(t *testing.T) {
	t.Parallel()
	client := newNilConnClient()
	resp, err := client.ObjectStorageGetObjectPolicy(context.Background(), &pb.GetObjectPolicyRequest{})
	require.Error(t, err)
	assert.Nil(t, resp)
	assert.Contains(t, err.Error(), "closed")
}

func TestClient_ObjectStorageListBackends_NilConn(t *testing.T) {
	t.Parallel()
	client := newNilConnClient()
	resp, err := client.ObjectStorageListBackends(context.Background(), &pb.ListBackendsRequest{})
	require.Error(t, err)
	assert.Nil(t, resp)
	assert.Contains(t, err.Error(), "closed")
}

func TestClient_ObjectStorageGetBackendHealth_NilConn(t *testing.T) {
	t.Parallel()
	client := newNilConnClient()
	resp, err := client.ObjectStorageGetBackendHealth(context.Background(), &pb.GetBackendHealthRequest{})
	require.Error(t, err)
	assert.Nil(t, resp)
	assert.Contains(t, err.Error(), "closed")
}

func TestClient_ObjectStorageGetTenantUsage_NilConn(t *testing.T) {
	t.Parallel()
	client := newNilConnClient()
	resp, err := client.ObjectStorageGetTenantUsage(context.Background(), &pb.GetObjectStorageTenantUsageRequest{})
	require.Error(t, err)
	assert.Nil(t, resp)
	assert.Contains(t, err.Error(), "closed")
}

func TestClient_ObjectStorageGetTenantQuotas_NilConn(t *testing.T) {
	t.Parallel()
	client := newNilConnClient()
	resp, err := client.ObjectStorageGetTenantQuotas(context.Background(), &pb.GetObjectStorageTenantQuotasRequest{})
	require.Error(t, err)
	assert.Nil(t, resp)
	assert.Contains(t, err.Error(), "closed")
}

// =============================================================================
// Ollama – nil-conn error paths
// =============================================================================

func TestClient_OllamaHealth_NilConn(t *testing.T) {
	t.Parallel()
	client := newNilConnClient()
	resp, err := client.OllamaHealth(context.Background(), &pb.OllamaHealthRequest{})
	require.Error(t, err)
	assert.Nil(t, resp)
	assert.Contains(t, err.Error(), "closed")
}

func TestClient_OllamaListModels_NilConn(t *testing.T) {
	t.Parallel()
	client := newNilConnClient()
	resp, err := client.OllamaListModels(context.Background(), &pb.OllamaListModelsRequest{})
	require.Error(t, err)
	assert.Nil(t, resp)
	assert.Contains(t, err.Error(), "closed")
}

func TestClient_OllamaGetModelInfo_NilConn(t *testing.T) {
	t.Parallel()
	client := newNilConnClient()
	resp, err := client.OllamaGetModelInfo(context.Background(), &pb.OllamaGetModelInfoRequest{})
	require.Error(t, err)
	assert.Nil(t, resp)
	assert.Contains(t, err.Error(), "closed")
}

func TestClient_OllamaEmbed_NilConn(t *testing.T) {
	t.Parallel()
	client := newNilConnClient()
	resp, err := client.OllamaEmbed(context.Background(), &pb.OllamaEmbedRequest{})
	require.Error(t, err)
	assert.Nil(t, resp)
	assert.Contains(t, err.Error(), "closed")
}

func TestClient_OllamaEmbedWithModel_NilConn(t *testing.T) {
	t.Parallel()
	client := newNilConnClient()
	resp, err := client.OllamaEmbedWithModel(context.Background(), &pb.OllamaEmbedWithModelRequest{})
	require.Error(t, err)
	assert.Nil(t, resp)
	assert.Contains(t, err.Error(), "closed")
}

func TestClient_OllamaEmbedBatch_NilConn(t *testing.T) {
	t.Parallel()
	client := newNilConnClient()
	resp, err := client.OllamaEmbedBatch(context.Background(), &pb.OllamaEmbedBatchRequest{})
	require.Error(t, err)
	assert.Nil(t, resp)
	assert.Contains(t, err.Error(), "closed")
}

// =============================================================================
// Vector – nil-conn error paths (getVectorClient)
// =============================================================================

func TestClient_VectorCreateNamespace_NilConn(t *testing.T) {
	t.Parallel()
	client := newNilConnClient()
	resp, err := client.VectorCreateNamespace(context.Background(), &pb.CreateNamespaceRequest{})
	require.Error(t, err)
	assert.Nil(t, resp)
	assert.Contains(t, err.Error(), "closed")
}

func TestClient_VectorDeleteNamespace_NilConn(t *testing.T) {
	t.Parallel()
	client := newNilConnClient()
	resp, err := client.VectorDeleteNamespace(context.Background(), &pb.DeleteNamespaceRequest{})
	require.Error(t, err)
	assert.Nil(t, resp)
	assert.Contains(t, err.Error(), "closed")
}

func TestClient_VectorListNamespaces_NilConn(t *testing.T) {
	t.Parallel()
	client := newNilConnClient()
	resp, err := client.VectorListNamespaces(context.Background(), &pb.ListNamespacesRequest{})
	require.Error(t, err)
	assert.Nil(t, resp)
	assert.Contains(t, err.Error(), "closed")
}

func TestClient_VectorGetNamespaceInfo_NilConn(t *testing.T) {
	t.Parallel()
	client := newNilConnClient()
	resp, err := client.VectorGetNamespaceInfo(context.Background(), &pb.GetNamespaceInfoRequest{})
	require.Error(t, err)
	assert.Nil(t, resp)
	assert.Contains(t, err.Error(), "closed")
}

func TestClient_VectorUpsert_NilConn(t *testing.T) {
	t.Parallel()
	client := newNilConnClient()
	resp, err := client.VectorUpsert(context.Background(), &pb.UpsertVectorRequest{})
	require.Error(t, err)
	assert.Nil(t, resp)
	assert.Contains(t, err.Error(), "closed")
}

func TestClient_VectorGet_NilConn(t *testing.T) {
	t.Parallel()
	client := newNilConnClient()
	resp, err := client.VectorGet(context.Background(), &pb.GetVectorRequest{})
	require.Error(t, err)
	assert.Nil(t, resp)
	assert.Contains(t, err.Error(), "closed")
}

func TestClient_VectorDelete_NilConn(t *testing.T) {
	t.Parallel()
	client := newNilConnClient()
	resp, err := client.VectorDelete(context.Background(), &pb.DeleteVectorRequest{})
	require.Error(t, err)
	assert.Nil(t, resp)
	assert.Contains(t, err.Error(), "closed")
}

func TestClient_VectorBatchUpsert_NilConn(t *testing.T) {
	t.Parallel()
	client := newNilConnClient()
	resp, err := client.VectorBatchUpsert(context.Background(), &pb.BatchUpsertVectorsRequest{})
	require.Error(t, err)
	assert.Nil(t, resp)
	assert.Contains(t, err.Error(), "closed")
}

func TestClient_VectorBatchDelete_NilConn(t *testing.T) {
	t.Parallel()
	client := newNilConnClient()
	resp, err := client.VectorBatchDelete(context.Background(), &pb.BatchDeleteVectorsRequest{})
	require.Error(t, err)
	assert.Nil(t, resp)
	assert.Contains(t, err.Error(), "closed")
}

func TestClient_VectorUpdateMetadata_NilConn(t *testing.T) {
	t.Parallel()
	client := newNilConnClient()
	resp, err := client.VectorUpdateMetadata(context.Background(), &pb.UpdateMetadataRequest{})
	require.Error(t, err)
	assert.Nil(t, resp)
	assert.Contains(t, err.Error(), "closed")
}

func TestClient_VectorHealth_NilConn(t *testing.T) {
	t.Parallel()
	client := newNilConnClient()
	resp, err := client.VectorHealth(context.Background(), &pb.VectorHealthRequest{})
	require.Error(t, err)
	assert.Nil(t, resp)
	assert.Contains(t, err.Error(), "closed")
}

func TestClient_VectorGetStats_NilConn(t *testing.T) {
	t.Parallel()
	client := newNilConnClient()
	resp, err := client.VectorGetStats(context.Background(), &pb.GetVectorStatsRequest{})
	require.Error(t, err)
	assert.Nil(t, resp)
	assert.Contains(t, err.Error(), "closed")
}

func TestClient_VectorSearchSimilar_NilConn(t *testing.T) {
	t.Parallel()
	client := newNilConnClient()
	resp, err := client.VectorSearchSimilar(context.Background(), &pb.SearchSimilarRequest{})
	require.Error(t, err)
	assert.Nil(t, resp)
	assert.Contains(t, err.Error(), "closed")
}

func TestClient_VectorSearchByID_NilConn(t *testing.T) {
	t.Parallel()
	client := newNilConnClient()
	resp, err := client.VectorSearchByID(context.Background(), &pb.SearchByIDRequest{})
	require.Error(t, err)
	assert.Nil(t, resp)
	assert.Contains(t, err.Error(), "closed")
}

func TestClient_VectorHybridSearch_NilConn(t *testing.T) {
	t.Parallel()
	client := newNilConnClient()
	resp, err := client.VectorHybridSearch(context.Background(), &pb.HybridSearchRequest{})
	require.Error(t, err)
	assert.Nil(t, resp)
	assert.Contains(t, err.Error(), "closed")
}

func TestClient_VectorSearchByMetadata_NilConn(t *testing.T) {
	t.Parallel()
	client := newNilConnClient()
	resp, err := client.VectorSearchByMetadata(context.Background(), &pb.SearchByMetadataRequest{})
	require.Error(t, err)
	assert.Nil(t, resp)
	assert.Contains(t, err.Error(), "closed")
}

func TestClient_VectorList_NilConn(t *testing.T) {
	t.Parallel()
	client := newNilConnClient()
	resp, err := client.VectorList(context.Background(), &pb.ListVectorsRequest{})
	require.Error(t, err)
	assert.Nil(t, resp)
	assert.Contains(t, err.Error(), "closed")
}

func TestClient_VectorRebuildIndex_NilConn(t *testing.T) {
	t.Parallel()
	client := newNilConnClient()
	resp, err := client.VectorRebuildIndex(context.Background(), &pb.RebuildIndexRequest{})
	require.Error(t, err)
	assert.Nil(t, resp)
	assert.Contains(t, err.Error(), "closed")
}

func TestClient_VectorGetIndexProgress_NilConn(t *testing.T) {
	t.Parallel()
	client := newNilConnClient()
	resp, err := client.VectorGetIndexProgress(context.Background(), &pb.GetIndexProgressRequest{})
	require.Error(t, err)
	assert.Nil(t, resp)
	assert.Contains(t, err.Error(), "closed")
}

// =============================================================================
// Vector Admin – nil-conn error paths (getVectorAdminClient)
// =============================================================================

func TestClient_VectorCompact_NilConn(t *testing.T) {
	t.Parallel()
	client := newNilConnClient()
	resp, err := client.VectorCompact(context.Background(), &pb.CompactNamespaceRequest{})
	require.Error(t, err)
	assert.Nil(t, resp)
	assert.Contains(t, err.Error(), "closed")
}

func TestClient_VectorGetCompactionProgress_NilConn(t *testing.T) {
	t.Parallel()
	client := newNilConnClient()
	resp, err := client.VectorGetCompactionProgress(context.Background(), &pb.GetCompactionProgressRequest{})
	require.Error(t, err)
	assert.Nil(t, resp)
	assert.Contains(t, err.Error(), "closed")
}

// =============================================================================
// getXxxClient – nil-conn direct tests
// =============================================================================

func TestGetObjectStorageClient_NilConn(t *testing.T) {
	t.Parallel()
	client := newNilConnClient()
	c, err := client.getObjectStorageClient()
	require.Error(t, err)
	assert.Nil(t, c)
	assert.Contains(t, err.Error(), "closed")
}

func TestGetOllamaClient_NilConn(t *testing.T) {
	t.Parallel()
	client := newNilConnClient()
	c, err := client.getOllamaClient()
	require.Error(t, err)
	assert.Nil(t, c)
	assert.Contains(t, err.Error(), "closed")
}

func TestGetVectorClient_NilConn(t *testing.T) {
	t.Parallel()
	client := newNilConnClient()
	c, err := client.getVectorClient()
	require.Error(t, err)
	assert.Nil(t, c)
	assert.Contains(t, err.Error(), "closed")
}

func TestGetVectorAdminClient_NilConn(t *testing.T) {
	t.Parallel()
	client := newNilConnClient()
	c, err := client.getVectorAdminClient()
	require.Error(t, err)
	assert.Nil(t, c)
	assert.Contains(t, err.Error(), "closed")
}
