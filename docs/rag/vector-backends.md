# Vector Backends

Vector index backend selection for the RAG auto-indexing pipeline.

Source: `pkg/rag/indexing/vector_adapter.go`

Build tag: `rag`

## Backend Selection

The `NewVectorIndexFromConfig()` function selects a vector index backend based on configuration:

| Backend | Config Value | Description | Status |
|---------|-------------|-------------|--------|
| Auto | `"auto"` | Try HNSW, fall back to memory | Currently resolves to memory |
| Memory | `"memory"` | In-memory brute-force search | Available |
| HNSW | `"hnsw"` | Hierarchical Navigable Small World graph | Falls back to memory (Phase 10) |
| GPU CAGRA | `"gpu_cagra"` | GPU-accelerated approximate nearest neighbor | Not available (`GPUUnavailableError`) |

## Auto-Detection Logic

```
NewVectorIndexFromConfig(cfg, dimensions)
    |
    +-- gpu_accelerated=true? --> GPUUnavailableError
    |
    +-- index_type="auto"?   --> try HNSW, fall back to memory
    +-- index_type="memory"? --> in-memory brute-force
    +-- index_type="hnsw"?   --> try HNSW, fall back to memory
    +-- unknown?             --> UnsupportedIndexTypeError
```

The handler map provides O(1) dispatch:

```go
var indexTypeHandlers = map[string]func(cfg IndexingConfig, dimensions int) (rag.VectorIndex, error){
    "auto":   newAutoIndex,
    "memory": newMemoryIndex,
    "hnsw":   newHNSWOrMemoryIndex,
}
```

## Build Tags

| Tag | Effect |
|-----|--------|
| `rag` | Enables the indexing package (required) |
| `rag` + `qrdb_vector` | Enables QRDB-backed vector storage (future) |

Without the `rag` build tag, the entire `pkg/rag/indexing/` package is excluded from compilation.

## Performance Characteristics

| Backend | Search | Insert | Memory | Accuracy | Best For |
|---------|--------|--------|--------|----------|----------|
| Memory (brute-force) | O(n) | O(1) | Linear | Exact (100%) | Small indices (<100K vectors) |
| HNSW | O(log n) | O(log n) | ~1.5x | Approximate (~95-99%) | Medium indices (100K-10M) |
| GPU CAGRA | O(1) amortized | O(1) batched | GPU VRAM | Approximate (~95-99%) | Large indices (>10M) |

## Configuration

```yaml
settings:
  vector_db:
    index_type: auto         # auto | memory | hnsw
    gpu_accelerated: false   # true requires GPU hardware + build tags
```

## Current State

As of the current implementation:

- `auto` and `hnsw` both resolve to the in-memory brute-force index
- GPU acceleration returns `GPUUnavailableError`
- The HNSW integration is planned for Phase 10 via the `qrdb` package
- All backends use cosine distance (`rag.CosineDistance`) as the default metric
