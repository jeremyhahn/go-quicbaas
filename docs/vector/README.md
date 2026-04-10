# Vector Database

QuicBaaS includes a high-performance vector database with HNSW, FLAT, and IVF index types, SIMD-accelerated distance functions, rich metadata filtering, scalar indexes, quantization, distributed search, and GPU acceleration support.

## Architecture

```
go-quicbaas/                         go-qrdb/
  pkg/interfaces/                      pkg/vector/
    vector_index.go    (VectorIndex)     hnsw/     (HNSW graph index)
    vector_storage.go  (VectorStorage)   flat/     (brute-force index)
    hnsw.go           (backward compat)  ivf/      (IVF index)
    scalar_index.go   (ScalarIndex)      scalar/   (bitmap/inverted/sorted)
  pkg/vector/                            quantization/ (SQ8/PQ/Float16)
    filter/           (expression AST)   bloom/    (segment pruning)
    autoindex/        (recommendations)  gpu/      (cuVS stubs)
  pkg/service/
    vector.go         (VectorService)
    vector_distributed.go (scatter-gather)
    vector_merge.go   (multi-way merge)
    vector_shard_router.go
    vector_compaction.go (state machine)
  pkg/server/
    index_factory.go  (VectorIndexFactory)
    hnsw_factory.go   (HNSW adapter)
    gpu_index_factory_stub.go
```

## Index Types

| Type | Best For | Recall | Complexity |
|------|----------|--------|------------|
| FLAT | <10K vectors | 100% | O(n*d) |
| HNSW | <1M vectors | ~95-99% | O(log n) |
| IVF_FLAT | <10M vectors | ~90-95% | O(k*d) |
| IVF_PQ | >10M vectors | ~85-90% | O(k*d/m) |
| GPU_CAGRA | >100K + GPU | ~95-99% | Hardware |
| GPU_IVF_FLAT | >1M + GPU | ~90-95% | Hardware |
| GPU_IVF_PQ | >10M + GPU | ~85-90% | Hardware |
| GPU_BRUTE_FORCE | Any + GPU | 100% | Hardware |

## Distance Metrics

All metrics have SIMD-accelerated implementations via `github.com/viterin/vek`:

| Metric | Description | Range |
|--------|-------------|-------|
| L2 (Euclidean) | sqrt(sum((a-b)^2)) | [0, inf) |
| Cosine | 1 - cos(a,b) | [0, 2] |
| Dot Product | -sum(a*b) | (-inf, inf) |
| Manhattan (L1) | sum(abs(a-b)) | [0, inf) |
| Hamming | Count of differing positions | [0, dim] |

Batch distance computation via `CalcDistanceBatch()` distributes work across `runtime.NumCPU()` goroutines for batches > 256 vectors.

## Filter Expressions

Rich expression language for metadata-based filtering:

```
age > 25 AND category IN ["tech", "science"]
status = "active" OR priority >= 5
NOT (deleted = "true")
title CONTAINS "golang"
name STARTS_WITH "al"
price >= 10.5 AND price <= 100
name != null
```

### Operators

`=`, `!=`, `>`, `>=`, `<`, `<=`, `IN`, `NOT IN`, `CONTAINS`, `STARTS_WITH`, `TEXT_MATCH` (case-insensitive), `IS NULL`, `IS NOT NULL`

### Expression Cache

LRU cache (`sync.Map`-based, lock-free reads) stores parsed ASTs. Default size: 1024 entries. Configure via `filter_cache_size`.

## Scalar Indexes

Accelerate metadata filters from O(n) scan to O(1)/O(log n):

| Type | Best For | Operations |
|------|----------|------------|
| Bitmap (Roaring) | Low-cardinality, booleans | Equal, In, IsNull (bitmap AND/OR) |
| Inverted | High-cardinality strings | Equal, Contains, PrefixMatch |
| Sorted (B-tree) | Numeric range queries | GreaterThan, LessEqual, Range |

Auto-selection based on field cardinality and data type.

## Quantization

| Type | Compression | Recall | Use Case |
|------|-------------|--------|----------|
| SQ8 | 4x | >95% | General purpose |
| PQ | 32x+ | >90% | Very large datasets |
| Float16 | 2x | >99% | Minimal loss |
| BFloat16 | 2x | >99% | Preserves exponent range |

SQ8 uses per-dimension min/max calibration. PQ uses k-means codebook training with asymmetric distance tables for O(M) lookup.

## Distributed Search

Scatter-gather k-NN across Raft shard replicas:

1. `ShardRouter` maps namespace to shards
2. `DistributedSearchCoordinator` fans out parallel searches
3. Per-shard results merged via min-heap (`MergeSearchResults`)
4. Deduplication by vector ID (keeps smallest distance)
5. Partial failure support (returns results from healthy shards)

## Auto-Index Recommender

Automatically recommends optimal index type based on dataset profile:

- <10K vectors: FLAT
- <1M vectors: HNSW
- <10M vectors: IVF_FLAT
- >10M vectors: IVF_PQ
- GPU available + >100K: GPU_CAGRA
- High recall target (>0.99): HNSW or FLAT
- Memory constrained: IVF_PQ + PQ quantization

## Build Tags

| Feature | Tags | Flag | CGo |
|---------|------|------|-----|
| Vector | `vector qrdb_vector` | `WITH_VECTOR=1` | No |
| GPU | `vector qrdb_vector gpu` | `WITH_GPU=1` | Yes |

## Makefile Targets

```bash
# Distance functions
make test-hnsw-distance
make bench-hnsw-distance

# Filter expressions
make test-vector-filter

# Scalar indexes
make test-vector-scalar-index

# Index types (FLAT, HNSW)
make test-vector-indexes
make bench-vector-indexes

# GPU (stubs without hardware)
make test-vector-gpu

# Distributed search
make test-vector-distributed

# Quantization
make test-vector-quantization
make bench-vector-quantization

# Auto-index
make test-vector-autoindex

# Bloom filter
make test-vector-bloom

# Aggregate coverage
make coverage-vector
```
