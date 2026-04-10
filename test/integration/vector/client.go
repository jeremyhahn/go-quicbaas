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

package vector

import (
	"context"
)

// VectorTestClient is the unified interface for all vector protocol clients.
// All protocol implementations (gRPC, REST, QUIC, Unix, MCP) implement this interface.
type VectorTestClient interface {
	// Protocol returns the protocol name ("grpc", "rest", "quic", "unix", "mcp").
	Protocol() string

	// Namespace operations
	CreateNamespace(ctx context.Context, config *NamespaceConfig) error
	DeleteNamespace(ctx context.Context, namespace string, force bool) error
	ListNamespaces(ctx context.Context) ([]*NamespaceInfo, error)
	GetNamespaceInfo(ctx context.Context, namespace string) (*NamespaceInfo, error)

	// Vector CRUD operations
	UpsertVector(ctx context.Context, req *UpsertRequest) (*UpsertResponse, error)
	GetVector(ctx context.Context, namespace, id string) (*VectorEntity, error)
	DeleteVector(ctx context.Context, namespace, id string) error
	ListVectors(ctx context.Context, req *ListVectorsRequest) (*ListVectorsResponse, error)

	// Batch operations
	BatchUpsert(ctx context.Context, req *BatchUpsertRequest) (*BatchUpsertResponse, error)

	// Search operations
	SearchSimilar(ctx context.Context, req *SearchRequest) ([]*SimilarityResult, error)
	SearchByID(ctx context.Context, namespace, id string, k int) ([]*SimilarityResult, error)
	HybridSearch(ctx context.Context, req *HybridSearchRequest) ([]*SimilarityResult, error)

	// Index operations
	RebuildIndex(ctx context.Context, namespace string, async bool) (*RebuildIndexResponse, error)
	GetIndexStats(ctx context.Context, namespace string) (*IndexStats, error)

	// Connection management
	Close() error
}

// ClusterVectorTestClient extends VectorTestClient with cluster-specific operations.
// This is implemented by clients that can connect to multiple nodes.
type ClusterVectorTestClient interface {
	VectorTestClient

	// NodeCount returns the number of nodes this client is connected to.
	NodeCount() int

	// GetNodeClient returns a client for a specific node index.
	GetNodeClient(nodeIndex int) VectorTestClient

	// WaitForReplication waits for a vector to be replicated across all nodes.
	WaitForReplication(ctx context.Context, namespace, id string) error
}

// EmbeddingClient provides embedding generation capabilities.
// This is used by MCP client to generate real embeddings for testing.
type EmbeddingClient interface {
	// GenerateEmbedding generates an embedding vector for the given text.
	GenerateEmbedding(ctx context.Context, text string) ([]float32, error)

	// GetDimensions returns the dimension of vectors produced by this client.
	GetDimensions() int

	// Close closes the embedding client.
	Close() error
}

// MCPVectorTestClient extends VectorTestClient with MCP-specific operations.
type MCPVectorTestClient interface {
	VectorTestClient

	// GetEmbeddingClient returns the embedding client used for generating vectors.
	GetEmbeddingClient() EmbeddingClient

	// CallTool directly invokes an MCP tool by name.
	CallTool(ctx context.Context, toolName string, params map[string]interface{}) (interface{}, error)

	// ListTools returns available MCP tools.
	ListTools(ctx context.Context) ([]string, error)
}
