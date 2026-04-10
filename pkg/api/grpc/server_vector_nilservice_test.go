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
	"io"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"

	pb "github.com/jeremyhahn/go-quicbaas/api/proto"
	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
	"github.com/jeremyhahn/go-quicbaas/pkg/routing"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
	"github.com/jeremyhahn/go-quicbaas/pkg/testutil"
)

// nilVectorServiceWrapper wraps MockQuicBaaS but returns nil for Vector().
type nilVectorServiceWrapper struct {
	*testutil.MockQuicBaaS
}

func (w *nilVectorServiceWrapper) Vector() service.VectorService {
	return nil
}

// newNilVectorServer creates a Server whose Vector() returns nil to cover
// the "vector service not available" error paths.
func newNilVectorServer(t *testing.T) *Server {
	t.Helper()
	log, err := logger.New(logger.LogConfig{Level: "error", Format: "json", Output: "stderr"})
	require.NoError(t, err)

	mockSvc := testutil.NewMockQuicBaaS()
	wrapper := &nilVectorServiceWrapper{MockQuicBaaS: mockSvc}
	srv, err := NewServer(wrapper, log, 1, "test-1.0.0")
	require.NoError(t, err)
	srv.initMetrics()
	srv.SetShardRouter(routing.New())
	return srv
}

// stubServerStreamNV is a minimal gRPC server streaming mock for nil-vector tests.
type stubServerStreamNV struct {
	ctx context.Context
}

func (s *stubServerStreamNV) SetHeader(md metadata.MD) error  { return nil }
func (s *stubServerStreamNV) SendHeader(md metadata.MD) error { return nil }
func (s *stubServerStreamNV) SetTrailer(md metadata.MD)       {}
func (s *stubServerStreamNV) Context() context.Context        { return s.ctx }
func (s *stubServerStreamNV) SendMsg(m interface{}) error     { return nil }
func (s *stubServerStreamNV) RecvMsg(m interface{}) error     { return nil }

// stubBatchUpsertStreamNV implements pb.VectorDB_BatchUpsertVectorsServer.
type stubBatchUpsertStreamNV struct{ stubServerStreamNV }

func (s *stubBatchUpsertStreamNV) Send(*pb.BatchUpsertVectorsResponse) error { return nil }

// stubSearchSimilarStreamNV implements pb.VectorDB_SearchSimilarServer.
type stubSearchSimilarStreamNV struct{ stubServerStreamNV }

func (s *stubSearchSimilarStreamNV) Send(*pb.SearchSimilarResponse) error { return nil }

// stubSearchByIDStreamNV implements pb.VectorDB_SearchByIDServer.
type stubSearchByIDStreamNV struct{ stubServerStreamNV }

func (s *stubSearchByIDStreamNV) Send(*pb.SearchByIDResponse) error { return nil }

// stubHybridSearchStreamNV implements pb.VectorDB_HybridSearchServer.
type stubHybridSearchStreamNV struct{ stubServerStreamNV }

func (s *stubHybridSearchStreamNV) Send(*pb.HybridSearchResponse) error { return nil }

// stubSearchByMetadataStreamNV implements pb.VectorDB_SearchByMetadataServer.
type stubSearchByMetadataStreamNV struct{ stubServerStreamNV }

func (s *stubSearchByMetadataStreamNV) Send(*pb.SearchByMetadataResponse) error { return nil }

// stubListVectorsStreamNV implements pb.VectorDB_ListVectorsServer.
type stubListVectorsStreamNV struct{ stubServerStreamNV }

func (s *stubListVectorsStreamNV) Send(*pb.ListVectorsResponse) error { return nil }

// stubExportVectorsStreamNV implements pb.VectorAdmin_ExportVectorsServer.
type stubExportVectorsStreamNV struct{ stubServerStreamNV }

func (s *stubExportVectorsStreamNV) Send(*pb.ExportVectorsResponse) error { return nil }

// stubImportVectorsStreamNV implements pb.VectorAdmin_ImportVectorsServer.
type stubImportVectorsStreamNV struct{ stubServerStreamNV }

func (s *stubImportVectorsStreamNV) SendAndClose(*pb.ImportVectorsResponse) error { return nil }
func (s *stubImportVectorsStreamNV) Recv() (*pb.ImportVectorsRequest, error) {
	return nil, io.EOF
}

