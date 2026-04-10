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

//go:build integration && rag

package rag

import (
	"context"
	"fmt"
	"math"
	"sync"
	"time"

	"github.com/jeremyhahn/go-quicbaas/pkg/config"
	"github.com/jeremyhahn/go-quicbaas/pkg/embedding"
	rag "github.com/jeremyhahn/go-quicbaas/pkg/rag"
	"github.com/jeremyhahn/go-quicbaas/pkg/rag/chunking"
	"github.com/jeremyhahn/go-quicbaas/pkg/rag/pipeline"
)

// cosineSimilarity calculates the cosine similarity between two float32 slices.
func cosineSimilarity(a, b []float32) float64 {
	if len(a) != len(b) || len(a) == 0 {
		return 0
	}
	var dotProduct, normA, normB float64
	for i := range a {
		dotProduct += float64(a[i]) * float64(b[i])
		normA += float64(a[i]) * float64(a[i])
		normB += float64(b[i]) * float64(b[i])
	}
	if normA == 0 || normB == 0 {
		return 0
	}
	return dotProduct / (math.Sqrt(normA) * math.Sqrt(normB))
}

// euclideanDistance calculates the Euclidean distance between two vectors.
func euclideanDistance(a, b []float32) float64 {
	if len(a) != len(b) || len(a) == 0 {
		return math.MaxFloat64
	}
	var sum float64
	for i := range a {
		diff := float64(a[i] - b[i])
		sum += diff * diff
	}
	return math.Sqrt(sum)
}

// normalizeVector normalizes a vector to unit length.
func normalizeVector(v []float32) []float32 {
	var norm float64
	for _, val := range v {
		norm += float64(val) * float64(val)
	}
	norm = math.Sqrt(norm)
	if norm == 0 {
		return v
	}
	result := make([]float32, len(v))
	for i, val := range v {
		result[i] = float32(float64(val) / norm)
	}
	return result
}

// createTestRAGConfig creates a RAG configuration for testing.
func createTestRAGConfig(useOllama bool) config.RAGConfig {
	provider := "mock"
	apiURL := ""
	if useOllama && env.ollamaAvailable {
		provider = "ollama"
		apiURL = env.ollamaURL
	}

	return config.RAGConfig{
		Enabled: true,
		Embedding: config.EmbeddingConfig{
			Provider:   provider,
			APIURL:     apiURL,
			Model:      env.ollamaModel,
			Dimensions: env.embedDimensions,
			Timeout:    120, // Increased for integration tests with real Ollama
		},
		Vector: config.RAGVectorConfig{
			Dimensions:   env.embedDimensions,
			Metric:       "cosine",
			NumCentroids: 10,
			NProbe:       3,
		},
		Pipeline: config.PipelineConfig{
			EnableCircuitBreaker: false,
			EnableMetrics:        true,
			MaxConcurrentQueries: 10,
			Timeout:              120 * time.Second, // Increased for integration tests
			DefaultTopK:          10,
			DefaultMinScore:      0.5,
			DefaultMaxTokens:     4000,
			DefaultNamespace:     "test",
			EnableHybrid:         true,
			EnableRerank:         false,
		},
		Ranking: config.RankingConfig{
			Strategy:      "hybrid",
			VectorWeight:  0.7,
			KeywordWeight: 0.3,
			BM25K1:        1.2,
			BM25B:         0.75,
		},
		Context: config.ContextConfig{
			MaxTokens:          4000,
			TruncationStrategy: "smart",
			IncludeCitations:   true,
			DefaultTemplate:    "markdown",
			MaxResultsPerChunk: 20,
			TokenMultiplier:    1.3,
		},
	}
}

// createTestEmbeddingProvider creates an embedding provider for testing.
func createTestEmbeddingProvider(useOllama bool) (embedding.Provider, error) {
	if useOllama && env.ollamaAvailable {
		cfg := getTestEmbeddingConfig()
		return embedding.NewOpenAIProvider(cfg)
	}
	return newMockEmbeddingProvider(env.embedDimensions), nil
}

