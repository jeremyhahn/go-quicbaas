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

// Example: Vector Similarity Search
//
// This example demonstrates:
// - Connecting to QuicBaaS using the SDK with multiprotocol support
// - Upserting vectors with metadata into namespaces
// - Searching for similar vectors (k-nearest neighbors)
// - Hybrid search combining vector similarity and metadata filters
// - Deleting vectors from namespaces
// - Practical use cases for semantic search and recommendations
//
// Prerequisites:
// - A running QuicBaaS server (default: localhost:50051)
// - Server must be initialized and unsealed
//
// Usage:
//
//	# Run with default gRPC transport
//	go run main.go
//
//	# Run with REST transport
//	go run main.go -transport rest -address http://localhost:8080
//
//	# Run with QUIC transport
//	go run main.go -transport quic -address localhost:50052
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"math"
	"time"

	quicbaas "github.com/jeremyhahn/go-quicbaas/sdk/go"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/client/vector"
)

// Document represents sample document data for creating embeddings.
type Document struct {
	ID       string
	Title    string
	Category string
	Content  string
	Tags     []string
}

// sampleDocuments represents a small corpus of documents for demonstration.
var sampleDocuments = []Document{
	{
		ID:       "doc-001",
		Title:    "Introduction to Machine Learning",
		Category: "technology",
		Content:  "Machine learning is a subset of artificial intelligence...",
		Tags:     []string{"ml", "ai", "beginner"},
	},
	{
		ID:       "doc-002",
		Title:    "Deep Learning Fundamentals",
		Category: "technology",
		Content:  "Deep learning uses neural networks with many layers...",
		Tags:     []string{"dl", "ai", "neural-networks"},
	},
	{
		ID:       "doc-003",
		Title:    "Natural Language Processing",
		Category: "technology",
		Content:  "NLP enables computers to understand human language...",
		Tags:     []string{"nlp", "ai", "text"},
	},
	{
		ID:       "doc-004",
		Title:    "Computer Vision Applications",
		Category: "technology",
		Content:  "Computer vision allows machines to interpret images...",
		Tags:     []string{"cv", "ai", "images"},
	},
	{
		ID:       "doc-005",
		Title:    "Healthy Cooking Recipes",
		Category: "lifestyle",
		Content:  "Discover nutritious and delicious recipes for everyday...",
		Tags:     []string{"cooking", "health", "recipes"},
	},
	{
		ID:       "doc-006",
		Title:    "Home Fitness Workouts",
		Category: "lifestyle",
		Content:  "Stay fit with these effective home workout routines...",
		Tags:     []string{"fitness", "health", "exercise"},
	},
	{
		ID:       "doc-007",
		Title:    "Financial Planning Guide",
		Category: "finance",
		Content:  "Learn how to manage your finances effectively...",
		Tags:     []string{"money", "planning", "investment"},
	},
	{
		ID:       "doc-008",
		Title:    "Retirement Investment Strategies",
		Category: "finance",
		Content:  "Plan for a secure retirement with smart investments...",
		Tags:     []string{"retirement", "investment", "planning"},
	},
}