// TestVectorNilService tests all vector methods return codes.Unimplemented
// when Vector() service returns nil.
func TestVectorNilService(t *testing.T) {
	srv := newNilVectorServer(t)
	ctx := context.Background()

	t.Run("CreateNamespace", func(t *testing.T) {
		_, err := srv.CreateNamespace(ctx, &pb.CreateNamespaceRequest{
			Config: &pb.NamespaceConfig{Namespace: "ns", Dimensions: 128},
		})
		require.Error(t, err)
		assert.Equal(t, codes.Unimplemented, status.Code(err))
	})

	t.Run("UpdateNamespace", func(t *testing.T) {
		_, err := srv.UpdateNamespace(ctx, &pb.UpdateNamespaceRequest{
			Namespace: "ns",
		})
		require.Error(t, err)
		assert.Equal(t, codes.Unimplemented, status.Code(err))
	})

	t.Run("DeleteNamespace", func(t *testing.T) {
		_, err := srv.DeleteNamespace(ctx, &pb.DeleteNamespaceRequest{
			Namespace: "ns",
		})
		require.Error(t, err)
		assert.Equal(t, codes.Unimplemented, status.Code(err))
	})

	t.Run("ListNamespaces", func(t *testing.T) {
		_, err := srv.ListNamespaces(ctx, &pb.ListNamespacesRequest{})
		require.Error(t, err)
		assert.Equal(t, codes.Unimplemented, status.Code(err))
	})

	t.Run("GetNamespaceInfo", func(t *testing.T) {
		_, err := srv.GetNamespaceInfo(ctx, &pb.GetNamespaceInfoRequest{
			Namespace: "ns",
		})
		require.Error(t, err)
		assert.Equal(t, codes.Unimplemented, status.Code(err))
	})

	t.Run("UpsertVector", func(t *testing.T) {
		_, err := srv.UpsertVector(ctx, &pb.UpsertVectorRequest{
			Vector: &pb.VectorEntity{Namespace: "ns", Id: "v1", Vector: []float32{1.0}},
		})
		require.Error(t, err)
		assert.Equal(t, codes.Unimplemented, status.Code(err))
	})

	t.Run("UpdateMetadata", func(t *testing.T) {
		_, err := srv.UpdateMetadata(ctx, &pb.UpdateMetadataRequest{
			Namespace: "ns",
			Id:        "v1",
			Metadata:  map[string]string{"k": "v"},
		})
		require.Error(t, err)
		assert.Equal(t, codes.Unimplemented, status.Code(err))
	})

	t.Run("GetVector", func(t *testing.T) {
		_, err := srv.GetVector(ctx, &pb.GetVectorRequest{
			Namespace: "ns",
			Id:        "v1",
		})
		require.Error(t, err)
		assert.Equal(t, codes.Unimplemented, status.Code(err))
	})

	t.Run("DeleteVector", func(t *testing.T) {
		_, err := srv.DeleteVector(ctx, &pb.DeleteVectorRequest{
			Namespace: "ns",
			Id:        "v1",
		})
		require.Error(t, err)
		assert.Equal(t, codes.Unimplemented, status.Code(err))
	})

	t.Run("BatchDeleteVectors", func(t *testing.T) {
		_, err := srv.BatchDeleteVectors(ctx, &pb.BatchDeleteVectorsRequest{
			Namespace: "ns",
			Ids:       []string{"v1"},
		})
		require.Error(t, err)
		assert.Equal(t, codes.Unimplemented, status.Code(err))
	})

	t.Run("BatchUpsertVectors", func(t *testing.T) {
		err := srv.BatchUpsertVectors(&pb.BatchUpsertVectorsRequest{
			Vectors: []*pb.VectorEntity{{Namespace: "ns", Id: "v1", Vector: []float32{1.0}}},
		}, &stubBatchUpsertStreamNV{stubServerStreamNV{ctx: ctx}})
		require.Error(t, err)
		assert.Equal(t, codes.Unimplemented, status.Code(err))
	})

	t.Run("SearchSimilar", func(t *testing.T) {
		err := srv.SearchSimilar(&pb.SearchSimilarRequest{
			Namespace:   "ns",
			QueryVector: []float32{1.0},
			K:           10,
		}, &stubSearchSimilarStreamNV{stubServerStreamNV{ctx: ctx}})
		require.Error(t, err)
		assert.Equal(t, codes.Unimplemented, status.Code(err))
	})

	t.Run("SearchByID", func(t *testing.T) {
		err := srv.SearchByID(&pb.SearchByIDRequest{
			Namespace: "ns",
			Id:        "v1",
			K:         10,
		}, &stubSearchByIDStreamNV{stubServerStreamNV{ctx: ctx}})
		require.Error(t, err)
		assert.Equal(t, codes.Unimplemented, status.Code(err))
	})

	t.Run("HybridSearch", func(t *testing.T) {
		err := srv.HybridSearch(&pb.HybridSearchRequest{
			Namespace:   "ns",
			QueryVector: []float32{1.0},
			K:           10,
		}, &stubHybridSearchStreamNV{stubServerStreamNV{ctx: ctx}})
		require.Error(t, err)
		assert.Equal(t, codes.Unimplemented, status.Code(err))
	})

	t.Run("SearchByMetadata", func(t *testing.T) {
		err := srv.SearchByMetadata(&pb.SearchByMetadataRequest{
			Namespace:       "ns",
			MetadataFilters: []*pb.MetadataFilter{{Key: "k", Value: "v"}},
		}, &stubSearchByMetadataStreamNV{stubServerStreamNV{ctx: ctx}})
		require.Error(t, err)
		assert.Equal(t, codes.Unimplemented, status.Code(err))
	})

	t.Run("ListVectors", func(t *testing.T) {
		err := srv.ListVectors(&pb.ListVectorsRequest{
			Namespace: "ns",
		}, &stubListVectorsStreamNV{stubServerStreamNV{ctx: ctx}})
		require.Error(t, err)
		assert.Equal(t, codes.Unimplemented, status.Code(err))
	})

	t.Run("GetVectorStats", func(t *testing.T) {
		_, err := srv.GetVectorStats(ctx, &pb.GetVectorStatsRequest{
			Namespace: "ns",
		})
		require.Error(t, err)
		assert.Equal(t, codes.Unimplemented, status.Code(err))
	})

	t.Run("RebuildIndex", func(t *testing.T) {
		_, err := srv.RebuildIndex(ctx, &pb.RebuildIndexRequest{
			Namespace: "ns",
		})
		require.Error(t, err)
		assert.Equal(t, codes.Unimplemented, status.Code(err))
	})

	t.Run("GetIndexProgress", func(t *testing.T) {
		_, err := srv.GetIndexProgress(ctx, &pb.GetIndexProgressRequest{
			Namespace: "ns",
		})
		require.Error(t, err)
		assert.Equal(t, codes.Unimplemented, status.Code(err))
	})

	t.Run("GetClusterStats", func(t *testing.T) {
		_, err := srv.GetClusterStats(ctx, &pb.GetClusterStatsRequest{})
		require.Error(t, err)
		assert.Equal(t, codes.Unimplemented, status.Code(err))
	})

	t.Run("CompactNamespace", func(t *testing.T) {
		_, err := srv.CompactNamespace(ctx, &pb.CompactNamespaceRequest{
			Namespace: "ns",
		})
		require.Error(t, err)
		assert.Equal(t, codes.Unimplemented, status.Code(err))
	})

	t.Run("GetCompactionProgress", func(t *testing.T) {
		_, err := srv.GetCompactionProgress(ctx, &pb.GetCompactionProgressRequest{
			Namespace: "ns",
		})
		require.Error(t, err)
		assert.Equal(t, codes.Unimplemented, status.Code(err))
	})

	t.Run("ExportVectors", func(t *testing.T) {
		err := srv.ExportVectors(&pb.ExportVectorsRequest{
			Namespace: "ns",
		}, &stubExportVectorsStreamNV{stubServerStreamNV{ctx: ctx}})
		require.Error(t, err)
		assert.Equal(t, codes.Unimplemented, status.Code(err))
	})

	t.Run("ImportVectors", func(t *testing.T) {
		err := srv.ImportVectors(&stubImportVectorsStreamNV{stubServerStreamNV{ctx: ctx}})
		require.Error(t, err)
		assert.Equal(t, codes.Unimplemented, status.Code(err))
	})
}
