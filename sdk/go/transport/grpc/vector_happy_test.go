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

	"google.golang.org/grpc"
	"google.golang.org/protobuf/types/known/timestamppb"

	pb "github.com/jeremyhahn/go-quicbaas/api/proto"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/client/vector"
)

// fakeVectorDBServer implements the VectorDB service.
type fakeVectorDBServer struct {
	pb.UnimplementedVectorDBServer
	fail bool
}

func (f *fakeVectorDBServer) e() error { return failErr(f.fail) }

func vecTime() *timestamppb.Timestamp { return timestamppb.New(timestamppb.Now().AsTime()) }

func sampleVectorEntity() *pb.VectorEntity {
	return &pb.VectorEntity{Id: "v1", Namespace: "ns", Vector: []float32{0.1, 0.2}, Metadata: map[string]string{"k": "v"}, Version: 1}
}

func (f *fakeVectorDBServer) CreateNamespace(context.Context, *pb.CreateNamespaceRequest) (*pb.CreateNamespaceResponse, error) {
	if err := f.e(); err != nil {
		return nil, err
	}
	return &pb.CreateNamespaceResponse{Namespace: "ns", Timestamp: vecTime()}, nil
}
func (f *fakeVectorDBServer) DeleteNamespace(context.Context, *pb.DeleteNamespaceRequest) (*pb.DeleteNamespaceResponse, error) {
	if err := f.e(); err != nil {
		return nil, err
	}
	return &pb.DeleteNamespaceResponse{Deleted: true, VectorsDeleted: 5, Timestamp: vecTime()}, nil
}
func (f *fakeVectorDBServer) ListNamespaces(context.Context, *pb.ListNamespacesRequest) (*pb.ListNamespacesResponse, error) {
	if err := f.e(); err != nil {
		return nil, err
	}
	return &pb.ListNamespacesResponse{Namespaces: []*pb.NamespaceInfo{{Config: &pb.NamespaceConfig{Namespace: "ns", Dimensions: 2}}}, TotalCount: 1}, nil
}
func (f *fakeVectorDBServer) GetNamespaceInfo(context.Context, *pb.GetNamespaceInfoRequest) (*pb.GetNamespaceInfoResponse, error) {
	if err := f.e(); err != nil {
		return nil, err
	}
	return &pb.GetNamespaceInfoResponse{Info: &pb.NamespaceInfo{Config: &pb.NamespaceConfig{Namespace: "ns", Dimensions: 2}}}, nil
}
func (f *fakeVectorDBServer) UpsertVector(context.Context, *pb.UpsertVectorRequest) (*pb.UpsertVectorResponse, error) {
	if err := f.e(); err != nil {
		return nil, err
	}
	return &pb.UpsertVectorResponse{Id: "v1", Version: 1, Timestamp: vecTime()}, nil
}
func (f *fakeVectorDBServer) BatchUpsertVectors(_ *pb.BatchUpsertVectorsRequest, stream grpc.ServerStreamingServer[pb.BatchUpsertVectorsResponse]) error {
	if err := f.e(); err != nil {
		return err
	}
	return stream.Send(&pb.BatchUpsertVectorsResponse{VectorsSucceeded: 1, VectorsFailed: 1, FailedIds: []string{"bad"}, ErrorMessage: "oops", IsFinal: true, Timestamp: vecTime()})
}
func (f *fakeVectorDBServer) GetVector(context.Context, *pb.GetVectorRequest) (*pb.GetVectorResponse, error) {
	if err := f.e(); err != nil {
		return nil, err
	}
	return &pb.GetVectorResponse{Vector: sampleVectorEntity(), Found: true}, nil
}
func (f *fakeVectorDBServer) DeleteVector(context.Context, *pb.DeleteVectorRequest) (*pb.DeleteVectorResponse, error) {
	if err := f.e(); err != nil {
		return nil, err
	}
	return &pb.DeleteVectorResponse{Deleted: true, Timestamp: vecTime()}, nil
}
func (f *fakeVectorDBServer) BatchDeleteVectors(context.Context, *pb.BatchDeleteVectorsRequest) (*pb.BatchDeleteVectorsResponse, error) {
	if err := f.e(); err != nil {
		return nil, err
	}
	return &pb.BatchDeleteVectorsResponse{DeletedCount: 2, Timestamp: vecTime()}, nil
}
func (f *fakeVectorDBServer) ListVectors(_ *pb.ListVectorsRequest, stream grpc.ServerStreamingServer[pb.ListVectorsResponse]) error {
	if err := f.e(); err != nil {
		return err
	}
	return stream.Send(&pb.ListVectorsResponse{Vectors: []*pb.VectorEntity{sampleVectorEntity()}, NextPageToken: "n", TotalCount: 1, IsFinal: true})
}
func (f *fakeVectorDBServer) UpdateMetadata(context.Context, *pb.UpdateMetadataRequest) (*pb.UpdateMetadataResponse, error) {
	if err := f.e(); err != nil {
		return nil, err
	}
	return &pb.UpdateMetadataResponse{Success: true, Version: 2, Timestamp: vecTime()}, nil
}
func (f *fakeVectorDBServer) SearchSimilar(_ *pb.SearchSimilarRequest, stream grpc.ServerStreamingServer[pb.SearchSimilarResponse]) error {
	if err := f.e(); err != nil {
		return err
	}
	return stream.Send(&pb.SearchSimilarResponse{Results: []*pb.SimilarityResult{{Vector: sampleVectorEntity(), Score: 0.9}}, TotalCandidates: 1, SearchTimeMs: 3, IsFinal: true})
}
func (f *fakeVectorDBServer) SearchByID(_ *pb.SearchByIDRequest, stream grpc.ServerStreamingServer[pb.SearchByIDResponse]) error {
	if err := f.e(); err != nil {
		return err
	}
	return stream.Send(&pb.SearchByIDResponse{Results: []*pb.SimilarityResult{{Vector: sampleVectorEntity(), Score: 0.8}}, TotalCandidates: 1, SearchTimeMs: 2, IsFinal: true})
}
func (f *fakeVectorDBServer) HybridSearch(_ *pb.HybridSearchRequest, stream grpc.ServerStreamingServer[pb.HybridSearchResponse]) error {
	if err := f.e(); err != nil {
		return err
	}
	return stream.Send(&pb.HybridSearchResponse{Results: []*pb.SimilarityResult{{Vector: sampleVectorEntity(), Score: 0.7}}, TotalCandidates: 1, TotalFiltered: 1, RerankingApplied: true, SearchTimeMs: 4, IsFinal: true})
}
func (f *fakeVectorDBServer) SearchByMetadata(_ *pb.SearchByMetadataRequest, stream grpc.ServerStreamingServer[pb.SearchByMetadataResponse]) error {
	if err := f.e(); err != nil {
		return err
	}
	return stream.Send(&pb.SearchByMetadataResponse{Results: []*pb.VectorEntity{sampleVectorEntity()}, TotalCount: 1, NextCursor: "c", SearchTimeMs: 5, IsFinal: true})
}
func (f *fakeVectorDBServer) RebuildIndex(context.Context, *pb.RebuildIndexRequest) (*pb.RebuildIndexResponse, error) {
	if err := f.e(); err != nil {
		return nil, err
	}
	return &pb.RebuildIndexResponse{OperationId: "op1", EstimatedDurationMs: 100, Started: vecTime()}, nil
}
func (f *fakeVectorDBServer) GetIndexProgress(context.Context, *pb.GetIndexProgressRequest) (*pb.GetIndexProgressResponse, error) {
	if err := f.e(); err != nil {
		return nil, err
	}
	return &pb.GetIndexProgressResponse{OperationId: "op1", Namespace: "ns", Status: "running", PercentComplete: 50, VectorsIndexed: 5, TotalVectors: 10, ElapsedTimeMs: 5}, nil
}
func (f *fakeVectorDBServer) GetVectorStats(context.Context, *pb.GetVectorStatsRequest) (*pb.GetVectorStatsResponse, error) {
	if err := f.e(); err != nil {
		return nil, err
	}
	return &pb.GetVectorStatsResponse{TotalVectors: 100, TotalIndexBytes: 1024, TotalStorageBytes: 2048}, nil
}