// createTestRAGSystem creates a RAG system for testing.
func createTestRAGSystem(useOllama bool) (*rag.System, error) {
	cfg := createTestRAGConfig(useOllama)
	return rag.NewSystem(cfg)
}

// mockEmbeddingProvider implements embedding.Provider for testing without Ollama.
type mockEmbeddingProvider struct {
	dimensions int
	mu         sync.Mutex
	embeddings map[string][]float32
}

func newMockEmbeddingProvider(dimensions int) *mockEmbeddingProvider {
	return &mockEmbeddingProvider{
		dimensions: dimensions,
		embeddings: make(map[string][]float32),
	}
}

func (m *mockEmbeddingProvider) Embed(ctx context.Context, text string) ([]float32, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Return cached if exists
	if emb, ok := m.embeddings[text]; ok {
		return emb, nil
	}

	// Generate deterministic embedding based on text hash
	emb := make([]float32, m.dimensions)
	hash := 0
	for _, c := range text {
		hash = (hash*31 + int(c)) % 65536
	}

	for i := 0; i < m.dimensions; i++ {
		emb[i] = float32((hash+i*17)%256) / 255.0
	}

	// Normalize the vector
	emb = normalizeVector(emb)
	m.embeddings[text] = emb
	return emb, nil
}

func (m *mockEmbeddingProvider) EmbedBatch(ctx context.Context, texts []string) ([][]float32, error) {
	results := make([][]float32, len(texts))
	for i, text := range texts {
		emb, err := m.Embed(ctx, text)
		if err != nil {
			return nil, err
		}
		results[i] = emb
	}
	return results, nil
}

func (m *mockEmbeddingProvider) Dimensions() int {
	return m.dimensions
}

func (m *mockEmbeddingProvider) MaxTokens() int {
	return 8192
}

func (m *mockEmbeddingProvider) Name() string {
	return "mock"
}

func (m *mockEmbeddingProvider) Close() error {
	return nil
}

// testDocument represents a test document.
type testDocument struct {
	ID       string
	Content  string
	Metadata map[string]interface{}
}

// createTestDocuments generates test documents for RAG testing.
func createTestDocuments() []testDocument {
	return []testDocument{
		{
			ID:      "doc1",
			Content: "Machine learning is a subset of artificial intelligence that enables systems to learn and improve from experience without being explicitly programmed.",
			Metadata: map[string]interface{}{
				"category": "AI",
				"topic":    "machine learning",
			},
		},
		{
			ID:      "doc2",
			Content: "Deep learning uses neural networks with multiple layers to model complex patterns in data, achieving state-of-the-art results in many domains.",
			Metadata: map[string]interface{}{
				"category": "AI",
				"topic":    "deep learning",
			},
		},
		{
			ID:      "doc3",
			Content: "Natural language processing enables computers to understand, interpret, and generate human language through various algorithms and techniques.",
			Metadata: map[string]interface{}{
				"category": "AI",
				"topic":    "NLP",
			},
		},
		{
			ID:      "doc4",
			Content: "Computer vision is a field of AI that trains computers to interpret and understand visual information from images and videos.",
			Metadata: map[string]interface{}{
				"category": "AI",
				"topic":    "computer vision",
			},
		},
		{
			ID:      "doc5",
			Content: "Reinforcement learning is a type of machine learning where agents learn optimal behaviors through trial and error interactions with an environment.",
			Metadata: map[string]interface{}{
				"category": "AI",
				"topic":    "reinforcement learning",
			},
		},
		{
			ID:      "doc6",
			Content: "Database systems provide efficient storage, retrieval, and management of structured data through query languages like SQL.",
			Metadata: map[string]interface{}{
				"category": "databases",
				"topic":    "SQL",
			},
		},
		{
			ID:      "doc7",
			Content: "NoSQL databases are designed for specific data models and have flexible schemas for building modern applications with large-scale distributed systems.",
			Metadata: map[string]interface{}{
				"category": "databases",
				"topic":    "NoSQL",
			},
		},
		{
			ID:      "doc8",
			Content: "Vector databases are specialized systems optimized for storing and querying high-dimensional vectors used in machine learning and similarity search.",
			Metadata: map[string]interface{}{
				"category": "databases",
				"topic":    "vector databases",
			},
		},
		{
			ID:      "doc9",
			Content: "Kubernetes is an open-source container orchestration platform that automates deployment, scaling, and management of containerized applications.",
			Metadata: map[string]interface{}{
				"category": "infrastructure",
				"topic":    "kubernetes",
			},
		},
		{
			ID:      "doc10",
			Content: "Microservices architecture structures an application as a collection of loosely coupled services that can be developed, deployed, and scaled independently.",
			Metadata: map[string]interface{}{
				"category": "architecture",
				"topic":    "microservices",
			},
		},
	}
}

