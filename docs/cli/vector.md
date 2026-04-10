# quicbaas vector

Vector similarity search commands for QuicBaaS vector storage.

## Overview

These commands allow you to manage vector namespaces, store embeddings, and perform similarity searches. All commands connect to a running QuicBaaS server via gRPC.

Alias: `vec`

## Namespace Management

### namespace create

Create a new vector namespace with specified dimensions.

```bash
quicbaas vector namespace create <name>
```

#### Flags
- `--dimension uint32`: Vector dimensionality (required)
- `--index-config stringToString`: Index configuration as key=value pairs
- `--metadata stringToString`: Namespace metadata as key=value pairs

#### Examples

```bash
# Create a namespace for 1536-dimensional OpenAI embeddings
quicbaas vector namespace create embeddings --dimension 1536

# Create with custom index configuration
quicbaas vector namespace create embeddings --dimension 768 --index-config M=16,efConstruction=200

# Create with metadata
quicbaas vector namespace create embeddings --dimension 384 --metadata model=all-MiniLM-L6-v2,purpose=search
```

### namespace delete

Delete a vector namespace and all its vectors.

```bash
quicbaas vector namespace delete <name>
```

#### Flags
- `-y, --yes`: Skip confirmation prompt

#### Examples

```bash
# Delete namespace with confirmation
quicbaas vector namespace delete embeddings

# Delete without confirmation
quicbaas vector namespace delete embeddings --yes
```

### namespace list

List all vector namespaces.

```bash
quicbaas vector namespace list
```

#### Flags
- `--limit int`: Maximum number of namespaces to return (default: 100)
- `--offset int`: Offset for pagination

#### Examples

```bash
# List all namespaces
quicbaas vector namespace list

# List with pagination
quicbaas vector namespace list --limit 50 --offset 100
```

### namespace info

Show detailed information about a vector namespace.

```bash
quicbaas vector namespace info <name>
```

#### Examples

```bash
# Show namespace info
quicbaas vector namespace info embeddings

# Show info as JSON
quicbaas vector namespace info embeddings --output json
```

## Vector Operations

### upsert

Insert or update a single vector.

```bash
quicbaas vector upsert <namespace>
```

#### Flags
- `--id string`: Vector ID (required)
- `--data string`: Vector data as JSON array (required)
- `--metadata stringToString`: Vector metadata as key=value pairs

#### Examples

```bash
# Upsert a vector
quicbaas vector upsert embeddings --id doc-1 --data '[0.1, 0.2, 0.3, ...]'

# Upsert with metadata
quicbaas vector upsert embeddings --id doc-1 --data '[0.1, 0.2, ...]' --metadata title=Document,type=article
```

### batch-upsert

Insert or update multiple vectors from a JSON file.

```bash
quicbaas vector batch-upsert <namespace> <json-file>
```

#### Example JSON

```json
[
  {
    "id": "doc-1",
    "vector": [0.1, 0.2, 0.3, ...],
    "metadata": {"title": "Document 1"}
  },
  {
    "id": "doc-2",
    "vector": [0.4, 0.5, 0.6, ...],
    "metadata": {"title": "Document 2"}
  }
]
```

#### Examples

```bash
# Batch upsert from file
quicbaas vector batch-upsert embeddings vectors.json
```

### get

Retrieve a vector by ID.

```bash
quicbaas vector get <namespace> <id>
```

#### Flags
- `--include-vectors`: Include vector data in response
- `--include-metadata`: Include vector metadata in response

#### Examples

```bash
# Get vector without data
quicbaas vector get embeddings doc-1

# Get vector with data and metadata
quicbaas vector get embeddings doc-1 --include-vectors --include-metadata
```

### delete

Delete a single vector by ID.

```bash
quicbaas vector delete <namespace> <id>
```

#### Flags
- `-y, --yes`: Skip confirmation prompt

#### Examples

```bash
# Delete with confirmation
quicbaas vector delete embeddings doc-1

# Delete without confirmation
quicbaas vector delete embeddings doc-1 --yes
```

### batch-delete

Delete multiple vectors by ID.

```bash
quicbaas vector batch-delete <namespace>
```

#### Flags
- `--ids stringSlice`: Vector IDs to delete (comma-separated) (required)
- `-y, --yes`: Skip confirmation prompt

#### Examples