// fakeVectorAdminServer implements the VectorAdmin service.
type fakeVectorAdminServer struct {
	pb.UnimplementedVectorAdminServer
	fail bool
}

func (f *fakeVectorAdminServer) e() error { return failErr(f.fail) }

func (f *fakeVectorAdminServer) ExportVectors(_ *pb.ExportVectorsRequest, stream grpc.ServerStreamingServer[pb.ExportVectorsResponse]) error {
	if err := f.e(); err != nil {
		return err
	}
	return stream.Send(&pb.ExportVectorsResponse{BatchData: []byte("data"), VectorsExported: 1, IsFinal: true})
}
func (f *fakeVectorAdminServer) ImportVectors(stream grpc.ClientStreamingServer[pb.ImportVectorsRequest, pb.ImportVectorsResponse]) error {
	if err := f.e(); err != nil {
		return err
	}
	for {
		_, err := stream.Recv()
		if err != nil {
			break
		}
	}
	return stream.SendAndClose(&pb.ImportVectorsResponse{VectorsImported: 2, VectorsSkipped: 0, VectorsFailed: 0, DurationMs: 10, Timestamp: vecTime()})
}
func (f *fakeVectorAdminServer) CompactNamespace(context.Context, *pb.CompactNamespaceRequest) (*pb.CompactNamespaceResponse, error) {
	if err := f.e(); err != nil {
		return nil, err
	}
	return &pb.CompactNamespaceResponse{OperationId: "op1", Started: vecTime()}, nil
}
func (f *fakeVectorAdminServer) GetCompactionProgress(context.Context, *pb.GetCompactionProgressRequest) (*pb.GetCompactionProgressResponse, error) {
	if err := f.e(); err != nil {
		return nil, err
	}
	return &pb.GetCompactionProgressResponse{OperationId: "op1", Namespace: "ns", Status: "running", PercentComplete: 30, BytesCompacted: 100, TotalBytes: 300, SpaceReclaimedBytes: 50, ElapsedTimeMs: 5}, nil
}

