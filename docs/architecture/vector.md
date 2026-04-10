# Vector Database Architecture

Native vector similarity search built on QuicBaaS's distributed foundation.

## Overview

QuicBaaS Vector Database extends the core key-value store with semantic search capabilities optimized for AI/ML workflows including Retrieval-Augmented Generation (RAG).

**Key Features:**
- HNSW-based approximate nearest neighbor search
- Multiple distance metrics (L2, Cosine, Dot Product)
- Hybrid search (metadata filters + vector similarity)
- Distributed architecture with Raft consensus
- ACID guarantees for vector operations
- MCP integration for AI agents

## Architecture

### System Design

```
┌─────────────────────────────────────────────────┐
│         Client Layer                             │
│  gRPC │ REST │ CLI │ MCP Server                  │
└─────────────────────────────────────────────────┘
                    ↓
┌─────────────────────────────────────────────────┐
│         Query Planner                            │
│  • Route to shards                               │
│  • Optimize hybrid queries                       │
│  • Coordinate distributed search                 │
└─────────────────────────────────────────────────┘
                    ↓
┌─────────────────────────────────────────────────┐
│         Multi-Raft Consensus                     │
│  Shard 1 │ Shard 2 │ ... │ Shard N              │
└─────────────────────────────────────────────────┘
                    ↓
┌─────────────────────────────────────────────────┐
│         Storage & Indexing                       │
│  ┌──────────────┐  ┌─────────────────────────┐ │
│  │  PebbleDB    │  │  HNSW Index             │ │
│  │  • Vectors   │  │  • Multi-level graph    │ │
│  │  • Metadata  │  │  • Distance metrics     │ │
│  └──────────────┘  └─────────────────────────┘ │
└─────────────────────────────────────────────────┘
```

### Data Flow

**Write Path (Vector Upsert):**
1. Client sends vector + metadata via API
2. QuicBaaS service layer validates and forwards request
3. qrdb SDK sends upsert to the qrdb database engine
4. qrdb engine stores in its storage layer and updates the HNSW index
5. Raft consensus replicates across the qrdb cluster
6. Response to client

**Read Path (Similarity Search):**
1. Client sends query vector + k + filters
2. QuicBaaS service layer forwards to qrdb SDK vector client
3. qrdb engine executes k-NN search via HNSW across relevant shards
4. qrdb coordinator merges and re-ranks results
5. Return top-k globally similar vectors

## Vector Operations

Vector operations are delegated to the qrdb database engine via the qrdb SDK (`github.com/jeremyhahn/go-qrdb/sdk/go`). The qrdb engine manages Raft consensus, state machines, and HNSW indexing internally.

### Operations

**Write Operations (via qrdb SDK):**
- `UpsertVector` - Add or update vector
- `DeleteVector` - Remove vector
- `BatchUpsert` - Bulk insert vectors
- `CreateNamespace` - Create vector namespace
- `DeleteNamespace` - Delete namespace

**Query Operations (via qrdb SDK):**
- `GetVector` - Retrieve vector by ID
- `SearchSimilar` - k-NN search
- `ListVectors` - Scan namespace
- `GetStats` - Namespace statistics

### Consistency

- **Linearizable Writes** - All writes through qrdb Raft consensus
- **Snapshot Isolation** - Read from consistent snapshots
- **MVCC Support** - Version tracking for vectors
- **Quorum Reads** - Optional strong consistency for reads

## Storage Layer

### PebbleDB Key Encoding

```
Namespaces:     ns\x00{name}\x00               → NamespaceInfo
Vectors:        v\x00{ns}\x00{id:u64}\x00{ver:u64} → VectorValue
Metadata Index: m\x00{ns}\x00{field}\x00{value}\x00{id:u64} → ∅
ID Sequence:    s\x00{ns}\x00                   → uint64
Statistics:     stat\x00{ns}\x00{key}\x00       → value
```

**Design Benefits:**
- Prefix-based namespacing enables efficient scans
- MVCC versioning for transaction support
- Metadata indexes for hybrid search
- Atomic ID allocation per namespace

### Vector Value Format

```go
type VectorValue struct {
    ID        uint64            // Primary key
    Embedding []float32         // Dense vector (128-1536 dims)
    Metadata  map[string]string // Key-value pairs
    CreatedAt int64             // Unix nano timestamp
    UpdatedAt int64             // Unix nano timestamp
    Version   uint64            // MVCC version
}
```

**Encoding:** MessagePack → PebbleDB value
**Size:** 50-500 bytes (depending on dimensions + metadata)

## HNSW Index

Hierarchical Navigable Small World algorithm for fast approximate nearest neighbor search.

### Index Structure

