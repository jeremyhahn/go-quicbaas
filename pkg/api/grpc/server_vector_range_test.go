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
	"math"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	pb "github.com/jeremyhahn/go-quicbaas/api/proto"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
	"github.com/jeremyhahn/go-quicbaas/pkg/tenantctx"
	"github.com/jeremyhahn/go-quicbaas/pkg/testutil"
)

// requireRangeStatus asserts that err is a gRPC status with the given code
// whose message names the field that failed to narrow.
func requireRangeStatus(t *testing.T, err error, code codes.Code, field string) {
	t.Helper()
	st, ok := status.FromError(err)
	require.True(t, ok, "expected a gRPC status, got %v", err)
	assert.Equal(t, code, st.Code())
	assert.Contains(t, st.Message(), field+":")
}

// TestVectorHandlers_ResponseOutOfRange proves that a service value too large
// for its wire field becomes codes.Internal instead of a wrapped integer.
func TestVectorHandlers_ResponseOutOfRange(t *testing.T) {
	ctx := tenantctx.WithSystem(context.Background())
	hugeStats := func(mutate func(*service.GetIndexStatsResponse)) *service.GetIndexStatsResponse {
		stats := &service.GetIndexStatsResponse{
			Namespace:         "ns1",
			SearchPerformance: &service.IndexPerformanceMetrics{},
		}
		mutate(stats)
		return stats
	}

	tests := []struct {
		name  string
		field string
		setup func(m *testutil.MockQuicBaaS)
		call  func(srv *Server) error
	}{
		{
			name:  "DeleteNamespace vectors deleted",
			field: "vectors_deleted",
			setup: func(m *testutil.MockQuicBaaS) {
				m.On("DeleteNamespace", mock.Anything, mock.Anything).
					Return(&service.DeleteNamespaceResponse{VectorsDeleted: math.MaxUint64}, nil)
			},
			call: func(srv *Server) error {
				_, err := srv.DeleteNamespace(ctx, &pb.DeleteNamespaceRequest{Namespace: "ns1"})
				return err
			},
		},
		{
			name:  "ListNamespaces vector count",
			field: "vector_count",
			setup: func(m *testutil.MockQuicBaaS) {
				m.On("ListNamespaces", mock.Anything, mock.Anything).
					Return(&service.ListNamespacesResponse{
						Namespaces: []*service.NamespaceInfo{{Namespace: "ns1", VectorCount: math.MaxUint64}},
					}, nil)
			},
			call: func(srv *Server) error {
				_, err := srv.ListNamespaces(ctx, &pb.ListNamespacesRequest{})
				return err
			},
		},
		{
			name:  "ListNamespaces total count",
			field: "total_count",
			setup: func(m *testutil.MockQuicBaaS) {
				m.On("ListNamespaces", mock.Anything, mock.Anything).
					Return(&service.ListNamespacesResponse{TotalCount: math.MaxInt32 + 1}, nil)
			},
			call: func(srv *Server) error {
				_, err := srv.ListNamespaces(ctx, &pb.ListNamespacesRequest{})
				return err
			},
		},
		{
			name:  "GetNamespaceInfo dimensions",
			field: "dimensions",
			setup: func(m *testutil.MockQuicBaaS) {
				m.On("GetNamespaceInfo", mock.Anything, mock.Anything).
					Return(&service.GetNamespaceInfoResponse{Namespace: "ns1", Dimension: math.MaxUint32}, nil)
			},
			call: func(srv *Server) error {
				_, err := srv.GetNamespaceInfo(ctx, &pb.GetNamespaceInfoRequest{Namespace: "ns1"})
				return err
			},
		},
		{
			name:  "GetNamespaceInfo vector count",
			field: "vector_count",
			setup: func(m *testutil.MockQuicBaaS) {
				m.On("GetNamespaceInfo", mock.Anything, mock.Anything).
					Return(&service.GetNamespaceInfoResponse{Namespace: "ns1", VectorCount: math.MaxUint64}, nil)
			},
			call: func(srv *Server) error {
				_, err := srv.GetNamespaceInfo(ctx, &pb.GetNamespaceInfoRequest{Namespace: "ns1"})
				return err
			},
		},
		{
			name:  "SearchSimilar rank",
			field: "rank",
			setup: func(m *testutil.MockQuicBaaS) {
				m.On("SearchSimilar", mock.Anything, mock.Anything).
					Return(&service.SearchSimilarResponse{
						Results: []*service.VectorSearchResult{{ID: 1, Rank: math.MaxInt32 + 1}},
					}, nil)
			},
			call: func(srv *Server) error {
				return srv.SearchSimilar(&pb.SearchSimilarRequest{
					Namespace:   "ns1",
					QueryVector: []float32{1.0},
					K:           10,
				}, NewMockSearchSimilarStream(ctx))
			},
		},
		{
			name:  "ListVectors total count",
			field: "total_count",
			setup: func(m *testutil.MockQuicBaaS) {
				m.On("GetNamespaceInfo", mock.Anything, mock.Anything).
					Return(&service.GetNamespaceInfoResponse{Namespace: "ns1", Dimension: 1}, nil)
				m.On("SearchByMetadata", mock.Anything, mock.Anything).
					Return(&service.SearchByMetadataResponse{TotalCount: math.MaxUint64}, nil)
			},
			call: func(srv *Server) error {
				return srv.ListVectors(&pb.ListVectorsRequest{Namespace: "ns1", PageSize: 10}, NewMockListVectorsStream(ctx))
			},
		},
		{
			name:  "GetVectorStats namespace index bytes",
			field: "index_bytes",
			setup: func(m *testutil.MockQuicBaaS) {
				m.On("GetIndexStats", mock.Anything, mock.Anything).
					Return(hugeStats(func(s *service.GetIndexStatsResponse) { s.IndexSizeBytes = math.MaxUint64 }), nil)
			},
			call: func(srv *Server) error {
				_, err := srv.GetVectorStats(ctx, &pb.GetVectorStatsRequest{Namespace: "ns1"})
				return err
			},
		},
		{
			name:  "GetVectorStats namespace search count",
			field: "search_count",
			setup: func(m *testutil.MockQuicBaaS) {
				m.On("GetIndexStats", mock.Anything, mock.Anything).
					Return(hugeStats(func(s *service.GetIndexStatsResponse) {
						s.SearchPerformance.TotalSearches = math.MaxUint64
					}), nil)
			},
			call: func(srv *Server) error {
				_, err := srv.GetVectorStats(ctx, &pb.GetVectorStatsRequest{Namespace: "ns1"})
				return err
			},
		},
		{
			name:  "GetVectorStats cluster-wide vector count",
			field: "vector_count",
			setup: func(m *testutil.MockQuicBaaS) {
				m.On("ListNamespaces", mock.Anything, mock.Anything).
					Return(&service.ListNamespacesResponse{Namespaces: []*service.NamespaceInfo{{Namespace: "ns1"}}}, nil)
				m.On("GetIndexStats", mock.Anything, mock.Anything).
					Return(hugeStats(func(s *service.GetIndexStatsResponse) { s.VectorCount = math.MaxUint64 }), nil)
			},
			call: func(srv *Server) error {
				_, err := srv.GetVectorStats(ctx, &pb.GetVectorStatsRequest{})
				return err
			},
		},
		{
			name:  "GetIndexProgress vectors indexed",
			field: "vectors_indexed",
			setup: func(m *testutil.MockQuicBaaS) {
				m.On("GetRebuildProgress", mock.Anything, mock.Anything).
					Return(&service.GetRebuildProgressResponse{JobID: "job-1", VectorsProcessed: math.MaxUint64}, nil)
			},
			call: func(srv *Server) error {
				_, err := srv.GetIndexProgress(ctx, &pb.GetIndexProgressRequest{OperationId: "job-1"})
				return err
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv, mockSvc := newMockTestServer(t)
			tt.setup(mockSvc)
			requireRangeStatus(t, tt.call(srv), codes.Internal, tt.field)
		})
	}
}

// TestNamespaceStatsToProto_NilSearchPerformance proves that index stats
// without search metrics convert cleanly instead of dereferencing nil.
func TestNamespaceStatsToProto_NilSearchPerformance(t *testing.T) {
	nsStats, err := namespaceStatsToProto(&service.GetIndexStatsResponse{
		Namespace:      "ns1",
		VectorCount:    7,
		IndexSizeBytes: 64,
	})

	require.NoError(t, err)
	assert.Equal(t, int64(7), nsStats.VectorCount)
	assert.Equal(t, int64(64), nsStats.IndexBytes)
	assert.Zero(t, nsStats.SearchCount)
}