func newVectorHandlerWithServer(t *testing.T, fail bool) *vectorHandler {
	srv := newHandlerServer(t, func(s *grpc.Server) {
		pb.RegisterVectorDBServer(s, &fakeVectorDBServer{fail: fail})
		pb.RegisterVectorAdminServer(s, &fakeVectorAdminServer{fail: fail})
	})
	return newVectorHandler(srv.dial(t))
}

type vectorCase struct {
	name   string
	method string
	req    interface{}
}

var vectorCases = []vectorCase{
	{"CreateNamespace", "CreateNamespace", &vector.CreateNamespaceRequest{Config: &vector.NamespaceConfig{Namespace: "ns", Dimensions: 2, IndexType: vector.IndexTypeHNSW}}},
	{"DeleteNamespace", "DeleteNamespace", &vector.DeleteNamespaceRequest{Namespace: "ns"}},
	{"ListNamespaces", "ListNamespaces", &vector.ListNamespacesRequest{Prefix: "n", PageSize: 10}},
	{"GetNamespaceInfo", "GetNamespaceInfo", &vector.GetNamespaceInfoRequest{Namespace: "ns", IncludeStats: true}},
	{"Upsert", "Upsert", &vector.UpsertRequest{Vector: &vector.VectorEntity{ID: "v1", Namespace: "ns", Vector: []float32{0.1, 0.2}}}},
	{"BatchUpsert", "BatchUpsert", &vector.BatchUpsertRequest{Vectors: []*vector.VectorEntity{{ID: "v1", Namespace: "ns", Vector: []float32{0.1}}}}},
	{"Get", "Get", &vector.GetRequest{Namespace: "ns", ID: "v1", IncludeVector: true, IncludeMetadata: true}},
	{"Delete", "Delete", &vector.DeleteRequest{Namespace: "ns", ID: "v1"}},
	{"BatchDelete", "BatchDelete", &vector.BatchDeleteRequest{Namespace: "ns", IDs: []string{"v1"}}},
	{"List", "List", &vector.ListVectorsRequest{Namespace: "ns", PageSize: 10}},
	{"UpdateMetadata", "UpdateMetadata", &vector.UpdateMetadataRequest{Namespace: "ns", ID: "v1", Metadata: map[string]string{"k": "v"}, Merge: true}},
	{"SearchSimilar", "SearchSimilar", &vector.SearchSimilarRequest{Namespace: "ns", QueryVector: []float32{0.1, 0.2}, K: 5}},
	{"SearchByID", "SearchByID", &vector.SearchByIDRequest{Namespace: "ns", ID: "v1", K: 5}},
	{"HybridSearch", "HybridSearch", &vector.HybridSearchRequest{Namespace: "ns", QueryVector: []float32{0.1, 0.2}, K: 5, MetadataFilters: []*vector.MetadataFilter{{Key: "k", Operator: "eq", Value: "v"}}}},
	{"SearchByMetadata", "SearchByMetadata", &vector.SearchByMetadataRequest{Namespace: "ns", MetadataFilters: []*vector.MetadataFilter{{Key: "k", Operator: "eq", Value: "v"}}, Limit: 10}},
	{"RebuildIndex", "RebuildIndex", &vector.RebuildIndexRequest{Namespace: "ns", Async: true}},
	{"GetIndexProgress", "GetIndexProgress", &vector.GetIndexProgressRequest{OperationID: "op1", Namespace: "ns"}},
	{"Export", "Export", &vector.ExportRequest{Namespace: "ns", Format: "json", BatchSize: 10}},
	{"Import", "Import", []*vector.ImportRequest{{Namespace: "ns", Format: "json", BatchData: []byte("d")}}},
	{"Compact", "Compact", &vector.CompactRequest{Namespace: "ns", Async: true}},
	{"GetCompactionProgress", "GetCompactionProgress", &vector.GetCompactionProgressRequest{OperationID: "op1", Namespace: "ns"}},
	{"GetStats", "GetStats", &vector.GetStatsRequest{Namespace: "ns"}},
}

