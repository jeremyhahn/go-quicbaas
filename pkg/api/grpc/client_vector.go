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
	"errors"
	"io"

	pb "github.com/jeremyhahn/go-quicbaas/api/proto"
	api "github.com/jeremyhahn/go-quicbaas/pkg/api"
)

// ========================================
// Vector Namespace Operations
// ========================================

// VectorCreateNamespace creates a new vector namespace.
func (c *Client) VectorCreateNamespace(ctx context.Context, req *pb.CreateNamespaceRequest) (*pb.CreateNamespaceResponse, error) {
	if err := c.ensureConnected(ctx); err != nil {
		return nil, err
	}

	// Add timeout if not already set
	if _, hasDeadline := ctx.Deadline(); !hasDeadline {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, c.opts.timeout)
		defer cancel()
	}

	// Add metadata
	ctx = c.addMetadata(ctx)

	// Get vector client
	client, err := c.getVectorClient()
	if err != nil {
		return nil, err
	}

	// Execute with retry
	var resp *pb.CreateNamespaceResponse
	err = retryWithBackoff(
		ctx,
		c.opts.maxRetries,
		c.opts.retryBackoff,
		c.opts.maxRetryBackoff,
		func() error {
			var rpcErr error
			resp, rpcErr = client.CreateNamespace(ctx, req)
			return rpcErr
		},
	)
	if err != nil {
		return nil, convertGRPCError(err, "CreateNamespace")
	}

	return resp, nil
}

// VectorDeleteNamespace deletes a vector namespace.
func (c *Client) VectorDeleteNamespace(ctx context.Context, req *pb.DeleteNamespaceRequest) (*pb.DeleteNamespaceResponse, error) {
	if err := c.ensureConnected(ctx); err != nil {
		return nil, err
	}

	// Add timeout if not already set
	if _, hasDeadline := ctx.Deadline(); !hasDeadline {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, c.opts.timeout)
		defer cancel()
	}

	// Add metadata
	ctx = c.addMetadata(ctx)

	// Get vector client
	client, err := c.getVectorClient()
	if err != nil {
		return nil, err
	}

	// Execute with retry
	var resp *pb.DeleteNamespaceResponse
	err = retryWithBackoff(
		ctx,
		c.opts.maxRetries,
		c.opts.retryBackoff,
		c.opts.maxRetryBackoff,
		func() error {
			var rpcErr error
			resp, rpcErr = client.DeleteNamespace(ctx, req)
			return rpcErr
		},
	)
	if err != nil {
		return nil, convertGRPCError(err, "DeleteNamespace")
	}

	return resp, nil
}

// VectorListNamespaces lists all vector namespaces.
func (c *Client) VectorListNamespaces(ctx context.Context, req *pb.ListNamespacesRequest) (*pb.ListNamespacesResponse, error) {
	if err := c.ensureConnected(ctx); err != nil {
		return nil, err
	}

	// Add timeout if not already set
	if _, hasDeadline := ctx.Deadline(); !hasDeadline {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, c.opts.timeout)
		defer cancel()
	}

	// Add metadata
	ctx = c.addMetadata(ctx)

	// Get vector client
	client, err := c.getVectorClient()
	if err != nil {
		return nil, err
	}

	// Execute with retry
	var resp *pb.ListNamespacesResponse
	err = retryWithBackoff(
		ctx,
		c.opts.maxRetries,
		c.opts.retryBackoff,
		c.opts.maxRetryBackoff,
		func() error {
			var rpcErr error
			resp, rpcErr = client.ListNamespaces(ctx, req)
			return rpcErr
		},
	)
	if err != nil {
		return nil, convertGRPCError(err, "ListNamespaces")
	}

	return resp, nil
}

// VectorGetNamespaceInfo retrieves information about a vector namespace.
func (c *Client) VectorGetNamespaceInfo(ctx context.Context, req *pb.GetNamespaceInfoRequest) (*pb.GetNamespaceInfoResponse, error) {
	if err := c.ensureConnected(ctx); err != nil {
		return nil, err
	}

	// Add timeout if not already set
	if _, hasDeadline := ctx.Deadline(); !hasDeadline {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, c.opts.timeout)
		defer cancel()
	}

	// Add metadata
	ctx = c.addMetadata(ctx)

	// Get vector client
	client, err := c.getVectorClient()
	if err != nil {
		return nil, err
	}

	// Execute with retry
	var resp *pb.GetNamespaceInfoResponse
	err = retryWithBackoff(
		ctx,
		c.opts.maxRetries,
		c.opts.retryBackoff,
		c.opts.maxRetryBackoff,
		func() error {
			var rpcErr error
			resp, rpcErr = client.GetNamespaceInfo(ctx, req)
			return rpcErr
		},
	)
	if err != nil {
		return nil, convertGRPCError(err, "GetNamespaceInfo")
	}

	return resp, nil
}

