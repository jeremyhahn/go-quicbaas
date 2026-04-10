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

// Package vector provides the vector similarity search client for the QuicBaaS SDK.
//
// The vector client supports storing and searching high-dimensional vectors
// for AI/ML applications, semantic search, and recommendation systems.
//
// # Inserting Vectors
//
//	client := vector.NewClient(transport)
//
//	// Insert a vector with metadata
//	resp, err := client.Insert(ctx, &vector.InsertRequest{
//	    Collection: "documents",
//	    ID:         "doc-1",
//	    Vector:     []float32{0.1, 0.2, 0.3, ...}, // embedding vector
//	    Metadata: map[string]interface{}{
//	        "title":    "Introduction to AI",
//	        "category": "technology",
//	    },
//	})
//
// # Similarity Search
//
//	// Find similar vectors
//	resp, err := client.Search(ctx, &vector.SearchRequest{
//	    Collection: "documents",
//	    Vector:     queryVector,
//	    TopK:       10,
//	    Filter: map[string]interface{}{
//	        "category": "technology",
//	    },
//	})
//	for _, result := range resp.Results {
//	    fmt.Printf("ID: %s, Score: %f\n", result.ID, result.Score)
//	}
//
// # Vector Operations
//
// The vector client supports:
//   - Insert: Add vectors to a collection
//   - Search: Find similar vectors using approximate nearest neighbor
//   - Delete: Remove vectors from a collection
//
// # Collections
//
// Vectors are organized into collections. Each collection can have its own
// configuration for indexing and distance metric.
package vector