func TestVectorHandler_AllMethods_Success(t *testing.T) {
	h := newVectorHandlerWithServer(t, false)
	for _, tc := range vectorCases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := h.handleRequest(context.Background(), "quicbaas.Vector", tc.method, tc.req); err != nil {
				t.Fatalf("%s: unexpected error: %v", tc.name, err)
			}
		})
	}
}

func TestVectorHandler_AllMethods_ServerError(t *testing.T) {
	h := newVectorHandlerWithServer(t, true)
	for _, tc := range vectorCases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := h.handleRequest(context.Background(), "quicbaas.Vector", tc.method, tc.req); err == nil {
				t.Fatalf("%s: expected error from failing server", tc.name)
			}
		})
	}
}

func TestVectorHandler_UnsupportedMethod(t *testing.T) {
	h := newVectorHandler(nil)
	if _, err := h.handleRequest(context.Background(), "quicbaas.Vector", "Nope", nil); err == nil {
		t.Fatal("expected unsupported-method error")
	}
}

func TestVectorHandler_InvalidRequestTypes(t *testing.T) {
	h := newVectorHandler(nil)
	for _, tc := range vectorCases {
		if _, err := h.handleRequest(context.Background(), "quicbaas.Vector", tc.method, 99); err == nil {
			t.Errorf("%s: expected invalid-request-type error", tc.method)
		}
	}
}