// ========================================
// Vector CRUD Operations
// ========================================

// VectorUpsert creates or updates a single vector.
func (c *Client) VectorUpsert(ctx context.Context, req *pb.UpsertVectorRequest) (*pb.UpsertVectorResponse, error) {
	if err := c.ensureConnected(ctx); err != nil {
		return nil, err
	}

	// Add timeout if not already set
	if _, hasDeadline := ctx.Deadline(); !hasDeadline {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, c.opts.timeout)
		defer cancel()
	}

	// Add metadata
	ctx = c.addMetadata(ctx)

	// Get vector client
	client, err := c.getVectorClient()
	if err != nil {
		return nil, err
	}

	// Execute with retry
	var resp *pb.UpsertVectorResponse
	err = retryWithBackoff(
		ctx,
		c.opts.maxRetries,
		c.opts.retryBackoff,
		c.opts.maxRetryBackoff,
		func() error {
			var rpcErr error
			resp, rpcErr = client.UpsertVector(ctx, req)
			return rpcErr
		},
	)
	if err != nil {
		return nil, convertGRPCError(err, "UpsertVector")
	}

	return resp, nil
}

// VectorGet retrieves a vector by ID.
func (c *Client) VectorGet(ctx context.Context, req *pb.GetVectorRequest) (*pb.GetVectorResponse, error) {
	if err := c.ensureConnected(ctx); err != nil {
		return nil, err
	}

	// Add timeout if not already set
	if _, hasDeadline := ctx.Deadline(); !hasDeadline {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, c.opts.timeout)
		defer cancel()
	}

	// Add metadata
	ctx = c.addMetadata(ctx)

	// Get vector client
	client, err := c.getVectorClient()
	if err != nil {
		return nil, err
	}

	// Execute with retry
	var resp *pb.GetVectorResponse
	err = retryWithBackoff(
		ctx,
		c.opts.maxRetries,
		c.opts.retryBackoff,
		c.opts.maxRetryBackoff,
		func() error {
			var rpcErr error
			resp, rpcErr = client.GetVector(ctx, req)
			return rpcErr
		},
	)
	if err != nil {
		return nil, convertGRPCError(err, "GetVector")
	}

	return resp, nil
}

// VectorDelete deletes a vector by ID.
func (c *Client) VectorDelete(ctx context.Context, req *pb.DeleteVectorRequest) (*pb.DeleteVectorResponse, error) {
	if err := c.ensureConnected(ctx); err != nil {
		return nil, err
	}

	// Add timeout if not already set
	if _, hasDeadline := ctx.Deadline(); !hasDeadline {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, c.opts.timeout)
		defer cancel()
	}

	// Add metadata
	ctx = c.addMetadata(ctx)

	// Get vector client
	client, err := c.getVectorClient()
	if err != nil {
		return nil, err
	}

	// Execute with retry
	var resp *pb.DeleteVectorResponse
	err = retryWithBackoff(
		ctx,
		c.opts.maxRetries,
		c.opts.retryBackoff,
		c.opts.maxRetryBackoff,
		func() error {
			var rpcErr error
			resp, rpcErr = client.DeleteVector(ctx, req)
			return rpcErr
		},
	)
	if err != nil {
		return nil, convertGRPCError(err, "DeleteVector")
	}

	return resp, nil
}

// VectorBatchUpsert creates or updates multiple vectors.
func (c *Client) VectorBatchUpsert(ctx context.Context, req *pb.BatchUpsertVectorsRequest) ([]*pb.BatchUpsertVectorsResponse, error) {
	if err := c.ensureConnected(ctx); err != nil {
		return nil, err
	}

	// Add timeout if not already set
	if _, hasDeadline := ctx.Deadline(); !hasDeadline {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, c.opts.timeout)
		defer cancel()
	}

	// Add metadata
	ctx = c.addMetadata(ctx)

	// Get vector client
	client, err := c.getVectorClient()
	if err != nil {
		return nil, err
	}

	// Call streaming method
	stream, err := client.BatchUpsertVectors(ctx, req)
	if err != nil {
		return nil, convertGRPCError(err, "BatchUpsertVectors")
	}

	// Collect responses
	var responses []*pb.BatchUpsertVectorsResponse
	for {
		resp, err := stream.Recv()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, convertGRPCError(err, "BatchUpsertVectors.Recv")
		}
		responses = append(responses, resp)
		if resp.IsFinal {
			break
		}
	}

	return responses, nil
}