func main() {
	// Parse command-line flags
	transportType := flag.String("transport", "grpc", "Transport protocol (grpc, rest, quic)")
	address := flag.String("address", "localhost:50051", "Server address")
	flag.Parse()

	fmt.Println("=== QuicBaaS Vector Search Example ===")
	fmt.Printf("Transport: %s\n", *transportType)
	fmt.Printf("Address:   %s\n", *address)
	fmt.Println()

	// Create client with the specified transport
	var opts []quicbaas.Option
	opts = append(opts, quicbaas.WithTimeout(30*time.Second))
	// Select transport based on flag
	switch *transportType {
	case "rest":
		opts = append(opts, quicbaas.WithREST())
	case "quic":
		opts = append(opts, quicbaas.WithQUIC())
		// default is gRPC, no option needed
	}

	client, err := quicbaas.NewClient(*address, opts...)
	if err != nil {
		log.Fatalf("Failed to create client: %v", err)
	}
	defer client.Close()

	ctx := context.Background()

	// Namespace serves as our logical grouping for organizing vectors
	namespace := "documents"

	// Step 1: Upsert vectors into namespace
	// In a real application, vectors would come from an embedding model (e.g., OpenAI, Sentence Transformers)
	fmt.Println("Step 1: Upserting document vectors into namespace...")
	insertedIDs := upsertDocuments(ctx, client, namespace)
	fmt.Printf("  Upserted %d document vectors into namespace '%s'\n", len(insertedIDs), namespace)

	// Step 2: Search for similar vectors (semantic search)
	fmt.Println("\nStep 2: Searching for similar documents...")
	searchSimilarDocuments(ctx, client, namespace)

	// Step 3: Hybrid search - combine vector similarity with metadata filters
	fmt.Println("\nStep 3: Hybrid search with metadata filtering...")
	hybridSearch(ctx, client, namespace)

	// Step 4: Search with different query vectors
	fmt.Println("\nStep 4: Topic-specific searches...")
	topicSearch(ctx, client, namespace)

	// Step 5: Search by similarity to existing document
	// We find documents similar to a known document by using its vector
	fmt.Println("\nStep 5: Finding documents similar to a known document...")
	searchByDocumentSimilarity(ctx, client, namespace)

	// Step 6: Delete vectors (cleanup)
	fmt.Println("\nStep 6: Cleaning up - Deleting vectors...")
	deleteDocuments(ctx, client, namespace, insertedIDs)

	fmt.Println("\n=== Example Complete ===")
	fmt.Println("\nKey Takeaways:")
	fmt.Println("  1. Namespaces organize vectors by logical grouping (e.g., 'documents', 'products')")
	fmt.Println("  2. Each vector has an ID, embedding values, and optional metadata")
	fmt.Println("  3. Similarity search finds the k-nearest neighbors to a query vector")
	fmt.Println("  4. Hybrid search combines vector similarity with metadata filters")
	fmt.Println("  5. Metadata enables filtering by category, tags, or any custom attributes")
	fmt.Println("  6. The SDK supports multiple transports (gRPC, REST, QUIC)")
}

// upsertDocuments upserts sample documents as vectors into the namespace.
func upsertDocuments(ctx context.Context, client quicbaas.Client, namespace string) []string {
	var insertedIDs []string

	for _, doc := range sampleDocuments {
		// Generate a mock embedding vector for the document
		// In production, use an embedding model like OpenAI text-embedding-ada-002
		vectorData := generateMockEmbedding(doc.Content, 128)

		// Build metadata from document attributes
		// Note: The SDK uses map[string]string for metadata
		metadata := map[string]string{
			"title":    doc.Title,
			"category": doc.Category,
			"content":  doc.Content,
		}

		// Upsert the vector with metadata
		resp, err := client.Vector().Upsert(ctx, &vector.UpsertRequest{
			Vector: &vector.VectorEntity{
				ID:        doc.ID,
				Namespace: namespace,
				Vector:    vectorData,
				Metadata:  metadata,
			},
		})
		if err != nil {
			log.Printf("  Warning: Failed to upsert %s: %v", doc.ID, err)
			continue
		}

		insertedIDs = append(insertedIDs, resp.ID)
		fmt.Printf("  Upserted: %s - %s\n", resp.ID, doc.Title)
	}

	return insertedIDs
}

// searchSimilarDocuments performs a basic similarity search.
func searchSimilarDocuments(ctx context.Context, client quicbaas.Client, namespace string) {
	// Create a query vector representing "artificial intelligence concepts"
	// In production, embed the user's query using the same model used for documents
	queryVector := generateQueryEmbedding("artificial intelligence machine learning", 128)

	// Search for the top 5 most similar documents
	resp, err := client.Vector().SearchSimilar(ctx, &vector.SearchSimilarRequest{
		Namespace:       namespace,
		QueryVector:     queryVector,
		K:               5,
		IncludeMetadata: true,
	})
	if err != nil {
		log.Fatalf("Failed to search: %v", err)
	}

	fmt.Printf("  Query: 'artificial intelligence machine learning'\n")
	fmt.Printf("  Found %d similar documents:\n", len(resp.Results))
	for i, result := range resp.Results {
		title := "Unknown"
		if result.Vector != nil && result.Vector.Metadata != nil {
			if t, ok := result.Vector.Metadata["title"]; ok {
				title = t
			}
		}
		fmt.Printf("    %d. [Score: %.4f] %s (ID: %s)\n", i+1, result.Score, title, result.Vector.ID)
	}
}