// createLargeTestDocument generates a large document for testing.
func createLargeTestDocument() testDocument {
	content := `# Introduction to Machine Learning

Machine learning is a branch of artificial intelligence (AI) that enables computers to learn from data and improve their performance on tasks without being explicitly programmed for every scenario.

## Types of Machine Learning

### Supervised Learning
In supervised learning, models are trained using labeled data. The algorithm learns to map inputs to known outputs. Common applications include:
- Classification tasks (spam detection, image recognition)
- Regression tasks (price prediction, demand forecasting)

### Unsupervised Learning
Unsupervised learning works with unlabeled data to discover hidden patterns and structures. Key techniques include:
- Clustering (customer segmentation, anomaly detection)
- Dimensionality reduction (PCA, t-SNE)

### Reinforcement Learning
Reinforcement learning involves an agent learning to make decisions by interacting with an environment and receiving rewards or penalties for its actions.

## Deep Learning

Deep learning is a subset of machine learning that uses neural networks with multiple layers (deep neural networks) to model complex patterns in data.

### Neural Network Architecture
- Input layer: Receives raw data
- Hidden layers: Extract features and patterns
- Output layer: Produces predictions

### Common Deep Learning Models
1. Convolutional Neural Networks (CNNs) for image processing
2. Recurrent Neural Networks (RNNs) for sequential data
3. Transformers for natural language processing
4. Generative Adversarial Networks (GANs) for data generation

## Applications

Machine learning has numerous applications across industries:
- Healthcare: Disease diagnosis, drug discovery
- Finance: Fraud detection, algorithmic trading
- Transportation: Autonomous vehicles, route optimization
- Retail: Recommendation systems, inventory management
- Manufacturing: Quality control, predictive maintenance

## Best Practices

When implementing machine learning solutions:
1. Start with clean, high-quality data
2. Choose appropriate algorithms for your problem
3. Use proper evaluation metrics
4. Implement cross-validation
5. Monitor model performance in production
6. Regularly retrain models with new data

## Conclusion

Machine learning continues to evolve rapidly, enabling new applications and capabilities across various domains. Understanding the fundamentals is essential for leveraging these technologies effectively.`

	return testDocument{
		ID:      "large-doc",
		Content: content,
		Metadata: map[string]interface{}{
			"category": "education",
			"topic":    "machine learning introduction",
			"type":     "tutorial",
		},
	}
}

// createMarkdownTestDocument creates a markdown document for testing.
func createMarkdownTestDocument() testDocument {
	return testDocument{
		ID: "markdown-doc",
		Content: `# API Reference Guide

## Overview

This document provides the API reference for our vector database system.

## Endpoints

### Vector Operations

#### POST /vectors

Create a new vector in the database.

**Request Body:**
` + "```json\n" + `{
  "id": "vec-001",
  "values": [0.1, 0.2, 0.3],
  "metadata": {"category": "test"}
}
` + "```\n" + `

**Response:**
` + "```json\n" + `{
  "success": true,
  "id": "vec-001"
}
` + "```\n" + `

#### GET /vectors/:id

Retrieve a vector by ID.

#### DELETE /vectors/:id

Delete a vector from the database.

### Search Operations

#### POST /search

Perform a similarity search.

**Parameters:**
- ` + "`topK`" + `: Number of results to return
- ` + "`minScore`" + `: Minimum similarity threshold
- ` + "`filter`" + `: Metadata filter expression

## Authentication

All API requests require authentication using Bearer tokens.

## Rate Limits

- 100 requests per minute for free tier
- 1000 requests per minute for paid tier
`,
		Metadata: map[string]interface{}{
			"category": "documentation",
			"format":   "markdown",
		},
	}
}

