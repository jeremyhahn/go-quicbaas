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
	api "github.com/jeremyhahn/go-quicbaas/pkg/api"
)

// ========================================
// Ollama Health Operations
// ========================================

// OllamaHealth checks the health status of the Ollama service.
func (c *Client) OllamaHealth(ctx context.Context, req *pb.OllamaHealthRequest) (*pb.OllamaHealthResponse, error) {
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

	// Get Ollama client
	client, err := c.getOllamaClient()
	if err != nil {
		return nil, err
	}

	// Execute with retry
	var resp *pb.OllamaHealthResponse
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
		return nil, convertGRPCError(err, "OllamaHealth")
	}

	return resp, nil
}

// ========================================
// Ollama Model Operations
// ========================================

// OllamaListModels returns the list of available Ollama models.
func (c *Client) OllamaListModels(ctx context.Context, req *pb.OllamaListModelsRequest) (*pb.OllamaListModelsResponse, error) {
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

	// Get Ollama client
	client, err := c.getOllamaClient()
	if err != nil {
		return nil, err
	}

	// Execute with retry
	var resp *pb.OllamaListModelsResponse
	err = retryWithBackoff(
		ctx,
		c.opts.maxRetries,
		c.opts.retryBackoff,
		c.opts.maxRetryBackoff,
		func() error {
			var rpcErr error
			resp, rpcErr = client.ListModels(ctx, req)
			return rpcErr
		},
	)
	if err != nil {
		return nil, convertGRPCError(err, "OllamaListModels")
	}

	return resp, nil
}

// OllamaGetModelInfo returns detailed information about a specific model.
func (c *Client) OllamaGetModelInfo(ctx context.Context, req *pb.OllamaGetModelInfoRequest) (*pb.OllamaGetModelInfoResponse, error) {
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

	// Get Ollama client
	client, err := c.getOllamaClient()
	if err != nil {
		return nil, err
	}

	// Execute with retry
	var resp *pb.OllamaGetModelInfoResponse
	err = retryWithBackoff(
		ctx,
		c.opts.maxRetries,
		c.opts.retryBackoff,
		c.opts.maxRetryBackoff,
		func() error {
			var rpcErr error
			resp, rpcErr = client.GetModelInfo(ctx, req)
			return rpcErr
		},
	)
	if err != nil {
		return nil, convertGRPCError(err, "OllamaGetModelInfo")
	}

	return resp, nil
}

// ========================================
// Ollama Embedding Operations
// ========================================

// OllamaEmbed generates an embedding for a single text using the default model.
func (c *Client) OllamaEmbed(ctx context.Context, req *pb.OllamaEmbedRequest) (*pb.OllamaEmbedResponse, error) {
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

	// Get Ollama client
	client, err := c.getOllamaClient()
	if err != nil {
		return nil, err
	}

	// Execute with retry
	var resp *pb.OllamaEmbedResponse
	err = retryWithBackoff(
		ctx,
		c.opts.maxRetries,
		c.opts.retryBackoff,
		c.opts.maxRetryBackoff,
		func() error {
			var rpcErr error
			resp, rpcErr = client.Embed(ctx, req)
			return rpcErr
		},
	)
	if err != nil {
		return nil, convertGRPCError(err, "OllamaEmbed")
	}

	return resp, nil
}

// OllamaEmbedWithModel generates an embedding using a specific model.
func (c *Client) OllamaEmbedWithModel(ctx context.Context, req *pb.OllamaEmbedWithModelRequest) (*pb.OllamaEmbedResponse, error) {
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

	// Get Ollama client
	client, err := c.getOllamaClient()
	if err != nil {
		return nil, err
	}

	// Execute with retry
	var resp *pb.OllamaEmbedResponse
	err = retryWithBackoff(
		ctx,
		c.opts.maxRetries,
		c.opts.retryBackoff,
		c.opts.maxRetryBackoff,
		func() error {
			var rpcErr error
			resp, rpcErr = client.EmbedWithModel(ctx, req)
			return rpcErr
		},
	)
	if err != nil {
		return nil, convertGRPCError(err, "OllamaEmbedWithModel")
	}

	return resp, nil
}

// OllamaEmbedBatch generates embeddings for multiple texts.
func (c *Client) OllamaEmbedBatch(ctx context.Context, req *pb.OllamaEmbedBatchRequest) (*pb.OllamaEmbedBatchResponse, error) {
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

	// Get Ollama client
	client, err := c.getOllamaClient()
	if err != nil {
		return nil, err
	}

	// Execute with retry
	var resp *pb.OllamaEmbedBatchResponse
	err = retryWithBackoff(
		ctx,
		c.opts.maxRetries,
		c.opts.retryBackoff,
		c.opts.maxRetryBackoff,
		func() error {
			var rpcErr error
			resp, rpcErr = client.EmbedBatch(ctx, req)
			return rpcErr
		},
	)
	if err != nil {
		return nil, convertGRPCError(err, "OllamaEmbedBatch")
	}

	return resp, nil
}

// ========================================
// Helper Functions
// ========================================

// getOllamaClient returns the Ollama gRPC client.
func (c *Client) getOllamaClient() (pb.OllamaServiceClient, error) {
	// If using connection pool, get a connection from pool
	if c.pool != nil {
		conn, err := c.pool.Get()
		if err != nil {
			return nil, err
		}
		return pb.NewOllamaServiceClient(conn.conn), nil
	}

	// Otherwise use the single connection
	if c.conn == nil {
		return nil, api.NewClientClosedError("grpc.getOllamaClient")
	}

	return pb.NewOllamaServiceClient(c.conn), nil
}