// hybridSearch combines vector similarity with metadata filtering.
func hybridSearch(ctx context.Context, client quicbaas.Client, namespace string) {
	// Query vector for "healthy lifestyle tips"
	queryVector := generateQueryEmbedding("healthy lifestyle tips", 128)

	// Use HybridSearch to filter by category
	resp, err := client.Vector().HybridSearch(ctx, &vector.HybridSearchRequest{
		Namespace:   namespace,
		QueryVector: queryVector,
		K:           3,
		MetadataFilters: []*vector.MetadataFilter{
			{
				Key:      "category",
				Operator: vector.FilterOperatorEquals,
				Value:    "lifestyle",
			},
		},
		IncludeMetadata: true,
	})
	if err != nil {
		log.Fatalf("Failed to hybrid search: %v", err)
	}

	fmt.Printf("  Query: 'healthy lifestyle tips' (filtered by category: lifestyle)\n")
	fmt.Printf("  Found %d documents:\n", len(resp.Results))
	for i, result := range resp.Results {
		title := "Unknown"
		category := "Unknown"
		if result.Vector != nil && result.Vector.Metadata != nil {
			if t, ok := result.Vector.Metadata["title"]; ok {
				title = t
			}
			if c, ok := result.Vector.Metadata["category"]; ok {
				category = c
			}
		}
		fmt.Printf("    %d. [Score: %.4f] %s (Category: %s)\n", i+1, result.Score, title, category)
	}

	// Another hybrid search: finance documents about investment
	fmt.Println()
	queryVector = generateQueryEmbedding("investment strategies retirement", 128)

	resp, err = client.Vector().HybridSearch(ctx, &vector.HybridSearchRequest{
		Namespace:   namespace,
		QueryVector: queryVector,
		K:           3,
		MetadataFilters: []*vector.MetadataFilter{
			{
				Key:      "category",
				Operator: vector.FilterOperatorEquals,
				Value:    "finance",
			},
		},
		IncludeMetadata: true,
	})
	if err != nil {
		log.Fatalf("Failed to hybrid search: %v", err)
	}

	fmt.Printf("  Query: 'investment strategies retirement' (filtered by category: finance)\n")
	fmt.Printf("  Found %d documents:\n", len(resp.Results))
	for i, result := range resp.Results {
		title := "Unknown"
		if result.Vector != nil && result.Vector.Metadata != nil {
			if t, ok := result.Vector.Metadata["title"]; ok {
				title = t
			}
		}
		fmt.Printf("    %d. [Score: %.4f] %s\n", i+1, result.Score, title)
	}
}

// topicSearch performs searches for specific topics.
func topicSearch(ctx context.Context, client quicbaas.Client, namespace string) {
	topics := []struct {
		name  string
		query string
	}{
		{"Neural Networks", "deep learning neural networks layers"},
		{"Text Processing", "natural language processing text understanding"},
		{"Image Analysis", "computer vision image recognition"},
	}

	for _, topic := range topics {
		queryVector := generateQueryEmbedding(topic.query, 128)

		resp, err := client.Vector().HybridSearch(ctx, &vector.HybridSearchRequest{
			Namespace:   namespace,
			QueryVector: queryVector,
			K:           2,
			MetadataFilters: []*vector.MetadataFilter{
				{
					Key:      "category",
					Operator: vector.FilterOperatorEquals,
					Value:    "technology",
				},
			},
			IncludeMetadata: true,
		})
		if err != nil {
			log.Printf("  Warning: Search failed for %s: %v", topic.name, err)
			continue
		}

		fmt.Printf("  Topic: %s\n", topic.name)
		for _, result := range resp.Results {
			title := "Unknown"
			if result.Vector != nil && result.Vector.Metadata != nil {
				if t, ok := result.Vector.Metadata["title"]; ok {
					title = t
				}
			}
			fmt.Printf("    - [Score: %.4f] %s\n", result.Score, title)
		}
	}
}

