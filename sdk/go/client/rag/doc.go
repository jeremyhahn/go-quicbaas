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
//
// RAG combines embedding generation, vector similarity search, and context building
// to provide relevant context for language model queries.
//
// # Operations
//
// Query performs an all-in-one RAG operation:
//  1. Generates an embedding for the query text using Ollama
//  2. Searches for similar vectors in the specified namespace
//  3. Builds formatted context from the search results
//
// BuildContext builds context from user-provided search results, useful when
// you want to use pre-computed embeddings or external search.
//
// # Example Usage
//
//	client := rag.NewClient(transport)
//
//	// Perform RAG query
//	resp, err := client.Query(ctx, &rag.QueryRequest{
//	    Query:            "How do I configure logging?",
//	    Namespace:        "docs",
//	    TopK:             10,
//	    MaxTokens:        4000,
//	    MinScore:         0.7,
//	    Template:         "markdown",
//	    IncludeCitations: true,
//	})
//
//	// Build context from custom results
//	resp, err := client.BuildContext(ctx, &rag.BuildContextRequest{
//	    Results: []rag.ResultInput{
//	        {Content: "Example content", Score: 0.95, Metadata: map[string]string{"file_path": "/path/to/file"}},
//	    },
//	    MaxTokens:          4000,
//	    Template:           "markdown",
//	    IncludeCitations:   true,
//	    TruncationStrategy: "smart",
//	})
package rag
