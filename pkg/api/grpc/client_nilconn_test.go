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

// nilConnCase is one RPC wrapper called on a nil-connection client.
type nilConnCase struct {
	name string
	call func(ctx context.Context, c *Client) (any, error)
}

// runNilConnCases checks that each wrapper returns a nil response and the
// "client closed" error from its getXxxClient helper.
func runNilConnCases(t *testing.T, cases []nilConnCase) {
	t.Helper()
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			resp, err := tc.call(context.Background(), newNilConnClient())
			require.Error(t, err)
			assert.Nil(t, resp)
			assert.Contains(t, err.Error(), "closed")
		})
	}
}

func TestClient_ObjectStorage_NilConn(t *testing.T) {
	t.Parallel()
	runNilConnCases(t, []nilConnCase{
		{"PutObject", func(ctx context.Context, c *Client) (any, error) {
			return c.ObjectStoragePutObject(ctx, &pb.PutObjectRequest{})
		}},
		{"GetObject", func(ctx context.Context, c *Client) (any, error) {
			return c.ObjectStorageGetObject(ctx, &pb.GetObjectRequest{})
		}},
		{"DeleteObject", func(ctx context.Context, c *Client) (any, error) {
			return c.ObjectStorageDeleteObject(ctx, &pb.DeleteObjectRequest{})
		}},
		{"HeadObject", func(ctx context.Context, c *Client) (any, error) {
			return c.ObjectStorageHeadObject(ctx, &pb.HeadObjectRequest{})
		}},
		{"ListObjects", func(ctx context.Context, c *Client) (any, error) {
			return c.ObjectStorageListObjects(ctx, &pb.ListObjectsRequest{})
		}},
		{"ListByTag", func(ctx context.Context, c *Client) (any, error) {
			return c.ObjectStorageListByTag(ctx, &pb.ListByTagRequest{})
		}},
		{"ListByTenant", func(ctx context.Context, c *Client) (any, error) {
			return c.ObjectStorageListByTenant(ctx, &pb.ListByTenantRequest{})
		}},
		{"CopyObject", func(ctx context.Context, c *Client) (any, error) {
			return c.ObjectStorageCopyObject(ctx, &pb.CopyObjectRequest{})
		}},
		{"ReplicateObject", func(ctx context.Context, c *Client) (any, error) {
			return c.ObjectStorageReplicateObject(ctx, &pb.ReplicateObjectRequest{})
		}},
		{"MigrateObject", func(ctx context.Context, c *Client) (any, error) {
			return c.ObjectStorageMigrateObject(ctx, &pb.MigrateObjectRequest{})
		}},
		{"ArchiveObject", func(ctx context.Context, c *Client) (any, error) {
			return c.ObjectStorageArchiveObject(ctx, &pb.ArchiveObjectRequest{})
		}},
		{"RestoreObject", func(ctx context.Context, c *Client) (any, error) {
			return c.ObjectStorageRestoreObject(ctx, &pb.RestoreObjectRequest{})
		}},
		{"VerifyObject", func(ctx context.Context, c *Client) (any, error) {
			return c.ObjectStorageVerifyObject(ctx, &pb.VerifyObjectRequest{})
		}},
		{"SetObjectPolicy", func(ctx context.Context, c *Client) (any, error) {
			return c.ObjectStorageSetObjectPolicy(ctx, &pb.SetObjectPolicyRequest{})
		}},
		{"GetObjectPolicy", func(ctx context.Context, c *Client) (any, error) {
			return c.ObjectStorageGetObjectPolicy(ctx, &pb.GetObjectPolicyRequest{})
		}},
		{"ListBackends", func(ctx context.Context, c *Client) (any, error) {
			return c.ObjectStorageListBackends(ctx, &pb.ListBackendsRequest{})
		}},
		{"GetBackendHealth", func(ctx context.Context, c *Client) (any, error) {
			return c.ObjectStorageGetBackendHealth(ctx, &pb.GetBackendHealthRequest{})
		}},
		{"GetTenantUsage", func(ctx context.Context, c *Client) (any, error) {
			return c.ObjectStorageGetTenantUsage(ctx, &pb.GetObjectStorageTenantUsageRequest{})
		}},
		{"GetTenantQuotas", func(ctx context.Context, c *Client) (any, error) {
			return c.ObjectStorageGetTenantQuotas(ctx, &pb.GetObjectStorageTenantQuotasRequest{})
		}},
	})
}

