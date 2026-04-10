//go:build rest

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

// Package rest provides interfaces for vector database services.
package rest

import (
	"context"

	pb "github.com/jeremyhahn/go-quicbaas/api/proto"
)

// VectorDBService defines the interface for vector database operations.
type VectorDBService interface {
	// CRUD Operations
	UpsertVector(ctx context.Context, req *pb.UpsertVectorRequest) (*pb.UpsertVectorResponse, error)
	BatchUpsertVectors(ctx context.Context, req *pb.BatchUpsertVectorsRequest) (pb.VectorDB_BatchUpsertVectorsClient, error)
	GetVector(ctx context.Context, req *pb.GetVectorRequest) (*pb.GetVectorResponse, error)
	DeleteVector(ctx context.Context, req *pb.DeleteVectorRequest) (*pb.DeleteVectorResponse, error)
	BatchDeleteVectors(ctx context.Context, req *pb.BatchDeleteVectorsRequest) (*pb.BatchDeleteVectorsResponse, error)

	// Search Operations
	SearchSimilar(ctx context.Context, req *pb.SearchSimilarRequest) (pb.VectorDB_SearchSimilarClient, error)
	SearchByID(ctx context.Context, req *pb.SearchByIDRequest) (pb.VectorDB_SearchByIDClient, error)

	// List Operations
	ListVectors(ctx context.Context, req *pb.ListVectorsRequest) (pb.VectorDB_ListVectorsClient, error)
	ListNamespaces(ctx context.Context, req *pb.ListNamespacesRequest) (*pb.ListNamespacesResponse, error)

	// Namespace Management
	CreateNamespace(ctx context.Context, req *pb.CreateNamespaceRequest) (*pb.CreateNamespaceResponse, error)
	UpdateNamespace(ctx context.Context, req *pb.UpdateNamespaceRequest) (*pb.UpdateNamespaceResponse, error)
	DeleteNamespace(ctx context.Context, req *pb.DeleteNamespaceRequest) (*pb.DeleteNamespaceResponse, error)
	GetNamespaceInfo(ctx context.Context, req *pb.GetNamespaceInfoRequest) (*pb.GetNamespaceInfoResponse, error)

	// Statistics and Health
	GetVectorStats(ctx context.Context, req *pb.GetVectorStatsRequest) (*pb.GetVectorStatsResponse, error)
	Health(ctx context.Context, req *pb.VectorHealthRequest) (*pb.VectorHealthResponse, error)

	// Index Operations
	RebuildIndex(ctx context.Context, req *pb.RebuildIndexRequest) (*pb.RebuildIndexResponse, error)
	GetIndexProgress(ctx context.Context, req *pb.GetIndexProgressRequest) (*pb.GetIndexProgressResponse, error)
}

// VectorAdminService defines the interface for vector database admin operations.
type VectorAdminService interface {
	// GetClusterStats returns cluster-wide vector database statistics
	GetClusterStats(ctx context.Context, req *pb.GetClusterStatsRequest) (*pb.GetClusterStatsResponse, error)

	// CompactNamespace performs compaction on a namespace
	CompactNamespace(ctx context.Context, req *pb.CompactNamespaceRequest) (*pb.CompactNamespaceResponse, error)

	// GetCompactionProgress returns the progress of an ongoing compaction
	GetCompactionProgress(ctx context.Context, req *pb.GetCompactionProgressRequest) (*pb.GetCompactionProgressResponse, error)

	// ExportVectors exports vectors from a namespace
	ExportVectors(ctx context.Context, req *pb.ExportVectorsRequest) (pb.VectorAdmin_ExportVectorsClient, error)

	// ImportVectors imports vectors into a namespace
	ImportVectors(ctx context.Context) (pb.VectorAdmin_ImportVectorsClient, error)
}
