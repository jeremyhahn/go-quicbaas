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

	pb "github.com/jeremyhahn/go-quicbaas/api/proto"
)

// VectorDBWrapper wraps the Server to provide VectorDB gRPC service.
// This is needed because VectorDB has its own Health method which is different
// than QuicBaaS.Health, and Go doesn't support method overloading.
type VectorDBWrapper struct {
	// Embed UnimplementedVectorDBServer for forward compatibility
	pb.UnimplementedVectorDBServer
	// Reference to the main server for vector operations
	server *Server
}

// NewVectorDBWrapper creates a new VectorDB wrapper around the main server.
func NewVectorDBWrapper(server *Server) *VectorDBWrapper {
	return &VectorDBWrapper{
		server: server,
	}
}

// Health returns the health status of the vector database.
// Checks if the underlying service is healthy and returns vector-specific status.
func (w *VectorDBWrapper) Health(ctx context.Context, req *pb.VectorHealthRequest) (*pb.VectorHealthResponse, error) {
	// Check if the underlying service is healthy
	healthy := true
	status := pb.VectorHealthStatus_VECTOR_HEALTH_STATUS_HEALTHY

	// The service implements a Health method that can be used to check overall health.
	// If service is nil or not initialized, report as unhealthy.
	if w.server == nil || w.server.service == nil {
		healthy = false
		status = pb.VectorHealthStatus_VECTOR_HEALTH_STATUS_UNHEALTHY
	}

	// For detailed health checking, the service layer would need to expose
	// vector-specific health metrics such as:
	// - Index status (building, ready, needs rebuild)
	// - Memory usage for vector indices
	// - Query latency percentiles
	// - Number of namespaces and vectors
	// This can be implemented when the service interface supports VectorStats.

	if !healthy {
		return &pb.VectorHealthResponse{
			Status:  status,
			NodeId:  w.server.nodeID,
			Version: w.server.version,
		}, nil
	}

	return &pb.VectorHealthResponse{
		Status:  status,
		NodeId:  w.server.nodeID,
		Version: w.server.version,
	}, nil
}

// CreateNamespace delegates to the main server.
func (w *VectorDBWrapper) CreateNamespace(ctx context.Context, req *pb.CreateNamespaceRequest) (*pb.CreateNamespaceResponse, error) {
	return w.server.CreateNamespace(ctx, req)
}

// UpdateNamespace delegates to the main server.
func (w *VectorDBWrapper) UpdateNamespace(ctx context.Context, req *pb.UpdateNamespaceRequest) (*pb.UpdateNamespaceResponse, error) {
	return w.server.UpdateNamespace(ctx, req)
}

// DeleteNamespace delegates to the main server.
func (w *VectorDBWrapper) DeleteNamespace(ctx context.Context, req *pb.DeleteNamespaceRequest) (*pb.DeleteNamespaceResponse, error) {
	return w.server.DeleteNamespace(ctx, req)
}

// ListNamespaces delegates to the main server.
func (w *VectorDBWrapper) ListNamespaces(ctx context.Context, req *pb.ListNamespacesRequest) (*pb.ListNamespacesResponse, error) {
	return w.server.ListNamespaces(ctx, req)
}

// GetNamespaceInfo delegates to the main server.
func (w *VectorDBWrapper) GetNamespaceInfo(ctx context.Context, req *pb.GetNamespaceInfoRequest) (*pb.GetNamespaceInfoResponse, error) {
	return w.server.GetNamespaceInfo(ctx, req)
}

// UpsertVector delegates to the main server.
func (w *VectorDBWrapper) UpsertVector(ctx context.Context, req *pb.UpsertVectorRequest) (*pb.UpsertVectorResponse, error) {
	return w.server.UpsertVector(ctx, req)
}

// UpdateMetadata delegates to the main server.
func (w *VectorDBWrapper) UpdateMetadata(ctx context.Context, req *pb.UpdateMetadataRequest) (*pb.UpdateMetadataResponse, error) {
	return w.server.UpdateMetadata(ctx, req)
}

// GetVector delegates to the main server.
func (w *VectorDBWrapper) GetVector(ctx context.Context, req *pb.GetVectorRequest) (*pb.GetVectorResponse, error) {
	return w.server.GetVector(ctx, req)
}