```bash
# Delete multiple vectors
quicbaas vector batch-delete embeddings --ids doc-1,doc-2,doc-3

# Delete without confirmation
quicbaas vector batch-delete embeddings --ids doc-1,doc-2 --yes
```

### list

List vectors in a namespace with pagination.

```bash
quicbaas vector list <namespace>
```

#### Flags
- `--limit int`: Maximum number of vectors to return (default: 100)
- `--offset int`: Offset for pagination
- `--prefix string`: Filter by ID prefix
- `--include-vectors`: Include vector data in response
- `--include-metadata`: Include vector metadata in response

#### Examples

```bash
# List vectors
quicbaas vector list embeddings

# List with pagination
quicbaas vector list embeddings --limit 50 --offset 100

# List with prefix filter
quicbaas vector list embeddings --prefix doc-

# List with vectors and metadata
quicbaas vector list embeddings --include-vectors --include-metadata
```

## Search Operations

### search similar

Search for similar vectors using a query vector.

```bash
quicbaas vector search similar <namespace>
```

#### Flags
- `--data string`: Query vector as JSON array (required)
- `--k int`: Number of results to return (default: 10)
- `--ef-search int`: Search quality parameter (higher = better quality, slower)
- `--min-score float32`: Minimum similarity score
- `--include-vectors`: Include vector data in results
- `--include-metadata`: Include vector metadata in results
- `--metadata-filter stringToString`: Filter by metadata
- `--rerank-algorithm string`: Reranking algorithm (bm25, hybrid)

#### Examples

```bash
# Search for similar vectors
quicbaas vector search similar embeddings --data '[0.1, 0.2, 0.3, ...]' --k 10

# Search with metadata filter
quicbaas vector search similar embeddings --data '[0.1, 0.2, ...]' --k 10 --metadata-filter type=article

# Search with higher quality
quicbaas vector search similar embeddings --data '[0.1, 0.2, ...]' --k 10 --ef-search 200

# Search with reranking
quicbaas vector search similar embeddings --data '[0.1, 0.2, ...]' --k 10 --rerank-algorithm hybrid
```

### search by-id

Search for vectors similar to an existing vector.

```bash
quicbaas vector search by-id <namespace> <id>
```

#### Flags
- `--k int`: Number of results to return (default: 10)
- `--ef-search int`: Search quality parameter
- `--min-score float32`: Minimum similarity score
- `--include-vectors`: Include vector data in results
- `--include-metadata`: Include vector metadata in results

#### Examples

```bash
# Find similar vectors to doc-1
quicbaas vector search by-id embeddings doc-1 --k 10

# Find similar with high quality
quicbaas vector search by-id embeddings doc-1 --k 10 --ef-search 200
```

## Index Management

### index rebuild

Rebuild the vector index for a namespace.

```bash
quicbaas vector index rebuild <namespace>
```

#### Flags
- `--async`: Rebuild asynchronously in background

#### Examples

```bash
# Rebuild index synchronously
quicbaas vector index rebuild embeddings

# Rebuild index asynchronously
quicbaas vector index rebuild embeddings --async
```

### index stats

Show index statistics for a namespace.

```bash
quicbaas vector index stats <namespace>
```

#### Examples

```bash
# Show index statistics
quicbaas vector index stats embeddings

# Show stats as JSON
quicbaas vector index stats embeddings --output json
```

## Index Configuration

### HNSW Parameters
- **M**: Number of connections per layer (default: 16)
  - Higher = better recall, more memory
  - Typical range: 12-48
- **efConstruction**: Construction-time search quality (default: 200)
  - Higher = better index quality, slower construction
  - Typical range: 100-500
- **efSearch**: Search-time quality parameter (runtime configurable)
  - Higher = better recall, slower search
  - Typical range: 50-500

## Metadata Filtering

Metadata filters use key=value pairs:
- Exact match: `type=article`
- Multiple filters: `type=article,status=published`
- Operators (via filter-operator flag):
  - `and`: All filters must match (default)
  - `or`: Any filter must match

## Notes

- All vectors in a namespace must have the same dimensionality
- Vector IDs must be unique within a namespace
- Similarity scores range from 0 (dissimilar) to 1 (identical)
- Batch operations are atomic (all succeed or all fail)
- Index rebuilding may take time for large datasets
- Async rebuild allows database to remain operational
- efSearch can be tuned per query for performance/quality tradeoff