// VectorBatchDelete deletes multiple vectors by IDs.
func (c *Client) VectorBatchDelete(ctx context.Context, req *pb.BatchDeleteVectorsRequest) (*pb.BatchDeleteVectorsResponse, error) {
	if err := c.ensureConnected(ctx); err != nil {
		return nil, err
	}

	// Add timeout if not already set
	if _, hasDeadline := ctx.Deadline(); !hasDeadline {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, c.opts.timeout)
		defer cancel()
	}

	// Add metadata
	ctx = c.addMetadata(ctx)

	// Get vector client
	client, err := c.getVectorClient()
	if err != nil {
		return nil, err
	}

	// Execute with retry
	var resp *pb.BatchDeleteVectorsResponse
	err = retryWithBackoff(
		ctx,
		c.opts.maxRetries,
		c.opts.retryBackoff,
		c.opts.maxRetryBackoff,
		func() error {
			var rpcErr error
			resp, rpcErr = client.BatchDeleteVectors(ctx, req)
			return rpcErr
		},
	)
	if err != nil {
		return nil, convertGRPCError(err, "BatchDeleteVectors")
	}

	return resp, nil
}

// VectorUpdateMetadata updates only the metadata of an existing vector.
func (c *Client) VectorUpdateMetadata(ctx context.Context, req *pb.UpdateMetadataRequest) (*pb.UpdateMetadataResponse, error) {
	if err := c.ensureConnected(ctx); err != nil {
		return nil, err
	}

	// Add timeout if not already set
	if _, hasDeadline := ctx.Deadline(); !hasDeadline {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, c.opts.timeout)
		defer cancel()
	}

	// Add metadata
	ctx = c.addMetadata(ctx)

	// Get vector client
	client, err := c.getVectorClient()
	if err != nil {
		return nil, err
	}

	// Execute with retry
	var resp *pb.UpdateMetadataResponse
	err = retryWithBackoff(
		ctx,
		c.opts.maxRetries,
		c.opts.retryBackoff,
		c.opts.maxRetryBackoff,
		func() error {
			var rpcErr error
			resp, rpcErr = client.UpdateMetadata(ctx, req)
			return rpcErr
		},
	)
	if err != nil {
		return nil, convertGRPCError(err, "UpdateMetadata")
	}

	return resp, nil
}

// VectorHealth retrieves the health status of the vector database.
func (c *Client) VectorHealth(ctx context.Context, req *pb.VectorHealthRequest) (*pb.VectorHealthResponse, error) {
	if err := c.ensureConnected(ctx); err != nil {
		return nil, err
	}

	// Add timeout if not already set
	if _, hasDeadline := ctx.Deadline(); !hasDeadline {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, c.opts.timeout)
		defer cancel()
	}

	// Add metadata
	ctx = c.addMetadata(ctx)

	// Get vector client
	client, err := c.getVectorClient()
	if err != nil {
		return nil, err
	}

	// Execute with retry
	var resp *pb.VectorHealthResponse
	err = retryWithBackoff(
		ctx,
		c.opts.maxRetries,
		c.opts.retryBackoff,
		c.opts.maxRetryBackoff,
		func() error {
			var rpcErr error
			resp, rpcErr = client.Health(ctx, req)
			return rpcErr
		},
	)
	if err != nil {
		return nil, convertGRPCError(err, "VectorHealth")
	}

	return resp, nil
}

