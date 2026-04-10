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

// Package rag provides the RAG (Retrieval-Augmented Generation) client for QuicBaaS.
// RAG operations combine embedding generation, vector search, and context building.
package rag

import (
	"context"

	"github.com/jeremyhahn/go-quicbaas/sdk/go/errors"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/transport"
)

const (
	serviceName = "quicbaas.RAG"
)

// Client provides RAG operations.
type Client interface {
	// Query performs an all-in-one RAG query: embed, search, build context.
	Query(ctx context.Context, req *QueryRequest) (*QueryResponse, error)

	// BuildContext builds context from user-provided search results.
	BuildContext(ctx context.Context, req *BuildContextRequest) (*BuildContextResponse, error)
}

// Citation represents a source citation in RAG responses.
type Citation struct {
	// Index is the citation index
	Index int

	// FilePath is the source file path
	FilePath string

	// StartLine is the starting line number
	StartLine int

	// EndLine is the ending line number
	EndLine int

	// Score is the similarity score
	Score float32
}

// ResultInput represents a single result for context building.
type ResultInput struct {
	// Content is the text content
	Content string

	// Score is the relevance score
	Score float32

	// Metadata contains additional information
	Metadata map[string]string
}

// QueryRequest is the request for RAG query operations.
type QueryRequest struct {
	// Query is the search query text
	Query string

	// Namespace is the vector namespace to search
	Namespace string

	// TopK is the number of results to retrieve
	TopK int

	// MaxTokens is the maximum tokens in generated context
	MaxTokens int

	// MinScore is the minimum similarity score threshold
	MinScore float32

	// Template is the context template format (markdown, json, xml, plain)
	Template string

	// IncludeCitations enables source citations in response
	IncludeCitations bool

	// Model is the embedding model to use (optional)
	Model string
}

// QueryResponse is the response for RAG query operations.
type QueryResponse struct {
	// Context is the generated context text
	Context string

	// Tokens is the token count of the context
	Tokens int

	// Truncated indicates if the context was truncated
	Truncated bool

	// ResultCount is the number of results used
	ResultCount int

	// Citations are the source citations (if requested)
	Citations []Citation
}

// BuildContextRequest is the request for building context from results.
type BuildContextRequest struct {
	// Results are the search results to build context from
	Results []ResultInput

	// MaxTokens is the maximum tokens in generated context
	MaxTokens int

	// Template is the context template format (markdown, json, xml, plain)
	Template string

	// IncludeCitations enables source citations in response
	IncludeCitations bool

	// TruncationStrategy is the truncation strategy (smart, hard, none)
	TruncationStrategy string
}

// BuildContextResponse is the response for context building operations.
type BuildContextResponse struct {
	// Context is the generated context text
	Context string

	// Tokens is the token count of the context
	Tokens int

	// Truncated indicates if the context was truncated
	Truncated bool

	// ResultCount is the number of results processed
	ResultCount int

	// Citations are the source citations (if requested)
	Citations []Citation
}

// client is the RAG client implementation.
type client struct {
	transport transport.Transport
}

// NewClient creates a new RAG client.
func NewClient(t transport.Transport) Client {
	if t == nil {
		return nil
	}
	return &client{
		transport: t,
	}
}

// Query performs an all-in-one RAG query.
func (c *client) Query(ctx context.Context, req *QueryRequest) (*QueryResponse, error) {
	if err := validateQueryRequest(req); err != nil {
		return nil, err
	}

	if ctx == nil {
		return nil, errors.NewInvalidInput("rag.Query", "context is required")
	}

	resp, err := c.transport.Request(ctx, serviceName, "Query", req)
	if err != nil {
		return nil, err
	}

	if resp == nil {
		return nil, errors.NewInternal("rag.Query", "nil response from server")
	}

	queryResp, ok := resp.(*QueryResponse)
	if !ok {
		return nil, errors.NewInternal("rag.Query", "invalid response type")
	}

	return queryResp, nil
}

// BuildContext builds context from user-provided search results.
func (c *client) BuildContext(ctx context.Context, req *BuildContextRequest) (*BuildContextResponse, error) {
	if err := validateBuildContextRequest(req); err != nil {
		return nil, err
	}

	if ctx == nil {
		return nil, errors.NewInvalidInput("rag.BuildContext", "context is required")
	}

	resp, err := c.transport.Request(ctx, serviceName, "BuildContext", req)
	if err != nil {
		return nil, err
	}

	if resp == nil {
		return nil, errors.NewInternal("rag.BuildContext", "nil response from server")
	}

	buildResp, ok := resp.(*BuildContextResponse)
	if !ok {
		return nil, errors.NewInternal("rag.BuildContext", "invalid response type")
	}

	return buildResp, nil
}

// validateQueryRequest validates the query request.
func validateQueryRequest(req *QueryRequest) error {
	if req == nil {
		return errors.NewInvalidInput("rag.Query", "request is required")
	}
	if req.Query == "" {
		return errors.NewInvalidInput("rag.Query", "query is required")
	}
	if req.Namespace == "" {
		return errors.NewInvalidInput("rag.Query", "namespace is required")
	}
	return nil
}

// validateBuildContextRequest validates the build context request.
func validateBuildContextRequest(req *BuildContextRequest) error {
	if req == nil {
		return errors.NewInvalidInput("rag.BuildContext", "request is required")
	}
	if len(req.Results) == 0 {
		return errors.NewInvalidInput("rag.BuildContext", "results is required and cannot be empty")
	}
	return nil
}