// DeleteVector delegates to the main server.
func (w *VectorDBWrapper) DeleteVector(ctx context.Context, req *pb.DeleteVectorRequest) (*pb.DeleteVectorResponse, error) {
	return w.server.DeleteVector(ctx, req)
}

// BatchDeleteVectors delegates to the main server.
func (w *VectorDBWrapper) BatchDeleteVectors(ctx context.Context, req *pb.BatchDeleteVectorsRequest) (*pb.BatchDeleteVectorsResponse, error) {
	return w.server.BatchDeleteVectors(ctx, req)
}

// BatchUpsertVectors delegates to the main server.
func (w *VectorDBWrapper) BatchUpsertVectors(req *pb.BatchUpsertVectorsRequest, stream pb.VectorDB_BatchUpsertVectorsServer) error {
	return w.server.BatchUpsertVectors(req, stream)
}

// SearchSimilar delegates to the main server.
func (w *VectorDBWrapper) SearchSimilar(req *pb.SearchSimilarRequest, stream pb.VectorDB_SearchSimilarServer) error {
	return w.server.SearchSimilar(req, stream)
}

// SearchByID delegates to the main server.
func (w *VectorDBWrapper) SearchByID(req *pb.SearchByIDRequest, stream pb.VectorDB_SearchByIDServer) error {
	return w.server.SearchByID(req, stream)
}

// ListVectors delegates to the main server.
func (w *VectorDBWrapper) ListVectors(req *pb.ListVectorsRequest, stream pb.VectorDB_ListVectorsServer) error {
	return w.server.ListVectors(req, stream)
}

// GetVectorStats delegates to the main server.
func (w *VectorDBWrapper) GetVectorStats(ctx context.Context, req *pb.GetVectorStatsRequest) (*pb.GetVectorStatsResponse, error) {
	return w.server.GetVectorStats(ctx, req)
}

// RebuildIndex delegates to the main server.
func (w *VectorDBWrapper) RebuildIndex(ctx context.Context, req *pb.RebuildIndexRequest) (*pb.RebuildIndexResponse, error) {
	return w.server.RebuildIndex(ctx, req)
}

// GetIndexProgress delegates to the main server.
func (w *VectorDBWrapper) GetIndexProgress(ctx context.Context, req *pb.GetIndexProgressRequest) (*pb.GetIndexProgressResponse, error) {
	return w.server.GetIndexProgress(ctx, req)
}

// VectorAdminWrapper wraps the Server to provide VectorAdmin gRPC service.
type VectorAdminWrapper struct {
	pb.UnimplementedVectorAdminServer
	server *Server
}

// NewVectorAdminWrapper creates a new VectorAdmin wrapper around the main server.
func NewVectorAdminWrapper(server *Server) *VectorAdminWrapper {
	return &VectorAdminWrapper{
		server: server,
	}
}

// GetClusterStats delegates to the main server.
func (w *VectorAdminWrapper) GetClusterStats(ctx context.Context, req *pb.GetClusterStatsRequest) (*pb.GetClusterStatsResponse, error) {
	return w.server.GetClusterStats(ctx, req)
}

// CompactNamespace delegates to the main server.
func (w *VectorAdminWrapper) CompactNamespace(ctx context.Context, req *pb.CompactNamespaceRequest) (*pb.CompactNamespaceResponse, error) {
	return w.server.CompactNamespace(ctx, req)
}

// GetCompactionProgress delegates to the main server.
func (w *VectorAdminWrapper) GetCompactionProgress(ctx context.Context, req *pb.GetCompactionProgressRequest) (*pb.GetCompactionProgressResponse, error) {
	return w.server.GetCompactionProgress(ctx, req)
}

// ExportVectors delegates to the main server.
func (w *VectorAdminWrapper) ExportVectors(req *pb.ExportVectorsRequest, stream pb.VectorAdmin_ExportVectorsServer) error {
	return w.server.ExportVectors(req, stream)
}

// ImportVectors delegates to the main server.
func (w *VectorAdminWrapper) ImportVectors(stream pb.VectorAdmin_ImportVectorsServer) error {
	return w.server.ImportVectors(stream)
}