```
Layer 3:    Entry ──→ ...          (sparse, long-range)
              ↓
Layer 2:    ○──○──○──○──...        (medium connectivity)
              ↓  ↓  ↓  ↓
Layer 1:    ○──○──○──○──○──...    (higher connectivity)
              ↓  ↓  ↓  ↓  ↓
Layer 0:    ●──●──●──●──●──...    (all vectors, high connectivity)
```

### Parameters

```yaml
hnsw:
  m: 16                    # Max connections per node
  ef_construction: 200     # Build-time search depth
  ef_search: 50            # Query-time search depth
```

**Performance Trade-offs:**
- Higher `M` → Better recall, more memory
- Higher `ef_construction` → Better index quality, slower build
- Higher `ef_search` → Better recall, slower search

### Distance Metrics

**L2 (Euclidean Distance) - Default:**
```
d(p, q) = sqrt(Σ(p_i - q_i)²)
```
Use for: General-purpose similarity, non-normalized vectors

**Cosine Similarity:**
```
similarity = (p · q) / (||p|| * ||q||)
distance = 1 - similarity
```
Use for: Text embeddings, direction-based similarity

**Dot Product:**
```
score = Σ(p_i * q_i)
```
Use for: Pre-normalized vectors, Maximum Inner Product Search (MIPS)

### Index Persistence

**Storage Strategy:**
- HNSW graph stored in PebbleDB alongside vectors
- Graph edges encoded in value field
- Atomic updates via PebbleDB batches
- Snapshot saves both vectors and HNSW graph

**Key Encoding:**
```
Graph Node:  hnsw\x00{ns}\x00{id:u64}\x00{level:u8}\x00 → neighbors[]
Entry Point: hnsw\x00{ns}\x00_entry\x00               → id + level
```

## Namespaces

Logical isolation for multi-tenancy and organization.

### Namespace Configuration

```go
type NamespaceConfig struct {
    Name       string           // Namespace identifier
    Dimensions int              // Vector dimensionality
    Metric     DistanceMetric   // L2, Cosine, DotProduct
    HNSWParams HNSWConfig       // Index parameters
}
```

### Operations

```bash
# Create namespace
quicbaas vector namespace create \
  --name documentation \
  --dimensions 1536 \
  --metric cosine

# List namespaces
quicbaas vector namespace list

# Get stats
quicbaas vector namespace stats --name documentation

# Delete namespace
quicbaas vector namespace delete --name documentation
```

## Hybrid Search

Combine metadata filtering with vector similarity for precise results.

### Query Structure

```go
type SearchRequest struct {
    QueryVector     []float32
    K               int
    Namespace       string
    Metric          DistanceMetric
    MetadataFilters map[string]string  // AND filters
    MinScore        float32             // Similarity threshold
}
```

### Example

```bash
# Search for similar documents with filters
quicbaas vector search \
  --query-file query.bin \
  --k 10 \
  --namespace docs \
  --filter "source=api-docs" \
  --filter "language=go" \
  --min-score 0.8
```

**Execution:**
1. Apply metadata filters to candidate set
2. Run HNSW search on filtered candidates
3. Re-rank by vector similarity
4. Return top-k results

## Performance Characteristics

### Latency Targets

| Operation | P50 | P99 | Dataset Size |
|-----------|-----|-----|--------------|
| UpsertVector | <3ms | <15ms | 10M vectors |
| GetVector | <1ms | <5ms | 10M vectors |
| SearchSimilar (k=10) | <10ms | <50ms | 10M vectors |
| BatchUpsert (100) | <50ms | <150ms | 10M vectors |

### Throughput

| Operation | Target QPS (per shard) |
|-----------|------------------------|
| UpsertVector | 1,000+ |
| SearchSimilar | 5,000+ |
| BatchUpsert | 500+ |

### Accuracy

- **Recall @ k=10:** 95%+ (default HNSW parameters)
- **Recall @ k=100:** 98%+

### Scalability

| Vector Count | Shards | RAM/Shard | Disk/Shard |
|--------------|--------|-----------|------------|
| 1M | 1 | 2 GB | 5 GB |
| 10M | 2 | 8 GB | 50 GB |
| 100M | 10 | 32 GB | 500 GB |
| 1B | 100 | 32 GB | 500 GB |

## RAG Integration

Optimized for Retrieval-Augmented Generation workflows.

### Document Chunking

```go
// Chunk documents
chunks := chunker.Split(document, 512) // 512 tokens per chunk

// Generate embeddings
for i, chunk := range chunks {
    embedding := embedModel.Encode(chunk.Text)

    client.UpsertVector(ctx, &pb.UpsertVectorRequest{
        Vector: embedding,
        Metadata: map[string]string{
            "document_id": doc.ID,
            "chunk_index": strconv.Itoa(i),
            "source": doc.Source,
            "title": doc.Title,
        },
        Namespace: "documentation",
    })
}
```