// VectorGetStats retrieves statistics for the vector database.
func (c *Client) VectorGetStats(ctx context.Context, req *pb.GetVectorStatsRequest) (*pb.GetVectorStatsResponse, error) {
	if err := c.ensureConnected(ctx); err != nil {
		return nil, err
	}

	// Add timeout if not already set
	if _, hasDeadline := ctx.Deadline(); !hasDeadline {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, c.opts.timeout)
		defer cancel()
	}

	// Add metadata
	ctx = c.addMetadata(ctx)

	// Get vector client
	client, err := c.getVectorClient()
	if err != nil {
		return nil, err
	}

	// Execute with retry
	var resp *pb.GetVectorStatsResponse
	err = retryWithBackoff(
		ctx,
		c.opts.maxRetries,
		c.opts.retryBackoff,
		c.opts.maxRetryBackoff,
		func() error {
			var rpcErr error
			resp, rpcErr = client.GetVectorStats(ctx, req)
			return rpcErr
		},
	)
	if err != nil {
		return nil, convertGRPCError(err, "GetVectorStats")
	}

	return resp, nil
}

// ========================================
// Vector Search Operations
// ========================================

// VectorSearchSimilar performs k-NN similarity search.
func (c *Client) VectorSearchSimilar(ctx context.Context, req *pb.SearchSimilarRequest) ([]*pb.SearchSimilarResponse, error) {
	if err := c.ensureConnected(ctx); err != nil {
		return nil, err
	}

	// Add timeout if not already set
	if _, hasDeadline := ctx.Deadline(); !hasDeadline {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, c.opts.timeout)
		defer cancel()
	}

	// Add metadata
	ctx = c.addMetadata(ctx)

	// Get vector client
	client, err := c.getVectorClient()
	if err != nil {
		return nil, err
	}

	// Call streaming method
	stream, err := client.SearchSimilar(ctx, req)
	if err != nil {
		return nil, convertGRPCError(err, "SearchSimilar")
	}

	// Collect responses
	var responses []*pb.SearchSimilarResponse
	for {
		resp, err := stream.Recv()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, convertGRPCError(err, "SearchSimilar.Recv")
		}
		responses = append(responses, resp)
		if resp.IsFinal {
			break
		}
	}

	return responses, nil
}

// VectorSearchByID performs k-NN search using an existing vector.
func (c *Client) VectorSearchByID(ctx context.Context, req *pb.SearchByIDRequest) ([]*pb.SearchByIDResponse, error) {
	if err := c.ensureConnected(ctx); err != nil {
		return nil, err
	}

	// Add timeout if not already set
	if _, hasDeadline := ctx.Deadline(); !hasDeadline {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, c.opts.timeout)
		defer cancel()
	}

	// Add metadata
	ctx = c.addMetadata(ctx)

	// Get vector client
	client, err := c.getVectorClient()
	if err != nil {
		return nil, err
	}

	// Call streaming method
	stream, err := client.SearchByID(ctx, req)
	if err != nil {
		return nil, convertGRPCError(err, "SearchByID")
	}

	// Collect responses
	var responses []*pb.SearchByIDResponse
	for {
		resp, err := stream.Recv()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, convertGRPCError(err, "SearchByID.Recv")
		}
		responses = append(responses, resp)
		if resp.IsFinal {
			break
		}
	}

	return responses, nil
}

// VectorHybridSearch performs hybrid search combining similarity and metadata filtering.
func (c *Client) VectorHybridSearch(ctx context.Context, req *pb.HybridSearchRequest) ([]*pb.HybridSearchResponse, error) {
	if err := c.ensureConnected(ctx); err != nil {
		return nil, err
	}

	// Add timeout if not already set
	if _, hasDeadline := ctx.Deadline(); !hasDeadline {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, c.opts.timeout)
		defer cancel()
	}

	// Add metadata
	ctx = c.addMetadata(ctx)

	// Get vector client
	client, err := c.getVectorClient()
	if err != nil {
		return nil, err
	}

	// Call streaming method
	stream, err := client.HybridSearch(ctx, req)
	if err != nil {
		return nil, convertGRPCError(err, "HybridSearch")
	}

	// Collect responses
	var responses []*pb.HybridSearchResponse
	for {
		resp, err := stream.Recv()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, convertGRPCError(err, "HybridSearch.Recv")
		}
		responses = append(responses, resp)
		if resp.IsFinal {
			break
		}
	}

	return responses, nil
}