// searchByDocumentSimilarity finds documents similar to an existing document.
func searchByDocumentSimilarity(ctx context.Context, client quicbaas.Client, namespace string) {
	// Use the same vector as doc-001 (Machine Learning introduction)
	// In production, you would retrieve the vector from the database first
	referenceDoc := sampleDocuments[0]
	referenceVector := generateMockEmbedding(referenceDoc.Content, 128)

	resp, err := client.Vector().SearchSimilar(ctx, &vector.SearchSimilarRequest{
		Namespace:       namespace,
		QueryVector:     referenceVector,
		K:               4, // Include the reference itself plus 3 similar
		IncludeMetadata: true,
	})
	if err != nil {
		log.Fatalf("Failed to search by document: %v", err)
	}

	fmt.Printf("  Reference document: %s\n", referenceDoc.Title)
	fmt.Printf("  Similar documents (excluding self):\n")
	for _, result := range resp.Results {
		// Skip the reference document itself
		if result.Vector.ID == referenceDoc.ID {
			continue
		}
		title := "Unknown"
		if result.Vector != nil && result.Vector.Metadata != nil {
			if t, ok := result.Vector.Metadata["title"]; ok {
				title = t
			}
		}
		fmt.Printf("    - [Score: %.4f] %s (ID: %s)\n", result.Score, title, result.Vector.ID)
	}
}

// deleteDocuments removes vectors from the namespace.
func deleteDocuments(ctx context.Context, client quicbaas.Client, namespace string, ids []string) {
	deletedCount := 0
	for _, id := range ids {
		resp, err := client.Vector().Delete(ctx, &vector.DeleteRequest{
			Namespace: namespace,
			ID:        id,
		})
		if err != nil {
			log.Printf("  Warning: Failed to delete %s: %v", id, err)
			continue
		}
		if resp.Deleted {
			deletedCount++
		}
	}
	fmt.Printf("  Deleted %d vectors from namespace '%s'\n", deletedCount, namespace)
}

// generateMockEmbedding creates a deterministic mock embedding vector for a text.
// In production, replace this with calls to an embedding API (OpenAI, Cohere, etc.).
func generateMockEmbedding(text string, dimensions int) []float32 {
	vectorData := make([]float32, dimensions)

	// Create a deterministic but varied embedding based on text content
	hash := simpleHash(text)

	for i := range dimensions {
		// Generate values between -1 and 1 based on hash and position
		seed := float64(hash) + float64(i)*0.1
		vectorData[i] = float32(math.Sin(seed) * 0.5)

		// Add some variation based on text length and position
		if len(text) > 0 {
			charInfluence := float64(text[i%len(text)]) / 255.0
			vectorData[i] += float32(charInfluence * 0.3)
		}
	}

	// Normalize the vector
	normalize(vectorData)
	return vectorData
}

// generateQueryEmbedding creates a query embedding.
// Uses the same method as document embedding for consistency.
func generateQueryEmbedding(query string, dimensions int) []float32 {
	return generateMockEmbedding(query, dimensions)
}

// simpleHash generates a simple hash value from text.
func simpleHash(text string) uint64 {
	var hash uint64 = 5381
	for _, c := range text {
		hash = ((hash << 5) + hash) + uint64(c)
	}
	return hash
}

// normalize normalizes a vector to unit length.
func normalize(vectorData []float32) {
	var sum float64
	for _, v := range vectorData {
		sum += float64(v * v)
	}
	if sum == 0 {
		return
	}
	magnitude := float32(math.Sqrt(sum))
	for i := range vectorData {
		vectorData[i] /= magnitude
	}
}