### Context Retrieval

```go
// Search for relevant context
query := "How do I configure TLS?"
queryEmbedding := embedModel.Encode(query)

results, _ := client.SearchSimilar(ctx, &pb.SearchSimilarRequest{
    QueryVector: queryEmbedding,
    K: 5,
    Namespace: "documentation",
    Metric: pb.DistanceMetric_DISTANCE_METRIC_COSINE,
    MinScore: 0.7,
})

// Build context for LLM
context := buildContext(results.Results)
response := llm.Complete(query, context)
```

## MCP Integration

Native support for Model Context Protocol.

### MCP Tools

```json
{
  "tools": [
    {
      "name": "store_embedding",
      "description": "Store a vector embedding with metadata",
      "parameters": {
        "vector": "array<float32>",
        "metadata": "object",
        "namespace": "string"
      }
    },
    {
      "name": "search_similar",
      "description": "Search for similar vectors",
      "parameters": {
        "query_vector": "array<float32>",
        "k": "integer",
        "namespace": "string",
        "filters": "object"
      }
    }
  ]
}
```

### Usage

```python
# Claude Desktop / MCP client
from mcp import Client

client = Client("quicbaas")

# Store documentation embeddings
client.call_tool("store_embedding", {
    "vector": doc_embedding,
    "metadata": {
        "source": "api-docs",
        "title": "Getting Started"
    },
    "namespace": "docs"
})

# Search for context
results = client.call_tool("search_similar", {
    "query_vector": question_embedding,
    "k": 5,
    "namespace": "docs",
    "filters": {"source": "api-docs"}
})
```

## Monitoring

### Prometheus Metrics

```
# Vector operations
quicbaas_vector_upsert_total{namespace, status}
quicbaas_vector_search_total{namespace, status}

# Performance
quicbaas_vector_search_latency_seconds{namespace, quantile}
quicbaas_vector_search_recall{namespace}

# Index health
quicbaas_hnsw_vector_count{namespace, shard}
quicbaas_hnsw_memory_bytes{namespace, shard}
quicbaas_hnsw_layer_distribution{namespace, layer}
```

### Health Checks

```bash
curl http://localhost:8080/health/vector
```

```json
{
  "status": "healthy",
  "shards": [
    {
      "id": 1,
      "vector_count": 1000000,
      "index_memory_mb": 200,
      "avg_search_latency_ms": 12
    }
  ]
}
```

## Best Practices

### Embedding Models

Choose dimensionality based on accuracy vs performance needs:

| Model | Dimensions | Use Case |
|-------|------------|----------|
| MiniLM-L6 | 384 | Fast, lightweight, good accuracy |
| BERT-base | 768 | Balanced quality/speed |
| OpenAI ada-002 | 1536 | High-quality semantic search |
| Cohere embed-v3 | 1024 | Multilingual support |

### Index Configuration

**High recall (>99%):**
```yaml
hnsw:
  m: 32
  ef_construction: 400
  ef_search: 100
```

**Balanced (95-98% recall):**
```yaml
hnsw:
  m: 16
  ef_construction: 200
  ef_search: 50
```

**Fast search (<5ms):**
```yaml
hnsw:
  m: 8
  ef_construction: 100
  ef_search: 20
```

### Memory Management

- **Float32 vs Float16:** Use Float16 for 50% memory savings with <1% accuracy loss
- **Batch Operations:** Use BatchUpsert for bulk imports to reduce overhead
- **Namespace Limits:** Keep namespaces under 10M vectors per shard for optimal performance

## References

### Implementation
- **Vector Service** - `pkg/service/vector.go`
- **qrdb SDK** - `github.com/jeremyhahn/go-qrdb/sdk/go` (vector operations delegated to qrdb engine)
- **gRPC Service** - `pkg/api/grpc/server_vector.go`

### Documentation
- **Integration Strategy** - `VECTOR_INTEGRATION_STRATEGY.md`
- **Quick Start** - `../guides/VECTOR_QUICK_START.md`
- **API Reference** - `../reference/VECTOR_API.md`
- **RAG Workflows** - `../guides/VECTOR_RAG_WORKFLOWS.md`

### External
- **HNSW Paper** - [arXiv:1603.09320](https://arxiv.org/abs/1603.09320)
- **FAISS** - [Facebook AI Similarity Search](https://github.com/facebookresearch/faiss)

---

**Last Updated:** 2025-12-19
**Version:** 0.3.0