// VectorSearchByMetadata searches vectors by metadata without a query vector.
func (c *Client) VectorSearchByMetadata(ctx context.Context, req *pb.SearchByMetadataRequest) ([]*pb.SearchByMetadataResponse, error) {
	if err := c.ensureConnected(ctx); err != nil {
		return nil, err
	}

	// Add timeout if not already set
	if _, hasDeadline := ctx.Deadline(); !hasDeadline {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, c.opts.timeout)
		defer cancel()
	}

	// Add metadata
	ctx = c.addMetadata(ctx)

	// Get vector client
	client, err := c.getVectorClient()
	if err != nil {
		return nil, err
	}

	// Call streaming method
	stream, err := client.SearchByMetadata(ctx, req)
	if err != nil {
		return nil, convertGRPCError(err, "SearchByMetadata")
	}

	// Collect responses
	var responses []*pb.SearchByMetadataResponse
	for {
		resp, err := stream.Recv()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, convertGRPCError(err, "SearchByMetadata.Recv")
		}
		responses = append(responses, resp)
		if resp.IsFinal {
			break
		}
	}

	return responses, nil
}

// VectorList lists all vectors in a namespace.
func (c *Client) VectorList(ctx context.Context, req *pb.ListVectorsRequest) ([]*pb.ListVectorsResponse, error) {
	if err := c.ensureConnected(ctx); err != nil {
		return nil, err
	}

	// Add timeout if not already set
	if _, hasDeadline := ctx.Deadline(); !hasDeadline {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, c.opts.timeout)
		defer cancel()
	}

	// Add metadata
	ctx = c.addMetadata(ctx)

	// Get vector client
	client, err := c.getVectorClient()
	if err != nil {
		return nil, err
	}

	// Call streaming method
	stream, err := client.ListVectors(ctx, req)
	if err != nil {
		return nil, convertGRPCError(err, "ListVectors")
	}

	// Collect responses
	var responses []*pb.ListVectorsResponse
	for {
		resp, err := stream.Recv()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, convertGRPCError(err, "ListVectors.Recv")
		}
		responses = append(responses, resp)
		if resp.IsFinal {
			break
		}
	}

	return responses, nil
}

// ========================================
// Vector Index Operations
// ========================================

// VectorRebuildIndex rebuilds the vector index for a namespace.
func (c *Client) VectorRebuildIndex(ctx context.Context, req *pb.RebuildIndexRequest) (*pb.RebuildIndexResponse, error) {
	if err := c.ensureConnected(ctx); err != nil {
		return nil, err
	}

	// Add timeout if not already set
	if _, hasDeadline := ctx.Deadline(); !hasDeadline {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, c.opts.timeout)
		defer cancel()
	}

	// Add metadata
	ctx = c.addMetadata(ctx)

	// Get vector client
	client, err := c.getVectorClient()
	if err != nil {
		return nil, err
	}

	// Execute with retry
	var resp *pb.RebuildIndexResponse
	err = retryWithBackoff(
		ctx,
		c.opts.maxRetries,
		c.opts.retryBackoff,
		c.opts.maxRetryBackoff,
		func() error {
			var rpcErr error
			resp, rpcErr = client.RebuildIndex(ctx, req)
			return rpcErr
		},
	)
	if err != nil {
		return nil, convertGRPCError(err, "RebuildIndex")
	}

	return resp, nil
}

// VectorGetIndexProgress retrieves the progress of an index rebuild operation.
func (c *Client) VectorGetIndexProgress(ctx context.Context, req *pb.GetIndexProgressRequest) (*pb.GetIndexProgressResponse, error) {
	if err := c.ensureConnected(ctx); err != nil {
		return nil, err
	}

	// Add timeout if not already set
	if _, hasDeadline := ctx.Deadline(); !hasDeadline {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, c.opts.timeout)
		defer cancel()
	}

	// Add metadata
	ctx = c.addMetadata(ctx)

	// Get vector client
	client, err := c.getVectorClient()
	if err != nil {
		return nil, err
	}

	// Execute with retry
	var resp *pb.GetIndexProgressResponse
	err = retryWithBackoff(
		ctx,
		c.opts.maxRetries,
		c.opts.retryBackoff,
		c.opts.maxRetryBackoff,
		func() error {
			var rpcErr error
			resp, rpcErr = client.GetIndexProgress(ctx, req)
			return rpcErr
		},
	)
	if err != nil {
		return nil, convertGRPCError(err, "GetIndexProgress")
	}

	return resp, nil
}

// ========================================
// Admin Operations
// ========================================