// createCodeTestDocument creates a code document for testing.
func createCodeTestDocument() testDocument {
	return testDocument{
		ID: "code-doc",
		Content: `package main

import (
	"context"
	"fmt"
	"log"
)

// VectorDB represents a vector database client.
type VectorDB struct {
	endpoint string
	apiKey   string
}

// NewVectorDB creates a new VectorDB client.
func NewVectorDB(endpoint, apiKey string) *VectorDB {
	return &VectorDB{
		endpoint: endpoint,
		apiKey:   apiKey,
	}
}

// Insert adds a vector to the database.
func (db *VectorDB) Insert(ctx context.Context, id string, vector []float32) error {
	if len(vector) == 0 {
		return fmt.Errorf("empty vector")
	}
	// Implementation details...
	log.Printf("Inserted vector %s with %d dimensions", id, len(vector))
	return nil
}

// Search performs a similarity search.
func (db *VectorDB) Search(ctx context.Context, query []float32, topK int) ([]Result, error) {
	if topK <= 0 {
		topK = 10
	}
	// Implementation details...
	return nil, nil
}

// Result represents a search result.
type Result struct {
	ID    string
	Score float32
}

func main() {
	db := NewVectorDB("localhost:8080", "api-key")
	ctx := context.Background()

	vector := []float32{0.1, 0.2, 0.3, 0.4}
	if err := db.Insert(ctx, "test-1", vector); err != nil {
		log.Fatal(err)
	}

	results, err := db.Search(ctx, vector, 5)
	if err != nil {
		log.Fatal(err)
	}

	for _, r := range results {
		fmt.Printf("ID: %s, Score: %.4f\n", r.ID, r.Score)
	}
}
`,
		Metadata: map[string]interface{}{
			"category": "code",
			"language": "go",
		},
	}
}

// verifyChunks validates chunking results.
func verifyChunks(chunks []chunking.Chunk, minChunks, maxChunks int) error {
	if len(chunks) < minChunks {
		return fmt.Errorf("%w: got %d chunks, expected at least %d", ErrInvalidResult, len(chunks), minChunks)
	}
	if maxChunks > 0 && len(chunks) > maxChunks {
		return fmt.Errorf("%w: got %d chunks, expected at most %d", ErrInvalidResult, len(chunks), maxChunks)
	}
	for i, chunk := range chunks {
		if len(chunk.Content) == 0 {
			return fmt.Errorf("%w: chunk %d has empty content", ErrInvalidResult, i)
		}
	}
	return nil
}

// verifyResults validates search results.
func verifyResults(results []pipeline.Result, minResults int, minScore float32) error {
	if len(results) < minResults {
		return fmt.Errorf("%w: got %d results, expected at least %d", ErrInvalidResult, len(results), minResults)
	}
	for i, result := range results {
		if result.Score < minScore {
			return fmt.Errorf("%w: result %d has score %.4f below threshold %.4f", ErrInvalidResult, i, result.Score, minScore)
		}
	}
	return nil
}

// verifyTopKOrdering validates that results are ordered by score descending.
func verifyTopKOrdering(results []pipeline.Result) error {
	for i := 1; i < len(results); i++ {
		if results[i].Score > results[i-1].Score {
			return fmt.Errorf("%w: results not ordered by score at index %d (%.4f > %.4f)",
				ErrInvalidResult, i, results[i].Score, results[i-1].Score)
		}
	}
	return nil
}