func TestClient_Ollama_NilConn(t *testing.T) {
	t.Parallel()
	runNilConnCases(t, []nilConnCase{
		{"Health", func(ctx context.Context, c *Client) (any, error) {
			return c.OllamaHealth(ctx, &pb.OllamaHealthRequest{})
		}},
		{"ListModels", func(ctx context.Context, c *Client) (any, error) {
			return c.OllamaListModels(ctx, &pb.OllamaListModelsRequest{})
		}},
		{"GetModelInfo", func(ctx context.Context, c *Client) (any, error) {
			return c.OllamaGetModelInfo(ctx, &pb.OllamaGetModelInfoRequest{})
		}},
		{"Embed", func(ctx context.Context, c *Client) (any, error) {
			return c.OllamaEmbed(ctx, &pb.OllamaEmbedRequest{})
		}},
		{"EmbedWithModel", func(ctx context.Context, c *Client) (any, error) {
			return c.OllamaEmbedWithModel(ctx, &pb.OllamaEmbedWithModelRequest{})
		}},
		{"EmbedBatch", func(ctx context.Context, c *Client) (any, error) {
			return c.OllamaEmbedBatch(ctx, &pb.OllamaEmbedBatchRequest{})
		}},
	})
}

// TestClient_Vector_NilConn covers the getVectorClient wrappers and the
// getVectorAdminClient wrappers (Compact, GetCompactionProgress).
func TestClient_Vector_NilConn(t *testing.T) {
	t.Parallel()
	runNilConnCases(t, []nilConnCase{
		{"CreateNamespace", func(ctx context.Context, c *Client) (any, error) {
			return c.VectorCreateNamespace(ctx, &pb.CreateNamespaceRequest{})
		}},
		{"DeleteNamespace", func(ctx context.Context, c *Client) (any, error) {
			return c.VectorDeleteNamespace(ctx, &pb.DeleteNamespaceRequest{})
		}},
		{"ListNamespaces", func(ctx context.Context, c *Client) (any, error) {
			return c.VectorListNamespaces(ctx, &pb.ListNamespacesRequest{})
		}},
		{"GetNamespaceInfo", func(ctx context.Context, c *Client) (any, error) {
			return c.VectorGetNamespaceInfo(ctx, &pb.GetNamespaceInfoRequest{})
		}},
		{"Upsert", func(ctx context.Context, c *Client) (any, error) {
			return c.VectorUpsert(ctx, &pb.UpsertVectorRequest{})
		}},
		{"Get", func(ctx context.Context, c *Client) (any, error) {
			return c.VectorGet(ctx, &pb.GetVectorRequest{})
		}},
		{"Delete", func(ctx context.Context, c *Client) (any, error) {
			return c.VectorDelete(ctx, &pb.DeleteVectorRequest{})
		}},
		{"BatchUpsert", func(ctx context.Context, c *Client) (any, error) {
			return c.VectorBatchUpsert(ctx, &pb.BatchUpsertVectorsRequest{})
		}},
		{"BatchDelete", func(ctx context.Context, c *Client) (any, error) {
			return c.VectorBatchDelete(ctx, &pb.BatchDeleteVectorsRequest{})
		}},
		{"UpdateMetadata", func(ctx context.Context, c *Client) (any, error) {
			return c.VectorUpdateMetadata(ctx, &pb.UpdateMetadataRequest{})
		}},
		{"Health", func(ctx context.Context, c *Client) (any, error) {
			return c.VectorHealth(ctx, &pb.VectorHealthRequest{})
		}},
		{"GetStats", func(ctx context.Context, c *Client) (any, error) {
			return c.VectorGetStats(ctx, &pb.GetVectorStatsRequest{})
		}},
		{"SearchSimilar", func(ctx context.Context, c *Client) (any, error) {
			return c.VectorSearchSimilar(ctx, &pb.SearchSimilarRequest{})
		}},
		{"SearchByID", func(ctx context.Context, c *Client) (any, error) {
			return c.VectorSearchByID(ctx, &pb.SearchByIDRequest{})
		}},
		{"HybridSearch", func(ctx context.Context, c *Client) (any, error) {
			return c.VectorHybridSearch(ctx, &pb.HybridSearchRequest{})
		}},
		{"SearchByMetadata", func(ctx context.Context, c *Client) (any, error) {
			return c.VectorSearchByMetadata(ctx, &pb.SearchByMetadataRequest{})
		}},
		{"List", func(ctx context.Context, c *Client) (any, error) {
			return c.VectorList(ctx, &pb.ListVectorsRequest{})
		}},
		{"RebuildIndex", func(ctx context.Context, c *Client) (any, error) {
			return c.VectorRebuildIndex(ctx, &pb.RebuildIndexRequest{})
		}},
		{"GetIndexProgress", func(ctx context.Context, c *Client) (any, error) {
			return c.VectorGetIndexProgress(ctx, &pb.GetIndexProgressRequest{})
		}},
		{"Compact", func(ctx context.Context, c *Client) (any, error) {
			return c.VectorCompact(ctx, &pb.CompactNamespaceRequest{})
		}},
		{"GetCompactionProgress", func(ctx context.Context, c *Client) (any, error) {
			return c.VectorGetCompactionProgress(ctx, &pb.GetCompactionProgressRequest{})
		}},
	})
}