// VectorExport exports vectors from a namespace for backup or migration.
func (c *Client) VectorExport(ctx context.Context, req *pb.ExportVectorsRequest) ([]*pb.ExportVectorsResponse, error) {
	if err := c.ensureConnected(ctx); err != nil {
		return nil, err
	}

	// Add metadata
	ctx = c.addMetadata(ctx)

	// Get vector admin client
	adminClient, err := c.getVectorAdminClient()
	if err != nil {
		return nil, err
	}

	// Call streaming export
	stream, err := adminClient.ExportVectors(ctx, req)
	if err != nil {
		return nil, convertGRPCError(err, "ExportVectors")
	}

	// Collect all responses
	var responses []*pb.ExportVectorsResponse
	for {
		resp, err := stream.Recv()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, convertGRPCError(err, "ExportVectors")
		}
		responses = append(responses, resp)
		if resp.IsFinal {
			break
		}
	}

	return responses, nil
}

// VectorImport imports vectors into a namespace from a backup or migration.
func (c *Client) VectorImport(ctx context.Context, batches []*pb.ImportVectorsRequest) (*pb.ImportVectorsResponse, error) {
	if err := c.ensureConnected(ctx); err != nil {
		return nil, err
	}

	// Add metadata
	ctx = c.addMetadata(ctx)

	// Get vector admin client
	adminClient, err := c.getVectorAdminClient()
	if err != nil {
		return nil, err
	}

	// Start streaming import
	stream, err := adminClient.ImportVectors(ctx)
	if err != nil {
		return nil, convertGRPCError(err, "ImportVectors")
	}

	// Send all batches
	for _, batch := range batches {
		if err := stream.Send(batch); err != nil {
			return nil, convertGRPCError(err, "ImportVectors")
		}
	}

	// Close and get response
	resp, err := stream.CloseAndRecv()
	if err != nil {
		return nil, convertGRPCError(err, "ImportVectors")
	}

	return resp, nil
}

// VectorCompact compacts a namespace to reclaim disk space.
func (c *Client) VectorCompact(ctx context.Context, req *pb.CompactNamespaceRequest) (*pb.CompactNamespaceResponse, error) {
	if err := c.ensureConnected(ctx); err != nil {
		return nil, err
	}

	// Add metadata
	ctx = c.addMetadata(ctx)

	// Get vector admin client
	adminClient, err := c.getVectorAdminClient()
	if err != nil {
		return nil, err
	}

	// Call the RPC
	resp, err := adminClient.CompactNamespace(ctx, req)
	if err != nil {
		return nil, convertGRPCError(err, "CompactNamespace")
	}

	return resp, nil
}

// VectorGetCompactionProgress retrieves the progress of a compaction operation.
func (c *Client) VectorGetCompactionProgress(ctx context.Context, req *pb.GetCompactionProgressRequest) (*pb.GetCompactionProgressResponse, error) {
	if err := c.ensureConnected(ctx); err != nil {
		return nil, err
	}

	// Add metadata
	ctx = c.addMetadata(ctx)

	// Get vector admin client
	adminClient, err := c.getVectorAdminClient()
	if err != nil {
		return nil, err
	}

	// Call the RPC
	resp, err := adminClient.GetCompactionProgress(ctx, req)
	if err != nil {
		return nil, convertGRPCError(err, "GetCompactionProgress")
	}

	return resp, nil
}

// ========================================
// Helper Functions
// ========================================

// getVectorClient returns the vector gRPC client.
func (c *Client) getVectorClient() (pb.VectorDBClient, error) {
	// If using connection pool, get a connection from pool
	if c.pool != nil {
		conn, err := c.pool.Get()
		if err != nil {
			return nil, err
		}
		return pb.NewVectorDBClient(conn.conn), nil
	}

	// Otherwise use the single connection
	if c.conn == nil {
		return nil, api.NewClientClosedError("grpc.getVectorClient")
	}

	return pb.NewVectorDBClient(c.conn), nil
}

// getVectorAdminClient returns the vector admin gRPC client.
func (c *Client) getVectorAdminClient() (pb.VectorAdminClient, error) {
	// If using connection pool, get a connection from pool
	if c.pool != nil {
		conn, err := c.pool.Get()
		if err != nil {
			return nil, err
		}
		return pb.NewVectorAdminClient(conn.conn), nil
	}

	// Otherwise use the single connection
	if c.conn == nil {
		return nil, api.NewClientClosedError("grpc.getVectorAdminClient")
	}

	return pb.NewVectorAdminClient(c.conn), nil
}
